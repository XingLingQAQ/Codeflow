package runstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/run"
)

// These tests cover the typed operations in records.go: the invariants each
// write is responsible for, the reads that must round-trip exactly, and the
// errors a caller is expected to branch on.

func projectRef(id string) run.ProjectRef {
	return run.ProjectRef{
		ProjectID:    id,
		SnapshotHash: "sha256:project-" + id,
		State:        run.ProjectRefStateActive,
		CapturedAt:   time.UnixMilli(1700000000000),
		VerifiedAt:   time.UnixMilli(1700000000001),
	}
}

func codeTask(id, projectID string, input string) run.Task {
	return run.Task{
		ID: id, ProjectID: projectID, Title: "task " + id, Kind: run.TaskKindCode,
		Status: run.TaskStatusReady, Priority: 1, InputJSON: input,
		CreatedAt: time.UnixMilli(1700000000000), UpdatedAt: time.UnixMilli(1700000000001),
	}
}

// TestUpsertProjectRefIsIdempotentButTracksReverification pins the documented
// split: captured_at records when the snapshot was taken and must never move,
// while state/verified_at/snapshot_hash track the latest verification — that is
// what lets a later dispatch notice an archived or rebound project.
func TestUpsertProjectRefIsIdempotentButTracksReverification(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)

	ref := projectRef("p-1")
	ref.SourceRevision = int64Ptr(10)
	if err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		return UpsertProjectRef(ctx, tx, ref)
	}); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	// Re-verify: same capture instant, new verification instant, archived.
	second := ref
	second.State = run.ProjectRefStateArchived
	second.SnapshotHash = "sha256:project-p-1-v2"
	second.SourceRevision = int64Ptr(11)
	second.VerifiedAt = time.UnixMilli(1700000009999)
	if err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		return UpsertProjectRef(ctx, tx, second)
	}); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	var (
		state      string
		hash       string
		capturedAt int64
		verifiedAt int64
		sourceRev  int64
		rowCount   int
	)
	if err := store.DB().QueryRowContext(ctx,
		`SELECT state, snapshot_hash, captured_at, verified_at, source_revision FROM project_refs WHERE project_id = 'p-1'`).
		Scan(&state, &hash, &capturedAt, &verifiedAt, &sourceRev); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM project_refs`).Scan(&rowCount); err != nil {
		t.Fatalf("count: %v", err)
	}

	if rowCount != 1 {
		t.Errorf("project_refs rows = %d, want 1 (the upsert must not insert twice)", rowCount)
	}
	if state != string(run.ProjectRefStateArchived) {
		t.Errorf("state = %q, want archived (re-verification must refresh it)", state)
	}
	if hash != second.SnapshotHash {
		t.Errorf("snapshot_hash = %q, want %q", hash, second.SnapshotHash)
	}
	if sourceRev != 11 {
		t.Errorf("source_revision = %d, want 11", sourceRev)
	}
	if capturedAt != ref.CapturedAt.UnixMilli() {
		t.Errorf("captured_at = %d, want %d (it records when the snapshot was taken and must not move on re-verification)",
			capturedAt, ref.CapturedAt.UnixMilli())
	}
	if verifiedAt != second.VerifiedAt.UnixMilli() {
		t.Errorf("verified_at = %d, want %d", verifiedAt, second.VerifiedAt.UnixMilli())
	}

	// A bad ref is refused before any SQL, and an unknown project would have
	// failed the foreign key instead of inserting a stray row.
	err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		return UpsertProjectRef(ctx, tx, run.ProjectRef{ProjectID: "p-2", State: "nonsense"})
	})
	if !errors.Is(err, ErrInvalidRecord) {
		t.Errorf("upsert with an invalid state = %v, want ErrInvalidRecord", err)
	}
}

// TestPutLegacyRefUpdatesOnlyOnARealChange pins the idempotency rule: the same
// hash re-put is a no-op (so a re-verify on every dispatch does not churn the
// row), while a different hash means the legacy resource really moved and must
// be recorded.
func TestPutLegacyRefUpdatesOnlyOnARealChange(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)

	if err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		return UpsertProjectRef(ctx, tx, projectRef("p-1"))
	}); err != nil {
		t.Fatalf("project ref: %v", err)
	}

	ref := run.LegacyResourceRef{
		ProjectID: "p-1", Kind: run.RefKindFlow, ResourceID: "flow-1",
		SnapshotHash: "sha256:flow-v1", CapturedAt: time.UnixMilli(1700000000000),
	}
	put := func(r run.LegacyResourceRef) {
		t.Helper()
		if err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
			return PutLegacyRef(ctx, tx, r)
		}); err != nil {
			t.Fatalf("PutLegacyRef: %v", err)
		}
	}
	read := func() (hash string, capturedAt int64, rows int) {
		t.Helper()
		if err := store.DB().QueryRowContext(ctx,
			`SELECT snapshot_hash, captured_at FROM legacy_resource_refs WHERE project_id = 'p-1' AND kind = 'flow' AND resource_id = 'flow-1'`).
			Scan(&hash, &capturedAt); err != nil {
			t.Fatalf("read back: %v", err)
		}
		if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM legacy_resource_refs`).Scan(&rows); err != nil {
			t.Fatalf("count: %v", err)
		}
		return hash, capturedAt, rows
	}

	put(ref)
	hash, capturedAt, rows := read()
	if hash != ref.SnapshotHash || rows != 1 {
		t.Fatalf("after first put: hash=%q rows=%d, want %q and 1", hash, rows, ref.SnapshotHash)
	}

	// Same hash again: a no-op, not an error and not a new row.
	put(ref)
	againHash, againCaptured, againRows := read()
	if againHash != hash {
		t.Errorf("re-putting the same hash changed it: %q -> %q", hash, againHash)
	}
	if againCaptured != capturedAt {
		t.Errorf("re-putting the same hash moved captured_at: %d -> %d", capturedAt, againCaptured)
	}
	if againRows != 1 {
		t.Errorf("re-putting the same hash produced %d rows, want 1", againRows)
	}

	// The legacy resource really moved: the store must record the new revision.
	moved := ref
	moved.SnapshotHash = "sha256:flow-v2"
	moved.SourceRevision = int64Ptr(9)
	put(moved)
	movedHash, _, movedRows := read()
	if movedHash != moved.SnapshotHash {
		t.Errorf("after a real change hash = %q, want %q", movedHash, moved.SnapshotHash)
	}
	if movedRows != 1 {
		t.Errorf("after a real change rows = %d, want 1", movedRows)
	}

	// An unknown kind is a caller bug, and an unknown project is refused by the
	// foreign key rather than creating an orphan reference.
	err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		return PutLegacyRef(ctx, tx, run.LegacyResourceRef{ProjectID: "p-1", Kind: "nope", ResourceID: "x"})
	})
	if !errors.Is(err, ErrInvalidRecord) {
		t.Errorf("put with an invalid kind = %v, want ErrInvalidRecord", err)
	}
	err = store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		return PutLegacyRef(ctx, tx, run.LegacyResourceRef{
			ProjectID: "no-such-project", Kind: run.RefKindFlow, ResourceID: "flow-9",
			SnapshotHash: "sha256:x", CapturedAt: time.UnixMilli(1),
		})
	})
	if err == nil {
		t.Error("put with an unknown project succeeded, want a foreign-key refusal")
	}
}

