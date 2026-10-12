package main

import (
	"strings"
	"testing"
)

// The panels layout is the only layout; --layout is an unknown flag.
func TestLayoutFlagRemoved(t *testing.T) {
	for _, args := range [][]string{
		{"--path", "d", "--layout", "panels"},
		{"--path", "d", "--layout=tabs"},
	} {
		if _, err := parseArgs(args); err == nil || !strings.Contains(err.Error(), "unknown flag") {
			t.Errorf("%v: err %v, want unknown flag", args, err)
		}
	}
	if strings.Contains(usage, "--layout") {
		t.Error("usage still documents --layout")
	}
}
