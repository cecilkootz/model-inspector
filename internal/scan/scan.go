// Package scan runs cheap structural checks on a loaded model.
// This is not a malware or behavior scan.
package scan

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/safetensors"
)

type Status string

const (
	StatusPass    Status = "pass"
	StatusWarn    Status = "warn"
	StatusFail    Status = "fail"
	StatusSkip    Status = "skip"
	StatusPlanned Status = "planned"
)

// Check IDs. The first four present the hardening checks under separate ids.
const (
	CheckHeader   = "header-parse"
	CheckFiles    = "file-presence"
	CheckOffsets  = "offsets-bounds"
	CheckIndex    = "index-shard-consistency"
	CheckCodeExec = "code-exec-formats"
	CheckCode     = "code-content"
	CheckHash     = "hash-vs-revision"
	CheckMetadata = "metadata-sanity"
)

// Disclaimer is shown wherever scan results are displayed.
const Disclaimer = "Structural integrity checks only: file completeness, header well-formedness and internal consistency. Not a malware or behavior scan; does not establish what the weights do or who produced them."

// Options changes how statuses are reported. It does not re-run validation.
type Options struct {
	// Strict promotes every warn to an effective fail. Raw Status is unchanged.
	Strict bool
}

type Check struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Status          Status   `json:"status"`
	EffectiveStatus Status   `json:"effective_status"`
	Summary         string   `json:"summary"`
	Details         []string `json:"details,omitempty"`
	// Duration is the attributable loader phase plus this check's own time.
	// It is omitted from JSON so goldens stay deterministic.
	Duration time.Duration `json:"-"`
}

// Counts is the raw status tally. Strict does not change it.
type Counts struct {
	Pass    int `json:"pass"`
	Warn    int `json:"warn"`
	Fail    int `json:"fail"`
	Skip    int `json:"skip"`
	Planned int `json:"planned"`
}

type Result struct {
	Disclaimer string  `json:"disclaimer"`
	Overall    Status  `json:"overall"`
	Strict     bool    `json:"strict"`
	Checks     []Check `json:"checks"`
}

// Run executes the checks with default options.
func Run(m *model.Model) Result { return RunOptions(m, Options{}) }

// RunOptions executes the checks. Validation comes from the loader and
// safetensors.CheckLayout; this function only sorts those results into
// check ids and applies the strict promotion.
func RunOptions(m *model.Model, opt Options) Result {
	// One pass: host-capable warnings and any Partial they set are part of
	// the verdict. HostCapable caches, so a later render does not append more.
	if m != nil {
		m.HostCapable()
	}
	checks := []Check{
		timedPhase(m, CheckHeader, func() Check { return headerParse(m) }),
		timedPhase(m, CheckFiles, func() Check { return filePresence(m) }),
		timedPhase(m, CheckOffsets, func() Check { return offsetsBounds(m) }),
		timedPhase(m, CheckIndex, func() Check { return indexConsistency(m) }),
		timed(func() Check { return codeExecFormats(m) }, 0),
		timed(func() Check { return codeContent(m) }, 0),
		timed(func() Check { return plannedHash(m) }, 0),
		timed(func() Check { return metadataSanity(m) }, 0),
	}
	for i := range checks {
		checks[i].EffectiveStatus = checks[i].Status
		promoteSkip(m, &checks[i])
		if opt.Strict && checks[i].Status == StatusWarn {
			checks[i].EffectiveStatus = StatusFail
		}
	}
	r := Result{
		Disclaimer: Disclaimer,
		Overall:    rollup(checks, len(m.Shards) > 0),
		Strict:     opt.Strict,
		Checks:     checks,
	}
	return r
}

// Count tallies raw statuses.
func (r Result) Count() Counts {
	var c Counts
	for _, ch := range r.Checks {
		switch ch.Status {
		case StatusPass:
			c.Pass++
		case StatusWarn:
			c.Warn++
		case StatusFail:
			c.Fail++
		case StatusSkip:
			c.Skip++
		case StatusPlanned:
			c.Planned++
		}
	}
	return c
}

