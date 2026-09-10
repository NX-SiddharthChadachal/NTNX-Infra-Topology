package ui

import (
	"github.com/nutanix/ntnx-topo/internal/model"

	lipgloss "charm.land/lipgloss/v2"
)

var (
	colorGreen  = lipgloss.Color("#00FF00")
	colorYellow = lipgloss.Color("#FFD700")
	colorRed    = lipgloss.Color("#FF4444")
	colorGray   = lipgloss.Color("#808080")
	colorWhite  = lipgloss.Color("#FAFAFA")
	colorDim    = lipgloss.Color("#626262")
	colorCyan   = lipgloss.Color("#00CED1")

	styleGreen  = lipgloss.NewStyle().Foreground(colorGreen).Bold(true)
	styleYellow = lipgloss.NewStyle().Foreground(colorYellow).Bold(true)
	styleRed    = lipgloss.NewStyle().Foreground(colorRed).Bold(true)
	styleGray   = lipgloss.NewStyle().Foreground(colorGray)

	styleTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorCyan).
			MarginBottom(1)

	styleHelp = lipgloss.NewStyle().Foreground(colorDim)

	styleMetrics = lipgloss.NewStyle().
			Foreground(colorWhite).
			Bold(true)

	styleBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			Padding(0, 1)

	styleConnector = lipgloss.NewStyle().Foreground(colorDim)

	styleSelected = lipgloss.NewStyle().
			Foreground(colorCyan).
			Bold(true).
			Reverse(true)

	styleDivider = lipgloss.NewStyle().Foreground(colorDim)

	styleDividerActive = lipgloss.NewStyle().Foreground(colorCyan).Bold(true)

	stylePaneTitle = lipgloss.NewStyle().
			Foreground(colorCyan).
			Bold(true).
			Italic(true).
			Padding(0, 2)

	styleTab = lipgloss.NewStyle().
			Foreground(colorDim).
			Padding(0, 1)

	styleTabActive = lipgloss.NewStyle().
			Foreground(colorCyan).
			Bold(true).
			Reverse(true).
			Padding(0, 1)

	styleRack = lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(colorDim).
			Padding(0, 1)

	styleLegend = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorCyan).
			Padding(0, 1).
			Background(lipgloss.Color("#0a1a22"))
)

// StyleForStatus returns the Lipgloss style for a given component status.
// When active (API call in flight), the style gets a blink effect.
func StyleForStatus(s model.Status, active bool) lipgloss.Style {
	var base lipgloss.Style
	switch s {
	case model.StatusUp:
		base = styleGreen
	case model.StatusDegraded:
		base = styleYellow
	case model.StatusDown:
		base = styleRed
	default:
		base = styleGray
	}
	if active {
		base = base.Blink(true)
	}
	return base
}

// cursorCell is the text cursor: a reverse-video space rather than U+2588 FULL
// BLOCK. It looks identical but is unambiguously one cell wide, where the block
// is an East Asian "Ambiguous" rune that a terminal may advance by two.
func cursorCell() string {
	return lipgloss.NewStyle().Reverse(true).Render(" ")
}

// inputBox draws value inside a bordered text box of exactly width cells,
// border included.
//
// The width is always pinned. Given no Width, Lipgloss sizes the border from its
// own measurement of the content, and on the Windows console that measurement
// does not match what the terminal actually advances: the horizontal border came
// out shorter than the text and the right-hand corners landed adrift, several
// cells into whatever was on screen before.
func inputBox(value string, width int, focused bool) string {
	if width < 6 {
		width = 6
	}
	if focused {
		value += cursorCell()
	}
	box := styleBox.Width(width).MaxWidth(width)
	if focused {
		box = box.BorderForeground(colorCyan)
	}
	// width less the two border columns and the two padding columns.
	return box.Render(truncCells(value, width-4))
}

// BoxForStatus renders a component name inside a styled box.
func BoxForStatus(name string, s model.Status, active bool) string {
	return BoxForStatusZoom(name, s, active, 1)
}

// BoxForStatusZoom is BoxForStatus with zoom-dependent padding.
func BoxForStatusZoom(name string, s model.Status, active bool, zoom float64) string {
	return BoxHover(name, s, active, zoom, false)
}

// BoxHover is a status box, optionally highlighted for mouse hover.
func BoxHover(name string, s model.Status, active bool, zoom float64, hover bool) string {
	st := StyleForStatus(s, active)
	pv, ph := zoomPad(zoom)
	fg := st.GetForeground()
	box := styleBox.BorderForeground(fg).Padding(pv, ph)
	label := st.Render(name)
	if hover {
		box = box.BorderForeground(colorCyan).Bold(true)
		label = lipgloss.NewStyle().Foreground(colorCyan).Reverse(true).Bold(true).Render(name)
	}
	return box.Render(label)
}
