package main

import (
	"crypto/subtle"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Each Pi process receives only this ephemeral generation capability. It does
// not receive the application's bearer token or administrative API access.
func (s *PiWorkspaceSession) startBridge() error {
	if s.bridgeServer != nil {
		return nil
	}
	dir, e := os.MkdirTemp("", "strixglm-pi-")
	if e != nil {
		return e
	}
	s.bridgeDir = dir
	listener, e := net.Listen("unix", filepath.Join(dir, "api.sock"))
	if e != nil {
		return e
	}
	s.bridgeToken = id() + id()
	if e = os.Chmod(filepath.Join(dir, "api.sock"), 0600); e != nil {
		_ = listener.Close()
		return e
	}
	// Rewritten by the in-namespace launcher once its internal loopback listener
	// chooses a free port. The host exposes only this private Unix socket.
	s.bridgeURL = "http://127.0.0.1:1"
	s.bridgeServer = &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		supplied := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(supplied), []byte(s.bridgeToken)) != 1 {
			jsonReply(w, 401, map[string]string{"error": "session capability required"})
			return
		}
		if r.Method == "GET" && r.URL.Path == "/v1/models" {
			jsonReply(w, 200, map[string]any{"object": "list", "data": []map[string]string{{"id": s.manager.app.cfg.Model, "object": "model"}}})
			return
		}
		if r.Method != "POST" || r.URL.Path != "/v1/chat/completions" {
			jsonReply(w, 403, map[string]string{"error": "Pi capability is limited to chat completions and model discovery"})
			return
		}
		s.manager.mu.Lock()
		ready, reason, closing := s.manager.ready, s.manager.reason, s.manager.closing
		s.manager.mu.Unlock()
		s.mu.Lock()
		active := s.State == "RUNNING" || s.State == "ABORTING"
		s.mu.Unlock()
		if !ready || closing || !active {
			jsonReply(w, 409, map[string]string{"error": "explicit active Pi prompt required; " + reason})
			return
		}
		r.Header.Set("Authorization", "Bearer "+s.manager.app.token)
		s.manager.app.proxyChat(w, r)
	})}
	go func() { _ = s.bridgeServer.Serve(listener) }()
	return nil
}
