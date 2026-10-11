package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cecilkootz/model-inspector/internal/fixture"
	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/scan"
	"github.com/cecilkootz/model-inspector/internal/tui/ui"
)

func checksShell(t *testing.T, m *model.Model, strict bool, w, h int) Model {
	t.Helper()
	res := scan.RunOptions(m, scan.Options{Strict: strict})
	for i := range res.Checks {
		res.Checks[i].Duration = 0
	}
	n := New(m, res)
	n.apply(Options{Layout: LayoutPanels, Now: func() time.Time { return time.Unix(100, 0) }})
	x, _ := n.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return press(x, "2").(Model)
}

func loadFixture(t *testing.T, fn func() ([]fixture.File, error)) *model.Model {
	t.Helper()
	files, err := fn()
	if err != nil {
		t.Fatal(err)
	}
	return loadFiles(t, files)
}

func hostileAgent(t *testing.T) *model.Model { return loadFixture(t, fixture.AgentHostile) }

// Every check of every fixture gets its sections, wraps inside the pane and
// prints no raw control bytes.
func TestChecksDetailSections(t *testing.T) {
	fixtures := []struct {
		name   string
		m      *model.Model
		strict bool
	}{
		{"tiny", loadTiny(t), false},
		{"code-exec", loadCode(t), false},
		{"code-exec strict", loadCode(t), true},
		{"metadata mismatch", loadFixture(t, fixture.MetadataMismatch), false},
		{"metadata mismatch strict", loadFixture(t, fixture.MetadataMismatch), true},
		{"agent tampered", loadFixture(t, fixture.AgentTampered), false},
		{"agent hostile", hostileAgent(t), false},
		{"code scan", loadFixture(t, fixture.CodeScan), true},
	}
	for _, f := range fixtures {
		for _, w := range []int{12, 30, 46, 60, 100} {
			cur := checksShell(t, f.m, f.strict, 140, 40)
			p := cur.sidePanels()[1].(*checksPanel)
			for i, c := range cur.scan.Checks {
				p.move(i - p.cursor())
				label := fmt.Sprintf("%s w=%d check %s", f.name, w, c.ID)
				text := strings.Join(p.detail(w, 30), "\n")
				plain := strings.Join(strings.Fields(ui.Strip(text)), " ")

				want := []string{"PROBLEMS"}
				if scan.Rule(c.ID) != "" {
					want = append(want, "RULE")
				}
				if scan.Why(c.ID) != "" {
					want = append(want, "WHY IT MATTERS")
				}
				if scan.NotProven(c.ID) != "" {
					want = append(want, "DOES NOT PROVE")
				}
				if _, facts, _ := splitDetails(c); len(facts) > 0 {
					want = append(want, "FACTS")
				}
				if i == 0 {
					want = append(want, "NOT COVERED BY ANY CHECK (5)")
				}
				for _, s := range want {
					if w < 30 {
						break
					}
					if !strings.Contains(plain, s) {
						t.Errorf("%s: missing section %q", label, s)
					}
				}
				if i != 0 && w >= 30 && !strings.Contains(plain, "Not a malware or behavior scan") {
					t.Errorf("%s: no disclaimer footer", label)
				}
				for _, legacy := range []string{"DETAIL", "CHECKED", "selected check"} {
					if strings.Contains(plain, legacy) {
						t.Errorf("%s: legacy text %q", label, legacy)
					}
				}
				for _, ln := range strings.Split(text, "\n") {
					if got := ui.Width(ln); got > w {
						t.Errorf("%s: line is %d cells in a %d-cell pane: %q", label, got, w, ui.Strip(ln))
					}
				}
				if raw, bad := rawBad(text); bad {
					t.Errorf("%s: raw %s", label, raw)
				}
			}
		}
	}
}

func TestChecksStrictPromotionRow(t *testing.T) {
	m := loadFixture(t, fixture.MetadataMismatch)
	for _, strict := range []bool{false, true} {
		cur := checksShell(t, m, strict, 140, 40)
		p := cur.sidePanels()[1].(*checksPanel)
		for i, c := range cur.scan.Checks {
			row := ui.Strip(p.rows(36)[i])
			if got := strings.Contains(row, "(warn)"); got != (strict && c.ID == scan.CheckMetadata) {
				t.Errorf("strict=%v %s: (warn) present=%v in %q", strict, c.ID, got, row)
			}
			if strict && c.ID == scan.CheckMetadata {
				if !strings.Contains(row, ui.G().Fail) || !isPromoted(c) {
					t.Errorf("promoted row %q does not use the fail glyph", row)
				}
				p.move(i - p.cursor())
				if !strings.Contains(strings.Join(p.detail(60, 30), "\n"), "promoted") {
					t.Error("detail does not say the warning was promoted")
				}
			}
		}
	}
}

