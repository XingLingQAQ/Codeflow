package runstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/run"
)

// These tests cover dispatcher.go and consumer.go: the delivery half of the
// event contract. The properties they exist to prove are the ones §19.3, §19.1
// and §28 T1.05.b make load-bearing:
//
//   - every registered destination of an event is delivered, and only
//     registered destinations are claimed;
//   - a failure is retried with exponential backoff, and the attempt budget is
//     spent when an attempt starts, so a dispatcher that dies mid-delivery
//     still counts;
//   - an unfixable failure is dead-lettered immediately, and a dead letter
//     cannot be erased;
//   - a lease that expires can be taken over, and the dispatcher that lost it
//     cannot overwrite the new owner's result (the epoch fence);
//   - delivery is at least once, so the downstream must de-duplicate on
//     IdempotencyKey — and MarkConsumedTx is how it does that, in the same
//     transaction as its derived state;
//   - pending rows survive a restart, and RetentionHold keeps retention away
//     from a fact that still owes a delivery.
//
// Time is always injected: no test sleeps for a backoff or a lease.

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// dispatchDestination names the destination every dispatcher test serves. The
// fixture appends events with it, so "registered" and "queued" line up.
const dispatchDestination = "ws:project:" + eventProjectID

// testClock is a movable clock for DispatcherOptions.Clock. Tests advance it
// instead of sleeping, which is what makes lease expiry and backoff
// deterministic.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func newTestClock() *testClock {
	return &testClock{now: time.UnixMilli(1700000005000)}
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// scriptedDeliverer records every call and decides what to return through fn.
// The recorder is what lets a test assert "delivered exactly once" (by counting
// calls for a destination) and "the downstream saw a duplicate" (by counting
// arrivals for one IdempotencyKey).
type scriptedDeliverer struct {
	mu    sync.Mutex
	calls []Delivery
	fn    func(d Delivery) error
	// notify, when non-nil, receives a value before fn runs. A blocking
	// deliverer uses it to signal "the delivery has started" and then waits on
	// a channel the test releases.
	notify chan<- Delivery
	// block, when non-nil, is waited on after notify. The test closes it to let
	// the delivery finish, which is how a dispatcher is held mid-delivery while
	// its lease expires.
	block <-chan struct{}
}

func (s *scriptedDeliverer) Deliver(ctx context.Context, d Delivery) error {
	s.mu.Lock()
	s.calls = append(s.calls, d)
	fn, notify, block := s.fn, s.notify, s.block
	s.mu.Unlock()

	if notify != nil {
		notify <- d
	}
	if block != nil {
		<-block
	}
	if fn == nil {
		return nil
	}
	return fn(d)
}

// callCount is how many deliveries this deliverer was asked to perform.
func (s *scriptedDeliverer) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

// deliveries returns a copy of the recorded calls.
func (s *scriptedDeliverer) deliveries() []Delivery {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Delivery(nil), s.calls...)
}

// idempotentSink is a downstream that applies each IdempotencyKey exactly once
// and counts the arrivals it had to absorb. It is the "consumers are idempotent
// on event_id" half of at-least-once delivery: two arrivals for one key must
// produce one application.
type idempotentSink struct {
	mu        sync.Mutex
	arrivals  map[string]int
	applied   map[string]int
	calls     []Delivery
	applyErr  error
	applyFunc func(d Delivery) error
}

func newIdempotentSink() *idempotentSink {
	return &idempotentSink{arrivals: map[string]int{}, applied: map[string]int{}}
}

func (s *idempotentSink) Deliver(_ context.Context, d Delivery) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, d)
	s.arrivals[d.IdempotencyKey]++
	if s.arrivals[d.IdempotencyKey] > 1 {
		// A duplicate delivery: absorbed, no side effect. This is what a
		// consumer's consumer_offsets row does in production.
		return nil
	}
	if s.applyFunc != nil {
		if err := s.applyFunc(d); err != nil {
			return err
		}
	}
	s.applied[d.IdempotencyKey]++
	return s.applyErr
}

func (s *idempotentSink) arrivalCount(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.arrivals[key]
}

func (s *idempotentSink) appliedCount(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.applied[key]
}

// callCount is how many deliveries this sink was asked to perform, duplicates
// included: it is the "at least once" side of the contract.
func (s *idempotentSink) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

// deliveries returns a copy of the recorded calls, so a test can inspect what
// the sink was handed rather than only how often.
func (s *idempotentSink) deliveries() []Delivery {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Delivery(nil), s.calls...)
}

// outboxRow is the stored state of one delivery, as the tests read it back.
type outboxRow struct {
	id          string
	state       string
	attempts    int64
	nextAttempt sql.NullInt64
	lastError   sql.NullString
	leaseOwner  sql.NullString
	leaseUntil  sql.NullInt64
	epoch       int64
	deliveredAt sql.NullInt64
}

// readOutboxRow reads the row for one (event, destination).
func readOutboxRow(t *testing.T, q Querier, eventID, destination string) outboxRow {
	t.Helper()
	var row outboxRow
	err := q.QueryRowContext(context.Background(), `
		SELECT id, state, attempt_count, next_attempt_at, last_error,
		       lease_owner, lease_until, lease_epoch, delivered_at
		FROM outbox WHERE event_id = ? AND destination = ?`, eventID, destination).
		Scan(&row.id, &row.state, &row.attempts, &row.nextAttempt, &row.lastError,
			&row.leaseOwner, &row.leaseUntil, &row.epoch, &row.deliveredAt)
	if err != nil {
		t.Fatalf("read outbox row (%s, %s): %v", eventID, destination, err)
	}
	return row
}

// outboxRowByID reads one row by outbox id, for the tests that hold a row id.
func outboxRowByID(t *testing.T, q Querier, id string) outboxRow {
	t.Helper()
	var row outboxRow
	err := q.QueryRowContext(context.Background(), `
		SELECT id, state, attempt_count, next_attempt_at, last_error,
		       lease_owner, lease_until, lease_epoch, delivered_at
		FROM outbox WHERE id = ?`, id).
		Scan(&row.id, &row.state, &row.attempts, &row.nextAttempt, &row.lastError,
			&row.leaseOwner, &row.leaseUntil, &row.epoch, &row.deliveredAt)
	if err != nil {
		t.Fatalf("read outbox row %s: %v", id, err)
	}
	return row
}

// dispatcherFixture is a store with one run and a movable clock.
type dispatcherFixture struct {
	t      *testing.T
	events *eventFixture
	store  *Store
	path   string
	clock  *testClock
}

func newDispatcherFixture(t *testing.T) *dispatcherFixture {
	t.Helper()
	events := newEventFixture(t)
	return &dispatcherFixture{
		t:      t,
		events: events,
		store:  events.store,
		path:   events.path,
		clock:  newTestClock(),
	}
}

// newDispatcher builds a dispatcher over the fixture's store.
func (f *dispatcherFixture) newDispatcher(t *testing.T, opts DispatcherOptions) *Dispatcher {
	t.Helper()
	if opts.Owner == "" {
		opts.Owner = "dispatcher-1"
	}
	if opts.Destinations == nil {
		opts.Destinations = map[string]Deliverer{dispatchDestination: &scriptedDeliverer{}}
	}
	if opts.Clock == nil {
		opts.Clock = f.clock.Now
	}
	d, err := NewDispatcher(f.store, opts)
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	return d
}

// appendWithDestination appends one run-scoped event queued to the given
// destinations and returns it.
func (f *dispatcherFixture) appendWithDestination(t *testing.T, destinations ...string) Event {
	t.Helper()
	return f.events.appendEvent(t, run.EventRunFailed, &f.events.runID, destinations...)
}

