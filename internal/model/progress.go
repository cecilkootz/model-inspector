package model

import "time"

// Progress phases. They match the loader loops; layout is reported while
// offsets are checked, which reads no extra bytes.
const (
	PhaseWalk    = "walk"
	PhaseHeaders = "headers"
	PhaseLayout  = "layout"
	PhaseIndex   = "index"
)

// Progress is one throttled load event.
type Progress struct {
	Phase       string
	Done, Total int
	Ranges      int
}

// Timings are wall times for the loader phases a check can attribute.
type Timings struct {
	Walk, Headers, Layout, Index time.Duration
}

// emitter sends progress at most every 50 ms. A full channel keeps the
// newest count so a slow consumer cannot stall the walk.
type emitter struct {
	ch   chan<- Progress
	last time.Time
}

func newEmitter(ch chan<- Progress) *emitter {
	if ch == nil {
		return nil
	}
	return &emitter{ch: ch}
}

func (m *Model) emit(p Progress, force bool) {
	if m == nil || m.prog == nil || m.prog.ch == nil {
		return
	}
	e := m.prog
	now := time.Now()
	if !force && !e.last.IsZero() && now.Sub(e.last) < 50*time.Millisecond {
		return
	}
	e.last = now
	select {
	case e.ch <- p:
	default:
	}
}

// NoteLayout records the offsets pass. Scan calls it from the existing
// CheckLayout loop; it does not open files.
func (m *Model) NoteLayout(done, total, ranges int, d time.Duration, force bool) {
	if m == nil {
		return
	}
	if d > 0 {
		m.Timings.Layout = d
	}
	m.emit(Progress{Phase: PhaseLayout, Done: done, Total: total, Ranges: ranges}, force)
}
