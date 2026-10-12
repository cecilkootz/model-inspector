package ui

import "github.com/charmbracelet/bubbles/key"

// Bind is one key binding in the registry.
type Bind struct {
	Keys    []string
	HelpKey string
	Help    string
	Hint    string
	Context string // empty means global
}

// Context is a keymap layered over Global.
type Context struct {
	Name     string
	Tree     bool
	Shadows  []string
	Bindings []Bind
}

// Binding converts a Bind into a bubbles key binding.
func (b Bind) Binding() key.Binding {
	return key.NewBinding(key.WithKeys(b.Keys...), key.WithHelp(b.HelpKey, b.Help))
}

// Global bindings apply in every context except where a Tree context
// shadows them.
var Global = []Bind{
	{Keys: []string{"1", "2", "3", "4"}, HelpKey: "1-4", Help: "focus panel", Hint: "1-4 panels"},
	{Keys: []string{"tab"}, HelpKey: "tab", Help: "next panel", Hint: "tab next panel"},
	{Keys: []string{"shift+tab"}, HelpKey: "S-tab", Help: "previous panel"},
	{Keys: []string{"up", "k"}, HelpKey: "↑/k", Help: "move", Hint: "↑↓ move"},
	{Keys: []string{"down", "j"}, HelpKey: "↓/j", Help: "move"},
	{Keys: []string{"pgup", "b", "u"}, HelpKey: "pgup", Help: "page up"},
	{Keys: []string{"pgdown", "f", "d"}, HelpKey: "pgdn", Help: "page down"},
	{Keys: []string{"home", "g"}, HelpKey: "g", Help: "top"},
	{Keys: []string{"end", "G"}, HelpKey: "G", Help: "bottom"},
	{Keys: []string{"enter"}, HelpKey: "enter", Help: "detail", Hint: "⏎ detail"},
	{Keys: []string{"esc"}, HelpKey: "esc", Help: "back"},
	{Keys: []string{"?"}, HelpKey: "?", Help: "help", Hint: "? keys"},
	{Keys: []string{"q"}, HelpKey: "q", Help: "quit"},
	{Keys: []string{"ctrl+c"}, HelpKey: "ctrl+c", Help: "quit"},
	{Keys: []string{":"}, HelpKey: ":", Help: "commands", Hint: ": commands"},
	{Keys: []string{"ctrl+k"}, HelpKey: "ctrl+k", Help: "commands"},
	{Keys: []string{"m"}, HelpKey: "m", Help: "mouse", Hint: "m mouse"},
}

// Shell is the panels screen. Left and right fold the detail tree of Layers
// and Config, so it is a tree context and may shadow only those keys.
var Shell = Context{
	Name:    "shell",
	Tree:    true,
	Shadows: []string{"left", "right", "h", "l"},
	Bindings: []Bind{
		{Keys: []string{"0"}, HelpKey: "0", Help: "detail pane"},
		{Keys: []string{"/"}, HelpKey: "/", Help: "filter list", Hint: "/ filter"},
		{Keys: []string{"]"}, HelpKey: "]", Help: "next problem", Hint: "] next problem"},
		{Keys: []string{"["}, HelpKey: "[", Help: "previous problem"},
		{Keys: []string{"y"}, HelpKey: "y", Help: "copy", Hint: "y copy"},
		{Keys: []string{"r"}, HelpKey: "r", Help: "raw config", Hint: "r raw"},
		{Keys: []string{"space"}, HelpKey: "space", Help: "toggle (detail)"},
		{Keys: []string{"right", "l"}, HelpKey: "→/l", Help: "expand (detail)"},
		{Keys: []string{"left", "h"}, HelpKey: "←/h", Help: "collapse (detail)"},
	},
}

// Contexts is every context the conflict test walks.
var Contexts = []Context{Shell}

// DuplicateKeys returns keys bound more than once in global ∪ ctx.
// Shadowed keys are taken from the context only.
func DuplicateKeys(ctx Context) []string {
	shadow := map[string]bool{}
	if ctx.Tree {
		for _, s := range ctx.Shadows {
			shadow[s] = true
		}
	}
	count := map[string]int{}
	for _, b := range Global {
		for _, k := range b.Keys {
			if shadow[k] {
				continue
			}
			count[k]++
		}
	}
	for _, b := range ctx.Bindings {
		for _, k := range b.Keys {
			count[k]++
		}
	}
	var dups []string
	for k, n := range count {
		if n > 1 {
			dups = append(dups, k)
		}
	}
	return dups
}
