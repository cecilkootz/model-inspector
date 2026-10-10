package tui

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"origin.cursor.com/cecilkootz/model-inspector/internal/model"
	"origin.cursor.com/cecilkootz/model-inspector/internal/sanitize"
	"origin.cursor.com/cecilkootz/model-inspector/internal/scan"
	"origin.cursor.com/cecilkootz/model-inspector/internal/tui/ui"
)

type tickMsg time.Time

func tick() tea.Cmd {
	return tea.Tick(25*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func mouseDefault() bool {
	return os.Getenv("MODEL_INSPECTOR_MOUSE") != "0"
}

func motionDefault() bool {
	if os.Getenv("MODEL_INSPECTOR_MOTION") == "0" || os.Getenv("TERM") == "dumb" {
		return false
	}
	return !ui.ColorOff()
}

func (t *Model) apply(o Options) {
	if o.Mouse != nil {
		t.mouse = *o.Mouse
	}
	if o.Motion != nil {
		t.motion = *o.Motion
		if !t.motion {
			t.motionFrame = 6
		}
	}
	if o.Now != nil {
		t.now = o.Now
	}
}

func (t Model) clock() time.Time {
	if t.now != nil {
		return t.now()
	}
	return time.Now()
}

func (t Model) treeFocused() bool { return false }

func (t Model) identityBody() string {
	d := derived{}
	if t.derived != nil {
		d = *t.derived
	}
	frac := 1.0
	if t.motion && t.motionFrame < 6 {
		frac = float64(t.motionFrame) / 6
		if frac < 0.05 {
			frac = 0.05
		}
	}
	return renderIdentityView(t.m, d, t.width, t.bodyHeight(), idState{
		frac: frac, focusSecondary: t.focusSecondary, zoom: t.zoom, drill: t.drill,
	})
}

func (t Model) frameKey() string {
	var open []string
	for id, on := range t.scanOpen {
		if on {
			open = append(open, id)
		}
	}
	sort.Strings(open)
	detail := ""
	if t.detail != nil {
		detail = t.detail.Key
	}
	return fmt.Sprintf("%d %d %d %v %v %s %d %v %q %v %d %v %v %d %v %v %s %d %d %s %v",
		t.width, t.height, t.active, t.help, t.palette.open, t.palette.query, t.palette.cursor,
		t.disclosure, t.notice, t.mouse, t.motionFrame, t.focusSecondary, t.zoom, t.ratio, t.drill,
		t.blurred, detail, t.scanCursor, t.scanScroll, strings.Join(open, ","),
		t.identityVP.YOffset+t.configVP.YOffset)
}

func (t Model) renderFrame() string {
	if t.width < 60 || t.height < 16 {
		return frame(t.tooSmall(), t.width, t.height)
	}
	body := t.bodyView()
	if t.help || t.palette.open || t.disclosure {
		body = t.overlay(body)
	}
	body = clipLines(body, t.bodyHeight(), t.width)
	raw := t.topRow() + "\n" + t.ruleRow() + "\n" + body + "\n" + t.statusRow()
	t.fillZones()
	return frame(raw, t.width, t.height)
}

func (t Model) bodyView() string {
	switch t.active {
	case tabIdentity:
		t.identityVP.Width = t.width
		t.identityVP.Height = t.bodyHeight()
		if !t.zoom && !t.drill {
			t.identityVP.SetContent(t.identityBody())
		} else {
			t.identityVP.SetContent(t.identityBody())
		}
		return t.identityVP.View()
	case tabConfig:
		return t.configVP.View()
	case tabLayers:
		return t.layersBody()
	case tabScan:
		return t.scanBody(t.width, t.bodyHeight())
	default:
		return ""
	}
}

func (t Model) overlay(body string) string {
	w, h := t.width, t.bodyHeight()
	switch {
	case t.help:
		return ui.Modal(ui.ModalSpec{
			Title: "KEYS  " + t.modeName(),
			Body:  t.helpBody(),
			Width: min(w-4, 76),
		}, w, h)
	case t.palette.open:
		return ui.Modal(ui.ModalSpec{
			Title: "COMMANDS",
			Body:  t.paletteBody(),
			Width: min(w-8, 72),
		}, w, h)
	default:
		return ui.Modal(t.disclosureSpec(), w, h)
	}
}

func chip(s string) string {
	if ui.ColorOff() || ui.Profile() == "ansi16" {
		return s
	}
	return ui.Paint(" "+s+" ", ui.TextPrimary.Hex(), ui.SurfaceSeg.Hex(), false)
}

func (t Model) helpBody() string {
	side := "<->"
	if ui.UTF8() {
		side = "←→"
	}
	var b strings.Builder
	b.WriteString("GLOBAL\n")
	fmt.Fprintf(&b, "  %s  jump to tab\n", chip("1 2 3 4"))
	fmt.Fprintf(&b, "  %s  next / prev tab\n", chip("tab"))
	fmt.Fprintf(&b, "  %s  command palette\n", chip(": ctrl+k"))
	fmt.Fprintf(&b, "  %s  this help\n", chip("?"))
	fmt.Fprintf(&b, "  %s  quit\n\n", chip("q ctrl+c"))
	b.WriteString("NAVIGATION\n")
	fmt.Fprintf(&b, "  %s  move\n", chip("up down"))
	fmt.Fprintf(&b, "  %s  page\n", chip("pgup pgdn"))
	fmt.Fprintf(&b, "  %s  top / bottom\n", chip("g G"))
	fmt.Fprintf(&b, "  %s  open / drill in\n", chip("enter"))
	fmt.Fprintf(&b, "  %s  back / close\n\n", chip("esc"))
	b.WriteString("PANES\n")
	fmt.Fprintf(&b, "  %s  focus other pane\n", chip("w"))
	fmt.Fprintf(&b, "  %s  resize split\n", chip("< >"))
	fmt.Fprintf(&b, "  %s  zoom focused pane\n\n", chip("z"))
	b.WriteString("VIEW\n")
	fmt.Fprintf(&b, "  %s  mouse on / off\n", chip("m"))
	mouse := "Mouse is on. Hold Shift to select text."
	if !t.mouse {
		mouse = "Mouse is off. Hold Shift to select text."
	}
	b.WriteString("\n" + mouse + "\n")
	if t.treeFocused() {
		b.WriteString("\n" + side + " fold\n")
	} else {
		b.WriteString("\n" + side + " tabs\n")
	}
	b.WriteString("esc close" + ui.SepWord() + "? close\n")
	return b.String()
}

func (t Model) paletteBody() string {
	items := t.commands()
	ranked := ui.Rank(t.palette.query, items)
	var b strings.Builder
	fmt.Fprintf(&b, "%d of %d\n", len(ranked), len(items))
	query := ui.G().Arrow + " " + sanitize.From(t.palette.query).String()
	if !ui.ColorOff() && ui.Profile() != "ansi16" {
		query = ui.Paint(query, ui.TextPrimary.Hex(), ui.SurfaceSeg.Hex(), false)
	}
	b.WriteString(query + "\n\n")
	n := len(ranked)
	if n > 8 {
		n = 8
	}
	if t.palette.cursor >= n && n > 0 {
		// cursor is clamped in update
	}
	for i := 0; i < n; i++ {
		it := ranked[i]
		mark := "  "
		if i == t.palette.cursor {
			mark = ui.G().Edge
			if mark == "" {
				mark = ">"
			}
			mark += " "
		}
		where := it.Where
		line := mark + it.Title
		if where != "" {
			line += "  " + where
		}
		if it.Key != "" {
			line += "  " + it.Key
		}
		if !it.Enable {
			line += "  unavailable"
		}
		if i == t.palette.cursor && !ui.ColorOff() && ui.Profile() != "ansi16" {
			line = ui.Paint(line, ui.TextPrimary.Hex(), ui.SurfaceSel.Hex(), false)
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\nup/down select" + ui.SepWord() + "enter run" + ui.SepWord() + "esc close\n")
	return b.String()
}

func (t Model) commands() []ui.Match {
	type cmd struct {
		id, title, key, where string
		enable                bool
	}
	list := []cmd{
		{"id", "Go to Identity", "1", "", true},
		{"cfg", "Go to Config", "2", "", true},
		{"lay", "Go to Layers", "3", "", true},
		{"scan", "Go to Scan", "4", "", true},
		{"help", "Toggle help", "?", "", true},
		{"mouse", "Toggle mouse", "m", "", true},
		{"motion", "Toggle motion", "", "", true},
		{"focus", "Focus other pane", "w", "", true},
		{"zoom", "Zoom pane", "z", "", true},
		{"quit", "Quit", "q", "", true},
		{"lsort", "Layers: sort by size", "s", "in Layers", true},
		{"lfilt", "Layers: filter by type", "/", "in Layers", true},
		{"csort", "Config: filter keys", "/", "in Config", false},
	}
	out := make([]ui.Match, len(list))
	for i, c := range list {
		out[i] = ui.Match{ID: c.id, Title: c.title, Key: c.key, Where: c.where, Order: i, Enable: c.enable}
	}
	return out
}

func (t *Model) runCommand(id string) tea.Cmd {
	switch id {
	case "id":
		t.active = tabIdentity
	case "cfg":
		t.active = tabConfig
	case "lay":
		t.active = tabLayers
	case "scan":
		t.active = tabScan
	case "help":
		t.help = !t.help
	case "mouse":
		t.palette.open = false
		t.inputFocused = false
		next, cmd := t.toggleMouse()
		if m, ok := next.(Model); ok {
			*t = m
		}
		return cmd
	case "motion":
		t.motion = !t.motion
		if t.motion {
			t.motionFrame = 0
			return tick()
		}
		t.motionFrame = 6
	case "focus":
		t.focusSecondary = !t.focusSecondary
	case "zoom":
		t.zoom = !t.zoom
	case "quit":
		return tea.Quit
	case "lsort", "lfilt":
		t.active = tabLayers
		t.notice = "that command arrives with the Layers tree"
	case "csort":
		t.active = tabConfig
	}
	return nil
}

func (t Model) updatePalette(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+k":
		t.palette.open = false
		t.inputFocused = false
		return t, nil
	case "up", "k":
		if t.palette.cursor > 0 {
			t.palette.cursor--
		}
		return t, nil
	case "down", "j":
		t.palette.cursor++
		return t, nil
	case "enter":
		items := ui.Rank(t.palette.query, t.commands())
		if len(items) == 0 {
			return t, nil
		}
		if t.palette.cursor >= len(items) {
			t.palette.cursor = len(items) - 1
		}
		if t.palette.cursor < 0 {
			t.palette.cursor = 0
		}
		id := items[t.palette.cursor].ID
		if !items[t.palette.cursor].Enable {
			t.notice = "unavailable"
			return t, nil
		}
		cmd := t.runCommand(id)
		if id != "mouse" {
			t.palette.open = false
			t.inputFocused = false
		}
		t.resize()
		return t, cmd
	case "backspace":
		r := []rune(t.palette.query)
		if len(r) > 0 {
			t.palette.query = string(r[:len(r)-1])
		}
		t.palette.cursor = 0
		return t, nil
	default:
		if msg.Type == tea.KeyRunes {
			r := []rune(t.palette.query)
			if len(r) < 64 {
				t.palette.query += string(msg.Runes)
			}
			t.palette.cursor = 0
		}
		return t, nil
	}
}

func (t Model) disclosureSpec() ui.ModalSpec {
	primary := ui.Button("c / enter", "content reading ships in a later version", true, false)
	secondary := ui.Button("esc", "Cancel", false, true)
	return ui.ModalSpec{
		Title:   "Read file contents?",
		Body:    "So far only names and sizes were read. Nothing is executed, imported or unpickled.",
		Buttons: primary + "  " + secondary,
		Width:   min(t.width-4, 72),
	}
}

func (t Model) updateDisclosure(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		t.disclosure = false
	case "enter", "c":
		t.notice = "Code & pickles arrives in a later version"
	default:
		t.disclosure = false
	}
	return t, nil
}

func (t Model) toggleMouse() (tea.Model, tea.Cmd) {
	t.mouse = !t.mouse
	if t.mouse {
		return t, tea.EnableMouseCellMotion
	}
	return t, tea.DisableMouse
}

func (t Model) updateMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if !t.mouse {
		return t, nil
	}
	if msg.Action != tea.MouseActionPress {
		return t, nil
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		return t.scrollAt(msg.Y, -3), nil
	case tea.MouseButtonWheelDown:
		return t.scrollAt(msg.Y, 3), nil
	case tea.MouseButtonLeft:
	default:
		return t, nil
	}
	if t.zones == nil {
		return t, nil
	}
	z, ok := t.zones.Hit(msg.X, msg.Y)
	if !ok {
		if t.help || t.palette.open || t.disclosure {
			t.help = false
			t.palette.open = false
			t.inputFocused = false
			t.disclosure = false
		}
		return t, nil
	}
	now := t.clock()
	double := z.ID == t.lastClickID && now.Sub(t.lastClickAt) <= 400*time.Millisecond
	t.lastClickID = z.ID
	t.lastClickAt = now
	switch {
	case z.ID == "status:mouse":
		return t.toggleMouse()
	case strings.HasPrefix(z.ID, "tab:"):
		if len(z.ID) > 4 {
			t.active = tab(z.ID[4] - '0')
		}
	case z.ID == "pane:primary":
		t.focusSecondary = false
	case z.ID == "pane:secondary":
		t.focusSecondary = true
	case strings.HasPrefix(z.ID, "row:scan:"):
		var i int
		fmt.Sscanf(z.ID, "row:scan:%d", &i)
		t.scanCursor = i
		if double {
			return t.updateScan(tea.KeyMsg{Type: tea.KeyEnter})
		}
	case strings.HasPrefix(z.ID, "palette:row:"):
		var i int
		fmt.Sscanf(z.ID, "palette:row:%d", &i)
		t.palette.cursor = i
		if double {
			return t.updatePalette(tea.KeyMsg{Type: tea.KeyEnter})
		}
	}
	return t, nil
}

func (t Model) scrollAt(y, d int) tea.Model {
	if y >= t.height-1 {
		return t
	}
	switch t.active {
	case tabIdentity:
		t.identityVP.YOffset += d
		if t.identityVP.YOffset < 0 {
			t.identityVP.YOffset = 0
		}
	case tabConfig:
		t.configVP.YOffset += d
		if t.configVP.YOffset < 0 {
			t.configVP.YOffset = 0
		}
	case tabScan:
		t.scanCursor += d
		if t.scanCursor < 0 {
			t.scanCursor = 0
		}
		if n := len(t.scan.Checks); n > 0 && t.scanCursor >= n {
			t.scanCursor = n - 1
		}
		t.ensureScanVisible()
	}
	return t
}

func (t Model) fillZones() {
	if t.zones == nil {
		return
	}
	t.zones.Reset()
	// Tab pills sit on row 0. Four zones of roughly equal leftover width.
	x := 0
	if ui.Breakpoint(t.width, t.height) != "L" || t.active != tabIdentity {
		x = ui.Width("model-inspector") + 2
	}
	for i := 0; i < int(numTabs); i++ {
		label := tabNames[i]
		w := ui.Width(label) + 2
		t.zones.Add(fmt.Sprintf("tab:%d", i), ui.Rect{X: x, Y: 0, W: w, H: 1})
		x += w + 1
	}
	t.zones.Add("status:mouse", ui.Rect{X: max(0, t.width-14), Y: 0, W: 14, H: 1})
	br := ui.Breakpoint(t.width, t.height)
	rects := ui.Layout(t.width, t.height, t.ratio, t.zoom, t.focusSecondary)
	if br == "M" || br == "L" {
		t.zones.Add("pane:primary", rects.Primary)
		if rects.Split {
			t.zones.Add("pane:secondary", rects.Secondary)
		}
	} else {
		t.zones.Add("pane:primary", rects.Body)
	}
	if t.active == tabScan {
		for i := 0; i < t.bodyHeight() && i < len(t.scan.Checks); i++ {
			t.zones.Add(fmt.Sprintf("row:scan:%d", t.scanScroll+i), ui.Rect{X: 0, Y: 2 + i, W: t.width, H: 1})
		}
	}
}

func (t Model) topRow() string {
	gl := ui.G()
	var b strings.Builder
	showWord := !(ui.Breakpoint(t.width, t.height) == "L" && t.active == tabIdentity)
	if showWord {
		if ui.ColorOff() {
			b.WriteString("model-inspector  ")
		} else if ui.Profile() == "ansi16" {
			b.WriteString(ui.GradText("model-inspector", ui.GradBrand, true))
			b.WriteString("  ")
		} else {
			b.WriteString(ui.GradText("model-inspector", ui.GradBrand, true))
			b.WriteString("  ")
		}
	}
	for i, name := range tabNames {
		label := name
		if tab(i) == tabScan {
			label = strings.TrimPrefix(name, "4 ")
			label = "4 Scan " + scanBadge(t.scan)
		}
		if tab(i) == t.active {
			if ui.ColorOff() {
				b.WriteString("[" + label + "]")
			} else {
				b.WriteString(ui.Pill(ui.FillKindViolet, sanitize.From(label)))
			}
		} else if ui.ColorOff() {
			b.WriteString(label)
		} else {
			b.WriteString(ui.Paint(" "+label+" ", ui.TextSecondary.Hex(), ui.SurfaceSeg.Hex(), false))
		}
		b.WriteString(" ")
	}
	left := b.String()
	hint := "? keys   : commands   m mouse "
	if t.mouse {
		hint += "on"
	} else {
		hint += "off"
	}
	if !ui.UTF8() {
		hint = "? keys   : commands   m mouse " + onOff(t.mouse)
	}
	_ = gl
	if ui.Width(left)+ui.Width(hint)+2 > t.width {
		hint = "m mouse " + onOff(t.mouse)
	}
	if ui.Width(left)+ui.Width(hint)+2 > t.width {
		return fitLine(left, t.width)
	}
	gap := t.width - ui.Width(StripANSI(left)) - ui.Width(hint)
	if gap < 1 {
		return fitLine(left, t.width)
	}
	return fitLine(left+strings.Repeat(" ", gap)+hint, t.width)
}

func onOff(v bool) string {
	if v {
		return "on"
	}
	return "off"
}

func StripANSI(s string) string { return ui.Strip(s) }

func (t Model) ruleRow() string {
	gl := ui.G()
	ch := gl.Rule
	if ch == "" {
		ch = "="
	}
	line := strings.Repeat(ch, t.width)
	if ui.ColorOff() {
		// Mark the active tab span with the on-glyph when it differs.
		return fitLine(line, t.width)
	}
	col := ui.RuleTab.Hex()
	painted := ui.Paint(line, col, "", false)
	return fitLine(painted, t.width)
}

func (t Model) statusRow() string {
	mode := t.modeName()
	if t.zoom {
		mode = "ZOOM"
	}
	name := ""
	if t.m != nil {
		name = t.m.RepoID
		if name == "" {
			name = t.m.Dir
		}
		name = sanitize.From(name).String()
	}
	hint := ui.HintLine(t.treeFocused())
	hintKeep := false
	if t.notice != "" {
		hint = t.notice
		// A transient notice replaces the hint and has to stay visible.
		// The existing scan test reads it at 80 columns.
		hintKeep = true
	}
	format := ""
	size := ""
	quant := ""
	if t.m != nil && !strings.HasPrefix(t.m.Format, "gguf") {
		format = compactFormat(t.m.Format)
		size = model.HumanBytes(t.m.TotalSize)
		if incomplete(t.m) {
			size += " (incomplete)"
		}
		quant = quantHeader(t.m)
	}
	verdict, vKind := t.verdictSeg()
	content := ui.G().NR + " headers-only"
	cKind := ui.FillKindNR
	left := []ui.StatusSeg{
		{Text: mode, Pri: 1, FG: ui.TextInk, BG: ui.FillPink, Bold: true, Keep: true},
		{Text: name, Pri: 3, FG: ui.TextPrimary, BG: ui.SurfaceSeg2, Min: 16, Shrink: true},
		{Text: hint, Pri: 5, FG: ui.TextSecondary, BG: ui.SurfaceSeg, Keep: hintKeep},
	}
	right := []ui.StatusSeg{
		{Text: format, Pri: 4, FG: ui.TextSecondary, BG: ui.SurfaceSeg},
		{Text: size, Pri: 2, FG: ui.TextPrimary, BG: ui.SurfaceSeg2},
		{Text: quant, Pri: 2, FG: ui.TextOnFill, BG: ui.FillViolet, Bold: true},
		{Text: verdict, Pri: 1, FG: fgFor(vKind), BG: bgFor(vKind), Bold: true, Keep: true},
		{Text: content, Pri: 1, FG: fgFor(cKind), BG: bgFor(cKind), Keep: true},
	}
	return ui.Segments(left, right, t.width)
}

func fgFor(k ui.FillKind) ui.Token {
	_, fg := ui.FillColors(k)
	return fg
}

func bgFor(k ui.FillKind) ui.Token {
	bg, _ := ui.FillColors(k)
	return bg
}

func (t Model) modeName() string {
	if t.help {
		return "HELP"
	}
	if t.palette.open {
		return "COMMAND"
	}
	switch t.active {
	case tabConfig:
		return "CONFIG"
	case tabLayers:
		return "LAYERS"
	case tabScan:
		return "SCAN"
	default:
		return "IDENTITY"
	}
}

func (t Model) verdictSeg() (string, ui.FillKind) {
	if t.m != nil && incomplete(t.m) {
		return "! incomplete", ui.FillKindWarn
	}
	g := ui.G()
	switch t.scan.Overall {
	case scan.StatusFail:
		return g.Fail + " " + fmt.Sprint(effectiveCount(t.scan, scan.StatusFail)) + " fail", ui.FillKindFail
	case scan.StatusWarn:
		return g.Warn + " " + fmt.Sprint(effectiveCount(t.scan, scan.StatusWarn)) + " warn", ui.FillKindWarn
	case scan.StatusPass:
		n := 0
		for _, c := range t.scan.Checks {
			if c.EffectiveStatus == scan.StatusPass {
				n++
			}
		}
		return g.Pass + fmt.Sprintf(" structure %d/%d", n, len(t.scan.Checks)), ui.FillKindOK
	default:
		if len(t.scan.Checks) == 0 {
			return g.NR + " not run", ui.FillKindNR
		}
		return g.NR + " not run", ui.FillKindNR
	}
}

func (t Model) tooSmall() string {
	msg := fmt.Sprintf("model-inspector needs at least 60×16.\nThis terminal is %d×%d.\n\nEnlarge the window, or press q to quit.", t.width, t.height)
	return msg
}
