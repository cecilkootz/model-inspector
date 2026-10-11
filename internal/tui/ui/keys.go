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

// Context is a screen or overlay keymap.
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

// Global bindings apply on every screen except where a context shadows them.
var Global = []Bind{
	{Keys: []string{"1"}, HelpKey: "1", Help: "Identity"},
	{Keys: []string{"2"}, HelpKey: "2", Help: "Config"},
	{Keys: []string{"3"}, HelpKey: "3", Help: "Layers"},
	{Keys: []string{"4"}, HelpKey: "4", Help: "Scan"},
	{Keys: []string{"tab"}, HelpKey: "tab", Help: "next tab", Hint: "tab"},
	{Keys: []string{"shift+tab"}, HelpKey: "S-tab", Help: "prev tab"},
	{Keys: []string{"right", "l"}, HelpKey: "→/l", Help: "next tab", Hint: "←→ tabs"},
	{Keys: []string{"left", "h"}, HelpKey: "←/h", Help: "prev tab"},
	{Keys: []string{"up", "k"}, HelpKey: "↑/k", Help: "move", Hint: "↑↓ scroll"},
	{Keys: []string{"down", "j"}, HelpKey: "↓/j", Help: "move"},
	{Keys: []string{"pgup"}, HelpKey: "pgup", Help: "page"},
	{Keys: []string{"pgdown"}, HelpKey: "pgdn", Help: "page"},
	{Keys: []string{"home", "g"}, HelpKey: "g", Help: "top"},
	{Keys: []string{"end", "G"}, HelpKey: "G", Help: "bottom"},
	{Keys: []string{"enter"}, HelpKey: "enter", Help: "open"},
	{Keys: []string{"esc"}, HelpKey: "esc", Help: "back"},
	{Keys: []string{"backspace"}, HelpKey: "bksp", Help: "back"},
	{Keys: []string{"?"}, HelpKey: "?", Help: "help", Hint: "? keys"},
	{Keys: []string{"q"}, HelpKey: "q", Help: "quit"},
	{Keys: []string{"ctrl+c"}, HelpKey: "ctrl+c", Help: "quit"},
	{Keys: []string{":"}, HelpKey: ":", Help: "commands", Hint: ": commands"},
	{Keys: []string{"ctrl+k"}, HelpKey: "ctrl+k", Help: "commands"},
	{Keys: []string{"w"}, HelpKey: "w", Help: "focus other pane"},
	{Keys: []string{"<"}, HelpKey: "<", Help: "shrink split"},
	{Keys: []string{">"}, HelpKey: ">", Help: "grow split"},
	{Keys: []string{"z"}, HelpKey: "z", Help: "zoom pane"},
	{Keys: []string{"m"}, HelpKey: "m", Help: "mouse", Hint: "m mouse"},
}

// Identity context adds no extra keys.
var Identity = Context{Name: "identity"}

// TreeDummy is the sanctioned tree context used until Config and Layers
// grow real trees. It may shadow only left, right, h, and l.
var TreeDummy = Context{
	Name:    "tree-dummy",
	Tree:    true,
	Shadows: []string{"left", "right", "h", "l"},
	Bindings: []Bind{
		{Keys: []string{"right", "l"}, HelpKey: "→/l", Help: "expand", Hint: "←→ fold"},
		{Keys: []string{"left", "h"}, HelpKey: "←/h", Help: "collapse"},
	},
}

// ConfigFields is the Key fields pane. Arrow keys stay global tab switches.
var ConfigFields = Context{
	Name: "config.fields",
	Bindings: []Bind{
		{Keys: []string{"/"}, HelpKey: "/", Help: "search", Hint: "/ search"},
		{Keys: []string{"y"}, HelpKey: "y", Help: "copy path"},
		{Keys: []string{"Y"}, HelpKey: "Y", Help: "copy value"},
	},
}

