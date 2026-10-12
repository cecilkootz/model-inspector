package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/cecilkootz/model-inspector/internal/sanitize"
)

type primProfile struct {
	name  string
	prof  termenv.Profile
	color bool
}

var primProfiles = []primProfile{
	{"truecolor", termenv.TrueColor, true},
	{"ansi256", termenv.ANSI256, true},
	{"ansi16", termenv.ANSI, true},
	{"colorless", termenv.Ascii, false},
}

// useProfile pins the colour profile and glyph set for one subtest.
func useProfile(t testing.TB, p primProfile, ascii bool) {
	t.Helper()
	prev := lipgloss.ColorProfile()
	prevDark := lipgloss.HasDarkBackground()
	t.Cleanup(func() {
		lipgloss.SetColorProfile(prev)
		lipgloss.SetHasDarkBackground(prevDark)
	})
	if p.color {
		t.Setenv("NO_COLOR", "")
	} else {
		t.Setenv("NO_COLOR", "1")
	}
	t.Setenv("MODEL_INSPECTOR_THEME", "dark")
	if ascii {
		t.Setenv("LC_ALL", "C")
	} else {
		t.Setenv("LC_ALL", "C.UTF-8")
	}
	lipgloss.SetColorProfile(p.prof)
	lipgloss.SetHasDarkBackground(true)
}

func eachProfile(t *testing.T, f func(t *testing.T)) {
	t.Helper()
	for _, p := range primProfiles {
		for _, ascii := range []bool{false, true} {
			name := p.name + "/utf8"
			if ascii {
				name = p.name + "/ascii"
			}
			t.Run(name, func(t *testing.T) {
				useProfile(t, p, ascii)
				f(t)
			})
		}
	}
}

// badRaw reports a control, C1, bidi or invisible rune left after the SGR
// sequences this package itself draws are removed.
func badRaw(s string) (rune, bool) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && ((s[j] >= '0' && s[j] <= '9') || s[j] == ';' || s[j] == ':') {
				j++
			}
			if j < len(s) && s[j] == 'm' {
				i = j
				continue
			}
		}
		b.WriteByte(s[i])
	}
	for _, r := range b.String() {
		switch {
		case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f,
			r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069,
			r >= 0x200b && r <= 0x200f, r == 0xfeff, r == 0x061c:
			return r, true
		}
	}
	return 0, false
}

func exactWidth(t *testing.T, what string, s string, w int) {
	t.Helper()
	if got := Width(s); got != w {
		t.Fatalf("%s: raw width %d, want %d: %q", what, got, w, s)
	}
	if got := Width(Strip(s)); got != w {
		t.Fatalf("%s: stripped width %d, want %d: %q", what, got, w, s)
	}
	if strings.Contains(s, "\n") {
		t.Fatalf("%s: newline in row %q", what, s)
	}
	if ColorOff() && strings.Contains(s, "\x1b") {
		t.Fatalf("%s: escape in colorless row %q", what, s)
	}
	if r, bad := badRaw(s); bad {
		t.Fatalf("%s: raw %U in %q", what, r, s)
	}
}

var hostileText = sanitize.From("\x1b[31m\x1b]52;c;AAAA\x07evil\u0085‮<script>​" + strings.Repeat("ab", 200))

var primTitles = []sanitize.Text{
	sanitize.Program(""),
	sanitize.Program("Checks"),
	sanitize.Program("A much longer panel title than any border can hold"),
	sanitize.From("日本語のタイトル漢字"),
	hostileText,
}

func counterOf(s string) Safe { return Chrome(s) }

