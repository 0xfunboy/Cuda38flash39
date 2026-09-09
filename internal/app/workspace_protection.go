package app

// Protected Pi sessions reuse the existing bounded source snapshots, isolated
// command runner and conflict-checked verified apply. Pi itself remains upstream.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

type WorkspaceProtectionOptions struct {
	Mode         string  `json:"mode"`
	AllowDirect  bool    `json:"allow_direct"`
	BuildCommand Command `json:"build_command"`
	TestCommand  Command `json:"test_command"`
}

type WorkspaceVerification struct {
	ID                 string            `json:"id"`
	Status             string            `json:"status"`
	Origin             string            `json:"origin"`
	Completion         string            `json:"completion"`
	Started            string            `json:"started"`
	Finished           string            `json:"finished,omitempty"`
	CandidateSHA256    string            `json:"candidate_sha256,omitempty"`
	FileSHA256         map[string]string `json:"file_sha256,omitempty"`
	Build              *CommandResult    `json:"build,omitempty"`
	Tests              *CommandResult    `json:"tests,omitempty"`
	ChangedTests       []string          `json:"changed_test_definitions,omitempty"`
	OriginalFileSHA256 map[string]string `json:"original_file_sha256,omitempty"`
	Excluded           []string          `json:"excluded,omitempty"`
	Error              string            `json:"error,omitempty"`
	Receipt            string            `json:"receipt,omitempty"`
	Applied            bool              `json:"applied"`
}

type protectedTree struct {
	Files    map[string][]byte
	Modes    map[string]fs.FileMode
	Info     fs.FileInfo
	Excluded []string
}

func protectionConfig(c Config) Config {
	// Never loosen configured limits. Defaults only support incomplete test/app
	// configurations; verified apply's existing per-file bound remains 256 KiB.
	if c.MaxFiles <= 0 {
		c.MaxFiles = 300
	}
	if c.MaxFileBytes <= 0 || c.MaxFileBytes > 262144 {
		c.MaxFileBytes = 262144
	}
	if c.MaxRepoBytes <= 0 {
		c.MaxRepoBytes = 32 << 20
	}
	if c.SandboxTimeout <= 0 || c.SandboxTimeout > 90 {
		c.SandboxTimeout = 90
	}
	if c.SandboxMemoryBytes <= 0 || c.SandboxMemoryBytes > 1<<30 {
		c.SandboxMemoryBytes = 1 << 30
	}
	if c.SandboxTasks <= 0 || c.SandboxTasks > 64 {
		c.SandboxTasks = 64
	}
	return c
}

