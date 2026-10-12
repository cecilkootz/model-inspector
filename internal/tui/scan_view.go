package tui

import (
	"fmt"
	"strings"

	"github.com/cecilkootz/model-inspector/internal/fileerr"
	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/sanitize"
	"github.com/cecilkootz/model-inspector/internal/scan"
	"github.com/cecilkootz/model-inspector/internal/tui/ui"
)

type scanView struct {
	loading   bool
	loadErr   string
	spin      int
	dir       string
	strict    bool
	fromCache bool
	repo, rev string
	progCh    chan model.Progress
	phases    loadPhases
}

func (s *scanView) framePart() string {
	if s == nil {
		return ""
	}
	return fmt.Sprintf("|sc %v %q %d", s.loading, s.loadErr, s.spin)
}

func notCoveredBody(m *model.Model, width int) []string {
	var lines []string
	for _, ln := range wrapText(scan.Disclaimer, width) {
		lines = append(lines, ln)
	}
	for _, b := range []string{
		"whether the weights are benign, unmodified or what the publisher intended",
		"what the model outputs or how it behaves",
		"tokenizer, chat template, license, or training-data provenance",
		revisionNote(m),
		"This is not a malware or behavior scan.",
	} {
		wrapped := wrapText(b, max(8, width-3))
		for i, ln := range wrapped {
			if i == 0 {
				lines = append(lines, "·  "+ln)
			} else {
				lines = append(lines, "   "+ln)
			}
		}
	}
	return lines
}

func bandCounts(res scan.Result) (pass, warn, fail, notRun, wasWarn int) {
	raw := res.Count()
	wasWarn = raw.Warn
	for _, c := range res.Checks {
		switch c.EffectiveStatus {
		case scan.StatusPass:
			pass++
		case scan.StatusWarn:
			warn++
		case scan.StatusFail:
			fail++
		default:
			notRun++
		}
	}
	return pass, warn, fail, notRun, wasWarn
}

func statusPill(c scan.Check) string {
	g := ui.G()
	arrow := ui.RArrow()
	promoted := c.Status == scan.StatusWarn && c.EffectiveStatus == scan.StatusFail
	if ui.ColorOff() {
		switch {
		case promoted:
			return g.Warn + " WARN" + arrow + "FAIL"
		case c.EffectiveStatus == scan.StatusPass:
			return g.Pass + " PASS"
		case c.EffectiveStatus == scan.StatusWarn:
			return g.Warn + " WARN"
		case c.EffectiveStatus == scan.StatusFail:
			return g.Fail + " FAIL"
		default:
			return g.NR + " N/R"
		}
	}
	if promoted {
		left := ui.Paint(" ! WARN ", ui.TextInk.Hex(), ui.FillWarn.Hex(), true)
		right := ui.Paint(" "+arrow+" "+g.Fail+" FAIL ", ui.TextInk.Hex(), ui.FillFail.Hex(), true)
		return left + right
	}
	var kind ui.FillKind
	var word string
	switch c.EffectiveStatus {
	case scan.StatusPass:
		kind, word = ui.FillKindOK, g.Pass+" PASS"
	case scan.StatusWarn:
		kind, word = ui.FillKindWarn, g.Warn+" WARN"
	case scan.StatusFail:
		kind, word = ui.FillKindFail, g.Fail+" FAIL"
	default:
		kind, word = ui.FillKindNR, g.NR+" N/R"
	}
	return ui.Pill(kind, sanitize.Program(word))
}

func durationText(c scan.Check) string {
	if c.Status == scan.StatusSkip || c.Status == scan.StatusPlanned || c.EffectiveStatus == scan.StatusSkip || c.EffectiveStatus == scan.StatusPlanned {
		if c.Status == scan.StatusSkip || c.Status == scan.StatusPlanned {
			return ui.Dash()
		}
	}
	if c.Status == scan.StatusSkip || c.Status == scan.StatusPlanned {
		return ui.Dash()
	}
	ms := c.Duration.Milliseconds()
	if ms < 1 {
		return "<1 ms"
	}
	return fmt.Sprintf("%d ms", ms)
}

func revisionNote(m *model.Model) string {
	if m == nil || m.Revision == "" {
		return "revision: " + ui.Dash() + " ; file contents were not hashed"
	}
	rev := m.Revision
	if len(rev) > 7 {
		rev = rev[:7]
	}
	return "revision " + clipScan(rev) + " is the cache ref; file contents were not hashed"
}

func clipScan(s string) string {
	return foldASCII(fileerr.Present(s))
}

func scanTrunc(s string, width int) string {
	if width < 1 {
		return ""
	}
	if ui.Width(s) <= width {
		return s
	}
	ell := ui.G().Ellipsis
	return ui.Trunc(s, width, ell)
}

func wrapText(s string, width int) []string {
	s = foldASCII(s)
	if width < 8 {
		width = 8
	}
	if s == "" {
		return nil
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}
	var lines []string
	var cur string
	for _, w := range words {
		if cur == "" {
			cur = w
			continue
		}
		if ui.Width(cur)+1+ui.Width(w) > width {
			lines = append(lines, cur)
			cur = w
			continue
		}
		cur += " " + w
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

func foldASCII(s string) string {
	if ui.UTF8() {
		return s
	}
	r := strings.NewReplacer(
		"≤", "<=", "≥", ">=", "−", "-", "–", "-", "—", "-",
		"×", "x", "→", "->", "·", ".", "…", "...",
		"▸", ">", "▾", "v", "✓", "+", "✗", "x", "▌", "|",
		"◇", "o", "◆", "*", "›", ">",
	)
	return r.Replace(s)
}
