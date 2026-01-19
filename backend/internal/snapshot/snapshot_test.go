// Package snapshot - Snapshot service tests
package snapshot

import (
	"context"
	"testing"
	"time"
)

func TestSnapshotCreate(t *testing.T) {
	svc := NewInMemorySnapshotService()
	ctx := context.Background()

	req := &SnapshotCreateRequest{
		Description: "Test snapshot",
		SessionID:   "session-123",
		Tags:        []string{"test", "dev"},
	}

	startTime := time.Now()
	snapshot, err := svc.Create(ctx, req)
	elapsed := time.Since(startTime)

	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if snapshot.ID == "" {
		t.Error("Snapshot ID should not be empty")
	}

	if snapshot.GitHash == "" {
		t.Error("GitHash should not be empty")
	}

	if snapshot.ConversationState == "" {
		t.Error("ConversationState should not be empty")
	}

	if snapshot.VectorPointer == "" {
		t.Error("VectorPointer should not be empty")
	}

	if snapshot.MemoryGraphVersion == "" {
		t.Error("MemoryGraphVersion should not be empty")
	}

	if snapshot.Description != req.Description {
		t.Errorf("Description mismatch: got %s, want %s", snapshot.Description, req.Description)
	}

	if snapshot.SessionID != req.SessionID {
		t.Errorf("SessionID mismatch: got %s, want %s", snapshot.SessionID, req.SessionID)
	}

	// Verify creation time < 500ms
	if elapsed > 500*time.Millisecond {
		t.Errorf("Snapshot creation took %v, exceeding 500ms threshold", elapsed)
	}
}

func TestSnapshotList(t *testing.T) {
	svc := NewInMemorySnapshotService()
	ctx := context.Background()

	// Create multiple snapshots
	for i := 0; i < 5; i++ {
		req := &SnapshotCreateRequest{
			Description: "Test snapshot",
			SessionID:   "session-123",
		}
		_, err := svc.Create(ctx, req)
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	// List all snapshots
	opts := &SnapshotListOptions{
		Limit: 10,
	}
	resp, err := svc.List(ctx, opts)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if resp.Total != 5 {
		t.Errorf("Total mismatch: got %d, want 5", resp.Total)
	}

	if len(resp.Items) != 5 {
		t.Errorf("Items count mismatch: got %d, want 5", len(resp.Items))
	}

	// Test pagination
	opts = &SnapshotListOptions{
		Limit:  2,
		Offset: 0,
	}
	resp, err = svc.List(ctx, opts)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(resp.Items) != 2 {
		t.Errorf("Items count mismatch: got %d, want 2", len(resp.Items))
	}

	if !resp.HasMore {
		t.Error("HasMore should be true")
	}

	if resp.NextOffset != 2 {
		t.Errorf("NextOffset mismatch: got %d, want 2", resp.NextOffset)
	}
}

func TestSnapshotGet(t *testing.T) {
	svc := NewInMemorySnapshotService()
	ctx := context.Background()

	req := &SnapshotCreateRequest{
		Description: "Test snapshot",
	}
	created, err := svc.Create(ctx, req)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Get existing snapshot
	snapshot, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if snapshot.ID != created.ID {
		t.Errorf("ID mismatch: got %s, want %s", snapshot.ID, created.ID)
	}

	// Get non-existing snapshot
	_, err = svc.Get(ctx, "non-existing-id")
	if err == nil {
		t.Error("Get should fail for non-existing snapshot")
	}
}

func TestSnapshotRestore(t *testing.T) {
	svc := NewInMemorySnapshotService()
	ctx := context.Background()

	req := &SnapshotCreateRequest{
		Description: "Test snapshot",
	}
	snapshot, err := svc.Create(ctx, req)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Restore snapshot
	result, err := svc.Restore(ctx, snapshot.ID)
	if err != nil {
		t.Fatalf("Restore failed: %v", err)
	}

	if result.SnapshotID != snapshot.ID {
		t.Errorf("SnapshotID mismatch: got %s, want %s", result.SnapshotID, snapshot.ID)
	}

	if !result.GitRestored {
		t.Error("GitRestored should be true")
	}

	if !result.ConversationRestored {
		t.Error("ConversationRestored should be true")
	}

	if !result.VectorRestored {
		t.Error("VectorRestored should be true")
	}

	if !result.MemoryGraphRestored {
		t.Error("MemoryGraphRestored should be true")
	}

	if len(result.Errors) > 0 {
		t.Errorf("Restore should not have errors: %v", result.Errors)
	}
}

func TestSnapshotDelete(t *testing.T) {
	svc := NewInMemorySnapshotService()
	ctx := context.Background()

	req := &SnapshotCreateRequest{
		Description: "Test snapshot",
	}
	snapshot, err := svc.Create(ctx, req)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Delete snapshot
	err = svc.Delete(ctx, snapshot.ID)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify deletion
	_, err = svc.Get(ctx, snapshot.ID)
	if err == nil {
		t.Error("Get should fail after deletion")
	}

	// Delete non-existing snapshot
	err = svc.Delete(ctx, "non-existing-id")
	if err == nil {
		t.Error("Delete should fail for non-existing snapshot")
	}
}

func TestSnapshotFilterBySessionID(t *testing.T) {
	svc := NewInMemorySnapshotService()
	ctx := context.Background()

	// Create snapshots with different session IDs
	for i := 0; i < 3; i++ {
		req := &SnapshotCreateRequest{
			Description: "Session 1 snapshot",
			SessionID:   "session-1",
		}
		_, err := svc.Create(ctx, req)
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	for i := 0; i < 2; i++ {
		req := &SnapshotCreateRequest{
			Description: "Session 2 snapshot",
			SessionID:   "session-2",
		}
		_, err := svc.Create(ctx, req)
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	// Filter by session ID
	opts := &SnapshotListOptions{
		SessionID: "session-1",
		Limit:     10,
	}
	resp, err := svc.List(ctx, opts)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if resp.Total != 3 {
		t.Errorf("Total mismatch: got %d, want 3", resp.Total)
	}

	for _, snap := range resp.Items {
		if snap.SessionID != "session-1" {
			t.Errorf("SessionID mismatch: got %s, want session-1", snap.SessionID)
		}
	}
}

func TestSnapshotFilterByTags(t *testing.T) {
	svc := NewInMemorySnapshotService()
	ctx := context.Background()

	// Create snapshots with different tags
	req1 := &SnapshotCreateRequest{
		Description: "Snapshot 1",
		Tags:        []string{"prod", "release"},
	}
	_, err := svc.Create(ctx, req1)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	req2 := &SnapshotCreateRequest{
		Description: "Snapshot 2",
		Tags:        []string{"dev", "test"},
	}
	_, err = svc.Create(ctx, req2)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Filter by tags
	opts := &SnapshotListOptions{
		Tags:  []string{"prod"},
		Limit: 10,
	}
	resp, err := svc.List(ctx, opts)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if resp.Total != 1 {
		t.Errorf("Total mismatch: got %d, want 1", resp.Total)
	}

	if len(resp.Items[0].Tags) == 0 || resp.Items[0].Tags[0] != "prod" {
		t.Error("Filtered snapshot should have 'prod' tag")
	}
}
