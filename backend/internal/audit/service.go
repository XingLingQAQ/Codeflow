// Package audit - Audit service implementation
package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// AuditService implements audit logging with hash chain.
type AuditService struct {
	storage IAuditStorage
	mu      sync.RWMutex
}

// NewAuditService creates a new audit service.
func NewAuditService(storage IAuditStorage) *AuditService {
	return &AuditService{
		storage: storage,
	}
}

// Log creates a new audit log entry with hash chain.
func (s *AuditService) Log(ctx context.Context, entry *AuditLogEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Generate ID if not provided
	if entry.ID == "" {
		entry.ID = uuid.New().String()
	}

	// Set timestamp if not provided
	if entry.Timestamp == 0 {
		entry.Timestamp = time.Now().UnixNano()
	}

	// Get previous entry to build hash chain
	lastEntry, err := s.storage.GetLastEntry(ctx)
	if err != nil {
		// If no previous entry, this is the first entry
		entry.PreviousHash = GenesisHash
	} else {
		entry.PreviousHash = lastEntry.Hash
	}

	// Calculate hash for this entry
	entry.Hash = s.calculateHash(entry)

	// Append to storage
	return s.storage.Append(ctx, entry)
}

// Query retrieves audit logs based on query parameters.
func (s *AuditService) Query(ctx context.Context, query *AuditQuery) (*AuditQueryResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Set defaults
	if query.Limit == 0 {
		query.Limit = 100
	}
	if query.Limit > 1000 {
		query.Limit = 1000
	}

	// Get entries
	entries, err := s.storage.Query(ctx, query)
	if err != nil {
		return nil, err
	}

	// Get total count
	total, err := s.storage.Count(ctx, query)
	if err != nil {
		return nil, err
	}

	return &AuditQueryResult{
		Entries: entries,
		Total:   total,
		HasMore: query.Offset+len(entries) < total,
	}, nil
}

// VerifyChain verifies the integrity of the audit log chain.
func (s *AuditService) VerifyChain(ctx context.Context) (*IntegrityVerificationResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.storage.VerifyHashChain(ctx)
}

// GetByID retrieves a specific audit log by ID.
func (s *AuditService) GetByID(ctx context.Context, id string) (*AuditLogEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.storage.Get(ctx, id)
}

// GetStatistics returns audit statistics.
func (s *AuditService) GetStatistics(ctx context.Context) (*AuditStatistics, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Get all entries for statistics
	query := &AuditQuery{
		Limit: 10000, // Large limit for statistics
	}
	entries, err := s.storage.Query(ctx, query)
	if err != nil {
		return nil, err
	}

	stats := &AuditStatistics{
		TotalEntries:      len(entries),
		EntriesByType:     make(map[AuditEventType]int),
		EntriesBySeverity: make(map[AuditSeverity]int),
	}

	for _, entry := range entries {
		// Count by type
		stats.EntriesByType[entry.EventType]++

		// Count by severity
		stats.EntriesBySeverity[entry.Severity]++

		// Count by outcome
		if entry.Outcome == OutcomeSuccess {
			stats.SuccessCount++
		} else {
			stats.FailureCount++
		}

		// Track oldest and newest
		if stats.OldestEntry == 0 || entry.Timestamp < stats.OldestEntry {
			stats.OldestEntry = entry.Timestamp
		}
		if entry.Timestamp > stats.NewestEntry {
			stats.NewestEntry = entry.Timestamp
		}
	}

	return stats, nil
}

// calculateHash calculates SHA-256 hash of an audit log entry.
func (s *AuditService) calculateHash(entry *AuditLogEntry) string {
	// Create a copy without the hash field
	data := map[string]interface{}{
		"id":            entry.ID,
		"timestamp":     entry.Timestamp,
		"event_type":    entry.EventType,
		"severity":      entry.Severity,
		"actor":         entry.Actor,
		"resource":      entry.Resource,
		"action":        entry.Action,
		"outcome":       entry.Outcome,
		"details":       entry.Details,
		"previous_hash": entry.PreviousHash,
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(data)
	if err != nil {
		// Fallback to simple hash
		return fmt.Sprintf("%x", sha256.Sum256([]byte(entry.ID)))
	}

	// Calculate SHA-256
	hash := sha256.Sum256(jsonData)
	return hex.EncodeToString(hash[:])
}

// Close closes the audit service.
func (s *AuditService) Close() error {
	return s.storage.Close()
}

// Global service instance
var defaultAuditService *AuditService
var auditMu sync.RWMutex

// GetAuditService returns the global audit service instance.
func GetAuditService() *AuditService {
	auditMu.RLock()
	defer auditMu.RUnlock()
	return defaultAuditService
}

// SetAuditService sets the global audit service instance.
func SetAuditService(svc *AuditService) {
	auditMu.Lock()
	defer auditMu.Unlock()
	defaultAuditService = svc
}