// appendWithDestinationAt is appendWithDestination with a caller-chosen
// occurred_at. The dispatcher claims in next_attempt_at order and falls back to
// the row id, which is a random UUID, so a test that needs "the oldest
// undelivered fact" to be a particular event must give the events distinct
// instants rather than rely on the order they were written in.
func (f *dispatcherFixture) appendWithDestinationAt(t *testing.T, occurredAt time.Time, destinations ...string) Event {
	t.Helper()
	identity, attemptID, _ := claimedIdentity(t, run.EventRunFailed, &f.events.runID)
	event, err := f.events.tryAppendInput(EventInput{
		ProjectID:    eventProjectID,
		RunID:        &f.events.runID,
		AttemptID:    attemptID,
		Type:         string(run.EventRunFailed),
		OccurredAt:   occurredAt,
		Identity:     identity,
		Payload:      []byte(`{"n":1}`),
		Destinations: destinations,
	})
	if err != nil {
		t.Fatalf("AppendEventTx(%s): %v", run.EventRunFailed, err)
	}
	return event
}

// mustDispatch runs one round and fails the test on an error.
func mustDispatch(t *testing.T, d *Dispatcher) DispatchReport {
	t.Helper()
	report, err := d.DispatchOnce(context.Background())
	if err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}
	return report
}

// ---------------------------------------------------------------------------
// Claiming and delivering
// ---------------------------------------------------------------------------

// TestDispatcherDeliversEachRegisteredDestinationOnce is the base case: one
// event, two registered destinations, one delivery each — and the row for an
// unregistered destination is left completely alone, because a dispatcher must
// not steal work from the dispatcher that serves it.
func TestDispatcherDeliversEachRegisteredDestinationOnce(t *testing.T) {
	f := newDispatcherFixture(t)

	const (
		first  = "ws:project:" + eventProjectID
		second = "audit:export"
		other  = "desktop:notify" // nobody registered for this one
	)
	event := f.appendWithDestination(t, first, second, other)

	sinkFirst := newIdempotentSink()
	sinkSecond := newIdempotentSink()
	d := f.newDispatcher(t, DispatcherOptions{Destinations: map[string]Deliverer{
		first:  sinkFirst,
		second: sinkSecond,
	}})

	report := mustDispatch(t, d)
	if report.Claimed != 2 || report.Delivered != 2 || report.Retried != 0 ||
		report.DeadLettered != 0 || report.LostLease != 0 {
		t.Errorf("report = %+v, want claimed=2 delivered=2 and nothing else", report)
	}
	if got := sinkFirst.callCount(); got != 1 {
		t.Errorf("first destination received %d deliveries, want 1", got)
	}
	if got := sinkSecond.callCount(); got != 1 {
		t.Errorf("second destination received %d deliveries, want 1", got)
	}

	for _, destination := range []string{first, second} {
		row := readOutboxRow(t, f.store.DB(), event.ID, destination)
		if row.state != "delivered" {
			t.Errorf("%s state = %q, want delivered", destination, row.state)
		}
		if !row.deliveredAt.Valid || row.deliveredAt.Int64 != unixMilli(f.clock.Now()) {
			t.Errorf("%s delivered_at = %+v, want %d", destination, row.deliveredAt, unixMilli(f.clock.Now()))
		}
		if row.attempts != 1 {
			t.Errorf("%s attempt_count = %d, want 1", destination, row.attempts)
		}
		if row.nextAttempt.Valid {
			t.Errorf("%s next_attempt_at = %d, want NULL on a delivered row", destination, row.nextAttempt.Int64)
		}
		if row.leaseOwner.Valid || row.leaseUntil.Valid {
			t.Errorf("%s still leased (owner=%+v until=%+v), want the lease released", destination, row.leaseOwner, row.leaseUntil)
		}
		if row.lastError.Valid {
			t.Errorf("%s last_error = %q, want NULL", destination, row.lastError.String)
		}
		if row.epoch != 1 {
			t.Errorf("%s lease_epoch = %d, want 1 after one claim", destination, row.epoch)
		}
	}

	// The unregistered destination: untouched in every column a claim touches.
	untouched := readOutboxRow(t, f.store.DB(), event.ID, other)
	if untouched.state != "pending" || untouched.attempts != 0 {
		t.Errorf("unregistered destination state=%q attempts=%d, want pending/0", untouched.state, untouched.attempts)
	}
	if untouched.leaseOwner.Valid || untouched.leaseUntil.Valid {
		t.Errorf("unregistered destination was leased (owner=%+v until=%+v), want NULL",
			untouched.leaseOwner, untouched.leaseUntil)
	}
	if untouched.epoch != 0 {
		t.Errorf("unregistered destination lease_epoch = %d, want 0 (never claimed)", untouched.epoch)
	}

	// The delivery carries the event and the key the downstream de-duplicates
	// on, so the assertions above are about the real payload, not an empty one.
	delivered := sinkSecond.deliveries()
	if len(delivered) != 1 {
		t.Fatalf("recorded deliveries = %d, want 1", len(delivered))
	}
	got := delivered[0]
	if got.EventID != event.ID || got.Event.ID != event.ID {
		t.Errorf("delivered event id = %q / %q, want %q", got.EventID, got.Event.ID, event.ID)
	}
	if got.Destination != second {
		t.Errorf("delivered destination = %q, want %q", got.Destination, second)
	}
	if got.Attempt != 1 {
		t.Errorf("delivered attempt = %d, want 1", got.Attempt)
	}
	if got.IdempotencyKey != event.ID+":"+second {
		t.Errorf("idempotency key = %q, want %q", got.IdempotencyKey, event.ID+":"+second)
	}
	if string(got.Event.Payload) != string(event.Payload) || got.Event.ProjectSeq != event.ProjectSeq {
		t.Errorf("delivered event = %+v, want the stored event %+v", got.Event, event)
	}

	// A second round finds nothing to do: the delivered rows are not pending.
	if again := mustDispatch(t, d); again.Claimed != 0 {
		t.Errorf("second round claimed %d rows, want 0", again.Claimed)
	}
}

