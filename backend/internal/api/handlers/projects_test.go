package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

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
		}
	}

	return router
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
	defer func() {
		project.SetProjectService(nil)
		planner.SetPlanner(nil)
		_ = projectSvc.Close()
		_ = plannerSvc.Close()
	}()

	plan, err := plannerSvc.CreatePlan(t.Context(), &planner.PlanCreateRequest{Title: "plan-for-project"})
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
