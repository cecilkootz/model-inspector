package codescan

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cecilkootz/model-inspector/internal/fixture"
	"github.com/cecilkootz/model-inspector/internal/sanitize"
)

func find(fs []Finding, cat, name string) *Finding {
	for i := range fs {
		if fs[i].Cat == cat && fs[i].Name == name {
			return &fs[i]
		}
	}
	return nil
}

func TestPythonRisky(t *testing.T) {
	r := ScanPython(fixture.RiskyPy(), false)
	for _, w := range []struct{ cat, name string }{
		{CatNetwork, "requests"}, {CatNetwork, "requests.post"}, {CatProcess, "subprocess"},
		{CatEnv, "os.environ"}, {CatLoading, "exec"}, {CatURL, "https://example.invalid/collect"},
	} {
		if find(r.Findings, w.cat, w.name) == nil {
			t.Errorf("missing %s %s in %+v", w.cat, w.name, r.Findings)
		}
	}
	if f := find(r.Findings, CatNetwork, "requests"); f == nil || !f.Import || f.Line != 3 {
		t.Errorf("requests import: %+v", f)
	}
	if len(r.Incomplete) != 0 {
		t.Errorf("incomplete %v", r.Incomplete)
	}
}

func TestPythonClean(t *testing.T) {
	r := ScanPython(fixture.CleanPy(), false)
	for _, f := range r.Findings {
		if f.Class != ClassInfo || f.Cat != CatImport {
			t.Errorf("unexpected %+v", f)
		}
	}
}

func TestPythonSkipsCommentsDocstringsAndMethods(t *testing.T) {
	src := "# requests is nice\n\"\"\"\nuse requests and eval(x)\n\"\"\"\nmodel.eval()\nre.compile('x')\n"
	for _, f := range ScanPython([]byte(src), false).Findings {
		if f.Class != ClassInfo {
			t.Errorf("false positive %+v", f)
		}
	}
	r := ScanPython([]byte("x = 1; eval(y)\nif a: import os; os.system('x')\n"), false)
	if find(r.Findings, CatLoading, "eval") == nil {
		t.Errorf("eval after ; missed: %+v", r.Findings)
	}
}

func TestPythonImportForms(t *testing.T) {
	src := "import a.b as c, socket\nfrom os import system, path\nfrom . import x\ndef f():\n    from http.client import HTTPConnection\n"
	r := ScanPython([]byte(src), false)
	for _, w := range []struct{ cat, name string }{
		{CatImport, "a.b"}, {CatNetwork, "socket"}, {CatProcess, "os.system"}, {CatNetwork, "http.client"},
	} {
		if find(r.Findings, w.cat, w.name) == nil {
			t.Errorf("missing %s %s in %+v", w.cat, w.name, r.Findings)
		}
	}
}

func TestPythonCaps(t *testing.T) {
	long := "x = '" + strings.Repeat("a", MaxPyLine+1) + "'\nimport requests\n"
	r := ScanPython([]byte(long), false)
	if find(r.Findings, CatMeta, "long line, not scanned") == nil || find(r.Findings, CatNetwork, "requests") == nil {
		t.Errorf("long line: %+v", r.Findings)
	}
	r = ScanPython([]byte(strings.Repeat("\n", MaxPyLines+5)), false)
	if len(r.Incomplete) == 0 || r.Lines != MaxPyLines {
		t.Errorf("line cap: %d %v", r.Lines, r.Incomplete)
	}
	var b strings.Builder
	for i := 0; i < 400; i++ {
		b.WriteString("import m")
		b.WriteString(strings.Repeat("x", i%7))
		b.WriteString(string(rune('a' + i%26)))
		b.WriteString(strings.Repeat("y", i/26))
		b.WriteString("\n")
	}
	r = ScanPython([]byte(b.String()), false)
	if len(r.Findings) != MaxFindings || len(r.Incomplete) == 0 {
		t.Errorf("finding cap: %d %v", len(r.Findings), r.Incomplete)
	}
}

func TestPythonHostileNamesAreBoundedAndSanitizable(t *testing.T) {
	hostile := "evil‮\u009b\x1b\xff" + strings.Repeat("z", 500)
	src := []byte("import " + hostile + "\nimport requests\x00\n")
	r := ScanPython(src, false)
	if find(r.Findings, CatMeta, "NUL bytes in source") == nil {
		t.Error("NUL not reported")
	}
	for _, f := range r.Findings {
		if len(f.Name) > maxName {
			t.Errorf("name %d bytes", len(f.Name))
		}
		out := sanitize.From(f.Name).String()
		for _, c := range out {
			if c < 0x20 || (c >= 0x7f && c <= 0x9f) || c == 0x202e {
				t.Errorf("raw %U after sanitize", c)
			}
		}
	}
}

