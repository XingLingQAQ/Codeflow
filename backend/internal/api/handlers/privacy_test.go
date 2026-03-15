// Package handlers - Privacy API tests
package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/codeflow/backend/internal/api/middleware"
	"github.com/codeflow/backend/internal/audit"
	"github.com/codeflow/backend/internal/privacy"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func setupPrivacyTestRouter(t *testing.T) (*gin.Engine, *audit.MemoryStorage) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.Trace())

	storage := audit.NewMemoryStorage()
	audit.SetAuditService(audit.NewAuditService(storage))

	// Initialize privacy service
	svc, _ := privacy.NewPrivacyService("test-password", nil)
	privacy.SetPrivacyService(svc)

	t.Cleanup(func() {
		cleanupPrivacyTestState()
	})

	// Setup routes
	v1 := router.Group("/api/v1")
	privacyGroup := v1.Group("/privacy")
	{
		privacyGroup.POST("/encrypt", Encrypt)
		privacyGroup.POST("/decrypt", Decrypt)
		privacyGroup.POST("/redact", Redact)
		privacyGroup.POST("/detect", DetectPII)
		privacyGroup.GET("/keys", GetKeys)
		privacyGroup.POST("/keys", ManageKeys)
		privacyGroup.POST("/verify-chain", VerifyChain)
		privacyGroup.GET("/metrics", GetPrivacyMetrics)
		privacyGroup.POST("/metrics/reset", ResetPrivacyMetrics)
	}

	return router, storage
}

func cleanupPrivacyTestState() {
	audit.SetAuditService(nil)
	privacy.SetPrivacyService(nil)
}

func lastPrivacyAuditEntry(t *testing.T, storage *audit.MemoryStorage) *audit.AuditLogEntry {
	t.Helper()

	entry, err := storage.GetLastEntry(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, audit.EventPrivacy, entry.EventType)
	return entry
}

func assertPrivacyTraceHeaders(t *testing.T, w *httptest.ResponseRecorder, requestID, sessionID, taskID, agentID string) {
	t.Helper()
	assert.Equal(t, requestID, w.Header().Get(middleware.HeaderRequestID))
	assert.Equal(t, sessionID, w.Header().Get(middleware.HeaderSessionID))
	assert.Equal(t, taskID, w.Header().Get(middleware.HeaderTaskID))
	assert.Equal(t, agentID, w.Header().Get(middleware.HeaderAgentID))
}

func setPrivacyTraceHeaders(req *http.Request, requestID, sessionID, taskID, agentID string) {
	req.Header.Set(middleware.HeaderRequestID, requestID)
	req.Header.Set(middleware.HeaderSessionID, sessionID)
	req.Header.Set(middleware.HeaderTaskID, taskID)
	req.Header.Set(middleware.HeaderAgentID, agentID)
}

func assertPrivacyAuditTrace(t *testing.T, entry *audit.AuditLogEntry, requestID, sessionID, taskID, agentID, method, path string) {
	t.Helper()
	if assert.NotNil(t, entry.Trace) {
		assert.Equal(t, requestID, entry.Trace.RequestID)
		assert.Equal(t, sessionID, entry.Trace.SessionID)
		assert.Equal(t, taskID, entry.Trace.TaskID)
		assert.Equal(t, agentID, entry.Trace.AgentID)
		assert.Equal(t, method, entry.Trace.Method)
		assert.Equal(t, path, entry.Trace.Path)
	}
}

func newPrivacyTraceValues() (string, string, string, string) {
	return "req-privacy-001", "session-privacy-001", "task-privacy-001", "agent-privacy-001"
}

