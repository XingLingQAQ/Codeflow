// Legacy projection: the runtime side of the pre-3.0 Flow bridge (T1.05.c).
//
// §19.3 and §27.1 item 4 keep the legacy Flow store and codeflow.db in two
// files until T3.01, so a Flow write and its projection cannot share one
// transaction. The bridge has two halves:
//
//   - the source half (internal/floweng, T1.05.c group 2) writes the Flow row
//     and a local outbox row in one transaction of the *source* database, and a
//     projector claims those rows and calls this file;
//   - this half makes the projection idempotent. A projector runs at least
//     once, so it re-sends the same source event after every crash, timeout and
//     retry. The runtime must therefore turn "the same source event" into
//     exactly one event row, one project sequence and one set of outbox rows,
//     however many times it is asked.
//
// The identity of a source event is (source_store, source_event_id), recorded
// in legacy_event_sources (migration 005). ProjectLegacyEventTx checks it
// inside the caller's transaction:
//
//   - already present → return the event stored the first time, created=false,
//     and touch nothing else: no sequence is allocated, no outbox row is
//     created. That is what keeps project_seq gap-free under an at-least-once
//     producer (§19.1 "序号事务分配").
//   - absent → AppendEventTx (event + sequences + outbox rows) and the mapping
//     row in the same transaction, created=true. A rollback therefore leaves
//     neither, the counter is not consumed, and the next attempt creates the
//     event as if the failed one had never run.
//
// Concurrency: two stores projecting the same source event at the same time are
// serialised by OpenStore's BEGIN IMMEDIATE, so one sees the mapping and returns
// created=false. Correctness does not depend on that: the mapping's primary key
// is the arbiter, and a loser that hits the conflict while the winner's
// transaction was still uncommitted gets ErrLegacyProjectionRaced and can retry
// — the retry then takes the "already present" path and returns the winner's
// event. Exactly one caller ever sees created=true, which is what the projector
// uses to decide whether it created a new fact or re-sent an old one.
//
// The type of the projected event is not this file's business: any type
// AppendEventTx accepts can be projected, and the check is the same closed enum.
// Today the only caller is the legacy Flow projector, which uses
// legacy.flow_event (contract amendment CA-2, §26.31): project-scoped, run_id
// absent, actor system, with the legacy event name in payload.source_type and
// the source identity in payload.source_store / payload.source_event_id. When
// T3.01 moves the Flow domain into this database, the projection switches to
// the real event types and the already-projected rows stay as history.

package runstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Legacy source limits, matching the CHECK constraints of
// legacy_event_sources (migration 005). They are enforced in Go as well so an
// oversized or blank source is refused before any SQL runs, which is what keeps
// a rejected call from leaving a partial write behind.
const (
	// MaxLegacySourceStoreLength is the maxLength of source_store.
	MaxLegacySourceStoreLength = 64
	// MaxLegacySourceEventIDLength is the maxLength of source_event_id.
	MaxLegacySourceEventIDLength = 128
)

// LegacySource names one event in a legacy store. It is the de-duplication key
// of the whole projection: the same pair must always resolve to the same runtime
// event.
//
// The two fields are a pair because the identity is per source store — two
// legacy databases may both hold an event called "ev-1", and treating those as
// one would silently drop a timeline.
type LegacySource struct {
	// Store names the legacy database the event came from (e.g. "floweng").
	// Required, at most MaxLegacySourceStoreLength characters, and stored
	// exactly as given: surrounding whitespace is a caller bug, not something
	// this package trims away, because " floweng" and "floweng" must not both
	// be accepted as the same store under one spelling and different keys under
	// another.
	Store string
	// EventID is the source event's own id, exactly as the legacy store
	// recorded it. Required, at most MaxLegacySourceEventIDLength characters,
	// no surrounding whitespace.
	EventID string
}

