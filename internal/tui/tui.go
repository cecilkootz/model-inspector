// Package tui implements the interactive Bubble Tea interface.
package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"origin.cursor.com/cecilkootz/model-inspector/internal/model"
	"origin.cursor.com/cecilkootz/model-inspector/internal/sanitize"
	"origin.cursor.com/cecilkootz/model-inspector/internal/scan"
)

type tab int

const (
	tabIdentity tab = iota
	tabConfig
	tabLayers
	tabScan
	numTabs
)

var tabNames = [numTabs]string{"1 Identity", "2 Config", "3 Layers", "4 Scan"}

// Model is the root Bubble Tea model.
type Model struct {
	m      *model.Model
	scan   scan.Result
	active tab
	width  int
	height int
	ready  bool

	help         bool
	notice       string
	inputFocused bool
	scanCursor   int
	scanScroll   int
	scanOpen     map[string]bool

	identityVP viewport.Model
	configVP   viewport.Model

	groupTable  table.Model
	tensorTable table.Model
	detail      *model.Group
}

// Run starts the TUI in the alternate screen.
func Run(m *model.Model, res scan.Result) error {
	_, err := tea.NewProgram(New(m, res), tea.WithAltScreen()).Run()
	return err
}

// New builds the UI for a loaded model.
func New(m *model.Model, res scan.Result) Model {
	configure()
	t := Model{m: m, scan: res, width: 100, height: 30, scanOpen: map[string]bool{}}
	t.identityVP = viewport.New(t.width, t.bodyHeight())
	t.configVP = viewport.New(t.width, t.bodyHeight())
	t.identityVP.SetContent(renderIdentity(m, t.width))
	t.configVP.SetContent(renderConfig(m, t.width))

	styles := table.DefaultStyles()
	styles.Header = styles.Header.Bold(true).Foreground(lipgloss.AdaptiveColor{Dark: "99", Light: "55"})
	styles.Selected = styles.Selected.Foreground(lipgloss.AdaptiveColor{Dark: "230", Light: "255"}).Background(lipgloss.AdaptiveColor{Dark: "62", Light: "61"})
	t.groupTable = table.New(table.WithColumns(groupColumns(t.width)), table.WithRows(groupRows(m)),
		table.WithFocused(true), table.WithHeight(t.bodyHeight()), table.WithStyles(styles))
	t.tensorTable = table.New(table.WithColumns(tensorColumns(t.width)),
		table.WithFocused(true), table.WithHeight(t.bodyHeight()-1), table.WithStyles(styles))
	return t
}

func (t Model) Init() tea.Cmd { return nil }

func (t Model) bodyHeight() int {
	if h := t.height - 3; h > 1 {
		return h
	}
	return 1
}

func (t *Model) resize() {
	h := t.bodyHeight()
	for _, vp := range []*viewport.Model{&t.identityVP, &t.configVP} {
		vp.Width, vp.Height = t.width, h
	}
	t.identityVP.SetContent(renderIdentity(t.m, t.width))
	t.configVP.SetContent(renderConfig(t.m, t.width))
	t.groupTable.SetColumns(groupColumns(t.width))
	t.groupTable.SetWidth(t.width)
	t.groupTable.SetHeight(h)
	t.tensorTable.SetColumns(tensorColumns(t.width))
	t.tensorTable.SetWidth(t.width)
	th := h - 1
	if th < 1 {
		th = 1
	}
	t.tensorTable.SetHeight(th)
}

