package v3

import (
	"fmt"
	"strings"
	"time"

	"github.com/nutanix/ntnx-topo/internal/api"
	"github.com/nutanix/ntnx-topo/internal/model"
)

// Client talks to a Prism Central v3 API.
type Client struct {
	base *api.BaseClient
}

func NewClient(host, username, password string, insecure bool, timeout time.Duration) *Client {
	return &Client{
		base: api.NewBaseClient(host, username, password, insecure, timeout),
	}
}

func (c *Client) Host() string { return c.base.Host }

// VerifyLogin makes the smallest authenticated call we can to check credentials
// before anything else runs. A 401/403 means the login is wrong; any other error
// says nothing about the credentials, only that we could not ask.
func (c *Client) VerifyLogin() error {
	body := ListRequest{Kind: "cluster", Length: 1}
	var resp V3ClusterListResponse
	if _, err := c.base.DoJSON("POST", PathClustersList, body, &resp); err != nil {
		return err
	}
	return nil
}

func (c *Client) FetchTopology() (*model.Cluster, error) {
	inv, err := c.ListInventory()
	if err != nil {
		return nil, err
	}
	if len(inv.PEs) > 0 {
		return c.FetchPETopology(inv.PEs[0].ID)
	}
	return v4Overview(inv), nil
}

func v4Overview(pc *model.PCNode) *model.Cluster {
	topo := &model.Cluster{
		ID:    pc.ID,
		Name:  pc.Name,
		Kind:  model.KindPC,
		HasPC: true,
	}
	topo.Nodes = append(topo.Nodes, model.Node{
		ID: pc.ID, Name: pc.Name, IP: pc.IP, Status: pc.Status, Role: model.RolePC,
	})
	for _, pe := range pc.PEs {
		topo.Nodes = append(topo.Nodes, model.Node{
			ID: pe.ID, Name: pe.Name, IP: pe.IP, Status: pe.Status, Role: model.RolePE,
		})
		topo.Connections = append(topo.Connections, model.Edge{From: pc.ID, To: pe.ID})
	}
	return topo
}