func TestPrimitivesExactWidth(t *testing.T) {
	eachProfile(t, func(t *testing.T) {
		// Built per profile: ink and ListRow paint with the profile in force.
		counters := []Safe{{}, counterOf(ink("6✓ 1! 2·", TextMuted, false)), counterOf("99+ problems found today")}
		body := []string{
			"plain",
			ListRow(RowSpec{Glyph: L(G().Pass), Name: sanitize.Program("row"), Value: sanitize.Program("12 ms")}, 30),
			strings.Repeat("x", 200),
			"",
			"日本語日本語日本語",
		}
		for w := -3; w <= 120; w++ {
			for _, h := range []int{-1, 0, 1, 2, 8} {
				for ti, title := range primTitles {
					for ci, c := range counters {
						for _, focus := range []bool{false, true} {
							num := -1
							if focus {
								num = 2
							}
							{
								rows := Panel(PanelSpec{Num: num, Title: title, Counter: c, Body: body, Width: w, Height: h, Focused: focus})
								if w < 1 || h < 1 {
									if len(rows) != 0 {
										t.Fatalf("w=%d h=%d: %d rows", w, h, len(rows))
									}
									continue
								}
								if len(rows) != h {
									t.Fatalf("w=%d h=%d t=%d: %d rows", w, h, ti, len(rows))
								}
								for i, r := range rows {
									exactWidth(t, fmt.Sprintf("Panel w=%d h=%d t=%d c=%d f=%v n=%d row %d", w, h, ti, ci, focus, num, i), r, w)
								}
							}
						}
					}
				}
			}
			for _, sel := range []bool{false, true} {
				for ti, title := range primTitles {
					{
						tone := Tone(ti % 6)
						for _, indent := range []int{0, 2, 500} {
							r := ListRow(RowSpec{Glyph: L("!"), Tone: tone, Name: title, Value: hostileText, Indent: indent, Selected: sel}, w)
							checkRow(t, "ListRow", r, w)
							r = ListRow(RowSpec{Name: title, Selected: sel, Indent: indent}, w)
							checkRow(t, "ListRow/novalue", r, w)
							r = ListRow(RowSpec{Glyph: L(G().Host), Name: title, Value: sanitize.Program("1 GiB"), Selected: sel}, w)
							checkRow(t, "ListRow/short", r, w)
						}
					}
				}
			}
			for _, title := range primTitles {
				checkRow(t, "SectionLabel", SectionLabel(title, w), w)
				spans := []Span{{Text: L("model-inspector"), Tone: ToneAccent, Bold: true}, {Text: title, Tone: ToneStrong, Bold: true}, {Text: L("4.2 GiB")}}
				right := []Span{{Text: L("! 1 warn"), Tone: ToneWarn, Bold: true}, {Text: title}}
				checkRow(t, "HeaderLine", HeaderLine(spans, right, w), w)
				checkRow(t, "HeaderLine/noright", HeaderLine(spans, nil, w), w)
				checkRow(t, "HeaderLine/empty", HeaderLine(nil, nil, w), w)
				hints := []KeyHint{{L("↑↓"), L("move")}, {L("enter"), title}, {L("q"), L("quit")}, {Key: L("x")}, {}}
				checkRow(t, "KeyLine", KeyLine(hints, title, w), w)
				checkRow(t, "KeyLine/noright", KeyLine(hints, sanitize.Program(""), w), w)
				checkRow(t, "KeyLine/nohints", KeyLine(nil, title, w), w)
			}
		}
	})
}

func checkRow(t *testing.T, what, s string, w int) {
	t.Helper()
	if w < 1 {
		if s != "" {
			t.Fatalf("%s: width %d returned %q", what, w, s)
		}
		return
	}
	exactWidth(t, fmt.Sprintf("%s w=%d", what, w), s, w)
}

func TestPrimitivesHostileTitle(t *testing.T) {
	eachProfile(t, func(t *testing.T) {
		rows := Panel(PanelSpec{Num: 1, Title: hostileText, Counter: counterOf("1!"), Body: []string{"x"}, Width: 40, Height: 5, Focused: true})
		for _, r := range rows {
			exactWidth(t, "panel", r, 40)
		}
		if strings.Contains(strings.Join(rows, ""), "\x1b]") {
			t.Fatal("OSC survived")
		}
		row := ListRow(RowSpec{Name: hostileText, Value: hostileText}, 40)
		exactWidth(t, "row", row, 40)
		exactWidth(t, "label", SectionLabel(hostileText, 40), 40)
		exactWidth(t, "key", KeyLine([]KeyHint{{Key: hostileText, Desc: hostileText}}, hostileText, 40), 40)
		exactWidth(t, "header", HeaderLine([]Span{{Text: hostileText}}, []Span{{Text: hostileText}}, 40), 40)
	})
}

func plain(rows []string) string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = Strip(r)
	}
	return strings.Join(out, "\n")
}

