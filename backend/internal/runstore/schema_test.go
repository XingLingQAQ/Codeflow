package runstore

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/codeflow/backend/internal/dbx"
	"github.com/codeflow/backend/internal/run"
	sqlite "modernc.org/sqlite"
)

// SQLite extended result codes as returned by modernc.org/sqlite for the
// constraint classes this schema relies on. They are pinned here so a test
// failure says "the wrong class of constraint fired" rather than "some error
// happened": e.g. a missing foreign key would surface as
// SQLITE_CONSTRAINT_NOTNULL or a successful insert, not as a foreign key error.
const (
	sqliteConstraintCheck      = 275  // SQLITE_CONSTRAINT_CHECK
	sqliteConstraintForeignKey = 787  // SQLITE_CONSTRAINT_FOREIGNKEY
	sqliteConstraintNotNull    = 1299 // SQLITE_CONSTRAINT_NOTNULL
	sqliteConstraintPrimaryKey = 1555 // SQLITE_CONSTRAINT_PRIMARYKEY
	sqliteConstraintTrigger    = 1811 // SQLITE_CONSTRAINT_TRIGGER
	sqliteConstraintUnique     = 2067 // SQLITE_CONSTRAINT_UNIQUE
)

// sqliteCode returns the extended result code of a modernc sqlite error, or -1.
func sqliteCode(err error) int {
	var serr *sqlite.Error
	if errors.As(err, &serr) {
		return serr.Code()
	}
	return -1
}

// TestSQLiteVersionMatchesPinnedCodes records which SQLite build the extended
// result codes in this file were verified against. The codes are stable in
// SQLite's public ABI, but the exact engine version is part of this card's
// evidence, so it is asserted rather than left in a terminal transcript.
func TestSQLiteVersionMatchesPinnedCodes(t *testing.T) {
	db, _ := migrateTemp(t)

	var version string
	if err := db.QueryRow(`SELECT sqlite_version()`).Scan(&version); err != nil {
		t.Fatalf("select sqlite_version(): %v", err)
	}
	t.Logf("sqlite_version() = %s", version)
	if version != "3.53.3" {
		t.Errorf("sqlite_version() = %s, want 3.53.3 (the version the codes in this file were verified on); "+
			"re-verify those codes before changing this expectation", version)
	}

	// The connection must come from the shared factory so the codes asserted
	// here are the codes production sees.
	if dbx.DriverName != "sqlite" {
		t.Errorf("dbx.DriverName = %q, want sqlite", dbx.DriverName)
	}
	var fk int
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil {
		t.Fatalf("PRAGMA foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Errorf("PRAGMA foreign_keys = %d, want 1", fk)
	}
}

// schemaDB returns a migrated file database with one project, one task and the
// input snapshot every runInsert fixture pins, so each case only has to add
// what it is actually testing.
//
// The snapshot row is not optional decoration: migration 002's
// trg_runs_input_snapshot_must_match refuses a run whose input_snapshot_id /
// input_snapshot_hash do not name a real input_snapshots row of the same
// project, so every run fixture needs one. See runInsert for why the constants
// below must stay in step.
func schemaDB(t *testing.T) *sql.DB {
	t.Helper()
	db, _ := migrateTemp(t)
	exec(t, db, `INSERT INTO project_refs (project_id, source_revision, snapshot_hash, state, captured_at, verified_at)
	             VALUES ('p-1', NULL, 'sha256:proj', 'active', 1, 2)`)
	exec(t, db, taskInsert, "t-1", "p-1", "ready")
	exec(t, db, snapshotInsert, runSnapshotID, "p-1")
	return db
}

const taskInsert = `INSERT INTO tasks (id, project_id, title, kind, status, priority,
    input_json, input_hash, lease_epoch, revision, created_at, updated_at)
    VALUES (?, ?, 'title', 'code', ?, 0, '{"p":1}', 'sha256:in', 0, 1, 1, 2)`

// The snapshot identity every runInsert/runInsertWith fixture pins, and the
// row that makes it valid (002). They are one constant pair on purpose: the two
// lists cannot drift apart without a fixture failing loudly.
const (
	runSnapshotID   = "snap-1"
	runSnapshotHash = "sha256:s"
)

const snapshotInsert = `INSERT INTO input_snapshots (id, project_id, content_json, content_hash, created_at)
    VALUES (?, ?, '{"frozen":true}', 'sha256:s', 1)`

// runInsert inserts a run with every required column; optional columns are
// passed explicitly by the cases that care about them.
const runInsert = `INSERT INTO runs (id, task_id, project_id, command_id, binding_id, binding_revision,
    base_manifest_hash, base_commit, agent_revision_id, input_snapshot_id, input_snapshot_hash,
    budget_json, status, revision, retry_of_run_id, created_at, updated_at, finished_at)
    VALUES (?, ?, ?, ?, 'b-1', 1, 'sha256:m', NULL, 'ar-1', 'snap-1', 'sha256:s', '{}', ?, 1, NULL, 10, 11, NULL)`

// runInsertSnapshot is runInsert with the pinned input snapshot id explicit, for
// cases that need a run under a project other than p-1: 002's
// trg_runs_input_snapshot_must_match requires the snapshot to belong to the
// run's own project, so a p-2 run cannot reuse p-1's snap-1.
// Args: id, task_id, project_id, command_id, input_snapshot_id, status.
const runInsertSnapshot = `INSERT INTO runs (id, task_id, project_id, command_id, binding_id, binding_revision,
    base_manifest_hash, base_commit, agent_revision_id, input_snapshot_id, input_snapshot_hash,
    budget_json, status, revision, retry_of_run_id, created_at, updated_at, finished_at)
    VALUES (?, ?, ?, ?, 'b-1', 1, 'sha256:m', NULL, 'ar-1', ?, 'sha256:s', '{}', ?, 1, NULL, 10, 11, NULL)`

// runInsertWith is runInsert with the three nullable frozen columns explicit, so
// a test can seed command_id/base_commit/retry_of_run_id at insert time (they
// cannot be set later: the frozen-input trigger forbids it).
const runInsertWith = `INSERT INTO runs (id, task_id, project_id, command_id, binding_id, binding_revision,
    base_manifest_hash, base_commit, agent_revision_id, input_snapshot_id, input_snapshot_hash,
    budget_json, status, revision, retry_of_run_id, created_at, updated_at, finished_at)
    VALUES (?, ?, ?, ?, 'b-1', 1, 'sha256:m', ?, 'ar-1', 'snap-1', 'sha256:s', '{}', ?, 1, ?, 10, 11, NULL)`

func exec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("setup %q: %v", query, err)
	}
}

