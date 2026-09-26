package runstore

// The four named acceptance tests of T1.02.c (plan §28 item 3; §29.2 T1.02 row:
// "c 回收 G(run,runstore)、GR(run)"), in the plan's order:
//
//	TestTerminalRunCannotReopen, TestCancelFinishRace,
//	TestRetryUsesNewIdentity, TestQueuedEventNoAttempt
//
// §28 forbids these tests from only comparing enum strings. Every assertion
// below therefore drives the real store over a real file database
// (t.TempDir()) through TransitionRunTx / RetryRunTx and checks *behaviour*:
// the stored row, the event rows, the sequence counters, the outbox rows, the
// attempt rows, and the canonical identity/payload bytes the store actually
// wrote. Where a property can also be stated as "the §21.1 table says so", the
// test calls run.Decide or run.RequiredIdentity as well — but only alongside
// the store-level assertion, never instead of it.
//
// They live in their own file because they are the card's acceptance evidence.
// The broader contract tests stay where they are: transition_test.go (the CAS,
// the per-row state events, rollback, restart), retry_test.go (the retry
// refusals and the task CAS) and acceptance_test.go. The fixtures and helpers
// are the ones those files already define — newTransitionFixture,
// transitionFixture.walk/transition/mustTransition/current/events,
// transitionInput, attemptPtr, agentRevisionPtr, newRetryFixture, retryInput,
// mustRetry, seedTask, insertSnapshotAndRun, eventRowCount, counterValue,
// projectScope, runScope, equalRun, equalBudget, equalStringPtr, stringPtr,
// stringPtrString, assertPayloadString, payloadKeyFromStatus and
// payloadKeyExpectedTerminal — and nothing here redefines them.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/codeflow/backend/internal/run"
)

// ---------------------------------------------------------------------------
// T1.02.c helpers
// ---------------------------------------------------------------------------

// t102cSnapshot is everything about one Run that a refused or accepted
// transition may move: the row itself, the rows of the tables a state event
// touches (events, outbox, attempts) and the two sequence counters. Comparing a
// whole snapshot is what makes "nothing was written" checkable without trusting
// the error the store returned.
type t102cSnapshot struct {
	row        run.Run
	events     int
	outbox     int
	attempts   int
	projectSeq int64
	runSeq     int64
}

