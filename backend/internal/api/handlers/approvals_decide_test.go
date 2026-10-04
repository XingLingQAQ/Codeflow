package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/approval"
	"github.com/codeflow/backend/internal/execbackend"
	"github.com/codeflow/backend/internal/policy"
	"github.com/codeflow/backend/internal/run"
	"github.com/codeflow/backend/internal/runstore"
	"github.com/gin-gonic/gin"
)

// These tests cover approvals_decide.go (T2.02.c group 2) over the real stack:
// a migrated file-backed runstore, the real approval service, the real
// RunCommand ledger and the real gin route — no mocks. The properties under
// test are database properties (one event per decision, an expiry that
// survives the 409, a key claimed exactly once), so the fixture builds a
// pending tool approval the way production does: a running Run, a real
// attempt, and approval.RequestToolApprovalTx.
//
// The named test TestApprovalFalseIsDecision lives in t202c_named_test.go; this
// file has the behaviour matrix.

const (
	decideTestProject  = "11111111-1111-4111-8111-111111111111"
	decideTestProject2 = "22222222-2222-4222-8222-222222222222"
	decideTestTask     = "task-1"
	decideTestTask2    = "task-2"
	decideTestRun      = "run-1"
	decideTestRun2     = "run-2"
	decideTestAttempt  = "attempt-1"
	decideTestAttempt2 = "attempt-2"
)

// decideClientKey is the client key most tests send.
const decideClientKey = "decide_test_0001"

// decideFixtureTime is the instant the fixture's rows are timestamped with and
// the clock the handler is pinned to.
var decideFixtureTime = time.UnixMilli(1700000000000).UTC()

// decideTestFingerprint is the fingerprint every decision must present for the
// fixture's approval. It is filled in by the fixture (the fingerprint is
// computed by the approval package, never chosen by a caller).
const decideTestToolCall = "tc-1"

// approvalDecideFixture is the router wired like production plus the store, so
// a test can read rows, events and command records directly.
type approvalDecideFixture struct {
	t       *testing.T
	store   *runstore.Store
	router  *gin.Engine
	project string
	// approvalID, fingerprint and revision describe the pending approval the
	// fixture created.
	approvalID  string
	fingerprint string
	revision    int64
	// foreignApprovalID is a pending approval of the other project.
	foreignApprovalID  string
	foreignFingerprint string
}

