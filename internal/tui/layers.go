package tui

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/sanitize"
	"github.com/cecilkootz/model-inspector/internal/tui/ui"
)

const (
	rowGroup = iota
	rowSub
	rowTensor
	rowMore
)

const (
	layerSortIndex = iota
	layerSortSize
	layerSortParams
	layerSortName
)

const (
	attnAll = iota
	attnLinear
	attnFull
)

const layersQueryMax = 128

// layersState is the Layers tab. Groups stay closed until opened. Children
// of a wide node are paged, the same way the config tree pages object keys.
type layersState struct {
	expanded map[string]bool
	temp     map[string]bool
	page     map[string]pageWin

	cursorPath string
	row        int
	scroll     int
	viewH      int

	search    bool
	query     string
	filter    bool
	matches   map[string]bool
	matchIDs  []string
	matchIdx  int
	largeWait bool
	keep      map[string]bool

	sortBy     int
	sortDesc   bool
	outliers   bool
	attn       int
	parts      bool
	packParts  bool
	jump       bool
	jumpQ      string
	drill      bool
	perTok     int
	sawDefault bool

	rows  []lrow
	dirty bool
	gen   int

	prepped bool
	info    map[int]model.LayerInfo
	diamT   map[string]bool
	big     map[string]bool
	san     map[string]sanitize.Text
	pack    map[int][]tensorPack

	ratio    int
	ratioSet bool
	hits     []hit
	snap     *layersSnap
	copyTo   io.Writer
	copyTTY  *bool
}

type lrow struct {
	path   string
	parent string
	kind   int
	depth  int
	anc    uint32
	last   bool
	more   bool
	lead   bool
	hidden bool
	hideN  int
	match  bool
	gidx   int
	sub    string
	tidx   int
}

type tensorPack struct {
	sub  string
	rows []model.LogicalTensor
}

type layersSnap struct {
	expanded map[string]bool
	temp     map[string]bool
	cursor   string
	scroll   int
	row      int
	query    string
	filter   bool
}

var subOrder = []string{"attn", "router", "shared", "experts", "norms", "hyper", "ple", "other"}

func newLayersState() *layersState {
	return &layersState{
		expanded: map[string]bool{},
		temp:     map[string]bool{},
		page:     map[string]pageWin{},
		matches:  map[string]bool{},
		dirty:    true,
		san:      map[string]sanitize.Text{},
		pack:     map[int][]tensorPack{},
	}
}

