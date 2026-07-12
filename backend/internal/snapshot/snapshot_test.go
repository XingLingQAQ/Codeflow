// Package snapshot - Snapshot service tests
package snapshot

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/agent"
	backendgit "github.com/codeflow/backend/internal/git"
	backendhooks "github.com/codeflow/backend/internal/hooks"
	"github.com/codeflow/backend/internal/memory"
	"github.com/codeflow/backend/internal/samg"
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
	assertRecoverableToken(t, "conversation", snapshot.ConversationState)
	assertRecoverableToken(t, "vector", snapshot.VectorPointer)
	assertRecoverableToken(t, "graph", snapshot.MemoryGraphVersion)
}

func assertRecoverableToken(t *testing.T, kind, raw string) {
	t.Helper()
	if strings.TrimSpace(raw) == "" {
		t.Fatalf("expected non-empty %s state token", kind)
	}
	if strings.HasPrefix(raw, kind+":") && !strings.HasPrefix(strings.TrimSpace(raw), "{") {
		t.Fatalf("expected recoverable JSON token for %s, got legacy digest %s", kind, raw)
	}
	if !strings.Contains(raw, `"schema_version"`) {
		t.Fatalf("expected schema_version in %s token, got %s", kind, raw)
	}
	if !strings.Contains(raw, `"kind":"`+kind+`"`) && !strings.Contains(raw, `"kind": "`+kind+`"`) {
		// encodeRecoverable always sets kind without spaces
		if !strings.Contains(raw, `"kind":"`+kind+`"`) {
			t.Fatalf("expected kind=%s in token, got %s", kind, raw)
		}
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

func TestSnapshotTrueRestoreGraph(t *testing.T) {
	ctx := context.Background()

	// Isolate SAMG global service for this test.
	prev := samg.GetSAMGService()
	svcGraph := samg.NewSAMGService(nil)
	samg.SetSAMGService(svcGraph)
	t.Cleanup(func() { samg.SetSAMGService(prev) })

	// Seed graph via real Triple types (ImportGraph expects []Triple).
	seedTriple := samg.Triple{
		ID:         samg.GenerateTripleID("entity:alice", samg.Predicates.RelatedTo, "entity:bob"),
		Subject:    samg.CreateNode("entity:alice", samg.EntityTypes.Concept, "Alice"),
		Predicate:  samg.Predicates.RelatedTo,
		Object:     samg.CreateNodeObject(samg.CreateNode("entity:bob", samg.EntityTypes.Concept, "Bob")),
		Confidence: 0.9,
		Timestamp:  time.Now().UnixMilli(),
		Source: samg.TripleSource{
			SessionID:        "snap-restore-graph",
			ExtractionMethod: samg.ExtractionUser,
		},
	}
	seed := &samg.JsonLdGraph{
		Context: samg.JsonLdContext{Vocab: "https://codeflow.ai/vocab/"},
		ID:      "codeflow:samg",
		Type:    "Graph",
		Graph:   []samg.Triple{seedTriple},
	}
	if _, err := svcGraph.ImportGraph(ctx, seed); err != nil {
		t.Fatalf("seed ImportGraph failed: %v", err)
	}

	provider := NewDefaultStateProvider()
	token, err := provider.CaptureMemoryGraphState(ctx)
	if err != nil {
		t.Fatalf("CaptureMemoryGraphState failed: %v", err)
	}
	assertRecoverableToken(t, "graph", token)

	// Mutate graph away from seed.
	mutatedTriple := samg.Triple{
		ID:         samg.GenerateTripleID("entity:mutated", samg.Predicates.RelatedTo, "entity:other"),
		Subject:    samg.CreateNode("entity:mutated", samg.EntityTypes.Concept, "Mutated"),
		Predicate:  samg.Predicates.RelatedTo,
		Object:     samg.CreateNodeObject(samg.CreateNode("entity:other", samg.EntityTypes.Concept, "Other")),
		Confidence: 0.5,
		Timestamp:  time.Now().UnixMilli(),
		Source: samg.TripleSource{
			SessionID:        "snap-restore-graph",
			ExtractionMethod: samg.ExtractionUser,
		},
	}
	mutated := &samg.JsonLdGraph{
		Context: samg.JsonLdContext{Vocab: "https://codeflow.ai/vocab/"},
		ID:      "codeflow:samg",
		Type:    "Graph",
		Graph:   []samg.Triple{mutatedTriple},
	}
	if _, err := svcGraph.ReplaceGraph(ctx, mutated); err != nil {
		t.Fatalf("mutate ReplaceGraph failed: %v", err)
	}

	// Restore from captured token.
	if err := provider.RestoreMemoryGraphState(ctx, token); err != nil {
		t.Fatalf("RestoreMemoryGraphState failed: %v", err)
	}

	restored, err := svcGraph.ExportGraph(ctx)
	if err != nil {
		t.Fatalf("ExportGraph after restore failed: %v", err)
	}
	if restored == nil || len(restored.Graph) == 0 {
		t.Fatalf("expected restored graph non-empty, got %#v", restored)
	}

	// Ensure seed entity is back and mutated entity is gone.
	raw, _ := json.Marshal(restored)
	body := string(raw)
	if !strings.Contains(body, "entity:alice") {
		t.Fatalf("restored graph missing seed content: %s", body)
	}
	if strings.Contains(body, "entity:mutated") {
		t.Fatalf("mutated entity should not remain after restore: %s", body)
	}
}

func TestSnapshotLegacyDigestNotRestorable(t *testing.T) {
	ctx := context.Background()
	provider := NewDefaultStateProvider()

	cases := []struct {
		name string
		fn   func() error
	}{
		{"conversation", func() error { return provider.RestoreConversationState(ctx, "conversation:deadbeef") }},
		{"vector", func() error { return provider.RestoreVectorState(ctx, "vector:cafebabe") }},
		{"graph", func() error { return provider.RestoreMemoryGraphState(ctx, "graph:0123456789abcdef") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.fn()
			if err == nil {
				t.Fatalf("expected error for legacy %s digest token", tc.name)
			}
			if !errors.Is(err, ErrNotRestorable) && !strings.Contains(err.Error(), "not restorable") {
				t.Fatalf("expected ErrNotRestorable for %s, got %v", tc.name, err)
			}
		})
	}
}

func TestSnapshotEmptyRecoverableRestoreSucceeds(t *testing.T) {
	ctx := context.Background()
	// Fresh isolated services so empty capture is truly empty.
	agent.SetAgentService(agent.NewInMemoryAgentService())
	memory.SetMemoryService(memory.NewInMemoryService())
	samg.SetSAMGService(samg.NewSAMGService(nil))

	svc := NewInMemorySnapshotService()
	snap, err := svc.Create(ctx, &SnapshotCreateRequest{Description: "empty recoverable", SessionID: "s-empty"})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	assertRecoverableToken(t, "conversation", snap.ConversationState)
	assertRecoverableToken(t, "vector", snap.VectorPointer)
	assertRecoverableToken(t, "graph", snap.MemoryGraphVersion)

	result, err := svc.Restore(ctx, snap.ID)
	if err != nil {
		t.Fatalf("Restore failed: %v", err)
	}
	if !result.ConversationRestored || !result.VectorRestored || !result.MemoryGraphRestored {
		t.Fatalf("expected conversation/vector/graph restored true, got %+v", result)
	}
	if len(result.Errors) != 0 {
		// Git may still succeed (no-op without env); other errors not expected.
		for _, e := range result.Errors {
			if strings.Contains(e, "conversation") || strings.Contains(e, "vector") || strings.Contains(e, "memory graph") {
				t.Fatalf("unexpected state restore errors: %v", result.Errors)
			}
		}
	}
}


func TestSnapshotTrueRestoreConversation(t *testing.T) {
	ctx := context.Background()
	prevAgent := agent.GetAgentService()
	agentSvc := agent.NewInMemoryAgentService()
	agent.SetAgentService(agentSvc)
	t.Cleanup(func() { agent.SetAgentService(prevAgent) })

	sessionID := "snap-restore-conversation"
	created, err := agentSvc.CreateAgent(ctx, &agent.AgentCreateRequest{
		Name:      "main-restore",
		Role:      agent.RoleMain,
		SessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	traceID := agentSvc.StartTrace(sessionID, created.ID, "seed_tool", map[string]interface{}{"q": "seed"})
	agentSvc.EndTrace(traceID, "seed-output", "completed")

	provider := NewDefaultStateProvider()
	token, err := provider.CaptureConversationState(ctx, sessionID)
	if err != nil {
		t.Fatalf("CaptureConversationState failed: %v", err)
	}
	assertRecoverableToken(t, "conversation", token)

	// Mutate conversation away from seed.
	mutated, err := agentSvc.CreateAgent(ctx, &agent.AgentCreateRequest{
		Name:      "mutated-agent",
		Role:      agent.RoleCoder,
		SessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("mutate CreateAgent failed: %v", err)
	}
	mutTraceID := agentSvc.StartTrace(sessionID, mutated.ID, "mut_tool", map[string]interface{}{"q": "mutated"})
	agentSvc.EndTrace(mutTraceID, "mutated-output", "completed")

	if err := provider.RestoreConversationState(ctx, token); err != nil {
		t.Fatalf("RestoreConversationState failed: %v", err)
	}

	restored, err := agentSvc.GetConversationTrace(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetConversationTrace after restore failed: %v", err)
	}
	if restored == nil {
		t.Fatal("expected restored conversation trace, got nil")
	}
	if restored.SessionID != sessionID {
		t.Fatalf("session id mismatch: got %s, want %s", restored.SessionID, sessionID)
	}
	if restored.Trace == nil {
		t.Fatal("expected restored root trace")
	}
	if restored.Trace.ToolName != "seed_tool" {
		t.Fatalf("expected seed_tool, got %q", restored.Trace.ToolName)
	}
	if restored.Trace.Output != "seed-output" {
		t.Fatalf("expected seed-output, got %q", restored.Trace.Output)
	}

	foundMain := false
	for _, a := range restored.Agents {
		if a.Name == "mutated-agent" {
			t.Fatalf("mutated agent should not remain after restore: %+v", restored.Agents)
		}
		if a.Name == "main-restore" || a.Role == agent.RoleMain {
			foundMain = true
		}
	}
	if !foundMain {
		t.Fatalf("expected seed main agent after restore, got %+v", restored.Agents)
	}
}

func TestSnapshotTrueRestoreVector(t *testing.T) {
	ctx := context.Background()
	prevMem := memory.GetMemoryService()
	memSvc := memory.NewInMemoryService()
	memory.SetMemoryService(memSvc)
	t.Cleanup(func() { memory.SetMemoryService(prevMem) })

	sessionID := "snap-restore-vector"
	seed, err := memSvc.Create(ctx, &memory.MemoryItemCreateRequest{
		Content:   "seed memory content",
		Type:      memory.MemoryTypeSTM,
		SessionID: sessionID,
		Source:    memory.SourceUser,
		Tags:      []string{"seed"},
	})
	if err != nil {
		t.Fatalf("seed Create failed: %v", err)
	}

	provider := NewDefaultStateProvider()
	token, err := provider.CaptureVectorState(ctx, sessionID)
	if err != nil {
		t.Fatalf("CaptureVectorState failed: %v", err)
	}
	assertRecoverableToken(t, "vector", token)

	// Mutate session memory away from seed.
	if _, err := memSvc.Create(ctx, &memory.MemoryItemCreateRequest{
		Content:   "mutated memory content",
		Type:      memory.MemoryTypeSTM,
		SessionID: sessionID,
		Source:    memory.SourceAssistant,
		Tags:      []string{"mutated"},
	}); err != nil {
		t.Fatalf("mutate Create failed: %v", err)
	}

	if err := provider.RestoreVectorState(ctx, token); err != nil {
		t.Fatalf("RestoreVectorState failed: %v", err)
	}

	listed, err := memSvc.List(ctx, &memory.MemoryListOptions{SessionID: sessionID, Limit: 100})
	if err != nil {
		t.Fatalf("List after restore failed: %v", err)
	}
	if listed == nil || listed.Total != 1 || len(listed.Items) != 1 {
		t.Fatalf("expected exactly one restored item, got total=%d items=%+v", listed.Total, listed.Items)
	}
	item := listed.Items[0]
	if item.ID != seed.ID {
		t.Fatalf("expected seed id %s, got %s", seed.ID, item.ID)
	}
	if item.Content != "seed memory content" {
		t.Fatalf("expected seed content restored, got %q", item.Content)
	}
	if strings.Contains(item.Content, "mutated") {
		t.Fatalf("mutated content should not remain: %q", item.Content)
	}
	for _, tag := range item.Tags {
		if tag == "mutated" {
			t.Fatalf("mutated tag should not remain: %+v", item.Tags)
		}
	}
}

func TestSnapshotTrueRestoreMultiKindE2E(t *testing.T) {
	ctx := context.Background()

	prevAgent := agent.GetAgentService()
	prevMem := memory.GetMemoryService()
	prevGraph := samg.GetSAMGService()
	agentSvc := agent.NewInMemoryAgentService()
	memSvc := memory.NewInMemoryService()
	graphSvc := samg.NewSAMGService(nil)
	agent.SetAgentService(agentSvc)
	memory.SetMemoryService(memSvc)
	samg.SetSAMGService(graphSvc)
	t.Cleanup(func() {
		agent.SetAgentService(prevAgent)
		memory.SetMemoryService(prevMem)
		samg.SetSAMGService(prevGraph)
	})

	sessionID := "snap-restore-e2e"

	// Seed conversation.
	ag, err := agentSvc.CreateAgent(ctx, &agent.AgentCreateRequest{
		Name:      "e2e-main",
		Role:      agent.RoleMain,
		SessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	tid := agentSvc.StartTrace(sessionID, ag.ID, "e2e_seed", map[string]interface{}{"k": "v"})
	agentSvc.EndTrace(tid, "e2e-seed-out", "completed")

	// Seed vector/memory.
	if _, err := memSvc.Create(ctx, &memory.MemoryItemCreateRequest{
		Content:   "e2e seed memory",
		Type:      memory.MemoryTypeSTM,
		SessionID: sessionID,
		Source:    memory.SourceUser,
		Tags:      []string{"e2e-seed"},
	}); err != nil {
		t.Fatalf("memory Create failed: %v", err)
	}

	// Seed graph.
	seedTriple := samg.Triple{
		ID:         samg.GenerateTripleID("entity:e2e-a", samg.Predicates.RelatedTo, "entity:e2e-b"),
		Subject:    samg.CreateNode("entity:e2e-a", samg.EntityTypes.Concept, "E2E-A"),
		Predicate:  samg.Predicates.RelatedTo,
		Object:     samg.CreateNodeObject(samg.CreateNode("entity:e2e-b", samg.EntityTypes.Concept, "E2E-B")),
		Confidence: 0.95,
		Timestamp:  time.Now().UnixMilli(),
		Source: samg.TripleSource{
			SessionID:        sessionID,
			ExtractionMethod: samg.ExtractionUser,
		},
	}
	if _, err := graphSvc.ImportGraph(ctx, &samg.JsonLdGraph{
		Context: samg.JsonLdContext{Vocab: "https://codeflow.ai/vocab/"},
		ID:      "codeflow:samg",
		Type:    "Graph",
		Graph:   []samg.Triple{seedTriple},
	}); err != nil {
		t.Fatalf("ImportGraph failed: %v", err)
	}

	// Capture full snapshot via service (uses default provider → real modules).
	snapSvc := NewInMemorySnapshotService()
	snap, err := snapSvc.Create(ctx, &SnapshotCreateRequest{
		Description: "multi-kind e2e",
		SessionID:   sessionID,
		Tags:        []string{"pr-5", "e2e"},
	})
	if err != nil {
		t.Fatalf("Create snapshot failed: %v", err)
	}
	assertRecoverableToken(t, "conversation", snap.ConversationState)
	assertRecoverableToken(t, "vector", snap.VectorPointer)
	assertRecoverableToken(t, "graph", snap.MemoryGraphVersion)

	// Mutate all three kinds.
	mutAg, err := agentSvc.CreateAgent(ctx, &agent.AgentCreateRequest{
		Name:      "e2e-mutated",
		Role:      agent.RoleCoder,
		SessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("mutate CreateAgent failed: %v", err)
	}
	mutTid := agentSvc.StartTrace(sessionID, mutAg.ID, "e2e_mut", map[string]interface{}{"k": "mut"})
	agentSvc.EndTrace(mutTid, "e2e-mut-out", "completed")

	if _, err := memSvc.Create(ctx, &memory.MemoryItemCreateRequest{
		Content:   "e2e mutated memory",
		Type:      memory.MemoryTypeSTM,
		SessionID: sessionID,
		Source:    memory.SourceAssistant,
		Tags:      []string{"e2e-mut"},
	}); err != nil {
		t.Fatalf("mutate memory Create failed: %v", err)
	}

	mutTriple := samg.Triple{
		ID:         samg.GenerateTripleID("entity:e2e-mut", samg.Predicates.RelatedTo, "entity:e2e-other"),
		Subject:    samg.CreateNode("entity:e2e-mut", samg.EntityTypes.Concept, "E2E-Mut"),
		Predicate:  samg.Predicates.RelatedTo,
		Object:     samg.CreateNodeObject(samg.CreateNode("entity:e2e-other", samg.EntityTypes.Concept, "E2E-Other")),
		Confidence: 0.4,
		Timestamp:  time.Now().UnixMilli(),
		Source: samg.TripleSource{
			SessionID:        sessionID,
			ExtractionMethod: samg.ExtractionUser,
		},
	}
	if _, err := graphSvc.ReplaceGraph(ctx, &samg.JsonLdGraph{
		Context: samg.JsonLdContext{Vocab: "https://codeflow.ai/vocab/"},
		ID:      "codeflow:samg",
		Type:    "Graph",
		Graph:   []samg.Triple{mutTriple},
	}); err != nil {
		t.Fatalf("mutate ReplaceGraph failed: %v", err)
	}

	// Full service restore.
	result, err := snapSvc.Restore(ctx, snap.ID)
	if err != nil {
		t.Fatalf("Restore failed: %v", err)
	}
	if !result.ConversationRestored || !result.VectorRestored || !result.MemoryGraphRestored {
		t.Fatalf("expected conversation/vector/graph restored, got %+v", result)
	}
	for _, e := range result.Errors {
		if strings.Contains(e, "conversation") || strings.Contains(e, "vector") || strings.Contains(e, "memory graph") {
			t.Fatalf("unexpected state restore errors: %v", result.Errors)
		}
	}

	// Assert conversation restored.
	conv, err := agentSvc.GetConversationTrace(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetConversationTrace failed: %v", err)
	}
	if conv == nil || conv.Trace == nil || conv.Trace.ToolName != "e2e_seed" {
		t.Fatalf("conversation not restored to seed: %+v", conv)
	}
	for _, a := range conv.Agents {
		if a.Name == "e2e-mutated" {
			t.Fatalf("mutated conversation agent still present: %+v", conv.Agents)
		}
	}

	// Assert vector restored.
	memList, err := memSvc.List(ctx, &memory.MemoryListOptions{SessionID: sessionID, Limit: 100})
	if err != nil {
		t.Fatalf("memory List failed: %v", err)
	}
	if memList.Total != 1 || len(memList.Items) != 1 || memList.Items[0].Content != "e2e seed memory" {
		t.Fatalf("vector not restored to seed: %+v", memList)
	}

	// Assert graph restored.
	graph, err := graphSvc.ExportGraph(ctx)
	if err != nil {
		t.Fatalf("ExportGraph failed: %v", err)
	}
	raw, _ := json.Marshal(graph)
	body := string(raw)
	if !strings.Contains(body, "entity:e2e-a") {
		t.Fatalf("graph missing seed entity: %s", body)
	}
	if strings.Contains(body, "entity:e2e-mut") {
		t.Fatalf("mutated graph entity still present: %s", body)
	}
}