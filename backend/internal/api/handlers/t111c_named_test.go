package handlers

// The four named tests of §28 T1.11.c, plus the helpers they need. This file is
// additive: commands_test.go is not touched, so its fixtures (newCommandFixture,
// createRunMount, seedProjectAndTask, apiResponse, decodeAPIResponse, ...) are
// reused as they are.
//
// What the four tests are for, in one line each:
//
//   - TestDuplicateCreateOneRun: a fresh idempotency key is claimed once under a
//     real race, through the HTTP router, against a real file-backed SQLite.
//   - TestSameKeyDifferentBody409: a key is bound to the request that claimed it;
//     another body under that key is refused and never sees the stored answer.
//   - TestLostResponseCanQueryCommand: a client that lost the response can find
//     the resource through GET /commands/:command_id, and the named resource
//     really exists in the run store.
//   - TestIfMatch412: the optimistic-concurrency precondition is a real CAS on
//     the Run row — 412 leaves the row and the event stream untouched, the
//     correct If-Match moves it exactly once, and the malformed forms are 422.
//
// Everything asserted about stored state is read with direct SQL or through
// runstore: a test that only looked at HTTP status codes would pass against a
// handler that answered from memory.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/run"
	"github.com/codeflow/backend/internal/runstore"
	"github.com/gin-gonic/gin"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// seedExtraTask writes one more ready task, so a concurrent test can give the
// two racing bodies two distinguishable side effects: whichever body wins the
// claim inserts its own Run, and the loser's Run must not exist either.
func seedExtraTask(t *testing.T, f *commandFixture, taskID string) {
	t.Helper()
	task := run.Task{
		ID:        taskID,
		ProjectID: f.project,
		Title:     "second task",
		Kind:      run.TaskKindCode,
		Status:    run.TaskStatusReady,
		Priority:  3,
		InputJSON: `{"prompt":"second task"}`,
		CreatedAt: time.UnixMilli(1700000000010),
		UpdatedAt: time.UnixMilli(1700000000011),
	}
	err := f.store.WithTx(context.Background(), func(ctx context.Context, tx runstore.Tx) error {
		return runstore.InsertTask(ctx, tx, &task)
	})
	if err != nil {
		t.Fatalf("seed task %s: %v", taskID, err)
	}
}

