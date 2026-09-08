package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// sandboxCommand intentionally re-execs the running binary. Preserve that
// entrypoint when it is the go test binary; this never invokes a model/runtime.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "sandbox-exec" {
		if err := sandboxEntry(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(126)
		}
		return
	}
	os.Exit(m.Run())
}

func coreConfig(t *testing.T) Config {
	t.Helper()
	root := t.TempDir()
	state := filepath.Join(root, "state")
	if e := os.Mkdir(state, 0700); e != nil {
		t.Fatal(e)
	}
	return Config{StateDir: state, WorkspaceRoots: []string{root}, PairLock: filepath.Join(root, "pair.lock"), Model: "mock", ModelTimeout: 10, SandboxTimeout: 15, SandboxMemoryBytes: 1 << 30, SandboxTasks: 64, MaxFiles: 100, MaxFileBytes: 262144, MaxRepoBytes: 1 << 20, DefaultProfile: "fast", MaxContextTokens: 50000, Profiles: map[string]Profile{"fast": {Reasoning: "low", ContextTokens: 8192, MaxTokens: 512, MaxRepairs: 2}}}
}

func TestCoreParseFilesPolicy(t *testing.T) {
	allowed := map[string]bool{"src/main.cpp": true}
	for _, input := range []string{`{"files":{"tests/hidden.cpp":"bad"}}`, `{"files":{"../escape":"bad"}}`, `{"files":{}}`, `{"files":{"src/main.cpp":"x"}} trailing`, `{"files":{"src/main.cpp":"\u0000"}}`} {
		if _, e := parseFiles(input, allowed); e == nil {
			t.Errorf("accepted unsafe/invalid update: %s", input)
		}
	}
	got, e := parseFiles("```json\n{\"files\":{\"src/main.cpp\":\"int main(){}\\n\"}}\n```", allowed)
	if e != nil || string(got["src/main.cpp"]) != "int main(){}\n" {
		t.Fatalf("valid final JSON: %v %v", got, e)
	}
}

func TestCorePrepareWorkspaceDoesNotLeakHidden(t *testing.T) {
	c := coreConfig(t)
	repo := filepath.Join(c.WorkspaceRoots[0], "repo")
	if e := writeFiles(repo, map[string][]byte{"src/main.cpp": []byte("int f(){return 1;}\n"), "notes.txt": []byte("irrelevant")}); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(repo, ".env"), []byte("SECRET_ENV"), 0600); e != nil {
		t.Fatal(e)
	}
	secret := filepath.Join(c.WorkspaceRoots[0], "oracle.cpp")
	if e := os.WriteFile(secret, []byte("HIDDEN_ORACLE_SENTINEL"), 0600); e != nil {
		t.Fatal(e)
	}
	s := TaskSpec{Repo: repo, Task: "fix f", Files: []string{"src/main.cpp"}, AllowedPaths: []string{"src/main.cpp"}, TestFiles: map[string]string{"verify.cpp": secret}}
	w, e := prepareWorkspace(c, s, c.Profiles["fast"])
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range w.Selected {
		if strings.Contains(string(w.Original[name]), "HIDDEN_ORACLE_SENTINEL") || strings.Contains(string(w.Original[name]), "SECRET_ENV") {
			t.Fatal("secret leaked into selected prompt")
		}
	}
	if string(w.Hidden["verify.cpp"]) != "HIDDEN_ORACLE_SENTINEL" {
		t.Fatal("missing separate oracle")
	}
	s.AllowedPaths = append(s.AllowedPaths, "verify.cpp")
	if _, e := prepareWorkspace(c, s, c.Profiles["fast"]); e == nil {
		t.Fatal("hidden/editable overlap accepted")
	}
}

func TestCorePrepareWorkspaceRejectsSymlink(t *testing.T) {
	c := coreConfig(t)
	repo := filepath.Join(c.WorkspaceRoots[0], "repo")
	if e := writeFiles(repo, map[string][]byte{"main.cpp": []byte("ok")}); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink("main.cpp", filepath.Join(repo, "alias.cpp")); e != nil {
		t.Fatal(e)
	}
	_, e := prepareWorkspace(c, TaskSpec{Repo: repo, Task: "fix", AllowedPaths: []string{"main.cpp"}}, c.Profiles["fast"])
	if e == nil {
		t.Fatal("source symlink accepted")
	}
}

