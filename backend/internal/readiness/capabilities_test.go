package readiness

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// readyComponent builds a ready component for a hand-built snapshot.
func readyComponent(name string) Component {
	return Component{Name: name, Readonly: true, Result: Result{State: StateReady}}
}

// componentWith builds a component with an explicit state and error code.
func componentWith(name string, state State, errCode string) Component {
	return Component{Name: name, Readonly: true, Result: Result{State: state, ErrCode: errCode}}
}

// fullSnapshot returns a snapshot where every dependency of the plan's
// enumeration is ready, with two executable backends registered.
func fullSnapshot() map[string]Component {
	components := map[string]Component{
		ComponentFrontendProtocol: readyComponent(ComponentFrontendProtocol),
		ComponentPolicy:           readyComponent(ComponentPolicy),
		ComponentWorkspace:        readyComponent(ComponentWorkspace),
		ComponentDatabase:         readyComponent(ComponentDatabase),
		ComponentMigrations:       readyComponent(ComponentMigrations),
		ComponentEventStore:       readyComponent(ComponentEventStore),
		ComponentOutboxDispatcher: readyComponent(ComponentOutboxDispatcher),
		ComponentVault:            readyComponent(ComponentVault),
	}
	for _, backend := range []string{"claude_code", "codex"} {
		name := ExecBackendPrefix + backend
		components[name] = readyComponent(name)
	}
	return components
}

func blockerComponents(capability Capability) []string {
	names := make([]string, 0, len(capability.Blocking))
	for _, blocker := range capability.Blocking {
		names = append(names, blocker.Component)
	}
	return names
}

func blockerFor(t *testing.T, capability Capability, component string) Blocker {
	t.Helper()
	for _, blocker := range capability.Blocking {
		if blocker.Component == component {
			return blocker
		}
	}
	t.Fatalf("blocker %q missing from %+v", component, capability.Blocking)
	return Blocker{}
}

func assertBlockers(t *testing.T, label string, capability Capability, want []string) {
	t.Helper()
	got := blockerComponents(capability)
	sorted := append([]string{}, want...)
	sort.Strings(sorted)
	if !reflect.DeepEqual(got, sorted) {
		t.Fatalf("%s: want blockers %v, got %v", label, sorted, got)
	}
}

// TestEvaluateCapabilitiesAllReady covers the healthy snapshot: the three
// capability sets are ready, every registered backend is executable, and a
// ready capability publishes an empty (never nil) blocking list.
func TestEvaluateCapabilitiesAllReady(t *testing.T) {
	capabilities := EvaluateCapabilities(fullSnapshot(), true, nil)

	for name, capability := range map[string]Capability{
		"read_only": capabilities.ReadOnly,
		"execution": capabilities.Execution,
		"merge":     capabilities.Merge,
	} {
		if capability.State != CapabilityReady {
			t.Fatalf("%s: want ready, got %s (blocking %+v)", name, capability.State, capability.Blocking)
		}
		if capability.Blocking == nil {
			t.Fatalf("%s: blocking must be an empty slice, not nil", name)
		}
		if len(capability.Blocking) != 0 {
			t.Fatalf("%s: want no blockers, got %+v", name, capability.Blocking)
		}
	}
	if got := len(capabilities.Backends); got != 2 {
		t.Fatalf("want 2 backends, got %d (%+v)", got, capabilities.Backends)
	}
	for _, backend := range []string{"claude_code", "codex"} {
		if capabilities.Backends[backend].State != CapabilityReady {
			t.Fatalf("backend %s: want ready, got %+v", backend, capabilities.Backends[backend])
		}
	}
}

