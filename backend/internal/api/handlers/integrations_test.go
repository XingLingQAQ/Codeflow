// Package handlers - Integration API tests
package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codeflow/backend/internal/audit"
	"github.com/codeflow/backend/internal/hooks"
	"github.com/codeflow/backend/internal/integration"
	"github.com/codeflow/backend/internal/snapshot"
)

func setupIntegrationRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	v1 := router.Group("/api/v1")
	{
		integrations := v1.Group("/integrations")
		{
			integrations.POST("", RegisterIntegration)
			integrations.GET("", ListIntegrations)
			integrations.GET("/:id", GetIntegration)
			integrations.POST("/:id/invoke", InvokeIntegration)
			integrations.POST("/:id/replay", ReplayIntegration)
		}
	}

	return router
}

func setupIntegrationDependencies(t *testing.T) {
	t.Helper()

	snapshot.SetSnapshotService(snapshot.NewInMemorySnapshotService())
	audit.SetAuditService(audit.NewAuditService(audit.NewMemoryStorage()))
	integration.SetIntegrationService(integration.NewInMemoryIntegrationService())

	hookMgr := hooks.NewHookManager()
	require.NoError(t, hookMgr.Register(hooks.HookConfig{
		Name:       "test-hook",
		Type:       hooks.HookBeforeSend,
		Enabled:    true,
		Priority:   1,
		Timeout:    time.Second,
		RetryCount: 0,
	}, func(ctx context.Context, payload interface{}) (interface{}, error) {
		return map[string]interface{}{"processed": true, "payload": payload}, nil
	}))
	hooks.SetHookManager(hookMgr)
}

func integrationRegisterRequestForType(integrationType integration.IntegrationType, distribution integration.DistributionMode, allowThirdParty bool) map[string]interface{} {
	return map[string]interface{}{
		"manifest": map[string]interface{}{
			"name":         "test-" + string(integrationType),
			"version":      "1.0.0",
			"description":  "governed " + string(integrationType) + " integration",
			"type":         string(integrationType),
			"hook_name":    "test-hook",
			"distribution": string(distribution),
			"capabilities": []string{"invoke", "replay"},
			"metadata": map[string]interface{}{
				"entry": string(integrationType),
			},
		},
		"signature": map[string]interface{}{
			"algorithm": "ed25519",
			"value":     "signed-manifest",
			"verified":  true,
		},
		"policy": map[string]interface{}{
			"allowed_actor_types":            []string{"agent"},
			"require_audit":                  true,
			"allow_third_party_distribution": allowThirdParty,
		},
		"actor": map[string]interface{}{
			"id":   "agent-1",
			"type": "agent",
			"name": "Test Agent",
		},
	}
}

func integrationRegisterRequest(distribution integration.DistributionMode, allowThirdParty bool) map[string]interface{} {
	return integrationRegisterRequestForType(integration.IntegrationTypeWebhook, distribution, allowThirdParty)
}

func TestRegisterIntegrationSupportsPluginVCSAndMarketplaceAPI(t *testing.T) {
	router := setupIntegrationRouter()
	setupIntegrationDependencies(t)

	tests := []struct {
		name            string
		integrationType integration.IntegrationType
		distribution    integration.DistributionMode
		allowThirdParty bool
	}{
		{name: "plugin internal", integrationType: integration.IntegrationTypePlugin, distribution: integration.DistributionInternal},
		{name: "vcs internal", integrationType: integration.IntegrationTypeVCS, distribution: integration.DistributionInternal},
		{name: "marketplace third party allowed", integrationType: integration.IntegrationTypeMarketplace, distribution: integration.DistributionThirdParty, allowThirdParty: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(integrationRegisterRequestForType(tt.integrationType, tt.distribution, tt.allowThirdParty))
			req, _ := http.NewRequest("POST", "/api/v1/integrations", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusCreated, w.Code)

			var resp integration.Integration
			err := json.Unmarshal(w.Body.Bytes(), &resp)
			require.NoError(t, err)
			assert.Equal(t, tt.integrationType, resp.Manifest.Type)
			assert.Equal(t, tt.distribution, resp.Manifest.Distribution)
		})
	}
}

