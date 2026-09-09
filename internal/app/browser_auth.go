package app

// Browser sessions are opaque, revocable capabilities, not copies of the API
// bearer token. Disk stores only their digests and a token-generation binding.
import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const browserSessionCookie = "haloclu_session"
const browserSessionAge = 180 * 24 * time.Hour
const browserSessionLimit = 32

type browserSession struct {
	Digest           string    `json:"digest"`
	TokenFingerprint string    `json:"token_fingerprint"`
	Origin           string    `json:"origin"`
	Created          time.Time `json:"created"`
	Expires          time.Time `json:"expires"`
}
type browserSessionStore struct {
	mu       sync.Mutex
	path     string
	sessions []browserSession
	initErr  error
}

var browserSessionStores sync.Map
var browserSessionInitMu sync.Mutex

func browserDigest(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}
func browserSecretValid(secret string) bool {
	if len(secret) != 64 {
		return false
	}
	_, e := hex.DecodeString(secret)
	return e == nil
}
func exactBearer(r *http.Request, token string) bool {
	values := r.Header.Values("Authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") || token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(values[0], "Bearer ")), []byte(token)) == 1
}

func browserSessionsFor(a *App) *browserSessionStore {
	if v, ok := browserSessionStores.Load(a); ok {
		return v.(*browserSessionStore)
	}
	browserSessionInitMu.Lock()
	defer browserSessionInitMu.Unlock()
	if v, ok := browserSessionStores.Load(a); ok {
		return v.(*browserSessionStore)
	}
	s := &browserSessionStore{path: filepath.Join(a.cfg.StateDir, "browser-sessions.json"), sessions: []browserSession{}}
	f, e := os.OpenFile(s.path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if e == nil {
		st, err := f.Stat()
		if err != nil || !st.Mode().IsRegular() || st.Mode().Perm() != 0600 {
			s.initErr = errors.New("browser session storage must be a private regular file")
		}
		raw, err := io.ReadAll(io.LimitReader(f, 65537))
		f.Close()
		if err != nil || len(raw) > 65536 {
			s.initErr = errors.New("browser session receipt is unreadable or oversized")
		}
		if s.initErr == nil {
			var saved struct {
				Version  int              `json:"version"`
				Sessions []browserSession `json:"sessions"`
			}
			if json.Unmarshal(raw, &saved) != nil || saved.Version != 1 || len(saved.Sessions) > browserSessionLimit {
				s.initErr = errors.New("invalid browser session receipt")
			} else {
				s.sessions = saved.Sessions
				for _, record := range s.sessions {
					if !browserSecretValid(record.Digest) || !browserSecretValid(record.TokenFingerprint) || record.Origin == "" || record.Created.IsZero() || record.Expires.IsZero() || record.Expires.Before(record.Created) || record.Expires.Sub(record.Created) > browserSessionAge {
						s.initErr = errors.New("invalid persisted browser session")
					}
				}
			}
		}
	} else if !os.IsNotExist(e) {
		s.initErr = errors.New("browser session storage unavailable")
	}
	browserSessionStores.Store(a, s)
	return s
}
func (s *browserSessionStore) saveLocked(sessions []browserSession) error {
	if s.initErr != nil {
		return s.initErr
	}
	if st, e := os.Lstat(s.path); e == nil && !st.Mode().IsRegular() {
		return errors.New("refusing nonregular browser session storage")
	}
	raw, e := json.Marshal(struct {
		Version  int              `json:"version"`
		Sessions []browserSession `json:"sessions"`
	}{1, sessions})
	if e != nil {
		return e
	}
	if e = persistGatewaySetting(s.path, raw); e != nil {
		return errors.New("cannot persist browser session change")
	}
	s.sessions = sessions
	return nil
}

// Forwarded headers are not trusted. HTTP cookie sessions are deliberately
// local-only; remote use requires TLS directly visible to this HTTP server.
func browserOrigin(r *http.Request) (string, error) {
	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}
	u, e := url.Parse(scheme + "://" + r.Host)
	if e != nil || u.Host != r.Host || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Hostname() == "" {
		return "", errors.New("invalid browser request host")
	}
	if r.TLS == nil {
		peer, _, e := net.SplitHostPort(r.RemoteAddr)
		ip := net.ParseIP(peer)
		hostIP := net.ParseIP(u.Hostname())
		hostLocal := strings.EqualFold(u.Hostname(), "localhost") || (hostIP != nil && hostIP.IsLoopback())
		if e != nil || ip == nil || !ip.IsLoopback() || !hostLocal {
			return "", errors.New("persistent browser login requires local loopback HTTP or direct HTTPS")
		}
	}
	return scheme + "://" + r.Host, nil
}
func browserRequestGuard(r *http.Request, mutation bool) (string, error) {
	origin, e := browserOrigin(r)
	if e != nil {
		return "", e
	}
	if r.Header.Get("X-HaloClu-Session") != "1" {
		return "", errors.New("browser session request header required")
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return "", errors.New("cross-origin browser session request refused")
	}
	provided := r.Header.Get("Origin")
	if provided != "" && provided != origin {
		return "", errors.New("browser request origin mismatch")
	}
	if mutation && provided != origin {
		return "", errors.New("same-origin Origin header required for browser session mutations")
	}
	return origin, nil
}
func cookieSessionValue(r *http.Request) (string, error) {
	cookies := r.CookiesNamed(browserSessionCookie)
	if len(cookies) != 1 || !browserSecretValid(cookies[0].Value) {
		return "", errors.New("valid browser session cookie required")
	}
	return cookies[0].Value, nil
}
func (a *App) authenticateBrowserSession(r *http.Request) (browserSession, error) {
	origin, e := browserRequestGuard(r, r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS")
	if e != nil {
		return browserSession{}, e
	}
	secret, e := cookieSessionValue(r)
	if e != nil {
		return browserSession{}, e
	}
	fingerprint := browserDigest(a.currentToken())
	digest := browserDigest(secret)
	s := browserSessionsFor(a)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initErr != nil {
		return browserSession{}, s.initErr
	}
	for _, record := range s.sessions {
		if subtle.ConstantTimeCompare([]byte(record.Digest), []byte(digest)) == 1 && record.TokenFingerprint == fingerprint && record.Origin == origin && time.Now().Before(record.Expires) {
			return record, nil
		}
	}
	return browserSession{}, errors.New("browser session expired or revoked; reconnect with the API token")
}
func sessionCookie(r *http.Request, value string, expires time.Time) *http.Cookie {
	return &http.Cookie{Name: browserSessionCookie, Value: value, Path: "/v1", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: int(browserSessionAge.Seconds()), Expires: expires}
}
func browserSessionReply(w http.ResponseWriter, method string, expires time.Time) {
	out := map[string]any{"authenticated": true, "auth_method": method, "token_rotation_requires_bearer": true}
	if !expires.IsZero() {
		out["expires_at"] = expires.UTC().Format(time.RFC3339)
	}
	jsonReply(w, 200, out)
}

func (a *App) registerBrowserAuthRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/auth/session", func(w http.ResponseWriter, r *http.Request) {
		if len(r.Header.Values("Authorization")) > 0 {
			if !a.authorized(w, r) {
				return
			}
			browserSessionReply(w, "bearer", time.Time{})
			return
		}
		record, e := a.authenticateBrowserSession(r)
		if e != nil {
			jsonReply(w, 401, map[string]string{"error": e.Error()})
			return
		}
		browserSessionReply(w, "browser_session", record.Expires)
	})
	mux.HandleFunc("POST /v1/auth/session", func(w http.ResponseWriter, r *http.Request) {
		origin, e := browserRequestGuard(r, true)
		if e != nil {
			jsonReply(w, 403, map[string]string{"error": e.Error()})
			return
		}
		// Hold a read lock until the fingerprint-bound receipt is committed;
		// a concurrent token rotation cannot mint a session for the new token
		// from a request authenticated using the old token.
		a.tokenMu.RLock()
		defer a.tokenMu.RUnlock()
		if !exactBearer(r, a.token) {
			jsonReply(w, 401, map[string]string{"error": "current Bearer API token required to create a browser session"})
			return
		}
		fingerprint := browserDigest(a.token)
		s := browserSessionsFor(a)
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.initErr != nil {
			jsonReply(w, 503, map[string]string{"error": s.initErr.Error()})
			return
		}
		now := time.Now().UTC()
		kept := []browserSession{}
		oldCookie, _ := cookieSessionValue(r)
		oldDigest := browserDigest(oldCookie)
		for _, record := range s.sessions {
			if now.Before(record.Expires) && record.TokenFingerprint == fingerprint && record.Digest != oldDigest {
				kept = append(kept, record)
			}
		}
		if len(kept) >= browserSessionLimit {
			jsonReply(w, 409, map[string]string{"error": "32 active browser sessions reached; sign out another browser or rotate the API token"})
			return
		}
		secret := id() + id()
		record := browserSession{Digest: browserDigest(secret), TokenFingerprint: fingerprint, Origin: origin, Created: now, Expires: now.Add(browserSessionAge)}
		kept = append(kept, record)
		if e = s.saveLocked(kept); e != nil {
			jsonReply(w, 503, map[string]string{"error": e.Error()})
			return
		}
		http.SetCookie(w, sessionCookie(r, secret, record.Expires))
		browserSessionReply(w, "browser_session", record.Expires)
	})
	mux.HandleFunc("DELETE /v1/auth/session", func(w http.ResponseWriter, r *http.Request) {
		if _, e := browserRequestGuard(r, true); e != nil {
			jsonReply(w, 403, map[string]string{"error": e.Error()})
			return
		}
		if len(r.Header.Values("Authorization")) > 0 && !a.authorized(w, r) {
			return
		}
		secret, _ := cookieSessionValue(r)
		if secret != "" {
			s := browserSessionsFor(a)
			s.mu.Lock()
			if s.initErr != nil {
				s.mu.Unlock()
				jsonReply(w, 503, map[string]string{"error": "browser session storage unavailable; sign-out not confirmed"})
				return
			}
			kept := []browserSession{}
			digest := browserDigest(secret)
			for _, record := range s.sessions {
				if record.Digest != digest {
					kept = append(kept, record)
				}
			}
			e := s.saveLocked(kept)
			s.mu.Unlock()
			if e != nil {
				jsonReply(w, 503, map[string]string{"error": e.Error()})
				return
			}
		}
		cookie := sessionCookie(r, "", time.Unix(1, 0))
		cookie.MaxAge = -1
		http.SetCookie(w, cookie)
		jsonReply(w, 200, map[string]any{"authenticated": false})
	})
}
