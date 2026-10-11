package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cecilkootz/model-inspector/internal/fixture"
	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/scan"
	"github.com/cecilkootz/model-inspector/internal/tui/ui"
)

var shellSizes = [][2]int{{140, 40}, {120, 30}, {100, 30}, {84, 24}}

func shellModel(tb testing.TB, m *model.Model, w, h int) tea.Model {
	tb.Helper()
	res := scan.Run(m)
	for i := range res.Checks {
		res.Checks[i].Duration = 0
	}
	n := New(m, res)
	n.apply(Options{Layout: LayoutPanels, Now: func() time.Time { return time.Unix(100, 0) }})
	x, _ := n.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return x
}

func press(x tea.Model, keys ...string) tea.Model {
	for _, k := range keys {
		x, _ = x.Update(keyMsg(k))
	}
	return x
}

func click(x tea.Model, cx, cy int) tea.Model {
	x, _ = x.Update(tea.MouseMsg{X: cx, Y: cy, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	return x
}

func screenOf(x tea.Model) []string { return strings.Split(stripView(x.View()), "\n") }

// shellMixed is a model with warnings, so Checks has problems to jump to.
func shellFixtures(t *testing.T) map[string]*model.Model {
	t.Helper()
	tiny := loadTiny(t)
	tiny.RepoID, tiny.Revision = "org/tiny-qwen", "0123456789abcdef0123456789abcdef01234567"
	code := loadCode(t)
	code.RepoID, code.Revision = "org/code-exec", "89abcdef0123456789abcdef0123456789abcdef"
	return map[string]*model.Model{
		"tiny": tiny, "code": code,
		"fail": loadPastEOF(t), "partial": loadDangling(t),
	}
}

// Every frame fills the terminal exactly, for every focus and size.
func TestShellFrameFits(t *testing.T) {
	sizes := append([][2]int{{200, 50}, {100, 20}, {84, 16}, {90, 23}}, shellSizes...)
	for name, m := range shellFixtures(t) {
		for _, sz := range sizes {
			for _, keys := range [][]string{nil, {"2"}, {"3"}, {"4"}, {"4", "0"}, {"2", "enter"}, {"2", "?"}, {":"}} {
				x := press(shellModel(t, m, sz[0], sz[1]), keys...)
				if !x.(Model).shellOn() {
					t.Fatalf("%s %v: shell is off", name, sz)
				}
				assertFrame(t, x.View(), sz[0], sz[1])
			}
		}
	}
}

// Each list row's zone sits on the screen row where that row is drawn, even
// when the list scrolls inside its panel.
func TestShellRowsHitWhereDrawn(t *testing.T) {
	for name, m := range shellFixtures(t) {
		for _, sz := range shellSizes {
			for n := 1; n <= 4; n++ {
				label := fmt.Sprintf("%s %dx%d panel %d", name, sz[0], sz[1], n)
				cur := press(shellModel(t, m, sz[0], sz[1]), fmt.Sprint(n)).(Model)
				iw := shellSideWidth(sz[0]) - 4
				p := cur.sidePanels()[n-1]
				count := len(p.rows(iw))
				if count == 0 {
					if name == "tiny" || name == "code" {
						t.Fatalf("%s: no rows", label)
					}
					continue
				}
				for i := 0; i < count; i++ {
					p.move(i - p.cursor())
					rows := p.rows(iw)
					screen := strings.Split(stripView(cur.View()), "\n")
					found := false
					for _, h := range cur.sh.hits {
						var hn, hr int
						if _, err := fmt.Sscanf(h.id, "sh:row:%d:%d", &hn, &hr); err != nil || hn != n {
							t.Errorf("%s: stray hit %q", label, h.id)
							continue
						}
						if h.y < 1 || h.y >= sz[1]-1 {
							t.Errorf("%s: row %d hit at %d leaves the body", label, hr, h.y)
							continue
						}
						if !strings.Contains(screen[h.y], strings.TrimRight(ui.Strip(rows[hr]), " ")) {
							t.Errorf("%s cursor %d: hit for row %d at screen row %d reads %q", label, i, hr, h.y, screen[h.y])
						}
						if z, ok := cur.zones.Hit(h.x+1, h.y); !ok || z.ID != h.id {
							t.Errorf("%s: zone at (%d,%d) is %v, want %s", label, h.x+1, h.y, z.ID, h.id)
						}
						if hr == i {
							found = true
						}
					}
					if !found {
						t.Errorf("%s: selected row %d is not on screen", label, i)
					}
				}
			}
		}
	}
}

// A missing, malformed or hostile config.json leaves Config without rows or
// without a tree; the shell still fills the frame and takes every key.
func TestShellConfigProblems(t *testing.T) {
	cfgs := map[string][]byte{
		"malformed": fixture.ConfigMalformed(), "dupkey": fixture.ConfigDupKey(),
		"deep": fixture.ConfigDeep(), "notobject": fixture.ConfigNotObject(),
		"empty": fixture.ConfigEmpty(), "huge": fixture.ConfigHuge(), "hostile": fixture.ConfigHostile(),
		"none": nil,
	}
	for name, cfg := range cfgs {
		files, _, err := fixture.TinyQwen()
		if err != nil {
			t.Fatal(err)
		}
		var kept []fixture.File
		for _, f := range files {
			if f.Name == "config.json" {
				if cfg == nil {
					continue
				}
				f.Data = cfg
			}
			kept = append(kept, f)
		}
		dir := t.TempDir()
		if err := fixture.Materialize(dir, kept); err != nil {
			t.Fatal(err)
		}
		m, err := model.Load(dir)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, sz := range [][2]int{{140, 40}, {84, 24}} {
			x := shellModel(t, m, sz[0], sz[1])
			for _, keys := range [][]string{{"4"}, {"4", "0"}, {"4", "0", "down", "down", "right", "left", "enter", "G", "g"}, {"4", "down", "y"}, {"3", "0", "down", "right", "left", "space"}} {
				v := press(x, keys...).View()
				assertFrame(t, v, sz[0], sz[1])
				if raw, bad := rawBad(v); bad {
					t.Fatalf("%s %v: raw %s", name, keys, raw)
				}
			}
		}
	}
}

func TestShellFocusModel(t *testing.T) {
	m := shellFixtures(t)["tiny"]
	x := shellModel(t, m, 120, 30)
	sh := func() *shellState { return x.(Model).sh }
	if sh().side != 1 || sh().inDetail {
		t.Fatalf("start focus %d detail=%v, want Model", sh().side, sh().inDetail)
	}
	x = press(x, "3")
	if sh().side != 3 {
		t.Fatalf("3 focused %d", sh().side)
	}
	x = press(x, "0")
	if !sh().inDetail || sh().side != 3 {
		t.Fatalf("0: side %d detail %v", sh().side, sh().inDetail)
	}
	x = press(x, "esc")
	if sh().inDetail || sh().side != 3 {
		t.Fatalf("esc: side %d detail %v", sh().side, sh().inDetail)
	}
	x = press(x, "enter")
	if !sh().inDetail {
		t.Fatal("enter did not focus the detail pane")
	}

	x = shellModel(t, m, 120, 30)
	var order []string
	for i := 0; i < 5; i++ {
		x = press(x, "tab")
		order = append(order, focusName(x.(Model)))
	}
	if got := strings.Join(order, " "); got != "2 3 4 0 1" {
		t.Fatalf("tab order %s", got)
	}
	order = nil
	for i := 0; i < 5; i++ {
		x = press(x, "shift+tab")
		order = append(order, focusName(x.(Model)))
	}
	if got := strings.Join(order, " "); got != "0 4 3 2 1" {
		t.Fatalf("shift+tab order %s", got)
	}
}

func focusName(t Model) string {
	if t.sh.inDetail {
		return "0"
	}
	return fmt.Sprint(t.sh.side)
}

func TestShellMoveKeys(t *testing.T) {
	m := shellFixtures(t)["code"]
	x := shellModel(t, m, 120, 30)
	x = press(x, "down", "j", "j", "j", "j", "j", "j", "j", "j")
	if got := x.(Model).sh.modelCur; got != 4 {
		t.Fatalf("Model cursor %d, want clamp at 4", got)
	}
	x = press(x, "k", "up")
	if got := x.(Model).sh.modelCur; got != 2 {
		t.Fatalf("Model cursor %d", got)
	}
	x = press(x, "2", "j", "j")
	if got := x.(Model).scanCursor; got != 2 {
		t.Fatalf("Checks cursor %d", got)
	}
	x = press(x, "G")
	if got, want := x.(Model).scanCursor, len(x.(Model).scan.Checks)-1; got != want {
		t.Fatalf("G: cursor %d, want %d", got, want)
	}
	x = press(x, "g", "up")
	if got := x.(Model).scanCursor; got != 0 {
		t.Fatalf("g: cursor %d", got)
	}
	// Moving the selection changes the detail title.
	if !strings.Contains(screenOf(x)[1], "Shard headers parse") {
		t.Fatalf("detail title:\n%s", screenOf(x)[1])
	}
	x = press(x, "down")
	if !strings.Contains(screenOf(x)[1], "Files present") {
		t.Fatalf("detail title after down:\n%s", screenOf(x)[1])
	}
}

func TestShellProblemJump(t *testing.T) {
	x := shellModel(t, shellFixtures(t)["code"], 120, 30)
	res := x.(Model).scan
	var probs []int
	for i, c := range res.Checks {
		if isProblem(c) {
			probs = append(probs, i)
		}
	}
	if len(probs) < 2 {
		t.Fatalf("fixture has %d problems", len(probs))
	}
	x = press(x, "]")
	cur := x.(Model)
	if cur.sh.side != 2 || cur.sh.inDetail || cur.scanCursor != probs[0] {
		t.Fatalf("] from Model: side %d detail %v cursor %d, want Checks on %d", cur.sh.side, cur.sh.inDetail, cur.scanCursor, probs[0])
	}
	x = press(x, "]")
	if got := x.(Model).scanCursor; got != probs[1] {
		t.Fatalf("second ]: %d", got)
	}
	x = press(x, "]")
	if got := x.(Model).scanCursor; got != probs[0] {
		t.Fatalf("] did not wrap: %d", got)
	}
	x = press(x, "[")
	if got := x.(Model).scanCursor; got != probs[len(probs)-1] {
		t.Fatalf("[ did not wrap: %d", got)
	}
	// From the detail pane of another panel it still lands on Checks.
	x = press(x, "4", "0", "[")
	cur = x.(Model)
	if cur.sh.side != 2 || cur.sh.inDetail {
		t.Fatalf("[ from Config detail: side %d detail %v", cur.sh.side, cur.sh.inDetail)
	}

	clean := press(shellModel(t, shellFixtures(t)["tiny"], 120, 30), "3", "]")
	if cur := clean.(Model); cur.sh.side != 3 || cur.notice == "" {
		t.Fatalf("] with no problems moved focus to %d (notice %q)", cur.sh.side, cur.notice)
	}
}

func TestShellDetailScroll(t *testing.T) {
	m := shellFixtures(t)["code"]
	x := press(shellModel(t, m, 84, 24), "2", "0")
	top := screenOf(x)
	x = press(x, "down", "down", "down")
	_ = x.View()
	if got := x.(Model).sh.top; got != 3 {
		t.Fatalf("detail top %d, want 3", got)
	}
	if strings.Join(top, "\n") == strings.Join(screenOf(x), "\n") {
		t.Fatal("scrolling did not change the detail pane")
	}
	x = press(x, "g")
	_ = x.View()
	if got := x.(Model).sh.top; got != 0 {
		t.Fatalf("g: top %d", got)
	}
	x = press(x, "G")
	_ = x.View()
	if x.(Model).sh.top == 0 {
		t.Fatal("G did not scroll to the end")
	}
	x = press(x, "esc", "down")
	if got := x.(Model).sh.top; got != 0 {
		t.Fatalf("moving the selection kept scroll %d", got)
	}

	// Tree panels keep the tree cursor in the detail pane.
	x = press(shellModel(t, m, 120, 30), "4", "0")
	_ = x.View()
	before := x.(Model).cfg.row
	x = press(x, "down", "down")
	_ = x.View()
	if got := x.(Model).cfg.row; got != before+2 {
		t.Fatalf("Config tree row %d -> %d", before, got)
	}
}

// Selecting a Config field points the tree at it, and Layers at its group.
func TestShellSelectionDrivesDetail(t *testing.T) {
	x := press(shellModel(t, shellFixtures(t)["tiny"], 120, 30), "4", "down", "down")
	if !strings.Contains(screenOf(x)[1], "[0] text_config.hidden_size") {
		t.Fatalf("Config detail title %q", screenOf(x)[1])
	}
	cur := x.(Model)
	cur.cfg.ensureRows()
	if id := cur.cfg.cursor; cur.cfg.tree == nil || id <= 0 {
		t.Fatalf("tree cursor %d did not follow the field", id)
	}
	x = press(shellModel(t, shellFixtures(t)["tiny"], 120, 30), "3", "down")
	if got := screenOf(x)[1]; !strings.Contains(got, "[0] n-gram table") {
		t.Fatalf("Layers detail title %q", got)
	}
}

func TestShellHelpAndPalette(t *testing.T) {
	x := press(shellModel(t, shellFixtures(t)["code"], 120, 30), "2", "?")
	if !x.(Model).help {
		t.Fatal("? did not open help")
	}
	v := strings.Join(screenOf(x), "\n")
	for _, want := range []string{"KEYS  CHECKS", "next problem", "DETAIL PANE"} {
		if !strings.Contains(v, want) {
			t.Errorf("help missing %q", want)
		}
	}
	x = press(x, "4", "esc")
	if x.(Model).help || x.(Model).sh.side != 2 {
		t.Fatal("help leaked a focus key or did not close")
	}
	x = press(x, "4", "?")
	if !strings.Contains(strings.Join(screenOf(x), "\n"), "KEYS  CONFIG") {
		t.Fatal("help is not scoped to the focused panel")
	}
	x = press(x, "?")

	x = press(x, ":", "l", "a", "y", "enter")
	cur := x.(Model)
	if cur.palette.open || cur.sh.side != 3 {
		t.Fatalf("palette: open=%v side=%d", cur.palette.open, cur.sh.side)
	}
	x = press(x, ":", "n", "e", "x", "enter")
	if got := x.(Model); got.sh.side != 2 {
		t.Fatalf("palette next problem: side %d", got.sh.side)
	}
}

// The palette's row zones line up with where the shell draws it.
func TestShellPaletteClick(t *testing.T) {
	x := press(shellModel(t, shellFixtures(t)["tiny"], 120, 30), ":")
	screen := screenOf(x)
	row := -1
	for y, ln := range screen {
		if strings.Contains(ln, "Focus Layers") {
			row = y
		}
	}
	if row < 0 {
		t.Fatalf("palette does not list Focus Layers:\n%s", strings.Join(screen, "\n"))
	}
	col := strings.Index(screen[row], "Focus Layers")
	x = click(x, col, row)
	_ = x.View()
	x = click(x, col, row)
	cur := x.(Model)
	if cur.palette.open || cur.sh.side != 3 {
		t.Fatalf("double click on a palette row: open=%v side=%d", cur.palette.open, cur.sh.side)
	}
}

func TestShellQuitAndMouseKeys(t *testing.T) {
	x := shellModel(t, shellFixtures(t)["tiny"], 120, 30)
	for _, k := range []string{"q", "ctrl+c"} {
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		if k == "ctrl+c" {
			msg = tea.KeyMsg{Type: tea.KeyCtrlC}
		}
		_, cmd := x.Update(msg)
		if cmd == nil {
			t.Fatalf("%s did not quit", k)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("%s: not a quit", k)
		}
	}
	on := x.(Model).mouse
	x = press(x, "m")
	if x.(Model).mouse == on {
		t.Fatal("m did not toggle the mouse")
	}
	x = press(x, "y")
	if x.(Model).notice == "" {
		t.Fatal("y on Model said nothing")
	}
}

func TestShellMouse(t *testing.T) {
	x := shellModel(t, shellFixtures(t)["code"], 140, 40)
	_ = x.View()
	sh := x.(Model).sh

	r3 := sh.panelRect[2]
	x = click(x, r3.X+3, r3.Y)
	if cur := x.(Model); cur.sh.side != 3 || cur.sh.inDetail {
		t.Fatalf("clicking the Layers title: side %d detail %v", cur.sh.side, cur.sh.inDetail)
	}
	_ = x.View()
	x = click(x, 100, 10)
	if !x.(Model).sh.inDetail {
		t.Fatal("clicking the detail pane did not focus it")
	}

	x = press(x, "2")
	_ = x.View()
	var row5 hit
	for _, h := range x.(Model).sh.hits {
		if h.id == "sh:row:2:5" {
			row5 = h
		}
	}
	if row5.id == "" {
		t.Fatal("no zone for Checks row 5")
	}
	x = click(x, row5.x+4, row5.y)
	cur := x.(Model)
	if cur.scanCursor != 5 || cur.sh.inDetail {
		t.Fatalf("row click: cursor %d detail %v", cur.scanCursor, cur.sh.inDetail)
	}
	_ = x.View()
	x = click(x, row5.x+4, row5.y)
	if !x.(Model).sh.inDetail {
		t.Fatal("double click did not open the detail pane")
	}

	// Wheel scrolls the pane under the pointer without moving focus.
	x = press(x, "esc")
	_ = x.View()
	x, _ = x.Update(tea.MouseMsg{X: row5.x + 4, Y: row5.y, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	if got := x.(Model).scanCursor; got != 2 {
		t.Fatalf("wheel up in Checks moved the cursor to %d", got)
	}
	x, _ = x.Update(tea.MouseMsg{X: 100, Y: 10, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	if got := x.(Model).sh.top; got != 3 {
		t.Fatalf("wheel in detail: top %d", got)
	}
	if x.(Model).sh.inDetail {
		t.Fatal("wheel moved focus")
	}

	// Overlays swallow clicks instead of passing them to the panels behind.
	x = press(x, "?")
	_ = x.View()
	x = click(x, r3.X+3, r3.Y)
	if cur := x.(Model); cur.sh.side == 3 {
		t.Fatal("a click under help reached a panel")
	}
}

func TestShellFallbacks(t *testing.T) {
	m := shellFixtures(t)["tiny"]
	x := shellModel(t, m, 83, 30)
	if x.(Model).shellOn() {
		t.Fatal("shell on at 83 columns")
	}
	if v := strings.Join(screenOf(x), "\n"); !strings.Contains(v, "1 Identity") || strings.Contains(v, "[1] Model") {
		t.Fatalf("83 columns did not use the tab layout:\n%s", v)
	}
	x, _ = x.Update(tea.WindowSizeMsg{Width: 84, Height: 30})
	if !x.(Model).shellOn() {
		t.Fatal("shell off at 84 columns")
	}
	if v := strings.Join(screenOf(x), "\n"); !strings.Contains(v, "[1] Model") {
		t.Fatalf("84 columns did not draw the shell:\n%s", v)
	}
	// The default layout is the tab shell.
	d, _ := New(m, scan.Run(m)).Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	if d.(Model).shellOn() || strings.Contains(stripView(d.View()), "[1] Model") {
		t.Fatal("default layout is not tabs")
	}
}

// While loading the tab layout shows today's loading view; the shell takes
// over when the model arrives, with Model focused.
func TestShellLoadingThenShell(t *testing.T) {
	files, _, err := fixture.TinyQwen()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := fixture.Materialize(dir, files); err != nil {
		t.Fatal(err)
	}
	n := NewLoading(dir, false, false, "org/tiny", "")
	n.apply(Options{Layout: LayoutPanels})
	x, _ := n.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	if x.(Model).shellOn() {
		t.Fatal("shell drew while loading")
	}
	if v := stripView(x.View()); strings.Contains(v, "[1] Model") {
		t.Fatalf("loading drew panels:\n%s", v)
	}
	x, _ = x.Update(runScanLoad(n)())
	cur := x.(Model)
	if !cur.shellOn() || cur.sh.side != 1 || cur.sh.inDetail {
		t.Fatalf("after load: shell %v side %d detail %v", cur.shellOn(), cur.sh.side, cur.sh.inDetail)
	}
	assertFrame(t, x.View(), 120, 30)
	if v := stripView(x.View()); !strings.Contains(v, "[1] Model") || !strings.Contains(v, "org/tiny") {
		t.Fatalf("shell after load:\n%s", v)
	}

	prev := loadModel
	t.Cleanup(func() { loadModel = prev })
	loadModel = func(string, chan<- model.Progress) (*model.Model, error) { return nil, errors.New("nope") }
	bad := NewLoading(dir, false, false, "", "")
	bad.apply(Options{Layout: LayoutPanels})
	y, _ := bad.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	y, _ = y.Update(runScanLoad(bad)())
	if y.(Model).shellOn() || !strings.Contains(stripView(y.View()), "could not inspect") {
		t.Fatal("a load error did not show the error card")
	}
}

// File-derived text stays sanitized in every panel and in the header.
func TestShellHostileText(t *testing.T) {
	files, _, err := fixture.TinyQwen()
	if err != nil {
		t.Fatal(err)
	}
	for i := range files {
		if files[i].Name == "config.json" {
			files[i].Data = fixture.ConfigHostile()
		}
	}
	dir := t.TempDir()
	if err := fixture.Materialize(dir, files); err != nil {
		t.Fatal(err)
	}
	m, err := model.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	m.RepoID = "org/ev\x1b]0;PWN\x07il‮"
	m.Revision = "rev\x1b[2J0123456789"
	x := shellModel(t, m, 140, 40)
	var views []string
	for _, keys := range [][]string{nil, {"2"}, {"3"}, {"4"}, {"4", "0"}, {"4", "down", "down", "down"}} {
		views = append(views, press(x, keys...).View())
	}
	for i, view := range views {
		for _, r := range view {
			if r < 0x20 && r != '\n' && r != 0x1b {
				t.Fatalf("view %d contains %#U", i, r)
			}
		}
		if raw, bad := rawBad(view); bad {
			t.Fatalf("view %d: raw %s", i, raw)
		}
		for _, raw := range []string{"\x1b]0;PWN", "\x1b[2J", "‮"} {
			if strings.Contains(view, raw) {
				t.Fatalf("view %d still contains %q", i, raw)
			}
		}
	}
	all := strings.Join(views, "\n")
	for _, tok := range []string{"<U+001B>", "<U+202E>"} {
		if !strings.Contains(all, tok) {
			t.Errorf("missing visible token %s", tok)
		}
	}
}

// Every colour profile fills the frame, and the ASCII profile stays ASCII.
func TestShellProfiles(t *testing.T) {
	m := loadFiles(t, mustCode(t))
	m.RepoID = "org/code-exec"
	for _, p := range []string{"truecolor", "ansi256", "ansi16", "colorless", "ascii"} {
		for _, keys := range [][]string{nil, {"2"}, {"3", "0"}, {"4"}, {"?"}} {
			view := renderPrepared(t, m, scan.Run(m), 100, 30, p, keys, Options{Layout: LayoutPanels})
			assertFrame(t, view, 100, 30)
			if p == "ascii" {
				for i := 0; i < len(view); i++ {
					if view[i] >= 0x80 {
						t.Fatalf("ascii frame has byte %#x at %d (keys %v)", view[i], i, keys)
					}
				}
			}
		}
	}
}

func TestShellGolden(t *testing.T) {
	tiny := loadFiles(t, mustFiles(t, fixture.TinyQwen))
	tiny.RepoID, tiny.Revision = "org/tiny-qwen", "0123456789abcdef0123456789abcdef01234567"
	code := loadFiles(t, mustCode(t))
	code.RepoID, code.Revision = "org/code-exec", "89abcdef0123456789abcdef0123456789abcdef"
	qwen := loadFiles(t, mustShape(t))
	qwen.RepoID, qwen.Revision = "org/qwen-shape", "fedcba9876543210fedcba9876543210fedcba98"
	focus := []struct {
		name string
		keys []string
	}{
		{"model", nil}, {"checks", []string{"2"}}, {"layers", []string{"3"}}, {"config", []string{"4"}},
	}
	type spec struct {
		name    string
		m       *model.Model
		w, h    int
		profile string
		keys    []string
	}
	var cases []spec
	for _, sz := range [][2]int{{140, 40}, {100, 30}} {
		for _, p := range []string{"colorless", "truecolor"} {
			for _, f := range focus {
				cases = append(cases, spec{"shell.tiny." + dim(sz) + "." + p + "." + f.name, tiny, sz[0], sz[1], p, f.keys})
			}
		}
	}
	for _, p := range []string{"colorless", "truecolor"} {
		cases = append(cases,
			spec{"shell.code.140x40." + p + ".problem", code, 140, 40, p, []string{"]"}},
			spec{"shell.tiny.140x40." + p + ".detail", tiny, 140, 40, p, []string{"3", "0"}},
			spec{"shell.code.84x24." + p + ".checks", code, 84, 24, p, []string{"2"}},
			spec{"shell.qwen.140x40." + p + ".layer", qwen, 140, 40, p, []string{"3", "right", "down", "down"}},
			spec{"shell.qwen.84x24." + p + ".layer", qwen, 84, 24, p, []string{"3", "right", "down", "down"}},
			spec{"shell.qwen.140x40." + p + ".decoder", qwen, 140, 40, p, []string{"3"}},
		)
	}
	for i, sec := range []string{"storage", "memory", "callouts", "files"} {
		keys := repeatKey("down", i+1)
		cases = append(cases,
			spec{"shell.tiny.140x40.colorless.model-" + sec, tiny, 140, 40, "colorless", keys},
			spec{"shell.code.84x24.colorless.model-" + sec, code, 84, 24, "colorless", keys},
		)
	}
	cases = append(cases,
		spec{"shell.code.84x24.colorless.model", code, 84, 24, "colorless", nil},
		spec{"shell.tiny.140x40.truecolor.model-storage", tiny, 140, 40, "truecolor", []string{"down"}},
	)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := scan.Run(c.m)
			view := renderPrepared(t, c.m, res, c.w, c.h, c.profile, c.keys, Options{Layout: LayoutPanels})
			assertFrame(t, view, c.w, c.h)
			if !strings.Contains(view, "[1]") || !strings.Contains(view, "[0]") {
				t.Fatal("not the shell")
			}
			if c.profile == "colorless" && strings.Contains(view, "\x1b") {
				t.Fatal("colorless frame contains ESC")
			}
			path := filepath.Join("testdata", "golden", c.name+".golden")
			if *updateGolden {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(view), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden: %v (go test -update)", err)
			}
			if string(got) != view {
				t.Errorf("golden mismatch\n%s", firstDiff(string(got), view))
			}
		})
	}
}

// A checkpoint with thousands of tensors still draws a first frame quickly.
func TestShellBigModelFirstFrame(t *testing.T) {
	x := shellModel(t, benchModel(t), 140, 40)
	for _, k := range []string{"", "2", "3", "4"} {
		start := time.Now()
		v := press(x, k).View()
		if d := time.Since(start); d > raceScale*50*time.Millisecond {
			t.Errorf("focus %q: first frame took %s", k, d)
		}
		assertFrame(t, v, 140, 40)
	}
}

func readyShell(tb testing.TB) tea.Model {
	tb.Helper()
	files, _, err := fixture.TinyQwen()
	if err != nil {
		tb.Fatal(err)
	}
	dir := tb.TempDir()
	if err := fixture.Materialize(dir, files); err != nil {
		tb.Fatal(err)
	}
	m, err := model.Load(dir)
	if err != nil {
		tb.Fatal(err)
	}
	return warmShell(tb, m)
}

func warmShell(tb testing.TB, m *model.Model) tea.Model {
	x := shellModel(tb, m, 140, 40)
	x = press(x, "2")
	_ = x.View()
	_ = x.View()
	return x
}

func TestShellBudgets(t *testing.T) {
	x := readyShell(t)
	view := median(9, func() { _ = x.View() })
	if view > 2*time.Millisecond {
		t.Errorf("cached View %s", view)
	}
	key := median(9, func() {
		x, _ = x.Update(tea.KeyMsg{Type: tea.KeyDown})
	})
	if key > 500*time.Microsecond {
		t.Errorf("Update key %s", key)
	}
	resize := median(9, func() {
		x, _ = x.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	})
	if resize > 5*time.Millisecond {
		t.Errorf("Resize %s", resize)
	}
}

func BenchmarkShellView(b *testing.B) {
	x := readyShell(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = x.View()
	}
}

// BenchmarkShellViewCold draws a frame that is not cached: each key moves
// the Checks cursor, so the frame key changes.
func BenchmarkShellViewCold(b *testing.B) {
	x := readyShell(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		k := tea.KeyDown
		if i%2 == 1 {
			k = tea.KeyUp
		}
		x, _ = x.Update(tea.KeyMsg{Type: k})
		_ = x.View()
	}
}

func BenchmarkShellUpdate(b *testing.B) {
	x := readyShell(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		x, _ = x.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
}

func BenchmarkShellResize(b *testing.B) {
	x := readyShell(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := 140
		if i%2 == 0 {
			w = 120
		}
		x, _ = x.Update(tea.WindowSizeMsg{Width: w, Height: 40})
	}
}
