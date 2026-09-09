package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func operationTestApp(t *testing.T) (*App, *operationManager, http.Handler) {
	t.Helper()
	base := t.TempDir()
	cfg := Config{Model: "GLM5.3-Flash-CIRU-STRIX-IU4", StateDir: filepath.Join(base, "state"), PairLock: filepath.Join(base, "pair.lock"), ModelTimeout: 5, DefaultProfile: "fast", Profiles: map[string]Profile{"fast": {Reasoning: "low", ContextTokens: 4096, MaxTokens: 4096}}, MaxContextTokens: 50000, WorkspaceRoots: []string{base}, MaxFiles: 100, MaxRepoBytes: 1 << 20, SandboxTasks: 64}
	a, e := newApp(cfg)
	if e != nil {
		t.Fatal(e)
	}
	m := operationsFor(a)
	if m.initErr != nil {
		t.Fatal(m.initErr)
	}
	m.options = func() []OperationOption {
		return []OperationOption{{ID: "fixture", Available: true, RequiresConfirmation: true}, {ID: "blocked", BlockedReason: "no audited compatible asset"}}
	}
	mux := http.NewServeMux()
	a.registerOperationRoutes(mux)
	t.Cleanup(func() { operationManagers.Delete(a) })
	return a, m, mux
}
func operationRequest(a *App, h http.Handler, method, path, body string, authenticated bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if authenticated {
		r.Header.Set("Authorization", "Bearer "+a.token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func waitOperation(t *testing.T, job *OperationJob) {
	t.Helper()
	select {
	case <-job.done:
	case <-time.After(5 * time.Second):
		t.Fatal("operation did not finish")
	}
}

func TestOperationsRequireAuthConfirmationAndAllowlist(t *testing.T) {
	a, m, h := operationTestApp(t)
	var calls atomic.Int32
	m.execute = func(context.Context, *OperationJob) error { calls.Add(1); return nil }
	for _, tc := range []struct {
		auth   bool
		body   string
		status int
	}{
		{false, `{"action_id":"fixture","confirm":true}`, 401},
		{true, `{"action_id":"fixture","confirm":false}`, 400},
		{true, `{"action_id":"fixture","confirm":true,"url":"http://evil"}`, 400},
		{true, `{"action_id":"fixture","confirm":true,"command":["rm"]}`, 400},
		{true, `{"action_id":"unknown","confirm":true}`, 400},
		{true, `{"action_id":"blocked","confirm":true}`, 409},
	} {
		w := operationRequest(a, h, "POST", "/v1/operations/jobs", tc.body, tc.auth)
		if w.Code != tc.status {
			t.Fatalf("status%d want%d: %s", w.Code, tc.status, w.Body.String())
		}
	}
	if calls.Load() != 0 {
		t.Fatal("rejected request executed an operation")
	}
}

func TestOperationsCancelDrainsAndKeepsGlobalExclusion(t *testing.T) {
	a, m, h := operationTestApp(t)
	started := make(chan struct{})
	drain := make(chan struct{})
	m.execute = func(ctx context.Context, j *OperationJob) error {
		close(started)
		<-ctx.Done()
		<-drain
		return ctx.Err()
	}
	job, _, e := m.start("fixture")
	if e != nil {
		t.Fatal(e)
	}
	<-started
	w := operationRequest(a, h, "POST", "/v1/operations/jobs/"+job.ID+"/cancel", `{"confirm":true}`, true)
	if w.Code != 202 {
		t.Fatal(w.Body.String())
	}
	select {
	case <-job.done:
		t.Fatal("cancel returned terminal before paired drain")
	default:
	}
	if _, code, e := m.start("fixture"); e == nil || code != 409 {
		t.Fatal("second operation admitted during drain")
	}
	b := a.cfg
	b.StateDir = filepath.Join(t.TempDir(), "other")
	other, e := newApp(b)
	if e != nil {
		t.Fatal(e)
	}
	defer operationManagers.Delete(other)
	om := operationsFor(other)
	om.options = m.options
	om.execute = func(context.Context, *OperationJob) error { return nil }
	if _, code, e := om.start("fixture"); e == nil || code != 409 {
		t.Fatal("second gateway bypassed global operation lock")
	}
	close(drain)
	waitOperation(t, job)
	if job.snapshot()["status"] != "CANCELLED" {
		t.Fatal(job.snapshot())
	}
	for _, name := range []string{"request.json", "job.json"} {
		if _, e = os.Stat(filepath.Join(job.RawDirectory, name)); e != nil {
			t.Fatal(e)
		}
	}
}

func TestOperationsPersistAndNeverReplayInterruptedJob(t *testing.T) {
	a, m, _ := operationTestApp(t)
	m.execute = func(context.Context, *OperationJob) error { return nil }
	job, _, e := m.start("fixture")
	if e != nil {
		t.Fatal(e)
	}
	waitOperation(t, job)
	if e = job.update(func() { job.Status = "running"; job.Finished = "" }); e != nil {
		t.Fatal(e)
	}
	operationManagers.Delete(a)
	reloaded := operationsFor(a)
	saved := reloaded.find(job.ID)
	if saved == nil || saved.snapshot()["status"] != "INTERRUPTED" || reloaded.active != "" {
		t.Fatal("interrupted operation replayed or not recorded")
	}
	b, e := os.ReadFile(filepath.Join(job.RawDirectory, "job.json"))
	if e != nil || !bytes.Contains(b, []byte("INTERRUPTED")) {
		t.Fatal("interruption not persisted")
	}
}

func TestOperationPinnedInputsRejectMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.json")
	data := []byte("frozen")
	sum := sha256.Sum256(data)
	if e := os.WriteFile(path, data, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := readOperationPinned(path, hex.EncodeToString(sum[:])); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path, []byte("changed"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := readOperationPinned(path, hex.EncodeToString(sum[:])); e == nil {
		t.Fatal("mutable pinned input accepted")
	}
}

func operationSSE(w http.ResponseWriter, generationMS int) {
	w.Header().Set("Content-Type", "text/event-stream")
	fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"42\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":15,\"completion_tokens\":10,\"total_tokens\":25},\"metrics\":{\"generation_time_ms\":%d,\"time_to_first_token_ms\":3,\"speculative_decoding\":{\"draft_acceptance_rate\":0.75}}}\n\ndata: [DONE]\n\n", generationMS)
}
func TestOperationSpeedExcludesWarmupAndCollectsRealMetrics(t *testing.T) {
	a, m, _ := operationTestApp(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Error("wrong route")
		}
		var payload map[string]any
		if e := json.NewDecoder(r.Body).Decode(&payload); e != nil {
			t.Error(e)
		}
		if payload["max_tokens"] != float64(768) || object(payload["chat_template_kwargs"])["reasoning_effort"] != "low" {
			t.Error("unexpected benchmark payload")
		}
		operationSSE(w, int(calls.Add(1)))
	}))
	defer server.Close()
	a.cfg.Backend = server.URL
	a.client = server.Client()
	m.options = func() []OperationOption { return []OperationOption{{ID: "speed-chat", Available: true}} }
	job, _, e := m.start("speed-chat")
	if e != nil {
		t.Fatal(e)
	}
	waitOperation(t, job)
	result := object(job.snapshot()["result"])
	if calls.Load() != 4 || job.snapshot()["status"] != "PASS" || result["measured_runs"] != float64(3) || result["decode_tps_median"] != float64(3000) {
		t.Fatalf("warmup/metrics mismatch: %v", job.snapshot())
	}
}
func TestOperationGenerationCancellationDoesNotAbortBackend(t *testing.T) {
	a, m, _ := operationTestApp(t)
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		close(started)
		select {
		case <-release:
		case <-r.Context().Done():
			t.Error("client cancellation aborted paired transport")
			return
		}
		operationSSE(w, 10)
	}))
	defer server.Close()
	a.cfg.Backend = server.URL
	a.client = server.Client()
	m.options = func() []OperationOption { return []OperationOption{{ID: "speed-chat", Available: true}} }
	job, _, e := m.start("speed-chat")
	if e != nil {
		t.Fatal(e)
	}
	<-started
	job.cancel()
	select {
	case <-job.done:
		t.Fatal("operation ended before drain")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	waitOperation(t, job)
	if calls.Load() != 1 || job.snapshot()["status"] != "CANCELLED" {
		t.Fatal(job.snapshot())
	}
}
func TestOperationShutdownClosesAdmissionAndDrains(t *testing.T) {
	a, m, _ := operationTestApp(t)
	entered := make(chan struct{})
	m.execute = func(ctx context.Context, j *OperationJob) error { close(entered); <-ctx.Done(); return ctx.Err() }
	job, _, e := m.start("fixture")
	if e != nil {
		t.Fatal(e)
	}
	<-entered
	if e = a.shutdownOperations(context.Background()); e != nil {
		t.Fatal(e)
	}
	if job.snapshot()["status"] != "CANCELLED" {
		t.Fatal(job.snapshot())
	}
	if _, code, e := m.start("fixture"); e == nil || code != 503 {
		t.Fatal("shutdown accepted another operation")
	}
}

