// Package handlers - Config API handlers
package handlers

import (
	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/config"
)

// GetGlobalConfig returns the global configuration.
func GetGlobalConfig(c *gin.Context) {
	svc := config.GetConfigService()
	cfg := svc.LoadGlobalConfig()
	respondOK(c, cfg)
}

// UpdateGlobalConfig updates the global configuration.
func UpdateGlobalConfig(c *gin.Context) {
	var req config.GlobalConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, 400, err.Error())
		return
	}

	svc := config.GetConfigService()
	current := svc.LoadGlobalConfig()
	if current == nil {
		current = &config.DefaultGlobalConfig
	}
	merged := mergeGlobalConfig(current, &req)

	if err := svc.SaveGlobalConfig(&merged); err != nil {
		respondError(c, 500, err.Error())
		return
	}

	respondOK(c, merged)
}

func mergeGlobalConfig(current, updates *config.GlobalConfig) config.GlobalConfig {
	merged := *current

	if updates.DefaultModel != "" {
		merged.DefaultModel = updates.DefaultModel
	}
	if updates.APIPool != nil {
		merged.APIPool = updates.APIPool
	}
	if updates.PublicMCP != nil {
		merged.PublicMCP = updates.PublicMCP
	}
	if updates.SummaryThreshold != 0 {
		merged.SummaryThreshold = updates.SummaryThreshold
	}
	if updates.MaxRetries != 0 {
		merged.MaxRetries = updates.MaxRetries
	}
	if updates.Timeout != 0 {
		merged.Timeout = updates.Timeout
	}

	return merged
}

// GetSessionConfig returns the session configuration.
func GetSessionConfig(c *gin.Context) {
	sessionID := c.Param("id")
	if sessionID == "" {
		respondError(c, 400, "session_id is required")
		return
	}

	svc := config.GetConfigService()
	cfg := svc.LoadSessionConfig(sessionID)
	if cfg == nil {
		respondError(c, 404, "session config not found")
		return
	}

	respondOK(c, cfg)
}

// UpdateSessionConfig updates the session configuration.
func UpdateSessionConfig(c *gin.Context) {
	sessionID := c.Param("id")
	if sessionID == "" {
		respondError(c, 400, "session_id is required")
		return
	}

	var req config.SessionConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, 400, err.Error())
		return
	}

	// Ensure session ID matches
	req.SessionID = sessionID

	svc := config.GetConfigService()
	if err := svc.SaveSessionConfig(&req); err != nil {
		respondError(c, 500, err.Error())
		return
	}

	respondOK(c, req)
}

// GetRoleConfig returns the role configuration.
func GetRoleConfig(c *gin.Context) {
	roleStr := c.Param("role")
	if roleStr == "" {
		respondError(c, 400, "role is required")
		return
	}

	role := config.RoleType(roleStr)
	if role != config.RoleMain && role != config.RoleCoder && role != config.RoleSub {
		respondError(c, 400, "invalid role, must be main, coder, or sub")
		return
	}

	svc := config.GetConfigService()
	cfg := svc.LoadRoleConfig(role)
	if cfg == nil {
		respondError(c, 404, "role config not found")
		return
	}

	respondOK(c, cfg)
}

// UpdateRoleConfig updates the role configuration.
func UpdateRoleConfig(c *gin.Context) {
	roleStr := c.Param("role")
	if roleStr == "" {
		respondError(c, 400, "role is required")
		return
	}

	role := config.RoleType(roleStr)
	if role != config.RoleMain && role != config.RoleCoder && role != config.RoleSub {
		respondError(c, 400, "invalid role, must be main, coder, or sub")
		return
	}

	var req config.RoleConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, 400, err.Error())
		return
	}

	svc := config.GetConfigService()
	if err := svc.SaveRoleConfig(role, &req); err != nil {
		respondError(c, 500, err.Error())
		return
	}

	respondOK(c, req)
}

// ResolveConfig resolves the effective configuration for a session and role.
func ResolveConfig(c *gin.Context) {
	sessionID := c.Query("session_id")
	roleStr := c.Query("role")

	if roleStr == "" {
		roleStr = "main"
	}

	role := config.RoleType(roleStr)
	if role != config.RoleMain && role != config.RoleCoder && role != config.RoleSub {
		respondError(c, 400, "invalid role, must be main, coder, or sub")
		return
	}

	svc := config.GetConfigService()
	resolved := svc.ResolveConfig(sessionID, role)

	respondOK(c, resolved)
}
