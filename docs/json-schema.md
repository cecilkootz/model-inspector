# JSON schema, version 1

`model-inspector inspect --json` writes one JSON object to stdout. The
object is `schema_version` 1. Later versions will bump that number; this
shape stays as written here.

Encoding uses `encoding/json` with HTML escaping off and two-space indent.
Every v1 key is present. Empty slices are `[]`, not `null`. Pointer fields
with no value are `null`. `code` is always `null` in this version.
`groups[].routed_experts` is always `null` until a later version fills it.

Strings are the parsed file values. They are not passed through the
terminal sanitizer, so a machine still sees the original characters after
`json.Unmarshal`. `encoding/json` writes C0 as `\uXXXX`. `sanitize.EscapeJSON`
does the same for C1, bidi, and other invisible characters inside strings.
`elapsed_ms` is the wall time of the inspection and is not stable across runs.
`partial` is true when the inspection did not finish: a walk cap, a per-entry
walk error, an unreadable or unparseable config, an index directory that
could not be listed, a display counter that overflowed, an index
`metadata.total_size` that is not a non-negative int64, or a shard that is
missing, unreadable, or not a local file name. The same flag drives the
TUI "(incomplete)" marker, the text summary, and a failing scan (exit 1).
It is false when the inspection completed. The field is additive, so
`schema_version` stays 1.

## Top level

| Field | Type | Meaning |
| --- | --- | --- |
| `schema_version` | number | Always `1`. |
| `tool` | object | `name` (`model-inspector`) and `version` (`0.1.0`). |
| `options` | object | `strict` (bool) and `code` (always `false`; content reads are not in this version). |
| `source` | object | Where the directory came from. See below. |
| `format` | object | `name`, `variant`, `supported`. |
| `files` | object | On-disk files and shards. |
| `architecture` | object | Counts derived from config and tensor names. |
| `config` | object | `present` and the extracted `fields`. |
| `quantization` | object or null | Present when config names a quantization block. |
| `tensors` | object | Header totals. |
| `dtypes` | array | One entry per dtype, sorted by dtype name. |
| `storage` | array | Full storage breakdown. The TUI bar merges this to four segments; JSON does not. |
| `memory` | object | Weights-only estimate. |
| `groups` | array | Tensor groups in display order. |
| `host_capable_tensors` | array | Lookup tables the `lookup_table_v1` rule flagged. Primaries only; `bytes` includes sibling `.scales` and `.biases`. |
| `callouts` | array | Identity findings, most serious first. |
| `scan` | object | Structural checks. |
| `code` | null | Reserved for code and pickle contents. |
| `load_warnings` | array of string | Loader warnings. Empty when there are none. |
| `partial` | bool | True when the inspection is incomplete. Schema version stays `1`; this field is additive. |
| `elapsed_ms` | number | Milliseconds spent loading and scanning. |

## `source`

| Field | Type | Meaning |
| --- | --- | --- |
| `path` | string | Directory that was read, after symlink resolution. |
| `repo_id` | string or null | HF repo id when the path is a hub snapshot or `--repo` resolved one. |
| `revision` | string or null | Full commit hash when one is known. The text UI shortens this to 7 characters. |
| `revision_source` | string | `hf_cache_ref` when the directory came from the hub cache and a revision is set. `path` when a revision was inferred from a local snapshot path. `none` when there is no revision. |

## `format`

`name` is `safetensors`, `gguf`, or `unknown`. `variant` is the safetensors
metadata format (for example `mlx`) or `""`. `supported` is true only for
safetensors. GGUF and an empty directory are not supported: the summary is
still printed, and the process exits 3.

## `files`

| Field | Type | Meaning |
| --- | --- | --- |
| `count` | number | Files walked. |
| `total_bytes` | number | Sum of regular-file sizes. |
| `shard_count` | number | Safetensors shards considered, including missing ones named by the index. |
| `index_file` | string or null | Index file name, or null when there is no usable index. |
| `shards` | array | `name`, `bytes`, `readable`, `error` (string or null). |

