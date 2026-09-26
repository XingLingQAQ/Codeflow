package runstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/run"
	sqlite "modernc.org/sqlite"
)

// These tests cover legacy.go and migration 005: the runtime half of the T1.05.c
// legacy Flow bridge. The properties they exist to prove:
//
//   - a source event becomes exactly one runtime event, however many times it is
//     projected: the second projection returns the stored event with created
//     false and touches no counter and no outbox row;
//   - a rolled-back projection leaves nothing: no event, no mapping, no consumed
//     sequence number, so the retry creates the fact as if the first attempt had
//     never run;
//   - a source id is never reused for a different fact: a second projection
//     naming another project or another event type is refused, not silently
//     answered with the first event;
//   - an unusable source identity is refused before any SQL runs;
//   - the mapping row is a fact: it cannot be rewritten or deleted;
//   - the projected event carries the source identity in its payload and the
//     system actor of the projection, with no run_id (§27.1 / CA-2).

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// legacyProjectID is the project the legacy projection fixtures belong to. It is
// deliberately different from eventProjectID: a projection test that accidentally
// relied on another fixture's project would otherwise pass for the wrong reason.
const legacyProjectID = "p-legacy"

// legacyStoreName is the source store name the fixtures project from. It stands
// for the legacy flow store's own name, which is what the real projector passes.
const legacyStoreName = "floweng"

// legacySourceImmutableMessage is the RAISE body of migration 005's BEFORE
// UPDATE / BEFORE DELETE triggers. It is asserted as a literal rather than
// through an errors.go constant because the store exposes no update or delete
// path for the mapping table, so there is no typed error for the store to map
// it to; T1.05.c's sentinel list is deliberately limited to
// ErrInvalidLegacySource and ErrLegacySourceConflict. The message is part of the
// schema contract, which is why the test pins the exact text.
const legacySourceImmutableMessage = "legacy_source_immutable"

// legacyFixture is a store with one project_ref the projection can name, plus
// the path so a test can reopen the same file.
type legacyFixture struct {
	t     *testing.T
	store *Store
	path  string
}

func newLegacyFixture(t *testing.T) *legacyFixture {
	t.Helper()
	store, path := newTestStore(t)
	seedLegacyProject(t, store, legacyProjectID)
	return &legacyFixture{t: t, store: store, path: path}
}

// seedLegacyProject inserts the project_ref a projected event's project_id must
// name (events.project_id is a foreign key to project_refs).
func seedLegacyProject(t *testing.T, store *Store, projectID string) {
	t.Helper()
	err := store.WithTx(context.Background(), func(ctx context.Context, tx Tx) error {
		return UpsertProjectRef(ctx, tx, run.ProjectRef{
			ProjectID:    projectID,
			SnapshotHash: "sha256:legacy-project",
			State:        run.ProjectRefStateActive,
			CapturedAt:   time.UnixMilli(1700000000000),
			VerifiedAt:   time.UnixMilli(1700000000001),
		})
	})
	if err != nil {
		t.Fatalf("seed project %s: %v", projectID, err)
	}
}

// legacyIdentity is the identity envelope of a projected legacy Flow event:
// project-scoped, actor system, and the projection's own id as actor.id (§27.1:
// "actor 为 system"; the id names the component that did the projecting).
func legacyIdentity(projectID string) json.RawMessage {
	return eventIdentityFor(projectID, "system", nil, nil, nil)
}

// legacyFlowPayload builds the payload §27.1 fixes for a projected Flow timeline
// entry: the legacy event name in source_type, the source identity, the Flow and
// stage the entry belongs to, and the human-readable message.
func legacyFlowPayload(sourceEventID, sourceType, flowID, stageID, message string) json.RawMessage {
	encoded, err := json.Marshal(map[string]any{
		"source_store":    legacyStoreName,
		"source_event_id": sourceEventID,
		"source_type":     sourceType,
		"flow_id":         flowID,
		"stage_id":        stageID,
		"message":         message,
	})
	if err != nil {
		panic(err)
	}
	return encoded
}

// legacyInput is the EventInput a projector hands to ProjectLegacyEventTx for
// one source event.
func legacyInput(sourceEventID string, destinations ...string) EventInput {
	return EventInput{
		ProjectID:    legacyProjectID,
		Type:         string(run.EventLegacyFlowEvent),
		OccurredAt:   time.UnixMilli(1700000002000),
		Identity:     legacyIdentity(legacyProjectID),
		Payload:      legacyFlowPayload(sourceEventID, "stage.done", "f1", "s1", "stage finished"),
		Destinations: destinations,
	}
}

// projectLegacy runs one projection in its own transaction and fails the test if
// it is refused.
func projectLegacy(t *testing.T, store *Store, src LegacySource, in EventInput) (Event, bool) {
	t.Helper()
	event, created, err := tryProjectLegacy(store, src, in)
	if err != nil {
		t.Fatalf("ProjectLegacyEventTx(%s/%s): %v", src.Store, src.EventID, err)
	}
	return event, created
}

// tryProjectLegacy is projectLegacy without the failure, for the tests that
// expect a refusal.
func tryProjectLegacy(store *Store, src LegacySource, in EventInput) (Event, bool, error) {
	var (
		event   Event
		created bool
	)
	err := store.WithTx(context.Background(), func(ctx context.Context, tx Tx) error {
		var err error
		event, created, err = ProjectLegacyEventTx(ctx, tx, src, in)
		return err
	})
	return event, created, err
}

