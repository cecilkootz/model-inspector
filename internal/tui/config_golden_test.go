package tui

import (
	"bytes"
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

func TestConfigGoldens(t *testing.T) {
	qwen := loadCfg(t, fixture.ConfigQwenLike())
	hostile := loadCfg(t, fixture.ConfigHostile())
	malformed := loadCfg(t, fixture.ConfigMalformed())
	dup := loadCfg(t, fixture.ConfigDupKey())
	deep := loadCfg(t, fixture.ConfigDeep())
	notObj := loadCfg(t, fixture.ConfigNotObject())
	none := loadCfg(t, nil)
	unread := unreadableModel(t)
	huge := oversizeModel(t)
	nodes := loadCfg(t, fixture.ConfigHuge())

	jump80 := fieldKeys(t, qwen, 80, 24, "heads q / kv")
	jump140 := fieldKeys(t, qwen, 140, 40, "heads q / kv")
	search := append([]string{"2", "/"}, runes("expert")...)
	filter := append(append([]string{}, search...), "tab")
	page := append(append([]string{"2", "w", "/"}, runes("synthetic_items")...), "enter", "enter", "end")
	modal := append(append([]string{"2", "/"}, runes("synthetic_long")...), "enter", "enter", "end")
	tooBig := append(append([]string{"2", "/"}, runes("synthetic_long")...), "enter", "Y")

	type spec struct {
		name, profile string
		m             *model.Model
		w, h          int
		keys          []string
		want          []string
		copy          bool
		hostile       bool
	}
	var cases []spec
	for _, sz := range [][2]int{{80, 24}, {100, 30}, {140, 40}} {
		for _, p := range []string{"truecolor", "ansi16", "colorless", "ascii"} {
			cases = append(cases, spec{
				name: "cfg.tree." + itoa(sz[0]) + "x" + itoa(sz[1]) + "." + p,
				m:    qwen, w: sz[0], h: sz[1], profile: p, keys: []string{"2"},
				want: []string{"KEY FIELDS", "model_type", "qwen4_exp"},
			})
		}
	}
	for _, sz := range [][2]int{{80, 24}, {140, 40}} {
		for _, p := range []string{"truecolor", "colorless"} {
			keys := jump140
			want := []string{"num_key_value_heads", "Key fields"}
			if sz[0] == 80 {
				keys = jump80
				want = []string{"num_key_value_heads"}
			}
			cases = append(cases, spec{
				name: "cfg.jump." + itoa(sz[0]) + "x" + itoa(sz[1]) + "." + p,
				m:    qwen, w: sz[0], h: sz[1], profile: p, keys: keys,
				want: want,
			})
			cases = append(cases, spec{
				name: "cfg.search." + itoa(sz[0]) + "x" + itoa(sz[1]) + "." + p,
				m:    qwen, w: sz[0], h: sz[1], profile: p, keys: search,
				want: []string{"expert", "matches"},
			})
			cases = append(cases, spec{
				name: "cfg.filter." + itoa(sz[0]) + "x" + itoa(sz[1]) + "." + p,
				m:    qwen, w: sz[0], h: sz[1], profile: p, keys: filter,
				want: []string{"FILTER", "hidden by filter"},
			})
		}
	}
	cases = append(cases,
		spec{name: "cfg.expand.140x40.colorless", m: qwen, w: 140, h: 40, profile: "colorless", keys: []string{"2", "w", "e", "pgdown", "pgdown", "pgdown"}, want: []string{"synthetic.override"}},
		spec{name: "cfg.level.140x40.colorless", m: qwen, w: 140, h: 40, profile: "colorless", keys: []string{"2", "w", "+"}, want: []string{"level"}},
		spec{name: "cfg.page.100x30.colorless", m: qwen, w: 100, h: 30, profile: "colorless", keys: page, want: []string{"show 100"}},
		spec{name: "cfg.modal.80x24.colorless", m: qwen, w: 80, h: 24, profile: "colorless", keys: modal, want: []string{"truncated", "64 KiB"}},
		spec{name: "cfg.raw.80x24.colorless", m: qwen, w: 80, h: 24, profile: "colorless", keys: []string{"2", "enter", "r"}, want: []string{"model_type"}},
		spec{name: "cfg.cursor.140x40.colorless", m: qwen, w: 140, h: 40, profile: "colorless", keys: []string{"2", "w", "down"}, want: []string{">"}},
		spec{name: "cfg.copy.80x24.colorless", m: qwen, w: 80, h: 24, profile: "colorless", keys: []string{"2", "y"}, want: []string{"copied path"}, copy: true},
		spec{name: "cfg.toolarge.80x24.colorless", m: qwen, w: 80, h: 24, profile: "colorless", keys: tooBig, want: []string{"too large to copy"}, copy: true},
	)
	for _, sz := range [][2]int{{80, 24}, {140, 40}} {
		for _, p := range []string{"truecolor", "colorless"} {
			hkeys := []string{"2", "w"}
			if sz[0] < 100 {
				hkeys = []string{"2", "enter"}
			}
			cases = append(cases, spec{
				name: "cfg.hostile." + itoa(sz[0]) + "x" + itoa(sz[1]) + "." + p,
				m:    hostile, w: sz[0], h: sz[1], profile: p, keys: hkeys,
				want: []string{"U+001B", "U+202E", "U+200B"}, hostile: true,
			})
		}
	}
	for _, sz := range [][2]int{{80, 24}, {140, 40}} {
		tag := itoa(sz[0]) + "x" + itoa(sz[1]) + ".colorless"
		cases = append(cases,
			spec{name: "cfg.none." + tag, m: none, w: sz[0], h: sz[1], profile: "colorless", keys: []string{"2"}, want: []string{"No config.json"}},
			spec{name: "cfg.unreadable." + tag, m: unread, w: sz[0], h: sz[1], profile: "colorless", keys: []string{"2"}, want: []string{"unreadable"}},
			spec{name: "cfg.malformed." + tag, m: malformed, w: sz[0], h: sz[1], profile: "colorless", keys: []string{"2"}, want: []string{"config.json did not parse."}},
			spec{name: "cfg.dup." + tag, m: dup, w: sz[0], h: sz[1], profile: "colorless", keys: []string{"2"}, want: []string{"config.json did not parse.", "duplicate key"}},
			spec{name: "cfg.deep." + tag, m: deep, w: sz[0], h: sz[1], profile: "colorless", keys: []string{"2"}, want: []string{"nesting deeper than 32"}},
			spec{name: "cfg.notobj." + tag, m: notObj, w: sz[0], h: sz[1], profile: "colorless", keys: []string{"2"}, want: []string{"not a JSON object"}},
			spec{name: "cfg.huge." + tag, m: huge, w: sz[0], h: sz[1], profile: "colorless", keys: []string{"2"}, want: []string{"71.4 MiB", "64 MiB"}},
			spec{name: "cfg.nodes." + tag, m: nodes, w: sz[0], h: sz[1], profile: "colorless", keys: []string{"2"}, want: []string{"Tree disabled"}},
		)
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			view := renderConfigFrame(t, c.m, c.w, c.h, c.profile, c.keys, c.copy)
			assertFrame(t, view, c.w, c.h)
			for _, w := range c.want {
				if !strings.Contains(view, w) {
					t.Errorf("missing %q\n%s", w, view)
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
						a, b := i-20, i+20
						if a < 0 {
							a = 0
						}
						if b > len(view) {
							b = len(view)
						}
						t.Fatalf("ascii byte %#x at %d: %q", view[i], i, view[a:b])
					}
				}
			}
			if c.hostile {
				plain := stripANSI(view)
				if strings.ContainsAny(plain, "\x1b\u202e\u200b") || strings.ContainsRune(plain, 0x1b) {
					t.Fatal("hostile view leaked a raw control or bidi rune")
				}
			}
			// The frame after a tree-cursor move must not be the unmoved frame.
			if strings.Contains(c.name, "cfg.cursor.") {
				base := renderConfigFrame(t, c.m, c.w, c.h, c.profile, []string{"2"}, false)
				if base == view {
					t.Fatal("cursor move did not change the golden frame")
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

func fieldKeys(t *testing.T, m *model.Model, w, h int, label string) []string {
	t.Helper()
	ui := bootConfig(t, m, w, h)
	_ = ui.fieldLines()
	n := 0
	lines := ui.fieldLines()
	i := ui.cfg.fieldCur
	for n < len(lines) {
		if i >= 0 && i < len(lines) && lines[i].kind == fieldRow && lines[i].label == label {
			break
		}
		i++
		n++
		for i < len(lines) && lines[i].kind != fieldRow {
			i++
		}
	}
	keys := []string{"2"}
	for k := 0; k < n; k++ {
		keys = append(keys, "down")
	}
	keys = append(keys, "enter")
	return keys
}

func runes(s string) []string {
	out := make([]string, 0, len(s))
	for _, r := range s {
		out = append(out, string(r))
	}
	return out
}

func unreadableModel(t *testing.T) *model.Model {
	t.Helper()
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "config.json")); err != nil {
		t.Fatal(err)
	}
	writeShard(t, dir)
	m, err := model.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	m.Dir = "/model"
	return m
}

func oversizeModel(t *testing.T) *model.Model {
	t.Helper()
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	// 71.4 MiB logical size. The reader refuses it from the stat, so the
	// file stays sparse.
	const size = int64(74868327)
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	f.Close()
	writeShard(t, dir)
	m, err := model.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	m.Dir = "/model"
	return m
}

func renderConfigFrame(t *testing.T, m *model.Model, w, h int, profile string, keys []string, copy bool) string {
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
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("TMUX", "")
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
	if copy {
		yes := true
		ui.cfg.copyTTY = &yes
		ui.cfg.copyTo = &bytes.Buffer{}
	}
	var x tea.Model = ui
	x, _ = x.Update(tea.WindowSizeMsg{Width: w, Height: h})
	for _, k := range keys {
		x, _ = x.Update(keyMsg(k))
	}
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
