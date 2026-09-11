package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

type timingRecorder struct {
	*httptest.ResponseRecorder
	early atomic.Bool
}

func (w *timingRecorder) Flush() { w.early.Store(true); w.ResponseRecorder.Flush() }

func TestPromptTimingOptInAndPersistence(t *testing.T) {
	for _, optIn := range []bool{false, true} {
		for _, failure := range []bool{false, true} {
			name := "legacy"
			if optIn {
				name = "timed"
			}
			if failure {
				name += "-failure"
			}
			t.Run(name, func(t *testing.T) {
				w := &timingRecorder{ResponseRecorder: httptest.NewRecorder()}
				backend := httptest.NewServer(http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/tokenize" {
						jsonReply(out, 200, map[string]any{"count": 100, "max_model_len": 65664, "tokens": make([]int, 100)})
						return
					}
					if w.early.Load() != optIn {
						t.Error("early telemetry must be opt-in and precede upstream generation")
					}
					if r.Header.Get("X-HaloClu-Timings") != "" {
						t.Error("telemetry header leaked upstream")
					}
					var p map[string]any
					_ = json.NewDecoder(r.Body).Decode(&p)
					if p["conversation_id"] != nil || p["prompt_timing"] != nil {
						t.Error("metadata leaked to model")
					}
					if failure {
						jsonReply(out, 502, map[string]string{"error": "fixture upstream failure"})
						return
					}
					out.Header().Set("Content-Type", "text/event-stream")
					_, _ = out.Write([]byte("data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"thinking\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"OK\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":2},\"metrics\":{\"queue_time_ms\":3,\"time_to_first_token_ms\":10}}\n\ndata: [DONE]\n\n"))
				}))
				defer backend.Close()
				cfg := coreConfig(t)
				cfg.Backend = backend.URL
				cfg.TokenizerEndpoint = backend.URL + "/tokenize"
				a, e := newApp(cfg)
				if e != nil {
					t.Fatal(e)
				}
				c := newTestConversation(t, a, "Timing fixture")
				p := chatFixture()
				p["stream"] = true
				p["conversation_id"] = c.ID
				body, _ := json.Marshal(p)
				r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(string(body)))
				r.Header.Set("Content-Type", "application/json")
				if optIn {
					r.Header.Set("X-HaloClu-Timings", "1")
				}
				a.proxyAuthorizedChat(w, r)
				text := w.Body.String()
				if failure {
					if optIn && (!strings.Contains(text, "event: error") || strings.Contains(text, "[DONE]")) {
						t.Fatalf("invalid streamed error: %s", text)
					}
					if !optIn && w.Code != 502 {
						t.Fatalf("legacy HTTP error changed: %d", w.Code)
					}
					return
				}
				if optIn && strings.Count(text, "event: haloclu.timing") != 3 {
					t.Fatalf("missing timing phases: %s", text)
				}
				if !optIn && strings.Contains(text, "haloclu.timing") {
					t.Fatal("legacy SSE modified")
				}
				got, e := a.conversationRead(c.ID)
				if e != nil {
					t.Fatal(e)
				}
				if len(got.Messages) != 2 {
					t.Fatalf("unexpected history; HTTP %d: %s", w.Code, text)
				}
				b, _ := json.Marshal(got.Messages[1].Settings["metrics"])
				var m Metrics
				_ = json.Unmarshal(b, &m)
				if m.PromptTiming == nil || m.PromptTiming.PromptTokens != 100 || m.PromptTiming.FirstTokenMS == nil || m.PromptTiming.BackendHeadersMS == nil {
					t.Fatalf("missing persisted prompt timing: %s", b)
				}
				if *m.PromptTiming.FirstTokenMS < m.PromptTiming.DispatchMS {
					t.Fatal("invalid timing order")
				}
				if got.Messages[1].Content != "OK" || got.Messages[1].Reasoning != "thinking" {
					t.Fatal("telemetry changed model text")
				}
			})
		}
	}
}