func (s *layersState) framePart() string {
	if s == nil {
		return ""
	}
	return fmt.Sprintf("|ly %d %d %d %q %v %v %d %d %d %d %d %d %d %d %q",
		s.row, s.scroll, s.gen, s.query, s.search, s.filter, s.matchIdx, s.sortBy, boolInt(s.sortDesc),
		boolInt(s.outliers), s.attn, boolInt(s.parts), boolInt(s.drill), boolInt(s.jump), s.jumpQ)
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (t *Model) touchLayers() {
	if t.lay != nil {
		t.lay.gen++
	}
	if t.cache != nil {
		t.cache.key = ""
	}
}

func (t Model) layersSearching() bool {
	return t.active == tabLayers && t.lay != nil && t.lay.search
}

func (t Model) layersTreeOn() bool {
	if t.active != tabLayers || t.lay == nil {
		return false
	}
	br := ui.Breakpoint(t.width, t.height)
	if br == "S" || br == "XS" || t.zoom {
		return !t.focusSecondary || !t.zoom
	}
	return !t.focusSecondary
}

func groupPath(g *model.Group) string {
	if g == nil {
		return "g:"
	}
	return "g:" + g.Key
}

func (s *layersState) isOpen(path string) bool {
	if s == nil {
		return false
	}
	if v, ok := s.expanded[path]; ok {
		return v
	}
	if v, ok := s.temp[path]; ok {
		return v
	}
	return false
}

func (s *layersState) setOpen(path string, open bool) {
	if s.expanded == nil {
		s.expanded = map[string]bool{}
	}
	s.expanded[path] = open
	if s.temp != nil {
		delete(s.temp, path)
	}
	s.dirty = true
}

func (s *layersState) ensure(m *model.Model) {
	if s == nil {
		return
	}
	if !s.dirty && s.rows != nil {
		return
	}
	path := s.cursorPath
	s.rebuild(m)
	s.dirty = false
	if path != "" {
		for i, row := range s.rows {
			if row.path == path {
				s.row = i
				return
			}
		}
	}
	if s.row >= len(s.rows) {
		s.row = len(s.rows) - 1
	}
	if s.row < 0 {
		s.row = 0
	}
	if len(s.rows) > 0 && s.row < len(s.rows) {
		s.cursorPath = s.rows[s.row].path
	}
}

func (s *layersState) rebuild(m *model.Model) {
	s.rows = s.rows[:0]
	if m == nil {
		return
	}
	s.prep(m)
	if !s.sawDefault {
		s.sawDefault = true
		s.expanded["dec"] = true
		if s.cursorPath == "" {
			s.cursorPath = "dec"
		}
	}
	if s.packParts != s.parts {
		s.pack = map[int][]tensorPack{}
		s.packParts = s.parts
	}
	if s.filter && strings.TrimSpace(s.query) != "" {
		s.keep = map[string]bool{}
		for p := range s.matches {
			for p != "" {
				if s.keep[p] {
					break
				}
				s.keep[p] = true
				i := strings.LastIndex(p, "/")
				if i < 0 {
					break
				}
				p = p[:i]
			}
		}
	} else {
		s.keep = nil
	}
	s.emitTops(m)
}

func (s *layersState) emitTops(m *model.Model) {
	type top struct {
		dec    bool
		gidx   int
		bytes  int64
		params int64
		name   string
	}
	var layers []int
	var layerBytes, layerParams int64
	for i, g := range m.Groups {
		if g == nil || g.Kind != model.KindLayer {
			continue
		}
		if !s.layerVisible(m, g) {
			continue
		}
		layers = append(layers, i)
		layerBytes += g.Bytes
		layerParams += g.LogicalParams
	}
	var tops []top
	decPlaced := false
	order := s.groupOrder(m)
	for _, i := range order {
		g := m.Groups[i]
		if g == nil {
			continue
		}
		if g.Kind == model.KindLayer {
			if !decPlaced && len(layers) > 0 {
				tops = append(tops, top{dec: true, bytes: layerBytes, params: layerParams, name: "decoder"})
				decPlaced = true
			}
			continue
		}
		if !s.groupVisible(g) {
			continue
		}
		tops = append(tops, top{gidx: i, bytes: g.Bytes, params: g.LogicalParams, name: g.Key})
	}
	if !decPlaced && len(layers) > 0 {
		tops = append(tops, top{dec: true, bytes: layerBytes, params: layerParams, name: "decoder"})
	}
	if s.sortBy != layerSortIndex {
		sort.SliceStable(tops, func(a, b int) bool {
			switch s.sortBy {
			case layerSortSize:
				if tops[a].bytes != tops[b].bytes {
					if s.sortDesc {
						return tops[a].bytes > tops[b].bytes
					}
					return tops[a].bytes < tops[b].bytes
				}
			case layerSortParams:
				if tops[a].params != tops[b].params {
					if s.sortDesc {
						return tops[a].params > tops[b].params
					}
					return tops[a].params < tops[b].params
				}
			default:
				na, nb := strings.ToLower(tops[a].name), strings.ToLower(tops[b].name)
				if na != nb {
					if s.sortDesc {
						return na > nb
					}
					return na < nb
				}
			}
			return a < b
		})
	}
	if s.sortBy != layerSortIndex && len(layers) > 1 {
		sort.SliceStable(layers, func(a, b int) bool {
			ga, gb := m.Groups[layers[a]], m.Groups[layers[b]]
			switch s.sortBy {
			case layerSortSize:
				if ga.Bytes != gb.Bytes {
					if s.sortDesc {
						return ga.Bytes > gb.Bytes
					}
					return ga.Bytes < gb.Bytes
				}
			case layerSortParams:
				if ga.LogicalParams != gb.LogicalParams {
					if s.sortDesc {
						return ga.LogicalParams > gb.LogicalParams
					}
					return ga.LogicalParams < gb.LogicalParams
				}
			default:
				na, nb := strings.ToLower(ga.Key), strings.ToLower(gb.Key)
				if na != nb {
					if s.sortDesc {
						return na > nb
					}
					return na < nb
				}
			}
			return layers[a] < layers[b]
		})
	}
	for j, tp := range tops {
		if len(s.rows) >= visibleMax {
			return
		}
		last := j == len(tops)-1
		if tp.dec {
			s.walkDecoder(m, layers, 0, 0, last)
			continue
		}
		s.walkGroup(m, tp.gidx, 0, 0, last, "")
	}
}

func (s *layersState) layerVisible(m *model.Model, g *model.Group) bool {
	if g == nil {
		return false
	}
	if s.keep != nil && !s.keep[groupPath(g)] {
		// A child match (tensor path) still keeps the group via ancestor trim.
		return false
	}
	if s.attn != attnAll && s.info != nil {
		info := s.info[g.Layer]
		switch s.attn {
		case attnLinear:
			if info.Short == "full" {
				return false
			}
		case attnFull:
			if info.Short != "full" {
				return false
			}
		}
	}
	if s.outliers && !s.groupOutlier(g) {
		return false
	}
	return true
}

func (s *layersState) groupVisible(g *model.Group) bool {
	if g == nil {
		return false
	}
	if s.keep != nil && !s.keep[groupPath(g)] {
		return false
	}
	if s.outliers && !s.groupOutlier(g) {
		return false
	}
	return true
}

func (s *layersState) groupOutlier(g *model.Group) bool {
	if g == nil {
		return false
	}
	if g.Key == "ngram" {
		return true
	}
	if s.big[g.Key] {
		return true
	}
	for _, tensor := range g.Tensors {
		if tensor != nil && s.diamT[tensor.Name] {
			return true
		}
	}
	return false
}

func (s *layersState) walkDecoder(m *model.Model, layers []int, depth int, anc uint32, last bool) {
	path := "dec"
	if len(s.rows) >= visibleMax {
		return
	}
	s.rows = append(s.rows, lrow{
		kind: rowGroup, path: path, gidx: -1, depth: depth, anc: anc, last: last,
		match: s.matches[path],
	})
	if !s.isOpen(path) {
		return
	}
	nextAnc := anc
	if depth < 31 && last {
		nextAnc |= 1 << uint(depth)
	}
	s.addIndexed(m, path, layers, depth+1, nextAnc, func(i int, depth int, anc uint32, last bool) {
		s.walkGroup(m, i, depth, anc, last, path)
	})
}

func (s *layersState) groupOrder(m *model.Model) []int {
	idx := make([]int, 0, len(m.Groups))
	for i, g := range m.Groups {
		if g != nil {
			idx = append(idx, i)
		}
	}
	if s.sortBy == layerSortIndex {
		return idx
	}
	sort.SliceStable(idx, func(a, b int) bool {
		ga, gb := m.Groups[idx[a]], m.Groups[idx[b]]
		switch s.sortBy {
		case layerSortSize:
			if ga.Bytes != gb.Bytes {
				if s.sortDesc {
					return ga.Bytes > gb.Bytes
				}
				return ga.Bytes < gb.Bytes
			}
		default:
			na, nb := strings.ToLower(ga.Key), strings.ToLower(gb.Key)
			if na != nb {
				if s.sortDesc {
					return na > nb
				}
				return na < nb
			}
		}
		return idx[a] < idx[b]
	})
	return idx
}

func (s *layersState) addIndexed(m *model.Model, path string, idxs []int, depth int, anc uint32, walk func(i int, depth int, anc uint32, last bool)) {
	total := len(idxs)
	start, count, paged := s.span(path, total, 64)
	if paged && start > 0 && len(s.rows) < visibleMax {
		s.rows = append(s.rows, lrow{
			kind: rowMore, path: path + "/lead", parent: path, depth: depth, anc: anc,
			more: true, lead: true, hideN: start, gidx: -1,
		})
	}
	end := start + count
	if end > total {
		end = total
	}
	for n, i := range idxs[start:end] {
		if len(s.rows) >= visibleMax {
			return
		}
		last := n == end-start-1 && (!paged || end >= total)
		walk(i, depth, anc, last)
	}
	if paged && end < total && len(s.rows) < visibleMax {
		s.rows = append(s.rows, lrow{
			kind: rowMore, path: path + "/more", parent: path, depth: depth, anc: anc,
			last: true, more: true, hideN: total - end, gidx: -1,
		})
	}
}

func (s *layersState) span(path string, total, threshold int) (start, count int, paged bool) {
	if total <= threshold || total <= 0 {
		return 0, total, false
	}
	w := s.page[path]
	if w.all {
		count = total
		if count > pageAllMax {
			count = pageAllMax
		}
		return 0, count, true
	}
	if w.n <= 0 {
		w.n = pageSize
	}
	if w.n > pageAllMax {
		w.n = pageAllMax
	}
	if w.start < 0 {
		w.start = 0
	}
	if w.start >= total {
		w.start = 0
	}
	if w.start+w.n > total {
		w.n = total - w.start
	}
	return w.start, w.n, true
}

func (s *layersState) walkGroup(m *model.Model, gidx int, depth int, anc uint32, last bool, parent string) {
	if gidx < 0 || gidx >= len(m.Groups) || m.Groups[gidx] == nil {
		return
	}
	g := m.Groups[gidx]
	path := groupPath(g)
	if len(s.rows) >= visibleMax {
		return
	}
	s.rows = append(s.rows, lrow{
		kind: rowGroup, path: path, parent: parent, gidx: gidx, depth: depth, anc: anc, last: last,
		match: s.matches[path],
	})
	if !s.isOpen(path) {
		return
	}
	nextAnc := anc
	if depth < 31 && last {
		nextAnc |= 1 << uint(depth)
	}
	packs := s.packs(m, gidx)
	if g.Kind == model.KindLayer && len(packs) > 1 {
		s.addLayerKids(m, gidx, path, packs, depth+1, nextAnc)
		return
	}
	var rows []model.LogicalTensor
	sub := ""
	if len(packs) == 1 {
		rows = packs[0].rows
		sub = ""
	}
	s.addTensors(path, sub, rows, depth+1, nextAnc)
}

func (s *layersState) addLayerKids(m *model.Model, gidx int, parent string, packs []tensorPack, depth int, anc uint32) {
	var moe []tensorPack
	var rest []tensorPack
	for _, p := range packs {
		switch p.sub {
		case "router", "shared", "experts":
			moe = append(moe, p)
		default:
			rest = append(rest, p)
		}
	}
	type kid struct {
		moe  bool
		pack *tensorPack
	}
	var kids []kid
	for i := range rest {
		if rest[i].sub == "attn" {
			kids = append(kids, kid{pack: &rest[i]})
		}
	}
	if len(moe) > 0 {
		kids = append(kids, kid{moe: true})
	}
	for i := range rest {
		if rest[i].sub != "attn" {
			kids = append(kids, kid{pack: &rest[i]})
		}
	}
	for j, k := range kids {
		if len(s.rows) >= visibleMax {
			return
		}
		last := j == len(kids)-1
		if k.moe {
			s.walkMoe(m, gidx, parent, moe, depth, anc, last)
			continue
		}
		s.walkSub(m, gidx, parent, *k.pack, depth, anc, last)
	}
}

func (s *layersState) walkMoe(m *model.Model, gidx int, parent string, packs []tensorPack, depth int, anc uint32, last bool) {
	path := parent + "/s:moe"
	if s.keep != nil && !s.keep[path] {
		// Still show moe when a child part matched and the keep set named it.
		child := false
		for _, p := range packs {
			if s.keep[parent+"/s:"+p.sub] || s.keep[path+"/s:"+p.sub] {
				child = true
			}
		}
		if !child {
			return
		}
	}
	if len(s.rows) >= visibleMax {
		return
	}
	s.rows = append(s.rows, lrow{
		kind: rowSub, path: path, parent: parent, gidx: gidx, sub: "moe",
		depth: depth, anc: anc, last: last, match: s.matches[path],
	})
	if !s.isOpen(path) {
		return
	}
	nextAnc := anc
	if depth < 31 && last {
		nextAnc |= 1 << uint(depth)
	}
	s.addPacks(m, gidx, path, packs, depth+1, nextAnc)
}

func (s *layersState) addPacks(m *model.Model, gidx int, parent string, packs []tensorPack, depth int, anc uint32) {
	idxs := make([]int, 0, len(packs))
	hidden := 0
	for i, p := range packs {
		path := parent + "/s:" + p.sub
		if s.keep != nil && !s.keep[path] {
			hidden++
			continue
		}
		idxs = append(idxs, i)
	}
	if s.keep != nil {
		for j, i := range idxs {
			if len(s.rows) >= visibleMax {
				return
			}
			s.walkSub(m, gidx, parent, packs[i], depth, anc, j == len(idxs)-1 && hidden == 0)
		}
		if hidden > 0 && len(s.rows) < visibleMax {
			s.rows = append(s.rows, lrow{
				kind: rowMore, hidden: true, hideN: hidden, depth: depth, anc: anc, last: true,
				path: parent + "/hidden", parent: parent, gidx: gidx,
			})
		}
		return
	}
	s.addIndexed(m, parent, idxs, depth, anc, func(i int, depth int, anc uint32, last bool) {
		s.walkSub(m, gidx, parent, packs[i], depth, anc, last)
	})
}

func (s *layersState) walkSub(m *model.Model, gidx int, parent string, p tensorPack, depth int, anc uint32, last bool) {
	path := parent + "/s:" + p.sub
	if len(s.rows) >= visibleMax {
		return
	}
	s.rows = append(s.rows, lrow{
		kind: rowSub, path: path, parent: parent, gidx: gidx, sub: p.sub,
		depth: depth, anc: anc, last: last, match: s.matches[path],
	})
	if !s.isOpen(path) {
		return
	}
	nextAnc := anc
	if depth < 31 && last {
		nextAnc |= 1 << uint(depth)
	}
	s.addTensors(path, p.sub, p.rows, depth+1, nextAnc)
}

func (s *layersState) addTensors(parent, sub string, rows []model.LogicalTensor, depth int, anc uint32) {
	idxs := make([]int, 0, len(rows))
	hidden := 0
	for i := range rows {
		path := parent + "/t:" + strconv.Itoa(i)
		if s.keep != nil && !s.keep[path] {
			hidden++
			continue
		}
		idxs = append(idxs, i)
	}
	write := func(i int, depth int, anc uint32, last bool) {
		if len(s.rows) >= visibleMax {
			return
		}
		path := parent + "/t:" + strconv.Itoa(i)
		s.rows = append(s.rows, lrow{
			kind: rowTensor, path: path, parent: parent, gidx: -1, sub: sub, tidx: i,
			depth: depth, anc: anc, last: last, match: s.matches[path],
		})
		// gidx is recovered from the parent path when rendering.
		if n := len(s.rows); n > 0 {
			s.rows[n-1].gidx = s.gidxOf(parent)
		}
	}
	if s.keep != nil {
		for j, i := range idxs {
			write(i, depth, anc, j == len(idxs)-1 && hidden == 0)
		}
		if hidden > 0 && len(s.rows) < visibleMax {
			s.rows = append(s.rows, lrow{
				kind: rowMore, hidden: true, hideN: hidden, depth: depth, anc: anc, last: true,
				path: parent + "/hidden", parent: parent, gidx: s.gidxOf(parent),
			})
		}
		return
	}
	// addIndexed walks group indexes. Tensor indexes are the same shape.
	s.addTensorIdx(parent, idxs, depth, anc, write)
}

func (s *layersState) gidxOf(path string) int {
	// parent paths are "g:<key>/..." — the group index is not in the path.
	// Callers pass gidx on the sub row. For tensors, walk stored it via gidxOf
	// only when we already know it. Fall back to scanning rows is unnecessary:
	// walkSub sets gidx before addTensors. Direct tensors pass through walkGroup,
	// which does not set a field. Store it on the closure by looking up the
	// group key.
	key := path
	if i := strings.Index(key, "/"); i >= 0 {
		key = key[:i]
	}
	for _, row := range s.rows {
		if row.kind == rowGroup && row.path == key {
			return row.gidx
		}
	}
	return -1
}

func (s *layersState) addTensorIdx(parent string, idxs []int, depth int, anc uint32, write func(i int, depth int, anc uint32, last bool)) {
	total := len(idxs)
	start, count, paged := s.span(parent, total, 64)
	if paged && start > 0 && len(s.rows) < visibleMax {
		s.rows = append(s.rows, lrow{
			kind: rowMore, path: parent + "/lead", parent: parent, depth: depth, anc: anc,
			more: true, lead: true, hideN: start, gidx: s.gidxOf(parent),
		})
	}
	end := start + count
	if end > total {
		end = total
	}
	for n, i := range idxs[start:end] {
		if len(s.rows) >= visibleMax {
			return
		}
		last := n == end-start-1 && (!paged || end >= total)
		write(i, depth, anc, last)
	}
	if paged && end < total && len(s.rows) < visibleMax {
		s.rows = append(s.rows, lrow{
			kind: rowMore, path: parent + "/more", parent: parent, depth: depth, anc: anc,
			last: true, more: true, hideN: total - end, gidx: s.gidxOf(parent),
		})
	}
}

func (s *layersState) packs(m *model.Model, gidx int) []tensorPack {
	if s.pack != nil {
		if p, ok := s.pack[gidx]; ok {
			return p
		}
	}
	var out []tensorPack
	if m == nil || gidx < 0 || gidx >= len(m.Groups) || m.Groups[gidx] == nil {
		out = nil
	} else if s.parts {
		out = storedPacks(m.Groups[gidx])
	} else {
		g := m.Groups[gidx]
		logical := g.LogicalTensors(m)
		if g.Kind != model.KindLayer {
			out = []tensorPack{{rows: logical}}
		} else {
			buckets := map[string][]model.LogicalTensor{}
			var seenOrder []string
			for _, row := range logical {
				sub, _ := model.Sublayer(row.FullName)
				if _, ok := buckets[sub]; !ok {
					seenOrder = append(seenOrder, sub)
				}
				buckets[sub] = append(buckets[sub], row)
			}
			used := map[string]bool{}
			for _, name := range subOrder {
				if rows, ok := buckets[name]; ok {
					out = append(out, tensorPack{sub: name, rows: rows})
					used[name] = true
				}
			}
			for _, name := range seenOrder {
				if !used[name] {
					out = append(out, tensorPack{sub: name, rows: buckets[name]})
				}
			}
		}
	}
	if s.pack == nil {
		s.pack = map[int][]tensorPack{}
	}
	s.pack[gidx] = out
	return out
}

func storedPacks(g *model.Group) []tensorPack {
	if g == nil {
		return nil
	}
	if g.Kind != model.KindLayer {
		var rows []model.LogicalTensor
		for _, tensor := range g.Tensors {
			if tensor != nil {
				rows = append(rows, storedLogical(tensor))
			}
		}
		return []tensorPack{{rows: rows}}
	}
	buckets := map[string][]model.LogicalTensor{}
	var seenOrder []string
	for _, tensor := range g.Tensors {
		if tensor == nil {
			continue
		}
		sub, _ := model.Sublayer(tensor.Name)
		if _, ok := buckets[sub]; !ok {
			seenOrder = append(seenOrder, sub)
		}
		buckets[sub] = append(buckets[sub], storedLogical(tensor))
	}
	var out []tensorPack
	used := map[string]bool{}
	for _, name := range subOrder {
		if rows, ok := buckets[name]; ok {
			out = append(out, tensorPack{sub: name, rows: rows})
			used[name] = true
		}
	}
	for _, name := range seenOrder {
		if !used[name] {
			out = append(out, tensorPack{sub: name, rows: buckets[name]})
		}
	}
	return out
}

func storedLogical(t *model.Tensor) model.LogicalTensor {
	role := "plain"
	switch t.Role {
	case model.RoleQuantWeight:
		if t.Bits > 0 {
			role = fmt.Sprintf("q%d", t.Bits)
		} else {
			role = "q"
		}
	case model.RoleQuantScales:
		role = "qscal"
	case model.RoleQuantBiases:
		role = "qbias"
	}
	n := t.ByteLen()
	if n < 0 {
		n = 0
	}
	return model.LogicalTensor{
		Name: t.Name, FullName: t.Name, DType: t.DType, Shape: t.Shape,
		Shards: 1, Bytes: n, Params: t.LogicalParams, Role: role, Shard: t.Shard,
	}
}

func (s *layersState) prep(m *model.Model) {
	if s.prepped || m == nil {
		return
	}
	s.prepped = true
	s.info = m.LayerInfoBy()
	s.diamT = map[string]bool{}
	for _, t := range m.Tensors {
		if t == nil {
			continue
		}
		if unexpectedPrecision(m, t) {
			s.diamT[t.Name] = true
		}
	}
	s.perTok = 0
	for _, kv := range m.KeyConfig() {
		if kv.Key != "num_experts_per_tok" {
			continue
		}
		switch n := kv.Value.(type) {
		case int:
			s.perTok = n
		case int64:
			s.perTok = int(n)
		case float64:
			s.perTok = int(n)
		}
	}
	s.noteSizeOutliers(m)
}

func (s *layersState) noteSizeOutliers(m *model.Model) {
	s.big = map[string]bool{}
	var vals []int64
	for _, g := range m.Groups {
		if g != nil && g.Kind == model.KindLayer && g.Bytes > 0 {
			vals = append(vals, g.Bytes)
		}
	}
	if len(vals) == 0 {
		return
	}
	sort.Slice(vals, func(a, b int) bool { return vals[a] < vals[b] })
	med := vals[len(vals)/2]
	if med <= 0 || med > (1<<62)/3 {
		return
	}
	for _, g := range m.Groups {
		if g != nil && g.Kind == model.KindLayer && g.Bytes > 3*med {
			s.big[g.Key] = true
		}
	}
}

func (s *layersState) once(raw string) sanitize.Text {
	if s.san == nil {
		s.san = map[string]sanitize.Text{}
	}
	if t, ok := s.san[raw]; ok {
		return t
	}
	t := sanitize.From(raw)
	s.san[raw] = t
	return t
}

func (t *Model) clampLayers() {
	s := t.lay
	if s == nil {
		return
	}
	n := len(s.rows)
	if n == 0 {
		s.row = 0
		s.scroll = 0
		return
	}
	if s.row < 0 {
		s.row = 0
	}
	if s.row >= n {
		s.row = n - 1
	}
	s.cursorPath = s.rows[s.row].path
	page := s.viewH
	if page < 1 {
		page = t.bodyHeight() - 4
	}
	if page < 1 {
		page = 1
	}
	if s.row < s.scroll {
		s.scroll = s.row
	}
	if s.row >= s.scroll+page {
		s.scroll = s.row - page + 1
	}
	if s.scroll < 0 {
		s.scroll = 0
	}
}

func (t *Model) layersMove(d int) {
	if t.lay == nil {
		return
	}
	t.lay.ensure(t.m)
	t.lay.row += d
	t.clampLayers()
	t.touchLayers()
}

func (t Model) layersPage() int {
	if t.lay != nil && t.lay.viewH > 1 {
		return t.lay.viewH - 1
	}
	n := t.bodyHeight() - 4
	if n < 1 {
		return 1
	}
	return n
}

func (t Model) currentLayer() lrow {
	s := t.lay
	if s == nil || s.row < 0 || s.row >= len(s.rows) {
		return lrow{gidx: -1}
	}
	return s.rows[s.row]
}

func (t Model) layersGroup() *model.Group {
	if t.m == nil || t.lay == nil {
		return nil
	}
	row := t.currentLayer()
	if row.gidx < 0 || row.gidx >= len(t.m.Groups) {
		return nil
	}
	return t.m.Groups[row.gidx]
}

func (t *Model) layersEnter(all bool) {
	s := t.lay
	if s == nil {
		return
	}
	s.ensure(t.m)
	if len(s.rows) == 0 {
		return
	}
	row := s.rows[s.row]
	if row.kind == rowMore {
		if row.hidden {
			return
		}
		t.layersMore(row, all)
		return
	}
	if row.kind == rowTensor {
		br := ui.Breakpoint(t.width, t.height)
		if br == "S" || br == "XS" {
			s.drill = true
		} else {
			t.focusSecondary = true
		}
		t.touchLayers()
		return
	}
	open := s.isOpen(row.path)
	s.setOpen(row.path, !open)
	if !open && row.kind == rowGroup && row.gidx >= 0 {
		s.expanded[row.path+"/s:moe"] = true
	}
	s.ensure(t.m)
	t.clampLayers()
	t.touchLayers()
}

func (t *Model) layersExpand() {
	s := t.lay
	s.ensure(t.m)
	if len(s.rows) == 0 {
		return
	}
	row := s.rows[s.row]
	if row.kind == rowMore {
		t.layersMore(row, false)
		return
	}
	if row.kind != rowTensor && !s.isOpen(row.path) && t.layerKids(row) {
		s.setOpen(row.path, true)
		if row.kind == rowGroup && row.gidx >= 0 {
			s.expanded[row.path+"/s:moe"] = true
		}
		s.ensure(t.m)
		t.clampLayers()
		t.touchLayers()
		return
	}
	if row.kind != rowTensor && s.isOpen(row.path) {
		if s.row+1 < len(s.rows) && s.rows[s.row+1].depth > row.depth {
			s.row++
			t.clampLayers()
			t.touchLayers()
		}
	}
}

func (t *Model) layersCollapse() {
	s := t.lay
	s.ensure(t.m)
	if len(s.rows) == 0 {
		return
	}
	row := s.rows[s.row]
	if row.kind != rowTensor && row.kind != rowMore && s.isOpen(row.path) {
		s.setOpen(row.path, false)
		s.ensure(t.m)
		t.clampLayers()
		t.touchLayers()
		return
	}
	t.layersParent()
}

func (t *Model) layersParent() {
	s := t.lay
	s.ensure(t.m)
	if len(s.rows) == 0 {
		return
	}
	row := s.rows[s.row]
	parent := row.parent
	if parent == "" && row.kind != rowGroup {
		parent = groupKeyPath(row.path)
	}
	if parent == "" {
		return
	}
	for i, r := range s.rows {
		if r.path == parent {
			s.row = i
			t.clampLayers()
			t.touchLayers()
			return
		}
	}
}

func groupKeyPath(path string) string {
	if i := strings.Index(path, "/"); i >= 0 {
		return path[:i]
	}
	return ""
}

func (t Model) layerKids(row lrow) bool {
	if row.path == "dec" || row.sub == "moe" {
		return true
	}
	if t.m == nil || row.gidx < 0 || row.gidx >= len(t.m.Groups) {
		return false
	}
	g := t.m.Groups[row.gidx]
	if row.kind == rowGroup {
		return g != nil && len(g.Tensors) > 0
	}
	if row.kind == rowSub {
		for _, p := range t.lay.packs(t.m, row.gidx) {
			if p.sub == row.sub {
				return len(p.rows) > 0
			}
		}
	}
	return false
}

func (t *Model) layersToggle() {
	s := t.lay
	s.ensure(t.m)
	if len(s.rows) == 0 {
		return
	}
	row := s.rows[s.row]
	if row.kind == rowTensor || row.kind == rowMore {
		return
	}
	s.setOpen(row.path, !s.isOpen(row.path))
	s.ensure(t.m)
	t.clampLayers()
	t.touchLayers()
}

func (t *Model) layersExpandAll() {
	if t.lay == nil || t.m == nil {
		return
	}
	s := t.lay
	s.setOpen("dec", true)
	for _, g := range t.m.Groups {
		if g == nil || len(g.Tensors) > expandArrMax {
			continue
		}
		s.setOpen(groupPath(g), true)
		if g.Kind == model.KindLayer {
			s.setOpen(groupPath(g)+"/s:moe", true)
		}
	}
	s.ensure(t.m)
	// Open visible subs with a modest child count. A second pass picks up
	// the rows the first open created.
	for _, row := range s.rows {
		if row.kind != rowSub {
			continue
		}
		n := 0
		for _, p := range s.packs(t.m, row.gidx) {
			if p.sub == row.sub {
				n = len(p.rows)
			}
		}
		if n > 0 && n <= expandArrMax {
			s.setOpen(row.path, true)
		}
	}
	s.ensure(t.m)
	t.clampLayers()
	t.touchLayers()
}

func (t *Model) layersCollapseAll() {
	if t.lay == nil {
		return
	}
	s := t.lay
	s.expanded = map[string]bool{}
	s.temp = map[string]bool{}
	s.dirty = true
	s.ensure(t.m)
	t.clampLayers()
	t.touchLayers()
}

func (t *Model) layersFold(dir int) {
	s := t.lay
	s.ensure(t.m)
	if dir > 0 {
		for _, row := range append([]lrow(nil), s.rows...) {
			if row.kind == rowTensor || row.kind == rowMore || s.isOpen(row.path) {
				continue
			}
			if t.layerKids(row) {
				s.setOpen(row.path, true)
			}
		}
		t.notice = "level open"
	} else {
		deep := -1
		for _, row := range s.rows {
			if s.isOpen(row.path) && row.depth > deep {
				deep = row.depth
			}
		}
		if deep >= 0 {
			for _, row := range s.rows {
				if row.depth == deep && s.isOpen(row.path) {
					s.setOpen(row.path, false)
				}
			}
		}
		t.notice = "level shut"
	}
	s.ensure(t.m)
	t.clampLayers()
	t.touchLayers()
}

func (t *Model) layersMore(row lrow, all bool) {
	s := t.lay
	parent := row.parent
	w := s.page[parent]
	if row.lead {
		w.start -= pageSize
		if w.start < 0 {
			w.start = 0
		}
		if w.n <= 0 {
			w.n = pageSize
		}
	} else if all {
		w.all = true
		w.start = 0
	} else {
		if w.n <= 0 {
			w.n = pageSize
		}
		w.n += pageSize
		if w.n > pageAllMax {
			w.n = pageAllMax
		}
	}
	if s.page == nil {
		s.page = map[string]pageWin{}
	}
	s.page[parent] = w
	s.dirty = true
	s.ensure(t.m)
	t.clampLayers()
	t.touchLayers()
}

func (t *Model) layersCycleSort() {
	s := t.lay
	if s == nil {
		s = newLayersState()
		t.lay = s
	}
	switch s.sortBy {
	case layerSortIndex:
		s.sortBy, s.sortDesc = layerSortSize, true
	case layerSortSize:
		s.sortBy, s.sortDesc = layerSortParams, true
	case layerSortParams:
		s.sortBy, s.sortDesc = layerSortName, false
	default:
		s.sortBy, s.sortDesc = layerSortIndex, false
	}
	t.notice = "sort: " + t.sortTitle()
	s.dirty = true
	s.ensure(t.m)
	t.clampLayers()
	t.touchLayers()
}

func (t *Model) layersReverseSort() {
	if t.lay == nil {
		return
	}
	t.lay.sortDesc = !t.lay.sortDesc
	t.notice = "sort: " + t.sortTitle()
	t.lay.dirty = true
	t.lay.ensure(t.m)
	t.clampLayers()
	t.touchLayers()
}

func (t Model) sortTitle() string {
	if t.lay == nil {
		return "natural"
	}
	name := "natural"
	switch t.lay.sortBy {
	case layerSortSize:
		name = "bytes"
	case layerSortParams:
		name = "params"
	case layerSortName:
		name = "name"
	}
	arrow := ""
	if t.lay.sortBy != layerSortIndex {
		arrow = " ↓"
		if !t.lay.sortDesc {
			arrow = " ↑"
		}
		if !ui.UTF8() {
			arrow = " v"
			if !t.lay.sortDesc {
				arrow = " ^"
			}
		}
	}
	return name + arrow
}

func (t Model) updateLayersSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := t.lay
	switch msg.String() {
	case "ctrl+c":
		return t, tea.Quit
	case "esc":
		t.cancelLayerSearch()
		return t, nil
	case "enter":
		s.search = false
		s.largeWait = false
		t.runLayerSearch()
		t.touchLayers()
		return t, nil
	case "tab":
		s.filter = !s.filter
		if !s.largeWait {
			t.runLayerSearch()
		}
		t.touchLayers()
		return t, nil
	case "backspace":
		r := []rune(s.query)
		if len(r) > 0 {
			s.query = string(r[:len(r)-1])
		}
		if !s.largeWait {
			t.runLayerSearch()
		}
		t.touchLayers()
		return t, nil
	default:
		if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
			add := string(msg.Runes)
			if msg.Type == tea.KeySpace && add == "" {
				add = " "
			}
			s.query += add
			r := []rune(s.query)
			if len(r) > layersQueryMax {
				s.query = string(r[:layersQueryMax])
			}
			if !s.largeWait {
				t.runLayerSearch()
			}
			t.touchLayers()
		}
		return t, nil
	}
}

