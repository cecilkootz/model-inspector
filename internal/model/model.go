// Package model loads a model directory's metadata: file sizes via stat,
// small JSON files, and safetensors headers. It never reads tensor data.
package model

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"origin.cursor.com/cecilkootz/model-inspector/internal/fileerr"
	"origin.cursor.com/cecilkootz/model-inspector/internal/jsonx"
	"origin.cursor.com/cecilkootz/model-inspector/internal/safeopen"
	"origin.cursor.com/cecilkootz/model-inspector/internal/safetensors"
)

const (
	// maxSmallJSON caps config and index reads; anything bigger is refused.
	maxSmallJSON = 64 << 20
	// maxWalkDepth, maxFiles, and maxWarnings stop a hostile tree from
	// turning a directory walk into unbounded work. maxFiles counts every
	// visited entry, directories included.
	maxWalkDepth = 8
	maxFiles     = 20_000
	maxWarnings  = 100
)

// ErrIndexWeightMap is reported when an index file parses but weight_map
// is missing or has no entries. The JSON itself was readable.
var ErrIndexWeightMap = errors.New("missing or empty weight_map")

// ErrMultipleIndex is reported when a directory contains more than one
// *.safetensors.index.json. None of them is selected.
var ErrMultipleIndex = errors.New("multiple safetensors index files")

// Tensor roles.
const (
	RolePlain       = "plain"
	RoleQuantWeight = "qweight"
	RoleQuantScales = "qscales"
	RoleQuantBiases = "qbiases"
)

type File struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	Symlink bool   `json:"symlink"`
	Broken  bool   `json:"broken,omitempty"`
}

type Shard struct {
	Name   string              `json:"name"`
	Path   string              `json:"-"`
	Size   int64               `json:"size"`
	Header *safetensors.Header `json:"-"`
	Err    error               `json:"-"`
}

type Tensor struct {
	safetensors.TensorInfo
	Shard         string
	Group         string
	Kind          string
	Layer         int
	Role          string
	Bits          int   // inferred bits for quantized weights
	LogicalParams int64 // dequantized element count; 0 for quant scales/biases
}

type Group struct {
	Key              string
	Kind             string
	Layer            int
	Tensors          []*Tensor
	Elements         int64
	LogicalParams    int64
	Bytes            int64
	HasExperts       bool
	QuantizedTensors int
}

type Index struct {
	File      string
	TotalSize int64
	WeightMap map[string]string
	// badTotalSize is set when metadata.total_size was present but did not
	// fit in a non-negative int64. TotalSize stays 0 so it is not displayed
	// as a wrapped value.
	badTotalSize bool
}

type QuantParams struct {
	Bits      int `json:"bits,omitempty"`
	GroupSize int `json:"group_size,omitempty"`
}

type Quantization struct {
	Source       string                 `json:"source"`
	Bits         int                    `json:"bits,omitempty"`
	GroupSize    int                    `json:"group_size,omitempty"`
	Mode         string                 `json:"mode,omitempty"`
	Overrides    map[string]QuantParams `json:"-"`
	NumOverrides int                    `json:"per_module_overrides"`
}

type DTypeStat struct {
	Tensors  int   `json:"tensors"`
	Elements int64 `json:"elements"`
	Bytes    int64 `json:"bytes"`
}

