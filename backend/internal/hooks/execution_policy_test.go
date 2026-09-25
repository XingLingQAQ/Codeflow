package hooks

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/policy"
)

// TestExecutionHostRejectsMissingPolicy is the I-49 closure evidence for the
// hook domain: a hook manager constructed directly — no main.go wiring, no
// bootstrap.Apply — must deny hook execution instead of falling back to the
// "policy not installed" compatibility allow.
func TestExecutionHostRejectsMissingPolicy(t *testing.T) {
	policy.SetEvaluator(nil)
	policy.RequireEnforcement(false)
	t.Cleanup(func() { policy.SetEvaluator(nil); policy.RequireEnforcement(false) })

	newManager := func(t *testing.T) (*HookManager, *bool) {
		t.Helper()
		executed := false
		mgr := NewHookManager()
		err := mgr.Register(
			HookConfig{Name: "guard-probe", Type: HookBeforeSend, Enabled: true, Timeout: time.Second},
			func(context.Context, HookPayload) (HookResult, error) { executed = true; return nil, nil },
		)
		if err != nil {
			t.Fatalf("register probe hook: %v", err)
		}
		return mgr, &executed
	}

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

	t.Run("trigger", func(t *testing.T) {
		mgr, executed := newManager(t)
		_, err := mgr.Trigger(context.Background(), HookBeforeSend, map[string]interface{}{})
		assertDenied(t, err)
		if *executed {
			t.Fatal("denied hook handler executed")
		}
	})

	t.Run("trigger hook by name", func(t *testing.T) {
		mgr, executed := newManager(t)
		_, err := mgr.TriggerHook(context.Background(), "guard-probe", map[string]interface{}{})
		assertDenied(t, err)
		if *executed {
			t.Fatal("denied hook handler executed")
		}
	})
}
