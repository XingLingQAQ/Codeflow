package debate

import (
	"context"
	"errors"
	"testing"
)

func TestCreateAndListByFlowStage(t *testing.T) {
	m := NewInMemoryDebateManager()
	ctx := context.Background()
	d, err := m.CreateDebate(ctx, &DebateCreateRequest{
		Title:        "design debate",
		GeneratorID:  "g1",
		CriticID:     "c1",
		InitialInput: "should we use X?",
		FlowID:       "flow-1",
		StageID:      "stage-design",
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.FlowID != "flow-1" || d.StageID != "stage-design" {
		t.Fatalf("fk missing: %+v", d)
	}
	list, err := m.ListDebates(ctx, &DebateListRequest{FlowID: "flow-1", StageID: "stage-design"})
	if err != nil || list.Total != 1 {
		t.Fatalf("list total=%d err=%v", list.Total, err)
	}
	list2, _ := m.ListDebates(ctx, &DebateListRequest{FlowID: "other"})
	if list2.Total != 0 {
		t.Fatalf("expected empty for other flow")
	}
}

func TestGetDebateReturnsClone(t *testing.T) {
	m := NewInMemoryDebateManager()
	ctx := context.Background()
	created, err := m.CreateDebate(ctx, &DebateCreateRequest{
		Title: "t", GeneratorID: "g", CriticID: "c", InitialInput: "i",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := m.GetDebate(ctx, created.ID)
	if err != nil || got == nil {
		t.Fatal(err)
	}
	got.Title = "mutated"
	again, _ := m.GetDebate(ctx, created.ID)
	if again.Title == "mutated" {
		t.Fatal("GetDebate must return a clone")
	}
}

func TestNextRoundNotFound(t *testing.T) {
	m := NewInMemoryDebateManager()
	_, err := m.NextRound(context.Background(), "missing", &NextRoundRequest{
		GeneratorOutput: "g", CriticFeedback: "c",
	})
	if err == nil || !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
