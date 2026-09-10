package ui

import (
	"sort"
	"strings"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/nutanix/ntnx-topo/internal/model"
)

type placedPC struct {
	pc     model.PCNode
	box    string
	x, y   int
	w, h   int
	cx, cy int
}

type rect struct {
	x, y, w, h int
}

// graphLabel is a caption drawn straight onto the canvas, such as the heading
// above the standalone section.
type graphLabel struct {
	text string
	x, y int
	w    int
}

type pcLayout struct {
	boxes  []placedPC
	labels []graphLabel
}

// RenderPCGraph draws reachable Prism Centrals / AZs as a connected graph.
// PEs are never included. Used only in the basic Topology tab.
func RenderPCGraph(state model.StateSnapshot, zoom float64, hoveredID string) artPiece {
	pcs := state.Inventory.PCs
	title := textPiece(styleTitle.Render("  PC / AZ graph  "))
	if len(pcs) == 0 {
		return joinV([]artPiece{title, textPiece(styleHelp.Render("no Prism Centrals"))}, false)
	}

	selected := state.Cluster.ID
	edges := reachablePCEdges(state.Inventory)
	layout := layoutPCGraph(pcs, edges, selected, zoom, hoveredID)
	placed, labels := layout.boxes, layout.labels

	minX, minY := placed[0].x, placed[0].y
	maxX, maxY := placed[0].x+placed[0].w, placed[0].y+placed[0].h
	grow := func(x, y, w, h int) {
		if x < minX {
			minX = x
		}
		if y < minY {
			minY = y
		}
		if x+w > maxX {
			maxX = x + w
		}
		if y+h > maxY {
			maxY = y + h
		}
	}
	for _, p := range placed {
		grow(p.x, p.y, p.w, p.h)
	}
	for _, l := range labels {
		grow(l.x, l.y, l.w, 1)
	}
	pad := 2
	w := maxX - minX + pad*2
	h := maxY - minY + pad*2
	if w < 8 {
		w = 8
	}
	if h < 3 {
		h = 3
	}

	byID := map[string]placedPC{}
	blocked := make([]rect, 0, len(placed)+len(labels))
	for i := range placed {
		placed[i].x -= minX - pad
		placed[i].y -= minY - pad
		placed[i].cx = placed[i].x + placed[i].w/2
		placed[i].cy = placed[i].y + placed[i].h/2
		byID[placed[i].pc.ID] = placed[i]
		blocked = append(blocked, rect{placed[i].x, placed[i].y, placed[i].w, placed[i].h})
	}
	for i := range labels {
		labels[i].x -= minX - pad
		labels[i].y -= minY - pad
		blocked = append(blocked, rect{labels[i].x, labels[i].y, labels[i].w, 1})
	}

	c := newRuneCanvas(w, h)
	c.forbid = blocked
	for _, e := range edges {
		a, okA := byID[e.FromID]
		b, okB := byID[e.ToID]
		if !okA || !okB {
			continue
		}
		x1, y1, x2, y2, horizontal := facingPoints(a, b)
		c.route(x1, y1, x2, y2, horizontal)
	}
	c.settleJunctions()
	art := c.String()
	{
		dim := strings.Split(art, "\n")
		for i, ln := range dim {
			dim[i] = styleConnector.Render(ln)
		}
		art = strings.Join(dim, "\n")
	}
	for _, l := range labels {
		art = overlayAt(art, styleHelp.Render(l.text), l.x, l.y)
	}
	var hits []Hit
	for _, p := range placed {
		art = overlayAt(art, p.box, p.x, p.y)
		hits = append(hits, Hit{
			ID:        p.pc.ID,
			Kind:      string(model.EntityPC),
			ClusterID: p.pc.ID,
			Name:      p.pc.Name,
			IP:        p.pc.IP,
			X:         p.x,
			Y:         p.y,
			W:         p.w,
			H:         p.h,
		})
	}

	graph := artPiece{Art: art, Hits: hits}
	hint := textPiece(styleHelp.Render("reachable PCs/AZs linked  •  PEs not shown"))
	return joinV([]artPiece{title, graph, hint}, false)
}

