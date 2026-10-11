package ui

import (
	"math"
	"sort"
	"strings"
)

// HeatRow is one component across decoder layers, plus an optional MTP column.
type HeatRow struct {
	Label  Safe
	Cells  []int64 // 0 means absent
	MTP    int64
	HasMTP bool

	spread []string
	flat   bool
	ready  bool
}

// HeatGrid is the bytes-per-layer grid. Values are on-disk bytes.
type HeatGrid struct {
	Rows    []HeatRow
	Full    []bool // full attention per decoder column
	Layers  []int  // decoder layer number for each column
	Caption Safe
	Note    Safe // shown beside the legend, for example an estimate marker
	// Dim, when non-nil, draws those columns as empty dots. Their bytes
	// stay in the shared scale.
	Dim []bool
	// MarkOn draws a cursor under decoder column Mark.
	MarkOn bool
	Mark   int
}

// Prepare caches each row's spread so a frame does not sort the cells again.
func (h *HeatGrid) Prepare() {
	if h == nil {
		return
	}
	for i := range h.Rows {
		h.Rows[i].prepare()
	}
}

func (r *HeatRow) prepare() {
	if r == nil || r.ready {
		return
	}
	r.spread, r.flat = spreadParts(r.Cells)
	r.ready = true
}

// Cached reports whether the row's spread was computed ahead of drawing.
func (r HeatRow) Cached() bool { return r.ready }

func (r HeatRow) spreadLine() (string, bool) {
	steps, flat := r.spreadSteps()
	return steps[0], flat
}

func (r HeatRow) spreadSteps() ([]string, bool) {
	if r.ready {
		return r.spread, r.flat
	}
	return spreadParts(r.Cells)
}

// fitSpread picks the longest step that fits, or nothing, so a narrow column
// never shows half a figure.
func fitSpread(steps []string, w int) string {
	for _, s := range steps {
		if Width(s) <= w {
			return s
		}
	}
	return ""
}

// Heatmap renders the grid at width. One log2 scale is shared by every row.
func Heatmap(h HeatGrid, width int, large bool) []string {
	h.Prepare()
	if width < 1 {
		return nil
	}
	gl := G()
	n := 0
	if len(h.Rows) > 0 {
		n = len(h.Rows[0].Cells)
	}
	if n < 1 {
		return []string{Fit("BYTES"+SepWord()+"no decoder layers", width)}
	}
	labelW := 12
	spreadW := 18
	if width < 80 {
		labelW = 8
		spreadW = 14
	}
	cw, pool, cols := fitCells(width-labelW-1-spreadW, n, large)
	if cols < 1 {
		cols = 1
	}
	lo, hi := heatDomain(h)
	title := "BYTES" + SepWord() + "LAYER " + Times() + " COMPONENT  log2 scale shared by all rows" + SepWord() + "each colour step = 2" + Times()
	if !h.Note.Empty() {
		title += SepWord() + h.Note.String()
	}
	var lines []string
	lines = append(lines, Fit(title, width))
	if pool > 1 {
		lines = append(lines, Fit("2 layers/cell", width))
	}
	// Column ticks and attention-type header.
	ticks := heatHeader(n, cols, pool, cw, labelW, h.Full, h.Dim, gl)
	lines = append(lines, Fit(ticks, width))
	show := h.Rows
	if !large {
		show = foldFlat(h.Rows)
	}
	var folded []string
	if !large {
		for _, r := range h.Rows {
			if !keepRow(r) {
				text, _ := r.spreadLine()
				folded = append(folded, r.Label.String()+" "+text)
			}
		}
	}
	for _, r := range show {
		lines = append(lines, Fit(heatRow(r, cols, pool, cw, labelW, spreadW, lo, hi, gl, h.Dim, n), width))
	}
	legend := heatLegend(lo, hi, gl, h.Note.String())
	if h.MarkOn {
		legend = markLegend(h.Mark, pool, cw, labelW, legend)
	}
	lines = append(lines, Fit(legend, width))
	if len(folded) > 0 {
		lines = append(lines, Fit("flat  "+strings.Join(folded, SepWord()), width))
	}
	if !h.Caption.Empty() {
		lines = append(lines, Fit(h.Caption.String(), width))
	}
	return lines
}

