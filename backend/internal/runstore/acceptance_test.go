// Acceptance tests for the minimal runtime store (T1.01.c, plan §15/§28).
//
// One test per question the card has to answer:
//
//   - TestRunStoreReopen: does a Run survive a process restart with its frozen
//     input, its budget and its revision intact, and are the database guards
//     still in force on the reopened handle — a stale revision loses, a
//     terminal state cannot be left, a frozen column cannot be rewritten?
//   - TestRunParentProjectMismatch: is "a wrong parent id cannot be inserted"
//     true at the database level for every parent edge (project_ref -> task ->
//     run -> attempt) and for the frozen input snapshot, with the whole
//     transaction rolled back when one insert is refused?
//   - TestNonGitBaseManifest: can a workspace with no Git be baselined by the
//     real runworkspace plain manifest, pinned on a Run with base_commit NULL,
//     and does the stored hash still detect a one-byte change after a restart?
//
// Every test opens a file database in t.TempDir(): close/reopen, the on-disk
// NULL encoding and the trigger behaviour after a restart are exactly what is
// asserted, and an in-memory handle has none of them.
//
// The card's acceptance assertion also lists "Event 可单独查询". Events and the
// outbox are T1.05's tables and do not exist yet, so that clause is T1.05.a's
// test and deliberately not asserted here.
//
// The assertions are deliberately field-by-field rather than struct equality:
// a lost NULL, a re-encoded enum or a truncated instant must be named in the
// failure message, not reported as "structs differ".
package runstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/run"
	"github.com/codeflow/backend/internal/runworkspace"
)

// Fixed instants, in Unix milliseconds. Every stored instant is exactly
// representable at millisecond precision, so a dropped or rounded millisecond
// shows up as a mismatch instead of comparing equal.
var (
	accT0 = time.UnixMilli(1700000000000)
	accT1 = time.UnixMilli(1700000000001)
	accT2 = time.UnixMilli(1700000000002)
	accT3 = time.UnixMilli(1700000000003)
	accT4 = time.UnixMilli(1700000000004)
	accT5 = time.UnixMilli(1700000000005)
	// accLeaseUntil is a claim deadline far from the other instants, so a
	// swapped column cannot pass by coincidence.
	accLeaseUntil = time.UnixMilli(1700000009000)
	// The CAS instants. They are monotonic so the transition order is visible
	// in a stored updated_at.
	accT10 = time.UnixMilli(1700000001000)
	accT11 = time.UnixMilli(1700000002000)
	accT12 = time.UnixMilli(1700000003000)
	accT13 = time.UnixMilli(1700000004000)
)

// ---------------------------------------------------------------------------
// Assertion helpers
// ---------------------------------------------------------------------------

// accPtr renders a pointer without printing its address, so a failure message
// shows the value or "<nil>" rather than a heap offset.
func accPtr[T any](p *T) string {
	if p == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%v", *p)
}

func accCheckStr(t *testing.T, field, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %q after reopen, want %q", field, got, want)
	}
}

func accCheckInt(t *testing.T, field string, got, want int64) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %d after reopen, want %d", field, got, want)
	}
}

// accCheckStrPtr pins both directions of a nullable column: a value set before
// the close must come back byte-identical, and a NULL must come back as nil —
// not as an empty string, which is a different fact.
func accCheckStrPtr(t *testing.T, field string, got, want *string) {
	t.Helper()
	if want == nil {
		if got != nil {
			t.Errorf("%s = %s after reopen, want nil (a NULL column must not come back as an empty string)",
				field, accPtr(got))
		}
		return
	}
	if got == nil {
		t.Fatalf("%s = nil after reopen, want %s", field, accPtr(want))
	}
	if *got != *want {
		t.Errorf("%s = %q after reopen, want %q", field, *got, *want)
	}
}

func accCheckInt64Ptr(t *testing.T, field string, got, want *int64) {
	t.Helper()
	if want == nil {
		if got != nil {
			t.Errorf("%s = %s after reopen, want nil (a NULL column must not come back as zero)", field, accPtr(got))
		}
		return
	}
	if got == nil {
		t.Fatalf("%s = nil after reopen, want %s", field, accPtr(want))
	}
	if *got != *want {
		t.Errorf("%s = %d after reopen, want %d", field, *got, *want)
	}
}

// accCheckInstant asserts an instant round-tripped exactly, at millisecond
// precision and in UTC: the store persists Unix milliseconds, so anything else
// means the conversion at the boundary changed the value.
func accCheckInstant(t *testing.T, field string, got, want time.Time) {
	t.Helper()
	if got.Location() != time.UTC {
		t.Errorf("%s location = %v, want UTC", field, got.Location())
	}
	if !got.Equal(want) {
		t.Errorf("%s = %v after reopen, want %v", field, got, want)
	}
	if got.UnixMilli() != want.UnixMilli() {
		t.Errorf("%s = %d ms after reopen, want %d ms", field, got.UnixMilli(), want.UnixMilli())
	}
	if got.Nanosecond()%int(time.Millisecond) != 0 {
		t.Errorf("%s = %v is not millisecond precision", field, got)
	}
}

func accCheckInstantPtr(t *testing.T, field string, got, want *time.Time) {
	t.Helper()
	if want == nil {
		if got != nil {
			t.Errorf("%s = %s after reopen, want nil (a NULL column must not come back as a zero time)",
				field, accPtr(got))
		}
		return
	}
	if got == nil {
		t.Fatalf("%s = nil after reopen, want %s", field, accPtr(want))
	}
	accCheckInstant(t, field, *got, *want)
}

// accCountWhere runs a COUNT(*) query. It is used to prove a rolled-back
// transaction left nothing behind.
func accCountWhere(t *testing.T, q Querier, query string, args ...any) int {
	t.Helper()
	var n int
	if err := q.QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return n
}

