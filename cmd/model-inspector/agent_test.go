package main

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/cecilkootz/model-inspector/internal/fixture"
)

func agentDir(t *testing.T, build func() ([]fixture.File, error)) string {
	t.Helper()
	files, err := build()
	if err != nil {
		t.Fatal(err)
	}
	d := t.TempDir()
	if err := fixture.Materialize(d, files); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestAgentHostileOutputHasNoRawControls(t *testing.T) {
	d := agentDir(t, fixture.AgentHostile)
	out, _, code := rc(t, "inspect", "--path", d, "--json")
	if code != 0 && code != 1 {
		t.Fatalf("exit %d", code)
	}
	if !json.Valid([]byte(out)) || !utf8.ValidString(out) {
		t.Fatal("json is not valid UTF-8 JSON")
	}
	for _, r := range out {
		if (r < 0x20 && r != '\n') || (r >= 0x7f && r <= 0x9f) || unicode.Is(unicode.Cf, r) || r == 0x2028 || r == '<' {
			t.Fatalf("raw %U in json", r)
		}
	}
	if !strings.Contains(out, `"id": "agent-surface"`) {
		t.Fatal("no agent-surface check")
	}

	text, _, code := rc(t, "inspect", "--path", d)
	if code != 0 && code != 1 {
		t.Fatalf("exit %d", code)
	}
	for _, r := range text {
		if (r < 0x20 && r != '\n') || (r >= 0x7f && r <= 0x9f) || unicode.Is(unicode.Cf, r) || r == 0x2028 {
			t.Fatalf("raw %U in text", r)
		}
	}
}

func TestAgentTamperedExitCodes(t *testing.T) {
	d := agentDir(t, fixture.AgentTampered)
	out, _, code := rc(t, "inspect", "--path", d)
	if code != 0 {
		t.Fatalf("a warn-only check exited %d\n%s", code, out)
	}
	if _, _, code := rc(t, "inspect", "--path", d, "--strict"); code != 1 {
		t.Fatalf("--strict exited %d, want 1", code)
	}
}
