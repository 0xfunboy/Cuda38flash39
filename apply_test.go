package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func applyFixture(t *testing.T, before, after map[string]string, modes map[string]fs.FileMode) (*App, *Task) {
	t.Helper()
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	output := filepath.Join(base, "result")
	if e := os.Mkdir(repo, 0755); e != nil {
		t.Fatal(e)
	}
	w := Workspace{Repo: repo, Original: map[string][]byte{}, Modes: map[string]fs.FileMode{}, Existed: map[string]bool{}, Verified: map[string]string{}, Editable: map[string]bool{}}
	for name, body := range before {
		if e := os.MkdirAll(filepath.Dir(filepath.Join(repo, name)), 0755); e != nil {
			t.Fatal(e)
		}
		mode := fs.FileMode(0644)
		if m, ok := modes[name]; ok {
			mode = m
		}
		if e := os.WriteFile(filepath.Join(repo, name), []byte(body), mode); e != nil {
			t.Fatal(e)
		}
		if e := os.Chmod(filepath.Join(repo, name), mode); e != nil {
			t.Fatal(e)
		}
		w.Original[name] = []byte(body)
		w.Modes[name] = mode
		w.Existed[name] = true
	}
	candidate := map[string][]byte{}
	task := &Task{ID: "apply-fixture", Status: "PASS", output: output}
	for name, body := range after {
		if _, exists := w.Original[name]; !exists {
			w.Original[name] = []byte{}
			w.Modes[name] = 0600
			w.Existed[name] = false
		}
		w.Editable[name] = true
		w.Verified[name] = applyHash([]byte(body))
		candidate[name] = []byte(body)
		task.FilesChanged = append(task.FilesChanged, name)
	}
	var e error
	w.RepoInfo, e = os.Lstat(repo)
	if e != nil {
		t.Fatal(e)
	}
	task.workspace = w
	if e = writeFiles(filepath.Join(output, "verified"), candidate); e != nil {
		t.Fatal(e)
	}
	return &App{}, task
}
func readApplyReceipt(t *testing.T, task *Task) ApplyReceipt {
	t.Helper()
	paths, e := filepath.Glob(filepath.Join(task.output, "apply-backup", "*", "receipt.json"))
	if e != nil || len(paths) != 1 {
		t.Fatalf("receipt paths %v: %v", paths, e)
	}
	b, e := os.ReadFile(paths[0])
	if e != nil {
		t.Fatal(e)
	}
	var v ApplyReceipt
	if e = json.Unmarshal(b, &v); e != nil {
		t.Fatal(e)
	}
	return v
}
func assertApplyBytes(t *testing.T, path, want string) {
	t.Helper()
	b, e := os.ReadFile(path)
	if e != nil || string(b) != want {
		t.Fatalf("%s: %q, %v; want %q", path, b, e, want)
	}
}