// TestInsertTaskOwnsTheDerivedColumns pins what the store computes rather than
// trusts: the canonical input body, its hash, and a revision that starts at 1.
func TestInsertTaskOwnsTheDerivedColumns(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)

	if err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		return UpsertProjectRef(ctx, tx, projectRef("p-1"))
	}); err != nil {
		t.Fatalf("project ref: %v", err)
	}

	// Spaced key order the caller did not sort, plus a revision the caller got
	// wrong on purpose.
	task := codeTask("t-1", "p-1", "{\n  \"prompt\": \"go\",\n  \"n\": 1\n}")
	task.Revision = 99
	if err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		return InsertTask(ctx, tx, &task)
	}); err != nil {
		t.Fatalf("InsertTask: %v", err)
	}

	if task.Revision != 1 {
		t.Errorf("in-memory Revision = %d, want the store to fill in 1", task.Revision)
	}
	if task.InputHash == "" {
		t.Fatal("in-memory InputHash is empty; the store must fill it in")
	}
	if task.InputJSON != `{"n":1,"prompt":"go"}` {
		t.Errorf("in-memory InputJSON = %q, want the canonical form", task.InputJSON)
	}

	stored, err := GetTask(ctx, store.DB(), "t-1")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if stored.InputHash != task.InputHash {
		t.Errorf("stored InputHash = %q, the caller holds %q", stored.InputHash, task.InputHash)
	}
	if stored.InputJSON != task.InputJSON {
		t.Errorf("stored InputJSON = %q, the caller holds %q", stored.InputJSON, task.InputJSON)
	}
	if stored.Revision != 1 {
		t.Errorf("stored Revision = %d, want 1", stored.Revision)
	}
	// A caller that hands us the same input serialised differently must land on
	// the same hash, or identity checks downstream would miss the duplicate.
	clone := codeTask("t-2", "p-1", `{"prompt":"go","n":1}`)
	if err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		return InsertTask(ctx, tx, &clone)
	}); err != nil {
		t.Fatalf("InsertTask clone: %v", err)
	}
	if clone.InputHash != stored.InputHash {
		t.Errorf("equal inputs hashed differently: %q vs %q", clone.InputHash, stored.InputHash)
	}

	// Rejected before SQL.
	for _, tc := range []struct {
		name string
		mut  func(*run.Task)
	}{
		{"running is not an initial state", func(x *run.Task) { x.Status = run.TaskStatusRunning }},
		{"completed is not an initial state", func(x *run.Task) { x.Status = run.TaskStatusCompleted }},
		{"empty status", func(x *run.Task) { x.Status = "" }},
		{"invalid kind", func(x *run.Task) { x.Kind = "nope" }},
		{"empty id", func(x *run.Task) { x.ID = "" }},
		{"empty project", func(x *run.Task) { x.ProjectID = "" }},
		{"unparseable input", func(x *run.Task) { x.InputJSON = "{not json" }},
		{"trailing content", func(x *run.Task) { x.InputJSON = `{"a":1}{"b":2}` }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := codeTask("t-bad", "p-1", `{"a":1}`)
			tc.mut(&bad)
			err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
				return InsertTask(ctx, tx, &bad)
			})
			if !errors.Is(err, ErrInvalidRecord) {
				t.Errorf("InsertTask = %v, want ErrInvalidRecord", err)
			}
		})
	}
	if err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		return InsertTask(ctx, tx, nil)
	}); !errors.Is(err, ErrInvalidRecord) {
		t.Errorf("InsertTask(nil) = %v, want ErrInvalidRecord", err)
	}
}

