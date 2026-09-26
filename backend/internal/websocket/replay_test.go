package websocket

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/run"
	"github.com/codeflow/backend/internal/runstore"
)

// These tests cover replay.go: the ordered-subscription blocks of T1.12.a. The
// properties they exist to prove are the ones §20.3/§27.4/§28 make load-bearing:
//
//   - the subscribe frame is parsed strictly: a case variant of a wire key is a
//     different key (encoding/json would accept it), an unknown key is refused,
//     `after` is a non-negative integer or nothing, and trailing content is a
//     rejection rather than a silently dropped delimiter;
//   - authorization is scoped to the connection's project: a connection to
//     project A cannot subscribe project B, and cannot tell "B's run" from "no
//     such run" — the error value and its text are identical;
//   - the wire event is the schema's event: its key set equals
//     schemas/execution-event.schema.json's properties, its sequence is
//     projected on the subscription's counter (project_seq or run_seq, never
//     mixed), and it never carries an event outside the grant;
//   - the replay page is the store's page: NextAfter/HasMore/high_watermark/
//     retention_floor pass through, and the two cursor errors stay identifiable
//     (ErrInvalidCursor for 422, ErrCursorExpired for 410);
//   - every frame has the fixed {"type","data"} shape of §20.3, and the error
//     frames carry no resource identity and no copy of the client's input.

