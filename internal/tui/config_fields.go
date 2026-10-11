package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/cecilkootz/model-inspector/internal/jsonx"
	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/sanitize"
	"github.com/cecilkootz/model-inspector/internal/tui/ui"
)

const (
	fieldHeader = iota
	fieldRow
	fieldBlank
	fieldNote
)

type fieldLine struct {
	kind    int
	label   string
	value   string
	unit    string
	derived string
	bad     bool
	jump    int32
	meter   float64
	meterOn bool
}

func (t Model) fieldLines() []fieldLine {
	if t.cfg == nil {
		return nil
	}
	if t.cfg.lines == nil || t.cfg.linesGen != t.cfg.gen || t.cfg.dirty {
		t.cfg.lines = t.buildFields()
		t.cfg.linesGen = t.cfg.gen
	}
	if t.cfg.fieldCur < 0 || t.cfg.fieldCur >= len(t.cfg.lines) || t.cfg.lines[t.cfg.fieldCur].kind != fieldRow {
		for i, ln := range t.cfg.lines {
			if ln.kind == fieldRow {
				t.cfg.fieldCur = i
				break
			}
		}
	}
	return t.cfg.lines
}

func (t Model) buildFields() []fieldLine {
	if t.m == nil {
		return nil
	}
	if t.cfg.err != nil {
		if _, ok := t.cfg.err.(*jsonx.NodeLimitError); !ok {
			return nil
		}
	}
	if t.cfg.tree != nil && len(t.cfg.tree.Nodes) > 0 && t.cfg.tree.Nodes[0].Kind != jsonx.KindObject {
		return []fieldLine{{kind: fieldNote, label: "config.json is not a JSON object; Key fields unavailable"}}
	}
	var out []fieldLine
	add := func(group string, rows []fieldLine) {
		if len(rows) == 0 {
			return
		}
		if len(out) > 0 {
			out = append(out, fieldLine{kind: fieldBlank})
		}
		out = append(out, fieldLine{kind: fieldHeader, label: group})
		out = append(out, rows...)
	}
	add("TEXT", t.textFields())
	add("ATTENTION", t.attentionFields())
	if rows := t.moeFields(); len(rows) > 0 {
		add("MOE", rows)
	}
	if t.m.Quant != nil {
		add("QUANT", t.quantFields())
	}
	if rows := t.visionFields(); len(rows) > 0 {
		add("VISION", rows)
	}
	if rows := t.mtpFields(); len(rows) > 0 {
		add("MTP", rows)
	}
	if len(out) == 0 && t.cfg.tree != nil && t.cfg.err == nil {
		out = append(out, fieldLine{kind: fieldNote, label: "config.json has no fields"})
	}
	return out
}

type picked struct {
	id    int32
	ok    bool
	kind  jsonx.Kind
	lit   string
	text  string
	bad   bool
	num   int64
	isInt bool
	f     float64
}

func (t Model) pick(scope string, keys ...string) picked {
	if t.cfg.tree != nil && t.cfg.err == nil && len(t.cfg.tree.Nodes) > 0 && t.cfg.tree.Nodes[0].Kind == jsonx.KindObject {
		base := int32(0)
		if scope != "" {
			base = t.cfg.findChild(0, scope)
			if base < 0 {
				return picked{id: -1}
			}
		}
		// KeyConfig order: root, text_config, llm_config, language_config.
		scopes := []int32{base}
		if scope == "" {
			scopes = []int32{0}
			for _, name := range []string{"text_config", "llm_config", "language_config"} {
				id := t.cfg.findChild(0, name)
				if id >= 0 {
					scopes = append(scopes, id)
				}
			}
		}
		for _, key := range keys {
			for _, sc := range scopes {
				id := t.cfg.findChild(sc, key)
				if id < 0 {
					continue
				}
				return t.pickedFrom(id)
			}
		}
		return picked{id: -1}
	}
	return t.pickConfig(scope, keys...)
}

