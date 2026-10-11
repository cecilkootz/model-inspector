package scan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cecilkootz/model-inspector/internal/fixture"
	"github.com/cecilkootz/model-inspector/internal/model"
)

func codeCheck(t *testing.T, files []fixture.File, opt Options) Check {
	t.Helper()
	dir := t.TempDir()
	if err := fixture.Materialize(dir, files); err != nil {
		t.Fatal(err)
	}
	m, err := model.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return findCheck(t, RunOptions(m, opt), CheckCode)
}

// withFiles is the code-exec fixture without its config, .py and .bin.
func withFiles(t *testing.T, extra ...fixture.File) []fixture.File {
	t.Helper()
	base, err := fixture.CodeExec()
	if err != nil {
		t.Fatal(err)
	}
	var out []fixture.File
	for _, f := range base {
		if f.Name == "weights.bin" || f.Name == "modeling_demo.py" || f.Name == "config.json" {
			continue
		}
		out = append(out, f)
	}
	return append(out, extra...)
}

func cfg(target string) fixture.File {
	return fixture.File{Name: "config.json", Data: []byte(`{"model_type":"demo","auto_map":{"AutoModel":"` + target + `"}}`)}
}

func TestCodeContentSkipsWithNothingToRead(t *testing.T) {
	files, _, err := fixture.TinyQwen()
	if err != nil {
		t.Fatal(err)
	}
	c := codeCheck(t, files, Options{})
	if c.Status != StatusSkip || !strings.Contains(c.Summary, "nothing to read") {
		t.Fatalf("%+v", c)
	}
}

func TestCodeContentAutoMapRiskyPyFails(t *testing.T) {
	files, err := fixture.CodeScan()
	if err != nil {
		t.Fatal(err)
	}
	c := codeCheck(t, files, Options{})
	if c.Status != StatusFail {
		t.Fatalf("%+v", c)
	}
	if !strings.Contains(c.Summary, "pytorch_model.bin references os.system") ||
		!strings.Contains(c.Summary, "modeling_risky.py imports") {
		t.Errorf("summary %q", c.Summary)
	}
	if c.Details[0] != "needs trust_remote_code: yes (auto_map → modeling_risky.py)" {
		t.Errorf("verdict %q", c.Details[0])
	}
	blob := strings.Join(c.Details, "\n")
	for _, want := range []string{
		"pytorch_model.bin → references os.system (dangerous import)",
		"modeling_risky.py:3 → network: imports requests",
		"modeling_risky.py:10 → network: requests.post",
		"modeling_risky.py:11 → code-loading: exec",
	} {
		if !strings.Contains(blob, want) {
			t.Errorf("missing %q in\n%s", want, blob)
		}
	}
	if strings.Contains(blob, "optimizer.pt → imports outside") {
		t.Error("usual torch pickle flagged")
	}
}

func TestCodeContentUnreferencedPyWarns(t *testing.T) {
	files := withFiles(t, cfg("other.Model"), fixture.File{Name: "other.py", Data: fixture.CleanPy()},
		fixture.File{Name: "helper.py", Data: fixture.RiskyPy()})
	c := codeCheck(t, files, Options{})
	if c.Status != StatusWarn || !strings.HasPrefix(c.Summary, "helper.py imports requests") {
		t.Fatalf("%+v", c)
	}
	if c.Details[0] != "needs trust_remote_code: yes (auto_map → other.py)" {
		t.Errorf("verdict %q", c.Details[0])
	}
	strict := codeCheck(t, files, Options{Strict: true})
	if strict.Status != StatusWarn || strict.EffectiveStatus != StatusFail {
		t.Errorf("strict %+v", strict)
	}
}

func TestCodeContentCleanPasses(t *testing.T) {
	c := codeCheck(t, withFiles(t, cfg("clean.Model"), fixture.File{Name: "clean.py", Data: fixture.CleanPy()},
		fixture.File{Name: "a.pt", Data: fixture.TorchZip(fixture.PickleTorchP4())},
		fixture.File{Name: "b.pkl", Data: fixture.PickleTorchP4()}), Options{})
	if c.Status != StatusPass {
		t.Fatalf("%+v", c)
	}
	if !strings.Contains(c.Summary, "2 pickles reference only torch/numpy") {
		t.Errorf("summary %q", c.Summary)
	}
}

func TestCodeContentWarnCases(t *testing.T) {
	for name, f := range map[string]fixture.File{
		"truncated":   {Name: "t.pkl", Data: fixture.PickleTruncated()},
		"deflated":    {Name: "d.pt", Data: fixture.TorchZipDeflated(fixture.PickleTorchP4())},
		"compressed":  {Name: "m.bin.gz", Data: []byte{0x1f, 0x8b}},
		"unsupported": {Name: "m.h5", Data: []byte("x")},
		"outside":     {Name: "o.pkl", Data: append([]byte{0x80, 2}, []byte("ctransformers.trainer\nTrainer\n.")...)},
		"zipbomb":     {Name: "z.pt", Data: fixture.ZipClaimingEntries(65000)},
	} {
		c := codeCheck(t, withFiles(t, f), Options{})
		if c.Status != StatusWarn {
			t.Errorf("%s: %+v", name, c)
		}
	}
}

func TestCodeContentMissingAutoMapTargetWarns(t *testing.T) {
	c := codeCheck(t, withFiles(t, cfg("someorg/repo--modeling_x.Model")), Options{})
	if c.Status != StatusWarn || !strings.Contains(c.Details[0], "yes") {
		t.Fatalf("%+v", c)
	}
}

func TestCodeContentHostileNamesStayBounded(t *testing.T) {
	hostile := "x‮\u009b\x1b\xff" + strings.Repeat("q", 300)
	files := withFiles(t, cfg("m.Model"), fixture.File{Name: "m.py", Data: []byte("import requests, " + hostile + "\n")})
	c := codeCheck(t, files, Options{})
	if c.Status != StatusFail {
		t.Fatalf("%+v", c)
	}
	for _, d := range c.Details {
		if len(d) > 1024 {
			t.Errorf("detail %d bytes", len(d))
		}
	}
}

func TestCodeContentRespectsFileLimit(t *testing.T) {
	d := t.TempDir()
	files := withFiles(t, cfg("m.Model"), fixture.File{Name: "m.py", Data: fixture.CleanPy()})
	if err := fixture.Materialize(d, files); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxCodeFiles+5; i++ {
		name := "f" + strings.Repeat("a", i%5) + string(rune('a'+i%26)) + string(rune('a'+i/26)) + ".pkl"
		if err := os.WriteFile(filepath.Join(d, name), fixture.PickleTorchP4(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m, err := model.Load(d)
	if err != nil {
		t.Fatal(err)
	}
	c := findCheck(t, Run(m), CheckCode)
	if c.Status != StatusWarn || !strings.Contains(strings.Join(c.Details, "\n"), "more files not scanned") {
		t.Fatalf("%+v", c)
	}
}
