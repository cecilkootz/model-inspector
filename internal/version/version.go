// Package version is the build identity written into schema_version 1 JSON.
//
// Version defaults to the string in this file. Commit is empty unless
// overridden with -ldflags -X. An empty Commit is filled from the toolchain
// VCS stamp (runtime/debug.ReadBuildInfo: vcs.revision, and a "-dirty"
// suffix when vcs.modified is true). The Makefile does not read this file.
// Commit is not part of the JSON document.
package version

import (
	"fmt"
	"runtime/debug"

	"origin.cursor.com/cecilkootz/model-inspector/internal/sanitize"
)

// Name is the tool name.
const Name = "model-inspector"

// Version is the release version. make build VERSION=... may override it
// with -ldflags -X. The default stays in this source file.
var Version = "0.1.0"

// Commit overrides the VCS revision when set with -ldflags -X.
// Empty selects the revision from build info.
var Commit string

// String is the identity printed by --version, without a trailing newline.
// Version and the commit are passed through the terminal sanitizer.
func String() string {
	return fmt.Sprintf("%s %s (%s)",
		Name,
		sanitize.From(Version).String(),
		sanitize.From(currentCommit()).String(),
	)
}

func currentCommit() string {
	if Commit != "" {
		return Commit
	}
	return commitFromSettings(buildSettings())
}

func buildSettings() []debug.BuildSetting {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return nil
	}
	return info.Settings
}

// commitFromSettings formats vcs.revision. vcs.modified=true appends "-dirty".
// A missing revision is "unknown".
func commitFromSettings(settings []debug.BuildSetting) string {
	var rev, modified string
	for _, s := range settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			modified = s.Value
		}
	}
	if rev == "" {
		return "unknown"
	}
	if modified == "true" {
		return rev + "-dirty"
	}
	return rev
}
