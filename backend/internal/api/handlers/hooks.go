// Package handlers - Hook API handlers
package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/hooks"
)

// GetHooks returns all registered hooks.
func GetHooks(c *gin.Context) {
	mgr := hooks.GetHookManager()
	hookList := mgr.ListHooks()

	// Convert to response format (exclude handler function)
	response := make([]gin.H, 0, len(hookList))
	for _, hook := range hookList {
		response = append(response, gin.H{
			"name":        hook.Config.Name,
			"type":        hook.Config.Type,
			"enabled":     hook.Config.Enabled,
			"priority":    hook.Config.Priority,
			"timeout":     hook.Config.Timeout.String(),
			"retry_count": hook.Config.RetryCount,
			"metadata":    hook.Config.Metadata,
		})
	}

	c.JSON(http.StatusOK, gin.H{"hooks": response})
}

// GetHook returns a specific hook by name.
func GetHook(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "hook name is required"})
		return
	}

	mgr := hooks.GetHookManager()
	hook, err := mgr.GetHook(name)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"name":        hook.Config.Name,
		"type":        hook.Config.Type,
		"enabled":     hook.Config.Enabled,
		"priority":    hook.Config.Priority,
		"timeout":     hook.Config.Timeout.String(),
		"retry_count": hook.Config.RetryCount,
		"metadata":    hook.Config.Metadata,
	})
}

// UpdateHookConfig updates hook configuration.
func UpdateHookConfig(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "hook name is required"})
		return
	}

	var req hooks.HookConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	mgr := hooks.GetHookManager()
	if err := mgr.UpdateConfig(name, req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "hook config updated successfully"})
}

// EnableHook enables a hook.
func EnableHook(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "hook name is required"})
		return
	}

	mgr := hooks.GetHookManager()
	if err := mgr.Enable(name); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "hook enabled successfully"})
}

// DisableHook disables a hook.
func DisableHook(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "hook name is required"})
		return
	}

	mgr := hooks.GetHookManager()
	if err := mgr.Disable(name); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "hook disabled successfully"})
}

// TriggerHook manually triggers a hook.
func TriggerHook(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "hook name is required"})
		return
	}

	var req struct {
		Payload interface{} `json:"payload"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	mgr := hooks.GetHookManager()
	hook, err := mgr.GetHook(name)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	result, err := mgr.Trigger(c.Request.Context(), hook.Config.Type, req.Payload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"result": result})
}

// GetHookEvents returns hook execution events.
func GetHookEvents(c *gin.Context) {
	limitStr := c.DefaultQuery("limit", "50")
	offsetStr := c.DefaultQuery("offset", "0")
	hookName := c.Query("hook_name")

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 1 {
		limit = 50
	}
	if limit > 1000 {
		limit = 1000
	}

	offset, err := strconv.Atoi(offsetStr)
	if err != nil || offset < 0 {
		offset = 0
	}

	mgr := hooks.GetHookManager()
	var events []*hooks.HookEvent

	if hookName != "" {
		events = mgr.GetEventsByHook(hookName, limit, offset)
	} else {
		events = mgr.GetEvents(limit, offset)
	}

	c.JSON(http.StatusOK, gin.H{
		"events": events,
		"limit":  limit,
		"offset": offset,
		"total":  len(events),
	})
}

// ClearHookEvents clears all hook events.
func ClearHookEvents(c *gin.Context) {
	mgr := hooks.GetHookManager()
	if err := mgr.ClearEvents(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "hook events cleared successfully"})
}
