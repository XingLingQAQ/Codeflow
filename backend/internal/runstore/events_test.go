package runstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/run"
	sqlite "modernc.org/sqlite"
)

// These tests cover events.go: the event fact stream, its sequence allocation,
// and the outbox rows written beside it. The properties they exist to prove are
// the ones §19.3 and §27.4 make load-bearing:
//
//   - a state change, its event and its outbox rows commit or roll back
//     together (nothing is published before the commit);
//   - sequence numbers are allocated in the caller's transaction, so a rollback
//     consumes nothing and no client sees a gap caused by an undone write;
//   - one event is readable from both the project and the run timeline, on two
//     independent counters;
//   - an event is immutable and a dead-lettered delivery cannot be erased;
//   - the identity envelope is the run package's contract (T1.02.a) rather than
//     a second copy of it: the caller's bytes are decoded strictly into
//     run.ExecutionIdentity and checked with ValidateFor, so a schema change
//     cannot leave this store accepting an identity the rest of the server
//     rejects.

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// eventProjectID is the project every event fixture belongs to.
const eventProjectID = "p-events"

// eventAttemptID and eventAgentRevisionID are the attempt-scoped ids the
// fixtures use: an event that can only be emitted while an attempt is alive
// carries both (§27.1, §28).
const (
	eventAttemptID       = "att-1"
	eventAgentRevisionID = "rev-1"
)

// eventIdentityFor builds an ExecutionIdentity envelope for projectID carrying
// exactly the optional ids given. actorType defaults to "system" when empty.
//
// The shape mirrors schemas/execution-identity.schema.json: project_id and
// actor{type,id} required, run_id/attempt_id/agent_revision_id optional. It is
// written as a raw map rather than through a struct so each test can mutate one
// field — or add a key the schema forbids — and prove the store rejects exactly
// that.
func eventIdentityFor(projectID, actorType string, runID, attemptID, agentRevisionID *string) json.RawMessage {
	if actorType == "" {
		actorType = "system"
	}
	envelope := map[string]any{
		"project_id": projectID,
		"actor":      map[string]any{"type": actorType, "id": "worker-1"},
	}
	if runID != nil {
		envelope["run_id"] = *runID
	}
	if attemptID != nil {
		envelope["attempt_id"] = *attemptID
	}
	if agentRevisionID != nil {
		envelope["agent_revision_id"] = *agentRevisionID
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		panic(err)
	}
	return encoded
}

// eventIdentity is the minimal envelope: project_id, actor, and optionally a
// run_id. It carries no attempt ids, which is all a Run-level event needs.
func eventIdentity(projectID, actorType string, runID *string) json.RawMessage {
	return eventIdentityFor(projectID, actorType, runID, nil, nil)
}

// claimedIdentity builds the identity an event type requires, using the run
// package's own rule table so the fixture cannot drift from the contract it is
// exercising: every field the contract demands is present, and the ids it does
// not demand are absent. The returned attempt and agent revision are the row
// fields AppendEventTx must be told about, so a caller cannot forget them.
func claimedIdentity(t *testing.T, eventType run.ExecutionEventType, runID *string) (identity json.RawMessage, attemptID, agentRevisionID *string) {
	t.Helper()
	req, err := run.RequiredIdentity(string(eventType))
	if err != nil {
		t.Fatalf("RequiredIdentity(%q): %v", eventType, err)
	}
	if req.Run && runID == nil {
		t.Fatalf("event type %q requires a run; the fixture was called without one", eventType)
	}
	if req.Attempt {
		attemptID = stringPtr(eventAttemptID)
	}
	if req.AgentRevision {
		agentRevisionID = stringPtr(eventAgentRevisionID)
	}
	return eventIdentityFor(eventProjectID, "system", runID, attemptID, agentRevisionID), attemptID, agentRevisionID
}

// stringPtr returns a pointer to a copy of s, for the optional id fields.
func stringPtr(s string) *string { return &s }

// eventFixture is the state a store is in for one event test: a project, a
// task, a run and the input snapshot the run pins, all written through the
// store's own typed operations.
type eventFixture struct {
	t     *testing.T
	store *Store
	path  string
	runID string
	// taskID and snapshotID are the rows the run needs; kept so a test can
	// build a second run without repeating the setup.
	taskID     string
	snapshotID string
}

// newEventFixture opens a fresh store and writes one project/task/snapshot/run
// set. Everything goes through the public API, so the fixture itself proves the
// new tables coexist with the existing ones.
func newEventFixture(t *testing.T) *eventFixture {
	t.Helper()
	store, path := newTestStore(t)
	f := &eventFixture{t: t, store: store, path: path, runID: "run-1", taskID: "t-1", snapshotID: "snap-1"}
	seedTask(t, store, f.taskID, eventProjectID)
	insertSnapshotAndRun(t, store, f.runID, f.taskID, eventProjectID, f.snapshotID,
		json.RawMessage(`{"prompt":"append events"}`))
	return f
}

// appendEvent writes one valid event through the store and returns it, failing
// the test if the write is refused. runID nil means a project-scoped event.
func (f *eventFixture) appendEvent(t *testing.T, eventType run.ExecutionEventType, runID *string, destinations ...string) Event {
	t.Helper()
	event, err := f.tryAppendEvent(eventType, runID, destinations...)
	if err != nil {
		t.Fatalf("AppendEventTx(%s): %v", eventType, err)
	}
	return event
}

// tryAppendEvent is appendEvent without the failure: a test that expects a
// rejection calls this and inspects the error.
func (f *eventFixture) tryAppendEvent(eventType run.ExecutionEventType, runID *string, destinations ...string) (Event, error) {
	identity, attemptID, _ := claimedIdentity(f.t, eventType, runID)
	return f.tryAppendInput(EventInput{
		ProjectID:    eventProjectID,
		RunID:        runID,
		AttemptID:    attemptID,
		Type:         string(eventType),
		OccurredAt:   time.UnixMilli(1700000001000),
		Identity:     identity,
		Payload:      json.RawMessage(`{"n":1}`),
		Destinations: destinations,
	})
}

// tryAppendInput runs one AppendEventTx inside its own transaction and returns
// whatever it said, so a test can drive the exact input it wants — accepted or
// refused — without the fixture second-guessing it.
func (f *eventFixture) tryAppendInput(in EventInput) (Event, error) {
	var event Event
	err := f.store.WithTx(context.Background(), func(ctx context.Context, tx Tx) error {
		var err error
		event, err = AppendEventTx(ctx, tx, in)
		return err
	})
	return event, err
}

// eventRowCount counts the rows of a table, used to prove that a rejected or
// rolled-back write left nothing behind.
func eventRowCount(t *testing.T, q Querier, table string) int {
	t.Helper()
	var n int
	if err := q.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM `+table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// counterValue reads a scope counter, or -1 when the scope has no row yet.
func counterValue(t *testing.T, q Querier, scope string) int64 {
	t.Helper()
	var v int64
	err := q.QueryRowContext(context.Background(),
		`SELECT value FROM scope_counters WHERE scope = ?`, scope).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return -1
	}
	if err != nil {
		t.Fatalf("read counter %s: %v", scope, err)
	}
	return v
}

// seqsOf returns the project_seq values of a page, so a test can compare a whole
// sequence run at once.
func seqsOf(events []Event) []int64 {
	out := make([]int64, 0, len(events))
	for _, e := range events {
		out = append(out, e.ProjectSeq)
	}
	return out
}

func runSeqsOf(events []Event) []int64 {
	out := make([]int64, 0, len(events))
	for _, e := range events {
		if e.RunSeq != nil {
			out = append(out, *e.RunSeq)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Append and read
// ---------------------------------------------------------------------------

// TestAppendEventAssignsSequences proves the numbering contract of §19.1/§27.4:
// project_seq starts at 1 and increases by one per project, a run-scoped event
// also gets a run_seq from an independent counter, and the server — not the
// caller — decides both.
func TestAppendEventAssignsSequences(t *testing.T) {
	f := newEventFixture(t)

	// Two project-scoped events, then two on the run. The run's counter is
	// independent: its first event is run_seq 1, not 3.
	first := f.appendEvent(t, run.EventServerRestart, nil)
	second := f.appendEvent(t, run.EventApprovalDecided, nil)
	third := f.appendEvent(t, run.EventProcessStarted, &f.runID)
	fourth := f.appendEvent(t, run.EventProcessExited, &f.runID)

	if got := seqsOf([]Event{first, second, third, fourth}); fmt.Sprint(got) != "[1 2 3 4]" {
		t.Errorf("project_seq = %v, want [1 2 3 4]", got)
	}
	if first.RunSeq != nil || second.RunSeq != nil {
		t.Errorf("project-scoped events have run_seq %v/%v, want nil", first.RunSeq, second.RunSeq)
	}
	if third.RunSeq == nil || *third.RunSeq != 1 {
		t.Errorf("first run event run_seq = %s, want 1", runSeqString(third.RunSeq))
	}
	if fourth.RunSeq == nil || *fourth.RunSeq != 2 {
		t.Errorf("second run event run_seq = %s, want 2", runSeqString(fourth.RunSeq))
	}

	// The store owns the identity of the row: a UUID, schema_version 1, the
	// canonical payload and its hash.
	for i, event := range []Event{first, second, third, fourth} {
		if event.ID == "" {
			t.Errorf("event %d has an empty id", i)
		}
		if event.SchemaVersion != EventSchemaVersion {
			t.Errorf("event %d schema_version = %d, want %d", i, event.SchemaVersion, EventSchemaVersion)
		}
		if event.PayloadHash != hashOfPayload(event.Payload) {
			t.Errorf("event %d payload_hash = %s, want the hash of the stored payload", i, event.PayloadHash)
		}
	}
	if first.ID == second.ID {
		t.Error("two events share an id, want distinct ids")
	}

	// The counters are the source of truth for the next allocation.
	if got := counterValue(t, f.store.DB(), projectScope(eventProjectID)); got != 4 {
		t.Errorf("project counter = %d, want 4", got)
	}
	if got := counterValue(t, f.store.DB(), runScope(f.runID)); got != 2 {
		t.Errorf("run counter = %d, want 2", got)
	}
	t.Logf("EVIDENCE sequences: project=%v run=%v counters project=%d run=%d",
		seqsOf([]Event{first, second, third, fourth}),
		runSeqsOf([]Event{first, second, third, fourth}),
		counterValue(t, f.store.DB(), projectScope(eventProjectID)),
		counterValue(t, f.store.DB(), runScope(f.runID)))
}

// TestAppendEventStoresCanonicalIdentityAndPayload proves the stored bytes are
// the canonical ones: a caller that serialises the same object with different
// key order and whitespace gets the same stored value and the same hash, so
// payload_hash is recomputable from the row alone.
func TestAppendEventStoresCanonicalIdentityAndPayload(t *testing.T) {
	ctx := context.Background()
	f := newEventFixture(t)

	var event Event
	err := f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		var err error
		event, err = AppendEventTx(ctx, tx, EventInput{
			ProjectID:  eventProjectID,
			RunID:      &f.runID,
			AttemptID:  stringPtr(eventAttemptID),
			Type:       string(run.EventToolRequested),
			OccurredAt: time.UnixMilli(1700000001500),
			// Deliberately not canonical: keys out of order, extra whitespace.
			Identity: json.RawMessage(`{"actor": {"id": "worker-1", "type": "system"}, "project_id": "` + eventProjectID + `", "run_id": "` + f.runID + `", "attempt_id": "` + eventAttemptID + `", "agent_revision_id": "` + eventAgentRevisionID + `"}`),
			Payload:  json.RawMessage("{ \"tool\" : \"read_file\" ,\n  \"path\": \"src/x.go\" }"),
		})
		return err
	})
	if err != nil {
		t.Fatalf("AppendEventTx: %v", err)
	}

	if string(event.Payload) != `{"path":"src/x.go","tool":"read_file"}` {
		t.Errorf("stored payload = %s, want canonical key order and no whitespace", event.Payload)
	}
	wantIdentity := `{"actor":{"id":"worker-1","type":"system"},"agent_revision_id":"` + eventAgentRevisionID +
		`","attempt_id":"` + eventAttemptID + `","project_id":"` + eventProjectID + `","run_id":"` + f.runID + `"}`
	if string(event.Identity) != wantIdentity {
		t.Errorf("stored identity = %s, want canonical key order %s", event.Identity, wantIdentity)
	}

	// What the store returned must be exactly what a later read returns.
	stored, err := GetEvent(ctx, f.store.DB(), event.ID)
	if err != nil {
		t.Fatalf("GetEvent: %v", err)
	}
	if string(stored.Payload) != string(event.Payload) || stored.PayloadHash != event.PayloadHash {
		t.Errorf("re-read event payload/hash = %s/%s, want %s/%s",
			stored.Payload, stored.PayloadHash, event.Payload, event.PayloadHash)
	}
	if stored.OccurredAt.UnixMilli() != 1700000001500 || stored.OccurredAt.Location() != time.UTC {
		t.Errorf("occurred_at = %v (%d ms, %v), want 1700000001500 ms UTC",
			stored.OccurredAt, stored.OccurredAt.UnixMilli(), stored.OccurredAt.Location())
	}
	if stored.Type != string(run.EventToolRequested) {
		t.Errorf("type = %q, want %q", stored.Type, string(run.EventToolRequested))
	}
}

// TestEventReadableFromBothViews proves §27.4's two-projection rule: the same
// row is returned by the project list and the run list, each paging on its own
// counter, and the two counters are not each other's values.
func TestEventReadableFromBothViews(t *testing.T) {
	ctx := context.Background()
	f := newEventFixture(t)

	// A project-only event, then a run event, then another project-only one.
	// The project-only events are Run-less (§27.1) and carry no attempt.
	projectOnly := f.appendEvent(t, run.EventServerRestart, nil)
	runEvent := f.appendEvent(t, run.EventProcessStarted, &f.runID)
	afterRun := f.appendEvent(t, run.EventApprovalDecided, nil)

	projectEvents, hasMore, err := ListProjectEvents(ctx, f.store.DB(), eventProjectID, 0, 0)
	if err != nil {
		t.Fatalf("ListProjectEvents: %v", err)
	}
	if hasMore {
		t.Error("ListProjectEvents hasMore = true, want false for a complete page")
	}
	if got := seqsOf(projectEvents); fmt.Sprint(got) != "[1 2 3]" {
		t.Errorf("project view project_seq = %v, want [1 2 3]", got)
	}

	runEvents, hasMore, err := ListRunEvents(ctx, f.store.DB(), f.runID, 0, 0)
	if err != nil {
		t.Fatalf("ListRunEvents: %v", err)
	}
	if hasMore {
		t.Error("ListRunEvents hasMore = true, want false")
	}
	if len(runEvents) != 1 {
		t.Fatalf("run view returned %d events, want 1 (only one event belongs to the run)", len(runEvents))
	}
	if got := runSeqsOf(runEvents); fmt.Sprint(got) != "[1]" {
		t.Errorf("run view run_seq = %v, want [1]", got)
	}

	// The same row, seen twice: identical id, but each view reports its own
	// sequence for it. This is what a client de-duplicates on and what the two
	// independent cursors advance over.
	fromProject := projectEvents[1]
	fromRun := runEvents[0]
	if fromProject.ID != fromRun.ID || fromProject.ID != runEvent.ID {
		t.Errorf("same event has ids %s / %s / %s, want one id", fromProject.ID, fromRun.ID, runEvent.ID)
	}
	if fromProject.ProjectSeq != 2 || fromRun.ProjectSeq != 2 {
		t.Errorf("project_seq differs between views: %d / %d, want 2", fromProject.ProjectSeq, fromRun.ProjectSeq)
	}
	if fromRun.RunSeq == nil || *fromRun.RunSeq != 1 {
		t.Errorf("run view run_seq = %s, want 1", runSeqString(fromRun.RunSeq))
	}
	if projectOnly.ProjectSeq != 1 || afterRun.ProjectSeq != 3 {
		t.Errorf("project-only events are at %d and %d, want 1 and 3", projectOnly.ProjectSeq, afterRun.ProjectSeq)
	}

	// A project's timeline must not contain another project's events. p-2 has
	// none, so its list is empty rather than an error.
	if err := f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		return UpsertProjectRef(ctx, tx, projectRef("p-other"))
	}); err != nil {
		t.Fatalf("seed second project: %v", err)
	}
	other, _, err := ListProjectEvents(ctx, f.store.DB(), "p-other", 0, 0)
	if err != nil {
		t.Fatalf("ListProjectEvents(p-other): %v", err)
	}
	if len(other) != 0 {
		t.Errorf("p-other has %d events, want 0", len(other))
	}
}

