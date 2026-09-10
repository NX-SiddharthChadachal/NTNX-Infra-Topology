package ui

import (
	"strings"

	lipgloss "charm.land/lipgloss/v2"
)

const (
	defaultSplitRatio  = 0.28
	defaultConfigRatio = 0.32
	minConfigRatio     = 0.15
	maxConfigRatio     = 0.7
	minLeftWidth       = 24
	minRightWidth      = 20
	minConfigWidth     = 28
	footerReserve      = 3
	dividerWidth       = 3
)

func leftWidthFor(termWidth int, ratio float64) int {
	usable := termWidth - dividerWidth
	if usable < minLeftWidth+minRightWidth {
		return max(8, usable/3)
	}
	w := int(ratio * float64(usable))
	maxLeft := usable / 2
	if maxLeft < minLeftWidth {
		maxLeft = minLeftWidth
	}
	if w < minLeftWidth {
		w = minLeftWidth
	}
	if w > maxLeft {
		w = maxLeft
	}
	if w > usable-minRightWidth {
		w = usable - minRightWidth
	}
	if w < 1 {
		w = 1
	}
	return w
}

// dragTarget names the drag bar the mouse is currently holding.
type dragTarget int

const (
	dragNone dragTarget = iota
	dragLeftSplit
	dragConfigSplit
)

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func inDivider(x, leftW int) bool {
	return x >= leftW && x < leftW+dividerWidth
}

func renderDivider(height int, active bool) string {
	if height < 1 {
		height = 1
	}
	st := lipgloss.NewStyle().
		Width(dividerWidth).
		Align(lipgloss.Center).
		Foreground(colorCyan).
		Background(lipgloss.Color("#1a3a4a")).
		Bold(true)
	if active {
		st = st.
			Foreground(lipgloss.Color("#00333a")).
			Background(colorCyan)
	}

	mid := height / 2
	lines := make([]string, height)
	for i := range lines {
		ch := "║"
		switch {
		case i == 0:
			ch = "╥"
		case i == height-1:
			ch = "╨"
		case i == mid:
			ch = "↕"
		case i == mid-1 && height > 4:
			ch = "═"
		case i == mid+1 && height > 4:
			ch = "═"
		}
		lines[i] = st.Render(ch)
	}
	return strings.Join(lines, "\n")
}

func renderPane(title, body string, width, height int) string {
	if width < 4 {
		width = 4
	}
	if height < 3 {
		height = 3
	}
	innerW := width - 2
	if innerW < 1 {
		innerW = 1
	}
	innerH := height - 2
	if innerH < 1 {
		innerH = 1
	}

	header := title
	headerH := lipgloss.Height(header)
	if headerH < 1 {
		headerH = 1
	}
	bodyH := innerH - headerH
	if bodyH < 1 {
		bodyH = 1
		headerH = innerH - 1
		if headerH < 1 {
			headerH = 1
		}
		header = padToHeight(header, headerH)
	}

	clipped := padToHeight(padLinesToWidth(body, innerW), bodyH)
	content := padToHeight(header+"\n"+clipped, innerH)
	content = padLinesToWidth(content, innerW)

	// Width counts the border, Height does not. Content is already exactly
	// innerW x innerH cells, so nothing can wrap and push the bottom border out.
	return lipgloss.NewStyle().
		Width(width).
		Height(innerH).
		MaxWidth(width).
		MaxHeight(height).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorDim).
		Render(content)
}

func padLinesToWidth(s string, w int) string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = cutCells(ln, 0, w)
	}
	return strings.Join(lines, "\n")
}

// overlayAt draws overlay onto base at cell (ox, oy). Overlay wins; base ANSI is preserved outside it.
func overlayAt(base, overlay string, ox, oy int) string {
	if overlay == "" {
		return base
	}
	baseLines := strings.Split(base, "\n")
	overLines := strings.Split(strings.TrimRight(overlay, "\n"), "\n")
	ow := 0
	for _, ln := range overLines {
		if w := lipgloss.Width(ln); w > ow {
			ow = w
		}
	}
	for len(baseLines) < oy+len(overLines) {
		baseLines = append(baseLines, "")
	}
	for i, oln := range overLines {
		row := oy + i
		if row < 0 || row >= len(baseLines) {
			continue
		}
		x := ox
		line := oln
		if x < 0 {
			line = cutCells(line, -x, lipgloss.Width(line)+x)
			x = 0
		}
		left := cutCells(baseLines[row], 0, x)
		mid := cutCells(line, 0, ow)
		baseW := lipgloss.Width(baseLines[row])
		rightStart := x + ow
		right := ""
		if baseW > rightStart {
			right = cutCells(baseLines[row], rightStart, baseW-rightStart)
			right = strings.TrimRight(right, " ")
		}
		baseLines[row] = left + mid + right
	}
	return strings.Join(baseLines, "\n")
}

func clipFrame(s string, width, height int) string {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	return lipgloss.NewStyle().
		Width(width).
		MaxWidth(width).
		Height(height).
		MaxHeight(height).
		Render(s)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// configDividerX is the first column of the drag bar between Topology and Config.
func configDividerX(leftW, topoW int) int {
	return leftW + dividerWidth + topoW
}

func layoutPanes(termWidth int, ratio, configRatio float64, configOpen bool) (leftW, topoW, cfgW int) {
	leftW = leftWidthFor(termWidth, ratio)
	rest := termWidth - leftW - dividerWidth
	if rest < minRightWidth {
		rest = minRightWidth
	}
	if !configOpen {
		return leftW, rest, 0
	}
	inner := rest - dividerWidth
	if inner < 8 {
		inner = 8
	}
	if configRatio < minConfigRatio {
		configRatio = minConfigRatio
	}
	if configRatio > maxConfigRatio {
		configRatio = maxConfigRatio
	}
	cfgW = int(float64(inner) * configRatio)
	if cfgW < minConfigWidth {
		cfgW = minConfigWidth
	}
	topoW = inner - cfgW
	if topoW < minRightWidth {
		topoW = minRightWidth
		cfgW = inner - topoW
	}
	if topoW < 8 {
		topoW = min(8, inner/2)
		if topoW < 1 {
			topoW = 1
		}
		cfgW = inner - topoW
	}
	if cfgW < 8 {
		cfgW = min(8, inner/3)
		if cfgW < 1 {
			cfgW = 1
		}
		topoW = inner - cfgW
	}
	return leftW, topoW, cfgW
}