func TestCoreSSEAndMetrics(t *testing.T) {
	stream := "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"think\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"final\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":26,\"completion_tokens_details\":{\"reasoning_tokens\":6}},\"metrics\":{\"generation_time_ms\":1000,\"time_to_first_token_ms\":50,\"speculative_decoding\":{\"acceptance_rate\":0.8}}}\n\ndata: [DONE]\n\n"
	var result ModelResult
	if e := consumeSSE(strings.NewReader(stream), time.Now(), nil, &result); e != nil {
		t.Fatal(e)
	}
	if result.Content != "final" || result.Reasoning != "think" || !result.StreamComplete || result.FinishReason != "stop" {
		t.Fatalf("bad response: %+v", result)
	}
	m := result.Metrics
	if m.PromptTokens != 100 || m.ReasoningTokens != 6 || m.FinalTokens != 20 || m.DecodeTPS == nil || *m.DecodeTPS != 25 || m.TTFTMS == nil || m.Acceptance == nil || *m.Acceptance != 0.8 {
		t.Fatalf("bad metrics: %+v", m)
	}
	for _, bad := range []string{"data: {bad}\n\n", "data: {\"error\":{\"message\":\"fail\"}}\n\n", "data: {\"choices\":[]}\n\n"} {
		var r ModelResult
		if e := consumeSSE(strings.NewReader(bad), time.Now(), nil, &r); e == nil {
			t.Fatal("malformed/truncated stream accepted")
		}
	}
}

func TestCoreActualCIRUMetricsShape(t *testing.T) {
	// Field names and numbers from the first live qualification response. No
	// source/runtime access or request is needed for this offline regression.
	var event map[string]any
	if e := json.Unmarshal([]byte(`{"usage":{"prompt_tokens":855,"completion_tokens":536,"completion_tokens_details":{"reasoning_tokens":48}},"metrics":{"generation_time_ms":19553.89065899908,"time_to_first_token_ms":2291.2792549996084,"tokens_per_second":24.536316362388362,"speculative_decoding":{"draft_acceptance_rate":0.809433962264151,"num_accepted_draft_tokens":429,"num_draft_tokens":530}}}`), &event); e != nil {
		t.Fatal(e)
	}
	var m Metrics
	parseMetrics(&m, event)
	if m.FinalTokens != 488 || m.Acceptance == nil || *m.Acceptance != 429.0/530.0 || m.DecodeTPS == nil || *m.DecodeTPS < 27.3602 || *m.DecodeTPS > 27.3604 {
		t.Fatalf("CIRU metric regression: %+v", m)
	}
	if *m.DecodeTPS == 24.536316362388362 {
		t.Fatal("HTTP/server tokens_per_second mislabeled decode")
	}
}

func TestCoreSandboxCompileIsolationAndFreshness(t *testing.T) {
	c := coreConfig(t)
	files := map[string][]byte{"main.cpp": []byte(`#include <fstream>
#include <sys/socket.h>
#include <arpa/inet.h>
#include <unistd.h>
int main(){
  std::ifstream secret("/home/funboy/STRIX_CLUSTER_ACCELERATION_PLAN.md");if(secret)return 10;
  std::ofstream mutation("oracle.txt");if(mutation)return 11;
  int s=socket(AF_INET,SOCK_STREAM,0);sockaddr_in a{};a.sin_family=AF_INET;a.sin_port=htons(18091);inet_pton(AF_INET,"127.0.0.1",&a.sin_addr);if(connect(s,(sockaddr*)&a,sizeof(a))==0)return 12;close(s);
  std::ifstream marker("marker");if(marker)return 13;
  std::ofstream output("marker");output<<"created";return 0;
}
`)}
	for i := 0; i < 2; i++ {
		build, check, e := runChecks(context.Background(), c, files, map[string][]byte{"oracle.txt": []byte("immutable")}, []string{"g++", "-std=c++17", "main.cpp", "-o", "app"}, []string{"./app"})
		if e != nil || build == nil || !build.Passed || check == nil || !check.Passed {
			t.Fatalf("fresh sandbox %d: build=%+v test=%+v error=%v", i, build, check, e)
		}
	}
	build, check, e := runChecks(context.Background(), c, map[string][]byte{"bad.cpp": []byte("not valid C++")}, nil, []string{"g++", "bad.cpp", "-o", "app"}, []string{"./app"})
	if e != nil || build == nil || build.Passed || check != nil {
		t.Fatalf("compiler failure must not run tests: %+v %+v %v", build, check, e)
	}
}

