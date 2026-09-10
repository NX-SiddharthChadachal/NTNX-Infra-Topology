package ui

import (
	"strings"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/nutanix/ntnx-topo/internal/model"
)

// Hit is a clickable rectangle in art-cell coordinates.
type Hit struct {
	ID, Kind   string
	ClusterID  string
	Name, IP   string
	Switch     string
	X, Y, W, H int
}

func (h Hit) Contains(x, y int) bool {
	return x >= h.X && x < h.X+h.W && y >= h.Y && y < h.Y+h.H
}

func (h Hit) Ref() model.EntityRef {
	return model.EntityRef{
		Kind:      model.EntityKind(h.Kind),
		ID:        h.ID,
		ClusterID: h.ClusterID,
		Name:      h.Name,
		IP:        h.IP,
		Switch:    h.Switch,
	}
}

func hitAt(hits []Hit, x, y int) (Hit, bool) {
	for i := len(hits) - 1; i >= 0; i-- {
		if hits[i].Contains(x, y) {
			return hits[i], true
		}
	}
	return Hit{}, false
}

func offsetHits(hits []Hit, dx, dy int) []Hit {
	if len(hits) == 0 {
		return nil
	}
	out := make([]Hit, len(hits))
	for i, h := range hits {
		h.X += dx
		h.Y += dy
		out[i] = h
	}
	return out
}

type artPiece struct {
	Art  string
	Hits []Hit
}

func (p artPiece) Size() (w, h int) {
	return lipgloss.Width(p.Art), lipgloss.Height(p.Art)
}

func blankArt(w, h int) string {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	line := strings.Repeat(" ", w)
	lines := make([]string, h)
	for i := range lines {
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

func joinH(items []artPiece, gap string) artPiece {
	if len(items) == 0 {
		return artPiece{}
	}
	if len(items) == 1 {
		return items[0]
	}
	gapW := lipgloss.Width(gap)
	maxH := 0
	widths := make([]int, len(items))
	for i, it := range items {
		w, h := it.Size()
		widths[i] = w
		if h > maxH {
			maxH = h
		}
	}
	totalW := 0
	for i, w := range widths {
		totalW += w
		if i > 0 {
			totalW += gapW
		}
	}
	if totalW < 1 {
		totalW = 1
	}
	if maxH < 1 {
		maxH = 1
	}
	canvas := blankArt(totalW, maxH)
	var hits []Hit
	x := 0
	for i, it := range items {
		if i > 0 {
			if gap != "" {
				canvas = overlayAt(canvas, gap, x, 0)
			}
			x += gapW
		}
		canvas = overlayAt(canvas, it.Art, x, 0)
		hits = append(hits, offsetHits(it.Hits, x, 0)...)
		x += widths[i]
	}
	return artPiece{Art: canvas, Hits: hits}
}

func joinV(items []artPiece, center bool) artPiece {
	if len(items) == 0 {
		return artPiece{}
	}
	if len(items) == 1 {
		return items[0]
	}
	maxW, totalH := 0, 0
	heights := make([]int, len(items))
	widths := make([]int, len(items))
	for i, it := range items {
		w, h := it.Size()
		widths[i] = w
		heights[i] = h
		if w > maxW {
			maxW = w
		}
		totalH += h
	}
	if maxW < 1 {
		maxW = 1
	}
	if totalH < 1 {
		totalH = 1
	}
	canvas := blankArt(maxW, totalH)
	var hits []Hit
	y := 0
	for i, it := range items {
		x := 0
		if center && widths[i] < maxW {
			x = (maxW - widths[i]) / 2
		}
		canvas = overlayAt(canvas, it.Art, x, y)
		hits = append(hits, offsetHits(it.Hits, x, y)...)
		y += heights[i]
	}
	return artPiece{Art: canvas, Hits: hits}
}

func textPiece(s string) artPiece {
	return artPiece{Art: s}
}

func nodeBox(n model.Node, kind string, clusterID string, active bool, zoom float64, hoveredID string) artPiece {
	if kind == "" {
		kind = kindFromRole(n.Role)
	}
	label := nodeLabel(n.Name, n.IP, zoom)
	if label == "" {
		label = n.Name
	}
	box := BoxHover(label, n.Status, active, zoom, hoveredID != "" && hoveredID == n.ID)
	w, h := lipgloss.Width(box), lipgloss.Height(box)
	return artPiece{
		Art: box,
		Hits: []Hit{{
			ID:        n.ID,
			Kind:      kind,
			ClusterID: clusterID,
			Name:      n.Name,
			IP:        n.IP,
			Switch:    n.Switch,
			W:         w,
			H:         h,
		}},
	}
}

func labeledBox(id, kind, clusterID, name, ip, sw string, st model.Status, active bool, zoom float64, hoveredID string) artPiece {
	label := nodeLabel(name, ip, zoom)
	if label == "" {
		label = name
	}
	box := BoxHover(label, st, active, zoom, hoveredID != "" && hoveredID == id)
	w, h := lipgloss.Width(box), lipgloss.Height(box)
	return artPiece{
		Art: box,
		Hits: []Hit{{
			ID:        id,
			Kind:      kind,
			ClusterID: clusterID,
			Name:      name,
			IP:        ip,
			Switch:    sw,
			W:         w,
			H:         h,
		}},
	}
}

func kindFromRole(r model.NodeRole) string {
	switch r {
	case model.RolePC:
		return string(model.EntityPC)
	case model.RolePE:
		return string(model.EntityPE)
	case model.RoleHost:
		return string(model.EntityHost)
	case model.RoleCVM:
		return string(model.EntityCVM)
	case model.RoleTOR:
		return string(model.EntitySwitch)
	default:
		return string(model.EntityHost)
	}
}

func mapHits(hits []Hit, t vpTransform) []Hit {
	out := make([]Hit, 0, len(hits))
	for _, h := range hits {
		h.X = h.X - t.offX + t.baseX
		h.Y = h.Y - t.offY + t.baseY
		out = append(out, h)
	}
	return out
}
