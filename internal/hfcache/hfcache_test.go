package hfcache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveRepoID(t *testing.T) {
	hub := t.TempDir()
	repo := filepath.Join(hub, "models--Org--Model-4bit")
	snap := filepath.Join(repo, "snapshots", "abc123")
	if err := os.MkdirAll(snap, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "refs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "refs", "main"), []byte("abc123\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := Resolve("Org/Model-4bit", hub)
	if err != nil {
		t.Fatal(err)
	}
	if r.Dir != snap || r.RepoID != "Org/Model-4bit" || r.Revision != "abc123" {
		t.Errorf("got %+v", r)
	}
	// Explicit snapshot path infers the same identity.
	r2, err := Resolve(snap, hub)
	if err != nil {
		t.Fatal(err)
	}
	if r2.RepoID != "Org/Model-4bit" || r2.Revision != "abc123" || r2.FromCache {
		t.Errorf("path resolve got %+v", r2)
	}
	if _, err := Resolve("Org/Missing", hub); err == nil {
		t.Error("expected error for missing repo")
	}
}
