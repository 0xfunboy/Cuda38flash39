package app

import (
	"errors"
	"maps"
	"time"
)

// Persistence belongs to the gateway, not to a connected browser. The upstream
// stream still drains after the browser disconnects; its final result is saved.
type conversationChat struct {
	app               *App
	key               string
	user              ConversationMessage
	answer            ConversationMessage
	lastStored        ConversationMessage
	release           func()
	lastSave          time.Time
	started, finished bool
	storageFailed     bool
}

func (a *App) prepareConversationChat(p map[string]any) (*conversationChat, error) {
	h := &conversationChat{app: a, key: stringValue(p["conversation_id"])}
	delete(p, "conversation_id") // Never forward product metadata to the model.
	if h.key == "" {
		return h, nil
	}
	if needsToolAdapter(p) {
		return nil, errors.New("conversation_id is for plain Chat; Pi stores tool transcripts through its scoped workspace bridge")
	}
	var e error
	h.release, e = a.beginConversationUse(h.key)
	if e != nil {
		return nil, e
	}
	msgs, _ := p["messages"].([]any)
	if len(msgs) > 0 {
		m := object(msgs[len(msgs)-1])
		h.user = ConversationMessage{ID: id(), Role: "user", Origin: "chat", Content: stringValue(m["content"]), Status: "submitted"}
		if stringValue(m["role"]) != "user" {
			h.release()
			return nil, errors.New("a persisted Chat request must end with a user message")
		}
		if ids, ok := m["attachment_ids"].([]any); ok {
			for _, v := range ids {
				if s, ok := v.(string); ok {
					h.user.AttachmentIDs = append(h.user.AttachmentIDs, s)
				}
			}
		}
	}
	return h, nil
}
func (h *conversationChat) start(p map[string]any, admitted chatSettings) error {
	if h.key == "" {
		return nil
	}
	settings := map[string]any{}
	settings["context_tokens"] = admitted.Context
	settings["max_tokens"] = admitted.Output
	settings["prompt_tokens"] = admitted.Prompt
	for _, k := range []string{"model", "reasoning_effort", "max_tokens", "context_window", "thinking_token_budget"} {
		if v, ok := p[k]; ok {
			settings[k] = v
		}
	}
	h.answer = ConversationMessage{ID: id(), Role: "assistant", Origin: "chat", Status: "pending", Settings: settings}
	if e := h.app.appendConversationMessage(h.key, h.user); e != nil {
		return e
	}
	if e := h.app.appendConversationMessage(h.key, h.answer); e != nil {
		return e
	}
	h.started = true
	h.lastStored = h.answer
	h.lastStored.Settings = maps.Clone(h.answer.Settings)
	return nil
}
func (h *conversationChat) save(r ModelResult, status string) {
	if !h.started || h.storageFailed {
		return
	}
	h.answer.Content = r.Content
	h.answer.Reasoning = r.Reasoning
	h.answer.Status = status
	h.answer.Settings["metrics"] = r.Metrics
	if e := h.app.updateConversationMessage(h.key, h.answer); e != nil {
		// Do not keep retrying an over-quota snapshot, nor leave the browser
		// polling a permanently pending record. Retain only the last successfully
		// stored text and change its status to the shorter terminal "error".
		// No error text or new metrics are appended to that bounded receipt.
		h.storageFailed = true
		h.answer = h.lastStored
		h.answer.Settings = maps.Clone(h.lastStored.Settings)
		h.answer.Status = "error"
		recoveryError := h.app.updateConversationMessage(h.key, h.answer)
		detail := map[string]any{"conversation_id": h.key, "message_id": h.answer.ID, "error": e.Error(), "terminal_status_saved": recoveryError == nil}
		if recoveryError != nil {
			detail["recovery_error"] = recoveryError.Error()
		}
		h.app.journal.Log("conversation_save_error", detail)
	} else {
		h.lastStored = h.answer
		h.lastStored.Settings = maps.Clone(h.answer.Settings)
	}
	h.lastSave = time.Now()
}
func (h *conversationChat) progress(r ModelResult) {
	if h.started && time.Since(h.lastSave) >= time.Second {
		h.save(r, "pending")
	}
}
func (h *conversationChat) finish(r ModelResult, ok bool) {
	status := "incomplete"
	if ok && r.StreamComplete && r.FinishReason == "stop" {
		status = "complete"
	}
	if !ok {
		status = "error"
	}
	h.save(r, status)
	h.finished = true
}
func (h *conversationChat) close() {
	if h == nil {
		return
	}
	if h.started && !h.finished {
		h.save(ModelResult{Content: h.answer.Content, Reasoning: h.answer.Reasoning}, "error")
	}
	if h.release != nil {
		h.release()
	}
}
