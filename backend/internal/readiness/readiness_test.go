package readiness

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// awaitComponents reads the result of one Run from a channel, failing the test
// if the Run does not return in time.
func awaitComponents(t *testing.T, done <-chan map[string]Component, timeout time.Duration) map[string]Component {
	t.Helper()
	select {
	case components := <-done:
		return components
	case <-time.After(timeout):
		t.Fatal("Run did not return in time")
		return nil
	}
}

// TestRunReportsNoComponentsWhenEmpty pins the contract the API layer relies on
// to keep its legacy response shape: an empty registry produces no components
// at all (not an empty map).
func TestRunReportsNoComponentsWhenEmpty(t *testing.T) {
	registry := NewRegistry()
	assert.Empty(t, registry.Registered())
	assert.Nil(t, registry.Run(context.Background()))

	require.NoError(t, registry.Register(Spec{Probe: NewProbeFunc("alpha", func(context.Context) Result {
		return Result{State: StateReady}
	})}))
	require.Len(t, registry.Registered(), 1)

	registry.Clear()
	assert.Empty(t, registry.Registered())
	assert.Nil(t, registry.Run(context.Background()))
}

// TestConcurrentPollsShareInFlightResult covers the shared-run rule: a caller
// that arrives while a healthy probe is still running waits for that run and
// reports the same result instead of failing the poll. Both callers must see
// ready and the probe must run exactly once.
func TestConcurrentPollsShareInFlightResult(t *testing.T) {
	registry := NewRegistry()
	var calls atomic.Int64
	require.NoError(t, registry.Register(Spec{
		Probe: NewProbeFunc("event_store", func(context.Context) Result {
			calls.Add(1)
			time.Sleep(150 * time.Millisecond)
			return Result{State: StateReady, Detail: "shared run"}
		}),
		Required: true,
		Timeout:  time.Second,
	}))

	results := make(chan map[string]Component, 2)
	start := make(chan struct{})
	var waitGroup sync.WaitGroup
	for poll := 0; poll < 2; poll++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			results <- registry.Run(context.Background())
		}()
	}

	began := time.Now()
	close(start)
	waitGroup.Wait()
	elapsed := time.Since(began)
	t.Logf("two concurrent Run calls over a 150ms probe returned in %s; probe invocations=%d",
		elapsed, calls.Load())

	for poll := 0; poll < 2; poll++ {
		components := <-results
		component, ok := components["event_store"]
		require.True(t, ok, "component missing: %#v", components)
		assert.Equal(t, StateReady, component.Result.State, "poll %d must share the healthy run", poll)
		assert.Equal(t, "", component.Result.ErrCode)
		assert.Equal(t, "shared run", component.Result.Detail)
		assert.True(t, component.Required)
		assert.True(t, component.Readonly)
		assert.False(t, component.CheckedAt.IsZero())
	}

	assert.Equal(t, int64(1), calls.Load(), "concurrent polls must share one probe run")
	assert.Less(t, elapsed, 600*time.Millisecond, "a shared run must not be waited on once per caller")
}