func readProtectedTree(path string, cfg Config) (protectedTree, error) {
	c := protectionConfig(cfg)
	t := protectedTree{Files: map[string][]byte{}, Modes: map[string]fs.FileMode{}, Excluded: []string{}}
	rootInfo, e := os.Lstat(path)
	if e != nil || !rootInfo.IsDir() {
		return t, errors.New("snapshot root must be a real directory, not a symlink")
	}
	r, e := os.OpenRoot(path)
	if e != nil {
		return t, e
	}
	defer r.Close()
	t.Info, e = r.Stat(".")
	if e != nil {
		return t, e
	}
	if !os.SameFile(rootInfo, t.Info) {
		return t, errors.New("snapshot root changed during open")
	}
	count, total, visited := 0, 0, 0
	e = fs.WalkDir(r.FS(), ".", func(name string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if name == "." {
			return nil
		}
		visited++
		if visited > c.MaxFiles*20+1000 {
			return errors.New("snapshot traversal limit exceeded; choose a smaller project")
		}
		if !eligible(name) || protectionSensitive(name) {
			if len(t.Excluded) < 1000 {
				t.Excluded = append(t.Excluded, name)
			}
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if e := cleanRel(name); e != nil {
			return e
		}
		info, e := r.Lstat(name)
		if e != nil {
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("snapshot symlink refused: %s", name)
		}
		if info.IsDir() {
			return nil
		}
		if st, ok := info.Sys().(*syscall.Stat_t); ok && st.Nlink > 1 {
			return fmt.Errorf("snapshot hardlink refused: %s", name)
		}
		b, opened, e := readRootRegular(r, name, c.MaxFileBytes)
		if e != nil {
			return e
		}
		if !os.SameFile(info, opened) {
			return fmt.Errorf("source changed during snapshot: %s", name)
		}
		if !utf8.Valid(b) || bytes.IndexByte(b, 0) >= 0 {
			if len(t.Excluded) < 1000 {
				t.Excluded = append(t.Excluded, name+" (binary)")
			}
			return nil
		}
		count++
		total += len(b)
		if count > c.MaxFiles || total > c.MaxRepoBytes {
			return errors.New("snapshot source quota exceeded; choose a smaller project")
		}
		t.Files[name], t.Modes[name] = b, info.Mode().Perm()
		return nil
	})
	return t, e
}

func protectedTreeHash(t protectedTree) string {
	names := make([]string, 0, len(t.Files))
	for name := range t.Files {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		fmt.Fprintf(&b, "%s\x00%o\x00%s\n", name, t.Modes[name].Perm(), applyHash(t.Files[name]))
	}
	return applyHash([]byte(b.String()))
}

func sameProtectedTree(a, b protectedTree) bool { return protectedTreeHash(a) == protectedTreeHash(b) }

func protectionSensitive(name string) bool {
	base := strings.ToLower(filepath.Base(name))
	switch base {
	case "api-token", "hosts.yml", "credentials", "credentials.json", "passwords", "passwords.json", "config.local.json":
		return true
	}
	return strings.HasSuffix(base, ".pfx")
}

func validateProtectionOptions(kind string, p WorkspaceProtectionOptions) (WorkspaceProtectionOptions, error) {
	if p.Mode == "" {
		p.Mode = "protected"
	}
	if p.Mode != "protected" && p.Mode != "direct" {
		return p, errors.New("mode must be protected or direct")
	}
	if p.Mode == "direct" && !p.AllowDirect {
		return p, errors.New("direct mode requires allow_direct:true; Pi writes to the original workspace")
	}
	if kind == "ssh" && p.Mode == "protected" {
		return p, errors.New("BLOCKED_REMOTE_PROTECTION: protected SSH snapshots/apply are not implemented; choose direct explicitly or use local protected mode")
	}
	for _, cmd := range []Command{p.BuildCommand, p.TestCommand} {
		if len(cmd) > 128 {
			return p, errors.New("verification command has too many arguments")
		}
		total := 0
		for _, arg := range cmd {
			total += len(arg)
			if strings.IndexByte(arg, 0) >= 0 {
				return p, errors.New("NUL in verification command")
			}
		}
		if total > 8192 {
			return p, errors.New("verification command exceeds 8192 bytes")
		}
		if len(cmd) > 0 && strings.TrimSpace(cmd[0]) == "" {
			return p, errors.New("empty verification executable")
		}
	}
	return p, nil
}

func (m *workspaceManager) createConfigured(kind, root, presetID, reasoning, conversationID string, opts WorkspaceProtectionOptions) (*PiWorkspaceSession, error) {
	opts, e := validateProtectionOptions(kind, opts)
	if e != nil {
		return nil, e
	}
	// Snapshot before creating a public session record. No Git stash/reset/checkout
	// occurs, and dirty/untracked eligible source bytes are copied exactly.
	var original protectedTree
	if kind == "local" {
		root, e = validateLocalWorkspace(m.app.cfg, root)
		if e != nil {
			return nil, e
		}
		original, e = readProtectedTree(root, m.app.cfg)
		if e != nil {
			return nil, e
		}
	}
	s, e := m.create(kind, root, presetID, reasoning, conversationID, "protection-preparing")
	if e != nil {
		return nil, e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Mode, s.OriginalRoot, s.WorkingRoot = opts.Mode, s.Root, s.Root
	s.BuildCommand, s.TestCommand = append(Command(nil), opts.BuildCommand...), append(Command(nil), opts.TestCommand...)
	s.Verification = &WorkspaceVerification{Status: "UNVERIFIED", Completion: "NOT_RUN", Origin: "session_create", Excluded: original.Excluded}
	if kind == "local" {
		s.protectedOriginal = original
		if e = writeFilesWithModes(filepath.Join(s.dir, "original"), original.Files, original.Modes); e != nil {
			s.State = "FAILED"
			_ = s.persistLocked()
			return nil, e
		}
		if e = writeJSON(filepath.Join(s.dir, "original-modes.json"), original.Modes); e != nil {
			s.State = "FAILED"
			_ = s.persistLocked()
			return nil, e
		}
		if opts.Mode == "protected" {
			s.Root = filepath.Join(s.dir, "working")
			s.WorkingRoot = s.Root
			if e = os.MkdirAll(s.Root, 0700); e == nil {
				e = writeFilesWithModes(s.Root, original.Files, original.Modes)
			}
			if e != nil {
				s.State = "FAILED"
				_ = s.persistLocked()
				return nil, e
			}
		}
		again, readErr := readProtectedTree(s.OriginalRoot, m.app.cfg)
		if readErr != nil || !os.SameFile(original.Info, again.Info) || !sameProtectedTree(original, again) {
			s.State = "FAILED"
			_ = s.persistLocked()
			return nil, errors.New("original changed while snapshot was created; no source modified; create a fresh session")
		}
	}
	if kind == "local" {
		s.State = "CONNECTED"
	} else {
		s.State = "CREATED"
	}
	if e = s.persistLocked(); e != nil {
		return nil, e
	}
	return s, nil
}

func (s *PiWorkspaceSession) loadProtectionOriginal() {
	if s.Mode == "" || s.Kind != "local" {
		return
	}
	t, e := readProtectedTree(filepath.Join(s.dir, "original"), s.manager.app.cfg)
	if e != nil {
		return
	}
	var modes map[string]fs.FileMode
	if b, e := os.ReadFile(filepath.Join(s.dir, "original-modes.json")); e == nil && json.Unmarshal(b, &modes) == nil {
		t.Modes = modes
	}
	// Root identity from a previous process is not reconstructed/adopted. These
	// closed records remain reviewable, never applicable or automatically resumed.
	t.Info = nil
	s.protectedOriginal = t
}

func (s *PiWorkspaceSession) validateWorkingRoot() error {
	if s.Mode != "protected" {
		_, e := validateLocalWorkspace(s.manager.app.cfg, s.Root)
		return e
	}
	if s.Kind != "local" || s.Root != filepath.Join(s.dir, "working") || s.WorkingRoot != s.Root {
		return errors.New("protected working root identity mismatch")
	}
	if _, e := validateLocalWorkspace(s.manager.app.cfg, s.OriginalRoot); e != nil {
		return e
	}
	real, e := filepath.EvalSymlinks(s.Root)
	if e != nil || real != s.Root {
		return errors.New("protected working root must not contain symlink aliases")
	}
	return nil
}

func (m *workspaceManager) reservePi(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.activePi != "" && m.activePi != id {
		return errors.New("one active Pi scope allowed; close the existing session before starting another")
	}
	m.activePi = id
	return nil
}
func (m *workspaceManager) releasePi(id string) {
	m.mu.Lock()
	if m.activePi == id {
		m.activePi = ""
	}
	m.mu.Unlock()
}

func protectionCompletion(v map[string]any) string {
	msgs, _ := v["messages"].([]any)
	for i := len(msgs) - 1; i >= 0; i-- {
		msg, ok := msgs[i].(map[string]any)
		if !ok || msg["role"] != "assistant" {
			continue
		}
		if msg["stopReason"] == "stop" {
			return "NATURAL"
		}
		if reason, ok := msg["stopReason"].(string); ok {
			return strings.ToUpper(reason)
		}
		return "UNKNOWN"
	}
	return "UNKNOWN"
}

func (s *PiWorkspaceSession) onProtectionAgentEnd(v map[string]any) {
	if s.Mode == "" {
		return
	} // Historical records/internal fixtures are not retroactively qualified.
	completion := protectionCompletion(v)
	s.mu.Lock()
	if s.lastCompletion == "ABORTED" {
		completion = "ABORTED"
	}
	s.lastCompletion = completion
	s.mu.Unlock()
	if completion != "NATURAL" {
		s.mu.Lock()
		s.Verification = &WorkspaceVerification{Status: "INCOMPLETE", Origin: "agent_end", Completion: completion, Error: "Agent end is not a naturally completed, independently verified solution"}
		if s.State != "CLOSED" {
			s.State = "READY"
		}
		_ = s.persistLocked()
		s.mu.Unlock()
		return
	}
	if e := s.beginVerification("agent_end"); e != nil {
		s.mu.Lock()
		s.Verification = &WorkspaceVerification{Status: "UNVERIFIED", Origin: "agent_end", Completion: completion, Error: e.Error()}
		if s.State == "VERIFYING" {
			s.State = "READY"
		}
		_ = s.persistLocked()
		s.mu.Unlock()
	}
}

func (s *PiWorkspaceSession) beginVerification(origin string) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Mode == "" {
		return errors.New("create a new protected or explicitly direct session to configure independent checks")
	}
	if s.State != "READY" && s.State != "CONNECTED" && !(origin == "agent_end" && s.State == "VERIFYING") {
		return errors.New("verification requires an idle session")
	}
	if s.verificationRunning {
		return errors.New("verification already running")
	}
	if s.Kind != "local" {
		s.Verification = &WorkspaceVerification{Status: "UNVERIFIED", Origin: origin, Completion: s.lastCompletion, Error: "BLOCKED_REMOTE_VERIFICATION: no isolated fresh remote snapshot runner; agent claims are not verification"}
		if s.State == "VERIFYING" {
			s.State = "READY"
		}
		return s.persistLocked()
	}
	s.verificationRunning = true
	s.State = "VERIFYING"
	v := &WorkspaceVerification{ID: id(), Status: "VERIFYING", Origin: origin, Completion: s.lastCompletion, Started: time.Now().UTC().Format(time.RFC3339Nano)}
	s.Verification = v
	_ = s.persistLocked()
	go s.runProtectionVerification(v)
	return nil
}

func changedTestDefinitions(before, after protectedTree) []string {
	changed := []string{}
	for name, body := range after.Files {
		if bytes.Equal(before.Files[name], body) && before.Files[name] != nil {
			continue
		}
		base := strings.ToLower(filepath.Base(name))
		lower := strings.ToLower(name)
		if strings.Contains(lower, "test") || strings.Contains(lower, "spec") || strings.Contains(lower, "fixture") || base == "makefile" || base == "cmakelists.txt" || base == "package.json" || base == "cargo.toml" {
			changed = append(changed, name)
		}
	}
	for name := range before.Files {
		if _, ok := after.Files[name]; !ok {
			lower := strings.ToLower(name)
			if strings.Contains(lower, "test") || strings.Contains(lower, "spec") {
				changed = append(changed, name+" (deleted)")
			}
		}
	}
	sort.Strings(changed)
	return changed
}

func (s *PiWorkspaceSession) runProtectionVerification(v *WorkspaceVerification) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	// One independent sandbox alongside at most one bounded Pi scope.
	s.manager.verificationMu.Lock()
	defer s.manager.verificationMu.Unlock()
	result := *v
	result.Status = "UNVERIFIED"
	result.Receipt = filepath.Join(s.dir, "verification", result.ID, "receipt.json")
	defer func() {
		result.Finished = time.Now().UTC().Format(time.RFC3339Nano)
		if e := writeJSON(result.Receipt, &result); e != nil {
			result.Status = "UNVERIFIED"
			result.Error = "receipt persistence failed: " + e.Error()
		}
		s.mu.Lock()
		s.Verification = &result
		s.verificationRunning = false
		if s.State == "VERIFYING" {
			if s.stdin != nil {
				s.State = "READY"
			} else {
				s.State = "CONNECTED"
			}
		}
		_ = s.persistLocked()
		s.mu.Unlock()
		raw, _ := json.Marshal(map[string]any{"type": "independent_verification", "verification": result})
		s.emit(raw)
	}()
	if e := s.validateWorkingRoot(); e != nil {
		result.Error = e.Error()
		return
	}
	candidate, e := readProtectedTree(s.Root, s.manager.app.cfg)
	if e != nil {
		result.Error = e.Error()
		return
	}
	result.CandidateSHA256 = protectedTreeHash(candidate)
	result.FileSHA256 = map[string]string{}
	result.OriginalFileSHA256 = map[string]string{}
	for name, body := range s.protectedOriginal.Files {
		result.OriginalFileSHA256[name] = applyHash(body)
	}
	for name, body := range candidate.Files {
		result.FileSHA256[name] = applyHash(body)
	}
	result.Excluded = candidate.Excluded
	result.ChangedTests = changedTestDefinitions(s.protectedOriginal, candidate)
	artifact := filepath.Join(s.dir, "verification", result.ID, "verified")
	if e = os.MkdirAll(artifact, 0700); e == nil {
		e = writeFilesWithModes(artifact, candidate.Files, candidate.Modes)
	}
	if e != nil {
		result.Error = e.Error()
		return
	}
	if len(s.TestCommand) == 0 {
		if len(s.BuildCommand) > 0 {
			// A build without a user test is useful evidence but never TEST_PASS.
			_, result.Build, e = runChecks(context.Background(), protectionConfig(s.manager.app.cfg), candidate.Files, nil, nil, s.BuildCommand, candidate.Modes)
			if e != nil {
				result.Error = e.Error()
				return
			}
			if result.Build != nil && result.Build.Passed {
				result.Status = "BUILD_PASS"
			} else {
				result.Status = "FAIL"
			}
		}
		result.Error = "No user-configured test command; correctness unverified; Apply disabled"
	} else {
		result.Build, result.Tests, e = runChecks(context.Background(), protectionConfig(s.manager.app.cfg), candidate.Files, nil, s.BuildCommand, s.TestCommand, candidate.Modes)
		if e != nil {
			result.Error = "isolated verification setup: " + e.Error()
			return
		}
		result.Status = "FAIL"
		if result.Tests != nil && result.Tests.Passed && (result.Build == nil || result.Build.Passed) {
			result.Status = "TEST_PASS"
		}
		if (result.Build != nil && result.Build.TimedOut) || (result.Tests != nil && result.Tests.TimedOut) {
			result.Status = "INCOMPLETE"
		}
	}
	for _, check := range []*CommandResult{result.Build, result.Tests} {
		if check != nil && (check.ExitCode == 126 || check.ExitCode == 127) {
			result.Status = "UNVERIFIED"
			result.Error = "Verification command/tool unavailable or not executable; inspect command output and fix setup"
		}
	}
	after, e := readProtectedTree(s.Root, s.manager.app.cfg)
	if e != nil || !sameProtectedTree(candidate, after) {
		result.Status = "UNVERIFIED"
		result.Error = "working files changed during verification; rerun checks"
		return
	}
	if result.Completion != "" && result.Completion != "NATURAL" {
		result.Status = "INCOMPLETE"
		result.Error = "Checks do not convert an incomplete agent turn into a completed solution"
	}
}

