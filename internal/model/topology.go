package model

import (
	"fmt"
	"strings"
)

type NodeRole string

const (
	RoleCVM  NodeRole = "CVM"
	RoleHost NodeRole = "HOST"
	RolePC   NodeRole = "PC"
	RolePE   NodeRole = "PE"
	RoleTOR  NodeRole = "TOR"
)

type Status string

const (
	StatusUp       Status = "UP"
	StatusDown     Status = "DOWN"
	StatusDegraded Status = "DEGRADED"
	StatusUnknown  Status = "UNKNOWN"
)

type ClusterKind string

const (
	KindPC ClusterKind = "pc"
	KindPE ClusterKind = "pe"
)

// NullSwitch is the display name when the API does not report a physical switch.
const NullSwitch = "null"

type Node struct {
	ID     string
	Name   string
	IP     string
	Status Status
	Role   NodeRole
	Switch string // physical switch name; "null" if unknown
}

type Edge struct {
	From string
	To   string
}

type Cluster struct {
	ID          string
	Name        string
	Kind        ClusterKind
	HasPC       bool
	Nodes       []Node
	Connections []Edge
}

// InventoryItem is a selectable row in the left-pane tree.
type InventoryItem struct {
	ID        string
	Name      string
	IP        string
	Status    Status
	Kind      ClusterKind
	Reachable bool
	CredsOK   bool
	Message   string
}

type PENode struct {
	InventoryItem
}

type PCNode struct {
	InventoryItem
	PEs []PENode
}

// PCPairing is a Prism Central / AZ pairing discovered from the local PC.
type PCPairing struct {
	FromID string
	ToID   string
}

// Inventory is the forest shown in the left pane.
type Inventory struct {
	PCs      []PCNode
	PEs      []PENode // standalone PE-only entries
	Pairings []PCPairing
}

// CVMs returns only CVM-role nodes.
func (c *Cluster) CVMs() []Node {
	var out []Node
	for _, n := range c.Nodes {
		if n.Role == RoleCVM {
			out = append(out, n)
		}
	}
	return out
}

// Hosts returns only HOST-role nodes.
func (c *Cluster) Hosts() []Node {
	var out []Node
	for _, n := range c.Nodes {
		if n.Role == RoleHost {
			out = append(out, n)
		}
	}
	return out
}

// PEs returns only PE-role nodes (used for PC overview).
func (c *Cluster) PEs() []Node {
	var out []Node
	for _, n := range c.Nodes {
		if n.Role == RolePE {
			out = append(out, n)
		}
	}
	return out
}

// PC returns the Prism Central node, if present.
func (c *Cluster) PC() *Node {
	for i := range c.Nodes {
		if c.Nodes[i].Role == RolePC {
			return &c.Nodes[i]
		}
	}
	return nil
}

// TOR returns the first switch node, if present.
func (c *Cluster) TOR() *Node {
	for i := range c.Nodes {
		if c.Nodes[i].Role == RoleTOR {
			return &c.Nodes[i]
		}
	}
	return nil
}

// Switches returns every physical-switch node.
func (c *Cluster) Switches() []Node {
	var out []Node
	for _, n := range c.Nodes {
		if n.Role == RoleTOR {
			out = append(out, n)
		}
	}
	return out
}

// Rack is one imaginary rack: a physical switch and the hosts/CVMs on it.
type Rack struct {
	Switch string
	Hosts  []Node
	CVMs   []Node
}

// Racks groups hosts (and their CVMs) by physical switch, in first-seen order.
func (c *Cluster) Racks() []Rack {
	hosts := c.Hosts()
	cvms := c.CVMs()
	cvmByHost := map[string]Node{}
	for _, cvm := range cvms {
		if strings.HasPrefix(cvm.ID, "cvm-") {
			cvmByHost[strings.TrimPrefix(cvm.ID, "cvm-")] = cvm
		}
	}

	seen := map[string]int{}
	var racks []Rack
	for _, h := range hosts {
		sw := h.Switch
		if sw == "" {
			sw = NullSwitch
		}
		if i, ok := seen[sw]; ok {
			racks[i].Hosts = append(racks[i].Hosts, h)
			if cvm, ok := cvmByHost[h.ID]; ok {
				racks[i].CVMs = append(racks[i].CVMs, cvm)
			}
			continue
		}
		seen[sw] = len(racks)
		r := Rack{Switch: sw, Hosts: []Node{h}}
		if cvm, ok := cvmByHost[h.ID]; ok {
			r.CVMs = append(r.CVMs, cvm)
		}
		racks = append(racks, r)
	}
	return racks
}

// DisplaySwitch returns name, or "null" when the API omitted it.
func DisplaySwitch(name string) string {
	if strings.TrimSpace(name) == "" {
		return NullSwitch
	}
	return name
}

