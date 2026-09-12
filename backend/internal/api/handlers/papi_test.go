// Package handlers - PAPI API tests
package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/codeflow/backend/internal/config"
	"github.com/codeflow/backend/internal/dbx"
)

func setupPAPITestRouter() (*gin.Engine, *config.SQLiteConfigService) {
	gin.SetMode(gin.TestMode)

	// Create test database
	dbPath := "test_papi_api.db"
	svc, err := config.NewSQLiteConfigService(dbPath)
	if err != nil {
		panic(err)
	}

	// Set as global service
	config.SetConfigService(svc)

	router := gin.New()
	v1 := router.Group("/api/v1")
	{
		cfg := v1.Group("/config")
		{
			cfg.GET("/papi", GetPAPIVariables)
			cfg.GET("/papi/:name", GetPAPIVariable)
			cfg.POST("/papi", CreatePAPIVariable)
			cfg.PUT("/papi/:name", UpdatePAPIVariable)
			cfg.DELETE("/papi/:name", DeletePAPIVariable)
			cfg.POST("/papi/resolve", ResolvePAPIByCategory)
			cfg.POST("/papi/hotswap", HotSwapPAPI)
			cfg.GET("/papi/conflicts", DetectPAPIConflicts)
		}
	}

	return router, svc
}

func cleanupPAPITest(svc *config.SQLiteConfigService) {
	svc.Close()
	os.Remove("test_papi_api.db")
}

