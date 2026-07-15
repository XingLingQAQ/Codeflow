package workflow

import (
	"context"
	"testing"

	"github.com/codeflow/backend/internal/floweng"
)

func TestCollectFlowengTimelineEvents(t *testing.T) {
	prev := floweng.GetEngine()
	eng := floweng.NewInMemoryEngine(nil)
	floweng.SetEngine(eng)
	t.Cleanup(func() { floweng.SetEngine(prev) })

	flow, err := eng.Create(context.Background(), &floweng.CreateFlowRequest{
		ProjectID:  "proj-bridge",
		TemplateID: floweng.TemplateNewProject,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Advance(context.Background(), flow.ID, &floweng.AdvanceRequest{}); err != nil {
		t.Fatal(err)
	}

	events := collectFlowengTimelineEvents(context.Background(), "proj-bridge")
	if len(events) < 2 {
		t.Fatalf("expected floweng events, got %d", len(events))
	}
	for _, e := range events {
		if e.Source != "floweng" || e.Lane != "floweng" {
			t.Fatalf("bad event: %+v", e)
		}
		if e.ProjectID != "proj-bridge" {
			t.Fatalf("project id: %+v", e)
		}
	}
	// other project empty
	if got := collectFlowengTimelineEvents(context.Background(), "other"); len(got) != 0 {
		t.Fatalf("expected empty for other project, got %d", len(got))
	}
}
