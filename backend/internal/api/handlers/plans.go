// Package handlers - Plan API handlers
package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/planner"
)

// GetPlans handles GET /api/v1/plans
func GetPlans(c *gin.Context) {
	var req planner.PlanListRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid query parameters: "+err.Error())
		return
	}

	svc := planner.GetPlanner()
	result, err := svc.ListPlans(c.Request.Context(), &req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to list plans: "+err.Error())
		return
	}

	respondOK(c, result)
}

// CreatePlan handles POST /api/v1/plans
func CreatePlan(c *gin.Context) {
	var req planner.PlanCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	svc := planner.GetPlanner()
	result, err := svc.CreatePlan(c.Request.Context(), &req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to create plan: "+err.Error())
		return
	}

	respondCreated(c, result)
}

// GetPlanTasks handles GET /api/v1/plans/:id/tasks
func GetPlanTasks(c *gin.Context) {
	planID := c.Param("id")
	if planID == "" {
		respondError(c, http.StatusBadRequest, "Missing plan ID")
		return
	}

	var req planner.TaskListRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid query parameters: "+err.Error())
		return
	}

	svc := planner.GetPlanner()
	result, err := svc.ListTasks(c.Request.Context(), planID, &req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to list tasks: "+err.Error())
		return
	}

	respondOK(c, result)
}

// CreatePlanTask handles POST /api/v1/plans/:id/tasks
func CreatePlanTask(c *gin.Context) {
	planID := c.Param("id")
	if planID == "" {
		respondError(c, http.StatusBadRequest, "Missing plan ID")
		return
	}

	var req planner.TaskCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	svc := planner.GetPlanner()
	result, err := svc.CreateTask(c.Request.Context(), planID, &req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to create task: "+err.Error())
		return
	}

	respondCreated(c, result)
}

// UpdatePlanTask handles PATCH /api/v1/plans/:id/tasks/:tid
func UpdatePlanTask(c *gin.Context) {
	planID := c.Param("id")
	taskID := c.Param("tid")
	if planID == "" || taskID == "" {
		respondError(c, http.StatusBadRequest, "Missing plan ID or task ID")
		return
	}

	var req planner.TaskUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	svc := planner.GetPlanner()
	result, err := svc.UpdateTask(c.Request.Context(), planID, taskID, &req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to update task: "+err.Error())
		return
	}

	respondOK(c, result)
}

// ReorderPlanTask handles POST /api/v1/plans/:id/tasks/:tid/reorder
func ReorderPlanTask(c *gin.Context) {
	planID := c.Param("id")
	taskID := c.Param("tid")
	if planID == "" || taskID == "" {
		respondError(c, http.StatusBadRequest, "Missing plan ID or task ID")
		return
	}

	var req planner.TaskReorderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	svc := planner.GetPlanner()
	result, err := svc.ReorderTask(c.Request.Context(), planID, taskID, &req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to reorder task: "+err.Error())
		return
	}

	respondOK(c, result)
}

// BatchUpdateTaskModel handles POST /api/v1/plans/:id/tasks/batch-model
func BatchUpdateTaskModel(c *gin.Context) {
	planID := c.Param("id")
	if planID == "" {
		respondError(c, http.StatusBadRequest, "Missing plan ID")
		return
	}

	var req planner.BatchModelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	svc := planner.GetPlanner()
	result, err := svc.BatchUpdateModel(c.Request.Context(), planID, &req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to batch update model: "+err.Error())
		return
	}

	respondOK(c, result)
}

// DeletePlanTask handles DELETE /api/v1/plans/:id/tasks/:tid
func DeletePlanTask(c *gin.Context) {
	planID := c.Param("id")
	taskID := c.Param("tid")
	if planID == "" || taskID == "" {
		respondError(c, http.StatusBadRequest, "Missing plan ID or task ID")
		return
	}

	svc := planner.GetPlanner()
	if err := svc.DeleteTask(c.Request.Context(), planID, taskID); err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to delete task: "+err.Error())
		return
	}

	respondOK(c, gin.H{"deleted": true, "plan_id": planID, "task_id": taskID})
}
