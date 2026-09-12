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
