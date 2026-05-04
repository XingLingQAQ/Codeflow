package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/codeflow/backend/internal/audit"
	"github.com/codeflow/backend/internal/planner"
	"github.com/codeflow/backend/internal/project"
)

func setupProjectsRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	v1 := router.Group("/api/v1")
	{
		projects := v1.Group("/projects")
		{
			projects.GET("", GetProjects)
			projects.POST("", CreateProject)
			projects.GET("/:id", GetProject)
			projects.PUT("/:id", UpdateProject)
			projects.DELETE("/:id", DeleteProject)
			projects.GET("/:id/plans", GetProjectPlans)
			projects.POST("/:id/plans", AddPlanToProject)
			projects.DELETE("/:id/plans/:planId", RemovePlanFromProject)
			projects.POST("/:id/plan", GenerateProjectPlan)
			projects.GET("/:id/plan", GetProjectPlan)
			projects.POST("/:id/plan/revise", ReviseProjectPlan)
			projects.POST("/:id/plan/approve", ApproveProjectPlan)
			projects.POST("/:id/plan/execute", ExecuteProjectPlan)
		}
	}

	return router
}

func TestGenerateProjectPlanAllowsPromptOnlyInput(t *testing.T) {
	router := setupProjectsRouter()
	projectSvc, err := project.NewSQLiteProjectService(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteProjectService failed: %v", err)
	}
	plannerSvc, err := planner.NewSQLitePlanner(":memory:")
	if err != nil {
		t.Fatalf("NewSQLitePlanner failed: %v", err)
	}
	project.SetProjectService(projectSvc)
	planner.SetPlanner(plannerSvc)
	defer func() {
		project.SetProjectService(nil)
		planner.SetPlanner(nil)
		_ = projectSvc.Close()
		_ = plannerSvc.Close()
	}()

	projectBody := map[string]any{"title": "prompt-only project"}
	body, _ := json.Marshal(projectBody)
	createProjectReq, _ := http.NewRequest("POST", "/api/v1/projects", bytes.NewBuffer(body))
	createProjectReq.Header.Set("Content-Type", "application/json")
	createProjectResp := httptest.NewRecorder()
	router.ServeHTTP(createProjectResp, createProjectReq)
	assert.Equal(t, http.StatusCreated, createProjectResp.Code)
	createdProject := decodeContextResponseData[project.Project](t, createProjectResp.Body.Bytes())

	generatePlanBody := map[string]any{
		"prompt": "Generate a project-scoped plan without an explicit title",
	}
	body, _ = json.Marshal(generatePlanBody)
	generatePlanReq, _ := http.NewRequest("POST", "/api/v1/projects/"+createdProject.ID+"/plan", bytes.NewBuffer(body))
	generatePlanReq.Header.Set("Content-Type", "application/json")
	generatePlanResp := httptest.NewRecorder()
	router.ServeHTTP(generatePlanResp, generatePlanReq)

	assert.Equal(t, http.StatusCreated, generatePlanResp.Code)
	generatedPlan := decodeContextResponseData[project.PlanDocument](t, generatePlanResp.Body.Bytes())
	assert.Equal(t, project.PlanDocumentStatusReady, generatedPlan.Status)
	assert.NotEmpty(t, generatedPlan.Title)
	assert.NotEmpty(t, generatedPlan.Tasks)
	assert.Equal(t, "Generate a project-scoped plan without an explicit title", generatedPlan.Metadata["prompt"])
}