func (t *Model) openLayerSearch() {
	s := t.lay
	if s == nil {
		return
	}
	s.snap = &layersSnap{
		expanded: copySet(s.expanded),
		temp:     copySet(s.temp),
		cursor:   s.cursorPath,
		scroll:   s.scroll,
		row:      s.row,
		query:    s.query,
		filter:   s.filter,
	}
	s.search = true
	n := 0
	if t.m != nil {
		n = len(t.m.Tensors)
	}
	s.largeWait = n > 20_000
	t.touchLayers()
}

func (t *Model) cancelLayerSearch() {
	s := t.lay
	if s.snap != nil {
		s.expanded = s.snap.expanded
		s.temp = s.snap.temp
		s.cursorPath = s.snap.cursor
		s.scroll = s.snap.scroll
		s.row = s.snap.row
		s.query = s.snap.query
		s.filter = s.snap.filter
	}
	s.search = false
	s.largeWait = false
	s.snap = nil
	s.dirty = true
	t.touchLayers()
}

func (t *Model) clearLayerSearch() {
	s := t.lay
	s.query = ""
	s.filter = false
	s.search = false
	s.largeWait = false
	s.matches = map[string]bool{}
	s.matchIDs = nil
	s.matchIdx = 0
	s.temp = map[string]bool{}
	s.snap = nil
	s.dirty = true
	t.touchLayers()
}

