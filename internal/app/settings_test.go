package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const disabledAPIs = `{"api":{"chat":false,"workspaces":false,"legacy_coding":false,"operations":false}}`
const enabledAPIs = `{"api":{"chat":true,"workspaces":true,"legacy_coding":true,"operations":true}}`

func settingsRequest(handler http.Handler, method, target, token, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestSettingsAuthAndPersistence(t *testing.T) {
	cfg := coreConfig(t)
	a, e := newApp(cfg)
	if e != nil {
		t.Fatal(e)
	}
	h := a.routes()
	for _, method := range []string{"GET", "PUT", "POST"} {
		target := "/v1/settings"
		if method == "POST" {
			target += "/token"
		}
		if w := settingsRequest(h, method, target, "wrong", disabledAPIs); w.Code != 401 {
			t.Fatal("settings must require authentication")
		}
	}
	w := settingsRequest(h, "GET", "/v1/settings", a.currentToken(), "")
	if w.Code != 200 || strings.Contains(w.Body.String(), a.currentToken()) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("settings response leaks or fails")
	}
	if a.currentAPISettings() != (APISettings{true, true, true, true}) {
		t.Fatal("new install changed defaults")
	}
	w = settingsRequest(h, "PUT", "/v1/settings", a.currentToken(), disabledAPIs)
	if w.Code != 200 || a.currentAPISettings() != (APISettings{}) {
		t.Fatalf("save failed: HTTP%d", w.Code)
	}
	info, e := os.Stat(filepath.Join(cfg.StateDir, "api-settings.json"))
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatal("API settings not private")
	}
	b, e := newApp(cfg)
	if e != nil || b.currentAPISettings() != (APISettings{}) {
		t.Fatal("switches lost across restart")
	}
	if settingsRequest(h, "PUT", "/v1/settings", a.currentToken(), enabledAPIs).Code != 200 {
		t.Fatal("cannot reenable without restart")
	}
}

func TestSettingsRejectInvalidNoSilentReset(t *testing.T) {
	cfg := coreConfig(t)
	a, _ := newApp(cfg)
	h := a.routes()
	for _, body := range []string{`{}`, `null`, `{"api":null}`, `{"api":{"chat":false}}`, strings.Replace(enabledAPIs, `true`, `null`, 1), strings.Replace(enabledAPIs, `true`, `"true"`, 1), strings.Replace(enabledAPIs, `"chat":true`, `"unknown":true`, 1), enabledAPIs + `{}`, strings.Replace(enabledAPIs, `{"api"`, `{"listen":"0.0.0.0:80","api"`, 1)} {
		if w := settingsRequest(h, "PUT", "/v1/settings", a.currentToken(), body); w.Code != 400 {
			t.Fatalf("accepted invalid settings: HTTP%d", w.Code)
		}
	}
	if a.currentAPISettings() != (APISettings{true, true, true, true}) {
		t.Fatal("invalid input changed live settings")
	}
	if e := os.WriteFile(filepath.Join(cfg.StateDir, "api-settings.json"), []byte(`{}`), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := newApp(cfg); e == nil {
		t.Fatal("invalid saved settings silently enabled APIs")
	}
}

func TestSettingsDisableAdmissionKeepCleanup(t *testing.T) {
	a, _ := newApp(coreConfig(t))
	h := a.routes()
	settingsRequest(h, "PUT", "/v1/settings", a.currentToken(), disabledAPIs)
	for _, target := range []string{
		"/v1/chat/completions", "/v1/chat/%63ompletions", "/v1/attachments", "/v1/coding/tasks", "/v1/coding/tasks/missing/apply", "/v1/operations/jobs", "/v1/workspaces/sessions", "/v1/workspaces/sessions/missing/start", "/v1/workspaces/sessions/missing/connect", "/v1/workspaces/sessions/missing/prompt", "/v1/workspaces/sessions/missing/terminal",
	} {
		w := settingsRequest(h, "POST", target, a.currentToken(), `{}`)
		if w.Code != 403 || !strings.Contains(w.Body.String(), "api_disabled") {
			t.Fatalf("not blocked: %s HTTP%d", target, w.Code)
		}
		if settingsRequest(h, "POST", target, "wrong", `{}`).Code != 401 {
			t.Fatal("disabled endpoint leaked admission before auth")
		}
	}
	for _, target := range []string{"/v1/workspaces/sessions/missing/close", "/v1/workspaces/sessions/missing/abort", "/v1/coding/tasks/missing/cancel", "/v1/operations/jobs/missing/cancel"} {
		w := settingsRequest(h, "POST", target, a.currentToken(), `{"confirm":true}`)
		if strings.Contains(w.Body.String(), "api_disabled") {
			t.Fatalf("cleanup disabled: %s", target)
		}
	}
	for _, target := range []string{"/v1/settings", "/v1/models", "/v1/options", "/v1/workspaces/presets", "/v1/workspaces/sessions", "/v1/operations/jobs"} {
		if w := settingsRequest(h, "GET", target, a.currentToken(), ""); w.Code != 200 {
			t.Fatalf("read disabled: %s HTTP%d", target, w.Code)
		}
	}
}

func TestSettingsRotateToken(t *testing.T) {
	cfg := coreConfig(t)
	a, _ := newApp(cfg)
	h := a.routes()
	old := a.currentToken()
	for _, body := range []string{`{}`, `{"confirm":false}`, `{"confirm":true,"token":"user-chosen"}`} {
		if settingsRequest(h, "POST", "/v1/settings/token", old, body).Code != 400 {
			t.Fatal("rotation lacks explicit safe confirmation")
		}
	}
	w := settingsRequest(h, "POST", "/v1/settings/token", old, `{"confirm":true}`)
	var result map[string]string
	json.Unmarshal(w.Body.Bytes(), &result)
	next := result["token"]
	if w.Code != 200 || len(next) < 32 || old == next || next != a.currentToken() || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("rotation failed")
	}
	if settingsRequest(h, "GET", "/v1/settings", old, "").Code != 401 || settingsRequest(h, "GET", "/v1/settings", next, "").Code != 200 {
		t.Fatal("revocation not enforced")
	}
	info, e := os.Stat(filepath.Join(cfg.StateDir, "api-token"))
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatal("rotated token is not private")
	}
	b, e := newApp(cfg)
	if e != nil || b.currentToken() != next {
		t.Fatal("token did not persist")
	}
	redacted := string(a.redactAPITokens([]byte(old + " " + next)))
	if strings.Contains(redacted, old) || strings.Contains(redacted, next) {
		t.Fatal("old or new credential not redacted")
	}
}

