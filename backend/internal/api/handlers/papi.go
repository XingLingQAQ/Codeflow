// Package handlers - PAPI API handlers
package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/config"
)

// GetPAPIVariables returns all PAPI variables.
func GetPAPIVariables(c *gin.Context) {
	sqliteSvc, ok := getSQLiteConfigService(c)
	if !ok {
		return
	}

	variables := sqliteSvc.GetPAPIManager().ListVariables()
	c.JSON(http.StatusOK, gin.H{"variables": variables})
}

// GetPAPIVariable returns a specific PAPI variable.
func GetPAPIVariable(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "variable name is required"})
		return
	}

	sqliteSvc, ok := getSQLiteConfigService(c)
	if !ok {
		return
	}

	variable, err := sqliteSvc.GetPAPIManager().GetVariable(name)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "variable not found"})
		return
	}

	c.JSON(http.StatusOK, variable)
}

// CreatePAPIVariable creates a new PAPI variable.
func CreatePAPIVariable(c *gin.Context) {
	var req config.PAPIVariable
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	sqliteSvc, ok := getSQLiteConfigService(c)
	if !ok {
		return
	}

	if err := sqliteSvc.DefinePAPIVariable(&req); err != nil {
		respondInternalError(c, "create PAPI variable", err)
		return
	}

	c.JSON(http.StatusCreated, req)
}

// UpdatePAPIVariable updates an existing PAPI variable.
func UpdatePAPIVariable(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "variable name is required"})
		return
	}

	var req config.PAPIVariable
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	// Ensure name matches
	req.Name = name

	sqliteSvc, ok := getSQLiteConfigService(c)
	if !ok {
		return
	}

	// Check if variable exists
	if _, err := sqliteSvc.GetPAPIManager().GetVariable(name); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "variable not found"})
		return
	}

	if err := sqliteSvc.DefinePAPIVariable(&req); err != nil {
		respondInternalError(c, "update PAPI variable", err)
		return
	}

	c.JSON(http.StatusOK, req)
}

// DeletePAPIVariable deletes a PAPI variable.
func DeletePAPIVariable(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "variable name is required"})
		return
	}

	sqliteSvc, ok := getSQLiteConfigService(c)
	if !ok {
		return
	}

	if err := sqliteSvc.DeletePAPIVariable(name); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "variable not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "variable deleted successfully"})
}

// ResolvePAPIByCategory resolves a PAPI variable by task category.
func ResolvePAPIByCategory(c *gin.Context) {
	var req struct {
		Category string `json:"category" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	sqliteSvc, ok := getSQLiteConfigService(c)
	if !ok {
		return
	}

	variable, err := sqliteSvc.GetPAPIManager().ResolveByCategory(req.Category)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "variable not found"})
		return
	}

	c.JSON(http.StatusOK, variable)
}

// HotSwapPAPI performs a hot swap of a PAPI variable.
func HotSwapPAPI(c *gin.Context) {
	var req struct {
		VariableName string              `json:"variable_name" binding:"required"`
		NewVariable  config.PAPIVariable `json:"new_variable" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	sqliteSvc, ok := getSQLiteConfigService(c)
	if !ok {
		return
	}

	if err := sqliteSvc.HotSwapPAPI(req.VariableName, &req.NewVariable); err != nil {
		respondInternalError(c, "hot swap PAPI variable", err)
		return
	}

	// Return the updated variable
	updated, _ := sqliteSvc.GetPAPIManager().GetVariable(req.VariableName)
	c.JSON(http.StatusOK, updated)
}

// DetectPAPIConflicts detects conflicts in PAPI variable categories.
func DetectPAPIConflicts(c *gin.Context) {
	sqliteSvc, ok := getSQLiteConfigService(c)
	if !ok {
		return
	}

	conflicts := sqliteSvc.GetPAPIManager().DetectConflicts()
	c.JSON(http.StatusOK, gin.H{"conflicts": conflicts})
}

func getSQLiteConfigService(c *gin.Context) (*config.SQLiteConfigService, bool) {
	svc := config.GetConfigService()
	sqliteSvc, ok := svc.(*config.SQLiteConfigService)
	if !ok {
		respondError(c, http.StatusInternalServerError, "PAPI not supported by current config service")
		return nil, false
	}
	return sqliteSvc, true
}
