package tui

import (
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/cecilkootz/model-inspector/internal/tui/ui"
)

// glyphs is the character set for the current locale.
type glyphs struct {
	pass, fail, nr        string
	size, precision, note string
	host, warnMark        string
	selected, ellipsis    string
	hbar, vbar            string
	tl, tr, bl, br        string
	bar                   [4]string
}

var (
	g glyphs

	titleStyle     lipgloss.Style
	tabStyle       lipgloss.Style
	activeTabStyle lipgloss.Style
	labelStyle     lipgloss.Style
	valueStyle     lipgloss.Style
	helpStyle      lipgloss.Style
	warnStyle      lipgloss.Style
	passStyle      lipgloss.Style
	failStyle      lipgloss.Style
	nrStyle        lipgloss.Style
	hostStyle      lipgloss.Style

	// noColor forces the ASCII color profile, same as NO_COLOR.
	noColor bool
)

// SetNoColor records --no-color. Call it before New.
func SetNoColor(v bool) {
	noColor = v
	ui.SetNoColor(v)
}

func configure() {
	ui.Configure()
	if utf8Locale() {
		g = glyphs{
			pass: "✓", fail: "✗", nr: "–",
			size: "▲", precision: "◆", note: "●",
			host: "⌂", warnMark: "!",
			selected: "▶", ellipsis: "…",
			hbar: "─", vbar: "│",
			tl: "┌", tr: "┐", bl: "└", br: "┘",
			bar: [4]string{"█", "▓", "▒", "░"},
		}
	} else {
		g = glyphs{
			pass: "+", fail: "x", nr: "-",
			size: "^", precision: "*", note: "o",
			host: "H", warnMark: "!",
			selected: ">", ellipsis: "...",
			hbar: "-", vbar: "|",
			tl: "+", tr: "+", bl: "+", br: "+",
			bar: [4]string{"#", "=", "+", "."},
		}
	}
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Dark: "212", Light: "162"})
	tabStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Dark: "245", Light: "243"})
	activeTabStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Dark: "230", Light: "255"}).Background(lipgloss.AdaptiveColor{Dark: "62", Light: "61"})
	labelStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Dark: "244", Light: "242"})
	valueStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Dark: "252", Light: "235"})
	helpStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Dark: "241", Light: "245"})
	warnStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Dark: "214", Light: "130"})
	passStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Dark: "42", Light: "28"})
	failStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Dark: "196", Light: "160"})
	nrStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Dark: "245", Light: "245"})
	hostStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Dark: "37", Light: "30"})
}

func utf8Locale() bool {
	loc := os.Getenv("LC_ALL")
	if loc == "" {
		loc = os.Getenv("LANG")
	}
	if loc == "" {
		return true
	}
	u := strings.ToUpper(loc)
	return strings.Contains(u, "UTF-8") || strings.Contains(u, "UTF8")
}
