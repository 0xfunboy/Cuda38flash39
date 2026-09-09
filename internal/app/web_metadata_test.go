package app

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	webui "strixhaloclusterglm/web"
)

func TestPublicLogosAndIconTransparency(t *testing.T) {
	for _, path := range []string{"assets/haloclu-icon.png", "assets/haloclu-horizontal.png"} {
		raw, err := webui.Assets.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		transparent, opaque := 0, 0
		for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
			for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
				_, _, _, a := img.At(x, y).RGBA()
				if a == 0 {
					transparent++
				}
				if a == 65535 {
					opaque++
				}
			}
		}
		if transparent == 0 || opaque == 0 {
			t.Fatalf("%s needs transparent background and opaque artwork", path)
		}
		_, _, _, corner := img.At(0, 0).RGBA()
		if corner != 0 {
			t.Fatalf("%s background corner is not transparent", path)
		}
		t.Logf("%s: %dx%d, %d transparent / %d opaque pixels", path, img.Bounds().Dx(), img.Bounds().Dy(), transparent, opaque)
	}
	ico, err := webui.Assets.ReadFile("favicon.ico")
	if err != nil {
		t.Fatal(err)
	}
	if len(ico) < 102 || binary.LittleEndian.Uint16(ico[4:6]) != 6 {
		t.Fatal("expected six ICO frames")
	}
	for i, size := range []int{16, 32, 48, 64, 128, 256} {
		entry := ico[6+i*16 : 6+(i+1)*16]
		length, offset := int(binary.LittleEndian.Uint32(entry[8:12])), int(binary.LittleEndian.Uint32(entry[12:16]))
		if offset < 102 || length < 1 || offset > len(ico)-length {
			t.Fatal("bad ICO frame bounds")
		}
		img, err := png.Decode(bytes.NewReader(ico[offset : offset+length]))
		if err != nil {
			t.Fatal(err)
		}
		_, _, _, a := img.At(0, 0).RGBA()
		if img.Bounds().Dx() != size || img.Bounds().Dy() != size || a != 0 {
			t.Fatalf("ICO frame %d dimensions/transparency", size)
		}
	}
}

func TestPublicURLValidation(t *testing.T) {
	for input, want := range map[string]string{"": "", "https://halo.example": "https://halo.example/", "https://halo.example/app": "https://halo.example/app/", "http://127.0.0.1:18093/": "http://127.0.0.1:18093/", "https://halo.example/app///": "https://halo.example/app/"} {
		got, e := validatePublicURL(input)
		if e != nil || got != want {
			t.Fatalf("%q => %q %v", input, got, e)
		}
	}
	for _, input := range []string{"/relative", "//halo.example", "javascript:alert(1)", "ftp://halo.example", "https://user:password@halo.example", "https://halo.example/?token=secret", "https://halo.example/?", "https://halo.example/#part", "https://halo.example/#", "https://halo.example/../app", "https://halo.example/%2e%2e/app", "https://halo.example/%00", "https://halo.example:/", " https://halo.example", "https://halo.example\n", "https://halo.example\\bad", "https://halo.example:70000/", "https://halo.example:0/"} {
		if _, e := validatePublicURL(input); e == nil {
			t.Fatalf("unsafe public URL accepted: %q", input)
		}
	}
}

func TestPublicMetadataRenderNoRequestAuthority(t *testing.T) {
	input := []byte(`<head><title>HaloClu</title><meta property="og:image" content="./assets/haloclu-social.png"><meta name="twitter:image" content="./assets/haloclu-social.png"><!-- HALOCLU_PUBLIC_METADATA --></head>`)
	local := string(renderPublicMetadata(input, ""))
	if !strings.Contains(local, `content="./assets/haloclu-social.png"`) || strings.Contains(local, "canonical") || strings.Contains(local, "og:url") || strings.Contains(local, publicMetadataMarker) {
		t.Fatal(local)
	}
	public := string(renderPublicMetadata(input, "https://halo.example/app/"))
	if strings.Count(public, `content="https://halo.example/app/assets/haloclu-social.png"`) != 2 || !strings.Contains(public, `property="og:url" content="https://halo.example/app/"`) || !strings.Contains(public, `rel="canonical" href="https://halo.example/app/"`) {
		t.Fatal(public)
	}
	quoted := string(renderPublicMetadata(input, "https://halo.example/a&b/"))
	if !strings.Contains(quoted, "a&amp;b/") || strings.Contains(quoted, `content="https://halo.example/a&b/"`) {
		t.Fatal("URL was not HTML escaped", quoted)
	}
}

