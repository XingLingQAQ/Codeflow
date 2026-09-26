package floweng

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// Tests for the legacy Flow store's local outbox (T1.05.c).
//
// The subject is the pair "the flows row and the outbox rows it produced commit
// together, and the outbox is readable as an ordered per-Flow queue". Every test
// runs against a file database in t.TempDir(): an in-memory database cannot be
// closed and reopened, and the reopen is half of what is being tested.

// newLegacyTestStore opens a file-backed store and returns it with its path.
func newLegacyTestStore(t *testing.T) (*SQLiteFlowStore, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "floweng.db")
	store, err := NewSQLiteFlowStore(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, path
}

// legacyOutboxRow is one flow_event_outbox row, read back for assertions.
type legacyOutboxRow struct {
	seq              int64
	sourceEventID    string
	flowID           string
	projectID        string
	eventType        string
	stageID          string
	message          string
	occurredAt       int64
	state            string
	attemptCount     int64
	nextAttemptAt    sql.NullInt64
	lastError        sql.NullString
	projectedEventID sql.NullString
	projectedAt      sql.NullInt64
	createdAt        int64
}

const legacyOutboxSelect = `SELECT seq, source_event_id, flow_id, project_id, event_type, stage_id, message,
       occurred_at, state, attempt_count, next_attempt_at, last_error, projected_event_id, projected_at, created_at
  FROM flow_event_outbox`

