// Package handlers - Plan API handlers
package handlers

import (
	"github.com/gin-gonic/gin"
)

// GetPlans handles GET /api/v1/plans
func GetPlans(c *gin.Context) {
	respondNotImplemented(c)
}

// CreatePlan handles POST /api/v1/plans
func CreatePlan(c *gin.Context) {
	respondNotImplemented(c)
}

// GetPlanTasks handles GET /api/v1/plans/:id/tasks
func GetPlanTasks(c *gin.Context) {
	respondNotImplemented(c)
}

// CreatePlanTask handles POST /api/v1/plans/:id/tasks
func CreatePlanTask(c *gin.Context) {
	respondNotImplemented(c)
}

// UpdatePlanTask handles PATCH /api/v1/plans/:id/tasks/:tid
func UpdatePlanTask(c *gin.Context) {
	respondNotImplemented(c)
}

// ReorderPlanTask handles POST /api/v1/plans/:id/tasks/:tid/reorder
func ReorderPlanTask(c *gin.Context) {
	respondNotImplemented(c)
}

// BatchUpdateTaskModel handles POST /api/v1/plans/:id/tasks/batch-model
func BatchUpdateTaskModel(c *gin.Context) {
	respondNotImplemented(c)
}

// DeletePlanTask handles DELETE /api/v1/plans/:id/tasks/:tid
func DeletePlanTask(c *gin.Context) {
	respondNotImplemented(c)
}
