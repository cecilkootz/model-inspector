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

Text output and the TUI scan screen state that 1 of 4 checks is implemented.

It never writes into the Hugging Face cache. Inspecting a ~106 GiB model takes
~15 ms and ~11 MB RSS.

## Install

```sh
go env -w GOPRIVATE=origin.cursor.com
go install origin.cursor.com/cecilkootz/model-inspector/cmd/model-inspector@latest
```

`go install` from `origin.cursor.com` needs `GOPRIVATE=origin.cursor.com` and
git credentials for that host. An SSH remote works
(`git@origin.cursor.com:cecilkootz/model-inspector.git`). The module path
stays `origin.cursor.com/cecilkootz/model-inspector`.

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

`<path-or-repo>` is a model/snapshot directory, or an HF repo id
(`org/name`, optionally `@ref` or `@commit`) resolved through the hub cache
(`$HF_HUB_CACHE`, `$HF_HOME/hub`, or `~/.cache/huggingface/hub`;
override with `--hub-dir`).

```sh
# Interactive TUI
bin/model-inspector Vontra/Qwen3.8-Flash-Next-MLX-4bit-MTP

# Headless summary
bin/model-inspector inspect Vontra/Qwen3.8-Flash-Next-MLX-4bit-MTP
bin/model-inspector inspect /path/to/snapshot --json
```

Keys: `tab`/`shift+tab`, `←`/`→` or `1`-`4` switch screens; `↑`/`↓` scroll or select;
`enter` drills into a layer group, `esc` goes back; `q` quits.

### Screens

1. **Identity**: path, repo id + revision, format, on-disk size, file/shard
   count, quantization (bits, group size, inferred bits per packed weight),
   dtype breakdown.
2. **Config**: key architecture/MoE/MTP fields (root or `text_config`) plus
   the raw `config.json` in a scrollable viewport.
3. **Layers**: tensors grouped by decoder layer (`...layers.N.`) plus
   embeddings, ngram (the PLE n-gram table, even when stored under
   `layers.N.ple.ple_embedding`), norms, hyper_connection (the model-level
   mixer, including `hc_norm`), lm_head, mtp, vision, and other. Per group:
   tensor count, params, bytes, whether routed experts are present. Enter
   shows each tensor's name, dtype, shape, bytes, and role (`q4`, `qscales`,
   `qbiases`).
4. **Scan**: structural checks. **This is not a malware scan.** 1 of 4 checks
   is implemented. Implemented:
   an unreadable index fails the check; a missing or empty weight_map fails
   it without being called unreadable; every tensor in the index exists in
   the referenced shard header; shard names must be local files; every
   `.safetensors` file on disk is checked, including ones the index omits;
   every header byte range is valid, matches dtype×shape, starts at offset 0,
   has no gaps or overlaps, and ends at the data section. Planned: hash vs
   pinned revision, metadata sanity, behavior probes.

"Params" counts MLX packed quantized weights at their dequantized size and
excludes quantization scales/biases; "stored elements" is the raw count.

A relative model path is resolved against the process working directory.
An entry on that relative path can shadow the directory you meant. That
case is not handled in this PR; UX PR1's `--path` / `--repo` flags will own it.

## Layout

```
cmd/model-inspector     CLI entry (tui / inspect)
internal/hfcache        repo id / path -> snapshot dir
internal/safetensors    header reader
internal/safeopen       single-fd regular-file reads with a size cap
internal/sanitize       terminal-safe text for file-derived strings
internal/model          loader, grouping, quant inference, config fields
internal/scan           structural checks (+ planned stubs)
internal/tui            Bubble Tea screens
```

## Roadmap

- GGUF support (header + tensor info parsing)
- Hash / provenance verification against the HF commit (LFS sha256)
- Behavior probes against a served copy
- Agent / tool config inspection (chat template, tool-call format, generation config)
- Offload planner: given a VRAM/unified-memory size, plan which layers/experts fit
