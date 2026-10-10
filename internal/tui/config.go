package tui

import (
	"fmt"
	"io"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"origin.cursor.com/cecilkootz/model-inspector/internal/jsonx"
	"origin.cursor.com/cecilkootz/model-inspector/internal/sanitize"
	"origin.cursor.com/cecilkootz/model-inspector/internal/tui/ui"
)

const (
	configQueryMax = 128
	configRawMax   = 1 << 20
	configCopyMax  = 64 << 10
	configSyncMax  = 1 << 20
)

// configState is the Config tab. The pointer lives on Model so a value
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

	fieldCur  int
	fieldTop  int
	lines     []fieldLine
	linesGen  int
	fromField string
	flash     int

	drill bool
	raw   bool

	search    bool
	query     string
	filter    bool
	matches   []int32
	matchSet  map[int32]bool
	matchIdx  int
	largeWait bool

	modal       bool
	modalID     int32
	modalScroll int

	rawLines  []sanitize.Text
	rawReady  bool
	rawCut    bool
	rawLine   int
	rawScroll int
	rawHits   []int

	rows     []crow
	dirty    bool
	gen      int
	viewH    int
	fieldH   int
	innerW   int
	ratio    int
	ratioSet bool

	keySan  map[int32]sanitize.Text
	litSan  map[int32]sanitize.Text
	paths   map[int32]string
	widths  map[int32]int
	keep    map[int32]bool
	hits    []hit
	snap    *configSnap
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

type configSnap struct {
	expanded map[string]bool
	temp     map[string]bool
	cursor   int32
	scroll   int
	row      int
	query    string
	filter   bool
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
		cursor:   0,
	}
}

func (c *configState) framePart() string {
	if c == nil {
		return ""
	}
	return fmt.Sprintf("|cf %d %d %d %d %d %v %v %v %q %v %v %d %d %d %d %d %v",
		c.fieldCur, c.cursor, c.scroll, c.row, c.gen,
		c.drill, c.raw, c.search, c.query, c.filter, c.modal,
		c.modalScroll, c.rawLine, c.rawScroll, c.matchIdx, c.fieldTop, c.spin)
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

func (t Model) configSearching() bool {
	return t.active == tabConfig && t.cfg != nil && t.cfg.search
}

func (t Model) updateConfigSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	c := t.cfg
	switch msg.String() {
	case "ctrl+c":
		return t, tea.Quit
	case "esc":
		t.cancelSearch()
		return t, nil
	case "enter":
		c.search = false
		c.largeWait = false
		t.runSearch()
		t.touchConfig()
		return t, nil
	case "tab":
		c.filter = !c.filter
		if !c.largeWait {
			t.runSearch()
		}
		t.touchConfig()
		return t, nil
	case "backspace":
		r := []rune(c.query)
		if len(r) > 0 {
			c.query = string(r[:len(r)-1])
		}
		if !c.largeWait {
			t.runSearch()
		}
		t.touchConfig()
		return t, nil
	default:
		if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
			add := string(msg.Runes)
			if msg.Type == tea.KeySpace && add == "" {
				add = " "
			}
			// Append first, then hard-cap. A paste that crosses 128 stays,
			// truncated to the first 128 runes.
			c.query += add
			r := []rune(c.query)
			if len(r) > configQueryMax {
				c.query = string(r[:configQueryMax])
			}
			if !c.largeWait {
				t.runSearch()
			}
			t.touchConfig()
		}
		return t, nil
	}
}

