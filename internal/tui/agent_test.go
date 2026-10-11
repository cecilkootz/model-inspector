package tui

import (
	"os"
	"strings"
	"testing"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cecilkootz/model-inspector/internal/fixture"
	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/scan"
)

func TestAgentCardHostileViewHasNoRawControls(t *testing.T) {
	files, err := fixture.AgentHostile()
	if err != nil {
		t.Fatal(err)
	}
	d := t.TempDir()
	if err := fixture.Materialize(d, files); err != nil {
		t.Fatal(err)
	}
	m, err := model.Load(d)
	if err != nil {
		t.Fatal(err)
	}
	var x tea.Model = New(m, scan.Run(m))
	x, _ = x.Update(tea.WindowSizeMsg{Width: 200, Height: 60})
	x, _ = x.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("4")})
	for i := 0; i < 8; i++ {
		x, _ = x.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	x, _ = x.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	view := x.View()
	if !strings.Contains(view, "Chat template") {
		t.Fatalf("no agent card in view\n%s", view)
	}
	for _, r := range view {
		if (r < 0x20 && r != '\n') || (r >= 0x7f && r <= 0x9f) || r == 0x202e || r == 0x200b || unicode.Is(unicode.Cf, r) {
			t.Fatalf("view contains %#U", r)
		}
	}
	if os.Getenv("MODEL_INSPECTOR_DEBUG_VIEW") != "" {
		t.Log(view)
	}
}
