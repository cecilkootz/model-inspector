package tui

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/rivo/uniseg"

	"github.com/cecilkootz/model-inspector/internal/jsonx"
	"github.com/cecilkootz/model-inspector/internal/sanitize"
	"github.com/cecilkootz/model-inspector/internal/tui/ui"
)

const (
	pageSize   = 100
	pageAllMax = 5000
	visibleMax = 100_000
)

func (c *configState) ensureRows() {
	if c == nil {
		return
	}
	if c.dirty || c.rows == nil {
		c.rebuild()
	}
}

func (c *configState) rebuild() {
	c.rows = c.rows[:0]
	c.dirty = false
	if c.tree == nil || len(c.tree.Nodes) == 0 {
		return
	}
	if c.filter && c.query != "" {
		c.markKeep()
	} else {
		c.keep = nil
	}
	c.walk(0, 0, 0, true)
	if c.row >= len(c.rows) {
		c.row = len(c.rows) - 1
	}
	if c.row < 0 {
		c.row = 0
	}
}

// keep is the filter inclusion set. It is not stored on the state between
// rebuilds except here, so a search clear drops it with the rows.
func (c *configState) markKeep() {
	c.keep = map[int32]bool{}
	for _, id := range c.matches {
		for p := id; p >= 0; p = c.tree.Nodes[p].Parent {
			if c.keep[p] {
				break
			}
			c.keep[p] = true
		}
	}
}

func (c *configState) walk(id int32, depth int, anc uint32, last bool) {
	if len(c.rows) >= visibleMax {
		return
	}
	row := crow{id: id, depth: uint8(depth), anc: anc, last: last}
	if c.matchSet != nil && c.matchSet[id] {
		row.match = true
	}
	c.rows = append(c.rows, row)
	if !c.isOpen(id) {
		return
	}
	nextAnc := anc
	if depth < 31 && last {
		nextAnc |= 1 << uint(depth)
	}
	c.addChildren(id, depth, nextAnc)
}

func (c *configState) addChildren(id int32, depth int, anc uint32) {
	n := c.tree.Nodes[id]
	if n.First < 0 {
		return
	}
	nextAnc := anc
	if c.keep != nil {
		hidden := 0
		shown := 0
		var lastID int32 = -1
		for ch := n.First; ch >= 0; ch = c.tree.Nodes[ch].Next {
			if !c.keep[ch] {
				hidden++
				continue
			}
			lastID = ch
		}
		for ch := n.First; ch >= 0; ch = c.tree.Nodes[ch].Next {
			if !c.keep[ch] {
				continue
			}
			if len(c.rows) >= visibleMax {
				return
			}
			shown++
			if shown > pageAllMax {
				break
			}
			c.walk(ch, depth+1, nextAnc, ch == lastID && hidden == 0)
		}
		if hidden > 0 && len(c.rows) < visibleMax {
			c.rows = append(c.rows, crow{
				id: id, depth: uint8(depth + 1), anc: nextAnc, last: true,
				hidden: true, hideN: hidden,
			})
		}
		return
	}
	start, count, total, paged := c.childSpan(id)
	if paged && start > 0 && len(c.rows) < visibleMax {
		c.rows = append(c.rows, crow{
			id: id, depth: uint8(depth + 1), anc: nextAnc,
			more: true, lead: true, hideN: start,
		})
	}
	idx := 0
	end := start + count
	var ids []int32
	for ch := n.First; ch >= 0; ch = c.tree.Nodes[ch].Next {
		if idx >= start && idx < end {
			ids = append(ids, ch)
		}
		idx++
		if idx >= end {
			break
		}
	}
	for i, ch := range ids {
		if len(c.rows) >= visibleMax {
			return
		}
		last := i == len(ids)-1 && (!paged || start+count >= total)
		c.walk(ch, depth+1, nextAnc, last)
	}
	if paged && start+count < total && len(c.rows) < visibleMax {
		c.rows = append(c.rows, crow{
			id: id, depth: uint8(depth + 1), anc: nextAnc, last: true,
			more: true, hideN: total - (start + count),
		})
	}
}

