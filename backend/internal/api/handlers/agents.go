// Package handlers - Agent API handlers
package handlers

import (
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
		respondError(c, http.StatusInternalServerError, "Failed to list agents: "+err.Error())
		return
	}

	respondOK(c, result)
}

// GetAgentLogs handles GET /api/v1/agents/:id/logs
func GetAgentLogs(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		respondError(c, http.StatusBadRequest, "Missing agent ID")
		return
	}

	limit := 100
	if l := c.Query("limit"); l != "" {
		if _, err := c.GetQuery("limit"); err {
			// 使用默认值
		}
	}

	svc := agent.GetAgentService()
	result, err := svc.GetAgentLogs(c.Request.Context(), id, limit)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to get agent logs: "+err.Error())
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
		respondError(c, http.StatusInternalServerError, "Failed to get conversation trace: "+err.Error())
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
		respondError(c, http.StatusInternalServerError, "Failed to stop conversation: "+err.Error())
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
		respondError(c, http.StatusInternalServerError, "Failed to retry conversation: "+err.Error())
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
