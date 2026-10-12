package tui

import (
	"github.com/cecilkootz/model-inspector/internal/sanitize"
	"github.com/cecilkootz/model-inspector/internal/tui/ui"
)

// sidePanel is one of the four stacked panels of the panels layout. Content
// PRs replace one adapter each; the shell only talks to this interface.
type sidePanel interface {
	num() int                // 1..4
	title() sanitize.Text    //
	counter() string         // pre-styled
	rows(width int) []string // full list, pre-rendered rows
	summary(width int) []string
	cursor() int
	move(delta int) // clamps
	detail(width, height int) []string
	keys() []ui.KeyHint
	problems() []int // row indices, for ] and [
}

// detailTitled names the detail pane after the selection.
type detailTitled interface {
	selection() (title sanitize.Text, tag ui.Safe)
}

// detailDriver is for panels whose detail is a tree with its own cursor.
// The shell scrolls the rendered lines of every other panel.
type detailDriver interface {
	scrollDetail(delta int)
	foldDetail(key string)
}

// detailToggler is for panels whose detail has a second view behind `r`.
type detailToggler interface {
	toggleDetail()
}

// sidePanels builds the adapters over t. They hold the pointer so a move
// lands in the Model the caller returns.
func (t *Model) sidePanels() [4]sidePanel {
	return [4]sidePanel{&modelPanel{t}, &checksPanel{t}, &layersPanel{t}, &configPanel{t}}
}

// paneView is t sized to a detail pane, so the screen renderers lay out for
// the pane rather than the terminal. Their state is shared through pointers.
func (t Model) paneView(w, h int) Model {
	c := t
	c.width, c.height = w, h+3
	return c
}

func upDown() string {
	if ui.UTF8() {
		return "↑↓"
	}
	return "up/down"
}

func enterKey() string {
	if ui.UTF8() {
		return "⏎"
	}
	return "enter"
}

func hint(key, desc string) ui.KeyHint {
	return ui.KeyHint{Key: sanitize.Program(key), Desc: sanitize.Program(desc)}
}

func moveHints(extra ...ui.KeyHint) []ui.KeyHint {
	out := []ui.KeyHint{hint(upDown(), "move"), hint(enterKey(), "detail"), hint("?", "keys")}
	out = append(out, extra...)
	return append(out, hint("1-4", "panels"), hint("tab", "next panel"))
}

func plainRow(name, value sanitize.Text, width int) string {
	return ui.ListRow(ui.RowSpec{Name: name, Value: value}, width)
}

func muted(s string) string { return ui.PaintToken(s, ui.TextMuted, nil, false) }