func newApprovalDecideFixture(t *testing.T) *approvalDecideFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)

	// The fixture's rows live at decideFixtureTime; the handler stamps the
	// decision with the clock nowUTC hands it. nowUTC is the variable
	// commands.go exposes so a test can pin time (production never replaces
	// it), and pinning it keeps the deadline semantics of the fixture exact:
	// the approval expires at decideFixtureTime+1h, and the decision is taken
	// at decideFixtureTime, so it is live unless a test moves the deadline.
	restoreClock := nowUTC
	nowUTC = func() time.Time { return decideFixtureTime }
	t.Cleanup(func() { nowUTC = restoreClock })

	f := &approvalDecideFixture{
		t: t, project: decideTestProject,
		approvalID: "ap-1", foreignApprovalID: "ap-other",
	}
	store, _, err := runstore.OpenStore(context.Background(), filepath.Join(t.TempDir(), "codeflow.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	f.store = store

	seedDecideFixture(t, store)

	// Read back the fingerprint and revision the service stored, so the test
	// presents what the row actually binds.
	row := f.mustGetApproval(decideTestProject, f.approvalID)
	f.fingerprint = row.Fingerprint
	f.revision = row.Revision
	foreign := f.mustGetApproval(decideTestProject2, f.foreignApprovalID)
	f.foreignFingerprint = foreign.Fingerprint

	f.router = gin.New()
	f.router.Use(gin.Recovery(), withSidecarIdentity())
	v1 := f.router.Group("/api/v1")
	v1.POST("/projects/:id/approvals/:approval_id/decide", DecideApproval)

	SetCommandStore(store)
	t.Cleanup(func() { SetCommandStore(nil) })
	return f
}

// seedDecideFixture creates two projects, tasks, snapshots, runs and attempts,
// moves run-1 to running (queued -> starting -> running through the production
// transitions) and requests one high-risk tool approval for it, plus a pending
// gate approval for the second project (no Run needed).
func seedDecideFixture(t *testing.T, store *runstore.Store) {
	t.Helper()
	ctx := context.Background()
	now := decideFixtureTime

	err := store.WithTx(ctx, func(ctx context.Context, tx runstore.Tx) error {
		for _, projectID := range []string{decideTestProject, decideTestProject2} {
			if err := runstore.UpsertProjectRef(ctx, tx, run.ProjectRef{
				ProjectID:    projectID,
				SnapshotHash: "sha256:" + projectID,
				State:        run.ProjectRefStateActive,
				CapturedAt:   now,
				VerifiedAt:   now,
			}); err != nil {
				return err
			}
		}
		tasks := []run.Task{
			{ID: decideTestTask, ProjectID: decideTestProject, Title: "add tests", Kind: run.TaskKindCode,
				Status: run.TaskStatusReady, InputJSON: `{"prompt":"add tests"}`, CreatedAt: now, UpdatedAt: now},
			{ID: decideTestTask2, ProjectID: decideTestProject2, Title: "other", Kind: run.TaskKindCode,
				Status: run.TaskStatusReady, InputJSON: `{"prompt":"other"}`, CreatedAt: now, UpdatedAt: now},
		}
		for i := range tasks {
			if err := runstore.InsertTask(ctx, tx, &tasks[i]); err != nil {
				return err
			}
		}
		hash1, err := runstore.InsertInputSnapshot(ctx, tx, decideTestProject, "snap-1",
			json.RawMessage(`{"prompt":"add tests"}`), now)
		if err != nil {
			return err
		}
		hash2, err := runstore.InsertInputSnapshot(ctx, tx, decideTestProject2, "snap-2",
			json.RawMessage(`{"prompt":"other"}`), now)
		if err != nil {
			return err
		}
		runs := []run.Run{
			{ID: decideTestRun, TaskID: decideTestTask, ProjectID: decideTestProject, BindingID: "b-1", BindingRevision: 1,
				BaseManifestHash: "sha256:manifest", AgentRevisionID: "ar-1",
				InputSnapshotID: "snap-1", InputSnapshotHash: hash1,
				Status: run.RunStatusQueued, CreatedAt: now, UpdatedAt: now},
			{ID: decideTestRun2, TaskID: decideTestTask2, ProjectID: decideTestProject2, BindingID: "b-1", BindingRevision: 1,
				BaseManifestHash: "sha256:manifest", AgentRevisionID: "ar-1",
				InputSnapshotID: "snap-2", InputSnapshotHash: hash2,
				Status: run.RunStatusQueued, CreatedAt: now, UpdatedAt: now},
		}
		for i := range runs {
			if err := runstore.InsertRun(ctx, tx, &runs[i]); err != nil {
				return err
			}
		}
		attempts := []run.Attempt{
			{ID: decideTestAttempt, RunID: decideTestRun, AttemptNo: 1, Backend: "claude_code",
				Status: run.AttemptStatusRunning, CreatedAt: now},
			{ID: decideTestAttempt2, RunID: decideTestRun2, AttemptNo: 1, Backend: "claude_code",
				Status: run.AttemptStatusRunning, CreatedAt: now},
		}
		for i := range attempts {
			if err := runstore.InsertAttempt(ctx, tx, &attempts[i]); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed fixture: %v", err)
	}

	// Move run-1 queued -> starting -> running, the way the scheduler will.
	for _, trigger := range []struct {
		from run.RunStatus
		name run.ExecutionEventType
	}{
		{run.RunStatusQueued, run.EventSchedulerClaimed},
		{run.RunStatusStarting, run.EventProcessStarted},
	} {
		err := store.WithTx(ctx, func(ctx context.Context, tx runstore.Tx) error {
			current, err := runstore.GetRun(ctx, tx, decideTestRun)
			if err != nil {
				return err
			}
			_, err = runstore.TransitionRunTx(ctx, tx, runstore.TransitionInput{
				RunID:            decideTestRun,
				ExpectedRevision: current.Revision,
				ExpectedStatus:   trigger.from,
				Trigger:          run.Trigger{Kind: run.TriggerEvent, Name: string(trigger.name)},
				At:               now.Add(time.Second),
				Actor:            run.Actor{Type: run.ActorTypeSystem, ID: "test"},
				AttemptID:        decideStrPtr(decideTestAttempt),
				AgentRevisionID:  decideStrPtr(current.AgentRevisionID),
				Destinations:     []string{"audit:export"},
			})
			return err
		})
		if err != nil {
			t.Fatalf("move run to %s: %v", trigger.from, err)
		}
	}

	// The pending tool approval: "npm test" is high risk (test_or_install), so
	// it becomes pending and moves the Run to waiting_approval.
	argumentsHash, err := execbackend.ToolRequestFingerprint("run_shell", json.RawMessage(`{"command":"npm test"}`))
	if err != nil {
		t.Fatalf("ToolRequestFingerprint: %v", err)
	}
	toolRequest := approval.RequestToolInput{
		ID:              "ap-1",
		ProjectID:       decideTestProject,
		RunID:           decideTestRun,
		AttemptID:       decideTestAttempt,
		AgentRevisionID: "ar-1",
		SubjectID:       decideTestToolCall,
		Action: approval.ToolAction{
			Tool:    "run_shell",
			Command: []string{"npm", "test"},
		},
		FingerprintInput: approval.FingerprintInput{
			ProjectID:        decideTestProject,
			SubjectType:      approval.SubjectTool,
			SubjectID:        decideTestToolCall,
			AgentRevisionID:  "ar-1",
			ArgumentsHash:    argumentsHash,
			Command:          []string{"npm", "test"},
			Cwd:              "/workspace/p-1",
			TargetPaths:      []string{"src/a.go"},
			BaseManifestHash: "sha256:manifest",
			PolicyVersion:    policy.RuleVersion,
		},
		Scope:         []byte(`{"paths":["src/a.go"]}`),
		RiskConfig:    approval.RiskConfig{},
		PolicyVersion: policy.RuleVersion,
		RequestedAt:   now,
		ExpiresAt:     now.Add(time.Hour),
	}
	// The fingerprint hashes RunID/AttemptID only when ToolRequestApprovalTx
	// passes them; the request struct has no such fields, so nothing more is
	// needed here.
	err = store.WithTx(ctx, func(ctx context.Context, tx runstore.Tx) error {
		result, err := approval.RequestToolApprovalTx(ctx, tx, toolRequest)
		if err != nil {
			return err
		}
		if result.Approval.Status != approval.StatusPending {
			return fmt.Errorf("fixture approval is %s, want pending", result.Approval.Status)
		}
		if result.Approval.Risk != approval.RiskHigh {
			return fmt.Errorf("fixture approval risk is %s, want high", result.Approval.Risk)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("request tool approval: %v", err)
	}

	// A pending gate approval for the other project, for the cross-project 404.
	err = store.WithTx(ctx, func(ctx context.Context, tx runstore.Tx) error {
		fingerprintInput := approval.FingerprintInput{
			ProjectID:        decideTestProject2,
			SubjectType:      approval.SubjectGate,
			SubjectID:        "gate-1",
			BaseManifestHash: "sha256:manifest",
			PolicyVersion:    policy.RuleVersion,
		}
		_, err := approval.CreateApprovalTx(ctx, tx, approval.NewApproval{
			ID:               "ap-other",
			ProjectID:        decideTestProject2,
			SubjectType:      approval.SubjectGate,
			SubjectID:        "gate-1",
			Risk:             approval.RiskMedium,
			Scope:            []byte(`{}`),
			FingerprintInput: fingerprintInput,
			RequestedAt:      now,
			ExpiresAt:        now.Add(time.Hour),
			PolicyVersion:    policy.RuleVersion,
		})
		return err
	})
	if err != nil {
		t.Fatalf("create foreign approval: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Fixture helpers
// ---------------------------------------------------------------------------

func decideStrPtr(s string) *string { return &s }

func (f *approvalDecideFixture) mustGetApproval(projectID, approvalID string) approval.Approval {
	f.t.Helper()
	row, err := approval.GetApproval(context.Background(), f.store.DB(), approvalID)
	if err != nil {
		f.t.Fatalf("GetApproval(%s): %v", approvalID, err)
	}
	if row.ProjectID != projectID {
		f.t.Fatalf("approval %s belongs to %s, want %s", approvalID, row.ProjectID, projectID)
	}
	return row
}

// storedApproval reads the current row with direct SQL-free access, via the
// service's own reader.
func (f *approvalDecideFixture) storedApproval(approvalID string) approval.Approval {
	f.t.Helper()
	row, err := approval.GetApproval(context.Background(), f.store.DB(), approvalID)
	if err != nil {
		f.t.Fatalf("GetApproval(%s): %v", approvalID, err)
	}
	return row
}

// decisionEventTypes returns the types of the approval's run events, in run_seq
// order.
func (f *approvalDecideFixture) decisionEventTypes(runID string) []string {
	f.t.Helper()
	events, _, err := runstore.ListRunEvents(context.Background(), f.store.DB(), runID, 0, 1000)
	if err != nil {
		f.t.Fatalf("ListRunEvents(%s): %v", runID, err)
	}
	out := make([]string, 0, len(events))
	for _, event := range events {
		out = append(out, event.Type)
	}
	return out
}

// countApprovalEvents counts approval.decided events whose payload names the
// given approval, over the project's timeline.
func (f *approvalDecideFixture) countApprovalEvents(approvalID string) int {
	f.t.Helper()
	events, _, err := runstore.ListProjectEvents(context.Background(), f.store.DB(), decideTestProject, 0, 1000)
	if err != nil {
		f.t.Fatalf("ListProjectEvents: %v", err)
	}
	count := 0
	for _, event := range events {
		if event.Type != string(run.EventApprovalDecided) {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			f.t.Fatalf("decode event payload: %v", err)
		}
		if payload["approval_id"] == approvalID {
			count++
		}
	}
	return count
}

// approvalEventPayload returns the payload of the (single) approval.decided
// event for one approval.
func (f *approvalDecideFixture) approvalEventPayload(approvalID string) map[string]any {
	f.t.Helper()
	events, _, err := runstore.ListProjectEvents(context.Background(), f.store.DB(), decideTestProject, 0, 1000)
	if err != nil {
		f.t.Fatalf("ListProjectEvents: %v", err)
	}
	for _, event := range events {
		if event.Type != string(run.EventApprovalDecided) {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			f.t.Fatalf("decode event payload: %v", err)
		}
		if payload["approval_id"] == approvalID {
			return payload
		}
	}
	f.t.Fatalf("no approval.decided event for %s", approvalID)
	return nil
}

// commandRecordCount counts the command records the endpoint claimed.
func (f *approvalDecideFixture) commandRecordCount() int {
	f.t.Helper()
	var n int
	if err := f.store.DB().QueryRow(`SELECT count(*) FROM command_records`).Scan(&n); err != nil {
		f.t.Fatalf("count command_records: %v", err)
	}
	return n
}

// ---------------------------------------------------------------------------
// Request helpers
// ---------------------------------------------------------------------------

// decideBody builds a well-formed body for the fixture's approval, overriding
// fields through the mutator when given.
func (f *approvalDecideFixture) decideBody(approved bool, mutate func(map[string]any)) string {
	f.t.Helper()
	body := map[string]any{
		"approved":          approved,
		"fingerprint":       f.fingerprint,
		"expected_revision": f.revision,
	}
	if mutate != nil {
		mutate(body)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		f.t.Fatalf("marshal body: %v", err)
	}
	return string(raw)
}

// decide posts one decision. clientKey "" omits the header; headers are merged
// over the defaults.
func (f *approvalDecideFixture) decide(approvalID, rawBody, clientKey string, headers map[string]string) apiResponse {
	f.t.Helper()
	return f.decidePath(f.project, approvalID, rawBody, clientKey, headers)
}

func (f *approvalDecideFixture) decidePath(projectID, approvalID, rawBody, clientKey string, headers map[string]string) apiResponse {
	f.t.Helper()
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/projects/"+projectID+"/approvals/"+approvalID+"/decide", bytes.NewReader([]byte(rawBody)))
	req.Header.Set("Content-Type", "application/json")
	if clientKey != "" {
		req.Header.Set(HeaderIdempotencyKey, clientKey)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return apiResponse{Status: rec.Code, Body: rec.Body.Bytes()}
}

func (f *approvalDecideFixture) decideWithHeader(approvalID, rawBody, clientKey string, headers map[string]string) recordedResponse {
	f.t.Helper()
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/projects/"+f.project+"/approvals/"+approvalID+"/decide", bytes.NewReader([]byte(rawBody)))
	req.Header.Set("Content-Type", "application/json")
	if clientKey != "" {
		req.Header.Set(HeaderIdempotencyKey, clientKey)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return recordedResponse{rec: rec}
}

// requireDecidedData asserts the shape of a 200 decision response. commandID is
// the key of the request that produced it: every answer echoes its own key as
// meta.command_id, so a replayed refusal of a second key carries that key.
func requireDecidedData(t *testing.T, decoded decodedBody, approvalID, status string, revision int64, commandID string) {
	t.Helper()
	if decoded.Data == nil {
		t.Fatalf("decision response has no data object")
	}
	if decoded.Data["id"] != approvalID {
		t.Errorf("data.id = %v, want %s", decoded.Data["id"], approvalID)
	}
	if decoded.Data["status"] != status {
		t.Errorf("data.status = %v, want %s", decoded.Data["status"], status)
	}
	if got, ok := decoded.Data["revision"].(float64); !ok || int64(got) != revision {
		t.Errorf("data.revision = %v, want %d", decoded.Data["revision"], revision)
	}
	for _, field := range []string{"project_id", "subject_type", "subject_id", "risk", "fingerprint", "expires_at"} {
		if value, _ := decoded.Data[field].(string); value == "" {
			t.Errorf("data.%s is missing (data %v)", field, decoded.Data)
		}
	}
	if decoded.Meta["command_id"] != commandID {
		t.Errorf("meta.command_id = %v, want %s", decoded.Meta["command_id"], commandID)
	}
}

// ---------------------------------------------------------------------------
// Success and idempotency
// ---------------------------------------------------------------------------

// TestDecideApprovalApproveSucceeds decides with approved:true and asserts the
// row, the event and the ETag.
func TestDecideApprovalApproveSucceeds(t *testing.T) {
	f := newApprovalDecideFixture(t)

	rec := f.decideWithHeader(f.approvalID, f.decideBody(true, nil), decideClientKey, nil)
	if rec.status() != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.status(), rec.body())
	}
	if got := rec.header(HeaderETag); got != `"2"` {
		t.Errorf("ETag = %q, want %q (a decision is revision 2)", got, `"2"`)
	}
	decoded := decodeAPIResponse(t, apiResponse{Body: rec.body()})
	requireDecidedData(t, decoded, f.approvalID, "approved", 2, decideClientKey)
	if decoded.Data["decided_by"] != principalUnderTest {
		t.Errorf("data.decided_by = %v, want %s", decoded.Data["decided_by"], principalUnderTest)
	}

	row := f.storedApproval(f.approvalID)
	if row.Status != approval.StatusApproved || row.DecidedBy != principalUnderTest {
		t.Errorf("stored row = %s by %s, want approved by %s", row.Status, row.DecidedBy, principalUnderTest)
	}
	if got := f.countApprovalEvents(f.approvalID); got != 1 {
		t.Errorf("approval.decided events = %d, want 1", got)
	}
	payload := f.approvalEventPayload(f.approvalID)
	if payload["status"] != "approved" {
		t.Errorf("event payload.status = %v, want approved", payload["status"])
	}
	// A decision does not move the Run (§27.2 item 3): it is still waiting for
	// the grant to be consumed.
	if payload["run_id"] != decideTestRun {
		t.Errorf("event payload.run_id = %v, want %s", payload["run_id"], decideTestRun)
	}
	runRow, err := runstore.GetRun(context.Background(), f.store.DB(), decideTestRun)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if runRow.Status != run.RunStatusWaitingApproval {
		t.Errorf("run status = %s, want waiting_approval (a decision never moves the Run)", runRow.Status)
	}
}

// TestDecideApprovalReplayIsByteIdentical retries the same key and the same
// request: the answer must be the recorded one, byte for byte, and the side
// effect must not happen twice.
func TestDecideApprovalReplayIsByteIdentical(t *testing.T) {
	f := newApprovalDecideFixture(t)
	body := f.decideBody(false, func(m map[string]any) { m["reason"] = "not on my machine" })

	first := f.decideWithHeader(f.approvalID, body, decideClientKey, nil)
	if first.status() != http.StatusOK {
		t.Fatalf("first status = %d, want 200 (body %s)", first.status(), first.body())
	}
	eventsAfterFirst := f.countApprovalEvents(f.approvalID)
	recordsAfterFirst := f.commandRecordCount()

	second := f.decideWithHeader(f.approvalID, body, decideClientKey, nil)
	if second.status() != first.status() {
		t.Fatalf("replay status = %d, want %d", second.status(), first.status())
	}
	if !bytes.Equal(second.body(), first.body()) {
		t.Errorf("replay body differs:\n first %s\nsecond %s", first.body(), second.body())
	}
	// The replay is answered from the recorded bytes, so the recorded answer
	// must be identical modulo the freshly minted request id: assert the data
	// branch the same way too.
	firstDecoded := decodeAPIResponse(t, apiResponse{Body: first.body()})
	secondDecoded := decodeAPIResponse(t, apiResponse{Body: second.body()})
	if fmt.Sprint(firstDecoded.Data) != fmt.Sprint(secondDecoded.Data) {
		t.Errorf("replay data differs: %v vs %v", firstDecoded.Data, secondDecoded.Data)
	}
	if got := f.countApprovalEvents(f.approvalID); got != eventsAfterFirst || got != 1 {
		t.Errorf("approval.decided events = %d, want 1 (no second event on a replay)", got)
	}
	if got := f.commandRecordCount(); got != recordsAfterFirst || got != 1 {
		t.Errorf("command records = %d, want 1", got)
	}
}

// TestDecideApprovalSameKeyDifferentDecisionIs409 sends a different decision
// under the same key: the stored answer belongs to the first request and must
// never be returned for this one, and this request must not execute.
func TestDecideApprovalSameKeyDifferentDecisionIs409(t *testing.T) {
	f := newApprovalDecideFixture(t)

	first := f.decide(f.approvalID, f.decideBody(true, nil), decideClientKey, nil)
	if first.Status != http.StatusOK {
		t.Fatalf("first status = %d, want 200 (body %s)", first.Status, first.Body)
	}
	// Same key, different answer (and a different comment, which is also part
	// of the request).
	second := f.decide(f.approvalID, f.decideBody(true, func(m map[string]any) { m["reason"] = "changed my mind" }), decideClientKey, nil)
	if second.Status != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body %s)", second.Status, second.Body)
	}
	decoded := requireStatus(t, second, http.StatusConflict)
	if decoded.Error.Code != string(CodeIdempotencyKeyReused) {
		t.Errorf("error.code = %s, want %s", decoded.Error.Code, CodeIdempotencyKeyReused)
	}
	if decoded.Error.Retryable {
		t.Error("idempotency_key_reused must not be retryable")
	}
	if got := f.countApprovalEvents(f.approvalID); got != 1 {
		t.Errorf("approval.decided events = %d, want 1", got)
	}
}

// TestDecideApprovalRepeatedDecisionDifferentKeyIsIdempotent re-answers the
// same decision under a new key: DecideTx reports it as the idempotent success
// it is, so the client gets 200 and the row keeps its one event.
func TestDecideApprovalRepeatedDecisionDifferentKeyIsIdempotent(t *testing.T) {
	f := newApprovalDecideFixture(t)

	first := f.decide(f.approvalID, f.decideBody(true, nil), decideClientKey, nil)
	if first.Status != http.StatusOK {
		t.Fatalf("first status = %d, want 200 (body %s)", first.Status, first.Body)
	}
	second := f.decide(f.approvalID, f.decideBody(true, nil), "decide_test_0002", nil)
	if second.Status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", second.Status, second.Body)
	}
	decoded := decodeAPIResponse(t, second)
	requireDecidedData(t, decoded, f.approvalID, "approved", 2, "decide_test_0002")
	if got := f.countApprovalEvents(f.approvalID); got != 1 {
		t.Errorf("approval.decided events = %d, want 1", got)
	}
}

// TestDecideApprovalOtherDecisionAlreadyLandedIs409 answers an approval someone
// else already rejected. The refusal names the current status and replays on a
// retry.
func TestDecideApprovalOtherDecisionAlreadyLandedIs409(t *testing.T) {
	f := newApprovalDecideFixture(t)

	first := f.decide(f.approvalID, f.decideBody(false, nil), decideClientKey, nil)
	if first.Status != http.StatusOK {
		t.Fatalf("first status = %d, want 200 (body %s)", first.Status, first.Body)
	}
	second := f.decide(f.approvalID, f.decideBody(true, nil), "decide_test_0002", nil)
	if second.Status != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body %s)", second.Status, second.Body)
	}
	errBody := requireErrorReason(t, second.Body, string(CodeConflict), reasonApprovalAlreadyDecided)
	if errBody.Details["status"] != "rejected" {
		t.Errorf("details.status = %v, want rejected", errBody.Details["status"])
	}
	// The refusal is recorded: the same key replays it byte-identically.
	replay := f.decide(f.approvalID, f.decideBody(true, nil), "decide_test_0002", nil)
	if replay.Status != http.StatusConflict || !bytes.Equal(replay.Body, second.Body) {
		t.Errorf("replay = %d %s, want the recorded 409 %s", replay.Status, replay.Body, second.Body)
	}
	if row := f.storedApproval(f.approvalID); row.Status != approval.StatusRejected {
		t.Errorf("stored status = %s, want rejected (the winner is not overwritten)", row.Status)
	}
}

// ---------------------------------------------------------------------------
// Preconditions
// ---------------------------------------------------------------------------

// TestDecideApprovalRevisionConflicts covers the three precondition shapes:
// a stale body expected_revision (409), a stale If-Match (412), and the two
// sources disagreeing (422).
func TestDecideApprovalRevisionConflicts(t *testing.T) {
	t.Run("body expected_revision conflict is 409", func(t *testing.T) {
		f := newApprovalDecideFixture(t)
		body := f.decideBody(true, func(m map[string]any) { m["expected_revision"] = float64(7) })
		response := f.decide(f.approvalID, body, decideClientKey, nil)
		if response.Status != http.StatusConflict {
			t.Fatalf("status = %d, want 409 (body %s)", response.Status, response.Body)
		}
		errBody := requireErrorReason(t, response.Body, string(CodeConflict), reasonRevisionConflict)
		if got, _ := errBody.Details["expected_revision"].(float64); int64(got) != 7 {
			t.Errorf("details.expected_revision = %v, want 7", errBody.Details["expected_revision"])
		}
		if got, _ := errBody.Details["current_revision"].(float64); int64(got) != 1 {
			t.Errorf("details.current_revision = %v, want 1", errBody.Details["current_revision"])
		}
		if row := f.storedApproval(f.approvalID); row.Status != approval.StatusPending {
			t.Errorf("stored status = %s, want pending (nothing written)", row.Status)
		}
		if got := f.countApprovalEvents(f.approvalID); got != 0 {
			t.Errorf("approval.decided events = %d, want 0", got)
		}
	})

	t.Run("If-Match conflict is 412", func(t *testing.T) {
		f := newApprovalDecideFixture(t)
		body := f.decideBody(true, func(m map[string]any) { delete(m, "expected_revision") })
		response := f.decide(f.approvalID, body, decideClientKey, map[string]string{HeaderIfMatch: `"7"`})
		if response.Status != http.StatusPreconditionFailed {
			t.Fatalf("status = %d, want 412 (body %s)", response.Status, response.Body)
		}
		errBody := requireErrorReason(t, response.Body, string(CodeConflict), reasonPreconditionFailed)
		if got, _ := errBody.Details["current_revision"].(float64); int64(got) != 1 {
			t.Errorf("details.current_revision = %v, want 1", errBody.Details["current_revision"])
		}
		if row := f.storedApproval(f.approvalID); row.Status != approval.StatusPending {
			t.Errorf("stored status = %s, want pending", row.Status)
		}
	})

	t.Run("If-Match and body disagree is 422", func(t *testing.T) {
		f := newApprovalDecideFixture(t)
		body := f.decideBody(true, func(m map[string]any) { m["expected_revision"] = float64(1) })
		response := f.decide(f.approvalID, body, decideClientKey, map[string]string{HeaderIfMatch: `"2"`})
		if response.Status != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422 (body %s)", response.Status, response.Body)
		}
		requireErrorReason(t, response.Body, string(CodeInvalidRequest), reasonIfMatchBodyMismatch)
		if f.commandRecordCount() != 0 {
			t.Error("a 422 before RunCommand must claim no key")
		}
	})

	t.Run("neither source is 422", func(t *testing.T) {
		f := newApprovalDecideFixture(t)
		body := f.decideBody(true, func(m map[string]any) { delete(m, "expected_revision") })
		response := f.decide(f.approvalID, body, decideClientKey, nil)
		if response.Status != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422 (body %s)", response.Status, response.Body)
		}
		requireErrorReason(t, response.Body, string(CodeInvalidRequest), reasonExpectedRevisionRequired)
		if f.commandRecordCount() != 0 {
			t.Error("a 422 before RunCommand must claim no key")
		}
	})

	t.Run("If-Match agreeing with the body succeeds", func(t *testing.T) {
		f := newApprovalDecideFixture(t)
		body := f.decideBody(true, nil)
		response := f.decide(f.approvalID, body, decideClientKey, map[string]string{HeaderIfMatch: `"1"`})
		if response.Status != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", response.Status, response.Body)
		}
		if row := f.storedApproval(f.approvalID); row.Status != approval.StatusApproved {
			t.Errorf("stored status = %s, want approved", row.Status)
		}
	})
}