func TestPanelLayoutColorless(t *testing.T) {
	useProfile(t, primProfiles[3], false)
	counter := Chrome("6✓ 1! 2·")
	body := []string{"one", "two"}

	got := plain(Panel(PanelSpec{Num: 2, Title: L("Checks"), Counter: counter, Body: body, Width: 40, Height: 5}))
	want := "" +
		"╭[2] Checks ──────────────── 6✓ 1! 2· ─╮\n" +
		"│ one                                  │\n" +
		"│ two                                  │\n" +
		"│                                      │\n" +
		"╰──────────────────────────────────────╯"
	if got != want {
		t.Fatalf("unfocused:\n%s\nwant:\n%s", got, want)
	}

	got = plain(Panel(PanelSpec{Num: 2, Title: L("Checks"), Counter: counter, Width: 40, Height: 2, Focused: true}))
	want = "" +
		"╭▶[2] Checks ─────────────── 6✓ 1! 2· ─╮\n" +
		"╰──────────────────────────────────────╯"
	if got != want {
		t.Fatalf("focused:\n%s\nwant:\n%s", got, want)
	}

	// The spec example, byte for byte.
	got = plain(Panel(PanelSpec{Num: 2, Title: L("Checks"), Counter: counter, Width: 31, Height: 1}))
	if want := "╭[2] Checks ─────── 6✓ 1! 2· ─╮"; got != want {
		t.Fatalf("spec example:\n%s\nwant:\n%s", got, want)
	}

	got = plain(Panel(PanelSpec{Num: -1, Title: L("Config"), Width: 20, Height: 1}))
	if want := "╭Config ───────────╮"; got != want {
		t.Fatalf("no number, no counter: %q want %q", got, want)
	}
}

func TestPanelTitleElidesBeforeCounterDrops(t *testing.T) {
	useProfile(t, primProfiles[3], false)
	counter := Chrome("6✓ 1! 2·")
	title := L("Checks and balances")
	top := func(w int) string {
		return Strip(Panel(PanelSpec{Num: 2, Title: title, Counter: counter, Width: w, Height: 1})[0])
	}
	if got := top(40); !strings.Contains(got, "Checks and…") && !strings.Contains(got, "Checks and balances") {
		t.Fatalf("w=40: %q", got)
	}
	// Narrow enough to force title elision with the counter kept.
	got := top(30)
	if !strings.Contains(got, "6✓ 1! 2·") || !strings.Contains(got, "…") {
		t.Fatalf("title should elide first at w=30: %q", got)
	}
	// Narrower still: the counter goes, the title keeps its room.
	got = top(20)
	if strings.Contains(got, "6✓") || !strings.Contains(got, "[2] Checks") {
		t.Fatalf("counter should drop at w=20: %q", got)
	}
	if got := top(5); Width(got) != 5 {
		t.Fatalf("w=5: %q", got)
	}
}

func TestPanelASCIIGlyphs(t *testing.T) {
	useProfile(t, primProfiles[0], true)
	rows := Panel(PanelSpec{Num: 1, Title: L("Model"), Counter: Chrome("3"), Body: []string{"x"}, Width: 24, Height: 4})
	for _, r := range rows {
		for _, c := range Strip(r) {
			if c >= 0x80 {
				t.Fatalf("non-ASCII %q in %q", c, r)
			}
		}
	}
	if got := Strip(rows[0]); !strings.HasPrefix(got, "+[1] Model ") || !strings.HasSuffix(got, " 3 -+") {
		t.Fatalf("ascii top %q", got)
	}
	if got := Strip(rows[3]); got != "+----------------------+" {
		t.Fatalf("ascii bottom %q", got)
	}
}

func TestListRowColorless(t *testing.T) {
	useProfile(t, primProfiles[3], false)
	rows := []string{
		ListRow(RowSpec{Glyph: L(G().Pass), Name: L("header"), Value: L("1 ms")}, 30),
		ListRow(RowSpec{Glyph: L("!"), Tone: ToneWarn, Name: L("offsets and layout"), Value: L("12 ms"), Selected: true}, 30),
		ListRow(RowSpec{Glyph: L(G().Fail), Tone: ToneFail, Name: L("a name that is far too long to fit"), Value: L("3 ms"), Indent: 2}, 30),
	}
	want := "" +
		"  ✓ header                1 ms\n" +
		"▶ ! offsets and layout   12 ms\n" +
		"    ✗ a name that is far… 3 ms"
	if got := strings.Join(rows, "\n"); got != want {
		t.Fatalf("rows:\n%s\nwant:\n%s", got, want)
	}
}

