// Package scan runs cheap structural checks on a loaded model.
// This is not a malware or behavior scan.
package scan

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"origin.cursor.com/cecilkootz/model-inspector/internal/model"
	"origin.cursor.com/cecilkootz/model-inspector/internal/safetensors"
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
		headerParse(m),
		filePresence(m),
		offsetsBounds(m),
		indexConsistency(m),
		codeExecFormats(m),
		codeContent(m),
		plannedHash(m),
		plannedMetadata(),
	}
	for i := range checks {
		checks[i].EffectiveStatus = checks[i].Status
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
		return "Comparing config fields with tensor shapes is planned for a later version."
	default:
		return ""
	}
}

// rollup is the worst effective status of executed checks.
// Skip and planned do not change it. When no shard was inspected and
// nothing warned or failed, the result stays skip: an empty directory
// or a GGUF-only directory is not reported as a pass.
func rollup(checks []Check, inspected bool) Status {
	overall := StatusPass
	executed := false
	for _, c := range checks {
		switch c.Status {
		case StatusSkip, StatusPlanned:
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
	if m.Partial {
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
		if m.Partial && missing == 0 && broken == 0 && escapes == 0 {
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
	for _, s := range m.Shards {
		if s.Header == nil {
			continue
		}
		checked++
		ranges += len(s.Header.Tensors)
		p, w := safetensors.CheckLayout(s.Name, s.Header, s.Size-s.Header.DataStart())
		problems = append(problems, p...)
		warnings = append(warnings, w...)
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

func plannedMetadata() Check {
	return Check{
		ID: CheckMetadata, Name: "Config vs tensor shapes", Status: StatusPlanned,
		Summary: "not run: planned",
	}
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
