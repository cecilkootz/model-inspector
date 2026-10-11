package tui

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/sanitize"
	"github.com/cecilkootz/model-inspector/internal/scan"
	"github.com/cecilkootz/model-inspector/internal/tui/ui"
)

// Layout names for Options.Layout and --layout.
const (
	LayoutTabs   = "tabs"
	LayoutPanels = "panels"
)

const (
	shellMinW     = 84 // narrower falls back to the tab layout
	shellMinH     = 16
	shellCompactH = 24 // shorter: unfocused panels keep only their title row
	shellSideMin  = 34
	shellSideMax  = 48
)

// shellState is the panels layout. Like cfg and lay it hangs off Model by
// pointer so value receivers can update it.
type shellState struct {
	side     int // 1..4: the panel whose list is open and whose selection the detail shows
	inDetail bool
	top      int // detail scroll, for panels whose detail is plain lines
	modelCur int
	scroll   [4]int
	filter   [4]shellFilter
	fcache   [4]filterCache

	panelRect  [4]ui.Rect
	detailRect ui.Rect
	hits       []hit
}

func newShellState() *shellState { return &shellState{side: 1} }

func (s *shellState) framePart() string {
	if s == nil {
		return ""
	}
	return fmt.Sprintf("|sh %d %v %d %d %q", s.side, s.inDetail, s.top, s.modelCur, s.filterFingerprint())
}

func (s *shellState) focusSide(n int) {
	if n < 1 || n > 4 {
		return
	}
	if n != s.side {
		s.top = 0
	}
	s.side, s.inDetail = n, false
}

// shellFocus moves focus to side panel n. Switching panels points the tree
// behind Layers and Config at the panel's selection.
func (t *Model) shellFocus(n int) {
	changed := n != t.sh.side
	t.filterStopEditing()
	t.sh.focusSide(n)
	if changed {
		t.sidePanels()[n-1].move(0)
	}
}

// shellOn is true when the panels layout is drawing. Loading, a load error
// and small terminals use the tab layout.
func (t Model) shellOn() bool {
	return t.layout == LayoutPanels && t.ready && t.m != nil && t.sh != nil &&
		t.width >= shellMinW && t.height >= shellMinH &&
		(t.scn == nil || t.scn.loadErr == "")
}

func shellSideWidth(w int) int { return clampInt(w/3, shellSideMin, shellSideMax) }

func (t Model) shellBodyHeight() int { return max(1, t.height-2) }

func (t Model) renderShell() string {
	W, H := t.width, t.height
	sh := t.sh
	ps := (&t).sidePanels()
	sideW := shellSideWidth(W)
	detW := W - sideW
	bodyH := H - 2
	iw := sideW - 4
	open := sh.side - 1

	var sums [4][]string
	var hs [4]int
	used := 0
	for i, p := range ps {
		if i == open {
			continue
		}
		if H < shellCompactH {
			hs[i] = 1
		} else {
			s := p.summary(iw)
			if len(s) > 3 {
				s = s[:3]
			}
			sums[i] = s
			hs[i] = 2 + clampInt(len(s), 1, 3)
		}
		used += hs[i]
	}
	hs[open] = max(3, bodyH-used)

	sh.hits = sh.hits[:0]
	var side []string
	y := 1
	for i, p := range ps {
		h := hs[i]
		spec := ui.PanelSpec{
			Num: i + 1, Title: p.title(), Counter: ui.Chrome(p.counter()),
			Width: sideW, Height: h, Focused: i == open && !sh.inDetail,
		}
		if f := sh.filter[i]; f.active() {
			spec.Counter = filterCounter(f.text, p.title(), sideW)
		}
		sh.panelRect[i] = ui.Rect{X: 0, Y: y, W: sideW, H: h}
		if i == open {
			spec.Body = t.shellList(p, i, y, h, iw)
		} else {
			spec.Body = sums[i]
		}
		side = append(side, ui.Panel(spec)...)
		y += h
	}

	active := ps[open]
	ih := bodyH - 2
	lines := active.detail(detW-4, ih)
	if _, drives := active.(detailDriver); drives {
		sh.top = 0
	} else {
		sh.top = clampInt(sh.top, 0, max(0, len(lines)-ih))
		lines = lines[min(sh.top, len(lines)):]
	}
	title, tag := ui.L("Detail"), ui.Safe{}
	if dt, ok := active.(detailTitled); ok {
		title, tag = dt.selection()
	}
	detail := ui.Panel(ui.PanelSpec{
		Num: 0, Title: title, Counter: tag, Body: lines,
		Width: detW, Height: bodyH, Focused: sh.inDetail,
	})
	sh.detailRect = ui.Rect{X: sideW, Y: 1, W: detW, H: bodyH}

	rows := make([]string, bodyH)
	for i := range rows {
		rows[i] = side[i] + detail[i]
	}
	body := strings.Join(rows, "\n")
	overlay := t.help || t.palette.open || t.disclosure
	if overlay {
		body = clipLines(t.overlay(body), bodyH, W)
	}
	t.fillShellZones(overlay)
	return t.shellHeader(W) + "\n" + body + "\n" + t.shellKeyLine(active)
}