func TestListRowValueKeepsNameAlive(t *testing.T) {
	useProfile(t, primProfiles[3], false)
	long := sanitize.From(strings.Repeat("v", 300))
	got := Strip(ListRow(RowSpec{Name: L("name"), Value: long}, 20))
	if !strings.Contains(got, "name") || Width(got) != 20 {
		t.Fatalf("name lost: %q", got)
	}
}

func TestListRowSelectionFill(t *testing.T) {
	for _, p := range primProfiles[:2] {
		t.Run(p.name, func(t *testing.T) {
			useProfile(t, p, false)
			sel := ListRow(RowSpec{Name: L("n"), Value: L("v"), Selected: true}, 20)
			unsel := ListRow(RowSpec{Name: L("n"), Value: L("v")}, 20)
			if !strings.Contains(sel, "\x1b[") || strings.Contains(unsel, "48;") || strings.Contains(unsel, "48:") {
				t.Fatalf("fill: sel %q unsel %q", sel, unsel)
			}
			if !strings.Contains(sel, "48;") {
				t.Fatalf("selected row has no background: %q", sel)
			}
			// The gap carries the fill, so no unstyled space survives in the row.
			for _, seg := range strings.Split(sel, "\x1b[m") {
				if strings.Contains(seg, " ") && !strings.Contains(seg, "\x1b[") {
					t.Fatalf("unfilled gap %q in %q", seg, sel)
				}
			}
		})
	}
	t.Run("ansi16 marks with a gutter", func(t *testing.T) {
		useProfile(t, primProfiles[2], false)
		sel := Strip(ListRow(RowSpec{Name: L("n"), Selected: true}, 10))
		unsel := Strip(ListRow(RowSpec{Name: L("n")}, 10))
		if !strings.HasPrefix(sel, "▶") || !strings.HasPrefix(unsel, " ") {
			t.Fatalf("gutter: %q %q", sel, unsel)
		}
	})
}

func TestKeyLineDropsTrailingHints(t *testing.T) {
	useProfile(t, primProfiles[3], false)
	hints := []KeyHint{{L("↑↓"), L("move")}, {L("/"), L("filter")}, {L("]"), L("next problem")}, {L("q"), L("quit")}}
	right := L("org/repo · a1b2c3d")
	got := Strip(KeyLine(hints, right, 100))
	leftS := "↑↓ move · / filter · ] next problem · q quit"
	want := leftS + strings.Repeat(" ", 100-Width(leftS)-Width("org/repo · a1b2c3d")) + "org/repo · a1b2c3d"
	if got != want {
		t.Fatalf("100 cols:\n%q\nwant\n%q", got, want)
	}
	got = Strip(KeyLine(hints, right, 50))
	if !strings.HasPrefix(got, "↑↓ move · / filter") || strings.Contains(got, "quit") || !strings.HasSuffix(got, "org/repo · a1b2c3d") {
		t.Fatalf("50 cols: %q", got)
	}
	// A hint is dropped whole, never cut.
	for w := 1; w <= 60; w++ {
		g := Strip(KeyLine(hints, L(""), w))
		for _, frag := range []string{"mov", "filte", "nex", "qui"} {
			if strings.Contains(g, frag) && !strings.Contains(g, map[string]string{"mov": "move", "filte": "filter", "nex": "next problem", "qui": "quit"}[frag]) {
				t.Fatalf("w=%d cut a hint: %q", w, g)
			}
		}
	}
}

func TestHeaderLineElidesLeftFirst(t *testing.T) {
	useProfile(t, primProfiles[3], false)
	left := []Span{{Text: L("model-inspector")}, {Text: L("Qwen3-Coder-480B-A35B-Instruct")}, {Text: L("894 GiB")}}
	right := []Span{{Text: L("! 1 warn")}, {Text: L("headers-only")}}
	got := Strip(HeaderLine(left, right, 100))
	leftS := "model-inspector · Qwen3-Coder-480B-A35B-Instruct · 894 GiB"
	want := leftS + strings.Repeat(" ", 100-Width(leftS)-Width("! 1 warn · headers-only")) + "! 1 warn · headers-only"
	if got != want {
		t.Fatalf("100 cols:\n%q\nwant\n%q", got, want)
	}
	got = Strip(HeaderLine(left, right, 50))
	if !strings.HasSuffix(got, "! 1 warn · headers-only") || !strings.Contains(got, "…") {
		t.Fatalf("50 cols: %q", got)
	}
	got = Strip(HeaderLine(left, right, 24))
	if strings.Contains(got, "model") || !strings.HasSuffix(got, "headers-only") {
		t.Fatalf("24 cols: %q", got)
	}
}

