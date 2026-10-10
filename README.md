# model-inspector

A terminal UI (Bubble Tea / Bubbles / Lip Gloss) for inspecting local LLM
weights **without loading them**: identity, architecture config, per-layer
tensor breakdown, and a structural integrity check.

Module path: `origin.cursor.com/cecilkootz/model-inspector`.

## Safety: headers only

model-inspector never reads or mmaps tensor data. Checkpoint files, symlinks,
index JSON, configs, and HF refs are treated as hostile input. It only:

- opens each small file once, fstats that descriptor, and requires a regular
  file (a symlink to `/dev/zero` or a FIFO is refused, not read),
- reads safetensors headers: the first 8 bytes (little-endian u64 length N)
  plus N bytes of JSON per shard, and only when N is at most the file size
  minus 8 and at most 100 MiB,
- reads small JSON files (`config.json`, `*.safetensors.index.json`), capped
  at 64 MiB, and HF `refs/` files, capped at 1 KiB.

File-derived text shown in the TUI or printed as text is sanitized. Controls,
bidi overrides, Unicode format characters, and other invisible characters
become `<U+XXXX>` tokens. A literal `<` is tokenized too, so the characters
`<U+001B>` in a file cannot impersonate an ESC. The cap is 4096 runes of
sanitized text (plus one ellipsis rune when the value is truncated), not
4096 bytes. `--json` keeps parsed values, and encodes C1, bidi, and other
invisible characters in those strings as `\uXXXX` escapes.

It never writes into the Hugging Face cache. Inspecting a ~106 GiB model takes
~15 ms and ~11 MB RSS.

## Install

Clone the repo, then install from that checkout:

```sh
git clone git@origin.cursor.com:cecilkootz/model-inspector.git
cd model-inspector
go install ./cmd/model-inspector
```

GNU make targets (on macOS, `gmake` when `/usr/bin/make` is BSD make):

```sh
make build   # bin/model-inspector
make test    # override check, go vet ./..., go test -race -count=1 ./...
make clean   # bin/ and coverage.out only; BIN is not a delete path
make fuzz    # every fuzz target, for FUZZTIME (default 10s)
```

Bare `make` runs `test`.

`make build` leaves the version at `internal/version.Version` (`0.1.0`) and
does not pass `-ldflags` unless `VERSION` or `COMMIT` is set. The commit
printed by `--version` comes from the Go toolchain VCS stamp
(`runtime/debug.ReadBuildInfo`: full `vcs.revision`, with `-dirty` appended
when `vcs.modified` is true). `go install ./cmd/model-inspector` uses that
same stamp. A binary with no VCS stamp prints `unknown`. `--version` runs
the version and the commit through the terminal sanitizer.

`VERSION`, `COMMIT`, `FUZZTIME`, and `BIN` may be set in the environment or
on the make command line. A command-line assignment wins. An empty
`VERSION` or `COMMIT`, or one that is only spaces and tabs, is treated as
unset. A set value must match `^[0-9A-Za-z][0-9A-Za-z.+_-]*$` (a letter or
digit, then those characters, `.`, `+`, `_`, or `-`). Any other value fails
the build before the linker runs. `FUZZTIME` matches `^[0-9A-Za-z.+_-]+$`.
The C `LDFLAGS` variable is left alone.

Go's default `-buildvcs=auto` records the git repository that contains the
main module, the current directory, and the main package. A checkout with
its own `.git` reports that revision, including when it sits inside another
repository. A tree with no `.git` of its own that lives inside another
repository reports the outer repository's revision; `-buildvcs=true` still
stamps that outer revision. A tree that is not inside any repository, or a
build with `-buildvcs=false`, prints `unknown`.

`BIN` defaults to `bin/model-inspector` (paths with spaces are quoted).
`build`, `test`, `cover`, `fuzz`, and `clean` run in the directory that
contains the Makefile, not the caller's working directory, so
`make -f /path/to/Makefile` from another directory still reads and writes
this checkout. That directory must match `[A-Za-z0-9._/+-]`; a space or
any other character makes `make` stop before it runs a recipe. `make clean` deletes only `coverage.out` (when it is a
regular file or a symlink) and this checkout's `bin/` directory. It does
not remove `BIN`. That variable is also read from the environment, and
using it as a delete path removed `.git` and tracked files. A binary
written under `bin/`, including a custom name, is removed with the
directory. `rm -rf` runs only after `bin` resolves to this repository's
`bin` directory; a symlinked `bin` is unlinked and not followed. A
directory named `coverage.out` is left in place with a warning, `bin/` is
still removed, and `clean` exits non-zero. The override check runs that
`clean` only against a scratch copy of the Makefile, so `make test` leaves
this checkout's `bin/` and `coverage.out` in place. `bin/` is gitignored.
Recipes call `go` on `PATH` and honor `GOTOOLCHAIN=local` (no toolchain
download).

