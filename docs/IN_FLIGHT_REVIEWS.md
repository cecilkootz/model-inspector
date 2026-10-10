# In-flight reviews

Status as of 2026-10-10. #16 lands last, after Layers.

## Merged on main

#1 through #12, and #17.

## Open

- **#13** — fuzz nits. Needs a rebase.
- **#14** — Notef `formatLit` and the cursor offset. QA approved. Rebased on main at `4056b47`.
- **#15** — Layers tree. In QA. Branch `cursor/v2-layers-tree-67c7`, head `48624ba`, base main.
- **#16** — Scan screen. In QA. Branch `cursor/v2-scan-bac1`, stacked on #15.

## Next

- A metadata-sanity warn-only PR. The Scan card stays `not run: planned` until that lands.
- Design's fidelity follow-ups for Layers and Scan.
- The PR #4 and #6 Makefile nits.
- Replace the synthetic Config golden with the real Vontra `config.json` once Matthew supplies it.
