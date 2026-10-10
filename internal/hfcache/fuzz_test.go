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
	f.Fuzz(func(t *testing.T, arg string) {
		if len(arg) > 512 {
			return
		}
		hub := t.TempDir()
		const commit = "0123456789abcdef0123456789abcdef01234567"
		repo := filepath.Join(hub, "models--Org--Model")
		snap := filepath.Join(repo, "snapshots", commit)
		if err := os.MkdirAll(snap, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(repo, "refs"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, "refs", "main"), []byte(commit+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		r, err := Resolve(arg, hub)
		if err != nil || !r.FromCache {
			return
		}
		root := filepath.Join(repo, "snapshots")
		realRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			t.Fatal(err)
		}
		if !safeopen.Within(realRoot, r.Dir) {
			t.Fatalf("resolved %q outside %q", r.Dir, realRoot)
		}
		if !commitRe.MatchString(r.Revision) {
			t.Fatalf("revision %q", r.Revision)
		}
	})
}
