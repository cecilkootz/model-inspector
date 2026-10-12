package tui

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/cecilkootz/model-inspector/internal/fixture"
	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/scan"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files")

// Goldens are recorded for a host without unified memory.
func TestMain(m *testing.M) {
	model.AppleSilicon = false
	os.Exit(m.Run())
}

func TestUILLiterals(t *testing.T) {
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		s := string(b)
		for {
			i := strings.Index(s, "ui.L(")
			if i < 0 {
				return nil
			}
			rest := s[i+5:]
			if !strings.HasPrefix(rest, `"`) {
				t.Errorf("%s: ui.L argument is not a string literal", path)
				return nil
			}
			s = rest
		}
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestQwenShapeHeat(t *testing.T) {
	m := loadFiles(t, mustShape(t))
	view := stripView(renderPrepared(t, m, scan.Run(m), 140, 40, "colorless", []string{"3"}))
	for _, w := range []string{"+18%", "flat"} {
		if !strings.Contains(view, w) {
			t.Errorf("missing %q\n%s", w, view)
		}
	}
	if strings.Contains(view, "mlx-community") {
		t.Fatal("the screen hard-codes the mockup repo")
	}
}

// renderPrepared draws a frame from an already-built scan result. Durations
// are zeroed so a slow machine does not change "<1 ms".
func renderPrepared(t *testing.T, m *model.Model, res scan.Result, w, h int, profile string, keys []string, opts ...Options) string {
	t.Helper()
	for i := range res.Checks {
		res.Checks[i].Duration = 0
	}
	return renderUI(t, w, h, profile, keys, func() Model {
		ui := New(m, res)
		for _, o := range opts {
			ui.apply(o)
		}
		return ui
	})
}

// renderUI sets the colour profile, builds the model, sizes it, sends keys
// and returns the frame.
func renderUI(t *testing.T, w, h int, profile string, keys []string, build func() Model) string {
	t.Helper()
	prevNC, hadNC := os.LookupEnv("NO_COLOR")
	t.Cleanup(func() {
		if hadNC {
			os.Setenv("NO_COLOR", prevNC)
		} else {
			os.Unsetenv("NO_COLOR")
		}
		lipgloss.SetColorProfile(termenv.Ascii)
		SetNoColor(false)
	})
	t.Setenv("MODEL_INSPECTOR_MOTION", "0")
	t.Setenv("MODEL_INSPECTOR_MOUSE", "1")
	t.Setenv("MODEL_INSPECTOR_THEME", "dark")
	SetNoColor(false)
	if profile == "ascii" {
		t.Setenv("LC_ALL", "C")
		t.Setenv("LANG", "C")
	} else {
		t.Setenv("LC_ALL", "C.UTF-8")
		t.Setenv("LANG", "C.UTF-8")
	}
	switch profile {
	case "colorless", "ascii":
		os.Setenv("NO_COLOR", "1")
		lipgloss.SetColorProfile(termenv.Ascii)
	case "ansi256":
		os.Unsetenv("NO_COLOR")
		lipgloss.SetColorProfile(termenv.ANSI256)
	case "ansi16":
		os.Unsetenv("NO_COLOR")
		lipgloss.SetColorProfile(termenv.ANSI)
	default:
		os.Unsetenv("NO_COLOR")
		lipgloss.SetColorProfile(termenv.TrueColor)
	}
	lipgloss.SetHasDarkBackground(true)
	var x tea.Model = build()
	x, _ = x.Update(tea.WindowSizeMsg{Width: w, Height: h})
	for _, k := range keys {
		x, _ = x.Update(keyMsg(k))
	}
	// View calls configure, which would force ASCII if NO_COLOR is set.
	// Re-apply the profile for the colour paths after that.
	if profile != "colorless" && profile != "ascii" {
		os.Unsetenv("NO_COLOR")
		switch profile {
		case "ansi256":
			lipgloss.SetColorProfile(termenv.ANSI256)
		case "ansi16":
			lipgloss.SetColorProfile(termenv.ANSI)
		default:
			lipgloss.SetColorProfile(termenv.TrueColor)
		}
	}
	return x.View()
}

func keyMsg(k string) tea.Msg {
	switch k {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "pgup":
		return tea.KeyMsg{Type: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyMsg{Type: tea.KeyPgDown}
	case "home":
		return tea.KeyMsg{Type: tea.KeyHome}
	case "end":
		return tea.KeyMsg{Type: tea.KeyEnd}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
}

func firstDiff(want, got string) string {
	ws, gs := strings.Split(want, "\n"), strings.Split(got, "\n")
	n := len(ws)
	if len(gs) < n {
		n = len(gs)
	}
	for i := 0; i < n; i++ {
		if ws[i] != gs[i] {
			return "line " + itoa(i) + "\nwant " + showESC(ws[i]) + "\ngot  " + showESC(gs[i])
		}
	}
	return "line count want " + itoa(len(ws)) + " got " + itoa(len(gs))
}

func showESC(s string) string {
	return strings.ReplaceAll(s, "\x1b", "␛")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func loadFiles(t *testing.T, files []fixture.File) *model.Model {
	t.Helper()
	dir := t.TempDir()
	if err := fixture.Materialize(dir, files); err != nil {
		t.Fatal(err)
	}
	m, err := model.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	// The temp directory name changes every run. The screen prints it.
	m.Dir = "/model"
	return m
}

func mustFiles(t *testing.T, fn func() ([]fixture.File, int64, error)) []fixture.File {
	t.Helper()
	files, _, err := fn()
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func mustCode(t *testing.T) []fixture.File {
	t.Helper()
	files, err := fixture.CodeExec()
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func mustShape(t *testing.T) []fixture.File {
	t.Helper()
	files, err := fixture.QwenShape()
	if err != nil {
		t.Fatal(err)
	}
	return files
}
