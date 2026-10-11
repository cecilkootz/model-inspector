package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/tui/ui"
)

var modelSections = []string{"Overview", "Storage", "Memory", "Callouts", "Files"}

func modelDetails(t *testing.T, m *model.Model, w, h int) map[string][]string {
	t.Helper()
	cur := shellModel(t, m, w, h).(Model)
	p := cur.sidePanels()[0]
	paneW := w - shellSideWidth(w) - 4
	out := map[string][]string{}
	for i, name := range modelSections {
		p.move(i - p.cursor())
		out[name] = p.detail(paneW, h-4)
	}
	return out
}

func modelFixtures(t *testing.T) map[string]*model.Model {
	t.Helper()
	fx := shellFixtures(t)
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, "model.gguf"), []byte("GGUF\x00not-a-real-header"), 0o644); err != nil {
		t.Fatal(err)
	}
	gguf, err := model.Load(d)
	if err != nil {
		t.Fatal(err)
	}
	fx["gguf"] = gguf
	h := loadDir(hostileDir(t))
	h.RepoID, h.Revision = "org/ev\x1b]0;PWN\x07il\u202e", "rev\x1b[2J0123456789abcdef"
	fx["hostile"] = h
	return fx
}

func repeatKey(s string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = s
	}
	return out
}

// Every section stays inside the detail pane at every supported size.
func TestModelPanelSectionsFit(t *testing.T) {
	for name, m := range modelFixtures(t) {
		for _, sz := range [][2]int{{140, 40}, {100, 30}, {84, 24}} {
			paneW := sz[0] - shellSideWidth(sz[0]) - 4
			for sec, lines := range modelDetails(t, m, sz[0], sz[1]) {
				if len(lines) == 0 {
					t.Errorf("%s %dx%d %s: empty", name, sz[0], sz[1], sec)
				}
				for i, ln := range lines {
					if w := ui.Width(ln); w > paneW {
						t.Errorf("%s %dx%d %s row %d is %d cells, pane is %d: %q", name, sz[0], sz[1], sec, i, w, paneW, ui.Strip(ln))
					}
				}
			}
			for i := range modelSections {
				x := press(shellModel(t, m, sz[0], sz[1]), append([]string{"1"}, repeatKey("down", i)...)...)
				assertFrame(t, x.View(), sz[0], sz[1])
			}
		}
	}
}

// File text is sanitized once: visible tokens, no raw controls, and no
// token whose "<" was escaped a second time.
func TestModelPanelHostileSanitizedOnce(t *testing.T) {
	m := modelFixtures(t)["hostile"]
	sawToken := false
	for _, sz := range [][2]int{{140, 40}, {84, 24}} {
		for sec, lines := range modelDetails(t, m, sz[0], sz[1]) {
			text := strings.Join(lines, "\n")
			if raw, bad := rawBad(text); bad {
				t.Fatalf("%s: raw %s", sec, raw)
			}
			for _, raw := range []string{"\x1b]", "\x1b[2J", "\u202e", "\u009b", "\u2028"} {
				if strings.Contains(text, raw) {
					t.Errorf("%s: contains %q", sec, raw)
				}
			}
			if strings.Contains(text, "<U+003C>U+") {
				t.Errorf("%s: sanitized twice", sec)
			}
			sawToken = sawToken || strings.Contains(text, "<U+")
		}
	}
	if !sawToken {
		t.Error("no sanitizer token reached the panel")
	}
}

func TestModelPanelGGUFAndPartial(t *testing.T) {
	fx := modelFixtures(t)
	det := modelDetails(t, fx["gguf"], 140, 40)
	for _, sec := range []string{"Overview", "Storage", "Memory"} {
		if !strings.Contains(ui.Strip(strings.Join(det[sec], "\n")), "GGUF") {
			t.Errorf("gguf %s does not say GGUF is unsupported", sec)
		}
	}
	if s := ui.Strip(strings.Join(det["Memory"], "\n")); strings.Contains(s, "WEIGHTS") {
		t.Errorf("gguf memory invents a weights figure:\n%s", s)
	}
	if s := ui.Strip(strings.Join(det["Files"], "\n")); !strings.Contains(s, "model.gguf") {
		t.Errorf("gguf files:\n%s", s)
	}

	det = modelDetails(t, fx["partial"], 140, 40)
	for _, sec := range []string{"Overview", "Memory"} {
		if !strings.Contains(strings.Join(det[sec], "\n"), "incomplete") {
			t.Errorf("partial %s does not say incomplete", sec)
		}
	}
}

