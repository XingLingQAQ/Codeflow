package runstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/codeflow/backend/internal/dbx"
)

// openTemp opens a file database under t.TempDir(). File (not ":memory:") is
// deliberate: the migration contract is about a database that survives a
// process, and dbx gives ":memory:" handles a private database per Open, which
// would make "close and reopen" untestable.
func openTemp(t *testing.T) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "codeflow.db")
	db, err := dbx.Open(path)
	if err != nil {
		t.Fatalf("dbx.Open(%s): %v", path, err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, path
}

// migrateTemp opens a fresh file database and migrates it, returning the handle
// and its path.
func migrateTemp(t *testing.T) (*sql.DB, string) {
	t.Helper()
	db, path := openTemp(t)
	if _, err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("Migrate on fresh database: %v", err)
	}
	return db, path
}

func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&n); err != nil {
		t.Fatalf("query sqlite_master for %q: %v", name, err)
	}
	return n > 0
}

// TestMigrateFromEmpty is §19.4 "empty database from zero": every business table
// plus the runner's own bookkeeping table exist, the reported versions are
// 0 -> 1, and the recorded checksum is the sha256 of the embedded file.
func TestMigrateFromEmpty(t *testing.T) {
	ctx := context.Background()
	db, _ := openTemp(t)

	// Foreign keys must be on for the whole feature to mean anything; assert it
	// before trusting any constraint test.
	var fk int
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil {
		t.Fatalf("PRAGMA foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Fatalf("PRAGMA foreign_keys = %d, want 1", fk)
	}

	res, err := Migrate(ctx, db)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if res.FromVersion != 0 {
		t.Errorf("FromVersion = %d, want 0", res.FromVersion)
	}
	if res.ToVersion != 3 {
		t.Errorf("ToVersion = %d, want 3", res.ToVersion)
	}
	if len(res.Applied) != 3 {
		t.Fatalf("Applied = %v, want all three migrations", res.Applied)
	}
	if res.Applied[0].Version != 1 || res.Applied[0].Name != "runtime" {
		t.Errorf("Applied[0] = %+v, want version 1 named runtime", res.Applied[0])
	}
	if res.Applied[1].Version != 2 || res.Applied[1].Name != "input_snapshots" {
		t.Errorf("Applied[1] = %+v, want version 2 named input_snapshots", res.Applied[1])
	}
	if res.Applied[2].Version != 3 || res.Applied[2].Name != "events" {
		t.Errorf("Applied[2] = %+v, want version 3 named events", res.Applied[2])
	}

	for _, table := range []string{
		"project_refs", "legacy_resource_refs", "tasks", "runs", "attempts",
		"input_snapshots", "events", "outbox", "scope_counters", "runstore_migrations",
	} {
		if !tableExists(t, db, table) {
			t.Errorf("table %q does not exist after migration", table)
		}
	}

	// The recorded checksum must equal the sha256 of the embedded migration
	// file, computed independently here rather than read back from the runner.
	body, err := embeddedMigrations.ReadFile("migrations/001_runtime.sql")
	if err != nil {
		t.Fatalf("read embedded migration: %v", err)
	}
	// Same rule as loadMigrations: the checksum is over LF-normalized text.
	body = bytes.ReplaceAll(body, []byte("\r\n"), []byte("\n"))
	sum := sha256.Sum256(body)
	wantChecksum := "sha256:" + hex.EncodeToString(sum[:])

	var (
		version  int
		name     string
		checksum string
		applied  int64
	)
	if err := db.QueryRow(
		`SELECT version, name, checksum, applied_at FROM runstore_migrations WHERE version = 1`,
	).Scan(&version, &name, &checksum, &applied); err != nil {
		t.Fatalf("read runstore_migrations: %v", err)
	}
	if version != 1 || name != "runtime" {
		t.Errorf("recorded (version,name) = (%d,%q), want (1,runtime)", version, name)
	}
	if checksum != wantChecksum {
		t.Errorf("recorded checksum = %s, want %s", checksum, wantChecksum)
	}
	if res.Applied[0].Checksum != wantChecksum {
		t.Errorf("Applied[0].Checksum = %s, want %s", res.Applied[0].Checksum, wantChecksum)
	}
	if applied <= 0 {
		t.Errorf("applied_at = %d, want a positive Unix millisecond timestamp", applied)
	}

	// PRAGMA user_version must stay untouched: T12.01 migrates other domains
	// into this same file and a single-slot version counter would collide.
	var userVersion int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&userVersion); err != nil {
		t.Fatalf("PRAGMA user_version: %v", err)
	}
	if userVersion != 0 {
		t.Errorf("PRAGMA user_version = %d, want 0 (runstore_migrations is the version record)", userVersion)
	}
}