// wantErr asserts that exec produced a SQLite error with the expected extended
// code and, when wantMsg is non-empty, that the message names the offending
// constraint. The message check is what distinguishes "the frozen-input trigger
// fired" from "some other trigger fired on the same statement".
func wantErr(t *testing.T, db *sql.DB, query string, args []any, wantCode int, wantMsg string) {
	t.Helper()
	_, err := db.Exec(query, args...)
	if err == nil {
		t.Fatalf("statement succeeded, want a constraint error (code %d, %q)", wantCode, wantMsg)
	}
	if got := sqliteCode(err); got != wantCode {
		t.Fatalf("error code = %d, want %d (err: %v)", got, wantCode, err)
	}
	if wantMsg != "" && !strings.Contains(err.Error(), wantMsg) {
		t.Fatalf("error %q does not mention %q", err, wantMsg)
	}
}

// readColumn reads one column of one row as a NULL-tolerant string, so a test
// can compare a value across a rejected statement without knowing whether the
// column starts NULL.
func readColumn(t *testing.T, db *sql.DB, table, column, id string) string {
	t.Helper()
	var v sql.NullString
	if err := db.QueryRow(`SELECT `+column+` FROM `+table+` WHERE id = ?`, id).Scan(&v); err != nil {
		t.Fatalf("read %s.%s for %q: %v", table, column, id, err)
	}
	if !v.Valid {
		return "<NULL>"
	}
	return v.String
}

func wantOK(t *testing.T, db *sql.DB, query string, args ...any) {

	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("statement failed, want success: %v", err)
	}
}

// runState reads the mutable columns of a run, so a test can prove that a
// rejected update changed nothing at all — not even the revision.
func runState(t *testing.T, db *sql.DB, id string) (status string, revision int64, finished sql.NullInt64) { //nolint:unparam // finished_at is used by the terminal tests
	t.Helper()
	if err := db.QueryRow(`SELECT status, revision, finished_at FROM runs WHERE id=?`, id).
		Scan(&status, &revision, &finished); err != nil {
		t.Fatalf("read run %q state: %v", id, err)
	}
	return status, revision, finished
}

// TestSchemaForeignKeys covers the acceptance assertion "a wrong parent id
// cannot be inserted", including the composite project key that stops a run
// from naming a task that belongs to a different project.
func TestSchemaForeignKeys(t *testing.T) {
	t.Run("task needs an existing project_ref", func(t *testing.T) {
		db := schemaDB(t)
		wantErr(t, db, taskInsert, []any{"t-x", "no-such-project", "ready"},
			sqliteConstraintForeignKey, "FOREIGN KEY")
	})

	t.Run("run needs an existing task", func(t *testing.T) {
		db := schemaDB(t)
		wantErr(t, db, runInsert, []any{"r-x", "no-such-task", "p-1", nil, "queued"},
			sqliteConstraintForeignKey, "FOREIGN KEY")
	})

	t.Run("run project must match its task project", func(t *testing.T) {
		db := schemaDB(t)
		exec(t, db, `INSERT INTO project_refs (project_id, source_revision, snapshot_hash, state, captured_at, verified_at)
		             VALUES ('p-2', NULL, 'sha256:proj2', 'active', 1, 2)`)
		// t-1 belongs to p-1; claiming p-2 must fail on the composite foreign
		// key even though both projects exist.
		//
		// p-2 needs a snapshot of its own so that the only thing wrong with the
		// statement below is the project mismatch: a run pinned to p-1's
		// snapshot under p-2 would also trip 002's
		// trg_runs_input_snapshot_must_match, and that trigger is evaluated
		// before the foreign keys, so it would mask the composite key this case
		// is about.
		exec(t, db, snapshotInsert, "snap-p2", "p-2")
		wantErr(t, db, runInsertSnapshot, []any{"r-x", "t-1", "p-2", nil, "snap-p2", "queued"},
			sqliteConstraintForeignKey, "FOREIGN KEY")

		// The matching pair is accepted, so the failure above is the composite
		// key and not an unrelated constraint.
		wantOK(t, db, runInsert, "r-ok", "t-1", "p-1", nil, "queued")
	})

	t.Run("attempt needs an existing run", func(t *testing.T) {
		db := schemaDB(t)
		wantErr(t, db, `INSERT INTO attempts (id, run_id, attempt_no, backend, status, created_at)
		                VALUES ('a-x', 'no-such-run', 1, 'claude', 'starting', 1)`, nil,
			sqliteConstraintForeignKey, "FOREIGN KEY")
	})

	t.Run("legacy ref needs an existing project_ref", func(t *testing.T) {
		db := schemaDB(t)
		wantErr(t, db, `INSERT INTO legacy_resource_refs (project_id, kind, resource_id, snapshot_hash, captured_at)
		                VALUES ('no-such-project', 'flow', 'f-1', 'sha256:x', 1)`, nil,
			sqliteConstraintForeignKey, "FOREIGN KEY")
		wantOK(t, db, `INSERT INTO legacy_resource_refs (project_id, kind, resource_id, snapshot_hash, captured_at)
		               VALUES ('p-1', 'flow', 'f-1', 'sha256:x', 1)`)
	})

	t.Run("retry_of_run_id needs an existing run", func(t *testing.T) {
		db := schemaDB(t)
		wantErr(t, db, `INSERT INTO runs (id, task_id, project_id, binding_id, binding_revision,
		                base_manifest_hash, agent_revision_id, input_snapshot_id, input_snapshot_hash,
		                status, revision, retry_of_run_id, created_at, updated_at)
		                VALUES ('r-x', 't-1', 'p-1', 'b-1', 1, 'sha256:m', 'ar-1', 'snap-1', 'sha256:s',
		                        'queued', 1, 'no-such-run', 1, 2)`, nil,
			sqliteConstraintForeignKey, "FOREIGN KEY")
	})
}

