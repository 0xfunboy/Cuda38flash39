package main

// Applying is a separate explicit capability from generation. Model code never
// executes here; only already verified bytes can enter the original repository.
import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

func applyHash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func makePatch(w Workspace, files map[string][]byte) (string, error) {
	if w.Existed == nil || w.Modes == nil || w.Verified == nil {
		return "", errors.New("missing frozen workspace metadata")
	}
	root, e := os.MkdirTemp("", "strixglm-diff-")
	if e != nil {
		return "", e
	}
	defer os.RemoveAll(root)
	before, after := map[string][]byte{}, map[string][]byte{}
	for name := range w.Verified {
		delete(w.Verified, name)
	}
	for name := range w.Editable {
		if e = cleanRel(name); e != nil {
			return "", e
		}
		existed, known := w.Existed[name]
		if !known {
			return "", fmt.Errorf("missing original existence: %s", name)
		}
		body, ok := files[name]
		if !ok {
			if !existed {
				continue // Allowed but never explicitly produced by the model.
			}
			return "", fmt.Errorf("candidate missing allowed file: %s", name)
		}
		if existed {
			before[name] = w.Original[name]
		}
		// Presence in the candidate map is an explicit creation, even when empty.
		after[name] = body
		if !existed || !bytes.Equal(body, w.Original[name]) {
			w.Verified[name] = applyHash(body)
		}
	}
	if e = writeFilesWithModes(root, before, w.Modes); e != nil {
		return "", e
	}
	runGit := func(args ...string) ([]byte, error) {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		for _, env := range os.Environ() {
			if !strings.HasPrefix(env, "GIT_") {
				cmd.Env = append(cmd.Env, env)
			}
		}
		cmd.Env = append(cmd.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_ATTR_NOSYSTEM=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("git snapshot diff: %w: %s", err, out)
		}
		return out, nil
	}
	if _, e = runGit("init", "--quiet", "--template="); e != nil {
		return "", e
	}
	if _, e = runGit("add", "--all"); e != nil {
		return "", e
	}
	if e = writeFilesWithModes(root, after, w.Modes); e != nil {
		return "", e
	}
	newFiles := []string{}
	for name := range after {
		if !w.Existed[name] {
			newFiles = append(newFiles, name)
		}
	}
	sort.Strings(newFiles)
	if len(newFiles) > 0 {
		if _, e = runGit(append([]string{"add", "--intent-to-add", "--"}, newFiles...)...); e != nil {
			return "", e
		}
	}
	b, e := runGit("-c", "core.fileMode=true", "diff", "--no-ext-diff", "--src-prefix=a/", "--dst-prefix=b/")
	return string(b), e
}

type ApplyFileReceipt struct {
	Path         string `json:"path"`
	Existed      bool   `json:"existed"`
	Mode         uint32 `json:"mode"`
	BeforeSHA256 string `json:"before_sha256"`
	AfterSHA256  string `json:"after_sha256"`
}
type ApplyReceipt struct {
	Schema             string             `json:"schema"`
	TaskID             string             `json:"task_id"`
	Repo               string             `json:"repo"`
	Status             string             `json:"status"`
	Files              []ApplyFileReceipt `json:"files"`
	Applied            []string           `json:"applied"`
	CreatedDirectories []string           `json:"created_directories"`
	Backup             string             `json:"backup"`
	Started            string             `json:"started"`
	Finished           string             `json:"finished,omitempty"`
	Error              string             `json:"error,omitempty"`
	RollbackErrors     []string           `json:"rollback_errors,omitempty"`
}
type applyEntry struct {
	receipt       ApplyFileReceipt
	before, after []byte
	parent        *os.Root
	base, temp    string
}

func (a *App) applyTask(t *Task) error { return a.applyTaskInternal(t, nil, false) }

// hook is only a local fault-injection seam for deterministic regression tests.
// It is not exposed by TaskSpec, tools, HTTP, or the CLI.
func (a *App) applyTaskWithHook(t *Task, hook func(string, string) error) error {
	return a.applyTaskInternal(t, hook, false)
}

