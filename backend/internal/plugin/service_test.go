package plugin

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codeflow/backend/internal/audit"
	"github.com/codeflow/backend/internal/hooks"
	"github.com/codeflow/backend/internal/integration"
)

func setupPluginServiceDependencies(t *testing.T) {
	t.Helper()

	audit.SetAuditService(audit.NewAuditService(audit.NewMemoryStorage()))
	integration.SetIntegrationService(integration.NewInMemoryIntegrationService())
	SetPluginService(nil)

	hookMgr := hooks.NewHookManager()
	require.NoError(t, hookMgr.Register(hooks.HookConfig{
		Name:       "plugin-hook",
		Type:       hooks.HookBeforeSend,
		Enabled:    true,
		Priority:   1,
		Timeout:    time.Second,
		RetryCount: 0,
	}, func(ctx context.Context, payload interface{}) (interface{}, error) {
		return payload, nil
	}))
	hooks.SetHookManager(hookMgr)

	svc := integration.GetIntegrationService()
	_, err := svc.Register(context.Background(), &integration.RegisterIntegrationRequest{
		Manifest: integration.Manifest{
			Name:         "installed-plugin",
			Version:      "1.0.0",
			Description:  "installed plugin",
			Type:         integration.IntegrationTypePlugin,
			HookName:     "plugin-hook",
			Distribution: integration.DistributionInternal,
			Capabilities: []string{"invoke"},
			Metadata: map[string]interface{}{
				"plugin_id":           "plugin.demo",
				"plugin_display_name": "Demo Plugin",
				"plugin_summary":      "Installed summary",
				"plugin_source":       "installed",
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
			HookName:     "plugin-hook",
			Distribution: integration.DistributionInternal,
			Capabilities: []string{"invoke", "replay"},
			Metadata: map[string]interface{}{
				"plugin_id":           "plugin.market",
				"plugin_display_name": "Marketplace Plugin",
				"plugin_summary":      "Marketplace summary",
				"plugin_source":       "marketplace",
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

func TestGetPluginServiceCachesLazyDefault(t *testing.T) {
	SetPluginService(nil)

	first := GetPluginService()
	second := GetPluginService()

	assert.Same(t, first, second)
}

func TestStoreListByTypeUsesGovernedIntegrationRegistry(t *testing.T) {
	setupPluginServiceDependencies(t)

	store := NewStore(nil)
	items, err := store.ListByType(context.Background(), integration.IntegrationTypePlugin)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, integration.IntegrationTypePlugin, items[0].Manifest.Type)
}

func TestServiceTogglePreservesDisabledFlagInResponse(t *testing.T) {
	setupPluginServiceDependencies(t)

	service := NewService()
	resp, err := service.Toggle(context.Background(), "plugin.demo", ToggleRequest{
		Enabled: false,
		Actor:   audit.AuditActor{ID: "agent-2", Type: "agent", Name: "Toggle Tester"},
	})
	require.NoError(t, err)
	assert.False(t, resp.Plugin.Enabled)
	assert.False(t, resp.Plugin.Featured)
	assert.Equal(t, "disabled", resp.Plugin.Health)
}
