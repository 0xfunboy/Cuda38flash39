package main

// Function tools use the qualified pair's *raw* completion endpoint. Native
// per-rank tool parsers allocate random IDs, which cannot be compared across
// ranks. The pair first agrees on generated text; only then does this adapter
// parse GLM47 tags and allocate a single client-visible tool-call ID.
// This is a protocol adapter, not an agent loop. Pi owns tool execution.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var functionName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]{0,63}$`)
var glmArgument = regexp.MustCompile(`(?s)^\s*<arg_key>(.*?)</arg_key>\s*<arg_value>(.*?)</arg_value>`)

func array(v any) []any { a, _ := v.([]any); return a }

func needsToolAdapter(p map[string]any) bool {
	if t, ok := p["tools"].([]any); ok && len(t) > 0 {
		return true
	}
	for _, v := range array(p["messages"]) {
		m := object(v)
		if m["role"] == "tool" || m["tool_calls"] != nil {
			return true
		}
	}
	return false
}

func toolDefinitions(p map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, v := range array(p["tools"]) {
		f := object(object(v)["function"])
		out[stringValue(f["name"])] = f
	}
	return out
}

func validateToolEnvelope(p map[string]any) error {
	defs := map[string]bool{}
	if raw, exists := p["tools"]; exists {
		list, ok := raw.([]any)
		if !ok || len(list) > 32 {
			return errors.New("tools must be an array, maximum32")
		}
		for _, v := range list {
			t := object(v)
			f := object(t["function"])
			name := stringValue(f["name"])
			if t["type"] != "function" || !functionName.MatchString(name) || defs[name] {
				return errors.New("unique named function tools required")
			}
			for k := range t {
				if k != "type" && k != "function" {
					return fmt.Errorf("unsupported tool field %s", k)
				}
			}
			for k := range f {
				if k != "name" && k != "description" && k != "parameters" && k != "strict" {
					return fmt.Errorf("unsupported function field %s", k)
				}
			}
			if f["description"] != nil {
				if _, ok := f["description"].(string); !ok {
					return errors.New("tool description must be string")
				}
			}
			if f["strict"] != nil && f["strict"] != false {
				return errors.New("strict schema generation is not supported")
			}
			schema := object(f["parameters"])
			if schema == nil || schema["type"] != "object" {
				return errors.New("tool parameters must have object schema")
			}
			if schema["properties"] != nil && object(schema["properties"]) == nil {
				return errors.New("tool properties must be object")
			}
			properties := object(schema["properties"])
			for key, value := range properties {
				if object(value) == nil || key == "" {
					return errors.New("each tool property must be a named schema object")
				}
			}
			if raw, exists := schema["required"]; exists {
				required, ok := raw.([]any)
				if !ok {
					return errors.New("tool required must be a string array")
				}
				seen := map[string]bool{}
				for _, v := range required {
					key, ok := v.(string)
					if !ok || key == "" || seen[key] || properties[key] == nil {
						return errors.New("required tool keys must be unique declared properties")
					}
					seen[key] = true
				}
			}
			defs[name] = true
		}
	}
	if v, ok := p["tool_choice"]; ok && v != "auto" {
		return errors.New("only tool_choice:auto is supported; forced/none choices are not silently ignored")
	}
	if v, ok := p["parallel_tool_calls"]; ok {
		if _, ok := v.(bool); !ok {
			return errors.New("parallel_tool_calls must be boolean")
		}
	}
	pending := map[string]bool{}
	used := map[string]bool{}
	for _, v := range array(p["messages"]) {
		m := object(v)
		role := stringValue(m["role"])
		if m["reasoning_content"] != nil {
			if role != "assistant" {
				return errors.New("reasoning_content belongs only to assistant")
			}
			if _, ok := m["reasoning_content"].(string); !ok {
				return errors.New("reasoning_content must be string")
			}
		}
		if m["name"] != nil {
			if !functionName.MatchString(stringValue(m["name"])) {
				return errors.New("invalid message name")
			}
		}
		if role == "tool" {
			tid := stringValue(m["tool_call_id"])
			if !pending[tid] || m["tool_calls"] != nil {
				return errors.New("tool result must match an unanswered assistant call")
			}
			delete(pending, tid)
			continue
		}
		if m["tool_call_id"] != nil {
			return errors.New("tool_call_id belongs only to a tool response")
		}
		if len(pending) > 0 {
			return errors.New("all tool results must precede the next non-tool message")
		}
		if raw, ok := m["tool_calls"]; ok {
			calls, ok := raw.([]any)
			if role != "assistant" || !ok || len(calls) < 1 || len(calls) > 32 {
				return errors.New("invalid assistant tool_calls")
			}
			for _, v := range calls {
				c := object(v)
				f := object(c["function"])
				tid := stringValue(c["id"])
				if c["type"] != "function" || tid == "" || len(tid) > 128 || strings.ContainsAny(tid, "\r\n\x00") || used[tid] || !functionName.MatchString(stringValue(f["name"])) {
					return errors.New("invalid or duplicate historical tool call")
				}
				var args map[string]any
				if json.Unmarshal([]byte(stringValue(f["arguments"])), &args) != nil || args == nil {
					return errors.New("tool arguments must encode a JSON object")
				}
				for k := range c {
					if k != "id" && k != "type" && k != "function" {
						return errors.New("unsupported historical tool-call field")
					}
				}
				pending[tid] = true
				used[tid] = true
			}
		}
	}
	if len(pending) > 0 {
		return errors.New("tool results are missing; no new generation dispatched")
	}
	return nil
}