`go install origin.cursor.com/cecilkootz/model-inspector/cmd/model-inspector@latest`
fails even with `GOPRIVATE=origin.cursor.com`. Origin does not answer Go's
import-path discovery (`?go-get=1`), so the toolchain 404s before it can
fetch the module. The module path stays
`origin.cursor.com/cecilkootz/model-inspector`.

## Build

Go is installed user-space at `~/go-sdk/go`. Add to your shell rc if you like:

```sh
export PATH="$HOME/go-sdk/go/bin:$HOME/src/model-inspector/bin:$PATH"
```

```sh
cd ~/src/model-inspector
make          # same as make test
make build    # bin/model-inspector
make test
make clean    # bin/ and coverage.out only
make fuzz     # FUZZTIME=10s by default; make fuzz FUZZTIME=30s
```

`make test` builds tiny safetensors fixtures in a temp directory (real
headers, an index, a config, and a fake HF hub cache). It does not need a
downloaded checkpoint.

## Run

A target is a model directory or an HF repo id (`org/name`, optionally
`@ref` or `@commit`) resolved through the hub cache (`$HF_HUB_CACHE`,
`$HF_HOME/hub`, or `~/.cache/huggingface/hub`; override with `--hub-dir`).

```sh
# Interactive TUI. A bare org/name that is also a local directory is refused.
bin/model-inspector --repo Vontra/Qwen3.8-Flash-Next-MLX-4bit-MTP

# Headless text, or schema_version 1 JSON
bin/model-inspector inspect --repo Vontra/Qwen3.8-Flash-Next-MLX-4bit-MTP
bin/model-inspector inspect --path /path/to/snapshot --json
```

When stdout is not a terminal, the default command prints the text summary
instead of opening the TUI. `--json` prints JSON instead of opening the TUI.
`NO_COLOR` or `--no-color` disables TUI color. A locale whose `LC_ALL` (or
`LANG`, when `LC_ALL` is unset) is set and is not UTF-8 uses ASCII glyphs;
an empty locale stays Unicode.

### Path or repo

`--path` always reads a directory and never consults the hub. `--repo`
always reads an HF repo id and never stats the working directory. The two
flags cannot be combined with each other or with a positional target.

A bare `org/name` that is also a directory in the current working directory
is ambiguous. The command exits 2 and does not open either one. The check
uses the repo id with `@ref` removed, so `org/name@main` is ambiguous when
`./org/name` is a directory. A broken symlink named like `org/name` is
ambiguous too and is not looked up in the hub. Pass `--path` or `--repo`
to say which you mean. An absolute path, a `./` or `../` prefix, or a
trailing slash is a directory even when the string looks like a repo id,
and a missing directory does not fall through to the hub. A bare `org/name`
that is not a local directory is still a repo id. A leading `~` is not
expanded; pass an absolute path or a path relative to the working directory.
An empty target, or `--path=` / `--repo=` with an empty value, is a usage
error (exit 2).

The library helper `hfcache.Resolve` still prefers an existing directory.
The CLI does not use it for user input; it uses `hfcache.ResolveTarget`.

### Exit codes

| Code | Meaning |
| --- | --- |
| 0 | Inspected, and no check failed. Warnings are fine unless `--strict`. The interactive TUI exits 0. When stdout is not a terminal, the default command prints the text summary and uses the inspect exit codes. |
| 1 | Inspected, and a check failed, or a warning under `--strict`. The summary is still printed. The non-TTY fallback uses this code too. |
| 2 | Usage: unknown flag, missing or empty target, empty `--path` or `--repo`, invalid repo id, or an ambiguous path/repo id. |
| 3 | Could not inspect: not found, not a directory, GGUF-only, or no weight files. GGUF and empty directories still print a summary. |

### Keys

`tab` / `shift+tab` or `1`-`4` switch screens. In a tree, `←` / `→` and
`h` / `l` fold a row; on every other screen they switch tabs.
On Config, `space` toggles the selected node, `p` jumps to its parent,
`e` expands (stopping at 100,000 visible rows and leaving arrays over
1,000 closed), `E` collapses back to the default, and `+` / `-` open or
close one depth. `/` searches keys and values (`k:` or `v:` to limit
which), `tab` inside that input switches to filter, and `n` / `N` step
through matches. `y` copies the jq path and `Y` copies the value, both
through OSC 52. `r` toggles the sanitized raw text.
`↑` / `↓` scroll or select. On Layers, `enter` opens a group and `esc` or
backspace goes back. `esc` never quits: it closes help, the command palette,
or a disclosure, then leaves a drill-down. `?` opens the key list. `q`
closes help when help is open, and otherwise quits. `ctrl+c` always quits.
`:` or `ctrl+k` opens the command palette. `w` moves focus between panes,
`<` and `>` resize the split, `z` zooms the focused pane, and `m` toggles
the mouse. The mouse is on unless you pass `--no-mouse` or set
`MODEL_INSPECTOR_MOUSE=0`. Hold Shift to select text while the mouse is on.
On Scan, `c` asks before reading file contents. That reader is not in this
version, and the status line says code and pickle contents arrive in a
later version. The window must be at least 60×16; below that the screen
says so and only `q` works.