// Why returns the one-line reason a check exists. Unknown ids return "".
func Why(id string) string {
	switch id {
	case CheckHeader:
		return "A header that is not valid JSON, or a length that does not fit the file, means that shard cannot be mapped."
	case CheckFiles:
		return "A missing shard, a broken link, or a path that leaves the model directory means the checkpoint is incomplete or the index names something outside it."
	case CheckOffsets:
		return "A range that runs past the file or overlaps another tensor means the shard is truncated or was edited after the header was written."
	case CheckIndex:
		return "The index maps tensor names to shards. If it disagrees with the headers, a loader and this tool are not looking at the same bytes."
	case CheckCodeExec:
		return "These formats can run code when some other tool loads the model. This check matches file names and reads top-level auto_map in config files. It does not import or execute anything."
	case CheckCode:
		return "Reading code and pickle contents is not part of this version."
	case CheckHash:
		return "A hash would show whether the bytes match the revision. It is not run, because it would read every weight byte."
	case CheckMetadata:
		return "A loader builds the model from config.json and then fills it from the weights. If the two disagree, it fails or loads something other than what the config describes. A decoder index above 4096 is reported here, not as a missing file."
	default:
		return ""
	}
}

// Rule is what the check compared. Unknown ids and checks with no rule return "".
func Rule(id string) string {
	switch id {
	case CheckHeader:
		return "8-byte length ≤ 100 MiB and ≤ file size; header is one JSON object, no duplicate keys, each tensor has dtype, shape and data_offsets"
	case CheckFiles:
		return "every shard the index names is a regular file inside the snapshot (links only into blobs/); unreferenced .safetensors warn; Partial fails"
	case CheckOffsets:
		return "each range lies inside its data section; end − start = dtype size × elements; no overlap; gaps/trailing bytes and unknown dtypes warn"
	case CheckIndex:
		return "every weight_map entry is found in that shard's header; header tensors not mapped to their shard, and a total_size mismatch, warn"
	case CheckCodeExec:
		return "file names with code-executing suffixes (.bin .pt .pth .pkl .ckpt .h5 .keras .nemo .dill .mar .py, .bin.gz …) and top-level auto_map in config files; nothing opened"
	case CheckCode:
		return "static pickle disassembly and .py import scan; never executes or unpickles"
	case CheckMetadata:
		return "hidden_size = token embedding width; vocab_size = token embedding and lm_head rows; num_hidden_layers = distinct decoder indices (plus num_nextn_predict_layers); text_config, llm_config, language_config before root; MLX packs compared dequantized; mismatches warn"
	default:
		return ""
	}
}

// NotProven is the limit of a check. Unknown ids and checks with no limit return "".
func NotProven(id string) string {
	switch id {
	case CheckHeader:
		return "that tensor values are sensible or finite"
	case CheckFiles:
		return "that files match what the publisher uploaded"
	case CheckOffsets:
		return "that the bytes are the intended values"
	case CheckIndex:
		return "that the index itself is authentic"
	case CheckCodeExec:
		return "that other files carry no risk, or that flagged files are harmful"
	case CheckCode:
		return "that loading these files with another tool is harmless"
	case CheckMetadata:
		return "that the fields not compared agree, or that the config describes how the weights were trained"
	default:
		return ""
	}
}

func timed(fn func() Check, extra time.Duration) Check {
	start := time.Now()
	c := fn()
	c.Duration = time.Since(start) + extra
	return c
}

func timedPhase(m *model.Model, id string, fn func() Check) Check {
	c := timed(fn, 0)
	c.Duration += phase(m, id)
	return c
}

func phase(m *model.Model, id string) time.Duration {
	if m == nil {
		return 0
	}
	switch id {
	case CheckHeader:
		return m.Timings.Headers
	case CheckFiles:
		return m.Timings.Walk
	case CheckOffsets:
		// Layout time is this check's own loop. NoteLayout records it;
		// adding it again would count the pass twice.
		return 0
	case CheckIndex:
		return m.Timings.Index
	default:
		return 0
	}
}

