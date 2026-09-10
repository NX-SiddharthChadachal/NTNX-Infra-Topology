package ui

import (
	"strings"
	"testing"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/nutanix/ntnx-topo/internal/model"
)

func TestRenderPaneKeepsBottomBorder(t *testing.T) {
	fields := []model.ConfigField{
		{Key: "kind", Value: "pc"},
		{Key: "name", Value: "PC_10.136.107.235"},
		{Key: "extId", Value: "62d3cc04-9f85-4cc4-bfbc-b5f7ffe59d46"},
	}
	const height = 12
	for _, width := range []int{12, 20, 28, 38, 60} {
		inner := width - 2
		body := renderConfigBody(fields, "no API client for this entity", false, inner, height-3, 0)
		pane := renderPane(stylePaneTitle.Render("Config"), body, width, height)
		if got := lipgloss.Height(pane); got != height {
			t.Errorf("width %d: pane height = %d, want %d", width, got, height)
		}
		lines := strings.Split(pane, "\n")
		last := lines[len(lines)-1]
		if !strings.Contains(last, "╰") || !strings.Contains(last, "╯") {
			t.Errorf("width %d: bottom border missing, last line = %q", width, last)
		}
		if got := lipgloss.Width(pane); got != width {
			t.Errorf("width %d: pane width = %d", width, got)
		}
	}
}

// The Config pane must react to its own drag bar without moving the Inventory
// split, and the bar must be where the layout says it is.
func TestConfigPaneResizes(t *testing.T) {
	const term = 160
	leftW, topoW, cfgW := layoutPanes(term, defaultSplitRatio, defaultConfigRatio, true)
	if x := configDividerX(leftW, topoW); x != leftW+dividerWidth+topoW {
		t.Fatalf("config divider at %d", x)
	}
	if !inDivider(configDividerX(leftW, topoW), configDividerX(leftW, topoW)) {
		t.Fatal("config divider is not a hit target")
	}

	wideLeft, wideTopo, wideCfg := layoutPanes(term, defaultSplitRatio, maxConfigRatio, true)
	if wideCfg <= cfgW {
		t.Errorf("dragging left should widen Config: %d -> %d", cfgW, wideCfg)
	}
	if wideTopo >= topoW {
		t.Errorf("Topology should shrink: %d -> %d", topoW, wideTopo)
	}
	if wideLeft != leftW {
		t.Errorf("Inventory width moved: %d -> %d", leftW, wideLeft)
	}
	if wideLeft+wideTopo+wideCfg+dividerWidth*2 != term {
		t.Errorf("panes do not fill the terminal: %d+%d+%d", wideLeft, wideTopo, wideCfg)
	}
	if narrow := clampFloat(0.01, minConfigRatio, maxConfigRatio); narrow != minConfigRatio {
		t.Errorf("clamp = %v", narrow)
	}
}

func TestConfigBodyLinesFitWidth(t *testing.T) {
	fields := []model.ConfigField{
		{Key: "extId", Value: "62d3cc04-9f85-4cc4-bfbc-b5f7ffe59d46"},
		{Key: "clusterFunction", Value: "PRISM_CENTRAL"},
	}
	for _, width := range []int{6, 10, 18, 26, 36} {
		body := renderConfigBody(fields, "", false, width, 20, 0)
		for i, ln := range strings.Split(body, "\n") {
			if got := lipgloss.Width(ln); got > width {
				t.Errorf("width %d: line %d is %d cells wide: %q", width, i, got, ln)
			}
		}
	}
}
