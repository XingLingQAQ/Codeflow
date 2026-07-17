// Package handlers - Debate API handlers
package handlers

import (
	"errors"
	"net/http"
	"strings"

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
		respondInternalError(c, "create debate", err)
		return
	}

	respondCreated(c, result)
}

// ListDebates handles GET /api/v1/debates?status=&flow_id=&stage_id=&limit=&offset=
func ListDebates(c *gin.Context) {
	var req debate.DebateListRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid query: "+err.Error())
		return
	}
	result, err := debate.GetDebateManager().ListDebates(c.Request.Context(), &req)
	if err != nil {
		respondInternalError(c, "list debates", err)
		return
	}
	respondOK(c, result)
}

// GetDebate handles GET /api/v1/debates/:id
func GetDebate(c *gin.Context) {
	id, ok := requireUUIDParam(c, "id", "debate ID")
	if !ok {
		return
	}

	svc := debate.GetDebateManager()
	result, err := svc.GetDebate(c.Request.Context(), id)
	if err != nil {
		respondInternalError(c, "get debate", err)
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
	id, ok := requireUUIDParam(c, "id", "debate ID")
	if !ok {
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
		mapDebateError(c, "advance debate round", err)
		return
	}

	// Notify topic subscribers (clients subscribe to debate_event).
	hub := websocket.GetHub()
	msg := &websocket.Message{
		Type:    websocket.MessageType("debate_event"),
		Content: "round_advanced",
		Data: map[string]interface{}{
			"debate_id":     id,
			"current_round": result.CurrentRound,
			"status":        result.Status,
			"flow_id":       result.FlowID,
			"stage_id":      result.StageID,
		},
	}
	hub.BroadcastToTopic(websocket.TopicDebateEvent, msg)
	if result.FlowID != "" {
		hub.BroadcastToTopic("debate:flow:"+result.FlowID, msg)
	}

	respondOK(c, result)
}

// ResolveConflict handles POST /api/v1/debates/:id/conflicts/:cid/resolve
func ResolveConflict(c *gin.Context) {
	debateID, ok := requireUUIDParam(c, "id", "debate ID")
	if !ok {
		return
	}
	conflictID, ok := requireUUIDParam(c, "cid", "conflict ID")
	if !ok {
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
		mapDebateError(c, "resolve debate conflict", err)
		return
	}

	// 通知WebSocket客户端
	hub := websocket.GetHub()
	hub.BroadcastToTopic(websocket.TopicDebateEvent, &websocket.Message{
		Type:    websocket.MessageType("debate_event"),
		Content: "conflict_resolved",
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
	id, ok := requireUUIDParam(c, "id", "debate ID")
	if !ok {
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
		mapDebateError(c, "select debate solution", err)
		return
	}

	// 通知WebSocket客户端
	hub := websocket.GetHub()
	msg := &websocket.Message{
		Type:    websocket.MessageType("debate_event"),
		Content: "solution_selected",
		Data: map[string]interface{}{
			"debate_id":         id,
			"selected_solution": result.SelectedSolution,
			"status":            result.Status,
			"flow_id":           result.FlowID,
		},
	}
	hub.BroadcastToTopic(websocket.TopicDebateEvent, msg)
	if result.FlowID != "" {
		hub.BroadcastToTopic("debate:flow:"+result.FlowID, msg)
	}

	respondOK(c, result)
}

// ExportDebateReport handles GET /api/v1/debates/:id/export
func ExportDebateReport(c *gin.Context) {
	id, ok := requireUUIDParam(c, "id", "debate ID")
	if !ok {
		return
	}

	format := c.Query("format")
	if format == "" {
		format = "json"
	}

	svc := debate.GetDebateManager()
	report, err := svc.ExportReport(c.Request.Context(), id)
	if err != nil {
		respondInternalError(c, "export debate report", err)
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
	id, ok := requireUUIDParam(c, "id", "debate ID")
	if !ok {
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
		mapDebateError(c, "propose debate solution", err)
		return
	}

	// 通知WebSocket客户端
	hub := websocket.GetHub()
	hub.BroadcastToTopic(websocket.TopicDebateEvent, &websocket.Message{
		Type:    websocket.MessageType("debate_event"),
		Content: "solution_proposed",
		Data: map[string]interface{}{
			"debate_id":   id,
			"solution_id": result.ID,
			"title":       result.Title,
		},
	})

	respondCreated(c, result)
}

func mapDebateError(c *gin.Context, op string, err error) {
	if err == nil {
		return
	}
	if errors.Is(err, debate.ErrNotFound) || strings.Contains(err.Error(), "not found") {
		respondError(c, http.StatusNotFound, err.Error())
		return
	}
	if strings.Contains(err.Error(), "not in progress") || strings.Contains(err.Error(), "already") {
		respondError(c, http.StatusConflict, err.Error())
		return
	}
	respondInternalError(c, op, err)
}
