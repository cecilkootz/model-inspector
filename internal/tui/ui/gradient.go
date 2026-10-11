package ui

import (
	"math"

	"github.com/charmbracelet/lipgloss"
	"github.com/lucasb-eyer/go-colorful"
)

// Stop is one position on a gradient, in [0,1].
type Stop struct {
	Pos float64
	Hex string
}

// Gradient is a 256-entry OKLab ramp built once.
type Gradient struct {
	lut [256]lipgloss.Color
	ok  bool
}

// NewGradient builds a piecewise OKLab ramp. Endpoints match the stop hexes.
func NewGradient(stops ...Stop) Gradient {
	var g Gradient
	if len(stops) == 0 {
		return g
	}
	cols := make([]colorful.Color, len(stops))
	for i, s := range stops {
		c, err := colorful.Hex(s.Hex)
		if err != nil {
			c = colorful.Color{R: 0, G: 0, B: 0}
		}
		cols[i] = c
	}
	for i := 0; i < 256; i++ {
		t := float64(i) / 255
		g.lut[i] = lipgloss.Color(blend(stops, cols, t))
	}
	g.lut[0] = lipgloss.Color(normalizeHex(stops[0].Hex))
	g.lut[255] = lipgloss.Color(normalizeHex(stops[len(stops)-1].Hex))
	g.ok = true
	return g
}

func blend(stops []Stop, cols []colorful.Color, t float64) string {
	if math.IsNaN(t) || t <= 0 {
		return normalizeHex(stops[0].Hex)
	}
	if t >= 1 {
		return normalizeHex(stops[len(stops)-1].Hex)
	}
	for i := 0; i < len(stops)-1; i++ {
		p0, p1 := stops[i].Pos, stops[i+1].Pos
		if t > p1 && i != len(stops)-2 {
			continue
		}
		u := 0.0
		if p1 > p0 {
			u = (t - p0) / (p1 - p0)
		}
		if u < 0 {
			u = 0
		}
		if u > 1 {
			u = 1
		}
		return cols[i].BlendOkLab(cols[i+1], u).Clamped().Hex()
	}
	return normalizeHex(stops[len(stops)-1].Hex)
}

func normalizeHex(h string) string {
	c, err := colorful.Hex(h)
	if err != nil {
		return h
	}
	return c.Clamped().Hex()
}

// At returns the colour at t in [0,1]. NaN and negatives map to the first stop.
func (g Gradient) At(t float64) lipgloss.Color {
	if !g.ok {
		return ""
	}
	if math.IsNaN(t) || t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	i := int(math.Round(t * 255))
	if i < 0 {
		i = 0
	}
	if i > 255 {
		i = 255
	}
	return g.lut[i]
}

// BlendAt is the exact OKLab blend at t, not the LUT sample.
func BlendAt(stops []Stop, t float64) string {
	cols := make([]colorful.Color, len(stops))
	for i, s := range stops {
		c, err := colorful.Hex(s.Hex)
		if err != nil {
			continue
		}
		cols[i] = c
	}
	return blend(stops, cols, t)
}

var (
	meterStops = []Stop{{0, Ramp2.DarkHex}, {1, Ramp5.DarkHex}}
	qStops     = []Stop{{0, Ramp2.DarkHex}, {1, Ramp4.DarkHex}}
	sStops     = []Stop{{0, Ramp3.DarkHex}, {1, Ramp5.DarkHex}}
	heatDark   = rampStops(func(t Token) string { return t.DarkHex })
	heatLight  = rampStops(func(t Token) string { return t.LightHex })

	GradMeter = NewGradient(meterStops...)
	GradQ     = NewGradient(qStops...)
	GradS     = NewGradient(sStops...)
	GradHeatD = NewGradient(heatDark...)
	GradHeatL = NewGradient(heatLight...)
)

func rampStops(hex func(Token) string) []Stop {
	return []Stop{{0, hex(Ramp1)}, {0.25, hex(Ramp2)}, {0.5, hex(Ramp3)}, {0.75, hex(Ramp4)}, {1, hex(Ramp5)}}
}

// Heat returns the heatmap ramp for the active theme.
func Heat() Gradient {
	if Dark() {
		return GradHeatD
	}
	return GradHeatL
}
