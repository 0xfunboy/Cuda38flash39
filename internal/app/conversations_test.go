package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func conversationRequest(t *testing.T, a *App, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	mux := http.NewServeMux()
	a.registerConversationRoutes(mux)
	r := httptest.NewRequest(method, path, strings.NewReader(string(b)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+a.currentToken())
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}
func newTestConversation(t *testing.T, a *App, title string) Conversation {
	t.Helper()
	w := conversationRequest(t, a, "POST", "/v1/conversations", map[string]any{"title": title})
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var c Conversation
	if e := json.Unmarshal(w.Body.Bytes(), &c); e != nil {
		t.Fatal(e)
	}
	return c
}
func TestConversationPersistenceAndPrivateMode(t *testing.T) {
	a, _ := workspaceTestApp(t)
	c := newTestConversation(t, a, "Persistent transcript")
	m := ConversationMessage{ID: id(), Role: "user", Origin: "chat", Content: "Remember this coding contract.", Status: "complete"}
	if e := a.appendConversationMessage(c.ID, m); e != nil {
		t.Fatal(e)
	}
	b, e := newApp(a.cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer conversationStores.Delete(b)
	got, e := b.conversationRead(c.ID)
	if e != nil || len(got.Messages) != 1 || got.Messages[0].Content != m.Content {
		t.Fatal(got, e)
	}
	info, e := os.Stat(filepath.Join(a.cfg.StateDir, "conversations", c.ID, "conversation.json"))
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatal(info, e)
	}
	if workspaces, ok := workspaceManagers.Load(b); ok && len(workspaces.(*workspaceManager).sessions) > 0 {
		t.Fatal("reading history started a workspace")
	}
}
func TestConversationAuthenticationAndValidation(t *testing.T) {
	a, _ := workspaceTestApp(t)
	mux := http.NewServeMux()
	a.registerConversationRoutes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/v1/conversations", nil))
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	c := newTestConversation(t, a, "x")
	for _, m := range []ConversationMessage{{ID: id(), Role: "system", Origin: "chat", Status: "complete"}, {ID: id(), Role: "user", Origin: "remote", Status: "complete"}, {ID: id(), Role: "user", Origin: "chat", AttachmentIDs: []string{"../private"}}} {
		w = conversationRequest(t, a, "PUT", "/v1/conversations/"+c.ID, map[string]any{"revision": c.Revision, "title": "x", "messages": []ConversationMessage{m}})
		if w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	w = conversationRequest(t, a, "GET", "/v1/conversations/not-a-local-id", nil)
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
	w = conversationRequest(t, a, "POST", "/v1/conversations", map[string]any{"title": "x", "id": c.ID})
	if w.Code != 400 {
		t.Fatal("caller supplied ID accepted")
	}
}
func TestConversationCASAndActiveLease(t *testing.T) {
	a, _ := workspaceTestApp(t)
	c := newTestConversation(t, a, "x")
	body := map[string]any{"revision": c.Revision, "title": "changed", "messages": []ConversationMessage{}}
	w := conversationRequest(t, a, "PUT", "/v1/conversations/"+c.ID, body)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = conversationRequest(t, a, "PUT", "/v1/conversations/"+c.ID, body)
	if w.Code != 409 {
		t.Fatal(w.Code)
	}
	c, _ = a.conversationRead(c.ID)
	release, e := a.beginConversationUse(c.ID)
	if e != nil {
		t.Fatal(e)
	}
	w = conversationRequest(t, a, "DELETE", "/v1/conversations/"+c.ID, map[string]any{"revision": c.Revision, "confirm": true})
	if w.Code != 409 {
		t.Fatal(w.Code)
	}
	w = conversationRequest(t, a, "PUT", "/v1/conversations/"+c.ID, map[string]any{"revision": c.Revision, "title": "x", "messages": []ConversationMessage{}})
	if w.Code != 409 {
		t.Fatal(w.Code)
	}
	release()
	release()
	w = conversationRequest(t, a, "DELETE", "/v1/conversations/"+c.ID, map[string]any{"revision": c.Revision, "confirm": true})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, e = a.beginConversationUse(c.ID); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("deleted conversation reacquired", e)
	}
}
func TestConversationChatPiHandoffIsExplicitTranscript(t *testing.T) {
	a, project := workspaceTestApp(t)
	c := newTestConversation(t, a, "Shared Chat and Pi")
	if e := a.appendConversationMessage(c.ID, ConversationMessage{ID: id(), Role: "user", Origin: "chat", Content: "Implement the parser without network calls.", Status: "complete"}); e != nil {
		t.Fatal(e)
	}
	ws, e := workspacesFor(a).create("local", project, "", "max", c.ID)
	if e != nil {
		t.Fatal(e)
	}
	c, _ = a.conversationRead(c.ID)
	w := conversationRequest(t, a, "POST", "/v1/conversations/"+c.ID+"/handoff", map[string]any{"workspace_id": ws.ID, "revision": c.Revision, "confirm": true})
	if w.Code != 200 || ws.cmd != nil || ws.State != "CONNECTED" {
		t.Fatal(w.Code, w.Body.String(), ws.State)
	}
	if !strings.Contains(ws.PendingTranscript, "parser without network") {
		t.Fatal("history not staged")
	}
	actual, e := ws.prepareConversationPrompt("Now inspect the files.")
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(actual, "Historical tool records") || !strings.HasSuffix(actual, "Now inspect the files.") {
		t.Fatal(actual)
	}
	if ws.PendingTranscript != "" {
		t.Fatal("handoff was not consumed")
	}
	ws.emit([]byte(`{"type":"message_end"}`))
	ws.recordPiMessage(map[string]any{"type": "message_end", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "thinking", "thinking": "A short recorded reasoning."}, map[string]any{"type": "text", "text": "Implemented and ready for independent checks."}}, "stopReason": "stop"}})
	c, e = a.conversationRead(c.ID)
	if e != nil {
		t.Fatal(e)
	}
	if len(c.Messages) != 3 || c.Messages[1].Origin != "pi" || c.Messages[1].Content != "Now inspect the files." || c.Messages[2].Reasoning == "" {
		t.Fatal(c.Messages)
	}
	if c.Messages[2].WorkspaceID != ws.ID || len(c.WorkspaceIDs) != 1 {
		t.Fatal("canonical linkage lost")
	}
	if ws.cmd != nil {
		t.Fatal("transcript transfer executed an agent")
	}
}
func TestConversationPiRecordsImmutableToChatSave(t *testing.T) {
	a, _ := workspaceTestApp(t)
	c := newTestConversation(t, a, "x")
	if e := a.appendConversationMessage(c.ID, ConversationMessage{ID: id(), Role: "assistant", Origin: "pi", Content: "Actual recorded result", Status: "complete"}); e != nil {
		t.Fatal(e)
	}
	c, _ = a.conversationRead(c.ID)
	w := conversationRequest(t, a, "PUT", "/v1/conversations/"+c.ID, map[string]any{"revision": c.Revision, "title": c.Title, "messages": []ConversationMessage{}})
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	c.Messages[0].Content = "Fake improved result"
	w = conversationRequest(t, a, "PUT", "/v1/conversations/"+c.ID, map[string]any{"revision": c.Revision, "title": c.Title, "messages": c.Messages})
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
}
func TestConversationServerPartialUpdate(t *testing.T) {
	a, _ := workspaceTestApp(t)
	c := newTestConversation(t, a, "x")
	m := ConversationMessage{ID: id(), Role: "assistant", Origin: "chat", Status: "streaming"}
	if e := a.appendConversationMessage(c.ID, m); e != nil {
		t.Fatal(e)
	}
	m.Content = "Partial output"
	m.Reasoning = "Recorded reasoning"
	m.Status = "incomplete"
	if e := a.updateConversationMessage(c.ID, m); e != nil {
		t.Fatal(e)
	}
	got, _ := a.conversationRead(c.ID)
	if len(got.Messages) != 1 || got.Messages[0].Status != "incomplete" || got.Messages[0].Created == "" {
		t.Fatal(got)
	}
	m.ID = id()
	if e := a.updateConversationMessage(c.ID, m); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("update created missing message", e)
	}
}
func TestConversationDeleteOwnsPiRecordsNotProject(t *testing.T) {
	a, project := workspaceTestApp(t)
	c := newTestConversation(t, a, "Delete test")
	ws, e := workspacesFor(a).create("local", project, "", "low", c.ID)
	if e != nil {
		t.Fatal(e)
	}
	projectFile := filepath.Join(project, "keep.c")
	if e = os.WriteFile(projectFile, []byte("original project"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.MkdirAll(filepath.Join(ws.dir, "pi-sessions"), 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(ws.dir, "pi-sessions", "session.jsonl"), []byte("canonical secret marker"), 0600); e != nil {
		t.Fatal(e)
	}
	ws.emit([]byte(`{"type":"message_end","message":{"role":"assistant","content":"canonical secret marker"}}`))
	c, _ = a.conversationRead(c.ID)
	if _, e = conversationsFor(a).delete(c.ID, c.Revision); !errors.Is(e, errConversationActive) {
		t.Fatal(e)
	}
	if e = ws.close(context.Background()); e != nil {
		t.Fatal(e)
	}
	if _, e = conversationsFor(a).delete(c.ID, c.Revision); e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{ws.dir, filepath.Join(a.cfg.StateDir, "conversations", c.ID)} {
		if _, e = os.Stat(path); !errors.Is(e, os.ErrNotExist) {
			t.Fatal("retained local record", path, e)
		}
	}
	if b, e := os.ReadFile(projectFile); e != nil || string(b) != "original project" {
		t.Fatal("project affected", e)
	}
	ws.emit([]byte(`{"late":"canonical secret marker"}`))
	ws.mu.Lock()
	e = ws.persistLocked()
	ws.mu.Unlock()
	if !errors.Is(e, os.ErrNotExist) {
		t.Fatal("deleted session resurrected", e)
	}
	if e = a.appendConversationMessage(c.ID, ConversationMessage{ID: id(), Role: "user", Origin: "chat", Content: "stale save", Status: "complete"}); !errors.Is(e, os.ErrNotExist) {
		t.Fatal(e)
	}
	w := conversationRequest(t, a, "PUT", "/v1/conversations/"+c.ID, map[string]any{"revision": c.Revision, "title": "resurrect", "messages": []ConversationMessage{}})
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
}
func TestConversationSharedAttachmentDeletion(t *testing.T) {
	a, _ := workspaceTestApp(t)
	c1 := newTestConversation(t, a, "one")
	c2 := newTestConversation(t, a, "two")
	shared, owned := id(), id()
	for _, aid := range []string{shared, owned} {
		p := filepath.Join(a.cfg.StateDir, "attachments", aid)
		if e := os.MkdirAll(p, 0700); e != nil {
			t.Fatal(e)
		}
		if e := writeJSON(filepath.Join(p, "attachment.json"), Attachment{ID: aid, Name: "test.txt", Text: "attachment text"}); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(filepath.Join(p, "upload"), []byte("attachment bytes"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	for _, x := range []struct {
		key string
		ids []string
	}{{c1.ID, []string{shared, owned}}, {c2.ID, []string{shared}}} {
		if e := a.appendConversationMessage(x.key, ConversationMessage{ID: id(), Role: "user", Origin: "chat", Status: "complete", AttachmentIDs: x.ids}); e != nil {
			t.Fatal(e)
		}
	}
	c1, _ = a.conversationRead(c1.ID)
	if _, e := conversationsFor(a).delete(c1.ID, c1.Revision); e != nil {
		t.Fatal(e)
	}
	if _, e := a.readAttachment(shared); e != nil {
		t.Fatal("shared upload removed", e)
	}
	if _, e := a.readAttachment(owned); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("owned upload retained", e)
	}
	c2, _ = a.conversationRead(c2.ID)
	if _, e := conversationsFor(a).delete(c2.ID, c2.Revision); e != nil {
		t.Fatal(e)
	}
	if _, e := a.readAttachment(shared); !errors.Is(e, os.ErrNotExist) {
		t.Fatal(e)
	}
}
func TestConversationClosedEventsRecoveredWithoutExecution(t *testing.T) {
	a, project := workspaceTestApp(t)
	ws, e := workspacesFor(a).create("local", project, "", "low")
	if e != nil {
		t.Fatal(e)
	}
	ws.emit([]byte(`{"type":"tool_execution_end","result":"read-only recovery marker"}`))
	if e = ws.close(context.Background()); e != nil {
		t.Fatal(e)
	}
	b, e := newApp(a.cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer workspaceManagers.Delete(b)
	defer conversationStores.Delete(b)
	recovered := workspacesFor(b).sessions[ws.ID]
	if recovered == nil || recovered.State != "CLOSED" || len(recovered.events) != 1 || recovered.cmd != nil || recovered.ssh != nil {
		t.Fatal("history recovery failed")
	}
	if !strings.Contains(string(recovered.events[0].Event), "recovery marker") {
		t.Fatal("event text missing")
	}
}
func TestConversationConcurrentRevisionWriters(t *testing.T) {
	a, _ := workspaceTestApp(t)
	c := newTestConversation(t, a, "x")
	s := conversationsFor(a)
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			s.mu.Lock()
			_, e := s.updateLocked(c.ID, c.Revision, fmt.Sprintf("writer%d", n), []ConversationMessage{})
			s.mu.Unlock()
			if e == nil {
				successes.Add(1)
			} else if !errors.Is(e, errConversationConflict) {
				t.Error(e)
			}
		}(i)
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatal(successes.Load())
	}
}
func TestConversationRejectsStorageSymlinkAndRedactsToken(t *testing.T) {
	a, _ := workspaceTestApp(t)
	c := newTestConversation(t, a, "x")
	if e := a.appendConversationMessage(c.ID, ConversationMessage{ID: id(), Role: "user", Origin: "chat", Status: "complete", Content: "accidental credential " + a.currentToken()}); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(filepath.Join(a.cfg.StateDir, "conversations", c.ID, "conversation.json"))
	if e != nil || strings.Contains(string(b), a.currentToken()) {
		t.Fatal("credential persisted", e)
	}
	fake := id()
	outside := filepath.Join(t.TempDir(), "outside.json")
	if e = writeJSON(outside, Conversation{ID: fake}); e != nil {
		t.Fatal(e)
	}
	if e = os.Symlink(filepath.Dir(outside), filepath.Join(a.cfg.StateDir, "conversations", fake)); e != nil {
		t.Fatal(e)
	}
	if _, e = a.conversationRead(fake); e == nil {
		t.Fatal("symlink read accepted")
	}
}

func TestConversationRestartMarksInterruptedNotLiveReads(t *testing.T) {
	a, _ := workspaceTestApp(t)
	c := newTestConversation(t, a, "unfinished")
	release, e := a.beginConversationUse(c.ID)
	if e != nil {
		t.Fatal(e)
	}
	if e = a.appendConversationMessage(c.ID, ConversationMessage{ID: id(), Role: "assistant", Origin: "chat", Content: "partial", Status: "streaming"}); e != nil {
		t.Fatal(e)
	}
	got, e := a.conversationRead(c.ID)
	if e != nil || got.Messages[0].Status != "streaming" {
		t.Fatal(got, e)
	}
	release()
	b, e := newApp(a.cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer conversationStores.Delete(b)
	got, e = b.conversationRead(c.ID)
	if e != nil || got.Messages[0].Status != "interrupted" || got.Messages[0].Content != "partial" {
		t.Fatal(got, e)
	}
}
func TestConversationLegacyPiRecoveryAndReplay(t *testing.T) {
	a, project := workspaceTestApp(t)
	ws, e := workspacesFor(a).create("local", project, "", "low")
	if e != nil {
		t.Fatal(e)
	}
	ws.ConversationID = "" // Metadata fixture from the version before canonical history.
	ws.emit([]byte(`{"type":"message_end","message":{"role":"user","content":"Inspect the parser"}}`))
	ws.emit([]byte(`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"I inspected it."},{"type":"toolCall","name":"bash","arguments":{"command":"never replay me"}}],"stopReason":"stop"}}`))
	ws.emit([]byte(`{"type":"message_end","message":{"role":"toolResult","content":"tool output should remain history only"}}`))
	if e = ws.close(context.Background()); e != nil {
		t.Fatal(e)
	}
	w := conversationRequest(t, a, "POST", "/v1/workspaces/sessions/"+ws.ID+"/recover-conversation", map[string]any{"confirm": true})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var c Conversation
	if e = json.Unmarshal(w.Body.Bytes(), &c); e != nil {
		t.Fatal(e)
	}
	if c.ID == "" || c.ID != ws.ConversationID || len(c.Messages) != 3 || ws.cmd != nil {
		t.Fatal(c)
	}
	replay := conversationReplay(c)
	b, _ := json.Marshal(replay)
	if len(replay) != 2 || strings.Contains(string(b), "never replay") || strings.Contains(string(b), "history only") {
		t.Fatal(string(b))
	}
	w = conversationRequest(t, a, "GET", "/v1/conversations/"+c.ID, nil)
	if !strings.Contains(w.Body.String(), "replay_messages") {
		t.Fatal(w.Body.String())
	}
}
func TestConversationAttachmentEndpointRejectsLinkedDeletion(t *testing.T) {
	a, _ := workspaceTestApp(t)
	c := newTestConversation(t, a, "with attachment")
	aid := id()
	p := filepath.Join(a.cfg.StateDir, "attachments", aid)
	if e := writeJSON(filepath.Join(p, "attachment.json"), Attachment{ID: aid, Name: "file.txt", Text: "owned content"}); e != nil {
		t.Fatal(e)
	}
	if e := a.appendConversationMessage(c.ID, ConversationMessage{ID: id(), Role: "user", Origin: "chat", Status: "complete", AttachmentIDs: []string{aid}}); e != nil {
		t.Fatal(e)
	}
	mux := http.NewServeMux()
	a.registerAttachmentRoutes(mux)
	r := httptest.NewRequest("DELETE", "/v1/attachments/"+aid, nil)
	r.Header.Set("Authorization", "Bearer "+a.currentToken())
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestConversationDeletionIncludesInterruptedAtomicCopies(t *testing.T) {
	a, _ := workspaceTestApp(t)
	c := newTestConversation(t, a, "delete all local copies")
	dir := filepath.Join(a.cfg.StateDir, "conversations", c.ID)
	if e := os.WriteFile(filepath.Join(dir, ".state-interrupted"), []byte("private incomplete atomic copy"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := conversationsFor(a).delete(c.ID, c.Revision); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(dir); !errors.Is(e, os.ErrNotExist) {
		t.Fatal(e)
	}
}
func TestConversationNoOverlappingChatPiPrompts(t *testing.T) {
	a, project := workspaceTestApp(t)
	c := newTestConversation(t, a, "single sequence")
	ws, e := workspacesFor(a).create("local", project, "", "low", c.ID)
	if e != nil {
		t.Fatal(e)
	}
	release, e := a.beginConversationUse(c.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = a.beginConversationUse(c.ID); !errors.Is(e, errConversationActive) {
		t.Fatal(e)
	}
	if _, e = ws.prepareConversationPrompt("new prompt"); !errors.Is(e, errConversationActive) {
		t.Fatal(e)
	}
	release()
	ws.mu.Lock()
	ws.State = "RUNNING"
	ws.mu.Unlock()
	if _, e = a.beginConversationUse(c.ID); !errors.Is(e, errConversationActive) {
		t.Fatal(e)
	}
	ws.mu.Lock()
	ws.State = "CONNECTED"
	ws.mu.Unlock()
}

func TestConversationPiCapabilityRedactionCoversEveryCopy(t *testing.T) {
	a, project := workspaceTestApp(t)
	ws, e := workspacesFor(a).create("local", project, "", "low")
	if e != nil {
		t.Fatal(e)
	}
	ws.bridgeToken = "fixture-scoped-capability-not-a-real-secret"
	v := map[string]any{"type": "message_end", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": ws.bridgeToken}, map[string]any{"type": "thinking", "thinking": ws.bridgeToken}}, "stopReason": "stop"}}
	b, _ := json.Marshal(v)
	ws.emit(b)
	ws.recordPiMessage(v)
	c, e := a.conversationRead(ws.ConversationID)
	if e != nil {
		t.Fatal(e)
	}
	b, _ = json.Marshal(c)
	if strings.Contains(string(b), ws.bridgeToken) || !strings.Contains(string(b), "SESSION-CAPABILITY-REDACTED") {
		t.Fatal("scoped capability leaked into canonical record")
	}
	b, e = os.ReadFile(filepath.Join(ws.dir, "rpc-events.jsonl"))
	if e != nil || strings.Contains(string(b), ws.bridgeToken) {
		t.Fatal("scoped capability leaked into event log", e)
	}
}
