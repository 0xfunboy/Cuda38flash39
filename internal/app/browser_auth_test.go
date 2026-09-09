package app

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func browserAuthFixture(t *testing.T) (*App, *http.ServeMux) {
	t.Helper()
	a, e := newApp(coreConfig(t))
	if e != nil {
		t.Fatal(e)
	}
	mux := http.NewServeMux()
	a.registerBrowserAuthRoutes(mux)
	mux.HandleFunc("GET /v1/private", func(w http.ResponseWriter, r *http.Request) {
		if a.authorized(w, r) {
			jsonReply(w, 200, map[string]any{"ok": true})
		}
	})
	mux.HandleFunc("POST /v1/private", func(w http.ResponseWriter, r *http.Request) {
		if a.authorized(w, r) {
			jsonReply(w, 200, map[string]any{"ok": true})
		}
	})
	return a, mux
}
func browserAuthRequest(mux *http.ServeMux, method, path string, cookie *http.Cookie, token string, origin bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1:18093"+path, strings.NewReader("{}"))
	r.RemoteAddr = "127.0.0.1:12345"
	r.Header.Set("X-HaloClu-Session", "1")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.Header.Set("Content-Type", "application/json")
	if origin {
		r.Header.Set("Origin", "http://127.0.0.1:18093")
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}
func browserAuthLogin(t *testing.T, a *App, mux *http.ServeMux) *http.Cookie {
	t.Helper()
	w := browserAuthRequest(mux, "POST", "/v1/auth/session", nil, a.currentToken(), true)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("one opaque session cookie required")
	}
	return cookies[0]
}

func TestBrowserSessionPersistencePrivateDigestAndRestart(t *testing.T) {
	a, mux := browserAuthFixture(t)
	cookie := browserAuthLogin(t, a, mux)
	if cookie.Name != browserSessionCookie || cookie.Path != "/v1" || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Secure || cookie.MaxAge != 15552000 || cookie.Value == a.currentToken() {
		t.Fatalf("unsafe cookie attributes: %+v", cookie)
	}
	w := browserAuthRequest(mux, "GET", "/v1/auth/session", cookie, "", false)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), cookie.Value) || strings.Contains(w.Body.String(), a.currentToken()) {
		t.Fatal("credential returned as JSON")
	}
	file := filepath.Join(a.cfg.StateDir, "browser-sessions.json")
	raw, e := os.ReadFile(file)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(raw), cookie.Value) || strings.Contains(string(raw), a.currentToken()) {
		t.Fatal("raw credential persisted")
	}
	if !strings.Contains(string(raw), browserDigest(cookie.Value)) {
		t.Fatal("digest missing")
	}
	st, _ := os.Stat(file)
	if st.Mode().Perm() != 0600 {
		t.Fatal(st.Mode())
	}
	restarted, e := newApp(a.cfg)
	if e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("GET", "http://127.0.0.1:18093/v1/private", nil)
	r.RemoteAddr = "127.0.0.1:1"
	r.AddCookie(cookie)
	r.Header.Set("X-HaloClu-Session", "1")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	w = httptest.NewRecorder()
	if !restarted.authorized(w, r) {
		t.Fatal("persisted session not restored", w.Body.String())
	}
}