// TestGetMissingRowsAreNotFound pins that every read reports absence with the
// same sentinel, so callers never have to know about sql.ErrNoRows.
func TestGetMissingRowsAreNotFound(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	q := store.DB()

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"GetTask", func() error { _, err := GetTask(ctx, q, "nope"); return err }},
		{"GetRun", func() error { _, err := GetRun(ctx, q, "nope"); return err }},
		{"GetAttempt", func() error { _, err := GetAttempt(ctx, q, "nope"); return err }},
		{"GetInputSnapshot", func() error { _, err := GetInputSnapshot(ctx, q, "nope"); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if err == nil {
				t.Fatal("missing row reported no error")
			}
			if !errors.Is(err, ErrNotFound) {
				t.Errorf("error = %v, want ErrNotFound", err)
			}
		})
	}

	// A list over an unknown parent is an empty list, not a missing row:
	// "this task has no runs" is a normal answer, and reporting it as
	// ErrNotFound would make a caller branch on the wrong thing.
	if rows, err := ListRunsByTask(ctx, q, "nope"); err != nil {
		t.Errorf("ListRunsByTask(unknown) = %v, want an empty list", err)
	} else if len(rows) != 0 {
		t.Errorf("ListRunsByTask(unknown) returned %d rows, want 0", len(rows))
	}
	if rows, err := ListAttemptsByRun(ctx, q, "nope"); err != nil {
		t.Errorf("ListAttemptsByRun(unknown) = %v, want an empty list", err)
	} else if len(rows) != 0 {
		t.Errorf("ListAttemptsByRun(unknown) returned %d rows, want 0", len(rows))
	}
}

