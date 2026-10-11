# In-flight reviews

Status as of 2026-10-10.

PRs moved from Cursor Origin to GitHub. Numbers below are GitHub PRs unless marked Cursor Origin.

## Merged on main

Cursor Origin #1 through #17.

## Open

- **#1**: the test suite passes on macOS, and this doc and AGENTS.md now point at GitHub.

## Next

- **metadata-sanity** (warn-only). `plannedMetadata` in `internal/scan/scan.go` still returns `planned`, so the Scan card reads `not run: planned`. The check compares config fields with tensor shapes.
- **Real Config golden.** `fixture.ConfigQwenLike` is synthetic. Replace it with the real Vontra `config.json` once Matthew supplies it.
- **Case-insensitive hub lookup.** On a case-folding filesystem (macOS APFS), `inspect ORG/NAME` resolves to the `Org/Name` cache, exits 0, and reports the repo id as `ORG/NAME`. Linux exits 3. `TestP3_Targets` skips on case-insensitive filesystems until the resolver decides this.
