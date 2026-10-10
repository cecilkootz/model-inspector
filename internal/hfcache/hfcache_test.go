package hfcache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveRepoID(t *testing.T) {
	hub := t.TempDir()
	repo := filepath.Join(hub, "models--Org--Model-4bit")
	const commit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
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
	r, err := Resolve("Org/Model-4bit", hub)
	if err != nil {
		t.Fatal(err)
	}
	if r.Dir != snap || r.RepoID != "Org/Model-4bit" || r.Revision != commit {
		t.Errorf("got %+v", r)
	}
	// Explicit snapshot path infers the same identity.
	r2, err := Resolve(snap, hub)
	if err != nil {
		t.Fatal(err)
	}
	if r2.RepoID != "Org/Model-4bit" || r2.Revision != commit || r2.FromCache {
		t.Errorf("path resolve got %+v", r2)
	}
	if _, err := Resolve("Org/Missing", hub); err == nil {
		t.Error("expected error for missing repo")
	}
}

func TestResolveRefPointsAtMissingSnapshot(t *testing.T) {
	hub := t.TempDir()
	repo := filepath.Join(hub, "models--Org--Model-4bit")
	if err := os.MkdirAll(filepath.Join(repo, "refs"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A different snapshot exists; refs/main names one that does not.
	if err := os.MkdirAll(filepath.Join(repo, "snapshots", "other"), 0o755); err != nil {
		t.Fatal(err)
	}
	const missing = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	if err := os.WriteFile(filepath.Join(repo, "refs", "main"), []byte(missing+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Resolve("Org/Model-4bit", hub)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), missing) || !strings.Contains(err.Error(), "not present") {
		t.Fatalf("error = %v", err)
	}
}