func TestRegisterIntegrationRejectsUnknownTypeAPI(t *testing.T) {
	router := setupIntegrationRouter()
	setupIntegrationDependencies(t)

	payload := integrationRegisterRequestForType(integration.IntegrationTypePlugin, integration.DistributionInternal, false)
	manifest := payload["manifest"].(map[string]interface{})
	manifest["type"] = "sidecar"

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", "/api/v1/integrations", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRegisterIntegrationRejectsUnknownDistributionAPI(t *testing.T) {
	router := setupIntegrationRouter()
	setupIntegrationDependencies(t)

	payload := integrationRegisterRequestForType(integration.IntegrationTypePlugin, integration.DistributionInternal, false)
	manifest := payload["manifest"].(map[string]interface{})
	manifest["distribution"] = "partner"

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", "/api/v1/integrations", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRegisterIntegrationAPI(t *testing.T) {
	router := setupIntegrationRouter()
	setupIntegrationDependencies(t)

	body, _ := json.Marshal(integrationRegisterRequest(integration.DistributionInternal, false))
	req, _ := http.NewRequest("POST", "/api/v1/integrations", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp integration.Integration
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.NotEmpty(t, resp.ID)
	assert.Equal(t, "test-webhook", resp.Manifest.Name)
	assert.Equal(t, integration.IntegrationTypeWebhook, resp.Manifest.Type)
}

func TestRegisterIntegrationRejectsThirdPartyWithoutPolicyAPI(t *testing.T) {
	router := setupIntegrationRouter()
	setupIntegrationDependencies(t)

	body, _ := json.Marshal(integrationRegisterRequest(integration.DistributionThirdParty, false))
	req, _ := http.NewRequest("POST", "/api/v1/integrations", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestIntegrationWorkflowAPI(t *testing.T) {
	router := setupIntegrationRouter()
	setupIntegrationDependencies(t)

	registerBody, _ := json.Marshal(integrationRegisterRequest(integration.DistributionInternal, false))
	registerReq, _ := http.NewRequest("POST", "/api/v1/integrations", bytes.NewBuffer(registerBody))
	registerReq.Header.Set("Content-Type", "application/json")
	registerResp := httptest.NewRecorder()
	router.ServeHTTP(registerResp, registerReq)
	require.Equal(t, http.StatusCreated, registerResp.Code)

	var created integration.Integration
	require.NoError(t, json.Unmarshal(registerResp.Body.Bytes(), &created))

	listReq, _ := http.NewRequest("GET", "/api/v1/integrations", nil)
	listResp := httptest.NewRecorder()
	router.ServeHTTP(listResp, listReq)
	require.Equal(t, http.StatusOK, listResp.Code)

	var listed map[string]interface{}
	require.NoError(t, json.Unmarshal(listResp.Body.Bytes(), &listed))
	assert.Equal(t, float64(1), listed["total"])

	getReq, _ := http.NewRequest("GET", "/api/v1/integrations/"+created.ID, nil)
	getResp := httptest.NewRecorder()
	router.ServeHTTP(getResp, getReq)
	require.Equal(t, http.StatusOK, getResp.Code)

	invokeBody, _ := json.Marshal(map[string]interface{}{
		"actor": map[string]interface{}{
			"id":   "agent-1",
			"type": "agent",
			"name": "Invoker",
		},
		"payload": map[string]interface{}{
			"message": "hello integration",
		},
		"session_id":  "session-1",
		"description": "invoke test integration",
		"tags":        []string{"handler-test"},
	})
	invokeReq, _ := http.NewRequest("POST", "/api/v1/integrations/"+created.ID+"/invoke", bytes.NewBuffer(invokeBody))
	invokeReq.Header.Set("Content-Type", "application/json")
	invokeResp := httptest.NewRecorder()
	router.ServeHTTP(invokeResp, invokeReq)
	require.Equal(t, http.StatusOK, invokeResp.Code)

	var invoked integration.InvocationResult
	require.NoError(t, json.Unmarshal(invokeResp.Body.Bytes(), &invoked))
	assert.NotEmpty(t, invoked.InvocationID)
	assert.NotEmpty(t, invoked.SnapshotID)
	assert.NotEmpty(t, invoked.AuditEntryID)

	replayBody, _ := json.Marshal(map[string]interface{}{
		"actor": map[string]interface{}{
			"id":   "agent-1",
			"type": "agent",
			"name": "Invoker",
		},
		"invocation_id": invoked.InvocationID,
	})
	replayReq, _ := http.NewRequest("POST", "/api/v1/integrations/"+created.ID+"/replay", bytes.NewBuffer(replayBody))
	replayReq.Header.Set("Content-Type", "application/json")
	replayResp := httptest.NewRecorder()
	router.ServeHTTP(replayResp, replayReq)
	require.Equal(t, http.StatusOK, replayResp.Code)

	var replayed integration.ReplayResult
	require.NoError(t, json.Unmarshal(replayResp.Body.Bytes(), &replayed))
	assert.Equal(t, invoked.SnapshotID, replayed.RestoredSnapshotID)
	assert.True(t, replayed.Invocation.Replayed)
	assert.Equal(t, invoked.InvocationID, replayed.Invocation.ReplayOf)

	auditResult, err := audit.GetAuditService().Query(context.Background(), &audit.AuditQuery{
		ResourceType: "integration",
		ResourceID:   created.ID,
		Limit:        10,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, auditResult.Total, 3)
}