func TestPickleOSSystem(t *testing.T) {
	r, err := WalkPickle(fixture.PickleOSSystem())
	if err != nil || r.Truncated {
		t.Fatalf("%v %+v", err, r)
	}
	if len(r.Globals) != 1 || r.Globals[0].Ref() != "os.system" || r.Globals[0].Class != ClassDanger {
		t.Errorf("globals %+v", r.Globals)
	}
	if r.Ops.Reduce != 1 || r.Pickles != 1 {
		t.Errorf("ops %+v", r)
	}
}

func TestPickleStackGlobalTorch(t *testing.T) {
	r, err := WalkPickle(fixture.PickleTorchP4())
	if err != nil || r.Truncated {
		t.Fatalf("%v %+v", err, r)
	}
	got := map[string]Class{}
	for _, g := range r.Globals {
		got[g.Ref()] = g.Class
	}
	want := map[string]Class{"torch._utils._rebuild_tensor_v2": ClassInfo, "collections.OrderedDict": ClassInfo}
	if len(got) != 2 || got["collections.OrderedDict"] != ClassInfo || got["torch._utils._rebuild_tensor_v2"] != ClassInfo {
		t.Errorf("got %v want %v", got, want)
	}
	if r.Ops.StackGlobal != 2 {
		t.Errorf("ops %+v", r.Ops)
	}
}

func TestPickleMemoResolvesStackGlobal(t *testing.T) {
	// "os" and "system" are memoized, then fetched with BINGET.
	b := []byte{0x80, 4, 0x8c, 2, 'o', 's', 0x94, 0x8c, 6, 's', 'y', 's', 't', 'e', 'm', 0x94, 'h', 0, 'h', 1, 0x93, '.'}
	r, err := WalkPickle(b)
	if err != nil || len(r.Globals) != 1 || r.Globals[0].Ref() != "os.system" || r.Globals[0].Class != ClassDanger {
		t.Fatalf("%v %+v", err, r)
	}
}

func TestPickleDottedNameBypass(t *testing.T) {
	if ClassifyGlobal("torch", "os.system") != ClassDanger {
		t.Error("dotted name not caught")
	}
	if ClassifyGlobal("transformers", "TrainingArguments") != ClassRisk {
		t.Error("outside set not risk")
	}
	if ClassifyGlobal("_codecs", "encode") != ClassInfo || ClassifyGlobal("_codecs", "decode") != ClassRisk {
		t.Error("_codecs")
	}
}

func TestPickleTruncatedAndMalformed(t *testing.T) {
	r, err := WalkPickle(fixture.PickleTruncated())
	if err != nil || !r.Truncated {
		t.Fatalf("truncated: %v %+v", err, r)
	}
	if _, err := WalkPickle([]byte{0x80, 2, 0xff}); err == nil {
		t.Error("unknown opcode accepted")
	}
	if _, err := WalkPickle([]byte{0x80, 9}); err == nil {
		t.Error("bad protocol accepted")
	}
	r, err = WalkPickle([]byte{'X', 0xff, 0xff, 0xff, 0xff})
	if err != nil || !r.Truncated {
		t.Errorf("huge length: %v %+v", err, r)
	}
	r, err = WalkPickle([]byte("c" + strings.Repeat("a", 600) + "\n"))
	if err == nil {
		t.Errorf("long GLOBAL line accepted: %+v", r)
	}
}

func TestPickleOpcodeCap(t *testing.T) {
	r, err := WalkPickle(bytes.Repeat([]byte{'N'}, MaxOpcodes+10))
	if err != nil || !r.Truncated || r.Opcodes != MaxOpcodes {
		t.Fatalf("%v %d %v", err, r.Opcodes, r.Truncated)
	}
}

func TestPickleLegacyMultiple(t *testing.T) {
	a := []byte{0x80, 2, 'K', 1, '.'}
	b := append(append([]byte{}, a...), fixture.PickleOSSystem()...)
	r, err := WalkPickle(b)
	if err != nil || r.Pickles != 2 || len(r.Globals) != 1 {
		t.Fatalf("%v %+v", err, r)
	}
}