func readLegacyOutboxRows(t *testing.T, s *SQLiteFlowStore, where string, args ...any) []legacyOutboxRow {
	t.Helper()
	q := legacyOutboxSelect
	if where != "" {
		q += " WHERE " + where
	}
	q += " ORDER BY seq"
	rows, err := s.db.Query(q, args...)
	if err != nil {
		t.Fatalf("read outbox: %v", err)
	}
	defer rows.Close()
	var out []legacyOutboxRow
	for rows.Next() {
		var r legacyOutboxRow
		if err := rows.Scan(&r.seq, &r.sourceEventID, &r.flowID, &r.projectID, &r.eventType, &r.stageID,
			&r.message, &r.occurredAt, &r.state, &r.attemptCount, &r.nextAttemptAt, &r.lastError,
			&r.projectedEventID, &r.projectedAt, &r.createdAt); err != nil {
			t.Fatalf("scan outbox row: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate outbox: %v", err)
	}
	return out
}

func legacyOutboxCount(t *testing.T, s *SQLiteFlowStore) int {
	t.Helper()
	return len(readLegacyOutboxRows(t, s, ""))
}

// storedFlowRow reads the raw payload/updated_at of a flows row, so a test can
// prove a rejected Put left the previous document exactly as it was.
func storedFlowRow(t *testing.T, s *SQLiteFlowStore, id string) (payload string, updatedAt int64) {
	t.Helper()
	err := s.db.QueryRow(`SELECT payload, updated_at FROM flows WHERE id = ?`, id).Scan(&payload, &updatedAt)
	if err == sql.ErrNoRows {
		t.Fatalf("flows row %s does not exist", id)
	}
	if err != nil {
		t.Fatalf("read flows row %s: %v", id, err)
	}
	return payload, updatedAt
}

func testLegacyFlow(id, projectID string, events ...FlowEvent) *Flow {
	now := time.Now().UTC()
	return &Flow{
		ID:         id,
		ProjectID:  projectID,
		TemplateID: TemplateNewProject,
		Status:     FlowStatusActive,
		Stages:     []Stage{{ID: "stage-1", Type: StageTypeIdea, Name: "想法", Canvas: "intent", Status: StageStatusActive}},
		Artifacts:  []Artifact{},
		Events:     events,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

func testLegacyEvent(id, typ, stageID, message string) FlowEvent {
	return FlowEvent{ID: id, Type: typ, StageID: stageID, Message: message, Timestamp: time.Now().UTC()}
}

// TestLegacyFlowPutAndOutboxRollbackTogether is the test the plan names
// verbatim (§28 T1.05.c). It proves the two directions of "one transaction":
// a failed outbox insert must not leave the flows row, and a failed flows
// update must not leave outbox rows — then that a successful Put queues exactly
// the new events, survives a reopen, and that the outbox table is added to a
// database written before this feature existed.
func TestLegacyFlowPutAndOutboxRollbackTogether(t *testing.T) {
	store, dbPath := newLegacyTestStore(t)

	flowID := "flow-rollback"
	v1 := testLegacyFlow(flowID, "proj-1", testLegacyEvent("ev-1", "flow.created", "", "created"))
	if err := store.Put(v1); err != nil {
		t.Fatalf("put v1: %v", err)
	}
	if got := legacyOutboxCount(t, store); got != 1 {
		t.Fatalf("outbox rows after first put = %d, want 1", got)
	}
	v1Payload, v1UpdatedAt := storedFlowRow(t, store, flowID)

	v2 := testLegacyFlow(flowID, "proj-1",
		testLegacyEvent("ev-1", "flow.created", "", "created"),
		testLegacyEvent("ev-2", "stage.done", "stage-1", "completed stage type=idea"))
	v2.UpdatedAt = v1.UpdatedAt.Add(time.Second)

	t.Run("outbox insert failure rolls the flows row back", func(t *testing.T) {
		if _, err := store.db.Exec(`CREATE TRIGGER legacy_test_block_outbox_insert
			BEFORE INSERT ON flow_event_outbox
			BEGIN SELECT RAISE(ABORT, 'outbox insert blocked for test'); END;`); err != nil {
			t.Fatalf("create trigger: %v", err)
		}
		defer func() {
			if _, err := store.db.Exec(`DROP TRIGGER legacy_test_block_outbox_insert`); err != nil {
				t.Fatalf("drop trigger: %v", err)
			}
		}()

		err := store.Put(v2)
		if err == nil {
			t.Fatal("Put succeeded although the outbox insert was blocked")
		}
		if !strings.Contains(err.Error(), "queue event") {
			t.Fatalf("error does not name the outbox write: %v", err)
		}
		payload, updatedAt := storedFlowRow(t, store, flowID)
		if payload != v1Payload || updatedAt != v1UpdatedAt {
			t.Fatalf("flows row changed although the transaction failed:\npayload same=%v updated_at %d -> %d",
				payload == v1Payload, v1UpdatedAt, updatedAt)
		}
		if got := legacyOutboxCount(t, store); got != 1 {
			t.Fatalf("outbox rows after failed put = %d, want the original 1", got)
		}
	})

	t.Run("flows update failure leaves no outbox row", func(t *testing.T) {
		if _, err := store.db.Exec(`CREATE TRIGGER legacy_test_block_flows_update
			BEFORE UPDATE ON flows
			BEGIN SELECT RAISE(ABORT, 'flows update blocked for test'); END;`); err != nil {
			t.Fatalf("create trigger: %v", err)
		}
		defer func() {
			if _, err := store.db.Exec(`DROP TRIGGER legacy_test_block_flows_update`); err != nil {
				t.Fatalf("drop trigger: %v", err)
			}
		}()

		err := store.Put(v2)
		if err == nil {
			t.Fatal("Put succeeded although the flows update was blocked")
		}
		if got := legacyOutboxCount(t, store); got != 1 {
			t.Fatalf("outbox rows after failed flows update = %d, want the original 1", got)
		}
		payload, updatedAt := storedFlowRow(t, store, flowID)
		if payload != v1Payload || updatedAt != v1UpdatedAt {
			t.Fatal("flows row changed although the update was blocked")
		}
	})

	t.Run("successful put queues only the new event and survives a reopen", func(t *testing.T) {
		if err := store.Put(v2); err != nil {
			t.Fatalf("put v2: %v", err)
		}
		rows := readLegacyOutboxRows(t, store, "flow_id = ?", flowID)
		if len(rows) != 2 {
			t.Fatalf("outbox rows = %d, want 2 (ev-1, ev-2)", len(rows))
		}
		if rows[0].sourceEventID != "ev-1" || rows[1].sourceEventID != "ev-2" {
			t.Fatalf("outbox order = [%s %s], want [ev-1 ev-2]", rows[0].sourceEventID, rows[1].sourceEventID)
		}
		if rows[0].seq >= rows[1].seq {
			t.Fatalf("seq must grow with the local order: %d then %d", rows[0].seq, rows[1].seq)
		}
		for _, r := range rows {
			if r.state != "pending" || r.attemptCount != 0 || !r.nextAttemptAt.Valid {
				t.Fatalf("new row is not a pending attempt-0 row with a due time: %+v", r)
			}
		}

		// A repeated Put of the same document is a no-op for the outbox.
		if err := store.Put(v2); err != nil {
			t.Fatalf("repeat put v2: %v", err)
		}
		if got := legacyOutboxCount(t, store); got != 2 {
			t.Fatalf("outbox rows after repeating the same document = %d, want 2", got)
		}

		if err := store.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
		reopened, err := NewSQLiteFlowStore(dbPath)
		if err != nil {
			t.Fatalf("reopen: %v", err)
		}
		defer reopened.Close()
		after := readLegacyOutboxRows(t, reopened, "flow_id = ?", flowID)
		if len(after) != 2 {
			t.Fatalf("outbox rows after reopen = %d, want 2", len(after))
		}
		if after[0].sourceEventID != "ev-1" || after[1].sourceEventID != "ev-2" {
			t.Fatalf("outbox order after reopen = [%s %s]", after[0].sourceEventID, after[1].sourceEventID)
		}
		flow, err := reopened.Get(flowID)
		if err != nil {
			t.Fatalf("get after reopen: %v", err)
		}
		if len(flow.Events) != 2 || flow.UpdatedAt.UnixMilli() != v2.UpdatedAt.UTC().UnixMilli() {
			t.Fatalf("reopened document does not match the last Put: %+v", flow)
		}
	})

	t.Run("a database written before the outbox existed gains the table", func(t *testing.T) {
		legacyPath := filepath.Join(t.TempDir(), "legacy.db")
		old, err := NewSQLiteFlowStore(legacyPath)
		if err != nil {
			t.Fatalf("open legacy store: %v", err)
		}
		// Simulate a database from a build that had no outbox at all.
		if _, err := old.db.Exec(`DROP TABLE flow_event_outbox`); err != nil {
			t.Fatalf("drop outbox table: %v", err)
		}
		if err := old.Close(); err != nil {
			t.Fatalf("close legacy store: %v", err)
		}

		upgraded, err := NewSQLiteFlowStore(legacyPath)
		if err != nil {
			t.Fatalf("reopen legacy store: %v", err)
		}
		defer upgraded.Close()
		if err := upgraded.Put(testLegacyFlow("flow-upgraded", "proj-1",
			testLegacyEvent("ev-upgraded", "flow.created", "", "created"))); err != nil {
			t.Fatalf("put after upgrade: %v", err)
		}
		if got := legacyOutboxCount(t, upgraded); got != 1 {
			t.Fatalf("outbox rows after upgrading an old database = %d, want 1", got)
		}
	})
}

// TestLegacyFlowPutQueuesOnlyNewEvents covers the upgrade rule: a Flow whose
// document already holds a timeline is not back-filled by the next Put, only the
// events the new document adds are queued. The pre-existing document is inserted
// directly, the way a database written before T1.05.c looks.
func TestLegacyFlowPutQueuesOnlyNewEvents(t *testing.T) {
	store, _ := newLegacyTestStore(t)
	const flowID = "flow-preexisting"

	pre := testLegacyFlow(flowID, "proj-7",
		testLegacyEvent("old-1", "flow.created", "", "created before the outbox existed"),
		testLegacyEvent("old-2", "stage.done", "stage-1", "old stage completion"))
	payload, err := json.Marshal(pre)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`
INSERT INTO flows (id, project_id, template_id, status, payload, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`, pre.ID, pre.ProjectID, string(pre.TemplateID), string(pre.Status),
		string(payload), pre.CreatedAt.UTC().UnixMilli(), pre.UpdatedAt.UTC().UnixMilli()); err != nil {
		t.Fatalf("insert pre-existing document: %v", err)
	}

	next := testLegacyFlow(flowID, "proj-7",
		testLegacyEvent("old-1", "flow.created", "", "created before the outbox existed"),
		testLegacyEvent("old-2", "stage.done", "stage-1", "old stage completion"),
		testLegacyEvent("new-1", "stage.active", "stage-2", "activated type=design"))
	if err := store.Put(next); err != nil {
		t.Fatalf("put: %v", err)
	}
	rows := readLegacyOutboxRows(t, store, "flow_id = ?", flowID)
	if len(rows) != 1 {
		t.Fatalf("queued %d events, want exactly the 1 new event (history is T3.01's job)", len(rows))
	}
	if rows[0].sourceEventID != "new-1" {
		t.Fatalf("queued %s, want new-1", rows[0].sourceEventID)
	}
	if rows[0].eventType != "stage.active" || rows[0].stageID != "stage-2" ||
		rows[0].message != "activated type=design" || rows[0].projectID != "proj-7" {
		t.Fatalf("queued row does not mirror the FlowEvent: %+v", rows[0])
	}
	if rows[0].occurredAt != next.Events[2].Timestamp.UTC().UnixMilli() {
		t.Fatalf("occurred_at = %d, want the event timestamp %d",
			rows[0].occurredAt, next.Events[2].Timestamp.UTC().UnixMilli())
	}
}

// TestLegacyEngineQueuesItsTimeline drives the real engine over the SQLite store
// and checks that every event the engine appended is queued, in the engine's own
// order, with the engine's own field values.
func TestLegacyEngineQueuesItsTimeline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "floweng.db")
	eng, err := NewSQLiteEngine(path, nil)
	if err != nil {
		t.Fatalf("new sqlite engine: %v", err)
	}
	defer eng.Close()
	store, ok := eng.store.(*SQLiteFlowStore)
	if !ok {
		t.Fatalf("engine store is %T, want *SQLiteFlowStore", eng.store)
	}

	flow, err := eng.Create(context.Background(), &CreateFlowRequest{ProjectID: "proj-engine", TemplateID: TemplateNewProject})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	rows := readLegacyOutboxRows(t, store, "flow_id = ?", flow.ID)
	if len(rows) != 1 || rows[0].eventType != "flow.created" {
		t.Fatalf("after Create the outbox = %+v, want one flow.created", rows)
	}

	advanced, err := eng.Advance(context.Background(), flow.ID, &AdvanceRequest{})
	if err != nil {
		t.Fatalf("advance: %v", err)
	}
	rows = readLegacyOutboxRows(t, store, "flow_id = ?", flow.ID)
	if len(rows) != len(advanced.Events) {
		t.Fatalf("outbox rows = %d, engine events = %d", len(rows), len(advanced.Events))
	}
	if len(advanced.Events) < 3 {
		t.Fatalf("advance produced %d events (%v), expected the created + done + active set",
			len(advanced.Events), flowEventTypes(advanced.Events))
	}
	for i, ev := range advanced.Events {
		r := rows[i]
		if r.sourceEventID != ev.ID {
			t.Fatalf("row %d is %s, engine event %d is %s (order drifted)", i, r.sourceEventID, i, ev.ID)
		}
		if r.eventType != ev.Type || r.stageID != ev.StageID || r.message != ev.Message {
			t.Fatalf("row %d does not mirror event %d: row=%+v event=%+v", i, i, r, ev)
		}
		if r.flowID != flow.ID || r.projectID != "proj-engine" {
			t.Fatalf("row %d has flow/project %s/%s, want %s/proj-engine", i, r.flowID, r.projectID, flow.ID)
		}
		if r.occurredAt != ev.Timestamp.UTC().UnixMilli() {
			t.Fatalf("row %d occurred_at = %d, want %d", i, r.occurredAt, ev.Timestamp.UTC().UnixMilli())
		}
	}
	if !flowHasEvent(advanced.Events, "stage.done") || !flowHasEvent(advanced.Events, "stage.active") {
		t.Fatalf("engine did not produce the expected transition events: %v", flowEventTypes(advanced.Events))
	}
}

