package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/cecilkootz/model-inspector/internal/sanitize"
)

// Tone picks a text token. The zero value is the quiet one so an unset tone
// never draws attention.
type Tone int

const (
	ToneMuted  Tone = iota // text-3: labels, meta, quiet glyphs
	ToneBody               // text-2
	ToneStrong             // text-1: names, values, titles
	ToneAccent             // focus, jump numbers
	ToneWarn
	ToneFail
)

func (t Tone) token() Token {
	switch t {
	case ToneBody:
		return TextSecondary
	case ToneStrong:
		return TextPrimary
	case ToneAccent:
		return Accent
	case ToneWarn:
		return Warn
	case ToneFail:
		return Fail
	default:
		return TextMuted
	}
}

func ink(s string, tok Token, bold bool) string { return PaintToken(s, tok, nil, bold) }

func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(" ", n)
}

// elide cuts plain text to w cells and ends it with ell. A budget narrower
// than ell hard-cuts so the result never overflows.
func elide(s string, w int, ell string) string {
	if w <= 0 {
		return ""
	}
	if Width(s) <= w {
		return s
	}
	if w < Width(ell) {
		return ansi.Truncate(s, w, "")
	}
	return ansi.Truncate(s, w, ell)
}

// closeSGR ends any style a cut may have left open. Colorless text has none
// and stays free of escapes.
func closeSGR(s string) string {
	if strings.Contains(s, "\x1b") {
		return s + "\x1b[m"
	}
	return s
}

// clipPad makes a possibly styled string exactly w cells. Unlike Fit it keeps
// the styling of what survives the cut and closes it with a reset.
func clipPad(s string, w int) string {
	if w <= 0 {
		return ""
	}
	n := Width(s)
	if n > w {
		s = closeSGR(ansi.Truncate(s, w, ""))
		n = Width(s)
	}
	return s + spaces(w-n)
}

// PanelSpec describes one bordered panel. Body rows are pre-rendered, so
// they may carry SGR; Panel only clips and pads them.
type PanelSpec struct {
	Num     int // shown as [n]; negative omits it
	Title   sanitize.Text
	Counter Safe // caller-styled; dropped when it does not fit
	Body    []string
	Width   int
	Height  int
	Focused bool
}

// Panel draws a rounded box with the title in the top border:
//
//	╭[2] Checks ─────── 6✓ 1! 2· ─╮
//
// It returns exactly Height rows of exactly Width cells. When the top border
// is too narrow the title is elided first and then the counter is dropped, so
// a counter never displaces the title entirely. Colorless output keeps the
// box glyphs and marks focus with the selection glyph before the title, since
// the accent is not available.
func Panel(p PanelSpec) []string {
	w, h := p.Width, p.Height
	if w < 1 || h < 1 {
		return nil
	}
	gl := G()
	edge := func(s string) string {
		if p.Focused {
			return ink(s, Accent, true)
		}
		return ink(s, RuleDim, false)
	}
	out := make([]string, 0, h)
	if w == 1 {
		for i := 0; i < h; i++ {
			out = append(out, edge(gl.V))
		}
		return out
	}
	out = append(out, panelTop(p, w, gl, edge))
	if h == 1 {
		return out
	}
	side := edge(gl.V)
	inner := w - 4
	for i := 0; i < h-2; i++ {
		var row string
		switch {
		case inner < 1:
			row = side + spaces(w-2) + side
		case i < len(p.Body):
			row = side + " " + clipPad(p.Body[i], inner) + " " + side
		default:
			row = side + spaces(w-2) + side
		}
		out = append(out, row)
	}
	out = append(out, edge(gl.BL+strings.Repeat(gl.H, w-2)+gl.BR))
	return out
}

