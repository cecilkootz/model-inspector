# In-flight reviews

Status as of 2026-10-10.

## Merged on main

#1 through #17. Nothing is open.

## Next

- **metadata-sanity** (warn-only). `plannedMetadata` in `internal/scan/scan.go` still returns `planned`, so the Scan card reads `not run: planned`. The check compares config fields with tensor shapes.
- **Real Config golden.** `fixture.ConfigQwenLike` is synthetic. Replace it with the real Vontra `config.json` once Matthew supplies it.
- **Case-insensitive hub lookup.** On a case-folding filesystem (macOS APFS), `inspect ORG/NAME` resolves to the `Org/Name` cache, exits 0, and reports the repo id as `ORG/NAME`. Linux exits 3. `TestP3_Targets` skips on case-insensitive filesystems until the resolver decides this.
