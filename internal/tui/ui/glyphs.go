package ui

// Glyphs is the character set for the current locale.
// The ASCII set is entirely below U+0080.
type Glyphs struct {
	Pass, Fail, NR         string
	Warn, Size, Host       string
	Selected, Ellipsis     string
	Rule, RuleOn, Edge     string
	Sep, Dot, Linear, Full string
	Heat                   [4]string
	Bar                    [4]string
	Eighth                 [8]string
	TL, TR, BL, BR, H, V   string
	Arrow                  string
}

// ASCII reports whether g contains only bytes below U+0080.
func (g Glyphs) ASCII() bool {
	for _, s := range []string{
		g.Pass, g.Fail, g.NR, g.Warn, g.Size, g.Host, g.Selected, g.Ellipsis,
		g.Rule, g.RuleOn, g.Edge, g.Sep, g.Dot, g.Linear, g.Full, g.Arrow,
		g.TL, g.TR, g.BL, g.BR, g.H, g.V,
	} {
		for i := 0; i < len(s); i++ {
			if s[i] >= 0x80 {
				return false
			}
		}
	}
	for _, s := range g.Heat {
		for i := 0; i < len(s); i++ {
			if s[i] >= 0x80 {
				return false
			}
		}
	}
	for _, s := range g.Bar {
		for i := 0; i < len(s); i++ {
			if s[i] >= 0x80 {
				return false
			}
		}
	}
	return true
}

func utfGlyphs() Glyphs {
	return Glyphs{
		Pass: "✓", Fail: "✗", NR: "–",
		Warn: "!", Size: "▲", Host: "⌂",
		Selected: "▶", Ellipsis: "…",
		Rule: "━", RuleOn: "━", Edge: "▌",
		Sep: "│", Dot: "·", Linear: "○", Full: "●",
		Heat:   [4]string{"░", "▒", "▓", "█"},
		Bar:    [4]string{"█", "▓", "▒", "░"},
		Eighth: [8]string{" ", "▏", "▎", "▍", "▌", "▋", "▊", "█"},
		TL:     "╭", TR: "╮", BL: "╰", BR: "╯", H: "─", V: "│",
		Arrow: "›",
	}
}

func asciiGlyphs() Glyphs {
	return Glyphs{
		Pass: "+", Fail: "x", NR: "-",
		Warn: "!", Size: "^", Host: "(H)",
		Selected: ">", Ellipsis: "...",
		Rule: "=", RuleOn: "^", Edge: "",
		Sep: "|", Dot: " ", Linear: "o", Full: "O",
		Heat:   [4]string{".", ":", "#", "@"},
		Bar:    [4]string{"#", "=", "+", "."},
		Eighth: [8]string{" ", ".", ".", ":", ":", "#", "#", "@"},
		TL:     "+", TR: "+", BL: "+", BR: "+", H: "-", V: "|",
		Arrow: ">",
	}
}

// SepWord joins clauses. ASCII uses a bar so the chrome stays below U+0080.
func SepWord() string {
	if UTF8() {
		return " · "
	}
	return " | "
}

// Dash is an empty-value mark.
func Dash() string {
	if UTF8() {
		return "—"
	}
	return "-"
}

// Geq is the "at least" mark. The UTF-8 form is what the memory line prints.
func Geq() string {
	if UTF8() {
		return "≥"
	}
	return ">="
}

// RArrow is the heuristic arrow.
func RArrow() string {
	if UTF8() {
		return "→"
	}
	return "->"
}

// Times is the multiplication sign in titles. The too-small screen keeps its own ×.
func Times() string {
	if UTF8() {
		return "×"
	}
	return "x"
}

// GlyphsFor returns the UTF-8 or ASCII set.
func GlyphsFor(utf8 bool) Glyphs {
	if utf8 {
		return utfGlyphs()
	}
	return asciiGlyphs()
}