func TestPublicURLCapturedAtStartup(t *testing.T) {
	t.Setenv("HALOCLU_PUBLIC_URL", "https://first.example/product")
	a, _ := workspaceTestApp(t)
	t.Setenv("HALOCLU_PUBLIC_URL", "https://second.example/")
	if a.publicURL != "https://first.example/product/" {
		t.Fatal("environment not pinned at startup", a.publicURL)
	}
	t.Setenv("HALOCLU_PUBLIC_URL", "https://user:secret@example.test/")
	state := filepath.Join(t.TempDir(), "must-not-be-created")
	if _, e := newApp(Config{StateDir: state}); e == nil {
		t.Fatal("invalid metadata URL did not block startup")
	}
	if _, e := os.Stat(state); !os.IsNotExist(e) {
		t.Fatal("invalid URL created application state", e)
	}
}

func TestPublicMetadataHTTPAndPrivateRoutes(t *testing.T) {
	t.Setenv("HALOCLU_PUBLIC_URL", "https://halo.example/")
	a, _ := workspaceTestApp(t)
	h := a.routes()
	for _, path := range []string{"/", "/index.html"} {
		req := httptest.NewRequest("GET", path, nil)
		req.Host = "attacker.invalid"
		req.Header.Set("X-Forwarded-Host", "forwarded-attacker.invalid")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		body := w.Body.String()
		if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") || strings.Contains(body, "attacker.invalid") {
			t.Fatal(path, w.Code, w.Header())
		}
		for _, part := range []string{`property="og:title"`, `property="og:description"`, `property="og:type" content="website"`, `property="og:site_name" content="HaloClu"`, `property="og:image" content="https://halo.example/assets/haloclu-social.png"`, `property="og:image:width" content="1729"`, `property="og:image:height" content="910"`, `name="twitter:card" content="summary_large_image"`, `name="twitter:title"`, `name="twitter:description"`, `name="twitter:image" content="https://halo.example/assets/haloclu-social.png"`, `rel="canonical"`, `name="description"`} {
			if !strings.Contains(body, part) {
				t.Fatalf("missing public head metadata %s", part)
			}
		}
		if w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Content-Security-Policy") == "" {
			t.Fatal("static safety headers lost")
		}
	}
	for _, path := range []string{"/v1/models", "/v1/conversations", "/v1/settings"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 401 {
			t.Fatal("metadata handler relaxed API auth", path, w.Code)
		}
	}
	for _, path := range []string{"/state/api-token", "/internal/app/main.go", "/README.md"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 404 {
			t.Fatal("nonembedded file exposed", path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("HEAD", "/", nil))
	if w.Code != 200 || w.Body.Len() != 0 || w.Header().Get("Content-Length") == "" {
		t.Fatal("HEAD response", w.Code, w.Body.Len(), w.Header())
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/", nil))
	if w.Code != 405 {
		t.Fatal("static POST accepted", w.Code)
	}
}

func TestPublicFaviconAndSocialAssets(t *testing.T) {
	icon, e := webui.Assets.ReadFile("favicon.ico")
	if e != nil {
		t.Fatal(e)
	}
	if len(icon) < 22 || !bytes.Equal(icon[:4], []byte{0, 0, 1, 0}) || binary.LittleEndian.Uint16(icon[4:6]) == 0 {
		t.Fatal("favicon is not a valid ICO directory")
	}
	image, e := webui.Assets.ReadFile(socialImagePath)
	if e != nil {
		t.Fatal(e)
	}
	if len(image) < 24 || !bytes.Equal(image[:8], []byte{137, 80, 78, 71, 13, 10, 26, 10}) || binary.BigEndian.Uint32(image[16:20]) != 1729 || binary.BigEndian.Uint32(image[20:24]) != 910 {
		t.Fatal("social image must match its actual 1729x910 PNG dimensions")
	}
	h := publicWebHandler("")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/favicon.ico", nil))
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/x-icon" || !bytes.Equal(w.Body.Bytes(), icon) {
		t.Fatal("favicon content/MIME", w.Code, w.Header())
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodHead, "/favicon.ico", nil))
	if w.Code != 200 || w.Body.Len() != 0 || w.Header().Get("Content-Type") != "image/x-icon" {
		t.Fatal("favicon HEAD", w.Code, w.Header())
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/"+socialImagePath, nil))
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" || !bytes.Equal(w.Body.Bytes(), image) {
		t.Fatal("social card content/MIME", w.Code, w.Header())
	}
}