// ---------------------------------------------------------------------------
// Field-by-field comparators
// ---------------------------------------------------------------------------

func accCompareTask(t *testing.T, before, after run.Task) {
	t.Helper()
	accCheckStr(t, "task.ID", after.ID, before.ID)
	accCheckStr(t, "task.ProjectID", after.ProjectID, before.ProjectID)
	accCheckStrPtr(t, "task.FlowID", after.FlowID, before.FlowID)
	accCheckStrPtr(t, "task.StageID", after.StageID, before.StageID)
	accCheckStr(t, "task.Title", after.Title, before.Title)
	accCheckStr(t, "task.Kind", string(after.Kind), string(before.Kind))
	accCheckStr(t, "task.Status", string(after.Status), string(before.Status))
	accCheckInt(t, "task.Priority", after.Priority, before.Priority)
	accCheckStr(t, "task.InputJSON", after.InputJSON, before.InputJSON)
	accCheckStr(t, "task.InputHash", after.InputHash, before.InputHash)
	accCheckStrPtr(t, "task.LeaseOwner", after.LeaseOwner, before.LeaseOwner)
	accCheckInstantPtr(t, "task.LeaseUntil", after.LeaseUntil, before.LeaseUntil)
	accCheckInt(t, "task.LeaseEpoch", after.LeaseEpoch, before.LeaseEpoch)
	accCheckInt(t, "task.Revision", after.Revision, before.Revision)
	accCheckInstant(t, "task.CreatedAt", after.CreatedAt, before.CreatedAt)
	accCheckInstant(t, "task.UpdatedAt", after.UpdatedAt, before.UpdatedAt)
}

func accCompareRun(t *testing.T, before, after run.Run) {
	t.Helper()
	accCheckStr(t, "run.ID", after.ID, before.ID)
	accCheckStr(t, "run.TaskID", after.TaskID, before.TaskID)
	accCheckStr(t, "run.ProjectID", after.ProjectID, before.ProjectID)
	accCheckStrPtr(t, "run.CommandID", after.CommandID, before.CommandID)
	accCheckStr(t, "run.BindingID", after.BindingID, before.BindingID)
	accCheckInt(t, "run.BindingRevision", after.BindingRevision, before.BindingRevision)
	accCheckStr(t, "run.BaseManifestHash", after.BaseManifestHash, before.BaseManifestHash)
	accCheckStrPtr(t, "run.BaseCommit", after.BaseCommit, before.BaseCommit)
	accCheckStr(t, "run.AgentRevisionID", after.AgentRevisionID, before.AgentRevisionID)
	accCheckStr(t, "run.InputSnapshotID", after.InputSnapshotID, before.InputSnapshotID)
	accCheckStr(t, "run.InputSnapshotHash", after.InputSnapshotHash, before.InputSnapshotHash)
	// Budget field by field: nil means "not budgeted", which must not decode as
	// zero, and a struct comparison would compare pointer identity.
	accCheckInt64Ptr(t, "run.Budget.WallTimeSeconds", after.Budget.WallTimeSeconds, before.Budget.WallTimeSeconds)
	accCheckInt64Ptr(t, "run.Budget.Tokens", after.Budget.Tokens, before.Budget.Tokens)
	accCheckInt64Ptr(t, "run.Budget.CostLimitMinor", after.Budget.CostLimitMinor, before.Budget.CostLimitMinor)
	accCheckStr(t, "run.Budget.Currency", after.Budget.Currency, before.Budget.Currency)
	accCheckStr(t, "run.Status", string(after.Status), string(before.Status))
	accCheckInt(t, "run.Revision", after.Revision, before.Revision)
	accCheckStrPtr(t, "run.RetryOfRunID", after.RetryOfRunID, before.RetryOfRunID)
	accCheckInstant(t, "run.CreatedAt", after.CreatedAt, before.CreatedAt)
	accCheckInstant(t, "run.UpdatedAt", after.UpdatedAt, before.UpdatedAt)
	accCheckInstantPtr(t, "run.FinishedAt", after.FinishedAt, before.FinishedAt)
}

func accCompareAttempt(t *testing.T, before, after run.Attempt) {
	t.Helper()
	accCheckStr(t, "attempt.ID", after.ID, before.ID)
	accCheckStr(t, "attempt.RunID", after.RunID, before.RunID)
	accCheckInt(t, "attempt.AttemptNo", after.AttemptNo, before.AttemptNo)
	accCheckStr(t, "attempt.Backend", after.Backend, before.Backend)
	accCheckStrPtr(t, "attempt.BackendVersion", after.BackendVersion, before.BackendVersion)
	accCheckStr(t, "attempt.Status", string(after.Status), string(before.Status))
	accCheckStrPtr(t, "attempt.OwnerInstance", after.OwnerInstance, before.OwnerInstance)
	accCheckInt64Ptr(t, "attempt.PID", after.PID, before.PID)
	accCheckStrPtr(t, "attempt.ProcessStartID", after.ProcessStartID, before.ProcessStartID)
	accCheckInstantPtr(t, "attempt.StartedAt", after.StartedAt, before.StartedAt)
	accCheckInstantPtr(t, "attempt.FinishedAt", after.FinishedAt, before.FinishedAt)
	accCheckInt64Ptr(t, "attempt.ExitCode", after.ExitCode, before.ExitCode)
	accCheckStrPtr(t, "attempt.ExitReason", after.ExitReason, before.ExitReason)
	accCheckInstant(t, "attempt.CreatedAt", after.CreatedAt, before.CreatedAt)
}

func accCompareSnapshot(t *testing.T, before, after InputSnapshot) {
	t.Helper()
	accCheckStr(t, "snapshot.ID", after.ID, before.ID)
	accCheckStr(t, "snapshot.ProjectID", after.ProjectID, before.ProjectID)
	accCheckStr(t, "snapshot.ContentJSON", after.ContentJSON, before.ContentJSON)
	accCheckStr(t, "snapshot.ContentHash", after.ContentHash, before.ContentHash)
	accCheckInstant(t, "snapshot.CreatedAt", after.CreatedAt, before.CreatedAt)
}