`MODEL_INSPECTOR_THEME` is `dark` or `light`. `MODEL_INSPECTOR_MOTION=0`
skips the short storage fill-in. `NO_COLOR` or `--no-color` drops colour.

### Screens

The first row is the wordmark and the tabs. The last row is the status
bar: mode, model name, hints, format, size, quant, scan verdict, and
whether the read is headers-only. A missing or unreadable shard sets the
same incomplete flag as a bad config: the status line, the text summary,
JSON `partial`, and the inspect exit code all follow it.
GGUF replaces the format and params with "not supported yet". The active
tab is a filled pill, and bracketed when colour is off. The Scan tab badge
follows the overall result.

1. **Identity**: repo and revision (or the directory, with an explicit
   empty revision), architecture when `config.json` has `model_type`, MoE,
   on-disk size, estimated params, quantization, a storage bar, callouts,
   a layer heatmap on one shared log2 scale, and a weights-only memory
   estimate. A lookup-table memory line appears when those
   tables are at least 1 GiB. A PLE n-gram table split across
   `.shard_N` weights (plus each piece's scales and biases) is one logical
   table for that line and for the n-gram callout. Pieces must share a dtype
   and a column count, and `shard_01` is the same piece as `shard_1`. The
   Params line sums those pieces before deciding whether one table is at
   least 10% of the estimate. An incomplete inspection marks the memory
   line and the n-gram callout as incomplete. This is not a load of the weights.
2. **Config**: key architecture, attention, MoE, quant, vision, and MTP
   fields (root, then `text_config`, `llm_config`, or `language_config`;
   vision only from `vision_config`) beside an ordered `config.json` tree.
   Enter on a field jumps to that node. The tree keeps file order and
   number literals, pages large arrays, and can show the sanitized raw
   text. A missing file, an unreadable file, a file over 64 MiB, a file
   that did not parse, and a tree over 250,000 nodes each get their own card.
3. **Layers**: tensors grouped by decoder layer (`...layers.N.`) plus
   embeddings, ngram (the PLE n-gram table, even when stored under
   `layers.N.ple.ple_embedding`), norms, hyper_connection (the model-level
   mixer, including `hc_norm`), lm_head, mtp, vision, and other. Enter
   shows each tensor's name, dtype, shape, bytes, and role (`q4`, `qscales`,
   `qbiases`). The Layers column layout itself is unchanged in this version.
4. **Scan**: structural checks, with a one-line reason on expand.
   **This is not a malware or behavior scan.** The disclaimer says so.
   Checks: shard headers parse; files present (including a shard the index
   does not name); offsets and sizes (bounds, dtype×shape, overlap, gaps,
   trailing bytes); index matches headers; code-executing formats by file
   name (including `.h5`, `.keras`, `.nemo`, `.dill`, `.mar`, `.tar`, `.tgz`, and compound
   suffixes such as `.bin.gz`, `.tar.gz`, `.pt.zip`, `.ckpt.zip`, `.pth.zip`, `.bin.zip`, and `.pth.tar`) and by top-level `auto_map` read from
   config files (warn, or fail under `--strict`; a config file that exists
   but cannot be read or parsed warns instead of passing as none); code and pickle contents (not
   run); hashes vs revision (planned); config vs tensor shapes (planned).
   Skip and planned do not turn a pass into a failure. An empty directory
   or GGUF-only directory is not reported as a pass.

"Params" counts packed quantized weights at their dequantized size and
excludes quantization scales and biases. The JSON document is described in
[docs/json-schema.md](docs/json-schema.md).

## Layout

```
cmd/model-inspector     CLI entry (tui / inspect)
internal/hfcache        repo id / path -> snapshot dir
internal/safetensors    header reader
internal/safeopen       single-fd regular-file reads with a size cap
internal/sanitize       terminal-safe text for file-derived strings
internal/model          loader, grouping, quant inference, config fields
internal/report         schema_version 1 JSON and the text summary
internal/scan           structural checks (+ planned stubs)
internal/tui            Bubble Tea screens
internal/version        tool name and version written into JSON
```

## Roadmap

- GGUF support (header + tensor info parsing)
- Hash / provenance verification against the HF commit (LFS sha256)
- Code and pickle contents (names are flagged today; contents are not read)
- Config vs tensor-shape checks
- Behavior probes against a served copy (not a scan check)
- Agent / tool config inspection (chat template, tool-call format, generation config)
- Offload planner: given a VRAM/unified-memory size, plan which layers/experts fit
