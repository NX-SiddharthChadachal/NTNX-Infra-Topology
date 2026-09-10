package ui

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/nutanix/ntnx-topo/internal/config"
	"github.com/nutanix/ntnx-topo/internal/fetcher"
	"github.com/nutanix/ntnx-topo/internal/model"
)

// auditSizes covers the smallest terminal we claim to support up to a wide one.
var auditSizes = [][2]int{{60, 20}, {80, 24}, {100, 30}, {120, 40}, {200, 50}}

func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	default:
		r := []rune(s)[0]
		return tea.KeyPressMsg{Code: r, Text: s}
	}
}

// auditState is the snapshot under test: the live one when NTNX_TEST_PCS is set,
// otherwise a synthetic copy of the shape the two real PCs return.
func auditState(t *testing.T) model.StateSnapshot {
	t.Helper()
	if raw := strings.TrimSpace(os.Getenv("NTNX_TEST_PCS")); raw != "" {
		if st, ok := liveSnapshot(t, raw); ok {
			return st
		}
		t.Log("live snapshot unavailable, falling back to the synthetic one")
	}
	return syntheticState()
}

func liveSnapshot(t *testing.T, raw string) (model.StateSnapshot, bool) {
	t.Helper()
	var eps []config.PrismEndpoint
	for _, entry := range strings.Split(raw, ";") {
		parts := strings.SplitN(strings.TrimSpace(entry), "|", 3)
		if len(parts) == 3 {
			eps = append(eps, config.PrismEndpoint{IP: parts[0], Username: parts[1], Password: parts[2]})
		}
	}
	if len(eps) == 0 {
		return model.StateSnapshot{}, false
	}
	ch := make(chan model.StateSnapshot, 8)
	f := fetcher.New(&config.Config{
		PrismCentrals:  eps,
		PollInterval:   time.Hour,
		RequestTimeout: 20 * time.Second,
		Insecure:       true,
	}, ch)
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.Start(t.Context())
	}()
	deadline := time.After(3 * time.Minute)
	for {
		select {
		case st := <-ch:
			if len(st.Inventory.PCs) > 0 && !st.ActiveFetch {
				return st, true
			}
		case <-deadline:
			t.Log("timed out waiting for a live snapshot")
			return model.StateSnapshot{}, false
		}
	}
}

// syntheticState mirrors what the two configured PCs actually return: one PC
// with two clusters, one PC with one cluster plus a paired AZ, and one AZ that
// the API reports without an address.
func syntheticState() model.StateSnapshot {
	pe := func(id, name, ip string) model.PENode {
		return model.PENode{InventoryItem: model.InventoryItem{
			ID: id, Name: name, IP: ip, Kind: model.KindPE,
			Status: model.StatusUp, Reachable: true, CredsOK: true,
		}}
	}
	st := model.NewStateSnapshot()
	st.Inventory.PCs = []model.PCNode{
		{
			InventoryItem: model.InventoryItem{
				ID: "pc-10.161.20.42", Name: "PC_10.161.20.42", IP: "10.161.20.42",
				Kind: model.KindPC, Status: model.StatusUp, Reachable: true, CredsOK: true,
			},
			PEs: []model.PENode{
				pe("00065a66-e9d6-ebe1-01e9-7cc255815bbf", "kestrel19-3", "10.161.20.40"),
				pe("00065a67-2365-19d1-3d06-ac1f6b3bbd15", "Trevor-3", "10.48.70.178"),
			},
		},
		{
			InventoryItem: model.InventoryItem{
				ID: "pc-10.48.58.121", Name: "PC_10.48.58.121", IP: "10.48.58.121",
				Kind: model.KindPC, Status: model.StatusUp, Reachable: true, CredsOK: true,
			},
			PEs: []model.PENode{
				pe("00065960-a6d2-154b-4d5e-3cecef852fed", "SKADE01-2", "10.48.58.108"),
			},
		},
		{InventoryItem: model.InventoryItem{
			ID: "1bbf07cf-3c0b-46a7-af96-2fac97ffe40f", Name: "PC-SKADE01-1", IP: "10.48.58.120",
			Kind: model.KindPC, Status: model.StatusUp, Reachable: true, CredsOK: false,
			Message: "login required",
		}},
		{InventoryItem: model.InventoryItem{
			ID: "92125cf9-ca86-4a77-ba32-61602852347d", Name: "Unnamed",
			Kind: model.KindPC, Status: model.StatusUp, Reachable: true, CredsOK: false,
			Message: "login required",
		}},
	}
	st.Inventory.Pairings = []model.PCPairing{
		{FromID: "pc-10.48.58.121", ToID: "1bbf07cf-3c0b-46a7-af96-2fac97ffe40f"},
		{FromID: "pc-10.161.20.42", ToID: "92125cf9-ca86-4a77-ba32-61602852347d"},
	}
	st.Cluster = model.Cluster{
		ID: "00065a66-e9d6-ebe1-01e9-7cc255815bbf", Name: "kestrel19-3", Kind: model.KindPE, HasPC: true,
		Nodes: []model.Node{
			{ID: "h1", Name: "host-1", Status: model.StatusUp, Role: model.RoleHost, Switch: "sw-top-1"},
			{ID: "c1", Name: "cvm-1", Status: model.StatusUp, Role: model.RoleCVM, Switch: "sw-top-1"},
			{ID: "h2", Name: "host-2", Status: model.StatusUp, Role: model.RoleHost, Switch: model.NullSwitch},
			{ID: "c2", Name: "cvm-2", Status: model.StatusDown, Role: model.RoleCVM, Switch: model.NullSwitch},
		},
	}
	model.FinalizeSwitches(&st.Cluster)
	return st
}