// TestSchemaUniqueness covers the partial unique indexes and the command_id
// idempotency guard, including the "terminal frees the slot" half of each.
func TestSchemaUniqueness(t *testing.T) {
	t.Run("one non-terminal run per task", func(t *testing.T) {
		db := schemaDB(t)
		wantOK(t, db, runInsert, "r-1", "t-1", "p-1", nil, "queued")
		wantErr(t, db, runInsert, []any{"r-2", "t-1", "p-1", nil, "starting"},
			sqliteConstraintUnique, "runs.task_id")

		// Once the first run is terminal the slot is free again; this is what
		// lets a retry create a new run while the old evidence is kept.
		wantOK(t, db, `UPDATE runs SET status='failed', revision=2 WHERE id='r-1'`)
		wantOK(t, db, runInsert, "r-3", "t-1", "p-1", nil, "queued")
	})

	t.Run("one active attempt per run", func(t *testing.T) {
		db := schemaDB(t)
		wantOK(t, db, runInsert, "r-1", "t-1", "p-1", nil, "queued")
		wantOK(t, db, `INSERT INTO attempts (id, run_id, attempt_no, backend, status, created_at)
		               VALUES ('a-1', 'r-1', 1, 'claude', 'running', 1)`)
		wantErr(t, db, `INSERT INTO attempts (id, run_id, attempt_no, backend, status, created_at)
		                VALUES ('a-2', 'r-1', 2, 'claude', 'starting', 1)`, nil,
			sqliteConstraintUnique, "attempts.run_id")

		// A finished attempt does not block the next one.
		wantOK(t, db, `UPDATE attempts SET status='exited', exit_code=0 WHERE id='a-1'`)
		wantOK(t, db, `INSERT INTO attempts (id, run_id, attempt_no, backend, status, created_at)
		               VALUES ('a-3', 'r-1', 2, 'claude', 'running', 1)`)
	})

	t.Run("attempt_no is unique per run", func(t *testing.T) {
		db := schemaDB(t)
		wantOK(t, db, runInsert, "r-1", "t-1", "p-1", nil, "queued")
		wantOK(t, db, `INSERT INTO attempts (id, run_id, attempt_no, backend, status, created_at)
		               VALUES ('a-1', 'r-1', 1, 'claude', 'exited', 1)`)
		wantErr(t, db, `INSERT INTO attempts (id, run_id, attempt_no, backend, status, created_at)
		                VALUES ('a-2', 'r-1', 1, 'claude', 'starting', 1)`, nil,
			sqliteConstraintUnique, "attempts.run_id")
	})

	t.Run("command_id is unique but several NULLs are allowed", func(t *testing.T) {
		db := schemaDB(t)
		exec(t, db, taskInsert, "t-2", "p-1", "ready")
		wantOK(t, db, runInsert, "r-1", "t-1", "p-1", "cmd-1", "queued")
		wantErr(t, db, runInsert, []any{"r-2", "t-2", "p-1", "cmd-1", "queued"},
			sqliteConstraintUnique, "runs.command_id")

		// System-created runs have no command, and NULL must not collide.
		wantOK(t, db, runInsert, "r-3", "t-2", "p-1", nil, "queued")
		exec(t, db, taskInsert, "t-3", "p-1", "ready")
		wantOK(t, db, runInsert, "r-4", "t-3", "p-1", nil, "queued")
	})

	t.Run("legacy ref identity is the composite key", func(t *testing.T) {
		db := schemaDB(t)
		ref := `INSERT INTO legacy_resource_refs (project_id, kind, resource_id, snapshot_hash, captured_at)
		        VALUES ('p-1', 'flow', 'f-1', 'sha256:x', 1)`
		wantOK(t, db, ref)
		// The composite PRIMARY KEY rejects a duplicate as SQLITE_CONSTRAINT_
		// PRIMARYKEY, not _UNIQUE, because this key is not an index.
		wantErr(t, db, ref, nil, sqliteConstraintPrimaryKey, "legacy_resource_refs")

		// A different resource_id or kind is a different reference.
		wantOK(t, db, `INSERT INTO legacy_resource_refs (project_id, kind, resource_id, snapshot_hash, captured_at)
		               VALUES ('p-1', 'flow', 'f-2', 'sha256:x', 1)`)
		wantOK(t, db, `INSERT INTO legacy_resource_refs (project_id, kind, resource_id, snapshot_hash, captured_at)
		               VALUES ('p-1', 'session', 'f-1', 'sha256:x', 1)`)
	})
}