func (t Model) pickedFrom(id int32) picked {
	n := t.cfg.tree.Nodes[id]
	p := picked{id: id, ok: true, kind: n.Kind, lit: n.Lit, text: t.cfg.litText(id).String()}
	switch n.Kind {
	case jsonx.KindNumber:
		if !strings.ContainsAny(n.Lit, ".eE") {
			if v, err := strconv.ParseInt(n.Lit, 10, 64); err == nil {
				p.isInt = true
				p.num = v
				p.f = float64(v)
			}
		}
		if !p.isInt {
			if v, ok := jsonx.NumberFits(n.Lit); ok {
				p.f = v
			} else {
				p.bad = true
			}
		}
	case jsonx.KindError:
		p.bad = true
		p.lit = n.Lit
		p.text = n.Lit
	case jsonx.KindString, jsonx.KindBool, jsonx.KindNull:
	default:
		p.bad = true
		p.text = t.cfg.preview(id, 40)
	}
	return p
}

func (t Model) pickConfig(scope string, keys ...string) picked {
	if t.m.Config == nil {
		return picked{id: -1}
	}
	scopes := []map[string]any{t.m.Config}
	if scope == "vision_config" {
		scopes = []map[string]any{sub(t.m.Config, "vision_config")}
	} else if scope == "" {
		for _, name := range []string{"text_config", "llm_config", "language_config"} {
			if m := sub(t.m.Config, name); m != nil {
				scopes = append(scopes, m)
			}
		}
	} else if m := sub(t.m.Config, scope); m != nil {
		scopes = []map[string]any{m}
	}
	for _, key := range keys {
		for _, sc := range scopes {
			if sc == nil {
				continue
			}
			v, ok := sc[key]
			if !ok || v == nil {
				continue
			}
			return pickedFromAny(v)
		}
	}
	return picked{id: -1}
}

func sub(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	v, _ := m[key].(map[string]any)
	return v
}

func pickedFromAny(v any) picked {
	p := picked{id: -1, ok: true}
	switch x := v.(type) {
	case float64:
		p.kind = jsonx.KindNumber
		p.f = x
		if x == float64(int64(x)) {
			p.isInt = true
			p.num = int64(x)
			p.lit = strconv.FormatInt(p.num, 10)
			p.text = model.Comma(p.num)
		} else {
			p.lit = strconv.FormatFloat(x, 'f', -1, 64)
			p.text = p.lit
		}
	case string:
		p.kind = jsonx.KindString
		p.lit = x
		p.text = sanitize.From(x).String()
	case bool:
		p.kind = jsonx.KindBool
		if x {
			p.lit, p.text = "true", "true"
		} else {
			p.lit, p.text = "false", "false"
		}
	default:
		p.bad = true
		p.text = sanitize.From(fmt.Sprint(v)).String()
	}
	return p
}

func (p picked) showNum(wantNum bool) (string, bool) {
	if !p.ok {
		return "", false
	}
	if wantNum && p.kind != jsonx.KindNumber {
		return p.text + " !", true
	}
	if p.kind == jsonx.KindNumber && p.isInt {
		return model.Comma(p.num), false
	}
	if p.bad && p.kind == jsonx.KindNumber {
		return p.text + " ! out of range", true
	}
	return p.text, p.bad
}

func (t Model) textFields() []fieldLine {
	var rows []fieldLine
	if p := t.pick("", "model_type"); p.ok {
		rows = append(rows, fieldLine{kind: fieldRow, label: "model_type", value: p.text, jump: p.id})
	}
	layers := t.pick("", "num_hidden_layers")
	if layers.ok {
		val, bad := layers.showNum(true)
		row := fieldLine{kind: fieldRow, label: "layers", value: val, unit: "layers", jump: layers.id, bad: bad}
		if !bad && layers.isInt {
			if d := t.layerMix(); d != "" {
				row.derived = d
			}
		}
		rows = append(rows, row)
	}
	if p := t.pick("", "hidden_size"); p.ok {
		val, bad := p.showNum(true)
		rows = append(rows, fieldLine{kind: fieldRow, label: "hidden_size", value: val, unit: "dim", jump: p.id, bad: bad})
	}
	if p := t.pick("", "vocab_size"); p.ok {
		val, bad := p.showNum(true)
		rows = append(rows, fieldLine{kind: fieldRow, label: "vocab_size", value: val, unit: "tok", jump: p.id, bad: bad})
	}
	if p := t.pick("", "max_position_embeddings"); p.ok {
		val, bad := p.showNum(true)
		row := fieldLine{kind: fieldRow, label: "context", value: val, unit: "tok", jump: p.id, bad: bad}
		if !bad && p.isInt {
			row.derived = t.contextDerived(p.num)
		}
		rows = append(rows, row)
	}
	if p := t.pick("", "tie_word_embeddings"); p.ok {
		rows = append(rows, fieldLine{kind: fieldRow, label: "tie_embeddings", value: p.text, jump: p.id, bad: p.kind != jsonx.KindBool && p.bad})
	}
	if p := t.pick("", "dtype", "torch_dtype"); p.ok {
		rows = append(rows, fieldLine{kind: fieldRow, label: "dtype", value: p.text, jump: p.id})
	}
	return rows
}

