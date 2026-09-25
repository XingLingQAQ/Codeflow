package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codeflow/backend/internal/agent"
	"github.com/codeflow/backend/internal/audit"
	ctxsvc "github.com/codeflow/backend/internal/context"
	"github.com/codeflow/backend/internal/memory"
	"github.com/codeflow/backend/internal/planner"
	"github.com/codeflow/backend/internal/project"
	"github.com/codeflow/backend/internal/readiness"
	"github.com/codeflow/backend/internal/samg"
)

// legacyReadinessComponentNames is the component set the pre-probe /ready
// answered with. Probe work must extend it, never shrink or rename it.
var legacyReadinessComponentNames = []string{
	"planner", "project", "context", "audit", "agent", "memory", "samg",
	"hooks", "privacy", "isolation", "floweng", "workspace", "guard", "skill",
}

// requiredLegacyReadinessNames are the components that gate the overall verdict.
var requiredLegacyReadinessNames = []string{
	"planner", "project", "context", "audit", "agent", "memory", "samg",
}

// readinessProbeTestRouter serves GET /ready on a bare engine, mirroring the
// route the real router installs.
func readinessProbeTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/ready", ReadinessCheck)
	return router
}

type readinessTestResponse struct {
	Success bool           `json:"success"`
	Data    map[string]any `json:"data"`
}

func callReadinessProbeEndpoint(t *testing.T, router *gin.Engine) (int, readinessTestResponse) {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/ready", nil)
	router.ServeHTTP(recorder, request)
	var parsed readinessTestResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &parsed), "body: %s", recorder.Body.String())
	require.NotNil(t, parsed.Data, "body: %s", recorder.Body.String())
	return recorder.Code, parsed
}

func readinessProbeComponents(t *testing.T, data map[string]any) map[string]map[string]any {
	t.Helper()
	raw, ok := data["components"].(map[string]any)
	require.True(t, ok, "components missing or not an object: %#v", data["components"])
	out := make(map[string]map[string]any, len(raw))
	for name, value := range raw {
		component, ok := value.(map[string]any)
		require.True(t, ok, "component %q is not an object: %#v", name, value)
		out[name] = component
	}
	return out
}

func sortedJSONKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// clearReadinessProbesForTest isolates the global probe registry per test. The
// probe framework itself lives in internal/readiness now; this file only pins
// how GET /ready consumes it.
func clearReadinessProbesForTest(t *testing.T) {
	t.Helper()
	readiness.Clear()
	t.Cleanup(readiness.Clear)
}

// stubRequiredReadinessServices installs in-memory implementations of the seven
// required legacy services so the overall verdict is driven only by probes, and
// restores whatever was there before.
func stubRequiredReadinessServices(t *testing.T) {
	t.Helper()
	previousPlanner := planner.GetPlanner()
	previousProject := project.GetProjectService()
	previousContext := ctxsvc.GetContextService()
	previousAudit := audit.GetAuditService()
	previousAgent := agent.GetAgentService()
	previousMemory := memory.GetMemoryService()
	previousSAMG := samg.GetSAMGService()

	planner.SetPlanner(planner.NewInMemoryPlanner())
	project.SetProjectService(project.NewInMemoryProjectService())
	ctxsvc.SetContextService(ctxsvc.NewInMemoryContextService())
	audit.SetAuditService(audit.NewAuditService(audit.NewMemoryStorage()))
	agent.SetAgentService(agent.NewInMemoryAgentService())
	memory.SetMemoryService(memory.NewInMemoryService())
	samg.SetSAMGService(samg.NewSAMGService(nil))

	t.Cleanup(func() {
		planner.SetPlanner(previousPlanner)
		project.SetProjectService(previousProject)
		ctxsvc.SetContextService(previousContext)
		audit.SetAuditService(previousAudit)
		agent.SetAgentService(previousAgent)
		memory.SetMemoryService(previousMemory)
		samg.SetSAMGService(previousSAMG)
	})
}

// nilRequiredReadinessServices removes the seven required legacy services so the
// legacy verdict is a deterministic not_ready, and restores them afterwards.
func nilRequiredReadinessServices(t *testing.T) {
	t.Helper()
	previousPlanner := planner.GetPlanner()
	previousProject := project.GetProjectService()
	previousContext := ctxsvc.GetContextService()
	previousAudit := audit.GetAuditService()
	previousAgent := agent.GetAgentService()
	previousMemory := memory.GetMemoryService()
	previousSAMG := samg.GetSAMGService()

	planner.SetPlanner(nil)
	project.SetProjectService(nil)
	ctxsvc.SetContextService(nil)
	audit.SetAuditService(nil)
	agent.SetAgentService(nil)
	memory.SetMemoryService(nil)
	samg.SetSAMGService(nil)

	t.Cleanup(func() {
		planner.SetPlanner(previousPlanner)
		project.SetProjectService(previousProject)
		ctxsvc.SetContextService(previousContext)
		audit.SetAuditService(previousAudit)
		agent.SetAgentService(previousAgent)
		memory.SetMemoryService(previousMemory)
		samg.SetSAMGService(previousSAMG)
	})
}

