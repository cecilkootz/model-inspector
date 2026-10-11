# AGENTS.md: model-inspector

Guidance for coding agents and reviewers working in this repo. `CLAUDE.md` is a symlink to this file. README.md is the detailed user-facing spec for flags, target resolution, keys and Makefile behaviour.

## What this is

model-inspector is a Go 1.27 TUI and CLI (Bubble Tea, Bubbles, Lip Gloss) that inspects local LLM checkpoints. It parses safetensors headers, resolves a Hugging Face cache repo id, groups tensors, estimates memory, and runs structural checks.

**All model files, config files and cache directories are untrusted input.**

## Layout

- `cmd/model-inspector`: CLI entry point (`tui`, `inspect [--json]`, `--path`/`--repo`, `--strict`, `--no-color`, `--no-mouse`, `--no-clipboard`, `--version`).
- `internal/safeopen`: the only way to read files. One descriptor, fstat, regular files only, reads capped.
- `internal/safetensors`: header read and parse, dtype sizes and layout checks.
- `internal/hfcache`: HF cache resolver. Refs must be 40-64 hex characters, and the resolved path must stay under `snapshots/`.
- `internal/model`: loading, classification, stats, layer exploration.
- `internal/scan`: checks and roll-up (pass/warn/fail/skip/partial).
- `internal/report`, `internal/jsonx`: JSON output (schema v1, `docs/json-schema.md`) and ordered JSON parsing.
- `internal/sanitize`, `internal/fileerr`: the text trust boundary (see Conventions).
- `internal/fixture`: synthetic safetensors shards, index files, configs and fake HF hub caches for tests.

## Architecture

Headless (`inspect`, `--json`, non-TTY) in `cmd/model-inspector/main.go`:

1. `hfcache.ResolveTarget(target, hubDir, mode)` turns `--path`/`--repo`/a positional target into a directory. The CLI uses this, not `hfcache.Resolve`, which prefers local dirs.
2. `model.Load(dir)` reads headers (via `safetensors` and `safeopen`), the index and config. Overflow or capped reads set `Model.Partial`.
3. `scan.RunOptions(m, Options{Strict})` runs the checks.
4. `report.Build` produces the `Document` for `report.Marshal` (JSON) or `report.Text`.

The interactive TUI resolves the target, then hands the directory to `tui.RunLoading`, which loads and scans inside the program.

`sanitize.Text` is opaque, so raw strings can't reach the screen without a deliberate conversion. `go vet -printf.funcs=Notef` plus an AST test in `internal/sanitize` enforce Notef's literal format.

The v2 UI lives in `internal/tui` and `internal/tui/ui`.

- **Theme tokens** (`ui/theme.go`): fills, ink, surfaces, and `ui.L`, which caches a program literal as `sanitize.Program`.
- **Chrome** (`screen.go`, `chrome.go`): wordmark, tab rule, status bar. The status bar splits file-derived fields (`sanitize.From`) from static segments (`ui.Chrome` / `ui.L`).
- **Primitives** (`ui/widgets.go`): Pill, Panel, Title, KV, Meter, Button, Spinner, Modal, Segments, Notice.
- **Zones** (`ui/layout.go`): hit rectangles for tabs, panes, cards, and palette rows. Later zones win on overlap.
- **Key registry** (`ui/keys.go`): one `Context` per screen or overlay. A `Tree` context may shadow only `left`, `right`, `h`, and `l`. `TestKeymapNoConflicts` walks `Contexts`.

## Screens

| Screen | Files | Notes |
| --- | --- | --- |
| Identity | `identity.go` | Tiles, storage, callouts. |
| Config tree | `config.go`, `config_tree.go`, `config_view.go` | Ordered JSON tree, key fields, raw text. |
| Layers tree | `layers.go`, `layers_view.go` | Groups, heatmap, detail. Distinct layer indices are capped. |
| Scan | `scan_view.go`, `scan_input.go` | Verdict band, check cards, detail, code list. Checks live in `internal/scan`. |

`1`–`4`, tab, and the command palette move between screens.

## Conventions

