package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/audit"
	"github.com/codeflow/backend/internal/hooks"
	"github.com/codeflow/backend/internal/integration"
	pluginsvc "github.com/codeflow/backend/internal/plugin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupPluginRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	v1 := router.Group("/api/v1")
	{
		plugins := v1.Group("/plugins")
		{
			plugins.GET("", ListPlugins)
			plugins.GET("/marketplace", ListMarketplacePlugins)
			plugins.GET("/:id", GetPlugin)
			plugins.POST("/:id/install", InstallPlugin)
			plugins.PATCH("/:id", TogglePlugin)
		}
	}

	return router
}

func setupPluginDependencies(t *testing.T) {
	t.Helper()

	audit.SetAuditService(audit.NewAuditService(audit.NewMemoryStorage()))
	integration.SetIntegrationService(integration.NewInMemoryIntegrationService())
	pluginsvc.SetPluginService(nil)
	t.Cleanup(func() {
		pluginsvc.SetPluginService(nil)
	})

	hookMgr := hooks.NewHookManager()
	require.NoError(t, hookMgr.Register(hooks.HookConfig{
		Name:       "test-hook",
		Type:       hooks.HookBeforeSend,
		Enabled:    true,
		Priority:   1,
		Timeout:    time.Second,
		RetryCount: 0,
		Metadata:   map[string]interface{}{"lane": "test"},
	}, func(ctx context.Context, payload interface{}) (interface{}, error) {
		return map[string]interface{}{"processed": true, "payload": payload}, nil
	}))
	hooks.SetHookManager(hookMgr)

	svc := integration.GetIntegrationService()
	_, err := svc.Register(context.Background(), &integration.RegisterIntegrationRequest{
		Manifest: integration.Manifest{
			Name:         "installed-plugin",
			Version:      "1.0.0",
			Description:  "installed plugin",
			Type:         integration.IntegrationTypePlugin,
			HookName:     "test-hook",
			Distribution: integration.DistributionInternal,
			Capabilities: []string{"invoke"},
			Metadata: map[string]interface{}{
				"plugin_id":           "plugin.demo",
				"plugin_display_name": "Demo Plugin",
				"plugin_summary":      "Installed summary",
				"plugin_category":     "utility",
				"plugin_source":       "installed",
				"plugin_downloads":    42,
				"plugin_featured":     true,
			},
		},
		Signature: integration.Signature{Algorithm: "ed25519", Value: "sig-plugin", Verified: true},
		Policy: integration.Policy{
			AllowedActorTypes: []string{"agent"},
			RequireAudit:      true,
		},
		Actor: audit.AuditActor{ID: "agent-1", Type: "agent", Name: "Plugin Tester"},
	})
	require.NoError(t, err)

	_, err = svc.Register(context.Background(), &integration.RegisterIntegrationRequest{
		Manifest: integration.Manifest{
			Name:         "marketplace-plugin",
			Version:      "2.0.0",
			Description:  "marketplace plugin",
			Type:         integration.IntegrationTypeMarketplace,
			HookName:     "test-hook",
			Distribution: integration.DistributionInternal,
			Capabilities: []string{"invoke", "replay"},
			Metadata: map[string]interface{}{
				"plugin_id":           "plugin.market",
				"plugin_display_name": "Marketplace Plugin",
				"plugin_summary":      "Marketplace summary",
				"plugin_category":     "market",
				"plugin_source":       "marketplace",
				"plugin_downloads":    128,
				"plugin_featured":     true,
				"plugin_homepage":     "https://example.com/plugin.market",
			},
		},
		Signature: integration.Signature{Algorithm: "ed25519", Value: "sig-market", Verified: true},
		Policy: integration.Policy{
			AllowedActorTypes: []string{"agent"},
			RequireAudit:      true,
		},
		Actor: audit.AuditActor{ID: "agent-1", Type: "agent", Name: "Plugin Tester"},
	})
	require.NoError(t, err)
}

func decodePluginEnvelope[T any](t *testing.T, body []byte) T {
	t.Helper()
	var envelope Response
	require.NoError(t, json.Unmarshal(body, &envelope))
	require.True(t, envelope.Success)
	raw, err := json.Marshal(envelope.Data)
	require.NoError(t, err)
	var data T
	require.NoError(t, json.Unmarshal(raw, &data))
	return data
}

