package app

// Workspace sessions wrap the pinned upstream Pi RPC process. They do not
// implement an agent loop and never own inference-rank lifecycle.
import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

const piVersion = "0.85.1"
const productRoot = "/home/funboy/StrixHaloClusterGLM"
const piRuntime = productRoot + "/.tools/pi"

type WorkspacePreset struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	User    string `json:"user"`
	KeyPath string `json:"key_path,omitempty"`
	Root    string `json:"root"`
}
type WorkspaceEvent struct {
	Seq   int64           `json:"seq"`
	Time  string          `json:"time"`
	Event json.RawMessage `json:"event"`
}
type PiWorkspaceSession struct {
	Mode                string                 `json:"mode,omitempty"`
	OriginalRoot        string                 `json:"original_root,omitempty"`
	WorkingRoot         string                 `json:"working_root,omitempty"`
	BuildCommand        Command                `json:"build_command,omitempty"`
	TestCommand         Command                `json:"test_command,omitempty"`
	Verification        *WorkspaceVerification `json:"verification,omitempty"`
	protectedOriginal   protectedTree
	verificationRunning bool
	lastCompletion      string
	ConversationID      string `json:"conversation_id,omitempty"`
	ClosedCleanly       bool   `json:"closed_cleanly"`
	HandoffRevision     int64  `json:"handoff_revision,omitempty"`
	PendingTranscript   string `json:"-"`
	deleted             bool
	ID                  string `json:"id"`
	Kind                string `json:"kind"`
	Root                string `json:"root"`
	State               string `json:"state"`
	Reasoning           string `json:"reasoning_effort"`
	PresetID            string `json:"preset_id,omitempty"`
	Created             string `json:"created"`
	BlockedReason       string `json:"blocked_reason,omitempty"`
	ScopeUnit           string `json:"scope_unit,omitempty"`
	ScopeInvocation     string `json:"scope_invocation,omitempty"`
	mu                  sync.Mutex
	writeMu             sync.Mutex
	opMu                sync.Mutex
	manager             *workspaceManager
	dir                 string
	cmd                 *exec.Cmd
	stdin               io.WriteCloser
	done                chan struct{}
	pending             map[string]chan map[string]any
	events              []WorkspaceEvent
	seq                 int64
	eventBytes          int
	logBytes            int
	logCapped           bool
	ssh                 *exec.Cmd
	sshDone             chan struct{}
	preset              WorkspacePreset
	bridgeServer        *http.Server
	bridgeToken         string
	bridgeURL           string
	bridgeDir           string
}
type workspaceManager struct {
	activePi       string
	verificationMu sync.Mutex
	app            *App
	mu             sync.Mutex
	sessions       map[string]*PiWorkspaceSession
	presets        map[string]WorkspacePreset
	ready          bool
	reason         string
	closing        bool
	initErr        error
}

var workspaceManagers sync.Map

