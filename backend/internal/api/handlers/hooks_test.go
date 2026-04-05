package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/codeflow/backend/internal/hooks"
)

func setupHooksRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	v1 := router.Group("/api/v1")
	{
		hooksGroup := v1.Group("/hooks")
		{
			hooksGroup.GET("", GetHooks)
			hooksGroup.GET("/:name", GetHook)
		}
	}

	return router
}

type hooksListResponse struct {
	Hooks []map[string]interface{} `json:"hooks"`
}

func decodeHooksResponseData[T any](t *testing.T, body []byte) T {
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

func TestGetHooksUsesEnvelope(t *testing.T) {
	mgr := hooks.NewHookManager()
	err := mgr.Register(hooks.HookConfig{
		Name:       "test-hook",
		Type:       hooks.HookBeforeSend,
		Enabled:    true,
		Priority:   10,
		Timeout:    5 * time.Second,
		RetryCount: 0,
	}, func(_ context.Context, payload interface{}) (interface{}, error) {
		return payload, nil
	})
	assert.NoError(t, err)
	hooks.SetHookManager(mgr)
	defer hooks.SetHookManager(nil)

	router := setupHooksRouter()
	req, _ := http.NewRequest("GET", "/api/v1/hooks", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	resp := decodeHooksResponseData[hooksListResponse](t, w.Body.Bytes())
	assert.Len(t, resp.Hooks, 1)
	assert.Equal(t, "test-hook", resp.Hooks[0]["name"])
}
