package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

type disconnectedChatWriter struct {
	header http.Header
	status int
	seen   strings.Builder
}

func (w *disconnectedChatWriter) Header() http.Header    { return w.header }
func (w *disconnectedChatWriter) WriteHeader(status int) { w.status = status }
func (w *disconnectedChatWriter) Write(b []byte) (int, error) {
	w.seen.Write(b)
	return 0, errors.New("fixture browser disconnected")
}

func TestChatConversationPersistsDetachedStream(t *testing.T) {
	for _, finish := range []string{"stop", "length", "broken", "json"} {
		t.Run(finish, func(t *testing.T) {
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/tokenize" {
					jsonReply(w, 200, map[string]any{"count": 3, "max_model_len": 65664, "tokens": []int{1, 2, 3}})
					return
				}
				var p map[string]any
				json.NewDecoder(r.Body).Decode(&p)
				if _, ok := p["conversation_id"]; ok {
					t.Error("product metadata leaked upstream")
				}
				if finish == "json" {
					jsonReply(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "Answer", "reasoning_content": "Reason"}, "finish_reason": "stop"}}})
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Answer\",\"reasoning_content\":\"Reason\"},\"finish_reason\":\"" + finish + "\"}]}\n\n"))
				if finish != "broken" {
					w.Write([]byte("data: [DONE]\n\n"))
				}
			}))
			defer backend.Close()
			cfg := coreConfig(t)
			cfg.Backend = backend.URL
			cfg.TokenizerEndpoint = backend.URL + "/tokenize"
			a, e := newApp(cfg)
			if e != nil {
				t.Fatal(e)
			}
			c := newTestConversation(t, a, "Detached fixture")
			p := chatFixture()
			p["conversation_id"] = c.ID
			p["stream"] = finish != "json"
			b, _ := json.Marshal(p)
			writer := &disconnectedChatWriter{header: http.Header{}}
			request := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(string(b)))
			request.Header.Set("Content-Type", "application/json")
			a.proxyAuthorizedChat(writer, request)
			if writer.status != 200 {
				t.Fatalf("gateway status %d: %s", writer.status, writer.seen.String())
			}
			got, e := a.conversationRead(c.ID)
			if e != nil {
				t.Fatal(e)
			}
			if len(got.Messages) != 2 {
				t.Fatalf("expected user and answer: %+v", got)
			}
			answer := got.Messages[1]
			want := "complete"
			if finish == "length" {
				want = "incomplete"
			}
			if finish == "broken" {
				want = "error"
			}
			if answer.Content != "Answer" || answer.Reasoning != "Reason" || answer.Status != want {
				t.Fatalf("saved %+v", answer)
			}
			restarted, e := newApp(cfg)
			if e != nil {
				t.Fatal(e)
			}
			again, e := restarted.conversationRead(c.ID)
			if e != nil || len(again.Messages) != 2 {
				t.Fatalf("restart %v %+v", e, again)
			}
			if _, e = conversationsFor(a).delete(c.ID, got.Revision); e != nil {
				t.Fatal("lease not released:", e)
			}
		})
	}
}

func TestChatConversationRejectsMissingRecord(t *testing.T) {
	a, _ := newApp(coreConfig(t))
	p := chatFixture()
	p["conversation_id"] = id()
	if _, e := a.prepareConversationChat(p); e == nil {
		t.Fatal("unknown conversation accepted")
	}
}

func TestChatConversationQuotaFailurePreservesLastPartialAndStopsUpdates(t *testing.T) {
	for _, limit := range []string{"message", "conversation"} {
		t.Run(limit, func(t *testing.T) {
			cfg := coreConfig(t)
			a, e := newApp(cfg)
			if e != nil {
				t.Fatal(e)
			}
			c := newTestConversation(t, a, "Quota fixture")
			if limit == "conversation" {
				for n := 0; n < 6; n++ {
					if e := a.appendConversationMessage(c.ID, ConversationMessage{ID: id(), Origin: "chat", Role: "user", Content: strings.Repeat("x", 1<<20), Status: "submitted"}); e != nil {
						t.Fatal(e)
					}
				}
			}
			p := chatFixture()
			p["conversation_id"] = c.ID
			h, e := a.prepareConversationChat(p)
			if e != nil {
				t.Fatal(e)
			}
			defer h.close()
			if e = h.start(p, chatSettings{Context: 8192, Output: 1024, Prompt: 16}); e != nil {
				t.Fatal(e)
			}
			h.save(ModelResult{Content: "Last stored partial", Reasoning: "Stored reasoning"}, "pending")
			oversized := ModelResult{Content: strings.Repeat("z", (1<<20)+1)}
			if limit == "conversation" {
				oversized.Content = strings.Repeat("z", 1<<20)
				oversized.Reasoning = strings.Repeat("r", 1<<20)
			}
			h.save(oversized, "pending")
			got, e := a.conversationRead(c.ID)
			if e != nil {
				t.Fatal(e)
			}
			answer := got.Messages[len(got.Messages)-1]
			if answer.Status != "error" || answer.Content != "Last stored partial" || answer.Reasoning != "Stored reasoning" || !h.storageFailed {
				t.Fatalf("lost bounded terminal receipt: %+v", answer)
			}
			revision := got.Revision
			h.save(ModelResult{Content: "must not overwrite"}, "pending")
			h.finish(ModelResult{Content: "must not claim saved completion", StreamComplete: true, FinishReason: "stop"}, true)
			h.close()
			again, e := a.conversationRead(c.ID)
			if e != nil {
				t.Fatal(e)
			}
			if again.Revision != revision || again.Messages[len(again.Messages)-1].Status != "error" {
				t.Fatal("failed storage resumed writes or claimed completion")
			}
			logs, e := os.ReadFile(a.journal.Path)
			if e != nil {
				t.Fatal(e)
			}
			if strings.Count(string(logs), `"conversation_save_error"`) != 1 {
				t.Fatalf("expected one metadata-only diagnostic: %s", logs)
			}
			if strings.Contains(string(logs), "Last stored partial") || strings.Contains(string(logs), "Stored reasoning") {
				t.Fatal("reply text leaked into diagnostic")
			}
		})
	}
}