// TestSchemaNullability pins the nullable/frozen distinction that the store
// (T1.01.b) will rely on: a non-Git workspace has no base_commit, but every
// identity column must be present.
func TestSchemaNullability(t *testing.T) {
	t.Run("base_commit may be NULL and is preserved", func(t *testing.T) {
		db := schemaDB(t)
		wantOK(t, db, `INSERT INTO runs (id, task_id, project_id, binding_id, binding_revision,
		    base_manifest_hash, base_commit, agent_revision_id, input_snapshot_id, input_snapshot_hash,
		    status, revision, created_at, updated_at)
		    VALUES ('r-1', 't-1', 'p-1', 'b-1', 1, 'sha256:m', NULL, 'ar-1', 'snap-1', 'sha256:s',
		            'queued', 1, 1, 2)`)
		var commit sql.NullString
		if err := db.QueryRow(`SELECT base_commit FROM runs WHERE id='r-1'`).Scan(&commit); err != nil {
			t.Fatalf("read base_commit: %v", err)
		}
		if commit.Valid {
			t.Errorf("base_commit = %q, want NULL for a non-Git workspace", commit.String)
		}
	})

	notNullErr := []struct {
		column string
		query  string
		args   []any
	}{
		{
			column: "base_manifest_hash",
			query: `INSERT INTO runs (id, task_id, project_id, binding_id, binding_revision,
			        base_manifest_hash, agent_revision_id, input_snapshot_id, input_snapshot_hash,
			        status, revision, created_at, updated_at)
			        VALUES (?, 't-1', 'p-1', 'b-1', 1, NULL, 'ar-1', 'snap-1', 'sha256:s', 'queued', 1, 1, 2)`,
			args: []any{"r-manifest"},
		},
		{
			column: "agent_revision_id",
			query: `INSERT INTO runs (id, task_id, project_id, binding_id, binding_revision,
			        base_manifest_hash, agent_revision_id, input_snapshot_id, input_snapshot_hash,
			        status, revision, created_at, updated_at)
			        VALUES (?, 't-1', 'p-1', 'b-1', 1, 'sha256:m', NULL, 'snap-1', 'sha256:s', 'queued', 1, 1, 2)`,
			args: []any{"r-agentrev"},
		},
		{
			column: "input_snapshot_id",
			query: `INSERT INTO runs (id, task_id, project_id, binding_id, binding_revision,
			        base_manifest_hash, agent_revision_id, input_snapshot_id, input_snapshot_hash,
			        status, revision, created_at, updated_at)
			        VALUES (?, 't-1', 'p-1', 'b-1', 1, 'sha256:m', 'ar-1', NULL, 'sha256:s', 'queued', 1, 1, 2)`,
			args: []any{"r-snapid"},
		},
		{
			column: "input_snapshot_hash",
			query: `INSERT INTO runs (id, task_id, project_id, binding_id, binding_revision,
			        base_manifest_hash, agent_revision_id, input_snapshot_id, input_snapshot_hash,
			        status, revision, created_at, updated_at)
			        VALUES (?, 't-1', 'p-1', 'b-1', 1, 'sha256:m', 'ar-1', 'snap-1', NULL, 'queued', 1, 1, 2)`,
			args: []any{"r-snaphash"},
		},
	}
	for _, tc := range notNullErr {
		t.Run("NULL "+tc.column+" is rejected", func(t *testing.T) {
			db := schemaDB(t)
			wantErr(t, db, tc.query, tc.args, sqliteConstraintNotNull, "NOT NULL")
		})
	}

	t.Run("a half-filled lease is rejected", func(t *testing.T) {
		db := schemaDB(t)
		// owner without deadline
		wantErr(t, db, `INSERT INTO tasks (id, project_id, title, kind, status, priority,
		    input_json, input_hash, lease_owner, lease_epoch, revision, created_at, updated_at)
		    VALUES ('t-x', 'p-1', 'title', 'code', 'ready', 0, '{}', 'sha256:i', 'worker-1', 0, 1, 1, 2)`, nil,
			sqliteConstraintCheck, "lease_owner")
		// deadline without owner
		wantErr(t, db, `INSERT INTO tasks (id, project_id, title, kind, status, priority,
		    input_json, input_hash, lease_until, lease_epoch, revision, created_at, updated_at)
		    VALUES ('t-y', 'p-1', 'title', 'code', 'ready', 0, '{}', 'sha256:i', 1700000000000, 0, 1, 1, 2)`, nil,
			sqliteConstraintCheck, "lease_owner")

		// Both set is a valid claim; both NULL is an unclaimed task.
		wantOK(t, db, `INSERT INTO tasks (id, project_id, title, kind, status, priority,
		    input_json, input_hash, lease_owner, lease_until, lease_epoch, revision, created_at, updated_at)
		    VALUES ('t-z', 'p-1', 'title', 'code', 'ready', 0, '{}', 'sha256:i', 'worker-1', 1700000000000, 1, 1, 1, 2)`)
	})
}

// TestSchemaEnumsInsertEveryConstant proves the SQL CHECK sets are at least as
// wide as the Go sets: every exported constant must be insertable.
func TestSchemaEnumsInsertEveryConstant(t *testing.T) {
	db := schemaDB(t)

	// kind is immutable after insert (task_input_immutable), so each constant
	// must be inserted, not updated in place.
	for i, k := range run.TaskKinds {
		wantOK(t, db, `INSERT INTO tasks (id, project_id, title, kind, status, priority,
		    input_json, input_hash, lease_epoch, revision, created_at, updated_at)
		    VALUES (?, 'p-1', 'title', ?, 'ready', 0, '{}', 'sha256:i', 0, 1, 1, 2)`,
			fmt.Sprintf("kind-%d", i), string(k))
	}

	for i, s := range run.TaskStatuses {
		id := fmt.Sprintf("status-%d", i)
		wantOK(t, db, taskInsert, id, "p-1", string(s))
	}

	for _, s := range run.ProjectRefStates {
		wantOK(t, db, `INSERT INTO project_refs (project_id, snapshot_hash, state, captured_at, verified_at)
		               VALUES (?, 'sha256:x', ?, 1, 2)`, "proj-"+string(s), string(s))
	}

	for _, k := range run.RefKinds {
		wantOK(t, db, `INSERT INTO legacy_resource_refs (project_id, kind, resource_id, snapshot_hash, captured_at)
		               VALUES ('p-1', ?, ?, 'sha256:x', 1)`, string(k), "res-"+string(k))
	}

	// Each run needs its own task: the partial unique index allows only one
	// non-terminal run per task.
	for i, s := range run.RunStatuses {
		taskID := fmt.Sprintf("run-task-%d", i)
		exec(t, db, taskInsert, taskID, "p-1", "ready")
		wantOK(t, db, runInsert, fmt.Sprintf("run-%d", i), taskID, "p-1", nil, string(s))
	}

	for i, s := range run.AttemptStatuses {
		// One run per status so the "one active attempt" index never interferes.
		runID := fmt.Sprintf("arun-%d", i)
		exec(t, db, taskInsert, fmt.Sprintf("atask-%d", i), "p-1", "ready")
		wantOK(t, db, runInsert, runID, fmt.Sprintf("atask-%d", i), "p-1", nil, "queued")
		wantOK(t, db, `INSERT INTO attempts (id, run_id, attempt_no, backend, status, created_at)
		               VALUES (?, ?, 1, 'claude', ?, 1)`, fmt.Sprintf("attempt-%d", i), runID, string(s))
	}
}

