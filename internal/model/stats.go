package model

import (
	"fmt"
	"math"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

const (
	// GiB is 2^30. Host-capable memory lines use this threshold.
	GiB int64 = 1 << 30

	// RuleLookupTable is the heuristic id for host-RAM-capable tables.
	RuleLookupTable = "lookup_table_v1"
)

// StorageClass is one slice of tensor bytes.
type StorageClass struct {
	Class string
	Bytes int64
	Pct   float64
}

// StorageBreakdown groups tensor bytes by storage class.
// Classes are q{N}_packed, q_scales_biases, plain_{dtype}, and other.
// The result is sorted by class name so callers can render a stable bar.
// A sum that does not fit in int64 is not accumulated and marks the
// inspection partial, so a wrapped negative size cannot reach the bar.
func (m *Model) StorageBreakdown() []StorageClass {
	if m == nil {
		return nil
	}
	acc := map[string]int64{}
	var other int64
	for _, t := range m.Tensors {
		n := t.ByteLen()
		switch t.Role {
		case RoleQuantWeight:
			if t.Bits > 0 {
				m.addClass(acc, fmt.Sprintf("q%d_packed", t.Bits), n)
			} else {
				addInto(m, &other, n)
			}
		case RoleQuantScales, RoleQuantBiases:
			m.addClass(acc, "q_scales_biases", n)
		case RolePlain:
			dt := strings.ToLower(t.DType)
			if dt == "" {
				addInto(m, &other, n)
			} else {
				m.addClass(acc, "plain_"+dt, n)
			}
		default:
			addInto(m, &other, n)
		}
	}
	if other > 0 {
		m.addClass(acc, "other", other)
	}
	keys := make([]string, 0, len(acc))
	for k := range acc {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]StorageClass, 0, len(keys))
	for _, k := range keys {
		out = append(out, StorageClass{Class: k, Bytes: acc[k], Pct: pct(acc[k], m.TensorBytes)})
	}
	return out
}

// MergeStorage caps a breakdown at max segments. The smallest leftover
// classes fold into "other". max < 1 returns the input.
// m may be nil. When it is set, a sum that overflows int64 marks the
// inspection partial and is not accumulated.
func MergeStorage(m *Model, classes []StorageClass, max int) []StorageClass {
	if max < 1 || len(classes) <= max {
		return classes
	}
	sorted := append([]StorageClass(nil), classes...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Bytes == sorted[j].Bytes {
			return sorted[i].Class < sorted[j].Class
		}
		return sorted[i].Bytes > sorted[j].Bytes
	})
	keep := max
	// Reserve one slot when the tail has to become "other" and "other"
	// is not already among the kept classes.
	head := sorted[:keep]
	tail := sorted[keep:]
	var rest int64
	keptOther := false
	for _, c := range head {
		if c.Class == "other" {
			keptOther = true
		}
	}
	if !keptOther {
		head = sorted[:keep-1]
		tail = sorted[keep-1:]
	}
	for _, c := range tail {
		addInto(m, &rest, c.Bytes)
	}
	out := append([]StorageClass(nil), head...)
	if rest > 0 {
		found := false
		for i := range out {
			if out[i].Class == "other" {
				addInto(m, &out[i].Bytes, rest)
				found = true
			}
		}
		if !found {
			out = append(out, StorageClass{Class: "other", Bytes: rest})
		}
	}
	total := int64(0)
	for _, c := range out {
		addInto(m, &total, c.Bytes)
	}
	for i := range out {
		out[i].Pct = pct(out[i].Bytes, total)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Class < out[j].Class })
	return out
}

// MemoryEstimate is the weights-only hint. It is not a VRAM plan.
type MemoryEstimate struct {
	WeightsBytes        int64
	TextOnlyBytes       int64
	TextOnlyKnown       bool
	HostCapableBytes    int64
	AcceleratorMinBytes int64
	UnifiedMemory       bool
	Note                string
	HostCapableNote     string
	// HostCapablePartial is true when the host-capable byte total is a
	// lower bound: the inspection is partial, or an index names pieces of
	// a table that were not loaded. It is not a JSON field.
	HostCapablePartial bool
}

const (
	MemoryNote      = "weights only; excludes KV cache, activations, runtime overhead"
	HostCapableNote = "heuristic lookup_table_v1; runtime support varies; no saving on unified memory"
)