func (t *Model) runLayerSearch() {
	s := t.lay
	if t.m == nil {
		return
	}
	s.matches = map[string]bool{}
	s.matchIDs = nil
	s.temp = map[string]bool{}
	s.matchIdx = 0
	mode, sub := queryMode(s.query)
	if strings.TrimSpace(sub) == "" {
		s.dirty = true
		return
	}
	order := s.groupOrder(t.m)
	for _, gi := range order {
		g := t.m.Groups[gi]
		if g == nil {
			continue
		}
		gp := groupPath(g)
		name := t.layerGroupLabel(g).String()
		dt := t.layerDType(g).String()
		if layerHit(mode, sub, name, dt) {
			s.noteMatch(gp)
		}
		if s.largeWait {
			continue
		}
		packs := s.packs(t.m, gi)
		multi := g.Kind == model.KindLayer && len(packs) > 1
		for _, p := range packs {
			parent := gp
			if multi {
				parent = gp + "/s:" + p.sub
				switch p.sub {
				case "router", "shared", "experts":
					parent = gp + "/s:moe/s:" + p.sub
					s.noteMatch(gp + "/s:moe")
				}
				if layerHit(mode, sub, subLabel(p.sub).String(), "") || layerHit(mode, sub, subLabel("moe").String(), "") {
					s.noteMatch(parent)
				}
			}
			for i, row := range p.rows {
				tn := t.layerTensorLabel(g, row).String()
				td := tensorBadge(row)
				if layerHit(mode, sub, tn, td) || layerHit(mode, sub, row.FullName, model.ShapeText(row.Shape)) {
					s.noteMatch(parent + "/t:" + strconv.Itoa(i))
				}
			}
		}
	}
	s.dirty = true
	s.ensure(t.m)
	if len(s.matchIDs) > 0 {
		t.revealLayer(s.matchIDs[0])
		s.cursorPath = s.matchIDs[0]
		s.dirty = true
		s.ensure(t.m)
		t.clampLayers()
	}
}

