// Package handlers - Context API handlers
package handlers

import (
	"github.com/gin-gonic/gin"
)

// GetFileTree handles GET /api/v1/context/files
func GetFileTree(c *gin.Context) {
	respondNotImplemented(c)
}

// ParseAST handles POST /api/v1/context/ast
func ParseAST(c *gin.Context) {
	respondNotImplemented(c)
}

// CalculateTokens handles POST /api/v1/context/tokens
func CalculateTokens(c *gin.Context) {
	respondNotImplemented(c)
}

// GetContextPresets handles GET /api/v1/context/presets
func GetContextPresets(c *gin.Context) {
	respondNotImplemented(c)
}

// CreateContextPreset handles POST /api/v1/context/presets
func CreateContextPreset(c *gin.Context) {
	respondNotImplemented(c)
}

// DeleteContextPreset handles DELETE /api/v1/context/presets/:id
func DeleteContextPreset(c *gin.Context) {
	respondNotImplemented(c)
}
