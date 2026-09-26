// Error contract for the run store (T1.01.b): the typed errors callers branch
// on, and the single place that translates a database refusal into one of them.
//
// The store is the only layer that talks to SQLite, so it is also the layer
// that turns a database-level refusal into a typed error. Callers must not
// match on SQLite result codes or RAISE message text themselves; they use
// errors.Is/errors.As against the sentinels below.

package runstore

import (
	"errors"
	"fmt"
	"strings"

	"github.com/codeflow/backend/internal/run"
)

// ErrNotFound means the addressed row does not exist. Reads return it
// directly; a CAS returns it when no row matched and the row is genuinely
// absent, as opposed to present with a stale revision (ErrRevisionConflict).
var ErrNotFound = errors.New("runstore: not found")

// ErrRevisionConflict is the sentinel wrapped by *RevisionConflictError and
// *TaskRevisionConflictError. Use errors.Is(err, ErrRevisionConflict) to detect
// a lost update; use errors.As to read the revision actually stored.
var ErrRevisionConflict = errors.New("runstore: revision conflict")

// ErrTerminalImmutable means the row is already in a terminal state and the
// requested change would have moved it out. The database triggers
// (run_terminal_immutable, task_terminal_immutable, attempt_terminal_immutable)
// refuse it, and the store reports that refusal as this error.
var ErrTerminalImmutable = errors.New("runstore: terminal state is immutable")

// ErrFrozenInputImmutable means a statement tried to rewrite a frozen column:
// a run's input pin, a task's input, an attempt's identity, or a stored
// snapshot body. The store never does this on purpose; surfacing it turns a bug
// into a loud error instead of a silently mangled row.
var ErrFrozenInputImmutable = errors.New("runstore: frozen input is immutable")

// ErrInputSnapshotMismatch means a Run named an input snapshot that does not
// exist, belongs to another project, or does not hash to the hash the run
// pins. The BEFORE INSERT trigger on runs refuses the row; the store maps that
// here so the caller learns the input was not frozen correctly rather than
// seeing a raw constraint violation. A rewrite of a stored body is
// ErrFrozenInputImmutable, not this.
var ErrInputSnapshotMismatch = errors.New("runstore: input snapshot does not match")

// ErrSecretInSnapshot means the snapshot body contained a value that looks like
// a secret. The check runs before any SQL is issued, so nothing is written and
// no transaction is left open. The error never contains the value.
var ErrSecretInSnapshot = errors.New("runstore: secret value in input snapshot")

// ErrHistoryDeleteForbidden means a hard DELETE against runs, attempts or
// input_snapshots was refused. Retention and cleanup are T12.02 and must go
// through an explicit migration, so the store exposes no delete path and the
// database refuses one issued directly.
var ErrHistoryDeleteForbidden = errors.New("runstore: run history cannot be deleted")

// ErrInvalidRecord means the caller handed the store a record the schema would
// reject: an unknown status, a revision other than the frozen 1, an empty id.
// These are programmer errors, not data conflicts, and are reported before any
// SQL runs so a partial write cannot happen.
var ErrInvalidRecord = errors.New("runstore: invalid record")

// ErrInvalidEvent means the caller handed AppendEventTx an event the contract
// rejects: a type outside the closed enum, an identity without project_id or
// actor (or one naming a different project/run than the row it would be written
// under), a payload that is not a JSON object, or a payload over the 256 KiB
// limit (§27.4). Like ErrInvalidRecord these are caught before any SQL runs, so
// a rejected event leaves the caller's transaction — including its sequence
// counters — exactly as it was.
var ErrInvalidEvent = errors.New("runstore: invalid event")

// ErrEventImmutable means a statement tried to UPDATE or DELETE an event row.
// The event stream is the record of what happened (§19.3/§27.4): a correction
// is a new event, never an edit, so the triggers refuse both and the store
// reports that refusal as this error. The store exposes no update or delete
// path; reaching this means a statement was issued by hand.
var ErrEventImmutable = errors.New("runstore: event is immutable")

// ErrOutboxDeadLetterRetained means a DELETE against an outbox row was refused
// because the row is dead_letter. A failed notification is itself a fact an
// operator must be able to find and replay (§19.1 "dead-letter 不删除"), and the
// event it carries must never be erasable from the fact stream. Delivered rows
// are still deletable: retention (T12.02) needs a way to trim them.
var ErrOutboxDeadLetterRetained = errors.New("runstore: dead-letter delivery is retained")

// ErrInvalidCursor means a replay cursor is not a position this scope can have:
// it is negative, or it is larger than the scope's high watermark (§27.4
// "after > current_high_watermark 返回 422 invalid_cursor"). The caller is
// asking about a sequence number that was never allocated, so there is nothing
// to replay and no snapshot to fall back on; the API layer answers 422.
var ErrInvalidCursor = errors.New("runstore: invalid cursor")

// ErrCursorExpired means a replay cursor is older than the history this
// database still holds: after < retention_floor-1, i.e. the events the caller
// wants to resume from have been trimmed (§27.4 "after < retention_floor-1
// 返回 410"). Retrying the same cursor cannot help - the front end must replace
// its cache with a snapshot and resume from the returned one - so the API layer
// answers 410 and carries snapshot_url. Note that after = retention_floor-1 is
// legal: it is the position immediately before the oldest retained event, which
// is exactly the position a client that has fully caught up on the retained
// history holds.
var ErrCursorExpired = errors.New("runstore: cursor expired")