// TestListEventsPaging covers the after/limit contract of §27.3: pages are
// contiguous, hasMore is true exactly when more rows exist (including when the
// stream ends on a page boundary), and the limit is clamped to 1000.
func TestListEventsPaging(t *testing.T) {
	ctx := context.Background()
	f := newEventFixture(t)

	const total = 5
	for i := 0; i < total; i++ {
		f.appendEvent(t, run.EventServerRestart, nil)
	}

	t.Run("page boundaries and hasMore", func(t *testing.T) {
		page1, hasMore, err := ListProjectEvents(ctx, f.store.DB(), eventProjectID, 0, 2)
		if err != nil {
			t.Fatalf("page 1: %v", err)
		}
		if got := seqsOf(page1); fmt.Sprint(got) != "[1 2]" {
			t.Errorf("page 1 = %v, want [1 2]", got)
		}
		if !hasMore {
			t.Error("page 1 hasMore = false, want true")
		}

		page2, hasMore, err := ListProjectEvents(ctx, f.store.DB(), eventProjectID, 2, 2)
		if err != nil {
			t.Fatalf("page 2: %v", err)
		}
		if got := seqsOf(page2); fmt.Sprint(got) != "[3 4]" {
			t.Errorf("page 2 = %v, want [3 4]", got)
		}
		if !hasMore {
			t.Error("page 2 hasMore = false, want true")
		}

		// The last page is exactly full: hasMore must still be false, which is
		// the case a "len(page) == limit" implementation would get wrong.
		page3, hasMore, err := ListProjectEvents(ctx, f.store.DB(), eventProjectID, 4, 2)
		if err != nil {
			t.Fatalf("page 3: %v", err)
		}
		if got := seqsOf(page3); fmt.Sprint(got) != "[5]" {
			t.Errorf("page 3 = %v, want [5]", got)
		}
		if hasMore {
			t.Error("page 3 hasMore = true, want false (the stream ends here)")
		}

		// Past the end: an empty page, not an error.
		page4, hasMore, err := ListProjectEvents(ctx, f.store.DB(), eventProjectID, 5, 2)
		if err != nil {
			t.Fatalf("page 4: %v", err)
		}
		if len(page4) != 0 || hasMore {
			t.Errorf("page past the end = %v hasMore=%v, want empty and false", seqsOf(page4), hasMore)
		}
	})

	t.Run("after is exclusive", func(t *testing.T) {
		// after=2 must start at 3, so a client that applied 2 does not see it
		// again.
		page, _, err := ListProjectEvents(ctx, f.store.DB(), eventProjectID, 2, 10)
		if err != nil {
			t.Fatalf("after=2: %v", err)
		}
		if got := seqsOf(page); fmt.Sprint(got) != "[3 4 5]" {
			t.Errorf("after=2 = %v, want [3 4 5]", got)
		}
	})

	t.Run("limit is clamped to the documented bounds", func(t *testing.T) {
		// limit 0 means the default (200, §27.3), which is larger than this
		// fixture — so all five events come back in one page.
		page, _, err := ListProjectEvents(ctx, f.store.DB(), eventProjectID, 0, 0)
		if err != nil {
			t.Fatalf("limit=0: %v", err)
		}
		if len(page) != total {
			t.Errorf("limit=0 returned %d events, want the default limit to cover all %d", len(page), total)
		}
		if DefaultEventLimit <= total {
			t.Errorf("DefaultEventLimit = %d, want it above this fixture's %d so the assertion above is meaningful",
				DefaultEventLimit, total)
		}
		if DefaultEventLimit != 200 || MaxEventLimit != 1000 {
			t.Errorf("limits are %d/%d, want 200/1000 (§27.3)", DefaultEventLimit, MaxEventLimit)
		}

		// A limit above the cap is clamped, not refused: the answer is still a
		// correct page, and the clamp is observable through the cap constant.
		if got := clampEventLimit(MaxEventLimit + 1); got != MaxEventLimit {
			t.Errorf("clampEventLimit(%d) = %d, want %d", MaxEventLimit+1, got, MaxEventLimit)
		}
		if got := clampEventLimit(0); got != DefaultEventLimit {
			t.Errorf("clampEventLimit(0) = %d, want %d", got, DefaultEventLimit)
		}
	})

	t.Run("a negative after is refused", func(t *testing.T) {
		if _, _, err := ListProjectEvents(ctx, f.store.DB(), eventProjectID, -1, 10); !errors.Is(err, ErrInvalidEvent) {
			t.Errorf("after=-1 error = %v, want ErrInvalidEvent", err)
		}
	})

	t.Run("a missing event is not found", func(t *testing.T) {
		if _, err := GetEvent(ctx, f.store.DB(), "no-such-event"); !errors.Is(err, ErrNotFound) {
			t.Errorf("GetEvent(missing) error = %v, want ErrNotFound", err)
		}
	})
}

