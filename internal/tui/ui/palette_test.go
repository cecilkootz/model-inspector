package ui

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestPalettePinned(t *testing.T) {
	for _, c := range []struct {
		name                   string
		tok                    Token
		dark, light, c256, c16 string
	}{
		{"surface", Surface, "#14161A", "#F2F4EF", "233", ""},
		{"selection", Selection, "#1F2620", "#E2EDD6", "235", "0"},
		{"line", Line, "#4A505C", "#B9C0B2", "239", "8"},
		{"text-1", Text1, "#E8EAED", "#15181C", "254", "15"},
		{"text-2", Text2, "#A7ADB7", "#4A5260", "248", "7"},
		{"text-3", Text3, "#838B99", "#626A76", "245", "8"},
		{"accent", AccentTok, "#8FD14F", "#3C7711", "113", "10"},
		{"warn", WarnTok, "#E8AA43", "#8A5A00", "179", "3"},
		{"warn-bg", WarnBG, "#3A2E17", "#F6E8CC", "58", ""},
		{"fail", FailTok, "#F0625F", "#B4232A", "203", "1"},
		{"fail-bg", FailBG, "#3D1F22", "#F8DCDC", "52", ""},
		{"ramp-1", Ramp1, "#1C2616", "#E6F2D8", "234", ""},
		{"ramp-2", Ramp2, "#2C4120", "#C6E2A4", "22", "2"},
		{"ramp-3", Ramp3, "#46682A", "#93C463", "64", "2"},
		{"ramp-4", Ramp4, "#6E9F3C", "#5E9A2C", "70", "10"},
		{"ramp-5", Ramp5, "#A8DC6E", "#3C7711", "149", "10"},
	} {
		if c.tok.DarkHex != c.dark || c.tok.LightHex != c.light || c.tok.ANSI256 != c.c256 || c.tok.ANSI != c.c16 {
			t.Errorf("%s = %+v, want %s %s %s %q", c.name, c.tok, c.dark, c.light, c.c256, c.c16)
		}
	}
	if Text3.ansi256Light() != "242" {
		t.Errorf("text-3 light 256 = %s, want 242", Text3.ansi256Light())
	}
}

func TestANSI16EmptyTokenIsTerminalDefault(t *testing.T) {
	useProfile(t, primProfiles[2], false)
	if _, ok := Surface.Color().(lipgloss.NoColor); !ok {
		t.Fatalf("empty ANSI token in ansi16 = %T, want NoColor", Surface.Color())
	}
	if _, ok := Text3.Color().(lipgloss.NoColor); ok {
		t.Fatal("token with an ANSI value must keep its colour in ansi16")
	}
}

var sgrParam = regexp.MustCompile(`\x1b\[([\d;]*)m`)

// TestANSI16ModalUsesDefaultFill guards the white-on-ansi16 modal: no SGR
// may set a background (40-47, 100-107 or 48).
func TestANSI16ModalUsesDefaultFill(t *testing.T) {
	useProfile(t, primProfiles[2], false)
	out := Modal(ModalSpec{Title: txt("HELP"), Body: txt("keys"), Width: 40}, 60, 16)
	if !strings.Contains(Strip(out), "HELP") {
		t.Fatalf("modal title missing: %q", Strip(out))
	}
	for _, m := range sgrParam.FindAllStringSubmatch(out, -1) {
		for _, p := range strings.Split(m[1], ";") {
			n, err := strconv.Atoi(p)
			if err != nil {
				continue
			}
			if (n >= 40 && n <= 47) || (n >= 100 && n <= 107) || n == 48 {
				t.Fatalf("ansi16 modal sets background %d: %q", n, m[0])
			}
		}
	}
}
