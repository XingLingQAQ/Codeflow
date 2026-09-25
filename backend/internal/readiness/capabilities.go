// Capability sets derived from one readiness snapshot (plan section 15 T0.12
// step 3 / section 28 T0.12.b).
//
// The readiness registry answers "is this dependency usable right now"; the
// capability evaluation turns that answer into the three questions the shell
// and the run API actually ask:
//
//   - read_only: may the user browse and read? Global and project-independent.
//   - execution: may a Run be created and dispatched for one backend? Per
//     backend, and deliberately separate from read_only: a locked vault or an
//     unwired run store must not hide the read-only surface.
//   - merge: may work be merged back? Execution plus the merge-specific
//     dependencies (workspace today; T1.09 appends more).
//
// Everything here is a pure function of the snapshot: no globals, no I/O, no
// caching. A caller that must not act on a stale verdict (the run creation
// path, T1.04) calls CheckExecution, which re-runs the probes on every call
// instead of trusting an earlier /ready answer.
package readiness

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
)

// FrontendProtocolVersion is the sidecar handshake protocol version the backend
// speaks with the desktop shell. It is the single source of truth for both the
// startup handshake (api/router.go) and the frontend_protocol probe, so the two
// cannot drift apart when the protocol is bumped.
const FrontendProtocolVersion = "1"

// Component names of the built-in probes (plan section 15 T0.12 step 1). The
// bootstrap registers these names; the capability evaluation reads them back.
const (
	ComponentFrontendProtocol = "frontend_protocol"
	ComponentPolicy           = "policy"
	ComponentWorkspace        = "workspace"
	ComponentDatabase         = "database"
	ComponentMigrations       = "migrations"
	ComponentEventStore       = "event_store"
	ComponentOutboxDispatcher = "outbox_dispatcher"
	ComponentVault            = "vault"
	// ExecBackendPrefix prefixes one probe per execution backend:
	// exec_backend:claude_code, exec_backend:codex, exec_backend:gemini. The
	// backend name in the capability payload is the suffix without this prefix.
	ExecBackendPrefix = "exec_backend:"
	// ComponentExecBackend is the aggregate blocker the global execution verdict
	// adds when no backend can execute at all. It is deliberately not a probe
	// name: the per-backend probes carry the prefix, this marker names the class.
	ComponentExecBackend = "exec_backend"
)

// Component error codes emitted by the production probes. They are stable
// machine-readable reasons; the runner's own codes (probe_timeout, ...) live in
// readiness.go.
const (
	// CodePolicyNotInstalled marks a policy probe that found no evaluator or no
	// enforcement requirement.
	CodePolicyNotInstalled = "policy_not_installed"
	// CodeWorkspaceRootsUnconfigured marks a workspace that has no allowed root
	// configured yet (nothing was restricted, nothing was proven usable).
	CodeWorkspaceRootsUnconfigured = "workspace_roots_unconfigured"
	// CodeWorkspaceRootMissing marks a configured workspace root that does not
	// exist or is not a directory.
	CodeWorkspaceRootMissing = "workspace_root_missing"
)

// Remediation codes (plan section 15 T0.12 step 5): the machine-readable action
// a consumer shows or takes when a dependency blocks a capability. The mapping
// from component to code lives in exactly one place, RemediationFor.
const (
	RemediationBackendNotInstalled        = "backend_not_installed"
	RemediationMigrationsPending          = "migrations_pending"
	RemediationVaultLocked                = "vault_locked"
	RemediationOutboxUnavailable          = "outbox_unavailable"
	RemediationEventStoreUnavailable      = "event_store_unavailable"
	RemediationWorkspaceRootsUnconfigured = "workspace_roots_unconfigured"
	RemediationWorkspaceRootMissing       = "workspace_root_missing"
	RemediationPolicyNotInstalled         = "policy_not_installed"
	RemediationProtocolMismatch           = "protocol_mismatch"
	RemediationDependencyNotReady         = "dependency_not_ready"
)

// remediationByComponent is the component -> remediation table. Components that
// need their error code to pick a remediation (workspace) are handled in
// RemediationFor; everything unlisted falls back to
// RemediationDependencyNotReady.
var remediationByComponent = map[string]string{
	ComponentMigrations:       RemediationMigrationsPending,
	ComponentVault:            RemediationVaultLocked,
	ComponentOutboxDispatcher: RemediationOutboxUnavailable,
	ComponentEventStore:       RemediationEventStoreUnavailable,
	ComponentPolicy:           RemediationPolicyNotInstalled,
	ComponentFrontendProtocol: RemediationProtocolMismatch,
}

// remediationByWorkspaceCode distinguishes the two workspace failures: no root
// configured at all versus a configured root that disappeared.
var remediationByWorkspaceCode = map[string]string{
	CodeWorkspaceRootMissing:       RemediationWorkspaceRootMissing,
	CodeWorkspaceRootsUnconfigured: RemediationWorkspaceRootsUnconfigured,
}