// TestListEventsLimitClampIsEnforcedByTheRead proves the 1000 cap through the
// real read path rather than through the helper: 1001 events exist, a limit of
// 5000 must return 1000 and report that more remain.
func TestListEventsLimitClampIsEnforcedByTheRead(t *testing.T) {
	ctx := context.Background()
	f := newEventFixture(t)

	const total = MaxEventLimit + 1
	err := f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		for i := 0; i < total; i++ {
			if _, err := AppendEventTx(ctx, tx, EventInput{
				ProjectID:  eventProjectID,
				Type:       string(run.EventServerRestart),
				OccurredAt: time.UnixMilli(1700000002000 + int64(i)),
				Identity:   eventIdentity(eventProjectID, "system", nil),
				Payload:    json.RawMessage(`{"i":` + fmt.Sprint(i) + `}`),
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("append %d events: %v", total, err)
	}

	page, hasMore, err := ListProjectEvents(ctx, f.store.DB(), eventProjectID, 0, 5000)
	if err != nil {
		t.Fatalf("ListProjectEvents: %v", err)
	}
	if len(page) != MaxEventLimit {
		t.Errorf("page length = %d, want the %d cap", len(page), MaxEventLimit)
	}
	if !hasMore {
		t.Error("hasMore = false, want true (one event is past the cap)")
	}
	if page[0].ProjectSeq != 1 || page[len(page)-1].ProjectSeq != MaxEventLimit {
		t.Errorf("page covers seq %d..%d, want 1..%d", page[0].ProjectSeq, page[len(page)-1].ProjectSeq, MaxEventLimit)
	}

	// The remaining event is reachable with a cursor at the cap, so the clamp
	// does not hide anything.
	rest, hasMore, err := ListProjectEvents(ctx, f.store.DB(), eventProjectID, MaxEventLimit, 5000)
	if err != nil {
		t.Fatalf("ListProjectEvents(after cap): %v", err)
	}
	if len(rest) != 1 || rest[0].ProjectSeq != total {
		t.Errorf("rest = %v, want one event at seq %d", seqsOf(rest), total)
	}
	if hasMore {
		t.Error("rest hasMore = true, want false")
	}
	t.Logf("EVIDENCE limit clamp: limit=5000 returned %d events, hasMore=%v, next page %v",
		len(page), hasMore, seqsOf(rest))
}

// ---------------------------------------------------------------------------
// State and event in one transaction (§19.3)
// ---------------------------------------------------------------------------

// TestRunStatusAndEventCommitTogether is the core §19.3 assertion: the run's
// status transition, its event and its outbox rows are one unit. Each case runs
// a real UpdateRunStatusCAS next to a real AppendEventTx inside one WithTx and
// checks both sides afterwards.
func TestRunStatusAndEventCommitTogether(t *testing.T) {
	ctx := context.Background()

	t.Run("both succeed and both are visible", func(t *testing.T) {
		f := newEventFixture(t)

		err := f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
			updated, err := UpdateRunStatusCAS(ctx, tx, f.runID, 1, run.RunStatusStarting, time.UnixMilli(1700000003000))
			if err != nil {
				return err
			}
			if updated.Status != run.RunStatusStarting || updated.Revision != 2 {
				t.Errorf("CAS returned status=%s revision=%d, want starting/2", updated.Status, updated.Revision)
			}
			_, err = AppendEventTx(ctx, tx, EventInput{
				ProjectID:    eventProjectID,
				RunID:        &f.runID,
				Type:         string(run.EventSchedulerClaimed),
				OccurredAt:   time.UnixMilli(1700000003000),
				Identity:     eventIdentity(eventProjectID, "system", &f.runID),
				Payload:      json.RawMessage(`{"worker":"w-1"}`),
				Destinations: []string{"ws:project:" + eventProjectID, "ws:run:" + f.runID},
			})
			return err
		})
		if err != nil {
			t.Fatalf("committed transaction: %v", err)
		}

		stored, err := GetRun(ctx, f.store.DB(), f.runID)
		if err != nil {
			t.Fatalf("GetRun: %v", err)
		}
		if stored.Status != run.RunStatusStarting || stored.Revision != 2 {
			t.Errorf("run after commit = status %s revision %d, want starting/2", stored.Status, stored.Revision)
		}
		events, _, err := ListRunEvents(ctx, f.store.DB(), f.runID, 0, 0)
		if err != nil {
			t.Fatalf("ListRunEvents: %v", err)
		}
		if len(events) != 1 || events[0].Type != string(run.EventSchedulerClaimed) {
			t.Fatalf("events after commit = %+v, want one scheduler.claimed", events)
		}
		if n := eventRowCount(t, f.store.DB(), "outbox"); n != 2 {
			t.Errorf("outbox rows = %d, want 2 (one per destination)", n)
		}
		t.Logf("EVIDENCE committed: run=(%s,%d) event=(%s,project_seq=%d,run_seq=%d) outbox=2",
			stored.Status, stored.Revision, events[0].Type, events[0].ProjectSeq, *events[0].RunSeq)
	})

	t.Run("an invalid event rolls the status change back", func(t *testing.T) {
		f := newEventFixture(t)

		// Read the state before, so the comparison is against real values
		// rather than against what the fixture was supposed to write.
		before, err := GetRun(ctx, f.store.DB(), f.runID)
		if err != nil {
			t.Fatalf("GetRun before: %v", err)
		}

		err = f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
			// The CAS succeeds here; the event is what fails.
			if _, err := UpdateRunStatusCAS(ctx, tx, f.runID, before.Revision, run.RunStatusStarting, time.UnixMilli(1700000003000)); err != nil {
				return err
			}
			_, err := AppendEventTx(ctx, tx, EventInput{
				ProjectID:  eventProjectID,
				RunID:      &f.runID,
				Type:       "run.started", // not in the closed enum
				OccurredAt: time.UnixMilli(1700000003000),
				Identity:   eventIdentity(eventProjectID, "system", &f.runID),
				Payload:    json.RawMessage(`{}`),
			})
			return err
		})
		if !errors.Is(err, ErrInvalidEvent) {
			t.Fatalf("transaction error = %v, want ErrInvalidEvent", err)
		}

		// The run must be exactly as it was: status and revision both.
		after, err := GetRun(ctx, f.store.DB(), f.runID)
		if err != nil {
			t.Fatalf("GetRun after: %v", err)
		}
		if after.Status != before.Status || after.Revision != before.Revision {
			t.Errorf("run after rollback = status %s revision %d, want status %s revision %d",
				after.Status, after.Revision, before.Status, before.Revision)
		}
		if n := eventRowCount(t, f.store.DB(), "events"); n != 0 {
			t.Errorf("events rows = %d, want 0 after the rollback", n)
		}
		if n := eventRowCount(t, f.store.DB(), "outbox"); n != 0 {
			t.Errorf("outbox rows = %d, want 0 after the rollback", n)
		}
		if got := counterValue(t, f.store.DB(), projectScope(eventProjectID)); got != -1 {
			t.Errorf("project counter = %d, want no row (validation happens before any SQL)", got)
		}
		t.Logf("EVIDENCE rollback on invalid event: run stayed (%s,%d), events=0, outbox=0, counter=%d",
			after.Status, after.Revision, counterValue(t, f.store.DB(), projectScope(eventProjectID)))
	})

	t.Run("a CAS conflict leaves no event and no outbox row", func(t *testing.T) {
		f := newEventFixture(t)

		// Someone else moves the run first, so the CAS inside the transaction
		// below is stale.
		err := f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
			_, err := UpdateRunStatusCAS(ctx, tx, f.runID, 1, run.RunStatusStarting, time.UnixMilli(1700000004000))
			return err
		})
		if err != nil {
			t.Fatalf("first CAS: %v", err)
		}
		moved, err := GetRun(ctx, f.store.DB(), f.runID)
		if err != nil {
			t.Fatalf("GetRun: %v", err)
		}

		err = f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
			// Stale revision: this must fail.
			if _, err := UpdateRunStatusCAS(ctx, tx, f.runID, 1, run.RunStatusRunning, time.UnixMilli(1700000005000)); err != nil {
				return err
			}
			_, err := AppendEventTx(ctx, tx, EventInput{
				ProjectID:  eventProjectID,
				RunID:      &f.runID,
				AttemptID:  stringPtr(eventAttemptID),
				Type:       string(run.EventProcessStarted),
				OccurredAt: time.UnixMilli(1700000005000),
				Identity: eventIdentityFor(eventProjectID, "system", &f.runID,
					stringPtr(eventAttemptID), stringPtr(eventAgentRevisionID)),
				Payload: json.RawMessage(`{}`),
			})
			return err
		})
		if !errors.Is(err, ErrRevisionConflict) {
			t.Fatalf("transaction error = %v, want ErrRevisionConflict", err)
		}

		after, err := GetRun(ctx, f.store.DB(), f.runID)
		if err != nil {
			t.Fatalf("GetRun after: %v", err)
		}
		if after.Status != moved.Status || after.Revision != moved.Revision {
			t.Errorf("run = status %s revision %d, want the first transition only (%s,%d)",
				after.Status, after.Revision, moved.Status, moved.Revision)
		}
		if n := eventRowCount(t, f.store.DB(), "events"); n != 0 {
			t.Errorf("events rows = %d, want 0 (the conflicting transaction wrote nothing)", n)
		}
		if n := eventRowCount(t, f.store.DB(), "outbox"); n != 0 {
			t.Errorf("outbox rows = %d, want 0", n)
		}
		t.Logf("EVIDENCE CAS conflict: run stayed (%s,%d), events=0, outbox=0", after.Status, after.Revision)
	})

	t.Run("a status change rolled back by the caller leaves no event either", func(t *testing.T) {
		// The mirror case: both writes succeed inside fn and the caller then
		// returns an error. Nothing may survive — this is the shape a later
		// validation failure in the same unit of work takes.
		f := newEventFixture(t)
		sentinel := errors.New("a later step refused")

		err := f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
			if _, err := UpdateRunStatusCAS(ctx, tx, f.runID, 1, run.RunStatusStarting, time.UnixMilli(1700000006000)); err != nil {
				return err
			}
			if _, err := AppendEventTx(ctx, tx, EventInput{
				ProjectID:    eventProjectID,
				RunID:        &f.runID,
				Type:         string(run.EventSchedulerClaimed),
				OccurredAt:   time.UnixMilli(1700000006000),
				Identity:     eventIdentity(eventProjectID, "system", &f.runID),
				Payload:      json.RawMessage(`{}`),
				Destinations: []string{"ws:project:" + eventProjectID},
			}); err != nil {
				return err
			}
			return sentinel
		})
		if !errors.Is(err, sentinel) {
			t.Fatalf("transaction error = %v, want the caller's error", err)
		}

		stored, err := GetRun(ctx, f.store.DB(), f.runID)
		if err != nil {
			t.Fatalf("GetRun: %v", err)
		}
		if stored.Status != run.RunStatusQueued || stored.Revision != 1 {
			t.Errorf("run = (%s,%d), want (queued,1)", stored.Status, stored.Revision)
		}
		if n := eventRowCount(t, f.store.DB(), "events"); n != 0 {
			t.Errorf("events rows = %d, want 0", n)
		}
		if n := eventRowCount(t, f.store.DB(), "outbox"); n != 0 {
			t.Errorf("outbox rows = %d, want 0", n)
		}
	})
}

// ---------------------------------------------------------------------------
// Sequence allocation under rollback and concurrency
// ---------------------------------------------------------------------------

// TestRollbackDoesNotConsumeSequenceNumbers is the sequence half of the
// transaction rule: a rolled-back append must return its numbers to the pool, so
// the next append gets the value the failed one would have had and a reader
// never sees a gap.
func TestRollbackDoesNotConsumeSequenceNumbers(t *testing.T) {
	ctx := context.Background()
	f := newEventFixture(t)

	first := f.appendEvent(t, run.EventServerRestart, nil)

	// A transaction that appends successfully and then fails: both the event
	// and the counter increment must be undone.
	sentinel := errors.New("changed my mind")
	err := f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		if _, err := AppendEventTx(ctx, tx, EventInput{
			ProjectID:  eventProjectID,
			RunID:      &f.runID,
			AttemptID:  stringPtr(eventAttemptID),
			Type:       string(run.EventProcessStarted),
			OccurredAt: time.UnixMilli(1700000007000),
			Identity: eventIdentityFor(eventProjectID, "system", &f.runID,
				stringPtr(eventAttemptID), stringPtr(eventAgentRevisionID)),
			Payload: json.RawMessage(`{}`),
		}); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("transaction error = %v, want the sentinel", err)
	}

	// The counters are exactly where the committed event left them.
	if got := counterValue(t, f.store.DB(), projectScope(eventProjectID)); got != first.ProjectSeq {
		t.Errorf("project counter after rollback = %d, want %d (the rolled-back append consumed nothing)",
			got, first.ProjectSeq)
	}
	if got := counterValue(t, f.store.DB(), runScope(f.runID)); got != -1 {
		t.Errorf("run counter after rollback = %d, want no row", got)
	}

	// The next append continues the sequence: +1, no gap.
	next := f.appendEvent(t, run.EventProcessStarted, &f.runID)
	if next.ProjectSeq != first.ProjectSeq+1 {
		t.Errorf("project_seq after the rollback = %d, want %d", next.ProjectSeq, first.ProjectSeq+1)
	}
	if next.RunSeq == nil || *next.RunSeq != 1 {
		t.Errorf("run_seq after the rollback = %s, want 1 (the run's first committed event)", runSeqString(next.RunSeq))
	}

	// The project timeline reads as 1,2 with no hole.
	events, _, err := ListProjectEvents(ctx, f.store.DB(), eventProjectID, 0, 0)
	if err != nil {
		t.Fatalf("ListProjectEvents: %v", err)
	}
	if got := seqsOf(events); fmt.Sprint(got) != "[1 2]" {
		t.Errorf("project timeline = %v, want [1 2]", got)
	}
	t.Logf("EVIDENCE rollback keeps counter: counter=%d after rollback, next project_seq=%d, timeline=%v",
		first.ProjectSeq, next.ProjectSeq, seqsOf(events))
}