// mappingRowCount counts the mapping rows of one source event, read straight
// from the table rather than inferred from a return value.
func mappingRowCount(t *testing.T, q Querier, src LegacySource) int {
	t.Helper()
	var n int
	if err := q.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM legacy_event_sources WHERE source_store = ? AND source_event_id = ?`,
		src.Store, src.EventID).Scan(&n); err != nil {
		t.Fatalf("count mapping for %s/%s: %v", src.Store, src.EventID, err)
	}
	return n
}

// outboxRowCount counts the outbox rows of one event.
func outboxRowCount(t *testing.T, q Querier, eventID string) int {
	t.Helper()
	var n int
	if err := q.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM outbox WHERE event_id = ?`, eventID).Scan(&n); err != nil {
		t.Fatalf("count outbox rows for %s: %v", eventID, err)
	}
	return n
}

// ---------------------------------------------------------------------------
// Idempotency
// ---------------------------------------------------------------------------

// TestProjectLegacyEventIsIdempotent is the core property of T1.05.c on the
// runtime side: the same source event projected twice produces one event, one
// project sequence and one set of outbox rows — and the second call reports
// created=false instead of pretending it wrote something.
//
// The counters and the row counts are read from the tables, not from the return
// values, so a bug that returned created=false while writing anyway would still
// fail here.
func TestProjectLegacyEventIsIdempotent(t *testing.T) {
	f := newLegacyFixture(t)
	src := LegacySource{Store: legacyStoreName, EventID: "ev-1"}
	const destination = "ws:project:" + legacyProjectID

	first, created := projectLegacy(t, f.store, src, legacyInput(src.EventID, destination))
	if !created {
		t.Fatal("first projection reported created=false, want true (nothing had been projected yet)")
	}
	if first.Type != string(run.EventLegacyFlowEvent) {
		t.Errorf("type = %q, want %q", first.Type, run.EventLegacyFlowEvent)
	}
	if first.ProjectID != legacyProjectID {
		t.Errorf("project_id = %q, want %q", first.ProjectID, legacyProjectID)
	}
	if first.ProjectSeq != 1 {
		t.Errorf("project_seq = %d, want 1 (the first event of the project)", first.ProjectSeq)
	}
	if first.RunID != nil || first.RunSeq != nil {
		t.Errorf("run_id/run_seq = %v/%v, want nil/nil (a Flow is not a Run)", first.RunID, first.RunSeq)
	}
	if n := mappingRowCount(t, f.store.DB(), src); n != 1 {
		t.Fatalf("mapping rows = %d, want 1", n)
	}
	if n := eventRowCount(t, f.store.DB(), "events"); n != 1 {
		t.Fatalf("events rows = %d, want 1", n)
	}
	if n := outboxRowCount(t, f.store.DB(), first.ID); n != 1 {
		t.Fatalf("outbox rows = %d, want 1", n)
	}
	if got := counterValue(t, f.store.DB(), projectScope(legacyProjectID)); got != 1 {
		t.Fatalf("project counter = %d, want 1", got)
	}

	// The second projection of the same source event: same event back, no write.
	second, created := projectLegacy(t, f.store, src, legacyInput(src.EventID, destination))
	if created {
		t.Error("second projection reported created=true, want false (the source event was already projected)")
	}
	if second.ID != first.ID {
		t.Errorf("second projection returned event %s, want the stored %s", second.ID, first.ID)
	}
	if second.ProjectSeq != first.ProjectSeq {
		t.Errorf("second projection project_seq = %d, want the stored %d", second.ProjectSeq, first.ProjectSeq)
	}
	if n := eventRowCount(t, f.store.DB(), "events"); n != 1 {
		t.Errorf("events rows after the second projection = %d, want 1", n)
	}
	if n := mappingRowCount(t, f.store.DB(), src); n != 1 {
		t.Errorf("mapping rows after the second projection = %d, want 1", n)
	}
	if n := outboxRowCount(t, f.store.DB(), first.ID); n != 1 {
		t.Errorf("outbox rows after the second projection = %d, want 1 (no second delivery was queued)", n)
	}
	if got := counterValue(t, f.store.DB(), projectScope(legacyProjectID)); got != 1 {
		t.Errorf("project counter after the second projection = %d, want 1 (a duplicate consumes no sequence)", got)
	}

	// The stored payload is byte-identical to what the first call wrote, and it
	// is canonical: the projector's payload is the fact, not a scratch buffer.
	stored, err := GetEvent(context.Background(), f.store.DB(), first.ID)
	if err != nil {
		t.Fatalf("GetEvent: %v", err)
	}
	if string(stored.Payload) != string(first.Payload) {
		t.Errorf("stored payload = %s, want %s", stored.Payload, first.Payload)
	}
	if stored.PayloadHash != hashOfPayload(stored.Payload) {
		t.Errorf("payload_hash = %s, want the hash of the stored bytes %s",
			stored.PayloadHash, hashOfPayload(stored.Payload))
	}
}