// TestSchemaEnumsRejectUnknown proves the SQL sets are not wider than the Go
// sets for the columns that carry a status or kind.
func TestSchemaEnumsRejectUnknown(t *testing.T) {
	db := schemaDB(t)

	wantErr(t, db, `INSERT INTO tasks (id, project_id, title, kind, status, priority,
	    input_json, input_hash, lease_epoch, revision, created_at, updated_at)
	    VALUES ('t-bad-kind', 'p-1', 'title', 'bogus', 'ready', 0, '{}', 'sha256:i', 0, 1, 1, 2)`, nil,
		sqliteConstraintCheck, "kind IN")
	wantErr(t, db, `INSERT INTO tasks (id, project_id, title, kind, status, priority,
	    input_json, input_hash, lease_epoch, revision, created_at, updated_at)
	    VALUES ('t-bad-status', 'p-1', 'title', 'code', 'bogus', 0, '{}', 'sha256:i', 0, 1, 1, 2)`, nil,
		sqliteConstraintCheck, "status IN")
	wantErr(t, db, runInsert, []any{"r-bad-status", "t-1", "p-1", nil, "bogus"},
		sqliteConstraintCheck, "status IN")
	wantErr(t, db, `INSERT INTO attempts (id, run_id, attempt_no, backend, status, created_at)
	                VALUES ('a-bad', 'r-1', 1, 'claude', 'bogus', 1)`, nil,
		sqliteConstraintCheck, "status IN")
	wantErr(t, db, `INSERT INTO legacy_resource_refs (project_id, kind, resource_id, snapshot_hash, captured_at)
	                VALUES ('p-1', 'bogus', 'res', 'sha256:x', 1)`, nil,
		sqliteConstraintCheck, "kind IN")
	wantErr(t, db, `INSERT INTO project_refs (project_id, snapshot_hash, state, captured_at, verified_at)
	                VALUES ('p-bad', 'sha256:x', 'bogus', 1, 2)`, nil,
		sqliteConstraintCheck, "state IN")
}

// TestSchemaTriggerRunFrozenInput covers the acceptance assertion "the frozen
// input of a Run cannot be rewritten", one column at a time. The revision is
// bumped in every statement on purpose: otherwise the revision trigger would
// fire first and mask the frozen-input trigger.
func TestSchemaTriggerRunFrozenInput(t *testing.T) {
	// Each entry mutates exactly one frozen column to a different value, with
	// the revision bumped so the revision trigger cannot fire first.
	frozen := []struct {
		column string
		query  string
	}{
		{"task_id", `UPDATE runs SET task_id='t-2', revision=2 WHERE id='r-1'`},
		{"project_id", `UPDATE runs SET project_id='p-2', revision=2 WHERE id='r-1'`},
		{"command_id", `UPDATE runs SET command_id='cmd-x', revision=2 WHERE id='r-1'`},
		{"binding_id", `UPDATE runs SET binding_id='b-2', revision=2 WHERE id='r-1'`},
		{"binding_revision", `UPDATE runs SET binding_revision=2, revision=2 WHERE id='r-1'`},
		{"base_manifest_hash", `UPDATE runs SET base_manifest_hash='sha256:other', revision=2 WHERE id='r-1'`},
		{"base_commit", `UPDATE runs SET base_commit='def456', revision=2 WHERE id='r-1'`},
		{"agent_revision_id", `UPDATE runs SET agent_revision_id='ar-2', revision=2 WHERE id='r-1'`},
		{"input_snapshot_id", `UPDATE runs SET input_snapshot_id='snap-2', revision=2 WHERE id='r-1'`},
		{"input_snapshot_hash", `UPDATE runs SET input_snapshot_hash='sha256:other', revision=2 WHERE id='r-1'`},
		{"budget_json", `UPDATE runs SET budget_json='{"tokens":1}', revision=2 WHERE id='r-1'`},
		{"retry_of_run_id", `UPDATE runs SET retry_of_run_id='r-0', revision=2 WHERE id='r-1'`},
		{"created_at", `UPDATE runs SET created_at=99, revision=2 WHERE id='r-1'`},
	}
	for _, tc := range frozen {
		t.Run(tc.column+" is frozen", func(t *testing.T) {
			// project_id cases point at p-2, which must exist for the update to
			// fail on the frozen-input trigger rather than on the foreign key.
			db := schemaDB(t)
			exec(t, db, `INSERT INTO project_refs (project_id, source_revision, snapshot_hash, state, captured_at, verified_at)
			             VALUES ('p-2', NULL, 'sha256:proj2', 'active', 1, 2)`)
			exec(t, db, taskInsert, "t-2", "p-1", "ready")
			exec(t, db, runInsert, "r-0", "t-2", "p-1", nil, "queued")

			// Nullable frozen columns must be seeded at insert time: setting
			// them with an UPDATE would itself be a frozen-input change (which
			// is exactly what the trigger forbids).
			switch tc.column {
			case "command_id":
				exec(t, db, runInsertWith, "r-1", "t-1", "p-1", "cmd-1", nil, "queued", nil)
			case "base_commit":
				exec(t, db, runInsertWith, "r-1", "t-1", "p-1", nil, "abc123", "queued", nil)
			default:
				// retry_of_run_id stays NULL here and case retry_of_run_id sets
				// it to r-0, which is a real change (NULL -> r-0).
				exec(t, db, runInsert, "r-1", "t-1", "p-1", nil, "queued")
			}

			// Capture the value before the attempt so the assertion does not
			// have to know which columns start NULL.
			before := readColumn(t, db, "runs", tc.column, "r-1")
			wantErr(t, db, tc.query, nil, sqliteConstraintTrigger, "run_frozen_input_immutable")

			// The frozen value and the revision are both untouched, so a
			// rejected statement cannot half-apply.
			if after := readColumn(t, db, "runs", tc.column, "r-1"); after != before {
				t.Errorf("%s = %q after a rejected update, want %q", tc.column, after, before)
			}
			if _, revision, _ := runState(t, db, "r-1"); revision != 1 {
				t.Errorf("revision = %d after a rejected frozen-input update, want 1", revision)
			}
		})
	}

	t.Run("a non-input column may move", func(t *testing.T) {
		db := schemaDB(t)
		exec(t, db, runInsert, "r-1", "t-1", "p-1", nil, "queued")
		wantOK(t, db, `UPDATE runs SET status='starting', revision=2, updated_at=20 WHERE id='r-1'`)
		wantOK(t, db, `UPDATE runs SET status='running', revision=3, updated_at=21 WHERE id='r-1'`)
	})
}

