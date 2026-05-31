// Package snapshot - Snapshot service implementation
package snapshot

import (
	"context"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	backendhooks "github.com/codeflow/backend/internal/hooks"
	"github.com/google/uuid"
)

// InMemorySnapshotService is an in-memory implementation of ISnapshotService.
type InMemorySnapshotService struct {
	mu        sync.RWMutex
	snapshots map[string]*Snapshot
	provider  StateProvider
}

// NewInMemorySnapshotService creates a new in-memory snapshot service.
func NewInMemorySnapshotService() *InMemorySnapshotService {
	return NewInMemorySnapshotServiceWithProvider(nil)
}

// NewInMemorySnapshotServiceWithProvider creates a new in-memory snapshot service with a custom state provider.
func NewInMemorySnapshotServiceWithProvider(provider StateProvider) *InMemorySnapshotService {
	if provider == nil {
		provider = NewDefaultStateProvider()
	}
	return &InMemorySnapshotService{
		snapshots: make(map[string]*Snapshot),
		provider:  provider,
	}
}

// Create creates a new atomic snapshot.
func (s *InMemorySnapshotService) Create(ctx context.Context, req *SnapshotCreateRequest) (*Snapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	startTime := time.Now()

	// Capture current state
	gitHash, err := s.captureGitState(ctx)
	if err != nil {
		return nil, err
	}
	conversationState, err := s.captureConversationState(ctx, req.SessionID)
	if err != nil {
		return nil, err
	}
	vectorPointer, err := s.captureVectorState(ctx, req.SessionID)
	if err != nil {
		return nil, err
	}
	memoryGraphVersion, err := s.captureMemoryGraphState(ctx)
	if err != nil {
		return nil, err
	}

	snapshot := &Snapshot{
		ID:                 uuid.New().String(),
		GitHash:            gitHash,
		ConversationState:  conversationState,
		VectorPointer:      vectorPointer,
		MemoryGraphVersion: memoryGraphVersion,
		Description:        req.Description,
		CreatedAt:          time.Now(),
		SessionID:          req.SessionID,
		Tags:               req.Tags,
	}

	s.snapshots[snapshot.ID] = snapshot

	// Ensure creation time < 500ms
	elapsed := time.Since(startTime)
	if elapsed > 500*time.Millisecond {
		return snapshot, fmt.Errorf("snapshot creation took %v, exceeding 500ms threshold", elapsed)
	}

	return snapshot, nil
}

// List returns a paginated list of snapshots.
func (s *InMemorySnapshotService) List(ctx context.Context, opts *SnapshotListOptions) (*SnapshotListResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Filter snapshots
	var filtered []*Snapshot
	for _, snap := range s.snapshots {
		if opts.SessionID != "" && snap.SessionID != opts.SessionID {
			continue
		}
		if opts.StartTime != nil && snap.CreatedAt.Before(*opts.StartTime) {
			continue
		}
		if opts.EndTime != nil && snap.CreatedAt.After(*opts.EndTime) {
			continue
		}
		if len(opts.Tags) > 0 && !s.hasAnyTag(snap.Tags, opts.Tags) {
			continue
		}
		filtered = append(filtered, snap)
	}

	// Sort by creation time (newest first)
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
	})

	// Pagination
	total := len(filtered)
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := opts.Offset
	if offset < 0 {
		offset = 0
	}

	start := offset
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}

	// Convert to value slice
	items := make([]Snapshot, 0, end-start)
	for _, snap := range filtered[start:end] {
		items = append(items, *snap)
	}

	hasMore := end < total
	var nextOffset int
	if hasMore {
		nextOffset = end
	}

	return &SnapshotListResponse{
		Items:      items,
		Total:      total,
		HasMore:    hasMore,
		NextOffset: nextOffset,
	}, nil
}

// Get retrieves a snapshot by ID.
func (s *InMemorySnapshotService) Get(ctx context.Context, id string) (*Snapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	snap, ok := s.snapshots[id]
	if !ok {
		return nil, fmt.Errorf("snapshot not found: %s", id)
	}
	return snap, nil
}

