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
	Delete(id string) error
}

// memoryStore is the default process-local store.
type memoryStore struct {
	mu    sync.RWMutex
	flows map[string]*Flow
}

func newMemoryStore() *memoryStore {
	return &memoryStore{flows: make(map[string]*Flow)}
}

// Put stores a copy of the document and assigns its revision.
//
// The revision rule is the store's, not the caller's, and it is the same one
// SQLiteFlowStore.Put applies: 1 for a Flow that is not stored yet, and the
// stored value plus one for every later Put. The value is written back into the
// caller's document so the copy the engine returns carries the revision that
// was just stored. Comparing the caller's revision against the stored one
// (compare-and-set) is T3.01.b, not this step: here the incoming value is
// ignored, exactly as it is on the SQLite side.
func (s *memoryStore) Put(flow *Flow) error {
	if flow == nil || flow.ID == "" {
		return fmt.Errorf("flow id is required")
	}
	if err := normalizeFlowDocument(flow); err != nil {
		return fmt.Errorf("put flow: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rev := int64(1)
	if prev, ok := s.flows[flow.ID]; ok {
		rev = prev.Revision + 1
	}
	flow.Revision = rev
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

func (s *memoryStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.flows[id]; !ok {
		return fmt.Errorf("flow not found: %s", id)
	}
	delete(s.flows, id)
	return nil
}