// TestDispatcherRetriesWithBackoffUntilSuccess proves the retry schedule: two
// failures then a success, with the delay doubling each time and the row's
// attempt counter matching the number of attempts that actually ran.
func TestDispatcherRetriesWithBackoffUntilSuccess(t *testing.T) {
	f := newDispatcherFixture(t)
	event := f.appendWithDestination(t, dispatchDestination)

	failures := 0
	sink := &scriptedDeliverer{fn: func(Delivery) error {
		failures++
		if failures <= 2 {
			return fmt.Errorf("destination unavailable (attempt %d)", failures)
		}
		return nil
	}}
	d := f.newDispatcher(t, DispatcherOptions{
		Destinations: map[string]Deliverer{dispatchDestination: sink},
		BackoffBase:  time.Second,
		BackoffMax:   5 * time.Minute,
	})

	// Attempt 1 fails.
	start := f.clock.Now()
	report := mustDispatch(t, d)
	if report.Retried != 1 || report.Delivered != 0 {
		t.Fatalf("round 1 report = %+v, want retried=1", report)
	}
	row := readOutboxRow(t, f.store.DB(), event.ID, dispatchDestination)
	if row.attempts != 1 || row.state != "pending" {
		t.Errorf("after attempt 1: attempts=%d state=%q, want 1/pending", row.attempts, row.state)
	}
	if !row.nextAttempt.Valid || row.nextAttempt.Int64 != unixMilli(start.Add(time.Second)) {
		t.Errorf("after attempt 1: next_attempt_at = %+v, want %d (base 1s)",
			row.nextAttempt, unixMilli(start.Add(time.Second)))
	}
	if !row.lastError.Valid || !strings.Contains(row.lastError.String, "attempt 1") {
		t.Errorf("after attempt 1: last_error = %+v, want the deliverer's message", row.lastError)
	}
	if row.leaseOwner.Valid || row.leaseUntil.Valid {
		t.Errorf("after attempt 1: still leased, want the lease released on a retry")
	}
	if row.epoch != 1 {
		t.Errorf("after attempt 1: lease_epoch = %d, want 1", row.epoch)
	}

	// Nothing is due until the backoff elapses.
	if early := mustDispatch(t, d); early.Claimed != 0 {
		t.Errorf("round before the backoff elapsed claimed %d rows, want 0", early.Claimed)
	}

	// Attempt 2 fails, and the delay doubles.
	f.clock.Advance(time.Second)
	second := f.clock.Now()
	report = mustDispatch(t, d)
	if report.Retried != 1 {
		t.Fatalf("round 2 report = %+v, want retried=1", report)
	}
	row = readOutboxRow(t, f.store.DB(), event.ID, dispatchDestination)
	if row.attempts != 2 {
		t.Errorf("after attempt 2: attempt_count = %d, want 2", row.attempts)
	}
	if !row.nextAttempt.Valid || row.nextAttempt.Int64 != unixMilli(second.Add(2*time.Second)) {
		t.Errorf("after attempt 2: next_attempt_at = %+v, want %d (base * 2)",
			row.nextAttempt, unixMilli(second.Add(2*time.Second)))
	}

	// Attempt 3 succeeds.
	f.clock.Advance(2 * time.Second)
	report = mustDispatch(t, d)
	if report.Delivered != 1 || report.Retried != 0 {
		t.Fatalf("round 3 report = %+v, want delivered=1", report)
	}
	row = readOutboxRow(t, f.store.DB(), event.ID, dispatchDestination)
	if row.state != "delivered" || row.attempts != 3 {
		t.Errorf("after attempt 3: state=%q attempts=%d, want delivered/3", row.state, row.attempts)
	}
	if row.lastError.Valid {
		t.Errorf("after attempt 3: last_error = %q, want NULL on success", row.lastError.String)
	}
	if !row.deliveredAt.Valid {
		t.Error("after attempt 3: delivered_at is NULL, want the success instant")
	}
	if sink.callCount() != 3 {
		t.Errorf("deliverer was called %d times, want 3", sink.callCount())
	}
}

// TestDispatcherBackoffIsCappedAtMax pins the ceiling: the doubling must stop at
// BackoffMax instead of growing without bound (or overflowing).
func TestDispatcherBackoffIsCappedAtMax(t *testing.T) {
	f := newDispatcherFixture(t)
	d := f.newDispatcher(t, DispatcherOptions{
		Destinations: map[string]Deliverer{dispatchDestination: &scriptedDeliverer{}},
		BackoffBase:  time.Second,
		BackoffMax:   10 * time.Second,
		MaxAttempts:  20,
	})
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{1, time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{4, 8 * time.Second},
		{5, 10 * time.Second},
		{1000, 10 * time.Second},
	}
	for _, tc := range cases {
		if got := d.backoffFor(tc.attempt); got != tc.want {
			t.Errorf("backoffFor(%d) = %s, want %s", tc.attempt, got, tc.want)
		}
	}
}

// TestDispatcherDeadLettersAfterMaxAttempts proves the attempt budget: a
// delivery that keeps failing is dead-lettered once the budget is spent, with
// the failure text kept for the operator (truncated), and the dead letter
// cannot be deleted (003's retention trigger).
func TestDispatcherDeadLettersAfterMaxAttempts(t *testing.T) {
	f := newDispatcherFixture(t)
	event := f.appendWithDestination(t, dispatchDestination)

	long := strings.Repeat("x", 700)
	attempt := 0
	sink := &scriptedDeliverer{fn: func(Delivery) error {
		attempt++
		return fmt.Errorf("destination refused: %s", long)
	}}
	d := f.newDispatcher(t, DispatcherOptions{
		Destinations: map[string]Deliverer{dispatchDestination: sink},
		BackoffBase:  time.Second,
		BackoffMax:   time.Minute,
		MaxAttempts:  3,
	})

	for i := 1; i <= 3; i++ {
		report := mustDispatch(t, d)
		if i < 3 {
			if report.Retried != 1 {
				t.Fatalf("attempt %d report = %+v, want retried=1", i, report)
			}
			f.clock.Advance(30 * time.Second)
			continue
		}
		if report.DeadLettered != 1 {
			t.Fatalf("attempt %d report = %+v, want dead_lettered=1", i, report)
		}
	}

	row := readOutboxRow(t, f.store.DB(), event.ID, dispatchDestination)
	if row.state != "dead_letter" {
		t.Errorf("state = %q, want dead_letter after the budget is spent", row.state)
	}
	if row.attempts != 3 {
		t.Errorf("attempt_count = %d, want 3", row.attempts)
	}
	if !row.lastError.Valid {
		t.Fatal("last_error is NULL, want the last failure kept for the operator")
	}
	if len(row.lastError.String) > maxLastErrorBytes {
		t.Errorf("last_error is %d bytes, want at most %d", len(row.lastError.String), maxLastErrorBytes)
	}
	if !strings.HasPrefix(row.lastError.String, "destination refused: ") {
		t.Errorf("last_error = %q, want the deliverer's message", row.lastError.String)
	}
	if row.nextAttempt.Valid {
		t.Errorf("next_attempt_at = %d, want NULL on a dead letter", row.nextAttempt.Int64)
	}
	if row.leaseOwner.Valid || row.leaseUntil.Valid {
		t.Errorf("dead letter still leased (owner=%+v until=%+v), want NULL", row.leaseOwner, row.leaseUntil)
	}

	// Further rounds must not touch it, and the deliverer must not be called
	// again.
	f.clock.Advance(time.Hour)
	if report := mustDispatch(t, d); report.Claimed != 0 {
		t.Errorf("a round after the dead letter claimed %d rows, want 0", report.Claimed)
	}
	if sink.callCount() != 3 {
		t.Errorf("deliverer was called %d times, want 3", sink.callCount())
	}

	// A dead letter is retained: deleting it is refused and mapped to the
	// typed error, exactly as 003 promised.
	if _, err := f.store.DB().ExecContext(context.Background(),
		`DELETE FROM outbox WHERE id = ?`, row.id); err == nil {
		t.Fatal("DELETE of a dead_letter row succeeded, want the retention trigger to refuse it")
	} else if mapped := mapConstraintError("delete delivery", err); !errors.Is(mapped, ErrOutboxDeadLetterRetained) {
		t.Errorf("mapConstraintError = %v, want ErrOutboxDeadLetterRetained", mapped)
	}
}

// TestDispatcherDeadLettersPermanentError proves that a failure retrying cannot
// fix skips the rest of the budget: one attempt, then dead_letter.
func TestDispatcherDeadLettersPermanentError(t *testing.T) {
	f := newDispatcherFixture(t)
	event := f.appendWithDestination(t, dispatchDestination)

	inner := errors.New("route does not exist")
	sink := &scriptedDeliverer{fn: func(Delivery) error {
		return &PermanentError{Err: inner}
	}}
	d := f.newDispatcher(t, DispatcherOptions{
		Destinations: map[string]Deliverer{dispatchDestination: sink},
		MaxAttempts:  10,
	})

	report := mustDispatch(t, d)
	if report.DeadLettered != 1 || report.Retried != 0 {
		t.Fatalf("report = %+v, want dead_lettered=1 after one attempt", report)
	}
	row := readOutboxRow(t, f.store.DB(), event.ID, dispatchDestination)
	if row.state != "dead_letter" || row.attempts != 1 {
		t.Errorf("state=%q attempts=%d, want dead_letter/1", row.state, row.attempts)
	}
	if !row.lastError.Valid || !strings.Contains(row.lastError.String, "route does not exist") {
		t.Errorf("last_error = %+v, want the wrapped message", row.lastError)
	}

	// PermanentError keeps its wrapped error reachable, so a deliverer can be
	// tested with errors.Is.
	permanent := &PermanentError{Err: inner}
	if !errors.Is(permanent, inner) {
		t.Error("errors.Is(PermanentError{inner}, inner) = false, want true")
	}
	if got := (&PermanentError{}).Error(); !strings.Contains(got, "permanent delivery failure") {
		t.Errorf("PermanentError{}.Error() = %q, want it to be usable with no wrapped error", got)
	}
}

