package tui

import "github.com/cecilkootz/model-inspector/internal/sanitize"

func (p *modelPanel) filterRows() []filterRow {
	items := p.items()
	out := make([]filterRow, len(items))
	for i, it := range items {
		out[i] = filterRow{name: it.name, parent: -1}
	}
	return out
}

func (p *checksPanel) filterRows() []filterRow {
	cs := p.t.scan.Checks
	out := make([]filterRow, len(cs))
	for i, c := range cs {
		out[i] = filterRow{name: c.Name, parent: -1}
	}
	return out
}

// filterRows names the rows as drawn. Layer rows hang off the decoder row, so
// an expanded decoder shows only the layers that match. Layers that are
// folded away are not searched.
func (p *layersPanel) filterRows() []filterRow {
	s := p.side()
	if s == nil {
		return nil
	}
	vis := s.visible()
	dec := s.decoderAt()
	out := make([]filterRow, len(vis))
	for i, e := range vis {
		r := filterRow{name: p.label(e).String(), parent: -1}
		switch {
		case e.layer:
			r.parent = dec
		case e.more > 0:
			r.skip = true
		}
		out[i] = r
	}
	return out
}

// filterRows treats group labels and notes as labels: they are drawn only
// above a field that matches.
func (p *configPanel) filterRows() []filterRow {
	lines, _ := p.listLines()
	out := make([]filterRow, len(lines))
	label := -1
	for i, ln := range lines {
		switch ln.kind {
		case fieldHeader:
			label = i
			out[i] = filterRow{parent: -1, skip: true}
		case fieldRow:
			out[i] = filterRow{name: sanitize.From(ln.label).String(), parent: label}
		default:
			out[i] = filterRow{parent: -1, skip: true}
		}
	}
	return out
}

func (p *modelPanel) filterGen() int { return len(p.items()) }

func (p *checksPanel) filterGen() int { return len(p.t.scan.Checks) }

func (p *layersPanel) filterGen() int {
	if s := p.side(); s != nil {
		return len(s.visible())
	}
	return 0
}

func (p *configPanel) filterGen() int {
	lines, _ := p.listLines()
	return len(lines)
}
