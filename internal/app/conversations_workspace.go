package app

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The pinned Pi RPC supports prompt/get_messages/new_session/switch_session,
// but there is no generic safe importer for arbitrary Chat role/tool history.
// A handoff therefore quotes history as user-context on the next explicit
// prompt. It neither fabricates tool-call identities nor executes old tools.
func (s *PiWorkspaceSession) prepareConversationPrompt(message string) (string, error) {
	s.mu.Lock()
	key, prefix, reasoning, revision := s.ConversationID, s.PendingTranscript, s.Reasoning, s.HandoffRevision
	s.mu.Unlock()
	if key == "" {
		return message, nil
	}
	if len(prefix)+len(message) > 192<<10 {
		return "", errors.New("handoff plus prompt exceeds192KiB; no silent truncation")
	}
	cs := conversationsFor(s.manager.app)
	cs.mu.Lock()
	if cs.active[key] > 0 {
		cs.mu.Unlock()
		return "", errConversationActive
	}
	e := cs.appendLocked(key, ConversationMessage{ID: id(), Role: "user", Origin: "pi", Content: message, Status: "submitted", WorkspaceID: s.ID, Settings: map[string]any{"reasoning_effort": reasoning, "handoff_revision": revision}})
	cs.mu.Unlock()
	if e != nil {
		return "", e
	}
	s.mu.Lock()
	s.PendingTranscript = ""
	_ = s.persistLocked()
	s.mu.Unlock()
	return prefix + message, nil
}

func (s *PiWorkspaceSession) recordPiMessage(v map[string]any) {
	if v["type"] != "message_end" {
		return
	}
	// scanPi receives the original event, while emit redacts only its own copy.
	// Sanitize the complete canonical input before extracting text and events.
	encoded, _ := json.Marshal(v)
	encoded = s.manager.app.redactAPITokens(encoded)
	if s.bridgeToken != "" {
		encoded = bytes.ReplaceAll(encoded, []byte(s.bridgeToken), []byte("[SESSION-CAPABILITY-REDACTED]"))
	}
	if json.Unmarshal(encoded, &v) != nil {
		return
	}
	m, ok := v["message"].(map[string]any)
	if !ok {
		return
	}
	role, _ := m["role"].(string)
	if role == "user" {
		return
	} // Original explicit prompt was already recorded, without duplicated handoff text.
	if role == "toolResult" {
		role = "tool"
	}
	if role != "assistant" && role != "tool" {
		return
	}
	s.mu.Lock()
	key, seq, deleted := s.ConversationID, s.seq, s.deleted
	s.mu.Unlock()
	if key == "" || deleted {
		return
	}
	content, thinking := []string{}, []string{}
	if text, ok := m["content"].(string); ok {
		content = append(content, text)
	}
	for _, raw := range array(m["content"]) {
		part, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch part["type"] {
		case "text":
			content = append(content, stringValue(part["text"]))
		case "thinking":
			thinking = append(thinking, stringValue(part["thinking"]))
		case "toolCall":
			encoded, _ := json.Marshal(part)
			content = append(content, "Historical tool call: "+string(encoded))
		}
	}
	status := "complete"
	if stop := stringValue(m["stopReason"]); stop == "length" {
		status = "incomplete"
	} else if stop == "error" || stop == "aborted" {
		status = stop
	}
	if m["isError"] == true {
		status = "error"
	}
	raw, _ := json.Marshal(m)
	raw = s.manager.app.redactAPITokens(raw)
	if e := s.manager.app.appendConversationMessage(key, ConversationMessage{ID: fmt.Sprintf("pi-%s-%d", s.ID, seq), Role: role, Origin: "pi", Content: strings.Join(content, "\n"), Reasoning: strings.Join(thinking, "\n"), Status: status, WorkspaceID: s.ID, Event: raw, Settings: map[string]any{"reasoning_effort": s.Reasoning}}); e != nil {
		s.mu.Lock()
		s.BlockedReason = conversationPersistenceError(e)
		_ = s.persistLocked()
		s.mu.Unlock()
	}
}

