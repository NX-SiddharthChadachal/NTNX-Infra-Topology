package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
)

// The wizard input box used to render with no pinned width, so Lipgloss sized
// the border from its own measurement of the content. On the Windows console
// that measurement disagreed with the terminal: the horizontal border came out
// shorter than the text and the corner runes landed several cells adrift. The
// box must be a clean rectangle whatever is typed into it.
func TestSetupInputBoxIsRectangular(t *testing.T) {
	m := newSetupModel()
	m.width = 100
	m.step = setupAskIP
	m.count = 2

	for _, value := range []string{
		"",
		"10.161.20.42",
		"pc-a.very-long-hostname.example.internal.test",
		"pässwörd-with-non-ascii",
	} {
		m.current.IP = value
		box := inputBox(value, m.inputWidth(), true)
		lines := strings.Split(box, "\n")
		if len(lines) != 3 {
			t.Fatalf("%q: box has %d lines, want 3", value, len(lines))
		}
		for i, ln := range lines {
			if got := lipgloss.Width(ln); got != m.inputWidth() {
				t.Errorf("%q: box line %d is %d cells, want %d: %q",
					value, i, got, m.inputWidth(), ln)
			}
		}
	}
}

// A password must be masked by rune count, not byte count.
func TestSetupMasksPasswordByRune(t *testing.T) {
	m := newSetupModel()
	m.width = 100
	m.count = 1
	m.step = setupAskPass
	m.current.Password = "pässwörd" // 8 runes, 10 bytes

	// The help line has bullets of its own, so look for the masked run rather
	// than counting every bullet in the frame.
	body := m.View().Content
	if !strings.Contains(body, strings.Repeat("•", 8)) {
		t.Errorf("want a run of 8 bullets, one per rune, in:\n%s", body)
	}
	if strings.Contains(body, strings.Repeat("•", 9)) {
		t.Error("password is masked by byte, not by rune: too many bullets")
	}
	if strings.Contains(body, "pässwörd") {
		t.Error("the password itself must never be drawn")
	}
}

// Nothing the wizard draws may exceed the terminal width, at any size.
func TestSetupFitsSmallTerminals(t *testing.T) {
	for _, size := range [][2]int{{100, 30}, {80, 24}, {60, 20}, {40, 12}, {24, 10}} {
		m := newSetupModel()
		m.count = 1
		m.step = setupAskIP
		m.current.IP = "pc-a.very-long-hostname.example.internal.test"

		next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		sm, ok := next.(setupModel)
		if !ok {
			t.Fatalf("%dx%d: Update did not return a setupModel", size[0], size[1])
		}

		for i, ln := range strings.Split(sm.View().Content, "\n") {
			if got := lipgloss.Width(ln); got > size[0] {
				t.Errorf("%dx%d: line %d is %d cells wide: %q",
					size[0], size[1], i, got, ln)
			}
		}
	}
}

// The cursor must be exactly one cell. U+2588 FULL BLOCK is East Asian
// "Ambiguous", which a terminal may advance by two, shifting every cell after it.
func TestCursorCellIsOneCell(t *testing.T) {
	if got := lipgloss.Width(cursorCell()); got != 1 {
		t.Errorf("cursor is %d cells wide, want 1", got)
	}
	if strings.Contains(cursorCell(), "█") {
		t.Error("cursor still uses the ambiguous-width block rune")
	}
}