func layerHit(mode int, sub, name, val string) bool {
	switch mode {
	case 1:
		return hasFold(name, sub)
	case 2:
		return hasFold(val, sub)
	default:
		return hasFold(name, sub) || hasFold(val, sub)
	}
}

func (s *layersState) noteMatch(path string) {
	if s.matches[path] {
		return
	}
	s.matches[path] = true
	s.matchIDs = append(s.matchIDs, path)
}

func (t *Model) revealLayer(path string) {
	s := t.lay
	if s.temp == nil {
		s.temp = map[string]bool{}
	}
	rest := path
	for {
		i := strings.LastIndex(rest, "/")
		if i <= 0 {
			break
		}
		rest = rest[:i]
		if _, ok := s.expanded[rest]; ok {
			continue
		}
		s.temp[rest] = true
	}
	s.dirty = true
}

func (t *Model) layersNext(dir int) {
	s := t.lay
	if len(s.matchIDs) == 0 {
		return
	}
	s.matchIdx += dir
	if s.matchIdx < 0 {
		s.matchIdx = len(s.matchIDs) - 1
	}
	if s.matchIdx >= len(s.matchIDs) {
		s.matchIdx = 0
	}
	id := s.matchIDs[s.matchIdx]
	t.revealLayer(id)
	s.cursorPath = id
	s.dirty = true
	s.ensure(t.m)
	t.clampLayers()
	t.touchLayers()
}