// TestDecideApprovalFingerprintMismatchIs409 refuses a decision for a different
// fingerprint and proves nothing was written.
func TestDecideApprovalFingerprintMismatchIs409(t *testing.T) {
	f := newApprovalDecideFixture(t)
	body := f.decideBody(true, func(m map[string]any) { m["fingerprint"] = strings.Replace(f.fingerprint, "sha256:", "sha256:ff", 1) })

	response := f.decide(f.approvalID, body, decideClientKey, nil)
	if response.Status != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body %s)", response.Status, response.Body)
	}
	requireErrorReason(t, response.Body, string(CodeConflict), reasonFingerprintMismatch)
	if row := f.storedApproval(f.approvalID); row.Status != approval.StatusPending {
		t.Errorf("stored status = %s, want pending (nothing written)", row.Status)
	}
	if got := f.countApprovalEvents(f.approvalID); got != 0 {
		t.Errorf("approval.decided events = %d, want 0", got)
	}
}

// TestDecideApprovalExpiredIs409AndPersistsExpiry answers a pending approval
// whose deadline has passed. The 409 is the answer, and the stored row must
// really be expired: the expiry is the write that had to survive.
func TestDecideApprovalExpiredIs409AndPersistsExpiry(t *testing.T) {
	f := newApprovalDecideFixture(t)
	// Move the deadline before the pinned decision clock (decideFixtureTime):
	// the expiry rule is about the stored expires_at versus the decision time,
	// not about the wall clock, so the test drives it with both values it owns.
	expiresAt := decideFixtureTime.Add(-time.Minute).UnixMilli()
	if _, err := f.store.DB().Exec(`UPDATE approvals SET expires_at = ?, requested_at = ? WHERE id = ?`,
		expiresAt, expiresAt-3600_000, f.approvalID); err != nil {
		t.Fatalf("age the approval: %v", err)
	}

	response := f.decide(f.approvalID, f.decideBody(true, nil), decideClientKey, nil)
	if response.Status != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body %s)", response.Status, response.Body)
	}
	errBody := requireErrorReason(t, response.Body, string(CodeConflict), reasonApprovalExpired)
	if errBody.Details["status"] != "expired" {
		t.Errorf("details.status = %v, want expired", errBody.Details["status"])
	}

	row := f.storedApproval(f.approvalID)
	if row.Status != approval.StatusExpired {
		t.Fatalf("stored status = %s, want expired (the expiry must be committed, not rolled back)", row.Status)
	}
	if row.DecidedBy != "system:expiry" {
		t.Errorf("decided_by = %s, want system:expiry", row.DecidedBy)
	}
	if got := f.countApprovalEvents(f.approvalID); got != 1 {
		t.Errorf("approval.decided events = %d, want 1 (the expiry event)", got)
	}
	payload := f.approvalEventPayload(f.approvalID)
	if payload["status"] != "expired" {
		t.Errorf("event payload.status = %v, want expired", payload["status"])
	}
}