## `architecture`

| Field | Type | Meaning |
| --- | --- | --- |
| `model_type` | string or null | From config. |
| `layer_count` | number | Numbered decoder layers found in tensor names. |
| `layers_with_routed_experts` | number | Of those, how many contain routed expert tensors. |
| `layer_type_counts` | array | `{type, count}` from config `layer_types`, else the `full_attention_interval` rule, else tensor names. |
| `routed_experts` | number or null | `num_experts`, `num_local_experts`, or `n_routed_experts`. |
| `experts_per_token` | number or null | `num_experts_per_tok`. |
| `has_mtp` | bool | An MTP group is present. |
| `has_vision` | bool | A vision group is present. |

`full_attention_interval` marks layer `i` as `full_attention` when
`(i+1) % interval == 0`, and `linear_attention` otherwise.

## `config.fields`

Each field is `{key, path, group, value}`. `value` is the JSON value from
config. `group` is `architecture`, `attention`, `moe`, `mtp_vision`, or
`other`. The Config screen does not group by these names yet.

## `quantization`

`config_key`, `bits`, `group_size`, `mode`, `override_count`, and
`bits_histogram` (`{bits, tensors}`, sorted by bits). Null when config has
no quantization block. Header text omits the group size when it is 0.

## `tensors`

`count`, `stored_elements`, `params_est`, `params_note`, `bytes`, and
`index_total_size_bytes` (number or null). Packed quantized weights are
counted at their dequantized size. Scales and biases are excluded from
`params_est`.

## `storage`, `memory`, `groups`

`storage[]` is `{class, bytes, pct}`. Classes look like `q4_packed`,
`q_scales_biases`, `plain_bf16`, and `other`. `pct` is one decimal, clamped
to the closed range 0 through 100. A saturated byte total cannot push a
class, or the storage legend, past 100%.

`memory` is weights only: `weights_bytes`, `text_only_bytes`,
`host_capable_bytes`, `accelerator_min_bytes`, `unified_memory`, `note`,
`host_capable_note`. `text_only_bytes` is always computed. `unified_memory`
is true for an MLX variant or for darwin/arm64.

`accelerator_min_bytes` is `weights_bytes − host_capable_bytes` when the
host total is exact, and that difference is a lower bound on bytes that
cannot stay in host RAM. When the inspection is partial, or the index
names a piece that was not loaded, `host_capable_bytes` is only a lower
bound. Subtracting it can overstate the accelerator remainder, so it is
not a lower bound. The field is then `0` — the only figure that is still
a lower bound — and the identity screen omits the "≥ … on the
GPU/accelerator" line. The text summary does not print that line.

`groups[]` is `{key, kind, layer, attention_type, tensors, params_est,
bytes, pct_bytes, storage, has_routed_experts, routed_experts, outliers,
host_capable_bytes}`. `pct_bytes` is one decimal, clamped to 0 through 100.
`layer` and `attention_type` are null for non-layer
groups. `outliers` is a subset of `size`, `precision`, `structural`.
`routed_experts` is null.

## `host_capable_tensors` and `callouts`