// shellList draws the open panel's rows through a window that keeps the
// cursor visible, and records a hit rectangle per drawn row. A filter narrows
// the rows and, while it is edited, takes the last body row for its input.
func (t Model) shellList(p sidePanel, i, y, h, iw int) []string {
	sh := t.sh
	rows := p.rows(iw)
	n := h - 2
	var input []string
	if sh.filter[i].editing && n >= 2 {
		n--
		input = []string{filterInputRow(sh.filter[i].text, iw)}
	}
	order, skip := (&t).listOrder(p, len(rows))
	if len(order) == 0 {
		msg := ui.L("none")
		if sh.filter[i].active() {
			msg = ui.L("no matches")
		}
		body := []string{plainRow(msg, sanitize.Text{}, iw)}
		if input != nil {
			body = append(body, make([]string, n-1)...)
		}
		return append(body, input...)
	}
	cur := clampInt(p.cursor(), 0, len(rows)-1)
	cp := min(sort.SearchInts(order, cur), len(order)-1)
	// A label directly above the cursor row stays on screen with it.
	anchor := cp
	for anchor > 0 && skip != nil && skip[order[anchor-1]] {
		anchor--
	}
	start := clampInt(sh.scroll[i], 0, max(0, len(order)-n))
	if cp < start {
		start = cp
	}
	if cp >= start+n {
		start = cp - n + 1
	}
	if anchor < start && cp < anchor+n {
		start = anchor
	}
	sh.scroll[i] = start
	end := min(len(order), start+n)
	body := make([]string, 0, h-2)
	for pos := start; pos < end; pos++ {
		r := order[pos]
		body = append(body, rows[r])
		sh.hits = append(sh.hits, hit{
			id: "sh:row:" + strconv.Itoa(p.num()) + ":" + strconv.Itoa(r),
			x:  1, y: y + 1 + pos - start, w: sh.panelRect[i].W - 2, h: 1,
		})
	}
	if input != nil {
		body = append(body, make([]string, n-len(body))...)
		body = append(body, input...)
	}
	return body
}

func (t Model) fillShellZones(overlay bool) {
	if t.zones == nil {
		return
	}
	sh := t.sh
	t.zones.Reset()
	if !overlay {
		t.zones.Add("sh:detail", sh.detailRect)
		for i, r := range sh.panelRect {
			t.zones.Add("sh:panel:"+strconv.Itoa(i+1), r)
		}
		for _, h := range sh.hits {
			t.zones.Add(h.id, ui.Rect{X: h.x, Y: h.y, W: h.w, H: h.h})
		}
	}
	if t.palette.open {
		t.addPaletteZones()
	}
}

func shellName(m *model.Model) string {
	if m.RepoID != "" {
		if i := strings.LastIndex(m.RepoID, "/"); i >= 0 {
			return m.RepoID[i+1:]
		}
		return m.RepoID
	}
	return filepath.Base(m.Dir)
}