// promoteSkip applies the v1.2 rule: a structural check that skipped while
// shards exist becomes an effective warn. Raw Status stays skip. Planned
// checks are never promoted. index-shard-consistency with exactly one shard
// and no index file stays skip.
func promoteSkip(m *model.Model, c *Check) {
	if m == nil || c == nil || c.Status != StatusSkip || len(m.Shards) == 0 {
		return
	}
	switch c.ID {
	case CheckHeader, CheckFiles, CheckOffsets, CheckIndex:
	default:
		return
	}
	if c.ID == CheckIndex && len(m.Shards) == 1 && m.Index == nil && m.IndexErr == nil {
		return
	}
	c.EffectiveStatus = StatusWarn
	c.Summary += " (skipped although shards exist)"
}

// rollup is the worst effective status of executed checks.
// A raw skip or planned check does not change it, unless v1.2 promoted
// that skip to an effective warn or fail. When no shard was inspected and
// nothing warned or failed, the result stays skip: an empty directory
// or a GGUF-only directory is not reported as a pass.
func rollup(checks []Check, inspected bool) Status {
	overall := StatusPass
	executed := false
	for _, c := range checks {
		promoted := (c.Status == StatusSkip || c.Status == StatusPlanned) &&
			(c.EffectiveStatus == StatusWarn || c.EffectiveStatus == StatusFail)
		if (c.Status == StatusSkip || c.Status == StatusPlanned) && !promoted {
			continue
		}
		executed = true
		switch c.EffectiveStatus {
		case StatusFail:
			overall = StatusFail
		case StatusWarn:
			if overall != StatusFail {
				overall = StatusWarn
			}
		}
	}
	if !executed || (!inspected && overall == StatusPass) {
		return StatusSkip
	}
	return overall
}

const maxDetails = 25

// incompleteInspection is the file-presence line for Partial. Partial now
// covers walk caps, unreadable entries, a dangling config, counter overflow,
// and an unusable index total_size, so the text does not say "safety cap".
const incompleteInspection = "inspection is incomplete (see warnings)"

func headerParse(m *model.Model) Check {
	c := Check{ID: CheckHeader, Name: "Shard headers parse"}
	var problems []string
	readable, attempted := 0, 0
	for _, s := range m.Shards {
		if presenceErr(m, s) {
			continue
		}
		attempted++
		if s.Header == nil {
			problems = append(problems, fmt.Sprintf("%s: unreadable: %v", s.Name, s.Err))
			continue
		}
		readable++
	}
	if attempted == 0 {
		c.Status, c.Summary = StatusSkip, "no safetensors shards found"
		return c
	}
	if len(problems) == 0 {
		c.Status = StatusPass
		c.Summary = fmt.Sprintf("%d/%d headers read (length + JSON)", readable, attempted)
	} else {
		c.Status = StatusFail
		c.Summary = fmt.Sprintf("%d of %d headers could not be read", len(problems), attempted)
	}
	c.Details = capDetails(problems)
	return c
}