func (c *configState) childSpan(id int32) (start, count, total int, paged bool) {
	n := c.tree.Nodes[id]
	total = int(n.Len)
	if total < 0 {
		total = 0
	}
	paged = (n.Kind == jsonx.KindArray && total > 32) || (n.Kind == jsonx.KindObject && total > 64)
	if !paged {
		return 0, total, total, false
	}
	w := c.page[c.path(id)]
	if w.all {
		count = total
		if count > pageAllMax {
			count = pageAllMax
		}
		return 0, count, total, true
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
	return w.start, w.n, total, true
}

func (c *configState) isOpen(id int32) bool {
	if c.tree == nil || id < 0 || int(id) >= len(c.tree.Nodes) {
		return false
	}
	n := c.tree.Nodes[id]
	if n.Kind != jsonx.KindObject && n.Kind != jsonx.KindArray {
		return false
	}
	if c.inline(id) {
		return false
	}
	p := c.path(id)
	if v, ok := c.expanded[p]; ok {
		return v
	}
	if v, ok := c.temp[p]; ok {
		return v
	}
	return c.defaultOpen(id)
}

func (c *configState) defaultOpen(id int32) bool {
	n := c.tree.Nodes[id]
	if n.Parent < 0 {
		return true
	}
	if n.Depth == 1 && n.Kind == jsonx.KindObject && n.Len <= 64 && !closedName(n.Key) {
		return true
	}
	return false
}

func closedName(key string) bool {
	if key == "quantization" || key == "vision_config" {
		return true
	}
	return strings.HasSuffix(key, "_config") && key != "text_config"
}

func (c *configState) inline(id int32) bool {
	n := c.tree.Nodes[id]
	if n.Kind != jsonx.KindArray || n.Len <= 0 || n.Len > 8 {
		return false
	}
	for ch := n.First; ch >= 0; ch = c.tree.Nodes[ch].Next {
		k := c.tree.Nodes[ch].Kind
		if k == jsonx.KindObject || k == jsonx.KindArray {
			return false
		}
	}
	budget := c.innerW
	if budget <= 0 {
		budget = 48
	}
	text := c.inlineJSON(id, budget)
	return ui.Width(text) <= budget
}

func (c *configState) path(id int32) string {
	if c.tree == nil || id <= 0 {
		return ""
	}
	if c.paths == nil {
		c.paths = map[int32]string{}
	}
	if p, ok := c.paths[id]; ok {
		return p
	}
	n := c.tree.Nodes[id]
	parent := ""
	if n.Parent > 0 {
		parent = c.path(n.Parent)
	}
	var p string
	if n.Key == "" && n.Index >= 0 {
		p = parent + fmt.Sprintf("[%d]", n.Index)
	} else if isIdent(n.Key) {
		p = parent + "." + n.Key
	} else {
		b, err := json.Marshal(n.Key)
		if err != nil {
			b = []byte(`""`)
		}
		p = parent + "[" + string(b) + "]"
	}
	c.paths[id] = p
	return p
}

func isIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		ok := r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
		if i > 0 {
			ok = ok || (r >= '0' && r <= '9')
		}
		if !ok {
			return false
		}
	}
	return true
}

func (c *configState) indexOfCursor() int {
	if c.row >= 0 && c.row < len(c.rows) && c.rows[c.row].id == c.cursor && !c.rows[c.row].more && !c.rows[c.row].hidden {
		return c.row
	}
	for i := range c.rows {
		if c.rows[i].id == c.cursor && !c.rows[i].more && !c.rows[i].hidden {
			return i
		}
	}
	if c.row >= 0 && c.row < len(c.rows) {
		return c.row
	}
	return 0
}

