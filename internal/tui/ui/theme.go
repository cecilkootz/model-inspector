package ui

import (
	"math"
	"os"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"origin.cursor.com/cecilkootz/model-inspector/internal/sanitize"
)

// Token is one semantic colour with true-colour, ANSI-256, and ANSI-16 values.
type Token struct {
	DarkHex  string
	LightHex string
	ANSI256  string
	ANSI     string // empty means "no fill" in 16-colour mode
}

// Color is the lipgloss adaptive colour for this token.
func (t Token) Color() lipgloss.TerminalColor {
	ansi := t.ANSI
	if ansi == "" {
		ansi = "7"
	}
	light := t.LightHex
	if light == "" {
		light = t.DarkHex
	}
	return lipgloss.CompleteAdaptiveColor{
		Dark: lipgloss.CompleteColor{
			TrueColor: t.DarkHex,
			ANSI256:   t.ANSI256,
			ANSI:      ansi,
		},
		Light: lipgloss.CompleteColor{
			TrueColor: light,
			ANSI256:   t.ANSI256,
			ANSI:      ansi,
		},
	}
}

// Hex is the true-colour value for the active theme.
func (t Token) Hex() string {
	if !Dark() && t.LightHex != "" {
		return t.LightHex
	}
	return t.DarkHex
}

// Surfaces, text, and fills. Names match the v2 token tables.
var (
	SurfaceCard = Token{DarkHex: "#1A1C29", LightHex: "#F1F2F7", ANSI256: "234", ANSI: ""}
	SurfaceSeg  = Token{DarkHex: "#2D3142", LightHex: "#E1E3EE", ANSI256: "236", ANSI: "0"}
	SurfaceSeg2 = Token{DarkHex: "#3A3F55", LightHex: "#D3D6E4", ANSI256: "238", ANSI: "8"}
	SurfaceBar  = Token{DarkHex: "#151722", LightHex: "#ECEDF3", ANSI256: "234", ANSI: ""}
	SurfaceSel  = Token{DarkHex: "#33295E", LightHex: "#E4DEFF", ANSI256: "237", ANSI: ""}
	BorderModal = Token{DarkHex: "#A594FF", LightHex: "#5B47D6", ANSI256: "141", ANSI: "13"}
	RuleTab     = Token{DarkHex: "#6B48F0", LightHex: "#6B48F0", ANSI256: "62", ANSI: "5"}
	RuleDim     = Token{DarkHex: "#3D4259", LightHex: "#B8BCCC", ANSI256: "238", ANSI: "8"}

	TextPrimary   = Token{DarkHex: "#ECEEF5", LightHex: "#1B1E2B", ANSI256: "255", ANSI: "15"}
	TextSecondary = Token{DarkHex: "#B8BDD0", LightHex: "#454B63", ANSI256: "250", ANSI: "7"}
	TextMuted     = Token{DarkHex: "#9AA0B8", LightHex: "#5F6580", ANSI256: "247", ANSI: "8"}
	TextLink      = Token{DarkHex: "#5BD68A", LightHex: "#1F7A44", ANSI256: "78", ANSI: "10"}
	TextOnFill    = Token{DarkHex: "#FFFFFF", LightHex: "#FFFFFF", ANSI256: "231", ANSI: "15"}
	TextInk       = Token{DarkHex: "#0E0F16", LightHex: "#0E0F16", ANSI256: "233", ANSI: "0"}
	Accent        = Token{DarkHex: "#A594FF", LightHex: "#5B47D6", ANSI256: "141", ANSI: "13"}
	Data          = Token{DarkHex: "#5FD7D7", LightHex: "#00727A", ANSI256: "80", ANSI: "14"}
	OK            = Token{DarkHex: "#5BD68A", LightHex: "#1F7A44", ANSI256: "78", ANSI: "10"}
	Warn          = Token{DarkHex: "#F5B94A", LightHex: "#8A5A00", ANSI256: "215", ANSI: "11"}
	Fail          = Token{DarkHex: "#FF6B6B", LightHex: "#C0272D", ANSI256: "203", ANSI: "9"}
	Host          = Token{DarkHex: "#4FC1E9", LightHex: "#0B6E99", ANSI256: "74", ANSI: "12"}

	FillViolet = Token{DarkHex: "#6B48F0", LightHex: "#6B48F0", ANSI256: "62", ANSI: "5"}
	FillPink   = Token{DarkHex: "#F25D94", LightHex: "#F25D94", ANSI256: "204", ANSI: "13"}
	FillOK     = Token{DarkHex: "#04B575", LightHex: "#04B575", ANSI256: "36", ANSI: "2"}
	FillWarn   = Token{DarkHex: "#F5B94A", LightHex: "#F5B94A", ANSI256: "215", ANSI: "3"}
	FillFail   = Token{DarkHex: "#E5484D", LightHex: "#E5484D", ANSI256: "167", ANSI: "1"}
	FillHost   = Token{DarkHex: "#22B8CF", LightHex: "#22B8CF", ANSI256: "38", ANSI: "6"}
	FillInfo   = Token{DarkHex: "#89B4FA", LightHex: "#89B4FA", ANSI256: "111", ANSI: "4"}

	DTypeQ    = Token{DarkHex: "#A594FF", LightHex: "#A594FF", ANSI256: "141", ANSI: "13"}
	DTypeBF16 = Token{DarkHex: "#F5B94A", LightHex: "#F5B94A", ANSI256: "215", ANSI: "11"}
	DTypeF32  = Token{DarkHex: "#FF9E64", LightHex: "#FF9E64", ANSI256: "209", ANSI: "3"}
	DTypeInt  = Token{DarkHex: "#8A90A6", LightHex: "#8A90A6", ANSI256: "245", ANSI: "8"}

	TermBG = "#11131A"
	InkHex = "#0E0F16"
	White  = "#FFFFFF"
)