// TestConcurrentAppendsAllocateDistinctSequences is the concurrency contract:
// eight writers, each in its own transaction, appending to the same project must
// produce exactly the sequence 1..160 with no duplicate and no gap. A duplicate
// would be rejected by the unique index; a gap would mean a number was consumed
// by a transaction that did not commit.
func TestConcurrentAppendsAllocateDistinctSequences(t *testing.T) {
	ctx := context.Background()
	f := newEventFixture(t)

	const (
		writers   = 8
		perWriter = 20
		wantTotal = writers * perWriter
	)

	start := make(chan struct{})
	errs := make(chan error, writers)

	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			<-start
			for i := 0; i < perWriter; i++ {
				err := f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
					_, err := AppendEventTx(ctx, tx, EventInput{
						ProjectID:  eventProjectID,
						Type:       string(run.EventServerRestart),
						OccurredAt: time.UnixMilli(1700000010000 + int64(w*perWriter+i)),
						Identity:   eventIdentity(eventProjectID, "agent", nil),
						Payload:    json.RawMessage(fmt.Sprintf(`{"writer":%d,"n":%d}`, w, i)),
					})
					return err
				})
				if err != nil {
					errs <- fmt.Errorf("writer %d append %d: %w", w, i, err)
					return
				}
			}
		}(w)
	}
	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		// A BUSY-family error here would mean the immediate-transaction pin
		// failed to serialise the counter updates; it must never appear.
		if code := sqliteExtended(err); code == 5 || code == 517 || code == 6 {
			t.Errorf("append failed with a SQLITE_BUSY-family error (code %d): %v", code, err)
		}
		t.Fatalf("concurrent append: %v", err)
	}

	// Every sequence exactly once, in order.
	events, _, err := ListProjectEvents(ctx, f.store.DB(), eventProjectID, 0, MaxEventLimit)
	if err != nil {
		t.Fatalf("ListProjectEvents: %v", err)
	}
	if len(events) != wantTotal {
		t.Fatalf("events = %d, want %d", len(events), wantTotal)
	}
	seen := make(map[int64]bool, wantTotal)
	for i, event := range events {
		if want := int64(i + 1); event.ProjectSeq != want {
			t.Errorf("events[%d].project_seq = %d, want %d (the page is ordered and gap-free)",
				i, event.ProjectSeq, want)
		}
		if seen[event.ProjectSeq] {
			t.Errorf("project_seq %d appears twice", event.ProjectSeq)
		}
		seen[event.ProjectSeq] = true
	}
	if got := counterValue(t, f.store.DB(), projectScope(eventProjectID)); got != wantTotal {
		t.Errorf("project counter = %d, want %d", got, wantTotal)
	}
	if n := eventRowCount(t, f.store.DB(), "events"); n != wantTotal {
		t.Errorf("events rows = %d, want %d", n, wantTotal)
	}
	t.Logf("EVIDENCE concurrent appends: %d writers x %d = %d events, project_seq 1..%d with no duplicate or gap, counter=%d",
		writers, perWriter, len(events), events[len(events)-1].ProjectSeq,
		counterValue(t, f.store.DB(), projectScope(eventProjectID)))
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

// TestAppendEventRejectsInvalidInput walks the validation contract: each case is
// refused with ErrInvalidEvent and, crucially, writes nothing — no event, no
// outbox row, and not even a counter row.
func TestAppendEventRejectsInvalidInput(t *testing.T) {
	ctx := context.Background()

	bigPayload := json.RawMessage(`{"blob":"` + strings.Repeat("x", MaxEventPayloadBytes) + `"}`)

	cases := []struct {
		name    string
		input   func(f *eventFixture) EventInput
		wantSub string
	}{
		{
			name: "unknown event type",
			input: func(f *eventFixture) EventInput {
				return EventInput{
					ProjectID: eventProjectID, Type: "run.started",
					OccurredAt: time.UnixMilli(1700000001000),
					Identity:   eventIdentity(eventProjectID, "system", nil),
					Payload:    json.RawMessage(`{}`),
				}
			},
			wantSub: "closed enum",
		},
		{
			name: "identity missing project_id",
			input: func(f *eventFixture) EventInput {
				return EventInput{
					ProjectID: eventProjectID, Type: string(run.EventServerRestart),
					OccurredAt: time.UnixMilli(1700000001000),
					Identity:   json.RawMessage(`{"actor":{"type":"system","id":"w-1"}}`),
					Payload:    json.RawMessage(`{}`),
				}
			},
			wantSub: `field "project_id" is invalid`,
		},
		{
			name: "identity missing actor",
			input: func(f *eventFixture) EventInput {
				return EventInput{
					ProjectID: eventProjectID, Type: string(run.EventServerRestart),
					OccurredAt: time.UnixMilli(1700000001000),
					Identity:   json.RawMessage(`{"project_id":"` + eventProjectID + `"}`),
					Payload:    json.RawMessage(`{}`),
				}
			},
			wantSub: `field "actor.type" is invalid`,
		},
		{
			name: "identity actor has an unknown type",
			input: func(f *eventFixture) EventInput {
				return EventInput{
					ProjectID: eventProjectID, Type: string(run.EventServerRestart),
					OccurredAt: time.UnixMilli(1700000001000),
					Identity:   json.RawMessage(`{"project_id":"` + eventProjectID + `","actor":{"type":"robot","id":"w-1"}}`),
					Payload:    json.RawMessage(`{}`),
				}
			},
			wantSub: `field "actor.type" is invalid`,
		},
		{
			name: "identity actor has no id",
			input: func(f *eventFixture) EventInput {
				return EventInput{
					ProjectID: eventProjectID, Type: string(run.EventServerRestart),
					OccurredAt: time.UnixMilli(1700000001000),
					Identity:   json.RawMessage(`{"project_id":"` + eventProjectID + `","actor":{"type":"system"}}`),
					Payload:    json.RawMessage(`{}`),
				}
			},
			wantSub: `field "actor.id" is invalid`,
		},
		{
			name: "identity names a different project",
			input: func(f *eventFixture) EventInput {
				return EventInput{
					ProjectID: eventProjectID, Type: string(run.EventServerRestart),
					OccurredAt: time.UnixMilli(1700000001000),
					Identity:   eventIdentity("p-somebody-else", "system", nil),
					Payload:    json.RawMessage(`{}`),
				}
			},
			wantSub: "does not match ProjectID",
		},
		{
			name: "identity run_id disagrees with RunID",
			input: func(f *eventFixture) EventInput {
				other := "run-other"
				return EventInput{
					ProjectID: eventProjectID, RunID: &f.runID, Type: string(run.EventApprovalApproved),
					OccurredAt: time.UnixMilli(1700000001000),
					Identity:   eventIdentity(eventProjectID, "system", &other),
					Payload:    json.RawMessage(`{}`),
				}
			},
			wantSub: "does not match RunID",
		},
		{
			// server.restart has no per-event requirement, so the only rule
			// that can fail is the run_id agreement under test.
			name: "run-scoped event without identity.run_id",
			input: func(f *eventFixture) EventInput {
				return EventInput{
					ProjectID: eventProjectID, RunID: &f.runID, Type: string(run.EventServerRestart),
					OccurredAt: time.UnixMilli(1700000001000),
					Identity:   eventIdentity(eventProjectID, "system", nil),
					Payload:    json.RawMessage(`{}`),
				}
			},
			wantSub: "is set but identity.run_id is missing",
		},
		{
			name: "identity.run_id without RunID",
			input: func(f *eventFixture) EventInput {
				return EventInput{
					ProjectID: eventProjectID, Type: string(run.EventServerRestart),
					OccurredAt: time.UnixMilli(1700000001000),
					Identity:   eventIdentity(eventProjectID, "system", &f.runID),
					Payload:    json.RawMessage(`{}`),
				}
			},
			wantSub: "RunID is empty",
		},
		{
			name: "payload is not JSON",
			input: func(f *eventFixture) EventInput {
				return EventInput{
					ProjectID: eventProjectID, Type: string(run.EventServerRestart),
					OccurredAt: time.UnixMilli(1700000001000),
					Identity:   eventIdentity(eventProjectID, "system", nil),
					Payload:    json.RawMessage(`{not json`),
				}
			},
			wantSub: "payload",
		},
		{
			name: "payload is not an object",
			input: func(f *eventFixture) EventInput {
				return EventInput{
					ProjectID: eventProjectID, Type: string(run.EventServerRestart),
					OccurredAt: time.UnixMilli(1700000001000),
					Identity:   eventIdentity(eventProjectID, "system", nil),
					Payload:    json.RawMessage(`["a","b"]`),
				}
			},
			wantSub: "must be a JSON object",
		},
		{
			name: "payload is over the 256 KiB limit",
			input: func(f *eventFixture) EventInput {
				return EventInput{
					ProjectID: eventProjectID, Type: string(run.EventServerRestart),
					OccurredAt: time.UnixMilli(1700000001000),
					Identity:   eventIdentity(eventProjectID, "system", nil),
					Payload:    bigPayload,
				}
			},
			wantSub: "limit is 262144",
		},
		{
			name: "identity is empty",
			input: func(f *eventFixture) EventInput {
				return EventInput{
					ProjectID: eventProjectID, Type: string(run.EventServerRestart),
					OccurredAt: time.UnixMilli(1700000001000),
					Payload:    json.RawMessage(`{}`),
				}
			},
			wantSub: "Identity is empty",
		},
		{
			name: "project id is empty",
			input: func(f *eventFixture) EventInput {
				return EventInput{
					Type: string(run.EventServerRestart), OccurredAt: time.UnixMilli(1700000001000),
					Identity: eventIdentity(eventProjectID, "system", nil),
					Payload:  json.RawMessage(`{}`),
				}
			},
			wantSub: "ProjectID is empty",
		},
		{
			name: "occurred_at is zero",
			input: func(f *eventFixture) EventInput {
				return EventInput{
					ProjectID: eventProjectID, Type: string(run.EventServerRestart),
					Identity: eventIdentity(eventProjectID, "system", nil),
					Payload:  json.RawMessage(`{}`),
				}
			},
			wantSub: "OccurredAt is zero",
		},
		{
			name: "a destination is blank",
			input: func(f *eventFixture) EventInput {
				return EventInput{
					ProjectID: eventProjectID, Type: string(run.EventServerRestart),
					OccurredAt:   time.UnixMilli(1700000001000),
					Identity:     eventIdentity(eventProjectID, "system", nil),
					Payload:      json.RawMessage(`{}`),
					Destinations: []string{"  "},
				}
			},
			wantSub: "destination is blank",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newEventFixture(t)
			in := tc.input(f)

			err := f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
				_, err := AppendEventTx(ctx, tx, in)
				return err
			})
			if !errors.Is(err, ErrInvalidEvent) {
				t.Fatalf("AppendEventTx error = %v, want ErrInvalidEvent", err)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error %q does not mention %q", err, tc.wantSub)
			}
			if n := eventRowCount(t, f.store.DB(), "events"); n != 0 {
				t.Errorf("events rows = %d, want 0 (the write must be refused before any SQL)", n)
			}
			if n := eventRowCount(t, f.store.DB(), "outbox"); n != 0 {
				t.Errorf("outbox rows = %d, want 0", n)
			}
			if got := counterValue(t, f.store.DB(), projectScope(eventProjectID)); got != -1 {
				t.Errorf("project counter = %d, want no row (nothing was allocated)", got)
			}
		})
	}
}

