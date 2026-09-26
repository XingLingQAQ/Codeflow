package runstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/run"
)

// ---------------------------------------------------------------------------
// Fixture
// ---------------------------------------------------------------------------

// transitionProjectID is the project every transition fixture belongs to. It is
// a different project from the event fixture's so a stray cross-test read is
// visible instead of silently consistent.
const transitionProjectID = "p-transition"

// transitionAt is the instant the fixtures report transitions at. It is a fixed
// millisecond instant so a test can compare stored timestamps exactly.
var transitionAt = time.UnixMilli(1700000100000)

// transitionFixture is a store with one project, one task and one queued run,
// ready for a transition.
type transitionFixture struct {
	t          *testing.T
	store      *Store
	path       string
	runID      string
	taskID     string
	snapshotID string
	// run is the run row as stored by the fixture, so a test compares against
	// what the store actually wrote rather than what it meant to write.
	run run.Run
}

// newTransitionStore opens a migrated Store over a file database in t.TempDir().
//
// It is deliberately not newTestStore: that helper asserts the exact migration
// version, and this card must not pin a version number — T1.11.a is adding
// migrations in the same package concurrently, and a version assertion here
// would turn their in-flight work into this card's failure. Everything this card
// needs from the opener is that the schema is fully applied, which OpenStore
// guarantees before it returns.
func newTransitionStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "codeflow.db")
	store, res, err := OpenStore(context.Background(), path)
	if err != nil {
		t.Fatalf("OpenStore(%s): %v", path, err)
	}
	if res.ToVersion < 5 {
		t.Fatalf("OpenStore migrated to version %d, want at least the T1.02 schema (5)", res.ToVersion)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, path
}

// newTransitionFixture writes one project/task/snapshot/queued-run set through
// the store's own typed operations.
func newTransitionFixture(t *testing.T) *transitionFixture {
	t.Helper()
	store, path := newTransitionStore(t)
	f := &transitionFixture{t: t, store: store, path: path, runID: "run-t1", taskID: "task-t1", snapshotID: "snap-t1"}
	seedTask(t, store, f.taskID, transitionProjectID)
	f.run, _ = insertSnapshotAndRun(t, store, f.runID, f.taskID, transitionProjectID, f.snapshotID,
		json.RawMessage(`{"prompt":"transition"}`))
	return f
}

// systemActor and userActor are the two actors the §27.1 mapping uses in these
// tests: the recoverer and the cancel issuer.
var (
	systemActor = run.Actor{Type: run.ActorTypeSystem, ID: "recoverer-1", Source: "bootstrap"}
	userActor   = run.Actor{Type: run.ActorTypeUser, ID: "user-1", Source: "api"}
)

// transitionInput builds a TransitionInput for the fixture's run with the
// fixture's actor and instant, so a test only states what it is actually varying.
func (f *transitionFixture) transitionInput(expectedRevision int64, expectedStatus run.RunStatus, trigger run.Trigger) TransitionInput {
	return TransitionInput{
		RunID:            f.runID,
		ExpectedRevision: expectedRevision,
		ExpectedStatus:   expectedStatus,
		Trigger:          trigger,
		At:               transitionAt,
		Actor:            systemActor,
	}
}

// transition runs one TransitionRunTx in its own transaction and returns what it
// said. A test that expects success calls mustTransition.
func (f *transitionFixture) transition(in TransitionInput) (TransitionResult, error) {
	f.t.Helper()
	var result TransitionResult
	err := f.store.WithTx(context.Background(), func(ctx context.Context, tx Tx) error {
		var err error
		result, err = TransitionRunTx(ctx, tx, in)
		return err
	})
	return result, err
}

// mustTransition runs one transition and fails the test if it is refused.
func (f *transitionFixture) mustTransition(in TransitionInput) TransitionResult {
	f.t.Helper()
	result, err := f.transition(in)
	if err != nil {
		f.t.Fatalf("TransitionRunTx(%s from %s): %v", in.Trigger, in.ExpectedStatus, err)
	}
	return result
}

// current reads the run row through the pool.
func (f *transitionFixture) current() run.Run {
	f.t.Helper()
	stored, err := GetRun(context.Background(), f.store.DB(), f.runID)
	if err != nil {
		f.t.Fatalf("GetRun(%s): %v", f.runID, err)
	}
	return stored
}

// events reads the run's whole timeline through the run-scoped view.
func (f *transitionFixture) events() []Event {
	f.t.Helper()
	events, _, err := ListRunEvents(context.Background(), f.store.DB(), f.runID, 0, 0)
	if err != nil {
		f.t.Fatalf("ListRunEvents(%s): %v", f.runID, err)
	}
	return events
}

// walk moves the fixture's run through a sequence of transitions, each time
// reading the row so the next input carries the real revision and status.
// (rev, status) pairs are returned as the caller saw them, because that is
// exactly what the next call must pass.
func (f *transitionFixture) walk(steps ...TransitionInput) []TransitionResult {
	f.t.Helper()
	out := make([]TransitionResult, 0, len(steps))
	for _, step := range steps {
		current := f.current()
		if step.RunID == "" {
			step.RunID = f.runID
		}
		step.ExpectedRevision = current.Revision
		step.ExpectedStatus = current.Status
		out = append(out, f.mustTransition(step))
	}
	return out
}

// payloadField reads one field out of a stored state event's payload, failing
// the test when the key is absent. It decodes the stored bytes rather than the
// bytes the caller passed, so it also proves the payload was persisted.
func payloadField(t *testing.T, event Event, key string) any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("event %s payload %s: %v", event.Type, event.Payload, err)
	}
	value, ok := payload[key]
	if !ok {
		t.Fatalf("event %s payload %s has no key %q", event.Type, event.Payload, key)
	}
	return value
}

// assertPayloadString is payloadField for a string-valued key.
func assertPayloadString(t *testing.T, event Event, key, want string) {
	t.Helper()
	if got, _ := payloadField(t, event, key).(string); got != want {
		t.Errorf("event %s payload %s = %q, want %q", event.Type, key, got, want)
	}
}

// assertNoPayloadKey fails when the stored payload carries key at all.
func assertNoPayloadKey(t *testing.T, event Event, key string) {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("event %s payload %s: %v", event.Type, event.Payload, err)
	}
	if value, ok := payload[key]; ok {
		t.Errorf("event %s payload has key %q = %v, want it absent", event.Type, key, value)
	}
}

// attemptPtr and revisionPtr name the attempt-scoped ids the fixtures use.
func attemptPtr() *string { return stringPtr("att-t1") }

func agentRevisionPtr() *string { return stringPtr("agent-rev-t1") }

// ---------------------------------------------------------------------------
// Happy path
// ---------------------------------------------------------------------------

