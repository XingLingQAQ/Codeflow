package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/codeflow/backend/internal/policy"
)

// TestExecutionHostRejectsMissingPolicy is the I-49 closure evidence for the
// git domain: a git manager constructed directly — no main.go wiring, no
// bootstrap.Apply — must deny process starts instead of falling back to the
// "policy not installed" compatibility allow.
func TestExecutionHostRejectsMissingPolicy(t *testing.T) {
	policy.SetEvaluator(nil)
	policy.RequireEnforcement(false)
	t.Cleanup(func() { policy.SetEvaluator(nil); policy.RequireEnforcement(false) })

	dir := t.TempDir()
	manager := NewGitManager(dir)
	err := manager.Init(context.Background())
	var denied *policy.DeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("expected *policy.DeniedError, got %v", err)
	}
	if denied.Decision.Allowed || denied.Decision.Reason != "policy evaluator is not configured" {
		t.Fatalf("unexpected decision: %+v", denied.Decision)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".git")); !os.IsNotExist(statErr) {
		t.Fatalf("denied git init created repository state: stat err=%v", statErr)
	}
}