// MemoryEstimate sums header byte ranges. text-only subtracts vision and
// MTP groups. Host-capable bytes include the q-scales and q-biases of
// tables flagged by lookup_table_v1.
func (m *Model) MemoryEstimate() MemoryEstimate {
	est := MemoryEstimate{
		WeightsBytes:    m.TensorBytes,
		Note:            MemoryNote,
		HostCapableNote: HostCapableNote,
		UnifiedMemory:   m.UnifiedMemory(),
	}
	var side int64
	var sawSide bool
	for _, g := range m.Groups {
		if g.Kind == KindVision || g.Kind == KindMTP {
			addInto(m, &side, g.Bytes)
			sawSide = true
		}
	}
	est.TextOnlyKnown = sawSide
	if m.TensorBytes >= side {
		est.TextOnlyBytes = m.TensorBytes - side
	}
	for _, t := range m.HostCapable() {
		addInto(m, &est.HostCapableBytes, t.Bytes)
		if t.incomplete {
			est.HostCapablePartial = true
		}
	}
	if m.Partial {
		est.HostCapablePartial = true
	}
	// weights − host is a lower bound on accelerator bytes only when the
	// host total is exact. When it is a lower bound, a larger true host
	// total (still inside the same weight bytes) would make the remainder
	// smaller, so the subtraction is not a lower bound. The only figure
	// that stays a lower bound is 0. The identity screen omits the GPU
	// line in that case rather than printing "≥ 0".
	if est.HostCapablePartial {
		est.AcceleratorMinBytes = 0
	} else {
		est.AcceleratorMinBytes = est.WeightsBytes - est.HostCapableBytes
		if est.AcceleratorMinBytes < 0 {
			// Display floor only. Callers that check host bytes against weight
			// bytes must use HostCapableBytes, not this clamped result.
			est.AcceleratorMinBytes = 0
		}
	}
	return est
}

// AppleSilicon reports whether this host's CPU and GPU share one memory pool.
// Golden tests set it false so their output does not depend on the host.
var AppleSilicon = runtime.GOOS == "darwin" && runtime.GOARCH == "arm64"

// UnifiedMemory is true for MLX checkpoints and for Apple silicon, where
// CPU and GPU share one pool.
func (m *Model) UnifiedMemory() bool {
	if m != nil && formatVariant(m.Format) == "mlx" {
		return true
	}
	return AppleSilicon
}

// HostTensor is one lookup table flagged by lookup_table_v1.
// Bytes includes the table's q-scales and q-biases.
// A table stored as .shard_N pieces is one entry: Name is the shared stem,
// and Bytes and ParamsEst are the sums across the pieces.
type HostTensor struct {
	Name      string
	Group     string
	Bytes     int64
	ParamsEst int64
	Rule      string
	// sharded is true when this entry sums .shard_N pieces rather than one
	// weight tensor. It is not part of the JSON document.
	sharded bool
	// stem is the shared lookup stem of a sharded table. Name stays equal
	// to stem unless a tensor is itself named that stem; then Name gains a
	// suffix so the two JSON entries are not the same string. Callers that
	// match pieces use stem, not Name.
	stem string
	// incomplete is true when this table's bytes are a lower bound.
	// It is not part of the JSON document; callout text carries the marker.
	incomplete bool
}

// shardAcc collects the pieces of one stem.shard_N table.
type shardAcc struct {
	stem   string
	group  string
	dtype  string
	cols   int64
	have   bool
	bad    bool
	seen   map[string]struct{}
	pieces []*Tensor
}

// HostCapable returns lookup tables the heuristic says can stay in host RAM.
// Pieces named stem.shard_N (plus .weight) are one logical table when they
// share a dtype and a column count. Each piece still has to be a 2-D table
// of at least 1024 rows. Shard indexes are compared numerically, so
// shard_1 and shard_01 are one piece. Bytes include that piece's .scales
// and .biases, and each tensor is counted once. Orphan scales and biases
// are not tables. One shard of the PLE n-gram table is under the 1 GiB /
// 10% callout thresholds on its own.
func (m *Model) HostCapable() []HostTensor {
	if m == nil {
		return nil
	}
	if !m.hostReady {
		m.hostCache = m.gatherHostCapable()
		m.hostReady = true
	}
	if !m.Partial {
		return m.hostCache
	}
	// Partial can still flip later (a saturated group total while the
	// report is built). Stamp the lower-bound marker on a copy so the
	// cached pass is not recomputed and cannot warn again.
	already := true
	for i := range m.hostCache {
		if !m.hostCache[i].incomplete {
			already = false
			break
		}
	}
	if already {
		return m.hostCache
	}
	out := append([]HostTensor(nil), m.hostCache...)
	for i := range out {
		out[i].incomplete = true
	}
	return out
}

