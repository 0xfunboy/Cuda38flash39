package app

// Public model acquisition has its own HTTP client. It never inherits proxy,
// cookies, API authentication or the paired-runtime transport.
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var downloadRepoRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*/[A-Za-z0-9][A-Za-z0-9_.-]*$`)
var downloadCommitRE = regexp.MustCompile(`^[a-f0-9]{40}$`)
var downloadHashRE = regexp.MustCompile(`^[a-f0-9]{64}$`)
var downloadIDRE = regexp.MustCompile(`^[a-f0-9]{32}$`)

func publicDownloadIP(ip net.IP) bool {
	a, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	a = a.Unmap()
	if !a.IsGlobalUnicast() || a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() {
		return false
	}
	// Reject non-public special-use ranges, including IPv4-mapped IPv6 and
	// translation/tunnel mechanisms that could reach private IPv4 destinations.
	for _, raw := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "2001::/23", "2002::/16"} {
		if netip.MustParsePrefix(raw).Contains(a) {
			return false
		}
	}
	return true
}

func validateDownloadURL(raw string, redirect bool) (*url.URL, error) {
	u, e := url.Parse(raw)
	if e != nil || len(raw) > 8192 || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" || (u.Port() != "" && u.Port() != "443") {
		return nil, errors.New("public HTTPS URL on port 443 required; no credentials or fragments")
	}
	if strings.ContainsAny(u.Host, "\\%\r\n\t") || strings.EqualFold(u.Hostname(), "localhost") || strings.HasSuffix(strings.ToLower(u.Hostname()), ".localhost") {
		return nil, errors.New("local or malformed hostname refused")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && !publicDownloadIP(ip) {
		return nil, errors.New("private or special-use destination refused")
	}
	// Signed CDN redirects may contain short-lived query credentials. They are
	// never saved or reported. User-submitted URLs cannot carry such secrets.
	if !redirect {
		for k, values := range u.Query() {
			if k != "download" || len(values) != 1 || (values[0] != "true" && values[0] != "1") {
				return nil, errors.New("query-bearing input URLs refused; use a public canonical download URL")
			}
		}
	}
	return u, nil
}

func newDownloadHTTPClient() *http.Client {
	transport := &http.Transport{Proxy: nil, DisableCompression: true, TLSHandshakeTimeout: 15 * time.Second, ResponseHeaderTimeout: 30 * time.Second, IdleConnTimeout: 30 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(address)
		if e != nil || port != "443" {
			return nil, errors.New("download destination port refused")
		}
		ips, e := net.DefaultResolver.LookupIPAddr(ctx, host)
		if e != nil || len(ips) == 0 {
			return nil, errors.New("public download DNS lookup failed")
		}
		for _, ip := range ips {
			if !publicDownloadIP(ip.IP) {
				return nil, errors.New("download DNS resolved to private or special-use address")
			}
		}
		var last error
		for _, ip := range ips {
			conn, err := (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(ip.IP.String(), port))
			if err == nil {
				return conn, nil
			}
			last = err
		}
		return nil, last
	}
	return &http.Client{Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 6 {
			return errors.New("download redirect limit reached")
		}
		_, e := validateDownloadURL(req.URL.String(), true)
		return e
	}}
}

func (m *downloadManager) metadata(ctx context.Context, raw string, value any) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if e != nil {
		return errors.New("invalid metadata request")
	}
	req.Header.Set("User-Agent", "HaloClu-model-download/1")
	res, e := m.client.Do(req)
	if e != nil {
		return errors.New("model-source metadata request failed (no credentials sent)")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("model-source metadata HTTP %d; public repositories only", res.StatusCode)
	}
	rawBody, e := io.ReadAll(io.LimitReader(res.Body, (8<<20)+1))
	if e != nil || len(rawBody) > 8<<20 {
		return errors.New("metadata response failed or exceeded 8 MiB")
	}
	if e = json.Unmarshal(rawBody, value); e != nil {
		return errors.New("model source returned invalid metadata JSON")
	}
	return nil
}

type downloadSearchItem struct {
	Source string   `json:"source"`
	Repo   string   `json:"repo"`
	URL    string   `json:"url"`
	Tags   []string `json:"tags"`
}
type downloadListing struct {
	Source   string         `json:"source"`
	Repo     string         `json:"repo"`
	Revision string         `json:"revision"`
	Files    []DownloadFile `json:"files"`
	Warning  string         `json:"warning"`
}

func (m *downloadManager) search(ctx context.Context, source, q string) ([]downloadSearchItem, error) {
	if len(q) < 2 || len(q) > 160 {
		return nil, errors.New("search requires 2–160 characters")
	}
	items := []downloadSearchItem{}
	if source == "huggingface" {
		var rows []struct {
			ID   string   `json:"id"`
			Tags []string `json:"tags"`
		}
		if e := m.metadata(ctx, "https://huggingface.co/api/models?search="+url.QueryEscape(q)+"&limit=20&full=false", &rows); e != nil {
			return nil, e
		}
		for _, row := range rows {
			if downloadRepoRE.MatchString(row.ID) {
				items = append(items, downloadSearchItem{source, row.ID, "https://huggingface.co/" + row.ID, row.Tags})
			}
		}
	} else if source == "modelscope" {
		var result struct {
			Success bool `json:"success"`
			Data    struct {
				Models []struct {
					ID   string   `json:"id"`
					Tags []string `json:"tags"`
				} `json:"models"`
			} `json:"data"`
		}
		if e := m.metadata(ctx, "https://modelscope.cn/openapi/v1/models?search="+url.QueryEscape(q)+"&page_size=20", &result); e != nil {
			return nil, e
		}
		if !result.Success {
			return nil, errors.New("ModelScope search returned an unsuccessful response")
		}
		for _, row := range result.Data.Models {
			if downloadRepoRE.MatchString(row.ID) {
				items = append(items, downloadSearchItem{source, row.ID, "https://modelscope.cn/models/" + row.ID, row.Tags})
			}
		}
	} else {
		return nil, errors.New("source must be huggingface or modelscope; vLLM is a runtime, VLM is a model category")
	}
	if len(items) > 20 {
		items = items[:20]
	}
	return items, nil
}

func downloadPath(p string) bool {
	if p == "" || len(p) > 1024 || cleanRel(p) != nil || strings.ContainsAny(p, "\x00\r\n\t") || strings.HasSuffix(p, ".part") {
		return false
	}
	return true
}

func (m *downloadManager) files(ctx context.Context, source, repo, revision string) (downloadListing, error) {
	out := downloadListing{Source: source, Repo: repo, Files: []DownloadFile{}, Warning: "Availability is not runtime compatibility or quality qualification. Select only the files you need."}
	if !downloadRepoRE.MatchString(repo) || len(revision) > 160 {
		return out, errors.New("valid organization/repository and revision required")
	}
	if source == "huggingface" {
		if revision == "" {
			revision = "main"
		}
		var result struct {
			SHA      string `json:"sha"`
			Siblings []struct {
				Path string `json:"rfilename"`
				Size int64  `json:"size"`
				LFS  *struct {
					SHA string `json:"sha256"`
				} `json:"lfs"`
			} `json:"siblings"`
		}
		if e := m.metadata(ctx, "https://huggingface.co/api/models/"+repo+"/revision/"+url.PathEscape(revision)+"?blobs=true", &result); e != nil {
			return out, e
		}
		if !downloadCommitRE.MatchString(result.SHA) {
			return out, errors.New("Hugging Face did not supply an immutable commit")
		}
		out.Revision = result.SHA
		for _, f := range result.Siblings {
			if !downloadPath(f.Path) || f.Size < 0 {
				continue
			}
			hash := ""
			if f.LFS != nil && downloadHashRE.MatchString(f.LFS.SHA) {
				hash = f.LFS.SHA
			}
			out.Files = append(out.Files, DownloadFile{Path: f.Path, Size: f.Size, SHA256: hash, Revision: result.SHA, URL: "https://huggingface.co/" + repo + "/resolve/" + result.SHA + "/" + escapedDownloadPath(f.Path), Status: "PLANNED"})
		}
	} else if source == "modelscope" {
		if revision == "" {
			revision = "master"
		}
		out.Revision = revision
		var result struct {
			Code int `json:"Code"`
			Data struct {
				Files []struct {
					Path     string `json:"Path"`
					Size     int64  `json:"Size"`
					SHA      string `json:"Sha256"`
					Revision string `json:"Revision"`
					Type     string `json:"Type"`
				} `json:"Files"`
			} `json:"Data"`
		}
		if e := m.metadata(ctx, "https://modelscope.cn/api/v1/models/"+repo+"/repo/files?Revision="+url.QueryEscape(revision)+"&Recursive=true", &result); e != nil {
			return out, e
		}
		if result.Code != 200 {
			return out, errors.New("ModelScope file listing failed")
		}
		for _, f := range result.Data.Files {
			if f.Type != "blob" || !downloadPath(f.Path) || f.Size < 0 || !downloadCommitRE.MatchString(f.Revision) || !downloadHashRE.MatchString(f.SHA) {
				continue
			}
			out.Files = append(out.Files, DownloadFile{Path: f.Path, Size: f.Size, SHA256: f.SHA, Revision: f.Revision, URL: "https://modelscope.cn/api/v1/models/" + repo + "/repo?Revision=" + f.Revision + "&FilePath=" + url.QueryEscape(f.Path), Status: "PLANNED"})
		}
		out.Warning += " ModelScope files are individually pinned to their returned commit and SHA256; this is not an atomic whole-repository revision."
	} else {
		return out, errors.New("unsupported repository source")
	}
	if len(out.Files) == 0 || len(out.Files) > 10000 {
		return out, errors.New("no supported regular files, or repository exceeds 10000-file listing limit")
	}
	return out, nil
}

func escapedDownloadPath(p string) string {
	var escaped []string
	for _, part := range strings.Split(p, "/") {
		escaped = append(escaped, url.PathEscape(part))
	}
	return strings.Join(escaped, "/")
}

func strongDownloadETag(s string) string {
	if strings.HasPrefix(s, "\"") && strings.HasSuffix(s, "\"") && len(s) <= 512 {
		return s
	}
	return ""
}

func (m *downloadManager) directFile(ctx context.Context, raw, hash string) (DownloadFile, error) {
	u, e := validateDownloadURL(raw, false)
	if e != nil {
		return DownloadFile{}, e
	}
	if strings.EqualFold(u.Hostname(), "huggingface.co") {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) == 2 {
			return DownloadFile{}, errors.New("Hugging Face repository page: choose the repository in Search and select files before planning")
		}
		if len(parts) >= 5 && (parts[2] == "blob" || parts[2] == "resolve") {
			listing, err := m.files(ctx, "huggingface", parts[0]+"/"+parts[1], parts[3])
			if err != nil {
				return DownloadFile{}, err
			}
			for _, file := range listing.Files {
				if file.Path == strings.Join(parts[4:], "/") {
					if hash != "" {
						if !downloadHashRE.MatchString(hash) || file.SHA256 != "" && file.SHA256 != hash {
							return DownloadFile{}, errors.New("supplied SHA256 does not match Hugging Face metadata")
						}
						file.SHA256 = hash
					}
					return file, nil
				}
			}
			return DownloadFile{}, errors.New("file missing from pinned Hugging Face revision")
		}
	}
	if strings.EqualFold(u.Hostname(), "modelscope.cn") && strings.HasPrefix(u.Path, "/models/") {
		return DownloadFile{}, errors.New("ModelScope repository page: search the repository and select files before planning")
	}
	name := path.Base(u.Path)
	if !downloadPath(name) || name == "/" {
		return DownloadFile{}, errors.New("URL must end with a safe filename")
	}
	if hash != "" && !downloadHashRE.MatchString(hash) {
		return DownloadFile{}, errors.New("SHA256 must contain 64 lowercase hex digits")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "HEAD", u.String(), nil)
	res, e := m.client.Do(req)
	if e != nil {
		return DownloadFile{}, errors.New("public URL HEAD request failed")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 || res.ContentLength < 0 || strings.Contains(strings.ToLower(res.Header.Get("Content-Type")), "text/html") {
		return DownloadFile{}, errors.New("URL must support HEAD with a known Content-Length; unknown-size acquisition is not enabled")
	}
	modified := res.Header.Get("Last-Modified")
	if _, err := http.ParseTime(modified); err != nil {
		modified = ""
	}
	return DownloadFile{Path: name, URL: u.String(), Size: res.ContentLength, SHA256: hash, ETag: strongDownloadETag(res.Header.Get("ETag")), LastModified: modified, Status: "PLANNED"}, nil
}

func parseDownloadRange(value string, offset, total int64) (int64, error) {
	var start, end, size int64
	// Split and canonical recomposition reject trailing garbage and wildcards.
	if _, e := fmt.Sscanf(value, "bytes %d-%d/%d", &start, &end, &size); e != nil || value != "bytes "+strconv.FormatInt(start, 10)+"-"+strconv.FormatInt(end, 10)+"/"+strconv.FormatInt(size, 10) || start != offset || end < start || end >= size || size != total {
		return 0, errors.New("resume Content-Range mismatch; partial preserved")
	}
	return end - start + 1, nil
}
