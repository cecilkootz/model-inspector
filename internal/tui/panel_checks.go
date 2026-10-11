package tui

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/sanitize"
	"github.com/cecilkootz/model-inspector/internal/scan"
	"github.com/cecilkootz/model-inspector/internal/tui/ui"
)

// Checks

type checksPanel struct{ t *Model }

func (p *checksPanel) num() int { return 2 }

func (p *checksPanel) title() sanitize.Text { return ui.L("Checks") }

func nrGlyph() string {
	if ui.UTF8() {
		return "·"
	}
	return ui.G().NR
}

// checkMark is the row glyph for a check's effective status.
func checkMark(c scan.Check) (string, ui.Tone) {
	g := ui.G()
	switch c.EffectiveStatus {
	case scan.StatusPass:
		return g.Pass, ui.ToneMuted
	case scan.StatusWarn:
		return g.Warn, ui.ToneWarn
	case scan.StatusFail:
		return g.Fail, ui.ToneFail
	default:
		return nrGlyph(), ui.ToneMuted
	}
}

func isProblem(c scan.Check) bool {
	return c.EffectiveStatus == scan.StatusWarn || c.EffectiveStatus == scan.StatusFail
}

// isPromoted is a warning that --strict turned into a failure.
func isPromoted(c scan.Check) bool {
	return c.Status == scan.StatusWarn && c.EffectiveStatus == scan.StatusFail
}

func tonedCount(n int, glyph string, tok ui.Token) string {
	return ui.PaintToken(strconv.Itoa(n)+glyph, tok, nil, false)
}

func (p *checksPanel) counter() string {
	var pass, warn, fail, nr int
	for _, c := range p.t.scan.Checks {
		switch c.EffectiveStatus {
		case scan.StatusPass:
			pass++
		case scan.StatusWarn:
			warn++
		case scan.StatusFail:
			fail++
		default:
			nr++
		}
	}
	g := ui.G()
	var parts []string
	if pass > 0 {
		parts = append(parts, tonedCount(pass, g.Pass, ui.TextMuted))
	}
	if fail > 0 {
		parts = append(parts, tonedCount(fail, g.Fail, ui.Fail))
	}
	if warn > 0 {
		parts = append(parts, tonedCount(warn, g.Warn, ui.Warn))
	}
	if nr > 0 {
		parts = append(parts, tonedCount(nr, nrGlyph(), ui.TextMuted))
	}
	return strings.Join(parts, " ")
}

func (p *checksPanel) row(c scan.Check, width int, selected bool) string {
	glyph, tone := checkMark(c)
	value := durationText(c)
	if isPromoted(c) {
		value += " (warn)"
	}
	return ui.ListRow(ui.RowSpec{
		Glyph: sanitize.Program(glyph), Tone: tone,
		Name: sanitize.Program(c.Name), Value: sanitize.Program(value),
		Selected: selected,
	}, width)
}

func (p *checksPanel) rows(width int) []string {
	cur := p.cursor()
	out := make([]string, 0, len(p.t.scan.Checks))
	for i, c := range p.t.scan.Checks {
		out = append(out, p.row(c, width, i == cur))
	}
	return out
}

// summary lists problems first, then how many passed and how many were not
// run. More than three problems collapse into a "+N more" row.
func (p *checksPanel) summary(width int) []string {
	var out []string
	pass, nr := 0, 0
	for _, c := range p.t.scan.Checks {
		switch {
		case isProblem(c):
			out = append(out, p.row(c, width, false))
		case c.EffectiveStatus == scan.StatusPass:
			pass++
		default:
			nr++
		}
	}
	if len(out) > 3 {
		more := len(out) - 2
		out = append(out[:2], ui.ListRow(ui.RowSpec{
			Glyph: sanitize.Program(ui.G().Warn), Tone: ui.ToneWarn,
			Name: sanitize.Program("+" + strconv.Itoa(more) + " more problems"),
		}, width))
	}
	if pass > 0 {
		out = append(out, ui.ListRow(ui.RowSpec{
			Glyph: sanitize.Program(ui.G().Pass), Name: sanitize.Program(strconv.Itoa(pass) + " passed"),
		}, width))
	}
	if nr > 0 {
		out = append(out, ui.ListRow(ui.RowSpec{
			Glyph: sanitize.Program(nrGlyph()), Name: sanitize.Program(strconv.Itoa(nr) + " not run"),
		}, width))
	}
	if len(out) > 3 {
		out = out[:3]
	}
	return out
}

