// Package policy provides the shared execution-boundary policy contract.
// It is deliberately independent of adapters, workspace, hooks, and plugins
// so those packages cannot accidentally create a policy bypass through a
// handler-only check.
package policy

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/codeflow/backend/internal/audit"
)

const RuleVersion = "b4-2026-08-19"

const (
	OperationOutboundRequest   = "outbound_request"
	OperationResponseReceive   = "response_receive"
	OperationWorkspaceWrite    = "workspace_write"
	OperationProcessStart      = "process_start"
	OperationHookExecute       = "hook_execute"
	OperationPluginInvoke      = "plugin_invoke"
	OperationPluginRegister    = "plugin_register"
	OperationPluginToggle      = "plugin_toggle"
	OperationIntegrationInvoke = "integration_invoke"
)

// Request describes a security-sensitive operation at its lowest execution
// boundary. Callers should provide the most specific identity available.
type Request struct {
	Operation string
	Resource  string
	ProjectID string
	AgentID   string
	PluginID  string
	ActorID   string
	Context   map[string]interface{}
}

// Decision is returned to callers and is also serialized into the audit log.
type Decision struct {
	Allowed     bool   `json:"allowed"`
	Reason      string `json:"reason"`
	RuleVersion string `json:"rule_version"`
	Operation   string `json:"operation"`
	Resource    string `json:"resource,omitempty"`
	ProjectID   string `json:"project_id,omitempty"`
	AgentID     string `json:"agent_id,omitempty"`
	PluginID    string `json:"plugin_id,omitempty"`
	AuditID     string `json:"audit_id,omitempty"`
}

// Evaluator is the shared policy implementation contract.
type Evaluator interface {
	Evaluate(context.Context, Request) Decision
}

// StaticEvaluator is a compact production/test evaluator. An empty allow-list
// is fail-closed; LocalDevelopment explicitly enables the local desktop mode.
type StaticEvaluator struct {
	RuleVersion       string
	AllowedOperations map[string]bool
	LocalDevelopment  bool
	RequireProjectID  bool
}

func NewFailClosedEvaluator() *StaticEvaluator {
	return &StaticEvaluator{RuleVersion: RuleVersion, AllowedOperations: map[string]bool{}}
}

func NewLocalEvaluator() *StaticEvaluator {
	return &StaticEvaluator{
		RuleVersion:      RuleVersion,
		LocalDevelopment: true,
		AllowedOperations: map[string]bool{
			OperationOutboundRequest:   true,
			OperationResponseReceive:   true,
			OperationWorkspaceWrite:    true,
			OperationProcessStart:      true,
			OperationHookExecute:       true,
			OperationPluginInvoke:      true,
			OperationPluginRegister:    true,
			OperationPluginToggle:      true,
			OperationIntegrationInvoke: true,
		},
	}
}

func (e *StaticEvaluator) Evaluate(ctx context.Context, req Request) Decision {
	req = normalizeRequest(ctx, req)
	if e == nil {
		return recordDecision(ctx, req, Decision{Allowed: false, Reason: "policy evaluator is not configured", RuleVersion: RuleVersion,
			Operation: req.Operation, Resource: req.Resource, ProjectID: req.ProjectID, AgentID: req.AgentID, PluginID: req.PluginID})
	}
	version := strings.TrimSpace(e.RuleVersion)
	if version == "" {
		version = RuleVersion
	}
	d := Decision{Allowed: false, Reason: "operation denied by policy", RuleVersion: version,
		Operation: req.Operation, Resource: req.Resource, ProjectID: req.ProjectID,
		AgentID: req.AgentID, PluginID: req.PluginID}
	if !e.LocalDevelopment && e.RequireProjectID && strings.TrimSpace(req.ProjectID) == "" {
		d.Reason = "project identity is required"
		return recordDecision(ctx, req, d)
	}
	if !e.AllowedOperations[req.Operation] {
		return recordDecision(ctx, req, d)
	}
	d.Allowed = true
	d.Reason = "operation allowed by policy"
	return recordDecision(ctx, req, d)
}

var (
	globalMu            sync.RWMutex
	global              Evaluator
	enforcementRequired bool
)

// SetEvaluator installs the process-wide evaluator. Passing nil removes it.
func SetEvaluator(e Evaluator) {
	globalMu.Lock()
	global = e
	globalMu.Unlock()
}

func GetEvaluator() Evaluator {
	globalMu.RLock()
	e := global
	globalMu.RUnlock()
	return e
}

func HasEvaluator() bool { return GetEvaluator() != nil }

// RequireEnforcement marks the process as a production execution host. Once
// enabled, a missing evaluator is denied instead of using unit-test
// compatibility mode.
func RequireEnforcement(required bool) {
	globalMu.Lock()
	enforcementRequired = required
	globalMu.Unlock()
}

