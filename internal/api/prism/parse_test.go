package prism

import (
	"encoding/json"
	"testing"
)

func TestParseRegistrationStringAndObject(t *testing.T) {
	raw := json.RawMessage(`{
		"remoteClusterExtId": "abc-uuid",
		"connectivityStatus": {"value": "CONNECTED"},
		"remoteClusterDetails": {
			"name": "PC-76",
			"clusterType": {"name": "PRISM_CENTRAL"},
			"externalAddress": {"ipv4": {"value": "10.1.2.3"}}
		}
	}`)
	reg, ok := parseRegistration(raw)
	if !ok {
		t.Fatal("expected registration")
	}
	if reg.ExtID != "abc-uuid" {
		t.Fatalf("extId: %q", reg.ExtID)
	}
	if reg.Name != "PC-76" {
		t.Fatalf("name: %q", reg.Name)
	}
	if reg.ClusterType != "PRISM_CENTRAL" {
		t.Fatalf("type: %q", reg.ClusterType)
	}
	if reg.ConnectivityStatus != "CONNECTED" {
		t.Fatalf("status: %q", reg.ConnectivityStatus)
	}
	if reg.IP != "10.1.2.3" {
		t.Fatalf("ip: %q", reg.IP)
	}
}

func TestMatchAnyClusterType(t *testing.T) {
	raw := json.RawMessage(`{
		"remoteClusterExtId": "az-1",
		"connectivityStatus": "REGISTERED",
		"remoteClusterDetails": {
			"name": "remote",
			"clusterType": "AOS",
			"externalAddress": {"ipv4": {"value": "10.0.0.9"}}
		}
	}`)
	reg, ok := parseRegistration(raw)
	if !ok {
		t.Fatal("expected registration")
	}
	if reg.IP != "10.0.0.9" {
		t.Fatalf("ip: %q", reg.IP)
	}
	if !reg.IsRemotePC() && reg.IP == "" {
		t.Fatal("IP must still be usable when type is not DOMAIN_MANAGER")
	}
}

func TestParseProductAndEnabledNames(t *testing.T) {
	raw := json.RawMessage(`{
		"name": {"value": "INTELLIGENT_OPERATIONS"},
		"enablementState": "ENABLED"
	}`)
	p, ok := parseProduct(raw)
	if !ok {
		t.Fatal("expected product")
	}
	if p.Name != "INTELLIGENT_OPERATIONS" || !p.Enabled() {
		t.Fatalf("got %+v", p)
	}
	names := EnabledProductNames([]Product{
		{Name: "FLOW_NETWORK_SECURITY", EnablementState: "ENABLED"},
		{Name: "FLOW_VIRTUAL_NETWORKING", EnablementState: "ENABLED"},
		{Name: "NUTANIX_CLOUD_MANAGER", EnablementState: "DISABLED"},
		{Name: "INTELLIGENT_OPERATIONS", EnablementState: "ENABLED"},
	})
	if len(names) != 2 || names[0] != "Flow" || names[1] != "AIOps" {
		t.Fatalf("names: %v", names)
	}
}
