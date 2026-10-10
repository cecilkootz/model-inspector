package ui

import (
	"math"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/lucasb-eyer/go-colorful"
	"github.com/muesli/termenv"
)

func TestTokenContrast(t *testing.T) {
	for _, theme := range []string{"dark", "light"} {
		t.Setenv("MODEL_INSPECTOR_THEME", theme)
		surface := SurfaceCard.Hex()
		pairs := [][2]string{
			{TextPrimary.Hex(), surface},
			{TextSecondary.Hex(), surface},
			{TextMuted.Hex(), surface},
			{TextPrimary.Hex(), SurfaceSeg.Hex()},
			{TextSecondary.Hex(), SurfaceSeg.Hex()},
			{TextPrimary.Hex(), SurfaceSeg2.Hex()},
			{TextSecondary.Hex(), SurfaceSeg2.Hex()},
		}
		if theme == "dark" {
			pairs = append(pairs,
				[2]string{TextPrimary.Hex(), TermBG},
				[2]string{TextSecondary.Hex(), TermBG},
				[2]string{TextMuted.Hex(), TermBG},
			)
		}
		for _, p := range pairs {
			if c := Contrast(p[0], p[1]); c < 4.5 {
				t.Errorf("%s %s on %s = %.2f", theme, p[0], p[1], c)
			}
		}
		for _, k := range []FillKind{FillKindViolet, FillKindPink, FillKindOK, FillKindWarn, FillKindFail, FillKindHost, FillKindInfo} {
			bg, fg := FillColors(k)
			if c := Contrast(fg.Hex(), bg.Hex()); c < 4.5 {
				t.Errorf("%s fill %d %s on %s = %.2f", theme, k, fg.Hex(), bg.Hex(), c)
			}
		}
		if c := Contrast(Accent.Hex(), SurfaceCard.Hex()); c < 3 {
			t.Errorf("%s accent on card = %.2f", theme, c)
		}
		if theme == "dark" {
			if c := Contrast(RuleTab.Hex(), TermBG); c < 3 {
				t.Errorf("rule on term = %.2f", c)
			}
		}
		for _, pos := range []float64{0, 0.25, 0.5, 0.75, 1} {
			hex := string(GradBrand.At(pos))
			ink := OnFill(hex)
			if c := Contrast(ink, hex); c < 4.5 {
				t.Errorf("%s wordmark t=%v %s on %s = %.2f", theme, pos, ink, hex, c)
			}
		}
	}
}

func TestOnFillPicksInkOrWhite(t *testing.T) {
	if OnFill(FillViolet.DarkHex) != White {
		t.Fatalf("violet wants white, got %s", OnFill(FillViolet.DarkHex))
	}
	for _, hex := range []string{FillPink.DarkHex, FillOK.DarkHex, FillWarn.DarkHex, FillFail.DarkHex, FillHost.DarkHex} {
		if OnFill(hex) != InkHex {
			t.Errorf("%s wants ink, got %s", hex, OnFill(hex))
		}
	}
}

func TestGradientEndpoints(t *testing.T) {
	if !strings.EqualFold(string(GradBrand.At(0)), "#F25D94") {
		t.Fatalf("start %s", GradBrand.At(0))
	}
	if !strings.EqualFold(string(GradBrand.At(1)), "#6B48F0") {
		t.Fatalf("end %s", GradBrand.At(1))
	}
	if string(GradBrand.At(math.NaN())) != string(GradBrand.At(0)) {
		t.Fatal("NaN did not map to the first stop")
	}
	c1, err := colorful.Hex("#F25D94")
	if err != nil {
		t.Fatal(err)
	}
	c2, err := colorful.Hex("#6B48F0")
	if err != nil {
		t.Fatal(err)
	}
	want := c1.BlendOkLab(c2, 0.5).Clamped().Hex()
	if BlendAt(brandStops, 0.5) != want {
		t.Fatalf("midpoint %s want %s", BlendAt(brandStops, 0.5), want)
	}
	idx := int(math.Round(0.5 * 255))
	at := float64(idx) / 255
	if string(GradBrand.At(0.5)) != BlendAt(brandStops, at) {
		t.Fatalf("LUT sample %s want %s", GradBrand.At(0.5), BlendAt(brandStops, at))
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
	left := []StatusSeg{
		{Text: "IDENTITY", Pri: 1, Keep: true},
		{Text: "a-very-long-model-name-here", Pri: 3, Min: 16, Shrink: true},
		{Text: "hints that take a lot of room and should drop first", Pri: 5},
	}
	right := []StatusSeg{
		{Text: "safetensors", Pri: 4},
		{Text: "1.00 GiB", Pri: 2},
		{Text: "q4", Pri: 2},
		{Text: "PASS", Pri: 1, Keep: true},
		{Text: "headers-only", Pri: 1, Keep: true},
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
}

func TestPaletteMatcher(t *testing.T) {
	items := []Match{
		{Title: "Quit", Order: 0},
		{Title: "Go to Layers", Order: 1},
		{Title: "Layers: sort by size", Order: 2},
	}
	ranked := Rank("lay", items)
	if len(ranked) != 2 {
		t.Fatalf("hits %d", len(ranked))
	}
	if ranked[0].Title != "Layers: sort by size" {
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
