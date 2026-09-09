package app

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// APISettings pauses admission at the public gateway, not the paired runtime.
// Existing work may finish; discovery, status and cleanup remain accessible.
type APISettings struct {
	Chat         bool `json:"chat"`
	Workspaces   bool `json:"workspaces"`
	LegacyCoding bool `json:"legacy_coding"`
	Operations   bool `json:"operations"`
}

func (a *App) currentToken() string {
	a.tokenMu.RLock()
	defer a.tokenMu.RUnlock()
	return a.token
}

func (a *App) redactAPITokens(raw []byte) []byte {
	a.tokenMu.RLock()
	defer a.tokenMu.RUnlock()
	if a.token != "" {
		raw = bytes.ReplaceAll(raw, []byte(a.token), []byte("[REDACTED]"))
	}
	for _, token := range a.retiredTokens {
		raw = bytes.ReplaceAll(raw, []byte(token), []byte("[REDACTED]"))
	}
	return raw
}

func (a *App) currentAPISettings() APISettings {
	if settings := a.apiSettings.Load(); settings != nil {
		return *settings
	}
	return APISettings{true, true, true, true}
}

// An explicit, complete object avoids accidental enablement via omitted flags.
func parseAPISettings(raw []byte) (APISettings, error) {
	var payload struct {
		API *struct {
			Chat         *bool `json:"chat"`
			Workspaces   *bool `json:"workspaces"`
			LegacyCoding *bool `json:"legacy_coding"`
			Operations   *bool `json:"operations"`
		} `json:"api"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if e := decoder.Decode(&payload); e != nil {
		return APISettings{}, errors.New("expected only api with four explicit boolean switches")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF || payload.API == nil || payload.API.Chat == nil || payload.API.Workspaces == nil || payload.API.LegacyCoding == nil || payload.API.Operations == nil {
		return APISettings{}, errors.New("api requires chat, workspaces, legacy_coding and operations booleans")
	}
	s := payload.API
	return APISettings{*s.Chat, *s.Workspaces, *s.LegacyCoding, *s.Operations}, nil
}

func (a *App) loadAPISettings() error {
	raw, e := os.ReadFile(filepath.Join(a.cfg.StateDir, "api-settings.json"))
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return fmt.Errorf("read API settings: %w", e)
	}
	s, e := parseAPISettings(raw)
	if e != nil {
		return fmt.Errorf("invalid saved API settings (not silently reset): %w", e)
	}
	a.apiSettings.Store(&s)
	return nil
}

// Persist before publishing to readers. A post-rename directory-sync error must
// not leave the live token different from the committed on-disk token.
func persistGatewaySetting(file string, raw []byte) error {
	if e := controllerBytes(file, raw); e != nil {
		committed, readErr := os.ReadFile(file)
		if readErr != nil || !bytes.Equal(committed, raw) {
			return e
		}
		log.Print("gateway setting committed, but directory durability could not be confirmed")
	}
	return nil
}

func (a *App) settingsReply(w http.ResponseWriter) {
	jsonReply(w, 200, map[string]any{
		"api": a.currentAPISettings(), "listen": a.cfg.Listen,
		"backend": a.cfg.Backend, "model": a.cfg.Model,
		"token_rotation_supported": true,
		"api_note":                 "Switches pause new public API actions. Existing work may finish; status, reads, cancellation and workspace close remain available. These are not separate security roles.",
		"network_note":             "Listener and backend are managed by the deployment config. Network changes require an explicit gateway restart; this page does not expose new ports or restart model ranks.",
	})
}

func (a *App) registerSettingsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/settings", func(w http.ResponseWriter, r *http.Request) {
		if a.authorized(w, r) {
			a.settingsReply(w)
		}
	})
	mux.HandleFunc("PUT /v1/settings", func(w http.ResponseWriter, r *http.Request) {
		if !a.authorized(w, r) {
			return
		}
		var raw json.RawMessage
		if e := decodeBody(w, r, &raw); e != nil {
			jsonReply(w, 400, map[string]string{"error": "valid settings JSON required"})
			return
		}
		s, e := parseAPISettings(raw)
		if e != nil {
			jsonReply(w, 400, map[string]string{"error": e.Error()})
			return
		}
		a.settingsMu.Lock()
		defer a.settingsMu.Unlock()
		data, _ := json.Marshal(map[string]any{"api": s})
		if e := persistGatewaySetting(filepath.Join(a.cfg.StateDir, "api-settings.json"), data); e != nil {
			jsonReply(w, 500, map[string]string{"error": "could not persist API settings; live switches unchanged"})
			return
		}
		a.apiSettings.Store(&s)
		a.settingsReply(w)
	})
	mux.HandleFunc("POST /v1/settings/token", func(w http.ResponseWriter, r *http.Request) {
		if !a.authorized(w, r) {
			return
		}
		var body map[string]any
		if e := decodeBody(w, r, &body); e != nil || len(body) != 1 || body["confirm"] != true {
			jsonReply(w, 400, map[string]string{"error": "explicit confirm:true required; the server generates the new secret"})
			return
		}
		a.tokenMu.Lock()
		defer a.tokenMu.Unlock()
		// Two simultaneous rotations with the old token must not both succeed.
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(got), []byte(a.token)) != 1 {
			jsonReply(w, 401, map[string]string{"error": "token already changed; reconnect before rotating again"})
			return
		}
		next := id() + id()
		if e := persistGatewaySetting(filepath.Join(a.cfg.StateDir, "api-token"), []byte(next)); e != nil {
			jsonReply(w, 500, map[string]string{"error": "could not persist new token; existing credential unchanged"})
			return
		}
		a.retiredTokens = append(a.retiredTokens, a.token)
		a.token = next
		jsonReply(w, 200, map[string]string{"token": next})
	})
}

func (a *App) disabledAPICategory(pattern string, r *http.Request) string {
	s := a.currentAPISettings()
	switch pattern {
	case "POST /v1/chat/completions", "POST /v1/attachments":
		if !s.Chat {
			return "chat"
		}
	case "POST /v1/coding/tasks", "POST /v1/coding/tasks/{id}/apply":
		if !s.LegacyCoding {
			return "legacy_coding"
		}
	case "POST /v1/operations/jobs":
		if !s.Operations {
			return "operations"
		}
	case "POST /v1/workspaces/sessions":
		if !s.Workspaces {
			return "workspaces"
		}
	case "POST /v1/workspaces/sessions/{id}/{action}":
		action := path.Base(r.URL.Path)
		if !s.Workspaces && action != "abort" && action != "close" {
			return "workspaces"
		}
	}
	return ""
}
