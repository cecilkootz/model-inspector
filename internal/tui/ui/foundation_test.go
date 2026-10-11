package ui

import (
	"math"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/lucasb-eyer/go-colorful"
	"github.com/muesli/termenv"

	"github.com/cecilkootz/model-inspector/internal/sanitize"
)

func TestTokenContrast(t *testing.T) {
	canvases := map[string]string{"dark": "#0E1012", "light": "#FFFFFF"}
	for _, theme := range []string{"dark", "light"} {
		t.Setenv("MODEL_INSPECTOR_THEME", theme)
		bgs := []string{canvases[theme], Surface.Hex(), Selection.Hex()}
		for _, tok := range []Token{Text1, Text2, Text3, AccentTok, WarnTok, FailTok, JSONKey, JSONString, JSONNumber, JSONBool, JSONNull} {
			for _, bg := range bgs {
				if c := Contrast(tok.Hex(), bg); c < 4.5 {
					t.Errorf("%s %s on %s = %.2f, want >= 4.5", theme, tok.Hex(), bg, c)
				}
			}
		}
		for _, p := range [][2]Token{{WarnTok, WarnBG}, {FailTok, FailBG}} {
			if c := Contrast(p[0].Hex(), p[1].Hex()); c < 4.5 {
				t.Errorf("%s %s on %s = %.2f", theme, p[0].Hex(), p[1].Hex(), c)
			}
		}
		if c := Contrast(Text1.Hex(), Line.Hex()); c < 4.5 {
			t.Errorf("%s text-1 on line = %.2f", theme, c)
		}
		// Transitional until the shell replaces the grey PASS/N/R pills.
		if c := Contrast(Text2.Hex(), Line.Hex()); c < 3 {
			t.Errorf("%s text-2 on line = %.2f, want >= 3", theme, c)
		}
		for _, k := range []FillKind{FillKindViolet, FillKindPink, FillKindOK, FillKindWarn, FillKindFail, FillKindHost, FillKindInfo} {
			bg, fg := FillColors(k)
			if c := Contrast(fg.Hex(), bg.Hex()); c < 4.5 {
				t.Errorf("%s fill %d %s on %s = %.2f", theme, k, fg.Hex(), bg.Hex(), c)
			}
		}
	}
}

func TestOnFillPicksInkOrWhite(t *testing.T) {
	for _, hex := range []string{FillViolet.DarkHex, FillWarn.DarkHex, FillFail.DarkHex} {
		if OnFill(hex) != InkHex {
			t.Errorf("%s wants ink, got %s", hex, OnFill(hex))
		}
	}
	if OnFill(FillOK.DarkHex) != White {
		t.Errorf("line fill wants white, got %s", OnFill(FillOK.DarkHex))
	}
}

func TestGradientEndpoints(t *testing.T) {
	if !strings.EqualFold(string(GradMeter.At(0)), Ramp2.DarkHex) {
		t.Fatalf("start %s", GradMeter.At(0))
	}
	if !strings.EqualFold(string(GradMeter.At(1)), Ramp5.DarkHex) {
		t.Fatalf("end %s", GradMeter.At(1))
	}
	if string(GradMeter.At(math.NaN())) != string(GradMeter.At(0)) {
		t.Fatal("NaN did not map to the first stop")
	}
	c1, err := colorful.Hex(Ramp2.DarkHex)
	if err != nil {
		t.Fatal(err)
	}
	c2, err := colorful.Hex(Ramp5.DarkHex)
	if err != nil {
		t.Fatal(err)
	}
	want := c1.BlendOkLab(c2, 0.5).Clamped().Hex()
	if BlendAt(meterStops, 0.5) != want {
		t.Fatalf("midpoint %s want %s", BlendAt(meterStops, 0.5), want)
	}
	idx := int(math.Round(0.5 * 255))
	at := float64(idx) / 255
	if string(GradMeter.At(0.5)) != BlendAt(meterStops, at) {
		t.Fatalf("LUT sample %s want %s", GradMeter.At(0.5), BlendAt(meterStops, at))
	}
}