func (c *configState) setOpen(id int32, open bool, permanent bool) {
	p := c.path(id)
	if permanent || c.query != "" {
		if c.expanded == nil {
			c.expanded = map[string]bool{}
		}
		c.expanded[p] = open
		delete(c.temp, p)
	} else {
		if c.expanded == nil {
			c.expanded = map[string]bool{}
		}
		c.expanded[p] = open
	}
	c.dirty = true
}

func (t Model) configToggle() {
	c := t.cfg
	c.ensureRows()
	if c.tree == nil || len(c.rows) == 0 {
		return
	}
	i := c.indexOfCursor()
	id := c.rows[i].id
	if id < 0 || c.inline(id) {
		return
	}
	n := c.tree.Nodes[id]
	if n.Kind != jsonx.KindObject && n.Kind != jsonx.KindArray {
		return
	}
	open := c.isOpen(id)
	c.setOpen(id, !open, true)
	c.rebuild()
	t.touchConfig()
}

func (t Model) configExpand() {
	c := t.cfg
	c.ensureRows()
	if len(c.rows) == 0 || c.tree == nil {
		return
	}
	i := c.indexOfCursor()
	id := c.rows[i].id
	n := c.tree.Nodes[id]
	if (n.Kind == jsonx.KindObject || n.Kind == jsonx.KindArray) && !c.inline(id) && !c.isOpen(id) {
		c.setOpen(id, true, true)
		c.rebuild()
		t.touchConfig()
		return
	}
	if c.isOpen(id) {
		// Move to the first child row.
		if i+1 < len(c.rows) && c.rows[i+1].depth > c.rows[i].depth {
			c.row = i + 1
			c.cursor = c.rows[i+1].id
			t.clampTree()
			t.touchConfig()
		}
	}
}

func (t Model) configCollapse() {
	c := t.cfg
	c.ensureRows()
	if len(c.rows) == 0 || c.tree == nil {
		return
	}
	i := c.indexOfCursor()
	id := c.rows[i].id
	n := c.tree.Nodes[id]
	if (n.Kind == jsonx.KindObject || n.Kind == jsonx.KindArray) && c.isOpen(id) {
		c.setOpen(id, false, true)
		c.rebuild()
		t.touchConfig()
		return
	}
	if n.Parent >= 0 {
		c.cursor = n.Parent
		c.dirty = true
		c.ensureRows()
		c.row = c.indexOfCursor()
		t.clampTree()
		t.touchConfig()
	}
}

func (t Model) reveal(id int32) {
	c := t.cfg
	if c.tree == nil || id < 0 || int(id) >= len(c.tree.Nodes) {
		return
	}
	for p := c.tree.Nodes[id].Parent; p >= 0; p = c.tree.Nodes[p].Parent {
		if c.expanded == nil {
			c.expanded = map[string]bool{}
		}
		c.expanded[c.path(p)] = true
		delete(c.temp, c.path(p))
		t.revealPage(p, id)
		id = p
	}
	c.dirty = true
}

func (t Model) revealPage(parent, child int32) {
	c := t.cfg
	n := c.tree.Nodes[parent]
	total := int(n.Len)
	paged := (n.Kind == jsonx.KindArray && total > 32) || (n.Kind == jsonx.KindObject && total > 64)
	if !paged {
		return
	}
	idx := 0
	for ch := n.First; ch >= 0; ch = c.tree.Nodes[ch].Next {
		if ch == child {
			break
		}
		idx++
	}
	w := c.page[c.path(parent)]
	if w.n <= 0 {
		w.n = pageSize
	}
	if idx < w.start || idx >= w.start+w.n {
		w.start = (idx / pageSize) * pageSize
		w.all = false
		c.page[c.path(parent)] = w
	}
}