func (t Model) layersCloseTop() bool {
	s := t.lay
	if s == nil {
		return false
	}
	if s.jump {
		s.jump = false
		s.jumpQ = ""
		t.touchLayers()
		return true
	}
	if s.drill {
		s.drill = false
		t.touchLayers()
		return true
	}
	if s.search || s.query != "" || s.filter {
		t.clearLayerSearch()
		return true
	}
	return false
}

func (t Model) layersJumping() bool {
	return t.active == tabLayers && t.lay != nil && t.lay.jump
}

func (t Model) updateLayersJump(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := t.lay
	switch msg.String() {
	case "ctrl+c":
		return t, tea.Quit
	case "esc":
		s.jump = false
		s.jumpQ = ""
		t.touchLayers()
		return t, nil
	case "enter":
		t.commitLayerJump()
		return t, nil
	case "backspace":
		r := []rune(s.jumpQ)
		if len(r) > 0 {
			s.jumpQ = string(r[:len(r)-1])
		}
		t.touchLayers()
		return t, nil
	default:
		if msg.Type == tea.KeyRunes {
			for _, r := range msg.Runes {
				if r < '0' || r > '9' {
					continue
				}
				if len(s.jumpQ) < 8 {
					s.jumpQ += string(r)
				}
			}
			t.touchLayers()
		}
		return t, nil
	}
}

func (t *Model) commitLayerJump() {
	s := t.lay
	s.jump = false
	n, err := strconv.Atoi(s.jumpQ)
	s.jumpQ = ""
	if err != nil || t.m == nil {
		t.touchLayers()
		return
	}
	s.expanded["dec"] = true
	for _, g := range t.m.Groups {
		if g != nil && g.Kind == model.KindLayer && g.Layer == n {
			s.expanded[groupPath(g)] = true
			s.cursorPath = groupPath(g)
			break
		}
	}
	s.dirty = true
	s.ensure(t.m)
	t.clampLayers()
	t.touchLayers()
}

func (t *Model) layersToggleOutliers() {
	if t.lay == nil {
		return
	}
	t.lay.outliers = !t.lay.outliers
	t.lay.dirty = true
	if t.lay.outliers {
		t.notice = "outliers"
	} else {
		t.notice = "all rows"
	}
	t.lay.ensure(t.m)
	t.clampLayers()
	t.touchLayers()
}

