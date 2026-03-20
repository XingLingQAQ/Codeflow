package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	ctxsvc "github.com/codeflow/backend/internal/context"
)

func setupContextRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	v1 := router.Group("/api/v1")
	{
		context := v1.Group("/context")
		{
			context.GET("/presets", GetContextPresets)
			context.POST("/presets", CreateContextPreset)
			context.DELETE("/presets/:id", DeleteContextPreset)
		}
	}

	return router
}

func decodeContextResponseData[T any](t *testing.T, body []byte) T {
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

func TestContextPresetLifecycleAPI(t *testing.T) {
	router := setupContextRouter()
	svc, err := ctxsvc.NewSQLiteContextService(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteContextService failed: %v", err)
	}
	ctxsvc.SetContextService(svc)
	defer func() {
		ctxsvc.SetContextService(nil)
		_ = svc.Close()
	}()

	createBody := map[string]any{
		"name":        "core preset",
		"description": "context api preset",
		"paths":       []string{"backend", "codeflow_template"},
		"extensions":  []string{"go", "ts"},
		"max_tokens":  2048,
	}
	body, _ := json.Marshal(createBody)

	createReq, _ := http.NewRequest("POST", "/api/v1/context/presets", bytes.NewBuffer(body))
	createReq.Header.Set("Content-Type", "application/json")
	createResp := httptest.NewRecorder()
	router.ServeHTTP(createResp, createReq)

	assert.Equal(t, http.StatusCreated, createResp.Code)
	created := decodeContextResponseData[ctxsvc.ContextPreset](t, createResp.Body.Bytes())
	assert.NotEmpty(t, created.ID)
	assert.Equal(t, "core preset", created.Name)

	listReq, _ := http.NewRequest("GET", "/api/v1/context/presets", nil)
	listResp := httptest.NewRecorder()
	router.ServeHTTP(listResp, listReq)

	assert.Equal(t, http.StatusOK, listResp.Code)
	listed := decodeContextResponseData[ctxsvc.PresetListResponse](t, listResp.Body.Bytes())
	assert.Equal(t, 1, listed.Total)
	assert.Len(t, listed.Presets, 1)
	assert.Equal(t, created.ID, listed.Presets[0].ID)
	assert.Equal(t, []string{"backend", "codeflow_template"}, listed.Presets[0].Paths)

	deleteReq, _ := http.NewRequest("DELETE", "/api/v1/context/presets/"+created.ID, nil)
	deleteResp := httptest.NewRecorder()
	router.ServeHTTP(deleteResp, deleteReq)

	assert.Equal(t, http.StatusOK, deleteResp.Code)
	deleted := decodeContextResponseData[map[string]any](t, deleteResp.Body.Bytes())
	assert.Equal(t, true, deleted["deleted"])
	assert.Equal(t, created.ID, deleted["id"])

	emptyResp := httptest.NewRecorder()
	router.ServeHTTP(emptyResp, listReq)
	assert.Equal(t, http.StatusOK, emptyResp.Code)
	empty := decodeContextResponseData[ctxsvc.PresetListResponse](t, emptyResp.Body.Bytes())
	assert.Equal(t, 0, empty.Total)
	assert.Len(t, empty.Presets, 0)
}
