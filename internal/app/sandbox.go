package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type CommandResult struct {
	Passed   bool     `json:"passed"`
	ExitCode int      `json:"exit_code"`
	Output   string   `json:"output"`
	Seconds  float64  `json:"seconds"`
	TimedOut bool     `json:"timed_out"`
	Command  []string `json:"command"`
}
type boundedOutput struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if b.b.Len() < 32768 {
		left := 32768 - b.b.Len()
		if len(p) > left {
			p = p[:left]
		}
		b.b.Write(p)
	}
	return n, nil
}
func writeFiles(root string, files map[string][]byte) error {
	return writeFilesWithModes(root, files, nil)
}

func writeFilesWithModes(root string, files map[string][]byte, modes map[string]fs.FileMode) error {
	for n, b := range files {
		if e := cleanRel(n); e != nil {
			return e
		}
		p := filepath.Join(root, n)
		if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
			return e
		}
		mode := fs.FileMode(0600)
		if original, ok := modes[n]; ok {
			// Preserve executable scripts and ordinary permission bits, but never
			// setuid/setgid/sticky metadata. The private copy remains owner-RW.
			mode |= original.Perm()
		}
		if e := os.WriteFile(p, b, mode); e != nil {
			return e
		}
		// Creation permissions can be narrowed by umask; retained executable
		// and source permission bits must be deterministic in fresh snapshots.
		if e := os.Chmod(p, mode); e != nil {
			return e
		}
	}
	return nil
}
func sandboxEntry(args []string) error {
	if len(args) == 0 {
		return errors.New("missing sandbox command")
	}
	for k, v := range map[int]uint64{syscall.RLIMIT_CPU: 90, syscall.RLIMIT_FSIZE: 128 << 20, syscall.RLIMIT_NOFILE: 256, syscall.RLIMIT_CORE: 0} {
		if e := syscall.Setrlimit(k, &syscall.Rlimit{Cur: v, Max: v}); e != nil {
			return e
		}
	}
	p, e := exec.LookPath(args[0])
	if e != nil {
		return e
	}
	return syscall.Exec(p, args, os.Environ())
}
func sandboxCommand(ctx context.Context, c Config, work string, hidden map[string][]byte, argv []string) (CommandResult, error) {
	r := CommandResult{Command: argv, ExitCode: -1}
	if len(argv) == 0 {
		return r, errors.New("empty command")
	}
	start := time.Now()
	defer func() { r.Seconds = time.Since(start).Seconds() }()
	exe, e := os.Executable()
	if e != nil {
		return r, e
	}
	unit := "strixglm-test-" + id()
	bargs := []string{"--unshare-all", "--die-with-parent", "--new-session", "--cap-drop", "ALL", "--clearenv", "--setenv", "PATH", "/usr/bin:/bin", "--setenv", "HOME", "/tmp", "--setenv", "LANG", "C.UTF-8", "--setenv", "TMPDIR", "/tmp", "--setenv", "PYTHONDONTWRITEBYTECODE", "1", "--proc", "/proc", "--dev", "/dev", "--size", "134217728", "--tmpfs", "/tmp"}
	for _, p := range []string{"/usr", "/bin", "/lib", "/lib64"} {
		if _, e := os.Stat(p); e == nil {
			bargs = append(bargs, "--ro-bind", p, p)
		}
	}
	bargs = append(bargs, "--ro-bind", exe, "/strixglm", "--bind", work, "/work", "--chdir", "/work")
	hiddenDir, e := os.MkdirTemp(c.StateDir, "oracle-")
	if e != nil {
		return r, e
	}
	defer os.RemoveAll(hiddenDir) // exact directory created by this invocation
	if e = writeFiles(hiddenDir, hidden); e != nil {
		return r, e
	}
	for name := range hidden {
		root, openErr := os.OpenRoot(work)
		if openErr != nil {
			return r, openErr
		}
		if e = root.MkdirAll(filepath.Dir(name), 0700); e != nil {
			root.Close()
			return r, e
		}
		// Never truncate an existing build-controlled pathname on the host.
		placeholder, createErr := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if createErr == nil {
			placeholder.Close()
		} else if !os.IsExist(createErr) {
			root.Close()
			return r, createErr
		}
		info, statErr := root.Lstat(name)
		root.Close()
		if statErr != nil || !info.Mode().IsRegular() {
			return r, errors.New("unsafe hidden mount target")
		}
		resolved, resolveErr := filepath.EvalSymlinks(filepath.Join(work, name))
		if resolveErr != nil || resolved != filepath.Join(work, name) {
			return r, errors.New("symlink in hidden mount target")
		}
		bargs = append(bargs, "--ro-bind", filepath.Join(hiddenDir, name), "/work/"+name)
	}
	bargs = append(bargs, "--remount-ro", "/", "/strixglm", "sandbox-exec")
	bargs = append(bargs, argv...)
	args := []string{"--user", "--scope", "--quiet", "--collect", "--unit", unit, "-p", "MemoryMax=" + strconv.FormatInt(c.SandboxMemoryBytes, 10), "-p", "MemorySwapMax=0", "-p", "TasksMax=" + strconv.Itoa(c.SandboxTasks), "/usr/bin/bwrap"}
	args = append(args, bargs...)
	limited, cancel := context.WithTimeout(ctx, time.Duration(c.SandboxTimeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(limited, "systemd-run", args...)
	out := &boundedOutput{}
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 3 * time.Second
	cmd.Cancel = func() error {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		_ = exec.CommandContext(stopCtx, "systemctl", "--user", "stop", unit+".scope").Run()
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	e = cmd.Run()
	r.Seconds = time.Since(start).Seconds()
	out.mu.Lock()
	r.Output = out.b.String()
	out.mu.Unlock()
	r.TimedOut = limited.Err() != nil
	if cmd.ProcessState != nil {
		r.ExitCode = cmd.ProcessState.ExitCode()
	}
	r.Passed = e == nil && !r.TimedOut
	if strings.Contains(r.Output, "bwrap:") || strings.Contains(r.Output, "Failed to connect to bus") {
		return r, fmt.Errorf("sandbox infrastructure: %s", r.Output)
	}
	return r, nil
}
func runChecks(ctx context.Context, c Config, files, hidden map[string][]byte, build, test []string, originalModes ...map[string]fs.FileMode) (*CommandResult, *CommandResult, error) {
	work, e := os.MkdirTemp(c.StateDir, "workspace-")
	if e != nil {
		return nil, nil, e
	}
	defer os.RemoveAll(work) // only our exact private temporary workspace
	var modes map[string]fs.FileMode
	if len(originalModes) > 0 {
		modes = originalModes[0]
	}
	if e = writeFilesWithModes(work, files, modes); e != nil {
		return nil, nil, e
	}
	var b *CommandResult
	if len(build) > 0 {
		r, e := sandboxCommand(ctx, c, work, hidden, build)
		b = &r
		if e != nil || !r.Passed {
			return b, nil, e
		}
	}
	r, e := sandboxCommand(ctx, c, work, hidden, test)
	return b, &r, e
}
