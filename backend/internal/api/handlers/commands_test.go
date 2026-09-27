package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/audit"
	"github.com/codeflow/backend/internal/run"
	"github.com/codeflow/backend/internal/runstore"
	"github.com/gin-gonic/gin"
)

// These tests cover commands.go and api_envelope.go (T1.11.b): the
// reconciliation endpoint, the reusable command helper and the If-Match /
// expected_revision precondition. They run against a real file-backed runstore
// — the same store class the production path uses, with WAL, real connections
// and the real migrations — because the properties under test are database
// properties: one side effect per key, a rollback releasing the key, a second
// connection's claim losing the race.
//
// The command callbacks insert a minimal Task-linked Run inside the command's
// own transaction, which is how T1.04's CreateRun is meant to use RunCommand.
// That is what makes "the key was claimed once" and "the Run was inserted once"
// the same assertion: the row cannot exist without the claim, and a second
// execution would have to insert it twice.

// commandTestClientKey is the client key most tests send. It is a constant so a
// test that accidentally compared two different keys fails loudly.
const commandTestClientKey = "rq_test_0001"

const (
	commandTestProject  = "11111111-1111-4111-8111-111111111111"
	commandTestProject2 = "22222222-2222-4222-8222-222222222222"
	commandTestTask     = "task-1"
)

// principalUnderTest is the principal those tests must use in direct store
// calls, spelled the way principalFromContext spells it.
const principalUnderTest = "user:sidecar-user"

// ---------------------------------------------------------------------------
// Fixture
// ---------------------------------------------------------------------------

// commandFixture is a router wired like production — real handlers, real
// middleware, real store — plus the store itself so a test can read rows and
// claim records directly.
type commandFixture struct {
	t       *testing.T
	store   *runstore.Store
	router  *gin.Engine
	project string
	task    string
}

// newCommandFixture builds the fixture. mount becomes the body of a test-only
// POST /api/v1/projects/:id/runs route, which is the shape T1.04's CreateRun
// will have; nil means the fixture only serves the reconciliation endpoint.
//
// The identity middleware is the one production uses, so the principal the
// handler derives is the principal production derives. A handler that read the
// principal from a header instead would not see it — which is the point.
func newCommandFixture(t *testing.T, mount func(f *commandFixture, c *gin.Context)) *commandFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)

	f := &commandFixture{t: t, project: commandTestProject, task: commandTestTask}
	store, _, err := runstore.OpenStore(context.Background(), filepath.Join(t.TempDir(), "codeflow.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	f.store = store
	seedProjectAndTask(t, store, f.project, f.task)

	f.router = gin.New()
	f.router.Use(gin.Recovery(), withSidecarIdentity())
	v1 := f.router.Group("/api/v1")
	v1.GET("/projects/:id/commands/:command_id", GetCommand)
	if mount != nil {
		v1.POST("/projects/:id/runs", func(c *gin.Context) { mount(f, c) })
	}

	SetCommandStore(store)
	t.Cleanup(func() { SetCommandStore(nil) })
	return f
}

// withSidecarIdentity injects the authenticated identity the sidecar-token
// middleware injects after a successful token check (middleware/auth.go). It is
// the only supported way for a request to be authenticated, and it keeps these
// tests from depending on a token value.
func withSidecarIdentity() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request = c.Request.WithContext(audit.WithActor(c.Request.Context(), audit.Actor{
			Type:   audit.ActorTypeUser,
			ID:     "sidecar-user",
			Source: "sidecar-token",
		}))
		c.Next()
	}
}

// seedProjectAndTask writes the project reference and one ready task, which is
// the least a run insert needs (the runs table's composite foreign key requires
// the task to exist and belong to the same project).
func seedProjectAndTask(t *testing.T, store *runstore.Store, project, taskID string) {
	t.Helper()
	task := run.Task{
		ID:        taskID,
		ProjectID: project,
		Title:     "add tests",
		Kind:      run.TaskKindCode,
		Status:    run.TaskStatusReady,
		Priority:  3,
		InputJSON: `{"prompt":"add tests"}`,
		CreatedAt: time.UnixMilli(1700000000000),
		UpdatedAt: time.UnixMilli(1700000000001),
	}
	err := store.WithTx(context.Background(), func(ctx context.Context, tx runstore.Tx) error {
		if err := runstore.UpsertProjectRef(ctx, tx, run.ProjectRef{
			ProjectID:    project,
			SnapshotHash: "sha256:project",
			State:        run.ProjectRefStateActive,
			CapturedAt:   time.UnixMilli(1700000000000),
			VerifiedAt:   time.UnixMilli(1700000000001),
		}); err != nil {
			return err
		}
		return runstore.InsertTask(ctx, tx, &task)
	})
	if err != nil {
		t.Fatalf("seed project %s and task %s: %v", project, taskID, err)
	}
}

// createRunMount is the stand-in for T1.04's CreateRun: it claims the key and
// inserts a snapshot and a Run in the *same* transaction, so the side effect and
// the claim commit or roll back together.
func createRunMount(f *commandFixture, c *gin.Context) {
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
			return CommandResult{
				StatusCode:   http.StatusAccepted,
				Data:         map[string]any{"run_id": runID},
				ResourceType: "run",
				ResourceID:   runID,
			}, nil
		})
}

// ---------------------------------------------------------------------------
// Request helpers
// ---------------------------------------------------------------------------

type apiResponse struct {
	Status int
	Body   []byte
}

// decodedBody is the union of both envelope branches, so a test can assert on
// whichever one the response used.
type decodedBody struct {
	Data  map[string]any `json:"data"`
	Error *decodedError  `json:"error"`
	Meta  map[string]any `json:"meta"`
}

type decodedError struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Retryable bool           `json:"retryable"`
	Details   map[string]any `json:"details"`
}

func (f *commandFixture) doRaw(method, target string, raw []byte, headers map[string]string) apiResponse {
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
	return apiResponse{Status: rec.Code, Body: rec.Body.Bytes()}
}

// postRuns sends the create-run request. An empty clientKey omits the
// Idempotency-Key header, an empty rawBody sends no body.
func (f *commandFixture) postRuns(clientKey, rawBody string) apiResponse {
	f.t.Helper()
	headers := map[string]string{}
	if clientKey != "" {
		headers[HeaderIdempotencyKey] = clientKey
	}
	return f.doRaw(http.MethodPost, "/api/v1/projects/"+f.project+"/runs", []byte(rawBody), headers)
}

func (f *commandFixture) getCommand(commandID string) apiResponse {
	f.t.Helper()
	return f.doRaw(http.MethodGet, "/api/v1/projects/"+f.project+"/commands/"+commandID, nil, nil)
}

func decodeAPIResponse(t *testing.T, response apiResponse) decodedBody {
	t.Helper()
	var decoded decodedBody
	if err := json.Unmarshal(response.Body, &decoded); err != nil {
		t.Fatalf("decode response %s: %v", response.Body, err)
	}
	return decoded
}

// requireStatus fails with the body so a wrong status is diagnosable.
func requireStatus(t *testing.T, response apiResponse, want int) decodedBody {
	t.Helper()
	if response.Status != want {
		t.Fatalf("status = %d, want %d (body %s)", response.Status, want, response.Body)
	}
	return decodeAPIResponse(t, response)
}

// assertCommandStatusData checks the CommandStatus schema's required fields and
// the expected status word.
func assertCommandStatusData(t *testing.T, data map[string]any, wantStatus string) {
	t.Helper()
	if data == nil {
		t.Fatal("response has no data object")
	}
	if id, _ := data["command_id"].(string); id == "" {
		t.Error("data.command_id is missing; it is required by the schema")
	}
	if data["status"] != wantStatus {
		t.Errorf("data.status = %v, want %s", data["status"], wantStatus)
	}
}

// runCount counts the runs the callbacks inserted, which is the side-effect
// counter every idempotency assertion is about.
func (f *commandFixture) runCount() int {
	f.t.Helper()
	var n int
	if err := f.store.DB().QueryRow(`SELECT count(*) FROM runs`).Scan(&n); err != nil {
		f.t.Fatalf("count runs: %v", err)
	}
	return n
}

