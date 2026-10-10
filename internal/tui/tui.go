// Package tui implements the interactive Bubble Tea interface.
package tui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"origin.cursor.com/cecilkootz/model-inspector/internal/model"
	"origin.cursor.com/cecilkootz/model-inspector/internal/sanitize"
	"origin.cursor.com/cecilkootz/model-inspector/internal/scan"
	"origin.cursor.com/cecilkootz/model-inspector/internal/tui/ui"
)

// termRestore leaves the alternate screen, turns mouse tracking off, and
// shows the cursor. A panic inside New or View must write this before exit:
// the runtime will not, and a stack overflow skips Bubble Tea's recover.
const termRestore = "\x1b[?1049l\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l\x1b[?25h"

// termOut is the stream that receives termRestore. Tests replace it.
var termOut io.Writer = os.Stdout

func restoreTerminal() {
	if termOut == nil {
		return
	}
	_, _ = io.WriteString(termOut, termRestore)
}

type tab int

const (
	tabIdentity tab = iota
	tabConfig
	tabLayers
	tabScan
	numTabs
)

var tabNames = [numTabs]string{"1 Identity", "2 Config", "3 Layers", "4 Scan"}

// Model is the root Bubble Tea model.
type Model struct {
	m      *model.Model
	scan   scan.Result
	active tab
	width  int
	height int
	ready  bool

	help         bool
	notice       string
	inputFocused bool
	scanCursor   int
	scanScroll   int
	scanOpen     map[string]bool

	identityVP viewport.Model
	configVP   viewport.Model

	groupTable  table.Model
	tensorTable table.Model
	detail      *model.Group

	mouse          bool
	motion         bool
	motionFrame    int
	blurred        bool
	focusSecondary bool
	ratio          int
	zoom           bool
	drill          bool
	disclosure     bool
	palette        palState
	derived        *derived
	cache          *frameCache
	zones          *ui.Zones
	now            func() time.Time
	lastClickID    string
	lastClickAt    time.Time
	cfg            *configState
}

type frameCache struct {
	key   string
	frame string
}

type palState struct {
	open   bool
	query  string
	cursor int
}

// Options configures a TUI session. A nil Mouse keeps the default
// (on, unless MODEL_INSPECTOR_MOUSE=0).
type Options struct {
	Mouse  *bool
	Motion *bool
	Now    func() time.Time
}

// Run starts the TUI in the alternate screen.
func Run(m *model.Model, res scan.Result, opt ...Options) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			restoreTerminal()
			err = fmt.Errorf("tui panic: %v", rec)
		}
	}()
	t := New(m, res)
	if len(opt) > 0 {
		t.apply(opt[0])
	}
	opts := []tea.ProgramOption{tea.WithAltScreen(), tea.WithReportFocus()}
	if t.mouse {
		opts = append(opts, tea.WithMouseCellMotion())
	}
	_, err = tea.NewProgram(t, opts...).Run()
	if err != nil && strings.Contains(err.Error(), "panic") {
		restoreTerminal()
	}
	return err
}

// New builds the UI for a loaded model.
func New(m *model.Model, res scan.Result) Model {
	configure()
	d := derive(m)
	motion := motionDefault()
	frame := 6
	if motion {
		frame = 0
	}
	t := Model{
		m: m, scan: res, width: 100, height: 30, scanOpen: map[string]bool{},
		mouse: mouseDefault(), motion: motion, motionFrame: frame,
		ratio: 55, derived: &d, cache: &frameCache{}, zones: &ui.Zones{},
		now: time.Now, cfg: newConfigState(),
	}
	t.identityVP = viewport.New(t.width, t.bodyHeight())
	t.configVP = viewport.New(t.width, t.bodyHeight())
	t.identityVP.SetContent(renderIdentity(m, t.width))

	styles := table.DefaultStyles()
	styles.Header = styles.Header.Bold(true).Foreground(lipgloss.AdaptiveColor{Dark: "99", Light: "55"})
	styles.Selected = styles.Selected.Foreground(lipgloss.AdaptiveColor{Dark: "230", Light: "255"}).Background(lipgloss.AdaptiveColor{Dark: "62", Light: "61"})
	t.groupTable = table.New(table.WithColumns(groupColumns(t.width)), table.WithRows(groupRows(m)),
		table.WithFocused(true), table.WithHeight(t.bodyHeight()), table.WithStyles(styles))
	t.tensorTable = table.New(table.WithColumns(tensorColumns(t.width)),
		table.WithFocused(true), table.WithHeight(t.bodyHeight()-1), table.WithStyles(styles))
	return t
}