// accState is the whole fixture read back through the typed reads, so the
// before/after comparison in TestRunStoreReopen works on stored values rather
// than on the structs the test happened to pass in.
type accState struct {
	task     run.Task
	r        run.Run
	attempt  run.Attempt
	snapshot InputSnapshot
}

func accReadAll(t *testing.T, q Querier) accState {
	t.Helper()
	ctx := context.Background()

	task, err := GetTask(ctx, q, accReopenTaskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	r, err := GetRun(ctx, q, accReopenRunID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	a, err := GetAttempt(ctx, q, accReopenAttemptID)
	if err != nil {
		t.Fatalf("GetAttempt: %v", err)
	}
	snap, err := GetInputSnapshot(ctx, q, accReopenSnapshotID)
	if err != nil {
		t.Fatalf("GetInputSnapshot: %v", err)
	}
	return accState{task: task, r: r, attempt: a, snapshot: snap}
}

// Identifiers of the reopen fixture. They are distinct from the ids used by the
// other tests in this package so a shared SQL literal can never address two
// different rows.
const (
	accReopenProjectID  = "p-reopen"
	accReopenTaskID     = "t-reopen"
	accReopenRunID      = "r-reopen"
	accReopenAttemptID  = "a-reopen"
	accReopenSnapshotID = "snap-reopen"
)

// TestRunStoreReopen is the restart assertion: write a full run graph to a file
// database, move the run one CAS step, close the handle, reopen the same file
// and prove that
//
//   - nothing was migrated twice (a restart is not a schema change),
//   - every column of Run/Task/Attempt/InputSnapshot reads back identical,
//     nullable columns included, at millisecond precision and in UTC,
//   - the stored snapshot hash is still the hash of the stored body,
//   - the revision moved by the CAS is the revision on disk (2), so a CAS
//     still holding revision 1 loses with the real current revision,
//   - the terminal and frozen-input guards survived the restart.
func TestRunStoreReopen(t *testing.T) {
	ctx := context.Background()
	store, path := newTestStore(t)

	// Optional columns are populated in both directions on purpose: the ones
	// left nil are as much a part of the assertion as the ones set.
	flowID := "flow-reopen"
	leaseOwner := "worker-1"
	leaseUntil := accLeaseUntil
	commandID := "cmd-reopen"
	baseCommit := "9f1c0a1b2c3d4e5f60718293a4b5c6d7e8f90123"
	wallSeconds := int64(1800)
	tokens := int64(50000)
	costMinor := int64(250)
	backendVersion := "fake-1.2.3"
	ownerInstance := "instance-1"
	pid := int64(4242)
	processStartID := "boot-1"
	startedAt := accT5

	task := run.Task{
		ID: accReopenTaskID, ProjectID: accReopenProjectID,
		FlowID: &flowID, // StageID stays nil.
		Title:  "add tests", Kind: run.TaskKindCode, Status: run.TaskStatusReady, Priority: 7,
		InputJSON:  `{"prompt":"add tests","n":1}`,
		LeaseOwner: &leaseOwner, LeaseUntil: &leaseUntil, LeaseEpoch: 2,
		CreatedAt: accT0, UpdatedAt: accT1,
	}
	r := run.Run{
		ID: accReopenRunID, TaskID: accReopenTaskID, ProjectID: accReopenProjectID,
		CommandID: &commandID,
		BindingID: "binding-reopen", BindingRevision: 3,
		BaseManifestHash: "sha256:manifest-reopen", BaseCommit: &baseCommit,
		AgentRevisionID: "agent-rev-1", InputSnapshotID: accReopenSnapshotID,
		Budget: run.Budget{
			WallTimeSeconds: &wallSeconds, Tokens: &tokens,
			CostLimitMinor: &costMinor, Currency: "USD",
		},
		Status: run.RunStatusQueued,
		// RetryOfRunID and FinishedAt stay nil.
		CreatedAt: accT2, UpdatedAt: accT3,
	}
	attempt := run.Attempt{
		ID: accReopenAttemptID, RunID: accReopenRunID, AttemptNo: 1, Backend: "fake",
		BackendVersion: &backendVersion, Status: run.AttemptStatusRunning,
		OwnerInstance: &ownerInstance, PID: &pid, ProcessStartID: &processStartID,
		StartedAt: &startedAt,
		// FinishedAt, ExitCode and ExitReason stay nil.
		CreatedAt: accT4,
	}
	// The body is written with unsorted keys and whitespace so the store's
	// canonicalisation is exercised as well as the round trip.
	content := json.RawMessage("{\n  \"prompt\": \"add tests\",\n  \"nested\": {\"b\": 2, \"a\": 1}\n}")

	var snapshotHash string
	err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		if err := UpsertProjectRef(ctx, tx, run.ProjectRef{
			ProjectID: accReopenProjectID, SnapshotHash: "sha256:project-reopen",
			State: run.ProjectRefStateActive, CapturedAt: accT0, VerifiedAt: accT1,
		}); err != nil {
			return err
		}
		if err := InsertTask(ctx, tx, &task); err != nil {
			return err
		}
		h, err := InsertInputSnapshot(ctx, tx, accReopenProjectID, accReopenSnapshotID, content, accT2)
		if err != nil {
			return err
		}
		snapshotHash = h
		r.InputSnapshotHash = h
		if err := InsertRun(ctx, tx, &r); err != nil {
			return err
		}
		return InsertAttempt(ctx, tx, &attempt)
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if snapshotHash == "" {
		t.Fatal("InsertInputSnapshot returned an empty hash")
	}

	// One CAS before the restart: queued -> starting, revision 1 -> 2. The
	// revision on disk must be the one the reopened handle reports.
	var started run.Run
	err = store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		var casErr error
		started, casErr = UpdateRunStatusCAS(ctx, tx, accReopenRunID, 1, run.RunStatusStarting, accT10)
		return casErr
	})
	if err != nil {
		t.Fatalf("CAS queued -> starting: %v", err)
	}
	if started.Status != run.RunStatusStarting || started.Revision != 2 {
		t.Fatalf("after CAS: status=%s revision=%d, want starting/2", started.Status, started.Revision)
	}

	before := accReadAll(t, store.DB())
	if before.r.Revision != 2 || before.r.Status != run.RunStatusStarting {
		t.Fatalf("stored run before close: status=%s revision=%d, want starting/2", before.r.Status, before.r.Revision)
	}

	// --- restart ---------------------------------------------------------
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	reopened, res, err := OpenStore(ctx, path)
	if err != nil {
		t.Fatalf("reopen %s: %v", path, err)
	}
	t.Cleanup(func() { _ = reopened.Close() })

	// A restart is not a schema change: the recorded versions still match the
	// embedded files, so nothing is applied.
	if len(res.Applied) != 0 {
		t.Errorf("reopen applied %d migrations, want 0 (the schema is already current): %+v", len(res.Applied), res.Applied)
	}
	if res.FromVersion != 6 || res.ToVersion != 6 {
		t.Errorf("reopen migration result = %d -> %d, want 6 -> 6", res.FromVersion, res.ToVersion)
	}
	t.Logf("EVIDENCE reopen MigrationResult: applied=%d from=%d to=%d", len(res.Applied), res.FromVersion, res.ToVersion)

	// --- field-by-field comparison ---------------------------------------
	after := accReadAll(t, reopened.DB())
	accCompareTask(t, before.task, after.task)
	accCompareRun(t, before.r, after.r)
	accCompareAttempt(t, before.attempt, after.attempt)
	accCompareSnapshot(t, before.snapshot, after.snapshot)

	// The nullable columns that were left nil must still be nil, and the ones
	// that were set must still carry the value.
	if after.task.StageID != nil {
		t.Errorf("task.StageID = %q after reopen, want nil", *after.task.StageID)
	}
	if after.r.RetryOfRunID != nil {
		t.Errorf("run.RetryOfRunID = %q after reopen, want nil", *after.r.RetryOfRunID)
	}
	if after.r.FinishedAt != nil {
		t.Errorf("run.FinishedAt = %v after reopen, want nil while the run is not terminal", *after.r.FinishedAt)
	}
	if after.attempt.FinishedAt != nil || after.attempt.ExitCode != nil || after.attempt.ExitReason != nil {
		t.Errorf("attempt finished fields = (%v, %s, %s), want nil while the attempt is running",
			accPtr(after.attempt.FinishedAt), accPtr(after.attempt.ExitCode), accPtr(after.attempt.ExitReason))
	}
	if after.r.BaseCommit == nil {
		t.Fatal("run.BaseCommit = nil after reopen, want the commit captured at insert")
	}

	// The hash a run pins must still be the hash of the stored body: that is
	// the whole point of freezing the input, and it must hold across a restart.
	sum := sha256.Sum256([]byte(after.snapshot.ContentJSON))
	recomputed := "sha256:" + hex.EncodeToString(sum[:])
	if recomputed != after.snapshot.ContentHash {
		t.Errorf("sha256(stored content) = %s, stored hash = %s", recomputed, after.snapshot.ContentHash)
	}
	if after.r.InputSnapshotHash != recomputed {
		t.Errorf("run.InputSnapshotHash = %s, sha256(stored content) = %s", after.r.InputSnapshotHash, recomputed)
	}
	if after.r.InputSnapshotID != after.snapshot.ID || after.r.ProjectID != after.snapshot.ProjectID {
		t.Errorf("run pins snapshot (%s,%s) but the stored snapshot is (%s,%s)",
			after.r.InputSnapshotID, after.r.ProjectID, after.snapshot.ID, after.snapshot.ProjectID)
	}

	// The run is individually queryable, and so is the attempt.
	runs, err := ListRunsByTask(ctx, reopened.DB(), accReopenTaskID)
	if err != nil {
		t.Fatalf("ListRunsByTask: %v", err)
	}
	if len(runs) != 1 || runs[0].ID != accReopenRunID {
		t.Errorf("ListRunsByTask = %+v, want exactly %s", runs, accReopenRunID)
	}
	attempts, err := ListAttemptsByRun(ctx, reopened.DB(), accReopenRunID)
	if err != nil {
		t.Fatalf("ListAttemptsByRun: %v", err)
	}
	if len(attempts) != 1 || attempts[0].ID != accReopenAttemptID {
		t.Errorf("ListAttemptsByRun = %+v, want exactly %s", attempts, accReopenAttemptID)
	}

	t.Logf("EVIDENCE reopen run: id=%s status=%s revision=%d command_id=%s base_commit=%s budget={wall:%s tokens:%s cost:%s %s} input=%s@%s",
		after.r.ID, after.r.Status, after.r.Revision, accPtr(after.r.CommandID), accPtr(after.r.BaseCommit),
		accPtr(after.r.Budget.WallTimeSeconds), accPtr(after.r.Budget.Tokens),
		accPtr(after.r.Budget.CostLimitMinor), after.r.Budget.Currency,
		after.r.InputSnapshotID, after.r.InputSnapshotHash)
	t.Logf("EVIDENCE reopen task: revision=%d status=%s flow=%s stage=%s lease=%s until=%s epoch=%d",
		after.task.Revision, after.task.Status, accPtr(after.task.FlowID), accPtr(after.task.StageID),
		accPtr(after.task.LeaseOwner), accPtr(after.task.LeaseUntil), after.task.LeaseEpoch)
	t.Logf("EVIDENCE reopen attempt: status=%s backend=%s pid=%s start_id=%s",
		after.attempt.Status, after.attempt.Backend, accPtr(after.attempt.PID), accPtr(after.attempt.ProcessStartID))
	t.Logf("EVIDENCE reopen snapshot: id=%s project=%s content=%s hash=%s recomputed=%s",
		after.snapshot.ID, after.snapshot.ProjectID, after.snapshot.ContentJSON, after.snapshot.ContentHash, recomputed)

	// --- the guards are still in force on the reopened handle -------------
	// A CAS that still believes revision 1 must lose, and must report the
	// revision actually on disk.
	err = reopened.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		_, casErr := UpdateRunStatusCAS(ctx, tx, accReopenRunID, 1, run.RunStatusRunning, accT11)
		return casErr
	})
	var conflict *RevisionConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("stale CAS after reopen = %v, want *RevisionConflictError", err)
	}
	if !errors.Is(err, ErrRevisionConflict) {
		t.Errorf("stale CAS after reopen = %v, want it to wrap ErrRevisionConflict", err)
	}
	if conflict.RunID != accReopenRunID || conflict.Expected != 1 || conflict.Current != 2 {
		t.Errorf("conflict = %+v, want run %s expected 1 current 2", conflict, accReopenRunID)
	}
	if conflict.CurrentStatus != run.RunStatusStarting {
		t.Errorf("conflict.CurrentStatus = %s, want starting (the status the revision belongs to)", conflict.CurrentStatus)
	}
	t.Logf("EVIDENCE stale CAS after reopen: %v", err)

	// Advance to a terminal state, then try to leave it.
	var completed run.Run
	err = reopened.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		var casErr error
		completed, casErr = UpdateRunStatusCAS(ctx, tx, accReopenRunID, 2, run.RunStatusCompleted, accT12)
		return casErr
	})
	if err != nil {
		t.Fatalf("CAS starting -> completed: %v", err)
	}
	if completed.Status != run.RunStatusCompleted || completed.Revision != 3 {
		t.Fatalf("after terminal CAS: status=%s revision=%d, want completed/3", completed.Status, completed.Revision)
	}
	if completed.FinishedAt == nil {
		t.Error("finished_at is nil after a terminal transition")
	}

	err = reopened.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		_, casErr := UpdateRunStatusCAS(ctx, tx, accReopenRunID, 3, run.RunStatusRunning, accT13)
		return casErr
	})
	if !errors.Is(err, ErrTerminalImmutable) {
		t.Fatalf("CAS out of completed = %v, want ErrTerminalImmutable", err)
	}
	t.Logf("EVIDENCE terminal reversal refused after reopen: %v", err)

	// The refused CAS must not have half-applied.
	afterTerminal, err := GetRun(ctx, reopened.DB(), accReopenRunID)
	if err != nil {
		t.Fatalf("GetRun after the refused CAS: %v", err)
	}
	if afterTerminal.Status != run.RunStatusCompleted || afterTerminal.Revision != 3 {
		t.Errorf("after a refused terminal CAS: status=%s revision=%d, want completed/3",
			afterTerminal.Status, afterTerminal.Revision)
	}

	// Rewriting a frozen input column directly is refused by the schema even
	// when the statement bumps the revision, and the store maps that refusal to
	// ErrFrozenInputImmutable.
	_, err = reopened.DB().ExecContext(ctx,
		`UPDATE runs SET input_snapshot_hash = 'sha256:tampered', revision = revision + 1 WHERE id = ?`,
		accReopenRunID)
	if err == nil {
		t.Fatal("rewriting runs.input_snapshot_hash succeeded, want the frozen-input trigger to refuse it")
	}
	if got := sqliteCode(err); got != sqliteConstraintTrigger {
		t.Errorf("frozen-input rewrite error code = %d, want %d (SQLITE_CONSTRAINT_TRIGGER): %v",
			got, sqliteConstraintTrigger, err)
	}
	if !strings.Contains(err.Error(), errNameRunFrozenInputImmutable) {
		t.Errorf("frozen-input rewrite error %q does not name %s", err, errNameRunFrozenInputImmutable)
	}
	if mapped := mapConstraintError("tamper run input", err); !errors.Is(mapped, ErrFrozenInputImmutable) {
		t.Errorf("mapped frozen-input rewrite = %v, want ErrFrozenInputImmutable", mapped)
	}
	t.Logf("EVIDENCE frozen run input refused after reopen: %v", err)

	// The stored snapshot body is frozen by the same rule.
	_, err = reopened.DB().ExecContext(ctx,
		`UPDATE input_snapshots SET content_hash = 'sha256:tampered' WHERE id = ?`, accReopenSnapshotID)
	if err == nil {
		t.Fatal("rewriting input_snapshots.content_hash succeeded, want the immutable trigger to refuse it")
	}
	if got := sqliteCode(err); got != sqliteConstraintTrigger {
		t.Errorf("snapshot rewrite error code = %d, want %d (SQLITE_CONSTRAINT_TRIGGER): %v",
			got, sqliteConstraintTrigger, err)
	}
	if !strings.Contains(err.Error(), errNameInputSnapshotImmutable) {
		t.Errorf("snapshot rewrite error %q does not name %s", err, errNameInputSnapshotImmutable)
	}
	if mapped := mapConstraintError("tamper snapshot", err); !errors.Is(mapped, ErrFrozenInputImmutable) {
		t.Errorf("mapped snapshot rewrite = %v, want ErrFrozenInputImmutable", mapped)
	}
	t.Logf("EVIDENCE frozen snapshot body refused after reopen: %v", err)

	// Neither refused statement touched a row.
	final, err := GetRun(ctx, reopened.DB(), accReopenRunID)
	if err != nil {
		t.Fatalf("GetRun after the refused rewrites: %v", err)
	}
	if final.InputSnapshotHash != recomputed || final.Revision != 3 {
		t.Errorf("after the refused rewrites: input_snapshot_hash=%s revision=%d, want %s and 3",
			final.InputSnapshotHash, final.Revision, recomputed)
	}
}

