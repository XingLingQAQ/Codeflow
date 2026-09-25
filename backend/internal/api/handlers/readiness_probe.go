package handlers

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// ProbeState is the machine-readable readiness state of one dependency
// (plan section 15 T0.12): ready, degraded, failed, or not_configured.
type ProbeState string

const (
	ProbeStateReady         ProbeState = "ready"
	ProbeStateDegraded      ProbeState = "degraded"
	ProbeStateFailed        ProbeState = "failed"
	ProbeStateNotConfigured ProbeState = "not_configured"
)

// Machine-readable probe error codes (stable for consumers; plan section 15
// T0.12 remediation codes). Component probes may return their own specific
// codes; the runner reserves the five below for the outcomes it determines
// itself and substitutes them whenever a probe reports a non-ready state
// without a code, so every non-ready component carries a stable error_code.
const (
	// ProbeErrTimeout marks a probe that exceeded its context deadline, or a
	// probe whose previous run is still in flight (single-flight, T0.12.a).
	// The component is reported not ready; /ready itself still answers fast.
	ProbeErrTimeout = "probe_timeout"
	// ProbeErrPanic marks a probe whose call panicked; the runner recovers.
	ProbeErrPanic = "probe_panic"
	// ProbeErrFailed is the fallback code for ProbeStateFailed.
	ProbeErrFailed = "probe_failed"
	// ProbeErrDegraded is the fallback code for ProbeStateDegraded.
	ProbeErrDegraded = "probe_degraded"
	// ProbeErrNotConfigured is the fallback code for ProbeStateNotConfigured.
	ProbeErrNotConfigured = "probe_not_configured"
)

// ProbeResult is the outcome of a single probe run. ErrCode is the
// machine-readable code of the most recent failure; it is empty when State is
// ProbeStateReady. Detail is optional human-readable context.
type ProbeResult struct {
	State   ProbeState
	ErrCode string
	Detail  string
}

// Probe is a readiness check for one dependency. Probes must be read-only
// (Readonly reports true and the check has no side effects on production
// state: it may observe process globals, open private in-memory resources, or
// stat paths, but never write) and must honor ctx cancellation. The runner
// enforces the deadline even against misbehaving probes.
type Probe interface {
	Name() string
	Readonly() bool
	Probe(ctx context.Context) ProbeResult
}

// probeFunc adapts a plain function into a read-only Probe.
type probeFunc struct {
	name string
	fn   func(context.Context) ProbeResult
}

func (f probeFunc) Name() string                          { return f.name }
func (f probeFunc) Readonly() bool                        { return true }
func (f probeFunc) Probe(ctx context.Context) ProbeResult { return f.fn(ctx) }

// NewProbeFunc returns a read-only probe with the given name backed by fn.
func NewProbeFunc(name string, fn func(context.Context) ProbeResult) Probe {
	return probeFunc{name: name, fn: fn}
}

// NewNotConfiguredProbe returns a probe that always reports
// ProbeStateNotConfigured. It exists so dependencies that are planned but not
// wired yet (vault, event_store, exec_backend:<name>, ...) are visible in
// /ready as not_configured instead of pretending to be ready. The runner fills
// in its error_code (probe_not_configured).
func NewNotConfiguredProbe(name, detail string) Probe {
	return probeFunc{name: name, fn: func(context.Context) ProbeResult {
		return ProbeResult{State: ProbeStateNotConfigured, Detail: detail}
	}}
}

// DefaultProbeTimeout bounds a single probe when ReadinessProbeSpec.Timeout
// is not set.
const DefaultProbeTimeout = 2 * time.Second

// probeBudgetGrace is added to a probe's own deadline to form the wall-clock
// bound on how long one /ready call waits for that probe's run to deliver its
// completion signal. A probe that ignores its context is reported as
// probe_timeout at this point; its run stays in flight, and later polls report
// that slot without starting a second run (single-flight, T0.12.a).
const probeBudgetGrace = 500 * time.Millisecond

// ReadinessProbeSpec bundles a probe with its gating metadata.
type ReadinessProbeSpec struct {
	Probe Probe
	// Required probes gate the overall readiness verdict: a required probe
	// that is not ProbeStateReady makes /ready answer not_ready (503).
	Required bool
	// Timeout bounds the probe's context; <= 0 selects DefaultProbeTimeout.
	Timeout time.Duration

	// name and readonly are captured once, at registration, so that serving
	// /ready never calls back into probe code for metadata.
	name     string
	readonly bool
}

// probeCall is one in-flight run of a registered probe. The runner goroutine
// writes result/latency/finishedAt and then closes done; every reader may touch
// those fields only after observing done closed.
type probeCall struct {
	startedAt  time.Time
	done       chan struct{}
	result     ProbeResult
	latency    time.Duration
	finishedAt time.Time
}