func (s *PiWorkspaceSession) protectionReview() (map[string]any, error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if s.Kind != "local" || s.Mode == "" {
		return nil, errors.New("bounded local protected/direct session required")
	}
	s.mu.Lock()
	state := s.State
	s.mu.Unlock()
	if state == "RUNNING" || state == "SHELL" || state == "VERIFYING" {
		return nil, errors.New("review requires an idle session")
	}
	candidate, e := readProtectedTree(s.Root, s.manager.app.cfg)
	if e != nil {
		return nil, e
	}
	changes, deleted := protectionChanges(s.protectedOriginal, candidate)
	patch, e := protectionPatch(s.protectedOriginal, candidate)
	if e != nil {
		return nil, e
	}
	s.mu.Lock()
	if s.Verification != nil && s.Verification.CandidateSHA256 != "" && s.Verification.CandidateSHA256 != protectedTreeHash(candidate) && !s.Verification.Applied {
		copy := *s.Verification
		copy.Status = "UNVERIFIED"
		copy.Error = "working files changed since verification"
		s.Verification = &copy
		_ = s.persistLocked()
	}
	v := s.Verification
	s.mu.Unlock()
	return map[string]any{"mode": s.Mode, "original_root": s.OriginalRoot, "working_root": s.Root, "diff": patch, "files_changed": changes, "deleted": deleted, "verification": v, "candidate_sha256": protectedTreeHash(candidate), "apply_supported": s.Mode == "protected" && len(deleted) == 0, "note": "Only bounded source files are included. Deletions/mode changes are reviewable but automatic deletion/mode-only apply is not supported. Passing configured commands is not proof of all correctness; review changed tests/build definitions."}, nil
}