func (t Model) layerMix() string {
	counts := map[string]int{}
	if t.cfg.tree != nil && t.cfg.err == nil {
		id := t.findScoped("layer_types")
		if id >= 0 && t.cfg.tree.Nodes[id].Kind == jsonx.KindArray {
			for ch := t.cfg.tree.Nodes[id].First; ch >= 0; ch = t.cfg.tree.Nodes[ch].Next {
				if t.cfg.tree.Nodes[ch].Kind == jsonx.KindString {
					counts[t.cfg.tree.Nodes[ch].Lit]++
				}
			}
		}
	}
	if len(counts) == 0 && t.m != nil {
		counts = t.m.LayerTypeCounts()
	}
	if len(counts) == 0 {
		return ""
	}
	lin := counts["linear_attention"]
	full := counts["full_attention"]
	sep := " · "
	if !ui.UTF8() {
		sep = " . "
	}
	if lin == 0 && full == 0 {
		return ""
	}
	return fmt.Sprintf("= %d linear%s%d full", lin, sep, full)
}

func (t Model) findScoped(key string) int32 {
	if t.cfg.tree == nil {
		return -1
	}
	if id := t.cfg.findChild(0, key); id >= 0 {
		return id
	}
	for _, name := range []string{"text_config", "llm_config", "language_config"} {
		sc := t.cfg.findChild(0, name)
		if sc < 0 {
			continue
		}
		if id := t.cfg.findChild(sc, key); id >= 0 {
			return id
		}
	}
	return -1
}

func (t Model) contextDerived(n int64) string {
	base := magLabel(n)
	if base == "" {
		base = model.Comma(n)
	}
	factor, typ, ok := t.ropeScale()
	times := ui.Times()
	arrow := ui.RArrow()
	if !ok {
		return "= " + base + " native"
	}
	scaled := int64(float64(n) * factor)
	dest := magLabel(scaled)
	if dest == "" {
		dest = model.Comma(scaled)
	}
	fac := strconv.FormatFloat(factor, 'f', -1, 64)
	return fmt.Sprintf("= %s %s %s %s %s %s", base, times, fac, typ, arrow, dest)
}

func (t Model) ropeScale() (float64, string, bool) {
	if t.cfg.tree == nil {
		return t.ropeScaleConfig()
	}
	id := t.findScoped("rope_scaling")
	if id < 0 || t.cfg.tree.Nodes[id].Kind != jsonx.KindObject {
		return 0, "", false
	}
	f := t.cfg.findChild(id, "factor")
	ty := t.cfg.findChild(id, "type")
	if f < 0 || ty < 0 {
		return 0, "", false
	}
	fn, ok := jsonx.NumberFits(t.cfg.tree.Nodes[f].Lit)
	if !ok || t.cfg.tree.Nodes[ty].Kind != jsonx.KindString {
		return 0, "", false
	}
	return fn, t.cfg.tree.Nodes[ty].Lit, true
}

func (t Model) ropeScaleConfig() (float64, string, bool) {
	return 0, "", false
}

