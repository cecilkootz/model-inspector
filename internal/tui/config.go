package tui

import (
	"fmt"
	"io"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cecilkootz/model-inspector/internal/jsonx"
	"github.com/cecilkootz/model-inspector/internal/sanitize"
	"github.com/cecilkootz/model-inspector/internal/tui/ui"
)

const (
	configRawMax  = 1 << 20
	configCopyMax = 64 << 10
	configSyncMax = 1 << 20
)

// configState is the Config panel. The pointer lives on Model so a value
// receiver can update it and still see the change on the next message.
type configState struct {
	tree    *jsonx.Tree
	err     error
	parsed  bool
	parsing bool
	spin    bool

	expanded map[string]bool
	temp     map[string]bool
	page     map[string]pageWin

	cursor int32
	scroll int
	row    int

	fieldCur int
	fieldTop int
	lines    []fieldLine
	linesGen int
	flash    int

	raw bool

	query    string
	filter   bool
	matches  []int32
	matchSet map[int32]bool

	rawLines  []sanitize.Text
	rawReady  bool
	rawCut    bool
	rawLine   int
	rawScroll int

	rows   []crow
	dirty  bool
	gen    int
	viewH  int
	fieldH int
	innerW int

	keySan  map[int32]sanitize.Text
	litSan  map[int32]sanitize.Text
	paths   map[int32]string
	widths  map[int32]int
	keep    map[int32]bool
	hits    []hit
	copyTo  io.Writer
	copyTTY *bool
}

type pageWin struct {
	start int
	n     int
	all   bool
}

type crow struct {
	id     int32
	depth  uint8
	anc    uint32
	last   bool
	more   bool
	lead   bool
	hidden bool
	hideN  int
	match  bool
}

type hit struct {
	id   string
	x, y int
	w, h int
}

type configParsedMsg struct {
	tree *jsonx.Tree
	err  error
}

type configSpinMsg struct{}

func newConfigState() *configState {
	return &configState{
		expanded: map[string]bool{},
		temp:     map[string]bool{},
		page:     map[string]pageWin{},
	}
}

func (c *configState) framePart() string {
	if c == nil {
		return ""
	}
	return fmt.Sprintf("|cf %d %d %d %d %d %v %q %v %d %d %d %v",
		c.fieldCur, c.cursor, c.scroll, c.row, c.gen,
		c.raw, c.query, c.filter, c.rawLine, c.rawScroll, c.fieldTop, c.spin)
}

func (t *Model) touchConfig() {
	if t.cfg != nil {
		t.cfg.gen++
	}
	if t.cache != nil {
		t.cache.key = ""
	}
}

func (t Model) prepareConfig() {
	if t.cfg == nil || t.m == nil {
		return
	}
	c := t.cfg
	if c.parsed || c.parsing {
		return
	}
	raw := t.m.ConfigRaw
	if len(raw) > configSyncMax {
		return
	}
	c.parse(raw)
}

func (c *configState) parse(raw []byte) {
	if c.parsed {
		return
	}
	c.parsed = true
	c.parsing = false
	c.spin = false
	if len(raw) == 0 {
		return
	}
	tree, err := jsonx.ParseTree(raw, jsonx.Limits{})
	c.tree = tree
	c.err = err
	c.dirty = true
	c.lines = nil
}

func (t Model) kickConfig() tea.Cmd {
	if t.cfg == nil || t.m == nil || t.cfg.parsed || t.cfg.parsing {
		return nil
	}
	raw := t.m.ConfigRaw
	if len(raw) <= configSyncMax {
		t.cfg.parse(raw)
		return nil
	}
	t.cfg.parsing = true
	return tea.Batch(parseConfigCmd(raw), tea.Tick(300*time.Millisecond, func(time.Time) tea.Msg {
		return configSpinMsg{}
	}))
}

func parseConfigCmd(raw []byte) tea.Cmd {
	return func() tea.Msg {
		tree, err := jsonx.ParseTree(raw, jsonx.Limits{})
		return configParsedMsg{tree: tree, err: err}
	}
}

func (t Model) onConfigParsed(msg configParsedMsg) (tea.Model, tea.Cmd) {
	if t.cfg == nil {
		return t, nil
	}
	t.cfg.parsed = true
	t.cfg.parsing = false
	t.cfg.tree = msg.tree
	t.cfg.err = msg.err
	t.cfg.dirty = true
	t.cfg.lines = nil
	t.cfg.spin = false
	t.touchConfig()
	return t, nil
}

func (t Model) configMove(d int, tree bool) {
	c := t.cfg
	if !tree {
		n := len(t.fieldLines())
		if n == 0 {
			return
		}
		c.fieldCur += d
		if c.fieldCur < 0 {
			c.fieldCur = 0
		}
		if c.fieldCur >= n {
			c.fieldCur = n - 1
		}
		// Skip headers and blanks so the cursor lands on a row.
		if d != 0 {
			step := 1
			if d < 0 {
				step = -1
			}
			for c.fieldCur >= 0 && c.fieldCur < n && t.fieldLines()[c.fieldCur].kind != fieldRow {
				next := c.fieldCur + step
				if next < 0 || next >= n {
					break
				}
				c.fieldCur = next
			}
		}
		t.clampFields()
		t.touchConfig()
		return
	}
	c.ensureRows()
	if len(c.rows) == 0 {
		return
	}
	i := c.indexOfCursor()
	i += d
	if i < 0 {
		i = 0
	}
	if i >= len(c.rows) {
		i = len(c.rows) - 1
	}
	c.row = i
	c.cursor = c.rows[i].id
	t.clampTree()
	t.touchConfig()
}

func (t *Model) clampFields() {
	c := t.cfg
	h := c.fieldH
	if h < 1 {
		h = 8
	}
	if c.fieldCur < c.fieldTop {
		c.fieldTop = c.fieldCur
	}
	if c.fieldCur >= c.fieldTop+h {
		c.fieldTop = c.fieldCur - h + 1
	}
	if c.fieldTop < 0 {
		c.fieldTop = 0
	}
}

func (t *Model) clampTree() {
	c := t.cfg
	h := c.viewH
	if h < 1 {
		h = 8
	}
	if c.row < c.scroll {
		c.scroll = c.row
	}
	if c.row >= c.scroll+h {
		c.scroll = c.row - h + 1
	}
	if c.scroll < 0 {
		c.scroll = 0
	}
	max := len(c.rows) - h
	if max < 0 {
		max = 0
	}
	if c.scroll > max {
		c.scroll = max
	}
}

func cfgSep() string {
	if ui.UTF8() {
		return " · "
	}
	return " . "
}

func cfgEnter() string {
	if ui.UTF8() {
		return "⏎"
	}
	return "enter"
}

func nodeCount(c *configState) int {
	if c == nil || c.tree == nil {
		return 0
	}
	return len(c.tree.Nodes)
}

func commaInt(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	pre := len(s) % 3
	if pre == 0 {
		pre = 3
	}
	b.WriteString(s[:pre])
	for i := pre; i < len(s); i += 3 {
		b.WriteByte(',')
		b.WriteString(s[i : i+3])
	}
	return b.String()
}