// TestRunParentProjectMismatch is the negative-parent assertion: a runtime fact
// may only name a parent that exists and belongs to the same project, and a
// refused insert must take its whole transaction with it.
//
// Every case seeds a legal row inside the same transaction before the illegal
// one, so "the transaction rolled back" is observable rather than assumed.
func TestRunParentProjectMismatch(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)

	// Two independent projects, each with the parent chain a run needs.
	seedProject := func(projectID, taskID, snapshotID string) string {
		t.Helper()
		task := run.Task{
			ID: taskID, ProjectID: projectID, Title: "task " + taskID,
			Kind: run.TaskKindCode, Status: run.TaskStatusReady,
			InputJSON: `{"task":"` + taskID + `"}`,
			CreatedAt: accT0, UpdatedAt: accT1,
		}
		var hash string
		err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
			if err := UpsertProjectRef(ctx, tx, run.ProjectRef{
				ProjectID: projectID, SnapshotHash: "sha256:project-" + projectID,
				State: run.ProjectRefStateActive, CapturedAt: accT0, VerifiedAt: accT1,
			}); err != nil {
				return err
			}
			if err := InsertTask(ctx, tx, &task); err != nil {
				return err
			}
			h, err := InsertInputSnapshot(ctx, tx, projectID, snapshotID,
				json.RawMessage(`{"input":"`+projectID+`"}`), accT2)
			if err != nil {
				return err
			}
			hash = h
			return nil
		})
		if err != nil {
			t.Fatalf("seed %s: %v", projectID, err)
		}
		return hash
	}
	hashP1 := seedProject("p-1", "t-1", "snap-1")
	hashP2 := seedProject("p-2", "t-2", "snap-2")
	if hashP1 == hashP2 {
		t.Fatalf("the two projects' snapshots hash identically (%s); the mismatch cases would not be distinguishable", hashP1)
	}

	newRun := func(id, taskID, projectID, snapshotID, snapshotHash string) run.Run {
		return run.Run{
			ID: id, TaskID: taskID, ProjectID: projectID,
			BindingID: "binding-1", BindingRevision: 1,
			BaseManifestHash: "sha256:manifest", AgentRevisionID: "agent-rev-1",
			InputSnapshotID: snapshotID, InputSnapshotHash: snapshotHash,
			Status: run.RunStatusQueued, CreatedAt: accT2, UpdatedAt: accT3,
		}
	}
	// legalRef is the row inserted before the refused one in each transaction.
	legalRef := func(projectID string) run.ProjectRef {
		return run.ProjectRef{
			ProjectID: projectID, SnapshotHash: "sha256:rollback",
			State: run.ProjectRefStateActive, CapturedAt: accT0, VerifiedAt: accT1,
		}
	}
	// assertRolledBack proves the legal row of a refused transaction is gone.
	assertRolledBack := func(projectID string) {
		t.Helper()
		if n := accCountWhere(t, store.DB(), `SELECT COUNT(*) FROM project_refs WHERE project_id = ?`, projectID); n != 0 {
			t.Errorf("project_refs rows for %s = %d after the refused transaction, want 0 (the whole transaction must roll back)",
				projectID, n)
		}
	}
	assertNoRow := func(table, id string) {
		t.Helper()
		if n := accCountWhere(t, store.DB(), `SELECT COUNT(*) FROM `+table+` WHERE id = ?`, id); n != 0 {
			t.Errorf("%s has %d row(s) for %s after a refused insert, want 0", table, n, id)
		}
	}
	assertForeignKeyRefusal := func(t *testing.T, err error) {
		t.Helper()
		if err == nil {
			t.Fatal("statement succeeded, want a foreign-key refusal")
		}
		if got := sqliteCode(err); got != sqliteConstraintForeignKey {
			t.Errorf("error code = %d, want %d (SQLITE_CONSTRAINT_FOREIGNKEY): %v", got, sqliteConstraintForeignKey, err)
		}
		if !strings.Contains(err.Error(), "FOREIGN KEY") {
			t.Errorf("error %q does not name the foreign key", err)
		}
	}

	t.Run("run under another project's task", func(t *testing.T) {
		const legal = "p-rb-run"
		// The task belongs to p-2, the run claims p-1: the composite foreign key
		// (task_id, project_id) must refuse it. The pinned snapshot is p-1's, so
		// the snapshot trigger passes and the foreign key is the guard under
		// test.
		r := newRun("r-cross-project", "t-2", "p-1", "snap-1", hashP1)
		err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
			if err := UpsertProjectRef(ctx, tx, legalRef(legal)); err != nil {
				return err
			}
			return InsertRun(ctx, tx, &r)
		})
		assertForeignKeyRefusal(t, err)
		t.Logf("EVIDENCE run/task project mismatch refused: %v", err)
		assertNoRow("runs", "r-cross-project")
		assertRolledBack(legal)
	})

	// Every way a run can misname its frozen input. Each is refused before the
	// row exists, and the refusal is the typed error a caller branches on.
	for _, tc := range []struct{ name, runID, projectID, snapshotID, snapshotHash string }{
		{"another project's snapshot", "r-snap-cross", "p-2", "snap-1", hashP1},
		{"a snapshot that does not exist", "r-snap-missing", "p-2", "snap-ghost", hashP2},
		{"a hash that is not the body's hash", "r-snap-hash", "p-2", "snap-2", hashP1},
	} {
		t.Run("run pinning "+tc.name, func(t *testing.T) {
			legal := "p-rb-" + tc.runID
			r := newRun(tc.runID, "t-2", tc.projectID, tc.snapshotID, tc.snapshotHash)
			err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
				if err := UpsertProjectRef(ctx, tx, legalRef(legal)); err != nil {
					return err
				}
				return InsertRun(ctx, tx, &r)
			})
			if !errors.Is(err, ErrInputSnapshotMismatch) {
				t.Fatalf("insert = %v, want ErrInputSnapshotMismatch", err)
			}
			t.Logf("EVIDENCE refused (%s): %v", tc.name, err)
			assertNoRow("runs", tc.runID)
			assertRolledBack(legal)
		})
	}

	t.Run("task under an unknown project ref", func(t *testing.T) {
		const legal = "p-rb-task"
		task := run.Task{
			ID: "t-ghost", ProjectID: "p-ghost", Title: "ghost",
			Kind: run.TaskKindCode, Status: run.TaskStatusReady,
			InputJSON: `{"ghost":true}`, CreatedAt: accT0, UpdatedAt: accT1,
		}
		err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
			if err := UpsertProjectRef(ctx, tx, legalRef(legal)); err != nil {
				return err
			}
			return InsertTask(ctx, tx, &task)
		})
		assertForeignKeyRefusal(t, err)
		t.Logf("EVIDENCE task under an unknown project_ref refused: %v", err)
		assertNoRow("tasks", "t-ghost")
		assertRolledBack(legal)
	})

	t.Run("attempt under an unknown run", func(t *testing.T) {
		const legal = "p-rb-attempt"
		a := run.Attempt{
			ID: "a-ghost", RunID: "r-ghost", AttemptNo: 1, Backend: "fake",
			Status: run.AttemptStatusStarting, CreatedAt: accT4,
		}
		err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
			if err := UpsertProjectRef(ctx, tx, legalRef(legal)); err != nil {
				return err
			}
			return InsertAttempt(ctx, tx, &a)
		})
		assertForeignKeyRefusal(t, err)
		t.Logf("EVIDENCE attempt under an unknown run refused: %v", err)
		assertNoRow("attempts", "a-ghost")
		assertRolledBack(legal)
	})

	// The seed is untouched: the refused transactions left no run, no attempt
	// and no extra task or project behind.
	if n := accCountWhere(t, store.DB(), `SELECT COUNT(*) FROM runs`); n != 0 {
		t.Errorf("runs rows = %d, want 0 (every insert in this test was refused)", n)
	}
	if n := accCountWhere(t, store.DB(), `SELECT COUNT(*) FROM attempts`); n != 0 {
		t.Errorf("attempts rows = %d, want 0 (every insert in this test was refused)", n)
	}
	if n := accCountWhere(t, store.DB(), `SELECT COUNT(*) FROM tasks`); n != 2 {
		t.Errorf("tasks rows = %d, want the 2 seeded tasks", n)
	}
	if n := accCountWhere(t, store.DB(), `SELECT COUNT(*) FROM project_refs`); n != 2 {
		t.Errorf("project_refs rows = %d, want the 2 seeded projects", n)
	}
}