type Model struct {
	Dir      string
	RepoID   string
	Revision string
	Format   string

	Files     []File
	TotalSize int64

	ConfigRaw []byte
	Config    map[string]any
	// auxParsed holds tokenizer/processor/preprocessor configs that parsed.
	auxParsed map[string]map[string]any
	// UncheckedAutoMap lists config files that exist but could not be read
	// or parsed, so top-level auto_map was not checked. The inspection is
	// partial, and code-exec warns instead of passing as "none".
	UncheckedAutoMap []string
	Quant            *Quantization
	Index            *Index
	// IndexErr is set when an index file is present but cannot be used
	// (unreadable JSON, a missing or empty weight_map, or several index
	// files). Index stays nil so shard discovery can still see the
	// .safetensors files.
	IndexErr error `json:"-"`
	// Partial is set when the inspection did not finish, or a value it
	// needed could not be used: a walk cap, a per-entry walk error, an
	// unreadable, dangling, or unparseable config.json (or an aux config
	// that exists but cannot be read or parsed), an index directory that could
	// not be listed, a display counter that overflowed, an index
	// metadata.total_size that is not a non-negative int64, or a shard that
	// is missing, unreadable, or not a local file name. The TUI, the text
	// summary, JSON partial, and the inspect exit code all follow this flag.
	// A partial inspection must not produce an overall pass.
	Partial bool `json:"-"`

	Shards  []*Shard
	Tensors []*Tensor
	Groups  []*Group

	DTypes             map[string]*DTypeStat
	BitsHistogram      map[int]int
	TotalElements      int64
	TotalLogicalParams int64
	TensorBytes        int64

	Warnings []string

	// DroppedLayers lists decoder indexes left out of the heatmap.
	// A negative index means the digits did not fit in int.
	// The scan reads this field. It does not match warning text.
	DroppedLayers []int
	dropped       map[int]struct{}

	warningsTrunc bool
	overflowed    bool
	// hostReady caches gatherHostCapable. The scan runs after that pass, so
	// a later render cannot append warnings or flip Partial on its own.
	hostReady bool
	hostCache []HostTensor
}

// Load inspects dir. Errors in individual shards are recorded, not fatal.
// dir is resolved with EvalSymlinks before the walk, so a symlinked root
// is inspected at its target.
func Load(dir string) (*Model, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(real)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}
	m := &Model{Dir: real, DTypes: map[string]*DTypeStat{}, BitsHistogram: map[int]int{}}
	if err := m.scanFiles(); err != nil {
		return nil, err
	}
	m.loadConfig()
	m.loadAuxConfigs()
	m.loadIndex()
	m.loadShards()
	m.buildTensors()
	m.buildGroups()
	m.detectFormat()
	// Host-capable warnings (duplicate shards, mixed pieces, a saturated
	// sum) settle here, before the caller scans. Rendering must not be the
	// first pass: that used to flip Partial after the verdict was fixed.
	m.HostCapable()
	m.NoteAbsurdLayers()
	return m, nil
}

func (m *Model) warnf(format string, a ...any) {
	if len(m.Warnings) >= maxWarnings {
		if !m.warningsTrunc {
			m.warningsTrunc = true
			m.Partial = true
			m.Warnings = append(m.Warnings, fileerr.String(fmt.Sprintf("too many warnings; truncated at %d", maxWarnings)))
		}
		return
	}
	m.Warnings = append(m.Warnings, fileerr.String(fmt.Sprintf(format, a...)))
}

func (m *Model) scanFiles() error {
	roots := m.allowedRoots()
	nfiles := 0
	return filepath.WalkDir(m.Dir, func(p string, d fs.DirEntry, err error) error {
		rel := p
		if r, relErr := filepath.Rel(m.Dir, p); relErr == nil {
			rel = r
		}
		// A per-entry failure (unreadable directory, vanished path) is a
		// warning that marks the inspection incomplete. It must not abort
		// the load: the error string contains the raw path.
		if err != nil {
			m.Partial = true
			m.warnf("%s: %v", rel, err)
			return nil
		}
		nfiles++
		if nfiles > maxFiles {
			m.Partial = true
			m.warnf("stopped after %d entries", maxFiles)
			return fs.SkipAll
		}
		depth := 0
		if rel != "." {
			depth = strings.Count(rel, string(filepath.Separator))
		}
		if d.IsDir() {
			if p != m.Dir && d.Name() == ".git" {
				return filepath.SkipDir
			}
			if depth > maxWalkDepth {
				m.Partial = true
				m.warnf("not descending past depth %d at %s", maxWalkDepth, rel)
				return filepath.SkipDir
			}
			return nil
		}
		if depth > maxWalkDepth {
			m.Partial = true
			m.warnf("skipping %s past depth %d", rel, maxWalkDepth)
			return nil
		}
		f := File{Name: rel, Symlink: d.Type()&fs.ModeSymlink != 0}
		if f.Symlink {
			real, err := filepath.EvalSymlinks(p)
			if err != nil {
				f.Broken = true
				m.warnf("%s: %v", rel, err)
				m.Files = append(m.Files, f)
				return nil
			}
			if !withinAny(real, roots) {
				f.Broken = true
				m.warnf("%s: symlink escapes the model directory", rel)
				m.Files = append(m.Files, f)
				return nil
			}
		}
		// Size for the file table. Content reads go through safeopen on one fd.
		st, err := os.Stat(p)
		if err != nil {
			f.Broken = true
			m.warnf("%s: %v", rel, err)
		} else if !st.Mode().IsRegular() {
			f.Broken = true
			m.warnf("%s: not a regular file", rel)
		} else {
			f.Size = st.Size()
			m.acc(&m.TotalSize, st.Size())
		}
		m.Files = append(m.Files, f)
		return nil
	})
}