func (p *checksPanel) cursor() int {
	return clampInt(p.t.scanCursor, 0, max(0, len(p.t.scan.Checks)-1))
}

func (p *checksPanel) move(d int) {
	p.t.scanCursor = clampInt(p.t.scanCursor+d, 0, max(0, len(p.t.scan.Checks)-1))
}

func (p *checksPanel) selected() (scan.Check, bool) {
	cs := p.t.scan.Checks
	if len(cs) == 0 {
		return scan.Check{}, false
	}
	return cs[p.cursor()], true
}

func (p *checksPanel) selection() (sanitize.Text, ui.Safe) {
	c, ok := p.selected()
	if !ok {
		return ui.L("Checks"), ui.Safe{}
	}
	kind := ui.PillNR
	switch c.EffectiveStatus {
	case scan.StatusPass:
		kind = ui.PillPass
	case scan.StatusWarn:
		kind = ui.PillWarn
	case scan.StatusFail:
		kind = ui.PillFail
	}
	return sanitize.Program(c.Name), ui.Chrome(ui.StatusPill(kind))
}

func (p *checksPanel) keys() []ui.KeyHint {
	return moveHints(hint("]", "next problem"), hint("y", "copy"))
}

func (p *checksPanel) problems() []int {
	var out []int
	for i, c := range p.t.scan.Checks {
		if isProblem(c) {
			out = append(out, i)
		}
	}
	return out
}

// detail returns every line; the shell scrolls them with sh.top.
func (p *checksPanel) detail(w, _ int) []string {
	c, ok := p.selected()
	if !ok {
		return []string{ui.PaintToken("No check selected.", ui.TextMuted, nil, false)}
	}
	return checkDetail(p.t.m, c, p.cursor() == 0, w)
}

// notCoveredCount is the number of limits notCoveredBody lists.
const notCoveredCount = 5

// checkDetail lays out one check for a pane w cells wide. The disclosure of
// what no check covers is a section on the first check and a one-line footer
// on the others, so the disclaimer is on screen for every selection.
func checkDetail(m *model.Model, c scan.Check, first bool, w int) []string {
	b := detailBuilder{w: max(w, 1)}
	b.add(wrapRuns([]run{{text: clipScan(c.Summary), tone: ui.ToneBody}}, b.w))
	if isPromoted(c) {
		b.add(wrapRuns([]run{{text: foldASCII("warning promoted to failure by --strict"), tone: ui.ToneMuted}}, b.w))
	}

	problems, facts, more := splitDetails(c)
	b.section("Problems")
	glyph, tone := checkMark(c)
	lead := []run{{text: glyph + " ", tone: tone}}
	pad := []run{{text: strings.Repeat(" ", ui.Width(glyph)+1)}}
	for _, d := range problems {
		head, snippet := splitSnippet(clipScan(d))
		b.add(hang(lead, quoteRuns(head, ui.ToneStrong), b.w))
		if snippet != "" {
			b.add(hang(pad, []run{{text: snippet, tone: ui.ToneMuted}}, b.w))
		}
	}
	if len(problems) == 0 && more == "" {
		none := "none"
		switch c.EffectiveStatus {
		case scan.StatusPass:
		case scan.StatusWarn, scan.StatusFail:
			none = "no detail recorded"
		default:
			none = "not run"
		}
		b.add(wrapRuns([]run{{text: none, tone: ui.ToneMuted}}, b.w))
	}
	if len(facts) == 0 && more != "" {
		b.add(moreLine(more, b.w))
	}

	if len(facts) > 0 {
		b.section("Facts")
		b.facts(facts)
		if more != "" {
			b.add(moreLine(more, b.w))
		}
	}
	b.text("Rule", scan.Rule(c.ID), ui.ToneBody)
	b.text("Why it matters", scan.Why(c.ID), ui.ToneBody)
	b.text("Does not prove", sentence(scan.NotProven(c.ID)), ui.ToneBody)

	if first {
		b.section("Not covered by any check (" + strconv.Itoa(notCoveredCount) + ")")
		for _, ln := range notCoveredBody(m, b.w) {
			b.line(wrapRunsKeep(foldASCII(ln), ui.ToneBody, b.w))
		}
	} else {
		b.gap()
		b.add(wrapRuns([]run{{text: foldASCII("Structural checks only. Not a malware or behavior scan."), tone: ui.ToneMuted}}, b.w))
	}
	return b.lines
}

