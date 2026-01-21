// Package handlers - Privacy API tests
package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/codeflow/backend/internal/privacy"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func setupPrivacyTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Initialize privacy service
	svc, _ := privacy.NewPrivacyService("test-password", nil)
	privacy.SetPrivacyService(svc)

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

	return router
}

func TestEncrypt_Standard(t *testing.T) {
	router := setupPrivacyTestRouter()

	reqBody := EncryptRequest{
		Plaintext: "Hello, World!",
		Method:    "standard",
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
	assert.Equal(t, "standard", response.Method)
	assert.NotNil(t, response.EncryptedData)
	assert.NotEmpty(t, response.EncryptedData.Ciphertext)
}

func TestEncrypt_Chain(t *testing.T) {
	router := setupPrivacyTestRouter()

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
	router := setupPrivacyTestRouter()

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
	router := setupPrivacyTestRouter()

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
	router := setupPrivacyTestRouter()

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
	router := setupPrivacyTestRouter()

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
	router := setupPrivacyTestRouter()

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
	router := setupPrivacyTestRouter()

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
	router := setupPrivacyTestRouter()

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
	router := setupPrivacyTestRouter()

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
	router := setupPrivacyTestRouter()

	req, _ := http.NewRequest("GET", "/api/v1/privacy/metrics", nil)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response privacy.EncryptionMetrics
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
}

func TestResetPrivacyMetrics(t *testing.T) {
	router := setupPrivacyTestRouter()

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
	router := setupPrivacyTestRouter()

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
	router := setupPrivacyTestRouter()

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
	router := setupPrivacyTestRouter()

	reqBody := KeyRequest{
		Action: "invalid",
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/api/v1/privacy/keys", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
