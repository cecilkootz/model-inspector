package tui

import (
	"sort"
	"strconv"
	"strings"

	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/sanitize"
	"github.com/cecilkootz/model-inspector/internal/tui/ui"
)

const (
	sideLayersMax = model.MaxLayerIndex + 1 // rows drawn for decoder layers
	sideKindsMax  = 2048                    // distinct tensor names aggregated for the decoder
	sideTopMin    = 5                       // detail rows kept for TENSORS before the heatmap yields
	sideNameMin   = 16                      // name cells a tensor row keeps before it drops columns
)

// sideFolder is implemented by panels whose side list is a tree. It sees the
// key before the shell does and reports whether it was handled.
type sideFolder interface {
	foldSide(key string) bool
}

type sideEntry struct {
	gidx  int // -1 for the decoder and the overflow row
	dec   bool
	layer bool
	more  int // >0: a row standing for layers past sideLayersMax
}

type kindRow struct {
	name, dtype string
	count       int
	bytes       int64
}

// layersSide is the panel's own tree state. It hangs off layersState so the
// adapters, rebuilt on every call, share it.
type layersSide struct {
	m      *model.Model
	built  bool
	info   map[int]model.LayerInfo
	tops   []sideEntry
	layers []int
	hidden int // layers beyond sideLayersMax
	decB   int64
	decP   int64

	open    bool
	cur     int
	top     int // first TENSORS row in the detail pane
	vis     []sideEntry
	visSet  bool
	visOpen bool

	kinds       []kindRow
	kindsSet    bool
	kindsMissed int
}

type layersPanel struct{ t *Model }

func (p *layersPanel) num() int { return 3 }

func (p *layersPanel) title() sanitize.Text { return ui.L("Layers") }

// side returns the tree state, building it on first use. It is nil without a
// model or Layers state.
func (p *layersPanel) side() *layersSide {
	t := p.t
	if t.lay == nil || t.m == nil {
		return nil
	}
	s := &t.lay.side
	if s.built && s.m == t.m {
		return s
	}
	*s = layersSide{m: t.m, built: true, info: t.m.LayerInfoBy()}
	decSeen := false
	for i, g := range t.m.Groups {
		if g == nil {
			continue
		}
		if g.Kind != model.KindLayer {
			s.tops = append(s.tops, sideEntry{gidx: i})
			continue
		}
		if !decSeen {
			decSeen = true
			s.tops = append(s.tops, sideEntry{gidx: -1, dec: true})
		}
		addBytes(&s.decB, g.Bytes)
		addBytes(&s.decP, g.LogicalParams)
		if len(s.layers) < sideLayersMax {
			s.layers = append(s.layers, i)
		} else {
			s.hidden++
		}
	}
	return s
}

func (s *layersSide) visible() []sideEntry {
	if s.visSet && s.visOpen == s.open {
		return s.vis
	}
	vis := make([]sideEntry, 0, len(s.tops)+len(s.layers)+1)
	for _, e := range s.tops {
		vis = append(vis, e)
		if !e.dec || !s.open {
			continue
		}
		for _, gi := range s.layers {
			vis = append(vis, sideEntry{gidx: gi, layer: true})
		}
		if s.hidden > 0 {
			vis = append(vis, sideEntry{gidx: -1, more: s.hidden})
		}
	}
	s.vis, s.visSet, s.visOpen = vis, true, s.open
	return vis
}

// selectable is the row count the cursor may reach: the overflow row is
// drawn but has nothing behind it.
func (s *layersSide) selectable() int {
	v := s.visible()
	if n := len(v); n > 0 && v[n-1].more > 0 {
		return n - 1
	}
	return len(v)
}

func (s *layersSide) decoderAt() int {
	for i, e := range s.visible() {
		if e.dec {
			return i
		}
	}
	return -1
}

func (s *layersSide) setCur(i int) {
	n := s.selectable()
	if n == 0 {
		s.cur = 0
		return
	}
	i = clampInt(i, 0, n-1)
	if i != s.cur {
		s.top = 0
	}
	s.cur = i
}

func (p *layersPanel) selected() (sideEntry, bool) {
	s := p.side()
	if s == nil || s.selectable() == 0 {
		return sideEntry{}, false
	}
	s.setCur(s.cur)
	return s.visible()[s.cur], true
}

func (p *layersPanel) group(e sideEntry) *model.Group { return p.t.groupAt(e.gidx) }

