package hfcache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func FuzzTarget(f *testing.F) {
	f.Add("org/name")
	f.Add("org/name@main")
	f.Add("org/name@0123456789abcdef0123456789abcdef01234567")
	f.Add("./org/name")
	f.Add("../org/name")
	f.Add("~/org/name")
	f.Add("")
	f.Add("org/name/")
	f.Add("/tmp/model")
	f.Add("./")
	f.Add("../")
	f.Fuzz(func(t *testing.T, arg string) {
		if len(arg) > 256 {
			return
		}
		hub := t.TempDir()
		const commit = "0123456789abcdef0123456789abcdef01234567"
		snap := filepath.Join(hub, "models--org--name", "snapshots", commit)
		if err := os.MkdirAll(snap, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(hub, "models--org--name", "refs"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(hub, "models--org--name", "refs", "main"), []byte(commit+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(snap, "config.json"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		snapReal, err := filepath.EvalSymlinks(snap)
		if err != nil {
			t.Fatal(err)
		}
		// The seed has to be a real snapshot. If this misses, the explicit-path
		// check below cannot tell a hub hit from a hub miss.
		seeded, seedErr := ResolveTarget("org/name", hub, TargetRepo)
		if seedErr != nil || !seeded.FromCache || seeded.Dir != snapReal {
			t.Fatalf("seeded hub did not resolve: %+v %v", seeded, seedErr)
		}
		outside := filepath.Join(t.TempDir(), "explicit-weights")
		if err := os.MkdirAll(outside, 0o755); err != nil {
			t.Fatal(err)
		}
		checkExplicit := func(mode TargetMode, path string) {
			t.Helper()
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("ResolveTarget mode %d arg %q panicked: %v", mode, path, r)
				}
			}()
			got, err := ResolveTarget(path, hub, mode)
			if err != nil || !independentExplicit(path, mode) {
				return
			}
			abs, absErr := filepath.Abs(path)
			if absErr != nil {
				t.Fatalf("abs %q: %v", path, absErr)
			}
			real, realErr := filepath.EvalSymlinks(abs)
			if realErr != nil {
				t.Fatalf("explicit path %q resolved to %s but does not eval: %v", path, got.Dir, realErr)
			}
			// FromCache is the hub flag. A path the user named may be the
			// snapshot directory itself; any other directory must not come
			// back as that seeded snapshot.
			if got.FromCache || got.Dir != real || (got.Dir == snapReal && real != snapReal) {
				t.Fatalf("explicit path %q mode %d resolved to hub snapshot %s as %+v, want %s", path, mode, snapReal, got, real)
			}
		}
		for _, mode := range []TargetMode{TargetAuto, TargetPath} {
			checkExplicit(mode, outside)
			checkExplicit(mode, outside+string(os.PathSeparator))
		}
		for _, mode := range []TargetMode{TargetAuto, TargetPath, TargetRepo} {
			checkExplicit(mode, arg)
		}
	})
}

// independentExplicit is the explicit-path rule written out again, so a
// bug in explicitPath cannot hide itself. TargetPath is always a path.
// TargetAuto is a path when the argument is absolute, ".", "..", starts
// with ./ or ../, has a trailing slash, or contains more than one slash.
func independentExplicit(arg string, mode TargetMode) bool {
	if mode == TargetPath {
		return true
	}
	if mode != TargetAuto {
		return false
	}
	if arg == "." || arg == ".." {
		return true
	}
	if strings.HasPrefix(arg, "/") || strings.HasPrefix(arg, "./") || strings.HasPrefix(arg, "../") {
		return true
	}
	if strings.HasSuffix(arg, "/") {
		return true
	}
	slashes := 0
	for i := 0; i < len(arg); i++ {
		if arg[i] == '/' {
			slashes++
		}
	}
	return slashes > 1
}