// TestLegacyPutRejectsEmptyEventID pins the refusal of a fact that cannot be
// identified: an event without an id would collapse onto one outbox row, so Put
// fails and neither table changes.
func TestLegacyPutRejectsEmptyEventID(t *testing.T) {
	store, _ := newLegacyTestStore(t)
	const flowID = "flow-empty-id"

	good := testLegacyFlow(flowID, "proj-1", testLegacyEvent("ev-1", "flow.created", "", "created"))
	if err := store.Put(good); err != nil {
		t.Fatalf("put: %v", err)
	}
	payloadBefore, updatedBefore := storedFlowRow(t, store, flowID)

	broken := testLegacyFlow(flowID, "proj-1",
		testLegacyEvent("ev-1", "flow.created", "", "created"),
		testLegacyEvent("", "stage.done", "stage-1", "anonymous completion"))
	broken.UpdatedAt = good.UpdatedAt.Add(time.Minute)

	err := store.Put(broken)
	if err == nil {
		t.Fatal("Put accepted an event with an empty id")
	}
	if !strings.Contains(err.Error(), "empty id") {
		t.Fatalf("error does not name the empty id: %v", err)
	}
	payloadAfter, updatedAfter := storedFlowRow(t, store, flowID)
	if payloadAfter != payloadBefore || updatedAfter != updatedBefore {
		t.Fatal("flows row changed although the Put was rejected")
	}
	if got := legacyOutboxCount(t, store); got != 1 {
		t.Fatalf("outbox rows = %d, want the original 1", got)
	}
}

