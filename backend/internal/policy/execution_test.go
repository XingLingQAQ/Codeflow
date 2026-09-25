package policy

import (
	"context"
	"errors"
	"testing"

	"github.com/codeflow/backend/internal/audit"
)

// clearGlobals puts the process-wide policy state into the pre-bootstrap
// condition (no evaluator, no enforcement) and restores it afterwards.
func clearGlobals(t *testing.T) {
	t.Helper()
	SetEvaluator(nil)
	RequireEnforcement(false)
	t.Cleanup(func() { SetEvaluator(nil); RequireEnforcement(false) })
}

func TestNewExecutionPolicyRequiresEvaluator(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		eval    Evaluator
		opts    []ExecutionPolicyOption
		wantErr bool
	}{
		{name: "nil interface", eval: nil, wantErr: true},
		{name: "typed nil *StaticEvaluator", eval: (*StaticEvaluator)(nil), wantErr: true},
		{name: "empty fail-closed evaluator", eval: NewFailClosedEvaluator()},
		{name: "local development evaluator", eval: NewLocalEvaluator()},
		{name: "valid evaluator with nil option", eval: NewLocalEvaluator(), opts: []ExecutionPolicyOption{nil}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := NewExecutionPolicy(tc.eval, tc.opts...)
			if tc.wantErr {
				if !errors.Is(err, ErrMissingEvaluator) {
					t.Fatalf("expected ErrMissingEvaluator, got %v", err)
				}
				if p != nil {
					t.Fatalf("rejected construction returned a policy: %+v", p)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected construction error: %v", err)
			}
			if p == nil {
				t.Fatal("construction succeeded but returned nil policy")
			}
		})
	}
}

// TestExecutionPolicyEvaluateEntryShapes covers every entry shape an
// execution service can take, with the process-wide globals left in the
// pre-bootstrap compatibility condition. The compatibility branch would
// allow the same request, so any denial here comes from ExecutionPolicy
// itself.
func TestExecutionPolicyEvaluateEntryShapes(t *testing.T) {
	clearGlobals(t)
	req := Request{Operation: OperationOutboundRequest, Resource: "https://example.test"}

	local, err := NewExecutionPolicy(NewLocalEvaluator())
	if err != nil {
		t.Fatalf("NewExecutionPolicy(local): %v", err)
	}
	failClosed, err := NewExecutionPolicy(NewFailClosedEvaluator())
	if err != nil {
		t.Fatalf("NewExecutionPolicy(fail-closed): %v", err)
	}

	cases := []struct {
		name       string
		entry      *ExecutionPolicy
		wantAllow  bool
		wantReason string
	}{
		{name: "constructed with valid evaluator allows listed op", entry: local, wantAllow: true, wantReason: "operation allowed by policy"},
		{name: "constructed with empty evaluator still denies unlisted op", entry: failClosed, wantAllow: false, wantReason: "operation denied by policy"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := tc.entry.Evaluate(context.Background(), req)
			if d.Allowed != tc.wantAllow || d.Reason != tc.wantReason {
				t.Fatalf("unexpected decision: %+v", d)
			}
		})
	}

	t.Run("zero value denies without constructor", func(t *testing.T) {
		var zero ExecutionPolicy
		d := zero.Evaluate(context.Background(), req)
		if d.Allowed || d.Reason != "policy evaluator is not configured" {
			t.Fatalf("zero value allowed: %+v", d)
		}
	})
	t.Run("nil receiver denies", func(t *testing.T) {
		var nilPolicy *ExecutionPolicy
		d := nilPolicy.Evaluate(context.Background(), req)
		if d.Allowed || d.Reason != "policy evaluator is not configured" {
			t.Fatalf("nil receiver allowed: %+v", d)
		}
	})
}

// TestExecutionPolicyBoundaryContrast is the I-49 proof: in the pre-bootstrap
// compatibility condition, EvaluateBoundary still allows for legacy unit
// tests, while an ExecutionPolicy that bypassed NewExecutionPolicy denies the
// same request. Calling the execution entry without bootstrap cannot obtain
// an allow.
func TestExecutionPolicyBoundaryContrast(t *testing.T) {
	clearGlobals(t)
	req := Request{Operation: OperationProcessStart, Resource: "secret command"}

	boundary := EvaluateBoundary(context.Background(), req)
	if !boundary.Allowed || boundary.Reason != "policy not installed (in-memory compatibility mode)" {
		t.Fatalf("compatibility behavior changed: %+v", boundary)
	}

	var bypassed ExecutionPolicy
	d := bypassed.Evaluate(context.Background(), req)
	if d.Allowed || d.Reason != "policy evaluator is not configured" {
		t.Fatalf("bypassed construction allowed: %+v", d)
	}
	if d.Resource != "process" {
		t.Fatalf("process command leaked into decision: %q", d.Resource)
	}
}