// allowedRoots are the directories a snapshot symlink may point at:
// the model directory itself, and the sibling blobs/ directory when this
// is an HF snapshot (.../snapshots/<commit>).
func (m *Model) allowedRoots() []string {
	roots := []string{m.Dir}
	parent := filepath.Dir(m.Dir)
	if filepath.Base(parent) == "snapshots" {
		blobs := filepath.Join(filepath.Dir(parent), "blobs")
		if real, err := filepath.EvalSymlinks(blobs); err == nil {
			roots = append(roots, real)
		}
	}
	return roots
}

func withinAny(path string, roots []string) bool {
	for _, root := range roots {
		if safeopen.Within(root, path) {
			return true
		}
	}
	return false
}

func (m *Model) loadConfig() {
	p := filepath.Join(m.Dir, "config.json")
	raw, err := safeopen.ReadAll(p, maxSmallJSON, m.allowedRoots()...)
	if err != nil {
		// A missing name is "no config". A directory entry that cannot be
		// read, including a dangling symlink, is an incomplete inspection:
		// Stat of a broken symlink is ENOENT, which would otherwise look
		// like the file was never there.
		if errors.Is(err, fs.ErrNotExist) {
			if _, lerr := os.Lstat(p); lerr != nil {
				return
			}
			m.warnf("config.json: broken symlink: %v", err)
			m.noteUnchecked("config.json")
			return
		}
		// The file is there but cannot be read safely (device, FIFO, escape).
		// That is an incomplete inspection, not a clean pass.
		m.warnf("%v", err)
		m.noteUnchecked("config.json")
		return
	}
	m.ConfigRaw = raw
	cfg, err := parseConfigObject("config.json", raw)
	if err != nil {
		m.warnf("%s", err.Error())
		m.noteUnchecked("config.json")
		return
	}
	m.Config = cfg
	m.noteOutOfRangeNumbers(cfg, "")
	m.Quant = parseQuant(cfg)
}

func parseConfigObject(name string, raw []byte) (map[string]any, error) {
	v, err := jsonx.Unmarshal(raw)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %v", name, err)
	}
	cfg, ok := v.(map[string]any)
	if !ok || cfg == nil {
		return nil, fmt.Errorf("parsing %s: expected a JSON object", name)
	}
	return cfg, nil
}

func parseQuant(cfg map[string]any) *Quantization {
	scopes := []struct {
		prefix string
		m      map[string]any
	}{{"", cfg}, {"text_config.", subMap(cfg, "text_config")}}
	for _, s := range scopes {
		if s.m == nil {
			continue
		}
		for _, key := range []string{"quantization", "quantization_config"} {
			q := subMap(s.m, key)
			if q == nil {
				continue
			}
			out := &Quantization{Source: s.prefix + key, Overrides: map[string]QuantParams{}}
			out.Bits = intOf(q["bits"])
			out.GroupSize = intOf(q["group_size"])
			out.Mode = strOf(q["mode"])
			if out.Mode == "" {
				out.Mode = strOf(q["quant_method"])
			}
			// MLX stores per-module overrides as {"<module path>": {"bits":..,"group_size":..}}.
			for k, v := range q {
				if sm, ok := v.(map[string]any); ok {
					if _, has := sm["bits"]; has {
						out.Overrides[k] = QuantParams{Bits: intOf(sm["bits"]), GroupSize: intOf(sm["group_size"])}
					}
				}
			}
			out.NumOverrides = len(out.Overrides)
			return out
		}
	}
	return nil
}

