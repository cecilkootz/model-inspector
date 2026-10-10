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

	"origin.cursor.com/cecilkootz/model-inspector/internal/safeopen"
)

// MaxRefBytes is the most that will be read from a refs/ file.
// A commit hash is well under this; anything larger is refused.
const MaxRefBytes = 1024

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

var (
	repoIDRe = regexp.MustCompile(`^[A-Za-z0-9][\w.-]*/[\w.-]+$`)
	// commitRe is a git object id: 40 hex chars, or up to 64 for SHA-256.
	commitRe = regexp.MustCompile(`^[0-9a-f]{40,64}$`)
)

// RepoFolderName maps "org/name" to the cache folder "models--org--name".
func RepoFolderName(id string) string { return "models--" + strings.ReplaceAll(id, "/", "--") }

// Resolve accepts a directory path, or "org/name" optionally suffixed with
// "@ref" or "@commit", looked up under hubDir.
//
// A ref name's segments must not contain "..". The commit ultimately used,
// whether it came from refs/<name> or from @<commit>, must match commitRe.
// The resolved snapshot path has to stay under snapshots/.
func Resolve(arg, hubDir string) (Resolved, error) {
	if fi, err := os.Stat(arg); err == nil && fi.IsDir() {
		abs, err := filepath.Abs(arg)
		if err != nil {
			return Resolved{}, err
		}
		real, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return Resolved{}, err
		}
		r := Resolved{Dir: real}
		r.RepoID, r.Revision = InferFromPath(real)
		return r, nil
	}
	id, rev, _ := strings.Cut(arg, "@")
	if !repoIDRe.MatchString(id) {
		return Resolved{}, fmt.Errorf("%q is neither a directory nor an org/name repo id", arg)
	}
	repoDir := filepath.Join(hubDir, RepoFolderName(id))
	repoReal, err := filepath.EvalSymlinks(repoDir)
	if err != nil {
		if os.IsNotExist(err) {
			return Resolved{}, fmt.Errorf("repo %s not found in HF cache %s", id, hubDir)
		}
		return Resolved{}, err
	}
	ref := rev
	if ref == "" {
		ref = "main"
	}
	if err := validRef(ref); err != nil {
		return Resolved{}, fmt.Errorf("repo %s: %w", id, err)
	}
	refsRoot := filepath.Join(repoReal, "refs")
	refPath := filepath.Join(refsRoot, filepath.FromSlash(ref))
	if !safeopen.Within(refsRoot, refPath) {
		return Resolved{}, fmt.Errorf("repo %s: ref %q escapes refs/", id, ref)
	}

	commit, err := readCommit(id, ref, rev, refPath)
	if err != nil {
		return Resolved{}, err
	}
	if !commitRe.MatchString(commit) {
		return Resolved{}, fmt.Errorf("repo %s: revision %q is not a 40-64 character hex commit", id, commit)
	}

	snapRoot := filepath.Join(repoReal, "snapshots")
	snapRootReal, err := filepath.EvalSymlinks(snapRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return Resolved{}, fmt.Errorf("snapshot %s not present for %s", commit, id)
		}
		return Resolved{}, err
	}
	if !safeopen.Within(repoReal, snapRootReal) {
		return Resolved{}, fmt.Errorf("repo %s: snapshots directory escapes the repo", id)
	}
	snap := filepath.Join(snapRootReal, commit)
	if !safeopen.Within(snapRootReal, snap) {
		return Resolved{}, fmt.Errorf("repo %s: revision %q escapes snapshots/", id, commit)
	}
	realSnap, err := filepath.EvalSymlinks(snap)
	if err != nil {
		return Resolved{}, fmt.Errorf("snapshot %s not present for %s", commit, id)
	}
	if !safeopen.Within(snapRootReal, realSnap) {
		return Resolved{}, fmt.Errorf("snapshot %s escapes snapshots/ for %s", commit, id)
	}
	return Resolved{Dir: realSnap, RepoID: id, Revision: commit, Ref: ref, FromCache: true}, nil
}

func readCommit(id, ref, rev, refPath string) (string, error) {
	b, err := safeopen.ReadAll(refPath, MaxRefBytes)
	switch {
	case err == nil:
		commit := strings.TrimSpace(string(b))
		if !commitRe.MatchString(commit) {
			return "", fmt.Errorf("repo %s: refs/%s is not a 40-64 character hex commit", id, ref)
		}
		return commit, nil
	case os.IsNotExist(err) && rev != "" && commitRe.MatchString(rev):
		return rev, nil
	case os.IsNotExist(err) && rev == "":
		return "", fmt.Errorf("repo %s has no refs/main; pass org/name@<commit>", id)
	case os.IsNotExist(err):
		return "", fmt.Errorf("repo %s: ref %q is not a commit hash and refs/%s is missing", id, rev, ref)
	default:
		return "", fmt.Errorf("repo %s: refs/%s: %w", id, ref, err)
	}
}

// validRef rejects absolute paths and any segment containing "..".
func validRef(ref string) error {
	if ref == "" || strings.ContainsRune(ref, 0) || strings.Contains(ref, `\`) || filepath.IsAbs(ref) {
		return fmt.Errorf("invalid ref %q", ref)
	}
	for _, seg := range strings.Split(ref, "/") {
		if seg == "" || seg == "." || strings.Contains(seg, "..") {
			return fmt.Errorf("invalid ref %q", ref)
		}
	}
	if !filepath.IsLocal(ref) {
		return fmt.Errorf("invalid ref %q", ref)
	}
	return nil
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