func TestCoreSandboxHiddenParentSymlinkCannotWriteHost(t *testing.T) {
	c := coreConfig(t)
	outside := filepath.Join(c.WorkspaceRoots[0], "sentinel")
	if e := os.Mkdir(outside, 0700); e != nil {
		t.Fatal(e)
	}
	sentinel := filepath.Join(outside, "oracle.txt")
	if e := os.WriteFile(sentinel, []byte("HOST_UNCHANGED"), 0600); e != nil {
		t.Fatal(e)
	}
	// The untrusted build cannot access outside. A symlink can nevertheless name
	// that host path; the next host-side staging pass must never follow it.
	cmd := []string{"sh", "-c", "mv tests tests-original && ln -s '" + outside + "' tests"}
	build, check, err := runChecks(context.Background(), c, map[string][]byte{"main.cpp": []byte("unused")}, map[string][]byte{"tests/oracle.txt": []byte("hidden")}, cmd, []string{"true"})
	t.Logf("adversarial build=%+v test=%+v error=%v", build, check, err)
	got, e := os.ReadFile(sentinel)
	if e != nil || string(got) != "HOST_UNCHANGED" {
		t.Fatalf("HOST_WRITE_ESCAPE: untrusted build changed external sentinel to %q (%v)", got, e)
	}
}

func TestCoreGenerateCancellationDrainsMockOnly(t *testing.T) {
	c := coreConfig(t)
	started := make(chan struct{})
	release := make(chan struct{})
	var completed atomic.Bool
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n")
		w.(http.Flusher).Flush()
		close(started)
		<-release
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		completed.Store(true)
	}))
	defer backend.Close()
	c.Backend = backend.URL
	a, e := newApp(c)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, e := a.generate(ctx, map[string]any{"model": "mock", "messages": []any{}}, filepath.Join(c.StateDir, "mock"))
		done <- e
	}()
	<-started
	cancel()
	select {
	case e := <-done:
		t.Fatalf("transport returned before paired drain: %v", e)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	if !completed.Load() {
		t.Fatal("backend did not finish")
	}
}

func TestCoreHealthMalformedBackendNeverPanics(t *testing.T) {
	c := coreConfig(t)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503); fmt.Fprint(w, "backend unavailable") }))
	defer backend.Close()
	c.Backend = backend.URL
	a, e := newApp(c)
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("health panicked on malformed 503: %v", r)
		}
	}()
	h := a.health(context.Background())
	b, _ := json.Marshal(h)
	if h["status"] != "degraded" {
		t.Fatalf("not degraded: %s", b)
	}
}