// TestStuckProbePastDeadlineReportsTimeoutImmediately covers the boundary
// between sharing and giving up: a caller that arrives while the run is still
// inside its own budget waits for it, while a caller that arrives after the run
// blew its deadline reports probe_timeout at once. A stuck probe is invoked
// exactly once no matter how often it is polled.
func TestStuckProbePastDeadlineReportsTimeoutImmediately(t *testing.T) {
	registry := NewRegistry()
	release := make(chan struct{})
	var calls atomic.Int64
	require.NoError(t, registry.Register(Spec{
		Probe: NewProbeFunc("exec_backend", func(context.Context) Result {
			calls.Add(1)
			<-release
			return Result{State: StateReady}
		}),
		Required: true,
		Timeout:  50 * time.Millisecond,
	}))
	// The probe never returns on its own; the test releases it at the end.
	defer close(release)

	firstDone := make(chan map[string]Component, 1)
	firstStart := time.Now()
	go func() { firstDone <- registry.Run(context.Background()) }()

	// A second caller arrives while the run is still inside its budget: it must
	// wait for the shared run rather than declare it failed on the spot.
	time.Sleep(100 * time.Millisecond)
	secondDone := make(chan map[string]Component, 1)
	go func() { secondDone <- registry.Run(context.Background()) }()
	select {
	case components := <-secondDone:
		t.Fatalf("a caller that arrives inside the run's budget must wait for it, got %#v", components)
	case <-time.After(100 * time.Millisecond):
	}

	first := awaitComponents(t, firstDone, 3*time.Second)
	firstElapsed := time.Since(firstStart)
	second := awaitComponents(t, secondDone, 3*time.Second)
	secondElapsed := time.Since(firstStart)
	t.Logf("first Run returned in %s; concurrent Run returned in %s (probe budget %s)",
		firstElapsed, secondElapsed, 50*time.Millisecond+budgetGrace)

	assert.GreaterOrEqual(t, firstElapsed, 500*time.Millisecond, "the starting caller waits out the probe budget")
	assert.Less(t, firstElapsed, 3*time.Second, "a stuck probe must not block its caller forever")
	for name, components := range map[string]map[string]Component{"first": first, "concurrent": second} {
		component, ok := components["exec_backend"]
		require.True(t, ok, "%s caller: component missing: %#v", name, components)
		assert.Equal(t, StateFailed, component.Result.State, "%s caller", name)
		assert.Equal(t, CodeTimeout, component.Result.ErrCode, "%s caller", name)
		assert.Contains(t, component.Result.Detail, "still running since", "%s caller", name)
	}

	// The run has now blown its deadline; later polls must report it at once
	// instead of waiting for it again.
	for poll := 0; poll < 3; poll++ {
		pollStart := time.Now()
		components := registry.Run(context.Background())
		pollElapsed := time.Since(pollStart)
		t.Logf("poll %d past the deadline returned in %s", poll+1, pollElapsed)

		component, ok := components["exec_backend"]
		require.True(t, ok, "component missing: %#v", components)
		assert.Equal(t, StateFailed, component.Result.State)
		assert.Equal(t, CodeTimeout, component.Result.ErrCode)
		assert.Contains(t, component.Result.Detail, "still running since")
		assert.Less(t, pollElapsed, 50*time.Millisecond, "a run past its deadline must be reported immediately")
	}

	assert.Equal(t, int64(1), calls.Load(), "a stuck probe must stay single-flight")
}

// TestProbeContextDetachedFromRequest covers the shared-context rule: the run
// is started by one caller but may be awaited by others, so the probe's context
// must keep the caller's values without inheriting the caller's cancellation.
func TestProbeContextDetachedFromRequest(t *testing.T) {
	registry := NewRegistry()
	started := make(chan struct{})
	var calls atomic.Int64
	var ctxErrDuringProbe error
	require.NoError(t, registry.Register(Spec{
		Probe: NewProbeFunc("event_store", func(probeCtx context.Context) Result {
			calls.Add(1)
			close(started)
			time.Sleep(100 * time.Millisecond)
			ctxErrDuringProbe = probeCtx.Err()
			if ctxErrDuringProbe != nil {
				return Result{State: StateFailed, ErrCode: "probe_ctx_canceled", Detail: ctxErrDuringProbe.Error()}
			}
			return Result{State: StateReady, Detail: "probe finished"}
		}),
		Required: true,
		Timeout:  time.Second,
	}))

	requestCtx, cancelRequest := context.WithCancel(context.Background())
	defer cancelRequest()

	firstDone := make(chan map[string]Component, 1)
	go func() { firstDone <- registry.Run(requestCtx) }()

	// The first caller goes away as soon as the probe has started.
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("the probe never started")
	}
	cancelRequest()

	secondDone := make(chan map[string]Component, 1)
	go func() { secondDone <- registry.Run(context.Background()) }()

	first := awaitComponents(t, firstDone, 3*time.Second)
	second := awaitComponents(t, secondDone, 3*time.Second)

	assert.NoError(t, ctxErrDuringProbe, "the probe context must not inherit the request's cancellation")
	assert.Equal(t, int64(1), calls.Load(), "both callers must share one probe run")

	_, ok := first["event_store"]
	require.True(t, ok, "the starting caller must still report the component: %#v", first)

	component, ok := second["event_store"]
	require.True(t, ok, "the second caller must report the component: %#v", second)
	assert.Equal(t, StateReady, component.Result.State, "the shared run must survive the first caller leaving")
	assert.Equal(t, "", component.Result.ErrCode)
	assert.Equal(t, "probe finished", component.Result.Detail)
}

