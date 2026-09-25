package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/codeflow/backend/internal/policy"
	"github.com/codeflow/backend/internal/readiness"
	"github.com/codeflow/backend/internal/workspace"
)

// expectedReadinessProbes is the production probe set Apply must install (plan
// section 15 T0.12 step 1). The count is pinned so a probe cannot silently
// disappear from /ready.
var expectedReadinessProbes = []string{
	readiness.ComponentFrontendProtocol,
	readiness.ComponentPolicy,
	readiness.ComponentWorkspace,
	readiness.ComponentDatabase,
	readiness.ComponentMigrations,
	readiness.ComponentEventStore,
	readiness.ComponentOutboxDispatcher,
	readiness.ComponentVault,
	readiness.ExecBackendPrefix + "claude_code",
	readiness.ExecBackendPrefix + "codex",
	readiness.ExecBackendPrefix + "gemini",
}

// applyReadinessServices applies a full test container with the production
// probes and restores the process globals afterwards. The returned probe set is
// the registry snapshot taken right after Apply.
func applyReadinessServices(t *testing.T) (Services, []readiness.Spec) {
	t.Helper()
	readiness.Clear()
	clearPolicyGlobals(t)
	services := newFullTestServices()
	services.Reset()
	t.Cleanup(services.Reset)

	if err := services.Apply(); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	return services, readiness.Default.Snapshot()
}

// TestApplyRegistersProductionProbes pins the probe set: every dependency of the
// plan's enumeration is registered exactly once, and every production probe is
// non-required so an unwired dependency cannot make /ready answer 503 forever.
func TestApplyRegistersProductionProbes(t *testing.T) {
	_, specs := applyReadinessServices(t)

	names := readiness.Registered()
	want := append([]string(nil), expectedReadinessProbes...)
	sort.Strings(want)
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("registered probes:\n want %v\n got  %v", want, names)
	}
	if len(specs) != len(expectedReadinessProbes) {
		t.Fatalf("snapshot has %d specs, want %d", len(specs), len(expectedReadinessProbes))
	}
	for _, spec := range specs {
		name := "?"
		if spec.Probe != nil {
			name = spec.Probe.Name()
		}
		if spec.Required {
			t.Fatalf("probe %q must not be required: a not_configured dependency would 503 /ready forever", name)
		}
		if spec.Timeout <= 0 || spec.Timeout > 1_000_000_000 {
			t.Fatalf("probe %q timeout %s is outside the read-only budget", name, spec.Timeout)
		}
		if spec.Probe == nil || !spec.Probe.Readonly() {
			t.Fatalf("probe %q must be read-only", name)
		}
	}
}

// TestApplyIsIdempotentAndResetClearsProbes covers the lifecycle contract:
// repeated Apply calls neither fail nor duplicate probes, and Reset removes the
// whole set so a torn-down container leaves no probe behind.
func TestApplyIsIdempotentAndResetClearsProbes(t *testing.T) {
	services, _ := applyReadinessServices(t)

	first := readiness.Registered()
	for i := 0; i < 3; i++ {
		if err := services.Apply(); err != nil {
			t.Fatalf("repeated Apply %d failed: %v", i, err)
		}
		if got := readiness.Registered(); !reflect.DeepEqual(got, first) {
			t.Fatalf("Apply %d changed the probe set:\n want %v\n got  %v", i, first, got)
		}
	}

	services.Reset()
	if got := readiness.Registered(); len(got) != 0 {
		t.Fatalf("Reset left probes registered: %v", got)
	}
	// Reset must be idempotent too.
	services.Reset()
	if got := readiness.Registered(); len(got) != 0 {
		t.Fatalf("second Reset left probes registered: %v", got)
	}
}

// TestFrontendProtocolProbeReportsHandshakeVersion covers the probe that makes
// the handshake version visible in /ready.
func TestFrontendProtocolProbeReportsHandshakeVersion(t *testing.T) {
	components := runProductionProbes(t)

	component, ok := components[readiness.ComponentFrontendProtocol]
	if !ok {
		t.Fatalf("frontend_protocol probe missing: %v", keys(components))
	}
	if component.Result.State != readiness.StateReady {
		t.Fatalf("frontend_protocol: want ready, got %+v", component.Result)
	}
	if !strings.Contains(component.Result.Detail, readiness.FrontendProtocolVersion) {
		t.Fatalf("frontend_protocol detail must name the handshake version, got %q", component.Result.Detail)
	}
}

