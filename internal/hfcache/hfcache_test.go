package hfcache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"origin.cursor.com/cecilkootz/model-inspector/internal/sanitize"
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

func TestLongPathMissingRef(t *testing.T) {
	hub := t.TempDir()
	const want = 220
	for len(hub) < want {
		room := want - len(hub) - 1
		if room > 180 {
			room = 180
		}
		if room < 1 {
			t.Fatalf("hub path stuck at %d", len(hub))
		}
		hub = filepath.Join(hub, strings.Repeat("h", room))
	}
	if err := os.MkdirAll(hub, 0o755); err != nil {
		t.Fatal(err)
	}
	if len(hub) != want {
		t.Fatalf("hub length %d, want %d", len(hub), want)
	}
	repo := filepath.Join(hub, "models--Org--Name")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	ref := filepath.Join(repo, "refs", "main")
	if utf8.RuneCountInString(ref) < 240 {
		t.Fatalf("ref path is only %d runes (%s); the not-exist check would not be clipped", utf8.RuneCountInString(ref), ref)
	}
	_, err := Resolve("Org/Name", hub)
	if err == nil || !strings.Contains(err.Error(), "no refs/main") {
		t.Fatalf("long missing ref: %v", err)
	}
	if strings.Contains(err.Error(), "U+003C") || !strings.Contains(err.Error(), "org/name@<commit>") {
		t.Fatalf("static placeholder was sanitized: %v", err)
	}
}

func TestResolveMissingMainRefKeepsCommitHint(t *testing.T) {
	hub := t.TempDir()
	repo := filepath.Join(hub, "models--Org--Name")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Resolve("Org/Name", hub)
	if err == nil {
		t.Fatal("expected error")
	}
	got := err.Error()
	if !strings.Contains(got, "org/name@<commit>") {
		t.Fatalf("hint: %q", got)
	}
	if strings.Contains(got, "U+003C") {
		t.Fatalf("static placeholder was sanitized: %q", got)
	}
	if _, ok := err.(sanitize.Note); !ok {
		t.Fatalf("type %T", err)
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
