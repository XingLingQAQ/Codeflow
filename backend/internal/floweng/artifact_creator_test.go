package floweng

import (
	"context"
	"testing"
)

// AttachArtifactBy records the author and rejects values outside {"","agent","user"}.
func TestAttachArtifactByRecordsCreator(t *testing.T) {
	e := NewInMemoryEngine(nil)
	flow, err := e.Create(context.Background(), &CreateFlowRequest{ProjectID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	stageID := flow.Stages[0].ID

	agentArt, err := e.AttachArtifactBy(context.Background(), flow.ID, stageID, "design.md", "file:x", ArtifactCreatorAgent)
	if err != nil {
		t.Fatal(err)
	}
	if agentArt.CreatedBy != "agent" {
		t.Fatalf("createdBy=%q want agent", agentArt.CreatedBy)
	}

	userArt, err := e.AttachArtifactBy(context.Background(), flow.ID, stageID, "idea.md", "", ArtifactCreatorUser)
	if err != nil {
		t.Fatal(err)
	}
	if userArt.CreatedBy != "user" {
		t.Fatalf("createdBy=%q want user", userArt.CreatedBy)
	}

	if _, err := e.AttachArtifactBy(context.Background(), flow.ID, stageID, "x", "", "robot"); err == nil {
		t.Fatal("invalid created_by should be rejected")
	}

	got, err := e.Get(context.Background(), flow.ID)
	if err != nil {
		t.Fatal(err)
	}
	if a := findArtifact(got.Artifacts, agentArt.ID); a == nil || a.CreatedBy != "agent" {
		t.Fatalf("agent artifact not persisted with creator: %+v", a)
	}
	// The rejected attach must not have persisted anything.
	if len(got.Artifacts) != 2 {
		t.Fatalf("expected 2 persisted artifacts, got %d", len(got.Artifacts))
	}
}

// AttachArtifact (the Engine-interface method) delegates with an empty author.
func TestAttachArtifactDefaultsCreatorEmpty(t *testing.T) {
	e := NewInMemoryEngine(nil)
	flow, err := e.Create(context.Background(), &CreateFlowRequest{ProjectID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	art, err := e.AttachArtifact(context.Background(), flow.ID, flow.Stages[0].ID, "note", "")
	if err != nil {
		t.Fatal(err)
	}
	if art.CreatedBy != "" {
		t.Fatalf("default created_by should be empty, got %q", art.CreatedBy)
	}
}

func TestAttachArtifactRetriesAreIdempotentByContentRef(t *testing.T) {
	e := NewInMemoryEngine(nil)
	flow, err := e.Create(context.Background(), &CreateFlowRequest{ProjectID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	stageID := flow.Stages[0].ID

	first, err := e.AttachArtifact(context.Background(), flow.ID, stageID, "idea.md", "data:text/markdown,first")
	if err != nil {
		t.Fatal(err)
	}
	retried, err := e.AttachArtifact(context.Background(), flow.ID, stageID, "idea.md", "data:text/markdown,first")
	if err != nil {
		t.Fatal(err)
	}
	if retried.ID != first.ID || retried.Version != 1 {
		t.Fatalf("retry created a new artifact: first=%+v retried=%+v", first, retried)
	}

	changed, err := e.AttachArtifact(context.Background(), flow.ID, stageID, "idea.md", "data:text/markdown,changed")
	if err != nil {
		t.Fatal(err)
	}
	if changed.ID == first.ID || changed.Version != 2 {
		t.Fatalf("changed content should create v2: %+v", changed)
	}

	got, err := e.Get(context.Background(), flow.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Artifacts) != 2 {
		t.Fatalf("artifacts=%d want 2", len(got.Artifacts))
	}
}

func TestAdvanceApprovesCurrentStageDraftArtifacts(t *testing.T) {
	e := NewInMemoryEngine(nil)
	flow, err := e.Create(context.Background(), &CreateFlowRequest{ProjectID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := e.AttachArtifact(context.Background(), flow.ID, flow.Stages[0].ID, "idea.md", "data:text/markdown,idea")
	if err != nil {
		t.Fatal(err)
	}

	advanced, err := e.Advance(context.Background(), flow.ID, &AdvanceRequest{ExpectedStageID: flow.Stages[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	persisted := findArtifact(advanced.Artifacts, artifact.ID)
	if persisted == nil || persisted.Status != ArtifactStatusApproved {
		t.Fatalf("artifact not approved with stage: %+v", persisted)
	}
}
