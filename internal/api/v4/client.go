package v4

import (
	"fmt"
	"strings"
	"time"

	sdkapi "github.com/nutanix/ntnx-api-golang-clients/clustermgmt-go-client/v4/api"
	sdkclient "github.com/nutanix/ntnx-api-golang-clients/clustermgmt-go-client/v4/client"
	sdkconfig "github.com/nutanix/ntnx-api-golang-clients/clustermgmt-go-client/v4/models/clustermgmt/v4/config"
	"github.com/nutanix/ntnx-topo/internal/model"
)

// Client wraps the Nutanix v4 Go SDK for cluster management.
type Client struct {
	clustersAPI *sdkapi.ClustersApi
	cvmsAPI     *sdkapi.CvmsApi
	host        string
}

func NewClient(host, username, password string, insecure bool, timeout time.Duration) *Client {
	apiClient := sdkclient.NewApiClient()
	apiClient.Host = host
	apiClient.Port = 9440
	apiClient.Username = username
	apiClient.Password = password
	apiClient.VerifySSL = !insecure
	apiClient.ReadTimeout = timeout
	apiClient.ConnectTimeout = timeout
	apiClient.Scheme = "https"
	apiClient.Debug = false
	apiClient.LoggerFile = "ntnx-topo.log"

	return &Client{
		clustersAPI: sdkapi.NewClustersApi(apiClient),
		cvmsAPI:     sdkapi.NewCvmsApi(apiClient),
		host:        host,
	}
}

func (c *Client) Host() string { return c.host }

// ListInventory returns this Prism Central and the PE clusters it manages.
func (c *Client) ListInventory() (*model.PCNode, error) {
	clusters, err := c.listClusters()
	if err != nil {
		return nil, fmt.Errorf("v4 clusters: %w", err)
	}

	pc := model.PCNode{
		InventoryItem: model.InventoryItem{
			ID:        "pc-" + c.host,
			Name:      c.host,
			IP:        c.host,
			Status:    model.StatusUp,
			Kind:      model.KindPC,
			Reachable: true,
			CredsOK:   true,
		},
	}

	for _, cl := range clusters {
		id := ptrStr(cl.ExtId)
		name := ptrStr(cl.Name)
		if name == "" {
			name = id
		}
		status := mapClusterStatus(cl)

		if classifyCluster(cl) == model.KindPC {
			pc.ID = id
			if id == "" {
				pc.ID = "pc-" + c.host
			}
			pc.Name = name
			pc.Status = status
			continue
		}

		peID := id
		if peID == "" {
			peID = "pe-" + name
		}
		pc.PEs = append(pc.PEs, model.PENode{
			InventoryItem: model.InventoryItem{
				ID:        peID,
				Name:      name,
				Status:    status,
				Kind:      model.KindPE,
				Reachable: status != model.StatusDown,
				CredsOK:   true,
			},
		})
	}

	return &pc, nil
}

// FetchPETopology builds the CVM/host/switch topology for one PE cluster.
func (c *Client) FetchPETopology(clusterExtId string) (*model.Cluster, error) {
	hosts, err := c.listHostsByCluster(clusterExtId)
	if err != nil {
		return nil, fmt.Errorf("v4 hosts: %w", err)
	}

	name := clusterExtId
	if clusters, listErr := c.listClusters(); listErr == nil {
		for _, cl := range clusters {
			if ptrStr(cl.ExtId) == clusterExtId && cl.Name != nil {
				name = *cl.Name
				break
			}
		}
	}

	switches := c.hostSwitchNames(clusterExtId, hosts)
	return buildPETopology(clusterExtId, name, hosts, switches), nil
}

// FetchTopology is a legacy entry point: first PE if any, otherwise PC overview.
func (c *Client) FetchTopology() (*model.Cluster, error) {
	inv, err := c.ListInventory()
	if err != nil {
		return nil, err
	}
	if len(inv.PEs) > 0 {
		return c.FetchPETopology(inv.PEs[0].ID)
	}
	return PCOverview(*inv), nil
}