// seedRuntimeFixture inserts one project_ref/task/snapshot/run set. Tests that
// compare data across a re-migration use it so they compare real rows, not an
// empty database.
//
// The input_snapshots row is required by migration 002: the run below pins
// snap-1/sha256:snap, and trg_runs_input_snapshot_must_match refuses a run whose
// snapshot id/hash do not name a real snapshot of the same project.
func seedRuntimeFixture(t *testing.T, db *sql.DB) {
	t.Helper()
	stmts := []string{
		`INSERT INTO project_refs (project_id, source_revision, snapshot_hash, state, captured_at, verified_at)
		 VALUES ('p-1', NULL, 'sha256:proj', 'active', 1700000000000, 1700000000001)`,
		`INSERT INTO tasks (id, project_id, flow_id, stage_id, title, kind, status, priority,
		                    input_json, input_hash, lease_owner, lease_until, lease_epoch, revision, created_at, updated_at)
		 VALUES ('t-1', 'p-1', NULL, NULL, 'add tests', 'code', 'ready', 3,
		         '{"prompt":"add tests"}', 'sha256:in', 'worker-1', 1700000009000, 2, 1, 1700000000002, 1700000000003)`,
		`INSERT INTO input_snapshots (id, project_id, content_json, content_hash, created_at)
		 VALUES ('snap-1', 'p-1', '{"prompt":"add tests"}', 'sha256:snap', 1700000000006)`,
		`INSERT INTO runs (id, task_id, project_id, command_id, binding_id, binding_revision,
		                   base_manifest_hash, base_commit, agent_revision_id, input_snapshot_id,
		                   input_snapshot_hash, budget_json, status, revision, retry_of_run_id,
		                   created_at, updated_at, finished_at)
		 VALUES ('r-1', 't-1', 'p-1', 'cmd-1', 'b-1', 7,
		         'sha256:manifest', 'abc123', 'ar-1', 'snap-1',
		         'sha256:snap', '{"tokens":50000}', 'queued', 1, NULL,
		         1700000000004, 1700000000005, NULL)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}
}

// rowSnapshot reads one run row field-by-field so a re-migration can be proven
// not to have touched any column.
func runRow(t *testing.T, db *sql.DB, id string) []any {
	t.Helper()
	var (
		taskID, projectID, bindingID, manifest, agentRev, snapID, snapHash, budget, status string
		commandID, baseCommit, retryOf                                                     sql.NullString
		finishedAt                                                                         sql.NullInt64
		bindingRevision, revision, createdAt, updatedAt                                    int64
	)
	err := db.QueryRow(`SELECT task_id, project_id, command_id, binding_id, binding_revision,
	       base_manifest_hash, base_commit, agent_revision_id, input_snapshot_id, input_snapshot_hash,
	       budget_json, status, revision, retry_of_run_id, created_at, updated_at, finished_at
	FROM runs WHERE id = ?`, id).Scan(
		&taskID, &projectID, &commandID, &bindingID, &bindingRevision,
		&manifest, &baseCommit, &agentRev, &snapID, &snapHash,
		&budget, &status, &revision, &retryOf, &createdAt, &updatedAt, &finishedAt,
	)
	if err != nil {
		t.Fatalf("read run %q: %v", id, err)
	}
	return []any{
		taskID, projectID, commandID, bindingID, bindingRevision,
		manifest, baseCommit, agentRev, snapID, snapHash,
		budget, status, revision, retryOf, createdAt, updatedAt, finishedAt,
	}
}

// TestMigrateTwiceKeepsData covers §19.4 "the same migration twice does not
// duplicate or overwrite user data" and the reopen case. Note: §19.4 also asks
// for a backfill test on an existing old schema, which does not apply to 001 —
// codeflow.db did not exist before this migration, so there is no legacy schema
// to backfill. The reopen assertion covers the part that does apply.
func TestMigrateTwiceKeepsData(t *testing.T) {
	ctx := context.Background()
	db, path := openTemp(t)

	first, err := Migrate(ctx, db)
	if err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if first.ToVersion != 3 {
		t.Fatalf("first Migrate ToVersion = %d, want 3", first.ToVersion)
	}

	seedRuntimeFixture(t, db)
	before := runRow(t, db, "r-1")

	// Second call on the live handle: nothing to apply, no new rows.
	second, err := Migrate(ctx, db)
	if err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	if second.FromVersion != 3 || second.ToVersion != 3 {
		t.Errorf("second Migrate versions = %d -> %d, want 3 -> 3", second.FromVersion, second.ToVersion)
	}
	if len(second.Applied) != 0 {
		t.Errorf("second Migrate applied %v, want nothing", second.Applied)
	}
	assertMigrationRowCount(t, db, 3)
	assertRunUnchanged(t, db, "r-1", before)

	// Close and reopen the file: the recorded version must make the third call
	// a no-op too, and the data must be byte-identical field by field.
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	reopened, err := dbx.Open(path)
	if err != nil {
		t.Fatalf("reopen %s: %v", path, err)
	}
	defer reopened.Close()

	third, err := Migrate(ctx, reopened)
	if err != nil {
		t.Fatalf("Migrate after reopen: %v", err)
	}
	if third.FromVersion != 3 || third.ToVersion != 3 || len(third.Applied) != 0 {
		t.Errorf("reopen Migrate = %+v, want 3 -> 3 with nothing applied", third)
	}
	assertMigrationRowCount(t, reopened, 3)
	assertRunUnchanged(t, reopened, "r-1", before)
}

func assertMigrationRowCount(t *testing.T, db *sql.DB, want int) {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM runstore_migrations`).Scan(&n); err != nil {
		t.Fatalf("count runstore_migrations: %v", err)
	}
	if n != want {
		t.Errorf("runstore_migrations rows = %d, want %d", n, want)
	}
}

