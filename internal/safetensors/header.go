// Package safetensors reads safetensors headers without touching tensor data.
//
// A safetensors file is: 8 bytes little-endian u64 header length N, then N
// bytes of JSON, then the raw tensor data. Only the first 8+N bytes are read.
package safetensors

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"

	"origin.cursor.com/cecilkootz/model-inspector/internal/fileerr"
	"origin.cursor.com/cecilkootz/model-inspector/internal/safeopen"
)

const (
	// MaxHeaderLen is the largest JSON header that will be buffered.
	// About 100 MiB. The length is rejected before any allocation when it
	// exceeds the file or this cap.
	MaxHeaderLen = 100 << 20
	// MaxTensors bounds how many tensor records a header may declare.
	MaxTensors = 200_000
	// MaxDims bounds the rank of a single tensor. A shape past this is
	// rejected while decoding, so a multi-megabyte "0,0,0,..." array is
	// never materialized.
	MaxDims = 8
	// MaxNameLen bounds a tensor name retained after parsing.
	MaxNameLen = 8 << 10
	// MaxMetaLen bounds one metadata value retained after parsing.
	MaxMetaLen = 64 << 10
)

// TensorInfo describes one tensor entry in a header.
type TensorInfo struct {
	Name        string   `json:"name"`
	DType       string   `json:"dtype"`
	Shape       []int64  `json:"shape"`
	DataOffsets [2]int64 `json:"data_offsets"` // relative to the start of the data section
}

// CheckedElements returns the product of the shape (1 for scalars).
// A negative dimension or a product that does not fit in int64 is an error.
func (t TensorInfo) CheckedElements() (int64, error) {
	n := int64(1)
	for _, d := range t.Shape {
		if d < 0 {
			return 0, fmt.Errorf("negative dimension %d", d)
		}
		next, err := mulInt64(n, d)
		if err != nil {
			return 0, fmt.Errorf("shape product overflows int64")
		}
		n = next
	}
	return n, nil
}

// NumElements returns the product of the shape, or 0 when the shape is not
// a safe size. Parsed headers have already passed CheckedElements.
func (t TensorInfo) NumElements() int64 {
	n, err := t.CheckedElements()
	if err != nil {
		return 0
	}
	return n
}

// ByteLen is the size of the tensor's data range in bytes.
// A reversed range reports 0 so callers do not accumulate a negative size.
func (t TensorInfo) ByteLen() int64 {
	if t.DataOffsets[1] < t.DataOffsets[0] {
		return 0
	}
	return t.DataOffsets[1] - t.DataOffsets[0]
}

func mulInt64(a, b int64) (int64, error) {
	if a < 0 || b < 0 {
		return 0, fmt.Errorf("negative size")
	}
	if a > 0 && b > math.MaxInt64/a {
		return 0, fmt.Errorf("overflow")
	}
	return a * b, nil
}

