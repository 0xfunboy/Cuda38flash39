package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The sole private-network exception is this test-only transport. Production
// exposes no endpoint, environment variable or config to bypass its IP policy.
type downloadFixtureTransport struct {
	endpoint *url.URL
	base     http.RoundTripper
}

func (f downloadFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	next := r.Clone(r.Context())
	u := *r.URL
	u.Scheme = f.endpoint.Scheme
	u.Host = f.endpoint.Host
	next.URL = &u
	return f.base.RoundTrip(next)
}

func nativeDownloadFixture(t *testing.T, handler http.HandlerFunc) *downloadManager {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	m := newDownloadManager(t.TempDir())
	if m.initErr != nil {
		t.Fatal(m.initErr)
	}
	m.reserve = 0
	m.free = func() (int64, error) { return 1 << 40, nil }
	endpoint, _ := url.Parse(server.URL)
	m.client = &http.Client{Transport: downloadFixtureTransport{endpoint, http.DefaultTransport}}
	t.Cleanup(func() {
		m.mu.Lock()
		if m.cancel != nil {
			m.cancel()
		}
		done := m.done
		m.mu.Unlock()
		if done != nil {
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Error("download worker did not stop")
			}
		}
		m.root.Close()
	})
	return m
}
func downloadDigest(s string) string { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:]) }
func downloadPlan(t *testing.T, m *downloadManager, hash string) DownloadJob {
	t.Helper()
	j, e := m.plan(context.Background(), downloadPlanRequest{URL: "https://models.example/file.bin", SHA256: hash})
	if e != nil {
		t.Fatal(e)
	}
	return j
}
func downloadWait(t *testing.T, m *downloadManager, id string) DownloadJob {
	t.Helper()
	m.mu.Lock()
	done := m.done
	m.mu.Unlock()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("download did not finish")
	}
	j, _ := m.snapshot(id)
	return j
}
func downloadSeedPart(t *testing.T, m *downloadManager, j DownloadJob, data string) {
	t.Helper()
	if e := m.root.WriteFile(j.ID+"/data/file.bin.part", []byte(data), 0600); e != nil {
		t.Fatal(e)
	}
	m.mu.Lock()
	m.jobs[j.ID].Status = "PAUSED"
	if e := m.persistLocked(m.jobs[j.ID]); e != nil {
		t.Fatal(e)
	}
	m.mu.Unlock()
}

func TestDownloadPublicURLPolicy(t *testing.T) {
	for _, raw := range []string{"http://example.com/model", "https://user:password@example.com/model", "https://example.com:8080/file", "https://127.0.0.1/file", "https://10.55.0.1/file", "https://[::1]/file", "https://[::ffff:127.0.0.1]/file", "https://169.254.169.254/latest/meta-data", "https://100.100.100.200/file", "https://example.com/file?token=secret", "https://example.com/file#secret", "https://localhost/file"} {
		if _, e := validateDownloadURL(raw, false); e == nil {
			t.Errorf("unsafe URL accepted: %s", raw)
		}
	}
	for _, ip := range []string{"192.0.2.1", "198.18.0.1", "224.0.0.1", "fc00::1", "fe80::1", "64:ff9b::7f00:1"} {
		if publicDownloadIP(net.ParseIP(ip)) {
			t.Errorf("special IP accepted: %s", ip)
		}
	}
	if _, e := validateDownloadURL("https://huggingface.co/owner/model/resolve/main/model.gguf?download=true", false); e != nil {
		t.Fatal(e)
	}
	client := newDownloadHTTPClient()
	for _, raw := range []string{"https://127.0.0.1/model", "http://example.com/file", "https://10.55.0.2:18100/status"} {
		u, _ := url.Parse(raw)
		if e := client.CheckRedirect(&http.Request{URL: u}, nil); e == nil {
			t.Error("unsafe redirect accepted")
		}
	}
	_, e := client.Transport.(*http.Transport).DialContext(context.Background(), "tcp", "127.0.0.1:443")
	if e == nil {
		t.Fatal("private DNS/dial destination accepted")
	}
	if client.Transport.(*http.Transport).Proxy != nil || client.Jar != nil {
		t.Fatal("ambient proxy or cookies enabled")
	}
}

