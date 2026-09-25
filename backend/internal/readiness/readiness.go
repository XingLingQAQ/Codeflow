// Package readiness runs the runtime capability checks behind GET /ready
// (plan section 15 T0.12 / I-54). It is deliberately free of HTTP and business
// dependencies: the probe registry only imports the standard library, so
// internal/run and the bootstrap wiring can re-check execution capability right
// before a Run is created without going through the API layer.
//
// The package contract:
//   - Every probe is read-only and declares itself so; writing probes are
//     rejected at registration.
//   - Registration is all-or-nothing and idempotent per name: re-registering a
//     name replaces the previous spec together with its in-flight run.
//   - Every probe has at most one run in flight. A caller that arrives while a
//     run is going waits for that run and shares its result as long as the run
//     has not passed its own deadline; a run that ignored its deadline is
//     reported as probe_timeout without starting a second run.
//   - A probe's context derives from the caller's values but not from the
//     caller's cancellation, so one departing request cannot cancel the run
//     other callers are waiting on.
//   - Nothing is cached across Run calls: the next poll after an in-flight run
//     returned starts a fresh run.
package readiness

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// State is the machine-readable readiness state of one dependency (plan section
// 15 T0.12): ready, degraded, failed, or not_configured.
type State string

const (
	StateReady         State = "ready"
	StateDegraded      State = "degraded"
	StateFailed        State = "failed"
	StateNotConfigured State = "not_configured"
)

// Machine-readable probe error codes (stable for consumers; plan section 15
// T0.12 remediation codes). Component probes may return their own specific
// codes; the runner reserves the five below for the outcomes it determines
// itself and substitutes them whenever a probe reports a non-ready state
// without a code, so every non-ready component carries a stable error_code.
const (
	// CodeTimeout marks a probe that exceeded its context deadline, or a probe
	// whose previous run is still in flight past its own deadline
	// (single-flight, T0.12.a). The component is reported not ready; /ready
	// itself still answers fast.
	CodeTimeout = "probe_timeout"
	// CodePanic marks a probe whose call panicked; the runner recovers.
	CodePanic = "probe_panic"
	// CodeFailed is the fallback code for StateFailed.
	CodeFailed = "probe_failed"
	// CodeDegraded is the fallback code for StateDegraded.
	CodeDegraded = "probe_degraded"
	// CodeNotConfigured is the fallback code for StateNotConfigured.
	CodeNotConfigured = "probe_not_configured"
)

// Result is the outcome of a single probe run. ErrCode is the machine-readable
// code of the most recent failure; it is empty when State is StateReady. Detail
// is optional human-readable context.
type Result struct {
	State   State
	ErrCode string
	Detail  string
}

// Probe is a readiness check for one dependency. Probes must be read-only
// (Readonly reports true and the check has no side effects on production state:
// it may observe process globals, open private in-memory resources, or stat
// paths, but never write) and must honor ctx cancellation. The runner enforces
// the deadline even against misbehaving probes.
type Probe interface {
	Name() string
	Readonly() bool
	Probe(ctx context.Context) Result
}

// probeFunc adapts a plain function into a read-only Probe.
type probeFunc struct {
	name string
	fn   func(context.Context) Result
}

func (f probeFunc) Name() string                     { return f.name }
func (f probeFunc) Readonly() bool                   { return true }
func (f probeFunc) Probe(ctx context.Context) Result { return f.fn(ctx) }

// NewProbeFunc returns a read-only probe with the given name backed by fn.
func NewProbeFunc(name string, fn func(context.Context) Result) Probe {
	return probeFunc{name: name, fn: fn}
}

// NewNotConfiguredProbe returns a probe that always reports
// StateNotConfigured. It exists so dependencies that are planned but not wired
// yet (vault, event_store, exec_backend:<name>, ...) are visible in /ready as
// not_configured instead of pretending to be ready. The runner fills in its
// error_code (probe_not_configured).
func NewNotConfiguredProbe(name, detail string) Probe {
	return probeFunc{name: name, fn: func(context.Context) Result {
		return Result{State: StateNotConfigured, Detail: detail}
	}}
}

// DefaultTimeout bounds a single probe when Spec.Timeout is not set.
const DefaultTimeout = 2 * time.Second

