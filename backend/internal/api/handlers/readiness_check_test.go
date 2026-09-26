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

// --- T0.12.c: GET /ready and the Run creation gate must agree ---------------

// capabilityProvider is the plan's "fake capability provider" (section 28
// T0.12.c): an independent Registry whose probes answer from a table the test
// rewrites between checks. No I/O and no timing, so one combination is
// described once and both consumers of the same snapshot are asked the same
// question — GET /ready and readiness.CheckExecution, the gate the Run
// creation path (T1.04) calls before creating a Run.
type capabilityProvider struct {
	registry *readiness.Registry
	results  map[string]readiness.Result
}

func newCapabilityProvider() *capabilityProvider {
	return &capabilityProvider{registry: readiness.NewRegistry(), results: map[string]readiness.Result{}}
}

// set registers (or replaces) one dependency's outcome; re-registering is how
// the fake provider flips a dependency between two checks.
func (p *capabilityProvider) set(t *testing.T, name string, result readiness.Result) {
	t.Helper()
	p.results[name] = result
	require.NoError(t, p.registry.Register(readiness.Spec{
		Probe: readiness.NewProbeFunc(name, func(context.Context) readiness.Result { return result }),
	}))
}

func (p *capabilityProvider) ready(t *testing.T, names ...string) {
	t.Helper()
	for _, name := range names {
		p.set(t, name, readiness.Result{State: readiness.StateReady})
	}
}

// capabilityDependencies is the dependency enumeration of plan section 15
// T0.12 step 1, so a combination never leaves a dependency out by accident.
var capabilityDependencies = []string{
	readiness.ComponentFrontendProtocol,
	readiness.ComponentPolicy,
	readiness.ComponentWorkspace,
	readiness.ComponentDatabase,
	readiness.ComponentMigrations,
	readiness.ComponentEventStore,
	readiness.ComponentOutboxDispatcher,
	readiness.ComponentVault,
}

// readyCapabilityProvider returns a provider where every dependency and both
// execution backends are ready.
func readyCapabilityProvider(t *testing.T) *capabilityProvider {
	t.Helper()
	provider := newCapabilityProvider()
	provider.ready(t, capabilityDependencies...)
	provider.ready(t, readiness.ExecBackendPrefix+"claude_code", readiness.ExecBackendPrefix+"codex")
	return provider
}

// install serves the provider's probes through the package-level registry GET
// /ready reads, so the endpoint and CheckExecution observe the same probes.
func (p *capabilityProvider) install(t *testing.T) {
	t.Helper()
	clearReadinessProbesForTest(t)
	require.NoError(t, readiness.Register(p.registry.Snapshot()...))
}