// legacyReadinessComponentShape is the pre-probe component payload. It is
// marshalled here so the response can be compared byte for byte.
type legacyReadinessComponentShape struct {
	Ready    bool `json:"ready"`
	Required bool `json:"required"`
}

// TestReadinessNoProbesKeepsLegacyResponse pins the production shape: with no
// probe registered the /ready body is byte for byte the pre-probe response, no
// matter which services happen to be globally registered.
func TestReadinessNoProbesKeepsLegacyResponse(t *testing.T) {
	clearReadinessProbesForTest(t)
	nilRequiredReadinessServices(t)
	assert.Empty(t, readiness.Registered())

	router := readinessProbeTestRouter()
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/ready", nil))

	// The seven required services are absent, so the verdict is a stable 503.
	assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)

	_, parsed := callReadinessProbeEndpoint(t, router)
	assert.Equal(t, "not_ready", parsed.Data["status"])
	components := readinessProbeComponents(t, parsed.Data)
	require.Len(t, components, len(legacyReadinessComponentNames))

	legacy := make(map[string]legacyReadinessComponentShape, len(components))
	for _, name := range legacyReadinessComponentNames {
		component, ok := components[name]
		require.True(t, ok, "legacy component %q missing", name)
		assert.Equal(t, []string{"ready", "required"}, sortedJSONKeys(component),
			"component %q must keep the legacy field set", name)
		ready, _ := component["ready"].(bool)
		required, _ := component["required"].(bool)
		legacy[name] = legacyReadinessComponentShape{Ready: ready, Required: required}
	}
	for _, name := range requiredLegacyReadinessNames {
		assert.False(t, legacy[name].Ready, "required service %q must be reported absent", name)
		assert.True(t, legacy[name].Required)
	}

	// capabilities (T0.12.b part 2) is the one field added on top of the legacy
	// data payload; here it carries no probe-derived state, because no probe is
	// registered. Everything else must still re-encode byte for byte: same field
	// names, same field order, same omission rules.
	rawCapabilities, ok := parsed.Data["capabilities"]
	assert.True(t, ok, "capabilities missing from /ready data: %#v", parsed.Data)
	delete(parsed.Data, "capabilities")
	expected, err := json.Marshal(struct {
		Success bool           `json:"success"`
		Data    map[string]any `json:"data"`
	}{
		Success: parsed.Success,
		Data: map[string]any{
			"components": legacy,
			"status":     parsed.Data["status"],
			"version":    parsed.Data["version"],
		},
	})
	require.NoError(t, err)
	observed, err := json.Marshal(struct {
		Success bool           `json:"success"`
		Data    map[string]any `json:"data"`
	}{Success: parsed.Success, Data: parsed.Data})
	require.NoError(t, err)
	assert.Equal(t, string(expected), string(observed))

	// The capability object itself must be exactly the documented shape.
	capabilities, ok := rawCapabilities.(map[string]any)
	require.True(t, ok, "capabilities must be an object: %#v", rawCapabilities)
	assert.Equal(t, []string{"execution", "merge", "read_only"}, sortedJSONKeys(capabilities))
	execution, ok := capabilities["execution"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []string{"backends", "blocking", "state"}, sortedJSONKeys(execution))

	// No probe-only field may appear inside any component object. The exact key
	// set assertion above already guarantees this, so re-encode the observed
	// components and compare field by field as a second, independent check.
	for name, component := range components {
		for _, probeField := range []string{"checked_at", "error_code", "latency_ms", "readonly", "detail"} {
			assert.NotContains(t, component, probeField, "component %q leaked %q", name, probeField)
		}
		assert.NotContains(t, component, "status", "component %q leaked probe status", name)
	}
}

// TestReadinessReadyProbeReportsProbeFields covers a healthy probe: the component
// carries the probe fields while ready/required keep their meaning.
func TestReadinessReadyProbeReportsProbeFields(t *testing.T) {
	stubRequiredReadinessServices(t)
	clearReadinessProbesForTest(t)

	var calls atomic.Int64
	require.NoError(t, readiness.Register(readiness.Spec{
		Probe: readiness.NewProbeFunc("event_store", func(context.Context) readiness.Result {
			calls.Add(1)
			return readiness.Result{State: readiness.StateReady, Detail: "sqlite ok"}
		}),
		Required: true,
		Timeout:  200 * time.Millisecond,
	}))

	code, parsed := callReadinessProbeEndpoint(t, readinessProbeTestRouter())
	require.Equal(t, http.StatusOK, code, "data: %#v", parsed.Data)
	assert.True(t, parsed.Success)
	assert.Equal(t, "ready", parsed.Data["status"])

	component := readinessProbeComponents(t, parsed.Data)["event_store"]
	require.NotNil(t, component)
	assert.Equal(t, true, component["ready"])
	assert.Equal(t, true, component["required"])
	assert.Equal(t, "ready", component["status"])
	assert.Equal(t, true, component["readonly"])
	assert.NotContains(t, component, "error_code", "a ready probe must not publish an error code")
	assert.Equal(t, "sqlite ok", component["detail"])

	latency, ok := component["latency_ms"].(float64)
	require.True(t, ok, "latency_ms missing: %#v", component)
	assert.GreaterOrEqual(t, latency, float64(0))

	checkedAt, ok := component["checked_at"].(string)
	require.True(t, ok, "checked_at missing: %#v", component)
	parsedCheckedAt, err := time.Parse(time.RFC3339, checkedAt)
	require.NoError(t, err, "checked_at must be RFC3339, got %q", checkedAt)
	assert.WithinDuration(t, time.Now().UTC(), parsedCheckedAt, time.Minute)

	assert.Equal(t, int64(1), calls.Load())
}

