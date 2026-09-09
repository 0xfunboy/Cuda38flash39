package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const downloadReserveBytes int64 = 16 << 30

type DownloadFile struct {
	Path         string `json:"path"`
	URL          string `json:"url"`
	Revision     string `json:"revision,omitempty"`
	Size         int64  `json:"size"`
	SHA256       string `json:"sha256,omitempty"`
	Bytes        int64  `json:"bytes"`
	Status       string `json:"status"`
	Verification string `json:"verification,omitempty"`
	ETag         string `json:"etag,omitempty"`
	LastModified string `json:"last_modified,omitempty"`
}
type DownloadJob struct {
	ID          string         `json:"id"`
	Source      string         `json:"source"`
	Repo        string         `json:"repo,omitempty"`
	Revision    string         `json:"revision,omitempty"`
	Status      string         `json:"status"`
	Created     string         `json:"created"`
	Updated     string         `json:"updated"`
	Destination string         `json:"destination"`
	Files       []DownloadFile `json:"files"`
	Bytes       int64          `json:"bytes"`
	TotalBytes  int64          `json:"total_bytes"`
	Rate        float64        `json:"bytes_per_second"`
	ETA         *float64       `json:"eta_seconds"`
	Error       string         `json:"error,omitempty"`
	Warning     string         `json:"warning"`
}
type downloadManager struct {
	mu      sync.Mutex
	dir     string
	root    *os.Root
	client  *http.Client
	jobs    map[string]*DownloadJob
	active  string
	cancel  context.CancelFunc
	done    chan struct{}
	closing bool
	initErr error
	reserve int64 // Production is fixed; tests use a tiny isolated filesystem allowance.
	free    func() (int64, error)
}

var downloadManagers sync.Map

func newDownloadManager(dir string) *downloadManager {
	m := &downloadManager{dir: dir, client: newDownloadHTTPClient(), jobs: map[string]*DownloadJob{}, reserve: downloadReserveBytes}
	if e := os.MkdirAll(dir, 0700); e != nil {
		m.initErr = e
		return m
	}
	if st, e := os.Lstat(dir); e != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		m.initErr = errors.New("download root must be a real directory")
		return m
	}
	root, e := os.OpenRoot(dir)
	if e != nil {
		m.initErr = e
		return m
	}
	m.root = root
	m.free = func() (int64, error) {
		var s syscall.Statfs_t
		e := syscall.Statfs(dir, &s)
		return int64(s.Bavail) * int64(s.Bsize), e
	}
	entries, e := os.ReadDir(dir)
	if e != nil {
		m.initErr = e
		return m
	}
	for _, entry := range entries {
		if !entry.IsDir() || !downloadIDRE.MatchString(entry.Name()) {
			continue
		}
		f, e := root.OpenFile(entry.Name()+"/job.json", os.O_RDONLY|syscall.O_NOFOLLOW, 0)
		if e != nil {
			continue
		}
		raw, e := io.ReadAll(io.LimitReader(f, (2<<20)+1))
		f.Close()
		if e != nil || len(raw) > 2<<20 {
			continue
		}
		var job DownloadJob
		if json.Unmarshal(raw, &job) != nil || job.ID != entry.Name() || len(job.Files) == 0 || len(job.Files) > 128 {
			continue
		}
		valid := true
		var total int64
		for _, file := range job.Files {
			if !downloadPath(file.Path) || file.Size < 0 || file.Size > 8<<40 {
				valid = false
				break
			}
			if _, e := validateDownloadURL(file.URL, true); e != nil {
				valid = false
				break
			}
			total += file.Size
		}
		if !valid || total != job.TotalBytes {
			continue
		}
		job.Destination = filepath.Join(dir, job.ID, "data")
		job.Rate = 0
		job.ETA = nil
		if job.Status == "RUNNING" || job.Status == "VERIFYING" || job.Status == "PAUSING" || job.Status == "CANCELLING" {
			job.Status = "PAUSED"
			job.Error = "Gateway stopped during acquisition. Partial files preserved; explicit resume required."
		}
		m.jobs[job.ID] = &job
		if e := m.reconcileLocked(&job); e != nil {
			job.Status = "FAILED"
			job.Error = e.Error()
		}
		if e := m.persistLocked(&job); e != nil {
			m.initErr = e
			return m
		}
	}
	return m
}

