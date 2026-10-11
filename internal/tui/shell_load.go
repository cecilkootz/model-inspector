package tui

import (
	"fmt"
	"math"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/sanitize"
	"github.com/cecilkootz/model-inspector/internal/tui/ui"
)

// phaseChecks is the scan that runs after the loader returns. The loader
// emits no event for it, so it counts as active from the end of the layout
// pass until the model arrives.
const phaseChecks = "checks"

// loadPhaseList is the loader's real order: index.json is read before the
// shard headers, and the layout pass runs inside the scan.
var loadPhaseList = [...]struct{ id, label string }{
	{model.PhaseWalk, "Walk snapshot"},
	{model.PhaseIndex, "Index"},
	{model.PhaseHeaders, "Shard headers"},
	{model.PhaseLayout, "Offsets & layout"},
	{phaseChecks, "Checks"},
}

type phaseState int

const (
	phasePending phaseState = iota
	phaseActive
	phaseDone
)

// loadPhases keeps the newest event of every phase, so a finished phase
// still shows its count after the next one starts.
type loadPhases struct {
	last [len(loadPhaseList)]model.Progress
	seen [len(loadPhaseList)]bool
}

func phaseIndex(id string) int {
	for i, p := range loadPhaseList {
		if p.id == id {
			return i
		}
	}
	return -1
}

func (l *loadPhases) note(p model.Progress) {
	if i := phaseIndex(p.Phase); i >= 0 {
		l.last[i], l.seen[i] = p, true
	}
}

// states derives each phase's state. A phase is done once a later one has
// started, or when its own last event reached its total. The walk reports
// done == total on every event, so only a later phase finishes it.
func (l *loadPhases) states() [len(loadPhaseList)]phaseState {
	var st [len(loadPhaseList)]phaseState
	latest := -1
	for i, ok := range l.seen {
		if ok {
			latest = i
		}
	}
	for i := range st {
		switch {
		case i < latest:
			st[i] = phaseDone
		case i == latest && l.complete(i):
			st[i] = phaseDone
			if i+1 < len(st) {
				st[i+1] = phaseActive
			}
		case i == latest:
			st[i] = phaseActive
		}
	}
	if latest < 0 {
		st[0] = phaseActive
	}
	return st
}

func (l *loadPhases) complete(i int) bool {
	if loadPhaseList[i].id == model.PhaseWalk {
		return false
	}
	p := l.last[i]
	return p.Done >= p.Total
}

func (l *loadPhases) active() int {
	for i, s := range l.states() {
		if s == phaseActive {
			return i
		}
	}
	return len(loadPhaseList) - 1
}

// short is the count shown in the Model panel row.
func (l *loadPhases) short(i int) string {
	if !l.seen[i] {
		return ""
	}
	p := l.last[i]
	switch loadPhaseList[i].id {
	case model.PhaseWalk:
		if p.Done == 1 {
			return "1 file"
		}
		return model.Comma(int64(p.Done)) + " files"
	case model.PhaseHeaders, model.PhaseLayout:
		if p.Total > 0 {
			return fmt.Sprintf("%d/%d", p.Done, p.Total)
		}
	}
	return ""
}

// long is the count shown beside the active phase in the detail pane.
func (l *loadPhases) long(i int) string {
	if !l.seen[i] {
		if loadPhaseList[i].id == phaseChecks {
			return "running the structural checks"
		}
		return ""
	}
	p := l.last[i]
	switch loadPhaseList[i].id {
	case model.PhaseWalk:
		return l.short(i)
	case model.PhaseHeaders, model.PhaseLayout:
		s := ""
		if p.Total > 0 {
			s = fmt.Sprintf("%d of %d shards", p.Done, p.Total)
		}
		if p.Ranges > 0 {
			if s != "" {
				s += ui.SepWord()
			}
			s += model.Comma(int64(p.Ranges)) + " ranges"
		}
		return s
	}
	return ""
}

// fraction is overall progress: finished phases plus the active phase's own
// share, so the meter only moves forward.
func (l *loadPhases) fraction() float64 {
	st := l.states()
	done := 0.0
	for i, s := range st {
		switch s {
		case phaseDone:
			done++
		case phaseActive:
			p := l.last[i]
			if l.seen[i] && p.Total > 0 && loadPhaseList[i].id != model.PhaseWalk {
				done += math.Min(1, float64(p.Done)/float64(p.Total))
			}
		}
	}
	return done / float64(len(st))
}

func (t Model) shellTooSmall() bool {
	return t.layout == LayoutPanels && t.ready && (t.width < shellMinW || t.height < shellMinH)
}

// shellLoad is true while the panels layout draws the loading frame or the
// load-error card in place of the loaded shell.
func (t Model) shellLoad() bool {
	return t.layout == LayoutPanels && t.ready && t.m == nil && t.sh != nil && t.scn != nil &&
		(t.scn.loading || t.scn.loadErr != "") && !t.shellTooSmall()
}

func (t Model) shellFrame() bool { return t.shellOn() || t.shellLoad() }

