package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

type Workspace struct {
	Repo     string
	Original map[string][]byte
	Modes    map[string]fs.FileMode
	Existed  map[string]bool
	RepoInfo fs.FileInfo
	Verified map[string]string
	Selected []string
	Editable map[string]bool
	Hidden   map[string][]byte
	Estimate int
}

func eligible(path string) bool {
	for _, p := range strings.Split(path, "/") {
		if strings.HasPrefix(p, ".") || p == "node_modules" || p == "target" || p == "build" || p == "dist" || p == "vendor" || p == "state" {
			return false
		}
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pem", ".key", ".p12", ".sqlite", ".db", ".gguf", ".safetensors", ".so", ".a", ".o", ".zip", ".gz":
		return false
	}
	return true
}
func readSmall(path string, max int) ([]byte, error) {
	s, e := os.Lstat(path)
	if e != nil {
		return nil, e
	}
	if !s.Mode().IsRegular() || s.Size() > int64(max) {
		return nil, fmt.Errorf("not small regular file: %s", path)
	}
	b, e := os.ReadFile(path)
	if e == nil && (!utf8.Valid(b) || strings.ContainsRune(string(b), 0)) {
		return nil, fmt.Errorf("not UTF-8 text: %s", path)
	}
	return b, e
}
func prepareWorkspace(c Config, s TaskSpec, profile Profile) (Workspace, error) {
	w := Workspace{Original: map[string][]byte{}, Modes: map[string]fs.FileMode{}, Existed: map[string]bool{}, Verified: map[string]string{}, Editable: map[string]bool{}, Hidden: map[string][]byte{}}
	var e error
	w.Repo, e = realPath(s.Repo)
	if e != nil {
		return w, e
	}
	w.RepoInfo, e = os.Lstat(w.Repo)
	if e != nil || !w.RepoInfo.IsDir() {
		return w, errors.New("workspace root is not a stable directory")
	}
	ok := false
	for _, r := range c.WorkspaceRoots {
		if within(w.Repo, r) && w.Repo != r {
			ok = true
		}
	}
	if !ok {
		return w, errors.New("repo outside configured workspace roots (or root itself)")
	}
	total := 0
	e = filepath.WalkDir(w.Repo, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == w.Repo {
			return nil
		}
		rel, _ := filepath.Rel(w.Repo, path)
		if !eligible(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink refused in workspace: %s", rel)
		}
		if d.IsDir() {
			return nil
		}
		b, err := readSmall(path, c.MaxFileBytes)
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("source changed to non-regular file: %s", rel)
		}
		total += len(b)
		if total > c.MaxRepoBytes || len(w.Original) >= c.MaxFiles {
			return errors.New("repository exceeds configured text snapshot size; select smaller subproject")
		}
		w.Original[rel] = b
		w.Modes[rel] = info.Mode().Perm()
		w.Existed[rel] = true
		return nil
	})
	if e != nil {
		return w, e
	}
	if len(s.AllowedPaths) == 0 {
		return w, errors.New("allowed_paths required; specify exact files or directory prefixes ending /")
	}
	for _, p := range s.AllowedPaths {
		dir := strings.HasSuffix(p, "/")
		name := strings.TrimSuffix(p, "/")
		if e := cleanRel(name); e != nil {
			return w, e
		}
		if !eligible(name) {
			return w, errors.New("sensitive/generated allowed path refused")
		}
		if dir {
			for n := range w.Original {
				if strings.HasPrefix(n, p) {
					w.Editable[n] = true
				}
			}
		} else {
			w.Editable[p] = true
			if _, ok := w.Original[p]; !ok {
				w.Original[p] = []byte{}
				w.Modes[p] = 0600
				w.Existed[p] = false
			}
		}
	}
	requested := map[string]bool{}
	for _, p := range s.Files {
		if e := cleanRel(p); e != nil {
			return w, e
		}
		if _, ok := w.Original[p]; !ok {
			return w, fmt.Errorf("context file missing: %s", p)
		}
		requested[p] = true
	}
	for p := range w.Editable {
		requested[p] = true
	}
	// Rank explicit sources first, then direct dependency references, issue terms,
	// build metadata. No vectors/network, no source or test code execution.
	scores := map[string]int{}
	terms := strings.FieldsFunc(strings.ToLower(s.Task), func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') })
	for name, body := range w.Original {
		score := 0
		if requested[name] {
			score += 10000
		}
		base := filepath.Base(name)
		for req := range requested {
			if strings.Contains(string(w.Original[req]), base) {
				score += 500
			}
		}
		for _, t := range terms {
			if len(t) > 3 && strings.Contains(strings.ToLower(name), t) {
				score += 50
			}
		}
		if base == "Makefile" || base == "package.json" || base == "Cargo.toml" || base == "CMakeLists.txt" {
			score += 100
		}
		_ = body
		scores[name] = score
	}
	names := make([]string, 0, len(scores))
	for n := range scores {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		if scores[names[i]] == scores[names[j]] {
			return names[i] < names[j]
		}
		return scores[names[i]] > scores[names[j]]
	})
	budget := profile.ContextTokens * 3
	used := len(s.Task) + 1000
	for _, n := range names {
		cost := len(w.Original[n]) + len(n) + 30
		if used+cost > budget {
			if requested[n] {
				return w, fmt.Errorf("explicit context exceeds estimated profile budget at %s; increase context_tokens or narrow files", n)
			}
			continue
		}
		if scores[n] == 0 && len(w.Selected) > 0 {
			continue
		}
		w.Selected = append(w.Selected, n)
		used += cost
	}
	sort.Strings(w.Selected)
	w.Estimate = (used + 2) / 3
	for dest, source := range s.TestFiles {
		if e := cleanRel(dest); e != nil {
			return w, e
		}
		if _, ok := w.Original[dest]; ok {
			return w, errors.New("hidden test overlaps repository file")
		}
		rp, e := realPath(source)
		if e != nil {
			return w, e
		}
		allowed := false
		for _, root := range c.WorkspaceRoots {
			if within(rp, root) {
				allowed = true
			}
		}
		if !allowed {
			return w, errors.New("hidden test outside workspace roots")
		}
		b, e := readSmall(rp, c.MaxFileBytes)
		if e != nil {
			return w, e
		}
		w.Hidden[dest] = b
	}
	return w, nil
}
func hashes(files map[string][]byte) map[string]string {
	r := map[string]string{}
	for n, b := range files {
		s := sha256.Sum256(b)
		r[n] = hex.EncodeToString(s[:])
	}
	return r
}
func cloneFiles(files map[string][]byte) map[string][]byte {
	r := map[string][]byte{}
	for n, b := range files {
		r[n] = append([]byte(nil), b...)
	}
	return r
}
