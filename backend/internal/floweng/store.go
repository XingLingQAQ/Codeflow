package floweng

import (
	"fmt"
	"sync"
)

// FlowStore persists Flow documents. Implementations must be safe for concurrent use
// when paired with Engine-level RMW locking.
type FlowStore interface {
	Put(flow *Flow) error
	Get(id string) (*Flow, error)
	List(projectID string) ([]*Flow, error)
}

// memoryStore is the default process-local store.
type memoryStore struct {
	mu    sync.RWMutex
	flows map[string]*Flow
}

func newMemoryStore() *memoryStore {
	return &memoryStore{flows: make(map[string]*Flow)}
}

func (s *memoryStore) Put(flow *Flow) error {
	if flow == nil || flow.ID == "" {
		return fmt.Errorf("flow id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flows[flow.ID] = cloneFlow(flow)
	return nil
}

func (s *memoryStore) Get(id string) (*Flow, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	f, ok := s.flows[id]
	if !ok {
		return nil, fmt.Errorf("flow not found: %s", id)
	}
	return cloneFlow(f), nil
}

func (s *memoryStore) List(projectID string) ([]*Flow, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Flow, 0)
	for _, f := range s.flows {
		if projectID == "" || f.ProjectID == projectID {
			out = append(out, cloneFlow(f))
		}
	}
	return out, nil
}
