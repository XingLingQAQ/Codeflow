// Package handlers - Config API tests
package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/codeflow/backend/internal/config"
)

func setupConfigRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	v1 := router.Group("/api/v1")
	{
		cfg := v1.Group("/config")
		{
			cfg.GET("/global", GetGlobalConfig)
			cfg.PUT("/global", UpdateGlobalConfig)
			cfg.GET("/sessions/:id", GetSessionConfig)
			cfg.PUT("/sessions/:id", UpdateSessionConfig)
			cfg.GET("/roles/:role", GetRoleConfig)
			cfg.PUT("/roles/:role", UpdateRoleConfig)
			cfg.GET("/resolve", ResolveConfig)
		}
	}

	return router
}

func decodeConfigResponseData[T any](t *testing.T, body []byte) T {
	t.Helper()

	var envelope Response
	err := json.Unmarshal(body, &envelope)
	assert.NoError(t, err)
	assert.True(t, envelope.Success)

	raw, err := json.Marshal(envelope.Data)
	assert.NoError(t, err)

	var data T
	err = json.Unmarshal(raw, &data)
	assert.NoError(t, err)
	return data
}

func TestGetGlobalConfigAPI(t *testing.T) {
	router := setupConfigRouter()
	config.SetConfigService(config.NewConfigManager(nil))

	req, _ := http.NewRequest("GET", "/api/v1/config/global", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	_ = decodeConfigResponseData[config.GlobalConfig](t, w.Body.Bytes())
}

func TestUpdateGlobalConfigAPI(t *testing.T) {
	router := setupConfigRouter()
	config.SetConfigService(config.NewConfigManager(nil))

	reqBody := config.GlobalConfig{
		DefaultModel:     "test-model",
		SummaryThreshold: 10000,
		MaxRetries:       5,
		Timeout:          30000,
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("PUT", "/api/v1/config/global", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	resp := decodeConfigResponseData[config.GlobalConfig](t, w.Body.Bytes())
	assert.Equal(t, "test-model", resp.DefaultModel)
	assert.Equal(t, 10000, resp.SummaryThreshold)
}

func TestUpdateGlobalConfigAllowsExplicitZeroValues(t *testing.T) {
	router := setupConfigRouter()
	svc := config.NewConfigManager(nil)
	config.SetConfigService(svc)

	seed := &config.GlobalConfig{
		DefaultModel:     "old-model",
		SummaryThreshold: 20000,
		MaxRetries:       3,
		Timeout:          60000,
	}
	_ = svc.SaveGlobalConfig(seed)

	body := []byte(`{"summary_threshold":0,"max_retries":0,"timeout":0}`)
	req, _ := http.NewRequest("PUT", "/api/v1/config/global", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	updated := decodeConfigResponseData[config.GlobalConfig](t, w.Body.Bytes())
	assert.Equal(t, "old-model", updated.DefaultModel)
	assert.Equal(t, 0, updated.SummaryThreshold)
	assert.Equal(t, 0, updated.MaxRetries)
	assert.Equal(t, 0, updated.Timeout)

	stored := svc.LoadGlobalConfig()
	if assert.NotNil(t, stored) {
		assert.Equal(t, 0, stored.SummaryThreshold)
		assert.Equal(t, 0, stored.MaxRetries)
		assert.Equal(t, 0, stored.Timeout)
	}
}


func TestGetSessionConfigAPI(t *testing.T) {
	router := setupConfigRouter()
	svc := config.NewConfigManager(nil)
	config.SetConfigService(svc)

	// Create a session config first
	temp := 0.8
	sessionCfg := &config.SessionConfig{
		SessionID:     "test-session",
		Mode:          config.ModeDevelopment,
		OverrideModel: "test-model",
		Temperature:   &temp,
	}
	svc.SaveSessionConfig(sessionCfg)

	req, _ := http.NewRequest("GET", "/api/v1/config/sessions/test-session", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	resp := decodeConfigResponseData[config.SessionConfig](t, w.Body.Bytes())
	assert.Equal(t, "test-session", resp.SessionID)
	assert.Equal(t, "test-model", resp.OverrideModel)
}

func TestGetSessionConfigNotFoundAPI(t *testing.T) {
	router := setupConfigRouter()
	config.SetConfigService(config.NewConfigManager(nil))

	req, _ := http.NewRequest("GET", "/api/v1/config/sessions/nonexistent", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestUpdateSessionConfigAPI(t *testing.T) {
	router := setupConfigRouter()
	config.SetConfigService(config.NewConfigManager(nil))

	temp := 0.7
	reqBody := config.SessionConfig{
		Mode:          config.ModeDevelopment,
		OverrideModel: "session-model",
		Temperature:   &temp,
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("PUT", "/api/v1/config/sessions/test-session", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	resp := decodeConfigResponseData[config.SessionConfig](t, w.Body.Bytes())
	assert.Equal(t, "test-session", resp.SessionID)
	assert.Equal(t, "session-model", resp.OverrideModel)
}

func TestUpdateSessionConfigPartialPreservesExistingFieldsAPI(t *testing.T) {
	router := setupConfigRouter()
	svc := config.NewConfigManager(nil)
	config.SetConfigService(svc)

	temp := 0.8
	maxTokens := 2048
	err := svc.SaveSessionConfig(&config.SessionConfig{
		SessionID:     "test-session",
		Mode:          config.ModeDevelopment,
		OverrideModel: "seed-model",
		Temperature:   &temp,
		MaxTokens:     &maxTokens,
	})
	assert.NoError(t, err)

	updatedTemp := 0.3
	body := []byte(`{"temperature":0.3}`)
	req, _ := http.NewRequest("PUT", "/api/v1/config/sessions/test-session", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	resp := decodeConfigResponseData[config.SessionConfig](t, w.Body.Bytes())
	assert.Equal(t, "test-session", resp.SessionID)
	assert.Equal(t, config.ModeDevelopment, resp.Mode)
	assert.Equal(t, "seed-model", resp.OverrideModel)
	if assert.NotNil(t, resp.Temperature) {
		assert.Equal(t, updatedTemp, *resp.Temperature)
	}
	if assert.NotNil(t, resp.MaxTokens) {
		assert.Equal(t, 2048, *resp.MaxTokens)
	}

	stored := svc.LoadSessionConfig("test-session")
	if assert.NotNil(t, stored) {
		assert.Equal(t, config.ModeDevelopment, stored.Mode)
		assert.Equal(t, "seed-model", stored.OverrideModel)
		if assert.NotNil(t, stored.Temperature) {
			assert.Equal(t, updatedTemp, *stored.Temperature)
		}
		if assert.NotNil(t, stored.MaxTokens) {
			assert.Equal(t, 2048, *stored.MaxTokens)
		}
	}
}

func TestGetRoleConfigAPI(t *testing.T) {
	router := setupConfigRouter()
	svc := config.NewConfigManager(nil)
	config.SetConfigService(svc)

	// Create a role config first
	roleCfg := &config.RoleConfig{
		Model:        "test-model",
		Temperature:  0.9,
		APIChannel:   "test-channel",
		MCPTools:     []string{"tool1", "tool2"},
		SystemPrompt: "Test prompt",
	}
	svc.SaveRoleConfig(config.RoleMain, roleCfg)

	req, _ := http.NewRequest("GET", "/api/v1/config/roles/main", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	resp := decodeConfigResponseData[config.RoleConfig](t, w.Body.Bytes())
	assert.Equal(t, "test-model", resp.Model)
	assert.Equal(t, 0.9, resp.Temperature)
}

func TestGetRoleConfigInvalidRoleAPI(t *testing.T) {
	router := setupConfigRouter()
	config.SetConfigService(config.NewConfigManager(nil))

	req, _ := http.NewRequest("GET", "/api/v1/config/roles/InvalidRole", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUpdateRoleConfigAPI(t *testing.T) {
	router := setupConfigRouter()
	config.SetConfigService(config.NewConfigManager(nil))

	reqBody := config.RoleConfig{
		Model:       "role-model",
		Temperature: 0.8,
		APIChannel:  "default",
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("PUT", "/api/v1/config/roles/coder", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	resp := decodeConfigResponseData[config.RoleConfig](t, w.Body.Bytes())
	assert.Equal(t, "role-model", resp.Model)
}

func TestResolveConfigAPI(t *testing.T) {
	router := setupConfigRouter()
	svc := config.NewConfigManager(nil)
	config.SetConfigService(svc)

	// Set up config hierarchy
	globalCfg := &config.GlobalConfig{
		DefaultModel: "global-model",
	}
	svc.SaveGlobalConfig(globalCfg)

	temp := 0.7
	sessionCfg := &config.SessionConfig{
		SessionID:     "test-session",
		OverrideModel: "session-model",
		Temperature:   &temp,
	}
	svc.SaveSessionConfig(sessionCfg)

	roleCfg := &config.RoleConfig{
		Model:       "role-model",
		Temperature: 0.9,
		APIChannel:  "default",
	}
	svc.SaveRoleConfig(config.RoleMain, roleCfg)

	req, _ := http.NewRequest("GET", "/api/v1/config/resolve?session_id=test-session&role=main", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	resp := decodeConfigResponseData[config.ResolvedConfig](t, w.Body.Bytes())
	// Role config should override session and global
	assert.Equal(t, "role-model", resp.Model)
	assert.Equal(t, 0.9, resp.Temperature)
}

func TestConfigChangeNotificationAPI(t *testing.T) {
	router := setupConfigRouter()
	svc := config.NewConfigManager(nil)
	config.SetConfigService(svc)

	notified := false
	unsubscribe := svc.OnConfigChange(func(cfg *config.ConfigHierarchy) {
		notified = true
	})
	defer unsubscribe()

	// Update global config via API
	reqBody := config.GlobalConfig{
		DefaultModel: "new-model",
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("PUT", "/api/v1/config/global", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, notified, "Config change callback should be triggered")
}
