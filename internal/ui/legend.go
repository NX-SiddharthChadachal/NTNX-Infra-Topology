package ui

import (
	"strings"

	lipgloss "charm.land/lipgloss/v2"
)

func renderLegend() string {
	body := strings.Join([]string{
		stylePaneTitle.Render("Commands") + styleHelp.Render("  L close"),
		"",
		styleMetrics.Render("Inventory"),
		legendRow("↑  k", "previous row"),
		legendRow("↓  j", "next row"),
		legendRow("←  h", "collapse PC"),
		legendRow("→  l", "expand PC"),
		legendRow("enter", "log in to selected host"),
		"",
		styleMetrics.Render("Topology"),
		legendRow("1  b", "basic view"),
		legendRow("2  d", "datacenter view"),
		legendRow("tab", "toggle basic / datacenter"),
		legendRow("+  =", "zoom in"),
		legendRow("-", "zoom out"),
		legendRow("0", "reset zoom and pan"),
		legendRow("shift+arrows", "pan canvas"),
		legendRow("wheel", "zoom Topology / scroll list"),
		legendRow("click node", "open Config pane"),
		legendRow("empty click", "close Config pane"),
		legendRow("drag canvas", "pan topology"),
		legendRow("drag ║", "resize any pane"),
		"",
		styleMetrics.Render("App"),
		legendRow("esc", "close overlay / Config / legend"),
		legendRow("r", "refresh"),
		legendRow("q", "quit"),
	}, "\n")
	return styleLegend.Render(body)
}

func legendRow(key, desc string) string {
	k := lipgloss.NewStyle().Foreground(colorCyan).Bold(true).Width(14).Render(key)
	d := styleHelp.Render(desc)
	return k + d
}