func (t Model) jumpField(i int) {
	lines := t.fieldLines()
	if i < 0 || i >= len(lines) {
		return
	}
	f := lines[i]
	if f.jump < 0 || t.cfg.tree == nil {
		return
	}
	t.reveal(f.jump)
	t.cfg.cursor = f.jump
	t.cfg.ensureRows()
	t.cfg.row = t.cfg.indexOfCursor()
	h := t.cfg.viewH
	if h < 1 {
		h = t.bodyHeight() / 2
	}
	t.cfg.scroll = t.cfg.row - h/2
	if t.cfg.scroll < 0 {
		t.cfg.scroll = 0
	}
	if t.motion {
		t.cfg.flash = 2
	}
	t.touchConfig()
}

func (c *configState) keyText(id int32) sanitize.Text {
	if c.keySan == nil {
		c.keySan = map[int32]sanitize.Text{}
	}
	if t, ok := c.keySan[id]; ok {
		return t
	}
	t := sanitize.From(c.tree.Nodes[id].Key)
	c.keySan[id] = t
	return t
}

func (c *configState) litText(id int32) sanitize.Text {
	if c.litSan == nil {
		c.litSan = map[int32]sanitize.Text{}
	}
	if t, ok := c.litSan[id]; ok {
		return t
	}
	t := sanitize.From(c.tree.Nodes[id].Lit)
	c.litSan[id] = t
	return t
}

func (c *configState) displayKey(id int32) string {
	raw := c.tree.Nodes[id].Key
	san := c.keyText(id)
	if quoteKey(raw, san) {
		return `"` + san.String() + `"`
	}
	return san.String()
}

func quoteKey(raw string, san sanitize.Text) bool {
	if raw == "" {
		return true
	}
	if san.String() != raw {
		return true
	}
	for _, r := range raw {
		if r == '.' || r == '[' || r == '"' || r == ' ' || r == '\t' {
			return true
		}
	}
	return false
}

func (c *configState) keyWidth(parent int32) int {
	if c.widths == nil {
		c.widths = map[int32]int{}
	}
	if w, ok := c.widths[parent]; ok {
		return w
	}
	max := 0
	n := 0
	first := c.tree.Nodes[parent].First
	for ch := first; ch >= 0 && n < 256; ch = c.tree.Nodes[ch].Next {
		w := uniseg.StringWidth(c.displayKey(ch))
		if w > max {
			max = w
		}
		if max >= 28 {
			max = 28
			break
		}
		n++
	}
	c.widths[parent] = max
	return max
}

func (c *configState) scalarText(id int32) string {
	n := c.tree.Nodes[id]
	switch n.Kind {
	case jsonx.KindString:
		return `"` + c.litText(id).String() + `"`
	case jsonx.KindNumber, jsonx.KindBool, jsonx.KindNull:
		return c.litText(id).String()
	case jsonx.KindError:
		return c.litText(id).String() + " !"
	default:
		return ""
	}
}

func (c *configState) inlineJSON(id int32, budget int) string {
	var b strings.Builder
	b.WriteByte('[')
	first := true
	for ch := c.tree.Nodes[id].First; ch >= 0; ch = c.tree.Nodes[ch].Next {
		if !first {
			b.WriteString(", ")
		}
		first = false
		b.WriteString(c.scalarText(ch))
		if budget > 0 && b.Len() > budget+8 {
			b.WriteString(", ")
			b.WriteString(ui.G().Ellipsis)
			break
		}
	}
	b.WriteByte(']')
	return b.String()
}