func (t Model) shellHeader(width int) string {
	m := t.m
	size := model.HumanBytes(m.TotalSize)
	if incomplete(m) {
		size += " (incomplete)"
	}
	left := []ui.Span{
		{Text: ui.L("model-inspector"), Tone: ui.ToneAccent, Bold: true},
		{Text: sanitize.From(shellName(m)), Tone: ui.ToneStrong, Bold: true},
		{Text: sanitize.Program(size), Tone: ui.ToneBody},
	}
	if strings.HasPrefix(m.Format, "gguf") {
		left = append(left, ui.Span{Text: ui.L("gguf"), Tone: ui.ToneBody})
	} else {
		left = append(left,
			ui.Span{Text: sanitize.Program(quantHeader(m)), Tone: ui.ToneBody},
			ui.Span{Text: sanitize.Program("~" + model.HumanCount(m.TotalLogicalParams) + " params"), Tone: ui.ToneBody},
		)
	}
	right := []ui.Span{t.shellVerdict(), {Text: ui.L("headers-only")}}
	return ui.HeaderLine(left, right, width)
}

func (t Model) shellVerdict() ui.Span {
	g := ui.G()
	if incomplete(t.m) {
		return ui.Span{Text: sanitize.Program(g.Warn + " incomplete"), Tone: ui.ToneWarn, Bold: true}
	}
	_, warn, fail, _, _ := bandCounts(t.scan)
	switch t.scan.Overall {
	case scan.StatusFail:
		return ui.Span{Text: sanitize.Program(g.Fail + " " + strconv.Itoa(fail) + " fail"), Tone: ui.ToneFail, Bold: true}
	case scan.StatusWarn:
		return ui.Span{Text: sanitize.Program(g.Warn + " " + strconv.Itoa(warn) + " warn"), Tone: ui.ToneWarn, Bold: true}
	case scan.StatusPass:
		return ui.Span{Text: sanitize.Program(g.Pass + " checks pass")}
	default:
		return ui.Span{Text: sanitize.Program(nrGlyph() + " not run")}
	}
}

func (t Model) shellKeyLine(p sidePanel) string {
	if t.notice != "" {
		return ui.KeyLine(nil, sanitize.From(t.notice), t.width)
	}
	hints := t.shellKeys(p)
	if t.sh.inDetail {
		hints = detailHints(p)
	} else if t.sh.filter[p.num()-1].editing {
		hints = t.filterEditHints()
	}
	return ui.KeyLine(hints, shellOrigin(t.m), t.width)
}

func detailHints(p sidePanel) []ui.KeyHint {
	out := []ui.KeyHint{hint(upDown(), "scroll"), hint("esc", "back"), hint("?", "keys")}
	if _, ok := p.(detailDriver); ok {
		fold := "←→"
		if !ui.UTF8() {
			fold = "<->"
		}
		out = append(out, hint(fold, "fold"))
	}
	return append(out, hint("1-4", "panels"), hint("tab", "next panel"))
}

// shellOrigin is `org/repo · rev7` for the key line.
func shellOrigin(m *model.Model) sanitize.Text {
	name := m.RepoID
	if name == "" {
		name = filepath.Base(m.Dir)
	}
	s := sanitize.From(name).String()
	if r := []rune(m.Revision); len(r) > 0 {
		s += ui.SepWord() + sanitize.From(string(r[:min(len(r), 7)])).String()
	}
	return sanitize.Program(s)
}

func (t Model) shellModeName() string {
	p := (&t).sidePanels()[t.sh.side-1]
	return strings.ToUpper(p.title().String())
}