// TestPolicyProbeReportsInstallation covers the policy probe in both
// directions: Apply installs the evaluator with enforcement required, and a
// process without it is failed/policy_not_installed.
func TestPolicyProbeReportsInstallation(t *testing.T) {
	// Apply installs the evaluator and registers the probes; the probe runs are
	// then taken through the registry without another Apply.
	applyReadinessServices(t)

	component := readiness.Run(context.Background())[readiness.ComponentPolicy]
	if component.Result.State != readiness.StateReady {
		t.Fatalf("policy after Apply: want ready, got %+v", component.Result)
	}

	// Tear the process-wide policy state down: the probe must notice without a
	// restart and without a cache.
	policy.SetEvaluator(nil)
	policy.RequireEnforcement(false)
	t.Cleanup(func() { policy.SetEvaluator(nil); policy.RequireEnforcement(false) })

	component = readiness.Run(context.Background())[readiness.ComponentPolicy]
	if component.Result.State != readiness.StateFailed {
		t.Fatalf("policy without evaluator: want failed, got %+v", component.Result)
	}
	if component.Result.ErrCode != readiness.CodePolicyNotInstalled {
		t.Fatalf("policy without evaluator: want %s, got %q", readiness.CodePolicyNotInstalled, component.Result.ErrCode)
	}
}

// TestPolicyProbeRequiresEnforcement covers the second half of the policy
// contract: an installed evaluator without enforcement is not readiness.
func TestPolicyProbeRequiresEnforcement(t *testing.T) {
	readiness.Clear()
	t.Cleanup(readiness.Clear)
	if err := readiness.Register(readiness.Spec{Probe: policyProbe()}); err != nil {
		t.Fatalf("register policy probe: %v", err)
	}
	policy.SetEvaluator(policy.NewFailClosedEvaluator())
	policy.RequireEnforcement(false)
	t.Cleanup(func() { policy.SetEvaluator(nil); policy.RequireEnforcement(false) })

	components := readiness.Run(context.Background())
	component := components[readiness.ComponentPolicy]
	if component.Result.State != readiness.StateFailed || component.Result.ErrCode != readiness.CodePolicyNotInstalled {
		t.Fatalf("policy with enforcement off: want failed/%s, got %+v", readiness.CodePolicyNotInstalled, component.Result)
	}
	if !strings.Contains(component.Result.Detail, "enforcement") {
		t.Fatalf("detail must explain the missing enforcement, got %q", component.Result.Detail)
	}
}

// TestWorkspaceProbeReportsRoots covers the three workspace outcomes the plan
// enumerates: configured and present -> ready; configured but gone ->
// failed/workspace_root_missing; nothing configured -> not_configured.
func TestWorkspaceProbeReportsRoots(t *testing.T) {
	readiness.Clear()
	t.Cleanup(readiness.Clear)
	fs := workspace.NewFSService(nil)
	if err := readiness.Register(readiness.Spec{Probe: workspaceProbe(fs)}); err != nil {
		t.Fatalf("register workspace probe: %v", err)
	}

	t.Run("unconfigured", func(t *testing.T) {
		components := readiness.Run(context.Background())
		component := components[readiness.ComponentWorkspace]
		if component.Result.State != readiness.StateNotConfigured {
			t.Fatalf("want not_configured, got %+v", component.Result)
		}
		if component.Result.ErrCode != readiness.CodeWorkspaceRootsUnconfigured {
			t.Fatalf("want %s, got %q", readiness.CodeWorkspaceRootsUnconfigured, component.Result.ErrCode)
		}
	})

	t.Run("present root", func(t *testing.T) {
		root := t.TempDir()
		fs.SetAllowedRoots([]string{root})
		components := readiness.Run(context.Background())
		component := components[readiness.ComponentWorkspace]
		if component.Result.State != readiness.StateReady {
			t.Fatalf("want ready, got %+v", component.Result)
		}
		if len(fs.AllowedRoots()) != 1 {
			t.Fatalf("AllowedRoots = %v", fs.AllowedRoots())
		}
	})

	t.Run("missing root", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "gone")
		fs.SetAllowedRoots([]string{root})
		components := readiness.Run(context.Background())
		component := components[readiness.ComponentWorkspace]
		if component.Result.State != readiness.StateFailed {
			t.Fatalf("want failed, got %+v", component.Result)
		}
		if component.Result.ErrCode != readiness.CodeWorkspaceRootMissing {
			t.Fatalf("want %s, got %q", readiness.CodeWorkspaceRootMissing, component.Result.ErrCode)
		}
		if !strings.Contains(component.Result.Detail, root) {
			t.Fatalf("detail must name the missing root %q, got %q", root, component.Result.Detail)
		}
	})
}

