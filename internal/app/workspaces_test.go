package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func workspaceTestApp(t *testing.T) (*App, string) {
	t.Helper()
	base := t.TempDir()
	project := filepath.Join(base, "project")
	if e := os.Mkdir(project, 0700); e != nil {
		t.Fatal(e)
	}
	a, e := newApp(Config{StateDir: filepath.Join(base, "state"), WorkspaceRoots: []string{base}, Model: "test-model", Listen: "127.0.0.1:1", DefaultProfile: "low", Profiles: map[string]Profile{"low": {Reasoning: "low"}}, ModelTimeout: 30, MaxContextTokens: 4096, ChatDefaultOutput: 128})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.shutdownWorkspaces(ctx)
		workspaceManagers.Delete(a)
	})
	return a, project
}
func workspaceRequest(t *testing.T, a *App, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	a.registerWorkspaceRoutes(mux)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.token)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}
func TestWorkspaceNoImplicitPrompt(t *testing.T) {
	a, project := workspaceTestApp(t)
	m := workspacesFor(a)
	s, e := m.create("local", project, "", "low")
	if e != nil {
		t.Fatal(e)
	}
	if s.State != "CONNECTED" {
		t.Fatal(s.State)
	}
	if e = s.prompt("do work"); e == nil {
		t.Fatal("unqualified tools accepted")
	}
	if s.cmd != nil {
		t.Fatal("create started Pi")
	}
	options := m.options()["pi"].(map[string]any)
	if options["tools_available"] != false {
		t.Fatal(options)
	}
}
func TestWorkspaceConfirmationAndUnknownFields(t *testing.T) {
	a, project := workspaceTestApp(t)
	for _, body := range []string{`{"kind":"local","root":"` + project + `"}`, `{"kind":"local","root":"` + project + `","confirm":true,"command":"rm"}`, `{"kind":"local","root":"` + project + `","confirm":true,"password":"not-saved"}`} {
		w := workspaceRequest(t, a, "POST", "/v1/workspaces/sessions", body)
		if w.Code != 400 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
}
func TestWorkspaceAuthRequired(t *testing.T) {
	a, _ := workspaceTestApp(t)
	mux := http.NewServeMux()
	a.registerWorkspaceRoutes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/v1/workspaces/options", nil))
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
}
func TestWorkspaceRootAndFilesConfinement(t *testing.T) {
	a, project := workspaceTestApp(t)
	if _, e := validateLocalWorkspace(a.cfg, filepath.Dir(project)); e == nil {
		t.Fatal("root containing state accepted")
	}
	s, e := workspacesFor(a).create("local", project, "", "low")
	if e != nil {
		t.Fatal(e)
	}
	outside := filepath.Join(filepath.Dir(project), "secret")
	_ = os.WriteFile(outside, []byte("secret"), 0600)
	_ = os.Symlink(outside, filepath.Join(project, "escape"))
	for _, path := range []string{"../secret", "escape", "/etc/passwd"} {
		if _, e = s.files(context.Background(), path, true); e == nil {
			t.Fatalf("escape accepted %s", path)
		}
	}
	_ = os.WriteFile(filepath.Join(project, "ok.txt"), []byte("hello"), 0600)
	v, e := s.files(context.Background(), "ok.txt", true)
	if e != nil || v["content"] != "hello" {
		t.Fatal(v, e)
	}
}
func TestWorkspacePresetSecretsAndFlags(t *testing.T) {
	a, _ := workspaceTestApp(t)
	p := WorkspacePreset{Name: "test", Host: "example.test", User: "coder", Port: 22, Root: "/srv/project"}
	if e := validateWorkspacePreset(p); e != nil {
		t.Fatal(e)
	}
	p.Host = "-oProxyCommand=evil"
	if validateWorkspacePreset(p) == nil {
		t.Fatal("flag accepted")
	}
	w := workspaceRequest(t, a, "POST", "/v1/workspaces/presets", `{"name":"x","host":"localhost","port":22,"user":"coder","root":"/project","password":"secret","confirm":true}`)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	p.Host = "example.test"
	key := filepath.Join(t.TempDir(), "key")
	_ = os.WriteFile(key, []byte("private material"), 0644)
	p.KeyPath = key
	if validateWorkspacePreset(p) == nil {
		t.Fatal("insecure key accepted")
	}
	_ = os.Chmod(key, 0600)
	if e := validateWorkspacePreset(p); e != nil {
		t.Fatal(e)
	}
}
func TestWorkspaceSSHControlFailClosed(t *testing.T) {
	s := &PiWorkspaceSession{dir: "/private/session", preset: WorkspacePreset{Host: "example.test", Port: 22, User: "coder"}}
	args := strings.Join(s.sshArgs(), " ")
	for _, part := range []string{"StrictHostKeyChecking=yes", "BatchMode=yes", "ProxyCommand=/bin/false", "ControlMaster=no", "ForwardAgent=no"} {
		if !strings.Contains(args, part) {
			t.Fatal(args)
		}
	}
	if strings.Contains(args, "StrictHostKeyChecking=no") {
		t.Fatal(args)
	}
}
func TestWorkspaceAskpassSecretMemoryOnly(t *testing.T) {
	dir := t.TempDir()
	secret := []byte("single-use-password")
	b, e := startAskpass(filepath.Join(dir, "askpass.sock"), secret)
	if e != nil {
		t.Fatal(e)
	}
	c, e := net.Dial("unix", b.path)
	if e != nil {
		t.Fatal(e)
	}
	got, e := io.ReadAll(c)
	_ = c.Close()
	if e != nil || string(got) != "single-use-password\n" {
		t.Fatal(string(got), e)
	}
	b.close()
	if !bytes.Equal(secret, make([]byte, len(secret))) {
		t.Fatal("secret not wiped")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatal(entries)
	}
}
func TestWorkspaceAskpassActualHelper(t *testing.T) {
	dir := t.TempDir()
	b, e := startAskpass(filepath.Join(dir, "askpass.sock"), []byte("ephemeral-test"))
	if e != nil {
		t.Fatal(e)
	}
	defer b.close()
	cmd := exec.Command(productRoot + "/runtime/ssh-askpass")
	cmd.Env = []string{"STRIXGLM_ASKPASS_SOCKET=" + b.path}
	out, e := cmd.Output()
	if e != nil || string(out) != "ephemeral-test\n" {
		t.Fatalf("%q %v", out, e)
	}
}
func TestWorkspaceBridgeLeastPrivilege(t *testing.T) {
	a, project := workspaceTestApp(t)
	s, e := workspacesFor(a).create("local", project, "", "low")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.startBridge(); e != nil {
		t.Fatal(e)
	}
	if s.bridgeToken == a.token {
		t.Fatal("admin token reused")
	}
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", filepath.Join(s.bridgeDir, "api.sock"))
	}}}
	defer client.CloseIdleConnections()
	for _, test := range []struct {
		path, method string
		want         int
	}{{"/v1/models", "GET", 200}, {"/v1/coding/tasks", "POST", 403}, {"/v1/chat/completions", "POST", 409}, {"/v1/operations/jobs", "POST", 403}} {
		req, _ := http.NewRequest(test.method, s.bridgeURL+test.path, strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer "+s.bridgeToken)
		res, e := client.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		_ = res.Body.Close()
		if res.StatusCode != test.want {
			t.Fatalf("%s %d", test.path, res.StatusCode)
		}
	}
}
func TestWorkspaceBridgeSurvivesAdminRotationAndPublicPause(t *testing.T) {
	a, project := workspaceTestApp(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	var dispatched atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("private bridge forwarded a credential to the mock backend")
		}
		switch r.URL.Path {
		case "/tokenize":
			jsonReply(w, 200, map[string]any{"count": 1, "tokens": []int{1}, "max_model_len": 4096})
		case "/v1/chat/completions":
			if dispatched.Add(1) == 1 {
				close(entered)
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
			}
			jsonReply(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": "mock-completed"}, "finish_reason": "stop"}}})
		default:
			t.Errorf("unexpected mock backend path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer backend.Close()
	defer releaseOnce.Do(func() { close(release) })
	a.cfg.Backend, a.cfg.TokenizerEndpoint = backend.URL, backend.URL+"/tokenize"
	a.cfg.PairLock = filepath.Join(a.cfg.StateDir, "mock-pair.lock")
	a.cfg.ChatContextTokens = 4096
	a.cfg.Profiles["low"] = Profile{Reasoning: "low", ContextTokens: 4096, MaxTokens: 128}
	a.cfg.ToolCalls = true
	public := a.routes()
	s, e := workspacesFor(a).create("local", project, "", "low")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.startBridge(); e != nil {
		t.Fatal(e)
	}
	s.setState("RUNNING", "mock active Pi turn; no Pi process or inference")
	defer s.setState("READY", "")
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", filepath.Join(s.bridgeDir, "api.sock"))
	}}}
	defer client.CloseIdleConnections()
	type reply struct {
		code int
		body string
		err  error
	}
	request := func(method, target, token string) reply {
		r, e := http.NewRequest(method, s.bridgeURL+target, strings.NewReader(`{"messages":[{"role":"user","content":"fixture"}],"max_tokens":8}`))
		if e != nil {
			return reply{err: e}
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+token)
		response, e := client.Do(r)
		if e != nil {
			return reply{err: e}
		}
		defer response.Body.Close()
		body, e := io.ReadAll(response.Body)
		return reply{response.StatusCode, string(body), e}
	}
	pending := make(chan reply, 1)
	go func() { pending <- request("POST", "/v1/chat/completions", s.bridgeToken) }()
	select {
	case <-entered:
	case response := <-pending:
		t.Fatalf("bridge rejected valid mock turn before dispatch: HTTP%d error=%v body=%s", response.code, response.err, response.body)
	case <-time.After(5 * time.Second):
		t.Fatal("active bridge turn never reached mock backend")
	}
	old := a.currentToken()
	if w := settingsRequest(public, "POST", "/v1/settings/token", old, `{"confirm":true}`); w.Code != 200 {
		t.Fatalf("admin rotation failed: HTTP%d", w.Code)
	}
	next := a.currentToken()
	if w := settingsRequest(public, "PUT", "/v1/settings", next, disabledAPIs); w.Code != 200 {
		t.Fatalf("public API pause failed: HTTP%d", w.Code)
	}
	if settingsRequest(public, "POST", "/v1/chat/completions", old, `{}`).Code != 401 || settingsRequest(public, "POST", "/v1/chat/completions", next, `{}`).Code != 403 {
		t.Fatal("public endpoint ignored token revocation or admission pause")
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case response := <-pending:
		if response.err != nil || response.code != 200 || !strings.Contains(response.body, "mock-completed") {
			t.Fatalf("rotation interrupted admitted Pi turn: HTTP%d error=%v", response.code, response.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("admitted Pi turn failed to finish")
	}
	// A further model turn in the same already-active Pi prompt uses its scoped
	// capability, even though public admission and the former admin key changed.
	if response := request("POST", "/v1/chat/completions", s.bridgeToken); response.err != nil || response.code != 200 {
		t.Fatalf("active prompt lost its independent capability: HTTP%d error=%v", response.code, response.err)
	}
	for _, token := range []string{"wrong-capability", old, next} {
		if response := request("POST", "/v1/chat/completions", token); response.err != nil || response.code != 401 {
			t.Fatal("bridge accepted something other than its scoped capability")
		}
	}
	for _, route := range []struct{ method, target string }{{"GET", "/v1/settings"}, {"PUT", "/v1/settings"}, {"POST", "/v1/settings/token"}, {"POST", "/v1/operations/jobs"}} {
		if response := request(route.method, route.target, s.bridgeToken); response.err != nil || response.code != 403 {
			t.Fatalf("bridge gained admin access: %s HTTP%d", route.target, response.code)
		}
	}
	s.setState("READY", "")
	if response := request("POST", "/v1/chat/completions", s.bridgeToken); response.err != nil || response.code != 409 {
		t.Fatal("inactive prompt retained generation authority")
	}
	if dispatched.Load() != 2 {
		t.Fatalf("unauthorized or inactive request reached backend: %d dispatches", dispatched.Load())
	}
}

func TestWorkspaceModelsCompatibility(t *testing.T) {
	a, _ := workspaceTestApp(t)
	b, _ := json.Marshal(piModels(a.cfg, "http://127.0.0.1:1234"))
	for _, part := range []string{`"maxTokensField":"max_tokens"`, `"supportsDeveloperRole":false`, `"supportsStore":false`, `"medium":null`, `"max":"max"`} {
		if !bytes.Contains(b, []byte(part)) {
			t.Fatalf("missing %s: %s", part, b)
		}
	}
	if bytes.Contains(b, []byte(a.token)) {
		t.Fatal("secret persisted")
	}
}
func TestWorkspaceSessionPersistenceNeverAutostarts(t *testing.T) {
	a, project := workspaceTestApp(t)
	s, e := workspacesFor(a).create("local", project, "", "low")
	if e != nil {
		t.Fatal(e)
	}
	workspaceManagers.Delete(a)
	loaded := workspacesFor(a).sessions[s.ID]
	if loaded == nil || loaded.State != "CLOSED" || loaded.cmd != nil {
		t.Fatalf("unexpected restore %#v", loaded)
	}
}
func TestWorkspacePiRPCNoInference(t *testing.T) {
	if _, e := os.Stat(piRuntime + "/node_modules/.bin/pi"); e != nil {
		t.Skip("pinned Pi not installed")
	}
	a, project := workspaceTestApp(t)
	s, e := workspacesFor(a).create("local", project, "", "low")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.start(); e != nil {
		s.mu.Lock()
		events := append([]WorkspaceEvent(nil), s.events...)
		s.mu.Unlock()
		tail := ""
		for _, ev := range events {
			if len(ev.Event) < 1000 {
				tail += string(ev.Event) + "\n"
			}
		}
		t.Fatalf("real Pi startup: %v; events=%s", e, tail)
	}
	snap := s.snapshot()
	if snap["state"] != "READY" {
		t.Fatal(snap)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	v, e := s.rpc(ctx, map[string]any{"type": "get_state"})
	if e != nil {
		t.Fatal(e)
	}
	data, _ := v["data"].(map[string]any)
	if data["isStreaming"] != false || data["messageCount"] != float64(0) {
		t.Fatal(v)
	}
	if s.ScopeInvocation == "" {
		t.Fatal("effective resource scope not verified")
	}
	// A real Pi bash command is local work, not an inference request. Its output
	// proves both network isolation and the sole permitted Unix bridge.
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("sandbox reached host TCP listener") }))
	defer host.Close()
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(host.URL, "http://"))
	command := `node -e 'const n=require("node:net");let s=n.connect(` + port + `,"127.0.0.1");s.on("connect",()=>{console.log("UNSAFE");s.destroy()});s.on("error",()=>console.log("HOST_TCP_BLOCKED"));'`
	result, e := s.shell(context.Background(), command)
	if e != nil || !strings.Contains(fmt.Sprint(result["output"]), "HOST_TCP_BLOCKED") {
		t.Fatal(result, e)
	}
	result, e = s.shell(context.Background(), `node -e 'const f=require("node:fs"),h=require("node:http");let m=JSON.parse(f.readFileSync("/pi-config/models.json","utf8"));h.get(m.providers.strixglm.baseUrl+"/models",{headers:{Authorization:"Bearer "+process.env.STRIXGLM_PI_API_TOKEN}},r=>r.pipe(process.stdout));'`)
	if e != nil || !strings.Contains(fmt.Sprint(result["output"]), "test-model") {
		t.Fatal(result, e)
	}
	for _, arg := range s.cmd.Args {
		if strings.Contains(arg, a.token) {
			t.Fatal("admin token in argv")
		}
	}
	for _, env := range s.cmd.Env {
		if strings.Contains(env, a.token) {
			t.Fatal("admin token in env")
		}
	}
	if e = s.close(ctx); e != nil {
		t.Fatal(e)
	}
}