// gatherHostCapable is the single host-capable pass. HostCapable caches it.
func (m *Model) gatherHostCapable() []HostTensor {
	byName := make(map[string]*Tensor, len(m.Tensors))
	loaded := make(map[string]struct{}, len(m.Tensors))
	for _, t := range m.Tensors {
		if t == nil {
			continue
		}
		byName[t.Name] = t
		loaded[t.Name] = struct{}{}
	}
	tied := m.tiedWordEmbeddings() && !m.hasKind(KindLMHead)
	used := map[string]struct{}{}
	var plain []*Tensor
	aggs := map[string]*shardAcc{}
	var order []string
	for _, t := range m.Tensors {
		if t == nil {
			continue
		}
		if t.Role != RolePlain && t.Role != RoleQuantWeight {
			continue
		}
		if t.Kind == KindVision {
			continue
		}
		// Scales and biases are companions of a weight, never their own table.
		if hasSuffixFold(t.Name, ".scales") || hasSuffixFold(t.Name, ".biases") {
			continue
		}
		stem, sharded := lookupStem(t.Name)
		nameForHost := t.Name
		if sharded {
			nameForHost = stem
		}
		if !hostName(nameForHost) {
			continue
		}
		if len(t.Shape) != 2 || t.Shape[0] < 1024 {
			continue
		}
		if tied && tokenEmbedSegment(nameForHost) {
			continue
		}
		if !sharded {
			plain = append(plain, t)
			continue
		}
		a := aggs[stem]
		if a == nil {
			a = &shardAcc{stem: stem, group: t.Group, seen: map[string]struct{}{}}
			aggs[stem] = a
			order = append(order, stem)
		}
		cols := t.Shape[len(t.Shape)-1]
		if !a.have {
			a.dtype, a.cols, a.have = t.DType, cols, true
		} else if foldASCII(a.dtype) != foldASCII(t.DType) || a.cols != cols {
			a.bad = true
		}
		key, ok := shardOrdinal(t.Name)
		if !ok {
			a.bad = true
			continue
		}
		if _, dup := a.seen[key]; dup {
			m.warnOnce("lookup table %s: duplicate shard index %s ignored", a.stem, key)
			continue
		}
		a.seen[key] = struct{}{}
		a.pieces = append(a.pieces, t)
	}
	out := make([]HostTensor, 0, len(plain)+len(order))
	for _, t := range plain {
		ht := HostTensor{Name: t.Name, Group: t.Group, Rule: RuleLookupTable}
		addTableBytes(m, byName, used, &ht, t)
		if m.tableIncomplete(t.Name, false, loaded) {
			ht.incomplete = true
		}
		out = append(out, ht)
	}
	for _, stem := range order {
		a := aggs[stem]
		if a.bad {
			m.warnOnce("lookup table %s: pieces differ in dtype or column count and were not merged", stem)
			continue
		}
		ht := HostTensor{Name: stem, stem: stem, Group: a.group, Rule: RuleLookupTable, sharded: true}
		for _, t := range a.pieces {
			addTableBytes(m, byName, used, &ht, t)
		}
		if m.tableIncomplete(stem, true, loaded) {
			ht.incomplete = true
		}
		out = append(out, ht)
	}
	if m.Partial {
		for i := range out {
			out[i].incomplete = true
		}
	}
	disambiguateHostNames(out)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return !out[i].sharded && out[j].sharded
	})
	return out
}

// shardedNameSuffix marks a summed shard table whose stem is also a tensor
// name. The suffix is not itself a ".shard_<digits>" tail, so lookupStem
// does not treat the JSON name as another shard.
const shardedNameSuffix = " (sharded)"

// disambiguateHostNames keeps every host_capable_tensors name unique.
// A plain tensor keeps its real name. A sharded table keeps the stem unless
// that string is already taken, in which case the table is "stem (sharded)".
func disambiguateHostNames(out []HostTensor) {
	used := map[string]struct{}{}
	for i := range out {
		if out[i].sharded {
			continue
		}
		used[out[i].Name] = struct{}{}
	}
	for i := range out {
		if !out[i].sharded {
			continue
		}
		name := out[i].Name
		if _, clash := used[name]; clash {
			name += shardedNameSuffix
		}
		for n := 2; ; n++ {
			if _, ok := used[name]; !ok {
				break
			}
			name = fmt.Sprintf("%s%s %d", out[i].stem, shardedNameSuffix, n)
		}
		used[name] = struct{}{}
		out[i].Name = name
	}
}