// TestTransitionRunningToCompleted is the §15 T1.02 acceptance case in its
// simplest form: a running Run exits with code 0 and becomes completed in one
// transaction that also writes exactly one run.completed event.
func TestTransitionRunningToCompleted(t *testing.T) {
	f := newTransitionFixture(t)

	// queued --scheduler.claimed--> starting --process.started--> running
	f.walk(
		f.transitionInput(0, "", run.EventTrigger(string(run.EventSchedulerClaimed))),
		TransitionInput{Trigger: run.EventTrigger(string(run.EventProcessStarted)), At: transitionAt, Actor: systemActor,
			AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr()},
	)
	before := f.current()
	if before.Status != run.RunStatusRunning || before.Revision != 3 {
		t.Fatalf("fixture run = (%s,%d), want (running,3)", before.Status, before.Revision)
	}
	eventsBefore := len(f.events())

	result, err := f.transition(TransitionInput{
		RunID:            f.runID,
		ExpectedRevision: before.Revision,
		ExpectedStatus:   before.Status,
		Trigger:          run.ProcessExitedTrigger(0),
		At:               transitionAt.Add(time.Second),
		Actor:            systemActor,
		AttemptID:        attemptPtr(),
		AgentRevisionID:  agentRevisionPtr(),
		Destinations:     []string{"ws:run:" + f.runID, "audit"},
	})
	if err != nil {
		t.Fatalf("running --process.exited(0)--> completed: %v", err)
	}

	// The returned row and the stored row agree, and the row carries the whole
	// transition: status, revision and the finished_at stamp.
	if result.Transition.From != run.RunStatusRunning || result.Transition.To != run.RunStatusCompleted {
		t.Errorf("transition = %s -> %s, want running -> completed", result.Transition.From, result.Transition.To)
	}
	if result.Transition.StateEvent != run.EventRunCompleted {
		t.Errorf("state event = %q, want %q", result.Transition.StateEvent, run.EventRunCompleted)
	}
	if eventRowCount(t, f.store.DB(), "events") != eventsBefore+1 {
		t.Fatalf("event rows = %d, want %d (exactly one event per transition)",
			eventRowCount(t, f.store.DB(), "events"), eventsBefore+1)
	}
	stored := f.current()
	if stored.Status != run.RunStatusCompleted {
		t.Errorf("stored status = %s, want completed", stored.Status)
	}
	if stored.Revision != before.Revision+1 {
		t.Errorf("stored revision = %d, want %d", stored.Revision, before.Revision+1)
	}
	if stored.FinishedAt == nil {
		t.Error("finished_at is nil on a completed run, want the transition instant")
	} else if !stored.FinishedAt.Equal(transitionAt.Add(time.Second)) {
		t.Errorf("finished_at = %s, want %s", stored.FinishedAt, transitionAt.Add(time.Second))
	}
	if !equalRun(result.Run, stored) {
		t.Errorf("returned run %+v differs from the stored row %+v", result.Run, stored)
	}

	// Exactly one event, on the run timeline, with the identity and payload
	// §21.1 requires.
	events := f.events()
	if len(events) != eventsBefore+1 {
		t.Fatalf("run timeline has %d events, want %d", len(events), eventsBefore+1)
	}
	event := events[len(events)-1]
	if event.Type != string(run.EventRunCompleted) {
		t.Fatalf("last event type = %q, want %q", event.Type, run.EventRunCompleted)
	}
	if result.Event == nil || result.Event.ID != event.ID {
		t.Fatalf("returned event = %+v, want the stored %s", result.Event, event.ID)
	}
	if event.RunID == nil || *event.RunID != f.runID {
		t.Errorf("event run_id = %s, want %q", stringPtrString(event.RunID), f.runID)
	}
	if event.AttemptID == nil || *event.AttemptID != *attemptPtr() {
		t.Errorf("event attempt_id = %s, want %q", stringPtrString(event.AttemptID), *attemptPtr())
	}
	// run_seq is consecutive: the fixture wrote claimed(1) and started(2).
	if event.RunSeq == nil || *event.RunSeq != 3 {
		t.Errorf("run_seq = %s, want 3 (third event of the run timeline)", runSeqString(event.RunSeq))
	}
	assertPayloadString(t, event, payloadKeyFromStatus, string(run.RunStatusRunning))
	assertPayloadString(t, event, payloadKeyToStatus, string(run.RunStatusCompleted))
	assertPayloadString(t, event, payloadKeyTrigger, "event:process.exited(exit=0)")

	var identity map[string]any
	if err := json.Unmarshal(event.Identity, &identity); err != nil {
		t.Fatalf("identity %s: %v", event.Identity, err)
	}
	if identity["run_id"] != f.runID || identity["project_id"] != transitionProjectID {
		t.Errorf("identity = %s, want run_id %q and project_id %q", event.Identity, f.runID, transitionProjectID)
	}

	// The destinations became pending outbox rows in the same transaction. Only
	// the completed transition named any, so there are exactly two.
	if n := eventRowCount(t, f.store.DB(), "outbox"); n != 2 {
		t.Errorf("outbox rows = %d, want 2 (one per destination of the completed event)", n)
	}
	t.Logf("EVIDENCE running->completed: revision %d->%d, finished_at=%s, event=%s run_seq=%d",
		before.Revision, stored.Revision, stored.FinishedAt, event.Type, *event.RunSeq)
}