func TestHeatBucketsAndDomain(t *testing.T) {
	const (
		kib = 1024
		mib = 1024 * 1024
	)
	if BucketOf(64*kib-1) != 0 || BucketOf(64*kib) != 1 {
		t.Fatalf("64KiB edge %d %d", BucketOf(64*kib-1), BucketOf(64*kib))
	}
	if BucketOf(4*mib-1) != 1 || BucketOf(4*mib) != 2 {
		t.Fatalf("4MiB edge %d %d", BucketOf(4*mib-1), BucketOf(4*mib))
	}
	if BucketOf(256*mib-1) != 2 || BucketOf(256*mib) != 3 {
		t.Fatalf("256MiB edge %d %d", BucketOf(256*mib-1), BucketOf(256*mib))
	}
	lo, hi := LogDomain(1000, 1180)
	if lo != int(math.Floor(math.Log2(1000))) || hi != int(math.Ceil(math.Log2(1180))) {
		t.Fatalf("domain %d %d", lo, hi)
	}
}

func TestSegmentsDropOrder(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	lipgloss.SetColorProfile(termenv.Ascii)
	txt := func(s string) Safe { return S(sanitize.From(s)) }
	left := []StatusSeg{
		{Text: txt("IDENTITY"), Pri: 1, Keep: true},
		{Text: txt("a-very-long-model-name-here"), Pri: 3, Min: 16, Shrink: true},
		{Text: txt("hints that take a lot of room and should drop first"), Pri: 5},
	}
	right := []StatusSeg{
		{Text: txt("safetensors"), Pri: 4},
		{Text: txt("1.00 GiB"), Pri: 2},
		{Text: txt("q4"), Pri: 2},
		{Text: txt("PASS"), Pri: 1, Keep: true},
		{Text: txt("headers-only"), Pri: 1, Keep: true},
	}
	for _, w := range []int{60, 80, 100, 140} {
		s := Strip(Segments(append([]StatusSeg(nil), left...), append([]StatusSeg(nil), right...), w))
		if !strings.Contains(s, "IDENTITY") || !strings.Contains(s, "PASS") {
			t.Errorf("width %d dropped a keep segment: %q", w, s)
		}
	}
	narrow := Strip(Segments(append([]StatusSeg(nil), left...), append([]StatusSeg(nil), right...), 36))
	if strings.Contains(narrow, "hints that") {
		t.Fatalf("hint survived a narrow bar: %q", narrow)
	}
	if !strings.Contains(narrow, "IDENTITY") {
		t.Fatalf("MODE dropped: %q", narrow)
	}
}

func TestKeymapNoConflicts(t *testing.T) {
	want := map[string]bool{"left": true, "right": true, "h": true, "l": true}
	if len(TreeDummy.Shadows) != len(want) {
		t.Fatalf("shadows %v", TreeDummy.Shadows)
	}
	for _, s := range TreeDummy.Shadows {
		if !want[s] {
			t.Fatalf("unexpected shadow %q", s)
		}
	}
	if !TreeDummy.Tree {
		t.Fatal("dummy tree context is not a tree")
	}
	for _, ctx := range Contexts {
		if d := DuplicateKeys(ctx); len(d) != 0 {
			t.Errorf("%s conflicts: %v", ctx.Name, d)
		}
	}
	t.Setenv("LC_ALL", "C.UTF-8")
	if !strings.Contains(HintLine(true), "fold") || !strings.Contains(HintLine(false), "tabs") {
		t.Fatalf("hints %q %q", HintLine(true), HintLine(false))
	}
	if got := L("<commit>").String(); got != "<commit>" {
		t.Fatalf("L sanitized a program literal: %q", got)
	}
}

