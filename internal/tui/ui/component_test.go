package ui

import (
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/cecilkootz/model-inspector/internal/sanitize"
)

func txt(s string) Safe { return S(sanitize.From(s)) }

func TestItoaMinInt(t *testing.T) {
	got := itoa(math.MinInt)
	if strings.Contains(got, "-") && len(got) < 4 {
		t.Fatalf("itoa(MinInt) = %q", got)
	}
	if !strings.HasPrefix(got, "-") {
		t.Fatalf("itoa(MinInt) = %q", got)
	}
	if itoa(0) != "0" || itoa(10) != "10" || itoa(-42) != "-42" {
		t.Fatalf("itoa samples %q %q %q", itoa(0), itoa(10), itoa(-42))
	}
}

func TestPow2Clamped(t *testing.T) {
	if pow2(0) != 1 || pow2(10) != 1024 {
		t.Fatalf("pow2 small %d %d", pow2(0), pow2(10))
	}
	if pow2(63) != 1<<63 || pow2(99) != 1<<63 {
		t.Fatalf("pow2 saturate %d %d", pow2(63), pow2(99))
	}
	// The signed shift that used to recurse is a uint64 power of two.
	if humanPow(pow2(63)) == "" || strings.Contains(humanPow(1<<63), "-") {
		t.Fatalf("humanPow(2^63) = %q", humanPow(pow2(63)))
	}
	if !strings.Contains(humanPow(1024), "KiB") {
		t.Fatalf("1 KiB: %q", humanPow(1024))
	}
	if BucketOf(-1) != 3 {
		t.Fatalf("negative bucket %d", BucketOf(-1))
	}
}

func TestLitCacheCap(t *testing.T) {
	litMu.Lock()
	litCache = map[string]sanitize.Text{}
	litOrder = nil
	litMu.Unlock()
	for i := 0; i < litCap+40; i++ {
		L(strings.Repeat("k", i+1))
	}
	litMu.Lock()
	n := len(litCache)
	litMu.Unlock()
	if n > litCap {
		t.Fatalf("literal cache %d, cap %d", n, litCap)
	}
	if L("model-inspector").String() == "" {
		t.Fatal("empty literal")
	}
}