// TestEvaluateCapabilitiesLegacyReadOnlyBlocksAll covers the legacy Has* layer
// being incomplete: read_only is unavailable and names the missing services,
// and execution/merge cannot be ready while read_only is not.
func TestEvaluateCapabilitiesLegacyReadOnlyBlocksAll(t *testing.T) {
	capabilities := EvaluateCapabilities(fullSnapshot(), false, []string{"planner", "memory"})

	if capabilities.ReadOnly.State != CapabilityUnavailable {
		t.Fatalf("read_only: want unavailable, got %s", capabilities.ReadOnly.State)
	}
	assertBlockers(t, "read_only", capabilities.ReadOnly, []string{"memory", "planner"})
	for _, name := range []string{"memory", "planner"} {
		blocker := blockerFor(t, capabilities.ReadOnly, name)
		if blocker.State != StateNotConfigured {
			t.Fatalf("%s: want state not_configured, got %s", name, blocker.State)
		}
		if blocker.ErrCode != CodeNotConfigured {
			t.Fatalf("%s: want code %s, got %s", name, CodeNotConfigured, blocker.ErrCode)
		}
		if blocker.Remediation != RemediationDependencyNotReady {
			t.Fatalf("%s: want remediation %s, got %s", name, RemediationDependencyNotReady, blocker.Remediation)
		}
	}
	// Every registered backend carries the legacy blockers; the global verdict
	// adds the aggregate "no executable backend" marker.
	for _, capability := range []Capability{capabilities.Execution, capabilities.Merge} {
		if capability.State != CapabilityUnavailable {
			t.Fatalf("want unavailable while read_only is unavailable, got %s", capability.State)
		}
		assertBlockers(t, "execution/merge", capability, []string{ComponentExecBackend, "memory", "planner"})
	}
	for backend, capability := range capabilities.Backends {
		assertBlockers(t, "backend "+backend, capability, []string{"memory", "planner"})
	}
}

// TestEvaluateCapabilitiesVaultLocked covers the plan's remediation mapping for
// a locked vault: execution and merge are unavailable with vault_locked while
// read_only stays ready (global reading must not be gated on the vault).
func TestEvaluateCapabilitiesVaultLocked(t *testing.T) {
	components := fullSnapshot()
	components[ComponentVault] = componentWith(ComponentVault, StateFailed, RemediationVaultLocked)

	capabilities := EvaluateCapabilities(components, true, nil)

	if capabilities.ReadOnly.State != CapabilityReady {
		t.Fatalf("read_only must not be gated on the vault: %+v", capabilities.ReadOnly)
	}
	for name, capability := range map[string]Capability{
		"execution": capabilities.Execution,
		"merge":     capabilities.Merge,
	} {
		if capability.State != CapabilityUnavailable {
			t.Fatalf("%s: want unavailable, got %s", name, capability.State)
		}
		assertBlockers(t, name, capability, []string{ComponentExecBackend, ComponentVault})
		blocker := blockerFor(t, capability, ComponentVault)
		if blocker.State != StateFailed || blocker.ErrCode != RemediationVaultLocked || blocker.Remediation != RemediationVaultLocked {
			t.Fatalf("%s: unexpected vault blocker %+v", name, blocker)
		}
	}
	for backend, capability := range capabilities.Backends {
		if capability.State != CapabilityUnavailable {
			t.Fatalf("backend %s: want unavailable, got %+v", backend, capability)
		}
		assertBlockers(t, "backend "+backend, capability, []string{ComponentVault})
	}
}

