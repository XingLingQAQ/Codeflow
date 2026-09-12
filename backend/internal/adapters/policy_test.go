package adapters

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/codeflow/backend/internal/policy"
)

func TestOutboundPolicyCannotBeBypassedByAdapterMethods(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[],"model":"test","usage":{}}`))
	}))
	defer server.Close()

	policy.SetEvaluator(&policy.StaticEvaluator{RuleVersion: policy.RuleVersion, AllowedOperations: map[string]bool{
		policy.OperationHookExecute: true,
	}})
	t.Cleanup(func() { policy.SetEvaluator(nil) })

	newAdapter := func() *ClaudeAdapter {
		return NewClaudeAdapter(&AdapterConfig{APIKey: "test", BaseURL: server.URL, Model: "test", MaxRetries: 0})
	}
	if _, err := newAdapter().Send(context.Background(), "hello", nil); err == nil {
		t.Fatal("Send bypassed outbound policy")
	}
	if _, err := newAdapter().Stream(context.Background(), "hello", nil); err == nil {
		t.Fatal("Stream bypassed outbound policy")
	}
	if _, err := newAdapter().SendToolTurn(context.Background(), &ToolTurnRequest{}); err == nil {
		t.Fatal("SendToolTurn bypassed outbound policy")
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("denied adapter methods issued %d HTTP requests", got)
	}
}

func TestResponseReceivePolicyClosesSuccessfulHTTPResponse(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[],"model":"test","usage":{}}`))
	}))
	defer server.Close()

	policy.SetEvaluator(&policy.StaticEvaluator{RuleVersion: policy.RuleVersion, AllowedOperations: map[string]bool{
		policy.OperationOutboundRequest: true,
	}})
	t.Cleanup(func() { policy.SetEvaluator(nil) })

	adapter := NewClaudeAdapter(&AdapterConfig{APIKey: "test", BaseURL: server.URL, Model: "test", MaxRetries: 0})
	if _, err := adapter.SendToolTurn(context.Background(), &ToolTurnRequest{}); err == nil {
		t.Fatal("expected response receive denial")
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("expected one outbound request before response denial, got %d", got)
	}
}
