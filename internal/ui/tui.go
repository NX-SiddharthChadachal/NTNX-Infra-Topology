package ui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/nutanix/ntnx-topo/internal/fetcher"
	"github.com/nutanix/ntnx-topo/internal/model"
)

type stateMsg model.StateSnapshot

type tickMsg time.Time

type configLoadedMsg struct {
	Ref    model.EntityRef
	Fields []model.ConfigField
	Err    string
	Auth   bool
}

type TUIModel struct {
	state    model.StateSnapshot
	updateCh <-chan model.StateSnapshot
	fetcher  *fetcher.Fetcher
	width    int
	height   int
	quitting bool

	// insecureTLS drives the footer warning. Certificate verification is a
	// startup choice, so it cannot change while the dashboard is running.
	insecureTLS bool

	selectedID string
	collapsed  map[string]bool
	listOffset int

	splitRatio  float64
	configRatio float64
	dragging    dragTarget
	visMode     visMode

	zoom       float64
	panX       int
	panY       int
	panning    bool
	panAnchorX int
	panAnchorY int
	panOriginX int
	panOriginY int

	showLegend bool

	hoveredID     string
	configOpen    bool
	configRef     model.EntityRef
	configFields  []model.ConfigField
	configErr     string
	configLoading bool
	configOffset  int

	reconnect *reconnectModel

	canvasDown    bool
	canvasDragged bool
	canvasHit     Hit
	canvasHitOK   bool
}

func NewTUIModel(f *fetcher.Fetcher) TUIModel {
	return TUIModel{
		state:       model.NewStateSnapshot(),
		updateCh:    f.UpdateCh,
		fetcher:     f,
		insecureTLS: f.InsecureTLS(),
		collapsed:   map[string]bool{},
		splitRatio:  defaultSplitRatio,
		configRatio: defaultConfigRatio,
		zoom:        1,
	}
}

func waitForState(ch <-chan model.StateSnapshot) tea.Cmd {
	return func() tea.Msg {
		return stateMsg(<-ch)
	}
}

func tickEvery(d time.Duration) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(d)
		return tickMsg(time.Now())
	}
}

func (m TUIModel) Init() tea.Cmd {
	return tea.Batch(
		waitForState(m.updateCh),
		tickEvery(1*time.Second),
	)
}