// TestTransitionStateEventPerRow walks one representative transition per state
// event the §21.1 table can write, so the mapping from a transition to its event
// type is covered row by row rather than for one happy path.
func TestTransitionStateEventPerRow(t *testing.T) {
	ctx := context.Background()

	// Each case starts from a fresh store, drives the run to a known (status,
	// revision) pair and then performs the transition under test.
	type step struct {
		name    string
		prepare func(f *transitionFixture) (int64, run.RunStatus)
		trigger func() run.Trigger
		details map[string]any
		attempt bool
		want    run.ExecutionEventType
		wantTo  run.RunStatus
		check   func(t *testing.T, event Event)
	}
	drive := func(f *transitionFixture, status run.RunStatus) (int64, run.RunStatus) {
		f.t.Helper()
		switch status {
		case run.RunStatusQueued:
		case run.RunStatusStarting:
			f.walk(f.transitionInput(0, "", run.EventTrigger(string(run.EventSchedulerClaimed))))
		case run.RunStatusRunning:
			f.walk(
				f.transitionInput(0, "", run.EventTrigger(string(run.EventSchedulerClaimed))),
				TransitionInput{Trigger: run.EventTrigger(string(run.EventProcessStarted)), At: transitionAt, Actor: systemActor,
					AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr()},
			)
		case run.RunStatusWaitingApproval:
			f.walk(
				f.transitionInput(0, "", run.EventTrigger(string(run.EventSchedulerClaimed))),
				TransitionInput{Trigger: run.EventTrigger(string(run.EventProcessStarted)), At: transitionAt, Actor: systemActor,
					AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr()},
				TransitionInput{Trigger: run.EventTrigger(string(run.EventApprovalRequired)), At: transitionAt, Actor: systemActor},
			)
		case run.RunStatusPaused:
			f.walk(
				f.transitionInput(0, "", run.EventTrigger(string(run.EventSchedulerClaimed))),
				TransitionInput{Trigger: run.EventTrigger(string(run.EventProcessStarted)), At: transitionAt, Actor: systemActor,
					AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr()},
				TransitionInput{Trigger: run.EventTrigger(string(run.EventCheckpointAcknowledged)), At: transitionAt, Actor: systemActor,
					AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr()},
			)
		case run.RunStatusCancelling:
			f.walk(
				f.transitionInput(0, "", run.EventTrigger(string(run.EventSchedulerClaimed))),
				TransitionInput{Trigger: run.EventTrigger(string(run.EventProcessStarted)), At: transitionAt, Actor: systemActor,
					AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr()},
				TransitionInput{Trigger: run.CommandTrigger(run.CommandRunCancel), At: transitionAt, Actor: userActor},
			)
		case run.RunStatusRecovering:
			f.walk(
				f.transitionInput(0, "", run.EventTrigger(string(run.EventSchedulerClaimed))),
				TransitionInput{Trigger: run.EventTrigger(string(run.EventProcessStarted)), At: transitionAt, Actor: systemActor,
					AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr()},
				TransitionInput{Trigger: run.EventTrigger(string(run.EventServerRestart)), At: transitionAt, Actor: systemActor},
			)
		case run.RunStatusCompleted:
			f.walk(
				f.transitionInput(0, "", run.EventTrigger(string(run.EventSchedulerClaimed))),
				TransitionInput{Trigger: run.EventTrigger(string(run.EventProcessStarted)), At: transitionAt, Actor: systemActor,
					AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr()},
				TransitionInput{Trigger: run.ProcessExitedTrigger(0), At: transitionAt, Actor: systemActor,
					AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr()},
			)
		default:
			f.t.Fatalf("drive: unsupported preparation %s", status)
		}
		current := f.current()
		return current.Revision, current.Status
	}

	cases := []step{
		{
			name:    "scheduler.claimed",
			prepare: func(f *transitionFixture) (int64, run.RunStatus) { return drive(f, run.RunStatusQueued) },
			trigger: func() run.Trigger { return run.EventTrigger(string(run.EventSchedulerClaimed)) },
			details: map[string]any{"lease_epoch": 1},
			want:    run.EventSchedulerClaimed,
			wantTo:  run.RunStatusStarting,
			check: func(t *testing.T, event Event) {
				assertNoPayloadKey(t, event, payloadKeyExpectedTerminal)
				assertPayloadString(t, event, payloadKeyFailureCode, run.CodeBackendUnavailable)
			},
		},
		{
			name:    "process.started",
			prepare: func(f *transitionFixture) (int64, run.RunStatus) { return drive(f, run.RunStatusStarting) },
			trigger: func() run.Trigger { return run.EventTrigger(string(run.EventProcessStarted)) },
			details: map[string]any{"pid": 4242},
			attempt: true,
			want:    run.EventProcessStarted,
			wantTo:  run.RunStatusRunning,
			check: func(t *testing.T, event Event) {
				if got, _ := payloadField(t, event, "pid").(float64); got != 4242 {
					t.Errorf("payload pid = %v, want 4242", got)
				}
			},
		},
		{
			name:    "approval.required",
			prepare: func(f *transitionFixture) (int64, run.RunStatus) { return drive(f, run.RunStatusRunning) },
			trigger: func() run.Trigger { return run.EventTrigger(string(run.EventApprovalRequired)) },
			details: map[string]any{"approval_id": "ap-1"},
			want:    run.EventApprovalRequired,
			wantTo:  run.RunStatusWaitingApproval,
		},
		{
			name:    "approval.approved",
			prepare: func(f *transitionFixture) (int64, run.RunStatus) { return drive(f, run.RunStatusWaitingApproval) },
			trigger: func() run.Trigger { return run.EventTrigger(string(run.EventApprovalApproved)) },
			details: map[string]any{"approval_id": "ap-1", "decided_by": "user-1"},
			want:    run.EventApprovalApproved,
			wantTo:  run.RunStatusRunning,
		},
		{
			name:    "budget.warning (budget.soft_exceeded)",
			prepare: func(f *transitionFixture) (int64, run.RunStatus) { return drive(f, run.RunStatusRunning) },
			trigger: func() run.Trigger { return run.EventTrigger(string(run.EventBudgetSoftExceeded)) },
			details: map[string]any{"metric": "tokens", "value": 50000},
			want:    run.EventBudgetWarning,
			wantTo:  run.RunStatusRunning,
		},
		{
			name:    "checkpoint.acknowledged",
			prepare: func(f *transitionFixture) (int64, run.RunStatus) { return drive(f, run.RunStatusRunning) },
			trigger: func() run.Trigger { return run.EventTrigger(string(run.EventCheckpointAcknowledged)) },
			details: map[string]any{"checkpoint_id": "ck-1"},
			attempt: true,
			want:    run.EventCheckpointAcknowledged,
			wantTo:  run.RunStatusPaused,
		},
		{
			name:    "run.resumed",
			prepare: func(f *transitionFixture) (int64, run.RunStatus) { return drive(f, run.RunStatusPaused) },
			trigger: func() run.Trigger { return run.CommandTrigger(run.CommandRunResume) },
			attempt: true,
			want:    run.EventRunResumed,
			wantTo:  run.RunStatusRunning,
		},
		{
			name:    "run.cancel_requested (run.cancel)",
			prepare: func(f *transitionFixture) (int64, run.RunStatus) { return drive(f, run.RunStatusRunning) },
			trigger: func() run.Trigger { return run.CommandTrigger(run.CommandRunCancel) },
			details: map[string]any{"reason": "user changed their mind"},
			want:    run.EventRunCancelRequested,
			wantTo:  run.RunStatusCancelling,
			check: func(t *testing.T, event Event) {
				assertPayloadString(t, event, payloadKeyExpectedTerminal, string(run.RunStatusCancelled))
				assertPayloadString(t, event, payloadKeyFailureCode, run.CodeForbidden)
				assertPayloadString(t, event, "reason", "user changed their mind")
			},
		},
		{
			name:    "run.cancel_requested (hard_deadline)",
			prepare: func(f *transitionFixture) (int64, run.RunStatus) { return drive(f, run.RunStatusRunning) },
			trigger: func() run.Trigger { return run.CommandTrigger(run.CommandHardDeadline) },
			details: map[string]any{"reason": "wall deadline 30min"},
			want:    run.EventRunCancelRequested,
			wantTo:  run.RunStatusCancelling,
			check: func(t *testing.T, event Event) {
				assertPayloadString(t, event, payloadKeyExpectedTerminal, string(run.RunStatusExpired))
			},
		},
		{
			name:    "run.cancelled (process.terminated)",
			prepare: func(f *transitionFixture) (int64, run.RunStatus) { return drive(f, run.RunStatusCancelling) },
			trigger: func() run.Trigger { return run.ProcessTerminatedTrigger(run.RunStatusCancelled) },
			details: map[string]any{"grace_seconds": 5, "forced": true},
			attempt: true,
			want:    run.EventRunCancelled,
			wantTo:  run.RunStatusCancelled,
			check: func(t *testing.T, event Event) {
				assertNoPayloadKey(t, event, payloadKeyExpectedTerminal)
				assertPayloadString(t, event, payloadKeyFailureCode, run.CodeProcessKillFailed)
			},
		},
		{
			name: "run.expired (process.terminated with expected expired)",
			prepare: func(f *transitionFixture) (int64, run.RunStatus) {
				rev, status := drive(f, run.RunStatusRunning)
				f.mustTransition(TransitionInput{RunID: f.runID, ExpectedRevision: rev, ExpectedStatus: status,
					Trigger: run.CommandTrigger(run.CommandHardDeadline), At: transitionAt, Actor: systemActor})
				current := f.current()
				return current.Revision, current.Status
			},
			trigger: func() run.Trigger { return run.ProcessTerminatedTrigger(run.RunStatusExpired) },
			attempt: true,
			want:    run.EventRunExpired,
			wantTo:  run.RunStatusExpired,
		},
		{
			name:    "run.recovering (server.restart)",
			prepare: func(f *transitionFixture) (int64, run.RunStatus) { return drive(f, run.RunStatusRunning) },
			trigger: func() run.Trigger { return run.EventTrigger(string(run.EventServerRestart)) },
			details: map[string]any{"owner_instance": "instance-1"},
			want:    run.EventRunRecovering,
			wantTo:  run.RunStatusRecovering,
			check: func(t *testing.T, event Event) {
				assertPayloadString(t, event, payloadKeyFailureCode, run.CodeRecoveryRequired)
			},
		},
		{
			name:    "run.recovering (process_kill_failed)",
			prepare: func(f *transitionFixture) (int64, run.RunStatus) { return drive(f, run.RunStatusCancelling) },
			trigger: func() run.Trigger { return run.ConclusionTrigger(run.ConclusionProcessKillFailed, "") },
			details: map[string]any{"last_error": "taskkill refused"},
			want:    run.EventRunRecovering,
			wantTo:  run.RunStatusRecovering,
		},
		{
			name:    "run.reattached (process_verified)",
			prepare: func(f *transitionFixture) (int64, run.RunStatus) { return drive(f, run.RunStatusRecovering) },
			trigger: func() run.Trigger { return run.ConclusionTrigger(run.ConclusionProcessVerified, "") },
			attempt: true,
			want:    run.EventRunReattached,
			wantTo:  run.RunStatusRunning,
		},
		{
			name:    "run.failed (process.exited non-zero)",
			prepare: func(f *transitionFixture) (int64, run.RunStatus) { return drive(f, run.RunStatusRunning) },
			trigger: func() run.Trigger { return run.ProcessExitedTrigger(3) },
			details: map[string]any{"exit_code": 3, "retryable": true},
			attempt: true,
			want:    run.EventRunFailed,
			wantTo:  run.RunStatusFailed,
			check: func(t *testing.T, event Event) {
				assertNoPayloadKey(t, event, payloadKeyFailureCode)
			},
		},
		{
			name:    "run.failed (process.exited while waiting for approval)",
			prepare: func(f *transitionFixture) (int64, run.RunStatus) { return drive(f, run.RunStatusWaitingApproval) },
			trigger: func() run.Trigger { return run.ProcessExitedTrigger(0) },
			attempt: true,
			want:    run.EventRunFailed,
			wantTo:  run.RunStatusFailed,
		},
		{
			name:    "run.cancelled (cleaned after a queued cancel)",
			prepare: func(f *transitionFixture) (int64, run.RunStatus) { return drive(f, run.RunStatusCancelling) },
			trigger: func() run.Trigger { return run.ConclusionTrigger(run.ConclusionCleaned, run.RunStatusCancelled) },
			details: map[string]any{"process_tree_empty": true},
			want:    run.EventRunCancelled,
			wantTo:  run.RunStatusCancelled,
		},
		{
			name:    "run.failed (cleaned during recovery)",
			prepare: func(f *transitionFixture) (int64, run.RunStatus) { return drive(f, run.RunStatusRecovering) },
			trigger: func() run.Trigger { return run.ConclusionTrigger(run.ConclusionCleaned, run.RunStatusFailed) },
			want:    run.EventRunFailed,
			wantTo:  run.RunStatusFailed,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newTransitionFixture(t)
			revision, status := tc.prepare(f)
			in := TransitionInput{
				RunID:            f.runID,
				ExpectedRevision: revision,
				ExpectedStatus:   status,
				Trigger:          tc.trigger(),
				At:               transitionAt,
				Actor:            systemActor,
				Details:          tc.details,
			}
			if tc.attempt {
				in.AttemptID = attemptPtr()
				in.AgentRevisionID = agentRevisionPtr()
			}
			result, err := f.transition(in)
			if err != nil {
				t.Fatalf("TransitionRunTx(%s from %s): %v", in.Trigger, status, err)
			}
			if result.Transition.StateEvent != tc.want {
				t.Errorf("state event = %q, want %q", result.Transition.StateEvent, tc.want)
			}
			if result.Run.Status != tc.wantTo {
				t.Errorf("status = %s, want %s", result.Run.Status, tc.wantTo)
			}
			if result.Event == nil {
				t.Fatalf("no state event was written, want %s", tc.want)
			}
			if result.Event.Type != string(tc.want) {
				t.Errorf("event type = %q, want %q", result.Event.Type, tc.want)
			}
			assertPayloadString(t, *result.Event, payloadKeyFromStatus, string(status))
			assertPayloadString(t, *result.Event, payloadKeyToStatus, string(tc.wantTo))
			// Exactly one event was added by this transition, and the row moved
			// with it.
			if got := f.current(); got.Revision != revision+1 || got.Status != tc.wantTo {
				t.Errorf("stored row = (%s,%d), want (%s,%d)", got.Status, got.Revision, tc.wantTo, revision+1)
			}
			if tc.check != nil {
				tc.check(t, *result.Event)
			}
			t.Logf("EVIDENCE %s: %s --%s--> %s, event %s, revision %d->%d",
				tc.name, status, in.Trigger, tc.wantTo, result.Event.Type, revision, result.Run.Revision)
		})
	}
	_ = ctx
}

