package api

import (
	"crypto/tls"
	"net"
	"net/url"
	"strings"
	"time"
)

const APIPort = "9440"

// HostFromURL extracts a hostname or IP from a management URL such as
// https://10.0.0.1:9440/. Bare hosts are returned unchanged.
func HostFromURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		// host or host:port
		host, _, err := net.SplitHostPort(raw)
		if err == nil {
			return host
		}
		return strings.TrimSuffix(raw, "/")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// LooksLikeUUID reports whether s is an 8-4-4-4-12 hex UUID (not a host/IP).
func LooksLikeUUID(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) != 36 {
		return false
	}
	if s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return false
	}
	for i, r := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return true
}

// NormalizeHost lowercases and trims a host for deduplication.
func NormalizeHost(host string) string {
	return strings.ToLower(strings.TrimSpace(host))
}

// ProbeReachable checks whether host:9440 accepts a TLS connection.
func ProbeReachable(host string, timeout time.Duration, insecure bool) bool {
	if host == "" {
		return false
	}
	dialer := &net.Dialer{Timeout: timeout}
	conn, err := tls.DialWithDialer(dialer, "tcp", net.JoinHostPort(host, APIPort), &tls.Config{
		InsecureSkipVerify: insecure, //nolint:gosec
	})
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
