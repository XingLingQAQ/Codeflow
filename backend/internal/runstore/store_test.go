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

	"github.com/codeflow/backend/internal/dbx"
	"github.com/codeflow/backend/internal/run"
	sqlite "modernc.org/sqlite"
)

// newTestStore opens a migrated Store over a file database in t.TempDir(). A
// file database (not ":memory:") is deliberate: WAL locking, cross-connection
// concurrency and close/reopen are all part of what this card must prove.
func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "codeflow.db")
	store, res, err := OpenStore(context.Background(), path)
	if err != nil {
		t.Fatalf("OpenStore(%s): %v", path, err)
	}
	if res.ToVersion != 2 {
		t.Fatalf("OpenStore migrated to version %d, want 2", res.ToVersion)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, path
}

// seedTask inserts the project ref and one ready task, returning the task as
// stored (so a test compares against what the store actually wrote).
func seedTask(t *testing.T, store *Store, taskID, projectID string) run.Task {
	t.Helper()
	task := run.Task{
		ID:        taskID,
		ProjectID: projectID,
		Title:     "add tests",
		Kind:      run.TaskKindCode,
		Status:    run.TaskStatusReady,
		Priority:  3,
		InputJSON: `{"prompt":"add tests","count":1}`,
		CreatedAt: time.UnixMilli(1700000000000),
		UpdatedAt: time.UnixMilli(1700000000001),
	}
	err := store.WithTx(context.Background(), func(ctx context.Context, tx Tx) error {
		if err := UpsertProjectRef(ctx, tx, run.ProjectRef{
			ProjectID:    projectID,
			SnapshotHash: "sha256:project",
			State:        run.ProjectRefStateActive,
			CapturedAt:   time.UnixMilli(1700000000000),
			VerifiedAt:   time.UnixMilli(1700000000001),
		}); err != nil {
			return err
		}
		return InsertTask(ctx, tx, &task)
	})
	if err != nil {
		t.Fatalf("seed task %s: %v", taskID, err)
	}
	return task
}

// insertSnapshotAndRun writes one snapshot and one queued run pinned to it, in
// a single transaction, and returns both.
func insertSnapshotAndRun(t *testing.T, store *Store, runID, taskID, projectID, snapshotID string, content json.RawMessage) (run.Run, string) {
	t.Helper()
	r := run.Run{
		ID:               runID,
		TaskID:           taskID,
		ProjectID:        projectID,
		BindingID:        "binding-1",
		BindingRevision:  1,
		BaseManifestHash: "sha256:manifest",
		AgentRevisionID:  "agent-rev-1",
		InputSnapshotID:  snapshotID,
		Budget:           run.Budget{Tokens: int64Ptr(50000)},
		Status:           run.RunStatusQueued,
		CreatedAt:        time.UnixMilli(1700000000002),
		UpdatedAt:        time.UnixMilli(1700000000003),
	}
	var hash string
	err := store.WithTx(context.Background(), func(ctx context.Context, tx Tx) error {
		h, err := InsertInputSnapshot(ctx, tx, projectID, snapshotID, content, time.UnixMilli(1700000000002))
		if err != nil {
			return err
		}
		hash = h
		r.InputSnapshotHash = h
		return InsertRun(ctx, tx, &r)
	})
	if err != nil {
		t.Fatalf("insert snapshot+run %s: %v", runID, err)
	}
	return r, hash
}

func int64Ptr(v int64) *int64 { return &v }

func sqliteExtended(err error) int {
	var serr *sqlite.Error
	if errors.As(err, &serr) {
		return serr.Code()
	}
	return -1
}

// TestWithTxCommitAndRollback covers the transaction contract: a nil return
// commits, an error rolls back leaving nothing behind, and a panic rolls back
// and is re-raised rather than swallowed.
func TestWithTxCommitAndRollback(t *testing.T) {
	ctx := context.Background()

	t.Run("nil commits", func(t *testing.T) {
		store, _ := newTestStore(t)
		task := seedTask(t, store, "t-commit", "p-1")

		err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
			got, err := GetTask(ctx, tx, "t-commit")
			if err != nil {
				return err
			}
			if got.InputHash != task.InputHash {
				t.Errorf("inside the transaction InputHash = %s, want %s", got.InputHash, task.InputHash)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("WithTx: %v", err)
		}
		if _, err := GetTask(ctx, store.DB(), "t-commit"); err != nil {
			t.Fatalf("committed task is not readable through the pool: %v", err)
		}
	})

	t.Run("error rolls back with no residue", func(t *testing.T) {
		store, _ := newTestStore(t)
		seedTask(t, store, "t-rollback", "p-1")
		sentinel := errors.New("caller said stop")

		err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
			// A write that must not survive the rollback.
			if _, err := InsertInputSnapshot(ctx, tx, "p-1", "snap-rolled-back", json.RawMessage(`{"a":1}`), time.UnixMilli(1)); err != nil {
				return err
			}
			// It is visible inside the transaction...
			if _, err := GetInputSnapshot(ctx, tx, "snap-rolled-back"); err != nil {
				return fmt.Errorf("snapshot not visible inside its own transaction: %w", err)
			}
			return sentinel
		})
		if !errors.Is(err, sentinel) {
			t.Fatalf("WithTx error = %v, want the caller's sentinel unchanged", err)
		}
		// ...and gone afterwards.
		if _, err := GetInputSnapshot(ctx, store.DB(), "snap-rolled-back"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("rolled-back snapshot is still readable: %v", err)
		}
	})

	t.Run("panic rolls back and re-panics", func(t *testing.T) {
		store, _ := newTestStore(t)
		seedTask(t, store, "t-panic", "p-1")

		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("WithTx swallowed the panic; it must re-raise after rollback")
			}
			if r != "boom" {
				t.Fatalf("recovered %v, want the original panic value", r)
			}
			// The rollback must already have happened by the time the panic
			// reaches the caller.
			if _, err := GetInputSnapshot(ctx, store.DB(), "snap-panicked"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("snapshot written before the panic survived the rollback: %v", err)
			}
			// The store must still be usable afterwards.
			if _, err := GetTask(ctx, store.DB(), "t-panic"); err != nil {
				t.Fatalf("store unusable after a recovered panic: %v", err)
			}
		}()

		_ = store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
			if _, err := InsertInputSnapshot(ctx, tx, "p-1", "snap-panicked", json.RawMessage(`{"a":1}`), time.UnixMilli(1)); err != nil {
				return err
			}
			panic("boom")
		})
		t.Fatal("WithTx returned after a panic")
	})

	t.Run("nil function is refused", func(t *testing.T) {
		store, _ := newTestStore(t)
		if err := store.WithTx(ctx, nil); err == nil {
			t.Fatal("WithTx(nil) succeeded, want an error")
		}
	})
}

