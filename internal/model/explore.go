package model

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// expertIndex matches ".experts.N." (and a trailing index). Shared experts
// are not numbered this way and are not counted.
var expertIndex = regexp.MustCompile(`(?:^|\.)experts\.(\d+)(?:\.|$)`)

// ExpertCount is the routed-expert count for a group.
// A rank-3 (or higher) stacked expert tensor contributes its leading
// dimension. Otherwise the count is the number of distinct ".experts.N."
// indices. ok is false when the group has no routed experts or the stacked
// dimensions disagree and there is no index to count.
func (g *Group) ExpertCount() (n int, ok bool) {
	if g == nil || !g.HasExperts {
		return 0, false
	}
	dim, status := stackedDim(g.Tensors)
	if status == stackOK && dim <= math.MaxInt {
		return int(dim), true
	}
	idxs := map[int]struct{}{}
	for _, t := range g.Tensors {
		if t == nil || !IsExpertTensor(t.Name) {
			continue
		}
		m := expertIndex.FindStringSubmatch(strings.ToLower(t.Name))
		if m == nil {
			continue
		}
		v, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		idxs[v] = struct{}{}
	}
	if len(idxs) > 0 {
		return len(idxs), true
	}
	return 0, false
}

// ExpertStacked reports that the routed-expert count came from one shared
// leading dimension on rank >= 3 stacked expert tensors.
func (g *Group) ExpertStacked() bool {
	if g == nil {
		return false
	}
	_, status := stackedDim(g.Tensors)
	return status == stackOK
}

const (
	stackNone = iota
	stackOK
	stackConflict
)

// StackedExpertDim is the shared leading dimension of rank >= 3 stacked
// expert tensors (switch_mlp / _exps). ok is false when none exist or the
// dimensions disagree.
func (m *Model) StackedExpertDim() (int, bool) {
	if m == nil {
		return 0, false
	}
	dim, status := stackedDim(m.Tensors)
	if status != stackOK || dim > math.MaxInt {
		return 0, false
	}
	return int(dim), true
}

// StackedExpertsDisagree reports that stacked expert tensors exist and do
// not share one leading dimension.
func (m *Model) StackedExpertsDisagree() bool {
	if m == nil {
		return false
	}
	_, status := stackedDim(m.Tensors)
	return status == stackConflict
}

func stackedDim(tensors []*Tensor) (int64, int) {
	var dim int64
	status := stackNone
	for _, t := range tensors {
		if t == nil || !stackedExpertName(t.Name) {
			continue
		}
		if len(t.Shape) < 3 || t.Shape[0] <= 0 {
			continue
		}
		if status == stackNone {
			dim = t.Shape[0]
			status = stackOK
			continue
		}
		if t.Shape[0] != dim {
			return 0, stackConflict
		}
	}
	return dim, status
}

func stackedExpertName(name string) bool {
	low := strings.ToLower(name)
	return strings.Contains(low, ".switch_mlp.") || strings.Contains(low, "_exps.")
}

// LayerInfo is how one decoder layer's attention type is shown.
// Short is lin, full, ?, -, or at most 4 runes of an unknown value.
// Compared is true when both a config type and a confident tensor-name
// type exist for the layer. Disagree is that pair not matching.
type LayerInfo struct {
	Short    string
	Long     string
	Compared bool
	Disagree bool
}

// LayerInfoBy returns one entry per decoder layer that can be described
// without sizing storage by the highest index. A lone layer 99999999 does
// not allocate a multi-million entry table. Indices above MaxLayerIndex
// are omitted. Config layer_types entries are included (that list is
// already capped). num_hidden_layers fills a dense prefix only when it
// stays close to the number of distinct layers present.
func (m *Model) LayerInfoBy() map[int]LayerInfo {
	if m == nil {
		return nil
	}
	present := map[int]*Group{}
	for _, g := range m.Groups {
		if g == nil || g.Kind != KindLayer || g.Layer < 0 || g.Layer > MaxLayerIndex {
			continue
		}
		present[g.Layer] = g
	}
	list := m.layerTypeList()
	idxs := map[int]struct{}{}
	for layer := range present {
		idxs[layer] = struct{}{}
	}
	for i := range list {
		if i <= MaxLayerIndex {
			idxs[i] = struct{}{}
		}
	}
	interval := 0
	if len(list) == 0 {
		interval = m.configInt("full_attention_interval")
		if interval < 0 {
			interval = 0
		}
	}
	declared := 0
	if len(list) == 0 {
		declared = m.configInt("num_hidden_layers")
		if declared < 0 {
			declared = 0
		}
		if declared > MaxLayerIndex+1 {
			declared = MaxLayerIndex + 1
			m.Partial = true
		}
	}
	if interval > 0 && declared > 0 {
		limit := declared
		if len(present) > 0 && limit > len(present)*2+16 {
			limit = 0
		}
		for i := 0; i < limit; i++ {
			idxs[i] = struct{}{}
		}
	}
	out := make(map[int]LayerInfo, len(idxs))
	for i := range idxs {
		cfg := ""
		switch {
		case i < len(list):
			cfg = list[i]
		case interval > 0:
			if (i+1)%interval == 0 {
				cfg = "full_attention"
			} else {
				cfg = "linear_attention"
			}
		}
		inf := ""
		weak := ""
		if g := present[i]; g != nil {
			inf = confidentLayerType(g)
			weak = inferLayerType(g)
		}
		disagree := cfg != "" && inf != "" && cfg != inf
		src := cfg
		if src == "" {
			src = weak
		}
		if src == "" {
			src = inf
		}
		out[i] = LayerInfo{
			Short:    shortAttn(src, disagree),
			Long:     longAttn(src, disagree),
			Compared: cfg != "" && inf != "",
			Disagree: disagree,
		}
	}
	return out
}

