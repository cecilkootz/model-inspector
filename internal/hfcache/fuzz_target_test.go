package hfcache

import (
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
		for _, mode := range []TargetMode{TargetAuto, TargetPath, TargetRepo} {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("ResolveTarget mode %d arg %q panicked: %v", mode, arg, r)
					}
				}()
				got, err := ResolveTarget(arg, hub, mode)
				if err != nil || !explicitTarget(arg, mode) {
					return
				}
				// An explicit path is the directory the user named (--path,
				// absolute, ./, ../, or a trailing slash). It must not come
				// back as a snapshot inside the HF hub cache.
				if got.FromCache {
					t.Fatalf("explicit path %q mode %d resolved from the hub cache: %+v", arg, mode, got)
				}
				abs, absErr := filepath.Abs(arg)
				if absErr != nil {
					t.Fatalf("abs %q: %v", arg, absErr)
				}
				real, realErr := filepath.EvalSymlinks(abs)
				if realErr != nil {
					t.Fatalf("explicit path %q resolved to %s but does not eval: %v", arg, got.Dir, realErr)
				}
				if got.Dir != real || (insideDir(hub, got.Dir) && !insideDir(hub, real)) {
					t.Fatalf("explicit path %q mode %d resolved into hub %s as %s, want %s", arg, mode, hub, got.Dir, real)
				}
			}()
		}
	})
}

func explicitTarget(arg string, mode TargetMode) bool {
	if mode == TargetPath {
		return true
	}
	if mode != TargetAuto {
		return false
	}
	return explicitPath(arg)
}

func insideDir(root, path string) bool {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
