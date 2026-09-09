package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func protectedSession(t *testing.T, files map[string][]byte, opts WorkspaceProtectionOptions) (*App, *PiWorkspaceSession, string) {
	t.Helper()
	a, project := workspaceTestApp(t)
	if e := writeFiles(project, files); e != nil {
		t.Fatal(e)
	}
	s, e := workspacesFor(a).createConfigured("local", project, "", "low", "", opts)
	if e != nil {
		t.Fatal(e)
	}
	return a, s, project
}

func waitProtectedVerification(t *testing.T, s *PiWorkspaceSession) *WorkspaceVerification {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		running := s.verificationRunning
		v := s.Verification
		s.mu.Unlock()
		if !running && v != nil && v.Status != "VERIFYING" {
			return v
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("independent verification did not finish")
	return nil
}

func TestProtectedDefaultPreservesDirtyAndUntracked(t *testing.T) {
	a, project := workspaceTestApp(t)
	before := map[string][]byte{"main.c": []byte("dirty working source\n"), "untracked.md": []byte("not in Git\n")}
	if e := writeFiles(project, before); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{".env", "api-token", "private.key"} {
		if e := os.WriteFile(filepath.Join(project, name), []byte("fake-secret-do-not-copy"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	body, _ := json.Marshal(map[string]any{"kind": "local", "root": project, "confirm": true})
	r := workspaceRequest(t, a, "POST", "/v1/workspaces/sessions", string(body))
	if r.Code != 201 {
		t.Fatal(r.Code, r.Body.String())
	}
	var got map[string]any
	_ = json.Unmarshal(r.Body.Bytes(), &got)
	if got["mode"] != "protected" || got["original_root"] != project || got["root"] == project || got["working_root"] != got["root"] {
		t.Fatal(got)
	}
	working := got["working_root"].(string)
	for name, want := range before {
		copy, e := os.ReadFile(filepath.Join(working, name))
		if e != nil || !bytes.Equal(copy, want) {
			t.Fatal(name, e, string(copy))
		}
	}
	for _, name := range []string{".env", "api-token", "private.key"} {
		if _, e := os.Stat(filepath.Join(working, name)); !os.IsNotExist(e) {
			t.Fatal("secret copied", name)
		}
	}
	if e := os.WriteFile(filepath.Join(working, "main.c"), []byte("agent edit"), 0600); e != nil {
		t.Fatal(e)
	}
	gotOriginal, _ := os.ReadFile(filepath.Join(project, "main.c"))
	if !bytes.Equal(gotOriginal, before["main.c"]) {
		t.Fatal("original modified")
	}
}

func TestProtectedExplicitDirectAndRemoteBlock(t *testing.T) {
	a, project := workspaceTestApp(t)
	m := workspacesFor(a)
	if _, e := m.createConfigured("local", project, "", "low", "", WorkspaceProtectionOptions{Mode: "direct"}); e == nil {
		t.Fatal("implicit direct allowed")
	}
	s, e := m.createConfigured("local", project, "", "low", "", WorkspaceProtectionOptions{Mode: "direct", AllowDirect: true})
	if e != nil || s.Root != project || s.Mode != "direct" {
		t.Fatal(s, e)
	}
	if _, e = m.createConfigured("ssh", "/srv/project", "unused", "low", "", WorkspaceProtectionOptions{}); e == nil || !strings.Contains(e.Error(), "BLOCKED_REMOTE_PROTECTION") {
		t.Fatal(e)
	}
	if _, e = validateProtectionOptions("ssh", WorkspaceProtectionOptions{Mode: "direct", AllowDirect: true}); e != nil {
		t.Fatal(e)
	}
}

func TestProtectedSnapshotBoundaries(t *testing.T) {
	t.Run("symlink", func(t *testing.T) {
		a, p := workspaceTestApp(t)
		if e := os.Symlink("/etc/passwd", filepath.Join(p, "escape")); e != nil {
			t.Fatal(e)
		}
		if _, e := readProtectedTree(p, a.cfg); e == nil {
			t.Fatal("symlink read")
		}
	})
	t.Run("hardlink", func(t *testing.T) {
		a, p := workspaceTestApp(t)
		f := filepath.Join(p, "a.txt")
		_ = os.WriteFile(f, []byte("x"), 0600)
		_ = os.Link(f, filepath.Join(p, "b.txt"))
		if _, e := readProtectedTree(p, a.cfg); e == nil {
			t.Fatal("hardlink read")
		}
	})
	t.Run("count", func(t *testing.T) {
		a, p := workspaceTestApp(t)
		a.cfg.MaxFiles = 1
		_ = writeFiles(p, map[string][]byte{"a": []byte("a"), "b": []byte("b")})
		if _, e := readProtectedTree(p, a.cfg); e == nil {
			t.Fatal("count quota ignored")
		}
	})
	t.Run("bytes", func(t *testing.T) {
		a, p := workspaceTestApp(t)
		a.cfg.MaxFileBytes = 8
		_ = writeFiles(p, map[string][]byte{"a": []byte("too many source bytes")})
		if _, e := readProtectedTree(p, a.cfg); e == nil {
			t.Fatal("byte quota ignored")
		}
	})
	t.Run("binary", func(t *testing.T) {
		a, p := workspaceTestApp(t)
		_ = os.WriteFile(filepath.Join(p, "compiled"), []byte{0x7f, 'E', 'L', 'F', 0, 1}, 0700)
		tree, e := readProtectedTree(p, a.cfg)
		if e != nil || len(tree.Files) != 0 || len(tree.Excluded) != 1 {
			t.Fatal(tree, e)
		}
	})
}

// Installs a synthetic receipt only for apply/conflict unit tests. The separate
// real sandbox test below validates compilation and independent command exits.
func installProtectedReceiptFixture(t *testing.T, s *PiWorkspaceSession) {
	t.Helper()
	tree, e := readProtectedTree(s.Root, s.manager.app.cfg)
	if e != nil {
		t.Fatal(e)
	}
	v := &WorkspaceVerification{ID: id(), Status: "TEST_PASS", Origin: "unit_fixture_not_execution", Completion: "NATURAL", CandidateSHA256: protectedTreeHash(tree), FileSHA256: map[string]string{}}
	for n, b := range tree.Files {
		v.FileSHA256[n] = applyHash(b)
	}
	if e = writeFilesWithModes(filepath.Join(s.dir, "verification", v.ID, "verified"), tree.Files, tree.Modes); e != nil {
		t.Fatal(e)
	}
	s.mu.Lock()
	s.Verification = v
	s.mu.Unlock()
}

func TestProtectedApplyExactVerifiedCandidate(t *testing.T) {
	_, s, p := protectedSession(t, map[string][]byte{"main.c": []byte("user dirty original\n"), "notes.md": []byte("untracked original\n")}, WorkspaceProtectionOptions{})
	_ = os.WriteFile(filepath.Join(s.Root, "main.c"), []byte("verified candidate\n"), 0600)
	_ = os.WriteFile(filepath.Join(s.Root, "new.c"), []byte("new source\n"), 0600)
	installProtectedReceiptFixture(t, s)
	review, e := s.protectionReview()
	if e != nil || !strings.Contains(review["diff"].(string), "verified candidate") {
		t.Fatal(review, e)
	}
	if e = s.applyProtection(); e != nil {
		t.Fatal(e)
	}
	got, _ := os.ReadFile(filepath.Join(p, "main.c"))
	if string(got) != "verified candidate\n" {
		t.Fatal(string(got))
	}
	untouched, _ := os.ReadFile(filepath.Join(p, "notes.md"))
	if string(untouched) != "untracked original\n" {
		t.Fatal("untracked file changed")
	}
	if !s.Verification.Applied {
		t.Fatal("missing apply receipt")
	}
	if e = s.applyProtection(); e == nil {
		t.Fatal("duplicate apply accepted")
	}
	backups, e := filepath.Glob(filepath.Join(s.dir, "verification", s.Verification.ID, "apply-backup", "*", "files", "main.c"))
	if e != nil || len(backups) != 1 {
		t.Fatal(backups, e)
	}
	before, _ := os.ReadFile(backups[0])
	if string(before) != "user dirty original\n" {
		t.Fatal("wrong backup", string(before))
	}
}

func TestProtectedApplyFailsClosed(t *testing.T) {
	for _, kind := range []string{"original_changed", "candidate_changed", "artifact_changed", "failed_test", "deleted", "mode_changed", "new_original_file"} {
		t.Run(kind, func(t *testing.T) {
			_, s, p := protectedSession(t, map[string][]byte{"main.c": []byte("original\n"), "keep.md": []byte("keep\n")}, WorkspaceProtectionOptions{})
			_ = os.WriteFile(filepath.Join(s.Root, "main.c"), []byte("candidate\n"), 0600)
			installProtectedReceiptFixture(t, s)
			switch kind {
			case "original_changed":
				_ = os.WriteFile(filepath.Join(p, "keep.md"), []byte("external edit\n"), 0600)
			case "new_original_file":
				_ = os.WriteFile(filepath.Join(p, "new.md"), []byte("external addition\n"), 0600)
			case "candidate_changed":
				_ = os.WriteFile(filepath.Join(s.Root, "main.c"), []byte("untested\n"), 0600)
			case "artifact_changed":
				_ = os.WriteFile(filepath.Join(s.dir, "verification", s.Verification.ID, "verified", "main.c"), []byte("tampered\n"), 0600)
			case "failed_test":
				s.Verification.Status = "FAIL"
			case "deleted":
				_ = os.Remove(filepath.Join(s.Root, "keep.md"))
				installProtectedReceiptFixture(t, s)
			case "mode_changed":
				_ = os.Chmod(filepath.Join(s.Root, "main.c"), 0700)
				installProtectedReceiptFixture(t, s)
			}
			if e := s.applyProtection(); e == nil {
				t.Fatal("unsafe apply accepted")
			}
			got, _ := os.ReadFile(filepath.Join(p, "main.c"))
			if string(got) != "original\n" {
				t.Fatal("original changed on refused apply")
			}
		})
	}
}

func TestProtectedVerificationRealSandbox(t *testing.T) {
	_, s, p := protectedSession(t, map[string][]byte{"main.c": []byte("#include <stdio.h>\nint main(void){puts(\"41\");return 0;}\n")}, WorkspaceProtectionOptions{BuildCommand: Command{"gcc", "-std=c11", "-Wall", "-Wextra", "-Werror", "main.c", "-o", "main"}, TestCommand: Command{"/bin/sh", "-c", "test \"$(./main)\" = 42"}})
	if e := s.beginVerification("manual"); e != nil {
		t.Fatal(e)
	}
	v := waitProtectedVerification(t, s)
	if v.Status != "FAIL" || v.Build == nil || !v.Build.Passed || v.Tests == nil || v.Tests.ExitCode == 0 {
		t.Fatalf("wrong negative result: %+v build=%+v tests=%+v", v, v.Build, v.Tests)
	}
	if e := s.applyProtection(); e == nil {
		t.Fatal("test failure applied")
	}
	correct := []byte("#include <stdio.h>\nint main(void){puts(\"42\");return 0;}\n")
	if e := os.WriteFile(filepath.Join(s.Root, "main.c"), correct, 0600); e != nil {
		t.Fatal(e)
	}
	s.scanPi(strings.NewReader(`{"type":"agent_end","messages":[{"role":"assistant","stopReason":"stop","content":[{"type":"text","text":"tests passed"}]}]}`+"\n"), true)
	v = waitProtectedVerification(t, s)
	if v.Status != "TEST_PASS" || v.Completion != "NATURAL" || v.Build == nil || !v.Build.Passed || v.Tests == nil || !v.Tests.Passed || v.Tests.ExitCode != 0 || v.CandidateSHA256 == "" {
		t.Fatalf("wrong positive result: %+v", v)
	}
	if _, e := os.Stat(v.Receipt); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(p, "main")); !os.IsNotExist(e) {
		t.Fatal("build touched original")
	}
	if e := s.applyProtection(); e != nil {
		t.Fatal(e)
	}
	got, _ := os.ReadFile(filepath.Join(p, "main.c"))
	if !bytes.Equal(got, correct) {
		t.Fatal("verified code not applied")
	}
}

func TestProtectedAgentClaimIsNotVerification(t *testing.T) {
	_, s, _ := protectedSession(t, map[string][]byte{"main.c": []byte("int main(void){return 0;}\n")}, WorkspaceProtectionOptions{})
	s.scanPi(strings.NewReader(`{"type":"agent_end","messages":[{"role":"assistant","stopReason":"length","content":[{"type":"text","text":"All tests passed; solution is perfect"}]}]}`+"\n"), true)
	if s.Verification.Status != "INCOMPLETE" || s.verificationRunning {
		t.Fatal(s.Verification)
	}
	s.scanPi(strings.NewReader(`{"type":"agent_end","messages":[{"role":"assistant","stopReason":"stop","content":[{"type":"text","text":"All tests passed"}]}]}`+"\n"), true)
	v := waitProtectedVerification(t, s)
	if v.Status != "UNVERIFIED" || v.Tests != nil {
		t.Fatal(v)
	}
}

func TestProtectedSinglePiScopeReservation(t *testing.T) {
	a, _ := workspaceTestApp(t)
	m := workspacesFor(a)
	if e := m.reservePi("one"); e != nil {
		t.Fatal(e)
	}
	if e := m.reservePi("two"); e == nil {
		t.Fatal("second Pi scope allowed")
	}
	m.releasePi("unowned")
	if e := m.reservePi("two"); e == nil {
		t.Fatal("foreign release succeeded")
	}
	m.releasePi("one")
	if e := m.reservePi("two"); e != nil {
		t.Fatal(e)
	}
	m.releasePi("two")
}

func TestProtectedChangedTestsRequireAcknowledgement(t *testing.T) {
	_, s, _ := protectedSession(t, map[string][]byte{"main.c": []byte("before\n"), "test.sh": []byte("exit 1\n")}, WorkspaceProtectionOptions{})
	_ = os.WriteFile(filepath.Join(s.Root, "main.c"), []byte("after\n"), 0600)
	_ = os.WriteFile(filepath.Join(s.Root, "test.sh"), []byte("exit 0\n"), 0600)
	installProtectedReceiptFixture(t, s)
	after, e := readProtectedTree(s.Root, s.manager.app.cfg)
	if e != nil {
		t.Fatal(e)
	}
	s.Verification.ChangedTests = changedTestDefinitions(s.protectedOriginal, after)
	if len(s.Verification.ChangedTests) != 1 || s.Verification.ChangedTests[0] != "test.sh" {
		t.Fatal(s.Verification.ChangedTests)
	}
	if e = s.applyProtection(); e == nil || !strings.Contains(e.Error(), "allow_test_changes") {
		t.Fatal("changed tests not acknowledged", e)
	}
	if e = s.applyProtection(true); e != nil {
		t.Fatal(e)
	}
}

func TestProtectedManualVerifyAndReviewAPI(t *testing.T) {
	a, s, _ := protectedSession(t, map[string][]byte{"a.c": []byte("source\n")}, WorkspaceProtectionOptions{})
	base := "/v1/workspaces/sessions/" + s.ID
	if w := workspaceRequest(t, a, "POST", base+"/verify", `{"confirm":false}`); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if w := workspaceRequest(t, a, "POST", base+"/verify", `{"confirm":true}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if v := waitProtectedVerification(t, s); v.Status != "UNVERIFIED" {
		t.Fatal(v)
	}
	if w := workspaceRequest(t, a, "GET", base+"/review", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "candidate_sha256") {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := workspaceRequest(t, a, "POST", base+"/apply", `{"confirm":true}`); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestProtectedReviewAfterRestartNeverAdoptsRoot(t *testing.T) {
	a, s, _ := protectedSession(t, map[string][]byte{"a.c": []byte("before\n")}, WorkspaceProtectionOptions{})
	_ = os.WriteFile(filepath.Join(s.Root, "a.c"), []byte("after\n"), 0600)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if e := s.close(ctx); e != nil {
		t.Fatal(e)
	}
	workspaceManagers.Delete(a)
	m := workspacesFor(a)
	loaded := m.sessions[s.ID]
	if loaded == nil || loaded.State != "CLOSED" || loaded.protectedOriginal.Info != nil {
		t.Fatal("unsafe restart adoption")
	}
	review, e := loaded.protectionReview()
	if e != nil || !strings.Contains(review["diff"].(string), "after") {
		t.Fatal(review, e)
	}
	if e = loaded.applyProtection(); e == nil {
		t.Fatal("closed session applied")
	}
}
