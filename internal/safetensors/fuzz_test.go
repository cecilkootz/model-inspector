package safetensors

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

func FuzzParse(f *testing.F) {
	name := longNameHeader(MaxNameLen + 1)
	meta := longMetaHeader(MaxMetaLen + 1)
	f.Add([]byte(`{"x":{"dtype":"F32","shape":[1],"data_offsets":[0,4]}}`))
	f.Add([]byte(`null`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"x":{"dtype":"U8","shape":[4],"data_offsets":[0,4]},"x":{"dtype":"U8","shape":[1],"data_offsets":[0,1]}}`))
	f.Add([]byte(`{"x":{"dtype":"F32","shape":[-1],"data_offsets":[0,4]}}`))
	f.Add(name)
	f.Add(meta)
	f.Add(tensorCountSeed)
	// The minimizer is quadratic in input length and does not publish execs
	// until it returns. make fuzz bounds that with -fuzzminimizetime=2s.
	// Inputs past this cap are shortened to their prefix. 128 KiB is above
	// the name seed (8193 bytes) and the metadata seed (65537 bytes), so
	// those limit checks stay reachable. The tensor-count marker is handled
	// before the cap: the header it builds does not fit in 128 KiB.
	const fuzzParseCap = 128 << 10
	f.Fuzz(func(t *testing.T, data []byte) {
		switch {
		case bytes.Equal(data, tensorCountSeed):
			// Seed #7 builds a ~10 MiB header. The unit test already parses
			// it. Running it again from the seed corpus doubles the race
			// time, so only the fuzz engine (baseline and workers) pays it.
			if !fuzzEngineOn() {
				return
			}
			_, err := Parse(tensorCountHeader(MaxTensors + 1))
			if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("more than %d tensors", MaxTensors)) {
				t.Fatalf("tensor count: %v", err)
			}
			return
		case len(data) == len(name) && bytes.Equal(data, name):
			_, err := Parse(data)
			if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("tensor name exceeds %d bytes", MaxNameLen)) {
				t.Fatalf("name: %v", err)
			}
			return
		case len(data) == len(meta) && bytes.Equal(data, meta):
			_, err := Parse(data)
			if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("exceeds %d bytes", MaxMetaLen)) {
				t.Fatalf("metadata: %v", err)
			}
			return
		}
		if len(data) > fuzzParseCap {
			data = data[:fuzzParseCap]
		}
		h, err := Parse(data)
		if err != nil || h == nil {
			return
		}
		if len(h.Tensors) > MaxTensors {
			t.Fatalf("accepted %d tensors", len(h.Tensors))
		}
		seen := map[string]struct{}{}
		for _, tensor := range h.Tensors {
			if _, ok := seen[tensor.Name]; ok {
				t.Fatalf("duplicate tensor %q", tensor.Name)
			}
			seen[tensor.Name] = struct{}{}
			if tensor.DType == "" {
				t.Fatalf("empty dtype on %q", tensor.Name)
			}
			if len(tensor.Shape) > MaxDims {
				t.Fatalf("%q rank %d", tensor.Name, len(tensor.Shape))
			}
			if _, err := tensor.CheckedElements(); err != nil {
				t.Fatalf("%q: %v", tensor.Name, err)
			}
		}
	})
}

func FuzzRead(f *testing.F) {
	f.Add([]byte{2, 0, 0, 0, 0, 0, 0, 0, '{', '}'})
	f.Add([]byte{0xff, 0xff, 0xff, 0xff, 0, 0, 0, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			data = data[:1<<20]
		}
		h, err := Read(bytes.NewReader(data))
		if err != nil || h == nil {
			return
		}
		if h.HeaderLen > MaxHeaderLen {
			t.Fatalf("header len %d", h.HeaderLen)
		}
		if int64(len(data)) < 8 || h.HeaderLen > uint64(len(data)-8) {
			t.Fatalf("header len %d file %d", h.HeaderLen, len(data))
		}
	})
}

func FuzzDataOffsets(f *testing.F) {
	gap := gapHeader()
	f.Add([]byte(`{"a":{"dtype":"U8","shape":[4],"data_offsets":[0,4]}}`), int64(4))
	f.Add([]byte(`{"a":{"dtype":"U8","shape":[4],"data_offsets":[100,104]}}`), int64(100000))
	f.Add([]byte(`{"a":{"dtype":"F32","shape":[1],"data_offsets":[0,4]},"b":{"dtype":"F32","shape":[1],"data_offsets":[2,6]}}`), int64(6))
	f.Add(gap, int64(12))
	// Returning early past the cap is its own coverage edge, so the
	// minimizer would keep a cap+1 input. Prefixes share the parse path
	// with a header of exactly the cap. 128 KiB matches FuzzParse.
	// -fuzzminimizetime=2s in make fuzz bounds a long minimize.
	const fuzzOffsetsCap = 128 << 10
	f.Fuzz(func(t *testing.T, hdr []byte, dataLen int64) {
		if dataLen == 12 && bytes.Equal(hdr, gap) {
			h, err := Parse(hdr)
			if err != nil {
				t.Fatalf("gap seed: %v", err)
			}
			problems, _ := CheckLayout("shard", h, dataLen)
			if !containsText(problems, "gap before [8,12]") {
				t.Fatalf("gap seed problems: %v", problems)
			}
			return
		}
		if len(hdr) > fuzzOffsetsCap {
			hdr = hdr[:fuzzOffsetsCap]
		}
		h, err := Parse(hdr)
		if err != nil || h == nil {
			return
		}
		problems, warnings := CheckLayout("shard", h, dataLen)
		if problems == nil && warnings == nil {
			return
		}
	})
}

func containsText(items []string, want string) bool {
	for _, s := range items {
		if strings.Contains(s, want) {
			return true
		}
	}
	return false
}

// FuzzLayoutStructured builds a header from three tensor specs, so Parse
// accepts most inputs and CheckLayout sees gaps, overlaps, and size
// mismatches instead of rejected JSON. The third spec is what exposes a
// covered end that is not the max: [0,8] then [2,4] then [4,8] overlaps
// the outer span only while that end stays 8.
func FuzzLayoutStructured(f *testing.F) {
	f.Add("a", "b", "c", byte(0), int64(0), int64(4), int64(4), int64(8), int64(-1), int64(-1), int64(8))
	f.Add("a", "b", "c", byte(0), int64(0), int64(4), int64(8), int64(12), int64(-1), int64(-1), int64(12))
	// One byte past the previous end is a gap. start == prevEnd is not.
	f.Add("a", "b", "c", byte(0), int64(0), int64(4), int64(5), int64(9), int64(-1), int64(-1), int64(9))
	f.Add("a", "b", "c", byte(0), int64(0), int64(4), int64(2), int64(6), int64(-1), int64(-1), int64(6))
	f.Add("a", "b", "c", byte(0), int64(4), int64(8), int64(8), int64(12), int64(-1), int64(-1), int64(12))
	// [0,8] [2,4] [4,8]. The third range overlaps only if prevEnd stayed at 8.
	f.Add("a", "b", "c", byte(0), int64(0), int64(8), int64(2), int64(4), int64(4), int64(8), int64(8))
	f.Fuzz(func(t *testing.T, nameA, nameB, nameC string, kind byte, a0, a1, b0, b1, c0, c1, dataLen int64) {
		nameA = clipFuzzName(nameA)
		nameB = clipFuzzName(nameB)
		nameC = clipFuzzName(nameC)
		if nameA == "" {
			nameA = "a"
		}
		if nameB == "" || nameB == nameA {
			nameB = nameA + "2"
		}
		if nameC == "" || nameC == nameA || nameC == nameB {
			nameC = nameA + "3"
			if nameC == nameB {
				nameC += "x"
			}
		}
		dtypes := [...]string{"U8", "U8", "F32", "BF16", "BOOL"}
		dt := dtypes[int(kind)%len(dtypes)]
		raw, err := json.Marshal(map[string]any{
			nameA: tensorSpec(dt, a0, a1),
			nameB: tensorSpec(dt, b0, b1),
			nameC: tensorSpec(dt, c0, c1),
		})
		if err != nil {
			return
		}
		h, err := Parse(raw)
		if err != nil || h == nil {
			return
		}
		if len(h.Tensors) > MaxTensors {
			t.Fatalf("accepted %d tensors", len(h.Tensors))
		}
		problems, _ := CheckLayout("shard", h, dataLen)
		if dataLen < 0 {
			return
		}
		wantGaps, wantOverlaps, wantStart := expectLayout(
			layoutSeg{nameA, a0, a1},
			layoutSeg{nameB, b0, b1},
			layoutSeg{nameC, c0, c1},
		)
		if !layoutMatches(problems, wantGaps, wantOverlaps, wantStart) {
			t.Fatalf("a=%s [%d,%d] b=%s [%d,%d] c=%s [%d,%d] data=%d problems=%v want gaps=%v overlaps=%v start=%v",
				nameA, a0, a1, nameB, b0, b1, nameC, c0, c1, dataLen, problems, wantGaps, wantOverlaps, wantStart)
		}
	})
}

type layoutSeg struct {
	name       string
	start, end int64
}

// expectLayout is the gap, overlap, and start-at-0 result for the ranges.
// Invalid ranges (start < 0 or end < start) are skipped. The rest are
// ordered by start, then name. Adjacent ranges (next start == previous
// end) are neither a gap nor an overlap. The covered end is the max end,
// so a later range still overlaps an outer span after a contained one.
// Each overlap is "name overlaps prevName"; each gap is the tensor name.
func expectLayout(segs ...layoutSeg) (gaps, overlaps []string, start bool) {
	var valid []layoutSeg
	for _, s := range segs {
		if s.start >= 0 && s.end >= s.start {
			valid = append(valid, s)
		}
	}
	sort.Slice(valid, func(i, j int) bool {
		if valid[i].start == valid[j].start {
			return valid[i].name < valid[j].name
		}
		return valid[i].start < valid[j].start
	})
	if len(valid) == 0 {
		return nil, nil, false
	}
	if valid[0].start != 0 {
		start = true
	}
	prevEnd := valid[0].end
	prevName := valid[0].name
	for _, s := range valid[1:] {
		switch {
		case s.start > prevEnd:
			gaps = append(gaps, s.name)
		case s.start < prevEnd:
			overlaps = append(overlaps, s.name+" overlaps "+prevName)
		}
		if s.end > prevEnd {
			prevEnd, prevName = s.end, s.name
		}
	}
	return gaps, overlaps, start
}

// layoutMatches requires each expected gap and overlap, not merely that
// some overlap exists. A contained range still overlaps the outer one
// before prevEnd is updated, so the following range is the witness that
// prevEnd stayed at the max.
func layoutMatches(problems, wantGaps, wantOverlaps []string, wantStart bool) bool {
	var gotGaps, gotOverlaps int
	gotStart := false
	for _, p := range problems {
		switch {
		case strings.Contains(p, "gap before"):
			gotGaps++
		case strings.Contains(p, " overlaps "):
			gotOverlaps++
		case strings.Contains(p, "want 0"):
			gotStart = true
		}
	}
	if gotStart != wantStart || gotGaps != len(wantGaps) || gotOverlaps != len(wantOverlaps) {
		return false
	}
	for _, name := range wantGaps {
		if !containsText(problems, "/"+name+": gap before") {
			return false
		}
	}
	for _, frag := range wantOverlaps {
		if !containsText(problems, "/"+frag) {
			return false
		}
	}
	return true
}

func fuzzEngineOn() bool {
	if f := flag.Lookup("test.fuzz"); f != nil && f.Value.String() != "" {
		return true
	}
	if f := flag.Lookup("test.fuzzworker"); f != nil && f.Value.String() == "true" {
		return true
	}
	return false
}

func tensorSpec(dt string, start, end int64) map[string]any {
	var n int64
	if end >= start {
		n = end - start
	}
	switch dt {
	case "F32":
		if n >= 4 && n%4 == 0 {
			n /= 4
		}
	case "BF16":
		if n >= 2 && n%2 == 0 {
			n /= 2
		}
	}
	return map[string]any{
		"dtype":        dt,
		"shape":        []int64{n},
		"data_offsets": []int64{start, end},
	}
}

func clipFuzzName(s string) string {
	const maxName = 32
	if len(s) > maxName {
		s = s[:maxName]
	}
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
