package floweng

import (
	"database/sql"
	"fmt"
	"time"
	"unicode/utf8"
)

// Local outbox access for the legacy Flow store (T1.05.c, §19.3/§27.1).
//
// Put writes the outbox rows in the same transaction as the flows row. This
// file is the read/advance side the projector drives: claim the due rows of a
// Flow in local order, then write back one of three outcomes — projected,
// retry with backoff, or dead letter. Every write-back is a compare-and-set on
// state='pending', so a projector that lost a race (or was restarted) cannot
// overwrite the result another round already recorded.
//
// Only one projector instance may run against a given floweng.db. There is no
// lease here (the local outbox is single-process by construction until T3.01
// moves the Flow into the runtime database), so the wiring in T1.04 must start
// exactly one; the CAS below makes a duplicate harmless rather than correct:
// the duplicate's Target call would be absorbed by the target's own
// source_event_id idempotency, not by this file.

// LegacyOutboxEvent is one claimed pending event: the fields the projector needs
// to build the runtime event, in the legacy vocabulary.
type LegacyOutboxEvent struct {
	// Seq is the local append order inside this database. The projector uses it
	// only for ordering; the identity of the event is SourceEventID.
	Seq int64
	// SourceEventID is the FlowEvent.ID this row was queued from. It is the
	// de-duplication key of the whole projection: the runtime side must treat
	// the same SourceEventID as one event, however many times it is projected.
	SourceEventID string
	FlowID        string
	ProjectID     string
	// Type is the legacy event name (flow.created, stage.done, gate.approved,
	// ...). §27.1 keeps the legacy name in the runtime payload as source_type
	// instead of growing the runtime event enum a second Flow vocabulary.
	Type    string
	StageID string
	Message string
	// OccurredAt is the FlowEvent.Timestamp, as stored (Unix milliseconds UTC).
	OccurredAt time.Time
	// Attempts is the row's attempt_count at claim time, i.e. how many attempts
	// have already been made (the row is not modified by a claim).
	Attempts int
}

// MaxLegacyLastErrorBytes bounds what one failure may store in last_error.
// The text is for an operator reading the outbox, not a log: a target that
// answers with a whole HTML page must not grow the row without bound. The cut is
// rune-safe so the stored text stays valid UTF-8.
const MaxLegacyLastErrorBytes = 512

// truncateLegacyLastError cuts msg to at most MaxLegacyLastErrorBytes bytes on a
// rune boundary.
func truncateLegacyLastError(msg string) string {
	if len(msg) <= MaxLegacyLastErrorBytes {
		return msg
	}
	cut := MaxLegacyLastErrorBytes
	for cut > 0 && !utf8.RuneStart(msg[cut]) {
		cut--
	}
	return msg[:cut]
}

// DueLegacyEvents returns up to limit pending events that are due at now, in
// local order, without modifying anything.
//
// The claim rule is "each Flow is projected strictly in its local order": a
// pending row is returned only when its Flow has no earlier pending row, and
// the rows are taken in seq order. A Flow whose head is retrying (its
// next_attempt_at is in the future) therefore contributes nothing at all — its
// later events wait behind the head, because the runtime timeline must not
// receive event 5 before event 4. Other Flows are unaffected: the rule is per
// flow_id, not global. A dead-lettered head stops blocking its Flow, which is
// what lets a Flow whose one poisonous event was given up on still make
// progress (and is why dead_letter rows are kept, not deleted).
//
// Because the claim does not modify rows, a crash between the claim and the
// write-back costs nothing: the same rows come back next round and the target
// absorbs the duplicate by SourceEventID.
func (s *SQLiteFlowStore) DueLegacyEvents(now time.Time, limit int) ([]LegacyOutboxEvent, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("legacy outbox: limit %d must be positive", limit)
	}
	nowMS := now.UTC().UnixMilli()
	rows, err := s.db.Query(`
SELECT o.seq, o.source_event_id, o.flow_id, o.project_id, o.event_type,
       o.stage_id, o.message, o.occurred_at, o.attempt_count
  FROM flow_event_outbox o
 WHERE o.state = 'pending'
   AND o.next_attempt_at IS NOT NULL AND o.next_attempt_at <= ?
   AND NOT EXISTS (
         SELECT 1 FROM flow_event_outbox h
          WHERE h.flow_id = o.flow_id AND h.state = 'pending' AND h.seq < o.seq)
 ORDER BY o.seq
 LIMIT ?`, nowMS, limit)
	if err != nil {
		return nil, fmt.Errorf("legacy outbox: select due events: %w", err)
	}
	defer rows.Close()

	out := make([]LegacyOutboxEvent, 0, limit)
	for rows.Next() {
		var (
			ev         LegacyOutboxEvent
			occurredAt int64
		)
		if err := rows.Scan(&ev.Seq, &ev.SourceEventID, &ev.FlowID, &ev.ProjectID, &ev.Type,
			&ev.StageID, &ev.Message, &occurredAt, &ev.Attempts); err != nil {
			return nil, fmt.Errorf("legacy outbox: scan due event: %w", err)
		}
		ev.OccurredAt = time.UnixMilli(occurredAt).UTC()
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("legacy outbox: iterate due events: %w", err)
	}
	return out, nil
}

