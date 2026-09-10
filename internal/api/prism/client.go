package prism

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/nutanix/ntnx-topo/internal/api"
)

const (
	typeDomainManager = "DOMAIN_MANAGER"
	statusConnected   = "CONNECTED"
)

var registrationPaths = []string{
	"/api/prism/v4.2/management/domain-managers/%s/registrations",
	"/api/prism/v4.1/management/domain-managers/%s/registrations",
	"/api/prism/v4.0/management/domain-managers/%s/registrations",
	"/api/prism/v4.3/management/domain-managers/%s/registrations",
}

var productPaths = []string{
	"/api/prism/v4.2/management/domain-managers/%s/products",
	"/api/prism/v4.1/management/domain-managers/%s/products",
	"/api/prism/v4.0/management/domain-managers/%s/products",
	"/api/prism/v4.3/management/domain-managers/%s/products",
}

// Client talks to Prism v4.x management APIs on a Prism Central.
type Client struct {
	base *api.BaseClient
}

func NewClient(host, username, password string, insecure bool, timeout time.Duration) *Client {
	return &Client{
		base: api.NewBaseClient(host, username, password, insecure, timeout),
	}
}

// Registration is a cluster (PE or remote PC) registered to this domain manager.
type Registration struct {
	ExtID              string
	Name               string
	ClusterType        string
	IP                 string
	ConnectivityStatus string
}

func (r Registration) IsRemotePC() bool {
	t := strings.ToUpper(r.ClusterType)
	return t == typeDomainManager || strings.Contains(t, "PRISM_CENTRAL") || strings.Contains(t, "DOMAIN_MANAGER")
}

func (r Registration) Connected() bool {
	s := strings.ToUpper(strings.TrimSpace(r.ConnectivityStatus))
	if s == "" || s == "$UNKNOWN" || s == "$REDACTED" {
		return false
	}
	return strings.Contains(s, statusConnected) && !strings.Contains(s, "DISCONNECTED")
}

type registrationsResponse struct {
	Data []json.RawMessage `json:"data"`
}