// TestNonGitBaseManifest is the non-Git baseline assertion (§27.5.1): a
// workspace without Git is baselined by runworkspace's plain manifest, the Run
// pins that hash with base_commit NULL, and the baseline still detects a change
// after a restart.
func TestNonGitBaseManifest(t *testing.T) {
	ctx := context.Background()

	// The fixture is read-only evidence: copy it into t.TempDir() before doing
	// anything with it, because this test mutates one byte.
	root := filepath.Join(t.TempDir(), "workspace")
	accCopyFixture(t, filepath.Join("..", "api", "testdata", "journeys", "project-no-git"), root)
	if _, err := os.Stat(filepath.Join(root, ".git")); !os.IsNotExist(err) {
		t.Fatalf("the copied fixture has a .git entry (%v); this test must exercise the non-Git path", err)
	}

	manifest, err := runworkspace.Capture(ctx, nil, root, runworkspace.CaptureOptions{})
	if err != nil {
		t.Fatalf("Capture plain: %v", err)
	}
	if manifest.Mode != runworkspace.ModePlain {
		t.Errorf("manifest.Mode = %q, want %q", manifest.Mode, runworkspace.ModePlain)
	}
	if manifest.HeadCommit != nil {
		t.Errorf("manifest.HeadCommit = %q, want nil in plain mode", *manifest.HeadCommit)
	}
	if !strings.HasPrefix(manifest.Hash, "sha256:") {
		t.Errorf("manifest.Hash = %q, want a sha256: prefix", manifest.Hash)
	}
	if len(manifest.Entries) == 0 {
		t.Fatal("the plain manifest captured no entries")
	}
	t.Logf("EVIDENCE plain manifest: mode=%s head=%s entries=%d hash=%s",
		manifest.Mode, accPtr(manifest.HeadCommit), len(manifest.Entries), manifest.Hash)

	// Pin the baseline on a run of a project with no Git.
	const (
		projectID  = "p-plain"
		taskID     = "t-plain"
		runID      = "r-plain"
		snapshotID = "snap-plain"
	)
	task := run.Task{
		ID: taskID, ProjectID: projectID, Title: "baseline a non-git workspace",
		Kind: run.TaskKindCode, Status: run.TaskStatusReady,
		InputJSON: `{"prompt":"baseline"}`,
		CreatedAt: accT0, UpdatedAt: accT1,
	}
	r := run.Run{
		ID: runID, TaskID: taskID, ProjectID: projectID,
		BindingID: "binding-plain", BindingRevision: 1,
		BaseManifestHash: manifest.Hash,
		// No Git, so no commit: nil, not the empty string.
		BaseCommit:      nil,
		AgentRevisionID: "agent-rev-1", InputSnapshotID: snapshotID,
		Status: run.RunStatusQueued, CreatedAt: accT2, UpdatedAt: accT3,
	}

	store, path := newTestStore(t)
	err = store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		if err := UpsertProjectRef(ctx, tx, run.ProjectRef{
			ProjectID: projectID, SnapshotHash: "sha256:project-plain",
			State: run.ProjectRefStateActive, CapturedAt: accT0, VerifiedAt: accT1,
		}); err != nil {
			return err
		}
		if err := InsertTask(ctx, tx, &task); err != nil {
			return err
		}
		h, err := InsertInputSnapshot(ctx, tx, projectID, snapshotID,
			json.RawMessage(`{"prompt":"baseline"}`), accT2)
		if err != nil {
			return err
		}
		r.InputSnapshotHash = h
		return InsertRun(ctx, tx, &r)
	})
	if err != nil {
		t.Fatalf("seed the non-git run: %v", err)
	}

	// base_commit must be NULL in the column, not the empty string: the schema
	// distinguishes "not a Git workspace" from "a Git workspace at no commit".
	assertBaseCommitNull := func(t *testing.T, q Querier) {
		t.Helper()
		if n := accCountWhere(t, q, `SELECT COUNT(*) FROM runs WHERE id = ? AND base_commit IS NULL`, runID); n != 1 {
			t.Errorf("runs.base_commit IS NULL for %s: %d rows, want 1 (nil must be stored as NULL, not as '')", runID, n)
		}
	}
	assertBaseCommitNull(t, store.DB())

	// --- restart ---------------------------------------------------------
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	reopened, res, err := OpenStore(ctx, path)
	if err != nil {
		t.Fatalf("reopen %s: %v", path, err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if len(res.Applied) != 0 {
		t.Errorf("reopen applied %d migrations, want 0", len(res.Applied))
	}

	stored, err := GetRun(ctx, reopened.DB(), runID)
	if err != nil {
		t.Fatalf("GetRun after reopen: %v", err)
	}
	if stored.BaseCommit != nil {
		t.Fatalf("run.BaseCommit = %q after reopen, want nil (a workspace without Git has no commit; the empty string is a different fact)",
			*stored.BaseCommit)
	}
	assertBaseCommitNull(t, reopened.DB())
	if stored.BaseManifestHash != manifest.Hash {
		t.Errorf("run.BaseManifestHash = %s after reopen, want %s", stored.BaseManifestHash, manifest.Hash)
	}
	t.Logf("EVIDENCE reopen non-git run: base_commit=nil (IS NULL), base_manifest_hash=%s", stored.BaseManifestHash)

	// Capturing the unchanged directory again must reproduce the stored hash:
	// the baseline is content-addressed, not time- or path-dependent.
	again, err := runworkspace.Capture(ctx, nil, root, runworkspace.CaptureOptions{})
	if err != nil {
		t.Fatalf("Capture again: %v", err)
	}
	if again.Hash != stored.BaseManifestHash {
		t.Errorf("re-capturing the unchanged workspace = %s, the run pins %s", again.Hash, stored.BaseManifestHash)
	}
	t.Logf("EVIDENCE re-capture unchanged: %s (pinned %s)", again.Hash, stored.BaseManifestHash)

	// Change exactly one byte of one captured file. The stored baseline must no
	// longer describe the workspace, which is what makes it usable to detect
	// drift.
	target := filepath.Join(root, "src", "hello.txt")
	original, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read %s: %v", target, err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat %s: %v", target, err)
	}
	mutated := append([]byte(nil), original...)
	mutated[0] ^= 0x01 // flip one bit of the first byte: one byte changed, size unchanged
	if err := os.WriteFile(target, mutated, info.Mode().Perm()); err != nil {
		t.Fatalf("write %s: %v", target, err)
	}

	changed, err := runworkspace.Capture(ctx, nil, root, runworkspace.CaptureOptions{})
	if err != nil {
		t.Fatalf("Capture after the edit: %v", err)
	}
	if changed.Hash == stored.BaseManifestHash {
		t.Fatal("the manifest hash is unchanged after editing a captured file; the baseline cannot detect drift")
	}
	if len(changed.Entries) != len(manifest.Entries) {
		t.Errorf("entry count changed from %d to %d; a one-byte edit must not add or drop a path",
			len(manifest.Entries), len(changed.Entries))
	}
	// Exactly one entry may differ, and it must be the edited file: a hash that
	// moved for any other reason would not be evidence about this file.
	changedCount, editedEntryChanged := 0, false
	for i := range manifest.Entries {
		beforeEntry, afterEntry := manifest.Entries[i], changed.Entries[i]
		if beforeEntry.Path != afterEntry.Path {
			t.Errorf("entry %d: path %q became %q", i, beforeEntry.Path, afterEntry.Path)
			continue
		}
		if beforeEntry.ContentHash == afterEntry.ContentHash {
			continue
		}
		changedCount++
		if beforeEntry.Path == "src/hello.txt" {
			editedEntryChanged = true
			if beforeEntry.Size != afterEntry.Size {
				t.Errorf("src/hello.txt size changed from %d to %d; only its content was meant to change",
					beforeEntry.Size, afterEntry.Size)
			}
		}
	}
	if changedCount != 1 || !editedEntryChanged {
		t.Errorf("entries with a changed content hash = %d (edited file changed: %v), want exactly src/hello.txt",
			changedCount, editedEntryChanged)
	}
	t.Logf("EVIDENCE one-byte edit: manifest hash %s -> %s (entries %d, changed %d)",
		stored.BaseManifestHash, changed.Hash, len(changed.Entries), changedCount)
}

// accCopyFixture copies a directory tree without following links, preserving
// each file's permission bits, so the capture under test sees the same content
// and the same modes as the checked-in fixture.
func accCopyFixture(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("fixture contains a non-regular file: %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
	if err != nil {
		t.Fatalf("copy fixture %s: %v", src, err)
	}
}
