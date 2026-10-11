package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/report"
	"github.com/cecilkootz/model-inspector/internal/scan"
	"github.com/cecilkootz/model-inspector/internal/tui/ui"
)

// Finding is one content-reading result. PR3 fills it; this version only
// reserves the type.
type Finding struct {
	Path   string
	Detail string
}

// ContentReader reads opted-in file contents. It stays nil until a later
// version registers one. A nil reader disables the confirm button.
type ContentReader interface {
	Read(ctx context.Context, m *model.Model, files []string, progress chan<- model.Progress) (scan.Check, []Finding, error)
}

// contentReader is the PR3 slot. Nil means content reading is not available.
var contentReader ContentReader

// SetContentReader registers the opt-in reader. Tests and PR3 use it.
func SetContentReader(r ContentReader) { contentReader = r }

// loadModel is the directory loader the scan screen runs. Tests replace it.
var loadModel = model.LoadWithProgress

type scanArmMsg struct{}
type scanProgMsg struct{ p model.Progress }
type scanLoadedMsg struct {
	m   *model.Model
	res scan.Result
	err error
}

// RunLoading opens the TUI immediately and loads dir inside it.
// Quitting from the load-error card returns 3. Every other quit returns 0.
func RunLoading(dir string, strict, fromCache bool, repo, rev string, opt ...Options) (code int, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			restoreTerminal()
			err = fmt.Errorf("tui panic: %v", rec)
			code = 1
		}
	}()
	t := NewLoading(dir, strict, fromCache, repo, rev)
	if len(opt) > 0 {
		t.apply(opt[0])
	}
	opts := []tea.ProgramOption{tea.WithAltScreen(), tea.WithReportFocus()}
	if t.mouse {
		opts = append(opts, tea.WithMouseCellMotion())
	}
	final, err := tea.NewProgram(t, opts...).Run()
	if err != nil && strings.Contains(err.Error(), "panic") {
		restoreTerminal()
		return 1, err
	}
	if err != nil {
		return 1, err
	}
	if m, ok := final.(Model); ok {
		return m.exitCode(), nil
	}
	return 0, nil
}

// NewLoading builds a Scan screen that loads dir on Init.
func NewLoading(dir string, strict, fromCache bool, repo, rev string) Model {
	t := New(nil, scan.Result{})
	t.active = tabScan
	if t.scn == nil {
		t.scn = &scanView{}
	}
	t.scn.loading = true
	t.scn.dir = dir
	t.scn.strict = strict
	t.scn.fromCache = fromCache
	t.scn.repo = repo
	t.scn.rev = rev
	return t
}

func (t Model) exitCode() int {
	if t.scn != nil && t.scn.loadErr != "" {
		return 3
	}
	return 0
}

func (t Model) loadCmds() tea.Cmd {
	if t.scn == nil || !t.scn.loading {
		return nil
	}
	ch := make(chan model.Progress, 64)
	t.scn.progCh = ch
	return tea.Batch(
		tea.Tick(150*time.Millisecond, func(time.Time) tea.Msg { return scanArmMsg{} }),
		waitScanProg(ch),
		runScanLoad(t),
	)
}

func waitScanProg(ch <-chan model.Progress) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		p, ok := <-ch
		if !ok {
			return nil
		}
		return scanProgMsg{p: p}
	}
}

func runScanLoad(t Model) tea.Cmd {
	return func() tea.Msg {
		if t.scn == nil {
			return scanLoadedMsg{err: fmt.Errorf("scan screen was not initialised")}
		}
		m, err := loadModel(t.scn.dir, t.scn.progCh)
		if t.scn.progCh != nil {
			close(t.scn.progCh)
		}
		if err != nil {
			return scanLoadedMsg{err: err}
		}
		if t.scn.repo != "" || t.scn.rev != "" {
			m.RepoID, m.Revision = t.scn.repo, t.scn.rev
		}
		res := finishScan(m, t.scn.strict, t.scn.fromCache)
		return scanLoadedMsg{m: m, res: res}
	}
}

