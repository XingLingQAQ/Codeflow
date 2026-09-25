package policytesting_test

import (
	"context"
	"testing"

	"github.com/codeflow/backend/internal/policy"
	"github.com/codeflow/backend/internal/policy/policytesting"
)

// snapshotGlobals restores the pre-test process-wide policy state afterwards,
// mirroring the clearGlobals pattern in the policy package tests.
func snapshotGlobals(t *testing.T) {
	t.Helper()
	previous := policy.GetEvaluator()
	enforcement := policy.EnforcementRequired()
	policy.SetEvaluator(nil)
	policy.RequireEnforcement(false)
	t.Cleanup(func() {
		policy.SetEvaluator(previous)
		policy.RequireEnforcement(enforcement)
	})
}

// TestAllowForTestRestrictsToListedOperations proves the helper turns the
// default global allow into an explicit, minimal allow-list: the listed
// operation is allowed, every other operation is denied, and the denial still
// scrubs process commands from the decision.
func TestAllowForTestRestrictsToListedOperations(t *testing.T) {
	snapshotGlobals(t)
	ctx := context.Background()

	if d := policy.EvaluateBoundary(ctx, policy.Request{Operation: policy.OperationProcessStart}); !d.Allowed {
		t.Fatalf("expected compatibility allow before helper, got %+v", d)
	}

	policytesting.AllowForTest(t, policy.OperationWorkspaceWrite)

	if d := policy.EvaluateBoundary(ctx, policy.Request{Operation: policy.OperationWorkspaceWrite, Resource: "main.go"}); !d.Allowed {
		t.Fatalf("listed operation denied: %+v", d)
	}
	d := policy.EvaluateBoundary(ctx, policy.Request{Operation: policy.OperationProcessStart, Resource: "secret command"})
	if d.Allowed || d.Reason != "operation denied by policy" {
		t.Fatalf("unlisted operation allowed: %+v", d)
	}
	if d.Resource != "process" {
		t.Fatalf("process command leaked into decision: %q", d.Resource)
	}
}

// TestAllowForTestCleanupRestoresPreviousEvaluator proves the helper replaces
// the installed evaluator for the scoped test only and restores the previous
// one when that test finishes.
func TestAllowForTestCleanupRestoresPreviousEvaluator(t *testing.T) {
	snapshotGlobals(t)
	sentinel := policy.NewLocalEvaluator()
	policy.SetEvaluator(sentinel)

	t.Run("scoped", func(t *testing.T) {
		policytesting.AllowForTest(t, policy.OperationHookExecute)
		if policy.GetEvaluator() == sentinel {
			t.Fatal("helper did not replace the evaluator")
		}
		ctx := context.Background()
		if d := policy.EvaluateBoundary(ctx, policy.Request{Operation: policy.OperationHookExecute, Resource: "hook-1"}); !d.Allowed {
			t.Fatalf("listed operation denied inside scope: %+v", d)
		}
		if d := policy.EvaluateBoundary(ctx, policy.Request{Operation: policy.OperationWorkspaceWrite, Resource: "main.go"}); d.Allowed {
			t.Fatalf("unlisted operation allowed inside scope: %+v", d)
		}
	})

	if got := policy.GetEvaluator(); got != sentinel {
		t.Fatalf("cleanup did not restore previous evaluator: got %v", got)
	}
}