func filePresence(m *model.Model) Check {
	c := Check{ID: CheckFiles, Name: "Files present"}
	var problems, warnings []string
	// A dropped decoder index marks the inspection partial, but the shard
	// is on disk. That warning belongs to the layer-index check.
	if m.Partial && !layerDropsOnly(m) {
		problems = append(problems, incompleteInspection)
	}
	files := filesByName(m)
	indexed := map[string]struct{}{}
	if m.Index != nil {
		for _, shard := range m.Index.WeightMap {
			indexed[shard] = struct{}{}
		}
	}
	var broken, missing, escapes int
	for _, s := range m.Shards {
		if !localName(s.Name) {
			escapes++
			problems = append(problems, fmt.Sprintf("%s: unreadable: %v", s.Name, s.Err))
			continue
		}
		f, ok := files[s.Name]
		if !ok {
			missing++
			problems = append(problems, fmt.Sprintf("%s: unreadable: %v", s.Name, s.Err))
			continue
		}
		if f.Broken {
			broken++
			problems = append(problems, fmt.Sprintf("%s: unreadable: %v", s.Name, s.Err))
		}
	}
	if m.Index != nil {
		for _, s := range m.Shards {
			if _, ok := indexed[s.Name]; ok {
				continue
			}
			if !strings.HasSuffix(s.Name, ".safetensors") {
				continue
			}
			warnings = append(warnings, fmt.Sprintf("%s: safetensors file is not referenced by the index", s.Name))
		}
	}
	switch {
	case len(m.Shards) == 0 && !m.Partial:
		c.Status, c.Summary = StatusSkip, "no safetensors shards found"
		return c
	case len(problems) > 0:
		c.Status = StatusFail
		if m.Partial && !layerDropsOnly(m) && missing == 0 && broken == 0 && escapes == 0 {
			c.Summary = incompleteInspection
		} else {
			c.Summary = fmt.Sprintf("%d missing, %d broken, %d path escapes", missing, broken, escapes)
		}
	case len(warnings) > 0:
		c.Status = StatusWarn
		c.Summary = fmt.Sprintf("%d safetensors files are not in the index", len(warnings))
	default:
		c.Status = StatusPass
		n := len(m.Shards)
		if m.Index != nil {
			n = len(indexed)
		}
		c.Summary = fmt.Sprintf("%d/%d indexed shards exist, %d broken links", n, n, broken)
		if m.Index == nil {
			c.Summary = fmt.Sprintf("%d safetensors files present, %d broken links", len(m.Shards), broken)
		}
	}
	c.Details = capDetails(append(problems, warnings...))
	return c
}

func offsetsBounds(m *model.Model) Check {
	c := Check{ID: CheckOffsets, Name: "Offsets & sizes"}
	var problems, warnings []string
	ranges := 0
	checked := 0
	start := time.Now()
	total := 0
	for _, s := range m.Shards {
		if s.Header != nil {
			total++
		}
	}
	for _, s := range m.Shards {
		if s.Header == nil {
			continue
		}
		checked++
		ranges += len(s.Header.Tensors)
		p, w := safetensors.CheckLayout(s.Name, s.Header, s.Size-s.Header.DataStart())
		problems = append(problems, p...)
		warnings = append(warnings, w...)
		if m != nil {
			m.NoteLayout(checked, total, ranges, time.Since(start), checked == total)
		}
	}
	if m != nil && total == 0 {
		m.NoteLayout(0, 0, 0, time.Since(start), true)
	}
	if checked == 0 {
		c.Status, c.Summary = StatusSkip, "no shard headers to check"
		return c
	}
	switch {
	case len(problems) > 0:
		c.Status = StatusFail
		c.Summary = fmt.Sprintf("%d of %d ranges have problems", len(problems), ranges)
	case len(warnings) > 0:
		c.Status = StatusWarn
		c.Summary = fmt.Sprintf("%d ranges, %d with unknown dtype", ranges, len(warnings))
	default:
		c.Status = StatusPass
		c.Summary = fmt.Sprintf("%d ranges in-bounds, = dtype×shape, no overlaps", ranges)
	}
	c.Details = capDetails(append(problems, warnings...))
	return c
}