// TestInsertRunRequiresMatchingSnapshot covers the snapshot identity contract
// from the acceptance list: a run may only pin a snapshot that exists, belongs
// to its own project and really hashes to the pinned value.
func TestInsertRunRequiresMatchingSnapshot(t *testing.T) {
	ctx := context.Background()

	baseRun := func(snapshotID, snapshotHash, projectID string) run.Run {
		return run.Run{
			ID:                "r-1",
			TaskID:            "t-1",
			ProjectID:         projectID,
			BindingID:         "binding-1",
			BindingRevision:   1,
			BaseManifestHash:  "sha256:manifest",
			AgentRevisionID:   "agent-rev-1",
			InputSnapshotID:   snapshotID,
			InputSnapshotHash: snapshotHash,
			Status:            run.RunStatusQueued,
			CreatedAt:         time.UnixMilli(1700000000002),
			UpdatedAt:         time.UnixMilli(1700000000003),
		}
	}

	t.Run("matching snapshot succeeds", func(t *testing.T) {
		store, _ := newTestStore(t)
		seedTask(t, store, "t-1", "p-1")
		_, hash := insertSnapshotAndRun(t, store, "r-1", "t-1", "p-1", "snap-1", json.RawMessage(`{"prompt":"go"}`))
		got, err := GetRun(ctx, store.DB(), "r-1")
		if err != nil {
			t.Fatalf("GetRun: %v", err)
		}
		if got.InputSnapshotHash != hash {
			t.Errorf("stored snapshot hash = %s, want the hash InsertInputSnapshot returned (%s)", got.InputSnapshotHash, hash)
		}
		if got.Revision != 1 {
			t.Errorf("new run revision = %d, want 1", got.Revision)
		}
		if got.Status != run.RunStatusQueued {
			t.Errorf("new run status = %s, want queued", got.Status)
		}
	})

	t.Run("missing snapshot is refused", func(t *testing.T) {
		store, _ := newTestStore(t)
		seedTask(t, store, "t-1", "p-1")
		err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
			r := baseRun("no-such-snapshot", "sha256:whatever", "p-1")
			return InsertRun(ctx, tx, &r)
		})
		if !errors.Is(err, ErrInputSnapshotMismatch) {
			t.Fatalf("InsertRun with a missing snapshot = %v, want ErrInputSnapshotMismatch", err)
		}
		// Nothing may have been written.
		if _, err := GetRun(ctx, store.DB(), "r-1"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("a refused run is still present: %v", err)
		}
	})

	t.Run("hash mismatch is refused", func(t *testing.T) {
		store, _ := newTestStore(t)
		seedTask(t, store, "t-1", "p-1")
		err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
			if _, err := InsertInputSnapshot(ctx, tx, "p-1", "snap-1", json.RawMessage(`{"prompt":"go"}`), time.UnixMilli(1)); err != nil {
				return err
			}
			// Same id and project, wrong hash: the run would pin an input that
			// is not the body stored under that id.
			r := baseRun("snap-1", "sha256:0000000000000000000000000000000000000000000000000000000000000000", "p-1")
			return InsertRun(ctx, tx, &r)
		})
		if !errors.Is(err, ErrInputSnapshotMismatch) {
			t.Fatalf("InsertRun with a wrong hash = %v, want ErrInputSnapshotMismatch", err)
		}
	})

	t.Run("cross-project snapshot is refused", func(t *testing.T) {
		store, _ := newTestStore(t)
		seedTask(t, store, "t-1", "p-1")
		err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
			// p-2 exists, and its snapshot is real — but it is not p-1's input.
			if err := UpsertProjectRef(ctx, tx, run.ProjectRef{
				ProjectID: "p-2", SnapshotHash: "sha256:project2", State: run.ProjectRefStateActive,
				CapturedAt: time.UnixMilli(1), VerifiedAt: time.UnixMilli(1),
			}); err != nil {
				return err
			}
			hash, err := InsertInputSnapshot(ctx, tx, "p-2", "snap-p2", json.RawMessage(`{"prompt":"other"}`), time.UnixMilli(1))
			if err != nil {
				return err
			}
			r := baseRun("snap-p2", hash, "p-1")
			return InsertRun(ctx, tx, &r)
		})
		if !errors.Is(err, ErrInputSnapshotMismatch) {
			t.Fatalf("InsertRun with another project's snapshot = %v, want ErrInputSnapshotMismatch", err)
		}
	})

	t.Run("a run that is not queued is refused before any SQL", func(t *testing.T) {
		store, _ := newTestStore(t)
		seedTask(t, store, "t-1", "p-1")
		err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
			r := baseRun("snap-1", "sha256:x", "p-1")
			r.Status = run.RunStatusRunning
			return InsertRun(ctx, tx, &r)
		})
		if !errors.Is(err, ErrInvalidRecord) {
			t.Fatalf("InsertRun with status running = %v, want ErrInvalidRecord", err)
		}
	})
}

