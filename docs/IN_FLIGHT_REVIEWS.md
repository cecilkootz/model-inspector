# In-flight reviews

Status as of 2026-10-11.

PRs moved from Cursor Origin to GitHub. Numbers below are GitHub PRs unless marked Cursor Origin.

## Merged on main

Cursor Origin #1 through #17, then GitHub #1 (macOS test fixes, docs point at GitHub) and #2 (module path `github.com/cecilkootz/model-inspector`).

## Open

- **#3**: GitHub Actions runs `make test` on Linux and macOS, plus `make fuzz` on Linux.

## Next

- **metadata-sanity** (warn-only). `plannedMetadata` in `internal/scan/scan.go` still returns `planned`, so the Scan card reads `not run: planned`. The check compares config fields with tensor shapes.
- **Real Config golden.** `fixture.ConfigQwenLike` is synthetic. Replace it with the real Vontra `config.json` once Matthew supplies it.
- **Case-insensitive hub lookup.** On a case-folding filesystem (macOS APFS), `inspect ORG/NAME` resolves to the `Org/Name` cache, exits 0, and reports the repo id as `ORG/NAME`. Linux exits 3. `TestP3_Targets` skips on case-insensitive filesystems until the resolver decides this. `FuzzResolve` hits the same case on macOS, so `make fuzz` fails there; CI fuzzes on Linux only.
