package ui

import (
	"fmt"
	"net"
	"strings"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/nutanix/ntnx-topo/internal/api"
)

const (
	reconnectAddr = iota
	reconnectUser
	reconnectPass
	reconnectFields
)

type reconnectModel struct {
	targetID string
	name     string
	addr     string
	user     string
	pass     string
	field    int
	err      string
}

func newReconnect(targetID, name, ip string) reconnectModel {
	addr := strings.TrimSpace(ip)
	if addr == "" {
		addr = addressFromName(name)
	}
	if name == "" {
		name = addr
	}
	r := reconnectModel{targetID: targetID, name: name, addr: addr, user: "admin"}
	if addr != "" {
		r.field = reconnectUser
	}
	return r
}

// addressFromName recovers an address from AZ display names such as
// "PC_10.136.107.235", which is all the v3 AZ list gives us when the
// registration carries no external address.
func addressFromName(name string) string {
	s := strings.TrimSpace(name)
	for _, prefix := range []string{"PC_", "PC-", "pc_", "pc-"} {
		if strings.HasPrefix(s, prefix) {
			s = s[len(prefix):]
			break
		}
	}
	if s == "" || api.LooksLikeUUID(s) {
		return ""
	}
	if net.ParseIP(s) != nil {
		return s
	}
	if strings.Contains(s, ".") && !strings.ContainsAny(s, " \t/") {
		return s
	}
	return ""
}

func (r *reconnectModel) handleKey(msg tea.KeyPressMsg) (submitted, cancelled bool) {
	switch msg.String() {
	case "esc", "escape":
		return false, true
	case "enter":
		return r.submit()
	case "tab", "down":
		r.field = (r.field + 1) % reconnectFields
		r.err = ""
		return false, false
	case "shift+tab", "up":
		r.field = (r.field + reconnectFields - 1) % reconnectFields
		r.err = ""
		return false, false
	case "backspace":
		r.err = ""
		s := r.activeValue()
		if len(s) > 0 {
			r.setActiveValue(s[:len(s)-1])
		}
		return false, false
	default:
		if text := msg.Key().Text; text != "" {
			r.err = ""
			r.setActiveValue(r.activeValue() + text)
		}
	}
	return false, false
}

func (r *reconnectModel) submit() (submitted, cancelled bool) {
	addr := strings.TrimSpace(r.addr)
	if addr == "" {
		r.err = "IP or FQDN is required"
		r.field = reconnectAddr
		return false, false
	}
	user := strings.TrimSpace(r.user)
	if user == "" {
		r.err = "username is required"
		r.field = reconnectUser
		return false, false
	}
	if r.pass == "" {
		r.err = "password is required"
		r.field = reconnectPass
		return false, false
	}
	r.addr = addr
	r.user = user
	return true, false
}

func (r *reconnectModel) activeValue() string {
	switch r.field {
	case reconnectAddr:
		return r.addr
	case reconnectPass:
		return r.pass
	default:
		return r.user
	}
}

func (r *reconnectModel) setActiveValue(v string) {
	switch r.field {
	case reconnectAddr:
		r.addr = v
	case reconnectPass:
		r.pass = v
	default:
		r.user = v
	}
}

const (
	reconnectLabelW = 13
	reconnectValueW = 28
	// reconnectPanelW is the full overlay width including its border.
	reconnectPanelW = reconnectLabelW + reconnectValueW + 6
)

// render draws the overlay at a fixed width so every line is the same length;
// ragged lines leave holes in the panel background.
func (r reconnectModel) render() string {
	return r.renderWidth(reconnectPanelW)
}

// renderWidth draws the overlay clamped to width cells, border included.
func (r reconnectModel) renderWidth(width int) string {
	if width > reconnectPanelW {
		width = reconnectPanelW
	}
	inner := width - 4 // border + horizontal padding
	if inner < 12 {
		inner = 12
	}
	labelW := reconnectLabelW
	if labelW > inner/2 {
		labelW = inner / 2
	}
	valueW := inner - labelW
	if valueW < 6 {
		valueW = 6
	}

	line := func(s string) string {
		return lipgloss.NewStyle().Width(inner).MaxWidth(inner).Render(s)
	}
	rows := []string{
		line(stylePaneTitle.Render(truncCells(fmt.Sprintf("connect to %s", r.name), inner-4))),
		"",
		r.fieldRow("IP / FQDN", r.addr, reconnectAddr, labelW, valueW),
		r.fieldRow("username", r.user, reconnectUser, labelW, valueW),
		r.fieldRow("password", strings.Repeat("•", len([]rune(r.pass))), reconnectPass, labelW, valueW),
	}
	if r.err != "" {
		rows = append(rows, "", line(styleRed.Render(truncCells(r.err, inner))))
	}
	rows = append(rows, "", line(styleHelp.Render(truncCells("enter: connect • tab: next field • esc: cancel", inner))))

	body := lipgloss.JoinVertical(lipgloss.Left, rows...)
	return styleLegend.Width(width).MaxWidth(width).Render(body)
}

// fieldRow puts the label and the bordered input side by side. Concatenating
// them with + only appends to the first line, which staircases the box.
func (r reconnectModel) fieldRow(label, value string, field, labelW, valueW int) string {
	name := lipgloss.NewStyle().
		Width(labelW).
		MaxWidth(labelW).
		PaddingLeft(1).
		Inherit(styleMetrics).
		Render(truncCells(label, labelW-2))
	return lipgloss.JoinHorizontal(lipgloss.Center, name, inputBox(value, valueW, r.field == field))
}