func TestPluginListDetailMarketplaceAndToggleAPI(t *testing.T) {
	router := setupPluginRouter()
	setupPluginDependencies(t)

	listReq, _ := http.NewRequest("GET", "/api/v1/plugins", nil)
	listResp := httptest.NewRecorder()
	router.ServeHTTP(listResp, listReq)
	require.Equal(t, http.StatusOK, listResp.Code)
	listed := decodePluginEnvelope[struct {
		Plugins []map[string]interface{} `json:"plugins"`
		Total   int                      `json:"total"`
	}](t, listResp.Body.Bytes())
	require.Equal(t, 1, listed.Total)
	require.Equal(t, "plugin.demo", listed.Plugins[0]["id"])

	detailReq, _ := http.NewRequest("GET", "/api/v1/plugins/plugin.demo", nil)
	detailResp := httptest.NewRecorder()
	router.ServeHTTP(detailResp, detailReq)
	require.Equal(t, http.StatusOK, detailResp.Code)
	detail := decodePluginEnvelope[struct {
		Plugin map[string]interface{} `json:"plugin"`
	}](t, detailResp.Body.Bytes())
	assert.Equal(t, "Demo Plugin", detail.Plugin["display_name"])
	assert.Equal(t, true, detail.Plugin["enabled"])

	marketReq, _ := http.NewRequest("GET", "/api/v1/plugins/marketplace", nil)
	marketResp := httptest.NewRecorder()
	router.ServeHTTP(marketResp, marketReq)
	require.Equal(t, http.StatusOK, marketResp.Code)
	market := decodePluginEnvelope[struct {
		Plugins []map[string]interface{} `json:"plugins"`
		Total   int                      `json:"total"`
	}](t, marketResp.Body.Bytes())
	require.Equal(t, 1, market.Total)
	assert.Equal(t, "plugin.market", market.Plugins[0]["id"])
	assert.Equal(t, "marketplace", market.Plugins[0]["source"])

	toggleBody := []byte(`{"enabled":false}`)
	toggleReq, _ := http.NewRequest("PATCH", "/api/v1/plugins/plugin.demo", bytes.NewBuffer(toggleBody))
	toggleReq.Header.Set("Content-Type", "application/json")
	toggleResp := httptest.NewRecorder()
	router.ServeHTTP(toggleResp, toggleReq)
	require.Equal(t, http.StatusOK, toggleResp.Code)
	toggled := decodePluginEnvelope[struct {
		Plugin map[string]interface{} `json:"plugin"`
	}](t, toggleResp.Body.Bytes())
	assert.Equal(t, false, toggled.Plugin["enabled"])
	assert.Equal(t, "disabled", toggled.Plugin["health"])
}

func TestInstallPluginAPIRegistersMarketplaceEntryAsInstalledPlugin(t *testing.T) {
	router := setupPluginRouter()
	setupPluginDependencies(t)

	installReq, _ := http.NewRequest("POST", "/api/v1/plugins/plugin.market/install", nil)
	installResp := httptest.NewRecorder()
	router.ServeHTTP(installResp, installReq)
	require.Equal(t, http.StatusOK, installResp.Code)
	installed := decodePluginEnvelope[struct {
		Plugin map[string]interface{} `json:"plugin"`
	}](t, installResp.Body.Bytes())
	assert.Equal(t, "plugin.market", installed.Plugin["id"])
	assert.Equal(t, true, installed.Plugin["installed"])

	listReq, _ := http.NewRequest("GET", "/api/v1/plugins", nil)
	listResp := httptest.NewRecorder()
	router.ServeHTTP(listResp, listReq)
	require.Equal(t, http.StatusOK, listResp.Code)
	listed := decodePluginEnvelope[struct {
		Plugins []map[string]interface{} `json:"plugins"`
		Total   int                      `json:"total"`
	}](t, listResp.Body.Bytes())
	assert.Equal(t, 2, listed.Total)
}

func TestTogglePluginAPIRejectsMissingEnabled(t *testing.T) {
	router := setupPluginRouter()
	setupPluginDependencies(t)

	req, _ := http.NewRequest("PATCH", "/api/v1/plugins/plugin.demo", bytes.NewBuffer([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	assert.Equal(t, http.StatusBadRequest, resp.Code)
}

func TestPluginRoutesRegisteredOnServerRouter(t *testing.T) {
	router := setupPluginRouter()
	setupPluginDependencies(t)

	req, _ := http.NewRequest("GET", "/api/v1/plugins?status=enabled", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	require.Equal(t, http.StatusOK, resp.Code)

	listed := decodePluginEnvelope[struct {
		Plugins []map[string]interface{} `json:"plugins"`
		Total   int                      `json:"total"`
	}](t, resp.Body.Bytes())
	require.Equal(t, 1, listed.Total)
	require.Len(t, listed.Plugins, 1)
	assert.Equal(t, true, listed.Plugins[0]["enabled"])
}