func TestProjectsLifecycleAPIWithSQLite(t *testing.T) {
	router := setupProjectsRouter()
	projectSvc, err := project.NewSQLiteProjectService(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteProjectService failed: %v", err)
	}
	plannerSvc, err := planner.NewSQLitePlanner(":memory:")
	if err != nil {
		t.Fatalf("NewSQLitePlanner failed: %v", err)
	}
	project.SetProjectService(projectSvc)
	planner.SetPlanner(plannerSvc)
	auditStorage := audit.NewMemoryStorage()
	auditSvc := audit.NewAuditService(auditStorage)
	audit.SetAuditService(auditSvc)
	defer func() {
		project.SetProjectService(nil)
		planner.SetPlanner(nil)
		audit.SetAuditService(nil)
		_ = projectSvc.Close()
		_ = plannerSvc.Close()
		_ = auditSvc.Close()
	}()

	plan, err := plannerSvc.CreatePlan(context.Background(), &planner.PlanCreateRequest{Title: "plan-for-project"})
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}

	projectBody := map[string]any{"title": "durable project", "tags": []string{"backend"}}
	body, _ := json.Marshal(projectBody)
	createProjectReq, _ := http.NewRequest("POST", "/api/v1/projects", bytes.NewBuffer(body))
	createProjectReq.Header.Set("Content-Type", "application/json")
	createProjectResp := httptest.NewRecorder()
	router.ServeHTTP(createProjectResp, createProjectReq)

	assert.Equal(t, http.StatusCreated, createProjectResp.Code)
	createdProject := decodeContextResponseData[project.Project](t, createProjectResp.Body.Bytes())
	assert.NotEmpty(t, createdProject.ID)
	assert.Equal(t, project.StatusPlanning, createdProject.Status)

	generatePlanBody := map[string]any{
		"title":   "Project planning stage",
		"summary": "Bootstrap planning in workflow",
		"prompt":  "Create a project-scoped planning document for workflow execution",
		"steps": []map[string]any{{
			"id":    "align",
			"title": "Align workflow stages",
		}},
	}
	body, _ = json.Marshal(generatePlanBody)
	generatePlanReq, _ := http.NewRequest("POST", "/api/v1/projects/"+createdProject.ID+"/plan", bytes.NewBuffer(body))
	generatePlanReq.Header.Set("Content-Type", "application/json")
	generatePlanResp := httptest.NewRecorder()
	router.ServeHTTP(generatePlanResp, generatePlanReq)
	assert.Equal(t, http.StatusCreated, generatePlanResp.Code)
	generatedPlan := decodeContextResponseData[project.PlanDocument](t, generatePlanResp.Body.Bytes())
	assert.Equal(t, project.PlanDocumentStatusReady, generatedPlan.Status)
	assert.Equal(t, 1, generatedPlan.Revision)
	assert.NotEmpty(t, generatedPlan.Tasks)
	assert.Equal(t, "tool-first", generatedPlan.Metadata["planning_mode"])
	assert.Contains(t, generatedPlan.Metadata, "planning_trace")
	assert.Equal(t, "Create a project-scoped planning document for workflow execution", generatedPlan.Metadata["prompt"])

	getProjectPlanReq, _ := http.NewRequest("GET", "/api/v1/projects/"+createdProject.ID+"/plan", nil)
	getProjectPlanResp := httptest.NewRecorder()
	router.ServeHTTP(getProjectPlanResp, getProjectPlanReq)
	assert.Equal(t, http.StatusOK, getProjectPlanResp.Code)
	fetchedPlan := decodeContextResponseData[project.PlanDocument](t, getProjectPlanResp.Body.Bytes())
	assert.Equal(t, generatedPlan.Title, fetchedPlan.Title)

	revisePlanBody := map[string]any{
		"change_request": map[string]any{
			"summary":      "Need stricter approval",
			"requested_by": "qa",
		},
	}
	body, _ = json.Marshal(revisePlanBody)
	revisePlanReq, _ := http.NewRequest("POST", "/api/v1/projects/"+createdProject.ID+"/plan/revise", bytes.NewBuffer(body))
	revisePlanReq.Header.Set("Content-Type", "application/json")
	revisePlanResp := httptest.NewRecorder()
	router.ServeHTTP(revisePlanResp, revisePlanReq)
	assert.Equal(t, http.StatusOK, revisePlanResp.Code)
	revisedPlan := decodeContextResponseData[project.PlanDocument](t, revisePlanResp.Body.Bytes())
	assert.Equal(t, project.PlanDocumentStatusNeedsRevision, revisedPlan.Status)
	assert.Equal(t, 2, revisedPlan.Revision)
	assert.Len(t, revisedPlan.ChangeRequests, 1)

	feedbackOnlyBody := map[string]any{
		"feedback": "Need rollout checklist and rollback notes",
	}
	body, _ = json.Marshal(feedbackOnlyBody)
	feedbackOnlyReq, _ := http.NewRequest("POST", "/api/v1/projects/"+createdProject.ID+"/plan/revise", bytes.NewBuffer(body))
	feedbackOnlyReq.Header.Set("Content-Type", "application/json")
	feedbackOnlyResp := httptest.NewRecorder()
	router.ServeHTTP(feedbackOnlyResp, feedbackOnlyReq)
	assert.Equal(t, http.StatusOK, feedbackOnlyResp.Code)
	feedbackOnlyPlan := decodeContextResponseData[project.PlanDocument](t, feedbackOnlyResp.Body.Bytes())
	assert.Equal(t, project.PlanDocumentStatusNeedsRevision, feedbackOnlyPlan.Status)
	assert.Len(t, feedbackOnlyPlan.ChangeRequests, 2)
	assert.Len(t, feedbackOnlyPlan.Feedback, 2)
	assert.Equal(t, "Need rollout checklist and rollback notes", feedbackOnlyPlan.Feedback[len(feedbackOnlyPlan.Feedback)-1].Message)
	assert.Equal(t, "applied", feedbackOnlyPlan.ChangeRequests[len(feedbackOnlyPlan.ChangeRequests)-1].Status)
	assert.Equal(t, feedbackOnlyPlan.Revision, feedbackOnlyPlan.ChangeRequests[len(feedbackOnlyPlan.ChangeRequests)-1].AppliedInRevision)

	resolutionBody := map[string]any{
		"feedback_resolution": []map[string]any{{
			"change_request_id": feedbackOnlyPlan.ChangeRequests[len(feedbackOnlyPlan.ChangeRequests)-1].ID,
			"decision":          "resolved",
			"reason":            "Checklist added to revision notes",
		}},
	}
	body, _ = json.Marshal(resolutionBody)
	resolutionReq, _ := http.NewRequest("POST", "/api/v1/projects/"+createdProject.ID+"/plan/revise", bytes.NewBuffer(body))
	resolutionReq.Header.Set("Content-Type", "application/json")
	resolutionResp := httptest.NewRecorder()
	router.ServeHTTP(resolutionResp, resolutionReq)
	assert.Equal(t, http.StatusOK, resolutionResp.Code)
	resolvedPlan := decodeContextResponseData[project.PlanDocument](t, resolutionResp.Body.Bytes())
	assert.Equal(t, feedbackOnlyPlan.Revision+1, resolvedPlan.Revision)
	assert.Len(t, resolvedPlan.FeedbackLoop.FeedbackResolution, 1)
	assert.Equal(t, "resolved", resolvedPlan.FeedbackLoop.FeedbackResolution[0].Decision)
	assert.Equal(t, "resolved", resolvedPlan.ChangeRequests[len(resolvedPlan.ChangeRequests)-1].Status)
	assert.Equal(t, resolvedPlan.Revision, resolvedPlan.ChangeRequests[len(resolvedPlan.ChangeRequests)-1].AppliedInRevision)

	approvePlanBody := map[string]any{
		"approved_by": "lead",
	}
	body, _ = json.Marshal(approvePlanBody)
	approvePlanReq, _ := http.NewRequest("POST", "/api/v1/projects/"+createdProject.ID+"/plan/approve", bytes.NewBuffer(body))
	approvePlanReq.Header.Set("Content-Type", "application/json")
	approvePlanResp := httptest.NewRecorder()
	router.ServeHTTP(approvePlanResp, approvePlanReq)
	assert.Equal(t, http.StatusOK, approvePlanResp.Code)
	approvedPlan := decodeContextResponseData[project.PlanDocument](t, approvePlanResp.Body.Bytes())
	assert.Equal(t, project.PlanDocumentStatusApproved, approvedPlan.Status)
	assert.Equal(t, "lead", approvedPlan.ApprovedBy)

	executePlanReq, _ := http.NewRequest("POST", "/api/v1/projects/"+createdProject.ID+"/plan/execute", bytes.NewBufferString("{}"))
	executePlanReq.Header.Set("Content-Type", "application/json")
	executePlanReq.Header.Set("X-Session-ID", "sess-project-plan-execute")
	executePlanResp := httptest.NewRecorder()
	router.ServeHTTP(executePlanResp, executePlanReq)
	assert.Equal(t, http.StatusOK, executePlanResp.Code)
	executeResult := decodeContextResponseData[map[string]any](t, executePlanResp.Body.Bytes())
	assert.Equal(t, true, executeResult["started"])
	assert.Equal(t, string(project.PlanDocumentStatusApproved), executeResult["status"])
	assert.Equal(t, "sess-project-plan-execute", executeResult["session_id"])

	auditResult, err := auditSvc.Query(context.Background(), &audit.AuditQuery{Limit: 20})
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, auditResult.Total, 3)
	var actions []string
	for _, entry := range auditResult.Entries {
		actions = append(actions, entry.Action)
	}
	assert.Contains(t, actions, "plan_generated")
	assert.Contains(t, actions, "plan_revised")
	assert.Contains(t, actions, "plan_approved")
	assert.Contains(t, actions, "plan_execute")

	addPlanBody := map[string]any{"plan_id": plan.ID}
	body, _ = json.Marshal(addPlanBody)
	addPlanReq, _ := http.NewRequest("POST", "/api/v1/projects/"+createdProject.ID+"/plans", bytes.NewBuffer(body))
	addPlanReq.Header.Set("Content-Type", "application/json")
	addPlanResp := httptest.NewRecorder()
	router.ServeHTTP(addPlanResp, addPlanReq)
	assert.Equal(t, http.StatusOK, addPlanResp.Code)

	getPlansReq, _ := http.NewRequest("GET", "/api/v1/projects/"+createdProject.ID+"/plans", nil)
	getPlansResp := httptest.NewRecorder()
	router.ServeHTTP(getPlansResp, getPlansReq)
	assert.Equal(t, http.StatusOK, getPlansResp.Code)
	linkedPlans := decodeContextResponseData[map[string]any](t, getPlansResp.Body.Bytes())
	assert.Equal(t, float64(1), linkedPlans["total"])

	updateProjectBody := map[string]any{"status": "active", "progress": 120}
	body, _ = json.Marshal(updateProjectBody)
	updateProjectReq, _ := http.NewRequest("PUT", "/api/v1/projects/"+createdProject.ID, bytes.NewBuffer(body))
	updateProjectReq.Header.Set("Content-Type", "application/json")
	updateProjectResp := httptest.NewRecorder()
	router.ServeHTTP(updateProjectResp, updateProjectReq)
	assert.Equal(t, http.StatusOK, updateProjectResp.Code)
	updatedProject := decodeContextResponseData[project.Project](t, updateProjectResp.Body.Bytes())
	assert.Equal(t, project.StatusActive, updatedProject.Status)
	assert.Equal(t, 100, updatedProject.Progress)

	listProjectsReq, _ := http.NewRequest("GET", "/api/v1/projects?status=active&tag=backend&search=durable", nil)
	listProjectsResp := httptest.NewRecorder()
	router.ServeHTTP(listProjectsResp, listProjectsReq)
	assert.Equal(t, http.StatusOK, listProjectsResp.Code)
	listedProjects := decodeContextResponseData[project.ProjectListResponse](t, listProjectsResp.Body.Bytes())
	assert.Equal(t, 1, listedProjects.Total)
	assert.Len(t, listedProjects.Projects, 1)
	assert.Equal(t, createdProject.ID, listedProjects.Projects[0].ID)
}
