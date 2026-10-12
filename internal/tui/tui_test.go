package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cecilkootz/model-inspector/internal/fixture"
	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/scan"
)

func TestHyperConnectionGroupListedWithItsTensors(t *testing.T) {
	m := loadTiny(t)
	var sawHyper bool
	for _, g := range m.Groups {
		if g == nil {
			continue
		}
		if g.Key == "other" || g.Kind == model.KindOther {
			t.Fatalf("layers has an other group: %s %s", g.Key, g.Kind)
		}
		if g.Key == "hyper_connection" && g.Kind == model.KindHyper {
			sawHyper = true
		}
	}
	if !sawHyper {
		t.Fatal("hyper_connection group missing")
	}
	x := press(shellModel(t, m, 180, 40), "3")
	cur := x.(Model)
	p := cur.sidePanels()[2]
	iw := shellSideWidth(180) - 4
	row := -1
	for i, r := range p.rows(iw) {
		if strings.Contains(stripView(r), "hyper_connection") {
			row = i
		}
	}
	if row < 0 {
		t.Fatal("Layers panel does not list hyper_connection")
	}
	p.move(row - p.cursor())
	detail := stripView(strings.Join(p.detail(120, 36), "\n"))
	for _, name := range []string{"input_mix_weight_up.weight", "input_mix_weight_down.weight", "hc_norm.weight"} {
		if !strings.Contains(detail, name) {
			t.Errorf("hyper detail missing %s\n%s", name, detail)
		}
	}
}

func TestViewBeforeFirstSizeAndTinyTerminal(t *testing.T) {
	m := loadTiny(t)
	ui := New(m, scan.Run(m))
	if before := ui.View(); before != "model-inspector" && !strings.HasPrefix(before, "model-inspector") {
		t.Fatalf("view before a window size: %q", before)
	}
	for _, sz := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 60, Height: 16}, {Width: 120, Height: 40}, {Width: 40, Height: 12}} {
		sized, _ := ui.Update(sz)
		view := sized.View()
		assertFrame(t, view, sz.Width, sz.Height)
		if strings.Contains(view, "too small") {
			t.Fatalf("%dx%d rendered the too-small screen:\n%s", sz.Width, sz.Height, view)
		}
	}
	small, _ := ui.Update(tea.WindowSizeMsg{Width: 39, Height: 11})
	smallView := small.View()
	assertFrame(t, smallView, 39, 11)
	if !strings.Contains(smallView, "terminal too small (39x11)") {
		t.Fatalf("too-small screen:\n%s", smallView)
	}
}

func loadTiny(t *testing.T) *model.Model {
	t.Helper()
	files, _, err := fixture.TinyQwen()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := fixture.Materialize(dir, files); err != nil {
		t.Fatal(err)
	}
	m, err := model.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func assertFrame(t *testing.T, view string, width, height int) {
	t.Helper()
	lines := strings.Split(view, "\n")
	if len(lines) != height {
		t.Fatalf("got %d lines, want %d\n%s", len(lines), height, view)
	}
	for i, ln := range lines {
		if w := lipgloss.Width(ln); w != width {
			t.Fatalf("line %d width %d, want %d: %q", i, w, width, ln)
		}
	}
}

// The sharded n-gram table is described once, on the Model panel's Memory
// and Callouts sections, with the heuristic stated.
func TestShardedNgramCallouts(t *testing.T) {
	files, err := fixture.ShardedNgram()
	if err != nil {
		t.Fatal(err)
	}
	m := loadFiles(t, files)
	t.Setenv("LC_ALL", "C.UTF-8")
	det := modelDetails(t, m, 140, 40)
	flat := stripView(strings.Join(det["Memory"], "\n") + "\n" + strings.Join(det["Callouts"], "\n"))
	flat = strings.Join(strings.Fields(flat), " ")
	for _, want := range []string{
		"29.80 GiB of lookup tables can stay in CPU RAM",
		"(heuristic)",
		"ACCELERATOR",
		"n-gram embedding table 29.80 GiB can stay in CPU RAM (heuristic)",
		"~51.20B-param n-gram embedding table",
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("Model panel missing %q\n%s", want, flat)
		}
	}
}

// The group label carries the shard count and the host mark.
func TestLayersShardedNgramMark(t *testing.T) {
	files, err := fixture.ShardedNgram()
	if err != nil {
		t.Fatal(err)
	}
	m := loadFiles(t, files)
	t.Setenv("LC_ALL", "C.UTF-8")
	view := renderPrepared(t, m, scan.Run(m), 100, 30, "colorless", []string{"3"})
	if strings.Contains(view, "\x1b") {
		t.Fatal("colorless frame contains ESC")
	}
	for _, want := range []string{"n-gram table (PLE)", "⌂", "×128"} {
		if !strings.Contains(view, want) {
			t.Errorf("missing %q\n%s", want, view)
		}
	}
}
