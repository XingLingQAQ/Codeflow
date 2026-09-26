// Outbox dispatcher: lease, fence, backoff, dead letter (T1.05.b).
//
// This file is the other half of events.go. AppendEventTx records "this event
// must be delivered to these destinations" inside the caller's transaction and
// commits; this file delivers it afterwards, outside any transaction, at least
// once. The split is the whole point of §19.3: the side effect (a WebSocket
// frame, a desktop notification, an audit export) happens after the commit, and
// a delivery that fails must never be able to un-commit the fact it carries.
//
// The rules this file exists to enforce:
//
//   - At least once, never exactly once by accident. A row is leased before it
//     is delivered and the lease is what keeps two dispatchers from working on
//     it at the same time, but a lease can expire while its owner is still
//     alive. The dispatcher therefore assumes it may deliver the same event
//     twice, and hands the downstream an IdempotencyKey (event_id + ":" +
//     destination) so that a duplicate is absorbed rather than acted on. The
//     attempt budget is spent at claim time, not at write-back time, so a
//     dispatcher that dies mid-delivery still burns an attempt and a crash loop
//     cannot retry forever.
//   - Every write-back is a compare-and-set on the lease it was granted
//     (state='pending' AND lease_owner AND lease_epoch). A dispatcher whose
//     lease was taken over matches no row and counts LostLease instead of
//     overwriting the new owner's result. This is §19.1's fence, applied to
//     outbox rows.
//   - A failure that cannot be fixed by retrying goes straight to dead_letter,
//     and dead_letter rows are retained (003's trigger). An abandoned
//     notification is a fact an operator must be able to find and replay.
//   - Time crosses this boundary exactly once and from the injected Clock, so
//     backoff and lease expiry are testable without sleeping.
//
// The dispatcher does not know what a destination means. It only claims rows
// whose destination it was registered to serve, so a second dispatcher (an
// audit exporter, say) can serve other destinations from the same table without
// the two fighting over rows.

package runstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Delivery is one claimed delivery, handed to a Deliverer outside any
// transaction.
//
// It carries the whole Event because the deliverer has to build the wire
// envelope and must not need a second read to do it; the row it came from is
// already leased, so the event cannot change underneath it (events are
// immutable).
type Delivery struct {
	OutboxID    string
	EventID     string
	Destination string
	// Attempt is the ordinal of the attempt that is starting: 1 for the first
	// try, 2 for the first retry. It is the outbox row's attempt_count after
	// the claim incremented it, which is why an attempt that crashed before it
	// could write back still counts.
	Attempt int
	// Event is the stored event row, exactly as GetEvent returns it.
	Event Event
	// IdempotencyKey identifies the delivery, not the attempt: a duplicate
	// delivery carries the same key. Downstream consumers de-duplicate on it
	// (or on Event.ID alone, which is the same identity for a smaller
	// vocabulary), which is what makes at-least-once delivery safe.
	IdempotencyKey string
}

// Deliverer performs one delivery. It is called outside any database
// transaction and with the dispatcher's context, so a cancelled dispatch stops
// the delivery rather than leaving it running against a closed store.
//
// Returning nil means "the destination has it". Returning any other error means
// "retry later" unless the error wraps PermanentError. The error text is stored
// on the row for an operator (truncated), so a Deliverer must not put a
// credential, a token or a request body into it.
type Deliverer interface {
	Deliver(ctx context.Context, d Delivery) error
}

// PermanentError marks a delivery failure that retrying cannot fix: an
// unrecoverable destination, a payload the destination rejects, a route that
// does not exist. The dispatcher dead-letters the row immediately instead of
// spending the attempt budget on it.
//
// It implements Error and Unwrap, so errors.Is/As against the wrapped error
// keep working for the deliverer's own tests.
type PermanentError struct {
	Err error
}

// Error makes PermanentError an error. A nil wrapped error is reported rather
// than dereferenced, because a deliverer that wants "permanent, no detail"
// should still get a usable message.
func (e *PermanentError) Error() string {
	if e == nil || e.Err == nil {
		return "runstore: permanent delivery failure"
	}
	return "runstore: permanent delivery failure: " + e.Err.Error()
}

