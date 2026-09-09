package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func toolFixture() map[string]any {
	p := chatFixture()
	p["tools"] = []any{map[string]any{"type": "function", "function": map[string]any{"name": "read", "parameters": map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}, "limit": map[string]any{"type": "integer"}}, "required": []any{"path"}, "additionalProperties": false}}}}
	return p
}
func TestToolEnvelopeAdmission(t *testing.T) {
	p := toolFixture()
	if e := validateChatWithTools(p, true); e != nil {
		t.Fatal(e)
	}
	if validateChat(p) == nil {
		t.Fatal("tools silently enabled in unqualified config")
	}
	for _, modify := range []func(map[string]any){
		func(p map[string]any) { p["tool_choice"] = "required" },
		func(p map[string]any) { object(array(p["tools"])[0])["type"] = "web_search" },
		func(p map[string]any) { object(object(array(p["tools"])[0])["function"])["strict"] = true },
		func(p map[string]any) {
			object(object(object(array(p["tools"])[0])["function"])["parameters"])["required"] = "path"
		},
		func(p map[string]any) {
			p["messages"] = append(array(p["messages"]), map[string]any{"role": "tool", "tool_call_id": "orphan", "content": "secret"})
		},
		func(p map[string]any) { object(array(p["messages"])[0])["reasoning_content"] = "invalid role" },
	} {
		p := toolFixture()
		modify(p)
		if validateChatWithTools(p, true) == nil {
			t.Fatalf("accepted malformed tool request %#v", p)
		}
	}
	p = toolFixture()
	call := map[string]any{"id": "call_demo", "type": "function", "function": map[string]any{"name": "read", "arguments": "{\"path\":\"README.md\"}"}}
	p["messages"] = append(array(p["messages"]), map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{call}})
	if validateChatWithTools(p, true) == nil {
		t.Fatal("missing result accepted")
	}
	p["messages"] = append(array(p["messages"]), map[string]any{"role": "tool", "tool_call_id": "call_demo", "content": "file text"})
	if e := validateChatWithTools(p, true); e != nil {
		t.Fatal(e)
	}
}
func TestToolPlainTextParts(t *testing.T) {
	p := chatFixture()
	object(array(p["messages"])[0])["content"] = []any{map[string]any{"type": "text", "text": "first"}, map[string]any{"type": "text", "text": "second"}}
	if e := normalizeToolText(p); e != nil {
		t.Fatal(e)
	}
	if object(array(p["messages"])[0])["content"] != "firstsecond" {
		t.Fatal("text lost")
	}
	object(array(p["messages"])[0])["content"] = []any{map[string]any{"type": "image_url", "image_url": "secret"}}
	if normalizeToolText(p) == nil {
		t.Fatal("image silently discarded")
	}
}

