package app

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func nativeShutdownServer(t *testing.T, p *PairedBackend, grace time.Duration) (context.CancelFunc, <-chan error, string) {
	t.Helper()
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	server := &http.Server{Handler: p, ReadHeaderTimeout: time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serveNativePairUntil(ctx, server, p, listener, grace) }()
	t.Cleanup(func() { cancel(); _ = server.Close() })
	return cancel, done, "http://" + listener.Addr().String()
}

func TestNativeGracefulShutdownWaitsForDetachedRankDrain(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	h := func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		started <- struct{}{}
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, pairedResponse("done"))
	}
	p := pairedTestBackend(t, h, h)
	stop, serverDone, url := nativeShutdownServer(t, p, time.Second)
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	defer cancelRequest()
	request, _ := http.NewRequestWithContext(requestCtx, "POST", url+"/v1/chat/completions", strings.NewReader(`{"seed":1}`))
	request.Header.Set("Content-Type", "application/json")
	clientDone := make(chan struct{})
	go func() {
		r, _ := http.DefaultClient.Do(request)
		if r != nil {
			_ = r.Body.Close()
		}
		close(clientDone)
	}()
	<-started
	<-started
	cancelRequest()
	<-clientDone
	stop()
	deadline := time.Now().Add(time.Second)
	for {
		p.mu.Lock()
		closing := p.closing
		p.mu.Unlock()
		if closing {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("shutdown did not close admission")
		}
		time.Sleep(time.Millisecond)
	}
	if w := pairedTestRequest(p, `{"seed":2}`); w.Code != 503 {
		t.Fatal("shutdown accepted a new generation")
	}
	select {
	case e := <-serverDone:
		t.Fatalf("service exited before detached ranks drained: %v", e)
	case <-time.After(25 * time.Millisecond):
	}
	once.Do(func() { close(release) })
	select {
	case e := <-serverDone:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("service did not finish graceful drain")
	}
	if _, e := os.Stat(p.marker()); !os.IsNotExist(e) {
		t.Fatal("successful graceful drain left uncertainty")
	}
}

func TestNativeShutdownDeadlineKeepsPoisonAfterLateCompletion(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	h := func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		started <- struct{}{}
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, pairedResponse("late"))
	}
	p := pairedTestBackend(t, h, h)
	stop, serverDone, _ := nativeShutdownServer(t, p, 30*time.Millisecond)
	requestDone := make(chan struct{})
	go func() { _ = pairedTestRequest(p, `{"seed":1}`); close(requestDone) }()
	<-started
	<-started
	stop()
	select {
	case e := <-serverDone:
		if e == nil {
			t.Fatal("shutdown deadline incorrectly succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown deadline ignored")
	}
	once.Do(func() { close(release) })
	select {
	case <-requestDone:
	case <-time.After(time.Second):
		t.Fatal("late rank responses did not drain")
	}
	if _, e := os.Stat(p.marker()); e != nil {
		t.Fatal("late completion cleared shutdown poison")
	}
	p.mu.Lock()
	poison := p.poison
	p.mu.Unlock()
	if !strings.Contains(poison, "shutdown deadline") {
		t.Fatalf("lost shutdown provenance: %s", poison)
	}
}

func TestNativeIdleShutdownDoesNotCallModel(t *testing.T) {
	h := func(w http.ResponseWriter, r *http.Request) { t.Error("idle shutdown sent a model request") }
	p := pairedTestBackend(t, h, h)
	stop, done, _ := nativeShutdownServer(t, p, time.Second)
	stop()
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("idle shutdown blocked")
	}
}
