package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/codeflow/backend/internal/audit"
	"github.com/codeflow/backend/internal/integration"
	pluginsvc "github.com/codeflow/backend/internal/plugin"
	"github.com/gin-gonic/gin"
)

// ListPlugins handles GET /api/v1/plugins.
func ListPlugins(c *gin.Context) {
	service := pluginsvc.GetPluginService()
	data, err := service.ListInstalled(c.Request.Context(), readPluginListParams(c))
	if err != nil {
		writePluginError(c, err)
		return
	}
	respondOK(c, data)
}

// GetPlugin handles GET /api/v1/plugins/:id.
func GetPlugin(c *gin.Context) {
	pluginID := c.Param("id")
	if pluginID == "" {
		respondError(c, http.StatusBadRequest, "plugin id is required")
		return
	}

	service := pluginsvc.GetPluginService()
	data, err := service.Get(c.Request.Context(), pluginID)
	if err != nil {
		writePluginError(c, err)
		return
	}
	respondOK(c, data)
}

// ListMarketplacePlugins handles GET /api/v1/plugins/marketplace.
func ListMarketplacePlugins(c *gin.Context) {
	service := pluginsvc.GetPluginService()
	data, err := service.ListMarketplace(c.Request.Context(), readPluginListParams(c))
	if err != nil {
		writePluginError(c, err)
		return
	}
	respondOK(c, data)
}

// InstallPlugin handles POST /api/v1/plugins/:id/install.
func InstallPlugin(c *gin.Context) {
	pluginID := c.Param("id")
	if pluginID == "" {
		respondError(c, http.StatusBadRequest, "plugin id is required")
		return
	}

	service := pluginsvc.GetPluginService()
	data, err := service.Install(c.Request.Context(), pluginID, pluginActorFromContext(c))
	if err != nil {
		writePluginError(c, err)
		return
	}
	respondOK(c, data)
}

// TogglePlugin handles PATCH /api/v1/plugins/:id.
func TogglePlugin(c *gin.Context) {
	pluginID := c.Param("id")
	if pluginID == "" {
		respondError(c, http.StatusBadRequest, "plugin id is required")
		return
	}

	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.Enabled == nil {
		respondError(c, http.StatusBadRequest, "enabled is required")
		return
	}

	service := pluginsvc.GetPluginService()
	data, err := service.Toggle(c.Request.Context(), pluginID, pluginsvc.ToggleRequest{
		Enabled: *req.Enabled,
		Actor:   pluginActorFromContext(c),
	})
	if err != nil {
		writePluginError(c, err)
		return
	}
	respondOK(c, data)
}

func readPluginListParams(c *gin.Context) pluginsvc.ListParams {
	return pluginsvc.ListParams{
		Scope:  strings.TrimSpace(c.Query("scope")),
		Status: strings.TrimSpace(c.Query("status")),
		Search: strings.TrimSpace(c.Query("search")),
		Limit:  parsePluginInt(c.Query("limit")),
		Offset: parsePluginInt(c.Query("offset")),
	}
}

func parsePluginInt(raw string) int {
	if strings.TrimSpace(raw) == "" {
		return 0
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return value
}

func pluginActorFromContext(c *gin.Context) audit.AuditActor {
	return audit.EnrichActorFromContext(c.Request.Context(), audit.AuditActor{
		Name: "Plugin API",
		Type: "agent",
	})
}

func writePluginError(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, integration.ErrInvalidManifest):
		status = http.StatusBadRequest
	case errors.Is(err, integration.ErrPermissionDenied):
		status = http.StatusForbidden
	case errors.Is(err, integration.ErrIntegrationNotFound):
		status = http.StatusNotFound
	case strings.Contains(err.Error(), "service not available"):
		status = http.StatusServiceUnavailable
	case strings.Contains(err.Error(), "hook ") && strings.Contains(err.Error(), "not found"):
		status = http.StatusNotFound
	}
	respondError(c, status, err.Error())
}
