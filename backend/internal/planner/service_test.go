package planner

import (
	stdcontext "context"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSQLitePlannerPersistenceAndOrdering(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "planner.db")

	svc, err := NewSQLitePlanner(dbPath)
	if err != nil {
		t.Fatalf("NewSQLitePlanner failed: %v", err)
	}
	defer svc.Close()

	plan, err := svc.CreatePlan(stdcontext.Background(), &PlanCreateRequest{
		Title:       "planner durable",
		Description: "persist planner state",
		Metadata: map[string]interface{}{
			"owner": "cfo-040",
		},
	})
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}

	first, err := svc.CreateTask(stdcontext.Background(), plan.ID, &TaskCreateRequest{Title: "first"})
	if err != nil {
		t.Fatalf("CreateTask first failed: %v", err)
	}
	blocked, err := svc.CreateTask(stdcontext.Background(), plan.ID, &TaskCreateRequest{
		Title:        "blocked",
		Dependencies: []string{first.ID},
	})
	if err != nil {
		t.Fatalf("CreateTask blocked failed: %v", err)
	}
	if blocked.Status != TaskStatusBlocked {
		t.Fatalf("expected blocked task status, got %s", blocked.Status)
	}

	updatedFirst, err := svc.UpdateTask(stdcontext.Background(), plan.ID, first.ID, &TaskUpdateRequest{Status: TaskStatusCompleted})
	if err != nil {
		t.Fatalf("UpdateTask complete failed: %v", err)
	}
	if updatedFirst.CompletedAt == 0 {
		t.Fatalf("expected completed_at to be set")
	}

	blockedAfter, err := svc.GetTask(stdcontext.Background(), plan.ID, blocked.ID)
	if err != nil {
		t.Fatalf("GetTask blocked failed: %v", err)
	}
	if blockedAfter.Status != TaskStatusPending {
		t.Fatalf("expected blocked task to unblock, got %s", blockedAfter.Status)
	}

	reordered, err := svc.ReorderTask(stdcontext.Background(), plan.ID, blocked.ID, &TaskReorderRequest{NewOrder: 1})
	if err != nil {
		t.Fatalf("ReorderTask failed: %v", err)
	}
	if reordered.Order != 1 {
		t.Fatalf("expected reordered task order=1, got %d", reordered.Order)
	}

	batchResp, err := svc.BatchUpdateModel(stdcontext.Background(), plan.ID, &BatchModelRequest{
		TaskIDs: []string{first.ID, blocked.ID, "missing"},
		Model:   "claude-sonnet-4-6",
	})
	if err != nil {
		t.Fatalf("BatchUpdateModel failed: %v", err)
	}
	if batchResp.Updated != 2 || !reflect.DeepEqual(batchResp.Failed, []string{"missing"}) {
		t.Fatalf("unexpected batch response: %+v", batchResp)
	}

	if err := svc.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	svc2, err := NewSQLitePlanner(dbPath)
	if err != nil {
		t.Fatalf("reopen failed: %v", err)
	}
	defer svc2.Close()

	plans, err := svc2.ListPlans(stdcontext.Background(), &PlanListRequest{})
	if err != nil {
		t.Fatalf("ListPlans failed: %v", err)
	}
	if plans.Total != 1 || len(plans.Plans) != 1 {
		t.Fatalf("expected one persisted plan, got total=%d len=%d", plans.Total, len(plans.Plans))
	}
	gotPlan := plans.Plans[0]
	if gotPlan.ID != plan.ID || gotPlan.TaskCount != 2 || gotPlan.CompletedCount != 1 {
		t.Fatalf("unexpected persisted plan: %+v", gotPlan)
	}

	tasks, err := svc2.ListTasks(stdcontext.Background(), plan.ID, &TaskListRequest{})
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}
	if tasks.Total != 2 || len(tasks.Tasks) != 2 {
		t.Fatalf("expected two persisted tasks, got total=%d len=%d", tasks.Total, len(tasks.Tasks))
	}
	if tasks.Tasks[0].ID != blocked.ID || tasks.Tasks[0].Order != 1 {
		t.Fatalf("expected blocked task first after reorder, got %+v", tasks.Tasks[0])
	}
	if tasks.Tasks[0].Model != "claude-sonnet-4-6" || tasks.Tasks[1].Model != "claude-sonnet-4-6" {
		t.Fatalf("expected persisted model updates, got %+v %+v", tasks.Tasks[0], tasks.Tasks[1])
	}
	if tasks.Tasks[1].ID != first.ID || tasks.Tasks[1].CompletedAt == 0 || tasks.Tasks[1].Status != TaskStatusCompleted {
		t.Fatalf("expected completed task second, got %+v", tasks.Tasks[1])
	}
}

func TestSQLitePlannerListPlansStableOrder(t *testing.T) {
	svc, err := NewSQLitePlanner(":memory:")
	if err != nil {
		t.Fatalf("NewSQLitePlanner failed: %v", err)
	}
	defer svc.Close()

	_, err = svc.db.Exec(`
		INSERT INTO plans (id, title, description, status, task_count, completed_count, created_at, updated_at, metadata_json)
		VALUES
			('a', 'older-a', '', 'draft', 0, 0, 100, 100, NULL),
			('b', 'older-b', '', 'draft', 0, 0, 100, 100, NULL),
			('c', 'newer', '', 'draft', 0, 0, 200, 200, NULL)
	`)
	if err != nil {
		t.Fatalf("seed plans failed: %v", err)
	}

	if err := svc.loadFromDBLocked(); err != nil {
		t.Fatalf("reload from db failed: %v", err)
	}

	listed, err := svc.ListPlans(stdcontext.Background(), &PlanListRequest{})
	if err != nil {
		t.Fatalf("ListPlans failed: %v", err)
	}

	gotIDs := []string{listed.Plans[0].ID, listed.Plans[1].ID, listed.Plans[2].ID}
	wantIDs := []string{"c", "b", "a"}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("stable order mismatch: got %v want %v", gotIDs, wantIDs)
	}
}

func TestGetPlannerFallsBackToInMemory(t *testing.T) {
	original := defaultPlanner
	defer SetPlanner(original)

	SetPlanner(nil)
	svc := GetPlanner()
	if _, ok := svc.(*InMemoryPlanner); !ok {
		t.Fatalf("expected default in-memory planner, got %T", svc)
	}
}