// TestReadinessRequiredProbeFailureReturns503 covers a required probe that
// reports a failure with its own code.
func TestReadinessRequiredProbeFailureReturns503(t *testing.T) {
	stubRequiredReadinessServices(t)
	clearReadinessProbesForTest(t)

	require.NoError(t, readiness.Register(readiness.Spec{
		Probe: readiness.NewProbeFunc("vault", func(context.Context) readiness.Result {
			return readiness.Result{State: readiness.StateFailed, ErrCode: "vault_locked", Detail: "sealed"}
		}),
		Required: true,
	}))

	code, parsed := callReadinessProbeEndpoint(t, readinessProbeTestRouter())
	require.Equal(t, http.StatusServiceUnavailable, code, "data: %#v", parsed.Data)
	assert.False(t, parsed.Success)
	assert.Equal(t, "not_ready", parsed.Data["status"])

	component := readinessProbeComponents(t, parsed.Data)["vault"]
	require.NotNil(t, component)
	assert.Equal(t, false, component["ready"])
	assert.Equal(t, true, component["required"])
	assert.Equal(t, "failed", component["status"])
	assert.Equal(t, "vault_locked", component["error_code"])
	assert.Equal(t, "sealed", component["detail"])
	assert.Contains(t, component, "checked_at")
}

// TestReadinessOptionalProbeFailureKeepsReady covers a non-required failure: the
// component is not ready and carries its code, but /ready stays 200.
func TestReadinessOptionalProbeFailureKeepsReady(t *testing.T) {
	stubRequiredReadinessServices(t)
	clearReadinessProbesForTest(t)

	require.NoError(t, readiness.Register(readiness.Spec{
		Probe: readiness.NewProbeFunc("event_store", func(context.Context) readiness.Result {
			return readiness.Result{State: readiness.StateFailed, ErrCode: "event_store_unavailable", Detail: "no wal"}
		}),
		Required: false,
	}))

	code, parsed := callReadinessProbeEndpoint(t, readinessProbeTestRouter())
	require.Equal(t, http.StatusOK, code, "data: %#v", parsed.Data)
	assert.True(t, parsed.Success)
	assert.Equal(t, "ready", parsed.Data["status"])

	component := readinessProbeComponents(t, parsed.Data)["event_store"]
	require.NotNil(t, component)
	assert.Equal(t, false, component["ready"])
	assert.Equal(t, false, component["required"])
	assert.Equal(t, "failed", component["status"])
	assert.Equal(t, "event_store_unavailable", component["error_code"])
}

// TestReadinessProbeTimeoutDoesNotHangReady covers a probe that ignores its
// context: /ready must answer within the probe budget and report probe_timeout,
// and a later poll must report the stuck run without waiting on it again.
func TestReadinessProbeTimeoutDoesNotHangReady(t *testing.T) {
	stubRequiredReadinessServices(t)
	clearReadinessProbesForTest(t)

	require.NoError(t, readiness.Register(readiness.Spec{
		Probe: readiness.NewProbeFunc("exec_backend", func(context.Context) readiness.Result {
			// Deliberately ignores ctx: the deadline is the runner's contract.
			time.Sleep(5 * time.Second)
			return readiness.Result{State: readiness.StateReady}
		}),
		Required: true,
		Timeout:  50 * time.Millisecond,
	}))

	router := readinessProbeTestRouter()
	start := time.Now()
	code, parsed := callReadinessProbeEndpoint(t, router)
	elapsed := time.Since(start)
	t.Logf("first /ready with a hung probe returned in %s (probe timeout 50ms plus the runner's grace)",
		elapsed)

	require.Less(t, elapsed, time.Second, "a hung probe must not block /ready")
	assert.Equal(t, http.StatusServiceUnavailable, code)
	assert.Equal(t, "not_ready", parsed.Data["status"])

	component := readinessProbeComponents(t, parsed.Data)["exec_backend"]
	require.NotNil(t, component)
	assert.Equal(t, false, component["ready"])
	assert.Equal(t, "failed", component["status"])
	assert.Equal(t, readiness.CodeTimeout, component["error_code"])
	detail, _ := component["detail"].(string)
	require.Contains(t, detail, "still running since")
	stamp := strings.TrimSpace(strings.TrimPrefix(detail, "still running since "))
	_, err := time.Parse(time.RFC3339, stamp)
	assert.NoError(t, err, "detail must carry an RFC3339 stamp, got %q", detail)

	// The run has by now blown its own deadline (the first poll already waited
	// out the whole budget), so the next poll reports the stuck slot at once.
	time.Sleep(100 * time.Millisecond)
	second := time.Now()
	secondCode, secondParsed := callReadinessProbeEndpoint(t, router)
	secondElapsed := time.Since(second)
	t.Logf("second /ready with the run past its deadline returned in %s", secondElapsed)
	assert.Less(t, secondElapsed, 200*time.Millisecond, "a run past its deadline must be reported, not waited on")
	assert.Equal(t, http.StatusServiceUnavailable, secondCode)
	secondComponent := readinessProbeComponents(t, secondParsed.Data)["exec_backend"]
	assert.Equal(t, readiness.CodeTimeout, secondComponent["error_code"])
}