// TestListRunsByTaskOrdersAndScopes pins that a task's runs come back in
// creation order and that another task's runs never appear.
func TestListRunsByTaskOrdersAndScopes(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	seedTask(t, store, "t-1", "p-1")
	seedTask(t, store, "t-2", "p-1")

	// t-1 gets three runs. The schema allows only one non-terminal run per
	// task, so each run is closed before the next is created; the creation
	// instants are deliberately out of id order, so only an ORDER BY
	// created_at can produce the expected sequence.
	for _, spec := range []struct {
		id        string
		createdAt int64
	}{
		{"r-b", 1700000002000},
		{"r-a", 1700000001000},
		{"r-c", 1700000003000},
	} {
		spec := spec
		err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
			hash, err := InsertInputSnapshot(ctx, tx, "p-1", "snap-"+spec.id, json.RawMessage(`{"a":1}`), time.UnixMilli(spec.createdAt))
			if err != nil {
				return err
			}
			r := run.Run{
				ID: spec.id, TaskID: "t-1", ProjectID: "p-1",
				BindingID: "binding-1", BindingRevision: 1, BaseManifestHash: "sha256:m",
				AgentRevisionID: "agent-rev-1",
				InputSnapshotID: "snap-" + spec.id, InputSnapshotHash: hash,
				Status:    run.RunStatusQueued,
				CreatedAt: time.UnixMilli(spec.createdAt), UpdatedAt: time.UnixMilli(spec.createdAt),
			}
			if err := InsertRun(ctx, tx, &r); err != nil {
				return err
			}
			_, err = UpdateRunStatusCAS(ctx, tx, spec.id, 1, run.RunStatusCompleted, time.UnixMilli(spec.createdAt+1))
			return err
		})
		if err != nil {
			t.Fatalf("seed run %s: %v", spec.id, err)
		}
	}

	// t-2 gets one run that must never leak into t-1's list.
	err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		hash, err := InsertInputSnapshot(ctx, tx, "p-1", "snap-t2", json.RawMessage(`{"a":2}`), time.UnixMilli(1700000001500))
		if err != nil {
			return err
		}
		r := run.Run{
			ID: "r-t2", TaskID: "t-2", ProjectID: "p-1",
			BindingID: "binding-1", BindingRevision: 1, BaseManifestHash: "sha256:m",
			AgentRevisionID: "agent-rev-1",
			InputSnapshotID: "snap-t2", InputSnapshotHash: hash,
			Status:    run.RunStatusQueued,
			CreatedAt: time.UnixMilli(1700000001500), UpdatedAt: time.UnixMilli(1700000001500),
		}
		return InsertRun(ctx, tx, &r)
	})
	if err != nil {
		t.Fatalf("seed t-2 run: %v", err)
	}

	runs, err := ListRunsByTask(ctx, store.DB(), "t-1")
	if err != nil {
		t.Fatalf("ListRunsByTask: %v", err)
	}
	var ids []string
	for _, r := range runs {
		ids = append(ids, r.ID)
	}
	want := []string{"r-a", "r-b", "r-c"}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Errorf("run ids = %v, want %v ordered by created_at", ids, want)
	}

	t2Runs, err := ListRunsByTask(ctx, store.DB(), "t-2")
	if err != nil {
		t.Fatalf("ListRunsByTask(t-2): %v", err)
	}
	if len(t2Runs) != 1 || t2Runs[0].ID != "r-t2" {
		t.Errorf("t-2 runs = %+v, want only r-t2", t2Runs)
	}
}

// TestListAttemptsByRunOrdersByAttemptNo pins ascending attempt order, which is
// the order a retry history has to be read in.
func TestListAttemptsByRunOrdersByAttemptNo(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	seedTask(t, store, "t-1", "p-1")
	insertSnapshotAndRun(t, store, "r-1", "t-1", "p-1", "snap-1", json.RawMessage(`{"a":1}`))

	// Insert no. 3, then no. 1, then no. 2: only an ORDER BY can produce 1,2,3.
	// Only one attempt per run may be active, so all but the last are terminal.
	for _, spec := range []struct {
		id     string
		no     int64
		status run.AttemptStatus
	}{
		{"a-3", 3, run.AttemptStatusExited},
		{"a-1", 1, run.AttemptStatusExited},
		{"a-2", 2, run.AttemptStatusRunning},
	} {
		spec := spec
		err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
			return InsertAttempt(ctx, tx, &run.Attempt{
				ID: spec.id, RunID: "r-1", AttemptNo: spec.no, Backend: "fake",
				Status: spec.status, CreatedAt: time.UnixMilli(1700000000004),
			})
		})
		if err != nil {
			t.Fatalf("InsertAttempt %s: %v", spec.id, err)
		}
	}

	attempts, err := ListAttemptsByRun(ctx, store.DB(), "r-1")
	if err != nil {
		t.Fatalf("ListAttemptsByRun: %v", err)
	}
	var got []string
	for _, a := range attempts {
		got = append(got, a.ID)
	}
	want := []string{"a-1", "a-2", "a-3"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("attempt ids = %v, want %v ordered by attempt_no", got, want)
	}

	// A duplicate attempt number is refused by the schema, not silently written.
	err = store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		return InsertAttempt(ctx, tx, &run.Attempt{
			ID: "a-dup", RunID: "r-1", AttemptNo: 1, Backend: "fake",
			Status: run.AttemptStatusExited, CreatedAt: time.UnixMilli(1700000000005),
		})
	})
	if err == nil {
		t.Error("a duplicate attempt_no was accepted, want a uniqueness refusal")
	}
}

// TestInsertRunRejectsNonQueuedStatus pins the insert-time half of the run
// lifecycle: a run is born queued, and the store refuses to be talked out of it.
func TestInsertRunRejectsNonQueuedStatus(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	seedTask(t, store, "t-1", "p-1")

	for _, status := range []run.RunStatus{
		run.RunStatusStarting, run.RunStatusRunning, run.RunStatusCompleted,
		run.RunStatusFailed, run.RunStatusCancelled, run.RunStatusExpired, "",
	} {
		status := status
		t.Run(string(status)+"_is_refused", func(t *testing.T) {
			err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
				r := run.Run{
					ID: "r-1", TaskID: "t-1", ProjectID: "p-1",
					BindingID: "binding-1", BindingRevision: 1, BaseManifestHash: "sha256:m",
					AgentRevisionID: "agent-rev-1",
					InputSnapshotID: "snap-1", InputSnapshotHash: "sha256:x",
					Status:    status,
					CreatedAt: time.UnixMilli(1), UpdatedAt: time.UnixMilli(1),
				}
				return InsertRun(ctx, tx, &r)
			})
			if !errors.Is(err, ErrInvalidRecord) {
				t.Errorf("InsertRun(status=%q) = %v, want ErrInvalidRecord", status, err)
			}
		})
	}
}

