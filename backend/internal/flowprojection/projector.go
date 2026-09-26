// Package flowprojection replays the legacy Flow store's local outbox into the
// runtime database (T1.05.c, §19.3/§27.1).
//
// The legacy Flow database and the runtime database are two files, so a Flow
// write and its runtime timeline entry cannot share a transaction. The bridge
// the plan prescribes is a local outbox in the source database plus an
// asynchronous, idempotent projection:
//
//  1. SQLiteFlowStore.Put writes the Flow and one pending outbox row per newly
//     produced FlowEvent in one transaction (both or neither, see
//     TestLegacyFlowPutAndOutboxRollbackTogether).
//  2. This package claims the due rows, hands each one to a Target that writes
//     it into the runtime database, and then marks the row projected.
//
// The window between "Target committed" and "row marked projected" is a crash
// window, and it is not closed by pretending it does not exist: the design is
// at-least-once delivery plus a target that de-duplicates on
// LegacyOutboxEvent.SourceEventID. A round that dies there is replayed, the
// Target answers with the same runtime event id for the same source event, and
// the mark finally lands. Nothing is lost (the row is still pending) and nothing
// is duplicated in the target (its own idempotency key is the source event id).
//
// Two further limits are deliberate, not oversights:
//
//   - Exactly one projector instance may run against a given floweng.db. The
//     local outbox has no lease (the plan puts leases in T1.05.b's dispatcher,
//     which serves the runtime database), so two instances would both call the
//     Target for the same row. That stays correct because the Target is
//     idempotent, but it is wasted work and the wiring in T1.04 must start one
//     instance only.
//   - A Flow's events are projected in local order, and a Flow whose head is
//     failing blocks its own later events (retry with backoff, or dead letter
//     after the attempt budget) without blocking any other Flow. The runtime
//     timeline of one Flow must read in the order the Flow happened.
package flowprojection

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/codeflow/backend/internal/floweng"
)

// Source is the local outbox this projector drains. *floweng.SQLiteFlowStore
// implements it; tests wrap it to inject failures.
type Source interface {
	// DueLegacyEvents returns up to limit pending events that are due at now,
	// in local order and without earlier pending events of the same Flow behind
	// them.
	DueLegacyEvents(now time.Time, limit int) ([]floweng.LegacyOutboxEvent, error)
	// MarkLegacyEventProjected records the runtime event id for a source event.
	// It is a compare-and-set on state='pending' and treats "already projected
	// as this same id" as success, which is what makes a replay after a crash
	// idempotent.
	MarkLegacyEventProjected(sourceEventID, projectedEventID string, now time.Time) error
	// MarkLegacyEventRetry records a failed attempt and when the next one is due.
	MarkLegacyEventRetry(sourceEventID, lastError string, nextAttemptAt time.Time) error
	// MarkLegacyEventDead gives up on a pending event.
	MarkLegacyEventDead(sourceEventID, lastError string) error
}

// Target writes one legacy Flow event into the runtime database.
//
// Idempotency is part of the contract, not a hint: for one SourceEventID, any
// number of calls must produce at most one runtime event, and every call must
// return the id of that event. The first call usually creates it; a call after
// the projector crashed between the write and its mark finds it and returns the
// same id. A Target that cannot promise this must not be wired in — at-least-once
// delivery has no other net.
//
// An error means "try again later" unless it is wrapped in PermanentError.
// Implementations must not put credentials or whole response bodies into the
// error text: it is stored on the outbox row for an operator (truncated to 512
// bytes on a rune boundary).
type Target interface {
	Project(ctx context.Context, ev floweng.LegacyOutboxEvent) (eventID string, err error)
}

// PermanentError marks a Target failure that retrying cannot fix (an unknown
// project, a payload the runtime store refuses by contract, a closed target).
// The row is dead-lettered immediately instead of spending the attempt budget.
//
// It implements Error and Unwrap, so errors.Is/As reach the wrapped error.
type PermanentError struct {
	Err error
}

// Error makes PermanentError an error. A nil wrapped error is reported instead
// of dereferenced.
func (e *PermanentError) Error() string {
	if e == nil || e.Err == nil {
		return "flowprojection: permanent projection failure"
	}
	return "flowprojection: permanent projection failure: " + e.Err.Error()
}

