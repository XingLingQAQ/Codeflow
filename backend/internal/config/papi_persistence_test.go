// Package config - PAPI persistence tests
package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSQLiteConfigService_PAPIPersistence(t *testing.T) {
	dbPath := "test_papi_config.db"
	defer os.Remove(dbPath)

	svc, err := NewSQLiteConfigService(dbPath)
	assert.NoError(t, err)
	defer svc.Close()

	// Define a PAPI variable
	variable := &PAPIVariable{
		Name:        "TEST_EXPERT",
		Model:       "test-model",
		Temperature: 0.8,
		APIChannel:  "default",
		MCPTools:    []string{"tool1", "tool2"},
		Prompt:      "Test expert prompt",
		Category:    []string{"test", "expert"},
	}

	err = svc.DefinePAPIVariable(variable)
	assert.NoError(t, err)

	// Reload from database
	svc2, err := NewSQLiteConfigService(dbPath)
	assert.NoError(t, err)
	defer svc2.Close()

	// Verify variable was persisted
	loaded, err := svc2.GetPAPIManager().GetVariable("TEST_EXPERT")
	assert.NoError(t, err)
	assert.Equal(t, "TEST_EXPERT", loaded.Name)
	assert.Equal(t, "test-model", loaded.Model)
	assert.Equal(t, 0.8, loaded.Temperature)
	assert.Equal(t, []string{"test", "expert"}, loaded.Category)
}

func TestSQLiteConfigService_PAPIHotSwap(t *testing.T) {
	dbPath := "test_papi_hotswap.db"
	defer os.Remove(dbPath)

	svc, err := NewSQLiteConfigService(dbPath)
	assert.NoError(t, err)
	defer svc.Close()

	// Define original variable
	originalVar := &PAPIVariable{
		Name:        "BACKEND_EXPERT",
		Model:       "original-model",
		Temperature: 0.7,
	}
	err = svc.DefinePAPIVariable(originalVar)
	assert.NoError(t, err)

	// Hot swap
	newVar := &PAPIVariable{
		Model:       "new-model",
		Temperature: 0.9,
	}
	err = svc.HotSwapPAPI("BACKEND_EXPERT", newVar)
	assert.NoError(t, err)

	// Reload and verify
	svc2, err := NewSQLiteConfigService(dbPath)
	assert.NoError(t, err)
	defer svc2.Close()

	loaded, err := svc2.GetPAPIManager().GetVariable("BACKEND_EXPERT")
	assert.NoError(t, err)
	assert.Equal(t, "BACKEND_EXPERT", loaded.Name)
	assert.Equal(t, "new-model", loaded.Model)
	assert.Equal(t, 0.9, loaded.Temperature)
}

func TestSQLiteConfigService_PAPIDelete(t *testing.T) {
	dbPath := "test_papi_delete.db"
	defer os.Remove(dbPath)

	svc, err := NewSQLiteConfigService(dbPath)
	assert.NoError(t, err)
	defer svc.Close()

	// Define variable
	variable := &PAPIVariable{
		Name:  "TO_DELETE",
		Model: "test-model",
	}
	err = svc.DefinePAPIVariable(variable)
	assert.NoError(t, err)

	// Delete variable
	err = svc.DeletePAPIVariable("TO_DELETE")
	assert.NoError(t, err)

	// Reload and verify deletion
	svc2, err := NewSQLiteConfigService(dbPath)
	assert.NoError(t, err)
	defer svc2.Close()

	_, err = svc2.GetPAPIManager().GetVariable("TO_DELETE")
	assert.Error(t, err)
}

func TestSQLiteConfigService_PAPIMultipleVariables(t *testing.T) {
	dbPath := "test_papi_multiple.db"
	defer os.Remove(dbPath)

	svc, err := NewSQLiteConfigService(dbPath)
	assert.NoError(t, err)
	defer svc.Close()

	// Define multiple variables
	variables := []*PAPIVariable{
		{Name: "VAR1", Model: "model1", Category: []string{"cat1"}},
		{Name: "VAR2", Model: "model2", Category: []string{"cat2"}},
		{Name: "VAR3", Model: "model3", Category: []string{"cat3"}},
	}

	for _, v := range variables {
		err = svc.DefinePAPIVariable(v)
		assert.NoError(t, err)
	}

	// Reload and verify all variables
	svc2, err := NewSQLiteConfigService(dbPath)
	assert.NoError(t, err)
	defer svc2.Close()

	loadedVars := svc2.GetPAPIManager().ListVariables()
	assert.Len(t, loadedVars, 3)

	names := make(map[string]bool)
	for _, v := range loadedVars {
		names[v.Name] = true
	}
	assert.True(t, names["VAR1"])
	assert.True(t, names["VAR2"])
	assert.True(t, names["VAR3"])
}

func TestSQLiteConfigService_PAPIResolveByCategory(t *testing.T) {
	dbPath := "test_papi_resolve.db"
	defer os.Remove(dbPath)

	svc, err := NewSQLiteConfigService(dbPath)
	assert.NoError(t, err)
	defer svc.Close()

	// Define variables with categories
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

	err = svc.DefinePAPIVariable(backendVar)
	assert.NoError(t, err)
	err = svc.DefinePAPIVariable(frontendVar)
	assert.NoError(t, err)

	// Reload and test category resolution
	svc2, err := NewSQLiteConfigService(dbPath)
	assert.NoError(t, err)
	defer svc2.Close()

	resolved, err := svc2.GetPAPIManager().ResolveByCategory("backend")
	assert.NoError(t, err)
	assert.Equal(t, "BACKEND_EXPERT", resolved.Name)

	resolved, err = svc2.GetPAPIManager().ResolveByCategory("frontend")
	assert.NoError(t, err)
	assert.Equal(t, "FRONTEND_EXPERT", resolved.Name)
}