// TestInputSnapshotCanonicalHash pins the hash contract: the hash depends on
// the decoded value, not on the caller's serialisation, and it is a sha256 in
// the documented shape.
func TestInputSnapshotCanonicalHash(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	seedTask(t, store, "t-1", "p-1")

	insert := func(id string, content string) string {
		t.Helper()
		var hash string
		err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
			h, err := InsertInputSnapshot(ctx, tx, "p-1", id, json.RawMessage(content), time.UnixMilli(1))
			hash = h
			return err
		})
		if err != nil {
			t.Fatalf("InsertInputSnapshot(%s): %v", id, err)
		}
		return hash
	}

	t.Run("key order and whitespace do not change the hash", func(t *testing.T) {
		a := insert("snap-a", `{"prompt":"go","count":3}`)
		b := insert("snap-b", "{\n  \"count\": 3,\n  \"prompt\": \"go\"\n}")
		if a != b {
			t.Errorf("canonicalisation failed: %s != %s for the same object", a, b)
		}
	})

	t.Run("different content changes the hash", func(t *testing.T) {
		a := insert("snap-c", `{"prompt":"go"}`)
		b := insert("snap-d", `{"prompt":"stop"}`)
		if a == b {
			t.Errorf("different bodies produced the same hash %s", a)
		}
	})

	t.Run("hash shape is sha256:<64 lowercase hex>", func(t *testing.T) {
		hash := insert("snap-e", `{"prompt":"go"}`)
		if !strings.HasPrefix(hash, "sha256:") {
			t.Fatalf("hash %q does not start with sha256:", hash)
		}
		hexPart := strings.TrimPrefix(hash, "sha256:")
		if len(hexPart) != 64 {
			t.Errorf("hex part %q has %d characters, want 64", hexPart, len(hexPart))
		}
		for _, r := range hexPart {
			if !strings.ContainsRune("0123456789abcdef", r) {
				t.Fatalf("hex part %q contains %q, want lowercase hex only", hexPart, r)
			}
		}
	})

	t.Run("large integers keep every digit", func(t *testing.T) {
		// A 20-digit value cannot survive a float64 round-trip: 12345678901234567890
		// would come back as 12345678901234567168.
		const big = "12345678901234567890"
		hash := insert("snap-big", `{"id":`+big+`}`)
		snap, err := GetInputSnapshot(ctx, store.DB(), "snap-big")
		if err != nil {
			t.Fatalf("GetInputSnapshot: %v", err)
		}
		if !strings.Contains(snap.ContentJSON, big) {
			t.Errorf("stored body %s lost the integer's precision, want %s", snap.ContentJSON, big)
		}
		// And the canonical form must be stable, so re-canonicalising the stored
		// body reproduces the same hash.
		again := insert("snap-big-2", snap.ContentJSON)
		if again != hash {
			t.Errorf("re-canonicalising the stored body changed the hash: %s != %s", again, hash)
		}
	})

	t.Run("array order is preserved", func(t *testing.T) {
		a := insert("snap-arr-a", `{"steps":["a","b"]}`)
		b := insert("snap-arr-b", `{"steps":["b","a"]}`)
		if a == b {
			t.Error("array order must be significant; reordering produced the same hash")
		}
	})
}