// ConfigTree is the config tree. It may shadow only left, right, h, and l.
var ConfigTree = Context{
	Name:    "config.tree",
	Tree:    true,
	Shadows: []string{"left", "right", "h", "l"},
	Bindings: []Bind{
		{Keys: []string{"right", "l"}, HelpKey: "→/l", Help: "expand", Hint: "←→ fold"},
		{Keys: []string{"left", "h"}, HelpKey: "←/h", Help: "collapse"},
		{Keys: []string{"space"}, HelpKey: "space", Help: "toggle", Hint: "␣ toggle"},
		{Keys: []string{"p"}, HelpKey: "p", Help: "parent"},
		{Keys: []string{"e"}, HelpKey: "e", Help: "expand all"},
		{Keys: []string{"E"}, HelpKey: "E", Help: "collapse all"},
		{Keys: []string{"+"}, HelpKey: "+", Help: "fold level"},
		{Keys: []string{"-"}, HelpKey: "-", Help: "fold level"},
		{Keys: []string{"/"}, HelpKey: "/", Help: "search", Hint: "/ search"},
		{Keys: []string{"n"}, HelpKey: "n", Help: "next match"},
		{Keys: []string{"N"}, HelpKey: "N", Help: "prev match"},
		{Keys: []string{"y"}, HelpKey: "y", Help: "copy path", Hint: "y/Y copy"},
		{Keys: []string{"Y"}, HelpKey: "Y", Help: "copy value"},
		{Keys: []string{"r"}, HelpKey: "r", Help: "raw", Hint: "r raw"},
		{Keys: []string{"b", "u"}, HelpKey: "b/u", Help: "page up"},
		{Keys: []string{"f", "d"}, HelpKey: "f/d", Help: "page down"},
	},
}

// ConfigSearch is the search input. Tab stays global so the handler can
// consume it while the input is focused; it is not registered here.
var ConfigSearch = Context{Name: "config.search"}

// LayersTree is the layers tree. It may shadow only left, right, h, and l.
var LayersTree = Context{
	Name:    "layers.tree",
	Tree:    true,
	Shadows: []string{"left", "right", "h", "l"},
	Bindings: []Bind{
		{Keys: []string{"right", "l"}, HelpKey: "→/l", Help: "expand", Hint: "←→ fold"},
		{Keys: []string{"left", "h"}, HelpKey: "←/h", Help: "collapse"},
		{Keys: []string{"space"}, HelpKey: "space", Help: "toggle", Hint: "␣ toggle"},
		{Keys: []string{"p"}, HelpKey: "p", Help: "parent"},
		{Keys: []string{"e"}, HelpKey: "e", Help: "expand all"},
		{Keys: []string{"E"}, HelpKey: "E", Help: "collapse all"},
		{Keys: []string{"+"}, HelpKey: "+", Help: "fold level"},
		{Keys: []string{"-"}, HelpKey: "-", Help: "fold level"},
		{Keys: []string{"/"}, HelpKey: "/", Help: "search", Hint: "/ search"},
		{Keys: []string{"n"}, HelpKey: "n", Help: "next match"},
		{Keys: []string{"N"}, HelpKey: "N", Help: "prev match"},
		{Keys: []string{"y"}, HelpKey: "y", Help: "copy path", Hint: "y/Y copy"},
		{Keys: []string{"Y"}, HelpKey: "Y", Help: "copy value"},
		{Keys: []string{"s"}, HelpKey: "s", Help: "sort", Hint: "s sort"},
		{Keys: []string{"S"}, HelpKey: "S", Help: "reverse sort"},
		{Keys: []string{"o"}, HelpKey: "o", Help: "outliers", Hint: "o outliers"},
		{Keys: []string{"t"}, HelpKey: "t", Help: "attention", Hint: "t attn"},
		{Keys: []string{"x"}, HelpKey: "x", Help: "parts mode"},
		{Keys: []string{"#"}, HelpKey: "#", Help: "jump"},
		{Keys: []string{"b", "u"}, HelpKey: "b/u", Help: "page up"},
		{Keys: []string{"f", "d"}, HelpKey: "f/d", Help: "page down"},
	},
}

// LayersSearch is the layers search input. Tab stays global so the handler
// can consume it while the input is focused; it is not registered here.
var LayersSearch = Context{Name: "layers.search"}

