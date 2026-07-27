// Package handlers - Agent registry API (experimental).
package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/agent"
)

func mapAgentRegistryError(c *gin.Context, op string, err error) {
	switch {
	case errors.Is(err, agent.ErrAgentAssetNotFound),
		strings.Contains(err.Error(), "not found"):
		respondError(c, http.StatusNotFound, err.Error())
	case errors.Is(err, agent.ErrBuiltinProtected),
		strings.Contains(err.Error(), "builtin"):
		respondError(c, http.StatusConflict, err.Error())
	default:
		respondError(c, http.StatusBadRequest, err.Error())
	}
}

// CreateRegistryAgent handles POST /api/v1/agents/registry
func CreateRegistryAgent(c *gin.Context) {
	if !agent.HasAgentRegistry() {
		respondError(c, http.StatusServiceUnavailable, "agent registry not available")
		return
	}
	var req agent.CreateAgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	result, err := agent.GetAgentRegistry().Create(c.Request.Context(), &req)
	if err != nil {
		mapAgentRegistryError(c, "create registry agent", err)
		return
	}
	respondCreated(c, result)
}

// ListRegistryAgents handles GET /api/v1/agents/registry
func ListRegistryAgents(c *gin.Context) {
	if !agent.HasAgentRegistry() {
		respondError(c, http.StatusServiceUnavailable, "agent registry not available")
		return
	}
	stage := c.Query("stage")
	role := agent.RoleBase(c.Query("role"))
	source := agent.AgentSource(c.Query("source"))
	items, err := agent.GetAgentRegistry().ListFiltered(c.Request.Context(), stage, role, source)
	if err != nil {
		respondInternalError(c, "list registry agents", err)
		return
	}
	respondOK(c, gin.H{"items": items, "total": len(items)})
}

// GetRegistryAgent handles GET /api/v1/agents/registry/:id
func GetRegistryAgent(c *gin.Context) {
	if !agent.HasAgentRegistry() {
		respondError(c, http.StatusServiceUnavailable, "agent registry not available")
		return
	}
	id := c.Param("id")
	result, err := agent.GetAgentRegistry().Get(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, agent.ErrAgentAssetNotFound) || strings.Contains(err.Error(), "not found") {
			respondError(c, http.StatusNotFound, "agent not found")
			return
		}
		respondInternalError(c, "get registry agent", err)
		return
	}
	respondOK(c, result)
}

// UpdateRegistryAgent handles PATCH /api/v1/agents/registry/:id
func UpdateRegistryAgent(c *gin.Context) {
	if !agent.HasAgentRegistry() {
		respondError(c, http.StatusServiceUnavailable, "agent registry not available")
		return
	}
	id := c.Param("id")
	var req agent.UpdateAgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	result, err := agent.GetAgentRegistry().Update(c.Request.Context(), id, &req)
	if err != nil {
		mapAgentRegistryError(c, "update registry agent", err)
		return
	}
	respondOK(c, result)
}

// DeleteRegistryAgent handles DELETE /api/v1/agents/registry/:id
func DeleteRegistryAgent(c *gin.Context) {
	if !agent.HasAgentRegistry() {
		respondError(c, http.StatusServiceUnavailable, "agent registry not available")
		return
	}
	id := c.Param("id")
	if err := agent.GetAgentRegistry().Delete(c.Request.Context(), id); err != nil {
		mapAgentRegistryError(c, "delete registry agent", err)
		return
	}
	respondOK(c, gin.H{"deleted": true, "id": id})
}

type incrementUsageBody struct {
	Count int `json:"count,omitempty"`
}

// IncrementRegistryAgentUsage handles POST /api/v1/agents/registry/:id/usage
func IncrementRegistryAgentUsage(c *gin.Context) {
	if !agent.HasAgentRegistry() {
		respondError(c, http.StatusServiceUnavailable, "agent registry not available")
		return
	}
	id := c.Param("id")
	if err := agent.GetAgentRegistry().IncrementUsage(c.Request.Context(), id); err != nil {
		mapAgentRegistryError(c, "increment registry agent usage", err)
		return
	}
	respondOK(c, gin.H{"id": id, "incremented": true})
}

type setScoreBody struct {
	Score float64 `json:"score" binding:"required"`
}

// SetRegistryAgentScore handles POST /api/v1/agents/registry/:id/score
func SetRegistryAgentScore(c *gin.Context) {
	if !agent.HasAgentRegistry() {
		respondError(c, http.StatusServiceUnavailable, "agent registry not available")
		return
	}
	id := c.Param("id")
	var body setScoreBody
	if err := c.ShouldBindJSON(&body); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	if err := agent.GetAgentRegistry().SetScore(c.Request.Context(), id, body.Score); err != nil {
		mapAgentRegistryError(c, "set registry agent score", err)
		return
	}
	respondOK(c, gin.H{"id": id, "score": body.Score})
}
