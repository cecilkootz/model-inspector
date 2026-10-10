// Package fileerr caps error text that was copied out of an untrusted file.
//
// encoding/json quotes the offending literal in full. A multi-megabyte
// number in a shape or a metadata value then shows up again in every
// warning and scan detail that wraps the same error. String keeps a short
// prefix on grapheme boundaries, an ellipsis, and the original rune count.
// Long paths are shortened in the middle so the head and the filename both
// stay visible. Present sanitizes before that clip, so the text a person
// actually sees stays inside the same cap when controls expand into tokens.
//
// Error keeps that text but still unwraps to the original error, so
// errors.Is can see fs.ErrNotExist through a clipped path error.
package fileerr

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/rivo/uniseg"

	"origin.cursor.com/cecilkootz/model-inspector/internal/sanitize"
)

// MaxRunes is the longest Error string returned after a clip, including
// the ellipsis and the original-length suffix.
const MaxRunes = 256

// String returns s unchanged when it fits in MaxRunes. Otherwise it elides
// long paths in the middle and, if the text is still longer than MaxRunes,
// keeps a prefix plus "… (N runes)". N is the rune count of the original s.
// The result is at most MaxRunes runes and never splits a grapheme cluster.
// String does not tokenize controls; Present does that before clipping so
// the visible text stays inside MaxRunes as well.
func String(s string) string {
	if utf8.RuneCountInString(s) <= MaxRunes {
		return s
	}
	return clipVisible(s, utf8.RuneCountInString(s))
}

// Present sanitizes s, then clips the visible text to MaxRunes. Controls
// become <U+XXXX> tokens before the cut, so an escape-heavy string cannot
// grow past the cap, and a token or a combining mark is not split.
func Present(s string) string {
	vis := sanitize.From(s).String()
	if utf8.RuneCountInString(vis) <= MaxRunes && vis == s {
		return s
	}
	return clipVisible(vis, utf8.RuneCountInString(s))
}

func clipVisible(s string, originalRunes int) string {
	s = elideLongPaths(s)
	if utf8.RuneCountInString(s) <= MaxRunes {
		return s
	}
	return clipAtoms(s, originalRunes)
}

// Error returns err unchanged when its text already fits in MaxRunes.
// A longer text is replaced with String(err.Error()).
// The result unwraps to err, so errors.Is and errors.As still see the
// cause. A second call is a no-op.
func Error(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(*clipped); ok {
		return err
	}
	msg := String(err.Error())
	if msg == err.Error() {
		return err
	}
	return &clipped{text: msg, err: err}
}

type clipped struct {
	text string
	err  error
}

func (c *clipped) Error() string { return c.text }

func (c *clipped) Unwrap() error { return c.err }

func clipAtoms(s string, originalRunes int) string {
	if utf8.RuneCountInString(s) <= MaxRunes {
		return s
	}
	suffix := fmt.Sprintf("… (%d runes)", originalRunes)
	keep := MaxRunes - utf8.RuneCountInString(suffix)
	if keep < 0 {
		keep = 0
	}
	return takeAtoms(s, keep) + suffix
}

// takeAtoms returns the longest prefix of s that is at most maxRunes runes
// and ends on a grapheme or sanitizer-token boundary.
func takeAtoms(s string, maxRunes int) string {
	if maxRunes <= 0 || s == "" {
		return ""
	}
	state := -1
	n := 0
	i := 0
	for i < len(s) && n < maxRunes {
		if tn := tokenLen(s[i:]); tn > 0 {
			tr := utf8.RuneCountInString(s[i : i+tn])
			if n+tr > maxRunes {
				break
			}
			n += tr
			i += tn
			state = -1
			continue
		}
		g, _, _, st := uniseg.FirstGraphemeClusterInString(s[i:], state)
		if g == "" {
			break
		}
		gr := utf8.RuneCountInString(g)
		if n+gr > maxRunes {
			break
		}
		n += gr
		i += len(g)
		state = st
	}
	return s[:i]
}

// tokenLen is the byte length of a leading <U+XXXX> sanitizer token, or 0.
func tokenLen(s string) int {
	if !strings.HasPrefix(s, "<U+") {
		return 0
	}
	i := strings.IndexByte(s, '>')
	if i < 7 || i > 9 {
		return 0
	}
	for _, c := range s[3:i] {
		if (c < '0' || c > '9') && (c < 'A' || c > 'F') {
			return 0
		}
	}
	return i + 1
}