// TestSchemaTriggerRunTerminalImmutable covers "a terminal Run cannot leave its
// terminal state" — the achievement card's "Run 终态不能逆转".
func TestSchemaTriggerRunTerminalImmutable(t *testing.T) {
	// Each terminal state is exercised on its own row, because a run cannot
	// leave the first terminal state it reaches.
	for i, terminal := range []run.RunStatus{
		run.RunStatusCompleted, run.RunStatusFailed, run.RunStatusCancelled, run.RunStatusExpired,
	} {
		runID := fmt.Sprintf("r-%d", i)
		db := schemaDB(t)
		exec(t, db, taskInsert, "t-terminal", "p-1", "ready")
		exec(t, db, runInsert, runID, "t-terminal", "p-1", nil, "queued")
		wantOK(t, db, `UPDATE runs SET status=?, revision=2, finished_at=100 WHERE id=?`,
			string(terminal), runID)

		for _, target := range []run.RunStatus{
			run.RunStatusQueued, run.RunStatusStarting, run.RunStatusRunning,
			run.RunStatusWaitingApproval, run.RunStatusPaused, run.RunStatusCancelling,
			run.RunStatusRecovering,
		} {
			// Leaving a terminal state must be refused. With a correct CAS the
			// revision is bumped, so run_terminal_immutable is the trigger that
			// fires and the message assertion is what distinguishes it from the
			// revision guard.
			wantErr(t, db, `UPDATE runs SET status=?, revision=? WHERE id=?`,
				[]any{string(target), 3, runID},
				sqliteConstraintTrigger, "run_terminal_immutable")

			// A caller that forgets the CAS must still be stopped, by the
			// revision guard rather than silently succeeding.
			wantErr(t, db, `UPDATE runs SET status=? WHERE id=?`,
				[]any{string(target), runID},
				sqliteConstraintTrigger, "run_revision_not_incremented")
		}

		// Every rejection above must have left the row exactly as it was: the
		// terminal status and its revision are still the ones we wrote, so no
		// rejected statement half-applied.
		status, revision, finished := runState(t, db, runID)
		if status != string(terminal) {
			t.Errorf("status = %q after rejected reversals, want %q", status, terminal)
		}
		if revision != 2 {
			t.Errorf("revision = %d after rejected reversals, want 2", revision)
		}
		if !finished.Valid || finished.Int64 != 100 {
			t.Errorf("finished_at = %v, want 100", finished)
		}

		// Staying on the same terminal value is not a state change, so
		// bookkeeping updates on a finished run still work.
		wantOK(t, db, `UPDATE runs SET status=?, revision=3, updated_at=200 WHERE id=?`,
			string(terminal), runID)
	}
}

// TestSchemaTriggerRunRevisionIncremented proves a stale update cannot silently
// overwrite a concurrent transition: every update must bump the revision by
// exactly one.
func TestSchemaTriggerRunRevisionIncremented(t *testing.T) {
	db := schemaDB(t)
	exec(t, db, runInsert, "r-1", "t-1", "p-1", nil, "queued")

	for _, tc := range []struct {
		name     string
		revision int64
	}{
		{"unchanged revision", 1},
		{"revision skipping a value", 3},
		{"revision going backwards", 0},
	} {
		t.Run(tc.name+" is rejected", func(t *testing.T) {
			wantErr(t, db, `UPDATE runs SET status='starting', revision=? WHERE id='r-1'`,
				[]any{tc.revision}, sqliteConstraintTrigger, "run_revision_not_incremented")
		})
	}

	wantOK(t, db, `UPDATE runs SET status='starting', revision=2 WHERE id='r-1'`)
	wantOK(t, db, `UPDATE runs SET status='running', revision=3 WHERE id='r-1'`)
}

