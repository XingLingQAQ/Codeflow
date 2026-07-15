package floweng

import (
	"context"
	"testing"

	"github.com/codeflow/backend/internal/snapshot"
)

func TestCreateNewProjectFlow(t *testing.T) {
	e := NewInMemoryEngine(nil)
	flow, err := e.Create(context.Background(), &CreateFlowRequest{
		ProjectID:  "proj-1",
		TemplateID: TemplateNewProject,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if flow.Status != FlowStatusActive {
		t.Fatalf("status=%s", flow.Status)
	}
	if len(flow.Stages) != 7 {
		t.Fatalf("stages=%d want 7", len(flow.Stages))
	}
	if flow.Stages[0].Status != StageStatusActive {
		t.Fatalf("first stage should be active, got %s", flow.Stages[0].Status)
	}
	if flow.Stages[0].Type != StageTypeIdea {
		t.Fatalf("first type=%s", flow.Stages[0].Type)
	}
	// research optional
	foundOptional := false
	for _, s := range flow.Stages {
		if s.Type == StageTypeResearch && s.Optional {
			foundOptional = true
		}
	}
	if !foundOptional {
		t.Fatal("expected optional research stage")
	}
}

func TestAdvanceThroughAllAndComplete(t *testing.T) {
	e := NewInMemoryEngine(nil)
	flow, err := e.Create(context.Background(), &CreateFlowRequest{ProjectID: "p", TemplateID: TemplateNewProject})
	if err != nil {
		t.Fatal(err)
	}
	// skip research when we reach it
	for flow.Status == FlowStatusActive {
		active := activeStage(flow)
		if active == nil {
			t.Fatal("no active")
		}
		if active.Optional {
			flow, err = e.Skip(context.Background(), flow.ID, &SkipRequest{StageID: active.ID})
			if err != nil {
				t.Fatalf("skip: %v", err)
			}
			continue
		}
		flow, err = e.Advance(context.Background(), flow.ID, &AdvanceRequest{})
		if err != nil {
			t.Fatalf("advance %s: %v", active.Type, err)
		}
	}
	if flow.Status != FlowStatusCompleted {
		t.Fatalf("want completed, got %s", flow.Status)
	}
	for _, s := range flow.Stages {
		if s.Optional {
			if s.Status != StageStatusSkipped && s.Status != StageStatusDone {
				t.Fatalf("optional stage %s status=%s", s.Type, s.Status)
			}
			continue
		}
		if s.Status != StageStatusDone {
			t.Fatalf("stage %s status=%s want done", s.Type, s.Status)
		}
	}
}

func TestSkipNonOptionalFails(t *testing.T) {
	e := NewInMemoryEngine(nil)
	flow, _ := e.Create(context.Background(), &CreateFlowRequest{ProjectID: "p"})
	_, err := e.Skip(context.Background(), flow.ID, &SkipRequest{StageID: flow.Stages[0].ID})
	if err == nil {
		t.Fatal("expected error skipping non-optional")
	}
}

func TestLoopReviewToCoding(t *testing.T) {
	e := NewInMemoryEngine(nil)
	flow, _ := e.Create(context.Background(), &CreateFlowRequest{ProjectID: "p"})

	// advance to coding (skip research)
	for {
		active := activeStage(flow)
		if active.Type == StageTypeCoding {
			break
		}
		if active.Optional {
			flow, _ = e.Skip(context.Background(), flow.ID, &SkipRequest{StageID: active.ID})
			continue
		}
		flow, _ = e.Advance(context.Background(), flow.ID, &AdvanceRequest{})
	}
	// finish coding + activate review
	flow, err := e.Advance(context.Background(), flow.ID, &AdvanceRequest{})
	if err != nil {
		t.Fatal(err)
	}
	review := activeStage(flow)
	if review.Type != StageTypeReview {
		t.Fatalf("want review active, got %s", review.Type)
	}
	coding := stageByType(flow, StageTypeCoding)
	if coding == nil || coding.Status != StageStatusDone {
		t.Fatal("coding should be done before loop")
	}

	// attach artifact on coding then loop back
	if _, err := e.AttachArtifact(flow.ID, coding.ID, "changeset"); err != nil {
		t.Fatal(err)
	}

	flow, err = e.Loop(context.Background(), flow.ID, &LoopRequest{
		FromStageID: review.ID,
		ToStageID:   coding.ID,
		Reason:      "tests failed",
	})
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	active := activeStage(flow)
	if active == nil || active.Type != StageTypeCoding {
		t.Fatalf("after loop active=%v", active)
	}
	// review should be pending again
	rev := stageByType(flow, StageTypeReview)
	if rev.Status != StageStatusPending {
		t.Fatalf("review status=%s want pending", rev.Status)
	}
	// artifact stale
	got, _ := e.Get(context.Background(), flow.ID)
	if len(got.Artifacts) != 1 || got.Artifacts[0].Status != ArtifactStatusStale {
		t.Fatalf("artifact stale expected, got %+v", got.Artifacts)
	}
}

func TestLoopDisallowed(t *testing.T) {
	e := NewInMemoryEngine(nil)
	flow, _ := e.Create(context.Background(), &CreateFlowRequest{ProjectID: "p"})
	// advance idea -> design
	flow, _ = e.Advance(context.Background(), flow.ID, &AdvanceRequest{})
	design := activeStage(flow)
	idea := stageByType(flow, StageTypeIdea)
	// design → idea not in loops
	_, err := e.Loop(context.Background(), flow.ID, &LoopRequest{
		FromStageID: design.ID,
		ToStageID:   idea.ID,
	})
	if err == nil {
		t.Fatal("expected disallowed loop error")
	}
}

func TestAdvanceCreatesSnapshot(t *testing.T) {
	snapSvc := snapshot.NewInMemorySnapshotService()
	hook := NewDefaultSnapshotHook(snapSvc)
	e := NewInMemoryEngine(hook)
	flow, _ := e.Create(context.Background(), &CreateFlowRequest{ProjectID: "p", SessionID: "s1"})
	flow, err := e.Advance(context.Background(), flow.ID, &AdvanceRequest{SessionID: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	done := stageByType(flow, StageTypeIdea)
	if done.SnapshotID == "" {
		t.Fatal("expected snapshot_id on completed stage")
	}
	if _, err := snapSvc.Get(context.Background(), done.SnapshotID); err != nil {
		t.Fatalf("snapshot missing: %v", err)
	}
}

func TestUnknownTemplate(t *testing.T) {
	e := NewInMemoryEngine(nil)
	_, err := e.Create(context.Background(), &CreateFlowRequest{
		ProjectID:  "p",
		TemplateID: "nope",
	})
	if err == nil {
		t.Fatal("expected unknown template error")
	}
}

func TestGetSetEngine(t *testing.T) {
	prev := GetEngine()
	t.Cleanup(func() { SetEngine(prev) })
	custom := NewInMemoryEngine(nil)
	SetEngine(custom)
	if GetEngine() != custom {
		t.Fatal("GetEngine should return custom")
	}
	SetEngine(nil)
	if GetEngine() == nil {
		t.Fatal("GetEngine should lazy-create")
	}
}

func activeStage(f *Flow) *Stage {
	for i := range f.Stages {
		if f.Stages[i].Status == StageStatusActive {
			return &f.Stages[i]
		}
	}
	return nil
}

func stageByType(f *Flow, typ StageType) *Stage {
	for i := range f.Stages {
		if f.Stages[i].Type == typ {
			return &f.Stages[i]
		}
	}
	return nil
}
