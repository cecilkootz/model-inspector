package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cecilkootz/model-inspector/internal/fixture"
	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/report"
	"github.com/cecilkootz/model-inspector/internal/scan"
)

func TestVerdictAgreesWithTextAndJSON(t *testing.T) {
	pass := loadTiny(t)
	warn := loadCode(t)
	fail := loadPastEOF(t)
	partial := loadDangling(t)
	cases := []struct {
		name   string
		m      *model.Model
		strict bool
		want   scan.Status
	}{
		{"pass", pass, false, scan.StatusPass},
		{"warn", warn, false, scan.StatusWarn},
		{"fail", fail, false, scan.StatusFail},
		{"strict", warn, true, scan.StatusFail},
		{"partial", partial, false, scan.StatusFail},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := finishScan(c.m, c.strict, false)
			doc := report.Build(c.m, res, false, 0)
			res = finishScan(c.m, c.strict, false)
			doc = report.Build(c.m, res, false, 0)
			if res.Overall != c.want {
				t.Fatalf("overall %s want %s", res.Overall, c.want)
			}
			if doc.Scan.Overall != string(res.Overall) {
				t.Fatalf("document overall %s scan %s", doc.Scan.Overall, res.Overall)
			}
			if doc.Partial != c.m.Partial {
				t.Fatalf("document partial %v model %v", doc.Partial, c.m.Partial)
			}
			raw, err := report.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			var parsed struct {
				Partial bool `json:"partial"`
				Scan    struct {
					Overall string `json:"overall"`
					Strict  bool   `json:"strict"`
				} `json:"scan"`
			}
			if err := json.Unmarshal(raw, &parsed); err != nil {
				t.Fatal(err)
			}
			if parsed.Scan.Overall != doc.Scan.Overall || parsed.Partial != doc.Partial || parsed.Scan.Strict != c.strict {
				t.Fatalf("json %+v doc overall %s partial %v", parsed, doc.Scan.Overall, doc.Partial)
			}
			text := report.Text(doc)
			if c.m.Partial && !strings.Contains(text, "(incomplete)") {
				t.Fatalf("text missing incomplete:\n%s", text)
			}
			if c.strict && !strings.Contains(text, "strict: warnings count as failures") {
				t.Fatalf("text missing strict:\n%s", text)
			}
			code := report.ExitCode(doc)
			switch {
			case doc.Scan.Overall == string(scan.StatusFail):
				if code != 1 {
					t.Fatalf("exit %d", code)
				}
			default:
				if code != 0 {
					t.Fatalf("exit %d", code)
				}
			}
			ui := New(c.m, res)
			ui.width = 140
			if ui.scan.Overall != scan.Status(doc.Scan.Overall) || ui.scan.Strict != doc.Scan.Strict {
				t.Fatalf("tui %+v", ui.scan.Overall)
			}
			title, _ := verdictTitle(c.m, ui.scan)
			if c.m.Partial && !strings.Contains(title, "INSPECTION INCOMPLETE") {
				t.Fatalf("band %q", title)
			}
			if c.name == "strict" {
				if !strings.Contains(title, "FAILS UNDER --strict") {
					t.Fatalf("band %q", title)
				}
			}
		})
	}
}

func loadCode(t *testing.T) *model.Model {
	t.Helper()
	return loadFiles(t, mustCode(t))
}

func loadPastEOF(t *testing.T) *model.Model {
	t.Helper()
	hdr := `{"model.layers.0.self_attn.q_proj.weight":{"dtype":"BF16","shape":[2],"data_offsets":[0,4]}}`
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "model.safetensors"), fixture.EncodeHeader([]byte(hdr), []byte{0x00}), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := model.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func loadDangling(t *testing.T) *model.Model {
	t.Helper()
	files, _, err := fixture.TinyQwen()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := fixture.Materialize(dir, files); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "config.json")
	if err := os.Remove(cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing-config.json", cfg); err != nil {
		t.Fatal(err)
	}
	m, err := model.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Partial {
		t.Fatal("dangling config was not partial")
	}
	return m
}