func (f *commandFixture) commandRecordCount() int {
	f.t.Helper()
	var n int
	if err := f.store.DB().QueryRow(`SELECT count(*) FROM command_records`).Scan(&n); err != nil {
		f.t.Fatalf("count command_records: %v", err)
	}
	return n
}

// ---------------------------------------------------------------------------
// GET reconciliation
// ---------------------------------------------------------------------------

// TestGetCommandReconciliationStates walks one key through every state the
// endpoint can answer with, checking the response shape at each step.
func TestGetCommandReconciliationStates(t *testing.T) {
	f := newCommandFixture(t, nil)
	hash := testRequestHash(t)

	// 404: never used.
	response := f.getCommand(commandTestClientKey)
	if response.Status != http.StatusNotFound {
		t.Fatalf("unused key status = %d, want 404 (body %s)", response.Status, response.Body)
	}
	decoded := decodeAPIResponse(t, response)
	if decoded.Error == nil || decoded.Error.Code != string(CodeInvalidRequest) {
		t.Fatalf("unused key error = %+v, want code invalid_request", decoded.Error)
	}
	if decoded.Error.Retryable {
		t.Error("command_not_found must not be retryable: the same request cannot succeed")
	}
	if reason, _ := decoded.Error.Details["reason"].(string); reason != reasonCommandNotFound {
		t.Errorf("details.reason = %v, want %s", decoded.Error.Details["reason"], reasonCommandNotFound)
	}
	if id, _ := decoded.Meta["request_id"].(string); id == "" {
		t.Error("meta.request_id is missing; it is required by the schema")
	}

	// accepted: claimed, still in flight.
	key := testCommandKey("runs.create", commandTestClientKey)
	if _, owner, err := claimCommand(f.store, key, hash); err != nil || !owner {
		t.Fatalf("claim = owner %v, err %v", owner, err)
	}
	decoded = requireStatus(t, f.getCommand(commandTestClientKey), http.StatusOK)
	assertCommandStatusData(t, decoded.Data, CommandStatusAccepted)
	if decoded.Data["operation"] != "runs.create" {
		t.Errorf("data.operation = %v, want runs.create", decoded.Data["operation"])
	}
	if decoded.Meta["command_id"] != commandTestClientKey {
		t.Errorf("meta.command_id = %v, want %s", decoded.Meta["command_id"], commandTestClientKey)
	}
	if _, present := decoded.Data["status_code"]; present {
		t.Errorf("data.status_code present on an in-flight command: %v", decoded.Data)
	}
	if _, present := decoded.Data["expired"]; present {
		t.Errorf("data.expired present on a live command: %v", decoded.Data)
	}

	// reconciling: the owner reported an unknown external outcome.
	if _, err := markReconciling(f.store, key, hash, "peer timeout"); err != nil {
		t.Fatalf("MarkCommandReconcilingTx: %v", err)
	}
	decoded = requireStatus(t, f.getCommand(commandTestClientKey), http.StatusOK)
	assertCommandStatusData(t, decoded.Data, CommandStatusReconciling)

	// applied: completed with a 2xx.
	if _, err := completeCommandTx(f.store, key, hash, runstore.CommandStateSucceeded, http.StatusAccepted,
		`{"data":{"run_id":"run-1"},"meta":{"request_id":"req-1"}}`); err != nil {
		t.Fatalf("CompleteCommandTx: %v", err)
	}
	decoded = requireStatus(t, f.getCommand(commandTestClientKey), http.StatusOK)
	assertCommandStatusData(t, decoded.Data, CommandStatusApplied)
	if code, _ := decoded.Data["status_code"].(float64); int(code) != http.StatusAccepted {
		t.Errorf("data.status_code = %v, want 202", decoded.Data["status_code"])
	}
	if completed, _ := decoded.Data["completed_at"].(string); completed == "" {
		t.Error("data.completed_at is missing on a terminal command")
	}
	if created, _ := decoded.Data["created_at"].(string); created == "" {
		t.Error("data.created_at is missing on a stored command")
	}

	// rejected: a recorded failure keeps its status.
	rejected := testCommandKey("runs.create", "rq_rejected_0001")
	if _, _, err := claimCommand(f.store, rejected, hash); err != nil {
		t.Fatalf("claim rejected command: %v", err)
	}
	if _, err := completeCommandTx(f.store, rejected, hash, runstore.CommandStateFailed, http.StatusConflict,
		`{"error":{"code":"conflict","message":"no","retryable":false},"meta":{"request_id":"req-2"}}`); err != nil {
		t.Fatalf("CompleteCommandTx (failed): %v", err)
	}
	decoded = requireStatus(t, f.getCommand("rq_rejected_0001"), http.StatusOK)
	assertCommandStatusData(t, decoded.Data, CommandStatusRejected)
	if code, _ := decoded.Data["status_code"].(float64); int(code) != http.StatusConflict {
		t.Errorf("data.status_code = %v, want 409", decoded.Data["status_code"])
	}

	// expired, 2xx: the tombstone keeps the outcome and the status code, drops
	// the body, and says so.
	expired := testCommandKey("runs.create", "rq_expired_0001")
	if _, _, err := claimCommand(f.store, expired, hash); err != nil {
		t.Fatalf("claim expiring command: %v", err)
	}
	if _, err := completeCommandTx(f.store, expired, hash, runstore.CommandStateSucceeded, http.StatusCreated,
		`{"data":{"run_id":"run-old"},"meta":{"request_id":"req-3"}}`); err != nil {
		t.Fatalf("CompleteCommandTx (expiring): %v", err)
	}
	expireAll(t, f.store)
	decoded = requireStatus(t, f.getCommand("rq_expired_0001"), http.StatusOK)
	assertCommandStatusData(t, decoded.Data, CommandStatusApplied)
	if expiredFlag, _ := decoded.Data["expired"].(bool); !expiredFlag {
		t.Error("data.expired must be true on a tombstone")
	}
	if _, present := decoded.Data["response"]; present {
		t.Errorf("a tombstone must not carry a response body: %v", decoded.Data)
	}
	if code, _ := decoded.Data["status_code"].(float64); int(code) != http.StatusCreated {
		t.Errorf("data.status_code = %v, want 201 (the recorded outcome survives expiry)", decoded.Data["status_code"])
	}

	// expired, 4xx: the same tombstone projects as rejected.
	expiredFailed := testCommandKey("runs.create", "rq_expired_failed")
	if _, _, err := claimCommand(f.store, expiredFailed, hash); err != nil {
		t.Fatalf("claim expiring failed command: %v", err)
	}
	if _, err := completeCommandTx(f.store, expiredFailed, hash, runstore.CommandStateFailed, http.StatusUnprocessableEntity,
		`{"error":{"code":"invalid_request","message":"no","retryable":false},"meta":{"request_id":"req-4"}}`); err != nil {
		t.Fatalf("CompleteCommandTx (expiring failed): %v", err)
	}
	expireAll(t, f.store)
	decoded = requireStatus(t, f.getCommand("rq_expired_failed"), http.StatusOK)
	assertCommandStatusData(t, decoded.Data, CommandStatusRejected)
}