// RemediationFor maps one component and its error code to the stable
// remediation code a consumer acts on.
func RemediationFor(name, errCode string) string {
	if strings.HasPrefix(name, ExecBackendPrefix) {
		return RemediationBackendNotInstalled
	}
	if name == ComponentWorkspace {
		if remediation, ok := remediationByWorkspaceCode[errCode]; ok {
			return remediation
		}
		return RemediationWorkspaceRootsUnconfigured
	}
	if remediation, ok := remediationByComponent[name]; ok {
		return remediation
	}
	return RemediationDependencyNotReady
}

// CapabilityState is the two-value capability verdict. It is intentionally
// coarser than State: a capability is either usable or not, and the Blocking
// list carries the nuance.
type CapabilityState string

const (
	// CapabilityReady means every dependency of the capability is ready.
	CapabilityReady CapabilityState = "ready"
	// CapabilityUnavailable means at least one dependency is not ready; see
	// Blocking for which one and what to do about it.
	CapabilityUnavailable CapabilityState = "unavailable"
)

// Blocker is one dependency that keeps a capability from being ready.
type Blocker struct {
	Component string `json:"component"`
	// State is the dependency's readiness state at the time of the check.
	State State `json:"state"`
	// ErrCode is the machine-readable reason (probe error code or a component
	// specific code such as vault_locked).
	ErrCode string `json:"error_code"`
	// Remediation is the machine-readable action that would unblock the
	// dependency (plan section 15 T0.12 step 5).
	Remediation string `json:"remediation"`
}

// Capability is one capability verdict plus, when it is not ready, the blocking
// dependencies in a stable order.
type Capability struct {
	State CapabilityState `json:"state"`
	// Blocking is never nil: a ready capability publishes an empty list.
	Blocking []Blocker `json:"blocking"`
}

// Capabilities is the payload GET /ready publishes under "capabilities" and the
// value CheckExecution returns a slice of. Backends is keyed by backend name
// without the exec_backend: prefix. In Go it is a sibling of Execution because
// the evaluator derives it on its own; MarshalJSON nests it under execution in
// the wire payload the OpenAPI contract describes.
type Capabilities struct {
	ReadOnly  Capability            `json:"read_only"`
	Execution Capability            `json:"execution"`
	Merge     Capability            `json:"merge"`
	Backends  map[string]Capability `json:"backends"`
}

// MarshalJSON publishes the wire shape GET /ready answers with: read_only and
// merge are {state, blocking[]}; execution carries the per-backend map nested
// under it (plan section 15 T0.12 step 3).
func (c Capabilities) MarshalJSON() ([]byte, error) {
	type capabilityJSON struct {
		State    CapabilityState `json:"state"`
		Blocking []Blocker       `json:"blocking"`
	}
	backends := make(map[string]capabilityJSON, len(c.Backends))
	for name, capability := range c.Backends {
		backends[name] = capabilityJSON{
			State:    capability.State,
			Blocking: nonNilBlockers(capability.Blocking),
		}
	}
	type executionJSON struct {
		capabilityJSON
		Backends map[string]capabilityJSON `json:"backends"`
	}
	return json.Marshal(struct {
		ReadOnly  capabilityJSON `json:"read_only"`
		Execution executionJSON  `json:"execution"`
		Merge     capabilityJSON `json:"merge"`
	}{
		ReadOnly: capabilityJSON{State: c.ReadOnly.State, Blocking: nonNilBlockers(c.ReadOnly.Blocking)},
		Execution: executionJSON{
			capabilityJSON: capabilityJSON{State: c.Execution.State, Blocking: nonNilBlockers(c.Execution.Blocking)},
			Backends:       backends,
		},
		Merge: capabilityJSON{State: c.Merge.State, Blocking: nonNilBlockers(c.Merge.Blocking)},
	})
}

// executionDependencies are the dependencies every backend needs on top of the
// read-only set and its own exec_backend:<name> probe (plan section 15 T0.12
// step 3). database is not in this list on purpose: the run store gate is
// decided by T1.01/T1.04 once the store is wired, and migrations already covers
// "the runtime schema is not usable yet".
var executionDependencies = []string{
	ComponentPolicy,
	ComponentWorkspace,
	ComponentMigrations,
	ComponentEventStore,
	ComponentOutboxDispatcher,
	ComponentVault,
}

