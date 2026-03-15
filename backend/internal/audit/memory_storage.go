// Package audit - In-memory storage implementation for testing
package audit

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// MemoryStorage implements IAuditStorage using in-memory storage.
type MemoryStorage struct {
	entries []AuditLogEntry
	mu      sync.RWMutex
}

// NewMemoryStorage creates a new memory storage.
func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{
		entries: make([]AuditLogEntry, 0),
	}
}

// Append adds a new audit log entry.
func (m *MemoryStorage) Append(ctx context.Context, entry *AuditLogEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.entries = append(m.entries, *entry)
	return nil
}

// Get retrieves a specific audit log by ID.
func (m *MemoryStorage) Get(ctx context.Context, id string) (*AuditLogEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, entry := range m.entries {
		if entry.ID == id {
			return &entry, nil
		}
	}

	return nil, fmt.Errorf("audit log not found: %s", id)
}

// Query retrieves audit logs based on query parameters.
func (m *MemoryStorage) Query(ctx context.Context, query *AuditQuery) ([]AuditLogEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var filtered []AuditLogEntry

	for _, entry := range m.entries {
		if m.matchesQuery(&entry, query) {
			filtered = append(filtered, entry)
		}
	}

	// Sort by timestamp descending
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Timestamp > filtered[j].Timestamp
	})

	// Apply pagination
	start := query.Offset
	if start > len(filtered) {
		return []AuditLogEntry{}, nil
	}

	end := start + query.Limit
	if end > len(filtered) {
		end = len(filtered)
	}

	return filtered[start:end], nil
}

// Count returns the number of entries matching the query.
func (m *MemoryStorage) Count(ctx context.Context, query *AuditQuery) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if query == nil {
		return len(m.entries), nil
	}

	count := 0
	for _, entry := range m.entries {
		if m.matchesQuery(&entry, query) {
			count++
		}
	}

	return count, nil
}

// GetLastEntry retrieves the most recent audit log entry.
func (m *MemoryStorage) GetLastEntry(ctx context.Context) (*AuditLogEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.entries) == 0 {
		return nil, fmt.Errorf("no entries found")
	}

	return &m.entries[len(m.entries)-1], nil
}

// Delete removes audit logs by IDs.
func (m *MemoryStorage) Delete(ctx context.Context, ids []string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	idSet := make(map[string]bool)
	for _, id := range ids {
		idSet[id] = true
	}

	var remaining []AuditLogEntry
	deleted := 0

	for _, entry := range m.entries {
		if idSet[entry.ID] {
			deleted++
		} else {
			remaining = append(remaining, entry)
		}
	}

	m.entries = remaining
	return deleted, nil
}

// Clear removes all audit logs.
func (m *MemoryStorage) Clear(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.entries = make([]AuditLogEntry, 0)
	return nil
}

// VerifyHashChain verifies the integrity of the hash chain.
func (m *MemoryStorage) VerifyHashChain(ctx context.Context) (*IntegrityVerificationResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := &IntegrityVerificationResult{
		Valid:          true,
		CheckedEntries: len(m.entries),
		InvalidEntries: make([]string, 0),
		VerifiedAt:     getCurrentTimestamp(),
	}

	if len(m.entries) == 0 {
		return result, nil
	}

	// Verify first entry
	if m.entries[0].PreviousHash != GenesisHash {
		result.Valid = false
		result.InvalidEntries = append(result.InvalidEntries, m.entries[0].ID)
		result.BrokenChainAt = m.entries[0].ID
	}

	firstHash := CalculateEntryHash(&m.entries[0])
	if m.entries[0].Hash != firstHash {
		result.Valid = false
		result.InvalidEntries = append(result.InvalidEntries, m.entries[0].ID)
		if result.BrokenChainAt == "" {
			result.BrokenChainAt = m.entries[0].ID
		}
	}

	// Verify chain continuity
	for i := 1; i < len(m.entries); i++ {
		if m.entries[i].PreviousHash != m.entries[i-1].Hash {
			result.Valid = false
			result.InvalidEntries = append(result.InvalidEntries, m.entries[i].ID)
			if result.BrokenChainAt == "" {
				result.BrokenChainAt = m.entries[i].ID
			}
		}
		if m.entries[i].Hash != CalculateEntryHash(&m.entries[i]) {
			result.Valid = false
			result.InvalidEntries = append(result.InvalidEntries, m.entries[i].ID)
			if result.BrokenChainAt == "" {
				result.BrokenChainAt = m.entries[i].ID
			}
		}
	}

	return result, nil
}

// Close closes the storage.
func (m *MemoryStorage) Close() error {
	return nil
}

// Helper function to check if entry matches query.
func (m *MemoryStorage) matchesQuery(entry *AuditLogEntry, query *AuditQuery) bool {
	if query == nil {
		return true
	}

	// Time range filter
	if query.StartTime > 0 && entry.Timestamp < query.StartTime {
		return false
	}
	if query.EndTime > 0 && entry.Timestamp > query.EndTime {
		return false
	}

	// Event types filter
	if len(query.EventTypes) > 0 {
		matched := false
		for _, et := range query.EventTypes {
			if entry.EventType == et {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Severities filter
	if len(query.Severities) > 0 {
		matched := false
		for _, sev := range query.Severities {
			if entry.Severity == sev {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Actor ID filter
	if query.ActorID != "" && entry.Actor.ID != query.ActorID {
		return false
	}

	// Resource ID filter
	if query.ResourceID != "" && entry.Resource.ID != query.ResourceID {
		return false
	}

	// Resource type filter
	if query.ResourceType != "" && entry.Resource.Type != query.ResourceType {
		return false
	}

	// Outcome filter
	if query.Outcome != "" && entry.Outcome != query.Outcome {
		return false
	}

	return true
}

// Helper function to get current timestamp.
func getCurrentTimestamp() int64 {
	return time.Now().UnixNano()
}
