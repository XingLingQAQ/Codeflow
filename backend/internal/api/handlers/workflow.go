package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/workflow"
)

// GetWorkflowOverview handles GET /api/v1/workflows/:projectId/overview
func GetWorkflowOverview(c *gin.Context) {
	projectID := c.Param("projectId")
	if projectID == "" {
		respondError(c, http.StatusBadRequest, "Missing project ID")
		return
	}

	svc := workflow.GetService()
	result, err := svc.GetOverview(c.Request.Context(), projectID)
	if err != nil {
		if errors.Is(err, workflow.ErrProjectNotFound) {
			respondError(c, http.StatusNotFound, "Project not found")
			return
		}
		respondError(c, http.StatusInternalServerError, "Failed to get workflow overview: "+err.Error())
		return
	}

	respondOK(c, result)
}

// GetWorkflowTimeline handles GET /api/v1/workflows/:projectId/timeline
func GetWorkflowTimeline(c *gin.Context) {
	projectID := c.Param("projectId")
	if projectID == "" {
		respondError(c, http.StatusBadRequest, "Missing project ID")
		return
	}

	svc := workflow.GetService()
	result, err := svc.GetTimeline(c.Request.Context(), projectID)
	if err != nil {
		if errors.Is(err, workflow.ErrProjectNotFound) {
			respondError(c, http.StatusNotFound, "Project not found")
			return
		}
		respondError(c, http.StatusInternalServerError, "Failed to get workflow timeline: "+err.Error())
		return
	}

	respondOK(c, result)
}

// GetWorkflowReplay handles GET /api/v1/workflows/:projectId/replay
func GetWorkflowReplay(c *gin.Context) {
	projectID := c.Param("projectId")
	if projectID == "" {
		respondError(c, http.StatusBadRequest, "Missing project ID")
		return
	}

	sessionID := c.Query("session_id")
	svc := workflow.GetService()
	result, err := svc.GetReplay(c.Request.Context(), projectID, sessionID)
	if err != nil {
		if errors.Is(err, workflow.ErrProjectNotFound) {
			respondError(c, http.StatusNotFound, "Project not found")
			return
		}
		if errors.Is(err, workflow.ErrSessionNotInProject) {
			respondSessionNotInProject(c, err)
			return
		}
		respondError(c, http.StatusInternalServerError, "Failed to get workflow replay: "+err.Error())
		return
	}

	respondOK(c, result)
}

// respondSessionNotInProject answers a replay request whose explicit session_id
// is not authorized for the path project: 403, with the envelope data naming
// both IDs so the caller can tell which scope rejected it (E-01).
func respondSessionNotInProject(c *gin.Context, err error) {
	data := gin.H{}
	var mismatch *workflow.SessionNotInProjectError
	if errors.As(err, &mismatch) {
		data["project_id"] = mismatch.ProjectID
		data["session_id"] = mismatch.SessionID
	}
	c.JSON(http.StatusForbidden, Response{
		Success: false,
		Error:   "Session is not authorized for this project",
		Data:    data,
	})
}
