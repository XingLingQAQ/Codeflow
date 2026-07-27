package debate

import (
	"context"
	"sync"
)

// SQLiteDebateManager is a durable IDebateManager. It embeds the in-memory
// manager as the source of truth and mirrors every mutation write-through to a
// SQLite table. All rows are loaded into memory at startup; each mutation
// persists the full debate document. Persistence failures are returned to the
// caller (fail loudly) and the in-memory change is rolled back so memory and
// disk stay consistent.
type SQLiteDebateManager struct {
	*InMemoryDebateManager
	store *sqliteDebateStore
	// mu serializes mutation+persist so the store never lags a completed write.
	// It is distinct from the embedded manager's lock (which guards the map and
	// still protects concurrent reads).
	mu sync.Mutex
}

var _ IDebateManager = (*SQLiteDebateManager)(nil)

// NewSQLiteDebateManager opens a durable debate manager at dbPath
// (e.g. data/debates.db). Existing rows are loaded into memory.
func NewSQLiteDebateManager(dbPath string) (*SQLiteDebateManager, error) {
	store, err := openSQLiteDebateStore(dbPath)
	if err != nil {
		return nil, err
	}
	m := &SQLiteDebateManager{
		InMemoryDebateManager: NewInMemoryDebateManager(),
		store:                 store,
	}
	loaded, err := store.loadAll()
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	for _, d := range loaded {
		m.InMemoryDebateManager.debates[d.ID] = d
	}
	return m, nil
}

// Close releases the durable store.
func (m *SQLiteDebateManager) Close() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.store == nil {
		return nil
	}
	err := m.store.Close()
	m.store = nil
	return err
}

// CreateDebate creates a debate in memory then persists it.
func (m *SQLiteDebateManager) CreateDebate(ctx context.Context, req *DebateCreateRequest) (*Debate, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, err := m.InMemoryDebateManager.CreateDebate(ctx, req)
	if err != nil {
		return nil, err
	}
	if err := m.store.put(d); err != nil {
		m.removeDebate(d.ID)
		return nil, err
	}
	return d, nil
}

// NextRound advances a round then persists the debate.
func (m *SQLiteDebateManager) NextRound(ctx context.Context, debateID string, req *NextRoundRequest) (*Debate, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	prev, _ := m.InMemoryDebateManager.GetDebate(ctx, debateID)
	d, err := m.InMemoryDebateManager.NextRound(ctx, debateID, req)
	if err != nil {
		return nil, err
	}
	if err := m.store.put(d); err != nil {
		m.restoreDebate(prev)
		return nil, err
	}
	return d, nil
}

// ResolveConflict resolves a conflict then persists the debate.
func (m *SQLiteDebateManager) ResolveConflict(ctx context.Context, debateID, conflictID string, req *ResolveConflictRequest) (*Conflict, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	prev, _ := m.InMemoryDebateManager.GetDebate(ctx, debateID)
	c, err := m.InMemoryDebateManager.ResolveConflict(ctx, debateID, conflictID, req)
	if err != nil {
		return nil, err
	}
	if err := m.persist(ctx, debateID); err != nil {
		m.restoreDebate(prev)
		return nil, err
	}
	return c, nil
}

// ProposeSolution records a solution then persists the debate.
func (m *SQLiteDebateManager) ProposeSolution(ctx context.Context, debateID string, req *ProposeSolutionRequest) (*Solution, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	prev, _ := m.InMemoryDebateManager.GetDebate(ctx, debateID)
	s, err := m.InMemoryDebateManager.ProposeSolution(ctx, debateID, req)
	if err != nil {
		return nil, err
	}
	if err := m.persist(ctx, debateID); err != nil {
		m.restoreDebate(prev)
		return nil, err
	}
	return s, nil
}

// SelectSolution selects a solution then persists the debate.
func (m *SQLiteDebateManager) SelectSolution(ctx context.Context, debateID string, req *SelectSolutionRequest) (*Debate, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	prev, _ := m.InMemoryDebateManager.GetDebate(ctx, debateID)
	d, err := m.InMemoryDebateManager.SelectSolution(ctx, debateID, req)
	if err != nil {
		return nil, err
	}
	if err := m.store.put(d); err != nil {
		m.restoreDebate(prev)
		return nil, err
	}
	return d, nil
}

// AppendContribution appends a contribution then persists the debate.
func (m *SQLiteDebateManager) AppendContribution(ctx context.Context, debateID string, c PartyContribution) (*Debate, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	prev, _ := m.InMemoryDebateManager.GetDebate(ctx, debateID)
	d, err := m.InMemoryDebateManager.AppendContribution(ctx, debateID, c)
	if err != nil {
		return nil, err
	}
	if err := m.store.put(d); err != nil {
		m.restoreDebate(prev)
		return nil, err
	}
	return d, nil
}

// persist writes the current in-memory state of a debate to the store.
func (m *SQLiteDebateManager) persist(ctx context.Context, id string) error {
	d, err := m.InMemoryDebateManager.GetDebate(ctx, id)
	if err != nil {
		return err
	}
	if d == nil {
		return nil
	}
	return m.store.put(d)
}

// removeDebate drops a debate from the in-memory map (create rollback).
func (m *SQLiteDebateManager) removeDebate(id string) {
	m.InMemoryDebateManager.mu.Lock()
	delete(m.InMemoryDebateManager.debates, id)
	m.InMemoryDebateManager.mu.Unlock()
}

// restoreDebate reinstates a pre-mutation snapshot (update rollback).
func (m *SQLiteDebateManager) restoreDebate(prev *Debate) {
	if prev == nil {
		return
	}
	m.InMemoryDebateManager.mu.Lock()
	m.InMemoryDebateManager.debates[prev.ID] = prev
	m.InMemoryDebateManager.mu.Unlock()
}
