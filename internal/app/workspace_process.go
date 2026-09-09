package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

func piModels(c Config, endpoint string) map[string]any {
	limit := c.ChatContextTokens
	if limit == 0 {
		limit = c.MaxContextTokens
	}
	out := c.ChatDefaultOutput
	if out == 0 {
		out = 4096
	}
	model := map[string]any{"id": c.Model, "name": "GLM Flash TP2 — local gateway", "reasoning": true, "input": []string{"text"}, "contextWindow": limit, "maxTokens": out, "thinkingLevelMap": map[string]any{"off": nil, "minimal": nil, "low": "low", "medium": nil, "high": "high", "xhigh": nil, "max": "max"}, "cost": map[string]int{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0}}
	provider := map[string]any{"baseUrl": endpoint + "/v1", "api": "openai-completions", "apiKey": "$STRIXGLM_PI_API_TOKEN", "authHeader": true, "compat": map[string]any{"maxTokensField": "max_tokens", "supportsDeveloperRole": false, "supportsReasoningEffort": true, "supportsUsageInStreaming": true, "supportsStore": false, "supportsStrictMode": false}, "models": []map[string]any{model}}
	return map[string]any{"providers": map[string]any{"strixglm": provider}}
}
func (s *PiWorkspaceSession) piCommand() (*exec.Cmd, error) {
	private := filepath.Join(s.dir, "pi-config")
	sessions := filepath.Join(s.dir, "pi-sessions")
	for _, p := range []string{private, sessions} {
		if e := os.MkdirAll(p, 0700); e != nil {
			return nil, e
		}
	}
	if e := s.startBridge(); e != nil {
		return nil, e
	}
	if e := writeJSON(filepath.Join(private, "models.json"), piModels(s.manager.app.cfg, s.bridgeURL)); e != nil {
		return nil, e
	}
	settings := map[string]any{"defaultThinkingLevel": s.Reasoning, "quietStartup": true, "compaction": map[string]any{"enabled": false}, "retry": map[string]any{"enabled": false, "provider": map[string]any{"maxRetries": 0, "timeoutMs": s.manager.app.cfg.ModelTimeout * 1000}}}
	if e := writeJSON(filepath.Join(private, "settings.json"), settings); e != nil {
		return nil, e
	}
	bargs := []string{"--unshare-all", "--die-with-parent", "--new-session", "--cap-drop", "ALL", "--unsetenv", "DBUS_SESSION_BUS_ADDRESS", "--unsetenv", "XDG_RUNTIME_DIR", "--setenv", "PATH", "/usr/bin:/bin", "--setenv", "LANG", "C.UTF-8", "--setenv", "PI_CODING_AGENT_DIR", "/pi-config", "--setenv", "PI_CODING_AGENT_SESSION_DIR", "/pi-sessions", "--setenv", "PI_OFFLINE", "1", "--setenv", "PI_SKIP_VERSION_CHECK", "1", "--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp"}
	for _, p := range []string{"/usr", "/bin", "/lib", "/lib64"} {
		if _, e := os.Stat(p); e == nil {
			bargs = append(bargs, "--ro-bind", p, p)
		}
	}
	// Node's homedir lookup requires passwd when HOME is deliberately not
	// repurposed. This exposes account metadata, not the host home directory.
	bargs = append(bargs, "--ro-bind", "/etc/passwd", "/etc/passwd", "--ro-bind", "/etc/group", "/etc/group")
	bargs = append(bargs, "--ro-bind", piRuntime, "/pi-runtime", "--bind", private, "/pi-config", "--bind", sessions, "/pi-sessions")
	bargs = append(bargs, "--ro-bind", productRoot+"/runtime/pi-launch.mjs", "/pi-launch.mjs", "--ro-bind", s.bridgeDir, "/pi-bridge")
	// Child environment is constructed from scratch below. The credential is
	// inherited as an environment variable, never as a visible process argument.
	tools := "read,bash,edit,write,grep,find,ls"
	if s.Kind == "local" {
		if _, e := validateLocalWorkspace(s.manager.app.cfg, s.Root); e != nil {
			return nil, e
		}
		bargs = append(bargs, "--bind", s.Root, "/workspace")
	} else {
		placeholder := filepath.Join(s.dir, "remote-cwd")
		if e := os.MkdirAll(placeholder, 0700); e != nil {
			return nil, e
		}
		bargs = append(bargs, "--bind", placeholder, "/workspace", "--dir", "/pi-extensions", "--symlink", "/pi-runtime/node_modules", "/pi-extensions/node_modules", "--ro-bind", productRoot+"/runtime/pi-ssh.ts", "/pi-extensions/pi-ssh.ts", "--bind", s.dir+"/ssh", "/ssh-control")
		sshArgs := []string{"-F", "/dev/null", "-S", "/ssh-control/control", "-p", strconv.Itoa(s.preset.Port), "-l", s.preset.User, "-o", "BatchMode=yes", "-o", "ControlMaster=no", "-o", "ProxyCommand=/bin/false", s.preset.Host}
		encoded, _ := json.Marshal(sshArgs)
		bargs = append(bargs, "--setenv", "STRIXGLM_SSH_ARGS", string(encoded), "--setenv", "STRIXGLM_REMOTE_ROOT", s.Root)
		tools = "read,bash,edit,write"
	}
	bargs = append(bargs, "--chdir", "/workspace", "/usr/bin/node", "/pi-launch.mjs", "--mode", "rpc", "--provider", "strixglm", "--model", s.manager.app.cfg.Model, "--thinking", s.Reasoning, "--no-extensions", "--no-skills", "--no-prompt-templates", "--no-themes", "--no-context-files", "--no-approve", "--tools", tools)
	if s.Kind == "ssh" {
		bargs = append(bargs, "-e", "/pi-extensions/pi-ssh.ts")
	}
	s.mu.Lock()
	s.ScopeUnit = "strixglm-pi-" + s.ID + ".scope"
	s.mu.Unlock()
	args := []string{"--user", "--scope", "--quiet", "--collect", "--unit", s.ScopeUnit, "-p", "MemoryMax=2147483648", "-p", "MemorySwapMax=0", "-p", "TasksMax=128", "-p", "CPUQuota=200%", "/usr/bin/bwrap"}
	args = append(args, bargs...)
	cmd := exec.Command("/usr/bin/systemd-run", args...)
	cmd.Env = append(workspaceSystemEnv(), "STRIXGLM_PI_API_TOKEN="+s.bridgeToken)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd, nil
}

var sshHostRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.:-]{0,252}$`)
var sshUserRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]{0,63}$`)

func validateWorkspacePreset(p WorkspacePreset) error {
	if p.Name == "" || len(p.Name) > 100 || strings.ContainsAny(p.Name, "\x00\r\n") {
		return errors.New("invalid preset name")
	}
	if !sshHostRE.MatchString(p.Host) || !sshUserRE.MatchString(p.User) || p.Port < 1 || p.Port > 65535 {
		return errors.New("explicit SSH host, user and port required; no SSH command/config aliases")
	}
	if !filepath.IsAbs(p.Root) || filepath.Clean(p.Root) != p.Root || p.Root == "/" || strings.ContainsAny(p.Root, "\x00\r\n") {
		return errors.New("absolute specific remote project root required")
	}
	if p.KeyPath != "" {
		if !filepath.IsAbs(p.KeyPath) || filepath.Clean(p.KeyPath) != p.KeyPath {
			return errors.New("absolute private-key path required")
		}
		info, e := os.Lstat(p.KeyPath)
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return errors.New("private key must be a regular owner-only file; key contents are never returned")
		}
	}
	return nil
}
func (s *PiWorkspaceSession) controlSocket() string { return filepath.Join(s.dir, "ssh", "control") }
func (s *PiWorkspaceSession) sshBaseArgs() []string {
	p := s.preset
	args := []string{"-F", "/dev/null", "-p", strconv.Itoa(p.Port), "-l", p.User, "-o", "StrictHostKeyChecking=yes", "-o", "UserKnownHostsFile=/home/funboy/.ssh/known_hosts", "-o", "GlobalKnownHostsFile=/etc/ssh/ssh_known_hosts", "-o", "ConnectTimeout=15", "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=2", "-o", "ForwardAgent=no", "-o", "ClearAllForwardings=yes", "-o", "ControlPath=" + s.controlSocket()}
	if p.KeyPath != "" {
		args = append(args, "-i", p.KeyPath, "-o", "IdentitiesOnly=yes")
	}
	return args
}
func (s *PiWorkspaceSession) sshArgs() []string {
	args := s.sshBaseArgs()
	args = append(args, "-o", "ControlMaster=no", "-o", "BatchMode=yes", "-o", "ProxyCommand=/bin/false", s.preset.Host)
	return args
}
func (s *PiWorkspaceSession) connect(ctx context.Context, password string) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	s.mu.Lock()
	state := s.State
	s.mu.Unlock()
	if s.Kind == "local" {
		if state == "CONNECTED" {
			return nil
		}
		return errors.New("local session cannot reconnect")
	}
	if state != "CREATED" {
		return errors.New("create a new session before reconnecting")
	}
	if e := validateWorkspacePreset(s.preset); e != nil {
		return e
	}
	if len(password) > 4096 || strings.ContainsAny(password, "\x00\r\n") {
		return errors.New("invalid password")
	}
	if e := os.MkdirAll(filepath.Dir(s.controlSocket()), 0700); e != nil {
		return e
	}
	args := s.sshBaseArgs()
	args = append(args, "-o", "ControlMaster=yes", "-o", "ControlPersist=no", "-N")
	env := []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8"}
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		env = append(env, "SSH_AUTH_SOCK="+sock)
	}
	var broker *askpassBroker
	if password != "" {
		var e error
		broker, e = startAskpass(filepath.Join(s.dir, "ssh", "askpass.sock"), []byte(password))
		if e != nil {
			return e
		}
		defer broker.close()
		env = append(env, "SSH_ASKPASS="+productRoot+"/runtime/ssh-askpass", "SSH_ASKPASS_REQUIRE=force", "DISPLAY=:0", "STRIXGLM_ASKPASS_SOCKET="+broker.path)
		args = append(args, "-o", "PreferredAuthentications=password,keyboard-interactive", "-o", "PubkeyAuthentication=no", "-o", "NumberOfPasswordPrompts=1")
	} else {
		args = append(args, "-o", "BatchMode=yes")
	}
	args = append(args, s.preset.Host)
	cmd := exec.Command("/usr/bin/ssh", args...)
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stderr := &workspaceBoundedBuffer{max: 8192}
	cmd.Stderr = stderr
	if e := cmd.Start(); e != nil {
		return e
	}
	s.mu.Lock()
	s.ssh = cmd
	s.sshDone = make(chan struct{})
	done := s.sshDone
	s.mu.Unlock()
	go func() { _ = cmd.Wait(); close(done) }()
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return errors.New("SSH connection failed: " + redactPassword(stderr.String(), password))
		case <-cctx.Done():
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
			return cctx.Err()
		case <-ticker.C:
			if _, e := os.Stat(s.controlSocket()); e != nil {
				continue
			}
			out, e := s.remote(cctx, "test -d "+shellQuote(s.Root)+" && test ! -L "+shellQuote(s.Root)+" && command -v bash >/dev/null && command -v realpath >/dev/null && realpath -e -- "+shellQuote(s.Root))
			if e != nil || strings.TrimSpace(string(out)) != s.Root {
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
				return errors.New("remote root must exist without symlink aliases and provide bash/realpath")
			}
			s.setState("CONNECTED", "")
			return nil
		}
	}
}
func redactPassword(s, password string) string {
	if password != "" {
		return strings.ReplaceAll(s, password, "[REDACTED]")
	}
	return s
}