// TestReadinessProbePanicIsReported covers a panicking probe: the runner
// recovers, reports probe_panic, and an optional probe does not gate /ready.
func TestReadinessProbePanicIsReported(t *testing.T) {
	stubRequiredReadinessServices(t)
	clearReadinessProbesForTest(t)

	require.NoError(t, readiness.Register(readiness.Spec{
		Probe: readiness.NewProbeFunc("migrations", func(context.Context) readiness.Result {
			panic("probe exploded")
		}),
		Required: false,
	}))

	code, parsed := callReadinessProbeEndpoint(t, readinessProbeTestRouter())
	require.Equal(t, http.StatusOK, code, "a panicking optional probe must not fail /ready")
	assert.Equal(t, "ready", parsed.Data["status"])

	component := readinessProbeComponents(t, parsed.Data)["migrations"]
	require.NotNil(t, component)
	assert.Equal(t, false, component["ready"])
	assert.Equal(t, "failed", component["status"])
	assert.Equal(t, readiness.CodePanic, component["error_code"])
	detail, _ := component["detail"].(string)
	assert.Contains(t, detail, "probe exploded")
	assert.Contains(t, component, "checked_at")
}

// TestReadinessProbeOverridesLegacyComponent covers a probe whose name collides
// with a legacy Has* component: the probe payload wins and keeps the
// ready/required semantics.
func TestReadinessProbeOverridesLegacyComponent(t *testing.T) {
	stubRequiredReadinessServices(t)
	clearReadinessProbesForTest(t)

	require.NoError(t, readiness.Register(readiness.Spec{
		Probe: readiness.NewProbeFunc("memory", func(context.Context) readiness.Result {
			return readiness.Result{State: readiness.StateFailed, ErrCode: "memory_store_down"}
		}),
		Required: true,
	}))

	code, parsed := callReadinessProbeEndpoint(t, readinessProbeTestRouter())
	require.Equal(t, http.StatusServiceUnavailable, code,
		"a required probe override must still gate the verdict")
	assert.Equal(t, "not_ready", parsed.Data["status"])

	component := readinessProbeComponents(t, parsed.Data)["memory"]
	require.NotNil(t, component)
	assert.Equal(t, []string{"checked_at", "error_code", "latency_ms", "readonly", "ready", "required", "status"},
		sortedJSONKeys(component), "the probe payload must replace the legacy one")
	assert.Equal(t, false, component["ready"])
	assert.Equal(t, true, component["required"])
	assert.Equal(t, "failed", component["status"])
	assert.Equal(t, "memory_store_down", component["error_code"])

	// A probe that takes over a legacy name as optional removes its gating.
	require.NoError(t, readiness.Register(readiness.Spec{
		Probe: readiness.NewProbeFunc("memory", func(context.Context) readiness.Result {
			return readiness.Result{State: readiness.StateFailed, ErrCode: "memory_store_down"}
		}),
		Required: false,
	}))
	code, parsed = callReadinessProbeEndpoint(t, readinessProbeTestRouter())
	assert.Equal(t, http.StatusOK, code, "an optional override must not gate the verdict")
	assert.Equal(t, "ready", parsed.Data["status"])
}

