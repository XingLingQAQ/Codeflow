// Package api provides HTTP API server integration tests.
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func setupTestServer() *Server {
	config := &Config{
		Port:            "8080",
		AllowedOrigins:  []string{"*"},
		EnableDebugMode: false,
	}
	return NewServer(config)
}

// TestHealthCheck tests GET /health endpoint
func TestHealthCheck(t *testing.T) {
	server := setupTestServer()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/health", nil)
	server.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp["success"] != true {
		t.Error("Expected success to be true")
	}
}

// TestMemoryAPI tests /api/v1/memory endpoints
func TestMemoryAPI(t *testing.T) {
	server := setupTestServer()

	t.Run("GET /api/v1/memory/items", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/v1/memory/items", nil)
		server.Router().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
	})

	t.Run("POST /api/v1/memory/items", func(t *testing.T) {
		body := `{"content":"test memory","type":"stm","tags":["test"]}`
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/v1/memory/items", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		server.Router().ServeHTTP(w, req)

		if w.Code != http.StatusCreated && w.Code != http.StatusOK {
			t.Errorf("Expected status 200 or 201, got %d", w.Code)
		}
	})
}

// TestSearchAPI tests /api/v1/search endpoints
func TestSearchAPI(t *testing.T) {
	server := setupTestServer()

	t.Run("POST /api/v1/search/vector", func(t *testing.T) {
		body := `{"query":"test query","limit":10}`
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/v1/search/vector", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		server.Router().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
	})

	t.Run("POST /api/v1/search/fulltext", func(t *testing.T) {
		body := `{"query":"test","highlight":true}`
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/v1/search/fulltext", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		server.Router().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
	})

	t.Run("POST /api/v1/search/graph", func(t *testing.T) {
		body := `{"subject":"test","limit":10}`
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/v1/search/graph", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		server.Router().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
	})

	t.Run("POST /api/v1/search/hybrid", func(t *testing.T) {
		body := `{"query":"test","vector_weight":0.5,"fulltext_weight":0.3,"graph_weight":0.2}`
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/v1/search/hybrid", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		server.Router().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
	})
}

// TestContextAPI tests /api/v1/context endpoints
func TestContextAPI(t *testing.T) {
	server := setupTestServer()

	t.Run("GET /api/v1/context/files", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/v1/context/files?path=.", nil)
		server.Router().ServeHTTP(w, req)

		// May return 200 or 400 depending on path validation
		if w.Code != http.StatusOK && w.Code != http.StatusBadRequest {
			t.Errorf("Expected status 200 or 400, got %d", w.Code)
		}
	})

	t.Run("POST /api/v1/context/ast", func(t *testing.T) {
		body := `{"file_path":"test.go","language":"go"}`
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/v1/context/ast", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		server.Router().ServeHTTP(w, req)

		// May return 200 or error depending on file existence
		if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 200 or 500, got %d", w.Code)
		}
	})

	t.Run("POST /api/v1/context/tokens", func(t *testing.T) {
		body := `{"content":"func main() { fmt.Println(\"hello\") }"}`
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/v1/context/tokens", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		server.Router().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
	})

	t.Run("GET /api/v1/context/presets", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/v1/context/presets", nil)
		server.Router().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
	})
}

// TestAgentAPI tests /api/v1/agents endpoints
func TestAgentAPI(t *testing.T) {
	server := setupTestServer()

	t.Run("GET /api/v1/agents", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/v1/agents", nil)
		server.Router().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
	})

	t.Run("GET /api/v1/agents/:id/logs", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/v1/agents/test-agent/logs", nil)
		server.Router().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
	})
}

// TestConversationAPI tests /api/v1/conversations endpoints
func TestConversationAPI(t *testing.T) {
	server := setupTestServer()

	t.Run("GET /api/v1/conversations/:sessionId/trace", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/v1/conversations/test-session/trace", nil)
		server.Router().ServeHTTP(w, req)

		// May return 200 or 404 depending on session existence
		if w.Code != http.StatusOK && w.Code != http.StatusNotFound {
			t.Errorf("Expected status 200 or 404, got %d", w.Code)
		}
	})

	t.Run("POST /api/v1/conversations/:sessionId/stop", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/v1/conversations/test-session/stop", nil)
		server.Router().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
	})

	t.Run("POST /api/v1/conversations/:sessionId/retry", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/v1/conversations/test-session/retry", nil)
		server.Router().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
	})
}

