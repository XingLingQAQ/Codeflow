package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/codeflow/backend/internal/audit"
	"github.com/codeflow/backend/internal/floweng"
	"github.com/codeflow/backend/internal/planner"
	"github.com/codeflow/backend/internal/project"
)

type createProjectAPIResponse struct {
	project.Project
	Flow    *floweng.Flow  `json:"flow"`
	Session map[string]any `json:"session"`
}

type failingFlowStore struct{}

func (failingFlowStore) Put(*floweng.Flow) error { return errors.New("flow store unavailable") }
func (failingFlowStore) Get(string) (*floweng.Flow, error) {
	return nil, errors.New("flow store unavailable")
}
func (failingFlowStore) List(string) ([]*floweng.Flow, error) {
	return nil, errors.New("flow store unavailable")
}
func (failingFlowStore) Delete(string) error { return errors.New("flow store unavailable") }

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

func TestCreateProjectProvisionsDefaultFlow(t *testing.T) {
	router := setupProjectsRouter()
	projectSvc := project.NewInMemoryProjectService()
	flowEngine := floweng.NewInMemoryEngine(nil)
	previousProjectSvc := project.GetProjectService()
	previousFlowEngine := floweng.GetEngine()
	project.SetProjectService(projectSvc)
	floweng.SetEngine(flowEngine)
	t.Cleanup(func() {
		project.SetProjectService(previousProjectSvc)
		floweng.SetEngine(previousFlowEngine)
	})

	body, _ := json.Marshal(map[string]any{"title": "new runnable project"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusCreated, resp.Code)
	created := decodeContextResponseData[createProjectAPIResponse](t, resp.Body.Bytes())
	assert.NotEmpty(t, created.ID)
	if assert.NotNil(t, created.Flow) {
		assert.Equal(t, created.ID, created.Flow.ProjectID)
		assert.Equal(t, floweng.TemplateNewProject, created.Flow.TemplateID)
		assert.Len(t, created.Flow.Stages, 7)
		assert.Equal(t, floweng.StageTypeIdea, created.Flow.Stages[0].Type)
		assert.Equal(t, floweng.StageStatusActive, created.Flow.Stages[0].Status)
		assert.Equal(t, created.DefaultSessionID, created.Flow.SessionID)
	}
	assert.NotEmpty(t, created.DefaultFlowID)
	assert.NotEmpty(t, created.DefaultSessionID)
	assert.Equal(t, created.DefaultSessionID, created.Session["id"])

	flows, err := flowEngine.List(context.Background(), created.ID)
	assert.NoError(t, err)
	assert.Len(t, flows, 1)
}

func TestCreateProjectIdempotencyKeyReturnsOriginalBindings(t *testing.T) {
	router := setupProjectsRouter()
	projectSvc := project.NewInMemoryProjectService()
	flowEngine := floweng.NewInMemoryEngine(nil)
	previousProjectSvc := project.GetProjectService()
	previousFlowEngine := floweng.GetEngine()
	project.SetProjectService(projectSvc)
	floweng.SetEngine(flowEngine)
	t.Cleanup(func() { project.SetProjectService(previousProjectSvc); floweng.SetEngine(previousFlowEngine) })
	request := func(title string) createProjectAPIResponse {
		body, _ := json.Marshal(map[string]any{"title": title})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "project-create-1")
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		assert.Equal(t, http.StatusCreated, resp.Code)
		return decodeContextResponseData[createProjectAPIResponse](t, resp.Body.Bytes())
	}
	one := request("original")
	two := request("retry payload")
	assert.Equal(t, one.ID, two.ID)
	assert.Equal(t, one.DefaultFlowID, two.DefaultFlowID)
	assert.Equal(t, one.DefaultSessionID, two.DefaultSessionID)
}

