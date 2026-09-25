// Store: the transactional entry point over the run domain (T1.01.b).
//
// Design rules this file exists to enforce:
//
//   - Every write is a package-level function taking a Tx. There is no
//     "convenient" write method that opens its own transaction, because the
//     whole point is that a caller (T1.05's events/outbox, T1.11's command
//     record) can compose several writes into one atomic unit. A write helper
//     that committed on its own would make that composition impossible.
//   - Reads take a Querier, so the same function works inside a transaction
//     (where it sees uncommitted changes) and on the pool (where it does not).
//   - Time crosses this boundary exactly once: run.Time fields are Unix
//     milliseconds in INTEGER columns, and every conversion happens here.
//
// Locking: OpenStore pins transactions to BEGIN IMMEDIATE. Under WAL a
// deferred transaction that reads and then writes can be refused with
// SQLITE_BUSY_SNAPSHOT, and SQLite does not call the busy handler for that
// failure, so busy_timeout cannot save it. Every write in this package reads a
// revision before it writes one, so the write lock must be taken at BEGIN.
// See dbx.WithTxLock and TestTxLockImmediateAvoidsSnapshotUpgradeFailure.

package runstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/codeflow/backend/internal/dbx"
)

// Tx is the subset of *sql.Tx the store needs. Write functions take this
// interface rather than *sql.Tx so that T1.05 (events/outbox) can add its own
// tables to the same transaction without the store having to know about them.
//
// *sql.Tx satisfies Tx. Callers obtain one from Store.WithTx and must not
// commit or roll back a Tx themselves: WithTx owns the transaction lifetime.
type Tx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Querier is what the read functions need. Both Tx and *sql.DB satisfy it, so a
// caller can read through the pool or inside its own transaction with the same
// call.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// compile-time proof that the concrete types satisfy the interfaces, so a
// future Go change cannot quietly break the contract.
var (
	_ Tx      = (*sql.Tx)(nil)
	_ Querier = (*sql.Tx)(nil)
	_ Querier = (*sql.DB)(nil)
)

// Store owns the database handle for the runtime library. It is a thin wrapper:
// its only jobs are to hold the handle and to run transactions with the right
// locking mode.
type Store struct {
	db *sql.DB
}

// OpenStore opens (or creates) the runtime database at path, migrates it to the
// embedded schema version and returns a Store over it.
//
// The caller owns the Store and must Close it. On error nothing is left open.
//
// The transaction mode is pinned to immediate and is appended after the
// caller's options on purpose: a caller cannot weaken it by passing
// dbx.WithTxLock("deferred"), because every store write reads before it writes
// and would then be exposed to the WAL snapshot-upgrade failure.
func OpenStore(ctx context.Context, path string, opts ...dbx.Option) (*Store, MigrationResult, error) {
	db, res, err := Open(ctx, path, append(opts, dbx.WithTxLock("immediate"))...)
	if err != nil {
		return nil, MigrationResult{}, err
	}
	return NewStore(db), res, nil
}

// NewStore wraps an already-open, already-migrated handle. It does not migrate
// and does not take ownership of the handle beyond Close: a caller that built
// the handle itself may prefer to close it directly.
//
// The handle is expected to use dbx.WithTxLock("immediate"); a handle opened
// without it still works but loses the snapshot-upgrade protection described in
// the package comment. NewStore cannot verify this, because the DSN is not
// readable back from *sql.DB.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// DB returns the underlying handle for the operations the store does not wrap
// (schema introspection in tests, and later the read-only projections of
// T1.05). Writes must go through the typed functions in records.go.
func (s *Store) DB() *sql.DB { return s.db }

// Close releases the handle. It is safe to call once; calling it twice returns
// whatever database/sql returns for the second call.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// WithTx runs fn inside one transaction.
//
//   - fn returns nil: the transaction is committed. A commit failure is
//     returned, because the caller must not believe a write landed when it did
//     not.
//   - fn returns an error: the transaction is rolled back and that error is
//     returned unchanged (not wrapped), so errors.Is/As keep working.
//   - fn panics: the transaction is rolled back and the panic is re-raised.
//     Swallowing it would turn a programming error into a silent data loss.
//
// The ctx handed to fn is the same one passed in; it is provided so the
// signature stays stable if the store later needs to attach values (a trace id,
// say) without touching every call site.
func (s *Store) WithTx(ctx context.Context, fn func(ctx context.Context, tx Tx) error) error {
	if fn == nil {
		return fmt.Errorf("runstore: WithTx: nil function")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("runstore: begin transaction: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			// Rollback after a successful commit is a no-op error
			// (sql.ErrTxDone); the commit path already handled the real one.
			_ = tx.Rollback()
		}
	}()

	// Re-panic after rollback. The deferred rollback above runs first because
	// defers unwind last-in-first-out, and this defer is registered after it.
	defer func() {
		if r := recover(); r != nil {
			panic(r)
		}
	}()

	if err := fn(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("runstore: commit transaction: %w", err)
	}
	committed = true
	return nil
}

// queryRowExists reports whether at least one row matches. It is used by the
// CAS paths, which must distinguish "no such row" from "row moved on".
func queryRowExists(ctx context.Context, q Querier, query string, args ...any) (bool, error) {
	var one int
	err := q.QueryRowContext(ctx, query, args...).Scan(&one)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return false, nil
	case err != nil:
		return false, err
	default:
		return true, nil
	}
}
