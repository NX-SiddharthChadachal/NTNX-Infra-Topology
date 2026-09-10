package ui

import (
	"strings"

	"github.com/nutanix/ntnx-topo/internal/model"
)

type inventoryRow struct {
	ID         string
	Kind       model.ClusterKind
	Label      string
	Status     model.Status
	Selectable bool
	Reachable  bool
	CredsOK    bool
	IP         string
	Name       string
	Depth      int
}

func healthSuffix(item model.InventoryItem) string {
	if !item.Reachable {
		return " (unreachable)"
	}
	if !item.CredsOK {
		return " (creds required)"
	}
	return ""
}

func flattenInventory(inv model.Inventory, collapsed map[string]bool) []inventoryRow {
	var rows []inventoryRow
	for _, pc := range inv.PCs {
		glyph := "▼"
		if collapsed[pc.ID] {
			glyph = "▶"
		}
		rows = append(rows, inventoryRow{
			ID:         pc.ID,
			Kind:       model.KindPC,
			Label:      glyph + " " + displayName(pc.InventoryItem) + healthSuffix(pc.InventoryItem),
			Status:     pc.Status,
			Selectable: true,
			Reachable:  pc.Reachable,
			CredsOK:    pc.CredsOK,
			IP:         pc.IP,
			Name:       displayName(pc.InventoryItem),
			Depth:      0,
		})
		if collapsed[pc.ID] {
			continue
		}
		for _, pe := range pc.PEs {
			rows = append(rows, inventoryRow{
				ID:         pe.ID,
				Kind:       model.KindPE,
				Label:      displayName(pe.InventoryItem) + healthSuffix(pe.InventoryItem),
				Status:     pe.Status,
				Selectable: true,
				Reachable:  pe.Reachable,
				CredsOK:    pe.CredsOK,
				IP:         pe.IP,
				Name:       displayName(pe.InventoryItem),
				Depth:      1,
			})
		}
	}
	for _, pe := range inv.PEs {
		rows = append(rows, inventoryRow{
			ID:         pe.ID,
			Kind:       model.KindPE,
			Label:      displayName(pe.InventoryItem) + healthSuffix(pe.InventoryItem),
			Status:     pe.Status,
			Selectable: true,
			Reachable:  pe.Reachable,
			CredsOK:    pe.CredsOK,
			IP:         pe.IP,
			Name:       displayName(pe.InventoryItem),
			Depth:      0,
		})
	}
	return rows
}

func displayName(item model.InventoryItem) string {
	if item.Name != "" {
		return item.Name
	}
	if item.IP != "" {
		return item.IP
	}
	return item.ID
}

func renderInventoryList(rows []inventoryRow, selectedID string, offset, width, height int) string {
	if width < 8 {
		width = 8
	}
	if len(rows) == 0 {
		return styleHelp.Render("no clusters")
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(rows) {
		offset = 0
	}

	var lines []string
	end := offset + height
	if end > len(rows) {
		end = len(rows)
	}
	for _, row := range rows[offset:end] {
		indent := strings.Repeat("  ", row.Depth)
		label := row.Label
		maxLen := width - len(indent) - 2
		if maxLen < 4 {
			maxLen = 4
		}
		runes := []rune(label)
		if len(runes) > maxLen {
			label = string(runes[:maxLen-1]) + "…"
		}

		st := StyleForStatus(row.Status, false)
		if !row.Reachable {
			st = styleGray
		}
		text := indent + label
		if row.ID == selectedID {
			text = styleSelected.Render(text)
		} else {
			text = st.Render(text)
		}
		lines = append(lines, text)
	}
	return strings.Join(lines, "\n")
}

func selectedIndex(rows []inventoryRow, id string) int {
	for i, r := range rows {
		if r.ID == id {
			return i
		}
	}
	return -1
}

func nextSelectable(rows []inventoryRow, from int, dir int) int {
	if len(rows) == 0 {
		return -1
	}
	i := from
	for n := 0; n < len(rows); n++ {
		i += dir
		if i < 0 {
			i = len(rows) - 1
		}
		if i >= len(rows) {
			i = 0
		}
		if rows[i].Selectable {
			return i
		}
	}
	if from >= 0 && from < len(rows) {
		return from
	}
	return 0
}

func ensureVisible(index, offset, height int) int {
	if height <= 0 {
		return 0
	}
	if index < offset {
		return index
	}
	if index >= offset+height {
		return index - height + 1
	}
	return offset
}
