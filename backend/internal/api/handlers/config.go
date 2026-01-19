// Package handlers - Config API handlers
package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/config"
)

// GetGlobalConfig returns the global configuration.
func GetGlobalConfig(c *gin.Context) {
	svc := config.GetConfigService()
	cfg := svc.LoadGlobalConfig()
	c.JSON(http.StatusOK, cfg)
}

// UpdateGlobalConfig updates the global configuration.
func UpdateGlobalConfig(c *gin.Context) {
	var req config.GlobalConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	svc := config.GetConfigService()
	if err := svc.SaveGlobalConfig(&req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, req)
}

// GetSessionConfig returns the session configuration.
func GetSessionConfig(c *gin.Context) {
	sessionID := c.Param("id")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session_id is required"})
		return
	}

	svc := config.GetConfigService()
	cfg := svc.LoadSessionConfig(sessionID)
	if cfg == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "session config not found"})
		return
	}

	c.JSON(http.StatusOK, cfg)
}

// UpdateSessionConfig updates the session configuration.
func UpdateSessionConfig(c *gin.Context) {
	sessionID := c.Param("id")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session_id is required"})
		return
	}

	var req config.SessionConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Ensure session ID matches
	req.SessionID = sessionID

	svc := config.GetConfigService()
	if err := svc.SaveSessionConfig(&req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, req)
}

// GetRoleConfig returns the role configuration.
func GetRoleConfig(c *gin.Context) {
	roleStr := c.Param("role")
	if roleStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "role is required"})
		return
	}

	role := config.RoleType(roleStr)
	if role != config.RoleMain && role != config.RoleCoder && role != config.RoleSub {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid role, must be main, coder, or sub"})
		return
	}

	svc := config.GetConfigService()
	cfg := svc.LoadRoleConfig(role)
	if cfg == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "role config not found"})
		return
	}

	c.JSON(http.StatusOK, cfg)
}

// UpdateRoleConfig updates the role configuration.
func UpdateRoleConfig(c *gin.Context) {
	roleStr := c.Param("role")
	if roleStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "role is required"})
		return
	}

	role := config.RoleType(roleStr)
	if role != config.RoleMain && role != config.RoleCoder && role != config.RoleSub {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid role, must be main, coder, or sub"})
		return
	}

	var req config.RoleConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	svc := config.GetConfigService()
	if err := svc.SaveRoleConfig(role, &req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, req)
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid role, must be main, coder, or sub"})
		return
	}

	svc := config.GetConfigService()
	resolved := svc.ResolveConfig(sessionID, role)

	c.JSON(http.StatusOK, resolved)
}
