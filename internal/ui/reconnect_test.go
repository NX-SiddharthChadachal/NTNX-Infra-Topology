package ui

import (
	"strings"
	"testing"

	lipgloss "charm.land/lipgloss/v2"
)

// The overlay used to concatenate a one-line label with a three-line bordered
// box, which staircased the input boxes and left holes in the panel background.
func TestReconnectOverlayRowsAlign(t *testing.T) {
	r := newReconnect("az-1", "Unnamed", "10.48.58.121")
	r.pass = "secret"

	for _, field := range []int{reconnectAddr, reconnectUser, reconnectPass} {
		r.field = field
		out := r.render()
		lines := strings.Split(out, "\n")
		want := lipgloss.Width(lines[0])
		for i, ln := range lines {
			if got := lipgloss.Width(ln); got != want {
				t.Errorf("field %d: line %d is %d cells, want %d: %q", field, i, got, want, ln)
			}
		}
		if got := lipgloss.Height(out); got != 15 {
			t.Errorf("field %d: overlay height = %d, want 15", field, got)
		}
		// Every label must share a line with its box, never sit alone above it.
		for _, label := range []string{"IP / FQDN", "username", "password"} {
			for _, ln := range lines {
				if strings.Contains(ln, label) && !strings.Contains(ln, "│") {
					t.Errorf("field %d: %q is not on a box line: %q", field, label, ln)
				}
			}
		}
	}
}

func TestReconnectOverlayFitsSmallTerminals(t *testing.T) {
	r := newReconnect("az-1", "PC_10.48.58.121", "10.48.58.121")
	r.err = "credentials required"
	for _, size := range [][2]int{{80, 24}, {60, 20}, {40, 12}, {24, 10}} {
		out := r.renderWidth(size[0] - 2)
		if got := lipgloss.Width(out); got > size[0] {
			t.Errorf("%dx%d: overlay is %d cells wide", size[0], size[1], got)
		}
		for i, ln := range strings.Split(out, "\n") {
			if got := lipgloss.Width(ln); got > size[0] {
				t.Errorf("%dx%d: line %d is %d cells wide", size[0], size[1], i, got)
			}
		}
	}
}

func TestAddressFromName(t *testing.T) {
	cases := map[string]string{
		"PC_10.136.107.235":                    "10.136.107.235",
		"PC-10.48.58.121":                      "10.48.58.121",
		"pc_pc-skade01.example.com":            "pc-skade01.example.com",
		"PC-SKADE01-1":                         "",
		"62d3cc04-9f85-4cc4-bfbc-b5f7ffe59d46": "",
		"":                                     "",
	}
	for name, want := range cases {
		if got := addressFromName(name); got != want {
			t.Errorf("addressFromName(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestReconnectPrefillsAndValidates(t *testing.T) {
	r := newReconnect("az-1", "PC_10.136.107.235", "")
	if r.addr != "10.136.107.235" {
		t.Fatalf("address = %q, want the address parsed from the name", r.addr)
	}
	if r.user != "admin" {
		t.Fatalf("username = %q, want admin", r.user)
	}
	if submitted, _ := r.submit(); submitted {
		t.Fatal("submitted without a password")
	}
	r.pass = "secret"
	submitted, cancelled := r.submit()
	if !submitted || cancelled {
		t.Fatalf("submit = (%v, %v), want (true, false)", submitted, cancelled)
	}

	blank := newReconnect("az-2", "PC-SKADE01-1", "")
	if blank.addr != "" {
		t.Fatalf("address = %q, want empty for a name with no address", blank.addr)
	}
	if blank.field != reconnectAddr {
		t.Fatal("cursor should start on the address field when it is unknown")
	}
	blank.user, blank.pass = "admin", "secret"
	if submitted, _ := blank.submit(); submitted {
		t.Fatal("submitted without an address")
	}
}
