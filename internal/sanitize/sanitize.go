// Package sanitize turns file-derived text into something a terminal can show.
//
// C0, C1, ESC, DEL, bidi overrides, and invisible or zero-width characters
// become visible tokens. Text is a distinct type so a raw string has to be
// passed through From before it can be rendered.
package sanitize

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// MaxLen is the maximum number of runes From emits before truncating.
const MaxLen = 4096

// Text is untrusted input made safe to print. String returns only the
// sanitized form; the original bytes are not retained.
type Text struct{ s string }

// String returns the sanitized text.
func (t Text) String() string { return t.s }

// From sanitizes s. Printable characters pass through. Controls, bidi
// overrides, and zero-width characters are replaced with <U+XXXX> tokens.
func From(s string) Text {
	var b strings.Builder
	if len(s) < MaxLen {
		b.Grow(len(s))
	} else {
		b.Grow(MaxLen)
	}
	n := 0
	for _, r := range s {
		if n >= MaxLen {
			b.WriteString("…")
			break
		}
		if !isBad(r) {
			b.WriteRune(r)
			n++
			continue
		}
		tok := token(r)
		tokRunes := utf8.RuneCountInString(tok)
		if n+tokRunes > MaxLen {
			b.WriteString("…")
			break
		}
		b.WriteString(tok)
		n += tokRunes
	}
	return Text{s: b.String()}
}

func isBad(r rune) bool {
	if r < 0x20 || r == 0x7F || (r >= 0x80 && r <= 0x9F) {
		return true
	}
	switch r {
	case 0x00AD, // soft hyphen
		0x061C, // arabic letter mark
		0x180E, // mongolian vowel separator
		0x200B, 0x200C, 0x200D, 0x200E, 0x200F,
		0x202A, 0x202B, 0x202C, 0x202D, 0x202E,
		0x2028, 0x2029, // line and paragraph separators
		0x2060, 0x2061, 0x2062, 0x2063, 0x2064,
		0x2066, 0x2067, 0x2068, 0x2069,
		0x206A, 0x206B, 0x206C, 0x206D, 0x206E, 0x206F,
		0xFEFF,
		0xFFF9, 0xFFFA, 0xFFFB:
		return true
	default:
		return false
	}
}

func token(r rune) string {
	return "<U+" + upperHex(uint32(r)) + ">"
}

func upperHex(v uint32) string {
	const digits = "0123456789ABCDEF"
	var buf [6]byte
	i := len(buf)
	for {
		i--
		buf[i] = digits[v&0xF]
		v >>= 4
		if v == 0 && len(buf)-i >= 4 {
			break
		}
	}
	return string(buf[i:])
}

// Value returns a deep copy of a JSON-like value with every string passed
// through From. Maps and slices are copied; the input is left unchanged.
func Value(v any) any {
	switch t := v.(type) {
	case string:
		return From(t).String()
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[From(k).String()] = Value(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = Value(val)
		}
		return out
	default:
		return v
	}
}

// JSON pretty-prints v after sanitizing every string. Structural newlines
// added by the indenter are preserved; hostile characters inside strings are not.
func JSON(v any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(Value(v)); err != nil {
		return "", err
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}
