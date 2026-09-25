package bootstrap

import (
	"context"
	"errors"
	"testing"

	"github.com/codeflow/backend/internal/policy"
	"github.com/codeflow/backend/internal/workspace"
)

// clearPolicyGlobals puts the process-wide policy state into the
// pre-bootstrap condition and restores that condition afterwards, mirroring
// the clearGlobals pattern in the policy package tests.
func clearPolicyGlobals(t *testing.T) {
	t.Helper()
	policy.SetEvaluator(nil)
	policy.RequireEnforcement(false)
	t.Cleanup(func() { policy.SetEvaluator(nil); policy.RequireEnforcement(false) })
}

// TestServicesApplyInstallsProductionPolicyByDefault proves a bootstrapped
// chain carries a non-nil evaluator without any main.go wiring: Apply with no
// PolicyEvaluator installs the fail-closed production evaluator with
// enforcement required, ExecutionPolicy exposes a bound handle, and Reset
// tears the policy state down again.
func TestServicesApplyInstallsProductionPolicyByDefault(t *testing.T) {
	clearPolicyGlobals(t)
	t.Setenv("CODEFLOW_ALLOW_LOCAL_EXECUTION", "")
	services := newFullTestServices()
	services.Reset()
	t.Cleanup(services.Reset)

	if err := services.Apply(); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	if !policy.HasEvaluator() {
		t.Fatal("Apply did not install an evaluator")
	}
	if !policy.EnforcementRequired() {
		t.Fatal("Apply did not require enforcement")
	}

	ctx := context.Background()
	req := policy.Request{Operation: policy.OperationWorkspaceWrite, Resource: "note.txt"}
	if d := policy.EvaluateBoundary(ctx, req); d.Allowed {
		t.Fatalf("default production evaluator allowed %+v: %+v", req, d)
	}

	handle, err := services.ExecutionPolicy()
	if err != nil {
		t.Fatalf("ExecutionPolicy failed: %v", err)
	}
	if handle == nil {
		t.Fatal("ExecutionPolicy returned a nil handle")
	}
	if d := handle.Evaluate(ctx, req); d.Allowed {
		t.Fatalf("execution policy handle allowed %+v: %+v", req, d)
	}
	// The handle is bound to the evaluator captured at construction; swapping
	// the process-wide evaluator afterwards must not change its decisions.
	policy.SetEvaluator(policy.NewLocalEvaluator())
	if d := handle.Evaluate(ctx, req); d.Allowed {
		t.Fatalf("handle consulted swapped process-wide evaluator: %+v", d)
	}

	services.Reset()
	if policy.HasEvaluator() || policy.EnforcementRequired() {
		t.Fatal("Reset did not clear the installed policy state")
	}
}

// TestServicesApplyInstallsInjectedPolicyEvaluator proves an explicitly
// injected restricted evaluator is the one enforced at execution boundaries
// and the one bound into the ExecutionPolicy handle.
func TestServicesApplyInstallsInjectedPolicyEvaluator(t *testing.T) {
	clearPolicyGlobals(t)
	restricted := &policy.StaticEvaluator{
		RuleVersion:       policy.RuleVersion,
		AllowedOperations: map[string]bool{policy.OperationWorkspaceWrite: true},
	}
	services := newFullTestServices()
	services.PolicyEvaluator = restricted
	services.Reset()
	t.Cleanup(services.Reset)

	if err := services.Apply(); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	ctx := context.Background()
	allowed := policy.Request{Operation: policy.OperationWorkspaceWrite, Resource: "note.txt"}
	if d := policy.EvaluateBoundary(ctx, allowed); !d.Allowed {
		t.Fatalf("injected evaluator denied listed op: %+v", d)
	}
	denied := policy.Request{Operation: policy.OperationProcessStart, Resource: "rm -rf"}
	if d := policy.EvaluateBoundary(ctx, denied); d.Allowed {
		t.Fatalf("injected evaluator allowed unlisted op: %+v", d)
	}

	handle, err := services.ExecutionPolicy()
	if err != nil {
		t.Fatalf("ExecutionPolicy failed: %v", err)
	}
	if d := handle.Evaluate(ctx, allowed); !d.Allowed {
		t.Fatalf("handle denied op allowed by injected evaluator: %+v", d)
	}
	if d := handle.Evaluate(ctx, denied); d.Allowed {
		t.Fatalf("handle allowed op denied by injected evaluator: %+v", d)
	}
}

// TestDirectHostEnforcesBootstrapInstalledPolicy constructs an execution host
// directly (workspace.NewFSService) — no main.go lines, no bootstrap
// container field — and proves its write boundary is fail-closed in both
// directions (I-49 closed): before bootstrap.Apply the missing evaluator
// denies (the former compatibility-allow residual pinned here by T0.09.b was
// closed in T0.09.c by routing execution entries through EnforceBoundary),
// and after Apply the installed fail-closed production policy still denies.
func TestDirectHostEnforcesBootstrapInstalledPolicy(t *testing.T) {
	clearPolicyGlobals(t)
	t.Setenv("CODEFLOW_ALLOW_LOCAL_EXECUTION", "")
	host := workspace.NewFSService(nil)
	req := &workspace.WriteRequest{Root: t.TempDir(), Path: "note.txt", Content: []byte("hi")}

	_, preApplyErr := host.Write(context.Background(), req)
	var preApplyDenied *policy.DeniedError
	if !errors.As(preApplyErr, &preApplyDenied) {
		t.Fatalf("expected missing-policy denial before bootstrap Apply, got %v", preApplyErr)
	}
	if preApplyDenied.Decision.Reason != "policy evaluator is not configured" {
		t.Fatalf("pre-Apply denial came from an unexpected branch: %+v", preApplyDenied.Decision)
	}

	services := newFullTestServices()
	services.Reset()
	t.Cleanup(services.Reset)
	if err := services.Apply(); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	_, err := host.Write(context.Background(), req)
	var deniedErr *policy.DeniedError
	if !errors.As(err, &deniedErr) {
		t.Fatalf("expected policy denial after bootstrap Apply, got %v", err)
	}
	if deniedErr.Decision.Allowed {
		t.Fatalf("denial error carries an allowed decision: %+v", deniedErr.Decision)
	}
}