// finishScan runs the checks and the same partial refresh the text and JSON
// paths use, so the TUI verdict matches report.ExitCode.
func finishScan(m *model.Model, strict, fromCache bool) scan.Result {
	opt := scan.Options{Strict: strict}
	res := scan.RunOptions(m, opt)
	doc := report.Build(m, res, fromCache, 0)
	if m != nil && m.Partial && res.Overall != scan.StatusFail {
		res = scan.RunOptions(m, opt)
	}
	if doc.Scan.Overall == string(scan.StatusFail) && res.Overall != scan.StatusFail {
		res = scan.RunOptions(m, opt)
	}
	return res
}

func (t Model) onScanLoaded(msg scanLoadedMsg) (tea.Model, tea.Cmd) {
	if t.scn == nil {
		t.scn = &scanView{}
	}
	t.scn.loading = false
	t.scn.armed = false
	if msg.err != nil {
		t.scn.loadErr = clipScan(msg.err.Error())
		t.touchScan()
		return t, nil
	}
	t.m = msg.m
	t.scan = msg.res
	if t.m != nil {
		d := derive(t.m)
		t.derived = &d
	}
	t.touchScan()
	return t, nil
}

func (t *Model) touchScan() {
	if t.cache != nil {
		t.cache.key = ""
	}
}

func (t Model) updateScan(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if t.scn != nil && t.scn.loadErr != "" {
		if msg.String() == "q" {
			return t, tea.Quit
		}
		return t, nil
	}
	if t.scn != nil && t.scn.code {
		return t.updateScanCode(msg)
	}
	if t.scn != nil && t.scn.drill {
		return t.updateScanDetail(msg)
	}
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
		t.openScanEnter()
	case " ":
		t.toggleScanCard(false)
	case "e":
		t.expandScan(true)
	case "E":
		t.expandScan(false)
	case "]":
		t.jumpProblem(1)
	case "[":
		t.jumpProblem(-1)
	case "i":
		t.toggleNotCovered()
	case "c":
		t.openCodeList()
	case "y":
		t.copyScan(false)
	case "Y":
		t.copyScan(true)
	}
	t.ensureScanVisible()
	t.touchScan()
	return t, nil
}

func (t *Model) openScanEnter() {
	if t.scanCursor < 0 || t.scanCursor >= len(t.scan.Checks) {
		return
	}
	id := t.scan.Checks[t.scanCursor].ID
	if id == scan.CheckCodeExec || id == scan.CheckCode {
		t.openCodeList()
		return
	}
	br := ui.Breakpoint(t.width, t.height)
	if br == "S" {
		if t.scn == nil {
			t.scn = &scanView{}
		}
		t.scn.drill = true
		return
	}
	t.focusSecondary = true
}

func (t *Model) toggleScanCard(all bool) {
	if t.scanOpen == nil {
		t.scanOpen = map[string]bool{}
	}
	if all {
		return
	}
	if t.scanCursor < 0 || t.scanCursor >= len(t.scan.Checks) {
		return
	}
	id := t.scan.Checks[t.scanCursor].ID
	t.scanOpen[id] = !t.scanOpen[id]
}

func (t *Model) expandScan(open bool) {
	if t.scanOpen == nil {
		t.scanOpen = map[string]bool{}
	}
	for _, c := range t.scan.Checks {
		if open {
			t.scanOpen[c.ID] = true
		} else {
			delete(t.scanOpen, c.ID)
		}
	}
}

func (t *Model) jumpProblem(dir int) {
	n := len(t.scan.Checks)
	if n == 0 {
		return
	}
	i := t.scanCursor
	for step := 0; step < n; step++ {
		i += dir
		if i < 0 {
			i = n - 1
		}
		if i >= n {
			i = 0
		}
		st := t.scan.Checks[i].EffectiveStatus
		if st == scan.StatusWarn || st == scan.StatusFail {
			t.scanCursor = i
			return
		}
	}
}