// TestDispatcherSurvivesAPanickingDeliverer: a deliverer that panics is a failed
// attempt (retried with backoff), not a crashed dispatcher. The round returns
// normally, the lease is released, and the recorded error names the panic's
// type but not its message.
func TestDispatcherSurvivesAPanickingDeliverer(t *testing.T) {
	f := newDispatcherFixture(t)
	event := f.appendWithDestination(t, dispatchDestination)

	sink := &scriptedDeliverer{fn: func(Delivery) error {
		panic("secret-token-in-panic-message")
	}}
	d := f.newDispatcher(t, DispatcherOptions{
		Destinations: map[string]Deliverer{dispatchDestination: sink},
		MaxAttempts:  10,
	})

	report := mustDispatch(t, d)
	if report.Claimed != 1 || report.Retried != 1 || report.Delivered != 0 || report.DeadLettered != 0 {
		t.Fatalf("report = %+v, want claimed=1 retried=1", report)
	}
	row := readOutboxRow(t, f.store.DB(), event.ID, dispatchDestination)
	if row.state != "pending" || row.attempts != 1 || row.leaseOwner.Valid || row.leaseUntil.Valid {
		t.Errorf("row = %+v, want pending, attempts=1, lease released", row)
	}
	if !row.lastError.Valid || !strings.Contains(row.lastError.String, "panicked") {
		t.Errorf("last_error = %+v, want it to record the panic", row.lastError)
	}
	if strings.Contains(row.lastError.String, "secret-token") {
		t.Errorf("last_error = %q, must not copy the panic message", row.lastError.String)
	}
}

// TestDispatcherDeadLettersAnExhaustedCrashLoop covers the attempt that starts
// and never reports back: the counter is incremented at claim time, so a row
// whose budget was already spent by crashed attempts is dead-lettered at claim
// time instead of being delivered (or retried) forever.
func TestDispatcherDeadLettersAnExhaustedCrashLoop(t *testing.T) {
	ctx := context.Background()
	f := newDispatcherFixture(t)
	event := f.appendWithDestination(t, dispatchDestination)

	// Model three attempts that started and died: attempt_count is at the
	// budget, the row is due and unleased, and no result was ever written.
	if _, err := f.store.DB().ExecContext(ctx,
		`UPDATE outbox SET attempt_count = 3, last_error = NULL WHERE event_id = ? AND destination = ?`,
		event.ID, dispatchDestination); err != nil {
		t.Fatalf("seed an exhausted row: %v", err)
	}

	sink := &scriptedDeliverer{}
	d := f.newDispatcher(t, DispatcherOptions{
		Destinations: map[string]Deliverer{dispatchDestination: sink},
		MaxAttempts:  3,
	})

	report := mustDispatch(t, d)
	if report.Claimed != 1 || report.DeadLettered != 1 {
		t.Fatalf("report = %+v, want claimed=1 dead_lettered=1", report)
	}
	if sink.callCount() != 0 {
		t.Errorf("deliverer was called %d times for an exhausted row, want 0", sink.callCount())
	}
	row := readOutboxRow(t, f.store.DB(), event.ID, dispatchDestination)
	if row.state != "dead_letter" {
		t.Errorf("state = %q, want dead_letter", row.state)
	}
	if row.attempts != 4 {
		t.Errorf("attempt_count = %d, want 4 (the claim that found the budget spent is still an attempt)", row.attempts)
	}
	if !row.lastError.Valid || !strings.Contains(row.lastError.String, "attempts exhausted without a completion") {
		t.Errorf("last_error = %+v, want the exhaustion recorded", row.lastError)
	}
	if row.leaseOwner.Valid || row.leaseUntil.Valid {
		t.Errorf("exhausted row is still leased (owner=%+v until=%+v), want NULL", row.leaseOwner, row.leaseUntil)
	}
	if row.deliveredAt.Valid {
		t.Error("delivered_at is set on a dead letter, want NULL (nothing was delivered)")
	}

	// The exhausted row is retained like any other dead letter.
	if _, err := f.store.DB().ExecContext(ctx, `DELETE FROM outbox WHERE id = ?`, row.id); err == nil {
		t.Error("DELETE of the exhausted dead letter succeeded, want the retention trigger to refuse it")
	}
}

// ---------------------------------------------------------------------------
// Lease, fence and crash recovery
// ---------------------------------------------------------------------------

// TestDispatcherLeaseTakeoverAfterExpiry is the crash-recovery case: dispatcher
// A claims a row and dies mid-delivery (its write-back never happens), the lease
// expires, dispatcher B claims the row and delivers it, and A's late write-back
// matches no row.
//
// The downstream sees two arrivals for one delivery and applies one: that is
// what at-least-once delivery plus an idempotent consumer means, and it is the
// property the IdempotencyKey exists for.
func TestDispatcherLeaseTakeoverAfterExpiry(t *testing.T) {
	ctx := context.Background()
	f := newDispatcherFixture(t)
	event := f.appendWithDestination(t, dispatchDestination)

	sink := newIdempotentSink()
	started := make(chan Delivery, 1)
	release := make(chan struct{})

	// A delivers slowly and is held mid-delivery; its own write-back then runs
	// after B has already taken the row over.
	slow := &scriptedDeliverer{notify: started, block: release, fn: func(d Delivery) error {
		// A does finish its delivery, after B already delivered the same event:
		// the downstream sees the duplicate at-least-once delivery promises.
		return sink.Deliver(ctx, d)
	}}
	a := f.newDispatcher(t, DispatcherOptions{
		Owner:         "dispatcher-a",
		Destinations:  map[string]Deliverer{dispatchDestination: slow},
		LeaseDuration: 30 * time.Second,
	})

	type result struct {
		report DispatchReport
		err    error
	}
	aDone := make(chan result, 1)
	go func() {
		report, err := a.DispatchOnce(ctx)
		aDone <- result{report: report, err: err}
	}()

	// Wait until A is inside the delivery, so the row is definitely leased.
	<-started
	row := readOutboxRow(t, f.store.DB(), event.ID, dispatchDestination)
	if !row.leaseOwner.Valid || row.leaseOwner.String != "dispatcher-a" {
		t.Fatalf("lease_owner = %+v, want dispatcher-a", row.leaseOwner)
	}
	if !row.leaseUntil.Valid || row.leaseUntil.Int64 != unixMilli(f.clock.Now().Add(30*time.Second)) {
		t.Fatalf("lease_until = %+v, want %d", row.leaseUntil, unixMilli(f.clock.Now().Add(30*time.Second)))
	}

	// B cannot touch a live lease. This is the assertion the "claim ignores
	// lease_until" mutation breaks.
	b := f.newDispatcher(t, DispatcherOptions{
		Owner:         "dispatcher-b",
		Destinations:  map[string]Deliverer{dispatchDestination: sink},
		LeaseDuration: 30 * time.Second,
	})
	if report := mustDispatch(t, b); report.Claimed != 0 {
		t.Fatalf("B claimed %d rows while A's lease was live, want 0", report.Claimed)
	}
	if row := readOutboxRow(t, f.store.DB(), event.ID, dispatchDestination); row.epoch != 1 {
		t.Errorf("lease_epoch = %d after B's empty round, want 1", row.epoch)
	}

	// The lease expires, and B takes the row over.
	f.clock.Advance(30*time.Second + time.Millisecond)
	reportB := mustDispatch(t, b)
	if reportB.Claimed != 1 || reportB.Delivered != 1 {
		t.Fatalf("B's takeover report = %+v, want claimed=1 delivered=1", reportB)
	}
	taken := readOutboxRow(t, f.store.DB(), event.ID, dispatchDestination)
	if taken.state != "delivered" || taken.epoch != 2 {
		t.Errorf("after takeover: state=%q epoch=%d, want delivered/2", taken.state, taken.epoch)
	}
	if taken.attempts != 2 {
		t.Errorf("after takeover: attempt_count = %d, want 2 (A's attempt counts even though it never reported)", taken.attempts)
	}
	if !taken.deliveredAt.Valid || taken.deliveredAt.Int64 != unixMilli(f.clock.Now()) {
		t.Errorf("after takeover: delivered_at = %+v, want B's instant %d", taken.deliveredAt, unixMilli(f.clock.Now()))
	}

	// A's delivery finishes and it tries to write "delivered" with a lease it
	// no longer holds. It must not be able to say so.
	close(release)
	outcome := <-aDone
	if outcome.err != nil {
		t.Fatalf("A's round failed: %v", outcome.err)
	}
	if outcome.report.LostLease != 1 {
		t.Errorf("A's report = %+v, want lost_lease=1", outcome.report)
	}
	if outcome.report.Delivered != 0 {
		t.Errorf("A's report = %+v, want delivered=0 (the lease was taken over)", outcome.report)
	}

	final := readOutboxRow(t, f.store.DB(), event.ID, dispatchDestination)
	if final.state != "delivered" || final.epoch != 2 {
		t.Errorf("after A's late write-back: state=%q epoch=%d, want delivered/2 (B's result kept)", final.state, final.epoch)
	}
	if final.attempts != 2 {
		t.Errorf("after A's late write-back: attempt_count = %d, want 2", final.attempts)
	}

	// At-least-once: two arrivals, one application.
	if got := sink.arrivalCount(event.ID + ":" + dispatchDestination); got != 2 {
		t.Errorf("downstream arrivals = %d, want 2 (A delivered late, B delivered first)", got)
	}
	if got := slow.callCount(); got != 1 {
		t.Errorf("A's deliverer was called %d times, want 1", got)
	}
	if got := sink.appliedCount(event.ID + ":" + dispatchDestination); got != 1 {
		t.Errorf("downstream applied = %d, want 1", got)
	}
}