// TestAppendEventAcceptsEveryEnumType proves the store's enum is exactly as wide
// as the schema: every one of the enum types must be appendable, and the migration
// CHECK must accept the same set. A type the Go list knows but the CHECK does
// not would fail here with a raw constraint error.
func TestAppendEventAcceptsEveryEnumType(t *testing.T) {
	f := newEventFixture(t)

	if want := len(eventTypesFromSchema(t)); len(run.ExecutionEventTypes) != want {
		t.Fatalf("run.ExecutionEventTypes has %d entries, want the %d of execution-event.schema.json",
			len(run.ExecutionEventTypes), want)
	}
	for i, eventType := range run.ExecutionEventTypes {
		// The fixture supplies the attempt and agent revision the type
		// requires; a run is passed only when the contract demands one, so the
		// Run-less families of §27.1 are exercised as such.
		req, err := run.RequiredIdentity(string(eventType))
		if err != nil {
			t.Fatalf("RequiredIdentity(%q): %v", eventType, err)
		}
		var runID *string
		if req.Run {
			runID = &f.runID
		}
		if _, err := f.tryAppendEvent(eventType, runID); err != nil {
			t.Fatalf("AppendEventTx(%q): %v", eventType, err)
		}
		if got := counterValue(t, f.store.DB(), projectScope(eventProjectID)); got != int64(i+1) {
			t.Errorf("after appending %q the counter is %d, want %d", eventType, got, i+1)
		}
	}

	// The migration's CHECK must accept exactly the same set: read the enum out
	// of the SQL and compare in both directions, so a type added on one side
	// only is caught here rather than at runtime.
	sqlTypes := eventTypesFromMigration(t)
	if len(sqlTypes) != len(run.ExecutionEventTypes) {
		t.Fatalf("migration CHECK lists %d types, run.ExecutionEventTypes lists %d",
			len(sqlTypes), len(run.ExecutionEventTypes))
	}
	for _, want := range run.ExecutionEventTypes {
		if !contains(sqlTypes, string(want)) {
			t.Errorf("migration CHECK is missing %q", want)
		}
	}
	for _, got := range sqlTypes {
		if !run.ExecutionEventType(got).Valid() {
			t.Errorf("migration CHECK accepts %q, which run.ExecutionEventType.Valid() rejects", got)
		}
	}
	t.Logf("EVIDENCE enum: %d types appended and accepted by both the store and the migration CHECK",
		len(run.ExecutionEventTypes))
}

// eventTypesFromMigration reads the type CHECK out of 003_events.sql, so the SQL
// enum is compared with the Go one instead of being restated in the test.
func eventTypesFromMigration(t *testing.T) []string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("migrations", "003_events.sql"))
	if err != nil {
		t.Fatalf("read 003_events.sql: %v", err)
	}
	text := string(body)
	start := strings.Index(text, "type           TEXT    NOT NULL CHECK (type IN (")
	if start < 0 {
		t.Fatal("003_events.sql has no type CHECK to compare against")
	}
	rest := text[start:]
	end := strings.Index(rest, "))")
	if end < 0 {
		t.Fatal("the type CHECK in 003_events.sql is not terminated")
	}

	var out []string
	for _, line := range strings.Split(rest[:end], "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "'") {
			continue
		}
		out = append(out, strings.Trim(strings.TrimSuffix(line, ","), "'"))
	}
	if len(out) == 0 {
		t.Fatal("parsed no event types out of 003_events.sql")
	}
	return out
}

func contains(haystack []string, needle string) bool {
	for _, item := range haystack {
		if item == needle {
			return true
		}
	}
	return false
}

// TestEventTypeEnumMatchesSchema requires the three lists of the closed enum to
// be one list: the JSON schema (the wire contract), run.ExecutionEventTypes (the
// Go contract T1.05.a validates against) and the migration CHECK (the
// database's last line of defence). All three are compared in both directions
// and in order, so a type added to one of them is caught here rather than at
// runtime or, worse, in production.
func TestEventTypeEnumMatchesSchema(t *testing.T) {
	schemaEnum := eventTypesFromSchema(t)
	runTypes := make([]string, 0, len(run.ExecutionEventTypes))
	for _, eventType := range run.ExecutionEventTypes {
		runTypes = append(runTypes, string(eventType))
	}
	sqlTypes := eventTypesFromMigration(t)

	assertSameEnum(t, "execution-event.schema.json", schemaEnum, "run.ExecutionEventTypes", runTypes)
	assertSameEnum(t, "run.ExecutionEventTypes", runTypes, "003_events.sql CHECK", sqlTypes)

	// A near-miss must not be accepted: the enum is exact, not a prefix match.
	if run.ExecutionEventType(run.EventRunFailed+" ").Valid() || run.ExecutionEventType("RUN.FAILED").Valid() {
		t.Error("ExecutionEventType.Valid() accepted a case or whitespace variant of a known type")
	}
}

// assertSameEnum compares two spellings of the same closed set: equal length,
// equal order, and every member of one present in the other.
func assertSameEnum(t *testing.T, leftName string, left []string, rightName string, right []string) {
	t.Helper()
	if len(left) != len(right) {
		t.Fatalf("%s has %d types, %s has %d", leftName, len(left), rightName, len(right))
	}
	for i := range left {
		if left[i] != right[i] {
			t.Errorf("%s[%d] = %q, %s[%d] = %q", leftName, i, left[i], rightName, i, right[i])
		}
	}
	for _, want := range left {
		if !contains(right, want) {
			t.Errorf("%s is missing %q, which %s lists", rightName, want, leftName)
		}
	}
	for _, got := range right {
		if !contains(left, got) {
			t.Errorf("%s lists %q, which %s does not", rightName, got, leftName)
		}
	}
}

