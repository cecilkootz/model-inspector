# In-flight reviews

Status as of 2026-10-11.

PRs moved from Cursor Origin to GitHub. Numbers below are GitHub PRs unless marked Cursor Origin.

## Merged on main

Cursor Origin #1 through #17, then GitHub #1 through #12: macOS test fixes, the `github.com` module path, CI (`make test` on Linux and macOS, `make fuzz` on both, a nightly longer fuzz), the green-CI merge rule, exact-case HF cache lookup, the metadata-sanity check, the names-only code and pickle content scan (`internal/codescan`), the agent-surface check over chat template, tokenizer and generation files (`internal/agentcfg`), and the jsonx fix that rejects signed NaN/Infinity.

## Open

- **#13**: bare `make` prints the target list.

## Next

- **`--path` with a wrong-case repo folder.** #7 fixed typed repo ids, but `--path HUB/models--ORG--NAME/snapshots/<c>` on APFS still reports `ORG/NAME`. Proposed: inspect the directory, report a repo id only when the folder name matches exactly. `TestP3_Targets` skips on case-insensitive filesystems until then.
- **Delete `model.MetadataMismatches` / `MetadataCompared`** in `internal/model/explore.go`. Unused since #8, converts config values unsafely, and mutates partial state.
- **Scan screen shows "not compared" notes under PROBLEMS** for a passing metadata-sanity card. Details need a note/problem split.
- **`LayersV2` fixture config disagrees with its tensors** (embedding width 32 vs `hidden_size` 8; `lm_head` rows 8 vs `vocab_size` 32). No golden shows its scan state yet.
- **Audio-tower `layers.N` tensors** would count as decoder layers in metadata-sanity. Classify them like vision.
- **Real Config golden.** `fixture.ConfigQwenLike` is synthetic. Replace it with the real Vontra `config.json` once Matthew supplies it.
- **Scan screen at 140x40 with the strict banner** clips the last card's summary row by one line, and the footer hint line is dropped when the pane is exactly full. Layout follow-up for design.
- **Structured `agent` JSON section** for the agent-surface facts (tool-call format, context, stop tokens). The check's details carry them as strings today.
- **CI minutes.** macOS jobs bill at 10x; a push costs about 36 of the 2,000 free minutes and the nightly fuzz about 1,650 a month. Proposed: drop the per-PR macOS fuzz job and run the long fuzz weekly.
- **Provenance and base-vs-quant diff**, then layer anatomy, then behaviour probes against a served copy. See the README roadmap.