func (p *layersPanel) counter() string {
	s := p.side()
	if s == nil || len(s.layers)+s.hidden == 0 {
		return ""
	}
	return muted(strconv.Itoa(len(s.layers)+s.hidden) + " decoder")
}

func foldGlyph(open bool) string {
	switch {
	case !ui.UTF8():
		if open {
			return "v"
		}
		return ">"
	case open:
		return "▾"
	default:
		return "▸"
	}
}

func (p *layersPanel) decoderName() string {
	s := p.side()
	return "decoder " + ui.Times() + strconv.Itoa(len(s.layers)+s.hidden)
}

func (p *layersPanel) label(e sideEntry) sanitize.Text {
	switch {
	case e.dec:
		return sanitize.Program(p.decoderName())
	case e.more > 0:
		return sanitize.Program("+" + strconv.Itoa(e.more) + " more layers")
	}
	g := p.group(e)
	if e.layer && g != nil && g.Layer >= 0 {
		return sanitize.Program("layer " + strconv.Itoa(g.Layer))
	}
	return p.t.layerGroupLabel(g)
}

func (p *layersPanel) bytesOf(e sideEntry) int64 {
	switch {
	case e.dec:
		return p.side().decB
	case e.more > 0:
		return 0
	}
	if g := p.group(e); g != nil {
		return nonNeg(g.Bytes)
	}
	return 0
}

func (p *layersPanel) attnGlyph(e sideEntry) string {
	gl := ui.G()
	if g := p.group(e); g != nil && p.side().info[g.Layer].Short == "full" {
		return gl.Full
	}
	return gl.Linear
}

func (p *layersPanel) row(e sideEntry, width int, selected bool) string {
	r := ui.RowSpec{Glyph: sanitize.Program(" "), Name: p.label(e), Selected: selected}
	switch {
	case e.dec:
		r.Glyph = sanitize.Program(foldGlyph(p.side().open))
		r.Tone = ui.ToneAccent
	case e.layer:
		r.Indent = 2
		r.Glyph = sanitize.Program(p.attnGlyph(e))
	case e.more > 0:
		r.Indent = 2
		return ui.ListRow(r, width)
	}
	r.Value = sanitize.Program(model.HumanBytes(p.bytesOf(e)))
	return ui.ListRow(r, width)
}

func (p *layersPanel) rows(width int) []string {
	s := p.side()
	if s == nil {
		return nil
	}
	cur := p.cursor()
	vis := s.visible()
	out := make([]string, len(vis))
	for i, e := range vis {
		out[i] = p.row(e, width, i == cur)
	}
	return out
}

// summary is the three largest groups, so a collapsed panel still says where
// the bytes are.
func (p *layersPanel) summary(width int) []string {
	s := p.side()
	if s == nil {
		return nil
	}
	tops := append([]sideEntry(nil), s.tops...)
	sort.SliceStable(tops, func(a, b int) bool { return p.bytesOf(tops[a]) > p.bytesOf(tops[b]) })
	var out []string
	for _, e := range tops {
		if len(out) == 3 {
			break
		}
		out = append(out, p.row(e, width, false))
	}
	return out
}

func (p *layersPanel) cursor() int {
	s := p.side()
	if s == nil || s.selectable() == 0 {
		return 0
	}
	return clampInt(s.cur, 0, s.selectable()-1)
}

func (p *layersPanel) move(d int) {
	s := p.side()
	if s == nil {
		return
	}
	s.setCur(clampInt(s.cur+d, -1, 1<<30))
	if d != 0 {
		s.top = 0
	}
	p.t.touchLayers()
}

func (p *layersPanel) problems() []int { return nil }

func (p *layersPanel) selection() (sanitize.Text, ui.Safe) {
	e, ok := p.selected()
	if !ok {
		return ui.L("Layers"), ui.Safe{}
	}
	title := p.label(e)
	if e.dec {
		title = ui.L("decoder")
	}
	return title, ui.Chrome(muted(model.HumanBytes(p.bytesOf(e))))
}

func (p *layersPanel) keys() []ui.KeyHint {
	fold := "←→"
	if !ui.UTF8() {
		fold = "<->"
	}
	out := []ui.KeyHint{hint(upDown(), "move")}
	if e, ok := p.selected(); ok && e.dec {
		return append(out, hint(enterKey(), "fold"), hint("0", "detail"), hint("?", "keys"),
			hint("y", "copy"), hint("1-4", "panels"), hint("tab", "next panel"))
	}
	out = append(out, hint(enterKey(), "detail"), hint("?", "keys"))
	if s := p.side(); s != nil && len(s.layers) > 0 {
		out = append(out, hint(fold, "fold"))
	}
	return append(out, hint("y", "copy"), hint("1-4", "panels"), hint("tab", "next panel"))
}