// TestTransitionQueuedCancelAndDeadlineAreAttemptless covers the §28
// "queued 可无 attempt" rule on the cancel path: a queued Run that is cancelled
// (or hits its hard deadline) has never had an attempt, so its
// run.cancel_requested event must be writable without one, and it finishes
// cancelled/expired through the cleaned conclusion.
func TestTransitionQueuedCancelAndDeadlineAreAttemptless(t *testing.T) {
	t.Run("run.cancel", func(t *testing.T) {
		f := newTransitionFixture(t)
		result, err := f.transition(f.transitionInput(1, run.RunStatusQueued, run.CommandTrigger(run.CommandRunCancel)))
		if err != nil {
			t.Fatalf("queued --run.cancel--> cancelling: %v", err)
		}
		if result.Event == nil || result.Event.Type != string(run.EventRunCancelRequested) {
			t.Fatalf("event = %+v, want run.cancel_requested", result.Event)
		}
		if result.Event.AttemptID != nil {
			t.Errorf("event attempt_id = %q, want nil (a queued run has no attempt)", *result.Event.AttemptID)
		}
		assertPayloadString(t, *result.Event, payloadKeyExpectedTerminal, string(run.RunStatusCancelled))

		// The canceller confirms the (empty) process tree and ends the run.
		done := f.mustTransition(TransitionInput{
			RunID: f.runID, ExpectedRevision: result.Run.Revision, ExpectedStatus: run.RunStatusCancelling,
			Trigger: run.ConclusionTrigger(run.ConclusionCleaned, run.RunStatusCancelled),
			At:      transitionAt, Actor: systemActor,
		})
		if done.Run.Status != run.RunStatusCancelled || done.Event == nil || done.Event.Type != string(run.EventRunCancelled) {
			t.Errorf("cleaned = (%s, %+v), want cancelled with run.cancelled", done.Run.Status, done.Event)
		}
	})

	t.Run("hard deadline", func(t *testing.T) {
		f := newTransitionFixture(t)
		result, err := f.transition(f.transitionInput(1, run.RunStatusQueued, run.CommandTrigger(run.CommandHardDeadline)))
		if err != nil {
			t.Fatalf("queued --hard_deadline--> cancelling: %v", err)
		}
		assertPayloadString(t, *result.Event, payloadKeyExpectedTerminal, string(run.RunStatusExpired))
		done := f.mustTransition(TransitionInput{
			RunID: f.runID, ExpectedRevision: result.Run.Revision, ExpectedStatus: run.RunStatusCancelling,
			Trigger: run.ConclusionTrigger(run.ConclusionCleaned, run.RunStatusExpired),
			At:      transitionAt, Actor: systemActor,
		})
		if done.Run.Status != run.RunStatusExpired || done.Event.Type != string(run.EventRunExpired) {
			t.Errorf("cleaned = (%s,%s), want expired with run.expired", done.Run.Status, done.Event.Type)
		}
	})
}

// TestTransitionProcessStartedNeedsAttempt is the rollback half of the identity
// contract: process.started requires an attempt and an agent revision, so a
// caller that forgets one must not get "status moved, event missing" — the whole
// transition rolls back.
func TestTransitionProcessStartedNeedsAttempt(t *testing.T) {
	ctx := context.Background()

	for _, tc := range []struct {
		name            string
		attemptID       *string
		agentRevisionID *string
	}{
		{"no attempt", nil, agentRevisionPtr()},
		{"no agent revision", attemptPtr(), nil},
		{"neither", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newTransitionFixture(t)
			claimed := f.mustTransition(f.transitionInput(1, run.RunStatusQueued,
				run.EventTrigger(string(run.EventSchedulerClaimed))))
			before := f.current()
			eventsBefore := eventRowCount(t, f.store.DB(), "events")
			outboxBefore := eventRowCount(t, f.store.DB(), "outbox")
			projectCounterBefore := counterValue(t, f.store.DB(), projectScope(transitionProjectID))

			_, err := f.transition(TransitionInput{
				RunID:            f.runID,
				ExpectedRevision: claimed.Run.Revision,
				ExpectedStatus:   run.RunStatusStarting,
				Trigger:          run.EventTrigger(string(run.EventProcessStarted)),
				At:               transitionAt,
				Actor:            systemActor,
				AttemptID:        tc.attemptID,
				AgentRevisionID:  tc.agentRevisionID,
				Destinations:     []string{"ws:run:" + f.runID},
			})
			if !errors.Is(err, ErrInvalidEvent) {
				t.Fatalf("error = %v, want ErrInvalidEvent", err)
			}
			var identityErr *run.IdentityError
			if !errors.As(err, &identityErr) {
				t.Errorf("error %v does not wrap *run.IdentityError", err)
			}
			if identityErr != nil && identityErr.Field == "" {
				t.Errorf("identity error %v names no field", identityErr)
			}

			after := f.current()
			if after.Status != before.Status || after.Revision != before.Revision {
				t.Errorf("run = (%s,%d) after the refusal, want (%s,%d) unchanged",
					after.Status, after.Revision, before.Status, before.Revision)
			}
			if n := eventRowCount(t, f.store.DB(), "events"); n != eventsBefore {
				t.Errorf("events = %d, want %d (no state event without a valid identity)", n, eventsBefore)
			}
			if n := eventRowCount(t, f.store.DB(), "outbox"); n != outboxBefore {
				t.Errorf("outbox rows = %d, want %d", n, outboxBefore)
			}
			if got := counterValue(t, f.store.DB(), projectScope(transitionProjectID)); got != projectCounterBefore {
				t.Errorf("project counter = %d, want %d (no sequence consumed)", got, projectCounterBefore)
			}
			t.Logf("EVIDENCE %s: rolled back, run stayed (%s,%d), events=%d, counter=%d",
				tc.name, after.Status, after.Revision, eventRowCount(t, f.store.DB(), "events"),
				counterValue(t, f.store.DB(), projectScope(transitionProjectID)))
		})
	}
	_ = ctx
}

// TestTransitionMergeWritesNoRunEvent covers the one §21.1 row without a state
// event: completed --merge--> completed. The merge creates a MergeOperation and
// the Run timeline must not grow.
func TestTransitionMergeWritesNoRunEvent(t *testing.T) {
	f := newTransitionFixture(t)
	f.walk(
		f.transitionInput(0, "", run.EventTrigger(string(run.EventSchedulerClaimed))),
		TransitionInput{Trigger: run.EventTrigger(string(run.EventProcessStarted)), At: transitionAt, Actor: systemActor,
			AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr()},
		TransitionInput{Trigger: run.ProcessExitedTrigger(0), At: transitionAt, Actor: systemActor,
			AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr()},
	)
	before := f.current()
	if before.Status != run.RunStatusCompleted {
		t.Fatalf("fixture status = %s, want completed", before.Status)
	}
	eventsBefore := eventRowCount(t, f.store.DB(), "events")

	result, err := f.transition(f.transitionInput(before.Revision, run.RunStatusCompleted,
		run.CommandTrigger(run.CommandMerge)))
	if err != nil {
		t.Fatalf("completed --merge--> completed: %v", err)
	}
	if result.Transition.StateEvent != "" {
		t.Errorf("merge transition names state event %q, want none", result.Transition.StateEvent)
	}
	if result.Event != nil {
		t.Errorf("merge wrote event %+v, want nil (the MergeOperation writes merge.completed itself)", result.Event)
	}
	if result.Run.Status != run.RunStatusCompleted {
		t.Errorf("status after merge = %s, want completed", result.Run.Status)
	}
	// The row still moved (a revision bump records that the merge command was
	// accepted), but no event did.
	if result.Run.Revision != before.Revision+1 {
		t.Errorf("revision after merge = %d, want %d", result.Run.Revision, before.Revision+1)
	}
	if n := eventRowCount(t, f.store.DB(), "events"); n != eventsBefore {
		t.Errorf("events after merge = %d, want %d (no run state event for a merge)", n, eventsBefore)
	}
}