// elideLongPaths shortens absolute paths inside s so the whole string can
// fit in MaxRunes. The head of the path and the final filename are kept.
func elideLongPaths(s string) string {
	if utf8.RuneCountInString(s) <= MaxRunes || !strings.Contains(s, "/") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	changed := false
	for i := 0; i < len(s); {
		if s[i] != '/' {
			b.WriteByte(s[i])
			i++
			continue
		}
		j := i + 1
		for j < len(s) {
			if s[j] == ' ' || s[j] == '\n' || s[j] == '\t' || s[j] == '"' {
				break
			}
			if s[j] == ':' && j+1 < len(s) && (s[j+1] == ' ' || s[j+1] == '"') {
				break
			}
			j++
		}
		path := s[i:j]
		if utf8.RuneCountInString(path) > 48 && strings.Count(path, "/") >= 2 {
			rest := utf8.RuneCountInString(s) - utf8.RuneCountInString(path)
			budget := MaxRunes - rest
			if budget < 8 {
				budget = 8
			}
			el := elidePath(path, budget)
			b.WriteString(el)
			if el != path {
				changed = true
			}
		} else {
			b.WriteString(path)
		}
		i = j
	}
	if !changed {
		return s
	}
	return b.String()
}

// minDirHead is the directory prefix kept when the filename uses the rest
// of the budget. Middle-clipping the filename alone would drop that head.
const minDirHead = 8

// elidePath keeps a prefix of p and "/filename" inside budget runes.
// When the filename is too long to keep whole, the directory head stays
// (at least minDirHead runes when the budget allows) and the filename
// keeps its suffix.
func elidePath(p string, budget int) string {
	if budget < 1 || utf8.RuneCountInString(p) <= budget {
		return p
	}
	base := filepath.Base(p)
	tail := "/" + base
	if !strings.Contains(p, "/") {
		tail = base
	}
	if utf8.RuneCountInString(tail) >= utf8.RuneCountInString(p) {
		return middle(p, budget)
	}
	tailN := utf8.RuneCountInString(tail)
	// Keep the whole filename only while the directory head still has
	// minDirHead runes. The band just above that (head of 1..7) used to
	// shrink "/KEEPHEAD" to "/K" and then "/".
	if budget-tailN-1 >= minDirHead {
		head := takeAtoms(p, budget-tailN-1)
		return head + "…" + tail
	}
	dir := strings.TrimSuffix(p, base)
	prefixN := utf8.RuneCountInString(dir)
	if strings.HasSuffix(dir, "/") {
		prefixN--
	}
	if prefixN < 1 {
		return middle(p, budget)
	}
	headN := minDirHead
	if headN > prefixN {
		headN = prefixN
	}
	// head + ellipsis + slash + at least one filename rune.
	for headN > 1 && budget < headN+3 {
		headN--
	}
	fileN := budget - headN - 2
	if fileN < 1 {
		return middle(p, budget)
	}
	return takeAtoms(dir, headN) + "…/" + suffixAtoms(base, fileN)
}

func middle(s string, budget int) string {
	if budget < 1 || utf8.RuneCountInString(s) <= budget {
		return s
	}
	if budget <= 1 {
		return takeAtoms(s, budget)
	}
	tailN := (budget - 1) / 2
	headN := budget - 1 - tailN
	if tailN < 1 || headN < 1 {
		return takeAtoms(s, budget)
	}
	return takeAtoms(s, headN) + "…" + suffixAtoms(s, tailN)
}

func suffixAtoms(s string, maxRunes int) string {
	if maxRunes <= 0 || s == "" {
		return ""
	}
	var cls []string
	state := -1
	rest := s
	for rest != "" {
		if n := tokenLen(rest); n > 0 {
			cls = append(cls, rest[:n])
			rest = rest[n:]
			state = -1
			continue
		}
		g, next, _, st := uniseg.FirstGraphemeClusterInString(rest, state)
		if g == "" {
			break
		}
		cls = append(cls, g)
		rest = next
		state = st
	}
	n := 0
	i := len(cls)
	for i > 0 {
		r := utf8.RuneCountInString(cls[i-1])
		if n+r > maxRunes {
			break
		}
		n += r
		i--
	}
	return strings.Join(cls[i:], "")
}
