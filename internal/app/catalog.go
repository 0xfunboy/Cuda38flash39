package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	runtimeassets "strixhaloclusterglm/runtime"
)

// The audited inventory is compiled into the product. A client cannot inject
// model commands, download URLs or destination paths by editing request JSON.
var modelCatalogJSON = runtimeassets.ModelCatalogJSON

type Catalog struct {
	Schema          int            `json:"schema"`
	AuditedUTC      string         `json:"audited_utc"`
	GeneratedUTC    string         `json:"generated_utc,omitempty"`
	ActiveModelID   string         `json:"active_model_id,omitempty"`
	ActivitySource  string         `json:"activity_source"`
	InventoryMethod string         `json:"inventory_method"`
	Sources         []string       `json:"sources"`
	Models          []CatalogModel `json:"models"`
}
type CatalogSource struct {
	URL      string `json:"url"`
	Revision string `json:"revision"`
	Evidence string `json:"evidence"`
}
type CatalogAsset struct {
	ID                    string `json:"id"`
	Path                  string `json:"path"`
	Node                  string `json:"node"`
	Role                  string `json:"role"`
	ExpectedBytes         int64  `json:"expected_bytes"`
	SHA256                string `json:"sha256,omitempty"`
	HashSource            string `json:"hash_source,omitempty"`
	URL                   string `json:"url,omitempty"`
	Revision              string `json:"revision,omitempty"`
	Required              bool   `json:"required"`
	DownloadEnabled       bool   `json:"download_enabled"`
	DownloadBlockedReason string `json:"download_blocked_reason"`
	Present               *bool  `json:"present"`
	ActualBytes           int64  `json:"actual_bytes"`
	Verification          string `json:"verification"`
}
type CatalogAction struct {
	Enabled bool   `json:"enabled"`
	Reason  string `json:"reason"`
}
type CatalogActions struct {
	Load      CatalogAction `json:"load"`
	Benchmark CatalogAction `json:"benchmark"`
	Download  CatalogAction `json:"download"`
}
type CatalogPreset struct {
	ID           string   `json:"id"`
	Label        string   `json:"label"`
	Distribution string   `json:"distribution"`
	Status       string   `json:"status"`
	Reasoning    string   `json:"reasoning"`
	Blockers     []string `json:"blockers"`
}
type CatalogEvidence struct {
	Label         string   `json:"label"`
	Status        string   `json:"status"`
	DecodeTPS     *float64 `json:"decode_tps"`
	HTTPTPS       *float64 `json:"http_tps"`
	ContextTokens *int     `json:"context_tokens"`
	Report        string   `json:"report"`
	Notes         string   `json:"notes"`
}
type CatalogModel struct {
	ID                 string            `json:"id"`
	Name               string            `json:"name"`
	ServingModelID     string            `json:"serving_model_id,omitempty"`
	Priority           int               `json:"priority"`
	Status             string            `json:"status"`
	Format             string            `json:"format"`
	Runtime            string            `json:"runtime"`
	Architecture       string            `json:"architecture"`
	Distribution       string            `json:"distribution"`
	Testability        string            `json:"testability"`
	NgramEmbedding     string            `json:"ngram_embedding"`
	DraftLookup        string            `json:"draft_lookup"`
	QualityLimits      []string          `json:"quality_limits"`
	Blockers           []string          `json:"blockers"`
	Sources            []CatalogSource   `json:"sources"`
	Assets             []CatalogAsset    `json:"assets"`
	Presets            []CatalogPreset   `json:"presets"`
	LegacyPresetIDs    []string          `json:"legacy_preset_ids"`
	Actions            CatalogActions    `json:"actions"`
	Evidence           []CatalogEvidence `json:"evidence"`
	LocalBytes         int64             `json:"local_bytes"`
	ExpectedLocalBytes int64             `json:"expected_local_bytes"`
}

var catalogRevision = regexp.MustCompile(`^[0-9a-f]{40}$`)
var catalogSHA = regexp.MustCompile(`^[0-9a-f]{64}$`)

func validCatalogDownload(a CatalogAsset) error {
	u, err := url.Parse(a.URL)
	if err != nil || u.Scheme != "https" || u.Host != "huggingface.co" || u.User != nil || u.Fragment != "" || !catalogRevision.MatchString(a.Revision) || !catalogSHA.MatchString(a.SHA256) || a.ExpectedBytes <= 0 {
		return errors.New("download requires immutable Hugging Face revision, SHA256 and positive size")
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) < 5 || parts[2] != "resolve" || parts[3] != a.Revision || strings.Contains(u.Path, "..") {
		return errors.New("download URL does not contain the pinned resolve revision")
	}
	if a.Node != "NODE01" || !filepath.IsAbs(a.Path) || filepath.Clean(a.Path) != a.Path || !(within(a.Path, "/home/funboy/models") || within(a.Path, "/home/funboy/StrixHaloClusterGLM/.engine/artifacts")) {
		return errors.New("download destination outside audited local artifact roots")
	}
	return nil
}

