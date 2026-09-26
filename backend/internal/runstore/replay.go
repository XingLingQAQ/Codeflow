// Replay: paged reads of the fact stream with their snapshot boundaries
// (T1.05.b).
//
// ListProjectEvents and ListRunEvents (T1.05.a) answer "give me the next page".
// A subscriber reconnecting after a disconnect needs three more things at the
// same time (§27.4):
//
//   - the page, on the scope's own counter (project_seq for a project
//     subscriber, run_seq for a run subscriber — the two are never mixed);
//   - the high watermark H: the highest sequence the scope has handed out, so
//     the client knows how far the replay reaches and can switch to live
//     events at > H;
//   - the retention floor F: the oldest sequence still retained, so the client
//     can tell that its cache has a hole rather than a gap-free stream, and
//     replace the cache with a snapshot (after < F-1 → 410, §27.4).
//
// The page and the two boundaries must come from one snapshot. Reading the
// watermark in a second statement would let a concurrent append land between
// them, and the client would be handed a page that is missing an event it was
// told exists (or a watermark behind the last event it received). So the three
// are read by ONE statement: the boundaries are two scalar subqueries joined to
// the page as a single guaranteed row, and SQLite evaluates a statement against
// one consistent snapshot regardless of how many appends commit while it runs.
// That is also why this file takes a Querier rather than opening a transaction:
// a read transaction it opened itself could not be combined with the caller's
// (Replay is not the only reader), and the store's write transactions are BEGIN
// IMMEDIATE — taking the write lock to read would block the appends the
// dispatcher exists to serve.
//
// The boundaries themselves:
//
//   - high_watermark is the scope counter's value, i.e. the last sequence ever
//     allocated for the scope (0 when the scope never had an event). That is
//     the number §27.4 means by "current_high_watermark": a cursor beyond it
//     names a sequence that was never allocated, so it is invalid (422) rather
//     than empty. While nothing has been trimmed it equals the largest
//     sequence in the table.
//   - retention_floor is the smallest sequence still retained for the scope.
//     When no event of the scope is left it is high_watermark+1: 1 for a
//     scope that was never written to, and one past the watermark for a scope
//     whose whole history was trimmed. The second case must not fall back to
//     1, or after=0 would be "legal" and return an empty page, and a client
//     would switch to live events believing it had missed nothing. Events
//     cannot be deleted today (003's trigger), so the floor equals the first
//     event's sequence; T12.02's retention makes it advance, and RetentionHold
//     is what stops it from passing an undelivered fact.

package runstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// ReplayPage is one consistent slice of a scope's timeline.
//
// Events are ordered by the scope's sequence, strictly increasing. NextAfter is
// the cursor to pass to the next call: the last event's sequence, or the
// request's after when the page is empty, so a client can always continue from
// NextAfter without special cases. HasMore reports whether the page was cut
// short by limit rather than by the end of the retained history.
type ReplayPage struct {
	Events         []Event
	NextAfter      int64
	HasMore        bool
	HighWatermark  int64
	RetentionFloor int64
}

// ReplayProject replays a project timeline on project_seq (§27.4: the number
// the response's "sequence" field carries for a project-scoped subscriber).
//
// after = 0 with a retention floor of 1 is the legal first replay (§20.2).
// limit follows the list reads: <= 0 means DefaultEventLimit, above
// MaxEventLimit is clamped.
//
// Errors:
//
//   - after < 0, or after > the scope's high watermark → ErrInvalidCursor
//     (the API layer answers 422 invalid_cursor);
//   - after < retention_floor-1 → ErrCursorExpired (410: the events the cursor
//     points at are no longer retained, and only a snapshot can recover).
//
// On the two cursor errors the returned page carries no events but does carry
// HighWatermark, RetentionFloor and NextAfter=after, read by the same statement
// that judged the cursor, so the caller can answer 410/422 with the numbers that
// decided it instead of re-reading them in a second snapshot.
func ReplayProject(ctx context.Context, q Querier, projectID string, after int64, limit int) (ReplayPage, error) {
	return replayScope(ctx, q, "replay project events "+projectID,
		projectScope(projectID), "project_id", "project_seq", projectID, after, limit)
}