// FillKind selects a pill fill.
type FillKind int

const (
	FillKindViolet FillKind = iota
	FillKindPink
	FillKindOK
	FillKindWarn
	FillKindFail
	FillKindHost
	FillKindInfo
	FillKindNR
)

// FillColors returns the background token and the text token that sits on it.
func FillColors(k FillKind) (bg, fg Token) {
	switch k {
	case FillKindViolet:
		return FillViolet, TextOnFill
	case FillKindPink:
		return FillPink, TextInk
	case FillKindOK:
		return FillOK, TextInk
	case FillKindWarn:
		return FillWarn, TextInk
	case FillKindFail:
		return FillFail, TextInk
	case FillKindHost:
		return FillHost, TextInk
	case FillKindInfo:
		return FillInfo, TextInk
	default:
		return SurfaceSeg, TextSecondary
	}
}

var (
	noColor  bool
	litMu    sync.Mutex
	litCache = map[string]sanitize.Text{}
)

// SetNoColor records --no-color. Call it before the program starts.
func SetNoColor(v bool) { noColor = v }

// Configure applies NO_COLOR, the theme override, and the colour profile.
func Configure() {
	if noColor || os.Getenv("NO_COLOR") != "" {
		lipgloss.SetColorProfile(termenv.Ascii)
	}
	switch strings.ToLower(os.Getenv("MODEL_INSPECTOR_THEME")) {
	case "dark":
		lipgloss.SetHasDarkBackground(true)
	case "light":
		lipgloss.SetHasDarkBackground(false)
	}
}

// ColorOff reports colorless output: NO_COLOR, --no-color, or an ASCII profile.
func ColorOff() bool {
	return noColor || os.Getenv("NO_COLOR") != "" || lipgloss.ColorProfile() == termenv.Ascii
}

// Dark reports whether the active theme is the dark column.
func Dark() bool {
	if strings.EqualFold(os.Getenv("MODEL_INSPECTOR_THEME"), "light") {
		return false
	}
	if strings.EqualFold(os.Getenv("MODEL_INSPECTOR_THEME"), "dark") {
		return true
	}
	return lipgloss.HasDarkBackground()
}

// UTF8 reports whether the locale wants Unicode glyphs.
// An explicit locale wins. TERM=dumb forces ASCII only when the locale
// does not say UTF-8, so a C.UTF-8 test still gets the host glyph.
func UTF8() bool {
	loc := os.Getenv("LC_ALL")
	if loc == "" {
		loc = os.Getenv("LANG")
	}
	if loc != "" {
		u := strings.ToUpper(loc)
		return strings.Contains(u, "UTF-8") || strings.Contains(u, "UTF8")
	}
	return os.Getenv("TERM") != "dumb"
}

// G returns the glyph set for the current locale.
func G() Glyphs { return GlyphsFor(UTF8()) }

// Profile is truecolor, ansi256, ansi16, or colorless.
func Profile() string {
	if ColorOff() {
		return "colorless"
	}
	switch lipgloss.ColorProfile() {
	case termenv.TrueColor:
		return "truecolor"
	case termenv.ANSI256:
		return "ansi256"
	case termenv.ANSI:
		return "ansi16"
	default:
		return "colorless"
	}
}

// L wraps a program literal. Callers must pass a string constant.
func L(s string) sanitize.Text {
	litMu.Lock()
	defer litMu.Unlock()
	if t, ok := litCache[s]; ok {
		return t
	}
	t := sanitize.From(s)
	litCache[s] = t
	return t
}

// OnFill returns white or ink, whichever has the higher contrast on bgHex.
func OnFill(bgHex string) string {
	if Contrast(White, bgHex) >= Contrast(InkHex, bgHex) {
		return White
	}
	return InkHex
}

// Contrast is the WCAG 2.x contrast ratio of two #RRGGBB colours.
func Contrast(a, b string) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func luminance(hex string) float64 {
	r, g, b := parseHex(hex)
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
}

func lin(c float64) float64 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

func parseHex(h string) (r, g, b float64) {
	h = strings.TrimPrefix(h, "#")
	if len(h) != 6 {
		return 0, 0, 0
	}
	var n uint32
	for i := 0; i < 6; i++ {
		n <<= 4
		c := h[i]
		switch {
		case c >= '0' && c <= '9':
			n |= uint32(c - '0')
		case c >= 'a' && c <= 'f':
			n |= uint32(c-'a') + 10
		case c >= 'A' && c <= 'F':
			n |= uint32(c-'A') + 10
		}
	}
	return float64(n>>16) / 255, float64((n>>8)&0xff) / 255, float64(n&0xff) / 255
}
