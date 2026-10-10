package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"origin.cursor.com/cecilkootz/model-inspector/internal/fixture"
	"origin.cursor.com/cecilkootz/model-inspector/internal/model"
	"origin.cursor.com/cecilkootz/model-inspector/internal/scan"
)

func TestViewHasNoFileDerivedControls(t *testing.T) {
	d := t.TempDir()
	// Raw strings keep the JSON \u escapes so the files parse and the
	// decoded text actually contains ESC, OSC, and a bidi override.
	hdr := `{"__metadata__":{"format":"mlx\u001b]0;pwned\u0007\u202e"},"zz\u001b]8;;http://x\u0007LINK":{"dtype":"F32","shape":[1],"data_offsets":[0,4]}}`
	if err := os.WriteFile(filepath.Join(d, "model.safetensors"), fixture.EncodeHeader([]byte(hdr), []byte{0, 0, 0, 0}), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := `{"layer_types":["\u001b]52;c;cHduZWQ=\u0007"],"quantization":{"bits":4,"mode":"\u001b[2Jfake"}}`
	if err := os.WriteFile(filepath.Join(d, "config.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := model.Load(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Tensors) != 1 || m.Config == nil {
		t.Fatalf("hostile files did not parse: tensors=%d config=%v warnings=%v", len(m.Tensors), m.Config != nil, m.Warnings)
	}
	m.Revision = "rev\x1b[2J"
	ui := New(m, scan.Run(m))
	var x tea.Model = ui
	x, _ = x.Update(tea.WindowSizeMsg{Width: 200, Height: 60})
	views := []string{x.View()}
	x, _ = x.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	views = append(views, x.View())
	x, _ = x.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	x, _ = x.Update(tea.KeyMsg{Type: tea.KeyEnter})
	views = append(views, x.View())
	x, _ = x.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("4")})
	views = append(views, x.View())

	for i, view := range views {
		for _, r := range view {
			if r < 0x20 && r != '\n' {
				t.Fatalf("view %d contains %#U\n%s", i, r, view)
			}
		}
		for _, raw := range []string{"\x1b]0;pwned", "\x1b[2J", "\x1b]52;c;", "\x1b]8;;http://x", "\u202e"} {
			if strings.Contains(view, raw) {
				t.Fatalf("view %d still contains %q", i, raw)
			}
		}
	}
	all := strings.Join(views, "\n")
	for _, tok := range []string{"<U+001B>", "<U+202E>", "<U+0007>"} {
		if !strings.Contains(all, tok) {
			t.Errorf("missing visible token %s", tok)
		}
	}
}