// TestDispatcherFenceRejectsStaleWritebackFromTheSameOwner is the case the
// epoch exists for: the same logical worker (same owner string) claims a row
// twice, because the first delivery outlived its lease, and the first
// write-back lands while the second attempt is still in flight. The owner check
// alone cannot tell the two attempts apart — the row still carries the owner
// string both of them wrote — so the epoch is what stops the stale attempt from
// rescheduling a row a newer attempt is holding.
//
// The stale attempt fails (the retry branch) and the fresh one succeeds. A
// stale *successful* write-back would be fenced by the state clause alone,
// because the fresh attempt has already moved the row off pending; the retry
// branch is the one that leaves the row pending, so it is the branch the epoch
// has to guard.
func TestDispatcherFenceRejectsStaleWritebackFromTheSameOwner(t *testing.T) {
	ctx := context.Background()
	f := newDispatcherFixture(t)
	event := f.appendWithDestination(t, dispatchDestination)

	const owner = "worker-1"

	// A: claims, then fails, and its write-back is released only after B has
	// taken the row over.
	staleStarted := make(chan Delivery, 1)
	staleRelease := make(chan struct{})
	stale := f.newDispatcher(t, DispatcherOptions{
		Owner: owner,
		Destinations: map[string]Deliverer{dispatchDestination: &scriptedDeliverer{
			notify: staleStarted, block: staleRelease,
			fn: func(Delivery) error { return errors.New("delivery failed after the lease expired") },
		}},
		LeaseDuration: 10 * time.Second,
		BackoffBase:   time.Second,
	})
	staleDone := make(chan DispatchReport, 1)
	go func() {
		report, err := stale.DispatchOnce(ctx)
		if err != nil {
			t.Errorf("stale round failed: %v", err)
		}
		staleDone <- report
	}()
	<-staleStarted

	// The same owner claims again after the lease expired: epoch 2. B's delivery
	// is held in flight, so the row is still pending and still owned by
	// "worker-1" when A's stale write-back arrives.
	freshStarted := make(chan Delivery, 1)
	freshRelease := make(chan struct{})
	sink := newIdempotentSink()
	fresh := f.newDispatcher(t, DispatcherOptions{
		Owner: owner,
		Destinations: map[string]Deliverer{dispatchDestination: &scriptedDeliverer{
			notify: freshStarted, block: freshRelease,
			fn: func(d Delivery) error { return sink.Deliver(ctx, d) },
		}},
		LeaseDuration: 10 * time.Second,
	})
	f.clock.Advance(10*time.Second + time.Millisecond)
	freshDone := make(chan DispatchReport, 1)
	go func() {
		report, err := fresh.DispatchOnce(ctx)
		if err != nil {
			t.Errorf("fresh round failed: %v", err)
		}
		freshDone <- report
	}()
	<-freshStarted

	held := readOutboxRow(t, f.store.DB(), event.ID, dispatchDestination)
	if held.epoch != 2 || held.attempts != 2 {
		t.Fatalf("after B's claim: epoch=%d attempts=%d, want 2/2", held.epoch, held.attempts)
	}
	if !held.leaseOwner.Valid || held.leaseOwner.String != owner {
		t.Fatalf("after B's claim: lease_owner = %+v, want %q", held.leaseOwner, owner)
	}
	bLeaseUntil, bNextAttempt := held.leaseUntil, held.nextAttempt

	// A's stale write-back: it failed, so without the epoch fence it would
	// reschedule the row — clear B's live lease and overwrite next_attempt_at
	// with its own backoff.
	close(staleRelease)
	staleReport := <-staleDone
	if staleReport.LostLease != 1 {
		t.Errorf("stale report = %+v, want lost_lease=1 (epoch 1 is fenced by epoch 2)", staleReport)
	}
	if staleReport.Retried != 0 || staleReport.DeadLettered != 0 {
		t.Errorf("stale report = %+v, want no retry and no dead letter recorded", staleReport)
	}

	stillHeld := readOutboxRow(t, f.store.DB(), event.ID, dispatchDestination)
	if !stillHeld.leaseOwner.Valid || stillHeld.leaseOwner.String != owner || stillHeld.leaseUntil != bLeaseUntil {
		t.Errorf("B's lease was disturbed by the stale write-back: owner=%+v until=%+v, want %q/%+v",
			stillHeld.leaseOwner, stillHeld.leaseUntil, owner, bLeaseUntil)
	}
	if stillHeld.nextAttempt != bNextAttempt {
		t.Errorf("next_attempt_at = %+v, want %+v unchanged: the stale retry must not reschedule a row B holds",
			stillHeld.nextAttempt, bNextAttempt)
	}
	if stillHeld.lastError.Valid {
		t.Errorf("last_error = %q, want NULL: the stale failure was fenced off", stillHeld.lastError.String)
	}
	if stillHeld.state != "pending" {
		t.Errorf("state = %q, want pending while B is in flight", stillHeld.state)
	}

	// B's delivery completes: the row is delivered once, by the attempt that
	// actually held the lease.
	close(freshRelease)
	freshReport := <-freshDone
	if freshReport.Delivered != 1 {
		t.Errorf("fresh report = %+v, want delivered=1", freshReport)
	}
	final := readOutboxRow(t, f.store.DB(), event.ID, dispatchDestination)
	if final.state != "delivered" {
		t.Errorf("state = %q, want delivered", final.state)
	}
	if final.attempts != 2 {
		t.Errorf("attempt_count = %d, want 2", final.attempts)
	}
	if !final.deliveredAt.Valid || final.deliveredAt.Int64 != unixMilli(f.clock.Now()) {
		t.Errorf("delivered_at = %+v, want %d", final.deliveredAt, unixMilli(f.clock.Now()))
	}
	if final.epoch != 2 {
		t.Errorf("lease_epoch = %d, want 2 (B's claim, never A's)", final.epoch)
	}
	if got := sink.appliedCount(event.ID + ":" + dispatchDestination); got != 1 {
		t.Errorf("downstream applied = %d, want 1", got)
	}
}