// ---------------------------------------------------------------------------
// Conflicts
// ---------------------------------------------------------------------------

// TestTransitionStaleRevisionConflicts proves the CAS half: a caller whose
// revision is stale gets the typed conflict carrying the stored revision and
// status, and nothing is written.
func TestTransitionStaleRevisionConflicts(t *testing.T) {
	f := newTransitionFixture(t)
	claimed := f.mustTransition(f.transitionInput(1, run.RunStatusQueued,
		run.EventTrigger(string(run.EventSchedulerClaimed))))
	before := f.current()
	if before.Revision != claimed.Run.Revision {
		t.Fatalf("fixture revision = %d, want %d", before.Revision, claimed.Run.Revision)
	}
	eventsBefore := eventRowCount(t, f.store.DB(), "events")

	// Stale revision *and* stale status: exactly what a caller that read the run
	// before the claim holds.
	_, err := f.transition(TransitionInput{
		RunID:            f.runID,
		ExpectedRevision: 1,
		ExpectedStatus:   run.RunStatusQueued,
		Trigger:          run.EventTrigger(string(run.EventProcessStarted)),
		At:               transitionAt,
		Actor:            systemActor,
		AttemptID:        attemptPtr(),
		AgentRevisionID:  agentRevisionPtr(),
	})
	var conflict *RevisionConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("error = %v, want *RevisionConflictError", err)
	}
	if !errors.Is(err, ErrRevisionConflict) {
		t.Errorf("error %v does not wrap ErrRevisionConflict", err)
	}
	if conflict.Expected != 1 || conflict.Current != before.Revision {
		t.Errorf("conflict = expected %d current %d, want expected 1 current %d",
			conflict.Expected, conflict.Current, before.Revision)
	}
	if conflict.CurrentStatus != before.Status {
		t.Errorf("conflict status = %s, want %s", conflict.CurrentStatus, before.Status)
	}
	if conflict.RunID != f.runID {
		t.Errorf("conflict run = %s, want %s", conflict.RunID, f.runID)
	}

	after := f.current()
	if !equalRun(after, before) {
		t.Errorf("run changed on a conflict:\n before %+v\n after  %+v", before, after)
	}
	if n := eventRowCount(t, f.store.DB(), "events"); n != eventsBefore {
		t.Errorf("events = %d, want %d", n, eventsBefore)
	}
	t.Logf("EVIDENCE stale revision: conflict expected=%d current=%d status=%s, row unchanged",
		conflict.Expected, conflict.Current, conflict.CurrentStatus)
}

// TestTransitionStatusMismatchConflicts proves the from-status half of the CAS.
// The revision matches but the status the caller decided on does not, which is
// exactly what a caller holds after it read a row, something else moved it, and
// it retried with a fresh revision but the status it originally decided on.
func TestTransitionStatusMismatchConflicts(t *testing.T) {
	f := newTransitionFixture(t)

	// Drive the run to running, then read it: this is the (revision, status)
	// pair the caller's decision is valid for.
	f.walk(
		f.transitionInput(0, "", run.EventTrigger(string(run.EventSchedulerClaimed))),
		TransitionInput{Trigger: run.EventTrigger(string(run.EventProcessStarted)), At: transitionAt, Actor: systemActor,
			AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr()},
	)
	current := f.current()
	if current.Status != run.RunStatusRunning {
		t.Fatalf("fixture status = %s, want running", current.Status)
	}

	// The caller now reports the *current* revision but the *old* status: the
	// revision check alone would pass, so only the from-status half of the CAS
	// can refuse this.
	eventsBefore := eventRowCount(t, f.store.DB(), "events")
	_, err := f.transition(TransitionInput{
		RunID:            f.runID,
		ExpectedRevision: current.Revision,
		ExpectedStatus:   run.RunStatusStarting,
		Trigger:          run.ProcessExitedTrigger(0),
		At:               transitionAt,
		Actor:            systemActor,
		AttemptID:        attemptPtr(),
		AgentRevisionID:  agentRevisionPtr(),
	})
	var conflict *RevisionConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("error = %v, want *RevisionConflictError", err)
	}
	if conflict.CurrentStatus != run.RunStatusRunning || conflict.Current != current.Revision {
		t.Errorf("conflict = (current %d, status %s), want (%d, running)",
			conflict.Current, conflict.CurrentStatus, current.Revision)
	}
	if conflict.Expected != current.Revision {
		t.Errorf("conflict expected = %d, want %d (the revision did match)", conflict.Expected, current.Revision)
	}
	after := f.current()
	if !equalRun(after, current) {
		t.Errorf("run changed on a status conflict:\n before %+v\n after  %+v", current, after)
	}
	if n := eventRowCount(t, f.store.DB(), "events"); n != eventsBefore {
		t.Errorf("events = %d, want %d", n, eventsBefore)
	}
	// From the run's *real* status the very same trigger is legal, so the
	// refusal really is the from-status CAS and not the transition table.
	if _, decideErr := run.Decide(run.RunStatusRunning, run.ProcessExitedTrigger(0)); decideErr != nil {
		t.Fatalf("running --process.exited(0)--> is not legal in the table (%v); the test proves nothing", decideErr)
	}
	t.Logf("EVIDENCE status mismatch: revision %d matched, status starting != running -> conflict, row unchanged",
		current.Revision)
}