func downloadFixture(t *testing.T, data []byte) (*OperationJob, CatalogAsset, string) {
	t.Helper()
	base := t.TempDir()
	sum := sha256.Sum256(data)
	job := &OperationJob{ID: id(), ActionID: "download:fixture", Status: "running", RawDirectory: t.TempDir(), LogTail: []string{}, done: make(chan struct{})}
	asset := CatalogAsset{ID: "fixture", Path: filepath.Join(base, "nested", "weights.bin"), Node: "NODE01", ExpectedBytes: int64(len(data)), SHA256: hex.EncodeToString(sum[:]), Revision: strings.Repeat("a", 40), DownloadEnabled: true}
	return job, asset, base
}
func TestDownloadResumeValidatesRangeHashAndNeverOverwrites(t *testing.T) {
	data := []byte("immutable model fixture")
	job, asset, base := downloadFixture(t, data)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		if call == 1 {
			if r.Header.Get("Range") != fmt.Sprintf("bytes=0-%d", len(data)-1) {
				t.Error("initial range missing")
			}
			w.Header().Set("Content-Length", fmt.Sprint(len(data)))
			w.WriteHeader(200)
			_, _ = w.Write(data[:7])
			return
		}
		if r.Header.Get("Range") != fmt.Sprintf("bytes=7-%d", len(data)-1) {
			t.Error("resume offset wrong")
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 7-%d/%d", len(data)-1, len(data)))
		w.Header().Set("Content-Length", fmt.Sprint(len(data)-7))
		w.WriteHeader(206)
		_, _ = w.Write(data[7:])
	}))
	defer server.Close()
	asset.URL = server.URL + "/pinned"
	if e := downloadPinnedOperation(context.Background(), job, asset, base, server.Client()); e == nil {
		t.Fatal("truncated transfer accepted")
	}
	part, e := os.ReadFile(asset.Path + ".strixglm.part")
	if e != nil || !bytes.Equal(part, data[:7]) {
		t.Fatalf("partial not preserved: %q %v", part, e)
	}
	if e = downloadPinnedOperation(context.Background(), job, asset, base, server.Client()); e != nil {
		t.Fatal(e)
	}
	complete, e := os.ReadFile(asset.Path)
	if e != nil || !bytes.Equal(complete, data) {
		t.Fatal("completed asset differs")
	}
	if e = downloadPinnedOperation(context.Background(), job, asset, base, server.Client()); e == nil || calls.Load() != 2 {
		t.Fatal("existing model was overwritten/downloaded")
	}
	if job.snapshot()["result"].(map[string]any)["verification"] != "SHA256_VERIFIED_THIS_DOWNLOAD" {
		t.Fatal(job.snapshot())
	}
}
func TestDownloadRejectsWrongRangeAndHash(t *testing.T) {
	for _, kind := range []string{"wrong-range", "wrong-hash"} {
		t.Run(kind, func(t *testing.T) {
			data := []byte("expected")
			job, asset, base := downloadFixture(t, data)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Length", fmt.Sprint(len(data)))
				if kind == "wrong-range" {
					w.Header().Set("Content-Range", "bytes 1-8/8")
					w.WriteHeader(206)
					_, _ = w.Write(data)
				} else {
					_, _ = w.Write([]byte("corrupt!"))
				}
			}))
			defer server.Close()
			asset.URL = server.URL + "/asset"
			if e := downloadPinnedOperation(context.Background(), job, asset, base, server.Client()); e == nil {
				t.Fatal("invalid transfer accepted")
			}
			if _, e := os.Lstat(asset.Path); !os.IsNotExist(e) {
				t.Fatal("invalid asset published")
			}
		})
	}
}
func TestDownloadRejectsSymlinkParentAndUnownedPartial(t *testing.T) {
	for _, kind := range []string{"symlink-parent", "symlink-partial", "unowned-partial", "disk-budget"} {
		t.Run(kind, func(t *testing.T) {
			job, asset, base := downloadFixture(t, []byte("data"))
			asset.URL = "http://127.0.0.1:1/not-requested"
			if kind == "symlink-parent" {
				if e := os.Symlink(t.TempDir(), filepath.Dir(asset.Path)); e != nil {
					t.Fatal(e)
				}
			} else {
				if e := os.MkdirAll(filepath.Dir(asset.Path), 0700); e != nil {
					t.Fatal(e)
				}
				switch kind {
				case "symlink-partial":
					if e := os.Symlink(filepath.Join(t.TempDir(), "elsewhere"), asset.Path+".strixglm.part"); e != nil {
						t.Fatal(e)
					}
				case "unowned-partial":
					if e := os.WriteFile(asset.Path+".strixglm.part", []byte("d"), 0600); e != nil {
						t.Fatal(e)
					}
				case "disk-budget":
					asset.ExpectedBytes = 1 << 62
				}
			}
			if e := downloadPinnedOperation(context.Background(), job, asset, base, http.DefaultClient); e == nil {
				t.Fatal("unsafe download accepted")
			}
			if _, e := os.Lstat(asset.Path); !os.IsNotExist(e) {
				t.Fatal("unsafe destination published")
			}
		})
	}
}