func (c *Client) listClusters() (*V3ClusterListResponse, error) {
	body := ListRequest{Kind: "cluster", Length: 500}
	var resp V3ClusterListResponse
	if _, err := c.base.DoJSON("POST", PathClustersList, body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) listHosts() (*V3HostListResponse, error) {
	body := ListRequest{Kind: "host", Length: 500}
	var resp V3HostListResponse
	if _, err := c.base.DoJSON("POST", PathHostsList, body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListAvailabilityZones returns paired availability zones (local + remote PCs).
func (c *Client) ListAvailabilityZones() ([]AvailabilityZone, error) {
	body := ListRequest{Kind: "availability_zone", Length: 100}
	var resp V3AZListResponse
	if _, err := c.base.DoJSON("POST", PathAvailabilityZonesList, body, &resp); err != nil {
		return nil, err
	}
	var out []AvailabilityZone
	for _, e := range resp.Entities {
		url := resourceString(e.Status.Resources, "management_url")
		if url == "" {
			url = resourceString(e.Spec.Resources, "management_url")
		}
		name := e.Status.Name
		if name == "" {
			name = e.Spec.Name
		}
		plane := resourceString(e.Status.Resources, "management_plane_type")
		if plane == "" {
			plane = resourceString(e.Spec.Resources, "management_plane_type")
		}
		display := resourceString(e.Status.Resources, "display_name")
		if display == "" {
			display = resourceString(e.Spec.Resources, "display_name")
		}
		host := api.HostFromURL(url)
		if api.LooksLikeUUID(host) {
			host = ""
		}
		out = append(out, AvailabilityZone{
			ID:            e.Metadata.UUID,
			Name:          name,
			ManagementURL: url,
			Host:          host,
			State:         e.Status.State,
			PlaneType:     plane,
			DisplayName:   display,
		})
	}
	return out, nil
}

// ListInventory returns this Prism Central and registered PE clusters.
func (c *Client) ListInventory() (*model.PCNode, error) {
	clusterResp, err := c.listClusters()
	if err != nil {
		return nil, fmt.Errorf("v3 clusters: %w", err)
	}

	pc := &model.PCNode{
		InventoryItem: model.InventoryItem{
			ID:        "pc-" + c.base.Host,
			Name:      c.base.Host,
			IP:        c.base.Host,
			Status:    model.StatusUp,
			Kind:      model.KindPC,
			Reachable: true,
			CredsOK:   true,
		},
	}

	for _, e := range clusterResp.Entities {
		id := e.Metadata.UUID
		name := e.Status.Name
		if name == "" {
			name = e.Spec.Name
		}
		if name == "" {
			name = id
		}
		status := mapV3Status(e.Status.State)
		if v3IsPC(e.Status.Resources) || v3IsPC(e.Spec.Resources) {
			pc.ID = id
			if id == "" {
				pc.ID = "pc-" + c.base.Host
			}
			pc.Name = name
			pc.Status = status
			continue
		}
		if id == "" {
			id = "pe-" + name
		}
		pc.PEs = append(pc.PEs, model.PENode{
			InventoryItem: model.InventoryItem{
				ID:        id,
				Name:      name,
				Status:    status,
				Kind:      model.KindPE,
				Reachable: status != model.StatusDown,
				CredsOK:   true,
			},
		})
	}
	return pc, nil
}

// FetchPETopology builds CVM/host/TOR topology for hosts belonging to clusterID.
func (c *Client) FetchPETopology(clusterID string) (*model.Cluster, error) {
	clusterResp, err := c.listClusters()
	if err != nil {
		return nil, fmt.Errorf("v3 clusters: %w", err)
	}
	hostResp, err := c.listHosts()
	if err != nil {
		return nil, fmt.Errorf("v3 hosts: %w", err)
	}

	name := clusterID
	for _, e := range clusterResp.Entities {
		if e.Metadata.UUID == clusterID {
			name = e.Status.Name
			if name == "" {
				name = e.Spec.Name
			}
			break
		}
	}

	var hosts []V3HostEntity
	for _, h := range hostResp.Entities {
		ref := h.Status.Resources.ClusterRef.UUID
		if ref == "" || ref == clusterID {
			// If the API omits cluster_reference, include all hosts only when
			// there is a single PE; otherwise require a match.
			if ref == clusterID {
				hosts = append(hosts, h)
			}
		}
	}
	if len(hosts) == 0 {
		// Fallback: if nothing matched (missing refs), use all hosts.
		matched := 0
		for _, h := range hostResp.Entities {
			if h.Status.Resources.ClusterRef.UUID == clusterID {
				matched++
			}
		}
		if matched == 0 {
			hosts = hostResp.Entities
		}
	}

	return buildPETopology(clusterID, name, hosts), nil
}

func buildPETopology(id, name string, hosts []V3HostEntity) *model.Cluster {
	topo := &model.Cluster{
		ID:    id,
		Name:  name,
		Kind:  model.KindPE,
		HasPC: false,
	}

	for i, h := range hosts {
		hostStatus := mapV3Status(h.Status.State)
		hostName := h.Status.Name
		if hostName == "" {
			hostName = fmt.Sprintf("Host %d", i+1)
		}
		hostID := h.Metadata.UUID
		sw := model.NullSwitch
		topo.Nodes = append(topo.Nodes, model.Node{
			ID:     hostID,
			Name:   hostName,
			IP:     h.Status.Resources.HypervisorIP,
			Status: hostStatus,
			Role:   model.RoleHost,
			Switch: sw,
		})

		cvmID := fmt.Sprintf("cvm-%s", hostID)
		topo.Nodes = append(topo.Nodes, model.Node{
			ID:     cvmID,
			Name:   fmt.Sprintf("CVM %d", i+1),
			IP:     h.Status.Resources.CVMAddress.IP,
			Status: hostStatus,
			Role:   model.RoleCVM,
			Switch: sw,
		})
		topo.Connections = append(topo.Connections, model.Edge{From: cvmID, To: hostID})
	}

	model.FinalizeSwitches(topo)
	return topo
}

func mapV3Status(state string) model.Status {
	switch strings.ToUpper(state) {
	case "COMPLETE", "UP", "ON":
		return model.StatusUp
	case "DEGRADED":
		return model.StatusDegraded
	case "ERROR", "DOWN", "OFF":
		return model.StatusDown
	default:
		return model.StatusUnknown
	}
}

func v3IsPC(resources map[string]interface{}) bool {
	return containsString(resources, "PRISM_CENTRAL")
}

func containsString(v interface{}, needle string) bool {
	switch t := v.(type) {
	case string:
		return strings.Contains(strings.ToUpper(t), needle)
	case []interface{}:
		for _, item := range t {
			if containsString(item, needle) {
				return true
			}
		}
	case map[string]interface{}:
		for _, item := range t {
			if containsString(item, needle) {
				return true
			}
		}
	}
	return false
}

func resourceString(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// GetCluster fetches one cluster by UUID (v3 GET).
func (c *Client) GetCluster(uuid string) (*V3ClusterEntity, error) {
	if uuid == "" {
		return nil, fmt.Errorf("empty cluster uuid")
	}
	var ent V3ClusterEntity
	if _, err := c.base.DoJSON("GET", fmt.Sprintf(PathClusterGet, uuid), nil, &ent); err != nil {
		return nil, err
	}
	return &ent, nil
}

// GetHost fetches one host by UUID (v3 GET).
func (c *Client) GetHost(uuid string) (*V3HostEntity, error) {
	if uuid == "" {
		return nil, fmt.Errorf("empty host uuid")
	}
	var ent V3HostEntity
	if _, err := c.base.DoJSON("GET", fmt.Sprintf(PathHostGet, uuid), nil, &ent); err != nil {
		return nil, err
	}
	return &ent, nil
}

func ClusterFields(e V3ClusterEntity) []model.ConfigField {
	name := e.Status.Name
	if name == "" {
		name = e.Spec.Name
	}
	out := []model.ConfigField{}
	add := func(k, v string) {
		if strings.TrimSpace(v) == "" {
			return
		}
		out = append(out, model.ConfigField{Key: k, Value: v})
	}
	add("name", name)
	add("extId", e.Metadata.UUID)
	add("state", e.Status.State)
	add("external IP", resourceString(e.Status.Resources, "external_ip"))
	return out
}

func HostFields(e V3HostEntity) []model.ConfigField {
	out := []model.ConfigField{}
	add := func(k, v string) {
		if strings.TrimSpace(v) == "" {
			return
		}
		out = append(out, model.ConfigField{Key: k, Value: v})
	}
	add("name", e.Status.Name)
	add("extId", e.Metadata.UUID)
	add("hypervisor IP", e.Status.Resources.HypervisorIP)
	add("CVM IP", e.Status.Resources.CVMAddress.IP)
	add("type", e.Status.Resources.HostType)
	add("status", e.Status.State)
	return out
}
