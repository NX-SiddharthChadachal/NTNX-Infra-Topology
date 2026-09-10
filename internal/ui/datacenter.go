package ui

import (
	"fmt"

	"github.com/nutanix/ntnx-topo/internal/model"
)

// RenderDatacenter draws imaginary racks keyed by physical switch.
func RenderDatacenter(cluster model.Cluster, active bool, zoom float64, hoveredID string) artPiece {
	title := textPiece(styleTitle.Render(fmt.Sprintf("  %s  ", cluster.Name)))
	boxGap, groupGap := zoomGaps(zoom)

	if cluster.Kind == model.KindPC {
		pes := cluster.PEs()
		if len(pes) == 0 {
			return joinV([]artPiece{title, textPiece(styleHelp.Render("no PE clusters"))}, false)
		}
		racks := make([]artPiece, 0, len(pes))
		for _, pe := range pes {
			racks = append(racks, renderRack("PE "+pe.Name, pe.Name, []model.Node{pe}, nil, cluster.ID, active, zoom, boxGap, hoveredID, true))
		}
		row := joinH(racks, groupGap)
		return joinV([]artPiece{title, row}, false)
	}

	groups := cluster.Racks()
	if len(groups) == 0 {
		return joinV([]artPiece{title, textPiece(styleHelp.Render("no hosts"))}, false)
	}

	racks := make([]artPiece, 0, len(groups))
	for i, g := range groups {
		label := fmt.Sprintf("Rack %d", i+1)
		racks = append(racks, renderRack(label, g.Switch, g.Hosts, g.CVMs, cluster.ID, active, zoom, boxGap, hoveredID, false))
	}
	row := joinH(racks, groupGap)
	return joinV([]artPiece{title, row}, false)
}

func renderRack(label, switchName string, hosts, cvms []model.Node, clusterID string, active bool, zoom float64, boxGap string, hoveredID string, peRack bool) artPiece {
	header := textPiece(styleHelp.Render(label))
	sw := model.DisplaySwitch(switchName)
	var swBox artPiece
	if peRack && len(hosts) == 1 && hosts[0].Role == model.RolePE {
		pe := hosts[0]
		swBox = labeledBox(pe.ID, string(model.EntityPE), pe.ID, pe.Name, pe.IP, "", pe.Status, active, zoom, hoveredID)
	} else {
		swBox = labeledBox("switch:"+sw, string(model.EntitySwitch), clusterID, sw, "", sw, model.StatusUp, active, zoom, hoveredID)
	}

	slots := make([]artPiece, 0, len(hosts))
	for i, h := range hosts {
		kind := kindFromRole(h.Role)
		hostBox := nodeBox(h, kind, clusterID, active, zoom, hoveredID)
		if i < len(cvms) {
			cvmBox := nodeBox(cvms[i], string(model.EntityCVM), clusterID, active, zoom, hoveredID)
			slots = append(slots, joinV([]artPiece{hostBox, textPiece(styleConnector.Render("│")), cvmBox}, true))
		} else {
			slots = append(slots, hostBox)
		}
	}

	var nodeRow artPiece
	if len(slots) > 0 {
		nodeRow = joinH(slots, boxGap)
	} else {
		nodeRow = textPiece(styleHelp.Render("empty"))
	}

	inner := joinV([]artPiece{
		swBox,
		textPiece(styleConnector.Render("│")),
		nodeRow,
	}, true)
	framedArt := styleRack.Render(inner.Art)
	framedHits := offsetHits(inner.Hits, 2, 1)
	framed := artPiece{Art: framedArt, Hits: framedHits}
	return joinV([]artPiece{header, framed}, true)
}