func assertRunUnchanged(t *testing.T, db *sql.DB, id string, want []any) {
	t.Helper()
	got := runRow(t, db, id)
	if len(got) != len(want) {
		t.Fatalf("run %q has %d columns, want %d", id, len(got), len(want))
	}
	for i := range want {
		if fmt.Sprint(got[i]) != fmt.Sprint(want[i]) {
			t.Errorf("run %q column %d = %v, want %v", id, i, got[i], want[i])
		}
	}
}

// TestMigrateFailureRollsBack is §19.4 "inject an error at a key step and
// re-running continues or rolls back safely". Migration 002 succeeds on its
// first statement and fails on its second, so this proves the whole migration —
// including the version row — is one transaction.
func TestMigrateFailureRollsBack(t *testing.T) {
	ctx := context.Background()
	db, _ := openTemp(t)

	broken := os.DirFS(filepath.Join("testdata", "inject"))
	if _, err := migrateFS(ctx, db, broken); err == nil {
		t.Fatal("migrateFS with a broken 002 succeeded, want an error")
	} else if !strings.Contains(err.Error(), "apply migration 2 (broken)") {
		t.Errorf("error %q does not name the failing migration", err)
	}

	// The failed migration must leave nothing: not its version row, and not the
	// table its first statement created.
	assertMigrationRowCount(t, db, 1)
	if tableExists(t, db, "inject_second") {
		t.Error("inject_second exists after a rolled-back migration 002")
	}
	if !tableExists(t, db, "inject_first") {
		t.Error("inject_first is missing; migration 001 should have been committed")
	}

	// Re-running the same broken set must not double-apply 001.
	if _, err := migrateFS(ctx, db, broken); err == nil {
		t.Fatal("second migrateFS with a broken 002 succeeded, want an error")
	}
	assertMigrationRowCount(t, db, 1)

	// Repair 002: the runner must resume at version 2 rather than restart.
	repaired := fstest.MapFS{
		"migrations/001_ok.sql":       {Data: mustRead(t, broken, "migrations/001_ok.sql")},
		"migrations/002_repaired.sql": {Data: []byte("CREATE TABLE inject_second (id TEXT PRIMARY KEY);\n")},
	}
	res, err := migrateFS(ctx, db, repaired)
	if err != nil {
		t.Fatalf("migrateFS after repair: %v", err)
	}
	if res.FromVersion != 1 || res.ToVersion != 2 {
		t.Errorf("repaired Migrate versions = %d -> %d, want 1 -> 2", res.FromVersion, res.ToVersion)
	}
	if len(res.Applied) != 1 || res.Applied[0].Version != 2 {
		t.Errorf("repaired Migrate applied %v, want only version 2", res.Applied)
	}
	if !tableExists(t, db, "inject_second") {
		t.Error("inject_second is missing after the repaired migration")
	}
	assertMigrationRowCount(t, db, 2)
}

