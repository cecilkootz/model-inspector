package tui

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/sanitize"
	"github.com/cecilkootz/model-inspector/internal/scan"
	"github.com/cecilkootz/model-inspector/internal/tui/ui"
)

type tickMsg time.Time

func tick() tea.Cmd {
	return tea.Tick(25*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func mouseDefault() bool {
	return os.Getenv("MODEL_INSPECTOR_MOUSE") != "0"
}

func osc52EnvOff() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MODEL_INSPECTOR_NO_OSC52"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
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
	if o.NoClipboard {
		t.noClipboard = true
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

func (t Model) treeFocused() bool {
	if t.active == tabConfig && t.treePane() {
		return true
	}
	return t.layersTreeOn()
}

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
	lay := ""
	if t.lay != nil {
		lay = t.lay.framePart()
	}
	scn := ""
	if t.scn != nil {
		scn = t.scn.framePart()
	}
	return fmt.Sprintf("%d %d %d %v %v %s %d %v %q %v %d %v %v %d %v %v %d %d %s %v %s %s %s",
		t.width, t.height, t.active, t.help, t.palette.open, t.palette.query, t.palette.cursor,
		t.disclosure, t.notice, t.mouse, t.motionFrame, t.focusSecondary, t.zoom, t.ratio, t.drill,
		t.blurred, t.scanCursor, t.scanScroll, strings.Join(open, ","),
		t.identityVP.YOffset+t.configVP.YOffset,
		t.cfg.framePart(), lay, scn)
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
	if t.m == nil && t.active != tabScan {
		return t.waitingLine()
	}
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
		return t.configBody()
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
			Title: ui.Chrome("KEYS  " + t.modeName()),
			Body:  ui.Chrome(t.helpBody()),
			Width: min(w-4, 76),
		}, w, h)
	case t.palette.open:
		return ui.Modal(ui.ModalSpec{
			Title: ui.S(ui.L("COMMANDS")),
			Body:  ui.Chrome(t.paletteBody()),
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
		where := it.Where.String()
		line := mark + it.Title.String()
		if where != "" {
			line += "  " + where
		}
		if !it.Key.Empty() {
			line += "  " + it.Key.String()
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
	var out []ui.Match
	for i, c := range list {
		if t.active == tabConfig && c.id == "csort" {
			continue
		}
		out = append(out, ui.Match{
			ID: c.id, Title: ui.S(sanitize.Program(c.title)), Key: ui.S(sanitize.Program(c.key)),
			Where: ui.S(sanitize.Program(c.where)), Order: i, Enable: c.enable,
		})
	}
	if t.active == tabConfig && t.cfg != nil && t.m != nil {
		t.prepareConfig()
		for _, c := range []struct{ id, title, key string }{
			{"cfg-expand", "Config: expand all", "e"},
			{"cfg-collapse", "Config: collapse all", "E"},
			{"cfg-search", "Config: search", "/"},
			{"cfg-path", "Config: copy path", "y"},
			{"cfg-value", "Config: copy value", "Y"},
			{"cfg-raw", "Config: toggle raw", "r"},
			{"cfg-jump", "Config: jump to field…", ""},
		} {
			out = append(out, ui.Match{
				ID: c.id, Title: ui.S(sanitize.Program(c.title)), Key: ui.S(sanitize.Program(c.key)),
				Where: ui.S(ui.L("in Config")), Order: len(out), Enable: true,
			})
		}
		for i, ln := range t.fieldLines() {
			if ln.kind != fieldRow {
				continue
			}
			// The "Config:" prefix is ours. The field label is file text.
			title := "Config: " + sanitize.From(ln.label).String()
			out = append(out, ui.Match{
				ID: fmt.Sprintf("cfg-field:%d", i), Title: ui.S(sanitize.Program(title)),
				Where: ui.S(ui.L("in Config")), Order: len(out), Enable: true,
			})
		}
	}
	if t.active == tabLayers && t.lay != nil {
		out = append(out,
			ui.Match{ID: "lay-expand", Title: ui.S(ui.L("Layers: expand all")), Key: ui.S(ui.L("e")), Where: ui.S(ui.L("in Layers")), Order: len(out), Enable: true},
			ui.Match{ID: "lay-collapse", Title: ui.S(ui.L("Layers: collapse all")), Key: ui.S(ui.L("E")), Where: ui.S(ui.L("in Layers")), Order: len(out) + 1, Enable: true},
			ui.Match{ID: "lay-search", Title: ui.S(ui.L("Layers: search")), Key: ui.S(ui.L("/")), Where: ui.S(ui.L("in Layers")), Order: len(out) + 2, Enable: true},
			ui.Match{ID: "lay-path", Title: ui.S(ui.L("Layers: copy name")), Key: ui.S(ui.L("y")), Where: ui.S(ui.L("in Layers")), Order: len(out) + 3, Enable: true},
			ui.Match{ID: "lay-value", Title: ui.S(ui.L("Layers: copy value")), Key: ui.S(ui.L("Y")), Where: ui.S(ui.L("in Layers")), Order: len(out) + 4, Enable: true},
			ui.Match{ID: "lay-sort-params", Title: ui.S(ui.L("Layers: sort by params")), Key: ui.S(ui.L("s")), Where: ui.S(ui.L("in Layers")), Order: len(out) + 5, Enable: true},
			ui.Match{ID: "lay-sort-name", Title: ui.S(ui.L("Layers: sort by name")), Key: ui.S(ui.L("s")), Where: ui.S(ui.L("in Layers")), Order: len(out) + 6, Enable: true},
			ui.Match{ID: "lay-sort-natural", Title: ui.S(ui.L("Layers: sort by natural")), Key: ui.S(ui.L("s")), Where: ui.S(ui.L("in Layers")), Order: len(out) + 7, Enable: true},
			ui.Match{ID: "lay-outliers", Title: ui.S(ui.L("Layers: outliers only")), Key: ui.S(ui.L("o")), Where: ui.S(ui.L("in Layers")), Order: len(out) + 8, Enable: true},
			ui.Match{ID: "lay-attn", Title: ui.S(ui.L("Layers: attention filter")), Key: ui.S(ui.L("t")), Where: ui.S(ui.L("in Layers")), Order: len(out) + 9, Enable: true},
			ui.Match{ID: "lay-parts", Title: ui.S(ui.L("Layers: parts mode")), Key: ui.S(ui.L("x")), Where: ui.S(ui.L("in Layers")), Order: len(out) + 10, Enable: true},
			ui.Match{ID: "lay-jump", Title: ui.S(ui.L("Layers: jump to layer…")), Key: ui.S(ui.L("#")), Where: ui.S(ui.L("in Layers")), Order: len(out) + 11, Enable: true},
		)
	}
	if t.active == tabScan {
		out = append(out,
			ui.Match{ID: "scan-next", Title: ui.S(sanitize.Program("Scan: next problem")), Key: ui.S(sanitize.Program("]")), Where: ui.S(sanitize.Program("in Scan")), Order: len(out), Enable: true},
			ui.Match{ID: "scan-expand", Title: ui.S(sanitize.Program("Scan: expand all")), Key: ui.S(sanitize.Program("e")), Where: ui.S(sanitize.Program("in Scan")), Order: len(out) + 1, Enable: true},
			ui.Match{ID: "scan-collapse", Title: ui.S(sanitize.Program("Scan: collapse all")), Key: ui.S(sanitize.Program("E")), Where: ui.S(sanitize.Program("in Scan")), Order: len(out) + 2, Enable: true},
			ui.Match{ID: "scan-nc", Title: ui.S(sanitize.Program("Scan: toggle not covered")), Key: ui.S(sanitize.Program("i")), Where: ui.S(sanitize.Program("in Scan")), Order: len(out) + 3, Enable: true},
			ui.Match{ID: "scan-code", Title: ui.S(sanitize.Program("Scan: code & pickles")), Key: ui.S(sanitize.Program("c")), Where: ui.S(sanitize.Program("in Scan")), Order: len(out) + 4, Enable: true},
			ui.Match{ID: "scan-copy", Title: ui.S(sanitize.Program("Scan: copy scan as text")), Key: ui.S(sanitize.Program("Y")), Where: ui.S(sanitize.Program("in Scan")), Order: len(out) + 5, Enable: true},
		)
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
	case "lsort":
		t.active = tabLayers
		t.layersCycleSort()
	case "lfilt":
		t.active = tabLayers
		t.openLayerSearch()
	case "lay-expand":
		t.active = tabLayers
		t.layersExpandAll()
	case "lay-collapse":
		t.active = tabLayers
		t.layersCollapseAll()
	case "lay-search":
		t.active = tabLayers
		t.openLayerSearch()
	case "lay-path":
		t.active = tabLayers
		t.copyLayers(false)
	case "lay-value":
		t.active = tabLayers
		t.copyLayers(true)
	case "lay-sort-params", "lay-sort-name", "lay-sort-natural":
		t.active = tabLayers
		t.layersCycleSort()
	case "lay-outliers":
		t.active = tabLayers
		t.layersToggleOutliers()
	case "lay-attn":
		t.active = tabLayers
		t.layersCycleAttn()
	case "lay-parts":
		t.active = tabLayers
		t.layersToggleParts()
	case "lay-jump":
		t.active = tabLayers
		t.openLayerJump()
	case "scan-next":
		t.active = tabScan
		t.jumpProblem(1)
	case "scan-expand":
		t.active = tabScan
		t.expandScan(true)
	case "scan-collapse":
		t.active = tabScan
		t.expandScan(false)
	case "scan-nc":
		t.active = tabScan
		t.toggleNotCovered()
	case "scan-code":
		t.active = tabScan
		t.openCodeList()
	case "scan-copy":
		t.active = tabScan
		t.copyScan(true)
	case "cfg-expand":
		t.active = tabConfig
		t.focusSecondary = true
		if t.cfg != nil {
			t.cfg.drill = true
			t.configExpandAll()
		}
	case "cfg-collapse":
		t.active = tabConfig
		if t.cfg != nil {
			t.configCollapseAll()
		}
	case "cfg-search":
		t.active = tabConfig
		t.openSearch()
	case "cfg-path":
		t.active = tabConfig
		t.copyConfig(false)
	case "cfg-value":
		t.active = tabConfig
		t.copyConfig(true)
	case "cfg-raw":
		t.active = tabConfig
		if t.cfg != nil {
			t.cfg.raw = !t.cfg.raw
			t.cfg.rawReady = false
			t.touchConfig()
		}
	case "cfg-jump":
		t.active = tabConfig
		t.focusSecondary = false
	default:
		if strings.HasPrefix(id, "cfg-field:") {
			var i int
			fmt.Sscanf(id, "cfg-field:%d", &i)
			t.active = tabConfig
			t.focusSecondary = false
			if t.cfg != nil {
				t.cfg.fieldCur = i
				t.jumpField(i)
			}
		}
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
			r := append([]rune(t.palette.query), msg.Runes...)
			if len(r) > 64 {
				r = r[:64]
			}
			t.palette.query = string(r)
			t.palette.cursor = 0
		}
		return t, nil
	}
}

func (t Model) disclosureSpec() ui.ModalSpec {
	label, enabled := t.disclosureConfirm()
	primary := ui.Button(ui.S(sanitize.Program("c / ⏎")), ui.S(sanitize.Program(label)), true, enabled)
	if !ui.UTF8() {
		primary = ui.Button(ui.S(sanitize.Program("c / enter")), ui.S(sanitize.Program(label)), true, enabled)
	}
	secondary := ui.Button(ui.S(sanitize.Program("esc")), ui.S(sanitize.Program("Cancel")), false, true)
	return ui.ModalSpec{
		Title:   ui.S(sanitize.Program("How contents are read")),
		Body:    ui.S(sanitize.Program(t.disclosureBody())),
		Buttons: ui.Chrome(primary + "  " + secondary),
		Width:   min(t.width-4, 72),
	}
}

func (t Model) updateDisclosure(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		t.disclosure = false
	case "enter", "c":
		if _, enabled := t.disclosureConfirm(); !enabled {
			t.notice = "contents are read by the scan; see the Code & pickle contents check"
			return t, nil
		}
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
		return t.scrollAt(msg.X, msg.Y, -3), nil
	case tea.MouseButtonWheelDown:
		return t.scrollAt(msg.X, msg.Y, 3), nil
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
	case strings.HasPrefix(z.ID, "scan:"):
		return t.scanClick(z.ID, double), nil
	case strings.HasPrefix(z.ID, "row:scan:"):
		var i int
		fmt.Sscanf(z.ID, "row:scan:%d", &i)
		t.scanCursor = i
		if double {
			return t.updateScan(tea.KeyMsg{Type: tea.KeyEnter})
		}
	case strings.HasPrefix(z.ID, "cfg:"):
		return t.configClick(z.ID, double), nil
	case strings.HasPrefix(z.ID, "lay:"):
		return t.layersClick(z.ID, double), nil
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

func (t Model) scrollAt(x, y, d int) tea.Model {
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
		tree := t.treePane()
		if br := ui.Breakpoint(t.width, t.height); br == "M" || br == "L" {
			rects := ui.Layout(t.width, t.height, t.configRatio(), t.zoom, t.focusSecondary)
			tree = rects.Split && x >= rects.Secondary.X
		}
		t.configMove(d, tree)
	case tabLayers:
		t.layersMove(d)
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
	ratio := t.ratio
	if t.active == tabConfig {
		ratio = t.configRatio()
	}
	if t.active == tabLayers {
		ratio = t.layersRatio()
	}
	rects := ui.Layout(t.width, t.height, ratio, t.zoom, t.focusSecondary)
	if br == "M" || br == "L" {
		t.zones.Add("pane:primary", rects.Primary)
		if rects.Split {
			t.zones.Add("pane:secondary", rects.Secondary)
		}
	} else {
		t.zones.Add("pane:primary", rects.Body)
	}
	if t.active == tabScan && t.scn != nil {
		for _, h := range t.scn.hits {
			t.zones.Add(h.id, ui.Rect{X: h.x, Y: h.y, W: h.w, H: h.h})
		}
	}
	if t.palette.open {
		t.addPaletteZones()
	}
	if t.active == tabConfig && t.cfg != nil {
		for _, h := range t.cfg.hits {
			t.zones.Add(h.id, ui.Rect{X: h.x, Y: h.y, W: h.w, H: h.h})
		}
	}
	if t.active == tabLayers && t.lay != nil {
		for _, h := range t.lay.hits {
			t.zones.Add(h.id, ui.Rect{X: h.x, Y: h.y, W: h.w, H: h.h})
		}
	}
}

// addPaletteZones registers one zone per visible command row. The body
// starts on screen row 2 (wordmark, rule). Inside the card the rows follow
// the title, a blank line, and the three palette header lines.
func (t Model) addPaletteZones() {
	body := t.paletteBody()
	items := ui.Rank(t.palette.query, t.commands())
	n := len(items)
	if n > 8 {
		n = 8
	}
	spec := ui.ModalSpec{
		Title: ui.S(ui.L("COMMANDS")),
		Body:  ui.Chrome(body),
		Width: min(t.width-8, 72),
	}
	lay := ui.ModalLayoutOf(spec, t.width, t.bodyHeight())
	// title, blank, "N of M", query, blank, then the rows.
	const rowsAfterContent = 5
	for i := 0; i < n; i++ {
		y := 2 + lay.Y + lay.ContentTop + rowsAfterContent + i
		t.zones.Add(fmt.Sprintf("palette:row:%d", i), ui.Rect{X: lay.X, Y: y, W: lay.W, H: 1})
	}
}

func (t Model) configClick(id string, double bool) tea.Model {
	if t.cfg == nil {
		return t
	}
	switch {
	case strings.HasPrefix(id, "cfg:field:"):
		var i int
		fmt.Sscanf(id, "cfg:field:%d", &i)
		t.cfg.fieldCur = i
		t.focusSecondary = false
		if double {
			t.jumpField(i)
		}
		t.touchConfig()
	case strings.HasPrefix(id, "cfg:exp:"):
		var n int
		fmt.Sscanf(id, "cfg:exp:%d", &n)
		t.cfg.cursor = int32(n)
		t.focusSecondary = true
		t.cfg.ensureRows()
		t.cfg.row = t.cfg.indexOfCursor()
		t.configToggle()
	case strings.HasPrefix(id, "cfg:node:"):
		var n int
		fmt.Sscanf(id, "cfg:node:%d", &n)
		t.cfg.cursor = int32(n)
		t.focusSecondary = true
		if t.configBreak() == "S" || t.configBreak() == "XS" {
			t.cfg.drill = true
		}
		t.cfg.ensureRows()
		t.cfg.row = t.cfg.indexOfCursor()
		if double {
			t.configEnter(false)
		}
		t.touchConfig()
	case strings.HasPrefix(id, "cfg:more:"):
		var n int
		fmt.Sscanf(id, "cfg:more:%d", &n)
		t.cfg.ensureRows()
		for _, row := range t.cfg.rows {
			if row.more && int(row.id) == n {
				t.showMore(row, false)
				break
			}
		}
	case strings.HasPrefix(id, "cfg:crumb:"):
		var depth int
		fmt.Sscanf(id, "cfg:crumb:%d", &depth)
		t.crumbClick(depth)
	}
	return t
}

func (t Model) topRow() string {
	gl := ui.G()
	var b strings.Builder
	showWord := !(ui.Breakpoint(t.width, t.height) == "L" && t.active == tabIdentity)
	if showWord {
		if ui.ColorOff() {
			b.WriteString("model-inspector  ")
		} else {
			b.WriteString(ui.PaintToken("model-inspector", ui.Accent, nil, true))
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
				b.WriteString(ui.Pill(ui.FillKindViolet, sanitize.Program(label)))
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
	}
	hint := ui.HintLine(t.treeFocused())
	hintKeep := false
	var hintSegs []ui.StatusSeg
	// name is the repo id or directory. A notice can embed a copied path or
	// value. Mode, static hints, and the rest of this row are ours, so a
	// placeholder such as "<->" stays a less-than.
	file := func(s string) ui.Safe { return ui.S(sanitize.From(s)) }
	own := func(s string) ui.Safe { return ui.Chrome(s) }
	if t.notice != "" {
		hint = t.notice
		// A transient notice replaces the hint and has to stay visible.
		// The existing scan test reads it at 80 columns.
		hintKeep = true
		hintSegs = []ui.StatusSeg{{Text: file(hint), Pri: 5, FG: ui.TextSecondary, BG: ui.SurfaceSeg, Keep: true}}
	} else if t.active == tabConfig {
		for i, h := range t.configHintSegs() {
			hintSegs = append(hintSegs, ui.StatusSeg{Text: own(h), Pri: 5 + i, FG: ui.TextSecondary, BG: ui.SurfaceSeg})
		}
	} else if t.active == tabLayers {
		for i, h := range t.layersHintSegs() {
			hintSegs = append(hintSegs, ui.StatusSeg{Text: ui.S(sanitize.Program(h)), Pri: 5 + i, FG: ui.TextSecondary, BG: ui.SurfaceSeg})
		}
	} else if t.active == tabScan {
		for i, h := range t.scanHintSegs() {
			hintSegs = append(hintSegs, ui.StatusSeg{Text: ui.S(sanitize.Program(h)), Pri: 3, FG: ui.TextSecondary, BG: ui.SurfaceSeg, Keep: i == 0})
		}
	} else {
		hintSegs = []ui.StatusSeg{{Text: own(hint), Pri: 5, FG: ui.TextSecondary, BG: ui.SurfaceSeg, Keep: hintKeep}}
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
	if t.active == tabScan {
		verdict, vKind = t.scanVerdictText()
		content, cKind = t.scanContentText()
	}
	left := []ui.StatusSeg{
		{Text: own(mode), Pri: 1, FG: ui.TextInk, BG: ui.FillPink, Bold: true, Keep: true},
		{Text: file(name), Pri: 3, FG: ui.TextPrimary, BG: ui.SurfaceSeg2, Min: 16, Shrink: true},
	}
	left = append(left, hintSegs...)
	right := []ui.StatusSeg{
		{Text: own(format), Pri: 4, FG: ui.TextSecondary, BG: ui.SurfaceSeg},
		{Text: own(size), Pri: 2, FG: ui.TextPrimary, BG: ui.SurfaceSeg2},
		{Text: own(quant), Pri: 2, FG: ui.TextOnFill, BG: ui.FillViolet, Bold: true},
		{Text: own(verdict), Pri: 1, FG: fgFor(vKind), BG: bgFor(vKind), Bold: true, Keep: true},
		{Text: own(content), Pri: 1, FG: fgFor(cKind), BG: bgFor(cKind), Keep: true},
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
		return t.configMode()
	case tabLayers:
		return t.layersMode()
	case tabScan:
		return t.scanModeName()
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