// TestReadinessProbeConcurrentReadyAndRegistration hammers /ready while probes
// are registered and cleared from other goroutines: no panic, no torn response.
func TestReadinessProbeConcurrentReadyAndRegistration(t *testing.T) {
	stubRequiredReadinessServices(t)
	clearReadinessProbesForTest(t)

	router := readinessProbeTestRouter()
	healthy := func(context.Context) readiness.Result { return readiness.Result{State: readiness.StateReady} }
	require.NoError(t, readiness.Register(
		readiness.Spec{Probe: readiness.NewProbeFunc("alpha", healthy), Required: true},
		readiness.Spec{Probe: readiness.NewProbeFunc("beta", healthy)},
	))

	var waitGroup sync.WaitGroup
	stop := make(chan struct{})
	statuses := make([]int32, 8)

	for worker := 0; worker < 8; worker++ {
		waitGroup.Add(1)
		go func(worker int) {
			defer waitGroup.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/ready", nil))
				status := recorder.Code
				if status != http.StatusOK && status != http.StatusServiceUnavailable {
					atomic.StoreInt32(&statuses[worker], int32(status))
					return
				}
				var parsed readinessTestResponse
				if err := json.Unmarshal(recorder.Body.Bytes(), &parsed); err != nil {
					atomic.StoreInt32(&statuses[worker], -1)
					return
				}
			}
		}(worker)
	}

	for round := 0; round < 30; round++ {
		require.NoError(t, readiness.Register(
			readiness.Spec{Probe: readiness.NewProbeFunc("alpha", healthy), Required: true},
			readiness.Spec{Probe: readiness.NewProbeFunc("beta", healthy)},
		))
		readiness.Clear()
	}
	close(stop)
	waitGroup.Wait()

	for worker, status := range statuses {
		assert.Equal(t, int32(0), status, "worker %d saw an unexpected response", worker)
	}
}

// --- T0.12.b part 2: capability sets on GET /ready -------------------------

// capabilityPayload is the decoded capabilities object of a /ready response.
type capabilityPayload struct {
	ReadOnly  capabilityEntry            `json:"read_only"`
	Execution capabilityEntryWithBackend `json:"execution"`
	Merge     capabilityEntry            `json:"merge"`
}

type capabilityEntry struct {
	State    string              `json:"state"`
	Blocking []capabilityBlocker `json:"blocking"`
}

type capabilityEntryWithBackend struct {
	capabilityEntry
	Backends map[string]capabilityEntry `json:"backends"`
}

type capabilityBlocker struct {
	Component   string `json:"component"`
	State       string `json:"state"`
	ErrCode     string `json:"error_code"`
	Remediation string `json:"remediation"`
}

// readinessCapabilities decodes the capabilities object of a /ready response.
func readinessCapabilities(t *testing.T, data map[string]any) capabilityPayload {
	t.Helper()
	raw, ok := data["capabilities"]
	require.True(t, ok, "capabilities missing from /ready data: %#v", data)
	encoded, err := json.Marshal(raw)
	require.NoError(t, err)
	var parsed capabilityPayload
	require.NoError(t, json.Unmarshal(encoded, &parsed), "capabilities: %s", encoded)
	return parsed
}

func blockerNames(blocking []capabilityBlocker) []string {
	names := make([]string, 0, len(blocking))
	for _, blocker := range blocking {
		names = append(names, blocker.Component)
	}
	return names
}

// registerProductionShapedProbes registers the probe set Apply installs in
// production: frontend_protocol, policy and workspace ready, every
// not-yet-wired dependency not_configured, all non-required.
func registerProductionShapedProbes(t *testing.T) {
	t.Helper()
	healthy := func(context.Context) readiness.Result { return readiness.Result{State: readiness.StateReady} }
	specs := []readiness.Spec{
		{Probe: readiness.NewProbeFunc(readiness.ComponentFrontendProtocol, func(context.Context) readiness.Result {
			return readiness.Result{State: readiness.StateReady, Detail: "protocol_version=" + readiness.FrontendProtocolVersion}
		})},
		{Probe: readiness.NewProbeFunc(readiness.ComponentPolicy, healthy)},
		{Probe: readiness.NewProbeFunc(readiness.ComponentWorkspace, healthy)},
		{Probe: readiness.NewNotConfiguredProbe(readiness.ComponentDatabase, "wired by T1.01")},
		{Probe: readiness.NewNotConfiguredProbe(readiness.ComponentMigrations, "wired by T1.01")},
		{Probe: readiness.NewNotConfiguredProbe(readiness.ComponentEventStore, "wired by T1.05")},
		{Probe: readiness.NewNotConfiguredProbe(readiness.ComponentOutboxDispatcher, "wired by T1.05")},
		{Probe: readiness.NewNotConfiguredProbe(readiness.ComponentVault, "wired by T2.04")},
		{Probe: readiness.NewNotConfiguredProbe(readiness.ExecBackendPrefix+"claude_code", "wired by T1.13")},
		{Probe: readiness.NewNotConfiguredProbe(readiness.ExecBackendPrefix+"codex", "wired by T4.01")},
		{Probe: readiness.NewNotConfiguredProbe(readiness.ExecBackendPrefix+"gemini", "wired by T4.02")},
	}
	require.NoError(t, readiness.Register(specs...))
}

