package bootstrap

import "github.com/codeflow/backend/internal/policy"

// NewProductionEvaluator creates the single production policy evaluator for
// the bootstrapped service chain. It reuses policy.BootstrapEvaluator as the
// sole definition: fail-closed (deny-by-default, project identity required)
// unless CODEFLOW_ALLOW_LOCAL_EXECUTION=1 explicitly opts into the local
// desktop development mode. No new allow-all switch is introduced.
func NewProductionEvaluator() policy.Evaluator {
	return policy.BootstrapEvaluator()
}

// policyEvaluator returns the evaluator Apply installs: the explicitly
// injected PolicyEvaluator when set, otherwise the production evaluator, so
// every bootstrapped chain carries a non-nil evaluator instead of relying on
// cmd/codeflow-server/main.go to install one (I-49). The production default
// is stateless, so constructing it per call is equivalent to sharing one.
func (s Services) policyEvaluator() policy.Evaluator {
	if s.PolicyEvaluator != nil {
		return s.PolicyEvaluator
	}
	return NewProductionEvaluator()
}

// ExecutionPolicy exposes the fail-closed execution-policy handle bound to
// the evaluator this container installs. It is reserved for future
// Run/merge/process entries: the handle evaluates through the injected
// evaluator only and never consults the process-wide globals, so an execution
// host that receives it cannot reach the "policy not installed" compatibility
// branch. policyEvaluator never returns nil, so construction cannot fail
// today; the error is forwarded to keep callers fail-closed if that changes.
func (s Services) ExecutionPolicy() (*policy.ExecutionPolicy, error) {
	return policy.NewExecutionPolicy(s.policyEvaluator())
}