func (t Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		t.width, t.height, t.ready = msg.Width, msg.Height, true
		t.resize()
		t.ensureScanVisible()
		return t, nil
	case tea.KeyMsg:
		if t.notice != "" && msg.String() != "ctrl+c" {
			t.notice = ""
		}
		if msg.String() == "ctrl+c" {
			return t, tea.Quit
		}
		if t.inputFocused {
			if msg.String() == "esc" {
				t.inputFocused = false
			}
			// q and other keys do not quit or change tabs while a text input is focused.
			return t, nil
		}
		if t.ready && (t.width < 60 || t.height < 16) {
			if msg.String() == "q" {
				return t, tea.Quit
			}
			return t, nil
		}
		switch msg.String() {
		case "q":
			return t, tea.Quit
		case "?":
			t.help = !t.help
			return t, nil
		case "esc":
			if t.help {
				t.help = false
				return t, nil
			}
			if t.detail != nil {
				t.detail = nil
				return t, nil
			}
			return t, nil
		case "backspace":
			if t.detail != nil {
				t.detail = nil
			}
			return t, nil
		case "tab", "right", "l":
			t.active = (t.active + 1) % numTabs
			return t, nil
		case "shift+tab", "left", "h":
			t.active = (t.active + numTabs - 1) % numTabs
			return t, nil
		case "1", "2", "3", "4":
			t.active = tab(msg.String()[0] - '1')
			return t, nil
		}
		if t.help {
			return t, nil
		}
		if t.active == tabScan {
			return t.updateScan(msg)
		}
		if t.active == tabLayers {
			switch msg.String() {
			case "enter":
				if t.detail == nil {
					if i := t.groupTable.Cursor(); i >= 0 && i < len(t.m.Groups) {
						t.detail = t.m.Groups[i]
						t.tensorTable.SetRows(tensorRows(t.detail))
						t.tensorTable.SetCursor(0)
					}
				}
				return t, nil
			}
		}
	}

	if t.help {
		return t, nil
	}
	var cmd tea.Cmd
	switch t.active {
	case tabIdentity:
		t.identityVP, cmd = t.identityVP.Update(msg)
	case tabConfig:
		t.configVP, cmd = t.configVP.Update(msg)
	case tabLayers:
		if t.detail != nil {
			t.tensorTable, cmd = t.tensorTable.Update(msg)
		} else {
			t.groupTable, cmd = t.groupTable.Update(msg)
		}
	}
	return t, cmd
}

func (t Model) updateScan(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	n := len(t.scan.Checks)
	switch msg.String() {
	case "up", "k":
		if t.scanCursor > 0 {
			t.scanCursor--
		}
	case "down", "j":
		if n > 0 && t.scanCursor < n-1 {
			t.scanCursor++
		}
	case "pgup", "b", "u":
		t.scanCursor -= t.bodyHeight() / 2
		if t.scanCursor < 0 {
			t.scanCursor = 0
		}
	case "pgdown", "f", "d":
		t.scanCursor += t.bodyHeight() / 2
		if n > 0 && t.scanCursor >= n {
			t.scanCursor = n - 1
		}
	case "home", "g":
		t.scanCursor = 0
	case "end", "G":
		if n > 0 {
			t.scanCursor = n - 1
		}
	case "enter":
		if n > 0 && t.scanCursor >= 0 && t.scanCursor < n {
			id := t.scan.Checks[t.scanCursor].ID
			if t.scanOpen == nil {
				t.scanOpen = map[string]bool{}
			}
			t.scanOpen[id] = !t.scanOpen[id]
		}
	case "c":
		t.notice = "Code & pickles arrives in a later version"
	}
	t.ensureScanVisible()
	return t, nil
}

func (t Model) View() string {
	configure()
	if !t.ready {
		return t.headerLine(80) + "\n" + t.tabLine(80)
	}
	if t.width < 60 || t.height < 16 {
		return frame(t.tooSmall(), t.width, t.height)
	}
	var body string
	switch {
	case t.help:
		body = t.helpText(t.width, t.bodyHeight())
	case t.active == tabIdentity:
		body = t.identityVP.View()
	case t.active == tabConfig:
		body = t.configVP.View()
	case t.active == tabLayers:
		body = t.layersBody()
	case t.active == tabScan:
		body = t.scanBody(t.width, t.bodyHeight())
	}
	body = clipLines(body, t.bodyHeight(), t.width)
	raw := t.headerLine(t.width) + "\n" + t.tabLine(t.width) + "\n" + body + "\n" + t.footerLine(t.width)
	return frame(raw, t.width, t.height)
}

func (t Model) layersBody() string {
	if strings.HasPrefix(t.m.Format, "gguf") {
		return "GGUF parsing is coming. model-inspector read file sizes only."
	}
	if len(t.m.Tensors) == 0 {
		return "No tensors: this directory has no safetensors shards."
	}
	if t.detail != nil {
		head := sanitize.From(fmt.Sprintf("%s · %d tensors · %s", t.detail.Key, len(t.detail.Tensors), model.HumanBytes(t.detail.Bytes))).String()
		return head + "\n" + t.tensorTable.View()
	}
	return t.groupTable.View()
}

func kv(label string, value sanitize.Text) string {
	return labelStyle.Render(sanitize.From(label).String()) + valueStyle.Render(value.String()) + "\n"
}

func text(s string) sanitize.Text { return sanitize.From(s) }

func textf(format string, args ...any) sanitize.Text {
	return sanitize.From(fmt.Sprintf(format, args...))
}

