package app

// Explicit allowlisted operations. No client-supplied command, path, URL,
// model switch or rank lifecycle is accepted here.
import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const operationSuite = "/home/funboy/StrixHaloClusterGLM/runtime/fixtures/suite/manifest.json"
const operationSuiteSHA = "94e6743320b56763ba5e8ee1db2432dbd4e17c676175a7931ea2ab34e90a970f"
const operationHistorical = "/home/funboy/StrixHaloClusterGLM/runtime/fixtures/historical-109-256.json"
const operationHistoricalSHA = "735954ea7cb1f145d36fcd451486665db22ac1463dce1126b0f6520a53ed24d7"

var operationSpecSHA = map[string]string{
	"01-escaped-settings":     "83da10f50024f07ed5d791cfa2df08a98969a72e0c329ff094649be67dbbfd34",
	"02-ttl-cache":            "25571745a2bb1f26f6073c7f03d53aa9395c8af1ff3ed43d2906e6591f941d4e",
	"03-schedule-difference":  "511f1610b90f98d4f03f0b4d18288d24c9dcbfd96beeed527c27be51dd8494de",
	"04-inventory-import":     "050abc31928b5d06ae8b7e0a3e53d15af42ba65820ff0d7c4e6132a3cff826c3",
	"05-price-cache-refactor": "d5c98b488f62ca1b79b8aa42e6f4647d4c621e5f65520fdaf649045d78ccfa0c",
	"06-job-acknowledgement":  "c9d9db6ac5098e6107833c30d733f3a8f61b003eec686cc0dc4a3f983d5bf9cb",
	"context-1k":              "98b898d1e3916b839768c5ef83a02a30fffe4e6735e837cedc3aed42879762d3",
	"context-2k":              "04ab0c545e2ec0b98a7dd842b1a0a4a3ef0cdbb5407a1fa5e159601f430f87ae",
	"context-4k":              "2212a2d0f650b2db5e70e0552b855f0cec11d02dd9fc17a9f01eb03b133ec1f5",
	"context-8k":              "7313a0f55cf21330e1c07b61e3fad03e0913ead1e1dd2a7015922e192b2bd042",
	"context-16k":             "1fba029f87e8a1ac81f9e7d66552625af507b395db558a8c36df0ba20798d280",
	"context-32k":             "e6d4589a4ab6fd24a42d198cae99c6a8edc3f3710d677138a5c4378a8bc564c4",
}

type OperationOption struct {
	ID                   string `json:"id"`
	Label                string `json:"label"`
	Description          string `json:"description"`
	RequiresConfirmation bool   `json:"requires_confirmation"`
	Available            bool   `json:"available"`
	BlockedReason        string `json:"blocked_reason,omitempty"`
	Model                string `json:"model"`
	DownloadBytes        int64  `json:"download_bytes,omitempty"`
	Requests             int    `json:"requests"`
}
type OperationProgress struct {
	Phase      string `json:"phase"`
	Current    int    `json:"current"`
	Total      int    `json:"total"`
	Bytes      int64  `json:"bytes"`
	TotalBytes int64  `json:"total_bytes"`
}
type OperationJob struct {
	ID           string            `json:"id"`
	ActionID     string            `json:"action_id"`
	Status       string            `json:"status"`
	Created      string            `json:"created"`
	Started      string            `json:"started,omitempty"`
	Finished     string            `json:"finished,omitempty"`
	Progress     OperationProgress `json:"progress"`
	Error        string            `json:"error,omitempty"`
	Result       map[string]any    `json:"result,omitempty"`
	LogTail      []string          `json:"log_tail"`
	RawDirectory string            `json:"raw_directory"`
	TaskIDs      []string          `json:"task_ids"`
	mu           sync.Mutex
	cancel       context.CancelFunc
	done         chan struct{}
}
type operationManager struct {
	app     *App
	mu      sync.Mutex
	jobs    map[string]*OperationJob
	active  string
	closing bool
	options func() []OperationOption
	execute func(context.Context, *OperationJob) error
	initErr error
}

var operationManagers sync.Map

func operationTerminal(s string) bool {
	return s == "PASS" || s == "FAIL" || s == "CANCELLED" || s == "INTERRUPTED" || s == "BLOCKED" || s == "INCOMPLETE"
}

