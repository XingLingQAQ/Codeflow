package floweng

import (
	"context"
	"strings"
	"testing"
)

// stubGuard is a test ExecutionGuard whose Busy result is set directly.
type stubGuard struct {
	busy   bool
	reason string
}

func (g *stubGuard) Busy() (bool, string) { return g.busy, g.reason }

// A busy guard blocks Advance before any mutation, then permits it once idle.
func TestExecutionGuardBlocksAdvance(t *testing.T) {
	e := NewInMemoryEngine(nil)
	guard := &stubGuard{}
	e.SetExecutionGuard(guard)

	flow, err := e.Create(context.Background(), &CreateFlowRequest{ProjectID: "p"})
	if err != nil {
		t.Fatal(err)
	}

	guard.busy = true
	guard.reason = "git commit in progress"
	before, _ := e.Get(context.Background(), flow.ID)
	if _, err := e.Advance(context.Background(), flow.ID, &AdvanceRequest{}); err == nil {
		t.Fatal("advance must be blocked while guard is busy")
	} else if !strings.Contains(err.Error(), "stage transition locked") || !strings.Contains(err.Error(), guard.reason) {
		t.Fatalf("guard error should mention lock and reason, got %q", err.Error())
	}
	after, _ := e.Get(context.Background(), flow.ID)
	assertFlowUnchanged(t, before, after)

	guard.busy = false
	got, err := e.Advance(context.Background(), flow.ID, &AdvanceRequest{})
	if err != nil {
		t.Fatalf("advance should succeed when idle: %v", err)
	}
	if stageByType(got, StageTypeIdea).Status != StageStatusDone {
		t.Fatal("idea should be done after an unblocked advance")
	}
}

// A busy guard blocks Loop before any mutation, then permits it once idle.
func TestExecutionGuardBlocksLoop(t *testing.T) {
	e := NewInMemoryEngine(nil)
	guard := &stubGuard{}
	e.SetExecutionGuard(guard)

	flow, err := e.Create(context.Background(), &CreateFlowRequest{ProjectID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	flow = advanceToStage(t, e, flow, StageTypeCoding)
	flow, err = e.Advance(context.Background(), flow.ID, &AdvanceRequest{}) // coding done -> review active
	if err != nil {
		t.Fatal(err)
	}
	review := stageByType(flow, StageTypeReview)
	coding := stageByType(flow, StageTypeCoding)

	guard.busy = true
	guard.reason = "write in progress"
	before, _ := e.Get(context.Background(), flow.ID)
	if _, err := e.Loop(context.Background(), flow.ID, &LoopRequest{
		FromStageID: review.ID, ToStageID: coding.ID, Reason: "retry",
	}); err == nil {
		t.Fatal("loop must be blocked while guard is busy")
	} else if !strings.Contains(err.Error(), "stage transition locked") {
		t.Fatalf("guard error should mention lock, got %q", err.Error())
	}
	after, _ := e.Get(context.Background(), flow.ID)
	assertFlowUnchanged(t, before, after)

	guard.busy = false
	looped, err := e.Loop(context.Background(), flow.ID, &LoopRequest{
		FromStageID: review.ID, ToStageID: coding.ID, Reason: "retry",
	})
	if err != nil {
		t.Fatalf("loop should succeed when idle: %v", err)
	}
	if activeStage(looped).Type != StageTypeCoding {
		t.Fatalf("loop should reactivate coding, got %v", activeStage(looped))
	}
}

// Skip is not a stage transition guarded by the execution lock.
func TestExecutionGuardDoesNotBlockSkip(t *testing.T) {
	e := NewInMemoryEngine(nil)
	e.SetExecutionGuard(&stubGuard{busy: true, reason: "busy"})

	flow, err := e.Create(context.Background(), &CreateFlowRequest{ProjectID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	research := stageByType(flow, StageTypeResearch)
	if research == nil || !research.Optional {
		t.Fatal("expected optional research stage")
	}
	got, err := e.Skip(context.Background(), flow.ID, &SkipRequest{StageID: research.ID})
	if err != nil {
		t.Fatalf("skip should succeed even while guard is busy: %v", err)
	}
	if stageByType(got, StageTypeResearch).Status != StageStatusSkipped {
		t.Fatalf("research should be skipped, got %s", stageByType(got, StageTypeResearch).Status)
	}
}
