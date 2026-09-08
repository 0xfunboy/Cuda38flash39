package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func cliMockConfig(t *testing.T, handler http.Handler) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-cli-token" {
			t.Error("CLI omitted configured bearer token")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	base := t.TempDir()
	state := filepath.Join(base, "state")
	if e := os.Mkdir(state, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(state, "api-token"), []byte("test-cli-token\n"), 0600); e != nil {
		t.Fatal(e)
	}
	cfg := Config{
		Listen: strings.TrimPrefix(server.URL, "http://"), Model: "fixture-model",
		StateDir: state, WorkspaceRoots: []string{base}, MaxFiles: 1,
		MaxRepoBytes: 1024, SandboxTasks: 1, ModelTimeout: 1, DefaultProfile: "fast",
		Profiles: map[string]Profile{"fast": {Reasoning: "low", ContextTokens: 4096, MaxTokens: 2048}},
	}
	path := filepath.Join(base, "config.json")
	if e := writeJSON(path, cfg); e != nil {
		t.Fatal(e)
	}
	return path
}

// These entry-point tests are intentionally serial: entry writes process stdout
// and stderr. They use private files, avoiding a pipe-size-dependent deadlock.
func captureCLIEntry(t *testing.T, args ...string) (stdout, stderr string, result error) {
	t.Helper()
	out, e := os.CreateTemp(t.TempDir(), "stdout")
	if e != nil {
		t.Fatal(e)
	}
	defer out.Close()
	errOut, e := os.CreateTemp(t.TempDir(), "stderr")
	if e != nil {
		t.Fatal(e)
	}
	defer errOut.Close()
	originalOut, originalErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = out, errOut
	defer func() { os.Stdout, os.Stderr = originalOut, originalErr }()
	result = entry(args)
	read := func(path string) string {
		b, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		return string(b)
	}
	return read(out.Name()), read(errOut.Name()), result
}

func TestCLICodeWaitsThroughApplyingUntilTerminal(t *testing.T) {
	var polls atomic.Int32
	posted := make(chan TaskSpec, 1)
	config := cliMockConfig(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/v1/coding/tasks":
			var spec TaskSpec
			if e := json.NewDecoder(r.Body).Decode(&spec); e != nil {
				http.Error(w, e.Error(), 400)
				return
			}
			posted <- spec
			jsonReply(w, 202, map[string]any{"id": "fixture-apply", "status": "queued"})
		case r.Method == "GET" && r.URL.Path == "/v1/coding/tasks/fixture-apply":
			if polls.Add(1) == 1 {
				jsonReply(w, 200, map[string]any{"id": "fixture-apply", "status": "applying", "applied": false})
			} else {
				jsonReply(w, 200, map[string]any{"id": "fixture-apply", "status": "PASS", "applied": true})
			}
		default:
			http.NotFound(w, r)
		}
	}))
	out, errOut, e := captureCLIEntry(t, "code", "--config", config, "--repo", "/mock/repo", "--task", "fix", "--apply")
	if e != nil {
		t.Fatalf("CLI prematurely treated applying as terminal: %v; stdout=%s stderr=%s", e, out, errOut)
	}
	if polls.Load() != 2 {
		t.Fatalf("expected applying then PASS polls; got %d", polls.Load())
	}
	if spec := <-posted; !spec.Apply {
		t.Fatal("explicit --apply missing from request")
	}
	var result map[string]any
	if e = json.Unmarshal([]byte(out), &result); e != nil || result["status"] != "PASS" || result["applied"] != true {
		t.Fatalf("CLI did not print only final applied result: %s", out)
	}
	if errOut != "task fixture-apply\n" || strings.Contains(out, "applying") {
		t.Fatalf("unexpected CLI status logs: stdout=%q stderr=%q", out, errOut)
	}
}

func TestCLIChatHonorsExplicitMaxTokensAndPreservesDefault(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want any
	}{
		{"omitted", nil, nil},
		{"explicit", []string{"--max-tokens", "137"}, float64(137)},
		{"explicit-zero-rejected-by-API", []string{"--max-tokens", "0"}, float64(0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payloads := make(chan map[string]any, 1)
			config := cliMockConfig(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/v1/chat/completions" {
					http.NotFound(w, r)
					return
				}
				var payload map[string]any
				if e := json.NewDecoder(r.Body).Decode(&payload); e != nil {
					http.Error(w, e.Error(), 400)
					return
				}
				payloads <- payload
				if e := validateChat(payload); e != nil {
					jsonReply(w, 400, map[string]string{"error": e.Error()})
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"fixture answer\"},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			}))
			args := []string{"chat", "--config", config, "--message", "hello", "--profile", "fast"}
			args = append(args, tc.args...)
			out, errOut, e := captureCLIEntry(t, args...)
			if tc.want == float64(0) {
				if e == nil || !strings.Contains(e.Error(), "HTTP400") {
					t.Fatalf("explicit zero was silently ignored: %v", e)
				}
			} else if e != nil || out != "fixture answer\n" || errOut != "" {
				t.Fatalf("chat CLI output changed: err=%v stdout=%q stderr=%q", e, out, errOut)
			}
			payload := <-payloads
			if payload["max_tokens"] != tc.want {
				t.Fatalf("max_tokens=%v want %v", payload["max_tokens"], tc.want)
			}
			if payload["stream"] != true || payload["profile"] != "fast" || payload["model"] != "fixture-model" {
				t.Fatalf("other chat options changed: %v", payload)
			}
		})
	}
}

func TestCLIHelpMarksTimeoutAsCodeOnly(t *testing.T) {
	_, help, e := captureCLIEntry(t, "chat", "--help")
	if !errors.Is(e, flag.ErrHelp) || !strings.Contains(help, "code-only model timeout seconds; chat uses configured model_timeout") {
		t.Fatalf("timeout scope not documented: err=%v help=%s", e, help)
	}
}
