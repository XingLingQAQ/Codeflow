// Production readiness probes (plan section 15 T0.12 step 1/2, section 28
// T0.12.b).
//
// Apply installs one probe per dependency the plan enumerates, so GET /ready
// answers with real, timed, read-only checks instead of Has* booleans. Two
// deliberate rules shape this file:
//
//   - Every production probe is Required=false. A dependency that is planned
//     but not wired yet (vault, event_store, the execution backends) reports
//     not_configured; if it gated the verdict, /ready would answer 503 forever
//     and the shell would never start. Executability is expressed by the
//     capability sets (internal/readiness/capabilities.go), not by the HTTP
//     status of /ready.
//   - Every check is read-only and bounded: process globals, a stat of the
//     configured workspace roots, nothing else. No file is written, no network
//     call is made, and no probe outlives its timeout.
//
// The probes cover the dependencies this build can actually verify today. The
// rest are registered as not_configured with the card that will wire them, so
// the missing dependency is visible in /ready instead of silently absent:
// runtime database codeflow.db and migrations are wired by T1.01/T1.04, the
// event store and outbox dispatcher by T1.05, the vault by T2.04, and the
// execution backends by T1.13 (claude_code) / T4.01 (codex) / T4.02 (gemini).
package bootstrap

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/codeflow/backend/internal/policy"
	"github.com/codeflow/backend/internal/readiness"
	"github.com/codeflow/backend/internal/workspace"
)

// probeTimeout bounds every production probe. The checks here are process-local
// (globals plus a stat per workspace root), so one second is generous while
// still keeping /ready fast when a filesystem stalls.
const probeTimeout = time.Second

// execBackendNames are the execution backends the plan enumerates. Each one gets
// an exec_backend:<name> probe; the capability payload keys the backend by the
// bare name (readiness.ExecBackendPrefix is the shared prefix).
var execBackendNames = []string{"claude_code", "codex", "gemini"}

// ExecBackendProbeNames returns the exec_backend:* probe names the bootstrap
// registers, in registration order. Consumers (the run creation path) use it to
// enumerate the backends a Run may target.
func ExecBackendProbeNames() []string {
	names := make([]string, 0, len(execBackendNames))
	for _, backend := range execBackendNames {
		names = append(names, readiness.ExecBackendPrefix+backend)
	}
	return names
}

// readinessSpecs builds the production probe set for one Services container.
// Every spec is Required=false (see the package comment) and read-only.
func (s Services) readinessSpecs() []readiness.Spec {
	specs := []readiness.Spec{
		{Probe: frontendProtocolProbe(), Timeout: probeTimeout},
		{Probe: policyProbe(), Timeout: probeTimeout},
		{Probe: workspaceProbe(s.Workspace), Timeout: probeTimeout},
		{Probe: readiness.NewNotConfiguredProbe(readiness.ComponentDatabase,
			"runtime database codeflow.db is wired by T1.01/T1.04"), Timeout: probeTimeout},
		{Probe: readiness.NewNotConfiguredProbe(readiness.ComponentMigrations,
			"runtime schema migrations are wired by T1.01/T1.04"), Timeout: probeTimeout},
		{Probe: readiness.NewNotConfiguredProbe(readiness.ComponentEventStore,
			"event store is wired by T1.05"), Timeout: probeTimeout},
		{Probe: readiness.NewNotConfiguredProbe(readiness.ComponentOutboxDispatcher,
			"outbox dispatcher is wired by T1.05"), Timeout: probeTimeout},
		{Probe: readiness.NewNotConfiguredProbe(readiness.ComponentVault,
			"secret vault is wired by T2.04"), Timeout: probeTimeout},
	}
	backendDetails := map[string]string{
		"claude_code": "claude_code execution backend is wired by T1.13",
		"codex":       "codex execution backend is wired by T4.01",
		"gemini":      "gemini execution backend is wired by T4.02",
	}
	for _, backend := range execBackendNames {
		detail := backendDetails[backend]
		if detail == "" {
			detail = "execution backend is not wired yet"
		}
		specs = append(specs, readiness.Spec{
			Probe:   readiness.NewNotConfiguredProbe(readiness.ExecBackendPrefix+backend, detail),
			Timeout: probeTimeout,
		})
	}
	return specs
}

