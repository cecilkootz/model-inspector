// Package fileerr caps error text that was copied out of an untrusted file.
//
// encoding/json quotes the offending literal in full. A multi-megabyte
// number in a shape or a metadata value then shows up again in every
// warning and scan detail that wraps the same error. String keeps a short
// prefix, an ellipsis, and the original rune count.
package fileerr

import (
	"errors"
	"fmt"
	"unicode/utf8"
)

// MaxRunes is the longest Error string returned after a clip, including
// the ellipsis and the original-length suffix.
const MaxRunes = 256

// String returns s unchanged when it fits in MaxRunes. Otherwise it keeps
// a prefix of s and appends "… (N runes)", and the result is exactly
// MaxRunes runes. A second call is a no-op because the result already fits.
func String(s string) string {
	n := utf8.RuneCountInString(s)
	if n <= MaxRunes {
		return s
	}
	suffix := fmt.Sprintf("… (%d runes)", n)
	keep := MaxRunes - utf8.RuneCountInString(suffix)
	if keep < 0 {
		keep = 0
	}
	i := 0
	for r := 0; r < keep && i < len(s); r++ {
		_, sz := utf8.DecodeRuneInString(s[i:])
		if sz <= 0 {
			break
		}
		i += sz
	}
	return s[:i] + suffix
}

// Error returns err unchanged when its text fits in MaxRunes.
// A longer text is replaced with String(err.Error()). The original error
// is not wrapped: its Error text is what was oversized, and retaining it
// would keep that text alive for every copy of the clipped value.
func Error(err error) error {
	if err == nil {
		return nil
	}
	s := err.Error()
	c := String(s)
	if c == s {
		return err
	}
	return errors.New(c)
}
