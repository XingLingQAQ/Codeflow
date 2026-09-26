package runstore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/run"
)

// ---------------------------------------------------------------------------
// Fixture
// ---------------------------------------------------------------------------

// retryAt is the instant the retry fixtures report their retry at.
var retryAt = time.UnixMilli(1700000200000)

// retryFixture is a store with one project, one task and one failed Run whose
// task is failed too: the state an explicit retry starts from.
type retryFixture struct {
	t      *testing.T
	store  *Store
	path   string
	runID  string
	taskID string
	// original is the failed run as stored, and task is the failed task as
	// stored, so a test compares against what the store actually wrote.
	original run.Run
	task     run.Task
}

// newRetryFixture builds the failed-run/failed-task pair by driving a fresh
// fixture's Run to failed through the transition path, and failing its Task with
// the CAS. Driving it rather than writing the status directly is deliberate: the
// fixture then proves the two rows the retry reads are rows this store can
// actually produce.
func newRetryFixture(t *testing.T) *retryFixture {
	t.Helper()
	f := &retryFixture{t: t, runID: "run-retry", taskID: "task-retry"}
	tf := newTransitionFixture(t)
	f.store, f.path = tf.store, tf.path
	f.runID, f.taskID = tf.runID, tf.taskID

	// queued --claim--> starting --start--> running --exit(1)--> failed
	tf.walk(
		tf.transitionInput(0, "", run.EventTrigger(string(run.EventSchedulerClaimed))),
		TransitionInput{Trigger: run.EventTrigger(string(run.EventProcessStarted)), At: transitionAt, Actor: systemActor,
			AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr()},
		TransitionInput{Trigger: run.ProcessExitedTrigger(1), At: transitionAt, Actor: systemActor,
			AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr()},
	)
	f.original = tf.current()
	if f.original.Status != run.RunStatusFailed {
		t.Fatalf("fixture run = %s, want failed", f.original.Status)
	}

	// The task is still ready (nothing in the fixture moved it), so it is CASed
	// to failed: §27.2.7's retry requires the task to be failed, and the fixture
	// must reach that state through the store.
	task, err := GetTask(context.Background(), f.store.DB(), f.taskID)
	if err != nil {
		t.Fatalf("GetTask(%s): %v", f.taskID, err)
	}
	var failed run.Task
	err = f.store.WithTx(context.Background(), func(ctx context.Context, tx Tx) error {
		var err error
		failed, err = UpdateTaskStatusCAS(ctx, tx, task.ID, task.Revision, run.TaskStatusFailed, transitionAt)
		return err
	})
	if err != nil {
		t.Fatalf("fail task %s: %v", f.taskID, err)
	}
	f.task = failed
	return f
}

// retryInput builds a RetryInput for the fixture with a fresh new-run id.
func (f *retryFixture) retryInput(newRunID string) RetryInput {
	return RetryInput{
		OriginalRunID:        f.runID,
		NewRunID:             newRunID,
		ExpectedTaskRevision: f.task.Revision,
		At:                   retryAt,
	}
}

// retry runs one RetryRunTx in its own transaction and returns what it said.
func (f *retryFixture) retry(in RetryInput) (RetryResult, error) {
	f.t.Helper()
	var result RetryResult
	err := f.store.WithTx(context.Background(), func(ctx context.Context, tx Tx) error {
		var err error
		result, err = RetryRunTx(ctx, tx, in)
		return err
	})
	return result, err
}

// mustRetry runs one retry and fails the test if it is refused.
func (f *retryFixture) mustRetry(in RetryInput) RetryResult {
	f.t.Helper()
	result, err := f.retry(in)
	if err != nil {
		f.t.Fatalf("RetryRunTx(%s -> %s): %v", in.OriginalRunID, in.NewRunID, err)
	}
	return result
}

// failRunOn drives a Run of this store to failed through the §21.1 table, so a
// test can end the Run a previous retry created (which is what makes the task
// retryable again). It returns the failed row.
func failRunOn(t *testing.T, store *Store, runID string) run.Run {
	t.Helper()
	tf := &transitionFixture{t: t, store: store, runID: runID}
	tf.walk(
		tf.transitionInput(0, "", run.EventTrigger(string(run.EventSchedulerClaimed))),
		TransitionInput{Trigger: run.EventTrigger(string(run.EventProcessStarted)), At: transitionAt, Actor: systemActor,
			AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr()},
		TransitionInput{Trigger: run.ProcessExitedTrigger(1), At: transitionAt, Actor: systemActor,
			AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr()},
	)
	failed := tf.current()
	if failed.Status != run.RunStatusFailed {
		t.Fatalf("failRunOn(%s): status = %s, want failed", runID, failed.Status)
	}
	return failed
}