// MarkLegacyEventProjected records that sourceEventID now exists in the target
// as projectedEventID.
//
// The write-back is a compare-and-set on state='pending':
//
//   - pending → projected (projected_event_id and projected_at set, next_attempt_at
//     cleared), one row affected: success.
//   - already projected with the same projected_event_id: success. This is the
//     crash window the whole design turns on — the target committed, the
//     projector died before this call, the next round re-projects the same
//     SourceEventID, the target answers with the same event id, and the row is
//     finally marked. Reporting an error here would turn a completed delivery
//     into a retry loop.
//   - already projected with a different id: error. The same source event
//     cannot legitimately become two runtime events.
//   - dead_letter: error. The row was given up on; reviving it silently would
//     hide the decision an operator has to make.
//   - no such row: error.
func (s *SQLiteFlowStore) MarkLegacyEventProjected(sourceEventID, projectedEventID string, now time.Time) error {
	if sourceEventID == "" {
		return fmt.Errorf("legacy outbox: source event id is required")
	}
	if projectedEventID == "" {
		return fmt.Errorf("legacy outbox: projected event id is required for %s", sourceEventID)
	}
	nowMS := now.UTC().UnixMilli()

	res, err := s.db.Exec(`
UPDATE flow_event_outbox
   SET state = 'projected', projected_event_id = ?, projected_at = ?,
       next_attempt_at = NULL, last_error = NULL
 WHERE source_event_id = ? AND state = 'pending'`, projectedEventID, nowMS, sourceEventID)
	if err != nil {
		return fmt.Errorf("legacy outbox: mark %s projected: %w", sourceEventID, err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}

	var state string
	var existing sql.NullString
	err = s.db.QueryRow(`SELECT state, projected_event_id FROM flow_event_outbox WHERE source_event_id = ?`,
		sourceEventID).Scan(&state, &existing)
	if err == sql.ErrNoRows {
		return fmt.Errorf("legacy outbox: no row for source event %s", sourceEventID)
	}
	if err != nil {
		return fmt.Errorf("legacy outbox: read %s after mark: %w", sourceEventID, err)
	}
	if state == "projected" && existing.Valid && existing.String == projectedEventID {
		return nil
	}
	if state == "projected" {
		return fmt.Errorf("legacy outbox: source event %s is already projected as %s, not %s",
			sourceEventID, existing.String, projectedEventID)
	}
	return fmt.Errorf("legacy outbox: source event %s is %s, not pending", sourceEventID, state)
}

// MarkLegacyEventRetry records a failed attempt that should be tried again:
// attempt_count+1, last_error (truncated to MaxLegacyLastErrorBytes), and
// next_attempt_at = nextAttemptAt. Only a pending row is touched; a row that was
// projected or dead-lettered in the meantime keeps its outcome.
func (s *SQLiteFlowStore) MarkLegacyEventRetry(sourceEventID, lastError string, nextAttemptAt time.Time) error {
	if sourceEventID == "" {
		return fmt.Errorf("legacy outbox: source event id is required")
	}
	res, err := s.db.Exec(`
UPDATE flow_event_outbox
   SET attempt_count = attempt_count + 1, last_error = ?, next_attempt_at = ?
 WHERE source_event_id = ? AND state = 'pending'`,
		truncateLegacyLastError(lastError), nextAttemptAt.UTC().UnixMilli(), sourceEventID)
	if err != nil {
		return fmt.Errorf("legacy outbox: mark %s for retry: %w", sourceEventID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("legacy outbox: source event %s is not pending (retry ignored)", sourceEventID)
	}
	return nil
}

// MarkLegacyEventDead gives up on a pending event: attempt_count+1,
// state='dead_letter', next_attempt_at=NULL and last_error set. The row is kept
// (it is a fact an operator must be able to find) and it stops blocking the
// Flow's later events, which is what DueLegacyEvents' ordering rule relies on.
func (s *SQLiteFlowStore) MarkLegacyEventDead(sourceEventID, lastError string) error {
	if sourceEventID == "" {
		return fmt.Errorf("legacy outbox: source event id is required")
	}
	res, err := s.db.Exec(`
UPDATE flow_event_outbox
   SET state = 'dead_letter', attempt_count = attempt_count + 1,
       next_attempt_at = NULL, last_error = ?
 WHERE source_event_id = ? AND state = 'pending'`,
		truncateLegacyLastError(lastError), sourceEventID)
	if err != nil {
		return fmt.Errorf("legacy outbox: mark %s dead: %w", sourceEventID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("legacy outbox: source event %s is not pending (dead-letter ignored)", sourceEventID)
	}
	return nil
}

// LegacyOutboxBacklog reports how many rows are still pending and how many were
// dead-lettered. Projected rows are not counted: they are history, not work.
func (s *SQLiteFlowStore) LegacyOutboxBacklog() (pending, deadLetter int, err error) {
	err = s.db.QueryRow(`
SELECT
  (SELECT count(*) FROM flow_event_outbox WHERE state = 'pending'),
  (SELECT count(*) FROM flow_event_outbox WHERE state = 'dead_letter')`).Scan(&pending, &deadLetter)
	if err != nil {
		return 0, 0, fmt.Errorf("legacy outbox: backlog: %w", err)
	}
	return pending, deadLetter, nil
}