func (m *Model) loadIndex() {
	// Read the directory and suffix-match. filepath.Glob treats "[" in the
	// directory name as a character class, so a folder like "Qwen3 [4bit]"
	// would lose its index and a folder named "m[" would be a pattern error.
	names, err := m.indexFileNames()
	if err != nil {
		m.Partial = true
		m.IndexErr = err
		m.warnf("%v", err)
		return
	}
	if len(names) == 0 {
		return
	}
	if len(names) > 1 {
		m.IndexErr = fmt.Errorf("%w: %s", ErrMultipleIndex, strings.Join(names, ", "))
		m.warnf("%v", m.IndexErr)
		return
	}
	p := filepath.Join(m.Dir, names[0])
	b, err := safeopen.ReadAll(p, maxSmallJSON, m.allowedRoots()...)
	if err != nil {
		m.IndexErr = err
		m.warnf("%v", err)
		return
	}
	idx, err := parseIndex(filepath.Base(p), b)
	if err != nil {
		m.IndexErr = err
		m.warnf("%v", err)
		return
	}
	if idx.badTotalSize {
		// Zero would look like "no total_size", and the mismatch check
		// skips that. The declaration cannot be verified, so this is partial.
		m.Partial = true
		m.warnf("%s: metadata.total_size is not a non-negative int64", idx.File)
	}
	m.Index = idx
}