// TestEvaluateCapabilitiesSingleBackendReady covers per-backend separation: one
// executable backend makes the global execution capability ready while the
// other backend reports backend_not_installed for itself.
func TestEvaluateCapabilitiesSingleBackendReady(t *testing.T) {
	components := fullSnapshot()
	claudeName := ExecBackendPrefix + "claude_code"
	components[claudeName] = componentWith(claudeName, StateNotConfigured, CodeNotConfigured)

	capabilities := EvaluateCapabilities(components, true, nil)

	if capabilities.Execution.State != CapabilityReady {
		t.Fatalf("execution: want ready when one backend is ready, got %+v", capabilities.Execution)
	}
	if capabilities.Merge.State != CapabilityReady {
		t.Fatalf("merge: want ready, got %+v", capabilities.Merge)
	}

	claude := capabilities.Backends["claude_code"]
	if claude.State != CapabilityUnavailable {
		t.Fatalf("claude_code: want unavailable, got %+v", claude)
	}
	assertBlockers(t, "claude_code", claude, []string{claudeName})
	blocker := blockerFor(t, claude, claudeName)
	if blocker.State != StateNotConfigured {
		t.Fatalf("claude_code: want state not_configured, got %s", blocker.State)
	}
	if blocker.Remediation != RemediationBackendNotInstalled {
		t.Fatalf("claude_code: want remediation %s, got %s", RemediationBackendNotInstalled, blocker.Remediation)
	}
	codex := capabilities.Backends["codex"]
	if codex.State != CapabilityReady {
		t.Fatalf("codex: want ready, got %+v", codex)
	}
	if len(codex.Blocking) != 0 {
		t.Fatalf("codex: want no blockers, got %+v", codex.Blocking)
	}
	if capabilities.Execution.Blocking == nil {
		t.Fatalf("ready execution must still publish an empty blocking list")
	}
}

// TestEvaluateCapabilitiesNoExecutableBackend covers every backend being
// unavailable: the global execution capability carries the aggregate
// no-executable-backend marker and no per-backend blocker (those differ per
// backend and would hide the common cause).
func TestEvaluateCapabilitiesNoExecutableBackend(t *testing.T) {
	components := fullSnapshot()
	for _, backend := range []string{"claude_code", "codex"} {
		name := ExecBackendPrefix + backend
		components[name] = componentWith(name, StateNotConfigured, CodeNotConfigured)
	}

	capabilities := EvaluateCapabilities(components, true, nil)

	if capabilities.Execution.State != CapabilityUnavailable {
		t.Fatalf("execution: want unavailable, got %+v", capabilities.Execution)
	}
	assertBlockers(t, "execution", capabilities.Execution, []string{ComponentExecBackend})
	aggregate := blockerFor(t, capabilities.Execution, ComponentExecBackend)
	if aggregate.Remediation != RemediationBackendNotInstalled {
		t.Fatalf("aggregate blocker: want %s, got %s", RemediationBackendNotInstalled, aggregate.Remediation)
	}
	// The per-backend capabilities keep their own detail.
	assertBlockers(t, "claude_code", capabilities.Backends["claude_code"],
		[]string{ExecBackendPrefix + "claude_code"})
	assertBlockers(t, "codex", capabilities.Backends["codex"], []string{ExecBackendPrefix + "codex"})
}

// TestEvaluateCapabilitiesMissingComponentsAreNotConfigured covers an empty
// snapshot: nothing was proven usable, so read_only is blocked by the frontend
// protocol and every execution dependency is reported not_configured with its
// remediation.
func TestEvaluateCapabilitiesMissingComponentsAreNotConfigured(t *testing.T) {
	capabilities := EvaluateCapabilities(nil, true, nil)

	if capabilities.ReadOnly.State != CapabilityUnavailable {
		t.Fatalf("read_only: want unavailable without frontend_protocol, got %+v", capabilities.ReadOnly)
	}
	protocol := blockerFor(t, capabilities.ReadOnly, ComponentFrontendProtocol)
	if protocol.State != StateNotConfigured || protocol.Remediation != RemediationProtocolMismatch {
		t.Fatalf("frontend_protocol blocker: %+v", protocol)
	}

	assertBlockers(t, "execution", capabilities.Execution, []string{
		ComponentEventStore, ComponentExecBackend, ComponentFrontendProtocol, ComponentMigrations,
		ComponentOutboxDispatcher, ComponentPolicy, ComponentVault, ComponentWorkspace,
	})
	for _, name := range []string{ComponentMigrations, ComponentOutboxDispatcher, ComponentVault, ComponentPolicy, ComponentWorkspace} {
		blocker := blockerFor(t, capabilities.Execution, name)
		if blocker.State != StateNotConfigured || blocker.ErrCode != CodeNotConfigured {
			t.Fatalf("%s: want not_configured/%s, got %+v", name, CodeNotConfigured, blocker)
		}
	}
	wantRemediation := map[string]string{
		ComponentMigrations:       RemediationMigrationsPending,
		ComponentOutboxDispatcher: RemediationOutboxUnavailable,
		ComponentVault:            RemediationVaultLocked,
		ComponentPolicy:           RemediationPolicyNotInstalled,
		ComponentWorkspace:        RemediationWorkspaceRootsUnconfigured,
	}
	for name, remediation := range wantRemediation {
		if got := blockerFor(t, capabilities.Execution, name).Remediation; got != remediation {
			t.Fatalf("%s remediation: want %s, got %s", name, remediation, got)
		}
	}
}

