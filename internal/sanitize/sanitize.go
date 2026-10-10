// Package sanitize turns file-derived text into something a terminal can show.
//
// C0, C1, ESC, DEL, bidi overrides, Unicode format characters, and other
// invisible or zero-width characters become visible tokens. A literal "<"
// is tokenized too, so the text "<U+001B>" cannot impersonate an ESC.
// Text is a distinct type so a raw string has to be passed through From
// before it can be rendered.
package sanitize

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxLen is the maximum number of runes kept from sanitized text.
// A value that does not fit may end with one extra ellipsis rune.
// The cap counts runes, not bytes.
const MaxLen = 4096

// Text is untrusted input made safe to print. String returns only the
// sanitized form; the original bytes are not retained.
type Text struct{ s string }

// String returns the sanitized text.
func (t Text) String() string { return t.s }

// From sanitizes s. Printable characters pass through. Controls, bidi
// overrides, format characters, and other invisible characters are replaced
// with <U+XXXX> tokens. '<' is replaced as well so a token cannot be forged.
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
	// C0, DEL, C1, and '<' (so "<U+XXXX>" cannot be typed in by the file).
	if r < 0x20 || r == 0x7F || (r >= 0x80 && r <= 0x9F) || r == '<' {
		return true
	}
	// Line and paragraph separators are Zl/Zp, not Cf.
	if r == 0x2028 || r == 0x2029 {
		return true
	}
	if unicode.Is(unicode.Cf, r) {
		return true
	}
	// Tag characters, including any non-Cf code point in that block.
	if r >= 0xE0000 && r <= 0xE007F {
		return true
	}
	// Variation selectors and variation selectors supplement.
	if (r >= 0xFE00 && r <= 0xFE0F) || (r >= 0xE0100 && r <= 0xE01EF) {
		return true
	}
	switch r {
	case 0x034F, // combining grapheme joiner
		0x115F, 0x1160, // Hangul choseong / jungseong fillers
		0x17B4, 0x17B5, // Khmer inherent vowels
		0x180B, 0x180C, 0x180D, // Mongolian free variation selectors
		0x2800, // Braille blank
		0x3164, // Hangul filler
		0xFFA0: // halfwidth Hangul filler
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

// EscapeJSON rewrites encoded JSON so C1, bidi, format, and other invisible
// characters inside strings are \uXXXX escapes (UTF-16 surrogate pairs above
// U+FFFF). Structural bytes, including the indenter's newlines, are unchanged.
// C0 inside strings is already escaped by encoding/json.
func EscapeJSON(b []byte) []byte {
	if !jsonStringNeedsEscape(b) {
		return b
	}
	var buf bytes.Buffer
	buf.Grow(len(b) + 8)
	inString := false
	escaped := false
	for len(b) > 0 {
		if !inString {
			if b[0] == '"' {
				inString = true
			}
			buf.WriteByte(b[0])
			b = b[1:]
			continue
		}
		if escaped {
			buf.WriteByte(b[0])
			b = b[1:]
			escaped = false
			continue
		}
		switch b[0] {
		case '\\':
			buf.WriteByte(b[0])
			b = b[1:]
			escaped = true
			continue
		case '"':
			buf.WriteByte(b[0])
			b = b[1:]
			inString = false
			continue
		}
		r, size := utf8.DecodeRune(b)
		if r == utf8.RuneError && size == 1 {
			fmt.Fprintf(&buf, `\u%04x`, b[0])
			b = b[1:]
			continue
		}
		if isBad(r) {
			writeUEscape(&buf, r)
		} else {
			buf.Write(b[:size])
		}
		b = b[size:]
	}
	return buf.Bytes()
}

func jsonStringNeedsEscape(b []byte) bool {
	inString := false
	escaped := false
	for len(b) > 0 {
		if !inString {
			if b[0] == '"' {
				inString = true
			}
			b = b[1:]
			continue
		}
		if escaped {
			b = b[1:]
			escaped = false
			continue
		}
		switch b[0] {
		case '\\':
			b = b[1:]
			escaped = true
			continue
		case '"':
			b = b[1:]
			inString = false
			continue
		}
		r, size := utf8.DecodeRune(b)
		if (r == utf8.RuneError && size == 1) || isBad(r) {
			return true
		}
		b = b[size:]
	}
	return false
}

func writeUEscape(buf *bytes.Buffer, r rune) {
	if r < 0 || r > unicode.MaxRune {
		r = utf8.RuneError
	}
	if r <= 0xFFFF {
		fmt.Fprintf(buf, `\u%04x`, uint32(r))
		return
	}
	r -= 0x10000
	hi := 0xD800 + (uint32(r) >> 10)
	lo := 0xDC00 + (uint32(r) & 0x3FF)
	fmt.Fprintf(buf, `\u%04x\u%04x`, hi, lo)
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
