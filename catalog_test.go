package main

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCatalogEmbeddedPinsAndHonestModes(t *testing.T) {
	c, err := loadModelCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Models) < 10 {
		t.Fatal("missing historical inventory")
	}
	byID := map[string]CatalogModel{}
	for _, m := range c.Models {
		byID[m.ID] = m
		if m.ID != "glm-ciru-flash-w4" && (m.Actions.Load.Enabled || m.Actions.Benchmark.Enabled) {
			t.Fatalf("unimplemented switch enabled: %s", m.ID)
		}
		if m.ID != "glm-ciru-flash-w4" && len(m.Blockers) == 0 {
			t.Fatalf("missing block reason: %s", m.ID)
		}
	}
	if !strings.Contains(byID["qwen-flash-next-q5"].Distribution, "SERIAL") {
		t.Fatal("Q5 serial layer-RPC disguised as TP2")
	}
	if !strings.Contains(byID["qwen-flash-next-iq3"].Distribution, "NODE01") {
		t.Fatal("IQ3 historical single-node scope lost")
	}
	if len(byID["glm-full-antirez-iq2"].Evidence) != 1 || byID["glm-full-antirez-iq2"].Evidence[0].DecodeTPS != nil {
		t.Fatal("invented FULL throughput")
	}
	if byID["qwen27-quark-retired"].Status != "HISTORICAL" {
		t.Fatal("retired dense reopened")
	}
	if !strings.Contains(byID["qwen-flash-next-iq3"].NgramEmbedding, "NOT") || !strings.Contains(byID["qwen-flash-next-iq3"].DraftLookup, "token-history") {
		t.Fatal("embedding/draft roles collapsed")
	}
}

func TestCatalogInventoryOnlyStatsAndRemoteUnknown(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "model.gguf")
	if err := os.WriteFile(p, []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	c := Catalog{Schema: 1, Models: []CatalogModel{{ID: "test", ServingModelID: "active", Priority: 1, Actions: CatalogActions{Load: CatalogAction{true, "test"}, Benchmark: CatalogAction{true, "test"}}, Assets: []CatalogAsset{
		{ID: "size", Node: "NODE01", Path: p, ExpectedBytes: 3, Required: true, DownloadEnabled: true},
		{ID: "remote", Node: "NODE02", Path: "/never/inspect", ExpectedBytes: 99, Required: true, DownloadEnabled: true},
	}}}}
	calls := 0
	got := catalogSnapshot(c, "active", func(n string) (fs.FileInfo, error) {
		calls++
		if n != p {
			t.Fatalf("unexpected stat %s", n)
		}
		return os.Lstat(n)
	})
	if calls != 1 || got.ActiveModelID != "test" {
		t.Fatalf("wrong snapshot: %+v", got)
	}
	m := got.Models[0]
	if m.LocalBytes != 3 || m.ExpectedLocalBytes != 3 {
		t.Fatal("remote bytes incorrectly counted local")
	}
	if m.Assets[0].Present == nil || !*m.Assets[0].Present || m.Assets[0].DownloadEnabled {
		t.Fatal("existing destination downloadable")
	}
	if m.Assets[1].Present != nil || !strings.Contains(m.Assets[1].Verification, "REMOTE_NOT_CHECKED") {
		t.Fatal("remote absence fabricated")
	}
	if strings.Contains(m.Assets[0].Verification, "SHA256_PASS") {
		t.Fatal("stat turned into checksum validation")
	}
}

func TestCatalogMissingMismatchAndSymlinkBlock(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "model")
	link := filepath.Join(d, "link")
	if err := os.WriteFile(p, []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(p, link); err != nil {
		t.Fatal(err)
	}
	for _, a := range []CatalogAsset{{ID: "missing", Path: filepath.Join(d, "missing"), ExpectedBytes: 3, DownloadEnabled: true}, {ID: "mismatch", Path: p, ExpectedBytes: 4}, {ID: "link", Path: link, ExpectedBytes: 3}} {
		a.Node = "NODE01"
		a.Required = true
		c := Catalog{Models: []CatalogModel{{ID: "m", ServingModelID: "active", Actions: CatalogActions{Load: CatalogAction{true, "test"}, Benchmark: CatalogAction{true, "test"}}, Assets: []CatalogAsset{a}}}}
		m := catalogSnapshot(c, "active", os.Lstat).Models[0]
		if m.Actions.Load.Enabled || m.Actions.Benchmark.Enabled {
			t.Fatalf("bad asset enabled: %s", a.ID)
		}
		if a.ID != "missing" && m.Actions.Download.Enabled {
			t.Fatal("unsafe overwrite offered")
		}
	}
}

func TestCatalogDownloadStrictPinsAndSelection(t *testing.T) {
	a := CatalogAsset{ID: "x", Node: "NODE01", Path: "/home/funboy/models/example/model.gguf", ExpectedBytes: 3, Revision: strings.Repeat("a", 40), SHA256: strings.Repeat("b", 64)}
	a.URL = "https://huggingface.co/o/r/resolve/" + a.Revision + "/model.gguf"
	if err := validCatalogDownload(a); err != nil {
		t.Fatal(err)
	}
	bad := []CatalogAsset{a, a, a, a, a, a, a}
	bad[0].URL = "http://huggingface.co/o/r/resolve/" + a.Revision + "/model.gguf"
	bad[1].Revision = "main"
	bad[2].SHA256 = ""
	bad[3].Path = "/etc/passwd"
	bad[4].Node = "NODE02"
	bad[5].URL = "https://huggingface.co.evil.test/o/r/resolve/" + a.Revision + "/model.gguf"
	bad[6].URL = "https://huggingface.co/o/r/resolve/" + strings.Repeat("c", 40) + "/model.gguf"
	for i, v := range bad {
		if validCatalogDownload(v) == nil {
			t.Fatalf("unsafe pin accepted %d", i)
		}
	}
	if _, err := catalogDownloadAsset("https://attacker.invalid/model.gguf"); err == nil {
		t.Fatal("arbitrary client URL accepted")
	}
}

func TestCatalogHTTPAuthenticatedReadOnly(t *testing.T) {
	a := &App{cfg: Config{Model: "GLM5.3-Flash-CIRU-STRIX-IU4"}, token: "catalog-secret"}
	mux := http.NewServeMux()
	registerCatalogRoutes(a, mux)
	for _, tc := range []struct {
		method, auth string
		code         int
	}{{"GET", "", 401}, {"POST", "Bearer catalog-secret", 405}, {"GET", "Bearer catalog-secret", 200}} {
		r := httptest.NewRequest(tc.method, "/v1/catalog", nil)
		r.Header.Set("Authorization", tc.auth)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != tc.code {
			t.Fatalf("%s got%d: %s", tc.method, w.Code, w.Body.String())
		}
		if w.Code == 200 {
			var c Catalog
			if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
				t.Fatal(err)
			}
			if c.ActiveModelID != "glm-ciru-flash-w4" {
				t.Fatal("configured model mismatch")
			}
			if !strings.Contains(c.InventoryMethod, "no hash") {
				t.Fatal("inventory caveat lost")
			}
		}
	}
}
