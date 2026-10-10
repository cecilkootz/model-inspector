// Package version is the build identity written into schema_version 1 JSON.
// Version and Commit are string variables so `make build` can set them with
// -ldflags -X. Commit is not part of the JSON document.
package version

import "fmt"

// Name is the tool name.
const Name = "model-inspector"

// Version is the release version. The default matches a binary built without
// -ldflags. `make build` injects the same string unless VERSION= is set.
// Commit is a short git revision set at link time, or "unknown" when the
// binary was not built from a git checkout.
var (
	Version = "0.1.0"
	Commit  = "unknown"
)

// String is the identity printed by --version, without a trailing newline.
func String() string {
	return fmt.Sprintf("%s %s (%s)", Name, Version, Commit)
}
