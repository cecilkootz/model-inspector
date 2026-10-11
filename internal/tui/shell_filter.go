package tui

import (
	"sort"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/sanitize"
	"github.com/cecilkootz/model-inspector/internal/tui/ui"
)

const shellFilterMax = 64

// filterRow describes one entry of a panel's rows() for the shell's `/`
// filter.
type filterRow struct {
	name   string // displayed (sanitized) name, matched case-insensitively
	parent int    // row kept on screen while this one matches, or -1
	skip   bool   // a label: never selectable and never matches, drawn only for a matching child
}

// filterable is implemented by panels whose list can be filtered. filterRows
// is parallel to rows(). filterGen changes whenever the rows do, cheaply, so
// the shell can reuse them. Panels that do not implement it ignore `/`.
type filterable interface {
	filterRows() []filterRow
	filterGen() int
}

// shellFilter is one side panel's filter. It survives focus changes.
type shellFilter struct {
	text    string
	editing bool
}

func (f shellFilter) active() bool { return f.text != "" }

// filterCache keeps a panel's lowered names and the result for one query, so
// a key press does not walk thousands of rows again.
type filterCache struct {
	ok   bool
	m    *model.Model
	gen  int
	rows []filterRow // names lowered
	skip []bool

	text  string
	hit   bool
	order []int // rows drawn
	sel   []int // rows the cursor may rest on
}

// data returns the panel's cache, refreshed for its rows and the current
// query. The caller checks that the panel is filterable and the query is set
// when it wants matches.
func (t *Model) filterData(p sidePanel, f filterable) *filterCache {
	i := p.num() - 1
	c := &t.sh.fcache[i]
	if gen := f.filterGen(); !c.ok || c.m != t.m || c.gen != gen {
		rows := f.filterRows()
		skip := make([]bool, len(rows))
		for j := range rows {
			skip[j] = rows[j].skip
			rows[j].name = strings.ToLower(rows[j].name)
		}
		*c = filterCache{ok: true, m: t.m, gen: gen, rows: rows, skip: skip}
	}
	if text := t.sh.filter[i].text; text != "" && (!c.hit || c.text != text) {
		q := strings.ToLower(text)
		keep := make([]bool, len(c.rows))
		for j, r := range c.rows {
			if r.skip || !strings.Contains(r.name, q) {
				continue
			}
			keep[j] = true
			if r.parent >= 0 && r.parent < len(keep) {
				keep[r.parent] = true
			}
		}
		c.order, c.sel = c.order[:0], []int{}
		for j, k := range keep {
			if k {
				c.order = append(c.order, j)
				if !c.rows[j].skip {
					c.sel = append(c.sel, j)
				}
			}
		}
		c.text, c.hit = text, true
	}
	return c
}

func identity(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}

// listOrder is the indices of the panel's rows to draw, ascending, and which
// rows are non-selectable labels (nil when the panel has none).
func (t *Model) listOrder(p sidePanel, n int) (order []int, skip []bool) {
	f, ok := p.(filterable)
	if !ok {
		return identity(n), nil
	}
	c := t.filterData(p, f)
	if len(c.rows) != n {
		return identity(n), nil
	}
	if !t.sh.filter[p.num()-1].active() {
		return identity(n), c.skip
	}
	return c.order, c.skip
}

// selectable lists the rows the cursor may rest on under the panel's filter,
// or nil when no filter applies.
func (t *Model) selectable(p sidePanel) []int {
	f, ok := p.(filterable)
	if !ok || !t.sh.filter[p.num()-1].active() {
		return nil
	}
	return t.filterData(p, f).sel
}

// filterSnap moves the cursor onto a row the filter keeps: the next one, or
// the last one before it.
func (t *Model) filterSnap(p sidePanel) {
	sel := t.selectable(p)
	if len(sel) == 0 {
		return
	}
	cur := p.cursor()
	at := sort.SearchInts(sel, cur)
	if at < len(sel) && sel[at] == cur {
		return
	}
	if at == len(sel) {
		at--
	}
	p.move(sel[at] - cur)
}

// filterMove steps by d matches. It reports false when no filter applies.
func (t *Model) filterMove(p sidePanel, d int) bool {
	sel := t.selectable(p)
	if sel == nil {
		return false
	}
	if len(sel) == 0 {
		return true
	}
	cur := p.cursor()
	at := sort.SearchInts(sel, cur)
	if at == len(sel) || sel[at] != cur {
		// Off the matches: a step down lands on the next one, a step up on the previous.
		if d > 0 {
			d--
		}
	}
	at = clampInt(at+d, 0, len(sel)-1)
	p.move(sel[at] - cur)
	return true
}