func TestDownloadCompleteHashAndReadOnlyStatus(t *testing.T) {
	var gets atomic.Int32
	m := nativeDownloadFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("credential forwarded")
		}
		w.Header().Set("Content-Length", "6")
		w.Header().Set("ETag", `"v1"`)
		if r.Method == "GET" {
			gets.Add(1)
			fmt.Fprint(w, "abcdef")
		}
	})
	j := downloadPlan(t, m, downloadDigest("abcdef"))
	if gets.Load() != 0 {
		t.Fatal("plan downloaded file body")
	}
	if _, e := m.start(j.ID, false); e != nil {
		t.Fatal(e)
	}
	final := downloadWait(t, m, j.ID)
	if final.Status != "COMPLETE" || final.Bytes != 6 || final.Files[0].Verification != "SIZE_AND_SHA256_VERIFIED" {
		t.Fatalf("bad receipt: %+v", final)
	}
	file, _ := os.Stat(filepath.Join(final.Destination, "file.bin"))
	part, _ := os.Stat(filepath.Join(final.Destination, "file.bin.part"))
	if !os.SameFile(file, part) {
		t.Fatal("publication duplicated data rather than retaining hard-link recovery alias")
	}
	for i := 0; i < 5; i++ {
		m.snapshot(j.ID)
	}
	if gets.Load() != 1 {
		t.Fatal("status re-downloaded data")
	}
	if _, e := m.start(j.ID, true); e == nil {
		t.Fatal("completed file resumed")
	}
}

func TestDownloadResumeRealHTTP206(t *testing.T) {
	var ranges atomic.Int32
	m := nativeDownloadFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		if r.Method == "HEAD" {
			w.Header().Set("Content-Length", "6")
			return
		}
		if r.Header.Get("Range") != "bytes=3-" || r.Header.Get("If-Range") != `"v1"` {
			t.Error("missing exact resume headers")
		}
		ranges.Add(1)
		w.Header().Set("Content-Length", "3")
		w.Header().Set("Content-Range", "bytes 3-5/6")
		w.WriteHeader(206)
		fmt.Fprint(w, "def")
	})
	j := downloadPlan(t, m, downloadDigest("abcdef"))
	downloadSeedPart(t, m, j, "abc")
	if _, e := m.start(j.ID, true); e != nil {
		t.Fatal(e)
	}
	final := downloadWait(t, m, j.ID)
	if final.Status != "COMPLETE" || ranges.Load() != 1 {
		t.Fatalf("resume failed: %+v", final)
	}
}

func TestDownloadResumeRejectsMutationAndInvalidRanges(t *testing.T) {
	for _, tc := range []struct {
		name               string
		status             int
		etag, contentRange string
	}{{"ignored", 200, `"v1"`, ""}, {"changed", 206, `"v2"`, "bytes 3-5/6"}, {"missing_etag", 206, "", "bytes 3-5/6"}, {"weak_etag", 206, `W/"v1"`, "bytes 3-5/6"}, {"wrong_offset", 206, `"v1"`, "bytes 2-4/6"}, {"wrong_total", 206, `"v1"`, "bytes 3-5/7"}, {"range416", 416, `"v1"`, "bytes */6"}, {"trailing", 206, `"v1"`, "bytes 3-5/6 junk"}} {
		t.Run(tc.name, func(t *testing.T) {
			m := nativeDownloadFixture(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("ETag", `"v1"`)
				if r.Method == "HEAD" {
					w.Header().Set("Content-Length", "6")
					return
				}
				w.Header().Set("ETag", tc.etag)
				w.Header().Set("Content-Length", "3")
				w.Header().Set("Content-Range", tc.contentRange)
				w.WriteHeader(tc.status)
				fmt.Fprint(w, "def")
			})
			j := downloadPlan(t, m, "")
			downloadSeedPart(t, m, j, "abc")
			if _, e := m.start(j.ID, true); e != nil {
				t.Fatal(e)
			}
			out := downloadWait(t, m, j.ID)
			if out.Status != "FAILED" {
				t.Fatalf("accepted invalid resume: %+v", out)
			}
			part, _ := m.root.ReadFile(j.ID + "/data/file.bin.part")
			if string(part) != "abc" {
				t.Fatal("partial changed despite invalid source")
			}
		})
	}
}