func (m TUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.KeyPressMsg:
		if m.reconnect != nil {
			return m.handleReconnectKey(msg)
		}
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "r":
			m.fetcher.ManualRefresh()
			return m, nil
		case "up", "k":
			m.moveSelection(-1)
			return m, nil
		case "down", "j":
			m.moveSelection(1)
			return m, nil
		case "left", "h":
			m.setCollapsed(true)
			return m, nil
		case "right", "l":
			m.setCollapsed(false)
			return m, nil
		case "L", "shift+l":
			m.showLegend = !m.showLegend
			return m, nil
		case "enter":
			m.openReconnectForSelected()
			return m, nil
		case "esc", "escape":
			if m.showLegend {
				m.showLegend = false
				return m, nil
			}
			if m.configOpen {
				m.closeConfig()
				return m, nil
			}
		case "1", "b":
			m.setVisMode(visBasic)
			return m, nil
		case "2", "d":
			m.setVisMode(visDatacenter)
			return m, nil
		case "tab":
			if m.visMode == visBasic {
				m.setVisMode(visDatacenter)
			} else {
				m.setVisMode(visBasic)
			}
			return m, nil
		case "+", "=", "plus":
			m.zoom = nextZoom(m.effectiveZoom(), 1)
			return m, nil
		case "-", "minus":
			m.zoom = nextZoom(m.effectiveZoom(), -1)
			return m, nil
		case "0":
			m.resetViewport()
			return m, nil
		case "shift+up":
			m.panY -= 2
			return m, nil
		case "shift+down":
			m.panY += 2
			return m, nil
		case "shift+left":
			m.panX -= 4
			return m, nil
		case "shift+right":
			m.panX += 4
			return m, nil
		}

	case tea.MouseClickMsg:
		if m.reconnect != nil {
			return m, nil
		}
		mouse := msg.Mouse()
		if mouse.Button == tea.MouseLeft {
			leftW, topoW, cfgW := layoutPanes(m.width, m.splitRatio, m.configRatio, m.configOpen)
			if inDivider(mouse.X, leftW) {
				m.dragging = dragLeftSplit
			} else if m.configOpen && cfgW > 0 && inDivider(mouse.X, configDividerX(leftW, topoW)) {
				m.dragging = dragConfigSplit
			} else if m.hitVisTab(mouse.X, mouse.Y, leftW) {
				return m, nil
			} else if m.inTopoCanvas(mouse.X, mouse.Y, leftW, topoW) {
				m.canvasDown = true
				m.canvasDragged = false
				m.panning = false
				m.panAnchorX = mouse.X
				m.panAnchorY = mouse.Y
				m.panOriginX = m.panX
				m.panOriginY = m.panY
				cx, cy := m.canvasXY(mouse.X, mouse.Y, leftW)
				m.canvasHit, m.canvasHitOK = hitAt(m.currentHits(leftW, topoW), cx, cy)
			}
			_ = cfgW
		}

	case tea.MouseMotionMsg:
		if m.reconnect != nil {
			return m, nil
		}
		mx, my := msg.Mouse().X, msg.Mouse().Y
		leftW, topoW, _ := layoutPanes(m.width, m.splitRatio, m.configRatio, m.configOpen)
		if m.dragging == dragLeftSplit && m.width > 0 {
			ratio := float64(mx) / float64(m.width)
			if ratio < 0.15 {
				ratio = 0.15
			}
			if ratio > 0.5 {
				ratio = 0.5
			}
			m.splitRatio = ratio
		} else if m.dragging == dragConfigSplit && m.width > 0 {
			// The pointer sits on the drag bar, so everything right of it is Config.
			inner := m.width - leftW - dividerWidth*2
			if inner > 0 {
				m.configRatio = clampFloat(float64(m.width-dividerWidth-mx)/float64(inner), minConfigRatio, maxConfigRatio)
			}
		} else if m.canvasDown {
			dx := mx - m.panAnchorX
			if dx < 0 {
				dx = -dx
			}
			dy := my - m.panAnchorY
			if dy < 0 {
				dy = -dy
			}
			if dx > 2 || dy > 2 {
				m.canvasDragged = true
				m.panning = true
			}
			if m.panning {
				m.panX = m.panOriginX + (m.panAnchorX - mx)
				m.panY = m.panOriginY + (m.panAnchorY - my)
			}
		} else {
			if m.inTopoCanvas(mx, my, leftW, topoW) {
				cx, cy := m.canvasXY(mx, my, leftW)
				if h, ok := hitAt(m.currentHits(leftW, topoW), cx, cy); ok {
					m.hoveredID = h.ID
				} else {
					m.hoveredID = ""
				}
			} else {
				m.hoveredID = ""
			}
		}

	case tea.MouseReleaseMsg:
		if m.reconnect != nil {
			return m, nil
		}
		wasDown := m.canvasDown
		dragged := m.canvasDragged
		hitOK := m.canvasHitOK
		hit := m.canvasHit
		m.dragging = dragNone
		m.panning = false
		m.canvasDown = false
		m.canvasDragged = false
		m.canvasHitOK = false
		if wasDown && !dragged {
			if hitOK {
				return m.openConfig(hit)
			}
			m.closeConfig()
			return m, nil
		}

	case tea.MouseWheelMsg:
		if m.reconnect != nil {
			return m, nil
		}
		leftW, topoW, cfgW := layoutPanes(m.width, m.splitRatio, m.configRatio, m.configOpen)
		mx := msg.Mouse().X
		cfgX := leftW + dividerWidth + topoW + dividerWidth
		if m.configOpen && cfgW > 0 && mx >= cfgX && mx < cfgX+cfgW {
			switch msg.Mouse().Button {
			case tea.MouseWheelUp:
				if m.configOffset > 0 {
					m.configOffset--
				}
			case tea.MouseWheelDown:
				m.configOffset++
			}
			return m, nil
		}
		if m.inTopoCanvas(mx, msg.Mouse().Y, leftW, topoW) || mx >= leftW+dividerWidth && mx < leftW+dividerWidth+topoW {
			switch msg.Mouse().Button {
			case tea.MouseWheelUp:
				m.zoom = nextZoom(m.effectiveZoom(), 1)
			case tea.MouseWheelDown:
				m.zoom = nextZoom(m.effectiveZoom(), -1)
			case tea.MouseWheelLeft:
				m.panX -= 4
			case tea.MouseWheelRight:
				m.panX += 4
			}
			return m, nil
		}
		if mx < leftW {
			rows := flattenInventory(m.state.Inventory, m.collapsed)
			switch msg.Mouse().Button {
			case tea.MouseWheelUp:
				if m.listOffset > 0 {
					m.listOffset--
				}
			case tea.MouseWheelDown:
				if m.listOffset < len(rows)-1 {
					m.listOffset++
				}
			}
		}

	case configLoadedMsg:
		if m.configOpen && m.configRef.ID == msg.Ref.ID && m.configRef.Kind == msg.Ref.Kind {
			m.configLoading = false
			if len(msg.Fields) > 0 {
				m.configFields = msg.Fields
			}
			m.configErr = msg.Err
			if msg.Auth {
				id, name, ip := m.credsTarget(msg.Ref)
				m.openReconnect(id, name, ip)
			}
		}

	case stateMsg:
		prev := m.selectedID
		m.state = model.StateSnapshot(msg)
		if m.selectedID == "" || !m.state.Inventory.HasItem(m.selectedID) {
			m.selectedID = m.state.Inventory.FirstSelectable()
			if m.selectedID == "" {
				m.selectedID = m.state.Inventory.FirstWithIP()
			}
			if m.selectedID == "" {
				if rows := m.rows(); len(rows) > 0 {
					m.selectedID = rows[0].ID
				}
			}
			if m.selectedID != "" {
				m.fetcher.SetSelected(m.selectedID)
			}
		}
		if m.reconnect != nil {
			if item, ok := m.state.Inventory.Find(m.reconnect.targetID); ok && item.CredsOK {
				m.closeReconnect()
			}
		}
		if m.selectedID != prev {
			m.resetPan()
			m.closeConfig()
		}
		return m, waitForState(m.updateCh)

	case tickMsg:
		return m, tickEvery(1 * time.Second)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}

	return m, nil
}