// TestProjectLegacyEventKeepsPayloadAndIdentity pins what a projected event must
// look like on the wire (§27.1 item 4 / CA-2): the legacy event name stays in
// payload.source_type, the source identity is in the payload, the actor is the
// system projector, and there is no run.
//
// The canonical form is asserted exactly, because the payload is stored as
// canonical JSON and a consumer reads those bytes.
func TestProjectLegacyEventKeepsPayloadAndIdentity(t *testing.T) {
	ctx := context.Background()
	f := newLegacyFixture(t)
	src := LegacySource{Store: legacyStoreName, EventID: "ev-payload"}

	// Deliberately not canonical: keys out of order and extra whitespace.
	raw := json.RawMessage(`{ "message" : "stage finished" , "stage_id":"s1",
		"flow_id":"f1", "source_type":"stage.done", "source_event_id":"ev-payload",
		"source_store":"floweng" }`)
	in := legacyInput(src.EventID)
	in.Payload = raw

	event, created := projectLegacy(t, f.store, src, in)
	if !created {
		t.Fatal("projection reported created=false, want true")
	}

	wantPayload := `{"flow_id":"f1","message":"stage finished","source_event_id":"ev-payload",` +
		`"source_store":"floweng","source_type":"stage.done","stage_id":"s1"}`
	if string(event.Payload) != wantPayload {
		t.Errorf("stored payload = %s\nwant canonical       %s", event.Payload, wantPayload)
	}

	// Identity: exactly project_id + actor{type,id,source}, canonical key order,
	// and no run_id/attempt_id — a Flow is not a Run.
	wantIdentity := `{"actor":{"id":"worker-1","type":"system"},"project_id":"` + legacyProjectID + `"}`
	if string(event.Identity) != wantIdentity {
		t.Errorf("stored identity = %s\nwant canonical        %s", event.Identity, wantIdentity)
	}
	if event.RunID != nil {
		t.Errorf("run_id = %q, want nil", *event.RunID)
	}
	if event.AttemptID != nil {
		t.Errorf("attempt_id = %q, want nil", *event.AttemptID)
	}
	if event.SchemaVersion != EventSchemaVersion {
		t.Errorf("schema_version = %d, want %d", event.SchemaVersion, EventSchemaVersion)
	}
	if event.OccurredAt.UnixMilli() != 1700000002000 {
		t.Errorf("occurred_at = %d, want the source event's instant 1700000002000", event.OccurredAt.UnixMilli())
	}

	// The event is readable through the project timeline, which is the view a
	// legacy subscriber follows (a Flow has no run timeline to read).
	events, _, err := ListProjectEvents(ctx, f.store.DB(), legacyProjectID, 0, MaxEventLimit)
	if err != nil {
		t.Fatalf("ListProjectEvents: %v", err)
	}
	if len(events) != 1 || events[0].ID != event.ID {
		t.Fatalf("project timeline = %+v, want exactly the projected event", events)
	}
}

// ---------------------------------------------------------------------------
// Conflict: one source id is one fact
// ---------------------------------------------------------------------------

// TestProjectLegacyEventRejectsReusedSourceID proves a source id cannot be
// reused for a different fact: the same (store, event id) projected into another
// project, or as another event type, is refused with ErrLegacySourceConflict
// instead of being answered with the event stored the first time.
//
// Both refusals must leave the store exactly as it was: no second event, one
// mapping row, and the counter where it was.
func TestProjectLegacyEventRejectsReusedSourceID(t *testing.T) {
	ctx := context.Background()
	f := newLegacyFixture(t)
	seedLegacyProject(t, f.store, "p-legacy-other")

	src := LegacySource{Store: legacyStoreName, EventID: "ev-reused"}
	first, created := projectLegacy(t, f.store, src, legacyInput(src.EventID))
	if !created {
		t.Fatal("first projection reported created=false, want true")
	}

	t.Run("a different project", func(t *testing.T) {
		in := legacyInput(src.EventID)
		in.ProjectID = "p-legacy-other"
		in.Identity = legacyIdentity("p-legacy-other")

		event, created, err := tryProjectLegacy(f.store, src, in)
		if !errors.Is(err, ErrLegacySourceConflict) {
			t.Fatalf("projection into another project = (%s, %v, %v), want ErrLegacySourceConflict",
				event.ID, created, err)
		}
		if event.ID != "" || created {
			t.Errorf("refused projection returned (%s, created=%v), want nothing", event.ID, created)
		}
		if !strings.Contains(err.Error(), "p-legacy-other") || !strings.Contains(err.Error(), legacyProjectID) {
			t.Errorf("error %q should name both projects so the caller can see the reuse", err)
		}
	})

	t.Run("a different event type", func(t *testing.T) {
		in := legacyInput(src.EventID)
		in.Type = string(run.EventApprovalDecided)

		event, created, err := tryProjectLegacy(f.store, src, in)
		if !errors.Is(err, ErrLegacySourceConflict) {
			t.Fatalf("projection as another type = (%s, %v, %v), want ErrLegacySourceConflict",
				event.ID, created, err)
		}
		if event.ID != "" || created {
			t.Errorf("refused projection returned (%s, created=%v), want nothing", event.ID, created)
		}
		if !strings.Contains(err.Error(), string(run.EventApprovalDecided)) {
			t.Errorf("error %q should name the type the source id was already projected as", err)
		}
	})

	// Neither refusal wrote anything.
	if n := eventRowCount(t, f.store.DB(), "events"); n != 1 {
		t.Errorf("events rows = %d, want 1 (a refused projection must not append)", n)
	}
	if n := mappingRowCount(t, f.store.DB(), src); n != 1 {
		t.Errorf("mapping rows = %d, want 1", n)
	}
	if got := counterValue(t, f.store.DB(), projectScope("p-legacy-other")); got != -1 {
		t.Errorf("counter for the other project = %d, want no row (nothing was allocated for it)", got)
	}
	if got := counterValue(t, f.store.DB(), projectScope(legacyProjectID)); got != 1 {
		t.Errorf("project counter = %d, want 1", got)
	}
	stored, err := GetEvent(ctx, f.store.DB(), first.ID)
	if err != nil {
		t.Fatalf("GetEvent: %v", err)
	}
	if stored.ProjectID != legacyProjectID || stored.Type != string(run.EventLegacyFlowEvent) {
		t.Errorf("stored event = %s/%s, want the original projection untouched",
			stored.ProjectID, stored.Type)
	}
}