// TestDispatcherConcurrentDispatchersDeliverExactlyOnce runs two dispatchers
// over the same file at the same time and checks that no row is delivered twice
// and no round fails: BEGIN IMMEDIATE plus the lease CAS must serialise them
// without a SQLITE_BUSY escaping to the caller.
func TestDispatcherConcurrentDispatchersDeliverExactlyOnce(t *testing.T) {
	ctx := context.Background()
	f := newDispatcherFixture(t)

	const (
		events     = 12
		perEvent   = 2
		secondDest = "audit:export"
	)
	ids := make([]string, 0, events)
	for i := 0; i < events; i++ {
		ids = append(ids, f.appendWithDestination(t, dispatchDestination, secondDest).ID)
	}

	// Two stores over the same file: two connection pools, which is what two
	// processes would look like.
	storeB, _, err := OpenStore(ctx, f.path)
	if err != nil {
		t.Fatalf("second OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = storeB.Close() })

	shared := newIdempotentSink()
	makeDispatcher := func(store *Store, owner string) *Dispatcher {
		d, err := NewDispatcher(store, DispatcherOptions{
			Owner: owner,
			Destinations: map[string]Deliverer{
				dispatchDestination: shared,
				secondDest:          shared,
			},
			Clock: f.clock.Now,
		})
		if err != nil {
			t.Fatalf("NewDispatcher(%s): %v", owner, err)
		}
		return d
	}
	a := makeDispatcher(f.store, "worker-a")
	b := makeDispatcher(storeB, "worker-b")

	const rounds = 4
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		deliver  int
		failures []error
	)
	runRounds := func(d *Dispatcher) {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			report, err := d.DispatchOnce(ctx)
			mu.Lock()
			deliver += report.Delivered
			if err != nil {
				failures = append(failures, err)
			}
			mu.Unlock()
		}
	}
	wg.Add(2)
	go runRounds(a)
	go runRounds(b)
	wg.Wait()

	if len(failures) != 0 {
		t.Fatalf("concurrent rounds reported %d errors, want 0: %v", len(failures), failures)
	}
	if deliver != events*perEvent {
		t.Errorf("dispatchers reported %d deliveries, want %d", deliver, events*perEvent)
	}
	for _, eventID := range ids {
		for _, destination := range []string{dispatchDestination, secondDest} {
			key := eventID + ":" + destination
			if got := shared.arrivalCount(key); got != 1 {
				t.Errorf("delivery %s arrived %d times, want exactly 1", key, got)
			}
			if got := shared.appliedCount(key); got != 1 {
				t.Errorf("delivery %s was applied %d times, want exactly 1", key, got)
			}
		}
	}

	// Every row is delivered exactly once, with the attempt counter proving
	// that nobody delivered a row it had not claimed.
	var rows int
	if err := f.store.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM outbox WHERE state = 'delivered' AND attempt_count = 1`).Scan(&rows); err != nil {
		t.Fatalf("count delivered rows: %v", err)
	}
	if rows != events*perEvent {
		t.Errorf("rows delivered on the first attempt = %d, want %d", rows, events*perEvent)
	}
}

// TestDispatcherPendingRowsSurviveRestart proves the dispatcher's work is
// durable, not in-memory: rows queued before the process closed are still there
// afterwards and a fresh dispatcher delivers them.
func TestDispatcherPendingRowsSurviveRestart(t *testing.T) {
	ctx := context.Background()
	f := newDispatcherFixture(t)
	event := f.appendWithDestination(t, dispatchDestination)

	if err := f.store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	reopened, res, err := OpenStore(ctx, f.path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if len(res.Applied) != 0 {
		t.Errorf("reopen applied %d migrations, want 0", len(res.Applied))
	}

	row := readOutboxRow(t, reopened.DB(), event.ID, dispatchDestination)
	if row.state != "pending" || row.attempts != 0 || row.epoch != 0 {
		t.Fatalf("row after reopen = %+v, want pending/0/0", row)
	}

	sink := newIdempotentSink()
	d, err := NewDispatcher(reopened, DispatcherOptions{
		Owner:        "dispatcher-after-restart",
		Destinations: map[string]Deliverer{dispatchDestination: sink},
		Clock:        f.clock.Now,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	report, err := d.DispatchOnce(ctx)
	if err != nil {
		t.Fatalf("DispatchOnce after restart: %v", err)
	}
	if report.Delivered != 1 {
		t.Fatalf("report after restart = %+v, want delivered=1", report)
	}
	if got := readOutboxRow(t, reopened.DB(), event.ID, dispatchDestination); got.state != "delivered" {
		t.Errorf("state after restart = %q, want delivered", got.state)
	}
	if sink.appliedCount(event.ID+":"+dispatchDestination) != 1 {
		t.Error("the restarted dispatcher did not deliver the event exactly once")
	}
}

// ---------------------------------------------------------------------------
// Configuration and the Run loop
// ---------------------------------------------------------------------------

// TestNewDispatcherValidatesOptions pins the fail-closed construction: a
// dispatcher that cannot work is refused before it can take a lease.
func TestNewDispatcherValidatesOptions(t *testing.T) {
	f := newDispatcherFixture(t)
	deliverer := &scriptedDeliverer{}

	cases := []struct {
		name string
		opts DispatcherOptions
	}{
		{"blank owner", DispatcherOptions{Owner: "  ", Destinations: map[string]Deliverer{"d": deliverer}}},
		{"no destinations", DispatcherOptions{Owner: "w", Destinations: map[string]Deliverer{}}},
		{"nil destinations", DispatcherOptions{Owner: "w"}},
		{"blank destination name", DispatcherOptions{Owner: "w", Destinations: map[string]Deliverer{" ": deliverer}}},
		{"nil deliverer", DispatcherOptions{Owner: "w", Destinations: map[string]Deliverer{"d": nil}}},
		{"negative lease", DispatcherOptions{Owner: "w", Destinations: map[string]Deliverer{"d": deliverer}, LeaseDuration: -time.Second}},
		{"negative batch", DispatcherOptions{Owner: "w", Destinations: map[string]Deliverer{"d": deliverer}, BatchSize: -1}},
		{"negative backoff base", DispatcherOptions{Owner: "w", Destinations: map[string]Deliverer{"d": deliverer}, BackoffBase: -time.Second}},
		{"negative backoff max", DispatcherOptions{Owner: "w", Destinations: map[string]Deliverer{"d": deliverer}, BackoffMax: -time.Second}},
		{"backoff max below base", DispatcherOptions{Owner: "w", Destinations: map[string]Deliverer{"d": deliverer}, BackoffBase: time.Minute, BackoffMax: time.Second}},
		{"negative attempts", DispatcherOptions{Owner: "w", Destinations: map[string]Deliverer{"d": deliverer}, MaxAttempts: -1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewDispatcher(f.store, tc.opts); !errors.Is(err, ErrInvalidDispatcherConfig) {
				t.Errorf("NewDispatcher(%s) error = %v, want ErrInvalidDispatcherConfig", tc.name, err)
			}
		})
	}

	if _, err := NewDispatcher(nil, DispatcherOptions{Owner: "w", Destinations: map[string]Deliverer{"d": deliverer}}); !errors.Is(err, ErrInvalidDispatcherConfig) {
		t.Errorf("NewDispatcher(nil store) error = %v, want ErrInvalidDispatcherConfig", err)
	}

	// The zero-valued options that have defaults get them, so a caller cannot
	// end up with an unleased or unbounded dispatcher by omission.
	d := f.newDispatcher(t, DispatcherOptions{Destinations: map[string]Deliverer{"d": deliverer}})
	if d.leaseDuration != DefaultLeaseDuration {
		t.Errorf("LeaseDuration = %s, want %s", d.leaseDuration, DefaultLeaseDuration)
	}
	if d.batchSize != DefaultDispatchBatch {
		t.Errorf("BatchSize = %d, want %d", d.batchSize, DefaultDispatchBatch)
	}
	if d.backoffBase != DefaultBackoffBase || d.backoffMax != DefaultBackoffMax {
		t.Errorf("backoff = %s..%s, want %s..%s", d.backoffBase, d.backoffMax, DefaultBackoffBase, DefaultBackoffMax)
	}
	if d.maxAttempts != DefaultMaxAttempts {
		t.Errorf("MaxAttempts = %d, want %d", d.maxAttempts, DefaultMaxAttempts)
	}

	// A non-positive run interval is refused by Run, not silently busy-looped.
	if err := d.Run(context.Background(), 0); !errors.Is(err, ErrInvalidDispatcherConfig) {
		t.Errorf("Run(interval 0) error = %v, want ErrInvalidDispatcherConfig", err)
	}
}

// TestDispatcherRunDeliversUntilCancelled drives the loop: it dispatches
// immediately, keeps going, and returns ctx.Err() when the context is done.
func TestDispatcherRunDeliversUntilCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := newDispatcherFixture(t)
	event := f.appendWithDestination(t, dispatchDestination)

	sink := newIdempotentSink()
	d := f.newDispatcher(t, DispatcherOptions{Destinations: map[string]Deliverer{dispatchDestination: sink}})

	done := make(chan error, 1)
	go func() { done <- d.Run(ctx, 5*time.Millisecond) }()

	deadline := time.Now().Add(3 * time.Second)
	for sink.appliedCount(event.ID+":"+dispatchDestination) == 0 {
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("Run did not deliver the pending row within 3s")
		}
		time.Sleep(2 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Run returned %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after its context was cancelled")
	}
}

// TestDispatcherRunKeepsLoopingAfterRoundError proves the loop's failure policy:
// a round that fails (here: the store is closed underneath it) is logged and
// the loop continues, and only the context stops it. If Run exited on the round
// error it would return the database error instead of context.Canceled.
func TestDispatcherRunKeepsLoopingAfterRoundError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := newDispatcherFixture(t)
	d := f.newDispatcher(t, DispatcherOptions{
		Destinations: map[string]Deliverer{dispatchDestination: &scriptedDeliverer{}},
	})

	done := make(chan error, 1)
	go func() { done <- d.Run(ctx, 5*time.Millisecond) }()

	// Every round from here on fails.
	if err := f.store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	time.Sleep(40 * time.Millisecond) // several failing rounds
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Run returned %v, want context.Canceled (a failed round must not end the loop)", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after its context was cancelled")
	}
}

// ---------------------------------------------------------------------------
// Consumer idempotency (consumer.go)
// ---------------------------------------------------------------------------

// TestMarkConsumedTxIsFirstOnlyOnce is the consumer half of at-least-once
// delivery: the first call reports first=true, a redelivery reports false, and
// the receipt survives a reopen.
func TestMarkConsumedTxIsFirstOnlyOnce(t *testing.T) {
	ctx := context.Background()
	f := newDispatcherFixture(t)
	event := f.appendWithDestination(t, dispatchDestination)
	const consumer = "notification-projection"

	var first, second bool
	err := f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		var err error
		first, err = MarkConsumedTx(ctx, tx, consumer, event.ID)
		return err
	})
	if err != nil {
		t.Fatalf("first MarkConsumedTx: %v", err)
	}
	if !first {
		t.Error("first MarkConsumedTx reported first=false, want true")
	}

	err = f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		var err error
		second, err = MarkConsumedTx(ctx, tx, consumer, event.ID)
		return err
	})
	if err != nil {
		t.Fatalf("second MarkConsumedTx: %v", err)
	}
	if second {
		t.Error("second MarkConsumedTx reported first=true, want false (the receipt already exists)")
	}

	var rows int
	if err := f.store.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM consumer_offsets WHERE consumer = ? AND event_id = ?`, consumer, event.ID).Scan(&rows); err != nil {
		t.Fatalf("count consumer_offsets: %v", err)
	}
	if rows != 1 {
		t.Errorf("consumer_offsets rows = %d, want 1", rows)
	}

	// Another consumer of the same event is a different receipt.
	var other bool
	err = f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		var err error
		other, err = MarkConsumedTx(ctx, tx, "audit-projection", event.ID)
		return err
	})
	if err != nil {
		t.Fatalf("MarkConsumedTx for the second consumer: %v", err)
	}
	if !other {
		t.Error("a second consumer of the same event reported first=false, want true")
	}
}