func moreLine(d string, w int) [][]run {
	return wrapRuns([]run{{text: clipScan(d) + ui.SepWord() + "inspect --json for all", tone: ui.ToneMuted}}, w)
}

// splitDetails separates findings from facts and takes off the trailing
// "... and N more" marker. Only the agent check mixes facts into Details:
// findings first, then facts, with the finding count leading the summary.
func splitDetails(c scan.Check) (problems, facts []string, more string) {
	d := c.Details
	if n := len(d); n > 0 && (strings.HasPrefix(d[n-1], "...") || strings.HasPrefix(d[n-1], "…")) {
		more, d = d[n-1], d[:n-1]
	}
	if c.ID != scan.CheckAgent {
		return d, nil, more
	}
	n := 0
	if c.Status != scan.StatusPass {
		n = len(d)
		if k, err := strconv.Atoi(strings.Fields(c.Summary + " 0")[0]); err == nil && k >= 0 {
			n = min(k, len(d))
		}
	}
	return d[:n], d[n:], more
}

// snippetRe finds where a finding hands over to the excerpt it matched.
var snippetRe = regexp.MustCompile(`^(.*? at byte \d+(?: \(\d+ matches?\))?): (.+)$`)

func splitSnippet(s string) (head, snippet string) {
	if m := snippetRe.FindStringSubmatch(s); m != nil {
		return m[1], m[2]
	}
	return s, ""
}