// ErrInvalidDispatcherConfig means NewDispatcher (or Dispatcher.Run) was handed
// a configuration that cannot work: a blank owner, no destination to serve, a
// nil deliverer, a negative lease/backoff/attempt bound, a non-positive run
// interval. These are programmer errors and are reported before any row is
// touched, so a misconfigured dispatcher never takes a lease it could not
// release.
var ErrInvalidDispatcherConfig = errors.New("runstore: invalid dispatcher config")

// RevisionConflictError reports a Run CAS whose expected revision no longer
// matches the stored one. It carries both sides so a caller can decide whether
// to re-read and retry, and it wraps ErrRevisionConflict.
type RevisionConflictError struct {
	RunID         string
	Expected      int64
	Current       int64
	CurrentStatus run.RunStatus
}

func (e *RevisionConflictError) Error() string {
	return fmt.Sprintf("runstore: run %s revision conflict: expected %d, current %d (status %s)",
		e.RunID, e.Expected, e.Current, e.CurrentStatus)
}

// Unwrap makes errors.Is(err, ErrRevisionConflict) true.
func (e *RevisionConflictError) Unwrap() error { return ErrRevisionConflict }

// TaskRevisionConflictError is the Task counterpart of RevisionConflictError.
// Task status CAS has a different terminal rule (failed is retryable,
// completed/cancelled are not), so it carries its own status type rather than
// overloading the run one.
type TaskRevisionConflictError struct {
	TaskID        string
	Expected      int64
	Current       int64
	CurrentStatus run.TaskStatus
}

func (e *TaskRevisionConflictError) Error() string {
	return fmt.Sprintf("runstore: task %s revision conflict: expected %d, current %d (status %s)",
		e.TaskID, e.Expected, e.Current, e.CurrentStatus)
}

// Unwrap makes errors.Is(err, ErrRevisionConflict) true for tasks too: from a
// caller's point of view, "someone else moved the row first" is one condition
// whatever the row is.
func (e *TaskRevisionConflictError) Unwrap() error { return ErrRevisionConflict }

// The RAISE message bodies used by migrations 001 and 002. They are part of the
// schema contract: schema_test.go asserts the messages, and the store maps them
// to typed errors. Keeping the names in one const block makes a rename a
// deliberate two-file change instead of a silent mismatch.
const (
	errNameRunFrozenInputImmutable   = "run_frozen_input_immutable"
	errNameRunTerminalImmutable      = "run_terminal_immutable"
	errNameRunRevisionNotIncremented = "run_revision_not_incremented"
	errNameTaskInputImmutable        = "task_input_immutable"
	errNameTaskTerminalImmutable     = "task_terminal_immutable"
	errNameAttemptIdentityImmutable  = "attempt_identity_immutable"
	errNameAttemptTerminalImmutable  = "attempt_terminal_immutable"
	errNameInputSnapshotImmutable    = "input_snapshot_immutable"
	errNameRunInputSnapshotMismatch  = "run_input_snapshot_mismatch"
	errNameRunHistoryDeleteForbidden = "run_history_delete_forbidden"
	errNameEventImmutable            = "event_immutable"
	errNameOutboxDeadLetterRetained  = "outbox_dead_letter_retained"
)

// mapConstraintError turns a database refusal into the store's typed error. op
// names the operation for context; the mapping itself is decided only by the
// stable RAISE name, so a new call site cannot get a different error class by
// accident.
//
// Anything unrecognised is the database reporting a constraint the caller
// really violated (a CHECK, a foreign key, a partial unique index). Those are
// returned wrapped with the operation for diagnosis rather than flattened,
// because the store cannot classify them all and must not pretend to.
func mapConstraintError(op string, err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, errNameRunTerminalImmutable),
		strings.Contains(msg, errNameTaskTerminalImmutable),
		strings.Contains(msg, errNameAttemptTerminalImmutable):
		return fmt.Errorf("%s: %w", op, ErrTerminalImmutable)
	case strings.Contains(msg, errNameRunInputSnapshotMismatch):
		// The run named a snapshot that is absent, foreign to its project, or
		// not the body that hashes to the pinned hash.
		return fmt.Errorf("%s: %w", op, ErrInputSnapshotMismatch)
	case strings.Contains(msg, errNameRunHistoryDeleteForbidden):
		return fmt.Errorf("%s: %w", op, ErrHistoryDeleteForbidden)
	case strings.Contains(msg, errNameInputSnapshotImmutable),
		strings.Contains(msg, errNameRunFrozenInputImmutable),
		strings.Contains(msg, errNameTaskInputImmutable):
		// A stored snapshot body is frozen input exactly like a run's pin or a
		// task's input: rewriting it in place is the same class of mistake.
		return fmt.Errorf("%s: %w", op, ErrFrozenInputImmutable)
	case strings.Contains(msg, errNameRunRevisionNotIncremented),
		strings.Contains(msg, errNameAttemptIdentityImmutable):
		// The store always writes revision = revision + 1 and never rewrites an
		// attempt's identity, so reaching either trigger means the store (or a
		// caller composing statements by hand) built an invalid update.
		return fmt.Errorf("%s: %w", op, ErrInvalidRecord)
	case strings.Contains(msg, errNameEventImmutable):
		// A fact cannot be rewritten or erased. The store has no update/delete
		// path for events, so this is either a hand-written statement or a bug.
		return fmt.Errorf("%s: %w", op, ErrEventImmutable)
	case strings.Contains(msg, errNameOutboxDeadLetterRetained):
		// The row records an abandoned delivery that an operator must still be
		// able to find and replay; deleting it would erase that fact.
		return fmt.Errorf("%s: %w", op, ErrOutboxDeadLetterRetained)
	default:
		return fmt.Errorf("%s: %w", op, err)
	}
}