// TestLegacyPutRejectsUnreadableStoredDocument covers the other refusal: when the
// stored document cannot be parsed, the set of "events already known" is
// unknown, and guessing either way would duplicate or drop a timeline.
func TestLegacyPutRejectsUnreadableStoredDocument(t *testing.T) {
	store, _ := newLegacyTestStore(t)
	const flowID = "flow-corrupt"

	if _, err := store.db.Exec(`
INSERT INTO flows (id, project_id, template_id, status, payload, created_at, updated_at)
VALUES (?, 'proj-1', 'new_project', 'active', ?, 1, 1)`, flowID, `{"id":"flow-corrupt","events":[{`); err != nil {
		t.Fatalf("insert corrupt document: %v", err)
	}

	err := store.Put(testLegacyFlow(flowID, "proj-1", testLegacyEvent("ev-1", "flow.created", "", "created")))
	if err == nil {
		t.Fatal("Put accepted a document whose stored copy is not readable JSON")
	}
	if !strings.Contains(err.Error(), "not readable JSON") {
		t.Fatalf("error does not explain the unreadable document: %v", err)
	}
	payload, _ := storedFlowRow(t, store, flowID)
	if payload != `{"id":"flow-corrupt","events":[{` {
		t.Fatalf("the corrupt document was overwritten: %s", payload)
	}
	if got := legacyOutboxCount(t, store); got != 0 {
		t.Fatalf("outbox rows = %d, want 0", got)
	}
}