// budgetGrace is added to a probe's own deadline to form the wall-clock bound on
// how long one /ready call waits for that probe's run to deliver its completion
// signal. A probe that ignores its context is reported as probe_timeout at this
// point; its run stays in flight, and later callers report that slot without
// starting a second run (single-flight, T0.12.a). It is also the point at which
// a caller stops waiting for a run started by someone else and reports
// probe_timeout instead.
const budgetGrace = 500 * time.Millisecond

// Spec bundles a probe with its gating metadata.
type Spec struct {
	Probe Probe
	// Required probes gate the overall readiness verdict: a required probe that
	// is not StateReady makes /ready answer not_ready (503).
	Required bool
	// Timeout bounds the probe's context; <= 0 selects DefaultTimeout.
	Timeout time.Duration

	// name and readonly are captured once, at registration, so that running the
	// probes never calls back into probe code for metadata.
	name     string
	readonly bool
}

// Component is one probe run's outcome plus the metadata a consumer needs to
// report it. Name is the registered name; Result, Latency and CheckedAt
// describe the run this caller observed, which may be a run it shared with
// another caller.
type Component struct {
	Name     string
	Required bool
	Readonly bool
	Result   Result
	// Latency is the wall time the probe itself took, or - for a run that was
	// already in flight when this caller arrived - the time since that run
	// started, as observed by the caller.
	Latency   time.Duration
	CheckedAt time.Time
}

// probeCall is one in-flight run of a registered probe. The runner goroutine
// writes result/latency/finishedAt and then closes done; every reader may touch
// those fields only after observing done closed.
type probeCall struct {
	startedAt  time.Time
	done       chan struct{}
	result     Result
	latency    time.Duration
	finishedAt time.Time
}

func newProbeCall() *probeCall {
	return &probeCall{startedAt: time.Now(), done: make(chan struct{})}
}

func (c *probeCall) complete(result Result, latency time.Duration) {
	c.result = result
	c.latency = latency
	c.finishedAt = time.Now()
	close(c.done)
}

// completed reports whether the run has finished without blocking.
func (c *probeCall) completed() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

// pastDeadline reports whether the run started more than timeout plus the grace
// margin ago, i.e. whether it ignored its own deadline. A caller that finds a
// run past its deadline reports probe_timeout immediately instead of waiting.
func (c *probeCall) pastDeadline(timeout time.Duration) bool {
	return time.Since(c.startedAt) > timeout+budgetGrace
}

// inFlightDetail is the detail shared by every path that reports a slot whose
// run has not returned yet.
func (c *probeCall) inFlightDetail() string {
	return "still running since " + c.startedAt.UTC().Format(time.RFC3339)
}

type entry struct {
	spec Spec
	// call is non-nil exactly while a run of spec is in flight. Registering a
	// replacement or Clear drops the reference, so a late result from a
	// replaced probe can never be written into the replacement's state.
	call *probeCall
}

// Registry holds the probe set of one consumer. The package-level Default
// registry backs the convenience helpers the API layer calls; a consumer that
// needs an isolated probe set (the bootstrap, tests) owns an explicit Registry.
type Registry struct {
	mu      sync.RWMutex
	entries map[string]*entry
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{entries: map[string]*entry{}}
}

// Register validates and installs probe specs atomically: either all specs are
// accepted or none are. Re-registering a name replaces the previous spec along
// with any in-flight run, so repeated bootstrap Apply calls are idempotent. A
// probe that does not declare itself read-only is rejected.
func (r *Registry) Register(specs ...Spec) error {
	normalized := make([]Spec, 0, len(specs))
	seen := make(map[string]struct{}, len(specs))
	for _, spec := range specs {
		if spec.Probe == nil {
			return fmt.Errorf("readiness probe: nil probe")
		}
		name := strings.TrimSpace(spec.Probe.Name())
		if name == "" {
			return fmt.Errorf("readiness probe: empty name")
		}
		if !spec.Probe.Readonly() {
			return fmt.Errorf("readiness probe %q: probes must be read-only", name)
		}
		if spec.Timeout < 0 {
			return fmt.Errorf("readiness probe %q: negative timeout", name)
		}
		if _, duplicate := seen[name]; duplicate {
			return fmt.Errorf("readiness probe %q: duplicate in registration batch", name)
		}
		seen[name] = struct{}{}
		if spec.Timeout == 0 {
			spec.Timeout = DefaultTimeout
		}
		spec.name = name
		spec.readonly = true
		normalized = append(normalized, spec)
	}
	// Every spec was validated above, so installation cannot fail: the batch is
	// installed all-or-nothing under one lock.
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.entries == nil {
		r.entries = map[string]*entry{}
	}
	for _, spec := range normalized {
		r.entries[spec.name] = &entry{spec: spec}
	}
	return nil
}

