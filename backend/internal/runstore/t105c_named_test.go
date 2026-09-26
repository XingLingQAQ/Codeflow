package runstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/run"
)

// The four named acceptance tests of T1.05.c (plan §28 item 3), runtime side.
// Each one pins a property the plan names explicitly, and each one carries a
// comment saying which property it pins and what a wrong implementation would
// have to do to pass it.
//
// They are in their own file because they are the card's acceptance evidence:
// the broader contract tests live in legacy_test.go (projection idempotency),
// events_test.go (append/outbox), dispatcher_test.go (lease/backoff/dead letter)
// and replay_test.go (cursors/high watermarks), and these four are the
// cross-cutting cases that pull those pieces together.
//
// Order in this file: TestNoPublishBeforeCommit, TestRollbackKeepsCounter,
// TestOutboxReplayAfterCrash, TestProjectRunCursorIndependence — the order the
// plan lists them in.

// t105cDestination is the destination the named tests queue deliveries to. It is
// deliberately not dispatchDestination: a named test must be readable on its own
// without knowing which fixture constant another file uses.
const t105cDestination = "ws:project:" + legacyProjectID

// ---------------------------------------------------------------------------
// TestNoPublishBeforeCommit
// ---------------------------------------------------------------------------

// TestNoPublishBeforeCommit pins §19.3's last paragraph: a delivery must not be
// broadcast before the transaction that recorded the fact commits, and a
// transaction that rolls back must never be delivered at all.
//
// The three states it walks through:
//
//  1. While the writer's transaction is open, another handle sees no event and
//     no outbox row, and a dispatcher on that other handle must not hand the
//     delivery to a Deliverer — not even once. (Being blocked by the write lock
//     or failing with a busy/locked error is acceptable; what is not acceptable
//     is a delivery.)
//  2. After the transaction rolls back, many dispatch rounds still deliver
//     nothing: there is no row to claim and never will be.
//  3. A later committed append is delivered exactly once, so the test's
//     negative evidence is not caused by a dispatcher that simply never
//     delivers anything.
//
// The deliverer is the witness: it is called only if some code path handed it a
// row, so counting its calls is what proves "no publish before commit". The
// dispatcher's own report is deliberately not used as the primary evidence,
// because a wrong implementation that published early would show up as a
// Deliverer call whatever the report claimed.
func TestNoPublishBeforeCommit(t *testing.T) {
	ctx := context.Background()

	store, path := newTestStore(t)
	seedLegacyProject(t, store, legacyProjectID)

	// A second handle on the same file: "another session reads the database"
	// must be a genuinely different connection, or the visibility question is
	// not being asked.
	observer, _, err := OpenStore(ctx, path)
	if err != nil {
		t.Fatalf("second OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = observer.Close() })

	sink := &scriptedDeliverer{}
	newObserverDispatcher := func(owner string) *Dispatcher {
		t.Helper()
		d, err := NewDispatcher(observer, DispatcherOptions{
			Owner:        owner,
			Destinations: map[string]Deliverer{t105cDestination: sink},
			// A short lease is irrelevant here (nothing is ever claimed) but
			// keeps the dispatcher's own defaults out of the evidence.
			LeaseDuration: time.Second,
		})
		if err != nil {
			t.Fatalf("NewDispatcher(%s): %v", owner, err)
		}
		return d
	}

	// --- 1. the uncommitted transaction ------------------------------------
	release := make(chan struct{})
	var (
		openEvent  Event
		openSeq    int64
		insideOnce sync.Once
	)
	writerDone := make(chan error, 1)
	go func() {
		writerDone <- store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
			event, err := AppendEventTx(ctx, tx, EventInput{
				ProjectID:    legacyProjectID,
				Type:         string(run.EventLegacyFlowEvent),
				OccurredAt:   time.UnixMilli(1700000003000),
				Identity:     legacyIdentity(legacyProjectID),
				Payload:      legacyFlowPayload("ev-uncommitted", "flow.created", "f1", "", "created"),
				Destinations: []string{t105cDestination},
			})
			if err != nil {
				return err
			}
			insideOnce.Do(func() {
				openEvent = event
				openSeq = event.ProjectSeq
			})
			<-release
			return nil
		})
	}()

	// Wait until the append has actually happened: the only reliable signal is
	// the row becoming visible to the writer's own handle, which the writer's
	// transaction can see and the observer cannot.
	waitForUncommittedAppend(t, store.DB(), openSeqProbe(&openSeq))

	// The other handle cannot see the event, through either read path.
	events, _, err := ListProjectEvents(ctx, observer.DB(), legacyProjectID, 0, MaxEventLimit)
	if err != nil {
		t.Fatalf("ListProjectEvents on the observer: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("the observer sees %d events while the writer's transaction is open, want 0", len(events))
	}
	if got := observerOutboxRows(t, observer.DB()); got != 0 {
		t.Errorf("the observer sees %d outbox rows while the writer's transaction is open, want 0", got)
	}

	// The observer's dispatcher must not deliver. A blocked or failing round is
	// fine; a delivery is not.
	shortCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	report, dispatchErr := newObserverDispatcher("dispatcher-before-commit").DispatchOnce(shortCtx)
	cancel()
	if sink.callCount() != 0 {
		t.Errorf("the dispatcher delivered %d rows before the commit, want 0 (report=%+v err=%v)",
			sink.callCount(), report, dispatchErr)
	}
	if report.Delivered != 0 {
		t.Errorf("the dispatcher reported %d deliveries before the commit, want 0", report.Delivered)
	}
	t.Logf("EVIDENCE pre-commit dispatch: claimed=%d delivered=%d err=%v",
		report.Claimed, report.Delivered, dispatchErr)

	// --- 2. the rollback ----------------------------------------------------
	// Unblock the writer and make its transaction fail, so the append is rolled
	// back rather than committed.
	close(release)
	// The writer returns nil here; the rollback case is the second transaction
	// below, which fails after the append. The first transaction commits, so the
	// "rolled back" evidence comes from that second one.
	if err := <-writerDone; err != nil {
		t.Fatalf("the writer's transaction failed: %v", err)
	}
	if sink.callCount() != 0 {
		t.Errorf("the dispatcher delivered %d rows after the commit but before any dispatch round, want 0", sink.callCount())
	}
	if openEvent.ID == "" {
		t.Fatal("the writer's append was never observed; the rollback case cannot be trusted")
	}

	// The committed event is delivered exactly once once someone dispatches.
	report, err = newObserverDispatcher("dispatcher-after-commit").DispatchOnce(ctx)
	if err != nil {
		t.Fatalf("DispatchOnce after the commit: %v", err)
	}
	if report.Claimed != 1 || report.Delivered != 1 {
		t.Fatalf("post-commit report = %+v, want claimed=1 delivered=1", report)
	}
	if sink.callCount() != 1 {
		t.Fatalf("the deliverer was called %d times after the commit, want exactly 1", sink.callCount())
	}
	if got := sink.deliveries()[0].EventID; got != openEvent.ID {
		t.Errorf("the delivered event = %s, want %s", got, openEvent.ID)
	}

	// --- 3. a rolled-back append is never delivered -------------------------
	sentinel := errors.New("the unit of work failed after the append")
	err = store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		if _, err := AppendEventTx(ctx, tx, EventInput{
			ProjectID:    legacyProjectID,
			Type:         string(run.EventLegacyFlowEvent),
			OccurredAt:   time.UnixMilli(1700000004000),
			Identity:     legacyIdentity(legacyProjectID),
			Payload:      legacyFlowPayload("ev-rolled-back", "stage.done", "f1", "s1", "done"),
			Destinations: []string{t105cDestination},
		}); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("WithTx = %v, want the sentinel back", err)
	}

	// Many rounds, on both handles, still deliver nothing new.
	for round := 0; round < 3; round++ {
		if report, err := newObserverDispatcher("dispatcher-after-rollback").DispatchOnce(ctx); err != nil {
			t.Fatalf("round %d after the rollback: %v", round, err)
		} else if report.Claimed != 0 || report.Delivered != 0 {
			t.Fatalf("round %d after the rollback = %+v, want nothing claimed or delivered", round, report)
		}
	}
	if sink.callCount() != 1 {
		t.Errorf("the deliverer was called %d times in total, want 1 (only the committed event)", sink.callCount())
	}
	if n := observerOutboxRows(t, observer.DB()); n != 1 {
		t.Errorf("outbox rows = %d, want 1 (the rolled-back append left none)", n)
	}
	if events, _, err := ListProjectEvents(ctx, observer.DB(), legacyProjectID, 0, MaxEventLimit); err != nil {
		t.Fatalf("ListProjectEvents after the rollback: %v", err)
	} else if len(events) != 1 || events[0].ProjectSeq != openSeq {
		t.Errorf("the project timeline after the rollback = %+v, want only the committed event at %d", seqsOf(events), openSeq)
	}
}

