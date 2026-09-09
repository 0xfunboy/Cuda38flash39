package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

func workspaceSystemEnv() []string {
	env := []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8"}
	for _, key := range []string{"XDG_RUNTIME_DIR", "DBUS_SESSION_BUS_ADDRESS"} {
		if v := os.Getenv(key); v != "" {
			env = append(env, key+"="+v)
		}
	}
	return env
}
func (s *PiWorkspaceSession) scopeProperties(ctx context.Context) (map[string]string, error) {
	cmd := exec.CommandContext(ctx, "/usr/bin/systemctl", "--user", "show", s.ScopeUnit, "-p", "Id", "-p", "LoadState", "-p", "InvocationID", "-p", "ActiveState", "-p", "MemoryMax", "-p", "MemorySwapMax", "-p", "TasksMax", "-p", "CPUQuotaPerSecUSec", "-p", "ControlGroup")
	cmd.Env = workspaceSystemEnv()
	b, e := cmd.Output()
	out := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok {
			out[k] = v
		}
	}
	if e != nil && out["LoadState"] != "not-found" {
		return nil, e
	}
	return out, nil
}

func ownedScopeMayStop(unit, invocation string, props map[string]string) (bool, error) {
	if props["LoadState"] == "not-found" || props["ActiveState"] == "inactive" {
		return false, nil
	}
	if unit == "" || invocation == "" || props["Id"] != unit || props["InvocationID"] != invocation {
		return false, errors.New("Pi scope identity changed; refusing to stop an unowned invocation")
	}
	return true, nil
}
func (s *PiWorkspaceSession) stopOwnedScope(ctx context.Context) error {
	if s.ScopeUnit == "" {
		return nil
	}
	props, e := s.scopeProperties(ctx)
	if e != nil {
		return e
	}
	stop, e := ownedScopeMayStop(s.ScopeUnit, s.ScopeInvocation, props)
	if e != nil || !stop {
		return e
	}
	cmd := exec.CommandContext(ctx, "/usr/bin/systemctl", "--user", "stop", s.ScopeUnit)
	cmd.Env = workspaceSystemEnv()
	if out, e := cmd.CombinedOutput(); e != nil {
		return fmt.Errorf("owned Pi scope stop: %w: %s", e, out)
	}
	props, e = s.scopeProperties(ctx)
	if e != nil {
		return e
	}
	if props["LoadState"] != "not-found" && props["ActiveState"] != "inactive" {
		return errors.New("owned Pi scope has not stopped")
	}
	return nil
}
func (s *PiWorkspaceSession) verifyScope(ctx context.Context) error {
	props, e := s.scopeProperties(ctx)
	if e != nil {
		return e
	}
	for key, want := range map[string]string{"ActiveState": "active", "MemoryMax": "2147483648", "MemorySwapMax": "0", "TasksMax": "128", "CPUQuotaPerSecUSec": "2s"} {
		if props[key] != want {
			return fmt.Errorf("Pi resource scope %s=%q, expected %q", key, props[key], want)
		}
	}
	if props["InvocationID"] == "" || !strings.HasSuffix(props["ControlGroup"], "/"+s.ScopeUnit) {
		return errors.New("Pi scope ownership/cgroup receipt missing")
	}
	s.mu.Lock()
	s.ScopeInvocation = props["InvocationID"]
	_ = s.persistLocked()
	s.mu.Unlock()
	return nil
}
func (s *PiWorkspaceSession) shell(ctx context.Context, command string) (map[string]any, error) {
	if len(command) > 8192 || strings.TrimSpace(command) == "" || strings.IndexByte(command, 0) >= 0 {
		return nil, errors.New("shell command must contain 1..8192 bytes without NUL")
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()
	s.mu.Lock()
	state := s.State
	s.mu.Unlock()
	if state != "READY" {
		return nil, errors.New("shell requires an idle started Pi session; cannot overlap generation")
	}
	s.setState("SHELL", "")
	start := time.Now()
	if s.Kind == "ssh" {
		// Explicit remote shell carries the selected remote account's authority.
		// It is not advertised as a remote OS sandbox or resource-limited process.
		cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		out, e := s.remote(cctx, s.remoteGuard(".")+"cd -- \"$p\" && "+command)
		s.setState("READY", "")
		code := 0
		if e != nil {
			code = -1
		}
		return map[string]any{"output": string(out), "exit_code": code, "seconds": time.Since(start).Seconds(), "truncated": len(out) > 256<<10, "remote_account_authority": true}, nil
	}
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	v, e := s.rpc(cctx, map[string]any{"type": "bash", "command": command})
	if e != nil {
		abortCtx, abortCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer abortCancel()
		_, abortErr := s.rpc(abortCtx, map[string]any{"type": "abort_bash"})
		if abortErr != nil {
			s.setState("FAILED", "shell cancellation unconfirmed: "+abortErr.Error())
		} else {
			s.setState("READY", "")
		}
		return nil, e
	}
	s.setState("READY", "")
	data, ok := v["data"].(map[string]any)
	if !ok {
		return nil, errors.New("Pi bash returned invalid result")
	}
	return map[string]any{"output": data["output"], "exit_code": data["exitCode"], "seconds": time.Since(start).Seconds(), "truncated": data["truncated"], "cancelled": data["cancelled"], "context_note": "Actual Pi bash output is included on the next prompt; no inference was triggered"}, nil
}