func (t *Model) toggleNotCovered() {
	if t.scn == nil {
		t.scn = &scanView{}
	}
	cur := t.ncOpen()
	t.scn.ncUser = true
	t.scn.ncOpen = !cur
}

func (t *Model) openCodeList() {
	if t.scn == nil {
		t.scn = &scanView{}
	}
	t.scn.code = true
	t.scn.codeCur = 0
}

func (t Model) updateScanCode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	files := codeFiles(t.m)
	switch msg.String() {
	case "esc":
		t.scn.code = false
	case "up", "k":
		if t.scn.codeCur > 0 {
			t.scn.codeCur--
		}
	case "down", "j":
		if t.scn.codeCur < len(files)-1 {
			t.scn.codeCur++
		}
	case "enter", "c":
		t.disclosure = true
	case "q":
		t.scn.code = false
	}
	t.touchScan()
	return t, nil
}

func (t Model) updateScanDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		t.scn.drill = false
	case "up", "k":
		if t.scn.detailTop > 0 {
			t.scn.detailTop--
		}
	case "down", "j":
		t.scn.detailTop++
	case "i":
		t.toggleNotCovered()
	case "c":
		t.openCodeList()
		t.scn.drill = false
	case "y":
		t.copyScan(false)
	case "Y":
		t.copyScan(true)
	case "]":
		t.jumpProblem(1)
	case "[":
		t.jumpProblem(-1)
	}
	t.touchScan()
	return t, nil
}

func (t *Model) copyScan(all bool) {
	var payload string
	if all {
		payload = t.scanPlain()
	} else if t.scanCursor >= 0 && t.scanCursor < len(t.scan.Checks) {
		payload = t.cardPlain(t.scan.Checks[t.scanCursor])
	}
	if payload == "" {
		t.notice = "nothing to copy"
		return
	}
	if t.clipboardOff() {
		t.notice = "not copied (OSC 52 disabled); select it from the value strip"
		return
	}
	if !t.canCopy() {
		t.notice = "not copied (terminal blocked OSC 52); select it from the value strip"
		return
	}
	writeOSC(t.copyWriter(), payload)
	if all {
		t.notice = "copied scan"
	} else {
		t.notice = "copied check"
	}
}

func (t Model) cardPlain(c scan.Check) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s  %s\n%s\n", c.Name, stripANSI(statusPill(c)), clipScan(c.Summary))
	if why := scan.Why(c.ID); why != "" {
		fmt.Fprintf(&b, "Why it matters: %s\n", why)
	}
	if rule := scan.Rule(c.ID); rule != "" {
		fmt.Fprintf(&b, "Rule: %s\n", rule)
	}
	if np := scan.NotProven(c.ID); np != "" {
		fmt.Fprintf(&b, "Does not prove: %s\n", np)
	}
	for _, d := range c.Details {
		fmt.Fprintf(&b, "  %s\n", clipScan(d))
	}
	return b.String()
}

func (t Model) scanPlain() string {
	var b strings.Builder
	title, _ := verdictTitle(t.m, t.scan)
	fmt.Fprintf(&b, "%s\n%s\n", title, scan.Disclaimer)
	for _, c := range t.scan.Checks {
		b.WriteString(t.cardPlain(c))
		b.WriteByte('\n')
	}
	return b.String()
}

func (t Model) waitingLine() string {
	msg := "reading…"
	if t.scn != nil && t.scn.armed {
		p := t.scn.prog
		msg = strings.ToLower(progressTitle(p.Phase))
		if p.Total > 0 {
			msg = fmt.Sprintf("%s %d/%d…", msg, p.Done, p.Total)
		}
	}
	return ui.Fit(msg, t.width)
}