func (t Model) Init() tea.Cmd {
	if t.motion && t.motionFrame < 6 {
		return tick()
	}
	return nil
}

func (t Model) bodyHeight() int {
	if h := t.height - 3; h > 1 {
		return h
	}
	return 1
}

func (t *Model) resize() {
	h := t.bodyHeight()
	for _, vp := range []*viewport.Model{&t.identityVP, &t.configVP} {
		vp.Width, vp.Height = t.width, h
	}
	t.identityVP.SetContent(t.identityBody())
	if t.cache != nil {
		t.cache.key = ""
	}
	t.groupTable.SetColumns(groupColumns(t.width))
	t.groupTable.SetWidth(t.width)
	t.groupTable.SetHeight(h)
	t.tensorTable.SetColumns(tensorColumns(t.width))
	t.tensorTable.SetWidth(t.width)
	th := h - 1
	if th < 1 {
		th = 1
	}
	t.tensorTable.SetHeight(th)
}

func (t Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		t.width, t.height, t.ready = msg.Width, msg.Height, true
		t.resize()
		t.ensureScanVisible()
		return t, t.kickConfig()
	case configParsedMsg:
		return t.onConfigParsed(msg)
	case configSpinMsg:
		if t.cfg != nil && t.cfg.parsing {
			t.cfg.spin = true
			t.touchConfig()
		}
		return t, nil
	case tea.MouseMsg:
		return t.updateMouse(msg)
	case tea.FocusMsg:
		t.blurred = false
		return t, nil
	case tea.BlurMsg:
		t.blurred = true
		return t, nil
	case tickMsg:
		if !t.motion || t.blurred || t.motionFrame >= 6 {
			return t, nil
		}
		t.motionFrame++
		t.resize()
		if t.motionFrame < 6 {
			return t, tick()
		}
		return t, nil
	case tea.KeyMsg:
		if t.notice != "" && msg.String() != "ctrl+c" {
			t.notice = ""
		}
		if msg.String() == "ctrl+c" {
			return t, tea.Quit
		}
		if t.configSearching() {
			return t.updateConfigSearch(msg)
		}
		if t.palette.open {
			return t.updatePalette(msg)
		}
		if t.inputFocused {
			if msg.String() == "esc" {
				t.inputFocused = false
			}
			// q and other keys do not quit or change tabs while a text input is focused.
			return t, nil
		}
		if t.disclosure {
			return t.updateDisclosure(msg)
		}
		if t.ready && (t.width < 60 || t.height < 16) {
			if msg.String() == "q" {
				return t, tea.Quit
			}
			return t, nil
		}
		switch msg.String() {
		case "q":
			if t.help {
				t.help = false
				return t, nil
			}
			return t, tea.Quit
		case "?":
			t.help = !t.help
			return t, nil
		case ":", "ctrl+k":
			t.palette.open = true
			t.palette.query = ""
			t.palette.cursor = 0
			t.inputFocused = true
			return t, nil
		case "m":
			return t.toggleMouse()
		case "esc":
			if t.help {
				t.help = false
				return t, nil
			}
			if t.active == tabConfig && t.configCloseTop() {
				t.touchConfig()
				return t, nil
			}
			if t.zoom {
				t.zoom = false
				return t, nil
			}
			if t.drill {
				t.drill = false
				return t, nil
			}
			if t.detail != nil {
				t.detail = nil
				return t, nil
			}
			return t, nil
		case "backspace":
			if t.drill {
				t.drill = false
				return t, nil
			}
			if t.detail != nil {
				t.detail = nil
			}
			return t, nil
		case "w":
			if ui.Breakpoint(t.width, t.height) == "M" || ui.Breakpoint(t.width, t.height) == "L" {
				t.focusSecondary = !t.focusSecondary
				t.resize()
			}
			return t, nil
		case "<", ">":
			if t.active == tabConfig {
				if t.cfg != nil && (ui.Breakpoint(t.width, t.height) == "M" || ui.Breakpoint(t.width, t.height) == "L") {
					d := -5
					if msg.String() == ">" {
						d = 5
					}
					if !t.cfg.ratioSet {
						t.cfg.ratio = t.configRatio()
						t.cfg.ratioSet = true
					}
					t.cfg.ratio += d
					if t.cfg.ratio < 30 {
						t.cfg.ratio = 30
					}
					if t.cfg.ratio > 70 {
						t.cfg.ratio = 70
					}
					t.touchConfig()
				}
				return t, nil
			}
			if br := ui.Breakpoint(t.width, t.height); br == "M" || br == "L" {
				d := -5
				if msg.String() == ">" {
					d = 5
				}
				t.ratio += d
				if t.ratio < 30 {
					t.ratio = 30
				}
				if t.ratio > 70 {
					t.ratio = 70
				}
				t.resize()
			}
			return t, nil
		case "z":
			if br := ui.Breakpoint(t.width, t.height); br == "M" || br == "L" {
				t.zoom = !t.zoom
				t.resize()
			}
			return t, nil
		case "tab":
			t.active = (t.active + 1) % numTabs
			return t, nil
		case "shift+tab":
			t.active = (t.active + numTabs - 1) % numTabs
			return t, nil
		case "right", "l":
			if t.treeFocused() {
				return t.updateConfigKey(msg)
			}
			t.active = (t.active + 1) % numTabs
			return t, nil
		case "left", "h":
			if t.treeFocused() {
				return t.updateConfigKey(msg)
			}
			t.active = (t.active + numTabs - 1) % numTabs
			return t, nil
		case "1", "2", "3", "4":
			t.active = tab(msg.String()[0] - '1')
			return t, nil
		case "enter":
			if t.active == tabConfig && !t.help {
				return t.updateConfigKey(msg)
			}
			if !t.help && t.active == tabIdentity && ui.Breakpoint(t.width, t.height) == "S" {
				t.drill = true
				t.resize()
				return t, nil
			}
		}
		if t.help {
			return t, nil
		}
		if t.active == tabConfig {
			return t.updateConfigKey(msg)
		}
		if t.active == tabScan {
			return t.updateScan(msg)
		}
		if t.active == tabLayers {
			switch msg.String() {
			case "enter":
				if t.detail == nil {
					if i := t.groupTable.Cursor(); i >= 0 && i < len(t.m.Groups) {
						t.detail = t.m.Groups[i]
						t.tensorTable.SetRows(tensorRows(t.detail))
						t.tensorTable.SetCursor(0)
					}
				}
				return t, nil
			}
		}
	}

	if t.help {
		return t, nil
	}
	var cmd tea.Cmd
	switch t.active {
	case tabIdentity:
		t.identityVP, cmd = t.identityVP.Update(msg)
	case tabConfig:
		return t, nil
	case tabLayers:
		if t.detail != nil {
			t.tensorTable, cmd = t.tensorTable.Update(msg)
		} else {
			t.groupTable, cmd = t.groupTable.Update(msg)
		}
	}
	return t, cmd
}