// hostStem is the name pieces are matched on. Sharded entries keep it even
// after the JSON name is disambiguated.
func hostStem(h HostTensor) string {
	if h.stem != "" {
		return h.stem
	}
	return h.Name
}

// addTableBytes adds one weight and, once, its .scales and .biases.
func addTableBytes(m *Model, byName map[string]*Tensor, used map[string]struct{}, ht *HostTensor, t *Tensor) {
	if _, ok := used[t.Name]; ok {
		return
	}
	used[t.Name] = struct{}{}
	addInto(m, &ht.ParamsEst, t.LogicalParams)
	addInto(m, &ht.Bytes, t.ByteLen())
	if !hasSuffixFold(t.Name, ".weight") {
		return
	}
	base := stripWeightSuffix(t.Name)
	for _, suf := range []string{".scales", ".biases"} {
		s := byName[base+suf]
		if s == nil {
			continue
		}
		if _, ok := used[s.Name]; ok {
			continue
		}
		used[s.Name] = struct{}{}
		addInto(m, &ht.Bytes, s.ByteLen())
	}
}

// tableIncomplete reports whether the index names a piece of this table
// that was not loaded. For an unsharded table, stem is the weight name.
func (m *Model) tableIncomplete(stem string, sharded bool, loaded map[string]struct{}) bool {
	if m == nil || m.Index == nil {
		return false
	}
	base := stem
	if !sharded {
		base = stripWeightSuffix(stem)
	}
	for name := range m.Index.WeightMap {
		got, isShard := lookupStem(name)
		if sharded {
			if !isShard || got != stem {
				continue
			}
		} else if name != stem && got != base {
			continue
		}
		if _, ok := loaded[name]; !ok {
			return true
		}
	}
	return false
}

// NotableParams is the largest parameter contributor. Shard pieces of one
// lookup table are summed first, so a split n-gram table can cross the
// 10% line a single shard never reaches. ngram is true when that
// contributor is an n-gram embedding table.
func (m *Model) NotableParams() (name string, params int64, ngram bool) {
	if m == nil {
		return "", 0, false
	}
	covered := map[*Tensor]bool{}
	consider := func(n int64, tensorName string) {
		if n > params {
			params = n
			name = tensorName
			ngram = ngramName(tensorName)
		}
	}
	for _, h := range m.HostCapable() {
		if !h.sharded {
			continue
		}
		consider(h.ParamsEst, h.Name)
		key := hostStem(h)
		for _, t := range m.Tensors {
			if t == nil {
				continue
			}
			stem, sharded := lookupStem(t.Name)
			if sharded && stem == key {
				covered[t] = true
			}
		}
	}
	for _, t := range m.Tensors {
		if t == nil || covered[t] {
			continue
		}
		consider(t.LogicalParams, t.Name)
	}
	return name, params, ngram
}

// Callout is one identity finding. Kind is the v1 JSON enum.
type Callout struct {
	Kind    string
	Group   string
	Tensor  string
	Message string
}