// Unwrap returns the wrapped error so errors.Is/As can reach it.
func (e *PermanentError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// DispatcherOptions configures a Dispatcher. Every field has a working default
// except Owner and Destinations, which have no sensible one.
type DispatcherOptions struct {
	// Owner identifies this dispatcher instance for the lease. It must be
	// non-blank. Two instances that share an owner string are one logical
	// worker (a restarted process keeping its name, or two overlapping rounds
	// of the same process); the lease epoch still fences them apart, which is
	// why the epoch is part of the write-back CAS and not just the owner.
	Owner string
	// Destinations maps a destination name to the Deliverer that serves it.
	// Only rows whose destination appears here are claimed; rows for other
	// destinations are left untouched for whoever serves them.
	Destinations map[string]Deliverer
	// LeaseDuration is how long a claim is valid. It must be comfortably
	// longer than a delivery: a lease that expires mid-delivery means the work
	// is done twice (allowed) and the first write-back is fenced off (correct,
	// but wasted). Default 30s.
	LeaseDuration time.Duration
	// BatchSize is the maximum number of rows claimed per round. Default 32.
	BatchSize int
	// BackoffBase and BackoffMax bound the retry delay: after the n-th failed
	// attempt the row is due again at now + min(BackoffBase * 2^(n-1),
	// BackoffMax). Defaults 1s and 5m.
	BackoffBase time.Duration
	BackoffMax  time.Duration
	// MaxAttempts is how many attempts a delivery may spend before it is
	// dead-lettered. Default 10. Attempts are counted when they start, so a
	// dispatcher that crashes mid-delivery still consumes one.
	MaxAttempts int
	// Clock is the time source for lease expiry, backoff and delivered_at.
	// Default time.Now; tests inject a movable clock so no test has to sleep
	// for a backoff.
	Clock func() time.Time
}

// Dispatcher claims due deliveries and hands them to their Deliverers.
//
// It is safe for concurrent use: every state change is a single transaction
// (Store.WithTx is BEGIN IMMEDIATE), and the write-back is fenced, so two
// dispatchers racing on the same row produce one delivery and one LostLease
// rather than two results.
type Dispatcher struct {
	store *Store
	owner string
	// destinations is the registered set, and destinationNames its sorted keys
	// for the claim statement's IN list. Sorted so the SQL and its arguments
	// are stable, which makes a failure reproducible.
	destinations     map[string]Deliverer
	destinationNames []string
	leaseDuration    time.Duration
	batchSize        int
	backoffBase      time.Duration
	backoffMax       time.Duration
	maxAttempts      int
	clock            func() time.Time
}

// Defaults for DispatcherOptions. They are exported behaviour even though the
// constants are not: a caller that leaves a field zero gets exactly these.
const (
	DefaultLeaseDuration = 30 * time.Second
	DefaultDispatchBatch = 32
	DefaultBackoffBase   = time.Second
	DefaultBackoffMax    = 5 * time.Minute
	DefaultMaxAttempts   = 10
)

// dispatcherLog is where Run reports a failed round. Run is a background loop:
// a round that fails because the database is briefly locked must not stop it,
// but the failure must not disappear either. The rest of this backend logs with
// the standard library, so this does too.
var dispatcherLog = log.New(os.Stderr, "runstore: dispatcher: ", log.LstdFlags|log.Lmsgprefix)

// NewDispatcher validates opts and returns a dispatcher over store. A rejected
// configuration returns ErrInvalidDispatcherConfig and no dispatcher, so a
// caller cannot accidentally run with a half-applied option set.
func NewDispatcher(store *Store, opts DispatcherOptions) (*Dispatcher, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: nil store", ErrInvalidDispatcherConfig)
	}
	if strings.TrimSpace(opts.Owner) == "" {
		return nil, fmt.Errorf("%w: Owner is blank (it is the lease owner, so it must identify this instance)", ErrInvalidDispatcherConfig)
	}
	if len(opts.Destinations) == 0 {
		// A dispatcher that serves nothing would silently never deliver, which
		// is exactly the kind of quiet gap §27.4 forbids. Fail at construction.
		return nil, fmt.Errorf("%w: no destinations registered", ErrInvalidDispatcherConfig)
	}
	for name, deliverer := range opts.Destinations {
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("%w: a destination name is blank", ErrInvalidDispatcherConfig)
		}
		if deliverer == nil {
			return nil, fmt.Errorf("%w: destination %q has a nil Deliverer", ErrInvalidDispatcherConfig, name)
		}
	}
	if opts.LeaseDuration < 0 {
		return nil, fmt.Errorf("%w: LeaseDuration %s is negative", ErrInvalidDispatcherConfig, opts.LeaseDuration)
	}
	if opts.BatchSize < 0 {
		return nil, fmt.Errorf("%w: BatchSize %d is negative", ErrInvalidDispatcherConfig, opts.BatchSize)
	}
	if opts.BackoffBase < 0 {
		return nil, fmt.Errorf("%w: BackoffBase %s is negative", ErrInvalidDispatcherConfig, opts.BackoffBase)
	}
	if opts.BackoffMax < 0 {
		return nil, fmt.Errorf("%w: BackoffMax %s is negative", ErrInvalidDispatcherConfig, opts.BackoffMax)
	}
	if opts.MaxAttempts < 0 {
		return nil, fmt.Errorf("%w: MaxAttempts %d is negative", ErrInvalidDispatcherConfig, opts.MaxAttempts)
	}

	d := &Dispatcher{
		store:         store,
		owner:         opts.Owner,
		destinations:  make(map[string]Deliverer, len(opts.Destinations)),
		leaseDuration: opts.LeaseDuration,
		batchSize:     opts.BatchSize,
		backoffBase:   opts.BackoffBase,
		backoffMax:    opts.BackoffMax,
		maxAttempts:   opts.MaxAttempts,
		clock:         opts.Clock,
	}
	for name, deliverer := range opts.Destinations {
		d.destinations[name] = deliverer
		d.destinationNames = append(d.destinationNames, name)
	}
	sort.Strings(d.destinationNames)

	if d.leaseDuration == 0 {
		d.leaseDuration = DefaultLeaseDuration
	}
	if d.batchSize == 0 {
		d.batchSize = DefaultDispatchBatch
	}
	if d.backoffBase == 0 {
		d.backoffBase = DefaultBackoffBase
	}
	if d.backoffMax == 0 {
		d.backoffMax = DefaultBackoffMax
	}
	if d.maxAttempts == 0 {
		d.maxAttempts = DefaultMaxAttempts
	}
	if d.clock == nil {
		d.clock = time.Now
	}
	if d.backoffMax < d.backoffBase {
		return nil, fmt.Errorf("%w: BackoffMax %s is smaller than BackoffBase %s",
			ErrInvalidDispatcherConfig, d.backoffMax, d.backoffBase)
	}
	return d, nil
}

