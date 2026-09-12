package plugin

import (
	"context"
	"testing"

	"github.com/codeflow/backend/internal/audit"
	"github.com/codeflow/backend/internal/policy"
)

func TestContributionRegistrationDeniedAndAudited(t *testing.T) {
	auditSvc := audit.NewAuditService(audit.NewMemoryStorage())
	audit.SetAuditService(auditSvc)
	policy.SetEvaluator(policy.NewFailClosedEvaluator())
	t.Cleanup(func() {
		policy.SetEvaluator(nil)
		audit.SetAuditService(nil)
	})

	reg := NewContributionRegistry()
	err := reg.RegisterContributionsContext(context.Background(), "plugin.denied", ContributionManifest{})
	if err == nil || len(reg.ListApplied("plugin.denied")) != 0 {
		t.Fatalf("denied plugin contribution was applied: err=%v", err)
	}
	result, queryErr := auditSvc.Query(context.Background(), &audit.AuditQuery{ResourceType: policy.OperationPluginRegister})
	if queryErr != nil || result.Total != 1 || result.Entries[0].Details["plugin_id"] != "plugin.denied" {
		t.Fatalf("missing plugin denial audit: result=%+v err=%v", result, queryErr)
	}
}
