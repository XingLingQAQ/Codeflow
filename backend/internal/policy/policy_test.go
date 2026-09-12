package policy

import (
	"context"
	"testing"

	"github.com/codeflow/backend/internal/audit"
)

func TestEvaluateMissingPolicyFailsClosed(t *testing.T) {
	SetEvaluator(nil)
	RequireEnforcement(true)
	t.Cleanup(func() { SetEvaluator(nil); RequireEnforcement(false) })

	decision := EvaluateBoundary(context.Background(), Request{Operation: OperationOutboundRequest, Resource: "https://example.test"})
	if decision.Allowed || decision.Reason != "policy evaluator is not configured" {
		t.Fatalf("unexpected decision: %+v", decision)
	}
}

func TestDeniedDecisionIsAuditedWithIdentity(t *testing.T) {
	auditSvc := audit.NewAuditService(audit.NewMemoryStorage())
	audit.SetAuditService(auditSvc)
	SetEvaluator(NewFailClosedEvaluator())
	t.Cleanup(func() {
		SetEvaluator(nil)
		audit.SetAuditService(nil)
	})

	decision := Evaluate(context.Background(), Request{
		Operation: OperationPluginInvoke,
		Resource:  "integration-1",
		ProjectID: "project-1",
		AgentID:   "agent-1",
		PluginID:  "plugin-1",
		Context:   map[string]interface{}{"api_key": "must-not-be-audited"},
	})
	if decision.Allowed || decision.AuditID == "" || decision.RuleVersion != RuleVersion {
		t.Fatalf("denial lacks evidence: %+v", decision)
	}
	entry, err := auditSvc.GetByID(context.Background(), decision.AuditID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if entry.Outcome != audit.OutcomeFailure || entry.Details["project_id"] != "project-1" ||
		entry.Details["agent_id"] != "agent-1" || entry.Details["plugin_id"] != "plugin-1" ||
		entry.Details["rule_version"] != RuleVersion {
		t.Fatalf("unexpected audit entry: %+v", entry)
	}
	if _, leaked := entry.Details["api_key"]; leaked {
		t.Fatalf("policy audit leaked context value: %+v", entry.Details)
	}
}

func TestOutboundAuditRedactsURLQuerySecrets(t *testing.T) {
	auditSvc := audit.NewAuditService(audit.NewMemoryStorage())
	audit.SetAuditService(auditSvc)
	SetEvaluator(NewFailClosedEvaluator())
	t.Cleanup(func() {
		SetEvaluator(nil)
		audit.SetAuditService(nil)
	})

	decision := Evaluate(context.Background(), Request{
		Operation: OperationOutboundRequest,
		Resource:  "https://provider.test/v1/generate?key=top-secret&mode=fast",
	})
	if decision.Resource != "https://provider.test/v1/generate" {
		t.Fatalf("query was retained in decision resource: %q", decision.Resource)
	}
	entry, err := auditSvc.GetByID(context.Background(), decision.AuditID)
	if err != nil {
		t.Fatal(err)
	}
	if entry.Resource.ID != decision.Resource || entry.Details["resource"] != decision.Resource {
		t.Fatalf("audit resource was not sanitized: %+v", entry)
	}
}