func (p *layersPanel) toggle(open bool) {
	s := p.side()
	s.open = open
	s.setCur(s.cur)
}

func (p *layersPanel) foldSide(key string) bool {
	s := p.side()
	e, ok := p.selected()
	if s == nil || !ok {
		return false
	}
	handled := false
	switch key {
	case "enter", " ", "space":
		if e.dec {
			p.toggle(!s.open)
			handled = true
		}
	case "right", "l":
		if e.dec {
			if !s.open {
				p.toggle(true)
			} else if len(s.layers) > 0 {
				s.setCur(s.cur + 1)
			}
			handled = true
		}
	case "left", "h":
		switch {
		case e.layer:
			s.setCur(s.decoderAt())
			handled = true
		case e.dec && s.open:
			p.toggle(false)
			handled = true
		}
	}
	if handled {
		p.t.touchLayers()
	}
	return handled
}

func (p *layersPanel) scrollDetail(d int) {
	if s := p.side(); s != nil {
		s.top = clampInt(s.top+d, 0, 1<<24)
		p.t.touchLayers()
	}
}

func (p *layersPanel) foldDetail(key string) { p.foldSide(key) }

// copySelection copies the selected group's key, like the tab layout's path copy.
func (p *layersPanel) copySelection() {
	e, ok := p.selected()
	g := p.group(e)
	if !ok || g == nil {
		p.t.copyLayerPayload("", "")
		return
	}
	p.t.copyLayerPayload(g.Key, "path")
}

// Detail

func (p *layersPanel) detail(w, h int) []string {
	e, ok := p.selected()
	if !ok || w < 1 || h < 1 {
		return nil
	}
	var lines []string
	if e.dec || e.layer {
		lines = p.heatLines(e, w, h-sideTopMin-2)
	}
	lines = append(lines, p.totalsLine(e, w))
	if len(lines) > 0 && h-len(lines) >= sideTopMin+2 {
		lines = append(lines, "")
	}
	return append(lines, p.tensorLines(e, w, max(1, h-len(lines)-1))...)
}

func (p *layersPanel) totalsLine(e sideEntry, w int) string {
	body := func(s string) ui.Span { return ui.Span{Text: sanitize.Program(s), Tone: ui.ToneBody} }
	s := p.side()
	var spans []ui.Span
	if e.dec {
		lin, full := 0, 0
		for _, gi := range s.layers {
			if s.info[p.t.groupAt(gi).Layer].Short == "full" {
				full++
			} else {
				lin++
			}
		}
		gl := ui.G()
		spans = []ui.Span{
			body(strconv.Itoa(len(s.layers)+s.hidden) + " layers"),
			body(model.HumanBytes(s.decB)),
			body(humanCount(s.decP) + " params"),
			body(strconv.Itoa(lin) + " " + gl.Linear),
			body(strconv.Itoa(full) + " " + gl.Full),
		}
		return fitSpans(spans, w)
	}
	g := p.group(e)
	if g == nil {
		return ui.HeaderLine(nil, nil, w)
	}
	spans = []ui.Span{
		body(strconv.Itoa(len(g.Tensors)) + " tensors"),
		body(model.HumanBytes(nonNeg(g.Bytes))),
		body(humanCount(g.LogicalParams) + " params"),
	}
	if label := p.t.layerDType(g).String(); label != "" && label != ui.Dash() {
		spans = append(spans, ui.Span{Text: p.t.layerDType(g), Tone: ui.ToneBody})
	}
	if e.layer {
		if short := p.t.attnShort(g); short != "" {
			spans = append(spans, ui.Span{Text: p.t.lay.once(short), Tone: ui.ToneBody})
		}
	}
	return fitSpans(spans, w)
}

// fitSpans drops trailing spans until the line fits, so a narrow pane loses
// whole facts instead of an elided tail.
func fitSpans(spans []ui.Span, w int) string {
	for len(spans) > 1 {
		n := 0
		for _, sp := range spans {
			n += ui.Width(sp.Text.String()) + ui.Width(ui.SepWord())
		}
		if n-ui.Width(ui.SepWord()) <= w {
			break
		}
		spans = spans[:len(spans)-1]
	}
	return ui.HeaderLine(spans, nil, w)
}

