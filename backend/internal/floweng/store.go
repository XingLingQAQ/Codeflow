package floweng

import (
	"fmt"
	"sync"
	"time"
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
	// revisions holds the in-process template revision history, mirroring the
	// durable flow_template_revisions table so an InMemoryEngine's semantics are
	// the same as a SQLite engine's (T3.01.a group 3). The slice is append-only:
	// nothing replaces or removes a revision.
	revisions map[TemplateID][]TemplateRevision
	// templates holds the current custom definitions, the counterpart of the
	// durable flow_templates table.
	templates map[TemplateID]CustomTemplate
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		flows:     make(map[string]*Flow),
		revisions: make(map[TemplateID][]TemplateRevision),
		templates: map[TemplateID]CustomTemplate{},
	}
}

// EnsureTemplateRevision implements the append rule of a template revision in
// process: the definition is encoded and hashed canonically, the latest
// revision is compared, and the next number is appended only when the content
// differs. Content a previous revision already froze resolves to that revision
// (the durable store's UNIQUE (template_id, content_hash) makes the same
// choice); a reverted definition therefore does not append a duplicate.
func (s *memoryStore) EnsureTemplateRevision(def templateDef, source TemplateRevisionSource) (int64, error) {
	hash, payload, err := canonicalTemplateHash(def)
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ensureRevisionLocked(def.ID, hash, payload, source), nil
}

// ensureRevisionLocked is the append rule once the content is encoded: if the
// latest revision already holds this hash (or an older one does — the content
// was frozen, then the definition was reverted), the existing number is
// returned and nothing is appended; otherwise the next number is appended. The
// caller holds s.mu.
func (s *memoryStore) ensureRevisionLocked(id TemplateID, hash string, payload []byte, source TemplateRevisionSource) int64 {
	history := s.revisions[id]
	if n := len(history); n > 0 && history[n-1].ContentHash == hash {
		return history[n-1].Revision
	}
	for i := range history {
		if history[i].ContentHash == hash {
			return history[i].Revision
		}
	}
	next := int64(1)
	if n := len(history); n > 0 {
		next = history[n-1].Revision + 1
	}
	s.revisions[id] = append(history, TemplateRevision{
		TemplateID:  id,
		Revision:    next,
		ContentHash: hash,
		Payload:     string(payload),
		Source:      source,
		CreatedAt:   time.Now().UTC(),
	})
	return next
}

// LatestTemplateRevision returns the newest revision number of a template, or 0
// when the store has none (unknown).
func (s *memoryStore) LatestTemplateRevision(id TemplateID) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if n := len(s.revisions[id]); n > 0 {
		return s.revisions[id][n-1].Revision, nil
	}
	return 0, nil
}

// GetTemplateRevision returns one revision, or nil when it does not exist.
func (s *memoryStore) GetTemplateRevision(id TemplateID, revision int64) (*TemplateRevision, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.revisions[id] {
		if s.revisions[id][i].Revision == revision {
			rec := s.revisions[id][i]
			return &rec, nil
		}
	}
	return nil, nil
}

// PutTemplate replaces the current custom definition in process and appends a
// revision for its canonical content — the same pair of writes SQLiteFlowStore
// performs in one transaction, so an InMemoryEngine's revision history is the
// one a durable engine would keep: the same content saved twice appends
// nothing, a changed definition appends the next revision. The registry itself
// (customTemplates) is package-level and is updated by SaveTemplate; this keeps
// the definition the template store would hold plus the history.
func (s *memoryStore) PutTemplate(def CustomTemplate) error {
	// The hash is computed before the lock: canonicalTemplateHash marshals the
	// definition and takes no store lock.
	hash, payload, err := canonicalTemplateHash(customTemplateDef(def))
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.templates == nil {
		s.templates = map[TemplateID]CustomTemplate{}
	}
	s.templates[def.ID] = def
	s.ensureRevisionLocked(def.ID, hash, payload, TemplateRevisionSourceCustom)
	return nil
}

// ListTemplateDefinitions returns the custom definitions this store holds.
func (s *memoryStore) ListTemplateDefinitions() ([]CustomTemplate, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]CustomTemplate, 0, len(s.templates))
	for _, def := range s.templates {
		out = append(out, def)
	}
	return out, nil
}

// DeleteTemplate removes the current custom definition; the revisions stay, as
// they do in the durable store.
func (s *memoryStore) DeleteTemplate(id TemplateID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.templates, id)
	return nil
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
//
// Put also enforces the one-active-project-flow rule against the map it holds,
// which is the in-process equivalent of the flows table's partial unique index:
// a second active project flow of the same project is refused with
// ErrActiveProjectFlowExists, and the refused document is not stored (the
// engine checks the same rule before it starts building the document; this is
// the backstop a caller that goes straight to the store meets).
func (s *memoryStore) Put(flow *Flow) error {
	if flow == nil || flow.ID == "" {
		return fmt.Errorf("flow id is required")
	}
	if err := normalizeFlowDocument(flow); err != nil {
		return fmt.Errorf("put flow: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	prev, ok := s.flows[flow.ID]
	rev := int64(1)
	if ok {
		rev = prev.Revision + 1
	}
	if flow.Kind == FlowKindProject && flow.Status == FlowStatusActive {
		for id, other := range s.flows {
			if id == flow.ID || other.Kind != FlowKindProject || other.Status != FlowStatusActive {
				continue
			}
			if other.ProjectID == flow.ProjectID {
				return activeProjectFlowError(id)
			}
		}
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
