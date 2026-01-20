// Package config - PAPI tests
package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPAPIManager_DefineVariable(t *testing.T) {
	mgr := NewPAPIManager()

	variable := &PAPIVariable{
		Name:        "TEST_VAR",
		Model:       "test-model",
		Temperature: 0.8,
		APIChannel:  "default",
		MCPTools:    []string{"tool1", "tool2"},
		Prompt:      "Test prompt",
		Category:    []string{"test"},
	}

	err := mgr.DefineVariable(variable)
	assert.NoError(t, err)

	// Verify variable was stored
	retrieved, err := mgr.GetVariable("TEST_VAR")
	assert.NoError(t, err)
	assert.Equal(t, "TEST_VAR", retrieved.Name)
	assert.Equal(t, "test-model", retrieved.Model)
	assert.Equal(t, 0.8, retrieved.Temperature)
}

func TestPAPIManager_GetVariable(t *testing.T) {
	mgr := NewPAPIManager()

	variable := &PAPIVariable{
		Name:  "BACKEND_EXPERT",
		Model: "claude-3-5-sonnet",
	}
	mgr.DefineVariable(variable)

	// Test existing variable
	retrieved, err := mgr.GetVariable("BACKEND_EXPERT")
	assert.NoError(t, err)
	assert.Equal(t, "BACKEND_EXPERT", retrieved.Name)

	// Test non-existent variable
	_, err = mgr.GetVariable("NONEXISTENT")
	assert.Error(t, err)
}

func TestPAPIManager_DeleteVariable(t *testing.T) {
	mgr := NewPAPIManager()

	variable := &PAPIVariable{
		Name:  "TO_DELETE",
		Model: "test-model",
	}
	mgr.DefineVariable(variable)

	// Delete existing variable
	deleted := mgr.DeleteVariable("TO_DELETE")
	assert.True(t, deleted)

	// Verify deletion
	_, err := mgr.GetVariable("TO_DELETE")
	assert.Error(t, err)

	// Delete non-existent variable
	deleted = mgr.DeleteVariable("NONEXISTENT")
	assert.False(t, deleted)
}

func TestPAPIManager_ListVariables(t *testing.T) {
	mgr := NewPAPIManager()

	var1 := &PAPIVariable{Name: "VAR1", Model: "model1"}
	var2 := &PAPIVariable{Name: "VAR2", Model: "model2"}

	mgr.DefineVariable(var1)
	mgr.DefineVariable(var2)

	variables := mgr.ListVariables()
	assert.Len(t, variables, 2)

	names := make(map[string]bool)
	for _, v := range variables {
		names[v.Name] = true
	}
	assert.True(t, names["VAR1"])
	assert.True(t, names["VAR2"])
}

func TestPAPIManager_ResolveByCategory(t *testing.T) {
	mgr := NewPAPIManager()

	backendVar := &PAPIVariable{
		Name:     "BACKEND_EXPERT",
		Model:    "backend-model",
		Category: []string{"backend", "api"},
	}
	frontendVar := &PAPIVariable{
		Name:     "FRONTEND_EXPERT",
		Model:    "frontend-model",
		Category: []string{"frontend", "ui"},
	}

	mgr.DefineVariable(backendVar)
	mgr.DefineVariable(frontendVar)

	// Test backend category
	resolved, err := mgr.ResolveByCategory("backend")
	assert.NoError(t, err)
	assert.Equal(t, "BACKEND_EXPERT", resolved.Name)

	// Test frontend category
	resolved, err = mgr.ResolveByCategory("frontend")
	assert.NoError(t, err)
	assert.Equal(t, "FRONTEND_EXPERT", resolved.Name)

	// Test case-insensitive
	resolved, err = mgr.ResolveByCategory("BACKEND")
	assert.NoError(t, err)
	assert.Equal(t, "BACKEND_EXPERT", resolved.Name)

	// Test non-existent category
	_, err = mgr.ResolveByCategory("nonexistent")
	assert.Error(t, err)
}

func TestPAPIManager_ParseVariables(t *testing.T) {
	mgr := NewPAPIManager()

	text := "Use ${BACKEND_EXPERT} for API and ${FRONTEND_EXPERT} for UI. Also ${BACKEND_EXPERT} again."
	variables := mgr.ParseVariables(text)

	assert.Len(t, variables, 2) // Should deduplicate
	assert.Contains(t, variables, "BACKEND_EXPERT")
	assert.Contains(t, variables, "FRONTEND_EXPERT")
}

func TestPAPIManager_ExpandVariables(t *testing.T) {
	mgr := NewPAPIManager()

	backendVar := &PAPIVariable{
		Name:  "BACKEND_EXPERT",
		Model: "claude-3-5-sonnet",
	}
	mgr.DefineVariable(backendVar)

	// Test successful expansion
	text := "Use ${BACKEND_EXPERT} for this task"
	expanded, err := mgr.ExpandVariables(text)
	assert.NoError(t, err)
	assert.Equal(t, "Use claude-3-5-sonnet for this task", expanded)

	// Test undefined variable
	text = "Use ${UNDEFINED_VAR} for this task"
	_, err = mgr.ExpandVariables(text)
	assert.Error(t, err)
}