// ---------------------------------------------------------------------------
// Scope, auth and unconfigured store
// ---------------------------------------------------------------------------

// TestDecideApprovalCrossProjectAndMissingAreIndistinguishable checks that a
// nonexistent approval and one of another project produce the same 404 refusal
// (status, code, message and details — the parts a client branches on; meta is
// per request by definition), and that neither claims a decision.
func TestDecideApprovalCrossProjectAndMissingAreIndistinguishable(t *testing.T) {
	f := newApprovalDecideFixture(t)
	body := f.decideBody(true, nil)

	foreign := f.decide(f.foreignApprovalID, body, "decide_foreign_0001", nil)
	missing := f.decide("ap-does-not-exist", body, "decide_missing_0001", nil)

	for label, response := range map[string]apiResponse{"foreign": foreign, "missing": missing} {
		if response.Status != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404 (body %s)", label, response.Status, response.Body)
		}
		requireErrorReason(t, response.Body, string(CodeInvalidRequest), reasonApprovalNotFound)
	}
	foreignError := decodeAPIResponse(t, foreign).Error
	missingError := decodeAPIResponse(t, missing).Error
	if foreignError.Code != missingError.Code ||
		foreignError.Message != missingError.Message ||
		foreignError.Retryable != missingError.Retryable ||
		fmt.Sprint(foreignError.Details) != fmt.Sprint(missingError.Details) {
		t.Errorf("404 refusals differ:\nforeign %+v\nmissing %+v", foreignError, missingError)
	}
	// Both were recorded refusals: retrying replays them byte-identically
	// (same key, same request).
	if replay := f.decide(f.foreignApprovalID, body, "decide_foreign_0001", nil); !bytes.Equal(replay.Body, foreign.Body) || replay.Status != foreign.Status {
		t.Errorf("foreign replay = %d %s, want the recorded 404 %s", replay.Status, replay.Body, foreign.Body)
	}
	if row := f.storedApproval(f.foreignApprovalID); row.Status != approval.StatusPending {
		t.Errorf("foreign approval status = %s, want pending (never touched)", row.Status)
	}
	if got := f.countApprovalEvents(f.approvalID); got != 0 {
		t.Errorf("approval.decided events on the local approval = %d, want 0", got)
	}
}

