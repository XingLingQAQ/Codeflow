// Package snapshot - restore-cycle, atomicity, cancellation, git-gate spec tests.
//
// Coverage targets NOT in snapshot_test.go:
//   - Destructive git opt-in gate (env var blocks/allows)
//   - Partial-restore failure atomicity (real + fake provider)
//   - Best-effort continuation past early provider failures
//   - Context cancellation (pre-cancel + mid-cancel) propagation
//   - Restore idempotency (real vector provider)
//   - Restore unknown snapshot ID
//   - Nil context tolerance
package snapshot

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/agent"
	backendgit "github.com/codeflow/backend/internal/git"
	"github.com/codeflow/backend/internal/memory"
	"github.com/codeflow/backend/internal/samg"
)

// ---------------------------------------------------------------------------
// Fake git manager — embeds IGitManager, overrides only GetCurrentHash + Reset
// ---------------------------------------------------------------------------

type gitResetCall struct {
	hash string
	hard bool
}

type recordingGitManager struct {
	backendgit.IGitManager
	mu          sync.Mutex
	currentHash string
	resetErr    error
	resetCalls  []gitResetCall
}

func (m *recordingGitManager) GetCurrentHash(_ context.Context) (string, error) {
	return m.currentHash, nil
}

func (m *recordingGitManager) Reset(_ context.Context, hash string, hard bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resetCalls = append(m.resetCalls, gitResetCall{hash: hash, hard: hard})
	return m.resetErr
}

// ---------------------------------------------------------------------------
// Context-aware provider for cancellation tests
// ---------------------------------------------------------------------------

type ctxAwareProvider struct {
	cancelDuringGit context.CancelFunc
}

func (p *ctxAwareProvider) CaptureGitState(context.Context) (string, error) {
	return "git-hash", nil
}
func (p *ctxAwareProvider) CaptureConversationState(context.Context, string) (string, error) {
	return "conversation-token", nil
}
func (p *ctxAwareProvider) CaptureVectorState(context.Context, string) (string, error) {
	return "vector-token", nil
}
func (p *ctxAwareProvider) CaptureMemoryGraphState(context.Context) (string, error) {
	return "graph-token", nil
}
func (p *ctxAwareProvider) RestoreGitState(ctx context.Context, _ string) error {
	if p.cancelDuringGit != nil {
		p.cancelDuringGit()
		return nil
	}
	return ctx.Err()
}
func (p *ctxAwareProvider) RestoreConversationState(ctx context.Context, _ string) error {
	return ctx.Err()
}
func (p *ctxAwareProvider) RestoreVectorState(ctx context.Context, _ string) error {
	return ctx.Err()
}
func (p *ctxAwareProvider) RestoreMemoryGraphState(ctx context.Context, _ string) error {
	return ctx.Err()
}

// ===========================================================================
// Destructive git opt-in gate
// ===========================================================================

func TestRestoreGitOptInGateBlocksWhenDisabled(t *testing.T) {
	t.Setenv("CODEFLOW_SNAPSHOT_ENABLE_GIT_RESTORE", "false")
	gm := &recordingGitManager{}
	p := &defaultStateProvider{gitManager: gm}

	if err := p.RestoreGitState(context.Background(), "abc123def"); err != nil {
		t.Fatalf("gate disabled should no-op without error, got %v", err)
	}
	gm.mu.Lock()
	n := len(gm.resetCalls)
	gm.mu.Unlock()
	if n != 0 {
		t.Fatalf("git reset must not run when opt-in disabled; got %d reset calls", n)
	}
}

func TestRestoreGitOptInGateBlocksWhenUnset(t *testing.T) {
	t.Setenv("CODEFLOW_SNAPSHOT_ENABLE_GIT_RESTORE", "")
	gm := &recordingGitManager{}
	p := &defaultStateProvider{gitManager: gm}

	if err := p.RestoreGitState(context.Background(), "abc123def"); err != nil {
		t.Fatalf("gate unset should no-op without error, got %v", err)
	}
	gm.mu.Lock()
	n := len(gm.resetCalls)
	gm.mu.Unlock()
	if n != 0 {
		t.Fatalf("git reset must not run when env var unset; got %d reset calls", n)
	}
}