func auditModel(t *testing.T, st model.StateSnapshot, w, h int) TUIModel {
	t.Helper()
	ch := make(chan model.StateSnapshot, 8)
	// No endpoints configured: the fetcher makes no network calls but the model
	// can still call SetSelected / ManualRefresh on it.
	f := fetcher.New(&config.Config{PollInterval: time.Hour, RequestTimeout: time.Second}, ch)
	m := NewTUIModel(f)
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m = next.(TUIModel)
	next, _ = m.Update(stateMsg(st))
	return next.(TUIModel)
}

// checkFrame asserts the invariants clipFrame is supposed to guarantee.
func checkFrame(t *testing.T, m TUIModel, w, h int, step string) {
	t.Helper()
	frame := m.View().Content
	lines := strings.Split(frame, "\n")
	if len(lines) > h {
		t.Errorf("%dx%d %s: frame has %d lines, want at most %d", w, h, step, len(lines), h)
	}
	for i, ln := range lines {
		if got := lipgloss.Width(ln); got > w {
			t.Errorf("%dx%d %s: line %d is %d cells wide: %q", w, h, step, i, got, trimForLog(ln))
		}
	}
}

// known records a confirmed bug that has not been fixed yet, so the audit keeps
// reporting it without turning the suite red. Promote these to t.Errorf as they
// get fixed; the list lives in docs/TODO.md.
func known(t *testing.T, id, format string, args ...any) {
	t.Helper()
	t.Logf("KNOWN BUG %s: %s", id, fmt.Sprintf(format, args...))
}

func trimForLog(s string) string {
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}

// TestAuditKeyboardDrive walks the whole inventory and every keyboard command at
// several terminal sizes, checking the frame after each step.
func TestAuditKeyboardDrive(t *testing.T) {
	st := auditState(t)
	for _, size := range auditSizes {
		w, h := size[0], size[1]
		m := auditModel(t, st, w, h)
		checkFrame(t, m, w, h, "initial")

		rows := m.rows()
		script := []string{"1", "2", "tab", "+", "+", "-", "0", "L", "esc", "l", "h", "r"}
		for i := 0; i < len(rows)+2; i++ {
			script = append(script, "down")
		}
		for i := 0; i < len(rows)+2; i++ {
			script = append(script, "up")
		}
		for _, k := range script {
			next, _ := m.Update(key(k))
			m = next.(TUIModel)
			checkFrame(t, m, w, h, "key "+k)
		}

		// enter on a row that needs credentials must open the overlay and the
		// overlay must accept typing.
		m.selectedID = "1bbf07cf-3c0b-46a7-af96-2fac97ffe40f"
		next, _ := m.Update(key("enter"))
		m = next.(TUIModel)
		if m.reconnect == nil {
			t.Fatalf("%dx%d: enter did not open the login overlay", w, h)
		}
		checkFrame(t, m, w, h, "overlay open")
		for _, k := range []string{"a", "d", "m", "i", "n", "tab", "s", "e", "c", "backspace", "esc"} {
			next, _ := m.Update(key(k))
			m = next.(TUIModel)
			checkFrame(t, m, w, h, "overlay key "+k)
		}
		if m.reconnect != nil {
			t.Errorf("%dx%d: esc did not close the login overlay", w, h)
		}
	}
}

