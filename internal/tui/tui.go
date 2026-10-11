// Package tui implements the interactive Bubble Tea interface.
package tui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/scan"
	"github.com/cecilkootz/model-inspector/internal/tui/ui"
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

	mouse          bool
	noClipboard    bool
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
	lay            *layersState
	scn            *scanView
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
// (on, unless MODEL_INSPECTOR_MOUSE=0). NoClipboard turns OSC 52 off
// for the session; MODEL_INSPECTOR_NO_OSC52=1 does the same.
type Options struct {
	Mouse       *bool
	Motion      *bool
	NoClipboard bool
	Now         func() time.Time
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
		mouse: mouseDefault(), noClipboard: osc52EnvOff(), motion: motion, motionFrame: frame,
		ratio: 55, derived: &d, cache: &frameCache{}, zones: &ui.Zones{},
		now: time.Now, cfg: newConfigState(), lay: newLayersState(),
		scn: &scanView{},
	}
	t.identityVP = viewport.New(t.width, t.bodyHeight())
	t.configVP = viewport.New(t.width, t.bodyHeight())
	t.identityVP.SetContent(renderIdentity(m, t.width))
	return t
}

func (t Model) Init() tea.Cmd {
	var cmds []tea.Cmd
	if t.motion && t.motionFrame < 6 {
		cmds = append(cmds, tick())
	}
	if c := t.loadCmds(); c != nil {
		cmds = append(cmds, c)
	}
	return tea.Batch(cmds...)
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
	if t.active == tabIdentity {
		t.identityVP.SetContent(t.identityBody())
	}
	if t.cache != nil {
		t.cache.key = ""
	}
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
	case scanArmMsg:
		if t.scn != nil && t.scn.loading && t.m == nil {
			t.scn.armed = true
			t.touchScan()
		}
		return t, nil
	case scanProgMsg:
		if t.scn != nil && t.scn.loading {
			t.scn.prog = msg.p
			t.scn.spin++
			t.touchScan()
			return t, waitScanProg(t.scn.progCh)
		}
		return t, nil
	case scanLoadedMsg:
		return t.onScanLoaded(msg)
	case nil:
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
		if t.layersSearching() {
			return t.updateLayersSearch(msg)
		}
		if t.layersJumping() {
			return t.updateLayersJump(msg)
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
			if t.active == tabLayers && t.layersCloseTop() {
				return t, nil
			}
			if t.active == tabLayers && t.layersTreeOn() {
				return t.updateLayersKey(msg)
			}
			if t.active == tabLayers && t.focusSecondary {
				t.focusSecondary = false
				t.touchLayers()
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
			if t.active == tabScan && t.scn != nil && (t.scn.drill || t.scn.code) {
				return t.updateScan(msg)
			}
			return t, nil
		case "backspace":
			if t.drill {
				t.drill = false
				return t, nil
			}
			if t.active == tabLayers && t.layersTreeOn() {
				return t.updateLayersKey(msg)
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
			if t.active == tabLayers {
				if t.lay != nil && (ui.Breakpoint(t.width, t.height) == "M" || ui.Breakpoint(t.width, t.height) == "L") {
					d := -5
					if msg.String() == ">" {
						d = 5
					}
					if !t.lay.ratioSet {
						t.lay.ratio = t.layersRatio()
						t.lay.ratioSet = true
					}
					t.lay.ratio += d
					if t.lay.ratio < 30 {
						t.lay.ratio = 30
					}
					if t.lay.ratio > 70 {
						t.lay.ratio = 70
					}
					t.touchLayers()
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
			if t.active == tabConfig && t.treePane() {
				return t.updateConfigKey(msg)
			}
			if t.layersTreeOn() {
				return t.updateLayersKey(msg)
			}
			t.active = (t.active + 1) % numTabs
			return t, nil
		case "left", "h":
			if t.active == tabConfig && t.treePane() {
				return t.updateConfigKey(msg)
			}
			if t.layersTreeOn() {
				return t.updateLayersKey(msg)
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
			if t.active == tabLayers && !t.help {
				return t.updateLayersKey(msg)
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
			return t.updateLayersKey(msg)
		}
	}

	if t.help {
		return t, nil
	}
	var cmd tea.Cmd
	switch t.active {
	case tabIdentity:
		t.identityVP, cmd = t.identityVP.Update(msg)
	case tabConfig, tabLayers:
		return t, nil
	}
	return t, cmd
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
