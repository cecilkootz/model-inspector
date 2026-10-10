package fileerr

import (
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
	err := Error(errString(strings.Repeat("1", 1000) + "TAIL"))
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
	short := Error(errString("ok"))
	if short.Error() != "ok" {
		t.Fatal(short)
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
