// Package handlers - Memory Preflight API handlers
package handlers

import (
	"net/http"
	"strconv"

	"github.com/codeflow/backend/internal/memory"
	"github.com/gin-gonic/gin"
)

// MemoryPreflight performs a preflight check and returns matching memories.
// POST /api/v1/memory/preflight
func MemoryPreflight(c *gin.Context) {
	var req memory.PreflightRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	svc := memory.GetPreflightService()
	resp, err := svc.Preflight(&req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetMemorySuggestions returns memory suggestions based on context.
// GET /api/v1/memory/suggestions?context_id=xxx&limit=10
func GetMemorySuggestions(c *gin.Context) {
	contextID := c.Query("context_id")
	if contextID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "context_id is required"})
		return
	}

	limitStr := c.DefaultQuery("limit", "10")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 1 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}

	svc := memory.GetPreflightService()
	suggestions, err := svc.GetSuggestions(contextID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"suggestions": suggestions,
		"context_id":  contextID,
		"limit":       limit,
		"total":       len(suggestions),
	})
}

// InjectMemory injects a memory into the context.
// POST /api/v1/memory/inject
func InjectMemory(c *gin.Context) {
	var req struct {
		ContextID string   `json:"context_id" binding:"required"`
		MemoryIDs []string `json:"memory_ids" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	svc := memory.GetPreflightService()

	// Support batch injection
	if len(req.MemoryIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "memory_ids cannot be empty"})
		return
	}

	if len(req.MemoryIDs) == 1 {
		// Single injection
		err := svc.InjectMemory(req.ContextID, req.MemoryIDs[0])
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	} else {
		// Batch injection
		err := svc.InjectMemories(req.ContextID, req.MemoryIDs)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message":    "Memory injected successfully",
		"context_id": req.ContextID,
		"count":      len(req.MemoryIDs),
	})
}