// LayerInfo returns one entry per decoder-layer index that fits in a dense
// span. The slice may be longer than LayerCount when config lists types
// for layers that have no tensors. Index i matches Group.Layer. A sparse
// high index is omitted from the slice; LayerInfoBy still has the layers
// that were actually present and in range.
func (m *Model) LayerInfo() []LayerInfo {
	by := m.LayerInfoBy()
	if len(by) == 0 {
		return nil
	}
	max := -1
	for i := range by {
		if i > max {
			max = i
		}
	}
	if max < 0 || max > MaxLayerIndex {
		return nil
	}
	if max+1 > len(by)*2+16 {
		dense := 0
		for {
			if _, ok := by[dense]; !ok || dense > max {
				break
			}
			dense++
		}
		out := make([]LayerInfo, dense)
		for i := 0; i < dense; i++ {
			out[i] = by[i]
		}
		return out
	}
	out := make([]LayerInfo, max+1)
	for i, info := range by {
		if i >= 0 && i <= max {
			out[i] = info
		}
	}
	return out
}

// confidentLayerType reads linear_attn / full_attn from tensor names.
// A bare self_attn name is not confident: many checkpoints use it for
// every block, including linear attention.
func confidentLayerType(g *Group) string {
	var linear, full bool
	for _, t := range g.Tensors {
		if t == nil {
			continue
		}
		n := strings.ToLower(t.Name)
		if strings.Contains(n, "linear_attn") || strings.Contains(n, "linear_attention") {
			linear = true
		}
		if strings.Contains(n, "full_attn") || strings.Contains(n, "full_attention") {
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

func shortAttn(s string, disagree bool) string {
	if disagree {
		return "?"
	}
	switch s {
	case "":
		return "-"
	case "linear_attention":
		return "lin"
	case "full_attention":
		return "full"
	default:
		r := []rune(s)
		if len(r) > 4 {
			return string(r[:4])
		}
		return s
	}
}

func longAttn(s string, disagree bool) string {
	if disagree {
		return "disagrees"
	}
	switch s {
	case "":
		return ""
	case "linear_attention":
		return "linear-attn"
	case "full_attention":
		return "full-attn"
	default:
		return s
	}
}

// MetadataMismatches is the warn-only config-vs-tensor list.
// num_hidden_layers and layer_types are compared only when at least one
// decoder layer tensor was found, so a directory that holds only an
// n-gram table is not warned for the layers it does not contain.
// vocab_size is compared only when a token embedding's leading dimension
// is greater than 1 (a 1-row pack is not treated as a vocabulary axis).
// num_experts is compared only to a rank >= 3 stacked expert dimension.
func (m *Model) MetadataMismatches() []string {
	if m == nil || m.Config == nil {
		return nil
	}
	var out []string
	layers, _ := m.LayerCount()
	if layers > 0 {
		if n := m.configInt("num_hidden_layers"); n > 0 && n != layers {
			out = append(out, fmt.Sprintf("num_hidden_layers is %d, found %d decoder layers", n, layers))
		}
		if lt := m.layerTypeList(); len(lt) > 0 && len(lt) != layers {
			out = append(out, fmt.Sprintf("layer_types has %d entries, found %d decoder layers", len(lt), layers))
		}
	}
	if exp := m.expertConfig(); exp > 0 {
		switch dim, status := stackedDim(m.Tensors); status {
		case stackOK:
			if dim <= math.MaxInt && int(dim) != exp {
				out = append(out, fmt.Sprintf("num_experts is %d, stacked expert dimension is %d", exp, dim))
			}
		case stackConflict:
			out = append(out, "stacked expert dimensions disagree across tensors")
		}
	}
	if vocab := m.configInt("vocab_size"); vocab > 0 {
		if rows, ok := m.TokenEmbedRows(); ok && rows != int64(vocab) {
			out = append(out, fmt.Sprintf("vocab_size is %d, token embedding leading dimension is %d", vocab, rows))
		}
	}
	by := m.LayerInfoBy()
	ids := make([]int, 0, len(by))
	for i, info := range by {
		if info.Disagree {
			ids = append(ids, i)
		}
	}
	sort.Ints(ids)
	for _, i := range ids {
		out = append(out, fmt.Sprintf("layer %d attention type disagrees between config and tensor names", i))
		if len(out) >= 64 {
			break
		}
	}
	return out
}

// MetadataCompared reports whether any config field was checked against
// tensors. A config with none of those fields is not a pass.
func (m *Model) MetadataCompared() bool {
	if m == nil || m.Config == nil {
		return false
	}
	layers, _ := m.LayerCount()
	if layers > 0 && m.configInt("num_hidden_layers") > 0 {
		return true
	}
	if layers > 0 && len(m.layerTypeList()) > 0 {
		return true
	}
	if m.expertConfig() > 0 {
		_, status := stackedDim(m.Tensors)
		if status == stackOK || status == stackConflict {
			return true
		}
	}
	if m.configInt("vocab_size") > 0 {
		if _, ok := m.TokenEmbedRows(); ok {
			return true
		}
	}
	for _, info := range m.LayerInfoBy() {
		if info.Compared {
			return true
		}
	}
	return false
}

func (m *Model) expertConfig() int {
	for _, key := range []string{"num_experts", "num_local_experts", "n_routed_experts"} {
		if n := m.configInt(key); n > 0 {
			return n
		}
	}
	return 0
}

// TokenEmbedRows is the shared leading dimension of token-embedding
// weights whose leading dimension is greater than 1. ok is false when no
// such tensor exists or the dimensions disagree.
func (m *Model) TokenEmbedRows() (int64, bool) {
	if m == nil {
		return 0, false
	}
	var rows int64
	var have bool
	for _, t := range m.Tensors {
		if t == nil || (t.Role != RolePlain && t.Role != RoleQuantWeight) {
			continue
		}
		if !tokenEmbedSegment(t.Name) || len(t.Shape) < 1 || t.Shape[0] <= 1 {
			continue
		}
		if !have {
			rows = t.Shape[0]
			have = true
			continue
		}
		if t.Shape[0] != rows {
			return 0, false
		}
	}
	return rows, have
}

// LogicalTensor is one drill-down row. Sharded pieces that share a stem,
// role, dtype, and shape are one row. Bytes and params count each piece
// once. Pieces that disagree stay separate so a merged row is never a mix
// of shapes.
type LogicalTensor struct {
	Name      string
	FullName  string
	DType     string
	Shape     []int64
	Shards    int
	Bytes     int64
	Params    int64
	Elems     int64
	Role      string
	Host      bool
	Outlier   bool
	Shard     string
	OffStart  int64
	OffEnd    int64
	DataStart int64
	AbsOK     bool
	Overflow  bool
}

// LogicalTensors collapses ".shard_N" pieces for display. The group table
// already has one row for the n-gram table; this keeps the drill-down from
// listing every piece. Host follows HostCapable, including scale and bias
// siblings of a flagged table.
func (g *Group) LogicalTensors(m *Model) []LogicalTensor {
	if g == nil {
		return nil
	}
	hosts := hostKeySet(m)
	type bucket struct {
		items   []*Tensor
		sharded bool
		stem    string
	}
	order := make([]*bucket, 0, len(g.Tensors))
	index := map[string]*bucket{}
	for _, t := range g.Tensors {
		if t == nil {
			continue
		}
		stem, sharded := lookupStem(t.Name)
		key := t.Name
		if sharded {
			key = stem + "\x00" + t.Role
		}
		b := index[key]
		if b == nil {
			b = &bucket{sharded: sharded, stem: stem}
			index[key] = b
			order = append(order, b)
		}
		b.items = append(b.items, t)
	}
	out := make([]LogicalTensor, 0, len(order))
	for _, b := range order {
		if b.sharded && piecesAgree(b.items) {
			out = append(out, logicalRow(g, m, b.stem, b.items, hosts))
			continue
		}
		for _, t := range b.items {
			out = append(out, logicalRow(g, m, "", []*Tensor{t}, hosts))
		}
	}
	return out
}

func piecesAgree(items []*Tensor) bool {
	if len(items) == 0 {
		return false
	}
	d0 := items[0].DType
	s0 := items[0].Shape
	for _, t := range items[1:] {
		if t.DType != d0 || !shapeEqual(t.Shape, s0) {
			return false
		}
	}
	return true
}

func shapeEqual(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func logicalRow(g *Group, m *Model, stem string, items []*Tensor, hosts map[string]struct{}) LogicalTensor {
	first := items[0]
	row := LogicalTensor{
		DType:  first.DType,
		Shape:  append([]int64(nil), first.Shape...),
		Shards: len(items),
		Role:   roleLabel(first),
		Shard:  first.Shard,
	}
	suffix := weightSuffix(first.Name)
	if stem != "" {
		row.FullName = stem + suffix
	} else {
		row.FullName = first.Name
		row.OffStart = first.DataOffsets[0]
		row.OffEnd = first.DataOffsets[1]
		if h := shardDataStart(m, first.Shard); h >= 0 {
			if _, _, ok := absRange(h, row.OffStart, row.OffEnd); ok {
				row.DataStart = h
				row.AbsOK = true
			}
		}
	}
	name := row.FullName
	if g.Kind == KindLayer {
		name = ShortName(name)
	}
	row.Name = name
	sameShard := true
	for _, t := range items {
		if t.Shard != row.Shard {
			sameShard = false
		}
		if !addChecked(&row.Bytes, t.ByteLen()) || !addChecked(&row.Params, t.LogicalParams) || !addChecked(&row.Elems, t.NumElements()) {
			row.Overflow = true
		}
		if tensorHost(t.Name, hosts) {
			row.Host = true
		}
	}
	if !sameShard {
		row.Shard = ""
	}
	if g.Bytes > 0 && atLeastHalf(row.Bytes, g.Bytes) {
		row.Outlier = true
	}
	return row
}

func weightSuffix(name string) string {
	low := strings.ToLower(name)
	for _, suf := range []string{".weight", ".scales", ".biases"} {
		if strings.HasSuffix(low, suf) {
			return name[len(name)-len(suf):]
		}
	}
	return ""
}

func roleLabel(t *Tensor) string {
	switch t.Role {
	case RoleQuantWeight:
		if t.Bits > 0 {
			return fmt.Sprintf("q%d", t.Bits)
		}
		return "q"
	case RoleQuantScales:
		return "qscal"
	case RoleQuantBiases:
		return "qbias"
	default:
		return "plain"
	}
}

func hostKeySet(m *Model) map[string]struct{} {
	if m == nil {
		return nil
	}
	out := map[string]struct{}{}
	for _, h := range m.HostCapable() {
		out[h.Name] = struct{}{}
	}
	return out
}

func tensorHost(name string, keys map[string]struct{}) bool {
	if _, ok := keys[name]; ok {
		return true
	}
	stem, _ := lookupStem(name)
	if _, ok := keys[stem]; ok {
		return true
	}
	base, suf := splitWeightSuffix(name)
	if suf != "" {
		if _, ok := keys[base+".weight"]; ok {
			return true
		}
		if _, ok := keys[base]; ok {
			return true
		}
	}
	return false
}

func splitWeightSuffix(name string) (base, suffix string) {
	low := strings.ToLower(name)
	for _, suf := range []string{".weight", ".scales", ".biases"} {
		if strings.HasSuffix(low, suf) {
			return name[:len(name)-len(suf)], suf
		}
	}
	return name, ""
}

func shardDataStart(m *Model, name string) int64 {
	if m == nil || name == "" {
		return -1
	}
	for _, s := range m.Shards {
		if s != nil && s.Name == name && s.Header != nil {
			return s.Header.DataStart()
		}
	}
	return -1
}

func absRange(start, off0, off1 int64) (int64, int64, bool) {
	if start < 0 || off0 < 0 || off1 < off0 {
		return 0, 0, false
	}
	if start > math.MaxInt64-off1 {
		return 0, 0, false
	}
	return start + off0, start + off1, true
}

func atLeastHalf(part, total int64) bool {
	if part <= 0 || total <= 0 {
		return false
	}
	if part >= total {
		return true
	}
	if part > math.MaxInt64/2 {
		return true
	}
	return part*2 >= total
}

func addChecked(dst *int64, n int64) bool {
	if dst == nil || n < 0 || *dst < 0 || *dst > math.MaxInt64-n {
		return false
	}
	*dst += n
	return true
}

// Absolute is the half-open file byte range of a single-piece row.
// ok is false for a merged row or when the header start and the relative
// offsets cannot be added in int64.
func (r LogicalTensor) Absolute() (start, end int64, ok bool) {
	if !r.AbsOK || r.Shards != 1 {
		return 0, 0, false
	}
	return absRange(r.DataStart, r.OffStart, r.OffEnd)
}

// ShapeText renders a shape without spaces.
func ShapeText(shape []int64) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, d := range shape {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatInt(d, 10))
	}
	b.WriteByte(']')
	return b.String()
}