func (t Model) attentionFields() []fieldLine {
	var rows []fieldLine
	q := t.pick("", "num_attention_heads")
	kv := t.pick("", "num_key_value_heads")
	if q.ok || kv.ok {
		qs, qb := "–", true
		ks := "–"
		if q.ok {
			qs, qb = q.showNum(true)
		}
		kb := false
		if kv.ok {
			ks, kb = kv.showNum(true)
		}
		row := fieldLine{kind: fieldRow, label: "heads q / kv", value: qs + " / " + ks, bad: qb || kb}
		if kv.ok {
			row.jump = kv.id
		} else {
			row.jump = q.id
		}
		if q.ok && kv.ok && q.isInt && kv.isInt && !qb && !kb {
			row.derived = gqaText(q.num, kv.num)
		}
		rows = append(rows, row)
	}
	hd := t.pick("", "head_dim")
	hidden := t.pick("", "hidden_size")
	if hd.ok || (hidden.ok && q.ok && hidden.isInt && q.isInt && q.num > 0 && hidden.num%q.num == 0) {
		row := fieldLine{kind: fieldRow, label: "head_dim", unit: "dim", jump: -1}
		if hd.ok {
			val, bad := hd.showNum(true)
			row.value = val
			row.bad = bad
			row.jump = hd.id
			if !bad && hd.isInt && q.ok && q.isInt && kv.ok && kv.isInt {
				row.derived = widthText(hd.num, q.num, kv.num)
			}
		} else {
			d := hidden.num / q.num
			row.value = model.Comma(d)
			row.jump = q.id
			row.derived = fmt.Sprintf("= %s (hidden %s heads)", model.Comma(d), divSign())
		}
		rows = append(rows, row)
	}
	if p := t.pick("", "full_attention_interval"); p.ok {
		val, bad := p.showNum(true)
		row := fieldLine{kind: fieldRow, label: "full-attn every", value: val, unit: "layers", jump: p.id, bad: bad}
		if !bad && p.isInt {
			row.derived = t.fullAgree(p.num)
		}
		rows = append(rows, row)
	}
	lk := t.pick("", "linear_num_key_heads")
	lv := t.pick("", "linear_num_value_heads")
	if lk.ok || lv.ok {
		a, b := "–", "–"
		if lk.ok {
			a, _ = lk.showNum(true)
		}
		if lv.ok {
			b, _ = lv.showNum(true)
		}
		jump := lk.id
		if !lk.ok {
			jump = lv.id
		}
		rows = append(rows, fieldLine{kind: fieldRow, label: "linear heads k/v", value: a + " / " + b, jump: jump})
	}
	if p := t.pick("", "sliding_window"); p.ok {
		val, bad := p.showNum(true)
		rows = append(rows, fieldLine{kind: fieldRow, label: "sliding_window", value: val, jump: p.id, bad: bad})
	}
	if p := t.pick("", "rope_theta"); p.ok {
		val, bad := p.showNum(true)
		row := fieldLine{kind: fieldRow, label: "rope_theta", value: val, jump: p.id, bad: bad}
		if part := t.pick("", "partial_rotary_factor"); part.ok {
			row.derived = "partial " + part.text
		}
		rows = append(rows, row)
	}
	return rows
}

func gqaText(q, kv int64) string {
	if kv <= 0 || q <= 0 {
		return ""
	}
	if kv == q {
		return "= MHA"
	}
	if kv == 1 {
		return "= MQA"
	}
	if q%kv == 0 {
		return fmt.Sprintf("= %d:1 GQA", q/kv)
	}
	return fmt.Sprintf("= %d:%d", q, kv)
}

func widthText(head, q, kv int64) string {
	sep := " · "
	if !ui.UTF8() {
		sep = " . "
	}
	return fmt.Sprintf("= %s q%s%s kv wide", model.Comma(head*q), sep, model.Comma(head*kv))
}

func divSign() string {
	if ui.UTF8() {
		return "÷"
	}
	return "/"
}