// ---------------------------------------------------------------------------
// Validation: refused before any SQL
// ---------------------------------------------------------------------------

// TestProjectLegacyEventRejectsInvalidSource walks the source identity contract:
// a blank store or event id, one over the column limit, and one with surrounding
// whitespace are all ErrInvalidLegacySource — and none of them writes anything,
// not even a counter row.
//
// Whitespace matters because the identity is a byte string: " floweng" is a
// different store from "floweng", so accepting it would create a second mapping
// for the same fact under a spelling nobody meant.
func TestProjectLegacyEventRejectsInvalidSource(t *testing.T) {
	ctx := context.Background()
	f := newLegacyFixture(t)

	cases := []struct {
		name string
		src  LegacySource
	}{
		{"empty store", LegacySource{Store: "", EventID: "ev-1"}},
		{"blank store", LegacySource{Store: "   ", EventID: "ev-1"}},
		{"empty event id", LegacySource{Store: legacyStoreName, EventID: ""}},
		{"blank event id", LegacySource{Store: legacyStoreName, EventID: "\t\n"}},
		{"store with leading space", LegacySource{Store: " " + legacyStoreName, EventID: "ev-1"}},
		{"store with trailing space", LegacySource{Store: legacyStoreName + " ", EventID: "ev-1"}},
		{"event id with trailing space", LegacySource{Store: legacyStoreName, EventID: "ev-1 "}},
		{"store over the limit", LegacySource{Store: strings.Repeat("s", MaxLegacySourceStoreLength+1), EventID: "ev-1"}},
		{"event id over the limit", LegacySource{Store: legacyStoreName, EventID: strings.Repeat("e", MaxLegacySourceEventIDLength+1)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			event, created, err := tryProjectLegacy(f.store, tc.src, legacyInput("ev-1"))
			if !errors.Is(err, ErrInvalidLegacySource) {
				t.Fatalf("ProjectLegacyEventTx(%+v) = (%s, %v, %v), want ErrInvalidLegacySource",
					tc.src, event.ID, created, err)
			}
			if event.ID != "" || created {
				t.Errorf("refused projection returned (%s, created=%v), want nothing", event.ID, created)
			}

			// Nothing was written: no event, no mapping, no counter row.
			if n := eventRowCount(t, f.store.DB(), "events"); n != 0 {
				t.Errorf("events rows = %d, want 0", n)
			}
			if n := eventRowCount(t, f.store.DB(), "legacy_event_sources"); n != 0 {
				t.Errorf("legacy_event_sources rows = %d, want 0", n)
			}
			if got := counterValue(t, f.store.DB(), projectScope(legacyProjectID)); got != -1 {
				t.Errorf("project counter = %d, want no row (validation runs before any SQL)", got)
			}
		})
	}

	// The limit boundary itself is accepted: the check is "at most", not
	// "shorter than", and a test that only proved the rejection would leave the
	// off-by-one unproven.
	t.Run("the limits themselves are accepted", func(t *testing.T) {
		src := LegacySource{
			Store:   strings.Repeat("s", MaxLegacySourceStoreLength),
			EventID: strings.Repeat("e", MaxLegacySourceEventIDLength),
		}
		if _, created := projectLegacy(t, f.store, src, legacyInput(src.EventID)); !created {
			t.Error("projection at the column limits reported created=false, want true")
		}
	})

	// LegacyEventFor applies the same contract, so a reader cannot ask with an
	// identity a writer would refuse.
	t.Run("LegacyEventFor refuses the same identities", func(t *testing.T) {
		for _, tc := range cases {
			if _, _, err := LegacyEventFor(ctx, f.store.DB(), tc.src); !errors.Is(err, ErrInvalidLegacySource) {
				t.Errorf("LegacyEventFor(%+v) error = %v, want ErrInvalidLegacySource", tc.src, err)
			}
		}
	})
}