func (t Model) updateConfigKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	c := t.cfg
	if c == nil {
		return t, nil
	}
	t.prepareConfig()
	k := msg.String()
	if c.modal {
		switch k {
		case "esc", "q", "enter":
			c.modal = false
			t.touchConfig()
		case "up", "k":
			if c.modalScroll > 0 {
				c.modalScroll--
			}
			t.touchConfig()
		case "down", "j":
			c.modalScroll++
			t.touchConfig()
		case "pgup", "b", "u":
			c.modalScroll -= 8
			if c.modalScroll < 0 {
				c.modalScroll = 0
			}
			t.touchConfig()
		case "pgdown", "f", "d":
			c.modalScroll += 8
			t.touchConfig()
		case "home", "g":
			c.modalScroll = 0
			t.touchConfig()
		case "end", "G":
			c.modalScroll = 1 << 30
			t.touchConfig()
		}
		return t, nil
	}
	if c.raw {
		return t.updateConfigRaw(msg)
	}
	tree := t.treePane()
	switch k {
	case "up", "k":
		t.configMove(-1, tree)
	case "down", "j":
		t.configMove(1, tree)
	case "pgup", "b", "u":
		t.configMove(-t.configPage(tree), tree)
	case "pgdown", "f", "d":
		t.configMove(t.configPage(tree), tree)
	case "home", "g":
		t.configMove(-1<<30, tree)
	case "end", "G":
		t.configMove(1<<30, tree)
	case "enter":
		t.configEnter(false)
	case "shift+enter":
		t.configEnter(true)
	case "right", "l":
		if tree {
			t.configExpand()
		}
	case "left", "h":
		if tree {
			t.configCollapse()
		}
	case " ", "space":
		if tree {
			t.configToggle()
		}
	case "p":
		if tree {
			t.configParent()
		}
	case "e":
		if tree {
			t.configExpandAll()
		}
	case "E":
		if tree {
			t.configCollapseAll()
		}
	case "+":
		if tree {
			t.configFold(1)
		}
	case "-":
		if tree {
			t.configFold(-1)
		}
	case "/":
		t.openSearch()
	case "n":
		if tree {
			t.nextMatch(1)
		}
	case "N":
		if tree {
			t.nextMatch(-1)
		}
	case "y":
		t.copyConfig(false)
	case "Y":
		t.copyConfig(true)
	case "r":
		if tree {
			c.raw = !c.raw
			c.rawReady = false
			t.syncRawLine()
			t.touchConfig()
		}
	case "esc":
		if t.configCloseTop() {
			t.touchConfig()
		}
	case "backspace":
		if c.drill {
			c.drill = false
			t.touchConfig()
		}
	}
	return t, nil
}

func (t Model) updateConfigRaw(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	c := t.cfg
	t.ensureRaw()
	n := len(c.rawLines)
	switch msg.String() {
	case "r", "esc":
		if msg.String() == "esc" && c.query != "" {
			t.clearSearch()
			return t, nil
		}
		c.raw = false
		t.touchConfig()
	case "up", "k":
		if c.rawLine > 0 {
			c.rawLine--
		}
		t.clampRaw()
		t.touchConfig()
	case "down", "j":
		if n > 0 && c.rawLine < n-1 {
			c.rawLine++
		}
		t.clampRaw()
		t.touchConfig()
	case "pgup", "b", "u":
		c.rawLine -= t.bodyHeight() / 2
		if c.rawLine < 0 {
			c.rawLine = 0
		}
		t.clampRaw()
		t.touchConfig()
	case "pgdown", "f", "d":
		c.rawLine += t.bodyHeight() / 2
		t.clampRaw()
		t.touchConfig()
	case "home", "g":
		c.rawLine = 0
		t.clampRaw()
		t.touchConfig()
	case "end", "G":
		if n > 0 {
			c.rawLine = n - 1
		}
		t.clampRaw()
		t.touchConfig()
	case "/":
		t.openSearch()
	case "n":
		t.nextRawMatch(1)
	case "N":
		t.nextRawMatch(-1)
	case "y":
		t.copyConfig(false)
	case "Y":
		t.copyConfig(true)
	}
	return t, nil
}

func (t Model) treePane() bool {
	if t.cfg == nil {
		return false
	}
	br := t.configBreak()
	if br == "S" || br == "XS" {
		return t.cfg.drill
	}
	return t.focusSecondary
}

func (t Model) configBreak() string {
	return ui.Breakpoint(t.width, t.height)
}

