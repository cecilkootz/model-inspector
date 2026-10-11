package ui

import "testing"

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
		{"text-3", Text3, "#737A86", "#6B7380", "243", "8"},
		{"accent", AccentTok, "#8FD14F", "#3F7D12", "113", "10"},
		{"warn", WarnTok, "#E8AA43", "#8A5A00", "179", "3"},
		{"warn-bg", WarnBG, "#3A2E17", "#F6E8CC", "58", ""},
		{"fail", FailTok, "#F0625F", "#B4232A", "203", "1"},
		{"fail-bg", FailBG, "#3D1F22", "#F8DCDC", "52", ""},
		{"ramp-1", Ramp1, "#1C2616", "#E6F2D8", "234", ""},
		{"ramp-2", Ramp2, "#2C4120", "#C6E2A4", "22", "2"},
		{"ramp-3", Ramp3, "#46682A", "#93C463", "64", "2"},
		{"ramp-4", Ramp4, "#6E9F3C", "#5E9A2C", "70", "10"},
		{"ramp-5", Ramp5, "#A8DC6E", "#3F7D12", "149", "10"},
	} {
		if c.tok.DarkHex != c.dark || c.tok.LightHex != c.light || c.tok.ANSI256 != c.c256 || c.tok.ANSI != c.c16 {
			t.Errorf("%s = %+v, want %s %s %s %q", c.name, c.tok, c.dark, c.light, c.c256, c.c16)
		}
	}
}