// TestMarkConsumedTxRollsBackWithItsTransaction is §19.2's "回执与派生状态同事务
// 提交" seen from the failure side: a transaction that records the receipt and
// then fails leaves no receipt, so the redelivery is processed again.
func TestMarkConsumedTxRollsBackWithItsTransaction(t *testing.T) {
	ctx := context.Background()
	f := newDispatcherFixture(t)
	event := f.appendWithDestination(t, dispatchDestination)

	const consumer = "projection-under-test"
	delivered := func() string {
		return readOutboxRow(t, f.store.DB(), event.ID, dispatchDestination).state
	}

	// The derived state is modelled by the outbox row's state: both writes are
	// in one transaction, so both must disappear.
	sentinel := errors.New("the consumer's unit of work failed")
	err := f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		first, err := MarkConsumedTx(ctx, tx, consumer, event.ID)
		if err != nil {
			return err
		}
		if !first {
			t.Error("MarkConsumedTx inside the rolled-back transaction reported first=false, want true")
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE outbox SET state = 'delivered', delivered_at = 1 WHERE event_id = ? AND destination = ?`,
			event.ID, dispatchDestination); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("WithTx error = %v, want the sentinel", err)
	}

	var rows int
	if err := f.store.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM consumer_offsets WHERE consumer = ? AND event_id = ?`, consumer, event.ID).Scan(&rows); err != nil {
		t.Fatalf("count consumer_offsets: %v", err)
	}
	if rows != 0 {
		t.Errorf("consumer_offsets rows after rollback = %d, want 0", rows)
	}
	if got := delivered(); got != "pending" {
		t.Errorf("derived state after rollback = %q, want pending (the two writes roll back together)", got)
	}

	// The redelivery is processed again, and this time it commits.
	var first bool
	err = f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		var err error
		first, err = MarkConsumedTx(ctx, tx, consumer, event.ID)
		return err
	})
	if err != nil {
		t.Fatalf("MarkConsumedTx after the rollback: %v", err)
	}
	if !first {
		t.Error("MarkConsumedTx after a rolled-back receipt reported first=false, want true")
	}
}

