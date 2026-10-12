package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cecilkootz/model-inspector/internal/sanitize"
	"github.com/cecilkootz/model-inspector/internal/tui/ui"
)

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

func (t Model) frameKey() string {
	scn := ""
	if t.scn != nil {
		scn = t.scn.framePart()
	}
	return fmt.Sprintf("%d %d %v %v %s %d %q %v %d %s %s %s",
		t.width, t.height, t.help, t.palette.open, t.palette.query, t.palette.cursor,
		t.notice, t.mouse, t.scanCursor, t.cfg.framePart(), t.lay.framePart(), scn)
}

func (t Model) renderFrame() string {
	switch {
	case t.shellTooSmall():
		return t.renderTooSmall()
	case t.shellOn():
		return t.renderShell()
	case t.shellLoad():
		return t.renderShellLoading()
	}
	return frame("", t.width, t.height)
}

func (t Model) overlay(body string) string {
	w, h := t.width, t.bodyHeight()
	switch {
	case t.help:
		return ui.Modal(ui.ModalSpec{
			Title: ui.Chrome("KEYS  " + t.helpMode()),
			Body:  ui.Chrome(t.helpBody()),
			Width: min(w-4, 76),
		}, w, h)
	default:
		return ui.Modal(ui.ModalSpec{
			Title: ui.S(ui.L("COMMANDS")),
			Body:  ui.Chrome(t.paletteBody()),
			Width: min(w-8, 72),
		}, w, h)
	}
}

func chip(s string) string {
	if ui.ColorOff() || ui.Profile() == "ansi16" {
		return s
	}
	return ui.Paint(" "+s+" ", ui.TextPrimary.Hex(), ui.SurfaceSeg.Hex(), false)
}

func (t Model) helpMode() string {
	if t.shellLoad() {
		return "LOADING"
	}
	return t.shellModeName()
}

func (t Model) helpBody() string {
	if t.shellLoad() {
		return t.loadHelpBody()
	}
	return t.shellHelpBody()
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

func (t Model) commands() []ui.Match { return t.shellCommands() }

func (t *Model) runCommand(id string) tea.Cmd {
	if t.runShellCommand(id) {
		return nil
	}
	switch id {
	case "help":
		t.help = !t.help
	case "mouse":
		t.palette.open = false
		next, cmd := t.toggleMouse()
		if m, ok := next.(Model); ok {
			*t = m
		}
		return cmd
	case "quit":
		return tea.Quit
	}
	return nil
}

func (t Model) updatePalette(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+k":
		t.palette.open = false
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
		t.help = false
		t.palette.open = false
		return t, nil
	}
	now := t.clock()
	double := z.ID == t.lastClickID && now.Sub(t.lastClickAt) <= 400*time.Millisecond
	t.lastClickID = z.ID
	t.lastClickAt = now
	switch {
	case strings.HasPrefix(z.ID, "sh:"):
		return t.shellClick(z.ID, double), nil
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
	if t.shellOn() {
		return t.shellScroll(x, y, d)
	}
	return t
}

// addPaletteZones registers one zone per visible command row. The body
// starts on screen row 1, under the header. Inside the card the rows follow the title, a blank line, and the three
// palette header lines.
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
		y := bodyTop + lay.Y + lay.ContentTop + rowsAfterContent + i
		t.zones.Add(fmt.Sprintf("palette:row:%d", i), ui.Rect{X: lay.X, Y: y, W: lay.W, H: 1})
	}
}

// bodyTop is the screen row of the body, under the header.
const bodyTop = 1
