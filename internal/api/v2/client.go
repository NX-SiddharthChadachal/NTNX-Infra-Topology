package v2

import (
	"fmt"
	"strings"
	"time"

	"github.com/nutanix/ntnx-topo/internal/api"
	"github.com/nutanix/ntnx-topo/internal/model"
)

// Client talks to a Prism Element v2 API.
type Client struct {
	base *api.BaseClient
}

func NewClient(host, username, password string, insecure bool, timeout time.Duration) *Client {
	return &Client{
		base: api.NewBaseClient(host, username, password, insecure, timeout),
	}
}

func (c *Client) Host() string { return c.base.Host }

// VerifyLogin makes one small authenticated call to check credentials before
// anything else runs. A 401/403 means the login is wrong; any other error says
// nothing about the credentials, only that we could not ask.
func (c *Client) VerifyLogin() error {
	var resp V2ClusterResponse
	if _, err := c.base.DoJSON("GET", PathClusters, nil, &resp); err != nil {
		return err
	}
	return nil
}

// FetchTopology retrieves cluster, host, and network data and assembles a topology.
func (c *Client) FetchTopology() (*model.Cluster, error) {
	var clusterResp V2ClusterResponse
	if _, err := c.base.DoJSON("GET", PathClusters, nil, &clusterResp); err != nil {
		return nil, fmt.Errorf("v2 clusters: %w", err)
	}

	var hostResp V2HostResponse
	if _, err := c.base.DoJSON("GET", PathHosts, nil, &hostResp); err != nil {
		return nil, fmt.Errorf("v2 hosts: %w", err)
	}

	return c.buildTopology(clusterResp, hostResp), nil
}

func (c *Client) buildTopology(clusterResp V2ClusterResponse, hostResp V2HostResponse) *model.Cluster {
	clusterName := "PE Cluster"
	if len(clusterResp.Entities) > 0 {
		clusterName = clusterResp.Entities[0].Name
	}

	id := ""
	if len(clusterResp.Entities) > 0 {
		id = clusterResp.Entities[0].UUID
		if id == "" {
			id = clusterResp.Entities[0].ID
		}
	}
	if id == "" {
		id = "pe-" + c.base.Host
	}

	topo := &model.Cluster{
		ID:    id,
		Name:  clusterName,
		Kind:  model.KindPE,
		HasPC: false,
	}

	for i, h := range hostResp.Entities {
		hostStatus := mapV2Status(h.State)

		hostName := h.Name
		if hostName == "" {
			hostName = fmt.Sprintf("Host %d", i+1)
		}

		sw := v2SwitchName(h)
		topo.Nodes = append(topo.Nodes, model.Node{
			ID:     h.UUID,
			Name:   hostName,
			IP:     h.HypervisorAddr,
			Status: hostStatus,
			Role:   model.RoleHost,
			Switch: sw,
		})

		cvmName := fmt.Sprintf("CVM %d", i+1)
		cvmIP := h.CVMAddr
		topo.Nodes = append(topo.Nodes, model.Node{
			ID:     h.ServiceVMID,
			Name:   cvmName,
			IP:     cvmIP,
			Status: hostStatus,
			Role:   model.RoleCVM,
			Switch: sw,
		})

		topo.Connections = append(topo.Connections, model.Edge{
			From: h.ServiceVMID,
			To:   h.UUID,
		})
	}

	model.FinalizeSwitches(topo)
	return topo
}

func v2SwitchName(h V2Host) string {
	for _, nic := range h.HostNICs {
		if nic.SwitchName != "" {
			return nic.SwitchName
		}
		if nic.SwitchDeviceID != "" {
			return nic.SwitchDeviceID
		}
		if nic.SwitchMac != "" {
			return nic.SwitchMac
		}
	}
	return model.NullSwitch
}

func mapV2Status(state string) model.Status {
	switch strings.ToUpper(state) {
	case "NORMAL", "UP", "OK":
		return model.StatusUp
	case "DEGRADED":
		return model.StatusDegraded
	case "DOWN", "CRITICAL":
		return model.StatusDown
	default:
		return model.StatusUnknown
	}
}
