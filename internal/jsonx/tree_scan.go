package jsonx

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"unsafe"
)

// errFallback tells ParseTree to use the encoding/json decoder so syntax
// errors keep that decoder's text. The scanner accepts JSON whose strings
// are ASCII and contain no escapes; anything else is parsed by the decoder.
var errFallback = errors.New("jsonx: scan fallback")

// errNonFinite is a NaN or Infinity token. It is an error node, not a
// reason to abandon the scan.
var errNonFinite = errors.New("jsonx: non-finite")

// errHtabFull means the open-addressed key table has no empty slot.
// The probe must stop instead of spinning.
var errHtabFull = errors.New("jsonx: key table full")

type scanner struct {
	raw   []byte
	i     int
	lim   Limits
	nodes []Node
	count int
	over  bool
	full  map[int32]int
}

func parseScan(raw []byte, lim Limits) (*Tree, error) {
	// The node cap follows the file's byte length, already bounded by the
	// loader, never a count parsed out of the JSON.
	ncap := len(raw) / 8
	if ncap > lim.nodes() {
		ncap = lim.nodes()
	}
	if ncap < 64 {
		ncap = 64
	}
	s := &scanner{
		raw:   raw,
		lim:   lim,
		nodes: make([]Node, 0, ncap),
	}
	if _, err := s.value(-1, -1, 0, 0, false); err != nil {
		return nil, err
	}
	s.skipSpace()
	if s.i < len(s.raw) {
		if isValueStart(s.raw[s.i]) {
			return nil, &LocatedError{Msg: "trailing data after JSON value", Off: int64(s.i)}
		}
		return nil, errFallback
	}
	if s.over {
		return nil, &NodeLimitError{Nodes: s.count, Limit: lim.nodes()}
	}
	// raw is retained so node strings, which alias it, stay valid.
	return &Tree{Nodes: s.nodes, FullBytes: s.full, raw: raw}, nil
}

func (s *scanner) value(parent, index int32, udepth, display int, afterKey bool) (int32, error) {
	if udepth > MaxDepth {
		return -1, &LocatedError{Msg: fmt.Sprintf("JSON nesting exceeds %d", MaxDepth), Off: int64(s.i)}
	}
	s.skipSpace()
	if afterKey {
		if s.i >= len(s.raw) || s.raw[s.i] != ':' {
			return -1, errFallback
		}
		s.i++
		s.skipSpace()
	}
	if s.i >= len(s.raw) {
		return -1, errFallback
	}
	off := int64(s.i)
	switch s.raw[s.i] {
	case '{':
		return s.object(parent, index, udepth+1, display, off)
	case '[':
		return s.array(parent, index, udepth+1, display, off)
	case '"':
		a, b, err := s.parseString()
		if err != nil {
			return -1, err
		}
		return s.addString(parent, index, display, off, a, b)
	case 't':
		if err := s.word("true"); err != nil {
			return -1, err
		}
		return s.add(KindBool, parent, index, display, off, "true")
	case 'f':
		if err := s.word("false"); err != nil {
			return -1, err
		}
		return s.add(KindBool, parent, index, display, off, "false")
	case 'n':
		if err := s.word("null"); err != nil {
			return -1, err
		}
		return s.add(KindNull, parent, index, display, off, "null")
	case 'N':
		if err := s.word("NaN"); err != nil {
			return -1, err
		}
		return s.add(KindError, parent, index, display, off, "NaN")
	case 'I':
		if err := s.word("Infinity"); err != nil {
			return -1, err
		}
		return s.add(KindError, parent, index, display, off, "Infinity")
	default:
		if s.raw[s.i] == '-' || (s.raw[s.i] >= '0' && s.raw[s.i] <= '9') {
			a, b, err := s.parseNumber()
			if err == errNonFinite {
				return s.add(KindError, parent, index, display, off, alias(s.raw[a:b]))
			}
			if err != nil {
				return -1, err
			}
			return s.add(KindNumber, parent, index, display, off, alias(s.raw[a:b]))
		}
		return -1, errFallback
	}
}

