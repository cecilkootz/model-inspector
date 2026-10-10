package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"origin.cursor.com/cecilkootz/model-inspector/internal/model"
	"origin.cursor.com/cecilkootz/model-inspector/internal/scan"
)

func TestScanGolden(t *testing.T) {
	pass := loadTiny(t)
	warn := loadCode(t)
	fail := loadPastEOF(t)
	partial := loadDangling(t)
	// A temp-dir path middle-truncates to a random suffix. Pin the name.
	pass.RepoID = "org/pass"
	warn.RepoID = "org/warn"
	fail.RepoID = "org/fail"
	partial.RepoID = "org/partial"
	type spec struct {
		name    string
		m       *model.Model
		strict  bool
		w, h    int
		profile string
		keys    []string
	}
	var cases []spec
	for _, sz := range [][2]int{{80, 24}, {140, 40}} {
		for _, p := range []string{"truecolor", "ansi256", "colorless"} {
			cases = append(cases,
				spec{"scan.pass." + dim(sz) + "." + p, pass, false, sz[0], sz[1], p, []string{"4"}},
				spec{"scan.fail." + dim(sz) + "." + p, fail, false, sz[0], sz[1], p, []string{"4"}},
				spec{"scan.strict." + dim(sz) + "." + p, warn, true, sz[0], sz[1], p, []string{"4"}},
				spec{"scan.partial." + dim(sz) + "." + p, partial, false, sz[0], sz[1], p, []string{"4"}},
			)
		}
	}
	cases = append(cases, spec{
		name: "scan.pass.80x24.colorless.cursor", m: pass, w: 80, h: 24, profile: "colorless",
		keys: []string{"4", "down"},
	})
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := scan.RunOptions(c.m, scan.Options{Strict: c.strict})
			view := renderPrepared(t, c.m, res, c.w, c.h, c.profile, c.keys)
			assertFrame(t, view, c.w, c.h)
			if c.profile == "colorless" && strings.Contains(view, "\x1b") {
				t.Fatal("colorless frame contains ESC")
			}
			if !strings.Contains(view, "Not a malware") && !strings.Contains(view, "not a malware") {
				t.Error("missing the structure-only line")
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

func dim(sz [2]int) string {
	return itoa(sz[0]) + "x" + itoa(sz[1])
}
