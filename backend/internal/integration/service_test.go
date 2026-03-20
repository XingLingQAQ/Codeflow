// Package integration - Governed integration service tests
package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codeflow/backend/internal/audit"
	"github.com/codeflow/backend/internal/hooks"
	"github.com/codeflow/backend/internal/snapshot"
)

func setupIntegrationTestDependencies(t *testing.T) {
	t.Helper()

	audit.SetAuditService(audit.NewAuditService(audit.NewMemoryStorage()))
	snapshot.SetSnapshotService(snapshot.NewInMemorySnapshotService())

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

func validRegisterRequestForType(integrationType IntegrationType, distribution DistributionMode, allowThirdParty bool) *RegisterIntegrationRequest {
	return &RegisterIntegrationRequest{
		Manifest: Manifest{
			Name:         "test-" + string(integrationType),
			Version:      "1.0.0",
			Description:  "governed " + string(integrationType) + " integration",
			Type:         integrationType,
			HookName:     "test-hook",
			Distribution: distribution,
			Capabilities: []string{"invoke", "replay"},
			Metadata: map[string]interface{}{
				"entry": string(integrationType),
			},
		},
		Signature: Signature{
			Algorithm: "ed25519",
			Value:     "signed-manifest",
			Verified:  true,
		},
		Policy: Policy{
			AllowedActorTypes:           []string{"agent"},
			RequireAudit:                true,
			AllowThirdPartyDistribution: allowThirdParty,
		},
		Actor: audit.AuditActor{
			ID:   "agent-1",
			Type: "agent",
			Name: "Integration Agent",
		},
	}
}

func validRegisterRequest(distribution DistributionMode, allowThirdParty bool) *RegisterIntegrationRequest {
	return validRegisterRequestForType(IntegrationTypeWebhook, distribution, allowThirdParty)
}

func TestInMemoryIntegrationService_RegisterInvokeReplay(t *testing.T) {
	setupIntegrationTestDependencies(t)
	svc := NewInMemoryIntegrationService()

	created, err := svc.Register(context.Background(), validRegisterRequest(DistributionInternal, false))
	require.NoError(t, err)
	assert.NotEmpty(t, created.ID)

	invoked, err := svc.Invoke(context.Background(), created.ID, &InvokeIntegrationRequest{
		Actor: audit.AuditActor{ID: "agent-1", Type: "agent", Name: "Invoker"},
		Payload: map[string]interface{}{
			"message": "hello integration",
		},
		SessionID:   "session-1",
		Description: "invoke test integration",
		Tags:        []string{"service-test"},
	})
	require.NoError(t, err)
	assert.NotEmpty(t, invoked.InvocationID)
	assert.NotEmpty(t, invoked.SnapshotID)
	assert.NotEmpty(t, invoked.AuditEntryID)

	replayed, err := svc.Replay(context.Background(), created.ID, &ReplayIntegrationRequest{
		Actor:        audit.AuditActor{ID: "agent-1", Type: "agent", Name: "Invoker"},
		InvocationID: invoked.InvocationID,
	})
	require.NoError(t, err)
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

func TestInMemoryIntegrationService_SupportsGovernedPluginVCSAndMarketplace(t *testing.T) {
	setupIntegrationTestDependencies(t)
	svc := NewInMemoryIntegrationService()

	tests := []struct {
		name            string
		integrationType IntegrationType
		distribution    DistributionMode
		allowThirdParty bool
	}{
		{name: "plugin internal", integrationType: IntegrationTypePlugin, distribution: DistributionInternal},
		{name: "vcs third party allowed", integrationType: IntegrationTypeVCS, distribution: DistributionThirdParty, allowThirdParty: true},
		{name: "marketplace internal", integrationType: IntegrationTypeMarketplace, distribution: DistributionInternal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			created, err := svc.Register(context.Background(), validRegisterRequestForType(tt.integrationType, tt.distribution, tt.allowThirdParty))
			require.NoError(t, err)
			assert.Equal(t, tt.integrationType, created.Manifest.Type)
			assert.Equal(t, tt.distribution, created.Manifest.Distribution)
			assert.Equal(t, "test-hook", created.Manifest.HookName)

			invoked, err := svc.Invoke(context.Background(), created.ID, &InvokeIntegrationRequest{
				Actor: audit.AuditActor{ID: "agent-1", Type: "agent", Name: "Invoker"},
				Payload: map[string]interface{}{
					"message": string(tt.integrationType),
				},
				SessionID:   "session-" + string(tt.integrationType),
				Description: "invoke " + string(tt.integrationType),
				Tags:        []string{"service-test", string(tt.integrationType)},
			})
			require.NoError(t, err)
			assert.NotEmpty(t, invoked.InvocationID)
			assert.NotEmpty(t, invoked.AuditEntryID)

			replayed, err := svc.Replay(context.Background(), created.ID, &ReplayIntegrationRequest{
				Actor:        audit.AuditActor{ID: "agent-1", Type: "agent", Name: "Invoker"},
				InvocationID: invoked.InvocationID,
			})
			require.NoError(t, err)
			assert.True(t, replayed.Invocation.Replayed)
			assert.Equal(t, invoked.InvocationID, replayed.Invocation.ReplayOf)

			auditResult, err := audit.GetAuditService().Query(context.Background(), &audit.AuditQuery{
				ResourceType: "integration",
				ResourceID:   created.ID,
				Limit:        10,
			})
			require.NoError(t, err)
			assert.GreaterOrEqual(t, auditResult.Total, 3)
		})
	}
}

func TestInMemoryIntegrationService_RejectsUnknownIntegrationType(t *testing.T) {
	setupIntegrationTestDependencies(t)
	svc := NewInMemoryIntegrationService()

	req := validRegisterRequestForType(IntegrationType("sidecar"), DistributionInternal, false)
	_, err := svc.Register(context.Background(), req)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidManifest)
}

func TestInMemoryIntegrationService_RejectsUnknownDistributionMode(t *testing.T) {
	setupIntegrationTestDependencies(t)
	svc := NewInMemoryIntegrationService()

	req := validRegisterRequestForType(IntegrationTypePlugin, DistributionMode("partner"), true)
	_, err := svc.Register(context.Background(), req)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidManifest)
}

func TestInMemoryIntegrationService_RejectsUngovernedThirdParty(t *testing.T) {
	setupIntegrationTestDependencies(t)
	svc := NewInMemoryIntegrationService()

	_, err := svc.Register(context.Background(), validRegisterRequest(DistributionThirdParty, false))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPermissionDenied)
}

func TestGetIntegrationService_CachesLazyDefault(t *testing.T) {
	SetIntegrationService(nil)

	first := GetIntegrationService()
	second := GetIntegrationService()

	assert.Same(t, first, second)
}