// tensorLines is the TENSORS heading and a window of rows. The window moves
// with side.top, which the render clamps and writes back.
func (p *layersPanel) tensorLines(e sideEntry, w, room int) []string {
	s := p.side()
	var rowAt func(i int) string
	var n int
	if e.dec {
		p.buildKinds()
		n = len(s.kinds)
		if s.kindsMissed > 0 {
			n++
		}
		rowAt = func(i int) string {
			if i >= len(s.kinds) {
				return ui.ListRow(ui.RowSpec{Name: sanitize.Program("+" + strconv.Itoa(s.kindsMissed) + " more tensors")}, w)
			}
			k := s.kinds[i]
			return p.tensorRow(p.t.lay.once(k.name), p.t.dtypeOnce(k.dtype), ui.Times()+strconv.Itoa(k.count), k.bytes, w)
		}
	} else {
		g := p.group(e)
		packs := p.t.lay.packs(p.t.m, e.gidx)
		for _, pk := range packs {
			n += len(pk.rows)
		}
		rowAt = func(i int) string {
			for _, pk := range packs {
				if i < len(pk.rows) {
					lt := pk.rows[i]
					return p.tensorRow(p.t.layerTensorLabel(g, lt), p.tensorDtype(lt), shapeLimited(lt.Shape), lt.Bytes, w)
				}
				i -= len(pk.rows)
			}
			return ""
		}
	}
	s.top = clampInt(s.top, 0, max(0, n-room))
	head := ui.SectionLabel(ui.L("tensors"), w)
	if n > room {
		span := strconv.Itoa(s.top+1) + "-" + strconv.Itoa(min(n, s.top+room)) + " of " + strconv.Itoa(n)
		head = ui.HeaderLine(
			[]ui.Span{{Text: ui.L("TENSORS"), Tone: ui.ToneMuted, Bold: true}},
			[]ui.Span{{Text: sanitize.Program(span), Tone: ui.ToneMuted}}, w)
	}
	out := []string{head}
	for i := s.top; i < min(n, s.top+room); i++ {
		out = append(out, rowAt(i))
	}
	if n == 0 {
		out = append(out, ui.ListRow(ui.RowSpec{Name: ui.L("none")}, w))
	}
	return out
}

func (p *layersPanel) tensorDtype(lt model.LogicalTensor) sanitize.Text {
	if b := quantBadge(p.t.m, lt.Role); b != "" {
		return sanitize.Program(b)
	}
	if lt.DType != "" {
		return p.t.lay.once(strings.ToLower(lt.DType))
	}
	return sanitize.Program(lt.Role)
}

// tensorRow drops shape, then dtype, before the name loses its room.
func (p *layersPanel) tensorRow(name, dtype sanitize.Text, shape string, bytes int64, w int) string {
	b := model.HumanBytes(nonNeg(bytes))
	d := dtype.String()
	need := min(ui.Width(name.String()), sideNameMin)
	value := b
	for _, v := range []string{join3(d, shape, b), join3(d, "", b)} {
		if ui.Width(v) <= w/2 && w-ui.Width(v)-1 >= need {
			value = v
			break
		}
	}
	return ui.ListRow(ui.RowSpec{Name: name, Value: sanitize.Program(value)}, w)
}

func join3(a, b, c string) string {
	parts := make([]string, 0, 3)
	for _, s := range []string{a, b, c} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " ")
}

// buildKinds folds every decoder layer's tensors by name without the layer
// prefix, so the decoder row lists one line per kind of tensor. The names and
// dtypes are still raw here; they are sanitized once when drawn.
func (p *layersPanel) buildKinds() {
	s := p.side()
	if s.kindsSet {
		return
	}
	s.kindsSet = true
	at := map[string]int{}
	for _, gi := range s.layers {
		g := p.t.groupAt(gi)
		if g == nil {
			continue
		}
		for _, t := range g.Tensors {
			if t == nil {
				continue
			}
			name := model.ShortName(t.Name)
			i, ok := at[name]
			if !ok {
				if len(s.kinds) >= sideKindsMax {
					s.kindsMissed++
					continue
				}
				i = len(s.kinds)
				at[name] = i
				s.kinds = append(s.kinds, kindRow{name: name, dtype: strings.ToLower(t.DType)})
			}
			s.kinds[i].count++
			addBytes(&s.kinds[i].bytes, t.ByteLen())
		}
	}
}

