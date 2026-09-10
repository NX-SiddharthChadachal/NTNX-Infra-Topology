package ui

import (
	"fmt"
	"strings"

	"github.com/nutanix/ntnx-topo/internal/model"
)

func renderConfigBody(fields []model.ConfigField, errMsg string, loading bool, width, height, offset int) string {
	if width < 4 {
		width = 4
	}
	var lines []string
	if loading {
		lines = append(lines, styleHelp.Render("loading..."))
	}
	if errMsg != "" {
		label := errMsg
		if errMsg != "credentials required" {
			label = "error: " + errMsg
		}
		wrapped := wrapWords(label, width)
		for _, ln := range wrapped {
			lines = append(lines, styleRed.Render(ln))
		}
	}
	keyW, valW := configColumns(fields, width)
	for _, f := range fields {
		k := styleHelp.Render(fmt.Sprintf("%-*s", keyW, truncCells(f.Key, keyW)))
		valLines := wrapWords(f.Value, valW)
		if len(valLines) == 0 {
			valLines = []string{""}
		}
		lines = append(lines, k+"  "+styleMetrics.Render(valLines[0]))
		pad := strings.Repeat(" ", keyW+2)
		for _, extra := range valLines[1:] {
			lines = append(lines, pad+styleMetrics.Render(extra))
		}
	}
	if len(lines) == 0 {
		lines = []string{styleHelp.Render("click a node")}
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(lines) {
		offset = max(0, len(lines)-1)
	}
	lines = lines[offset:]
	if height > 0 && len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}

// configColumns splits width into a key and a value column that always add up
// to at most width, including the two-space gutter. A value column narrower
// than eight cells shrinks the key column first.
func configColumns(fields []model.ConfigField, width int) (keyW, valW int) {
	keyW = 10
	for _, f := range fields {
		if n := len(f.Key); n > keyW {
			keyW = n
		}
	}
	if keyW > width/2 {
		keyW = width / 2
	}
	if valW = width - keyW - 2; valW < 8 {
		keyW = width - 2 - 8
		valW = 8
	}
	if keyW < 1 {
		keyW = 1
	}
	if valW = width - keyW - 2; valW < 1 {
		valW = 1
	}
	return keyW, valW
}

func truncCells(s string, width int) string {
	if width < 1 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	return string(runes[:width-1]) + "…"
}

func wrapWords(s string, width int) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if width < 1 {
		width = 1
	}
	var out []string
	for len(s) > width {
		cut := width
		if i := strings.LastIndex(s[:width], " "); i > width/3 {
			cut = i
		}
		out = append(out, strings.TrimSpace(s[:cut]))
		s = strings.TrimSpace(s[cut:])
	}
	if s != "" {
		out = append(out, s)
	}
	return out
}

func entityFallbackFields(h Hit) []model.ConfigField {
	var out []model.ConfigField
	add := func(k, v string) {
		if strings.TrimSpace(v) == "" {
			return
		}
		out = append(out, model.ConfigField{Key: k, Value: v})
	}
	add("kind", h.Kind)
	add("name", h.Name)
	add("extId", h.ID)
	add("IP", h.IP)
	add("switch", h.Switch)
	return out
}