// TestWorkspaceProbeReactsToRepair covers "fix the dependency, re-check, no
// restart": the same probe reports failed while the root is gone and ready once
// it exists again.
func TestWorkspaceProbeReactsToRepair(t *testing.T) {
	readiness.Clear()
	t.Cleanup(readiness.Clear)
	fs := workspace.NewFSService(nil)
	if err := readiness.Register(readiness.Spec{Probe: workspaceProbe(fs)}); err != nil {
		t.Fatalf("register workspace probe: %v", err)
	}
	root := filepath.Join(t.TempDir(), "late")
	fs.SetAllowedRoots([]string{root})

	before := readiness.Run(context.Background())[readiness.ComponentWorkspace]
	if before.Result.State != readiness.StateFailed {
		t.Fatalf("before repair: want failed, got %+v", before.Result)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("create root: %v", err)
	}
	after := readiness.Run(context.Background())[readiness.ComponentWorkspace]
	if after.Result.State != readiness.StateReady {
		t.Fatalf("after repair: want ready, got %+v", after.Result)
	}
}

// TestAllowedRootsReturnsCopy covers the read accessor the probe depends on: the
// caller cannot mutate the service's root list through the returned slice.
func TestAllowedRootsReturnsCopy(t *testing.T) {
	fs := workspace.NewFSService(nil)
	root := t.TempDir()
	fs.SetAllowedRoots([]string{root})

	roots := fs.AllowedRoots()
	if len(roots) != 1 {
		t.Fatalf("AllowedRoots = %v", roots)
	}
	roots[0] = "tampered"
	if got := fs.AllowedRoots(); len(got) != 1 || got[0] == "tampered" {
		t.Fatalf("AllowedRoots exposed the internal slice: %v", got)
	}
	if fs.AllowedRoots() == nil {
		t.Fatalf("AllowedRoots must return a non-nil empty slice when unconfigured")
	}
}

// TestNotConfiguredProbesCarryTheirOwningCard covers the planned-but-unwired
// dependencies: each one reports not_configured with the card that will wire it,
// so /ready explains the gap instead of hiding it.
func TestNotConfiguredProbesCarryTheirOwningCard(t *testing.T) {
	components := runProductionProbes(t)

	cases := map[string]string{
		readiness.ComponentDatabase:                 "T1.01",
		readiness.ComponentMigrations:               "T1.01",
		readiness.ComponentEventStore:               "T1.05",
		readiness.ComponentOutboxDispatcher:         "T1.05",
		readiness.ComponentVault:                    "T2.04",
		readiness.ExecBackendPrefix + "claude_code": "T1.13",
		readiness.ExecBackendPrefix + "codex":       "T4.01",
		readiness.ExecBackendPrefix + "gemini":      "T4.02",
	}
	for name, card := range cases {
		component, ok := components[name]
		if !ok {
			t.Fatalf("probe %q missing: %v", name, keys(components))
		}
		if component.Result.State != readiness.StateNotConfigured {
			t.Fatalf("%s: want not_configured, got %+v", name, component.Result)
		}
		if component.Result.ErrCode != readiness.CodeNotConfigured {
			t.Fatalf("%s: want %s, got %q", name, readiness.CodeNotConfigured, component.Result.ErrCode)
		}
		if !strings.Contains(component.Result.Detail, card) {
			t.Fatalf("%s: detail must name the owning card %s, got %q", name, card, component.Result.Detail)
		}
	}
}

// runProductionProbes applies the production probe set and runs it once,
// returning the component snapshot.
func runProductionProbes(t *testing.T) map[string]readiness.Component {
	t.Helper()
	services := newFullTestServices()
	services.Reset()
	t.Cleanup(services.Reset)
	readiness.Clear()
	clearPolicyGlobals(t)
	t.Cleanup(readiness.Clear)

	if err := services.Apply(); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	components := readiness.Run(context.Background())
	if len(components) != len(expectedReadinessProbes) {
		t.Fatalf("run returned %d components, want %d (%v)", len(components), len(expectedReadinessProbes), keys(components))
	}
	return components
}

func keys(components map[string]readiness.Component) []string {
	names := make([]string, 0, len(components))
	for name := range components {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
