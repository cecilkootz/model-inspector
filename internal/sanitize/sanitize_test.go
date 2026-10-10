package sanitize

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestFromReplacesControlsBidiAndZeroWidth(t *testing.T) {
	in := "ok\x1b]0;pwned\a\u009b\u007f\u202eHID\u200b"
	got := From(in).String()
	for _, r := range got {
		if r < 0x20 || r == 0x7F || (r >= 0x80 && r <= 0x9F) {
			t.Fatalf("raw control %#U in %q", r, got)
		}
	}
	for _, tok := range []string{"<U+001B>", "<U+0007>", "<U+009B>", "<U+007F>", "<U+202E>", "<U+200B>"} {
		if !strings.Contains(got, tok) {
			t.Errorf("missing %s in %q", tok, got)
		}
	}
	if strings.Contains(got, "\x1b") || strings.ContainsRune(got, '\u202e') {
		t.Fatalf("raw sequence survived: %q", got)
	}
}

func TestFromLeavesPlainText(t *testing.T) {
	const s = "model.layers.0.mlp.weight"
	if got := From(s).String(); got != s {
		t.Fatalf("got %q", got)
	}
}

func TestFromCapsLength(t *testing.T) {
	got := From(strings.Repeat("a", MaxLen+100)).String()
	if utf8.RuneCountInString(got) > MaxLen+1 {
		t.Fatalf("len %d", utf8.RuneCountInString(got))
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("got %q", got[len(got)-8:])
	}
}

func TestJSONSanitizesStringsOnly(t *testing.T) {
	pretty, err := JSON(map[string]any{
		"mode": "\x1b[2J",
		"n":    float64(4),
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(pretty, "\x1b") {
		t.Fatalf("raw ESC in %s", pretty)
	}
	if !strings.Contains(pretty, "<U+001B>") || !strings.Contains(pretty, "\n") {
		t.Fatalf("pretty = %s", pretty)
	}
}
