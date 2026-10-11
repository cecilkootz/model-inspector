package ui

import "testing"

func TestSpreadDegradesByWholePieces(t *testing.T) {
	rows := map[string][]int64{
		"spread": {1000, 1100, 1200, 1180},
		"flat":   {2048, 2048},
		"empty":  {0, 0},
	}
	for name, cells := range rows {
		steps, _ := spreadParts(cells)
		for w := 0; w <= 30; w++ {
			got := fitSpread(steps, w)
			if Width(got) > w {
				t.Fatalf("%s w=%d: %q is %d wide", name, w, got, Width(got))
			}
			want := ""
			for _, s := range steps {
				if Width(s) <= w {
					want = s
					break
				}
			}
			if got != want {
				t.Fatalf("%s w=%d: got %q, want the longest fit %q", name, w, got, want)
			}
		}
	}

	steps, _ := spreadParts(rows["spread"])
	if len(steps) != 3 || steps[2] != "+18%" || steps[1] != "+18% "+G().Full {
		t.Fatalf("steps %q", steps)
	}
	for w, want := range map[int]string{3: "", 4: "+18%", 5: "+18%", 6: steps[1], 17: steps[1], 30: steps[0]} {
		if got := fitSpread(steps, w); got != want {
			t.Fatalf("w=%d: got %q, want %q", w, got, want)
		}
	}
}
