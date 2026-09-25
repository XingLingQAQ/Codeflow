// Package policytesting provides explicit policy installation helpers for
// unit tests. Tests that exercise execution boundaries (EvaluateBoundary)
// must opt into a restricted allow-list through these helpers instead of
// relying on the implicit "policy not installed" compatibility mode, so a
// test that forgets bootstrap cannot silently allow everything (I-49).
package policytesting

import (
	"testing"

	"github.com/codeflow/backend/internal/policy"
)

// AllowForTest installs a restricted StaticEvaluator that allows exactly the
// given operations and denies everything else, then restores the previously
// installed evaluator via t.Cleanup. Calling it with no operations installs
// an explicit deny-all evaluator. It never enables LocalDevelopment and never
// touches RequireEnforcement: the test runs under real evaluation semantics
// with a minimal, explicit allow-list.
func AllowForTest(t *testing.T, ops ...string) {
	t.Helper()
	previous := policy.GetEvaluator()
	allowed := make(map[string]bool, len(ops))
	for _, op := range ops {
		allowed[op] = true
	}
	policy.SetEvaluator(&policy.StaticEvaluator{
		RuleVersion:       policy.RuleVersion,
		AllowedOperations: allowed,
	})
	t.Cleanup(func() { policy.SetEvaluator(previous) })
}