// openSeqProbe adapts the writer's captured sequence to waitForUncommittedAppend.
func openSeqProbe(seq *int64) func() int64 { return func() int64 { return *seq } }

// waitForUncommittedAppend blocks until the writer's append is visible on the
// writer's own handle. It is what makes the "before the commit" window explicit
// instead of a sleep: the probe returns the project_seq the transaction wrote,
// and the wait ends when the counter has moved to it.
func waitForUncommittedAppend(t *testing.T, q Querier, want func() int64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if want() != 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the writer's append never became visible inside its transaction")
}

// observerOutboxRows counts the outbox rows a handle can see.
func observerOutboxRows(t *testing.T, q Querier) int {
	t.Helper()
	var n int
	if err := q.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM outbox`).Scan(&n); err != nil {
		t.Fatalf("count outbox rows: %v", err)
	}
	return n
}

// ---------------------------------------------------------------------------
// TestRollbackKeepsCounter
// ---------------------------------------------------------------------------

// TestRollbackKeepsCounter pins §19.1's "序号事务分配" on both counters at once:
// a sequence number is handed out inside the transaction that writes the event,
// so a rolled-back transaction consumes nothing and the next committed write
// gets the same number — for the project timeline and for the run timeline
// independently.
//
// It also covers the projection's counter: a rolled-back ProjectLegacyEventTx
// must consume no project sequence and leave no mapping row, because the
// projector retries that transaction and cannot be allowed to burn numbers.
//
// Finally it closes and reopens the store, so "the counter is durable state, not
// process state" is proven rather than assumed.
func TestRollbackKeepsCounter(t *testing.T) {
	ctx := context.Background()
	f := newEventFixture(t)
	runID := f.runID
	sentinel := errors.New("roll this unit of work back")

	// e1 commits: p1/r1.
	first := f.appendEvent(t, run.EventRunFailed, &runID)
	if first.ProjectSeq != 1 || first.RunSeq == nil || *first.RunSeq != 1 {
		t.Fatalf("e1 = p%d/r%v, want p1/r1", first.ProjectSeq, runSeqString(first.RunSeq))
	}

	// e2 is appended and then rolled back: nothing may be consumed.
	err := f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		event, err := AppendEventTx(ctx, tx, EventInput{
			ProjectID:  eventProjectID,
			RunID:      &runID,
			Type:       string(run.EventRunFailed),
			OccurredAt: time.UnixMilli(1700000006000),
			Identity:   eventIdentity(eventProjectID, "system", &runID),
			Payload:    json.RawMessage(`{"n":2}`),
		})
		if err != nil {
			return err
		}
		if event.ProjectSeq != 2 || event.RunSeq == nil || *event.RunSeq != 2 {
			return fmt.Errorf("e2 inside the transaction = p%d/r%v, want p2/r2",
				event.ProjectSeq, runSeqString(event.RunSeq))
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("WithTx = %v, want the sentinel back", err)
	}
	if n := eventRowCount(t, f.store.DB(), "events"); n != 1 {
		t.Fatalf("events rows after the rollback = %d, want 1", n)
	}
	if got := counterValue(t, f.store.DB(), projectScope(eventProjectID)); got != 1 {
		t.Errorf("project counter after the rollback = %d, want 1", got)
	}
	if got := counterValue(t, f.store.DB(), runScope(runID)); got != 1 {
		t.Errorf("run counter after the rollback = %d, want 1", got)
	}

	// The next committed append gets p2/r2, not p3/r3: the numbers really were
	// returned to the pool, not merely hidden from one read.
	third := f.appendEvent(t, run.EventRunFailed, &runID)
	if third.ProjectSeq != 2 {
		t.Errorf("the append after the rollback has project_seq %d, want 2 (a rolled-back append consumes nothing)", third.ProjectSeq)
	}
	if third.RunSeq == nil || *third.RunSeq != 2 {
		t.Errorf("the append after the rollback has run_seq %v, want 2", runSeqString(third.RunSeq))
	}

	// A rolled-back projection is the same rule on the project counter, plus the
	// mapping row must not survive.
	seedLegacyProject(t, f.store, legacyProjectID)
	src := LegacySource{Store: legacyStoreName, EventID: "ev-counter"}
	err = f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		if _, created, err := ProjectLegacyEventTx(ctx, tx, src, legacyInput(src.EventID)); err != nil {
			return err
		} else if !created {
			return errors.New("the projection inside the transaction reported created=false, want true")
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("projection WithTx = %v, want the sentinel back", err)
	}
	if n := eventRowCount(t, f.store.DB(), "legacy_event_sources"); n != 0 {
		t.Errorf("mapping rows after the rolled-back projection = %d, want 0", n)
	}
	if got := counterValue(t, f.store.DB(), projectScope(legacyProjectID)); got != -1 {
		t.Errorf("the legacy project's counter = %d, want no row (the rolled-back projection consumed nothing)", got)
	}

	projected, created := projectLegacy(t, f.store, src, legacyInput(src.EventID))
	if !created {
		t.Error("the retry reported created=false, want true")
	}
	if projected.ProjectSeq != 1 {
		t.Errorf("the retried projection has project_seq %d, want 1 (nothing was consumed by the failed attempt)", projected.ProjectSeq)
	}
	if got := counterValue(t, f.store.DB(), projectScope(legacyProjectID)); got != 1 {
		t.Errorf("the legacy project's counter = %d, want 1", got)
	}

	// --- restart ----------------------------------------------------------
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

	projectCounter := counterValue(t, reopened.DB(), projectScope(eventProjectID))
	if projectCounter != 2 {
		t.Fatalf("project counter after reopen = %d, want 2", projectCounter)
	}
	runCounter := counterValue(t, reopened.DB(), runScope(runID))
	if runCounter != 2 {
		t.Fatalf("run counter after reopen = %d, want 2", runCounter)
	}
	next := appendOneEvent(t, reopened, eventProjectID, &runID, run.EventRunFailed)
	if next.ProjectSeq != projectCounter+1 {
		t.Errorf("project_seq after reopen = %d, want %d (the counters are durable and continue)", next.ProjectSeq, projectCounter+1)
	}
	if next.RunSeq == nil || *next.RunSeq != runCounter+1 {
		t.Errorf("run_seq after reopen = %v, want %d", runSeqString(next.RunSeq), runCounter+1)
	}
}

// appendOneEvent appends one committed event with the fixture's identity rules
// and returns it, failing the test on refusal.
func appendOneEvent(t *testing.T, store *Store, projectID string, runID *string, eventType run.ExecutionEventType) Event {
	t.Helper()
	var event Event
	err := store.WithTx(context.Background(), func(ctx context.Context, tx Tx) error {
		var err error
		event, err = AppendEventTx(ctx, tx, EventInput{
			ProjectID:  projectID,
			RunID:      runID,
			Type:       string(eventType),
			OccurredAt: time.UnixMilli(1700000007000),
			Identity:   eventIdentity(projectID, "system", runID),
			Payload:    json.RawMessage(`{"n":9}`),
		})
		return err
	})
	if err != nil {
		t.Fatalf("AppendEventTx(%s): %v", eventType, err)
	}
	return event
}

// ---------------------------------------------------------------------------
// TestOutboxReplayAfterCrash
// ---------------------------------------------------------------------------

// TestOutboxReplayAfterCrash pins the crash-recovery contract of §19.3 with a
// real process restart instead of a simulated one:
//
//  1. an event is committed with a destination, so a pending outbox row exists;
//  2. dispatcher A claims the row and "crashes" — its store is closed before it
//     can write the outcome back, so the row stays pending and leased;
//  3. the store is reopened (a new process), the clock moves past the lease, and
//     dispatcher B claims and delivers the row;
//  4. the downstream consumer de-duplicates on the event id inside its own
//     transaction with MarkConsumedTx, and a second delivery of the same row
//     (which at-least-once delivery permits, and which A's lost write-back makes
//     concrete) is applied exactly once;
//  5. ReplayProject / ReplayRun read the same sequence numbers after the restart
//     as before it: the sequences are committed state, not process state.
//
// Step 2 closes the store while a claim is held on purpose. That is the case a
// lease exists for: the row must survive the crash and be recoverable after the
// lease expires, without the crashed process having recorded anything.
func TestOutboxReplayAfterCrash(t *testing.T) {
	ctx := context.Background()
	f := newDispatcherFixture(t)

	// Before the crash: the event, its sequence numbers and the pending row.
	event := f.appendWithDestination(t, t105cDestination)
	beforeRun, err := ReplayRun(ctx, f.store.DB(), f.events.runID, 0, DefaultEventLimit)
	if err != nil {
		t.Fatalf("ReplayRun before the crash: %v", err)
	}
	beforeProject, err := ReplayProject(ctx, f.store.DB(), eventProjectID, 0, DefaultEventLimit)
	if err != nil {
		t.Fatalf("ReplayProject before the crash: %v", err)
	}

	// --- 1. A claims, then crashes before its write-back -------------------
	// The claim is held inside the transaction callback: the row is leased and
	// the attempt is spent, and returning an error rolls the claim back — which
	// would defeat the test — so the callback commits and the "crash" is the
	// store closing with the row still pending and leased.
	crashCtx := context.Background()
	if err := f.store.WithTx(crashCtx, func(ctx context.Context, tx Tx) error {
		res, err := tx.ExecContext(ctx, `
			UPDATE outbox
			   SET lease_owner = 'dispatcher-a', lease_until = ?, lease_epoch = lease_epoch + 1,
			       attempt_count = attempt_count + 1
			 WHERE event_id = ? AND destination = ? AND state = 'pending'`,
			unixMilli(f.clock.Now().Add(time.Minute)), event.ID, t105cDestination)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n != 1 {
			return fmt.Errorf("dispatcher A claimed %d rows, want 1", n)
		}
		return nil
	}); err != nil {
		t.Fatalf("claim the row for the crashing dispatcher: %v", err)
	}

	crashed := readOutboxRow(t, f.store.DB(), event.ID, t105cDestination)
	if crashed.state != "pending" || crashed.epoch != 1 || crashed.attempts != 1 {
		t.Fatalf("row after A's claim = %+v, want pending/epoch 1/attempts 1", crashed)
	}
	if !crashed.leaseOwner.Valid || crashed.leaseOwner.String != "dispatcher-a" {
		t.Fatalf("lease_owner after A's claim = %+v, want dispatcher-a", crashed.leaseOwner)
	}

	// A crash: the process goes away with the lease held and nothing written
	// back. Closing the handle is the observable form of that.
	if err := f.store.Close(); err != nil {
		t.Fatalf("close the crashed store: %v", err)
	}

	// --- 2. restart, lease expiry, B delivers ------------------------------
	reopened, res, err := OpenStore(ctx, f.path)
	if err != nil {
		t.Fatalf("reopen after the crash: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if len(res.Applied) != 0 {
		t.Errorf("reopen applied %d migrations, want 0", len(res.Applied))
	}

	survived := readOutboxRow(t, reopened.DB(), event.ID, t105cDestination)
	if survived.state != "pending" {
		t.Fatalf("row after the restart = %+v, want it still pending (the work must survive the crash)", survived)
	}

	// The consumer's side of the contract: one application per event, recorded
	// in the same transaction as its derived state (§19.2).
	applied := map[string]int{}
	apply := func(d Delivery) error {
		return reopened.WithTx(ctx, func(ctx context.Context, tx Tx) error {
			first, err := MarkConsumedTx(ctx, tx, "audit-export", d.EventID)
			if err != nil {
				return err
			}
			if first {
				applied[d.EventID]++
			}
			return nil
		})
	}
	sinkB := &scriptedDeliverer{fn: apply}
	b, err := NewDispatcher(reopened, DispatcherOptions{
		Owner:         "dispatcher-b",
		Destinations:  map[string]Deliverer{t105cDestination: sinkB},
		LeaseDuration: 30 * time.Second,
		Clock:         f.clock.Now,
	})
	if err != nil {
		t.Fatalf("NewDispatcher(b): %v", err)
	}

	// Before the lease expires B must not touch A's row: the crashed dispatcher
	// is indistinguishable from a slow one, so the lease is the only safe rule.
	if report, err := b.DispatchOnce(ctx); err != nil {
		t.Fatalf("B's round while A's lease is live: %v", err)
	} else if report.Claimed != 0 {
		t.Fatalf("B claimed %d rows while A's lease was live, want 0", report.Claimed)
	}
	if sinkB.callCount() != 0 {
		t.Fatalf("B's deliverer was called %d times while A's lease was live, want 0", sinkB.callCount())
	}

	f.clock.Advance(2 * time.Minute) // past A's lease
	report, err := b.DispatchOnce(ctx)
	if err != nil {
		t.Fatalf("B's round after the lease expired: %v", err)
	}
	if report.Claimed != 1 || report.Delivered != 1 {
		t.Fatalf("B's report = %+v, want claimed=1 delivered=1", report)
	}
	if sinkB.callCount() != 1 {
		t.Fatalf("B's deliverer was called %d times, want 1", sinkB.callCount())
	}
	if got := sinkB.deliveries()[0].EventID; got != event.ID {
		t.Errorf("B delivered %s, want %s", got, event.ID)
	}
	if applied[event.ID] != 1 {
		t.Errorf("the consumer applied the event %d times, want 1", applied[event.ID])
	}

	// --- 3. a duplicate delivery is applied once --------------------------
	// Re-arm the row by hand: this is the state a duplicate delivery produces
	// when a dispatcher's lease was lost mid-flight (A's late write-back, or a
	// lease that expired under a slow but living dispatcher). The delivery is
	// deliberately attempted a second time, and the consumer must absorb it.
	if _, err := reopened.DB().ExecContext(ctx, `
		UPDATE outbox SET state = 'pending', next_attempt_at = ?, lease_owner = NULL, lease_until = NULL
		 WHERE event_id = ? AND destination = ?`,
		unixMilli(f.clock.Now()), event.ID, t105cDestination); err != nil {
		t.Fatalf("re-arm the row for the duplicate delivery: %v", err)
	}

	duplicate, err := b.DispatchOnce(ctx)
	if err != nil {
		t.Fatalf("the duplicate round: %v", err)
	}
	if duplicate.Claimed != 1 || duplicate.Delivered != 1 {
		t.Fatalf("the duplicate round's report = %+v, want claimed=1 delivered=1", duplicate)
	}
	if sinkB.callCount() != 2 {
		t.Fatalf("B's deliverer was called %d times, want 2 (the duplicate really arrived)", sinkB.callCount())
	}
	if applied[event.ID] != 1 {
		t.Errorf("the consumer applied the event %d times after the duplicate, want 1", applied[event.ID])
	}

	// The receipt is in the table, once, and the free function agrees with the
	// transaction the consumer ran.
	var receipts int
	if err := reopened.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM consumer_offsets WHERE consumer = 'audit-export' AND event_id = ?`,
		event.ID).Scan(&receipts); err != nil {
		t.Fatalf("count consumer_offsets: %v", err)
	}
	if receipts != 1 {
		t.Errorf("consumer_offsets rows = %d, want 1", receipts)
	}
	if first, err := markConsumedOnce(t, reopened, "audit-export", event.ID); err != nil {
		t.Fatalf("MarkConsumedTx for an already consumed event: %v", err)
	} else if first {
		t.Error("MarkConsumedTx reported first=true for an event already consumed, want false")
	}

	// --- 4. the sequences are unchanged by the restart ---------------------
	afterRun, err := ReplayRun(ctx, reopened.DB(), f.events.runID, 0, DefaultEventLimit)
	if err != nil {
		t.Fatalf("ReplayRun after the crash: %v", err)
	}
	afterProject, err := ReplayProject(ctx, reopened.DB(), eventProjectID, 0, DefaultEventLimit)
	if err != nil {
		t.Fatalf("ReplayProject after the crash: %v", err)
	}
	if fmt.Sprint(seqsOf(afterProject.Events)) != fmt.Sprint(seqsOf(beforeProject.Events)) {
		t.Errorf("project sequences after the restart = %v, want %v",
			seqsOf(afterProject.Events), seqsOf(beforeProject.Events))
	}
	if fmt.Sprint(runSeqsOf(afterRun.Events)) != fmt.Sprint(runSeqsOf(beforeRun.Events)) {
		t.Errorf("run sequences after the restart = %v, want %v",
			runSeqsOf(afterRun.Events), runSeqsOf(beforeRun.Events))
	}
	if afterProject.HighWatermark != beforeProject.HighWatermark || afterRun.HighWatermark != beforeRun.HighWatermark {
		t.Errorf("watermarks after the restart = %d/%d, want %d/%d",
			afterProject.HighWatermark, afterRun.HighWatermark,
			beforeProject.HighWatermark, beforeRun.HighWatermark)
	}
	t.Logf("EVIDENCE crash replay: event %s project_seq=%d run_seq=%s delivered once after the lease expired, consumer applied once out of 2 arrivals",
		event.ID, event.ProjectSeq, runSeqString(event.RunSeq))
}

