package report

import (
	"strings"
	"testing"

	"origin.cursor.com/cecilkootz/model-inspector/internal/fixture"
	"origin.cursor.com/cecilkootz/model-inspector/internal/model"
	"origin.cursor.com/cecilkootz/model-inspector/internal/scan"
)

func TestTextSummary(t *testing.T) {
	files, _, err := fixture.TinyQwen()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := fixture.Materialize(dir, files); err != nil {
		t.Fatal(err)
	}
	m, err := model.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := Text(Build(m, scan.Run(m), false, 0))
	if strings.Contains(out, "\x1b") {
		t.Fatalf("text contains ESC:\n%s", out)
	}
	if strings.Contains(out, " @ ") {
		t.Fatalf("empty revision rendered with @:\n%s", out)
	}
	for _, want := range []string{"safetensors", "revision", "—", "checks", "behavior scan"} {
		if !strings.Contains(out, want) {
			t.Errorf("text missing %q\n%s", want, out)
		}
	}
}
