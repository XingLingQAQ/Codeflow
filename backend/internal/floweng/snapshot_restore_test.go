package floweng

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// countingSnapshotCreator hands each stage completion a distinct snapshot id.
type countingSnapshotCreator struct{ n int }

func (s *countingSnapshotCreator) CreateStageSnapshot(ctx context.Context, flow *Flow, stage *Stage, sessionID string) (string, error) {
	s.n++
	return fmt.Sprintf("snap-%d", s.n), nil
}

// stubRestorer captures the arguments of the last RestoreStageSnapshot call.
type stubRestorer struct {
	calls      int
	gotID      string
	gotStageID string
	err        error
}

func (r *stubRestorer) RestoreStageSnapshot(ctx context.Context, flow *Flow, target *Stage, snapshotID string) error {
	r.calls++
	r.gotID = snapshotID
	if target != nil {
		r.gotStageID = target.ID
	}
	return r.err
}

// loopToCodingReady drives a new_project flow until coding is done (with a
// snapshot) and review is the active stage, returning the flow.
func loopToCodingReady(t *testing.T, e *InMemoryEngine) *Flow {
	t.Helper()
	flow, err := e.Create(context.Background(), &CreateFlowRequest{ProjectID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	flow = advanceToStage(t, e, flow, StageTypeCoding)
	flow, err = e.Advance(context.Background(), flow.ID, &AdvanceRequest{}) // coding done -> review active
	if err != nil {
		t.Fatal(err)
	}
	return flow
}

// Loop restores the target stage's completion snapshot and emits flow.restored.
func TestLoopRestoresTargetSnapshot(t *testing.T) {
	e := NewInMemoryEngine(&countingSnapshotCreator{})
	restorer := &stubRestorer{}
	e.SetSnapshotRestorer(restorer)

	flow := loopToCodingReady(t, e)
	coding := stageByType(flow, StageTypeCoding)
	review := stageByType(flow, StageTypeReview)
	wantSnap := coding.SnapshotID
	if wantSnap == "" {
		t.Fatal("coding should carry a snapshot id after completion")
	}

	flow, err := e.Loop(context.Background(), flow.ID, &LoopRequest{
		FromStageID: review.ID, ToStageID: coding.ID, Reason: "tests failed",
	})
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if restorer.calls != 1 {
		t.Fatalf("restorer calls=%d want 1", restorer.calls)
	}
	if restorer.gotID != wantSnap {
		t.Fatalf("restorer got snapshot %q want %q", restorer.gotID, wantSnap)
	}
	if restorer.gotStageID != coding.ID {
		t.Fatalf("restorer got stage %q want %q", restorer.gotStageID, coding.ID)
	}
	if !flowHasEvent(flow.Events, "flow.restored") {
		t.Fatalf("expected flow.restored event, got %v", flowEventTypes(flow.Events))
	}
	if stageByType(flow, StageTypeCoding).Status != StageStatusActive {
		t.Fatal("coding should be active after loop")
	}
}

// A restore failure aborts the loop with the flow left completely unchanged.
func TestLoopRestoreErrorAbortsUnchanged(t *testing.T) {
	e := NewInMemoryEngine(&countingSnapshotCreator{})
	e.SetSnapshotRestorer(&stubRestorer{err: fmt.Errorf("git restore failed")})

	flow := loopToCodingReady(t, e)
	coding := stageByType(flow, StageTypeCoding)
	review := stageByType(flow, StageTypeReview)

	before, _ := e.Get(context.Background(), flow.ID)
	_, err := e.Loop(context.Background(), flow.ID, &LoopRequest{
		FromStageID: review.ID, ToStageID: coding.ID, Reason: "x",
	})
	if err == nil {
		t.Fatal("expected loop to fail on restore error")
	}
	if !strings.Contains(err.Error(), "restore stage snapshot") {
		t.Fatalf("error should wrap the restore failure, got %q", err.Error())
	}
	after, _ := e.Get(context.Background(), flow.ID)
	assertFlowUnchanged(t, before, after)
	if flowHasEvent(after.Events, "flow.restored") {
		t.Fatal("failed restore must not emit flow.restored")
	}
}

// Without a restorer the loop behaves exactly as before.
func TestLoopWithoutRestorerWorks(t *testing.T) {
	e := NewInMemoryEngine(&countingSnapshotCreator{})
	flow := loopToCodingReady(t, e)
	coding := stageByType(flow, StageTypeCoding)
	review := stageByType(flow, StageTypeReview)

	flow, err := e.Loop(context.Background(), flow.ID, &LoopRequest{
		FromStageID: review.ID, ToStageID: coding.ID, Reason: "x",
	})
	if err != nil {
		t.Fatalf("loop without restorer: %v", err)
	}
	if activeStage(flow).Type != StageTypeCoding {
		t.Fatal("coding should be active after loop")
	}
	if flowHasEvent(flow.Events, "flow.restored") {
		t.Fatal("no restorer must mean no flow.restored event")
	}
}

// A target stage with no snapshot id (no SnapshotCreator was configured) does
// not invoke the restorer.
func TestLoopEmptySnapshotSkipsRestorer(t *testing.T) {
	e := NewInMemoryEngine(nil) // no snapshot creator -> empty SnapshotID
	restorer := &stubRestorer{}
	e.SetSnapshotRestorer(restorer)

	flow := loopToCodingReady(t, e)
	coding := stageByType(flow, StageTypeCoding)
	review := stageByType(flow, StageTypeReview)
	if coding.SnapshotID != "" {
		t.Fatalf("expected empty snapshot id without a creator, got %q", coding.SnapshotID)
	}

	flow, err := e.Loop(context.Background(), flow.ID, &LoopRequest{
		FromStageID: review.ID, ToStageID: coding.ID, Reason: "x",
	})
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if restorer.calls != 0 {
		t.Fatalf("restorer must not be called for an empty snapshot id, calls=%d", restorer.calls)
	}
	if flowHasEvent(flow.Events, "flow.restored") {
		t.Fatal("empty snapshot must mean no flow.restored event")
	}
}
