package tui

import (
	"strings"
	"testing"
)

func TestClassLabelDoesNotDependOnCaseOrder(t *testing.T) {
	if got := classLabel("q4_packed").String(); got != "q4 packed" {
		t.Fatalf("packed %q", got)
	}
	if got := classLabel("q_scales_biases").String(); got != "scales/biases" {
		t.Fatalf("scales %q", got)
	}
	// plain_<dtype>_packed must sanitize the dtype even though it ends in _packed.
	got := classLabel("plain_\x1b_packed").String()
	if strings.Contains(got, "\x1b") || !strings.Contains(got, "<U+001B>") {
		t.Fatalf("plain dtype %q", got)
	}
	// A class that merely ends in _packed is file data, not a bit width.
	raw := classLabel("q\x1b_packed").String()
	if strings.Contains(raw, "\x1b") || !strings.Contains(raw, "<U+001B>") {
		t.Fatalf("unsanitized packed suffix %q", raw)
	}
}
