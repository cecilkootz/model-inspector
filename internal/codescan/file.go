// Package codescan reads code and pickle files by name and by opcode, never
// by execution. It does not run, import, unpickle or decompress anything.
// Every file is untrusted: reads go through safeopen with a cap, and every
// count and length is bounded.
//
// Names in results come from the file. They are clipped here and must pass
// through sanitize.From (or fileerr.Present) once at the display boundary.
package codescan

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/cecilkootz/model-inspector/internal/safeopen"
)

// Torch zip limits.
const (
	MaxZipEntries = 4096
	MaxZipName    = 512
	// maxZipDir bounds the central directory that is read.
	maxZipDir = 2 << 20
	// maxDataPkl bounds how many data.pkl entries one archive may have scanned.
	maxDataPkl = 4
	// pickleOpenCap is the descriptor's total read budget: the 8 MiB pickle
	// plus the magic sniff, zip tail, directory and local headers scanZip may also read.
	// A plain pickle is still limited to MaxPickleBytes by its own reader.
	pickleOpenCap = MaxPickleBytes + maxZipDir + 22 + 65535 + maxDataPkl*30 + 8
)

// Kind is how a file is read.
type Kind string

const (
	KindPython      Kind = "python"
	KindPickle      Kind = "pickle"
	KindCompressed  Kind = "compressed"
	KindUnsupported Kind = "unsupported"
)

var pickleExts = set(".pkl", ".pickle", ".bin", ".pt", ".pth", ".ckpt", ".dill", ".joblib")

var compressedExts = set(".gz", ".tgz", ".zip", ".bz2", ".xz", ".zst")

// Classify picks a reader from the file name alone.
func Classify(name string) Kind {
	base := strings.ToLower(path.Base(strings.ReplaceAll(name, "\\", "/")))
	ext := path.Ext(base)
	if ext == ".py" {
		return KindPython
	}
	if _, ok := compressedExts[ext]; ok {
		return KindCompressed
	}
	if _, ok := pickleExts[ext]; ok {
		return KindPickle
	}
	return KindUnsupported
}

// Report is what scanning one file found.
type Report struct {
	Kind     Kind
	Findings []Finding
	Pickle   *PickleResult
	// Incomplete lists why part of the file was not scanned.
	Incomplete []string
	// Bytes is how much of the file was read.
	Bytes int64
	// Zip is set for a torch zip archive.
	Zip bool
}

// ScanFile reads path (shown as name) under the given roots. It never
// returns an error: a file that cannot be read is Incomplete.
func ScanFile(path, name string, roots ...string) Report {
	r := Report{Kind: Classify(name)}
	switch r.Kind {
	case KindCompressed:
		r.Incomplete = []string{"not scanned: compressed"}
		return r
	case KindUnsupported:
		r.Incomplete = []string{"not scanned: format not supported"}
		return r
	case KindPython:
		scanPyFile(&r, path, roots)
	default:
		scanPickleFile(&r, path, roots)
	}
	return r
}

func scanPyFile(r *Report, path string, roots []string) {
	f, err := safeopen.OpenUnder(path, MaxPyBytes, roots...)
	if err != nil {
		r.Incomplete = []string{"could not be read"}
		return
	}
	defer f.Close()
	buf, err := io.ReadAll(f)
	if err != nil {
		r.Incomplete = []string{"could not be read"}
		return
	}
	cut := len(buf) > MaxPyBytes
	if cut {
		buf = buf[:MaxPyBytes]
	}
	r.Bytes = int64(len(buf))
	res := ScanPython(buf, cut)
	r.Findings = res.Findings
	r.Incomplete = res.Incomplete
}