// TestInputSnapshotRejectsSecrets covers the §27.1 rule that a secret value
// never enters the snapshot table, and that the refusal never echoes the value.
func TestInputSnapshotRejectsSecrets(t *testing.T) {
	ctx := context.Background()

	const secretValue = "sk-live-DO-NOT-LEAK-0123456789"

	cases := []struct {
		name string
		body string
		want bool // true: must be rejected
	}{
		{"top-level password", `{"prompt":"go","password":"` + secretValue + `"}`, true},
		{"top-level api_key", `{"api_key":"` + secretValue + `"}`, true},
		{"camelCase apiKey", `{"apiKey":"` + secretValue + `"}`, true},
		{"mixed-case Authorization", `{"Authorization":"Bearer ` + secretValue + `"}`, true},
		{"nested object", `{"config":{"auth":{"token":"` + secretValue + `"}}}`, true},
		{"inside an array", `{"items":[{"name":"a"},{"private_key":"` + secretValue + `"}]}`, true},
		{"secret in a nested array of objects", `{"a":[{"b":[{"cookie":"` + secretValue + `"}]}]}`, true},
		{"passwd", `{"passwd":"` + secretValue + `"}`, true},
		// Allowed: a reference names where the secret lives.
		{"secret_ref is a reference", `{"secret_ref":"vault://projects/p/secrets/db"}`, false},
		{"token_name is a reference", `{"token_name":"github-pat"}`, false},
		{"api_key_id is a reference", `{"api_key_id":"key-123"}`, false},
		{"empty password carries no value", `{"password":""}`, false},
		{"null password carries no value", `{"password":null}`, false},
		{"unrelated key", `{"prompt":"use the token from the vault"}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, _ := newTestStore(t)
			seedTask(t, store, "t-1", "p-1")
			err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
				_, err := InsertInputSnapshot(ctx, tx, "p-1", "snap-1", json.RawMessage(tc.body), time.UnixMilli(1))
				return err
			})
			switch {
			case tc.want && !errors.Is(err, ErrSecretInSnapshot):
				t.Fatalf("body %s was accepted (err=%v), want ErrSecretInSnapshot", tc.body, err)
			case !tc.want && err != nil:
				t.Fatalf("body %s was rejected (%v), want acceptance", tc.body, err)
			}
			if tc.want {
				// The error must name the key path and never the value.
				if strings.Contains(err.Error(), secretValue) {
					t.Fatalf("the rejection leaks the secret value: %v", err)
				}
				t.Logf("rejected: %v", err)
			}
			// Nothing may have been written either way on a rejection.
			if tc.want {
				if _, err := GetInputSnapshot(ctx, store.DB(), "snap-1"); !errors.Is(err, ErrNotFound) {
					t.Fatalf("a rejected snapshot is still stored: %v", err)
				}
			}
		})
	}
}

// TestUpdateRunStatusCAS covers the CAS contract from the acceptance list:
// success bumps the revision, a stale expectation reports both sides, an unknown
// id is not found, and a terminal run cannot move.
func TestUpdateRunStatusCAS(t *testing.T) {
	ctx := context.Background()

	t.Run("success bumps the revision", func(t *testing.T) {
		store, _ := newTestStore(t)
		seedTask(t, store, "t-1", "p-1")
		insertSnapshotAndRun(t, store, "r-1", "t-1", "p-1", "snap-1", json.RawMessage(`{"a":1}`))

		var updated run.Run
		err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
			var err error
			updated, err = UpdateRunStatusCAS(ctx, tx, "r-1", 1, run.RunStatusStarting, time.UnixMilli(1700000001000))
			return err
		})
		if err != nil {
			t.Fatalf("CAS: %v", err)
		}
		if updated.Revision != 2 {
			t.Errorf("revision = %d, want 2", updated.Revision)
		}
		if updated.Status != run.RunStatusStarting {
			t.Errorf("status = %s, want starting", updated.Status)
		}
		if updated.FinishedAt != nil {
			t.Errorf("finished_at = %v, want nil while not terminal", updated.FinishedAt)
		}
		if !updated.UpdatedAt.Equal(time.UnixMilli(1700000001000)) {
			t.Errorf("updated_at = %v, want the CAS instant", updated.UpdatedAt)
		}

		// The change is durable, not just returned.
		stored, err := GetRun(ctx, store.DB(), "r-1")
		if err != nil {
			t.Fatalf("GetRun: %v", err)
		}
		if stored.Revision != 2 || stored.Status != run.RunStatusStarting {
			t.Errorf("stored (status,revision) = (%s,%d), want (starting,2)", stored.Status, stored.Revision)
		}
	})

	t.Run("a stale revision reports both sides", func(t *testing.T) {
		store, _ := newTestStore(t)
		seedTask(t, store, "t-1", "p-1")
		insertSnapshotAndRun(t, store, "r-1", "t-1", "p-1", "snap-1", json.RawMessage(`{"a":1}`))

		// Move it once so revision becomes 2...
		if err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
			_, err := UpdateRunStatusCAS(ctx, tx, "r-1", 1, run.RunStatusStarting, time.UnixMilli(1700000001000))
			return err
		}); err != nil {
			t.Fatalf("first CAS: %v", err)
		}

		// ...then CAS again with the now-stale revision 1.
		err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
			_, err := UpdateRunStatusCAS(ctx, tx, "r-1", 1, run.RunStatusRunning, time.UnixMilli(1700000002000))
			return err
		})
		if !errors.Is(err, ErrRevisionConflict) {
			t.Fatalf("stale CAS = %v, want ErrRevisionConflict", err)
		}
		var conflict *RevisionConflictError
		if !errors.As(err, &conflict) {
			t.Fatalf("stale CAS error %v is not a *RevisionConflictError", err)
		}
		if conflict.RunID != "r-1" || conflict.Expected != 1 || conflict.Current != 2 {
			t.Errorf("conflict = %+v, want run r-1 expected 1 current 2", conflict)
		}
		if conflict.CurrentStatus != run.RunStatusStarting {
			t.Errorf("conflict.CurrentStatus = %s, want starting", conflict.CurrentStatus)
		}
	})

	t.Run("an unknown run is not found", func(t *testing.T) {
		store, _ := newTestStore(t)
		err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
			_, err := UpdateRunStatusCAS(ctx, tx, "no-such-run", 1, run.RunStatusStarting, time.UnixMilli(1))
			return err
		})
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("CAS on an unknown run = %v, want ErrNotFound", err)
		}
		if errors.Is(err, ErrRevisionConflict) {
			t.Error("an unknown run must not be reported as a revision conflict")
		}
	})

	t.Run("a terminal run cannot move", func(t *testing.T) {
		store, _ := newTestStore(t)
		seedTask(t, store, "t-1", "p-1")
		insertSnapshotAndRun(t, store, "r-1", "t-1", "p-1", "snap-1", json.RawMessage(`{"a":1}`))

		// queued -> completed (terminal), revision 1 -> 2.
		var completed run.Run
		if err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
			var err error
			completed, err = UpdateRunStatusCAS(ctx, tx, "r-1", 1, run.RunStatusCompleted, time.UnixMilli(1700000001000))
			return err
		}); err != nil {
			t.Fatalf("terminal CAS: %v", err)
		}
		if completed.FinishedAt == nil {
			t.Fatal("finished_at is nil after a terminal transition")
		}
		if !completed.FinishedAt.Equal(time.UnixMilli(1700000001000)) {
			t.Errorf("finished_at = %v, want the CAS instant", completed.FinishedAt)
		}

		// completed -> running must be refused by the schema trigger.
		err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
			_, err := UpdateRunStatusCAS(ctx, tx, "r-1", 2, run.RunStatusRunning, time.UnixMilli(1700000002000))
			return err
		})
		if !errors.Is(err, ErrTerminalImmutable) {
			t.Fatalf("CAS out of a terminal state = %v, want ErrTerminalImmutable", err)
		}
		// The row is untouched: not even the revision moved.
		stored, err := GetRun(ctx, store.DB(), "r-1")
		if err != nil {
			t.Fatalf("GetRun: %v", err)
		}
		if stored.Status != run.RunStatusCompleted || stored.Revision != 2 {
			t.Errorf("after a refused CAS (status,revision) = (%s,%d), want (completed,2)", stored.Status, stored.Revision)
		}
	})

	t.Run("task CAS follows the same rules", func(t *testing.T) {
		store, _ := newTestStore(t)
		seedTask(t, store, "t-1", "p-1")

		var updated run.Task
		if err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
			var err error
			updated, err = UpdateTaskStatusCAS(ctx, tx, "t-1", 1, run.TaskStatusQueued, time.UnixMilli(1700000001000))
			return err
		}); err != nil {
			t.Fatalf("task CAS: %v", err)
		}
		if updated.Revision != 2 || updated.Status != run.TaskStatusQueued {
			t.Errorf("task after CAS = (%s,%d), want (queued,2)", updated.Status, updated.Revision)
		}

		// Stale expectation.
		err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
			_, err := UpdateTaskStatusCAS(ctx, tx, "t-1", 1, run.TaskStatusRunning, time.UnixMilli(1700000002000))
			return err
		})
		var conflict *TaskRevisionConflictError
		if !errors.As(err, &conflict) {
			t.Fatalf("stale task CAS = %v, want *TaskRevisionConflictError", err)
		}
		if conflict.TaskID != "t-1" || conflict.Expected != 1 || conflict.Current != 2 {
			t.Errorf("conflict = %+v, want task t-1 expected 1 current 2", conflict)
		}
		if conflict.CurrentStatus != run.TaskStatusQueued {
			t.Errorf("conflict.CurrentStatus = %s, want queued", conflict.CurrentStatus)
		}

		// Unknown task.
		err = store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
			_, err := UpdateTaskStatusCAS(ctx, tx, "no-such-task", 1, run.TaskStatusRunning, time.UnixMilli(1))
			return err
		})
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("task CAS on an unknown task = %v, want ErrNotFound", err)
		}

		// completed is terminal for a task too.
		if err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
			_, err := UpdateTaskStatusCAS(ctx, tx, "t-1", 2, run.TaskStatusCompleted, time.UnixMilli(1700000003000))
			return err
		}); err != nil {
			t.Fatalf("task CAS to completed: %v", err)
		}
		err = store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
			_, err := UpdateTaskStatusCAS(ctx, tx, "t-1", 3, run.TaskStatusRunning, time.UnixMilli(1700000004000))
			return err
		})
		if !errors.Is(err, ErrTerminalImmutable) {
			t.Fatalf("task CAS out of completed = %v, want ErrTerminalImmutable", err)
		}
	})
}

// TestConcurrentRunCASSingleWinner is the concurrency assertion: eight writers
// race to CAS the same run from revision 1, and exactly one may win. It also
// proves the immediate transaction mode is in force — a deferred
// read-then-write here would produce SQLITE_BUSY_SNAPSHOT failures, and the
// test asserts that no BUSY-family error appears at all.
func TestConcurrentRunCASSingleWinner(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	seedTask(t, store, "t-1", "p-1")
	insertSnapshotAndRun(t, store, "r-1", "t-1", "p-1", "snap-1", json.RawMessage(`{"a":1}`))

	const writers = 8
	type result struct {
		index  int
		status run.RunStatus
		err    error
	}
	start := make(chan struct{})
	results := make(chan result, writers)

	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			target := run.RunStatusStarting
			if i%2 == 1 {
				target = run.RunStatusRunning
			}
			<-start // release all writers at once to maximise the race
			var err error
			werr := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
				// Read first, then write: this is what every runstore write
				// path does, and it is the sequence a deferred transaction
				// cannot upgrade safely.
				if _, err := GetRun(ctx, tx, "r-1"); err != nil {
					return err
				}
				// Hold the read-before-write gap open for a moment so the race
				// is real rather than theoretical. Under the immediate mode
				// this package pins, the write lock is already held here, so
				// the writers simply queue and the gap costs only wall time.
				// Under deferred mode every writer would be sitting on the same
				// stale snapshot and the upgrades below would fail.
				time.Sleep(2 * time.Millisecond)
				_, err = UpdateRunStatusCAS(ctx, tx, "r-1", 1, target, time.UnixMilli(1700000001000))
				return err
			})
			if werr != nil {
				err = werr
			}
			results <- result{index: i, status: target, err: err}
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)

	var (
		winners   int
		conflicts int
		other     []error
	)
	for res := range results {
		switch {
		case res.err == nil:
			winners++
		case errors.Is(res.err, ErrRevisionConflict):
			conflicts++
		default:
			other = append(other, fmt.Errorf("writer %d: %w", res.index, res.err))
		}
		// A BUSY-family error here would mean the snapshot-upgrade failure this
		// card exists to prevent; it must never appear.
		if code := sqliteExtended(res.err); code == 5 || code == 517 || code == 6 {
			t.Errorf("writer %d failed with a SQLITE_BUSY-family error (code %d): %v", res.index, code, res.err)
		}
	}

	if winners != 1 {
		t.Errorf("winners = %d, want exactly 1", winners)
	}
	if conflicts != writers-1 {
		t.Errorf("revision conflicts = %d, want %d", conflicts, writers-1)
	}
	for _, err := range other {
		t.Errorf("unexpected error class: %v", err)
	}

	// The row moved exactly once: one CAS means one revision bump.
	stored, err := GetRun(ctx, store.DB(), "r-1")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if stored.Revision != 2 {
		t.Errorf("final revision = %d, want 2 (exactly one successful CAS)", stored.Revision)
	}
	if !stored.Status.Valid() {
		t.Errorf("final status %q is not a valid run status", stored.Status)
	}
	t.Logf("%d writers: %d winner, %d revision conflicts, final (status,revision)=(%s,%d)",
		writers, winners, conflicts, stored.Status, stored.Revision)
}

// TestDeferredTxModeStillFails proves the hazard the concurrency test guards
// against is real in this build: over the very same schema, a deferred
// transaction that reads first and writes second is refused with
// SQLITE_BUSY_SNAPSHOT (extended code 517) when another connection commits in
// between — the failure dbx.WithTxLock exists to prevent. If this test ever
// stops reproducing the failure, an immediate-mode regression would silently
// pass the concurrency test above, so this is the control, not a duplicate.
func TestDeferredTxModeStillFails(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "deferred.db")

	// Deliberately the un-pinned mode: raw dbx.Open, no WithTxLock.
	db, err := dbx.Open(path)
	if err != nil {
		t.Fatalf("dbx.Open: %v", err)
	}
	defer db.Close()
	if _, err := Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE ctr (id INTEGER PRIMARY KEY, n INTEGER NOT NULL)`); err != nil {
		t.Fatalf("create ctr: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO ctr (id, n) VALUES (1, 0)`); err != nil {
		t.Fatalf("seed ctr: %v", err)
	}
	if _, err := db.ExecContext(ctx, `PRAGMA busy_timeout = 5000`); err != nil {
		t.Fatalf("busy_timeout: %v", err)
	}

	// A: a deferred transaction that reads, pauses, then writes.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin deferred tx: %v", err)
	}
	defer tx.Rollback()
	var n int64
	if err := tx.QueryRowContext(ctx, `SELECT n FROM ctr WHERE id = 1`).Scan(&n); err != nil {
		t.Fatalf("A read: %v", err)
	}

	// B: commits in the gap, while A still holds its read snapshot.
	if _, err := db.ExecContext(ctx, `UPDATE ctr SET n = n + 1 WHERE id = 1`); err != nil {
		t.Fatalf("B write: %v", err)
	}

	// A's write now has to upgrade a stale snapshot.
	_, werr := tx.ExecContext(ctx, `UPDATE ctr SET n = n + 1 WHERE id = 1`)
	if werr == nil {
		t.Skip("this build of SQLite did not refuse the snapshot upgrade; the concurrency test cannot be validated here")
	}
	if code := sqliteExtended(werr); code != 517 {
		t.Fatalf("deferred upgrade failed with extended code %d (%v), want 517 SQLITE_BUSY_SNAPSHOT", code, werr)
	}
	if !strings.Contains(werr.Error(), "database is locked") {
		t.Errorf("error %q does not look like a busy failure", werr)
	}
	// The busy handler was given 5s and was never consulted: the refusal is
	// immediate, which is exactly why busy_timeout cannot rescue this.
	t.Logf("deferred read-then-write refused as expected: %v (extended code %d)", werr, sqliteExtended(werr))
}

// TestRunHistoryCannotBeDeleted pins the §26.27 registration that the runtime
// history has no hard-delete path: retention and cleanup belong to T12.02 and
// must go through an explicit migration. The store exposes no delete function,
// so this drives the statements directly and asserts the schema refuses them.
func TestRunHistoryCannotBeDeleted(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	seedTask(t, store, "t-1", "p-1")
	insertSnapshotAndRun(t, store, "r-1", "t-1", "p-1", "snap-1", json.RawMessage(`{"a":1}`))

	// An attempt, so the attempts guard has something to refuse.
	if err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		return InsertAttempt(ctx, tx, &run.Attempt{
			ID: "a-1", RunID: "r-1", AttemptNo: 1, Backend: "fake",
			Status: run.AttemptStatusRunning, CreatedAt: time.UnixMilli(1700000000004),
		})
	}); err != nil {
		t.Fatalf("InsertAttempt: %v", err)
	}

	cases := []struct {
		name  string
		query string
	}{
		{"run", `DELETE FROM runs WHERE id = 'r-1'`},
		{"attempt", `DELETE FROM attempts WHERE id = 'a-1'`},
		{"input snapshot", `DELETE FROM input_snapshots WHERE id = 'snap-1'`},
		{"all runs", `DELETE FROM runs`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := store.DB().ExecContext(ctx, tc.query)
			if err == nil {
				t.Fatalf("%s was deleted, want the history guard to refuse", tc.query)
			}
			if !strings.Contains(err.Error(), "run_history_delete_forbidden") {
				t.Errorf("error %q does not name run_history_delete_forbidden", err)
			}
			if got := mapConstraintError("test", err); !errors.Is(got, ErrHistoryDeleteForbidden) {
				t.Errorf("mapped error = %v, want ErrHistoryDeleteForbidden", got)
			}
		})
	}

	// The rows are all still there.
	if _, err := GetRun(ctx, store.DB(), "r-1"); err != nil {
		t.Errorf("run r-1 is gone: %v", err)
	}
	if _, err := GetAttempt(ctx, store.DB(), "a-1"); err != nil {
		t.Errorf("attempt a-1 is gone: %v", err)
	}
	if _, err := GetInputSnapshot(ctx, store.DB(), "snap-1"); err != nil {
		t.Errorf("snapshot snap-1 is gone: %v", err)
	}
}