// TestInsertRunBudgetRoundTrips pins that the budget survives the write path as
// a value, with nil limits staying nil rather than becoming zero.
func TestInsertRunBudgetRoundTrips(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	seedTask(t, store, "t-1", "p-1")

	// budget_json is a frozen column (a run's budget is part of its frozen
	// input), so the two runs go on two tasks rather than being rewritten.
	seedTask(t, store, "t-2", "p-1")

	// An empty budget is the default and must read back as "no limits", not as
	// a budget of zeroes. It is written inline rather than through the shared
	// helper, which seeds a token limit.
	err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		hash, err := InsertInputSnapshot(ctx, tx, "p-1", "snap-empty", json.RawMessage(`{}`), time.UnixMilli(1))
		if err != nil {
			return err
		}
		r := run.Run{
			ID: "r-empty", TaskID: "t-1", ProjectID: "p-1",
			BindingID: "binding-1", BindingRevision: 1, BaseManifestHash: "sha256:m",
			AgentRevisionID: "agent-rev-1",
			InputSnapshotID: "snap-empty", InputSnapshotHash: hash,
			Status:    run.RunStatusQueued,
			CreatedAt: time.UnixMilli(1), UpdatedAt: time.UnixMilli(1),
		}
		return InsertRun(ctx, tx, &r)
	})
	if err != nil {
		t.Fatalf("seed empty budget: %v", err)
	}
	empty, err := GetRun(ctx, store.DB(), "r-empty")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if empty.Budget.WallTimeSeconds != nil || empty.Budget.Tokens != nil || empty.Budget.CostLimitMinor != nil {
		t.Fatalf("empty budget came back as %+v, want every limit nil", empty.Budget)
	}
	if empty.Budget.Currency != "" {
		t.Errorf("empty budget currency = %q, want empty", empty.Budget.Currency)
	}

	// A fully populated budget, with one limit deliberately left nil.
	wall, tokens := int64(1800), int64(50000)
	err = store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		hash, err := InsertInputSnapshot(ctx, tx, "p-1", "snap-full", json.RawMessage(`{}`), time.UnixMilli(1))
		if err != nil {
			return err
		}
		r := run.Run{
			ID: "r-full", TaskID: "t-2", ProjectID: "p-1",
			BindingID: "binding-1", BindingRevision: 1, BaseManifestHash: "sha256:m",
			AgentRevisionID: "agent-rev-1",
			InputSnapshotID: "snap-full", InputSnapshotHash: hash,
			Budget:    run.Budget{WallTimeSeconds: &wall, Tokens: &tokens, Currency: "USD"},
			Status:    run.RunStatusQueued,
			CreatedAt: time.UnixMilli(1), UpdatedAt: time.UnixMilli(1),
		}
		return InsertRun(ctx, tx, &r)
	})
	if err != nil {
		t.Fatalf("seed full budget: %v", err)
	}
	full, err := GetRun(ctx, store.DB(), "r-full")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if !equalBudget(full.Budget, run.Budget{WallTimeSeconds: &wall, Tokens: &tokens, Currency: "USD"}) {
		t.Errorf("budget = %+v, want wall=1800 tokens=50000 currency=USD cost=nil", full.Budget)
	}
	if full.Budget.CostLimitMinor != nil {
		t.Errorf("cost_limit_minor = %v, want nil (a limit that was never set must not become 0)", *full.Budget.CostLimitMinor)
	}
}

