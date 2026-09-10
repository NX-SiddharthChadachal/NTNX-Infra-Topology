package model

import (
	"sync"
	"time"
)

// StateSnapshot is a plain value type that can be freely copied.
// It is sent over channels from the fetcher to the UI.
// Selection lives in the TUI model so a poll does not reset the cursor.
type StateSnapshot struct {
	Inventory   Inventory
	Cluster     Cluster
	Topologies  map[string]Cluster
	APIVersion  string
	LastRefresh time.Time
	LastLatency time.Duration
	Errors      []string
	ActiveFetch bool
}

func NewStateSnapshot() StateSnapshot {
	return StateSnapshot{
		Cluster: Cluster{
			Name: "unknown",
		},
		Topologies: map[string]Cluster{},
		APIVersion: "none",
	}
}

// AppState is the mutex-protected state owned by the fetcher.
// Use Snapshot() to get a copy-safe value to send to the UI.
type AppState struct {
	mu   sync.RWMutex
	snap StateSnapshot
}

func NewAppState() *AppState {
	return &AppState{
		snap: NewStateSnapshot(),
	}
}

func (s *AppState) Snapshot() StateSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cp := s.snap
	cp.Inventory = s.snap.Inventory.Clone()
	cp.Cluster.Nodes = make([]Node, len(s.snap.Cluster.Nodes))
	copy(cp.Cluster.Nodes, s.snap.Cluster.Nodes)
	cp.Cluster.Connections = make([]Edge, len(s.snap.Cluster.Connections))
	copy(cp.Cluster.Connections, s.snap.Cluster.Connections)
	cp.Errors = make([]string, len(s.snap.Errors))
	copy(cp.Errors, s.snap.Errors)
	cp.Topologies = make(map[string]Cluster, len(s.snap.Topologies))
	for k, v := range s.snap.Topologies {
		cl := v
		cl.Nodes = make([]Node, len(v.Nodes))
		copy(cl.Nodes, v.Nodes)
		cl.Connections = make([]Edge, len(v.Connections))
		copy(cl.Connections, v.Connections)
		cp.Topologies[k] = cl
	}
	return cp
}

func (s *AppState) SetInventory(inv Inventory) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snap.Inventory = inv
}

func (s *AppState) SetCluster(c Cluster) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snap.Cluster = c
	if s.snap.Topologies == nil {
		s.snap.Topologies = map[string]Cluster{}
	}
	if c.ID != "" {
		s.snap.Topologies[c.ID] = c
	}
}

func (s *AppState) SetMeta(apiVersion string, latency time.Duration, errs []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snap.APIVersion = apiVersion
	s.snap.LastLatency = latency
	s.snap.LastRefresh = time.Now()
	s.snap.Errors = errs
}

func (s *AppState) SetActiveFetch(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snap.ActiveFetch = v
}
