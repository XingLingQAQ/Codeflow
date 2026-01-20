// Package audit - Audit service tests
package audit

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestAuditService_Log(t *testing.T) {
	storage := NewMemoryStorage()
	svc := NewAuditService(storage)
	ctx := context.Background()

	entry := &AuditLogEntry{
		EventType: EventAccess,
		Severity:  SeverityInfo,
		Actor: AuditActor{
			ID:   "user1",
			Type: "user",
			Name: "Test User",
		},
		Resource: AuditResource{
			Type: "memory",
			ID:   "mem1",
		},
		Action:  "read",
		Outcome: OutcomeSuccess,
	}

	err := svc.Log(ctx, entry)
	assert.NoError(t, err)
	assert.NotEmpty(t, entry.ID)
	assert.NotZero(t, entry.Timestamp)
	assert.Equal(t, GenesisHash, entry.PreviousHash)
	assert.NotEmpty(t, entry.Hash)
}

func TestAuditService_LogChain(t *testing.T) {
	storage := NewMemoryStorage()
	svc := NewAuditService(storage)
	ctx := context.Background()

	// Log first entry
	entry1 := &AuditLogEntry{
		EventType: EventAccess,
		Severity:  SeverityInfo,
		Actor:     AuditActor{ID: "user1", Type: "user"},
		Resource:  AuditResource{Type: "memory", ID: "mem1"},
		Action:    "read",
		Outcome:   OutcomeSuccess,
	}
	err := svc.Log(ctx, entry1)
	assert.NoError(t, err)

	// Log second entry
	entry2 := &AuditLogEntry{
		EventType: EventModify,
		Severity:  SeverityInfo,
		Actor:     AuditActor{ID: "user1", Type: "user"},
		Resource:  AuditResource{Type: "memory", ID: "mem1"},
		Action:    "write",
		Outcome:   OutcomeSuccess,
	}
	err = svc.Log(ctx, entry2)
	assert.NoError(t, err)

	// Verify chain
	assert.Equal(t, entry1.Hash, entry2.PreviousHash)
}

func TestAuditService_Query(t *testing.T) {
	storage := NewMemoryStorage()
	svc := NewAuditService(storage)
	ctx := context.Background()

	// Log multiple entries
	for i := 0; i < 5; i++ {
		entry := &AuditLogEntry{
			EventType: EventAccess,
			Severity:  SeverityInfo,
			Actor:     AuditActor{ID: "user1", Type: "user"},
			Resource:  AuditResource{Type: "memory", ID: "mem1"},
			Action:    "read",
			Outcome:   OutcomeSuccess,
		}
		svc.Log(ctx, entry)
	}

	// Query all
	query := &AuditQuery{
		Limit: 10,
	}
	result, err := svc.Query(ctx, query)
	assert.NoError(t, err)
	assert.Equal(t, 5, len(result.Entries))
	assert.Equal(t, 5, result.Total)
	assert.False(t, result.HasMore)
}

func TestAuditService_QueryWithFilters(t *testing.T) {
	storage := NewMemoryStorage()
	svc := NewAuditService(storage)
	ctx := context.Background()

	// Log entries with different event types
	entry1 := &AuditLogEntry{
		EventType: EventAccess,
		Severity:  SeverityInfo,
		Actor:     AuditActor{ID: "user1", Type: "user"},
		Resource:  AuditResource{Type: "memory", ID: "mem1"},
		Action:    "read",
		Outcome:   OutcomeSuccess,
	}
	svc.Log(ctx, entry1)

	entry2 := &AuditLogEntry{
		EventType: EventModify,
		Severity:  SeverityWarning,
		Actor:     AuditActor{ID: "user2", Type: "user"},
		Resource:  AuditResource{Type: "config", ID: "cfg1"},
		Action:    "write",
		Outcome:   OutcomeFailure,
	}
	svc.Log(ctx, entry2)

	// Query by event type
	query := &AuditQuery{
		EventTypes: []AuditEventType{EventAccess},
		Limit:      10,
	}
	result, err := svc.Query(ctx, query)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(result.Entries))
	assert.Equal(t, EventAccess, result.Entries[0].EventType)

	// Query by actor
	query = &AuditQuery{
		ActorID: "user2",
		Limit:   10,
	}
	result, err = svc.Query(ctx, query)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(result.Entries))
	assert.Equal(t, "user2", result.Entries[0].Actor.ID)
}