// TestProbeExceededDeadlineIsNotReportedReady pins the deadline contract: a
// probe that only returns after its own context expired is reported as
// probe_timeout, never as the state it returned.
func TestProbeExceededDeadlineIsNotReportedReady(t *testing.T) {
	registry := NewRegistry()
	require.NoError(t, registry.Register(Spec{
		Probe: NewProbeFunc("slow_dep", func(ctx context.Context) Result {
			<-ctx.Done()
			time.Sleep(20 * time.Millisecond)
			return Result{State: StateReady, Detail: "late but happy"}
		}),
		Required: true,
		Timeout:  30 * time.Millisecond,
	}))

	components := registry.Run(context.Background())
	component, ok := components["slow_dep"]
	require.True(t, ok, "component missing: %#v", components)
	assert.Equal(t, StateFailed, component.Result.State)
	assert.Equal(t, CodeTimeout, component.Result.ErrCode)
	assert.Contains(t, component.Result.Detail, "exceeded deadline")
}

// TestProbePanicIsReportedAsPanic pins panic recovery at the framework level.
func TestProbePanicIsReportedAsPanic(t *testing.T) {
	registry := NewRegistry()
	require.NoError(t, registry.Register(Spec{
		Probe: NewProbeFunc("migrations", func(context.Context) Result {
			panic("probe exploded")
		}),
	}))

	components := registry.Run(context.Background())
	component, ok := components["migrations"]
	require.True(t, ok, "component missing: %#v", components)
	assert.Equal(t, StateFailed, component.Result.State)
	assert.Equal(t, CodePanic, component.Result.ErrCode)
	assert.Contains(t, component.Result.Detail, "probe exploded")
}

// TestRegistryInstancesAreIsolated pins that an explicit Registry (what the
// bootstrap owns) shares nothing with Default (what the API layer reads).
func TestRegistryInstancesAreIsolated(t *testing.T) {
	healthy := func(context.Context) Result { return Result{State: StateReady} }
	owned := NewRegistry()
	require.NoError(t, owned.Register(Spec{Probe: NewProbeFunc("owned_dep", healthy)}))

	assert.Equal(t, []string{"owned_dep"}, owned.Registered())
	assert.NotContains(t, Registered(), "owned_dep", "Default must not see another registry's probes")

	components := owned.Run(context.Background())
	require.Contains(t, components, "owned_dep")
	assert.Nil(t, Run(context.Background()), "an empty Default must still report no components")
}

// notReadonlyProbe is a probe that declares itself not read-only.
type notReadonlyProbe struct{ name string }

func (p notReadonlyProbe) Name() string                 { return p.name }
func (p notReadonlyProbe) Readonly() bool               { return false }
func (p notReadonlyProbe) Probe(context.Context) Result { return Result{State: StateReady} }