// TestTransitionTerminalRunCannotAdvance is the "终态 advance" acceptance case:
// no trigger of §21.1 leaves completed, failed, cancelled or expired, so every
// attempt is an invalid transition and writes nothing. It is the store-level
// half of T1.02.c's TestTerminalRunCannotReopen.
func TestTransitionTerminalRunCannotAdvance(t *testing.T) {
	ctx := context.Background()

	// How to reach each terminal state, and the actor that does it.
	type terminal struct {
		name  string
		setup func(f *transitionFixture) run.Run
	}
	terminals := []terminal{
		{"completed", func(f *transitionFixture) run.Run {
			f.walk(
				f.transitionInput(0, "", run.EventTrigger(string(run.EventSchedulerClaimed))),
				TransitionInput{Trigger: run.EventTrigger(string(run.EventProcessStarted)), At: transitionAt, Actor: systemActor,
					AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr()},
				TransitionInput{Trigger: run.ProcessExitedTrigger(0), At: transitionAt, Actor: systemActor,
					AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr()},
			)
			return f.current()
		}},
		{"failed", func(f *transitionFixture) run.Run {
			f.walk(
				f.transitionInput(0, "", run.EventTrigger(string(run.EventSchedulerClaimed))),
				TransitionInput{Trigger: run.EventTrigger(string(run.EventProcessStarted)), At: transitionAt, Actor: systemActor,
					AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr()},
				TransitionInput{Trigger: run.ProcessExitedTrigger(2), At: transitionAt, Actor: systemActor,
					AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr()},
			)
			return f.current()
		}},
		{"cancelled", func(f *transitionFixture) run.Run {
			claimed := f.mustTransition(f.transitionInput(1, run.RunStatusQueued,
				run.CommandTrigger(run.CommandRunCancel)))
			return f.mustTransition(TransitionInput{
				RunID: f.runID, ExpectedRevision: claimed.Run.Revision, ExpectedStatus: run.RunStatusCancelling,
				Trigger: run.ConclusionTrigger(run.ConclusionCleaned, run.RunStatusCancelled),
				At:      transitionAt, Actor: systemActor,
			}).Run
		}},
		{"expired", func(f *transitionFixture) run.Run {
			claimed := f.mustTransition(f.transitionInput(1, run.RunStatusQueued,
				run.CommandTrigger(run.CommandHardDeadline)))
			return f.mustTransition(TransitionInput{
				RunID: f.runID, ExpectedRevision: claimed.Run.Revision, ExpectedStatus: run.RunStatusCancelling,
				Trigger: run.ConclusionTrigger(run.ConclusionCleaned, run.RunStatusExpired),
				At:      transitionAt, Actor: systemActor,
			}).Run
		}},
	}

	// Every trigger vocabulary the table speaks, applied to a terminal run.
	triggers := []run.Trigger{
		run.EventTrigger(string(run.EventSchedulerClaimed)),
		run.EventTrigger(string(run.EventProcessStarted)),
		run.EventTrigger(string(run.EventApprovalRequired)),
		run.EventTrigger(string(run.EventApprovalApproved)),
		run.EventTrigger(string(run.EventBudgetSoftExceeded)),
		run.EventTrigger(string(run.EventCheckpointAcknowledged)),
		run.ProcessExitedTrigger(0),
		run.ProcessExitedTrigger(1),
		run.ProcessTerminatedTrigger(run.RunStatusCancelled),
		run.ProcessTerminatedTrigger(run.RunStatusExpired),
		run.EventTrigger(string(run.EventServerRestart)),
		run.CommandTrigger(run.CommandRunCancel),
		run.CommandTrigger(run.CommandRunResume),
		run.CommandTrigger(run.CommandHardDeadline),
		run.ConclusionTrigger(run.ConclusionProcessVerified, ""),
		run.ConclusionTrigger(run.ConclusionProcessKillFailed, ""),
		run.ConclusionTrigger(run.ConclusionCleaned, run.RunStatusCancelled),
		run.ConclusionTrigger(run.ConclusionCleaned, run.RunStatusFailed),
	}

	for _, tc := range terminals {
		t.Run(tc.name, func(t *testing.T) {
			for _, tcTrigger := range triggers {
				// merge is the one command a terminal run may receive, and only
				// from completed. Every other (terminal, trigger) pair must be
				// refused.
				trigger := tcTrigger
				if trigger.Kind == run.TriggerCommand && trigger.Name == run.CommandMerge {
					continue
				}
				f := newTransitionFixture(t)
				terminal := tc.setup(f)
				if terminal.Status != run.RunStatus(tc.name) {
					t.Fatalf("setup produced %s, want %s", terminal.Status, tc.name)
				}
				eventsBefore := eventRowCount(t, f.store.DB(), "events")

				_, err := f.transition(TransitionInput{
					RunID:            f.runID,
					ExpectedRevision: terminal.Revision,
					ExpectedStatus:   terminal.Status,
					Trigger:          trigger,
					At:               transitionAt,
					Actor:            systemActor,
					AttemptID:        attemptPtr(),
					AgentRevisionID:  agentRevisionPtr(),
					Destinations:     []string{"ws:run:" + f.runID},
				})
				if !errors.Is(err, ErrInvalidTransition) {
					t.Fatalf("%s --%s--> : error = %v, want ErrInvalidTransition", tc.name, trigger, err)
				}
				var transitionErr *run.TransitionError
				if !errors.As(err, &transitionErr) {
					t.Fatalf("%s --%s--> : error %v does not wrap *run.TransitionError", tc.name, trigger, err)
				}
				if transitionErr.From != terminal.Status {
					t.Errorf("%s --%s--> : TransitionError.From = %s, want %s",
						tc.name, trigger, transitionErr.From, terminal.Status)
				}

				after := f.current()
				if !equalRun(after, terminal) {
					t.Errorf("%s --%s--> changed the row:\n before %+v\n after  %+v",
						tc.name, trigger, terminal, after)
				}
				if n := eventRowCount(t, f.store.DB(), "events"); n != eventsBefore {
					t.Errorf("%s --%s--> wrote an event (%d -> %d)", tc.name, trigger, eventsBefore, n)
				}
			}
		})
	}
	_ = ctx
}

// TestTransitionRepeatedCancel covers §15's "重复 cancel" case: the first cancel
// of a running run moves it to cancelling; a second attempt with the old
// revision is a conflict; a second run.cancel with the *current* revision is a
// §21.1 question, and the table has no cancelling --run.cancel--> row, so it is
// an invalid transition and the run stays cancelling (it is not "cancelled
// twice" and it is not resurrected).
func TestTransitionRepeatedCancel(t *testing.T) {
	f := newTransitionFixture(t)
	f.walk(
		f.transitionInput(0, "", run.EventTrigger(string(run.EventSchedulerClaimed))),
		TransitionInput{Trigger: run.EventTrigger(string(run.EventProcessStarted)), At: transitionAt, Actor: systemActor,
			AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr()},
	)
	running := f.current()

	first, err := f.transition(TransitionInput{
		RunID: f.runID, ExpectedRevision: running.Revision, ExpectedStatus: run.RunStatusRunning,
		Trigger: run.CommandTrigger(run.CommandRunCancel), At: transitionAt, Actor: userActor,
		Details: map[string]any{"reason": "user pressed cancel"},
	})
	if err != nil {
		t.Fatalf("first cancel: %v", err)
	}
	if first.Run.Status != run.RunStatusCancelling || first.Run.Revision != running.Revision+1 {
		t.Fatalf("first cancel = (%s,%d), want (cancelling,%d)",
			first.Run.Status, first.Run.Revision, running.Revision+1)
	}
	eventsAfterFirst := eventRowCount(t, f.store.DB(), "events")

	// The same request again: stale revision, stale status -> conflict, and the
	// state event of the first cancel is the only one.
	_, err = f.transition(TransitionInput{
		RunID: f.runID, ExpectedRevision: running.Revision, ExpectedStatus: run.RunStatusRunning,
		Trigger: run.CommandTrigger(run.CommandRunCancel), At: transitionAt, Actor: userActor,
	})
	var conflict *RevisionConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("second cancel with the old revision: error = %v, want *RevisionConflictError", err)
	}
	if conflict.CurrentStatus != run.RunStatusCancelling {
		t.Errorf("conflict status = %s, want cancelling", conflict.CurrentStatus)
	}
	if n := eventRowCount(t, f.store.DB(), "events"); n != eventsAfterFirst {
		t.Errorf("events = %d, want %d", n, eventsAfterFirst)
	}

	// A caller that re-read and tried again: cancelling --run.cancel--> has no
	// §21.1 row.
	_, err = f.transition(TransitionInput{
		RunID: f.runID, ExpectedRevision: first.Run.Revision, ExpectedStatus: run.RunStatusCancelling,
		Trigger: run.CommandTrigger(run.CommandRunCancel), At: transitionAt, Actor: userActor,
	})
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("cancel of a cancelling run: error = %v, want ErrInvalidTransition", err)
	}
	var transitionErr *run.TransitionError
	if !errors.As(err, &transitionErr) {
		t.Errorf("error %v does not wrap *run.TransitionError", err)
	}
	after := f.current()
	if after.Status != run.RunStatusCancelling || after.Revision != first.Run.Revision {
		t.Errorf("run = (%s,%d) after the refused repeat, want (cancelling,%d)",
			after.Status, after.Revision, first.Run.Revision)
	}
	if n := eventRowCount(t, f.store.DB(), "events"); n != eventsAfterFirst {
		t.Errorf("events = %d, want %d (the refused repeat wrote nothing)", n, eventsAfterFirst)
	}
	t.Logf("EVIDENCE repeated cancel: first (running,%d)->(cancelling,%d), stale repeat conflict current=%d, "+
		"re-read repeat ErrInvalidTransition, events=%d",
		running.Revision, first.Run.Revision, conflict.Current, eventsAfterFirst)
}