func workspacesFor(a *App) *workspaceManager {
	if v, ok := workspaceManagers.Load(a); ok {
		return v.(*workspaceManager)
	}
	m := &workspaceManager{app: a, sessions: map[string]*PiWorkspaceSession{}, presets: map[string]WorkspacePreset{}, reason: "GLM function-tool protocol not yet qualified; no model request allowed"}
	m.mu.Lock()
	actual, loaded := workspaceManagers.LoadOrStore(a, m)
	if loaded {
		m.mu.Unlock()
		return actual.(*workspaceManager)
	}
	defer m.mu.Unlock()
	dir := filepath.Join(a.cfg.StateDir, "workspaces")
	if e := os.MkdirAll(dir, 0700); e != nil {
		m.initErr = e
		return m
	}
	if b, e := os.ReadFile(filepath.Join(dir, "presets.json")); e == nil {
		var entries []WorkspacePreset
		if e = json.Unmarshal(b, &entries); e != nil {
			m.initErr = e
			return m
		}
		for _, p := range entries {
			if e = validateWorkspacePreset(p); e != nil {
				m.initErr = e
				return m
			}
			m.presets[p.ID] = p
		}
	}
	// Persisted sessions are records, never implicitly reconnected or restarted.
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if !entry.IsDir() || !workspaceID(entry.Name()) {
			continue
		}
		b, e := os.ReadFile(filepath.Join(dir, entry.Name(), "session.json"))
		if e != nil {
			continue
		}
		s := &PiWorkspaceSession{}
		if json.Unmarshal(b, s) != nil || s.ID != entry.Name() {
			continue
		}
		s.manager = m
		s.dir = filepath.Join(dir, s.ID)
		s.loadProtectionOriginal()
		s.pending = map[string]chan map[string]any{}
		s.loadRecordedEvents()
		if s.State != "CLOSED" {
			s.State = "CLOSED"
			s.BlockedReason = "Gateway restarted: explicit new session required; no process adopted"
			_ = s.persistLocked()
		}
		m.sessions[s.ID] = s
	}
	return m
}
func (a *App) setWorkspaceToolCapability(ready bool, reason string) {
	m := workspacesFor(a)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ready = ready
	m.reason = reason
}
func workspaceID(s string) bool { return len(s) == 32 && strings.Trim(s, "0123456789abcdef") == "" }
func (s *PiWorkspaceSession) persistLocked() error {
	if s.deleted {
		return os.ErrNotExist
	}
	return writeJSON(filepath.Join(s.dir, "session.json"), s)
}
func (s *PiWorkspaceSession) snapshot() map[string]any {
	s.manager.mu.Lock()
	ready := s.manager.ready
	s.manager.mu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	b, _ := json.Marshal(s)
	out := map[string]any{}
	_ = json.Unmarshal(b, &out)
	connected := s.State == "CONNECTED" || s.State == "READY" || s.State == "RUNNING" || s.State == "ABORTING"
	out["capabilities"] = map[string]bool{"files": connected, "terminal": connected, "pi": connected, "prompt": ready && s.State == "READY", "shell": s.State == "READY", "review": s.Mode != "" && s.Kind == "local" && (s.State == "READY" || s.State == "CONNECTED" || s.State == "CLOSED"), "verify": s.Mode != "" && s.Kind == "local" && (s.State == "READY" || s.State == "CONNECTED"), "apply": s.Mode == "protected" && (s.State == "READY" || s.State == "CONNECTED") && s.Verification != nil && s.Verification.Status == "TEST_PASS" && !s.Verification.Applied}
	out["next_seq"] = s.seq
	return out
}
func (s *PiWorkspaceSession) setState(state, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.State = state
	s.BlockedReason = reason
	_ = s.persistLocked()
}
func (s *PiWorkspaceSession) emit(raw []byte) {
	// Never persist the gateway bearer credential, including a tool's accidental env dump.
	raw = s.manager.app.redactAPITokens(raw)
	if s.bridgeToken != "" {
		raw = bytes.ReplaceAll(raw, []byte(s.bridgeToken), []byte("[SESSION-CAPABILITY-REDACTED]"))
	}
	if !json.Valid(raw) {
		raw, _ = json.Marshal(map[string]any{"type": "stderr", "text": string(raw)})
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deleted {
		return
	}
	s.seq++
	ev := WorkspaceEvent{s.seq, time.Now().UTC().Format(time.RFC3339Nano), append(json.RawMessage(nil), raw...)}
	s.events = append(s.events, ev)
	s.eventBytes += len(raw)
	for len(s.events) > 1000 || s.eventBytes > 8<<20 {
		s.eventBytes -= len(s.events[0].Event)
		s.events = s.events[1:]
	}
	if s.logCapped {
		return
	}
	b, _ := json.Marshal(ev)
	if s.logBytes+len(b) > 16<<20 {
		s.logCapped = true
		b, _ = json.Marshal(map[string]any{"type": "log_truncated", "reason": "16 MiB per-session RPC raw cap reached; bounded live event tail remains available", "seq": s.seq})
	}
	f, e := os.OpenFile(filepath.Join(s.dir, "rpc-events.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY|syscall.O_NOFOLLOW, 0600)
	if e == nil {
		_, _ = f.Write(append(b, '\n'))
		s.logBytes += len(b) + 1
		_ = f.Close()
	}
}
func workspaceBody(w http.ResponseWriter, r *http.Request, v any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return errors.New("Content-Type application/json required")
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return errors.New("one JSON object required")
	}
	return nil
}
func (a *App) registerWorkspaceRoutes(mux *http.ServeMux) {
	m := workspacesFor(a)
	a.registerProtectionRoutes(mux)
	mux.HandleFunc("GET /v1/workspaces/options", func(w http.ResponseWriter, r *http.Request) {
		if !a.authorized(w, r) {
			return
		}
		jsonReply(w, 200, m.options())
	})
	mux.HandleFunc("GET /v1/workspaces/presets", func(w http.ResponseWriter, r *http.Request) {
		if !a.authorized(w, r) {
			return
		}
		jsonReply(w, 200, map[string]any{"presets": m.presetList()})
	})
	mux.HandleFunc("POST /v1/workspaces/presets", func(w http.ResponseWriter, r *http.Request) {
		if !a.authorized(w, r) {
			return
		}
		var v struct {
			WorkspacePreset
			Confirm bool `json:"confirm"`
		}
		if e := workspaceBody(w, r, &v); e != nil || !v.Confirm {
			jsonReply(w, 400, map[string]string{"error": "valid preset and confirm:true required; passwords are not persisted"})
			return
		}
		v.ID = id()
		if v.Port == 0 {
			v.Port = 22
		}
		if e := validateWorkspacePreset(v.WorkspacePreset); e != nil {
			jsonReply(w, 400, map[string]string{"error": e.Error()})
			return
		}
		m.mu.Lock()
		m.presets[v.ID] = v.WorkspacePreset
		e := m.savePresetsLocked()
		if e != nil {
			delete(m.presets, v.ID)
		}
		m.mu.Unlock()
		if e != nil {
			jsonReply(w, 500, map[string]string{"error": e.Error()})
			return
		}
		jsonReply(w, 201, v.WorkspacePreset)
	})
	mux.HandleFunc("GET /v1/workspaces/sessions", func(w http.ResponseWriter, r *http.Request) {
		if !a.authorized(w, r) {
			return
		}
		m.mu.Lock()
		sessions := make([]*PiWorkspaceSession, 0, len(m.sessions))
		for _, s := range m.sessions {
			sessions = append(sessions, s)
		}
		m.mu.Unlock()
		out := []map[string]any{}
		for _, s := range sessions {
			out = append(out, s.snapshot())
		}
		sort.Slice(out, func(i, j int) bool { return out[i]["created"].(string) < out[j]["created"].(string) })
		jsonReply(w, 200, map[string]any{"sessions": out})
	})
	mux.HandleFunc("POST /v1/workspaces/sessions", func(w http.ResponseWriter, r *http.Request) {
		if !a.authorized(w, r) {
			return
		}
		var v struct {
			ConversationID string `json:"conversation_id"`
			WorkspaceProtectionOptions
			Kind      string `json:"kind"`
			Root      string `json:"root"`
			PresetID  string `json:"preset_id"`
			Password  string `json:"password"`
			Reasoning string `json:"reasoning_effort"`
			Confirm   bool   `json:"confirm"`
		}
		if e := workspaceBody(w, r, &v); e != nil || !v.Confirm {
			jsonReply(w, 400, map[string]string{"error": "session and confirm:true required"})
			return
		}
		if v.Password != "" {
			jsonReply(w, 400, map[string]string{"error": "password belongs only to explicit connect; it is never saved with session metadata"})
			return
		}
		s, e := m.createConfigured(v.Kind, v.Root, v.PresetID, v.Reasoning, v.ConversationID, v.WorkspaceProtectionOptions)
		if e != nil {
			jsonReply(w, 400, map[string]string{"error": e.Error()})
			return
		}
		jsonReply(w, 201, s.snapshot())
	})
	mux.HandleFunc("GET /v1/workspaces/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		s := m.requestSession(w, r)
		if s != nil {
			jsonReply(w, 200, s.snapshot())
		}
	})
	mux.HandleFunc("GET /v1/workspaces/sessions/{id}/events", func(w http.ResponseWriter, r *http.Request) {
		s := m.requestSession(w, r)
		if s == nil {
			return
		}
		after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		s.mu.Lock()
		out := []WorkspaceEvent{}
		for _, e := range s.events {
			if e.Seq > after {
				out = append(out, e)
			}
		}
		state, next := s.State, s.seq
		oldest := int64(0)
		if len(s.events) > 0 {
			oldest = s.events[0].Seq
		}
		s.mu.Unlock()
		jsonReply(w, 200, map[string]any{"events": out, "next_seq": next, "state": state, "truncated": after > 0 && after < oldest-1})
	})
	mux.HandleFunc("POST /v1/workspaces/sessions/{id}/{action}", func(w http.ResponseWriter, r *http.Request) {
		s := m.requestSession(w, r)
		if s == nil {
			return
		}
		var v struct {
			Confirm          bool   `json:"confirm"`
			AllowTestChanges bool   `json:"allow_test_changes"`
			Password         string `json:"password"`
			Message          string `json:"message"`
			CommandID        string `json:"command_id"`
			Command          string `json:"command"`
		}
		if e := workspaceBody(w, r, &v); e != nil || !v.Confirm {
			jsonReply(w, 400, map[string]string{"error": "valid action and confirm:true required"})
			return
		}
		action := r.PathValue("action")
		if v.Password != "" && action != "connect" {
			jsonReply(w, 400, map[string]string{"error": "password accepted only by connect"})
			return
		}
		var e error
		switch action {
		case "verify":
			e = s.beginVerification("manual")
		case "apply":
			e = s.applyProtection(v.AllowTestChanges)
		case "connect":
			e = s.connect(r.Context(), v.Password)
		case "start":
			e = s.start()
		case "prompt":
			e = s.prompt(v.Message)
		case "abort":
			e = s.abort(r.Context())
		case "close":
			e = s.close(r.Context())
		case "terminal":
			var out map[string]any
			if v.Command != "" {
				if v.CommandID != "" {
					e = errors.New("choose command or command_id, not both")
				} else {
					out, e = s.shell(r.Context(), v.Command)
				}
			} else {
				out, e = s.terminal(r.Context(), v.CommandID)
			}
			if e == nil {
				jsonReply(w, 200, out)
				return
			}
		default:
			jsonReply(w, 404, map[string]string{"error": "unknown workspace action"})
			return
		}
		if e != nil {
			jsonReply(w, 409, map[string]string{"error": e.Error()})
			return
		}
		jsonReply(w, 200, s.snapshot())
	})
	for _, kind := range []string{"files", "file"} {
		kind := kind
		mux.HandleFunc("GET /v1/workspaces/sessions/{id}/"+kind, func(w http.ResponseWriter, r *http.Request) {
			s := m.requestSession(w, r)
			if s == nil {
				return
			}
			v, e := s.files(r.Context(), r.URL.Query().Get("path"), kind == "file")
			if e != nil {
				jsonReply(w, 400, map[string]string{"error": e.Error()})
				return
			}
			jsonReply(w, 200, v)
		})
	}
}
func (m *workspaceManager) requestSession(w http.ResponseWriter, r *http.Request) *PiWorkspaceSession {
	if !m.app.authorized(w, r) {
		return nil
	}
	m.mu.Lock()
	s := m.sessions[r.PathValue("id")]
	m.mu.Unlock()
	if s == nil {
		jsonReply(w, 404, map[string]string{"error": "unknown session"})
	}
	return s
}
func (m *workspaceManager) options() map[string]any {
	_, e := os.Stat(piRuntime + "/node_modules/.bin/pi")
	m.mu.Lock()
	ready, reason := m.ready, m.reason
	initErr := m.initErr
	m.mu.Unlock()
	if initErr != nil {
		reason = initErr.Error()
		ready = false
	}
	return map[string]any{"local_roots": m.app.cfg.WorkspaceRoots, "pi": map[string]any{"installed": e == nil, "version": piVersion, "tools_available": ready, "blocked_reason": reason, "protocol": "rpc", "sandbox": "bwrap filesystem/PID/network isolation; Unix-socket generation capability only; explicit workspace writes"}, "auth_methods": []map[string]any{{"id": "agent", "available": os.Getenv("SSH_AUTH_SOCK") != ""}, {"id": "key", "available": true}, {"id": "password", "available": true}}, "terminal": map[string]any{"commands": []map[string]string{{"id": "pwd", "label": "Working directory"}, {"id": "git-status", "label": "Git status (read only)"}, {"id": "list", "label": "List directory"}, {"id": "disk", "label": "Disk space"}}}}
}
func (m *workspaceManager) presetList() []WorkspacePreset {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []WorkspacePreset{}
	for _, p := range m.presets {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func (m *workspaceManager) savePresetsLocked() error {
	out := []WorkspacePreset{}
	for _, p := range m.presets {
		out = append(out, p)
	}
	return writeJSON(filepath.Join(m.app.cfg.StateDir, "workspaces", "presets.json"), out)
}
func validateLocalWorkspace(c Config, root string) (string, error) {
	p, e := realPath(root)
	if e != nil {
		return "", e
	}
	info, e := os.Stat(p)
	if e != nil || !info.IsDir() {
		return "", errors.New("workspace must be an existing directory")
	}
	allowed := false
	for _, r := range c.WorkspaceRoots {
		if within(p, r) {
			allowed = true
		}
	}
	if !allowed {
		return "", errors.New("workspace outside configured roots")
	}
	protected := []string{c.StateDir, productRoot + "/.tools", productRoot + "/runtime", "/home/funboy/ai-exp/strix-ciru-tp2", "/home/funboy/models", "/home/funboy/ai-exp/models"}
	for _, r := range protected {
		if r != "" && (within(p, r) || within(r, p)) {
			return "", errors.New("choose a project subdirectory that does not contain operational runtime, state or weights")
		}
	}
	return p, nil
}
func (m *workspaceManager) create(kind, root, presetID, reason string, conversationIDs ...string) (*PiWorkspaceSession, error) {
	if reason == "" {
		reason = "low"
	}
	if reason != "low" && reason != "high" && reason != "max" {
		return nil, errors.New("reasoning_effort must be low, high or max")
	}
	cs := conversationsFor(m.app)
	cs.mu.Lock()
	defer cs.mu.Unlock()
	var conversation Conversation
	var ce error
	if len(conversationIDs) > 0 && conversationIDs[0] != "" {
		conversation, ce = cs.readLocked(conversationIDs[0])
	} else {
		conversation, ce = cs.createLocked("Coding conversation")
	}
	if ce != nil {
		return nil, ce
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.initErr != nil {
		return nil, m.initErr
	}
	if m.closing {
		return nil, errors.New("gateway shutting down")
	}
	s := &PiWorkspaceSession{ID: id(), Kind: kind, Root: root, State: "CREATED", Reasoning: reason, PresetID: presetID, Created: time.Now().UTC().Format(time.RFC3339Nano), manager: m, pending: map[string]chan map[string]any{}}
	s.ConversationID = conversation.ID
	switch kind {
	case "local":
		p, e := validateLocalWorkspace(m.app.cfg, root)
		if e != nil {
			return nil, e
		}
		s.Root = p
		s.State = "CONNECTED"
	case "ssh":
		p, ok := m.presets[presetID]
		if !ok {
			return nil, errors.New("saved SSH preset required")
		}
		if root != "" && root != p.Root {
			return nil, errors.New("root must match saved preset")
		}
		s.Root = p.Root
		s.preset = p
	default:
		return nil, errors.New("kind must be local or ssh")
	}
	s.dir = filepath.Join(m.app.cfg.StateDir, "workspaces", s.ID)
	if e := os.MkdirAll(s.dir, 0700); e != nil {
		return nil, e
	}
	if len(conversationIDs) > 1 && conversationIDs[1] == "protection-preparing" {
		s.State = "PREPARING"
	}
	if e := s.persistLocked(); e != nil {
		return nil, e
	}
	if e := cs.linkLocked(conversation, s.ID); e != nil {
		return nil, e
	}
	m.sessions[s.ID] = s
	return s, nil
}
func (s *PiWorkspaceSession) rpc(ctx context.Context, command map[string]any) (map[string]any, error) {
	rid := id()
	command["id"] = rid
	ch := make(chan map[string]any, 1)
	s.mu.Lock()
	if s.stdin == nil {
		s.mu.Unlock()
		return nil, errors.New("Pi not running")
	}
	s.pending[rid] = ch
	stdin := s.stdin
	done := s.done
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.pending, rid); s.mu.Unlock() }()
	b, e := json.Marshal(command)
	if e != nil {
		return nil, e
	}
	s.writeMu.Lock()
	_, e = stdin.Write(append(b, '\n'))
	s.writeMu.Unlock()
	if e != nil {
		return nil, e
	}
	select {
	case v := <-ch:
		if v["success"] != true {
			return v, fmt.Errorf("Pi RPC %s refused: %v", command["type"], v["error"])
		}
		return v, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-done:
		return nil, errors.New("Pi process exited")
	}
}
func (s *PiWorkspaceSession) start() (ret error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	s.mu.Lock()
	state := s.State
	s.mu.Unlock()
	if state != "CONNECTED" {
		return errors.New("session must be connected and not already started")
	}
	if e := s.manager.reservePi(s.ID); e != nil {
		return e
	}
	launched := false
	defer func() {
		if ret != nil && !launched {
			s.manager.releasePi(s.ID)
		}
	}()
	cmd, e := s.piCommand()
	if e != nil {
		return e
	}
	stdin, e := cmd.StdinPipe()
	if e != nil {
		return e
	}
	stdout, e := cmd.StdoutPipe()
	if e != nil {
		return e
	}
	stderr, e := cmd.StderrPipe()
	if e != nil {
		return e
	}
	if e = cmd.Start(); e != nil {
		return e
	}
	launched = true
	s.mu.Lock()
	s.cmd = cmd
	s.stdin = stdin
	s.done = make(chan struct{})
	s.State = "STARTING"
	_ = s.persistLocked()
	s.mu.Unlock()
	go s.scanPi(stdout, true)
	go s.scanPi(stderr, false)
	go func() {
		err := cmd.Wait()
		s.mu.Lock()
		s.stdin = nil
		if s.State != "CLOSED" {
			s.State = "FAILED"
			s.BlockedReason = "Pi exited"
			if err != nil {
				s.BlockedReason = err.Error()
			}
		}
		_ = s.persistLocked()
		close(s.done)
		s.mu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for _, c := range []map[string]any{{"type": "get_state"}, {"type": "set_auto_retry", "enabled": false}, {"type": "set_auto_compaction", "enabled": false}} {
		if _, e = s.rpc(ctx, c); e != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
			return e
		}
	}
	if e = s.verifyScope(ctx); e != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		s.setState("FAILED", e.Error())
		return e
	}
	s.setState("READY", "")
	return nil
}
func (s *PiWorkspaceSession) scanPi(reader io.Reader, protocol bool) {
	scan := bufio.NewScanner(reader)
	scan.Buffer(make([]byte, 4096), 2<<20)
	for scan.Scan() {
		b := append([]byte(nil), scan.Bytes()...)
		s.emit(b)
		if !protocol {
			continue
		}
		v := map[string]any{}
		if json.Unmarshal(b, &v) != nil {
			continue
		}
		s.recordPiMessage(v)
		s.mu.Lock()
		switch v["type"] {
		case "response":
			rid, _ := v["id"].(string)
			if ch := s.pending[rid]; ch != nil {
				select {
				case ch <- v:
				default:
				}
			}
		case "agent_start":
			s.State = "RUNNING"
			_ = s.persistLocked()
		case "agent_end":
			if s.State != "CLOSED" {
				s.State = "READY"
				if s.Mode != "" {
					s.State = "VERIFYING"
				}
				_ = s.persistLocked()
			}
		}
		s.mu.Unlock()
		if v["type"] == "agent_end" {
			s.onProtectionAgentEnd(v)
		}
	}
	if e := scan.Err(); e != nil {
		s.emit([]byte("RPC stream failed: " + e.Error()))
	}
}
func (s *PiWorkspaceSession) prompt(message string) error {
	if strings.TrimSpace(message) == "" || len(message) > 48<<10 {
		return errors.New("prompt must contain 1..49152 UTF-8 bytes")
	}
	s.manager.mu.Lock()
	ready, reason, closing := s.manager.ready, s.manager.reason, s.manager.closing
	s.manager.mu.Unlock()
	if !ready {
		return errors.New(reason)
	}
	if closing {
		return errors.New("gateway shutting down")
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()
	s.mu.Lock()
	if s.State != "READY" {
		s.mu.Unlock()
		return errors.New("Pi is not idle/ready; queued prompts are disabled")
	}
	s.State = "RUNNING"
	if s.Mode != "" {
		s.lastCompletion = "RUNNING"
		s.Verification = &WorkspaceVerification{Status: "UNVERIFIED", Origin: "prompt", Completion: "RUNNING", Error: "New agent turn invalidates previous candidate verification"}
	}
	_ = s.persistLocked()
	s.mu.Unlock()
	actualMessage, pe := s.prepareConversationPrompt(message)
	if pe != nil {
		s.setState("READY", conversationPersistenceError(pe))
		return pe
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, e := s.rpc(ctx, map[string]any{"type": "prompt", "message": actualMessage})
	if e != nil {
		s.setState("FAILED", "prompt admission uncertain: "+e.Error())
	}
	return e
}
func (s *PiWorkspaceSession) abort(ctx context.Context) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	return s.abortLocked(ctx)
}
func (s *PiWorkspaceSession) abortLocked(ctx context.Context) error {
	s.mu.Lock()
	state := s.State
	if s.Mode != "" && (state == "RUNNING" || state == "ABORTING") {
		s.lastCompletion = "ABORTED"
		s.Verification = &WorkspaceVerification{Status: "INCOMPLETE", Origin: "abort", Completion: "ABORTED", Error: "Explicitly aborted agent turn is not a completed solution"}
		_ = s.persistLocked()
	}
	s.mu.Unlock()
	if state == "CONNECTED" || state == "CREATED" || state == "CLOSED" {
		return nil
	}
	if _, e := s.rpc(ctx, map[string]any{"type": "clear_queue"}); e != nil {
		return e
	}
	s.setState("ABORTING", "")
	_, e := s.rpc(ctx, map[string]any{"type": "abort"})
	if e == nil {
		s.setState("READY", "")
	}
	return e
}
func (s *PiWorkspaceSession) close(ctx context.Context) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	s.mu.Lock()
	active := s.State == "RUNNING" || s.State == "ABORTING"
	s.mu.Unlock()
	if active {
		if e := s.abortLocked(ctx); e != nil {
			return fmt.Errorf("generation must drain before process close: %w", e)
		}
	}
	if s.bridgeServer != nil {
		if e := s.bridgeServer.Shutdown(ctx); e != nil {
			return e
		}
	}
	// Drain first, then stop the entire owned cgroup, not only the launcher's
	// process group. A tool may have deliberately created a detached child.
	if e := s.stopOwnedScope(ctx); e != nil {
		return e
	}
	s.mu.Lock()
	cmd, done, ssh, sshDone := s.cmd, s.done, s.ssh, s.sshDone
	s.State = "CLOSED"
	_ = s.persistLocked()
	s.mu.Unlock()
	for _, p := range []struct {
		cmd  *exec.Cmd
		done chan struct{}
	}{{cmd, done}, {ssh, sshDone}} {
		if p.cmd == nil || p.cmd.Process == nil {
			continue
		}
		select {
		case <-p.done:
			continue
		default:
		}
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGTERM)
		select {
		case <-p.done:
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
			_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
			select {
			case <-p.done:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	s.mu.Lock()
	s.ClosedCleanly = true
	_ = s.persistLocked()
	s.mu.Unlock()
	s.manager.releasePi(s.ID)
	return nil
}
func (a *App) shutdownWorkspaces(ctx context.Context) error {
	v, ok := workspaceManagers.Load(a)
	if !ok {
		return nil
	}
	m := v.(*workspaceManager)
	m.mu.Lock()
	m.closing = true
	ss := []*PiWorkspaceSession{}
	for _, s := range m.sessions {
		ss = append(ss, s)
	}
	m.mu.Unlock()
	var errs []error
	for _, s := range ss {
		if e := s.close(ctx); e != nil {
			errs = append(errs, e)
		}
	}
	return errors.Join(errs...)
}
func workspaceRelative(p string) error {
	if p == "" || p == "." {
		return nil
	}
	return cleanRel(p)
}
func (s *PiWorkspaceSession) files(ctx context.Context, path string, file bool) (map[string]any, error) {
	if e := workspaceRelative(path); e != nil {
		return nil, e
	}
	if path == "" {
		path = "."
	}
	s.mu.Lock()
	state := s.State
	s.mu.Unlock()
	if state == "CREATED" || state == "CLOSED" || state == "FAILED" {
		return nil, errors.New("session not connected")
	}
	if s.Kind == "ssh" {
		return s.remoteFiles(ctx, path, file)
	}
	root, e := os.OpenRoot(s.Root)
	if e != nil {
		return nil, e
	}
	defer root.Close()
	f, e := root.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil {
		return nil, e
	}
	if file {
		if !info.Mode().IsRegular() {
			return nil, errors.New("regular text file required")
		}
		b, e := io.ReadAll(io.LimitReader(f, (256<<10)+1))
		if e != nil {
			return nil, e
		}
		trunc := len(b) > 256<<10
		if trunc {
			b = b[:256<<10]
		}
		if bytes.IndexByte(b, 0) >= 0 || !utf8.Valid(b) {
			return nil, errors.New("binary/non-UTF-8 file refused")
		}
		return map[string]any{"path": path, "content": string(b), "truncated": trunc}, nil
	}
	if !info.IsDir() {
		return nil, errors.New("directory required")
	}
	entries, e := f.ReadDir(1001)
	if e != nil && e != io.EOF {
		return nil, e
	}
	out := []map[string]any{}
	for _, en := range entries {
		if strings.HasPrefix(en.Name(), ".") {
			continue
		}
		i, e := en.Info()
		if e != nil {
			continue
		}
		kind := "file"
		if en.IsDir() {
			kind = "directory"
		}
		if en.Type()&os.ModeSymlink != 0 {
			kind = "symlink"
		}
		out = append(out, map[string]any{"name": en.Name(), "path": filepath.Join(path, en.Name()), "type": kind, "size": i.Size()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["name"].(string) < out[j]["name"].(string) })
	return map[string]any{"path": path, "entries": out, "truncated": len(entries) > 1000}, nil
}
