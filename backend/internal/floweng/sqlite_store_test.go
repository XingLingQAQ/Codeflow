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