// frontendProtocolProbe reports the handshake protocol version this backend
// speaks. The value is a compile-time constant, so the probe cannot fail today;
// it exists so the shell can see the version the backend expects and so a
// future mismatch has a place to be reported (protocol_mismatch).
func frontendProtocolProbe() readiness.Probe {
	return readiness.NewProbeFunc(readiness.ComponentFrontendProtocol, func(context.Context) readiness.Result {
		return readiness.Result{
			State:  readiness.StateReady,
			Detail: "protocol_version=" + readiness.FrontendProtocolVersion,
		}
	})
}

// policyProbe reports whether this process carries a policy evaluator with
// enforcement required. Both conditions matter: an evaluator that is installed
// but not required would silently allow boundaries the plan requires to be
// denied (I-49), so that combination is a failure, not readiness.
func policyProbe() readiness.Probe {
	return readiness.NewProbeFunc(readiness.ComponentPolicy, func(context.Context) readiness.Result {
		hasEvaluator := policy.HasEvaluator()
		enforcementRequired := policy.EnforcementRequired()
		switch {
		case hasEvaluator && enforcementRequired:
			return readiness.Result{State: readiness.StateReady, Detail: "policy evaluator installed, enforcement required"}
		case !hasEvaluator && !enforcementRequired:
			return readiness.Result{
				State:   readiness.StateFailed,
				ErrCode: readiness.CodePolicyNotInstalled,
				Detail:  "no policy evaluator installed and enforcement is not required",
			}
		case !hasEvaluator:
			return readiness.Result{
				State:   readiness.StateFailed,
				ErrCode: readiness.CodePolicyNotInstalled,
				Detail:  "enforcement is required but no policy evaluator is installed",
			}
		default:
			return readiness.Result{
				State:   readiness.StateFailed,
				ErrCode: readiness.CodePolicyNotInstalled,
				Detail:  "policy evaluator installed but enforcement is not required",
			}
		}
	})
}

// workspaceProbe reports whether the workspace service has usable roots. An
// unconfigured workspace is not_configured (the desktop default is
// unrestricted, which proves nothing); a configured root that disappeared is
// failed with the missing paths, because the shell must not offer write or
// merge actions against a root that is gone.
func workspaceProbe(service workspace.Service) readiness.Probe {
	return readiness.NewProbeFunc(readiness.ComponentWorkspace, func(context.Context) readiness.Result {
		fs, ok := service.(*workspace.FSService)
		if !ok {
			return readiness.Result{
				State:   readiness.StateNotConfigured,
				ErrCode: readiness.CodeWorkspaceRootsUnconfigured,
				Detail:  "workspace service is not an FSService; no allowed roots to check",
			}
		}
		roots := fs.AllowedRoots()
		if len(roots) == 0 {
			return readiness.Result{
				State:   readiness.StateNotConfigured,
				ErrCode: readiness.CodeWorkspaceRootsUnconfigured,
				Detail:  "no workspace root configured (CODEFLOW_WORKSPACE_ROOTS)",
			}
		}
		missing := make([]string, 0, len(roots))
		for _, root := range roots {
			info, err := os.Stat(root)
			if err != nil || !info.IsDir() {
				missing = append(missing, root)
			}
		}
		if len(missing) > 0 {
			return readiness.Result{
				State:   readiness.StateFailed,
				ErrCode: readiness.CodeWorkspaceRootMissing,
				Detail: fmt.Sprintf("%d of %d configured workspace root(s) missing: %s",
					len(missing), len(roots), strings.Join(missing, ", ")),
			}
		}
		return readiness.Result{
			State:  readiness.StateReady,
			Detail: fmt.Sprintf("%d workspace root(s) present", len(roots)),
		}
	})
}