func panelTop(p PanelSpec, w int, gl Glyphs, edge func(string) string) string {
	inner := w - 2
	if inner < 1 {
		return edge(gl.TL + gl.TR)
	}
	title := p.Title.String()
	counter := p.Counter.String()
	cw := Width(counter)
	num := ""
	if p.Num >= 0 {
		num = "[" + itoa(p.Num) + "]"
	}
	mark := ""
	if p.Focused && ColorOff() {
		mark = gl.Selected
	}
	minTitle := Width(title)
	if m := Width(gl.Ellipsis) + 1; minTitle > m {
		minTitle = m
	}

	type cand struct {
		pre      string
		counter  bool
		minTitle int
	}
	cands := []cand{
		{mark + num, cw > 0, minTitle},
		{mark + num, false, 1},
		{mark, false, 1},
		{"", false, 1},
	}
	for _, c := range cands {
		extra := 0
		if c.counter {
			extra = cw + 3
		}
		pw := Width(c.pre)
		room := inner - 1 - extra
		if pw > 0 {
			room -= pw + 1
		}
		head := ""
		headW := 0
		if title != "" {
			room-- // space after the title
			if room < c.minTitle {
				continue
			}
			t := elide(title, room, gl.Ellipsis)
			headW = Width(t) + 1
			if pw > 0 {
				headW += pw + 1
			}
			head = styleHead(c.pre, t, p.Focused)
		} else {
			if room < 0 {
				continue
			}
			if pw > 0 {
				headW = pw + 1
				head = styleHead(c.pre, "", p.Focused)
			}
		}
		fill := inner - headW - extra
		if fill < 1 {
			continue
		}
		var b strings.Builder
		b.WriteString(edge(gl.TL))
		b.WriteString(head)
		b.WriteString(edge(strings.Repeat(gl.H, fill)))
		if c.counter {
			b.WriteString(" " + counter + " " + edge(gl.H))
		}
		b.WriteString(edge(gl.TR))
		return clipPad(b.String(), w)
	}
	return edge(gl.TL + strings.Repeat(gl.H, inner) + gl.TR)
}

// styleHead renders the mark, number and title plus the space that follows
// the last of them. Focused text is accent bold; unfocused is text-3 for the
// number and text-2 for the title.
func styleHead(pre, title string, focused bool) string {
	var b strings.Builder
	if pre != "" {
		numTok, bold := TextMuted, false
		if focused {
			numTok, bold = Accent, true
		}
		b.WriteString(ink(pre, numTok, bold))
		if title != "" {
			b.WriteByte(' ')
		}
	}
	if title != "" {
		if focused {
			b.WriteString(ink(title, Accent, true))
		} else {
			b.WriteString(ink(title, TextSecondary, false))
		}
	}
	b.WriteByte(' ')
	return b.String()
}

// RowSpec is one list row: glyph, name, right-aligned value.
type RowSpec struct {
	Glyph    sanitize.Text // program glyph, such as G().Pass; empty for none
	Tone     Tone          // glyph tone
	Name     sanitize.Text
	Value    sanitize.Text
	Indent   int // cells before the glyph, for tree rows
	Selected bool
}

// ListRow draws `glyph name … value` in exactly width cells. The name is
// elided before the value is; the value is capped at half the row so a long
// one cannot erase the name. Selection fills the whole row with the selection
// token. Colorless and ANSI-16 output cannot show a fill, so every row there
// reserves a two-cell gutter that holds the selection glyph.
func ListRow(r RowSpec, width int) string {
	if width < 1 {
		return ""
	}
	gl := G()
	noFill := ColorOff() || Profile() == "ansi16"
	var bg *Token
	if r.Selected && !noFill {
		bg = &SurfaceSel
	}
	var b strings.Builder
	used := 0
	put := func(s string, tok Token, bold bool) {
		if s == "" {
			return
		}
		b.WriteString(PaintToken(s, tok, bg, bold))
		used += Width(s)
	}

	if noFill {
		if r.Selected {
			put(gl.Selected+" ", Accent, true)
		} else {
			put("  ", TextMuted, false)
		}
	}
	if ind := min(max(r.Indent, 0), width); ind > 0 {
		put(spaces(ind), TextMuted, false)
	}
	if g := r.Glyph.String(); g != "" {
		put(g, r.Tone.token(), false)
		put(" ", TextMuted, false)
	}

	value := r.Value.String()
	if vmax := width / 2; Width(value) > vmax {
		value = elide(value, vmax, gl.Ellipsis)
	}
	nameRoom := width - used
	vw := Width(value)
	if vw > 0 {
		nameRoom -= vw + 1
		if nameRoom < 1 {
			value, vw = "", 0
			nameRoom = width - used
		}
	}
	name := elide(r.Name.String(), nameRoom, gl.Ellipsis)
	put(name, TextPrimary, r.Selected)
	if vw > 0 {
		put(spaces(width-used-vw), TextMuted, false)
		put(value, TextMuted, false)
	}
	put(spaces(width-used), TextMuted, false)
	return clipPad(b.String(), width)
}