// DispatchReport is what one DispatchOnce round did. The counters are disjoint
// outcomes of the rows it touched:
//
//   - Claimed: rows this round leased (attempt_count incremented), including
//     the ones dead-lettered at claim time because their budget was already
//     spent.
//   - Delivered / Retried / DeadLettered: rows whose write-back landed.
//   - LostLease: rows whose write-back matched no row, because the lease had
//     been taken over (or the row was trimmed) while the delivery was running.
//     The writer must not record an outcome it no longer owns, so this is a
//     count, not an error.
type DispatchReport struct {
	Claimed      int
	Delivered    int
	Retried      int
	DeadLettered int
	LostLease    int
}

// DispatchOnce claims one batch of due deliveries, delivers each of them
// outside any transaction, and writes the outcome back under the lease fence.
//
// Errors: a delivery failure is never an error here — it is recorded on the row
// (retry with backoff, or dead_letter) and counted in the report. An error
// means the database itself refused something (a claim or a write-back), and
// the report carries whatever happened before that point.
func (d *Dispatcher) DispatchOnce(ctx context.Context) (DispatchReport, error) {
	var report DispatchReport
	now := d.clock()

	claims, err := d.claim(ctx, now, &report)
	if err != nil {
		return report, err
	}
	for _, c := range claims {
		if err := ctx.Err(); err != nil {
			// A cancelled dispatch stops here. The claims already taken keep
			// their leases (and their spent attempts) and are recovered by
			// whoever claims them after the lease expires — at-least-once
			// delivery does not depend on this process finishing its round.
			return report, err
		}
		if err := d.complete(ctx, c, d.deliver(ctx, c), &report); err != nil {
			return report, err
		}
	}
	return report, nil
}