// FinalizeSwitches replaces synthetic TOR nodes with one switch node per
// distinct host.Switch and wires host→switch edges.
func FinalizeSwitches(topo *Cluster) {
	var kept []Node
	for _, n := range topo.Nodes {
		if n.Role != RoleTOR {
			if n.Role == RoleHost && n.Switch == "" {
				n.Switch = NullSwitch
			}
			kept = append(kept, n)
		}
	}
	var edges []Edge
	for _, e := range topo.Connections {
		if e.To == "tor-switch" {
			continue
		}
		edges = append(edges, e)
	}
	topo.Nodes = kept
	topo.Connections = edges

	for i, r := range topo.Racks() {
		id := fmt.Sprintf("switch-%d", i)
		topo.Nodes = append(topo.Nodes, Node{
			ID:     id,
			Name:   DisplaySwitch(r.Switch),
			Status: StatusUp,
			Role:   RoleTOR,
			Switch: DisplaySwitch(r.Switch),
		})
		for _, h := range r.Hosts {
			topo.Connections = append(topo.Connections, Edge{From: h.ID, To: id})
		}
	}
}

// FirstSelectable returns the ID of the first reachable, credentialed item.
func (inv Inventory) FirstSelectable() string {
	for _, pc := range inv.PCs {
		if pc.Reachable && pc.CredsOK {
			return pc.ID
		}
		for _, pe := range pc.PEs {
			if pe.Reachable && pe.CredsOK {
				return pe.ID
			}
		}
	}
	for _, pe := range inv.PEs {
		if pe.Reachable && pe.CredsOK {
			return pe.ID
		}
	}
	return ""
}

// FirstWithIP returns the first PC or PE that has an address, including unhealthy hosts.
func (inv Inventory) FirstWithIP() string {
	for _, pc := range inv.PCs {
		if pc.IP != "" {
			return pc.ID
		}
		for _, pe := range pc.PEs {
			if pe.IP != "" {
				return pe.ID
			}
		}
	}
	for _, pe := range inv.PEs {
		if pe.IP != "" {
			return pe.ID
		}
	}
	return ""
}

// Find returns the inventory item with the given ID.
func (inv Inventory) Find(id string) (item InventoryItem, ok bool) {
	for _, pc := range inv.PCs {
		if pc.ID == id {
			return pc.InventoryItem, true
		}
		for _, pe := range pc.PEs {
			if pe.ID == id {
				return pe.InventoryItem, true
			}
		}
	}
	for _, pe := range inv.PEs {
		if pe.ID == id {
			return pe.InventoryItem, true
		}
	}
	return InventoryItem{}, false
}

// FindPC returns the PC node with the given ID.
func (inv Inventory) FindPC(id string) (PCNode, bool) {
	for _, pc := range inv.PCs {
		if pc.ID == id {
			return pc, true
		}
	}
	return PCNode{}, false
}

// FindPE returns the PE node with the given ID and the parent PC ID (empty if standalone).
func (inv Inventory) FindPE(id string) (pe PENode, parentID string, ok bool) {
	for _, pc := range inv.PCs {
		for _, child := range pc.PEs {
			if child.ID == id {
				return child, pc.ID, true
			}
		}
	}
	for _, child := range inv.PEs {
		if child.ID == id {
			return child, "", true
		}
	}
	return PENode{}, "", false
}

// Clone returns a deep copy of the inventory.
func (inv Inventory) Clone() Inventory {
	out := Inventory{
		PCs:      make([]PCNode, len(inv.PCs)),
		PEs:      make([]PENode, len(inv.PEs)),
		Pairings: append([]PCPairing(nil), inv.Pairings...),
	}
	for i, pc := range inv.PCs {
		out.PCs[i] = pc
		out.PCs[i].PEs = append([]PENode(nil), pc.PEs...)
	}
	copy(out.PEs, inv.PEs)
	return out
}

// HasItem reports whether an item with the given ID exists.
func (inv Inventory) HasItem(id string) bool {
	_, ok := inv.Find(id)
	return ok
}

// EntityKind is a clickable topology node.
type EntityKind string

const (
	EntityPC     EntityKind = "pc"
	EntityPE     EntityKind = "pe"
	EntityHost   EntityKind = "host"
	EntityCVM    EntityKind = "cvm"
	EntitySwitch EntityKind = "switch"
)

// EntityRef identifies a topology node for the Config pane.
type EntityRef struct {
	Kind      EntityKind
	ID        string
	ClusterID string
	Name      string
	IP        string
	Switch    string
}

// ConfigField is one key/value row in the Config pane.
type ConfigField struct {
	Key   string
	Value string
}