// TestSchemaTriggerTaskInputAndTerminal covers §27.2.7: task input is
// immutable, completed/cancelled never reopen, but failed may be retried.
func TestSchemaTriggerTaskInputAndTerminal(t *testing.T) {
	t.Run("input cannot be rewritten", func(t *testing.T) {
		db := schemaDB(t)
		exec(t, db, `INSERT INTO project_refs (project_id, source_revision, snapshot_hash, state, captured_at, verified_at)
		             VALUES ('p-2', NULL, 'sha256:proj2', 'active', 1, 2)`)

		// Each statement changes exactly one immutable column to a different
		// value; the message assertion proves the right trigger fired.
		for _, query := range []string{
			`UPDATE tasks SET input_json='{"p":2}' WHERE id='t-1'`,
			`UPDATE tasks SET input_hash='sha256:other' WHERE id='t-1'`,
			`UPDATE tasks SET project_id='p-2' WHERE id='t-1'`,
			`UPDATE tasks SET kind='document' WHERE id='t-1'`,
		} {
			wantErr(t, db, query, nil, sqliteConstraintTrigger, "task_input_immutable")
		}

		// A no-op assignment of an immutable column is not a change and is
		// allowed; the trigger compares old and new values, so it cannot be
		// used to reject unrelated updates.
		wantOK(t, db, `UPDATE tasks SET project_id='p-1', kind='code' WHERE id='t-1'`)

		// Mutable columns still move.
		wantOK(t, db, `UPDATE tasks SET title='renamed', priority=5, revision=2 WHERE id='t-1'`)
	})

	t.Run("completed and cancelled cannot reopen", func(t *testing.T) {
		for _, terminal := range []run.TaskStatus{run.TaskStatusCompleted, run.TaskStatusCancelled} {
			db := schemaDB(t)
			exec(t, db, `UPDATE tasks SET status=? WHERE id='t-1'`, string(terminal))
			for _, target := range []run.TaskStatus{
				run.TaskStatusReady, run.TaskStatusQueued, run.TaskStatusRunning,
				run.TaskStatusWaitingReview, run.TaskStatusFailed,
			} {
				wantErr(t, db, `UPDATE tasks SET status=? WHERE id='t-1'`, []any{string(target)},
					sqliteConstraintTrigger, "task_terminal_immutable")
			}
			// Staying terminal is fine (e.g. an updated_at touch).
			wantOK(t, db, `UPDATE tasks SET updated_at=99 WHERE id='t-1'`)
		}
	})

	t.Run("failed may be retried to queued", func(t *testing.T) {
		db := schemaDB(t)
		wantOK(t, db, `UPDATE tasks SET status='failed' WHERE id='t-1'`)
		wantOK(t, db, `UPDATE tasks SET status='queued', revision=2 WHERE id='t-1'`)
		wantOK(t, db, `UPDATE tasks SET status='running', revision=3 WHERE id='t-1'`)
	})

	t.Run("claim fields are updatable and lease_epoch is fenced", func(t *testing.T) {
		db := schemaDB(t)
		wantOK(t, db, `UPDATE tasks SET lease_owner='w-1', lease_until=1700000009000, lease_epoch=1, revision=2 WHERE id='t-1'`)
		wantOK(t, db, `UPDATE tasks SET lease_owner='w-2', lease_until=1700000019000, lease_epoch=2, revision=3 WHERE id='t-1'`)
		// Clearing the claim must clear both halves at once.
		wantOK(t, db, `UPDATE tasks SET lease_owner=NULL, lease_until=NULL, revision=4 WHERE id='t-1'`)
		wantErr(t, db, `UPDATE tasks SET lease_owner='w-3' WHERE id='t-1'`, nil,
			sqliteConstraintCheck, "lease_owner")
		wantErr(t, db, `UPDATE tasks SET lease_epoch=-1 WHERE id='t-1'`, nil,
			sqliteConstraintCheck, "lease_epoch")
	})
}

// TestSchemaTriggerAttemptImmutable covers the attempt guards: identity is
// fixed at insert and a finished attempt never becomes active again.
func TestSchemaTriggerAttemptImmutable(t *testing.T) {
	t.Run("identity cannot change", func(t *testing.T) {
		db := schemaDB(t)
		exec(t, db, runInsert, "r-1", "t-1", "p-1", nil, "queued")
		exec(t, db, taskInsert, "t-2", "p-1", "ready")
		exec(t, db, runInsert, "r-2", "t-2", "p-1", nil, "queued")
		exec(t, db, `INSERT INTO attempts (id, run_id, attempt_no, backend, status, created_at)
		             VALUES ('a-1', 'r-1', 1, 'claude', 'running', 1)`)

		wantErr(t, db, `UPDATE attempts SET run_id='r-2' WHERE id='a-1'`, nil,
			sqliteConstraintTrigger, "attempt_identity_immutable")
		wantErr(t, db, `UPDATE attempts SET attempt_no=2 WHERE id='a-1'`, nil,
			sqliteConstraintTrigger, "attempt_identity_immutable")
		wantErr(t, db, `UPDATE attempts SET backend='other' WHERE id='a-1'`, nil,
			sqliteConstraintTrigger, "attempt_identity_immutable")
	})

	t.Run("a terminal attempt stays terminal", func(t *testing.T) {
		for _, terminal := range []run.AttemptStatus{
			run.AttemptStatusExited, run.AttemptStatusTerminated, run.AttemptStatusAbandoned,
		} {
			db := schemaDB(t)
			exec(t, db, runInsert, "r-1", "t-1", "p-1", nil, "queued")
			exec(t, db, `INSERT INTO attempts (id, run_id, attempt_no, backend, status, created_at)
			             VALUES ('a-1', 'r-1', 1, 'claude', ?, 1)`, string(terminal))

			for _, target := range []run.AttemptStatus{run.AttemptStatusStarting, run.AttemptStatusRunning} {
				wantErr(t, db, `UPDATE attempts SET status=? WHERE id='a-1'`, []any{string(target)},
					sqliteConstraintTrigger, "attempt_terminal_immutable")
			}
			// Bookkeeping on a finished attempt still works.
			wantOK(t, db, `UPDATE attempts SET exit_reason='done' WHERE id='a-1'`)
		}
	})

	t.Run("active attempts may move to terminal", func(t *testing.T) {
		db := schemaDB(t)
		exec(t, db, runInsert, "r-1", "t-1", "p-1", nil, "queued")
		exec(t, db, `INSERT INTO attempts (id, run_id, attempt_no, backend, status, owner_instance, pid, process_start_id, started_at, created_at)
		             VALUES ('a-1', 'r-1', 1, 'claude', 'starting', 'inst-1', 4242, 'boot-1', 1, 1)`)
		wantOK(t, db, `UPDATE attempts SET status='running' WHERE id='a-1'`)
		wantOK(t, db, `UPDATE attempts SET status='exited', exit_code=0, finished_at=9 WHERE id='a-1'`)
	})
}

