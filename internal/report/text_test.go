package report

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/cecilkootz/model-inspector/internal/fixture"
	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/scan"
)

func TestTextSummary(t *testing.T) {
	files, _, err := fixture.TinyQwen()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := fixture.Materialize(dir, files); err != nil {
		t.Fatal(err)
	}
	m, err := model.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := Text(Build(m, scan.Run(m), false, 0))
	if strings.Contains(out, "\x1b") {
		t.Fatalf("text contains ESC:\n%s", out)
	}
	if strings.Contains(out, " @ ") {
		t.Fatalf("empty revision rendered with @:\n%s", out)
	}
	assertTextUTF8(t, out)
	for _, want := range []string{"safetensors", "revision", "—", "checks", "behavior scan"} {
		if !strings.Contains(out, want) {
			t.Errorf("text missing %q\n%s", want, out)
		}
	}
}

func TestWrapKeepsRunesAndTokens(t *testing.T) {
	var b strings.Builder
	// 61 ASCII columns leave the following <U+009B> token (8 columns)
	// past the row width, so a byte wrap would split it or the Û run.
	raw := strings.Repeat("Û", 40) + strings.Repeat("A", 61) + "\u009b" + strings.Repeat("é", 20)
	writeWrapped(&b, "warnings", raw)
	out := b.String()
	assertTextUTF8(t, out)
	if !strings.Contains(out, "<U+009B>") {
		t.Fatalf("sanitizer token missing:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "<U+") && !strings.Contains(line, ">") {
			t.Fatalf("token split across a wrap: %q", line)
		}
		if len(line) > 0 && line[0] >= 0x80 && line[0] < 0xC0 {
			t.Fatalf("line starts with a continuation byte: %q", line)
		}
	}
}

func assertTextUTF8(t *testing.T, out string) {
	t.Helper()
	if !utf8.ValidString(out) {
		t.Fatalf("text is not valid UTF-8")
	}
	for i, line := range strings.Split(out, "\n") {
		if r, bad := strayC1(line); bad {
			t.Fatalf("line %d has stray C1 %U: %q", i, r, line)
		}
	}
}

// strayC1 reports a C1 control or an undecoded 0x80-0x9F byte.
// A continuation byte inside a valid sequence (Û is C3 9B) is not a C1 control.
func strayC1(line string) (rune, bool) {
	if !utf8.ValidString(line) {
		return utf8.RuneError, true
	}
	for i := 0; i < len(line); {
		r, size := utf8.DecodeRuneInString(line[i:])
		if size == 1 && line[i] >= 0x80 {
			return rune(line[i]), true
		}
		if r >= 0x80 && r <= 0x9F {
			return r, true
		}
		if size < 1 {
			return utf8.RuneError, true
		}
		i += size
	}
	return 0, false
}
