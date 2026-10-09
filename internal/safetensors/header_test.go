package safetensors

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

func encode(hdr string, data int) []byte {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, uint64(len(hdr)))
	buf.WriteString(hdr)
	buf.Write(make([]byte, data))
	return buf.Bytes()
}

type explodingReader struct{}

func (explodingReader) Read([]byte) (int, error) { return 0, errors.New("read past header") }

func TestReadParsesHeaderOnly(t *testing.T) {
	hdr := `{"__metadata__":{"format":"mlx"},` +
		`"b.weight":{"dtype":"BF16","shape":[2,3],"data_offsets":[16,28]},` +
		`"a.weight":{"dtype":"F32","shape":[4],"data_offsets":[0,16]}}   `
	// Anything after the header explodes: proves we never touch tensor data.
	r := io.MultiReader(bytes.NewReader(encode(hdr, 0)), explodingReader{})
	h, err := Read(r)
	if err != nil {
		t.Fatal(err)
	}
	if h.HeaderLen != uint64(len(hdr)) {
		t.Errorf("HeaderLen = %d, want %d", h.HeaderLen, len(hdr))
	}
	if got := h.DataStart(); got != int64(8+len(hdr)) {
		t.Errorf("DataStart = %d", got)
	}
	if h.Metadata["format"] != "mlx" {
		t.Errorf("metadata = %v", h.Metadata)
	}
	if len(h.Tensors) != 2 || h.Tensors[0].Name != "a.weight" || h.Tensors[1].Name != "b.weight" {
		t.Fatalf("tensors not parsed/sorted: %+v", h.Tensors)
	}
	b := h.Tensors[1]
	if b.DType != "BF16" || b.NumElements() != 6 || b.ByteLen() != 12 {
		t.Errorf("b.weight = %+v elems=%d bytes=%d", b, b.NumElements(), b.ByteLen())
	}
}

func TestRejectsImplausibleLength(t *testing.T) {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, uint64(1)<<40)
	if _, err := Read(&buf); err == nil {
		t.Fatal("expected error for 1 TiB header length")
	}
}

func TestRejectsBadOffsets(t *testing.T) {
	_, err := Parse([]byte(`{"x":{"dtype":"F32","shape":[1],"data_offsets":[0]}}`))
	if err == nil {
		t.Fatal("expected error for 1-element data_offsets")
	}
}

func TestTruncatedHeader(t *testing.T) {
	full := encode(`{"a":{"dtype":"F32","shape":[1],"data_offsets":[0,4]}}`, 0)
	if _, err := Read(bytes.NewReader(full[:len(full)-5])); err == nil {
		t.Fatal("expected error for truncated header")
	}
}