func TestBrowserSessionOriginHeaderAndExplicitBearerPrecedence(t *testing.T) {
	a, mux := browserAuthFixture(t)
	cookie := browserAuthLogin(t, a, mux)
	if w := browserAuthRequest(mux, "GET", "/v1/private", cookie, "", false); w.Code != 200 {
		t.Fatal("normal same-origin GET without Origin failed")
	}
	if w := browserAuthRequest(mux, "POST", "/v1/private", cookie, "", false); w.Code != 401 {
		t.Fatal("mutation without Origin accepted")
	}
	if w := browserAuthRequest(mux, "POST", "/v1/private", cookie, "", true); w.Code != 200 {
		t.Fatal("same-origin cookie mutation failed")
	}
	if w := browserAuthRequest(mux, "GET", "/v1/private", cookie, "wrong-bearer", false); w.Code != 401 {
		t.Fatal("invalid explicit bearer fell through to cookie")
	}
	for _, tc := range []struct {
		name    string
		headers map[string]string
	}{{"no custom", map[string]string{"X-HaloClu-Session": ""}}, {"wrong origin", map[string]string{"Origin": "http://127.0.0.1:9999"}}, {"cross site", map[string]string{"Sec-Fetch-Site": "cross-site"}}, {"other port same site", map[string]string{"Sec-Fetch-Site": "same-site"}}, {"null origin", map[string]string{"Origin": "null"}}} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "http://127.0.0.1:18093/v1/private", nil)
			r.RemoteAddr = "127.0.0.1:1"
			r.AddCookie(cookie)
			r.Header.Set("X-HaloClu-Session", "1")
			for key, value := range tc.headers {
				r.Header.Set(key, value)
			}
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != 401 {
				t.Fatal("unsafe request accepted")
			}
		})
	}
	// CLI bearer clients retain the original protocol without browser headers.
	r := httptest.NewRequest("POST", "http://example.org/v1/private", nil)
	r.Header.Set("Authorization", "Bearer "+a.currentToken())
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("bearer CLI compatibility broken")
	}
}

func TestBrowserSessionLogoutExpiryAndRotationInvalidation(t *testing.T) {
	a, mux := browserAuthFixture(t)
	cookie := browserAuthLogin(t, a, mux)
	w := browserAuthRequest(mux, "DELETE", "/v1/auth/session", cookie, "", true)
	if w.Code != 200 || w.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("logout did not revoke/expire")
	}
	if browserAuthRequest(mux, "GET", "/v1/private", cookie, "", false).Code != 401 {
		t.Fatal("logged out session usable")
	}
	if browserAuthRequest(mux, "DELETE", "/v1/auth/session", cookie, "", true).Code != 200 {
		t.Fatal("logout not idempotent")
	}
	cookie = browserAuthLogin(t, a, mux)
	s := browserSessionsFor(a)
	s.mu.Lock()
	s.sessions[0].Expires = time.Now().Add(-time.Second)
	s.mu.Unlock()
	if browserAuthRequest(mux, "GET", "/v1/private", cookie, "", false).Code != 401 {
		t.Fatal("expired session usable")
	}
	cookie = browserAuthLogin(t, a, mux)
	a.tokenMu.Lock()
	a.token = id() + id()
	a.tokenMu.Unlock()
	if browserAuthRequest(mux, "GET", "/v1/private", cookie, "", false).Code != 401 {
		t.Fatal("old token-generation session usable")
	}
	fresh := browserAuthLogin(t, a, mux)
	if browserAuthRequest(mux, "GET", "/v1/private", fresh, "", false).Code != 200 {
		t.Fatal("new token session failed")
	}
}

func TestBrowserSessionLocalHTTPAndDirectTLS(t *testing.T) {
	for _, tc := range []struct {
		name, host, peer string
		tls              bool
		want             int
	}{{"local", "127.0.0.1:18093", "127.0.0.1:123", false, 200}, {"localhost", "localhost:18093", "127.0.0.1:123", false, 200}, {"remote cleartext", "10.55.0.1:18093", "10.55.0.2:123", false, 403}, {"spoofed forwarding", "example.com:18093", "127.0.0.1:123", false, 403}, {"remote TLS", "example.com:443", "192.0.2.10:123", true, 200}} {
		t.Run(tc.name, func(t *testing.T) {
			a, mux := browserAuthFixture(t)
			scheme := "http"
			if tc.tls {
				scheme = "https"
			}
			r := httptest.NewRequest("POST", scheme+"://"+tc.host+"/v1/auth/session", nil)
			r.RemoteAddr = tc.peer
			if tc.tls {
				r.TLS = &tls.ConnectionState{}
			}
			r.Header.Set("Authorization", "Bearer "+a.currentToken())
			r.Header.Set("X-HaloClu-Session", "1")
			r.Header.Set("Origin", scheme+"://"+tc.host)
			r.Header.Set("X-Forwarded-Proto", "https")
			r.Header.Set("X-Forwarded-For", "127.0.0.1")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatal(w.Code, w.Body.String())
			}
			if tc.tls && w.Code == 200 && !w.Result().Cookies()[0].Secure {
				t.Fatal("direct TLS cookie missing Secure")
			}
		})
	}
}