func TestChecksSummaryRows(t *testing.T) {
	cur := checksShell(t, loadCode(t), false, 140, 40)
	p := cur.sidePanels()[1].(*checksPanel)
	got := make([]string, 0, 3)
	for _, r := range p.summary(36) {
		got = append(got, strings.TrimSpace(ui.Strip(r)))
	}
	if len(got) != 3 || !strings.HasPrefix(got[0], ui.G().Warn+" Code-executing formats") ||
		!strings.HasPrefix(got[1], ui.G().Warn+" Code & pickle contents") ||
		!strings.Contains(got[2], "4 passed") {
		t.Fatalf("summary rows %q", got)
	}
}

// A long detail scrolls inside the detail pane and keeps its tail reachable.
func TestChecksDetailScrolls(t *testing.T) {
	m := loadFixture(t, fixture.AgentTampered)
	x := press(shellModel(t, m, 84, 24), "2", "]", "0")
	top := screenOf(x)
	if strings.Contains(strings.Join(top, "\n"), "Structural checks only") {
		t.Fatal("the footer is on screen before scrolling; the fixture is too short")
	}
	x = press(x, "G")
	_ = x.View()
	if x.(Model).sh.top == 0 {
		t.Fatal("G did not scroll the detail")
	}
	if !strings.Contains(strings.Join(screenOf(x), "\n"), "Structural checks only") {
		t.Fatal("the end of the detail is not reachable")
	}
	x = press(x, "pgup")
	_ = x.View()
	if got := x.(Model).sh.top; got == 0 {
		t.Fatal("pgup jumped to the top")
	}
}

// Hostile check text never reaches the screen raw, in any profile.
func TestChecksHostileFrames(t *testing.T) {
	m := hostileAgent(t)
	res := scan.Run(m)
	for _, p := range []string{"colorless", "truecolor", "ascii"} {
		for _, sz := range [][2]int{{84, 24}, {140, 40}} {
			view := renderPrepared(t, m, res, sz[0], sz[1], p, []string{"2", "]"}, Options{Layout: LayoutPanels})
			assertFrame(t, view, sz[0], sz[1])
			if raw, bad := rawBad(view); bad {
				t.Fatalf("%s %v: raw %s", p, sz, raw)
			}
			if !strings.Contains(ui.Strip(view), "Chat template") {
				t.Fatalf("%s %v: the agent check is not selected", p, sz)
			}
		}
	}
}

func TestWrapRuns(t *testing.T) {
	long := strings.Repeat("x", 25)
	lines := wrapRuns([]run{{text: "ab " + long + " cd"}}, 10)
	var got []string
	for _, ln := range lines {
		got = append(got, ui.Strip(paintRuns(ln)))
	}
	want := []string{"ab", "xxxxxxxxxx", "xxxxxxxxxx", "xxxxx cd"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("wrapped %q, want %q", got, want)
	}
	if lines := wrapRuns([]run{{text: "   "}}, 10); len(lines) != 0 {
		t.Fatalf("blank text wrapped to %d lines", len(lines))
	}
	for _, w := range []int{1, 2, 3} {
		for _, ln := range wrapRuns([]run{{text: "日本語のテキスト and more"}}, w) {
			if got := ui.Width(paintRuns(ln)); got > max(w, 2) {
				t.Errorf("width %d: line is %d cells", w, got)
			}
		}
	}
}

func TestQuoteRunsAndSnippets(t *testing.T) {
	var muted []string
	for _, r := range quoteRuns(`eos "<|im_end|>", pad "x`, ui.ToneStrong) {
		if r.tone == ui.ToneMuted {
			muted = append(muted, r.text)
		}
	}
	if len(muted) != 1 || muted[0] != `"<|im_end|>"` {
		t.Fatalf("muted spans %q", muted)
	}
	head, snip := splitSnippet("URL in template in tokenizer_config.json at byte 63 (2 matches): d the key")
	if head != "URL in template in tokenizer_config.json at byte 63 (2 matches)" || snip != "d the key" {
		t.Fatalf("split %q / %q", head, snip)
	}
	if head, snip = splitSnippet("modeling_demo.py"); head != "modeling_demo.py" || snip != "" {
		t.Fatalf("plain detail split %q / %q", head, snip)
	}
}

func TestSplitDetailsAgent(t *testing.T) {
	c := scan.Check{
		ID: scan.CheckAgent, Status: scan.StatusWarn, EffectiveStatus: scan.StatusWarn,
		Summary: "2 findings, strongest: x; tool calling: no",
		Details: []string{"f1", "f2", "fact: a", "fact: b", "... and 3 more"},
	}
	p, f, more := splitDetails(c)
	if len(p) != 2 || len(f) != 2 || more != "... and 3 more" {
		t.Fatalf("split %q %q %q", p, f, more)
	}
	c.Status, c.EffectiveStatus = scan.StatusPass, scan.StatusPass
	if p, f, _ = splitDetails(c); len(p) != 0 || len(f) != 4 {
		t.Fatalf("passing agent check: %q %q", p, f)
	}
	c = scan.Check{ID: scan.CheckFiles, Status: scan.StatusWarn, EffectiveStatus: scan.StatusWarn, Details: []string{"a", "b"}}
	if p, f, _ = splitDetails(c); len(p) != 2 || len(f) != 0 {
		t.Fatalf("other checks keep every detail as a problem: %q %q", p, f)
	}
}
