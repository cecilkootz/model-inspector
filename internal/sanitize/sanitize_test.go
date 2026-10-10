package sanitize

import (
	"bytes"
	"encoding/json"
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
	if n := utf8.RuneCountInString(got); n > MaxLen+1 {
		t.Fatalf("runes %d", n)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("got %q", got[len(got)-8:])
	}
}

func TestFromCapsRunesNotBytes(t *testing.T) {
	got := From(strings.Repeat("é", MaxLen+5)).String()
	n := utf8.RuneCountInString(got)
	if !utf8.ValidString(got) || n > MaxLen+1 || n < MaxLen {
		t.Fatalf("runes=%d bytes=%d valid=%v", n, len(got), utf8.ValidString(got))
	}
	// é is two bytes. A 4096-byte cap would keep about half as many runes.
	if n < MaxLen-1 {
		t.Fatalf("cap looks like bytes: runes=%d", n)
	}
}

func TestTokenIsNotForgeable(t *testing.T) {
	esc := From("\x1b").String()
	lit := From("<U+001B>").String()
	if esc == lit {
		t.Fatalf("literal token matches sanitized ESC: %q", esc)
	}
	if esc != "<U+001B>" {
		t.Fatalf("ESC token = %q", esc)
	}
	if strings.Contains(lit, "<U+001B>") {
		t.Fatalf("forged token still contains the ESC token: %q", lit)
	}
}

func TestInvisibleRunesAreTokens(t *testing.T) {
	samples := []rune{
		0x00AD, 0x034F, 0x061C, 0x115F, 0x1160, 0x17B4, 0x17B5,
		0x180B, 0x180C, 0x180D, 0x180E, 0x200B, 0x200E, 0x200F,
		0x2028, 0x2029, 0x202A, 0x202E, 0x2066, 0x2067, 0x2068, 0x2069,
		0x2800, 0x3164, 0xFE00, 0xFE0F, 0xFFA0, 0xFEFF,
		0x1D173, 0x1D17A, 0xE0001, 0xE0041, 0xE007F,
	}
	for _, r := range samples {
		in := "a" + string(r) + "b"
		got := From(in).String()
		if strings.ContainsRune(got, r) || got == in {
			t.Errorf("%U passed through: %q", r, got)
		}
	}
}

func TestEscapeJSONUsesUEscapes(t *testing.T) {
	in := map[string]string{"s": "a\u009b\u202e\U000E0041b"}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	out := EscapeJSON(raw)
	text := string(out)
	for _, r := range []rune{0x9b, 0x202e, 0xE0041} {
		if strings.ContainsRune(text, r) {
			t.Fatalf("raw %U in %s", r, text)
		}
	}
	if !strings.Contains(text, `\u009b`) || !strings.Contains(text, `\u202e`) || !strings.Contains(text, `\udb40\udc41`) {
		t.Fatalf("missing escapes: %s", text)
	}
	var back map[string]string
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatal(err)
	}
	if back["s"] != in["s"] {
		t.Fatalf("round trip %q", back["s"])
	}
	if !bytes.Equal(EscapeJSON([]byte(`{"ok":true}`)), []byte(`{"ok":true}`)) {
		t.Fatal("clean JSON was rewritten")
	}
	indented := []byte("{\n  \"ok\": true\n}\n")
	if !bytes.Equal(EscapeJSON(indented), indented) {
		t.Fatalf("indented JSON was rewritten: %q", EscapeJSON(indented))
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