func TestDownloadCompletePartAvoids416AndHashMismatchPreserved(t *testing.T) {
	for _, hash := range []string{downloadDigest("abcdef"), strings.Repeat("0", 64)} {
		t.Run(hash[:8], func(t *testing.T) {
			var gets atomic.Int32
			m := nativeDownloadFixture(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Length", "6")
				if r.Method == "GET" {
					gets.Add(1)
					w.WriteHeader(416)
				}
			})
			j := downloadPlan(t, m, hash)
			downloadSeedPart(t, m, j, "abcdef")
			if _, e := m.start(j.ID, true); e != nil {
				t.Fatal(e)
			}
			out := downloadWait(t, m, j.ID)
			if gets.Load() != 0 {
				t.Fatal("complete partial triggered GET/416")
			}
			if hash == downloadDigest("abcdef") && out.Status != "COMPLETE" {
				t.Fatal(out.Error)
			}
			if hash != downloadDigest("abcdef") && (out.Status != "FAILED" || !strings.Contains(out.Error, "SHA256")) {
				t.Fatal("hash mismatch not caught")
			}
		})
	}
}

func TestDownloadPauseCancelRestartAndConcurrency(t *testing.T) {
	started := make(chan struct{}, 1)
	m := nativeDownloadFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Content-Length", "6")
		if r.Method == "HEAD" {
			return
		}
		fmt.Fprint(w, "abc")
		w.(http.Flusher).Flush()
		started <- struct{}{}
		<-r.Context().Done()
	})
	j := downloadPlan(t, m, downloadDigest("abcdef"))
	if _, e := m.start(j.ID, false); e != nil {
		t.Fatal(e)
	}
	<-started
	j2 := downloadPlan(t, m, "")
	if _, e := m.start(j2.ID, false); e == nil {
		t.Fatal("second active job admitted")
	}
	if _, e := m.pause(j.ID, false); e != nil {
		t.Fatal(e)
	}
	paused := downloadWait(t, m, j.ID)
	if paused.Status != "PAUSED" {
		t.Fatal(paused.Status)
	}
	if _, e := m.pause(j.ID, true); e != nil {
		t.Fatal(e)
	}
	cancelled, _ := m.snapshot(j.ID)
	if cancelled.Status != "CANCELLED" {
		t.Fatal(cancelled.Status)
	}
	m.mu.Lock()
	m.jobs[j.ID].Status = "VERIFYING"
	m.persistLocked(m.jobs[j.ID])
	m.mu.Unlock()
	restarted := newDownloadManager(m.dir)
	defer restarted.root.Close()
	out, ok := restarted.snapshot(j.ID)
	if !ok || out.Status != "PAUSED" || restarted.active != "" {
		t.Fatalf("restart did not preserve manual-resume state: %+v", out)
	}
}

