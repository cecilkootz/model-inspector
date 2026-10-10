package fileerr

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestStringKeepsPrefixEllipsisAndLength(t *testing.T) {
	const tail = "END"
	s := strings.Repeat("é", 300) + tail
	got := String(s)
	if n := utf8.RuneCountInString(got); n != MaxRunes {
		t.Fatalf("runes %d, want %d", n, MaxRunes)
	}
	if !utf8.ValidString(got) {
		t.Fatal("invalid UTF-8")
	}
	if strings.Contains(got, tail) {
		t.Fatalf("tail survived: %q", got)
	}
	if !strings.Contains(got, "… (") || !strings.HasSuffix(got, " runes)") {
		t.Fatalf("suffix: %q", got)
	}
	if n := reportedLen(t, got); n != utf8.RuneCountInString(s) {
		t.Fatalf("reported %d, want %d", n, utf8.RuneCountInString(s))
	}
	if String(got) != got {
		t.Fatal("clipping twice changed the text")
	}
	if String("short") != "short" {
		t.Fatal("short string changed")
	}
}

func TestErrorDropsOversizedText(t *testing.T) {
	orig := errString(strings.Repeat("1", 1000) + "TAIL")
	err := Error(orig)
	msg := err.Error()
	if strings.Contains(msg, "TAIL") {
		t.Fatalf("tail survived: %q", msg)
	}
	if utf8.RuneCountInString(msg) != MaxRunes {
		t.Fatalf("runes %d", utf8.RuneCountInString(msg))
	}
	if reportedLen(t, msg) != 1004 {
		t.Fatalf("length %d in %q", reportedLen(t, msg), msg)
	}
	if errors.Unwrap(err) != error(orig) {
		t.Fatal("clipped error dropped the original")
	}
	short := Error(errString("ok"))
	if short.Error() != "ok" || short != errString("ok") {
		t.Fatal(short)
	}
}

func TestPresentSanitizesThenClips(t *testing.T) {
	in := strings.Repeat("\x1b", 400)
	got := Present(in)
	if n := utf8.RuneCountInString(got); n > MaxRunes {
		t.Fatalf("visible runes %d", n)
	}
	if strings.ContainsRune(got, '\x1b') {
		t.Fatal("raw ESC survived sanitizing")
	}
	if !strings.Contains(got, "<U+001B>") {
		t.Fatalf("controls were not tokenized: %q", got)
	}
	if reportedLen(t, got) != len(in) {
		t.Fatalf("reported %d, want %d", reportedLen(t, got), len(in))
	}
	body := got[:strings.LastIndex(got, "… (")]
	if strings.Contains(body, "<") && !strings.HasSuffix(body, ">") {
		t.Fatalf("token split: %q", body[len(body)-12:])
	}
	if String(got) != got {
		t.Fatal("clipping a sanitized string changed it again")
	}
}

func TestStringKeepsCombiningMarkWithBase(t *testing.T) {
	const mark = "\u0301"
	in := strings.Repeat("e"+mark, 300)
	got := String(in)
	if n := utf8.RuneCountInString(got); n > MaxRunes {
		t.Fatalf("runes %d", n)
	}
	body := got[:strings.LastIndex(got, "… (")]
	if strings.Count(body, "e") != strings.Count(body, mark) {
		t.Fatalf("split a base from its combining mark: %q", body[max(0, len(body)-8):])
	}
	if strings.HasSuffix(body, "e") {
		t.Fatal("prefix ends on the base and drops the combining mark")
	}
}

func TestErrorElidesLongPathAndUnwraps(t *testing.T) {
	dir := strings.Repeat("d", 240)
	path := "/" + dir + "/config.json"
	orig := &fs.PathError{Op: "lstat", Path: path, Err: fs.ErrNotExist}
	err := Error(orig)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("clipped path error is not fs.ErrNotExist")
	}
	var pe *fs.PathError
	if !errors.As(err, &pe) || pe.Path != path {
		t.Fatalf("unwrap path = %#v", pe)
	}
	msg := err.Error()
	if n := utf8.RuneCountInString(msg); n > MaxRunes {
		t.Fatalf("visible runes %d: %q", n, msg)
	}
	if !strings.Contains(msg, "config.json") || !strings.Contains(msg, "lstat /"+dir[:8]) {
		t.Fatalf("path clip dropped the head or the filename: %q", msg)
	}
	if !strings.Contains(msg, "…/") {
		t.Fatalf("path was not elided in the middle: %q", msg)
	}
	if strings.Contains(msg, dir) {
		t.Fatal("full directory survived")
	}
	short := &fs.PathError{Op: "open", Path: "/no/such", Err: fs.ErrNotExist}
	if Error(short) != short {
		t.Fatal("short path error was replaced")
	}
}