// Header is a parsed safetensors header. The raw JSON is not retained.
type Header struct {
	HeaderLen uint64            `json:"header_len"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Tensors   []TensorInfo      `json:"tensors"` // sorted by name
}

// DataStart is the absolute file offset where tensor data begins.
func (h *Header) DataStart() int64 { return 8 + int64(h.HeaderLen) }

// ReadFile opens path with safeopen and reads only its header.
// roots, when set, constrain the resolved descriptor (see safeopen.OpenUnder).
// The returned size is the fstat size of that same descriptor.
func ReadFile(path string, roots ...string) (*Header, int64, error) {
	f, err := safeopen.OpenUnder(path, int64(MaxHeaderLen)+8, roots...)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	h, err := ReadSized(f, f.Size())
	if err != nil {
		return nil, f.Size(), fileerr.Error(fmt.Errorf("%s: %w", path, err))
	}
	return h, f.Size(), nil
}

// Read consumes exactly 8+N bytes from r and parses the header.
// When r exposes a remaining length (bytes.Reader, bytes.Buffer,
// strings.Reader), N is checked against that length before the header
// buffer is allocated.
func Read(r io.Reader) (*Header, error) {
	if n, ok := readerLen(r); ok {
		return read(r, n)
	}
	return read(r, -1)
}

// ReadSized is Read with a known file size in bytes. The header length is
// rejected before allocation when it is larger than size-8.
func ReadSized(r io.Reader, size int64) (*Header, error) {
	return read(r, size)
}

func readerLen(r io.Reader) (int64, bool) {
	switch v := r.(type) {
	case *bytes.Reader:
		return int64(v.Len()), true
	case *bytes.Buffer:
		return int64(v.Len()), true
	case *strings.Reader:
		return int64(v.Len()), true
	default:
		return 0, false
	}
}

func read(r io.Reader, size int64) (*Header, error) {
	h, err := readHeader(r, size)
	return h, fileerr.Error(err)
}

func readHeader(r io.Reader, size int64) (*Header, error) {
	var lenBuf [8]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return nil, fmt.Errorf("reading header length: %w", err)
	}
	n := binary.LittleEndian.Uint64(lenBuf[:])
	if n < 2 || n > MaxHeaderLen {
		return nil, fmt.Errorf("implausible header length %d", n)
	}
	if size >= 0 && (size < 8 || n > uint64(size-8)) {
		return nil, fmt.Errorf("header length %d exceeds file size %d", n, size)
	}
	// Grow with the bytes actually available. A short reader never forces
	// an allocation of n when n was already rejected above, and a reader
	// that ends early allocates only what it returned.
	buf, err := io.ReadAll(io.LimitReader(r, int64(n)))
	if err != nil {
		return nil, fmt.Errorf("reading %d-byte header: %w", n, err)
	}
	if uint64(len(buf)) != n {
		return nil, fmt.Errorf("reading %d-byte header: unexpected EOF", n)
	}
	h, err := Parse(buf)
	if err != nil {
		return nil, err
	}
	h.HeaderLen = n
	return h, nil
}

// Parse decodes the JSON portion of a header.
// It rejects null, {}, duplicate keys, missing dtype or shape, negative
// dimensions, overflowing shapes, and headers that declare too many tensors
// or dimensions. Error text copied from the file is capped: a huge number
// nested in shape or data_offsets is not quoted in full.
func Parse(js []byte) (*Header, error) {
	h, err := parseHeader(js)
	return h, fileerr.Error(err)
}

func parseHeader(js []byte) (*Header, error) {
	// A safetensors header is one JSON object. Reject anything else before
	// encoding/json. A bare number is not an object, and the decoder's error
	// for one quotes the entire literal: a megabyte of digits becomes a
	// megabyte error string, and strconv spends a long time on it. The
	// fuzzer's minimizer is quadratic in that length, so the slow path also
	// stalls fuzzing.
	trimmed := bytes.TrimLeft(js, " \t\r\n")
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, fmt.Errorf("parsing header JSON: header must be a JSON object")
	}
	dec := json.NewDecoder(bytes.NewReader(js))
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("parsing header JSON: %w", err)
	}
	if tok == nil {
		return nil, fmt.Errorf("parsing header JSON: null header")
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != '{' {
		return nil, fmt.Errorf("parsing header JSON: header must be a JSON object")
	}
	h := &Header{Metadata: map[string]string{}}
	seen := make(map[string]struct{})
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("parsing header JSON: %w", err)
		}
		name, ok := tok.(string)
		if !ok {
			return nil, fmt.Errorf("parsing header JSON: expected string key")
		}
		if _, dup := seen[name]; dup {
			return nil, fmt.Errorf("parsing header JSON: duplicate key %q", name)
		}
		seen[name] = struct{}{}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, fmt.Errorf("parsing header JSON: %w", err)
		}
		if name == "__metadata__" {
			md, err := parseMetadata(raw)
			if err != nil {
				return nil, fmt.Errorf("parsing __metadata__: %w", err)
			}
			h.Metadata = md
			continue
		}
		if len(h.Tensors) >= MaxTensors {
			return nil, fmt.Errorf("parsing header JSON: more than %d tensors", MaxTensors)
		}
		if len(name) > MaxNameLen {
			return nil, fmt.Errorf("tensor name exceeds %d bytes", MaxNameLen)
		}
		ti, err := parseTensor(name, raw)
		if err != nil {
			return nil, err
		}
		h.Tensors = append(h.Tensors, ti)
	}
	end, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("parsing header JSON: %w", err)
	}
	if d, ok := end.(json.Delim); !ok || d != '}' {
		return nil, fmt.Errorf("parsing header JSON: expected end of object")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("parsing header JSON: trailing data")
		}
		return nil, fmt.Errorf("parsing header JSON: %w", err)
	}
	if len(seen) == 0 {
		return nil, fmt.Errorf("parsing header JSON: empty header")
	}
	sort.Slice(h.Tensors, func(i, j int) bool { return h.Tensors[i].Name < h.Tensors[j].Name })
	return h, nil
}

func parseMetadata(raw json.RawMessage) (map[string]string, error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, fmt.Errorf("null")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != '{' {
		return nil, fmt.Errorf("expected object")
	}
	out := map[string]string{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, fmt.Errorf("expected string key")
		}
		if _, dup := out[key]; dup {
			return nil, fmt.Errorf("duplicate key %q", key)
		}
		var val string
		if err := dec.Decode(&val); err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		if len(val) > MaxMetaLen {
			return nil, fmt.Errorf("%s exceeds %d bytes", key, MaxMetaLen)
		}
		out[key] = val
	}
	end, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := end.(json.Delim); !ok || d != '}' {
		return nil, fmt.Errorf("expected end of object")
	}
	return out, nil
}

func parseTensor(name string, raw json.RawMessage) (TensorInfo, error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return TensorInfo{}, fmt.Errorf("tensor %q: null", name)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return TensorInfo{}, fmt.Errorf("tensor %q: %w", name, err)
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != '{' {
		return TensorInfo{}, fmt.Errorf("tensor %q: expected object", name)
	}
	fields := map[string]json.RawMessage{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return TensorInfo{}, fmt.Errorf("tensor %q: %w", name, err)
		}
		key, ok := tok.(string)
		if !ok {
			return TensorInfo{}, fmt.Errorf("tensor %q: expected string key", name)
		}
		if _, dup := fields[key]; dup {
			return TensorInfo{}, fmt.Errorf("tensor %q: duplicate key %q", name, key)
		}
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return TensorInfo{}, fmt.Errorf("tensor %q: %w", name, err)
		}
		fields[key] = val
	}
	end, err := dec.Token()
	if err != nil {
		return TensorInfo{}, fmt.Errorf("tensor %q: %w", name, err)
	}
	if d, ok := end.(json.Delim); !ok || d != '}' {
		return TensorInfo{}, fmt.Errorf("tensor %q: expected end of object", name)
	}
	dtRaw, ok := fields["dtype"]
	if !ok {
		return TensorInfo{}, fmt.Errorf("tensor %q: missing dtype", name)
	}
	var dtype string
	if err := json.Unmarshal(dtRaw, &dtype); err != nil || dtype == "" {
		return TensorInfo{}, fmt.Errorf("tensor %q: dtype must be a non-empty string", name)
	}
	shRaw, ok := fields["shape"]
	if !ok {
		return TensorInfo{}, fmt.Errorf("tensor %q: missing shape", name)
	}
	shape, err := parseShape(name, shRaw)
	if err != nil {
		return TensorInfo{}, err
	}
	offRaw, ok := fields["data_offsets"]
	if !ok {
		return TensorInfo{}, fmt.Errorf("tensor %q: missing data_offsets", name)
	}
	offs, err := parseOffsets(name, offRaw)
	if err != nil {
		return TensorInfo{}, err
	}
	ti := TensorInfo{Name: name, DType: dtype, Shape: shape, DataOffsets: offs}
	if _, err := ti.CheckedElements(); err != nil {
		return TensorInfo{}, fmt.Errorf("tensor %q: %w", name, err)
	}
	return ti, nil
}

func parseShape(name string, raw json.RawMessage) ([]int64, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("tensor %q: shape: %w", name, err)
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != '[' {
		return nil, fmt.Errorf("tensor %q: shape must be an array", name)
	}
	var shape []int64
	for dec.More() {
		if len(shape) >= MaxDims {
			return nil, fmt.Errorf("tensor %q: shape has more than %d dims", name, MaxDims)
		}
		var n int64
		if err := dec.Decode(&n); err != nil {
			return nil, fmt.Errorf("tensor %q: shape: %w", name, err)
		}
		if n < 0 {
			return nil, fmt.Errorf("tensor %q: negative dimension %d", name, n)
		}
		shape = append(shape, n)
	}
	end, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("tensor %q: shape: %w", name, err)
	}
	if d, ok := end.(json.Delim); !ok || d != ']' {
		return nil, fmt.Errorf("tensor %q: shape must be an array", name)
	}
	return shape, nil
}

func parseOffsets(name string, raw json.RawMessage) ([2]int64, error) {
	var zero [2]int64
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return zero, fmt.Errorf("tensor %q: data_offsets: %w", name, err)
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != '[' {
		return zero, fmt.Errorf("tensor %q: data_offsets must be an array", name)
	}
	var out [2]int64
	n := 0
	for dec.More() {
		if n >= 2 {
			return zero, fmt.Errorf("tensor %q: data_offsets must have 2 entries", name)
		}
		if err := dec.Decode(&out[n]); err != nil {
			return zero, fmt.Errorf("tensor %q: data_offsets: %w", name, err)
		}
		n++
	}
	if n != 2 {
		return zero, fmt.Errorf("tensor %q: data_offsets must have 2 entries, got %d", name, n)
	}
	end, err := dec.Token()
	if err != nil {
		return zero, fmt.Errorf("tensor %q: data_offsets: %w", name, err)
	}
	if d, ok := end.(json.Delim); !ok || d != ']' {
		return zero, fmt.Errorf("tensor %q: data_offsets must be an array", name)
	}
	return out, nil
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