func TestGetPAPIVariables(t *testing.T) {
	router, svc := setupPAPITestRouter()
	defer cleanupPAPITest(svc)

	// Define test variables
	svc.DefinePAPIVariable(&config.PAPIVariable{
		Name:  "TEST_VAR1",
		Model: "model1",
	})
	svc.DefinePAPIVariable(&config.PAPIVariable{
		Name:  "TEST_VAR2",
		Model: "model2",
	})

	req, _ := http.NewRequest("GET", "/api/v1/config/papi", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	variables := response["variables"].([]interface{})
	assert.Len(t, variables, 2)
}

func TestGetPAPIVariable(t *testing.T) {
	router, svc := setupPAPITestRouter()
	defer cleanupPAPITest(svc)

	// Define test variable
	svc.DefinePAPIVariable(&config.PAPIVariable{
		Name:        "BACKEND_EXPERT",
		Model:       "claude-3-5-sonnet",
		Temperature: 0.7,
	})

	req, _ := http.NewRequest("GET", "/api/v1/config/papi/BACKEND_EXPERT", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var variable config.PAPIVariable
	json.Unmarshal(w.Body.Bytes(), &variable)
	assert.Equal(t, "BACKEND_EXPERT", variable.Name)
	assert.Equal(t, "claude-3-5-sonnet", variable.Model)
	assert.Equal(t, 0.7, variable.Temperature)
}

func TestGetPAPIVariable_NotFound(t *testing.T) {
	router, svc := setupPAPITestRouter()
	defer cleanupPAPITest(svc)

	req, _ := http.NewRequest("GET", "/api/v1/config/papi/NONEXISTENT", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestCreatePAPIVariable(t *testing.T) {
	router, svc := setupPAPITestRouter()
	defer cleanupPAPITest(svc)

	variable := config.PAPIVariable{
		Name:        "NEW_VAR",
		Model:       "test-model",
		Temperature: 0.8,
		Category:    []string{"test"},
	}

	body, _ := json.Marshal(variable)
	req, _ := http.NewRequest("POST", "/api/v1/config/papi", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	// Verify variable was created
	retrieved, err := svc.GetPAPIManager().GetVariable("NEW_VAR")
	assert.NoError(t, err)
	assert.Equal(t, "NEW_VAR", retrieved.Name)
	assert.Equal(t, "test-model", retrieved.Model)
}

func TestUpdatePAPIVariable(t *testing.T) {
	router, svc := setupPAPITestRouter()
	defer cleanupPAPITest(svc)

	// Create initial variable
	svc.DefinePAPIVariable(&config.PAPIVariable{
		Name:        "UPDATE_VAR",
		Model:       "original-model",
		Temperature: 0.7,
	})

	// Update variable
	updated := config.PAPIVariable{
		Name:        "UPDATE_VAR",
		Model:       "updated-model",
		Temperature: 0.9,
	}

	body, _ := json.Marshal(updated)
	req, _ := http.NewRequest("PUT", "/api/v1/config/papi/UPDATE_VAR", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Verify update
	retrieved, err := svc.GetPAPIManager().GetVariable("UPDATE_VAR")
	assert.NoError(t, err)
	assert.Equal(t, "updated-model", retrieved.Model)
	assert.Equal(t, 0.9, retrieved.Temperature)
}

func TestUpdatePAPIVariable_NotFound(t *testing.T) {
	router, svc := setupPAPITestRouter()
	defer cleanupPAPITest(svc)

	variable := config.PAPIVariable{
		Name:  "NONEXISTENT",
		Model: "test-model",
	}

	body, _ := json.Marshal(variable)
	req, _ := http.NewRequest("PUT", "/api/v1/config/papi/NONEXISTENT", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestDeletePAPIVariable(t *testing.T) {
	router, svc := setupPAPITestRouter()
	defer cleanupPAPITest(svc)

	// Create variable
	svc.DefinePAPIVariable(&config.PAPIVariable{
		Name:  "DELETE_VAR",
		Model: "test-model",
	})

	req, _ := http.NewRequest("DELETE", "/api/v1/config/papi/DELETE_VAR", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Verify deletion
	_, err := svc.GetPAPIManager().GetVariable("DELETE_VAR")
	assert.Error(t, err)
}

func TestDeletePAPIVariable_NotFound(t *testing.T) {
	router, svc := setupPAPITestRouter()
	defer cleanupPAPITest(svc)

	req, _ := http.NewRequest("DELETE", "/api/v1/config/papi/NONEXISTENT", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestResolvePAPIByCategory(t *testing.T) {
	router, svc := setupPAPITestRouter()
	defer cleanupPAPITest(svc)

	// Define variables with categories
	svc.DefinePAPIVariable(&config.PAPIVariable{
		Name:     "BACKEND_EXPERT",
		Model:    "backend-model",
		Category: []string{"backend", "api"},
	})
	svc.DefinePAPIVariable(&config.PAPIVariable{
		Name:     "FRONTEND_EXPERT",
		Model:    "frontend-model",
		Category: []string{"frontend", "ui"},
	})

	// Test backend category
	reqBody := map[string]string{"category": "backend"}
	body, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", "/api/v1/config/papi/resolve", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var variable config.PAPIVariable
	json.Unmarshal(w.Body.Bytes(), &variable)
	assert.Equal(t, "BACKEND_EXPERT", variable.Name)
	assert.Equal(t, "backend-model", variable.Model)
}

func TestResolvePAPIByCategory_NotFound(t *testing.T) {
	router, svc := setupPAPITestRouter()
	defer cleanupPAPITest(svc)

	reqBody := map[string]string{"category": "nonexistent"}
	body, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", "/api/v1/config/papi/resolve", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHotSwapPAPI(t *testing.T) {
	router, svc := setupPAPITestRouter()
	defer cleanupPAPITest(svc)

	// Create original variable
	svc.DefinePAPIVariable(&config.PAPIVariable{
		Name:        "BACKEND_EXPERT",
		Model:       "original-model",
		Temperature: 0.7,
	})

	// Hot swap
	reqBody := map[string]interface{}{
		"variable_name": "BACKEND_EXPERT",
		"new_variable": map[string]interface{}{
			"model":       "new-model",
			"temperature": 0.9,
		},
	}
	body, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", "/api/v1/config/papi/hotswap", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Verify hot swap
	var variable config.PAPIVariable
	json.Unmarshal(w.Body.Bytes(), &variable)
	assert.Equal(t, "BACKEND_EXPERT", variable.Name)
	assert.Equal(t, "new-model", variable.Model)
	assert.Equal(t, 0.9, variable.Temperature)
}

func TestHotSwapPAPI_NotFound(t *testing.T) {
	router, svc := setupPAPITestRouter()
	defer cleanupPAPITest(svc)

	reqBody := map[string]interface{}{
		"variable_name": "NONEXISTENT",
		"new_variable": map[string]interface{}{
			"model": "new-model",
		},
	}
	body, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", "/api/v1/config/papi/hotswap", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestDetectPAPIConflicts seeds persisted rows from before the category rule
// existed: the current write path rejects conflicting categories, so the
// detection surface is exercised with legacy rows written straight into
// papi_variables (E-10). The endpoint must report the deterministic conflict
// and the load-time diagnostics, keep the legacy rows visible, and never
// delete them across a reopen.
func TestDetectPAPIConflicts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dbPath := filepath.Join(t.TempDir(), "papi_conflicts.db")

	// Establish the schema through the normal constructor, then seed the
	// legacy conflict rows over a raw connection so the service's load path
	// picks them up exactly as pre-rule persisted data.
	seedSvc, err := config.NewSQLiteConfigService(dbPath)
	if err != nil {
		t.Fatalf("create seed service: %v", err)
	}
	if err := seedSvc.Close(); err != nil {
		t.Fatalf("close seed service: %v", err)
	}
	raw, err := dbx.Open(dbPath)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	seed := func(v config.PAPIVariable) {
		payload, err := json.Marshal(&v)
		if err != nil {
			t.Fatalf("marshal seed variable: %v", err)
		}
		if _, err := raw.Exec("INSERT INTO papi_variables (name, variable_json) VALUES (?, ?)", v.Name, string(payload)); err != nil {
			t.Fatalf("seed %s: %v", v.Name, err)
		}
	}
	seed(config.PAPIVariable{Name: "LEGACY_ONE", Model: "m1", Category: []string{"Backend"}})
	seed(config.PAPIVariable{Name: "LEGACY_TWO", Model: "m2", Category: []string{" backend "}})
	seed(config.PAPIVariable{Name: "CLEAN_VAR", Model: "m3", Category: []string{"docs"}})
	if err := raw.Close(); err != nil {
		t.Fatalf("close raw db: %v", err)
	}

	svc, err := config.NewSQLiteConfigService(dbPath)
	if err != nil {
		t.Fatalf("reopen service: %v", err)
	}
	config.SetConfigService(svc)
	defer svc.Close()

	router := gin.New()
	router.GET("/api/v1/config/papi", GetPAPIVariables)
	router.GET("/api/v1/config/papi/conflicts", DetectPAPIConflicts)

	assertConflictsVisible := func() {
		req, _ := http.NewRequest("GET", "/api/v1/config/papi/conflicts", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode conflicts response: %v (body=%s)", err, w.Body.String())
		}
		conflicts, ok := response["conflicts"].([]interface{})
		if !ok || len(conflicts) == 0 {
			t.Fatalf("conflicts must be a non-empty list, body=%s", w.Body.String())
		}
		joined := fmt.Sprint(conflicts)
		assert.Contains(t, joined, "backend")
		assert.Contains(t, joined, "LEGACY_ONE")
		assert.Contains(t, joined, "LEGACY_TWO")

		diagnostics, ok := response["diagnostics"].([]interface{})
		if !ok || len(diagnostics) == 0 {
			t.Fatalf("diagnostics must be a non-empty list, body=%s", w.Body.String())
		}
		joinedDiag := fmt.Sprint(diagnostics)
		assert.Contains(t, joinedDiag, "backend")
		assert.Contains(t, joinedDiag, "LEGACY_ONE")
		assert.Contains(t, joinedDiag, "LEGACY_TWO")

		// The legacy rows stay visible and are not deleted.
		req, _ = http.NewRequest("GET", "/api/v1/config/papi", nil)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		var listResponse map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &listResponse); err != nil {
			t.Fatalf("decode list response: %v (body=%s)", err, w.Body.String())
		}
		variables, ok := listResponse["variables"].([]interface{})
		if !ok {
			t.Fatalf("variables must be a list, body=%s", w.Body.String())
		}
		assert.Len(t, variables, 3)
	}

	assertConflictsVisible()

	// A close/reopen cycle must not repair or drop the legacy conflict data:
	// it remains loadable and reported exactly the same way.
	if err := svc.Close(); err != nil {
		t.Fatalf("close service: %v", err)
	}
	reopened, err := config.NewSQLiteConfigService(dbPath)
	if err != nil {
		t.Fatalf("reopen after cycle: %v", err)
	}
	config.SetConfigService(reopened)
	defer reopened.Close()
	assertConflictsVisible()
}
