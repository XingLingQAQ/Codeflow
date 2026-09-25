package plugin

import (
	"context"
	"errors"
	"testing"

	"github.com/codeflow/backend/internal/audit"
	"github.com/codeflow/backend/internal/policy"
)

// TestExecutionHostRejectsMissingPolicy is the I-49 closure evidence for the
// plugin domain: plugin services constructed directly — no main.go wiring, no
// bootstrap.Apply — must deny registration entries instead of falling back to
// the "policy not installed" compatibility allow.
func TestExecutionHostRejectsMissingPolicy(t *testing.T) {
	policy.SetEvaluator(nil)
	policy.RequireEnforcement(false)
	t.Cleanup(func() { policy.SetEvaluator(nil); policy.RequireEnforcement(false) })

	assertDenied := func(t *testing.T, err error) {
		t.Helper()
		var denied *policy.DeniedError
		if !errors.As(err, &denied) {
			t.Fatalf("expected *policy.DeniedError, got %v", err)
		}
		if denied.Decision.Allowed || denied.Decision.Reason != "policy evaluator is not configured" {
			t.Fatalf("unexpected decision: %+v", denied.Decision)
		}
	}

	t.Run("install", func(t *testing.T) {
		svc := NewService()
		_, err := svc.Install(context.Background(), "plugin.missing-policy", audit.AuditActor{ID: "u1", Type: "user"})
		assertDenied(t, err)
	})

	t.Run("contribution register", func(t *testing.T) {
		reg := NewContributionRegistry()
		err := reg.RegisterContributionsContext(context.Background(), "plugin.missing-policy", ContributionManifest{})
		assertDenied(t, err)
		if got := reg.ListApplied("plugin.missing-policy"); len(got) != 0 {
			t.Fatalf("denied contribution was applied: %+v", got)
		}
	})
}