// Restart recovery is read-only. No RPC prompt, connection or tool is replayed.
func (s *PiWorkspaceSession) loadRecordedEvents() {
	path := filepath.Join(s.dir, "rpc-events.jsonl")
	if _, e := realPath(path); e != nil {
		return
	}
	f, e := os.Open(path)
	if e != nil {
		return
	}
	defer f.Close()
	scan := bufio.NewScanner(io.LimitReader(f, 17<<20))
	scan.Buffer(make([]byte, 4096), 2<<20)
	for scan.Scan() {
		var ev WorkspaceEvent
		if json.Unmarshal(scan.Bytes(), &ev) != nil || ev.Seq <= 0 {
			continue
		}
		s.events = append(s.events, ev)
		s.eventBytes += len(ev.Event)
		if ev.Seq > s.seq {
			s.seq = ev.Seq
		}
		for len(s.events) > 1000 || s.eventBytes > 8<<20 {
			s.eventBytes -= len(s.events[0].Event)
			s.events = s.events[1:]
		}
	}
	if info, e := f.Stat(); e == nil {
		s.logBytes = int(info.Size())
		s.logCapped = s.logBytes >= 16<<20
	}
}

func (s *conversationStore) recoverWorkspace(key string) (Conversation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := workspacesFor(s.app)
	m.mu.Lock()
	defer m.mu.Unlock()
	ws := m.sessions[key]
	if ws == nil {
		return Conversation{}, os.ErrNotExist
	}
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if ws.ConversationID != "" {
		return s.readLocked(ws.ConversationID)
	}
	if ws.State != "CLOSED" {
		return Conversation{}, errConversationActive
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	c := Conversation{ID: id(), Title: "Recovered Coding conversation", Revision: 1, Created: ws.Created, Updated: now, Messages: []ConversationMessage{}, WorkspaceIDs: []string{ws.ID}}
	path := filepath.Join(ws.dir, "rpc-events.jsonl")
	if _, e := realPath(path); e != nil && !errors.Is(e, os.ErrNotExist) {
		return c, e
	}
	f, e := os.Open(path)
	if e != nil && !errors.Is(e, os.ErrNotExist) {
		return c, e
	}
	if e == nil {
		defer f.Close()
		info, e := f.Stat()
		if e != nil {
			return c, e
		}
		if info.Size() > 17<<20 {
			return c, errors.New("historical event log exceeds recovery limit; raw retained, no partial transcript promoted")
		}
		scan := bufio.NewScanner(f)
		scan.Buffer(make([]byte, 4096), 2<<20)
		for scan.Scan() {
			var ev WorkspaceEvent
			if e = json.Unmarshal(scan.Bytes(), &ev); e != nil {
				return c, errors.New("malformed historical event record; raw retained")
			}
			var v map[string]any
			if len(ev.Event) == 0 {
				var marker map[string]any
				_ = json.Unmarshal(scan.Bytes(), &marker)
				if marker["type"] == "log_truncated" {
					return c, errors.New("historical raw log was capped; recovery cannot claim complete history")
				}
				continue
			}
			if json.Unmarshal(ev.Event, &v) != nil {
				continue
			}
			if v["type"] != "message_end" {
				continue
			}
			raw, ok := v["message"].(map[string]any)
			if !ok {
				continue
			}
			role := stringValue(raw["role"])
			if role == "toolResult" {
				role = "tool"
			}
			if role != "user" && role != "assistant" && role != "tool" {
				continue
			}
			content, reasoning := []string{}, []string{}
			if text, ok := raw["content"].(string); ok {
				content = append(content, text)
			}
			for _, part := range array(raw["content"]) {
				p, ok := part.(map[string]any)
				if !ok {
					continue
				}
				switch p["type"] {
				case "text":
					content = append(content, stringValue(p["text"]))
				case "thinking":
					reasoning = append(reasoning, stringValue(p["thinking"]))
				case "toolCall":
					b, _ := json.Marshal(p)
					content = append(content, "Historical tool call: "+string(b))
				}
			}
			status := "complete"
			if stop := stringValue(raw["stopReason"]); stop == "length" {
				status = "incomplete"
			} else if stop == "error" || stop == "aborted" {
				status = stop
			}
			if raw["isError"] == true {
				status = "error"
			}
			event, _ := json.Marshal(raw)
			message := ConversationMessage{ID: fmt.Sprintf("pi-%s-%d", ws.ID, ev.Seq), Role: role, Origin: "pi", Content: strings.Join(content, "\n"), Reasoning: strings.Join(reasoning, "\n"), Status: status, WorkspaceID: ws.ID, Created: ev.Time, Event: event, Settings: map[string]any{"reasoning_effort": ws.Reasoning}}
			if e = validConversationMessage(message); e != nil {
				return c, e
			}
			c.Messages = append(c.Messages, message)
			if len(c.Messages) > 4096 {
				return c, errors.New("historical message count exceeds recovery limit")
			}
		}
		if e = scan.Err(); e != nil {
			return c, e
		}
	}
	if e = s.writeLocked(c, true); e != nil {
		return c, e
	}
	ws.ConversationID = c.ID
	if e = ws.persistLocked(); e != nil {
		return c, e
	}
	return c, nil
}