func (c *configState) preview(id int32, budget int) string {
	n := c.tree.Nodes[id]
	if n.Kind == jsonx.KindArray && c.inline(id) {
		return c.inlineJSON(id, budget)
	}
	gl := ui.G()
	switch n.Kind {
	case jsonx.KindObject:
		head := fmt.Sprintf("{%d}", n.Len)
		if c.isOpen(id) {
			return head
		}
		var b strings.Builder
		b.WriteString(head)
		for ch := n.First; ch >= 0; ch = c.tree.Nodes[ch].Next {
			chN := c.tree.Nodes[ch]
			if chN.Kind == jsonx.KindObject || chN.Kind == jsonx.KindArray {
				continue
			}
			piece := " " + c.displayKey(ch) + " " + c.scalarText(ch)
			if budget > 0 && uniseg.StringWidth(b.String()+piece) > budget {
				b.WriteString(" ")
				b.WriteString(gl.Ellipsis)
				break
			}
			b.WriteString(piece)
		}
		return b.String()
	case jsonx.KindArray:
		head := fmt.Sprintf("[%d]", n.Len)
		if c.isOpen(id) || n.Len == 0 {
			return head
		}
		if s, ok := c.runLen(id); ok {
			return head + " " + s
		}
		if s, ok := c.numSpan(id); ok {
			return head + " " + s
		}
		var b strings.Builder
		b.WriteString(head)
		b.WriteString(" [")
		count := 0
		for ch := n.First; ch >= 0 && count < 8; ch = c.tree.Nodes[ch].Next {
			if count > 0 {
				b.WriteString(", ")
			}
			kn := c.tree.Nodes[ch].Kind
			if kn == jsonx.KindObject || kn == jsonx.KindArray {
				b.WriteString(gl.Ellipsis)
				break
			}
			b.WriteString(c.scalarText(ch))
			count++
			if budget > 0 && b.Len() > budget {
				b.WriteString(", ")
				b.WriteString(gl.Ellipsis)
				break
			}
		}
		if int(n.Len) > count {
			b.WriteString(", ")
			b.WriteString(gl.Ellipsis)
		}
		b.WriteByte(']')
		return b.String()
	default:
		return c.scalarText(id)
	}
}

func (c *configState) runLen(id int32) (string, bool) {
	n := c.tree.Nodes[id]
	if n.Len == 0 || n.First < 0 {
		return "", false
	}
	type pair struct {
		text string
		n    int
	}
	var order []pair
	idx := map[string]int{}
	for ch := n.First; ch >= 0; ch = c.tree.Nodes[ch].Next {
		kn := c.tree.Nodes[ch]
		if kn.Kind == jsonx.KindObject || kn.Kind == jsonx.KindArray {
			return "", false
		}
		text := c.scalarText(ch)
		if i, ok := idx[text]; ok {
			order[i].n++
			continue
		}
		if len(order) >= 5 {
			return "", false
		}
		idx[text] = len(order)
		order = append(order, pair{text: text, n: 1})
	}
	if len(order) >= 5 {
		return "", false
	}
	sep := " · "
	times := "×"
	if !ui.UTF8() {
		sep = " . "
		times = "x"
	}
	var b strings.Builder
	for i, p := range order {
		if i > 0 {
			b.WriteString(sep)
		}
		fmt.Fprintf(&b, "%s%s%d", p.text, times, p.n)
	}
	return b.String(), true
}

func (c *configState) numSpan(id int32) (string, bool) {
	n := c.tree.Nodes[id]
	var first, last string
	var min, max float64
	var minL, maxL string
	seen := false
	for ch := n.First; ch >= 0; ch = c.tree.Nodes[ch].Next {
		kn := c.tree.Nodes[ch]
		if kn.Kind != jsonx.KindNumber {
			return "", false
		}
		f, ok := jsonx.NumberFits(kn.Lit)
		if !ok {
			return "", false
		}
		if !seen {
			first = c.litText(ch).String()
			min, max = f, f
			minL, maxL = first, first
			seen = true
		}
		last = c.litText(ch).String()
		if f < min {
			min = f
			minL = last
		}
		if f > max {
			max = f
			maxL = last
		}
	}
	if !seen {
		return "", false
	}
	ell := ui.G().Ellipsis
	return fmt.Sprintf("%s %s %s (min %s, max %s)", first, ell, last, minL, maxL), true
}

