//go:build race

package tui

// The race detector slows rendering 10–30x, and CI runners vary further.
const raceScale = 10
