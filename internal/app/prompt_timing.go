package app

import (
	"encoding/json"
	"net/http"
	"time"
)

// Gateway wall-clock intervals, not GPU/kernel timings. Prompt throughput uses
// dispatch -> first content/reasoning token, including transport and scheduling.
type PromptTiming struct {
	PromptTokens     int      `json:"prompt_tokens"`
	PreparationMS    float64  `json:"preparation_ms"`
	AdmissionMS      float64  `json:"admission_ms"`
	TokenizeMS       float64  `json:"tokenize_ms"`
	DispatchMS       float64  `json:"dispatch_ms"`
	BackendHeadersMS *float64 `json:"backend_headers_ms,omitempty"`
	FirstTokenMS     *float64 `json:"first_token_ms,omitempty"`
}

func elapsedMS(start time.Time) float64 { return float64(time.Since(start).Microseconds()) / 1000 }

func writeChatEvent(w http.ResponseWriter, name string, value any) {
	b, _ := json.Marshal(value)
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, _ = w.Write(append(append([]byte("event: "+name+"\ndata: "), b...), '\n', '\n'))
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}