// TestGetCommandAmbiguousAndOperationSelection covers the multi-operation case:
// 409 with the operation list, then the named operation answers.
func TestGetCommandAmbiguousAndOperationSelection(t *testing.T) {
	f := newCommandFixture(t, nil)
	hash := testRequestHash(t)
	for _, operation := range []string{"runs.cancel", "runs.create"} {
		if _, owner, err := claimCommand(f.store, testCommandKey(operation, "rq_ambiguous_1"), hash); err != nil || !owner {
			t.Fatalf("claim %s = owner %v, err %v", operation, owner, err)
		}
	}

	target := "/api/v1/projects/" + f.project + "/commands/rq_ambiguous_1"
	decoded := requireStatus(t, f.doRaw(http.MethodGet, target, nil, nil), http.StatusConflict)
	if decoded.Error == nil || decoded.Error.Code != string(CodeConflict) {
		t.Fatalf("ambiguous error = %+v, want code conflict", decoded.Error)
	}
	if reason, _ := decoded.Error.Details["reason"].(string); reason != reasonAmbiguousCommand {
		t.Errorf("details.reason = %v, want %s", decoded.Error.Details["reason"], reasonAmbiguousCommand)
	}
	operations, ok := decoded.Error.Details["operations"].([]any)
	if !ok || len(operations) != 2 {
		t.Fatalf("details.operations = %v, want the two operations", decoded.Error.Details["operations"])
	}
	// Ordered by operation, so a client can diff the answer.
	if operations[0] != "runs.cancel" || operations[1] != "runs.create" {
		t.Errorf("details.operations = %v, want [runs.cancel runs.create]", operations)
	}
	if decoded.Error.Retryable {
		t.Error("ambiguous_command must not be retryable as-is: the client must choose an operation")
	}

	decoded = requireStatus(t, f.doRaw(http.MethodGet, target+"?operation=runs.create", nil, nil), http.StatusOK)
	assertCommandStatusData(t, decoded.Data, CommandStatusAccepted)
	if decoded.Data["operation"] != "runs.create" {
		t.Errorf("selected data = %v, want the runs.create record", decoded.Data)
	}

	// An operation with no record is answered like a key that was never used,
	// not with the list of operations that do exist.
	decoded = requireStatus(t, f.doRaw(http.MethodGet, target+"?operation=runs.retry", nil, nil), http.StatusNotFound)
	if reason, _ := decoded.Error.Details["reason"].(string); reason != reasonCommandNotFound {
		t.Errorf("details.reason = %v, want %s", decoded.Error.Details["reason"], reasonCommandNotFound)
	}
}

// TestGetCommandScopeIsolation proves the lookup is scoped by BOTH the
// authenticated principal and the path's project: a foreign key answers exactly
// like a key that was never used.
func TestGetCommandScopeIsolation(t *testing.T) {
	f := newCommandFixture(t, nil)
	hash := testRequestHash(t)

	foreignPrincipal := testCommandKey("runs.create", "rq_foreign_principal")
	foreignPrincipal.PrincipalID = "user:someone-else"
	foreignProject := testCommandKey("runs.create", "rq_foreign_project")
	foreignProject.ProjectID = commandTestProject2
	for _, key := range []runstore.CommandKey{foreignPrincipal, foreignProject} {
		if _, owner, err := claimCommand(f.store, key, hash); err != nil || !owner {
			t.Fatalf("claim %v = owner %v, err %v", key, owner, err)
		}
	}

	unused := requireStatus(t, f.getCommand("rq_never_used"), http.StatusNotFound)
	for _, commandID := range []string{"rq_foreign_principal", "rq_foreign_project"} {
		decoded := requireStatus(t, f.getCommand(commandID), http.StatusNotFound)
		if decoded.Error == nil {
			t.Fatalf("%s: response has no error envelope", commandID)
		}
		// Same code, message and reason: the only difference a caller can see is
		// the request id, so the endpoint cannot be used to confirm that another
		// scope's key exists.
		if decoded.Error.Code != unused.Error.Code {
			t.Errorf("%s: code = %s, want %s", commandID, decoded.Error.Code, unused.Error.Code)
		}
		if fmt.Sprint(decoded.Error.Details["reason"]) != fmt.Sprint(unused.Error.Details["reason"]) {
			t.Errorf("%s: reason = %v, want %v", commandID, decoded.Error.Details["reason"], unused.Error.Details["reason"])
		}
		if decoded.Error.Message != unused.Error.Message {
			t.Errorf("%s: message = %q, want %q", commandID, decoded.Error.Message, unused.Error.Message)
		}
	}
}

// TestGetCommandUnconfiguredStoreIs503 covers the deployment state: no store
// installed is a retryable 503, not a 500 and not a panic.
func TestGetCommandUnconfiguredStoreIs503(t *testing.T) {
	SetCommandStore(nil)
	t.Cleanup(func() { SetCommandStore(nil) })

	rec := httptest.NewRecorder()
	bareCommandRouter().ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/v1/projects/"+commandTestProject+"/commands/rq_1", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured store status = %d, want 503 (body %s)", rec.Code, rec.Body.String())
	}
	decoded := decodeAPIResponse(t, apiResponse{Status: rec.Code, Body: rec.Body.Bytes()})
	if decoded.Error == nil || decoded.Error.Code != string(CodeBackendUnavailable) {
		t.Fatalf("error = %+v, want backend_unavailable", decoded.Error)
	}
	if !decoded.Error.Retryable {
		t.Error("backend_unavailable must be retryable")
	}
	if reason, _ := decoded.Error.Details["reason"].(string); reason != reasonRunStoreUnavailable {
		t.Errorf("details.reason = %v, want %s", decoded.Error.Details["reason"], reasonRunStoreUnavailable)
	}
}

// TestGetCommandUnauthenticatedAndInvalidParams covers the boundary cases: no
// identity is 401, a malformed project id is the 400 the shared validator gives,
// and an unusable command id is 422 rather than a 404 that would read as "this
// key was never used".
func TestGetCommandUnauthenticatedAndInvalidParams(t *testing.T) {
	f := newCommandFixture(t, nil)

	// The fixture always injects an identity, so the unauthenticated case needs
	// a router without that middleware.
	rec := httptest.NewRecorder()
	bareCommandRouter().ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/v1/projects/"+f.project+"/commands/rq_1", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no identity status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
	}
	decoded := decodeAPIResponse(t, apiResponse{Status: rec.Code, Body: rec.Body.Bytes()})
	if decoded.Error == nil || decoded.Error.Code != string(CodeUnauthorized) {
		t.Errorf("error = %+v, want unauthorized", decoded.Error)
	}

	// A project id that is not a UUID is refused before any lookup, in the 3.0
	// error envelope: a v1 client parses {error, meta}, never the pre-3.0
	// {success, error} shape.
	decoded = requireStatus(t, f.doRaw(http.MethodGet, "/api/v1/projects/not-a-uuid/commands/rq_1", nil, nil), http.StatusBadRequest)
	if decoded.Error == nil || decoded.Error.Code != string(CodeInvalidRequest) {
		t.Fatalf("invalid project id error = %+v, want invalid_request in the 3.0 envelope", decoded.Error)
	}
	if reason, _ := decoded.Error.Details["reason"].(string); reason != reasonInvalidProjectID {
		t.Errorf("invalid project id details.reason = %v, want %s", decoded.Error.Details["reason"], reasonInvalidProjectID)
	}
	if id, _ := decoded.Meta["request_id"].(string); id == "" {
		t.Error("invalid project id answer has no meta.request_id")
	}

	// A blank or padded key can never have been recorded (a write refuses it),
	// and it is answered as an unusable key rather than trimmed into another one.
	for _, key := range []string{"%20", "%20rq_1", "rq_1%20"} {
		decoded = requireStatus(t, f.getCommand(key), http.StatusUnprocessableEntity)
		if decoded.Error == nil || decoded.Error.Code != string(CodeInvalidRequest) {
			t.Fatalf("command id %q error = %+v, want invalid_request", key, decoded.Error)
		}
		if reason, _ := decoded.Error.Details["reason"].(string); reason != reasonIdempotencyKeyInvalid {
			t.Errorf("command id %q details.reason = %v, want %s", key, decoded.Error.Details["reason"], reasonIdempotencyKeyInvalid)
		}
	}

	// A command id the ledger would refuse is a request error, not an absence.
	decoded = requireStatus(t, f.getCommand(strings.Repeat("k", 200)), http.StatusUnprocessableEntity)
	if decoded.Error == nil || decoded.Error.Code != string(CodeInvalidRequest) {
		t.Fatalf("over-long command id error = %+v, want invalid_request", decoded.Error)
	}
	if reason, _ := decoded.Error.Details["reason"].(string); reason != reasonIdempotencyKeyInvalid {
		t.Errorf("details.reason = %v, want %s", decoded.Error.Details["reason"], reasonIdempotencyKeyInvalid)
	}
}

