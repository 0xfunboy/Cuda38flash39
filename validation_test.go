package main

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