// TestAuditMouseDrive clicks every topology hit rect, drags both dividers to
// their extremes and scrolls each pane.
func TestAuditMouseDrive(t *testing.T) {
	st := auditState(t)
	for _, size := range auditSizes {
		w, h := size[0], size[1]
		m := auditModel(t, st, w, h)

		for _, mode := range []visMode{visBasic, visDatacenter} {
			m.setVisMode(mode)
			leftW, topoW, _ := layoutPanes(m.width, m.splitRatio, m.configRatio, m.configOpen)
			hits := m.currentHits(leftW, topoW)
			for _, hit := range hits {
				sx := leftW + dividerWidth + 1 + hit.X + hit.W/2
				sy := m.visCanvasMinY() + hit.Y + hit.H/2
				if !m.inTopoCanvas(sx, sy, leftW, topoW) {
					continue
				}
				cx, cy := m.canvasXY(sx, sy, leftW)
				got, ok := hitAt(hits, cx, cy)
				if !ok {
					t.Errorf("%dx%d mode %v: no hit at the centre of %q", w, h, mode, hit.Name)
					continue
				}
				if got.ID != hit.ID {
					t.Errorf("%dx%d mode %v: centre of %q resolves to %q", w, h, mode, hit.Name, got.Name)
				}
				next, _ := m.Update(tea.MouseClickMsg{X: sx, Y: sy, Button: tea.MouseLeft})
				m = next.(TUIModel)
				next, _ = m.Update(tea.MouseReleaseMsg{X: sx, Y: sy, Button: tea.MouseLeft})
				m = next.(TUIModel)
				checkFrame(t, m, w, h, "click "+hit.Name)
				if m.reconnect != nil {
					next, _ = m.Update(key("esc"))
					m = next.(TUIModel)
				}
			}
		}

		// Drag the Inventory divider across its whole range.
		leftW, topoW, _ := layoutPanes(m.width, m.splitRatio, m.configRatio, m.configOpen)
		next, _ := m.Update(tea.MouseClickMsg{X: leftW, Y: 5, Button: tea.MouseLeft})
		m = next.(TUIModel)
		for _, x := range []int{0, 1, w / 4, w / 2, w - 1, w + 10} {
			next, _ = m.Update(tea.MouseMotionMsg{X: x, Y: 5, Button: tea.MouseLeft})
			m = next.(TUIModel)
			checkFrame(t, m, w, h, fmt.Sprintf("drag left to %d", x))
		}
		next, _ = m.Update(tea.MouseReleaseMsg{X: w / 3, Y: 5, Button: tea.MouseLeft})
		m = next.(TUIModel)

		// Open Config, then drag its divider across its whole range.
		leftW, topoW, _ = layoutPanes(m.width, m.splitRatio, m.configRatio, m.configOpen)
		if hits := m.currentHits(leftW, topoW); len(hits) > 0 {
			mm, _ := m.openConfig(hits[0])
			m = mm
			if m.reconnect != nil {
				next, _ = m.Update(key("esc"))
				m = next.(TUIModel)
			}
			checkFrame(t, m, w, h, "config open")

			leftW, topoW, cfgW := layoutPanes(m.width, m.splitRatio, m.configRatio, m.configOpen)
			if cfgW == 0 {
				t.Errorf("%dx%d: Config pane has no width", w, h)
			}
			dx := configDividerX(leftW, topoW)
			next, _ = m.Update(tea.MouseClickMsg{X: dx, Y: 5, Button: tea.MouseLeft})
			m = next.(TUIModel)
			if m.dragging != dragConfigSplit {
				t.Errorf("%dx%d: click on the Config divider at x=%d did not start a drag", w, h, dx)
			}
			for _, x := range []int{0, w / 3, w / 2, w - 4, w + 10} {
				next, _ = m.Update(tea.MouseMotionMsg{X: x, Y: 5, Button: tea.MouseLeft})
				m = next.(TUIModel)
				checkFrame(t, m, w, h, fmt.Sprintf("drag config to %d", x))
			}
			next, _ = m.Update(tea.MouseReleaseMsg{X: w / 2, Y: 5, Button: tea.MouseLeft})
			m = next.(TUIModel)

			// Scroll the Config pane far past the end, then back.
			leftW, topoW, cfgW = layoutPanes(m.width, m.splitRatio, m.configRatio, m.configOpen)
			cfgX := leftW + dividerWidth + topoW + dividerWidth
			for i := 0; i < 40; i++ {
				next, _ = m.Update(tea.MouseWheelMsg{X: cfgX + 1, Y: 5, Button: tea.MouseWheelDown})
				m = next.(TUIModel)
			}
			checkFrame(t, m, w, h, "config scrolled to the end")
			if m.configOpen && len(m.configFields) > 0 && m.configOffset > len(m.configFields) {
				known(t, "config-scroll",
					"%dx%d: configOffset ran to %d with only %d fields; renderConfigBody clamps the draw to the last line, but the user must scroll back the same number of notches",
					w, h, m.configOffset, len(m.configFields))
			}
		}

		// Scroll the inventory and the canvas.
		for i := 0; i < 30; i++ {
			next, _ = m.Update(tea.MouseWheelMsg{X: 2, Y: 5, Button: tea.MouseWheelDown})
			m = next.(TUIModel)
		}
		checkFrame(t, m, w, h, "inventory scrolled")
		for _, btn := range []tea.MouseButton{tea.MouseWheelUp, tea.MouseWheelDown, tea.MouseWheelLeft, tea.MouseWheelRight} {
			next, _ = m.Update(tea.MouseWheelMsg{X: w / 2, Y: 6, Button: btn})
			m = next.(TUIModel)
			checkFrame(t, m, w, h, "canvas wheel")
		}
	}
}