func TestPAPIManager_HotSwap(t *testing.T) {
	mgr := NewPAPIManager()

	originalVar := &PAPIVariable{
		Name:        "BACKEND_EXPERT",
		Model:       "original-model",
		Temperature: 0.7,
	}
	mgr.DefineVariable(originalVar)

	// Hot swap to new configuration
	newVar := &PAPIVariable{
		Model:       "new-model",
		Temperature: 0.9,
	}
	err := mgr.HotSwap("BACKEND_EXPERT", newVar)
	assert.NoError(t, err)

	// Verify swap
	retrieved, err := mgr.GetVariable("BACKEND_EXPERT")
	assert.NoError(t, err)
	assert.Equal(t, "BACKEND_EXPERT", retrieved.Name) // Name should be preserved
	assert.Equal(t, "new-model", retrieved.Model)
	assert.Equal(t, 0.9, retrieved.Temperature)

	// Test hot swap non-existent variable
	err = mgr.HotSwap("NONEXISTENT", newVar)
	assert.Error(t, err)
}

func TestPAPIManager_ApplyToRoleConfig(t *testing.T) {
	mgr := NewPAPIManager()

	variable := &PAPIVariable{
		Name:        "BACKEND_EXPERT",
		Model:       "claude-3-5-sonnet",
		Temperature: 0.7,
		APIChannel:  "custom-channel",
		MCPTools:    []string{"tool1", "tool2"},
		Prompt:      "Backend expert prompt",
	}
	mgr.DefineVariable(variable)

	roleConfig := &RoleConfig{
		Model:       "original-model",
		Temperature: 1.0,
		APIChannel:  "default",
		MCPTools:    []string{"existing-tool"},
	}

	err := mgr.ApplyToRoleConfig("BACKEND_EXPERT", roleConfig)
	assert.NoError(t, err)

	assert.Equal(t, "claude-3-5-sonnet", roleConfig.Model)
	assert.Equal(t, 0.7, roleConfig.Temperature)
	assert.Equal(t, "custom-channel", roleConfig.APIChannel)
	assert.Contains(t, roleConfig.MCPTools, "tool1")
	assert.Contains(t, roleConfig.MCPTools, "tool2")
	assert.Contains(t, roleConfig.MCPTools, "existing-tool") // Should preserve existing
	assert.Equal(t, "Backend expert prompt", roleConfig.SystemPrompt)
}

func TestPAPIManager_DetectConflicts(t *testing.T) {
	mgr := NewPAPIManager()

	// Define two variables with overlapping categories
	var1 := &PAPIVariable{
		Name:     "VAR1",
		Model:    "model1",
		Category: []string{"backend", "api"},
	}
	var2 := &PAPIVariable{
		Name:     "VAR2",
		Model:    "model2",
		Category: []string{"backend", "database"},
	}

	mgr.DefineVariable(var1)
	mgr.DefineVariable(var2)

	conflicts := mgr.DetectConflicts()
	assert.NotEmpty(t, conflicts)
	assert.Contains(t, conflicts[0], "backend")
}

func TestPAPIManager_GetMapping(t *testing.T) {
	mgr := NewPAPIManager()

	var1 := &PAPIVariable{Name: "VAR1", Model: "model1"}
	var2 := &PAPIVariable{Name: "VAR2", Model: "model2"}

	mgr.DefineVariable(var1)
	mgr.DefineVariable(var2)

	mapping := mgr.GetMapping()
	assert.Len(t, mapping.Variables, 2)
	assert.NotNil(t, mapping.Variables["VAR1"])
	assert.NotNil(t, mapping.Variables["VAR2"])
}

func TestPAPIManager_LoadMapping(t *testing.T) {
	mgr := NewPAPIManager()

	mapping := &PAPIMapping{
		Variables: map[string]*PAPIVariable{
			"VAR1": {Name: "VAR1", Model: "model1"},
			"VAR2": {Name: "VAR2", Model: "model2"},
		},
	}

	err := mgr.LoadMapping(mapping)
	assert.NoError(t, err)

	// Verify loaded variables
	retrieved, err := mgr.GetVariable("VAR1")
	assert.NoError(t, err)
	assert.Equal(t, "VAR1", retrieved.Name)

	retrieved, err = mgr.GetVariable("VAR2")
	assert.NoError(t, err)
	assert.Equal(t, "VAR2", retrieved.Name)
}

func TestDefaultPAPIVariables(t *testing.T) {
	assert.NotEmpty(t, DefaultPAPIVariables)

	// Verify default variables have required fields
	for _, variable := range DefaultPAPIVariables {
		assert.NotEmpty(t, variable.Name)
		assert.NotEmpty(t, variable.Model)
		assert.NotEmpty(t, variable.Category)
	}

	// Verify specific default variables exist
	names := make(map[string]bool)
	for _, v := range DefaultPAPIVariables {
		names[v.Name] = true
	}
	assert.True(t, names["BACKEND_EXPERT"])
	assert.True(t, names["FRONTEND_EXPERT"])
	assert.True(t, names["DEBUGGER"])
	assert.True(t, names["DOC_WRITER"])
}