// failTaskAgain CASes the fixture's task from queued back to failed, the way a
// second failed execution would leave it. It returns the task as stored, so the
// caller has the revision the next retry must carry.
func (f *retryFixture) failTaskAgain() run.Task {
	f.t.Helper()
	task := f.taskNow()
	if task.Status != run.TaskStatusQueued {
		f.t.Fatalf("failTaskAgain: task is %s, want queued (a retry left it there)", task.Status)
	}
	var failed run.Task
	err := f.store.WithTx(context.Background(), func(ctx context.Context, tx Tx) error {
		var err error
		failed, err = UpdateTaskStatusCAS(ctx, tx, task.ID, task.Revision, run.TaskStatusFailed, transitionAt)
		return err
	})
	if err != nil {
		f.t.Fatalf("failTaskAgain(%s): %v", task.ID, err)
	}
	return failed
}

// runs reads every Run of the fixture's task through the pool.
func (f *retryFixture) runs() []run.Run {
	f.t.Helper()
	runs, err := ListRunsByTask(context.Background(), f.store.DB(), f.taskID)
	if err != nil {
		f.t.Fatalf("ListRunsByTask(%s): %v", f.taskID, err)
	}
	return runs
}

// taskNow reads the task row through the pool.
func (f *retryFixture) taskNow() run.Task {
	f.t.Helper()
	task, err := GetTask(context.Background(), f.store.DB(), f.taskID)
	if err != nil {
		f.t.Fatalf("GetTask(%s): %v", f.taskID, err)
	}
	return task
}

// ---------------------------------------------------------------------------
// The accepted retry
// ---------------------------------------------------------------------------

// TestRetryCreatesNewAssociatedRun is the §15 T1.02 acceptance case: a failed Run
// plus a failed Task become a queued Task plus a *new* Run that points at the old
// one and reuses its frozen input, in one transaction. Nothing about the old Run
// moves.
func TestRetryCreatesNewAssociatedRun(t *testing.T) {
	f := newRetryFixture(t)
	originalEvents, _, err := ListRunEvents(context.Background(), f.store.DB(), f.runID, 0, 0)
	if err != nil {
		t.Fatalf("ListRunEvents(%s): %v", f.runID, err)
	}
	originalRunsBefore := len(f.runs())

	result := f.mustRetry(f.retryInput("run-retry-2"))

	// The new Run is a new identity: a new id, queued, revision 1, and the old
	// Run named as what it retries.
	if result.Run.ID != "run-retry-2" {
		t.Errorf("new run id = %s, want run-retry-2", result.Run.ID)
	}
	if result.Run.ID == f.original.ID {
		t.Errorf("new run id = the original's (%s); a retry creates a new run", result.Run.ID)
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
		t.Errorf("new run finished_at = %s, want nil (it is queued)", result.Run.FinishedAt)
	}
	if !result.Run.CreatedAt.Equal(retryAt) || !result.Run.UpdatedAt.Equal(retryAt) {
		t.Errorf("new run timestamps = (%s,%s), want %s", result.Run.CreatedAt, result.Run.UpdatedAt, retryAt)
	}

	// It reuses the original's frozen input pin — the same snapshot, the same
	// binding revision, the same agent revision — because §27.2.7 retries the
	// same frozen input.
	if result.Run.InputSnapshotID != f.original.InputSnapshotID ||
		result.Run.InputSnapshotHash != f.original.InputSnapshotHash {
		t.Errorf("new run pins snapshot (%s,%s), want the original's (%s,%s)",
			result.Run.InputSnapshotID, result.Run.InputSnapshotHash,
			f.original.InputSnapshotID, f.original.InputSnapshotHash)
	}
	if result.Run.TaskID != f.original.TaskID || result.Run.ProjectID != f.original.ProjectID {
		t.Errorf("new run = (task %s, project %s), want (%s,%s)",
			result.Run.TaskID, result.Run.ProjectID, f.original.TaskID, f.original.ProjectID)
	}
	if result.Run.BindingID != f.original.BindingID ||
		result.Run.BindingRevision != f.original.BindingRevision ||
		result.Run.BaseManifestHash != f.original.BaseManifestHash ||
		!equalStringPtr(result.Run.BaseCommit, f.original.BaseCommit) ||
		result.Run.AgentRevisionID != f.original.AgentRevisionID {
		t.Errorf("new run does not copy the original's frozen pin:\n new      %+v\n original %+v",
			result.Run, f.original)
	}
	if !equalBudget(result.Run.Budget, f.original.Budget) {
		t.Errorf("new run budget = %+v, want the original's %+v", result.Run.Budget, f.original.Budget)
	}

	// The stored row agrees with the returned one, and the Task CASed to queued.
	stored, err := GetRun(context.Background(), f.store.DB(), "run-retry-2")
	if err != nil {
		t.Fatalf("GetRun(run-retry-2): %v", err)
	}
	if !equalRun(stored, result.Run) {
		t.Errorf("stored new run %+v differs from the returned %+v", stored, result.Run)
	}
	if result.Task.Status != run.TaskStatusQueued {
		t.Errorf("task status = %s, want queued", result.Task.Status)
	}
	if result.Task.Revision != f.task.Revision+1 {
		t.Errorf("task revision = %d, want %d", result.Task.Revision, f.task.Revision+1)
	}
	if now := f.taskNow(); !equalTask(now, result.Task) {
		t.Errorf("stored task %+v differs from the returned %+v", now, result.Task)
	}

	// The old Run is untouched: same row, same timeline, and the task now has two
	// runs of which exactly one is non-terminal.
	originalAfter, err := GetRun(context.Background(), f.store.DB(), f.runID)
	if err != nil {
		t.Fatalf("GetRun(%s): %v", f.runID, err)
	}
	if !equalRun(originalAfter, f.original) {
		t.Errorf("the original run changed:\n before %+v\n after  %+v", f.original, originalAfter)
	}
	originalEventsAfter, _, err := ListRunEvents(context.Background(), f.store.DB(), f.runID, 0, 0)
	if err != nil {
		t.Fatalf("ListRunEvents(%s): %v", f.runID, err)
	}
	if len(originalEventsAfter) != len(originalEvents) {
		t.Errorf("the original run's timeline grew from %d to %d events; a retry writes nothing to the old run",
			len(originalEvents), len(originalEventsAfter))
	}
	all := f.runs()
	if len(all) != originalRunsBefore+1 {
		t.Fatalf("task has %d runs, want %d", len(all), originalRunsBefore+1)
	}
	active := 0
	for _, r := range all {
		if !r.Status.IsTerminal() {
			active++
		}
	}
	if active != 1 {
		t.Errorf("task has %d non-terminal runs, want 1 (§19.1)", active)
	}
	t.Logf("EVIDENCE retry: %s(failed) -> %s(queued, revision 1, retry_of=%s), task %s->queued revision %d->%d",
		f.original.ID, result.Run.ID, *result.Run.RetryOfRunID,
		result.Task.Status, f.task.Revision, result.Task.Revision)
}

