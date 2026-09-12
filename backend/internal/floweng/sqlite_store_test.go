package floweng

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSQLiteEnginePersistence(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "floweng.db")
	eng, err := NewSQLiteEngine(dbPath, nil)
	if err != nil {
		t.Skipf("sqlite unavailable (need CGO): %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })

	flow, err := eng.Create(context.Background(), &CreateFlowRequest{
		ProjectID:  "proj-sql",
		TemplateID: TemplateNewProject,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Advance(context.Background(), flow.ID, &AdvanceRequest{}); err != nil {
		t.Fatal(err)
	}

	// reopen store/engine on same file
	eng2, err := NewSQLiteEngine(dbPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = eng2.Close() })
	loaded, err := eng2.Get(context.Background(), flow.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if loaded.ProjectID != "proj-sql" {
		t.Fatalf("project=%s", loaded.ProjectID)
	}
	if stageByType(loaded, StageTypeIdea).Status != StageStatusDone {
		t.Fatalf("idea status=%s", stageByType(loaded, StageTypeIdea).Status)
	}
	if stageByType(loaded, StageTypeDesign).Status != StageStatusActive {
		t.Fatalf("design status=%s", stageByType(loaded, StageTypeDesign).Status)
	}

	list, err := eng2.List(context.Background(), "proj-sql")
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%d err=%v", len(list), err)
	}
}

func TestSQLiteMemoryMode(t *testing.T) {
	store, err := NewSQLiteFlowStore(":memory:")
	if err != nil {
		t.Skipf("sqlite unavailable: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	eng := NewEngineWithStore(store, nil)
	flow, err := eng.Create(context.Background(), &CreateFlowRequest{ProjectID: "m"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := eng.Get(context.Background(), flow.ID)
	if err != nil || got.ID != flow.ID {
		t.Fatalf("get=%+v err=%v", got, err)
	}
}

func TestSQLiteCustomTemplatePersistsAgentBinding(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "floweng.db")
	id := uniqTemplateID("persisted_agent_")
	previous := GetEngine()
	t.Cleanup(func() {
		SetEngine(previous)
		_ = UnregisterTemplate(id)
	})

	eng, err := NewSQLiteEngine(dbPath, nil)
	if err != nil {
		t.Skipf("sqlite unavailable: %v", err)
	}
	SetEngine(eng)
	def := CustomTemplate{
		ID: id, Name: "Agent flow", Description: "durable template",
		Stages: []CustomStage{
			{
				Type: StageTypePlanning, Name: "规划", Canvas: "planning_board",
				AgentID: "custom-planner",
				Gates:   []CustomGate{{Phase: GatePhaseExit, Kind: GateKindHumanApproval}},
			},
			{Type: StageTypeCoding, Name: "编码", Canvas: "coding", AgentID: "custom-coder"},
		},
	}
	if err := SaveTemplate(def); err != nil {
		t.Fatalf("save template: %v", err)
	}
	if err := eng.Close(); err != nil {
		t.Fatal(err)
	}

	eng2, err := NewSQLiteEngine(dbPath, nil)
	if err != nil {
		t.Fatalf("reopen engine: %v", err)
	}
	defer eng2.Close()
	SetEngine(eng2)
	info, ok := DescribeTemplate(id)
	if !ok || info.Name != "Agent flow" || len(info.Stages) != 2 {
		t.Fatalf("persisted template mismatch: %#v", info)
	}
	if info.Stages[0].AgentID != "custom-planner" || len(info.Stages[0].Gates) != 1 {
		t.Fatalf("stage contract not persisted: %#v", info.Stages[0])
	}
	flow, err := eng2.Create(context.Background(), &CreateFlowRequest{ProjectID: "p", TemplateID: id})
	if err != nil {
		t.Fatal(err)
	}
	if flow.Stages[0].AgentID != "custom-planner" {
		t.Fatalf("flow stage agent=%q", flow.Stages[0].AgentID)
	}
}