// Only the agent's already-verified, explicitly requested automatic apply may
// enter from the nonterminal applying phase. The public apply path remains PASS-only.
func (a *App) applyTaskInternal(t *Task, hook func(string, string) error, automatic bool) (ret error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.Status != "PASS" && !(automatic && t.spec.Apply && t.Status == "applying") {
		return errors.New("only PASS may apply")
	}
	if t.Applied {
		return errors.New("already applied")
	}
	w := t.workspace
	if w.RepoInfo == nil || w.Existed == nil || w.Modes == nil || w.Verified == nil {
		return errors.New("frozen apply metadata unavailable; run a fresh task")
	}
	repo, e := os.OpenRoot(w.Repo)
	if e != nil {
		return e
	}
	defer repo.Close()
	info, e := repo.Stat(".")
	if e != nil || !os.SameFile(w.RepoInfo, info) {
		return errors.New("repository root changed since task")
	}
	// The capability is pinned to this exact directory; symlink swaps cannot
	// redirect access to another host tree. Root never follows outside links.
	verified, e := os.OpenRoot(filepath.Join(t.output, "verified"))
	if e != nil {
		return e
	}
	defer verified.Close()
	names := append([]string(nil), t.FilesChanged...)
	sort.Strings(names)
	entries := []*applyEntry{}
	seen := map[string]bool{}
	for _, name := range names {
		if e = cleanRel(name); e != nil {
			return e
		}
		if !w.Editable[name] || seen[name] {
			return fmt.Errorf("invalid changed path: %s", name)
		}
		seen[name] = true
		existed, known := w.Existed[name]
		mode, modeKnown := w.Modes[name]
		if !known || !modeKnown {
			return fmt.Errorf("missing frozen metadata: %s", name)
		}
		if e = checkApplyPath(repo, name, existed); e != nil {
			return e
		}
		before, e := readApplySource(repo, name, existed, mode, w.Original[name])
		if e != nil {
			return e
		}
		if e = checkApplyPath(verified, name, true); e != nil {
			return fmt.Errorf("verified artifact: %w", e)
		}
		after, _, e := readRootRegular(verified, name, 262144)
		if e != nil {
			return e
		}
		if w.Verified[name] == "" || applyHash(after) != w.Verified[name] {
			return fmt.Errorf("verified artifact changed since tests: %s", name)
		}
		entries = append(entries, &applyEntry{receipt: ApplyFileReceipt{Path: name, Existed: existed, Mode: uint32(mode.Perm()), BeforeSHA256: applyHash(before), AfterSHA256: applyHash(after)}, before: before, after: after, base: filepath.Base(name)})
	}
	// ALL originals and all verified outputs were inspected before creating a
	// directory or temporary file in the original repository.
	transaction := id()
	backup := filepath.Join(t.output, "apply-backup", transaction)
	if e = os.MkdirAll(backup, 0700); e != nil {
		return e
	}
	backupFiles := filepath.Join(backup, "files")
	if e = os.Mkdir(backupFiles, 0700); e != nil {
		return e
	}
	receipt := ApplyReceipt{Schema: "strixglm-apply-v1", TaskID: t.ID, Repo: w.Repo, Status: "PREPARED", Backup: backupFiles, Started: time.Now().UTC().Format(time.RFC3339Nano)}
	for _, entry := range entries {
		receipt.Files = append(receipt.Files, entry.receipt)
		if entry.receipt.Existed {
			if e = controllerBytes(filepath.Join(backupFiles, entry.receipt.Path), entry.before); e != nil {
				return e
			}
		}
	}
	receiptPath := filepath.Join(backup, "receipt.json")
	save := func() error { return controllerWrite(receiptPath, receipt) }
	if e = save(); e != nil {
		return e
	}
	applied := []*applyEntry{}
	createdInfo := map[string]fs.FileInfo{}
	defer func() {
		if ret != nil {
			receipt.Error = ret.Error()
			receipt.Status = "ROLLED_BACK"
			for i := len(applied) - 1; i >= 0; i-- {
				entry := applied[i]
				if err := rollbackApplyEntry(entry); err != nil {
					receipt.RollbackErrors = append(receipt.RollbackErrors, entry.receipt.Path+": "+err.Error())
				}
			}
		}
		for _, entry := range entries {
			if entry.parent != nil {
				if entry.temp != "" {
					_ = entry.parent.Remove(entry.temp)
				}
				_ = entry.parent.Close()
			}
		}
		if ret != nil {
			for i := len(receipt.CreatedDirectories) - 1; i >= 0; i-- {
				name := receipt.CreatedDirectories[i]
				current, err := repo.Lstat(name)
				if os.IsNotExist(err) {
					continue
				}
				if err != nil || !current.IsDir() || createdInfo[name] == nil || !os.SameFile(current, createdInfo[name]) {
					receipt.RollbackErrors = append(receipt.RollbackErrors, "created directory changed externally: "+name)
					continue
				}
				if err := repo.Remove(name); err != nil && !os.IsNotExist(err) {
					receipt.RollbackErrors = append(receipt.RollbackErrors, "directory "+name+": "+err.Error())
				}
			}
			if len(receipt.RollbackErrors) > 0 {
				receipt.Status = "ROLLBACK_UNCERTAIN"
				ret = fmt.Errorf("%w; rollback uncertain: %s", ret, strings.Join(receipt.RollbackErrors, "; "))
			}
		}
		receipt.Finished = time.Now().UTC().Format(time.RFC3339Nano)
		if err := save(); err != nil {
			ret = errors.Join(ret, fmt.Errorf("apply receipt persistence failed: %w", err))
		}
	}()
	for _, entry := range entries {
		if e = ensureApplyParents(repo, filepath.Dir(entry.receipt.Path), &receipt.CreatedDirectories, createdInfo); e != nil {
			return e
		}
		entry.parent, e = repo.OpenRoot(filepath.Dir(entry.receipt.Path))
		if e != nil {
			return e
		}
		entry.temp, e = stageApplyFile(entry.parent, entry.after, fs.FileMode(entry.receipt.Mode))
		if e != nil {
			return e
		}
	}
	if e = save(); e != nil {
		return e
	}
	// Staging may have taken time: re-check the complete set before committing
	// even the first file, then again immediately before each atomic rename.
	for _, entry := range entries {
		if e = verifyApplyEntry(repo, entry); e != nil {
			return e
		}
	}
	for _, entry := range entries {
		if hook != nil {
			if e = hook("before_commit", entry.receipt.Path); e != nil {
				return e
			}
		}
		if e = verifyApplyEntry(repo, entry); e != nil {
			return e
		}
		if e = entry.parent.Rename(entry.temp, entry.base); e != nil {
			return e
		}
		entry.temp = ""
		applied = append(applied, entry)
		receipt.Applied = append(receipt.Applied, entry.receipt.Path)
		if e = syncApplyRoot(entry.parent); e != nil {
			return e
		}
		if e = save(); e != nil {
			return e
		}
	}
	receipt.Status = "APPLIED"
	if e = save(); e != nil {
		return e
	}
	t.Applied = true
	t.FinalResponse = "Verified patch applied to the source repository. Original bytes and modes are preserved in the apply backup."
	if strings.HasPrefix(t.Error, "PASS but apply refused: ") {
		t.Error = ""
	}
	return nil
}