// TestRetryStartsFromTheOriginalSnapshot proves the "same frozen input" half more
// sharply: the new Run's pin must be the *original* snapshot even when the task
// could have produced another one, and a snapshot body that changed is not
// re-read. It also covers a retry of a retry: the second retry still points at
// the snapshot of the first, not at a new one.
func TestRetryStartsFromTheOriginalSnapshot(t *testing.T) {
	f := newRetryFixture(t)

	first := f.mustRetry(f.retryInput("run-retry-2"))

	// Fail the new run and the task again, the way a second failed execution
	// would, then retry that one.
	failRunOn(t, f.store, first.Run.ID)
	failed := f.failTaskAgain()

	second := f.mustRetry(RetryInput{
		OriginalRunID:        first.Run.ID,
		NewRunID:             "run-retry-3",
		ExpectedTaskRevision: failed.Revision,
		At:                   retryAt.Add(time.Second),
	})

	if second.Run.InputSnapshotID != f.original.InputSnapshotID ||
		second.Run.InputSnapshotHash != f.original.InputSnapshotHash {
		t.Errorf("second retry pins snapshot (%s,%s), want the first snapshot's (%s,%s)",
			second.Run.InputSnapshotID, second.Run.InputSnapshotHash,
			f.original.InputSnapshotID, f.original.InputSnapshotHash)
	}
	if second.Run.RetryOfRunID == nil || *second.Run.RetryOfRunID != first.Run.ID {
		t.Errorf("second retry points at %s, want the run it retried (%s)",
			stringPtrString(second.Run.RetryOfRunID), first.Run.ID)
	}
	if second.Run.ID == first.Run.ID || second.Run.ID == f.original.ID {
		t.Errorf("second retry reused an existing run id (%s)", second.Run.ID)
	}
	t.Logf("EVIDENCE retry of retry: %s -> %s -> %s, all pinning snapshot %s",
		f.original.ID, first.Run.ID, second.Run.ID, second.Run.InputSnapshotID)
}

