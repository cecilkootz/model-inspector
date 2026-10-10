# AGENTS.md: model-inspector

Guidance for coding agents and reviewers working in this repo.

## What this is

model-inspector is a Go 1.27 TUI and CLI (Bubble Tea, Bubbles, Lip Gloss) that inspects local LLM checkpoints. It parses safetensors headers, resolves a Hugging Face cache repo id, groups tensors, estimates memory, and runs structural checks.

**All model files, config files and cache directories are untrusted input.**

## Architecture

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
- Goldens are 80×24 and 140×40 in truecolor, ansi256, and colorless. Regenerate with `env -u NO_COLOR go test ./internal/tui/ -run 'TestGolden|TestConfigGoldens|TestLayersGolden|TestScanGolden' -update`.
- Perf budgets, median of a short loop: cached View ≤ 2 ms, Update ≤ 0.5 ms, Resize ≤ 5 ms.

## Build and test

```
make test         # go vet (incl. -printf.funcs=Notef) + go test -race
make build        # bin/model-inspector
make clean        # this checkout's bin/ and coverage.out only
make fuzz         # every Fuzz* target, FUZZTIME (default 10s)
```

Requires GNU make. Run tests with `NO_COLOR` unset (`env -u NO_COLOR make test`).

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
- Only Matthew merges.
- Stacked PRs stay based on the parent branch. After the parent merges, retarget the child to main.
- Current in-flight status is in `docs/IN_FLIGHT_REVIEWS.md`.
- PRs are on Cursor Origin: https://cursor.com/codebase/cecilkootz/model-inspector
