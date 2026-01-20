// Package handlers - PAPI API performance tests
package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/codeflow/backend/internal/config"
)

func BenchmarkGetPAPIVariables(b *testing.B) {
	gin.SetMode(gin.TestMode)

	dbPath := "bench_papi.db"
	svc, _ := config.NewSQLiteConfigService(dbPath)
	defer func() {
		svc.Close()
		os.Remove(dbPath)
	}()

	config.SetConfigService(svc)

	// Populate with test data
	for i := 0; i < 10; i++ {
		svc.DefinePAPIVariable(&config.PAPIVariable{
			Name:  "VAR_" + string(rune('A'+i)),
			Model: "test-model",
		})
	}

	router := gin.New()
	router.GET("/papi", GetPAPIVariables)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req, _ := http.NewRequest("GET", "/papi", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}

func BenchmarkResolvePAPIByCategory(b *testing.B) {
	gin.SetMode(gin.TestMode)

	dbPath := "bench_papi_resolve.db"
	svc, _ := config.NewSQLiteConfigService(dbPath)
	defer func() {
		svc.Close()
		os.Remove(dbPath)
	}()

	config.SetConfigService(svc)

	svc.DefinePAPIVariable(&config.PAPIVariable{
		Name:     "BACKEND_EXPERT",
		Model:    "backend-model",
		Category: []string{"backend", "api"},
	})

	router := gin.New()
	router.POST("/papi/resolve", ResolvePAPIByCategory)

	reqBody := map[string]string{"category": "backend"}
	body, _ := json.Marshal(reqBody)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req, _ := http.NewRequest("POST", "/papi/resolve", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}

func BenchmarkHotSwapPAPI(b *testing.B) {
	gin.SetMode(gin.TestMode)

	dbPath := "bench_papi_hotswap.db"
	svc, _ := config.NewSQLiteConfigService(dbPath)
	defer func() {
		svc.Close()
		os.Remove(dbPath)
	}()

	config.SetConfigService(svc)

	svc.DefinePAPIVariable(&config.PAPIVariable{
		Name:        "BACKEND_EXPERT",
		Model:       "original-model",
		Temperature: 0.7,
	})

	router := gin.New()
	router.POST("/papi/hotswap", HotSwapPAPI)

	reqBody := map[string]interface{}{
		"variable_name": "BACKEND_EXPERT",
		"new_variable": map[string]interface{}{
			"model":       "new-model",
			"temperature": 0.9,
		},
	}
	body, _ := json.Marshal(reqBody)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req, _ := http.NewRequest("POST", "/papi/hotswap", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}

func TestPAPIAPIPerformance(t *testing.T) {
	gin.SetMode(gin.TestMode)

	dbPath := "test_papi_perf.db"
	svc, _ := config.NewSQLiteConfigService(dbPath)
	defer func() {
		svc.Close()
		os.Remove(dbPath)
	}()

	config.SetConfigService(svc)

	// Populate with test data
	for i := 0; i < 10; i++ {
		svc.DefinePAPIVariable(&config.PAPIVariable{
			Name:     "VAR_" + string(rune('A'+i)),
			Model:    "test-model",
			Category: []string{"test"},
		})
	}

	router := gin.New()
	v1 := router.Group("/api/v1")
	{
		cfg := v1.Group("/config")
		{
			cfg.GET("/papi", GetPAPIVariables)
			cfg.POST("/papi/resolve", ResolvePAPIByCategory)
			cfg.POST("/papi/hotswap", HotSwapPAPI)
		}
	}

	// Test GET /papi performance
	t.Run("GET /papi < 100ms", func(t *testing.T) {
		start := time.Now()
		req, _ := http.NewRequest("GET", "/api/v1/config/papi", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		duration := time.Since(start)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Less(t, duration.Milliseconds(), int64(100), "GET /papi should respond in < 100ms")
		t.Logf("GET /papi: %dms", duration.Milliseconds())
	})

	// Test POST /papi/resolve performance
	t.Run("POST /papi/resolve < 100ms", func(t *testing.T) {
		reqBody := map[string]string{"category": "test"}
		body, _ := json.Marshal(reqBody)

		start := time.Now()
		req, _ := http.NewRequest("POST", "/api/v1/config/papi/resolve", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		duration := time.Since(start)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Less(t, duration.Milliseconds(), int64(100), "POST /papi/resolve should respond in < 100ms")
		t.Logf("POST /papi/resolve: %dms", duration.Milliseconds())
	})

	// Test POST /papi/hotswap performance
	t.Run("POST /papi/hotswap < 100ms", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"variable_name": "VAR_A",
			"new_variable": map[string]interface{}{
				"model":       "new-model",
				"temperature": 0.9,
			},
		}
		body, _ := json.Marshal(reqBody)

		start := time.Now()
		req, _ := http.NewRequest("POST", "/api/v1/config/papi/hotswap", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		duration := time.Since(start)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Less(t, duration.Milliseconds(), int64(100), "POST /papi/hotswap should respond in < 100ms")
		t.Logf("POST /papi/hotswap: %dms", duration.Milliseconds())
	})
}