// LayersDetail is the detail pane. It is not a tree, so arrows stay global.
var LayersDetail = Context{
	Name: "layers.detail",
	Bindings: []Bind{
		{Keys: []string{"y"}, HelpKey: "y", Help: "copy name"},
		{Keys: []string{"Y"}, HelpKey: "Y", Help: "copy range"},
	},
}

// LayersJump is the layer prompt. Digits, enter, and esc stay with the
// handler while it is focused, the same way search consumes tab.
var LayersJump = Context{Name: "layers.jump"}

// ScanList is the check timeline. Arrow keys stay global.
var ScanList = Context{
	Name: "scan.list",
	Bindings: []Bind{
		{Keys: []string{"space"}, HelpKey: "space", Help: "expand", Hint: "space expand"},
		{Keys: []string{"e"}, HelpKey: "e", Help: "expand all"},
		{Keys: []string{"E"}, HelpKey: "E", Help: "collapse all"},
		{Keys: []string{"]"}, HelpKey: "]", Help: "next problem"},
		{Keys: []string{"["}, HelpKey: "[", Help: "previous problem"},
		{Keys: []string{"i"}, HelpKey: "i", Help: "not covered", Hint: "i not covered"},
		{Keys: []string{"c"}, HelpKey: "c", Help: "code & pickles", Hint: "c code"},
		{Keys: []string{"y"}, HelpKey: "y", Help: "copy check"},
		{Keys: []string{"Y"}, HelpKey: "Y", Help: "copy scan"},
		{Keys: []string{"b", "u"}, HelpKey: "b/u", Help: "page up"},
		{Keys: []string{"f", "d"}, HelpKey: "f/d", Help: "page down"},
	},
}

// ScanDetail scrolls the drill. It adds the same scan keys that are not global.
var ScanDetail = Context{
	Name: "scan.detail",
	Bindings: []Bind{
		{Keys: []string{"]"}, HelpKey: "]", Help: "next problem"},
		{Keys: []string{"["}, HelpKey: "[", Help: "previous problem"},
		{Keys: []string{"i"}, HelpKey: "i", Help: "not covered"},
		{Keys: []string{"c"}, HelpKey: "c", Help: "code & pickles"},
		{Keys: []string{"y"}, HelpKey: "y", Help: "copy check"},
		{Keys: []string{"Y"}, HelpKey: "Y", Help: "copy scan"},
	},
}

// ScanCode is the names-only list. Enter stays global.
var ScanCode = Context{
	Name: "scan.code",
	Bindings: []Bind{
		{Keys: []string{"c"}, HelpKey: "c", Help: "open disclosure"},
	},
}

// ScanDisclose is the modal. Enter and esc stay global; the handler ignores
// every other key. c confirms.
var ScanDisclose = Context{
	Name: "scan.disclose",
	Bindings: []Bind{
		{Keys: []string{"c"}, HelpKey: "c", Help: "confirm"},
	},
}

// Contexts is every context the conflict test walks.
var Contexts = []Context{Identity, TreeDummy, ConfigFields, ConfigTree, ConfigSearch, LayersTree, LayersSearch, LayersDetail, LayersJump, ScanList, ScanDetail, ScanCode, ScanDisclose}

// HintLine is the status-bar hint for a context. Tree contexts say fold.
func HintLine(tree bool) string {
	up, side := "↑↓", "←→"
	if !UTF8() {
		up, side = "up/down", "<->"
	}
	if tree {
		return up + " scroll" + SepWord() + side + " fold" + SepWord() + "? keys"
	}
	return up + " scroll" + SepWord() + side + " tabs" + SepWord() + "? keys"
}

// ActiveKeys returns global keys plus context keys, dropping shadowed globals.
func ActiveKeys(ctx Context) []string {
	shadow := map[string]bool{}
	if ctx.Tree {
		for _, s := range ctx.Shadows {
			shadow[s] = true
		}
	}
	var out []string
	seen := map[string]bool{}
	add := func(k string) {
		if seen[k] {
			return
		}
		seen[k] = true
		out = append(out, k)
	}
	for _, b := range Global {
		for _, k := range b.Keys {
			if shadow[k] {
				continue
			}
			add(k)
		}
	}
	for _, b := range ctx.Bindings {
		for _, k := range b.Keys {
			add(k)
		}
	}
	return out
}

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