// sortedBackendNames returns the backend keys of a decoded capability payload.
func sortedBackendNames(backends map[string]capabilityEntry) []string {
	names := make([]string, 0, len(backends))
	for name := range backends {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// capabilityBlockerNames names the components of a domain blocker list.
func capabilityBlockerNames(blocking []readiness.Blocker) []string {
	names := make([]string, 0, len(blocking))
	for _, blocker := range blocking {
		names = append(names, blocker.Component)
	}
	return names
}

// blockerJSON renders a blocker list as JSON so a published payload and a
// domain verdict can be compared field by field; the evaluator publishes a
// stable order and both sides use the same field names.
func blockerJSON(t *testing.T, blockers interface{}) string {
	t.Helper()
	encoded, err := json.Marshal(blockers)
	require.NoError(t, err)
	return string(encoded)
}

// TestReadinessCapabilityMatchesCreateRun is the plan's named test for
// T0.12.c: for every combination, the execution verdict GET /ready publishes
// (global and per backend) equals what readiness.CheckExecution — the gate the
// Run creation path uses — returns for the same snapshot, field by field.
func TestReadinessCapabilityMatchesCreateRun(t *testing.T) {
	ctx := context.Background()

	combinations := []struct {
		name string
		// arrange mutates the provider into the combination.
		arrange func(t *testing.T, provider *capabilityProvider)
		// legacyReady false means the required Has* services are absent, which
		// is the same verdict both consumers must derive from that snapshot.
		legacyReady bool
	}{
		{
			name:        "all_dependencies_ready",
			arrange:     func(t *testing.T, provider *capabilityProvider) {},
			legacyReady: true,
		},
		{
			name: "vault_not_ready",
			arrange: func(t *testing.T, provider *capabilityProvider) {
				provider.set(t, readiness.ComponentVault,
					readiness.Result{State: readiness.StateNotConfigured, Detail: "wired by T2.04"})
			},
			legacyReady: true,
		},
		{
			name: "only_codex_backend_ready",
			arrange: func(t *testing.T, provider *capabilityProvider) {
				provider.set(t, readiness.ExecBackendPrefix+"claude_code",
					readiness.Result{State: readiness.StateNotConfigured})
			},
			legacyReady: true,
		},
		{
			name:        "legacy_read_only_not_ready",
			arrange:     func(t *testing.T, provider *capabilityProvider) {},
			legacyReady: false,
		},
		{
			name: "no_backend_registered",
			arrange: func(t *testing.T, provider *capabilityProvider) {
				provider.registry.Clear()
				provider.ready(t, capabilityDependencies...)
			},
			legacyReady: true,
		},
		{
			name: "vault_locked_and_protocol_failed",
			arrange: func(t *testing.T, provider *capabilityProvider) {
				provider.set(t, readiness.ComponentVault, readiness.Result{
					State:   readiness.StateFailed,
					ErrCode: readiness.RemediationVaultLocked,
					Detail:  "sealed",
				})
				provider.set(t, readiness.ComponentFrontendProtocol,
					readiness.Result{State: readiness.StateFailed, ErrCode: readiness.CodeFailed})
			},
			legacyReady: true,
		},
	}

	for _, combination := range combinations {
		t.Run(combination.name, func(t *testing.T) {
			provider := readyCapabilityProvider(t)
			combination.arrange(t, provider)
			provider.install(t)

			// The legacy Has* layer is real process state, not a test fake:
			// either the seven required services are stubbed ready or they are
			// absent. The Run creation gate is given the same verdict the
			// endpoint derives from that state.
			var legacy func() (bool, []string)
			if combination.legacyReady {
				stubRequiredReadinessServices(t)
				legacy = func() (bool, []string) { return true, nil }
			} else {
				nilRequiredReadinessServices(t)
				legacy = func() (bool, []string) { return false, requiredLegacyReadinessNames }
			}

			// GET /ready: what the shell reads before it offers a Run.
			code, parsed := callReadinessProbeEndpoint(t, readinessProbeTestRouter())
			require.Contains(t, []int{http.StatusOK, http.StatusServiceUnavailable}, code,
				"unexpected status %d: %#v", code, parsed.Data)
			ready := readinessCapabilities(t, parsed.Data)

			backends := sortedBackendNames(ready.Execution.Backends)
			if len(backends) == 0 {
				// No backend is registered at all: execution cannot be ready,
				// and the Run creation gate refuses every backend name.
				assert.Equal(t, string(readiness.CapabilityUnavailable), ready.Execution.State)
				refused := readiness.CheckExecution(ctx, provider.registry, "codex", legacy)
				assert.Equal(t, string(readiness.CapabilityUnavailable), string(refused.State),
					"an unregistered backend must not be executable")
				return
			}

			for _, backend := range backends {
				fromEndpoint := ready.Execution.Backends[backend]
				fromCreateRun := readiness.CheckExecution(ctx, provider.registry, backend, legacy)
				assert.Equal(t, string(fromCreateRun.State), string(fromEndpoint.State),
					"backend %s: /ready and CheckExecution disagree", backend)
				assert.Equal(t, blockerJSON(t, fromCreateRun.Blocking), blockerJSON(t, fromEndpoint.Blocking),
					"backend %s: blockers differ", backend)
			}

			// The global verdict summarises the per-backend ones: ready exactly
			// when some backend is executable.
			anyExecutable := false
			for _, backend := range backends {
				if ready.Execution.Backends[backend].State == string(readiness.CapabilityReady) {
					anyExecutable = true
					break
				}
			}
			assert.Equal(t, anyExecutable, ready.Execution.State == string(readiness.CapabilityReady),
				"global execution %s does not summarise backends %+v", ready.Execution.State, ready.Execution.Backends)

			// The Run creation gate agrees with the published capability for
			// every backend, and never accepts while read_only is broken.
			for _, backend := range backends {
				fromCreateRun := readiness.CheckExecution(ctx, provider.registry, backend, legacy)
				wantReady := ready.Execution.Backends[backend].State == string(readiness.CapabilityReady)
				assert.Equal(t, wantReady, fromCreateRun.State == readiness.CapabilityReady,
					"backend %s: CheckExecution %s vs /ready %s", backend, fromCreateRun.State,
					ready.Execution.Backends[backend].State)
				if fromCreateRun.State == readiness.CapabilityReady {
					assert.Equal(t, string(readiness.CapabilityReady), string(ready.ReadOnly.State),
						"backend %s is creatable while read_only is %s", backend, ready.ReadOnly.State)
				}
			}

			// An unregistered backend is never creatable, whatever the
			// snapshot says about the registered ones.
			unregistered := readiness.CheckExecution(ctx, provider.registry, "not_a_backend", legacy)
			assert.Equal(t, string(readiness.CapabilityUnavailable), string(unregistered.State),
				"an unregistered backend must not be executable")
		})
	}
}

// TestReadinessCapabilityMatchesCreateRunAfterRepair covers the acceptance
// assertion "repair the dependency and the next check reflects it" for both
// consumers: neither GET /ready nor CheckExecution serves a cached verdict, so
// the flip from unavailable to ready is visible to both with no restart.
func TestReadinessCapabilityMatchesCreateRunAfterRepair(t *testing.T) {
	ctx := context.Background()
	stubRequiredReadinessServices(t)
	legacy := func() (bool, []string) { return true, nil }

	provider := readyCapabilityProvider(t)
	provider.set(t, readiness.ComponentVault, readiness.Result{State: readiness.StateNotConfigured})
	provider.install(t)

	_, parsed := callReadinessProbeEndpoint(t, readinessProbeTestRouter())
	before := readinessCapabilities(t, parsed.Data)
	require.Equal(t, string(readiness.CapabilityUnavailable), before.Execution.State)
	beforeCreate := readiness.CheckExecution(ctx, provider.registry, "codex", legacy)
	require.Equal(t, string(readiness.CapabilityUnavailable), string(beforeCreate.State),
		"before repair: CheckExecution must refuse")

	// The dependency is repaired; both consumers must see it on the next call.
	provider.set(t, readiness.ComponentVault, readiness.Result{State: readiness.StateReady})
	provider.install(t)

	_, parsed = callReadinessProbeEndpoint(t, readinessProbeTestRouter())
	after := readinessCapabilities(t, parsed.Data)
	assert.Equal(t, string(readiness.CapabilityReady), after.Execution.State,
		"the second poll must see the repaired dependency, not a cached verdict")
	afterCreate := readiness.CheckExecution(ctx, provider.registry, "codex", legacy)
	assert.Equal(t, string(readiness.CapabilityReady), string(afterCreate.State),
		"after repair: CheckExecution must accept")
	assert.Equal(t, blockerJSON(t, afterCreate.Blocking),
		blockerJSON(t, after.Execution.Backends["codex"].Blocking),
		"after repair: blockers differ between /ready and CheckExecution")
}

// TestProbeTimeoutDoesNotHangReady is the plan's named T0.12.c case seen from
// the capability side: a dependency probe that ignores its context must not
// stall GET /ready, and the capability set it feeds must keep execution
// unavailable — a Run may not be created from a snapshot whose dependency never
// answered.
func TestProbeTimeoutDoesNotHangReady(t *testing.T) {
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
		{Probe: readiness.NewProbeFunc(readiness.ExecBackendPrefix+"codex", healthy)},
		{Probe: readiness.NewProbeFunc(readiness.ComponentVault, func(context.Context) readiness.Result {
			// Deliberately ignores ctx: the deadline is the runner's contract.
			time.Sleep(5 * time.Second)
			return readiness.Result{State: readiness.StateReady}
		}), Timeout: 50 * time.Millisecond},
	}
	require.NoError(t, readiness.Register(specs...))

	router := readinessProbeTestRouter()
	start := time.Now()
	code, parsed := callReadinessProbeEndpoint(t, router)
	elapsed := time.Since(start)
	t.Logf("first /ready with a hung dependency probe returned in %s (probe timeout 50ms plus the runner's grace)",
		elapsed)

	require.Less(t, elapsed, time.Second, "a hung probe must not block /ready")
	require.Equal(t, http.StatusOK, code,
		"the hung probe is optional and must not 503 the read-only surface: %#v", parsed.Data)

	component := readinessProbeComponents(t, parsed.Data)[readiness.ComponentVault]
	require.NotNil(t, component)
	assert.Equal(t, false, component["ready"])
	assert.Equal(t, readiness.CodeTimeout, component["error_code"])

	// The capability sets agree with the component: the dependency that timed
	// out keeps execution unavailable.
	capabilities := readinessCapabilities(t, parsed.Data)
	assert.Equal(t, string(readiness.CapabilityUnavailable), capabilities.Execution.State,
		"a timed-out dependency must keep execution unavailable")
	assert.Contains(t, blockerNames(capabilities.Execution.Blocking), readiness.ComponentVault,
		"the timed-out dependency must be named as blocking: %+v", capabilities.Execution.Blocking)

	// The Run creation gate refuses for the same snapshot. Wait out the
	// probe's own budget first so the run is unambiguously past its deadline.
	if remaining := 50*time.Millisecond + 500*time.Millisecond - time.Since(start); remaining > 0 {
		time.Sleep(remaining + 50*time.Millisecond)
	}
	refused := readiness.CheckExecution(context.Background(), readiness.Default, "codex", nil)
	assert.Equal(t, string(readiness.CapabilityUnavailable), string(refused.State),
		"CheckExecution must refuse while a dependency probe times out")
	assert.Contains(t, capabilityBlockerNames(refused.Blocking), readiness.ComponentVault,
		"the timed-out dependency must be named as blocking: %+v", refused.Blocking)
}