// sentence capitalizes and ends a fragment such as "that the index is
// authentic".
func sentence(s string) string {
	if s == "" {
		return ""
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	s = string(r)
	if !strings.HasSuffix(s, ".") {
		s += "."
	}
	return s
}

// run is text that is already safe to print.
type run struct {
	text string
	tone ui.Tone
	bold bool
}

func toneToken(t ui.Tone) ui.Token {
	switch t {
	case ui.ToneBody:
		return ui.TextSecondary
	case ui.ToneStrong:
		return ui.TextPrimary
	case ui.ToneAccent:
		return ui.Accent
	case ui.ToneWarn:
		return ui.Warn
	case ui.ToneFail:
		return ui.Fail
	default:
		return ui.TextMuted
	}
}

func paintRuns(line []run) string {
	var b strings.Builder
	for _, r := range line {
		b.WriteString(ui.PaintToken(r.text, toneToken(r.tone), nil, r.bold))
	}
	return b.String()
}

// quoteRuns mutes "quoted" spans; an unterminated quote stays plain.
func quoteRuns(s string, base ui.Tone) []run {
	parts := strings.Split(s, `"`)
	if len(parts) < 3 {
		return []run{{text: s, tone: base}}
	}
	out := []run{{text: parts[0], tone: base}}
	for i := 1; i < len(parts); i++ {
		switch {
		case i%2 == 1 && i < len(parts)-1:
			out = append(out, run{text: `"` + parts[i] + `"`, tone: ui.ToneMuted})
		case i%2 == 1:
			out = append(out, run{text: `"` + parts[i], tone: base})
		default:
			out = append(out, run{text: parts[i], tone: base})
		}
	}
	return out
}

type word struct {
	parts []run
	w     int
}

func splitWords(runs []run) []word {
	var words []word
	var cur word
	open := false
	for _, r := range runs {
		var b strings.Builder
		flush := func() {
			if b.Len() > 0 {
				cur.parts = append(cur.parts, run{text: b.String(), tone: r.tone, bold: r.bold})
				cur.w += ui.Width(b.String())
				b.Reset()
			}
		}
		for _, c := range r.text {
			if unicode.IsSpace(c) {
				flush()
				if open {
					words = append(words, cur)
					cur, open = word{}, false
				}
				continue
			}
			open = true
			b.WriteRune(c)
		}
		flush()
	}
	if open {
		words = append(words, cur)
	}
	return words
}

// wrapRuns breaks at spaces so no line is wider than width. A token wider
// than the pane is cut at the edge instead of overflowing.
func wrapRuns(runs []run, width int) [][]run {
	width = max(width, 1)
	var lines [][]run
	var cur []run
	cw := 0
	push := func() {
		if len(cur) > 0 {
			lines = append(lines, cur)
		}
		cur, cw = nil, 0
	}
	for _, wd := range splitWords(runs) {
		switch {
		case cw > 0 && cw+1+wd.w <= width:
			cur = append(cur, run{text: " "})
			cur = append(cur, wd.parts...)
			cw += 1 + wd.w
		case wd.w <= width:
			push()
			cur = append(cur, wd.parts...)
			cw = wd.w
		default:
			push()
			for _, pt := range wd.parts {
				var b strings.Builder
				flush := func() {
					if b.Len() > 0 {
						cur = append(cur, run{text: b.String(), tone: pt.tone, bold: pt.bold})
						b.Reset()
					}
				}
				for _, c := range pt.text {
					rw := ui.Width(string(c))
					if cw > 0 && cw+rw > width {
						flush()
						push()
					}
					b.WriteRune(c)
					cw += rw
				}
				flush()
			}
		}
	}
	push()
	return lines
}

// wrapRunsKeep is for text that is already wrapped: leading spaces stay.
func wrapRunsKeep(s string, tone ui.Tone, w int) []run {
	return []run{{text: scanTrunc(s, w), tone: tone}}
}

// hang wraps body behind lead and indents the continuation lines to match.
func hang(lead, body []run, w int) [][]run {
	lw := 0
	for _, r := range lead {
		lw += ui.Width(r.text)
	}
	indent := []run{{text: strings.Repeat(" ", lw)}}
	wrapped := wrapRuns(body, max(1, w-lw))
	if len(wrapped) == 0 {
		return [][]run{lead}
	}
	out := make([][]run, len(wrapped))
	for i, ln := range wrapped {
		pre := indent
		if i == 0 {
			pre = lead
		}
		out[i] = append(append([]run{}, pre...), ln...)
	}
	return out
}

type detailBuilder struct {
	w     int
	lines []string
}

func (b *detailBuilder) gap() {
	if len(b.lines) > 0 {
		b.lines = append(b.lines, "")
	}
}

func (b *detailBuilder) section(label string) {
	b.gap()
	b.lines = append(b.lines, ui.SectionLabel(sanitize.Program(label), b.w))
}

func (b *detailBuilder) add(lines [][]run) {
	for _, ln := range lines {
		b.lines = append(b.lines, paintRuns(ln))
	}
}

func (b *detailBuilder) line(r []run) { b.lines = append(b.lines, paintRuns(r)) }

// text is a labelled paragraph; an empty body leaves the section out.
func (b *detailBuilder) text(label, body string, tone ui.Tone) {
	if body == "" {
		return
	}
	b.section(label)
	b.add(wrapRuns([]run{{text: foldASCII(body), tone: tone}}, b.w))
}

const factKeyMax = 20

// facts prints `key  value` rows with the keys muted and aligned, unless the
// keys would take more than a third of the pane.
func (b *detailBuilder) facts(facts []string) {
	type kv struct{ k, v string }
	rows := make([]kv, len(facts))
	kw := 0
	for i, f := range clipFacts(facts) {
		rows[i] = kv{v: f}
		if at := strings.Index(f, ": "); at > 0 && ui.Width(f[:at]) <= factKeyMax {
			rows[i] = kv{k: f[:at], v: f[at+2:]}
			kw = max(kw, ui.Width(f[:at]))
		}
	}
	if kw > b.w/3 {
		kw = 0
		for i := range rows {
			if rows[i].k != "" {
				rows[i] = kv{v: rows[i].k + ": " + rows[i].v}
			}
		}
	}
	for _, r := range rows {
		if r.k == "" {
			b.add(wrapRuns(quoteRuns(r.v, ui.ToneStrong), b.w))
			continue
		}
		lead := []run{{text: r.k + strings.Repeat(" ", kw-ui.Width(r.k)+2), tone: ui.ToneMuted}}
		b.add(hang(lead, quoteRuns(r.v, ui.ToneStrong), b.w))
	}
}

func clipFacts(facts []string) []string {
	out := make([]string, len(facts))
	for i, f := range facts {
		out[i] = clipScan(f)
	}
	return out
}
