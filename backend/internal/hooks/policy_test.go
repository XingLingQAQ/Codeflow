package hooks

import (
	"context"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/audit"
	"github.com/codeflow/backend/internal/policy"
)

func TestHookExecutionDeniedAndAudited(t *testing.T) {
	auditSvc := audit.NewAuditService(audit.NewMemoryStorage())
	audit.SetAuditService(auditSvc)
	policy.SetEvaluator(policy.NewFailClosedEvaluator())
	t.Cleanup(func() {
		policy.SetEvaluator(nil)
		audit.SetAuditService(nil)
	})

	executed := false
	mgr := NewHookManager()
	if err := mgr.Register(HookConfig{Name: "denied-hook", Type: HookBeforeSend, Enabled: true, Timeout: time.Second},
		func(context.Context, HookPayload) (HookResult, error) { executed = true; return nil, nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.TriggerHook(context.Background(), "denied-hook", map[string]interface{}{}); err == nil {
		t.Fatal("hook execution bypassed policy")
	}
	if executed {
		t.Fatal("denied hook handler executed")
	}
	result, err := auditSvc.Query(context.Background(), &audit.AuditQuery{ResourceType: policy.OperationHookExecute})
	if err != nil || result.Total != 1 {
		t.Fatalf("missing hook denial audit: result=%+v err=%v", result, err)
	}
}
