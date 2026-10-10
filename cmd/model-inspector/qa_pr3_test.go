package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"origin.cursor.com/cecilkootz/model-inspector/internal/fixture"
)

func goodShard(t *testing.T) []byte {
	t.Helper()
	b, _, err := fixture.WriteShard("", []fixture.Tensor{{Name: "model.embed_tokens.weight", DType: "F32", Shape: []int64{1}}})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func write(t *testing.T, p string, b []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func rc(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	return capture(t, func() int { return run(args) })
}

func TestP3_Targets(t *testing.T) {
	hub := t.TempDir()
	commit := strings.Repeat("a", 40)
	if _, err := fixture.MaterializeHub(hub, "org/name", commit, []fixture.File{{Name: "model.safetensors", Data: goodShard(t)}}); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.MaterializeHub(hub, "Org/Name", commit, []fixture.File{{Name: "model.safetensors", Data: goodShard(t)}}); err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	t.Chdir(cwd)
	write(t, filepath.Join(cwd, "org", "name", "model.safetensors"), goodShard(t))
	if err := os.MkdirAll(filepath.Join(cwd, "lnk"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(cwd, "org", "name"), filepath.Join(cwd, "lnk", "rep")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(cwd, "dir@x", "y", "model.safetensors"), goodShard(t))
	if err := os.MkdirAll(filepath.Join(cwd, "dang"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/nonexistent/zz", filepath.Join(cwd, "dang", "ling")); err != nil {
		t.Fatal(err)
	}

	// src is CWD, HUB, or empty when the command does not print a document.
	cases := []struct {
		args []string
		code int
		src  string
	}{
		{[]string{"inspect", "org/name"}, 2, ""},
		{[]string{"inspect", "org/name@main"}, 2, ""},
		{[]string{"inspect", "org/name@" + commit}, 2, ""},
		{[]string{"inspect", "./org/name"}, 0, "CWD"},
		{[]string{"inspect", "org/name/"}, 0, "CWD"},
		{[]string{"inspect", filepath.Join(cwd, "org/name")}, 0, "CWD"},
		{[]string{"inspect", "../" + filepath.Base(cwd) + "/org/name"}, 0, "CWD"},
		{[]string{"inspect", "lnk/rep"}, 2, ""},
		{[]string{"inspect", "Org/Name"}, 0, "HUB"},
		{[]string{"inspect", "ORG/NAME"}, 3, ""},
		{[]string{"inspect", "dang/ling"}, 2, ""},
		{[]string{"inspect", "dir@x/y"}, 0, "CWD"},
		{[]string{"inspect", "~/org/name"}, 3, ""},
		{[]string{"inspect", "~/x"}, 3, ""},
		{[]string{"inspect", ""}, 2, ""},
		{[]string{"inspect", "--repo", "../x"}, 2, ""},
		{[]string{"inspect", "--repo", "/etc"}, 2, ""},
		{[]string{"inspect", "--repo", "./org/name"}, 2, ""},
		{[]string{"inspect", "--repo", "org/name"}, 0, "HUB"},
		{[]string{"inspect", "--repo=org/name@../../x"}, 2, ""},
		{[]string{"inspect", "--path", "org/name"}, 0, "CWD"},
		{[]string{"inspect", "--path", "Org/Name"}, 3, ""},
		{[]string{"inspect", "--path", "org/name", "--repo", "org/name"}, 2, ""},
		{[]string{"inspect", "--path", "org/name", "org/name"}, 2, ""},
		{[]string{"inspect", "--path="}, 2, ""},
		{[]string{"inspect", "--path=", "org/name"}, 2, ""},
		{[]string{"inspect", "--repo", ""}, 2, ""},
		{[]string{"inspect", "--repo=", "org/name"}, 2, ""},
		{[]string{"inspect", "org\u2215name"}, 3, ""},
		{[]string{"inspect", "org\uff0fname"}, 3, ""},
		{[]string{"inspect", "оrg/name"}, 3, ""},
		{[]string{"inspect", "org/name", "extra"}, 2, ""},
		{[]string{"inspect", "org/na\x1bme"}, 3, ""},
	}
	for _, c := range cases {
		args := append(append([]string{}, c.args...), "--hub-dir", hub, "--json")
		out, errS, code := rc(t, args...)
		if code != c.code {
			t.Errorf("%q: exit %d, want %d\nstderr %q\nstdout %s", c.args, code, c.code, strings.TrimSpace(errS), out)
		}
		if strings.ContainsRune(errS, 0x1b) {
			t.Errorf("%q: raw ESC on stderr %q", c.args, errS)
		}
		src := sourceClass(out, hub, cwd)
		if c.src == "" {
			if src == "HUB" || src == "CWD" {
				t.Errorf("%q: resolved to %s, want no inspection", c.args, src)
			}
			continue
		}
		if src != c.src {
			t.Errorf("%q: src %s, want %s\n%s", c.args, src, c.src, out)
		}
	}
}

func sourceClass(out, hub, cwd string) string {
	var d map[string]any
	if json.Unmarshal([]byte(out), &d) != nil {
		return ""
	}
	s, _ := d["source"].(map[string]any)
	if s == nil {
		return ""
	}
	src, _ := s["path"].(string)
	switch {
	case strings.HasPrefix(src, hub):
		return "HUB"
	case strings.HasPrefix(src, cwd):
		return "CWD"
	default:
		return src
	}
}

func TestP3_ExitCodes(t *testing.T) {
	good := goodShard(t)
	mk := func(files map[string][]byte) string {
		t.Helper()
		d := t.TempDir()
		for n, b := range files {
			write(t, filepath.Join(d, n), b)
		}
		return d
	}
	unk := fixture.EncodeHeader([]byte(`{"w":{"dtype":"Q9","shape":[1],"data_offsets":[0,4]}}`), make([]byte, 4))
	bad := fixture.EncodeHeader([]byte(`{"w":{"dtype":"F32","shape":[1],"data_offsets":[0,999]}}`), make([]byte, 4))
	partial := mk(map[string][]byte{"a.safetensors": good})
	if err := os.Mkdir(filepath.Join(partial, "locked"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Join(partial, "locked"), 0o755) })

	cases := []struct {
		name    string
		dir     string
		args    []string
		code    int
		overall string
	}{
		{"pass", mk(map[string][]byte{"a.safetensors": good}), nil, 0, "pass"},
		{"warn-unknown-dtype", mk(map[string][]byte{"a.safetensors": unk}), nil, 0, "warn"},
		{"warn-unknown-dtype-strict", mk(map[string][]byte{"a.safetensors": unk}), []string{"--strict"}, 1, "fail"},
		{"warn-py", mk(map[string][]byte{"a.safetensors": good, "modeling.py": []byte("x")}), nil, 0, "warn"},
		{"warn-py-strict", mk(map[string][]byte{"a.safetensors": good, "modeling.py": []byte("x")}), []string{"--strict"}, 1, "fail"},
		{"fail", mk(map[string][]byte{"a.safetensors": bad}), nil, 1, "fail"},
		{"empty", t.TempDir(), nil, 3, "skip"},
		{"gguf", mk(map[string][]byte{"m.gguf": []byte("GGUF\x00junk")}), nil, 3, "skip"},
		{"gguf+py", mk(map[string][]byte{"m.gguf": []byte("GGUF\x00junk"), "x.py": nil}), nil, 3, "warn"},
		{"partial", partial, nil, 1, "fail"},
		{"only-index", mk(map[string][]byte{"model.safetensors.index.json": []byte(`{"weight_map":{"w":"a.safetensors"}}`)}), nil, 3, "fail"},
		{"only-config", mk(map[string][]byte{"config.json": []byte(`{}`)}), nil, 3, "skip"},
		{"missing", filepath.Join(t.TempDir(), "nope"), nil, 3, ""},
		{"file-not-dir", "", nil, 3, ""},
	}
	fileDir := mk(map[string][]byte{"f": nil})
	cases[len(cases)-1].dir = filepath.Join(fileDir, "f")

	var failDir string
	for _, c := range cases {
		if c.name == "fail" {
			failDir = c.dir
		}
		args := []string{"inspect", "--path", c.dir, "--json"}
		args = append(args, c.args...)
		out, errS, code := rc(t, args...)
		if code != c.code {
			t.Errorf("%s: exit %d, want %d stderr %q", c.name, code, c.code, strings.TrimSpace(errS))
		}
		if c.overall == "" {
			continue
		}
		var d struct {
			Scan struct {
				Overall string
				Checks  []struct {
					ID, Status, Effective string
				} `json:"checks"`
			} `json:"scan"`
		}
		// effective_status needs the right field name; re-decode below when strict.
		if err := json.Unmarshal([]byte(out), &d); err != nil {
			t.Errorf("%s: json: %v\n%s", c.name, err, out)
			continue
		}
		if d.Scan.Overall != c.overall {
			t.Errorf("%s: overall %q, want %q", c.name, d.Scan.Overall, c.overall)
		}
		if c.name == "warn-unknown-dtype-strict" || c.name == "warn-py-strict" {
			var raw struct {
				Scan struct {
					Checks []struct {
						ID              string
						Status          string
						EffectiveStatus string `json:"effective_status"`
					}
				}
			}
			if err := json.Unmarshal([]byte(out), &raw); err != nil {
				t.Fatal(err)
			}
			var saw bool
			for _, ch := range raw.Scan.Checks {
				if ch.Status == "warn" && ch.EffectiveStatus == "fail" {
					saw = true
				}
			}
			if !saw {
				t.Errorf("%s: wanted a raw warn promoted to effective fail\n%s", c.name, out)
			}
		}
	}
	txt, _, code := rc(t, "inspect", "--path", partial)
	if code != 1 {
		t.Errorf("partial text exit %d, want 1", code)
	}
	assertNoC1Text(t, txt)

	_, _, code = rc(t, "--path", failDir)
	if code != 1 {
		t.Errorf("non-tty default command on a failing dir exited %d, want 1", code)
	}
}

func TestP3_CodeExec(t *testing.T) {
	good := goodShard(t)
	d := t.TempDir()
	write(t, filepath.Join(d, "a.safetensors"), good)
	write(t, filepath.Join(d, "pytorch_model.BIN"), []byte("\x80\x02pickle-not-read"))
	write(t, filepath.Join(d, "Modeling.Py"), []byte("import os"))
	write(t, filepath.Join(d, "a/b/c/d/e/deep.pkl"), nil)
	write(t, filepath.Join(d, "x\x1b]0;PWN\x07.pt"), nil)
	write(t, filepath.Join(d, "e\u202egnp.py"), nil)
	outside := filepath.Join(t.TempDir(), "evil.py")
	write(t, outside, []byte("x"))
	if err := os.Symlink(outside, filepath.Join(d, "link.py")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/nonexistent.ckpt", filepath.Join(d, "dangling.ckpt")); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"weights.h5", "m.keras", "m.nemo", "m.dill", "m.bin.gz", "m.pt.zip", "m.onnx", "m.msgpack", "m.mar", "m.tar", "m.pth.tar", "m.ckpt.zip", "m.tar.gz", "m.tgz", "m.pth.zip", "m.bin.zip"} {
		write(t, filepath.Join(d, n), nil)
	}
	out, _, code := rc(t, "inspect", "--path", d, "--json")
	if code != 0 && code != 1 {
		t.Fatalf("exit %d", code)
	}
	details, status, summary := codeExec(t, out)
	if status != "warn" {
		t.Fatalf("code-exec status %s summary %q details %q", status, summary, details)
	}
	if strings.Contains(strings.ToLower(summary), "name-only") || strings.Contains(strings.ToLower(summary), "names only") {
		t.Fatalf("summary still says name-only: %q", summary)
	}
	blob := strings.Join(details, "\n")
	for _, want := range []string{
		"pytorch_model.BIN", "Modeling.Py", "deep.pkl", "link.py", "dangling.ckpt",
		"weights.h5", "m.keras", "m.nemo", "m.dill", "m.bin.gz", "m.mar",
		"m.pt.zip", "m.tar", "m.pth.tar", "m.ckpt.zip", "m.tar.gz", "m.tgz", "m.pth.zip", "m.bin.zip",
	} {
		if !strings.Contains(blob, want) {
			t.Errorf("code-exec missing %s\n%s", want, blob)
		}
	}
	for _, absent := range []string{"m.onnx", "m.msgpack"} {
		if strings.Contains(blob, absent) {
			t.Errorf("code-exec unexpectedly flagged %s", absent)
		}
	}
	txt, _, _ := rc(t, "inspect", "--path", d)
	assertNoC1Text(t, txt)

	for name, cfg := range map[string]string{
		"plain":       `{"auto_map":{"AutoTokenizer":"other/repo--tok.Tok"}}`,
		"dupkey":      `{"auto_map":{"AutoTokenizer":"other/repo--tok.Tok"},"x":1,"x":2}`,
		"nan":         `{"auto_map":{"AutoTokenizer":"other/repo--tok.Tok"},"x":NaN}`,
		"deep":        `{"auto_map":{"AutoTokenizer":"other/repo--tok.Tok"},"x":` + strings.Repeat("[", 40) + strings.Repeat("]", 40) + `}`,
		"bom":         "\ufeff" + `{"auto_map":{"AutoTokenizer":"other/repo--tok.Tok"}}`,
		"list":        `{"auto_map":{"AutoTokenizer":["other/repo--tok.Tok", null]}}`,
		"nested-only": `{"x":{"auto_map":{"AutoModel":"m.M"}}}`,
	} {
		unparsed := name == "dupkey" || name == "nan" || name == "deep" || name == "bom"
		for _, file := range []string{"tokenizer_config.json", "config.json"} {
			dd := t.TempDir()
			write(t, filepath.Join(dd, "a.safetensors"), good)
			write(t, filepath.Join(dd, file), []byte(cfg))
			out, _, code := rc(t, "inspect", "--path", dd, "--json")
			var doc struct {
				Scan struct {
					Overall string
					Checks  []struct {
						ID, Status, Summary string
						Details             []string
					}
				}
				LoadWarnings []string `json:"load_warnings"`
			}
			if err := json.Unmarshal([]byte(out), &doc); err != nil {
				t.Fatalf("%s %s: %v\n%s", name, file, err, out)
			}
			st, sum, det := "", "", ""
			for _, c := range doc.Scan.Checks {
				if c.ID == "code-exec-formats" {
					st, sum, det = c.Status, c.Summary, strings.Join(c.Details, "\n")
				}
			}
			phrase := "could not check auto_map in " + file
			if unparsed {
				if st != "warn" || strings.Contains(sum, "none") || !strings.Contains(det, phrase) {
					t.Errorf("%s in %s: status %q summary %q details %q", name, file, st, sum, det)
				}
				if len(doc.LoadWarnings) == 0 {
					t.Errorf("%s in %s: no load warning", name, file)
				}
				if doc.Scan.Overall == "pass" || code == 0 {
					t.Errorf("%s in %s: overall %s exit %d, want incomplete", name, file, doc.Scan.Overall, code)
				}
			} else if name == "nested-only" {
				if st != "pass" || !strings.Contains(sum, "none") {
					t.Errorf("nested-only %s: status %q summary %q", file, st, sum)
				}
				if doc.Scan.Overall != "pass" || code != 0 {
					t.Errorf("nested-only %s: overall %s exit %d", file, doc.Scan.Overall, code)
				}
			} else {
				if st != "warn" || strings.Contains(sum, "none") {
					t.Errorf("%s in %s: status %q summary %q", name, file, st, sum)
				}
				if !strings.Contains(det, "auto_map") {
					t.Errorf("%s in %s: details %q", name, file, det)
				}
				if code != 0 || doc.Scan.Overall == "fail" {
					t.Errorf("%s in %s: overall %s exit %d", name, file, doc.Scan.Overall, code)
				}
			}
		}
	}

	dd := t.TempDir()
	write(t, filepath.Join(dd, "a.safetensors"), good)
	write(t, filepath.Join(dd, "config.json"), []byte(`{"auto_map":{"AutoTokenizer":"t.T"},"x":1,"x":2}`))
	out, _, code = rc(t, "inspect", "--path", dd, "--json", "--strict")
	if code != 1 {
		t.Fatalf("strict unparseable config exit %d", code)
	}
	var strict struct {
		Scan struct {
			Checks []struct {
				ID              string
				Status          string
				EffectiveStatus string `json:"effective_status"`
				Details         []string
			}
		}
	}
	if err := json.Unmarshal([]byte(out), &strict); err != nil {
		t.Fatal(err)
	}
	for _, c := range strict.Scan.Checks {
		if c.ID != "code-exec-formats" {
			continue
		}
		if c.Status != "warn" || c.EffectiveStatus != "fail" {
			t.Fatalf("strict code-exec status %s effective %s", c.Status, c.EffectiveStatus)
		}
		if !strings.Contains(strings.Join(c.Details, "\n"), "could not check auto_map in config.json") {
			t.Fatalf("details %q", c.Details)
		}
	}
}

func codeExec(t *testing.T, out string) (details []string, status, summary string) {
	t.Helper()
	var doc struct {
		Scan struct {
			Checks []struct {
				ID, Status, Summary string
				Details             []string
			}
		}
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	for _, c := range doc.Scan.Checks {
		if c.ID == "code-exec-formats" {
			return c.Details, c.Status, c.Summary
		}
	}
	t.Fatal("no code-exec check")
	return nil, "", ""
}

func TestP3_JSON(t *testing.T) {
	hostile := "h\u009b\u202e\u2066\u200b\U000E0041\U0001D173\u2028<x>\u00ad\x1b\"\\\xff"
	d := t.TempDir()
	hb, err := json.Marshal(map[string]any{
		"__metadata__": map[string]string{"format": hostile},
		"model.layers.0.self_attn.q_proj.weight" + hostile: map[string]any{"dtype": "F32", "shape": []int{1}, "data_offsets": []int{0, 4}},
	})
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(d, "m"+strings.ReplaceAll(hostile, "/", "")+".safetensors"), fixture.EncodeHeader(hb, make([]byte, 4)))
	cfg, err := json.Marshal(map[string]any{
		"model_type": hostile, "architectures": []string{hostile}, "layer_types": []string{hostile},
		"hidden_size": 8, "quantization": map[string]any{"bits": 4, "group_size": 64, "mode": hostile},
		"auto_map": map[string]any{"AutoModel" + hostile: hostile},
	})
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(d, "config.json"), cfg)
	write(t, filepath.Join(d, "model.safetensors.index.json"), []byte(`{"weight_map":{"zz`+"\u202e"+`":"q`+"\u009b"+`.safetensors"}}`))
	write(t, filepath.Join(d, "x"+hostile+".py"), nil)
	out, _, code := rc(t, "inspect", "--path", d, "--json")
	if code != 0 && code != 1 {
		t.Fatalf("exit %d", code)
	}
	if !json.Valid([]byte(out)) {
		t.Fatalf("invalid json")
	}
	if !utf8.ValidString(out) {
		t.Fatal("json not utf8")
	}
	for _, r := range out {
		if (r < 0x20 && r != '\n') || (r >= 0x7f && r <= 0x9f) || unicode.Is(unicode.Cf, r) || r == 0x2028 || r == '<' {
			t.Fatalf("raw %U in json", r)
		}
	}
	var v any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatal(err)
	}
	n := 0
	want := strings.ToValidUTF8(hostile, "\uFFFD")
	var walk func(any)
	walk = func(x any) {
		switch y := x.(type) {
		case string:
			if strings.Contains(y, want) {
				n++
			}
		case map[string]any:
			for k, vv := range y {
				walk(k)
				walk(vv)
			}
		case []any:
			for _, vv := range y {
				walk(vv)
			}
		}
	}
	walk(v)
	if n == 0 {
		t.Fatal("hostile text did not round-trip through JSON")
	}
	txt, _, _ := rc(t, "inspect", "--path", d)
	assertNoC1Text(t, txt)
}

func TestP3_TextWrapSplitsRunes(t *testing.T) {
	d := t.TempDir()
	write(t, filepath.Join(d, "a.safetensors"), goodShard(t))
	for i := 0; i < 3; i++ {
		n := filepath.Join(d, strings.Repeat("Û", 30+i)+"é")
		if err := os.Mkdir(n, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(n, 0o755) })
		// A broken symlink with the same kind of name is a load warning
		// even when the process can still read a mode-0 directory.
		if err := os.Symlink("/nonexistent/wrap", n+"-link"); err != nil {
			t.Fatal(err)
		}
	}
	txt, _, code := rc(t, "inspect", "--path", d)
	if code != 1 && code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(txt, "Û") {
		t.Fatalf("wrapped warning did not include the multi-byte name:\n%s", txt)
	}
	assertNoC1Text(t, txt)
}

func assertNoC1Text(t *testing.T, txt string) {
	t.Helper()
	if !utf8.ValidString(txt) {
		t.Fatal("text is not valid UTF-8")
	}
	for i, line := range strings.Split(txt, "\n") {
		if !utf8.ValidString(line) {
			t.Errorf("line %d invalid UTF-8: %q", i, line)
			continue
		}
		for j := 0; j < len(line); {
			r, size := utf8.DecodeRuneInString(line[j:])
			// A lone 0x80-0x9F byte is invalid UTF-8 (size 1, RuneError).
			// Û is C3 9B: the 0x9B is a continuation, not a C1 control.
			if size == 1 && line[j] >= 0x80 {
				t.Errorf("line %d stray C1 byte %#x: %q", i, line[j], line)
				break
			}
			if (r < 0x20 && r != '\n') || (r >= 0x7f && r <= 0x9f) || unicode.Is(unicode.Cf, r) {
				t.Errorf("line %d raw %U", i, r)
				break
			}
			if size < 1 {
				t.Errorf("line %d stuck at %d", i, j)
				break
			}
			j += size
		}
	}
}