// TestProjectLegacyEventRejectsInvalidEvent proves the projection does not
// bypass the event contract: an input AppendEventTx would refuse is refused
// here, with ErrInvalidEvent and no write at all. A projector that could store
// an event a direct append would reject would make the legacy path the one hole
// in the closed enum.
func TestProjectLegacyEventRejectsInvalidEvent(t *testing.T) {
	f := newLegacyFixture(t)
	src := LegacySource{Store: legacyStoreName, EventID: "ev-bad"}

	cases := []struct {
		name string
		mut  func(*EventInput)
	}{
		{"unknown event type", func(in *EventInput) { in.Type = "legacy.not_a_type" }},
		{"identity of another project", func(in *EventInput) { in.Identity = legacyIdentity("p-somewhere-else") }},
		{"run id without an identity run", func(in *EventInput) { in.RunID = stringPtr("run-1") }},
		{"payload that is not an object", func(in *EventInput) { in.Payload = json.RawMessage(`[1,2,3]`) }},
		{"empty payload", func(in *EventInput) { in.Payload = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := legacyInput(src.EventID)
			tc.mut(&in)

			event, created, err := tryProjectLegacy(f.store, src, in)
			if !errors.Is(err, ErrInvalidEvent) {
				t.Fatalf("ProjectLegacyEventTx with %s = (%s, %v, %v), want ErrInvalidEvent",
					tc.name, event.ID, created, err)
			}
			if event.ID != "" || created {
				t.Errorf("refused projection returned (%s, created=%v), want nothing", event.ID, created)
			}
			if n := eventRowCount(t, f.store.DB(), "events"); n != 0 {
				t.Errorf("events rows = %d, want 0", n)
			}
			if n := eventRowCount(t, f.store.DB(), "legacy_event_sources"); n != 0 {
				t.Errorf("legacy_event_sources rows = %d, want 0", n)
			}
			if got := counterValue(t, f.store.DB(), projectScope(legacyProjectID)); got != -1 {
				t.Errorf("project counter = %d, want no row", got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Rollback and retry
// ---------------------------------------------------------------------------

// TestProjectLegacyEventRollbackLeavesNothing is the atomicity rule for the
// projection: the mapping row and the event are one unit. A caller that rolls
// back leaves neither, the sequence number is not consumed, and the retry
// creates exactly one event — with the sequence the failed attempt would have
// used, so the timeline has no hole caused by an undone write.
func TestProjectLegacyEventRollbackLeavesNothing(t *testing.T) {
	ctx := context.Background()
	f := newLegacyFixture(t)
	src := LegacySource{Store: legacyStoreName, EventID: "ev-rollback"}
	sentinel := errors.New("the unit of work failed after the projection")

	// A committed first event, so the retry's sequence number is visibly not 1:
	// a counter that had leaked would show up as a gap rather than as a
	// coincidence.
	projectLegacy(t, f.store, LegacySource{Store: legacyStoreName, EventID: "ev-before"}, legacyInput("ev-before"))
	if got := counterValue(t, f.store.DB(), projectScope(legacyProjectID)); got != 1 {
		t.Fatalf("project counter = %d, want 1 before the rollback case", got)
	}

	err := f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		if _, created, err := ProjectLegacyEventTx(ctx, tx, src, legacyInput(src.EventID, "ws:project:"+legacyProjectID)); err != nil {
			return err
		} else if !created {
			return fmt.Errorf("inside the transaction the projection reported created=false, want true")
		}
		// The transaction sees its own uncommitted work: a second projection of
		// the same source event inside one unit of work must be a no-op, not a
		// duplicate.
		if _, created, err := ProjectLegacyEventTx(ctx, tx, src, legacyInput(src.EventID)); err != nil {
			return err
		} else if created {
			return fmt.Errorf("the second projection in the same transaction reported created=true")
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("WithTx error = %v, want the sentinel back unchanged", err)
	}

	// Nothing survived the rollback.
	if n := eventRowCount(t, f.store.DB(), "events"); n != 1 {
		t.Errorf("events rows after the rollback = %d, want 1 (only the committed one)", n)
	}
	if n := eventRowCount(t, f.store.DB(), "legacy_event_sources"); n != 1 {
		t.Errorf("legacy_event_sources rows after the rollback = %d, want 1", n)
	}
	if n := eventRowCount(t, f.store.DB(), "outbox"); n != 0 {
		t.Errorf("outbox rows after the rollback = %d, want 0 (nothing was queued for delivery)", n)
	}
	if _, found, err := LegacyEventFor(ctx, f.store.DB(), src); err != nil {
		t.Fatalf("LegacyEventFor after the rollback: %v", err)
	} else if found {
		t.Error("LegacyEventFor found the rolled-back mapping, want it gone")
	}
	if got := counterValue(t, f.store.DB(), projectScope(legacyProjectID)); got != 1 {
		t.Errorf("project counter after the rollback = %d, want 1 (a rolled-back projection consumes nothing)", got)
	}

	// The retry creates the fact with the sequence the failed attempt would have
	// used, and reports created=true because it really did create it.
	retried, created := projectLegacy(t, f.store, src, legacyInput(src.EventID))
	if !created {
		t.Error("the retry reported created=false, want true (nothing was projected)")
	}
	if retried.ProjectSeq != 2 {
		t.Errorf("retried project_seq = %d, want 2 (no gap from the undone write)", retried.ProjectSeq)
	}
	if n := eventRowCount(t, f.store.DB(), "events"); n != 2 {
		t.Errorf("events rows after the retry = %d, want 2", n)
	}
	if n := mappingRowCount(t, f.store.DB(), src); n != 1 {
		t.Errorf("mapping rows after the retry = %d, want exactly 1", n)
	}
	if got := counterValue(t, f.store.DB(), projectScope(legacyProjectID)); got != 2 {
		t.Errorf("project counter after the retry = %d, want 2", got)
	}
}

// TestLegacyEventForFindsTheMapping covers the lookup both directions: not found
// before the projection, found afterwards with the stored event id, and readable
// from inside a transaction as well as from the pool.
func TestLegacyEventForFindsTheMapping(t *testing.T) {
	ctx := context.Background()
	f := newLegacyFixture(t)
	src := LegacySource{Store: legacyStoreName, EventID: "ev-lookup"}
	other := LegacySource{Store: legacyStoreName, EventID: "ev-lookup-other"}
	otherStore := LegacySource{Store: "otherstore", EventID: src.EventID}

	// Nothing is mapped yet — for either key of the pair.
	for _, s := range []LegacySource{src, other, otherStore} {
		id, found, err := LegacyEventFor(ctx, f.store.DB(), s)
		if err != nil {
			t.Fatalf("LegacyEventFor(%+v): %v", s, err)
		}
		if found || id != "" {
			t.Errorf("LegacyEventFor(%+v) = (%q, %v), want not found", s, id, found)
		}
	}

	event, _ := projectLegacy(t, f.store, src, legacyInput(src.EventID))

	for _, q := range []struct {
		name string
		q    Querier
	}{
		{"pool", f.store.DB()},
		{"transaction", nil}, // filled in below
	} {
		if q.q == nil {
			var id string
			var found bool
			err := f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
				var err error
				id, found, err = LegacyEventFor(ctx, tx, src)
				return err
			})
			if err != nil {
				t.Fatalf("LegacyEventFor in a transaction: %v", err)
			}
			if !found || id != event.ID {
				t.Errorf("LegacyEventFor in a transaction = (%q, %v), want (%q, true)", id, found, event.ID)
			}
			continue
		}
		id, found, err := LegacyEventFor(ctx, q.q, src)
		if err != nil {
			t.Fatalf("LegacyEventFor from the %s: %v", q.name, err)
		}
		if !found || id != event.ID {
			t.Errorf("LegacyEventFor from the %s = (%q, %v), want (%q, true)", q.name, id, found, event.ID)
		}
	}

	// The pair is the identity: the same event id under another store is a
	// different source event, and the same store under another event id too.
	for _, s := range []LegacySource{other, otherStore} {
		if _, found, err := LegacyEventFor(ctx, f.store.DB(), s); err != nil {
			t.Fatalf("LegacyEventFor(%+v): %v", s, err)
		} else if found {
			t.Errorf("LegacyEventFor(%+v) found a mapping, want not found (the pair is the identity)", s)
		}
	}
}

// ---------------------------------------------------------------------------
// The mapping row is a fact
// ---------------------------------------------------------------------------

// TestLegacyEventSourceRowIsImmutable proves the mapping is written once: an
// UPDATE or DELETE is refused by migration 005's triggers, so no statement can
// re-point a source event at another runtime event or erase the fact that it was
// projected.
//
// The schema-level refusal is the guarantee; the store exposes no update/delete
// path at all, which is why this test drives the raw statements.
func TestLegacyEventSourceRowIsImmutable(t *testing.T) {
	ctx := context.Background()
	f := newLegacyFixture(t)
	src := LegacySource{Store: legacyStoreName, EventID: "ev-immutable"}
	event, _ := projectLegacy(t, f.store, src, legacyInput(src.EventID))

	// The setup must be sound: an unrelated row can be updated, so the failures
	// below are caused by the mapping row itself.
	if _, err := f.store.DB().ExecContext(ctx,
		`UPDATE project_refs SET verified_at = 99 WHERE project_id = ?`, legacyProjectID); err != nil {
		t.Fatalf("setup update on project_refs: %v", err)
	}

	assertLegacySourceImmutable := func(op string, err error) {
		t.Helper()
		var serr *sqlite.Error
		if !errors.As(err, &serr) || serr.Code() != sqliteConstraintTrigger {
			t.Errorf("%s: error = %v (code %d), want a trigger refusal (code %d)",
				op, err, sqliteExtended(err), sqliteConstraintTrigger)
		}
		if !strings.Contains(err.Error(), legacySourceImmutableMessage) {
			t.Errorf("%s: error %q does not name %s", op, err, legacySourceImmutableMessage)
		}
	}

	updateErr := func(query string, args ...any) error {
		_, err := f.store.DB().ExecContext(ctx, query, args...)
		return err
	}

	if err := updateErr(`UPDATE legacy_event_sources SET event_id = 'other' WHERE source_store = ? AND source_event_id = ?`,
		src.Store, src.EventID); err == nil {
		t.Error("UPDATE legacy_event_sources succeeded, want the immutability trigger to refuse it")
	} else {
		assertLegacySourceImmutable("update event_id", err)
	}
	if err := updateErr(`UPDATE legacy_event_sources SET projected_at = 1 WHERE source_store = ? AND source_event_id = ?`,
		src.Store, src.EventID); err == nil {
		t.Error("UPDATE legacy_event_sources.projected_at succeeded, want a refusal")
	} else {
		assertLegacySourceImmutable("update projected_at", err)
	}
	if err := updateErr(`DELETE FROM legacy_event_sources WHERE source_store = ? AND source_event_id = ?`,
		src.Store, src.EventID); err == nil {
		t.Error("DELETE FROM legacy_event_sources succeeded, want the immutability trigger to refuse it")
	} else {
		assertLegacySourceImmutable("delete", err)
	}

	// The row is exactly as it was, and the projection still resolves.
	id, found, err := LegacyEventFor(ctx, f.store.DB(), src)
	if err != nil {
		t.Fatalf("LegacyEventFor after the refused mutations: %v", err)
	}
	if !found || id != event.ID {
		t.Errorf("mapping after the refused mutations = (%q, %v), want (%q, true)", id, found, event.ID)
	}
	if n := mappingRowCount(t, f.store.DB(), src); n != 1 {
		t.Errorf("mapping rows = %d, want 1", n)
	}
}

// TestLegacyEventSourceSchemaRejectsBadRows pins the column contract the Go
// validation mirrors: a blank or oversized store or event id, and a mapping that
// names no event, are refused by the database itself. The Go checks exist so a
// caller gets a typed error before any SQL; this test proves the SQL would not
// have accepted the row either.
func TestLegacyEventSourceSchemaRejectsBadRows(t *testing.T) {
	ctx := context.Background()
	f := newLegacyFixture(t)
	src := LegacySource{Store: legacyStoreName, EventID: "ev-schema"}
	event, _ := projectLegacy(t, f.store, src, legacyInput(src.EventID))

	insert := func(store, eventID, runtimeEventID string, projectedAt any) error {
		_, err := f.store.DB().ExecContext(ctx, `
			INSERT INTO legacy_event_sources (source_store, source_event_id, event_id, projected_at)
			VALUES (?, ?, ?, ?)`, store, eventID, runtimeEventID, projectedAt)
		return err
	}

	t.Run("a blank store is refused", func(t *testing.T) {
		err := insert("", "ev-x", event.ID, 1)
		if code := sqliteExtended(err); code != sqliteConstraintCheck && code != sqliteConstraintNotNull {
			t.Errorf("blank source_store error = %v (code %d), want a CHECK or NOT NULL refusal", err, code)
		}
	})
	t.Run("an oversized store is refused", func(t *testing.T) {
		err := insert(strings.Repeat("s", MaxLegacySourceStoreLength+1), "ev-x", event.ID, 1)
		if code := sqliteExtended(err); code != sqliteConstraintCheck {
			t.Errorf("oversized source_store error = %v (code %d), want a CHECK refusal", err, code)
		}
	})
	t.Run("an oversized event id is refused", func(t *testing.T) {
		err := insert(legacyStoreName, strings.Repeat("e", MaxLegacySourceEventIDLength+1), event.ID, 1)
		if code := sqliteExtended(err); code != sqliteConstraintCheck {
			t.Errorf("oversized source_event_id error = %v (code %d), want a CHECK refusal", err, code)
		}
	})
	t.Run("a mapping without an event is refused", func(t *testing.T) {
		err := insert(legacyStoreName, "ev-orphan", "no-such-event", 1)
		if code := sqliteExtended(err); code != sqliteConstraintForeignKey {
			t.Errorf("orphan mapping error = %v (code %d), want a foreign key refusal", err, code)
		}
	})
	t.Run("the same event cannot be two source events", func(t *testing.T) {
		// event_id is UNIQUE: one runtime event is the projection of exactly one
		// source event, or replaying either source event would be ambiguous.
		err := insert(legacyStoreName, "ev-second-source", event.ID, 1)
		if code := sqliteExtended(err); code != sqliteConstraintUnique {
			t.Errorf("second mapping for one event error = %v (code %d), want a UNIQUE refusal", err, code)
		}
	})
	t.Run("a duplicate source event is refused by the primary key", func(t *testing.T) {
		err := insert(legacyStoreName, src.EventID, event.ID, 1)
		if code := sqliteExtended(err); code != sqliteConstraintUnique && code != sqliteConstraintPrimaryKey {
			t.Errorf("duplicate mapping error = %v (code %d), want a PRIMARY KEY/UNIQUE refusal", err, code)
		}
	})

	// None of the refused rows changed the table.
	if n := eventRowCount(t, f.store.DB(), "legacy_event_sources"); n != 1 {
		t.Errorf("mapping rows = %d, want 1", n)
	}
}

// ---------------------------------------------------------------------------
// Concurrency
// ---------------------------------------------------------------------------

// TestProjectLegacyEventRaceIsRetryableNotAConflict pins the classification of
// the mapping insert's primary-key refusal. Under BEGIN IMMEDIATE the race
// cannot be produced with two real writers, so a test trigger raises SQLite's own
// constraint message at the insert: the call must report
// ErrLegacyProjectionRaced (retry) and must not report ErrLegacySourceConflict
// (do not retry), and the caller's rollback must leave nothing behind.
func TestProjectLegacyEventRaceIsRetryableNotAConflict(t *testing.T) {
	ctx := context.Background()
	f := newLegacyFixture(t)
	if _, err := f.store.DB().ExecContext(ctx, `
		CREATE TRIGGER zz_simulate_projection_race BEFORE INSERT ON legacy_event_sources
		BEGIN
			SELECT RAISE(ABORT, 'UNIQUE constraint failed: legacy_event_sources.source_store, legacy_event_sources.source_event_id');
		END`); err != nil {
		t.Fatalf("create race trigger: %v", err)
	}
	src := LegacySource{Store: legacyStoreName, EventID: "ev-raced"}
	_, _, err := tryProjectLegacy(f.store, src, legacyInput(src.EventID))
	if !errors.Is(err, ErrLegacyProjectionRaced) {
		t.Fatalf("projection that lost the mapping race = %v, want ErrLegacyProjectionRaced", err)
	}
	if errors.Is(err, ErrLegacySourceConflict) {
		t.Fatalf("a lost race must not be reported as ErrLegacySourceConflict (that one means do not retry): %v", err)
	}
	var events int
	if err := f.store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&events); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if events != 0 {
		t.Fatalf("events after the rolled-back raced projection = %d, want 0", events)
	}
}

// TestProjectLegacyEventConcurrentProjectionCreatesOne is the projector-race
// case: two store handles project the same source event at the same time. The
// database must end up with exactly one event and exactly one mapping row, and
// exactly one of the callers may report created=true — the other either sees the
// committed mapping (created=false) or gets ErrLegacyProjectionRaced and retries
// into the created=false path.
//
// Both writers retry, because "who wins" is not deterministic: whichever loses
// the race must still end up with the winner's event, which is what makes the
// projector's write-back safe.
func TestProjectLegacyEventConcurrentProjectionCreatesOne(t *testing.T) {
	ctx := context.Background()
	f := newLegacyFixture(t)
	src := LegacySource{Store: legacyStoreName, EventID: "ev-race"}

	// A second handle on the same file: the race must be between connections,
	// not between goroutines sharing one handle's pool.
	second, _, err := OpenStore(ctx, f.path)
	if err != nil {
		t.Fatalf("second OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })

	const writers = 4
	start := make(chan struct{})
	type outcome struct {
		eventID string
		created bool
		err     error
	}
	results := make(chan outcome, writers)

	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			<-start
			store := f.store
			if w%2 == 1 {
				store = second
			}
			// Retry the conflict, which is what a projector does: the loser of
			// the primary-key race re-reads and finds the winner's event.
			for attempt := 0; attempt < 5; attempt++ {
				event, created, err := tryProjectLegacy(store, src, legacyInput(src.EventID))
				switch {
				case err == nil:
					results <- outcome{eventID: event.ID, created: created}
					return
				case errors.Is(err, ErrLegacyProjectionRaced):
					continue
				default:
					results <- outcome{err: err}
					return
				}
			}
			results <- outcome{err: fmt.Errorf("writer %d gave up after repeated conflicts", w)}
		}(w)
	}
	close(start)
	wg.Wait()
	close(results)

	var (
		createdCount int
		eventIDs     = map[string]bool{}
	)
	for res := range results {
		if res.err != nil {
			t.Fatalf("concurrent projection: %v", res.err)
		}
		if res.created {
			createdCount++
		}
		eventIDs[res.eventID] = true
	}
	if createdCount != 1 {
		t.Errorf("created=true reported %d times, want exactly 1", createdCount)
	}
	if len(eventIDs) != 1 {
		t.Errorf("writers resolved %d different events (%v), want one", len(eventIDs), eventIDs)
	}

	// The tables agree with the callers.
	if n := eventRowCount(t, f.store.DB(), "events"); n != 1 {
		t.Errorf("events rows = %d, want 1", n)
	}
	if n := mappingRowCount(t, f.store.DB(), src); n != 1 {
		t.Errorf("mapping rows = %d, want 1", n)
	}
	if got := counterValue(t, f.store.DB(), projectScope(legacyProjectID)); got != 1 {
		t.Errorf("project counter = %d, want 1 (one fact, one sequence)", got)
	}
	if n := eventRowCount(t, f.store.DB(), "outbox"); n != 0 {
		t.Errorf("outbox rows = %d, want 0 (the fixture queued no destination)", n)
	}
	t.Logf("EVIDENCE concurrent projection: %d writers, 1 event, 1 mapping, created=true reported once", writers)
}

// ---------------------------------------------------------------------------
// Reopen
// ---------------------------------------------------------------------------

// TestProjectLegacyEventSurvivesReopen proves the projection's de-duplication is
// durable: after the process restarts, projecting the same source event again is
// still a no-op, and the mapping is readable from the new handle.
func TestProjectLegacyEventSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	f := newLegacyFixture(t)
	src := LegacySource{Store: legacyStoreName, EventID: "ev-reopen"}
	first, _ := projectLegacy(t, f.store, src, legacyInput(src.EventID, "ws:project:"+legacyProjectID))

	if err := f.store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	reopened, res, err := OpenStore(ctx, f.path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if len(res.Applied) != 0 {
		t.Errorf("reopen applied %d migrations, want 0", len(res.Applied))
	}

	id, found, err := LegacyEventFor(ctx, reopened.DB(), src)
	if err != nil {
		t.Fatalf("LegacyEventFor after reopen: %v", err)
	}
	if !found || id != first.ID {
		t.Fatalf("mapping after reopen = (%q, %v), want (%q, true)", id, found, first.ID)
	}

	again, created := projectLegacy(t, reopened, src, legacyInput(src.EventID))
	if created {
		t.Error("projection after reopen reported created=true, want false (the mapping is durable)")
	}
	if again.ID != first.ID {
		t.Errorf("projection after reopen returned %s, want %s", again.ID, first.ID)
	}
	if n := eventRowCount(t, reopened.DB(), "events"); n != 1 {
		t.Errorf("events rows after reopen = %d, want 1", n)
	}
}