// renderConfig is the scrollable Config screen. Grouped layout is a later
// change; this screen keeps the key fields and the sanitized JSON.
// Missing, unreadable, and unparsed configs each get their own line.
func renderConfig(m *model.Model, width int) string {
	if width < 20 {
		width = 20
	}
	if m.Config == nil {
		msg := "No config.json."
		switch {
		case len(m.ConfigRaw) > 0:
			msg = "config.json did not parse."
			excerpt := string(m.ConfigRaw)
			if len(excerpt) > 512 {
				excerpt = excerpt[:512]
			}
			msg += "\n" + sanitize.From(excerpt).String()
		case configUnreadable(m):
			msg = "config.json is unreadable."
		}
		var b strings.Builder
		b.WriteString(msg)
		n := 0
		for _, w := range m.Warnings {
			if !strings.Contains(w, "config.json") {
				continue
			}
			if n == 5 {
				break
			}
			b.WriteByte('\n')
			b.WriteString(sanitize.From(w).String())
			n++
		}
		return cutBlock(b.String(), width)
	}
	var b strings.Builder
	b.WriteString(headingStyle.Render("Key fields") + "\n")
	for _, f := range m.KeyConfig() {
		b.WriteString(kv(f.Path, text(compact(f.Value))))
	}
	if lt := m.LayerTypeCounts(); lt != nil {
		for _, k := range model.SortedKeys(lt) {
			b.WriteString(kv("layer_types: "+k, textf("%d", lt[k])))
		}
	}
	if m.Quant != nil {
		b.WriteString(kv(m.Quant.Source, textf("bits=%d group_size=%d mode=%s", m.Quant.Bits, m.Quant.GroupSize, m.Quant.Mode)))
	}
	b.WriteString("\n" + headingStyle.Render("config.json (raw)") + "\n")
	pretty, err := sanitize.JSON(m.Config)
	if err != nil {
		b.WriteString(sanitize.From(string(m.ConfigRaw)).String())
	} else {
		b.WriteString(pretty)
	}
	return cutBlock(b.String(), width)
}

func configUnreadable(m *model.Model) bool {
	for _, w := range m.Warnings {
		if strings.Contains(w, "config.json") {
			return true
		}
	}
	return false
}

func cutBlock(s string, width int) string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		if strings.Contains(ln, "\x1b") && lipgloss.Width(ln) > width {
			ln = stripANSI(ln)
		}
		lines[i] = truncWidth(ln, width)
	}
	return strings.Join(lines, "\n")
}

func groupColumns(width int) []table.Column {
	cols := []table.Column{
		{Title: "Group", Width: 0}, {Title: "Kind", Width: 10}, {Title: "Tensors", Width: 8},
		{Title: "Params*", Width: 9}, {Title: "Bytes", Width: 11}, {Title: "Experts", Width: 7}, {Title: "QWeights", Width: 8},
	}
	return flex(cols, width, 12)
}

func tensorColumns(width int) []table.Column {
	cols := []table.Column{
		{Title: "Tensor", Width: 0}, {Title: "DType", Width: 6}, {Title: "Shape", Width: 20},
		{Title: "Bytes", Width: 11}, {Title: "Role", Width: 8},
	}
	return flex(cols, width, 20)
}

// flex gives column 0 whatever width remains (cells have 1 char padding each side).
func flex(cols []table.Column, width, minW int) []table.Column {
	used := 0
	for _, c := range cols[1:] {
		used += c.Width
	}
	w := width - used - 2*len(cols)
	if w < minW {
		w = minW
	}
	cols[0].Width = w
	return cols
}

func groupRows(m *model.Model) []table.Row {
	rows := make([]table.Row, 0, len(m.Groups))
	for _, g := range m.Groups {
		exp := ""
		if g.HasExperts {
			exp = "yes"
		}
		rows = append(rows, table.Row{
			sanitize.From(g.Key).String(), sanitize.From(g.Kind).String(), fmt.Sprint(len(g.Tensors)), model.HumanCount(g.LogicalParams),
			model.HumanBytes(g.Bytes), exp, fmt.Sprint(g.QuantizedTensors)})
	}
	return rows
}

func tensorRows(g *model.Group) []table.Row {
	rows := make([]table.Row, 0, len(g.Tensors))
	for _, t := range g.Tensors {
		name := t.Name
		if g.Kind == model.KindLayer {
			name = model.ShortName(name)
		}
		role := t.Role
		if t.Role == model.RoleQuantWeight {
			role = fmt.Sprintf("q%d", t.Bits)
		}
		rows = append(rows, table.Row{
			sanitize.From(name).String(), sanitize.From(t.DType).String(), sanitize.From(fmt.Sprint(t.Shape)).String(),
			model.HumanBytes(t.ByteLen()), sanitize.From(role).String()})
	}
	return rows
}

func compact(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}