func TestAuditService_QueryPagination(t *testing.T) {
	storage := NewMemoryStorage()
	svc := NewAuditService(storage)
	ctx := context.Background()

	// Log 10 entries
	for i := 0; i < 10; i++ {
		entry := &AuditLogEntry{
			EventType: EventAccess,
			Severity:  SeverityInfo,
			Actor:     AuditActor{ID: "user1", Type: "user"},
			Resource:  AuditResource{Type: "memory", ID: "mem1"},
			Action:    "read",
			Outcome:   OutcomeSuccess,
		}
		svc.Log(ctx, entry)
	}

	// Query first page
	query := &AuditQuery{
		Limit:  5,
		Offset: 0,
	}
	result, err := svc.Query(ctx, query)
	assert.NoError(t, err)
	assert.Equal(t, 5, len(result.Entries))
	assert.True(t, result.HasMore)

	// Query second page
	query.Offset = 5
	result, err = svc.Query(ctx, query)
	assert.NoError(t, err)
	assert.Equal(t, 5, len(result.Entries))
	assert.False(t, result.HasMore)
}

func TestAuditService_VerifyChain(t *testing.T) {
	storage := NewMemoryStorage()
	svc := NewAuditService(storage)
	ctx := context.Background()

	// Log entries
	for i := 0; i < 3; i++ {
		entry := &AuditLogEntry{
			EventType: EventAccess,
			Severity:  SeverityInfo,
			Actor:     AuditActor{ID: "user1", Type: "user"},
			Resource:  AuditResource{Type: "memory", ID: "mem1"},
			Action:    "read",
			Outcome:   OutcomeSuccess,
		}
		svc.Log(ctx, entry)
	}

	// Verify chain
	result, err := svc.VerifyChain(ctx)
	assert.NoError(t, err)
	assert.True(t, result.Valid)
	assert.Equal(t, 3, result.CheckedEntries)
	assert.Empty(t, result.InvalidEntries)
}

func TestAuditService_VerifyChainBroken(t *testing.T) {
	storage := NewMemoryStorage()
	svc := NewAuditService(storage)
	ctx := context.Background()

	// Log entries
	entry1 := &AuditLogEntry{
		EventType: EventAccess,
		Severity:  SeverityInfo,
		Actor:     AuditActor{ID: "user1", Type: "user"},
		Resource:  AuditResource{Type: "memory", ID: "mem1"},
		Action:    "read",
		Outcome:   OutcomeSuccess,
	}
	svc.Log(ctx, entry1)

	entry2 := &AuditLogEntry{
		EventType: EventModify,
		Severity:  SeverityInfo,
		Actor:     AuditActor{ID: "user1", Type: "user"},
		Resource:  AuditResource{Type: "memory", ID: "mem1"},
		Action:    "write",
		Outcome:   OutcomeSuccess,
	}
	svc.Log(ctx, entry2)

	// Manually break the chain
	storage.entries[1].PreviousHash = "invalid_hash"

	// Verify chain
	result, err := svc.VerifyChain(ctx)
	assert.NoError(t, err)
	assert.False(t, result.Valid)
	assert.Greater(t, len(result.InvalidEntries), 0)
}

func TestAuditService_GetByID(t *testing.T) {
	storage := NewMemoryStorage()
	svc := NewAuditService(storage)
	ctx := context.Background()

	// Log entry
	entry := &AuditLogEntry{
		EventType: EventAccess,
		Severity:  SeverityInfo,
		Actor:     AuditActor{ID: "user1", Type: "user"},
		Resource:  AuditResource{Type: "memory", ID: "mem1"},
		Action:    "read",
		Outcome:   OutcomeSuccess,
	}
	svc.Log(ctx, entry)

	// Get by ID
	retrieved, err := svc.GetByID(ctx, entry.ID)
	assert.NoError(t, err)
	assert.NotNil(t, retrieved)
	assert.Equal(t, entry.ID, retrieved.ID)
	assert.Equal(t, entry.Action, retrieved.Action)
}