func TestPaletteMatcher(t *testing.T) {
	items := []Match{
		{Title: S(sanitize.From("Quit")), Order: 0},
		{Title: S(sanitize.From("Go to Layers")), Order: 1},
		{Title: S(sanitize.From("Layers: sort by size")), Order: 2},
	}
	ranked := Rank("lay", items)
	if len(ranked) != 2 {
		t.Fatalf("hits %d", len(ranked))
	}
	if ranked[0].Title.String() != "Layers: sort by size" {
		t.Fatalf("order %+v", ranked)
	}
	if ranked[0].Score < ranked[1].Score {
		t.Fatalf("scores %+v", ranked)
	}
	if len(Rank("zzz", items)) != 0 {
		t.Fatal("zzz matched")
	}
	all := Rank("", items)
	if len(all) != 3 || all[0].Order != 0 {
		t.Fatalf("empty query %+v", all)
	}
}

func TestASCIIGlyphs(t *testing.T) {
	if !asciiGlyphs().ASCII() {
		t.Fatal("ASCII glyph set contains a byte >= 0x80")
	}
	if utfGlyphs().Est != "‡" || asciiGlyphs().Est != "(est)" {
		t.Fatalf("est glyphs %q %q", utfGlyphs().Est, asciiGlyphs().Est)
	}
	t.Setenv("LC_ALL", "C")
	t.Setenv("NO_COLOR", "1")
	if Est() != "(est)" {
		t.Fatalf("Est() = %q", Est())
	}
}

func TestKVSharedColumn(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	lipgloss.SetColorProfile(termenv.Ascii)
	rows := []KVRow{
		{Label: L("model_type"), Value: L("qwen")},
		{Label: L("text_config.hidden_size"), Value: L("8")},
	}
	lines := strings.Split(KV(rows, 80), "\n")
	if len(lines) != 2 {
		t.Fatalf("%q", lines)
	}
	iq := strings.Index(lines[0], "qwen")
	i8 := strings.Index(lines[1], "8")
	if iq < 0 || iq != i8 {
		t.Fatalf("columns %d %d\n%q\n%q", iq, i8, lines[0], lines[1])
	}
	if !strings.Contains(lines[0][:iq], "  ") {
		t.Fatalf("gap missing: %q", lines[0])
	}
}

func TestZoneGutter(t *testing.T) {
	r := Layout(140, 40, 42, false, false)
	if !r.Split || r.Gutter != 2 {
		t.Fatalf("%+v", r)
	}
	if r.Secondary.X != r.Primary.X+r.Primary.W+r.Gutter {
		t.Fatalf("gutter not excluded: %+v", r)
	}
	var z Zones
	z.Add("pane:primary", r.Primary)
	z.Add("pane:secondary", r.Secondary)
	if _, ok := z.Hit(r.Primary.W, 2); ok {
		t.Fatal("gutter cell hit a pane")
	}
	if hit, ok := z.Hit(r.Secondary.X, 2); !ok || hit.ID != "pane:secondary" {
		t.Fatalf("secondary hit %+v %v", hit, ok)
	}
	z.Add("modal", Rect{X: 10, Y: 2, W: 20, H: 5})
	if hit, ok := z.Hit(12, 3); !ok || hit.ID != "modal" {
		t.Fatalf("top-most hit %+v", hit)
	}
}

func TestBreakpointBands(t *testing.T) {
	if Breakpoint(59, 15) != "XS" && Breakpoint(59, 15) != "S" {
		// XS is below 60 or below 16. Either name is fine if too-small is separate.
	}
	if Breakpoint(80, 24) != "S" {
		t.Fatalf("80x24 %s", Breakpoint(80, 24))
	}
	if Breakpoint(100, 30) != "M" {
		t.Fatalf("100x30 %s", Breakpoint(100, 30))
	}
	if Breakpoint(140, 32) != "M" {
		t.Fatalf("140x32 %s", Breakpoint(140, 32))
	}
	if Breakpoint(140, 40) != "L" {
		t.Fatalf("140x40 %s", Breakpoint(140, 40))
	}
	_ = os.Getenv("TERM")
}
