package ui

import (
	"fmt"
	"strings"
	"time"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/nutanix/ntnx-topo/internal/model"
)

type visMode int

const (
	visBasic visMode = iota
	visDatacenter
)

func renderVisHeader(mode visMode, zoom float64) string {
	basic := styleTab.Render("basic")
	dc := styleTab.Render("datacenter")
	if mode == visBasic {
		basic = styleTabActive.Render("basic")
	} else {
		dc = styleTabActive.Render("datacenter")
	}
	z := styleHelp.Render(fmt.Sprintf("  %d%%", int(zoom*100)))
	return stylePaneTitle.Render("Topology") + "\n" + basic + " " + dc + z
}

func tabLabelStart(mode visMode) (basicStart, dcStart, basicW, dcW int) {
	basicW = lipgloss.Width(styleTab.Render("basic"))
	dcW = lipgloss.Width(styleTab.Render("datacenter"))
	if mode == visBasic {
		basicW = lipgloss.Width(styleTabActive.Render("basic"))
	} else {
		dcW = lipgloss.Width(styleTabActive.Render("datacenter"))
	}
	basicStart = 0
	dcStart = basicW + 1
	return basicStart, dcStart, basicW, dcW
}

// RenderVisualise draws the right-pane canvas (not yet windowed).
func RenderVisualise(state model.StateSnapshot, mode visMode, zoom float64, hoveredID string) artPiece {
	if state.Cluster.Kind == model.KindPC {
		if mode == visBasic {
			return RenderPCGraph(state, zoom, hoveredID)
		}
		return RenderDatacenter(state.Cluster, state.ActiveFetch, zoom, hoveredID)
	}
	if mode == visDatacenter {
		return RenderDatacenter(state.Cluster, state.ActiveFetch, zoom, hoveredID)
	}
	return RenderTopology(state, zoom, hoveredID)
}

// RenderTopology draws the PE CVM/host/switch diagram grouped by physical switch.
func RenderTopology(state model.StateSnapshot, zoom float64, hoveredID string) artPiece {
	active := state.ActiveFetch
	cluster := state.Cluster

	title := textPiece(styleTitle.Render(fmt.Sprintf("  %s  ", cluster.Name)))
	if cluster.Kind == model.KindPC {
		return RenderPCOverview(cluster, active, zoom, hoveredID)
	}

	racks := cluster.Racks()
	if len(racks) == 0 {
		return joinV([]artPiece{title, textPiece(styleHelp.Render("no hosts"))}, false)
	}

	boxGap, groupGap := zoomGaps(zoom)
	groups := make([]artPiece, 0, len(racks))
	for _, rack := range racks {
		groups = append(groups, renderBasicSwitchGroup(rack, cluster.ID, active, zoom, boxGap, hoveredID))
	}
	row := joinH(groups, groupGap)
	return joinV([]artPiece{title, row}, false)
}

func renderBasicSwitchGroup(rack model.Rack, clusterID string, active bool, zoom float64, boxGap string, hoveredID string) artPiece {
	n := len(rack.Hosts)
	if n == 0 {
		return artPiece{}
	}

	cvmBoxes := make([]artPiece, n)
	hostBoxes := make([]artPiece, n)
	for i, h := range rack.Hosts {
		if i < len(rack.CVMs) {
			cvmBoxes[i] = nodeBox(rack.CVMs[i], string(model.EntityCVM), clusterID, active, zoom, hoveredID)
		} else {
			cvmBoxes[i] = labeledBox(
				fmt.Sprintf("cvm-placeholder-%d", i),
				string(model.EntityCVM),
				clusterID,
				fmt.Sprintf("CVM %d", i+1),
				"",
				h.Switch,
				model.StatusUnknown,
				active,
				zoom,
				hoveredID,
			)
		}
		hostBoxes[i] = nodeBox(h, string(model.EntityHost), clusterID, active, zoom, hoveredID)
	}

	cvmRow := joinH(cvmBoxes, boxGap)
	hostRow := joinH(hostBoxes, boxGap)
	swName := model.DisplaySwitch(rack.Switch)
	swBox := labeledBox(
		"switch:"+swName,
		string(model.EntitySwitch),
		clusterID,
		swName,
		"",
		swName,
		model.StatusUp,
		active,
		zoom,
		hoveredID,
	)

	groupW, _ := cvmRow.Size()
	if w, _ := hostRow.Size(); w > groupW {
		groupW = w
	}
	if w, _ := swBox.Size(); w > groupW {
		groupW = w
	}

	return joinV([]artPiece{
		cvmRow,
		textPiece(renderVerticalPipes(n, groupW)),
		hostRow,
		textPiece(renderConnectorDown(n, groupW)),
		swBox,
	}, true)
}