func write(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestScanFileKinds(t *testing.T) {
	d := t.TempDir()
	cases := []struct {
		name string
		data []byte
		want string
		kind Kind
	}{
		{"m.py", fixture.RiskyPy(), "", KindPython},
		{"a.bin", fixture.PickleOSSystem(), "", KindPickle},
		{"a.pt", fixture.TorchZip(fixture.PickleTorchP4()), "", KindPickle},
		{"a.pth", fixture.TorchZipDeflated(fixture.PickleTorchP4()), "compressed; not scanned", KindPickle},
		{"a.pkl", fixture.PickleTruncated(), "truncated", KindPickle},
		{"a.bin.gz", nil, "not scanned: compressed", KindCompressed},
		{"a.h5", nil, "not scanned: format not supported", KindUnsupported},
		{"w.bin", []byte("not-a-pickle\n"), "unknown opcode", KindPickle},
		{"g.joblib", []byte{0x1f, 0x8b, 8, 0}, "not scanned: compressed", KindPickle},
		{"big.pt", fixture.ZipClaimingEntries(60000), "zip claims", KindPickle},
		{"z64.pt", fixture.ZipClaimingEntries(0xFFFF), "zip64", KindPickle},
	}
	for _, c := range cases {
		p := write(t, d, c.name, c.data)
		r := ScanFile(p, c.name, d)
		if r.Kind != c.kind {
			t.Errorf("%s kind %s", c.name, r.Kind)
		}
		if c.want == "" && len(r.Incomplete) != 0 {
			t.Errorf("%s incomplete %v", c.name, r.Incomplete)
		}
		if c.want != "" && !strings.Contains(strings.Join(r.Incomplete, "|"), c.want) {
			t.Errorf("%s incomplete %v want %q", c.name, r.Incomplete, c.want)
		}
	}
	r := ScanFile(filepath.Join(d, "a.pt"), "a.pt", d)
	if !r.Zip || r.Pickle == nil || len(r.Pickle.Globals) != 2 {
		t.Errorf("torch zip %+v", r)
	}
	if r := ScanFile(filepath.Join(d, "a.pth"), "a.pth", d); r.Pickle != nil {
		t.Error("deflated entry was walked")
	}
	if r := ScanFile(filepath.Join(d, "missing.py"), "missing.py", d); len(r.Incomplete) == 0 {
		t.Error("missing file not incomplete")
	}
}

func TestScanFileReadsOnlyCap(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "huge.bin")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(fixture.PickleOSSystem()[:len(fixture.PickleOSSystem())-1]); err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(100 << 20); err != nil {
		t.Fatal(err)
	}
	f.Close()
	r := ScanFile(p, "huge.bin", d)
	if r.Bytes > MaxPickleBytes+1 {
		t.Fatalf("read %d bytes", r.Bytes)
	}
	if r.Pickle == nil || len(r.Pickle.Globals) != 1 {
		t.Errorf("%+v", r.Pickle)
	}

	py := filepath.Join(d, "huge.py")
	f, err = os.Create(py)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("import requests\n")
	f.Truncate(100 << 20)
	f.Close()
	r = ScanFile(py, "huge.py", d)
	if r.Bytes > MaxPyBytes || !strings.Contains(strings.Join(r.Incomplete, "|"), "1 MiB") {
		t.Errorf("py read %d %v", r.Bytes, r.Incomplete)
	}
}

func FuzzPickleWalk(f *testing.F) {
	for _, s := range [][]byte{
		fixture.PickleOSSystem(), fixture.PickleTorchP4(), fixture.PickleTruncated(),
		{0x80, 2, 'c', 'o', 's', '\n', 's', 'y', 's', 't', 'e', 'm', '\n', '.'},
		{0x80, 4, 0x8d, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
		{'(', 'i', 'a', '\n', 'b', '\n', 'o', '.'},
		{'j', 0xff, 0xff, 0xff, 0xff, 'g', '9', '\n', 0x93, '.'},
		{},
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		r, _ := WalkPickle(b)
		if r.Opcodes > len(b) || len(r.Globals) > MaxGlobals {
			t.Fatalf("bounds: %d opcodes for %d bytes, %d globals", r.Opcodes, len(b), len(r.Globals))
		}
	})
}

func FuzzPyScan(f *testing.F) {
	for _, s := range []string{
		string(fixture.RiskyPy()), string(fixture.CleanPy()), "import a;import b\nfrom x import (y,\n z)\n",
		"'''\nimport os\n'''\nos.system('x')", "x = \"#\" + eval(\"1\")\r\n", "import \xff\x00‮",
		"from import\nimport\nfrom .\n", strings.Repeat("a", MaxPyLine+1),
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		r := ScanPython(b, false)
		if len(r.Findings) > MaxFindings {
			t.Fatalf("%d findings", len(r.Findings))
		}
		for _, x := range r.Findings {
			if len(x.Name) > maxName {
				t.Fatalf("name %d bytes", len(x.Name))
			}
		}
	})
}
