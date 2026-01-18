// Package handlers - Debate API handlers
package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/debate"
	"github.com/codeflow/backend/internal/websocket"
)

// CreateDebate handles POST /api/v1/debates
func CreateDebate(c *gin.Context) {
	var req debate.DebateCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	svc := debate.GetDebateManager()
	result, err := svc.CreateDebate(c.Request.Context(), &req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to create debate: "+err.Error())
		return
	}

	respondCreated(c, result)
}

// GetDebate handles GET /api/v1/debates/:id
func GetDebate(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		respondError(c, http.StatusBadRequest, "Missing debate ID")
		return
	}

	svc := debate.GetDebateManager()
	result, err := svc.GetDebate(c.Request.Context(), id)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to get debate: "+err.Error())
		return
	}
	if result == nil {
		respondError(c, http.StatusNotFound, "Debate not found")
		return
	}

	respondOK(c, result)
}

// NextDebateRound handles POST /api/v1/debates/:id/next-round
func NextDebateRound(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		respondError(c, http.StatusBadRequest, "Missing debate ID")
		return
	}

	var req debate.NextRoundRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	svc := debate.GetDebateManager()
	result, err := svc.NextRound(c.Request.Context(), id, &req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to advance round: "+err.Error())
		return
	}

	// 通知WebSocket客户端
	hub := websocket.GetHub()
	hub.BroadcastAll(&websocket.Message{
		Type:    websocket.MsgTypeText,
		Content: "Debate round advanced",
		Data: map[string]interface{}{
			"debate_id":     id,
			"current_round": result.CurrentRound,
			"status":        result.Status,
		},
	})

	respondOK(c, result)
}

// ResolveConflict handles POST /api/v1/debates/:id/conflicts/:cid/resolve
func ResolveConflict(c *gin.Context) {
	debateID := c.Param("id")
	conflictID := c.Param("cid")
	if debateID == "" || conflictID == "" {
		respondError(c, http.StatusBadRequest, "Missing debate ID or conflict ID")
		return
	}

	var req debate.ResolveConflictRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	svc := debate.GetDebateManager()
	result, err := svc.ResolveConflict(c.Request.Context(), debateID, conflictID, &req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to resolve conflict: "+err.Error())
		return
	}

	// 通知WebSocket客户端
	hub := websocket.GetHub()
	hub.BroadcastAll(&websocket.Message{
		Type:    websocket.MsgTypeText,
		Content: "Conflict resolved",
		Data: map[string]interface{}{
			"debate_id":   debateID,
			"conflict_id": conflictID,
			"resolution":  result.Resolution,
		},
	})

	respondOK(c, result)
}

// SelectSolution handles POST /api/v1/debates/:id/select-solution
func SelectSolution(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		respondError(c, http.StatusBadRequest, "Missing debate ID")
		return
	}

	var req debate.SelectSolutionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	svc := debate.GetDebateManager()
	result, err := svc.SelectSolution(c.Request.Context(), id, &req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to select solution: "+err.Error())
		return
	}

	// 通知WebSocket客户端
	hub := websocket.GetHub()
	hub.BroadcastAll(&websocket.Message{
		Type:    websocket.MsgTypeText,
		Content: "Solution selected, debate resolved",
		Data: map[string]interface{}{
			"debate_id":         id,
			"selected_solution": result.SelectedSolution,
			"status":            result.Status,
		},
	})

	respondOK(c, result)
}

// ExportDebateReport handles GET /api/v1/debates/:id/export
func ExportDebateReport(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		respondError(c, http.StatusBadRequest, "Missing debate ID")
		return
	}

	format := c.Query("format")
	if format == "" {
		format = "json"
	}

	svc := debate.GetDebateManager()
	report, err := svc.ExportReport(c.Request.Context(), id)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to export report: "+err.Error())
		return
	}

	respondOK(c, report)
}

// StreamDebate handles WebSocket /api/v1/debates/:id/stream
func StreamDebate(c *gin.Context) {
	hub := websocket.GetHub()
	websocket.HandleWebSocket(hub, c)
}

// ProposeSolution handles POST /api/v1/debates/:id/solutions (additional endpoint)
func ProposeSolution(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		respondError(c, http.StatusBadRequest, "Missing debate ID")
		return
	}

	var req debate.ProposeSolutionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	svc := debate.GetDebateManager()
	result, err := svc.ProposeSolution(c.Request.Context(), id, &req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to propose solution: "+err.Error())
		return
	}

	// 通知WebSocket客户端
	hub := websocket.GetHub()
	hub.BroadcastAll(&websocket.Message{
		Type:    websocket.MsgTypeText,
		Content: "New solution proposed",
		Data: map[string]interface{}{
			"debate_id":   id,
			"solution_id": result.ID,
			"title":       result.Title,
		},
	})

	respondCreated(c, result)
}