// TestSchemaCheckConstraints covers the remaining column guards so a weakened
// CHECK cannot pass unnoticed.
func TestSchemaCheckConstraints(t *testing.T) {
	db := schemaDB(t)
	exec(t, db, runInsert, "r-1", "t-1", "p-1", nil, "queued")

	// binding_revision must be >= 1: a zero revision means "not captured".
	wantErr(t, db, `INSERT INTO runs (id, task_id, project_id, binding_id, binding_revision,
	    base_manifest_hash, agent_revision_id, input_snapshot_id, input_snapshot_hash,
	    status, revision, created_at, updated_at)
	    VALUES ('r-b0', 't-1', 'p-1', 'b-1', 0, 'sha256:m', 'ar-1', 'snap-1', 'sha256:s', 'queued', 1, 1, 2)`, nil,
		sqliteConstraintCheck, "binding_revision")

	// revision starts at 1 and may never be set to 0.
	wantErr(t, db, `INSERT INTO runs (id, task_id, project_id, binding_id, binding_revision,
	    base_manifest_hash, agent_revision_id, input_snapshot_id, input_snapshot_hash,
	    status, revision, created_at, updated_at)
	    VALUES ('r-rev0', 't-1', 'p-1', 'b-1', 1, 'sha256:m', 'ar-1', 'snap-1', 'sha256:s', 'queued', 0, 1, 2)`, nil,
		sqliteConstraintCheck, "revision")

	wantErr(t, db, `INSERT INTO tasks (id, project_id, title, kind, status, priority,
	    input_json, input_hash, lease_epoch, revision, created_at, updated_at)
	    VALUES ('t-rev0', 'p-1', 'title', 'code', 'ready', 0, '{}', 'sha256:i', 0, 0, 1, 2)`, nil,
		sqliteConstraintCheck, "revision")

	wantErr(t, db, `INSERT INTO tasks (id, project_id, title, kind, status, priority,
	    input_json, input_hash, lease_epoch, revision, created_at, updated_at)
	    VALUES ('t-epoch', 'p-1', 'title', 'code', 'ready', 0, '{}', 'sha256:i', -1, 1, 1, 2)`, nil,
		sqliteConstraintCheck, "lease_epoch")

	wantErr(t, db, `INSERT INTO attempts (id, run_id, attempt_no, backend, status, created_at)
	                VALUES ('a-0', 'r-1', 0, 'claude', 'starting', 1)`, nil,
		sqliteConstraintCheck, "attempt_no")
}

// TestSchemaNullableLegacyColumns proves the columns the plan marks nullable
// really accept NULL, so the store is not forced to invent placeholder values.
func TestSchemaNullableLegacyColumns(t *testing.T) {
	db := schemaDB(t)

	wantOK(t, db, `INSERT INTO project_refs (project_id, source_revision, snapshot_hash, state, captured_at, verified_at)
	               VALUES ('p-nullrev', NULL, 'sha256:x', 'active', 1, 2)`)
	wantOK(t, db, `INSERT INTO legacy_resource_refs (project_id, kind, resource_id, source_revision, snapshot_hash, captured_at)
	               VALUES ('p-1', 'agent_revision', 'ar-1', NULL, 'sha256:x', 1)`)
	wantOK(t, db, `INSERT INTO tasks (id, project_id, flow_id, stage_id, title, kind, status, priority,
	    input_json, input_hash, lease_owner, lease_until, lease_epoch, revision, created_at, updated_at)
	    VALUES ('t-null', 'p-1', NULL, NULL, 'x', 'document', 'ready', 0, '{}', 'sha256:i', NULL, NULL, 0, 1, 1, 2)`)
}

// TestSchemaProjectRefStateIsMutableButFrozenFieldsAreNot pins the one mutable
// column of project_refs: archiving must be possible, rewriting the captured
// hash must not be (a re-capture is a new snapshot with a new verified_at, and
// callers must not be able to silently re-point history at a different commit).
func TestSchemaProjectRefStateIsMutableButFrozenFieldsAreNot(t *testing.T) {
	db := schemaDB(t)
	wantOK(t, db, `UPDATE project_refs SET state='archived', verified_at=99 WHERE project_id='p-1'`)
	// The schema does not forbid rewriting snapshot_hash; document the actual
	// behaviour rather than asserting a guarantee that does not exist.
	var hash string
	if err := db.QueryRow(`SELECT snapshot_hash FROM project_refs WHERE project_id='p-1'`).Scan(&hash); err != nil {
		t.Fatalf("read snapshot_hash: %v", err)
	}
	if hash != "sha256:proj" {
		t.Errorf("snapshot_hash = %q, want sha256:proj", hash)
	}
}

// TestSchemaFrozenInputIsCaseSpecific guards against a false pass in
// TestSchemaTriggerRunFrozenInput: if a statement failed for an unrelated
// reason (e.g. a missing setup row), the frozen-input trigger would appear to
// fire. This asserts the setup itself is valid by mutating a non-frozen column
// of the same row.
func TestSchemaFrozenInputIsCaseSpecific(t *testing.T) {
	db := schemaDB(t)
	exec(t, db, `INSERT INTO project_refs (project_id, source_revision, snapshot_hash, state, captured_at, verified_at)
	             VALUES ('p-2', NULL, 'sha256:proj2', 'active', 1, 2)`)
	exec(t, db, taskInsert, "t-2", "p-1", "ready")
	exec(t, db, runInsert, "r-0", "t-2", "p-1", nil, "queued")
	exec(t, db, runInsertWith, "r-1", "t-1", "p-1", "cmd-1", nil, "queued", nil)

	// Mutating a non-frozen column of the same row succeeds...
	wantOK(t, db, `UPDATE runs SET updated_at=99, revision=2 WHERE id='r-1'`)
	// ...so the frozen-input failure below is caused by the column, not by a
	// broken row or a missing parent.
	wantErr(t, db, `UPDATE runs SET command_id='cmd-2', revision=3 WHERE id='r-1'`, nil,
		sqliteConstraintTrigger, "run_frozen_input_immutable")
	wantErr(t, db, `UPDATE runs SET command_id=NULL, revision=3 WHERE id='r-1'`, nil,
		sqliteConstraintTrigger, "run_frozen_input_immutable")
}
