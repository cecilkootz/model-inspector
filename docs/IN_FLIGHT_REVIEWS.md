# In-flight reviews

Status as of 2026-10-11.

PRs moved from Cursor Origin to GitHub. Numbers below are GitHub PRs unless marked Cursor Origin.

## Merged on main

Cursor Origin #1 through #17, then GitHub #1 through #8: macOS test fixes, the `github.com` module path, CI (`make test` on Linux and macOS, `make fuzz` on both, a nightly longer fuzz), the green-CI merge rule, exact-case HF cache lookup, and the metadata-sanity check.

## Open

- **code-content** (branch `cecilkootz/code-content-scan`): names-only static scan of `.py` imports and pickle `GLOBAL` opcodes in `internal/codescan`. Replaces the `not run` stub. Fails on dangerous pickle imports or an `auto_map` file that does network, process or code loading.
- **agent-surface** (branch `cecilkootz/agent-surface`): warn-only check over the chat template, tokenizer and generation config. Tool-calling format, reasoning markers, stop-token agreement, added-token overflow, and tamper heuristics on template text. Never renders the template.

Both edit `internal/scan/scan.go`; the second to land rebases and regenerates goldens.

## Next

- **`--path` with a wrong-case repo folder.** #7 fixed typed repo ids, but `--path HUB/models--ORG--NAME/snapshots/<c>` on APFS still reports `ORG/NAME`. Proposed: inspect the directory, report a repo id only when the folder name matches exactly. `TestP3_Targets` skips on case-insensitive filesystems until then.
- **Delete `model.MetadataMismatches` / `MetadataCompared`** in `internal/model/explore.go`. Unused since #8, converts config values unsafely, and mutates partial state.
- **Scan screen shows "not compared" notes under PROBLEMS** for a passing metadata-sanity card. Details need a note/problem split.
- **`LayersV2` fixture config disagrees with its tensors** (embedding width 32 vs `hidden_size` 8; `lm_head` rows 8 vs `vocab_size` 32). No golden shows its scan state yet.
- **Audio-tower `layers.N` tensors** would count as decoder layers in metadata-sanity. Classify them like vision.
- **Real Config golden.** `fixture.ConfigQwenLike` is synthetic. Replace it with the real Vontra `config.json` once Matthew supplies it.
- **Provenance and base-vs-quant diff**, then layer anatomy, then behaviour probes against a served copy. See the README roadmap.
