// Event store and delivery outbox (T1.05.a).
//
// This file is the fact stream the rest of the runtime library is audited
// against: every state change the plan cares about is paired with an event, and
// the pairing is one transaction (§19.3 item 1 "Run 状态迁移 + 状态事件 +
// outbox 行").
//
// The design rules this file exists to enforce:
//
//   - AppendEventTx takes a Tx and never opens one. There is deliberately no
//     AppendEvent(ctx, store, ...) shortcut: a caller that could append an
//     event on its own would be able to commit the event while the state change
//     it describes rolled back (or the reverse), which is exactly the "状态与事
//     件同时失败/同时成功" property this card must prove. The transaction
//     belongs to the caller; T1.02.b is the canonical composition
//     (UpdateRunStatusCAS + AppendEventTx inside one WithTx).
//   - Sequence numbers are allocated inside that same transaction, from
//     scope_counters. A rolled-back transaction consumes nothing, so the
//     sequence has no gaps that would mean "something happened and was undone"
//     (§19.1 "序号事务分配"; §27.4 "同事务分配 sequence").
//   - The server fills the id, the sequences and the schema version. A caller
//     supplies what happened (type, payload, identity, when, and the identity
//     and project/run the event belongs to) and the destinations to notify; it
//     cannot supply an id or a sequence (§27.4 "服务端填 identity/ID/time，不能
//     直接接受 CLI 自报 project/sequence"). The one judgement the caller makes
//     about identity is *which* project and run the event belongs to — those are
//     the caller's own scope — and the envelope it hands over must agree with
//     them, or the store refuses the event.
//   - The identity envelope is the run package's frozen contract (T1.02.a), not
//     a shape this file invents: the caller's bytes are decoded strictly into
//     run.ExecutionIdentity and checked with its ValidateFor, so the wire schema,
//     the per-event requirements and the store cannot disagree.
//
// Two sequences, two views (§27.4): every event has a project_seq, and an event
// that belongs to a run also has a run_seq. They are independent counters, and
// the same row is readable through both — ListProjectEvents and ListRunEvents
// return the same Event for the same id, with the caller's scope deciding which
// number it pages on.
//
// Canonical form: payload and identity are stored as canonical JSON (object keys
// sorted, no insignificant whitespace, numbers preserved exactly — the rule
// input_snapshots already follows). Canonical payload is what makes payload_hash
// recomputable from the stored bytes alone; canonical identity makes two
// spellings of the same identity the same stored value.

package runstore

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/codeflow/backend/internal/run"
	"github.com/google/uuid"
)

// Event types: the closed enum of schemas/execution-event.schema.json.
//
// The Go list lives in exactly one place — run.ExecutionEventType, with
// run.ExecutionEventTypes and ExecutionEventType.Valid() beside it (T1.02.a) —
// because the run package already owns the identity contract, including the
// per-event identity requirements, and two copies of the same enum would be free
// to drift. runstore imports run for the state machine anyway, so this adds no
// dependency; T1.02.b and the API layer use the same constants.
//
// The enum is still restated in the migration CHECK, deliberately: the CHECK is
// the database's last line of defence and must keep holding for a writer that
// never goes through this package. TestEventTypeEnumMatchesSchema proves all
// three lists — the JSON schema, run.ExecutionEventTypes and the migration CHECK
// — are equal in both directions and in order.

// EventSchemaVersion is the only schema_version this binary writes or reads
// (§20.2 instance: schema_version 1; §19.1 events.schema_version, whose CHECK
// pins the same value).
const EventSchemaVersion = 1

// MaxEventPayloadBytes is the per-event payload limit (§27.4 "单事件默认最大
// 256 KiB"). An oversized payload is refused, not truncated: the plan says the
// excess belongs in an artifact and the event carries a reference, so the caller
// decides how to reference it. Truncating here would silently destroy the part
// of the payload the hash was supposed to cover.
const MaxEventPayloadBytes = 256 * 1024

// Event read paging (§27.3: GET /runs/:rid/events?after=N&limit=L, "默认 L=200,
// 上限 1000"). The bounds live here rather than only at the HTTP layer so a
// caller using the store directly gets the same behaviour as the API.
const (
	DefaultEventLimit = 200
	MaxEventLimit     = 1000
)