// EvaluateCapabilities derives the three capability sets from one readiness
// snapshot.
//
// legacyReadOnlyReady is the caller's verdict on the seven required legacy
// services (the Has* layer the API keeps for compatibility) and legacyBlocking
// names the ones that are missing; both are produced by the caller because this
// package must not depend on the HTTP layer. A component that is not registered
// at all counts as not_configured: nothing has proven that dependency usable.
func EvaluateCapabilities(components map[string]Component, legacyReadOnlyReady bool, legacyBlocking []string) Capabilities {
	return evaluateCapabilities(components, legacyReadOnlyReady, legacyBlocking, "")
}

// CheckExecution re-checks one backend's execution capability right before a
// Run is created (plan section 28 T0.12.b: a new dispatch must re-check every
// time, never trust an earlier ready verdict). It always runs the probes again;
// nothing is cached across calls. Only the named backend is evaluated, so an
// unusable backend does not block a different one.
//
// legacyReadOnly is the caller's legacy Has* verdict; a nil value means the
// caller has no legacy layer (the run package) and contributes no legacy
// blockers.
func CheckExecution(ctx context.Context, reg *Registry, backend string, legacyReadOnly func() (bool, []string)) Capability {
	var components map[string]Component
	if reg != nil {
		components = reg.Run(ctx)
	}
	legacyReady := true
	var legacyBlocking []string
	if legacyReadOnly != nil {
		legacyReady, legacyBlocking = legacyReadOnly()
	}
	return evaluateCapabilities(components, legacyReady, legacyBlocking, backend).Execution
}

// evaluateCapabilities is the shared implementation. onlyBackend, when set,
// restricts the evaluation to that one backend and makes the returned Execution
// that backend's capability.
func evaluateCapabilities(components map[string]Component, legacyReadOnlyReady bool, legacyBlocking []string, onlyBackend string) Capabilities {
	readOnlyBlockers := make([]Blocker, 0, len(legacyBlocking)+1)
	for _, name := range legacyBlocking {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		readOnlyBlockers = append(readOnlyBlockers, Blocker{
			Component:   name,
			State:       StateNotConfigured,
			ErrCode:     CodeNotConfigured,
			Remediation: RemediationFor(name, CodeNotConfigured),
		})
	}
	frontendBlocker, frontendReady := componentBlocker(components, ComponentFrontendProtocol)
	if !frontendReady {
		readOnlyBlockers = append(readOnlyBlockers, frontendBlocker)
	}
	readOnlyReady := legacyReadOnlyReady && frontendReady && len(readOnlyBlockers) == 0
	readOnly := capability(readOnlyReady, readOnlyBlockers)

	// Shared blockers: read-only plus every backend-independent execution
	// dependency. A per-backend capability is these plus its own probe.
	sharedBlockers := append([]Blocker(nil), readOnly.Blocking...)
	for _, name := range executionDependencies {
		if blocker, ready := componentBlocker(components, name); !ready {
			sharedBlockers = append(sharedBlockers, blocker)
		}
	}
	sharedReady := readOnlyReady && len(sharedBlockers) == 0

	backendNames := registeredBackends(components, onlyBackend)
	backends := make(map[string]Capability, len(backendNames))
	for _, name := range backendNames {
		blockers := append([]Blocker(nil), sharedBlockers...)
		backendBlocker, backendReady := componentBlocker(components, ExecBackendPrefix+name)
		if !backendReady {
			blockers = append(blockers, backendBlocker)
		}
		backends[name] = capability(sharedReady && backendReady, blockers)
	}

	// The aggregate "no executable backend" marker belongs to the global verdict
	// only; a single-backend query (CheckExecution) already names that backend.
	execution := globalExecution(backendNames, backends, sharedBlockers, onlyBackend == "")
	merge := mergeCapability(components, execution)
	return Capabilities{ReadOnly: readOnly, Execution: execution, Merge: merge, Backends: backends}
}

// globalExecution folds the per-backend capabilities into one verdict: execution
// is ready as soon as one backend is, and when none is, the payload names the
// cause common to every backend instead of one backend's private problem. When
// aggregate is set (the global verdict, not a single-backend query) it also adds
// the marker that says "no backend can execute at all".
func globalExecution(backendNames []string, backends map[string]Capability, sharedBlockers []Blocker, aggregate bool) Capability {
	for _, name := range backendNames {
		if backends[name].State == CapabilityReady {
			return capability(true, nil)
		}
	}
	// No backend is executable. With at least one backend registered, the common
	// blockers are the interesting cause; with none registered at all, the shared
	// dependency blockers are the whole story.
	blockers := commonBlockers(backendNames, backends)
	if len(blockers) == 0 {
		blockers = append([]Blocker(nil), sharedBlockers...)
	}
	if aggregate {
		blockers = append(blockers, noExecutableBackendBlocker())
	}
	return capability(false, blockers)
}