// TestExecutionPolicyIgnoresProcessGlobals proves the injected evaluator is
// the only one consulted: installing or removing the process-wide evaluator
// after construction does not change decisions in either direction.
func TestExecutionPolicyIgnoresProcessGlobals(t *testing.T) {
	clearGlobals(t)
	ctx := context.Background()
	req := Request{Operation: OperationHookExecute, Resource: "hook-1"}

	local, err := NewExecutionPolicy(NewLocalEvaluator())
	if err != nil {
		t.Fatalf("NewExecutionPolicy(local): %v", err)
	}
	failClosed, err := NewExecutionPolicy(NewFailClosedEvaluator())
	if err != nil {
		t.Fatalf("NewExecutionPolicy(fail-closed): %v", err)
	}

	if d := local.Evaluate(ctx, req); !d.Allowed {
		t.Fatalf("local policy denied before globals installed: %+v", d)
	}
	if d := failClosed.Evaluate(ctx, req); d.Allowed {
		t.Fatalf("fail-closed policy allowed before globals installed: %+v", d)
	}

	SetEvaluator(NewFailClosedEvaluator())
	if d := local.Evaluate(ctx, req); !d.Allowed {
		t.Fatalf("global fail-closed evaluator overrode injected local evaluator: %+v", d)
	}

	SetEvaluator(NewLocalEvaluator())
	if d := failClosed.Evaluate(ctx, req); d.Allowed {
		t.Fatalf("global local evaluator overrode injected fail-closed evaluator: %+v", d)
	}
}

// spyEvaluator records every Evaluate call so a test can prove a denial
// happened before the evaluator ran.
type spyEvaluator struct {
	calls    int
	decision Decision
}

func (e *spyEvaluator) Evaluate(_ context.Context, req Request) Decision {
	e.calls++
	d := e.decision
	d.Operation = req.Operation
	return d
}

