// Package handlers - Summarize API tests
package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/summarize"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestSummarizeConversation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/v1/summarize/conversation", SummarizeConversation)

	// Setup test data
	svc := summarize.NewSummarizerService()
	summarize.SetSummarizer(svc)

	// Test summarize request
	reqBody := summarize.SummarizeRequest{
		Messages: []summarize.Message{
			{Role: "user", Content: "Let's implement authentication", Timestamp: time.Now()},
			{Role: "assistant", Content: "I'll create the auth module", Timestamp: time.Now()},
		},
		CompressionTarget: 0.6,
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/api/v1/summarize/conversation", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp summarize.ConversationSummary
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Equal(t, 2, resp.OriginalMessages)
	assert.Greater(t, len(resp.SummaryText), 0)
	assert.Greater(t, len(resp.KeyPoints), 0)
}

func TestSummarizeConversation_InvalidRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/v1/summarize/conversation", SummarizeConversation)

	// Invalid JSON
	req, _ := http.NewRequest("POST", "/api/v1/summarize/conversation", bytes.NewBuffer([]byte("invalid")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSummarizeConversation_EmptyMessages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/v1/summarize/conversation", SummarizeConversation)

	svc := summarize.NewSummarizerService()
	summarize.SetSummarizer(svc)

	// Empty messages
	reqBody := summarize.SummarizeRequest{
		Messages: []summarize.Message{},
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/api/v1/summarize/conversation", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestCompressContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/v1/summarize/context", CompressContext)

	svc := summarize.NewSummarizerService()
	summarize.SetSummarizer(svc)

	// Test compress request
	reqBody := summarize.CompressRequest{
		Context:           "This is a long context that needs compression. We discussed many topics and made several decisions.",
		CompressionRatio:  0.6,
		PreserveRecentPct: 0.2,
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/api/v1/summarize/context", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp summarize.ContextCompression
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Greater(t, resp.OriginalTokens, 0)
	assert.Greater(t, resp.CompressedTokens, 0)
	assert.Less(t, resp.CompressedTokens, resp.OriginalTokens)
}

func TestCompressContext_InvalidRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/v1/summarize/context", CompressContext)

	// Invalid JSON
	req, _ := http.NewRequest("POST", "/api/v1/summarize/context", bytes.NewBuffer([]byte("invalid")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCompressContext_EmptyContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/v1/summarize/context", CompressContext)

	svc := summarize.NewSummarizerService()
	summarize.SetSummarizer(svc)

	// Empty context
	reqBody := summarize.CompressRequest{
		Context: "",
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/api/v1/summarize/context", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestCompressContext_CustomRatio(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/v1/summarize/context", CompressContext)

	svc := summarize.NewSummarizerService()
	summarize.SetSummarizer(svc)

	// Custom compression ratio
	reqBody := summarize.CompressRequest{
		Context:           "This is a test context with custom compression ratio settings.",
		CompressionRatio:  0.7,
		PreserveRecentPct: 0.3,
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/api/v1/summarize/context", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp summarize.ContextCompression
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Greater(t, resp.CompressionRatio, 0.0)
}

func TestGetDecisionSkeleton(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/v1/summarize/skeleton", GetDecisionSkeleton)

	svc := summarize.NewSummarizerService()
	summarize.SetSummarizer(svc)

	// Test skeleton request
	reqBody := map[string]interface{}{
		"messages": []summarize.Message{
			{Role: "user", Content: "We decided to use microservices architecture", Timestamp: time.Now()},
			{Role: "assistant", Content: "I'll implement it in backend/internal/auth/service.go", Timestamp: time.Now()},
			{Role: "user", Content: "There's a bug in the login function", Timestamp: time.Now()},
		},
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/api/v1/summarize/skeleton", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp summarize.DecisionSkeleton
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Greater(t, len(resp.ArchitectureDecisions), 0)
	assert.Greater(t, len(resp.UnfixedBugs), 0)
	assert.Greater(t, len(resp.KeyFiles), 0)
}

func TestGetDecisionSkeleton_InvalidRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/v1/summarize/skeleton", GetDecisionSkeleton)

	// Invalid JSON
	req, _ := http.NewRequest("POST", "/api/v1/summarize/skeleton", bytes.NewBuffer([]byte("invalid")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetDecisionSkeleton_EmptyMessages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/v1/summarize/skeleton", GetDecisionSkeleton)

	svc := summarize.NewSummarizerService()
	summarize.SetSummarizer(svc)

	// Empty messages
	reqBody := map[string]interface{}{
		"messages": []summarize.Message{},
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/api/v1/summarize/skeleton", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestGetDecisionSkeleton_MissingMessages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/v1/summarize/skeleton", GetDecisionSkeleton)

	// Missing messages field
	reqBody := map[string]interface{}{}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/api/v1/summarize/skeleton", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
