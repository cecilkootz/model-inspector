package hfcache

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveTargetAmbiguousDirectory(t *testing.T) {
	hub := t.TempDir()
	const (
		repo   = "Org/Model-4bit"
		commit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	)
	snap := filepath.Join(hub, RepoFolderName(repo), "snapshots", commit)
	if err := os.MkdirAll(snap, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(hub, RepoFolderName(repo), "refs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hub, RepoFolderName(repo), "refs", "main"), []byte(commit+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cwd := t.TempDir()
	t.Chdir(cwd)
	if err := os.MkdirAll(filepath.Join(cwd, "Org", "Model-4bit"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := ResolveTarget(repo, hub, TargetAuto)
	if !errors.Is(err, ErrAmbiguousTarget) {
		t.Fatalf("auto = %v", err)
	}

	got, err := ResolveTarget(repo, hub, TargetRepo)
	if err != nil {
		t.Fatal(err)
	}
	if !got.FromCache || got.Revision != commit {
		t.Fatalf("repo = %+v", got)
	}

	local, err := ResolveTarget(repo, hub, TargetPath)
	if err != nil {
		t.Fatal(err)
	}
	if local.FromCache || local.Dir == snap {
		t.Fatalf("path resolved to the hub: %+v", local)
	}

	// An explicit ./ prefix is a path even when the name matches a repo id.
	dotted, err := ResolveTarget("./"+repo, hub, TargetAuto)
	if err != nil {
		t.Fatal(err)
	}
	if dotted.Dir != local.Dir {
		t.Fatalf("./ path = %+v, local = %+v", dotted, local)
	}
}

func TestResolveTargetMissingLocalDoesNotBecomeRepo(t *testing.T) {
	t.Chdir(t.TempDir())
	_, err := ResolveTarget("./Org/Missing", t.TempDir(), TargetAuto)
	if err == nil || errors.Is(err, ErrAmbiguousTarget) {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveTargetBareRepoWithoutDirectory(t *testing.T) {
	hub := t.TempDir()
	const (
		repo   = "Org/Only-Hub"
		commit = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	)
	snap := filepath.Join(hub, RepoFolderName(repo), "snapshots", commit)
	if err := os.MkdirAll(filepath.Join(hub, RepoFolderName(repo), "refs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(snap, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hub, RepoFolderName(repo), "refs", "main"), []byte(commit+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	got, err := ResolveTarget(repo, hub, TargetAuto)
	if err != nil {
		t.Fatal(err)
	}
	if !got.FromCache || got.Dir != snap {
		t.Fatalf("got %+v", got)
	}
}
