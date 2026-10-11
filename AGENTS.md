# AGENTS.md: model-inspector

Guidance for coding agents and reviewers working in this repo. `CLAUDE.md` is a symlink to this file. README.md is the detailed user-facing spec for flags, target resolution, keys and Makefile behaviour.

## What this is

model-inspector is a Go 1.27 TUI and CLI (Bubble Tea, Bubbles, Lip Gloss) that inspects local LLM checkpoints. It:
- parses safetensors headers (an 8-byte little-endian length followed by a JSON header)
- resolves Hugging Face cache repo ids (`refs/<ref>`, then `snapshots/<commit>`)
- groups tensors by layer
- reports memory and storage estimates
- runs integrity checks (index vs shards, offsets and bounds)

**All model files, config files and cache directories are untrusted input.**

## Layout

- `cmd/model-inspector`: CLI entry point (`tui`, `inspect [--json]`, `--path`/`--repo`, `--strict`, `--no-color`, `--no-mouse`, `--no-clipboard`, `--version`).
- `internal/safeopen`: the only way to read files. It opens a single file descriptor, runs fstat, requires a regular file and caps reads with a LimitReader.
- `internal/safetensors`: header read and parse, dtype sizes and layout checks.
- `internal/hfcache`: HF cache resolver. Refs must be 40-64 hex characters, and the resolved path must stay under `snapshots/`.
- `internal/model`: loading, classification, stats, and the host-capable lookup table heuristic.
- `internal/scan`: checks and roll-up (pass/warn/fail/skip/partial).
- `internal/report`, `internal/jsonx`: JSON output (schema v1) and ordered JSON parsing.
- `internal/sanitize`: every string derived from a file goes through `sanitize.Text`/`From`. Program-authored text uses `Program`/`ui.L`, and `Notef` takes a literal format only.
- `internal/tui`, `internal/tui/ui`: the v2 UI (theme, chrome, Identity, Config tree, Layers, modals).
- `internal/fixture`: synthetic safetensors shards, index files, configs and fake HF hub caches for tests.

## Architecture

The pipeline in `cmd/model-inspector/main.go`:

1. `hfcache.ResolveTarget(target, hubDir, mode)` turns `--path`/`--repo`/a positional target into a directory. The CLI uses this, not `hfcache.Resolve`, which prefers local dirs.
2. `model.Load(dir)` reads headers (via `safetensors` and `safeopen`), the index and config, and produces `*model.Model`. Overflow or capped reads set `Model.Partial`.
3. `scan.RunOptions(m, Options{Strict})` runs the checks and computes `Result.Overall`.
4. `report.Build(m, res, ...)` produces the `Document` for `report.Marshal` (JSON schema v1, see `docs/json-schema.md`) and `report.Text` (non-TTY or `inspect`). The TUI skips it: `tui.Run(m, res)` takes the model and result directly.

Text trust boundary: `sanitize.Text` is an opaque type. File-derived strings enter via `sanitize.From`, and program literals via `sanitize.Program` or `ui.L`. Rendering code takes `Text`, so raw strings can't reach the screen without a deliberate conversion. `go vet -printf.funcs=Notef` plus an AST test in `internal/sanitize` enforce that `Notef` gets literal formats.

TUI: `internal/tui` holds the screens (Identity, Config tree, Layers, Scan) and chrome. `internal/tui/ui` holds the theme, palette and glyphs (with colour-profile and ASCII fallbacks), widgets, and the virtual list. Goldens cover 4 sizes × 5 colour profiles (truecolor, ansi256, ansi16, colorless, ascii) × overlays, so UI changes usually need `-update` and a diff review.

## Build and test

Requires GNU make (`gmake` on macOS). Bare `make` runs `test`. Run tests with `NO_COLOR` unset so colour paths are exercised.

```sh
make build                                           # bin/model-inspector, version and commit from Go build info
env -u NO_COLOR make test                            # go vet (incl. -printf.funcs=Notef) + go test -race + Makefile self-test
make fuzz                                            # all fuzz targets, FUZZTIME (default 10s), -fuzzminimizetime=2s
make cover                                           # coverage.out + per-func summary
make clean                                           # removes only this checkout's bin/ and coverage.out
go test -race -count=1 ./internal/scan -run TestName # single test
go test ./internal/safetensors -run '^$' -fuzz '^FuzzParse$' -fuzztime 30s  # one fuzz target
go test ./internal/tui -run TestGolden -update       # rewrite TUI goldens (testdata/golden)
UPDATE_GOLDEN=1 go test ./internal/report -run TestGoldenJSON  # rewrite JSON goldens
bin/model-inspector inspect --path DIR --json        # headless run
```

Tests need no downloaded checkpoints. Add new scenarios to `internal/fixture` (`Materialize`, `MaterializeHub`) rather than checking in binary files.

`make test` asserts it did not touch `bin/model-inspector` or `coverage.out`. Don't add tests that write to either.

## Security rules (non-negotiable)

1. **No raw file bytes on screen.** Route every file-derived string (tensor names, metadata, config keys and values, paths, repo/revision, error text) through `sanitize` before rendering in the TUI, text output or stderr. `--json` escapes C1, bidi and invisible characters, and `<`, as `\uXXXX`.
2. **Program text only through `Program`/`ui.L`/`Notef`,** with constant arguments. Never pass variables, `err.Error()`, or formatted strings that contain file data.
3. **Read files only via `safeopen`.** No `os.ReadFile`, and no stat-then-open. Reject non-regular files (FIFOs, devices) and keep symlinks contained.
4. **Bound everything:**
   - header length must be at most the file size minus 8, with a 100 MiB cap
   - tensor-count, rank, name-length and metadata-length caps
   - config limits: depth 32 and the node cap
   - walk entry and depth caps
   - layer index cap of 4096
   - clipboard cap of 64 KiB
5. **Never wrap or overflow arithmetic silently.** Use checked or saturating math, and mark the result Partial on overflow. Partial must never roll up to PASS or exit 0.
6. **Never execute or unpickle anything.** Code and pickle inspection, when it lands, is static and capped.
7. **OSC52 copy** happens only on a user keypress, with sanitized content. It is disabled on non-TTY output, with `--no-clipboard`, or with `MODEL_INSPECTOR_NO_OSC52=1`. Never read the clipboard.
8. **Makefile:** VERSION and COMMIT values must match `[0-9A-Za-z.+_-]`. Never interpolate repo paths or file content into shell commands. `clean` must never delete outside `bin/`.

## Exit codes

- `0`: pass or warn.
- `1`: fail, partial, or warn under `--strict`.
- `2`: usage error or ambiguous target.
- `3`: skip (empty folder, GGUF-only, missing or not a directory).

## Workflow

- PRs are on Cursor Origin: https://cursor.com/codebase/cecilkootz/model-inspector
- Every PR gets a QA and security review from the "Inspector QA & Security" agent. It blocks only on sanitizer bypasses, crashes or panics, OOM or hangs, and wrong numbers; nits are tracked in follow-up PRs.
- Add a regression test or fuzz seed for every bug fixed, in the matching `qa_prN_test.go` or in `hostile_test.go`. Keep goldens up to date (`TestGolden`, `TestGoldenJSON`).
- Design specs live in the shared workspace (`ux-spec-v1.md`, `v2/*.md`) and are owned by the Inspector Design agent.
- Current in-flight status is in `docs/IN_FLIGHT_REVIEWS.md`. Check it before touching an area with an open PR.
