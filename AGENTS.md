# AGENTS.md: model-inspector

Guidance for coding agents and reviewers working in this repo.

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

## Build and test

```
make build        # bin/model-inspector, version and commit from Go build info
make test         # go vet (incl. -printf.funcs=Notef) + go test -race + Makefile self-test
make fuzz         # all fuzz targets, FUZZTIME (default 10s), -fuzzminimizetime=2s
make clean        # removes only this checkout's bin/ and coverage.out
```

Requires GNU make (`gmake` on macOS). Run tests with `NO_COLOR` unset (`env -u NO_COLOR make test`) so colour paths are exercised.

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
- Add a regression test or fuzz seed for every bug fixed. Keep goldens up to date (`TestGolden`).
- Design specs live in the shared workspace (`ux-spec-v1.md`, `v2/*.md`) and are owned by the Inspector Design agent.
- Current in-flight status is in `docs/IN_FLIGHT_REVIEWS.md`.
