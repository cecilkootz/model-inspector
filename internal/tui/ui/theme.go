package ui

import (
	"math"
	"os"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/cecilkootz/model-inspector/internal/sanitize"
)

// Token is one semantic colour with true-colour, ANSI-256, and ANSI-16 values.
type Token struct {
	DarkHex  string
	LightHex string
	ANSI256  string
	ANSI     string // empty means "no fill" in 16-colour mode

	// ANSI256Light replaces ANSI256 in the light theme when set.
	ANSI256Light string
}

func (t Token) ansi256Light() string {
	if t.ANSI256Light != "" {
		return t.ANSI256Light
	}
	return t.ANSI256
}

// Color is the lipgloss adaptive colour for this token.
func (t Token) Color() lipgloss.TerminalColor {
	if t.ANSI == "" && lipgloss.ColorProfile() == termenv.ANSI {
		return lipgloss.NoColor{}
	}
	light := t.LightHex
	if light == "" {
		light = t.DarkHex
	}
	return lipgloss.CompleteAdaptiveColor{
		Dark: lipgloss.CompleteColor{
			TrueColor: t.DarkHex,
			ANSI256:   t.ANSI256,
			ANSI:      t.ANSI,
		},
		Light: lipgloss.CompleteColor{
			TrueColor: light,
			ANSI256:   t.ansi256Light(),
			ANSI:      t.ANSI,
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

// The 17-role palette. Green marks focus only; pass is quiet; only warn and
// fail carry status colour; magnitude uses the ramp.
var (
	Surface   = Token{DarkHex: "#14161A", LightHex: "#F2F4EF", ANSI256: "233", ANSI: ""}
	Selection = Token{DarkHex: "#1F2620", LightHex: "#E2EDD6", ANSI256: "235", ANSI: "0"}
	Line      = Token{DarkHex: "#4A505C", LightHex: "#B9C0B2", ANSI256: "239", ANSI: "8"}
	Text1     = Token{DarkHex: "#E8EAED", LightHex: "#15181C", ANSI256: "254", ANSI: "15"}
	Text2     = Token{DarkHex: "#A7ADB7", LightHex: "#4A5260", ANSI256: "248", ANSI: "7"}
	Text3     = Token{DarkHex: "#838B99", LightHex: "#626A76", ANSI256: "245", ANSI256Light: "242", ANSI: "8"}
	AccentTok = Token{DarkHex: "#8FD14F", LightHex: "#3C7711", ANSI256: "113", ANSI: "10"}
	WarnTok   = Token{DarkHex: "#E8AA43", LightHex: "#8A5A00", ANSI256: "179", ANSI: "3"}
	WarnBG    = Token{DarkHex: "#3A2E17", LightHex: "#F6E8CC", ANSI256: "58", ANSI: ""}
	FailTok   = Token{DarkHex: "#F0625F", LightHex: "#B4232A", ANSI256: "203", ANSI: "1"}
	FailBG    = Token{DarkHex: "#3D1F22", LightHex: "#F8DCDC", ANSI256: "52", ANSI: ""}
	Ramp1     = Token{DarkHex: "#1C2616", LightHex: "#E6F2D8", ANSI256: "234", ANSI: ""}
	Ramp2     = Token{DarkHex: "#2C4120", LightHex: "#C6E2A4", ANSI256: "22", ANSI: "2"}
	Ramp3     = Token{DarkHex: "#46682A", LightHex: "#93C463", ANSI256: "64", ANSI: "2"}
	Ramp4     = Token{DarkHex: "#6E9F3C", LightHex: "#5E9A2C", ANSI256: "70", ANSI: "10"}
	Ramp5     = Token{DarkHex: "#A8DC6E", LightHex: "#3C7711", ANSI256: "149", ANSI: "10"}
)

// Legacy names, kept so call sites do not change.
var (
	SurfaceCard = Surface
	SurfaceBar  = Surface
	SurfaceSeg  = Surface
	SurfaceSeg2 = Line
	SurfaceSel  = Selection
	BorderModal = AccentTok
	RuleTab     = AccentTok
	RuleDim     = Line

	TextPrimary   = Text1
	TextSecondary = Text2
	TextMuted     = Text3
	TextLink      = Text2
	TextOnFill    = Token{DarkHex: "#FFFFFF", LightHex: "#FFFFFF", ANSI256: "231", ANSI: "15"}
	TextInk       = Token{DarkHex: "#0E0F16", LightHex: "#0E0F16", ANSI256: "233", ANSI: "0"}
	Accent        = AccentTok
	Data          = Ramp4
	OK            = Text3
	Warn          = WarnTok
	Fail          = FailTok
	Host          = Text2

	FillViolet = AccentTok
	FillPink   = Line
	FillOK     = Line
	FillWarn   = WarnTok
	FillFail   = FailTok
	FillHost   = Line
	FillInfo   = Line

	DTypeQ    = Text2
	DTypeBF16 = Text2
	DTypeF32  = Text2
	DTypeInt  = Text3

	JSONKey    = Text1
	JSONString = Text2
	JSONNumber = Ramp5
	JSONBool   = Ramp5
	JSONNull   = Text3

	TermBG = "#11131A"
	InkHex = "#0E0F16"
	White  = "#FFFFFF"
)

var paletteTokens = []Token{
	Surface, Selection, Line, Text1, Text2, Text3, AccentTok, WarnTok, WarnBG, FailTok, FailBG,
	Ramp1, Ramp2, Ramp3, Ramp4, Ramp5, TextOnFill, TextInk,
}

// ansi256Of maps a palette hex to its authored ANSI-256 index. Hex strings
// that bypass tokens would otherwise be quantized by termenv, which drifts
// neutrals toward lavender.
func ansi256Of(hex string) (string, bool) {
	for _, t := range paletteTokens {
		if strings.EqualFold(hex, t.DarkHex) {
			return t.ANSI256, true
		}
		if strings.EqualFold(hex, t.LightHex) {
			return t.ansi256Light(), true
		}
	}
	return "", false
}

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
// Text is white or ink, whichever contrasts more with the active theme's fill.
func FillColors(k FillKind) (bg, fg Token) {
	switch k {
	case FillKindViolet:
		bg = FillViolet
	case FillKindPink:
		bg = FillPink
	case FillKindOK:
		bg = FillOK
	case FillKindWarn:
		bg = FillWarn
	case FillKindFail:
		bg = FillFail
	case FillKindHost:
		bg = FillHost
	case FillKindInfo:
		bg = FillInfo
	default:
		return SurfaceSeg, TextSecondary
	}
	if OnFill(bg.Hex()) == White {
		return bg, TextOnFill
	}
	return bg, TextInk
}

const litCap = 256

var (
	noColor  bool
	litMu    sync.Mutex
	litCache = map[string]sanitize.Text{}
	litOrder []string
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
// The text is not sanitized. The cache is capped; the oldest entry is dropped when it is full.
func L(s string) sanitize.Text {
	litMu.Lock()
	defer litMu.Unlock()
	if t, ok := litCache[s]; ok {
		return t
	}
	t := sanitize.Program(s)
	if len(litCache) >= litCap {
		drop := litOrder[0]
		litOrder = litOrder[1:]
		delete(litCache, drop)
	}
	litCache[s] = t
	litOrder = append(litOrder, s)
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
