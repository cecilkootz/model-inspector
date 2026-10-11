package codescan

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Pickle walker limits.
const (
	// MaxPickleBytes is the most of one pickle (or all data.pkl entries of one
	// torch zip) that is read.
	MaxPickleBytes = 8 << 20
	// MaxOpcodes stops a walk; the result is Truncated.
	MaxOpcodes = 1_000_000
	// maxPickles bounds back-to-back pickles (legacy torch files hold several).
	maxPickles = 8
	// MaxGlobals bounds distinct module.name references kept.
	MaxGlobals = 256
	// maxGlobalLine bounds each line of GLOBAL/INST and each kept string.
	maxGlobalLine = 512
	maxMemo       = 1 << 17
)

// Ops counts opcodes that construct or mutate objects.
type Ops struct {
	Reduce, Build, Inst, Obj, NewObj, NewObjEx, Global, StackGlobal int
}

// Total is the sum of the counted opcodes.
func (o Ops) Total() int {
	return o.Reduce + o.Build + o.Inst + o.Obj + o.NewObj + o.NewObjEx
}

// Global is one module.name a pickle asks the unpickler to import.
type Global struct {
	Module, Name string
	Class        Class
}

// Ref is "module.name", or a placeholder when STACK_GLOBAL operands could
// not be resolved.
func (g Global) Ref() string {
	if g.Module == "" && g.Name == "" {
		return "unresolved STACK_GLOBAL"
	}
	return g.Module + "." + g.Name
}

// PickleResult is what a walk saw. Nothing is unpickled or imported.
type PickleResult struct {
	Globals []Global
	Ops     Ops
	Opcodes int
	Pickles int
	// Truncated: the stream ended before STOP, a cap was hit, or the input
	// itself was cut at MaxPickleBytes.
	Truncated bool
}

var dangerousModules = set("os", "subprocess", "builtins", "__builtin__", "sys", "socket",
	"requests", "urllib", "runpy", "importlib", "shutil", "pty", "webbrowser",
	"codecs", "base64", "operator", "types", "functools", "pickle", "copyreg",
	"nt", "posix", "ctypes", "marshal")

// usualGlobals are exact module (or module.name) references that ordinary
// torch and numpy checkpoints use.
var usualGlobals = set("torch", "torch._utils", "torch._tensor", "collections.OrderedDict",
	"numpy", "numpy.core.multiarray", "numpy._core.multiarray", "_codecs.encode")

func set(items ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(items))
	for _, s := range items {
		m[s] = struct{}{}
	}
	return m
}

// ClassifyGlobal rates a reference. Every dotted segment of module and name
// is checked, because protocol 4 resolves dotted names by attribute walk
// (torch + "os.system" reaches os.system).
func ClassifyGlobal(module, name string) Class {
	for _, part := range []string{module, name} {
		for _, seg := range strings.Split(part, ".") {
			if _, bad := dangerousModules[seg]; bad {
				return ClassDanger
			}
		}
	}
	if _, ok := usualGlobals[module]; ok {
		return ClassInfo
	}
	if _, ok := usualGlobals[module+"."+name]; ok {
		return ClassInfo
	}
	return ClassRisk
}

type item struct {
	s     string
	isStr bool
}

type walker struct {
	b     []byte
	pos   int
	res   PickleResult
	last  [2]item
	memo  map[uint64]item
	memoN uint64
	seen  map[string]struct{}
}

var errMalformed = errors.New("not a valid pickle stream")

// WalkPickle disassembles data without unpickling it. A stream that ends
// before STOP (including one cut at the read cap) is Truncated, not an
// error; an unknown opcode or impossible length is an error.
func WalkPickle(data []byte) (PickleResult, error) {
	w := &walker{b: data, seen: map[string]struct{}{}}
	w.resetStack()
	err := w.run()
	return w.res, err
}

func (w *walker) resetStack() {
	w.last = [2]item{}
	w.memo = map[uint64]item{}
	w.memoN = 0
}

func (w *walker) push(it item) { w.last[0], w.last[1] = w.last[1], it }