// bareCommandRouter serves GetCommand with no identity middleware and no other
// routes, for the cases the fixture cannot express.
func bareCommandRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/v1/projects/:id/commands/:command_id", GetCommand)
	return router
}

// ---------------------------------------------------------------------------
// RunCommand: idempotency
// ---------------------------------------------------------------------------

// TestRunCommandSameKeySameBodyReplays is the core §27.3 property: the same key
// with the same body executes once and replays the recorded answer.
func TestRunCommandSameKeySameBodyReplays(t *testing.T) {
	f := newCommandFixture(t, createRunMount)
	body := `{"task_id":"` + f.task + `"}`

	first := f.postRuns(commandTestClientKey, body)
	firstDecoded := requireStatus(t, first, http.StatusAccepted)
	if firstDecoded.Data["run_id"] == nil {
		t.Fatalf("first response has no run_id: %v", firstDecoded.Data)
	}
	if firstDecoded.Meta["command_id"] != commandTestClientKey {
		t.Errorf("meta.command_id = %v, want %s", firstDecoded.Meta["command_id"], commandTestClientKey)
	}

	second := f.postRuns(commandTestClientKey, body)
	requireStatus(t, second, http.StatusAccepted)
	if !bytes.Equal(first.Body, second.Body) {
		t.Errorf("replay body = %s, want the recorded answer %s byte for byte", second.Body, first.Body)
	}
	if f.runCount() != 1 {
		t.Errorf("runs = %d, want exactly 1 side effect", f.runCount())
	}

	// The reconciliation endpoint answers the same state and names the resource.
	decoded := requireStatus(t, f.getCommand(commandTestClientKey), http.StatusOK)
	assertCommandStatusData(t, decoded.Data, CommandStatusApplied)
	resource, _ := decoded.Data["resource"].(map[string]any)
	if resource["type"] != "run" || resource["id"] != "run-"+commandTestClientKey {
		t.Errorf("data.resource = %v, want run/run-%s", decoded.Data["resource"], commandTestClientKey)
	}
}

// TestRunCommandSameKeyDifferentBodyIs409 covers §27.3's reuse rule: the stored
// answer belongs to another request and is never returned for this one.
func TestRunCommandSameKeyDifferentBodyIs409(t *testing.T) {
	f := newCommandFixture(t, createRunMount)

	requireStatus(t, f.postRuns(commandTestClientKey, `{"task_id":"`+f.task+`"}`), http.StatusAccepted)

	// A different body under the same key. The task id is a real one, so the
	// only thing wrong with this request is the key reuse.
	response := f.postRuns(commandTestClientKey, `{"task_id":"task-2"}`)
	if response.Status != http.StatusConflict {
		t.Fatalf("reused key status = %d, want 409 (body %s)", response.Status, response.Body)
	}
	decoded := decodeAPIResponse(t, response)
	if decoded.Error == nil || decoded.Error.Code != string(CodeIdempotencyKeyReused) {
		t.Fatalf("error = %+v, want idempotency_key_reused", decoded.Error)
	}
	if decoded.Error.Retryable {
		t.Error("idempotency_key_reused must not be retryable with the same key")
	}
	if decoded.Data != nil {
		t.Errorf("a reused key must not receive the first command's data: %v", decoded.Data)
	}
	if f.runCount() != 1 {
		t.Errorf("runs = %d, want 1 (the refused request must not execute)", f.runCount())
	}
}

// TestRunCommandConcurrentSameKeySingleSideEffect is the concurrency proof:
// several callers sending the same key at once produce exactly one Run, and the
// losers observe the key as active instead of starting a second side effect.
func TestRunCommandConcurrentSameKeySingleSideEffect(t *testing.T) {
	f := newCommandFixture(t, createRunMount)
	body := []byte(`{"task_id":"` + f.task + `"}`)

	const callers = 8
	var (
		wg       sync.WaitGroup
		start    = make(chan struct{})
		statuses = make([]int, callers)
		bodies   = make([][]byte, callers)
	)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			response := f.doRaw(http.MethodPost, "/api/v1/projects/"+f.project+"/runs", body,
				map[string]string{HeaderIdempotencyKey: commandTestClientKey})
			statuses[index] = response.Status
			bodies[index] = response.Body
		}(i)
	}
	close(start)
	wg.Wait()

	for index, status := range statuses {
		if status != http.StatusAccepted {
			t.Fatalf("caller %d status = %d, want 202 (body %s); all statuses %v",
				index, status, bodies[index], statuses)
		}
	}
	if f.runCount() != 1 {
		t.Errorf("runs = %d, want exactly 1 despite %d concurrent requests", f.runCount(), callers)
	}
	if f.commandRecordCount() != 1 {
		t.Errorf("command_records = %d, want 1", f.commandRecordCount())
	}
}

// TestRunCommandCallbackErrorReleasesKey proves the rollback contract: a failed
// callback leaves no claim, so the side effect, the key and the ledger row are
// all gone and the same key can be tried again.
func TestRunCommandCallbackErrorReleasesKey(t *testing.T) {
	var failNext atomic.Bool
	failNext.Store(true)
	f := newCommandFixture(t, func(f *commandFixture, c *gin.Context) {
		RunCommand(c, f.store, CommandSpec{Operation: "runs.create", ProjectID: c.Param("id"),
			Body: map[string]any{"task_id": f.task}},
			func(ctx context.Context, tx runstore.Tx) (CommandResult, error) {
				if failNext.Load() {
					return CommandResult{}, errors.New("side effect failed")
				}
				key := c.GetHeader(HeaderIdempotencyKey)
				created := run.Run{
					ID: "run-retry", TaskID: f.task, ProjectID: c.Param("id"), CommandID: &key,
					BindingID: "binding-1", BindingRevision: 1,
					BaseManifestHash: "sha256:base", AgentRevisionID: "agent-rev-1",
					Budget:    run.Budget{Tokens: int64Ptr(100)},
					Status:    run.RunStatusQueued,
					CreatedAt: time.UnixMilli(1700000000003), UpdatedAt: time.UnixMilli(1700000000004),
				}
				if err := insertRunWithSnapshot(ctx, tx, c.Param("id"), "snap-retry", &created); err != nil {
					return CommandResult{}, err
				}
				return CommandResult{StatusCode: http.StatusAccepted,
					Data: map[string]any{"run_id": "run-retry"}}, nil
			})
	})

	body := `{"task_id":"` + f.task + `"}`
	failed := f.postRuns("rq_rollback_1", body)
	if failed.Status != http.StatusInternalServerError {
		t.Fatalf("failed callback status = %d, want 500 (body %s)", failed.Status, failed.Body)
	}
	if f.runCount() != 0 {
		t.Errorf("runs after a rolled-back callback = %d, want 0", f.runCount())
	}
	if f.commandRecordCount() != 0 {
		t.Fatalf("command_records = %d, want 0: the rollback must release the key", f.commandRecordCount())
	}

	// The same key now succeeds, because the rollback released it.
	failNext.Store(false)
	requireStatus(t, f.postRuns("rq_rollback_1", body), http.StatusAccepted)
	if f.runCount() != 1 {
		t.Errorf("runs after the retry = %d, want 1", f.runCount())
	}
}