// mergeCapability is execution plus the merge-specific dependencies. Only
// workspace gates it today; T1.09 appends the journal/publisher dependencies.
func mergeCapability(components map[string]Component, execution Capability) Capability {
	if execution.State == CapabilityReady {
		if blocker, ready := componentBlocker(components, ComponentWorkspace); !ready {
			return capability(false, []Blocker{blocker})
		}
		return capability(true, nil)
	}
	blockers := append([]Blocker(nil), execution.Blocking...)
	if blocker, ready := componentBlocker(components, ComponentWorkspace); !ready {
		blockers = append(blockers, blocker)
	}
	return capability(false, blockers)
}

// registeredBackends returns the backend names that have a probe registered,
// sorted. With onlyBackend set it returns just that name, registered or not: an
// unregistered backend is reported as not_configured by componentBlocker.
func registeredBackends(components map[string]Component, onlyBackend string) []string {
	if onlyBackend != "" {
		return []string{onlyBackend}
	}
	names := make([]string, 0, len(components))
	for name := range components {
		if !strings.HasPrefix(name, ExecBackendPrefix) {
			continue
		}
		if backend := strings.TrimPrefix(name, ExecBackendPrefix); backend != "" {
			names = append(names, backend)
		}
	}
	sort.Strings(names)
	return names
}

// componentBlocker reports whether one component is ready and, when it is not,
// the blocker describing it. A component that is not registered counts as
// not_configured.
func componentBlocker(components map[string]Component, name string) (Blocker, bool) {
	component, ok := components[name]
	if !ok {
		return Blocker{
			Component:   name,
			State:       StateNotConfigured,
			ErrCode:     CodeNotConfigured,
			Remediation: RemediationFor(name, CodeNotConfigured),
		}, false
	}
	if component.Result.State == StateReady {
		return Blocker{}, true
	}
	errCode := component.Result.ErrCode
	if errCode == "" {
		errCode = stateFallbackCode(component.Result.State)
	}
	return Blocker{
		Component:   name,
		State:       component.Result.State,
		ErrCode:     errCode,
		Remediation: RemediationFor(name, errCode),
	}, false
}

// stateFallbackCode mirrors the runner's normalization for a component that
// reached the evaluator without an error code (a hand-built snapshot in a
// test): a non-ready state must always publish a stable code.
func stateFallbackCode(state State) string {
	switch state {
	case StateDegraded:
		return CodeDegraded
	case StateNotConfigured:
		return CodeNotConfigured
	default:
		return CodeFailed
	}
}

// noExecutableBackendBlocker marks the aggregate verdict "no backend can
// execute". It has no backend suffix, so a consumer can tell it apart from a
// specific exec_backend:<name> blocker.
func noExecutableBackendBlocker() Blocker {
	return Blocker{
		Component:   ComponentExecBackend,
		State:       StateNotConfigured,
		ErrCode:     CodeNotConfigured,
		Remediation: RemediationBackendNotInstalled,
	}
}

// commonBlockers returns the blockers every backend shares, keeping the first
// backend's ordering. Per-backend blockers (exec_backend:<name>) differ per
// backend and are excluded on purpose: they would hide the common cause.
func commonBlockers(backendNames []string, backends map[string]Capability) []Blocker {
	if len(backendNames) == 0 {
		return nil
	}
	first := backends[backendNames[0]].Blocking
	out := make([]Blocker, 0, len(first))
	for _, blocker := range first {
		shared := true
		for _, name := range backendNames[1:] {
			found := false
			for _, other := range backends[name].Blocking {
				if other.Component == blocker.Component {
					found = true
					break
				}
			}
			if !found {
				shared = false
				break
			}
		}
		if shared {
			out = append(out, blocker)
		}
	}
	return out
}

// capability builds a Capability with a deduplicated, stably ordered Blocking
// list; ready capabilities publish an empty (never nil) list.
func capability(ready bool, blockers []Blocker) Capability {
	if ready {
		return Capability{State: CapabilityReady, Blocking: []Blocker{}}
	}
	return Capability{State: CapabilityUnavailable, Blocking: sortedBlockers(blockers)}
}

// sortedBlockers deduplicates blockers and orders them by component, then by
// error code, so the same snapshot always produces the same payload.
func sortedBlockers(blockers []Blocker) []Blocker {
	seen := make(map[Blocker]struct{}, len(blockers))
	out := make([]Blocker, 0, len(blockers))
	for _, blocker := range blockers {
		if _, duplicate := seen[blocker]; duplicate {
			continue
		}
		seen[blocker] = struct{}{}
		out = append(out, blocker)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Component != out[j].Component {
			return out[i].Component < out[j].Component
		}
		return out[i].ErrCode < out[j].ErrCode
	})
	return out
}

// nonNilBlockers keeps the JSON contract "blocking is always an array".
func nonNilBlockers(blockers []Blocker) []Blocker {
	if blockers == nil {
		return []Blocker{}
	}
	return blockers
}