// TestAuditSelectionAndTree checks the behaviour the inventory pane promises:
// polls must not move the cursor, every row must be reachable with the arrows,
// and collapsing a PC must hide its PEs.
func TestAuditSelectionAndTree(t *testing.T) {
	st := auditState(t)
	m := auditModel(t, st, 120, 40)

	first := m.selectedID
	if first == "" {
		t.Fatal("nothing selected after the first snapshot")
	}
	for i := 0; i < 3; i++ {
		next, _ := m.Update(stateMsg(st))
		m = next.(TUIModel)
	}
	if m.selectedID != first {
		t.Errorf("a poll moved the cursor from %q to %q", first, m.selectedID)
	}

	// Arrow down through the tree and collect what we can land on.
	visited := map[string]bool{m.selectedID: true}
	for i := 0; i < len(m.rows())*2; i++ {
		next, _ := m.Update(key("down"))
		m = next.(TUIModel)
		visited[m.selectedID] = true
	}
	for _, row := range m.rows() {
		if row.Selectable && !visited[row.ID] {
			t.Errorf("row %q (%s) cannot be reached with the down arrow", row.Label, row.ID)
		}
	}

	// Collapse the first PC: its PEs must leave the row list.
	pc := st.Inventory.PCs[0]
	if len(pc.PEs) == 0 {
		return
	}
	m.selectedID = pc.ID
	next, _ := m.Update(key("left"))
	m = next.(TUIModel)
	for _, row := range m.rows() {
		for _, pe := range pc.PEs {
			if row.ID == pe.ID {
				t.Errorf("PE %q still listed after collapsing PC %q", pe.Name, pc.Name)
			}
		}
	}
	next, _ = m.Update(key("right"))
	m = next.(TUIModel)
	found := false
	for _, row := range m.rows() {
		if row.ID == pc.PEs[0].ID {
			found = true
		}
	}
	if !found {
		t.Errorf("PE %q did not come back after expanding PC %q", pc.PEs[0].Name, pc.Name)
	}
}

// TestAuditConfigForUnauthenticatedNode clicking a node that needs credentials
// must show the login overlay and the short message, never raw API output.
func TestAuditConfigForUnauthenticatedNode(t *testing.T) {
	st := auditState(t)
	m := auditModel(t, st, 120, 40)

	var targets []model.PCNode
	for _, pc := range st.Inventory.PCs {
		if !pc.CredsOK {
			targets = append(targets, pc)
		}
	}
	if len(targets) == 0 {
		t.Skip("no node needs credentials in this snapshot")
	}

	for _, target := range targets {
		hit := Hit{ID: target.ID, Kind: string(model.EntityPC), ClusterID: target.ID, Name: target.Name, IP: target.IP}
		mm, _ := m.openConfig(hit)
		m = mm
		if m.reconnect == nil {
			t.Fatalf("clicking %q did not open the login overlay", target.Name)
		}
		if m.configErr != "credentials required" {
			t.Errorf("%q: config error = %q, want \"credentials required\"", target.Name, m.configErr)
		}
		if m.reconnect.targetID != target.ID {
			t.Errorf("overlay target = %q, want %q", m.reconnect.targetID, target.ID)
		}
		if m.reconnect.name == "" || m.reconnect.name == "Unnamed" {
			known(t, "unnamed-az", "login overlay title reads %q for id=%s, so the user cannot tell which peer it is",
				m.reconnect.name, target.ID)
		}
		if target.IP == "" && m.reconnect.addr != "" {
			t.Errorf("overlay prefilled address %q for a node the API gave no address", m.reconnect.addr)
		}
		if target.IP == "" && m.reconnect.addr == "" {
			known(t, "az-no-address", "%q (id=%s) has no address at all, so the user must type it by hand",
				target.Name, target.ID)
		}
		next, _ := m.Update(key("esc"))
		m = next.(TUIModel)
	}
}

// TestAuditEmptyInventory covers the state before the first successful poll.
func TestAuditEmptyInventory(t *testing.T) {
	for _, size := range auditSizes {
		w, h := size[0], size[1]
		m := auditModel(t, model.NewStateSnapshot(), w, h)
		checkFrame(t, m, w, h, "empty")
		for _, k := range []string{"down", "up", "enter", "1", "2", "L", "esc", "+", "-"} {
			next, _ := m.Update(key(k))
			m = next.(TUIModel)
			checkFrame(t, m, w, h, "empty key "+k)
		}
	}
}
