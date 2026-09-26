// Consumer idempotency and the retention guard (T1.05.b).
//
// Two ends of the delivery contract meet in this file:
//
//   - MarkConsumedTx is the consumer's half of at-least-once delivery. The
//     dispatcher guarantees a delivery happens at least once, which means it can
//     happen twice; the consumer records which (consumer, event) pairs it has
//     already applied, so the second delivery is recognised and its side effect
//     is skipped (§19.2 "consumer + event_id 唯一；回执与派生状态同事务提交").
//   - RetentionHold is the dispatcher's protection from retention. T12.02 may
//     trim delivered history, but it must not trim a fact whose delivery never
//     landed: "retention 不删关键未投递事实" (§28 T1.05.b). The hold answers
//     "which is the oldest event of this project that still owes a delivery",
//     and the cleanup job must not delete that event or anything after it.
//
// Both are deliberately small and take the caller's transaction / querier
// rather than opening one: a receipt that committed separately from the state it
// describes would be worse than no receipt at all.

package runstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// MarkConsumedTx records that consumer has applied eventID, inside the
// caller's transaction, and reports whether this call is the first one to do so.
//
// first = true: the record was inserted, the caller should apply the event's
// side effect now.
// first = false: this consumer already applied the event (a redelivery under
// at-least-once), and the caller must skip the side effect.
//
// It must be called in the same transaction as the derived state the side
// effect writes (§19.3), which is what the two halves of the contract mean
// together:
//
//   - if the caller's transaction commits, the receipt and the derived state
//     are both durable, and the redelivery that follows a later crash is
//     skipped;
//   - if the caller's transaction rolls back, both are gone, and the next
//     delivery of the same event is applied again — an event is never silently
//     marked consumed without being consumed.
//
// The insert is one statement with ON CONFLICT DO NOTHING, not a SELECT
// followed by an INSERT: the upsert is atomic in the database, so "exactly one
// caller sees first=true" does not depend on the caller's transaction mode or
// on the store's BEGIN IMMEDIATE — the same reasoning as nextScopeSeq's upsert.
// RowsAffected is what distinguishes the two outcomes: 1 means the row was
// created, 0 means the conflict clause swallowed a duplicate.
//
// A blank consumer or eventID is refused with ErrInvalidEvent before any SQL
// runs (the store's sentinel for a record the caller got wrong), because a
// receipt filed under "" would de-duplicate every event for every anonymous
// consumer at once. A non-existent eventID is refused by the foreign key to
// events(id): a consumer cannot claim to have applied an event nobody can read.
func MarkConsumedTx(ctx context.Context, tx Tx, consumer, eventID string) (bool, error) {
	if tx == nil {
		return false, fmt.Errorf("%w: nil Tx", ErrInvalidEvent)
	}
	if strings.TrimSpace(consumer) == "" {
		return false, fmt.Errorf("%w: consumer is blank", ErrInvalidEvent)
	}
	if strings.TrimSpace(eventID) == "" {
		return false, fmt.Errorf("%w: event id is blank", ErrInvalidEvent)
	}

	res, err := tx.ExecContext(ctx, `
		INSERT INTO consumer_offsets (consumer, event_id, applied_at)
		VALUES (?, ?, ?)
		ON CONFLICT (consumer, event_id) DO NOTHING`,
		consumer, eventID, unixMilli(time.Now()))
	if err != nil {
		return false, mapConstraintError("mark event "+eventID+" consumed by "+consumer, err)
	}
	inserted, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rows affected marking event %s consumed by %s: %w", eventID, consumer, err)
	}
	return inserted == 1, nil
}

// RetentionHold reports the oldest project_seq of projectID whose event still
// owes a delivery: an event with at least one outbox row that is pending (never
// delivered) or dead_letter (delivery abandoned but retained).
//
// held = false means every event of the project that was ever queued has been
// delivered, so nothing is waiting on this project.
//
// T12.02's cleanup must not delete the event this returns, nor any event with a
// larger project_seq: the plan's rule is "retention 不删关键未投递事实" (§28
// T1.05.b). The reason is the same one that makes dead letters permanent — a
// notification that never landed is a fact an operator must be able to find and
// replay, and deleting the event would delete the only record of what was to be
// delivered.
//
// The query is intentionally an EXISTS on outbox rather than a join: the answer
// is a bound, not a list, and outbox's unique index is (event_id, destination), so
// the lookup is indexed.
//
// A blank projectID is refused (ErrInvalidEvent) instead of returning
// held=false: a retention job that asked about no project at all must fail
// closed, because "no hold" is permission to delete.
func RetentionHold(ctx context.Context, q Querier, projectID string) (int64, bool, error) {
	if strings.TrimSpace(projectID) == "" {
		return 0, false, fmt.Errorf("%w: project id is blank", ErrInvalidEvent)
	}
	var seq sql.NullInt64
	err := q.QueryRowContext(ctx, `
		SELECT MIN(e.project_seq)
		FROM events e
		WHERE e.project_id = ?
		  AND EXISTS (SELECT 1 FROM outbox o
		              WHERE o.event_id = e.id AND o.state IN ('pending', 'dead_letter'))`,
		projectID).Scan(&seq)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// MIN() over no rows yields one NULL row, never ErrNoRows; treated
			// as "nothing held" so a driver that reports it differently cannot
			// turn a missing hold into a startup failure.
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("retention hold for project %s: %w", projectID, err)
	}
	if !seq.Valid {
		return 0, false, nil
	}
	return seq.Int64, true, nil
}
