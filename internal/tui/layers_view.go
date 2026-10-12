package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/cecilkootz/model-inspector/internal/model"
	"github.com/cecilkootz/model-inspector/internal/sanitize"
)

func (t Model) dtypeOnce(raw string) sanitize.Text {
	if raw == "" {
		return sanitize.Program("")
	}
	if t.lay != nil {
		return t.lay.once(raw)
	}
	return sanitize.From(raw)
}

func quantBadge(m *model.Model, role string) string {
	if role == "" || role == "plain" || role == "qscal" || role == "qbias" {
		return ""
	}
	if !strings.HasPrefix(role, "q") {
		return ""
	}
	if m != nil && m.Quant != nil && m.Quant.GroupSize > 0 && !strings.Contains(role, "+") {
		return role + " g" + strconv.Itoa(m.Quant.GroupSize)
	}
	return role
}

func humanCount(n int64) string {
	if n < 0 {
		n = 0
	}
	if n < 1000 {
		return strconv.FormatInt(n, 10)
	}
	units := []string{"K", "M", "B", "T"}
	v := float64(n)
	u := -1
	for v >= 1000 && u < len(units)-1 {
		v /= 1000
		u++
	}
	return fmt.Sprintf("%.1f%s", v, units[u])
}

func (t Model) groupAt(gidx int) *model.Group {
	if t.m == nil || gidx < 0 || gidx >= len(t.m.Groups) {
		return nil
	}
	return t.m.Groups[gidx]
}