// Event is one stored event: §19.1's column list in Go form.
//
// Identity and Payload are the canonical JSON exactly as stored, so a caller can
// recompute payload_hash or re-verify the identity without a re-encode. RunID
// and RunSeq are both nil or both set; AttemptID is nil unless the event
// happened while a specific attempt was live (§27.1).
type Event struct {
	ID            string
	ProjectID     string
	ProjectSeq    int64
	RunID         *string
	RunSeq        *int64
	AttemptID     *string
	Type          string
	SchemaVersion int
	OccurredAt    time.Time
	Identity      json.RawMessage
	Payload       json.RawMessage
	PayloadHash   string
}

// EventInput is what a caller must supply to append an event.
//
// Everything the server owns is absent by design: id, the two sequences,
// schema_version and payload_hash are filled by AppendEventTx, and occurred_at
// is the caller's fact rather than the write time. There is no field through
// which a caller could claim an id or a sequence (§27.4 "服务端填
// identity/ID/time，不能直接接受 CLI 自报 project/sequence").
type EventInput struct {
	ProjectID string
	// RunID is nil for an event that belongs to no run (a Gate decision, a
	// manual document, a system recovery event: §27.1).
	RunID *string
	// AttemptID is nil unless an attempt was live when the event happened.
	AttemptID *string
	// Type must be one of the values of run.ExecutionEventTypes.
	Type string
	// OccurredAt is when the fact happened, not when it was appended. It must
	// be non-zero; the store stores it as Unix milliseconds UTC.
	OccurredAt time.Time
	// Identity is the ExecutionIdentity envelope of
	// schemas/execution-identity.schema.json. It is decoded strictly into
	// run.ExecutionIdentity (T1.02.a): unknown keys are refused because the
	// schema declares additionalProperties:false, and the decoded value is
	// checked with ValidateFor(in.Type), which applies the per-event
	// requirements of §27.1/§28. It must agree with ProjectID, RunID and
	// AttemptID.
	Identity json.RawMessage
	// Payload is the event-family body: one JSON object, at most
	// MaxEventPayloadBytes.
	Payload json.RawMessage
	// Destinations names the deliveries to queue for this event. An empty list
	// is legal (an event nothing needs to be told about); each name becomes one
	// pending outbox row in the same transaction. Names must be non-blank and
	// distinct: one delivery per (event, destination).
	Destinations []string
}