// Outliers returns identity callouts, most serious first.
// Host-capable callouts are included when the flagged bytes are at least
// 1 GiB or one table is at least 5% of weight bytes.
func (m *Model) Outliers() []Callout {
	if m == nil {
		return nil
	}
	var out []Callout
	layers := make([]*Group, 0)
	for _, g := range m.Groups {
		if g.Kind == KindLayer {
			layers = append(layers, g)
		}
	}
	sizes := make([]int64, len(layers))
	for i, g := range layers {
		sizes[i] = g.Bytes
	}
	med := median(sizes)
	mentioned := map[string]bool{}
	for _, g := range layers {
		if !exceeds3x(g.Bytes, med) {
			continue
		}
		largest, share := largestTensor(g)
		msg := fmt.Sprintf("%s is %s, %.1f%% of weights", g.Key, HumanBytes(g.Bytes), pct(g.Bytes, m.TensorBytes))
		tensor := ""
		if largest != nil && share >= 50 {
			tensor = largest.Name
			mentioned[largest.Name] = true
			msg += ": " + tensorPhrase(largest)
		}
		out = append(out, Callout{Kind: "size_outlier", Group: g.Key, Tensor: tensor, Message: msg})
	}

	host := m.HostCapable()
	var hostBytes int64
	var maxShare float64
	aggregated := map[string]struct{}{}
	for _, h := range host {
		if h.sharded {
			aggregated[hostStem(h)] = struct{}{}
		}
		addInto(m, &hostBytes, h.Bytes)
		if m.TensorBytes > 0 {
			if s := float64(h.Bytes) / float64(m.TensorBytes); s > maxShare {
				maxShare = s
			}
		}
	}
	if hostBytes >= GiB || maxShare >= 0.05 {
		for _, h := range host {
			msg := hostMessage(h)
			out = append(out, Callout{Kind: "host_capable", Group: h.Group, Tensor: h.Name, Message: msg})
		}
	}

	for _, t := range m.Tensors {
		if stem, sharded := lookupStem(t.Name); sharded {
			if _, ok := aggregated[stem]; ok {
				continue
			}
		}
		if m.TensorBytes <= 0 || t.ByteLen()*10 < m.TensorBytes {
			continue
		}
		if mentioned[t.Name] {
			continue
		}
		out = append(out, Callout{
			Kind: "big_tensor", Group: t.Group, Tensor: t.Name,
			Message: fmt.Sprintf("%s is %s, %.1f%% of weights", ShortName(t.Name), HumanBytes(t.ByteLen()), pct(t.ByteLen(), m.TensorBytes)),
		})
	}
	for _, h := range host {
		if !h.sharded || mentioned[h.Name] {
			continue
		}
		if m.TensorBytes <= 0 || (h.Bytes <= math.MaxInt64/10 && h.Bytes*10 < m.TensorBytes) {
			continue
		}
		msg := fmt.Sprintf("%s is %s, %.1f%% of weights", ShortName(h.Name), HumanBytes(h.Bytes), pct(h.Bytes, m.TensorBytes))
		if ngramName(h.Name) {
			msg += fmt.Sprintf(": ~%s-param n-gram embedding table", HumanCount(h.ParamsEst))
		}
		if h.incomplete {
			msg += " (incomplete)"
		}
		out = append(out, Callout{Kind: "big_tensor", Group: h.Group, Tensor: h.Name, Message: msg})
	}

	quantBytes := int64(0)
	for _, t := range m.Tensors {
		if t.Role == RoleQuantWeight {
			addInto(m, &quantBytes, t.ByteLen())
		}
	}
	mostlyQuant := m.TensorBytes > 0 && quantBytes*2 >= m.TensorBytes
	if mostlyQuant {
		for _, g := range m.Groups {
			dt, plain := dominantPlain(g)
			if plain == 0 || g.Bytes == 0 || plain*2 < g.Bytes {
				continue
			}
			if !float16or32(dt) {
				continue
			}
			out = append(out, Callout{
				Kind: "precision_outlier", Group: g.Key,
				Message: fmt.Sprintf("%s is %s, not quantized: %s", groupTitle(g), dt, HumanBytes(g.Bytes)),
			})
		}
	}

	for _, g := range m.Groups {
		switch g.Kind {
		case KindMTP:
			out = append(out, Callout{
				Kind: "structural", Group: g.Key,
				Message: fmt.Sprintf("MTP layer present (%s): used only for speculative decoding", HumanBytes(g.Bytes)),
			})
		case KindVision:
			if textArch(m) && !hasCallout(out, "precision_outlier", g.Key) {
				out = append(out, Callout{
					Kind: "structural", Group: g.Key,
					Message: fmt.Sprintf("vision tower present (%s)", HumanBytes(g.Bytes)),
				})
			}
		case KindOther:
			if m.TensorBytes > 0 && g.Bytes*100 > m.TensorBytes {
				out = append(out, Callout{
					Kind: "structural", Group: g.Key,
					Message: fmt.Sprintf("%s is %.1f%% of weights", g.Key, pct(g.Bytes, m.TensorBytes)),
				})
			}
		}
	}
	if n := len(m.Warnings); n > 0 {
		out = append(out, Callout{Kind: "load_warnings", Message: fmt.Sprintf("%d load warnings", n)})
	}
	return out
}