func (m *TUIModel) rows() []inventoryRow {
	return flattenInventory(m.state.Inventory, m.collapsed)
}

func (m *TUIModel) moveSelection(dir int) {
	rows := m.rows()
	if len(rows) == 0 {
		return
	}
	cur := selectedIndex(rows, m.selectedID)
	next := nextSelectable(rows, cur, dir)
	if next < 0 || !rows[next].Selectable {
		return
	}
	if rows[next].ID != m.selectedID {
		m.resetPan()
		m.closeConfig()
	}
	m.selectedID = rows[next].ID
	m.fetcher.SetSelected(m.selectedID)
	listH := m.listHeight()
	m.listOffset = ensureVisible(next, m.listOffset, listH)
}

func (m *TUIModel) setCollapsed(collapsed bool) {
	rows := m.rows()
	idx := selectedIndex(rows, m.selectedID)
	if idx < 0 {
		return
	}
	id := rows[idx].ID
	if rows[idx].Kind != model.KindPC {
		_, parent, ok := m.state.Inventory.FindPE(id)
		if !ok || parent == "" {
			return
		}
		id = parent
	}
	m.collapsed[id] = collapsed
}

func (m *TUIModel) setVisMode(mode visMode) {
	if m.visMode == mode {
		return
	}
	m.visMode = mode
	m.resetPan()
	m.closeConfig()
}

func (m *TUIModel) resetPan() {
	m.panX = 0
	m.panY = 0
}

func (m *TUIModel) resetViewport() {
	m.zoom = 1
	m.resetPan()
}

func (m TUIModel) effectiveZoom() float64 {
	if m.zoom <= 0 {
		return 1
	}
	return m.zoom
}

func (m TUIModel) listHeight() int {
	bodyH := m.bodyHeight()
	return bodyH - 3
}

func (m TUIModel) bodyHeight() int {
	w := m.width
	if w < 1 {
		w = 80
	}
	footerH := lipgloss.Height(renderFooter(m.state, w, m.insecureTLS))
	if footerH < 1 {
		footerH = footerReserve
	}
	bodyH := m.height - footerH
	if bodyH < 6 {
		bodyH = 6
	}
	return bodyH
}

func (m TUIModel) visTabRow() int {
	return 1 + lipgloss.Height(stylePaneTitle.Render("Topology"))
}

func (m TUIModel) visCanvasMinY() int {
	return m.visTabRow() + 1
}

func (m TUIModel) inTopoCanvas(x, y, leftW, topoW int) bool {
	rightX := leftW + dividerWidth
	if x < rightX+1 || x >= rightX+topoW-1 {
		return false
	}
	if y < m.visCanvasMinY() || y >= m.bodyHeight() {
		return false
	}
	return true
}

func (m TUIModel) canvasXY(x, y, leftW int) (cx, cy int) {
	return x - (leftW + dividerWidth + 1), y - m.visCanvasMinY()
}