// TestStoreReopenPreservesRows is the recovery assertion: everything written
// through the store must come back field by field after the handle is closed
// and the file is reopened, with times intact to the millisecond.
func TestStoreReopenPreservesRows(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "codeflow.db")

	store, _, err := OpenStore(ctx, path)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}

	// Seed with every optional column populated, so a dropped NULL or a lost
	// pointer shows up as a mismatch rather than as two nil values comparing
	// equal.
	commandID := "cmd-1"
	baseCommit := "abc123"
	flowID := "flow-1"
	stageID := "stage-1"
	leaseOwner := "worker-1"
	leaseUntil := time.UnixMilli(1700000009000)
	backendVersion := "1.2.3"
	ownerInstance := "instance-1"
	pid := int64(4242)
	processStartID := "boot-1"
	startedAt := time.UnixMilli(1700000000005)
	tokens := int64(50000)
	wall := int64(1800)
	costMinor := int64(250)

	task := run.Task{
		ID: "t-1", ProjectID: "p-1", FlowID: &flowID, StageID: &stageID,
		Title: "add tests", Kind: run.TaskKindCode, Status: run.TaskStatusReady, Priority: 7,
		InputJSON: `{"prompt":"add tests"}`, LeaseOwner: &leaseOwner, LeaseUntil: &leaseUntil,
		LeaseEpoch: 2, CreatedAt: time.UnixMilli(1700000000000), UpdatedAt: time.UnixMilli(1700000000001),
	}
	r := run.Run{
		ID: "r-1", TaskID: "t-1", ProjectID: "p-1", CommandID: &commandID,
		BindingID: "binding-1", BindingRevision: 7,
		BaseManifestHash: "sha256:manifest", BaseCommit: &baseCommit, AgentRevisionID: "agent-rev-1",
		InputSnapshotID: "snap-1",
		Budget:          run.Budget{WallTimeSeconds: &wall, Tokens: &tokens, CostLimitMinor: &costMinor, Currency: "USD"},
		Status:          run.RunStatusQueued,
		CreatedAt:       time.UnixMilli(1700000000002), UpdatedAt: time.UnixMilli(1700000000003),
	}
	attempt := run.Attempt{
		ID: "a-1", RunID: "r-1", AttemptNo: 1, Backend: "fake", BackendVersion: &backendVersion,
		Status: run.AttemptStatusRunning, OwnerInstance: &ownerInstance, PID: &pid,
		ProcessStartID: &processStartID, StartedAt: &startedAt,
		CreatedAt: time.UnixMilli(1700000000004),
	}

	content := json.RawMessage(`{"prompt":"add tests","nested":{"b":2,"a":1}}`)
	var snapshotHash string
	err = store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		if err := UpsertProjectRef(ctx, tx, run.ProjectRef{
			ProjectID: "p-1", SnapshotHash: "sha256:project", State: run.ProjectRefStateActive,
			CapturedAt: time.UnixMilli(1700000000000), VerifiedAt: time.UnixMilli(1700000000001),
		}); err != nil {
			return err
		}
		if err := PutLegacyRef(ctx, tx, run.LegacyResourceRef{
			ProjectID: "p-1", Kind: run.RefKindFlow, ResourceID: "flow-1",
			SnapshotHash: "sha256:flow", CapturedAt: time.UnixMilli(1700000000000),
		}); err != nil {
			return err
		}
		if err := InsertTask(ctx, tx, &task); err != nil {
			return err
		}
		h, err := InsertInputSnapshot(ctx, tx, "p-1", "snap-1", content, time.UnixMilli(1700000000002))
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

	beforeTask, err := GetTask(ctx, store.DB(), "t-1")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	beforeRun, err := GetRun(ctx, store.DB(), "r-1")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	beforeAttempts, err := ListAttemptsByRun(ctx, store.DB(), "r-1")
	if err != nil {
		t.Fatalf("ListAttemptsByRun: %v", err)
	}
	beforeSnap, err := GetInputSnapshot(ctx, store.DB(), "snap-1")
	if err != nil {
		t.Fatalf("GetInputSnapshot: %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Reopen the same file. The second OpenStore must find the schema current
	// and apply nothing.
	reopened, res, err := OpenStore(ctx, path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	if len(res.Applied) != 0 || res.ToVersion != 2 {
		t.Errorf("reopen migration result = %+v, want nothing applied at version 2", res)
	}

	afterTask, err := GetTask(ctx, reopened.DB(), "t-1")
	if err != nil {
		t.Fatalf("GetTask after reopen: %v", err)
	}
	if !equalTask(beforeTask, afterTask) {
		t.Errorf("task differs after reopen:\n before %+v\n after  %+v", beforeTask, afterTask)
	}
	afterRun, err := GetRun(ctx, reopened.DB(), "r-1")
	if err != nil {
		t.Fatalf("GetRun after reopen: %v", err)
	}
	if !equalRun(beforeRun, afterRun) {
		t.Errorf("run differs after reopen:\n before %+v\n after  %+v", beforeRun, afterRun)
	}
	afterAttempts, err := ListAttemptsByRun(ctx, reopened.DB(), "r-1")
	if err != nil {
		t.Fatalf("ListAttemptsByRun after reopen: %v", err)
	}
	if len(afterAttempts) != len(beforeAttempts) {
		t.Fatalf("attempt count = %d, want %d", len(afterAttempts), len(beforeAttempts))
	}
	for i := range beforeAttempts {
		if !equalAttempt(beforeAttempts[i], afterAttempts[i]) {
			t.Errorf("attempt %d differs after reopen:\n before %+v\n after  %+v", i, beforeAttempts[i], afterAttempts[i])
		}
	}
	afterSnap, err := GetInputSnapshot(ctx, reopened.DB(), "snap-1")
	if err != nil {
		t.Fatalf("GetInputSnapshot after reopen: %v", err)
	}
	if afterSnap != beforeSnap {
		t.Errorf("snapshot differs after reopen:\n before %+v\n after  %+v", beforeSnap, afterSnap)
	}
	if afterSnap.ContentHash != snapshotHash {
		t.Errorf("snapshot hash = %s, want %s", afterSnap.ContentHash, snapshotHash)
	}

	// Times must survive to the millisecond, in UTC.
	for _, tc := range []struct {
		name string
		got  time.Time
		want time.Time
	}{
		{"task.created_at", afterTask.CreatedAt, time.UnixMilli(1700000000000)},
		{"task.updated_at", afterTask.UpdatedAt, time.UnixMilli(1700000000001)},
		{"run.created_at", afterRun.CreatedAt, time.UnixMilli(1700000000002)},
		{"run.updated_at", afterRun.UpdatedAt, time.UnixMilli(1700000000003)},
		{"attempt.created_at", afterAttempts[0].CreatedAt, time.UnixMilli(1700000000004)},
		{"snapshot.created_at", afterSnap.CreatedAt, time.UnixMilli(1700000000002)},
	} {
		if !tc.got.Equal(tc.want) {
			t.Errorf("%s = %v (%d ms), want %v (%d ms)",
				tc.name, tc.got, tc.got.UnixMilli(), tc.want, tc.want.UnixMilli())
		}
		if tc.got.Location() != time.UTC {
			t.Errorf("%s location = %v, want UTC", tc.name, tc.got.Location())
		}
	}
	if afterTask.LeaseUntil == nil || !afterTask.LeaseUntil.Equal(leaseUntil) {
		t.Errorf("task.lease_until = %v, want %v", afterTask.LeaseUntil, leaseUntil)
	}
	if afterAttempts[0].StartedAt == nil || !afterAttempts[0].StartedAt.Equal(startedAt) {
		t.Errorf("attempt.started_at = %v, want %v", afterAttempts[0].StartedAt, startedAt)
	}
	// A NULL must stay NULL, not become the zero time.
	if afterRun.FinishedAt != nil {
		t.Errorf("run.finished_at = %v, want nil (the run is not terminal)", afterRun.FinishedAt)
	}
	if afterRun.BaseCommit == nil || *afterRun.BaseCommit != baseCommit {
		t.Errorf("run.base_commit = %v, want %q", afterRun.BaseCommit, baseCommit)
	}
	if afterTask.FlowID == nil || *afterTask.FlowID != flowID {
		t.Errorf("task.flow_id = %v, want %q", afterTask.FlowID, flowID)
	}

}

// The three comparison helpers below exist so a reopen mismatch names the field
// that changed instead of only "structs differ". They compare every field,
// including the pointers, because a lost NULL is exactly the failure this test
// is looking for.
func equalTask(a, b run.Task) bool {
	return a.ID == b.ID && a.ProjectID == b.ProjectID &&
		equalStringPtr(a.FlowID, b.FlowID) && equalStringPtr(a.StageID, b.StageID) &&
		a.Title == b.Title && a.Kind == b.Kind && a.Status == b.Status &&
		a.Priority == b.Priority && a.InputJSON == b.InputJSON && a.InputHash == b.InputHash &&
		equalStringPtr(a.LeaseOwner, b.LeaseOwner) && equalTimePtr(a.LeaseUntil, b.LeaseUntil) &&
		a.LeaseEpoch == b.LeaseEpoch && a.Revision == b.Revision &&
		a.CreatedAt.Equal(b.CreatedAt) && a.UpdatedAt.Equal(b.UpdatedAt)
}

func equalRun(a, b run.Run) bool {
	return a.ID == b.ID && a.TaskID == b.TaskID && a.ProjectID == b.ProjectID &&
		equalStringPtr(a.CommandID, b.CommandID) && a.BindingID == b.BindingID &&
		a.BindingRevision == b.BindingRevision && a.BaseManifestHash == b.BaseManifestHash &&
		equalStringPtr(a.BaseCommit, b.BaseCommit) && a.AgentRevisionID == b.AgentRevisionID &&
		a.InputSnapshotID == b.InputSnapshotID && a.InputSnapshotHash == b.InputSnapshotHash &&
		// Budget is compared field by field: the decoded struct holds freshly
		// allocated pointers, so a struct == would fail on pointer identity
		// even when every limit and the currency match.
		equalBudget(a.Budget, b.Budget) && a.Status == b.Status && a.Revision == b.Revision &&
		equalStringPtr(a.RetryOfRunID, b.RetryOfRunID) &&
		a.CreatedAt.Equal(b.CreatedAt) && a.UpdatedAt.Equal(b.UpdatedAt) &&
		equalTimePtr(a.FinishedAt, b.FinishedAt)
}

func equalAttempt(a, b run.Attempt) bool {
	return a.ID == b.ID && a.RunID == b.RunID && a.AttemptNo == b.AttemptNo &&
		a.Backend == b.Backend && equalStringPtr(a.BackendVersion, b.BackendVersion) &&
		a.Status == b.Status && equalStringPtr(a.OwnerInstance, b.OwnerInstance) &&
		equalInt64Ptr(a.PID, b.PID) && equalStringPtr(a.ProcessStartID, b.ProcessStartID) &&
		equalTimePtr(a.StartedAt, b.StartedAt) && equalTimePtr(a.FinishedAt, b.FinishedAt) &&
		equalInt64Ptr(a.ExitCode, b.ExitCode) && equalStringPtr(a.ExitReason, b.ExitReason) &&
		a.CreatedAt.Equal(b.CreatedAt)
}

func equalBudget(a, b run.Budget) bool {
	return equalInt64Ptr(a.WallTimeSeconds, b.WallTimeSeconds) &&
		equalInt64Ptr(a.Tokens, b.Tokens) &&
		equalInt64Ptr(a.CostLimitMinor, b.CostLimitMinor) &&
		a.Currency == b.Currency
}

func equalStringPtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func equalInt64Ptr(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func equalTimePtr(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

// TestStoreDBAndNewStore covers the small surface: NewStore over a handle the
// caller opened, DB() returning that handle, and Close being safe on a nil
// store.
func TestStoreDBAndNewStore(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "wrapped.db")

	db, err := dbx.Open(path, dbx.WithTxLock("immediate"))
	if err != nil {
		t.Fatalf("dbx.Open: %v", err)
	}
	defer db.Close()
	if _, err := Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	store := NewStore(db)
	if store.DB() != db {
		t.Error("DB() did not return the wrapped handle")
	}
	// The wrapped store must work: seed and read back through it.
	seedTask(t, store, "t-1", "p-1")
	if _, err := GetTask(ctx, store.DB(), "t-1"); err != nil {
		t.Fatalf("GetTask through a wrapped store: %v", err)
	}

	// Close is a no-op on a nil Store so a deferred Close cannot panic before
	// the store was built.
	var nilStore *Store
	if err := nilStore.Close(); err != nil {
		t.Errorf("nil Store Close = %v, want nil", err)
	}
}