// Clear removes every registered probe along with its in-flight run. It is the
// bootstrap Reset counterpart of Register; tests use it for isolation.
func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = map[string]*entry{}
}

// Registered returns the registered probe names, sorted. It is intended for
// diagnostics and tests.
func (r *Registry) Registered() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.entries))
	for name := range r.entries {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Snapshot returns the registered specs, sorted by name.
func (r *Registry) Snapshot() []Spec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Spec, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, e.spec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// acquire reserves the single-flight slot of one probe. It returns the call to
// wait on and true when this caller started the run, or the still-running call
// and false when another caller already owns it. A slot whose run finished
// after its own caller gave up is discarded and re-run, so results are fresh.
// A nil call means the probe is no longer registered.
func (r *Registry) acquire(name string) (*probeCall, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[name]
	if !ok {
		return nil, false
	}
	if e.call == nil || e.call.completed() {
		call := newProbeCall()
		e.call = call
		return call, true
	}
	return e.call, false
}

// release frees the slot after the caller consumed call's result. A slot that a
// replacement or Clear already moved on from is left alone.
func (r *Registry) release(name string, call *probeCall) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.entries[name]; ok && e.call == call {
		e.call = nil
	}
}

// Run executes every registered probe and returns one component per probe,
// keyed by probe name. Probes run concurrently, each under its own context
// deadline. Returns nil when no probes are registered, so a consumer keeps its
// legacy response shape byte for byte.
func (r *Registry) Run(ctx context.Context) map[string]Component {
	specs := r.Snapshot()
	if len(specs) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	out := make(map[string]Component, len(specs))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, spec := range specs {
		wg.Add(1)
		go func(spec Spec) {
			defer wg.Done()
			component, report := r.runOne(ctx, spec)
			if !report {
				return
			}
			mu.Lock()
			out[component.Name] = component
			mu.Unlock()
		}(spec)
	}
	wg.Wait()
	return out
}

// runOne performs, or observes, the single run of one probe. The second result
// is false when the probe was unregistered while the check was in flight, in
// which case the response omits it.
func (r *Registry) runOne(ctx context.Context, spec Spec) (Component, bool) {
	call, acquired := r.acquire(spec.name)
	if call == nil {
		return Component{}, false
	}
	component := Component{Name: spec.name, Required: spec.Required, Readonly: spec.readonly}
	if !acquired {
		// Single-flight: a run started by an earlier caller is still going.
		// Starting another goroutine per caller is what leaked before, and
		// reporting that run as failed the moment someone else polls is wrong:
		// it may have just started. A run that is still inside its own budget is
		// fresh enough to share, so wait for it and report the same result. A
		// run that ignored its deadline is reported immediately and left alone,
		// so a stuck probe never piles up goroutines or makes later callers
		// wait for it again.
		if call.pastDeadline(spec.Timeout) {
			component.Result = Result{
				State:   StateFailed,
				ErrCode: CodeTimeout,
				Detail:  call.inFlightDetail(),
			}
			component.Latency = time.Since(call.startedAt)
			component.CheckedAt = time.Now()
			return component, true
		}
		wait := time.NewTimer(time.Until(call.startedAt.Add(spec.Timeout + budgetGrace)))
		defer wait.Stop()
		select {
		case <-call.done:
			component.Result = call.result
			component.Latency = call.latency
			component.CheckedAt = call.finishedAt
		case <-wait.C:
			// The shared run has now passed its deadline without returning.
			component.Result = Result{
				State:   StateFailed,
				ErrCode: CodeTimeout,
				Detail:  call.inFlightDetail(),
			}
			component.Latency = time.Since(call.startedAt)
			component.CheckedAt = time.Now()
		case <-ctx.Done():
			// The request went away while waiting; nothing will read the response.
			component.Result = Result{
				State:   StateFailed,
				ErrCode: CodeTimeout,
				Detail:  "check abandoned: " + ctx.Err().Error(),
			}
			component.Latency = time.Since(call.startedAt)
			component.CheckedAt = time.Now()
		}
		return component, true
	}
	startProbeCall(ctx, spec, call)
	timer := time.NewTimer(spec.Timeout + budgetGrace)
	defer timer.Stop()
	select {
	case <-call.done:
		component.Result = call.result
		component.Latency = call.latency
		component.CheckedAt = call.finishedAt
		r.release(spec.name, call)
	case <-timer.C:
		// The run ignored its context deadline. Its goroutine keeps going, but
		// the slot stays occupied, so later callers report it instead of piling
		// up more runs.
		component.Result = Result{
			State:   StateFailed,
			ErrCode: CodeTimeout,
			Detail:  call.inFlightDetail(),
		}
		component.Latency = time.Since(call.startedAt)
		component.CheckedAt = time.Now()
	case <-ctx.Done():
		// The request went away while waiting; nothing will read the response.
		component.Result = Result{
			State:   StateFailed,
			ErrCode: CodeTimeout,
			Detail:  "check abandoned: " + ctx.Err().Error(),
		}
		component.Latency = time.Since(call.startedAt)
		component.CheckedAt = time.Now()
	}
	return component, true
}

