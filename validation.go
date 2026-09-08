package main

import (
	"errors"
	"math"
)

// Reject malformed client requests BEFORE dispatching either TP2 rank. A normal
// 400 must never turn into an uncertain paired engine generation.
func validateChat(p map[string]any) error {
	if p == nil {
		return errors.New("chat body must be an object")
	}
	messages, ok := p["messages"].([]any)
	if !ok || len(messages) == 0 || len(messages) > 512 {
		return errors.New("messages must be a nonempty array (maximum512)")
	}
	for _, v := range messages {
		m, ok := v.(map[string]any)
		if !ok {
			return errors.New("each message must be an object")
		}
		role := stringValue(m["role"])
		if role != "system" && role != "user" && role != "assistant" && role != "tool" {
			return errors.New("unsupported message role")
		}
		if _, ok := m["content"].(string); !ok {
			return errors.New("this text engine requires string message content; multimedia/tool-call envelopes are not qualified")
		}
	}
	if v, ok := p["stream"]; ok {
		if _, ok := v.(bool); !ok {
			return errors.New("stream must be boolean")
		}
	}
	for key, bounds := range map[string][2]float64{"max_tokens": {1, 16384}, "n": {1, 1}, "seed": {0, 2147483647}} {
		if v, ok := p[key]; ok {
			n, ok := v.(float64)
			if !ok || math.IsNaN(n) || math.IsInf(n, 0) || math.Trunc(n) != n || n < bounds[0] || n > bounds[1] {
				return errors.New("invalid integer " + key)
			}
		}
	}
	for key, bounds := range map[string][2]float64{"temperature": {0, 2}, "top_p": {0, 1}} {
		if v, ok := p[key]; ok {
			n, ok := v.(float64)
			if !ok || math.IsNaN(n) || math.IsInf(n, 0) || n < bounds[0] || n > bounds[1] {
				return errors.New("invalid " + key)
			}
		}
	}
	if v, ok := p["chat_template_kwargs"]; ok {
		k, ok := v.(map[string]any)
		if !ok {
			return errors.New("chat_template_kwargs must be object")
		}
		if r, ok := k["reasoning_effort"]; ok {
			if r != "low" && r != "high" && r != "max" && r != "medium" {
				return errors.New("unknown reasoning effort")
			}
		}
	}
	if p["tools"] != nil || p["functions"] != nil {
		return errors.New("tools/functions envelopes are not qualified; use coding task API")
	}
	return nil
}
