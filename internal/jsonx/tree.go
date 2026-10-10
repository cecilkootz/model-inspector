package jsonx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

// Kind is the JSON type of a tree node.
type Kind uint8

const (
	KindObject Kind = iota
	KindArray
	KindString
	KindNumber
	KindBool
	KindNull
)

// Default node and string caps. Callers can override them through Limits.
const (
	MaxNodesDefault  = 250_000
	MaxStringDefault = 64 << 10
)

// Node is one JSON value in a pre-order arena. Index 0 is the root.
// First and Next are -1 when absent. Index is -1 for non-array nodes.
// Parent is -1 for the root.
type Node struct {
	Kind   Kind
	Parent int32
	Depth  uint8
	Key    string
	Index  int32
	Lit    string
	Len    int32
	First  int32
	Next   int32
	Off    int64
}

// Tree is an ordered JSON document. Numbers keep their literal text.
type Tree struct {
	Nodes []Node
	// FullBytes is the original string byte length for nodes whose Lit
	// was capped at Limits.MaxString. Missing entries were stored whole.
	FullBytes map[int32]int
	// raw keeps the input alive. The scanner aliases keys and literals
	// into it; dropping the caller's slice must not free those bytes.
	raw []byte
}

// Limits bounds a parse. Zero fields use the defaults.
type Limits struct {
	MaxNodes  int
	MaxString int
}

func (l Limits) nodes() int {
	if l.MaxNodes <= 0 {
		return MaxNodesDefault
	}
	return l.MaxNodes
}

func (l Limits) str() int {
	if l.MaxString <= 0 {
		return MaxStringDefault
	}
	return l.MaxString
}

// LocatedError is a parse failure at a byte offset. Error text matches
// Unmarshal for duplicate keys, depth, trailing data, and syntax errors.
type LocatedError struct {
	Msg string
	Off int64
}

func (e *LocatedError) Error() string { return e.Msg }

// NodeLimitError means the document is valid JSON but has too many nodes
// for the tree. Unmarshal may still accept it.
type NodeLimitError struct {
	Nodes int
	Limit int
}

func (e *NodeLimitError) Error() string {
	return fmt.Sprintf("%d nodes exceeds limit %d", e.Nodes, e.Limit)
}

// ParseTree decodes one JSON value into an ordered arena. Objects keep
// file order, numbers keep their literal text, and duplicate keys, depth,
// and trailing data fail the same way Unmarshal does.
func ParseTree(raw []byte, lim Limits) (*Tree, error) {
	tree, err := parseScan(raw, lim)
	if err == errFallback {
		return parseDecoder(raw, lim)
	}
	return tree, err
}

func parseDecoder(raw []byte, lim Limits) (*Tree, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	b := &builder{
		lim:   lim,
		raw:   raw,
		nodes: make([]Node, 0, 64),
	}
	if _, err := b.value(dec, -1, "", -1, 0, 0, false); err != nil {
		return nil, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, &LocatedError{Msg: "trailing data after JSON value", Off: dec.InputOffset()}
		}
		return nil, locate(err, dec.InputOffset())
	}
	if b.over {
		return nil, &NodeLimitError{Nodes: b.count, Limit: lim.nodes()}
	}
	return &Tree{Nodes: b.nodes, FullBytes: b.full}, nil
}

type builder struct {
	lim   Limits
	raw   []byte
	nodes []Node
	count int
	over  bool
	full  map[int32]int
}

// udepth matches Unmarshal's nesting counter. display is the node's depth
// in the tree, with the root at 0.
func (b *builder) value(dec *json.Decoder, parent int32, key string, index int32, udepth, display int, afterKey bool) (int32, error) {
	if udepth > MaxDepth {
		return -1, &LocatedError{Msg: fmt.Sprintf("JSON nesting exceeds %d", MaxDepth), Off: dec.InputOffset()}
	}
	off := valueStart(b.raw, dec.InputOffset(), afterKey)
	tok, err := dec.Token()
	if err != nil {
		return -1, locate(err, off)
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			return b.object(dec, parent, key, index, udepth+1, off, display)
		case '[':
			return b.array(dec, parent, key, index, udepth+1, off, display)
		default:
			return -1, &LocatedError{Msg: fmt.Sprintf("unexpected delimiter %q", t), Off: off}
		}
	case nil:
		return b.add(KindNull, parent, key, index, display, off, "null", 0)
	case bool:
		lit := "false"
		if t {
			lit = "true"
		}
		return b.add(KindBool, parent, key, index, display, off, lit, 0)
	case string:
		return b.addString(parent, key, index, display, off, t)
	case json.Number:
		return b.add(KindNumber, parent, key, index, display, off, t.String(), int32(len(t.String())))
	default:
		return -1, &LocatedError{Msg: fmt.Sprintf("unexpected JSON token %T", tok), Off: off}
	}
}

