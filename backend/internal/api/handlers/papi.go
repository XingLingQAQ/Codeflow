// Package handlers - PAPI API handlers
package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/config"
)

// GetPAPIVariables returns all PAPI variables.
func GetPAPIVariables(c *gin.Context) {
	svc := config.GetConfigService()

	// Type assert to SQLiteConfigService to access PAPI methods
	sqliteSvc, ok := svc.(*config.SQLiteConfigService)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "PAPI not supported by current config service"})
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

	svc := config.GetConfigService()
	sqliteSvc, ok := svc.(*config.SQLiteConfigService)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "PAPI not supported by current config service"})
		return
	}

	variable, err := sqliteSvc.GetPAPIManager().GetVariable(name)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, variable)
}

// CreatePAPIVariable creates a new PAPI variable.
func CreatePAPIVariable(c *gin.Context) {
	var req config.PAPIVariable
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	svc := config.GetConfigService()
	sqliteSvc, ok := svc.(*config.SQLiteConfigService)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "PAPI not supported by current config service"})
		return
	}

	if err := sqliteSvc.DefinePAPIVariable(&req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Ensure name matches
	req.Name = name

	svc := config.GetConfigService()
	sqliteSvc, ok := svc.(*config.SQLiteConfigService)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "PAPI not supported by current config service"})
		return
	}

	// Check if variable exists
	if _, err := sqliteSvc.GetPAPIManager().GetVariable(name); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "variable not found"})
		return
	}

	if err := sqliteSvc.DefinePAPIVariable(&req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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

	svc := config.GetConfigService()
	sqliteSvc, ok := svc.(*config.SQLiteConfigService)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "PAPI not supported by current config service"})
		return
	}

	if err := sqliteSvc.DeletePAPIVariable(name); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
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
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	svc := config.GetConfigService()
	sqliteSvc, ok := svc.(*config.SQLiteConfigService)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "PAPI not supported by current config service"})
		return
	}

	variable, err := sqliteSvc.GetPAPIManager().ResolveByCategory(req.Category)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, variable)
}

// HotSwapPAPI performs a hot swap of a PAPI variable.
func HotSwapPAPI(c *gin.Context) {
	var req struct {
		VariableName string                `json:"variable_name" binding:"required"`
		NewVariable  config.PAPIVariable   `json:"new_variable" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	svc := config.GetConfigService()
	sqliteSvc, ok := svc.(*config.SQLiteConfigService)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "PAPI not supported by current config service"})
		return
	}

	if err := sqliteSvc.HotSwapPAPI(req.VariableName, &req.NewVariable); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Return the updated variable
	updated, _ := sqliteSvc.GetPAPIManager().GetVariable(req.VariableName)
	c.JSON(http.StatusOK, updated)
}

// DetectPAPIConflicts detects conflicts in PAPI variable categories.
func DetectPAPIConflicts(c *gin.Context) {
	svc := config.GetConfigService()
	sqliteSvc, ok := svc.(*config.SQLiteConfigService)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "PAPI not supported by current config service"})
		return
	}

	conflicts := sqliteSvc.GetPAPIManager().DetectConflicts()
	c.JSON(http.StatusOK, gin.H{"conflicts": conflicts})
}
