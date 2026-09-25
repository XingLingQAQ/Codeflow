package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/codeflow/backend/internal/policy"
)

// clearPolicyGlobals puts the process-wide policy state into the
// pre-bootstrap condition (no evaluator, no enforcement) for the duration of
// the test.
func clearPolicyGlobals(t *testing.T) {
	t.Helper()
	policy.SetEvaluator(nil)
	policy.RequireEnforcement(false)
	t.Cleanup(func() { policy.SetEvaluator(nil); policy.RequireEnforcement(false) })
}

// assertMissingPolicyDenied proves the failure is the fail-closed
// missing-evaluator policy denial, not an incidental error.
func assertMissingPolicyDenied(t *testing.T, err error) {
	t.Helper()
	var denied *policy.DeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("expected *policy.DeniedError, got %v", err)
	}
	if denied.Decision.Allowed || denied.Decision.Reason != "policy evaluator is not configured" {
		t.Fatalf("unexpected decision: %+v", denied.Decision)
	}
}

// TestExecutionHostRejectsMissingPolicy is the I-49 closure evidence for the
// workspace/process domains: execution hosts constructed directly — no
// main.go wiring, no bootstrap.Apply — must deny at their execution entries
// instead of falling back to the "policy not installed" compatibility allow.
func TestExecutionHostRejectsMissingPolicy(t *testing.T) {
	clearPolicyGlobals(t)
	ctx := context.Background()

	t.Run("workspace write", func(t *testing.T) {
		svc := NewFSService(nil)
		root := t.TempDir()
		_, err := svc.Write(ctx, &WriteRequest{Root: root, Path: "note.txt", Content: []byte("hi")})
		assertMissingPolicyDenied(t, err)
		if _, statErr := os.Stat(filepath.Join(root, "note.txt")); !os.IsNotExist(statErr) {
			t.Fatalf("denied write reached disk: stat err=%v", statErr)
		}
	})

	t.Run("process start", func(t *testing.T) {
		svc := NewFSService(nil)
		root := t.TempDir()
		manifest := []byte(`{"scripts":{"dev":"node server.js"}}`)
		if err := os.WriteFile(filepath.Join(root, "package.json"), manifest, 0o644); err != nil {
			t.Fatalf("write package.json fixture: %v", err)
		}
		mgr := NewDevServerManager(svc)
		t.Cleanup(mgr.Shutdown)
		_, err := mgr.StartContext(ctx, root, "dev")
		assertMissingPolicyDenied(t, err)
		if got := mgr.List(); len(got) != 0 {
			t.Fatalf("denied process start registered handles: %+v", got)
		}
	})
}