func newProbeCall() *probeCall {
	return &probeCall{startedAt: time.Now(), done: make(chan struct{})}
}

func (c *probeCall) complete(result ProbeResult, latency time.Duration) {
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

// inFlightDetail is the detail shared by every path that reports a slot whose
// run has not returned yet.
func (c *probeCall) inFlightDetail() string {
	return "still running since " + c.startedAt.UTC().Format(time.RFC3339)
}

type readinessProbeEntry struct {
	spec ReadinessProbeSpec
	// call is non-nil exactly while a run of spec is in flight. Registering a
	// replacement or ClearReadinessProbes drops the reference, so a late result
	// from a replaced probe can never be written into the replacement's state.
	call *probeCall
}

type readinessProbeRegistry struct {
	mu      sync.RWMutex
	entries map[string]*readinessProbeEntry
}

var readinessProbes = &readinessProbeRegistry{entries: map[string]*readinessProbeEntry{}}

// RegisterReadinessProbes validates and installs probe specs atomically:
// either all specs are accepted or none are. Re-registering a name replaces the
// previous spec along with any in-flight run, so repeated bootstrap Apply calls
// are idempotent. A probe that does not declare itself read-only is rejected.
func RegisterReadinessProbes(specs ...ReadinessProbeSpec) error {
	normalized := make([]ReadinessProbeSpec, 0, len(specs))
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
			spec.Timeout = DefaultProbeTimeout
		}
		spec.name = name
		spec.readonly = true
		normalized = append(normalized, spec)
	}
	// Every spec was validated above, so installation cannot fail: the batch is
	// installed all-or-nothing under one lock.
	readinessProbes.mu.Lock()
	defer readinessProbes.mu.Unlock()
	for _, spec := range normalized {
		readinessProbes.entries[spec.name] = &readinessProbeEntry{spec: spec}
	}
	return nil
}

// ClearReadinessProbes removes every registered probe. It is the bootstrap
// Reset counterpart of RegisterReadinessProbes; tests use it for isolation.
func ClearReadinessProbes() {
	readinessProbes.mu.Lock()
	defer readinessProbes.mu.Unlock()
	readinessProbes.entries = map[string]*readinessProbeEntry{}
}