func TestCoreAgentRepairAndIndependentVerificationMockOnly(t *testing.T) {
	c := coreConfig(t)
	repo := filepath.Join(c.WorkspaceRoots[0], "repo")
	if e := writeFiles(repo, map[string][]byte{"main.cpp": []byte("int result(){return -1;}\n")}); e != nil {
		t.Fatal(e)
	}
	hidden := filepath.Join(c.WorkspaceRoots[0], "verify.cpp")
	if e := os.WriteFile(hidden, []byte("// ORACLE_PRIVATE_SENTINEL\n#include <cstdio>\nint result();int main(){if(result()!=42){std::fprintf(stderr,\"incorrect result: expected 42\\n\");return 1;}return 0;}\n"), 0600); e != nil {
		t.Fatal(e)
	}
	var calls atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if e := json.NewDecoder(r.Body).Decode(&request); e != nil {
			t.Error(e)
		}
		serialized, _ := json.Marshal(request)
		if strings.Contains(string(serialized), "ORACLE_PRIVATE_SENTINEL") {
			t.Error("hidden oracle leaked into model request")
		}
		call := calls.Add(1)
		answer := "int result(){return 0;}\n"
		if call > 1 {
			answer = "int result(){return 42;}\n"
			if !strings.Contains(string(serialized), "incorrect result: expected 42") {
				t.Error("repair omitted test failure")
			}
		}
		content, _ := json.Marshal(map[string]any{"files": map[string]string{"main.cpp": answer}})
		event, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]string{"content": string(content)}, "finish_reason": "stop"}}, "usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 20, "completion_tokens_details": map[string]int{"reasoning_tokens": 2}}})
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", event)
	}))
	defer backend.Close()
	c.Backend = backend.URL
	a, e := newApp(c)
	if e != nil {
		t.Fatal(e)
	}
	task, e := a.submit(TaskSpec{Repo: repo, Task: "Return the documented answer 42", AllowedPaths: []string{"main.cpp"}, BuildCommand: Command{"g++", "main.cpp", "verify.cpp", "-o", "app"}, TestCommand: Command{"./app"}, TestFiles: map[string]string{"verify.cpp": hidden}})
	if e != nil {
		t.Fatal(e)
	}
	select {
	case <-task.done:
	case <-time.After(20 * time.Second):
		task.cancel()
		t.Fatal("mock repair did not finish")
	}
	if task.Status != "PASS" || task.ModelCalls != 2 || len(task.Attempts) != 2 || task.Attempts[0].Status != "FAIL" || task.Attempts[1].Status != "PASS" {
		t.Fatalf("bad repair: %+v", task.snapshot())
	}
	independent, e := os.ReadFile(filepath.Join(task.output, "independent-check.json"))
	if e != nil || !strings.Contains(string(independent), `"passed": true`) {
		t.Fatalf("missing independent verification: %s %v", independent, e)
	}
	before, e := os.ReadFile(filepath.Join(repo, "main.cpp"))
	if e != nil || string(before) != "int result(){return -1;}\n" {
		t.Fatal("agent modified original without apply")
	}
	if !strings.Contains(task.Diff, "+int result(){return 42;}") {
		t.Fatal("missing tested patch")
	}
	if e := os.WriteFile(filepath.Join(repo, "main.cpp"), []byte("user concurrent edit"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := a.applyTask(task); e == nil {
		t.Fatal("apply accepted changed original")
	}
}

func TestCoreAgentIncompleteDoesNotExecuteReasoningMockOnly(t *testing.T) {
	c := coreConfig(t)
	repo := filepath.Join(c.WorkspaceRoots[0], "repo")
	if e := writeFiles(repo, map[string][]byte{"main.cpp": []byte("int main(){return 1;}")}); e != nil {
		t.Fatal(e)
	}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"{\\\"files\\\":{\\\"main.cpp\\\":\\\"int main(){return 0;}\\\"}}\"},\"finish_reason\":\"length\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer backend.Close()
	c.Backend = backend.URL
	a, e := newApp(c)
	if e != nil {
		t.Fatal(e)
	}
	task, e := a.submit(TaskSpec{Repo: repo, Task: "fix main", AllowedPaths: []string{"main.cpp"}, TestCommand: Command{"true"}})
	if e != nil {
		t.Fatal(e)
	}
	select {
	case <-task.done:
	case <-time.After(5 * time.Second):
		task.cancel()
		t.Fatal("mock did not finish")
	}
	if task.Status != "INCOMPLETE" || task.ModelCalls != 1 || task.Attempts[0].Build != nil || task.Attempts[0].Tests != nil {
		t.Fatalf("executed reasoning as source: %+v", task.snapshot())
	}
}

func TestCoreExecutableModesPreservedInFreshChecks(t *testing.T) {
	c := coreConfig(t)
	repo := filepath.Join(c.WorkspaceRoots[0], "repo")
	sources := map[string][]byte{
		"build.sh": []byte("#!/bin/sh\nset -eu\ng++ main.cpp -o app\n"),
		"test.sh":  []byte("#!/bin/sh\nset -eu\ntest ! -e marker\n./app\nprintf verified > marker\n"),
		"main.cpp": []byte("int main(){return 0;}\n"),
	}
	if e := writeFiles(repo, sources); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"build.sh", "test.sh"} {
		if e := os.Chmod(filepath.Join(repo, name), 0755); e != nil {
			t.Fatal(e)
		}
	}
	w, e := prepareWorkspace(c, TaskSpec{Repo: repo, Task: "build existing scripts", Files: []string{"main.cpp", "build.sh", "test.sh"}, AllowedPaths: []string{"main.cpp"}}, c.Profiles["fast"])
	if e != nil {
		t.Fatal(e)
	}
	if w.Modes["build.sh"] != 0755 || w.Modes["test.sh"] != 0755 || w.Modes["main.cpp"] != 0600 {
		t.Fatalf("bad mode capture: %v", w.Modes)
	}
	for i := 0; i < 2; i++ {
		b, v, e := runChecks(context.Background(), c, w.Original, nil, []string{"./build.sh"}, []string{"./test.sh"}, w.Modes)
		if e != nil || b == nil || !b.Passed || v == nil || !v.Passed {
			t.Fatalf("exec scripts lost in snapshot %d: build=%+v test=%+v err=%v", i, b, v, e)
		}
	}
	original, e := os.Stat(filepath.Join(repo, "build.sh"))
	if e != nil || original.Mode().Perm() != 0755 {
		t.Fatal("original mode changed")
	}
}