// TestEvaluateCapabilitiesWorkspaceRemediation covers the two workspace
// failures having distinct remediation codes.
func TestEvaluateCapabilitiesWorkspaceRemediation(t *testing.T) {
	cases := []struct {
		name        string
		errCode     string
		remediation string
	}{
		{"missing root", CodeWorkspaceRootMissing, RemediationWorkspaceRootMissing},
		{"unconfigured roots", CodeWorkspaceRootsUnconfigured, RemediationWorkspaceRootsUnconfigured},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			components := fullSnapshot()
			components[ComponentWorkspace] = componentWith(ComponentWorkspace, StateFailed, tc.errCode)

			capabilities := EvaluateCapabilities(components, true, nil)

			for name, capability := range map[string]Capability{
				"execution": capabilities.Execution,
				"merge":     capabilities.Merge,
			} {
				if capability.State != CapabilityUnavailable {
					t.Fatalf("%s: want unavailable, got %+v", name, capability)
				}
				blocker := blockerFor(t, capability, ComponentWorkspace)
				if blocker.ErrCode != tc.errCode || blocker.Remediation != tc.remediation {
					t.Fatalf("%s: workspace blocker %+v", name, blocker)
				}
			}
		})
	}
}

// TestEvaluateCapabilitiesMergeBlockedWhenMergeDependencyNotReady pins that
// merge re-checks its own dependencies rather than copying execution's verdict.
func TestEvaluateCapabilitiesMergeBlockedWhenMergeDependencyNotReady(t *testing.T) {
	components := fullSnapshot()
	components[ComponentWorkspace] = componentWith(ComponentWorkspace, StateFailed, CodeWorkspaceRootMissing)

	capabilities := EvaluateCapabilities(components, true, nil)

	// workspace is an execution dependency too (plan section 15 T0.12 step 3),
	// so both capabilities are unavailable and both name workspace.
	if capabilities.Execution.State != CapabilityUnavailable {
		t.Fatalf("execution: workspace must gate execution as well, got %+v", capabilities.Execution)
	}
	assertBlockers(t, "merge", capabilities.Merge, []string{ComponentExecBackend, ComponentWorkspace})
}

// TestCapabilitiesBlockingOrderIsStable covers the payload being deterministic:
// blockers are sorted by component then error code regardless of map iteration
// order.
func TestCapabilitiesBlockingOrderIsStable(t *testing.T) {
	components := fullSnapshot()
	components[ComponentVault] = componentWith(ComponentVault, StateFailed, RemediationVaultLocked)
	components[ComponentEventStore] = componentWith(ComponentEventStore, StateNotConfigured, CodeNotConfigured)
	components[ComponentMigrations] = componentWith(ComponentMigrations, StateFailed, RemediationMigrationsPending)
	components[ComponentPolicy] = componentWith(ComponentPolicy, StateFailed, CodePolicyNotInstalled)

	first := EvaluateCapabilities(components, false, []string{"samg", "planner"})
	for i := 0; i < 20; i++ {
		next := EvaluateCapabilities(components, false, []string{"samg", "planner"})
		if !reflect.DeepEqual(first, next) {
			t.Fatalf("evaluation %d differs from the first:\n%+v\n%+v", i, first, next)
		}
	}
	assertBlockers(t, "execution", first.Execution, []string{
		ComponentEventStore, ComponentExecBackend, ComponentMigrations, "planner", ComponentPolicy, "samg", ComponentVault,
	})
	assertBlockers(t, "read_only", first.ReadOnly, []string{"planner", "samg"})
}