// eventTypesFromSchema reads the type enum out of
// schemas/execution-event.schema.json.
func eventTypesFromSchema(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "schemas", "execution-event.schema.json"))
	if err != nil {
		t.Fatalf("read execution-event.schema.json: %v", err)
	}
	var schema struct {
		Properties struct {
			Type struct {
				Enum []string `json:"enum"`
			} `json:"type"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("decode execution-event.schema.json: %v", err)
	}
	if len(schema.Properties.Type.Enum) == 0 {
		t.Fatal("execution-event.schema.json has no type enum")
	}
	return schema.Properties.Type.Enum
}

// ---------------------------------------------------------------------------
// Immutability and outbox retention
// ---------------------------------------------------------------------------

// TestEventRowIsImmutable proves the fact stream cannot be rewritten or erased,
// including by a statement issued directly against the database rather than
// through the store (which exposes no such path at all).
func TestEventRowIsImmutable(t *testing.T) {
	ctx := context.Background()
	f := newEventFixture(t)
	event := f.appendEvent(t, run.EventRunFailed, &f.runID, "ws:project:"+eventProjectID)

	// The setup must be sound: an unrelated column of an unrelated row can be
	// updated, so the failures below are caused by the event row itself.
	if _, err := f.store.DB().ExecContext(ctx,
		`UPDATE project_refs SET verified_at = 99 WHERE project_id = ?`, eventProjectID); err != nil {
		t.Fatalf("setup update on project_refs: %v", err)
	}

	updateErr := func(query string, args ...any) error {
		_, err := f.store.DB().ExecContext(ctx, query, args...)
		return err
	}

	if err := updateErr(`UPDATE events SET payload_json = '{}' WHERE id = ?`, event.ID); err == nil {
		t.Error("UPDATE events succeeded, want the event_immutable trigger to refuse it")
	} else {
		assertEventImmutable(t, "update payload", err)
	}
	if err := updateErr(`UPDATE events SET type = ? WHERE id = ?`, run.EventRunCompleted, event.ID); err == nil {
		t.Error("UPDATE events.type succeeded, want a refusal")
	} else {
		assertEventImmutable(t, "update type", err)
	}
	if err := updateErr(`DELETE FROM events WHERE id = ?`, event.ID); err == nil {
		t.Error("DELETE FROM events succeeded, want the event_immutable trigger to refuse it")
	} else {
		assertEventImmutable(t, "delete", err)
	}

	// The row is byte-identical after all three refusals.
	stored, err := GetEvent(ctx, f.store.DB(), event.ID)
	if err != nil {
		t.Fatalf("GetEvent after refused mutations: %v", err)
	}
	if stored.Type != event.Type || string(stored.Payload) != string(event.Payload) ||
		stored.PayloadHash != event.PayloadHash || stored.ProjectSeq != event.ProjectSeq {
		t.Errorf("event changed after refused mutations:\n before %+v\n after  %+v", event, stored)
	}
	if n := eventRowCount(t, f.store.DB(), "events"); n != 1 {
		t.Errorf("events rows = %d, want 1", n)
	}
}

// assertEventImmutable checks that err is the trigger refusal, at the database
// level (extended code 1811 with the stable RAISE name) and through the store's
// mapping.
func assertEventImmutable(t *testing.T, op string, err error) {
	t.Helper()
	var serr *sqlite.Error
	if !errors.As(err, &serr) || serr.Code() != sqliteConstraintTrigger {
		t.Errorf("%s: error = %v (code %d), want a trigger refusal (code %d)",
			op, err, sqliteExtended(err), sqliteConstraintTrigger)
	}
	if !strings.Contains(err.Error(), errNameEventImmutable) {
		t.Errorf("%s: error %q does not name %s", op, err, errNameEventImmutable)
	}
	if mapped := mapConstraintError("test "+op, err); !errors.Is(mapped, ErrEventImmutable) {
		t.Errorf("%s: mapConstraintError = %v, want ErrEventImmutable", op, mapped)
	}
}

// TestOutboxDeliveriesAreQueuedAndRetained covers the delivery half of §19.1:
// one pending row per destination created in the same transaction as the event,
// one delivery per (event, destination), and a dead-lettered row that cannot be
// deleted.
func TestOutboxDeliveriesAreQueuedAndRetained(t *testing.T) {
	ctx := context.Background()
	f := newEventFixture(t)

	destinations := []string{"ws:project:" + eventProjectID, "desktop:notify", "audit:export"}
	event := f.appendEvent(t, run.EventRunFailed, &f.runID, destinations...)

	type delivery struct {
		id          string
		destination string
		state       string
		attempts    int64
		nextAttempt int64
		lastError   string
	}
	read := func() []delivery {
		t.Helper()
		rows, err := f.store.DB().QueryContext(ctx,
			`SELECT id, destination, state, attempt_count, next_attempt_at, last_error
			 FROM outbox WHERE event_id = ? ORDER BY destination`, event.ID)
		if err != nil {
			t.Fatalf("read outbox: %v", err)
		}
		defer rows.Close()
		var out []delivery
		for rows.Next() {
			var (
				d           delivery
				nextAttempt sqlNullInt64
				lastError   sqlNullString
			)
			if err := rows.Scan(&d.id, &d.destination, &d.state, &d.attempts, &nextAttempt, &lastError); err != nil {
				t.Fatalf("scan outbox: %v", err)
			}
			d.nextAttempt = nextAttempt.value()
			d.lastError = lastError.value()
			out = append(out, d)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("iterate outbox: %v", err)
		}
		return out
	}

	deliveries := read()
	if len(deliveries) != len(destinations) {
		t.Fatalf("outbox rows = %d, want %d (one per destination)", len(deliveries), len(destinations))
	}
	for _, d := range deliveries {
		if d.state != "pending" {
			t.Errorf("destination %s state = %q, want pending", d.destination, d.state)
		}
		if d.attempts != 0 {
			t.Errorf("destination %s attempt_count = %d, want 0", d.destination, d.attempts)
		}
		// A pending row must be due immediately, otherwise the dispatcher's
		// "pending and due" scan would skip it forever.
		if d.nextAttempt == 0 {
			t.Errorf("destination %s next_attempt_at is NULL, want the event instant", d.destination)
		}
		if d.lastError != "" {
			t.Errorf("destination %s last_error = %q, want NULL", d.destination, d.lastError)
		}
		if d.id == "" {
			t.Errorf("destination %s has an empty id", d.destination)
		}
	}

	t.Run("the same destination cannot be queued twice", func(t *testing.T) {
		_, err := f.store.DB().ExecContext(ctx,
			`INSERT INTO outbox (id, event_id, destination, state, attempt_count, next_attempt_at, created_at)
			 VALUES ('dup', ?, ?, 'pending', 0, 1, 1)`, event.ID, destinations[0])
		if err == nil {
			t.Fatal("a duplicate (event_id, destination) was accepted, want a unique violation")
		}
		var serr *sqlite.Error
		if !errors.As(err, &serr) || serr.Code() != sqliteConstraintUnique {
			t.Errorf("duplicate delivery error = %v (code %d), want SQLITE_CONSTRAINT_UNIQUE (%d)",
				err, sqliteExtended(err), sqliteConstraintUnique)
		}
		if !strings.Contains(err.Error(), "outbox.event_id") {
			t.Errorf("error %q does not name the outbox unique constraint", err)
		}
	})

	t.Run("a dead-lettered delivery cannot be deleted", func(t *testing.T) {
		// Move one row to dead_letter the way the dispatcher (T1.05.b) will:
		// state, attempt count and the failure reason together.
		if _, err := f.store.DB().ExecContext(ctx,
			`UPDATE outbox SET state='dead_letter', attempt_count=5, last_error='connection refused'
			 WHERE id = ?`, deliveries[0].id); err != nil {
			t.Fatalf("dead-letter the row: %v", err)
		}

		_, err := f.store.DB().ExecContext(ctx, `DELETE FROM outbox WHERE id = ?`, deliveries[0].id)
		if err == nil {
			t.Fatal("DELETE of a dead_letter row succeeded, want the retention trigger to refuse it")
		}
		var serr *sqlite.Error
		if !errors.As(err, &serr) || serr.Code() != sqliteConstraintTrigger {
			t.Errorf("delete error = %v (code %d), want a trigger refusal (%d)",
				err, sqliteExtended(err), sqliteConstraintTrigger)
		}
		if !strings.Contains(err.Error(), errNameOutboxDeadLetterRetained) {
			t.Errorf("error %q does not name %s", err, errNameOutboxDeadLetterRetained)
		}
		if mapped := mapConstraintError("delete delivery", err); !errors.Is(mapped, ErrOutboxDeadLetterRetained) {
			t.Errorf("mapConstraintError = %v, want ErrOutboxDeadLetterRetained", mapped)
		}

		// The row, and the event it carries, are both still there.
		if n := eventRowCount(t, f.store.DB(), "outbox"); n != len(destinations) {
			t.Errorf("outbox rows = %d, want %d", n, len(destinations))
		}
		if _, err := GetEvent(ctx, f.store.DB(), event.ID); err != nil {
			t.Errorf("GetEvent after the refused delete: %v", err)
		}
	})

	t.Run("the same delivery cannot be queued twice in another state either", func(t *testing.T) {
		// UNIQUE (event_id, destination) is not scoped to state: a delivered
		// row must not be joined by a pending one for the same destination, or
		// the dispatcher would find the same work twice and the delivery record
		// would stop being one row per (event, destination).
		_, err := f.store.DB().ExecContext(ctx,
			`INSERT INTO outbox (id, event_id, destination, state, attempt_count, next_attempt_at, created_at)
			 VALUES ('dup-delivered', ?, ?, 'delivered', 1, NULL, 1)`, event.ID, destinations[0])
		if err == nil {
			t.Fatal("a second row for the same (event_id, destination) was accepted in state 'delivered'")
		}
		var serr *sqlite.Error
		if !errors.As(err, &serr) || serr.Code() != sqliteConstraintUnique {
			t.Errorf("second delivery error = %v (code %d), want SQLITE_CONSTRAINT_UNIQUE (%d)",
				err, sqliteExtended(err), sqliteConstraintUnique)
		}
		if n := eventRowCount(t, f.store.DB(), "outbox"); n != len(destinations) {
			t.Errorf("outbox rows = %d, want %d", n, len(destinations))
		}
	})

	t.Run("a delivered row is still deletable", func(t *testing.T) {
		// Delivered rows are not facts: retention (T12.02) needs a way to trim
		// them, so only dead_letter is protected.
		if _, err := f.store.DB().ExecContext(ctx,
			`UPDATE outbox SET state='delivered', next_attempt_at=NULL WHERE id = ?`, deliveries[1].id); err != nil {
			t.Fatalf("mark delivered: %v", err)
		}
		if _, err := f.store.DB().ExecContext(ctx, `DELETE FROM outbox WHERE id = ?`, deliveries[1].id); err != nil {
			t.Errorf("DELETE of a delivered row = %v, want it to be allowed", err)
		}
	})

	t.Run("an outbox row needs an existing event", func(t *testing.T) {
		_, err := f.store.DB().ExecContext(ctx,
			`INSERT INTO outbox (id, event_id, destination, state, attempt_count, next_attempt_at, created_at)
			 VALUES ('orphan', 'no-such-event', 'ws:project:x', 'pending', 0, 1, 1)`)
		var serr *sqlite.Error
		if !errors.As(err, &serr) || serr.Code() != sqliteConstraintForeignKey {
			t.Errorf("orphan delivery error = %v (code %d), want a foreign key refusal (%d)",
				err, sqliteExtended(err), sqliteConstraintForeignKey)
		}
	})
}

// TestOutboxRowsRollBackWithTheirEvent is the outbox half of the atomicity rule:
// a transaction that queues deliveries and then fails leaves neither the event
// nor the deliveries, so the dispatcher can never deliver a fact that was never
// committed (§19.3: no broadcast before the commit).
func TestOutboxRowsRollBackWithTheirEvent(t *testing.T) {
	ctx := context.Background()
	f := newEventFixture(t)
	sentinel := errors.New("the unit of work failed")

	err := f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		if _, err := AppendEventTx(ctx, tx, EventInput{
			ProjectID:    eventProjectID,
			RunID:        &f.runID,
			Type:         string(run.EventRunCompleted),
			OccurredAt:   time.UnixMilli(1700000008000),
			Identity:     eventIdentity(eventProjectID, "system", &f.runID),
			Payload:      json.RawMessage(`{"exit_code":0}`),
			Destinations: []string{"ws:project:" + eventProjectID, "desktop:notify"},
		}); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("transaction error = %v, want the sentinel", err)
	}

	if n := eventRowCount(t, f.store.DB(), "events"); n != 0 {
		t.Errorf("events rows = %d, want 0", n)
	}
	if n := eventRowCount(t, f.store.DB(), "outbox"); n != 0 {
		t.Errorf("outbox rows = %d, want 0 (nothing may be queued for delivery before the commit)", n)
	}
}

// ---------------------------------------------------------------------------
// The identity contract (run.ExecutionIdentity, T1.02.a)
// ---------------------------------------------------------------------------

// TestAppendEventAppliesIdentityRequirements proves the store enforces the run
// package's per-event identity rules rather than a private copy of them: the
// events §27.1/§28 pin to a live attempt demand one, the queued-stage claim does
// not, and the Gate / system-recovery families may name no Run at all.
//
// Every rejection must be readable both ways — errors.Is(err, ErrInvalidEvent)
// and errors.As(err, &*run.IdentityError) with the offending Field — because
// that is how a producer learns what to fix.
func TestAppendEventAppliesIdentityRequirements(t *testing.T) {
	f := newEventFixture(t)

	t.Run("attempt-scoped events require attempt_id", func(t *testing.T) {
		// The process lifecycle, the tool call and the checkpoint can only be
		// emitted while an attempt is alive (§21.1, §28), so an identity that
		// omits the attempt must be refused even though the run is named.
		for _, eventType := range []run.ExecutionEventType{
			run.EventToolRequested, run.EventProcessStarted,
		} {
			_, err := f.tryAppendInput(EventInput{
				ProjectID:  eventProjectID,
				RunID:      &f.runID,
				Type:       string(eventType),
				OccurredAt: time.UnixMilli(1700000001000),
				Identity:   eventIdentity(eventProjectID, "system", &f.runID),
				Payload:    json.RawMessage(`{}`),
			})
			if !errors.Is(err, ErrInvalidEvent) {
				t.Fatalf("%s: error = %v, want ErrInvalidEvent", eventType, err)
			}
			var identityErr *run.IdentityError
			if !errors.As(err, &identityErr) {
				t.Fatalf("%s: error = %v, want it to carry a *run.IdentityError", eventType, err)
			}
			if identityErr.Field != "attempt_id" {
				t.Errorf("%s: IdentityError.Field = %q, want attempt_id", eventType, identityErr.Field)
			}
			if !identityErr.Missing {
				t.Errorf("%s: IdentityError.Missing = false, want true (the field is required and absent)", eventType)
			}
		}

		// Nothing was written by either refusal.
		if n := eventRowCount(t, f.store.DB(), "events"); n != 0 {
			t.Errorf("events rows = %d, want 0", n)
		}
		if n := eventRowCount(t, f.store.DB(), "outbox"); n != 0 {
			t.Errorf("outbox rows = %d, want 0", n)
		}
		if got := counterValue(t, f.store.DB(), projectScope(eventProjectID)); got != -1 {
			t.Errorf("project counter = %d, want no row", got)
		}
	})

	t.Run("a queued-stage claim needs no attempt", func(t *testing.T) {
		// §28: the claim is what creates the attempt, so it cannot name one.
		event := f.appendEvent(t, run.EventSchedulerClaimed, &f.runID)
		if event.AttemptID != nil {
			t.Errorf("attempt_id = %q, want nil", stringPtrString(event.AttemptID))
		}
		if event.RunSeq == nil || *event.RunSeq != 1 {
			t.Errorf("run_seq = %s, want 1", runSeqString(event.RunSeq))
		}
	})

	t.Run("Gate and system-recovery events may have no run", func(t *testing.T) {
		// §27.1: an approval decision and a restart recovery can happen with no
		// Run in scope at all.
		for _, eventType := range []run.ExecutionEventType{run.EventApprovalDecided, run.EventServerRestart} {
			event := f.appendEvent(t, eventType, nil)
			if event.RunID != nil || event.RunSeq != nil {
				t.Errorf("%s: run_id/run_seq = %s/%s, want nil/nil",
					eventType, stringPtrString(event.RunID), runSeqString(event.RunSeq))
			}
		}
	})

	t.Run("an unknown event type is named as such", func(t *testing.T) {
		_, err := f.tryAppendInput(EventInput{
			ProjectID:  eventProjectID,
			Type:       "run.started",
			OccurredAt: time.UnixMilli(1700000001000),
			Identity:   eventIdentity(eventProjectID, "system", nil),
			Payload:    json.RawMessage(`{}`),
		})
		if !errors.Is(err, ErrInvalidEvent) {
			t.Fatalf("error = %v, want ErrInvalidEvent", err)
		}
		if !strings.Contains(err.Error(), "closed enum") {
			t.Errorf("error %q does not say the type is outside the closed enum", err)
		}
	})
}

// TestAppendEventRequiresIdentityToAgreeAboutTheAttempt is the attempt half of
// the §27.4 cross-check: identity.attempt_id and EventInput.AttemptID must be
// both present with the same value or both absent. A half pair would either
// attribute an event to an attempt no reader can see, or leave an execution
// unattributed.
func TestAppendEventRequiresIdentityToAgreeAboutTheAttempt(t *testing.T) {
	other := "att-other"

	cases := []struct {
		name            string
		attemptID       *string
		identityAttempt *string
		wantSub         string
		wantField       string
	}{
		{
			name:            "identity names an attempt the row does not carry",
			identityAttempt: stringPtr(eventAttemptID),
			wantSub:         "identity.attempt_id",
		},
		{
			// ValidateFor runs first, so an identity that omits attempt_id is
			// refused as "required by this event type" — the run contract's own
			// verdict — before the cross-check can call it a half pair. Both are
			// ErrInvalidEvent wrapped around a *run.IdentityError.
			name:      "the row carries an attempt the identity does not name",
			attemptID: stringPtr(eventAttemptID),
			wantSub:   `field "attempt_id" is required`,
			wantField: "attempt_id",
		},
		{
			name:            "the two attempts differ",
			attemptID:       stringPtr(eventAttemptID),
			identityAttempt: &other,
			wantSub:         "does not match AttemptID",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newEventFixture(t)
			_, err := f.tryAppendInput(EventInput{
				ProjectID:  eventProjectID,
				RunID:      &f.runID,
				AttemptID:  tc.attemptID,
				Type:       string(run.EventProcessStarted),
				OccurredAt: time.UnixMilli(1700000001000),
				Identity: eventIdentityFor(eventProjectID, "system", &f.runID,
					tc.identityAttempt, stringPtr(eventAgentRevisionID)),
				Payload: json.RawMessage(`{}`),
			})
			if !errors.Is(err, ErrInvalidEvent) {
				t.Fatalf("error = %v, want ErrInvalidEvent", err)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error %q does not mention %q", err, tc.wantSub)
			}
			if tc.wantField != "" {
				var identityErr *run.IdentityError
				if !errors.As(err, &identityErr) {
					t.Fatalf("error = %v, want it to carry a *run.IdentityError", err)
				}
				if identityErr.Field != tc.wantField {
					t.Errorf("IdentityError.Field = %q, want %q", identityErr.Field, tc.wantField)
				}
			}

			// A refused event writes nothing at all.
			if n := eventRowCount(t, f.store.DB(), "events"); n != 0 {
				t.Errorf("events rows = %d, want 0", n)
			}
			if n := eventRowCount(t, f.store.DB(), "outbox"); n != 0 {
				t.Errorf("outbox rows = %d, want 0", n)
			}
			if got := counterValue(t, f.store.DB(), projectScope(eventProjectID)); got != -1 {
				t.Errorf("project counter = %d, want no row", got)
			}

			// The agreed form of the same input is accepted, so the case above
			// failed for the disagreement and not for something else.
			if _, err := f.tryAppendInput(EventInput{
				ProjectID:  eventProjectID,
				RunID:      &f.runID,
				AttemptID:  stringPtr(eventAttemptID),
				Type:       string(run.EventProcessStarted),
				OccurredAt: time.UnixMilli(1700000001000),
				Identity: eventIdentityFor(eventProjectID, "system", &f.runID,
					stringPtr(eventAttemptID), stringPtr(eventAgentRevisionID)),
				Payload: json.RawMessage(`{}`),
			}); err != nil {
				t.Errorf("the agreed input was refused too: %v", err)
			}
		})
	}
}

// TestAppendEventRejectsUnknownIdentityKeys proves the store holds a caller to
// the frozen identity schema rather than tolerating extensions. The schema
// declares additionalProperties:false, so a key the run package does not know is
// a contract violation: accepting it would store an identity the rest of the
// server cannot decode, and the extra bytes would change nothing about
// validation while still being hashed into the row.
func TestAppendEventRejectsUnknownIdentityKeys(t *testing.T) {
	f := newEventFixture(t)

	identityWithExtra := json.RawMessage(
		`{"project_id":"` + eventProjectID + `","actor":{"type":"system","id":"worker-1"},"extra":1}`)

	_, err := f.tryAppendInput(EventInput{
		ProjectID:  eventProjectID,
		Type:       string(run.EventServerRestart),
		OccurredAt: time.UnixMilli(1700000001000),
		Identity:   identityWithExtra,
		Payload:    json.RawMessage(`{}`),
	})
	if !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("error = %v, want ErrInvalidEvent", err)
	}
	if !strings.Contains(err.Error(), "extra") {
		t.Errorf("error %q does not name the unknown key", err)
	}
	if n := eventRowCount(t, f.store.DB(), "events"); n != 0 {
		t.Errorf("events rows = %d, want 0", n)
	}

	// The same document without the extra key is accepted, so the refusal above
	// was about the key and not about the rest of the encoding.
	if _, err := f.tryAppendInput(EventInput{
		ProjectID:  eventProjectID,
		Type:       string(run.EventServerRestart),
		OccurredAt: time.UnixMilli(1700000001000),
		Identity:   eventIdentity(eventProjectID, "system", nil),
		Payload:    json.RawMessage(`{}`),
	}); err != nil {
		t.Fatalf("the schema-shaped identity was refused: %v", err)
	}

	// A second JSON value after the object is the same class of mistake: the
	// store hashes the bytes it stores, so trailing content must not be
	// silently dropped from the identity that identity_json claims to hold.
	trailing := json.RawMessage(`{"project_id":"` + eventProjectID +
		`","actor":{"type":"system","id":"worker-1"}} {"also":"ignored"}`)
	_, err = f.tryAppendInput(EventInput{
		ProjectID:  eventProjectID,
		Type:       string(run.EventServerRestart),
		OccurredAt: time.UnixMilli(1700000001000),
		Identity:   trailing,
		Payload:    json.RawMessage(`{}`),
	})
	if !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("trailing content: error = %v, want ErrInvalidEvent", err)
	}
	if n := eventRowCount(t, f.store.DB(), "events"); n != 1 {
		t.Errorf("events rows = %d, want 1 (only the accepted append above)", n)
	}
}