func centerLine(s string, width int) string {
	pad := max(0, (width-ui.Width(s))/2)
	return strings.Repeat(" ", pad) + s
}

func (t Model) renderTooSmall() string {
	W, H := t.width, t.height
	msg := ui.PaintToken(fmt.Sprintf("terminal too small (%dx%d)", W, H), ui.TextSecondary, nil, false)
	quit := ui.PaintToken("q", ui.TextPrimary, nil, false) + ui.PaintToken(" quit", ui.TextMuted, nil, false)
	rows := make([]string, H)
	mid := (H - 1) / 2
	rows[mid] = centerLine(msg, W)
	if mid+1 < H {
		rows[mid+1] = centerLine(quit, W)
	}
	return frame(strings.Join(rows, "\n"), W, H)
}

func (t Model) loadName() string {
	if t.scn.repo != "" {
		return t.scn.repo
	}
	return t.scn.dir
}

func (t Model) loadHeader(width int) string {
	name := midElide(sanitize.From(t.loadName()).String(), max(20, width/2))
	left := []ui.Span{
		{Text: ui.L("model-inspector"), Tone: ui.ToneAccent, Bold: true},
		{Text: sanitize.Program(name), Tone: ui.ToneStrong, Bold: true},
	}
	var right []ui.Span
	if t.scn.loadErr != "" {
		right = append(right, ui.Span{Text: sanitize.Program(ui.G().Fail + " load failed"), Tone: ui.ToneFail, Bold: true})
	} else {
		left = append(left, ui.Span{Text: sanitize.Program("reading headers" + ui.G().Ellipsis)})
	}
	right = append(right, ui.Span{Text: ui.L("headers-only")})
	return ui.HeaderLine(left, right, width)
}

func (t Model) loadKeyLine() string {
	hints := []ui.KeyHint{hint("q", "cancel")}
	switch {
	case t.scn.loadErr != "":
		hints = []ui.KeyHint{hint("q", "quit")}
	case t.shellPortrait() && t.sh.inDetail:
		hints = append(hints, hint("esc", "back"), hint("?", "keys"))
	case t.shellPortrait():
		hints = append(hints, hint(enterKey(), "detail"), hint("?", "keys"))
	default:
		hints = append(hints, hint("?", "keys"))
	}
	return ui.KeyLine(hints, sanitize.Text{}, t.width)
}

func loadGlyphs() (active, pending string) {
	if ui.UTF8() {
		return "•", "○"
	}
	return "*", "o"
}

// loadRow is one phase row. Colorless and ANSI-16 rows keep the two-cell
// gutter the list rows reserve, so the columns line up with the loaded shell.
func loadRow(glyph string, gTone ui.Token, gBold bool, name string, nTone ui.Token, nBold bool, count string, cTone ui.Token, width int) string {
	var b strings.Builder
	used := 0
	put := func(s string, tok ui.Token, bold bool) {
		if s == "" {
			return
		}
		b.WriteString(ui.PaintToken(s, tok, nil, bold))
		used += ui.Width(s)
	}
	if ui.ColorOff() || ui.Profile() == "ansi16" {
		put("  ", ui.TextMuted, false)
	}
	put(glyph, gTone, gBold)
	put(" ", ui.TextMuted, false)
	room := width - used
	cw := ui.Width(count)
	if cw > 0 {
		room -= cw + 1
	}
	if room < 1 {
		count, cw, room = "", 0, width-used
	}
	put(ui.Trunc(name, room, ui.G().Ellipsis), nTone, nBold)
	if cw > 0 {
		put(strings.Repeat(" ", max(0, width-used-cw)), ui.TextMuted, false)
		put(count, cTone, false)
	}
	return b.String()
}

func (t Model) loadRows(iw int) (rows []string, active int) {
	ph := &t.scn.phases
	st := ph.states()
	active = ph.active()
	g := ui.G()
	act, pend := loadGlyphs()
	failed := t.scn.loadErr != ""
	for i, p := range loadPhaseList {
		short := ph.short(i)
		switch {
		case st[i] == phaseDone:
			rows = append(rows, loadRow(g.Pass, ui.TextMuted, false, p.label, ui.TextPrimary, false, short, ui.TextMuted, iw))
		case st[i] == phaseActive && failed:
			rows = append(rows, loadRow(g.Fail, ui.Fail, true, p.label, ui.TextPrimary, false, short, ui.Fail, iw))
		case st[i] == phaseActive:
			rows = append(rows, loadRow(act, ui.Accent, true, p.label, ui.TextPrimary, false, short, ui.TextPrimary, iw))
		default:
			rows = append(rows, loadRow(pend, ui.TextMuted, false, p.label, ui.TextMuted, false, "", ui.TextMuted, iw))
		}
	}
	return rows, active
}

// loadWindow keeps the active row on screen when the panel is shorter than
// the phase list.
func loadWindow(rows []string, active, n int) []string {
	if n >= len(rows) {
		return rows
	}
	start := clampInt(active-n+1, 0, len(rows)-n)
	return rows[start : start+n]
}

