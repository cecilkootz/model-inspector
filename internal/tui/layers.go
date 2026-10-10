package tui

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"origin.cursor.com/cecilkootz/model-inspector/internal/model"
	"origin.cursor.com/cecilkootz/model-inspector/internal/sanitize"
	"origin.cursor.com/cecilkootz/model-inspector/internal/tui/ui"
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
	layerSortName
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

	sortBy   int
	sortDesc bool

	rows  []lrow
	dirty bool
	gen   int

	prepped bool
	info    map[int]model.LayerInfo
	diamT   map[string]bool
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
	return fmt.Sprintf("|ly %d %d %d %q %v %v %d %d %d",
		s.row, s.scroll, s.gen, s.query, s.search, s.filter, s.matchIdx, s.sortBy, boolInt(s.sortDesc))
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
	order := s.groupOrder(m)
	if s.keep != nil {
		var idxs []int
		hidden := 0
		for _, i := range order {
			g := m.Groups[i]
			if g != nil && s.keep[groupPath(g)] {
				idxs = append(idxs, i)
			} else {
				hidden++
			}
		}
		for j, i := range idxs {
			if len(s.rows) >= visibleMax {
				return
			}
			s.walkGroup(m, i, 0, 0, j == len(idxs)-1 && hidden == 0)
		}
		if hidden > 0 && len(s.rows) < visibleMax {
			s.rows = append(s.rows, lrow{kind: rowMore, hidden: true, hideN: hidden, depth: 0, last: true, path: "/hidden", gidx: -1})
		}
		return
	}
	s.addIndexed(m, "", order, 0, 0, func(i int, depth int, anc uint32, last bool) {
		s.walkGroup(m, i, depth, anc, last)
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

func (s *layersState) walkGroup(m *model.Model, gidx int, depth int, anc uint32, last bool) {
	if gidx < 0 || gidx >= len(m.Groups) || m.Groups[gidx] == nil {
		return
	}
	g := m.Groups[gidx]
	path := groupPath(g)
	if len(s.rows) >= visibleMax {
		return
	}
	s.rows = append(s.rows, lrow{
		kind: rowGroup, path: path, gidx: gidx, depth: depth, anc: anc, last: last,
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
		s.addPacks(m, gidx, path, packs, depth+1, nextAnc)
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
		return
	}
	open := s.isOpen(row.path)
	s.setOpen(row.path, !open)
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
	s.ensure(t.m)
	for _, g := range t.m.Groups {
		if g == nil || len(g.Tensors) > expandArrMax {
			continue
		}
		s.setOpen(groupPath(g), true)
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
		t.notice = "sort: size"
	case layerSortSize:
		s.sortBy, s.sortDesc = layerSortName, false
		t.notice = "sort: name"
	default:
		s.sortBy, s.sortDesc = layerSortIndex, false
		t.notice = "sort: index"
	}
	s.dirty = true
	s.ensure(t.m)
	t.clampLayers()
	t.touchLayers()
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
				if layerHit(mode, sub, subLabel(p.sub).String(), "") {
					s.noteMatch(parent)
				}
			}
			for i, row := range p.rows {
				tn := t.layerTensorLabel(g, row).String()
				td := t.lay.once(tensorBadge(row)).String()
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
	if s.search || s.query != "" || s.filter {
		t.clearLayerSearch()
		return true
	}
	return false
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
		return tensorBadge(lt) + " " + shapeLimited(lt.Shape) + " " + model.HumanBytes(lt.Bytes), "value"
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
	if (br == "M" || br == "L") && !t.zoom && t.focusSecondary {
		return "LAYERS" + cfgSep() + "DETAIL"
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
			return []string{"←→ fold", "␣ toggle", "/ search", "y/Y copy"}
		}
		return []string{"h/l fold", "space toggle", "/ search", "y/Y copy"}
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
	if double {
		t.layersEnter(false)
	}
	t.touchLayers()
	return t
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
		return ui.L("attention")
	case "router":
		return ui.L("router")
	case "shared":
		return ui.L("shared exp")
	case "experts":
		return ui.L("routed exp")
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
		return ui.L("n-gram table (PLE)")
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
	}
	if t.lay == nil {
		return sanitize.From(name)
	}
	return t.lay.once(name)
}

func tensorBadge(row model.LogicalTensor) string {
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