// layoutPCGraph groups PCs by pairing: each set of paired PCs becomes a band of
// BFS columns, and PCs with no pairing get their own row underneath. Layout does
// not depend on the selection, so choosing a PC never reshuffles the drawing and
// an unrelated box can never land between two paired ones.
func layoutPCGraph(pcs []model.PCNode, edges []model.PCPairing, selectedID string, zoom float64, hoveredID string) pcLayout {
	placed := make([]placedPC, len(pcs))
	index := make(map[string]int, len(pcs))
	for i, pc := range pcs {
		st := model.StatusDown
		if pc.Reachable {
			st = pc.Status
			if st == model.StatusUnknown {
				st = model.StatusUp
			}
		}
		label := nodeLabel(pc.Name, pc.IP, zoom)
		if label == "" {
			label = "PC"
		}
		if pc.ID == selectedID {
			label = "▸ " + label
		}
		box := BoxHover(label, st, false, zoom, hoveredID != "" && hoveredID == pc.ID)
		placed[i] = placedPC{pc: pc, box: box, w: lipgloss.Width(box), h: lipgloss.Height(box)}
		index[pc.ID] = i
	}

	gapX, gapY := 10, 4
	if zoom >= 1.5 {
		gapX, gapY = 14, 6
	}
	if zoom <= 0.75 {
		gapX, gapY = 6, 3
	}

	adj := map[string][]string{}
	for _, e := range edges {
		if _, ok := index[e.FromID]; !ok {
			continue
		}
		if _, ok := index[e.ToID]; !ok {
			continue
		}
		adj[e.FromID] = append(adj[e.FromID], e.ToID)
		adj[e.ToID] = append(adj[e.ToID], e.FromID)
	}

	var linked [][][]int
	var alone []int
	visited := map[string]bool{}
	for _, pc := range pcs {
		if visited[pc.ID] {
			continue
		}
		if len(adj[pc.ID]) == 0 {
			visited[pc.ID] = true
			alone = append(alone, index[pc.ID])
			continue
		}
		linked = append(linked, bfsColumns(pc.ID, adj, index, visited))
	}

	var labels []graphLabel
	caption := len(linked) > 0 && len(alone) > 0
	y := 0
	if caption {
		labels = append(labels, newGraphLabel("paired", 0, y))
		y += 2
	}
	for _, columns := range linked {
		y = placeBand(placed, columns, y, gapX, gapY) + gapY
	}
	if len(alone) > 0 {
		if caption {
			labels = append(labels, newGraphLabel("not paired", 0, y))
			y += 2
		}
		placeRow(placed, alone, y, gapX)
	}
	return pcLayout{boxes: placed, labels: labels}
}

func newGraphLabel(text string, x, y int) graphLabel {
	return graphLabel{text: text, x: x, y: y, w: lipgloss.Width(text)}
}

// bfsColumns walks one pairing component and returns its members grouped by
// distance from the start, so column i only links to columns i-1 and i+1.
func bfsColumns(start string, adj map[string][]string, index map[string]int, visited map[string]bool) [][]int {
	visited[start] = true
	frontier := []string{start}
	var columns [][]int
	for len(frontier) > 0 {
		col := make([]int, 0, len(frontier))
		var next []string
		for _, id := range frontier {
			col = append(col, index[id])
			for _, peer := range adj[id] {
				if visited[peer] {
					continue
				}
				visited[peer] = true
				next = append(next, peer)
			}
		}
		sort.Ints(col)
		columns = append(columns, col)
		frontier = next
	}
	return columns
}

// placeBand lays columns out left to right, each column centred vertically in
// the band, and returns the y just below the band.
func placeBand(placed []placedPC, columns [][]int, top, gapX, gapY int) int {
	colW := make([]int, len(columns))
	colH := make([]int, len(columns))
	bandH := 0
	for i, col := range columns {
		for _, k := range col {
			colH[i] += placed[k].h
			if placed[k].w > colW[i] {
				colW[i] = placed[k].w
			}
		}
		if len(col) > 1 {
			colH[i] += gapY * (len(col) - 1)
		}
		if colH[i] > bandH {
			bandH = colH[i]
		}
	}
	x := 0
	for i, col := range columns {
		y := top + (bandH-colH[i])/2
		for _, k := range col {
			placed[k].x = x + (colW[i]-placed[k].w)/2
			placed[k].y = y
			y += placed[k].h + gapY
		}
		x += colW[i] + gapX
	}
	return top + bandH
}

// placeRow spreads nodes horizontally on one line, used for the PCs that have
// no pairing to draw.
func placeRow(placed []placedPC, ids []int, top, gapX int) {
	x := 0
	for _, k := range ids {
		placed[k].x = x
		placed[k].y = top
		x += placed[k].w + gapX
	}
}

func reachablePCEdges(inv model.Inventory) []model.PCPairing {
	byID := map[string]model.PCNode{}
	for _, pc := range inv.PCs {
		byID[pc.ID] = pc
	}
	seen := map[string]bool{}
	var out []model.PCPairing
	for _, e := range inv.Pairings {
		a, b := e.FromID, e.ToID
		pa, okA := byID[a]
		pb, okB := byID[b]
		if !okA || !okB || !pa.Reachable || !pb.Reachable {
			continue
		}
		if a > b {
			a, b = b, a
		}
		k := a + "\x00" + b
		if a == b || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, model.PCPairing{FromID: a, ToID: b})
	}
	return out
}

type runeCanvas struct {
	w, h   int
	cells  [][]rune
	forbid []rect
}

func newRuneCanvas(w, h int) *runeCanvas {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	cells := make([][]rune, h)
	for i := range cells {
		row := make([]rune, w)
		for j := range row {
			row[j] = ' '
		}
		cells[i] = row
	}
	return &runeCanvas{w: w, h: h, cells: cells}
}

