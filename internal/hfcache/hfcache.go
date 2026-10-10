// Package hfcache resolves a model argument (a directory or an HF repo id)
// to a local snapshot directory inside the Hugging Face hub cache.
// It only reads; it never writes into the cache.
package hfcache

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"origin.cursor.com/cecilkootz/model-inspector/internal/safeopen"
	"origin.cursor.com/cecilkootz/model-inspector/internal/sanitize"
)

// ErrAmbiguousTarget is returned when a bare argument is both an HF repo id
// and an existing directory. Callers must not pick one.
var ErrAmbiguousTarget = errors.New("ambiguous model argument")

// TargetMode selects how a user argument is interpreted.
type TargetMode int

const (
	// TargetAuto refuses the case where a relative repo id is also a directory.
	TargetAuto TargetMode = iota
	// TargetPath is always a filesystem directory, never a hub lookup.
	TargetPath
	// TargetRepo is always an HF repo id, never a cwd-relative directory.
	TargetRepo
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
// A relative directory wins over a repo id of the same string. The CLI does
// not call Resolve for user input; it calls ResolveTarget, which refuses
// that ambiguous case. Resolve remains for callers that already know the
// argument is a directory or that a missing path should fall through to the hub.
//
// A ref name's segments must not contain "..". The commit ultimately used,
// whether it came from refs/<name> or from @<commit>, must match commitRe.
// The resolved snapshot path has to stay under snapshots/.
func Resolve(arg, hubDir string) (Resolved, error) {
	if fi, err := os.Stat(arg); err == nil && fi.IsDir() {
		return resolveExistingDir(arg)
	}
	return resolveRepo(arg, hubDir)
}

// ResolveTarget applies the CLI rule for a model argument.
//
//   - TargetPath never consults the hub. A missing path is an error.
//   - TargetRepo never stats the working directory.
//   - TargetAuto treats an absolute path, a "./" or "../" prefix, a trailing
//     separator, or anything that is not org/name[@ref] as a path.
//     A bare org/name[@ref] that does not exist as a directory is a repo id.
//     The local check uses the repo id with @ref removed, so org/name@main
//     is ambiguous when ./org/name is a directory.
//     A bare org/name[@ref] that is also a directory, or a broken symlink
//     of that name, is ErrAmbiguousTarget. It is not looked up in the hub.
//     A leading ~ is not expanded.
func ResolveTarget(arg, hubDir string, mode TargetMode) (Resolved, error) {
	switch mode {
	case TargetPath:
		return resolvePath(arg)
	case TargetRepo:
		return resolveRepo(arg, hubDir)
	default:
		if explicitPath(arg) || !repoShaped(arg) {
			return resolvePath(arg)
		}
		return resolveAutoRepo(arg, hubDir)
	}
}

// resolveAutoRepo decides a bare org/name[@ref]. The directory check uses
// the id without @ref. A broken symlink is ambiguous, not a hub miss.
func resolveAutoRepo(arg, hubDir string) (Resolved, error) {
	id, _, _ := strings.Cut(arg, "@")
	lst, lerr := os.Lstat(id)
	st, serr := os.Stat(id)
	switch {
	case lerr == nil && serr != nil && errors.Is(serr, fs.ErrNotExist):
		return Resolved{}, fmt.Errorf("%w: %q matches an HF repo id and a broken symlink; pass --repo to read the hub cache, or --path to read that path", ErrAmbiguousTarget, arg)
	case serr == nil && st.IsDir():
		return Resolved{}, fmt.Errorf("%w: %q matches an HF repo id and an existing directory; pass --repo to read the hub cache, or --path to read that directory", ErrAmbiguousTarget, arg)
	case serr == nil:
		return resolveRepo(arg, hubDir)
	case lerr == nil && !lst.Mode().IsRegular() && serr != nil && !errors.Is(serr, fs.ErrNotExist):
		return Resolved{}, fmt.Errorf("%w: %q matches an HF repo id and a local path (%v); pass --path or --repo", ErrAmbiguousTarget, arg, serr)
	case errors.Is(serr, fs.ErrNotExist) || errors.Is(lerr, fs.ErrNotExist):
		return resolveRepo(arg, hubDir)
	default:
		if serr == nil {
			serr = lerr
		}
		return Resolved{}, fmt.Errorf("%q: stat: %v; pass --path or --repo", arg, serr)
	}
}

func explicitPath(arg string) bool {
	if arg == "." || arg == ".." || filepath.IsAbs(arg) {
		return true
	}
	if strings.HasPrefix(arg, "./") || strings.HasPrefix(arg, "../") {
		return true
	}
	if strings.HasSuffix(arg, "/") || strings.HasSuffix(arg, string(filepath.Separator)) {
		return true
	}
	return strings.Count(filepath.ToSlash(arg), "/") > 1
}

func repoShaped(arg string) bool {
	id, _, _ := strings.Cut(arg, "@")
	return repoIDRe.MatchString(id)
}

func resolvePath(arg string) (Resolved, error) {
	fi, err := os.Stat(arg)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Resolved{}, fmt.Errorf("path %s not found", arg)
		}
		return Resolved{}, err
	}
	if !fi.IsDir() {
		return Resolved{}, fmt.Errorf("%s is not a directory", arg)
	}
	return resolveExistingDir(arg)
}

func resolveExistingDir(arg string) (Resolved, error) {
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

func resolveRepo(arg, hubDir string) (Resolved, error) {
	id, rev, _ := strings.Cut(arg, "@")
	if !repoIDRe.MatchString(id) {
		return Resolved{}, fmt.Errorf("%q is neither a directory nor an org/name repo id", arg)
	}
	repoDir := filepath.Join(hubDir, RepoFolderName(id))
	repoReal, err := filepath.EvalSymlinks(repoDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
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
	refsReal := refsRoot
	if real, err := filepath.EvalSymlinks(refsRoot); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return Resolved{}, err
		}
	} else if !safeopen.Within(repoReal, real) {
		return Resolved{}, fmt.Errorf("repo %s: refs directory escapes the repo", id)
	} else {
		refsReal = real
	}
	refPath := filepath.Join(refsReal, filepath.FromSlash(ref))
	if !safeopen.Within(refsReal, refPath) {
		return Resolved{}, fmt.Errorf("repo %s: ref %q escapes refs/", id, ref)
	}

	commit, err := readCommit(id, ref, rev, refPath, refsReal)
	if err != nil {
		return Resolved{}, err
	}
	if !commitRe.MatchString(commit) {
		return Resolved{}, fmt.Errorf("repo %s: revision %q is not a 40-64 character hex commit", id, commit)
	}

	snapRoot := filepath.Join(repoReal, "snapshots")
	snapRootReal, err := filepath.EvalSymlinks(snapRoot)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
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

func readCommit(id, ref, rev, refPath, refsRoot string) (string, error) {
	// refsRoot contains the opened descriptor. A symlink from refs/ to a
	// regular file outside the repo is refused, not followed blindly.
	b, err := safeopen.ReadAll(refPath, MaxRefBytes, refsRoot)
	switch {
	case err == nil:
		commit := strings.TrimSpace(string(b))
		if !commitRe.MatchString(commit) {
			return "", fmt.Errorf("repo %s: refs/%s is not a 40-64 character hex commit", id, ref)
		}
		return commit, nil
	case errors.Is(err, fs.ErrNotExist) && rev != "" && commitRe.MatchString(rev):
		return rev, nil
	case errors.Is(err, fs.ErrNotExist) && rev == "":
		return "", sanitize.Notef("repo %s has no refs/main; pass org/name@<commit>", id)
	case errors.Is(err, fs.ErrNotExist):
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