func TestWidgetsRender(t *testing.T) {
	profiles := []struct {
		name string
		set  func(t *testing.T)
	}{
		{"colorless", func(t *testing.T) {
			t.Setenv("NO_COLOR", "1")
			t.Setenv("LC_ALL", "C")
			lipgloss.SetColorProfile(termenv.Ascii)
		}},
		{"truecolor", func(t *testing.T) {
			t.Setenv("NO_COLOR", "")
			t.Setenv("LC_ALL", "C.UTF-8")
			lipgloss.SetColorProfile(termenv.TrueColor)
			lipgloss.SetHasDarkBackground(true)
		}},
		{"ansi16", func(t *testing.T) {
			t.Setenv("NO_COLOR", "")
			t.Setenv("LC_ALL", "C.UTF-8")
			lipgloss.SetColorProfile(termenv.ANSI)
		}},
	}
	for _, p := range profiles {
		t.Run(p.name, func(t *testing.T) {
			p.set(t)
			Configure()
			if Title(txt("STORAGE"), txt("on disk"), 40) == "" {
				t.Fatal("empty title")
			}
			if Button(txt("esc"), txt("Cancel"), false, true) == "" {
				t.Fatal("empty button")
			}
			if Button(txt("c"), txt("later"), true, false) == "" {
				t.Fatal("empty disabled button")
			}
			if _, over := Meter(1.4, 12, GradMeter); !over {
				t.Fatal("meter overflow")
			}
			if s, over := Meter(0.25, 8, GradQ); s == "" || over {
				t.Fatal("meter")
			}
			rows, legend := Stacked([]Seg{
				{Label: txt("q4 packed"), Frac: 0.6, Grad: GradQ},
				{Label: txt("plain"), Frac: 0.4, Flat: DTypeBF16.Hex()},
			}, 20, 2)
			if len(rows) != 2 || legend == "" {
				t.Fatalf("stacked %d %q", len(rows), legend)
			}
			tile := Tile(TileSpec{Label: sanitize.From("SIZE"), Value: sanitize.From("1 GiB"), Sub: sanitize.From("2 files"), Edge: "#4A505C"}, 16, 3)
			if len(tile) < 3 {
				t.Fatalf("tile %d", len(tile))
			}
			band := TileRow([]TileSpec{
				{Label: sanitize.From("A"), Value: sanitize.From("1"), Sub: sanitize.From("a"), Edge: "#4A505C"},
				{Label: sanitize.From("B"), Value: sanitize.From("2"), Sub: sanitize.From("b"), Edge: "#8FD14F"},
			}, 40, 3)
			if len(band) == 0 {
				t.Fatal("tile row")
			}
			panel := LegacyPanel(sanitize.From("SCAN"), sanitize.From("PASS"), []string{"one", "two"}, 30, 6, true)
			if len(panel) == 0 {
				t.Fatal("panel")
			}
			call := Callout(CalloutSpec{Kind: FillKindHost, Tag: sanitize.From("HOST"), Title: sanitize.From("ngram table"), Right: sanitize.From("est")}, 40)
			if len(call) == 0 {
				t.Fatal("callout")
			}
			modal := Modal(ModalSpec{
				Title: txt("COMMANDS"), Body: txt("one\ntwo"), Buttons: Chrome(Button(txt("esc"), txt("close"), false, true)), Width: 40,
			}, 80, 24)
			if !strings.Contains(modal, "COMMANDS") && !strings.Contains(Strip(modal), "COMMANDS") {
				t.Fatal("modal missing title")
			}
			lay := ModalLayoutOf(ModalSpec{Title: txt("COMMANDS"), Body: txt("a\nb"), Width: 40}, 80, 24)
			if lay.W < 10 || lay.H < 3 || lay.ContentTop < 1 {
				t.Fatalf("layout %+v", lay)
			}
			list := &VList{Rows: []Safe{txt("alpha"), txt("beta"), txt("gamma"), txt("delta")}, Height: 2}
			list.Move(2)
			list.Page(1)
			view := list.View(20, true)
			if !strings.Contains(view, "gamma") && !strings.Contains(Strip(view), "gamma") && !strings.Contains(Strip(view), "delta") {
				t.Fatalf("vlist %q", view)
			}
			if TreeRow(2, true, false, txt("block"), 30) == "" {
				t.Fatal("tree")
			}
			if Est() == "" {
				t.Fatal("est")
			}
			grid := HeatGrid{Rows: []HeatRow{
				{Label: txt("attention"), Cells: []int64{1000, 1180, 1000}, HasMTP: true, MTP: 900},
				{Label: txt("router"), Cells: []int64{64, 64, 64}},
				{Label: txt("routed exp"), Cells: []int64{64, 64, 64}},
			}, Full: []bool{false, true, false}, Note: txt("per-layer"), Caption: txt("excludes ple 1 KiB")}
			lines := Heatmap(grid, 80, false)
			if len(lines) < 3 {
				t.Fatalf("heatmap %d", len(lines))
			}
			wide := Heatmap(grid, 140, true)
			if len(wide) == 0 {
				t.Fatal("wide heatmap")
			}
			if Heatmap(HeatGrid{}, 40, false) == nil {
				t.Fatal("empty heatmap")
			}
			_ = Paint("x", TextPrimary.Hex(), FillViolet.Hex(), true)
			_ = Pill(FillKindOK, sanitize.From("PASS"))
			_ = Frame("hi", 10, 3)
			_ = MiddleTrunc("abcdefghijklmnopqrstuvwxyz", 8, "…")
		})
	}
}

func TestMedianCached(t *testing.T) {
	cells := make([]int64, 5000)
	for i := range cells {
		cells[i] = int64(i + 1)
	}
	row := HeatRow{Label: txt("attention"), Cells: cells}
	row.prepare()
	if !row.ready || len(row.spread) == 0 {
		t.Fatal("spread not cached")
	}
	before := row.spread[0]
	text, flat := row.spreadLine()
	if text != before || flat {
		t.Fatalf("cached spread %q flat %v", text, flat)
	}
	if median([]int64{1, 100}) != 50 {
		t.Fatalf("even median %d", median([]int64{1, 100}))
	}
}