func (t Model) updateScan(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	n := len(t.scan.Checks)
	switch msg.String() {
	case "up", "k":
		if t.scanCursor > 0 {
			t.scanCursor--
		}
	case "down", "j":
		if n > 0 && t.scanCursor < n-1 {
			t.scanCursor++
		}
	case "pgup", "b", "u":
		t.scanCursor -= t.bodyHeight() / 2
		if t.scanCursor < 0 {
			t.scanCursor = 0
		}
	case "pgdown", "f", "d":
		t.scanCursor += t.bodyHeight() / 2
		if n > 0 && t.scanCursor >= n {
			t.scanCursor = n - 1
		}
	case "home", "g":
		t.scanCursor = 0
	case "end", "G":
		if n > 0 {
			t.scanCursor = n - 1
		}
	case "enter":
		if n > 0 && t.scanCursor >= 0 && t.scanCursor < n {
			id := t.scan.Checks[t.scanCursor].ID
			if t.scanOpen == nil {
				t.scanOpen = map[string]bool{}
			}
			t.scanOpen[id] = !t.scanOpen[id]
		}
	case "c":
		t.notice = "Code & pickles arrives in a later version"
		t.disclosure = true
	}
	t.ensureScanVisible()
	return t, nil
}

func (t Model) View() string {
	defer func() {
		if rec := recover(); rec != nil {
			restoreTerminal()
			panic(rec)
		}
	}()
	configure()
	if !t.ready {
		return fitLine("model-inspector   1 Identity   2 Config   3 Layers   4 Scan", 80)
	}
	key := t.frameKey()
	if t.cache != nil && t.cache.key == key && t.cache.frame != "" {
		return t.cache.frame
	}
	s := t.renderFrame()
	if t.cache != nil {
		t.cache.key = key
		t.cache.frame = s
	}
	return s
}