func mustRead(t *testing.T, fsys fs.FS, name string) []byte {
	t.Helper()
	body, err := fs.ReadFile(fsys, name)
	if err != nil {
		t.Fatalf("read %s in test fs: %v", name, err)
	}
	return body
}

// TestMigrateChecksumMismatchFailsClosed tampers with the recorded checksum of
// version 1. The runner must stop with ErrMigrationChecksumMismatch (and not
// "fix" the record or apply anything else), because it cannot tell whether the
// recorded migration was really the one that ran.
func TestMigrateChecksumMismatchFailsClosed(t *testing.T) {
	ctx := context.Background()
	db, _ := migrateTemp(t)

	if _, err := db.Exec(`UPDATE runstore_migrations SET checksum='sha256:0000' WHERE version=1`); err != nil {
		t.Fatalf("tamper checksum: %v", err)
	}

	_, err := Migrate(ctx, db)
	if !errors.Is(err, ErrMigrationChecksumMismatch) {
		t.Fatalf("Migrate error = %v, want ErrMigrationChecksumMismatch", err)
	}
	// Fail-closed: the tampered record is still exactly as it was, and no
	// further migration was applied.
	var checksum string
	if err := db.QueryRow(`SELECT checksum FROM runstore_migrations WHERE version=1`).Scan(&checksum); err != nil {
		t.Fatalf("read checksum: %v", err)
	}
	if checksum != "sha256:0000" {
		t.Errorf("checksum = %s, want the tampered value to be left alone", checksum)
	}
	assertMigrationRowCount(t, db, 3)

	// A renamed file is the same class of mismatch and must also stop the run.
	if _, err := db.Exec(`UPDATE runstore_migrations SET checksum=? WHERE version=1`,
		mustEmbeddedChecksum(t)); err != nil {
		t.Fatalf("restore checksum: %v", err)
	}
	if _, err := db.Exec(`UPDATE runstore_migrations SET name='renamed' WHERE version=1`); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if _, err := Migrate(ctx, db); !errors.Is(err, ErrMigrationChecksumMismatch) {
		t.Fatalf("Migrate with renamed record = %v, want ErrMigrationChecksumMismatch", err)
	}
}