func (t Model) shellHelpBody() string {
	p := (&t).sidePanels()[t.sh.side-1]
	var b strings.Builder
	line := func(key, desc string) { fmt.Fprintf(&b, "  %s  %s\n", chip(key), desc) }
	b.WriteString(strings.ToUpper(p.title().String()) + "\n")
	for _, h := range p.keys() {
		switch h.Key.String() {
		case "?", "1-4", "tab":
			continue
		}
		line(h.Key.String(), h.Desc.String())
	}
	if _, ok := p.(filterable); ok {
		b.WriteString("\nFILTER\n")
		line("/", "filter the list by name")
		line(enterKey(), "keep the filter")
		line("esc", "clear the filter")
	}
	b.WriteString("\nDETAIL PANE\n")
	for _, h := range detailHints(p) {
		switch h.Key.String() {
		case "?", "1-4", "tab":
			continue
		}
		line(h.Key.String(), h.Desc.String())
	}
	b.WriteString("\nANYWHERE\n")
	line("1-4 tab", "focus a panel")
	line("0", "focus the detail pane")
	line("] [", "next / previous problem")
	line(": ctrl+k", "command palette")
	line("m", "mouse on / off")
	line("q ctrl+c", "quit")
	mouse := "Mouse is on. Hold Shift to select text."
	if !t.mouse {
		mouse = "Mouse is off. Hold Shift to select text."
	}
	b.WriteString("\n" + mouse + "\n")
	return b.String()
}

func (t Model) shellCommands() []ui.Match {
	list := []struct{ id, title, key string }{
		{"sh:1", "Focus Model", "1"},
		{"sh:2", "Focus Checks", "2"},
		{"sh:3", "Focus Layers", "3"},
		{"sh:4", "Focus Config", "4"},
		{"sh:0", "Focus detail pane", "0"},
		{"sh:next", "Next problem", "]"},
		{"sh:prev", "Previous problem", "["},
		{"help", "Toggle help", "?"},
		{"mouse", "Toggle mouse", "m"},
		{"quit", "Quit", "q"},
	}
	out := make([]ui.Match, 0, len(list))
	for i, c := range list {
		out = append(out, ui.Match{
			ID: c.id, Title: ui.S(sanitize.Program(c.title)), Key: ui.S(sanitize.Program(c.key)),
			Order: i, Enable: true,
		})
	}
	return out
}

// runShellCommand handles the shell-only palette ids. Help, mouse and quit
// share the tab layout's handlers.
func (t *Model) runShellCommand(id string) bool {
	switch id {
	case "sh:1", "sh:2", "sh:3", "sh:4":
		t.shellFocus(int(id[3] - '0'))
	case "sh:0":
		t.sh.inDetail = true
	case "sh:next":
		t.shellJump(1)
	case "sh:prev":
		t.shellJump(-1)
	default:
		return false
	}
	return true
}

// shellJump selects the next or previous warning or failure and focuses
// Checks, wrapping at the ends.
func (t *Model) shellJump(dir int) {
	c := (&checksPanel{t})
	probs := c.problems()
	if sel := t.selectable(c); sel != nil {
		probs = slices.DeleteFunc(probs, func(i int) bool { _, ok := slices.BinarySearch(sel, i); return !ok })
	}
	if len(probs) == 0 {
		t.notice = "no warnings or failures"
		return
	}
	cur := c.cursor()
	target := probs[0]
	if dir < 0 {
		target = probs[len(probs)-1]
	}
	for i, at := range probs {
		if dir > 0 && at > cur {
			target = at
			break
		}
		if dir < 0 && at < cur {
			target = probs[i]
		}
	}
	c.move(target - cur)
	t.sh.top = 0
	t.shellFocus(2)
}

func (t *Model) shellMove(p sidePanel, d int) {
	sh := t.sh
	if !sh.inDetail {
		if !t.filterMove(p, d) {
			p.move(d)
		}
		sh.top = 0
		return
	}
	if drv, ok := p.(detailDriver); ok {
		drv.scrollDetail(d)
		return
	}
	sh.top = max(0, sh.top+d)
}

func (t *Model) shellCopy() {
	switch t.sh.side {
	case 2:
		t.copyScan(false)
	case 3:
		(&layersPanel{t}).copySelection()
	case 4:
		t.copyConfig(false)
	default:
		t.notice = "nothing to copy"
	}
}