A host-capable tensor is a 2-D table with at least 1024 rows whose name
matches `lookup_table_v1` (embed-style names, including n-gram tables, and
not projections, norms, or heads). The match folds ASCII case only;
`strings.ToLower` is not used to slice a tensor name, because some runes
change length when lowercased. Tied token embeddings are excluded when
`tie_word_embeddings` is true and there is no `lm_head`. Pieces named
`stem.shard_<N>` (the PLE n-gram table is `...ngram_embedding.shard_0`
through `shard_127`) are one logical table when they share a dtype and a
column count: each piece is tested for the row rule, then bytes (including
that piece's `.scales` and `.biases`) and dequantized params are summed.
Shard indexes are numeric, so `shard_01` and `shard_1` are one piece.
Orphan `.scales` and `.biases` are not tables, and each tensor is counted
once. Pieces that disagree in dtype or column count are not merged.
One `host_capable_tensors` entry is emitted, named with the shared stem.
Names are unique. A tensor whose name is exactly that stem stays under
the stem; the summed table is then named `stem (sharded)`, and a further
clash gets a numeric suffix. Piece matching still uses the stem.
When `partial` is true, or the index names a piece that was not loaded,
the ⌂ memory line and the n-gram callouts say the figure is incomplete.

`callouts[]` is `{kind, group, tensor, message}`. `group` and `tensor` are
null when they do not apply. `kind` is `size_outlier`, `host_capable`,
`big_tensor`, `precision_outlier`, `structural`, or `load_warnings`.

## `scan`

| Field | Type | Meaning |
| --- | --- | --- |
| `strict` | bool | True when `--strict` was set. |
| `overall` | string | Worst effective status of the executed checks. `skip` and `planned` do not lower a pass. When no shard was inspected and nothing warned or failed, `overall` stays `skip` (an empty directory or GGUF-only directory is not a pass). |
| `disclaimer` | string | The structural-only sentence. This is not a malware or behavior scan. |
| `counts` | object | Raw `{pass, warn, fail, skip, planned}`. `--strict` does not change these numbers. |
| `checks` | array | One object per check. |

Each check is `{id, name, status, effective_status, summary, details}`.
`status` is the raw result. `effective_status` equals `status`, except
under `--strict`, where a raw `warn` becomes `fail`, and except the v1.2
rule: a structural check (`header-parse`, `file-presence`, `offsets-bounds`,
`index-shard-consistency`) that skipped while shards exist is an effective
`warn` and its summary gains " (skipped although shards exist)". A single
shard with no index file stays `skip`. `planned` is never promoted. A
promoted skip stays an effective `warn` under `--strict`, because strict
only promotes a raw `warn`. `details` is `[]` when there is nothing to list.

Check ids, in order:

| id | What it uses |
| --- | --- |
| `header-parse` | Safetensors headers that were actually read. |
| `file-presence` | Missing, unreadable, or non-local shards, an incomplete inspection (`Partial`), and `.safetensors` files the index does not name. |
| `offsets-bounds` | `safetensors.CheckLayout`: bounds, dtype×shape, overlap, gaps, trailing bytes. |
| `index-shard-consistency` | Index against shard headers. |
| `code-exec-formats` | File names (`.bin`, `.pt`, `.pth`, `.ckpt`, `.pkl`, `.pickle`, `.joblib`, `.npy`, `.pyc`, `.py`, `.h5`, `.keras`, `.nemo`, `.dill`, `.mar`, `.tar`, `.tgz`, and `.bin.gz` / `.pt.gz` / `.pkl.gz` / `.tar.gz` / `.pt.zip` / `.ckpt.zip` / `.pth.zip` / `.bin.zip` / `.pth.tar`) and top-level `auto_map` read from `config.json`, `tokenizer_config.json`, `processor_config.json`, and `preprocessor_config.json`. Warns. `--strict` makes that a failure. A file that exists but cannot be read or parsed warns `could not check auto_map in <file>` and does not pass as none. The check does not import or execute anything. |
| `code-content` | Always `skip`. |
| `hash-vs-revision` | Always `planned`. |
| `metadata-sanity` | Config fields against tensor shapes: `hidden_size` (or `word_embed_proj_dim` / `embedding_size` when present) against the token embedding width, `vocab_size` against the token embedding and `lm_head` rows, and `num_hidden_layers` against the distinct decoder indices in tensor names (a count of `num_hidden_layers + num_nextn_predict_layers` also matches). Fields are read from `text_config`, `llm_config`, or `language_config` before the root. MLX packed weights are compared at their dequantized width; an integer-dtype embedding with no scales is not compared. A mismatch, or a field that is not a whole number in range, warns; it never fails. `skip` when there are no shards, no usable `config.json`, or nothing to compare. A decoder index above 4096 makes this check `fail` with name `Layer index`, and no comparison is made. |

Behavior probes are not a check. They remain a roadmap item.