func EnforcementRequired() bool {
	globalMu.RLock()
	required := enforcementRequired
	globalMu.RUnlock()
	return required
}

// Evaluate evaluates against the global policy. A missing evaluator is
// fail-closed for direct callers; execution packages use EvaluateBoundary for
// backwards-compatible in-memory unit construction.
func Evaluate(ctx context.Context, req Request) Decision {
	req = normalizeRequest(ctx, req)
	e := GetEvaluator()
	if e == nil {
		return recordDecision(ctx, req, Decision{Allowed: false, Reason: "policy evaluator is not configured", RuleVersion: RuleVersion,
			Operation: req.Operation, Resource: req.Resource, ProjectID: req.ProjectID, AgentID: req.AgentID, PluginID: req.PluginID})
	}
	return e.Evaluate(ctx, req)
}

func normalizeRequest(ctx context.Context, req Request) Request {
	if trace := audit.TraceFromContext(ctx); trace != nil {
		if req.ProjectID == "" {
			req.ProjectID = trace.ProjectID
		}
		if req.AgentID == "" {
			req.AgentID = trace.AgentID
		}
	}
	if req.Operation == OperationOutboundRequest || req.Operation == OperationResponseReceive {
		if parsed, err := url.Parse(req.Resource); err == nil && parsed.Scheme != "" {
			parsed.RawQuery = ""
			parsed.Fragment = ""
			req.Resource = parsed.String()
		}
	}
	if req.Operation == OperationProcessStart {
		// Commands may contain credentials, commit messages, or user data. The
		// policy is operation-based, so never retain the raw command in audit.
		req.Resource = "process"
	}
	return req
}

// EvaluateBoundary lets constructors used by isolated unit tests operate
// without process-wide bootstrap, while still enforcing any installed policy.
func EvaluateBoundary(ctx context.Context, req Request) Decision {
	if !HasEvaluator() {
		if EnforcementRequired() {
			return Evaluate(ctx, req)
		}
		return Decision{Allowed: true, Reason: "policy not installed (in-memory compatibility mode)", RuleVersion: RuleVersion,
			Operation: req.Operation, Resource: req.Resource, ProjectID: req.ProjectID, AgentID: req.AgentID, PluginID: req.PluginID}
	}
	return Evaluate(ctx, req)
}

func recordDecision(ctx context.Context, req Request, d Decision) Decision {
	if ctx == nil {
		ctx = context.Background()
	}
	details := map[string]interface{}{
		"allowed": d.Allowed, "reason": d.Reason, "rule_version": d.RuleVersion,
		"operation": req.Operation, "resource": req.Resource,
		"project_id": req.ProjectID, "agent_id": req.AgentID, "plugin_id": req.PluginID,
	}
	if len(req.Context) > 0 {
		keys := make([]string, 0, len(req.Context))
		for key := range req.Context {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		details["context_keys"] = keys
	}
	severity := audit.SeverityInfo
	outcome := audit.OutcomeSuccess
	if !d.Allowed {
		severity = audit.SeverityWarning
		outcome = audit.OutcomeFailure
	}
	entryID, err := audit.Record(ctx, &audit.AuditLogEntry{
		EventType: audit.EventSecurity,
		Severity:  severity,
		Actor:     audit.AuditActor{ID: firstNonEmpty(req.AgentID, req.ActorID), Type: "agent"},
		Resource:  audit.AuditResource{Type: req.Operation, ID: req.Resource, Name: req.Resource},
		Action:    req.Operation,
		Outcome:   outcome,
		Details:   details,
	})
	if err == nil {
		d.AuditID = entryID
	}
	return d
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return "policy"
}

// BootstrapEvaluator returns the production evaluator. Local execution is an
// explicit opt-in for desktop development; server deployments remain closed
// until a policy is installed and operations are allowed.
func BootstrapEvaluator() Evaluator {
	if os.Getenv("CODEFLOW_ALLOW_LOCAL_EXECUTION") == "1" {
		return NewLocalEvaluator()
	}
	e := NewFailClosedEvaluator()
	e.RequireProjectID = true
	return e
}

func DenialError(d Decision) error {
	if d.Allowed {
		return nil
	}
	return &DeniedError{Decision: d}
}

// DeniedError is non-retryable: repeating an operation cannot change policy.
type DeniedError struct {
	Decision Decision
}

func (e *DeniedError) Error() string {
	d := e.Decision
	return fmt.Sprintf("policy denied %s: %s (rule %s, audit %s)", d.Operation, d.Reason, d.RuleVersion, d.AuditID)
}
