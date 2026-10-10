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

	"origin.cursor.com/cecilkootz/model-inspector/internal/fixture"
	"origin.cursor.com/cecilkootz/model-inspector/internal/model"
	"origin.cursor.com/cecilkootz/model-inspector/internal/scan"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files")

func TestGolden(t *testing.T) {
	tiny := loadFiles(t, mustFiles(t, fixture.TinyQwenMLXQuant))
	ngram := loadFiles(t, mustNgram(t))
	code := loadFiles(t, mustCode(t))
	shape := loadFiles(t, mustShape(t))

	type spec struct {
		name    string
		m       *model.Model
		w, h    int
		profile string
		keys    []string
		want    []string
	}
	var cases []spec
	sizes := [][2]int{{60, 16}, {80, 24}, {100, 30}, {140, 40}}
	profiles := []string{"truecolor", "ansi256", "ansi16", "colorless", "ascii"}
	overlays := []struct {
		name string
		keys []string
	}{
		{"none", nil},
		{"help", []string{"?"}},
		{"palette", []string{":", "l", "a", "y"}},
		{"disclosure", []string{"4", "c"}},
	}
	for _, sz := range sizes {
		for _, p := range profiles {
			for _, o := range overlays {
				cases = append(cases, spec{
					name: "tiny",
					m:    tiny,
					w:    sz[0], h: sz[1],
					profile: p,
					keys:    o.keys,
				})
				cases[len(cases)-1].name = "tiny." + itoa(sz[0]) + "x" + itoa(sz[1]) + "." + p + "." + o.name
			}
		}
	}
	for _, sz := range [][2]int{{80, 24}, {140, 40}} {
		for _, p := range []string{"truecolor", "colorless"} {
			cases = append(cases, spec{
				name: "ngram." + itoa(sz[0]) + "x" + itoa(sz[1]) + "." + p,
				m:    ngram, w: sz[0], h: sz[1], profile: p,
				want: []string{"n-gram embedding table", "HOST"},
			})
		}
	}
	for _, sz := range [][2]int{{80, 24}, {140, 40}} {
		for _, p := range []string{"truecolor", "ansi16", "colorless"} {
			want := []string{"+18%", "qwen3_next"}
			if sz[1] >= 36 {
				want = append(want, "flat")
			}
			cases = append(cases, spec{
				name: "qwen." + itoa(sz[0]) + "x" + itoa(sz[1]) + "." + p,
				m:    shape, w: sz[0], h: sz[1], profile: p,
				want: want,
			})
		}
	}
	cases = append(cases,
		spec{name: "layers.80x24.truecolor.down2", m: tiny, w: 80, h: 24, profile: "truecolor", keys: []string{"3", "j", "j"}, want: []string{"3 Layers"}},
		spec{name: "layers.80x24.colorless.down2", m: tiny, w: 80, h: 24, profile: "colorless", keys: []string{"3", "j", "j"}, want: []string{"3 Layers", ">layer 0"}},
		spec{name: "layers.80x24.colorless.down5", m: tiny, w: 80, h: 24, profile: "colorless", keys: []string{"3", "j", "j", "j", "j", "j"}, want: []string{"3 Layers", ">hyper_connec"}},
		spec{name: "code.80x24.colorless", m: code, w: 80, h: 24, profile: "colorless", want: []string{"!"}},
		spec{name: "toosmall.59x15.colorless", m: tiny, w: 59, h: 15, profile: "colorless", want: []string{"needs at least 60×16", "59×15"}},
		spec{name: "config.80x24.colorless", m: tiny, w: 80, h: 24, profile: "colorless", keys: []string{"2"}, want: []string{"model_type", "Key fields"}},
	)

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			view := renderGolden(t, c.m, c.w, c.h, c.profile, c.keys)
			assertFrame(t, view, c.w, c.h)
			for _, w := range c.want {
				if !strings.Contains(view, w) {
					t.Errorf("missing %q", w)
				}
			}
			if c.profile == "colorless" || c.profile == "ascii" {
				if strings.Contains(view, "\x1b") {
					t.Error("colorless view contains ESC")
				}
			}
			if c.profile == "ascii" {
				for i := 0; i < len(view); i++ {
					if view[i] >= 0x80 {
						a, b := i-24, i+24
						if a < 0 {
							a = 0
						}
						if b > len(view) {
							b = len(view)
						}
						t.Fatalf("ascii chrome byte %#x at %d: %q", view[i], i, view[a:b])
					}
				}
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
	t.Setenv("LC_ALL", "C.UTF-8")
	t.Setenv("NO_COLOR", "1")
	view := renderIdentity(m, 160)
	for _, w := range []string{"+18%", "flat", "qwen3_next"} {
		if !strings.Contains(view, w) {
			t.Errorf("missing %q\n%s", w, view)
		}
	}
	if strings.Contains(view, "mlx-community") {
		t.Fatal("identity hard-coded the mockup repo")
	}
}

func renderGolden(t *testing.T, m *model.Model, w, h int, profile string, keys []string) string {
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
	ui := New(m, scan.Run(m))
	var x tea.Model = ui
	x, _ = x.Update(tea.WindowSizeMsg{Width: w, Height: h})
	for _, k := range keys {
		x, _ = x.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
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

func mustNgram(t *testing.T) []fixture.File {
	t.Helper()
	files, err := fixture.NgramTable()
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