func indexConsistency(m *model.Model) Check {
	c := Check{ID: CheckIndex, Name: "Index matches headers"}
	if len(m.Shards) == 0 && m.IndexErr == nil && !m.Partial {
		c.Status, c.Summary = StatusSkip, "no safetensors shards found"
		return c
	}
	var problems, warnings []string
	if m.IndexErr != nil {
		switch {
		case errors.Is(m.IndexErr, model.ErrIndexWeightMap), errors.Is(m.IndexErr, model.ErrMultipleIndex):
			problems = append(problems, m.IndexErr.Error())
		default:
			problems = append(problems, fmt.Sprintf("index file unreadable: %v", m.IndexErr))
		}
	}
	headerNames := map[string]map[string]bool{}
	for _, s := range m.Shards {
		if s.Header == nil {
			continue
		}
		set := make(map[string]bool, len(s.Header.Tensors))
		for _, t := range s.Header.Tensors {
			set[t.Name] = true
		}
		headerNames[s.Name] = set
	}
	switch {
	case m.Index != nil:
		found := 0
		for name, shard := range m.Index.WeightMap {
			if !localName(shard) {
				problems = append(problems, fmt.Sprintf("%s -> %s: shard name is not a local file name", name, shard))
				continue
			}
			set, ok := headerNames[shard]
			switch {
			case ok && set[name]:
				found++
			case !ok:
				problems = append(problems, fmt.Sprintf("%s -> %s: shard missing or unreadable", name, shard))
			default:
				problems = append(problems, fmt.Sprintf("%s: not present in %s header", name, shard))
			}
		}
		var unmapped []string
		for shard, set := range headerNames {
			for name := range set {
				if m.Index.WeightMap[name] != shard {
					unmapped = append(unmapped, fmt.Sprintf("%s: not mapped to %s by the index", name, shard))
				}
			}
		}
		if len(unmapped) > 0 {
			warnings = append(warnings, fmt.Sprintf("%d tensors in shard headers are not mapped to that shard by the index", len(unmapped)))
			warnings = append(warnings, unmapped...)
		}
		if m.Index.TotalSize > 0 && m.Index.TotalSize != m.TensorBytes {
			warnings = append(warnings, fmt.Sprintf("index metadata.total_size=%d but header byte ranges sum to %d", m.Index.TotalSize, m.TensorBytes))
		}
		c.Summary = fmt.Sprintf("%d/%d index entries found in shard headers", found, len(m.Index.WeightMap))
	case errors.Is(m.IndexErr, model.ErrIndexWeightMap):
		c.Summary = "index parsed but has missing or empty weight_map"
	case errors.Is(m.IndexErr, model.ErrMultipleIndex):
		c.Summary = "multiple index files; not selecting one"
	case m.IndexErr != nil:
		c.Summary = "index file present but unreadable"
	default:
		c.Status, c.Summary = StatusSkip, "no index file (single-file model)"
		return c
	}
	sort.Strings(problems)
	sort.Strings(warnings)
	switch {
	case len(problems) > 0:
		c.Status = StatusFail
	case len(warnings) > 0:
		c.Status = StatusWarn
	default:
		c.Status = StatusPass
	}
	c.Details = capDetails(append(problems, warnings...))
	return c
}

func codeExecFormats(m *model.Model) Check {
	c := Check{ID: CheckCodeExec, Name: "Code-executing formats"}
	var details []string
	for _, f := range m.CodeExecFiles() {
		details = append(details, f.Name)
	}
	for _, e := range m.AutoMapEntries() {
		details = append(details, fmt.Sprintf("%s auto_map %s -> %s", e.File, e.Key, e.Target))
	}
	for _, name := range m.UncheckedAutoMap {
		details = append(details, "could not check auto_map in "+name)
	}
	sort.Strings(details)
	if len(details) == 0 {
		c.Status = StatusPass
		if name, _, ok := model.FormatName(m.Format); ok && name == "safetensors" {
			c.Summary = "none: weights are safetensors, no .py"
		} else {
			c.Summary = "none"
		}
		return c
	}
	c.Status = StatusWarn
	c.Summary = fmt.Sprintf("%d findings (code-exec formats or auto_map)", len(details))
	c.Details = capDetails(details)
	return c
}

func codeContent(m *model.Model) Check {
	c := Check{ID: CheckCode, Name: "Code & pickle contents", Status: StatusSkip}
	if len(m.CodeExecFiles()) == 0 && len(m.AutoMapEntries()) == 0 {
		c.Summary = "nothing to read (no flagged files)"
		return c
	}
	c.Summary = "not run (open Code & pickles, or pass --code)"
	return c
}

