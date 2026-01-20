// Package handlers - Memory Preflight API tests
package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/codeflow/backend/internal/memory"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func setupTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	return router
}

func TestMemoryPreflight(t *testing.T) {
	router := setupTestRouter()
	router.POST("/api/v1/memory/preflight", MemoryPreflight)

	// Setup test data
	svc := memory.NewMemoryPreflightService()
	svc.AddMemory(&memory.MemoryMatch{
		ID:       "mem1",
		Content:  "Use React hooks for state management",
		Source:   ".claude/rules",
		Strength: memory.MatchStrong,
	})
	memory.SetPreflightService(svc)

	// Test preflight request
	reqBody := map[string]interface{}{
		"query":       "How to use React hooks?",
		"max_results": 10,
		"min_score":   0.3,
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/api/v1/memory/preflight", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp memory.PreflightResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Greater(t, len(resp.Matches), 0)
	assert.Greater(t, len(resp.Keywords), 0)
}

func TestMemoryPreflight_InvalidRequest(t *testing.T) {
	router := setupTestRouter()
	router.POST("/api/v1/memory/preflight", MemoryPreflight)

	// Invalid JSON
	req, _ := http.NewRequest("POST", "/api/v1/memory/preflight", bytes.NewBuffer([]byte("invalid")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetMemorySuggestions(t *testing.T) {
	router := setupTestRouter()
	router.GET("/api/v1/memory/suggestions", GetMemorySuggestions)

	// Setup test data
	svc := memory.NewMemoryPreflightService()
	svc.AddMemory(&memory.MemoryMatch{
		ID:      "mem1",
		Content: "Test memory",
		Source:  ".claude/rules",
	})
	memory.SetPreflightService(svc)

	// Test suggestions request
	req, _ := http.NewRequest("GET", "/api/v1/memory/suggestions?context_id=ctx1&limit=10", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Equal(t, "ctx1", resp["context_id"])
	assert.NotNil(t, resp["suggestions"])
}

func TestGetMemorySuggestions_MissingContextID(t *testing.T) {
	router := setupTestRouter()
	router.GET("/api/v1/memory/suggestions", GetMemorySuggestions)

	// Missing context_id
	req, _ := http.NewRequest("GET", "/api/v1/memory/suggestions", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetMemorySuggestions_DefaultLimit(t *testing.T) {
	router := setupTestRouter()
	router.GET("/api/v1/memory/suggestions", GetMemorySuggestions)

	svc := memory.NewMemoryPreflightService()
	memory.SetPreflightService(svc)

	// No limit specified, should default to 10
	req, _ := http.NewRequest("GET", "/api/v1/memory/suggestions?context_id=ctx1", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Equal(t, float64(10), resp["limit"])
}

func TestInjectMemory_Single(t *testing.T) {
	router := setupTestRouter()
	router.POST("/api/v1/memory/inject", InjectMemory)

	// Setup test data
	svc := memory.NewMemoryPreflightService()
	svc.AddMemory(&memory.MemoryMatch{
		ID:      "mem1",
		Content: "Test memory",
		Source:  ".claude/rules",
	})
	memory.SetPreflightService(svc)

	// Test single injection
	reqBody := map[string]interface{}{
		"context_id": "ctx1",
		"memory_ids": []string{"mem1"},
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/api/v1/memory/inject", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Equal(t, "ctx1", resp["context_id"])
	assert.Equal(t, float64(1), resp["count"])
}

func TestInjectMemory_Batch(t *testing.T) {
	router := setupTestRouter()
	router.POST("/api/v1/memory/inject", InjectMemory)

	// Setup test data
	svc := memory.NewMemoryPreflightService()
	svc.AddMemory(&memory.MemoryMatch{ID: "mem1", Content: "Memory 1", Source: ".claude/rules"})
	svc.AddMemory(&memory.MemoryMatch{ID: "mem2", Content: "Memory 2", Source: ".claude/rules"})
	memory.SetPreflightService(svc)

	// Test batch injection
	reqBody := map[string]interface{}{
		"context_id": "ctx1",
		"memory_ids": []string{"mem1", "mem2"},
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/api/v1/memory/inject", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Equal(t, float64(2), resp["count"])
}

func TestInjectMemory_InvalidRequest(t *testing.T) {
	router := setupTestRouter()
	router.POST("/api/v1/memory/inject", InjectMemory)

	// Missing required fields
	reqBody := map[string]interface{}{
		"context_id": "ctx1",
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/api/v1/memory/inject", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestInjectMemory_EmptyMemoryIDs(t *testing.T) {
	router := setupTestRouter()
	router.POST("/api/v1/memory/inject", InjectMemory)

	svc := memory.NewMemoryPreflightService()
	memory.SetPreflightService(svc)

	// Empty memory_ids
	reqBody := map[string]interface{}{
		"context_id": "ctx1",
		"memory_ids": []string{},
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/api/v1/memory/inject", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestInjectMemory_NotFound(t *testing.T) {
	router := setupTestRouter()
	router.POST("/api/v1/memory/inject", InjectMemory)

	svc := memory.NewMemoryPreflightService()
	memory.SetPreflightService(svc)

	// Non-existent memory ID
	reqBody := map[string]interface{}{
		"context_id": "ctx1",
		"memory_ids": []string{"nonexistent"},
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/api/v1/memory/inject", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