func (c *Client) listClusters() ([]sdkconfig.Cluster, error) {
	resp, err := c.clustersAPI.ListClusters(nil, nil, nil, nil, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	data := resp.GetData()
	if data == nil {
		return nil, nil
	}
	if clusters, ok := data.([]sdkconfig.Cluster); ok {
		return clusters, nil
	}
	return nil, fmt.Errorf("unexpected cluster list response type: %T", data)
}

func (c *Client) listHostsByCluster(clusterExtId string) ([]sdkconfig.Host, error) {
	id := clusterExtId
	resp, err := c.clustersAPI.ListHostsByClusterId(&id, nil, nil, nil, nil, nil, nil)
	if err != nil {
		return c.listHostsFiltered(clusterExtId)
	}
	data := resp.GetData()
	if data == nil {
		return nil, nil
	}
	if hosts, ok := data.([]sdkconfig.Host); ok {
		return hosts, nil
	}
	return nil, fmt.Errorf("unexpected host list response type: %T", data)
}

func (c *Client) listHostsFiltered(clusterExtId string) ([]sdkconfig.Host, error) {
	resp, err := c.clustersAPI.ListHosts(nil, nil, nil, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	data := resp.GetData()
	if data == nil {
		return nil, nil
	}
	hosts, ok := data.([]sdkconfig.Host)
	if !ok {
		return nil, fmt.Errorf("unexpected host list response type: %T", data)
	}
	var out []sdkconfig.Host
	for _, h := range hosts {
		if h.Cluster != nil && ptrStr(h.Cluster.Uuid) == clusterExtId {
			out = append(out, h)
		}
	}
	return out, nil
}

func classifyCluster(c sdkconfig.Cluster) model.ClusterKind {
	if c.Config == nil {
		return model.KindPE
	}
	for _, f := range c.Config.ClusterFunction {
		if f == sdkconfig.CLUSTERFUNCTIONREF_PRISM_CENTRAL {
			return model.KindPC
		}
	}
	return model.KindPE
}

func mapClusterStatus(c sdkconfig.Cluster) model.Status {
	if c.Config != nil && c.Config.IsAvailable != nil {
		if *c.Config.IsAvailable {
			return model.StatusUp
		}
		return model.StatusDown
	}
	return model.StatusUnknown
}

func buildPETopology(id, name string, hosts []sdkconfig.Host, switches map[string]string) *model.Cluster {
	topo := &model.Cluster{
		ID:    id,
		Name:  name,
		Kind:  model.KindPE,
		HasPC: false,
	}

	for i, h := range hosts {
		hostID := ptrStr(h.ExtId)
		hostName := ptrStr(h.HostName)
		if hostName == "" {
			hostName = fmt.Sprintf("Host %d", i+1)
		}

		hostIP := extractIP(h.Hypervisor)
		hostStatus := mapHostStatus(h)
		sw := model.DisplaySwitch(switches[hostID])

		topo.Nodes = append(topo.Nodes, model.Node{
			ID:     hostID,
			Name:   hostName,
			IP:     hostIP,
			Status: hostStatus,
			Role:   model.RoleHost,
			Switch: sw,
		})

		cvmID := fmt.Sprintf("cvm-%s", hostID)
		cvmIP := extractCVMIP(h.ControllerVm)
		topo.Nodes = append(topo.Nodes, model.Node{
			ID:     cvmID,
			Name:   fmt.Sprintf("CVM %d", i+1),
			IP:     cvmIP,
			Status: hostStatus,
			Role:   model.RoleCVM,
			Switch: sw,
		})

		topo.Connections = append(topo.Connections, model.Edge{From: cvmID, To: hostID})
	}

	model.FinalizeSwitches(topo)
	return topo
}

func (c *Client) hostSwitchNames(clusterExtId string, hosts []sdkconfig.Host) map[string]string {
	out := map[string]string{}
	nics, err := c.listAllHostNics()
	if err != nil || len(nics) == 0 {
		for _, h := range hosts {
			id := ptrStr(h.ExtId)
			if id == "" {
				continue
			}
			hostNics, nerr := c.listNicsByHost(clusterExtId, id)
			if nerr != nil {
				out[id] = model.NullSwitch
				continue
			}
			out[id] = switchNameFromNics(hostNics)
		}
		return out
	}
	byHost := map[string][]sdkconfig.HostNic{}
	for _, nic := range nics {
		hid := ptrStr(nic.NodeUuid)
		if hid == "" {
			continue
		}
		byHost[hid] = append(byHost[hid], nic)
	}
	for _, h := range hosts {
		id := ptrStr(h.ExtId)
		out[id] = switchNameFromNics(byHost[id])
	}
	return out
}

func (c *Client) listAllHostNics() ([]sdkconfig.HostNic, error) {
	resp, err := c.clustersAPI.ListHostNics(nil, nil, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	data := resp.GetData()
	if data == nil {
		return nil, nil
	}
	nics, ok := data.([]sdkconfig.HostNic)
	if !ok {
		return nil, fmt.Errorf("unexpected host nic list type: %T", data)
	}
	return nics, nil
}

func (c *Client) listNicsByHost(clusterExtId, hostExtId string) ([]sdkconfig.HostNic, error) {
	cid, hid := clusterExtId, hostExtId
	resp, err := c.clustersAPI.ListHostNicsByHostId(&cid, &hid, nil, nil, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	data := resp.GetData()
	if data == nil {
		return nil, nil
	}
	nics, ok := data.([]sdkconfig.HostNic)
	if !ok {
		return nil, fmt.Errorf("unexpected host nic list type: %T", data)
	}
	return nics, nil
}

func switchNameFromNics(nics []sdkconfig.HostNic) string {
	counts := map[string]int{}
	var order []string
	for _, nic := range nics {
		name := nicSwitchName(nic)
		if name == "" || name == model.NullSwitch {
			continue
		}
		if _, ok := counts[name]; !ok {
			order = append(order, name)
		}
		counts[name]++
	}
	if len(order) == 0 {
		return model.NullSwitch
	}
	best := order[0]
	for _, n := range order[1:] {
		if counts[n] > counts[best] {
			best = n
		}
	}
	return best
}

func nicSwitchName(nic sdkconfig.HostNic) string {
	if s := ptrStr(nic.SwitchDeviceId); s != "" {
		return s
	}
	if s := ptrStr(nic.SwitchVendorInfo); s != "" {
		return s
	}
	if s := ptrStr(nic.SwitchMacAddress); s != "" {
		return s
	}
	for _, iface := range nic.AttachedSwitchInterfaceList {
		if s := ptrStr(iface.SwitchInterfaceName); s != "" {
			return s
		}
		if s := ptrStr(iface.SwitchUuid); s != "" {
			return s
		}
	}
	return model.NullSwitch
}

// PCOverview builds a PC-plus-its-PEs topology from inventory (no host calls).
func PCOverview(pc model.PCNode) *model.Cluster {
	topo := &model.Cluster{
		ID:    pc.ID,
		Name:  pc.Name,
		Kind:  model.KindPC,
		HasPC: true,
	}
	topo.Nodes = append(topo.Nodes, model.Node{
		ID:     pc.ID,
		Name:   pc.Name,
		IP:     pc.IP,
		Status: pc.Status,
		Role:   model.RolePC,
	})
	for _, pe := range pc.PEs {
		topo.Nodes = append(topo.Nodes, model.Node{
			ID:     pe.ID,
			Name:   pe.Name,
			IP:     pe.IP,
			Status: pe.Status,
			Role:   model.RolePE,
		})
		topo.Connections = append(topo.Connections, model.Edge{From: pc.ID, To: pe.ID})
	}
	return topo
}

func extractIP(hyp *sdkconfig.HypervisorReference) string {
	if hyp == nil || hyp.ExternalAddress == nil || hyp.ExternalAddress.Ipv4 == nil {
		return ""
	}
	return ptrStr(hyp.ExternalAddress.Ipv4.Value)
}

func extractCVMIP(cvm *sdkconfig.ControllerVmReference) string {
	if cvm == nil || cvm.ExternalAddress == nil || cvm.ExternalAddress.Ipv4 == nil {
		return ""
	}
	return ptrStr(cvm.ExternalAddress.Ipv4.Value)
}

func mapHostStatus(h sdkconfig.Host) model.Status {
	if h.IsDegraded != nil && *h.IsDegraded {
		return model.StatusDegraded
	}
	if h.Hypervisor != nil && h.Hypervisor.AcropolisConnectionState != nil {
		switch *h.Hypervisor.AcropolisConnectionState {
		case sdkconfig.ACROPOLISCONNECTIONSTATE_CONNECTED:
			return model.StatusUp
		case sdkconfig.ACROPOLISCONNECTIONSTATE_DISCONNECTED:
			return model.StatusDown
		}
	}
	return model.StatusUnknown
}

func ptrStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func ptrBool(b *bool) string {
	if b == nil {
		return ""
	}
	if *b {
		return "true"
	}
	return "false"
}

// GetClusterById wraps GET /api/clustermgmt/v4.2/config/clusters/{extId}.
func (c *Client) GetClusterById(extId string) (*sdkconfig.Cluster, error) {
	if extId == "" {
		return nil, fmt.Errorf("empty cluster extId")
	}
	id := extId
	resp, err := c.clustersAPI.GetClusterById(&id, nil)
	if err != nil {
		return nil, err
	}
	switch cl := resp.GetData().(type) {
	case sdkconfig.Cluster:
		return &cl, nil
	case *sdkconfig.Cluster:
		if cl == nil {
			return nil, fmt.Errorf("empty cluster")
		}
		return cl, nil
	default:
		return nil, fmt.Errorf("unexpected cluster type %T", resp.GetData())
	}
}

// GetHostById wraps GET .../clusters/{clusterExtId}/hosts/{extId}.
func (c *Client) GetHostById(clusterExtId, hostExtId string) (*sdkconfig.Host, error) {
	if clusterExtId == "" || hostExtId == "" {
		return nil, fmt.Errorf("empty cluster or host extId")
	}
	cid, hid := clusterExtId, hostExtId
	resp, err := c.clustersAPI.GetHostById(&cid, &hid)
	if err != nil {
		return nil, err
	}
	switch h := resp.GetData().(type) {
	case sdkconfig.Host:
		return &h, nil
	case *sdkconfig.Host:
		if h == nil {
			return nil, fmt.Errorf("empty host")
		}
		return h, nil
	default:
		return nil, fmt.Errorf("unexpected host type %T", resp.GetData())
	}
}

// ClusterFields extracts Config-pane rows from a v4 cluster.
func ClusterFields(cl sdkconfig.Cluster) []model.ConfigField {
	var out []model.ConfigField
	add := func(k, v string) {
		if strings.TrimSpace(v) == "" {
			return
		}
		out = append(out, model.ConfigField{Key: k, Value: v})
	}
	add("name", ptrStr(cl.Name))
	add("extId", ptrStr(cl.ExtId))
	if cl.Config != nil {
		var fns []string
		for _, f := range cl.Config.ClusterFunction {
			n := f.GetName()
			if n == "" || n == "$UNKNOWN" || n == "$REDACTED" {
				continue
			}
			fns = append(fns, n)
		}
		add("functions", strings.Join(fns, ", "))
		if cl.Config.BuildInfo != nil {
			ver := ptrStr(cl.Config.BuildInfo.FullVersion)
			if ver == "" {
				ver = ptrStr(cl.Config.BuildInfo.Version)
			}
			add("version", ver)
		}
		for _, sm := range cl.Config.ClusterSoftwareMap {
			label := "software"
			if sm.SoftwareType != nil {
				label = sm.SoftwareType.GetName()
			}
			add(label, ptrStr(sm.Version))
		}
		add("available", ptrBool(cl.Config.IsAvailable))
	}
	if cl.Network != nil {
		if cl.Network.ExternalAddress != nil && cl.Network.ExternalAddress.Ipv4 != nil {
			add("external IP", ptrStr(cl.Network.ExternalAddress.Ipv4.Value))
		}
		add("fqdn", ptrStr(cl.Network.Fqdn))
	}
	return out
}

// HostFields extracts Config-pane rows from a v4 host.
func HostFields(h sdkconfig.Host) []model.ConfigField {
	var out []model.ConfigField
	add := func(k, v string) {
		if strings.TrimSpace(v) == "" {
			return
		}
		out = append(out, model.ConfigField{Key: k, Value: v})
	}
	add("name", ptrStr(h.HostName))
	add("extId", ptrStr(h.ExtId))
	add("hypervisor IP", extractIP(h.Hypervisor))
	add("CVM IP", extractCVMIP(h.ControllerVm))
	if h.HostType != nil {
		n := h.HostType.GetName()
		if n != "$UNKNOWN" && n != "$REDACTED" {
			add("type", n)
		}
	}
	if h.NodeStatus != nil {
		n := h.NodeStatus.GetName()
		if n != "$UNKNOWN" && n != "$REDACTED" {
			add("status", n)
		}
	}
	return out
}

// SwitchFields builds Config rows from already-fetched NICs matching switchName.
func SwitchFields(switchName string, nics []sdkconfig.HostNic, hostNames map[string]string) []model.ConfigField {
	var out []model.ConfigField
	add := func(k, v string) {
		if strings.TrimSpace(v) == "" {
			return
		}
		out = append(out, model.ConfigField{Key: k, Value: v})
	}
	disp := model.DisplaySwitch(switchName)
	add("name", disp)
	seenHost := map[string]bool{}
	var hosts []string
	var device, vendor, mac string
	for _, nic := range nics {
		got := nicSwitchName(nic)
		if got != disp && got != switchName {
			continue
		}
		if device == "" {
			device = ptrStr(nic.SwitchDeviceId)
		}
		if vendor == "" {
			vendor = ptrStr(nic.SwitchVendorInfo)
		}
		if mac == "" {
			mac = ptrStr(nic.SwitchMacAddress)
		}
		hid := ptrStr(nic.NodeUuid)
		if hid == "" || seenHost[hid] {
			continue
		}
		seenHost[hid] = true
		label := hostNames[hid]
		if label == "" {
			label = hid
		}
		hosts = append(hosts, label)
	}
	add("device id", device)
	add("vendor", vendor)
	add("MAC", mac)
	if len(hosts) > 0 {
		add("attached hosts", strings.Join(hosts, ", "))
	}
	return out
}

// ListHostNics returns all host NICs visible to this PC.
func (c *Client) ListHostNics() ([]sdkconfig.HostNic, error) {
	return c.listAllHostNics()
}
