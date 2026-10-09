// Package model loads a model directory's metadata: file sizes via stat,
// small JSON files, and safetensors headers. It never reads tensor data.
package model

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mwelch/model-inspector/internal/safetensors"
)

// maxSmallJSON caps config/index reads; anything bigger is refused.
const maxSmallJSON = 64 << 20

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
	Quant     *Quantization
	Index     *Index
	// IndexErr is set when an index file is present but unreadable.
	// Index stays nil so shard discovery can still see the .safetensors files.
	IndexErr error `json:"-"`

	Shards  []*Shard
	Tensors []*Tensor
	Groups  []*Group

	DTypes             map[string]*DTypeStat
	BitsHistogram      map[int]int
	TotalElements      int64
	TotalLogicalParams int64
	TensorBytes        int64

	Warnings []string
}

// Load inspects dir. Errors in individual shards are recorded, not fatal.
func Load(dir string) (*Model, error) {
	fi, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}
	m := &Model{Dir: dir, DTypes: map[string]*DTypeStat{}, BitsHistogram: map[int]int{}}
	if err := m.scanFiles(); err != nil {
		return nil, err
	}
	m.loadConfig()
	m.loadIndex()
	m.loadShards()
	m.buildTensors()
	m.buildGroups()
	m.detectFormat()
	return m, nil
}

func (m *Model) warnf(format string, a ...any) {
	m.Warnings = append(m.Warnings, fmt.Sprintf(format, a...))
}

func (m *Model) scanFiles() error {
	return filepath.WalkDir(m.Dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != m.Dir && d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(m.Dir, p)
		f := File{Name: rel, Symlink: d.Type()&fs.ModeSymlink != 0}
		// os.Stat follows HF snapshot symlinks into blobs/: metadata only, no data read.
		if st, err := os.Stat(p); err != nil {
			f.Broken = true
			m.warnf("%s: %v", rel, err)
		} else {
			f.Size = st.Size()
			m.TotalSize += st.Size()
		}
		m.Files = append(m.Files, f)
		return nil
	})
}

func readSmallJSON(path string, v any) ([]byte, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if st.Size() > maxSmallJSON {
		return nil, fmt.Errorf("%s is %d bytes; refusing to read JSON over %d bytes", filepath.Base(path), st.Size(), maxSmallJSON)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return b, fmt.Errorf("parsing %s: %w", filepath.Base(path), err)
	}
	return b, nil
}

func (m *Model) loadConfig() {
	p := filepath.Join(m.Dir, "config.json")
	if _, err := os.Stat(p); err != nil {
		return
	}
	var cfg map[string]any
	raw, err := readSmallJSON(p, &cfg)
	m.ConfigRaw = raw
	if err != nil {
		m.warnf("%v", err)
		return
	}
	m.Config = cfg
	m.Quant = parseQuant(cfg)
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
	p := filepath.Join(m.Dir, "model.safetensors.index.json")
	if _, err := os.Stat(p); err != nil {
		matches, _ := filepath.Glob(filepath.Join(m.Dir, "*.safetensors.index.json"))
		if len(matches) == 0 {
			return
		}
		p = matches[0]
	}
	var raw struct {
		Metadata  map[string]any    `json:"metadata"`
		WeightMap map[string]string `json:"weight_map"`
	}
	if _, err := readSmallJSON(p, &raw); err != nil {
		m.IndexErr = err
		m.warnf("%v", err)
		return
	}
	if raw.WeightMap == nil {
		m.IndexErr = fmt.Errorf("%s: missing weight_map", filepath.Base(p))
		m.warnf("%v", m.IndexErr)
		return
	}
	idx := &Index{File: filepath.Base(p), WeightMap: raw.WeightMap}
	if v, ok := raw.Metadata["total_size"]; ok {
		idx.TotalSize = int64(floatOf(v))
	}
	m.Index = idx
}

func (m *Model) loadShards() {
	var names []string
	if m.Index != nil {
		seen := map[string]bool{}
		for _, s := range m.Index.WeightMap {
			if !seen[s] {
				seen[s] = true
				names = append(names, s)
			}
		}
	} else {
		for _, f := range m.Files {
			if strings.HasSuffix(f.Name, ".safetensors") {
				names = append(names, f.Name)
			}
		}
	}
	sort.Strings(names)
	for _, n := range names {
		s := &Shard{Name: n, Path: filepath.Join(m.Dir, n)}
		if st, err := os.Stat(s.Path); err != nil {
			s.Err = err
		} else {
			s.Size = st.Size()
			s.Header, s.Err = safetensors.ReadFile(s.Path)
		}
		if s.Err != nil {
			m.warnf("shard %s: %v", n, s.Err)
		}
		m.Shards = append(m.Shards, s)
	}
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
	in := scales.Shape[len(scales.Shape)-1] * int64(groupSize)
	if packed == 0 || in == 0 {
		return 0, 0
	}
	total := packed * int64(storage)
	if total%in != 0 {
		return 0, 0
	}
	return int(total / in), w.NumElements() / packed * in
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
		d.Elements += t.NumElements()
		d.Bytes += t.ByteLen()
		m.TotalElements += t.NumElements()
		m.TotalLogicalParams += t.LogicalParams
		m.TensorBytes += t.ByteLen()
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
		g.Elements += t.NumElements()
		g.LogicalParams += t.LogicalParams
		g.Bytes += t.ByteLen()
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