// Unwrap returns the wrapped error so errors.Is/As can reach it.
func (e *PermanentError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Defaults for Options. A caller that leaves a field zero gets exactly these.
const (
	// DefaultBatchSize is how many outbox rows one round claims.
	DefaultBatchSize = 64
	// DefaultBackoffBase and DefaultBackoffMax bound the retry delay: after the
	// n-th failed attempt the row is due again at
	// now + min(BackoffBase * 2^(n-1), BackoffMax).
	DefaultBackoffBase = time.Second
	DefaultBackoffMax  = 5 * time.Minute
	// DefaultMaxAttempts is how many attempts an event may spend before it is
	// dead-lettered.
	DefaultMaxAttempts = 10
)

// Options configures a Projector. Source and Target are required; everything
// else has a working default.
type Options struct {
	Source Source
	Target Target
	// Clock is the time source for due times, marks and backoff. Default
	// time.Now; tests inject a movable clock so no test has to sleep.
	Clock func() time.Time
	// BatchSize is the maximum number of events claimed per round. Default 64.
	BatchSize int
	// BackoffBase and BackoffMax bound the retry delay. Defaults 1s and 5m.
	BackoffBase time.Duration
	BackoffMax  time.Duration
	// MaxAttempts is how many attempts an event may spend before it is
	// dead-lettered. Default 10.
	MaxAttempts int
}

// Report is what one ProjectOnce round did.
//
// Due counts the rows the claim returned, including the ones this round skipped
// because an earlier event of the same Flow had already failed: keeping one
// Flow in order is not a reason to hide that the round saw them. The other
// three are disjoint outcomes, so
// Due = Projected + Retried + DeadLettered + skipped.
type Report struct {
	Due          int
	Projected    int
	Retried      int
	DeadLettered int
}

// Projector drains the legacy Flow outbox into a Target.
type Projector struct {
	source      Source
	target      Target
	clock       func() time.Time
	batchSize   int
	backoffBase time.Duration
	backoffMax  time.Duration
	maxAttempts int
}

// projectorLog is where Run reports a failed round. Run is a background loop: a
// round that fails because the database is briefly locked must not stop it, but
// the failure must not disappear either. The rest of this backend logs with the
// standard library, so this does too.
var projectorLog = log.New(os.Stderr, "flowprojection: projector: ", log.LstdFlags|log.Lmsgprefix)

// New validates opts and returns a projector. A rejected configuration returns
// an error and no projector, so a caller cannot run with a half-applied option
// set (a nil Target would otherwise panic on the first event).
func New(opts Options) (*Projector, error) {
	if opts.Source == nil {
		return nil, errors.New("flowprojection: Source is required")
	}
	if opts.Target == nil {
		return nil, errors.New("flowprojection: Target is required")
	}
	if opts.BatchSize < 0 {
		return nil, fmt.Errorf("flowprojection: BatchSize %d is negative", opts.BatchSize)
	}
	if opts.BackoffBase < 0 {
		return nil, fmt.Errorf("flowprojection: BackoffBase %s is negative", opts.BackoffBase)
	}
	if opts.BackoffMax < 0 {
		return nil, fmt.Errorf("flowprojection: BackoffMax %s is negative", opts.BackoffMax)
	}
	if opts.MaxAttempts < 0 {
		return nil, fmt.Errorf("flowprojection: MaxAttempts %d is negative", opts.MaxAttempts)
	}

	p := &Projector{
		source:      opts.Source,
		target:      opts.Target,
		clock:       opts.Clock,
		batchSize:   opts.BatchSize,
		backoffBase: opts.BackoffBase,
		backoffMax:  opts.BackoffMax,
		maxAttempts: opts.MaxAttempts,
	}
	if p.clock == nil {
		p.clock = time.Now
	}
	if p.batchSize == 0 {
		p.batchSize = DefaultBatchSize
	}
	if p.backoffBase == 0 {
		p.backoffBase = DefaultBackoffBase
	}
	if p.backoffMax == 0 {
		p.backoffMax = DefaultBackoffMax
	}
	if p.maxAttempts == 0 {
		p.maxAttempts = DefaultMaxAttempts
	}
	if p.backoffMax < p.backoffBase {
		return nil, fmt.Errorf("flowprojection: BackoffMax %s is smaller than BackoffBase %s",
			p.backoffMax, p.backoffBase)
	}
	return p, nil
}

// ProjectOnce projects one batch of due events.
//
// Events are handled in the order the claim returned them, which is each Flow's
// local order. Once an event of a Flow fails this round — retried with backoff
// or dead-lettered — the rest of that Flow's events in this batch are skipped:
// the runtime timeline must not receive event 5 before event 4. Other Flows are
// unaffected. Subsequent rounds are ordered by the outbox itself: a Flow whose
// head is waiting for its backoff window has no due head, so the claim returns
// none of its events (wasDueLegacyEvents' per-Flow rule).
//
// Errors: a Target failure is never an error here — it is recorded on the row
// and counted in the report. An error means the Source itself refused something
// (a claim, a mark), or the context ended; whatever was already decided in the
// round stays decided, and the next round replays whatever was not.
func (p *Projector) ProjectOnce(ctx context.Context) (Report, error) {
	var report Report

	due, err := p.source.DueLegacyEvents(p.clock(), p.batchSize)
	if err != nil {
		return report, err
	}
	report.Due = len(due)

	// failedFlows remembers the Flows whose earlier event already failed in this
	// round, so their later events stay in the outbox instead of overtaking it.
	failedFlows := make(map[string]bool, len(due))

	for i := range due {
		// Re-read the clock every iteration: a Target call can take real time, so
		// "now" is not one instant for the whole round. It cannot make a row
		// arrive early — a row written during this round is only read by the
		// next claim — and the fresh value keeps a backoff deadline honest.
		now := p.clock()
		ev := due[i]
		if err := ctx.Err(); err != nil {
			// The round stops, but nothing about the rows already handled is
			// undone: a projected row stays projected (the Target has it), a
			// retried row stays pending with its backoff. The claim itself
			// modifies nothing, so an interrupted round costs a repeat, never a
			// gap.
			return report, err
		}
		if failedFlows[ev.FlowID] {
			continue
		}

		projectedID, projectErr := p.project(ctx, ev)
		if projectErr == nil && projectedID == "" {
			// The Target contract says a successful call returns the id of the
			// runtime event. An empty id would be unmappable back to anything
			// and MarkLegacyEventProjected refuses it, so treat it as a failed
			// attempt rather than writing a row nobody can interpret.
			projectErr = fmt.Errorf("target returned an empty event id for source event %s", ev.SourceEventID)
		}

		if projectErr == nil {
			if err := p.source.MarkLegacyEventProjected(ev.SourceEventID, projectedID, now); err != nil {
				// The Target may already hold the event; the mark is what did
				// not land. Reporting the error and returning keeps the row
				// pending, and the next round re-projects it — the Target's
				// idempotency turns that repeat into the same runtime event.
				return report, err
			}
			report.Projected++
			continue
		}

		failedFlows[ev.FlowID] = true
		// ev.Attempts is the attempt_count the claim read, i.e. the attempts
		// already made; this failure is attempt ev.Attempts+1.
		attempt := ev.Attempts + 1
		var permanent *PermanentError
		switch {
		case errors.As(projectErr, &permanent):
			if err := p.source.MarkLegacyEventDead(ev.SourceEventID, projectErr.Error()); err != nil {
				return report, err
			}
			report.DeadLettered++
		case attempt >= p.maxAttempts:
			if err := p.source.MarkLegacyEventDead(ev.SourceEventID, projectErr.Error()); err != nil {
				return report, err
			}
			report.DeadLettered++
		default:
			nextAttemptAt := now.Add(p.backoff(attempt))
			if err := p.source.MarkLegacyEventRetry(ev.SourceEventID, projectErr.Error(), nextAttemptAt); err != nil {
				return report, err
			}
			report.Retried++
		}
	}
	return report, nil
}

// Run projects immediately and then every interval until ctx is done.
//
// A round that fails (a locked database, a closed store) is logged and the loop
// continues — the next round is what recovers it, and stopping would silently
// stop the projection until someone restarted the process. Target panics never
// reach here at all (project recovers them into an ordinary failed attempt), so
// no event can take the loop down.
//
// The one thing that stops the loop is ctx: Run then returns ctx.Err().
func (p *Projector) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		return fmt.Errorf("flowprojection: run interval %s must be positive", interval)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		report, err := p.ProjectOnce(ctx)
		switch {
		case err != nil && ctx.Err() != nil:
			return ctx.Err()
		case err != nil:
			projectorLog.Printf("projection round failed, continuing: %v", err)
		case report.Due > 0:
			projectorLog.Printf("projection round: due=%d projected=%d retried=%d dead_lettered=%d",
				report.Due, report.Projected, report.Retried, report.DeadLettered)
		}

		// The claim hands out one event per Flow (the head of its queue), so a
		// round that projected something may have exposed the next event of
		// every Flow it served. Run the next round at once instead of waiting a
		// full interval per event: a burst of N events on one Flow would
		// otherwise lag N intervals behind. The drain ends by itself when a round
		// projects nothing (queue empty, heads backing off or dead-lettered).
		if err == nil && report.Projected > 0 {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			continue
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// project calls the Target once, turning a panic into an ordinary error.
//
// A panic in a Target is a bug in the Target, not a reason to lose the event and
// not a reason to take down the loop: it is recorded as one failed attempt and
// spends one unit of the attempt budget, so a Target that panics on every call
// dead-letters the row after MaxAttempts instead of looping forever. Only the
// panic value's type is recorded, never its content (it can be anything the
// Target happened to hold, including a payload).
func (p *Projector) project(ctx context.Context, ev floweng.LegacyOutboxEvent) (id string, err error) {
	defer func() {
		if r := recover(); r != nil {
			id = ""
			err = fmt.Errorf("target panicked while projecting source event %s: %T", ev.SourceEventID, r)
		}
	}()
	return p.target.Project(ctx, ev)
}

// backoff returns how long to wait after the attempt whose ordinal is attempt
// (1 = the first attempt): min(BackoffBase * 2^(attempt-1), BackoffMax), with
// the doubling capped so a large attempt count cannot overflow the duration.
func (p *Projector) backoff(attempt int) time.Duration {
	delay := p.backoffBase
	for i := 1; i < attempt; i++ {
		if delay >= p.backoffMax/2 {
			return p.backoffMax
		}
		delay *= 2
	}
	if delay > p.backoffMax {
		return p.backoffMax
	}
	return delay
}