func (t Model) layersBody() string {
	if strings.HasPrefix(t.m.Format, "gguf") {
		return "GGUF parsing is coming. model-inspector read file sizes only."
	}
	if len(t.m.Tensors) == 0 {
		return "No tensors: this directory has no safetensors shards."
	}
	if t.detail != nil {
		head := sanitize.From(fmt.Sprintf("%s · %d tensors · %s", t.detail.Key, len(t.detail.Tensors), model.HumanBytes(t.detail.Bytes))).String()
		return head + "\n" + t.tensorTable.View()
	}
	return t.groupTable.View()
}

func configUnreadable(m *model.Model) bool {
	for _, w := range m.Warnings {
		if strings.Contains(w, "config.json") {
			return true
		}
	}
	return false
}

func cutBlock(s string, width int) string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		if strings.Contains(ln, "\x1b") && lipgloss.Width(ln) > width {
			ln = stripANSI(ln)
		}
		lines[i] = truncWidth(ln, width)
	}
	return strings.Join(lines, "\n")
}

func groupColumns(width int) []table.Column {
	cols := []table.Column{
		{Title: "Group", Width: 0}, {Title: "Kind", Width: 10}, {Title: "Tensors", Width: 8},
		{Title: "Params*", Width: 9}, {Title: "Bytes", Width: 11}, {Title: "Experts", Width: 7}, {Title: "QWeights", Width: 8},
	}
	return flex(cols, width, 12)
}

func tensorColumns(width int) []table.Column {
	cols := []table.Column{
		{Title: "Tensor", Width: 0}, {Title: "DType", Width: 6}, {Title: "Shape", Width: 20},
		{Title: "Bytes", Width: 11}, {Title: "Role", Width: 8},
	}
	return flex(cols, width, 20)
}

// flex gives column 0 whatever width remains (cells have 1 char padding each side).
// Every column width is clamped to at least 0.
func flex(cols []table.Column, width, minW int) []table.Column {
	if width < 0 {
		width = 0
	}
	if minW < 0 {
		minW = 0
	}
	used := 0
	for i := 1; i < len(cols); i++ {
		if cols[i].Width < 0 {
			cols[i].Width = 0
		}
		used += cols[i].Width
	}
	w := width - used - 2*len(cols)
	if w < minW {
		w = minW
	}
	if w < 0 {
		w = 0
	}
	if len(cols) > 0 {
		cols[0].Width = w
	}
	return cols
}

func groupRows(m *model.Model) []table.Row {
	rows := make([]table.Row, 0, len(m.Groups))
	for _, g := range m.Groups {
		exp := ""
		if g.HasExperts {
			exp = "yes"
		}
		rows = append(rows, table.Row{
			sanitize.From(g.Key).String(), sanitize.From(g.Kind).String(), fmt.Sprint(len(g.Tensors)), model.HumanCount(g.LogicalParams),
			model.HumanBytes(g.Bytes), exp, fmt.Sprint(g.QuantizedTensors)})
	}
	return rows
}

func tensorRows(g *model.Group) []table.Row {
	rows := make([]table.Row, 0, len(g.Tensors))
	for _, t := range g.Tensors {
		name := t.Name
		if g.Kind == model.KindLayer {
			name = model.ShortName(name)
		}
		role := t.Role
		if t.Role == model.RoleQuantWeight {
			role = fmt.Sprintf("q%d", t.Bits)
		}
		rows = append(rows, table.Row{
			sanitize.From(name).String(), sanitize.From(t.DType).String(), sanitize.From(fmt.Sprint(t.Shape)).String(),
			model.HumanBytes(t.ByteLen()), sanitize.From(role).String()})
	}
	return rows
}
