package ui

import (
	"testing"

	"github.com/nutanix/ntnx-topo/internal/model"
)

// A PC with no pairing must sit in its own band, whichever PC is selected, so it
// can never be drawn between two paired PCs.
func TestLayoutSeparatesUnpairedPCs(t *testing.T) {
	pcs := []model.PCNode{
		{InventoryItem: model.InventoryItem{ID: "a", Name: "PC_A", IP: "10.0.0.1", Kind: model.KindPC, Status: model.StatusUp, Reachable: true}},
		{InventoryItem: model.InventoryItem{ID: "lone", Name: "PC_LONE", IP: "10.0.0.2", Kind: model.KindPC, Status: model.StatusUp, Reachable: true}},
		{InventoryItem: model.InventoryItem{ID: "b", Name: "PC_B", IP: "10.0.0.3", Kind: model.KindPC, Status: model.StatusUp, Reachable: true}},
	}
	edges := []model.PCPairing{{FromID: "a", ToID: "b"}}

	for _, selected := range []string{"a", "lone", "b"} {
		layout := layoutPCGraph(pcs, edges, selected, 1, "")
		box := map[string]placedPC{}
		for _, p := range layout.boxes {
			box[p.pc.ID] = p
		}
		a, b, lone := box["a"], box["b"], box["lone"]
		pairedBottom := max(a.y+a.h, b.y+b.h)
		if lone.y < pairedBottom {
			t.Fatalf("selected %q: unpaired PC at y=%d overlaps the paired band ending at y=%d", selected, lone.y, pairedBottom)
		}
		if a.y != b.y {
			t.Fatalf("selected %q: paired PCs on different rows (%d, %d)", selected, a.y, b.y)
		}
		if len(layout.labels) != 2 {
			t.Fatalf("selected %q: want paired/not paired captions, got %d", selected, len(layout.labels))
		}
	}
}

func TestSettleJunctionsUsesTNotPlusStub(t *testing.T) {
	c := newRuneCanvas(7, 4)
	c.hline(1, 0, 6)
	c.vline(3, 1, 3)
	c.settleJunctions()
	if got := c.cells[1][3]; got != '┬' {
		t.Fatalf("junction rune = %q, want ┬", got)
	}
}

func TestFacingPointsAttachToBoxSides(t *testing.T) {
	a := placedPC{x: 0, y: 0, w: 4, h: 3, cx: 2, cy: 1}
	b := placedPC{x: 10, y: 0, w: 4, h: 3, cx: 12, cy: 1}
	x1, y1, x2, y2, horizontal := facingPoints(a, b)
	if !horizontal {
		t.Fatal("side-by-side boxes should attach horizontally")
	}
	if x1 != 4 || x2 != 9 {
		t.Fatalf("horizontal attach x1,x2 = %d,%d want 4,9", x1, x2)
	}
	if y1 != 1 || y2 != 1 {
		t.Fatalf("horizontal attach y1,y2 = %d,%d want 1,1", y1, y2)
	}
}