// TestRunSnapshotIsImmutableAfterInsert pins that the frozen input columns
// cannot be edited in place: changing a run's input requires a new run.
func TestRunSnapshotIsImmutableAfterInsert(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	seedTask(t, store, "t-1", "p-1")
	_, hash := insertSnapshotAndRun(t, store, "r-1", "t-1", "p-1", "snap-1", json.RawMessage(`{"a":1}`))

	// A different, real snapshot for the same project.
	otherHash := func() string {
		var h string
		if err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
			var err error
			h, err = InsertInputSnapshot(ctx, tx, "p-1", "snap-other", json.RawMessage(`{"a":2}`), time.UnixMilli(1))
			return err
		}); err != nil {
			t.Fatalf("second snapshot: %v", err)
		}
		return h
	}()

	for _, tc := range []struct {
		name  string
		query string
		args  []any
	}{
		// revision = revision + 1 is included on purpose: without it the
		// revision trigger would fire first and mask the guard under test.
		{"input_snapshot_id", `UPDATE runs SET input_snapshot_id = ?, revision = revision + 1 WHERE id = 'r-1'`, []any{"snap-other"}},
		{"input_snapshot_hash", `UPDATE runs SET input_snapshot_hash = ?, revision = revision + 1 WHERE id = 'r-1'`, []any{otherHash}},
		{"binding_id", `UPDATE runs SET binding_id = ?, revision = revision + 1 WHERE id = 'r-1'`, []any{"binding-2"}},
		{"base_manifest_hash", `UPDATE runs SET base_manifest_hash = ?, revision = revision + 1 WHERE id = 'r-1'`, []any{"sha256:other"}},
		{"agent_revision_id", `UPDATE runs SET agent_revision_id = ?, revision = revision + 1 WHERE id = 'r-1'`, []any{"agent-rev-2"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := store.DB().ExecContext(ctx, tc.query, tc.args...)
			if err == nil {
				t.Fatalf("%s was updated in place, want the frozen-input guard to refuse", tc.query)
			}
			got := mapConstraintError("test", err)
			if !errors.Is(got, ErrFrozenInputImmutable) {
				t.Errorf("mapped error = %v (raw %v), want ErrFrozenInputImmutable", got, err)
			}
		})
	}

	// The pin is unchanged after all those attempts.
	r, err := GetRun(ctx, store.DB(), "r-1")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if r.InputSnapshotID != "snap-1" || r.InputSnapshotHash != hash {
		t.Errorf("run now pins (%s,%s), want (snap-1,%s)", r.InputSnapshotID, r.InputSnapshotHash, hash)
	}

	// Snapshot bodies are immutable too.
	_, err = store.DB().ExecContext(ctx, `UPDATE input_snapshots SET content_json = '{"a":9}' WHERE id = 'snap-1'`)
	if err == nil {
		t.Fatal("a snapshot body was updated in place, want the immutability trigger to refuse")
	}
	if !strings.Contains(err.Error(), "input_snapshot_immutable") {
		t.Errorf("error %q does not name input_snapshot_immutable", err)
	}
	if got := mapConstraintError("test", err); !errors.Is(got, ErrFrozenInputImmutable) {
		t.Errorf("mapped error = %v, want ErrFrozenInputImmutable", got)
	}
}

// TestInsertInputSnapshotRejectsMalformedBodies pins that an unparseable body
// is refused before it can be hashed, and that the container-level identity
// arguments are checked.
func TestInsertInputSnapshotRejectsMalformedBodies(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	seedTask(t, store, "t-1", "p-1")

	for _, tc := range []struct {
		name    string
		content string
	}{
		{"not JSON", `nonsense`},
		{"truncated", `{"a":`},
		{"two documents", `{"a":1}{"b":2}`},
		{"trailing garbage", `{"a":1} trailing`},
		{"empty", ``},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
				_, err := InsertInputSnapshot(ctx, tx, "p-1", "snap-bad", json.RawMessage(tc.content), time.UnixMilli(1))
				return err
			})
			if !errors.Is(err, ErrInvalidRecord) {
				t.Errorf("InsertInputSnapshot(%q) = %v, want ErrInvalidRecord", tc.content, err)
			}
		})
	}

	for _, tc := range []struct {
		name      string
		projectID string
		id        string
	}{
		{"empty project", "", "snap-x"},
		{"empty id", "p-1", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
				_, err := InsertInputSnapshot(ctx, tx, tc.projectID, tc.id, json.RawMessage(`{}`), time.UnixMilli(1))
				return err
			})
			if !errors.Is(err, ErrInvalidRecord) {
				t.Errorf("InsertInputSnapshot = %v, want ErrInvalidRecord", err)
			}
		})
	}

	// A snapshot for a project that is not registered is refused by the foreign
	// key, so a snapshot can never outlive its project anchor.
	err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		_, err := InsertInputSnapshot(ctx, tx, "no-such-project", "snap-orphan", json.RawMessage(`{}`), time.UnixMilli(1))
		return err
	})
	if err == nil {
		t.Error("a snapshot for an unknown project was accepted, want a foreign-key refusal")
	}

	// A duplicate id is refused rather than overwriting a frozen body.
	if err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		_, err := InsertInputSnapshot(ctx, tx, "p-1", "snap-dup", json.RawMessage(`{"a":1}`), time.UnixMilli(1))
		return err
	}); err != nil {
		t.Fatalf("first snapshot: %v", err)
	}
	err = store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		_, err := InsertInputSnapshot(ctx, tx, "p-1", "snap-dup", json.RawMessage(`{"a":2}`), time.UnixMilli(2))
		return err
	})
	if err == nil {
		t.Error("a duplicate snapshot id was accepted, want a uniqueness refusal")
	}
}

