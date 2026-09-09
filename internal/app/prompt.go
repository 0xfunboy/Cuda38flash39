package app

import (
	"encoding/json"
	"strings"
)

// Preserve the original JSON source envelope and repair feedback byte for byte.
func buildCodingPrompt(task string, files map[string]string, allowed []string, feedback string) string {
	var b strings.Builder
	b.WriteString("TASK\n" + task + "\nALLOWED PATHS\n" + strings.Join(allowed, "\n") + "\nSELECTED CURRENT SOURCES\n")
	data, _ := json.Marshal(files)
	b.Write(data)
	if feedback != "" {
		b.WriteString("\nRELEVANT FAILURE FROM PREVIOUS ATTEMPT\n" + feedback + "\nCorrect the current code, provide final JSON. The prior conversation is intentionally omitted.")
	}
	return b.String()
}