func TestApplyPreservesExecutableModeAndBackup(t *testing.T) {
	a, task := applyFixture(t, map[string]string{"run.sh": "old", "receipt.json": "keep original"}, map[string]string{"run.sh": "new", "receipt.json": "new receipt"}, map[string]fs.FileMode{"run.sh": 0755})
	if e := a.applyTask(task); e != nil {
		t.Fatal(e)
	}
	info, e := os.Stat(filepath.Join(task.workspace.Repo, "run.sh"))
	if e != nil || info.Mode().Perm() != 0755 {
		t.Fatalf("executable mode lost: %v %v", info, e)
	}
	receipt := readApplyReceipt(t, task)
	if receipt.Status != "APPLIED" || !task.Applied {
		t.Fatal("missing apply success")
	}
	assertApplyBytes(t, filepath.Join(receipt.Backup, "run.sh"), "old")
	assertApplyBytes(t, filepath.Join(receipt.Backup, "receipt.json"), "keep original")
	if e = a.applyTask(task); e == nil {
		t.Fatal("duplicate apply accepted")
	}
}
func TestApplyRejectsAllStaleBeforeAnyWrite(t *testing.T) {
	a, task := applyFixture(t, map[string]string{"z.txt": "original"}, map[string]string{"new/nested/a.txt": "created", "z.txt": "patched"}, nil)
	if e := os.WriteFile(filepath.Join(task.workspace.Repo, "z.txt"), []byte("user edit"), 0644); e != nil {
		t.Fatal(e)
	}
	if e := a.applyTask(task); e == nil {
		t.Fatal("stale source accepted")
	}
	assertApplyBytes(t, filepath.Join(task.workspace.Repo, "z.txt"), "user edit")
	if _, e := os.Lstat(filepath.Join(task.workspace.Repo, "new")); !os.IsNotExist(e) {
		t.Fatal("created parents before complete validation")
	}
}
func TestApplyRejectsModeOnlyChange(t *testing.T) {
	a, task := applyFixture(t, map[string]string{"run.sh": "old"}, map[string]string{"run.sh": "new"}, map[string]fs.FileMode{"run.sh": 0755})
	if e := os.Chmod(filepath.Join(task.workspace.Repo, "run.sh"), 0644); e != nil {
		t.Fatal(e)
	}
	if e := a.applyTask(task); e == nil {
		t.Fatal("stale mode accepted")
	}
	assertApplyBytes(t, filepath.Join(task.workspace.Repo, "run.sh"), "old")
}
func TestApplyRejectsNewEmptyFileAppearing(t *testing.T) {
	a, task := applyFixture(t, nil, map[string]string{"new.txt": "model output"}, nil)
	if e := os.WriteFile(filepath.Join(task.workspace.Repo, "new.txt"), nil, 0600); e != nil {
		t.Fatal(e)
	}
	if e := a.applyTask(task); e == nil {
		t.Fatal("existing empty file confused with missing original")
	}
	assertApplyBytes(t, filepath.Join(task.workspace.Repo, "new.txt"), "")
}
func TestApplyNestedNewFile(t *testing.T) {
	a, task := applyFixture(t, nil, map[string]string{"lib/deep/new.txt": "created"}, nil)
	if e := a.applyTask(task); e != nil {
		t.Fatal(e)
	}
	assertApplyBytes(t, filepath.Join(task.workspace.Repo, "lib/deep/new.txt"), "created")
	receipt := readApplyReceipt(t, task)
	if len(receipt.Files) != 1 || receipt.Files[0].Existed || len(receipt.CreatedDirectories) != 2 {
		t.Fatalf("incorrect new-file metadata: %+v", receipt)
	}
}
func TestApplySymlinkNeverReadsOrWritesOutside(t *testing.T) {
	for _, parent := range []bool{false, true} {
		t.Run(map[bool]string{true: "parent", false: "target"}[parent], func(t *testing.T) {
			a, task := applyFixture(t, map[string]string{"lib/file": "old"}, map[string]string{"lib/file": "new"}, nil)
			outside := t.TempDir()
			if e := os.WriteFile(filepath.Join(outside, "file"), []byte("secret"), 0600); e != nil {
				t.Fatal(e)
			}
			dest := filepath.Join(task.workspace.Repo, "lib/file")
			target := filepath.Join(outside, "file")
			if parent {
				dest = filepath.Dir(dest)
				target = outside
			}
			if e := os.Rename(dest, dest+"-saved"); e != nil {
				t.Fatal(e)
			}
			if e := os.Symlink(target, dest); e != nil {
				t.Fatal(e)
			}
			if e := a.applyTask(task); e == nil {
				t.Fatal("symlink accepted")
			}
			assertApplyBytes(t, filepath.Join(outside, "file"), "secret")
		})
	}
}
func TestApplyRootReplacementRefused(t *testing.T) {
	a, task := applyFixture(t, map[string]string{"file": "old"}, map[string]string{"file": "new"}, nil)
	outside := t.TempDir()
	if e := os.WriteFile(filepath.Join(outside, "file"), []byte("old"), 0644); e != nil {
		t.Fatal(e)
	}
	if e := os.Rename(task.workspace.Repo, task.workspace.Repo+"-saved"); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(outside, task.workspace.Repo); e != nil {
		t.Fatal(e)
	}
	if e := a.applyTask(task); e == nil {
		t.Fatal("new root accepted")
	}
	assertApplyBytes(t, filepath.Join(outside, "file"), "old")
}
func TestApplyParentSwapDuringCommitCannotEscape(t *testing.T) {
	a, task := applyFixture(t, map[string]string{"lib/file": "old"}, map[string]string{"lib/file": "new"}, nil)
	outside := t.TempDir()
	if e := os.WriteFile(filepath.Join(outside, "file"), []byte("secret"), 0644); e != nil {
		t.Fatal(e)
	}
	e := a.applyTaskWithHook(task, func(_, name string) error {
		parent := filepath.Join(task.workspace.Repo, "lib")
		if e := os.Rename(parent, parent+"-saved"); e != nil {
			return e
		}
		return os.Symlink(outside, parent)
	})
	if e == nil {
		t.Fatal("parent swap accepted")
	}
	assertApplyBytes(t, filepath.Join(outside, "file"), "secret")
	assertApplyBytes(t, filepath.Join(task.workspace.Repo, "lib-saved/file"), "old")
}
func TestApplyPartialFailureRollsBackExistenceAndModes(t *testing.T) {
	a, task := applyFixture(t, map[string]string{"b.sh": "old script", "z.txt": "old text"}, map[string]string{"a/deep/new": "new file", "b.sh": "new script", "z.txt": "new text"}, map[string]fs.FileMode{"b.sh": 0755})
	e := a.applyTaskWithHook(task, func(_, name string) error {
		if name == "z.txt" {
			return errors.New("injected commit failure")
		}
		return nil
	})
	if e == nil {
		t.Fatal("failure injection ignored")
	}
	assertApplyBytes(t, filepath.Join(task.workspace.Repo, "b.sh"), "old script")
	assertApplyBytes(t, filepath.Join(task.workspace.Repo, "z.txt"), "old text")
	if _, e = os.Lstat(filepath.Join(task.workspace.Repo, "a")); !os.IsNotExist(e) {
		t.Fatal("rollback left a new empty file/directory")
	}
	info, _ := os.Stat(filepath.Join(task.workspace.Repo, "b.sh"))
	if info.Mode().Perm() != 0755 {
		t.Fatal("rollback lost executable mode")
	}
	r := readApplyReceipt(t, task)
	if r.Status != "ROLLED_BACK" || len(r.RollbackErrors) > 0 || task.Applied {
		t.Fatalf("rollback not proven: %+v", r)
	}
}
func TestApplyRollbackRefusesConcurrentExternalEdit(t *testing.T) {
	a, task := applyFixture(t, map[string]string{"a.txt": "old a", "z.txt": "old z"}, map[string]string{"a.txt": "new a", "z.txt": "new z"}, nil)
	e := a.applyTaskWithHook(task, func(_, name string) error {
		if name == "z.txt" {
			if e := os.WriteFile(filepath.Join(task.workspace.Repo, "a.txt"), []byte("concurrent user edit"), 0644); e != nil {
				return e
			}
			return errors.New("injected failure")
		}
		return nil
	})
	if e == nil {
		t.Fatal("failure ignored")
	}
	assertApplyBytes(t, filepath.Join(task.workspace.Repo, "a.txt"), "concurrent user edit")
	r := readApplyReceipt(t, task)
	if r.Status != "ROLLBACK_UNCERTAIN" || len(r.RollbackErrors) != 1 {
		t.Fatalf("external edit not protected: %+v", r)
	}
}
func TestApplyRejectsModifiedVerifiedArtifact(t *testing.T) {
	a, task := applyFixture(t, map[string]string{"file": "old"}, map[string]string{"file": "tested"}, nil)
	if e := os.WriteFile(filepath.Join(task.output, "verified/file"), []byte("untested"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := a.applyTask(task); e == nil {
		t.Fatal("untested artifact accepted")
	}
	assertApplyBytes(t, filepath.Join(task.workspace.Repo, "file"), "old")
}
func TestMakePatchUsesFrozenExistenceModesAndLiteralContent(t *testing.T) {
	_, task := applyFixture(t, map[string]string{"run.sh": "old\n"}, map[string]string{"run.sh": "echo 'a/old/literal b/new/literal'\n", "new/n.txt": "new\n"}, map[string]fs.FileMode{"run.sh": 0755})
	if e := os.Remove(filepath.Join(task.workspace.Repo, "run.sh")); e != nil {
		t.Fatal(e)
	}
	if e := os.MkdirAll(filepath.Join(task.workspace.Repo, "new"), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(task.workspace.Repo, "new/n.txt"), []byte("user-created"), 0600); e != nil {
		t.Fatal(e)
	}
	patch, e := makePatch(task.workspace, map[string][]byte{"run.sh": []byte("echo 'a/old/literal b/new/literal'\n"), "new/n.txt": []byte("new\n")})
	if e != nil {
		t.Fatal(e)
	}
	for _, want := range []string{"new file mode 100644", "100755", "-old", "+echo 'a/old/literal b/new/literal'", "+++ b/new/n.txt"} {
		if !strings.Contains(patch, want) {
			t.Fatalf("missing %q in:\n%s", want, patch)
		}
	}
}

func TestMakePatchIsApplicableIncludingSpacesAndNewFiles(t *testing.T) {
	_, task := applyFixture(t, map[string]string{"src/a file.txt": "before\n"}, map[string]string{"src/a file.txt": "after a/old/literal\n", "new/deep/file.txt": "new\n"}, nil)
	patch, e := makePatch(task.workspace, map[string][]byte{"src/a file.txt": []byte("after a/old/literal\n"), "new/deep/file.txt": []byte("new\n")})
	if e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command("git", "apply", "--check", "-")
	cmd.Dir = task.workspace.Repo
	cmd.Stdin = strings.NewReader(patch)
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("patch not applicable: %v %s\n%s", e, out, patch)
	}
}
