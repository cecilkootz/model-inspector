package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"origin.cursor.com/cecilkootz/model-inspector/internal/fixture"
	"origin.cursor.com/cecilkootz/model-inspector/internal/scan"
)

func TestQA_PrintTextAndJSON(t *testing.T) {
	hdr := `{"__metadata__":{"format":"\u009b2J\u202e\u001b"},"lm_head\u001b]0;x\u0007.weight":{"dtype":"F32","shape":[1],"data_offsets":[0,4]}}`
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "m\u202e\x1b[2J.safetensors"), fixture.EncodeHeader([]byte(hdr), []byte{0, 0, 0, 0}), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("{\x1b"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := captureStdout(t, func() error { return run([]string{"inspect", dir}) })
	if err != nil {
		t.Fatal(err)
	}
	assertNoHostileRunes(t, "text", out)
	for _, tok := range []string{"<U+001B>", "<U+009B>", "<U+202E>", scan.ChecksImplemented} {
		if !strings.Contains(out, tok) {
			t.Errorf("text missing %s\n%s", tok, out)
		}
	}
	js, err := captureStdout(t, func() error { return run([]string{"inspect", dir, "--json"}) })
	if err != nil {
		t.Fatal(err)
	}
	assertNoHostileRunes(t, "json", js)
	if !strings.Contains(js, `\u009b`) || !strings.Contains(js, `\u202e`) || !strings.Contains(js, `\u001b`) {
		t.Fatalf("json missing \\u escapes:\n%s", js)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(js), &parsed); err != nil {
		t.Fatalf("json: %v\n%s", err, js)
	}
}

func TestQA_StderrAndWalkError(t *testing.T) {
	good, _, err := fixture.WriteShard("", []fixture.Tensor{{Name: "w", DType: "F32", Shape: []int64{1}}})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.safetensors"), good, 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "x\x1b]0;PWNED\x07")
	if err := os.Mkdir(sub, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sub, 0o755) })

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = w
	var errBuf bytes.Buffer
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(&errBuf, r)
	}()
	out, runErr := captureStdout(t, func() error { return run([]string{"inspect", dir}) })
	_ = w.Close()
	os.Stderr = orig
	<-done

	if runErr != nil {
		t.Fatalf("walk error aborted the load: %v", runErr)
	}
	assertNoHostileRunes(t, "stderr", errBuf.String())
	assertNoHostileRunes(t, "stdout", out)
	if !strings.Contains(out, "<U+001B>") || !strings.Contains(out, "<U+0007>") {
		t.Fatalf("stdout did not tokenize the directory name:\n%s", out)
	}
	if !strings.Contains(out, "scan:     fail") {
		t.Fatalf("scan did not fail:\n%s", out)
	}
	if !strings.Contains(out, scan.ChecksImplemented) {
		t.Fatalf("missing checks sentence:\n%s", out)
	}
}

func TestWriteErrorSanitizes(t *testing.T) {
	var buf bytes.Buffer
	writeError(&buf, errors.New("open x\x1b]0;PWNED\x07: permission denied"))
	got := buf.String()
	assertNoHostileRunes(t, "writeError", got)
	if !strings.Contains(got, "<U+001B>") || !strings.Contains(got, "<U+0007>") {
		t.Fatalf("got %q", got)
	}
}

func assertNoHostileRunes(t *testing.T, label, s string) {
	t.Helper()
	for _, r := range s {
		bad := (r < 0x20 && r != '\n') || (r >= 0x7f && r <= 0x9f) || (r >= 0x202a && r <= 0x202e) ||
			(r >= 0x2066 && r <= 0x2069) || r == 0x200e || r == 0x200f || r == 0x061c || r == 0x2028 || r == 0x2029 || r == 0x200b || r == 0xfeff
		if bad {
			t.Fatalf("%s contains raw %U\n%s", label, r, s)
		}
	}
}