// ListRegistrations returns PE and remote-PC registrations for a domain manager.
func (c *Client) ListRegistrations(domainManagerExtId string) ([]Registration, error) {
	if domainManagerExtId == "" {
		return nil, fmt.Errorf("empty domain manager id")
	}
	var lastErr error
	for _, tmpl := range registrationPaths {
		path := fmt.Sprintf(tmpl, url.PathEscape(domainManagerExtId))
		var resp registrationsResponse
		code, err := c.base.DoJSON("GET", path, nil, &resp)
		if err != nil {
			lastErr = err
			if ae, ok := err.(*api.APIError); ok && ae.IsNotFound() {
				continue
			}
			if code == 404 {
				continue
			}
			return nil, err
		}
		out := make([]Registration, 0, len(resp.Data))
		for _, raw := range resp.Data {
			reg, ok := parseRegistration(raw)
			if !ok {
				continue
			}
			slog.Debug("domain-manager registration",
				"extId", reg.ExtID,
				"name", reg.Name,
				"type", reg.ClusterType,
				"status", reg.ConnectivityStatus,
				"ip", reg.IP,
			)
			out = append(out, reg)
		}
		return out, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("registrations not found")
}

// Product is a domain-manager opt-in product (Flow, AIOps, NCM, …).
type Product struct {
	Name            string
	EnablementState string
}

func (p Product) Enabled() bool {
	s := strings.ToUpper(strings.TrimSpace(p.EnablementState))
	return s == "ENABLED"
}

type listEnvelope struct {
	Data []json.RawMessage `json:"data"`
}

// ListProducts returns opt-in products for a domain manager (PC).
func (c *Client) ListProducts(domainManagerExtId string) ([]Product, error) {
	if domainManagerExtId == "" {
		return nil, fmt.Errorf("empty domain manager id")
	}
	var lastErr error
	for _, tmpl := range productPaths {
		path := fmt.Sprintf(tmpl, url.PathEscape(domainManagerExtId))
		var resp listEnvelope
		code, err := c.base.DoJSON("GET", path, nil, &resp)
		if err != nil {
			lastErr = err
			if ae, ok := err.(*api.APIError); ok && ae.IsNotFound() {
				continue
			}
			if code == 404 {
				continue
			}
			return nil, err
		}
		out := make([]Product, 0, len(resp.Data))
		for _, raw := range resp.Data {
			p, ok := parseProduct(raw)
			if !ok {
				continue
			}
			slog.Debug("domain-manager product", "name", p.Name, "state", p.EnablementState)
			out = append(out, p)
		}
		return out, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("products not found")
}

func parseProduct(raw json.RawMessage) (Product, bool) {
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return Product{}, false
	}
	p := Product{
		Name:            flexString(m["name"]),
		EnablementState: flexString(m["enablementState"]),
	}
	if p.Name == "" {
		return Product{}, false
	}
	return p, true
}

// FriendlyProductName maps API enums to Config-pane labels.
func FriendlyProductName(apiName string) string {
	switch strings.ToUpper(strings.TrimSpace(apiName)) {
	case "FLOW_NETWORK_SECURITY", "FLOW_VIRTUAL_NETWORKING", "FLOW_CONTROLLER":
		return "Flow"
	case "INTELLIGENT_OPERATIONS":
		return "AIOps"
	case "NUTANIX_CLOUD_MANAGER":
		return "NCM"
	case "SELF_SERVICE":
		return "Self Service"
	case "NUTANIX_DISASTER_RECOVERY":
		return "DR"
	case "NUTANIX_MARKETPLACE":
		return "Marketplace"
	default:
		s := strings.ReplaceAll(apiName, "_", " ")
		if s == "" || s == "$UNKNOWN" || s == "$REDACTED" {
			return ""
		}
		return s
	}
}

// EnabledProductNames returns unique friendly names of ENABLED products.
func EnabledProductNames(products []Product) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range products {
		if !p.Enabled() {
			continue
		}
		name := FriendlyProductName(p.Name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

func parseRegistration(raw json.RawMessage) (Registration, bool) {
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return Registration{}, false
	}
	details, _ := m["remoteClusterDetails"].(map[string]interface{})
	if details == nil {
		details = map[string]interface{}{}
	}
	reg := Registration{
		ExtID:              flexString(m["remoteClusterExtId"]),
		Name:               flexString(details["name"]),
		ClusterType:        flexString(details["clusterType"]),
		ConnectivityStatus: flexString(m["connectivityStatus"]),
		IP:                 ipv4From(details["externalAddress"]),
	}
	if reg.ExtID == "" && reg.IP == "" && reg.Name == "" {
		return Registration{}, false
	}
	return reg, true
}

func flexString(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case json.Number:
		return t.String()
	case float64:
		return fmt.Sprintf("%v", t)
	case map[string]interface{}:
		for _, k := range []string{"value", "name", "GetName"} {
			if s := flexString(t[k]); s != "" {
				return s
			}
		}
		if s := flexString(t["$unknownFields"]); s != "" {
			return s
		}
	}
	return ""
}

func ipv4From(v interface{}) string {
	m, ok := v.(map[string]interface{})
	if !ok {
		return flexString(v)
	}
	if ipv4, ok := m["ipv4"].(map[string]interface{}); ok {
		if s := flexString(ipv4["value"]); s != "" {
			return s
		}
	}
	if s := flexString(m["ipv4"]); s != "" && !strings.HasPrefix(s, "map[") {
		return s
	}
	if fqdn, ok := m["fqdn"].(map[string]interface{}); ok {
		if s := flexString(fqdn["value"]); s != "" {
			return s
		}
	}
	return flexString(m["fqdn"])
}

// IndexByExtID maps remote cluster UUID → registration (last wins).
func IndexByExtID(regs []Registration) map[string]Registration {
	m := make(map[string]Registration, len(regs))
	for _, r := range regs {
		if r.ExtID == "" {
			continue
		}
		m[strings.ToLower(r.ExtID)] = r
	}
	return m
}