// TestProbeRegistrationValidation covers the batch validation rules: bad specs
// are rejected, a rejected batch installs nothing, and re-registering a name
// replaces it.
func TestProbeRegistrationValidation(t *testing.T) {
	registry := NewRegistry()
	healthy := func(context.Context) Result { return Result{State: StateReady} }
	require.NoError(t, registry.Register(Spec{Probe: NewProbeFunc("alpha", healthy), Required: true}))
	require.Equal(t, []string{"alpha"}, registry.Registered())

	t.Run("nil probe", func(t *testing.T) {
		err := registry.Register(Spec{Required: true})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nil probe")
		assert.Equal(t, []string{"alpha"}, registry.Registered())
	})

	t.Run("empty name", func(t *testing.T) {
		err := registry.Register(Spec{Probe: NewProbeFunc("   ", healthy)})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty name")
		assert.Equal(t, []string{"alpha"}, registry.Registered())
	})

	t.Run("not read-only", func(t *testing.T) {
		err := registry.Register(Spec{Probe: notReadonlyProbe{name: "beta"}})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "read-only")
		assert.Equal(t, []string{"alpha"}, registry.Registered())
	})

	t.Run("negative timeout", func(t *testing.T) {
		err := registry.Register(Spec{Probe: NewProbeFunc("beta", healthy), Timeout: -time.Second})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "negative timeout")
		assert.Equal(t, []string{"alpha"}, registry.Registered())
	})

	t.Run("duplicate in batch", func(t *testing.T) {
		err := registry.Register(
			Spec{Probe: NewProbeFunc("beta", healthy)},
			Spec{Probe: NewProbeFunc("beta", healthy)},
		)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate")
		assert.Equal(t, []string{"alpha"}, registry.Registered())
	})

	t.Run("rejected batch installs nothing", func(t *testing.T) {
		err := registry.Register(
			Spec{Probe: NewProbeFunc("beta", healthy), Required: true},
			Spec{Probe: NewProbeFunc("gamma", healthy), Timeout: -time.Millisecond},
		)
		require.Error(t, err)
		assert.Equal(t, []string{"alpha"}, registry.Registered(),
			"a single invalid spec must reject the whole batch")
	})

	t.Run("re-registration replaces", func(t *testing.T) {
		require.NoError(t, registry.Register(Spec{Probe: NewProbeFunc("beta", healthy), Required: true}))
		require.NoError(t, registry.Register(Spec{Probe: NewProbeFunc("beta", healthy), Required: false}))
		assert.Equal(t, []string{"alpha", "beta"}, registry.Registered())

		byName := make(map[string]Spec)
		for _, spec := range registry.Snapshot() {
			byName[spec.name] = spec
		}
		require.Contains(t, byName, "beta")
		assert.False(t, byName["beta"].Required, "the replacement spec must win")
		assert.True(t, byName["alpha"].Required)
	})

	t.Run("name is trimmed", func(t *testing.T) {
		trimmed := NewRegistry()
		require.NoError(t, trimmed.Register(Spec{Probe: NewProbeFunc("  spaced  ", healthy)}))
		assert.Equal(t, []string{"spaced"}, trimmed.Registered())
	})

	t.Run("timeout defaults when unset", func(t *testing.T) {
		defaulted := NewRegistry()
		require.NoError(t, defaulted.Register(Spec{Probe: NewProbeFunc("alpha", healthy)}))
		specs := defaulted.Snapshot()
		require.Len(t, specs, 1)
		assert.Equal(t, DefaultTimeout, specs[0].Timeout)
		assert.Equal(t, "alpha", specs[0].name)
		assert.True(t, specs[0].readonly)
	})
}