func checkApplyPath(root *os.Root, name string, existed bool) error {
	if e := cleanRel(name); e != nil {
		return e
	}
	parts := strings.Split(name, "/")
	for i := range parts {
		p := strings.Join(parts[:i+1], "/")
		info, e := root.Lstat(p)
		if os.IsNotExist(e) {
			if existed {
				return fmt.Errorf("source disappeared: %s", name)
			}
			return nil
		}
		if e != nil {
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink apply path refused: %s", p)
		}
		if i < len(parts)-1 && !info.IsDir() {
			return fmt.Errorf("non-directory apply parent: %s", p)
		}
		if i == len(parts)-1 && !info.Mode().IsRegular() {
			return fmt.Errorf("apply target is not regular: %s", name)
		}
	}
	return nil
}
func readRootRegular(root *os.Root, name string, limit int) ([]byte, fs.FileInfo, error) {
	f, e := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if e != nil {
		return nil, nil, e
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil {
		return nil, nil, e
	}
	if !info.Mode().IsRegular() || info.Size() > int64(limit) {
		return nil, nil, fmt.Errorf("not small regular file: %s", name)
	}
	b, e := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if e == nil && len(b) > limit {
		e = errors.New("file grew beyond limit")
	}
	return b, info, e
}
func readApplySource(root *os.Root, name string, existed bool, mode fs.FileMode, want []byte) ([]byte, error) {
	info, e := root.Lstat(name)
	if !existed {
		if os.IsNotExist(e) {
			return nil, nil
		}
		if e != nil {
			return nil, e
		}
		return nil, fmt.Errorf("new file appeared since task: %s", name)
	}
	if e != nil {
		return nil, e
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("apply target changed type: %s", name)
	}
	now, opened, e := readRootRegular(root, name, len(want)+1)
	if e != nil {
		return nil, e
	}
	if !os.SameFile(info, opened) || opened.Mode().Perm() != mode.Perm() || !bytes.Equal(now, want) {
		return nil, fmt.Errorf("source bytes/mode changed since task: %s", name)
	}
	return now, nil
}
func ensureApplyParents(root *os.Root, parent string, created *[]string, createdInfo map[string]fs.FileInfo) error {
	if parent == "." {
		return nil
	}
	parts := strings.Split(parent, "/")
	for i := range parts {
		name := strings.Join(parts[:i+1], "/")
		info, e := root.Lstat(name)
		if os.IsNotExist(e) {
			e = root.Mkdir(name, 0755)
			if e == nil {
				*created = append(*created, name)
				createdInfo[name], e = root.Lstat(name)
				if e != nil {
					return e
				}
				continue
			}
			if !os.IsExist(e) {
				return e
			}
			info, e = root.Lstat(name)
		}
		if e != nil {
			return e
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe apply parent: %s", name)
		}
	}
	return nil
}
func stageApplyFile(parent *os.Root, b []byte, mode fs.FileMode) (string, error) {
	name := ".strixglm-apply-" + id()
	f, e := parent.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0600)
	if e != nil {
		return "", e
	}
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = parent.Remove(name)
		}
	}()
	if _, e = f.Write(b); e != nil {
		return "", e
	}
	if e = f.Chmod(mode.Perm()); e != nil {
		return "", e
	}
	if e = f.Sync(); e != nil {
		return "", e
	}
	if e = f.Close(); e != nil {
		return "", e
	}
	ok = true
	return name, nil
}
func syncApplyRoot(root *os.Root) error {
	f, e := root.Open(".")
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
func verifyApplyEntry(repo *os.Root, entry *applyEntry) error {
	if e := checkApplyPath(repo, entry.receipt.Path, entry.receipt.Existed); e != nil {
		return e
	}
	current, e := repo.Lstat(filepath.Dir(entry.receipt.Path))
	if e != nil {
		return e
	}
	pinned, e := entry.parent.Stat(".")
	if e != nil {
		return e
	}
	if !os.SameFile(current, pinned) {
		return fmt.Errorf("apply parent changed: %s", entry.receipt.Path)
	}
	_, e = readApplySource(entry.parent, entry.base, entry.receipt.Existed, fs.FileMode(entry.receipt.Mode), entry.before)
	return e
}
func rollbackApplyEntry(entry *applyEntry) error {
	current, info, e := readRootRegular(entry.parent, entry.base, len(entry.after)+1)
	if e != nil {
		return e
	}
	if !bytes.Equal(current, entry.after) || info.Mode().Perm() != fs.FileMode(entry.receipt.Mode) {
		return errors.New("applied file changed externally; refusing destructive rollback")
	}
	if !entry.receipt.Existed {
		if e = entry.parent.Remove(entry.base); e != nil {
			return e
		}
		return syncApplyRoot(entry.parent)
	}
	temp, e := stageApplyFile(entry.parent, entry.before, fs.FileMode(entry.receipt.Mode))
	if e != nil {
		return e
	}
	defer entry.parent.Remove(temp)
	if e = entry.parent.Rename(temp, entry.base); e != nil {
		return e
	}
	return syncApplyRoot(entry.parent)
}