- Untrusted data goes through `sanitize.From` once. Do not sanitize it again.
- Static program text goes through `ui.L` or `sanitize.Program` unchanged. Never sanitize our own strings.
- `sanitize.Notef` takes a constant format (`formatLit`). Arguments are sanitized inside Notef. The result is an error; do not pass it through `From`.
- Errors copied out of a file are clipped at 256 runes with `fileerr.Present` (sanitize, then clip). `fileerr.Error` keeps `errors.Is`.
- Allocations are capped by distinct layers (the 4096 index cap and the sparse `LayerInfoBy` path). Do not allocate by the maximum index.
- Screen goldens (Layers, Scan) are 80×24 and 140×40 in truecolor, ansi256, and colorless; `TestGolden` also covers 60×16, 100×30, ansi16 and ascii. Regenerate with `env -u NO_COLOR go test ./internal/tui/ -run 'TestGolden|TestConfigGoldens|TestLayersGolden|TestScanGolden' -update`, and review the diff.
- Perf budgets, median of a short loop: cached View ≤ 2 ms, Update ≤ 0.5 ms, Resize ≤ 5 ms.

## Build and test

Requires GNU make 3.81 or later; macOS's `/usr/bin/make` works, BSD make does not. Bare `make` runs `test`. Run tests with `NO_COLOR` unset so colour paths are exercised.

```sh
env -u NO_COLOR make test                            # go vet (incl. -printf.funcs=Notef) + go test -race + Makefile self-test
make build                                           # bin/model-inspector
make clean                                           # this checkout's bin/ and coverage.out only
make fuzz                                            # every Fuzz* target, FUZZTIME (default 10s)
make cover                                           # coverage.out + per-func summary
go test -race -count=1 ./internal/scan -run TestName # single test
go test ./internal/safetensors -run '^$' -fuzz '^FuzzParse$' -fuzztime 30s  # one fuzz target
UPDATE_GOLDEN=1 go test ./internal/report -run TestGoldenJSON  # rewrite JSON goldens
bin/model-inspector inspect --path DIR --json        # headless run
```

Tests need no downloaded checkpoints. Add new scenarios to `internal/fixture` rather than checking in binary files.

`make test` asserts it did not touch `bin/model-inspector` or `coverage.out`. Don't add tests that write to either.

## Security rules

1. **No raw file bytes on screen.** File-derived strings go through `sanitize.From` or `fileerr.Present` before the TUI, the text summary, or stderr. `--json` escapes C1, bidi, invisible characters, and `<` as `\uXXXX`.
2. **Program text only through `ui.L`, `sanitize.Program`, or `Notef`,** with constant arguments.
3. **Read files only via `safeopen`.** No `os.ReadFile`, and no stat-then-open.
4. **Bound everything:** header length, tensor count, rank, name length, config depth and node cap, walk caps, the 4096 layer index, and the 64 KiB clipboard.
5. **Never wrap arithmetic silently.** Mark the inspection Partial on overflow. Partial must not roll up to a pass.
6. **Never execute or unpickle anything.** Code and pickle inspection is names-only until a later reader.
7. **OSC 52 copy** happens only on a user keypress. `--no-clipboard` or `MODEL_INSPECTOR_NO_OSC52=1` turns it off.
8. **Makefile:** VERSION and COMMIT must match `[0-9A-Za-z.+_-]`. `clean` must never delete outside `bin/`.

## Exit codes

`inspect` and `--json`:

- `0`: inspected, and no check failed (warnings are fine unless `--strict`).
- `1`: a check failed, or a warning under `--strict`. The summary is still printed.
- `2`: usage error or ambiguous target.
- `3`: the target could not be inspected (missing, not a directory, GGUF-only, or no weights).

The interactive TUI exits 0 when the snapshot loads, and 3 when the load-error card is dismissed. That status is load success, not the scan verdict. CI should use `inspect` or `--json`.

## Workflow

- Small PRs. One screen or one behaviour each.
- QA reviews before merge. QA blocks only on sanitizer bypasses, crashes or panics, OOM or hangs, and wrong numbers. Nits are batched into a follow-up PR.
- Add a regression test or fuzz seed for every bug fixed, in the matching `qa_prN_test.go` or in `hostile_test.go`. The N in existing `qa_prN_test.go` files is a Cursor Origin PR number, not a GitHub one.
- Only Matthew merges.
- Merge only when every CI check on the PR is green. Branch protection is unavailable on this private free-plan repo, so this is not enforced; reviewers confirm `gh pr checks` before recommending a merge.
- Stacked PRs stay based on the parent branch. After the parent merges, retarget the child to main.
- Design specs live in the shared workspace (`ux-spec-v1.md`, `v2/*.md`) and are owned by the Inspector Design agent.
- Current in-flight status is in `docs/IN_FLIGHT_REVIEWS.md`. Check it before touching an area with an open PR.
- PRs are on GitHub: https://github.com/cecilkootz/model-inspector. Push to the `origin` remote and use `gh`. The `cursor` remote (Cursor Origin) is retired. Do not push to it or open PRs there.
