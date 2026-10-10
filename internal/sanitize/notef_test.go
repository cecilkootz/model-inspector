package sanitize

import (
	"errors"
	"go/ast"
	"go/parser"
	gotoken "go/token"
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
}

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
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !isNotef(call.Fun) {
				return true
			}
			pos := fset.Position(call.Pos())
			if len(call.Args) < 1 {
				t.Errorf("%s: Notef has no format string", pos)
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != gotoken.STRING {
				t.Errorf("%s: Notef format is not a string literal", pos)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func isNotef(fun ast.Expr) bool {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name == "Notef"
	case *ast.SelectorExpr:
		return f.Sel.Name == "Notef"
	default:
		return false
	}
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