// TestRetryCommandIDIsUnique covers the idempotency key: a retry may carry the
// command that requested it, and the database refuses a second Run claiming the
// same one, so a replayed command cannot create a second Run.
func TestRetryCommandIDIsUnique(t *testing.T) {
	f := newRetryFixture(t)
	commandID := "cmd-retry-1"
	in := f.retryInput("run-retry-2")
	in.CommandID = stringPtr(commandID)

	first := f.mustRetry(in)
	if first.Run.CommandID == nil || *first.Run.CommandID != commandID {
		t.Fatalf("new run command_id = %s, want %q", stringPtrString(first.Run.CommandID), commandID)
	}

	// The same command replayed: the run id is new (a caller that lost the
	// response generates one) but the command is not, and the unique index on
	// runs.command_id refuses it. The first retry's run has to end first, or the
	// replay would be refused by the one-active-run rule before it ever reached
	// the insert — and the command id would never be exercised.
	failRunOn(t, f.store, first.Run.ID)
	failed := f.failTaskAgain()
	replayed := f.retryInput("run-retry-3")
	replayed.CommandID = stringPtr(commandID)
	replayed.ExpectedTaskRevision = failed.Revision

	if _, err := f.retry(replayed); err == nil {
		t.Fatal("a replayed command id was accepted, want a unique-constraint refusal")
	} else if !strings.Contains(err.Error(), "command_id") && !strings.Contains(err.Error(), "UNIQUE") {
		t.Errorf("error = %v, want a refusal naming the command_id unique constraint", err)
	}
	// Nothing partial was written: the task is still failed and no third run
	// exists.
	if now := f.taskNow(); now.Status != run.TaskStatusFailed || now.Revision != failed.Revision {
		t.Errorf("task = (%s,%d) after the refused replay, want (%s,%d)",
			now.Status, now.Revision, run.TaskStatusFailed, failed.Revision)
	}
	if runs := f.runs(); len(runs) != 2 {
		t.Errorf("task has %d runs after the refused replay, want 2", len(runs))
	}
	t.Logf("EVIDENCE command id: first retry accepted with %q, replay refused and rolled back", commandID)
}

// ---------------------------------------------------------------------------
// Refusals
// ---------------------------------------------------------------------------

// TestRetryRefusesNonFailedRun walks the original-Run rule: only a failed Run can
// be retried. A live Run would fork a second execution off a running one, and a
// completed/cancelled/expired Run ended for a reason a retry would silently
// override.
func TestRetryRefusesNonFailedRun(t *testing.T) {
	ctx := context.Background()

	t.Run("running", func(t *testing.T) {
		f := newTransitionFixture(t)
		// Give the task a non-terminal run and make the task failed, so the run
		// rule is the only thing that can refuse.
		f.walk(
			f.transitionInput(0, "", run.EventTrigger(string(run.EventSchedulerClaimed))),
			TransitionInput{Trigger: run.EventTrigger(string(run.EventProcessStarted)), At: transitionAt, Actor: systemActor,
				AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr()},
		)
		running := f.current()
		if running.Status != run.RunStatusRunning {
			t.Fatalf("fixture status = %s, want running", running.Status)
		}
		_, err := retryOn(f.store, running.ID, "run-retry-2", 1)
		if !errors.Is(err, ErrRunNotRetryable) {
			t.Fatalf("retry of a running run = %v, want ErrRunNotRetryable", err)
		}
		if now := f.current(); !equalRun(now, running) {
			t.Errorf("run changed on the refusal:\n before %+v\n after  %+v", running, now)
		}
	})

	for _, terminal := range []struct {
		name string
		to   run.RunStatus
	}{
		{"completed", run.RunStatusCompleted},
	} {
		t.Run(terminal.name, func(t *testing.T) {
			f := newTransitionFixture(t)
			// Reach the terminal state through the table: claim, start, then the
			// trigger that ends in this status.
			f.walk(
				f.transitionInput(0, "", run.EventTrigger(string(run.EventSchedulerClaimed))),
				TransitionInput{Trigger: run.EventTrigger(string(run.EventProcessStarted)), At: transitionAt, Actor: systemActor,
					AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr()},
			)
			var end TransitionInput
			switch terminal.to {
			case run.RunStatusCompleted:
				end = TransitionInput{Trigger: run.ProcessExitedTrigger(0), At: transitionAt, Actor: systemActor,
					AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr()}
			case run.RunStatusCancelled:
				// running --run.cancel--> cancelling --cleaned--> cancelled
				f.walk(TransitionInput{Trigger: run.CommandTrigger(run.CommandRunCancel), At: transitionAt, Actor: userActor})
				end = TransitionInput{Trigger: run.ConclusionTrigger(run.ConclusionCleaned, run.RunStatusCancelled),
					At: transitionAt, Actor: systemActor}
			case run.RunStatusExpired:
				// running --hard_deadline--> cancelling --cleaned--> expired
				f.walk(TransitionInput{Trigger: run.CommandTrigger(run.CommandHardDeadline), At: transitionAt, Actor: systemActor})
				end = TransitionInput{Trigger: run.ConclusionTrigger(run.ConclusionCleaned, run.RunStatusExpired),
					At: transitionAt, Actor: systemActor}
			}
			f.walk(end)
			ended := f.current()
			if ended.Status != terminal.to {
				t.Fatalf("fixture status = %s, want %s", ended.Status, terminal.to)
			}
			_, err := retryOn(f.store, ended.ID, "run-retry-2", 1)
			if !errors.Is(err, ErrRunNotRetryable) {
				t.Fatalf("retry of a %s run = %v, want ErrRunNotRetryable", terminal.name, err)
			}
			if now := f.current(); !equalRun(now, ended) {
				t.Errorf("run changed on the refusal:\n before %+v\n after  %+v", ended, now)
			}
			if runs := mustRunsByTask(t, f.store, f.taskID); len(runs) != 1 {
				t.Errorf("task has %d runs after the refusal, want 1 (no partial write)", len(runs))
			}
		})
	}
	_ = ctx
}

