// Package handlers - Project API handlers
package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/project"
)

// GetProjects handles GET /api/v1/projects
func GetProjects(c *gin.Context) {
	var req project.ProjectListRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid query parameters: "+err.Error())
		return
	}

	svc := project.GetProjectService()
	result, err := svc.ListProjects(c.Request.Context(), &req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to list projects: "+err.Error())
		return
	}

	respondOK(c, result)
}

// CreateProject handles POST /api/v1/projects
func CreateProject(c *gin.Context) {
	var req project.ProjectCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	svc := project.GetProjectService()
	result, err := svc.CreateProject(c.Request.Context(), &req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to create project: "+err.Error())
		return
	}

	respondCreated(c, result)
}

// GetProject handles GET /api/v1/projects/:id
func GetProject(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		respondError(c, http.StatusBadRequest, "Missing project ID")
		return
	}

	svc := project.GetProjectService()
	result, err := svc.GetProject(c.Request.Context(), id)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to get project: "+err.Error())
		return
	}
	if result == nil {
		respondError(c, http.StatusNotFound, "Project not found")
		return
	}

	respondOK(c, result)
}

// UpdateProject handles PUT /api/v1/projects/:id
func UpdateProject(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		respondError(c, http.StatusBadRequest, "Missing project ID")
		return
	}

	var req project.ProjectUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	svc := project.GetProjectService()
	result, err := svc.UpdateProject(c.Request.Context(), id, &req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to update project: "+err.Error())
		return
	}

	respondOK(c, result)
}

// DeleteProject handles DELETE /api/v1/projects/:id
func DeleteProject(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		respondError(c, http.StatusBadRequest, "Missing project ID")
		return
	}

	svc := project.GetProjectService()
	if err := svc.DeleteProject(c.Request.Context(), id); err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to delete project: "+err.Error())
		return
	}

	respondOK(c, gin.H{"deleted": true, "id": id})
}

// GetProjectPlans handles GET /api/v1/projects/:id/plans
func GetProjectPlans(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		respondError(c, http.StatusBadRequest, "Missing project ID")
		return
	}

	svc := project.GetProjectService()
	plans, err := svc.GetProjectPlans(c.Request.Context(), id)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to get project plans: "+err.Error())
		return
	}

	respondOK(c, gin.H{"plans": plans, "total": len(plans)})
}

// AddPlanToProject handles POST /api/v1/projects/:id/plans
func AddPlanToProject(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		respondError(c, http.StatusBadRequest, "Missing project ID")
		return
	}

	var req project.AddPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	svc := project.GetProjectService()
	if err := svc.AddPlanToProject(c.Request.Context(), id, req.PlanID); err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to add plan to project: "+err.Error())
		return
	}

	respondOK(c, gin.H{"project_id": id, "plan_id": req.PlanID, "associated": true})
}

// RemovePlanFromProject handles DELETE /api/v1/projects/:id/plans/:planId
func RemovePlanFromProject(c *gin.Context) {
	id := c.Param("id")
	planID := c.Param("planId")
	if id == "" || planID == "" {
		respondError(c, http.StatusBadRequest, "Missing project ID or plan ID")
		return
	}

	svc := project.GetProjectService()
	if err := svc.RemovePlanFromProject(c.Request.Context(), id, planID); err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to remove plan from project: "+err.Error())
		return
	}

	respondOK(c, gin.H{"project_id": id, "plan_id": planID, "removed": true})
}