func (t *Model) layersCycleAttn() {
	if t.lay == nil {
		return
	}
	t.lay.attn = (t.lay.attn + 1) % 3
	t.lay.dirty = true
	switch t.lay.attn {
	case attnLinear:
		t.notice = "attention: linear"
	case attnFull:
		t.notice = "attention: full"
	default:
		t.notice = "attention: all"
	}
	t.lay.ensure(t.m)
	t.clampLayers()
	t.touchLayers()
}

func (t *Model) layersToggleParts() {
	if t.lay == nil {
		return
	}
	t.lay.parts = !t.lay.parts
	t.lay.dirty = true
	if t.lay.parts {
		t.notice = "parts mode"
	} else {
		t.notice = "logical tensors"
	}
	t.lay.ensure(t.m)
	t.clampLayers()
	t.touchLayers()
}

func (t *Model) openLayerJump() {
	if t.lay == nil {
		return
	}
	t.lay.jump = true
	t.lay.jumpQ = ""
	t.touchLayers()
}

func (t Model) updateLayersKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if t.lay == nil {
		return t, nil
	}
	k := msg.String()
	tree := t.layersTreeOn()
	switch k {
	case "up", "k":
		t.layersMove(-1)
	case "down", "j":
		t.layersMove(1)
	case "pgup", "b", "u":
		t.layersMove(-t.layersPage())
	case "pgdown", "f", "d":
		t.layersMove(t.layersPage())
	case "home", "g":
		t.layersMove(-1 << 30)
	case "end", "G":
		t.layersMove(1 << 30)
	case "enter":
		t.layersEnter(false)
	case "shift+enter":
		t.layersEnter(true)
	case "right", "l":
		if tree {
			t.layersExpand()
		}
	case "left", "h":
		if tree {
			t.layersCollapse()
		}
	case " ", "space":
		if tree {
			t.layersToggle()
		}
	case "p":
		if tree {
			t.layersParent()
		}
	case "e":
		if tree {
			t.layersExpandAll()
		}
	case "E":
		if tree {
			t.layersCollapseAll()
		}
	case "+":
		if tree {
			t.layersFold(1)
		}
	case "-":
		if tree {
			t.layersFold(-1)
		}
	case "/":
		t.openLayerSearch()
	case "n":
		if tree {
			t.layersNext(1)
		}
	case "N":
		if tree {
			t.layersNext(-1)
		}
	case "y":
		t.copyLayers(false)
	case "Y":
		t.copyLayers(true)
	case "s":
		if tree {
			t.layersCycleSort()
		}
	case "S":
		if tree {
			t.layersReverseSort()
		}
	case "o":
		if tree {
			t.layersToggleOutliers()
		}
	case "t":
		if tree {
			t.layersCycleAttn()
		}
	case "x":
		if tree {
			t.layersToggleParts()
		}
	case "#":
		if tree {
			t.openLayerJump()
		}
	case "esc":
		if !t.layersCloseTop() && tree {
			t.layersCollapse()
		}
	case "backspace":
		if tree {
			t.layersCollapse()
		}
	}
	return t, nil
}

func (t *Model) copyLayers(value bool) {
	if t.lay == nil || t.m == nil {
		return
	}
	t.lay.ensure(t.m)
	payload, kind := t.layerPayload(value)
	if payload == "" {
		t.notice = "nothing to copy"
		t.touchLayers()
		return
	}
	if len(payload) > configCopyMax {
		t.notice = "too large to copy"
		t.touchLayers()
		return
	}
	if t.clipboardOff() {
		t.notice = "not copied (OSC 52 disabled); select it from the value strip"
		t.touchLayers()
		return
	}
	n := controlCount(payload)
	text := payload
	if n > 0 {
		text = sanitize.From(payload).String()
		t.notice = fmt.Sprintf("copied with %d control characters shown as tokens", n)
	} else if kind == "path" {
		t.notice = "copied path" + cfgSep() + clipCells(payload, 80)
	} else {
		t.notice = "copied value" + cfgSep() + clipCells(payload, 80)
	}
	if !t.canCopy() {
		if n == 0 {
			t.notice = "not copied (terminal blocked OSC 52); select it from the value strip"
		}
		t.touchLayers()
		return
	}
	writeOSC(t.copyWriter(), text)
	t.touchLayers()
}

func (t Model) layerPayload(value bool) (string, string) {
	row := t.currentLayer()
	if row.path == "" || row.kind == rowMore {
		return "", ""
	}
	if !value {
		switch row.kind {
		case rowGroup:
			if g := t.layersGroup(); g != nil {
				return g.Key, "path"
			}
		case rowSub:
			if g := t.layersGroup(); g != nil {
				return g.Key + "." + row.sub, "path"
			}
		case rowTensor:
			if lt, ok := t.layerTensor(row); ok {
				return lt.FullName, "path"
			}
		}
		return "", ""
	}
	switch row.kind {
	case rowTensor:
		lt, ok := t.layerTensor(row)
		if !ok {
			return "", ""
		}
		return rawTensorBadge(lt) + " " + shapeLimited(lt.Shape) + " " + model.HumanBytes(lt.Bytes), "value"
	default:
		g := t.layersGroup()
		if g == nil {
			return "", ""
		}
		return g.StorageLabel() + " " + model.HumanBytes(g.Bytes) + " " + strconv.Itoa(len(g.Tensors)), "value"
	}
}

func (t Model) layerTensor(row lrow) (model.LogicalTensor, bool) {
	if t.lay == nil || t.m == nil || row.gidx < 0 {
		return model.LogicalTensor{}, false
	}
	packs := t.lay.packs(t.m, row.gidx)
	if row.sub != "" {
		for _, p := range packs {
			if p.sub == row.sub && row.tidx >= 0 && row.tidx < len(p.rows) {
				return p.rows[row.tidx], true
			}
		}
		return model.LogicalTensor{}, false
	}
	if len(packs) == 1 && row.tidx >= 0 && row.tidx < len(packs[0].rows) {
		return packs[0].rows[row.tidx], true
	}
	return model.LogicalTensor{}, false
}

func (t Model) layersMode() string {
	s := t.lay
	if s == nil {
		return "LAYERS"
	}
	if s.search && s.filter {
		return "FILTER"
	}
	if s.search {
		return "SEARCH"
	}
	if s.filter && s.query != "" {
		return "FILTER"
	}
	br := ui.Breakpoint(t.width, t.height)
	if s.jump {
		return "JUMP"
	}
	if s.drill || ((br == "M" || br == "L") && !t.zoom && t.focusSecondary) {
		return "DETAIL"
	}
	if t.layersTreeOn() {
		return "LAYERS" + cfgSep() + "TREE"
	}
	return "LAYERS"
}

func (t Model) layersHintSegs() []string {
	s := t.lay
	if s != nil && s.search {
		if s.filter {
			return []string{"type", cfgEnter() + " keep", "n/N"}
		}
		return []string{"type", "tab filter", cfgEnter() + " keep"}
	}
	if t.layersTreeOn() {
		if ui.UTF8() {
			return []string{"←→ fold", "␣ toggle", "s sort", "o outliers", "t attn"}
		}
		return []string{"h/l fold", "space toggle", "s sort", "o outliers", "t attn"}
	}
	if ui.UTF8() {
		return []string{"↑↓ move", "w tree", "? keys"}
	}
	return []string{"up/down move", "w tree", "? keys"}
}