// RegisteredReadinessProbes returns the registered probe names, sorted. It is
// intended for diagnostics and tests.
func RegisteredReadinessProbes() []string {
	readinessProbes.mu.RLock()
	defer readinessProbes.mu.RUnlock()
	names := make([]string, 0, len(readinessProbes.entries))
	for name := range readinessProbes.entries {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func snapshotReadinessProbes() []ReadinessProbeSpec {
	readinessProbes.mu.RLock()
	defer readinessProbes.mu.RUnlock()
	out := make([]ReadinessProbeSpec, 0, len(readinessProbes.entries))
	for _, entry := range readinessProbes.entries {
		out = append(out, entry.spec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// acquire reserves the single-flight slot of one probe. It returns the call to
// wait on and true when this caller started the run, or the still-running call
// and false when another /ready already owns it. A slot whose run finished
// after its own /ready gave up is discarded and re-run, so results are fresh.
// A nil call means the probe is no longer registered.
func (r *readinessProbeRegistry) acquire(name string) (*probeCall, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.entries[name]
	if !ok {
		return nil, false
	}
	if entry.call == nil || entry.call.completed() {
		call := newProbeCall()
		entry.call = call
		return call, true
	}
	return entry.call, false
}

// release frees the slot after the caller consumed call's result. A slot that a
// replacement or ClearReadinessProbes already moved on from is left alone.
func (r *readinessProbeRegistry) release(name string, call *probeCall) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if entry, ok := r.entries[name]; ok && entry.call == call {
		entry.call = nil
	}
}

// probedComponent is one probe run's outcome plus the metadata the handler
// needs to merge it into the readiness components.
type probedComponent struct {
	spec      ReadinessProbeSpec
	result    ProbeResult
	latency   time.Duration
	checkedAt time.Time
}

// runReadinessProbes executes every registered probe and returns one component
// per probe, keyed by probe name. Probes run concurrently, each under its own
// context deadline, and the call is bounded by the slowest probe's deadline
// plus probeBudgetGrace. Every probe has at most one run in flight: a poll that
// arrives while a run is still going reports that slot as probe_timeout instead
// of starting another goroutine (single-flight). Returns nil when no probes are
// registered, which keeps the legacy Has*-only response shape byte for byte.
func runReadinessProbes(ctx context.Context) map[string]probedComponent {
	specs := snapshotReadinessProbes()
	if len(specs) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	out := make(map[string]probedComponent, len(specs))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, spec := range specs {
		wg.Add(1)
		go func(spec ReadinessProbeSpec) {
			defer wg.Done()
			pc, report := runOneReadinessProbe(ctx, spec)
			if !report {
				return
			}
			mu.Lock()
			out[pc.spec.name] = pc
			mu.Unlock()
		}(spec)
	}
	wg.Wait()
	return out
}

// runOneReadinessProbe performs, or observes, the single run of one probe. The
// second result is false when the probe was unregistered while the check was in
// flight, in which case the response omits it.
func runOneReadinessProbe(ctx context.Context, spec ReadinessProbeSpec) (probedComponent, bool) {
	call, acquired := readinessProbes.acquire(spec.name)
	if call == nil {
		return probedComponent{}, false
	}
	pc := probedComponent{spec: spec}
	if !acquired {
		// Single-flight: a run started by an earlier poll is still going.
		// Starting another goroutine per poll is what leaked before; report the
		// slot instead and let the running call finish on its own.
		pc.result = ProbeResult{
			State:   ProbeStateFailed,
			ErrCode: ProbeErrTimeout,
			Detail:  call.inFlightDetail(),
		}
		pc.latency = time.Since(call.startedAt)
		pc.checkedAt = time.Now()
		return pc, true
	}
	startProbeCall(ctx, spec, call)
	timer := time.NewTimer(spec.Timeout + probeBudgetGrace)
	defer timer.Stop()
	select {
	case <-call.done:
		pc.result = call.result
		pc.latency = call.latency
		pc.checkedAt = call.finishedAt
		readinessProbes.release(spec.name, call)
	case <-timer.C:
		// The run ignored its context deadline. Its goroutine keeps going, but
		// the slot stays occupied, so later polls report it instead of piling up
		// more runs.
		pc.result = ProbeResult{
			State:   ProbeStateFailed,
			ErrCode: ProbeErrTimeout,
			Detail:  call.inFlightDetail(),
		}
		pc.latency = time.Since(call.startedAt)
		pc.checkedAt = time.Now()
	case <-ctx.Done():
		// The request went away while waiting; nothing will read the response.
		pc.result = ProbeResult{
			State:   ProbeStateFailed,
			ErrCode: ProbeErrTimeout,
			Detail:  "check abandoned: " + ctx.Err().Error(),
		}
		pc.latency = time.Since(call.startedAt)
		pc.checkedAt = time.Now()
	}
	return pc, true
}

// startProbeCall launches the run of one acquired slot. The goroutine owns the
// call until it completes and never blocks on anything but the probe itself, so
// a probe that ignores its context leaves exactly one goroutine behind.
func startProbeCall(parent context.Context, spec ReadinessProbeSpec, call *probeCall) {
	go func() {
		probeCtx, cancel := context.WithTimeout(parent, spec.Timeout)
		defer cancel()
		start := time.Now()
		result := runProbeGuarded(probeCtx, spec.Probe)
		latency := time.Since(start)
		// A probe that returned after its own deadline was exceeded does not get
		// to report success: the deadline is the contract.
		if errors.Is(probeCtx.Err(), context.DeadlineExceeded) {
			result = ProbeResult{
				State:   ProbeStateFailed,
				ErrCode: ProbeErrTimeout,
				Detail:  fmt.Sprintf("probe exceeded deadline %s", spec.Timeout),
			}
		}
		call.complete(normalizeProbeResult(result), latency)
	}()
}

// runProbeGuarded calls the probe and converts a panic into a failed result.
func runProbeGuarded(ctx context.Context, p Probe) (result ProbeResult) {
	defer func() {
		if r := recover(); r != nil {
			result = ProbeResult{
				State:   ProbeStateFailed,
				ErrCode: ProbeErrPanic,
				Detail:  fmt.Sprintf("probe panicked: %v", r),
			}
		}
	}()
	return p.Probe(ctx)
}

// normalizeProbeResult keeps the machine-readable contract stable: a ready
// result never carries an error code, and a non-ready result always carries
// one. States outside the four-value enumeration are reported as failed.
func normalizeProbeResult(result ProbeResult) ProbeResult {
	switch result.State {
	case ProbeStateReady:
		result.ErrCode = ""
	case ProbeStateDegraded:
		if result.ErrCode == "" {
			result.ErrCode = ProbeErrDegraded
		}
	case ProbeStateFailed:
		if result.ErrCode == "" {
			result.ErrCode = ProbeErrFailed
		}
	case ProbeStateNotConfigured:
		if result.ErrCode == "" {
			result.ErrCode = ProbeErrNotConfigured
		}
	default:
		unknown := string(result.State)
		result.State = ProbeStateFailed
		if result.ErrCode == "" {
			result.ErrCode = ProbeErrFailed
		}
		if result.Detail == "" {
			result.Detail = fmt.Sprintf("unrecognized probe state %q", unknown)
		} else {
			result.Detail = fmt.Sprintf("unrecognized probe state %q: %s", unknown, result.Detail)
		}
	}
	return result
}