func (m TUIModel) currentHits(leftW, topoW int) []Hit {
	piece := RenderVisualise(m.state, m.visMode, m.effectiveZoom(), "")
	innerW := topoW - 2
	if innerW < 10 {
		innerW = 10
	}
	headerH := lipgloss.Height(renderVisHeader(m.visMode, m.effectiveZoom()))
	if headerH < 1 {
		headerH = 1
	}
	canvasH := m.bodyHeight() - 2 - headerH
	if canvasH < 1 {
		canvasH = 1
	}
	t := viewportTransform(piece.Art, innerW, canvasH, m.panX, m.panY)
	return mapHits(piece.Hits, t)
}

func (m *TUIModel) openConfig(h Hit) (TUIModel, tea.Cmd) {
	m.configOpen = true
	m.configRef = h.Ref()
	m.configFields = entityFallbackFields(h)
	m.configErr = ""
	m.configLoading = true
	m.configOffset = 0
	if id, name, ip, credsOK := m.credsForHit(h); !credsOK {
		m.configErr = "credentials required"
		m.configLoading = false
		m.openReconnect(id, name, ip)
		return *m, nil
	}
	return *m, fetchConfigCmd(m.fetcher, m.configRef, m.state.Inventory, m.state.Cluster)
}

func (m *TUIModel) closeConfig() {
	m.configOpen = false
	m.configLoading = false
	m.configErr = ""
	m.configOffset = 0
	m.configFields = nil
}

func (m *TUIModel) handleReconnectKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		m.quitting = true
		return *m, tea.Quit
	}
	submitted, cancelled := m.reconnect.handleKey(msg)
	if cancelled {
		m.closeReconnect()
		return *m, nil
	}
	if submitted {
		return *m, m.applyReconnect()
	}
	return *m, nil
}

func (m *TUIModel) applyReconnect() tea.Cmd {
	if m.reconnect == nil {
		return nil
	}
	targetID := m.reconnect.targetID
	addr := strings.TrimSpace(m.reconnect.addr)
	user := strings.TrimSpace(m.reconnect.user)
	pass := m.reconnect.pass
	m.closeReconnect()
	m.fetcher.SetHostLogin(targetID, addr, user, pass)
	if m.configOpen {
		m.configErr = ""
		m.configLoading = true
		return fetchConfigCmd(m.fetcher, m.configRef, m.state.Inventory, m.state.Cluster)
	}
	return nil
}

// openReconnectForSelected is the keyboard entry point: enter on an inventory
// row that still needs credentials.
func (m *TUIModel) openReconnectForSelected() {
	if m.reconnect != nil {
		return
	}
	item, ok := m.state.Inventory.Find(m.selectedID)
	if !ok || item.CredsOK {
		return
	}
	m.openReconnect(item.ID, displayName(item), item.IP)
}

func (m *TUIModel) openReconnect(targetID, name, ip string) {
	r := newReconnect(targetID, name, ip)
	m.reconnect = &r
}

func (m *TUIModel) closeReconnect() {
	m.reconnect = nil
}

func (m TUIModel) credsForHit(h Hit) (id, name, ip string, credsOK bool) {
	if item, ok := m.state.Inventory.Find(h.ID); ok {
		return item.ID, displayName(item), item.IP, item.CredsOK
	}
	ref := h.Ref()
	id, name, ip = m.credsTarget(ref)
	if item, ok := m.state.Inventory.Find(id); ok {
		return item.ID, displayName(item), item.IP, item.CredsOK
	}
	return id, name, ip, true
}

func (m TUIModel) credsTarget(ref model.EntityRef) (id, name, ip string) {
	for _, candidate := range []string{ref.ID, ref.ClusterID} {
		if candidate == "" {
			continue
		}
		if item, ok := m.state.Inventory.Find(candidate); ok {
			return item.ID, displayName(item), item.IP
		}
	}
	name = ref.Name
	if name == "" {
		name = ref.IP
	}
	return ref.ID, name, ref.IP
}

func fetchConfigCmd(f *fetcher.Fetcher, ref model.EntityRef, inv model.Inventory, cluster model.Cluster) tea.Cmd {
	return func() tea.Msg {
		fields, err := f.FetchEntityConfig(ref, inv, cluster)
		msg := configLoadedMsg{Ref: ref, Fields: fields}
		if err != nil {
			if fetcher.IsAuthErr(err) {
				msg.Auth = true
				msg.Err = "credentials required"
			} else {
				msg.Err = err.Error()
			}
		}
		return msg
	}
}

