package sanitize

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	gotoken "go/token"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNotefSanitizesEveryUntrustedArg(t *testing.T) {
	hostile := "a\x1b]0;PWN\x07\u202eb"
	type named string
	check := func(t *testing.T, got, token string) {
		t.Helper()
		if strings.Contains(got, "\x1b") || strings.ContainsRune(got, '\u202e') || strings.Contains(got, "\a") {
			t.Fatalf("raw control in %q", got)
		}
		if !strings.Contains(got, token) {
			t.Fatalf("missing %s in %q", token, got)
		}
	}
	check(t, Notef("%v", errors.New(hostile)).Error(), "<U+001B>")
	check(t, Notef("%v", stringer{hostile}).Error(), "<U+001B>")
	check(t, Notef("%s", []byte(hostile)).Error(), "<U+001B>")
	check(t, Notef("%v", []string{hostile}).Error(), "<U+001B>")
	check(t, Notef("%v", struct{ S string }{hostile}).Error(), "<U+001B>")
	check(t, Notef("%v", map[string]string{"k": hostile}).Error(), "<U+001B>")
	check(t, Notef("%s", named(hostile)).Error(), "<U+001B>")
	check(t, Notef("%c", rune('\x1b')).Error(), "<U+001B>")
	check(t, Notef("%c", 0x202e).Error(), "<U+202E>")
	if got := Notef("pass org/name@<commit> %s", "ok").Error(); !strings.Contains(got, "org/name@<commit>") || strings.Contains(got, "U+003C") {
		t.Fatalf("placeholder: %q", got)
	}
	if got := Notef("n=%d ok=%t", 7, false).Error(); got != "n=7 ok=false" {
		t.Fatalf("numbers: %q", got)
	}
	if got := Notef("%*d", 4, 2).Error(); got != "   2" {
		t.Fatalf("star width: %q", got)
	}
	if got := Notef("%6d", 2).Error(); got != "     2" {
		t.Fatalf("width: %q", got)
	}

	check(t, Notef("%v", stringerInt(1)).Error(), "<U+001B>")
	check(t, Notef("%v", errorInt(1)).Error(), "<U+001B>")
	check(t, Notef("%#v", goStringInt(1)).Error(), "<U+001B>")
	check(t, Notef("%v", formatInt(1)).Error(), "<U+001B>")
	check(t, Notef("%v", stringerBool(true)).Error(), "<U+001B>")
	check(t, Notef("%v", (*nilStringer)(nil)).Error(), "<U+001B>")
	if got := Notef("%v", (*panicStringer)(nil)).Error(); got != "<nil>" {
		t.Fatalf("panicking nil stringer: %q", got)
	}
	if got := Notef("%d", plainInt(3)).Error(); got != "3" {
		t.Fatalf("named int: %q", got)
	}
	if got := Notef("%d", stringerInt(4)).Error(); got != "4" || strings.Contains(got, "U+") {
		t.Fatalf("%%d of stringer int: %q", got)
	}
	if got := Notef("%t", plainBool(true)).Error(); got != "true" {
		t.Fatalf("named bool: %q", got)
	}
	if got := Notef("%c", plainInt(0x1b)).Error(); !strings.Contains(got, "<U+001B>") || strings.Contains(got, "\x1b") {
		t.Fatalf("%%c named int: %q", got)
	}
	if got := Notef("%*d", plainInt(4), plainInt(2)).Error(); got != "   2" {
		t.Fatalf("named star: %q", got)
	}

	already := From("e\x1b")
	if got := Notef("%s", already).Error(); got != "e<U+001B>" || strings.Contains(got, "U+003C") {
		t.Fatalf("Text twice: %q", got)
	}
	if got := Notef("%s", Program("<commit>")).Error(); got != "<commit>" || strings.Contains(got, "U+003C") {
		t.Fatalf("Program twice: %q", got)
	}
	if got := Notef("%v", nil).Error(); got != "<nil>" {
		t.Fatalf("nil: %q", got)
	}
	var p *int
	if got := Notef("%v", p).Error(); got != "<nil>" {
		t.Fatalf("nil pointer: %q", got)
	}
	var ch chan int
	if got := Notef("%v", ch).Error(); got != "<nil>" {
		t.Fatalf("nil chan: %q", got)
	}
	var fn func()
	var boxed any = fn
	if got := Notef("%v", boxed).Error(); got != "<nil>" {
		t.Fatalf("nil func: %q", got)
	}
	var slice []string
	if got := Notef("%v", slice).Error(); got != "[]" || strings.Contains(got, "U+003C") {
		t.Fatalf("nil slice: %q", got)
	}
	var m map[string]string
	if got := Notef("%v", m).Error(); got != "map[]" || strings.Contains(got, "U+003C") {
		t.Fatalf("nil map: %q", got)
	}
}

func hostileText() string { return "a\x1b]0;PWN\x07\u202eb" }

type stringerInt int

func (stringerInt) String() string { return hostileText() }

type errorInt int

func (errorInt) Error() string { return hostileText() }

type goStringInt int

func (goStringInt) GoString() string { return hostileText() }

type formatInt int

