package model

import (
	"testing"
	"time"

	"github.com/cecilkootz/model-inspector/internal/fixture"
)

func TestLoadProgressEndsAtTotal(t *testing.T) {
	files, _, err := fixture.TinyQwen()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := fixture.Materialize(dir, files); err != nil {
		t.Fatal(err)
	}
	ch := make(chan Progress, 64)
	if _, err := LoadWithProgress(dir, ch); err != nil {
		t.Fatal(err)
	}
	var ev []Progress
	for {
		select {
		case p := <-ch:
			ev = append(ev, p)
		default:
			goto drained
		}
	}
drained:
	if len(ev) == 0 {
		t.Fatal("no progress events")
	}
	last := map[string]Progress{}
	prev := map[string]int{}
	for _, p := range ev {
		if p.Total > 0 && p.Done > p.Total {
			t.Fatalf("done %d > total %d (%s)", p.Done, p.Total, p.Phase)
		}
		if d, ok := prev[p.Phase]; ok && p.Done < d {
			t.Fatalf("%s went %d -> %d", p.Phase, d, p.Done)
		}
		prev[p.Phase] = p.Done
		last[p.Phase] = p
	}
	for phase, p := range last {
		if p.Total > 0 && p.Done != p.Total {
			t.Fatalf("last %s event %+v", phase, p)
		}
	}
}

func TestNoteLayoutEmits(t *testing.T) {
	ch := make(chan Progress, 4)
	m := &Model{prog: newEmitter(ch)}
	m.NoteLayout(1, 4, 2, time.Millisecond, false)
	m.NoteLayout(4, 4, 9, 2*time.Millisecond, true)
	var p Progress
	n := 0
	for {
		select {
		case ev := <-ch:
			p = ev
			n++
		default:
			goto got
		}
	}
got:
	if n == 0 || p.Phase != PhaseLayout || p.Done != 4 || p.Total != 4 || p.Ranges != 9 {
		t.Fatalf("n=%d last=%+v", n, p)
	}
	if m.Timings.Layout == 0 {
		t.Fatal("layout time was not recorded")
	}
}