func downloadsFor(a *App) *downloadManager {
	if existing, ok := downloadManagers.Load(a); ok {
		return existing.(*downloadManager)
	}
	// Serialize initialization to avoid two managers recovering the same files.
	downloadInitMu.Lock()
	defer downloadInitMu.Unlock()
	if existing, ok := downloadManagers.Load(a); ok {
		return existing.(*downloadManager)
	}
	m := newDownloadManager(filepath.Join(a.cfg.StateDir, "model-downloads"))
	downloadManagers.Store(a, m)
	return m
}

var downloadInitMu sync.Mutex

func (m *downloadManager) persistLocked(j *DownloadJob) error {
	j.Updated = time.Now().UTC().Format(time.RFC3339)
	raw, e := json.MarshalIndent(j, "", "  ")
	if e != nil {
		return e
	}
	tmp := j.ID + "/job-" + id() + ".tmp"
	f, e := m.root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0600)
	if e != nil {
		return errors.New("cannot persist download receipt")
	}
	_, e = f.Write(raw)
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e == nil {
		e = m.root.Rename(tmp, j.ID+"/job.json")
	}
	if e != nil {
		_ = m.root.Remove(tmp)
		return errors.New("cannot commit download receipt")
	}
	directory, err := m.root.Open(j.ID)
	if err != nil {
		return errors.New("cannot open download receipt directory")
	}
	err = directory.Sync()
	directory.Close()
	if err != nil {
		return errors.New("cannot sync download receipt directory")
	}
	return nil
}

func (m *downloadManager) noLinks(p string) error {
	parts := strings.Split(p, "/")
	for i := range parts {
		st, e := m.root.Lstat(strings.Join(parts[:i+1], "/"))
		if os.IsNotExist(e) {
			return nil
		}
		if e != nil || st.Mode()&os.ModeSymlink != 0 {
			return errors.New("symlink or inaccessible download path refused")
		}
	}
	return nil
}

func (m *downloadManager) regularSize(p string) (int64, error) {
	if e := m.noLinks(p); e != nil {
		return 0, e
	}
	st, e := m.root.Lstat(p)
	if os.IsNotExist(e) {
		return 0, nil
	}
	if e != nil || !st.Mode().IsRegular() {
		return 0, errors.New("download path must be a regular file")
	}
	return st.Size(), nil
}

func (m *downloadManager) reconcileLocked(j *DownloadJob) error {
	var completed int64
	for i := range j.Files {
		f := &j.Files[i]
		base := j.ID + "/data/" + f.Path
		part, e := m.regularSize(base + ".part")
		if e != nil {
			return e
		}
		if part > f.Size {
			return errors.New("partial exceeds pinned size; preserved for manual review")
		}
		if st, e := m.root.Lstat(base); e == nil {
			if !st.Mode().IsRegular() || st.Size() != f.Size {
				return errors.New("final file identity/size changed; no overwrite permitted")
			}
			if f.Status != "COMPLETE" {
				return errors.New("unrecorded final file exists; preserved for manual review")
			}
			f.Bytes = f.Size
		} else if !os.IsNotExist(e) {
			return e
		} else {
			if f.Status == "COMPLETE" {
				return errors.New("previously completed file is missing; manual review required")
			}
			f.Bytes = part
		}
		completed += f.Bytes
	}
	j.Bytes = completed
	return nil
}

type downloadPlanRequest struct {
	Source   string   `json:"source"`
	Repo     string   `json:"repo"`
	Revision string   `json:"revision"`
	Files    []string `json:"files"`
	URL      string   `json:"url"`
	SHA256   string   `json:"sha256"`
}