// TestTransitionCancelFinishRace is the §27.2.5/§27.2.6 single-winner race: two
// store handles over the same file race one run.cancel against one
// process.exited(0) from the same expected revision, 20 times over. Exactly one
// wins each round, the loser gets a conflict, the run ends in the winner's
// target state, and a loser that re-reads and finds a terminal state can never
// cancel it back.
func TestTransitionCancelFinishRace(t *testing.T) {
	ctx := context.Background()
	f := newTransitionFixture(t)

	// A second handle on the same file: the race must be between two real
	// connections, not two goroutines sharing one *sql.DB.
	second, _, err := OpenStore(ctx, f.path)
	if err != nil {
		t.Fatalf("OpenStore(%s): %v", f.path, err)
	}
	t.Cleanup(func() { _ = second.Close() })

	const rounds = 20
	for round := 0; round < rounds; round++ {
		// Start each round from running, in a fresh run of its own so the rounds
		// are independent and the assertion "exactly one state event" is exact.
		runID := fmt.Sprintf("run-race-%d", round)
		snapshotID := fmt.Sprintf("snap-race-%d", round)
		taskID := fmt.Sprintf("task-race-%d", round)
		seedTask(t, f.store, taskID, transitionProjectID)
		insertSnapshotAndRun(t, f.store, runID, taskID, transitionProjectID, snapshotID,
			json.RawMessage(`{"prompt":"race"}`))

		running := func(store *Store) run.Run {
			var result TransitionResult
			err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
				var err error
				result, err = TransitionRunTx(ctx, tx, TransitionInput{
					RunID: runID, ExpectedRevision: 1, ExpectedStatus: run.RunStatusQueued,
					Trigger: run.EventTrigger(string(run.EventSchedulerClaimed)), At: transitionAt, Actor: systemActor,
				})
				return err
			})
			if err != nil {
				t.Fatalf("round %d: claim: %v", round, err)
			}
			err = store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
				var err error
				result, err = TransitionRunTx(ctx, tx, TransitionInput{
					RunID: runID, ExpectedRevision: result.Run.Revision, ExpectedStatus: run.RunStatusStarting,
					Trigger: run.EventTrigger(string(run.EventProcessStarted)), At: transitionAt, Actor: systemActor,
					AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr(),
				})
				return err
			})
			if err != nil {
				t.Fatalf("round %d: start: %v", round, err)
			}
			return result.Run
		}
		before := running(f.store)
		if before.Status != run.RunStatusRunning {
			t.Fatalf("round %d: fixture status = %s, want running", round, before.Status)
		}

		// The two racers, released together. Each carries the same expectation:
		// the (revision, status) pair both read.
		type racer struct {
			name    string
			store   *Store
			trigger run.Trigger
			actor   run.Actor
			details map[string]any
		}
		racers := []racer{
			{name: "cancel", store: f.store, trigger: run.CommandTrigger(run.CommandRunCancel),
				actor: userActor, details: map[string]any{"reason": "race"}},
			{name: "finish", store: second, trigger: run.ProcessExitedTrigger(0),
				actor: systemActor, details: map[string]any{"exit_code": 0}},
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
				in := TransitionInput{
					RunID: runID, ExpectedRevision: before.Revision, ExpectedStatus: before.Status,
					Trigger: r.trigger, At: transitionAt, Actor: r.actor, Details: r.details,
					Destinations: []string{"ws:run:" + runID},
				}
				if r.trigger.Name == string(run.EventProcessExited) {
					in.AttemptID = attemptPtr()
					in.AgentRevisionID = agentRevisionPtr()
				}
				<-start
				var result TransitionResult
				err := r.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
					var err error
					result, err = TransitionRunTx(ctx, tx, in)
					return err
				})
				results <- outcome{name: r.name, result: result, err: err}
			}(r)
		}
		close(start)
		wg.Wait()
		close(results)

		// Drain deterministically: the channel holds exactly two outcomes. Each
		// is taken by value into a local before its address is kept, so winner
		// and loser do not both alias the range variable.
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
				out := out
				loser = &out
			default:
				t.Fatalf("round %d: %s failed with %v, want a revision conflict", round, out.name, out.err)
			}
		}
		if winner == nil || loser == nil {
			t.Fatalf("round %d: winner=%v loser=%v, want exactly one of each", round, winner, loser)
		}
		// No BUSY-family failure may appear: the immediate transaction mode must
		// serialise the two writers.
		if code := sqliteExtended(winner.err); code == 5 || code == 517 || code == 6 {
			t.Fatalf("round %d: winner hit a SQLITE_BUSY-family error (%d)", round, code)
		}

		// The final state is the winner's target, exactly one revision bump and
		// exactly one state event beyond the two setup events.
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
		events, _, err := ListRunEvents(ctx, f.store.DB(), runID, 0, 0)
		if err != nil {
			t.Fatalf("round %d: ListRunEvents: %v", round, err)
		}
		if len(events) != 3 {
			t.Errorf("round %d: run timeline has %d events, want 3 (claim, start, one winner)", round, len(events))
		}
		// The loser's event is absent, and the winner's is present.
		if last := events[len(events)-1]; last.Type != string(winner.result.Transition.StateEvent) {
			t.Errorf("round %d: last event = %s, want the winner's %s",
				round, last.Type, winner.result.Transition.StateEvent)
		}

		// The loser re-reads and tries to cancel. If the winner finished the run,
		// the cancel must be refused as an invalid transition: a terminal run is
		// not resurrected.
		if stored.Status == run.RunStatusCompleted {
			_, err := f.transition(TransitionInput{
				RunID: runID, ExpectedRevision: stored.Revision, ExpectedStatus: stored.Status,
				Trigger: run.CommandTrigger(run.CommandRunCancel), At: transitionAt, Actor: userActor,
			})
			if !errors.Is(err, ErrInvalidTransition) {
				t.Errorf("round %d: cancel of a completed run = %v, want ErrInvalidTransition", round, err)
			}
			after, err := GetRun(ctx, f.store.DB(), runID)
			if err != nil {
				t.Fatalf("round %d: GetRun after the refused cancel: %v", round, err)
			}
			if after.Status != run.RunStatusCompleted {
				t.Errorf("round %d: status after the refused cancel = %s, want completed", round, after.Status)
			}
		}
		t.Logf("EVIDENCE round %d: winner=%s -> %s, loser=%s conflict (current %d/%s), events=%d",
			round, winner.name, stored.Status, loser.name, stored.Revision, stored.Status, len(events))
	}
}

// ---------------------------------------------------------------------------
// Rollback and restart
// ---------------------------------------------------------------------------

// TestTransitionRollsBackWithCaller is the "caller's later step failed" case: the
// transition succeeded inside the transaction and the caller then returned an
// error, so the status, the revision, the event and the sequence numbers must
// all be exactly as they were.
func TestTransitionRollsBackWithCaller(t *testing.T) {
	ctx := context.Background()
	f := newTransitionFixture(t)
	claimed := f.mustTransition(f.transitionInput(1, run.RunStatusQueued,
		run.EventTrigger(string(run.EventSchedulerClaimed))))
	before := f.current()

	eventsBefore := eventRowCount(t, f.store.DB(), "events")
	outboxBefore := eventRowCount(t, f.store.DB(), "outbox")
	projectCounterBefore := counterValue(t, f.store.DB(), projectScope(transitionProjectID))
	runCounterBefore := counterValue(t, f.store.DB(), runScope(f.runID))

	sentinel := errors.New("a later step in the same unit of work refused")
	err := f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		if _, err := TransitionRunTx(ctx, tx, TransitionInput{
			RunID: f.runID, ExpectedRevision: claimed.Run.Revision, ExpectedStatus: run.RunStatusStarting,
			Trigger: run.EventTrigger(string(run.EventProcessStarted)), At: transitionAt, Actor: systemActor,
			AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr(),
			Destinations: []string{"ws:run:" + f.runID},
		}); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("transaction error = %v, want the caller's sentinel", err)
	}

	after := f.current()
	if !equalRun(after, before) {
		t.Errorf("run after rollback = %+v, want %+v", after, before)
	}
	if n := eventRowCount(t, f.store.DB(), "events"); n != eventsBefore {
		t.Errorf("events = %d, want %d", n, eventsBefore)
	}
	if n := eventRowCount(t, f.store.DB(), "outbox"); n != outboxBefore {
		t.Errorf("outbox rows = %d, want %d", n, outboxBefore)
	}
	if got := counterValue(t, f.store.DB(), projectScope(transitionProjectID)); got != projectCounterBefore {
		t.Errorf("project counter = %d, want %d (a rolled-back transition consumes no sequence)", got, projectCounterBefore)
	}
	if got := counterValue(t, f.store.DB(), runScope(f.runID)); got != runCounterBefore {
		t.Errorf("run counter = %d, want %d", got, runCounterBefore)
	}

	// The next transition continues the sequence without a gap, and the run is
	// still in the state the caller read.
	next := f.mustTransition(TransitionInput{
		RunID: f.runID, ExpectedRevision: before.Revision, ExpectedStatus: run.RunStatusStarting,
		Trigger: run.EventTrigger(string(run.EventProcessStarted)), At: transitionAt, Actor: systemActor,
		AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr(),
	})
	if next.Run.Status != run.RunStatusRunning || next.Event == nil {
		t.Fatalf("transition after the rollback = (%s,%+v), want running with an event", next.Run.Status, next.Event)
	}
	if next.Event.ProjectSeq != projectCounterBefore+1 {
		t.Errorf("project_seq after the rollback = %d, want %d", next.Event.ProjectSeq, projectCounterBefore+1)
	}
	t.Logf("EVIDENCE rollback: run stayed (%s,%d), events=%d, counters %d/%d, next project_seq=%d",
		after.Status, after.Revision, eventsBefore, projectCounterBefore, runCounterBefore, next.Event.ProjectSeq)
}

