// Package scan runs cheap structural checks on a loaded model.
// This is NOT a malware scan.
package scan

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"

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

// Disclaimer is shown wherever scan results are displayed.
const Disclaimer = "Structural integrity checks only. This is NOT a malware scan and does not prove the weights are safe or authentic."

// ChecksImplemented is the plain count of checks that run today.
// One check is implemented; three more are listed as planned.
const ChecksImplemented = "1 of 4 checks is implemented."

type Check struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Status  Status   `json:"status"`
	Summary string   `json:"summary"`
	Details []string `json:"details,omitempty"`
}

type Result struct {
	Disclaimer string  `json:"disclaimer"`
	Overall    Status  `json:"overall"`
	Checks     []Check `json:"checks"`
}

// Run executes implemented checks and lists planned ones.
func Run(m *model.Model) Result {
	checks := append([]Check{IndexConsistency(m)}, planned()...)
	r := Result{Disclaimer: Disclaimer, Overall: StatusPass, Checks: checks}
	for _, c := range checks {
		switch c.Status {
		case StatusFail:
			r.Overall = StatusFail
		case StatusWarn:
			if r.Overall != StatusFail {
				r.Overall = StatusWarn
			}
		case StatusSkip:
			// A skipped integrity check must not collapse to an overall pass.
			// Planned checks stay planned and do not change the result.
			if r.Overall == StatusPass {
				r.Overall = StatusSkip
			}
		}
	}
	return r
}

func planned() []Check {
	return []Check{
		{ID: "hash-vs-revision", Name: "Hash vs pinned revision", Status: StatusPlanned,
			Summary: "Stream-hash each blob and compare with the LFS sha256 for the pinned HF commit."},
		{ID: "metadata-sanity", Name: "Metadata sanity", Status: StatusPlanned,
			Summary: "Cross-check config.json (hidden_size, vocab, experts, layers) against tensor shapes; flag odd __metadata__."},
		{ID: "behavior-probes", Name: "Behavior probes", Status: StatusPlanned,
			Summary: "Run a small prompt suite against a served copy to spot tampering, backdoor triggers, or refusal drift."},
	}
}

const maxDetails = 25

// IndexConsistency verifies that every tensor named in the index exists in the
// referenced shard's header and that every header byte range fits in its file.
func IndexConsistency(m *model.Model) Check {
	c := Check{ID: "index-shard-consistency", Name: "Index vs shard headers / offsets within file sizes"}
	if len(m.Shards) == 0 && m.IndexErr == nil && !m.Partial {
		c.Status, c.Summary = StatusSkip, "no safetensors shards found"
		return c
	}
	var problems, warnings []string
	if m.Partial {
		problems = append(problems, "inspection hit a safety cap and is incomplete")
	}
	if m.IndexErr != nil {
		switch {
		case errors.Is(m.IndexErr, model.ErrIndexWeightMap), errors.Is(m.IndexErr, model.ErrMultipleIndex):
			// Parsed, or deliberately not parsed because the choice is ambiguous.
			// Do not call either case unreadable.
			problems = append(problems, m.IndexErr.Error())
		default:
			problems = append(problems, fmt.Sprintf("index file unreadable: %v", m.IndexErr))
		}
	}
	headerNames := map[string]map[string]bool{}
	for _, s := range m.Shards {
		if s.Header == nil {
			problems = append(problems, fmt.Sprintf("%s: unreadable: %v", s.Name, s.Err))
			continue
		}
		set := make(map[string]bool, len(s.Header.Tensors))
		for _, t := range s.Header.Tensors {
			set[t.Name] = true
		}
		headerNames[s.Name] = set
		p, w := safetensors.CheckLayout(s.Name, s.Header, s.Size-s.Header.DataStart())
		problems = append(problems, p...)
		warnings = append(warnings, w...)
	}

	if m.Index != nil {
		found := 0
		referenced := map[string]bool{}
		for name, shard := range m.Index.WeightMap {
			referenced[shard] = true
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
		for _, s := range m.Shards {
			if !referenced[s.Name] {
				warnings = append(warnings, fmt.Sprintf("%s: safetensors file is not referenced by the index", s.Name))
			}
		}
		extra := 0
		for shard, set := range headerNames {
			for name := range set {
				if m.Index.WeightMap[name] != shard {
					extra++
				}
			}
		}
		if extra > 0 {
			warnings = append(warnings, fmt.Sprintf("%d tensors in shard headers are not mapped to that shard by the index", extra))
		}
		if m.Index.TotalSize > 0 && m.Index.TotalSize != m.TensorBytes {
			warnings = append(warnings, fmt.Sprintf("index metadata.total_size=%d but header byte ranges sum to %d", m.Index.TotalSize, m.TensorBytes))
		}
		c.Summary = fmt.Sprintf("%d/%d index entries found in %d shard headers; %d tensor byte ranges checked against file sizes",
			found, len(m.Index.WeightMap), len(headerNames), len(m.Tensors))
	} else if errors.Is(m.IndexErr, model.ErrIndexWeightMap) {
		c.Summary = fmt.Sprintf("index parsed but has missing or empty weight_map; %d tensor byte ranges in %d shard headers checked against file sizes",
			len(m.Tensors), len(headerNames))
	} else if errors.Is(m.IndexErr, model.ErrMultipleIndex) {
		c.Summary = fmt.Sprintf("multiple index files; not selecting one; %d tensor byte ranges in %d shard headers checked against file sizes",
			len(m.Tensors), len(headerNames))
	} else if m.IndexErr != nil {
		c.Summary = fmt.Sprintf("index file present but unreadable; %d tensor byte ranges in %d shard headers checked against file sizes",
			len(m.Tensors), len(headerNames))
	} else {
		c.Summary = fmt.Sprintf("no index file; %d tensor byte ranges in %d shard headers checked against file sizes",
			len(m.Tensors), len(headerNames))
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
	all := append(problems, warnings...)
	if len(all) > maxDetails {
		all = append(all[:maxDetails], fmt.Sprintf("... and %d more", len(all)-maxDetails))
	}
	c.Details = all
	return c
}

func localName(name string) bool {
	return name != "" && filepath.IsLocal(name)
}
