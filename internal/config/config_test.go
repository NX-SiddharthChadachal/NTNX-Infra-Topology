package config

import "testing"

// The --verify-tls flag decides certificate verification, but only when it was
// actually passed: at its default it must not override the config file.
func TestApplyTLSChoice(t *testing.T) {
	cases := []struct {
		name      string
		start     bool // Insecure before the flag is applied
		verifyTLS bool
		explicit  bool
		want      bool // Insecure after
	}{
		{"flag absent keeps the default", true, false, false, true},
		{"flag absent keeps insecure: false from the file", false, false, false, false},
		{"--verify-tls enables verification", true, true, true, false},
		{"--verify-tls=false disables verification", false, false, true, true},
	}

	for _, tc := range cases {
		c := &Config{Insecure: tc.start}
		c.applyTLSChoice(tc.verifyTLS, tc.explicit)
		if c.Insecure != tc.want {
			t.Errorf("%s: Insecure = %v, want %v", tc.name, c.Insecure, tc.want)
		}
	}
}

// Verification is off by default because Prism ships self-signed certificates;
// turning it on has to be a deliberate choice.
func TestDefaultIsInsecure(t *testing.T) {
	c := defaults()
	if !c.Insecure {
		t.Error("verification must default to off, or every run against a self-signed Prism fails")
	}
	c.applyTLSChoice(false, false)
	if !c.Insecure {
		t.Error("an unpassed --verify-tls flag must not change the default")
	}
	c.applyTLSChoice(true, true)
	if c.Insecure {
		t.Error("--verify-tls must turn verification on")
	}
}
