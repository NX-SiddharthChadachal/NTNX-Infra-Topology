package v2

const (
	PathClusters = "/api/nutanix/v2.0/clusters/"
	PathHosts    = "/api/nutanix/v2.0/hosts/"
	PathNetworks = "/api/nutanix/v2.0/networks/"
)

// V2ClusterResponse is the envelope returned by GET /clusters/.
type V2ClusterResponse struct {
	Entities []V2Cluster `json:"entities"`
	Metadata struct {
		TotalEntities int `json:"total_entities"`
	} `json:"metadata"`
}

type V2Cluster struct {
	ID                string   `json:"id"`
	UUID              string   `json:"uuid"`
	Name              string   `json:"name"`
	ClusterExternalIP string   `json:"cluster_external_ipaddress"`
	NumNodes          int      `json:"num_nodes"`
	OperationMode     string   `json:"operation_mode"`
	Hypervisors       []string `json:"hypervisor_types"`
}

// V2HostResponse is the envelope returned by GET /hosts/.
type V2HostResponse struct {
	Entities []V2Host `json:"entities"`
	Metadata struct {
		TotalEntities int `json:"total_entities"`
	} `json:"metadata"`
}

type V2Host struct {
	UUID           string      `json:"uuid"`
	Name           string      `json:"name"`
	ServiceVMID    string      `json:"service_vmid"`
	HypervisorAddr string      `json:"hypervisor_address"`
	CVMAddr        string      `json:"service_vmexternal_ip"`
	State          string      `json:"state"`
	NodeStatus     string      `json:"node_status"`
	HostNICs       []V2HostNIC `json:"host_nics"`
}

type V2HostNIC struct {
	SwitchDeviceID string `json:"switch_device_id"`
	SwitchMac      string `json:"switch_mac_addr"`
	SwitchName     string `json:"switch_name"`
}

// V2NetworkResponse is the envelope returned by GET /networks/.
type V2NetworkResponse struct {
	Entities []V2Network `json:"entities"`
	Metadata struct {
		TotalEntities int `json:"total_entities"`
	} `json:"metadata"`
}

type V2Network struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
	Vlan int    `json:"vlan_id"`
}
