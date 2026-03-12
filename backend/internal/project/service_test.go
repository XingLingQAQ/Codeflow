package project

import (
	stdcontext "context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/codeflow/backend/internal/planner"
)

func TestSQLiteProjectServicePersistenceAndProgress(t *testing.T) {
	plannerOriginal := planner.GetPlanner()
	defer planner.SetPlanner(plannerOriginal)

	plannerSvc, err := planner.NewSQLitePlanner(":memory:")
	if err != nil {
		t.Fatalf("NewSQLitePlanner failed: %v", err)
	}
	defer plannerSvc.Close()
	planner.SetPlanner(plannerSvc)

	plan1, err := plannerSvc.CreatePlan(stdcontext.Background(), &planner.PlanCreateRequest{Title: "plan-1"})
	if err != nil {
		t.Fatalf("CreatePlan plan1 failed: %v", err)
	}
	plan2, err := plannerSvc.CreatePlan(stdcontext.Background(), &planner.PlanCreateRequest{Title: "plan-2"})
	if err != nil {
		t.Fatalf("CreatePlan plan2 failed: %v", err)
	}

	plan1Task, err := plannerSvc.CreateTask(stdcontext.Background(), plan1.ID, &planner.TaskCreateRequest{Title: "p1-task"})
	if err != nil {
		t.Fatalf("CreateTask plan1 failed: %v", err)
	}
	if _, err := plannerSvc.CreateTask(stdcontext.Background(), plan2.ID, &planner.TaskCreateRequest{Title: "p2-task-a"}); err != nil {
		t.Fatalf("CreateTask plan2 task a failed: %v", err)
	}
	plan2TaskB, err := plannerSvc.CreateTask(stdcontext.Background(), plan2.ID, &planner.TaskCreateRequest{Title: "p2-task-b"})
	if err != nil {
		t.Fatalf("CreateTask plan2 task b failed: %v", err)
	}
	if _, err := plannerSvc.UpdateTask(stdcontext.Background(), plan1.ID, plan1Task.ID, &planner.TaskUpdateRequest{Status: planner.TaskStatusCompleted}); err != nil {
		t.Fatalf("UpdateTask plan1 complete failed: %v", err)
	}
	if _, err := plannerSvc.UpdateTask(stdcontext.Background(), plan2.ID, plan2TaskB.ID, &planner.TaskUpdateRequest{Status: planner.TaskStatusCompleted}); err != nil {
		t.Fatalf("UpdateTask plan2 complete failed: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "project.db")
	svc, err := NewSQLiteProjectService(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteProjectService failed: %v", err)
	}
	defer svc.Close()

	created, err := svc.CreateProject(stdcontext.Background(), &ProjectCreateRequest{
		Title:       "durable project",
		Description: "persist project state",
		Tags:        []string{"backend", "durable"},
		GitBranch:   "feature/cfo-040",
		Metadata: map[string]interface{}{
			"owner": "planner-project",
		},
	})
	if err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}

	if err := svc.AddPlanToProject(stdcontext.Background(), created.ID, plan1.ID); err != nil {
		t.Fatalf("AddPlanToProject plan1 failed: %v", err)
	}
	if err := svc.AddPlanToProject(stdcontext.Background(), created.ID, plan2.ID); err != nil {
		t.Fatalf("AddPlanToProject plan2 failed: %v", err)
	}
	if err := svc.RecalculateProgress(stdcontext.Background(), created.ID); err != nil {
		t.Fatalf("RecalculateProgress failed: %v", err)
	}

	progressProject, err := svc.GetProject(stdcontext.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetProject failed: %v", err)
	}
	if progressProject.Progress != 66 {
		t.Fatalf("expected progress 66, got %d", progressProject.Progress)
	}

	updated, err := svc.UpdateProject(stdcontext.Background(), created.ID, &ProjectUpdateRequest{
		Progress: func(v int) *int { return &v }(150),
		Status:   func(v ProjectStatus) *ProjectStatus { return &v }(StatusActive),
	})
	if err != nil {
		t.Fatalf("UpdateProject failed: %v", err)
	}
	if updated.Progress != 100 {
		t.Fatalf("expected clamped progress 100, got %d", updated.Progress)
	}

	plans, err := svc.GetProjectPlans(stdcontext.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetProjectPlans failed: %v", err)
	}
	if len(plans) != 2 || plans[0].ID != plan1.ID || plans[1].ID != plan2.ID {
		t.Fatalf("unexpected project plans: %+v", plans)
	}

	if err := svc.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	svc2, err := NewSQLiteProjectService(dbPath)
	if err != nil {
		t.Fatalf("reopen failed: %v", err)
	}
	defer svc2.Close()

	listed, err := svc2.ListProjects(stdcontext.Background(), &ProjectListRequest{})
	if err != nil {
		t.Fatalf("ListProjects failed: %v", err)
	}
	if listed.Total != 1 || len(listed.Projects) != 1 {
		t.Fatalf("expected one persisted project, got total=%d len=%d", listed.Total, len(listed.Projects))
	}
	got := listed.Projects[0]
	if got.ID != created.ID || got.Progress != 100 || got.Status != StatusActive {
		t.Fatalf("unexpected persisted project: %+v", got)
	}
	if !reflect.DeepEqual(got.Tags, []string{"backend", "durable"}) {
		t.Fatalf("unexpected persisted tags: %v", got.Tags)
	}
	if !reflect.DeepEqual(got.PlanIDs, []string{plan1.ID, plan2.ID}) {
		t.Fatalf("unexpected persisted plan ids: %v", got.PlanIDs)
	}

	filtered, err := svc2.ListProjects(stdcontext.Background(), &ProjectListRequest{Status: string(StatusActive), Tag: "backend", Search: "durable"})
	if err != nil {
		t.Fatalf("filtered ListProjects failed: %v", err)
	}
	if filtered.Total != 1 || len(filtered.Projects) != 1 {
		t.Fatalf("expected filtered project result, got total=%d len=%d", filtered.Total, len(filtered.Projects))
	}
}

func TestSQLiteProjectServiceListProjectsStableOrder(t *testing.T) {
	svc, err := NewSQLiteProjectService(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteProjectService failed: %v", err)
	}
	defer svc.Close()

	_, err = svc.db.Exec(`
		INSERT INTO projects (id, title, description, status, progress, tags_json, git_branch, created_at, updated_at, last_active, metadata_json)
		VALUES
			('a', 'older-a', '', 'planning', 0, '[]', NULL, 100, 100, 100, NULL),
			('b', 'older-b', '', 'planning', 0, '[]', NULL, 100, 100, 100, NULL),
			('c', 'newer', '', 'planning', 0, '[]', NULL, 200, 200, 200, NULL)
	`)
	if err != nil {
		t.Fatalf("seed projects failed: %v", err)
	}

	if err := svc.loadFromDBLocked(); err != nil {
		t.Fatalf("reload from db failed: %v", err)
	}

	listed, err := svc.ListProjects(stdcontext.Background(), &ProjectListRequest{})
	if err != nil {
		t.Fatalf("ListProjects failed: %v", err)
	}

	gotIDs := []string{listed.Projects[0].ID, listed.Projects[1].ID, listed.Projects[2].ID}
	wantIDs := []string{"c", "b", "a"}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("stable order mismatch: got %v want %v", gotIDs, wantIDs)
	}
}

func TestGetProjectServiceFallsBackToInMemory(t *testing.T) {
	original := defaultProjectService
	defer SetProjectService(original)

	SetProjectService(nil)
	svc := GetProjectService()
	if _, ok := svc.(*InMemoryProjectService); !ok {
		t.Fatalf("expected default in-memory project service, got %T", svc)
	}
}
