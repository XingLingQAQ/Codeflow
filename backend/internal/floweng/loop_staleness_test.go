package floweng

import (
	"context"
	"testing"
)

// Loop marks artifacts on the loop target and every downstream stage stale
// (artStageIdx >= toIdx), while leaving strictly-upstream artifacts untouched.
// This pins the intended boundary semantics of the staleness sweep (design §4).
func TestLoopMarksTargetAndDownstreamStaleNotUpstream(t *testing.T) {
	e := NewInMemoryEngine(nil)
	flow, err := e.Create(context.Background(), &CreateFlowRequest{ProjectID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	flow = advanceToStage(t, e, flow, StageTypeCoding)
	flow, err = e.Advance(context.Background(), flow.ID, &AdvanceRequest{}) // coding done -> review active
	if err != nil {
		t.Fatal(err)
	}

	design := stageByType(flow, StageTypeDesign) // strictly upstream of coding (loop target)
	coding := stageByType(flow, StageTypeCoding) // the loop target itself
	review := stageByType(flow, StageTypeReview) // downstream (loop source)

	upstreamArt, err := e.AttachArtifact(context.Background(), flow.ID, design.ID, "design.md", "")
	if err != nil {
		t.Fatal(err)
	}
	targetArt, err := e.AttachArtifact(context.Background(), flow.ID, coding.ID, "changeset", "git:worktree")
	if err != nil {
		t.Fatal(err)
	}

	flow, err = e.Loop(context.Background(), flow.ID, &LoopRequest{
		FromStageID: review.ID, ToStageID: coding.ID, Reason: "tests failed",
	})
	if err != nil {
		t.Fatalf("loop: %v", err)
	}

	got, err := e.Get(context.Background(), flow.ID)
	if err != nil {
		t.Fatal(err)
	}
	target := findArtifact(got.Artifacts, targetArt.ID)
	upstream := findArtifact(got.Artifacts, upstreamArt.ID)
	if target == nil || upstream == nil {
		t.Fatalf("artifacts missing after loop: target=%v upstream=%v", target, upstream)
	}
	if target.Status != ArtifactStatusStale {
		t.Fatalf("loop-target artifact should be stale (>= toIdx), got %s", target.Status)
	}
	if upstream.Status == ArtifactStatusStale {
		t.Fatalf("strictly-upstream artifact should NOT be stale, got %s", upstream.Status)
	}
}