func TestCoreNewFilesRemainPrivateAndSpecialBitsRemoved(t *testing.T) {
	root := t.TempDir()
	files := map[string][]byte{"script.sh": []byte("#!/bin/sh\n"), "new.txt": []byte("private")}
	modes := map[string]fs.FileMode{"script.sh": 0755 | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky}
	if e := writeFilesWithModes(root, files, modes); e != nil {
		t.Fatal(e)
	}
	script, e := os.Stat(filepath.Join(root, "script.sh"))
	if e != nil {
		t.Fatal(e)
	}
	if script.Mode().Perm() != 0755 || script.Mode()&(fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky) != 0 {
		t.Fatalf("unsafe script mode: %v", script.Mode())
	}
	fresh, e := os.Stat(filepath.Join(root, "new.txt"))
	if e != nil || fresh.Mode().Perm() != 0600 {
		t.Fatalf("new file not private: %v %v", fresh, e)
	}
}

func TestCoreExplicitEmptyFileIsVerifiedAndApplicableMockOnly(t *testing.T) {
	c := coreConfig(t)
	repo := filepath.Join(c.WorkspaceRoots[0], "repo")
	if e := writeFiles(repo, map[string][]byte{"README.md": []byte("Create one empty marker, leave other allowed paths absent.\n")}); e != nil {
		t.Fatal(e)
	}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		event, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]string{"content": `{"files":{"created-empty.txt":""}}`}, "finish_reason": "stop"}}})
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", event)
	}))
	defer backend.Close()
	c.Backend = backend.URL
	a, e := newApp(c)
	if e != nil {
		t.Fatal(e)
	}
	task, e := a.submit(TaskSpec{Repo: repo, Task: "Create only created-empty.txt as an empty marker", AllowedPaths: []string{"created-empty.txt", "untouched.txt"}, TestCommand: Command{"sh", "-c", "test -f created-empty.txt && test ! -s created-empty.txt && test ! -e untouched.txt"}})
	if e != nil {
		t.Fatal(e)
	}
	select {
	case <-task.done:
	case <-time.After(10 * time.Second):
		task.cancel()
		t.Fatal("mock empty-file task did not finish")
	}
	if task.Status != "PASS" || task.ModelCalls != 1 || len(task.FilesChanged) != 1 || task.FilesChanged[0] != "created-empty.txt" {
		t.Fatalf("new empty file not delivered: %+v", task.snapshot())
	}
	if !strings.Contains(task.Diff, "new file mode") || !strings.Contains(task.Diff, "created-empty.txt") || strings.Contains(task.Diff, "untouched.txt") {
		t.Fatalf("bad empty-file diff: %s", task.Diff)
	}
	if task.workspace.Verified["created-empty.txt"] != applyHash(nil) {
		t.Fatal("missing verified SHA(empty)")
	}
	if _, e := os.Stat(filepath.Join(repo, "created-empty.txt")); !os.IsNotExist(e) {
		t.Fatal("original changed without explicit apply")
	}
	if e := a.applyTask(task); e != nil {
		t.Fatal(e)
	}
	info, e := os.Stat(filepath.Join(repo, "created-empty.txt"))
	if e != nil || info.Size() != 0 || info.Mode().Perm() != 0600 {
		t.Fatalf("new empty file not applied privately: %v %v", info, e)
	}
	if _, e := os.Stat(filepath.Join(repo, "untouched.txt")); !os.IsNotExist(e) {
		t.Fatal("unproduced allowed file created")
	}
}