func plannedHash(m *model.Model) Check {
	return Check{
		ID: CheckHash, Name: "Hashes vs revision", Status: StatusPlanned,
		Summary: fmt.Sprintf("not run: needs a full read of %s", model.HumanBytes(m.TotalSize)),
	}
}

func metadataSanity(m *model.Model) Check {
	c := Check{ID: CheckMetadata, Name: "Config vs tensor shapes", Status: StatusSkip}
	if m == nil {
		c.Summary = "no model loaded"
		return c
	}
	if len(m.DroppedLayers) > 0 {
		return droppedLayers(m, c)
	}
	switch {
	case len(m.Shards) == 0:
		c.Summary = "no safetensors shards found"
		return c
	case m.Config == nil:
		c.Summary = "no usable config.json"
		return c
	}
	r := m.ConfigShapes()
	var problems []string
	for _, cmp := range r.Comparisons {
		if !cmp.Match {
			problems = append(problems, fmt.Sprintf("%s is %d, %s is %d", cmp.Path, cmp.Want, cmp.What, cmp.Got))
		}
	}
	problems = append(problems, r.Invalid...)
	total := len(r.Comparisons) + len(r.Invalid)
	switch {
	case total == 0:
		c.Summary = "no config field to compare with tensors"
	case len(problems) > 0:
		c.Status = StatusWarn
		c.Summary = fmt.Sprintf("%d of %d config comparisons disagree", len(problems), total)
	default:
		c.Status = StatusPass
		c.Summary = fmt.Sprintf("%d config comparisons match tensor shapes", total)
		if total == 1 {
			c.Summary = "1 config comparison matches tensor shapes"
		}
	}
	c.Details = capDetails(append(problems, r.Skipped...))
	return c
}

// droppedLayers reports decoder indices above model.MaxLayerIndex. The
// files are present; the index itself is what the inspection dropped, so
// the layer count is incomplete and no config comparison is made.
// The summary is built from the typed indexes, not by matching warning text.
func droppedLayers(m *model.Model, c Check) Check {
	drops := m.DroppedLayers
	c.Name = "Layer index"
	c.Status = StatusFail
	details := make([]string, len(drops))
	for i, idx := range drops {
		details[i] = model.DroppedLayerWarning(idx)
	}
	if len(drops) == 1 {
		c.Summary = details[0]
	} else {
		c.Summary = fmt.Sprintf("%d layer indices exceed %d; dropped", len(drops), model.MaxLayerIndex)
	}
	c.Details = capDetails(details)
	return c
}

// layerDropsOnly reports a partial inspection whose warnings are all
// dropped decoder indices. File presence still fails when a shard is
// missing or some other warning made the walk incomplete.
func layerDropsOnly(m *model.Model) bool {
	if m == nil || !m.Partial || len(m.DroppedLayers) == 0 || len(m.Warnings) != len(m.DroppedLayers) {
		return false
	}
	for i, idx := range m.DroppedLayers {
		if m.Warnings[i] != model.DroppedLayerWarning(idx) {
			return false
		}
	}
	return true
}

func filesByName(m *model.Model) map[string]model.File {
	out := make(map[string]model.File, len(m.Files))
	for _, f := range m.Files {
		out[f.Name] = f
	}
	return out
}

// presenceErr reports shards that never reached safetensors.ReadFile:
// a non-local name, a file that is not in the directory, or a broken link.
func presenceErr(m *model.Model, s *model.Shard) bool {
	if s.Header != nil {
		return false
	}
	if !localName(s.Name) {
		return true
	}
	f, ok := filesByName(m)[s.Name]
	if !ok || f.Broken {
		return true
	}
	return false
}

func capDetails(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	sort.Strings(items)
	if len(items) > maxDetails {
		items = append(items[:maxDetails], fmt.Sprintf("... and %d more", len(items)-maxDetails))
	}
	return items
}

func localName(name string) bool {
	return name != "" && filepath.IsLocal(name)
}
