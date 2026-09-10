package v3

const (
	PathClustersList          = "/api/nutanix/v3/clusters/list"
	PathClusterGet            = "/api/nutanix/v3/clusters/%s"
	PathHostsList             = "/api/nutanix/v3/hosts/list"
	PathHostGet               = "/api/nutanix/v3/hosts/%s"
	PathSubnetsList           = "/api/nutanix/v3/subnets/list"
	PathAvailabilityZonesList = "/api/nutanix/v3/availability_zones/list"
)

// ListRequest is the common POST body for v3 list endpoints.
type ListRequest struct {
	Kind   string `json:"kind"`
	Length int    `json:"length,omitempty"`
	Offset int    `json:"offset,omitempty"`
}

// V3ClusterListResponse is the envelope from POST /clusters/list.
type V3ClusterListResponse struct {
	Entities []V3ClusterEntity `json:"entities"`
	Metadata struct {
		TotalMatches int `json:"total_matches"`
	} `json:"metadata"`
}

type V3ClusterEntity struct {
	Metadata V3EntityMetadata `json:"metadata"`
	Spec     V3ClusterSpec    `json:"spec"`
	Status   V3ClusterStatus  `json:"status"`
}

type V3EntityMetadata struct {
	UUID string `json:"uuid"`
	Kind string `json:"kind"`
}

type V3ClusterSpec struct {
	Name      string                 `json:"name"`
	Resources map[string]interface{} `json:"resources"`
}

type V3ClusterStatus struct {
	State     string                 `json:"state"`
	Name      string                 `json:"name"`
	Resources map[string]interface{} `json:"resources"`
}

// V3HostListResponse is the envelope from POST /hosts/list.
type V3HostListResponse struct {
	Entities []V3HostEntity `json:"entities"`
	Metadata struct {
		TotalMatches int `json:"total_matches"`
	} `json:"metadata"`
}

type V3HostEntity struct {
	Metadata V3EntityMetadata `json:"metadata"`
	Status   V3HostStatus     `json:"status"`
}

type V3HostStatus struct {
	State     string          `json:"state"`
	Name      string          `json:"name"`
	Resources V3HostResources `json:"resources"`
}

type V3HostResources struct {
	HypervisorIP string       `json:"hypervisor"`
	CVMAddress   V3IPAddress  `json:"controller_vm"`
	HostType     string       `json:"host_type"`
	ClusterRef   V3ClusterRef `json:"cluster_reference"`
}

type V3IPAddress struct {
	IP string `json:"ip"`
}

type V3ClusterRef struct {
	UUID string `json:"uuid"`
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// V3AZListResponse is the envelope from POST /availability_zones/list.
type V3AZListResponse struct {
	Entities []V3AZEntity `json:"entities"`
	Metadata struct {
		TotalMatches int `json:"total_matches"`
	} `json:"metadata"`
}

type V3AZEntity struct {
	Metadata V3EntityMetadata `json:"metadata"`
	Spec     V3AZSpec         `json:"spec"`
	Status   V3AZStatus       `json:"status"`
}

type V3AZSpec struct {
	Name      string                 `json:"name"`
	Resources map[string]interface{} `json:"resources"`
}

type V3AZStatus struct {
	Name      string                 `json:"name"`
	State     string                 `json:"state"`
	Resources map[string]interface{} `json:"resources"`
}

// AvailabilityZone is a normalized AZ discovered from v3.
type AvailabilityZone struct {
	ID            string
	Name          string
	ManagementURL string
	Host          string
	State         string
	PlaneType     string // Local, PC, ...
	DisplayName   string
}