func protectionChanges(before, after protectedTree) ([]string, []string) {
	changes, deleted := []string{}, []string{}
	for name, body := range after.Files {
		old, ok := before.Files[name]
		if !ok || !bytes.Equal(body, old) || after.Modes[name].Perm() != before.Modes[name].Perm() {
			changes = append(changes, name)
		}
	}
	for name := range before.Files {
		if _, ok := after.Files[name]; !ok {
			deleted = append(deleted, name)
		}
	}
	sort.Strings(changes)
	sort.Strings(deleted)
	return changes, deleted
}

func protectionWorkspace(before, after protectedTree, repo string) Workspace {
	w := Workspace{Repo: repo, RepoInfo: before.Info, Original: cloneFiles(before.Files), Modes: map[string]fs.FileMode{}, Existed: map[string]bool{}, Verified: map[string]string{}, Editable: map[string]bool{}}
	for name, mode := range before.Modes {
		w.Modes[name] = mode
		w.Existed[name] = true
		w.Editable[name] = true
	}
	for name, mode := range after.Modes {
		if !w.Existed[name] {
			w.Modes[name] = mode
			w.Existed[name] = false
			w.Editable[name] = true
		}
	}
	return w
}

func protectionPatch(before, after protectedTree) (string, error) {
	_, deleted := protectionChanges(before, after)
	if len(deleted) > 0 {
		return "Deleted paths (automatic Apply blocked):\n" + strings.Join(deleted, "\n"), nil
	}
	w := protectionWorkspace(before, after, "")
	return makePatch(w, after.Files)
}

