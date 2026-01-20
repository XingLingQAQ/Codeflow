// Package handlers - Summarize API handlers
package handlers

import (
	"net/http"

	"github.com/codeflow/backend/internal/summarize"
	"github.com/gin-gonic/gin"
)

// SummarizeConversation summarizes a conversation.
// POST /api/v1/summarize/conversation
func SummarizeConversation(c *gin.Context) {
	var req summarize.SummarizeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	svc := summarize.GetSummarizer()
	summary, err := svc.SummarizeConversation(&req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, summary)
}

// CompressContext compresses context using 80/20 strategy.
// POST /api/v1/summarize/context
func CompressContext(c *gin.Context) {
	var req summarize.CompressRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	svc := summarize.GetSummarizer()
	result, err := svc.CompressContext(&req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// GetDecisionSkeleton extracts decision skeleton from conversation.
// POST /api/v1/summarize/skeleton
func GetDecisionSkeleton(c *gin.Context) {
	var req struct {
		Messages []summarize.Message `json:"messages" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	svc := summarize.GetSummarizer()
	skeleton, err := svc.ExtractSkeleton(req.Messages)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, skeleton)
}
