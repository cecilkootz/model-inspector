package safeopen

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

func TestReadAllExactAndOverCap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, bytes.Repeat([]byte("a"), 32), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadAll(path, 32)
	if err != nil || len(got) != 32 {
		t.Fatalf("exact: %d bytes, err %v", len(got), err)
	}
	got, err = ReadAll(path, 31)
	if err == nil || got != nil {
		t.Fatalf("over cap returned buf=%d err=%v", len(got), err)
	}
}

func TestOpenRejectsDevice(t *testing.T) {
	if _, err := os.Stat("/dev/zero"); err != nil {
		t.Skip("/dev/zero not available")
	}
	start := time.Now()
	_, err := Open("/dev/zero", 64)
	if err == nil {
		t.Fatal("expected /dev/zero to be rejected")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("open /dev/zero took %s", time.Since(start))
	}
}

func TestOpenRejectsFIFO(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fifo")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "pipe")
	if err := syscall.Mkfifo(path, 0o644); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	start := time.Now()
	_, err := Open(path, 64)
	if err == nil {
		t.Fatal("expected fifo to be rejected")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("open fifo took %s", time.Since(start))
	}
}

func TestOpenUnderRejectsEscapingSymlink(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(root, "outside")
	inside := filepath.Join(root, "model")
	if err := os.Mkdir(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(inside, "config.json")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenUnder(link, 1024, inside); err == nil {
		t.Fatal("symlink out of the model directory was accepted")
	}
	got, err := ReadAll(link, 1024)
	if err != nil || string(got) != "secret" {
		t.Fatalf("unconstrained read = %q, %v", got, err)
	}
}