func (w *walker) short() { w.res.Truncated = true }

func (w *walker) remaining() uint64 { return uint64(len(w.b) - w.pos) }

// take returns the next n bytes, or ok=false when fewer remain.
func (w *walker) take(n uint64) ([]byte, bool) {
	if n > w.remaining() {
		return nil, false
	}
	out := w.b[w.pos : w.pos+int(n)]
	w.pos += int(n)
	return out, true
}

func (w *walker) uint(n int) (uint64, bool) {
	b, ok := w.take(uint64(n))
	if !ok {
		return 0, false
	}
	switch n {
	case 1:
		return uint64(b[0]), true
	case 2:
		return uint64(binary.LittleEndian.Uint16(b)), true
	case 4:
		return uint64(binary.LittleEndian.Uint32(b)), true
	default:
		return binary.LittleEndian.Uint64(b), true
	}
}

// line returns the next newline-terminated line. limit<=0 means unbounded
// (the data is already capped). ok=false means the data ended.
func (w *walker) line(limit int) (string, bool, error) {
	rest := w.b[w.pos:]
	i := indexByte(rest, '\n')
	if i < 0 {
		if limit > 0 && len(rest) > limit {
			return "", false, errMalformed
		}
		w.pos = len(w.b)
		return "", false, nil
	}
	if limit > 0 && i > limit {
		return "", false, errMalformed
	}
	s := string(rest[:i])
	w.pos += i + 1
	return strings.TrimSuffix(s, "\r"), true, nil
}

func indexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}

func (w *walker) addGlobal(module, name string) {
	key := module + "\x00" + name
	if _, dup := w.seen[key]; dup {
		return
	}
	if len(w.res.Globals) >= MaxGlobals {
		w.res.Truncated = true
		return
	}
	w.seen[key] = struct{}{}
	w.res.Globals = append(w.res.Globals, Global{Module: module, Name: name, Class: ClassifyGlobal(module, name)})
}

func clipStr(b []byte) string {
	if len(b) > maxGlobalLine {
		b = b[:maxGlobalLine]
	}
	return string(b)
}

func (w *walker) remember(idx uint64, it item) {
	if len(w.memo) < maxMemo {
		w.memo[idx] = it
	}
}

