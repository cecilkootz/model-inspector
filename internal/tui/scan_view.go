package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"origin.cursor.com/cecilkootz/model-inspector/internal/fileerr"
	"origin.cursor.com/cecilkootz/model-inspector/internal/model"
	"origin.cursor.com/cecilkootz/model-inspector/internal/sanitize"
	"origin.cursor.com/cecilkootz/model-inspector/internal/scan"
)

type scanLine struct {
	text  string
	check int
}

func (t Model) scanBody(width, height int) string {
	if strings.HasPrefix(t.m.Format, "gguf") {
		return "GGUF parsing is coming. model-inspector read file sizes only."
	}
	lines := t.scanLines(width)
	if height < 1 {
		height = 1
	}
	start := t.scanScroll
	if start < 0 {
		start = 0
	}
	if start > len(lines) {
		start = len(lines)
	}
	var b strings.Builder
	for i := 0; i < height; i++ {
		if i > 0 {
			b.WriteByte('\n')
		}
		if start+i < len(lines) {
			b.WriteString(lines[start+i].text)
		}
	}
	return b.String()
}

func (t Model) scanLines(width int) []scanLine {
	if width < 20 {
		width = 20
	}
	var lines []scanLine
	add := func(s string) {
		lines = append(lines, scanLine{text: truncWidth(s, width), check: -1})
	}
	counts := t.scan.Count()
	notRun := counts.Skip + counts.Planned
	head := fmt.Sprintf("Integrity & provenance · structure only    %d passed · %d warn · %d fail · %d n/r",
		counts.Pass, counts.Warn, counts.Fail, notRun)
	if t.scan.Strict {
		eff := effectiveCounts(t.scan)
		head = fmt.Sprintf("Integrity & provenance · structure only    %d passed · %d warn · %d fail · %d n/r · strict: warnings count as failures",
			eff.pass, eff.warn, eff.fail, notRun)
	}
	add(head)
	add(sanitize.From(t.scan.Disclaimer).String())
	add("Checks that the files are complete, well-formed and agree with each other.")
	add("It does not look at what the weights compute. See \"Not covered\" below.")
	add("")
	for i, c := range t.scan.Checks {
		mark := "  "
		if i == t.scanCursor {
			mark = g.selected + " "
		}
		row := mark + statusCell(c) + "  " + padLabel(c.Name, 26) + " " + sanitize.From(c.Summary).String()
		lines = append(lines, scanLine{text: truncWidth(row, width), check: i})
		if t.scanOpen[c.ID] {
			why := scan.Why(c.ID)
			if why != "" {
				lines = append(lines, scanLine{text: truncWidth("    Why it matters: "+why, width), check: i})
			}
			shown := 0
			for _, d := range c.Details {
				if shown == 5 {
					extra := len(c.Details) - shown
					lines = append(lines, scanLine{text: truncWidth(fmt.Sprintf("    · …and %d more   (all details: inspect --json)", extra), width), check: i})
					break
				}
				lines = append(lines, scanLine{text: truncWidth("    · "+fileerr.Present(d), width), check: i})
				shown++
			}
		}
	}
	add("")
	add("Not covered by these checks")
	add(" · whether the weights are benign, unmodified or what the publisher intended")
	add(" · what the model outputs or how it behaves")
	add(" · tokenizer, chat template, license, or training-data provenance")
	add(" · " + revisionNote(t.m))
	add(" · This is not a malware or behavior scan.")
	return lines
}

func (t *Model) ensureScanVisible() {
	lines := t.scanLines(t.width)
	idx := 0
	for i, ln := range lines {
		if ln.check == t.scanCursor {
			idx = i
			break
		}
	}
	h := t.bodyHeight()
	if h < 1 {
		h = 1
	}
	if idx < t.scanScroll {
		t.scanScroll = idx
	}
	if idx >= t.scanScroll+h {
		t.scanScroll = idx - h + 1
	}
	if t.scanScroll < 0 {
		t.scanScroll = 0
	}
}

func statusCell(c scan.Check) string {
	word, style := statusWord(c)
	text := word
	if colorOff() {
		return text
	}
	return style.Render(text)
}

func statusWord(c scan.Check) (string, lipgloss.Style) {
	st := c.EffectiveStatus
	raw := c.Status
	switch {
	case raw == scan.StatusSkip || raw == scan.StatusPlanned:
		return g.nr + " N/R", nrStyle
	case c.Status == scan.StatusWarn && st == scan.StatusFail:
		return g.warnMark + " WARN→FAIL", warnStyle
	case st == scan.StatusPass:
		return g.pass + " PASS", passStyle
	case st == scan.StatusWarn:
		return g.warnMark + " WARN", warnStyle
	case st == scan.StatusFail:
		return g.fail + " FAIL", failStyle
	default:
		return g.nr + " N/R", nrStyle
	}
}

func revisionNote(m *model.Model) string {
	if m.Revision == "" {
		return "revision: — ; file contents were not hashed"
	}
	rev := m.Revision
	if len(rev) > 7 {
		rev = rev[:7]
	}
	return "revision " + sanitize.From(rev).String() + " is the cache ref; file contents were not hashed"
}

type effCounts struct{ pass, warn, fail int }

func effectiveCounts(res scan.Result) effCounts {
	var c effCounts
	for _, ch := range res.Checks {
		switch ch.EffectiveStatus {
		case scan.StatusPass:
			c.pass++
		case scan.StatusWarn:
			c.warn++
		case scan.StatusFail:
			c.fail++
		}
	}
	return c
}
