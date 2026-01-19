// Package snapshot provides atomic snapshot functionality for Git-Conversation-Vector state.
package snapshot

import (
	"context"
	"time"
)

// Snapshot represents an atomic snapshot of the entire system state.
type Snapshot struct {
	ID                  string    `json:"id"`
	GitHash             string    `json:"git_hash"`
	ConversationState   string    `json:"conversation_state"`
	VectorPointer       string    `json:"vector_pointer"`
	MemoryGraphVersion  string    `json:"memory_graph_version"`
	Description         string    `json:"description"`
	CreatedAt           time.Time `json:"created_at"`
	SessionID           string    `json:"session_id,omitempty"`
	Tags                []string  `json:"tags,omitempty"`
}

// SnapshotCreateRequest represents a request to create a snapshot.
type SnapshotCreateRequest struct {
	Description string   `json:"description"`
	SessionID   string   `json:"session_id,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

// SnapshotListOptions represents options for listing snapshots.
type SnapshotListOptions struct {
	SessionID string
	StartTime *time.Time
	EndTime   *time.Time
	Tags      []string
	Limit     int
	Offset    int
}

// SnapshotListResponse represents a paginated list of snapshots.
type SnapshotListResponse struct {
	Items      []Snapshot `json:"items"`
	Total      int        `json:"total"`
	HasMore    bool       `json:"has_more"`
	NextOffset int        `json:"next_offset,omitempty"`
}

// RestoreResult represents the result of a snapshot restore operation.
type RestoreResult struct {
	SnapshotID         string    `json:"snapshot_id"`
	GitRestored        bool      `json:"git_restored"`
	ConversationRestored bool    `json:"conversation_restored"`
	VectorRestored     bool      `json:"vector_restored"`
	MemoryGraphRestored bool     `json:"memory_graph_restored"`
	RestoredAt         time.Time `json:"restored_at"`
	Errors             []string  `json:"errors,omitempty"`
}

// ISnapshotService defines the interface for snapshot operations.
type ISnapshotService interface {
	// Create creates a new atomic snapshot of the current system state.
	Create(ctx context.Context, req *SnapshotCreateRequest) (*Snapshot, error)

	// List returns a paginated list of snapshots.
	List(ctx context.Context, opts *SnapshotListOptions) (*SnapshotListResponse, error)

	// Get retrieves a snapshot by ID.
	Get(ctx context.Context, id string) (*Snapshot, error)

	// Restore restores the system state from a snapshot.
	Restore(ctx context.Context, id string) (*RestoreResult, error)

	// Delete deletes a snapshot by ID.
	Delete(ctx context.Context, id string) error
}
