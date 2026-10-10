package hfcache

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRefsDotDotRejected(t *testing.T) {
	hub := t.TempDir()
	repo := filepath.Join(hub, "models--Org--M")
	if err := os.MkdirAll(filepath.Join(repo, "snapshots"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "refs"), 0o755); err != nil {
		t.Fatal(err)
	}
	escaped := filepath.Join(hub, "escaped-target")
	if err := os.MkdirAll(escaped, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "refs", "main"), []byte("../../../escaped-target\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := Resolve("Org/M", hub)
	if err == nil {
		t.Fatalf("accepted traversal ref: %+v", r)
	}
	if strings.Contains(r.Dir, "escaped-target") {
		t.Fatalf("dir %q", r.Dir)
	}
}

func TestRevArgDotDotRejected(t *testing.T) {
	hub := t.TempDir()
	if err := os.MkdirAll(filepath.Join(hub, "models--Org--M", "snapshots"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(hub, "target"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, arg := range []string{"Org/M@../../target", "Org/M@../../../../tmp", "Org/M@foo/../../target"} {
		r, err := Resolve(arg, hub)
		if err == nil {
			t.Fatalf("%s accepted: %+v", arg, r)
		}
		if strings.Contains(r.Dir, "target") || strings.Contains(r.Dir, "tmp") {
			t.Fatalf("%s dir %q", arg, r.Dir)
		}
	}
}

func TestRefDeviceRejected(t *testing.T) {
	if _, err := os.Stat("/dev/zero"); err != nil {
		t.Skip("/dev/zero not available")
	}
	hub := t.TempDir()
	repo := filepath.Join(hub, "models--Org--M")
	if err := os.MkdirAll(filepath.Join(repo, "refs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "snapshots"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/dev/zero", filepath.Join(repo, "refs", "main")); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err := Resolve("Org/M", hub)
	if err == nil {
		t.Fatal("accepted /dev/zero ref")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("resolve took %s", time.Since(start))
	}
}

func TestRefSymlinkOutsideRepoRejected(t *testing.T) {
	hub := t.TempDir()
	repo := filepath.Join(hub, "models--Org--M")
	const commit = "cccccccccccccccccccccccccccccccccccccccc"
	if err := os.MkdirAll(filepath.Join(repo, "snapshots", commit), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "refs"), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(hub, "commit.txt")
	if err := os.WriteFile(outside, []byte(commit+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repo, "refs", "main")); err != nil {
		t.Fatal(err)
	}
	r, err := Resolve("Org/M", hub)
	if err == nil {
		t.Fatalf("followed a ref symlink outside refs/: %+v", r)
	}
}

func TestRefFIFORejected(t *testing.T) {
	hub := t.TempDir()
	repo := filepath.Join(hub, "models--Org--M")
	if err := os.MkdirAll(filepath.Join(repo, "refs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "snapshots"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(repo, "refs", "main"), 0o644); err != nil {
		t.Skip(err)
	}
	start := time.Now()
	r, err := Resolve("Org/M", hub)
	if err == nil {
		t.Fatalf("resolved a FIFO ref: %+v", r)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("resolve took %s", time.Since(start))
	}
}

func TestCommitMustStayUnderSnapshots(t *testing.T) {
	hub := t.TempDir()
	repo := filepath.Join(hub, "models--Org--M")
	const commit = "0123456789abcdef0123456789abcdef01234567"
	snap := filepath.Join(repo, "snapshots", commit)
	if err := os.MkdirAll(snap, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "refs"), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(hub, "elsewhere")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(snap); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, snap); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "refs", "main"), []byte(commit+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := Resolve("Org/M", hub)
	if err == nil {
		t.Fatalf("snapshot symlink escaped: %+v", r)
	}
}