func (b *builder) object(dec *json.Decoder, parent int32, key string, index int32, depth int, off int64, display int) (int32, error) {
	if depth > MaxDepth {
		return -1, &LocatedError{Msg: fmt.Sprintf("JSON nesting exceeds %d", MaxDepth), Off: off}
	}
	id, err := b.add(KindObject, parent, key, index, display, off, "", 0)
	if err != nil {
		return -1, err
	}
	// The seen set is dropped when this object returns, and it stops
	// growing once the node cap is hit. It is not sized from the file.
	var seen map[string]struct{}
	if id >= 0 {
		seen = make(map[string]struct{})
	}
	var last int32 = -1
	var nchild int32
	for dec.More() {
		koff := dec.InputOffset()
		tok, err := dec.Token()
		if err != nil {
			return -1, locate(err, koff)
		}
		k, ok := tok.(string)
		if !ok {
			return -1, &LocatedError{Msg: "expected string object key", Off: koff}
		}
		if id >= 0 {
			if _, dup := seen[k]; dup {
				return -1, &LocatedError{Msg: fmt.Sprintf("duplicate key %q", k), Off: koff}
			}
			seen[k] = struct{}{}
		}
		cid, err := b.value(dec, id, k, -1, depth, display+1, true)
		if err != nil {
			return -1, err
		}
		if id >= 0 && cid >= 0 {
			if last < 0 {
				b.nodes[id].First = cid
			} else {
				b.nodes[last].Next = cid
			}
			last = cid
			nchild++
		}
	}
	tok, err := dec.Token()
	if err != nil {
		return -1, locate(err, dec.InputOffset())
	}
	if d, ok := tok.(json.Delim); !ok || d != '}' {
		return -1, &LocatedError{Msg: "expected end of object", Off: dec.InputOffset()}
	}
	if id >= 0 {
		b.nodes[id].Len = nchild
	}
	return id, nil
}

func (b *builder) array(dec *json.Decoder, parent int32, key string, index int32, depth int, off int64, display int) (int32, error) {
	if depth > MaxDepth {
		return -1, &LocatedError{Msg: fmt.Sprintf("JSON nesting exceeds %d", MaxDepth), Off: off}
	}
	id, err := b.add(KindArray, parent, key, index, display, off, "", 0)
	if err != nil {
		return -1, err
	}
	var last int32 = -1
	var nchild int32
	var idx int32
	for dec.More() {
		cid, err := b.value(dec, id, "", idx, depth, display+1, false)
		if err != nil {
			return -1, err
		}
		if id >= 0 && cid >= 0 {
			if last < 0 {
				b.nodes[id].First = cid
			} else {
				b.nodes[last].Next = cid
			}
			last = cid
			nchild++
		}
		idx++
	}
	tok, err := dec.Token()
	if err != nil {
		return -1, locate(err, dec.InputOffset())
	}
	if d, ok := tok.(json.Delim); !ok || d != ']' {
		return -1, &LocatedError{Msg: "expected end of array", Off: dec.InputOffset()}
	}
	if id >= 0 {
		b.nodes[id].Len = nchild
	}
	return id, nil
}

func (b *builder) add(kind Kind, parent int32, key string, index int32, depth int, off int64, lit string, n int32) (int32, error) {
	b.count++
	if b.over || b.count > b.lim.nodes() {
		b.over = true
		return -1, nil
	}
	if depth < 0 {
		depth = 0
	}
	if depth > 255 {
		depth = 255
	}
	b.nodes = append(b.nodes, Node{
		Kind:   kind,
		Parent: parent,
		Depth:  uint8(depth),
		Key:    key,
		Index:  index,
		Lit:    lit,
		Len:    n,
		First:  -1,
		Next:   -1,
		Off:    off,
	})
	return int32(len(b.nodes) - 1), nil
}

func (b *builder) addString(parent int32, key string, index int32, depth int, off int64, s string) (int32, error) {
	stored, full, cut := capString(s, b.lim.str())
	runes := utf8.RuneCountInString(s)
	if runes > int(^uint32(0)>>1) {
		runes = int(^uint32(0) >> 1)
	}
	id, err := b.add(KindString, parent, key, index, depth, off, stored, int32(runes))
	if err != nil || id < 0 {
		return id, err
	}
	if cut {
		if b.full == nil {
			b.full = make(map[int32]int)
		}
		b.full[id] = full
	}
	return id, nil
}

// capString keeps at most max bytes, copied off the original backing
// array, and never cuts a UTF-8 sequence. max is the parser limit, not a
// size read from the file.
func capString(s string, max int) (stored string, full int, cut bool) {
	full = len(s)
	if max < 0 {
		max = 0
	}
	if len(s) <= max {
		return s, full, false
	}
	cutAt := max
	for cutAt > 0 && !utf8.RuneStart(s[cutAt]) {
		cutAt--
	}
	buf := make([]byte, cutAt)
	copy(buf, s[:cutAt])
	return string(buf), full, true
}

// valueStart lands on the first byte of a value. Decoder.InputOffset
// sits at the end of the previous token, before whitespace and, for
// object values, before the colon.
func valueStart(raw []byte, off int64, afterKey bool) int64 {
	off = skipSpace(raw, off)
	if afterKey && int(off) < len(raw) && raw[off] == ':' {
		off = skipSpace(raw, off+1)
	}
	return off
}

func skipSpace(raw []byte, off int64) int64 {
	for int(off) < len(raw) {
		switch raw[off] {
		case ' ', '\n', '\r', '\t':
			off++
		default:
			return off
		}
	}
	return off
}

func locate(err error, off int64) error {
	if err == nil {
		return nil
	}
	var se *json.SyntaxError
	if errors.As(err, &se) {
		o := se.Offset - 1
		if o < 0 {
			o = 0
		}
		return &LocatedError{Msg: se.Error(), Off: o}
	}
	return &LocatedError{Msg: err.Error(), Off: off}
}