func TestToolNumbersNotRounded(t *testing.T) {
	for _, tc := range []struct{ raw, typ string }{{"9007199254740993", "integer"}, {"{\"id\":9007199254740993}", "object"}, {"[9007199254740993]", "array"}} {
		v, e := typedToolValue(tc.raw, map[string]any{"type": tc.typ})
		if e != nil {
			t.Fatal(e)
		}
		b, _ := json.Marshal(v)
		if string(b) != tc.raw {
			t.Fatalf("rounded tool data: %s -> %s", tc.raw, b)
		}
	}
	if _, e := typedToolValue("1.5", map[string]any{"type": "integer"}); e == nil {
		t.Fatal("fraction accepted as integer")
	}
}
func TestGLMToolsParsedOnlyAfterNaturalEnd(t *testing.T) {
	raw := "Check the file.</think><tool_call>read<arg_key>path</arg_key><arg_value>src/è.c</arg_value><arg_key>limit</arg_key><arg_value>12</arg_value></tool_call>"
	p := toolFixture()
	m, finish, e := parseGLMToolOutput(raw, "stop", p)
	if e != nil {
		t.Fatal(e)
	}
	if finish != "tool_calls" || m["reasoning_content"] != "Check the file." {
		t.Fatal(m)
	}
	call := object(array(m["tool_calls"])[0])
	f := object(call["function"])
	var args map[string]any
	json.Unmarshal([]byte(stringValue(f["arguments"])), &args)
	if args["limit"] != float64(12) || args["path"] != "src/è.c" || !strings.HasPrefix(stringValue(call["id"]), "call_") {
		t.Fatal(call)
	}
	m, finish, e = parseGLMToolOutput(raw, "length", p)
	if e != nil || finish != "length" || m["tool_calls"] != nil {
		t.Fatal("truncated tools executable")
	}
	for _, bad := range []string{
		"</think><tool_call>delete_all</tool_call>",
		"</think><tool_call>read</tool_call>",
		"</think><tool_call>read<arg_key>path</arg_key><arg_value>a</arg_value><arg_key>path</arg_key><arg_value>b</arg_value></tool_call>",
		"</think><tool_call>read<arg_key>path</arg_key><arg_value>a</arg_value><arg_key>limit</arg_key><arg_value>oops</arg_value></tool_call>",
		"</think><tool_call>read<arg_key>path</arg_key><arg_value>a</arg_value>garbage</tool_call>",
		"</think><tool_call>read<arg_key>path</arg_key><arg_value>a",
	} {
		if _, _, e := parseGLMToolOutput(bad, "stop", p); e == nil {
			t.Fatalf("malformed output accepted %s", bad)
		}
	}
	m, finish, e = parseGLMToolOutput("</think>Done.", "stop", p)
	if e != nil || finish != "stop" || m["content"] != "Done." {
		t.Fatal(m, e)
	}
	if _, _, e := parseGLMToolOutput("still thinking", "stop", p); e == nil {
		t.Fatal("missing reason terminator falsely concluded")
	}
}
func TestToolAdapterUsesPairedRawTokens(t *testing.T) {
	var rawCalls atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p map[string]any
		json.NewDecoder(r.Body).Decode(&p)
		switch r.URL.Path {
		case "/tokenize":
			if p["tools"] == nil {
				t.Error("tool template missing")
			}
			jsonReply(w, 200, map[string]any{"count": 3, "tokens": []int{4, 5, 6}, "max_model_len": 65664})
		case "/v1/completions":
			rawCalls.Add(1)
			if p["stream"] != false || p["skip_special_tokens"] != false || p["add_special_tokens"] != false || p["tools"] != nil || p["messages"] != nil || len(array(p["prompt"])) != 3 {
				t.Errorf("unsafe raw envelope %#v", p)
			}
			jsonReply(w, 200, map[string]any{"choices": []any{map[string]any{"index": 0, "text": "</think><tool_call>read<arg_key>path</arg_key><arg_value>a.c</arg_value></tool_call>", "finish_reason": "stop"}}, "usage": map[string]any{"prompt_tokens": 3, "completion_tokens": 20, "total_tokens": 23}})
		default:
			t.Error("native per-rank chat tool parser used")
			http.Error(w, "unsafe", 500)
		}
	}))
	defer backend.Close()
	cfg := coreConfig(t)
	cfg.Backend = backend.URL
	cfg.TokenizerEndpoint = backend.URL + "/tokenize"
	cfg.ToolCalls = true
	a, e := newApp(cfg)
	if e != nil {
		t.Fatal(e)
	}
	for _, stream := range []bool{false, true} {
		p := toolFixture()
		p["stream"] = stream
		b, _ := json.Marshal(p)
		r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(string(b)))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+a.token)
		w := httptest.NewRecorder()
		a.proxyChat(w, r)
		if w.Code != 200 || !strings.Contains(w.Body.String(), "tool_calls") || w.Header().Get("X-StrixGLM-Tool-Transport") != "paired-completions-buffered" {
			t.Fatal(w.Code, w.Body.String())
		}
		if stream && !strings.Contains(w.Body.String(), "[DONE]") {
			t.Fatal("unterminated SSE")
		}
	}
	if rawCalls.Load() != 2 {
		t.Fatal("unexpected inference count")
	}
}