// AppendEventTx validates the input, allocates its sequence numbers and writes
// the event plus one pending outbox row per destination — all inside the
// transaction the caller passes in.
//
// It never opens a transaction of its own, and there is no variant that does.
// That is the point of the signature: §19.3 requires the state change, the event
// and the outbox row to commit or roll back together, which is only possible if
// the caller owns the transaction and calls this from inside it.
//
// Validation happens before any SQL runs, so a rejected input leaves the
// caller's transaction exactly as it was — not even a counter row is touched:
//
//   - Type must be in the closed enum (run.ExecutionEventType.Valid()).
//   - Identity must decode strictly into run.ExecutionIdentity and pass
//     ValidateFor(Type): project_id and actor{type,id} required, actor.type one
//     of the four fixed values, every id at most 128 characters, and the fields
//     the event type requires present (a tool event needs the attempt that
//     requested it, a queued-stage claim does not, a Gate decision may have no
//     Run at all). The rejection wraps both ErrInvalidEvent and the original
//     *run.IdentityError, so a caller can report the exact field.
//   - Identity.project_id must equal ProjectID, and identity.run_id and
//     identity.attempt_id must equal RunID and AttemptID (both present or both
//     absent). The store does not trust a body that names a different project,
//     run or attempt than the caller's own scope: §27.4 requires the server to
//     derive the parent from the authoritative resource instead of believing
//     what the body claims, and a row whose identity disagrees with its own
//     project_id would let one project forge another's audit trail.
//   - Payload must be one JSON object of at most MaxEventPayloadBytes
//     (measured on the canonical bytes that will be stored).
//   - No destination may be blank or repeated.
//
// AttemptID is stored as given (non-empty, and the identity must agree about the
// attempt). It is not looked up in attempts: the legacy Flow projection (T1.05.c)
// carries attempt ids that live in the old database until T12.01 migrates that
// domain into this file.
//
// The counters are updated in the same transaction as the insert, so a rollback
// after a successful append returns the numbers to the pool: the next append
// gets the same ones, and no client ever sees a gap caused by an undone write.
func AppendEventTx(ctx context.Context, tx Tx, in EventInput) (Event, error) {
	if tx == nil {
		return Event{}, fmt.Errorf("%w: nil Tx", ErrInvalidEvent)
	}
	validated, err := validateEventInput(in)
	if err != nil {
		return Event{}, err
	}

	event := Event{
		ID:            uuid.NewString(),
		ProjectID:     in.ProjectID,
		RunID:         in.RunID,
		AttemptID:     in.AttemptID,
		Type:          in.Type,
		SchemaVersion: EventSchemaVersion,
		OccurredAt:    in.OccurredAt.UTC(),
		Identity:      json.RawMessage(validated.identity),
		Payload:       json.RawMessage(validated.payload),
		PayloadHash:   validated.payloadHash,
	}

	// Project sequence first, then run sequence. Both happen in the same
	// transaction, so the order only decides which counter a failure would have
	// left untouched had it not been rolled back — and it is rolled back.
	projectSeq, err := nextScopeSeq(ctx, tx, projectScope(in.ProjectID))
	if err != nil {
		return Event{}, err
	}
	event.ProjectSeq = projectSeq

	if in.RunID != nil {
		runSeq, err := nextScopeSeq(ctx, tx, runScope(*in.RunID))
		if err != nil {
			return Event{}, err
		}
		event.RunSeq = &runSeq
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO events (id, project_id, project_seq, run_id, run_seq, attempt_id, type,
		                    schema_version, occurred_at, identity_json, payload_json, payload_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID, event.ProjectID, event.ProjectSeq, nullableString(event.RunID), nullableInt64(event.RunSeq),
		nullableString(event.AttemptID), event.Type, event.SchemaVersion, unixMilli(event.OccurredAt),
		string(event.Identity), string(event.Payload), event.PayloadHash,
	); err != nil {
		return Event{}, mapConstraintError("append event "+event.ID, err)
	}

	// One pending delivery per destination, in the same transaction: the
	// dispatcher must be able to find the work after a crash, and the plan
	// forbids notifying anyone before the commit (§19.3 last paragraph). The
	// row is a promise to deliver; the delivery itself is T1.05.b's. The first
	// attempt is due immediately (next_attempt_at = the event's instant), so a
	// pending row is never invisible to the dispatcher's "pending and due" scan.
	for _, destination := range in.Destinations {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO outbox (id, event_id, destination, state, attempt_count, next_attempt_at, created_at)
			VALUES (?, ?, ?, 'pending', 0, ?, ?)`,
			uuid.NewString(), event.ID, destination, unixMilli(event.OccurredAt), unixMilli(event.OccurredAt),
		); err != nil {
			return Event{}, mapConstraintError("queue delivery to "+destination+" for event "+event.ID, err)
		}
	}

	return event, nil
}

// validatedEvent is the canonical form of an EventInput, produced by the
// validation pass so the canonical encoding is computed once.
type validatedEvent struct {
	identity    string
	payload     string
	payloadHash string
}

// validateEventInput checks everything that must hold before any SQL runs and
// returns the canonical bytes to store.
func validateEventInput(in EventInput) (validatedEvent, error) {
	if in.ProjectID == "" {
		return validatedEvent{}, fmt.Errorf("%w: ProjectID is empty", ErrInvalidEvent)
	}
	if !run.ExecutionEventType(in.Type).Valid() {
		return validatedEvent{}, fmt.Errorf("%w: event type %q is not in the closed enum", ErrInvalidEvent, in.Type)
	}
	if in.OccurredAt.IsZero() {
		return validatedEvent{}, fmt.Errorf("%w: OccurredAt is zero", ErrInvalidEvent)
	}
	if in.RunID != nil && *in.RunID == "" {
		return validatedEvent{}, fmt.Errorf("%w: RunID is set but empty", ErrInvalidEvent)
	}
	if in.AttemptID != nil && *in.AttemptID == "" {
		return validatedEvent{}, fmt.Errorf("%w: AttemptID is set but empty", ErrInvalidEvent)
	}

	identity, err := validateEventIdentity(in)
	if err != nil {
		return validatedEvent{}, err
	}

	// The payload must be one JSON object and fit the limit. Object, not any
	// JSON value: §20.2's envelope and every event-family payload are objects,
	// and a bare array or scalar would give each consumer a shape it cannot
	// switch on.
	payload, payloadHash, err := canonicalJSON(in.Payload)
	if err != nil {
		return validatedEvent{}, fmt.Errorf("%w: payload: %v", ErrInvalidEvent, err)
	}
	if !bytes.HasPrefix([]byte(payload), []byte("{")) {
		return validatedEvent{}, fmt.Errorf("%w: payload must be a JSON object", ErrInvalidEvent)
	}
	// Measured on the canonical bytes — what is actually stored and hashed —
	// not on the caller's raw input, so reformatting cannot smuggle a larger
	// document past the limit.
	if len(payload) > MaxEventPayloadBytes {
		return validatedEvent{}, fmt.Errorf(
			"%w: payload is %d bytes, limit is %d (store the body as an artifact and reference it)",
			ErrInvalidEvent, len(payload), MaxEventPayloadBytes)
	}

	if err := validateDestinations(in.Destinations); err != nil {
		return validatedEvent{}, err
	}

	return validatedEvent{identity: identity, payload: payload, payloadHash: payloadHash}, nil
}

// validateDestinations refuses a blank name and a repeated one.
//
// A duplicate is refused here rather than left to the UNIQUE (event_id,
// destination) index: the index would abort the transaction midway through the
// outbox writes, after the event row and the counters had already been written,
// and the caller would get a raw constraint error for what is plainly a caller
// mistake. Checking before any SQL means a rejected list leaves the transaction
// — counters included — exactly as it was.
func validateDestinations(destinations []string) error {
	seen := make(map[string]bool, len(destinations))
	for _, destination := range destinations {
		if strings.TrimSpace(destination) == "" {
			return fmt.Errorf("%w: a destination is blank", ErrInvalidEvent)
		}
		if seen[destination] {
			return fmt.Errorf("%w: destination %q is listed twice (one delivery per event and destination)",
				ErrInvalidEvent, destination)
		}
		seen[destination] = true
	}
	return nil
}

// validateEventIdentity decodes the identity, applies the run package's contract
// to it, cross-checks it against the row it is about to be written under, and
// returns the canonical identity bytes.
//
// The identity rules themselves belong to run.ExecutionIdentity (T1.02.a): the
// required fields, the 128-character limits, the actor enum, and the per-event
// requirements of §27.1/§28. This function adds the two things the store knows
// and the identity type cannot: that the envelope agrees with the caller's
// scope, and that the bytes stored are canonical.
func validateEventIdentity(in EventInput) (string, error) {
	id, err := decodeExecutionIdentity(in)
	if err != nil {
		return "", err
	}

	// Wrapped so both errors.Is(err, ErrInvalidEvent) and
	// errors.As(err, &*run.IdentityError) hold: the caller learns that the event
	// is invalid *and* which field to fix.
	if err := id.ValidateFor(in.Type); err != nil {
		return "", fmt.Errorf("%w: identity: %w", ErrInvalidEvent, err)
	}

	// An event whose identity names a different project, run or attempt than the
	// row it is written to would make the audit trail disagree with itself, and
	// §27.4 forbids trusting a self-reported parent.
	if id.ProjectID != in.ProjectID {
		return "", fmt.Errorf("%w: identity.project_id %q does not match ProjectID %q",
			ErrInvalidEvent, id.ProjectID, in.ProjectID)
	}
	if err := crossCheckIdentityID("run_id", "RunID", id.RunID, in.RunID); err != nil {
		return "", err
	}
	if err := crossCheckIdentityID("attempt_id", "AttemptID", id.AttemptID, in.AttemptID); err != nil {
		return "", err
	}

	// Store what was validated, not the caller's bytes. encoding/json matches
	// field names case-insensitively, so {"project_id":"a","PROJECT_ID":"b"}
	// decodes and validates as "b" while its raw bytes still say "a" to any
	// reader that looks the key up exactly. Re-encoding the decoded identity
	// makes the stored row say exactly what the checks above accepted.
	validated, err := json.Marshal(id)
	if err != nil {
		return "", fmt.Errorf("%w: identity: %v", ErrInvalidEvent, err)
	}
	canonical, _, err := canonicalJSON(validated)
	if err != nil {
		return "", fmt.Errorf("%w: identity: %v", ErrInvalidEvent, err)
	}
	return canonical, nil
}

// decodeExecutionIdentity decodes in.Identity into the run package's frozen
// identity contract.
//
// The decoder is strict on purpose. Unknown keys are refused because
// schemas/execution-identity.schema.json declares additionalProperties:false, so
// an extra key is a contract violation rather than a forward-compatible
// extension; trailing content after the object is refused because the store
// hashes the bytes it stored, and a second value would be silently dropped from
// the hash. A producer that needs a new field extends the schema and the run
// package first.
func decodeExecutionIdentity(in EventInput) (run.ExecutionIdentity, error) {
	if len(in.Identity) == 0 {
		return run.ExecutionIdentity{}, fmt.Errorf("%w: Identity is empty", ErrInvalidEvent)
	}
	dec := json.NewDecoder(bytes.NewReader(in.Identity))
	dec.DisallowUnknownFields()

	var id run.ExecutionIdentity
	if err := dec.Decode(&id); err != nil {
		return run.ExecutionIdentity{}, fmt.Errorf("%w: identity: %v", ErrInvalidEvent, err)
	}
	if err := requireJSONEnd(dec); err != nil {
		return run.ExecutionIdentity{}, fmt.Errorf("%w: identity: %v", ErrInvalidEvent, err)
	}
	return id, nil
}

// crossCheckIdentityID requires an identity id and the EventInput field that
// mirrors it to agree: both present with the same value, or both absent.
//
// A half pair is not a lesser mistake than a mismatch. An identity.run_id with
// no RunID would name a run the row is not indexed under, and a RunID with no
// identity.run_id would leave the two views of §27.4 disagreeing about the same
// row. The same argument holds for the attempt: an attempt the identity names
// but the row does not carry would be invisible to every reader of the event,
// and an attempt the row carries but the identity omits would be an unattributed
// execution.
func crossCheckIdentityID(field, inputField string, identityValue, inputValue *string) error {
	switch {
	case identityValue == nil && inputValue == nil:
		return nil
	case identityValue == nil:
		return fmt.Errorf("%w: %s %q is set but identity.%s is missing",
			ErrInvalidEvent, inputField, *inputValue, field)
	case inputValue == nil:
		return fmt.Errorf("%w: identity.%s %q is set but %s is empty",
			ErrInvalidEvent, field, *identityValue, inputField)
	case *identityValue != *inputValue:
		return fmt.Errorf("%w: identity.%s %q does not match %s %q",
			ErrInvalidEvent, field, *identityValue, inputField, *inputValue)
	}
	return nil
}

// projectScope is the counter key for a project's event timeline.
func projectScope(projectID string) string { return "project:" + projectID }

// runScope is the counter key for a run's event timeline.
func runScope(runID string) string { return "run:" + runID }

// nextScopeSeq allocates the next sequence number for scope inside the caller's
// transaction.
//
// One statement, not a read followed by a write: the upsert takes the row and
// returns the new value atomically, so two appends cannot be handed the same
// number even if a future caller forgets the immediate transaction. Under
// OpenStore's BEGIN IMMEDIATE they are serialised anyway; this statement means
// correctness does not depend on that.
//
// The first event of a scope gets 1, which is what §20.2 requires for the first
// replay (after=0 with floor=1).
func nextScopeSeq(ctx context.Context, tx Tx, scope string) (int64, error) {
	var seq int64
	err := tx.QueryRowContext(ctx, `
		INSERT INTO scope_counters (scope, value) VALUES (?, 1)
		ON CONFLICT(scope) DO UPDATE SET value = value + 1
		RETURNING value`, scope).Scan(&seq)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Cannot happen: the upsert always produces a row. Reported rather
			// than ignored so a future schema change cannot silently stop
			// allocating sequence numbers.
			return 0, fmt.Errorf("runstore: allocate sequence for %s: no row returned", scope)
		}
		return 0, mapConstraintError("allocate sequence for "+scope, err)
	}
	return seq, nil
}

// ---------------------------------------------------------------------------
// Reads
// ---------------------------------------------------------------------------

// eventColumns is the column list shared by every event read, in the order
// scanEvent expects. Naming it once means a read cannot drift from its scanner.
const eventColumns = `id, project_id, project_seq, run_id, run_seq, attempt_id, type,
	       schema_version, occurred_at, identity_json, payload_json, payload_hash`

// GetEvent reads one event by id. A missing row is ErrNotFound.
//
// No scope is needed: the id is unique across both timelines, which is what
// makes it the one thing a client that received the same event over the project
// subscription and the run subscription can de-duplicate on (§27.4).
func GetEvent(ctx context.Context, q Querier, id string) (Event, error) {
	row := q.QueryRowContext(ctx, `SELECT `+eventColumns+` FROM events WHERE id = ?`, id)
	event, err := scanEvent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Event{}, fmt.Errorf("get event %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return Event{}, fmt.Errorf("get event %s: %w", id, err)
	}
	return event, nil
}

// ListProjectEvents returns up to limit events of a project with
// project_seq > after, oldest first.
//
// after = 0 means "from the beginning", which is the legal first replay of
// §20.2. limit is clamped to [1, MaxEventLimit]; a limit <= 0 means
// DefaultEventLimit. hasMore reports whether another page exists, so a caller
// never has to infer it from a short page — a short page is also what the end of
// the stream looks like.
func ListProjectEvents(ctx context.Context, q Querier, projectID string, after int64, limit int) ([]Event, bool, error) {
	return listEvents(ctx, q, "list project events "+projectID,
		`SELECT `+eventColumns+` FROM events WHERE project_id = ? AND project_seq > ? ORDER BY project_seq LIMIT ?`,
		projectID, after, limit)
}

// ListRunEvents is ListProjectEvents over the run timeline: it pages on run_seq,
// the counter a run-scoped subscriber follows (§27.4). The events returned are
// the same rows the project view returns, with their project_seq intact — the
// caller projects whichever number its scope asked for, and the two cursors
// advance independently.
func ListRunEvents(ctx context.Context, q Querier, runID string, after int64, limit int) ([]Event, bool, error) {
	return listEvents(ctx, q, "list run events "+runID,
		`SELECT `+eventColumns+` FROM events WHERE run_id = ? AND run_seq > ? ORDER BY run_seq LIMIT ?`,
		runID, after, limit)
}

// listEvents runs one paged read and decides hasMore.
//
// hasMore is computed by asking for one row more than the caller wants and
// dropping it: COUNT(*) over the remaining rows would be a second scan, and
// "returned == limit" is wrong exactly when the stream ends on a page boundary.
// The extra row costs one indexed lookup.
func listEvents(ctx context.Context, q Querier, op, query string, scopeArg any, after int64, limit int) ([]Event, bool, error) {
	if after < 0 {
		return nil, false, fmt.Errorf("%w: after %d must be >= 0", ErrInvalidEvent, after)
	}
	limit = clampEventLimit(limit)

	rows, err := q.QueryContext(ctx, query, scopeArg, after, limit+1)
	if err != nil {
		return nil, false, fmt.Errorf("%s: %w", op, err)
	}
	defer rows.Close()

	var out []Event
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return nil, false, fmt.Errorf("%s: %w", op, err)
		}
		out = append(out, event)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("%s: %w", op, err)
	}

	hasMore := len(out) > limit
	if hasMore {
		out = out[:limit]
	}
	return out, hasMore, nil
}

// clampEventLimit applies the §27.3 bounds: default 200, hard cap 1000. The cap
// is a clamp rather than an error because it protects the server from a client
// asking for the whole stream, and a clamped page is still a correct answer
// (hasMore says the rest is there).
func clampEventLimit(limit int) int {
	switch {
	case limit <= 0:
		return DefaultEventLimit
	case limit > MaxEventLimit:
		return MaxEventLimit
	default:
		return limit
	}
}

// scanEvent reads one event row. It serves both a single-row read and a list
// read through the scanner interface.
func scanEvent(row scanner) (Event, error) {
	var (
		event      Event
		runID      sql.NullString
		runSeq     sql.NullInt64
		attemptID  sql.NullString
		occurredAt int64
		identity   string
		payload    string
	)
	if err := row.Scan(
		&event.ID, &event.ProjectID, &event.ProjectSeq, &runID, &runSeq, &attemptID, &event.Type,
		&event.SchemaVersion, &occurredAt, &identity, &payload, &event.PayloadHash,
	); err != nil {
		return Event{}, err
	}
	event.RunID = fromNullString(runID)
	event.RunSeq = fromNullInt64(runSeq)
	event.AttemptID = fromNullString(attemptID)
	event.OccurredAt = fromUnixMilli(occurredAt)
	event.Identity = json.RawMessage(identity)
	event.Payload = json.RawMessage(payload)
	return event, nil
}
