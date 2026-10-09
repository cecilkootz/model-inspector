package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mwelch/model-inspector/internal/fixture"
	"github.com/mwelch/model-inspector/internal/model"
	"github.com/mwelch/model-inspector/internal/scan"
)

func TestLayersViewKeepsHyperConnectionTogether(t *testing.T) {
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
	ui := New(m, scan.Run(m))
	sized, _ := ui.Update(tea.WindowSizeMsg{Width: 180, Height: 40})
	layers, _ := sized.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	view := layers.(Model).View()
	if !strings.Contains(view, "hyper_connection") || !strings.Contains(view, "ngram") {
		t.Fatalf("group table missing hyper_connection or ngram:\n%s", view)
	}
	if strings.Contains(view, "other") {
		t.Fatalf("grouping view still has an other row:\n%s", view)
	}

	hyper := drill(t, layers.(Model), m, "hyper_connection")
	for _, name := range []string{
		"hyper_connection_mixer.input_mix_weight_up.weight",
		"hyper_connection_mixer.input_mix_weight_down.weight",
		"hyper_connection_mixer.hc_norm.weight",
	} {
		if !strings.Contains(hyper, name) {
			t.Errorf("hyper group view missing %s\n%s", name, hyper)
		}
	}
	norms := drill(t, layers.(Model), m, "norms")
	if !strings.Contains(norms, "model.norm.weight") {
		t.Fatalf("norms view = %s", norms)
	}
	if strings.Contains(norms, "hyper_connection_mixer") {
		t.Fatalf("hc_norm still rendered under norms:\n%s", norms)
	}
}

func drill(t *testing.T, ui Model, m *model.Model, key string) string {
	t.Helper()
	idx := -1
	for i, g := range m.Groups {
		if g.Key == key {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("no group %s", key)
	}
	ui.groupTable.SetCursor(idx)
	drilled, _ := ui.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return drilled.(Model).View()
}
