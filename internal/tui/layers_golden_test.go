package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLayersGolden(t *testing.T) {
	m := loadLayersV2(t)
	type spec struct {
		name    string
		w, h    int
		profile string
		keys    []string
		want    []string
	}
	cases := []spec{
		{name: "layersv2.80x24.truecolor", w: 80, h: 24, profile: "truecolor", keys: []string{"3"}, want: []string{"LAYERS", "log2", "n-gram table (PLE)", "hyper_connection"}},
		{name: "layersv2.80x24.ansi256", w: 80, h: 24, profile: "ansi256", keys: []string{"3"}, want: []string{"LAYERS", "log2", "n-gram table (PLE)"}},
		{name: "layersv2.80x24.colorless", w: 80, h: 24, profile: "colorless", keys: []string{"3"}, want: []string{"LAYERS", "log2", "TREE", "n-gram table (PLE)", "⌂"}},
		{name: "layersv2.80x24.colorless.cursor", w: 80, h: 24, profile: "colorless", keys: []string{"3", "j", "j"}, want: []string{"LAYERS", ">"}},
		{name: "layersv2.80x24.truecolor.open", w: 80, h: 24, profile: "truecolor", keys: []string{"3", "j", "j", "l"}, want: []string{"LAYERS", "log2"}},
		{name: "layersv2.140x40.truecolor", w: 140, h: 40, profile: "truecolor", keys: []string{"3"}, want: []string{"LAYERS", "log2", "TREE", "DETAIL", "n-gram table (PLE)"}},
		{name: "layersv2.140x40.ansi256", w: 140, h: 40, profile: "ansi256", keys: []string{"3"}, want: []string{"LAYERS", "DETAIL"}},
		{name: "layersv2.140x40.colorless", w: 140, h: 40, profile: "colorless", keys: []string{"3"}, want: []string{"LAYERS", "log2", "TREE", "DETAIL", "NAME", "n-gram table (PLE)"}},
	}
	plain := renderGolden(t, m, 80, 24, "colorless", []string{"3"})
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			view := renderGolden(t, m, c.w, c.h, c.profile, c.keys)
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
			if c.name == "layersv2.80x24.colorless.cursor" && view == plain {
				t.Fatal("cursor frame matches the unmoved frame")
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