func TestErrorKeepsDirectoryHeadWhenFilenameDominates(t *testing.T) {
	dir := "/KEEPHEAD"
	name := strings.Repeat("n", 300) + ".bin"
	path := dir + "/" + name
	orig := &fs.PathError{Op: "lstat", Path: path, Err: fs.ErrNotExist}
	err := Error(orig)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("clipped path error is not fs.ErrNotExist")
	}
	msg := err.Error()
	if n := utf8.RuneCountInString(msg); n > MaxRunes {
		t.Fatalf("visible runes %d: %q", n, msg)
	}
	if !strings.Contains(msg, "/KEEPHEA") {
		t.Fatalf("dropped directory head: %q", msg)
	}
	if !strings.Contains(msg, ".bin") {
		t.Fatalf("dropped filename tail: %q", msg)
	}
	if strings.Contains(msg, name) {
		t.Fatal("full filename survived")
	}
	if strings.Contains(msg, dir+"/") {
		t.Fatal("filename was not elided from the directory")
	}
}

// TestElideKeepsMinDirectoryHeadOnBoundary covers every filename length
// where keeping the whole name would leave a directory head shorter than
// minDirHead. That is the band between a full "/KEEPHEA…" head and the
// switch that used to produce "/K…" and then "/…", for a path shaped like
// scan /KEEPHEAD/sub/ + n's + .bin.
func TestElideKeepsMinDirectoryHeadOnBoundary(t *testing.T) {
	const dir = "/KEEPHEAD/sub/"
	var band []int
	atMin := -1
	for n := 1; n <= 400; n++ {
		name := strings.Repeat("n", n) + ".bin"
		path := dir + name
		raw := (&fs.PathError{Op: "scan", Path: path, Err: fs.ErrNotExist}).Error()
		rest := utf8.RuneCountInString(raw) - utf8.RuneCountInString(path)
		budget := MaxRunes - rest
		if budget < 8 {
			budget = 8
		}
		headRoom := budget - (1 + utf8.RuneCountInString(name)) - 1
		msg := String(raw)
		if utf8.RuneCountInString(msg) > MaxRunes {
			t.Fatalf("n=%d visible runes %d: %q", n, utf8.RuneCountInString(msg), msg)
		}
		switch {
		case headRoom == minDirHead:
			atMin = n
			if !strings.Contains(msg, "/KEEPHEA") || !strings.Contains(msg, name) {
				t.Fatalf("n=%d on the min-head boundary: %q", n, msg)
			}
		case headRoom >= 1 && headRoom < minDirHead:
			band = append(band, n)
			if !strings.Contains(msg, "/KEEPHEA") || !strings.Contains(msg, ".bin") {
				t.Fatalf("n=%d head room %d collapsed: %q", n, headRoom, msg)
			}
			if strings.Contains(msg, "/K…/") || strings.Contains(msg, "/…/") {
				t.Fatalf("n=%d head room %d shrunk the directory head: %q", n, headRoom, msg)
			}
			if strings.Contains(msg, name) {
				t.Fatalf("n=%d kept the whole filename: %q", n, msg)
			}
		}
	}
	if atMin < 0 {
		t.Fatal("no filename length sits on the minDirHead boundary")
	}
	if len(band) != minDirHead-1 {
		t.Fatalf("band %v, want %d lengths", band, minDirHead-1)
	}
	for i := 1; i < len(band); i++ {
		if band[i] != band[i-1]+1 {
			t.Fatalf("band is not contiguous: %v", band)
		}
	}
}

func reportedLen(t *testing.T, msg string) int {
	t.Helper()
	const mark = "… ("
	i := strings.LastIndex(msg, mark)
	if i < 0 {
		t.Fatalf("no length in %q", msg)
	}
	rest := msg[i+len(mark):]
	end := strings.Index(rest, " runes)")
	if end <= 0 {
		t.Fatalf("bad suffix %q", msg)
	}
	n := 0
	for _, r := range rest[:end] {
		if r < '0' || r > '9' {
			t.Fatalf("bad length %q", rest[:end])
		}
		n = n*10 + int(r-'0')
	}
	return n
}

type errString string

func (e errString) Error() string { return string(e) }
