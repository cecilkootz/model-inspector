// Package hfcache resolves a model argument (a directory or an HF repo id)
// to a local snapshot directory inside the Hugging Face hub cache.
// It only reads; it never writes into the cache.
package hfcache

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// DefaultHubDir follows the huggingface_hub conventions.
func DefaultHubDir() string {
	if v := os.Getenv("HF_HUB_CACHE"); v != "" {
		return v
	}
	if v := os.Getenv("HUGGINGFACE_HUB_CACHE"); v != "" {
		return v
	}
	if v := os.Getenv("HF_HOME"); v != "" {
		return filepath.Join(v, "hub")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "huggingface", "hub")
}

// Resolved is the outcome of Resolve.
type Resolved struct {
	Dir       string `json:"dir"`
	RepoID    string `json:"repo_id,omitempty"`
	Revision  string `json:"revision,omitempty"`
	Ref       string `json:"ref,omitempty"`
	FromCache bool   `json:"from_cache"`
}

var repoIDRe = regexp.MustCompile(`^[A-Za-z0-9][\w.-]*/[\w.-]+$`)

// RepoFolderName maps "org/name" to the cache folder "models--org--name".
func RepoFolderName(id string) string { return "models--" + strings.ReplaceAll(id, "/", "--") }

// Resolve accepts a directory path, or "org/name" optionally suffixed with
// "@ref" or "@commit", looked up under hubDir.
func Resolve(arg, hubDir string) (Resolved, error) {
	if fi, err := os.Stat(arg); err == nil && fi.IsDir() {
		abs, err := filepath.Abs(arg)
		if err != nil {
			return Resolved{}, err
		}
		r := Resolved{Dir: abs}
		r.RepoID, r.Revision = InferFromPath(abs)
		return r, nil
	}
	id, rev, _ := strings.Cut(arg, "@")
	if !repoIDRe.MatchString(id) {
		return Resolved{}, fmt.Errorf("%q is neither a directory nor an org/name repo id", arg)
	}
	repoDir := filepath.Join(hubDir, RepoFolderName(id))
	if _, err := os.Stat(repoDir); err != nil {
		return Resolved{}, fmt.Errorf("repo %s not found in HF cache %s", id, hubDir)
	}
	ref := rev
	if ref == "" {
		ref = "main"
	}
	commit := ref
	if b, err := os.ReadFile(filepath.Join(repoDir, "refs", ref)); err == nil {
		commit = strings.TrimSpace(string(b))
	} else if rev == "" {
		return Resolved{}, fmt.Errorf("repo %s has no refs/main; pass org/name@<commit>", id)
	}
	snap := filepath.Join(repoDir, "snapshots", commit)
	if fi, err := os.Stat(snap); err != nil || !fi.IsDir() {
		return Resolved{}, fmt.Errorf("snapshot %s not present for %s", commit, id)
	}
	return Resolved{Dir: snap, RepoID: id, Revision: commit, Ref: ref, FromCache: true}, nil
}

// InferFromPath recovers repo id and revision from a .../models--org--name/snapshots/<rev> path.
func InferFromPath(p string) (repo, rev string) {
	parts := strings.Split(filepath.ToSlash(p), "/")
	for i := 0; i+2 < len(parts); i++ {
		if strings.HasPrefix(parts[i], "models--") && parts[i+1] == "snapshots" {
			return strings.Replace(strings.TrimPrefix(parts[i], "models--"), "--", "/", 1), parts[i+2]
		}
	}
	return "", ""
}