// ReplayRun is ReplayProject over a run timeline: it pages on run_seq, the
// counter a run-scoped subscriber follows. The events it returns are the same
// rows the project view returns, with their project_seq intact — the caller
// projects whichever number its scope asked for, and the two cursors advance
// independently (§27.4 "不混用两种计数").
func ReplayRun(ctx context.Context, q Querier, runID string, after int64, limit int) (ReplayPage, error) {
	return replayScope(ctx, q, "replay run events "+runID,
		runScope(runID), "run_id", "run_seq", runID, after, limit)
}

// replayScope is the shared body of the two replay reads.
//
// scopeColumn/seqColumn and scopeID select the projection (project_id/
// project_seq or run_id/run_seq) and must come from this package's own callers,
// never from a request body: §27.4 requires the server to derive the scope from
// the authoritative resource rather than to re-interpret a cursor under a
// filter the client supplied.
func replayScope(ctx context.Context, q Querier, op, scope, scopeColumn, seqColumn, scopeID string, after int64, limit int) (ReplayPage, error) {
	if after < 0 {
		return ReplayPage{}, fmt.Errorf("%w: %s: after %d must be >= 0", ErrInvalidCursor, op, after)
	}
	limit = clampEventLimit(limit)

	// One statement, three reads (§27.4 "快照和 cursor 同一个 SQL snapshot"):
	//
	//   b  - one row, always: the scope's high watermark and retention floor.
	//        COALESCE turns "no counter row" into 0 and "no retained event"
	//        into watermark+1: 1 for a scope never written to (the legal first
	//        cursor, after=0 with floor=1), one past the watermark for a scope
	//        whose history was trimmed entirely (every older cursor expires).
	//   p  - the page: the next limit+1 events of the scope, oldest first. One
	//        extra row is fetched and dropped so has_more does not need a
	//        second scan (§27.3's bounding rule, same as listEvents).
	//   LEFT JOIN - keeps the boundaries visible even when the page is empty;
	//        an inner join would return no row at all and the cursor checks
	//        would have nothing to compare against.
	//
	// p.* expands to eventColumns in order, because that is exactly what the
	// page subquery selects; scanReplayPage depends on that order, and
	// TestReplayPageMatchesGetEvent pins it against GetEvent.
	query := `SELECT p.*, b.high_watermark, b.retention_floor
	          FROM (SELECT COALESCE((SELECT value FROM scope_counters WHERE scope = ?), 0) AS high_watermark,
	                       COALESCE((SELECT MIN(` + seqColumn + `) FROM events WHERE ` + scopeColumn + ` = ?),
	                                COALESCE((SELECT value FROM scope_counters WHERE scope = ?), 0) + 1) AS retention_floor) b
	          LEFT JOIN (SELECT ` + eventColumns + `
	                     FROM events
	                     WHERE ` + scopeColumn + ` = ? AND ` + seqColumn + ` > ?
	                     ORDER BY ` + seqColumn + `
	                     LIMIT ?) p ON 1
	          ORDER BY p.` + seqColumn

	rows, err := q.QueryContext(ctx, query, scope, scopeID, scope, scopeID, after, limit+1)
	if err != nil {
		return ReplayPage{}, fmt.Errorf("%s: %w", op, err)
	}
	defer rows.Close()

	page := ReplayPage{NextAfter: after}
	seenBoundary := false
	var events []Event
	for rows.Next() {
		event, present, watermark, floor, err := scanReplayPage(rows)
		if err != nil {
			return ReplayPage{}, fmt.Errorf("%s: %w", op, err)
		}
		if !seenBoundary {
			seenBoundary = true
			page.HighWatermark = watermark
			page.RetentionFloor = floor
		}
		if !present {
			// The boundaries-only row of an empty page.
			continue
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return ReplayPage{}, fmt.Errorf("%s: %w", op, err)
	}
	if !seenBoundary {
		// The bounds subquery always produces exactly one row; reaching here
		// means the statement was changed in a way that broke that guarantee,
		// and the cursor checks below would silently compare against zero.
		return ReplayPage{}, fmt.Errorf("%s: no watermark row returned", op)
	}

	// The boundaries that judged a bad cursor are returned with the error (and
	// no events), so a 410/422 answer quotes the same snapshot.
	bounds := ReplayPage{NextAfter: after, HighWatermark: page.HighWatermark, RetentionFloor: page.RetentionFloor}
	switch {
	case after > page.HighWatermark:
		// A sequence this scope never allocated. Not "no new events": the
		// cursor is wrong, and answering with an empty page would make a
		// client that mixed up two scopes believe it is up to date.
		return bounds, fmt.Errorf("%w: %s: after %d is past the high watermark %d",
			ErrInvalidCursor, op, after, page.HighWatermark)
	case after < page.RetentionFloor-1:
		// The cursor points at history that is no longer here. after =
		// retention_floor-1 is legal: it is the position just before the
		// oldest retained event.
		return bounds, fmt.Errorf("%w: %s: after %d is below the retention floor %d",
			ErrCursorExpired, op, after, page.RetentionFloor)
	}

	page.HasMore = len(events) > limit
	if page.HasMore {
		events = events[:limit]
	}
	page.Events = events
	if n := len(events); n > 0 {
		page.NextAfter = sequenceOf(events[n-1], seqColumn)
	}
	return page, nil
}

// sequenceOf returns the sequence of an event on the scope's own counter. The
// column name is this package's, not a caller's, so a run-scoped replay can
// never hand back a project_seq by mistake.
func sequenceOf(event Event, seqColumn string) int64 {
	if seqColumn == "run_seq" {
		if event.RunSeq == nil {
			// Cannot happen: the page is filtered on run_id, and 003's CHECK
			// ties run_id and run_seq together.
			return 0
		}
		return *event.RunSeq
	}
	return event.ProjectSeq
}

// scanReplayPage reads one row of the replay statement: eventColumns in order,
// then the two boundaries.
//
// The event columns are NULL on the boundaries-only row of an empty page, so
// this scans through nullable holders and reports whether an event was present.
// The mapping from holder to Event mirrors scanEvent; TestReplayPageMatchesGetEvent
// compares the two field by field so they cannot drift apart.
func scanReplayPage(row scanner) (Event, bool, int64, int64, error) {
	var (
		id          sql.NullString
		projectID   sql.NullString
		projectSeq  sql.NullInt64
		runID       sql.NullString
		runSeq      sql.NullInt64
		attemptID   sql.NullString
		eventType   sql.NullString
		schemaVer   sql.NullInt64
		occurredAt  sql.NullInt64
		identity    sql.NullString
		payload     sql.NullString
		payloadHash sql.NullString
		watermark   sql.NullInt64
		floor       sql.NullInt64
	)
	if err := row.Scan(&id, &projectID, &projectSeq, &runID, &runSeq, &attemptID, &eventType,
		&schemaVer, &occurredAt, &identity, &payload, &payloadHash, &watermark, &floor); err != nil {
		return Event{}, false, 0, 0, err
	}
	if !watermark.Valid || !floor.Valid {
		return Event{}, false, 0, 0, fmt.Errorf("replay row has no watermark/floor (the statement must always return them)")
	}
	if !id.Valid {
		return Event{}, false, watermark.Int64, floor.Int64, nil
	}
	event := Event{
		ID:            id.String,
		ProjectID:     projectID.String,
		ProjectSeq:    projectSeq.Int64,
		RunID:         fromNullString(runID),
		RunSeq:        fromNullInt64(runSeq),
		AttemptID:     fromNullString(attemptID),
		Type:          eventType.String,
		SchemaVersion: int(schemaVer.Int64),
		OccurredAt:    fromUnixMilli(occurredAt.Int64),
		Identity:      json.RawMessage(identity.String),
		Payload:       json.RawMessage(payload.String),
		PayloadHash:   payloadHash.String,
	}
	return event, true, watermark.Int64, floor.Int64, nil
}