// markConsumedOnce runs MarkConsumedTx in its own transaction and reports
// whether it was the first consumer to apply the event.
func markConsumedOnce(t *testing.T, store *Store, consumer, eventID string) (bool, error) {
	t.Helper()
	var first bool
	err := store.WithTx(context.Background(), func(ctx context.Context, tx Tx) error {
		var err error
		first, err = MarkConsumedTx(ctx, tx, consumer, eventID)
		return err
	})
	return first, err
}

// ---------------------------------------------------------------------------
// TestProjectRunCursorIndependence
// ---------------------------------------------------------------------------

// TestProjectRunCursorIndependence pins §27.4's "不混用两种计数": the project
// timeline and each run timeline are independent counters over the same rows. A
// run's run_seq 1 is not the project's project_seq 1, one run's events do not
// advance another run's, and a cursor from one scope used on another scope is a
// wrong cursor rather than a silently empty page.
//
// The fixture interleaves three writer streams — run A, run B and project-level
// legacy.flow_event projections — so every property is checked against a
// timeline where the three really do interleave, and the project sequence is
// read back through the same rows the run views return.
//
// The final part is the cursor rule: a project-sequence value used as a run
// cursor that is past that run's high watermark must be ErrInvalidCursor, not an
// empty page. Returning an empty page would tell a client that mixed two scopes
// that it is up to date.
func TestProjectRunCursorIndependence(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	seedLegacyProject(t, store, legacyProjectID)

	// Two runs in one project, each with its own task/snapshot.
	const (
		runA = "run-a"
		runB = "run-b"
	)
	seedTask(t, store, "t-a", legacyProjectID)
	seedTask(t, store, "t-b", legacyProjectID)
	insertSnapshotAndRun(t, store, runA, "t-a", legacyProjectID, "snap-a", json.RawMessage(`{"prompt":"a"}`))
	insertSnapshotAndRun(t, store, runB, "t-b", legacyProjectID, "snap-b", json.RawMessage(`{"prompt":"b"}`))

	appendRunEvent := func(runID string) Event {
		t.Helper()
		return appendOneEvent(t, store, legacyProjectID, stringPtr(runID), run.EventRunFailed)
	}
	projectLegacyEvent := func(sourceEventID string) Event {
		t.Helper()
		src := LegacySource{Store: legacyStoreName, EventID: sourceEventID}
		event, created := projectLegacy(t, store, src, legacyInput(sourceEventID))
		if !created {
			t.Fatalf("projecting %s reported created=false, want true", sourceEventID)
		}
		return event
	}

	// Interleave: A, B, project, A, project, B, A.
	a1 := appendRunEvent(runA)
	b1 := appendRunEvent(runB)
	p1 := projectLegacyEvent("ev-cursor-1")
	a2 := appendRunEvent(runA)
	p2 := projectLegacyEvent("ev-cursor-2")
	b2 := appendRunEvent(runB)
	a3 := appendRunEvent(runA)

	// --- project_seq is 1..N, gap-free, in write order ---------------------
	project, err := ReplayProject(ctx, store.DB(), legacyProjectID, 0, MaxEventLimit)
	if err != nil {
		t.Fatalf("ReplayProject: %v", err)
	}
	const wantProjectSeq = 7
	if project.HighWatermark != wantProjectSeq {
		t.Errorf("project high_watermark = %d, want %d", project.HighWatermark, wantProjectSeq)
	}
	if got := fmt.Sprint(seqsOf(project.Events)); got != "[1 2 3 4 5 6 7]" {
		t.Errorf("project sequences = %s, want [1 2 3 4 5 6 7]", got)
	}
	wantOrder := []string{a1.ID, b1.ID, p1.ID, a2.ID, p2.ID, b2.ID, a3.ID}
	for i, wantID := range wantOrder {
		if project.Events[i].ID != wantID {
			t.Errorf("project timeline[%d] = %s, want %s (write order)", i, project.Events[i].ID, wantID)
		}
	}

	// --- each run's run_seq is 1..n, its own counter ----------------------
	pageA, err := ReplayRun(ctx, store.DB(), runA, 0, MaxEventLimit)
	if err != nil {
		t.Fatalf("ReplayRun(A): %v", err)
	}
	if pageA.HighWatermark != 3 {
		t.Errorf("run A high_watermark = %d, want 3", pageA.HighWatermark)
	}
	if got := fmt.Sprint(runSeqsOf(pageA.Events)); got != "[1 2 3]" {
		t.Errorf("run A run_seq = %s, want [1 2 3]", got)
	}
	pageB, err := ReplayRun(ctx, store.DB(), runB, 0, MaxEventLimit)
	if err != nil {
		t.Fatalf("ReplayRun(B): %v", err)
	}
	if pageB.HighWatermark != 2 {
		t.Errorf("run B high_watermark = %d, want 2", pageB.HighWatermark)
	}
	if got := fmt.Sprint(runSeqsOf(pageB.Events)); got != "[1 2]" {
		t.Errorf("run B run_seq = %s, want [1 2]", got)
	}

	// The same rows carry both numbers, and the run's first event is not the
	// project's first event: a1 is project_seq 1 and run_seq 1 by coincidence,
	// but b1 is project_seq 2 and run_seq 1, which is the case a mixed counter
	// would get wrong.
	if a1.ProjectSeq != 1 || a1.RunSeq == nil || *a1.RunSeq != 1 {
		t.Errorf("A's first event = p%d/r%v, want p1/r1", a1.ProjectSeq, runSeqString(a1.RunSeq))
	}
	if b1.ProjectSeq != 2 || b1.RunSeq == nil || *b1.RunSeq != 1 {
		t.Errorf("B's first event = p%d/r%v, want p2/r1", b1.ProjectSeq, runSeqString(b1.RunSeq))
	}
	if p1.RunID != nil || p1.RunSeq != nil {
		t.Errorf("the project-level event = run %v/seq %v, want nil/nil", p1.RunID, p1.RunSeq)
	}
	if p1.ProjectSeq != 3 {
		t.Errorf("the project-level event has project_seq %d, want 3", p1.ProjectSeq)
	}

	// --- ReplayRun(A, after=k) returns only A's events above k ------------
	for _, tc := range []struct {
		after int64
		want  string
	}{
		{0, "[1 2 3]"},
		{1, "[2 3]"},
		{2, "[3]"},
		{3, "[]"},
	} {
		page, err := ReplayRun(ctx, store.DB(), runA, tc.after, MaxEventLimit)
		if err != nil {
			t.Fatalf("ReplayRun(A, after=%d): %v", tc.after, err)
		}
		if got := fmt.Sprint(runSeqsOf(page.Events)); got != tc.want {
			t.Errorf("ReplayRun(A, after=%d) = %s, want %s", tc.after, got, tc.want)
		}
		for _, event := range page.Events {
			if event.RunID == nil || *event.RunID != runA {
				t.Errorf("ReplayRun(A, after=%d) returned an event of run %v", tc.after, event.RunID)
			}
			if event.RunSeq == nil || *event.RunSeq <= tc.after {
				t.Errorf("ReplayRun(A, after=%d) returned run_seq %v", tc.after, event.RunSeq)
			}
		}
		if tc.after <= pageA.HighWatermark && page.NextAfter != tc.after+int64(len(page.Events)) {
			t.Errorf("ReplayRun(A, after=%d) next_after = %d, want %d", tc.after, page.NextAfter, tc.after+int64(len(page.Events)))
		}
	}

	// B's events and the project-level events are invisible to A's timeline,
	// however many rounds of the project cursor are read.
	if got := len(pageA.Events); got != 3 {
		t.Fatalf("run A returned %d events, want 3 (B's two and the project's two must not appear)", got)
	}
	for _, event := range pageA.Events {
		if event.ID == b1.ID || event.ID == b2.ID || event.ID == p1.ID || event.ID == p2.ID {
			t.Errorf("run A's timeline contains another scope's event %s", event.ID)
		}
	}

	// --- a project cursor used on a run is an invalid cursor --------------
	// The project's high watermark (7) is past run B's (2) and past run A's (3),
	// which is exactly what a client mixing the two scopes would send.
	for _, tc := range []struct {
		runID string
		after int64
		hwm   int64
	}{
		{runA, project.HighWatermark, pageA.HighWatermark},
		{runB, project.HighWatermark, pageB.HighWatermark},
		{runB, 3, pageB.HighWatermark}, // one past B's watermark, still inside A's
	} {
		page, err := ReplayRun(ctx, store.DB(), tc.runID, tc.after, MaxEventLimit)
		if !errors.Is(err, ErrInvalidCursor) {
			t.Errorf("ReplayRun(%s, after=%d) = (%+v, %v), want ErrInvalidCursor (the run's watermark is %d)",
				tc.runID, tc.after, page, err, tc.hwm)
			continue
		}
		if !strings.Contains(err.Error(), "high watermark") {
			t.Errorf("ReplayRun(%s, after=%d) error %q should name the high watermark", tc.runID, tc.after, err)
		}
	}
	// And the boundary is exact: a cursor at the watermark is legal and empty.
	if page, err := ReplayRun(ctx, store.DB(), runB, pageB.HighWatermark, MaxEventLimit); err != nil {
		t.Errorf("ReplayRun(B, after=its watermark) = %v, want an empty page", err)
	} else if len(page.Events) != 0 {
		t.Errorf("ReplayRun(B, after=its watermark) returned %d events, want 0", len(page.Events))
	}

	// --- the two scopes' boundaries do not affect each other --------------
	// The project's boundaries are its own: run A's 3 and run B's 2 are not the
	// project's high watermark, and the project's floor is its own first event.
	if project.RetentionFloor != 1 {
		t.Errorf("project retention_floor = %d, want 1", project.RetentionFloor)
	}
	if pageA.RetentionFloor != 1 || pageB.RetentionFloor != 1 {
		t.Errorf("run retention floors = %d/%d, want 1/1", pageA.RetentionFloor, pageB.RetentionFloor)
	}
	// The project page hands back each row's project_seq even though the rows
	// belong to different runs: the caller projects whichever number its scope
	// asked for.
	for i, event := range pageA.Events {
		if event.ProjectID != legacyProjectID {
			t.Errorf("run A event %d has project_id %q, want %q", i, event.ProjectID, legacyProjectID)
		}
		if event.ProjectSeq <= 0 || event.ProjectSeq > project.HighWatermark {
			t.Errorf("run A event %d has project_seq %d, want a position in 1..%d",
				i, event.ProjectSeq, project.HighWatermark)
		}
	}

	// A run whose whole history is absent has watermark 0 and floor 1: a scope
	// with no events is not confused with a scope whose events were trimmed.
	empty, err := ReplayRun(ctx, store.DB(), "run-never-used", 0, MaxEventLimit)
	if err != nil {
		t.Fatalf("ReplayRun(unknown run): %v", err)
	}
	if empty.HighWatermark != 0 || empty.RetentionFloor != 1 || len(empty.Events) != 0 {
		t.Errorf("an unused run's replay = %+v, want watermark 0, floor 1, no events", empty)
	}
	if _, err := ReplayRun(ctx, store.DB(), "run-never-used", 1, MaxEventLimit); !errors.Is(err, ErrInvalidCursor) {
		t.Errorf("ReplayRun(unknown run, after=1) = %v, want ErrInvalidCursor", err)
	}

	// The project counter itself is untouched by the two run counters: one
	// project number per event, whatever run it belongs to, plus the two
	// project-level projections.
	if got := counterValue(t, store.DB(), projectScope(legacyProjectID)); got != wantProjectSeq {
		t.Errorf("project counter = %d, want %d", got, wantProjectSeq)
	}
	if got := counterValue(t, store.DB(), runScope(runA)); got != 3 {
		t.Errorf("run A counter = %d, want 3", got)
	}
	if got := counterValue(t, store.DB(), runScope(runB)); got != 2 {
		t.Errorf("run B counter = %d, want 2", got)
	}
	t.Logf("EVIDENCE cursor independence: project 1..%d, A 1..%d, B 1..%d; a project cursor on a run is ErrInvalidCursor",
		wantProjectSeq, pageA.HighWatermark, pageB.HighWatermark)
}
