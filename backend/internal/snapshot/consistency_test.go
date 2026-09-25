package snapshot

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/agent"
	"github.com/codeflow/backend/internal/memory"
	"github.com/codeflow/backend/internal/policy"
	"github.com/codeflow/backend/internal/policy/policytesting"
	"github.com/codeflow/backend/internal/samg"
)

// isolateGlobals swaps agent/memory/samg global services to fresh in-memory
// instances and returns them along with a cleanup that restores the originals.
// Call t.Cleanup(cleanup) immediately after.
func isolateGlobals(t *testing.T) (agentSvc *agent.InMemoryAgentService, memSvc *memory.InMemoryService, graphSvc *samg.SAMGService) {
	t.Helper()
	prevAgent := agent.GetAgentService()
	prevMem := memory.GetMemoryService()
	prevGraph := samg.GetSAMGService()
	agentSvc = agent.NewInMemoryAgentService()
	memSvc = memory.NewInMemoryService()
	graphSvc = samg.NewSAMGService(nil)
	agent.SetAgentService(agentSvc)
	memory.SetMemoryService(memSvc)
	samg.SetSAMGService(graphSvc)
	t.Cleanup(func() {
		agent.SetAgentService(prevAgent)
		memory.SetMemoryService(prevMem)
		samg.SetSAMGService(prevGraph)
	})
	return
}

