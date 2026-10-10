package safetensors

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// tensorCountSeed is the fuzz input that stands in for a header with
// MaxTensors+1 entries. That header is about 10 MiB. Putting those bytes
// in the mutation corpus would make every mutation of the seed copy
// megabytes, so the seed is a short marker and the header is built when
// the marker is seen. `go test` and fuzz warmup run it once.
var tensorCountSeed = []byte("seed:tensor-count-limit")

func longNameHeader(n int) []byte {
	var b bytes.Buffer
	b.Grow(n + 64)
	b.WriteString(`{"`)
	b.Write(bytes.Repeat([]byte("n"), n))
	b.WriteString(`":{"dtype":"U8","shape":[],"data_offsets":[0,0]}}`)
	return b.Bytes()
}

func longMetaHeader(n int) []byte {
	var b bytes.Buffer
	b.Grow(n + 64)
	b.WriteString(`{"__metadata__":{"note":"`)
	b.Write(bytes.Repeat([]byte("m"), n))
	b.WriteString(`"}}`)
	return b.Bytes()
}

func gapHeader() []byte {
	return []byte(`{"a":{"dtype":"U8","shape":[4],"data_offsets":[0,4]},"b":{"dtype":"U8","shape":[4],"data_offsets":[8,12]}}`)
}

func tensorCountHeader(n int) []byte {
	var b bytes.Buffer
	b.Grow(n * 48)
	b.WriteByte('{')
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('"')
		b.WriteString(strconv.Itoa(i))
		b.WriteString(`":{"dtype":"U8","shape":[],"data_offsets":[0,0]}`)
	}
	b.WriteByte('}')
	return b.Bytes()
}

func TestParseRejectsLongTensorName(t *testing.T) {
	_, err := Parse(longNameHeader(MaxNameLen + 1))
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("tensor name exceeds %d bytes", MaxNameLen)) {
		t.Fatalf("8193-byte name: %v", err)
	}
	h, err := Parse(longNameHeader(MaxNameLen))
	if err != nil {
		t.Fatalf("name at the cap: %v", err)
	}
	if len(h.Tensors) != 1 || len(h.Tensors[0].Name) != MaxNameLen {
		t.Fatalf("kept name len %d", len(h.Tensors[0].Name))
	}
}

func TestParseRejectsLongMetadataValue(t *testing.T) {
	_, err := Parse(longMetaHeader(MaxMetaLen + 1))
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("exceeds %d bytes", MaxMetaLen)) {
		t.Fatalf("65537-byte metadata: %v", err)
	}
	h, err := Parse(longMetaHeader(MaxMetaLen))
	if err != nil {
		t.Fatalf("metadata at the cap: %v", err)
	}
	if got := len(h.Metadata["note"]); got != MaxMetaLen {
		t.Fatalf("kept metadata len %d", got)
	}
}

func TestParseRejectsTooManyTensors(t *testing.T) {
	_, err := Parse(tensorCountHeader(MaxTensors + 1))
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("more than %d tensors", MaxTensors)) {
		t.Fatalf("tensor count: %v", err)
	}
}

func TestLayoutGapBetweenTensors(t *testing.T) {
	h, err := Parse(gapHeader())
	if err != nil {
		t.Fatal(err)
	}
	problems, warnings := CheckLayout("shard", h, 12)
	var hit string
	for _, p := range problems {
		if strings.Contains(p, "gap before") {
			hit = p
		}
	}
	if hit == "" {
		t.Fatalf("problems %v warnings %v", problems, warnings)
	}
	for _, want := range []string{"shard/b", "gap before [8,12]"} {
		if !strings.Contains(hit, want) {
			t.Fatalf("gap problem %q, missing %q", hit, want)
		}
	}
	for _, p := range problems {
		if strings.Contains(p, "want 0") || strings.Contains(p, "overlap") {
			t.Fatalf("gap case reported %q", p)
		}
	}
}
