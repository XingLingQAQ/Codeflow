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

func TestInMemoryProjectPlanningLifecycle(t *testing.T) {
	svc := NewInMemoryProjectService()
	created, err := svc.CreateProject(stdcontext.Background(), &ProjectCreateRequest{Title: "planning lifecycle"})
	if err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}

	generated, err := svc.GeneratePlanDocument(stdcontext.Background(), created.ID, &PlanGenerateRequest{
		Title:   "Initial plan",
		Prompt:  "Project-first planning workflow",
		Summary: "Ship planning stage",
		Goal:    "Project-first planning",
		Steps:   []PlanStep{{ID: "s1", Title: "Model workflow"}},
		Metadata: map[string]interface{}{
			"session_id": "sess-plan",
		},
	})
	if err != nil {
		t.Fatalf("GeneratePlanDocument failed: %v", err)
	}
	if generated.Status != PlanDocumentStatusReady || generated.Revision != 1 {
		t.Fatalf("unexpected generated document: %+v", generated)
	}
	if generated.Metadata["planning_mode"] != "tool-first" {
		t.Fatalf("expected planning_mode tool-first, got %+v", generated.Metadata)
	}
	if _, ok := generated.Metadata["planning_trace"]; !ok {
		t.Fatalf("expected planning_trace metadata, got %+v", generated.Metadata)
	}
	if len(generated.Tasks) == 0 {
		t.Fatalf("expected generated tasks, got %+v", generated)
	}
	if generated.Review == nil || len(generated.Review.ReviewFocus) == 0 {
		t.Fatalf("expected generated review, got %+v", generated)
	}
	if generated.DecisionRationale == nil {
		t.Fatalf("expected decision rationale, got %+v", generated)
	}
	if generated.PlanOverview == nil || len(generated.PlanOverview.CriticalPathTaskIDs) == 0 {
		t.Fatalf("expected generated plan overview, got %+v", generated)
	}
	if generated.Summary != "Ship planning stage" {
		t.Fatalf("expected request summary override, got %+v", generated)
	}
	if generated.Goal != "Project-first planning" {
		t.Fatalf("expected request goal override, got %+v", generated)
	}
	if len(generated.Steps) != 1 || generated.Steps[0].ID != "s1" {
		t.Fatalf("expected request steps override, got %+v", generated.Steps)
	}
	if generated.Metadata["prompt"] != "Project-first planning workflow" {
		t.Fatalf("expected prompt metadata, got %+v", generated.Metadata)
	}
	if created.Status != StatusPlanning {
		t.Fatalf("expected project status planning, got %s", created.Status)
	}

	manual, err := svc.GeneratePlanDocument(stdcontext.Background(), created.ID, &PlanGenerateRequest{
		Title:   "Manual title",
		Summary: "Manual summary",
		Tasks: []PlanTask{{
			TaskID:         "custom",
			Title:          "Custom task",
			Parallelizable: true,
		}},
	})
	if err != nil {
		t.Fatalf("GeneratePlanDocument manual override failed: %v", err)
	}
	if manual.Title != "Manual title" || manual.Summary != "Manual summary" {
		t.Fatalf("expected manual overrides, got %+v", manual)
	}
	if len(manual.Tasks) != 1 || manual.Tasks[0].TaskID != "custom" {
		t.Fatalf("expected custom task override, got %+v", manual.Tasks)
	}
	if manual.Metadata["planning_mode"] != "tool-first" {
		t.Fatalf("expected planning mode on manual override, got %+v", manual.Metadata)
	}
	if created.Status != StatusPlanning {
		t.Fatalf("expected project status planning, got %s", created.Status)
	}

	// keep revised/approved lifecycle on latest stored document
	generated = manual
	if generated.Status != PlanDocumentStatusReady || generated.Revision != 1 {
		t.Fatalf("unexpected generated document: %+v", generated)
	}
	if created.Status != StatusPlanning {
		t.Fatalf("expected project status planning, got %s", created.Status)
	}

	goal := "Project planning with revision"
	revised, err := svc.RevisePlanDocument(stdcontext.Background(), created.ID, &PlanReviseRequest{
		Goal: &goal,
		ChangeRequest: &PlanChangeRequestInput{
			Summary:     "Add approval guard",
			RequestedBy: "reviewer",
		},
	})
	if err != nil {
		t.Fatalf("RevisePlanDocument failed: %v", err)
	}
	if revised.Status != PlanDocumentStatusNeedsRevision || revised.Revision != 2 {
		t.Fatalf("unexpected revised document: %+v", revised)
	}
	if len(revised.ChangeRequests) != 1 || len(revised.Revisions) != 2 {
		t.Fatalf("expected one change request and two revisions, got %+v", revised)
	}
	if revised.ChangeRequests[0].AppliedInRevision != 2 {
		t.Fatalf("expected change request applied in revision 2, got %+v", revised.ChangeRequests[0])
	}

	approved, err := svc.ApprovePlanDocument(stdcontext.Background(), created.ID, &PlanApproveRequest{ApprovedBy: "lead"})
	if err != nil {
		t.Fatalf("ApprovePlanDocument failed: %v", err)
	}
	if approved.Status != PlanDocumentStatusApproved || approved.ApprovedBy != "lead" {
		t.Fatalf("unexpected approved document: %+v", approved)
	}
	projectAfterApprove, err := svc.GetProject(stdcontext.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetProject after approve failed: %v", err)
	}
	if projectAfterApprove.Status != StatusActive {
		t.Fatalf("expected project status active after approval, got %s", projectAfterApprove.Status)
	}
}