type workspaceBoundedBuffer struct {
	mu   syncMutex
	data []byte
	max  int
}

// Alias keeps the bounded subprocess collector's lock independent of session locks.
type syncMutex = sync.Mutex

func (b *workspaceBoundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if len(b.data) < b.max {
		remaining := b.max - len(b.data)
		if len(p) > remaining {
			p = p[:remaining]
		}
		b.data = append(b.data, p...)
	}
	return n, nil
}
func (b *workspaceBoundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.data)
}
func (s *PiWorkspaceSession) remote(ctx context.Context, command string) ([]byte, error) {
	args := append(s.sshArgs(), "bash -c "+shellQuote(command))
	cmd := exec.CommandContext(ctx, "/usr/bin/ssh", args...)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8"}
	out := &workspaceBoundedBuffer{max: (256 << 10) + 1}
	cmd.Stdout = out
	cmd.Stderr = out
	e := cmd.Run()
	return []byte(out.String()), e
}
func (s *PiWorkspaceSession) remoteGuard(path string) string {
	target := filepath.Join(s.Root, path)
	return "p=$(realpath -e -- " + shellQuote(target) + ") && case \"$p\" in " + shellQuote(s.Root) + "|" + shellQuote(s.Root) + "/*) ;; *) exit 73;; esac && "
}
func (s *PiWorkspaceSession) remoteFiles(ctx context.Context, path string, file bool) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	command := s.remoteGuard(path)
	if file {
		b, e := s.remote(ctx, command+"test -f \"$p\" && head -c 262145 -- \"$p\"")
		if e != nil {
			return nil, e
		}
		if bytes.IndexByte(b, 0) >= 0 || !utf8.Valid(b) {
			return nil, errors.New("binary/non-UTF-8 file refused")
		}
		trunc := len(b) > 256<<10
		if trunc {
			b = b[:256<<10]
		}
		return map[string]any{"path": path, "content": string(b), "truncated": trunc}, nil
	}
	b, e := s.remote(ctx, command+"test -d \"$p\" && find \"$p\" -mindepth 1 -maxdepth 1 -printf '%f\\0%y\\0%s\\0'")
	if e != nil {
		return nil, e
	}
	parts := bytes.Split(b, []byte{0})
	out := []map[string]any{}
	for i := 0; i+2 < len(parts) && len(out) < 1000; i += 3 {
		name := string(parts[i])
		if strings.HasPrefix(name, ".") {
			continue
		}
		kind := "file"
		if string(parts[i+1]) == "d" {
			kind = "directory"
		}
		if string(parts[i+1]) == "l" {
			kind = "symlink"
		}
		size, _ := strconv.ParseInt(string(parts[i+2]), 10, 64)
		out = append(out, map[string]any{"name": name, "path": filepath.Join(path, name), "type": kind, "size": size})
	}
	return map[string]any{"path": path, "entries": out, "truncated": len(out) >= 1000 || len(b) > 256<<10}, nil
}

