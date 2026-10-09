// Package safetensors reads safetensors headers without touching tensor data.
//
// A safetensors file is: 8 bytes little-endian u64 header length N, then N
// bytes of JSON, then the raw tensor data. Only the first 8+N bytes are read.
package safetensors

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
)

// MaxHeaderLen guards against corrupt or hostile files claiming a huge header.
const MaxHeaderLen = 256 << 20

// TensorInfo describes one tensor entry in a header.
type TensorInfo struct {
	Name        string   `json:"name"`
	DType       string   `json:"dtype"`
	Shape       []int64  `json:"shape"`
	DataOffsets [2]int64 `json:"data_offsets"` // relative to the start of the data section
}

// NumElements returns the product of the shape (1 for scalars).
func (t TensorInfo) NumElements() int64 {
	n := int64(1)
	for _, d := range t.Shape {
		n *= d
	}
	return n
}

// ByteLen is the size of the tensor's data range in bytes.
func (t TensorInfo) ByteLen() int64 { return t.DataOffsets[1] - t.DataOffsets[0] }

// Header is a parsed safetensors header.
type Header struct {
	HeaderLen uint64            `json:"header_len"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Tensors   []TensorInfo      `json:"tensors"` // sorted by name
}

// DataStart is the absolute file offset where tensor data begins.
func (h *Header) DataStart() int64 { return 8 + int64(h.HeaderLen) }

// ReadFile opens path and reads only its header.
func ReadFile(path string) (*Header, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h, err := Read(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return h, nil
}

// Read consumes exactly 8+N bytes from r and parses the header.
func Read(r io.Reader) (*Header, error) {
	var lenBuf [8]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return nil, fmt.Errorf("reading header length: %w", err)
	}
	n := binary.LittleEndian.Uint64(lenBuf[:])
	if n < 2 || n > MaxHeaderLen {
		return nil, fmt.Errorf("implausible header length %d", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, fmt.Errorf("reading %d-byte header: %w", n, err)
	}
	h, err := Parse(buf)
	if err != nil {
		return nil, err
	}
	h.HeaderLen = n
	return h, nil
}

type rawTensor struct {
	DType       string  `json:"dtype"`
	Shape       []int64 `json:"shape"`
	DataOffsets []int64 `json:"data_offsets"`
}

// Parse decodes the JSON portion of a header.
func Parse(js []byte) (*Header, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(js, &raw); err != nil {
		return nil, fmt.Errorf("parsing header JSON: %w", err)
	}
	h := &Header{Metadata: map[string]string{}}
	for name, msg := range raw {
		if name == "__metadata__" {
			if err := json.Unmarshal(msg, &h.Metadata); err != nil {
				return nil, fmt.Errorf("parsing __metadata__: %w", err)
			}
			continue
		}
		var rt rawTensor
		if err := json.Unmarshal(msg, &rt); err != nil {
			return nil, fmt.Errorf("tensor %q: %w", name, err)
		}
		if len(rt.DataOffsets) != 2 {
			return nil, fmt.Errorf("tensor %q: data_offsets must have 2 entries, got %d", name, len(rt.DataOffsets))
		}
		h.Tensors = append(h.Tensors, TensorInfo{
			Name:        name,
			DType:       rt.DType,
			Shape:       rt.Shape,
			DataOffsets: [2]int64{rt.DataOffsets[0], rt.DataOffsets[1]},
		})
	}
	sort.Slice(h.Tensors, func(i, j int) bool { return h.Tensors[i].Name < h.Tensors[j].Name })
	return h, nil
}

var dtypeSizes = map[string]int64{
	"BOOL": 1, "U8": 1, "I8": 1, "F8_E4M3": 1, "F8_E5M2": 1, "F8_E8M0": 1,
	"U16": 2, "I16": 2, "F16": 2, "BF16": 2,
	"U32": 4, "I32": 4, "F32": 4,
	"U64": 8, "I64": 8, "F64": 8,
}

// DTypeSize returns the element size in bytes for a known dtype.
func DTypeSize(dtype string) (int64, bool) {
	s, ok := dtypeSizes[dtype]
	return s, ok
}