// TestCreationDifferentPayloadConflicts is the HTTP-layer half of the §28
// acceptance pair: replaying the create endpoint under the same
// Idempotency-Key with a different payload is a 409 naming the conflicting
// operation and the differing journaled fields, while the original payload
// still replays the original operation. The service-layer half lives in
// backend/internal/project (typed *ProjectCreateConflictError).
func TestCreationDifferentPayloadConflicts(t *testing.T) {
	router := setupProjectsRouter()
	projectSvc, err := project.NewSQLiteProjectService(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteProjectService failed: %v", err)
	}
	flowEngine := floweng.NewInMemoryEngine(nil)
	previousProjectSvc := project.GetProjectService()
	previousFlowEngine := floweng.GetEngine()
	project.SetProjectService(projectSvc)
	floweng.SetEngine(flowEngine)
	t.Cleanup(func() {
		project.SetProjectService(previousProjectSvc)
		floweng.SetEngine(previousFlowEngine)
		_ = projectSvc.Close()
	})

	post := func(title string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]any{"title": title})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "http-conflict-key")
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		return resp
	}

	first := post("alpha")
	assert.Equal(t, http.StatusCreated, first.Code)
	created := decodeContextResponseData[createProjectAPIResponse](t, first.Body.Bytes())
	assert.NotEmpty(t, created.ID)

	conflict := post("beta")
	assert.Equal(t, http.StatusConflict, conflict.Code)
	var envelope struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
		Data    struct {
			IdempotencyKey  string   `json:"idempotency_key"`
			ProjectID       string   `json:"project_id"`
			DifferingFields []string `json:"differing_fields"`
		} `json:"data"`
	}
	assert.NoError(t, json.Unmarshal(conflict.Body.Bytes(), &envelope))
	assert.False(t, envelope.Success)
	assert.NotEmpty(t, envelope.Error)
	assert.Equal(t, "http-conflict-key", envelope.Data.IdempotencyKey)
	assert.Equal(t, created.ID, envelope.Data.ProjectID)
	assert.Equal(t, []string{"title"}, envelope.Data.DifferingFields)

	replay := post("alpha")
	assert.Equal(t, http.StatusCreated, replay.Code)
	replayed := decodeContextResponseData[createProjectAPIResponse](t, replay.Body.Bytes())
	assert.Equal(t, created.ID, replayed.ID)
	assert.Equal(t, created.DefaultFlowID, replayed.DefaultFlowID)
	assert.Equal(t, created.DefaultSessionID, replayed.DefaultSessionID)
}

func TestCreateProjectDoesNotPersistProjectWhenFlowCreationFails(t *testing.T) {
	router := setupProjectsRouter()
	projectSvc := project.NewInMemoryProjectService()
	flowEngine := floweng.NewEngineWithStore(failingFlowStore{}, nil)
	previousProjectSvc := project.GetProjectService()
	previousFlowEngine := floweng.GetEngine()
	project.SetProjectService(projectSvc)
	floweng.SetEngine(flowEngine)
	t.Cleanup(func() {
		project.SetProjectService(previousProjectSvc)
		floweng.SetEngine(previousFlowEngine)
	})

	body, _ := json.Marshal(map[string]any{"title": "must not be partial"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusInternalServerError, resp.Code)
	listed, err := projectSvc.ListProjects(context.Background(), &project.ProjectListRequest{})
	assert.NoError(t, err)
	assert.Zero(t, listed.Total)
}

func TestCreateProjectRejectsWhitespaceOnlyTitle(t *testing.T) {
	router := setupProjectsRouter()
	projectSvc := project.NewInMemoryProjectService()
	flowEngine := floweng.NewInMemoryEngine(nil)
	previousProjectSvc := project.GetProjectService()
	previousFlowEngine := floweng.GetEngine()
	project.SetProjectService(projectSvc)
	floweng.SetEngine(flowEngine)
	t.Cleanup(func() {
		project.SetProjectService(previousProjectSvc)
		floweng.SetEngine(previousFlowEngine)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewBufferString(`{"title":"   "}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusBadRequest, resp.Code)
	flows, err := flowEngine.List(context.Background(), "")
	assert.NoError(t, err)
	assert.Empty(t, flows)
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