func (t Model) updateShellKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	if t.help {
		switch k {
		case "?", "esc", "q":
			t.help = false
		}
		return t, nil
	}
	sh := t.sh
	p := (&t).sidePanels()[sh.side-1]
	page := max(1, t.shellBodyHeight()/2)
	if !sh.inDetail && sh.filter[sh.side-1].editing {
		(&t).filterKey(p, msg)
		return t, nil
	}
	if f, ok := p.(sideFolder); ok && !sh.inDetail && f.foldSide(k) {
		(&t).filterSnap(p)
		return t, nil
	}
	switch k {
	case "/":
		if !sh.inDetail {
			(&t).filterOpen(p)
		}
	case "q":
		return t, tea.Quit
	case "?":
		t.help = true
	case ":", "ctrl+k":
		t.palette = palState{open: true}
		t.inputFocused = true
	case "m":
		return t.toggleMouse()
	case "1", "2", "3", "4":
		(&t).shellFocus(int(k[0] - '0'))
	case "0":
		sh.inDetail = true
	case "enter":
		if !sh.inDetail {
			sh.inDetail = true
		} else if drv, ok := p.(detailDriver); ok {
			drv.foldDetail(k)
		}
	case "esc":
		if !sh.inDetail && sh.filter[sh.side-1].active() {
			(&t).filterClear(sh.side - 1)
		}
		sh.inDetail = false
	case "tab", "shift+tab":
		// Order is 1-4 then the detail pane.
		cur := sh.side - 1
		if sh.inDetail {
			cur = 4
		}
		if k == "tab" {
			cur = (cur + 1) % 5
		} else {
			cur = (cur + 4) % 5
		}
		if cur == 4 {
			sh.inDetail = true
		} else {
			(&t).shellFocus(cur + 1)
		}
	case "]":
		(&t).shellJump(1)
	case "[":
		(&t).shellJump(-1)
	case "y":
		(&t).shellCopy()
	case "r":
		if tg, ok := p.(detailToggler); ok {
			tg.toggleDetail()
		}
	case "up", "k":
		(&t).shellMove(p, -1)
	case "down", "j":
		(&t).shellMove(p, 1)
	case "pgup", "b", "u":
		(&t).shellMove(p, -page)
	case "pgdown", "f", "d":
		(&t).shellMove(p, page)
	case "home", "g":
		(&t).shellMove(p, -1<<30)
	case "end", "G":
		(&t).shellMove(p, 1<<30)
	case "left", "h", "right", "l", " ", "space":
		if sh.inDetail {
			if drv, ok := p.(detailDriver); ok {
				drv.foldDetail(k)
			}
		}
	}
	return t, nil
}

func (t Model) shellClick(id string, double bool) Model {
	sh := t.sh
	if t.help || t.palette.open || t.disclosure {
		return t
	}
	(&t).filterStopEditing()
	switch {
	case id == "sh:detail":
		sh.inDetail = true
	case strings.HasPrefix(id, "sh:panel:"):
		n, _ := strconv.Atoi(strings.TrimPrefix(id, "sh:panel:"))
		(&t).shellFocus(n)
	case strings.HasPrefix(id, "sh:row:"):
		var n, row int
		if _, err := fmt.Sscanf(id, "sh:row:%d:%d", &n, &row); err != nil || n < 1 || n > 4 {
			return t
		}
		(&t).shellFocus(n)
		p := (&t).sidePanels()[n-1]
		p.move(row - p.cursor())
		sh.top = 0
		if double {
			sh.inDetail = true
		}
	}
	return t
}

func (t Model) shellScroll(x, y, d int) Model {
	if y < 1 || y >= t.height-1 || t.sh == nil {
		return t
	}
	sh := t.sh
	ps := (&t).sidePanels()
	open := ps[sh.side-1]
	if x >= sh.detailRect.X {
		wasDetail := sh.inDetail
		sh.inDetail = true
		(&t).shellMove(open, d)
		sh.inDetail = wasDetail
		return t
	}
	r := sh.panelRect[sh.side-1]
	if y >= r.Y && y < r.Y+r.H {
		wasDetail := sh.inDetail
		sh.inDetail = false
		(&t).shellMove(open, d)
		sh.inDetail = wasDetail
	}
	return t
}
