// Package handlers - Integration API handlers
package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/integration"
)

// RegisterIntegration registers a governed integration.
// POST /api/v1/integrations
func RegisterIntegration(c *gin.Context) {
	var req integration.RegisterIntegrationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	svc := integration.GetIntegrationService()
	created, err := svc.Register(c.Request.Context(), &req)
	if err != nil {
		writeIntegrationError(c, err)
		return
	}

	c.JSON(http.StatusCreated, created)
}

// ListIntegrations lists registered integrations.
// GET /api/v1/integrations
func ListIntegrations(c *gin.Context) {
	svc := integration.GetIntegrationService()
	items, err := svc.List(c.Request.Context())
	if err != nil {
		writeIntegrationError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"items": items,
		"total": len(items),
	})
}

// GetIntegration gets one integration by ID.
// GET /api/v1/integrations/:id
func GetIntegration(c *gin.Context) {
	svc := integration.GetIntegrationService()
	item, err := svc.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeIntegrationError(c, err)
		return
	}

	c.JSON(http.StatusOK, item)
}

// InvokeIntegration invokes a registered integration.
// POST /api/v1/integrations/:id/invoke
func InvokeIntegration(c *gin.Context) {
	var req integration.InvokeIntegrationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	svc := integration.GetIntegrationService()
	result, err := svc.Invoke(c.Request.Context(), c.Param("id"), &req)
	if err != nil {
		writeIntegrationError(c, err)
		return
	}

	c.JSON(http.StatusOK, result)
}

// ReplayIntegration restores the original snapshot and replays the integration.
// POST /api/v1/integrations/:id/replay
func ReplayIntegration(c *gin.Context) {
	var req integration.ReplayIntegrationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	svc := integration.GetIntegrationService()
	result, err := svc.Replay(c.Request.Context(), c.Param("id"), &req)
	if err != nil {
		writeIntegrationError(c, err)
		return
	}

	c.JSON(http.StatusOK, result)
}

func writeIntegrationError(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, integration.ErrInvalidManifest):
		status = http.StatusBadRequest
	case errors.Is(err, integration.ErrPermissionDenied):
		status = http.StatusForbidden
	case errors.Is(err, integration.ErrIntegrationNotFound), errors.Is(err, integration.ErrInvocationNotFound):
		status = http.StatusNotFound
	case strings.Contains(err.Error(), "service not available"):
		status = http.StatusServiceUnavailable
	case strings.Contains(err.Error(), "hook ") && strings.Contains(err.Error(), "not found"):
		status = http.StatusBadRequest
	}

	c.JSON(status, gin.H{"error": err.Error()})
}