func magHint(lit string) string {
	if strings.ContainsAny(lit, ".eE+") {
		return ""
	}
	n, err := strconv.ParseInt(lit, 10, 64)
	if err != nil || n < 10_000 {
		return ""
	}
	return magLabel(n)
}

func magLabel(n int64) string {
	if n >= 1024*1024 && n%(1024*1024) == 0 {
		return fmt.Sprintf("%dM", n/(1024*1024))
	}
	if n >= 1024 && n%1024 == 0 {
		return fmt.Sprintf("%dK", n/1024)
	}
	if n >= 1_000_000 && n%1_000_000 == 0 {
		return fmt.Sprintf("%dM", n/1_000_000)
	}
	if n >= 10_000 && n%1000 == 0 {
		return fmt.Sprintf("%dK", n/1000)
	}
	return ""
}

func clipCells(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if uniseg.StringWidth(s) <= width {
		return s
	}
	ell := ui.G().Ellipsis
	ew := uniseg.StringWidth(ell)
	if width <= ew {
		return ell
	}
	var b strings.Builder
	got := 0
	state := -1
	rest := s
	for len(rest) > 0 {
		cluster, next, _, st := uniseg.FirstGraphemeClusterInString(rest, state)
		cw := uniseg.StringWidth(cluster)
		if got+cw+ew > width {
			break
		}
		b.WriteString(cluster)
		got += cw
		rest = next
		state = st
	}
	b.WriteString(ell)
	return b.String()
}

func (c *configState) findChild(parent int32, key string) int32 {
	if c.tree == nil || parent < 0 || int(parent) >= len(c.tree.Nodes) {
		return -1
	}
	for ch := c.tree.Nodes[parent].First; ch >= 0; ch = c.tree.Nodes[ch].Next {
		if c.tree.Nodes[ch].Key == key {
			return ch
		}
	}
	return -1
}

func (t Model) ensureRaw() {
	c := t.cfg
	if c.rawReady || t.m == nil {
		return
	}
	raw := t.m.ConfigRaw
	c.rawCut = len(raw) > configRawMax
	if c.rawCut {
		raw = raw[:configRawMax]
	}
	const maxLines = 100_000
	const maxLine = 8192
	c.rawLines = c.rawLines[:0]
	start := 0
	for i := 0; i <= len(raw) && len(c.rawLines) < maxLines; i++ {
		if i == len(raw) || raw[i] == '\n' {
			line := raw[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			if len(line) > maxLine {
				line = line[:maxLine]
			}
			buf := make([]byte, len(line))
			copy(buf, line)
			c.rawLines = append(c.rawLines, sanitize.From(string(buf)))
			start = i + 1
		}
	}
	c.rawReady = true
}

func (t Model) syncRawLine() {
	c := t.cfg
	if c.tree == nil || t.m == nil || c.cursor < 0 || int(c.cursor) >= len(c.tree.Nodes) {
		return
	}
	off := c.tree.Nodes[c.cursor].Off
	raw := t.m.ConfigRaw
	if int(off) > len(raw) {
		off = int64(len(raw))
	}
	line := 0
	for i := 0; i < int(off) && i < len(raw); i++ {
		if raw[i] == '\n' {
			line++
		}
	}
	c.rawLine = line
	t.clampRaw()
}

func (t *Model) clampRaw() {
	c := t.cfg
	n := len(c.rawLines)
	if c.rawLine < 0 {
		c.rawLine = 0
	}
	if n > 0 && c.rawLine >= n {
		c.rawLine = n - 1
	}
	h := c.viewH
	if h < 1 {
		h = 8
	}
	if c.rawLine < c.rawScroll {
		c.rawScroll = c.rawLine
	}
	if c.rawLine >= c.rawScroll+h {
		c.rawScroll = c.rawLine - h + 1
	}
	if c.rawScroll < 0 {
		c.rawScroll = 0
	}
}