func (t Model) layersClick(id string, double bool) tea.Model {
	if t.lay == nil {
		return t
	}
	if strings.HasPrefix(id, "lay:strip:") {
		n, err := strconv.Atoi(strings.TrimPrefix(id, "lay:strip:"))
		if err != nil {
			return t
		}
		t.lay.jumpQ = strconv.Itoa(n)
		t.commitLayerJump()
		return t
	}
	if strings.HasPrefix(id, "lay:col:") {
		t.layersSortColumn(strings.TrimPrefix(id, "lay:col:"))
		return t
	}
	exp := strings.HasPrefix(id, "lay:exp:")
	if exp {
		id = "lay:" + strings.TrimPrefix(id, "lay:exp:")
	}
	var i int
	if _, err := fmt.Sscanf(id, "lay:%d", &i); err != nil {
		return t
	}
	t.focusSecondary = false
	t.lay.ensure(t.m)
	if i >= 0 && i < len(t.lay.rows) {
		t.lay.row = i
	}
	t.clampLayers()
	if exp {
		t.layersToggle()
		return t
	}
	if double {
		t.layersEnter(false)
	}
	t.touchLayers()
	return t
}

func (t *Model) layersSortColumn(col string) {
	if t.lay == nil {
		return
	}
	s := t.lay
	var next int
	desc := true
	switch col {
	case "bytes":
		next = layerSortSize
	case "params":
		next = layerSortParams
	case "name":
		next = layerSortName
		desc = false
	default:
		return
	}
	if s.sortBy == next {
		s.sortDesc = !s.sortDesc
	} else {
		s.sortBy = next
		s.sortDesc = desc
	}
	t.notice = "sort: " + t.sortTitle()
	s.dirty = true
	s.ensure(t.m)
	t.clampLayers()
	t.touchLayers()
}

func unexpectedPrecision(m *model.Model, t *model.Tensor) bool {
	if m == nil || t == nil || m.Quant == nil || m.Quant.Bits <= 0 {
		return false
	}
	if t.Role != model.RoleQuantWeight || t.Bits <= 0 || t.Bits == m.Quant.Bits {
		return false
	}
	if bits, ok := overrideBits(m, t.Name); ok && bits == t.Bits {
		return false
	}
	if t.Bits == 8 && routerOrGate(t.Name) {
		return false
	}
	return true
}

func overrideBits(m *model.Model, name string) (int, bool) {
	if m == nil || m.Quant == nil || len(m.Quant.Overrides) == 0 {
		return 0, false
	}
	base := name
	low := strings.ToLower(name)
	for _, suf := range []string{".weight", ".scales", ".biases"} {
		if strings.HasSuffix(low, suf) && len(name) >= len(suf) {
			base = name[:len(name)-len(suf)]
			break
		}
	}
	for k, o := range m.Quant.Overrides {
		if o.Bits <= 0 {
			continue
		}
		if k == base || k == name || strings.HasSuffix(base, k) {
			return o.Bits, true
		}
	}
	return 0, false
}

func routerOrGate(name string) bool {
	for _, seg := range strings.Split(strings.ToLower(name), ".") {
		switch seg {
		case "gate", "router", "moe_gate":
			return true
		}
		if strings.HasSuffix(seg, "_gate") || strings.HasSuffix(seg, "_router") || strings.Contains(seg, "router") {
			return true
		}
	}
	return false
}

func subLabel(sub string) sanitize.Text {
	switch sub {
	case "attn":
		return ui.L("attn")
	case "moe":
		return ui.L("moe")
	case "router":
		return ui.L("router")
	case "shared":
		return ui.L("shared expert")
	case "experts":
		return ui.L("routed experts")
	case "norms":
		return ui.L("norms")
	case "hyper":
		return ui.L("hyper")
	case "ple":
		return ui.L("ple")
	default:
		return ui.L("other")
	}
}

func (t Model) layerGroupLabel(g *model.Group) sanitize.Text {
	if g == nil {
		return sanitize.Program("")
	}
	if g.Key == "ngram" {
		label := "n-gram table (PLE)"
		if mark := t.ngramMark(g); mark != "" {
			label += " " + mark + " shards"
		}
		return sanitize.Program(label)
	}
	if g.Kind == model.KindLayer {
		return sanitize.Program(strconv.Itoa(g.Layer))
	}
	if t.lay == nil {
		return sanitize.From(g.Key)
	}
	return t.lay.once(g.Key)
}

func (t Model) layerDType(g *model.Group) sanitize.Text {
	if g == nil {
		return sanitize.Program("")
	}
	label := g.StorageLabel()
	if t.lay == nil {
		return sanitize.From(label)
	}
	return t.lay.once(label)
}

func (t Model) layerTensorLabel(g *model.Group, row model.LogicalTensor) sanitize.Text {
	name := row.Name
	if name == "" {
		name = row.FullName
	}
	if g != nil && g.Kind == model.KindLayer {
		name = model.ShortName(name)
	} else {
		name = tailName(name)
	}
	if t.lay == nil {
		return sanitize.From(name)
	}
	return t.lay.once(name)
}

func tailName(name string) string {
	parts := strings.Split(name, ".")
	if len(parts) <= 2 {
		return name
	}
	return strings.Join(parts[len(parts)-2:], ".")
}

func tensorBadge(row model.LogicalTensor) string {
	if row.Role != "" && row.Role != "plain" {
		return row.Role
	}
	if row.DType == "" {
		return ""
	}
	return sanitize.From(strings.ToLower(row.DType)).String()
}

// rawTensorBadge is the copy payload. The notice is sanitized once later.
func rawTensorBadge(row model.LogicalTensor) string {
	if row.Role != "" && row.Role != "plain" {
		return row.Role
	}
	if row.DType == "" {
		return ""
	}
	return strings.ToLower(row.DType)
}

func shapeLimited(shape []int64) string {
	if len(shape) == 0 {
		return "[]"
	}
	n := len(shape)
	extra := 0
	if n > 8 {
		extra = n - 8
		n = 8
	}
	var b strings.Builder
	b.Grow(n * 4)
	b.WriteByte('[')
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatInt(shape[i], 10))
		if b.Len() > 48 {
			extra += len(shape) - i - 1
			break
		}
	}
	if extra > 0 {
		b.WriteString(",")
		b.WriteString(ui.G().Ellipsis)
	}
	b.WriteByte(']')
	return b.String()
}

func (t Model) groupDiamond(g *model.Group) bool {
	if g == nil || t.lay == nil {
		return false
	}
	for _, tensor := range g.Tensors {
		if tensor != nil && t.lay.diamT[tensor.Name] {
			return true
		}
	}
	return false
}

func (t Model) ngramMark(g *model.Group) string {
	if g == nil || g.Key != "ngram" || t.lay == nil || t.m == nil {
		return ""
	}
	// gidx
	gi := -1
	for i, gg := range t.m.Groups {
		if gg == g {
			gi = i
			break
		}
	}
	if gi < 0 {
		return ""
	}
	packs := t.lay.packs(t.m, gi)
	shards := 0
	host := false
	for _, p := range packs {
		for _, row := range p.rows {
			if row.Shards > shards {
				shards = row.Shards
			}
			if row.Host {
				host = true
			}
		}
	}
	if shards < 1 && !host {
		return ""
	}
	gl := ui.G()
	var b strings.Builder
	if host {
		b.WriteString(gl.Host)
		b.WriteByte(' ')
	}
	if shards > 1 || host {
		b.WriteString(ui.Times())
		b.WriteString(strconv.Itoa(shards))
	}
	return b.String()
}