// TestRetryAcceptsCancelledAndExpiredRuns: a run that ended without completing
// is retryable whatever the reason, and the task decides. An expired run (hard
// deadline) whose task was failed gets a new Run; so does a cancelled one; a
// cancelled run whose task was cancelled too is refused by the task rule, not by
// the run rule.
func TestRetryAcceptsCancelledAndExpiredRuns(t *testing.T) {
	for _, tc := range []struct {
		name    string
		command string
		ended   run.RunStatus
	}{
		{"expired", run.CommandHardDeadline, run.RunStatusExpired},
		{"cancelled", run.CommandRunCancel, run.RunStatusCancelled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newTransitionFixture(t)
			f.walk(
				f.transitionInput(0, "", run.EventTrigger(string(run.EventSchedulerClaimed))),
				TransitionInput{Trigger: run.EventTrigger(string(run.EventProcessStarted)), At: transitionAt, Actor: systemActor,
					AttemptID: attemptPtr(), AgentRevisionID: agentRevisionPtr()},
				TransitionInput{Trigger: run.CommandTrigger(tc.command), At: transitionAt, Actor: userActor},
				TransitionInput{Trigger: run.ConclusionTrigger(run.ConclusionCleaned, tc.ended), At: transitionAt, Actor: systemActor},
			)
			ended := f.current()
			if ended.Status != tc.ended {
				t.Fatalf("fixture status = %s, want %s", ended.Status, tc.ended)
			}
			task := setTaskStatusForTest(t, f.store, f.taskID, run.TaskStatusFailed)

			res, err := retryOn(f.store, ended.ID, "run-retry-"+tc.name, task.Revision)
			if err != nil {
				t.Fatalf("retry of a %s run whose task is failed = %v, want a new Run", tc.name, err)
			}
			if res.Run.Status != run.RunStatusQueued || res.Run.RetryOfRunID == nil || *res.Run.RetryOfRunID != ended.ID {
				t.Fatalf("retried run = %+v, want queued and linked to %s", res.Run, ended.ID)
			}
			if res.Task.Status != run.TaskStatusQueued {
				t.Fatalf("task after retry = %s, want queued", res.Task.Status)
			}
			if now := f.current(); !equalRun(now, ended) {
				t.Errorf("the %s original changed:\n before %+v\n after  %+v", tc.name, ended, now)
			}
		})
	}

	t.Run("cancelled_task_is_not_reopened", func(t *testing.T) {
		f := newTransitionFixture(t)
		f.walk(
			f.transitionInput(0, "", run.EventTrigger(string(run.EventSchedulerClaimed))),
			TransitionInput{Trigger: run.CommandTrigger(run.CommandRunCancel), At: transitionAt, Actor: userActor},
			TransitionInput{Trigger: run.ConclusionTrigger(run.ConclusionCleaned, run.RunStatusCancelled), At: transitionAt, Actor: systemActor},
		)
		ended := f.current()
		task := setTaskStatusForTest(t, f.store, f.taskID, run.TaskStatusCancelled)
		if _, err := retryOn(f.store, ended.ID, "run-retry-x", task.Revision); !errors.Is(err, ErrTaskNotRetryable) {
			t.Fatalf("retry of a cancelled run whose task is cancelled = %v, want ErrTaskNotRetryable", err)
		}
	})
}

// setTaskStatusForTest CASes a task to status from whatever it is now.
func setTaskStatusForTest(t *testing.T, store *Store, taskID string, status run.TaskStatus) run.Task {
	t.Helper()
	task, err := GetTask(context.Background(), store.DB(), taskID)
	if err != nil {
		t.Fatalf("GetTask(%s): %v", taskID, err)
	}
	var updated run.Task
	err = store.WithTx(context.Background(), func(ctx context.Context, tx Tx) error {
		var err error
		updated, err = UpdateTaskStatusCAS(ctx, tx, task.ID, task.Revision, status, transitionAt)
		return err
	})
	if err != nil {
		t.Fatalf("set task %s to %s: %v", taskID, status, err)
	}
	return updated
}