func (s *scanner) object(parent, index int32, depth, display int, off int64) (int32, error) {
	if depth > MaxDepth {
		return -1, &LocatedError{Msg: fmt.Sprintf("JSON nesting exceeds %d", MaxDepth), Off: off}
	}
	s.i++
	id, err := s.push(KindObject, parent, index, display, off)
	if err != nil {
		return -1, err
	}
	s.skipSpace()
	if s.eat('}') {
		return id, nil
	}
	var ht *htab
	var last int32 = -1
	var nchild int32
	for {
		if s.i >= len(s.raw) || s.raw[s.i] != '"' {
			return -1, errFallback
		}
		koff := int64(s.i)
		a, b, err := s.parseString()
		if err != nil {
			return -1, err
		}
		key := alias(s.raw[a:b])
		if id >= 0 {
			dup := false
			if ht != nil {
				var herr error
				dup, herr = ht.add(s.raw, a, b)
				if herr != nil {
					return -1, &LocatedError{Msg: "key table is full", Off: koff}
				}
			} else if s.dup(id, key, nil) {
				dup = true
			}
			if dup {
				return -1, &LocatedError{Msg: fmt.Sprintf("duplicate key %q", key), Off: koff}
			}
		}
		// String values skip the general value() call. That path is the
		// bulk of a large flat object.
		var cid int32
		if err := s.skipColon(); err != nil {
			return -1, err
		}
		if s.i < len(s.raw) && s.raw[s.i] == '"' {
			voff := int64(s.i)
			va, vb, err := s.parseString()
			if err != nil {
				return -1, err
			}
			cid, err = s.addString(id, -1, display+1, voff, va, vb)
			if err != nil {
				return -1, err
			}
		} else {
			var err error
			cid, err = s.value(id, -1, depth, display+1, false)
			if err != nil {
				return -1, err
			}
		}
		if id >= 0 && cid >= 0 {
			s.nodes[cid].Key = key
			if ht == nil && nchild+1 >= 24 {
				ht = s.tableFor(id, a, b)
			}
			if last < 0 {
				s.nodes[id].First = cid
			} else {
				s.nodes[last].Next = cid
			}
			last = cid
			nchild++
			s.nodes[id].Len = nchild
		}
		s.skipSpace()
		if s.i >= len(s.raw) {
			return -1, errFallback
		}
		switch s.raw[s.i] {
		case ',':
			s.i++
			s.skipSpace()
			if s.i < len(s.raw) && s.raw[s.i] == '}' {
				return -1, errFallback
			}
		case '}':
			s.i++
			return id, nil
		default:
			return -1, errFallback
		}
	}
}

func (s *scanner) array(parent, index int32, depth, display int, off int64) (int32, error) {
	if depth > MaxDepth {
		return -1, &LocatedError{Msg: fmt.Sprintf("JSON nesting exceeds %d", MaxDepth), Off: off}
	}
	s.i++
	id, err := s.push(KindArray, parent, index, display, off)
	if err != nil {
		return -1, err
	}
	s.skipSpace()
	if s.eat(']') {
		return id, nil
	}
	var last int32 = -1
	var nchild int32
	var idx int32
	for {
		cid, err := s.value(id, idx, depth, display+1, false)
		if err != nil {
			return -1, err
		}
		if id >= 0 && cid >= 0 {
			if last < 0 {
				s.nodes[id].First = cid
			} else {
				s.nodes[last].Next = cid
			}
			last = cid
			nchild++
			s.nodes[id].Len = nchild
		}
		idx++
		s.skipSpace()
		if s.i >= len(s.raw) {
			return -1, errFallback
		}
		switch s.raw[s.i] {
		case ',':
			s.i++
			s.skipSpace()
			if s.i < len(s.raw) && s.raw[s.i] == ']' {
				return -1, errFallback
			}
		case ']':
			s.i++
			return id, nil
		default:
			return -1, errFallback
		}
	}
}

func (s *scanner) skipColon() error {
	s.skipSpace()
	if s.i >= len(s.raw) || s.raw[s.i] != ':' {
		return errFallback
	}
	s.i++
	s.skipSpace()
	return nil
}

func (s *scanner) tableFor(id int32, ea, eb int) *htab {
	hint := (len(s.raw) - s.i) / 16
	if hint < 64 {
		hint = 64
	}
	if hint > s.lim.nodes() {
		hint = s.lim.nodes()
	}
	h := newHtab(hint)
	for c := s.nodes[id].First; c >= 0; c = s.nodes[c].Next {
		h.addStr(s.raw, s.nodes[c].Key)
	}
	_, _ = h.add(s.raw, ea, eb)
	return h
}

func (s *scanner) dup(parent int32, key string, _ *htab) bool {
	for c := s.nodes[parent].First; c >= 0; c = s.nodes[c].Next {
		if s.nodes[c].Key == key {
			return true
		}
	}
	return false
}