func TestDownloadDiskAndPathSafety(t *testing.T) {
	m := nativeDownloadFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "6")
		w.Header().Set("ETag", `"v1"`)
		if r.Method == "GET" {
			fmt.Fprint(w, "abcdef")
		}
	})
	j := downloadPlan(t, m, "")
	m.free = func() (int64, error) { return 5, nil }
	if _, e := m.start(j.ID, false); e == nil {
		t.Fatal("disk deficit admitted")
	}
	m.free = func() (int64, error) { return 1 << 40, nil }
	outside := filepath.Join(t.TempDir(), "untouched")
	if e := os.WriteFile(outside, []byte("secret"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := m.root.Symlink(outside, j.ID+"/data/file.bin.part"); e != nil {
		t.Fatal(e)
	}
	if _, e := m.start(j.ID, false); e == nil {
		t.Fatal("symlink partial admitted")
	}
	raw, _ := os.ReadFile(outside)
	if string(raw) != "secret" {
		t.Fatal("symlink target changed")
	}
	for _, p := range []string{"../file", "/file", "a/../../file", "a\\file", ".env", "a/.ssh/key", "a/../file", "file.part"} {
		if downloadPath(p) {
			t.Errorf("unsafe path accepted %q", p)
		}
	}
}

func TestDownloadMissingValidatorNoHashDoesNotResume(t *testing.T) {
	m := nativeDownloadFixture(t, func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Content-Length", "6") })
	j := downloadPlan(t, m, "")
	downloadSeedPart(t, m, j, "abc")
	if _, e := m.start(j.ID, true); e != nil {
		t.Fatal(e)
	}
	out := downloadWait(t, m, j.ID)
	if !strings.Contains(out.Error, "resume requires") {
		t.Fatal(out.Error)
	}
}

func TestDownloadPlanQuotaAndHardLinkSafety(t *testing.T) {
	m := nativeDownloadFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "6")
		w.Header().Set("ETag", `"v1"`)
		if r.Method == "GET" {
			fmt.Fprint(w, "abcdef")
		}
	})
	m.free = func() (int64, error) { return 1024, nil }
	if _, e := m.plan(context.Background(), downloadPlanRequest{URL: "https://models.example/file.bin"}); e == nil {
		t.Fatal("plan admitted without receipt/reserve headroom")
	}
	if len(m.jobs) != 0 {
		t.Fatal("rejected plan persisted")
	}
	m.free = func() (int64, error) { return 1 << 40, nil }
	j := downloadPlan(t, m, "")
	downloadSeedPart(t, m, j, "abc")
	if e := m.root.Link(j.ID+"/data/file.bin.part", j.ID+"/data/shared.bin"); e != nil {
		t.Fatal(e)
	}
	if _, e := m.start(j.ID, true); e != nil {
		t.Fatal(e)
	}
	out := downloadWait(t, m, j.ID)
	if !strings.Contains(out.Error, "hard links") {
		t.Fatal(out.Error)
	}
	raw, _ := m.root.ReadFile(j.ID + "/data/shared.bin")
	if string(raw) != "abc" {
		t.Fatal("shared inode overwritten")
	}
}

func TestDownloadRepositorySourcesAndPinning(t *testing.T) {
	commit := strings.Repeat("a", 40)
	hash := downloadDigest("abcdef")
	m := nativeDownloadFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/openapi/"):
			jsonReply(w, 200, map[string]any{"success": true, "data": map[string]any{"models": []any{map[string]any{"id": "Qwen/model", "tags": []string{"safetensors"}}}}})
		case strings.HasSuffix(r.URL.Path, "/repo/files"):
			jsonReply(w, 200, map[string]any{"Code": 200, "Data": map[string]any{"Files": []any{map[string]any{"Type": "blob", "Path": "weights/model.safetensors", "Size": 6, "Sha256": hash, "Revision": commit}}}})
		case strings.Contains(r.URL.Path, "/revision/"):
			jsonReply(w, 200, map[string]any{"sha": commit, "siblings": []any{map[string]any{"rfilename": "model.gguf", "size": 6, "lfs": map[string]any{"sha256": hash}}, map[string]any{"rfilename": ".gitattributes", "size": 1}}})
		default:
			jsonReply(w, 200, []any{map[string]any{"id": "Qwen/model", "tags": []string{"gguf"}}})
		}
	})
	for _, source := range []string{"huggingface", "modelscope"} {
		rows, e := m.search(context.Background(), source, "Qwen")
		if e != nil || len(rows) != 1 {
			t.Fatalf("search %s %v", source, e)
		}
		listing, e := m.files(context.Background(), source, "Qwen/model", "")
		if e != nil || len(listing.Files) != 1 {
			t.Fatalf("files %s %+v %v", source, listing, e)
		}
		f := listing.Files[0]
		if f.SHA256 != hash || f.Revision != commit || !strings.Contains(f.URL, commit) {
			t.Fatal("file not pinned")
		}
		j, e := m.plan(context.Background(), downloadPlanRequest{Source: source, Repo: "Qwen/model", Revision: listing.Revision, Files: []string{f.Path}})
		if e != nil || j.Status != "PLANNED" {
			t.Fatalf("plan %s %v", source, e)
		}
	}
	f, e := m.directFile(context.Background(), "https://huggingface.co/Qwen/model/blob/main/model.gguf", "")
	if e != nil || !strings.Contains(f.URL, "/resolve/"+commit+"/") {
		t.Fatal("HF blob link not pinned/normalized", e)
	}
	if _, e := m.directFile(context.Background(), "https://huggingface.co/Qwen/model", ""); e == nil {
		t.Fatal("repository HTML treated as weights")
	}
}

