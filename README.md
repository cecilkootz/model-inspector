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
make test   # go vet ./... && go test -race -count=1 ./...
go build -o bin/model-inspector ./cmd/model-inspector
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

`tab` / `shift+tab`, `←` / `→`, `h` / `l`, or `1`-`4` switch screens.
`↑` / `↓` scroll or select. On Layers, `enter` opens a group and `esc` or
backspace goes back. `esc` never quits: it closes help, then leaves a
drill-down, then does nothing. `?` opens the key list; `q` quits.
`ctrl+c` always quits. On Scan, `c` shows a footer notice that code and
pickle contents arrive in a later version. Filter, sort, and raw-JSON keys
are listed in help as later and do nothing. The window must be at least
60×16; below that the screen says so and only `q` works.

### Screens

The header is the model name, format, on-disk size, estimated params, and
quantization. A partial or unreadable shard marks the size incomplete.
GGUF replaces the format and params with "not supported yet". The active
tab is bracketed. The Scan tab badge follows the overall result.

1. **Identity**: repo and revision (or the directory, with an explicit
   empty revision), architecture, MoE, on-disk size, estimated params,
   quantization, a storage bar, a weights-only memory estimate, callouts,
   then the dtype table. A lookup-table memory line appears when those
   tables are at least 1 GiB. This is not a load of the weights.
2. **Config**: key architecture, attention, MoE, and MTP fields (root or
   `text_config`) plus the sanitized `config.json`. A missing file, an
   unreadable file, and a file that did not parse each get their own line.
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
   name (including `.h5`, `.keras`, `.nemo`, `.dill`, `.mar`, and compound
   suffixes such as `.bin.gz`) and by top-level `auto_map` read from
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