func TestCoreAutomaticApplyIsNonterminalUntilCommit(t *testing.T) {
	a, task := applyFixture(t, map[string]string{"file.txt": "before"}, map[string]string{"file.txt": "after"}, nil)
	task.Status = "running"
	task.spec.Apply = true
	task.created = time.Now()
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		a.finishVerifiedTask(task, func(stage, name string) error {
			if stage == "before_commit" {
				if task.Status != "applying" {
					t.Errorf("premature status inside apply: %s", task.Status)
				}
				close(entered)
				<-release
			}
			return nil
		})
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("apply did not reach commit")
	}
	saved, e := os.ReadFile(filepath.Join(task.output, "result.json"))
	if e != nil {
		close(release)
		<-done
		t.Fatal(e)
	}
	var snapshot map[string]any
	if e = json.Unmarshal(saved, &snapshot); e != nil {
		t.Error(e)
	}
	if snapshot["status"] != "applying" || snapshot["applied"] != false {
		t.Errorf("premature terminal snapshot: %s", saved)
	}
	before, e := os.ReadFile(filepath.Join(task.workspace.Repo, "file.txt"))
	if e != nil || string(before) != "before" {
		t.Error("source changed before commit")
	}
	close(release)
	<-done
	if task.Status != "PASS" || !task.Applied || task.Error != "" || !strings.Contains(task.FinalResponse, "applied to the source") || strings.Contains(task.FinalResponse, "unchanged") {
		t.Fatalf("incoherent applied completion: %+v", task.snapshot())
	}
}

func TestCoreAutomaticApplyRefusalKeepsCorrectnessPass(t *testing.T) {
	a, task := applyFixture(t, map[string]string{"file.txt": "before"}, map[string]string{"file.txt": "after"}, nil)
	task.Status = "running"
	task.spec.Apply = true
	task.created = time.Now()
	a.finishVerifiedTask(task, func(stage, name string) error {
		if stage == "before_commit" {
			return errors.New("injected apply refusal")
		}
		return nil
	})
	if task.Status != "PASS" || task.Applied || !strings.Contains(task.Error, "injected apply refusal") || !strings.Contains(task.FinalResponse, "failed or was refused") {
		t.Fatalf("refusal hidden or correctness mislabeled: %+v", task.snapshot())
	}
	assertApplyBytes(t, filepath.Join(task.workspace.Repo, "file.txt"), "before")
}

func TestCorePublicApplyCannotEnterAutomaticApplyingPhase(t *testing.T) {
	a, task := applyFixture(t, map[string]string{"file.txt": "before"}, map[string]string{"file.txt": "after"}, nil)
	task.Status = "applying"
	task.spec.Apply = true
	if e := a.applyTask(task); e == nil {
		t.Fatal("public apply entered an active automatic transaction")
	}
	task.spec.Apply = false
	if e := a.applyTaskInternal(task, nil, true); e == nil {
		t.Fatal("private applying path accepted without explicit original opt-in")
	}
	assertApplyBytes(t, filepath.Join(task.workspace.Repo, "file.txt"), "before")
}
