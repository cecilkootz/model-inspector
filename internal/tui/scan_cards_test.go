package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cecilkootz/model-inspector/internal/scan"
	"github.com/cecilkootz/model-inspector/internal/tui/ui"
)

// The Scan card list must keep the selected card on screen, draw only whole
// cards, and put each card's click zone on the row where the card is drawn.
func TestScanCardsFitAndHitWhereDrawn(t *testing.T) {
	m := loadTiny(t)
	for _, strict := range []bool{false, true} {
		res := scan.RunOptions(m, scan.Options{Strict: strict})
		for _, sz := range [][2]int{{140, 40}, {120, 30}, {100, 30}, {80, 24}, {60, 16}} {
			name := fmt.Sprintf("%dx%d strict=%v", sz[0], sz[1], strict)
			ui := New(m, res)
			sized, _ := ui.Update(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
			on, _ := sized.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'4'}})
			cur := on.(Model)
			for i := range res.Checks {
				cur.scanCursor = i
				cur.ensureScanVisible()
				screen := strings.Split(stripView(cur.View()), "\n")
				sel := res.Checks[i]
				var selHit *hit
				for k := range cur.scn.hits {
					h := cur.scn.hits[k]
					if !strings.HasPrefix(h.id, "scan:card:") {
						continue
					}
					id := strings.TrimPrefix(h.id, "scan:card:")
					c := checkByID(res, id)
					if h.y < 2 || h.y+h.h > sz[1]-1 {
						t.Errorf("%s cursor %d: card %s rows %d..%d leave the body (status bar at %d)", name, i, id, h.y, h.y+h.h-1, sz[1]-1)
						continue
					}
					if !strings.Contains(screen[h.y], prefix(c.Name, 12)) {
						t.Errorf("%s cursor %d: hit for %q at row %d, row reads %q", name, i, c.Name, h.y, strings.TrimSpace(screen[h.y]))
					}
					if id == sel.ID {
						selHit = &h
					}
				}
				if selHit == nil {
					t.Errorf("%s: selected card %d (%s) is not on screen", name, i, sel.Name)
				}
			}
		}
	}
}

func checkByID(res scan.Result, id string) scan.Check {
	for _, c := range res.Checks {
		if c.ID == id {
			return c
		}
	}
	return scan.Check{}
}

func prefix(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}

func stripView(s string) string { return ui.Strip(s) }
