package ui

import (
	"strings"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

var zoomSteps = []float64{0.5, 0.75, 1.0, 1.5, 2.0}

func nextZoom(cur float64, dir int) float64 {
	idx := 2
	best := 1.0
	for i, z := range zoomSteps {
		if absf(z-cur) < absf(best-cur) {
			best = z
			idx = i
		}
	}
	idx += dir
	if idx < 0 {
		idx = 0
	}
	if idx >= len(zoomSteps) {
		idx = len(zoomSteps) - 1
	}
	return zoomSteps[idx]
}

func absf(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func padToHeight(s string, h int) string {
	if h < 1 {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func cutCells(s string, start, width int) string {
	if width <= 0 {
		return ""
	}
	if start < 0 {
		pad := -start
		if pad >= width {
			return strings.Repeat(" ", width)
		}
		return strings.Repeat(" ", pad) + cutCells(s, 0, width-pad)
	}
	cut := ansi.Cut(s, start, start+width)
	w := ansi.StringWidth(cut)
	if w < width {
		cut += strings.Repeat(" ", width-w)
	}
	return cut
}

func zoomGaps(zoom float64) (boxGap, groupGap string) {
	switch {
	case zoom >= 2:
		return "    ", "        "
	case zoom >= 1.5:
		return "   ", "      "
	case zoom <= 0.5:
		return " ", "  "
	default:
		return "  ", "    "
	}
}

func zoomPad(zoom float64) (v, h int) {
	switch {
	case zoom >= 2:
		return 1, 3
	case zoom >= 1.5:
		return 1, 2
	case zoom <= 0.5:
		return 0, 0
	default:
		return 0, 1
	}
}

type vpTransform struct {
	baseX, baseY, offX, offY, viewW, viewH int
}

func viewportTransform(art string, viewW, viewH, offX, offY int) vpTransform {
	if viewW < 1 {
		viewW = 1
	}
	if viewH < 1 {
		viewH = 1
	}
	raw := strings.Split(strings.TrimRight(art, "\n"), "\n")
	if len(raw) == 1 && raw[0] == "" {
		raw = nil
	}
	contentH := len(raw)
	contentW := 0
	for _, ln := range raw {
		if w := lipgloss.Width(ln); w > contentW {
			contentW = w
		}
	}
	t := vpTransform{viewW: viewW, viewH: viewH, offX: offX, offY: offY}
	if contentW < viewW {
		t.baseX = (viewW - contentW) / 2
	}
	if contentH < viewH {
		t.baseY = (viewH - contentH) / 2
	}
	if contentW > viewW {
		if t.offX < 0 {
			t.offX = 0
		}
		if t.offX > contentW-viewW {
			t.offX = contentW - viewW
		}
	} else {
		t.offX = 0
	}
	if contentH > viewH {
		if t.offY < 0 {
			t.offY = 0
		}
		if t.offY > contentH-viewH {
			t.offY = contentH - viewH
		}
	} else {
		t.offY = 0
	}
	return t
}

// applyViewport windows art into viewW x viewH with a cell offset (offX, offY).
func applyViewport(art string, viewW, viewH, offX, offY int) string {
	t := viewportTransform(art, viewW, viewH, offX, offY)
	raw := strings.Split(strings.TrimRight(art, "\n"), "\n")
	if len(raw) == 1 && raw[0] == "" {
		raw = nil
	}
	contentH := len(raw)
	srcY := t.offY - t.baseY
	lines := make([]string, t.viewH)
	for i := 0; i < t.viewH; i++ {
		si := srcY + i
		if si < 0 || si >= contentH {
			lines[i] = strings.Repeat(" ", t.viewW)
			continue
		}
		lines[i] = cutCells(raw[si], t.offX-t.baseX, t.viewW)
	}
	return strings.Join(lines, "\n")
}

func showIPs(zoom float64) bool {
	return zoom >= 0.75
}

func nodeLabel(name, ip string, zoom float64) string {
	if ip != "" && showIPs(zoom) {
		return name + "\n" + ip
	}
	return name
}