func (t Model) fullAgree(interval int64) string {
	if interval <= 0 {
		return ""
	}
	layers := t.pick("", "num_hidden_layers")
	if !layers.ok || !layers.isInt || layers.num%interval != 0 {
		return ""
	}
	want := int(layers.num / interval)
	got := -1
	if t.cfg.tree != nil {
		id := t.findScoped("layer_types")
		if id >= 0 {
			n := 0
			for ch := t.cfg.tree.Nodes[id].First; ch >= 0; ch = t.cfg.tree.Nodes[ch].Next {
				if t.cfg.tree.Nodes[ch].Kind == jsonx.KindString && t.cfg.tree.Nodes[ch].Lit == "full_attention" {
					n++
				}
			}
			got = n
		}
	}
	if got < 0 && t.m != nil {
		if c := t.m.LayerTypeCounts(); c != nil {
			got = c["full_attention"]
		}
	}
	if got < 0 {
		return ""
	}
	if got == want {
		return "= agrees with layer_types " + ui.G().Pass
	}
	return fmt.Sprintf("! layer_types says %d", got)
}

func (t Model) moeFields() []fieldLine {
	exp := t.pick("", "num_experts", "num_local_experts", "n_routed_experts")
	per := t.pick("", "num_experts_per_tok")
	ffn := t.pick("", "moe_intermediate_size")
	if !exp.ok && !per.ok && !ffn.ok {
		return nil
	}
	var rows []fieldLine
	if exp.ok {
		val, bad := exp.showNum(true)
		rows = append(rows, fieldLine{kind: fieldRow, label: "experts", value: val, jump: exp.id, bad: bad})
	}
	if per.ok {
		val, bad := per.showNum(true)
		sharedN, sharedExplicit := t.sharedExperts()
		row := fieldLine{kind: fieldRow, label: "per token", value: val, jump: per.id, bad: bad}
		if sharedN > 0 {
			row.value = fmt.Sprintf("%s + %d shared", val, sharedN)
		}
		if !bad && per.isInt && exp.ok && exp.isInt && exp.num > 0 {
			active := per.num + int64(sharedN)
			pct := float64(active) / float64(exp.num) * 100
			if sharedN > 0 {
				row.derived = fmt.Sprintf("= %d of %d active (%.1f%%)", active, exp.num, pct)
				row.meterOn = true
				row.meter = float64(active) / float64(exp.num)
			} else if !sharedExplicit {
				row.derived = fmt.Sprintf("= %d of %d (%.1f%%)", per.num, exp.num, float64(per.num)/float64(exp.num)*100)
			}
		}
		rows = append(rows, row)
	}
	if ffn.ok {
		val, bad := ffn.showNum(true)
		row := fieldLine{kind: fieldRow, label: "expert ffn", value: val, unit: "dim", jump: ffn.id, bad: bad}
		if sh := t.pick("", "shared_expert_intermediate_size"); sh.ok && sh.isInt {
			row.derived = "shared " + model.Comma(sh.num)
		}
		rows = append(rows, row)
	}
	return rows
}

func (t Model) sharedExperts() (int, bool) {
	if p := t.pick("", "n_shared_experts"); p.ok && p.isInt {
		return int(p.num), true
	}
	if p := t.pick("", "shared_expert_intermediate_size"); p.ok && p.isInt && p.num > 0 {
		return 1, false
	}
	return 0, false
}

func (t Model) quantFields() []fieldLine {
	q := t.m.Quant
	if q == nil {
		return nil
	}
	sep := " · "
	if !ui.UTF8() {
		sep = " . "
	}
	scheme := fmt.Sprintf("q%d%sg%d", q.Bits, sep, q.GroupSize)
	mode := q.Mode
	if mode == "" {
		mode = "affine"
	}
	derived := mode
	if t.m.Format != "" && strings.Contains(strings.ToLower(t.m.Format), "mlx") {
		derived += sep + "mlx"
	}
	jump := int32(-1)
	if t.cfg.tree != nil {
		if id := t.findScoped("quantization"); id >= 0 {
			jump = id
		} else if id := t.findScoped("quantization_config"); id >= 0 {
			jump = id
		}
	}
	rows := []fieldLine{{
		kind: fieldRow, label: "scheme", value: scheme, derived: derived, jump: jump,
	}}
	if eff, ok := effectiveBits(q.Bits, q.GroupSize, mode); ok {
		rows = append(rows, fieldLine{kind: fieldRow, label: "effective", value: eff, unit: "bits/w", jump: jump})
	}
	rows = append(rows, fieldLine{
		kind: fieldRow, label: "overrides", value: fmt.Sprintf("%d modules", q.NumOverrides), jump: jump,
	})
	return rows
}

