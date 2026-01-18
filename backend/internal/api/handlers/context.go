// Package handlers - Context API handlers
package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	ctxsvc "github.com/codeflow/backend/internal/context"
)

// GetFileTree handles GET /api/v1/context/files
func GetFileTree(c *gin.Context) {
	var req ctxsvc.FileTreeRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid query parameters: "+err.Error())
		return
	}

	if req.RootPath == "" {
		req.RootPath = "."
	}

	svc := ctxsvc.GetContextService()
	result, err := svc.GetFileTree(c.Request.Context(), &req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to get file tree: "+err.Error())
		return
	}

	respondOK(c, result)
}

// ParseAST handles POST /api/v1/context/ast
func ParseAST(c *gin.Context) {
	var req ctxsvc.ASTParseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	if req.FilePath == "" && req.Code == "" {
		respondError(c, http.StatusBadRequest, "Either file_path or code is required")
		return
	}

	svc := ctxsvc.GetContextService()
	result, err := svc.ParseAST(c.Request.Context(), &req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to parse AST: "+err.Error())
		return
	}

	respondOK(c, result)
}

// CalculateTokens handles POST /api/v1/context/tokens
func CalculateTokens(c *gin.Context) {
	var req ctxsvc.TokenCalculateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	svc := ctxsvc.GetContextService()
	result, err := svc.CalculateTokens(c.Request.Context(), &req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to calculate tokens: "+err.Error())
		return
	}

	respondOK(c, result)
}

// GetContextPresets handles GET /api/v1/context/presets
func GetContextPresets(c *gin.Context) {
	svc := ctxsvc.GetContextService()
	result, err := svc.ListPresets(c.Request.Context())
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to list presets: "+err.Error())
		return
	}

	respondOK(c, result)
}

// CreateContextPreset handles POST /api/v1/context/presets
func CreateContextPreset(c *gin.Context) {
	var req ctxsvc.PresetCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	svc := ctxsvc.GetContextService()
	result, err := svc.CreatePreset(c.Request.Context(), &req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to create preset: "+err.Error())
		return
	}

	respondCreated(c, result)
}

// DeleteContextPreset handles DELETE /api/v1/context/presets/:id
func DeleteContextPreset(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		respondError(c, http.StatusBadRequest, "Missing preset ID")
		return
	}

	svc := ctxsvc.GetContextService()
	if err := svc.DeletePreset(c.Request.Context(), id); err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to delete preset: "+err.Error())
		return
	}

	respondOK(c, gin.H{"deleted": true, "id": id})
}