// The two projects and their runs. Real ids are UUIDs (§20.4: the examples'
// p_123/run_88 are reading aliases), so the fixtures use UUIDs too.
const (
	testProjectA = "11111111-1111-4111-8111-111111111111"
	testProjectB = "22222222-2222-4222-8222-222222222222"
	testRunA     = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	testRunB     = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	testRunNone  = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// newReplayStore opens a real runtime database in a temporary directory. The
// store is the same one the runtime library uses, so the replay tests read the
// actual SQL snapshot of runstore rather than a fake.
func newReplayStore(t *testing.T) *runstore.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "codeflow.db")
	store, _, err := runstore.OpenStore(context.Background(), path)
	if err != nil {
		t.Fatalf("OpenStore(%s): %v", path, err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// taskIDFor names the one task each seeded project gets.
func taskIDFor(projectID string) string { return "task-" + projectID }

// seedProject writes the project ref and one ready task through the store's own
// typed operations, so the fixture cannot drift from the schema.
func seedProject(t *testing.T, store *runstore.Store, projectID string) {
	t.Helper()
	err := store.WithTx(context.Background(), func(ctx context.Context, tx runstore.Tx) error {
		if err := runstore.UpsertProjectRef(ctx, tx, run.ProjectRef{
			ProjectID:    projectID,
			SnapshotHash: "sha256:project",
			State:        run.ProjectRefStateActive,
			CapturedAt:   time.UnixMilli(1700000000000),
			VerifiedAt:   time.UnixMilli(1700000000001),
		}); err != nil {
			return err
		}
		task := run.Task{
			ID:        taskIDFor(projectID),
			ProjectID: projectID,
			Title:     "subscribe",
			Kind:      run.TaskKindCode,
			Status:    run.TaskStatusReady,
			Priority:  3,
			InputJSON: `{"prompt":"subscribe"}`,
			CreatedAt: time.UnixMilli(1700000000000),
			UpdatedAt: time.UnixMilli(1700000000001),
		}
		return runstore.InsertTask(ctx, tx, &task)
	})
	if err != nil {
		t.Fatalf("seed project %s: %v", projectID, err)
	}
}

// seedRun writes one input snapshot and one queued run pinned to it. The run's
// project is the authorization fact every run subscription is checked against.
func seedRun(t *testing.T, store *runstore.Store, projectID, runID string) {
	t.Helper()
	snapshotID := "snap-" + runID
	err := store.WithTx(context.Background(), func(ctx context.Context, tx runstore.Tx) error {
		hash, err := runstore.InsertInputSnapshot(ctx, tx, projectID, snapshotID,
			json.RawMessage(`{"prompt":"subscribe"}`), time.UnixMilli(1700000000002))
		if err != nil {
			return err
		}
		r := run.Run{
			ID:                runID,
			TaskID:            taskIDFor(projectID),
			ProjectID:         projectID,
			BindingID:         "binding-1",
			BindingRevision:   1,
			BaseManifestHash:  "sha256:manifest",
			AgentRevisionID:   "agent-rev-1",
			InputSnapshotID:   snapshotID,
			InputSnapshotHash: hash,
			Budget:            run.Budget{Tokens: int64Ptr(50000)},
			Status:            run.RunStatusQueued,
			CreatedAt:         time.UnixMilli(1700000000002),
			UpdatedAt:         time.UnixMilli(1700000000003),
		}
		return runstore.InsertRun(ctx, tx, &r)
	})
	if err != nil {
		t.Fatalf("seed run %s: %v", runID, err)
	}
}

func int64Ptr(v int64) *int64 { return &v }

// replayFixture is one store with both projects and both runs, plus the two
// adapters under test. Every appended event gets a distinct instant so claim
// order and replay order agree.
type replayFixture struct {
	t      *testing.T
	store  *runstore.Store
	reader EventReader
	auth   Authorizer
	next   int64
}

func newReplayFixture(t *testing.T) *replayFixture {
	t.Helper()
	store := newReplayStore(t)
	seedProject(t, store, testProjectA)
	seedProject(t, store, testProjectB)
	seedRun(t, store, testProjectA, testRunA)
	seedRun(t, store, testProjectB, testRunB)
	return &replayFixture{
		t:      t,
		store:  store,
		reader: RunstoreEventReader(store.DB()),
		auth:   Authorizer{Runs: RunstoreRunLookup(store.DB())},
		next:   1700000001000,
	}
}

// projectEvent appends one project-level event (no Run) and returns it as stored.
func (f *replayFixture) projectEvent(projectID string) runstore.Event {
	f.next += 1000
	return appendTestEvent(f.t, f.store, projectID, nil, run.EventServerRestart, f.next)
}

// projectEventAt appends one project-level event at an explicit instant, for the
// occurred_at format test.
func (f *replayFixture) projectEventAt(projectID string, atMillis int64) runstore.Event {
	return appendTestEvent(f.t, f.store, projectID, nil, run.EventServerRestart, atMillis)
}

// legacyEvent appends one project-level legacy Flow event: the §27.1/CA-2 case
// of a fact that belongs to a project and to no Run.
func (f *replayFixture) legacyEvent(projectID string) runstore.Event {
	f.next += 1000
	return appendTestEvent(f.t, f.store, projectID, nil, run.EventLegacyFlowEvent, f.next)
}

// runEvent appends one run-scoped event and returns it as stored (so a test can
// compare project_seq and run_seq of the same row).
func (f *replayFixture) runEvent(projectID, runID string) runstore.Event {
	f.next += 1000
	return appendTestEvent(f.t, f.store, projectID, &runID, run.EventRunFailed, f.next)
}

// appendTestEvent writes one valid event through the store. The identity is
// built from run.RequiredIdentity so the fixture cannot hand the store an
// envelope the contract would refuse.
func appendTestEvent(t *testing.T, store *runstore.Store, projectID string, runID *string, eventType run.ExecutionEventType, atMillis int64) runstore.Event {
	t.Helper()
	req, err := run.RequiredIdentity(string(eventType))
	if err != nil {
		t.Fatalf("RequiredIdentity(%q): %v", eventType, err)
	}
	if req.Run && runID == nil {
		t.Fatalf("event type %q requires a run; the fixture was called without one", eventType)
	}
	var attemptID *string
	if req.Attempt {
		attemptID = stringPtr("att-1")
	}
	var event runstore.Event
	err = store.WithTx(context.Background(), func(ctx context.Context, tx runstore.Tx) error {
		var err error
		event, err = runstore.AppendEventTx(ctx, tx, runstore.EventInput{
			ProjectID:  projectID,
			RunID:      runID,
			AttemptID:  attemptID,
			Type:       string(eventType),
			OccurredAt: time.UnixMilli(atMillis),
			Identity:   testIdentity(t, projectID, runID, attemptID),
			Payload:    json.RawMessage(`{"n":1}`),
		})
		return err
	})
	if err != nil {
		t.Fatalf("AppendEventTx(%s): %v", eventType, err)
	}
	return event
}

// testIdentity builds the minimal ExecutionIdentity envelope for a fixture event.
func testIdentity(t *testing.T, projectID string, runID, attemptID *string) json.RawMessage {
	t.Helper()
	envelope := map[string]any{
		"project_id": projectID,
		"actor":      map[string]any{"type": "system", "id": "worker-1"},
	}
	if runID != nil {
		envelope["run_id"] = *runID
	}
	if attemptID != nil {
		envelope["attempt_id"] = *attemptID
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal identity: %v", err)
	}
	return encoded
}

// stringPtr returns a pointer to a copy of s.
func stringPtr(s string) *string { return &s }

// subscribeFrameJSON renders a subscribe frame the way a client would send it.
func subscribeFrameJSON(resourceType, resourceID string, after *int64, clientRequestID *string) string {
	data := map[string]any{"resource_type": resourceType, "resource_id": resourceID}
	if after != nil {
		data["after"] = *after
	}
	if clientRequestID != nil {
		data["client_request_id"] = *clientRequestID
	}
	encoded, err := json.Marshal(map[string]any{"type": "subscribe", "data": data})
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

// mustParse parses a frame a test built itself, failing on a refusal.
func mustParse(t *testing.T, raw string) SubscribeRequest {
	t.Helper()
	req, err := ParseSubscribeFrame([]byte(raw))
	if err != nil {
		t.Fatalf("ParseSubscribeFrame(%s): %v", raw, err)
	}
	return req
}

// presetScopeCounter forces a scope's counter, which is how a test models history
// that retention already trimmed: the next event gets value+1 and the retained
// history starts there (the same technique runstore/replay_test.go uses).
func presetScopeCounter(t *testing.T, store *runstore.Store, scope string, value int64) {
	t.Helper()
	if _, err := store.DB().ExecContext(context.Background(), `
		INSERT INTO scope_counters (scope, value) VALUES (?, ?)
		ON CONFLICT (scope) DO UPDATE SET value = ?`, scope, value, value); err != nil {
		t.Fatalf("set counter %s to %d: %v", scope, value, err)
	}
}

// projectScopeName is runstore's scope key for a project counter.
func projectScopeName(projectID string) string { return "project:" + projectID }

// ---------------------------------------------------------------------------
// Frame parsing
// ---------------------------------------------------------------------------

// TestParseSubscribeFrameAcceptsSection203Frame pins the frame §20.3 prints,
// verbatim, plus the project variant and the two optional fields being absent.
func TestParseSubscribeFrameAcceptsSection203Frame(t *testing.T) {
	// §20.3's frame, byte for byte.
	section203 := `{"type":"subscribe","data":{"resource_type":"run","resource_id":"run_88","after":41,"client_request_id":"sub_1"}}`
	req, err := ParseSubscribeFrame([]byte(section203))
	if err != nil {
		t.Fatalf("ParseSubscribeFrame(§20.3 frame): %v", err)
	}
	if req.ResourceType != ResourceTypeRun || req.ResourceID != "run_88" || req.After != 41 || req.ClientRequestID != "sub_1" {
		t.Errorf("parsed §20.3 frame = %+v, want run/run_88/41/sub_1", req)
	}

	after := int64(0)
	clientRequestID := "sub_2"
	cases := []struct {
		name string
		raw  string
		want SubscribeRequest
	}{
		{
			name: "project scope with every field",
			raw:  subscribeFrameJSON(ResourceTypeProject, "p_123", &after, &clientRequestID),
			want: SubscribeRequest{ResourceType: ResourceTypeProject, ResourceID: "p_123", After: 0, ClientRequestID: "sub_2"},
		},
		{
			name: "run scope without after and without client_request_id",
			raw:  subscribeFrameJSON(ResourceTypeRun, "run_88", nil, nil),
			want: SubscribeRequest{ResourceType: ResourceTypeRun, ResourceID: "run_88", After: 0},
		},
		{
			name: "whitespace and key order do not matter",
			raw:  "{\n \"data\" : {\"client_request_id\":\"sub_3\",\"after\":7,\"resource_id\":\"run_88\",\"resource_type\":\"run\"} , \"type\" : \"subscribe\"\n}",
			want: SubscribeRequest{ResourceType: ResourceTypeRun, ResourceID: "run_88", After: 7, ClientRequestID: "sub_3"},
		},
		{
			name: "resource id of exactly 128 bytes",
			raw:  subscribeFrameJSON(ResourceTypeRun, strings.Repeat("r", 128), nil, nil),
			want: SubscribeRequest{ResourceType: ResourceTypeRun, ResourceID: strings.Repeat("r", 128)},
		},
		{
			name: "client_request_id of exactly 128 bytes",
			raw:  subscribeFrameJSON(ResourceTypeRun, "run_88", nil, stringPtr(strings.Repeat("c", 128))),
			want: SubscribeRequest{ResourceType: ResourceTypeRun, ResourceID: "run_88", ClientRequestID: strings.Repeat("c", 128)},
		},
		{
			name: "empty client_request_id means no correlation",
			raw:  subscribeFrameJSON(ResourceTypeRun, "run_88", nil, stringPtr("")),
			want: SubscribeRequest{ResourceType: ResourceTypeRun, ResourceID: "run_88"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseSubscribeFrame([]byte(tc.raw))
			if err != nil {
				t.Fatalf("ParseSubscribeFrame(%s): %v", tc.raw, err)
			}
			if got != tc.want {
				t.Errorf("parsed frame = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestParseSubscribeFrameAcceptsFrameAtTheSizeLimit pins the 64 KiB control-frame
// bound (§27.4) from both sides: exactly 64 KiB is a valid frame, one byte more is
// not.
func TestParseSubscribeFrameAcceptsFrameAtTheSizeLimit(t *testing.T) {
	base := subscribeFrameJSON(ResourceTypeRun, "run_88", nil, nil)
	if len(base) > MaxSubscribeFrameBytes {
		t.Fatalf("base frame is %d bytes, already over the limit", len(base))
	}
	// Trailing whitespace is insignificant JSON, so padding is a legal way to
	// reach the bound.
	atLimit := base + strings.Repeat(" ", MaxSubscribeFrameBytes-len(base))
	if len(atLimit) != MaxSubscribeFrameBytes {
		t.Fatalf("padded frame is %d bytes, want %d", len(atLimit), MaxSubscribeFrameBytes)
	}
	if _, err := ParseSubscribeFrame([]byte(atLimit)); err != nil {
		t.Errorf("ParseSubscribeFrame(frame of exactly %d bytes): %v", MaxSubscribeFrameBytes, err)
	}

	overLimit := atLimit + " "
	if _, err := ParseSubscribeFrame([]byte(overLimit)); !errors.Is(err, ErrInvalidSubscribe) {
		t.Errorf("ParseSubscribeFrame(frame of %d bytes) error = %v, want ErrInvalidSubscribe", len(overLimit), err)
	} else if reason := SubscribeErrorReason(err); reason != ReasonFrameTooLarge {
		t.Errorf("reason = %q, want %q", reason, ReasonFrameTooLarge)
	}
}

// TestParseSubscribeFrameRejectsCaseVariantsAndUnknownKeys is the strictness that
// makes the parser worth having: encoding/json would accept "RESOURCE_ID" for a
// `resource_id` tag, and DisallowUnknownFields would not stop it. Every variant
// here is a different key and must be refused.
func TestParseSubscribeFrameRejectsCaseVariantsAndUnknownKeys(t *testing.T) {
	cases := []struct {
		name   string
		raw    string
		reason string
	}{
		{
			name:   "uppercase data key",
			raw:    `{"type":"subscribe","data":{"RESOURCE_ID":"run_88","resource_type":"run"}}`,
			reason: ReasonUnknownField,
		},
		{
			name:   "mixed-case data key",
			raw:    `{"type":"subscribe","data":{"Resource_Type":"run","resource_id":"run_88"}}`,
			reason: ReasonUnknownField,
		},
		{
			name:   "uppercase after",
			raw:    `{"type":"subscribe","data":{"resource_type":"run","resource_id":"run_88","AFTER":1}}`,
			reason: ReasonUnknownField,
		},
		{
			name:   "mixed-case client_request_id",
			raw:    `{"type":"subscribe","data":{"resource_type":"run","resource_id":"run_88","Client_Request_Id":"sub_1"}}`,
			reason: ReasonUnknownField,
		},
		{
			name:   "uppercase top-level type",
			raw:    `{"TYPE":"subscribe","data":{"resource_type":"run","resource_id":"run_88"}}`,
			reason: ReasonUnknownField,
		},
		{
			name:   "uppercase top-level data",
			raw:    `{"type":"subscribe","DATA":{"resource_type":"run","resource_id":"run_88"}}`,
			reason: ReasonUnknownField,
		},
		{
			name:   "unknown data key",
			raw:    `{"type":"subscribe","data":{"resource_type":"run","resource_id":"run_88","topic":"flow_event"}}`,
			reason: ReasonUnknownField,
		},
		{
			name:   "unknown top-level key",
			raw:    `{"type":"subscribe","session_id":"session-1","data":{"resource_type":"run","resource_id":"run_88"}}`,
			reason: ReasonUnknownField,
		},
		{
			name:   "duplicate resource_id",
			raw:    `{"type":"subscribe","data":{"resource_type":"run","resource_id":"run_88","resource_id":"run_99"}}`,
			reason: ReasonDuplicateField,
		},
		{
			name:   "duplicate type",
			raw:    `{"type":"subscribe","type":"subscribe","data":{"resource_type":"run","resource_id":"run_88"}}`,
			reason: ReasonDuplicateField,
		},
		{
			name:   "duplicate data",
			raw:    `{"type":"subscribe","data":{"resource_type":"run","resource_id":"run_88"},"data":{"resource_type":"run","resource_id":"run_99"}}`,
			reason: ReasonDuplicateField,
		},
		{
			name:   "duplicate after",
			raw:    `{"type":"subscribe","data":{"resource_type":"run","resource_id":"run_88","after":1,"after":2}}`,
			reason: ReasonDuplicateField,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseSubscribeFrame([]byte(tc.raw))
			if !errors.Is(err, ErrInvalidSubscribe) {
				t.Fatalf("ParseSubscribeFrame(%s) error = %v, want ErrInvalidSubscribe", tc.raw, err)
			}
			if reason := SubscribeErrorReason(err); reason != tc.reason {
				t.Errorf("reason = %q, want %q", reason, tc.reason)
			}
			// The refusal must not hand the input back to the caller: the frame
			// built from it carries a fixed phrase only.
			frame := InvalidRequestFrame("sub_1", SubscribeErrorReason(err))
			if bytes.Contains(frame, []byte(tc.raw)) {
				t.Errorf("invalid_request frame echoes the input: %s", frame)
			}
		})
	}
}

// TestParseSubscribeFrameRejectsBadTypesAndValues walks the value rules: the type
// must be "subscribe", data must be an object with a usable resource, `after` must
// be a non-negative integer, and the ids must fit their limits.
func TestParseSubscribeFrameRejectsBadTypesAndValues(t *testing.T) {
	long := strings.Repeat("x", 129)
	cases := []struct {
		name   string
		raw    string
		reason string
	}{
		{"type is unsubscribe", `{"type":"unsubscribe","data":{"resource_type":"run","resource_id":"run_88"}}`, ReasonWrongFrameType},
		{"type missing", `{"data":{"resource_type":"run","resource_id":"run_88"}}`, ReasonWrongFrameType},
		{"type is a number", `{"type":1,"data":{"resource_type":"run","resource_id":"run_88"}}`, ReasonWrongFrameType},
		{"type is null", `{"type":null,"data":{"resource_type":"run","resource_id":"run_88"}}`, ReasonWrongFrameType},
		{"data missing", `{"type":"subscribe"}`, ReasonInvalidData},
		{"data is a string", `{"type":"subscribe","data":"run_88"}`, ReasonInvalidData},
		{"data is an array", `{"type":"subscribe","data":[]}`, ReasonInvalidData},
		{"data is null", `{"type":"subscribe","data":null}`, ReasonInvalidData},
		{"resource_type missing", `{"type":"subscribe","data":{"resource_id":"run_88"}}`, ReasonInvalidResourceType},
		{"resource_type flow", `{"type":"subscribe","data":{"resource_type":"flow","resource_id":"f_1"}}`, ReasonInvalidResourceType},
		{"resource_type empty", `{"type":"subscribe","data":{"resource_type":"","resource_id":"run_88"}}`, ReasonInvalidResourceType},
		{"resource_type Run", `{"type":"subscribe","data":{"resource_type":"Run","resource_id":"run_88"}}`, ReasonInvalidResourceType},
		{"resource_type is a number", `{"type":"subscribe","data":{"resource_type":1,"resource_id":"run_88"}}`, ReasonInvalidResourceType},
		{"resource_id missing", `{"type":"subscribe","data":{"resource_type":"run"}}`, ReasonInvalidResourceID},
		{"resource_id empty", `{"type":"subscribe","data":{"resource_type":"run","resource_id":""}}`, ReasonInvalidResourceID},
		{"resource_id blank", `{"type":"subscribe","data":{"resource_type":"run","resource_id":"   "}}`, ReasonInvalidResourceID},
		{"resource_id leading space", `{"type":"subscribe","data":{"resource_type":"run","resource_id":" run_88"}}`, ReasonInvalidResourceID},
		{"resource_id trailing space", `{"type":"subscribe","data":{"resource_type":"run","resource_id":"run_88 "}}`, ReasonInvalidResourceID},
		{"resource_id contains the scope separator", `{"type":"subscribe","data":{"resource_type":"run","resource_id":"run:88"}}`, ReasonInvalidResourceID},
		{"resource_id 129 bytes", subscribeFrameJSON(ResourceTypeRun, long, nil, nil), ReasonInvalidResourceID},
		{"resource_id is a number", `{"type":"subscribe","data":{"resource_type":"run","resource_id":88}}`, ReasonInvalidResourceID},
		{"after negative", `{"type":"subscribe","data":{"resource_type":"run","resource_id":"run_88","after":-1}}`, ReasonInvalidAfter},
		{"after decimal", `{"type":"subscribe","data":{"resource_type":"run","resource_id":"run_88","after":1.5}}`, ReasonInvalidAfter},
		{"after integer-valued decimal", `{"type":"subscribe","data":{"resource_type":"run","resource_id":"run_88","after":41.0}}`, ReasonInvalidAfter},
		{"after exponent", `{"type":"subscribe","data":{"resource_type":"run","resource_id":"run_88","after":1e3}}`, ReasonInvalidAfter},
		{"after string", `{"type":"subscribe","data":{"resource_type":"run","resource_id":"run_88","after":"41"}}`, ReasonInvalidAfter},
		{"after null", `{"type":"subscribe","data":{"resource_type":"run","resource_id":"run_88","after":null}}`, ReasonInvalidAfter},
		{"after boolean", `{"type":"subscribe","data":{"resource_type":"run","resource_id":"run_88","after":true}}`, ReasonInvalidAfter},
		{"after overflows int64", `{"type":"subscribe","data":{"resource_type":"run","resource_id":"run_88","after":99999999999999999999}}`, ReasonInvalidAfter},
		{"client_request_id 129 bytes", subscribeFrameJSON(ResourceTypeRun, "run_88", nil, stringPtr(long)), ReasonInvalidClientRequestID},
		{"client_request_id is a number", `{"type":"subscribe","data":{"resource_type":"run","resource_id":"run_88","client_request_id":1}}`, ReasonInvalidClientRequestID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseSubscribeFrame([]byte(tc.raw))
			if !errors.Is(err, ErrInvalidSubscribe) {
				t.Fatalf("ParseSubscribeFrame(%s) error = %v, want ErrInvalidSubscribe", tc.raw, err)
			}
			if reason := SubscribeErrorReason(err); reason != tc.reason {
				t.Errorf("reason = %q, want %q", reason, tc.reason)
			}
		})
	}
}

// TestParseSubscribeFrameRejectsTrailingContent covers the More() trap named in
// the card: dec.More() reports false for a stray closing brace, so a parser built
// on it would accept `{...}}` and drop the extra delimiter. The check must read a
// token and require io.EOF.
func TestParseSubscribeFrameRejectsTrailingContent(t *testing.T) {
	base := subscribeFrameJSON(ResourceTypeRun, "run_88", nil, nil)
	cases := []struct {
		name string
		raw  string
	}{
		{"stray closing brace", base + "}"},
		{"stray closing bracket", base + "]"},
		{"second frame", base + base},
		{"trailing garbage", base + "garbage"},
		{"trailing number", base + " 1"},
		{"empty frame", ""},
		{"whitespace only", "   \n\t "},
		{"bare string", `"subscribe"`},
		{"bare array", `[{"type":"subscribe"}]`},
		{"truncated object", `{"type":"subscribe","data":{"resource_type":"run","resource_id":"run_88"`},
		{"truncated data", `{"type":"subscribe","data":`},
		{"unterminated string", `{"type":"subscribe","data":{"resource_type":"run","resource_id":"run_88`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseSubscribeFrame([]byte(tc.raw))
			if !errors.Is(err, ErrInvalidSubscribe) {
				t.Fatalf("ParseSubscribeFrame(%q) error = %v, want ErrInvalidSubscribe", tc.raw, err)
			}
			reason := SubscribeErrorReason(err)
			if reason != ReasonNotJSONObject && reason != ReasonInvalidData {
				t.Errorf("reason = %q, want a malformed-frame reason", reason)
			}
		})
	}
}

// TestSubscribeErrorReasonIsAlwaysAFixedPhrase pins the closed reason set: no
// error, whatever it holds, can put foreign text into an invalid_request frame.
func TestSubscribeErrorReasonIsAlwaysAFixedPhrase(t *testing.T) {
	fixed := map[string]bool{
		ReasonFrameTooLarge:          true,
		ReasonNotJSONObject:          true,
		ReasonUnknownField:           true,
		ReasonDuplicateField:         true,
		ReasonWrongFrameType:         true,
		ReasonInvalidData:            true,
		ReasonInvalidResourceType:    true,
		ReasonInvalidResourceID:      true,
		ReasonInvalidAfter:           true,
		ReasonInvalidClientRequestID: true,
		ReasonUnusableFrame:          true,
	}
	for _, err := range []error{
		nil,
		errors.New("some other failure: resource_id=secret-marker"),
		&SubscribeError{Reason: ReasonUnknownField},
		&SubscribeError{Reason: ReasonInvalidAfter, Field: "data.after"},
	} {
		reason := SubscribeErrorReason(err)
		if !fixed[reason] {
			t.Errorf("SubscribeErrorReason(%v) = %q, which is not a fixed phrase", err, reason)
		}
		if strings.Contains(reason, "secret-marker") {
			t.Errorf("reason %q echoes the error text", reason)
		}
	}
	// A nil error must not panic and must not invent a reason from nothing.
	if got := SubscribeErrorReason(nil); got != ReasonUnusableFrame {
		t.Errorf("SubscribeErrorReason(nil) = %q, want %q", got, ReasonUnusableFrame)
	}
}

// ---------------------------------------------------------------------------
// Authorization
// ---------------------------------------------------------------------------

// TestAuthorizeProjectScopeIsBoundToTheConnectionProject is the T1.12 acceptance
// assertion on the server side: a connection to project A can subscribe project A
// and cannot subscribe project B, in either direction.
func TestAuthorizeProjectScopeIsBoundToTheConnectionProject(t *testing.T) {
	f := newReplayFixture(t)
	ctx := context.Background()

	grant, err := f.auth.Authorize(ctx, testProjectA, mustParse(t, subscribeFrameJSON(ResourceTypeProject, testProjectA, nil, stringPtr("sub_1"))))
	if err != nil {
		t.Fatalf("authorize project A from a project A connection: %v", err)
	}
	want := Grant{ProjectID: testProjectA, Scope: Scope{Kind: ResourceTypeProject, ID: testProjectA}}
	if grant != want {
		t.Errorf("grant = %+v, want %+v", grant, want)
	}
	if got := grant.Scope.String(); got != "project:"+testProjectA {
		t.Errorf("scope = %q, want %q", got, "project:"+testProjectA)
	}

	// A's connection asking for B.
	if _, err := f.auth.Authorize(ctx, testProjectA, mustParse(t, subscribeFrameJSON(ResourceTypeProject, testProjectB, nil, nil))); !errors.Is(err, ErrForbidden) {
		t.Errorf("project A connection subscribing project B error = %v, want ErrForbidden", err)
	}
	// B's connection asking for A (the same rule, the other direction).
	if _, err := f.auth.Authorize(ctx, testProjectB, mustParse(t, subscribeFrameJSON(ResourceTypeProject, testProjectA, nil, nil))); !errors.Is(err, ErrForbidden) {
		t.Errorf("project B connection subscribing project A error = %v, want ErrForbidden", err)
	}
	// A connection whose project is empty can authorize nothing.
	if _, err := f.auth.Authorize(ctx, "", mustParse(t, subscribeFrameJSON(ResourceTypeProject, testProjectA, nil, nil))); !errors.Is(err, ErrForbidden) {
		t.Errorf("empty connection project subscribing project A error = %v, want ErrForbidden", err)
	}
}

// TestAuthorizeRunScopeRequiresTheRunsProject covers the run half of the rule: the
// run must exist AND belong to the connection's project.
func TestAuthorizeRunScopeRequiresTheRunsProject(t *testing.T) {
	f := newReplayFixture(t)
	ctx := context.Background()

	grant, err := f.auth.Authorize(ctx, testProjectA, mustParse(t, subscribeFrameJSON(ResourceTypeRun, testRunA, nil, stringPtr("sub_1"))))
	if err != nil {
		t.Fatalf("authorize run A from a project A connection: %v", err)
	}
	want := Grant{ProjectID: testProjectA, Scope: Scope{Kind: ResourceTypeRun, ID: testRunA}}
	if grant != want {
		t.Errorf("grant = %+v, want %+v", grant, want)
	}
	if got := grant.Scope.String(); got != "run:"+testRunA {
		t.Errorf("scope = %q, want %q", got, "run:"+testRunA)
	}

	// A's connection asking for B's run.
	if _, err := f.auth.Authorize(ctx, testProjectA, mustParse(t, subscribeFrameJSON(ResourceTypeRun, testRunB, nil, nil))); !errors.Is(err, ErrForbidden) {
		t.Errorf("project A connection subscribing run B error = %v, want ErrForbidden", err)
	}
	// A run that does not exist at all.
	if _, err := f.auth.Authorize(ctx, testProjectA, mustParse(t, subscribeFrameJSON(ResourceTypeRun, testRunNone, nil, nil))); !errors.Is(err, ErrForbidden) {
		t.Errorf("project A connection subscribing an unknown run error = %v, want ErrForbidden", err)
	}
	// A resource type outside the enum never gets a grant either.
	if _, err := f.auth.Authorize(ctx, testProjectA, SubscribeRequest{ResourceType: "flow", ResourceID: "f_1"}); !errors.Is(err, ErrForbidden) {
		t.Errorf("unknown resource type error = %v, want ErrForbidden", err)
	}
	if _, err := f.auth.Authorize(ctx, testProjectA, SubscribeRequest{ResourceType: ResourceTypeRun}); !errors.Is(err, ErrForbidden) {
		t.Errorf("run subscription without a resource id error = %v, want ErrForbidden", err)
	}
}

// TestAuthorizeForbiddenIsIdenticalForAnotherProjectsRunAndAMissingRun is the
// §20.3 non-disclosure rule, asserted the only way it can be: the two refusals are
// the same error value with byte-identical text, so the client cannot use the
// answer to learn whether another project's run exists.
func TestAuthorizeForbiddenIsIdenticalForAnotherProjectsRunAndAMissingRun(t *testing.T) {
	f := newReplayFixture(t)
	ctx := context.Background()

	_, errOtherProject := f.auth.Authorize(ctx, testProjectA, mustParse(t, subscribeFrameJSON(ResourceTypeRun, testRunB, nil, stringPtr("sub_1"))))
	_, errMissing := f.auth.Authorize(ctx, testProjectA, mustParse(t, subscribeFrameJSON(ResourceTypeRun, testRunNone, nil, stringPtr("sub_1"))))
	if !errors.Is(errOtherProject, ErrForbidden) || !errors.Is(errMissing, ErrForbidden) {
		t.Fatalf("errors = (%v, %v), want both ErrForbidden", errOtherProject, errMissing)
	}
	if errOtherProject.Error() != errMissing.Error() {
		t.Errorf("error texts differ:\n  another project's run: %q\n  missing run:          %q", errOtherProject.Error(), errMissing.Error())
	}
	if errOtherProject.Error() != ErrForbidden.Error() {
		t.Errorf("forbidden error = %q, want the bare sentinel text %q (no detail that could leak)", errOtherProject.Error(), ErrForbidden.Error())
	}
	// And the frames the two failures produce are the same bytes, because the
	// frame is a function of the client_request_id alone.
	frameOtherProject := ForbiddenFrame("sub_1")
	frameMissing := ForbiddenFrame("sub_1")
	if !bytes.Equal(frameOtherProject, frameMissing) {
		t.Errorf("forbidden frames differ: %s vs %s", frameOtherProject, frameMissing)
	}
	for _, id := range []string{testRunB, testRunNone, testProjectA} {
		if bytes.Contains(frameOtherProject, []byte(id)) {
			t.Errorf("forbidden frame leaks %s: %s", id, frameOtherProject)
		}
	}
}

// TestAuthorizePropagatesLookupFailures proves a database failure is not an
// authorization decision: a transient error must not become "forbidden", or the
// client would be told its own run does not exist.
func TestAuthorizePropagatesLookupFailures(t *testing.T) {
	f := newReplayFixture(t)
	ctx := context.Background()

	// A real database failure: the handle is closed, so GetRun fails on the
	// query rather than by finding no row.
	closed := newReplayStore(t)
	lookup := RunstoreRunLookup(closed.DB())
	if err := closed.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	auth := Authorizer{Runs: lookup}
	_, err := auth.Authorize(ctx, testProjectA, mustParse(t, subscribeFrameJSON(ResourceTypeRun, testRunA, nil, nil)))
	if err == nil {
		t.Fatal("authorize with a closed database returned no error")
	}
	if errors.Is(err, ErrForbidden) {
		t.Errorf("authorize with a closed database returned ErrForbidden (%v); a lookup failure must be distinguishable", err)
	}

	// A lookup that fails must not be mistaken for "no such run" either: the same
	// lookup on a live store answers found=false with no error, and that IS
	// forbidden.
	projectID, found, err := f.auth.Runs(ctx, testRunNone)
	if err != nil || found || projectID != "" {
		t.Errorf("lookup of an unknown run = (%q, %v, %v), want (\"\", false, nil)", projectID, found, err)
	}
	projectID, found, err = f.auth.Runs(ctx, testRunB)
	if err != nil || !found || projectID != testProjectB {
		t.Errorf("lookup of run B = (%q, %v, %v), want (%q, true, nil)", projectID, found, err, testProjectB)
	}

	// An authorizer with no lookup at all is a wiring bug: it fails closed, but
	// not as forbidden.
	if _, err := (Authorizer{}).Authorize(ctx, testProjectA, mustParse(t, subscribeFrameJSON(ResourceTypeRun, testRunA, nil, nil))); err == nil || errors.Is(err, ErrForbidden) {
		t.Errorf("authorize without a run lookup error = %v, want a non-forbidden wiring error", err)
	}
	// A project subscription needs no lookup, so the same empty authorizer still
	// grants it.
	if _, err := (Authorizer{}).Authorize(ctx, testProjectA, mustParse(t, subscribeFrameJSON(ResourceTypeProject, testProjectA, nil, nil))); err != nil {
		t.Errorf("authorize project without a run lookup: %v", err)
	}
}

// TestScopeString pins the wire spelling of a scope and its refusal to render
// anything that is not project/run.
func TestScopeString(t *testing.T) {
	cases := []struct {
		scope Scope
		want  string
	}{
		{Scope{Kind: ResourceTypeProject, ID: "p_123"}, "project:p_123"},
		{Scope{Kind: ResourceTypeRun, ID: "run_88"}, "run:run_88"},
		{Scope{Kind: ResourceTypeRun}, ""},
		{Scope{ID: "run_88"}, ""},
		{Scope{Kind: "flow", ID: "f_1"}, ""},
		{Scope{}, ""},
	}
	for _, tc := range cases {
		if got := tc.scope.String(); got != tc.want {
			t.Errorf("Scope%+v.String() = %q, want %q", tc.scope, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Wire mapping
// ---------------------------------------------------------------------------

// TestToWireEventProjectsSequenceBySubscriptionScope is §27.4's projection rule:
// one row, two subscriptions, two counters, never mixed. A project event is
// appended first so the run event's project_seq and run_seq differ, which is what
// makes the assertion able to fail if the wrong column were read.
func TestToWireEventProjectsSequenceBySubscriptionScope(t *testing.T) {
	f := newReplayFixture(t)

	f.projectEvent(testProjectA)                 // project_seq 1
	stored := f.runEvent(testProjectA, testRunA) // project_seq 2, run_seq 1
	if stored.ProjectSeq != 2 || stored.RunSeq == nil || *stored.RunSeq != 1 {
		t.Fatalf("stored event sequences = project %d / run %v, want 2 / 1", stored.ProjectSeq, stored.RunSeq)
	}

	projectGrant := Grant{ProjectID: testProjectA, Scope: Scope{Kind: ResourceTypeProject, ID: testProjectA}}
	runGrant := Grant{ProjectID: testProjectA, Scope: Scope{Kind: ResourceTypeRun, ID: testRunA}}

	underProject, err := ToWireEvent(projectGrant, stored)
	if err != nil {
		t.Fatalf("ToWireEvent(project grant): %v", err)
	}
	if underProject.Sequence != stored.ProjectSeq {
		t.Errorf("project subscription sequence = %d, want project_seq %d", underProject.Sequence, stored.ProjectSeq)
	}
	if underProject.Scope != "project:"+testProjectA {
		t.Errorf("project subscription scope = %q, want %q", underProject.Scope, "project:"+testProjectA)
	}
	if underProject.RunID == nil || *underProject.RunID != testRunA {
		t.Errorf("project subscription run_id = %v, want %q (the event still names its run)", underProject.RunID, testRunA)
	}
	if underProject.ID != stored.ID || underProject.ProjectID != testProjectA || underProject.Type != stored.Type {
		t.Errorf("project subscription event = %+v, want id/project/type of the stored event", underProject)
	}
	if underProject.SchemaVersion != runstore.EventSchemaVersion {
		t.Errorf("schema_version = %d, want %d", underProject.SchemaVersion, runstore.EventSchemaVersion)
	}
	if !bytes.Equal(underProject.Identity, stored.Identity) || !bytes.Equal(underProject.Payload, stored.Payload) {
		t.Errorf("identity/payload = %s / %s, want the stored bytes %s / %s",
			underProject.Identity, underProject.Payload, stored.Identity, stored.Payload)
	}

	underRun, err := ToWireEvent(runGrant, stored)
	if err != nil {
		t.Fatalf("ToWireEvent(run grant): %v", err)
	}
	if underRun.Sequence != *stored.RunSeq {
		t.Errorf("run subscription sequence = %d, want run_seq %d", underRun.Sequence, *stored.RunSeq)
	}
	if underRun.Scope != "run:"+testRunA {
		t.Errorf("run subscription scope = %q, want %q", underRun.Scope, "run:"+testRunA)
	}

	// The two frames must actually differ on the wire, or the projection is only
	// happening in the struct.
	projectJSON := EventFrame(underProject)
	runJSON := EventFrame(underRun)
	if !bytes.Contains(projectJSON, []byte(`"sequence":2`)) || !bytes.Contains(projectJSON, []byte(`"scope":"project:`+testProjectA+`"`)) {
		t.Errorf("project frame = %s, want sequence 2 and the project scope", projectJSON)
	}
	if !bytes.Contains(runJSON, []byte(`"sequence":1`)) || !bytes.Contains(runJSON, []byte(`"scope":"run:`+testRunA+`"`)) {
		t.Errorf("run frame = %s, want sequence 1 and the run scope", runJSON)
	}
}

// TestToWireEventRejectsEventsOutsideTheGrant covers the mappings that must fail
// rather than be sent: a project-level event under a run subscription, an event of
// another run, an event of another project, and a run event with no run counter.
func TestToWireEventRejectsEventsOutsideTheGrant(t *testing.T) {
	f := newReplayFixture(t)

	projectLevel := f.legacyEvent(testProjectA) // no run: the §27.1 case
	runAEvent := f.runEvent(testProjectA, testRunA)
	projectAEvent := f.projectEvent(testProjectA)

	runAGrant := Grant{ProjectID: testProjectA, Scope: Scope{Kind: ResourceTypeRun, ID: testRunA}}
	runBGrant := Grant{ProjectID: testProjectB, Scope: Scope{Kind: ResourceTypeRun, ID: testRunB}}
	projectBGrant := Grant{ProjectID: testProjectB, Scope: Scope{Kind: ResourceTypeProject, ID: testProjectB}}

	noRunSeq := runAEvent
	noRunSeq.RunSeq = nil

	cases := []struct {
		name  string
		grant Grant
		event runstore.Event
	}{
		{"project-level event under a run grant", runAGrant, projectLevel},
		{"another run's event under a run grant", runBGrant, runAEvent},
		{"another project's event under a project grant", projectBGrant, projectAEvent},
		{"another project's run event under a run grant", runBGrant, runAEvent},
		{"run event without a run sequence", runAGrant, noRunSeq},
		{"grant with no project", Grant{Scope: Scope{Kind: ResourceTypeProject, ID: testProjectA}}, projectAEvent},
		{"grant with no scope", Grant{ProjectID: testProjectA}, projectAEvent},
		{"grant with an unknown kind", Grant{ProjectID: testProjectA, Scope: Scope{Kind: "flow", ID: testProjectA}}, projectAEvent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ToWireEvent(tc.grant, tc.event); err == nil {
				t.Errorf("ToWireEvent(%+v, event %s) succeeded, want an error", tc.grant, tc.event.ID)
			}
		})
	}
}

// TestWireEventJSONKeysMatchExecutionEventSchema pins the wire contract against
// the frozen schema in both directions: every property is a field of WireEvent,
// every field is a property, and the schema's required list is a subset.
func TestWireEventJSONKeysMatchExecutionEventSchema(t *testing.T) {
	var schema struct {
		Required             []string                   `json:"required"`
		AdditionalProperties *bool                      `json:"additionalProperties"`
		Properties           map[string]json.RawMessage `json:"properties"`
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "schemas", "execution-event.schema.json"))
	if err != nil {
		t.Fatalf("read execution-event.schema.json: %v", err)
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("parse execution-event.schema.json: %v", err)
	}
	if len(schema.Properties) == 0 {
		t.Fatal("execution-event.schema.json has no properties")
	}
	if schema.AdditionalProperties == nil || *schema.AdditionalProperties {
		t.Fatal("execution-event.schema.json must forbid additional properties for the key-set claim to hold")
	}

	tags := map[string]bool{}
	typ := reflect.TypeOf(WireEvent{})
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		tag := field.Tag.Get("json")
		if tag == "" || tag == "-" {
			t.Errorf("WireEvent.%s has no json tag", field.Name)
			continue
		}
		name := strings.Split(tag, ",")[0]
		if tags[name] {
			t.Errorf("WireEvent has two fields tagged %q", name)
		}
		tags[name] = true
	}

	for name := range schema.Properties {
		if !tags[name] {
			t.Errorf("schema property %q has no WireEvent field", name)
		}
	}
	for name := range tags {
		if _, ok := schema.Properties[name]; !ok {
			t.Errorf("WireEvent field %q is not a schema property", name)
		}
	}
	for _, name := range schema.Required {
		if !tags[name] {
			t.Errorf("schema requires %q but WireEvent has no such field", name)
		}
	}
	if len(tags) != len(schema.Properties) {
		t.Errorf("WireEvent has %d keys, schema has %d properties", len(tags), len(schema.Properties))
	}
}

// TestWireEventOccurredAtIsRFC3339UTCWithMilliseconds pins the one decision
// T1.12.a had to make about occurred_at (§20.2's example prints whole seconds):
// the wire carries RFC3339 UTC with a fixed millisecond field, so the stored
// instant survives the round trip instead of being rounded to the second.
func TestWireEventOccurredAtIsRFC3339UTCWithMilliseconds(t *testing.T) {
	f := newReplayFixture(t)

	instant := time.Date(2026, 9, 6, 8, 0, 0, 123000000, time.UTC)
	stored := f.projectEventAt(testProjectA, instant.UnixMilli())
	grant := Grant{ProjectID: testProjectA, Scope: Scope{Kind: ResourceTypeProject, ID: testProjectA}}
	wire, err := ToWireEvent(grant, stored)
	if err != nil {
		t.Fatalf("ToWireEvent: %v", err)
	}
	const want = "2026-09-06T08:00:00.123Z"
	if wire.OccurredAt != want {
		t.Errorf("occurred_at = %q, want %q", wire.OccurredAt, want)
	}
	parsed, err := time.Parse(time.RFC3339Nano, wire.OccurredAt)
	if err != nil {
		t.Fatalf("occurred_at %q is not RFC3339: %v", wire.OccurredAt, err)
	}
	if !parsed.Equal(instant) {
		t.Errorf("occurred_at round trip = %s, want %s", parsed.UTC(), instant)
	}
	if !strings.HasSuffix(wire.OccurredAt, "Z") {
		t.Errorf("occurred_at %q is not UTC (it must end in Z)", wire.OccurredAt)
	}

	// A whole second keeps the same shape: one contract, not two.
	whole := time.Date(2026, 9, 6, 8, 0, 0, 0, time.UTC)
	wireWhole, err := ToWireEvent(grant, f.projectEventAt(testProjectA, whole.UnixMilli()))
	if err != nil {
		t.Fatalf("ToWireEvent(whole second): %v", err)
	}
	if wireWhole.OccurredAt != "2026-09-06T08:00:00.000Z" {
		t.Errorf("whole-second occurred_at = %q, want %q", wireWhole.OccurredAt, "2026-09-06T08:00:00.000Z")
	}
	if got := len(wireWhole.OccurredAt); got != len(want) {
		t.Errorf("occurred_at lengths differ: %d vs %d", got, len(want))
	}

	// The frame carries it unchanged.
	if frame := EventFrame(wire); !bytes.Contains(frame, []byte(`"occurred_at":"`+want+`"`)) {
		t.Errorf("event frame = %s, want occurred_at %q", frame, want)
	}
}

// TestWireEventRunIDIsNullForProjectLevelEvents pins the stable key set: a
// project-level event (no Run) travels with `"run_id":null`, which the schema's
// nullable type allows, rather than dropping the key.
func TestWireEventRunIDIsNullForProjectLevelEvents(t *testing.T) {
	f := newReplayFixture(t)
	stored := f.legacyEvent(testProjectA)
	if stored.RunID != nil {
		t.Fatalf("legacy event has run_id %v, want none", *stored.RunID)
	}
	grant := Grant{ProjectID: testProjectA, Scope: Scope{Kind: ResourceTypeProject, ID: testProjectA}}
	wire, err := ToWireEvent(grant, stored)
	if err != nil {
		t.Fatalf("ToWireEvent: %v", err)
	}
	if wire.RunID != nil {
		t.Errorf("run_id = %v, want nil", wire.RunID)
	}
	if frame := EventFrame(wire); !bytes.Contains(frame, []byte(`"run_id":null`)) {
		t.Errorf("event frame = %s, want an explicit null run_id", frame)
	}
}

// TestToWireEventRejectsBrokenRows covers the defensive checks: rows the store
// cannot produce today, each of which would be a protocol violation on the wire.
func TestToWireEventRejectsBrokenRows(t *testing.T) {
	f := newReplayFixture(t)
	stored := f.projectEvent(testProjectA)
	grant := Grant{ProjectID: testProjectA, Scope: Scope{Kind: ResourceTypeProject, ID: testProjectA}}

	broken := []struct {
		name   string
		mutate func(runstore.Event) runstore.Event
	}{
		{"unknown event type", func(e runstore.Event) runstore.Event { e.Type = "run.started"; return e }},
		{"empty event type", func(e runstore.Event) runstore.Event { e.Type = ""; return e }},
		{"other schema version", func(e runstore.Event) runstore.Event { e.SchemaVersion = 2; return e }},
		{"no id", func(e runstore.Event) runstore.Event { e.ID = ""; return e }},
		{"zero occurred_at", func(e runstore.Event) runstore.Event { e.OccurredAt = time.Time{}; return e }},
		{"identity is not JSON", func(e runstore.Event) runstore.Event { e.Identity = json.RawMessage(`{`); return e }},
		{"empty identity", func(e runstore.Event) runstore.Event { e.Identity = nil; return e }},
		{"payload is not JSON", func(e runstore.Event) runstore.Event { e.Payload = json.RawMessage(`not json`); return e }},
		{"empty payload", func(e runstore.Event) runstore.Event { e.Payload = nil; return e }},
		{"sequence below the schema minimum", func(e runstore.Event) runstore.Event { e.ProjectSeq = 0; return e }},
	}
	for _, tc := range broken {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ToWireEvent(grant, tc.mutate(stored)); err == nil {
				t.Error("ToWireEvent accepted a broken row, want an error")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Replay
// ---------------------------------------------------------------------------

// TestReadReplayPagesWithSnapshotBoundaries walks a project timeline in pages of
// two and checks the cursor chain and the boundaries: NextAfter is the last
// delivered sequence, HasMore is true until the stream ends, and the watermark and
// floor come from the same read as the page.
func TestReadReplayPagesWithSnapshotBoundaries(t *testing.T) {
	f := newReplayFixture(t)
	ctx := context.Background()

	appended := []runstore.Event{
		f.projectEvent(testProjectA),
		f.runEvent(testProjectA, testRunA),
		f.projectEvent(testProjectA),
		f.runEvent(testProjectA, testRunA),
		f.projectEvent(testProjectA),
	}
	// B's events exist but belong to another scope: they must never appear in A's
	// page, and they must not move A's counters.
	f.projectEvent(testProjectB)

	grant := Grant{ProjectID: testProjectA, Scope: Scope{Kind: ResourceTypeProject, ID: testProjectA}}
	var (
		after int64
		seen  []WireEvent
		pages int
	)
	for {
		batch, err := ReadReplay(ctx, f.reader, grant, after, 2)
		if err != nil {
			t.Fatalf("ReadReplay(after=%d): %v", after, err)
		}
		pages++
		if batch.HighWatermark != 5 {
			t.Errorf("page %d high_watermark = %d, want 5 (B's event does not count)", pages, batch.HighWatermark)
		}
		if batch.RetentionFloor != 1 {
			t.Errorf("page %d retention_floor = %d, want 1 (nothing was trimmed)", pages, batch.RetentionFloor)
		}
		if len(batch.Events) > 2 {
			t.Errorf("page %d returned %d events, want at most the limit 2", pages, len(batch.Events))
		}
		for _, ev := range batch.Events {
			if ev.Scope != "project:"+testProjectA {
				t.Errorf("page %d event scope = %q, want the project scope", pages, ev.Scope)
			}
			if ev.ProjectID != testProjectA {
				t.Errorf("page %d event project = %q, want %q", pages, ev.ProjectID, testProjectA)
			}
			if ev.Sequence < 1 {
				t.Errorf("page %d event sequence = %d, want >= 1", pages, ev.Sequence)
			}
			seen = append(seen, ev)
		}
		if batch.HasMore {
			if len(batch.Events) != 2 {
				t.Errorf("page %d has_more with %d events, want a full page", pages, len(batch.Events))
			}
			if batch.NextAfter != batch.Events[len(batch.Events)-1].Sequence {
				t.Errorf("page %d next_after = %d, want the last sequence %d", pages, batch.NextAfter, batch.Events[len(batch.Events)-1].Sequence)
			}
			after = batch.NextAfter
			continue
		}
		if batch.NextAfter != appended[4].ProjectSeq {
			t.Errorf("last page next_after = %d, want %d", batch.NextAfter, appended[4].ProjectSeq)
		}
		break
	}
	if pages != 3 {
		t.Errorf("pages = %d, want 3", pages)
	}
	if len(seen) != 5 {
		t.Fatalf("delivered %d events, want 5", len(seen))
	}
	for i, ev := range seen {
		if ev.Sequence != appended[i].ProjectSeq {
			t.Errorf("delivered[%d].sequence = %d, want project_seq %d", i, ev.Sequence, appended[i].ProjectSeq)
		}
		if ev.ID != appended[i].ID {
			t.Errorf("delivered[%d].id = %q, want %q", i, ev.ID, appended[i].ID)
		}
		if i > 0 && ev.Sequence != seen[i-1].Sequence+1 {
			t.Errorf("delivered sequences are not consecutive: %d after %d", ev.Sequence, seen[i-1].Sequence)
		}
	}
}

// TestReadReplayRunScopeUsesRunSeqAndRunBounds is the other half of the projection
// rule: a run subscription pages on run_seq, and its watermark is the run's own
// counter — the project's extra events do not appear in it.
func TestReadReplayRunScopeUsesRunSeqAndRunBounds(t *testing.T) {
	f := newReplayFixture(t)
	ctx := context.Background()

	f.projectEvent(testProjectA)
	first := f.runEvent(testProjectA, testRunA)
	second := f.runEvent(testProjectA, testRunA)
	f.projectEvent(testProjectA)
	f.runEvent(testProjectB, testRunB) // another project's run: invisible here

	grant := Grant{ProjectID: testProjectA, Scope: Scope{Kind: ResourceTypeRun, ID: testRunA}}
	batch, err := ReadReplay(ctx, f.reader, grant, 0, 10)
	if err != nil {
		t.Fatalf("ReadReplay(run scope): %v", err)
	}
	if batch.HighWatermark != 2 {
		t.Errorf("run high_watermark = %d, want 2 (the run's own counter)", batch.HighWatermark)
	}
	if batch.RetentionFloor != 1 {
		t.Errorf("run retention_floor = %d, want 1", batch.RetentionFloor)
	}
	if batch.HasMore || batch.NextAfter != 2 {
		t.Errorf("run batch has_more/next_after = %v/%d, want false/2", batch.HasMore, batch.NextAfter)
	}
	if len(batch.Events) != 2 {
		t.Fatalf("run page has %d events, want 2", len(batch.Events))
	}
	want := []runstore.Event{first, second}
	for i, ev := range batch.Events {
		if ev.Sequence != *want[i].RunSeq {
			t.Errorf("run event[%d].sequence = %d, want run_seq %d", i, ev.Sequence, *want[i].RunSeq)
		}
		if ev.Scope != "run:"+testRunA {
			t.Errorf("run event[%d].scope = %q, want the run scope", i, ev.Scope)
		}
		if ev.RunID == nil || *ev.RunID != testRunA {
			t.Errorf("run event[%d].run_id = %v, want %q", i, ev.RunID, testRunA)
		}
	}

	// Paging a run cursor past the run's watermark is invalid even though the
	// project has more events: the scope decides.
	if _, err := ReadReplay(ctx, f.reader, grant, 3, 10); !errors.Is(err, runstore.ErrInvalidCursor) {
		t.Errorf("ReadReplay(run, after=3) error = %v, want runstore.ErrInvalidCursor", err)
	}
}

// TestReadReplayEmptyScopeIsTheLegalFirstReplay pins §20.2's first replay on a
// scope that has no events at all: after=0 answers an empty page with hwm 0 and
// floor 1 rather than an error.
func TestReadReplayEmptyScopeIsTheLegalFirstReplay(t *testing.T) {
	f := newReplayFixture(t)
	grant := Grant{ProjectID: testProjectB, Scope: Scope{Kind: ResourceTypeProject, ID: testProjectB}}
	batch, err := ReadReplay(context.Background(), f.reader, grant, 0, 0)
	if err != nil {
		t.Fatalf("ReadReplay(empty scope): %v", err)
	}
	if len(batch.Events) != 0 || batch.HasMore || batch.NextAfter != 0 {
		t.Errorf("empty scope batch = %+v, want an empty page with next_after 0", batch)
	}
	if batch.HighWatermark != 0 || batch.RetentionFloor != 1 {
		t.Errorf("empty scope boundaries = hwm %d floor %d, want 0/1", batch.HighWatermark, batch.RetentionFloor)
	}
}

// TestReadReplayPropagatesCursorErrors keeps the two cursor failures identifiable:
// ErrInvalidCursor is the 422 case (after past the watermark) and ErrCursorExpired
// the 410 case (after below the retention floor), and neither may be reported as
// the other.
func TestReadReplayPropagatesCursorErrors(t *testing.T) {
	f := newReplayFixture(t)
	ctx := context.Background()
	grant := Grant{ProjectID: testProjectA, Scope: Scope{Kind: ResourceTypeProject, ID: testProjectA}}

	f.projectEvent(testProjectA)
	f.projectEvent(testProjectA)

	// after = watermark is legal (the client is caught up).
	if _, err := ReadReplay(ctx, f.reader, grant, 2, 10); err != nil {
		t.Errorf("ReadReplay(after=watermark): %v", err)
	}
	for _, after := range []int64{3, 1000} {
		invalid, err := ReadReplay(ctx, f.reader, grant, after, 10)
		if !errors.Is(err, runstore.ErrInvalidCursor) {
			t.Errorf("ReadReplay(after=%d) error = %v, want runstore.ErrInvalidCursor", after, err)
		}
		// The frame quotes the watermark that invalidated the cursor.
		if invalid.HighWatermark != 2 || len(invalid.Events) != 0 {
			t.Errorf("ReadReplay(after=%d) batch = %+v, want watermark 2 and no events", after, invalid)
		}
		if errors.Is(err, runstore.ErrCursorExpired) {
			t.Errorf("ReadReplay(after=%d) error = %v, want it not to be ErrCursorExpired", after, err)
		}
	}

	// The 410 side needs history that retention trimmed, which is modelled by
	// presetting the scope counter: the next event gets 5, so the retained history
	// starts there and the floor is 5. Project B has no events yet, so its floor is
	// exactly the preset.
	presetScopeCounter(t, f.store, projectScopeName(testProjectB), 4)
	f.projectEvent(testProjectB) // project_seq 5
	bGrant := Grant{ProjectID: testProjectB, Scope: Scope{Kind: ResourceTypeProject, ID: testProjectB}}

	// after = floor-1 = 4 is the last legal position before the retained history.
	atFloor, err := ReadReplay(ctx, f.reader, bGrant, 4, 10)
	if err != nil {
		t.Fatalf("ReadReplay(after=floor-1): %v", err)
	}
	if atFloor.RetentionFloor != 5 || len(atFloor.Events) != 1 || atFloor.Events[0].Sequence != 5 {
		t.Errorf("batch at floor-1 = %+v, want floor 5 and the event at sequence 5", atFloor)
	}
	for _, after := range []int64{0, 3} {
		expired, err := ReadReplay(ctx, f.reader, bGrant, after, 10)
		if !errors.Is(err, runstore.ErrCursorExpired) {
			t.Errorf("ReadReplay(after=%d) error = %v, want runstore.ErrCursorExpired", after, err)
		}
		// The frame quotes the floor that expired the cursor, from the same read.
		if expired.RetentionFloor != 5 || expired.HighWatermark != 5 || len(expired.Events) != 0 {
			t.Errorf("ReadReplay(after=%d) batch = %+v, want floor 5, watermark 5 and no events", after, expired)
		}
		if errors.Is(err, runstore.ErrInvalidCursor) {
			t.Errorf("ReadReplay(after=%d) error = %v, want it not to be ErrInvalidCursor", after, err)
		}
	}

	// A grant that cannot name a scope is refused before any read happens.
	if _, err := ReadReplay(ctx, f.reader, Grant{ProjectID: testProjectA}, 0, 10); err == nil {
		t.Error("ReadReplay with a scopeless grant succeeded, want an error")
	}
	if _, err := ReadReplay(ctx, nil, grant, 0, 10); err == nil {
		t.Error("ReadReplay without a reader succeeded, want an error")
	}
}

// ---------------------------------------------------------------------------
// Frames
// ---------------------------------------------------------------------------

// frameShape is one decoded server frame: its type and its data key set.
type frameShape struct {
	Type MessageType                `json:"type"`
	Data map[string]json.RawMessage `json:"data"`
}

// decodeFrame decodes a frame and fails the test if it is not one JSON object
// with exactly the keys type and data (§20.3's frame shape).
func decodeFrame(t *testing.T, raw []byte) frameShape {
	t.Helper()
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatalf("frame is not JSON: %v (%s)", err, raw)
	}
	if len(top) != 2 {
		t.Fatalf("frame has %d top-level keys (%s), want exactly type and data", len(top), raw)
	}
	for _, key := range []string{"type", "data"} {
		if _, ok := top[key]; !ok {
			t.Fatalf("frame is missing the %q key: %s", key, raw)
		}
	}
	var shape frameShape
	if err := json.Unmarshal(raw, &shape); err != nil {
		t.Fatalf("frame could not be decoded: %v (%s)", err, raw)
	}
	if shape.Data == nil {
		t.Fatalf("frame data is not an object: %s", raw)
	}
	return shape
}

// dataKeys returns the sorted key set of a decoded data object.
func dataKeys(data map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// TestFrameShapes pins the JSON shape of every frame this file can build: the type
// value and the exact key set of data. The key sets are deliberately frozen here,
// because T1.12.b and T1.14 program against them.
func TestFrameShapes(t *testing.T) {
	runGrant := Grant{ProjectID: testProjectA, Scope: Scope{Kind: ResourceTypeRun, ID: testRunA}}
	wire := WireEvent{
		ID:            "evt_1",
		Scope:         "run:" + testRunA,
		ProjectID:     testProjectA,
		RunID:         stringPtr(testRunA),
		Sequence:      42,
		Type:          "tool.requested",
		SchemaVersion: 1,
		OccurredAt:    "2026-09-06T08:00:00.000Z",
		Identity:      json.RawMessage(`{"project_id":"` + testProjectA + `"}`),
		Payload:       json.RawMessage(`{"tool":"read_file"}`),
	}

	cases := []struct {
		name     string
		frame    []byte
		wantType MessageType
		wantKeys []string
	}{
		{
			name:     "subscribed",
			frame:    SubscribedFrame("sub_1", runGrant),
			wantType: MsgTypeSubscribed,
			wantKeys: []string{"client_request_id", "project_id", "resource_id", "resource_type", "scope"},
		},
		{
			name:     "replay_started",
			frame:    ReplayStartedFrame("sub_1", "run:"+testRunA, 41, 43, 1),
			wantType: MsgTypeReplayStarted,
			wantKeys: []string{"after", "client_request_id", "high_watermark", "retention_floor", "scope"},
		},
		{
			name:     "event",
			frame:    EventFrame(wire),
			wantType: MsgTypeEvent,
			wantKeys: []string{"id", "identity", "occurred_at", "payload", "project_id", "run_id", "schema_version", "scope", "sequence", "type"},
		},
		{
			name:     "replay_finished",
			frame:    ReplayFinishedFrame("sub_1", "run:"+testRunA, 43, 43),
			wantType: MsgTypeReplayFinished,
			wantKeys: []string{"client_request_id", "high_watermark", "next_after", "scope"},
		},
		{
			name:     "forbidden",
			frame:    ForbiddenFrame("sub_1"),
			wantType: MsgTypeForbidden,
			wantKeys: []string{"client_request_id", "code", "message"},
		},
		{
			name:     "cursor_expired",
			frame:    CursorExpiredFrame("sub_1", "project:"+testProjectA, 5),
			wantType: MsgTypeCursorExpired,
			wantKeys: []string{"client_request_id", "code", "message", "recovery", "retention_floor", "scope"},
		},
		{
			name:     "invalid_cursor",
			frame:    InvalidCursorFrame("sub_1", "project:"+testProjectA, 7),
			wantType: MsgTypeInvalidCursor,
			wantKeys: []string{"client_request_id", "code", "high_watermark", "message", "scope"},
		},
		{
			name:     "invalid_request",
			frame:    InvalidRequestFrame("sub_1", ReasonInvalidAfter),
			wantType: MsgTypeInvalidRequest,
			wantKeys: []string{"client_request_id", "code", "reason"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			shape := decodeFrame(t, tc.frame)
			if shape.Type != tc.wantType {
				t.Errorf("type = %q, want %q", shape.Type, tc.wantType)
			}
			if got := dataKeys(shape.Data); !reflect.DeepEqual(got, tc.wantKeys) {
				t.Errorf("data keys = %v, want %v", got, tc.wantKeys)
			}
		})
	}

	// The values, not just the keys, of the frames a client correlates by.
	t.Run("subscribed values", func(t *testing.T) {
		shape := decodeFrame(t, SubscribedFrame("sub_1", runGrant))
		assertJSONString(t, shape.Data, "client_request_id", "sub_1")
		assertJSONString(t, shape.Data, "scope", "run:"+testRunA)
		assertJSONString(t, shape.Data, "resource_type", ResourceTypeRun)
		assertJSONString(t, shape.Data, "resource_id", testRunA)
		assertJSONString(t, shape.Data, "project_id", testProjectA)
	})
	t.Run("replay_started values", func(t *testing.T) {
		shape := decodeFrame(t, ReplayStartedFrame("sub_1", "run:"+testRunA, 41, 43, 1))
		assertJSONNumber(t, shape.Data, "after", 41)
		assertJSONNumber(t, shape.Data, "high_watermark", 43)
		assertJSONNumber(t, shape.Data, "retention_floor", 1)
	})
	t.Run("replay_finished values", func(t *testing.T) {
		shape := decodeFrame(t, ReplayFinishedFrame("sub_1", "run:"+testRunA, 43, 42))
		assertJSONNumber(t, shape.Data, "high_watermark", 43)
		assertJSONNumber(t, shape.Data, "next_after", 42)
	})
	t.Run("cursor_expired recovery instruction", func(t *testing.T) {
		shape := decodeFrame(t, CursorExpiredFrame("sub_1", "project:"+testProjectA, 5))
		assertJSONString(t, shape.Data, "code", CodeCursorExpired)
		assertJSONString(t, shape.Data, "recovery", RecoverySnapshot)
		assertJSONNumber(t, shape.Data, "retention_floor", 5)
		// The snapshot URL is T1.12.b/T1.14's to define; this frame must not
		// invent one, and must not carry a key that later has to be changed.
		if _, ok := shape.Data["snapshot_url"]; ok {
			t.Error("cursor_expired frame carries a snapshot_url, which this card must not invent")
		}
	})
	t.Run("invalid_cursor value", func(t *testing.T) {
		shape := decodeFrame(t, InvalidCursorFrame("sub_1", "project:"+testProjectA, 7))
		assertJSONString(t, shape.Data, "code", CodeInvalidCursor)
		assertJSONNumber(t, shape.Data, "high_watermark", 7)
	})
	t.Run("forbidden values", func(t *testing.T) {
		shape := decodeFrame(t, ForbiddenFrame("sub_1"))
		assertJSONString(t, shape.Data, "code", CodeForbidden)
		assertJSONString(t, shape.Data, "message", forbiddenMessage)
	})
	t.Run("invalid_request values", func(t *testing.T) {
		shape := decodeFrame(t, InvalidRequestFrame("sub_1", ReasonInvalidResourceType))
		assertJSONString(t, shape.Data, "code", CodeInvalidRequest)
		assertJSONString(t, shape.Data, "reason", ReasonInvalidResourceType)
	})
	t.Run("empty client request id keeps the key", func(t *testing.T) {
		shape := decodeFrame(t, ForbiddenFrame(""))
		if _, ok := shape.Data["client_request_id"]; !ok {
			t.Error("client_request_id is missing from a frame with no correlation id")
		}
	})
}

// TestEventFrameDataIsTheExecutionEvent pins that an event frame's data object is
// the wire event itself — same key set as the schema — rather than a wrapper with
// the event nested inside it.
func TestEventFrameDataIsTheExecutionEvent(t *testing.T) {
	wire := WireEvent{
		ID:            "evt_1",
		Scope:         "project:" + testProjectA,
		ProjectID:     testProjectA,
		Sequence:      3,
		Type:          "server.restart",
		SchemaVersion: 1,
		OccurredAt:    "2026-09-06T08:00:00.000Z",
		Identity:      json.RawMessage(`{"project_id":"` + testProjectA + `"}`),
		Payload:       json.RawMessage(`{}`),
	}
	shape := decodeFrame(t, EventFrame(wire))
	assertJSONNumber(t, shape.Data, "sequence", 3)
	assertJSONString(t, shape.Data, "id", "evt_1")
	if raw, ok := shape.Data["run_id"]; !ok {
		t.Error("run_id is missing from the event frame")
	} else if string(raw) != "null" {
		t.Errorf("run_id = %s, want null for a project-level event", raw)
	}

	// Every schema property is present, so the frame is a complete event and not
	// a projection with holes.
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "schemas", "execution-event.schema.json"))
	if err != nil {
		t.Fatalf("read execution-event.schema.json: %v", err)
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("parse execution-event.schema.json: %v", err)
	}
	for name := range schema.Properties {
		if _, ok := shape.Data[name]; !ok {
			t.Errorf("event frame is missing schema property %q", name)
		}
	}
}

// TestInvalidRequestFrameNeverEchoesTheParseInput is the §20.3 non-disclosure rule
// applied to the parser's answer: a frame built from a rejected input contains the
// fixed reason and the client_request_id, and none of the client's bytes.
func TestInvalidRequestFrameNeverEchoesTheParseInput(t *testing.T) {
	inputs := []string{
		`{"type":"subscribe","data":{"resource_type":"run","resource_id":"SecretMarkerRun","after":"41"}}`,
		`{"type":"subscribe","data":{"resource_type":"run","resource_id":"run_88","leak_me":"SecretMarkerKey"}}`,
		`{"type":"subscribe","data":{"RESOURCE_ID":"SecretMarkerCase","resource_type":"run"}}`,
		`not json at all SecretMarkerGarbage`,
	}
	for _, input := range inputs {
		_, err := ParseSubscribeFrame([]byte(input))
		if err == nil {
			t.Fatalf("ParseSubscribeFrame(%s) succeeded, want a refusal", input)
		}
		frame := InvalidRequestFrame("sub_1", SubscribeErrorReason(err))
		shape := decodeFrame(t, frame)
		assertJSONString(t, shape.Data, "client_request_id", "sub_1")
		assertJSONString(t, shape.Data, "code", CodeInvalidRequest)
		reason := string(shape.Data["reason"])
		if reason == `""` || reason == "" {
			t.Errorf("reason is empty for input %s", input)
		}
		for _, marker := range []string{"SecretMarkerRun", "SecretMarkerKey", "SecretMarkerCase", "SecretMarkerGarbage"} {
			if bytes.Contains(frame, []byte(marker)) {
				t.Errorf("invalid_request frame leaks %q from the input: %s", marker, frame)
			}
		}
	}
}

// TestErrorFramesCarryAFixedMessage pins that the messages are constants: an error
// frame is never a place where an internal error text could reach the client.
func TestErrorFramesCarryAFixedMessage(t *testing.T) {
	forbidden := decodeFrame(t, ForbiddenFrame("sub_1"))
	assertJSONString(t, forbidden.Data, "message", forbiddenMessage)
	expired := decodeFrame(t, CursorExpiredFrame("sub_1", "project:"+testProjectA, 5))
	assertJSONString(t, expired.Data, "message", cursorExpiredMessage)
	invalid := decodeFrame(t, InvalidCursorFrame("sub_1", "project:"+testProjectA, 7))
	assertJSONString(t, invalid.Data, "message", invalidCursorMessage)
}

// assertJSONString fails unless data[key] is the given JSON string.
func assertJSONString(t *testing.T, data map[string]json.RawMessage, key, want string) {
	t.Helper()
	raw, ok := data[key]
	if !ok {
		t.Errorf("data has no %q key", key)
		return
	}
	var got string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Errorf("data[%q] = %s, which is not a string", key, raw)
		return
	}
	if got != want {
		t.Errorf("data[%q] = %q, want %q", key, got, want)
	}
}

// assertJSONNumber fails unless data[key] is the given JSON integer.
func assertJSONNumber(t *testing.T, data map[string]json.RawMessage, key string, want int64) {
	t.Helper()
	raw, ok := data[key]
	if !ok {
		t.Errorf("data has no %q key", key)
		return
	}
	var got int64
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Errorf("data[%q] = %s, which is not an integer", key, raw)
		return
	}
	if got != want {
		t.Errorf("data[%q] = %d, want %d", key, got, want)
	}
}