func effectiveBits(bits, group int, mode string) (string, bool) {
	if bits <= 0 || group <= 0 {
		return "", false
	}
	times := ui.Times()
	approx := "≈"
	if !ui.UTF8() {
		approx = "~"
	}
	var v float64
	var expr string
	if strings.Contains(strings.ToLower(mode), "sym") {
		v = float64(bits) + 16.0/float64(group)
		expr = fmt.Sprintf("%d + 16 / %d", bits, group)
	} else {
		v = float64(bits) + 2*16.0/float64(group)
		expr = fmt.Sprintf("%d + 2%s16 / %d", bits, times, group)
	}
	return fmt.Sprintf("%s %.1f bits/w = %s", approx, v, expr), true
}

func (t Model) visionFields() []fieldLine {
	if t.cfg.tree != nil && t.cfg.err == nil {
		if t.cfg.findChild(0, "vision_config") < 0 && t.pick("vision_config", "depth").id < 0 && !t.pick("vision_config", "depth").ok {
			if t.m.Config == nil || sub(t.m.Config, "vision_config") == nil {
				return nil
			}
		}
	} else if t.m.Config == nil || sub(t.m.Config, "vision_config") == nil {
		return nil
	}
	depth := t.pick("vision_config", "depth")
	hidden := t.pick("vision_config", "hidden_size")
	heads := t.pick("vision_config", "num_heads")
	patch := t.pick("vision_config", "patch_size")
	if !depth.ok && !hidden.ok && !heads.ok && !patch.ok {
		return nil
	}
	var rows []fieldLine
	if depth.ok || hidden.ok {
		a, b := "–", "–"
		jump := int32(-1)
		if depth.ok {
			a, _ = depth.showNum(true)
			jump = depth.id
		}
		if hidden.ok {
			b, _ = hidden.showNum(true)
			if jump < 0 {
				jump = hidden.id
			}
		}
		row := fieldLine{kind: fieldRow, label: "depth · hidden", value: a + " · " + b, jump: jump}
		if !ui.UTF8() {
			row.label = "depth . hidden"
			row.value = a + " . " + b
		}
		if hidden.ok && heads.ok && hidden.isInt && heads.isInt && heads.num > 0 && hidden.num%heads.num == 0 {
			row.derived = "= head_dim " + model.Comma(hidden.num/heads.num)
		}
		rows = append(rows, row)
	}
	if heads.ok || patch.ok {
		a, b := "–", "–"
		jump := int32(-1)
		if heads.ok {
			a, _ = heads.showNum(true)
			jump = heads.id
		}
		if patch.ok {
			b, _ = patch.showNum(true)
			if jump < 0 {
				jump = patch.id
			}
		}
		lab, val := "heads · patch", a+" · "+b
		if !ui.UTF8() {
			lab, val = "heads . patch", a+" . "+b
		}
		rows = append(rows, fieldLine{kind: fieldRow, label: lab, value: val, unit: "px", jump: jump})
	}
	return rows
}

func (t Model) mtpFields() []fieldLine {
	p := t.pick("", "mtp_num_hidden_layers", "num_nextn_predict_layers")
	if !p.ok {
		return nil
	}
	val, bad := p.showNum(true)
	row := fieldLine{kind: fieldRow, label: "layers", value: val, unit: "layers", jump: p.id, bad: bad}
	row.derived = "= speculative head"
	if t.m != nil {
		var bytes int64
		for _, g := range t.m.Groups {
			if g != nil && g.Kind == model.KindMTP {
				bytes += g.Bytes
			}
		}
		if bytes > 0 {
			row.derived = "= speculative head, +" + model.HumanBytes(bytes)
		}
	}
	return []fieldLine{row}
}