// SectionLabel is an uppercase muted heading for a detail pane, padded to
// width so stacked rows stay rectangular.
func SectionLabel(label sanitize.Text, width int) string {
	if width < 1 {
		return ""
	}
	s := elide(strings.ToUpper(label.String()), width, G().Ellipsis)
	return ink(s, TextMuted, true) + spaces(width-Width(s))
}

// Span is one run of header text.
type Span struct {
	Text sanitize.Text
	Tone Tone
	Bold bool
}

func joinSpans(spans []Span) string {
	var b strings.Builder
	sep := ""
	for _, s := range spans {
		t := s.Text.String()
		if t == "" {
			continue
		}
		b.WriteString(sep)
		sep = ink(SepWord(), TextMuted, false)
		b.WriteString(ink(t, s.Tone.token(), s.Bold))
	}
	return b.String()
}

// HeaderLine lays out one row: left spans, a flexible gap, right spans. Spans
// within a group are joined by the separator word. A left group that does not
// fit is elided first and dropped before the right group is touched.
func HeaderLine(left, right []Span, width int) string {
	if width < 1 {
		return ""
	}
	ell := G().Ellipsis
	l, r := joinSpans(left), joinSpans(right)
	rw := Width(r)
	if rw > width {
		return clipPad(closeSGR(ansi.Truncate(r, width, ell)), width)
	}
	room := width - rw
	if rw > 0 {
		room--
	}
	lw := Width(l)
	switch {
	case room < 1:
		l, lw = "", 0
	case lw > room:
		l = closeSGR(ansi.Truncate(l, room, ell))
		lw = Width(l)
	}
	return clipPad(l+spaces(width-lw-rw)+r, width)
}

// KeyHint is one key and what it does.
type KeyHint struct {
	Key  sanitize.Text
	Desc sanitize.Text
}

// KeyLine draws `key desc · key desc` on the left and right text at the edge.
// Trailing hints that do not fit are dropped whole, never wrapped, and the
// right text is capped at half the row while any hint remains.
func KeyLine(hints []KeyHint, right sanitize.Text, width int) string {
	if width < 1 {
		return ""
	}
	ell := G().Ellipsis
	rt := right.String()
	rmax := width
	if len(hints) > 0 {
		rmax = width / 2
	}
	if Width(rt) > rmax {
		rt = MiddleTrunc(rt, rmax, ell)
	}
	rw := Width(rt)
	room := width - rw
	if rw > 0 {
		room--
	}
	sep := SepWord()
	var b strings.Builder
	used := 0
	for _, h := range hints {
		k, d := h.Key.String(), h.Desc.String()
		if k == "" && d == "" {
			continue
		}
		cell := ink(k, TextPrimary, false)
		cw := Width(k)
		if d != "" {
			if k != "" {
				cell += " "
				cw++
			}
			cell += ink(d, TextMuted, false)
			cw += Width(d)
		}
		next := cw
		if used > 0 {
			next += Width(sep)
		}
		if used+next > room {
			break
		}
		if used > 0 {
			b.WriteString(ink(sep, TextMuted, false))
		}
		b.WriteString(cell)
		used += next
	}
	line := b.String() + spaces(width-used-rw)
	if rw > 0 {
		line += ink(rt, TextMuted, false)
	}
	return clipPad(line, width)
}

// PillKind is a check status as the lists and detail panes show it.
type PillKind int

const (
	PillPass PillKind = iota
	PillWarn
	PillFail
	PillNR
)

// StatusPill is the status mark. Only warn and fail get a fill; pass and
// not-run stay quiet text-3 glyphs so problems are the only loud thing on
// screen.
func StatusPill(k PillKind) string {
	gl := G()
	switch k {
	case PillWarn:
		return statusFill("WARN", WarnTok, WarnBG)
	case PillFail:
		return statusFill("FAIL", FailTok, FailBG)
	case PillNR:
		g := gl.NR
		if UTF8() {
			g = "·"
		}
		return ink(g, TextMuted, false)
	default:
		return ink(gl.Pass, TextMuted, false)
	}
}

func statusFill(label string, fg, bg Token) string {
	if ColorOff() {
		return "[" + label + "]"
	}
	return PaintToken(" "+label+" ", fg, &bg, true)
}
