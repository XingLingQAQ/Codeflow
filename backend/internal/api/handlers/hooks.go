// Package handlers - Hook API handlers
package handlers

import (
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

	respondOK(c, gin.H{"hooks": response})
}

// GetHook returns a specific hook by name.
func GetHook(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		respondError(c, 400, "hook name is required")
		return
	}

	mgr := hooks.GetHookManager()
	hook, err := mgr.GetHook(name)
	if err != nil {
		respondError(c, 404, err.Error())
		return
	}

	respondOK(c, gin.H{
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
		respondError(c, 400, "hook name is required")
		return
	}

	var req hooks.HookConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, 400, err.Error())
		return
	}

	mgr := hooks.GetHookManager()
	if err := mgr.UpdateConfig(name, req); err != nil {
		respondError(c, 500, err.Error())
		return
	}

	respondOK(c, gin.H{"message": "hook config updated successfully"})
}

// EnableHook enables a hook.
func EnableHook(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		respondError(c, 400, "hook name is required")
		return
	}

	mgr := hooks.GetHookManager()
	if err := mgr.Enable(name); err != nil {
		respondError(c, 404, err.Error())
		return
	}

	respondOK(c, gin.H{"message": "hook enabled successfully"})
}

// DisableHook disables a hook.
func DisableHook(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		respondError(c, 400, "hook name is required")
		return
	}

	mgr := hooks.GetHookManager()
	if err := mgr.Disable(name); err != nil {
		respondError(c, 404, err.Error())
		return
	}

	respondOK(c, gin.H{"message": "hook disabled successfully"})
}

// TriggerHook manually triggers a hook.
func TriggerHook(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		respondError(c, 400, "hook name is required")
		return
	}

	var req struct {
		Payload interface{} `json:"payload"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, 400, err.Error())
		return
	}

	mgr := hooks.GetHookManager()
	if _, err := mgr.GetHook(name); err != nil {
		respondError(c, 404, err.Error())
		return
	}

	result, err := mgr.TriggerHook(c.Request.Context(), name, req.Payload)
	if err != nil {
		respondError(c, 500, err.Error())
		return
	}

	respondOK(c, gin.H{"result": result})
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

	respondOK(c, gin.H{
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
		respondError(c, 500, err.Error())
		return
	}

	respondOK(c, gin.H{"message": "hook events cleared successfully"})
}