// TestReadinessProductionProbesKeepReadyWithCapabilities covers the production
// picture: the legacy services are wired, every unwired runtime dependency is
// not_configured, /ready still answers 200/ready (the HTTP verdict is about the
// read-only surface), and the capability sets explain what is actually blocked.
func TestReadinessProductionProbesKeepReadyWithCapabilities(t *testing.T) {
	stubRequiredReadinessServices(t)
	clearReadinessProbesForTest(t)
	registerProductionShapedProbes(t)

	router := readinessProbeTestRouter()
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/ready", nil))
	code := recorder.Code
	var parsed readinessTestResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &parsed), "body: %s", recorder.Body.String())
	require.NotNil(t, parsed.Data)
	// Evidence for the receipt: the exact production-shaped body.
	t.Logf("production-shaped /ready (status %d): %s", code, recorder.Body.String())

	require.Equal(t, http.StatusOK, code, "unwired dependencies must not 503 /ready: %#v", parsed.Data)
	assert.True(t, parsed.Success)
	assert.Equal(t, "ready", parsed.Data["status"])

	components := readinessProbeComponents(t, parsed.Data)
	for _, name := range append(append([]string{}, legacyReadinessComponentNames...),
		readiness.ComponentFrontendProtocol, readiness.ComponentVault, readiness.ComponentEventStore,
		readiness.ComponentOutboxDispatcher, readiness.ComponentDatabase, readiness.ComponentMigrations,
		readiness.ExecBackendPrefix+"claude_code", readiness.ExecBackendPrefix+"codex", readiness.ExecBackendPrefix+"gemini") {
		component, ok := components[name]
		require.True(t, ok, "probe component %q missing", name)
		if _, isProbe := component["status"]; isProbe {
			assert.Equal(t, false, component["required"], "production probe %q must be optional", name)
		}
	}

	capabilities := readinessCapabilities(t, parsed.Data)
	assert.Equal(t, string(readiness.CapabilityReady), capabilities.ReadOnly.State,
		"the read-only surface is usable: legacy services plus frontend_protocol are ready")
	assert.Empty(t, capabilities.ReadOnly.Blocking)

	assert.Equal(t, string(readiness.CapabilityUnavailable), capabilities.Execution.State)
	assert.Equal(t, string(readiness.CapabilityUnavailable), capabilities.Merge.State)
	executionBlockers := blockerNames(capabilities.Execution.Blocking)
	for _, name := range []string{
		readiness.ComponentVault, readiness.ComponentEventStore, readiness.ComponentOutboxDispatcher,
		readiness.ComponentMigrations,
	} {
		assert.Contains(t, executionBlockers, name, "execution must name %s as blocking", name)
	}
	assert.NotContains(t, executionBlockers, readiness.ComponentDatabase,
		"database is not an execution gate before T1.01/T1.04 wire the run store")
	// The remediation codes are the plan's contract; the shell switches on them.
	byName := make(map[string]capabilityBlocker, len(capabilities.Execution.Blocking))
	for _, blocker := range capabilities.Execution.Blocking {
		byName[blocker.Component] = blocker
	}
	assert.Equal(t, readiness.RemediationVaultLocked, byName[readiness.ComponentVault].Remediation)
	assert.Equal(t, readiness.RemediationEventStoreUnavailable, byName[readiness.ComponentEventStore].Remediation)
	assert.Equal(t, readiness.RemediationOutboxUnavailable, byName[readiness.ComponentOutboxDispatcher].Remediation)
	assert.Equal(t, readiness.RemediationMigrationsPending, byName[readiness.ComponentMigrations].Remediation)
	assert.Equal(t, "not_configured", byName[readiness.ComponentVault].State)

	// Every registered backend is separately unusable and says so.
	require.Len(t, capabilities.Execution.Backends, 3)
	for _, backend := range []string{"claude_code", "codex", "gemini"} {
		entry := capabilities.Execution.Backends[backend]
		assert.Equal(t, string(readiness.CapabilityUnavailable), entry.State, "backend %s", backend)
		assert.Contains(t, blockerNames(entry.Blocking), readiness.ExecBackendPrefix+backend)
		found := false
		for _, blocker := range entry.Blocking {
			if blocker.Component == readiness.ExecBackendPrefix+backend {
				assert.Equal(t, readiness.RemediationBackendNotInstalled, blocker.Remediation)
				found = true
			}
		}
		assert.True(t, found, "backend %s must explain its own unavailability", backend)
	}
}

