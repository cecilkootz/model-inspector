package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"origin.cursor.com/cecilkootz/model-inspector/internal/fileerr"
	"origin.cursor.com/cecilkootz/model-inspector/internal/fixture"
)

func TestJSONDoesNotRepeatHugeNumber(t *testing.T) {
	const tail = "2222222222"
	digits := append(bytes.Repeat([]byte("1"), 1<<18), []byte(tail)...)
	hdr := append([]byte(`{"x":{"dtype":"U8","shape":[`), digits...)
	hdr = append(hdr, []byte(`],"data_offsets":[0,0]}}`)...)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "model.safetensors"), fixture.EncodeHeader(hdr, nil), 0o644); err != nil {
		t.Fatal(err)
	}
	js, stderr, code := capture(t, func() int { return run([]string{"inspect", "--path", dir, "--json"}) })
	if code != 1 {
		t.Fatalf("exit %d\n%s\n%s", code, stderr, js[:min(len(js), 500)])
	}
	if len(js) > 64<<10 {
		t.Fatalf("json is %d bytes", len(js))
	}
	if strings.Contains(js, tail) || strings.Contains(stderr, tail) {
		t.Fatalf("digit tail leaked into output (%d json bytes)", len(js))
	}
	if !strings.Contains(js, "… (") || !strings.Contains(js, " runes)") {
		t.Fatalf("clipped length missing:\n%s", js)
	}
	// The same parser error is stored on the shard, the load warning, and
	// the header-parse check. Each copy has to stay inside the cap.
	n := strings.Count(js, " runes)")
	if n < 3 {
		t.Fatalf("clipped error appears %d times, want at least 3\n%s", n, js)
	}
	for _, field := range jsonStrings(t, js) {
		if utf8.RuneCountInString(field) > fileerr.MaxRunes+512 {
			t.Fatalf("file-derived string is %d runes", utf8.RuneCountInString(field))
		}
	}
}

func jsonStrings(t *testing.T, js string) []string {
	t.Helper()
	var out []string
	in := false
	esc := false
	var b strings.Builder
	for _, r := range js {
		if !in {
			if r == '"' {
				in = true
				b.Reset()
			}
			continue
		}
		if esc {
			b.WriteRune(r)
			esc = false
			continue
		}
		if r == '\\' {
			esc = true
			b.WriteRune(r)
			continue
		}
		if r == '"' {
			out = append(out, b.String())
			in = false
			continue
		}
		b.WriteRune(r)
	}
	return out
}
