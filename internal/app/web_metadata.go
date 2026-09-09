package app

// Public metadata contains only product artwork and a trusted operator-supplied
// URL. It never derives authority from Host or forwarded request headers.
import (
	"bytes"
	"errors"
	"html"
	"net/http"
	"net/url"
	"strings"
	"time"

	webui "strixhaloclusterglm/web"
)

const socialImagePath = "assets/haloclu-social.png"
const publicMetadataMarker = "<!-- HALOCLU_PUBLIC_METADATA -->"

func validatePublicURL(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if len(value) > 2048 || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\x00\r\n\t\\") || strings.Contains(value, "#") {
		return "", errors.New("HALOCLU_PUBLIC_URL must be a plain absolute HTTP(S) URL without whitespace or backslashes")
	}
	u, e := url.Parse(value)
	if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return "", errors.New("HALOCLU_PUBLIC_URL must be an absolute HTTP(S) URL without credentials, query or fragment")
	}
	if strings.ContainsAny(u.Path, "\x00\r\n\t\\") || strings.HasSuffix(u.Host, ":") {
		return "", errors.New("invalid HALOCLU_PUBLIC_URL path or empty port")
	}
	for _, segment := range strings.Split(u.Path, "/") {
		if segment == "." || segment == ".." {
			return "", errors.New("HALOCLU_PUBLIC_URL path must not contain dot segments")
		}
	}
	if u.Port() != "" {
		// Parse validates the syntax, but not the complete TCP port range.
		port := 0
		for _, ch := range u.Port() {
			if ch < '0' || ch > '9' {
				return "", errors.New("invalid HALOCLU_PUBLIC_URL port")
			}
			port = port*10 + int(ch-'0')
			if port > 65535 {
				return "", errors.New("invalid HALOCLU_PUBLIC_URL port")
			}
		}
		if port < 1 {
			return "", errors.New("invalid HALOCLU_PUBLIC_URL port")
		}
	}
	// A trailing slash makes artwork paths relative to an explicitly configured
	// deployment prefix instead of replacing its last path component.
	u.Path = strings.TrimRight(u.Path, "/") + "/"
	u.RawPath = ""
	return u.String(), nil
}

func renderPublicMetadata(index []byte, publicURL string) []byte {
	body := string(index)
	metadata := ""
	if publicURL != "" {
		base, e := url.Parse(publicURL)
		if e == nil {
			image := html.EscapeString(base.ResolveReference(&url.URL{Path: socialImagePath}).String())
			for _, tag := range []string{"<meta property=\"og:image\"", "<meta name=\"twitter:image\""} {
				body = strings.ReplaceAll(body, tag+" content=\"./"+socialImagePath+"\">", tag+" content=\""+image+"\">")
			}
			canonical := html.EscapeString(publicURL)
			metadata = "<meta property=\"og:url\" content=\"" + canonical + "\">\n  <link rel=\"canonical\" href=\"" + canonical + "\">"
		}
	}
	return []byte(strings.ReplaceAll(body, publicMetadataMarker, metadata))
}

func publicWebHandler(publicURL string) http.Handler {
	index, indexErr := webui.Assets.ReadFile("index.html")
	index = renderPublicMetadata(index, publicURL)
	files := http.FileServer(http.FS(webui.Assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		switch r.URL.Path {
		case "/", "/index.html":
			if indexErr != nil {
				http.Error(w, "interface unavailable", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(index))
		case "/favicon.ico":
			icon, e := webui.Assets.ReadFile("favicon.ico")
			if e != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "image/x-icon")
			http.ServeContent(w, r, "favicon.ico", time.Time{}, bytes.NewReader(icon))
		default:
			files.ServeHTTP(w, r)
		}
	})
}