// indexFileNames lists top-level *.safetensors.index.json names.
// The conventional model.safetensors.index.json is included by the same
// suffix check; more than one match is left for the caller to reject.
// Names are not globbed.
func (m *Model) indexFileNames() ([]string, error) {
	entries, err := os.ReadDir(m.Dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".safetensors.index.json") {
			continue
		}
		if !filepath.IsLocal(name) || strings.ContainsRune(name, os.PathSeparator) {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func parseIndex(name string, b []byte) (*Index, error) {
	v, err := jsonx.Unmarshal(b)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", name, err)
	}
	obj, ok := v.(map[string]any)
	if !ok || obj == nil {
		return nil, fmt.Errorf("parsing %s: expected a JSON object", name)
	}
	rawMap, exists := obj["weight_map"]
	wm, isMap := rawMap.(map[string]any)
	// A missing key, JSON null, and {"weight_map":{}} all have no entries.
	// An empty map must not become a usable index: shard discovery would
	// trust it and skip every file on disk.
	if !exists || rawMap == nil || (isMap && len(wm) == 0) {
		return nil, fmt.Errorf("%s: %w", name, ErrIndexWeightMap)
	}
	if !isMap {
		return nil, fmt.Errorf("parsing %s: weight_map must be an object", name)
	}
	weight := make(map[string]string, len(wm))
	for k, val := range wm {
		s, ok := val.(string)
		if !ok {
			return nil, fmt.Errorf("parsing %s: weight_map %q is not a string", name, k)
		}
		weight[k] = s
	}
	idx := &Index{File: name, WeightMap: weight}
	if meta, ok := obj["metadata"].(map[string]any); ok {
		if ts, ok := meta["total_size"]; ok {
			n, ok := fitInt64(ts)
			if !ok {
				idx.badTotalSize = true
			} else {
				idx.TotalSize = n
			}
		}
	}
	return idx, nil
}

// fitInt64 converts a JSON number to int64 when the value is finite and in range.
// Out-of-range floats are rejected instead of truncated into a wrapped int64.
func fitInt64(v any) (int64, bool) {
	f, ok := jsonx.FitFloat(v)
	if !ok || f < 0 || f >= float64(uint64(1)<<63) {
		return 0, false
	}
	return int64(f), true
}

func mulNonNeg(a, b int64) (int64, bool) {
	if a < 0 || b < 0 {
		return 0, false
	}
	if a == 0 || b == 0 {
		return 0, true
	}
	if a > math.MaxInt64/b {
		return 0, false
	}
	return a * b, true
}

func (m *Model) acc(dst *int64, n int64) {
	if n < 0 || *dst < 0 || *dst > math.MaxInt64-n {
		m.noteOverflow()
		return
	}
	*dst += n
}

func (m *Model) noteOverflow() {
	if m.overflowed {
		return
	}
	m.overflowed = true
	m.Partial = true
	m.warnf("displayed size overflowed int64 and was not accumulated")
}

func (m *Model) loadShards() {
	byName := make(map[string]File, len(m.Files))
	names := map[string]struct{}{}
	for _, f := range m.Files {
		byName[f.Name] = f
		if strings.HasSuffix(f.Name, ".safetensors") {
			names[f.Name] = struct{}{}
		}
	}
	// Names from the index are included even when the file is absent, so a
	// missing or non-local shard is reported. On-disk .safetensors files
	// are always included, including ones the index does not mention.
	if m.Index != nil {
		for _, shard := range m.Index.WeightMap {
			names[shard] = struct{}{}
		}
	}
	ordered := make([]string, 0, len(names))
	for n := range names {
		ordered = append(ordered, n)
	}
	sort.Strings(ordered)
	roots := m.allowedRoots()
	for _, n := range ordered {
		s := &Shard{Name: n}
		full, ok := joinWithin(m.Dir, n)
		if !ok {
			s.Err = fmt.Errorf("refusing non-local shard name %q", n)
			m.warnf("shard %s: %v", n, s.Err)
			m.addShard(s)
			continue
		}
		f, listed := byName[n]
		if !listed {
			s.Err = fmt.Errorf("not present in the model directory")
			m.warnf("shard %s: %v", n, s.Err)
			m.addShard(s)
			continue
		}
		if f.Broken {
			s.Err = fmt.Errorf("not a readable regular file inside the model directory")
			m.warnf("shard %s: %v", n, s.Err)
			m.addShard(s)
			continue
		}
		s.Path = full
		s.Header, s.Size, s.Err = safetensors.ReadFile(full, roots...)
		if s.Err != nil {
			m.warnf("shard %s: %v", n, s.Err)
		}
		m.addShard(s)
	}
}

// addShard records a shard. A shard that did not yield a header is an
// incomplete inspection, same as a walk cap or an unparsed config.
func (m *Model) addShard(s *Shard) {
	if s.Err != nil {
		m.Partial = true
	}
	m.Shards = append(m.Shards, s)
}

// localShardName accepts a relative file name and rejects absolute paths
// and any ".." segment. filepath.IsLocal is the check weight_map values
// have to pass before they are joined onto the model directory.
func localShardName(name string) bool {
	return name != "" && filepath.IsLocal(name)
}

func joinWithin(root, name string) (string, bool) {
	if !localShardName(name) {
		return "", false
	}
	full := filepath.Join(root, name)
	if !safeopen.Within(root, full) {
		return "", false
	}
	return full, true
}

func (m *Model) groupSizeFor(module string) int {
	if m.Quant != nil {
		if o, ok := m.Quant.Overrides[module]; ok && o.GroupSize > 0 {
			return o.GroupSize
		}
		if m.Quant.GroupSize > 0 {
			return m.Quant.GroupSize
		}
	}
	return 64 // MLX default
}

func storageBits(dtype string) int {
	switch dtype {
	case "U32", "I32":
		return 32
	case "U16", "I16":
		return 16
	case "U8", "I8":
		return 8
	}
	return 0
}

// inferQuant derives bits/element and the dequantized element count for an
// MLX-style packed weight from its own shape and its scales' shape.
func inferQuant(w, scales *Tensor, groupSize int) (bits int, logical int64) {
	if len(w.Shape) == 0 || len(scales.Shape) == 0 || groupSize <= 0 {
		return 0, 0
	}
	storage := storageBits(w.DType)
	if storage == 0 {
		return 0, 0
	}
	packed := w.Shape[len(w.Shape)-1]
	in, ok := mulNonNeg(scales.Shape[len(scales.Shape)-1], int64(groupSize))
	if !ok || packed == 0 || in == 0 {
		return 0, 0
	}
	total, ok := mulNonNeg(packed, int64(storage))
	if !ok || total%in != 0 {
		return 0, 0
	}
	bits64 := total / in
	if bits64 > int64(math.MaxInt) {
		return 0, 0
	}
	perPacked := w.NumElements() / packed
	logical, ok = mulNonNeg(perPacked, in)
	if !ok {
		return 0, 0
	}
	return int(bits64), logical
}

func (m *Model) buildTensors() {
	byName := map[string]*Tensor{}
	for _, s := range m.Shards {
		if s.Header == nil {
			continue
		}
		for _, ti := range s.Header.Tensors {
			key, kind, layer := Classify(ti.Name)
			t := &Tensor{TensorInfo: ti, Shard: s.Name, Group: key, Kind: kind, Layer: layer,
				Role: RolePlain, LogicalParams: ti.NumElements()}
			m.Tensors = append(m.Tensors, t)
			byName[ti.Name] = t
		}
	}
	for _, t := range m.Tensors {
		base, ok := strings.CutSuffix(t.Name, ".weight")
		if !ok {
			continue
		}
		scales := byName[base+".scales"]
		if scales == nil {
			continue
		}
		bits, logical := inferQuant(t, scales, m.groupSizeFor(base))
		if bits == 0 {
			continue
		}
		t.Role, t.Bits, t.LogicalParams = RoleQuantWeight, bits, logical
		scales.Role, scales.LogicalParams = RoleQuantScales, 0
		if b := byName[base+".biases"]; b != nil {
			b.Role, b.LogicalParams = RoleQuantBiases, 0
		}
	}
	for _, t := range m.Tensors {
		d := m.DTypes[t.DType]
		if d == nil {
			d = &DTypeStat{}
			m.DTypes[t.DType] = d
		}
		d.Tensors++
		m.acc(&d.Elements, t.NumElements())
		m.acc(&d.Bytes, t.ByteLen())
		m.acc(&m.TotalElements, t.NumElements())
		m.acc(&m.TotalLogicalParams, t.LogicalParams)
		m.acc(&m.TensorBytes, t.ByteLen())
		if t.Role == RoleQuantWeight {
			m.BitsHistogram[t.Bits]++
		}
	}
}

func (m *Model) buildGroups() {
	byKey := map[string]*Group{}
	for _, t := range m.Tensors {
		g := byKey[t.Group]
		if g == nil {
			g = &Group{Key: t.Group, Kind: t.Kind, Layer: t.Layer}
			byKey[t.Group] = g
			m.Groups = append(m.Groups, g)
		}
		g.Tensors = append(g.Tensors, t)
		m.acc(&g.Elements, t.NumElements())
		m.acc(&g.LogicalParams, t.LogicalParams)
		m.acc(&g.Bytes, t.ByteLen())
		if IsExpertTensor(t.Name) {
			g.HasExperts = true
		}
		if t.Role == RoleQuantWeight {
			g.QuantizedTensors++
		}
	}
	for _, g := range m.Groups {
		sort.Slice(g.Tensors, func(i, j int) bool { return g.Tensors[i].Name < g.Tensors[j].Name })
	}
	sort.SliceStable(m.Groups, func(i, j int) bool {
		a, b := m.Groups[i], m.Groups[j]
		if kindOrder[a.Kind] != kindOrder[b.Kind] {
			return kindOrder[a.Kind] < kindOrder[b.Kind]
		}
		if a.Layer != b.Layer {
			return a.Layer < b.Layer
		}
		return a.Key < b.Key
	})
}

func (m *Model) detectFormat() {
	var st, gguf int
	for _, f := range m.Files {
		switch {
		case strings.HasSuffix(f.Name, ".safetensors"):
			st++
		case strings.HasSuffix(f.Name, ".gguf"):
			gguf++
		}
	}
	formats := map[string]bool{}
	for _, s := range m.Shards {
		if s.Header != nil && s.Header.Metadata["format"] != "" {
			formats[s.Header.Metadata["format"]] = true
		}
	}
	switch {
	case st > 0:
		m.Format = "safetensors"
		if len(formats) > 0 {
			var keys []string
			for k := range formats {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			m.Format += " (" + strings.Join(keys, ",") + ")"
		}
	case gguf > 0:
		m.Format = "gguf (not supported in v1)"
	default:
		m.Format = "unknown"
	}
}

// LayerCount is the number of numbered decoder layers found in tensor names.
func (m *Model) LayerCount() (layers, withExperts int) {
	for _, g := range m.Groups {
		if g.Kind == KindLayer {
			layers++
			if g.HasExperts {
				withExperts++
			}
		}
	}
	return
}