func loadModelCatalog() (Catalog, error) {
	var c Catalog
	if err := json.Unmarshal(modelCatalogJSON, &c); err != nil {
		return c, err
	}
	if c.Schema != 1 || len(c.Models) == 0 {
		return c, errors.New("invalid model catalog")
	}
	seen := map[string]bool{}
	assets := map[string]bool{}
	for _, m := range c.Models {
		if m.ID == "" || seen[m.ID] {
			return c, errors.New("duplicate/empty model catalog id")
		}
		seen[m.ID] = true
		for _, a := range m.Assets {
			if a.ID == "" || assets[a.ID] {
				return c, errors.New("duplicate/empty asset catalog id")
			}
			assets[a.ID] = true
			if a.DownloadEnabled {
				if err := validCatalogDownload(a); err != nil {
					return c, fmt.Errorf("asset %s: %w", a.ID, err)
				}
			}
		}
	}
	return c, nil
}

func catalogSnapshot(c Catalog, configured string, stat func(string) (fs.FileInfo, error)) Catalog {
	c.GeneratedUTC = time.Now().UTC().Format(time.RFC3339)
	for i := range c.Models {
		m := &c.Models[i]
		if m.ServingModelID != "" && m.ServingModelID == configured {
			c.ActiveModelID = m.ID
		}
		missing := false
		canDownload := false
		for j := range m.Assets {
			a := &m.Assets[j]
			if a.Node != "NODE01" {
				a.Present = nil
				a.Verification = "REMOTE_NOT_CHECKED; prior evidence only, no SSH from catalog GET"
				a.DownloadEnabled = false
				a.DownloadBlockedReason = "Remote copy/lifecycle requires an explicit paired operation"
				continue
			}
			m.ExpectedLocalBytes += a.ExpectedBytes
			info, err := stat(a.Path)
			present := err == nil
			a.Present = &present
			if err == nil && info.Mode().IsRegular() {
				a.ActualBytes = info.Size()
				m.LocalBytes += info.Size()
				a.DownloadEnabled = false
				a.DownloadBlockedReason = "Local destination already exists; never overwrite/re-download from catalog"
				if a.ExpectedBytes > 0 && a.ExpectedBytes != info.Size() {
					a.Verification = "SIZE_MISMATCH; no hash read"
					if a.Required {
						missing = true
					}
				} else {
					a.Verification = "STAT_SIZE_MATCH_ONLY; prior SHA evidence is not a fresh checksum"
				}
			} else if errors.Is(err, fs.ErrNotExist) {
				a.Verification = "NOT_LOCAL"
				if a.Required {
					missing = true
				}
				canDownload = canDownload || a.DownloadEnabled
			} else {
				a.Verification = "BLOCKED_FILE_TYPE_OR_STAT; no data read"
				a.DownloadEnabled = false
				a.DownloadBlockedReason = "Destination is non-regular or cannot be inspected safely"
				if a.Required {
					missing = true
				}
			}
		}
		m.Actions.Download = CatalogAction{canDownload, "No missing local asset with an enabled immutable download pin"}
		if canDownload {
			m.Actions.Download.Reason = "Pinned missing assets only; explicit selection/confirmation and disk budget required"
		}
		if missing {
			m.Actions.Load = CatalogAction{false, "Required local asset missing, non-regular or size mismatch"}
			m.Actions.Benchmark = CatalogAction{false, "Required local asset missing, non-regular or size mismatch"}
		}
		if m.ServingModelID != configured && m.Actions.Benchmark.Enabled {
			m.Actions.Benchmark = CatalogAction{false, "Configured backend is not this model; no automatic model switch"}
		}
	}
	sort.SliceStable(c.Models, func(i, j int) bool { return c.Models[i].Priority < c.Models[j].Priority })
	return c
}

// Selection is by immutable catalog id, never by an arbitrary client URL/path.
// The operation manager still owns confirmation, disk budget, partial-file
// handling and final SHA validation; this function never reads model bytes.
func catalogDownloadAsset(id string) (CatalogAsset, error) {
	c, err := loadModelCatalog()
	if err != nil {
		return CatalogAsset{}, err
	}
	for _, m := range c.Models {
		for _, a := range m.Assets {
			if a.ID == id {
				if !a.DownloadEnabled {
					return CatalogAsset{}, fmt.Errorf("asset download blocked: %s", a.DownloadBlockedReason)
				}
				if err := validCatalogDownload(a); err != nil {
					return CatalogAsset{}, err
				}
				if _, err := os.Lstat(a.Path); err == nil {
					return CatalogAsset{}, errors.New("destination already exists; download would overwrite local data")
				} else if !errors.Is(err, fs.ErrNotExist) {
					return CatalogAsset{}, err
				}
				return a, nil
			}
		}
	}
	return CatalogAsset{}, errors.New("unknown catalog asset")
}

func registerCatalogRoutes(a *App, mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/catalog", func(w http.ResponseWriter, r *http.Request) {
		if !a.authorized(w, r) {
			return
		}
		c, err := loadModelCatalog()
		if err != nil {
			jsonReply(w, 500, map[string]string{"error": err.Error()})
			return
		}
		jsonReply(w, 200, catalogSnapshot(c, a.cfg.Model, os.Lstat))
	})
}
