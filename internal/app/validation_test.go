package app

import (
	"encoding/json"
	"testing"
)

func TestClientValidationNoMalformedDispatch(t *testing.T) {
	for _, raw := range []string{`null`, `{}`, `{"messages":[]}`, `{"messages":[{"role":"user","content":"x"}],"stream":"yes"}`, `{"messages":[{"role":"user","content":"x"}],"max_tokens":-1}`, `{"messages":[{"role":"root","content":"x"}]}`, `{"messages":[{"role":"user","content":"x"}],"n":2}`} {
		var p map[string]any
		_ = json.Unmarshal([]byte(raw), &p)
		if validateChat(p) == nil {
			t.Errorf("accepted invalid %s", raw)
		}
	}
	var good map[string]any
	_ = json.Unmarshal([]byte(`{"messages":[{"role":"user","content":"Hello"}],"stream":true,"max_tokens":128}`), &good)
	if e := validateChat(good); e != nil {
		t.Fatal(e)
	}
}

func TestNoIgnoredThinkingOrInvalidStops(t *testing.T) {
	for _, extra := range []string{`"enable_thinking":false`, `"thinking":{"type":"disabled"}`, `"reasoning":{"effort":"none"}`, `"extra_body":{}`, `"stop":42`, `"stop":[""]`, `"stop":[1]`, `"stream_options":{"include_usage":"yes"}`, `"reasoning_effort":false`, `"model":42`, `"top_k":0`, `"top_p":0`, `"response_format":{"type":"json_object"}`} {
		var p map[string]any
		json.Unmarshal([]byte(`{"messages":[{"role":"user","content":"hi"}],`+extra+`}`), &p)
		if validateChat(p) == nil {
			t.Errorf("accepted ignored/invalid control %s", extra)
		}
	}
}