// TestReadinessExecutionReadyWhenDependenciesRecover covers the acceptance
// assertion "repair the dependency and the next check reflects it": nothing in
// the response is cached between polls, so flipping a probe from not_configured
// to ready makes execution executable with no restart.
func TestReadinessExecutionReadyWhenDependenciesRecover(t *testing.T) {
	stubRequiredReadinessServices(t)
	clearReadinessProbesForTest(t)

	healthy := func(context.Context) readiness.Result { return readiness.Result{State: readiness.StateReady} }
	specs := []readiness.Spec{
		{Probe: readiness.NewProbeFunc(readiness.ComponentFrontendProtocol, healthy)},
		{Probe: readiness.NewProbeFunc(readiness.ComponentPolicy, healthy)},
		{Probe: readiness.NewProbeFunc(readiness.ComponentWorkspace, healthy)},
		{Probe: readiness.NewProbeFunc(readiness.ComponentMigrations, healthy)},
		{Probe: readiness.NewProbeFunc(readiness.ComponentEventStore, healthy)},
		{Probe: readiness.NewProbeFunc(readiness.ComponentOutboxDispatcher, healthy)},
		{Probe: readiness.NewProbeFunc(readiness.ComponentVault, healthy)},
		{Probe: readiness.NewNotConfiguredProbe(readiness.ExecBackendPrefix+"codex", "wired by T4.01")},
	}
	require.NoError(t, readiness.Register(specs...))

	_, parsed := callReadinessProbeEndpoint(t, readinessProbeTestRouter())
	before := readinessCapabilities(t, parsed.Data)
	require.Equal(t, string(readiness.CapabilityUnavailable), before.Execution.State)
	// The global verdict names both the class marker and the single backend that
	// is registered: the class marker says no backend can execute, the suffixed
	// entry says which one is missing.
	require.Equal(t, []string{readiness.ComponentExecBackend, readiness.ExecBackendPrefix + "codex"},
		blockerNames(before.Execution.Blocking))

	// The backend gets installed while the process keeps running.
	specs[len(specs)-1] = readiness.Spec{Probe: readiness.NewProbeFunc(readiness.ExecBackendPrefix+"codex", healthy)}
	require.NoError(t, readiness.Register(specs...))

	_, parsed = callReadinessProbeEndpoint(t, readinessProbeTestRouter())
	after := readinessCapabilities(t, parsed.Data)
	assert.Equal(t, string(readiness.CapabilityReady), after.Execution.State,
		"the second poll must see the repaired backend, not a cached verdict")
	assert.Empty(t, after.Execution.Blocking)
	assert.Equal(t, string(readiness.CapabilityReady), after.Merge.State)
	assert.Equal(t, string(readiness.CapabilityReady), after.Execution.Backends["codex"].State)
}

// TestReadinessCapabilitiesBlockLegacyGap covers a missing required legacy
// service: /ready is 503 and read_only is unavailable with the missing service
// named.
func TestReadinessCapabilitiesBlockLegacyGap(t *testing.T) {
	nilRequiredReadinessServices(t)
	clearReadinessProbesForTest(t)

	code, parsed := callReadinessProbeEndpoint(t, readinessProbeTestRouter())
	require.Equal(t, http.StatusServiceUnavailable, code)
	require.Equal(t, "not_ready", parsed.Data["status"])

	capabilities := readinessCapabilities(t, parsed.Data)
	assert.Equal(t, string(readiness.CapabilityUnavailable), capabilities.ReadOnly.State)
	names := blockerNames(capabilities.ReadOnly.Blocking)
	for _, name := range requiredLegacyReadinessNames {
		assert.Contains(t, names, name, "read_only must name the missing service %s", name)
	}
	assert.Equal(t, string(readiness.CapabilityUnavailable), capabilities.Execution.State)
	assert.Equal(t, string(readiness.CapabilityUnavailable), capabilities.Merge.State)

	// Every legacy blocker carries the documented fallback remediation; the
	// frontend_protocol entry keeps its own code.
	for _, blocker := range capabilities.ReadOnly.Blocking {
		expected := readiness.RemediationDependencyNotReady
		if blocker.Component == readiness.ComponentFrontendProtocol {
			expected = readiness.RemediationProtocolMismatch
		}
		assert.Equal(t, expected, blocker.Remediation, "%+v", blocker)
	}
}

