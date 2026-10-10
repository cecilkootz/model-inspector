package version

import (
	"runtime/debug"
	"strings"
	"testing"
)

func TestCommitFromSettings(t *testing.T) {
	cases := []struct {
		name     string
		settings []debug.BuildSetting
		want     string
	}{
		{name: "missing", want: "unknown"},
		{
			name: "clean",
			settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "abc123"},
				{Key: "vcs.modified", Value: "false"},
			},
			want: "abc123",
		},
		{
			name: "dirty",
			settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "abc123"},
				{Key: "vcs.modified", Value: "true"},
			},
			want: "abc123-dirty",
		},
		{
			name: "modified without revision",
			settings: []debug.BuildSetting{
				{Key: "vcs.modified", Value: "true"},
			},
			want: "unknown",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := commitFromSettings(tc.settings); got != tc.want {
				t.Fatalf("commit %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExplicitCommitSkipsBuildInfo(t *testing.T) {
	old := Commit
	t.Cleanup(func() { Commit = old })
	Commit = "override"
	if got := currentCommit(); got != "override" {
		t.Fatalf("commit %q", got)
	}
}

func TestStringSanitizesVersionAndCommit(t *testing.T) {
	oldV, oldC := Version, Commit
	t.Cleanup(func() {
		Version, Commit = oldV, oldC
	})
	Version = "1.2\x1b"
	Commit = "abc<def"
	got := String()
	if strings.Contains(got, "\x1b") || strings.Contains(got, "abc<def") {
		t.Fatalf("unsanitized %q", got)
	}
	if !strings.Contains(got, "<U+001B>") || !strings.Contains(got, "<U+003C>") {
		t.Fatalf("tokens missing in %q", got)
	}
}

func TestDefaultVersion(t *testing.T) {
	if Version != "0.1.0" {
		t.Fatalf("Version %q", Version)
	}
}