// TestRunCommandAnswerIsRecordedWithTheSideEffect: the claim, the side effect
// and the recorded answer are one transaction. When the answer cannot be
// recorded — here the callback's response echoes a credential, which the ledger
// refuses to store — the side effect is rolled back with it and the key is
// released. Recording the answer in a later transaction would leave a committed
// Run behind a key stuck in_flight, answered 202 to every retry forever.
func TestRunCommandAnswerIsRecordedWithTheSideEffect(t *testing.T) {
	var calls atomic.Int32
	f := newCommandFixture(t, func(f *commandFixture, c *gin.Context) {
		key := c.GetHeader(HeaderIdempotencyKey)
		RunCommand(c, f.store, CommandSpec{Operation: "runs.create", ProjectID: c.Param("id"), Body: map[string]string{"task_id": f.task}},
			func(ctx context.Context, tx runstore.Tx) (CommandResult, error) {
				calls.Add(1)
				runID := "run-" + key
				created := run.Run{
					ID: runID, TaskID: f.task, ProjectID: c.Param("id"),
					CommandID: &key,
					BindingID: "binding-1", BindingRevision: 1,
					BaseManifestHash: "sha256:base", AgentRevisionID: "agent-rev-1",
					Budget:    run.Budget{Tokens: int64Ptr(50000)},
					Status:    run.RunStatusQueued,
					CreatedAt: time.UnixMilli(1700000000003),
					UpdatedAt: time.UnixMilli(1700000000004),
				}
				if err := insertRunWithSnapshot(ctx, tx, c.Param("id"), "snap-"+key, &created); err != nil {
					return CommandResult{}, err
				}
				// The side effect is in the transaction; the answer is not storable.
				return CommandResult{StatusCode: http.StatusAccepted, Data: map[string]any{"run_id": runID, "api_key": "sk-leak"}}, nil
			})
	})

	response := f.postRuns(commandTestClientKey, `{"task_id":"`+f.task+`"}`)
	if response.Status < 400 {
		t.Fatalf("an answer the ledger refuses was reported as success: %d %s", response.Status, response.Body)
	}
	if bytes.Contains(response.Body, []byte("sk-leak")) {
		t.Fatalf("the refused credential reached the client: %s", response.Body)
	}
	if n := f.runCount(); n != 0 {
		t.Fatalf("runs = %d after the answer could not be recorded, want 0: the side effect must roll back with it", n)
	}
	if n := f.commandRecordCount(); n != 0 {
		t.Fatalf("command_records = %d, want 0: the key must be released, not left in_flight", n)
	}
	requireStatus(t, f.getCommand(commandTestClientKey), http.StatusNotFound)

	// The key is free again: a retry executes (and fails the same way) instead of
	// being told the command is still in progress.
	if retry := f.postRuns(commandTestClientKey, `{"task_id":"`+f.task+`"}`); retry.Status == http.StatusAccepted {
		t.Fatalf("retry after a rolled-back command answered 202 (%s): the key was left claimed", retry.Body)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("callback ran %d times, want 2 (first attempt + retry of a released key)", got)
	}
}

// TestRunCommandBusinessRejectionIsRecordedAndReplayed proves a
// *CommandRejection is an answer, not a failure: it is recorded, the key stays
// claimed, and a retry replays it without re-running the callback.
func TestRunCommandBusinessRejectionIsRecordedAndReplayed(t *testing.T) {
	var calls atomic.Int64
	f := newCommandFixture(t, func(f *commandFixture, c *gin.Context) {
		RunCommand(c, f.store, CommandSpec{Operation: "runs.create", ProjectID: c.Param("id"),
			Body: map[string]any{"task_id": f.task}},
			func(ctx context.Context, tx runstore.Tx) (CommandResult, error) {
				calls.Add(1)
				return CommandResult{}, &CommandRejection{
					StatusCode: http.StatusConflict,
					Code:       CodeConflict,
					Message:    "task is not ready",
					Details:    map[string]any{"reason": "task_not_ready"},
				}
			})
	})

	body := `{"task_id":"` + f.task + `"}`
	first := f.postRuns("rq_reject_1", body)
	if first.Status != http.StatusConflict {
		t.Fatalf("rejection status = %d, want 409 (body %s)", first.Status, first.Body)
	}
	decoded := decodeAPIResponse(t, first)
	if decoded.Error == nil || decoded.Error.Code != string(CodeConflict) || decoded.Error.Message != "task is not ready" {
		t.Fatalf("rejection body = %s, want the recorded refusal", first.Body)
	}
	if reason, _ := decoded.Error.Details["reason"].(string); reason != "task_not_ready" {
		t.Errorf("details.reason = %v, want task_not_ready", decoded.Error.Details["reason"])
	}

	second := f.postRuns("rq_reject_1", body)
	requireStatus(t, second, http.StatusConflict)
	if !bytes.Equal(first.Body, second.Body) {
		t.Errorf("replayed body = %s, want the recorded refusal %s byte for byte", second.Body, first.Body)
	}
	if calls.Load() != 1 {
		t.Errorf("callback calls = %d, want 1: a rejection must not re-execute", calls.Load())
	}

	// The reconciliation endpoint reports the refusal as rejected.
	decoded = requireStatus(t, f.getCommand("rq_reject_1"), http.StatusOK)
	assertCommandStatusData(t, decoded.Data, CommandStatusRejected)
	if code, _ := decoded.Data["status_code"].(float64); int(code) != http.StatusConflict {
		t.Errorf("data.status_code = %v, want 409", decoded.Data["status_code"])
	}
}

// TestRunCommandMissingIdempotencyKeyIs422 covers §20.4's "the client generates
// the key": a request without one is refused, never given a server-generated
// key, and a padded key is refused rather than trimmed into another key.
func TestRunCommandMissingIdempotencyKeyIs422(t *testing.T) {
	f := newCommandFixture(t, createRunMount)
	body := `{"task_id":"` + f.task + `"}`

	response := f.postRuns("", body)
	if response.Status != http.StatusUnprocessableEntity {
		t.Fatalf("missing key status = %d, want 422 (body %s)", response.Status, response.Body)
	}
	decoded := decodeAPIResponse(t, response)
	if decoded.Error == nil || decoded.Error.Code != string(CodeInvalidRequest) {
		t.Fatalf("error = %+v, want invalid_request", decoded.Error)
	}
	if reason, _ := decoded.Error.Details["reason"].(string); reason != reasonIdempotencyKeyRequired {
		t.Errorf("details.reason = %v, want %s", decoded.Error.Details["reason"], reasonIdempotencyKeyRequired)
	}

	response = f.postRuns(" rq_padded", body)
	if response.Status != http.StatusUnprocessableEntity {
		t.Fatalf("padded key status = %d, want 422 (body %s)", response.Status, response.Body)
	}
	decoded = decodeAPIResponse(t, response)
	if reason, _ := decoded.Error.Details["reason"].(string); reason != reasonIdempotencyKeyInvalid {
		t.Errorf("details.reason = %v, want %s", decoded.Error.Details["reason"], reasonIdempotencyKeyInvalid)
	}

	if f.runCount() != 0 {
		t.Errorf("runs = %d, want 0: nothing may execute without a usable key", f.runCount())
	}
}

// ---------------------------------------------------------------------------
// Two-phase execution
// ---------------------------------------------------------------------------

// TestBeginCommandUnknownOutcomeIsNotReprocessed is §20.4's reconciling rule:
// once the external side effect's outcome is unknown, every resend answers 202
// reconciling and the side effect is never started a second time.
func TestBeginCommandUnknownOutcomeIsNotReprocessed(t *testing.T) {
	var sideEffects atomic.Int64
	f := newCommandFixture(t, func(f *commandFixture, c *gin.Context) {
		pending, ok := BeginCommand(c, f.store, CommandSpec{Operation: "runs.create",
			ProjectID: c.Param("id"), Body: map[string]any{"task_id": f.task}})
		if !ok {
			return // BeginCommand answered: duplicate, not configured, or bad key.
		}
		sideEffects.Add(1)
		pending.MarkCommandUnknown(c, "process start returned no acknowledgement")
	})

	body := `{"task_id":"` + f.task + `"}`
	first := requireStatus(t, f.postRuns("rq_two_phase", body), http.StatusAccepted)
	assertCommandStatusData(t, first.Data, CommandStatusReconciling)

	for attempt := 0; attempt < 3; attempt++ {
		resend := requireStatus(t, f.postRuns("rq_two_phase", body), http.StatusAccepted)
		assertCommandStatusData(t, resend.Data, CommandStatusReconciling)
	}
	if sideEffects.Load() != 1 {
		t.Fatalf("external side effect calls = %d, want exactly 1", sideEffects.Load())
	}

	// The reconciliation endpoint answers the same, and the reason the owner
	// gave is still recorded for whoever reconciles it.
	decoded := requireStatus(t, f.getCommand("rq_two_phase"), http.StatusOK)
	assertCommandStatusData(t, decoded.Data, CommandStatusReconciling)

	record := getStoredCommand(t, f.store, testCommandKey("runs.create", "rq_two_phase"))
	if record.State != runstore.CommandStateReconciling {
		t.Errorf("stored state = %s, want reconciling", record.State)
	}
	if record.LastError == nil || !strings.Contains(*record.LastError, "no acknowledgement") {
		t.Errorf("stored LastError = %v, want the reconciling reason", record.LastError)
	}
	if record.StatusCode != nil || record.Response != nil {
		t.Errorf("a reconciling command must have no recorded answer, got %v / %s",
			record.StatusCode, record.Response)
	}
}