func mustEmbeddedChecksum(t *testing.T) string {
	t.Helper()
	body, err := embeddedMigrations.ReadFile("migrations/001_runtime.sql")
	if err != nil {
		t.Fatalf("read embedded migration: %v", err)
	}
	// Same rule as loadMigrations: the checksum is over LF-normalized text.
	body = bytes.ReplaceAll(body, []byte("\r\n"), []byte("\n"))
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// TestMigrationChecksumIgnoresLineEndings: go:embed takes the migration bytes as
// checked out, and checkouts disagree about line endings (core.autocrlf on
// Windows turns the committed LF into CRLF). A database migrated by a binary
// built from one checkout must open cleanly with a binary built from another,
// so the checksum is computed on LF-normalized content; a real content change
// is still a mismatch.
func TestMigrationChecksumIgnoresLineEndings(t *testing.T) {
	ctx := context.Background()
	const lf = "CREATE TABLE widgets (\n    id INTEGER PRIMARY KEY\n);\n"
	crlf := strings.ReplaceAll(lf, "\n", "\r\n")
	lfFS := fstest.MapFS{"migrations/001_widgets.sql": {Data: []byte(lf)}}
	crlfFS := fstest.MapFS{"migrations/001_widgets.sql": {Data: []byte(crlf)}}

	fromLF, err := loadMigrations(lfFS)
	if err != nil {
		t.Fatalf("loadMigrations(LF): %v", err)
	}
	fromCRLF, err := loadMigrations(crlfFS)
	if err != nil {
		t.Fatalf("loadMigrations(CRLF): %v", err)
	}
	sum := sha256.Sum256([]byte(lf))
	if want := "sha256:" + hex.EncodeToString(sum[:]); fromLF[0].checksum != want || fromCRLF[0].checksum != want {
		t.Fatalf("checksums LF=%s CRLF=%s, want both %s (the LF text)", fromLF[0].checksum, fromCRLF[0].checksum, want)
	}

	for _, tc := range []struct {
		name          string
		first, second fstest.MapFS
	}{
		{"built from LF, reopened from CRLF", lfFS, crlfFS},
		{"built from CRLF, reopened from LF", crlfFS, lfFS},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, _ := openTemp(t)
			if _, err := migrateFS(ctx, db, tc.first); err != nil {
				t.Fatalf("first migrate: %v", err)
			}
			res, err := migrateFS(ctx, db, tc.second)
			if err != nil {
				t.Fatalf("reopen with the other line endings: %v", err)
			}
			if len(res.Applied) != 0 {
				t.Fatalf("reopen applied %d migration(s), want none", len(res.Applied))
			}
		})
	}

	changed := fstest.MapFS{"migrations/001_widgets.sql": {Data: []byte(strings.Replace(lf, " id ", " uid ", 1))}}
	db, _ := openTemp(t)
	if _, err := migrateFS(ctx, db, lfFS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := migrateFS(ctx, db, changed); !errors.Is(err, ErrMigrationChecksumMismatch) {
		t.Fatalf("changed content: err = %v, want ErrMigrationChecksumMismatch", err)
	}
}

// TestMigrateRejectsNewerSchema is the downgrade guard: an older binary must
// refuse to touch a database written by a newer one instead of applying its own
// older migrations over it.
func TestMigrateRejectsNewerSchema(t *testing.T) {
	ctx := context.Background()
	db, _ := migrateTemp(t)

	if _, err := db.Exec(
		`INSERT INTO runstore_migrations (version, name, checksum, applied_at) VALUES (99, 'future', 'sha256:x', 1)`,
	); err != nil {
		t.Fatalf("insert future version: %v", err)
	}

	_, err := Migrate(ctx, db)
	if !errors.Is(err, ErrSchemaTooNew) {
		t.Fatalf("Migrate error = %v, want ErrSchemaTooNew", err)
	}
	// Nothing was applied and the future row is untouched.
	assertMigrationRowCount(t, db, 4)
}

// TestMigrateRejectsCorruptHistory covers a hole in the recorded version set.
// Re-applying an unrecorded version over a live database could corrupt data, so
// the runner refuses instead of guessing.
func TestMigrateRejectsCorruptHistory(t *testing.T) {
	ctx := context.Background()
	db, _ := migrateTemp(t)

	if _, err := db.Exec(`DELETE FROM runstore_migrations WHERE version=1`); err != nil {
		t.Fatalf("delete version 1: %v", err)
	}
	// Version 2 is already recorded by the successful migration above; the hole
	// is what the runner must refuse. (Inserting a synthetic version 2 would
	// collide with the real row.)
	if _, err := Migrate(ctx, db); err == nil {
		t.Fatal("Migrate with a version gap succeeded, want an error")
	}
}