// startProbeCall launches the run of one acquired slot. The goroutine owns the
// call until it completes and never blocks on anything but the probe itself, so
// a probe that ignores its context leaves exactly one goroutine behind.
//
// The probe's context keeps the caller's values but drops the caller's
// cancellation: several callers may be waiting on this one run, so the first
// caller to go away must not cancel it for the others. The probe's own timeout
// still bounds it.
func startProbeCall(parent context.Context, spec Spec, call *probeCall) {
	go func() {
		probeCtx, cancel := context.WithTimeout(context.WithoutCancel(parent), spec.Timeout)
		defer cancel()
		start := time.Now()
		result := runProbeGuarded(probeCtx, spec.Probe)
		latency := time.Since(start)
		// A probe that returned after its own deadline was exceeded does not get
		// to report success: the deadline is the contract.
		if errors.Is(probeCtx.Err(), context.DeadlineExceeded) {
			result = Result{
				State:   StateFailed,
				ErrCode: CodeTimeout,
				Detail:  fmt.Sprintf("probe exceeded deadline %s", spec.Timeout),
			}
		}
		call.complete(normalizeResult(result), latency)
	}()
}

// runProbeGuarded calls the probe and converts a panic into a failed result.
func runProbeGuarded(ctx context.Context, p Probe) (result Result) {
	defer func() {
		if r := recover(); r != nil {
			result = Result{
				State:   StateFailed,
				ErrCode: CodePanic,
				Detail:  fmt.Sprintf("probe panicked: %v", r),
			}
		}
	}()
	return p.Probe(ctx)
}

// normalizeResult keeps the machine-readable contract stable: a ready result
// never carries an error code, and a non-ready result always carries one.
// States outside the four-value enumeration are reported as failed.
func normalizeResult(result Result) Result {
	switch result.State {
	case StateReady:
		result.ErrCode = ""
	case StateDegraded:
		if result.ErrCode == "" {
			result.ErrCode = CodeDegraded
		}
	case StateFailed:
		if result.ErrCode == "" {
			result.ErrCode = CodeFailed
		}
	case StateNotConfigured:
		if result.ErrCode == "" {
			result.ErrCode = CodeNotConfigured
		}
	default:
		unknown := string(result.State)
		result.State = StateFailed
		if result.ErrCode == "" {
			result.ErrCode = CodeFailed
		}
		if result.Detail == "" {
			result.Detail = fmt.Sprintf("unrecognized probe state %q", unknown)
		} else {
			result.Detail = fmt.Sprintf("unrecognized probe state %q: %s", unknown, result.Detail)
		}
	}
	return result
}

// Default is the process-wide probe registry behind the package-level helpers.
// The API layer reads it; the bootstrap writes it.
var Default = NewRegistry()

// Register validates and installs probe specs into Default.
func Register(specs ...Spec) error { return Default.Register(specs...) }

// Clear removes every probe from Default.
func Clear() { Default.Clear() }

// Registered returns the registered probe names of Default, sorted.
func Registered() []string { return Default.Registered() }

// Run executes every probe registered in Default.
func Run(ctx context.Context) map[string]Component { return Default.Run(ctx) }