// TestFinishCommandReplaysRecordedResult covers the happy path of the two-phase
// form: once finished, a resend gets the recorded answer and the side effect
// count stays one.
func TestFinishCommandReplaysRecordedResult(t *testing.T) {
	var sideEffects atomic.Int64
	f := newCommandFixture(t, func(f *commandFixture, c *gin.Context) {
		pending, ok := BeginCommand(c, f.store, CommandSpec{Operation: "runs.create",
			ProjectID: c.Param("id"), Body: map[string]any{"task_id": f.task}})
		if !ok {
			return
		}
		sideEffects.Add(1)
		pending.FinishCommand(c, CommandResult{
			StatusCode:   http.StatusCreated,
			Data:         map[string]any{"run_id": "run-finished"},
			ResourceType: "run",
			ResourceID:   "run-finished",
		})
	})

	body := `{"task_id":"` + f.task + `"}`
	first := f.postRuns("rq_finish_1", body)
	firstDecoded := requireStatus(t, first, http.StatusCreated)
	if firstDecoded.Data["run_id"] != "run-finished" {
		t.Fatalf("first data = %v, want run-finished", firstDecoded.Data)
	}

	second := f.postRuns("rq_finish_1", body)
	requireStatus(t, second, http.StatusCreated)
	if !bytes.Equal(first.Body, second.Body) {
		t.Errorf("replay body = %s, want %s byte for byte", second.Body, first.Body)
	}
	if sideEffects.Load() != 1 {
		t.Errorf("side effect calls = %d, want 1", sideEffects.Load())
	}

	// A client that lost the response finds the created resource through the
	// reconciliation endpoint.
	decoded := requireStatus(t, f.getCommand("rq_finish_1"), http.StatusOK)
	assertCommandStatusData(t, decoded.Data, CommandStatusApplied)
	resource, _ := decoded.Data["resource"].(map[string]any)
	if resource["type"] != "run" || resource["id"] != "run-finished" {
		t.Errorf("data.resource = %v, want run/run-finished", decoded.Data["resource"])
	}
}

// TestFinishCommandWithRejectionIsRecorded covers the two-phase refusal: an
// external step said no, the refusal is the command's recorded answer, and a
// resend replays it instead of starting the external operation again.
func TestFinishCommandWithRejectionIsRecorded(t *testing.T) {
	var sideEffects atomic.Int64
	f := newCommandFixture(t, func(f *commandFixture, c *gin.Context) {
		pending, ok := BeginCommand(c, f.store, CommandSpec{Operation: "runs.create",
			ProjectID: c.Param("id"), Body: map[string]any{"task_id": f.task}})
		if !ok {
			return
		}
		sideEffects.Add(1)
		pending.FinishCommandWithRejection(c, &CommandRejection{
			StatusCode: http.StatusForbidden,
			Code:       CodeForbidden,
			Message:    "workspace is read-only",
		})
	})

	body := `{"task_id":"` + f.task + `"}`
	first := f.postRuns("rq_two_phase_reject", body)
	if first.Status != http.StatusForbidden {
		t.Fatalf("rejection status = %d, want 403 (body %s)", first.Status, first.Body)
	}
	decoded := decodeAPIResponse(t, first)
	if decoded.Error == nil || decoded.Error.Code != string(CodeForbidden) {
		t.Fatalf("error = %+v, want forbidden", decoded.Error)
	}

	second := f.postRuns("rq_two_phase_reject", body)
	if second.Status != http.StatusForbidden {
		t.Fatalf("replay status = %d, want 403 (body %s)", second.Status, second.Body)
	}
	if sideEffects.Load() != 1 {
		t.Errorf("side effect calls = %d, want 1", sideEffects.Load())
	}

	decoded = requireStatus(t, f.getCommand("rq_two_phase_reject"), http.StatusOK)
	assertCommandStatusData(t, decoded.Data, CommandStatusRejected)
}

// ---------------------------------------------------------------------------
// If-Match / expected_revision
// ---------------------------------------------------------------------------

// TestExpectedRevisionRules pins the header and body rules: a strong tag holding
// a positive integer, agreement between the two sources, and 422 for anything
// else (§27.3).
func TestExpectedRevisionRules(t *testing.T) {
	cases := []struct {
		name         string
		ifMatch      string
		bodyRevision *int64
		wantOK       bool
		wantRev      int64
		wantReason   string
		wantStatus   int
	}{
		{name: "valid if-match", ifMatch: `"7"`, wantOK: true, wantRev: 7},
		{name: "valid body", bodyRevision: int64Ptr(3), wantOK: true, wantRev: 3},
		{name: "agreeing sources", ifMatch: `"5"`, bodyRevision: int64Ptr(5), wantOK: true, wantRev: 5},
		{name: "disagreeing sources", ifMatch: `"5"`, bodyRevision: int64Ptr(6),
			wantReason: reasonIfMatchBodyMismatch, wantStatus: http.StatusUnprocessableEntity},
		{name: "weak tag", ifMatch: `W/"5"`, wantReason: reasonInvalidIfMatch, wantStatus: http.StatusUnprocessableEntity},
		{name: "unquoted", ifMatch: `5`, wantReason: reasonInvalidIfMatch, wantStatus: http.StatusUnprocessableEntity},
		{name: "non-numeric", ifMatch: `"abc"`, wantReason: reasonInvalidIfMatch, wantStatus: http.StatusUnprocessableEntity},
		{name: "zero", ifMatch: `"0"`, wantReason: reasonInvalidIfMatch, wantStatus: http.StatusUnprocessableEntity},
		{name: "negative", ifMatch: `"-1"`, wantReason: reasonInvalidIfMatch, wantStatus: http.StatusUnprocessableEntity},
		{name: "list", ifMatch: `"1", "2"`, wantReason: reasonInvalidIfMatch, wantStatus: http.StatusUnprocessableEntity},
		{name: "empty tag", ifMatch: `""`, wantReason: reasonInvalidIfMatch, wantStatus: http.StatusUnprocessableEntity},
		{name: "zero body revision", bodyRevision: int64Ptr(0),
			wantReason: reasonInvalidExpectedRev, wantStatus: http.StatusUnprocessableEntity},
		{name: "neither"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			router := gin.New()
			var gotRev int64
			var gotFromHeader, gotOK bool
			router.POST("/probe", func(c *gin.Context) {
				rev, fromHeader, ok := ExpectedRevision(c, tc.bodyRevision)
				gotRev, gotFromHeader, gotOK = rev, fromHeader, ok
				if !ok {
					return // ExpectedRevision answered.
				}
				c.JSON(http.StatusOK, gin.H{"revision": rev})
			})

			req := httptest.NewRequest(http.MethodPost, "/probe", strings.NewReader("{}"))
			if tc.ifMatch != "" {
				req.Header.Set(HeaderIfMatch, tc.ifMatch)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if tc.wantOK {
				if rec.Code != http.StatusOK {
					t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
				}
				if !gotOK || gotRev != tc.wantRev {
					t.Fatalf("revision = %d ok = %v, want %d", gotRev, gotOK, tc.wantRev)
				}
				if tc.ifMatch != "" && !gotFromHeader {
					t.Error("fromIfMatch = false for an If-Match request")
				}
				if tc.ifMatch == "" && tc.bodyRevision != nil && gotFromHeader {
					t.Error("fromIfMatch = true for a body-only request")
				}
				return
			}

			if gotOK {
				t.Fatalf("ok = true, want false (body %s)", rec.Body.String())
			}
			if tc.wantStatus != 0 && rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantReason == "" {
				return
			}
			decoded := decodeAPIResponse(t, apiResponse{Status: rec.Code, Body: rec.Body.Bytes()})
			if decoded.Error == nil {
				t.Fatalf("no error body for %s", tc.name)
			}
			if decoded.Error.Code != string(CodeInvalidRequest) {
				t.Errorf("code = %s, want invalid_request", decoded.Error.Code)
			}
			if reason, _ := decoded.Error.Details["reason"].(string); reason != tc.wantReason {
				t.Errorf("details.reason = %v, want %s", decoded.Error.Details["reason"], tc.wantReason)
			}
		})
	}
}