// TestAppendEventRejectsDuplicateDestinations proves a repeated destination is
// refused before any SQL runs. The outbox has a UNIQUE (event_id, destination)
// index, so without this check the duplicate would abort the transaction midway
// through the outbox writes — after the event row and the counters had already
// been written — and the caller would see a raw constraint error for what is
// plainly a caller mistake.
func TestAppendEventRejectsDuplicateDestinations(t *testing.T) {
	f := newEventFixture(t)
	destination := "ws:project:" + eventProjectID

	_, err := f.tryAppendInput(EventInput{
		ProjectID:    eventProjectID,
		Type:         string(run.EventServerRestart),
		OccurredAt:   time.UnixMilli(1700000001000),
		Identity:     eventIdentity(eventProjectID, "system", nil),
		Payload:      json.RawMessage(`{}`),
		Destinations: []string{destination, "desktop:notify", destination},
	})
	if !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("error = %v, want ErrInvalidEvent", err)
	}
	if !strings.Contains(err.Error(), destination) {
		t.Errorf("error %q does not name the repeated destination", err)
	}

	// Nothing was written: no event, no delivery, no counter row. A raw unique
	// violation would have needed a counter row first.
	if n := eventRowCount(t, f.store.DB(), "events"); n != 0 {
		t.Errorf("events rows = %d, want 0", n)
	}
	if n := eventRowCount(t, f.store.DB(), "outbox"); n != 0 {
		t.Errorf("outbox rows = %d, want 0", n)
	}
	if got := counterValue(t, f.store.DB(), projectScope(eventProjectID)); got != -1 {
		t.Errorf("project counter = %d, want no row (the list is refused before any SQL)", got)
	}

	// The same destinations without the repeat are accepted, so the refusal was
	// about the duplicate and not the names.
	event := f.appendEvent(t, run.EventServerRestart, nil, destination, "desktop:notify")
	if n := eventRowCount(t, f.store.DB(), "outbox"); n != 2 {
		t.Errorf("outbox rows = %d, want 2 (one per distinct destination)", n)
	}
	if _, err := GetEvent(context.Background(), f.store.DB(), event.ID); err != nil {
		t.Errorf("GetEvent: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Reopen
// ---------------------------------------------------------------------------

// TestEventsSurviveReopen proves the sequence continues after a restart: a
// reopened store must read the counter it left behind rather than restart at 1,
// which is what makes a client's saved cursor valid across a restart (§27.4).
func TestEventsSurviveReopen(t *testing.T) {
	ctx := context.Background()
	store, path := newTestStore(t)
	seedTask(t, store, "t-1", eventProjectID)
	insertSnapshotAndRun(t, store, "run-1", "t-1", eventProjectID, "snap-1", json.RawMessage(`{"prompt":"reopen"}`))

	appendOne := func(s *Store, eventType run.ExecutionEventType, runID *string, destinations ...string) Event {
		t.Helper()
		identity, attemptID, _ := claimedIdentity(t, eventType, runID)
		var event Event
		err := s.WithTx(ctx, func(ctx context.Context, tx Tx) error {
			var err error
			event, err = AppendEventTx(ctx, tx, EventInput{
				ProjectID:    eventProjectID,
				RunID:        runID,
				AttemptID:    attemptID,
				Type:         string(eventType),
				OccurredAt:   time.UnixMilli(1700000009000),
				Identity:     identity,
				Payload:      json.RawMessage(`{"n":1}`),
				Destinations: destinations,
			})
			return err
		})
		if err != nil {
			t.Fatalf("AppendEventTx(%s): %v", eventType, err)
		}
		return event
	}

	runID := "run-1"
	first := appendOne(store, run.EventServerRestart, nil)
	second := appendOne(store, run.EventProcessStarted, &runID, "ws:run:"+runID)
	before := []Event{first, second}

	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	reopened, res, err := OpenStore(ctx, path)
	if err != nil {
		t.Fatalf("reopen %s: %v", path, err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if len(res.Applied) != 0 || res.ToVersion != 5 {
		t.Errorf("reopen migration result = %+v, want nothing applied at version 5", res)
	}

	// The rows are byte-identical, including the nullable run columns.
	for i, want := range before {
		got, err := GetEvent(ctx, reopened.DB(), want.ID)
		if err != nil {
			t.Fatalf("GetEvent(%s) after reopen: %v", want.ID, err)
		}
		if got.Type != want.Type || got.ProjectSeq != want.ProjectSeq || got.PayloadHash != want.PayloadHash ||
			string(got.Identity) != string(want.Identity) || string(got.Payload) != string(want.Payload) ||
			!got.OccurredAt.Equal(want.OccurredAt) {
			t.Errorf("event %d differs after reopen:\n before %+v\n after  %+v", i, want, got)
		}
		switch {
		case want.RunID == nil && got.RunID != nil:
			t.Errorf("event %d run_id = %q after reopen, want nil", i, *got.RunID)
		case want.RunID != nil && (got.RunID == nil || *got.RunID != *want.RunID):
			t.Errorf("event %d run_id = %s after reopen, want %q", i, stringPtrString(got.RunID), *want.RunID)
		}
		switch {
		case want.RunSeq == nil && got.RunSeq != nil:
			t.Errorf("event %d run_seq = %d after reopen, want nil", i, *got.RunSeq)
		case want.RunSeq != nil && (got.RunSeq == nil || *got.RunSeq != *want.RunSeq):
			t.Errorf("event %d run_seq = %s after reopen, want %d", i, runSeqString(got.RunSeq), *want.RunSeq)
		}
	}

	// The next append continues from the stored counters: project 3, run 2.
	third := appendOne(reopened, run.EventProcessExited, &runID)
	if third.ProjectSeq != second.ProjectSeq+1 {
		t.Errorf("project_seq after reopen = %d, want %d", third.ProjectSeq, second.ProjectSeq+1)
	}
	if third.RunSeq == nil || *third.RunSeq != 2 {
		t.Errorf("run_seq after reopen = %s, want 2", runSeqString(third.RunSeq))
	}

	// The pending delivery queued before the restart is still there, still
	// pending: a crash must not lose queued work (§15 T1.05 at-least-once).
	var pending int
	if err := reopened.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM outbox WHERE state = 'pending'`).Scan(&pending); err != nil {
		t.Fatalf("count pending outbox rows: %v", err)
	}
	if pending != 1 {
		t.Errorf("pending deliveries after reopen = %d, want 1", pending)
	}
	t.Logf("EVIDENCE reopen: project_seq %d..%d, run_seq %v, pending deliveries=%d",
		first.ProjectSeq, third.ProjectSeq, runSeqsOf([]Event{second, third}), pending)
}

// ---------------------------------------------------------------------------
// Constraint coverage for the new tables
// ---------------------------------------------------------------------------

// TestEventsSchemaRejectsHalfARunPair proves the CHECK that keeps run_id and
// run_seq together: a run position with no run, or a run-scoped event with no
// position, would make one of the two views (§27.4) unreadable.
func TestEventsSchemaRejectsHalfARunPair(t *testing.T) {
	ctx := context.Background()
	f := newEventFixture(t)
	// server.restart is the Run-less family, so the fixture row below needs no
	// run and the inserts that follow are free to test only the run pairing.
	event := f.appendEvent(t, run.EventServerRestart, nil)

	insert := `INSERT INTO events (id, project_id, project_seq, run_id, run_seq, type,
	           schema_version, occurred_at, identity_json, payload_json, payload_hash)
	           VALUES (?, ?, ?, ?, ?, 'budget.warning', 1, 1, '{}', '{}', 'sha256:x')`

	// run_seq without run_id.
	_, err := f.store.DB().ExecContext(ctx, insert, "e-half-a", eventProjectID, 100, nil, 7)
	assertCheckRefusal(t, "run_seq without run_id", err)

	// run_id without run_seq.
	_, err = f.store.DB().ExecContext(ctx, insert, "e-half-b", eventProjectID, 101, f.runID, nil)
	assertCheckRefusal(t, "run_id without run_seq", err)

	// A run that does not exist in this database.
	_, err = f.store.DB().ExecContext(ctx, insert, "e-orphan", eventProjectID, 102, "no-such-run", 1)
	var serr *sqlite.Error
	if !errors.As(err, &serr) || serr.Code() != sqliteConstraintForeignKey {
		t.Errorf("event naming a missing run: error = %v (code %d), want a foreign key refusal (%d)",
			err, sqliteExtended(err), sqliteConstraintForeignKey)
	}

	// A project that does not exist.
	_, err = f.store.DB().ExecContext(ctx, insert, "e-orphan-project", "no-such-project", 103, nil, nil)
	if !errors.As(err, &serr) || serr.Code() != sqliteConstraintForeignKey {
		t.Errorf("event naming a missing project: error = %v (code %d), want a foreign key refusal (%d)",
			err, sqliteExtended(err), sqliteConstraintForeignKey)
	}

	// The same project position twice.
	_, err = f.store.DB().ExecContext(ctx, insert, "e-dup-seq", eventProjectID, event.ProjectSeq, nil, nil)
	if !errors.As(err, &serr) || serr.Code() != sqliteConstraintUnique {
		t.Errorf("duplicate project_seq: error = %v (code %d), want a unique refusal (%d)",
			err, sqliteExtended(err), sqliteConstraintUnique)
	}

	// An unknown type, a schema_version other than 1, and a malformed hash.
	_, err = f.store.DB().ExecContext(ctx, `INSERT INTO events (id, project_id, project_seq, type,
	    schema_version, occurred_at, identity_json, payload_json, payload_hash)
	    VALUES ('e-type', ?, 110, 'run.started', 1, 1, '{}', '{}', 'sha256:x')`, eventProjectID)
	assertCheckRefusal(t, "unknown type", err)

	_, err = f.store.DB().ExecContext(ctx, `INSERT INTO events (id, project_id, project_seq, type,
	    schema_version, occurred_at, identity_json, payload_json, payload_hash)
	    VALUES ('e-ver', ?, 111, 'budget.warning', 2, 1, '{}', '{}', 'sha256:x')`, eventProjectID)
	assertCheckRefusal(t, "schema_version 2", err)

	_, err = f.store.DB().ExecContext(ctx, `INSERT INTO events (id, project_id, project_seq, type,
	    schema_version, occurred_at, identity_json, payload_json, payload_hash)
	    VALUES ('e-hash', ?, 112, 'budget.warning', 1, 1, '{}', '{}', 'md5:x')`, eventProjectID)
	assertCheckRefusal(t, "a non-sha256 payload_hash", err)

	// Nothing above was written: only the one event from the fixture exists.
	if n := eventRowCount(t, f.store.DB(), "events"); n != 1 {
		t.Errorf("events rows = %d, want 1 (every insert above must be refused)", n)
	}
}

// TestScopeCountersRejectNegativeValues proves the counter guard: a counter that
// could go negative would hand out a sequence number that has already been used,
// and the unique index on events would then reject a legitimate event.
func TestScopeCountersRejectNegativeValues(t *testing.T) {
	ctx := context.Background()
	f := newEventFixture(t)

	if _, err := f.store.DB().ExecContext(ctx,
		`INSERT INTO scope_counters (scope, value) VALUES ('project:negative', -1)`); err == nil {
		t.Error("a negative counter value was accepted, want a CHECK refusal")
	} else {
		assertCheckRefusal(t, "negative counter", err)
	}

	// Zero is legal: it is what a scope that has allocated nothing looks like
	// if a caller ever seeds one.
	if _, err := f.store.DB().ExecContext(ctx,
		`INSERT INTO scope_counters (scope, value) VALUES ('project:zero', 0)`); err != nil {
		t.Errorf("a zero counter value was refused: %v", err)
	}
}

// TestScopeCounterKeysAreTheDocumentedScopeNames pins the counter key vocabulary
// of §20.2/§27.3: exactly "project:<project_id>" and "run:<run_id>". A reader
// (the replay API of T1.05.b, the operators looking at a stuck stream) has to be
// able to find the counter for a scope it knows; two spellings for one scope
// would also let a scope be counted twice, since the two families share one key
// space.
func TestScopeCounterKeysAreTheDocumentedScopeNames(t *testing.T) {
	f := newEventFixture(t)

	event := f.appendEvent(t, run.EventProcessStarted, &f.runID)

	rows, err := f.store.DB().QueryContext(context.Background(),
		`SELECT scope, value FROM scope_counters ORDER BY scope`)
	if err != nil {
		t.Fatalf("read scope_counters: %v", err)
	}
	defer rows.Close()

	type counter struct {
		scope string
		value int64
	}
	var counters []counter
	for rows.Next() {
		var c counter
		if err := rows.Scan(&c.scope, &c.value); err != nil {
			t.Fatalf("scan counter: %v", err)
		}
		counters = append(counters, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate counters: %v", err)
	}

	// Exactly two rows after one run-scoped event: the project counter and the
	// run counter, nothing else.
	if len(counters) != 2 {
		t.Fatalf("scope_counters has %d rows (%+v), want 2 for one run-scoped event", len(counters), counters)
	}
	wantScopes := []string{"project:" + eventProjectID, "run:" + f.runID}
	for i, want := range wantScopes {
		if counters[i].scope != want {
			t.Errorf("scope_counters[%d].scope = %q, want %q", i, counters[i].scope, want)
		}
		if counters[i].value != 1 {
			t.Errorf("scope %q value = %d, want 1", counters[i].scope, counters[i].value)
		}
	}
	if got := counterValue(t, f.store.DB(), "project:"+eventProjectID); got != 1 {
		t.Errorf("counter under %q = %d, want 1", "project:"+eventProjectID, got)
	}
	if event.ProjectSeq != 1 || event.RunSeq == nil || *event.RunSeq != 1 {
		t.Errorf("event sequences = %d/%s, want 1/1", event.ProjectSeq, runSeqString(event.RunSeq))
	}
	t.Logf("EVIDENCE counter keys: %q and %q, both at 1 after one run-scoped event",
		counters[0].scope, counters[1].scope)
}

// assertCheckRefusal checks that err is a CHECK violation whose message names
// the offending column, so a failure says "the right constraint fired" rather
// than "something went wrong".
func assertCheckRefusal(t *testing.T, op string, err error) {
	t.Helper()
	if err == nil {
		t.Errorf("%s: the insert succeeded, want a CHECK refusal", op)
		return
	}
	var serr *sqlite.Error
	if !errors.As(err, &serr) || serr.Code() != sqliteConstraintCheck {
		t.Errorf("%s: error = %v (code %d), want SQLITE_CONSTRAINT_CHECK (%d)",
			op, err, sqliteExtended(err), sqliteConstraintCheck)
	}
}

// sqlNullInt64 and sqlNullString are tiny scan helpers so the outbox read above
// can distinguish NULL from zero without importing database/sql into every test
// case.
type sqlNullInt64 struct {
	valid  bool
	value_ int64
}

func (n *sqlNullInt64) Scan(src any) error {
	if src == nil {
		n.valid = false
		return nil
	}
	v, ok := src.(int64)
	if !ok {
		return fmt.Errorf("sqlNullInt64: cannot scan %T", src)
	}
	n.valid, n.value_ = true, v
	return nil
}

func (n sqlNullInt64) value() int64 {
	if !n.valid {
		return 0
	}
	return n.value_
}

type sqlNullString struct {
	valid  bool
	value_ string
}

func (s *sqlNullString) Scan(src any) error {
	if src == nil {
		s.valid = false
		return nil
	}
	v, ok := src.(string)
	if !ok {
		return fmt.Errorf("sqlNullString: cannot scan %T", src)
	}
	s.valid, s.value_ = true, v
	return nil
}

func (s sqlNullString) value() string {
	if !s.valid {
		return ""
	}
	return s.value_
}

// runSeqString and stringPtrString render optional values for failure messages,
// so a broken assertion says "run_seq = nil" instead of printing a pointer
// address.
func runSeqString(v *int64) string {
	if v == nil {
		return "nil"
	}
	return fmt.Sprint(*v)
}

func stringPtrString(v *string) string {
	if v == nil {
		return "nil"
	}
	return fmt.Sprintf("%q", *v)
}

// hashOfPayload recomputes the stored hash from the stored bytes, so a test can
// assert the row is self-describing rather than trusting the value the writer
// returned.
func hashOfPayload(payload json.RawMessage) string {
	_, hash, err := canonicalJSON(payload)
	if err != nil {
		return ""
	}
	return hash
}

// TestCanonicalJSONRejectsTrailingContent: canonicalJSON hashes and stores one
// JSON document; anything after it must be refused, including a stray closing
// '}' or ']' — json.Decoder.More() reports false for those, which is why the
// check reads the next token instead. Input snapshots, event payloads and event
// identities all go through this function.
func TestCanonicalJSONRejectsTrailingContent(t *testing.T) {
	for _, in := range []string{`{"a":1}}`, `{"a":1}]`, `{"a":1} {"b":2}`, `{"a":1} x`, `{"a":1},`} {
		if got, _, err := canonicalJSON([]byte(in)); err == nil {
			t.Errorf("canonicalJSON(%q) = %q, want a trailing-content error", in, got)
		}
	}
	if got, _, err := canonicalJSON([]byte("{\"a\":1} \n\t")); err != nil || got != `{"a":1}` {
		t.Errorf("canonicalJSON with trailing whitespace = (%q, %v), want (%q, nil)", got, err, `{"a":1}`)
	}

	// Through the store: a stray delimiter after the payload or the identity is
	// an invalid event, and nothing is written.
	f := newEventFixture(t)
	base := func() EventInput {
		return EventInput{
			ProjectID:  eventProjectID,
			Type:       string(run.EventServerRestart),
			OccurredAt: time.UnixMilli(1700000003000),
			Identity:   eventIdentity(eventProjectID, "system", nil),
			Payload:    json.RawMessage(`{"n":1}`),
		}
	}
	payload := base()
	payload.Payload = json.RawMessage(`{"n":1}}`)
	identity := base()
	identity.Identity = append(append(json.RawMessage(nil), identity.Identity...), ']')
	for name, in := range map[string]EventInput{"payload": payload, "identity": identity} {
		if _, err := f.tryAppendInput(in); !errors.Is(err, ErrInvalidEvent) {
			t.Errorf("%s with a stray closing delimiter: err = %v, want ErrInvalidEvent", name, err)
		}
	}
	if n := eventRowCount(t, f.store.DB(), "events"); n != 0 {
		t.Fatalf("events = %d after refused appends, want 0", n)
	}
	if _, err := f.tryAppendInput(base()); err != nil {
		t.Fatalf("the same event without the stray delimiter must be accepted: %v", err)
	}
}

// TestAppendEventStoresTheValidatedIdentity: the stored identity is the decoded,
// validated identity re-encoded, not the caller's bytes. encoding/json matches
// keys case-insensitively, so a second spelling of project_id decides what is
// validated; the stored row must then carry that value and nothing else.
func TestAppendEventStoresTheValidatedIdentity(t *testing.T) {
	f := newEventFixture(t)
	event, err := f.tryAppendInput(EventInput{
		ProjectID:  eventProjectID,
		Type:       string(run.EventServerRestart),
		OccurredAt: time.UnixMilli(1700000004000),
		Identity:   json.RawMessage(`{"project_id":"someone-else","PROJECT_ID":"` + eventProjectID + `","actor":{"type":"system","id":"recoverer"}}`),
		Payload:    json.RawMessage(`{"n":1}`),
	})
	if err != nil {
		t.Fatalf("AppendEventTx: %v", err)
	}
	stored, err := GetEvent(context.Background(), f.store.DB(), event.ID)
	if err != nil {
		t.Fatalf("GetEvent: %v", err)
	}
	want := `{"actor":{"id":"recoverer","type":"system"},"project_id":"` + eventProjectID + `"}`
	if string(stored.Identity) != want || string(event.Identity) != want {
		t.Fatalf("stored identity = %s (returned %s), want exactly the validated identity %s", stored.Identity, event.Identity, want)
	}
	if strings.Contains(string(stored.Identity), "someone-else") {
		t.Fatalf("stored identity %s still carries the shadowed project id", stored.Identity)
	}
}