// TestLegacyDeleteKeepsOutboxRows: events are facts. Deleting the Flow document
// must not delete the timeline rows a projector has not read yet.
func TestLegacyDeleteKeepsOutboxRows(t *testing.T) {
	store, _ := newLegacyTestStore(t)
	const flowID = "flow-deleted"
	if err := store.Put(testLegacyFlow(flowID, "proj-1",
		testLegacyEvent("ev-1", "flow.created", "", "created"),
		testLegacyEvent("ev-2", "stage.done", "stage-1", "done"))); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := store.Delete(flowID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got := legacyOutboxCount(t, store); got != 2 {
		t.Fatalf("outbox rows after Delete = %d, want 2", got)
	}
	due, err := store.DueLegacyEvents(time.Now().UTC(), 10)
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	if len(due) != 1 || due[0].SourceEventID != "ev-1" {
		t.Fatalf("due after Delete = %+v, want ev-1 only (per-Flow order)", due)
	}
}

// TestLegacyDueEventsOrdersAndBlocksPerFlow is the claim rule: due rows come back
// in local order, a Flow contributes only its earliest pending event, a Flow
// whose head is in its backoff window contributes nothing (its later events must
// not overtake it), and other Flows are unaffected. limit is honoured.
func TestLegacyDueEventsOrdersAndBlocksPerFlow(t *testing.T) {
	store, _ := newLegacyTestStore(t)

	for _, f := range []struct {
		id    string
		types []string
	}{
		{"flow-A", []string{"a-1", "a-2", "a-3"}},
		{"flow-B", []string{"b-1", "b-2"}},
	} {
		events := make([]FlowEvent, 0, len(f.types))
		for _, id := range f.types {
			events = append(events, testLegacyEvent(id, "stage.done", "stage-1", id))
		}
		if err := store.Put(testLegacyFlow(f.id, "proj-1", events...)); err != nil {
			t.Fatalf("put %s: %v", f.id, err)
		}
	}
	// now is read after the writes: a row is created due immediately, and a
	// `now` captured before them could fall a millisecond short of its due time.
	now := time.Now().UTC()

	due, err := store.DueLegacyEvents(now, 10)
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	if ids := dueIDs(due); strings.Join(ids, ",") != "a-1,b-1" {
		t.Fatalf("due = %v, want [a-1 b-1] (each Flow's head only)", ids)
	}
	if due[0].Attempts != 0 {
		t.Fatalf("attempts = %d, want 0 (a claim must not spend an attempt)", due[0].Attempts)
	}
	if due[0].ProjectID != "proj-1" || due[0].FlowID != "flow-A" || due[0].Type != "stage.done" {
		t.Fatalf("claimed event lost its identity: %+v", due[0])
	}
	if !due[0].OccurredAt.Equal(time.UnixMilli(due[0].OccurredAt.UnixMilli()).UTC()) {
		t.Fatalf("occurred_at is not millisecond-precision UTC: %v", due[0].OccurredAt)
	}

	if _, err := store.db.Exec(`UPDATE flow_event_outbox SET next_attempt_at = ? WHERE source_event_id = 'a-1'`,
		now.Add(time.Minute).UnixMilli()); err != nil {
		t.Fatalf("push a-1 back: %v", err)
	}
	due, err = store.DueLegacyEvents(now, 10)
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	if ids := dueIDs(due); strings.Join(ids, ",") != "b-1" {
		t.Fatalf("due with a-1 retrying = %v, want [b-1]: a-2/a-3 must not overtake a-1", ids)
	}

	// Past the backoff window a-1 is due again, and a-2/a-3 are still behind it.
	due, err = store.DueLegacyEvents(now.Add(2*time.Minute), 10)
	if err != nil {
		t.Fatalf("due after backoff: %v", err)
	}
	if ids := dueIDs(due); strings.Join(ids, ",") != "a-1,b-1" {
		t.Fatalf("due after the backoff window = %v, want [a-1 b-1]", ids)
	}

	// limit bounds the claim (one Flow's head here), not the number of Flows the
	// rule applies to. a-1 is reached through now+2min because it is still in its
	// backoff window at plain now.
	due, err = store.DueLegacyEvents(now.Add(2*time.Minute), 1)
	if err != nil {
		t.Fatalf("due limit 1: %v", err)
	}
	if len(due) != 1 || due[0].SourceEventID != "a-1" {
		t.Fatalf("due with limit 1 = %v, want just a-1", dueIDs(due))
	}
	if _, err := store.DueLegacyEvents(now, 0); err == nil {
		t.Fatal("a non-positive limit was accepted")
	}

	// A dead-lettered head stops blocking its Flow; a retrying head does not.
	if err := store.MarkLegacyEventDead("b-1", "poisonous event"); err != nil {
		t.Fatalf("dead-letter b-1: %v", err)
	}
	due, err = store.DueLegacyEvents(now, 10)
	if err != nil {
		t.Fatalf("due after dead letter: %v", err)
	}
	if ids := dueIDs(due); strings.Join(ids, ",") != "b-2" {
		t.Fatalf("due after dead-lettering b-1 = %v, want [b-2] (a-1 still in backoff)", ids)
	}
}

func dueIDs(events []LegacyOutboxEvent) []string {
	out := make([]string, 0, len(events))
	for _, ev := range events {
		out = append(out, ev.SourceEventID)
	}
	return out
}

// TestLegacyMarkProjectedCAS walks every branch of the projected write-back.
func TestLegacyMarkProjectedCAS(t *testing.T) {
	store, _ := newLegacyTestStore(t)
	if err := store.Put(testLegacyFlow("flow-CAS", "proj-1",
		testLegacyEvent("ev-p", "flow.created", "", "created"),
		testLegacyEvent("ev-d", "stage.done", "stage-1", "done"))); err != nil {
		t.Fatalf("put: %v", err)
	}
	now := time.Now().UTC()

	if err := store.MarkLegacyEventProjected("ev-p", "runtime-1", now); err != nil {
		t.Fatalf("mark projected: %v", err)
	}
	rows := readLegacyOutboxRows(t, store, "source_event_id = ?", "ev-p")
	if len(rows) != 1 {
		t.Fatalf("rows = %d", len(rows))
	}
	r := rows[0]
	if r.state != "projected" || !r.projectedEventID.Valid || r.projectedEventID.String != "runtime-1" {
		t.Fatalf("row after mark = %+v", r)
	}
	if r.nextAttemptAt.Valid {
		t.Fatalf("a projected row must not keep a due time: %+v", r)
	}
	if !r.projectedAt.Valid || r.projectedAt.Int64 != now.UnixMilli() {
		t.Fatalf("projected_at = %+v, want %d", r.projectedAt, now.UnixMilli())
	}

	// Idempotent replay: the same source event, the same runtime event.
	if err := store.MarkLegacyEventProjected("ev-p", "runtime-1", now.Add(time.Hour)); err != nil {
		t.Fatalf("re-marking with the same projected id must succeed: %v", err)
	}
	// The same source event cannot become a different runtime event.
	if err := store.MarkLegacyEventProjected("ev-p", "runtime-2", now); err == nil {
		t.Fatal("re-marking with a different projected id was accepted")
	} else if !strings.Contains(err.Error(), "already projected") {
		t.Fatalf("unexpected error: %v", err)
	}
	// Unknown source event.
	if err := store.MarkLegacyEventProjected("ev-missing", "runtime-9", now); err == nil {
		t.Fatal("marking an unknown source event was accepted")
	}
	// Blank arguments.
	if err := store.MarkLegacyEventProjected("", "runtime-9", now); err == nil {
		t.Fatal("an empty source event id was accepted")
	}
	if err := store.MarkLegacyEventProjected("ev-d", "", now); err == nil {
		t.Fatal("an empty projected event id was accepted")
	}
	// Dead-lettered rows are not revived.
	if err := store.MarkLegacyEventDead("ev-d", "poison"); err != nil {
		t.Fatalf("dead-letter: %v", err)
	}
	if err := store.MarkLegacyEventProjected("ev-d", "runtime-3", now); err == nil {
		t.Fatal("a dead-lettered row was marked projected")
	} else if !strings.Contains(err.Error(), "dead_letter") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestLegacyMarkRetryAndDeadOnlyTouchPending: the retry/dead write-backs are
// compare-and-sets too. A row another round already decided must keep its
// outcome, and its attempt count must not move.
func TestLegacyMarkRetryAndDeadOnlyTouchPending(t *testing.T) {
	store, _ := newLegacyTestStore(t)
	if err := store.Put(testLegacyFlow("flow-mark", "proj-1",
		testLegacyEvent("ev-r", "flow.created", "", "created"),
		testLegacyEvent("ev-p", "stage.done", "stage-1", "done"))); err != nil {
		t.Fatalf("put: %v", err)
	}
	now := time.Now().UTC()

	next := now.Add(30 * time.Second)
	if err := store.MarkLegacyEventRetry("ev-r", "target unavailable", next); err != nil {
		t.Fatalf("retry: %v", err)
	}
	r := readLegacyOutboxRows(t, store, "source_event_id = ?", "ev-r")[0]
	if r.state != "pending" || r.attemptCount != 1 || r.lastError.String != "target unavailable" {
		t.Fatalf("row after retry = %+v", r)
	}
	if !r.nextAttemptAt.Valid || r.nextAttemptAt.Int64 != next.UnixMilli() {
		t.Fatalf("next_attempt_at = %+v, want %d", r.nextAttemptAt, next.UnixMilli())
	}
	if err := store.MarkLegacyEventRetry("ev-r", "still unavailable", next); err != nil {
		t.Fatalf("second retry: %v", err)
	}
	if r = readLegacyOutboxRows(t, store, "source_event_id = ?", "ev-r")[0]; r.attemptCount != 2 {
		t.Fatalf("attempt_count after two retries = %d, want 2", r.attemptCount)
	}
	if err := store.MarkLegacyEventRetry("ev-missing", "nope", next); err == nil {
		t.Fatal("retrying an unknown source event was accepted")
	}

	if err := store.MarkLegacyEventProjected("ev-r", "runtime-1", now); err != nil {
		t.Fatalf("mark projected: %v", err)
	}
	if err := store.MarkLegacyEventRetry("ev-r", "late failure", next); err == nil {
		t.Fatal("a projected row was retried")
	}
	if err := store.MarkLegacyEventDead("ev-r", "late death"); err == nil {
		t.Fatal("a projected row was dead-lettered")
	}
	if r = readLegacyOutboxRows(t, store, "source_event_id = ?", "ev-r")[0]; r.state != "projected" || r.attemptCount != 2 {
		t.Fatalf("a rejected write-back changed the row: %+v", r)
	}

	if err := store.MarkLegacyEventDead("ev-p", "poisonous event"); err != nil {
		t.Fatalf("dead-letter: %v", err)
	}
	d := readLegacyOutboxRows(t, store, "source_event_id = ?", "ev-p")[0]
	if d.state != "dead_letter" || d.attemptCount != 1 || d.nextAttemptAt.Valid || d.lastError.String != "poisonous event" {
		t.Fatalf("row after dead letter = %+v", d)
	}
	if err := store.MarkLegacyEventDead("ev-p", "again"); err == nil {
		t.Fatal("a dead-lettered row was dead-lettered twice")
	}
	if err := store.MarkLegacyEventDead("ev-missing", "nope"); err == nil {
		t.Fatal("dead-lettering an unknown source event was accepted")
	}
}

// TestLegacyLastErrorIsTruncatedOnARuneBoundary: last_error is for an operator,
// not a log, and it must stay valid UTF-8.
func TestLegacyLastErrorIsTruncatedOnARuneBoundary(t *testing.T) {
	store, _ := newLegacyTestStore(t)
	if err := store.Put(testLegacyFlow("flow-trunc", "proj-1",
		testLegacyEvent("ev-1", "flow.created", "", "created"),
		testLegacyEvent("ev-2", "stage.done", "stage-1", "done"))); err != nil {
		t.Fatalf("put: %v", err)
	}

	// 200 three-byte runes = 600 bytes, so the cut lands inside a rune unless it
	// is moved to a boundary.
	long := strings.Repeat("错", 200)
	if err := store.MarkLegacyEventRetry("ev-1", long, time.Now().UTC()); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if err := store.MarkLegacyEventDead("ev-2", long); err != nil {
		t.Fatalf("dead: %v", err)
	}
	for _, id := range []string{"ev-1", "ev-2"} {
		got := readLegacyOutboxRows(t, store, "source_event_id = ?", id)[0].lastError.String
		if len(got) > MaxLegacyLastErrorBytes {
			t.Fatalf("%s: stored %d bytes, want at most %d", id, len(got), MaxLegacyLastErrorBytes)
		}
		if !utf8.ValidString(got) {
			t.Fatalf("%s: stored text is not valid UTF-8: %q", id, got)
		}
		if !strings.HasPrefix(long, got) {
			t.Fatalf("%s: stored text is not a prefix of the failure", id)
		}
		if len(got) == 0 {
			t.Fatalf("%s: stored text is empty", id)
		}
	}
}

// TestLegacyOutboxBacklogCountsWork: the backlog is the number an operator (or a
// later readiness probe) reads, so it must count states, not rows.
func TestLegacyOutboxBacklogCountsWork(t *testing.T) {
	store, _ := newLegacyTestStore(t)
	if err := store.Put(testLegacyFlow("flow-backlog", "proj-1",
		testLegacyEvent("ev-1", "flow.created", "", "created"),
		testLegacyEvent("ev-2", "stage.done", "stage-1", "done"),
		testLegacyEvent("ev-3", "stage.active", "stage-2", "activated"))); err != nil {
		t.Fatalf("put: %v", err)
	}
	pending, dead, err := store.LegacyOutboxBacklog()
	if err != nil {
		t.Fatalf("backlog: %v", err)
	}
	if pending != 3 || dead != 0 {
		t.Fatalf("backlog = %d pending / %d dead, want 3/0", pending, dead)
	}

	if err := store.MarkLegacyEventProjected("ev-1", "runtime-1", time.Now().UTC()); err != nil {
		t.Fatalf("mark: %v", err)
	}
	if err := store.MarkLegacyEventDead("ev-2", "poison"); err != nil {
		t.Fatalf("dead: %v", err)
	}
	pending, dead, err = store.LegacyOutboxBacklog()
	if err != nil {
		t.Fatalf("backlog: %v", err)
	}
	if pending != 1 || dead != 1 {
		t.Fatalf("backlog = %d pending / %d dead, want 1/1", pending, dead)
	}
}

// TestLegacyOutboxSchemaGuards rejects the states the CHECK constraints exist to
// make impossible, so a future edit cannot quietly relax them.
func TestLegacyOutboxSchemaGuards(t *testing.T) {
	store, _ := newLegacyTestStore(t)
	if err := store.Put(testLegacyFlow("flow-checks", "proj-1",
		testLegacyEvent("ev-1", "flow.created", "", "created"))); err != nil {
		t.Fatalf("put: %v", err)
	}
	now := time.Now().UTC().UnixMilli()

	cases := []struct {
		name string
		sql  string
		args []any
	}{
		{"pending without a due time", `UPDATE flow_event_outbox SET next_attempt_at = NULL WHERE source_event_id = 'ev-1'`, nil},
		{"projected without an id", `UPDATE flow_event_outbox SET state = 'projected', next_attempt_at = NULL, projected_at = ? WHERE source_event_id = 'ev-1'`, []any{now}},
		{"negative attempts", `UPDATE flow_event_outbox SET attempt_count = -1 WHERE source_event_id = 'ev-1'`, nil},
		{"unknown state", `UPDATE flow_event_outbox SET state = 'in_flight' WHERE source_event_id = 'ev-1'`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := store.db.Exec(tc.sql, tc.args...); err == nil {
				t.Fatalf("the schema accepted %s", tc.name)
			}
		})
	}

	// The unique source event id is what makes a repeated Put a no-op rather
	// than a duplicate.
	_, err := store.db.Exec(`
INSERT INTO flow_event_outbox (source_event_id, flow_id, project_id, event_type, occurred_at, state, next_attempt_at, created_at)
VALUES ('ev-1', 'flow-checks', 'proj-1', 'flow.created', ?, 'pending', ?, ?)`, now, now, now)
	if err == nil {
		t.Fatal("a duplicate source_event_id was accepted")
	}
}

// TestLegacyDueEventsAfterRestartIsIdempotentAndComplete is the restart story in
// one place: a claim changes nothing, so an interrupted round leaves the rows
// exactly as they were, and a fresh store sees the same work.
func TestLegacyDueEventsAfterRestartIsIdempotentAndComplete(t *testing.T) {
	store, path := newLegacyTestStore(t)
	if err := store.Put(testLegacyFlow("flow-restart", "proj-1",
		testLegacyEvent("ev-1", "flow.created", "", "created"),
		testLegacyEvent("ev-2", "stage.done", "stage-1", "done"))); err != nil {
		t.Fatalf("put: %v", err)
	}
	now := time.Now().UTC()
	first, err := store.DueLegacyEvents(now, 10)
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	if len(first) != 1 || first[0].SourceEventID != "ev-1" {
		t.Fatalf("first claim = %v", dueIDs(first))
	}
	if pending, _, err := store.LegacyOutboxBacklog(); err != nil || pending != 2 {
		t.Fatalf("a claim must not change the backlog: pending=%d err=%v", pending, err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened, err := NewSQLiteFlowStore(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	second, err := reopened.DueLegacyEvents(now, 10)
	if err != nil {
		t.Fatalf("due after restart: %v", err)
	}
	if len(second) != 1 || second[0].SourceEventID != "ev-1" {
		t.Fatalf("claim after restart = %v, want the same ev-1 (at-least-once)", dueIDs(second))
	}
}

// memoryStore has no outbox: the projection path exists only for the durable
// store. This is the compile-time statement of that (the methods below exist on
// *SQLiteFlowStore and not on the FlowStore interface), plus a runtime check
// that the interface the engine depends on is unchanged.
func TestFlowStoreInterfaceUnchangedByOutbox(t *testing.T) {
	var store FlowStore = newMemoryStore()
	if err := store.Put(testLegacyFlow("flow-memory", "proj-1",
		testLegacyEvent("ev-1", "flow.created", "", "created"))); err != nil {
		t.Fatalf("memory put: %v", err)
	}
	if _, err := store.Get("flow-memory"); err != nil {
		t.Fatalf("memory get: %v", err)
	}
	if _, ok := any(store).(*SQLiteFlowStore); ok {
		t.Fatal("memoryStore must not be a SQLiteFlowStore")
	}
}