func (t *Model) filterClear(i int) {
	t.sh.filter[i] = shellFilter{}
	t.sh.scroll[i] = 0
}

func (t *Model) filterStopEditing() {
	for i := range t.sh.filter {
		t.sh.filter[i].editing = false
	}
}

// filterKey handles a key while the open panel's filter input has focus.
func (t *Model) filterKey(p sidePanel, msg tea.KeyMsg) {
	sh := t.sh
	i := p.num() - 1
	st := &sh.filter[i]
	k := msg.String()
	switch k {
	case "esc":
		t.filterClear(i)
		return
	case "enter":
		st.editing = false
		return
	case "up":
		t.shellMove(p, -1)
		return
	case "down":
		t.shellMove(p, 1)
		return
	case "pgup":
		t.shellMove(p, -max(1, t.shellBodyHeight()/2))
		return
	case "pgdown":
		t.shellMove(p, max(1, t.shellBodyHeight()/2))
		return
	case "backspace":
		if r := []rune(st.text); len(r) > 0 {
			st.text = string(r[:len(r)-1])
		}
	default:
		if msg.Alt || (msg.Type != tea.KeyRunes && msg.Type != tea.KeySpace) {
			return
		}
		add := msg.Runes
		if msg.Type == tea.KeySpace && len(add) == 0 {
			add = []rune{' '}
		}
		r := []rune(st.text)
		for _, c := range add {
			// Pasted escape sequences and invisible characters never enter the text.
			if len(r) >= shellFilterMax {
				break
			}
			if unicode.IsPrint(c) {
				r = append(r, c)
			}
		}
		st.text = string(r)
	}
	sh.scroll[i] = 0
	sh.top = 0
	t.filterSnap(p)
}

// filterOpen starts editing the open panel's filter. It reports whether the
// panel can filter.
func (t *Model) filterOpen(p sidePanel) bool {
	if _, ok := p.(filterable); !ok {
		return false
	}
	t.sh.filter[p.num()-1].editing = true
	return true
}

func tailFit(s string, w int) string {
	r := []rune(s)
	for len(r) > 0 && ui.Width(string(r)) > w {
		r = r[1:]
	}
	return string(r)
}

// filterInputRow is the line drawn in the last body row while editing. The
// typed text is sanitized once here.
func filterInputRow(text string, w int) string {
	caret := "_"
	if ui.UTF8() {
		caret = "▏"
	}
	shown := tailFit(sanitize.From(text).String(), max(0, w-3))
	row := ui.PaintToken("/", ui.Accent, nil, true) + " " + ui.PaintToken(shown, ui.TextPrimary, nil, false) +
		ui.PaintToken(caret, ui.Accent, nil, false)
	return row
}

// filterCounter replaces the panel counter while a filter is set. The text is
// elided to what the border has room for, so an accepted filter stays visible.
func filterCounter(text string, title sanitize.Text, sideW int) ui.Safe {
	const lead = "filter: "
	room := sideW - 12 - ui.Width(title.String()) - len(lead)
	shown := sanitize.From(text).String()
	if ui.Width(shown) > room {
		shown = ui.Trunc(shown, max(1, room), ui.G().Ellipsis)
	}
	return ui.Chrome(muted(lead + shown))
}

// shellKeys is the open panel's key hints plus the filter keys when it can
// filter. The shell owns them so panels do not repeat them.
func (t Model) shellKeys(p sidePanel) []ui.KeyHint {
	hints := p.keys()
	if _, ok := p.(filterable); !ok || t.sh.inDetail {
		return hints
	}
	extra := []ui.KeyHint{hint("/", "filter")}
	if t.sh.filter[p.num()-1].active() {
		extra = append(extra, hint("esc", "clear filter"))
	}
	for i, h := range hints {
		if h.Key.String() == "1-4" {
			return append(hints[:i:i], append(extra, hints[i:]...)...)
		}
	}
	return append(hints, extra...)
}

func (t Model) filterEditHints() []ui.KeyHint {
	return []ui.KeyHint{hint(upDown(), "move"), hint(enterKey(), "keep"), hint("esc", "clear")}
}

// filterFingerprint is the filter state for the frame cache key.
func (s *shellState) filterFingerprint() string {
	var b strings.Builder
	for _, f := range s.filter {
		b.WriteString(f.text)
		if f.editing {
			b.WriteByte(1)
		}
		b.WriteByte(0)
	}
	return b.String()
}
