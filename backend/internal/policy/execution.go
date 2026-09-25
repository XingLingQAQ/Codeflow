package policy

import (
	"context"
	"errors"
	"reflect"
	"strings"
)

// ErrMissingEvaluator rejects execution-policy construction without an
// explicit, non-nil evaluator (I-49).
var ErrMissingEvaluator = errors.New("policy: execution policy requires a non-nil evaluator")

// PolicySession is the request-scoped identity plus policy version object
// carried across one execution entry. It records which identity and which
// policy version a boundary decision is made under, so audits can attribute
// the decision without re-deriving either.
type PolicySession struct {
	ProjectID     string
	AgentID       string
	ActorID       string
	PolicyVersion string
}

// ExecutionPolicy is the fail-closed policy handle for execution services
// (Run/merge/process entries). It is only usable after NewExecutionPolicy:
// the zero value and a nil receiver both deny, and evaluation never consults
// the process-wide globals, so bypassing the constructor cannot reach the
// "policy not installed" compatibility branch.
type ExecutionPolicy struct {
	evaluator Evaluator
}

// ExecutionPolicyOption customizes NewExecutionPolicy.
type ExecutionPolicyOption func(*ExecutionPolicy)

// NewExecutionPolicy binds an execution service to an explicit evaluator. A
// nil or typed-nil evaluator is rejected at construction: an execution host
// must never start on the implicit "policy not installed" branch.
func NewExecutionPolicy(eval Evaluator, opts ...ExecutionPolicyOption) (*ExecutionPolicy, error) {
	if isNilEvaluator(eval) {
		return nil, ErrMissingEvaluator
	}
	p := &ExecutionPolicy{evaluator: eval}
	for _, opt := range opts {
		if opt != nil {
			opt(p)
		}
	}
	return p, nil
}

// Session snapshots the request identity and the bound policy version.
func (p *ExecutionPolicy) Session(req Request) PolicySession {
	return PolicySession{
		ProjectID:     strings.TrimSpace(req.ProjectID),
		AgentID:       strings.TrimSpace(req.AgentID),
		ActorID:       strings.TrimSpace(req.ActorID),
		PolicyVersion: p.Version(),
	}
}

// Version reports the rule version of the bound evaluator.
func (p *ExecutionPolicy) Version() string {
	if p == nil || isNilEvaluator(p.evaluator) {
		return RuleVersion
	}
	if se, ok := p.evaluator.(*StaticEvaluator); ok {
		if version := strings.TrimSpace(se.RuleVersion); version != "" {
			return version
		}
	}
	return RuleVersion
}

// EvaluateInSession binds req to an established PolicySession before
// evaluation. Every non-empty session identity field must equal the
// normalized request value; a disagreement denies before the evaluator runs,
// so a session captured under one project or identity can never authorize an
// operation arriving under another (I-49). Empty session fields are unbound
// and skipped: Session() snapshots legitimately anonymous requests, and those
// fields carry no identity to contradict. A zero-value or nil policy still
// denies through Evaluate when the session matches.
func (p *ExecutionPolicy) EvaluateInSession(ctx context.Context, session PolicySession, req Request) Decision {
	req = normalizeRequest(ctx, req)
	if field := sessionMismatch(session, req); field != "" {
		return recordDecision(ctx, req, Decision{Allowed: false, Reason: "policy session context mismatch: " + field, RuleVersion: p.Version(),
			Operation: req.Operation, Resource: req.Resource, ProjectID: req.ProjectID, AgentID: req.AgentID, PluginID: req.PluginID})
	}
	return p.Evaluate(ctx, req)
}

// sessionMismatch names the first bound identity field the normalized request
// contradicts, or "" when every bound field matches.
func sessionMismatch(session PolicySession, req Request) string {
	switch {
	case session.ProjectID != "" && session.ProjectID != strings.TrimSpace(req.ProjectID):
		return "project_id"
	case session.AgentID != "" && session.AgentID != strings.TrimSpace(req.AgentID):
		return "agent_id"
	case session.ActorID != "" && session.ActorID != strings.TrimSpace(req.ActorID):
		return "actor_id"
	default:
		return ""
	}
}

// EnforceBoundary is the execution-host boundary guard (I-49). Unlike
// EvaluateBoundary it never uses the in-memory compatibility mode: with no
// evaluator installed the request is denied, so an execution host running
// without bootstrap cannot obtain an allow. Execution entries (workspace
// writes, process starts, hook/plugin execution, adapter traffic) use this
// guard; EvaluateBoundary remains only for legacy packages still on the
// compatibility contract.
func EnforceBoundary(ctx context.Context, req Request) Decision {
	return Evaluate(ctx, req)
}

// Evaluate runs req through the injected evaluator only. Unlike
// EvaluateBoundary it never falls back to the in-memory compatibility mode,
// so a zero-value or nil ExecutionPolicy denies instead of allowing.
func (p *ExecutionPolicy) Evaluate(ctx context.Context, req Request) Decision {
	req = normalizeRequest(ctx, req)
	if p == nil || isNilEvaluator(p.evaluator) {
		return recordDecision(ctx, req, Decision{Allowed: false, Reason: "policy evaluator is not configured", RuleVersion: RuleVersion,
			Operation: req.Operation, Resource: req.Resource, ProjectID: req.ProjectID, AgentID: req.AgentID, PluginID: req.PluginID})
	}
	return p.evaluator.Evaluate(ctx, req)
}

// isNilEvaluator also rejects typed-nil implementations (e.g. a nil
// *StaticEvaluator behind a non-nil interface), which would otherwise pass a
// plain nil check and resurrect the missing-evaluator path.
func isNilEvaluator(eval Evaluator) bool {
	if eval == nil {
		return true
	}
	v := reflect.ValueOf(eval)
	switch v.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.Interface:
		return v.IsNil()
	default:
		return false
	}
}