// Restore restores the system state from a snapshot.
func (s *InMemorySnapshotService) Restore(ctx context.Context, id string) (*RestoreResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.RLock()
	snap, ok := s.snapshots[id]
	s.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("snapshot not found: %s", id)
	}

	result := &RestoreResult{
		SnapshotID: id,
		RestoredAt: time.Now(),
	}

	// Restore Git state
	if err := s.restoreGitState(ctx, snap.GitHash); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("git restore failed: %v", err))
	} else {
		result.GitRestored = true
	}

	// Restore conversation state
	if err := s.restoreConversationState(ctx, snap.ConversationState); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("conversation restore failed: %v", err))
	} else {
		result.ConversationRestored = true
	}

	// Restore vector state
	if err := s.restoreVectorState(ctx, snap.VectorPointer); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("vector restore failed: %v", err))
	} else {
		result.VectorRestored = true
	}

	// Restore memory graph state
	if err := s.restoreMemoryGraphState(ctx, snap.MemoryGraphVersion); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("memory graph restore failed: %v", err))
	} else {
		result.MemoryGraphRestored = true
	}

	if backendhooks.HasHookManager() {
		if _, err := backendhooks.GetHookManager().HookRestoreState(ctx, id); err != nil {
			log.Printf("[WARN] snapshot restore-state hook failed: snapshot=%s err=%v", id, err)
		}
	}

	return result, nil
}

// Delete deletes a snapshot by ID.
func (s *InMemorySnapshotService) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.snapshots[id]; !ok {
		return fmt.Errorf("snapshot not found: %s", id)
	}

	delete(s.snapshots, id)
	return nil
}

// Helper methods for state capture and restore

func (s *InMemorySnapshotService) captureGitState(ctx context.Context) (string, error) {
	return s.provider.CaptureGitState(ctx)
}

func (s *InMemorySnapshotService) captureConversationState(ctx context.Context, sessionID string) (string, error) {
	return s.provider.CaptureConversationState(ctx, sessionID)
}

func (s *InMemorySnapshotService) captureVectorState(ctx context.Context, sessionID string) (string, error) {
	return s.provider.CaptureVectorState(ctx, sessionID)
}

func (s *InMemorySnapshotService) captureMemoryGraphState(ctx context.Context) (string, error) {
	return s.provider.CaptureMemoryGraphState(ctx)
}

func (s *InMemorySnapshotService) restoreGitState(ctx context.Context, gitHash string) error {
	return s.provider.RestoreGitState(ctx, gitHash)
}

func (s *InMemorySnapshotService) restoreConversationState(ctx context.Context, state string) error {
	return s.provider.RestoreConversationState(ctx, state)
}

func (s *InMemorySnapshotService) restoreVectorState(ctx context.Context, pointer string) error {
	return s.provider.RestoreVectorState(ctx, pointer)
}

func (s *InMemorySnapshotService) restoreMemoryGraphState(ctx context.Context, version string) error {
	return s.provider.RestoreMemoryGraphState(ctx, version)
}

func (s *InMemorySnapshotService) hasAnyTag(snapshotTags, filterTags []string) bool {
	tagSet := make(map[string]bool)
	for _, tag := range snapshotTags {
		tagSet[tag] = true
	}
	for _, tag := range filterTags {
		if tagSet[tag] {
			return true
		}
	}
	return false
}

// Global service instance
var defaultSnapshotService ISnapshotService

// GetSnapshotService returns the global snapshot service instance.
func GetSnapshotService() ISnapshotService {
	if defaultSnapshotService == nil {
		defaultSnapshotService = NewInMemorySnapshotService()
	}
	return defaultSnapshotService
}

// SetSnapshotService sets the global snapshot service instance (for testing).
func SetSnapshotService(svc ISnapshotService) {
	defaultSnapshotService = svc
}