func (t Model) loadDetail(iw, ih int) []string {
	if t.scn.loadErr != "" {
		return t.loadErrorLines(iw)
	}
	ph := &t.scn.phases
	a := ph.active()
	lines := []string{}
	for _, ln := range wrapText("Reading safetensors headers only; no weights are loaded.", iw) {
		lines = append(lines, paint(ln, ui.TextSecondary, false))
	}
	lines = append(lines, "")
	label, count := loadPhaseList[a].label, ph.long(a)
	if count != "" && ui.Width(label)+3+ui.Width(count) <= iw {
		lines = append(lines, paint(label, ui.TextPrimary, true)+"   "+paint(count, ui.TextSecondary, false))
	} else {
		lines = append(lines, paint(label, ui.TextPrimary, true))
		for _, ln := range wrapHard(count, iw) {
			lines = append(lines, paint(ln, ui.TextSecondary, false))
		}
	}
	bar, _ := ui.Meter(ph.fraction(), iw, ui.GradMeter)
	lines = append(lines, bar, "")
	lines = append(lines, paint(midElide(sanitize.From(t.scn.dir).String(), iw), ui.TextMuted, false))
	return lines
}

// loadErrorLines is the load-error card. The message was sanitized and
// clipped when it was stored.
func (t Model) loadErrorLines(iw int) []string {
	head := paint(ui.G().Fail+" could not inspect", ui.Fail, true)
	lines := []string{head, ""}
	for _, ln := range wrapHard(foldASCII(t.scn.loadErr), iw) {
		lines = append(lines, paint(ln, ui.TextSecondary, false))
	}
	return append(lines, "", paint("q quit (exit 3)", ui.TextMuted, false))
}

func (t Model) renderShellLoading() string {
	W, H := t.width, t.height
	sh := t.sh
	failed := t.scn.loadErr != ""
	portrait := t.shellPortrait()
	bodyH := H - 2
	sideW, detX := shellSideWidth(W), shellSideWidth(W)
	detW := W - detX
	if portrait {
		sideW, detX, detW = W, 0, W
	}
	// A failed load has nothing to stack, so the card takes the screen.
	showDetail := !portrait || sh.inDetail || failed
	showSide := !portrait || !showDetail
	detailFocus := sh.inDetail || failed

	sh.hits = sh.hits[:0]
	sh.panelRect, sh.detailRect = [4]ui.Rect{}, ui.Rect{}
	titles := [4]sanitize.Text{ui.L("Model"), ui.L("Checks"), ui.L("Layers"), ui.L("Config")}
	wait := "waiting for headers"
	if failed {
		wait = "not loaded"
	}
	var side []string
	if showSide {
		side = t.shellStack(0, sideW, bodyH, !detailFocus,
			func(i int) (sanitize.Text, ui.Safe) { return titles[i], ui.Safe{} },
			func(i, iw int) []string {
				return []string{paint(wait, ui.TextMuted, false), "", ""}
			},
			func(i, y, h, iw int) []string {
				rows, a := t.loadRows(iw)
				return loadWindow(rows, a, h-2)
			})
	}
	var detail []string
	if showDetail {
		title, tag := ui.L("Loading"), ui.Safe{}
		if failed {
			title, tag = ui.L("Load failed"), ui.Chrome(ui.StatusPill(ui.PillFail))
		}
		detail = ui.Panel(ui.PanelSpec{
			Num: 0, Title: title, Counter: tag, Body: t.loadDetail(detW-4, bodyH-2),
			Width: detW, Height: bodyH, Focused: detailFocus,
		})
		sh.detailRect = ui.Rect{X: detX, Y: 1, W: detW, H: bodyH}
	}

	body := t.joinColumns(side, detail, bodyH)
	if t.help {
		body = clipLines(t.overlay(body), bodyH, W)
	}
	// Loading has no clickable rows; drop the zones of the previous frame.
	t.zones.Reset()
	return t.loadHeader(W) + "\n" + body + "\n" + t.loadKeyLine()
}

func (t Model) loadHelpBody() string {
	var b strings.Builder
	line := func(key, desc string) { fmt.Fprintf(&b, "  %s  %s\n", chip(key), desc) }
	b.WriteString("LOADING\n")
	line("q ctrl+c", "cancel and quit")
	if t.shellPortrait() {
		line(enterKey()+" 0", "detail pane")
		line("esc", "back to the panels")
	}
	line("?", "close this help")
	b.WriteString("\nThe panels fill in when the headers are read.\n")
	return b.String()
}

func (t Model) updateLoadKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	if t.help {
		switch k {
		case "?", "esc", "q":
			t.help = false
		}
		return t, nil
	}
	switch k {
	case "q":
		return t, tea.Quit
	case "?":
		t.help = true
	case "0", "enter":
		t.sh.inDetail = true
	case "esc":
		t.sh.inDetail = false
	}
	return t, nil
}