func TestAuditService_GetStatistics(t *testing.T) {
	storage := NewMemoryStorage()
	svc := NewAuditService(storage)
	ctx := context.Background()

	// Log entries with different types and severities
	entries := []struct {
		eventType AuditEventType
		severity  AuditSeverity
		outcome   AuditOutcome
	}{
		{EventAccess, SeverityInfo, OutcomeSuccess},
		{EventAccess, SeverityInfo, OutcomeSuccess},
		{EventModify, SeverityWarning, OutcomeFailure},
		{EventDelete, SeverityError, OutcomeFailure},
	}

	for _, e := range entries {
		entry := &AuditLogEntry{
			EventType: e.eventType,
			Severity:  e.severity,
			Actor:     AuditActor{ID: "user1", Type: "user"},
			Resource:  AuditResource{Type: "memory", ID: "mem1"},
			Action:    "test",
			Outcome:   e.outcome,
		}
		svc.Log(ctx, entry)
	}

	// Get statistics
	stats, err := svc.GetStatistics(ctx)
	assert.NoError(t, err)
	assert.Equal(t, 4, stats.TotalEntries)
	assert.Equal(t, 2, stats.EntriesByType[EventAccess])
	assert.Equal(t, 1, stats.EntriesByType[EventModify])
	assert.Equal(t, 1, stats.EntriesByType[EventDelete])
	assert.Equal(t, 2, stats.SuccessCount)
	assert.Equal(t, 2, stats.FailureCount)
}

func TestAuditService_QueryTimeRange(t *testing.T) {
	storage := NewMemoryStorage()
	svc := NewAuditService(storage)
	ctx := context.Background()

	now := time.Now().UnixNano()

	// Log entries with different timestamps
	entry1 := &AuditLogEntry{
		Timestamp: now - 3600*1e9, // 1 hour ago
		EventType: EventAccess,
		Severity:  SeverityInfo,
		Actor:     AuditActor{ID: "user1", Type: "user"},
		Resource:  AuditResource{Type: "memory", ID: "mem1"},
		Action:    "read",
		Outcome:   OutcomeSuccess,
	}
	svc.Log(ctx, entry1)

	entry2 := &AuditLogEntry{
		Timestamp: now,
		EventType: EventModify,
		Severity:  SeverityInfo,
		Actor:     AuditActor{ID: "user1", Type: "user"},
		Resource:  AuditResource{Type: "memory", ID: "mem1"},
		Action:    "write",
		Outcome:   OutcomeSuccess,
	}
	svc.Log(ctx, entry2)

	// Query recent entries only
	query := &AuditQuery{
		StartTime: now - 1800*1e9, // Last 30 minutes
		Limit:     10,
	}
	result, err := svc.Query(ctx, query)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(result.Entries))
	assert.Equal(t, EventModify, result.Entries[0].EventType)
}

func TestMemoryStorage_Operations(t *testing.T) {
	storage := NewMemoryStorage()
	ctx := context.Background()

	// Test Append
	entry := &AuditLogEntry{
		ID:        "test1",
		Timestamp: time.Now().UnixNano(),
		EventType: EventAccess,
		Severity:  SeverityInfo,
		Actor:     AuditActor{ID: "user1", Type: "user"},
		Resource:  AuditResource{Type: "memory", ID: "mem1"},
		Action:    "read",
		Outcome:   OutcomeSuccess,
	}
	err := storage.Append(ctx, entry)
	assert.NoError(t, err)

	// Test Get
	retrieved, err := storage.Get(ctx, "test1")
	assert.NoError(t, err)
	assert.NotNil(t, retrieved)
	assert.Equal(t, "test1", retrieved.ID)

	// Test GetLastEntry
	lastEntry, err := storage.GetLastEntry(ctx)
	assert.NoError(t, err)
	assert.Equal(t, "test1", lastEntry.ID)

	// Test Count
	count, err := storage.Count(ctx, nil)
	assert.NoError(t, err)
	assert.Equal(t, 1, count)

	// Test Clear
	err = storage.Clear(ctx)
	assert.NoError(t, err)

	count, err = storage.Count(ctx, nil)
	assert.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestGetSetAuditService(t *testing.T) {
	storage := NewMemoryStorage()
	svc := NewAuditService(storage)

	SetAuditService(svc)
	retrieved := GetAuditService()

	assert.Equal(t, svc, retrieved)
}
