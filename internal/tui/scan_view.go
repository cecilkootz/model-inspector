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

// scanClip is the rune cap for a hostile path, detail, or error, matching
// the bound used before a file-derived error is shown.
const scanClip = 256

type scanView struct {
	drill     bool
	ncOpen    bool
	ncUser    bool
	code      bool
	codeCur   int
	loading   bool
	armed     bool
	prog      model.Progress
	loadErr   string
	spin      int
	hits      []hit
	dir       string
	strict    bool
	fromCache bool
	repo, rev string
	progCh    chan model.Progress
	detailTop int
}

func (s *scanView) framePart() string {
	if s == nil {
		return ""
	}
	return fmt.Sprintf("|sc %v %v %v %v %d %v %v %q %d %d %s %d",
		s.drill, s.ncOpen, s.ncUser, s.code, s.codeCur, s.loading, s.armed,
		s.loadErr, s.spin, s.prog.Done, s.prog.Phase, s.detailTop)
}

func (t Model) scanBody(width, height int) string {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	lines := t.scanRender(width, height)
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	for i := range lines {
		lines[i] = ui.Fit(lines[i], width)
	}
	return strings.Join(lines, "\n")
}

func (t *Model) ensureScanVisible() {
	n := len(t.scan.Checks)
	if n == 0 {
		t.scanCursor = 0
		t.scanScroll = 0
		return
	}
	if t.scanCursor < 0 {
		t.scanCursor = 0
	}
	if t.scanCursor >= n {
		t.scanCursor = n - 1
	}
	window := 5
	if ui.Breakpoint(t.width, t.height) != "S" {
		window = 4
	}
	if t.scanCursor < t.scanScroll {
		t.scanScroll = t.scanCursor
	}
	if t.scanCursor >= t.scanScroll+window {
		t.scanScroll = t.scanCursor - window + 1
	}
	if t.scanScroll < 0 {
		t.scanScroll = 0
	}
}

func (t Model) scanRender(width, height int) []string {
	if t.scn != nil {
		t.scn.hits = t.scn.hits[:0]
	}
	switch {
	case t.scn != nil && t.scn.loadErr != "":
		return t.scanErrorLines(width, height)
	case t.scn != nil && t.scn.loading && t.m == nil:
		if t.scn.armed {
			return t.scanProgressLines(width, height)
		}
		return t.scanQuietLoad(width, height)
	case t.scn != nil && t.scn.code:
		return t.scanCodeLines(width, height)
	case t.scn != nil && t.scn.drill:
		return t.scanDetailPage(width, height)
	default:
		return t.scanLayout(width, height)
	}
}

func (t Model) addScanHit(id string, x, y, w, h int) {
	if t.scn == nil || w < 1 || h < 1 || id == "" {
		return
	}
	t.scn.hits = append(t.scn.hits, hit{id: id, x: x, y: y, w: w, h: h})
}

func (t Model) scanLayout(width, height int) []string {
	long := width >= 140
	rows := 1
	br := ui.Breakpoint(width, height+3)
	if br == "M" || br == "L" {
		rows = 3
	}
	var lines []string
	if t.scan.Strict {
		lines = append(lines, t.strictBanner(width))
	}
	title, kind := verdictTitle(t.m, t.scan)
	pass, warn, fail, notRun, wasWarn := bandCounts(t.scan)
	counts := fmt.Sprintf("%d passed · %d warn · %d fail", pass, warn, fail)
	if t.scan.Strict && wasWarn > 0 {
		counts += fmt.Sprintf(" (%d was warn)", wasWarn)
	}
	counts += fmt.Sprintf(" · %d not run", notRun)
	right := ""
	if long && t.m != nil {
		right = scanStatLine(t.m)
	}
	lines = append(lines, bandLines(glyphWord(kind)+" "+title, counts, right, kind, rows, width)...)
	frameY := len(lines)
	lines = append(lines, framingLine(long, width))
	t.addScanHit("scan:nc", width-ui.Width(framingChip(long))-2, 2+frameY, ui.Width(framingChip(long))+2, 1)
	if br == "S" || width < 100 {
		lines = append(lines, "")
		lines = append(lines, t.cardBlock(width, 1, false)...)
		block, rel := t.notCoveredBlock(width, t.ncOpen())
		t.addScanHit("scan:nc", 0, 2+len(lines)+rel, width, 1)
		lines = append(lines, block...)
		return lines
	}
	lines = append(lines, "")
	leftW, rightW := scanColumns(width)
	left := t.cardBlock(leftW, 2, width >= 110)
	pre := len(lines)
	detail, ncRel := t.detailLines(rightW, height)
	if ncRel >= 0 {
		t.addScanHit("scan:nc", leftW+2, 2+pre+ncRel, rightW, 1)
	}
	body := scanZip(left, detail, leftW, width)
	// Not covered lives at the bottom of Detail when the pane is split.
	lines = append(lines, body...)
	return lines
}