func (m *downloadManager) plan(ctx context.Context, p downloadPlanRequest) (DownloadJob, error) {
	j := DownloadJob{ID: id(), Source: p.Source, Repo: p.Repo, Revision: p.Revision, Status: "PLANNED", Created: time.Now().UTC().Format(time.RFC3339), Files: []DownloadFile{}, Warning: "Download only: no model load, conversion, code execution or quality qualification. Public sources only. Partial files are preserved; one active job."}
	if p.URL != "" {
		if p.Source != "" && p.Source != "url" || p.Repo != "" || len(p.Files) != 0 || p.Revision != "" {
			return j, errors.New("direct URL and repository selection are mutually exclusive")
		}
		f, e := m.directFile(ctx, p.URL, p.SHA256)
		if e != nil {
			return j, e
		}
		j.Source = "url"
		j.Files = append(j.Files, f)
	} else {
		if len(p.Files) == 0 || len(p.Files) > 128 || p.SHA256 != "" {
			return j, errors.New("select 1–128 repository files; hashes come from source metadata")
		}
		listing, e := m.files(ctx, p.Source, p.Repo, p.Revision)
		if e != nil {
			return j, e
		}
		j.Revision = listing.Revision
		j.Warning += " " + listing.Warning
		selected := map[string]bool{}
		for _, name := range p.Files {
			if selected[name] {
				return j, errors.New("duplicate selected file")
			}
			selected[name] = true
		}
		for _, f := range listing.Files {
			if selected[f.Path] {
				j.Files = append(j.Files, f)
				delete(selected, f.Path)
			}
		}
		if len(selected) != 0 {
			return j, errors.New("selected file missing from pinned listing; refresh and select again")
		}
	}
	for _, f := range j.Files {
		if f.Size < 0 || f.Size > 8<<40 || j.TotalBytes > (8<<40)-f.Size {
			return j, errors.New("selected files exceed 8 TiB plan limit")
		}
		j.TotalBytes += f.Size
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.initErr != nil {
		return j, errors.New("download state unavailable")
	}
	if m.closing {
		return j, errors.New("gateway is draining")
	}
	if len(m.jobs) >= 200 {
		return j, errors.New("200 preserved download plans reached; archive completed state manually before adding more")
	}
	free, diskErr := m.free()
	if diskErr != nil || free < m.reserve+(512<<10) {
		return j, errors.New("insufficient disk for a durable plan plus the 16 GiB reserve")
	}
	if e := m.root.Mkdir(j.ID, 0700); e != nil {
		return j, e
	}
	j.Destination = filepath.Join(m.dir, j.ID, "data")
	if e := m.root.Mkdir(j.ID+"/data", 0700); e != nil {
		return j, e
	}
	if e := m.persistLocked(&j); e != nil {
		return j, e
	}
	m.jobs[j.ID] = &j
	return cloneDownloadJob(j), nil
}

func cloneDownloadJob(j DownloadJob) DownloadJob {
	j.Files = append([]DownloadFile(nil), j.Files...)
	if j.ETA != nil {
		eta := *j.ETA
		j.ETA = &eta
	}
	return j
}

func (m *downloadManager) snapshot(jobID string) (DownloadJob, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[jobID]
	if !ok {
		return DownloadJob{}, false
	}
	return cloneDownloadJob(*j), true
}

func (m *downloadManager) start(jobID string, resume bool) (DownloadJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j := m.jobs[jobID]
	if j == nil {
		return DownloadJob{}, errors.New("unknown download")
	}
	if m.initErr != nil || m.closing {
		return DownloadJob{}, errors.New("download manager unavailable or draining")
	}
	if m.active != "" {
		return DownloadJob{}, errors.New("another download is active; pause it first")
	}
	if (!resume && j.Status != "PLANNED") || (resume && j.Status != "PAUSED" && j.Status != "FAILED" && j.Status != "CANCELLED") {
		return DownloadJob{}, errors.New("download state does not allow this action")
	}
	if e := m.reconcileLocked(j); e != nil {
		return DownloadJob{}, e
	}
	free, e := m.free()
	if e != nil || free < m.reserve+j.TotalBytes-j.Bytes {
		return DownloadJob{}, errors.New("insufficient disk: selected remaining bytes plus 16 GiB reserve required")
	}
	j.Status = "RUNNING"
	j.Error = ""
	j.Rate = 0
	j.ETA = nil
	if e = m.persistLocked(j); e != nil {
		return DownloadJob{}, e
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.done = make(chan struct{})
	m.active = jobID
	go m.run(ctx, jobID, m.done)
	return cloneDownloadJob(*j), nil
}

func (m *downloadManager) pause(jobID string, cancelled bool) (DownloadJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j := m.jobs[jobID]
	if j == nil {
		return DownloadJob{}, errors.New("unknown download")
	}
	if j.Status == "COMPLETE" {
		return DownloadJob{}, errors.New("completed downloads cannot be cancelled or deleted here")
	}
	if m.active == jobID {
		if cancelled {
			j.Status = "CANCELLING"
		} else {
			j.Status = "PAUSING"
		}
		m.cancel()
	} else {
		if cancelled {
			j.Status = "CANCELLED"
		} else {
			j.Status = "PAUSED"
		}
		j.Rate = 0
		j.ETA = nil
	}
	if e := m.persistLocked(j); e != nil {
		return DownloadJob{}, e
	}
	return cloneDownloadJob(*j), nil
}

func (m *downloadManager) run(ctx context.Context, jobID string, done chan struct{}) {
	var failure error
	defer func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		j := m.jobs[jobID]
		// Stat only, never rehash, after cancellation/error so the displayed byte
		// count includes the most recent chunk that had not reached a checkpoint.
		if e := m.reconcileLocked(j); e != nil && failure == nil {
			failure = e
		}
		if ctx.Err() != nil {
			if j.Status == "CANCELLING" {
				j.Status = "CANCELLED"
			} else {
				j.Status = "PAUSED"
			}
			j.Error = "Partial files preserved. Resume is explicit; no automatic retry."
		} else if failure != nil {
			j.Status = "FAILED"
			j.Error = failure.Error()
		} else {
			j.Status = "COMPLETE"
			j.Error = ""
		}
		j.Rate = 0
		j.ETA = nil
		if e := m.persistLocked(j); e != nil {
			j.Status = "FAILED"
			j.Error = e.Error()
		}
		m.active = ""
		m.cancel = nil
		close(done)
	}()
	m.mu.Lock()
	count := len(m.jobs[jobID].Files)
	m.mu.Unlock()
	for i := 0; i < count; i++ {
		if e := ctx.Err(); e != nil {
			failure = e
			return
		}
		if e := m.transfer(ctx, jobID, i); e != nil {
			failure = e
			return
		}
	}
}

func (m *downloadManager) transfer(ctx context.Context, jobID string, index int) error {
	m.mu.Lock()
	file := m.jobs[jobID].Files[index]
	m.mu.Unlock()
	if file.Status == "COMPLETE" {
		return nil
	}
	base := jobID + "/data/" + file.Path
	part := base + ".part"
	if e := m.noLinks(part); e != nil {
		return e
	}
	if e := m.root.MkdirAll(filepath.Dir(base), 0700); e != nil {
		return e
	}
	if _, e := m.root.Lstat(base); e == nil {
		return errors.New("final file already exists; no overwrite permitted")
	}
	f, e := m.root.OpenFile(part, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if e != nil {
		return errors.New("cannot open download partial")
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() {
		return errors.New("partial is not a regular file")
	}
	offset := st.Size()
	if offset > file.Size {
		return errors.New("oversized partial preserved")
	}
	if stat, ok := st.Sys().(*syscall.Stat_t); ok && stat.Nlink > 1 && offset < file.Size {
		return errors.New("incomplete partial has additional hard links; refusing writes to a shared inode")
	}
	if offset < file.Size {
		if _, e = validateDownloadURL(file.URL, true); e != nil {
			return e
		}
		if offset > 0 && file.ETag == "" && file.LastModified == "" && file.SHA256 == "" {
			return errors.New("resume requires a strong ETag, Last-Modified or pinned SHA256; partial preserved")
		}
		requestCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		req, _ := http.NewRequestWithContext(requestCtx, "GET", file.URL, nil)
		req.Header.Set("Accept-Encoding", "identity")
		req.Header.Set("User-Agent", "HaloClu-model-download/1")
		if offset > 0 {
			req.Header.Set("Range", "bytes="+strconv.FormatInt(offset, 10)+"-")
			if file.ETag != "" {
				req.Header.Set("If-Range", file.ETag)
			} else if file.LastModified != "" {
				req.Header.Set("If-Range", file.LastModified)
			}
		}
		res, e := m.client.Do(req)
		if e != nil {
			return errors.New("download connection failed; partial preserved")
		}
		defer res.Body.Close()
		if res.Header.Get("Content-Encoding") != "" && res.Header.Get("Content-Encoding") != "identity" {
			return errors.New("encoded download refused; byte offsets must refer to original file")
		}
		if offset == 0 {
			if res.StatusCode != 200 {
				return fmt.Errorf("download HTTP %d; no automatic retry", res.StatusCode)
			}
		} else {
			if res.StatusCode != 206 {
				return fmt.Errorf("resume requires HTTP 206, received %d; partial preserved without overwrite", res.StatusCode)
			}
			n, e := parseDownloadRange(res.Header.Get("Content-Range"), offset, file.Size)
			if e != nil {
				return e
			}
			if n != file.Size-offset {
				return errors.New("partial range did not include the full remaining suffix")
			}
		}
		if res.ContentLength >= 0 && res.ContentLength != file.Size-offset {
			return errors.New("download Content-Length changed; partial preserved")
		}
		etag := strongDownloadETag(res.Header.Get("ETag"))
		modified := res.Header.Get("Last-Modified")
		if _, e := http.ParseTime(modified); e != nil {
			modified = ""
		}
		if file.ETag != "" && etag != file.ETag {
			return errors.New("source ETag changed; partial preserved, create a fresh plan")
		}
		if file.ETag == "" && file.LastModified != "" && modified != file.LastModified {
			return errors.New("source Last-Modified changed; partial preserved")
		}
		m.mu.Lock()
		j := m.jobs[jobID]
		j.Files[index].ETag = etag
		j.Files[index].LastModified = modified
		j.Files[index].Status = "RUNNING"
		e = m.persistLocked(j)
		m.mu.Unlock()
		if e != nil {
			return e
		}
		if _, e = f.Seek(offset, io.SeekStart); e != nil {
			return e
		}
		started := time.Now()
		last := started
		startOffset := offset
		buf := make([]byte, 256<<10)
		idle := time.AfterFunc(90*time.Second, cancel)
		defer idle.Stop()
		for {
			if e := ctx.Err(); e != nil {
				return e
			}
			n, readErr := res.Body.Read(buf)
			if n > 0 {
				idle.Reset(90 * time.Second)
				if offset+int64(n) > file.Size {
					return errors.New("response exceeded pinned size; excess bytes were not written")
				}
				written, e := f.Write(buf[:n])
				offset += int64(written)
				if e != nil {
					return errors.New("download disk write failed; partial preserved")
				}
				if time.Since(last) >= time.Second || offset == file.Size {
					if e = f.Sync(); e != nil {
						return errors.New("download fsync failed")
					}
					free, e := m.free()
					if e != nil || free < m.reserve {
						return errors.New("disk reserve reached; partial preserved")
					}
					m.mu.Lock()
					j = m.jobs[jobID]
					j.Files[index].Bytes = offset
					j.Bytes = 0
					for _, row := range j.Files {
						j.Bytes += row.Bytes
					}
					j.Rate = float64(offset-startOffset) / time.Since(started).Seconds()
					if j.Rate > 0 {
						eta := float64(j.TotalBytes-j.Bytes) / j.Rate
						j.ETA = &eta
					}
					e = m.persistLocked(j)
					m.mu.Unlock()
					if e != nil {
						return e
					}
					last = time.Now()
				}
			}
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				return errors.New("download interrupted; partial preserved for explicit resume")
			}
		}
		if offset != file.Size {
			return errors.New("download ended before pinned size; partial preserved")
		}
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return errors.New("cannot sync completed partial")
	}
	m.mu.Lock()
	j := m.jobs[jobID]
	j.Status = "VERIFYING"
	j.Files[index].Status = "VERIFYING"
	j.Files[index].Bytes = file.Size
	e = m.persistLocked(j)
	m.mu.Unlock()
	if e != nil {
		return e
	}
	verification := "SIZE_VERIFIED_NO_SOURCE_SHA256"
	if file.SHA256 != "" {
		if _, e = f.Seek(0, io.SeekStart); e != nil {
			return e
		}
		h := sha256.New()
		buf := make([]byte, 1<<20)
		for {
			if e := ctx.Err(); e != nil {
				return e
			}
			n, err := f.Read(buf)
			if n > 0 {
				h.Write(buf[:n])
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				return errors.New("SHA256 read failed; partial preserved")
			}
		}
		if hex.EncodeToString(h.Sum(nil)) != file.SHA256 {
			return errors.New("SHA256 mismatch; partial preserved for manual review")
		}
		verification = "SIZE_AND_SHA256_VERIFIED"
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	if e = m.noLinks(base); e != nil {
		return e
	}
	if e = m.root.Link(part, base); e != nil {
		return errors.New("atomic no-overwrite publication failed; partial preserved")
	}
	directory, err := m.root.Open(filepath.Dir(base))
	if err != nil {
		return errors.New("publication directory unavailable; files preserved")
	}
	err = directory.Sync()
	directory.Close()
	if err != nil {
		return errors.New("publication directory fsync failed; files preserved")
	}
	// Keep the .part hard link as a recovery alias, as the original tools do.
	// It occupies no second data copy. Never append to a completed file.
	m.mu.Lock()
	defer m.mu.Unlock()
	j = m.jobs[jobID]
	j.Files[index].Status = "COMPLETE"
	j.Files[index].Verification = verification
	j.Files[index].Bytes = file.Size
	j.Status = "RUNNING"
	j.Bytes = 0
	for _, f := range j.Files {
		j.Bytes += f.Bytes
	}
	return m.persistLocked(j)
}

func (a *App) shutdownDownloads(ctx context.Context) error {
	v, ok := downloadManagers.Load(a)
	if !ok {
		return nil
	}
	m := v.(*downloadManager)
	m.mu.Lock()
	m.closing = true
	done := m.done
	if m.active != "" && m.cancel != nil {
		m.cancel()
	}
	m.mu.Unlock()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *App) registerDownloadRoutes(mux *http.ServeMux) {
	handle := func(fn http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !a.authorized(w, r) {
				return
			}
			if downloadsFor(a).initErr != nil {
				jsonReply(w, 503, map[string]string{"error": "download state unavailable; no acquisitions started"})
				return
			}
			fn(w, r)
		}
	}
	mux.HandleFunc("GET /v1/downloads/options", handle(func(w http.ResponseWriter, r *http.Request) {
		jsonReply(w, 200, map[string]any{"sources": []string{"huggingface", "modelscope"}, "direct_https": true, "public_only": true, "max_selected_files": 128, "concurrency": 1, "disk_reserve_bytes": downloadReserveBytes, "destination_root": downloadsFor(a).dir, "note": "vLLM is a runtime and VLM a model category, not a hosting source. Downloads never load or qualify a model."})
	}))
	mux.HandleFunc("GET /v1/downloads/search", handle(func(w http.ResponseWriter, r *http.Request) {
		source := r.URL.Query().Get("source")
		if source == "" {
			source = "all"
		}
		sources := []string{source}
		if source == "all" {
			sources = []string{"huggingface", "modelscope"}
		}
		items := []downloadSearchItem{}
		warnings := map[string]string{}
		for _, s := range sources {
			rows, e := downloadsFor(a).search(r.Context(), s, r.URL.Query().Get("q"))
			if e != nil {
				warnings[s] = e.Error()
			} else {
				items = append(items, rows...)
			}
		}
		jsonReply(w, 200, map[string]any{"items": items, "warnings": warnings, "source": source})
	}))
	mux.HandleFunc("GET /v1/downloads/files", handle(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		listing, e := downloadsFor(a).files(r.Context(), q.Get("source"), q.Get("repo"), q.Get("revision"))
		if e != nil {
			jsonReply(w, 400, map[string]string{"error": e.Error()})
			return
		}
		jsonReply(w, 200, listing)
	}))
	mux.HandleFunc("POST /v1/downloads/plan", handle(func(w http.ResponseWriter, r *http.Request) {
		if !a.currentAPISettings().Operations {
			jsonReply(w, 403, map[string]string{"error": "download admission disabled in Options", "code": "api_disabled"})
			return
		}
		var p downloadPlanRequest
		if strictDownloadPlanBody(w, r, &p) != nil {
			jsonReply(w, 400, map[string]string{"error": "invalid explicit download plan"})
			return
		}
		j, e := downloadsFor(a).plan(r.Context(), p)
		if e != nil {
			jsonReply(w, 400, map[string]string{"error": e.Error()})
			return
		}
		jsonReply(w, 201, j)
	}))
	mux.HandleFunc("GET /v1/downloads/jobs", handle(func(w http.ResponseWriter, r *http.Request) {
		m := downloadsFor(a)
		m.mu.Lock()
		jobs := []DownloadJob{}
		for _, j := range m.jobs {
			jobs = append(jobs, cloneDownloadJob(*j))
		}
		m.mu.Unlock()
		sort.Slice(jobs, func(i, j int) bool { return jobs[i].Created > jobs[j].Created })
		jsonReply(w, 200, map[string]any{"jobs": jobs})
	}))
	mux.HandleFunc("GET /v1/downloads/jobs/{id}", handle(func(w http.ResponseWriter, r *http.Request) {
		j, ok := downloadsFor(a).snapshot(r.PathValue("id"))
		if !ok {
			jsonReply(w, 404, map[string]string{"error": "unknown download"})
			return
		}
		jsonReply(w, 200, j)
	}))
	mux.HandleFunc("POST /v1/downloads/jobs/{id}/{action}", handle(func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			Confirm bool `json:"confirm"`
		}
		if strictOperationBody(w, r, &p) != nil || !p.Confirm {
			jsonReply(w, 400, map[string]string{"error": "explicit confirm:true required"})
			return
		}
		action := r.PathValue("action")
		m := downloadsFor(a)
		var j DownloadJob
		var e error
		switch action {
		case "start", "resume":
			if !a.currentAPISettings().Operations {
				jsonReply(w, 403, map[string]string{"error": "download admission disabled in Options", "code": "api_disabled"})
				return
			}
			j, e = m.start(r.PathValue("id"), action == "resume")
		case "pause", "cancel":
			j, e = m.pause(r.PathValue("id"), action == "cancel")
		default:
			e = errors.New("unknown download action")
		}
		if e != nil {
			jsonReply(w, 409, map[string]string{"error": e.Error()})
			return
		}
		jsonReply(w, 202, j)
	}))
}

func strictDownloadPlanBody(w http.ResponseWriter, r *http.Request, p *downloadPlanRequest) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return errors.New("JSON required")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(p); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}