// TestReadinessCapabilitiesAgreeWithComponents checks that the capability
// payload is derived from the same snapshot the components describe: a
// dependency the response reports as ready is never listed as blocking, and the
// blocking lists are sorted and duplicate-free.
func TestReadinessCapabilitiesAgreeWithComponents(t *testing.T) {
	stubRequiredReadinessServices(t)
	clearReadinessProbesForTest(t)
	registerProductionShapedProbes(t)

	_, parsed := callReadinessProbeEndpoint(t, readinessProbeTestRouter())
	components := readinessProbeComponents(t, parsed.Data)
	capabilities := readinessCapabilities(t, parsed.Data)

	seen := map[string]bool{}
	for _, grouping := range []struct {
		label    string
		blocking []capabilityBlocker
	}{
		{"read_only", capabilities.ReadOnly.Blocking},
		{"execution", capabilities.Execution.Blocking},
		{"merge", capabilities.Merge.Blocking},
	} {
		for _, blocker := range grouping.blocking {
			seen[blocker.Component] = true
			component, ok := components[blocker.Component]
			if !ok {
				continue // legacy service names are not probe components
			}
			assert.NotEqual(t, true, component["ready"],
				"%s lists %s as blocking but the component says ready", grouping.label, blocker.Component)
		}
	}
	assert.True(t, seen[readiness.ComponentVault])

	for label, blocking := range map[string][]capabilityBlocker{
		"read_only": capabilities.ReadOnly.Blocking,
		"execution": capabilities.Execution.Blocking,
		"merge":     capabilities.Merge.Blocking,
	} {
		names := blockerNames(blocking)
		sorted := append([]string{}, names...)
		sort.Strings(sorted)
		assert.Equal(t, sorted, names, "%s blocking must be sorted", label)
		assert.Equal(t, len(sorted), len(uniqueStrings(names)), "%s blocking has duplicates", label)
	}
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

// TestReadinessNoProbesKeepsComponentShape re-checks the legacy promise from the
// capabilities side: adding the new field must not change any component object.
func TestReadinessNoProbesKeepsComponentShape(t *testing.T) {
	clearReadinessProbesForTest(t)
	stubRequiredReadinessServices(t)

	_, parsed := callReadinessProbeEndpoint(t, readinessProbeTestRouter())
	components := readinessProbeComponents(t, parsed.Data)
	require.Len(t, components, len(legacyReadinessComponentNames))
	for _, name := range legacyReadinessComponentNames {
		assert.Equal(t, []string{"ready", "required"}, sortedJSONKeys(components[name]),
			"component %q must keep the legacy field set", name)
	}
	capabilities := readinessCapabilities(t, parsed.Data)
	assert.Equal(t, string(readiness.CapabilityUnavailable), capabilities.ReadOnly.State,
		"the legacy services are stubbed ready, but no frontend_protocol probe proves the protocol")
	names := blockerNames(capabilities.ReadOnly.Blocking)
	require.Len(t, names, 1, "read_only must name exactly the missing frontend_protocol: %v", names)
	assert.Equal(t, readiness.ComponentFrontendProtocol, names[0])
	assert.Equal(t, string(readiness.CapabilityUnavailable), capabilities.Execution.State,
		"without the frontend_protocol probe nothing proves the execution surface")
	assert.Empty(t, capabilities.Execution.Backends)

	// The rest of the data payload is untouched: status, version, components.
	for _, field := range []string{"status", "version", "components"} {
		assert.Contains(t, parsed.Data, field)
	}
	raw, ok := parsed.Data["capabilities"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []string{"execution", "merge", "read_only"}, sortedJSONKeys(raw))
	execution, ok := raw["execution"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, execution, "backends")
}

// TestReadinessCapabilitiesJSONHasNoGoFieldNames keeps the payload free of Go
// field names, so the generated OpenAPI types and the payload agree.
func TestReadinessCapabilitiesJSONHasNoGoFieldNames(t *testing.T) {
	stubRequiredReadinessServices(t)
	clearReadinessProbesForTest(t)
	registerProductionShapedProbes(t)

	_, parsed := callReadinessProbeEndpoint(t, readinessProbeTestRouter())
	encoded, err := json.Marshal(parsed.Data["capabilities"])
	require.NoError(t, err)
	for _, leaked := range []string{"ReadOnly", "Blocking", "ErrCode", "Remediation", "Backends", "CapabilityState"} {
		assert.False(t, strings.Contains(string(encoded), leaked), "%q leaked into %s", leaked, encoded)
	}
	assert.True(t, strings.Contains(string(encoded), `"error_code"`), "expected snake_case keys in %s", encoded)
}

// TestReadinessCapabilitiesReflectProbeFailures covers a failed (not merely
// unwired) dependency: its state and error code travel into the blocker.
func TestReadinessCapabilitiesReflectProbeFailures(t *testing.T) {
	stubRequiredReadinessServices(t)
	clearReadinessProbesForTest(t)

	require.NoError(t, readiness.Register(
		readiness.Spec{Probe: readiness.NewProbeFunc(readiness.ComponentFrontendProtocol,
			func(context.Context) readiness.Result { return readiness.Result{State: readiness.StateReady} })},
		readiness.Spec{Probe: readiness.NewProbeFunc(readiness.ComponentVault,
			func(context.Context) readiness.Result {
				return readiness.Result{State: readiness.StateFailed, ErrCode: readiness.RemediationVaultLocked, Detail: "sealed"}
			})},
	))

	code, parsed := callReadinessProbeEndpoint(t, readinessProbeTestRouter())
	require.Equal(t, http.StatusOK, code, "an optional probe failure must not 503 /ready")
	capabilities := readinessCapabilities(t, parsed.Data)
	require.Equal(t, string(readiness.CapabilityUnavailable), capabilities.Execution.State)
	found := false
	for _, blocker := range capabilities.Execution.Blocking {
		if blocker.Component != readiness.ComponentVault {
			continue
		}
		found = true
		assert.Equal(t, "failed", blocker.State)
		assert.Equal(t, readiness.RemediationVaultLocked, blocker.ErrCode)
		assert.Equal(t, readiness.RemediationVaultLocked, blocker.Remediation)
	}
	assert.True(t, found, "vault blocker missing: %+v", capabilities.Execution.Blocking)
	assert.Equal(t, string(readiness.CapabilityUnavailable), capabilities.Merge.State)
}
