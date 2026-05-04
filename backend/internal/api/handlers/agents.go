// Package handlers - Agent API handlers
package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/agent"
	"github.com/codeflow/backend/internal/websocket"
)

// GetAgents handles GET /api/v1/agents
func GetAgents(c *gin.Context) {
	svc := agent.GetAgentService()
	result, err := svc.ListAgents(c.Request.Context())
	if err != nil {
		respondInternalError(c, "list agents", err)
		return
	}

	respondOK(c, result)
}

// GetAgent handles GET /api/v1/agents/:id
func GetAgent(c *gin.Context) {
	id, ok := requireUUIDParam(c, "id", "agent ID")
	if !ok {
		return
	}

	svc := agent.GetAgentService()
	result, err := svc.GetAgent(c.Request.Context(), id)
	if err != nil {
		respondInternalError(c, "get agent", err)
		return
	}
	if result == nil {
		respondError(c, http.StatusNotFound, "Agent not found")
		return
	}

	respondOK(c, result)
}

// CreateAgent handles POST /api/v1/agents
func CreateAgent(c *gin.Context) {
	var req agent.AgentCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	svc := agent.GetAgentService()
	result, err := svc.CreateAgent(c.Request.Context(), &req)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	respondCreated(c, result)
}

// UpdateAgent handles PUT /api/v1/agents/:id
func UpdateAgent(c *gin.Context) {
	id, ok := requireUUIDParam(c, "id", "agent ID")
	if !ok {
		return
	}

	var req agent.AgentUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	svc := agent.GetAgentService()
	result, err := svc.UpdateAgent(c.Request.Context(), id, &req)
	if err != nil {
		if errors.Is(err, agent.ErrAgentNotFound) {
			respondError(c, http.StatusNotFound, "Agent not found")
			return
		}
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	respondOK(c, result)
}

// DeleteAgent handles DELETE /api/v1/agents/:id
func DeleteAgent(c *gin.Context) {
	id, ok := requireUUIDParam(c, "id", "agent ID")
	if !ok {
		return
	}

	svc := agent.GetAgentService()
	if err := svc.DeleteAgent(c.Request.Context(), id); err != nil {
		if errors.Is(err, agent.ErrAgentNotFound) {
			respondError(c, http.StatusNotFound, "Agent not found")
			return
		}
		respondInternalError(c, "delete agent", err)
		return
	}

	respondOK(c, gin.H{"deleted": true, "id": id})
}

// GetAgentLogs handles GET /api/v1/agents/:id/logs
func GetAgentLogs(c *gin.Context) {
	id, ok := requireUUIDParam(c, "id", "agent ID")
	if !ok {
		return
	}

	limit, ok := parsePositiveQueryInt(c, "limit", 100, 1000)
	if !ok {
		return
	}

	svc := agent.GetAgentService()
	result, err := svc.GetAgentLogs(c.Request.Context(), id, limit)
	if err != nil {
		respondInternalError(c, "get agent logs", err)
		return
	}

	respondOK(c, result)
}

// GetConversationTrace handles GET /api/v1/conversations/:sessionId/trace
func GetConversationTrace(c *gin.Context) {
	sessionID := c.Param("sessionId")
	if sessionID == "" {
		respondError(c, http.StatusBadRequest, "Missing session ID")
		return
	}

	svc := agent.GetAgentService()
	result, err := svc.GetConversationTrace(c.Request.Context(), sessionID)
	if err != nil {
		respondInternalError(c, "get conversation trace", err)
		return
	}
	if result == nil {
		respondError(c, http.StatusNotFound, "Conversation not found")
		return
	}

	respondOK(c, result)
}

// StopConversation handles POST /api/v1/conversations/:sessionId/stop
func StopConversation(c *gin.Context) {
	sessionID := c.Param("sessionId")
	if sessionID == "" {
		respondError(c, http.StatusBadRequest, "Missing session ID")
		return
	}

	svc := agent.GetAgentService()
	if err := svc.StopConversation(c.Request.Context(), sessionID); err != nil {
		respondInternalError(c, "stop conversation", err)
		return
	}

	// 通知WebSocket客户端
	hub := websocket.GetHub()
	hub.BroadcastToSession(sessionID, &websocket.Message{
		Type:    websocket.MsgTypeText,
		Content: "Conversation stopped",
		Data:    map[string]interface{}{"action": "stopped"},
	})

	respondOK(c, gin.H{"stopped": true, "session_id": sessionID})
}

// RetryConversation handles POST /api/v1/conversations/:sessionId/retry
func RetryConversation(c *gin.Context) {
	sessionID := c.Param("sessionId")
	if sessionID == "" {
		respondError(c, http.StatusBadRequest, "Missing session ID")
		return
	}

	svc := agent.GetAgentService()
	if err := svc.RetryConversation(c.Request.Context(), sessionID); err != nil {
		respondInternalError(c, "retry conversation", err)
		return
	}

	// 通知WebSocket客户端
	hub := websocket.GetHub()
	hub.BroadcastToSession(sessionID, &websocket.Message{
		Type:    websocket.MsgTypeText,
		Content: "Conversation retrying",
		Data:    map[string]interface{}{"action": "retry"},
	})

	respondOK(c, gin.H{"retrying": true, "session_id": sessionID})
}

// StreamConversation handles WebSocket /api/v1/conversations/:sessionId/stream
func StreamConversation(c *gin.Context) {
	hub := websocket.GetHub()
	websocket.HandleWebSocket(hub, c)
}
