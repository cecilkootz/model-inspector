// Package tui implements the interactive Bubble Tea interface.
package tui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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

// Model is the root Bubble Tea model.
type Model struct {
	m      *model.Model
	scan   scan.Result
	width  int
	height int
	ready  bool

	help        bool
	notice      string
	scanCursor  int
	mouse       bool
	noClipboard bool
	motion      bool
	palette     palState
	derived     *derived
	cache       *frameCache
	zones       *ui.Zones
	now         func() time.Time
	lastClickID string
	lastClickAt time.Time
	cfg         *configState
	lay         *layersState
	scn         *scanView
	sh          *shellState
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
	opts := []tea.ProgramOption{tea.WithAltScreen()}
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
	return Model{
		m: m, scan: res, width: 100, height: 30,
		mouse: mouseDefault(), noClipboard: osc52EnvOff(), motion: motionDefault(),
		derived: &d, cache: &frameCache{}, zones: &ui.Zones{},
		now: time.Now, cfg: newConfigState(), lay: newLayersState(),
		scn: &scanView{}, sh: newShellState(),
	}
}

func (t Model) Init() tea.Cmd {
	return t.loadCmds()
}

func (t Model) bodyHeight() int { return t.shellBodyHeight() }

func (t *Model) resize() {
	if t.cache != nil {
		t.cache.key = ""
	}
}

func (t Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		t.width, t.height, t.ready = msg.Width, msg.Height, true
		t.resize()
		return t, t.kickConfig()
	case configParsedMsg:
		return t.onConfigParsed(msg)
	case configSpinMsg:
		if t.cfg != nil && t.cfg.parsing {
			t.cfg.spin = true
			t.touchConfig()
		}
		return t, nil
	case scanProgMsg:
		if t.scn != nil && t.scn.loading {
			t.scn.phases.note(msg.p)
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
	case tea.KeyMsg:
		if t.notice != "" && msg.String() != "ctrl+c" {
			t.notice = ""
		}
		if msg.String() == "ctrl+c" {
			return t, tea.Quit
		}
		if t.palette.open {
			return t.updatePalette(msg)
		}
		switch {
		case t.shellLoad():
			return t.updateLoadKey(msg)
		case t.shellOn():
			return t.updateShellKey(msg)
		}
		// Too small to draw, or not sized yet: only quitting works.
		if msg.String() == "q" {
			return t, tea.Quit
		}
	}
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
		return fitLine("model-inspector", 80)
	}
	key := t.frameKey() + t.sh.framePart()
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