// TestPolicyContextMismatch covers EvaluateInSession: every bound session
// identity field (project/agent/actor) must equal the normalized request
// value, and a disagreement denies before the evaluator runs. Empty session
// fields are unbound and skipped; a nil or zero-value policy still denies.
func TestPolicyContextMismatch(t *testing.T) {
	clearGlobals(t)
	ctx := context.Background()
	spy := &spyEvaluator{decision: Decision{Allowed: true, Reason: "spy allow", RuleVersion: RuleVersion}}
	entry, err := NewExecutionPolicy(spy)
	if err != nil {
		t.Fatalf("NewExecutionPolicy(spy): %v", err)
	}

	req := Request{Operation: OperationWorkspaceWrite, Resource: "note.txt", ProjectID: "p1", AgentID: "a1", ActorID: "u1"}
	bound := PolicySession{ProjectID: "p1", AgentID: "a1", ActorID: "u1", PolicyVersion: RuleVersion}

	cases := []struct {
		name       string
		entry      *ExecutionPolicy
		session    PolicySession
		req        Request
		wantAllow  bool
		wantReason string
		wantCalls  int
	}{
		{name: "bound identity matches evaluates", entry: entry, session: bound, req: req,
			wantAllow: true, wantReason: "spy allow", wantCalls: 1},
		{name: "project mismatch denies before evaluation", entry: entry, session: bound,
			req:        Request{Operation: req.Operation, Resource: req.Resource, ProjectID: "p2", AgentID: "a1", ActorID: "u1"},
			wantReason: "policy session context mismatch: project_id"},
		{name: "agent mismatch denies before evaluation", entry: entry, session: bound,
			req:        Request{Operation: req.Operation, Resource: req.Resource, ProjectID: "p1", AgentID: "a2", ActorID: "u1"},
			wantReason: "policy session context mismatch: agent_id"},
		{name: "actor mismatch denies before evaluation", entry: entry, session: bound,
			req:        Request{Operation: req.Operation, Resource: req.Resource, ProjectID: "p1", AgentID: "a1", ActorID: "u2"},
			wantReason: "policy session context mismatch: actor_id"},
		{name: "empty session fields are unbound", entry: entry,
			session: PolicySession{ProjectID: "p1"}, req: req,
			wantAllow: true, wantReason: "spy allow", wantCalls: 1},
		{name: "zero session evaluates", entry: entry, session: PolicySession{}, req: req,
			wantAllow: true, wantReason: "spy allow", wantCalls: 1},
		{name: "request identity trimmed before compare", entry: entry, session: bound,
			req:       Request{Operation: req.Operation, Resource: req.Resource, ProjectID: " p1 ", AgentID: " a1 ", ActorID: " u1 "},
			wantAllow: true, wantReason: "spy allow", wantCalls: 1},
		{name: "nil policy with matching session still denies", entry: nil, session: bound, req: req,
			wantReason: "policy evaluator is not configured"},
		{name: "zero value with matching session still denies", entry: &ExecutionPolicy{}, session: bound, req: req,
			wantReason: "policy evaluator is not configured"},
		{name: "mismatch denial precedes missing evaluator", entry: &ExecutionPolicy{}, session: bound,
			req:        Request{Operation: req.Operation, Resource: req.Resource, ProjectID: "p9"},
			wantReason: "policy session context mismatch: project_id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spy.calls = 0
			d := tc.entry.EvaluateInSession(ctx, tc.session, tc.req)
			if d.Allowed != tc.wantAllow || d.Reason != tc.wantReason {
				t.Fatalf("decision = %+v, want allowed=%v reason %q", d, tc.wantAllow, tc.wantReason)
			}
			if spy.calls != tc.wantCalls {
				t.Fatalf("evaluator ran %d times, want %d", spy.calls, tc.wantCalls)
			}
			if d.RuleVersion != RuleVersion {
				t.Fatalf("rule version = %q, want %q", d.RuleVersion, RuleVersion)
			}
			if d.Operation != tc.req.Operation {
				t.Fatalf("decision operation = %q, want %q", d.Operation, tc.req.Operation)
			}
		})
	}

	t.Run("trace-filled identity participates in binding", func(t *testing.T) {
		spy.calls = 0
		traced := audit.ContextWithTrace(ctx, &audit.AuditTrace{ProjectID: "t1", AgentID: "ta"})
		anon := Request{Operation: OperationWorkspaceWrite, Resource: "note.txt"}

		d := entry.EvaluateInSession(traced, PolicySession{ProjectID: "t1", AgentID: "ta"}, anon)
		if !d.Allowed || spy.calls != 1 {
			t.Fatalf("trace-filled identity should match: %+v (calls %d)", d, spy.calls)
		}

		spy.calls = 0
		d = entry.EvaluateInSession(traced, PolicySession{ProjectID: "t9"}, anon)
		if d.Allowed || d.Reason != "policy session context mismatch: project_id" || spy.calls != 0 {
			t.Fatalf("trace-filled mismatch should deny before evaluation: %+v (calls %d)", d, spy.calls)
		}
	})
}

func TestExecutionPolicySession(t *testing.T) {
	t.Parallel()
	custom := &StaticEvaluator{RuleVersion: "  custom-v1 ", AllowedOperations: map[string]bool{}}
	withVersion, err := NewExecutionPolicy(custom)
	if err != nil {
		t.Fatalf("NewExecutionPolicy(custom): %v", err)
	}
	local, err := NewExecutionPolicy(NewLocalEvaluator())
	if err != nil {
		t.Fatalf("NewExecutionPolicy(local): %v", err)
	}

	cases := []struct {
		name        string
		entry       *ExecutionPolicy
		req         Request
		wantSession PolicySession
	}{
		{
			name:  "identity trimmed and evaluator version",
			entry: withVersion,
			req:   Request{ProjectID: " p1 ", AgentID: " a1 ", ActorID: " u1 "},
			wantSession: PolicySession{
				ProjectID: "p1", AgentID: "a1", ActorID: "u1", PolicyVersion: "custom-v1",
			},
		},
		{
			name:  "default rule version",
			entry: local,
			req:   Request{ProjectID: "p2"},
			wantSession: PolicySession{
				ProjectID: "p2", PolicyVersion: RuleVersion,
			},
		},
		{
			name:        "zero value still snapshots identity with default version",
			entry:       &ExecutionPolicy{},
			req:         Request{ProjectID: "p3", ActorID: "u3"},
			wantSession: PolicySession{ProjectID: "p3", ActorID: "u3", PolicyVersion: RuleVersion},
		},
		{
			name:        "nil receiver",
			entry:       nil,
			req:         Request{AgentID: "a4"},
			wantSession: PolicySession{AgentID: "a4", PolicyVersion: RuleVersion},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.entry.Session(tc.req); got != tc.wantSession {
				t.Fatalf("session = %+v, want %+v", got, tc.wantSession)
			}
			if got := tc.entry.Version(); got != tc.wantSession.PolicyVersion {
				t.Fatalf("version = %q, want %q", got, tc.wantSession.PolicyVersion)
			}
		})
	}
}
