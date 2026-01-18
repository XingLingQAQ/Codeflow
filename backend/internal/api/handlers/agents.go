// Package handlers - Agent API handlers
package handlers

import (
	"github.com/gin-gonic/gin"
)

// GetAgents handles GET /api/v1/agents
func GetAgents(c *gin.Context) {
	respondNotImplemented(c)
}

// GetAgentLogs handles GET /api/v1/agents/:id/logs
func GetAgentLogs(c *gin.Context) {
	respondNotImplemented(c)
}

// GetConversationTrace handles GET /api/v1/conversations/:sessionId/trace
func GetConversationTrace(c *gin.Context) {
	respondNotImplemented(c)
}

// StopConversation handles POST /api/v1/conversations/:sessionId/stop
func StopConversation(c *gin.Context) {
	respondNotImplemented(c)
}

// RetryConversation handles POST /api/v1/conversations/:sessionId/retry
func RetryConversation(c *gin.Context) {
	respondNotImplemented(c)
}

// StreamConversation handles WebSocket /api/v1/conversations/:sessionId/stream
func StreamConversation(c *gin.Context) {
	respondNotImplemented(c)
}