// TestRetryRefusesNonFailedTask covers the task rule: §27.2.7 turns `failed` back
// into `queued` and nothing else, so a task that is not failed has no retry.
func TestRetryRefusesNonFailedTask(t *testing.T) {
	for _, status := range []run.TaskStatus{
		run.TaskStatusReady, run.TaskStatusQueued, run.TaskStatusRunning,
		run.TaskStatusWaitingReview, run.TaskStatusCompleted, run.TaskStatusCancelled,
	} {
		t.Run(string(status), func(t *testing.T) {
			f := newRetryFixture(t)
			task := f.taskNow()
			if task.Status != run.TaskStatusFailed {
				t.Fatalf("fixture task = %s, want failed", task.Status)
			}
			// Move the task to the status under test with the CAS. completed and
			// cancelled are frozen by the schema (trg_tasks_terminal_immutable),
			// so the test writes those two directly: it is setting up a state the
			// store can legitimately hold, not testing the trigger.
			revision := task.Revision
			if status == run.TaskStatusCompleted || status == run.TaskStatusCancelled {
				if _, err := f.store.DB().ExecContext(context.Background(),
					`UPDATE tasks SET status = ? WHERE id = ?`, string(status), f.taskID); err != nil {
					t.Fatalf("set task status %s: %v", status, err)
				}
			} else {
				var moved run.Task
				err := f.store.WithTx(context.Background(), func(ctx context.Context, tx Tx) error {
					var err error
					moved, err = UpdateTaskStatusCAS(ctx, tx, f.taskID, task.Revision, status, transitionAt)
					return err
				})
				if err != nil {
					t.Fatalf("task -> %s: %v", status, err)
				}
				revision = moved.Revision
			}

			_, err := retryOn(f.store, f.runID, "run-retry-2", revision)
			if !errors.Is(err, ErrTaskNotRetryable) {
				t.Fatalf("retry with a %s task = %v, want ErrTaskNotRetryable", status, err)
			}
			if runs := mustRunsByTask(t, f.store, f.taskID); len(runs) != 1 {
				t.Errorf("task has %d runs after the refusal, want 1", len(runs))
			}
			// The task is exactly where it was: the refusal wrote nothing.
			if now := f.taskNow(); now.Status != status || now.Revision != revision {
				t.Errorf("task = (%s,%d) after the refusal, want (%s,%d)", now.Status, now.Revision, status, revision)
			}
		})
	}
}

// TestRetryRefusesWhenAnActiveRunExists covers §19.1 at the retry: a task with a
// second, non-terminal Run may not gain a third. The active run is created with
// InsertRun directly, because the store has no path that would create one for a
// failed task.
func TestRetryRefusesWhenAnActiveRunExists(t *testing.T) {
	f := newRetryFixture(t)
	active := run.Run{
		ID: "run-active", TaskID: f.taskID, ProjectID: transitionProjectID,
		BindingID: "binding-1", BindingRevision: 1, BaseManifestHash: "sha256:manifest",
		AgentRevisionID: "agent-rev-1", InputSnapshotID: f.original.InputSnapshotID,
		InputSnapshotHash: f.original.InputSnapshotHash,
		Budget:            f.original.Budget,
		Status:            run.RunStatusQueued,
		CreatedAt:         retryAt, UpdatedAt: retryAt,
	}
	err := f.store.WithTx(context.Background(), func(ctx context.Context, tx Tx) error {
		return InsertRun(ctx, tx, &active)
	})
	if err != nil {
		t.Fatalf("insert the active run: %v", err)
	}

	_, err = f.retry(f.retryInput("run-retry-2"))
	if !errors.Is(err, ErrActiveRunExists) {
		t.Fatalf("retry with an active run present = %v, want ErrActiveRunExists", err)
	}
	// Nothing partial: the task is still failed at its old revision, and the run
	// the retry would have created does not exist.
	if now := f.taskNow(); now.Status != run.TaskStatusFailed || now.Revision != f.task.Revision {
		t.Errorf("task = (%s,%d) after the refusal, want (%s,%d)",
			now.Status, now.Revision, run.TaskStatusFailed, f.task.Revision)
	}
	if _, err := GetRun(context.Background(), f.store.DB(), "run-retry-2"); !errors.Is(err, ErrNotFound) {
		t.Errorf("run-retry-2 exists after the refusal (err=%v), want ErrNotFound", err)
	}
	if runs := f.runs(); len(runs) != 2 {
		t.Errorf("task has %d runs after the refusal, want 2", len(runs))
	}
	t.Logf("EVIDENCE active run: retry refused with %v, task still failed at revision %d", err, f.task.Revision)
}