func (s *PiWorkspaceSession) applyProtection(allowTestChanges ...bool) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	s.mu.Lock()
	v := s.Verification
	state := s.State
	s.mu.Unlock()
	if s.Mode != "protected" || s.Kind != "local" {
		return errors.New("Apply exists only for protected local sessions; direct mode already writes original files")
	}
	if state != "READY" && state != "CONNECTED" {
		return errors.New("Apply requires an idle session")
	}
	if v == nil || v.Status != "TEST_PASS" || v.Applied || v.ID == "" {
		return errors.New("only an unapplied independent TEST_PASS candidate may apply")
	}
	if len(v.ChangedTests) > 0 && (len(allowTestChanges) == 0 || !allowTestChanges[0]) {
		return errors.New("test/build definitions changed; review the diff and explicitly confirm allow_test_changes:true; command success is not an unchanged independent oracle")
	}
	candidate, e := readProtectedTree(s.Root, s.manager.app.cfg)
	if e != nil {
		return e
	}
	if protectedTreeHash(candidate) != v.CandidateSHA256 {
		s.mu.Lock()
		copy := *v
		copy.Status = "UNVERIFIED"
		copy.Error = "candidate changed after verification"
		s.Verification = &copy
		_ = s.persistLocked()
		s.mu.Unlock()
		return errors.New("candidate changed after verification; rerun checks")
	}
	original, e := readProtectedTree(s.OriginalRoot, s.manager.app.cfg)
	if e != nil {
		return e
	}
	if !os.SameFile(s.protectedOriginal.Info, original.Info) || !sameProtectedTree(s.protectedOriginal, original) {
		return errors.New("original source changed since snapshot; explicit conflict resolution/new session required; nothing applied")
	}
	changed, deleted := protectionChanges(s.protectedOriginal, candidate)
	if len(deleted) > 0 {
		return errors.New("BLOCKED_DELETION_APPLY: review deletions manually; original remains untouched")
	}
	for _, name := range changed {
		if mode, ok := s.protectedOriginal.Modes[name]; ok && mode.Perm() != candidate.Modes[name].Perm() {
			return fmt.Errorf("mode change requires manual review: %s", name)
		}
	}
	w := protectionWorkspace(s.protectedOriginal, candidate, s.OriginalRoot)
	for _, name := range changed {
		w.Verified[name] = v.FileSHA256[name]
	}
	t := &Task{ID: s.ID + "-" + v.ID, Status: "PASS", FilesChanged: changed, workspace: w, output: filepath.Join(s.dir, "verification", v.ID)}
	if e = s.manager.app.applyTask(t); e != nil {
		return e
	}
	s.mu.Lock()
	copy := *v
	copy.Applied = true
	s.Verification = &copy
	_ = s.persistLocked()
	s.mu.Unlock()
	return nil
}

func (a *App) registerProtectionRoutes(mux *http.ServeMux) {
	m := workspacesFor(a)
	mux.HandleFunc("GET /v1/workspaces/sessions/{id}/review", func(w http.ResponseWriter, r *http.Request) {
		s := m.requestSession(w, r)
		if s == nil {
			return
		}
		out, e := s.protectionReview()
		if e != nil {
			jsonReply(w, 409, map[string]string{"error": e.Error()})
			return
		}
		jsonReply(w, 200, out)
	})
}