func TestEncrypt_Standard(t *testing.T) {
	router, storage := setupPrivacyTestRouter(t)
	requestID, sessionID, taskID, agentID := newPrivacyTraceValues()

	reqBody := EncryptRequest{
		Plaintext: "Hello, World!",
		Method:    "standard",
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/api/v1/privacy/encrypt", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	setPrivacyTraceHeaders(req, requestID, sessionID, taskID, agentID)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assertPrivacyTraceHeaders(t, w, requestID, sessionID, taskID, agentID)

	var response EncryptResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "standard", response.Method)
	assert.NotNil(t, response.EncryptedData)
	assert.NotEmpty(t, response.EncryptedData.Ciphertext)

	entry := lastPrivacyAuditEntry(t, storage)
	assert.Equal(t, audit.OutcomeSuccess, entry.Outcome)
	assert.Equal(t, "encrypt", entry.Action)
	assert.Equal(t, "privacy", entry.Resource.Type)
	assert.Equal(t, "encrypt", entry.Resource.ID)
	assert.Equal(t, "standard", entry.Details["method"])
	assert.Equal(t, string(response.EncryptedData.Algorithm), entry.Details["algorithm"])
	assertPrivacyAuditTrace(t, entry, requestID, sessionID, taskID, agentID, http.MethodPost, "/api/v1/privacy/encrypt")
}