// LayerTypes returns one attention type per decoder layer.
// Order: config layer_types, then full_attention_interval, then tensor names.
// full_attention_interval treats layer i as full attention when
// (i+1) % interval == 0, and linear attention otherwise.
func (m *Model) LayerTypes() []string {
	if m == nil {
		return nil
	}
	if raw := m.layerTypeList(); len(raw) > 0 {
		return raw
	}
	n, _ := m.LayerCount()
	if n == 0 {
		n = m.configInt("num_hidden_layers")
	}
	n = m.clampLayerSlots(n)
	if interval := m.configInt("full_attention_interval"); interval > 0 && n > 0 {
		out := make([]string, n)
		for i := 0; i < n; i++ {
			if (i+1)%interval == 0 {
				out[i] = "full_attention"
			} else {
				out[i] = "linear_attention"
			}
		}
		return out
	}
	if n == 0 {
		return nil
	}
	out := make([]string, n)
	for _, g := range m.Groups {
		if g.Kind != KindLayer || g.Layer < 0 || g.Layer >= n {
			continue
		}
		out[g.Layer] = inferLayerType(g)
	}
	return out
}

// StorageLabel is the short dtype label for a group (q4, q4+bf16, bf16).
func (g *Group) StorageLabel() string {
	if g == nil || len(g.Tensors) == 0 {
		return "—"
	}
	bitsSet := map[int]struct{}{}
	var plainBytes int64
	plainBy := map[string]int64{}
	quant := false
	for _, t := range g.Tensors {
		switch t.Role {
		case RoleQuantWeight:
			quant = true
			if t.Bits > 0 {
				bitsSet[t.Bits] = struct{}{}
			}
		case RolePlain:
			addInto(nil, &plainBytes, t.ByteLen())
			v := plainBy[t.DType]
			addInto(nil, &v, t.ByteLen())
			plainBy[t.DType] = v
		}
	}
	plainDT := ""
	var best int64 = -1
	for _, dt := range sortedStringKeys(plainBy) {
		if plainBy[dt] > best {
			best = plainBy[dt]
			plainDT = displayDType(dt)
		}
	}
	if !quant {
		if plainDT == "" {
			return "—"
		}
		return plainDT
	}
	bits := make([]int, 0, len(bitsSet))
	for b := range bitsSet {
		bits = append(bits, b)
	}
	sort.Ints(bits)
	var label string
	if len(bits) == 0 {
		label = "q"
	} else {
		parts := make([]string, len(bits))
		for i, b := range bits {
			parts[i] = fmt.Sprintf("q%d", b)
		}
		label = strings.Join(parts, "+")
	}
	if g.Bytes > 0 && plainBytes*10 >= g.Bytes && plainDT != "" {
		label += "+" + plainDT
	}
	return label
}

// FormatName splits Model.Format into the v1 name, variant, and supported flag.
func FormatName(format string) (name, variant string, supported bool) {
	switch {
	case strings.HasPrefix(format, "gguf"):
		return "gguf", "", false
	case format == "" || format == "unknown":
		return "unknown", "", false
	}
	name = format
	if i := strings.Index(format, " ("); i >= 0 && strings.HasSuffix(format, ")") {
		name = format[:i]
		variant = format[i+2 : len(format)-1]
	}
	return name, variant, name == "safetensors"
}

func formatVariant(format string) string {
	_, v, _ := FormatName(format)
	return v
}

func (m *Model) layerTypeList() []string {
	for _, scope := range []map[string]any{m.Config, subMap(m.Config, "text_config"), subMap(m.Config, "llm_config"), subMap(m.Config, "language_config")} {
		if scope == nil {
			continue
		}
		lt, ok := scope["layer_types"].([]any)
		if !ok || len(lt) == 0 {
			continue
		}
		if len(lt) > MaxLayerIndex+1 {
			m.noteDroppedLayer(MaxLayerIndex + 1)
			lt = lt[:MaxLayerIndex+1]
		}
		out := make([]string, 0, len(lt))
		for _, v := range lt {
			out = append(out, strOf(v))
		}
		return out
	}
	return nil
}

// clampLayerSlots bounds a count taken from a config integer or a group
// tally so a hostile num_hidden_layers cannot allocate a giant slice.
func (m *Model) clampLayerSlots(n int) int {
	if n < 0 {
		return 0
	}
	if n > MaxLayerIndex+1 {
		m.noteDroppedLayer(n)
		return MaxLayerIndex + 1
	}
	return n
}

func (m *Model) configInt(key string) int {
	for _, kv := range m.KeyConfig() {
		if kv.Key == key {
			return intOf(kv.Value)
		}
	}
	return 0
}

func (m *Model) tiedWordEmbeddings() bool {
	for _, kv := range m.KeyConfig() {
		if kv.Key != "tie_word_embeddings" {
			continue
		}
		b, ok := kv.Value.(bool)
		return ok && b
	}
	return false
}