// Normalize only lossless text parts used by the official Pi OpenAI provider.
// Images, audio and unknown annotations must be rejected, never discarded.
func normalizeToolText(p map[string]any) error {
	for _, v := range array(p["messages"]) {
		m := object(v)
		parts, ok := m["content"].([]any)
		if !ok {
			continue
		}
		var text strings.Builder
		for _, part := range parts {
			o := object(part)
			s, ok := o["text"].(string)
			if !ok || o["type"] != "text" || len(o) != 2 {
				return errors.New("only plain text content parts are supported")
			}
			text.WriteString(s)
		}
		m["content"] = text.String()
	}
	return nil
}

func typedToolValue(raw string, schema map[string]any) (any, error) {
	typ := schema["type"]
	if typ == "string" || typ == nil {
		return raw, nil
	}
	var v any
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&v) != nil {
		return nil, errors.New("non-string tool argument is not valid JSON")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, errors.New("trailing tool argument JSON")
	}
	switch typ {
	case "number":
		if _, ok := v.(json.Number); !ok {
			return nil, errors.New("expected number")
		}
	case "integer":
		n, ok := v.(json.Number)
		r, valid := new(big.Rat).SetString(string(n))
		if !ok || !valid || !r.IsInt() {
			return nil, errors.New("expected integer")
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return nil, errors.New("expected boolean")
		}
	case "array":
		if _, ok := v.([]any); !ok {
			return nil, errors.New("expected array")
		}
	case "object":
		if object(v) == nil {
			return nil, errors.New("expected object")
		}
	default:
		return nil, errors.New("unsupported non-string tool argument type")
	}
	return v, nil
}

func parseGLMToolOutput(raw, finish string, p map[string]any) (map[string]any, string, error) {
	// The pinned target template ends in <think>. A missing closing tag is
	// incomplete reasoning, not executable code/tools and not a final answer.
	raw = strings.TrimPrefix(raw, "<think>")
	reasoning, content := raw, ""
	if i := strings.Index(raw, "</think>"); i >= 0 {
		reasoning, content = raw[:i], raw[i+len("</think>"):]
	} else if finish != "length" {
		return nil, "", errors.New("GLM output ended without closing its reasoning segment")
	}
	for _, stop := range []string{"<|user|>", "<|endoftext|>", "[gMASK]"} {
		content = strings.TrimSuffix(content, stop)
	}
	msg := map[string]any{"role": "assistant", "content": content}
	if reasoning != "" {
		msg["reasoning_content"] = reasoning
	}
	if !strings.Contains(content, "<tool_call>") {
		return msg, finish, nil
	}
	if finish != "stop" {
		msg["content"] = ""
		return msg, finish, nil
	} // no partial tool execution
	defs := toolDefinitions(p)
	calls := []any{}
	var prose strings.Builder
	for strings.Contains(content, "<tool_call>") {
		start := strings.Index(content, "<tool_call>")
		prose.WriteString(content[:start])
		content = content[start+len("<tool_call>"):]
		end := strings.Index(content, "</tool_call>")
		if end < 0 {
			return nil, "", errors.New("unterminated tool call")
		}
		block := content[:end]
		content = content[end+len("</tool_call>"):]
		name, argsRaw := strings.TrimSpace(block), ""
		if i := strings.Index(block, "<arg_key>"); i >= 0 {
			name = strings.TrimSpace(block[:i])
			argsRaw = block[i:]
		}
		def, ok := defs[name]
		if !ok {
			return nil, "", fmt.Errorf("model requested unknown tool %q", name)
		}
		schema := object(def["parameters"])
		props := object(schema["properties"])
		args := map[string]any{}
		for strings.TrimSpace(argsRaw) != "" {
			idx := glmArgument.FindStringSubmatchIndex(argsRaw)
			if idx == nil {
				return nil, "", errors.New("malformed GLM47 argument delimiters")
			}
			key := strings.TrimSpace(argsRaw[idx[2]:idx[3]])
			value := argsRaw[idx[4]:idx[5]]
			argsRaw = argsRaw[idx[1]:]
			if _, exists := args[key]; exists {
				return nil, "", errors.New("duplicate tool argument")
			}
			property, exists := props[key]
			if !exists && schema["additionalProperties"] == false {
				return nil, "", fmt.Errorf("unknown argument %s", key)
			}
			converted, e := typedToolValue(value, object(property))
			if e != nil {
				return nil, "", fmt.Errorf("argument %s: %w", key, e)
			}
			args[key] = converted
		}
		for _, key := range array(schema["required"]) {
			if _, ok := args[stringValue(key)]; !ok {
				return nil, "", fmt.Errorf("missing required tool argument %s", key)
			}
		}
		encoded, _ := json.Marshal(args)
		calls = append(calls, map[string]any{"id": "call_" + id(), "type": "function", "function": map[string]any{"name": name, "arguments": string(encoded)}})
		if len(calls) > 32 {
			return nil, "", errors.New("too many generated tool calls")
		}
	}
	if p["parallel_tool_calls"] == false && len(calls) > 1 {
		return nil, "", errors.New("model emitted multiple tools despite parallel_tool_calls:false")
	}
	prose.WriteString(content)
	msg["content"] = prose.String()
	msg["tool_calls"] = calls
	return msg, "tool_calls", nil
}