// Called by the gateway's graceful shutdown. It stops only its own operation;
// an already submitted paired generation still drains through the normal core.
func (a *App) shutdownOperations(ctx context.Context) error {
	value, ok := operationManagers.Load(a)
	if !ok {
		return nil
	}
	m := value.(*operationManager)
	m.mu.Lock()
	m.closing = true
	job := m.jobs[m.active]
	m.mu.Unlock()
	if job == nil {
		return nil
	}
	job.mu.Lock()
	if job.cancel != nil {
		job.cancel()
	}
	job.mu.Unlock()
	select {
	case <-job.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (j *OperationJob) snapshot() map[string]any {
	j.mu.Lock()
	defer j.mu.Unlock()
	b, _ := json.Marshal(j)
	var result map[string]any
	_ = json.Unmarshal(b, &result)
	return result
}
func (j *OperationJob) update(fn func()) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	fn()
	return writeJSON(filepath.Join(j.RawDirectory, "job.json"), j)
}
func (j *OperationJob) log(message string) error {
	return j.update(func() {
		line := time.Now().UTC().Format(time.RFC3339) + " " + message
		j.LogTail = append(j.LogTail, line)
		if len(j.LogTail) > 80 {
			j.LogTail = j.LogTail[len(j.LogTail)-80:]
		}
		f, e := os.OpenFile(filepath.Join(j.RawDirectory, "operation.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY|syscall.O_NOFOLLOW, 0600)
		if e == nil {
			_, _ = fmt.Fprintln(f, line)
			_ = f.Close()
		}
	})
}
func operationsFor(a *App) *operationManager {
	if existing, ok := operationManagers.Load(a); ok {
		return existing.(*operationManager)
	}
	m := &operationManager{app: a, jobs: map[string]*OperationJob{}}
	m.options = m.defaultOptions
	m.execute = m.run
	m.mu.Lock()
	actual, loaded := operationManagers.LoadOrStore(a, m)
	if loaded {
		m.mu.Unlock()
		return actual.(*operationManager)
	}
	defer m.mu.Unlock()
	dir := filepath.Join(a.cfg.StateDir, "operations")
	if e := os.MkdirAll(dir, 0700); e != nil {
		m.initErr = e
		return m
	}
	entries, e := os.ReadDir(dir)
	if e != nil {
		m.initErr = e
		return m
	}
	for _, entry := range entries {
		if !entry.IsDir() || !controllerHex.MatchString(entry.Name()) {
			continue
		}
		b, e := readSmall(filepath.Join(dir, entry.Name(), "job.json"), 4<<20)
		if e != nil {
			continue
		}
		var job OperationJob
		if json.Unmarshal(b, &job) != nil || job.ID != entry.Name() {
			continue
		}
		job.RawDirectory = filepath.Join(dir, job.ID)
		job.done = make(chan struct{})
		close(job.done)
		if !operationTerminal(job.Status) {
			job.Status = "INTERRUPTED"
			job.Error = "Gateway restarted; no operation or model submission is automatically replayed. Check task IDs and paired health before starting another operation."
			job.Finished = time.Now().UTC().Format(time.RFC3339Nano)
			_ = job.update(func() {})
		}
		m.jobs[job.ID] = &job
	}
	return m
}
func readOperationPinned(path, expected string) ([]byte, error) {
	b, e := readSmall(path, 4<<20)
	if e != nil {
		return nil, e
	}
	sum := sha256.Sum256(b)
	if hex.EncodeToString(sum[:]) != expected {
		return nil, errors.New("pinned operation input changed: " + path)
	}
	return b, nil
}
func (m *operationManager) defaultOptions() []OperationOption {
	options := []OperationOption{
		{ID: "api-smoke", Label: "API smoke", Description: "One arithmetic chat request; natural stop, answer42, real usage. No performance claim.", Requests: 1},
		{ID: "speed-chat", Label: "Natural coding throughput", Description: "One excluded warmup and three measured identical coding chats; natural stop required. Distinct from historical109/256.", Requests: 4},
		{ID: "speed-historical", Label: "Historical109/256", Description: "Frozen native completion payload, one excluded warmup and three measurements. ignore_eos=true is intentional in this speed-only historical category, not quality.", Requests: 4},
		{ID: "coding-sanity", Label: "Coding sanity: six frozen tasks", Description: "Six existing C++ tasks and independent tests; low reasoning, at most two repairs each, no apply. Maximum18 model calls.", Requests: 18},
		{ID: "context-small", Label: "Context sanity: three small fixtures", Description: "Frozen context-1k/2k/4k fixtures; report actual runtime token counts. Maximum9 calls, low reasoning. Labels are approximate.", Requests: 9},
		{ID: "context-long", Label: "Explicit long-context diagnostic", Description: "Frozen context-8k/16k/32k, previously failed and NOT qualified. Explicit diagnostic only, no promotion. Maximum9 calls;600s per call, output4096.", Requests: 9},
	}
	for i := range options {
		o := &options[i]
		o.RequiresConfirmation, o.Available, o.Model = true, true, m.app.cfg.Model
		if o.ID == "speed-historical" {
			if _, e := readOperationPinned(operationHistorical, operationHistoricalSHA); e != nil {
				o.Available, o.BlockedReason = false, e.Error()
			}
		}
		if strings.Contains(o.ID, "sanity") || strings.HasPrefix(o.ID, "context-") {
			if _, e := readOperationPinned(operationSuite, operationSuiteSHA); e != nil {
				o.Available, o.BlockedReason = false, e.Error()
			}
		}
	}
	if catalog, e := loadModelCatalog(); e == nil {
		for _, model := range catalog.Models {
			for _, asset := range model.Assets {
				if asset.URL == "" {
					continue
				}
				_, e := catalogDownloadAsset(asset.ID)
				o := OperationOption{ID: "download:" + asset.ID, Label: "Resume " + asset.ID, Description: "Pinned asset only; validated ranges and final SHA-256. Never loads or switches a model.", RequiresConfirmation: true, Model: model.ID, DownloadBytes: asset.ExpectedBytes, Available: e == nil}
				if e != nil {
					o.BlockedReason = e.Error()
				}
				options = append(options, o)
			}
		}
	}
	return options
}
func (a *App) registerOperationRoutes(mux *http.ServeMux) {
	handle := func(fn http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !a.authorized(w, r) {
				return
			}
			fn(w, r)
		}
	}
	mux.HandleFunc("GET /v1/operations/options", handle(func(w http.ResponseWriter, r *http.Request) {
		jsonReply(w, 200, map[string]any{"actions": operationsFor(a).options()})
	}))
	mux.HandleFunc("GET /v1/operations/jobs", handle(func(w http.ResponseWriter, r *http.Request) {
		m := operationsFor(a)
		m.mu.Lock()
		var jobs []map[string]any
		for _, job := range m.jobs {
			jobs = append(jobs, job.snapshot())
		}
		m.mu.Unlock()
		sort.Slice(jobs, func(i, j int) bool { return stringValue(jobs[i]["created"]) > stringValue(jobs[j]["created"]) })
		if len(jobs) > 100 {
			jobs = jobs[:100]
		}
		if jobs == nil {
			jobs = []map[string]any{}
		}
		jsonReply(w, 200, map[string]any{"jobs": jobs})
	}))
	mux.HandleFunc("POST /v1/operations/jobs", handle(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ActionID string `json:"action_id"`
			Confirm  bool   `json:"confirm"`
		}
		if e := strictOperationBody(w, r, &request); e != nil || !request.Confirm {
			jsonReply(w, 400, map[string]string{"error": "explicit confirm:true and allowlisted action_id required; no extra fields"})
			return
		}
		job, code, e := operationsFor(a).start(request.ActionID)
		if e != nil {
			jsonReply(w, code, map[string]string{"error": e.Error()})
			return
		}
		jsonReply(w, 202, job.snapshot())
	}))
	mux.HandleFunc("GET /v1/operations/jobs/{id}", handle(func(w http.ResponseWriter, r *http.Request) {
		job := operationsFor(a).find(r.PathValue("id"))
		if job == nil {
			jsonReply(w, 404, map[string]string{"error": "unknown operation"})
			return
		}
		jsonReply(w, 200, job.snapshot())
	}))
	mux.HandleFunc("POST /v1/operations/jobs/{id}/cancel", handle(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Confirm bool `json:"confirm"`
		}
		if e := strictOperationBody(w, r, &request); e != nil || !request.Confirm {
			jsonReply(w, 400, map[string]string{"error": "explicit confirm:true required"})
			return
		}
		job := operationsFor(a).find(r.PathValue("id"))
		if job == nil {
			jsonReply(w, 404, map[string]string{"error": "unknown operation"})
			return
		}
		job.mu.Lock()
		if !operationTerminal(job.Status) && job.cancel != nil {
			job.cancel()
			job.Status = "cancelling"
		}
		job.mu.Unlock()
		_ = job.update(func() {})
		jsonReply(w, 202, job.snapshot())
	}))
}
func strictOperationBody(w http.ResponseWriter, r *http.Request, target any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return errors.New("JSON required")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(target); e != nil {
		return e
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}
func (m *operationManager) find(id string) *OperationJob {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.jobs[id]
}
func (m *operationManager) start(action string) (*OperationJob, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.initErr != nil {
		return nil, 503, m.initErr
	}
	if m.closing {
		return nil, 503, errors.New("operation admission is shutting down")
	}
	found := false
	for _, o := range m.options() {
		if o.ID == action {
			found = true
			if !o.Available {
				return nil, 409, errors.New(o.BlockedReason)
			}
		}
	}
	if !found {
		return nil, 400, errors.New("action is not allowlisted")
	}
	if m.active != "" {
		return nil, 409, errors.New("another operation is active")
	}
	lock, e := os.OpenFile(m.app.cfg.PairLock+".operations", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if e != nil {
		return nil, 503, e
	}
	if e = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		lock.Close()
		return nil, 409, errors.New("another gateway operation is active")
	}
	ctx, cancel := context.WithCancel(context.Background())
	jobID := id()
	job := &OperationJob{ID: jobID, ActionID: action, Status: "queued", Created: time.Now().UTC().Format(time.RFC3339Nano), RawDirectory: filepath.Join(m.app.cfg.StateDir, "operations", jobID), LogTail: []string{}, TaskIDs: []string{}, cancel: cancel, done: make(chan struct{})}
	if e = os.Mkdir(job.RawDirectory, 0700); e == nil {
		e = writeJSON(filepath.Join(job.RawDirectory, "request.json"), map[string]any{"action_id": action, "confirm": true, "created": job.Created, "automatic_retry": false})
	}
	if e == nil {
		e = job.update(func() {})
	}
	if e != nil {
		cancel()
		lock.Close()
		return nil, 500, e
	}
	m.active = jobID
	m.jobs[jobID] = job
	go func() {
		defer close(job.done)
		defer cancel()
		defer lock.Close()
		defer func() { m.mu.Lock(); m.active = ""; m.mu.Unlock() }()
		e := job.update(func() { job.Status, job.Started = "running", time.Now().UTC().Format(time.RFC3339Nano) })
		if e == nil {
			e = m.execute(ctx, job)
		}
		_ = job.update(func() {
			if e != nil {
				job.Error = e.Error()
			}
			switch {
			case ctx.Err() != nil:
				job.Status = "CANCELLED"
			case operationTerminal(job.Status):
			case e != nil:
				job.Status = "FAIL"
			default:
				job.Status = "PASS"
			}
			job.Finished = time.Now().UTC().Format(time.RFC3339Nano)
		})
	}()
	return job, 202, nil
}
func (m *operationManager) run(ctx context.Context, job *OperationJob) error {
	if strings.HasPrefix(job.ActionID, "download:") {
		asset, e := catalogDownloadAsset(strings.TrimPrefix(job.ActionID, "download:"))
		if e != nil {
			return e
		}
		return downloadOperationAsset(ctx, job, asset)
	}
	switch job.ActionID {
	case "api-smoke", "speed-chat":
		return m.chatOperation(ctx, job)
	case "speed-historical":
		return m.historicalOperation(ctx, job)
	case "coding-sanity", "context-small", "context-long":
		return m.codingOperation(ctx, job)
	}
	return errors.New("unsupported operation")
}
func operationMedian(values []float64) any {
	if len(values) == 0 {
		return nil
	}
	sort.Float64s(values)
	n := len(values)
	if n%2 == 1 {
		return values[n/2]
	}
	return (values[n/2-1] + values[n/2]) / 2
}
func operationMetrics(records []Metrics) map[string]any {
	var decode, httpTPS, wall, clientTTFT, serverTTFT, acceptance []float64
	for _, r := range records {
		if r.DecodeTPS != nil {
			decode = append(decode, *r.DecodeTPS)
		}
		if r.HTTPSeconds > 0 {
			httpTPS = append(httpTPS, float64(r.CompletionTokens)/r.HTTPSeconds)
			wall = append(wall, r.HTTPSeconds)
		}
		if r.TTFTMS != nil {
			clientTTFT = append(clientTTFT, *r.TTFTMS)
		}
		if r.ServerTTFTMS != nil {
			serverTTFT = append(serverTTFT, *r.ServerTTFTMS)
		}
		if r.Acceptance != nil {
			acceptance = append(acceptance, *r.Acceptance)
		}
	}
	var dispersion any
	if len(decode) > 1 {
		sum := 0.0
		for _, v := range decode {
			sum += v
		}
		mean, squared := sum/float64(len(decode)), 0.0
		for _, v := range decode {
			squared += (v - mean) * (v - mean)
		}
		dispersion = math.Sqrt(squared / float64(len(decode)))
	}
	return map[string]any{"measured_runs": len(records), "decode_tps_median": operationMedian(decode), "decode_tps_stddev_population": dispersion, "http_tps_median": operationMedian(httpTPS), "http_seconds_median": operationMedian(wall), "client_ttft_ms_median": operationMedian(clientTTFT), "server_ttft_ms_median": operationMedian(serverTTFT), "acceptance_median": operationMedian(acceptance), "records": records}
}

func (m *operationManager) chatOperation(ctx context.Context, job *OperationJob) error {
	total := 4
	message := "Write one complete Python function that merges overlapping closed intervals. Explain its time complexity briefly. Include no external libraries."
	cap := 768
	if job.ActionID == "api-smoke" {
		total, cap = 1, 64
		message = "What is 6 times 7? Reply with the number only."
	}
	metrics := []Metrics{}
	for i := 0; i < total; i++ {
		if e := ctx.Err(); e != nil {
			return e
		}
		if e := job.update(func() { job.Progress = OperationProgress{Phase: "generation", Current: i, Total: total} }); e != nil {
			return e
		}
		if e := job.log(fmt.Sprintf("request%d/%d; low reasoning; cap%d; no model/runtime changes", i+1, total, cap)); e != nil {
			return e
		}
		payload := map[string]any{"model": m.app.cfg.Model, "messages": []map[string]string{{"role": "user", "content": message}}, "temperature": 0, "seed": 1, "n": 1, "max_tokens": cap, "chat_template_kwargs": map[string]any{"reasoning_effort": "low"}}
		result, e := m.app.generate(ctx, payload, filepath.Join(job.RawDirectory, fmt.Sprintf("request-%02d", i)))
		if e != nil {
			return e
		}
		if result.FinishReason != "stop" || !result.StreamComplete {
			_ = job.update(func() { job.Status = "INCOMPLETE" })
			return errors.New("response did not conclude naturally; output cap is not a quality PASS")
		}
		if job.ActionID == "api-smoke" && strings.TrimSpace(result.Content) != "42" {
			return errors.New("API arithmetic answer differs from42")
		}
		if result.Metrics.PromptTokens < 1 || result.Metrics.CompletionTokens < 1 || result.Metrics.ServerTTFTMS == nil || result.Metrics.Raw == nil {
			return errors.New("smoke/benchmark is missing actual runtime usage or timing metrics")
		}
		if total == 4 && result.Metrics.DecodeTPS == nil {
			return errors.New("insufficient measured output for decode throughput")
		}
		if total == 1 || i > 0 {
			metrics = append(metrics, result.Metrics)
		}
		if e := job.update(func() {
			job.Progress.Current = i + 1
			job.Result = operationMetrics(metrics)
			job.Result["warmup_excluded"] = total == 4
			job.Result["category"] = "natural-chat"
			job.Result["code_tested"] = false
		}); e != nil {
			return e
		}
	}
	return ctx.Err()
}
func (m *operationManager) historicalOperation(ctx context.Context, job *OperationJob) error {
	b, e := readOperationPinned(operationHistorical, operationHistoricalSHA)
	if e != nil {
		return e
	}
	var protocol struct {
		Request  json.RawMessage `json:"request"`
		Route    string          `json:"route"`
		Expected int             `json:"expected_prompt_tokens"`
	}
	if e = json.Unmarshal(b, &protocol); e != nil || protocol.Route != "/v1/completions" || protocol.Expected != 109 {
		return errors.New("historical protocol mismatch")
	}
	var payload map[string]any
	if e = json.Unmarshal(protocol.Request, &payload); e != nil || payload["model"] != m.app.cfg.Model {
		return errors.New("historical model mismatch")
	}
	var body bytes.Buffer
	if e = json.Compact(&body, protocol.Request); e != nil {
		return e
	}
	if e = writeJSON(filepath.Join(job.RawDirectory, "protocol.json"), map[string]any{"source": operationHistorical, "sha256": operationHistoricalSHA, "request": payload, "category": "fixed-historical109/256", "warmup": 1, "measured": 3, "reasoning": "absent in exact historical payload", "fidelity": "repeat only; no new target-reference qualification"}); e != nil {
		return e
	}
	metrics := []Metrics{}
	var reference []int
	for i := 0; i < 4; i++ {
		if e = ctx.Err(); e != nil {
			return e
		}
		if e = job.update(func() { job.Progress = OperationProgress{Phase: "historical-completion", Current: i, Total: 4} }); e != nil {
			return e
		}
		if e = job.log(fmt.Sprintf("historical request%d/4; warmup=%t; exact pinned payload", i+1, i == 0)); e != nil {
			return e
		}
		release, e := m.app.modelLock(ctx)
		if e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			release()
			return e
		}
		result, raw, e := func() (map[string]any, []byte, error) {
			defer release()
			drain, cancel := context.WithTimeout(context.Background(), time.Duration(m.app.cfg.ModelTimeout)*time.Second)
			defer cancel()
			request, e := http.NewRequestWithContext(drain, "POST", m.app.cfg.Backend+protocol.Route, bytes.NewReader(body.Bytes()))
			if e != nil {
				return nil, nil, e
			}
			request.Header.Set("Content-Type", "application/json")
			if e = writeJSON(filepath.Join(job.RawDirectory, fmt.Sprintf("request-%02d-intent.json", i)), map[string]any{"request": payload, "phase": "before-dispatch"}); e != nil {
				return nil, nil, e
			}
			started := time.Now()
			response, e := m.app.client.Do(request)
			if e != nil {
				return nil, nil, e
			}
			defer response.Body.Close()
			raw, e := io.ReadAll(io.LimitReader(response.Body, (16<<20)+1))
			if e != nil {
				return nil, raw, e
			}
			if len(raw) > 16<<20 {
				return nil, nil, errors.New("historical response exceeded limit")
			}
			var result map[string]any
			if e = json.Unmarshal(raw, &result); e != nil {
				return nil, raw, e
			}
			if response.StatusCode != 200 {
				return result, raw, fmt.Errorf("historical backend HTTP%d", response.StatusCode)
			}
			result["_http_seconds"] = time.Since(started).Seconds()
			return result, raw, nil
		}()
		if len(raw) > 0 {
			_ = controllerBytes(filepath.Join(job.RawDirectory, fmt.Sprintf("response-%02d.json", i)), raw)
		}
		if e != nil {
			return e
		}
		var r struct {
			Choices []struct {
				TokenIDs     []int  `json:"token_ids"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
		}
		if e = json.Unmarshal(raw, &r); e != nil {
			return e
		}
		measured := Metrics{HTTPSeconds: number(result["_http_seconds"])}
		parseMetrics(&measured, result)
		if measured.PromptTokens != 109 || measured.CompletionTokens != 256 || len(r.Choices) != 1 || r.Choices[0].FinishReason != "length" || len(r.Choices[0].TokenIDs) != 256 {
			return errors.New("historical109/256 protocol/usage/tokenID check failed")
		}
		if i > 0 {
			if reference == nil {
				reference = append([]int(nil), r.Choices[0].TokenIDs...)
			} else if !reflect.DeepEqual(reference, r.Choices[0].TokenIDs) {
				return errors.New("historical repeat tokenIDs diverged")
			}
			metrics = append(metrics, measured)
		}
		if e = job.update(func() {
			job.Progress.Current = i + 1
			job.Result = operationMetrics(metrics)
			job.Result["warmup_excluded"] = true
			job.Result["repeat_token_ids"] = true
			job.Result["category"] = "fixed-historical109/256"
			job.Result["quality_pass"] = false
		}); e != nil {
			return e
		}
	}
	return ctx.Err()
}
func (m *operationManager) codingOperation(ctx context.Context, job *OperationJob) error {
	b, e := readOperationPinned(operationSuite, operationSuiteSHA)
	if e != nil {
		return e
	}
	var manifest struct {
		Tasks []struct {
			ID     string `json:"id"`
			Spec   string `json:"spec"`
			Hashes map[string]struct {
				Buggy string `json:"buggy"`
			} `json:"hashes"`
			TestSHA string `json:"test_sha256"`
		} `json:"tasks"`
	}
	if e = json.Unmarshal(b, &manifest); e != nil {
		return e
	}
	ids := []string{"01-escaped-settings", "02-ttl-cache", "03-schedule-difference", "04-inventory-import", "05-price-cache-refactor", "06-job-acknowledgement"}
	if job.ActionID == "context-small" {
		ids = []string{"context-1k", "context-2k", "context-4k"}
	}
	if job.ActionID == "context-long" {
		ids = []string{"context-8k", "context-16k", "context-32k"}
	}
	var specs []TaskSpec
	for _, want := range ids {
		found := false
		for _, fixture := range manifest.Tasks {
			if fixture.ID != want {
				continue
			}
			found = true
			expected := filepath.Join(filepath.Dir(operationSuite), want, "task.json")
			if fixture.Spec != expected {
				return errors.New("frozen task path changed")
			}
			data, e := readOperationPinned(expected, operationSpecSHA[want])
			if e != nil {
				return e
			}
			var spec TaskSpec
			if e = json.Unmarshal(data, &spec); e != nil {
				return e
			}
			if spec.Repo != filepath.Join(filepath.Dir(expected), "repo") {
				return errors.New("frozen repository path changed")
			}
			for name, hash := range fixture.Hashes {
				if cleanRel(name) != nil {
					return errors.New("unsafe frozen source")
				}
				if _, e = readOperationPinned(filepath.Join(spec.Repo, name), hash.Buggy); e != nil {
					return e
				}
			}
			if len(spec.TestFiles) != 1 {
				return errors.New("frozen hidden-test shape changed")
			}
			for dest, path := range spec.TestFiles {
				if cleanRel(dest) != nil || path != filepath.Join(filepath.Dir(expected), "private", dest) {
					return errors.New("frozen test path changed")
				}
				if _, e = readOperationPinned(path, fixture.TestSHA); e != nil {
					return e
				}
			}
			repairs := 2
			spec.Profile = "fast"
			spec.ReasoningEffort = "low"
			spec.MaxRepairs = &repairs
			spec.MaxTokens = 4096
			spec.Timeout = min(600, m.app.cfg.ModelTimeout)
			spec.ContextTokens = 4096
			spec.Apply = false
			spec.SandboxPolicy = "isolated"
			if job.ActionID == "context-long" {
				spec.ContextTokens = min(50000, m.app.cfg.MaxContextTokens)
			}
			specs = append(specs, spec)
		}
		if !found {
			return errors.New("frozen fixture missing: " + want)
		}
	}
	results := []map[string]any{}
	allPassed := true
	for i, spec := range specs {
		if e = ctx.Err(); e != nil {
			return e
		}
		if e = job.update(func() { job.Progress = OperationProgress{Phase: "coding-fixture", Current: i, Total: len(specs)} }); e != nil {
			return e
		}
		if e = writeJSON(filepath.Join(job.RawDirectory, fmt.Sprintf("fixture-%02d-intent.json", i)), spec); e != nil {
			return e
		}
		task, e := m.app.submit(spec)
		if e != nil {
			return e
		}
		if e = job.update(func() { job.TaskIDs = append(job.TaskIDs, task.ID) }); e != nil {
			task.cancel()
			<-task.done
			return e
		}
		if e = job.log("observing owned task " + task.ID + " for " + ids[i]); e != nil {
			task.cancel()
			<-task.done
			return e
		}
		select {
		case <-ctx.Done():
			task.cancel()
			_ = job.update(func() { job.Status = "draining"; job.Progress.Phase = "waiting-for-owned-task-drain" })
			<-task.done
			result := task.snapshot()
			_ = writeJSON(filepath.Join(job.RawDirectory, fmt.Sprintf("fixture-%02d-result.json", i)), result)
			return ctx.Err()
		case <-task.done:
		}
		result := task.snapshot()
		if result["status"] != "PASS" {
			allPassed = false
		}
		if e = writeJSON(filepath.Join(job.RawDirectory, fmt.Sprintf("fixture-%02d-result.json", i)), result); e != nil {
			return e
		}
		results = append(results, result)
		if e = job.update(func() {
			job.Progress.Current = i + 1
			job.Result = map[string]any{"category": job.ActionID, "tasks": results, "all_passed": allPassed, "no_apply": true, "quality_scope": "only frozen supplied tests; no universal or long-context qualification"}
		}); e != nil {
			return e
		}
	}
	if !allPassed {
		return errors.New("one or more frozen tasks did not PASS; negative results retained")
	}
	return ctx.Err()
}

func downloadOperationAsset(ctx context.Context, job *OperationJob, asset CatalogAsset) error {
	if e := validCatalogDownload(asset); e != nil {
		return e
	}
	if !asset.DownloadEnabled {
		return errors.New("catalog asset download is blocked")
	}
	base := "/home/funboy/models"
	if within(asset.Path, "/home/funboy/StrixHaloClusterGLM/.engine/artifacts") {
		base = "/home/funboy/StrixHaloClusterGLM/.engine/artifacts"
	}
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext, ResponseHeaderTimeout: 30 * time.Second}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		host := r.URL.Hostname()
		if len(via) >= 8 || r.URL.Scheme != "https" || r.URL.User != nil || !(host == "huggingface.co" || strings.HasSuffix(host, ".huggingface.co") || strings.HasSuffix(host, ".hf.co")) {
			return errors.New("unapproved download redirect")
		}
		return nil
	}}
	return downloadPinnedOperation(ctx, job, asset, base, client)
}

var operationContentRange = regexp.MustCompile("^bytes ([0-9]+)-([0-9]+)/([0-9]+)$")

// The caller is the catalog-only gate. Internal HTTP injection is only for
// offline regression tests; no API endpoint can select a URL, root or client.
func downloadPinnedOperation(ctx context.Context, job *OperationJob, asset CatalogAsset, base string, client *http.Client) error {
	if !filepath.IsAbs(base) || !within(asset.Path, base) || asset.Path == base || asset.ExpectedBytes <= 0 || !catalogSHA.MatchString(asset.SHA256) {
		return errors.New("invalid pinned download destination")
	}
	if _, e := url.ParseRequestURI(asset.URL); e != nil {
		return e
	}
	realBase, e := filepath.EvalSymlinks(base)
	if e != nil || realBase != base {
		return errors.New("download artifact root must not contain symlinks")
	}
	root, e := os.OpenRoot(base)
	if e != nil {
		return e
	}
	defer func() { root.Close() }()
	relative, e := filepath.Rel(base, asset.Path)
	if e != nil || cleanRel(relative) != nil {
		return errors.New("unsafe pinned destination")
	}
	parts := strings.Split(relative, string(os.PathSeparator))
	for _, part := range parts[:len(parts)-1] {
		info, e := root.Lstat(part)
		if os.IsNotExist(e) {
			if e = root.Mkdir(part, 0700); e != nil {
				return e
			}
			info, e = root.Lstat(part)
		}
		if e != nil {
			return e
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("symlink/non-directory download parent refused")
		}
		next, e := root.OpenRoot(part)
		if e != nil {
			return e
		}
		root.Close()
		root = next
	}
	name := parts[len(parts)-1]
	if _, e = root.Lstat(name); e == nil {
		return errors.New("destination already exists; never overwrite or rehash an existing model")
	} else if !os.IsNotExist(e) {
		return e
	}
	partName, metaName := name+".strixglm.part", name+".strixglm.download.json"
	pin := map[string]any{"asset_id": asset.ID, "url": asset.URL, "revision": asset.Revision, "expected_bytes": asset.ExpectedBytes, "sha256": asset.SHA256}
	pinBytes, e := json.Marshal(pin)
	if e != nil {
		return e
	}
	meta, e := root.OpenFile(metaName, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0600)
	if e == nil {
		if _, pe := root.Lstat(partName); pe == nil {
			meta.Close()
			root.Remove(metaName)
			return errors.New("unowned partial exists without matching pin receipt; no automatic adoption")
		}
		_, e = meta.Write(pinBytes)
		if e == nil {
			e = meta.Sync()
		}
		ce := meta.Close()
		if e == nil {
			e = ce
		}
		if e != nil {
			return e
		}
	} else if os.IsExist(e) {
		meta, e = root.OpenFile(metaName, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
		if e != nil {
			return e
		}
		info, e := meta.Stat()
		if e != nil || !info.Mode().IsRegular() || info.Size() > 8192 {
			meta.Close()
			return errors.New("unsafe partial pin receipt")
		}
		record, e := io.ReadAll(io.LimitReader(meta, 8193))
		meta.Close()
		if e != nil || !bytes.Equal(record, pinBytes) {
			return errors.New("partial pin receipt differs; refusing incompatible resume")
		}
	} else {
		return e
	}
	part, e := root.OpenFile(partName, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if e != nil {
		return e
	}
	defer part.Close()
	info, e := part.Stat()
	if e != nil {
		return e
	}
	if !info.Mode().IsRegular() || info.Size() > asset.ExpectedBytes {
		return errors.New("invalid/oversized partial")
	}
	if st, ok := info.Sys().(*syscall.Stat_t); !ok || st.Nlink != 1 {
		return errors.New("hardlinked partial refused")
	}
	offset := info.Size()
	defer func() { _ = job.update(func() { job.Progress.Bytes = offset }) }()
	var disk syscall.Statfs_t
	if e = syscall.Fstatfs(int(part.Fd()), &disk); e != nil {
		return e
	}
	available := disk.Bavail * uint64(disk.Bsize)
	if available < uint64(asset.ExpectedBytes-offset)+(256<<20) {
		return errors.New("insufficient free space: missing bytes plus256MiB safety margin required")
	}
	if e = job.update(func() {
		job.Progress = OperationProgress{Phase: "download", Bytes: offset, TotalBytes: asset.ExpectedBytes}
	}); e != nil {
		return e
	}
	if e = job.log(fmt.Sprintf("resume pinned asset%s at byte%d/%d; no model loading", asset.ID, offset, asset.ExpectedBytes)); e != nil {
		return e
	}
	if offset < asset.ExpectedBytes {
		request, e := http.NewRequestWithContext(ctx, "GET", asset.URL, nil)
		if e != nil {
			return e
		}
		request.Header.Set("Accept-Encoding", "identity")
		request.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", offset, asset.ExpectedBytes-1))
		response, e := client.Do(request)
		if e != nil {
			return e
		}
		defer response.Body.Close()
		if response.StatusCode == 206 {
			match := operationContentRange.FindStringSubmatch(response.Header.Get("Content-Range"))
			if match == nil {
				return errors.New("missing/invalid Content-Range")
			}
			start, _ := strconv.ParseInt(match[1], 10, 64)
			end, _ := strconv.ParseInt(match[2], 10, 64)
			size, _ := strconv.ParseInt(match[3], 10, 64)
			if start != offset || end != asset.ExpectedBytes-1 || size != asset.ExpectedBytes {
				return errors.New("Content-Range does not match pinned resume")
			}
		} else if response.StatusCode != 200 || offset != 0 {
			return fmt.Errorf("HTTP%d cannot safely resume offset%d", response.StatusCode, offset)
		}
		if response.Header.Get("Content-Encoding") != "" && response.Header.Get("Content-Encoding") != "identity" {
			return errors.New("compressed download representation refused")
		}
		remaining := asset.ExpectedBytes - offset
		if response.ContentLength >= 0 && response.ContentLength != remaining {
			return errors.New("Content-Length differs from expected range")
		}
		if _, e = part.Seek(offset, io.SeekStart); e != nil {
			return e
		}
		buffer := make([]byte, 1<<20)
		last := time.Now()
		for {
			if e = ctx.Err(); e != nil {
				_ = part.Sync()
				return e
			}
			n, readErr := response.Body.Read(buffer)
			if n > 0 {
				if int64(n) > remaining {
					_ = part.Sync()
					return errors.New("download exceeds pinned byte count")
				}
				written, we := part.Write(buffer[:n])
				offset += int64(written)
				remaining -= int64(written)
				if we != nil {
					return we
				}
				if written != n {
					return io.ErrShortWrite
				}
				if time.Since(last) >= time.Second {
					if e = job.update(func() { job.Progress.Bytes = offset }); e != nil {
						return e
					}
					last = time.Now()
				}
			}
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				_ = part.Sync()
				return readErr
			}
		}
		if remaining != 0 {
			_ = part.Sync()
			return errors.New("download ended early; partial preserved for explicit resume")
		}
	}
	if e = part.Sync(); e != nil {
		return e
	}
	if e = job.update(func() { job.Progress.Phase = "verify-sha256"; job.Progress.Bytes = offset }); e != nil {
		return e
	}
	if _, e = part.Seek(0, io.SeekStart); e != nil {
		return e
	}
	hash := sha256.New()
	buffer := make([]byte, 1<<20)
	for {
		if e = ctx.Err(); e != nil {
			return e
		}
		n, re := part.Read(buffer)
		if n > 0 {
			_, _ = hash.Write(buffer[:n])
		}
		if re == io.EOF {
			break
		}
		if re != nil {
			return re
		}
	}
	if hex.EncodeToString(hash.Sum(nil)) != asset.SHA256 {
		return errors.New("final SHA256 mismatch; partial retained, no model published")
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	currentPart, e := root.Lstat(partName)
	openedPart, se := part.Stat()
	if e != nil || se != nil || !currentPart.Mode().IsRegular() || !os.SameFile(currentPart, openedPart) || openedPart.Size() != asset.ExpectedBytes {
		return errors.New("partial identity changed before publication")
	}
	// Link is atomic no-replace: a destination created during download is never
	// overwritten. The completed file and partial initially share the same inode.
	if e = root.Link(partName, name); e != nil {
		return fmt.Errorf("publish refused (no overwrite): %w", e)
	}
	if e = root.Remove(partName); e != nil {
		return e
	}
	if directory, e := root.Open("."); e == nil {
		e = directory.Sync()
		directory.Close()
		if e != nil {
			return e
		}
	} else {
		return e
	}
	if e = job.update(func() {
		job.Progress.Phase = "complete"
		job.Progress.Bytes = asset.ExpectedBytes
		job.Result = map[string]any{"asset_id": asset.ID, "path": asset.Path, "bytes": asset.ExpectedBytes, "sha256": asset.SHA256, "verification": "SHA256_VERIFIED_THIS_DOWNLOAD", "model_loaded": false}
	}); e != nil {
		return e
	}
	return job.log("download verified and published; runtime unchanged")
}