func TestRestoreGitOptInGateAllowsWhenEnabled(t *testing.T) {
	t.Setenv("CODEFLOW_SNAPSHOT_ENABLE_GIT_RESTORE", "true")
	gm := &recordingGitManager{}
	p := &defaultStateProvider{gitManager: gm}

	if err := p.RestoreGitState(context.Background(), "abc123def"); err != nil {
		t.Fatalf("gate enabled should call reset, got err %v", err)
	}
	gm.mu.Lock()
	defer gm.mu.Unlock()
	if len(gm.resetCalls) != 1 {
		t.Fatalf("want 1 reset call, got %d", len(gm.resetCalls))
	}
	if gm.resetCalls[0].hash != "abc123def" {
		t.Fatalf("reset hash = %q, want abc123def", gm.resetCalls[0].hash)
	}
	if !gm.resetCalls[0].hard {
		t.Fatal("git restore must use hard reset")
	}
}

func TestRestoreGitEmptyHashRejected(t *testing.T) {
	t.Setenv("CODEFLOW_SNAPSHOT_ENABLE_GIT_RESTORE", "true")
	gm := &recordingGitManager{}
	p := &defaultStateProvider{gitManager: gm}

	if err := p.RestoreGitState(context.Background(), "   "); err == nil {
		t.Fatal("empty git hash should be rejected")
	}
	gm.mu.Lock()
	defer gm.mu.Unlock()
	if len(gm.resetCalls) != 0 {
		t.Fatalf("empty hash must not trigger reset, got %d calls", len(gm.resetCalls))
	}
}

func TestRestoreGitResetErrorPropagates(t *testing.T) {
	t.Setenv("CODEFLOW_SNAPSHOT_ENABLE_GIT_RESTORE", "true")
	gm := &recordingGitManager{resetErr: errors.New("reset denied by policy")}
	p := &defaultStateProvider{gitManager: gm}

	err := p.RestoreGitState(context.Background(), "abc123")
	if err == nil || !strings.Contains(err.Error(), "reset denied") {
		t.Fatalf("reset error should propagate, got %v", err)
	}
}

// ===========================================================================
// Best-effort continuation / partial-failure (fake provider — contract level)
// ===========================================================================