// TestProbeSingleFlightBoundsGoroutines covers the single-flight rule: a
// permanently stuck probe is invoked once no matter how often it is polled, and
// probing resumes after the stuck run returns.
func TestProbeSingleFlightBoundsGoroutines(t *testing.T) {
	registry := NewRegistry()
	release := make(chan struct{})
	finished := make(chan struct{})
	var calls atomic.Int64
	require.NoError(t, registry.Register(Spec{
		Probe: NewProbeFunc("exec_backend", func(context.Context) Result {
			if calls.Add(1) == 1 {
				<-release
				close(finished)
			}
			return Result{State: StateReady}
		}),
		Required: true,
		Timeout:  50 * time.Millisecond,
	}))

	// Warm up: the first poll starts the run, which then blocks forever.
	components := registry.Run(context.Background())
	component, ok := components["exec_backend"]
	require.True(t, ok, "component missing: %#v", components)
	require.Equal(t, CodeTimeout, component.Result.ErrCode)

	before := runtime.NumGoroutine()
	for poll := 0; poll < 20; poll++ {
		polled := registry.Run(context.Background())
		polledComponent, ok := polled["exec_backend"]
		require.True(t, ok, "component missing: %#v", polled)
		assert.Equal(t, CodeTimeout, polledComponent.Result.ErrCode)
		assert.Equal(t, StateFailed, polledComponent.Result.State)
	}
	after := runtime.NumGoroutine()
	t.Logf("goroutines before=%d after=%d; probe invocations=%d", before, after, calls.Load())

	assert.Equal(t, int64(1), calls.Load(), "a stuck probe must be invoked exactly once while it is in flight")
	assert.LessOrEqual(t, after-before, 4, "20 polls of a stuck probe must not grow goroutines without bound")

	close(release)
	<-finished

	// The stuck run has returned; the next poll probes again and reports ready.
	deadline := time.Now().Add(3 * time.Second)
	var last map[string]Component
	var lastComponent Component
	var resumed bool
	for time.Now().Before(deadline) {
		last = registry.Run(context.Background())
		lastComponent, resumed = last["exec_backend"]
		if resumed && lastComponent.Result.State == StateReady {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	require.True(t, resumed, "component missing: %#v", last)
	require.Equal(t, StateReady, lastComponent.Result.State, "probing must resume once the stuck run returned")
	assert.Equal(t, int64(2), calls.Load(), "the resumed poll must run the probe again")

	t.Logf("after release: probe invocations=%d, goroutines=%d", calls.Load(), runtime.NumGoroutine())
}

// TestProbeReplacementDiscardsInFlightResult covers the hand-off rule: a run
// that was still in flight when its spec was replaced must never be written
// into the replacement.
func TestProbeReplacementDiscardsInFlightResult(t *testing.T) {
	registry := NewRegistry()
	release := make(chan struct{})
	var stalledCalls atomic.Int64
	require.NoError(t, registry.Register(Spec{
		Probe: NewProbeFunc("event_store", func(context.Context) Result {
			stalledCalls.Add(1)
			<-release
			return Result{State: StateFailed, ErrCode: "stale_run", Detail: "old spec"}
		}),
		Required: true,
		Timeout:  50 * time.Millisecond,
	}))

	components := registry.Run(context.Background())
	component, ok := components["event_store"]
	require.True(t, ok, "component missing: %#v", components)
	require.Equal(t, CodeTimeout, component.Result.ErrCode)

	// Replace the spec while the old run is still stuck.
	require.NoError(t, registry.Register(Spec{
		Probe: NewProbeFunc("event_store", func(context.Context) Result {
			return Result{State: StateReady, Detail: "new spec"}
		}),
		Required: true,
		Timeout:  50 * time.Millisecond,
	}))

	components = registry.Run(context.Background())
	component, ok = components["event_store"]
	require.True(t, ok, "component missing: %#v", components)
	assert.Equal(t, StateReady, component.Result.State, "the replacement probe must be probed, not the old run")
	assert.Equal(t, "new spec", component.Result.Detail)
	assert.Equal(t, "", component.Result.ErrCode)
	assert.Equal(t, int64(1), stalledCalls.Load(), "the replaced probe must not be run again")

	// Letting the replaced run finish must not change the replacement's state.
	close(release)
	time.Sleep(50 * time.Millisecond)
	components = registry.Run(context.Background())
	component, ok = components["event_store"]
	require.True(t, ok, "component missing: %#v", components)
	assert.Equal(t, StateReady, component.Result.State)
	assert.Equal(t, "", component.Result.ErrCode)
}

// TestProbeStateCodesAreStable covers the state-to-code contract for every
// non-ready state, including probes that return no code and probes that return
// a state outside the enumeration.
func TestProbeStateCodesAreStable(t *testing.T) {
	registry := NewRegistry()
	require.NoError(t, registry.Register(
		Spec{Probe: NewProbeFunc("degraded_dep", func(context.Context) Result {
			return Result{State: StateDegraded}
		})},
		Spec{Probe: NewProbeFunc("failed_dep", func(context.Context) Result {
			return Result{State: StateFailed}
		})},
		Spec{Probe: NewNotConfiguredProbe("vault", "no vault configured")},
		Spec{Probe: NewProbeFunc("bogus_dep", func(context.Context) Result {
			return Result{State: State("melted")}
		})},
		Spec{Probe: NewProbeFunc("ready_with_stale_code", func(context.Context) Result {
			return Result{State: StateReady, ErrCode: "leftover_code"}
		})},
	))

	components := registry.Run(context.Background())
	expected := map[string]string{
		"degraded_dep":          CodeDegraded,
		"failed_dep":            CodeFailed,
		"vault":                 CodeNotConfigured,
		"bogus_dep":             CodeFailed,
		"ready_with_stale_code": "",
	}
	for name, expectedCode := range expected {
		component, ok := components[name]
		require.True(t, ok, "component %q missing", name)
		if expectedCode == "" {
			assert.Equal(t, "", component.Result.ErrCode, "a ready probe must not publish a code")
			assert.Equal(t, StateReady, component.Result.State)
			continue
		}
		assert.Equal(t, expectedCode, component.Result.ErrCode, "component %q", name)
		assert.NotEqual(t, StateReady, component.Result.State, "component %q", name)
	}

	assert.Equal(t, StateFailed, components["bogus_dep"].Result.State,
		"an unrecognized state must be reported as failed")
	assert.Contains(t, components["bogus_dep"].Result.Detail, "melted")
	assert.Equal(t, StateNotConfigured, components["vault"].Result.State)
	assert.Equal(t, "no vault configured", components["vault"].Result.Detail)
	assert.Equal(t, StateDegraded, components["degraded_dep"].Result.State)
}

// TestProbeConcurrentRunAndRegistration hammers Run while probes are registered
// and cleared from other goroutines: no panic, no torn result.
func TestProbeConcurrentRunAndRegistration(t *testing.T) {
	registry := NewRegistry()
	healthy := func(context.Context) Result { return Result{State: StateReady} }
	require.NoError(t, registry.Register(
		Spec{Probe: NewProbeFunc("alpha", healthy), Required: true},
		Spec{Probe: NewProbeFunc("beta", healthy)},
	))

	var waitGroup sync.WaitGroup
	stop := make(chan struct{})
	failures := make([]int32, 8)

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
				components := registry.Run(context.Background())
				for name, component := range components {
					if component.Result.State != StateReady && component.Result.State != StateFailed {
						atomic.StoreInt32(&failures[worker], 1)
						t.Logf("worker %d saw %q in state %q", worker, name, component.Result.State)
						return
					}
				}
			}
		}(worker)
	}

	for round := 0; round < 30; round++ {
		require.NoError(t, registry.Register(
			Spec{Probe: NewProbeFunc("alpha", healthy), Required: true},
			Spec{Probe: NewProbeFunc("beta", healthy)},
		))
		registry.Clear()
	}
	close(stop)
	waitGroup.Wait()

	for worker, failure := range failures {
		assert.Equal(t, int32(0), failure, "worker %d saw an unexpected result", worker)
	}
}

// TestProbeClearDropsInFlightResult covers Clear while a run is still in
// flight: the cleared probe disappears from the result, its late result is
// never published, and the slot is not reused.
func TestProbeClearDropsInFlightResult(t *testing.T) {
	registry := NewRegistry()
	release := make(chan struct{})
	var calls atomic.Int64
	require.NoError(t, registry.Register(Spec{
		Probe: NewProbeFunc("event_store", func(context.Context) Result {
			calls.Add(1)
			<-release
			return Result{State: StateReady}
		}),
		Required: true,
		Timeout:  50 * time.Millisecond,
	}))

	components := registry.Run(context.Background())
	require.Contains(t, components, "event_store")

	registry.Clear()
	assert.Nil(t, registry.Run(context.Background()), "the cleared probe must not gate the verdict")

	// The abandoned run finishes late; nothing may resurface.
	close(release)
	time.Sleep(50 * time.Millisecond)
	assert.Nil(t, registry.Run(context.Background()),
		"a late result from a cleared probe must not be published")
	assert.Equal(t, int64(1), calls.Load(), "a cleared probe must not be run again")
}