func TestEncrypt_Chain(t *testing.T) {
	router, _ := setupPrivacyTestRouter(t)

	reqBody := EncryptRequest{
		Plaintext: "Secret message",
		Method:    "chain",
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/api/v1/privacy/encrypt", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response EncryptResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "chain", response.Method)
	assert.NotNil(t, response.ChainEncrypted)
	assert.NotEmpty(t, response.ChainEncrypted.NodeID)
}

func TestDecrypt_Standard(t *testing.T) {
	router, _ := setupPrivacyTestRouter(t)

	// First encrypt
	encReqBody := EncryptRequest{
		Plaintext: "Test message",
		Method:    "standard",
	}
	encBody, _ := json.Marshal(encReqBody)

	encReq, _ := http.NewRequest("POST", "/api/v1/privacy/encrypt", bytes.NewBuffer(encBody))
	encReq.Header.Set("Content-Type", "application/json")

	encW := httptest.NewRecorder()
	router.ServeHTTP(encW, encReq)

	var encResponse EncryptResponse
	json.Unmarshal(encW.Body.Bytes(), &encResponse)

	// Then decrypt
	decReqBody := DecryptRequest{
		Method:        "standard",
		EncryptedData: encResponse.EncryptedData,
	}
	decBody, _ := json.Marshal(decReqBody)

	decReq, _ := http.NewRequest("POST", "/api/v1/privacy/decrypt", bytes.NewBuffer(decBody))
	decReq.Header.Set("Content-Type", "application/json")

	decW := httptest.NewRecorder()
	router.ServeHTTP(decW, decReq)

	assert.Equal(t, http.StatusOK, decW.Code)

	var decResponse DecryptResponse
	err := json.Unmarshal(decW.Body.Bytes(), &decResponse)
	assert.NoError(t, err)
	assert.Equal(t, "Test message", decResponse.Plaintext)
	assert.True(t, decResponse.Verified)
}

func TestDecrypt_Chain(t *testing.T) {
	router, _ := setupPrivacyTestRouter(t)

	// First encrypt
	encReqBody := EncryptRequest{
		Plaintext: "Chain test message",
		Method:    "chain",
	}
	encBody, _ := json.Marshal(encReqBody)

	encReq, _ := http.NewRequest("POST", "/api/v1/privacy/encrypt", bytes.NewBuffer(encBody))
	encReq.Header.Set("Content-Type", "application/json")

	encW := httptest.NewRecorder()
	router.ServeHTTP(encW, encReq)

	var encResponse EncryptResponse
	json.Unmarshal(encW.Body.Bytes(), &encResponse)

	// Then decrypt
	decReqBody := DecryptRequest{
		Method:         "chain",
		ChainEncrypted: encResponse.ChainEncrypted,
	}
	decBody, _ := json.Marshal(decReqBody)

	decReq, _ := http.NewRequest("POST", "/api/v1/privacy/decrypt", bytes.NewBuffer(decBody))
	decReq.Header.Set("Content-Type", "application/json")

	decW := httptest.NewRecorder()
	router.ServeHTTP(decW, decReq)

	assert.Equal(t, http.StatusOK, decW.Code)

	var decResponse DecryptResponse
	err := json.Unmarshal(decW.Body.Bytes(), &decResponse)
	assert.NoError(t, err)
	assert.Equal(t, "Chain test message", decResponse.Plaintext)
	assert.True(t, decResponse.Verified)
}

func TestRedact(t *testing.T) {
	router, _ := setupPrivacyTestRouter(t)

	reqBody := RedactRequest{
		Text: "Contact: user@example.com, IP: 192.168.1.1",
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/api/v1/privacy/redact", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response privacy.RedactionResult
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Greater(t, response.RedactedCount, 0)
	assert.NotEqual(t, reqBody.Text, response.RedactedText)
}

func TestDetectPII(t *testing.T) {
	router, _ := setupPrivacyTestRouter(t)

	reqBody := map[string]string{
		"text": "Email: test@example.com",
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/api/v1/privacy/detect", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Greater(t, int(response["count"].(float64)), 0)
}

func TestGetKeys(t *testing.T) {
	router, _ := setupPrivacyTestRouter(t)

	req, _ := http.NewRequest("GET", "/api/v1/privacy/keys", nil)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response, "keys")
	assert.Contains(t, response, "chain_info")
}

func TestManageKeys_Generate(t *testing.T) {
	router, _ := setupPrivacyTestRouter(t)

	reqBody := KeyRequest{
		Action: "generate",
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/api/v1/privacy/keys", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "generate", response["action"])
	assert.Contains(t, response, "key_info")
}

func TestManageKeys_RotateChain(t *testing.T) {
	router, _ := setupPrivacyTestRouter(t)

	reqBody := KeyRequest{
		Action: "rotate_chain",
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/api/v1/privacy/keys", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "rotate_chain", response["action"])
	assert.Contains(t, response, "node")
}

func TestVerifyChain(t *testing.T) {
	router, _ := setupPrivacyTestRouter(t)

	req, _ := http.NewRequest("POST", "/api/v1/privacy/verify-chain", nil)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response privacy.ChainVerificationResult
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.True(t, response.Valid)
}

func TestGetPrivacyMetrics(t *testing.T) {
	router, _ := setupPrivacyTestRouter(t)

	req, _ := http.NewRequest("GET", "/api/v1/privacy/metrics", nil)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response privacy.EncryptionMetrics
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
}

func TestResetPrivacyMetrics(t *testing.T) {
	router, _ := setupPrivacyTestRouter(t)

	req, _ := http.NewRequest("POST", "/api/v1/privacy/metrics/reset", nil)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "metrics reset successfully", response["message"])
}

func TestEncrypt_MissingPlaintext(t *testing.T) {
	router, _ := setupPrivacyTestRouter(t)

	reqBody := map[string]string{
		"method": "standard",
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/api/v1/privacy/encrypt", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDecrypt_MissingEncryptedData(t *testing.T) {
	router, _ := setupPrivacyTestRouter(t)

	reqBody := DecryptRequest{
		Method: "standard",
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/api/v1/privacy/decrypt", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestManageKeys_InvalidAction(t *testing.T) {
	router, storage := setupPrivacyTestRouter(t)
	requestID, sessionID, taskID, agentID := newPrivacyTraceValues()

	reqBody := KeyRequest{
		Action: "invalid",
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/api/v1/privacy/keys", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	setPrivacyTraceHeaders(req, requestID, sessionID, taskID, agentID)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertPrivacyTraceHeaders(t, w, requestID, sessionID, taskID, agentID)

	entry := lastPrivacyAuditEntry(t, storage)
	assert.Equal(t, audit.OutcomeFailure, entry.Outcome)
	assert.Equal(t, audit.SeverityWarning, entry.Severity)
	assert.Equal(t, "manage_keys", entry.Action)
	assert.Equal(t, "privacy", entry.Resource.Type)
	assert.Equal(t, "manage_keys", entry.Resource.ID)
	assert.Equal(t, "invalid", entry.Details["action"])
	assert.Equal(t, "invalid action", entry.Details["error"])
	assertPrivacyAuditTrace(t, entry, requestID, sessionID, taskID, agentID, http.MethodPost, "/api/v1/privacy/keys")
}
