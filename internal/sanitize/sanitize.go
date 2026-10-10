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
	"io"
	"reflect"
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

// Program is text this program wrote: a label, a hint, a count. It is stored
// unchanged, so a placeholder such as "<commit>" or "<1%" stays a less-than.
// Model, file, and user bytes use From.
func Program(s string) Text { return Text{s: s} }

// Note is an error safe to print. Notef leaves the format string alone and
// sanitizes the arguments. Callers must not pass the result through From
// again, or the "<" in the format becomes a token.
type Note struct{ msg string }

// Error returns the formatted sentence.
func (n Note) Error() string { return n.msg }

// formatLit is unexported so a caller can pass only an untyped string
// constant. A string variable does not convert to formatLit, which keeps a
// non-constant format from compiling. Inside this package an explicit
// conversion still compiles; the AST test rejects that.
//
// go:linkname, a generic call-through, reflect, and unsafe can reach Notef
// without converting to formatLit. The AST test catches a generic
// call-through, reflect, and unsafe, because each names Notef as a value.
// It does not see a go:linkname directive, so the same test fails if one
// targets this package.
type formatLit string

// Notef formats a program sentence. The format string is trusted and must be
// a constant literal. Arguments that implement fmt.Stringer, error,
// fmt.Formatter, or fmt.GoStringer are sanitized even when the underlying
// kind is numeric or bool, because fmt will call the method and print its
// text. Other numbers and bools stay raw so verbs such as %d keep working.
// %c is untrusted even for a number: the character is sanitized. A width or
// precision star stays a raw integer, which is what fmt requires.
// Text, *Text, and []Text, including Program, are already safe and are not
// passed through From again. A nil argument is left for fmt, which prints
// "<nil>". Nil is decided from the value, never from the formatted text.
func Notef(format formatLit, args ...any) error {
	spec := string(format)
	roles := scanFormat(spec)
	safe := make([]any, len(args))
	for i, a := range args {
		safe[i] = safeArg(a, roles.star[i], roles.char[i])
	}
	return Note{msg: fmt.Sprintf(spec, safe...)}
}

func safeArg(a any, star, char bool) any {
	switch {
	case star:
		return a
	case isText(a):
		return a
	case formatsItself(a):
		return sanitized{a}
	case isNilLike(a):
		return a
	case char || !isNumberOrBool(a):
		return sanitized{a}
	default:
		return a
	}
}

func isText(a any) bool {
	switch a.(type) {
	case Text, *Text, []Text:
		return true
	default:
		return false
	}
}

// formatsItself is true when fmt will print a method's text instead of the
// underlying number or bool.
func formatsItself(a any) bool {
	switch a.(type) {
	case fmt.Stringer, error, fmt.Formatter, fmt.GoStringer:
		return true
	default:
		return false
	}
}

// isNilLike reports a nil value: the interface itself, or a nil pointer,
// interface, map, slice, chan, or func. The formatted text is irrelevant.
// A typed nil that implements Stringer or error is handled earlier, so its
// method text is still sanitized.
func isNilLike(a any) bool {
	if a == nil {
		return true
	}
	v := reflect.ValueOf(a)
	switch v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.Interface:
		return v.IsNil()
	default:
		return false
	}
}

// sanitized prints v with the verb fmt selected, then tokenizes that text.
type sanitized struct{ v any }

func (s sanitized) Format(f fmt.State, verb rune) {
	formatted := fmt.Sprintf(fmt.FormatString(f, verb), s.v)
	_, _ = io.WriteString(f, From(formatted).String())
}

func isNumberOrBool(a any) bool {
	switch a.(type) {
	case bool,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64, uintptr,
		float32, float64,
		complex64, complex128:
		return true
	}
	if a == nil {
		return false
	}
	switch reflect.TypeOf(a).Kind() {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64,
		reflect.Complex64, reflect.Complex128:
		return true
	default:
		return false
	}
}

// formatRoles records which argument indexes are %c values and which are
// width or precision stars. Stars must stay integers for fmt.
type formatRoles struct {
	char map[int]bool
	star map[int]bool
}

func scanFormat(format string) formatRoles {
	var r formatRoles
	r.char = map[int]bool{}
	r.star = map[int]bool{}
	argNum := 0
	end := len(format)
	for i := 0; i < end; {
		if format[i] != '%' {
			i++
			continue
		}
		i++
		if i >= end {
			break
		}
		if format[i] == '%' {
			i++
			continue
		}
		for i < end {
			switch format[i] {
			case '#', '0', '+', '-', ' ':
				i++
				continue
			}
			break
		}
		if n, ni, ok := argIndex(format, i); ok {
			argNum = n
			i = ni
		}
		if i < end && format[i] == '*' {
			r.star[argNum] = true
			argNum++
			i++
		} else {
			for i < end && format[i] >= '0' && format[i] <= '9' {
				i++
			}
		}
		if i < end && format[i] == '.' {
			i++
			if n, ni, ok := argIndex(format, i); ok {
				argNum = n
				i = ni
			}
			if i < end && format[i] == '*' {
				r.star[argNum] = true
				argNum++
				i++
			} else {
				for i < end && format[i] >= '0' && format[i] <= '9' {
					i++
				}
			}
		}
		if n, ni, ok := argIndex(format, i); ok {
			argNum = n
			i = ni
		}
		if i >= end {
			break
		}
		verb, size := utf8.DecodeRuneInString(format[i:])
		i += size
		if verb == '%' {
			continue
		}
		if verb == 'c' {
			r.char[argNum] = true
		}
		argNum++
	}
	return r
}

// argIndex parses a leading [n] argument index. n is 1-based in the format
// and 0-based in the result, matching fmt.
func argIndex(format string, i int) (n, newi int, ok bool) {
	if i >= len(format) || format[i] != '[' {
		return 0, i, false
	}
	j := i + 1
	if j >= len(format) || format[j] < '0' || format[j] > '9' {
		return 0, i, false
	}
	num := 0
	for j < len(format) && format[j] >= '0' && format[j] <= '9' {
		num = num*10 + int(format[j]-'0')
		j++
	}
	if j >= len(format) || format[j] != ']' || num < 1 {
		return 0, i, false
	}
	return num - 1, j + 1, true
}

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