// TestDecideApprovalStoreUnconfiguredIs503 covers the deployment state: no
// store installed means backend_unavailable with retryable=true.
func TestDecideApprovalStoreUnconfiguredIs503(t *testing.T) {
	f := newApprovalDecideFixture(t)
	SetCommandStore(nil)
	t.Cleanup(func() { SetCommandStore(f.store) })

	response := f.decide(f.approvalID, f.decideBody(true, nil), decideClientKey, nil)
	if response.Status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (body %s)", response.Status, response.Body)
	}
	decoded := requireStatus(t, response, http.StatusServiceUnavailable)
	if decoded.Error.Code != string(CodeBackendUnavailable) || !decoded.Error.Retryable {
		t.Errorf("error = %+v, want backend_unavailable retryable", decoded.Error)
	}
	if reason, _ := decoded.Error.Details["reason"].(string); reason != reasonRunStoreUnavailable {
		t.Errorf("details.reason = %v, want %s", decoded.Error.Details["reason"], reasonRunStoreUnavailable)
	}
}

// TestDecideApprovalUnauthenticatedIs401 drops the identity middleware's actor
// and expects the unified 401, with nothing claimed.
func TestDecideApprovalUnauthenticatedIs401(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store, _, err := runstore.OpenStore(context.Background(), filepath.Join(t.TempDir(), "codeflow.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	SetCommandStore(store)
	t.Cleanup(func() { SetCommandStore(nil) })

	router := gin.New()
	router.Use(gin.Recovery())
	v1 := router.Group("/api/v1")
	v1.POST("/projects/:id/approvals/:approval_id/decide", DecideApproval)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/projects/"+decideTestProject+"/approvals/ap-none/decide",
		bytes.NewReader([]byte(`{"approved":true,"fingerprint":"sha256:`+strings.Repeat("ab", 32)+`","expected_revision":1}`)))
	req.Header.Set(HeaderIdempotencyKey, decideClientKey)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
	}
	decoded := decodeAPIResponse(t, apiResponse{Body: rec.Body.Bytes()})
	if decoded.Error == nil || decoded.Error.Code != string(CodeUnauthorized) {
		t.Errorf("error = %+v, want unauthorized", decoded.Error)
	}
	var n int
	if err := store.DB().QueryRow(`SELECT count(*) FROM command_records`).Scan(&n); err != nil {
		t.Fatalf("count command_records: %v", err)
	}
	if n != 0 {
		t.Errorf("command records = %d, want 0 (401 claims nothing)", n)
	}
}