func TestServiceRestoreContinuesAfterEarlyProviderFailures(t *testing.T) {
	provider := &fakeSnapshotStateProvider{
		gitHash:      "git:h",
		conversation: "conversation:c",
		vector:       "vector:v",
		graph:        "graph:g",
		restoreErrs: map[string]error{
			"git":          errors.New("git boom"),
			"conversation": errors.New("conversation boom"),
		},
	}
	svc := NewInMemorySnapshotServiceWithProvider(provider)
	snap, err := svc.Create(context.Background(), &SnapshotCreateRequest{Description: "continue"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	result, err := svc.Restore(context.Background(), snap.ID)
	if err != nil {
		t.Fatalf("Restore top-level error: %v", err)
	}

	if provider.restoreVector != provider.vector {
		t.Fatalf("vector restore should still be attempted after earlier failures; got %q", provider.restoreVector)
	}
	if provider.restoreGraph != provider.graph {
		t.Fatalf("graph restore should still be attempted after earlier failures; got %q", provider.restoreGraph)
	}
	if result.GitRestored || result.ConversationRestored {
		t.Fatalf("git/conversation should have failed: %+v", result)
	}
	if !result.VectorRestored || !result.MemoryGraphRestored {
		t.Fatalf("vector/graph should have succeeded: %+v", result)
	}
	if len(result.Errors) != 2 {
		t.Fatalf("expected exactly 2 errors, got %+v", result.Errors)
	}
}

// ===========================================================================
// Partial-failure atomicity — real providers, tampered graph token
// ===========================================================================

// TestServiceRestorePartialFailureIsNonAtomic demonstrates the non-atomic,
// best-effort restore contract: when a later provider fails (here, graph via
// digest-tampered token), earlier providers' side effects persist — no rollback.
func TestServiceRestorePartialFailureIsNonAtomic(t *testing.T) {
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

	sessionID := "snap-partial-atomicity"

	ag, err := agentSvc.CreateAgent(ctx, &agent.AgentCreateRequest{
		Name: "partial-main", Role: agent.RoleMain, SessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	tid := agentSvc.StartTrace(sessionID, ag.ID, "seed_tool", map[string]interface{}{"k": "v"})
	agentSvc.EndTrace(tid, "seed-output", "completed")

	if _, err := memSvc.Create(ctx, &memory.MemoryItemCreateRequest{
		Content: "seed memory", Type: memory.MemoryTypeSTM, SessionID: sessionID, Source: memory.SourceUser,
	}); err != nil {
		t.Fatalf("seed memory Create: %v", err)
	}

	seedTriple := samg.Triple{
		ID:         samg.GenerateTripleID("entity:partial-a", samg.Predicates.RelatedTo, "entity:partial-b"),
		Subject:    samg.CreateNode("entity:partial-a", samg.EntityTypes.Concept, "PartialA"),
		Predicate:  samg.Predicates.RelatedTo,
		Object:     samg.CreateNodeObject(samg.CreateNode("entity:partial-b", samg.EntityTypes.Concept, "PartialB")),
		Confidence: 0.9, Timestamp: time.Now().UnixMilli(),
		Source: samg.TripleSource{SessionID: sessionID, ExtractionMethod: samg.ExtractionUser},
	}
	if _, err := graphSvc.ImportGraph(ctx, &samg.JsonLdGraph{
		Context: samg.JsonLdContext{Vocab: "https://codeflow.ai/vocab/"},
		ID:      "codeflow:samg", Type: "Graph", Graph: []samg.Triple{seedTriple},
	}); err != nil {
		t.Fatalf("seed ImportGraph: %v", err)
	}

	snapSvc := NewInMemorySnapshotService()
	snap, err := snapSvc.Create(ctx, &SnapshotCreateRequest{
		Description: "partial-atomicity", SessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("Create snapshot: %v", err)
	}

	// Mutate all three kinds.
	mutAg, err := agentSvc.CreateAgent(ctx, &agent.AgentCreateRequest{
		Name: "mutated-agent", Role: agent.RoleCoder, SessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("mutate CreateAgent: %v", err)
	}
	mutTid := agentSvc.StartTrace(sessionID, mutAg.ID, "mut_tool", map[string]interface{}{"k": "mut"})
	agentSvc.EndTrace(mutTid, "mut-output", "completed")

	if _, err := memSvc.Create(ctx, &memory.MemoryItemCreateRequest{
		Content: "mutated memory", Type: memory.MemoryTypeSTM, SessionID: sessionID, Source: memory.SourceAssistant,
	}); err != nil {
		t.Fatalf("mutate memory Create: %v", err)
	}

	mutTriple := samg.Triple{
		ID:         samg.GenerateTripleID("entity:mutated", samg.Predicates.RelatedTo, "entity:other"),
		Subject:    samg.CreateNode("entity:mutated", samg.EntityTypes.Concept, "Mutated"),
		Predicate:  samg.Predicates.RelatedTo,
		Object:     samg.CreateNodeObject(samg.CreateNode("entity:other", samg.EntityTypes.Concept, "Other")),
		Confidence: 0.4, Timestamp: time.Now().UnixMilli(),
		Source: samg.TripleSource{SessionID: sessionID, ExtractionMethod: samg.ExtractionUser},
	}
	if _, err := graphSvc.ReplaceGraph(ctx, &samg.JsonLdGraph{
		Context: samg.JsonLdContext{Vocab: "https://codeflow.ai/vocab/"},
		ID:      "codeflow:samg", Type: "Graph", Graph: []samg.Triple{mutTriple},
	}); err != nil {
		t.Fatalf("mutate ReplaceGraph: %v", err)
	}

	// Corrupt only the service's internal graph token. Public Create/Get/List
	// results are detached copies and cannot mutate restore-critical state.
	snapSvc.mu.Lock()
	snapSvc.snapshots[snap.ID].MemoryGraphVersion = tamperRecoverablePayload(t, snapSvc.snapshots[snap.ID].MemoryGraphVersion)
	snapSvc.mu.Unlock()

	result, err := snapSvc.Restore(ctx, snap.ID)
	if err != nil {
		t.Fatalf("Restore top-level error (should be nil): %v", err)
	}

	if !result.ConversationRestored {
		t.Fatal("conversation restore should have succeeded (before graph)")
	}
	if !result.VectorRestored {
		t.Fatal("vector restore should have succeeded (before graph)")
	}
	if result.MemoryGraphRestored {
		t.Fatal("graph restore should have FAILED (tampered token)")
	}
	graphErrFound := false
	for _, e := range result.Errors {
		if strings.Contains(e, "memory graph") {
			graphErrFound = true
		}
	}
	if !graphErrFound {
		t.Fatalf("expected memory graph error in result.Errors, got %+v", result.Errors)
	}

	// Verify NON-ATOMIC: conversation + vector rolled back to seed (committed),
	// graph left in mutated state (no cross-provider rollback).
	conv, err := agentSvc.GetConversationTrace(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetConversationTrace: %v", err)
	}
	if conv == nil || conv.Trace == nil || conv.Trace.ToolName != "seed_tool" {
		t.Fatalf("conversation should be restored to seed (seed_tool), got %+v", conv)
	}

	memList, err := memSvc.List(ctx, &memory.MemoryListOptions{SessionID: sessionID, Limit: 100})
	if err != nil {
		t.Fatalf("memory List: %v", err)
	}
	if memList.Total != 1 || memList.Items[0].Content != "seed memory" {
		t.Fatalf("vector should be restored to seed, got total=%d content=%q",
			memList.Total, memList.Items[0].Content)
	}

	graph, err := graphSvc.ExportGraph(ctx)
	if err != nil {
		t.Fatalf("ExportGraph: %v", err)
	}
	raw, _ := json.Marshal(graph)
	body := string(raw)
	if !strings.Contains(body, "entity:mutated") {
		t.Fatalf("graph should remain mutated (no rollback across providers): %s", body)
	}
}

// ===========================================================================
// Context cancellation
// ===========================================================================

func TestServiceRestorePropagatesCancellationToProviders(t *testing.T) {
	svc := NewInMemorySnapshotServiceWithProvider(&ctxAwareProvider{})
	snap, err := svc.Create(context.Background(), &SnapshotCreateRequest{Description: "cancel"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := svc.Restore(ctx, snap.ID)
	if err != nil {
		t.Fatalf("Restore returns nil top-level error even when cancelled (characterization): %v", err)
	}
	if result.GitRestored || result.ConversationRestored || result.VectorRestored || result.MemoryGraphRestored {
		t.Fatalf("no provider should report restored under cancelled context: %+v", result)
	}
	if len(result.Errors) != 4 {
		t.Fatalf("expected 4 provider errors under pre-cancelled context, got %d: %+v",
			len(result.Errors), result.Errors)
	}
}

func TestServiceRestoreContinuesPastMidCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	svc := NewInMemorySnapshotServiceWithProvider(&ctxAwareProvider{cancelDuringGit: cancel})
	snap, err := svc.Create(context.Background(), &SnapshotCreateRequest{Description: "cancel-mid"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	result, err := svc.Restore(ctx, snap.ID)
	if err != nil {
		t.Fatalf("Restore should not return a top-level error: %v", err)
	}
	if !result.GitRestored {
		t.Fatal("git restore (before cancellation) should have succeeded")
	}
	if result.ConversationRestored || result.VectorRestored || result.MemoryGraphRestored {
		t.Fatalf("providers after mid-restore cancellation should fail: %+v", result)
	}
	if len(result.Errors) != 3 {
		t.Fatalf("expected 3 post-cancellation errors, got %d: %+v",
			len(result.Errors), result.Errors)
	}
}

// ===========================================================================
// Restore idempotency (real vector provider)
// ===========================================================================

func TestServiceRestoreIsIdempotent(t *testing.T) {
	ctx := context.Background()
	prevMem := memory.GetMemoryService()
	memSvc := memory.NewInMemoryService()
	memory.SetMemoryService(memSvc)
	t.Cleanup(func() { memory.SetMemoryService(prevMem) })

	sessionID := "snap-idempotent"
	seed, err := memSvc.Create(ctx, &memory.MemoryItemCreateRequest{
		Content: "seed for idempotency", Type: memory.MemoryTypeSTM,
		SessionID: sessionID, Source: memory.SourceUser,
	})
	if err != nil {
		t.Fatalf("seed Create: %v", err)
	}

	provider := NewDefaultStateProvider()
	token, err := provider.CaptureVectorState(ctx, sessionID)
	if err != nil {
		t.Fatalf("CaptureVectorState: %v", err)
	}

	if _, err := memSvc.Create(ctx, &memory.MemoryItemCreateRequest{
		Content: "mutated for idempotency", Type: memory.MemoryTypeSTM,
		SessionID: sessionID, Source: memory.SourceAssistant,
	}); err != nil {
		t.Fatalf("mutate Create: %v", err)
	}

	if err := provider.RestoreVectorState(ctx, token); err != nil {
		t.Fatalf("first RestoreVectorState: %v", err)
	}
	first, err := memSvc.List(ctx, &memory.MemoryListOptions{SessionID: sessionID, Limit: 100})
	if err != nil {
		t.Fatalf("List after first restore: %v", err)
	}
	if first.Total != 1 || first.Items[0].Content != "seed for idempotency" {
		t.Fatalf("first restore failed to restore seed: total=%d", first.Total)
	}

	if err := provider.RestoreVectorState(ctx, token); err != nil {
		t.Fatalf("second RestoreVectorState: %v", err)
	}
	second, err := memSvc.List(ctx, &memory.MemoryListOptions{SessionID: sessionID, Limit: 100})
	if err != nil {
		t.Fatalf("List after second restore: %v", err)
	}
	if second.Total != 1 || second.Items[0].ID != seed.ID || second.Items[0].Content != "seed for idempotency" {
		t.Fatalf("restore is not idempotent: total=%d id=%s content=%q",
			second.Total, second.Items[0].ID, second.Items[0].Content)
	}
}

// ===========================================================================
// Edge: unknown snapshot ID, nil context
// ===========================================================================

func TestServiceRestoreUnknownIDErrors(t *testing.T) {
	svc := NewInMemorySnapshotService()
	_, err := svc.Restore(context.Background(), "does-not-exist-id")
	if err == nil {
		t.Fatal("expected error restoring unknown snapshot ID")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' in error, got %v", err)
	}
}

func TestServiceRestoreNilContextTolerated(t *testing.T) {
	provider := &fakeSnapshotStateProvider{
		gitHash: "git:nil-ctx", conversation: "conversation:nil-ctx",
		vector: "vector:nil-ctx", graph: "graph:nil-ctx",
	}
	svc := NewInMemorySnapshotServiceWithProvider(provider)

	var nilCtx context.Context
	snap, err := svc.Create(nilCtx, &SnapshotCreateRequest{Description: "nil-ctx"})
	if err != nil {
		t.Fatalf("Create with nil ctx: %v", err)
	}

	result, err := svc.Restore(nilCtx, snap.ID)
	if err != nil {
		t.Fatalf("Restore with nil ctx should be tolerated: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil RestoreResult")
	}
}