func scanPickleFile(r *Report, path string, roots []string) {
	f, err := safeopen.OpenUnder(path, pickleOpenCap, roots...)
	if err != nil {
		r.Incomplete = []string{"could not be read"}
		return
	}
	defer f.Close()
	var head [8]byte
	n, _ := f.ReadAt(head[:], 0)
	if why := compressedMagic(head[:n]); why {
		r.Incomplete = []string{"not scanned: compressed"}
		return
	}
	if n >= 2 && head[0] == 'P' && head[1] == 'K' {
		r.Zip = true
		scanZip(r, f)
		return
	}
	buf, err := io.ReadAll(io.LimitReader(f, MaxPickleBytes+1))
	if err != nil {
		r.Incomplete = []string{"could not be read"}
		return
	}
	if len(buf) > MaxPickleBytes {
		buf = buf[:MaxPickleBytes]
	}
	r.Bytes = int64(len(buf))
	r.walk(buf)
}

func compressedMagic(h []byte) bool {
	switch {
	case bytes.HasPrefix(h, []byte{0x1f, 0x8b}), bytes.HasPrefix(h, []byte("BZh")),
		bytes.HasPrefix(h, []byte{0xfd, '7', 'z', 'X', 'Z', 0}), bytes.HasPrefix(h, []byte{0x28, 0xb5, 0x2f, 0xfd}):
		return true
	case len(h) >= 2 && h[0] == 0x78 && (int(h[0])<<8|int(h[1]))%31 == 0:
		return true
	}
	return false
}

// walk runs the opcode walker over one pickle and merges the result.
func (r *Report) walk(b []byte) {
	res, err := WalkPickle(b)
	r.merge(res)
	if err != nil {
		r.Incomplete = append(r.Incomplete, err.Error())
	}
	if res.Truncated {
		r.Incomplete = append(r.Incomplete, "truncated: pickle ended before STOP or hit a cap")
	}
}

func (r *Report) merge(res PickleResult) {
	if r.Pickle == nil {
		r.Pickle = &PickleResult{}
	}
	p := r.Pickle
	p.Opcodes += res.Opcodes
	p.Pickles += res.Pickles
	p.Ops.Reduce += res.Ops.Reduce
	p.Ops.Build += res.Ops.Build
	p.Ops.Inst += res.Ops.Inst
	p.Ops.Obj += res.Ops.Obj
	p.Ops.NewObj += res.Ops.NewObj
	p.Ops.NewObjEx += res.Ops.NewObjEx
	p.Ops.Global += res.Ops.Global
	p.Ops.StackGlobal += res.Ops.StackGlobal
	p.Truncated = p.Truncated || res.Truncated
	for _, g := range res.Globals {
		dup := false
		for _, h := range p.Globals {
			if h.Module == g.Module && h.Name == g.Name {
				dup = true
				break
			}
		}
		if !dup && len(p.Globals) < MaxGlobals {
			p.Globals = append(p.Globals, g)
		}
	}
}

type zipEntry struct {
	method  uint16
	comp    uint32
	uncomp  uint32
	localAt uint32
}