// TestRetryTaskRevisionConflict proves the retry CASes the task: a stale task
// revision is a conflict, not a lost update, and the new Run is not left behind.
func TestRetryTaskRevisionConflict(t *testing.T) {
	f := newRetryFixture(t)
	stale := f.task.Revision - 1
	if stale < 1 {
		stale = 1
	}

	in := f.retryInput("run-retry-2")
	in.ExpectedTaskRevision = stale
	_, err := f.retry(in)
	var conflict *TaskRevisionConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("retry with a stale task revision = %v, want *TaskRevisionConflictError", err)
	}
	if !errors.Is(err, ErrRevisionConflict) {
		t.Errorf("error %v does not wrap ErrRevisionConflict", err)
	}
	if conflict.Expected != stale || conflict.Current != f.task.Revision {
		t.Errorf("conflict = expected %d current %d, want expected %d current %d",
			conflict.Expected, conflict.Current, stale, f.task.Revision)
	}
	if conflict.CurrentStatus != run.TaskStatusFailed {
		t.Errorf("conflict status = %s, want failed", conflict.CurrentStatus)
	}
	if _, err := GetRun(context.Background(), f.store.DB(), "run-retry-2"); !errors.Is(err, ErrNotFound) {
		t.Errorf("the new run survived a task conflict (err=%v), want ErrNotFound", err)
	}
	if runs := f.runs(); len(runs) != 1 {
		t.Errorf("task has %d runs after the conflict, want 1", len(runs))
	}
	if now := f.taskNow(); !equalTask(now, f.task) {
		t.Errorf("task changed on a conflict:\n before %+v\n after  %+v", f.task, now)
	}
	t.Logf("EVIDENCE task conflict: expected %d current %d, no run created, task unchanged", stale, conflict.Current)
}

// TestRetryNewRunIDCollision proves a caller that reuses a run id gets a refusal
// and no partial write, rather than a run row that overwrites the existing one.
func TestRetryNewRunIDCollision(t *testing.T) {
	f := newRetryFixture(t)
	// A first retry takes run-retry-2, and then that run fails so the id
	// collision is the only thing wrong with the second call: the one-active-run
	// rule would otherwise refuse it first.
	first := f.mustRetry(f.retryInput("run-retry-2"))
	failRunOn(t, f.store, first.Run.ID)
	before := f.failTaskAgain()
	runsBefore := len(f.runs())

	_, err := f.retry(RetryInput{
		OriginalRunID: f.runID, NewRunID: "run-retry-2",
		ExpectedTaskRevision: before.Revision, At: retryAt.Add(time.Second),
	})
	if !errors.Is(err, ErrInvalidTransitionInput) {
		t.Fatalf("retry reusing an existing run id = %v, want ErrInvalidTransitionInput", err)
	}
	if !strings.Contains(err.Error(), "run-retry-2") {
		t.Errorf("error %q does not name the colliding run id", err)
	}
	if runs := f.runs(); len(runs) != runsBefore {
		t.Errorf("task has %d runs after the refusal, want %d", len(runs), runsBefore)
	}
	if now := f.taskNow(); !equalTask(now, before) {
		t.Errorf("task changed on the refusal:\n before %+v\n after  %+v", before, now)
	}
	t.Logf("EVIDENCE id collision: %v, no partial write", err)
}

// ---------------------------------------------------------------------------
// Rollback and validation
// ---------------------------------------------------------------------------

// TestRetryRollsBackWithCaller proves the retry is one unit of work: when the
// caller's later step fails, neither the new Run nor the task CAS survives.
func TestRetryRollsBackWithCaller(t *testing.T) {
	ctx := context.Background()
	f := newRetryFixture(t)
	before := f.taskNow()
	runsBefore := len(f.runs())

	sentinel := errors.New("a later step in the same unit of work refused")
	err := f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		if _, err := RetryRunTx(ctx, tx, f.retryInput("run-retry-2")); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("transaction error = %v, want the caller's sentinel", err)
	}

	if _, err := GetRun(ctx, f.store.DB(), "run-retry-2"); !errors.Is(err, ErrNotFound) {
		t.Errorf("the new run survived the rollback (err=%v), want ErrNotFound", err)
	}
	if now := f.taskNow(); !equalTask(now, before) {
		t.Errorf("task after rollback = %+v, want %+v", now, before)
	}
	if runs := f.runs(); len(runs) != runsBefore {
		t.Errorf("task has %d runs after the rollback, want %d", len(runs), runsBefore)
	}

	// The retry works after the rollback: nothing was consumed or poisoned.
	result := f.mustRetry(f.retryInput("run-retry-2"))
	if result.Run.Status != run.RunStatusQueued || result.Task.Status != run.TaskStatusQueued {
		t.Errorf("retry after the rollback = (%s,%s), want (queued,queued)", result.Run.Status, result.Task.Status)
	}
	t.Logf("EVIDENCE retry rollback: task unchanged at revision %d, no run row, retry then succeeded", before.Revision)
}