func TestBrowserSessionRotationRequiresBearerAndInvalidatesOldCookie(t *testing.T) {
	a, mux := browserAuthFixture(t)
	a.registerSettingsRoutes(mux)
	cookie := browserAuthLogin(t, a, mux)
	request := func(token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://127.0.0.1:18093/v1/settings/token", strings.NewReader(`{"confirm":true}`))
		r.RemoteAddr = "127.0.0.1:12345"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "http://127.0.0.1:18093")
		r.Header.Set("X-HaloClu-Session", "1")
		r.AddCookie(cookie)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	oldToken := a.currentToken()
	if w := request(""); w.Code != 401 {
		t.Fatal("cookie-only secret rotation allowed", w.Code)
	}
	if a.currentToken() != oldToken {
		t.Fatal("failed cookie-only rotation changed token")
	}
	if w := request(oldToken); w.Code != 200 {
		t.Fatal("explicit fresh bearer could not rotate", w.Code, w.Body.String())
	}
	if a.currentToken() == oldToken {
		t.Fatal("rotation did not change generation")
	}
	if browserAuthRequest(mux, "GET", "/v1/private", cookie, "", false).Code != 401 {
		t.Fatal("old browser session survived token rotation")
	}
	fresh := browserAuthLogin(t, a, mux)
	if browserAuthRequest(mux, "GET", "/v1/private", fresh, "", false).Code != 200 {
		t.Fatal("new browser session unusable")
	}
}

func TestBrowserSessionTamperQuotaAndHostBinding(t *testing.T) {
	a, mux := browserAuthFixture(t)
	cookie := browserAuthLogin(t, a, mux)
	forged := *cookie
	forged.Value = id() + id()
	if browserAuthRequest(mux, "GET", "/v1/private", &forged, "", false).Code != 401 {
		t.Fatal("forged session accepted")
	}
	r := httptest.NewRequest("GET", "http://127.0.0.1:9999/v1/private", nil)
	r.RemoteAddr = "127.0.0.1:1"
	r.Header.Set("X-HaloClu-Session", "1")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("cookie reused on another origin")
	}
	for n := 1; n < browserSessionLimit; n++ {
		browserAuthLogin(t, a, mux)
	}
	if w := browserAuthRequest(mux, "POST", "/v1/auth/session", nil, a.currentToken(), true); w.Code != 409 {
		t.Fatal("session quota ignored", w.Code)
	}
	if w := browserAuthRequest(mux, "POST", "/v1/auth/session", cookie, a.currentToken(), true); w.Code != 200 {
		t.Fatal("same-browser replacement incorrectly consumes extra slot", w.Code)
	}
}

func TestBrowserSessionStorageSymlinkAndMalformedReceiptFailClosed(t *testing.T) {
	for _, mode := range []string{"symlink", "malformed", "public permissions"} {
		t.Run(mode, func(t *testing.T) {
			a, mux := browserAuthFixture(t)
			path := filepath.Join(a.cfg.StateDir, "browser-sessions.json")
			switch mode {
			case "symlink":
				outside := filepath.Join(t.TempDir(), "outside")
				os.WriteFile(outside, []byte("private"), 0600)
				os.Symlink(outside, path)
			case "malformed":
				os.WriteFile(path, []byte(`{"version":7,"sessions":[]}`), 0600)
			case "public permissions":
				raw, _ := json.Marshal(map[string]any{"version": 1, "sessions": []any{}})
				os.WriteFile(path, raw, 0644)
			}
			w := browserAuthRequest(mux, "POST", "/v1/auth/session", nil, a.currentToken(), true)
			if w.Code != 503 {
				t.Fatal("unsafe session storage accepted", w.Code, w.Body.String())
			}
		})
	}
}