func (c *runeCanvas) blocked(x, y int) bool {
	for _, p := range c.forbid {
		if x >= p.x && x < p.x+p.w && y >= p.y && y < p.y+p.h {
			return true
		}
	}
	return false
}

func (c *runeCanvas) set(x, y int, r rune) {
	if x < 0 || y < 0 || x >= c.w || y >= c.h {
		return
	}
	if c.blocked(x, y) {
		return
	}
	c.cells[y][x] = mergeLine(c.cells[y][x], r)
}

func facingPoints(a, b placedPC) (x1, y1, x2, y2 int, horizontal bool) {
	dx := b.cx - a.cx
	dy := b.cy - a.cy
	if absInt(dx) >= absInt(dy) {
		if dx >= 0 {
			x1 = a.x + a.w
			x2 = b.x - 1
		} else {
			x1 = a.x - 1
			x2 = b.x + b.w
		}
		y1, y2 = a.cy, b.cy
		return x1, y1, x2, y2, true
	}
	if dy >= 0 {
		y1 = a.y + a.h
		y2 = b.y - 1
	} else {
		y1 = a.y - 1
		y2 = b.y + b.h
	}
	x1, x2 = a.cx, b.cx
	return x1, y1, x2, y2, false
}

// route draws an elbow that turns halfway between the two boxes, so the long
// run stays inside the empty gap and never tracks along an unrelated box.
func (c *runeCanvas) route(x1, y1, x2, y2 int, horizontal bool) {
	if x1 == x2 || y1 == y2 {
		c.ortho(x1, y1, x2, y2)
		return
	}
	if horizontal {
		mid := (x1 + x2) / 2
		c.hline(y1, x1, mid)
		c.vline(mid, y1, y2)
		c.hline(y2, mid, x2)
		return
	}
	mid := (y1 + y2) / 2
	c.vline(x1, y1, mid)
	c.hline(mid, x1, x2)
	c.vline(x2, mid, y2)
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func (c *runeCanvas) ortho(x1, y1, x2, y2 int) {
	if x1 == x2 {
		c.vline(x1, y1, y2)
		return
	}
	if y1 == y2 {
		c.hline(y1, x1, x2)
		return
	}
	c.hline(y1, x1, x2)
	c.vline(x2, y1, y2)
}

func (c *runeCanvas) hline(y, x1, x2 int) {
	if x2 < x1 {
		x1, x2 = x2, x1
	}
	for x := x1; x <= x2; x++ {
		c.set(x, y, '─')
	}
}

func (c *runeCanvas) vline(x, y1, y2 int) {
	if y2 < y1 {
		y1, y2 = y2, y1
	}
	for y := y1; y <= y2; y++ {
		c.set(x, y, '│')
	}
}

func (c *runeCanvas) isLine(x, y int) bool {
	if x < 0 || y < 0 || x >= c.w || y >= c.h {
		return false
	}
	return isBoxDraw(c.cells[y][x])
}

func isBoxDraw(r rune) bool {
	switch r {
	case '─', '│', '┌', '┐', '└', '┘', '├', '┤', '┬', '┴', '┼':
		return true
	}
	return false
}

func (c *runeCanvas) settleJunctions() {
	next := make([][]rune, c.h)
	for y := 0; y < c.h; y++ {
		next[y] = make([]rune, c.w)
		copy(next[y], c.cells[y])
		for x := 0; x < c.w; x++ {
			if !isBoxDraw(c.cells[y][x]) {
				continue
			}
			n := c.isLine(x, y-1)
			e := c.isLine(x+1, y)
			s := c.isLine(x, y+1)
			w := c.isLine(x-1, y)
			if r := junctionRune(n, e, s, w); r != 0 {
				next[y][x] = r
			}
		}
	}
	c.cells = next
}

func junctionRune(n, e, s, w bool) rune {
	switch {
	case n && e && s && w:
		return '┼'
	case e && s && w:
		return '┬'
	case n && e && w:
		return '┴'
	case n && s && w:
		return '┤'
	case n && e && s:
		return '├'
	case n && s:
		return '│'
	case e && w:
		return '─'
	case s && e:
		return '┌'
	case s && w:
		return '┐'
	case n && e:
		return '└'
	case n && w:
		return '┘'
	case n, s:
		return '│'
	case e, w:
		return '─'
	default:
		return 0
	}
}

func (c *runeCanvas) String() string {
	lines := make([]string, c.h)
	for i, row := range c.cells {
		lines[i] = string(row)
	}
	return strings.Join(lines, "\n")
}

func mergeLine(cur, add rune) rune {
	if cur == ' ' || cur == 0 {
		return add
	}
	if add == ' ' {
		return cur
	}
	if cur == add {
		return cur
	}
	h := cur == '─' || add == '─' || cur == '┼' || add == '┼'
	v := cur == '│' || add == '│' || cur == '┼' || add == '┼'
	if h && v {
		return '┼'
	}
	return add
}