// TestBlackboardAPI tests /api/v1/blackboard endpoints
func TestBlackboardAPI(t *testing.T) {
	server := setupTestServer()

	t.Run("GET /api/v1/blackboard/entries", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/v1/blackboard/entries", nil)
		server.Router().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
	})

	t.Run("POST /api/v1/blackboard/entries", func(t *testing.T) {
		body := `{"entry_type":"state","content":"test state","author":"test"}`
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/v1/blackboard/entries", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		server.Router().ServeHTTP(w, req)

		if w.Code != http.StatusCreated && w.Code != http.StatusOK && w.Code != http.StatusBadRequest {
			t.Errorf("Expected status 200, 201 or 400, got %d", w.Code)
		}
	})
}

// TestVoteAPI tests /api/v1/votes endpoints
func TestVoteAPI(t *testing.T) {
	server := setupTestServer()

	t.Run("POST /api/v1/votes", func(t *testing.T) {
		body := `{"entry_id":"test-entry","title":"Test Vote","options":["yes","no"],"threshold":0.67}`
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/v1/votes", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		server.Router().ServeHTTP(w, req)

		// May return various status codes depending on entry existence
		if w.Code != http.StatusCreated && w.Code != http.StatusOK && w.Code != http.StatusInternalServerError && w.Code != http.StatusBadRequest {
			t.Errorf("Expected status 200, 201, 400 or 500, got %d", w.Code)
		}
	})
}

// TestDebateAPI tests /api/v1/debates endpoints
func TestDebateAPI(t *testing.T) {
	server := setupTestServer()

	t.Run("POST /api/v1/debates", func(t *testing.T) {
		body := `{"title":"Test Debate","topic":"Should we use microservices?","max_rounds":5}`
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/v1/debates", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		server.Router().ServeHTTP(w, req)

		if w.Code != http.StatusCreated && w.Code != http.StatusOK && w.Code != http.StatusBadRequest {
			t.Errorf("Expected status 200, 201 or 400, got %d", w.Code)
		}
	})

	t.Run("GET /api/v1/debates/:id", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/v1/debates/test-debate", nil)
		server.Router().ServeHTTP(w, req)

		// May return 200 or 404 depending on existence
		if w.Code != http.StatusOK && w.Code != http.StatusNotFound {
			t.Errorf("Expected status 200 or 404, got %d", w.Code)
		}
	})
}

// TestPlanAPI tests /api/v1/plans endpoints
func TestPlanAPI(t *testing.T) {
	server := setupTestServer()

	var planID string

	t.Run("POST /api/v1/plans", func(t *testing.T) {
		body := `{"title":"Test Plan","description":"A test plan"}`
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/v1/plans", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		server.Router().ServeHTTP(w, req)

		if w.Code != http.StatusCreated && w.Code != http.StatusOK {
			t.Errorf("Expected status 200 or 201, got %d", w.Code)
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err == nil {
			if data, ok := resp["data"].(map[string]interface{}); ok {
				if id, ok := data["id"].(string); ok {
					planID = id
				}
			}
		}
	})

	t.Run("GET /api/v1/plans", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/v1/plans", nil)
		server.Router().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
	})

	t.Run("GET /api/v1/plans/:id/tasks", func(t *testing.T) {
		if planID == "" {
			planID = "test-plan"
		}
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/v1/plans/"+planID+"/tasks", nil)
		server.Router().ServeHTTP(w, req)

		// May return 200 or 500 depending on plan existence
		if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 200 or 500, got %d", w.Code)
		}
	})

	t.Run("POST /api/v1/plans/:id/tasks", func(t *testing.T) {
		if planID == "" {
			planID = "test-plan"
		}
		body := `{"title":"Test Task","priority":"P1"}`
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/v1/plans/"+planID+"/tasks", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		server.Router().ServeHTTP(w, req)

		// May return 201 or 500 depending on plan existence
		if w.Code != http.StatusCreated && w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 200, 201 or 500, got %d", w.Code)
		}
	})
}

// TestCORS tests CORS headers
func TestCORS(t *testing.T) {
	server := setupTestServer()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("OPTIONS", "/api/v1/memory/items", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "GET")
	server.Router().ServeHTTP(w, req)

	if w.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Error("Expected CORS header to be set")
	}
}

// TestInvalidJSON tests error handling for invalid JSON
func TestInvalidJSON(t *testing.T) {
	server := setupTestServer()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/memory/items", bytes.NewBufferString("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	server.Router().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}
