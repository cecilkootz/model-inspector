# model-inspector

A terminal UI (Bubble Tea / Bubbles / Lip Gloss) for inspecting local LLM
weights **without loading them**: identity, architecture config, per-layer
tensor breakdown, and a structural integrity check.

> Module path `github.com/mwelch/model-inspector` is a placeholder; change it
> in `go.mod` (and imports) once a real remote exists.

## Safety: headers only

model-inspector never reads or mmaps tensor data. It only:

- `stat`s files (following HF cache symlinks) for sizes,
- reads safetensors headers: the first 8 bytes (little-endian u64 length N)
  plus N bytes of JSON per shard (N capped at 256 MiB),
- reads small JSON files (`config.json`, `model.safetensors.index.json`),
  capped at 64 MiB.

It never writes into the Hugging Face cache. Inspecting a ~106 GiB model takes
~15 ms and ~11 MB RSS.

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
4. **Scan**: structural checks. **This is not a malware scan.** Implemented:
   an unreadable index fails the check; every tensor in the index exists in
   the referenced shard header; every header byte range is valid, matches
   dtype×shape, doesn't overlap, and fits within the file. Planned: hash vs
   pinned revision, metadata sanity, behavior probes.

"Params" counts MLX packed quantized weights at their dequantized size and
excludes quantization scales/biases; "stored elements" is the raw count.

## Layout

```
cmd/model-inspector     CLI entry (tui / inspect)
internal/hfcache        repo id / path -> snapshot dir
internal/safetensors    header reader
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