// scanZip reads the central directory with explicit caps and walks only
// stored (method 0) entries named data.pkl. Nothing is inflated.
func scanZip(r *Report, f *safeopen.File) {
	size := f.Size()
	tailLen := int64(22 + 65535)
	if size < tailLen {
		tailLen = size
	}
	tail := make([]byte, tailLen)
	n, _ := f.ReadAt(tail, size-tailLen)
	tail = tail[:n]
	r.Bytes += int64(n)
	eocd := -1
	for i := len(tail) - 22; i >= 0; i-- {
		if binary.LittleEndian.Uint32(tail[i:]) == 0x06054b50 {
			eocd = i
			break
		}
	}
	if eocd < 0 {
		r.Incomplete = append(r.Incomplete, "zip directory not found; not scanned")
		return
	}
	count := uint64(binary.LittleEndian.Uint16(tail[eocd+10:]))
	cdSize := uint64(binary.LittleEndian.Uint32(tail[eocd+12:]))
	cdOff := uint64(binary.LittleEndian.Uint32(tail[eocd+16:]))
	switch {
	case count == 0xFFFF || cdSize == 0xFFFFFFFF || cdOff == 0xFFFFFFFF:
		r.Incomplete = append(r.Incomplete, "zip64 archive; not scanned")
		return
	case count > MaxZipEntries:
		r.Incomplete = append(r.Incomplete, fmt.Sprintf("zip claims %d entries (limit %d); not scanned", count, MaxZipEntries))
		return
	case cdSize > maxZipDir:
		r.Incomplete = append(r.Incomplete, "zip directory too large; not scanned")
		return
	case cdOff+cdSize > uint64(size):
		r.Incomplete = append(r.Incomplete, "zip directory out of range; not scanned")
		return
	}
	cd := make([]byte, cdSize)
	if n, _ := f.ReadAt(cd, int64(cdOff)); n != len(cd) {
		r.Incomplete = append(r.Incomplete, "zip directory out of range; not scanned")
		return
	}
	r.Bytes += int64(len(cd))
	var cands []zipEntry
	longNames, extra := false, 0
	pos := 0
	for i := uint64(0); i < count; i++ {
		if len(cd)-pos < 46 || binary.LittleEndian.Uint32(cd[pos:]) != 0x02014b50 {
			r.Incomplete = append(r.Incomplete, "zip directory malformed; partly scanned")
			break
		}
		h := cd[pos:]
		nameLen := int(binary.LittleEndian.Uint16(h[28:]))
		total := 46 + nameLen + int(binary.LittleEndian.Uint16(h[30:])) + int(binary.LittleEndian.Uint16(h[32:]))
		if total > len(h) {
			r.Incomplete = append(r.Incomplete, "zip directory malformed; partly scanned")
			break
		}
		pos += total
		if nameLen > MaxZipName {
			longNames = true
			continue
		}
		name := string(h[46 : 46+nameLen])
		if name != "data.pkl" && !strings.HasSuffix(name, "/data.pkl") {
			continue
		}
		if len(cands) >= maxDataPkl {
			extra++
			continue
		}
		cands = append(cands, zipEntry{
			method: binary.LittleEndian.Uint16(h[10:]), comp: binary.LittleEndian.Uint32(h[20:]),
			uncomp: binary.LittleEndian.Uint32(h[24:]), localAt: binary.LittleEndian.Uint32(h[42:]),
		})
	}
	if longNames {
		r.Incomplete = append(r.Incomplete, "zip entry name over 512 bytes; skipped")
	}
	if extra > 0 {
		r.Incomplete = append(r.Incomplete, fmt.Sprintf("%d more data.pkl entries; not scanned", extra))
	}
	if len(cands) == 0 && len(r.Incomplete) == 0 {
		r.Incomplete = append(r.Incomplete, "zip has no data.pkl; not scanned")
	}
	budget := int64(MaxPickleBytes)
	for _, e := range cands {
		if e.method != 0 {
			r.Incomplete = append(r.Incomplete, "compressed; not scanned")
			continue
		}
		if e.uncomp == 0xFFFFFFFF || e.localAt == 0xFFFFFFFF {
			r.Incomplete = append(r.Incomplete, "zip64 entry; not scanned")
			continue
		}
		var lh [30]byte
		if n, _ := f.ReadAt(lh[:], int64(e.localAt)); n != len(lh) || binary.LittleEndian.Uint32(lh[:]) != 0x04034b50 {
			r.Incomplete = append(r.Incomplete, "zip entry header malformed; not scanned")
			continue
		}
		dataAt := int64(e.localAt) + 30 + int64(binary.LittleEndian.Uint16(lh[26:])) + int64(binary.LittleEndian.Uint16(lh[28:]))
		if dataAt > size {
			r.Incomplete = append(r.Incomplete, "zip entry out of range; not scanned")
			continue
		}
		want := int64(e.uncomp)
		if want > size-dataAt {
			want = size - dataAt
			r.Incomplete = append(r.Incomplete, "zip entry runs past end of file")
		}
		if want > budget {
			want = budget
		}
		if budget <= 0 {
			r.Incomplete = append(r.Incomplete, "read budget exhausted; not scanned")
			break
		}
		buf := make([]byte, want)
		n, _ := f.ReadAt(buf, dataAt)
		budget -= int64(n)
		r.Bytes += int64(n)
		r.walk(buf[:n])
	}
}