func TestStatusPill(t *testing.T) {
	useProfile(t, primProfiles[3], false)
	for k, want := range map[PillKind]string{PillPass: "✓", PillWarn: "[WARN]", PillFail: "[FAIL]", PillNR: "·"} {
		if got := StatusPill(k); got != want {
			t.Errorf("colorless %d: %q want %q", k, got, want)
		}
	}
	useProfile(t, primProfiles[0], true)
	for k, want := range map[PillKind]string{PillPass: "+", PillWarn: " WARN ", PillFail: " FAIL ", PillNR: "-"} {
		if got := Strip(StatusPill(k)); got != want {
			t.Errorf("ascii %d: %q want %q", k, got, want)
		}
	}
	useProfile(t, primProfiles[0], false)
	if w, f := StatusPill(PillWarn), StatusPill(PillFail); !strings.Contains(w, "\x1b[") || !strings.Contains(f, "\x1b[") {
		t.Fatalf("warn/fail unstyled: %q %q", w, f)
	}
	if w, f := StatusPill(PillWarn), StatusPill(PillFail); w != PaintToken(" WARN ", WarnTok, &WarnBG, true) || f != PaintToken(" FAIL ", FailTok, &FailBG, true) {
		t.Fatalf("warn/fail fills are not warn-bg/fail-bg: %q %q", w, f)
	}
	for _, k := range []PillKind{PillPass, PillNR} {
		if strings.Contains(StatusPill(k), "48;") {
			t.Errorf("pill %d has a fill", k)
		}
	}
}

func TestPanelFocusStyling(t *testing.T) {
	useProfile(t, primProfiles[0], false)
	f := Panel(PanelSpec{Num: 1, Title: L("Model"), Width: 20, Height: 3, Focused: true})
	u := Panel(PanelSpec{Num: 1, Title: L("Model"), Width: 20, Height: 3})
	if f[0] == u[0] || f[1] == u[1] {
		t.Fatal("focus does not change the border")
	}
	if Strip(f[0]) != Strip(u[0]) {
		t.Fatalf("truecolor focus changed glyphs: %q vs %q", Strip(f[0]), Strip(u[0]))
	}
}

func TestPrimitivesDoNotPanicOnOddInput(t *testing.T) {
	useProfile(t, primProfiles[0], false)
	if Panel(PanelSpec{Width: 0, Height: 5}) != nil || Panel(PanelSpec{Width: 5, Height: 0}) != nil {
		t.Fatal("empty size should return nil")
	}
	_ = Panel(PanelSpec{Num: 1 << 40, Title: hostileText, Width: 7, Height: 2, Body: nil})
	_ = ListRow(RowSpec{Indent: -5, Name: hostileText}, 3)
	_ = KeyLine([]KeyHint{{}}, sanitize.Text{}, 1)
}

func panelBody(n, w int) []string {
	rows := make([]string, n)
	for i := range rows {
		spec := RowSpec{Glyph: L(G().Pass), Name: L("a layer or check name"), Value: L("12 ms"), Selected: i == 3}
		if i%7 == 0 {
			spec.Glyph, spec.Tone = L("!"), ToneWarn
		}
		rows[i] = ListRow(spec, w)
	}
	return rows
}

func benchProfile(b *testing.B) {
	useProfile(b, primProfiles[0], false)
}

func BenchmarkListRow(b *testing.B) {
	benchProfile(b)
	spec := RowSpec{Glyph: L("!"), Tone: ToneWarn, Name: L("offsets and layout"), Value: L("12 ms"), Selected: true}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = ListRow(spec, 44)
	}
}

func BenchmarkPanel140x40(b *testing.B) {
	benchProfile(b)
	spec := PanelSpec{Num: 2, Title: L("Checks"), Counter: Chrome("6✓ 1! 2·"), Body: panelBody(38, 44), Width: 48, Height: 40, Focused: true}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Panel(spec)
	}
}

// BenchmarkPanelRows40 measures the whole 40-row cost: building every
// ListRow and drawing the panel, which is what a screen redraw pays.
func BenchmarkPanelRows40(b *testing.B) {
	benchProfile(b)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Panel(PanelSpec{Num: 2, Title: L("Checks"), Counter: Chrome("6✓ 1! 2·"), Body: panelBody(38, 44), Width: 48, Height: 40, Focused: true})
	}
}