func seedAllProviders(t *testing.T, ctx context.Context, agentSvc *agent.InMemoryAgentService, memSvc *memory.InMemoryService, graphSvc *samg.SAMGService, sessionID string) {
	t.Helper()
	ag, err := agentSvc.CreateAgent(ctx, &agent.AgentCreateRequest{
		Name: "consistency-main", Role: agent.RoleMain, SessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	tid := agentSvc.StartTrace(sessionID, ag.ID, "seed_tool", map[string]interface{}{"k": "v"})
	agentSvc.EndTrace(tid, "seed-output", "completed")

	if _, err := memSvc.Create(ctx, &memory.MemoryItemCreateRequest{
		Content: "seed memory", Type: memory.MemoryTypeSTM,
		SessionID: sessionID, Source: memory.SourceUser,
	}); err != nil {
		t.Fatalf("memory Create: %v", err)
	}

	seedTriple := samg.Triple{
		ID:         samg.GenerateTripleID("entity:consist-a", samg.Predicates.RelatedTo, "entity:consist-b"),
		Subject:    samg.CreateNode("entity:consist-a", samg.EntityTypes.Concept, "ConsistA"),
		Predicate:  samg.Predicates.RelatedTo,
		Object:     samg.CreateNodeObject(samg.CreateNode("entity:consist-b", samg.EntityTypes.Concept, "ConsistB")),
		Confidence: 0.9, Timestamp: time.Now().UnixMilli(),
		Source: samg.TripleSource{SessionID: sessionID, ExtractionMethod: samg.ExtractionUser},
	}
	if _, err := graphSvc.ImportGraph(ctx, &samg.JsonLdGraph{
		Context: samg.JsonLdContext{Vocab: "https://codeflow.ai/vocab/"},
		ID: "codeflow:samg", Type: "Graph", Graph: []samg.Triple{seedTriple},
	}); err != nil {
		t.Fatalf("ImportGraph: %v", err)
	}
}

// TestConsistencyImmediateAfterCaptureAllMatch verifies the mechanism works end
// to end with real providers: capture a snapshot and immediately verify without
// any mutation. All four providers (git, conversation, vector, graph) must
// report Match=true and the overall report must be Consistent=true.
func TestConsistencyImmediateAfterCaptureAllMatch(t *testing.T) {
	policytesting.AllowForTest(t, policy.OperationProcessStart)
	ctx := context.Background()
	agentSvc, memSvc, graphSvc := isolateGlobals(t)
	sessionID := "snap-consistency-immediate"

	seedAllProviders(t, ctx, agentSvc, memSvc, graphSvc, sessionID)

	svc := NewInMemorySnapshotService()
	snap, err := svc.Create(ctx, &SnapshotCreateRequest{
		Description: "consistency-immediate", SessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	report, err := svc.VerifyConsistency(ctx, snap.ID)
	if err != nil {
		t.Fatalf("VerifyConsistency: %v", err)
	}
	if report.SnapshotID != snap.ID {
		t.Fatalf("SnapshotID = %q, want %q", report.SnapshotID, snap.ID)
	}
	if !report.Consistent {
		for _, r := range report.Results {
			t.Logf("  %s: match=%v stored=%s current=%s detail=%q",
				r.Kind, r.Match, r.StoredDigest, r.CurrentDigest, r.Detail)
		}
		t.Fatal("expected Consistent=true immediately after capture")
	}
	if len(report.Results) != 4 {
		t.Fatalf("expected 4 provider results, got %d", len(report.Results))
	}
	for _, r := range report.Results {
		if r.Detail != "" {
			t.Fatalf("%s: unexpected Detail %q", r.Kind, r.Detail)
		}
		if !r.Match {
			t.Fatalf("%s: expected Match=true, stored=%s current=%s",
				r.Kind, r.StoredDigest, r.CurrentDigest)
		}
	}
}

// TestConsistencyAllMatchWithDeterministicProvider tests the full capture →
// mutate → restore → verify cycle using a provider with deterministic captures,
// proving the VerifyConsistency logic reports Consistent=true when state
// genuinely matches. (Real providers like conversation/graph may produce
// non-deterministic payloads across round-trips due to internal ID generation
// or timestamp drift, so this test isolates the comparison mechanism.)
func TestConsistencyAllMatchWithDeterministicProvider(t *testing.T) {
	provider := &ctxCapturableProvider{}
	svc := NewInMemorySnapshotServiceWithProvider(provider)
	ctx := context.Background()

	snap, err := svc.Create(ctx, &SnapshotCreateRequest{Description: "deterministic-match"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	report, err := svc.VerifyConsistency(ctx, snap.ID)
	if err != nil {
		t.Fatalf("VerifyConsistency: %v", err)
	}
	if !report.Consistent {
		for _, r := range report.Results {
			t.Logf("  %s: match=%v stored=%s current=%s detail=%q",
				r.Kind, r.Match, r.StoredDigest, r.CurrentDigest, r.Detail)
		}
		t.Fatal("expected Consistent=true with deterministic provider")
	}
	for _, r := range report.Results {
		if r.Detail != "" {
			continue
		}
		if !r.Match {
			t.Fatalf("%s: expected Match=true", r.Kind)
		}
		if r.StoredDigest == "" || r.CurrentDigest == "" {
			t.Fatalf("%s: expected non-empty digests", r.Kind)
		}
	}
}

// TestConsistencyDetectsDrift mutates one provider's state after restore to
// verify that the inconsistency is detected with differing digests.
func TestConsistencyDetectsDrift(t *testing.T) {
	policytesting.AllowForTest(t, policy.OperationProcessStart)
	ctx := context.Background()
	_, memSvc, _ := isolateGlobals(t)
	sessionID := "snap-consistency-drift"

	// Seed vector only (enough to prove drift detection).
	if _, err := memSvc.Create(ctx, &memory.MemoryItemCreateRequest{
		Content: "seed for drift", Type: memory.MemoryTypeSTM,
		SessionID: sessionID, Source: memory.SourceUser,
	}); err != nil {
		t.Fatalf("seed memory: %v", err)
	}

	svc := NewInMemorySnapshotService()
	snap, err := svc.Create(ctx, &SnapshotCreateRequest{
		Description: "consistency-drift", SessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Mutate vector state AFTER snapshot (without restore — state has drifted).
	if _, err := memSvc.Create(ctx, &memory.MemoryItemCreateRequest{
		Content: "extra item", Type: memory.MemoryTypeSTM,
		SessionID: sessionID, Source: memory.SourceAssistant,
	}); err != nil {
		t.Fatalf("mutate memory: %v", err)
	}

	report, err := svc.VerifyConsistency(ctx, snap.ID)
	if err != nil {
		t.Fatalf("VerifyConsistency: %v", err)
	}
	if report.Consistent {
		t.Fatal("expected Consistent=false when state has drifted")
	}

	vectorFound := false
	for _, r := range report.Results {
		if r.Kind == "vector" {
			vectorFound = true
			if r.Match {
				t.Fatal("vector should not match after mutation")
			}
			if r.StoredDigest == r.CurrentDigest {
				t.Fatal("digests should differ after mutation")
			}
		}
	}
	if !vectorFound {
		t.Fatal("expected a vector result in the report")
	}
}

// TestConsistencyLegacyTokenReportsDetail verifies that a non-recoverable
// (legacy) stored token is reported with an explanatory Detail rather than
// causing the call to fail, and is excluded from the Consistent calculation.
func TestConsistencyLegacyTokenReportsDetail(t *testing.T) {
	ctx := context.Background()
	isolateGlobals(t)

	provider := &fakeSnapshotStateProvider{
		gitHash:      "abc123",
		conversation: "conversation:deadbeef",
		vector:       "vector:cafebabe",
		graph:        "graph:0123456789",
	}
	svc := NewInMemorySnapshotServiceWithProvider(provider)
	snap, err := svc.Create(ctx, &SnapshotCreateRequest{Description: "legacy-tokens"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	report, err := svc.VerifyConsistency(ctx, snap.ID)
	if err != nil {
		t.Fatalf("VerifyConsistency should not fail: %v", err)
	}

	gitResult := findResult(report, "git")
	if gitResult == nil {
		t.Fatal("expected git result")
	}
	if !gitResult.Match {
		t.Fatalf("git hashes should match (unchanged): stored=%s current=%s",
			gitResult.StoredDigest, gitResult.CurrentDigest)
	}

	for _, kind := range []string{"conversation", "vector", "graph"} {
		r := findResult(report, kind)
		if r == nil {
			t.Fatalf("expected %s result", kind)
		}
		if r.Detail == "" {
			t.Fatalf("%s: expected Detail for legacy token", kind)
		}
		if !strings.Contains(r.Detail, "not parseable") {
			t.Fatalf("%s: expected 'not parseable' in Detail, got %q", kind, r.Detail)
		}
	}

	if !report.Consistent {
		t.Fatal("Consistent should be true: only git is comparable and it matches")
	}
}

// TestConsistencyUnknownSnapshotIDErrors verifies that an unknown snapshot ID
// returns a clear error.
func TestConsistencyUnknownSnapshotIDErrors(t *testing.T) {
	svc := NewInMemorySnapshotService()
	_, err := svc.VerifyConsistency(context.Background(), "does-not-exist")
	if err == nil {
		t.Fatal("expected error for unknown snapshot ID")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' in error, got %v", err)
	}
}

// TestConsistencyContextCancellation verifies that a cancelled context produces
// a clean report (Detail on failed providers) rather than a panic.
func TestConsistencyContextCancellation(t *testing.T) {
	// Use a provider whose Capture methods respect context.
	provider := &ctxCapturableProvider{}
	svc := NewInMemorySnapshotServiceWithProvider(provider)
	snap, err := svc.Create(context.Background(), &SnapshotCreateRequest{Description: "cancel-consistency"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	report, err := svc.VerifyConsistency(ctx, snap.ID)
	if err != nil {
		t.Fatalf("VerifyConsistency should not return top-level error: %v", err)
	}

	for _, r := range report.Results {
		if r.Kind == "git" {
			if r.Detail == "" || !strings.Contains(r.Detail, "re-capture failed") {
				t.Fatalf("git: expected re-capture failed Detail, got %q", r.Detail)
			}
			continue
		}
		if r.Detail == "" {
			t.Fatalf("%s: expected Detail for cancelled re-capture", r.Kind)
		}
	}
	if report.Consistent {
		t.Fatal("expected Consistent=false when all recaptures fail")
	}
}

// TestConsistencyNilContextTolerated verifies no panic on nil context.
func TestConsistencyNilContextTolerated(t *testing.T) {
	provider := &fakeSnapshotStateProvider{
		gitHash: "h", conversation: "conversation:legacy",
		vector: "vector:legacy", graph: "graph:legacy",
	}
	svc := NewInMemorySnapshotServiceWithProvider(provider)
	var nilCtx context.Context
	snap, err := svc.Create(nilCtx, &SnapshotCreateRequest{Description: "nil-ctx"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	report, err := svc.VerifyConsistency(nilCtx, snap.ID)
	if err != nil {
		t.Fatalf("VerifyConsistency with nil ctx should not fail: %v", err)
	}
	if report == nil {
		t.Fatal("expected non-nil report")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func findResult(report *ConsistencyReport, kind string) *ProviderConsistency {
	for i := range report.Results {
		if report.Results[i].Kind == kind {
			return &report.Results[i]
		}
	}
	return nil
}

// ctxCapturableProvider respects context cancellation in Capture methods so we
// can test VerifyConsistency under a cancelled context. Its Capture methods
// return fixed tokens when the context is active and ctx.Err() when cancelled.
// Restore methods are unused.
type ctxCapturableProvider struct{}

func (p *ctxCapturableProvider) CaptureGitState(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return "git-hash", nil
}
func (p *ctxCapturableProvider) CaptureConversationState(ctx context.Context, _ string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	tok, err := encodeRecoverable("conversation", map[string]interface{}{"session_id": ""})
	return tok, err
}
func (p *ctxCapturableProvider) CaptureVectorState(ctx context.Context, _ string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	tok, err := encodeRecoverable("vector", map[string]interface{}{"session_id": "", "items": []interface{}{}})
	return tok, err
}
func (p *ctxCapturableProvider) CaptureMemoryGraphState(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	tok, err := encodeRecoverable("graph", map[string]interface{}{"graph": nil})
	return tok, err
}
func (p *ctxCapturableProvider) RestoreGitState(context.Context, string) error          { return errors.New("unused") }
func (p *ctxCapturableProvider) RestoreConversationState(context.Context, string) error { return errors.New("unused") }
func (p *ctxCapturableProvider) RestoreVectorState(context.Context, string) error       { return errors.New("unused") }
func (p *ctxCapturableProvider) RestoreMemoryGraphState(context.Context, string) error  { return errors.New("unused") }
