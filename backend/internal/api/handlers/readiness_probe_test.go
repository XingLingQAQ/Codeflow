package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
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

// clearReadinessProbesForTest isolates the global probe registry per test.
func clearReadinessProbesForTest(t *testing.T) {
	t.Helper()
	ClearReadinessProbes()
	t.Cleanup(ClearReadinessProbes)
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
	assert.Empty(t, RegisteredReadinessProbes())

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

	// Re-encoding the legacy payload must reproduce the body exactly: same field
	// names, same field order, same omission rules.
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
	assert.Equal(t, string(expected), recorder.Body.String())

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
	require.NoError(t, RegisterReadinessProbes(ReadinessProbeSpec{
		Probe: NewProbeFunc("event_store", func(context.Context) ProbeResult {
			calls.Add(1)
			return ProbeResult{State: ProbeStateReady, Detail: "sqlite ok"}
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

	require.NoError(t, RegisterReadinessProbes(ReadinessProbeSpec{
		Probe: NewProbeFunc("vault", func(context.Context) ProbeResult {
			return ProbeResult{State: ProbeStateFailed, ErrCode: "vault_locked", Detail: "sealed"}
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

	require.NoError(t, RegisterReadinessProbes(ReadinessProbeSpec{
		Probe: NewProbeFunc("event_store", func(context.Context) ProbeResult {
			return ProbeResult{State: ProbeStateFailed, ErrCode: "event_store_unavailable", Detail: "no wal"}
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
// context: /ready must answer within the probe budget and report probe_timeout.
func TestReadinessProbeTimeoutDoesNotHangReady(t *testing.T) {
	stubRequiredReadinessServices(t)
	clearReadinessProbesForTest(t)

	require.NoError(t, RegisterReadinessProbes(ReadinessProbeSpec{
		Probe: NewProbeFunc("exec_backend", func(context.Context) ProbeResult {
			// Deliberately ignores ctx: the deadline is the runner's contract.
			time.Sleep(5 * time.Second)
			return ProbeResult{State: ProbeStateReady}
		}),
		Required: true,
		Timeout:  50 * time.Millisecond,
	}))

	router := readinessProbeTestRouter()
	start := time.Now()
	code, parsed := callReadinessProbeEndpoint(t, router)
	elapsed := time.Since(start)
	t.Logf("first /ready with a hung probe returned in %s (probe budget %s)",
		elapsed, 50*time.Millisecond+probeBudgetGrace)

	require.Less(t, elapsed, time.Second, "a hung probe must not block /ready")
	assert.Equal(t, http.StatusServiceUnavailable, code)
	assert.Equal(t, "not_ready", parsed.Data["status"])

	component := readinessProbeComponents(t, parsed.Data)["exec_backend"]
	require.NotNil(t, component)
	assert.Equal(t, false, component["ready"])
	assert.Equal(t, "failed", component["status"])
	assert.Equal(t, ProbeErrTimeout, component["error_code"])
	detail, _ := component["detail"].(string)
	require.Contains(t, detail, "still running since")
	stamp := strings.TrimSpace(strings.TrimPrefix(detail, "still running since "))
	_, err := time.Parse(time.RFC3339, stamp)
	assert.NoError(t, err, "detail must carry an RFC3339 stamp, got %q", detail)

	// The run is still in flight; a second poll answers immediately.
	second := time.Now()
	secondCode, secondParsed := callReadinessProbeEndpoint(t, router)
	secondElapsed := time.Since(second)
	t.Logf("second /ready with the run still in flight returned in %s", secondElapsed)
	assert.Less(t, secondElapsed, 200*time.Millisecond, "an in-flight run must be reported, not waited on")
	assert.Equal(t, http.StatusServiceUnavailable, secondCode)
	secondComponent := readinessProbeComponents(t, secondParsed.Data)["exec_backend"]
	assert.Equal(t, ProbeErrTimeout, secondComponent["error_code"])
}

// TestReadinessProbePanicIsReported covers a panicking probe.
func TestReadinessProbePanicIsReported(t *testing.T) {
	stubRequiredReadinessServices(t)
	clearReadinessProbesForTest(t)

	require.NoError(t, RegisterReadinessProbes(ReadinessProbeSpec{
		Probe: NewProbeFunc("migrations", func(context.Context) ProbeResult {
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
	assert.Equal(t, ProbeErrPanic, component["error_code"])
	detail, _ := component["detail"].(string)
	assert.Contains(t, detail, "probe exploded")
	assert.Contains(t, component, "checked_at")
}

// TestReadinessProbeSingleFlightBoundsGoroutines covers the single-flight rule:
// a permanently stuck probe is invoked once no matter how often /ready polls,
// and probing resumes after the stuck run returns.
func TestReadinessProbeSingleFlightBoundsGoroutines(t *testing.T) {
	stubRequiredReadinessServices(t)
	clearReadinessProbesForTest(t)

	release := make(chan struct{})
	finished := make(chan struct{})
	var calls atomic.Int64
	require.NoError(t, RegisterReadinessProbes(ReadinessProbeSpec{
		Probe: NewProbeFunc("exec_backend", func(context.Context) ProbeResult {
			if calls.Add(1) == 1 {
				<-release
				close(finished)
			}
			return ProbeResult{State: ProbeStateReady}
		}),
		Required: true,
		Timeout:  50 * time.Millisecond,
	}))

	router := readinessProbeTestRouter()

	// Warm up: the first poll starts the run, which then blocks forever.
	code, parsed := callReadinessProbeEndpoint(t, router)
	require.Equal(t, http.StatusServiceUnavailable, code)
	require.Equal(t, ProbeErrTimeout,
		readinessProbeComponents(t, parsed.Data)["exec_backend"]["error_code"])

	before := runtime.NumGoroutine()
	for i := 0; i < 20; i++ {
		pollCode, pollParsed := callReadinessProbeEndpoint(t, router)
		assert.Equal(t, http.StatusServiceUnavailable, pollCode)
		component := readinessProbeComponents(t, pollParsed.Data)["exec_backend"]
		assert.Equal(t, ProbeErrTimeout, component["error_code"])
		assert.Equal(t, false, component["ready"])
	}
	after := runtime.NumGoroutine()
	t.Logf("goroutines before=%d after=%d; probe invocations=%d", before, after, calls.Load())

	assert.Equal(t, int64(1), calls.Load(), "a stuck probe must be invoked exactly once while it is in flight")
	assert.LessOrEqual(t, after-before, 4, "20 polls of a stuck probe must not grow goroutines without bound")

	close(release)
	<-finished

	// The stuck run has returned; the next poll probes again and reports ready.
	deadline := time.Now().Add(3 * time.Second)
	var last readinessTestResponse
	var lastCode int
	for time.Now().Before(deadline) {
		lastCode, last = callReadinessProbeEndpoint(t, router)
		if lastCode == http.StatusOK {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	require.Equal(t, http.StatusOK, lastCode, "probing must resume once the stuck run returned")
	assert.Equal(t, "ready", last.Data["status"])
	component := readinessProbeComponents(t, last.Data)["exec_backend"]
	assert.Equal(t, true, component["ready"])
	assert.Equal(t, "ready", component["status"])
	assert.Equal(t, int64(2), calls.Load(), "the resumed poll must run the probe again")

	t.Logf("after release: probe invocations=%d, goroutines=%d", calls.Load(), runtime.NumGoroutine())
}

// notReadonlyProbe is a probe that declares itself not read-only.
type notReadonlyProbe struct{ name string }

func (p notReadonlyProbe) Name() string   { return p.name }
func (p notReadonlyProbe) Readonly() bool { return false }
func (p notReadonlyProbe) Probe(context.Context) ProbeResult {
	return ProbeResult{State: ProbeStateReady}
}

// TestReadinessProbeRegistrationValidation covers the batch validation rules:
// bad specs are rejected, a rejected batch installs nothing, and re-registering
// a name replaces it.
func TestReadinessProbeRegistrationValidation(t *testing.T) {
	clearReadinessProbesForTest(t)

	healthy := func(context.Context) ProbeResult { return ProbeResult{State: ProbeStateReady} }
	require.NoError(t, RegisterReadinessProbes(ReadinessProbeSpec{
		Probe: NewProbeFunc("alpha", healthy), Required: true,
	}))
	require.Equal(t, []string{"alpha"}, RegisteredReadinessProbes())

	t.Run("nil probe", func(t *testing.T) {
		err := RegisterReadinessProbes(ReadinessProbeSpec{Required: true})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nil probe")
		assert.Equal(t, []string{"alpha"}, RegisteredReadinessProbes())
	})

	t.Run("empty name", func(t *testing.T) {
		err := RegisterReadinessProbes(ReadinessProbeSpec{Probe: NewProbeFunc("   ", healthy)})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty name")
		assert.Equal(t, []string{"alpha"}, RegisteredReadinessProbes())
	})

	t.Run("not read-only", func(t *testing.T) {
		err := RegisterReadinessProbes(ReadinessProbeSpec{Probe: notReadonlyProbe{name: "beta"}})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "read-only")
		assert.Equal(t, []string{"alpha"}, RegisteredReadinessProbes())
	})

	t.Run("negative timeout", func(t *testing.T) {
		err := RegisterReadinessProbes(ReadinessProbeSpec{
			Probe: NewProbeFunc("beta", healthy), Timeout: -time.Second,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "negative timeout")
		assert.Equal(t, []string{"alpha"}, RegisteredReadinessProbes())
	})

	t.Run("duplicate in batch", func(t *testing.T) {
		err := RegisterReadinessProbes(
			ReadinessProbeSpec{Probe: NewProbeFunc("beta", healthy)},
			ReadinessProbeSpec{Probe: NewProbeFunc("beta", healthy)},
		)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate")
		assert.Equal(t, []string{"alpha"}, RegisteredReadinessProbes())
	})

	t.Run("rejected batch installs nothing", func(t *testing.T) {
		err := RegisterReadinessProbes(
			ReadinessProbeSpec{Probe: NewProbeFunc("beta", healthy), Required: true},
			ReadinessProbeSpec{Probe: NewProbeFunc("gamma", healthy), Timeout: -time.Millisecond},
		)
		require.Error(t, err)
		assert.Equal(t, []string{"alpha"}, RegisteredReadinessProbes(),
			"a single invalid spec must reject the whole batch")
	})

	t.Run("re-registration replaces", func(t *testing.T) {
		require.NoError(t, RegisterReadinessProbes(ReadinessProbeSpec{
			Probe: NewProbeFunc("beta", healthy), Required: true,
		}))
		require.NoError(t, RegisterReadinessProbes(ReadinessProbeSpec{
			Probe: NewProbeFunc("beta", healthy), Required: false,
		}))
		assert.Equal(t, []string{"alpha", "beta"}, RegisteredReadinessProbes())

		stubRequiredReadinessServices(t)
		_, parsed := callReadinessProbeEndpoint(t, readinessProbeTestRouter())
		component := readinessProbeComponents(t, parsed.Data)["beta"]
		require.NotNil(t, component)
		assert.Equal(t, false, component["required"], "the replacement spec must win")
	})

	t.Run("timeout defaults when unset", func(t *testing.T) {
		ClearReadinessProbes()
		require.NoError(t, RegisterReadinessProbes(ReadinessProbeSpec{
			Probe: NewProbeFunc("alpha", healthy),
		}))
		specs := snapshotReadinessProbes()
		require.Len(t, specs, 1)
		assert.Equal(t, DefaultProbeTimeout, specs[0].Timeout)
		assert.Equal(t, "alpha", specs[0].name)
		assert.True(t, specs[0].readonly)
	})
}

// TestReadinessProbeReplacementDiscardsInFlightResult covers the hand-off rule:
// a run that was still in flight when its spec was replaced must never be
// written into the replacement.
func TestReadinessProbeReplacementDiscardsInFlightResult(t *testing.T) {
	stubRequiredReadinessServices(t)
	clearReadinessProbesForTest(t)

	release := make(chan struct{})
	var stalledCalls atomic.Int64
	require.NoError(t, RegisterReadinessProbes(ReadinessProbeSpec{
		Probe: NewProbeFunc("event_store", func(context.Context) ProbeResult {
			stalledCalls.Add(1)
			<-release
			return ProbeResult{State: ProbeStateFailed, ErrCode: "stale_run", Detail: "old spec"}
		}),
		Required: true,
		Timeout:  50 * time.Millisecond,
	}))

	router := readinessProbeTestRouter()
	code, parsed := callReadinessProbeEndpoint(t, router)
	require.Equal(t, http.StatusServiceUnavailable, code)
	require.Equal(t, ProbeErrTimeout,
		readinessProbeComponents(t, parsed.Data)["event_store"]["error_code"])

	// Replace the spec while the old run is still stuck.
	require.NoError(t, RegisterReadinessProbes(ReadinessProbeSpec{
		Probe: NewProbeFunc("event_store", func(context.Context) ProbeResult {
			return ProbeResult{State: ProbeStateReady, Detail: "new spec"}
		}),
		Required: true,
		Timeout:  50 * time.Millisecond,
	}))

	code, parsed = callReadinessProbeEndpoint(t, router)
	require.Equal(t, http.StatusOK, code, "the replacement probe must be probed, not the old run")
	component := readinessProbeComponents(t, parsed.Data)["event_store"]
	assert.Equal(t, true, component["ready"])
	assert.Equal(t, "ready", component["status"])
	assert.Equal(t, "new spec", component["detail"])
	assert.NotContains(t, component, "error_code")
	assert.Equal(t, int64(1), stalledCalls.Load(), "the replaced probe must not be run again")

	// Letting the replaced run finish must not change the replacement's state.
	close(release)
	time.Sleep(50 * time.Millisecond)
	code, parsed = callReadinessProbeEndpoint(t, router)
	require.Equal(t, http.StatusOK, code)
	component = readinessProbeComponents(t, parsed.Data)["event_store"]
	assert.Equal(t, true, component["ready"])
	assert.NotContains(t, component, "error_code")
}

// TestReadinessProbeStateCodesAreStable covers the state-to-code contract for
// every non-ready state, including probes that return no code and probes that
// return a state outside the enumeration.
func TestReadinessProbeStateCodesAreStable(t *testing.T) {
	stubRequiredReadinessServices(t)
	clearReadinessProbesForTest(t)

	require.NoError(t, RegisterReadinessProbes(
		ReadinessProbeSpec{Probe: NewProbeFunc("degraded_dep", func(context.Context) ProbeResult {
			return ProbeResult{State: ProbeStateDegraded}
		})},
		ReadinessProbeSpec{Probe: NewProbeFunc("failed_dep", func(context.Context) ProbeResult {
			return ProbeResult{State: ProbeStateFailed}
		})},
		ReadinessProbeSpec{Probe: NewNotConfiguredProbe("vault", "no vault configured")},
		ReadinessProbeSpec{Probe: NewProbeFunc("bogus_dep", func(context.Context) ProbeResult {
			return ProbeResult{State: ProbeState("melted")}
		})},
		ReadinessProbeSpec{Probe: NewProbeFunc("ready_with_stale_code", func(context.Context) ProbeResult {
			return ProbeResult{State: ProbeStateReady, ErrCode: "leftover_code"}
		})},
	))

	code, parsed := callReadinessProbeEndpoint(t, readinessProbeTestRouter())
	require.Equal(t, http.StatusOK, code)
	components := readinessProbeComponents(t, parsed.Data)

	expected := map[string]string{
		"degraded_dep":          ProbeErrDegraded,
		"failed_dep":            ProbeErrFailed,
		"vault":                 ProbeErrNotConfigured,
		"bogus_dep":             ProbeErrFailed,
		"ready_with_stale_code": "",
	}
	for name, expectedCode := range expected {
		component, ok := components[name]
		require.True(t, ok, "component %q missing", name)
		if expectedCode == "" {
			assert.NotContains(t, component, "error_code", "a ready probe must not publish a code")
			continue
		}
		assert.Equal(t, expectedCode, component["error_code"], "component %q", name)
		assert.Equal(t, false, component["ready"], "component %q", name)
	}

	assert.Equal(t, "failed", components["bogus_dep"]["status"],
		"an unrecognized state must be reported as failed")
	assert.Equal(t, "not_configured", components["vault"]["status"])
	assert.Equal(t, "no vault configured", components["vault"]["detail"])
	assert.Equal(t, "degraded", components["degraded_dep"]["status"])
}

// TestReadinessProbeOverridesLegacyComponent covers a probe whose name collides
// with a legacy Has* component: the probe payload wins and keeps the
// ready/required semantics.
func TestReadinessProbeOverridesLegacyComponent(t *testing.T) {
	stubRequiredReadinessServices(t)
	clearReadinessProbesForTest(t)

	require.NoError(t, RegisterReadinessProbes(ReadinessProbeSpec{
		Probe: NewProbeFunc("memory", func(context.Context) ProbeResult {
			return ProbeResult{State: ProbeStateFailed, ErrCode: "memory_store_down"}
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
	require.NoError(t, RegisterReadinessProbes(ReadinessProbeSpec{
		Probe: NewProbeFunc("memory", func(context.Context) ProbeResult {
			return ProbeResult{State: ProbeStateFailed, ErrCode: "memory_store_down"}
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
	healthy := func(context.Context) ProbeResult { return ProbeResult{State: ProbeStateReady} }
	require.NoError(t, RegisterReadinessProbes(
		ReadinessProbeSpec{Probe: NewProbeFunc("alpha", healthy), Required: true},
		ReadinessProbeSpec{Probe: NewProbeFunc("beta", healthy)},
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
		require.NoError(t, RegisterReadinessProbes(
			ReadinessProbeSpec{Probe: NewProbeFunc("alpha", healthy), Required: true},
			ReadinessProbeSpec{Probe: NewProbeFunc("beta", healthy)},
		))
		ClearReadinessProbes()
	}
	close(stop)
	waitGroup.Wait()

	for worker, status := range statuses {
		assert.Equal(t, int32(0), status, "worker %d saw an unexpected response", worker)
	}
}

// TestReadinessProbeClearDropsInFlightProbe covers ClearReadinessProbes while a
// run is still in flight: the cleared probe disappears from the response, its
// late result is never published, and the slot is not reused.
func TestReadinessProbeClearDropsInFlightProbe(t *testing.T) {
	stubRequiredReadinessServices(t)
	clearReadinessProbesForTest(t)

	release := make(chan struct{})
	var calls atomic.Int64
	require.NoError(t, RegisterReadinessProbes(ReadinessProbeSpec{
		Probe: NewProbeFunc("event_store", func(context.Context) ProbeResult {
			calls.Add(1)
			<-release
			return ProbeResult{State: ProbeStateReady}
		}),
		Required: true,
		Timeout:  50 * time.Millisecond,
	}))

	router := readinessProbeTestRouter()
	code, parsed := callReadinessProbeEndpoint(t, router)
	require.Equal(t, http.StatusServiceUnavailable, code)
	require.Contains(t, readinessProbeComponents(t, parsed.Data), "event_store")

	ClearReadinessProbes()
	code, parsed = callReadinessProbeEndpoint(t, router)
	require.Equal(t, http.StatusOK, code, "the cleared probe must not gate the verdict")
	assert.NotContains(t, readinessProbeComponents(t, parsed.Data), "event_store")

	// The abandoned run finishes late; nothing may resurface.
	close(release)
	time.Sleep(50 * time.Millisecond)
	code, parsed = callReadinessProbeEndpoint(t, router)
	require.Equal(t, http.StatusOK, code)
	assert.NotContains(t, readinessProbeComponents(t, parsed.Data), "event_store",
		"a late result from a cleared probe must not be published")
	assert.Equal(t, int64(1), calls.Load(), "a cleared probe must not be run again")
}
