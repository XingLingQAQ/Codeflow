package floweng

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// flowHasEvent reports whether any event of the given type is present.
func flowHasEvent(events []FlowEvent, typ string) bool {
	return findFlowEvent(events, typ) != nil
}

// findFlowEvent returns the first event of the given type, or nil.
func findFlowEvent(events []FlowEvent, typ string) *FlowEvent {
	for i := range events {
		if events[i].Type == typ {
			return &events[i]
		}
	}
	return nil
}

// flowEventTypes lists event types (handy in failure messages).
func flowEventTypes(events []FlowEvent) []string {
	out := make([]string, 0, len(events))
	for _, ev := range events {
		out = append(out, ev.Type)
	}
	return out
}

// findArtifact returns the artifact with the given id, or nil.
func findArtifact(arts []Artifact, id string) *Artifact {
	for i := range arts {
		if arts[i].ID == id {
			return &arts[i]
		}
	}
	return nil
}

// containsTemplateID reports whether ids includes want.
func containsTemplateID(ids []TemplateID, want TemplateID) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// uniqTemplateID builds a collision-free custom template id for tests.
func uniqTemplateID(prefix string) TemplateID {
	return TemplateID(prefix + uuid.NewString())
}

// advanceToStage drives the flow (skipping optional stages) until target is the
// active stage, returning the updated flow.
func advanceToStage(t *testing.T, e *InMemoryEngine, flow *Flow, target StageType) *Flow {
	t.Helper()
	for i := 0; i < 50; i++ {
		active := activeStage(flow)
		if active == nil {
			t.Fatalf("no active stage while seeking %s", target)
		}
		if active.Type == target {
			return flow
		}
		var err error
		if active.Optional {
			flow, err = e.Skip(context.Background(), flow.ID, &SkipRequest{StageID: active.ID})
		} else {
			flow, err = e.Advance(context.Background(), flow.ID, &AdvanceRequest{})
		}
		if err != nil {
			t.Fatalf("seek %s: %v", target, err)
		}
	}
	t.Fatalf("did not reach stage %s within bound", target)
	return nil
}

// assertFlowUnchanged fails if any observable state differs between before and
// after. Used to prove a rejected transition performed no mutation.
func assertFlowUnchanged(t *testing.T, before, after *Flow) {
	t.Helper()
	if !before.UpdatedAt.Equal(after.UpdatedAt) {
		t.Fatalf("UpdatedAt changed: %v -> %v", before.UpdatedAt, after.UpdatedAt)
	}
	if before.Status != after.Status {
		t.Fatalf("flow status changed: %s -> %s", before.Status, after.Status)
	}
	if len(before.Events) != len(after.Events) {
		t.Fatalf("event count changed: %d -> %d", len(before.Events), len(after.Events))
	}
	if len(before.Stages) != len(after.Stages) {
		t.Fatalf("stage count changed: %d -> %d", len(before.Stages), len(after.Stages))
	}
	for i := range before.Stages {
		if before.Stages[i].Status != after.Stages[i].Status {
			t.Fatalf("stage %d (%s) status changed: %s -> %s",
				i, before.Stages[i].Type, before.Stages[i].Status, after.Stages[i].Status)
		}
	}
	if len(before.Artifacts) != len(after.Artifacts) {
		t.Fatalf("artifact count changed: %d -> %d", len(before.Artifacts), len(after.Artifacts))
	}
}