func (s *scanner) push(kind Kind, parent, index int32, display int, off int64) (int32, error) {
	s.count++
	if s.over || s.count > s.lim.nodes() {
		s.over = true
		return -1, nil
	}
	if display < 0 {
		display = 0
	}
	if display > 255 {
		display = 255
	}
	s.nodes = append(s.nodes, Node{
		Kind:   kind,
		Parent: parent,
		Depth:  uint8(display),
		Index:  index,
		First:  -1,
		Next:   -1,
		Off:    off,
	})
	return int32(len(s.nodes) - 1), nil
}

func (s *scanner) add(kind Kind, parent, index int32, display int, off int64, lit string) (int32, error) {
	id, err := s.push(kind, parent, index, display, off)
	if err != nil || id < 0 {
		return id, err
	}
	s.nodes[id].Lit = lit
	s.nodes[id].Len = int32(len(lit))
	return id, nil
}

func (s *scanner) addString(parent, index int32, display int, off int64, a, b int) (int32, error) {
	id, err := s.push(KindString, parent, index, display, off)
	if err != nil || id < 0 {
		return id, err
	}
	full := b - a
	end := b
	if full > s.lim.str() {
		end = a + s.lim.str()
		if s.full == nil {
			s.full = make(map[int32]int)
		}
		s.full[id] = full
	}
	s.nodes[id].Lit = alias(s.raw[a:end])
	s.nodes[id].Len = int32(full) // ASCII on this path, so bytes == runes
	return id, nil
}

func (s *scanner) parseString() (int, int, error) {
	raw := s.raw
	if s.i >= len(raw) || raw[s.i] != '"' {
		return 0, 0, errFallback
	}
	s.i++
	a := s.i
	i := s.i
	n := len(raw)
	// Eight clean ASCII bytes at a time. A quote, escape, control, or
	// non-ASCII byte drops into the byte loop so the error path matches
	// the decoder.
	for i+8 <= n {
		v := binary.LittleEndian.Uint64(raw[i:])
		if stringWordDirty(v) {
			break
		}
		i += 8
	}
	for i < n {
		c := raw[i]
		if c == '"' {
			s.i = i + 1
			return a, i, nil
		}
		if c == '\\' || c < 0x20 || c >= 0x80 {
			return 0, 0, errFallback
		}
		i++
	}
	return 0, 0, errFallback
}

// stringWordDirty reports whether any byte is a quote, backslash, ASCII
// control, or has the high bit set. Non-ASCII has to fall back so invalid
// UTF-8 becomes U+FFFD the way encoding/json does.
func stringWordDirty(v uint64) bool {
	const quote = 0x2222222222222222
	const slash = 0x5c5c5c5c5c5c5c5c
	const e0 = 0xE0E0E0E0E0E0E0E0
	const high = 0x8080808080808080
	return hasZero(v^quote) || hasZero(v^slash) || hasZero(v&e0) || v&high != 0
}

func hasZero(v uint64) bool {
	return (v-0x0101010101010101)&^v&0x8080808080808080 != 0
}

func (s *scanner) parseNumber() (int, int, error) {
	a := s.i
	if s.raw[s.i] == '-' {
		if hasWord(s.raw, s.i, "-Infinity") {
			s.i += len("-Infinity")
			return a, s.i, errNonFinite
		}
		s.i++
		if s.i >= len(s.raw) || s.raw[s.i] < '0' || s.raw[s.i] > '9' {
			return 0, 0, errFallback
		}
	}
	if s.i >= len(s.raw) || s.raw[s.i] < '0' || s.raw[s.i] > '9' {
		return 0, 0, errFallback
	}
	if s.raw[s.i] == '0' {
		s.i++
	} else {
		for s.i < len(s.raw) && s.raw[s.i] >= '0' && s.raw[s.i] <= '9' {
			s.i++
		}
	}
	if s.i < len(s.raw) && s.raw[s.i] == '.' {
		s.i++
		digs := 0
		for s.i < len(s.raw) && s.raw[s.i] >= '0' && s.raw[s.i] <= '9' {
			digs++
			s.i++
		}
		if digs == 0 {
			return 0, 0, errFallback
		}
	}
	if s.i < len(s.raw) && (s.raw[s.i] == 'e' || s.raw[s.i] == 'E') {
		s.i++
		if s.i < len(s.raw) && (s.raw[s.i] == '+' || s.raw[s.i] == '-') {
			s.i++
		}
		digs := 0
		for s.i < len(s.raw) && s.raw[s.i] >= '0' && s.raw[s.i] <= '9' {
			digs++
			s.i++
		}
		if digs == 0 {
			return 0, 0, errFallback
		}
	}
	return a, s.i, nil
}