func TestModelPanelContent(t *testing.T) {
	tiny := shellFixtures(t)["tiny"]
	det := modelDetails(t, tiny, 140, 40)
	text := func(sec string) string { return ui.Strip(strings.Join(det[sec], "\n")) }
	for _, want := range []string{"org", "revision 0123456789ab", "SIZE ON DISK", "PARAMETERS", "QUANT", "LAYERS", "EXPERTS", "STORAGE", "CALLOUTS"} {
		if !strings.Contains(text("Overview"), want) {
			t.Errorf("overview missing %q:\n%s", want, text("Overview"))
		}
	}
	if strings.Contains(text("Overview"), "WEIGHTS") {
		t.Error("overview still has the weights tile")
	}
	for _, sec := range modelSections {
		for _, bad := range []string{"BYTES · LAYER", "routed exp", "DETAIL"} {
			if strings.Contains(text(sec), bad) {
				t.Errorf("%s still draws the heatmap or a legacy heading (%q)", sec, bad)
			}
		}
	}
	for _, want := range []string{"CLASS", "BYTES", "SHARE"} {
		if !strings.Contains(text("Storage"), want) {
			t.Errorf("storage missing %q:\n%s", want, text("Storage"))
		}
	}
	mem := text("Memory")
	if strings.Count(mem, "weights only; excludes KV cache") != 1 {
		t.Errorf("memory note not printed exactly once:\n%s", mem)
	}
	for _, want := range []string{"WEIGHTS", "UNIFIED MEMORY", "NOTES"} {
		if !strings.Contains(mem, want) {
			t.Errorf("memory missing %q:\n%s", want, mem)
		}
	}
	files := text("Files")
	for _, want := range []string{"config.json", "shard", "config"} {
		if !strings.Contains(files, want) {
			t.Errorf("files missing %q:\n%s", want, files)
		}
	}
	if got := len(strings.Split(files, "\n")); got != len(tiny.Files) {
		t.Errorf("files has %d rows for %d files", got, len(tiny.Files))
	}
}

func TestModelPanelHeaderUnit(t *testing.T) {
	x := shellModel(t, shellFixtures(t)["tiny"], 140, 40)
	if h := screenOf(x)[0]; !strings.Contains(h, "~86 params") {
		t.Errorf("header %q", h)
	}
}

func TestModelPanelSummary(t *testing.T) {
	cur := shellModel(t, shellFixtures(t)["tiny"], 140, 40).(Model)
	got := cur.sidePanels()[0].summary(30)
	if len(got) != 3 {
		t.Fatalf("summary has %d rows", len(got))
	}
	for i, want := range []string{"Overview", "Storage", "Callouts"} {
		if !strings.Contains(ui.Strip(got[i]), want) {
			t.Errorf("summary row %d = %q, want %s", i, ui.Strip(got[i]), want)
		}
	}
}

func TestMidElideAndWrapHard(t *testing.T) {
	p := "/home/user/.cache/huggingface/hub/models--org--name/snapshots/0123456789abcdef0123456789abcdef01234567"
	for _, w := range []int{8, 20, 40, 60} {
		got := midElide(p, w)
		if ui.Width(got) > w {
			t.Errorf("midElide w=%d: %q", w, got)
		}
		if w >= 20 && (!strings.HasSuffix(got, "4567") || !strings.HasPrefix(got, "/ho")) {
			t.Errorf("midElide w=%d lost its ends: %q", w, got)
		}
	}
	for _, ln := range wrapHard("short "+strings.Repeat("x", 50)+" 日本語日本語日本語", 12) {
		if ui.Width(ln) > 12 {
			t.Errorf("wrapHard line %q is %d cells", ln, ui.Width(ln))
		}
	}
}