// createRunWithInjectableFailure is createRunMount plus a test-only trapdoor. A
// request carrying X-Test-Fail runs the same side effect and then returns a plain
// error, which is the "the side effect happened but the transaction rolled back"
// case §27.3's rollback rule is about. A normal request behaves exactly like
// createRunMount, down to the run id.
func createRunWithInjectableFailure(f *commandFixture, c *gin.Context) {
	if c.GetHeader("X-Test-Fail") == "" {
		createRunMount(f, c)
		return
	}
	var body struct {
		TaskID string `json:"task_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		respondAPIError(c, http.StatusUnprocessableEntity, CodeInvalidRequest, "invalid body", false, nil)
		return
	}
	key := c.GetHeader(HeaderIdempotencyKey)
	projectID := c.Param("id")
	RunCommand(c, f.store, CommandSpec{Operation: "runs.create", ProjectID: projectID, Body: body},
		func(ctx context.Context, tx runstore.Tx) (CommandResult, error) {
			runID := "run-" + key
			created := run.Run{
				ID: runID, TaskID: body.TaskID, ProjectID: projectID,
				CommandID: &key,
				BindingID: "binding-1", BindingRevision: 1,
				BaseManifestHash: "sha256:base", AgentRevisionID: "agent-rev-1",
				Budget:    run.Budget{Tokens: int64Ptr(50000)},
				Status:    run.RunStatusQueued,
				CreatedAt: time.UnixMilli(1700000000003),
				UpdatedAt: time.UnixMilli(1700000000004),
			}
			if err := insertRunWithSnapshot(ctx, tx, projectID, "snap-"+key, &created); err != nil {
				return CommandResult{}, err
			}
			return CommandResult{}, errors.New("injected failure after the side effect")
		})
}

// ---------------------------------------------------------------------------
// The test-only cancel route
// ---------------------------------------------------------------------------
// cancelBody is the request body of the test-only cancel route. expected_revision
// is a pointer so "absent" and "0" are different inputs, which is the
// distinction ExpectedRevision makes.
type cancelBody struct {
	ExpectedRevision *int64 `json:"expected_revision"`
	Reason           string `json:"reason"`
}

// cancelRunMount is the handler §28 T1.11.c asks for: a route the production
// table does not have yet (T1.04.c wires the real cancel), whose callback CASes
// the Run through runstore.TransitionRunTx and answers a lost CAS through a
// recorded refusal shaped exactly like RespondRevisionConflict's answer.
//
// The command operator and this route make the request one request to this
// handler only, so the test does not depend on the shape of any other route's
// request hash.
func cancelRunMount(f *commandFixture, c *gin.Context) {
	var body cancelBody
	if err := c.ShouldBindJSON(&body); err != nil {
		respondAPIError(c, http.StatusUnprocessableEntity, CodeInvalidRequest, "invalid body", false, nil)
		return
	}
	// Two 422s ExpectedRevision cannot express on its own: it reports "no source
	// supplied" and "the source was malformed" identically (ok = false), so a
	// route that called it and then proceeded would silently ignore a
	// precondition the client believes is protecting it.
	if body.Reason == "" {
		respondAPIError(c, http.StatusUnprocessableEntity, CodeInvalidRequest, "reason is required", false,
			apiErrorDetails("reason", "reason_required"))
		return
	}
	if strings.TrimSpace(c.GetHeader(HeaderIfMatch)) == "" && body.ExpectedRevision == nil {
		respondAPIError(c, http.StatusUnprocessableEntity, CodeInvalidRequest,
			"An If-Match header or a body expected_revision is required", false,
			apiErrorDetails("reason", reasonInvalidExpectedRev))
		return
	}

	revision, fromIfMatch, ok := ExpectedRevision(c, body.ExpectedRevision)
	if !ok {
		return // ExpectedRevision answered (422 invalid_if_match / mismatch / invalid_expected_revision).
	}
	runID := c.Param("run_id")
	projectID := c.Param("id")

	RunCommand(c, f.store, CommandSpec{Operation: "runs.cancel", ProjectID: projectID, Body: body},
		func(ctx context.Context, tx runstore.Tx) (CommandResult, error) {
			// The CAS: expected revision AND expected status, inside the command's
			// own transaction. A lost CAS becomes a recorded refusal rather than a
			// plain error, because a plain error rolls the transaction back and
			// answers 500 through handleCommandStoreError: the refusal has to be
			// recorded so a retry replays the 412 instead of re-running the cancel.
			result, err := runstore.TransitionRunTx(ctx, tx, runstore.TransitionInput{
				RunID:            runID,
				ExpectedRevision: revision,
				ExpectedStatus:   run.RunStatusQueued,
				Trigger:          run.CommandTrigger(run.CommandRunCancel),
				At:               time.Now().UTC(),
				Actor:            run.Actor{Type: run.ActorTypeUser, ID: "sidecar-user", Source: "sidecar-token"},
				Details:          map[string]any{"reason": body.Reason},
			})
			if err != nil {
				var conflict *runstore.RevisionConflictError
				if errors.As(err, &conflict) {
					return CommandResult{}, revisionConflictRejection(conflict, fromIfMatch)
				}
				return CommandResult{}, err
			}
			SetRevisionETag(c, result.Run.Revision)
			return CommandResult{
				StatusCode: http.StatusOK,
				Data: map[string]any{
					"run_id":   result.Run.ID,
					"status":   string(result.Run.Status),
					"revision": result.Run.Revision,
				},
				ResourceType: "run",
				ResourceID:   result.Run.ID,
			}, nil
		})
}

// revisionConflictRejection turns a lost Run CAS into the recorded refusal the
// production helper RespondRevisionConflict would have answered with, so the
// 412 is stored and replayed instead of being re-derived.
//
// Why a rejection and not RespondRevisionConflict: the callback runs inside
// RunCommand's transaction. A caller that answered the conflict itself would
// write a body and then let RunCommand write its own, and a writer that returned
// the conflict as a plain error would be mapped to 500 by
// handleCommandStoreError and would lose the recorded answer. TestIfMatch412
// pins the shape by running the same conflict error through
// RespondRevisionConflict and comparing the results field by field.
func revisionConflictRejection(conflict *runstore.RevisionConflictError, fromIfMatch bool) *CommandRejection {
	if fromIfMatch {
		return &CommandRejection{
			StatusCode: http.StatusPreconditionFailed,
			Code:       CodeConflict,
			Message:    "If-Match precondition failed",
			Details: map[string]any{
				"reason":            reasonPreconditionFailed,
				"run_id":            conflict.RunID,
				"current_status":    string(conflict.CurrentStatus),
				"expected_revision": conflict.Expected,
				"current_revision":  conflict.Current,
			},
		}
	}
	return &CommandRejection{
		StatusCode: http.StatusConflict,
		Code:       CodeConflict,
		Message:    "Resource revision has changed",
		Details: map[string]any{
			"reason":            reasonRevisionConflict,
			"run_id":            conflict.RunID,
			"current_status":    string(conflict.CurrentStatus),
			"expected_revision": conflict.Expected,
			"current_revision":  conflict.Current,
		},
	}
}

// ---------------------------------------------------------------------------
// Request helpers
// ---------------------------------------------------------------------------

// recordedResponse is one answer with its headers, for the cases apiResponse
// cannot express: the ETag of a successful cancel is a header, not a body.
type recordedResponse struct {
	rec *httptest.ResponseRecorder
}

// status and body are read at the moment they are asked for rather than through
// a *httptest.ResponseRecorder value: the recorder is only safe to touch from
// the goroutine that owns it, so a concurrent helper must copy scalars.
func (r recordedResponse) status() int               { return r.rec.Code }
func (r recordedResponse) body() []byte              { return r.rec.Body.Bytes() }
func (r recordedResponse) header(name string) string { return r.rec.Header().Get(name) }

// doRawRecorder is doRaw for the cases that need a response header.
func (f *commandFixture) doRawRecorder(method, target string, raw []byte, headers map[string]string) recordedResponse {
	f.t.Helper()
	req := httptest.NewRequest(method, target, bytes.NewReader(raw))
	if len(raw) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return recordedResponse{rec: rec}
}

// postCancel sends one cancel request to the test-only route. An empty ifMatch
// omits the header; rawBody is sent verbatim (which is what makes the hash
// comparison in the 409 case meaningful).
func (f *commandFixture) postCancel(clientKey, runID, rawBody, ifMatch string) apiResponse {
	f.t.Helper()
	headers := map[string]string{}
	if clientKey != "" {
		headers[HeaderIdempotencyKey] = clientKey
	}
	if ifMatch != "" {
		headers[HeaderIfMatch] = ifMatch
	}
	return f.doRaw(http.MethodPost, "/api/v1/projects/"+f.project+"/runs/"+runID+"/cancel", []byte(rawBody), headers)
}

// ---------------------------------------------------------------------------
// Assertions over stored state (direct SQL and runstore, never the HTTP answer)
// ---------------------------------------------------------------------------

// concurrentResponses runs callers goroutines that all start together and
// returns their answers in caller order. The barrier is a real one: every
// caller is parked on start before the first is released, so the responses come
// from a genuine race rather than from a sleep that usually orders them.
func concurrentResponses(callers int, send func(index int) apiResponse) []apiResponse {
	var (
		parked sync.WaitGroup
		done   sync.WaitGroup
		start  = make(chan struct{})
	)
	responses := make([]apiResponse, callers)
	parked.Add(callers)
	done.Add(callers)
	for i := 0; i < callers; i++ {
		go func(index int) {
			defer done.Done()
			parked.Done()
			<-start
			responses[index] = send(index)
		}(i)
	}
	parked.Wait()
	close(start)
	done.Wait()
	return responses
}

// storedRunStatusRevision reads a Run's status and revision with direct SQL, so
// an assertion does not go through the code under test.
func storedRunStatusRevision(t *testing.T, f *commandFixture, runID string) (string, int64) {
	t.Helper()
	var (
		status   string
		revision int64
	)
	if err := f.store.DB().QueryRow(`SELECT status, revision FROM runs WHERE id = ?`, runID).Scan(&status, &revision); err != nil {
		t.Fatalf("read run %s: %v", runID, err)
	}
	return status, revision
}

// requireRunState asserts a Run's stored status and revision.
func requireRunState(t *testing.T, f *commandFixture, runID, wantStatus string, wantRevision int64) {
	t.Helper()
	status, revision := storedRunStatusRevision(t, f, runID)
	if status != wantStatus || revision != wantRevision {
		t.Errorf("run %s = (%s, revision %d), want (%s, %d)", runID, status, revision, wantStatus, wantRevision)
	}
}

// countEventsForRun returns the run's event count, how many of them are state
// events, and the per-type counts.
func countEventsForRun(t *testing.T, f *commandFixture, runID string) (total, state int, byType map[string]int) {
	t.Helper()
	rows, err := f.store.DB().Query(`SELECT type, count(*) FROM events WHERE run_id = ? GROUP BY type`, runID)
	if err != nil {
		t.Fatalf("count events for %s: %v", runID, err)
	}
	defer rows.Close()
	byType = map[string]int{}
	for rows.Next() {
		var (
			eventType string
			n         int
		)
		if err := rows.Scan(&eventType, &n); err != nil {
			t.Fatalf("scan event count: %v", err)
		}
		byType[eventType] = n
		total += n
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate event counts: %v", err)
	}
	state = total - byType[string(run.EventLegacyFlowEvent)]
	return total, state, byType
}

// eventPayloadForRun decodes the payload of the single event of one type.
func eventPayloadForRun(t *testing.T, f *commandFixture, runID, eventType string) map[string]any {
	t.Helper()
	var raw string
	err := f.store.DB().QueryRow(`SELECT payload_json FROM events WHERE run_id = ? AND type = ?`, runID, eventType).Scan(&raw)
	if err != nil {
		t.Fatalf("read %s payload for %s: %v", eventType, runID, err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode %s payload %s: %v", eventType, raw, err)
	}
	return payload
}

// storedCommandRow is the command_records row read with direct SQL: the columns
// the tests compare against the request they sent.
type storedCommandRow struct {
	RequestHash   string
	State         string
	StatusCode    *int
	ResourceType  *string
	ResourceID    *string
	ResponseBytes []byte
}

func readCommandRow(t *testing.T, f *commandFixture, commandID string) storedCommandRow {
	t.Helper()
	var (
		row           storedCommandRow
		stateCode     *int64
		resourceType  *string
		resourceID    *string
		responseBytes []byte
	)
	err := f.store.DB().QueryRow(`
		SELECT request_hash, state, status_code, resource_type, resource_id, response_json
		FROM command_records WHERE command_id = ?`, commandID).Scan(
		&row.RequestHash, &row.State, &stateCode, &resourceType, &resourceID, &responseBytes)
	if err != nil {
		t.Fatalf("read command_records for %s: %v", commandID, err)
	}
	if stateCode != nil {
		code := int(*stateCode)
		row.StatusCode = &code
	}
	row.ResourceType = resourceType
	row.ResourceID = resourceID
	row.ResponseBytes = responseBytes
	return row
}

// describeResponses renders a race's answers compactly: statuses in caller
// order, with the distinct bodies counted once. A failure message must stay
// readable when sixteen callers answered.
func describeResponses(responses []apiResponse) string {
	bodies := map[string]int{}
	for _, response := range responses {
		bodies[string(response.Body)]++
	}
	keys := make([]string, 0, len(bodies))
	for body := range bodies {
		keys = append(keys, body)
	}
	sort.Strings(keys)
	statuses := make([]string, len(responses))
	for index, response := range responses {
		statuses[index] = strconv.Itoa(response.Status)
	}
	parts := make([]string, 0, len(keys))
	for _, body := range keys {
		parts = append(parts, fmt.Sprintf("%d x %s", bodies[body], body))
	}
	return "statuses [" + strings.Join(statuses, " ") + "]; bodies " + strings.Join(parts, " | ")
}

// requireErrorReason asserts an error envelope's code and details.reason.
func requireErrorReason(t *testing.T, body []byte, wantCode, wantReason string) decodedError {
	t.Helper()
	decoded := decodeAPIResponse(t, apiResponse{Body: body})
	if decoded.Error == nil {
		t.Fatalf("response has no error envelope: %s", body)
	}
	if decoded.Error.Code != wantCode {
		t.Errorf("error.code = %s, want %s (body %s)", decoded.Error.Code, wantCode, body)
	}
	if reason, _ := decoded.Error.Details["reason"].(string); reason != wantReason {
		t.Errorf("details.reason = %v, want %s (body %s)", decoded.Error.Details["reason"], wantReason, body)
	}
	return *decoded.Error
}

// ifMatchStatusFor reports the status the same conflict error is answered with
// by the production helper, for the 412/409 rule.
func ifMatchStatusFor(t *testing.T, conflict *runstore.RevisionConflictError, fromIfMatch bool) int {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/probe", func(c *gin.Context) { RespondRevisionConflict(c, conflict, fromIfMatch) })
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/probe", nil))
	return rec.Code
}

// ---------------------------------------------------------------------------
// TestDuplicateCreateOneRun
// ---------------------------------------------------------------------------

// TestDuplicateCreateOneRun is the retry-storm test of §28 T1.11.c: sixteen
// clients send the same create command at the same time, through the real
// router, and the ledger has to make that one command.
//
// The race is real rather than nominal: all sixteen are parked on a barrier and
// released together, so at least one pair overlaps whatever the scheduler does.
// The record is read after the race and again after a replay. What a race cannot
// reliably expose is an answer recorded in a second transaction (that window is
// microseconds wide); that atomicity is pinned deterministically by
// TestRunCommandAnswerIsRecordedWithTheSideEffect.
func TestDuplicateCreateOneRun(t *testing.T) {
	f := newCommandFixture(t, createRunMount)
	key := "rq_dup_0001"
	body := `{"task_id":"` + f.task + `"}`
	wantRunID := "run-" + key

	// The hash the route computes for this exact request, so the direct SQL
	// assertions compare against what the ledger actually stored.
	requestHash, err := runstore.CommandRequestHash(http.MethodPost, "/api/v1/projects/:id/runs", []byte(body))
	if err != nil {
		t.Fatalf("CommandRequestHash: %v", err)
	}

	const callers = 16
	responses := concurrentResponses(callers, func(int) apiResponse { return f.postRuns(key, body) })

	// Every caller must get the recorded answer, byte for byte. A single-phase
	// command is never visible while it is in flight: the claim, the side effect
	// and the answer are one transaction, so a loser can only ever read a
	// committed (that is, terminal) record. A 202 carrying an in-flight
	// projection here would mean the record was committed before the side
	// effect, which is the double-execution window the ledger exists to close.
	for index, response := range responses {
		if response.Status != http.StatusAccepted {
			t.Fatalf("caller %d status = %d, want 202 (body %s)", index, response.Status, response.Body)
		}
		if response.Status >= 500 {
			t.Fatalf("caller %d got a server error: %d %s", index, response.Status, response.Body)
		}
		if !bytes.Equal(response.Body, responses[0].Body) {
			t.Fatalf("caller %d answered %s, want the first answer byte for byte: %s", index, response.Body, responses[0].Body)
		}
		if !bytes.Contains(response.Body, []byte(wantRunID)) || !bytes.Contains(response.Body, []byte(key)) {
			t.Fatalf("caller %d answer %s does not name %s", index, response.Body, wantRunID)
		}
	}

	// Exactly one row in each table, with the request that claimed them.
	if n := f.runCount(); n != 1 {
		t.Fatalf("runs = %d, want exactly 1 despite %d concurrent requests", n, callers)
	}
	if n := f.commandRecordCount(); n != 1 {
		t.Fatalf("command_records = %d, want exactly 1", n)
	}
	row := readCommandRow(t, f, key)
	if row.RequestHash != requestHash {
		t.Errorf("stored request_hash = %s, want %s (the hash of the request that claimed it)", row.RequestHash, requestHash)
	}
	if row.State != string(runstore.CommandStateSucceeded) {
		t.Errorf("stored state = %s, want succeeded after every caller was answered", row.State)
	}
	if row.StatusCode == nil || *row.StatusCode != http.StatusAccepted {
		t.Errorf("stored status_code = %v, want 202", row.StatusCode)
	}
	if row.ResourceType == nil || *row.ResourceType != "run" {
		t.Errorf("stored resource_type = %v, want run", row.ResourceType)
	}
	if row.ResourceID == nil || *row.ResourceID != wantRunID {
		t.Errorf("stored resource_id = %v, want %s", row.ResourceID, wantRunID)
	}
	// The stored response is the bytes the callers were answered with, which is
	// what makes a later replay identical rather than merely equivalent.
	if len(row.ResponseBytes) == 0 || !bytes.Equal(row.ResponseBytes, responses[0].Body) {
		t.Errorf("stored response %s is not the answer the callers received: %s", row.ResponseBytes, responses[0].Body)
	}

	// The single Run is the one the command created, queued, at revision 1.
	status, revision := storedRunStatusRevision(t, f, wantRunID)
	if status != string(run.RunStatusQueued) || revision != 1 {
		t.Errorf("run %s = (%s, revision %d), want (queued, 1)", wantRunID, status, revision)
	}
	var commandID *string
	if err := f.store.DB().QueryRow(`SELECT command_id FROM runs WHERE id = ?`, wantRunID).Scan(&commandID); err != nil {
		t.Fatalf("read runs.command_id: %v", err)
	}
	if commandID == nil || *commandID != key {
		t.Errorf("runs.command_id = %v, want %s", commandID, key)
	}

	// A command that has never been retried replays without touching the side
	// effect, so a replay cannot add a second Run either.
	replay := f.postRuns(key, body)
	if replay.Status != http.StatusAccepted {
		t.Fatalf("replay status = %d, want 202 (body %s)", replay.Status, replay.Body)
	}
	if !bytes.Equal(replay.Body, responses[0].Body) {
		t.Errorf("replay body = %s, want the recorded answer %s", replay.Body, responses[0].Body)
	}
	if n := f.runCount(); n != 1 {
		t.Errorf("runs after the replay = %d, want 1", n)
	}

	// Read the record a second time, after every response has been written: a
	// race that left a second execution half-done would show up here as a state
	// other than succeeded, or as another row.
	row = readCommandRow(t, f, key)
	if row.State != string(runstore.CommandStateSucceeded) {
		t.Errorf("stored state after the replay = %s, want succeeded", row.State)
	}
	if n := f.commandRecordCount(); n != 1 {
		t.Errorf("command_records after the replay = %d, want 1", n)
	}
	if n := f.runCount(); n != 1 {
		t.Errorf("runs after the replay = %d, want 1", n)
	}
}

// ---------------------------------------------------------------------------
// TestSameKeyDifferentBody409
// ---------------------------------------------------------------------------

// TestSameKeyDifferentBody409 covers §27.3's key-reuse rule from both sides. A
// key is bound to the request that claimed it: another body under the same key
// is refused, never executed, and never shown the stored answer — and when the
// two bodies arrive at the same time, the same rule decides the race, so exactly
// one of them may produce a Run.
//
// The stored request_hash is asserted with direct SQL: the property is that the
// ledger kept the *first* request's hash, not that a status code looked right.
func TestSameKeyDifferentBody409(t *testing.T) {
	bodyA := `{"task_id":"` + commandTestTask + `"}`
	bodyB := `{"task_id":"task-2"}`

	t.Run("sequential reuse", func(t *testing.T) {
		f := newCommandFixture(t, createRunMount)
		seedExtraTask(t, f, "task-2")
		key := "rq_diff_body_1"
		runID := "run-" + key

		hashA, err := runstore.CommandRequestHash(http.MethodPost, "/api/v1/projects/:id/runs", []byte(bodyA))
		if err != nil {
			t.Fatalf("CommandRequestHash: %v", err)
		}
		hashB, err := runstore.CommandRequestHash(http.MethodPost, "/api/v1/projects/:id/runs", []byte(bodyB))
		if err != nil {
			t.Fatalf("CommandRequestHash: %v", err)
		}
		if hashA == hashB {
			t.Fatal("the two bodies hash the same; this test would prove nothing")
		}

		first := f.postRuns(key, bodyA)
		requireStatus(t, first, http.StatusAccepted)

		// The second body: a real task, a real request, and a key that is taken.
		reused := f.postRuns(key, bodyB)
		if reused.Status != http.StatusConflict {
			t.Fatalf("reused key status = %d, want 409 (body %s)", reused.Status, reused.Body)
		}
		reusedEnvelope := decodeAPIResponse(t, reused)
		decoded := requireErrorReason(t, reused.Body, string(CodeIdempotencyKeyReused), "idempotency_key_reused")
		if decoded.Retryable {
			t.Error("idempotency_key_reused must not be retryable with the same key")
		}
		if reusedEnvelope.Data != nil {
			t.Errorf("the refused request was given a data object: %v", reusedEnvelope.Data)
		}
		if bytes.Contains(reused.Body, []byte(runID)) {
			t.Errorf("the refused request was told about %s: %s", runID, reused.Body)
		}

		// Nothing executed, and the stored hash is still the first request's.
		if n := f.runCount(); n != 1 {
			t.Errorf("runs = %d, want 1: the refused request must not execute", n)
		}
		row := readCommandRow(t, f, key)
		if row.RequestHash != hashA {
			t.Errorf("stored request_hash = %s, want the first request's %s", row.RequestHash, hashA)
		}
		if row.RequestHash == hashB {
			t.Error("stored request_hash is the second body's; the key was rebound")
		}

		// The first body still replays its own answer, byte for byte.
		replay := f.postRuns(key, bodyA)
		requireStatus(t, replay, http.StatusAccepted)
		if !bytes.Equal(first.Body, replay.Body) {
			t.Errorf("replay body = %s, want the first answer %s", replay.Body, first.Body)
		}
		if n := f.runCount(); n != 1 {
			t.Errorf("runs after the replay = %d, want 1", n)
		}
	})

	t.Run("concurrent bodies", func(t *testing.T) {
		f := newCommandFixture(t, createRunMount)
		seedExtraTask(t, f, "task-2")
		key := "rq_diff_body_race"
		runID := "run-" + key

		hashA, err := runstore.CommandRequestHash(http.MethodPost, "/api/v1/projects/:id/runs", []byte(bodyA))
		if err != nil {
			t.Fatalf("CommandRequestHash: %v", err)
		}
		hashB, err := runstore.CommandRequestHash(http.MethodPost, "/api/v1/projects/:id/runs", []byte(bodyB))
		if err != nil {
			t.Fatalf("CommandRequestHash: %v", err)
		}

		// A fresh key, so the race — not a replay of an earlier claim — decides
		// which body wins: whichever request reaches the ledger first hashes the
		// record, and the other is refused for the rest of the key's life.
		responses := concurrentResponses(16, func(index int) apiResponse {
			if index%2 == 0 {
				return f.postRuns(key, bodyA)
			}
			return f.postRuns(key, bodyB)
		})

		var (
			winnerIndex  = -1
			winnerTask   string
			winnerBody   []byte
			winnerCount  int
			refusedCount int
			badStatuses  []string
		)
		for index, response := range responses {
			switch response.Status {
			case http.StatusAccepted:
				if winnerIndex < 0 {
					winnerIndex = index
					winnerBody = response.Body
					if index%2 == 0 {
						winnerTask = commandTestTask
					} else {
						winnerTask = "task-2"
					}
				}
				winnerCount++
				if index%2 != winnerIndex%2 {
					t.Errorf("caller %d sent the other body but was accepted: %s", index, response.Body)
				}
				if !bytes.Equal(response.Body, winnerBody) {
					t.Errorf("winning caller %d answered %s, want %s", index, response.Body, winnerBody)
				}
				if !bytes.Contains(response.Body, []byte(runID)) {
					t.Errorf("winning caller %d answer %s does not name %s", index, response.Body, runID)
				}
			case http.StatusConflict:
				refusedCount++
				requireErrorReason(t, response.Body, string(CodeIdempotencyKeyReused), "idempotency_key_reused")
				if envelope := decodeAPIResponse(t, response); envelope.Data != nil {
					t.Errorf("refused caller %d was given a data object: %v", index, envelope.Data)
				}
				if bytes.Contains(response.Body, []byte(runID)) {
					t.Errorf("refused caller %d was told about %s: %s", index, runID, response.Body)
				}
			default:
				badStatuses = append(badStatuses, fmt.Sprintf("caller %d -> %d %s", index, response.Status, response.Body))
			}
		}
		if len(badStatuses) > 0 {
			t.Fatalf("callers that were neither accepted nor refused: %v", badStatuses)
		}
		if winnerIndex < 0 {
			t.Fatal("no caller succeeded; the race produced no command at all")
		}
		if winnerCount+refusedCount != 16 {
			t.Fatalf("accepted %d + refused %d != 16", winnerCount, refusedCount)
		}
		// Which body wins is what the race decides; the split is not. Every caller
		// that sent the winning body gets the recorded answer and every caller that
		// sent the other body is refused, so it is exactly 8 and 8.
		if winnerCount != 8 || refusedCount != 8 {
			t.Fatalf("the race did not split the two bodies: accepted %d, refused %d (statuses: %s)",
				winnerCount, refusedCount, describeResponses(responses))
		}

		// Exactly one Run, belonging to the winning body's task, and one record
		// carrying the winning body's hash and the run it created.
		if n := f.runCount(); n != 1 {
			t.Fatalf("runs = %d, want exactly 1", n)
		}
		var taskID string
		if err := f.store.DB().QueryRow(`SELECT task_id FROM runs WHERE id = ?`, runID).Scan(&taskID); err != nil {
			t.Fatalf("read the stored run: %v", err)
		}
		if taskID != winnerTask {
			t.Errorf("stored run task_id = %s, want %s (the winning body's task)", taskID, winnerTask)
		}
		row := readCommandRow(t, f, key)
		if row.State != string(runstore.CommandStateSucceeded) {
			t.Errorf("stored state = %s, want succeeded", row.State)
		}
		if row.RequestHash != hashA && row.RequestHash != hashB {
			t.Fatalf("stored request_hash = %s, want one of the two requests' hashes", row.RequestHash)
		}
		if winnerTask == commandTestTask && row.RequestHash != hashA {
			t.Errorf("body A won but the stored hash is %s (body A is %s)", row.RequestHash, hashA)
		}
		if winnerTask == "task-2" && row.RequestHash != hashB {
			t.Errorf("body B won but the stored hash is %s (body B is %s)", row.RequestHash, hashB)
		}
		if row.StatusCode == nil || *row.StatusCode != http.StatusAccepted {
			t.Errorf("stored status_code = %v, want 202", row.StatusCode)
		}
		if row.ResourceID == nil || *row.ResourceID != runID {
			t.Errorf("stored resource_id = %v, want %s", row.ResourceID, runID)
		}

		// The loser stays refused after the race, and the winner still replays.
		losingBody := bodyB
		if winnerTask == "task-2" {
			losingBody = bodyA
		}
		after := f.postRuns(key, losingBody)
		if after.Status != http.StatusConflict {
			t.Fatalf("the losing body after the race = %d, want 409 (body %s)", after.Status, after.Body)
		}
		requireErrorReason(t, after.Body, string(CodeIdempotencyKeyReused), "idempotency_key_reused")

		winningBody := bodyA
		if winnerTask == "task-2" {
			winningBody = bodyB
		}
		replay := f.postRuns(key, winningBody)
		requireStatus(t, replay, http.StatusAccepted)
		if !bytes.Equal(replay.Body, winnerBody) {
			t.Errorf("replay of the winning body = %s, want %s", replay.Body, winnerBody)
		}
		if n := f.runCount(); n != 1 {
			t.Errorf("runs after the replays = %d, want 1", n)
		}
		if n := f.commandRecordCount(); n != 1 {
			t.Errorf("command_records after the replays = %d, want 1", n)
		}
	})
}

// ---------------------------------------------------------------------------
// TestLostResponseCanQueryCommand
// ---------------------------------------------------------------------------

// TestLostResponseCanQueryCommand is §20.4's recovery path: the POST happened,
// the client never saw the answer, and the only thing it still holds is the key
// it chose. This test deliberately keeps only the raw bytes of the response —
// it never parses them into the answer it would have used — and then recovers
// through GET /commands/:command_id.
//
// The resource the endpoint names is verified against the run store directly
// (runstore.GetRun), so the test proves the Run exists rather than that two
// JSON documents mention the same string. The negative case and the rollback
// contrast are here too: a key that was never claimed is 404, and a callback
// that failed before recording anything leaves no command to find.
func TestLostResponseCanQueryCommand(t *testing.T) {
	t.Run("lost response", func(t *testing.T) {
		f := newCommandFixture(t, createRunMount)
		key := "rq_lost_0001"
		body := `{"task_id":"` + f.task + `"}`
		runID := "run-" + key

		// The response arrives and is immediately "lost": only the bytes survive,
		// as a reference for the replay comparison below.
		lost := f.postRuns(key, body)
		requireStatus(t, lost, http.StatusAccepted)

		// Recovery through the reconciliation endpoint.
		recovered := requireStatus(t, f.getCommand(key), http.StatusOK)
		assertCommandStatusData(t, recovered.Data, CommandStatusApplied)
		if code, _ := recovered.Data["status_code"].(float64); int(code) != http.StatusAccepted {
			t.Errorf("data.status_code = %v, want 202", recovered.Data["status_code"])
		}
		if recovered.Meta["command_id"] != key {
			t.Errorf("meta.command_id = %v, want %s", recovered.Meta["command_id"], key)
		}
		resource, _ := recovered.Data["resource"].(map[string]any)
		if resource == nil {
			t.Fatalf("data.resource is missing: the client cannot find what the command created from %v", recovered.Data)
		}
		if resource["type"] != "run" || resource["id"] != runID {
			t.Fatalf("data.resource = %v, want run/%s", recovered.Data["resource"], runID)
		}

		// The named resource really exists, read through the run store rather
		// than through another endpoint.
		stored, err := runstore.GetRun(context.Background(), f.store.DB(), resource["id"].(string))
		if err != nil {
			t.Fatalf("runstore.GetRun(%v): %v", resource["id"], err)
		}
		if stored.ProjectID != f.project {
			t.Errorf("stored run project_id = %s, want %s", stored.ProjectID, f.project)
		}
		if stored.TaskID != f.task {
			t.Errorf("stored run task_id = %s, want %s", stored.TaskID, f.task)
		}
		if stored.CommandID == nil || *stored.CommandID != key {
			t.Errorf("stored run command_id = %v, want %s", stored.CommandID, key)
		}
		if stored.Status != run.RunStatusQueued || stored.Revision != 1 {
			t.Errorf("stored run = (%s, revision %d), want (queued, 1)", stored.Status, stored.Revision)
		}

		// What the endpoint reports is the recorded answer: the same status code
		// and the same data the client would have received. It is not a fresh
		// summary of the Run.
		lostDecoded := decodeAPIResponse(t, lost)
		if code, _ := recovered.Data["status_code"].(float64); int(code) != lost.Status {
			t.Errorf("data.status_code = %v, want the recorded %d", recovered.Data["status_code"], lost.Status)
		}
		if resource["id"] != lostDecoded.Data["run_id"] {
			t.Errorf("the resource names %v but the recorded answer created %v", resource["id"], lostDecoded.Data["run_id"])
		}

		// The client can also just send the same command again: the answer is
		// the recorded one, byte for byte, and no second Run appears.
		replay := f.postRuns(key, body)
		requireStatus(t, replay, http.StatusAccepted)
		if !bytes.Equal(lost.Body, replay.Body) {
			t.Errorf("replay body = %s, want the answer the client lost %s", replay.Body, lost.Body)
		}
		if n := f.runCount(); n != 1 {
			t.Errorf("runs = %d, want exactly 1", n)
		}
		if n := f.commandRecordCount(); n != 1 {
			t.Errorf("command_records = %d, want 1", n)
		}
	})

	t.Run("never used key", func(t *testing.T) {
		f := newCommandFixture(t, createRunMount)

		unused := f.getCommand("rq_lost_never_used")
		if unused.Status != http.StatusNotFound {
			t.Fatalf("a key that was never used = %d, want 404 (body %s)", unused.Status, unused.Body)
		}
		requireErrorReason(t, unused.Body, string(CodeInvalidRequest), reasonCommandNotFound)
		decoded := decodeAPIResponse(t, unused)
		if decoded.Error.Retryable {
			t.Error("command_not_found must not be retryable: the same request could not have succeeded")
		}
	})

	t.Run("callback failure leaves nothing to find", func(t *testing.T) {
		f := newCommandFixture(t, createRunWithInjectableFailure)
		key := "rq_lost_rollback"
		body := `{"task_id":"` + f.task + `"}`

		failed := f.doRaw(http.MethodPost, "/api/v1/projects/"+f.project+"/runs", []byte(body),
			map[string]string{HeaderIdempotencyKey: key, "X-Test-Fail": "after-side-effect"})
		if failed.Status != http.StatusInternalServerError {
			t.Fatalf("failed callback status = %d, want 500 (body %s)", failed.Status, failed.Body)
		}
		// This is the contrast with the lost-response case: the client cannot
		// recover through the command endpoint, because the failed transaction
		// rolled back the claim along with the side effect.
		gone := f.getCommand(key)
		if gone.Status != http.StatusNotFound {
			t.Fatalf("a rolled-back command = %d, want 404 (body %s)", gone.Status, gone.Body)
		}
		requireErrorReason(t, gone.Body, string(CodeInvalidRequest), reasonCommandNotFound)
		if n := f.runCount(); n != 0 {
			t.Fatalf("runs = %d after the rollback, want 0", n)
		}
		if n := f.commandRecordCount(); n != 0 {
			t.Fatalf("command_records = %d after the rollback, want 0: the key must be released", n)
		}

		// The key is free, so the retry executes: a plain error is not an answer.
		retry := f.postRuns(key, body)
		requireStatus(t, retry, http.StatusAccepted)
		if n := f.runCount(); n != 1 {
			t.Errorf("runs after the retry = %d, want 1", n)
		}
		decoded := requireStatus(t, f.getCommand(key), http.StatusOK)
		assertCommandStatusData(t, decoded.Data, CommandStatusApplied)
		resource, _ := decoded.Data["resource"].(map[string]any)
		if resource["type"] != "run" || resource["id"] != "run-"+key {
			t.Errorf("data.resource = %v, want run/%s", decoded.Data["resource"], "run-"+key)
		}
	})
}

// ---------------------------------------------------------------------------
// TestIfMatch412
// ---------------------------------------------------------------------------

// TestIfMatch412 pins §27.3's precondition as a real compare-and-swap on the Run
// row. It mounts the test-only cancel route, seeds one queued Run directly, and
// then drives every outcome the precondition has: a stale If-Match is 412 with
// the row and the event stream untouched, the right If-Match moves the row
// exactly once and stamps the new revision, the malformed forms are 422, a body
// expectation is 409, and two callers with the same correct If-Match produce
// exactly one state change.
//
// The failure body is compared against the production helper's own answer (see
// ifMatchStatusFor), so the recorded refusal and RespondRevisionConflict cannot
// drift apart without this test failing.
func TestIfMatch412(t *testing.T) {
	f := newCommandFixture(t, nil)
	// The route the production table does not have yet (T1.04.c owns the real
	// cancel). It is mounted here deliberately: this test is about the
	// precondition, and mounting it from the test file keeps the production
	// router out of the assertion.
	f.router.POST("/api/v1/projects/:id/runs/:run_id/cancel", func(c *gin.Context) { cancelRunMount(f, c) })
	runID := "run-cancel-checkpoint"
	insertQueuedRun(t, f, f.task, runID)
	reason := `{"reason":"superseded by a newer plan"}`

	t.Run("stale if-match is 412 and changes nothing", func(t *testing.T) {
		stale := f.postCancel("rq_cancel_stale_1", runID, reason, `"7"`)
		if stale.Status != http.StatusPreconditionFailed {
			t.Fatalf("stale If-Match = %d, want 412 (body %s)", stale.Status, stale.Body)
		}
		requireErrorReason(t, stale.Body, string(CodeConflict), reasonPreconditionFailed)
		details := decodedErrorDetails(t, stale.Body)
		if got, _ := details["run_id"].(string); got != runID {
			t.Errorf("details.run_id = %v, want %s", details["run_id"], runID)
		}
		if got, _ := details["expected_revision"].(float64); int64(got) != 7 {
			t.Errorf("details.expected_revision = %v, want 7", details["expected_revision"])
		}
		if got, _ := details["current_revision"].(float64); int64(got) != 1 {
			t.Errorf("details.current_revision = %v, want 1", details["current_revision"])
		}
		if got, _ := details["current_status"].(string); got != string(run.RunStatusQueued) {
			t.Errorf("details.current_status = %v, want queued", details["current_status"])
		}

		// The compare-and-swap failed for every row it touches: the run, the
		// event stream, and the command ledger.
		requireRunState(t, f, runID, string(run.RunStatusQueued), 1)
		total, state, byType := countEventsForRun(t, f, runID)
		if total != 0 || state != 0 {
			t.Errorf("events after a stale If-Match = %d (%v), want none", total, byType)
		}
		if n := f.commandRecordCount(); n != 1 {
			t.Errorf("command_records = %d, want 1: the refusal is recorded so a retry replays it", n)
		}
		row := readCommandRow(t, f, "rq_cancel_stale_1")
		if row.State != string(runstore.CommandStateFailed) {
			t.Errorf("stored state = %s, want failed", row.State)
		}
		if row.StatusCode == nil || *row.StatusCode != http.StatusPreconditionFailed {
			t.Errorf("stored status_code = %v, want 412", row.StatusCode)
		}

		// The 412 is recorded, so the same request replays it byte for byte
		// instead of re-running the CAS.
		replay := f.postCancel("rq_cancel_stale_1", runID, reason, `"7"`)
		if replay.Status != http.StatusPreconditionFailed {
			t.Fatalf("replay = %d, want 412 (body %s)", replay.Status, replay.Body)
		}
		if !bytes.Equal(stale.Body, replay.Body) {
			t.Errorf("replayed 412 = %s, want the recorded refusal %s", replay.Body, stale.Body)
		}
		if n := f.commandRecordCount(); n != 1 {
			t.Errorf("command_records after the replay = %d, want 1", n)
		}

		// The recorded refusal and the production helper's answer are the same
		// answer: same status, code, message and details. Only the meta object
		// differs, because each answer mints its own request id.
		if status := ifMatchStatusFor(t, &runstore.RevisionConflictError{
			RunID: runID, Expected: 7, Current: 1, CurrentStatus: run.RunStatusQueued}, true); status != http.StatusPreconditionFailed {
			t.Errorf("the helper answers the same conflict with %d, want 412", status)
		}
		requireRefusalMatchesHelper(t, f, "rq_cancel_stale_1", runID, reason, `"7"`,
			7, 1, run.RunStatusQueued, http.StatusPreconditionFailed, reasonPreconditionFailed)
	})

	t.Run("correct if-match succeeds once", func(t *testing.T) {
		key := "rq_cancel_ok_1"
		rec := f.doRawRecorder(http.MethodPost, "/api/v1/projects/"+f.project+"/runs/"+runID+"/cancel",
			[]byte(reason), map[string]string{HeaderIdempotencyKey: key, HeaderIfMatch: `"1"`})
		if rec.status() != http.StatusOK {
			t.Fatalf("correct If-Match = %d, want 200 (body %s)", rec.status(), rec.body())
		}
		if got := rec.header(HeaderETag); got != `"2"` {
			t.Errorf("ETag = %q, want %q (the revision the response represents)", got, `"2"`)
		}
		decoded := decodeAPIResponse(t, apiResponse{Status: rec.status(), Body: rec.body()})
		if decoded.Data["run_id"] != runID || decoded.Data["status"] != string(run.RunStatusCancelling) {
			t.Errorf("data = %v, want %s/cancelling", decoded.Data, runID)
		}
		if rev, _ := decoded.Data["revision"].(float64); int64(rev) != 2 {
			t.Errorf("data.revision = %v, want 2", decoded.Data["revision"])
		}

		requireRunState(t, f, runID, string(run.RunStatusCancelling), 2)
		total, state, byType := countEventsForRun(t, f, runID)
		if total != 1 || state != 1 {
			t.Fatalf("events after the cancel = %d (%d state, %v), want exactly one state event", total, state, byType)
		}
		if byType[string(run.EventRunCancelRequested)] != 1 {
			t.Errorf("event types = %v, want exactly one %s", byType, run.EventRunCancelRequested)
		}
		payload := eventPayloadForRun(t, f, runID, string(run.EventRunCancelRequested))
		wantPayload := map[string]any{
			"from_status":       string(run.RunStatusQueued),
			"to_status":         string(run.RunStatusCancelling),
			"trigger":           "command:run.cancel",
			"expected_terminal": string(run.RunStatusCancelled),
			"reason":            "superseded by a newer plan",
		}
		for field, want := range wantPayload {
			if payload[field] != want {
				t.Errorf("event payload %s = %v, want %v", field, payload[field], want)
			}
		}

		// The recorded command names the resource it moved, and a replay does
		// not move it again.
		row := readCommandRow(t, f, key)
		if row.State != string(runstore.CommandStateSucceeded) {
			t.Errorf("stored state = %s, want succeeded", row.State)
		}
		if row.ResourceType == nil || *row.ResourceType != "run" || row.ResourceID == nil || *row.ResourceID != runID {
			t.Errorf("stored resource = %v/%v, want run/%s", row.ResourceType, row.ResourceID, runID)
		}
		replay := f.postCancel(key, runID, reason, `"1"`)
		if replay.Status != http.StatusOK {
			t.Fatalf("replay = %d, want 200 (body %s)", replay.Status, replay.Body)
		}
		if !bytes.Equal(rec.body(), replay.Body) {
			t.Errorf("replayed body = %s, want %s", replay.Body, rec.body())
		}
		requireRunState(t, f, runID, string(run.RunStatusCancelling), 2)
		if _, state, _ := countEventsForRun(t, f, runID); state != 1 {
			t.Errorf("state events after the replay = %d, want 1", state)
		}
	})

	// The malformed and mismatched sources: all 422, none of them a 412, and
	// none of them moving the row. Every case gets a fresh key, so the
	// precondition checks are what refuse the request rather than a replay of an
	// earlier refusal under the same key.
	for index, tc := range []struct {
		name       string
		ifMatch    string
		body       string
		wantReason string
	}{
		{name: "weak tag", ifMatch: `W/"1"`, body: reason, wantReason: reasonInvalidIfMatch},
		{name: "tag list", ifMatch: `"1", "2"`, body: reason, wantReason: reasonInvalidIfMatch},
		{name: "unquoted", ifMatch: `1`, body: reason, wantReason: reasonInvalidIfMatch},
		{name: "zero", ifMatch: `"0"`, body: reason, wantReason: reasonInvalidIfMatch},
		{name: "non-numeric", ifMatch: `"abc"`, body: reason, wantReason: reasonInvalidIfMatch},
		{name: "header and body disagree", ifMatch: `"1"`, body: `{"reason":"x","expected_revision":2}`, wantReason: reasonIfMatchBodyMismatch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := f.postCancel(fmt.Sprintf("rq_cancel_bad_%d", index), runID, tc.body, tc.ifMatch)
			if response.Status != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422 (body %s)", response.Status, response.Body)
			}
			requireErrorReason(t, response.Body, string(CodeInvalidRequest), tc.wantReason)
			requireRunState(t, f, runID, string(run.RunStatusCancelling), 2)
			if _, state, _ := countEventsForRun(t, f, runID); state != 1 {
				t.Errorf("state events = %d, want 1 (a refused precondition must not move the run)", state)
			}
		})
	}

	t.Run("body expectation is 409", func(t *testing.T) {
		// The run is at revision 2 (cancelling) after the success above; a body
		// expectation of revision 1 is stale, and §27.3 answers a body source
		// with 409 rather than 412.
		body := `{"reason":"stale body","expected_revision":1}`
		if status := ifMatchStatusFor(t, &runstore.RevisionConflictError{
			RunID: runID, Expected: 1, Current: 2, CurrentStatus: run.RunStatusCancelling}, false); status != http.StatusConflict {
			t.Errorf("the helper answers a body expectation with %d, want 409", status)
		}
		requireRefusalMatchesHelper(t, f, "rq_cancel_body_1", runID, body, "",
			1, 2, run.RunStatusCancelling, http.StatusConflict, reasonRevisionConflict)
		requireRunState(t, f, runID, string(run.RunStatusCancelling), 2)
		if _, state, _ := countEventsForRun(t, f, runID); state != 1 {
			t.Errorf("state events = %d, want 1", state)
		}
	})

	t.Run("same if-match twice", func(t *testing.T) {
		// A run of its own, so the CAS can actually be won by one of the two
		// callers: both send revision 1, one moves it to 2, and the other must
		// lose. (A task can have only one non-terminal Run, so a second run
		// needs a second task.)
		raceRunID := "run-cancel-race"
		seedExtraTask(t, f, "task-cancel-race")
		insertQueuedRun(t, f, "task-cancel-race", raceRunID)
		keys := []string{"rq_cancel_race_a", "rq_cancel_race_b"}

		responses := concurrentResponses(2, func(index int) apiResponse {
			return f.postCancel(keys[index], raceRunID, `{"reason":"race"}`, `"1"`)
		})

		var ok, stale int
		for index, response := range responses {
			switch response.Status {
			case http.StatusOK:
				ok++
				if !bytes.Contains(response.Body, []byte(`"revision":2`)) {
					t.Errorf("winner %d answer %s does not report revision 2", index, response.Body)
				}
			case http.StatusPreconditionFailed:
				stale++
				requireErrorReason(t, response.Body, string(CodeConflict), reasonPreconditionFailed)
			default:
				t.Fatalf("caller %d = %d, want 200 or 412 (body %s)", index, response.Status, response.Body)
			}
		}
		if ok != 1 || stale != 1 {
			t.Fatalf("the two callers answered %d success and %d stale, want exactly 1 and 1", ok, stale)
		}
		requireRunState(t, f, raceRunID, string(run.RunStatusCancelling), 2)
		total, state, byType := countEventsForRun(t, f, raceRunID)
		if total != 1 || state != 1 {
			t.Errorf("events = %d (%d state, %v), want exactly one state event", total, state, byType)
		}
		if byType[string(run.EventRunCancelRequested)] != 1 {
			t.Errorf("event types = %v, want one %s", byType, run.EventRunCancelRequested)
		}
	})
}

// ---------------------------------------------------------------------------
// Helpers the four tests share
// ---------------------------------------------------------------------------

// insertQueuedRun writes the Run the cancel tests CAS, directly, at revision 1.
// The command ledger is not involved: these runs preexist the commands, which is
// what a cancel of an already-created run always looks like. The task is named
// explicitly because a task may hold only one non-terminal Run (§19.1).
func insertQueuedRun(t *testing.T, f *commandFixture, taskID, runID string) {
	t.Helper()
	created := run.Run{
		ID:        runID,
		TaskID:    taskID,
		ProjectID: f.project,
		BindingID: "binding-1", BindingRevision: 1,
		BaseManifestHash: "sha256:base", AgentRevisionID: "agent-rev-1",
		Budget:    run.Budget{Tokens: int64Ptr(50000)},
		Status:    run.RunStatusQueued,
		CreatedAt: time.UnixMilli(1700000000005),
		UpdatedAt: time.UnixMilli(1700000000006),
	}
	err := f.store.WithTx(context.Background(), func(ctx context.Context, tx runstore.Tx) error {
		hash, err := runstore.InsertInputSnapshot(ctx, tx, f.project, "snap-"+runID,
			json.RawMessage(`{"prompt":"cancel me"}`), time.UnixMilli(1700000000005))
		if err != nil {
			return err
		}
		created.InputSnapshotID = "snap-" + runID
		created.InputSnapshotHash = hash
		return runstore.InsertRun(ctx, tx, &created)
	})
	if err != nil {
		t.Fatalf("insert run %s: %v", runID, err)
	}
}

// decodedErrorDetails returns details of an error response, failing when there
// is no error branch.
func decodedErrorDetails(t *testing.T, body []byte) map[string]any {
	t.Helper()
	decoded := decodeAPIResponse(t, apiResponse{Body: body})
	if decoded.Error == nil {
		t.Fatalf("response has no error envelope: %s", body)
	}
	return decoded.Error.Details
}

// requireRefusalMatchesHelper sends one cancel request that loses the CAS, then
// asserts the recorded refusal is the answer the production helper gives for the
// same conflict — same status, code, message and details. The two can only
// differ in meta, because each answer mints its own request id.
//
// The comparison is the reason the route records a rejection instead of calling
// RespondRevisionConflict itself: this test would fail if the two shapes drifted.
func requireRefusalMatchesHelper(t *testing.T, f *commandFixture, key, runID, body, ifMatch string,
	expected, current int64, status run.RunStatus, wantStatus int, wantReason string) apiResponse {
	t.Helper()
	response := f.postCancel(key, runID, body, ifMatch)
	if response.Status != wantStatus {
		t.Fatalf("refused request = %d, want %d (body %s)", response.Status, wantStatus, response.Body)
	}
	recorded := requireErrorReason(t, response.Body, string(CodeConflict), wantReason)

	conflict := &runstore.RevisionConflictError{RunID: runID, Expected: expected, Current: current, CurrentStatus: status}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/probe", func(c *gin.Context) { RespondRevisionConflict(c, conflict, ifMatch != "") })
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/probe", nil))
	helper := decodeAPIResponse(t, apiResponse{Status: rec.Code, Body: rec.Body.Bytes()})
	if helper.Error == nil {
		t.Fatalf("RespondRevisionConflict answered no error: %s", rec.Body.Bytes())
	}
	if rec.Code != response.Status {
		t.Fatalf("the helper answers the same conflict with %d, the route answered %d", rec.Code, response.Status)
	}
	if recorded.Code != helper.Error.Code || recorded.Message != helper.Error.Message ||
		!reflect.DeepEqual(recorded.Details, helper.Error.Details) {
		t.Errorf("recorded refusal = %+v, helper = %+v", recorded, *helper.Error)
	}
	return response
}