func (s *scanner) word(lit string) error {
	if s.i+len(lit) > len(s.raw) {
		return errFallback
	}
	for j := 0; j < len(lit); j++ {
		if s.raw[s.i+j] != lit[j] {
			return errFallback
		}
	}
	end := s.i + len(lit)
	if end < len(s.raw) {
		c := s.raw[end]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
			return errFallback
		}
	}
	s.i = end
	return nil
}

func (s *scanner) eat(c byte) bool {
	if s.i < len(s.raw) && s.raw[s.i] == c {
		s.i++
		return true
	}
	return false
}

func (s *scanner) skipSpace() {
	for s.i < len(s.raw) {
		switch s.raw[s.i] {
		case ' ', '\n', '\r', '\t':
			s.i++
		default:
			return
		}
	}
}

func isValueStart(c byte) bool {
	switch c {
	case '{', '[', '"', 't', 'f', 'n', 'N', 'I', '-':
		return true
	default:
		return c >= '0' && c <= '9'
	}
}

func alias(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return unsafe.String(unsafe.SliceData(b), len(b))
}

type htab struct {
	slot []slot
	mask int
	n    int
	// max is the slot cap. Zero means 1<<22 (4,194,304).
	max int
}

// Slots store a byte range in the input, not a string, so inserts do not
// write a pointer into the table.
type slot struct {
	hash uint64
	off  uint32
	n    uint32
}

func (h *htab) cap() int {
	if h.max > 0 {
		return h.max
	}
	return 1 << 22
}

func newHtab(hint int) *htab {
	h := &htab{}
	sz := 32
	for sz < hint*2 && sz < h.cap() {
		sz *= 2
	}
	if sz > h.cap() {
		sz = h.cap()
	}
	h.slot = make([]slot, sz)
	h.mask = sz - 1
	return h
}

// add records the raw[a:b] key and reports whether it was already present.
// One probe covers both the duplicate check and the insert. The probe
// stops after one pass so a full table cannot spin.
func (h *htab) add(raw []byte, a, b int) (bool, error) {
	if len(h.slot) == 0 {
		return false, errHtabFull
	}
	if h.n*2 >= len(h.slot) && len(h.slot) < h.cap() {
		h.rehash()
	}
	if h.n >= len(h.slot) {
		return false, errHtabFull
	}
	n := b - a
	if n < 0 {
		n = 0
		b = a
	}
	hash := hashBytes(raw[a:b])
	i := int(hash) & h.mask
	for probes := 0; probes < len(h.slot); probes++ {
		sl := h.slot[i]
		if sl.hash == 0 {
			h.slot[i] = slot{hash: hash, off: uint32(a), n: uint32(n)}
			h.n++
			return false, nil
		}
		if sl.hash == hash && int(sl.n) == n && bytes.Equal(raw[int(sl.off):int(sl.off)+int(sl.n)], raw[a:b]) {
			return true, nil
		}
		i = (i + 1) & h.mask
	}
	return false, errHtabFull
}

func (h *htab) addStr(raw []byte, key string) {
	if key == "" {
		_, _ = h.add(raw, 0, 0)
		return
	}
	p := unsafe.StringData(key)
	base := unsafe.SliceData(raw)
	off := int(uintptr(unsafe.Pointer(p)) - uintptr(unsafe.Pointer(base)))
	if off < 0 || off+len(key) > len(raw) {
		// Scanner keys alias raw. A key outside the buffer cannot be
		// probed by offset; treat it as a fresh key so a later equal
		// alias still collides on its own bytes.
		return
	}
	_, _ = h.add(raw, off, off+len(key))
}

func (h *htab) rehash() {
	old := h.slot
	n := len(old) * 2
	if n > h.cap() {
		n = h.cap()
	}
	if n < 1 {
		return
	}
	h.slot = make([]slot, n)
	h.mask = len(h.slot) - 1
	h.n = 0
	for _, sl := range old {
		if sl.hash == 0 {
			continue
		}
		i := int(sl.hash) & h.mask
		placed := false
		for probes := 0; probes < len(h.slot); probes++ {
			if h.slot[i].hash == 0 {
				h.slot[i] = sl
				h.n++
				placed = true
				break
			}
			i = (i + 1) & h.mask
		}
		if !placed {
			return
		}
	}
}

func hashBytes(s []byte) uint64 {
	var h uint64 = 14695981039346656037
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	if h == 0 {
		h = 1
	}
	return h
}