func (formatInt) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, hostileText()) }

type plainInt int

type plainBool bool

type stringerBool bool

func (stringerBool) String() string { return hostileText() }

type nilStringer struct{}

func (*nilStringer) String() string { return hostileText() }

type panicStringer struct{}

func (*panicStringer) String() string { panic("nil") }

type stringer struct{ s string }

func (s stringer) String() string { return s.s }

func TestNotefFormatsAreLiterals(t *testing.T) {
	root := moduleRoot(t)
	fset := gotoken.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			base := d.Name()
			if base == ".git" || base == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, msg := range notefProblems(fset, file) {
			t.Error(msg)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestNotefFormatParamIsUnexported(t *testing.T) {
	fset := gotoken.NewFileSet()
	file, err := parser.ParseFile(fset, "sanitize.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var spec *ast.TypeSpec
	var fn *ast.FuncDecl
	for _, d := range file.Decls {
		switch d := d.(type) {
		case *ast.GenDecl:
			for _, s := range d.Specs {
				ts, ok := s.(*ast.TypeSpec)
				if ok && ts.Name.Name == "formatLit" {
					spec = ts
				}
			}
		case *ast.FuncDecl:
			if d.Name.Name == "Notef" {
				fn = d
			}
		}
	}
	if spec == nil || spec.Assign.IsValid() {
		t.Fatal("formatLit must be a defined type, not an alias of string")
	}
	if _, ok := spec.Type.(*ast.Ident); !ok || spec.Type.(*ast.Ident).Name != "string" {
		t.Fatal("formatLit underlying type must be string")
	}
	if fn == nil || fn.Type.Params == nil || len(fn.Type.Params.List) < 1 {
		t.Fatal("Notef is missing")
	}
	id, ok := fn.Type.Params.List[0].Type.(*ast.Ident)
	if !ok || id.Name != "formatLit" || ast.IsExported(id.Name) {
		t.Fatalf("Notef format parameter is %v, want unexported formatLit", fn.Type.Params.List[0].Type)
	}
}

func TestNotefAliasesAndParensAreFlagged(t *testing.T) {
	src := `package p
func Notef(format string, args ...any) {}
func ok() {
	Notef("%s", "a")
	(Notef)("%d", 1)
}
func apply(fn any, msg string) {}
func bad(msg string) {
	f := Notef
	apply(Notef, msg)
	Notef(msg)
	(Notef)(msg)
}
`
	fset := gotoken.NewFileSet()
	file, err := parser.ParseFile(fset, "alias.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, msg := range notefProblems(fset, file) {
		got = append(got, msg)
	}
	for _, want := range []string{"used as a value", "used as a value", "not a string literal", "not a string literal"} {
		found := false
		for i, msg := range got {
			if strings.Contains(msg, want) {
				got = append(got[:i], got[i+1:]...)
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing %q in %v", want, got)
		}
	}
	if len(got) != 0 {
		t.Fatalf("unexpected: %v", got)
	}
}

func notefProblems(fset *gotoken.FileSet, file *ast.File) []string {
	var out []string
	var stack []ast.Node
	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		if msg, ok := notefProblem(n, stack); ok {
			out = append(out, fset.Position(n.Pos()).String()+": "+msg)
		}
		stack = append(stack, n)
		return true
	})
	return out
}

func notefProblem(n ast.Node, stack []ast.Node) (string, bool) {
	if !isNotefExpr(n, stack) || isFuncDeclName(n, stack) {
		return "", false
	}
	call, ok := callOfNotef(n, stack)
	if !ok {
		return "Notef is used as a value", true
	}
	if len(call.Args) < 1 {
		return "Notef has no format string", true
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != gotoken.STRING {
		return "Notef format is not a string literal", true
	}
	return "", false
}

func isNotefExpr(n ast.Node, stack []ast.Node) bool {
	switch e := n.(type) {
	case *ast.Ident:
		if e.Name != "Notef" {
			return false
		}
		if len(stack) > 0 {
			if sel, ok := stack[len(stack)-1].(*ast.SelectorExpr); ok && sel.Sel == e {
				return false
			}
		}
		return true
	case *ast.SelectorExpr:
		return e.Sel != nil && e.Sel.Name == "Notef"
	default:
		return false
	}
}

func isFuncDeclName(n ast.Node, stack []ast.Node) bool {
	id, ok := n.(*ast.Ident)
	if !ok || len(stack) == 0 {
		return false
	}
	fd, ok := stack[len(stack)-1].(*ast.FuncDecl)
	return ok && fd.Name == id
}

// callOfNotef unwraps parentheses so (Notef)("%s", x) is still a call.
func callOfNotef(n ast.Node, stack []ast.Node) (*ast.CallExpr, bool) {
	cur := n
	for i := len(stack) - 1; i >= 0; i-- {
		switch p := stack[i].(type) {
		case *ast.ParenExpr:
			if p.X != cur {
				return nil, false
			}
			cur = p
		case *ast.CallExpr:
			if p.Fun == cur {
				return p, true
			}
			return nil, false
		default:
			return nil, false
		}
	}
	return nil, false
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