func TestWorkspaceOwnedScopeForeignInvocation(t *testing.T) {
	props := map[string]string{"Id": "strixglm-pi-owned.scope", "LoadState": "loaded", "ActiveState": "active", "InvocationID": "foreign"}
	if stop, e := ownedScopeMayStop("strixglm-pi-owned.scope", "our-invocation", props); e == nil || stop {
		t.Fatal("foreign scope accepted")
	}
	props["InvocationID"] = "our-invocation"
	if stop, e := ownedScopeMayStop("strixglm-pi-owned.scope", "our-invocation", props); e != nil || !stop {
		t.Fatal(stop, e)
	}
	props["ActiveState"] = "inactive"
	if stop, e := ownedScopeMayStop("strixglm-pi-owned.scope", "old-invocation", props); e != nil || stop {
		t.Fatal(stop, e)
	}
}
func TestWorkspaceCloseStopsDetachedChild(t *testing.T) {
	if _, e := os.Stat(piRuntime + "/node_modules/.bin/pi"); e != nil {
		t.Skip("pinned Pi not installed")
	}
	a, project := workspaceTestApp(t)
	s, e := workspacesFor(a).create("local", project, "", "low")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.start(); e != nil {
		t.Fatal(e)
	}
	result, e := s.shell(context.Background(), "setsid /usr/bin/sleep 30 </dev/null >/dev/null 2>&1 & disown; printf DETACHED_STARTED")
	if e != nil || !strings.Contains(fmt.Sprint(result["output"]), "DETACHED_STARTED") {
		t.Fatal(result, e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if e = s.close(ctx); e != nil {
		t.Fatal(e)
	}
	props, e := s.scopeProperties(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if props["LoadState"] != "not-found" && props["ActiveState"] != "inactive" {
		t.Fatal(props)
	}
}

func TestWorkspaceRemoteExtensionNoConnection(t *testing.T) {
	if _, e := os.Stat(piRuntime + "/node_modules/.bin/pi"); e != nil {
		t.Skip("pinned Pi not installed")
	}
	a, _ := workspaceTestApp(t)
	m := workspacesFor(a)
	p := WorkspacePreset{ID: id(), Name: "offline transport test", Host: "does-not-exist.invalid", Port: 22, User: "coder", Root: "/srv/project"}
	m.presets[p.ID] = p
	s, e := m.create("ssh", "", p.ID, "low")
	if e != nil {
		t.Fatal(e)
	}
	// Deliberately no SSH connection: this verifies extension loading and that
	// a missing ControlSocket cannot fall through to a local bash execution.
	if e = os.MkdirAll(filepath.Join(s.dir, "ssh"), 0700); e != nil {
		t.Fatal(e)
	}
	s.setState("CONNECTED", "")
	if e = s.start(); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	v, e := s.rpc(ctx, map[string]any{"type": "bash", "command": "printf LOCAL_FALLBACK_UNSAFE"})
	if e != nil {
		t.Fatal(e)
	}
	data, _ := v["data"].(map[string]any)
	out := fmt.Sprint(data["output"])
	if strings.Contains(out, "LOCAL_FALLBACK_UNSAFE") || data["exitCode"] == float64(0) {
		t.Fatalf("remote extension did not intercept bash: %v", v)
	}
	if !strings.Contains(out, "Connection closed") && !strings.Contains(out, "Control socket") && !strings.Contains(out, "connect") {
		t.Fatalf("missing SSH failure evidence: %v", v)
	}
}

func TestWorkspaceEventCapsAndSecrets(t *testing.T) {
	a, project := workspaceTestApp(t)
	s, e := workspacesFor(a).create("local", project, "", "low")
	if e != nil {
		t.Fatal(e)
	}
	s.bridgeToken = "session-secret-token"
	s.emit([]byte(a.token + " " + s.bridgeToken))
	s.mu.Lock()
	raw := string(s.events[0].Event)
	s.logBytes = 16 << 20
	s.mu.Unlock()
	if strings.Contains(raw, a.token) || strings.Contains(raw, s.bridgeToken) {
		t.Fatal("credential leaked")
	}
	s.emit([]byte(`{"type":"test"}`))
	s.mu.Lock()
	capped := s.logCapped
	s.mu.Unlock()
	if !capped {
		t.Fatal("raw log not capped")
	}
	b, e := os.ReadFile(filepath.Join(s.dir, "rpc-events.jsonl"))
	if e != nil || !bytes.Contains(b, []byte("log_truncated")) {
		t.Fatal(string(b), e)
	}
}
