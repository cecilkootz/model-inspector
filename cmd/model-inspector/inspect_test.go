package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"origin.cursor.com/cecilkootz/model-inspector/internal/fixture"
	"origin.cursor.com/cecilkootz/model-inspector/internal/scan"
)

func TestInspectJSONOnHubFixture(t *testing.T) {
	files, tensorBytes, err := fixture.TinyQwen()
	if err != nil {
		t.Fatal(err)
	}
	const (
		repo   = "Qwen/Tiny-Next"
		commit = "0123456789abcdef0123456789abcdef01234567"
	)
	hub := t.TempDir()
	if _, err := fixture.MaterializeHub(hub, repo, commit, files); err != nil {
		t.Fatal(err)
	}
	out, err := captureStdout(t, func() error {
		return run([]string{"inspect", repo, "--hub-dir", hub, "--json"})
	})
	if err != nil {
		t.Fatal(err)
	}
	var s summary
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out)
	}
	if s.Identity.RepoID != repo || s.Identity.Revision != commit {
		t.Errorf("identity = %+v", s.Identity)
	}
	if s.Identity.Format != "safetensors (mlx)" || s.Identity.SafetensorsShards != 2 {
		t.Errorf("identity = %+v", s.Identity)
	}
	if s.Layers.Count != 2 || s.Layers.WithExperts != 1 {
		t.Errorf("layers = %+v", s.Layers)
	}
	if s.Tensors.Count != 25 || s.Tensors.IndexTotalSize != tensorBytes {
		t.Errorf("tensors = %+v, payload %d", s.Tensors, tensorBytes)
	}
	if s.Scan.Overall != scan.StatusPass {
		t.Errorf("scan = %s", s.Scan.Overall)
	}
	if s.Quantization == nil || s.Quantization.Bits != 4 || s.Quantization.GroupSize != 32 {
		t.Errorf("quant = %+v", s.Quantization)
	}
	if len(s.Warnings) != 0 {
		t.Errorf("warnings = %v", s.Warnings)
	}
	got := map[string]bool{}
	for _, g := range s.Layers.OtherGroups {
		got[g.Name] = true
	}
	for _, name := range []string{"embeddings", "ngram", "norms", "hyper_connection", "lm_head", "mtp", "vision"} {
		if !got[name] {
			t.Errorf("missing other group %s in %+v", name, s.Layers.OtherGroups)
		}
	}
	if got["other"] {
		t.Errorf("mixer tensors fell through to other: %+v", s.Layers.OtherGroups)
	}
}

func TestInspectTextSanitizesFormat(t *testing.T) {
	// JSON \u escapes so the header parses; the decoded format string contains ESC.
	hdr := `{"__metadata__":{"format":"mlx\u001b]0;pwned\u0007"},"w":{"dtype":"F32","shape":[1],"data_offsets":[0,4]}}`
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "model.safetensors"), fixture.EncodeHeader([]byte(hdr), []byte{0, 0, 0, 0}), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := captureStdout(t, func() error {
		return run([]string{"inspect", dir})
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "\x1b") {
		t.Fatalf("text output contains ESC:\n%s", out)
	}
	if !strings.Contains(out, "<U+001B>") {
		t.Fatalf("expected a visible token:\n%s", out)
	}
}

func TestInspectMissingRepo(t *testing.T) {
	_, err := captureStdout(t, func() error {
		return run([]string{"inspect", "Qwen/Missing", "--hub-dir", t.TempDir(), "--json"})
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func captureStdout(t *testing.T, fn func() error) (out string, callErr error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(&buf, r)
	}()
	defer func() {
		_ = w.Close()
		os.Stdout = orig
		<-done
		out = buf.String()
	}()
	callErr = fn()
	return out, callErr
}
