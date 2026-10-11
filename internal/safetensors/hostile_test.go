package safetensors

import (
	"bytes"
	"encoding/binary"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cecilkootz/model-inspector/internal/fileerr"
)

func TestParseRejectsNullAndEmpty(t *testing.T) {
	for _, js := range []string{"null", "{}", "[]", "1"} {
		if _, err := Parse([]byte(js)); err == nil {
			t.Fatalf("accepted %s", js)
		}
	}
}

func TestParseRejectsHugeNumberWithoutQuotingIt(t *testing.T) {
	js := bytes.Repeat([]byte("0"), 1<<20)
	js[0] = '1'
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	start := time.Now()
	_, err := Parse(js)
	elapsed := time.Since(start)
	runtime.ReadMemStats(&after)
	if err == nil {
		t.Fatal("accepted a bare number")
	}
	if len(err.Error()) > 200 {
		t.Fatalf("error quotes the literal (%d bytes)", len(err.Error()))
	}
	if !strings.Contains(err.Error(), "JSON object") {
		t.Fatal(err)
	}
	if elapsed > 50*time.Millisecond {
		t.Fatalf("Parse took %s", elapsed)
	}
	if delta := after.TotalAlloc - before.TotalAlloc; delta > 1<<20 {
		t.Fatalf("alloc delta %d", delta)
	}
}

func TestParseTruncatesNestedHugeNumber(t *testing.T) {
	const tail = "2222222222"
	digits := append(bytes.Repeat([]byte("1"), 1<<18), []byte(tail)...)
	for _, field := range []string{"shape", "data_offsets"} {
		var js []byte
		if field == "shape" {
			js = append([]byte(`{"x":{"dtype":"U8","shape":[`), digits...)
			js = append(js, []byte(`],"data_offsets":[0,0]}}`)...)
		} else {
			js = append([]byte(`{"x":{"dtype":"U8","shape":[],"data_offsets":[`), digits...)
			js = append(js, []byte(`,0]}}`)...)
		}
		_, err := Parse(js)
		if err == nil {
			t.Fatalf("%s: accepted", field)
		}
		msg := err.Error()
		if n := utf8.RuneCountInString(msg); n > fileerr.MaxRunes {
			t.Fatalf("%s: error is %d runes", field, n)
		}
		if strings.Contains(msg, tail) {
			t.Fatalf("%s: error still quotes the end of the literal: %q", field, msg)
		}
		if !strings.Contains(msg, "… (") || !strings.HasSuffix(msg, " runes)") {
			t.Fatalf("%s: %q", field, msg)
		}
		reported := reportedRunes(t, msg)
		if reported < len(digits) {
			t.Fatalf("%s: reported length %d, digits %d, msg %q", field, reported, len(digits), msg)
		}
	}
}

func reportedRunes(t *testing.T, msg string) int {
	t.Helper()
	const mark = "… ("
	i := strings.LastIndex(msg, mark)
	if i < 0 {
		t.Fatalf("no length in %q", msg)
	}
	rest := msg[i+len(mark):]
	end := strings.Index(rest, " runes)")
	if end <= 0 {
		t.Fatalf("bad suffix %q", msg)
	}
	n := 0
	for _, r := range rest[:end] {
		if r < '0' || r > '9' {
			t.Fatalf("bad length %q", rest[:end])
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func TestParseRejectsDuplicateKeys(t *testing.T) {
	js := []byte(`{"x":{"dtype":"U8","shape":[4],"data_offsets":[0,4]},"x":{"dtype":"U8","shape":[8],"data_offsets":[0,8]}}`)
	h, err := Parse(js)
	if err == nil {
		t.Fatalf("duplicate key accepted: %+v", h.Tensors)
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatal(err)
	}
}

func TestParseRejectsNegativeAndOverflowShapes(t *testing.T) {
	if _, err := Parse([]byte(`{"x":{"dtype":"F32","shape":[-1,-4],"data_offsets":[0,16]}}`)); err == nil || !strings.Contains(err.Error(), "negative") {
		t.Fatalf("negative: %v", err)
	}
	if _, err := Parse([]byte(`{"x":{"dtype":"F32","shape":[4294967296,4294967296],"data_offsets":[0,0]}}`)); err == nil || !strings.Contains(err.Error(), "overflow") {
		t.Fatalf("overflow: %v", err)
	}
}

func TestParseRequiresDtypeAndShape(t *testing.T) {
	if _, err := Parse([]byte(`{"x":{"data_offsets":[0,4]}}`)); err == nil || !strings.Contains(err.Error(), "dtype") {
		t.Fatalf("missing dtype: %v", err)
	}
	if _, err := Parse([]byte(`{"x":{"dtype":"U8","data_offsets":[0,4]}}`)); err == nil || !strings.Contains(err.Error(), "shape") {
		t.Fatalf("missing shape: %v", err)
	}
}

func TestParseCapsRankWithoutMaterializingIt(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"x":{"dtype":"U8","data_offsets":[0,0],"shape":[`)
	const dims = 10000
	for i := 0; i < dims; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('1')
	}
	b.WriteString(`]}}`)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, err := Parse([]byte(b.String()))
	runtime.ReadMemStats(&after)
	if err == nil || !strings.Contains(err.Error(), "dims") {
		t.Fatal(err)
	}
	if delta := after.TotalAlloc - before.TotalAlloc; delta > 8<<20 {
		t.Fatalf("alloc delta %d", delta)
	}
}

func TestHeaderLongerThanFileDoesNotAllocate(t *testing.T) {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, uint64(1<<20))
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, err := Read(bytes.NewReader(buf.Bytes()))
	runtime.ReadMemStats(&after)
	if err == nil {
		t.Fatal("expected error")
	}
	if delta := after.TotalAlloc - before.TotalAlloc; delta > 1<<20 {
		t.Fatalf("alloc delta %d for an 8-byte file", delta)
	}
}

func TestLayoutGapsTrailingAndUnknownDtype(t *testing.T) {
	h, err := Parse([]byte(`{"x":{"dtype":"U8","shape":[4],"data_offsets":[8,12]},"y":{"dtype":"Q4_WAT","shape":[4],"data_offsets":[12,16]}}`))
	if err != nil {
		t.Fatal(err)
	}
	problems, warnings := CheckLayout("model.safetensors", h, 20)
	blob := strings.Join(append(problems, warnings...), "\n")
	for _, want := range []string{"want 0", "data section is 20", "unknown dtype Q4_WAT"} {
		if !strings.Contains(blob, want) {
			t.Errorf("missing %q\n%s", want, blob)
		}
	}
	if len(problems) == 0 {
		t.Fatal("gaps were not failures")
	}
}