// t102cSnapshotOf reads the snapshot through the pool, so it sees only
// committed state.
func t102cSnapshotOf(t *testing.T, store *Store, runID string) t102cSnapshot {
	t.Helper()
	ctx := context.Background()
	row, err := GetRun(ctx, store.DB(), runID)
	if err != nil {
		t.Fatalf("GetRun(%s): %v", runID, err)
	}
	var attempts int
	if err := store.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM attempts WHERE run_id = ?`, runID).Scan(&attempts); err != nil {
		t.Fatalf("count attempts of %s: %v", runID, err)
	}
	return t102cSnapshot{
		row:        row,
		events:     eventRowCount(t, store.DB(), "events"),
		outbox:     eventRowCount(t, store.DB(), "outbox"),
		attempts:   attempts,
		projectSeq: counterValue(t, store.DB(), projectScope(row.ProjectID)),
		runSeq:     counterValue(t, store.DB(), runScope(runID)),
	}
}

// t102cAssertUnchanged fails when any part of the snapshot moved. what names the
// operation under test, so a failure says which trigger wrote something it
// should not have.
func t102cAssertUnchanged(t *testing.T, store *Store, want t102cSnapshot, what string) {
	t.Helper()
	got := t102cSnapshotOf(t, store, want.row.ID)
	if !equalRun(got.row, want.row) {
		t.Errorf("%s: run row changed:\n before %+v\n after  %+v", what, want.row, got.row)
	}
	if got.events != want.events {
		t.Errorf("%s: events = %d, want %d", what, got.events, want.events)
	}
	if got.outbox != want.outbox {
		t.Errorf("%s: outbox rows = %d, want %d", what, got.outbox, want.outbox)
	}
	if got.attempts != want.attempts {
		t.Errorf("%s: attempts = %d, want %d", what, got.attempts, want.attempts)
	}
	if got.projectSeq != want.projectSeq {
		t.Errorf("%s: project counter = %d, want %d (a refused transition consumes no sequence)",
			what, got.projectSeq, want.projectSeq)
	}
	if got.runSeq != want.runSeq {
		t.Errorf("%s: run counter = %d, want %d (a refused transition consumes no sequence)",
			what, got.runSeq, want.runSeq)
	}
}

// t102cRunTx runs one TransitionRunTx in its own transaction against any Store,
// filling in the fixture's instant and actor when the caller left them zero. It
// exists so a test can drive a *second* handle over the same file (the race) and
// a reopened store without borrowing a fixture's own methods.
func t102cRunTx(store *Store, in TransitionInput) (TransitionResult, error) {
	if in.At.IsZero() {
		in.At = transitionAt
	}
	if in.Actor.ID == "" {
		in.Actor = systemActor
	}
	var result TransitionResult
	err := store.WithTx(context.Background(), func(ctx context.Context, tx Tx) error {
		var err error
		result, err = TransitionRunTx(ctx, tx, in)
		return err
	})
	return result, err
}

// t102cDriveToRunning takes a freshly inserted queued Run to running through the
// §21.1 table (queued --scheduler.claimed--> starting --process.started-->
// running) and returns the stored row.
func t102cDriveToRunning(t *testing.T, store *Store, runID string) run.Run {
	t.Helper()
	claimed, err := t102cRunTx(store, TransitionInput{
		RunID: runID, ExpectedRevision: 1, ExpectedStatus: run.RunStatusQueued,
		Trigger: run.EventTrigger(string(run.EventSchedulerClaimed)),
	})
	if err != nil {
		t.Fatalf("claim %s: %v", runID, err)
	}
	started, err := t102cRunTx(store, TransitionInput{
		RunID: runID, ExpectedRevision: claimed.Run.Revision, ExpectedStatus: run.RunStatusStarting,
		Trigger:   run.EventTrigger(string(run.EventProcessStarted)),
		AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr(),
	})
	if err != nil {
		t.Fatalf("start %s: %v", runID, err)
	}
	if started.Run.Status != run.RunStatusRunning {
		t.Fatalf("driving %s to running produced %s", runID, started.Run.Status)
	}
	return started.Run
}

// t102cTerminal drives a fresh fixture's Run into one of the four terminal
// states through the real transition path — never by writing the status column
// — and returns the fixture and the stored terminal row. A fixture whose row
// cannot be produced by the store is not evidence about the store.
func t102cTerminal(t *testing.T, status run.RunStatus) (*transitionFixture, run.Run) {
	t.Helper()
	f := newTransitionFixture(t)
	switch status {
	case run.RunStatusCompleted:
		f.walk(
			f.transitionInput(0, "", run.EventTrigger(string(run.EventSchedulerClaimed))),
			TransitionInput{Trigger: run.EventTrigger(string(run.EventProcessStarted)), At: transitionAt, Actor: systemActor,
				AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr()},
			TransitionInput{Trigger: run.ProcessExitedTrigger(0), At: transitionAt, Actor: systemActor,
				AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr()},
		)
	case run.RunStatusFailed:
		f.walk(
			f.transitionInput(0, "", run.EventTrigger(string(run.EventSchedulerClaimed))),
			TransitionInput{Trigger: run.EventTrigger(string(run.EventProcessStarted)), At: transitionAt, Actor: systemActor,
				AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr()},
			TransitionInput{Trigger: run.ProcessExitedTrigger(1), At: transitionAt, Actor: systemActor,
				AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr()},
		)
	case run.RunStatusCancelled:
		// A queued Run is cancelled without ever having an attempt (§28).
		f.walk(
			f.transitionInput(0, "", run.CommandTrigger(run.CommandRunCancel)),
			TransitionInput{Trigger: run.ConclusionTrigger(run.ConclusionCleaned, run.RunStatusCancelled),
				At: transitionAt, Actor: systemActor},
		)
	case run.RunStatusExpired:
		f.walk(
			f.transitionInput(0, "", run.CommandTrigger(run.CommandHardDeadline)),
			TransitionInput{Trigger: run.ConclusionTrigger(run.ConclusionCleaned, run.RunStatusExpired),
				At: transitionAt, Actor: systemActor},
		)
	default:
		t.Fatalf("t102cTerminal: %s is not one of the four terminal run statuses", status)
	}
	stored := f.current()
	if stored.Status != status {
		t.Fatalf("driving the run to %s produced %s", status, stored.Status)
	}
	if stored.FinishedAt == nil {
		t.Fatalf("the %s run has no finished_at; a terminal row must carry one", status)
	}
	return f, stored
}

// ---------------------------------------------------------------------------
// TestTerminalRunCannotReopen
// ---------------------------------------------------------------------------

// TestTerminalRunCannotReopen pins §21.1's irreversibility and §27.2.5/§27.2.6's
// "输方读取当前状态，不反向复活终态": once a Run is completed, failed, cancelled
// or expired, no trigger the state machine speaks — the whole
// run.AllTriggers() vocabulary — may move it, and a restart must not change
// that.
//
// What is asserted, per terminal state and per trigger:
//
//   - the store refuses with ErrInvalidTransition that wraps the frozen
//     *run.TransitionError naming the terminal status it refused from, and the
//     refusal is *not* a *RevisionConflictError: the expected revision and
//     status handed in are the stored ones, so only the §21.1 table can be
//     refusing. run.Decide is called first to prove the table refuses this pair
//     at all (a store that only refused because of a lost CAS would fail there).
//   - nothing moved: the row (status, revision, finished_at, every frozen pin),
//     the event count, the outbox count, the attempt count and both sequence
//     counters are read back through SQL and compared field by field.
//   - completed --merge--> completed is the one legal row from a terminal state,
//     and it is not a reopening: it stays completed, writes no Run state event
//     and consumes no sequence. Every other terminal state refuses merge too.
//   - after closing and reopening the database file, the terminal row is still
//     terminal and still refuses run.cancel, the hard deadline,
//     process.exited(0) and the cleaned conclusion with its *current* revision —
//     so a restart cannot make a finished Run writable again.
//
// How a wrong implementation could pass a weaker test: one that only asserted
// run.Decide(terminal, trigger) returns an error (a table check) would pass while
// the store happily wrote the status, an event and a sequence number. One that
// returned a plain error instead of wrapping *run.TransitionError, or that
// re-decided the trigger from the current status and moved the run anyway, or
// that let a second writer in through a stale revision, would all pass an
// error-string test but fail the row/event/counter assertions here.
func TestTerminalRunCannotReopen(t *testing.T) {
	ctx := context.Background()
	terminals := []run.RunStatus{
		run.RunStatusCompleted, run.RunStatusFailed, run.RunStatusCancelled, run.RunStatusExpired,
	}
	for _, terminal := range terminals {
		t.Run(string(terminal), func(t *testing.T) {
			f, stored := t102cTerminal(t, terminal)
			before := t102cSnapshotOf(t, f.store, f.runID)
			if !equalRun(before.row, stored) {
				t.Fatalf("snapshot row %+v differs from the terminal row %+v", before.row, stored)
			}

			refused := 0
			for _, trigger := range run.AllTriggers() {
				// completed + merge is the one §21.1 row that leaves completed for
				// completed; it is checked separately below, because it legitimately
				// bumps the revision.
				if trigger.Kind == run.TriggerCommand && trigger.Name == run.CommandMerge {
					continue
				}
				// The table itself must refuse this pair; otherwise the test is
				// asserting a store refusal for something the contract allows.
				if _, decideErr := run.Decide(terminal, trigger); decideErr == nil {
					t.Fatalf("run.Decide(%s, %s) is legal; this test assumes no §21.1 row leaves a terminal state",
						terminal, trigger)
				}

				_, err := f.transition(TransitionInput{
					RunID: f.runID, ExpectedRevision: stored.Revision, ExpectedStatus: terminal,
					Trigger: trigger, At: transitionAt, Actor: systemActor,
					AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr(),
					Destinations: []string{"ws:run:" + f.runID},
				})
				if !errors.Is(err, ErrInvalidTransition) {
					t.Fatalf("%s --%s--> : error = %v, want ErrInvalidTransition", terminal, trigger, err)
				}
				var transitionErr *run.TransitionError
				if !errors.As(err, &transitionErr) {
					t.Fatalf("%s --%s--> : error %v does not wrap *run.TransitionError", terminal, trigger, err)
				}
				if transitionErr.From != terminal {
					t.Errorf("%s --%s--> : TransitionError.From = %s, want %s",
						terminal, trigger, transitionErr.From, terminal)
				}
				// The CAS pair was the stored one, so a conflict here would mean the
				// store refused for the wrong reason.
				if errors.Is(err, ErrRevisionConflict) {
					t.Errorf("%s --%s--> : error %v is a revision conflict; the refusal must be the §21.1 table",
						terminal, trigger, err)
				}
				t102cAssertUnchanged(t, f.store, before, fmt.Sprintf("%s --%s-->", terminal, trigger))
				refused++
			}
			if want := len(run.AllTriggers()) - 1; refused != want {
				t.Fatalf("walked %d triggers, want %d (every trigger but merge)", refused, want)
			}

			// merge: the one legal command from completed, and still not a reopen.
			if terminal == run.RunStatusCompleted {
				result, err := f.transition(TransitionInput{
					RunID: f.runID, ExpectedRevision: stored.Revision, ExpectedStatus: run.RunStatusCompleted,
					Trigger: run.CommandTrigger(run.CommandMerge), At: transitionAt, Actor: userActor,
				})
				if err != nil {
					t.Fatalf("completed --merge--> completed: %v", err)
				}
				if result.Run.Status != run.RunStatusCompleted {
					t.Errorf("merge moved the completed run to %s; a merge is not a reopening", result.Run.Status)
				}
				if result.Run.Revision != before.row.Revision+1 {
					t.Errorf("merge revision = %d, want %d (the command is recorded, the status is not changed)",
						result.Run.Revision, before.row.Revision+1)
				}
				if result.Event != nil {
					t.Errorf("merge wrote a Run state event %+v, want none", result.Event)
				}
				if n := eventRowCount(t, f.store.DB(), "events"); n != before.events {
					t.Errorf("events after merge = %d, want %d", n, before.events)
				}
				if got := counterValue(t, f.store.DB(), projectScope(transitionProjectID)); got != before.projectSeq {
					t.Errorf("project counter after merge = %d, want %d", got, before.projectSeq)
				}
			}

			// Restart: the file is closed and reopened, and the terminal row is read
			// back and refused again — with its *current* revision, so the refusal
			// cannot be a stale CAS.
			if err := f.store.Close(); err != nil {
				t.Fatalf("close: %v", err)
			}
			reopened, res, err := OpenStore(ctx, f.path)
			if err != nil {
				t.Fatalf("reopen %s: %v", f.path, err)
			}
			t.Cleanup(func() { _ = reopened.Close() })
			if len(res.Applied) != 0 {
				t.Errorf("reopen applied %d migrations, want none", len(res.Applied))
			}
			after := t102cSnapshotOf(t, reopened, f.runID)
			if after.row.Status != terminal {
				t.Fatalf("status after reopen = %s, want %s", after.row.Status, terminal)
			}
			for _, trigger := range []run.Trigger{
				run.CommandTrigger(run.CommandRunCancel),
				run.CommandTrigger(run.CommandHardDeadline),
				run.ProcessExitedTrigger(0),
				run.ConclusionTrigger(run.ConclusionCleaned, run.RunStatusCancelled),
			} {
				_, err := t102cRunTx(reopened, TransitionInput{
					RunID: f.runID, ExpectedRevision: after.row.Revision, ExpectedStatus: after.row.Status,
					Trigger: trigger, At: transitionAt, Actor: userActor,
				})
				if !errors.Is(err, ErrInvalidTransition) {
					t.Fatalf("after reopen, %s --%s--> : error = %v, want ErrInvalidTransition", terminal, trigger, err)
				}
				var transitionErr *run.TransitionError
				if !errors.As(err, &transitionErr) {
					t.Errorf("after reopen, %s --%s--> : error %v does not wrap *run.TransitionError", terminal, trigger, err)
				}
				t102cAssertUnchanged(t, reopened, after, fmt.Sprintf("after reopen, %s --%s-->", terminal, trigger))
			}
			t.Logf("EVIDENCE %s: %d triggers refused (every one but merge), row (%s, revision %d, finished_at=%s), events=%d, counters %d/%d; after reopen still %s and refused again",
				terminal, refused, after.row.Status, after.row.Revision, after.row.FinishedAt,
				after.events, after.projectSeq, after.runSeq, after.row.Status)
		})
	}
}

// ---------------------------------------------------------------------------
// TestCancelFinishRace
// ---------------------------------------------------------------------------

// TestCancelFinishRace pins §27.2.5/§27.2.6's single winner: a concurrent
// run.cancel and a concurrent process.exited(0) that both decided from the same
// (revision, status) pair cannot both be applied, and the loser must re-read the
// row and decide again rather than resurrect a terminal state.
//
// Two Store handles over the same file — genuinely two connections, not two
// goroutines sharing one *sql.DB — race one round per Run, at least 20 rounds.
// Each round asserts:
//
//   - exactly one racer succeeded and the other got a *RevisionConflictError
//     carrying the stored revision and the winner's status (never a BUSY-family
//     failure: BEGIN IMMEDIATE must serialise the two writers);
//   - the stored row is the *winner's* target, with exactly one revision bump,
//     and exactly one state event was added to the run's timeline (the loser's
//     event is absent, the winner's is the last row);
//   - exactly one outbox row exists — the winner's destination — even though the
//     loser passed Destinations too: a lost CAS queues no delivery;
//   - the loser re-reads and, if the winner finished the Run, a fresh run.cancel
//     at the *current* revision is ErrInvalidTransition wrapping
//     *run.TransitionError and the row stays completed — a terminal state is not
//     resurrected;
//   - if the winner was the cancel, the Run is cancelling and the loser's
//     completion is re-decided from cancelling per §21.1: process.exited(0) has
//     no row there (ErrInvalidTransition, matching run.Decide), while
//     process.terminated(cancelled) is the legal row, and the run then ends
//     cancelled;
//   - a caller whose expectation is stale in *status* but current in revision —
//     the shape a caller that re-reads only the revision produces — still gets a
//     conflict, not a re-decision from the stored status: both halves of the CAS
//     pair are compared, and the status half is not decoration;
//   - the winner counts over the 20 simultaneous rounds are logged, as the plan
//     asks for this test ("统计并 t.Logf 两种赢家的次数"), and the handle that races
//     the cancel alternates by round. Which writer takes the write lock is the
//     scheduler's business, so coverage of the two orders is not left to that
//     split: two forced rounds, one per order, drive the loser's re-decision
//     deterministically (see the comment above them).
//
// How a wrong implementation could pass a weaker test: one that serialised the
// two racers but let the loser re-decide from the *current* status (instead of
// returning a conflict) would end the round in the wrong state or write a second
// event — the "exactly one event / winner's target" assertions catch it. One
// that returned a conflict but still wrote the status change would pass an
// error-only test and fail the row/event/counter assertions. One that answered
// the loser with a busy/locked error, or with a bare error rather than the typed
// conflict, would pass a "one of them failed" test and fail the conflict
// assertion.
func TestCancelFinishRace(t *testing.T) {
	ctx := context.Background()
	f := newTransitionFixture(t)

	// A second handle on the same file: the race must be between two real
	// connections.
	second, _, err := OpenStore(ctx, f.path)
	if err != nil {
		t.Fatalf("OpenStore(%s): %v", f.path, err)
	}
	t.Cleanup(func() { _ = second.Close() })

	const rounds = 20
	wins := map[string]int{}
	for round := 0; round < rounds; round++ {
		runID := fmt.Sprintf("run-t102c-race-%d", round)
		snapshotID := fmt.Sprintf("snap-t102c-race-%d", round)
		taskID := fmt.Sprintf("task-t102c-race-%d", round)
		seedTask(t, f.store, taskID, transitionProjectID)
		if _, hash := insertSnapshotAndRun(t, f.store, runID, taskID, transitionProjectID, snapshotID,
			json.RawMessage(`{"prompt":"t102c race"}`)); hash == "" {
			t.Fatalf("round %d: the snapshot hash is empty", round)
		}
		before := t102cDriveToRunning(t, f.store, runID)
		eventsBefore := len(t102cEventsOf(t, f.store, runID))
		if eventsBefore != 2 {
			t.Fatalf("round %d: the running run has %d events, want 2 (claim and start)", round, eventsBefore)
		}
		// The outbox is cumulative across rounds (every winner queues one row and
		// nothing delivers it here), so each round asserts a delta of exactly one.
		outboxBefore := eventRowCount(t, f.store.DB(), "outbox")

		// The two racers carry the same expectation: the pair both read. The
		// handles alternate so neither side is always the one holding the lock.
		type racer struct {
			name  string
			store *Store
			in    TransitionInput
		}
		handles := []*Store{f.store, second}
		if round%2 == 1 {
			handles = []*Store{second, f.store}
		}
		racers := []racer{
			{
				name:  "cancel",
				store: handles[0],
				in: TransitionInput{
					RunID: runID, ExpectedRevision: before.Revision, ExpectedStatus: before.Status,
					Trigger: run.CommandTrigger(run.CommandRunCancel), At: transitionAt, Actor: userActor,
					Details: map[string]any{"reason": "t102c race"}, Destinations: []string{"ws:run:" + runID},
				},
			},
			{
				name:  "finish",
				store: handles[1],
				in: TransitionInput{
					RunID: runID, ExpectedRevision: before.Revision, ExpectedStatus: before.Status,
					Trigger: run.ProcessExitedTrigger(0), At: transitionAt, Actor: systemActor,
					AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr(),
					Details: map[string]any{"exit_code": 0}, Destinations: []string{"ws:run:" + runID},
				},
			},
		}

		type outcome struct {
			name   string
			result TransitionResult
			err    error
		}
		start := make(chan struct{})
		results := make(chan outcome, len(racers))
		var wg sync.WaitGroup
		for _, r := range racers {
			wg.Add(1)
			go func(r racer) {
				defer wg.Done()
				<-start
				result, err := t102cRunTx(r.store, r.in)
				results <- outcome{name: r.name, result: result, err: err}
			}(r)
		}
		close(start)
		wg.Wait()
		close(results)

		var winner, loser *outcome
		for i := 0; i < len(racers); i++ {
			out := <-results
			switch {
			case out.err == nil:
				if winner != nil {
					t.Fatalf("round %d: both racers succeeded (%s and %s)", round, winner.name, out.name)
				}
				out := out
				winner = &out
			case errors.Is(out.err, ErrRevisionConflict):
				if loser != nil {
					t.Fatalf("round %d: both racers conflicted (%s and %s)", round, loser.name, out.name)
				}
				var conflict *RevisionConflictError
				if !errors.As(out.err, &conflict) {
					t.Fatalf("round %d: %s error %v is not a *RevisionConflictError", round, out.name, out.err)
				}
				if conflict.RunID != runID || conflict.Expected != before.Revision {
					t.Errorf("round %d: conflict = (%s, expected %d), want (%s, %d)",
						round, conflict.RunID, conflict.Expected, runID, before.Revision)
				}
				if conflict.CurrentStatus != winner.result.Run.Status {
					t.Errorf("round %d: conflict status = %s, want the winner's %s",
						round, conflict.CurrentStatus, winner.result.Run.Status)
				}
				out := out
				loser = &out
			default:
				t.Fatalf("round %d: %s failed with %v, want a revision conflict", round, out.name, out.err)
			}
		}
		if winner == nil || loser == nil {
			t.Fatalf("round %d: winner=%v loser=%v, want exactly one of each", round, winner, loser)
		}
		wins[winner.name]++
		if code := sqliteExtended(winner.err); code == 5 || code == 517 || code == 6 {
			t.Fatalf("round %d: the winner hit a SQLITE_BUSY-family error (%d)", round, code)
		}

		// The final row is the winner's target, one revision bump, one new event.
		stored, err := GetRun(ctx, f.store.DB(), runID)
		if err != nil {
			t.Fatalf("round %d: GetRun: %v", round, err)
		}
		if stored.Status != winner.result.Run.Status {
			t.Fatalf("round %d: final status = %s, want the winner's %s",
				round, stored.Status, winner.result.Run.Status)
		}
		if stored.Revision != before.Revision+1 {
			t.Errorf("round %d: final revision = %d, want %d", round, stored.Revision, before.Revision+1)
		}
		events := t102cEventsOf(t, f.store, runID)
		if len(events) != eventsBefore+1 {
			t.Fatalf("round %d: the run has %d events, want %d (only the winner writes one)",
				round, len(events), eventsBefore+1)
		}
		last := events[len(events)-1]
		if last.Type != string(winner.result.Transition.StateEvent) {
			t.Errorf("round %d: last event = %s, want the winner's %s",
				round, last.Type, winner.result.Transition.StateEvent)
		}
		if loser.result.Event != nil {
			t.Errorf("round %d: the loser returned event %+v, want nil", round, loser.result.Event)
		}
		if n := eventRowCount(t, f.store.DB(), "outbox"); n != outboxBefore+1 {
			t.Errorf("round %d: outbox rows = %d, want %d (the winner's delivery; the loser queues none)",
				round, n, outboxBefore+1)
		}
		if got := counterValue(t, f.store.DB(), runScope(runID)); got != int64(eventsBefore+1) {
			t.Errorf("round %d: run counter = %d, want %d", round, got, eventsBefore+1)
		}

		// The loser re-reads and decides again.
		if stored.Status.IsTerminal() {
			// The winner finished the run: the loser's cancel must be refused by the
			// table, not applied and not turned into a conflict.
			_, err := f.transition(TransitionInput{
				RunID: runID, ExpectedRevision: stored.Revision, ExpectedStatus: stored.Status,
				Trigger: run.CommandTrigger(run.CommandRunCancel), At: transitionAt, Actor: userActor,
			})
			if !errors.Is(err, ErrInvalidTransition) {
				t.Errorf("round %d: cancel of a %s run = %v, want ErrInvalidTransition", round, stored.Status, err)
			}
			var transitionErr *run.TransitionError
			if !errors.As(err, &transitionErr) {
				t.Errorf("round %d: error %v does not wrap *run.TransitionError", round, err)
			}
			after, err := GetRun(ctx, f.store.DB(), runID)
			if err != nil {
				t.Fatalf("round %d: GetRun after the refused cancel: %v", round, err)
			}
			if !equalRun(after, stored) {
				t.Errorf("round %d: the terminal run moved on the refused cancel:\n before %+v\n after  %+v",
					round, stored, after)
			}
		} else {
			// The winner cancelled: the Run is cancelling and the loser's completion
			// is re-decided from there (§21.1 has no cancelling + process.exited row).
			if stored.Status != run.RunStatusCancelling {
				t.Fatalf("round %d: the cancel winner left the run in %s, want cancelling", round, stored.Status)
			}
			if _, err := run.Decide(stored.Status, run.ProcessExitedTrigger(0)); err == nil {
				t.Errorf("round %d: §21.1 has a cancelling --process.exited(0)--> row; the re-decision is not a refusal", round)
			}
			_, err := f.transition(TransitionInput{
				RunID: runID, ExpectedRevision: stored.Revision, ExpectedStatus: stored.Status,
				Trigger: run.ProcessExitedTrigger(0), At: transitionAt, Actor: systemActor,
				AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr(),
			})
			if !errors.Is(err, ErrInvalidTransition) {
				t.Errorf("round %d: cancelling --process.exited(0)--> = %v, want ErrInvalidTransition", round, err)
			}
			// The legal row from cancelling is process.terminated with the recorded
			// expected terminal; the run ends cancelled.
			done, err := f.transition(TransitionInput{
				RunID: runID, ExpectedRevision: stored.Revision, ExpectedStatus: run.RunStatusCancelling,
				Trigger: run.ProcessTerminatedTrigger(run.RunStatusCancelled), At: transitionAt, Actor: systemActor,
				AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr(),
			})
			if err != nil {
				t.Fatalf("round %d: cancelling --process.terminated(cancelled)--> : %v", round, err)
			}
			if done.Run.Status != run.RunStatusCancelled {
				t.Errorf("round %d: the cancelled run ended %s, want cancelled", round, done.Run.Status)
			}
		}
		t.Logf("EVIDENCE round %d: winner=%s -> %s (revision %d->%d), loser=%s conflict (current %d/%s), events=%d, outbox=1",
			round, winner.name, stored.Status, before.Revision, stored.Revision, loser.name,
			loser.result.Run.Revision, loser.result.Run.Status, len(events))
	}
	t.Logf("EVIDENCE race: %d simultaneous rounds, winners cancel=%d finish=%d "+
		"(which writer wins the lock is the scheduler's choice; both loser paths are covered by the forced rounds below)",
		rounds, wins["cancel"], wins["finish"])

	// The status half of the CAS pair, on its own. A caller can hold the current
	// revision and still be wrong about the status — that is exactly the shape of
	// a caller that re-reads only the revision, or of one whose decision was taken
	// before a status-only change. The store must answer with a conflict rather
	// than re-deciding the trigger from the status it finds, because the trigger
	// was chosen for a different status.
	t.Run("stale_status_current_revision_conflicts", func(t *testing.T) {
		f := newTransitionFixture(t)
		if _, err := f.transition(TransitionInput{
			RunID: f.runID, ExpectedRevision: 1, ExpectedStatus: run.RunStatusQueued,
			Trigger: run.EventTrigger(string(run.EventSchedulerClaimed)), At: transitionAt, Actor: systemActor,
		}); err != nil {
			t.Fatalf("claim: %v", err)
		}
		stored, err := GetRun(ctx, f.store.DB(), f.runID)
		if err != nil {
			t.Fatalf("GetRun: %v", err)
		}
		if stored.Status != run.RunStatusStarting {
			t.Fatalf("the claimed run is %s, want starting", stored.Status)
		}
		before := t102cSnapshotOf(t, f.store, f.runID)

		// Current revision, stale status, and a trigger that is legal from the
		// stale status (queued --run.cancel--> cancelling) but not from starting.
		_, err = f.transition(TransitionInput{
			RunID: f.runID, ExpectedRevision: stored.Revision, ExpectedStatus: run.RunStatusQueued,
			Trigger: run.CommandTrigger(run.CommandRunCancel), At: transitionAt, Actor: userActor,
		})
		if !errors.Is(err, ErrRevisionConflict) {
			t.Fatalf("current revision + stale status = %v, want ErrRevisionConflict "+
				"(a store that re-decides from the stored status would accept or refuse by the table)", err)
		}
		var conflict *RevisionConflictError
		if !errors.As(err, &conflict) {
			t.Fatalf("error %v is not a *RevisionConflictError", err)
		}
		if conflict.RunID != f.runID || conflict.Expected != stored.Revision ||
			conflict.Current != stored.Revision || conflict.CurrentStatus != run.RunStatusStarting {
			t.Errorf("conflict = (%s, expected %d, current %d/%s), want (%s, %d, %d/starting)",
				conflict.RunID, conflict.Expected, conflict.Current, conflict.CurrentStatus,
				f.runID, stored.Revision, stored.Revision)
		}
		t102cAssertUnchanged(t, f.store, before, "current revision + stale status")
		t.Logf("EVIDENCE status CAS: revision %d with a stale queued status refused as %v; the run stayed (%s,%d)",
			stored.Revision, conflict, before.row.Status, before.row.Revision)
	})

	// Both loser paths, deterministically, one per order. A simultaneous race
	// cannot be asked for a particular winner — the scheduler decides which
	// writer takes the write lock first — so the coverage of the two re-decisions
	// does not rest on the split above. Each round below holds the write lock on
	// one handle and only then starts the other writer: the holder commits its
	// transition first, and the starter reads the row after that commit, so the
	// loser is exactly the starter and its trigger can be chosen. This uses no
	// test-only hook in the implementation: it is a second handle and the store's
	// own WithTx.
	t.Run("loser_finishes_after_a_cancel", func(t *testing.T) {
		f := newTransitionFixture(t)
		second, _, err := OpenStore(ctx, f.path)
		if err != nil {
			t.Fatalf("OpenStore(%s): %v", f.path, err)
		}
		t.Cleanup(func() { _ = second.Close() })
		before := t102cDriveToRunning(t, f.store, f.runID)
		eventsBefore := len(t102cEventsOf(t, f.store, f.runID))

		winner, loser := t102cOrderedRace(t, f.store, second,
			TransitionInput{
				RunID: f.runID, ExpectedRevision: before.Revision, ExpectedStatus: before.Status,
				Trigger: run.CommandTrigger(run.CommandRunCancel), At: transitionAt, Actor: userActor,
				Details: map[string]any{"reason": "forced race"}, Destinations: []string{"ws:run:" + f.runID},
			},
			TransitionInput{
				RunID: f.runID, ExpectedRevision: before.Revision, ExpectedStatus: before.Status,
				Trigger: run.ProcessExitedTrigger(0), At: transitionAt, Actor: systemActor,
				AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr(),
				Details: map[string]any{"exit_code": 0}, Destinations: []string{"ws:run:" + f.runID},
			})
		if winner.result.Run.Status != run.RunStatusCancelling {
			t.Fatalf("the lock holder ended in %s, want cancelling (the cancel must have committed first)",
				winner.result.Run.Status)
		}
		if loser.result.Event != nil {
			t.Errorf("the loser returned event %+v, want nil", loser.result.Event)
		}
		stored, err := GetRun(ctx, f.store.DB(), f.runID)
		if err != nil {
			t.Fatalf("GetRun: %v", err)
		}
		if stored.Status != run.RunStatusCancelling {
			t.Fatalf("the run is %s, want cancelling (the loser must not have finished it)", stored.Status)
		}
		if stored.Revision != before.Revision+1 {
			t.Errorf("revision = %d, want %d (exactly one writer moved it)", stored.Revision, before.Revision+1)
		}
		if events := t102cEventsOf(t, f.store, f.runID); len(events) != eventsBefore+1 {
			t.Errorf("events = %d, want %d (only the winner wrote one)", len(events), eventsBefore+1)
		}
		if got := counterValue(t, f.store.DB(), runScope(f.runID)); got != int64(eventsBefore+1) {
			t.Errorf("run counter = %d, want %d", got, eventsBefore+1)
		}

		// The loser re-reads cancelling and re-decides: §21.1 has no
		// cancelling --process.exited(0)--> row, so the store refuses it and the
		// run keeps waiting for the termination confirmation. process.terminated
		// with the recorded expected terminal is the legal way out.
		if _, err := run.Decide(stored.Status, run.ProcessExitedTrigger(0)); err == nil {
			t.Errorf("§21.1 has a cancelling --process.exited(0)--> row; the re-decision is not a refusal")
		}
		beforeRefusal := t102cSnapshotOf(t, f.store, f.runID)
		_, err = t102cRunTx(f.store, TransitionInput{
			RunID: f.runID, ExpectedRevision: stored.Revision, ExpectedStatus: stored.Status,
			Trigger: run.ProcessExitedTrigger(0), At: transitionAt, Actor: systemActor,
			AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr(),
		})
		if !errors.Is(err, ErrInvalidTransition) {
			t.Errorf("cancelling --process.exited(0)--> = %v, want ErrInvalidTransition", err)
		}
		t102cAssertUnchanged(t, f.store, beforeRefusal, "cancelling --process.exited(0)-->")
		done, err := t102cRunTx(f.store, TransitionInput{
			RunID: f.runID, ExpectedRevision: stored.Revision, ExpectedStatus: run.RunStatusCancelling,
			Trigger: run.ProcessTerminatedTrigger(run.RunStatusCancelled), At: transitionAt, Actor: systemActor,
			AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr(),
		})
		if err != nil {
			t.Fatalf("cancelling --process.terminated(cancelled)--> : %v", err)
		}
		if done.Run.Status != run.RunStatusCancelled {
			t.Errorf("the cancelled run ended %s, want cancelled", done.Run.Status)
		}
		t.Logf("EVIDENCE forced race (cancel first): winner=%s -> %s, loser=%s %v; re-decision refused, "+
			"process.terminated(cancelled) ended %s", winner.name, winner.result.Run.Status, loser.name, loser.err, done.Run.Status)
	})

	t.Run("loser_cancels_after_a_finish", func(t *testing.T) {
		f := newTransitionFixture(t)
		second, _, err := OpenStore(ctx, f.path)
		if err != nil {
			t.Fatalf("OpenStore(%s): %v", f.path, err)
		}
		t.Cleanup(func() { _ = second.Close() })
		before := t102cDriveToRunning(t, f.store, f.runID)
		eventsBefore := len(t102cEventsOf(t, f.store, f.runID))

		winner, loser := t102cOrderedRace(t, f.store, second,
			TransitionInput{
				RunID: f.runID, ExpectedRevision: before.Revision, ExpectedStatus: before.Status,
				Trigger: run.ProcessExitedTrigger(0), At: transitionAt, Actor: systemActor,
				AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr(),
				Details: map[string]any{"exit_code": 0}, Destinations: []string{"ws:run:" + f.runID},
			},
			TransitionInput{
				RunID: f.runID, ExpectedRevision: before.Revision, ExpectedStatus: before.Status,
				Trigger: run.CommandTrigger(run.CommandRunCancel), At: transitionAt, Actor: userActor,
				Details: map[string]any{"reason": "forced race"}, Destinations: []string{"ws:run:" + f.runID},
			})
		if winner.result.Run.Status != run.RunStatusCompleted {
			t.Fatalf("the lock holder ended in %s, want completed (the finish must have committed first)",
				winner.result.Run.Status)
		}
		if loser.result.Event != nil {
			t.Errorf("the loser returned event %+v, want nil", loser.result.Event)
		}
		stored, err := GetRun(ctx, f.store.DB(), f.runID)
		if err != nil {
			t.Fatalf("GetRun: %v", err)
		}
		if stored.Status != run.RunStatusCompleted {
			t.Fatalf("the run is %s, want completed (the loser must not have reopened it)", stored.Status)
		}
		if stored.Revision != before.Revision+1 {
			t.Errorf("revision = %d, want %d", stored.Revision, before.Revision+1)
		}
		if events := t102cEventsOf(t, f.store, f.runID); len(events) != eventsBefore+1 {
			t.Errorf("events = %d, want %d (only the winner wrote one)", len(events), eventsBefore+1)
		}
		// The loser re-reads the terminal row: the cancel is refused by the table,
		// not applied, and the row does not move.
		_, err = t102cRunTx(f.store, TransitionInput{
			RunID: f.runID, ExpectedRevision: stored.Revision, ExpectedStatus: stored.Status,
			Trigger: run.CommandTrigger(run.CommandRunCancel), At: transitionAt, Actor: userActor,
		})
		if !errors.Is(err, ErrInvalidTransition) {
			t.Errorf("cancel of a completed run = %v, want ErrInvalidTransition", err)
		}
		var transitionErr *run.TransitionError
		if !errors.As(err, &transitionErr) {
			t.Errorf("error %v does not wrap *run.TransitionError", err)
		}
		after, err := GetRun(ctx, f.store.DB(), f.runID)
		if err != nil {
			t.Fatalf("GetRun after the refused cancel: %v", err)
		}
		if !equalRun(after, stored) {
			t.Errorf("the terminal run moved on the refused cancel:\n before %+v\n after  %+v", stored, after)
		}
		t.Logf("EVIDENCE forced race (finish first): winner=%s -> %s, loser=%s %v; the refused cancel left the row at (%s, revision %d)",
			winner.name, winner.result.Run.Status, loser.name, loser.err, after.Status, after.Revision)
	})
}

// t102cForcedOutcome is one writer's result in a forced round: the name, the
// transition result and the error, held by value so neither pointer aliases the
// other.
type t102cForcedOutcome struct {
	name   string
	result TransitionResult
	err    error
}

// t102cOrderedRace runs first and second as two real writers on the same file,
// with the order fixed by the write lock rather than by the scheduler: first is
// already inside a transaction (BEGIN IMMEDIATE has taken the lock) when second
// is started, so first commits before second reads the row, and second is the
// loser. The two handles must be different stores over the same file, or the
// second call would deadlock on the first's lock.
//
// It fails the test unless first succeeded, second got a *RevisionConflictError
// whose stored revision and status are the winner's, and neither writer hit a
// SQLITE_BUSY-family failure.
func t102cOrderedRace(t *testing.T, first, second *Store, firstIn, secondIn TransitionInput) (t102cForcedOutcome, t102cForcedOutcome) {
	t.Helper()
	if first == second {
		t.Fatalf("t102cOrderedRace needs two handles over the same file")
	}
	ctx := context.Background()
	firstIn, secondIn = t102cDefaults(firstIn), t102cDefaults(secondIn)

	firstInLock := make(chan struct{})
	firstResult := make(chan t102cForcedOutcome, 1)
	go func() {
		var out t102cForcedOutcome
		out.name = firstIn.Trigger.String()
		out.err = first.WithTx(ctx, func(ctx context.Context, tx Tx) error {
			close(firstInLock)
			result, err := TransitionRunTx(ctx, tx, firstIn)
			out.result = result
			return err
		})
		firstResult <- out
	}()
	<-firstInLock

	var secondOut t102cForcedOutcome
	secondOut.name = secondIn.Trigger.String()
	secondOut.err = second.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		result, err := TransitionRunTx(ctx, tx, secondIn)
		secondOut.result = result
		return err
	})
	winner := <-firstResult

	if winner.err != nil {
		t.Fatalf("the lock holder failed with %v, want the committed first transition", winner.err)
	}
	if !errors.Is(secondOut.err, ErrRevisionConflict) {
		t.Fatalf("the started-later writer got %v, want *RevisionConflictError (it read the row after the holder committed)",
			secondOut.err)
	}
	var conflict *RevisionConflictError
	if !errors.As(secondOut.err, &conflict) {
		t.Fatalf("error %v is not a *RevisionConflictError", secondOut.err)
	}
	if conflict.RunID != firstIn.RunID || conflict.Expected != firstIn.ExpectedRevision ||
		conflict.Current != winner.result.Run.Revision || conflict.CurrentStatus != winner.result.Run.Status {
		t.Errorf("conflict = (%s, expected %d, current %d/%s), want (%s, %d, %d/%s)",
			conflict.RunID, conflict.Expected, conflict.Current, conflict.CurrentStatus,
			firstIn.RunID, firstIn.ExpectedRevision, winner.result.Run.Revision, winner.result.Run.Status)
	}
	for _, out := range []t102cForcedOutcome{winner, secondOut} {
		if code := sqliteExtended(out.err); code == 5 || code == 517 || code == 6 {
			t.Errorf("%s hit a SQLITE_BUSY-family error (%d)", out.name, code)
		}
	}
	return winner, secondOut
}

// t102cDefaults fills the fixture's instant and actor into an input that left
// them zero, the same defaults t102cRunTx applies.
func t102cDefaults(in TransitionInput) TransitionInput {
	if in.At.IsZero() {
		in.At = transitionAt
	}
	if in.Actor.ID == "" {
		in.Actor = systemActor
	}
	return in
}

// t102cEventsOf reads a run's whole timeline through the run-scoped view.
func t102cEventsOf(t *testing.T, store *Store, runID string) []Event {
	t.Helper()
	events, _, err := ListRunEvents(context.Background(), store.DB(), runID, 0, 0)
	if err != nil {
		t.Fatalf("ListRunEvents(%s): %v", runID, err)
	}
	return events
}

// ---------------------------------------------------------------------------
// TestRetryUsesNewIdentity
// ---------------------------------------------------------------------------

// TestRetryUsesNewIdentity pins §27.2.7's "重试创建新 Run": a retry is a new
// identity executing the same frozen input, not a continuation of the old Run.
//
// The assertions, in order:
//
//  1. the new Run's id is the caller's NewRunID and differs from the original's;
//     it is queued at revision 1 with RetryOfRunID pointing at the original, and
//     every frozen pin (project, task, binding and binding revision, base
//     manifest hash, base commit, agent revision, input snapshot id and hash,
//     budget) equals the original's field by field. The stored row agrees with
//     the returned one.
//  2. the new Run has no attempt (no attempts row, none listed) and no event:
//     its run-scoped timeline is empty, the events table did not grow, and its
//     run sequence counter does not exist (no sequence was ever allocated).
//  3. the original Run is untouched: the row compares equal field by field and
//     its whole timeline — ids, types, run_seq, payload bytes, identity bytes —
//     is byte-for-byte what it was before the retry.
//  4. the new Run's *own* first transitions carry the new identity: claim, then
//     process.started with a fresh attempt id (a real attempts row for the new
//     run). The state event identity's run_id and attempt_id are the new values,
//     and a scan of every event row proves no new-run id or new-attempt id ever
//     appears on the original run's timeline.
//
// On approvals: "retry 不复制 approval" is T2.02's property (the approvals table
// does not exist yet). What this test can and does assert is the store-level half
// of it: nothing approval-shaped is carried forward — no approval key appears in
// the new Run's events or payloads, and the original attempt's id (which is what
// an approval fingerprint would be scoped to) appears nowhere in the new Run's
// rows or timeline.
//
// How a wrong implementation could pass a weaker test: one that returned the
// original row (or reused its id) would satisfy "queued and linked" only if the
// assertions on id inequality and on the original being unchanged were missing.
// One that copied the original's attempt or events forward would pass a
// "frozen input copied" test and fail the empty-timeline and byte-comparison
// assertions. One that wrote the new Run's events under the *original* run's
// identity would pass a "status event exists" check and fail the identity scan.
func TestRetryUsesNewIdentity(t *testing.T) {
	ctx := context.Background()
	f := newRetryFixture(t)

	originalEventsBefore := t102cEventsOf(t, f.store, f.runID)
	if len(originalEventsBefore) == 0 {
		t.Fatal("the failed fixture run has no events; the byte-comparison below would prove nothing")
	}
	originalAttemptsBefore, err := ListAttemptsByRun(ctx, f.store.DB(), f.runID)
	if err != nil {
		t.Fatalf("ListAttemptsByRun(%s): %v", f.runID, err)
	}
	if len(originalAttemptsBefore) != 0 {
		// The transition fixtures carry the attempt id as an identity value, not as
		// an attempts row (T1.02.b does not look the attempt up). Record what the
		// fixture actually holds rather than assuming.
		t.Logf("note: the fixture's original run has %d attempts rows", len(originalAttemptsBefore))
	}
	eventsBefore := eventRowCount(t, f.store.DB(), "events")
	outboxBefore := eventRowCount(t, f.store.DB(), "outbox")

	const newRunID = "run-t102c-retry-2"
	result := f.mustRetry(f.retryInput(newRunID))

	// --- 1. a new identity reusing the frozen input ---------------------------
	if result.Run.ID != newRunID {
		t.Errorf("new run id = %s, want %s", result.Run.ID, newRunID)
	}
	if result.Run.ID == f.original.ID {
		t.Errorf("new run id equals the original's (%s); a retry creates a new run", result.Run.ID)
	}
	if result.Run.Status != run.RunStatusQueued {
		t.Errorf("new run status = %s, want queued", result.Run.Status)
	}
	if result.Run.Revision != 1 {
		t.Errorf("new run revision = %d, want 1", result.Run.Revision)
	}
	if result.Run.RetryOfRunID == nil || *result.Run.RetryOfRunID != f.original.ID {
		t.Errorf("retry_of_run_id = %s, want %q", stringPtrString(result.Run.RetryOfRunID), f.original.ID)
	}
	if result.Run.FinishedAt != nil {
		t.Errorf("new run finished_at = %s, want nil (a queued run is not terminal)", result.Run.FinishedAt)
	}
	for _, pin := range []struct {
		name      string
		got, want string
	}{
		{"project_id", result.Run.ProjectID, f.original.ProjectID},
		{"task_id", result.Run.TaskID, f.original.TaskID},
		{"binding_id", result.Run.BindingID, f.original.BindingID},
		{"base_manifest_hash", result.Run.BaseManifestHash, f.original.BaseManifestHash},
		{"agent_revision_id", result.Run.AgentRevisionID, f.original.AgentRevisionID},
		{"input_snapshot_id", result.Run.InputSnapshotID, f.original.InputSnapshotID},
		{"input_snapshot_hash", result.Run.InputSnapshotHash, f.original.InputSnapshotHash},
		{"base_commit", stringPtrString(result.Run.BaseCommit), stringPtrString(f.original.BaseCommit)},
	} {
		if pin.got != pin.want {
			t.Errorf("new run %s = %q, want the original's %q", pin.name, pin.got, pin.want)
		}
	}
	if result.Run.BindingRevision != f.original.BindingRevision {
		t.Errorf("new run binding_revision = %d, want the original's %d",
			result.Run.BindingRevision, f.original.BindingRevision)
	}
	if !equalBudget(result.Run.Budget, f.original.Budget) {
		t.Errorf("new run budget = %+v, want the original's %+v", result.Run.Budget, f.original.Budget)
	}
	if !result.Run.CreatedAt.Equal(retryAt) || !result.Run.UpdatedAt.Equal(retryAt) {
		t.Errorf("new run timestamps = (%s,%s), want %s", result.Run.CreatedAt, result.Run.UpdatedAt, retryAt)
	}
	stored, err := GetRun(ctx, f.store.DB(), newRunID)
	if err != nil {
		t.Fatalf("GetRun(%s): %v", newRunID, err)
	}
	if !equalRun(stored, result.Run) {
		t.Errorf("stored new run %+v differs from the returned %+v", stored, result.Run)
	}

	// --- 2. no attempt and no event ------------------------------------------
	newAttempts, err := ListAttemptsByRun(ctx, f.store.DB(), newRunID)
	if err != nil {
		t.Fatalf("ListAttemptsByRun(%s): %v", newRunID, err)
	}
	if len(newAttempts) != 0 {
		t.Errorf("the new run has %d attempts, want 0 (a retry starts no process)", len(newAttempts))
	}
	if events := t102cEventsOf(t, f.store, newRunID); len(events) != 0 {
		t.Errorf("the new run has %d events, want 0 (its timeline starts with its first transition)", len(events))
	}
	if n := eventRowCount(t, f.store.DB(), "events"); n != eventsBefore {
		t.Errorf("events = %d after the retry, want %d", n, eventsBefore)
	}
	if n := eventRowCount(t, f.store.DB(), "outbox"); n != outboxBefore {
		t.Errorf("outbox rows = %d after the retry, want %d", n, outboxBefore)
	}
	if got := counterValue(t, f.store.DB(), runScope(newRunID)); got > 0 {
		t.Errorf("the new run's sequence counter = %d, want no row (a retry allocates no sequence)", got)
	}

	// --- 3. the original Run is untouched, byte for byte ----------------------
	originalAfter, err := GetRun(ctx, f.store.DB(), f.runID)
	if err != nil {
		t.Fatalf("GetRun(%s): %v", f.runID, err)
	}
	if !equalRun(originalAfter, f.original) {
		t.Errorf("the original run changed:\n before %+v\n after  %+v", f.original, originalAfter)
	}
	t102cAssertEventsIdentical(t, originalEventsBefore, t102cEventsOf(t, f.store, f.runID), "after the retry")

	// --- 4. the new run's own transitions carry the new identity -------------
	const (
		// Deliberately not "att-t1...": the substring scan below must not match the
		// new id as a prefix of the old one.
		newAttemptID   = "attempt-t102c-1"
		newAgentRevID  = "agentrev-t102c-1"
		newProcessNote = "the retry's own process"
	)
	claimed, err := t102cRunTx(f.store, TransitionInput{
		RunID: newRunID, ExpectedRevision: result.Run.Revision, ExpectedStatus: run.RunStatusQueued,
		Trigger: run.EventTrigger(string(run.EventSchedulerClaimed)), At: retryAt,
		Details: map[string]any{"note": newProcessNote},
	})
	if err != nil {
		t.Fatalf("claim the retried run: %v", err)
	}
	if claimed.Event == nil || claimed.Event.RunID == nil || *claimed.Event.RunID != newRunID {
		t.Fatalf("claim event = %+v, want one on run %s", claimed.Event, newRunID)
	}
	if got := t102cIdentityValue(t, *claimed.Event, "run_id"); got != newRunID {
		t.Errorf("claim identity run_id = %v, want %s", got, newRunID)
	}

	// The attempt row and the process.started transition go in one transaction:
	// the new run's attempt is its own.
	attempt := run.Attempt{
		ID: newAttemptID, RunID: newRunID, AttemptNo: 1, Backend: "fake",
		Status: run.AttemptStatusRunning, StartedAt: &retryAt, CreatedAt: retryAt,
	}
	var started TransitionResult
	err = f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		if err := InsertAttempt(ctx, tx, &attempt); err != nil {
			return err
		}
		var err error
		started, err = TransitionRunTx(ctx, tx, TransitionInput{
			RunID: newRunID, ExpectedRevision: claimed.Run.Revision, ExpectedStatus: run.RunStatusStarting,
			Trigger: run.EventTrigger(string(run.EventProcessStarted)), At: retryAt, Actor: systemActor,
			AttemptID: stringPtr(newAttemptID), AgentRevisionID: stringPtr(newAgentRevID),
			Details: map[string]any{"pid": 4242, "note": newProcessNote},
		})
		return err
	})
	if err != nil {
		t.Fatalf("start the retried run: %v", err)
	}
	if started.Run.Status != run.RunStatusRunning {
		t.Fatalf("the retried run is %s after process.started, want running", started.Run.Status)
	}
	startedEvent := started.Event
	if startedEvent == nil {
		t.Fatal("process.started wrote no event")
	}
	if startedEvent.AttemptID == nil || *startedEvent.AttemptID != newAttemptID {
		t.Errorf("process.started attempt_id = %s, want %s", stringPtrString(startedEvent.AttemptID), newAttemptID)
	}
	for key, want := range map[string]any{
		"run_id":            newRunID,
		"attempt_id":        newAttemptID,
		"agent_revision_id": newAgentRevID,
	} {
		if got := t102cIdentityValue(t, *startedEvent, key); got != want {
			t.Errorf("process.started identity %s = %v, want %v", key, got, want)
		}
	}
	if got := payloadField(t, *startedEvent, "note"); got != newProcessNote {
		t.Errorf("process.started payload note = %v, want %q", got, newProcessNote)
	}
	if got := t102cIdentityValue(t, *startedEvent, "run_id"); got == f.original.ID {
		t.Errorf("the retried run's event identity names the original run %s", f.original.ID)
	}

	// The original timeline did not grow, and no new value leaked into it.
	originalEventsAfter := t102cEventsOf(t, f.store, f.runID)
	t102cAssertEventsIdentical(t, originalEventsBefore, originalEventsAfter, "after the retried run started")
	for _, event := range originalEventsAfter {
		text := string(event.Identity) + " " + string(event.Payload)
		if strings.Contains(text, newRunID) {
			t.Errorf("the original run's event %s names the retried run %s", event.ID, newRunID)
		}
		if strings.Contains(text, newAttemptID) {
			t.Errorf("the original run's event %s names the retried attempt %s", event.ID, newAttemptID)
		}
	}

	// --- 5. nothing approval-shaped was carried forward ----------------------
	newRunEvents := t102cEventsOf(t, f.store, newRunID)
	if len(newRunEvents) != 2 {
		t.Fatalf("the retried run has %d events, want 2 (claim and start)", len(newRunEvents))
	}
	for _, event := range newRunEvents {
		text := string(event.Identity) + " " + string(event.Payload)
		if strings.Contains(strings.ToLower(text), "approval") {
			t.Errorf("the retried run's event %s carries approval-shaped data (%s); a retry must not copy approvals",
				event.ID, text)
		}
		if strings.Contains(text, *attemptPtr()) {
			t.Errorf("the retried run's event %s names the original attempt %s; a retry must not carry the old attempt forward",
				event.ID, *attemptPtr())
		}
	}
	allAttempts, err := ListAttemptsByRun(ctx, f.store.DB(), newRunID)
	if err != nil {
		t.Fatalf("ListAttemptsByRun(%s): %v", newRunID, err)
	}
	if len(allAttempts) != 1 || allAttempts[0].ID != newAttemptID {
		t.Errorf("the retried run's attempts = %+v, want exactly %s", allAttempts, newAttemptID)
	}
	t.Logf("EVIDENCE retry identity: %s(failed, revision %d) -> %s(queued, revision 1, retry_of=%s, no attempt, no event); "+
		"original timeline still %d events byte-identical; the new run's own claim+start carry run_id=%s attempt_id=%s",
		f.original.ID, f.original.Revision, result.Run.ID, stringPtrString(result.Run.RetryOfRunID),
		len(originalEventsAfter), newRunID, newAttemptID)
}

// t102cIdentityValue reads one key out of a stored event's identity JSON. The
// stored bytes are decoded rather than the bytes the caller passed, so the
// assertion is about what was persisted.
func t102cIdentityValue(t *testing.T, event Event, key string) any {
	t.Helper()
	var identity map[string]any
	if err := json.Unmarshal(event.Identity, &identity); err != nil {
		t.Fatalf("event %s identity %s: %v", event.Type, event.Identity, err)
	}
	return identity[key]
}

// t102cAssertEventsIdentical compares two reads of the same run timeline field
// by field, including the payload and identity *bytes*: a retry that rewrote an
// old event's payload in place (impossible through the store, which has no
// update path, but possible for a hand-written statement) must fail here.
func t102cAssertEventsIdentical(t *testing.T, before, after []Event, when string) {
	t.Helper()
	if len(before) != len(after) {
		t.Fatalf("the run timeline has %d events %s, want %d", len(after), when, len(before))
	}
	for i := range before {
		a, b := before[i], after[i]
		if a.ID != b.ID || a.Type != b.Type || a.ProjectSeq != b.ProjectSeq {
			t.Errorf("event %d %s = (%s, %s, project_seq %d), want (%s, %s, %d)",
				i, when, b.ID, b.Type, b.ProjectSeq, a.ID, a.Type, a.ProjectSeq)
			continue
		}
		if !equalInt64Ptr(a.RunSeq, b.RunSeq) {
			t.Errorf("event %s run_seq %s %s, want %s", a.ID, when, runSeqString(b.RunSeq), runSeqString(a.RunSeq))
		}
		if !equalStringPtr(a.AttemptID, b.AttemptID) {
			t.Errorf("event %s attempt_id %s %s, want %s", a.ID, when,
				stringPtrString(b.AttemptID), stringPtrString(a.AttemptID))
		}
		if string(a.Payload) != string(b.Payload) {
			t.Errorf("event %s payload %s = %s, want %s", a.ID, when, b.Payload, a.Payload)
		}
		if string(a.Identity) != string(b.Identity) {
			t.Errorf("event %s identity %s = %s, want %s", a.ID, when, b.Identity, a.Identity)
		}
		if a.PayloadHash != b.PayloadHash {
			t.Errorf("event %s payload_hash %s = %s, want %s", a.ID, when, b.PayloadHash, a.PayloadHash)
		}
		if !a.OccurredAt.Equal(b.OccurredAt) {
			t.Errorf("event %s occurred_at %s = %s, want %s", a.ID, when, b.OccurredAt, a.OccurredAt)
		}
	}
}

// ---------------------------------------------------------------------------
// TestQueuedEventNoAttempt
// ---------------------------------------------------------------------------

// TestQueuedEventNoAttempt pins §28's "queued 可无 attempt": a state event
// written while the Run is still queued must be writable without an attempt,
// and that permission is a property of the queued rule, not of the store being
// lax about identity.
//
// The test walks the two §21.1 rows that leave queued for cancelling — run.cancel
// and hard_deadline — with neither AttemptID nor AgentRevisionID set, and asserts
// the *stored* row of the event:
//
//   - events.attempt_id is SQL NULL (read with a NULL-capable scan, not inferred
//     from a Go nil);
//   - the stored identity JSON has no attempt_id or agent_revision_id key, or
//     has it as JSON null — the store re-encodes the validated identity, so the
//     check reads what was persisted rather than what was passed;
//   - the identity does carry run_id and project_id, and the actor, so "no
//     attempt" is not "no identity";
//   - the payload's expected_terminal is cancelled for run.cancel and expired
//     for the hard deadline (CA-1), read out of the stored payload;
//   - the attempt count of the run is still zero: the attemptless event did not
//     invent an attempt row either.
//
// The control half proves the permission is specific to the queued rules: the
// same caller shape (no attempt, no agent revision) against the §21.1 rows whose
// state event carries the attempt in its identity requirement — every row
// starting at starting or cancelling that writes process.started or
// process.terminated, picked from run.Rules() rather than named here — fails the
// whole transaction. The status is unchanged, no event row was written, both
// sequence counters are exactly where they were, and the error is ErrInvalidEvent
// wrapping *run.IdentityError naming attempt_id; the same transition with the
// attempt supplied then succeeds, so the refusal is the identity rule and not the
// table refusing the row. run.RequiredIdentity is consulted so the test fails
// loudly if the requirement table ever stops requiring the attempt.
//
// One finding is recorded in the control rather than worked around: running
// --process.exited(0)--> is *not* one of those rows. Its state event is
// run.completed, and CA-1's requirement for run.completed is the Run alone
// ("进程此时已退出，attempt 可缺省"), so an attemptless process.exited moves a
// running Run to completed. §21.1's process.exited row demands the attempt as a
// precondition of the fact ("exit code 属于某个 attempt 的进程"), and that
// enforcement belongs to the caller that observed the exit (§19.3 item 4), not to
// the requirement table of the event type the transition writes. The dispatch
// note for this test named process.exited as the control; that premise does not
// hold against the frozen T1.02.a/b contract, and this test says so.
//
// How a wrong implementation could pass a weaker test: a test that only compared
// run.RequiredIdentity(EventRunCancelRequested).Attempt with the boolean false
// would pass while the store refused the transition for some other reason, or
// while it wrote an attempt_id copied from the run's previous event. One that
// asserted "no error" without reading the row would miss a stored attempt_id, and
// one that only checked the caller's input struct would miss a store that
// re-encoded a null attempt into an empty string.
func TestQueuedEventNoAttempt(t *testing.T) {
	// The requirement table itself: the queued rules must not require an attempt,
	// and process.exited must. This is the contract the store is being checked
	// against, not a substitute for the check.
	for _, eventType := range []string{string(run.EventRunCancelRequested), string(run.EventProcessExited)} {
		if _, err := run.RequiredIdentity(eventType); err != nil {
			t.Fatalf("RequiredIdentity(%s): %v", eventType, err)
		}
	}
	if req, err := run.RequiredIdentity(string(run.EventRunCancelRequested)); err != nil || req.Attempt {
		t.Fatalf("RequiredIdentity(run.cancel_requested) = %+v (err %v), want attempt not required", req, err)
	}
	if req, err := run.RequiredIdentity(string(run.EventProcessExited)); err != nil || !req.Attempt {
		t.Fatalf("RequiredIdentity(process.exited) = %+v (err %v), want the attempt required", req, err)
	}

	for _, tc := range []struct {
		name     string
		trigger  run.Trigger
		actor    run.Actor
		expected run.RunStatus
	}{
		{"run.cancel", run.CommandTrigger(run.CommandRunCancel), userActor, run.RunStatusCancelled},
		{"hard_deadline", run.CommandTrigger(run.CommandHardDeadline), systemActor, run.RunStatusExpired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newTransitionFixture(t)
			before := t102cSnapshotOf(t, f.store, f.runID)
			if before.row.Status != run.RunStatusQueued || before.row.Revision != 1 {
				t.Fatalf("fixture run = (%s,%d), want (queued,1)", before.row.Status, before.row.Revision)
			}
			if before.attempts != 0 {
				t.Fatalf("the queued run already has %d attempts; the test would not prove anything", before.attempts)
			}

			// No AttemptID, no AgentRevisionID: exactly the caller shape §28 allows
			// for a queued Run.
			result, err := f.transition(TransitionInput{
				RunID: f.runID, ExpectedRevision: before.row.Revision, ExpectedStatus: run.RunStatusQueued,
				Trigger: tc.trigger, At: transitionAt, Actor: tc.actor,
				Details:   map[string]any{"reason": "queued " + tc.name},
				AttemptID: nil, AgentRevisionID: nil,
			})
			if err != nil {
				t.Fatalf("queued --%s--> cancelling without an attempt: %v", tc.trigger, err)
			}
			if result.Run.Status != run.RunStatusCancelling {
				t.Fatalf("status = %s, want cancelling", result.Run.Status)
			}
			if result.Event == nil || result.Event.Type != string(run.EventRunCancelRequested) {
				t.Fatalf("event = %+v, want run.cancel_requested", result.Event)
			}

			// The stored row, read back with a NULL-capable scan.
			stored := t102cGetEvent(t, f.store, result.Event.ID)
			if t102cRawAttemptID(t, f.store, stored.ID).Valid {
				t.Errorf("events.attempt_id of %s is not SQL NULL, want NULL (a queued run has no attempt)", stored.ID)
			}
			if !t102cJSONKeyAbsentOrNull(t, stored.Identity, "attempt_id") {
				t.Errorf("stored identity %s carries a non-null attempt_id, want the key absent or null", stored.Identity)
			}
			if !t102cJSONKeyAbsentOrNull(t, stored.Identity, "agent_revision_id") {
				t.Errorf("stored identity %s carries a non-null agent_revision_id, want the key absent or null", stored.Identity)
			}
			if got := t102cIdentityValue(t, stored, "run_id"); got != f.runID {
				t.Errorf("stored identity run_id = %v, want %s", got, f.runID)
			}
			if got := t102cIdentityValue(t, stored, "project_id"); got != transitionProjectID {
				t.Errorf("stored identity project_id = %v, want %s", got, transitionProjectID)
			}
			actor, _ := t102cIdentityValue(t, stored, "actor").(map[string]any)
			if actor == nil || actor["type"] != string(tc.actor.Type) || actor["id"] != tc.actor.ID {
				t.Errorf("stored identity actor = %v, want %+v", actor, tc.actor)
			}
			assertPayloadString(t, stored, payloadKeyExpectedTerminal, string(tc.expected))
			assertPayloadString(t, stored, payloadKeyFromStatus, string(run.RunStatusQueued))
			assertPayloadString(t, stored, payloadKeyToStatus, string(run.RunStatusCancelling))
			if got := payloadField(t, stored, "reason"); got != "queued "+tc.name {
				t.Errorf("payload reason = %v, want %q", got, "queued "+tc.name)
			}

			// Nothing invented an attempt row, and the run moved exactly once.
			after := t102cSnapshotOf(t, f.store, f.runID)
			if after.attempts != 0 {
				t.Errorf("attempts after the attemptless transition = %d, want 0", after.attempts)
			}
			if after.row.Status != run.RunStatusCancelling || after.row.Revision != before.row.Revision+1 {
				t.Errorf("row after the transition = (%s,%d), want (cancelling,%d)",
					after.row.Status, after.row.Revision, before.row.Revision+1)
			}
			if after.events != before.events+1 {
				t.Errorf("events = %d, want %d", after.events, before.events+1)
			}
			t.Logf("EVIDENCE %s: run.cancel_requested on a queued run with attempt_id NULL, expected_terminal=%s, attempts=%d",
				tc.name, tc.expected, after.attempts)
		})
	}

	t.Run("attemptless_events_are_refused", func(t *testing.T) {
		// The control: the same caller shape — no attempt, no agent revision —
		// against the two rules whose *state event* requires an attempt
		// (process.started and process.terminated each carry the attempt in their
		// own identity requirements). Both must fail the whole transaction, so
		// "no attempt is fine" is a property of the queued rules and not of a
		// store that is lax about identity.
		//
		// The two rows are walked from the frozen table rather than restated, so
		// this test fails loudly if the requirement table ever stops demanding the
		// attempt: an intermediate state's rows are whatever §21.1 says they are.
		//
		// Note for the record: running --process.exited(0)--> is *not* used here.
		// Its state event is run.completed, and CA-1's identity requirement for
		// run.completed is the Run alone ("进程此时已退出，attempt 可缺省"), so an
		// attemptless process.exited moves a running Run to completed. §21.1's
		// process.exited row demands the attempt as a *precondition* ("exit code
		// 属于某个 attempt 的进程") and on the trigger itself, and that enforcement
		// lives in the caller (§19.3 item 4: the executor observes the exit of the
		// attempt it owns), not in the requirement table — TransitionRunTx checks
		// the requirements of the event type it writes, and for this row that is
		// run.completed. The running-state control is checkpoint.acknowledged,
		// whose own state event does require the attempt. This is reported as a
		// finding, not worked around.
		var covered []string
		for _, from := range []run.RunStatus{
			run.RunStatusStarting, run.RunStatusRunning, run.RunStatusPaused, run.RunStatusRecovering,
		} {
			rows := 0
			for _, rule := range run.Rules() {
				if rule.From != from || !t102cStateEventRequiresAttempt(rule.StateEvent) {
					continue
				}
				rows++
				name := fmt.Sprintf("%s_%s_needs_one", from, rule.Trigger.Name)
				t.Run(name, func(t *testing.T) {
					t102cAssertAttemptRequired(t, from, rule)
				})
				covered = append(covered, name)
			}
			if rows == 0 {
				t.Fatalf("no §21.1 row starts at %s and writes an attempt-requiring state event; "+
					"the control no longer proves what it claims", from)
			}
		}
		if len(covered) != 4 {
			t.Errorf("covered %d attempt-requiring rows (%v), want the four §21.1 rows whose state event carries the attempt",
				len(covered), covered)
		}
		t.Logf("EVIDENCE control: %d attemptless transitions refused (ErrInvalidEvent + attempt_id missing, nothing moved): %s",
			len(covered), strings.Join(covered, ", "))
	})
}

// t102cStateEventRequiresAttempt reports whether an event type's identity
// requirement includes the attempt. The table is the contract (T1.02.a) and this
// is how the control above picks its rows; a type outside the enum cannot be
// looked up, and the rules only ever carry enum members.
func t102cStateEventRequiresAttempt(eventType run.ExecutionEventType) bool {
	req, err := run.RequiredIdentity(string(eventType))
	return err == nil && req.Attempt
}

// t102cAssertAttemptRequired drives the fixture's run to `from` through the
// frozen table, attempts `rule`'s representative trigger without an attempt, and
// asserts the whole transition rolled back. It then shows the refusal is the
// identity rule and not the row being illegal by re-running the same transition
// with the attempt and agent revision set.
//
// The exact trigger is re-used, never the rule's: for the rows whose terminal is
// part of the match (process.terminated, cleaned) a hand-built trigger would have
// to restate that value, and this helper must not be the place a table value gets
// retyped.
func t102cAssertAttemptRequired(t *testing.T, from run.RunStatus, rule run.TransitionRule) {
	t.Helper()
	f := newTransitionFixture(t)
	claimed, err := t102cRunTx(f.store, TransitionInput{
		RunID: f.runID, ExpectedRevision: 1, ExpectedStatus: run.RunStatusQueued,
		Trigger: run.EventTrigger(string(run.EventSchedulerClaimed)),
	})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if claimed.Run.Status != run.RunStatusStarting {
		t.Fatalf("the claimed run is %s, want starting", claimed.Run.Status)
	}
	// The states the four attempt-requiring rows start from, each reached through
	// the §21.1 table: starting is the claim; running is process.started; paused is
	// a checkpoint on a running attempt; recovering is a server restart. The
	// transitions that lead there carry the attempt themselves, which is what the
	// legal half below relies on being possible.
	switch from {
	case run.RunStatusStarting:
		// claimed above
	case run.RunStatusRunning:
		if _, err := t102cRunTx(f.store, TransitionInput{
			RunID: f.runID, ExpectedRevision: claimed.Run.Revision, ExpectedStatus: run.RunStatusStarting,
			Trigger:   run.EventTrigger(string(run.EventProcessStarted)),
			AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr(),
		}); err != nil {
			t.Fatalf("starting --process.started--> running: %v", err)
		}
	case run.RunStatusPaused:
		running, err := t102cRunTx(f.store, TransitionInput{
			RunID: f.runID, ExpectedRevision: claimed.Run.Revision, ExpectedStatus: run.RunStatusStarting,
			Trigger:   run.EventTrigger(string(run.EventProcessStarted)),
			AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr(),
		})
		if err != nil {
			t.Fatalf("starting --process.started--> running: %v", err)
		}
		if _, err := t102cRunTx(f.store, TransitionInput{
			RunID: f.runID, ExpectedRevision: running.Run.Revision, ExpectedStatus: run.RunStatusRunning,
			Trigger:   run.EventTrigger(string(run.EventCheckpointAcknowledged)),
			AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr(),
		}); err != nil {
			t.Fatalf("running --checkpoint.acknowledged--> paused: %v", err)
		}
	case run.RunStatusRecovering:
		if _, err := t102cRunTx(f.store, TransitionInput{
			RunID: f.runID, ExpectedRevision: claimed.Run.Revision, ExpectedStatus: run.RunStatusStarting,
			Trigger: run.EventTrigger(string(run.EventServerRestart)),
		}); err != nil {
			t.Fatalf("starting --server.restart--> recovering: %v", err)
		}
	default:
		t.Fatalf("t102cAssertAttemptRequired: no way to reach %s is defined", from)
	}
	before := t102cSnapshotOf(t, f.store, f.runID)
	if before.row.Status != from {
		t.Fatalf("fixture status = %s, want %s", before.row.Status, from)
	}

	_, err = f.transition(TransitionInput{
		RunID: f.runID, ExpectedRevision: before.row.Revision, ExpectedStatus: from,
		Trigger: rule.Trigger, At: transitionAt, Actor: systemActor,
		Destinations: []string{"ws:run:" + f.runID},
	})
	if !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("%s --%s--> without an attempt = %v, want ErrInvalidEvent",
			from, rule.Trigger, err)
	}
	var identityErr *run.IdentityError
	if !errors.As(err, &identityErr) {
		t.Fatalf("error %v does not wrap *run.IdentityError", err)
	}
	if identityErr.Field != "attempt_id" || !identityErr.Missing {
		t.Errorf("IdentityError = %+v, want attempt_id reported missing", identityErr)
	}
	t102cAssertUnchanged(t, f.store, before, fmt.Sprintf("%s --%s--> without an attempt", from, rule.Trigger))
	t.Logf("EVIDENCE %s --%s--> attemptless: rolled back, the run stayed (%s,%d), events=%d, counters untouched",
		from, rule.Trigger, before.row.Status, before.row.Revision, before.events)

	// The legal form succeeds — same trigger, same row, only the attempt identity
	// supplied — so the refusal above is the identity rule.
	legal, err := f.transition(TransitionInput{
		RunID: f.runID, ExpectedRevision: before.row.Revision, ExpectedStatus: from,
		Trigger: rule.Trigger, At: transitionAt, Actor: systemActor,
		AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr(),
	})
	if err != nil {
		t.Fatalf("%s --%s--> with an attempt and an agent revision: %v", from, rule.Trigger, err)
	}
	if legal.Run.Status != rule.To {
		t.Errorf("status = %s, want the table's %s", legal.Run.Status, rule.To)
	}
	if legal.Event == nil || legal.Event.Type != string(rule.StateEvent) {
		t.Errorf("event = %+v, want the row's state event %s", legal.Event, rule.StateEvent)
	}
	if got := t102cSnapshotOf(t, f.store, f.runID); got.events != before.events+1 {
		t.Errorf("events after the legal transition = %d, want %d", got.events, before.events+1)
	}
}

// t102cGetEvent reads one event row by id through the pool, so the assertions
// above are about persisted state.
func t102cGetEvent(t *testing.T, store *Store, id string) Event {
	t.Helper()
	event, err := GetEvent(context.Background(), store.DB(), id)
	if err != nil {
		t.Fatalf("GetEvent(%s): %v", id, err)
	}
	return event
}

// t102cJSONKeyAbsentOrNull reports whether key is absent from a stored JSON
// object or present with a JSON null value. The store re-encodes the validated
// identity with omitempty, so both spellings are correct for "no attempt"; an
// empty string or any other value is not.
func t102cJSONKeyAbsentOrNull(t *testing.T, raw json.RawMessage, key string) bool {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatalf("decode stored JSON %s: %v", raw, err)
	}
	value, ok := object[key]
	if !ok {
		return true
	}
	return string(value) == "null"
}

// t102cRawAttemptID reads the attempt_id column with a NULL-capable scan, so the
// test can tell SQL NULL from an empty string. It is used by the table-driven
// check above through t102cGetEvent; kept separate so a future test that needs
// the raw column has the same tool.
func t102cRawAttemptID(t *testing.T, store *Store, eventID string) sql.NullString {
	t.Helper()
	var attemptID sql.NullString
	err := store.DB().QueryRowContext(context.Background(),
		`SELECT attempt_id FROM events WHERE id = ?`, eventID).Scan(&attemptID)
	if err != nil {
		t.Fatalf("read attempt_id of event %s: %v", eventID, err)
	}
	return attemptID
}
