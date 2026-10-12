package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cecilkootz/model-inspector/internal/scan"
)

func FuzzScanUpdate(f *testing.F) {
	f.Add([]byte("2j eEi][cyY \x1b"))
	f.Add([]byte("\x1b\x9b\u202e\u2066"))
	f.Fuzz(func(t *testing.T, data []byte) {
		m := loadTiny(t)
		res := scan.Run(m)
		if len(res.Checks) > 0 {
			res.Checks[0].Details = []string{string(data)}
			res.Checks[0].Summary = string(data)
		}
		var x tea.Model = New(m, res)
		x, _ = x.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
		x, _ = x.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
		steps := 0
		for i := 0; i < len(data) && steps < 40; i++ {
			steps++
			b := data[i]
			switch b % 5 {
			case 0:
				x, _ = x.Update(tea.WindowSizeMsg{Width: int(b)%180 + 40, Height: int(b)%40 + 16})
			case 1:
				x, _ = x.Update(tea.KeyMsg{Type: fzKeys[int(b)%len(fzKeys)]})
			default:
				x, _ = x.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{fzRunes[int(b)%len(fzRunes)]}})
			}
			v := x.View()
			if r, bad := rawBad(v); bad {
				t.Fatalf("raw %s", r)
			}
		}
	})
}