// Heatmap

// heatLines is the BYTES PER LAYER block, sized to w. The shared heatmap
// draws its own title and legend, which cut mid-word when the pane is narrow,
// so both are rebuilt here from pieces that are dropped whole. budget is the
// most rows the block may take; optional lines go first, then the block.
func (p *layersPanel) heatLines(e sideEntry, w, budget int) []string {
	d := p.t.derived
	if d == nil || len(d.heat.Rows) == 0 || len(d.heat.Rows[0].Cells) == 0 || budget < 4 {
		return nil
	}
	grid := d.heat
	note, caption := grid.Note.String(), grid.Caption.String()
	grid.Note, grid.Caption = ui.Safe{}, ui.Safe{}
	n := len(grid.Rows[0].Cells)
	large := w >= 70
	raw := ui.Heatmap(grid, w, large)
	labelW, cw, pool, _ := ui.HeatGeom(w, n, large)
	gl := ui.G()

	legendAt := -1
	for i := len(raw) - 1; i >= 0; i-- {
		if strings.Contains(ui.Strip(raw[i]), gl.Linear+" linear") {
			legendAt = i
			break
		}
	}
	if legendAt < 1 {
		return nil
	}
	head := 1
	if pool > 1 {
		head = 2
	}
	var required []string
	required = append(required, ui.SectionLabel(ui.L("bytes per layer"), w))
	if pool > 1 {
		required = append(required, ui.PaintToken(strconv.Itoa(pool)+" layers/cell", ui.TextMuted, nil, false))
	}
	required = append(required, raw[head:legendAt]...)

	var mark string
	if g := p.group(e); e.layer && g != nil {
		if i := sort.SearchInts(grid.Layers, g.Layer); i < len(grid.Layers) && grid.Layers[i] == g.Layer && labelW+i/pool*cw < w {
			arrow := gl.Size
			mark = strings.Repeat(" ", labelW+i/pool*cw) + ui.PaintToken(arrow, ui.TextPrimary, nil, true)
		}
	}
	legend := p.legend(raw[legendAt], w)

	var extra []string
	var pieces []string
	if note != "" {
		pieces = append(pieces, note)
	}
	if caption != "" {
		pieces = append(pieces, strings.Split(caption, ui.SepWord())...)
	}
	for _, l := range packPieces(pieces, w, 2) {
		extra = append(extra, ui.PaintToken(l, ui.TextMuted, nil, false))
	}

	// Each attempt keeps fewer optional lines than the one before it.
	for _, a := range []struct{ mark, extra bool }{{true, true}, {true, false}, {false, false}} {
		out := append([]string(nil), required...)
		if a.mark && mark != "" {
			out = append(out, mark)
		}
		if legend != "" {
			out = append(out, legend)
		}
		if a.extra {
			out = append(out, extra...)
		}
		if len(out) <= budget {
			return out
		}
	}
	return nil
}

// legend rebuilds the shared legend line from whole pieces: the scale first,
// then the attention marks, then the log2 note. It returns "" when not even
// the scale fits.
func (p *layersPanel) legend(line string, w int) string {
	gl := ui.G()
	core := line
	if i := strings.LastIndex(line, "  "+gl.Linear+" linear"); i >= 0 {
		core = line[:i]
	}
	for _, tail := range []string{
		"  " + gl.Linear + " linear  " + gl.Full + " full  log2",
		"  " + gl.Linear + " linear  " + gl.Full + " full",
		"",
	} {
		if ui.Width(core+tail) <= w {
			return core + tail
		}
	}
	return ""
}

// packPieces joins whole pieces with the separator into at most maxLines
// lines of w cells. A piece wider than a line, or past the last line, is
// dropped rather than cut.
func packPieces(pieces []string, w, maxLines int) []string {
	var lines []string
	cur := ""
	for _, pc := range pieces {
		if ui.Width(pc) > w {
			continue
		}
		next := pc
		if cur != "" {
			next = cur + ui.SepWord() + pc
		}
		if ui.Width(next) <= w {
			cur = next
			continue
		}
		lines = append(lines, cur)
		if len(lines) == maxLines {
			return lines
		}
		cur = pc
	}
	if cur != "" && len(lines) < maxLines {
		lines = append(lines, cur)
	}
	return lines
}

func nonNeg(n int64) int64 {
	if n < 0 {
		return 0
	}
	return n
}