// TestRetryRejectsInvalidInput walks the input contract. Every violation is
// caught before any SQL, so the caller's transaction is untouched.
func TestRetryRejectsInvalidInput(t *testing.T) {
	f := newRetryFixture(t)
	before := f.taskNow()

	base := f.retryInput("run-retry-2")
	cases := []struct {
		name   string
		mutate func(in *RetryInput)
		want   string
	}{
		{"empty original", func(in *RetryInput) { in.OriginalRunID = "" }, "OriginalRunID"},
		{"blank original", func(in *RetryInput) { in.OriginalRunID = "  " }, "OriginalRunID"},
		{"empty new", func(in *RetryInput) { in.NewRunID = "" }, "NewRunID"},
		{"new equals original", func(in *RetryInput) { in.NewRunID = in.OriginalRunID }, "NewRunID equals OriginalRunID"},
		{"zero expected revision", func(in *RetryInput) { in.ExpectedTaskRevision = 0 }, "ExpectedTaskRevision"},
		{"negative expected revision", func(in *RetryInput) { in.ExpectedTaskRevision = -3 }, "ExpectedTaskRevision"},
		{"zero at", func(in *RetryInput) { in.At = time.Time{} }, "At"},
		{"empty command id", func(in *RetryInput) { in.CommandID = stringPtr("") }, "CommandID"},
		{"blank command id", func(in *RetryInput) { in.CommandID = stringPtr(" ") }, "CommandID"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := base
			tc.mutate(&in)
			_, err := f.retry(in)
			if !errors.Is(err, ErrInvalidTransitionInput) {
				t.Fatalf("error = %v, want ErrInvalidTransitionInput", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not name %q", err, tc.want)
			}
			if now := f.taskNow(); !equalTask(now, before) {
				t.Errorf("task changed on an invalid input:\n before %+v\n after  %+v", before, now)
			}
			if runs := f.runs(); len(runs) != 1 {
				t.Errorf("task has %d runs after the refusal, want 1", len(runs))
			}
		})
	}
}

// TestRetryUnknownOriginalIsNotFound proves a missing original Run is ErrNotFound
// and not a retryable refusal.
func TestRetryUnknownOriginalIsNotFound(t *testing.T) {
	f := newRetryFixture(t)
	_, err := f.retry(RetryInput{
		OriginalRunID: "run-does-not-exist", NewRunID: "run-retry-2",
		ExpectedTaskRevision: f.task.Revision, At: retryAt,
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	if errors.Is(err, ErrRunNotRetryable) {
		t.Errorf("error %v is ErrRunNotRetryable, want a plain not-found", err)
	}
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// retryOn runs one RetryRunTx in its own transaction against the store, for the
// tests that build their fixture by hand rather than through retryFixture.
func retryOn(store *Store, originalRunID, newRunID string, expectedTaskRevision int64) (RetryResult, error) {
	var result RetryResult
	err := store.WithTx(context.Background(), func(ctx context.Context, tx Tx) error {
		var err error
		result, err = RetryRunTx(ctx, tx, RetryInput{
			OriginalRunID:        originalRunID,
			NewRunID:             newRunID,
			ExpectedTaskRevision: expectedTaskRevision,
			At:                   retryAt,
		})
		return err
	})
	return result, err
}

// mustRunsByTask reads a task's runs and fails the test when the read fails.
func mustRunsByTask(t *testing.T, store *Store, taskID string) []run.Run {
	t.Helper()
	runs, err := ListRunsByTask(context.Background(), store.DB(), taskID)
	if err != nil {
		t.Fatalf("ListRunsByTask(%s): %v", taskID, err)
	}
	return runs
}

// TestRetryWritesNoEvent is the explicit record of the one thing a retry does not
// do: there is no event type in the closed enum (§27.4, CA-1/CA-2) for "a Run was
// created", so a retry writes no event at all. The new Run's timeline starts with
// its first transition, exactly as a first-try Run's does. This is the behaviour
// the file comment documents and T1.04 has to adjudicate (see the receipt's
// remaining items).
func TestRetryWritesNoEvent(t *testing.T) {
	f := newRetryFixture(t)
	eventsBefore := eventRowCount(t, f.store.DB(), "events")
	projectCounterBefore := counterValue(t, f.store.DB(), projectScope(transitionProjectID))
	outboxBefore := eventRowCount(t, f.store.DB(), "outbox")

	result := f.mustRetry(f.retryInput("run-retry-2"))

	if n := eventRowCount(t, f.store.DB(), "events"); n != eventsBefore {
		t.Errorf("events = %d after a retry, want %d (no event type exists for run creation)", n, eventsBefore)
	}
	if got := counterValue(t, f.store.DB(), projectScope(transitionProjectID)); got != projectCounterBefore {
		t.Errorf("project counter = %d after a retry, want %d", got, projectCounterBefore)
	}
	if n := eventRowCount(t, f.store.DB(), "outbox"); n != outboxBefore {
		t.Errorf("outbox rows = %d after a retry, want %d", n, outboxBefore)
	}
	// The new Run's own timeline is empty until its first transition.
	events, _, err := ListRunEvents(context.Background(), f.store.DB(), result.Run.ID, 0, 0)
	if err != nil {
		t.Fatalf("ListRunEvents(%s): %v", result.Run.ID, err)
	}
	if len(events) != 0 {
		t.Errorf("the new run has %d events, want 0 (a retry writes no event)", len(events))
	}
	t.Logf("EVIDENCE no event: retry created %s with an empty timeline, project counter still %d",
		result.Run.ID, projectCounterBefore)
}
