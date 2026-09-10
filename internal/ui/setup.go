package ui

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/nutanix/ntnx-topo/internal/config"
)

type setupStep int

const (
	setupAskCount setupStep = iota
	setupAskIP
	setupAskUser
	setupAskPass
)

type setupModel struct {
	step      setupStep
	count     int
	countStr  string
	index     int
	current   config.PrismEndpoint
	endpoints []config.PrismEndpoint
	err       string
	done      bool
	cancelled bool
	width     int
}

func newSetupModel() setupModel {
	return setupModel{step: setupAskCount}
}

func (m setupModel) Init() tea.Cmd { return nil }

func (m setupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.cancelled = true
			return m, tea.Quit
		case "enter":
			return m.submit()
		case "backspace":
			m.err = ""
			s := m.activeValue()
			if len(s) > 0 {
				m.setActiveValue(s[:len(s)-1])
			}
		case "tab":
			return m.submit()
		default:
			if text := msg.Key().Text; text != "" {
				m.err = ""
				m.setActiveValue(m.activeValue() + text)
			}
		}
	}
	return m, nil
}

func (m setupModel) submit() (tea.Model, tea.Cmd) {
	switch m.step {
	case setupAskCount:
		n, err := strconv.Atoi(strings.TrimSpace(m.countStr))
		if err != nil || n < 1 {
			m.err = "enter a number of 1 or more"
			return m, nil
		}
		m.count = n
		m.index = 0
		m.current = config.PrismEndpoint{}
		m.step = setupAskIP
	case setupAskIP:
		ip := strings.TrimSpace(m.current.IP)
		if ip == "" {
			m.err = "IP or FQDN is required"
			return m, nil
		}
		m.current.IP = ip
		m.step = setupAskUser
	case setupAskUser:
		user := strings.TrimSpace(m.current.Username)
		if user == "" {
			m.err = "username is required"
			return m, nil
		}
		m.current.Username = user
		m.step = setupAskPass
	case setupAskPass:
		if m.current.Password == "" {
			m.err = "password is required"
			return m, nil
		}
		m.endpoints = append(m.endpoints, m.current)
		m.index++
		if m.index >= m.count {
			m.done = true
			return m, nil
		}
		m.current = config.PrismEndpoint{}
		m.step = setupAskIP
	}
	m.err = ""
	return m, nil
}

func (m setupModel) activeValue() string {
	switch m.step {
	case setupAskCount:
		return m.countStr
	case setupAskIP:
		return m.current.IP
	case setupAskUser:
		return m.current.Username
	case setupAskPass:
		return m.current.Password
	}
	return ""
}

func (m *setupModel) setActiveValue(v string) {
	switch m.step {
	case setupAskCount:
		m.countStr = v
	case setupAskIP:
		m.current.IP = v
	case setupAskUser:
		m.current.Username = v
	case setupAskPass:
		m.current.Password = v
	}
}

// setupInputW is the wizard input box width, border included.
const setupInputW = 34

// inputWidth pins the box width, shrinking it for a narrow terminal so the
// border can never run past the edge of the window.
func (m setupModel) inputWidth() int {
	w := setupInputW
	if m.width > 0 && m.width-4 < w {
		w = m.width - 4
	}
	if w < 10 {
		w = 10
	}
	return w
}

func (m setupModel) View() tea.View {
	title := styleTitle.Render("  ntnx-topo setup  ")
	prompt := ""
	value := m.activeValue()
	display := value
	if m.step == setupAskPass {
		// Mask by rune, not by byte: a non-ASCII password would otherwise get
		// more bullets than it has characters.
		display = strings.Repeat("•", len([]rune(value)))
	}

	switch m.step {
	case setupAskCount:
		prompt = "How many Prism Centrals do you want to add?"
	case setupAskIP:
		prompt = fmt.Sprintf("PC %d/%d — IP or FQDN", m.index+1, m.count)
	case setupAskUser:
		prompt = fmt.Sprintf("PC %d/%d (%s) — username", m.index+1, m.count, m.current.IP)
	case setupAskPass:
		prompt = fmt.Sprintf("PC %d/%d (%s) — password", m.index+1, m.count, m.current.IP)
	}

	body := lipgloss.JoinVertical(lipgloss.Left,
		title,
		"",
		styleHelp.Render("  No Prism Central or Prism Element is configured."),
		"",
		styleMetrics.Render("  "+prompt),
		// PaddingLeft, not a "  " prefix: concatenating indents only the first
		// line of a bordered box and staircases the rest.
		lipgloss.NewStyle().PaddingLeft(2).Render(inputBox(display, m.inputWidth(), true)),
	)
	if m.err != "" {
		body = lipgloss.JoinVertical(lipgloss.Left, body, "", styleRed.Render("  "+m.err))
	}
	body = lipgloss.JoinVertical(lipgloss.Left, body, "",
		styleHelp.Render("  enter: next  •  esc: cancel"),
	)

	// JoinVertical pads every line out to the widest one, so without this the
	// wizard emits 50-cell lines into a 40-cell window and the terminal wraps
	// them, tearing the input box in half.
	if m.width > 0 {
		body = lipgloss.NewStyle().MaxWidth(m.width).Render(body)
	}

	v := tea.NewView(body)
	v.AltScreen = true
	return v
}