func (m TUIModel) View() tea.View {
	if m.quitting {
		return tea.NewView("\n  Goodbye!\n\n")
	}

	w, h := m.width, m.height
	if w < 1 || h < 1 {
		v := tea.NewView("")
		v.AltScreen = true
		v.MouseMode = tea.MouseModeAllMotion
		return v
	}

	footer := renderFooter(m.state, w, m.insecureTLS)
	footerH := lipgloss.Height(footer)
	if footerH < 1 {
		footerH = footerReserve
	}
	bodyH := h - footerH
	if bodyH < 6 {
		bodyH = 6
	}

	leftW, topoW, cfgW := layoutPanes(w, m.splitRatio, m.configRatio, m.configOpen)

	rows := flattenInventory(m.state.Inventory, m.collapsed)
	listH := bodyH - 3
	if listH < 1 {
		listH = 1
	}
	idx := selectedIndex(rows, m.selectedID)
	offset := ensureVisible(idx, m.listOffset, listH)
	leftBody := renderInventoryList(rows, m.selectedID, offset, leftW-2, listH)
	leftPane := renderPane(stylePaneTitle.Render("Inventory"), leftBody, leftW, bodyH)

	topoInner := topoW - 2
	if topoInner < 10 {
		topoInner = 10
	}
	header := renderVisHeader(m.visMode, m.effectiveZoom())
	headerH := lipgloss.Height(header)
	if headerH < 1 {
		headerH = 1
	}
	canvasH := bodyH - 2 - headerH
	if canvasH < 1 {
		canvasH = 1
	}
	piece := RenderVisualise(m.state, m.visMode, m.effectiveZoom(), m.hoveredID)
	topoBody := applyViewport(piece.Art, topoInner, canvasH, m.panX, m.panY)
	topoPane := renderPane(header, topoBody, topoW, bodyH)

	panes := []string{leftPane, topoPane}
	if m.configOpen && cfgW > 0 {
		cfgInner := cfgW - 2
		if cfgInner < 4 {
			cfgInner = 4
		}
		cfgH := bodyH - 3
		if cfgH < 1 {
			cfgH = 1
		}
		cfgBody := renderConfigBody(m.configFields, m.configErr, m.configLoading, cfgInner, cfgH, m.configOffset)
		panes = append(panes, renderPane(stylePaneTitle.Render("Config"), cfgBody, cfgW, bodyH))
	}

	paneH := bodyH
	for _, p := range panes {
		if h := lipgloss.Height(p); h > paneH {
			paneH = h
		}
	}
	parts := make([]string, 0, len(panes)*2-1)
	for i, p := range panes {
		if i > 0 {
			active := i == 1 && m.dragging == dragLeftSplit || i == 2 && m.dragging == dragConfigSplit
			parts = append(parts, renderDivider(paneH, active))
		}
		parts = append(parts, padToHeight(p, paneH))
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, parts...)
	if m.showLegend {
		leg := renderLegend()
		lw := lipgloss.Width(leg)
		lh := lipgloss.Height(leg)
		ox := w - lw - 1
		if ox < 0 {
			ox = 0
		}
		oy := bodyH - lh
		if oy < 0 {
			oy = 0
		}
		body = overlayAt(padToHeight(body, bodyH), leg, ox, oy)
	}
	if m.reconnect != nil {
		box := m.reconnect.renderWidth(w - 2)
		bw := lipgloss.Width(box)
		bh := lipgloss.Height(box)
		ox := (w - bw) / 2
		if ox < 0 {
			ox = 0
		}
		oy := (bodyH - bh) / 2
		if oy < 0 {
			oy = 0
		}
		body = overlayAt(padToHeight(body, bodyH), box, ox, oy)
	}
	frame := clipFrame(lipgloss.JoinVertical(lipgloss.Left, body, footer), w, h)

	v := tea.NewView(frame)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeAllMotion
	return v
}

func (m *TUIModel) hitVisTab(x, y, leftW int) bool {
	rightX := leftW + dividerWidth
	if x < rightX {
		return false
	}
	if y != m.visTabRow() {
		return false
	}
	rel := x - rightX - 1
	basicStart, dcStart, basicW, dcW := tabLabelStart(m.visMode)
	if rel >= basicStart && rel < basicStart+basicW {
		m.setVisMode(visBasic)
		return true
	}
	if rel >= dcStart && rel < dcStart+dcW {
		m.setVisMode(visDatacenter)
		return true
	}
	return false
}