func TestInMemoryProjectPlanningFeedbackResolutionLifecycle(t *testing.T) {
	svc := NewInMemoryProjectService()
	created, err := svc.CreateProject(stdcontext.Background(), &ProjectCreateRequest{Title: "feedback lifecycle"})
	if err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}

	generated, err := svc.GeneratePlanDocument(stdcontext.Background(), created.ID, &PlanGenerateRequest{
		Prompt: "Feedback resolution planning",
	})
	if err != nil {
		t.Fatalf("GeneratePlanDocument failed: %v", err)
	}
	if generated.Revision != 1 {
		t.Fatalf("expected initial revision 1, got %+v", generated)
	}

	feedbackText := "Need rollback and approval notes"
	revised, err := svc.RevisePlanDocument(stdcontext.Background(), created.ID, &PlanReviseRequest{
		Feedback: feedbackText,
	})
	if err != nil {
		t.Fatalf("RevisePlanDocument feedback failed: %v", err)
	}
	if revised.Status != PlanDocumentStatusNeedsRevision || revised.Revision != 2 {
		t.Fatalf("unexpected revised document after feedback: %+v", revised)
	}
	if len(revised.ChangeRequests) != 1 {
		t.Fatalf("expected one synthesized change request, got %+v", revised.ChangeRequests)
	}
	if len(revised.Feedback) != 1 || revised.Feedback[0].Message != feedbackText {
		t.Fatalf("expected feedback item recorded, got %+v", revised.Feedback)
	}
	if revised.ChangeRequests[0].Status != "applied" {
		t.Fatalf("expected new change request applied in current revision, got %+v", revised.ChangeRequests[0])
	}
	if revised.ChangeRequests[0].AppliedInRevision != 2 {
		t.Fatalf("expected applied_in_revision 2, got %+v", revised.ChangeRequests[0])
	}
	if revised.BasedOnRevision != 1 {
		t.Fatalf("expected based_on_revision 1, got %+v", revised)
	}

	resolved, err := svc.RevisePlanDocument(stdcontext.Background(), created.ID, &PlanReviseRequest{
		FeedbackResolution: []PlanFeedbackResolution{{
			ChangeRequestID: revised.ChangeRequests[0].ID,
			Decision:        "resolved",
			Reason:          "Covered in revision plan",
		}},
	})
	if err != nil {
		t.Fatalf("RevisePlanDocument resolution failed: %v", err)
	}
	if resolved.Revision != 3 {
		t.Fatalf("expected revision 3 after resolution, got %+v", resolved)
	}
	if len(resolved.FeedbackLoop.FeedbackResolution) != 1 {
		t.Fatalf("expected one feedback resolution, got %+v", resolved.FeedbackLoop)
	}
	if resolved.FeedbackLoop.FeedbackResolution[0].Decision != "resolved" {
		t.Fatalf("expected resolution decision recorded, got %+v", resolved.FeedbackLoop.FeedbackResolution[0])
	}
	if resolved.ChangeRequests[0].Status != "resolved" {
		t.Fatalf("expected change request resolved, got %+v", resolved.ChangeRequests[0])
	}
	if resolved.ChangeRequests[0].AppliedInRevision != 3 {
		t.Fatalf("expected applied_in_revision 3, got %+v", resolved.ChangeRequests[0])
	}
	if resolved.BasedOnRevision != 2 {
		t.Fatalf("expected based_on_revision 2, got %+v", resolved)
	}
}

func TestInMemoryProjectPlanningGeneratorInjection(t *testing.T) {
	svc := NewInMemoryProjectService()
	created, err := svc.CreateProject(stdcontext.Background(), &ProjectCreateRequest{Title: "planning injection"})
	if err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}

	svc.SetPlanningGenerator(planningGeneratorFunc(func(ctx stdcontext.Context, input PlanningGenerationInput) (*PlanGenerateRequest, *PlanningTrace, error) {
		return &PlanGenerateRequest{
			Title:   "Injected title",
			Summary: "Injected summary",
			Tasks: []PlanTask{{
				TaskID:         "inject",
				Title:          "Injected task",
				Parallelizable: true,
			}},
		}, &PlanningTrace{Mode: "native-tool-call", StartedAt: 1, FinishedAt: 2}, nil
	}))

	generated, err := svc.GeneratePlanDocument(stdcontext.Background(), created.ID, &PlanGenerateRequest{Prompt: "Use injected generator"})
	if err != nil {
		t.Fatalf("GeneratePlanDocument failed: %v", err)
	}
	if generated.Title != "Injected title" || generated.Summary != "Injected summary" {
		t.Fatalf("expected injected generator result, got %+v", generated)
	}
	if generated.Metadata["planning_mode"] != "tool-first" {
		t.Fatalf("expected compatibility planning_mode, got %+v", generated.Metadata)
	}
	if generated.Metadata["planning_executor"] != "native-tool-call" {
		t.Fatalf("expected planning_executor native-tool-call, got %+v", generated.Metadata)
	}
	if _, ok := generated.Metadata["planning_trace"]; !ok {
		t.Fatalf("expected planning_trace metadata, got %+v", generated.Metadata)
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

type planningGeneratorFunc func(ctx stdcontext.Context, input PlanningGenerationInput) (*PlanGenerateRequest, *PlanningTrace, error)

func (f planningGeneratorFunc) Generate(ctx stdcontext.Context, input PlanningGenerationInput) (*PlanGenerateRequest, *PlanningTrace, error) {
	return f(ctx, input)
}