// TestOpenCreatesMigratedDatabase is the entry point T1.01.b will call: Open
// must return a migrated, usable handle, and must close it if migration fails.
func TestOpenCreatesMigratedDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "codeflow.db")

	db, res, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	if res.FromVersion != 0 || res.ToVersion != 3 {
		t.Errorf("Open migration result = %d -> %d, want 0 -> 3", res.FromVersion, res.ToVersion)
	}
	if !tableExists(t, db, "runs") {
		t.Error("runs table is missing after Open")
	}
	// The handle must be immediately usable, and a second Open must be a no-op
	// that keeps the first handle's data.
	seedRuntimeFixture(t, db)

	db2, res2, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer db2.Close()
	if len(res2.Applied) != 0 || res2.ToVersion != 3 {
		t.Errorf("second Open = %+v, want no applied migrations at version 3", res2)
	}
	_ = runRow(t, db2, "r-1")
}

// TestOpenClosesHandleOnMigrationFailure proves Open does not leak a
// half-initialised pool when the database is too new for this binary.
func TestOpenClosesHandleOnMigrationFailure(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "codeflow.db")

	db, _, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("initial Open: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO runstore_migrations (version, name, checksum, applied_at) VALUES (99, 'future', 'sha256:x', 1)`,
	); err != nil {
		t.Fatalf("insert future version: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if _, _, err := Open(ctx, path); !errors.Is(err, ErrSchemaTooNew) {
		t.Fatalf("Open on newer schema = %v, want ErrSchemaTooNew", err)
	}
	// The file must still be openable by a fresh handle, which it would not be
	// if the failed Open had left a lock behind.
	verify, err := dbx.Open(path)
	if err != nil {
		t.Fatalf("reopen after failed Open: %v", err)
	}
	defer verify.Close()
	assertMigrationRowCount(t, verify, 4)
}

// TestLoadMigrationsRejectsBadNames pins the file naming contract, because a
// silently misparsed version is the one failure the runner cannot detect later.
func TestLoadMigrationsRejectsBadNames(t *testing.T) {
	cases := []struct {
		name string
		file string
	}{
		{"no underscore", "001runtime.sql"},
		{"non-numeric version", "abc_runtime.sql"},
		{"zero version", "000_runtime.sql"},
		{"unpadded version", "1_runtime.sql"},
		{"empty name", "001_.sql"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fsys := fstest.MapFS{"migrations/" + tc.file: {Data: []byte("SELECT 1;")}}
			if _, err := loadMigrations(fsys); err == nil {
				t.Fatalf("loadMigrations accepted %q, want an error", tc.file)
			}
		})
	}
}

// TestLoadMigrationsRejectsVersionGap pins the contiguity rule: versions must
// run 1..N, otherwise "highest recorded version" stops identifying the applied
// set.
func TestLoadMigrationsRejectsVersionGap(t *testing.T) {
	fsys := fstest.MapFS{
		"migrations/001_a.sql": {Data: []byte("SELECT 1;")},
		"migrations/003_c.sql": {Data: []byte("SELECT 1;")},
	}
	if _, err := loadMigrations(fsys); err == nil {
		t.Fatal("loadMigrations accepted a version gap, want an error")
	}
}

// TestLoadMigrationsSortsByVersion proves the runner orders by the numeric
// prefix, not by filename string order.
func TestLoadMigrationsSortsByVersion(t *testing.T) {
	fsys := fstest.MapFS{
		"migrations/002_b.sql": {Data: []byte("SELECT 1;")},
		"migrations/001_a.sql": {Data: []byte("SELECT 1;")},
	}
	got, err := loadMigrations(fsys)
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	if len(got) != 2 || got[0].version != 1 || got[1].version != 2 {
		t.Fatalf("loadMigrations order = %+v, want versions 1,2", got)
	}
}

// TestEmbeddedMigrationIsContiguous guards the shipped set itself: a hand-added
// migration with a wrong prefix would otherwise only fail at runtime.
func TestEmbeddedMigrationIsContiguous(t *testing.T) {
	got, err := loadMigrations(embeddedMigrations)
	if err != nil {
		t.Fatalf("loadMigrations(embedded): %v", err)
	}
	if len(got) == 0 {
		t.Fatal("no embedded migrations found")
	}
	for i, m := range got {
		if m.version != i+1 {
			t.Errorf("embedded migration %d has version %d", i, m.version)
		}
		if m.checksum == "" || m.checksum[:7] != "sha256:" {
			t.Errorf("embedded migration %s has checksum %q, want a sha256: prefix", m.name, m.checksum)
		}
	}
}
