package main

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/cecilkootz/model-inspector/internal/fixture"
)

func TestCodeContentInspectFailsAndExplains(t *testing.T) {
	files, err := fixture.CodeScan()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := fixture.Materialize(dir, files); err != nil {
		t.Fatal(err)
	}
	out, _, code := rc(t, "inspect", "--path", dir, "--json")
	if code != 1 {
		t.Fatalf("exit %d", code)
	}
	var doc struct {
		Scan struct {
			Checks []struct {
				ID, Status, Summary string
				Details             []string
			}
		}
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	for _, c := range doc.Scan.Checks {
		if c.ID != "code-content" {
			continue
		}
		if c.Status != "fail" || !strings.Contains(c.Summary, "pytorch_model.bin references os.system") {
			t.Fatalf("%+v", c)
		}
		if c.Details[0] != "needs trust_remote_code: yes (auto_map → modeling_risky.py)" {
			t.Fatalf("verdict %q", c.Details[0])
		}
		return
	}
	t.Fatal("no code-content check")
}

func TestCodeContentJSONEscapesHostileNames(t *testing.T) {
	hostile := "h\u009b‮⁦​\x1b\"\\<x>"
	d := t.TempDir()
	write(t, d+"/model.safetensors", goodShard(t))
	write(t, d+"/config.json", []byte(`{"auto_map":{"AutoModel":"m.M"}}`))
	write(t, d+"/m.py", []byte("import requests, a"+hostile+"\nimport os\nos.system('x"+hostile+"')\x00\nx = 'http://h"+hostile+"/p'\n"))
	pick := append([]byte{0x80, 2, 'c'}, []byte("mod"+hostile+"\nname"+hostile+"\n")...)
	pick = append(pick, 'R', '.')
	write(t, d+"/w.bin", pick)
	out, _, code := rc(t, "inspect", "--path", d, "--json")
	if code != 1 {
		t.Fatalf("exit %d", code)
	}
	if !json.Valid([]byte(out)) || !utf8.ValidString(out) {
		t.Fatal("invalid json")
	}
	for _, r := range out {
		if (r < 0x20 && r != '\n') || (r >= 0x7f && r <= 0x9f) || unicode.Is(unicode.Cf, r) || r == 0x2028 || r == '<' {
			t.Fatalf("raw %U in json", r)
		}
	}
	text, _, _ := rc(t, "inspect", "--path", d)
	for _, r := range text {
		if (r < 0x20 && r != '\n' && r != '\t') || (r >= 0x7f && r <= 0x9f) || r == 0x202e || r == 0x1b {
			t.Fatalf("raw %U in text", r)
		}
	}
}
