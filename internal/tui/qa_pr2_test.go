package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"origin.cursor.com/cecilkootz/model-inspector/internal/fixture"
	"origin.cursor.com/cecilkootz/model-inspector/internal/model"
	"origin.cursor.com/cecilkootz/model-inspector/internal/sanitize"
	"origin.cursor.com/cecilkootz/model-inspector/internal/scan"
)

func TestQA_HostileNamesView(t *testing.T) {
	d := t.TempDir()
	evilName := "e\u009b31m\u202egnp.\u2066x\u2028y\u200bz\x1b]0;T\x07.safetensors"
	hdr := `{"__metadata__":{"format":"\u009b2J\u2067"},"model.layers.0.self_attn.q_proj.weight\u001b[31m":{"dtype":"F\u001b32","shape":[1],"data_offsets":[0,4]}}`
	if err := os.WriteFile(filepath.Join(d, evilName), fixture.EncodeHeader([]byte(hdr), []byte{0, 0, 0, 0}), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "bad\x1b]8;;x\x07.safetensors"), fixture.EncodeHeader([]byte(`{"x":{"dtype":"F32","shape":[1],"data_offsets":[0,999]}}`), []byte{1, 2, 3, 4}), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "c\x1b[2J.safetensors.index.json"), []byte(`{"weight_map":{"k\u001b]52;c;x\u0007":"m\u009b.safetensors"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Unparseable raw fallback: ESC, C1, bidi, and invalid UTF-8.
	if err := os.WriteFile(filepath.Join(d, "config.json"), []byte("{\"model_type\":\"\x1b[2J\xc2\x9b\xe2\x80\xae\xff\xfe"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := model.Load(d)
	if err != nil {
		t.Fatal(err)
	}
	m.RepoID = "org/\u202ename"
	m.Revision = "\u009b]0;x"
	vs := qaViews(t, m)
	assertViewRunes(t, "hostile", vs)
	all := strings.Join(vs, "\n")
	for _, want := range []string{"<U+009B>", "<U+202E>", "<U+2066>", "<U+2067>", "<U+2028>", "<U+200B>", "<U+001B>", "config.json did not parse.", "Not a malware or behavior scan"} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %s", want)
		}
	}
	// Enter on a code card opens the code list, and the next enter opens the
	// disclosure card, so the last frame is that card. The scan screen itself
	// still carries the disclaimer.
	var scanFrame string
	for _, v := range vs {
		if strings.Contains(v, "Not a malware or behavior scan") && strings.Contains(v, "CHECKS") {
			scanFrame = v
			break
		}
	}
	if scanFrame == "" {
		t.Fatalf("scan tab missing the disclaimer:\n%s", vs[len(vs)-1])
	}

	d2 := t.TempDir()
	cfg := `{"model_type":"\u009b\u202e","architectures":["\u001b]0;x\u0007"],"text_config":{"hidden_size":"\u2066"},"layer_types":["\u202e\u001b"]}`
	if err := os.WriteFile(filepath.Join(d2, "config.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	m2, err := model.Load(d2)
	if err != nil {
		t.Fatal(err)
	}
	if m2.Config == nil {
		t.Fatalf("config did not parse: %v", m2.Warnings)
	}
	assertViewRunes(t, "config", qaViews(t, m2))
}

func TestQA_BidiBeyondOverride(t *testing.T) {
	d := t.TempDir()
	hdr := `{"__metadata__":{"format":"\u2066\u2067\u2068\u2069\u200e\u200f\u061c"},"w":{"dtype":"F32","shape":[1],"data_offsets":[0,4]}}`
	if err := os.WriteFile(filepath.Join(d, "model.safetensors"), fixture.EncodeHeader([]byte(hdr), []byte{0, 0, 0, 0}), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := model.Load(d)
	if err != nil {
		t.Fatal(err)
	}
	vs := qaViews(t, m)
	assertViewRunes(t, "bidi", vs)
	all := strings.Join(vs, "\n")
	for _, tok := range []string{"<U+2066>", "<U+2067>", "<U+2068>", "<U+2069>", "<U+200E>", "<U+200F>", "<U+061C>"} {
		if !strings.Contains(all, tok) {
			t.Errorf("missing %s", tok)
		}
	}
}

func qaViews(t *testing.T, m *model.Model) []string {
	t.Helper()
	ui := New(m, scan.Run(m))
	var x tea.Model = ui
	x, _ = x.Update(tea.WindowSizeMsg{Width: 250, Height: 80})
	vs := []string{x.View()}
	for _, k := range []string{"2", "3", "4"} {
		x, _ = x.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
		vs = append(vs, x.View())
		if k == "3" {
			x, _ = x.Update(tea.KeyMsg{Type: tea.KeyEnter})
			vs = append(vs, x.View())
			x, _ = x.Update(tea.KeyMsg{Type: tea.KeyEsc})
		}
		if k == "4" {
			for i := 0; i < len(scan.Run(m).Checks); i++ {
				x, _ = x.Update(tea.KeyMsg{Type: tea.KeyEnter})
				vs = append(vs, x.View())
				x, _ = x.Update(tea.KeyMsg{Type: tea.KeyDown})
			}
		}
	}
	return vs
}

func assertViewRunes(t *testing.T, label string, vs []string) {
	t.Helper()
	for i, v := range vs {
		for _, r := range v {
			bad := (r < 0x20 && r != '\n') || (r >= 0x7f && r <= 0x9f) || (r >= 0x202a && r <= 0x202e) ||
				(r >= 0x2066 && r <= 0x2069) || r == 0x200e || r == 0x200f || r == 0x061c ||
				r == 0x2028 || r == 0x2029 || r == 0x200b || r == 0xfeff || r == 0xad
			if bad {
				t.Errorf("%s view %d contains raw %U", label, i, r)
				return
			}
		}
	}
}

func TestQA_SanitizeSpoofNotedByViewTokens(t *testing.T) {
	if sanitize.From("\x1b").String() == sanitize.From("<U+001B>").String() {
		t.Fatal("sanitized ESC collides with a literal token")
	}
}
