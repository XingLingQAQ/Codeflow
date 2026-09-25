// Package runstore owns the persistent runtime library (codeflow.db): its
// migrations, and — from T1.01.b on — the transactional store over the run
// domain.
//
// This file is the migration runner. It is the only writer of the
// runstore_migrations table, which records the applied version, the migration
// file name and a content checksum.
//
// Why a private table instead of PRAGMA user_version: codeflow.db is scheduled
// to receive other domains later (T12.01), and user_version is a single slot
// that two domains would fight over. The runstore_migrations table also keeps
// the checksum, which user_version cannot.
//
// Failure policy is fail-closed. A recorded version whose checksum no longer
// matches the embedded file, or a database that is ahead of this binary, stops
// the runner before any pending migration is applied. Migrations never "fix
// forward" over an unknown state.
//
// Dependency direction: this package may import run and dbx; it must not
// import other business packages.
package runstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/codeflow/backend/internal/dbx"
)

// migrationsDir is the directory inside the embedded filesystem that holds the
// migration scripts. Files are named NNN_name.sql.
const migrationsDir = "migrations"

//go:embed migrations/*.sql
var embeddedMigrations embed.FS

// ErrMigrationChecksumMismatch means a version recorded in
// runstore_migrations no longer matches the embedded migration file (content
// changed, or the file was renamed). Nothing is applied: the runner stops
// because it cannot know whether the recorded version was really applied.
var ErrMigrationChecksumMismatch = errors.New("runstore: migration checksum mismatch")

// ErrSchemaTooNew means the database records a migration version that this
// binary does not embed — an older binary was pointed at a newer database.
// Downgrades are not supported and nothing is applied.
var ErrSchemaTooNew = errors.New("runstore: database schema is newer than this binary")

// AppliedMigration is one migration that this run applied, in order.
type AppliedMigration struct {
	Version int
	Name    string
	// Checksum is "sha256:" + lowercase hex of the migration file bytes.
	Checksum string
}

// MigrationResult reports what a Migrate call did. FromVersion is the highest
// version recorded before the call (0 for a fresh database); ToVersion is the
// highest version recorded after it. Applied is empty when the database was
// already current, which is what makes a second Migrate a no-op.
type MigrationResult struct {
	FromVersion int
	ToVersion   int
	Applied     []AppliedMigration
}

// migration is one embedded script plus its identity.
type migration struct {
	version  int
	name     string
	checksum string
	sql      string
}

// Migrate brings db up to the embedded schema version. It is safe to call on a
// fresh database, on a current one (no-op) and after a partially failed run
// (pending migrations continue where they stopped).
//
// Migrate does not close db; the caller owns the handle.
func Migrate(ctx context.Context, db *sql.DB) (MigrationResult, error) {
	return migrateFS(ctx, db, embeddedMigrations)
}

// Open opens (or creates) the runtime database at path with the dbx baseline
// and migrates it to the embedded schema version. It is the single entry point
// callers should use for codeflow.db.
//
// The returned *sql.DB is owned by the caller, who must Close it. On error the
// handle is already closed and nil is returned, so a failed migration cannot
// leak a half-initialised pool.
func Open(ctx context.Context, path string, opts ...dbx.Option) (*sql.DB, MigrationResult, error) {
	db, err := dbx.Open(path, opts...)
	if err != nil {
		return nil, MigrationResult{}, err
	}
	res, err := Migrate(ctx, db)
	if err != nil {
		_ = db.Close()
		return nil, MigrationResult{}, err
	}
	return db, res, nil
}

// migrateFS is the injectable core of Migrate: fsys must contain a
// migrationsDir directory. Tests use it to drive synthetic migrations and
// failure injection without touching the embedded set.
func migrateFS(ctx context.Context, db *sql.DB, fsys fs.FS) (MigrationResult, error) {
	migrations, err := loadMigrations(fsys)
	if err != nil {
		return MigrationResult{}, err
	}

	if err := ensureMigrationTable(ctx, db); err != nil {
		return MigrationResult{}, err
	}
	recorded, err := readRecordedVersions(ctx, db)
	if err != nil {
		return MigrationResult{}, err
	}

	from, err := verifyRecorded(recorded, migrations)
	if err != nil {
		return MigrationResult{}, err
	}

	result := MigrationResult{FromVersion: from, ToVersion: from}
	for _, m := range migrations[from:] {
		if err := applyMigration(ctx, db, m); err != nil {
			return result, err
		}
		result.Applied = append(result.Applied, AppliedMigration{
			Version:  m.version,
			Name:     m.name,
			Checksum: m.checksum,
		})
		result.ToVersion = m.version
	}
	return result, nil
}