func TestDownloadRoutesAuthAndExplicitAdmission(t *testing.T) {
	a, e := newApp(coreConfig(t))
	if e != nil {
		t.Fatal(e)
	}
	m := nativeDownloadFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "6")
		w.Header().Set("ETag", `"v1"`)
		if r.Method == "GET" {
			fmt.Fprint(w, "abcdef")
		}
	})
	downloadManagers.Store(a, m)
	t.Cleanup(func() { downloadManagers.Delete(a) })
	mux := http.NewServeMux()
	a.registerDownloadRoutes(mux)
	request := func(method, path, body string, auth bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if auth {
			r.Header.Set("Authorization", "Bearer "+a.currentToken())
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	if request("GET", "/v1/downloads/options", "", false).Code != 401 {
		t.Fatal("unauthenticated discovery allowed")
	}
	plan := request("POST", "/v1/downloads/plan", `{"url":"https://models.example/file.bin"}`, true)
	if plan.Code != 201 {
		t.Fatal(plan.Body.String())
	}
	var j DownloadJob
	json.Unmarshal(plan.Body.Bytes(), &j)
	if request("POST", "/v1/downloads/jobs/"+j.ID+"/start", `{}`, true).Code != 400 {
		t.Fatal("missing confirmation accepted")
	}
	settings := APISettings{Chat: true, Workspaces: true, LegacyCoding: true, Operations: false}
	a.apiSettings.Store(&settings)
	if request("POST", "/v1/downloads/jobs/"+j.ID+"/start", `{"confirm":true}`, true).Code != 403 {
		t.Fatal("disabled operations allowed download")
	}
	if request("POST", "/v1/downloads/jobs/"+j.ID+"/pause", `{"confirm":true}`, true).Code != 202 {
		t.Fatal("cleanup incorrectly gated")
	}
	if request("GET", "/v1/downloads/jobs", "", true).Code != 200 {
		t.Fatal("GET status gated")
	}
}

func TestDownloadPublicMetadataOptIn(t *testing.T) {
	if os.Getenv("HALOCLU_TEST_PUBLIC_METADATA") != "1" {
		t.Skip("explicit metadata-only Internet test; never downloads model files")
	}
	m := newDownloadManager(t.TempDir())
	if m.initErr != nil {
		t.Fatal(m.initErr)
	}
	defer m.root.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for _, source := range []string{"huggingface", "modelscope"} {
		listing, e := m.files(ctx, source, "Qwen/Qwen2.5-0.5B-Instruct", "")
		if e != nil {
			t.Fatalf("%s metadata: %v", source, e)
		}
		if len(listing.Files) == 0 {
			t.Fatal("empty listing")
		}
		t.Logf("%s metadata only: files=%d revision=%s first_file_commit=%s", source, len(listing.Files), listing.Revision, listing.Files[0].Revision)
	}
	items, e := m.search(ctx, "modelscope", "Qwen")
	if e != nil || len(items) == 0 {
		t.Fatalf("ModelScope public search failed: %v", e)
	}
	t.Logf("ModelScope anonymous search: %d returned records; zero model files acquired", len(items))
}
