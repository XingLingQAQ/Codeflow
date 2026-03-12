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
)

func setupPlansRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	v1 := router.Group("/api/v1")
	{
		plans := v1.Group("/plans")
		{
			plans.GET("", GetPlans)
			plans.POST("", CreatePlan)
			plans.GET("/:id/tasks", GetPlanTasks)
			plans.POST("/:id/tasks", CreatePlanTask)
			plans.PATCH("/:id/tasks/:tid", UpdatePlanTask)
			plans.POST("/:id/tasks/:tid/reorder", ReorderPlanTask)
			plans.POST("/:id/tasks/batch-model", BatchUpdateTaskModel)
			plans.DELETE("/:id/tasks/:tid", DeletePlanTask)
		}
	}

	return router
}

func TestPlansLifecycleAPIWithSQLite(t *testing.T) {
	router := setupPlansRouter()
	svc, err := planner.NewSQLitePlanner(":memory:")
	if err != nil {
		t.Fatalf("NewSQLitePlanner failed: %v", err)
	}
	planner.SetPlanner(svc)
	defer func() {
		planner.SetPlanner(nil)
		_ = svc.Close()
	}()

	planBody := map[string]any{"title": "backend durable"}
	body, _ := json.Marshal(planBody)
	createPlanReq, _ := http.NewRequest("POST", "/api/v1/plans", bytes.NewBuffer(body))
	createPlanReq.Header.Set("Content-Type", "application/json")
	createPlanResp := httptest.NewRecorder()
	router.ServeHTTP(createPlanResp, createPlanReq)

	assert.Equal(t, http.StatusCreated, createPlanResp.Code)
	createdPlan := decodeContextResponseData[planner.Plan](t, createPlanResp.Body.Bytes())
	assert.NotEmpty(t, createdPlan.ID)

	createTaskBody := map[string]any{"title": "task-a"}
	body, _ = json.Marshal(createTaskBody)
	createTaskReq, _ := http.NewRequest("POST", "/api/v1/plans/"+createdPlan.ID+"/tasks", bytes.NewBuffer(body))
	createTaskReq.Header.Set("Content-Type", "application/json")
	createTaskResp := httptest.NewRecorder()
	router.ServeHTTP(createTaskResp, createTaskReq)

	assert.Equal(t, http.StatusCreated, createTaskResp.Code)
	createdTask := decodeContextResponseData[planner.Task](t, createTaskResp.Body.Bytes())
	assert.NotEmpty(t, createdTask.ID)

	updateTaskBody := map[string]any{"status": "completed"}
	body, _ = json.Marshal(updateTaskBody)
	updateTaskReq, _ := http.NewRequest("PATCH", "/api/v1/plans/"+createdPlan.ID+"/tasks/"+createdTask.ID, bytes.NewBuffer(body))
	updateTaskReq.Header.Set("Content-Type", "application/json")
	updateTaskResp := httptest.NewRecorder()
	router.ServeHTTP(updateTaskResp, updateTaskReq)

	assert.Equal(t, http.StatusOK, updateTaskResp.Code)
	updatedTask := decodeContextResponseData[planner.Task](t, updateTaskResp.Body.Bytes())
	assert.Equal(t, planner.TaskStatusCompleted, updatedTask.Status)

	listTasksReq, _ := http.NewRequest("GET", "/api/v1/plans/"+createdPlan.ID+"/tasks", nil)
	listTasksResp := httptest.NewRecorder()
	router.ServeHTTP(listTasksResp, listTasksReq)

	assert.Equal(t, http.StatusOK, listTasksResp.Code)
	listedTasks := decodeContextResponseData[planner.TaskListResponse](t, listTasksResp.Body.Bytes())
	assert.Equal(t, 1, listedTasks.Total)
	assert.Len(t, listedTasks.Tasks, 1)
	assert.Equal(t, planner.TaskStatusCompleted, listedTasks.Tasks[0].Status)

	listPlansReq, _ := http.NewRequest("GET", "/api/v1/plans", nil)
	listPlansResp := httptest.NewRecorder()
	router.ServeHTTP(listPlansResp, listPlansReq)
	assert.Equal(t, http.StatusOK, listPlansResp.Code)
	listedPlans := decodeContextResponseData[planner.PlanListResponse](t, listPlansResp.Body.Bytes())
	assert.Equal(t, 1, listedPlans.Total)
	assert.Equal(t, 1, listedPlans.Plans[0].CompletedCount)
}