// TestTransitionSurvivesReopen closes the store and reopens the file: the status
// and the state event must both still be there, and the run's timeline must
// continue where it left off.
func TestTransitionSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	f := newTransitionFixture(t)

	result, err := f.transition(TransitionInput{
		RunID: f.runID, ExpectedRevision: 1, ExpectedStatus: run.RunStatusQueued,
		Trigger: run.EventTrigger(string(run.EventSchedulerClaimed)), At: transitionAt, Actor: systemActor,
		Destinations: []string{"ws:project:" + transitionProjectID},
	})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
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

	stored, err := GetRun(ctx, reopened.DB(), f.runID)
	if err != nil {
		t.Fatalf("GetRun after reopen: %v", err)
	}
	if !equalRun(stored, result.Run) {
		t.Errorf("run after reopen = %+v, want %+v", stored, result.Run)
	}
	events, _, err := ListRunEvents(ctx, reopened.DB(), f.runID, 0, 0)
	if err != nil {
		t.Fatalf("ListRunEvents after reopen: %v", err)
	}
	if len(events) != 1 || events[0].Type != string(run.EventSchedulerClaimed) {
		t.Fatalf("events after reopen = %+v, want one scheduler.claimed", events)
	}
	if result.Event == nil || events[0].ID != result.Event.ID {
		t.Errorf("event id after reopen = %s, want %s", events[0].ID, result.Event.ID)
	}
	if string(events[0].Payload) != string(result.Event.Payload) {
		t.Errorf("payload after reopen = %s, want %s", events[0].Payload, result.Event.Payload)
	}

	// The next transition continues from the stored revision and counters.
	var next TransitionResult
	err = reopened.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		var err error
		next, err = TransitionRunTx(ctx, tx, TransitionInput{
			RunID: f.runID, ExpectedRevision: stored.Revision, ExpectedStatus: stored.Status,
			Trigger: run.EventTrigger(string(run.EventProcessStarted)), At: transitionAt, Actor: systemActor,
			AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr(),
		})
		return err
	})
	if err != nil {
		t.Fatalf("transition after reopen: %v", err)
	}
	if next.Run.Status != run.RunStatusRunning {
		t.Errorf("status after reopen = %s, want running", next.Run.Status)
	}
	if next.Event == nil || next.Event.RunSeq == nil || *next.Event.RunSeq != 2 {
		t.Errorf("run_seq after reopen = %+v, want 2", next.Event)
	}
	t.Logf("EVIDENCE reopen: run (%s,%d), event %s, next run_seq=%s",
		stored.Status, stored.Revision, events[0].Type, runSeqString(next.Event.RunSeq))
}

// ---------------------------------------------------------------------------
// Input validation
// ---------------------------------------------------------------------------

// TestTransitionRejectsInvalidInput walks the input contract: every required
// field is checked before any SQL runs, so a rejected call leaves the run, the
// event stream and the counters exactly as they were.
func TestTransitionRejectsInvalidInput(t *testing.T) {
	f := newTransitionFixture(t)
	before := f.current()

	base := f.transitionInput(1, run.RunStatusQueued, run.EventTrigger(string(run.EventSchedulerClaimed)))

	cases := []struct {
		name   string
		mutate func(in *TransitionInput)
		want   string
	}{
		{"empty run id", func(in *TransitionInput) { in.RunID = "" }, "RunID"},
		{"blank run id", func(in *TransitionInput) { in.RunID = "   " }, "RunID"},
		{"zero expected revision", func(in *TransitionInput) { in.ExpectedRevision = 0 }, "ExpectedRevision"},
		{"negative expected revision", func(in *TransitionInput) { in.ExpectedRevision = -1 }, "ExpectedRevision"},
		{"invalid expected status", func(in *TransitionInput) { in.ExpectedStatus = "finished" }, "ExpectedStatus"},
		{"empty expected status", func(in *TransitionInput) { in.ExpectedStatus = "" }, "ExpectedStatus"},
		{"zero at", func(in *TransitionInput) { in.At = time.Time{} }, "At"},
		{"invalid actor type", func(in *TransitionInput) { in.Actor.Type = "robot" }, "Actor.Type"},
		{"empty actor type", func(in *TransitionInput) { in.Actor.Type = "" }, "Actor.Type"},
		{"empty actor id", func(in *TransitionInput) { in.Actor.ID = "" }, "Actor.ID"},
		{"blank actor id", func(in *TransitionInput) { in.Actor.ID = " " }, "Actor.ID"},
		{"actor source too long", func(in *TransitionInput) {
			in.Actor.Source = strings.Repeat("s", run.MaxActorSourceLength+1)
		}, "Actor.Source"},
		{"empty attempt id", func(in *TransitionInput) { in.AttemptID = stringPtr("") }, "AttemptID"},
		{"empty agent revision id", func(in *TransitionInput) { in.AgentRevisionID = stringPtr("  ") }, "AgentRevisionID"},
		{"reserved from_status", func(in *TransitionInput) {
			in.Details = map[string]any{payloadKeyFromStatus: "queued"}
		}, "from_status"},
		{"reserved key in another case", func(in *TransitionInput) {
			in.Details = map[string]any{"FROM_STATUS": "queued"}
		}, "FROM_STATUS"},
		{"reserved expected_terminal", func(in *TransitionInput) {
			in.Details = map[string]any{"Expected_Terminal": "cancelled"}
		}, "Expected_Terminal"},
		{"details not JSON-encodable", func(in *TransitionInput) {
			in.Details = map[string]any{"bad": make(chan int)}
		}, "JSON-encodable"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := base
			tc.mutate(&in)
			_, err := f.transition(in)
			if !errors.Is(err, ErrInvalidTransitionInput) {
				t.Fatalf("error = %v, want ErrInvalidTransitionInput", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not name %q", err, tc.want)
			}
			after := f.current()
			if !equalRun(after, before) {
				t.Errorf("run changed on an invalid input:\n before %+v\n after  %+v", before, after)
			}
			if n := eventRowCount(t, f.store.DB(), "events"); n != 0 {
				t.Errorf("events = %d, want 0", n)
			}
			if got := counterValue(t, f.store.DB(), projectScope(transitionProjectID)); got != -1 {
				t.Errorf("project counter = %d, want no row (nothing ran)", got)
			}
		})
	}
}

// TestTransitionUnknownRunIsNotFound proves the read half of step 2: an id that
// does not exist is ErrNotFound, not a conflict and not a silent no-op.
func TestTransitionUnknownRunIsNotFound(t *testing.T) {
	f := newTransitionFixture(t)
	_, err := f.transition(TransitionInput{
		RunID: "run-does-not-exist", ExpectedRevision: 1, ExpectedStatus: run.RunStatusQueued,
		Trigger: run.EventTrigger(string(run.EventSchedulerClaimed)), At: transitionAt, Actor: systemActor,
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	var conflict *RevisionConflictError
	if errors.As(err, &conflict) {
		t.Errorf("error %v is a revision conflict, want a plain not-found", err)
	}
}

// TestReservedStateEventPayloadKeysMatchTheWrittenPayload keeps the exported
// key list and the payload the store actually writes in step: a key added to one
// but not the other would let a caller's Details silently overwrite a store
// field.
func TestReservedStateEventPayloadKeysMatchTheWrittenPayload(t *testing.T) {
	f := newTransitionFixture(t)
	result, err := f.transition(TransitionInput{
		RunID: f.runID, ExpectedRevision: 1, ExpectedStatus: run.RunStatusQueued,
		Trigger: run.CommandTrigger(run.CommandRunCancel), At: transitionAt, Actor: userActor,
		Details: map[string]any{"reason": "reserved-key check"},
	})
	if err != nil {
		t.Fatalf("queued cancel: %v", err)
	}
	if result.Event == nil {
		t.Fatal("no state event was written")
	}
	var payload map[string]any
	if err := json.Unmarshal(result.Event.Payload, &payload); err != nil {
		t.Fatalf("payload %s: %v", result.Event.Payload, err)
	}
	for _, key := range ReservedStateEventPayloadKeys {
		if _, ok := payload[key]; !ok {
			t.Errorf("payload %s has no reserved key %q", result.Event.Payload, key)
		}
		if !IsReservedStateEventPayloadKey(key) || !IsReservedStateEventPayloadKey(strings.ToUpper(key)) {
			t.Errorf("IsReservedStateEventPayloadKey(%q) = false", key)
		}
	}
	// A caller's own key survives alongside them.
	if payload["reason"] != "reserved-key check" {
		t.Errorf("payload reason = %v, want the caller's detail", payload["reason"])
	}
	// failure_code is only written when §21.1 gives the operation a code.
	if _, ok := payload[payloadKeyFailureCode]; !ok {
		t.Errorf("payload %s has no failure_code for run.cancel (forbidden)", result.Event.Payload)
	}
}