func (w *walker) run() error {
	for {
		if w.res.Opcodes >= MaxOpcodes {
			w.short()
			return nil
		}
		if w.pos >= len(w.b) {
			w.short()
			return nil
		}
		op := w.b[w.pos]
		w.pos++
		w.res.Opcodes++
		done, err := w.step(op)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
}

// step executes one opcode. done is true when the walk is over.
func (w *walker) step(op byte) (done bool, err error) {
	marker := item{}
	switch op {
	case '.': // STOP
		w.res.Pickles++
		if w.pos < len(w.b) && w.b[w.pos] == 0x80 && w.res.Pickles < maxPickles {
			w.resetStack()
			return false, nil
		}
		return true, nil
	case 0x80: // PROTO
		v, ok := w.uint(1)
		if !ok {
			w.short()
			return true, nil
		}
		if v > 5 {
			return true, errMalformed
		}
	case 0x95: // FRAME
		if _, ok := w.uint(8); !ok {
			w.short()
			return true, nil
		}
	case 0x94: // MEMOIZE
		w.remember(w.memoN, w.last[1])
		w.memoN++
	case 'p': // PUT
		s, ok, err := w.line(0)
		if err != nil {
			return true, err
		}
		if !ok {
			w.short()
			return true, nil
		}
		if n, perr := strconv.ParseUint(strings.TrimSpace(s), 10, 64); perr == nil {
			w.remember(n, w.last[1])
		}
	case 'q', 'r': // BINPUT, LONG_BINPUT
		size := 1
		if op == 'r' {
			size = 4
		}
		n, ok := w.uint(size)
		if !ok {
			w.short()
			return true, nil
		}
		w.remember(n, w.last[1])
	case 'g': // GET
		s, ok, err := w.line(0)
		if err != nil {
			return true, err
		}
		if !ok {
			w.short()
			return true, nil
		}
		n, _ := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
		w.push(w.memo[n])
	case 'h', 'j': // BINGET, LONG_BINGET
		size := 1
		if op == 'j' {
			size = 4
		}
		n, ok := w.uint(size)
		if !ok {
			w.short()
			return true, nil
		}
		w.push(w.memo[n])
	case 0x8c, 'X', 0x8d: // SHORT_BINUNICODE, BINUNICODE, BINUNICODE8
		size := 1
		switch op {
		case 'X':
			size = 4
		case 0x8d:
			size = 8
		}
		n, ok := w.uint(size)
		if !ok {
			w.short()
			return true, nil
		}
		b, ok := w.take(n)
		if !ok {
			w.short()
			return true, nil
		}
		w.push(item{s: clipStr(b), isStr: true})
	case 'c', 'i': // GLOBAL, INST
		mod, ok, err := w.line(maxGlobalLine)
		if err != nil {
			return true, err
		}
		if !ok {
			w.short()
			return true, nil
		}
		name, ok, err := w.line(maxGlobalLine)
		if err != nil {
			return true, err
		}
		if !ok {
			w.short()
			return true, nil
		}
		if op == 'i' {
			w.res.Ops.Inst++
		} else {
			w.res.Ops.Global++
		}
		w.addGlobal(mod, name)
		w.push(marker)
	case 0x93: // STACK_GLOBAL
		w.res.Ops.StackGlobal++
		if w.last[0].isStr && w.last[1].isStr {
			w.addGlobal(w.last[0].s, w.last[1].s)
		} else {
			w.addGlobal("", "")
		}
		w.push(marker)
	default:
		if skip, ok := fixedSkip[op]; ok {
			if _, ok := w.take(uint64(skip)); !ok {
				w.short()
				return true, nil
			}
			w.push(marker)
			return false, nil
		}
		if size, ok := lenPrefixed[op]; ok {
			n, ok := w.uint(size)
			if !ok {
				w.short()
				return true, nil
			}
			if _, ok := w.take(n); !ok {
				w.short()
				return true, nil
			}
			w.push(marker)
			return false, nil
		}
		if lineOps[op] {
			if _, ok, err := w.line(0); err != nil {
				return true, err
			} else if !ok {
				w.short()
				return true, nil
			}
			w.push(marker)
			return false, nil
		}
		if !noArg[op] {
			return true, fmt.Errorf("%w: unknown opcode 0x%02x at byte %d", errMalformed, op, w.pos-1)
		}
		switch op {
		case 'R':
			w.res.Ops.Reduce++
		case 'b':
			w.res.Ops.Build++
		case 'o':
			w.res.Ops.Obj++
		case 0x81:
			w.res.Ops.NewObj++
		case 0x92:
			w.res.Ops.NewObjEx++
		}
		w.push(marker)
	}
	return false, nil
}

// fixedSkip: opcodes with a fixed-size argument.
var fixedSkip = map[byte]int{
	'J': 4, 'K': 1, 'M': 2, 'G': 8, 0x82: 1, 0x83: 2, 0x84: 4,
}

// lenPrefixed: opcodes whose argument is a length followed by that many bytes.
var lenPrefixed = map[byte]int{
	'T': 4, 'U': 1, 'B': 4, 'C': 1, 0x8a: 1, 0x8b: 4, 0x8e: 8, 0x96: 8,
}

// lineOps: opcodes whose argument is one text line.
var lineOps = map[byte]bool{'F': true, 'I': true, 'L': true, 'P': true, 'S': true, 'V': true}

// noArg: opcodes with no argument.
var noArg = func() map[byte]bool {
	m := map[byte]bool{}
	for _, c := range []byte("(012NQRabdeolstu])}") {
		m[c] = true
	}
	for _, c := range []byte{0x81, 0x85, 0x86, 0x87, 0x88, 0x89, 0x8f, 0x90, 0x91, 0x92, 0x97, 0x98} {
		m[c] = true
	}
	return m
}()