// TestCanonicalJSONNumbersAndScalars pins the parts of the canonical form that
// the higher-level tests do not reach: numbers keep their exact spelling, and
// the JSON scalars round-trip.
func TestCanonicalJSONNumbersAndScalars(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"nested sort", `{"b":{"d":1,"c":2},"a":[1,2]}`, `{"a":[1,2],"b":{"c":2,"d":1}}`},
		{"scale is preserved", `{"n":1.0}`, `{"n":1.0}`},
		{"exponent is preserved", `{"n":1e3}`, `{"n":1e3}`},
		{"high precision is preserved", `{"n":0.10000000000000000001}`, `{"n":0.10000000000000000001}`},
		{"negative zero is preserved", `{"n":-0}`, `{"n":-0}`},
		{"scalars", `{"t":true,"f":false,"z":null,"s":"x"}`, `{"f":false,"s":"x","t":true,"z":null}`},
		{"string escaping", `{"s":"a\"b\n\té"}`, `{"s":"a\"b\n\té"}`},
		{"empty containers", `{"o":{},"a":[]}`, `{"a":[],"o":{}}`},
		{"unicode keys sort by byte", `{"é":1,"z":2}`, `{"z":2,"é":1}`},
		{"top-level scalar", `  42  `, `42`},
		{"top-level null", `null`, `null`},
		{"top-level string", `"hi"`, `"hi"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, _, err := canonicalJSON([]byte(tc.in))
			if err != nil {
				t.Fatalf("canonicalJSON(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("canonicalJSON(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}

	// Numbers that differ only in spelling must hash differently: they are
	// different bytes and the hash is over the canonical text.
	for _, pair := range [][2]string{
		{`{"n":1}`, `{"n":1.0}`},
		{`{"n":1}`, `{"n":1e0}`},
		{`{"n":"1"}`, `{"n":1}`},
		{`{"n":true}`, `{"n":1}`},
	} {
		a, _, err := canonicalJSON([]byte(pair[0]))
		if err != nil {
			t.Fatalf("canonicalJSON(%q): %v", pair[0], err)
		}
		b, _, err := canonicalJSON([]byte(pair[1]))
		if err != nil {
			t.Fatalf("canonicalJSON(%q): %v", pair[1], err)
		}
		if a == b {
			t.Errorf("%s and %s both canonicalised to %q; they are different values", pair[0], pair[1], a)
		}
	}
}

// TestMapConstraintErrorLeavesUnknownErrorsAlone pins the translation layer: a
// known RAISE name becomes a typed error, everything else keeps its own text so
// a real bug is not disguised as a constraint violation.
func TestMapConstraintErrorLeavesUnknownErrorsAlone(t *testing.T) {
	// The names are exactly the RAISE strings in migrations/001_runtime.sql and
	// 002_input_snapshots.sql. They are copied here rather than referenced so
	// that renaming a trigger in a migration fails this test.
	known := []struct {
		msg  string
		want error
	}{
		{"run_terminal_immutable", ErrTerminalImmutable},
		{"task_terminal_immutable", ErrTerminalImmutable},
		{"attempt_terminal_immutable", ErrTerminalImmutable},
		{"run_input_snapshot_mismatch", ErrInputSnapshotMismatch},
		{"input_snapshot_immutable", ErrFrozenInputImmutable},
		{"run_frozen_input_immutable", ErrFrozenInputImmutable},
		{"task_input_immutable", ErrFrozenInputImmutable},
		{"run_history_delete_forbidden", ErrHistoryDeleteForbidden},
		{"run_revision_not_incremented", ErrInvalidRecord},
		{"attempt_identity_immutable", ErrInvalidRecord},
	}
	for _, tc := range known {
		got := mapConstraintError("op", errors.New("constraint failed: "+tc.msg))
		if !errors.Is(got, tc.want) {
			t.Errorf("mapConstraintError(%q) = %v, want %v", tc.msg, got, tc.want)
		}
	}

	// A nil error stays nil.
	if got := mapConstraintError("op", nil); got != nil {
		t.Errorf("mapConstraintError(nil) = %v, want nil", got)
	}

	// An unrelated error keeps its text and gains the operation name, so the
	// caller can still see what failed.
	plain := errors.New("disk on fire")
	got := mapConstraintError("insert task t-1", plain)
	if got == nil || !strings.Contains(got.Error(), "disk on fire") || !strings.Contains(got.Error(), "insert task t-1") {
		t.Errorf("mapConstraintError(plain) = %v, want it to keep the original text and the operation", got)
	}
	if errors.Is(got, ErrInvalidRecord) || errors.Is(got, ErrTerminalImmutable) {
		t.Errorf("an unrelated error was mapped onto a constraint sentinel: %v", got)
	}

	// A known name hidden inside a larger driver message is still recognised:
	// the driver composes text ("... (1811)") around the RAISE name.
	wrapped := fmt.Errorf("step failed: %w", errors.New("constraint failed: run_history_delete_forbidden (1811)"))
	if !errors.Is(mapConstraintError("op", wrapped), ErrHistoryDeleteForbidden) {
		t.Errorf("a wrapped RAISE name was not recognised: %v", wrapped)
	}
}

// TestGetInputSnapshotReturnsStoredBody pins that the body read back is the
// canonical text that was hashed, and that created_at is the millisecond the
// caller passed.
func TestGetInputSnapshotReturnsStoredBody(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	seedTask(t, store, "t-1", "p-1")

	when := time.UnixMilli(1700000042424)
	var hash string
	if err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		var err error
		hash, err = InsertInputSnapshot(ctx, tx, "p-1", "snap-1",
			json.RawMessage("{\n \"b\": 2,\n \"a\": 1\n}"), when)
		return err
	}); err != nil {
		t.Fatalf("InsertInputSnapshot: %v", err)
	}

	snap, err := GetInputSnapshot(ctx, store.DB(), "snap-1")
	if err != nil {
		t.Fatalf("GetInputSnapshot: %v", err)
	}
	if snap.ID != "snap-1" || snap.ProjectID != "p-1" {
		t.Errorf("identity = (%s,%s), want (snap-1,p-1)", snap.ID, snap.ProjectID)
	}
	if snap.ContentJSON != `{"a":1,"b":2}` {
		t.Errorf("ContentJSON = %q, want the canonical text", snap.ContentJSON)
	}
	if snap.ContentHash != hash {
		t.Errorf("ContentHash = %q, the insert returned %q", snap.ContentHash, hash)
	}
	if !snap.CreatedAt.Equal(when) {
		t.Errorf("CreatedAt = %v (%d ms), want %v (%d ms)", snap.CreatedAt, snap.CreatedAt.UnixMilli(), when, when.UnixMilli())
	}
	if snap.CreatedAt.Location() != time.UTC {
		t.Errorf("CreatedAt location = %v, want UTC", snap.CreatedAt.Location())
	}

	// The hash stored in the row equals the hash of the stored canonical text,
	// which is what makes the pin verifiable later.
	if _, recomputed, err := canonicalJSON([]byte(snap.ContentJSON)); err != nil {
		t.Fatalf("re-canonicalise stored body: %v", err)
	} else if recomputed != snap.ContentHash {
		t.Errorf("hash of the stored body = %q, the row says %q", recomputed, snap.ContentHash)
	}
}

// TestSecretsAreFoundThroughTheWholeDocument pins the traversal: a secret is
// caught wherever it sits, and a reference that merely mentions a credential is
// not a false positive.
func TestSecretsAreFoundThroughTheWholeDocument(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"deep object", `{"a":{"b":{"c":{"d":{"password":"x"}}}}}`, true},
		{"array of arrays", `{"a":[[[{"token":"x"}]]]}`, true},
		{"object inside array inside object", `{"steps":[{"params":{"authorization":"x"}}]}`, true},
		{"sibling key is fine", `{"password_policy":{"min_length":12}}`, false},
		{"reference beside a real value", `{"secret_ref":"vault://x","token_name":"ci"}`, false},
		{"keyword in a value", `{"note":"rotate the password monthly"}`, false},
		{"password_hint is not a password", `{"password_hint":"first pet"}`, false},
		{"nested reference", `{"a":{"b":{"db_secret_ref":"vault://y"}}}`, false},
		{"array of references", `{"refs":[{"token_name":"a"},{"api_key_id":"b"}]}`, false},
		{"empty object", `{}`, false},
		{"empty array", `[]`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkNoSecrets([]byte(tc.body))
			switch {
			case tc.want && err == nil:
				t.Errorf("body %s was accepted, want a secret refusal", tc.body)
			case tc.want && !errors.Is(err, ErrSecretInSnapshot):
				t.Errorf("body %s gave %v, want ErrSecretInSnapshot", tc.body, err)
			case !tc.want && err != nil:
				t.Errorf("body %s was refused (%v), want acceptance", tc.body, err)
			}
			if err != nil && tc.want {
				// The path names the key; the value never appears.
				if strings.Contains(err.Error(), "x") && strings.HasSuffix(tc.body, `"x"}}}`) {
					t.Errorf("the refusal may leak the value: %v", err)
				}
				t.Logf("refused: %v", err)
			}
		})
	}
}