func (m *Model) hasKind(kind string) bool {
	for _, g := range m.Groups {
		if g.Kind == kind {
			return true
		}
	}
	return false
}

// lookupStem strips a trailing .weight/.scales/.biases and, when the
// remainder ends in .shard_<digits>, the shard suffix. sharded is false
// for an ordinary tensor name. The original spelling of the stem is kept.
//
// Case is folded for ASCII only. strings.ToLower changes the byte length
// of some runes (U+023A grows, U+212A and U+0130 shrink), and an index
// taken from that lowered string panics or cuts the original name short
// when it is used to slice it. Every match below is a window of the
// original string, so the stem is always a substring of name.
func lookupStem(name string) (stem string, sharded bool) {
	base := stripWeightSuffix(name)
	const mark = ".shard_"
	i := lastIndexFold(base, mark)
	if i < 0 {
		return base, false
	}
	digits := base[i+len(mark):]
	if !asciiDigits(digits) {
		return base, false
	}
	return base[:i], true
}

// shardOrdinal is the numeric shard index, with leading zeros removed.
// A digit string that does not fit in uint64 keeps its own key so it
// cannot collide with a smaller index. ok is false when name is not a shard.
func shardOrdinal(name string) (string, bool) {
	base := stripWeightSuffix(name)
	const mark = ".shard_"
	i := lastIndexFold(base, mark)
	if i < 0 {
		return "", false
	}
	digits := base[i+len(mark):]
	if !asciiDigits(digits) {
		return "", false
	}
	n, err := strconv.ParseUint(digits, 10, 64)
	if err != nil {
		return "raw:" + digits, true
	}
	return strconv.FormatUint(n, 10), true
}

func ngramName(name string) bool {
	folded := foldASCII(name)
	return strings.Contains(folded, "ngram") || strings.Contains(folded, "n_gram")
}

func hostName(name string) bool {
	seg := foldASCII(lastSegment(stripWeightSuffix(name)))
	for _, bad := range []string{"patch", "pos", "position", "proj", "norm", "head"} {
		if strings.Contains(seg, bad) {
			return false
		}
	}
	switch seg {
	case "embed_tokens", "tok_embeddings", "word_embeddings", "wte", "embedding", "embeddings":
		return true
	}
	if strings.Contains(seg, "embed") {
		for _, k := range []string{"ngram", "n_gram", "hash", "engram", "table"} {
			if strings.Contains(seg, k) {
				return true
			}
		}
	}
	return false
}

func tokenEmbedSegment(name string) bool {
	switch foldASCII(lastSegment(stripWeightSuffix(name))) {
	case "embed_tokens", "tok_embeddings", "word_embeddings", "wte":
		return true
	default:
		return false
	}
}

func stripWeightSuffix(name string) string {
	for _, suf := range []string{".weight", ".scales", ".biases"} {
		if hasSuffixFold(name, suf) {
			return name[:len(name)-len(suf)]
		}
	}
	return name
}

// foldASCII lowercases ASCII letters and leaves every other byte alone,
// so the result has the same length as s.
func foldASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

func foldByte(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}

func foldEq(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		if foldByte(a[i]) != foldByte(b[i]) {
			return false
		}
	}
	return true
}

func hasSuffixFold(s, suf string) bool {
	if len(suf) == 0 || len(s) < len(suf) {
		return false
	}
	return foldEq(s[len(s)-len(suf):], suf)
}

func lastIndexFold(s, sub string) int {
	if len(sub) == 0 || len(s) < len(sub) {
		return -1
	}
	for i := len(s) - len(sub); i >= 0; i-- {
		if foldEq(s[i:i+len(sub)], sub) {
			return i
		}
	}
	return -1
}

func asciiDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func lastSegment(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return name[i+1:]
	}
	return name
}

func largestTensor(g *Group) (*Tensor, float64) {
	var best *Tensor
	for _, t := range g.Tensors {
		if best == nil || t.ByteLen() > best.ByteLen() {
			best = t
		}
	}
	if best == nil || g.Bytes <= 0 {
		return best, 0
	}
	return best, float64(best.ByteLen()) / float64(g.Bytes) * 100
}

func tensorPhrase(t *Tensor) string {
	if ngramName(t.Name) {
		return fmt.Sprintf("~%s-param n-gram embedding table", HumanCount(t.LogicalParams))
	}
	return ShortName(t.Name)
}