var workspaceDiagnostics = map[string][]string{"pwd": {"/usr/bin/pwd"}, "git-status": {"/usr/bin/git", "--no-optional-locks", "-c", "core.fsmonitor=false", "status", "--short", "--untracked-files=normal"}, "list": {"/usr/bin/ls", "-la", "--", "."}, "disk": {"/usr/bin/df", "-h", "--", "."}}

func (s *PiWorkspaceSession) terminal(ctx context.Context, commandID string) (map[string]any, error) {
	argv, ok := workspaceDiagnostics[commandID]
	if !ok {
		return nil, errors.New("unknown diagnostic command_id; arbitrary terminal commands are not accepted")
	}
	s.mu.Lock()
	state := s.State
	s.mu.Unlock()
	if state == "CREATED" || state == "CLOSED" || state == "FAILED" {
		return nil, errors.New("session not connected")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	start := time.Now()
	var output []byte
	var e error
	if s.Kind == "ssh" {
		quoted := []string{}
		for _, a := range argv {
			quoted = append(quoted, shellQuote(a))
		}
		output, e = s.remote(ctx, s.remoteGuard(".")+"cd -- \"$p\" && "+strings.Join(quoted, " "))
	} else {
		args := []string{"--unshare-all", "--die-with-parent", "--new-session", "--clearenv", "--setenv", "PATH", "/usr/bin:/bin", "--setenv", "LANG", "C.UTF-8", "--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp"}
		for _, p := range []string{"/usr", "/bin", "/lib", "/lib64"} {
			args = append(args, "--ro-bind", p, p)
		}
		args = append(args, "--ro-bind", s.Root, "/workspace", "--chdir", "/workspace")
		args = append(args, argv...)
		cmd := exec.CommandContext(ctx, "/usr/bin/bwrap", args...)
		buf := &workspaceBoundedBuffer{max: 64 << 10}
		cmd.Stdout = buf
		cmd.Stderr = buf
		e = cmd.Run()
		output = []byte(buf.String())
	}
	code := 0
	if e != nil {
		code = -1
		var x *exec.ExitError
		if errors.As(e, &x) {
			code = x.ExitCode()
		}
	}
	return map[string]any{"command_id": commandID, "output": string(output), "exit_code": code, "seconds": time.Since(start).Seconds(), "truncated": len(output) >= 64<<10}, nil
}

type askpassBroker struct {
	path     string
	listener net.Listener
	done     chan struct{}
	secret   []byte
}

func startAskpass(path string, secret []byte) (*askpassBroker, error) {
	l, e := net.Listen("unix", path)
	if e != nil {
		return nil, e
	}
	if e = os.Chmod(path, 0600); e != nil {
		_ = l.Close()
		return nil, e
	}
	b := &askpassBroker{path: path, listener: l, done: make(chan struct{}), secret: secret}
	go func() {
		defer close(b.done)
		c, e := l.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(5 * time.Second))
		_, _ = c.Write(append(b.secret, '\n'))
	}()
	return b, nil
}
func (b *askpassBroker) close() {
	_ = b.listener.Close()
	<-b.done
	for i := range b.secret {
		b.secret[i] = 0
	}
	_ = os.Remove(b.path)
}