func keepRow(r HeatRow) bool {
	if strings.Contains(r.Label.String(), "routed") {
		return true
	}
	_, flat := r.spreadLine()
	return !flat
}

func foldFlat(rows []HeatRow) []HeatRow {
	var out []HeatRow
	for _, r := range rows {
		if keepRow(r) {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return rows
	}
	return out
}

func fitCells(room, n int, large bool) (cw, pool, cols int) {
	if n < 1 {
		return 1, 1, 1
	}
	cw = 1
	if large {
		cw = 2
	}
	pool = 1
	cols = n
	for {
		need := cols*cw + 1 + cw // gap + mtp
		if need <= room || (cw == 1 && pool >= n) {
			break
		}
		if cw > 1 {
			cw = 1
			continue
		}
		pool *= 2
		cols = (n + pool - 1) / pool
		if cols < 1 {
			cols = 1
			break
		}
	}
	return cw, pool, cols
}

func heatDomain(h HeatGrid) (lo, hi int) {
	min, max := int64(0), int64(0)
	consider := func(v int64) {
		if v <= 0 {
			return
		}
		if min == 0 || v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	for _, r := range h.Rows {
		for _, c := range r.Cells {
			consider(c)
		}
		if r.HasMTP {
			consider(r.MTP)
		}
	}
	if min <= 0 {
		return 0, 1
	}
	lo = int(math.Floor(math.Log2(float64(min))))
	hi = int(math.Ceil(math.Log2(float64(max))))
	if hi <= lo {
		hi = lo + 1
	}
	return lo, hi
}

// HeatGeom is the cell geometry Heatmap uses for width and n columns.
func HeatGeom(width, n int, large bool) (labelW, cw, pool, cols int) {
	labelW = 12
	spreadW := 18
	if width < 80 {
		labelW = 8
		spreadW = 14
	}
	if n < 1 {
		return labelW, 1, 1, 1
	}
	cw, pool, cols = fitCells(width-labelW-1-spreadW, n, large)
	if cols < 1 {
		cols = 1
	}
	return labelW, cw, pool, cols
}

func colDim(dim []bool, c, pool, n int) bool {
	if len(dim) == 0 {
		return false
	}
	if pool < 1 {
		pool = 1
	}
	any := false
	for k := 0; k < pool; k++ {
		idx := c*pool + k
		if idx >= n {
			break
		}
		any = true
		if idx >= len(dim) || !dim[idx] {
			return false
		}
	}
	return any
}

func markLegend(mark, pool, cw, labelW int, legend string) string {
	if pool < 1 {
		pool = 1
	}
	if mark < 0 {
		mark = 0
	}
	col := mark / pool
	pad := labelW + col*cw
	arrow := G().Size
	if arrow == "" {
		arrow = "^"
	}
	return strings.Repeat(" ", pad) + arrow + " " + legend
}

func heatHeader(n, cols, pool, cw, labelW int, full, dim []bool, gl Glyphs) string {
	var b strings.Builder
	b.WriteString(strings.Repeat(" ", labelW))
	if n < 1 {
		return strings.Repeat(" ", labelW)
	}
	for c := 0; c < cols; c++ {
		idx := c * pool
		if idx >= n {
			idx = n - 1
		}
		if idx < 0 {
			idx = 0
		}
		mark := gl.Linear
		if idx < len(full) && full[idx] {
			mark = gl.Full
		}
		if colDim(dim, c, pool, n) {
			mark = gl.Dot
			if mark == "" {
				mark = "."
			}
		}
		cell := mark
		if cw == 2 {
			cell = mark + " "
		}
		b.WriteString(cell)
	}
	b.WriteString(" ")
	mtp := gl.Full
	if cw == 2 {
		mtp += " "
	}
	b.WriteString(mtp)
	return b.String()
}

func heatRow(r HeatRow, cols, pool, cw, labelW, spreadW, lo, hi int, gl Glyphs, dim []bool, n int) string {
	var b strings.Builder
	lab := r.Label.String()
	if Width(lab) > labelW-1 {
		lab = Trunc(lab, labelW-1, "")
	}
	b.WriteString(PadLabel(lab, labelW))
	vals := poolCells(r.Cells, cols, pool)
	for c, v := range vals {
		b.WriteString(heatCell(v, cw, lo, hi, gl, colDim(dim, c, pool, n)))
	}
	b.WriteString(" ")
	mtp := int64(0)
	if r.HasMTP {
		mtp = r.MTP
	}
	b.WriteString(heatCell(mtp, cw, lo, hi, gl, false))
	b.WriteString("  ")
	steps, _ := r.spreadSteps()
	b.WriteString(fitSpread(steps, spreadW))
	return b.String()
}

func poolCells(cells []int64, cols, pool int) []int64 {
	out := make([]int64, cols)
	for c := 0; c < cols; c++ {
		var m int64
		for k := 0; k < pool; k++ {
			i := c*pool + k
			if i >= len(cells) {
				break
			}
			if cells[i] > m {
				m = cells[i]
			}
		}
		out[c] = m
	}
	return out
}

func heatCell(v int64, cw, lo, hi int, gl Glyphs, dim bool) string {
	if dim || v <= 0 {
		dot := gl.Dot
		if dot == "" {
			dot = "."
		}
		if cw == 2 {
			return dot + dot
		}
		return dot
	}
	if ColorOff() || Profile() == "ansi16" {
		ch := gl.Heat[bucket(v)]
		if Profile() == "ansi16" {
			cols := []string{Ramp2.ANSI, Ramp3.ANSI, Ramp4.ANSI, Ramp5.ANSI}
			ch = paintANSI(ch, cols[bucket(v)])
		}
		if cw == 2 {
			return ch + ch
		}
		return ch
	}
	t := 0.0
	span := float64(hi - lo)
	if span > 0 {
		t = (math.Log2(float64(v)) - float64(lo)) / span
	}
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	hex := heatColor(t)
	block := " "
	if cw == 2 {
		block = "  "
	}
	return paintHex(block, "", hex, false)
}

// heatColor is the heat colour at t. ANSI-256 snaps to the authored ramp
// indices: termenv's nearest-index search on blended hexes flips on
// floating-point differences between platforms.
func heatColor(t float64) string {
	if Profile() == "ansi256" {
		ramp := [...]Token{Ramp1, Ramp2, Ramp3, Ramp4, Ramp5}
		i := int(math.Round(t * float64(len(ramp)-1)))
		if i < 0 {
			i = 0
		}
		if i >= len(ramp) {
			i = len(ramp) - 1
		}
		if Dark() {
			return ramp[i].ANSI256
		}
		return ramp[i].ansi256Light()
	}
	return string(Heat().At(t))
}

func paintANSI(ch, ansi string) string {
	return lipglossPaint(ch, ansi)
}

func bucket(b int64) int {
	const (
		k64  = 64 * 1024
		m4   = 4 * 1024 * 1024
		m256 = 256 * 1024 * 1024
	)
	// A negative size is a saturated overflow, not a tiny tensor.
	if b < 0 {
		return 3
	}
	switch {
	case b < k64:
		return 0
	case b < m4:
		return 1
	case b < m256:
		return 2
	default:
		return 3
	}
}

// pow2 is 2^exp, clamped to the uint64 powers of two. A signed shift of 63
// is MinInt64, which the byte formatter cannot negate.
func pow2(exp int) uint64 {
	if exp <= 0 {
		return 1
	}
	if exp >= 63 {
		return 1 << 63
	}
	return 1 << uint(exp)
}

func heatLegend(lo, hi int, gl Glyphs, note string) string {
	loB := pow2(lo)
	hiB := pow2(hi)
	if ColorOff() || Profile() == "ansi16" {
		edges := "<64K " + gl.Heat[1] + " <4M " + gl.Heat[2] + " <256M " + gl.Heat[3]
		s := humanPow(loB) + "  " + edges + "  " + humanPow(hiB)
		s += "  " + gl.Linear + " linear  " + gl.Full + " full"
		if note != "" {
			s += "  " + note
		}
		return s
	}
	var ramp strings.Builder
	for i := 0; i < 16; i++ {
		hex := heatColor(float64(i) / 15)
		ramp.WriteString(paintHex(" ", "", hex, false))
	}
	return humanPow(loB) + " " + ramp.String() + " " + humanPow(hiB) + "  " + gl.Linear + " linear  " + gl.Full + " full"
}

func humanPow(n uint64) string {
	if n < 1024 {
		return itoaU(n) + " B"
	}
	units := []string{"KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}
	v := float64(n)
	u := -1
	for v >= 1024 && u < len(units)-1 {
		v /= 1024
		u++
	}
	if v >= 10 {
		rounded := uint64(math.Round(v))
		return itoaU(rounded) + " " + units[u]
	}
	return sprintf1(v) + " " + units[u]
}

func itoaU(n uint64) string {
	if n > uint64(^uint(0)>>1) {
		// Wider than int. Format without passing through a signed value.
		var buf [32]byte
		i := len(buf)
		u := n
		for u > 0 {
			i--
			buf[i] = byte('0' + u%10)
			u /= 10
		}
		return string(buf[i:])
	}
	return itoa(int(n))
}

func spreadText(cells []int64) string {
	steps, _ := spreadParts(cells)
	return steps[0]
}

// spreadParts returns the spread label and its shorter forms, longest first.
// Each form drops whole pieces of the one before.
func spreadParts(cells []int64) ([]string, bool) {
	var pos []int64
	for _, v := range cells {
		if v > 0 {
			pos = append(pos, v)
		}
	}
	if len(pos) == 0 {
		if UTF8() {
			return []string{"·"}, true
		}
		return []string{"-"}, true
	}
	min, max := pos[0], pos[0]
	var sum int64
	for _, v := range pos {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
		sum += v
	}
	med := median(pos)
	gl := G()
	if med > 0 && float64(max-min)/float64(med) < 0.01 {
		v := humanPow(uint64(med))
		return []string{v + "  flat", v}, true
	}
	pct := 0
	if med > 0 {
		pct = int(math.Round(float64(max-min) / float64(med) * 100))
	}
	steps := []string{"+" + itoa(pct) + "%"}
	if max > min {
		steps = append([]string{steps[0] + " " + gl.Full}, steps...)
	}
	full := steps[0] + " " + humanPow(uint64(min)) + "-" + humanPow(uint64(max))
	return append([]string{full}, steps...), false
}

func median(v []int64) int64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]int64(nil), v...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	mid := len(s) / 2
	if len(s)%2 == 1 {
		return s[mid]
	}
	return avg2(s[mid-1], s[mid])
}

func avg2(a, b int64) int64 {
	return a/2 + b/2 + (a%2+b%2)/2
}

// LogDomain returns the shared log2 floor and ceil for tests.
func LogDomain(min, max int64) (int, int) {
	if min <= 0 {
		min = 1
	}
	if max < min {
		max = min
	}
	lo := int(math.Floor(math.Log2(float64(min))))
	hi := int(math.Ceil(math.Log2(float64(max))))
	if hi <= lo {
		hi = lo + 1
	}
	return lo, hi
}

// BucketOf exposes the 64× heat buckets.
func BucketOf(b int64) int { return bucket(b) }
