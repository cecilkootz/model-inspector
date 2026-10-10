# In-flight work: PR #14 and PR #15

Status as of 2026-10-10. QA was paused when credits ran out. This file records where the review of each PR stands so it can be picked up later.

## PR #14: follow-up to the PR #12 sanitizer nits

- Link: https://cursor.com/codebase/cecilkootz/model-inspector/pull/14
- Base: PR #12 at `27829de`.
- **Approved at `d3edc89`** (QA review `rev_01m4jv5ppwezysgfz04veqrtzq`). Safe to merge at that commit.
- **Not reviewed: `ed92038`**, the latest head. It's a small commit from Inspector Coder covering one cosmetic nit: Notef no longer calls String/Error/Format/GoString on a typed nil pointer and prints a literal `<nil>` instead.

What PR #14 delivers (verified at `d3edc89`):
- A1: named number or bool types with their own String/Error/Format/GoString methods are sanitized.
- A2: `sanitize.Text` and `Program` arguments (including `*Text` and `[]Text`) pass through once, with no double sanitizing.
- A3: Notef's format parameter is the unexported type `formatLit`. The AST test flags non-call uses, parenthesized calls, in-package conversions, and any `//go:linkname` that targets `sanitize`. The comment documents the known deliberate bypasses: linkname, generic call-through, reflect and unsafe.
- B1: the NO_COLOR Layers cursor marker tracks the viewport offset itself, with no reflection into bubbles. The `layers.80x24.colorless.down5` golden is the tripwire for bubbles upgrades.
- E1: nil is decided from the value, not from the formatted text, so inputs like `"\r<nil>"`, `"\f<nil>\v"` and `"\u0085<nil>\u2028"` are tokenized.

Verified at `d3edc89`:
- `go vet -printf.funcs=Notef` is clean.
- 380 race tests pass, with 76.6% coverage.
- Fuzz runs are clean.
- Hostile sweeps over the TUI, the CLI and the NO_COLOR cursor found 0 raw and 0 double-sanitized output.

To finish:
- [ ] Re-check `ed92038` with a diff against `d3edc89`:
  - a typed nil pointer whose method panics prints `<nil>`
  - a typed nil pointer whose method returns hostile text produces no raw bytes
  - non-nil pointers still call their methods and are sanitized
- [ ] Rerun the Notef probe matrix (16 argument types x all verbs), vet with `-printf.funcs=Notef`, the race tests and the goldens.

## PR #15: v2 Layers tree (spec 03)

- Link: https://cursor.com/codebase/cecilkootz/model-inspector/pull/15
- Head `901cf2b`, stacked on PR #11's branch. +5966/-219 across 28 files.
- **Not reviewed.** The QA review was stopped before it produced results.
- It reuses `internal/model/explore.go` from the paused Explore branch: LayerInfo, ExpertCount, StackedExpertDim, LogicalTensors and ShapeText. LayerInfo is dense only for small spans; sparse or huge indices go through LayerInfoBy.
- `sanitize.Program` is **duplicated** from the PR #12 trust-split branch. Check that it doesn't diverge from #12/#14. Prefer rebasing onto #12/#14 and deleting the duplicate.
- Claimed performance at 50k groups / 200k tensors: cached view about 2.5 µs, keys and mouse under 1 µs, resize 3.5 µs.

Review plan (block only on sanitizer bypasses, crashes or panics, OOM or hangs, and wrong numbers):
- [ ] Hostile tensor names in tree rows, breadcrumbs, details, expand labels and jump feedback: ESC/OSC/CSI/C1/bidi/zero-width characters, `<`, huge names and invalid UTF-8.
- [ ] Absurd layer indices: 0, 4096, 4097, near MaxInt, leading zeros, sparse (0 and 10^9), and many distinct indices. Check the dense/sparse threshold and consistency with the PR #10 4096 cap and the `DroppedLayers`/partial logic.
- [ ] Absurd dims: zero dims, rank caps, and overflow in ShapeText, LogicalTensors, StackedExpertDim and ExpertCount.
- [ ] The expand cap on huge MoE models (1000 layers x 512 experts, 200k tensors): bounded time and memory, with the cap message shown.
- [ ] The 2-D heatmap with sparse or huge layers: no allocation sized by the max index, and no NaN/Inf or negative widths.
- [ ] `jump: #` input: non-digits, overflow, negative numbers, `+`, unicode digits, empty input, long pastes, control characters, and jumps to dropped or sparse layers.
- [ ] Every new `Program`, `ui.L` or `Notef` call takes only constant arguments, and no `<U+003C>` artefacts show up in static text.
- [ ] Uncached performance at 50k groups / 200k tensors, expand-all and the heatmap, checked for quadratic behaviour.
- [ ] Prior repro suites and the `FuzzUpdate` harness (random key, mouse and resize sequences), with NO_COLOR unset.

## Merge order (QA view)

- PR #10 first: it fixes the crash, OOM and slow-redraw bugs that PR #8 left on main.
- PR #9, then #13.
- PR #11 (approved at `e9d00cd`).
- PR #12 (approved at `27829de`).
- PR #14 (approved at `d3edc89`; `ed92038` is pending re-check).
- PR #15 after review.