// TestMarkConsumedTxConcurrentCallsHaveOneWinner runs many consumers of the same
// (consumer, event) pair at once: exactly one may be told it is first.
func TestMarkConsumedTxConcurrentCallsHaveOneWinner(t *testing.T) {
	ctx := context.Background()
	f := newDispatcherFixture(t)
	event := f.appendWithDestination(t, dispatchDestination)

	const (
		consumer = "contended-consumer"
		callers  = 8
	)
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		first int
		errs  []error
	)
	wg.Add(callers)
	for i := 0; i < callers; i++ {
		go func() {
			defer wg.Done()
			var got bool
			err := f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
				var err error
				got, err = MarkConsumedTx(ctx, tx, consumer, event.ID)
				return err
			})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			if got {
				first++
			}
		}()
	}
	wg.Wait()

	if len(errs) != 0 {
		t.Fatalf("concurrent MarkConsumedTx reported %d errors, want 0: %v", len(errs), errs)
	}
	if first != 1 {
		t.Errorf("%d callers were told they were first, want exactly 1", first)
	}
	var rows int
	if err := f.store.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM consumer_offsets WHERE consumer = ? AND event_id = ?`, consumer, event.ID).Scan(&rows); err != nil {
		t.Fatalf("count consumer_offsets: %v", err)
	}
	if rows != 1 {
		t.Errorf("consumer_offsets rows = %d, want 1", rows)
	}
}

// TestMarkConsumedTxRejectsBadInput pins the fail-closed validation: a blank
// consumer or event id is refused before any SQL runs, and an event that does
// not exist is refused by the foreign key.
func TestMarkConsumedTxRejectsBadInput(t *testing.T) {
	ctx := context.Background()
	f := newDispatcherFixture(t)
	event := f.appendWithDestination(t, dispatchDestination)

	cases := []struct {
		name              string
		consumer, eventID string
	}{
		{"blank consumer", "  ", event.ID},
		{"blank event", "consumer", " "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
				_, err := MarkConsumedTx(ctx, tx, tc.consumer, tc.eventID)
				return err
			})
			if !errors.Is(err, ErrInvalidEvent) {
				t.Errorf("MarkConsumedTx(%s) error = %v, want ErrInvalidEvent", tc.name, err)
			}
		})
	}
	if n := eventRowCount(t, f.store.DB(), "consumer_offsets"); n != 0 {
		t.Errorf("consumer_offsets rows = %d, want 0 after rejected input", n)
	}

	// An unknown event cannot be recorded as consumed.
	err := f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		_, err := MarkConsumedTx(ctx, tx, "consumer", "no-such-event")
		return err
	})
	if err == nil {
		t.Error("MarkConsumedTx accepted an event id that does not exist, want a foreign key refusal")
	}
	if n := eventRowCount(t, f.store.DB(), "consumer_offsets"); n != 0 {
		t.Errorf("consumer_offsets rows = %d, want 0 after the refused insert", n)
	}
}

// ---------------------------------------------------------------------------
// Retention hold (consumer.go)
// ---------------------------------------------------------------------------

// TestRetentionHoldFollowsUndeliveredFacts is the retention guard T12.02 must
// obey: the oldest event of a project that still owes a delivery is the floor
// retention may not pass, and it follows the deliveries as they land.
func TestRetentionHoldFollowsUndeliveredFacts(t *testing.T) {
	ctx := context.Background()
	f := newDispatcherFixture(t)

	// Three events of one project, plus one of another project: only the first
	// project's undelivered facts may hold anything.
	first := f.appendWithDestinationAt(t, time.UnixMilli(1700000004000), dispatchDestination)
	second := f.appendWithDestinationAt(t, time.UnixMilli(1700000004001), dispatchDestination)
	third := f.appendWithDestinationAt(t, time.UnixMilli(1700000004002), dispatchDestination)

	otherStore := f.store
	if err := otherStore.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		if err := UpsertProjectRef(ctx, tx, run.ProjectRef{
			ProjectID:    "p-other",
			SnapshotHash: "sha256:other",
			State:        run.ProjectRefStateActive,
			CapturedAt:   time.UnixMilli(1700000000000),
			VerifiedAt:   time.UnixMilli(1700000000001),
		}); err != nil {
			return err
		}
		_, err := AppendEventTx(ctx, tx, EventInput{
			ProjectID:  "p-other",
			Type:       string(run.EventServerRestart),
			OccurredAt: time.UnixMilli(1700000004003),
			Identity:   eventIdentity("p-other", "system", nil),
			Payload:    []byte(`{"other":true}`),
			// A destination nobody serves, so the dispatcher under test never
			// delivers this row: the assertions below are about which project a
			// hold belongs to, not about what the dispatcher can reach.
			Destinations: []string{"audit:other-project"},
		})
		return err
	}); err != nil {
		t.Fatalf("seed the other project: %v", err)
	}

	seq, held, err := RetentionHold(ctx, f.store.DB(), eventProjectID)
	if err != nil {
		t.Fatalf("RetentionHold: %v", err)
	}
	if !held || seq != first.ProjectSeq {
		t.Errorf("RetentionHold = (%d, %v), want (%d, true): the oldest pending fact is the floor",
			seq, held, first.ProjectSeq)
	}

	// Deliver the first two: the hold moves to the third, which is still
	// pending, and the other project's pending fact is irrelevant here.
	sink := newIdempotentSink()
	d := f.newDispatcher(t, DispatcherOptions{
		Destinations: map[string]Deliverer{dispatchDestination: sink},
		BatchSize:    2,
	})
	if report := mustDispatch(t, d); report.Delivered != 2 {
		t.Fatalf("first round = %+v, want 2 deliveries", report)
	}
	for _, delivered := range []Event{first, second} {
		if got := readOutboxRow(t, f.store.DB(), delivered.ID, dispatchDestination).state; got != "delivered" {
			t.Fatalf("event %s state = %q, want delivered (the batch claims oldest first)", delivered.ID, got)
		}
	}
	if got := readOutboxRow(t, f.store.DB(), third.ID, dispatchDestination).state; got != "pending" {
		t.Fatalf("third event state = %q, want pending (the batch was two rows)", got)
	}
	seq, held, err = RetentionHold(ctx, f.store.DB(), eventProjectID)
	if err != nil {
		t.Fatalf("RetentionHold after two deliveries: %v", err)
	}
	if !held || seq != third.ProjectSeq {
		t.Errorf("RetentionHold = (%d, %v), want (%d, true)", seq, held, third.ProjectSeq)
	}

	// Deliver the last one: the project holds nothing.
	if report := mustDispatch(t, d); report.Delivered != 1 {
		t.Fatalf("second round = %+v, want 1 delivery", report)
	}
	seq, held, err = RetentionHold(ctx, f.store.DB(), eventProjectID)
	if err != nil {
		t.Fatalf("RetentionHold after all deliveries: %v", err)
	}
	if held || seq != 0 {
		t.Errorf("RetentionHold = (%d, %v), want (0, false): every queued event was delivered", seq, held)
	}

	// The other project's pending fact is its own hold: a retention job for
	// p-other may not pass its oldest undelivered event either.
	otherSeq, otherHeld, err := RetentionHold(ctx, f.store.DB(), "p-other")
	if err != nil {
		t.Fatalf("RetentionHold(p-other): %v", err)
	}
	if !otherHeld || otherSeq != 1 {
		t.Errorf("RetentionHold(p-other) = (%d, %v), want (1, true)", otherSeq, otherHeld)
	}

	// A dead letter holds its event just as a pending one does: an abandoned
	// delivery is a fact an operator must still be able to find.
	if _, err := f.store.DB().ExecContext(ctx,
		`UPDATE outbox SET state = 'dead_letter' WHERE event_id = ?`, third.ID); err != nil {
		t.Fatalf("dead-letter the third delivery: %v", err)
	}
	seq, held, err = RetentionHold(ctx, f.store.DB(), eventProjectID)
	if err != nil {
		t.Fatalf("RetentionHold with a dead letter: %v", err)
	}
	if !held || seq != third.ProjectSeq {
		t.Errorf("RetentionHold = (%d, %v), want (%d, true) for the dead letter", seq, held, third.ProjectSeq)
	}

	// A blank project id fails closed: "no hold" is permission to delete.
	if _, _, err := RetentionHold(ctx, f.store.DB(), "  "); !errors.Is(err, ErrInvalidEvent) {
		t.Errorf("RetentionHold(blank project) error = %v, want ErrInvalidEvent", err)
	}

	// A project with no events holds nothing.
	seq, held, err = RetentionHold(ctx, f.store.DB(), "p-never-used")
	if err != nil {
		t.Fatalf("RetentionHold for an empty project: %v", err)
	}
	if held || seq != 0 {
		t.Errorf("RetentionHold for an empty project = (%d, %v), want (0, false)", seq, held)
	}
}