// TestDecideApprovalMissingIdempotencyKeyIs422 covers the ledger's own
// precondition.
func TestDecideApprovalMissingIdempotencyKeyIs422(t *testing.T) {
	f := newApprovalDecideFixture(t)
	response := f.decide(f.approvalID, f.decideBody(true, nil), "", nil)
	if response.Status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (body %s)", response.Status, response.Body)
	}
	requireErrorReason(t, response.Body, string(CodeInvalidRequest), reasonIdempotencyKeyRequired)
	if row := f.storedApproval(f.approvalID); row.Status != approval.StatusPending {
		t.Errorf("stored status = %s, want pending", row.Status)
	}
}

// TestDecideApprovalInvalidPathProjectIs400 covers the path check every v1
// route shares.
func TestDecideApprovalInvalidPathProjectIs400(t *testing.T) {
	f := newApprovalDecideFixture(t)
	response := f.decidePath("not-a-uuid", f.approvalID, f.decideBody(true, nil), decideClientKey, nil)
	if response.Status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", response.Status, response.Body)
	}
	requireErrorReason(t, response.Body, string(CodeInvalidRequest), reasonInvalidProjectID)
}

// TestDecideApprovalApprovedFalseLandsRejected is the named behaviour at the
// HTTP layer (the named test in t202c_named_test.go drives it through the
// strict parser's corners as well).
func TestDecideApprovalApprovedFalseLandsRejected(t *testing.T) {
	f := newApprovalDecideFixture(t)
	response := f.decide(f.approvalID, f.decideBody(false, func(m map[string]any) { m["reason"] = "do not run that" }), decideClientKey, nil)
	if response.Status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", response.Status, response.Body)
	}
	row := f.storedApproval(f.approvalID)
	if row.Status != approval.StatusRejected {
		t.Fatalf("stored status = %s, want rejected", row.Status)
	}
	payload := f.approvalEventPayload(f.approvalID)
	if payload["status"] != "rejected" || payload["comment"] != "do not run that" {
		t.Errorf("event payload = %v, want status rejected with the comment", payload)
	}
}

// runStatus reads one run's status with the production reader.
func (f *approvalDecideFixture) runStatus(runID string) string {
	f.t.Helper()
	row, err := runstore.GetRun(context.Background(), f.store.DB(), runID)
	if err != nil {
		f.t.Fatalf("GetRun(%s): %v", runID, err)
	}
	return string(row.Status)
}