// RenderPCOverview draws a PC box with PE cluster boxes beneath it.
func RenderPCOverview(cluster model.Cluster, active bool, zoom float64, hoveredID string) artPiece {
	title := textPiece(styleTitle.Render(fmt.Sprintf("  %s  ", cluster.Name)))

	pc := cluster.PC()
	pcName := cluster.Name
	pcStatus := model.StatusUnknown
	pcID := cluster.ID
	pcIP := ""
	if pc != nil {
		pcStatus = pc.Status
		if pc.Name != "" {
			pcName = pc.Name
		}
		pcID = pc.ID
		pcIP = pc.IP
	}
	pcBox := labeledBox(pcID, string(model.EntityPC), pcID, pcName, pcIP, "", pcStatus, active, zoom, hoveredID)

	pes := cluster.PEs()
	if len(pes) == 0 {
		return joinV([]artPiece{title, pcBox, textPiece(styleHelp.Render("no Prism Element clusters"))}, true)
	}

	_, groupGap := zoomGaps(zoom)
	peBoxes := make([]artPiece, len(pes))
	for i, pe := range pes {
		peBoxes[i] = nodeBox(pe, string(model.EntityPE), pe.ID, active, zoom, hoveredID)
	}
	peRow := joinH(peBoxes, groupGap)
	peW, _ := peRow.Size()
	conn := textPiece(renderConnectorDown(len(pes), peW))
	return joinV([]artPiece{title, pcBox, conn, peRow}, true)
}

func renderFooter(state model.StateSnapshot, termWidth int, insecureTLS bool) string {
	apiLabel := styleMetrics.Render(fmt.Sprintf("API: %s", state.APIVersion))

	refreshAgo := "never"
	if !state.LastRefresh.IsZero() {
		ago := time.Since(state.LastRefresh).Truncate(time.Second)
		refreshAgo = fmt.Sprintf("%s ago", ago)
	}
	refreshLabel := styleMetrics.Render(fmt.Sprintf("Last refresh: %s", refreshAgo))
	latencyLabel := styleMetrics.Render(fmt.Sprintf("Latency: %s", state.LastLatency.Truncate(time.Millisecond)))

	fetchIndicator := ""
	if state.ActiveFetch {
		fetchIndicator = styleYellow.Render(" ● fetching...")
	}

	metrics := fmt.Sprintf("  %s  │  %s  │  %s%s", apiLabel, refreshLabel, latencyLabel, fetchIndicator)
	if insecureTLS {
		metrics += styleRed.Render("  │  TLS: unverified")
	}

	var parts []string
	parts = append(parts, metrics)

	if len(state.Errors) > 0 {
		lastErr := state.Errors[len(state.Errors)-1]
		if termWidth > 8 && len(lastErr) > termWidth-4 {
			lastErr = lastErr[:termWidth-7] + "..."
		}
		parts = append(parts, styleRed.Render(fmt.Sprintf("  ⚠ %s", lastErr)))
	}

	help := styleHelp.Render("  ↑↓ select  •  click node Config  •  empty click close  •  L commands  •  r  •  q")
	parts = append(parts, help)
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func renderConnectorDown(n int, termWidth int) string {
	if n == 0 {
		return ""
	}

	connector := styleConnector.Render("│")
	if n == 1 {
		return lipgloss.Place(termWidth, 0, lipgloss.Center, lipgloss.Top, connector)
	}

	parts := make([]string, n)
	for i := range n {
		if i == 0 {
			parts[i] = styleConnector.Render("┌──")
		} else if i == n-1 {
			parts[i] = styleConnector.Render("──┐")
		} else {
			parts[i] = styleConnector.Render("──┼──")
		}
	}

	topLine := strings.Join(parts, "")
	bottomParts := make([]string, n)
	spacing := "     "
	for i := range n {
		bottomParts[i] = connector
	}
	bottomLine := strings.Join(interleaveStr(bottomParts, spacing), "")

	result := lipgloss.JoinVertical(lipgloss.Center,
		lipgloss.Place(termWidth, 0, lipgloss.Center, lipgloss.Top, topLine),
		lipgloss.Place(termWidth, 0, lipgloss.Center, lipgloss.Top, bottomLine),
	)
	return result
}

func renderVerticalPipes(n int, termWidth int) string {
	if n == 0 {
		return ""
	}
	connector := styleConnector.Render("│")
	pipes := make([]string, n)
	for i := range n {
		pipes[i] = connector
	}
	line := strings.Join(interleaveStr(pipes, "     "), "")
	return lipgloss.Place(termWidth, 0, lipgloss.Center, lipgloss.Top, line)
}

func interleave(items []string, sep string) []string {
	if len(items) <= 1 {
		return items
	}
	out := make([]string, 0, len(items)*2-1)
	for i, item := range items {
		if i > 0 {
			out = append(out, sep)
		}
		out = append(out, item)
	}
	return out
}

func interleaveStr(items []string, sep string) []string {
	if len(items) <= 1 {
		return items
	}
	out := make([]string, 0, len(items)*2-1)
	for i, item := range items {
		if i > 0 {
			out = append(out, sep)
		}
		out = append(out, item)
	}
	return out
}