// TestRespondRevisionConflictStatuses pins the two failure shapes: a failed
// If-Match is 412 with precondition_failed, a body expectation is 409 with
// revision_conflict, and both carry the revisions when they are known.
func TestRespondRevisionConflictStatuses(t *testing.T) {
	runConflict := &runstore.RevisionConflictError{RunID: "run-1", Expected: 3, Current: 5, CurrentStatus: run.RunStatusRunning}
	taskConflict := &runstore.TaskRevisionConflictError{TaskID: "task-1", Expected: 2, Current: 4, CurrentStatus: run.TaskStatusQueued}

	cases := []struct {
		name         string
		err          error
		fromIfMatch  bool
		wantStatus   int
		wantReason   string
		wantExpected float64
		wantCurrent  float64
		wantIDKey    string
		wantID       string
	}{
		{name: "if-match run", err: runConflict, fromIfMatch: true, wantStatus: http.StatusPreconditionFailed,
			wantReason: reasonPreconditionFailed, wantExpected: 3, wantCurrent: 5, wantIDKey: "run_id", wantID: "run-1"},
		{name: "body run", err: runConflict, fromIfMatch: false, wantStatus: http.StatusConflict,
			wantReason: reasonRevisionConflict, wantExpected: 3, wantCurrent: 5, wantIDKey: "run_id", wantID: "run-1"},
		{name: "if-match task", err: taskConflict, fromIfMatch: true, wantStatus: http.StatusPreconditionFailed,
			wantReason: reasonPreconditionFailed, wantExpected: 2, wantCurrent: 4, wantIDKey: "task_id", wantID: "task-1"},
		{name: "body task", err: taskConflict, fromIfMatch: false, wantStatus: http.StatusConflict,
			wantReason: reasonRevisionConflict, wantExpected: 2, wantCurrent: 4, wantIDKey: "task_id", wantID: "task-1"},
		{name: "unknown error", err: errors.New("some other failure"), fromIfMatch: true,
			wantStatus: http.StatusPreconditionFailed, wantReason: reasonPreconditionFailed},
		{name: "wrapped run error", err: fmt.Errorf("cas: %w", runConflict), fromIfMatch: false,
			wantStatus: http.StatusConflict, wantReason: reasonRevisionConflict, wantExpected: 3, wantCurrent: 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			router := gin.New()
			router.POST("/probe", func(c *gin.Context) { RespondRevisionConflict(c, tc.err, tc.fromIfMatch) })
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/probe", nil))

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			decoded := decodeAPIResponse(t, apiResponse{Status: rec.Code, Body: rec.Body.Bytes()})
			if decoded.Error == nil {
				t.Fatal("no error body")
			}
			if decoded.Error.Code != string(CodeConflict) {
				t.Errorf("code = %s, want conflict", decoded.Error.Code)
			}
			if decoded.Error.Retryable {
				t.Error("a revision conflict must not be retryable without re-reading")
			}
			if reason, _ := decoded.Error.Details["reason"].(string); reason != tc.wantReason {
				t.Errorf("details.reason = %v, want %s", decoded.Error.Details["reason"], tc.wantReason)
			}
			if tc.wantExpected != 0 {
				if got, _ := decoded.Error.Details["expected_revision"].(float64); got != tc.wantExpected {
					t.Errorf("expected_revision = %v, want %v", decoded.Error.Details["expected_revision"], tc.wantExpected)
				}
				if got, _ := decoded.Error.Details["current_revision"].(float64); got != tc.wantCurrent {
					t.Errorf("current_revision = %v, want %v", decoded.Error.Details["current_revision"], tc.wantCurrent)
				}
			}
			if tc.wantIDKey != "" {
				if got, _ := decoded.Error.Details[tc.wantIDKey].(string); got != tc.wantID {
					t.Errorf("details.%s = %v, want %s", tc.wantIDKey, decoded.Error.Details[tc.wantIDKey], tc.wantID)
				}
			}
			if id, _ := decoded.Meta["request_id"].(string); id == "" {
				t.Error("meta.request_id is missing on a precondition failure")
			}
		})
	}
}