func (a *App) proxyToolChat(w http.ResponseWriter, r *http.Request, p map[string]any, s chatSettings, started time.Time) {
	if !a.cfg.ToolCalls || len(s.TokenIDs) == 0 {
		jsonReply(w, 409, map[string]string{"error": "function tools require qualified paired-token transport and the runtime tokenizer"})
		return
	}
	data := map[string]any{"model": a.cfg.Model, "prompt": s.TokenIDs, "max_tokens": s.Output, "stream": false, "n": 1, "skip_special_tokens": false, "add_special_tokens": false}
	for _, k := range []string{"temperature", "top_p", "top_k", "min_p", "seed", "presence_penalty", "frequency_penalty", "repetition_penalty", "stop", "thinking_token_budget"} {
		if v, ok := p[k]; ok {
			data[k] = v
		}
	}
	body, _ := json.Marshal(data)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(a.cfg.ModelTimeout)*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", a.cfg.Backend+"/v1/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	response, e := a.client.Do(req)
	if e != nil {
		jsonReply(w, 502, map[string]string{"error": e.Error()})
		return
	}
	defer response.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if e != nil {
		jsonReply(w, 502, map[string]string{"error": e.Error()})
		return
	}
	if response.StatusCode != 200 {
		jsonReply(w, 502, map[string]string{"error": fmt.Sprintf("paired raw completion HTTP%d; no tools released", response.StatusCode)})
		return
	}
	out := map[string]any{}
	if json.Unmarshal(raw, &out) != nil || len(array(out["choices"])) != 1 {
		jsonReply(w, 502, map[string]string{"error": "invalid paired completion envelope"})
		return
	}
	choice := object(array(out["choices"])[0])
	msg, finish, e := parseGLMToolOutput(stringValue(choice["text"]), stringValue(choice["finish_reason"]), p)
	if e != nil {
		jsonReply(w, 502, map[string]string{"error": "no tools released: " + e.Error()})
		return
	}
	result := map[string]any{"id": "chatcmpl-" + id(), "object": "chat.completion", "created": time.Now().Unix(), "model": a.cfg.Model, "choices": []any{map[string]any{"index": 0, "message": msg, "finish_reason": finish}}, "usage": out["usage"]}
	setChatHeaders(w, s)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-StrixGLM-Tool-Transport", "paired-completions-buffered")
	if p["stream"] != true {
		jsonReply(w, 200, result)
		return
	}
	// Deliberately buffered: no fabricated streaming TTFT or per-token TPS.
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(200)
	send := func(v any) { b, _ := json.Marshal(v); _, _ = fmt.Fprintf(w, "data: %s\n\n", b) }
	delta := map[string]any{"role": "assistant", "content": msg["content"]}
	if msg["reasoning_content"] != nil {
		delta["reasoning_content"] = msg["reasoning_content"]
	}
	if calls, ok := msg["tool_calls"].([]any); ok {
		for i, v := range calls {
			object(v)["index"] = i
		}
		delta["tool_calls"] = calls
	}
	base := func(choices []any) map[string]any {
		return map[string]any{"id": result["id"], "object": "chat.completion.chunk", "created": result["created"], "model": a.cfg.Model, "choices": choices}
	}
	send(base([]any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}}))
	send(base([]any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finish}}))
	if object(p["stream_options"])["include_usage"] == true {
		v := base([]any{})
		v["usage"] = out["usage"]
		send(v)
	}
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}