func (t Model) configCloseTop() bool {
	c := t.cfg
	if c == nil {
		return false
	}
	if c.modal {
		c.modal = false
		return true
	}
	if c.query != "" || c.search {
		t.clearSearch()
		return true
	}
	if c.drill {
		c.drill = false
		return true
	}
	if c.raw {
		c.raw = false
		return true
	}
	return false
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

func (t Model) configPage(tree bool) int {
	h := t.cfg.viewH
	if !tree {
		h = t.cfg.fieldH
	}
	if h < 1 {
		h = t.bodyHeight()
	}
	if h > 2 {
		h -= 2
	}
	if h < 1 {
		h = 1
	}
	return h
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

func (t Model) configEnter(all bool) {
	c := t.cfg
	if !t.treePane() {
		if t.configBreak() == "S" || t.configBreak() == "XS" {
			// Enter on a field jumps; the pinned browse row is a separate key.
		}
		lines := t.fieldLines()
		if c.fieldCur >= 0 && c.fieldCur < len(lines) && lines[c.fieldCur].kind == fieldRow {
			t.jumpField(c.fieldCur)
		}
		return
	}
	c.ensureRows()
	if len(c.rows) == 0 {
		return
	}
	i := c.indexOfCursor()
	row := c.rows[i]
	if row.more || row.hidden {
		t.showMore(row, all)
		return
	}
	if c.tree == nil || row.id < 0 || int(row.id) >= len(c.tree.Nodes) {
		return
	}
	n := c.tree.Nodes[row.id]
	if n.Kind == jsonx.KindObject || n.Kind == jsonx.KindArray {
		if !c.inline(row.id) {
			t.configToggle()
			return
		}
	}
	c.modal = true
	c.modalID = row.id
	c.modalScroll = 0
	t.touchConfig()
}

func (t Model) openSearch() {
	c := t.cfg
	if c.tree == nil && !c.raw {
		if t.configBreak() == "S" || t.configBreak() == "XS" {
			c.drill = true
		} else {
			t.focusSecondary = true
		}
	}
	if (t.configBreak() == "S" || t.configBreak() == "XS") && !c.drill && !c.raw {
		c.drill = true
		t.focusSecondary = true
	}
	c.snap = t.snapshot()
	c.search = true
	c.largeWait = c.tree != nil && len(c.tree.Nodes) > 50_000
	t.touchConfig()
}

func (t Model) snapshot() *configSnap {
	c := t.cfg
	s := &configSnap{
		expanded: copySet(c.expanded),
		temp:     copySet(c.temp),
		cursor:   c.cursor,
		scroll:   c.scroll,
		row:      c.row,
		query:    c.query,
		filter:   c.filter,
	}
	return s
}

func copySet(m map[string]bool) map[string]bool {
	out := map[string]bool{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

func (t Model) cancelSearch() {
	c := t.cfg
	if c.snap != nil {
		c.expanded = c.snap.expanded
		c.temp = c.snap.temp
		c.cursor = c.snap.cursor
		c.scroll = c.snap.scroll
		c.row = c.snap.row
		c.query = c.snap.query
		c.filter = c.snap.filter
	}
	c.search = false
	c.largeWait = false
	c.snap = nil
	c.dirty = true
	t.touchConfig()
}

func (t Model) clearSearch() {
	c := t.cfg
	c.query = ""
	c.filter = false
	c.search = false
	c.largeWait = false
	c.matches = nil
	c.matchSet = nil
	c.matchIdx = 0
	c.temp = map[string]bool{}
	c.rawHits = nil
	c.snap = nil
	c.dirty = true
	t.touchConfig()
}

func (t Model) configMode() string {
	c := t.cfg
	if c == nil {
		return "CONFIG"
	}
	if c.search && c.filter {
		return "FILTER"
	}
	if c.search {
		return "SEARCH"
	}
	if c.filter && c.query != "" {
		return "FILTER"
	}
	br := t.configBreak()
	if br == "S" || br == "XS" {
		if c.drill || c.raw {
			return "TREE"
		}
		return "CONFIG"
	}
	if t.focusSecondary || c.raw {
		return "CONFIG" + cfgSep() + "TREE"
	}
	return "CONFIG"
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

func (t Model) configHintSegs() []string {
	c := t.cfg
	if c != nil && c.search {
		if c.filter {
			return []string{"type", cfgEnter() + " keep", "n/N"}
		}
		return []string{"type", "tab filter", cfgEnter() + " keep"}
	}
	if t.treePane() || (c != nil && c.raw) {
		if ui.UTF8() {
			return []string{"←→ fold", "␣ toggle", "/ search", "y/Y copy", "? keys"}
		}
		return []string{"h/l fold", "space toggle", "/ search", "y/Y copy", "? keys"}
	}
	if ui.UTF8() {
		return []string{"↑↓ move", "⏎ open in tree", "? keys"}
	}
	return []string{"up/down move", "enter open in tree", "? keys"}
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
