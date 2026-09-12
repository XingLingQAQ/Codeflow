package integration

import (
	"context"
	"testing"

	"github.com/codeflow/backend/internal/audit"
	"github.com/codeflow/backend/internal/policy"
)

func TestPluginInvocationDeniedBySharedPolicy(t *testing.T) {
	setupIntegrationTestDependencies(t)
	policy.SetEvaluator(nil)
	t.Cleanup(func() { policy.SetEvaluator(nil) })
	svc := NewInMemoryIntegrationService()
	req := validRegisterRequestForType(IntegrationTypePlugin, DistributionInternal, false)
	req.Manifest.Metadata["plugin_id"] = "plugin.denied"
	created, err := svc.Register(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	policy.SetEvaluator(policy.NewFailClosedEvaluator())
	_, err = svc.Invoke(context.Background(), created.ID, &InvokeIntegrationRequest{Actor: req.Actor, Payload: "blocked"})
	if err == nil {
		t.Fatal("plugin invocation bypassed shared policy")
	}
	result, queryErr := audit.GetAuditService().Query(context.Background(), &audit.AuditQuery{ResourceType: policy.OperationPluginInvoke})
	if queryErr != nil || result.Total != 1 || result.Entries[0].Details["plugin_id"] != "plugin.denied" {
		t.Fatalf("missing plugin invocation audit: result=%+v err=%v", result, queryErr)
	}
}