// ProjectLegacyEventTx projects one legacy source event into the runtime event
// stream, inside the transaction the caller passes in.
//
// It returns the runtime event and whether this call created it:
//
//   - created = true: the source event had never been projected, so in.Event was
//     appended (id, project_seq, run_seq, schema_version and payload_hash
//     server-allocated by AppendEventTx) and the mapping row was written in the
//     same transaction.
//   - created = false: the source event was already projected, so the event
//     returned is the one stored the first time and *nothing else happened*: no
//     sequence was allocated, no outbox row was created, and in is ignored apart
//     from the conflict check below. A projector that re-sends an event after a
//     crash gets the original fact back and can report success.
//
// The function never opens a transaction of its own. That is the point of the
// signature: the mapping row and the event must commit or roll back together,
// and the caller owns the transaction (Store.WithTx). A variant that committed
// on its own could write the mapping without the event (or the reverse) and the
// projection would lose the very de-duplication it exists for.
//
// Validation, all of it before any SQL runs so a rejected call leaves the
// caller's transaction untouched:
//
//   - src must name a store and an event id: both non-empty, within the limits
//     above, and without surrounding whitespace. A violation wraps
//     ErrInvalidLegacySource.
//   - in must be an event AppendEventTx accepts (closed enum type, identity that
//     agrees with the project and run, payload limits). A violation wraps
//     ErrInvalidEvent, exactly as a direct append would.
//
// Conflict rule: if the source event is already projected and the event stored
// the first time has a different project_id or type than in, the call fails with
// ErrLegacySourceConflict instead of returning the stored event. One source
// event cannot legitimately become two different runtime facts, and silently
// returning another project's event would make the projector advance its local
// outbox while the fact it believed it had projected was never written. The
// caller must treat this as a bug in the producer or in the source store, not as
// a retryable delivery failure: dead-letter the source row with this error.
//
// The one retryable outcome is ErrLegacyProjectionRaced (the mapping insert lost
// a race with another writer); it is a separate sentinel so a caller can tell
// "retry" from "do not retry" with errors.Is.
//
// The event id is deliberately not compared: a caller that does not know the id
// yet cannot be asked to match it, and the id is the runtime's own allocation.
// Everything the caller does control — which project and which kind of fact —
// must agree.
func ProjectLegacyEventTx(ctx context.Context, tx Tx, src LegacySource, in EventInput) (Event, bool, error) {
	if tx == nil {
		return Event{}, false, fmt.Errorf("%w: nil Tx", ErrInvalidLegacySource)
	}
	if err := validateLegacySource(src); err != nil {
		return Event{}, false, err
	}

	// The already-projected check runs first so a duplicate costs one indexed
	// lookup and no writes at all. It is a read inside the caller's transaction,
	// so it sees the caller's own uncommitted projection — the case of a caller
	// that projects the same source event twice in one unit of work.
	existing, found, err := LegacyEventFor(ctx, tx, src)
	if err != nil {
		return Event{}, false, err
	}
	if found {
		event, err := GetEvent(ctx, tx, existing)
		if err != nil {
			return Event{}, false, fmt.Errorf("project legacy event %s/%s: %w", src.Store, src.EventID, err)
		}
		if err := checkLegacyEventMatches(src, event, in); err != nil {
			return Event{}, false, err
		}
		return event, false, nil
	}

	event, err := AppendEventTx(ctx, tx, in)
	if err != nil {
		return Event{}, false, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO legacy_event_sources (source_store, source_event_id, event_id, projected_at)
		VALUES (?, ?, ?, ?)`,
		src.Store, src.EventID, event.ID, unixMilli(time.Now()),
	); err != nil {
		// The primary key conflict is the one refusal a caller can act on: the
		// source event was projected by another writer whose transaction was
		// still uncommitted when the check above ran. It is reported as the
		// projection conflict, not as a raw constraint error, because the caller
		// must not see this as "the source event is broken" — a retry takes the
		// already-present path and returns the other writer's event.
		//
		// Note that the caller's own transaction still holds the event and the
		// sequence it allocated; a caller that retries must roll this transaction
		// back first (Store.WithTx does that when the callback returns the
		// error), which is why the retry sees no partial write.
		if isLegacySourceConflict(err) {
			return Event{}, false, fmt.Errorf("%w: %s/%s",
				ErrLegacyProjectionRaced, src.Store, src.EventID)
		}
		return Event{}, false, mapConstraintError("project legacy event "+src.Store+"/"+src.EventID, err)
	}

	return event, true, nil
}

// LegacyEventFor resolves a source event to the runtime event it became. found
// is false when the source event has never been projected, which is not an
// error: it is the normal state of the first projection and of every source
// event a projector has not reached yet.
//
// It takes a Querier, so a caller can ask through the pool (what does this
// source event map to?) or inside its own transaction (ProjectLegacyEventTx's
// own check, which must see the caller's uncommitted projection).
func LegacyEventFor(ctx context.Context, q Querier, src LegacySource) (string, bool, error) {
	if q == nil {
		return "", false, fmt.Errorf("%w: nil Querier", ErrInvalidLegacySource)
	}
	if err := validateLegacySource(src); err != nil {
		return "", false, err
	}
	var eventID string
	err := q.QueryRowContext(ctx, `
		SELECT event_id FROM legacy_event_sources WHERE source_store = ? AND source_event_id = ?`,
		src.Store, src.EventID).Scan(&eventID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("look up legacy source %s/%s: %w", src.Store, src.EventID, err)
	default:
		return eventID, true, nil
	}
}

// validateLegacySource enforces the source identity contract before any SQL
// runs: both fields non-empty, within the column limits, and exactly as given
// (no surrounding whitespace).
//
// The whitespace rule is stricter than the schema's length CHECK on purpose.
// "floweng" and " floweng " are different byte strings, so they are different
// rows and different identities; accepting both spellings while intending one
// store would let a projector create two runtime events for one source fact, and
// the collision would only surface as a duplicated timeline much later. A caller
// that wants to normalize must do it before calling.
func validateLegacySource(src LegacySource) error {
	if err := checkLegacySourceField("source_store", src.Store, MaxLegacySourceStoreLength); err != nil {
		return err
	}
	return checkLegacySourceField("source_event_id", src.EventID, MaxLegacySourceEventIDLength)
}

// checkLegacySourceField applies the one field rule both halves of the identity
// share: non-blank, at most max bytes, and unchanged by trimming.
func checkLegacySourceField(field, value string, max int) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%w: %s is blank", ErrInvalidLegacySource, field)
	}
	if len(value) > max {
		return fmt.Errorf("%w: %s is %d bytes, limit is %d",
			ErrInvalidLegacySource, field, len(value), max)
	}
	if strings.TrimSpace(value) != value {
		return fmt.Errorf("%w: %s %q has surrounding whitespace; the identity is stored verbatim",
			ErrInvalidLegacySource, field, value)
	}
	return nil
}

// checkLegacyEventMatches refuses a source event whose stored projection is a
// different fact than the caller is now projecting: a different project or a
// different event type.
//
// Both differences mean the source id is being reused, which the design does not
// allow. Returning the stored event instead would be worse than an error: the
// projector would mark the source row projected and the fact the caller believed
// it had written would never exist.
func checkLegacyEventMatches(src LegacySource, stored Event, in EventInput) error {
	switch {
	case stored.ProjectID != in.ProjectID:
		return fmt.Errorf("%w: %s/%s was projected into project %q, but this call projects project %q",
			ErrLegacySourceConflict, src.Store, src.EventID, stored.ProjectID, in.ProjectID)
	case stored.Type != in.Type:
		return fmt.Errorf("%w: %s/%s was projected as type %q, but this call projects type %q",
			ErrLegacySourceConflict, src.Store, src.EventID, stored.Type, in.Type)
	default:
		return nil
	}
}

// isLegacySourceConflict reports whether err is the mapping table's primary key
// refusal, i.e. another writer projected the same source event concurrently.
//
// It matches on the SQLite message rather than on an extended result code so
// this package keeps its single-driver dependency (dbx) and does not import the
// driver: the message text of a UNIQUE/PK refusal is stable and the table and
// index names appear in it, which is the same evidence a test can assert.
func isLegacySourceConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "legacy_event_sources") &&
		(strings.Contains(msg, "UNIQUE constraint failed") ||
			strings.Contains(msg, "PRIMARY KEY must be unique"))
}
