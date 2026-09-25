package adapters

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/codeflow/backend/internal/policy"
)

// TestExecutionHostRejectsMissingPolicy is the I-49 closure evidence for the
// adapter domain: an adapter constructed directly — no main.go wiring, no
// bootstrap.Apply — must deny outbound traffic instead of falling back to the
// "policy not installed" compatibility allow.
func TestExecutionHostRejectsMissingPolicy(t *testing.T) {
	policy.SetEvaluator(nil)
	policy.RequireEnforcement(false)
	t.Cleanup(func() { policy.SetEvaluator(nil); policy.RequireEnforcement(false) })

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[],"model":"test","usage":{}}`))
	}))
	defer server.Close()

	adapter := NewClaudeAdapter(&AdapterConfig{APIKey: "test", BaseURL: server.URL, Model: "test", MaxRetries: 0})
	_, err := adapter.Send(context.Background(), "hello", nil)
	var denied *policy.DeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("expected *policy.DeniedError, got %v", err)
	}
	if denied.Decision.Allowed || denied.Decision.Reason != "policy evaluator is not configured" {
		t.Fatalf("unexpected decision: %+v", denied.Decision)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("denied adapter issued %d HTTP requests", got)
	}
}
