package ui

import "github.com/cecilkootz/model-inspector/internal/sanitize"

// Safe is text a widget is allowed to draw. Build it with S from sanitized
// text, or with Chrome for strings this program already rendered (its own
// SGR and glyphs). A plain string does not satisfy the type.
type Safe struct{ s string }

// S wraps sanitized text.
func S(t sanitize.Text) Safe { return Safe{s: t.String()} }

// Chrome marks program-drawn text. File-derived bytes have to pass through
// sanitize.From and S before they are joined into a chrome string.
func Chrome(s string) Safe { return Safe{s: s} }

// String returns the text.
func (t Safe) String() string { return t.s }

// Empty reports the zero text.
func (t Safe) Empty() bool { return t.s == "" }
