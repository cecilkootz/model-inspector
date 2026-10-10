package main

import (
	"bytes"
	"encoding/json"
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
	var doc any
	if err := json.Unmarshal([]byte(js), &doc); err != nil {
		t.Fatal(err)
	}
	var walk func(any)
	walk = func(v any) {
		switch val := v.(type) {
		case string:
			if strings.Contains(val, " runes)") && utf8.RuneCountInString(val) > fileerr.MaxRunes {
				t.Fatalf("clipped error is %d runes: %q", utf8.RuneCountInString(val), val[:80])
			}
		case map[string]any:
			for _, x := range val {
				walk(x)
			}
		case []any:
			for _, x := range val {
				walk(x)
			}
		}
	}
	walk(doc)
}

func TestLongPathMissingConfigIsComplete(t *testing.T) {
	dir := dirOfLen(t, 253)
	body := fixture.EncodeHeader([]byte(`{"w":{"dtype":"U8","shape":[1],"data_offsets":[0,1]}}`), []byte{0})
	if err := os.WriteFile(filepath.Join(dir, "model.safetensors"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	js, stderr, code := capture(t, func() int { return run([]string{"inspect", "--path", dir, "--json"}) })
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, stderr, js)
	}
	if strings.Contains(js, "inspection is incomplete") || strings.Contains(stderr, "inspection is incomplete") {
		t.Fatalf("missing config on a long path was partial\n%s\n%s", stderr, js)
	}
	var doc struct {
		Partial bool `json:"partial"`
		Scan    struct {
			Overall string `json:"overall"`
		} `json:"scan"`
	}
	if err := json.Unmarshal([]byte(js), &doc); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, js)
	}
	if doc.Partial || doc.Scan.Overall == "fail" {
		t.Fatalf("partial=%v overall=%s\n%s", doc.Partial, doc.Scan.Overall, js)
	}
}

func dirOfLen(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	for len(dir) < n {
		room := n - len(dir) - 1
		if room > 200 {
			room = 200
		}
		if room < 1 {
			t.Fatalf("path length stuck at %d", len(dir))
		}
		dir = filepath.Join(dir, strings.Repeat("d", room))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if len(dir) != n {
		t.Fatalf("dir length %d, want %d", len(dir), n)
	}
	return dir
}