func scanColumns(width int) (left, right int) {
	if width >= 140 {
		left = 62
		right = width - left - 2
		if right < 20 {
			right = 20
			left = width - right - 2
		}
		return left, right
	}
	left = width * 55 / 100
	right = width - left - 2
	if right < 16 {
		right = 16
		left = width - right - 2
	}
	return left, right
}

func (t Model) cardBlock(width, cardRows int, showDur bool) []string {
	if cardRows < 1 {
		cardRows = 1
	}
	var lines []string
	if cardRows > 1 {
		n := len(t.scan.Checks)
		head := fmt.Sprintf("CHECKS  %d · structure only · in main's order", n)
		lines = append(lines, scanPaint(head, ui.TextPrimary, true)+" "+scanPaint("", ui.TextMuted, false))
		lines = append(lines, "")
	}
	checks := indexChecks(t.scan)
	split := anyPromoted(t.scan)
	shown := 0
	for _, ph := range scanPhases {
		var ids []string
		for _, id := range ph.ids {
			if _, ok := checks[id]; ok {
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 {
			continue
		}
		// Drop a phase whose cards are all above the scroll window.
		visible := false
		for _, id := range ids {
			if checks[id].i >= t.scanScroll {
				visible = true
			}
		}
		if !visible {
			continue
		}
		lines = append(lines, scanPaint(ph.name, ui.Accent, true))
		for _, id := range ids {
			ch := checks[id]
			if ch.i < t.scanScroll {
				continue
			}
			shown++
			sel := ch.i == t.scanCursor
			block := t.cardLines(ch.c, width, cardRows, showDur, sel, split)
			y0 := 2 + len(lines)
			for _, ln := range block {
				lines = append(lines, ln)
			}
			t.addScanHit("scan:card:"+ch.c.ID, 0, y0, width, len(block))
			t.addScanHit("scan:exp:"+ch.c.ID, 0, y0, 4, 1)
			if cardRows > 1 {
				lines = append(lines, "")
			}
			if t.scanOpen[id] {
				for _, ln := range t.expandLines(ch.c, width) {
					lines = append(lines, ln)
				}
			}
		}
	}
	_ = shown
	if cardRows > 1 && width >= 40 {
		lines = append(lines, scanPaint("⏎ details · space expand · e/E all · ] next problem · y copy", ui.TextMuted, false))
	}
	return lines
}

type indexedCheck struct {
	c scan.Check
	i int
}

func indexChecks(res scan.Result) map[string]indexedCheck {
	out := make(map[string]indexedCheck, len(res.Checks))
	for i, c := range res.Checks {
		out[c.ID] = indexedCheck{c: c, i: i}
	}
	return out
}

type scanPhase struct {
	name string
	ids  []string
}

var scanPhases = []scanPhase{
	{"READ", []string{scan.CheckHeader, scan.CheckFiles}},
	{"STRUCTURE", []string{scan.CheckOffsets, scan.CheckIndex}},
	{"CODE", []string{scan.CheckCodeExec, scan.CheckCode}},
	{"PROVENANCE", []string{scan.CheckHash, scan.CheckMetadata}},
}

func (t Model) cardLines(c scan.Check, width, rows int, showDur, sel, split bool) []string {
	bg := ui.SurfaceCard
	if sel {
		bg = ui.SurfaceSel
	}
	edge := gutterGlyph()
	edgeCol := statusToken(c)
	pill := statusPill(c)
	name := c.Name
	sum := clipScan(c.Summary)
	dur := durationText(c)
	if !showDur {
		dur = ""
	}
	if rows <= 1 {
		return []string{t.oneRow(c, width, bg, edge, edgeCol, pill, name, sum, dur, sel, split)}
	}
	row1 := t.wideRow(width, bg, edge, edgeCol, pill, name, dur, true, sel)
	sumCol := ui.TextMuted
	if sel {
		sumCol = ui.TextSecondary
	}
	sum = scanTrunc(sum, max(8, width-14))
	row2 := t.wideRow(width, bg, edge, edgeCol, strings.Repeat(" ", ui.Width(stripANSI(pill))), sum, "", false, sel)
	// The second row's summary should use the muted colour, not the name colour.
	row2 = t.summaryRow(width, bg, edge, edgeCol, pill, sum, sumCol)
	return []string{row1, row2}
}

func (t Model) oneRow(c scan.Check, width int, bg ui.Token, edge string, edgeCol ui.Token, pill, name, sum, dur string, sel, split bool) string {
	mark := " "
	if sel && ui.ColorOff() {
		mark = ui.G().Selected
		if mark == "" {
			mark = ">"
		}
	}
	statusW := ui.Width(stripANSI(statusPill(passSample())))
	if split {
		statusW += 4
	}
	pillPlain := stripANSI(pill)
	if ui.Width(pillPlain) < statusW {
		pill = pill + strings.Repeat(" ", statusW-ui.Width(pillPlain))
	}
	durW := ui.Width(dur)
	nameW := 22
	if width < 70 {
		nameW = 16
	}
	sumW := width - 2 - ui.Width(edge) - 1 - statusW - 1 - nameW - 1 - durW - 1
	if sumW < 8 {
		sumW = 8
	}
	sum = scanTrunc(sum, sumW)
	var b strings.Builder
	b.WriteString(mark)
	b.WriteString(paintOn(edge, edgeCol, bg, false))
	b.WriteString(paintOn(" ", ui.TextMuted, bg, false))
	b.WriteString(pill)
	if gap := statusW - ui.Width(stripANSI(pill)); gap > 0 && !strings.HasSuffix(pill, strings.Repeat(" ", gap)) {
		b.WriteString(paintOn(strings.Repeat(" ", gap), ui.TextMuted, bg, false))
	}
	b.WriteString(paintOn(" ", ui.TextMuted, bg, false))
	nameCol := ui.TextPrimary
	b.WriteString(paintOn(padRight(name, nameW), nameCol, bg, true))
	b.WriteString(paintOn(" ", ui.TextMuted, bg, false))
	sumCol := ui.TextMuted
	if sel {
		sumCol = ui.TextSecondary
	}
	b.WriteString(paintOn(padRight(sum, sumW), sumCol, bg, false))
	if dur != "" {
		b.WriteString(paintOn(" ", ui.TextMuted, bg, false))
		b.WriteString(paintOn(dur, ui.TextSecondary, bg, false))
	}
	return fitPaint(b.String(), width, bg)
}

func (t Model) wideRow(width int, bg ui.Token, edge string, edgeCol ui.Token, pill, name, dur string, bold, sel bool) string {
	var b strings.Builder
	b.WriteString(paintOn(" ", ui.TextMuted, bg, false))
	b.WriteString(paintOn(edge, edgeCol, bg, false))
	b.WriteString(paintOn("  ", ui.TextMuted, bg, false))
	b.WriteString(pill)
	b.WriteString(paintOn("  ", ui.TextMuted, bg, false))
	col := ui.TextPrimary
	if !bold {
		col = ui.TextMuted
		if sel {
			col = ui.TextSecondary
		}
	}
	b.WriteString(paintOn(name, col, bg, bold))
	if dur != "" {
		used := ui.Width(stripANSI(b.String()))
		gap := width - used - ui.Width(dur) - 1
		if gap < 1 {
			gap = 1
		}
		b.WriteString(paintOn(strings.Repeat(" ", gap)+dur, ui.TextSecondary, bg, false))
	}
	return fitPaint(b.String(), width, bg)
}

func (t Model) summaryRow(width int, bg ui.Token, edge string, edgeCol ui.Token, pill, sum string, col ui.Token) string {
	indent := " " + edge + "  " + strings.Repeat(" ", ui.Width(stripANSI(pill))) + "  "
	text := scanTrunc(sum, max(4, width-ui.Width(indent)-1))
	var b strings.Builder
	b.WriteString(paintOn(" ", ui.TextMuted, bg, false))
	b.WriteString(paintOn(edge, edgeCol, bg, false))
	b.WriteString(paintOn("  "+strings.Repeat(" ", ui.Width(stripANSI(pill)))+"  ", ui.TextMuted, bg, false))
	b.WriteString(paintOn(text, col, bg, false))
	return fitPaint(b.String(), width, bg)
}

func (t Model) expandLines(c scan.Check, width int) []string {
	indent := " " + gutterGlyph() + "           "
	var parts []string
	if line := checkedOne(t.m, c); line != "" {
		parts = append(parts, line)
	}
	if r := ruleShort(c.ID); r != "" {
		parts = append(parts, "rule: "+r)
	}
	probs := problemLines(c, 1)
	if len(probs) == 0 {
		parts = append(parts, "problems: none")
	}
	if np := scan.NotProven(c.ID); np != "" {
		parts = append(parts, "does not prove: "+shortProve(np))
	}
	body := strings.Join(parts, " · ")
	extra := probs
	if len(probs) > 0 {
		n := len(c.Details)
		extra = append(probs, fmt.Sprintf("rule: %s · %d problem shown of %d", ruleShort(c.ID), min(1, n), realProblems(c)))
		body = ""
	}
	var lines []string
	if body != "" {
		for _, ln := range wrapText(body, max(8, width-ui.Width(indent))) {
			lines = append(lines, scanPaint(indent+ln, ui.TextSecondary, false))
			if len(lines) == 3 {
				return lines
			}
		}
	}
	for _, ln := range extra {
		for _, w := range wrapText(ln, max(8, width-ui.Width(indent))) {
			lines = append(lines, scanPaint(indent+w, ui.TextSecondary, false))
			if len(lines) == 3 {
				return lines
			}
		}
	}
	return lines
}

func (t Model) detailLines(width, height int) ([]string, int) {
	var lines []string
	ncRel := -1
	lines = append(lines, scanPaint("DETAIL", ui.TextPrimary, true)+"  "+scanPaint("selected check", ui.TextMuted, false))
	if len(t.scan.Checks) == 0 || t.scanCursor < 0 || t.scanCursor >= len(t.scan.Checks) {
		lines = append(lines, scanPaint("No check selected.", ui.TextMuted, false))
		return lines, -1
	}
	c := t.scan.Checks[t.scanCursor]
	head := c.Name + "   " + stripANSI(statusPill(c))
	dur := durationText(c)
	lines = append(lines, fitPaint(scanPaint(head, ui.TextPrimary, true)+"  "+scanPaint(dur, ui.TextSecondary, false), width, ui.SurfaceCard))
	lines = append(lines, scanPaint(scanTrunc(clipScan(c.Summary), width-2), ui.TextSecondary, false))
	lines = append(lines, "")
	lines = append(lines, t.detailSection(width, "CHECKED", "")...)
	for _, ln := range checkedLines(t.m, c) {
		lines = append(lines, t.meterLine(width, ln))
	}
	lines = append(lines, "")
	rule := scan.Rule(c.ID)
	if rule == "" {
		rule = "—"
	}
	lines = append(lines, t.detailSection(width, "RULE", "")...)
	for _, ln := range wrapText(foldASCII(rule), width-2) {
		lines = append(lines, scanPaint(ln, ui.TextPrimary, false))
	}
	lines = append(lines, "")
	probs := problemLines(c, 25)
	beside := ""
	if len(probs) == 0 {
		beside = "none"
	}
	lines = append(lines, t.detailSection(width, "PROBLEMS", beside)...)
	for _, ln := range probs {
		lines = append(lines, scanPaint(scanMiddle(ln, width-2), ui.TextSecondary, false))
	}
	lines = append(lines, "")
	lines = append(lines, t.detailSection(width, "WHY IT MATTERS", "")...)
	for _, ln := range wrapText(foldASCII(scan.Why(c.ID)), width-2) {
		lines = append(lines, scanPaint(ln, ui.TextPrimary, false))
	}
	lines = append(lines, "")
	np := scan.NotProven(c.ID)
	lines = append(lines, t.detailSection(width, "DOES NOT PROVE", np)...)
	if t.scn != nil {
		open := t.notCoveredOpen(len(lines), height)
		t.scn.ncOpen = open
	}
	lines = append(lines, "")
	block, rel := t.notCoveredBlock(width, t.ncOpen())
	ncRel = len(lines) + rel
	lines = append(lines, block...)
	return lines, ncRel
}

func (t Model) detailSection(width int, title, beside string) []string {
	label := ui.Title(ui.S(sanitize.Program(title)), ui.S(sanitize.Program(foldASCII(beside))), width)
	return []string{label}
}

func (t Model) meterLine(width int, text string) string {
	frac := 1.0
	if a, b, ok := leadingFrac(text); ok && b > 0 {
		frac = float64(a) / float64(b)
	}
	barW := 16
	if width < 40 {
		barW = 8
	}
	bar, _ := ui.Meter(frac, barW, ui.GradMeter)
	if ui.ColorOff() {
		bar = strings.Repeat("#", int(frac*float64(barW)))
		bar = padRight(bar, barW)
	}
	rest := scanTrunc(text, max(4, width-barW-1))
	return bar + " " + scanPaint(rest, ui.TextPrimary, false)
}

func (t Model) notCoveredOpen(detailLines, height int) bool {
	if t.scn != nil && t.scn.ncUser {
		return t.scn.ncOpen
	}
	if t.width < 140 {
		return false
	}
	body := len(notCoveredBody(t.m, 40))
	return detailLines+1+body <= height-6
}

func (t Model) ncOpen() bool {
	if t.scn == nil {
		return false
	}
	if t.scn.ncUser {
		return t.scn.ncOpen
	}
	return t.scn.ncOpen
}

func (t Model) notCoveredBlock(width int, open bool) ([]string, int) {
	mark := "▸"
	if open {
		mark = "▾"
	}
	if !ui.UTF8() {
		if open {
			mark = "v"
		} else {
			mark = ">"
		}
	}
	title := mark + " Not covered by any check (5)"
	chip := "i"
	gap := width - ui.Width(title) - ui.Width(chip) - 2
	if gap < 1 {
		gap = 1
	}
	row := paintOn(" "+title+strings.Repeat(" ", gap)+chip+" ", ui.TextPrimary, ui.SurfaceSeg, true)
	if ui.ColorOff() {
		row = ui.Fit(" "+title+strings.Repeat(" ", gap)+chip, width)
	}
	lines := []string{"", row}
	if !open {
		return lines, 1
	}
	for _, ln := range notCoveredBody(t.m, width-2) {
		lines = append(lines, scanPaint(ln, ui.TextSecondary, false))
	}
	return lines, 1
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

func (t Model) scanDetailPage(width, height int) []string {
	lines := []string{scanPaint("DETAIL", ui.TextPrimary, true)}
	if t.scanCursor >= 0 && t.scanCursor < len(t.scan.Checks) {
		detail, rel := t.detailLines(width, height)
		if rel >= 0 {
			t.addScanHit("scan:nc", 0, 2+len(lines)+rel, width, 1)
		}
		lines = append(lines, detail...)
	}
	lines = append(lines, scanPaint("esc back", ui.TextMuted, false))
	return lines
}

func (t Model) scanCodeLines(width, height int) []string {
	var lines []string
	title := "Scan › Code & pickles"
	if !ui.UTF8() {
		title = "Scan > Code & pickles"
	}
	lines = append(lines, scanPaint(title, ui.TextPrimary, true)+"  "+scanPaint("[ names only ]", ui.TextMuted, false))
	files := codeFiles(t.m)
	if len(files) == 0 {
		lines = append(lines, "")
		lines = append(lines, scanPaint(ui.G().NR+" Nothing flagged. No code-executing formats or auto_map entries were found by name.", ui.TextSecondary, false))
		return lines
	}
	lines = append(lines, scanPaint(fmt.Sprintf("%d flagged · nothing has been opened yet (stat only)", len(files)), ui.TextMuted, false))
	lines = append(lines, "")
	for i, f := range files {
		sel := t.scn != nil && i == t.scn.codeCur
		bg := ui.SurfaceCard
		if sel {
			bg = ui.SurfaceSel
		}
		row := f.mark + "  " + f.name + "   " + f.kind + "   " + f.size + "   contents not read"
		lines = append(lines, fitPaint(paintOn(scanTrunc(row, width-1), ui.TextPrimary, bg, false), width, bg))
		t.addScanHit("scan:code", 0, 2+len(lines)-1, width, 1)
	}
	return lines
}

type codeFile struct {
	name, kind, size, mark string
}

func codeFiles(m *model.Model) []codeFile {
	if m == nil {
		return nil
	}
	var out []codeFile
	mark := ui.G().Warn
	for _, f := range m.CodeExecFiles() {
		out = append(out, codeFile{
			name: clipScan(f.Name), kind: "code-exec", size: model.HumanBytes(f.Size), mark: mark,
		})
	}
	for _, e := range m.AutoMapEntries() {
		out = append(out, codeFile{
			name: clipScan(e.File + " auto_map"),
			kind: clipScan(e.Key + " → " + e.Target),
			size: "", mark: mark,
		})
	}
	return out
}

func (t Model) scanErrorLines(width, height int) []string {
	msg := t.scn.loadErr
	head := ui.G().Fail + " could not inspect: " + msg
	var lines []string
	for _, ln := range wrapText(head, width-2) {
		lines = append(lines, scanPaint(ln, ui.Fail, true))
	}
	lines = append(lines, "")
	lines = append(lines, scanPaint("q quit (exit 3)", ui.TextSecondary, false))
	return lines
}

func (t Model) scanQuietLoad(width, height int) []string {
	return []string{
		scanPaint("… reading", ui.TextMuted, false),
		framingLine(width >= 140, width),
	}
}

func (t Model) scanProgressLines(width, height int) []string {
	p := t.scn.prog
	title := "… " + progressTitle(p.Phase)
	extra := ""
	if p.Total > 0 {
		extra = fmt.Sprintf("   %d/%d", p.Done, p.Total)
		if p.Ranges > 0 {
			extra += fmt.Sprintf(" · %s ranges so far", model.Comma(int64(p.Ranges)))
		}
	}
	var lines []string
	lines = append(lines, bandLines(title+extra, "", "", ui.FillKindNR, 1, width)...)
	lines = append(lines, framingLine(false, width))
	lines = append(lines, "")
	running := progressCheck(p.Phase)
	for _, ph := range scanPhases {
		lines = append(lines, scanPaint(ph.name, ui.Accent, true))
		for _, id := range ph.ids {
			name := checkName(id)
			switch {
			case id == running:
				frac := 0.0
				if p.Total > 0 {
					frac = float64(p.Done) / float64(p.Total)
				}
				bar, _ := ui.Meter(frac, 16, ui.GradMeter)
				spin := spinGlyph(t.scn.spin, t.motion)
				pill := ui.Pill(ui.FillKindViolet, sanitize.Program("RUN"))
				if ui.ColorOff() {
					pill = "RUN"
					bar = padRight(strings.Repeat("#", int(frac*16)), 16)
				}
				row := spin + " " + pill + "  " + name + "  " + bar
				if p.Total > 0 {
					row += fmt.Sprintf("  %d/%d", p.Done, p.Total)
				}
				lines = append(lines, row)
			case phaseRank(id) > phaseRank(running):
				wait := "· WAIT"
				if ui.ColorOff() {
					wait = ". WAIT"
				}
				lines = append(lines, scanPaint(gutterGlyph()+" "+wait+"  "+name+"   after headers", ui.TextMuted, false))
			default:
				lines = append(lines, scanPaint(gutterGlyph()+" "+ui.G().Pass+" PASS  "+name, ui.TextSecondary, false))
			}
		}
	}
	return lines
}

func (t Model) strictBanner(width int) string {
	text := "  STRICT (--strict): warnings count as failures"
	if ui.ColorOff() {
		return ui.Fit("== STRICT (--strict): warnings count as failures ==", width)
	}
	bg, fg := ui.FillColors(ui.FillKindWarn)
	return ui.Paint(ui.Fit(text, width), fg.Hex(), bg.Hex(), true)
}

func bandLines(glyph, counts, right string, kind ui.FillKind, rows, width int) []string {
	if rows < 1 {
		rows = 1
	}
	text := glyph
	if counts != "" {
		text += "   " + counts
	}
	bg, fg := ui.FillColors(kind)
	if kind == ui.FillKindNR {
		bg, fg = ui.SurfaceSeg2, ui.TextPrimary
	}
	mid := 0
	if rows > 1 {
		mid = rows / 2
	}
	out := make([]string, rows)
	for i := 0; i < rows; i++ {
		if i != mid {
			if ui.ColorOff() {
				out[i] = ""
				continue
			}
			out[i] = ui.Paint(strings.Repeat(" ", width), bg.Hex(), bg.Hex(), false)
			continue
		}
		text = foldASCII(text)
		right = foldASCII(right)
		line := "  " + text
		if right != "" && ui.Width(line)+ui.Width(right)+4 < width {
			gap := width - ui.Width(line) - ui.Width(right) - 2
			line += strings.Repeat(" ", gap) + right
		}
		if ui.ColorOff() {
			out[i] = line
			continue
		}
		out[i] = ui.Paint(ui.Fit(line, width), fg.Hex(), bg.Hex(), true)
	}
	return out
}

func framingLine(long bool, width int) string {
	text := "Structure only" + ui.SepWord() + "not a malware or behavior scan"
	if long {
		text = "Structure only: files complete, headers well-formed, internally consistent. Not a malware or behavior scan."
	}
	chip := framingChip(long)
	gap := width - 2 - ui.Width(text) - ui.Width(chip) - 2
	if gap < 1 {
		text = scanTrunc(text, max(8, width-ui.Width(chip)-4))
		gap = 1
	}
	if ui.ColorOff() {
		return ui.Fit("  "+text+strings.Repeat(" ", gap)+chip, width)
	}
	left := ui.Paint(ui.Fit("  "+text+strings.Repeat(" ", gap), width-ui.Width(chip)-1), ui.TextPrimary.Hex(), ui.SurfaceSeg.Hex(), false)
	right := ui.Paint(" "+chip+" ", ui.TextPrimary.Hex(), ui.SurfaceSeg2.Hex(), false)
	return fitPaint(left+right, width, ui.SurfaceSeg)
}

func framingChip(long bool) string {
	if long {
		return "i  what this does not cover"
	}
	return "i more"
}

func verdictTitle(m *model.Model, res scan.Result) (string, ui.FillKind) {
	title := "STRUCTURE CONSISTENT"
	kind := ui.FillKindOK
	switch res.Overall {
	case scan.StatusWarn:
		n := 0
		for _, c := range res.Checks {
			if c.EffectiveStatus == scan.StatusWarn {
				n++
			}
		}
		word := "WARNINGS"
		if n == 1 {
			word = "WARNING"
		}
		title = fmt.Sprintf("STRUCTURE CONSISTENT · REVIEW %d %s", n, word)
		kind = ui.FillKindWarn
	case scan.StatusFail:
		if strictOnly(res) {
			title = "FAILS UNDER --strict"
		} else {
			title = "STRUCTURE PROBLEMS FOUND"
		}
		kind = ui.FillKindFail
	case scan.StatusSkip, "":
		title = "NOTHING INSPECTED"
		kind = ui.FillKindNR
	}
	if m != nil && m.Partial {
		title += " · INSPECTION INCOMPLETE"
		if kind == ui.FillKindOK {
			kind = ui.FillKindWarn
		}
	}
	if m != nil && strings.HasPrefix(m.Format, "gguf") {
		title = "NOTHING INSPECTED"
		kind = ui.FillKindNR
	}
	return foldASCII(title), kind
}

func glyphWord(kind ui.FillKind) string {
	g := ui.G()
	switch kind {
	case ui.FillKindOK:
		return g.Pass
	case ui.FillKindWarn:
		return g.Warn
	case ui.FillKindFail:
		return g.Fail
	default:
		return g.NR
	}
}

func strictOnly(res scan.Result) bool {
	if !res.Strict || res.Overall != scan.StatusFail {
		return false
	}
	for _, c := range res.Checks {
		if c.Status == scan.StatusFail {
			return false
		}
	}
	return true
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

func scanStatLine(m *model.Model) string {
	if m == nil {
		return ""
	}
	shards := len(m.Shards)
	readable := 0
	ranges := 0
	for _, s := range m.Shards {
		if s != nil && s.Header != nil {
			readable++
			ranges += len(s.Header.Tensors)
		}
	}
	entries, total := 0, 0
	if m.Index != nil {
		total = len(m.Index.WeightMap)
		entries = total
	}
	return fmt.Sprintf("%d/%d shards · %s ranges · %s/%s index entries · %s",
		readable, shards, model.Comma(int64(ranges)), model.Comma(int64(entries)), model.Comma(int64(total)), model.HumanBytes(m.TotalSize))
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

func passSample() scan.Check {
	return scan.Check{Status: scan.StatusPass, EffectiveStatus: scan.StatusPass}
}

func statusToken(c scan.Check) ui.Token {
	switch c.EffectiveStatus {
	case scan.StatusPass:
		return ui.OK
	case scan.StatusWarn:
		return ui.Warn
	case scan.StatusFail:
		return ui.Fail
	default:
		return ui.TextMuted
	}
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

func gutterGlyph() string {
	if !ui.UTF8() || ui.ColorOff() {
		return "|"
	}
	g := ui.G().Edge
	if g == "" {
		return "|"
	}
	return g
}

func anyPromoted(res scan.Result) bool {
	for _, c := range res.Checks {
		if c.Status == scan.StatusWarn && c.EffectiveStatus == scan.StatusFail {
			return true
		}
	}
	return false
}

func checkedLines(m *model.Model, c scan.Check) []string {
	if m == nil {
		return []string{clipScan(c.Summary)}
	}
	switch c.ID {
	case scan.CheckHeader:
		return []string{clipScan(c.Summary)}
	case scan.CheckFiles:
		return []string{fileChecked(m)}
	case scan.CheckOffsets:
		return []string{shardFrac(m), offsetChecked(m)}
	case scan.CheckIndex:
		return []string{clipScan(c.Summary)}
	case scan.CheckCodeExec:
		return []string{fmt.Sprintf("%d files · %d auto_map entries", len(m.CodeExecFiles()), len(m.AutoMapEntries()))}
	default:
		return []string{clipScan(c.Summary)}
	}
}

func checkedOne(m *model.Model, c scan.Check) string {
	ls := checkedLines(m, c)
	if len(ls) == 0 {
		return ""
	}
	return ls[0]
}

func shardFrac(m *model.Model) string {
	n := len(m.Shards)
	ok := 0
	for _, s := range m.Shards {
		if s != nil && s.Header != nil {
			ok++
		}
	}
	return fmt.Sprintf("%d/%d shards", ok, n)
}

func offsetChecked(m *model.Model) string {
	ranges, shards := 0, 0
	for _, s := range m.Shards {
		if s != nil && s.Header != nil {
			shards++
			ranges += len(s.Header.Tensors)
		}
	}
	return fmt.Sprintf("%s ranges in %d shards · %s data", model.Comma(int64(ranges)), shards, model.HumanBytes(m.TensorBytes))
}

func fileChecked(m *model.Model) string {
	broken, escapes, unref := 0, 0, 0
	indexed := map[string]struct{}{}
	if m.Index != nil {
		for _, name := range m.Index.WeightMap {
			indexed[name] = struct{}{}
		}
	}
	for _, s := range m.Shards {
		if s == nil {
			continue
		}
		if !localName(s.Name) {
			escapes++
		}
	}
	for _, f := range m.Files {
		if f.Broken {
			broken++
		}
	}
	if m.Index != nil {
		for _, s := range m.Shards {
			if s == nil || !strings.HasSuffix(s.Name, ".safetensors") {
				continue
			}
			if _, ok := indexed[s.Name]; !ok {
				unref++
			}
		}
	}
	n := len(m.Shards)
	if m.Index != nil {
		n = len(indexed)
	}
	return fmt.Sprintf("%d/%d indexed shards · %d broken links · %d path escapes · %d unreferenced", n, n, broken, escapes, unref)
}

func localName(name string) bool {
	return name != "" && !strings.Contains(name, "..") && !strings.HasPrefix(name, "/")
}

func problemLines(c scan.Check, limit int) []string {
	var out []string
	for _, d := range c.Details {
		if strings.HasPrefix(d, "...") || strings.HasPrefix(d, "…") {
			out = append(out, "… and more · inspect --json for all")
			break
		}
		out = append(out, clipScan(d))
		if len(out) >= limit {
			break
		}
	}
	return out
}

func realProblems(c scan.Check) int {
	n := 0
	for _, d := range c.Details {
		if strings.HasPrefix(d, "...") || strings.HasPrefix(d, "…") {
			continue
		}
		n++
	}
	return n
}

// scanMiddle keeps the start and the end of a hostile path. A line that
// fits is unchanged, so the sanitized name is not cut off the front.
func scanMiddle(s string, width int) string {
	if width < 1 {
		return ""
	}
	if ui.Width(s) <= width {
		return s
	}
	ell := foldASCII(ui.G().Ellipsis)
	ew := ui.Width(ell)
	if width <= ew+2 {
		return scanTrunc(s, width)
	}
	budget := width - ew
	headW := budget / 2
	tailW := budget - headW
	return takeWidth(s, headW) + ell + takeTailWidth(s, tailW)
}

func takeWidth(s string, width int) string {
	if width < 1 {
		return ""
	}
	var b strings.Builder
	n := 0
	for _, r := range s {
		rw := ui.Width(string(r))
		if n+rw > width {
			break
		}
		b.WriteRune(r)
		n += rw
	}
	return b.String()
}

func takeTailWidth(s string, width int) string {
	if width < 1 {
		return ""
	}
	rs := []rune(s)
	n := 0
	i := len(rs)
	for i > 0 {
		rw := ui.Width(string(rs[i-1]))
		if n+rw > width {
			break
		}
		i--
		n += rw
	}
	return string(rs[i:])
}

func ruleShort(id string) string {
	switch id {
	case scan.CheckHeader:
		return "length + one JSON object"
	case scan.CheckFiles:
		return "named shards exist inside the snapshot"
	case scan.CheckOffsets:
		return "in-bounds, = dtype×shape, no overlap"
	case scan.CheckIndex:
		return "weight_map entries found in headers"
	case scan.CheckCodeExec:
		return "names only; nothing imported or run"
	default:
		return ""
	}
}

func shortProve(s string) string {
	s = strings.TrimPrefix(s, "that ")
	if i := strings.Index(s, ","); i > 0 && i < 40 {
		return s[:i]
	}
	return scanTrunc(s, 48)
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

func scanPaint(s string, fg ui.Token, bold bool) string {
	s = foldASCII(s)
	if ui.ColorOff() || s == "" {
		return s
	}
	return ui.Paint(s, fg.Hex(), "", bold)
}

func paintOn(s string, fg, bg ui.Token, bold bool) string {
	if s == "" {
		return ""
	}
	if ui.ColorOff() {
		return s
	}
	return ui.Paint(s, fg.Hex(), bg.Hex(), bold)
}

func fitPaint(s string, width int, bg ui.Token) string {
	w := ui.Width(s)
	if w > width {
		return ui.Fit(s, width)
	}
	if w == width || ui.ColorOff() {
		return ui.Fit(s, width)
	}
	return s + paintOn(strings.Repeat(" ", width-w), ui.TextMuted, bg, false)
}

func padRight(s string, n int) string {
	w := ui.Width(s)
	if w >= n {
		return scanTrunc(s, n)
	}
	return s + strings.Repeat(" ", n-w)
}

func scanZip(left, right []string, leftW, total int) []string {
	n := len(left)
	if len(right) > n {
		n = len(right)
	}
	out := make([]string, n)
	for i := 0; i < n; i++ {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		gap := leftW - ui.Width(l)
		if gap < 2 {
			gap = 2
		}
		out[i] = ui.Fit(l+strings.Repeat(" ", gap)+r, total)
	}
	return out
}

func leadingFrac(s string) (a, b int, ok bool) {
	// first n/m in the string
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			continue
		}
		j := i
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		if j >= len(s) || s[j] != '/' {
			i = j
			continue
		}
		k := j + 1
		if k >= len(s) || s[k] < '0' || s[k] > '9' {
			continue
		}
		e := k
		for e < len(s) && s[e] >= '0' && s[e] <= '9' {
			e++
		}
		fmt.Sscanf(s[i:j], "%d", &a)
		fmt.Sscanf(s[k:e], "%d", &b)
		return a, b, true
	}
	return 0, 0, false
}

func progressTitle(phase string) string {
	switch phase {
	case model.PhaseWalk:
		return "READING FILES"
	case model.PhaseHeaders:
		return "READING SHARD HEADERS"
	case model.PhaseLayout:
		return "CHECKING OFFSETS"
	case model.PhaseIndex:
		return "READING INDEX"
	default:
		return "READING"
	}
}

func progressCheck(phase string) string {
	switch phase {
	case model.PhaseWalk:
		return scan.CheckFiles
	case model.PhaseHeaders:
		return scan.CheckHeader
	case model.PhaseLayout:
		return scan.CheckOffsets
	case model.PhaseIndex:
		return scan.CheckIndex
	default:
		return scan.CheckHeader
	}
}

func phaseRank(id string) int {
	switch id {
	case scan.CheckFiles:
		return 1
	case scan.CheckHeader:
		return 2
	case scan.CheckOffsets:
		return 3
	case scan.CheckIndex:
		return 4
	default:
		return 9
	}
}

func checkName(id string) string {
	switch id {
	case scan.CheckHeader:
		return "Shard headers parse"
	case scan.CheckFiles:
		return "Files present"
	case scan.CheckOffsets:
		return "Offsets & sizes"
	case scan.CheckIndex:
		return "Index matches headers"
	case scan.CheckCodeExec:
		return "Code-executing formats"
	case scan.CheckCode:
		return "Code & pickle contents"
	case scan.CheckHash:
		return "Hashes vs revision"
	case scan.CheckMetadata:
		return "Config vs tensor shapes"
	default:
		return id
	}
}

var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func spinGlyph(frame int, motion bool) string {
	if !motion || ui.ColorOff() || !ui.UTF8() {
		if ui.UTF8() {
			return "…"
		}
		return "..."
	}
	if len(spinFrames) == 0 {
		return "…"
	}
	if frame < 0 {
		frame = 0
	}
	return spinFrames[frame%len(spinFrames)]
}
