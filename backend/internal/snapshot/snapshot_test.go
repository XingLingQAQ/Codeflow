// Package snapshot - Snapshot service tests
package snapshot

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	backendgit "github.com/codeflow/backend/internal/git"
	backendhooks "github.com/codeflow/backend/internal/hooks"
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

func TestSnapshotRestoreTriggersRestoreStateHook(t *testing.T) {
	mgr := backendhooks.NewHookManager()
	previous := backendhooks.GetHookManager()
	backendhooks.SetHookManager(mgr)
	t.Cleanup(func() {
		backendhooks.SetHookManager(previous)
	})

	var snapshotID string
	err := mgr.Register(backendhooks.HookConfig{Name: "restore-state", Type: backendhooks.HookRestoreState, Enabled: true}, func(ctx context.Context, value interface{}) (interface{}, error) {
		id, ok := value.(string)
		if !ok {
			t.Fatalf("expected snapshot id payload, got %#v", value)
		}
		snapshotID = id
		return value, nil
	})
	if err != nil {
		t.Fatalf("register hook: %v", err)
	}

	svc := NewInMemorySnapshotService()
	ctx := context.Background()
	snapshot, err := svc.Create(ctx, &SnapshotCreateRequest{Description: "restore hook"})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	_, err = svc.Restore(ctx, snapshot.ID)
	if err != nil {
		t.Fatalf("Restore failed: %v", err)
	}
	if snapshotID != snapshot.ID {
		t.Fatalf("expected restore hook for %s, got %s", snapshot.ID, snapshotID)
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

func TestSnapshotCreateUsesRealGitHash(t *testing.T) {
	svc := NewInMemorySnapshotService()
	ctx := context.Background()

	req := &SnapshotCreateRequest{
		Description: "Real git hash snapshot",
		SessionID:   "session-123",
	}

	snapshot, err := svc.Create(ctx, req)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	gitManager := backendgit.NewGitManager(".")
	head, err := gitManager.GetCurrentHash(ctx)
	if err != nil {
		t.Fatalf("GetCurrentHash failed: %v", err)
	}
	if snapshot.GitHash != head {
		t.Fatalf("expected git hash %s, got %s", head, snapshot.GitHash)
	}
	if !strings.HasPrefix(snapshot.ConversationState, "conversation:") {
		t.Fatalf("expected conversation digest prefix, got %s", snapshot.ConversationState)
	}
	if !strings.HasPrefix(snapshot.VectorPointer, "vector:") {
		t.Fatalf("expected vector digest prefix, got %s", snapshot.VectorPointer)
	}
	if !strings.HasPrefix(snapshot.MemoryGraphVersion, "graph:") {
		t.Fatalf("expected graph digest prefix, got %s", snapshot.MemoryGraphVersion)
	}
}

type fakeSnapshotStateProvider struct {
	gitHash       string
	conversation  string
	vector        string
	graph         string
	restoreGit    string
	restoreConv   string
	restoreVector string
	restoreGraph  string
	restoreErrs   map[string]error
}

func (f *fakeSnapshotStateProvider) CaptureGitState(ctx context.Context) (string, error) {
	return f.gitHash, nil
}

func (f *fakeSnapshotStateProvider) CaptureConversationState(ctx context.Context, sessionID string) (string, error) {
	return f.conversation, nil
}

func (f *fakeSnapshotStateProvider) CaptureVectorState(ctx context.Context, sessionID string) (string, error) {
	return f.vector, nil
}

func (f *fakeSnapshotStateProvider) CaptureMemoryGraphState(ctx context.Context) (string, error) {
	return f.graph, nil
}

func (f *fakeSnapshotStateProvider) RestoreGitState(ctx context.Context, gitHash string) error {
	f.restoreGit = gitHash
	if f.restoreErrs != nil {
		return f.restoreErrs["git"]
	}
	return nil
}

func (f *fakeSnapshotStateProvider) RestoreConversationState(ctx context.Context, state string) error {
	f.restoreConv = state
	if f.restoreErrs != nil {
		return f.restoreErrs["conversation"]
	}
	return nil
}

func (f *fakeSnapshotStateProvider) RestoreVectorState(ctx context.Context, pointer string) error {
	f.restoreVector = pointer
	if f.restoreErrs != nil {
		return f.restoreErrs["vector"]
	}
	return nil
}

func (f *fakeSnapshotStateProvider) RestoreMemoryGraphState(ctx context.Context, version string) error {
	f.restoreGraph = version
	if f.restoreErrs != nil {
		return f.restoreErrs["graph"]
	}
	return nil
}

func TestSnapshotRestoreDelegatesToProvider(t *testing.T) {
	provider := &fakeSnapshotStateProvider{
		gitHash:      "git:abc123",
		conversation: "conversation:def456",
		vector:       "vector:ghi789",
		graph:        "graph:jkl012",
	}
	svc := NewInMemorySnapshotServiceWithProvider(provider)
	ctx := context.Background()

	snap, err := svc.Create(ctx, &SnapshotCreateRequest{Description: "provider snapshot", SessionID: "session-x"})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	result, err := svc.Restore(ctx, snap.ID)
	if err != nil {
		t.Fatalf("Restore failed: %v", err)
	}
	if !result.GitRestored || !result.ConversationRestored || !result.VectorRestored || !result.MemoryGraphRestored {
		t.Fatalf("expected all restore flags true, got %+v", result)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("expected no restore errors, got %+v", result.Errors)
	}
	if provider.restoreGit != provider.gitHash {
		t.Fatalf("expected git restore hash %s, got %s", provider.gitHash, provider.restoreGit)
	}
	if provider.restoreConv != provider.conversation {
		t.Fatalf("expected conversation restore state %s, got %s", provider.conversation, provider.restoreConv)
	}
	if provider.restoreVector != provider.vector {
		t.Fatalf("expected vector restore state %s, got %s", provider.vector, provider.restoreVector)
	}
	if provider.restoreGraph != provider.graph {
		t.Fatalf("expected graph restore state %s, got %s", provider.graph, provider.restoreGraph)
	}
}

func TestSnapshotRestoreCollectsProviderErrors(t *testing.T) {
	provider := &fakeSnapshotStateProvider{
		gitHash:      "git:abc123",
		conversation: "conversation:def456",
		vector:       "vector:ghi789",
		graph:        "graph:jkl012",
		restoreErrs: map[string]error{
			"git":    errors.New("reset denied"),
			"vector": errors.New("vector pointer stale"),
		},
	}
	svc := NewInMemorySnapshotServiceWithProvider(provider)
	ctx := context.Background()

	snap, err := svc.Create(ctx, &SnapshotCreateRequest{Description: "partial restore"})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	result, err := svc.Restore(ctx, snap.ID)
	if err != nil {
		t.Fatalf("Restore failed: %v", err)
	}

	if result.GitRestored || result.VectorRestored {
		t.Fatalf("expected failed restore flags false, got %+v", result)
	}
	if !result.ConversationRestored || !result.MemoryGraphRestored {
		t.Fatalf("expected successful restore flags true, got %+v", result)
	}
	if len(result.Errors) != 2 {
		t.Fatalf("expected two restore errors, got %+v", result.Errors)
	}
	joined := strings.Join(result.Errors, ";")
	if !strings.Contains(joined, "git restore failed") || !strings.Contains(joined, "vector restore failed") {
		t.Fatalf("expected git and vector errors, got %+v", result.Errors)
	}
}

func TestSnapshotGlobalServiceCompatibility(t *testing.T) {
	previous := defaultSnapshotService
	t.Cleanup(func() {
		SetSnapshotService(previous)
	})

	SetSnapshotService(nil)
	created := GetSnapshotService()
	if created == nil {
		t.Fatal("expected default snapshot service")
	}
	if GetSnapshotService() != created {
		t.Fatal("expected GetSnapshotService to reuse default service")
	}

	custom := NewInMemorySnapshotServiceWithProvider(&fakeSnapshotStateProvider{
		gitHash:      "git:custom",
		conversation: "conversation:custom",
		vector:       "vector:custom",
		graph:        "graph:custom",
	})
	SetSnapshotService(custom)
	if GetSnapshotService() != custom {
		t.Fatal("expected injected snapshot service")
	}
}