// TestCapabilitiesJSONShape pins the wire contract the OpenAPI schema and the
// StartupGate consume: read_only/execution/merge each carry state+blocking,
// execution carries the per-backend map, and blocking is always an array.
func TestCapabilitiesJSONShape(t *testing.T) {
	components := fullSnapshot()
	components[ComponentVault] = componentWith(ComponentVault, StateFailed, RemediationVaultLocked)

	encoded, err := json.Marshal(EvaluateCapabilities(components, true, nil))
	if err != nil {
		t.Fatalf("marshal capabilities: %v", err)
	}
	var decoded struct {
		ReadOnly  map[string]any `json:"read_only"`
		Execution struct {
			State    string                    `json:"state"`
			Blocking []map[string]any          `json:"blocking"`
			Backends map[string]map[string]any `json:"backends"`
		} `json:"execution"`
		Merge map[string]any `json:"merge"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal capabilities: %v (body %s)", err, encoded)
	}
	if decoded.ReadOnly["state"] != string(CapabilityReady) {
		t.Fatalf("read_only: %v", decoded.ReadOnly)
	}
	if blocking, ok := decoded.ReadOnly["blocking"].([]any); !ok || len(blocking) != 0 {
		t.Fatalf("read_only.blocking must be an empty array: %#v", decoded.ReadOnly["blocking"])
	}
	if decoded.Execution.State != string(CapabilityUnavailable) {
		t.Fatalf("execution: %v", decoded.Execution.State)
	}
	if len(decoded.Execution.Blocking) != 2 {
		t.Fatalf("execution.blocking: %#v", decoded.Execution.Blocking)
	}
	var vaultBlocker map[string]any
	for _, blocker := range decoded.Execution.Blocking {
		if blocker["component"] == ComponentVault {
			vaultBlocker = blocker
		}
		for _, key := range []string{"component", "state", "error_code", "remediation"} {
			if _, ok := blocker[key]; !ok {
				t.Fatalf("blocker %#v lacks %q", blocker, key)
			}
		}
	}
	if vaultBlocker == nil {
		t.Fatalf("vault blocker missing: %#v", decoded.Execution.Blocking)
	}
	if vaultBlocker["remediation"] != RemediationVaultLocked || vaultBlocker["error_code"] != RemediationVaultLocked {
		t.Fatalf("vault blocker: %#v", vaultBlocker)
	}
	backends, ok := decoded.Execution.Backends["claude_code"]
	if !ok {
		t.Fatalf("execution.backends.claude_code missing: %#v", decoded.Execution.Backends)
	}
	if backends["state"] != string(CapabilityUnavailable) {
		t.Fatalf("claude_code state: %#v", backends["state"])
	}
	blocking, ok := backends["blocking"].([]any)
	if !ok || len(blocking) != 1 {
		t.Fatalf("claude_code.blocking: %#v", backends["blocking"])
	}
	if decoded.Merge["state"] != string(CapabilityUnavailable) {
		t.Fatalf("merge: %#v", decoded.Merge)
	}
	// The Go-side Backends field must not leak as a sibling of execution.
	var raw map[string]any
	if err := json.Unmarshal(encoded, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	for _, leaked := range []string{"backends", "Backends", "ReadOnly", "ReadOnlyCapability"} {
		if _, found := raw[leaked]; found {
			t.Fatalf("field %q leaked into the capability payload: %s", leaked, encoded)
		}
	}
}

// TestEvaluateCapabilitiesProductionShapedNotConfiguredVault covers the exact
// production shape: frontend_protocol/policy/workspace ready, every runtime
// dependency and all three backends not_configured. read_only is ready (the
// legacy services plus the protocol are the whole requirement) and execution
// names the unwired dependencies, not the per-backend details that differ.
func TestEvaluateCapabilitiesProductionShapedNotConfiguredVault(t *testing.T) {
	components := map[string]Component{
		ComponentFrontendProtocol: readyComponent(ComponentFrontendProtocol),
		ComponentPolicy:           readyComponent(ComponentPolicy),
		ComponentWorkspace:        readyComponent(ComponentWorkspace),
		ComponentDatabase:         componentWith(ComponentDatabase, StateNotConfigured, CodeNotConfigured),
		ComponentMigrations:       componentWith(ComponentMigrations, StateNotConfigured, CodeNotConfigured),
		ComponentEventStore:       componentWith(ComponentEventStore, StateNotConfigured, CodeNotConfigured),
		ComponentOutboxDispatcher: componentWith(ComponentOutboxDispatcher, StateNotConfigured, CodeNotConfigured),
		ComponentVault:            componentWith(ComponentVault, StateNotConfigured, CodeNotConfigured),
	}
	for _, backend := range []string{"claude_code", "codex", "gemini"} {
		name := ExecBackendPrefix + backend
		components[name] = componentWith(name, StateNotConfigured, CodeNotConfigured)
	}

	capabilities := EvaluateCapabilities(components, true, nil)

	if capabilities.ReadOnly.State != CapabilityReady {
		t.Fatalf("read_only: want ready, got %+v", capabilities.ReadOnly)
	}
	assertBlockers(t, "execution", capabilities.Execution, []string{
		ComponentEventStore, ComponentExecBackend, ComponentMigrations, ComponentOutboxDispatcher, ComponentVault,
	})
	assertBlockers(t, "merge", capabilities.Merge, []string{
		ComponentEventStore, ComponentExecBackend, ComponentMigrations, ComponentOutboxDispatcher, ComponentVault,
	})
	if got := blockerFor(t, capabilities.Execution, ComponentVault).Remediation; got != RemediationVaultLocked {
		t.Fatalf("vault remediation: %s", got)
	}
	for backend, capability := range capabilities.Backends {
		if capability.State != CapabilityUnavailable {
			t.Fatalf("backend %s: want unavailable, got %+v", backend, capability)
		}
		assertBlockers(t, "backend "+backend, capability, []string{
			ComponentEventStore, ExecBackendPrefix + backend, ComponentMigrations,
			ComponentOutboxDispatcher, ComponentVault,
		})
	}
}

// TestCheckExecutionRechecksEveryCall is the plan's "a new dispatch must
// re-check" requirement: every call runs the probes again, so a dependency
// repaired between two calls is reflected by the second one.
func TestCheckExecutionRechecksEveryCall(t *testing.T) {
	registry := NewRegistry()
	var calls atomic.Int64
	var vaultReady atomic.Bool
	if err := registry.Register(Spec{
		Probe: NewProbeFunc(ComponentVault, func(context.Context) Result {
			calls.Add(1)
			if vaultReady.Load() {
				return Result{State: StateReady}
			}
			return Result{State: StateFailed, ErrCode: RemediationVaultLocked, Detail: "sealed"}
		}),
		Timeout: time.Second,
	}); err != nil {
		t.Fatalf("register vault probe: %v", err)
	}

	first := CheckExecution(context.Background(), registry, "codex", nil)
	if first.State != CapabilityUnavailable {
		t.Fatalf("first check: want unavailable, got %+v", first)
	}
	blocker := blockerFor(t, first, ComponentVault)
	if blocker.Remediation != RemediationVaultLocked || blocker.ErrCode != RemediationVaultLocked {
		t.Fatalf("first check blocker: %+v", blocker)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("first check must run the probe once, ran %d times", got)
	}

	// Repair the dependency: no restart, no cache invalidation call.
	vaultReady.Store(true)

	second := CheckExecution(context.Background(), registry, "codex", nil)
	if got := calls.Load(); got != 2 {
		t.Fatalf("second check must re-run the probe (want 2 runs), ran %d times", got)
	}
	if second.State != CapabilityUnavailable {
		t.Fatalf("second check: want unavailable because codex itself is unwired, got %+v", second)
	}
	for _, blocker := range second.Blocking {
		if blocker.Component == ComponentVault {
			t.Fatalf("repaired vault still reported: %+v", second)
		}
	}
	// The repaired vault must be observable by the aggregate evaluation too.
	full := EvaluateCapabilities(registry.Run(context.Background()), true, nil)
	for _, blocker := range full.ReadOnly.Blocking {
		if blocker.Component == ComponentVault {
			t.Fatalf("repaired vault still blocks read_only: %+v", full.ReadOnly)
		}
	}
	for _, blocker := range full.Execution.Blocking {
		if blocker.Component == ComponentVault {
			t.Fatalf("repaired vault still blocks execution: %+v", full.Execution)
		}
	}
}

// TestCheckExecutionOnlyEvaluatesNamedBackend covers the per-backend scope: a
// backend that is not ready does not appear in another backend's blockers, and
// an unregistered backend is reported not_configured.
func TestCheckExecutionOnlyEvaluatesNamedBackend(t *testing.T) {
	registry := NewRegistry()
	specs := []Spec{
		{Probe: NewProbeFunc(ComponentFrontendProtocol, func(context.Context) Result {
			return Result{State: StateReady, Detail: "protocol_version=" + FrontendProtocolVersion}
		})},
		{Probe: NewProbeFunc(ComponentPolicy, func(context.Context) Result { return Result{State: StateReady} })},
		{Probe: NewProbeFunc(ComponentWorkspace, func(context.Context) Result { return Result{State: StateReady} })},
		{Probe: NewProbeFunc(ComponentMigrations, func(context.Context) Result { return Result{State: StateReady} })},
		{Probe: NewProbeFunc(ComponentEventStore, func(context.Context) Result { return Result{State: StateReady} })},
		{Probe: NewProbeFunc(ComponentOutboxDispatcher, func(context.Context) Result { return Result{State: StateReady} })},
		{Probe: NewProbeFunc(ComponentVault, func(context.Context) Result { return Result{State: StateReady} })},
		{Probe: NewNotConfiguredProbe(ExecBackendPrefix+"claude_code", "wired by T1.13")},
		{Probe: NewProbeFunc(ExecBackendPrefix+"codex", func(context.Context) Result {
			return Result{State: StateReady, Detail: "fake backend"}
		})},
	}
	if err := registry.Register(specs...); err != nil {
		t.Fatalf("register probes: %v", err)
	}

	codex := CheckExecution(context.Background(), registry, "codex", nil)
	if codex.State != CapabilityReady {
		t.Fatalf("codex: want ready, got %+v", codex)
	}
	claude := CheckExecution(context.Background(), registry, "claude_code", nil)
	if claude.State != CapabilityUnavailable {
		t.Fatalf("claude_code: want unavailable, got %+v", claude)
	}
	assertBlockers(t, "claude_code", claude, []string{ExecBackendPrefix + "claude_code"})

	unknown := CheckExecution(context.Background(), registry, "gemini", nil)
	if unknown.State != CapabilityUnavailable {
		t.Fatalf("gemini: want unavailable, got %+v", unknown)
	}
	blocker := blockerFor(t, unknown, ExecBackendPrefix+"gemini")
	if blocker.State != StateNotConfigured || blocker.Remediation != RemediationBackendNotInstalled {
		t.Fatalf("gemini blocker: %+v", blocker)
	}
	// The unregistered backend must not leak into the registered ones.
	assertBlockers(t, "codex", codex, []string{})
}

// TestCheckExecutionLegacyReadOnlyGate covers the legacy Has* layer still
// gating execution: a missing required service blocks execution even when every
// probe is ready.
func TestCheckExecutionLegacyReadOnlyGate(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(Spec{
		Probe: NewProbeFunc(ComponentVault, func(context.Context) Result { return Result{State: StateReady} }),
	}); err != nil {
		t.Fatalf("register probes: %v", err)
	}

	capability := CheckExecution(context.Background(), registry, "codex", func() (bool, []string) {
		return false, []string{"planner", "audit"}
	})
	if capability.State != CapabilityUnavailable {
		t.Fatalf("want unavailable, got %+v", capability)
	}
	blocker := blockerFor(t, capability, "planner")
	if blocker.ErrCode != CodeNotConfigured || blocker.Remediation != RemediationDependencyNotReady {
		t.Fatalf("planner blocker: %+v", blocker)
	}
	blockerFor(t, capability, "audit")
	// Everything not registered in this registry is not_configured, including
	// the execution dependencies and the backend itself; the registered vault
	// probe is ready, so it is not a blocker.
	blockerFor(t, capability, ComponentEventStore)
	blockerFor(t, capability, ExecBackendPrefix+"codex")
	for _, blocker := range capability.Blocking {
		if blocker.Component == ComponentVault {
			t.Fatalf("the ready vault probe must not block: %+v", capability)
		}
	}
}

// TestCheckExecutionNilRegistry covers a caller with no registry: the
// evaluation still answers, reporting every dependency as not_configured rather
// than panicking.
func TestCheckExecutionNilRegistry(t *testing.T) {
	capability := CheckExecution(context.Background(), nil, "codex", nil)
	if capability.State != CapabilityUnavailable {
		t.Fatalf("want unavailable, got %+v", capability)
	}
	if len(capability.Blocking) == 0 {
		t.Fatalf("want blockers for an empty registry, got %+v", capability)
	}
	if got := blockerFor(t, capability, ComponentVault).Remediation; got != RemediationVaultLocked {
		t.Fatalf("vault remediation: %s", got)
	}
}

// TestRemediationForTable pins the remediation mapping in one place: every
// component of the plan's enumeration maps to its documented code, and unknown
// components fall back to dependency_not_ready.
func TestRemediationForTable(t *testing.T) {
	cases := []struct {
		name     string
		errCode  string
		expected string
	}{
		{ExecBackendPrefix + "claude_code", CodeNotConfigured, RemediationBackendNotInstalled},
		{ComponentMigrations, CodeNotConfigured, RemediationMigrationsPending},
		{ComponentVault, RemediationVaultLocked, RemediationVaultLocked},
		{ComponentOutboxDispatcher, CodeFailed, RemediationOutboxUnavailable},
		{ComponentEventStore, CodeFailed, RemediationEventStoreUnavailable},
		{ComponentWorkspace, CodeWorkspaceRootMissing, RemediationWorkspaceRootMissing},
		{ComponentWorkspace, CodeWorkspaceRootsUnconfigured, RemediationWorkspaceRootsUnconfigured},
		{ComponentWorkspace, CodeFailed, RemediationWorkspaceRootsUnconfigured},
		{ComponentPolicy, CodePolicyNotInstalled, RemediationPolicyNotInstalled},
		{ComponentFrontendProtocol, CodeFailed, RemediationProtocolMismatch},
		{ComponentDatabase, CodeFailed, RemediationDependencyNotReady},
		{"something_else", CodeFailed, RemediationDependencyNotReady},
	}
	for _, tc := range cases {
		if got := RemediationFor(tc.name, tc.errCode); got != tc.expected {
			t.Fatalf("RemediationFor(%q, %q) = %q, want %q", tc.name, tc.errCode, got, tc.expected)
		}
	}
}

// TestFrontendProtocolVersionIsTheHandshakeVersion keeps the probe detail and
// the handshake from drifting apart: the constant is the single source of truth
// for both (api/router.go writes it into the startup handshake).
func TestFrontendProtocolVersionIsTheHandshakeVersion(t *testing.T) {
	if FrontendProtocolVersion != "1" {
		t.Fatalf("FrontendProtocolVersion = %q; the handshake contract is 1 (api/handshake_test.go)", FrontendProtocolVersion)
	}
	if !strings.HasPrefix(ExecBackendPrefix, "exec_backend:") {
		t.Fatalf("ExecBackendPrefix = %q", ExecBackendPrefix)
	}
}