// TestSetRevisionETag pins the success side of the precondition: the response
// carries the revision in the exact form If-Match expects back.
func TestSetRevisionETag(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/probe", func(c *gin.Context) {
		SetRevisionETag(c, 42)
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	router.GET("/zero", func(c *gin.Context) {
		SetRevisionETag(c, 0)
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/probe", nil))
	if got := rec.Header().Get(HeaderETag); got != `"42"` {
		t.Errorf("ETag = %q, want %q", got, `"42"`)
	}
	// The stamp must be usable as its own precondition.
	rev, fromHeader, ok := parseExpectedRevisionForTest(t, `"42"`)
	if !ok || !fromHeader || rev != 42 {
		t.Errorf("ETag is not accepted back: rev = %d fromHeader = %v ok = %v", rev, fromHeader, ok)
	}

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/zero", nil))
	if got := rec.Header().Get(HeaderETag); got != "" {
		t.Errorf("ETag for revision 0 = %q, want no header", got)
	}
}

// ---------------------------------------------------------------------------
// Envelope contract
// ---------------------------------------------------------------------------

// TestErrorCodeSetMatchesSchema reads backend/schemas/error.schema.json and
// compares its enum with the Go constants. The closed set is the contract's most
// load-bearing list: a code added on one side and not the other is a client that
// cannot branch on a response it will receive.
func TestErrorCodeSetMatchesSchema(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "schemas", "error.schema.json"))
	if err != nil {
		t.Fatalf("read error.schema.json: %v", err)
	}
	var schema struct {
		Properties struct {
			Code struct {
				Enum []string `json:"enum"`
			} `json:"code"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("decode error.schema.json: %v", err)
	}
	if len(schema.Properties.Code.Enum) == 0 {
		t.Fatal("error.schema.json has no code enum; the test would pass vacuously")
	}
	if len(schema.Properties.Code.Enum) != 13 {
		t.Errorf("schema enum has %d values, want the closed 13 (§15 T0.03)", len(schema.Properties.Code.Enum))
	}
	if len(apiErrorCodes) != 13 {
		t.Errorf("apiErrorCodes has %d values, want the closed 13 (§15 T0.03)", len(apiErrorCodes))
	}

	inSchema := map[string]bool{}
	for _, code := range schema.Properties.Code.Enum {
		if inSchema[code] {
			t.Errorf("schema enum lists %q twice", code)
		}
		inSchema[code] = true
	}
	inGo := map[string]bool{}
	for _, code := range apiErrorCodes {
		if inGo[string(code)] {
			t.Fatalf("apiErrorCodes contains %q twice: %v", code, apiErrorCodes)
		}
		inGo[string(code)] = true
	}
	for code := range inSchema {
		if !inGo[code] {
			t.Errorf("schema code %q has no Go constant", code)
		}
	}
	for code := range inGo {
		if !inSchema[code] {
			t.Errorf("Go code %q is not in the schema enum", code)
		}
	}
}

// TestAPICodeValid pins the type-level guard: only the closed set is accepted,
// so a handler cannot emit a code no client knows.
func TestAPICodeValid(t *testing.T) {
	for _, code := range apiErrorCodes {
		if !code.Valid() {
			t.Errorf("%q is in apiErrorCodes but not Valid()", code)
		}
	}
	for _, code := range []APICode{"", "not_a_code", "command_not_found", "run_store_unavailable"} {
		if code.Valid() {
			t.Errorf("%q must not be a valid code (§15 T0.03 keeps command-layer reasons out of the enum)", code)
		}
	}
}

// TestRespondAPIErrorRejectsUnknownCode proves the guard is enforced at the
// boundary: an unknown code becomes a legal 500 rather than an invalid envelope.
func TestRespondAPIErrorRejectsUnknownCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/probe", func(c *gin.Context) {
		respondAPIError(c, http.StatusConflict, APICode("made_up"), "secret detail", false, nil)
	})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/probe", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	decoded := decodeAPIResponse(t, apiResponse{Status: rec.Code, Body: rec.Body.Bytes()})
	if decoded.Error == nil || !APICode(decoded.Error.Code).Valid() {
		t.Fatalf("error = %+v, want a legal code", decoded.Error)
	}
	if strings.Contains(rec.Body.String(), "secret detail") {
		t.Error("an unusable code must not leak the caller's message")
	}
}

// TestEnvelopesMatchResponseSchema checks the two envelope branches against the
// schema's rules: exactly one branch, always a meta object, and a non-empty
// request_id inside it.
func TestEnvelopesMatchResponseSchema(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/ok", func(c *gin.Context) {
		respondAPIData(c, http.StatusOK, map[string]any{"run_id": "run-1"}, "rq_1", 3)
	})
	router.GET("/fail", func(c *gin.Context) {
		respondAPIError(c, http.StatusNotFound, CodeInvalidRequest, "gone", false,
			apiErrorDetails("reason", reasonCommandNotFound))
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ok", nil))
	var success map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &success); err != nil {
		t.Fatalf("decode success envelope: %v", err)
	}
	if _, present := success["data"]; !present {
		t.Error("success envelope has no data")
	}
	if _, present := success["error"]; present {
		t.Error("success envelope carries an error key; the branches are mutually exclusive")
	}
	var meta APIMeta
	if raw, present := success["meta"]; !present {
		t.Error("success envelope has no meta")
	} else if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatalf("decode meta: %v", err)
	}
	if meta.RequestID == "" {
		t.Error("meta.request_id is empty; it is required and must be non-empty")
	}
	if meta.CommandID != "rq_1" {
		t.Errorf("meta.command_id = %q, want rq_1", meta.CommandID)
	}
	if meta.Revision == nil || *meta.Revision != 3 {
		t.Errorf("meta.revision = %v, want 3", meta.Revision)
	}

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/fail", nil))
	var failure map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &failure); err != nil {
		t.Fatalf("decode failure envelope: %v", err)
	}
	if _, present := failure["error"]; !present {
		t.Error("failure envelope has no error")
	}
	if _, present := failure["data"]; present {
		t.Error("failure envelope carries a data key")
	}
	if _, present := failure["meta"]; !present {
		t.Error("failure envelope has no meta")
	}
}

// TestAPIResponseMetaGeneratesRequestID covers the "no trace middleware" case: a
// handler mounted without Trace must still produce a usable meta object, because
// an empty request_id would make the envelope invalid rather than merely
// uninformative.
func TestAPIResponseMetaGeneratesRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/probe", func(c *gin.Context) {
		c.JSON(http.StatusOK, APIResponseMeta(c, "", 0))
	})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/probe", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var meta APIMeta
	if err := json.Unmarshal(rec.Body.Bytes(), &meta); err != nil {
		t.Fatalf("decode meta: %v", err)
	}
	if meta.RequestID == "" {
		t.Error("generated request_id is empty")
	}
	if meta.CommandID != "" || meta.Revision != nil {
		t.Errorf("meta = %+v, want only request_id", meta)
	}
}

// ---------------------------------------------------------------------------
// Helpers the assertions above share
// ---------------------------------------------------------------------------

func int64Ptr(v int64) *int64 { return &v }

// testCommandKey is the ledger key a test names directly.
func testCommandKey(operation, commandID string) runstore.CommandKey {
	return runstore.CommandKey{
		PrincipalID: principalUnderTest,
		ProjectID:   commandTestProject,
		Operation:   operation,
		CommandID:   commandID,
	}
}

// testRequestHash is a stable request hash for records a test writes directly.
// Its input is the route pattern the handler hashes, so the value is what a real
// request would produce.
func testRequestHash(t *testing.T) string {
	t.Helper()
	hash, err := runstore.CommandRequestHash(http.MethodPost, "/api/v1/projects/:id/runs", []byte(`{"task_id":"task-1"}`))
	if err != nil {
		t.Fatalf("CommandRequestHash: %v", err)
	}
	return hash
}

func claimCommand(store *runstore.Store, key runstore.CommandKey, hash string) (runstore.CommandRecord, bool, error) {
	var (
		record runstore.CommandRecord
		owner  bool
	)
	err := store.WithTx(context.Background(), func(ctx context.Context, tx runstore.Tx) error {
		var err error
		record, owner, err = runstore.ClaimCommandTx(ctx, tx, key, hash, time.Now().UTC())
		return err
	})
	return record, owner, err
}

func markReconciling(store *runstore.Store, key runstore.CommandKey, hash, reason string) (runstore.CommandRecord, error) {
	var record runstore.CommandRecord
	err := store.WithTx(context.Background(), func(ctx context.Context, tx runstore.Tx) error {
		var err error
		record, err = runstore.MarkCommandReconcilingTx(ctx, tx, key, hash, reason, time.Now().UTC())
		return err
	})
	return record, err
}

func completeCommandTx(store *runstore.Store, key runstore.CommandKey, hash string, state runstore.CommandState, status int, body string) (runstore.CommandRecord, error) {
	var record runstore.CommandRecord
	err := store.WithTx(context.Background(), func(ctx context.Context, tx runstore.Tx) error {
		var err error
		record, err = runstore.CompleteCommandTx(ctx, tx, key, hash, runstore.CommandOutcome{
			State:      state,
			StatusCode: status,
			Response:   json.RawMessage(body),
		}, 0, time.Now().UTC())
		return err
	})
	return record, err
}

func getStoredCommand(t *testing.T, store *runstore.Store, key runstore.CommandKey) runstore.CommandRecord {
	t.Helper()
	record, err := runstore.GetCommand(context.Background(), store.DB(), key)
	if err != nil {
		t.Fatalf("GetCommand %v: %v", key, err)
	}
	return record
}

// expireAll tombstones every terminal command, which is what the retention sweep
// does once the window passes.
func expireAll(t *testing.T, store *runstore.Store) {
	t.Helper()
	var expired int
	err := store.WithTx(context.Background(), func(ctx context.Context, tx runstore.Tx) error {
		var err error
		expired, err = runstore.ExpireCommandsTx(ctx, tx, time.Now().UTC().Add(365*24*time.Hour), 0)
		return err
	})
	if err != nil {
		t.Fatalf("ExpireCommandsTx: %v", err)
	}
	if expired == 0 {
		t.Fatal("ExpireCommandsTx tombstoned nothing; the fixture no longer exercises the expired state")
	}
}

// insertRunWithSnapshot writes the snapshot and the run a callback needs, in the
// callback's own transaction.
func insertRunWithSnapshot(ctx context.Context, tx runstore.Tx, projectID, snapshotID string, created *run.Run) error {
	hash, err := runstore.InsertInputSnapshot(ctx, tx, projectID, snapshotID,
		json.RawMessage(`{"prompt":"add tests"}`), time.UnixMilli(1700000000002))
	if err != nil {
		return err
	}
	created.InputSnapshotID = snapshotID
	created.InputSnapshotHash = hash
	return runstore.InsertRun(ctx, tx, created)
}

// parseExpectedRevisionForTest drives ExpectedRevision through a request, so the
// ETag round-trip assertion uses the same parser a handler does.
func parseExpectedRevisionForTest(t *testing.T, ifMatch string) (int64, bool, bool) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	var rev int64
	var fromHeader, ok bool
	router.GET("/probe", func(c *gin.Context) {
		rev, fromHeader, ok = ExpectedRevision(c, nil)
	})
	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.Header.Set(HeaderIfMatch, ifMatch)
	router.ServeHTTP(httptest.NewRecorder(), req)
	return rev, fromHeader, ok
}