func (t Model) scanClick(id string, double bool) tea.Model {
	switch {
	case id == "scan:nc" || id == "scan:chip:nr":
		t.toggleNotCovered()
	case id == "scan:code" || strings.HasPrefix(id, "scan:btn:"):
		if id == "scan:btn:cancel" {
			t.disclosure = false
			break
		}
		t.openCodeList()
		if id == "scan:btn:read" || double {
			t.disclosure = true
		}
	case strings.HasPrefix(id, "scan:chip:"):
		want := strings.TrimPrefix(id, "scan:chip:")
		for i, c := range t.scan.Checks {
			if string(c.EffectiveStatus) == want {
				t.scanCursor = i
				break
			}
		}
	case strings.HasPrefix(id, "scan:exp:"):
		cid := strings.TrimPrefix(id, "scan:exp:")
		if t.scanOpen == nil {
			t.scanOpen = map[string]bool{}
		}
		t.scanOpen[cid] = !t.scanOpen[cid]
		for i, c := range t.scan.Checks {
			if c.ID == cid {
				t.scanCursor = i
			}
		}
	case strings.HasPrefix(id, "scan:card:"):
		cid := strings.TrimPrefix(id, "scan:card:")
		for i, c := range t.scan.Checks {
			if c.ID == cid {
				t.scanCursor = i
			}
		}
		if double {
			t.openScanEnter()
		}
	}
	t.ensureScanVisible()
	t.touchScan()
	return t
}

func (t Model) scanHintSegs() []string {
	up := "↑↓"
	enter := "⏎"
	if !ui.UTF8() {
		up = "up/down"
		enter = "enter"
	}
	sep := ui.SepWord()
	if t.scn != nil && t.scn.code {
		return []string{up + sep + enter + " details" + sep + "c code"}
	}
	if t.width < 100 {
		return []string{up + sep + enter + " details"}
	}
	return []string{up + " check" + sep + enter + " details" + sep + "space expand" + sep + "i not covered"}
}

func (t Model) scanVerdictText() (string, ui.FillKind) {
	if t.scn != nil && t.scn.loading {
		return "… scanning", ui.FillKindNR
	}
	if t.scn != nil && t.scn.loadErr != "" {
		return ui.G().Fail + " error", ui.FillKindFail
	}
	g := ui.G()
	pass, warn, fail, _, _ := bandCounts(t.scan)
	switch t.scan.Overall {
	case scan.StatusFail:
		return g.Fail + fmt.Sprintf(" %d fail", fail), ui.FillKindFail
	case scan.StatusWarn:
		word := "warn"
		if warn != 1 {
			word = "warn"
		}
		return g.Warn + fmt.Sprintf(" %d %s", warn, word), ui.FillKindWarn
	case scan.StatusPass:
		if t.width >= 100 {
			return g.Pass + fmt.Sprintf(" structure %d/%d", pass, len(t.scan.Checks)), ui.FillKindOK
		}
		return g.Pass + fmt.Sprintf(" %d/%d", pass, len(t.scan.Checks)), ui.FillKindOK
	default:
		return g.NR + " not run", ui.FillKindNR
	}
}

func (t Model) scanContentText() (string, ui.FillKind) {
	mark := "◇"
	if !ui.UTF8() {
		mark = "o"
	}
	return mark + " headers-only", ui.FillKindNR
}

func (t Model) scanModeName() string {
	if t.scn != nil && t.scn.drill {
		return "DETAIL"
	}
	if t.scn != nil && t.scn.code {
		return "CODE"
	}
	if t.scan.Strict {
		return "SCAN · STRICT"
	}
	return "SCAN"
}

func (t Model) disclosureBody() string {
	n := 0
	if t.m != nil {
		n = len(t.m.CodeExecFiles())
	}
	return foldASCII(fmt.Sprintf("The Code & pickle contents check has read these %d files (≤ 8 MiB of each pickle, ≤ 1 MiB of each .py, ≤ 64 MiB in all). Names only: nothing is executed, imported, unpickled or decompressed.", n))
}

func (t Model) disclosureConfirm() (label string, enabled bool) {
	if contentReader == nil {
		return "already read by the scan", false
	}
	return "Read contents", true
}