// Run dispatches immediately and then every interval until ctx is done.
//
// A round that fails (a locked database, a closed store) is logged and the loop
// continues: the next round is what recovers it, and stopping would silently
// stop deliveries until someone restarted the process. The one thing that stops
// the loop is ctx: Run then returns ctx.Err().
func (d *Dispatcher) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		return fmt.Errorf("%w: run interval %s must be positive", ErrInvalidDispatcherConfig, interval)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		report, err := d.DispatchOnce(ctx)
		switch {
		case err != nil && ctx.Err() != nil:
			return ctx.Err()
		case err != nil:
			dispatcherLog.Printf("dispatch round failed, continuing: %v", err)
		case report.Claimed > 0 || report.DeadLettered > 0 || report.LostLease > 0:
			dispatcherLog.Printf("dispatch round: claimed=%d delivered=%d retried=%d dead_lettered=%d lost_lease=%d",
				report.Claimed, report.Delivered, report.Retried, report.DeadLettered, report.LostLease)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// claimedDelivery is one row this dispatcher holds a lease on, plus the epoch
// that lease was granted under. The epoch is what the write-back fences on.
type claimedDelivery struct {
	outboxID       string
	eventID        string
	destination    string
	attempt        int
	epoch          int64
	event          Event
	idempotencyKey string
}

// errAttemptsExhausted is written to last_error when a claim finds that the
// row's attempt budget was already spent by attempts that never reported back
// (the dispatcher died mid-delivery). Without that text an operator would see a
// dead letter with no explanation.
const errAttemptsExhausted = "attempts exhausted without a completion"

// maxLastErrorBytes bounds what one failure can put on a row.
//
// The text is for an operator reading the outbox; it is not a log. A
// destination that answers with a whole HTML error page must not be able to
// grow the row without bound, and the deliverer is told (in Deliverer's doc) not
// to include credentials. The cut is rune-safe so the stored text stays valid
// UTF-8.
const maxLastErrorBytes = 512

// truncateLastError cuts msg to at most maxLastErrorBytes bytes on a rune
// boundary.
func truncateLastError(msg string) string {
	if len(msg) <= maxLastErrorBytes {
		return msg
	}
	cut := maxLastErrorBytes
	for cut > 0 && !utf8.RuneStart(msg[cut]) {
		cut--
	}
	return msg[:cut]
}

// claim leases up to BatchSize due rows and returns them with their events.
//
// Everything happens in one write transaction, so a batch is claimed all at
// once or not at all, and the rows a dispatcher has claimed are visible to
// every other dispatcher the moment the transaction commits.
//
// The SELECT and the UPDATE are separate on purpose. The SELECT picks the
// candidates (pending, due, unleased, served here, oldest first); the UPDATE
// takes each one with a CAS on "still pending and still unleased now", because
// between the two another dispatcher could have committed a claim on the same
// row. Under BEGIN IMMEDIATE the two cannot actually interleave — the write
// lock is held from BEGIN — but the CAS is what makes correctness independent
// of that, exactly as nextScopeSeq's upsert is.
func (d *Dispatcher) claim(ctx context.Context, now time.Time, report *DispatchReport) ([]claimedDelivery, error) {
	nowMS := unixMilli(now)
	leaseUntil := unixMilli(now.Add(d.leaseDuration))

	selectSQL := `SELECT id, event_id, destination, attempt_count
	               FROM outbox
	               WHERE state = 'pending'
	                 AND next_attempt_at IS NOT NULL AND next_attempt_at <= ?
	                 AND (lease_until IS NULL OR lease_until <= ?)
	                 AND destination IN (` + d.destinationPlaceholders() + `)
	               ORDER BY next_attempt_at, id
	               LIMIT ?`
	selectArgs := make([]any, 0, 3+len(d.destinationNames))
	selectArgs = append(selectArgs, nowMS, nowMS)
	for _, name := range d.destinationNames {
		selectArgs = append(selectArgs, name)
	}
	selectArgs = append(selectArgs, d.batchSize)

	const claimSQL = `UPDATE outbox
	                   SET lease_owner = ?, lease_until = ?, lease_epoch = lease_epoch + 1,
	                       attempt_count = attempt_count + 1
	                   WHERE id = ? AND state = 'pending' AND (lease_until IS NULL OR lease_until <= ?)
	                   RETURNING lease_epoch, attempt_count`

	const exhaustSQL = `UPDATE outbox
	                     SET state = 'dead_letter', next_attempt_at = NULL,
	                         lease_owner = NULL, lease_until = NULL, last_error = ?
	                     WHERE id = ? AND state = 'pending' AND lease_owner = ? AND lease_epoch = ?`

	var claims []claimedDelivery
	err := d.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		claims = nil

		type candidate struct {
			id          string
			eventID     string
			destination string
			attempts    int64
		}
		var candidates []candidate

		rows, err := tx.QueryContext(ctx, selectSQL, selectArgs...)
		if err != nil {
			return fmt.Errorf("select due deliveries: %w", err)
		}
		for rows.Next() {
			var c candidate
			if err := rows.Scan(&c.id, &c.eventID, &c.destination, &c.attempts); err != nil {
				rows.Close()
				return fmt.Errorf("scan due delivery: %w", err)
			}
			candidates = append(candidates, c)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("iterate due deliveries: %w", err)
		}
		// Close before the UPDATE below: the same transaction is about to
		// issue new statements, and an open cursor would keep the driver's
		// statement busy.
		rows.Close()

		for _, c := range candidates {
			var (
				epoch    int64
				attempts int64
			)
			err := tx.QueryRowContext(ctx, claimSQL, d.owner, leaseUntil, c.id, nowMS).Scan(&epoch, &attempts)
			if errors.Is(err, sql.ErrNoRows) {
				// Someone else got there first (or the row moved on). Not an
				// error: it is not ours to deliver.
				continue
			}
			if err != nil {
				return fmt.Errorf("claim delivery %s: %w", c.id, err)
			}
			report.Claimed++

			// The attempt counter was incremented by the claim, so attempts is
			// the ordinal of the attempt that is starting. If that ordinal is
			// past the budget, the previous attempts never reported back (a
			// crash loop) and there is no point in delivering again: the row is
			// dead-lettered here, while the claim is fresh, so the exhaustion
			// is recorded rather than retried forever.
			if d.maxAttempts > 0 && attempts > int64(d.maxAttempts) {
				if _, err := tx.ExecContext(ctx, exhaustSQL, errAttemptsExhausted, c.id, d.owner, epoch); err != nil {
					return fmt.Errorf("dead-letter exhausted delivery %s: %w", c.id, err)
				}
				report.DeadLettered++
				continue
			}

			event, err := GetEvent(ctx, tx, c.eventID)
			if err != nil {
				// Impossible by construction: outbox.event_id is a foreign key
				// to an immutable events row. Reported loudly rather than
				// skipped, because a delivery whose event cannot be read is a
				// corrupt database, not a delivery problem.
				return fmt.Errorf("load event %s for delivery %s: %w", c.eventID, c.id, err)
			}
			claims = append(claims, claimedDelivery{
				outboxID:       c.id,
				eventID:        c.eventID,
				destination:    c.destination,
				attempt:        int(attempts),
				epoch:          epoch,
				event:          event,
				idempotencyKey: c.eventID + ":" + c.destination,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return claims, nil
}

// deliver calls the registered Deliverer for one claimed row, outside any
// transaction. An unregistered destination cannot reach here (the claim only
// selects registered ones); it is reported as a permanent failure rather than
// panicking, so a registration bug dead-letters one row instead of taking the
// process down.
//
// A Deliverer that panics is treated the same way as one that returns an
// error: the attempt failed and is retried with backoff (and dead-lettered once
// the budget is spent). Letting the panic escape would end Run's loop - the one
// thing a failing round must not do - and, since the claim already holds the
// lease, the row would come back after the lease expired and take the process
// down again. The recorded text names only the panic value's type: a panic
// message is not under the deliverer's "no credentials in errors" contract.
func (d *Dispatcher) deliver(ctx context.Context, c claimedDelivery) (err error) {
	deliverer, ok := d.destinations[c.destination]
	if !ok {
		return &PermanentError{Err: fmt.Errorf("no deliverer is registered for destination %q", c.destination)}
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("deliverer for destination %q panicked (%T)", c.destination, r)
		}
	}()
	return deliverer.Deliver(ctx, Delivery{
		OutboxID:       c.outboxID,
		EventID:        c.eventID,
		Destination:    c.destination,
		Attempt:        c.attempt,
		Event:          c.event,
		IdempotencyKey: c.idempotencyKey,
	})
}

// deliveryOutcome is what a write-back records. It decides the statement, and
// then the counter, in one place, so the two cannot disagree.
type deliveryOutcome int

const (
	outcomeDelivered deliveryOutcome = iota
	outcomeRetried
	outcomeDeadLettered
)

// complete writes the outcome of one delivery back to its row.
//
// Every statement fences on (state='pending', lease_owner, lease_epoch): the
// row must still be exactly the claim this dispatcher was granted. Zero rows
// affected therefore means the lease was taken over (or the row was trimmed)
// while the delivery ran, and the outcome — success or failure — belongs to the
// other writer, not to this one. That is counted as LostLease and nothing is
// written: overwriting it would let a slow dispatcher mark a row delivered
// while the new owner is mid-delivery, or reschedule a delivery that already
// succeeded.
func (d *Dispatcher) complete(ctx context.Context, c claimedDelivery, deliverErr error, report *DispatchReport) error {
	now := d.clock()
	outcome := outcomeDelivered
	switch {
	case deliverErr == nil:
		outcome = outcomeDelivered
	case isPermanentDeliveryError(deliverErr), d.maxAttempts > 0 && c.attempt >= d.maxAttempts:
		// Either the failure cannot be fixed by retrying, or this attempt was
		// the last one the budget allowed.
		outcome = outcomeDeadLettered
	default:
		outcome = outcomeRetried
	}

	var (
		query string
		args  []any
	)
	switch outcome {
	case outcomeDelivered:
		query = `UPDATE outbox
		         SET state = 'delivered', delivered_at = ?, next_attempt_at = NULL, last_error = NULL,
		             lease_owner = NULL, lease_until = NULL
		         WHERE id = ? AND state = 'pending' AND lease_owner = ? AND lease_epoch = ?`
		args = []any{unixMilli(now), c.outboxID, d.owner, c.epoch}
	case outcomeDeadLettered:
		query = `UPDATE outbox
		         SET state = 'dead_letter', next_attempt_at = NULL, last_error = ?,
		             lease_owner = NULL, lease_until = NULL
		         WHERE id = ? AND state = 'pending' AND lease_owner = ? AND lease_epoch = ?`
		args = []any{truncateLastError(deliverErr.Error()), c.outboxID, d.owner, c.epoch}
	default:
		query = `UPDATE outbox
		         SET next_attempt_at = ?, last_error = ?, lease_owner = NULL, lease_until = NULL
		         WHERE id = ? AND state = 'pending' AND lease_owner = ? AND lease_epoch = ?`
		args = []any{unixMilli(now.Add(d.backoffFor(c.attempt))), truncateLastError(deliverErr.Error()),
			c.outboxID, d.owner, c.epoch}
	}

	var affected int64
	err := d.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		res, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("write back delivery %s: %w", c.outboxID, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("rows affected for delivery %s: %w", c.outboxID, err)
		}
		affected = n
		return nil
	})
	if err != nil {
		return err
	}
	if affected == 0 {
		report.LostLease++
		return nil
	}
	switch outcome {
	case outcomeDelivered:
		report.Delivered++
	case outcomeDeadLettered:
		report.DeadLettered++
	default:
		report.Retried++
	}
	return nil
}

// backoffFor is the delay after the attempt-th failure: min(base * 2^(n-1),
// max), computed by doubling rather than shifting so that a large attempt count
// cannot overflow into a negative duration (which would make the row due in the
// past and retry in a tight loop).
func (d *Dispatcher) backoffFor(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := d.backoffBase
	for i := 1; i < attempt; i++ {
		if delay >= d.backoffMax {
			return d.backoffMax
		}
		delay *= 2
		if delay <= 0 { // overflowed
			return d.backoffMax
		}
	}
	if delay > d.backoffMax {
		return d.backoffMax
	}
	return delay
}

// isPermanentDeliveryError reports whether a delivery failure wraps
// PermanentError.
func isPermanentDeliveryError(err error) bool {
	var permanent *PermanentError
	return errors.As(err, &permanent)
}

// destinationPlaceholders builds the "?, ?, ?" list for the claim's IN clause.
func (d *Dispatcher) destinationPlaceholders() string {
	return strings.TrimSuffix(strings.Repeat("?, ", len(d.destinationNames)), ", ")
}
