package tui

import (
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"origin.cursor.com/cecilkootz/model-inspector/internal/model"
)

func TestP3_ColorForced(t *testing.T) {
	prev, had := os.LookupEnv("NO_COLOR")
	t.Cleanup(func() {
		if had {
			os.Setenv("NO_COLOR", prev)
		} else {
			os.Unsetenv("NO_COLOR")
		}
		SetNoColor(false)
		lipgloss.SetColorProfile(termenv.Ascii)
	})
	os.Unsetenv("NO_COLOR")
	SetNoColor(false)
	lipgloss.SetColorProfile(termenv.TrueColor)
	m, err := model.Load(hostileDir(t))
	if err != nil {
		t.Fatal(err)
	}
	vs := allViews(m, 80, 24)
	n := 0
	for _, v := range vs {
		n += strings.Count(v, "\x1b")
	}
	if n == 0 {
		t.Fatal("TrueColor with NO_COLOR unset produced no ESC")
	}
	checkViews(t, "forced", vs, false)

	t.Setenv("NO_COLOR", "1")
	lipgloss.SetColorProfile(termenv.TrueColor)
	vs = allViews(m, 80, 24)
	checkViews(t, "forced+NO_COLOR", vs, true)
}

func TestP3_NoColorFlag(t *testing.T) {
	m, err := model.Load(hostileDir(t))
	if err != nil {
		t.Fatal(err)
	}
	SetNoColor(true)
	t.Cleanup(func() {
		SetNoColor(false)
		lipgloss.SetColorProfile(termenv.Ascii)
	})
	lipgloss.SetColorProfile(termenv.TrueColor)
	checkViews(t, "--no-color", allViews(m, 80, 24), true)
}