// verifyRecorded checks the recorded state against the embedded set and
// returns the highest recorded version. It fails closed: any mismatch means
// the pending migrations must not run.
func verifyRecorded(recorded map[int]AppliedMigration, migrations []migration) (int, error) {
	if len(recorded) == 0 {
		return 0, nil
	}

	versions := make([]int, 0, len(recorded))
	for v := range recorded {
		versions = append(versions, v)
	}
	sort.Ints(versions)

	highest := versions[len(versions)-1]
	if highest > len(migrations) {
		return 0, fmt.Errorf("%w: recorded version %d, embedded highest %d",
			ErrSchemaTooNew, highest, len(migrations))
	}
	// A recorded set must be exactly 1..highest. A hole means either the table
	// was hand-edited or a previous runner lost a row; re-applying an
	// unrecorded version over a live database would corrupt data, so refuse.
	for i, v := range versions {
		if v != i+1 {
			return 0, fmt.Errorf("runstore: corrupt migration history: recorded versions %v are not contiguous from 1", versions)
		}
	}

	for _, v := range versions {
		rec := recorded[v]
		want := migrations[v-1]
		if rec.Checksum != want.checksum || rec.Name != want.name {
			return 0, fmt.Errorf("%w: version %d recorded as %q/%s but embedded is %q/%s",
				ErrMigrationChecksumMismatch, v, rec.Name, rec.Checksum, want.name, want.checksum)
		}
	}
	return highest, nil
}

// applyMigration runs one migration script and records it in a single
// transaction, so a script that fails halfway leaves neither its schema
// changes nor a version row behind.
func applyMigration(ctx context.Context, db *sql.DB, m migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("runstore: begin migration %d (%s): %w", m.version, m.name, err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return fmt.Errorf("runstore: apply migration %d (%s): %w", m.version, m.name, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO runstore_migrations (version, name, checksum, applied_at) VALUES (?, ?, ?, ?)`,
		m.version, m.name, m.checksum, time.Now().UnixMilli(),
	); err != nil {
		return fmt.Errorf("runstore: record migration %d (%s): %w", m.version, m.name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("runstore: commit migration %d (%s): %w", m.version, m.name, err)
	}
	return nil
}

// ensureMigrationTable creates the runner's own bookkeeping table. It is
// created here rather than in 001 so that the runner can read the recorded
// version before any migration has run.
func ensureMigrationTable(ctx context.Context, db *sql.DB) error {
	const ddl = `CREATE TABLE IF NOT EXISTS runstore_migrations (
    version    INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL,
    checksum   TEXT    NOT NULL,
    applied_at INTEGER NOT NULL
)`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("runstore: create runstore_migrations: %w", err)
	}
	return nil
}

// readRecordedVersions returns the applied migrations keyed by version.
func readRecordedVersions(ctx context.Context, db *sql.DB) (map[int]AppliedMigration, error) {
	rows, err := db.QueryContext(ctx, `SELECT version, name, checksum FROM runstore_migrations`)
	if err != nil {
		return nil, fmt.Errorf("runstore: read runstore_migrations: %w", err)
	}
	defer rows.Close()

	out := map[int]AppliedMigration{}
	for rows.Next() {
		var m AppliedMigration
		if err := rows.Scan(&m.Version, &m.Name, &m.Checksum); err != nil {
			return nil, fmt.Errorf("runstore: scan runstore_migrations: %w", err)
		}
		out[m.Version] = m
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("runstore: iterate runstore_migrations: %w", err)
	}
	return out, nil
}

// loadMigrations reads and validates every NNN_name.sql file in fsys. Versions
// must start at 1 and be contiguous: a gap would make "highest recorded
// version" ambiguous, which is the one thing the runner must never guess.
func loadMigrations(fsys fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, migrationsDir)
	if err != nil {
		return nil, fmt.Errorf("runstore: read %s: %w", migrationsDir, err)
	}

	var out []migration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version, name, err := parseMigrationName(entry.Name())
		if err != nil {
			return nil, err
		}
		body, err := fs.ReadFile(fsys, path.Join(migrationsDir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("runstore: read %s: %w", entry.Name(), err)
		}
		sum := sha256.Sum256(body)
		out = append(out, migration{
			version:  version,
			name:     name,
			checksum: "sha256:" + hex.EncodeToString(sum[:]),
			sql:      string(body),
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	for i, m := range out {
		if m.version != i+1 {
			return nil, fmt.Errorf("runstore: migration versions must be contiguous from 1, got %d at position %d (%s)", m.version, i+1, m.name)
		}
	}
	return out, nil
}

// parseMigrationName splits "001_runtime.sql" into version 1 and name
// "runtime". The numeric prefix must be digits and the name must be non-empty,
// so a typo fails at startup instead of silently reordering migrations.
func parseMigrationName(filename string) (int, string, error) {
	base := strings.TrimSuffix(filename, ".sql")
	digits, name, found := strings.Cut(base, "_")
	if !found || digits == "" || name == "" {
		return 0, "", fmt.Errorf("runstore: migration %q must be named NNN_name.sql", filename)
	}
	version, err := strconv.Atoi(digits)
	if err != nil {
		return 0, "", fmt.Errorf("runstore: migration %q has a non-numeric version prefix: %w", filename, err)
	}
	if version < 1 {
		return 0, "", fmt.Errorf("runstore: migration %q has version %d, want >= 1", filename, version)
	}
	if digits != fmt.Sprintf("%03d", version) {
		return 0, "", fmt.Errorf("runstore: migration %q must zero-pad its version to three digits", filename)
	}
	return version, name, nil
}