func TestSettingsRotationConcurrency(t *testing.T) {
	a, _ := newApp(coreConfig(t))
	h := a.routes()
	old := a.currentToken()
	var group sync.WaitGroup
	codes := make(chan int, 12)
	for range 12 {
		group.Go(func() {
			for range 8 {
				_ = a.currentToken()
				_ = a.redactAPITokens([]byte("event"))
				_ = a.currentAPISettings()
			}
			codes <- settingsRequest(h, "POST", "/v1/settings/token", old, `{"confirm":true}`).Code
		})
	}
	group.Wait()
	close(codes)
	success := 0
	for code := range codes {
		if code == 200 {
			success++
		} else if code != 401 {
			t.Fatalf("unexpected rotation code %d", code)
		}
	}
	if success != 1 {
		t.Fatalf("old credential rotated %d times", success)
	}
}

func TestSettingsPersistenceFailureKeepsLiveValues(t *testing.T) {
	cfg := coreConfig(t)
	a, _ := newApp(cfg)
	h := a.routes()
	old := a.currentToken()
	// A directory at each destination makes atomic replacement fail without
	// changing the application state. No production files or credentials used.
	if e := os.Mkdir(filepath.Join(cfg.StateDir, "api-settings.json"), 0700); e != nil {
		t.Fatal(e)
	}
	if settingsRequest(h, "PUT", "/v1/settings", old, disabledAPIs).Code != 500 || a.currentAPISettings() != (APISettings{true, true, true, true}) {
		t.Fatal("failed persistence changed switches")
	}
	if e := os.Rename(filepath.Join(cfg.StateDir, "api-token"), filepath.Join(cfg.StateDir, "original-token")); e != nil {
		t.Fatal(e)
	}
	if e := os.Mkdir(filepath.Join(cfg.StateDir, "api-token"), 0700); e != nil {
		t.Fatal(e)
	}
	if settingsRequest(h, "POST", "/v1/settings/token", old, `{"confirm":true}`).Code != 500 || a.currentToken() != old {
		t.Fatal("failed persistence changed token")
	}
}