func hostMessage(h HostTensor) string {
	tail := "heuristic"
	if h.incomplete {
		tail = "heuristic, incomplete"
	}
	if ngramName(h.Name) {
		return fmt.Sprintf("n-gram embedding table %s can stay in CPU RAM (%s)", HumanBytes(h.Bytes), tail)
	}
	return fmt.Sprintf("%s (%s) can stay in CPU RAM (%s)", ShortName(h.Name), HumanBytes(h.Bytes), tail)
}

func dominantPlain(g *Group) (string, int64) {
	by := map[string]int64{}
	var n int64
	for _, t := range g.Tensors {
		if t.Role != RolePlain {
			continue
		}
		v := by[t.DType]
		addInto(nil, &v, t.ByteLen())
		by[t.DType] = v
		addInto(nil, &n, t.ByteLen())
	}
	var best string
	var b int64 = -1
	for _, dt := range sortedStringKeys(by) {
		if by[dt] > b {
			b = by[dt]
			best = dt
		}
	}
	return best, n
}

func float16or32(dt string) bool {
	switch strings.ToUpper(dt) {
	case "BF16", "F16", "F32", "F64", "BF32":
		return true
	default:
		return false
	}
}

func groupTitle(g *Group) string {
	if g.Kind == KindVision {
		return "vision tower"
	}
	return g.Key
}

func textArch(m *Model) bool {
	mt := ""
	for _, kv := range m.KeyConfig() {
		if kv.Key == "model_type" {
			mt = strings.ToLower(strOf(kv.Value))
			break
		}
	}
	if mt == "" {
		return true
	}
	return !strings.Contains(mt, "vision")
}

func hasCallout(cs []Callout, kind, group string) bool {
	for _, c := range cs {
		if c.Kind == kind && c.Group == group {
			return true
		}
	}
	return false
}

func inferLayerType(g *Group) string {
	var linear, full bool
	for _, t := range g.Tensors {
		n := strings.ToLower(t.Name)
		if strings.Contains(n, "linear_attn") {
			linear = true
		}
		if strings.Contains(n, "full_attn") || strings.Contains(n, "self_attn") {
			full = true
		}
	}
	switch {
	case linear && !full:
		return "linear_attention"
	case full && !linear:
		return "full_attention"
	default:
		return ""
	}
}

func median(xs []int64) int64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]int64(nil), xs...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	a, b := s[n/2-1], s[n/2]
	return a/2 + b/2 + (a%2+b%2)/2
}

func exceeds3x(v, med int64) bool {
	if med <= 0 {
		return v > 0
	}
	if med > math.MaxInt64/3 {
		return false
	}
	return v > 3*med
}

func pct(part, total int64) float64 {
	if total <= 0 || part <= 0 {
		return 0
	}
	// A saturated total can be smaller than one class that still fit in
	// int64, and float64 rounding can push a ratio just over 1. The bar
	// and its legend stay inside [0, 100].
	if part >= total {
		return 100
	}
	v := math.Round(float64(part)/float64(total)*1000) / 10
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

// satAdd adds n to *dst when the sum fits in a non-negative int64.
// A negative operand or an overflow leaves *dst unchanged.
func satAdd(dst *int64, n int64) bool {
	if dst == nil || n < 0 || *dst < 0 || *dst > math.MaxInt64-n {
		return false
	}
	*dst += n
	return true
}

func (m *Model) warnOnce(format string, a ...any) {
	if m == nil {
		return
	}
	msg := fmt.Sprintf(format, a...)
	for _, w := range m.Warnings {
		if w == msg {
			return
		}
	}
	m.warnf("%s", msg)
}

// addInto is satAdd, and marks m partial when the sum does not fit.
// m may be nil when the caller has no inspection to mark.
func addInto(m *Model, dst *int64, n int64) {
	if satAdd(dst, n) {
		return
	}
	if m != nil {
		m.noteOverflow()
	}
}

func (m *Model) addClass(acc map[string]int64, key string, n int64) {
	v, ok := acc[key]
	if !satAdd(&v, n) {
		m.noteOverflow()
		if ok {
			acc[key] = v
		}
		return
	}
	acc[key] = v
}

// DisplayDType renders a dtype the way the header and group labels do.
func DisplayDType(dt string) string { return displayDType(dt) }

func displayDType(dt string) string {
	switch strings.ToUpper(dt) {
	case "":
		return ""
	case "BF16":
		return "bf16"
	case "F16":
		return "fp16"
	case "F32":
		return "fp32"
	case "F64":
		return "fp64"
	default:
		return strings.ToLower(dt)
	}
}

func sortedStringKeys(m map[string]int64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
