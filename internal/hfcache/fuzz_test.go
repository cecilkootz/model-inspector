package hfcache

import (
	"os"
	"path/filepath"
	"testing"

	"origin.cursor.com/cecilkootz/model-inspector/internal/safeopen"
)

func FuzzResolve(f *testing.F) {
	f.Add("Org/Model")
	f.Add("Org/Model@main")
	f.Add("Org/Model@0123456789abcdef0123456789abcdef01234567")
	f.Add("Org/Model@../../../../tmp")
	f.Add("Org/Model@..%2F..%2Ftmp")
	// The hub is built once per process. Creating it inside the fuzz
	// function did a TempDir, two MkdirAll, and a WriteFile on every input.
	// The minimizer then ran that for every subset of a new string and
	// stopped publishing execs, so the target sat at 0/s after a few
	// thousand calls. The snapshot is read-only; Resolve only reads it.
	hub, err := os.MkdirTemp("", "mi-fuzz-resolve-")
	if err != nil {
		f.Fatal(err)
	}
	f.Cleanup(func() { _ = os.RemoveAll(hub) })
	const commit = "0123456789abcdef0123456789abcdef01234567"
	repo := filepath.Join(hub, "models--Org--Model")
	snap := filepath.Join(repo, "snapshots", commit)
	if err := os.MkdirAll(snap, 0o755); err != nil {
		f.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "refs"), 0o755); err != nil {
		f.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "refs", "main"), []byte(commit+"\n"), 0o644); err != nil {
		f.Fatal(err)
	}
	root := filepath.Join(repo, "snapshots")
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, arg string) {
		// Strings past this length are not looked up. The bound matches the
		// previous target, so every string that used to call Resolve still
		// does, including the containment checks on a hub hit.
		if len(arg) > 512 {
			return
		}
		r, err := Resolve(arg, hub)
		if err != nil || !r.FromCache {
			return
		}
		if !safeopen.Within(realRoot, r.Dir) {
			t.Fatalf("resolved %q outside %q", r.Dir, realRoot)
		}
		if !commitRe.MatchString(r.Revision) {
			t.Fatalf("revision %q", r.Revision)
		}
	})
}
