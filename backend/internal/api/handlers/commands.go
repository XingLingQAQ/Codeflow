// Command reconciliation and the reusable idempotency / revision helpers
// (plan §28 T1.11.b, §20.4, §27.3).
//
// This file is the HTTP half of the command ledger whose storage half is
// runstore/commands.go. It contains three things, in the order a reader needs
// them:
//
//  1. GetCommand: the reconciliation endpoint §20.4 names by route. A client
//     whose response was lost keeps the Idempotency-Key it generated *before*
//     sending and asks here; the server answers the recorded state
//     (accepted/applied/rejected/reconciling) or 404 command_not_found. The
//     lookup is scoped by the authenticated principal and the path's project,
//     so another principal's key is indistinguishable from an unused one.
//
//  2. RunCommand and its two-phase form (BeginCommand / FinishCommand /
//     MarkCommandUnknown): the reusable helper T1.04's CreateRun is meant to
//     consume. It owns the whole idempotency dance — read the Idempotency-Key
//     header, hash the request, claim the key in the same transaction as the
//     side effect, record the answer, replay it — so no handler writes that
//     logic a second time and no two handlers can disagree about what "same
//     request" means.
//
//  3. ExpectedRevision / RespondRevisionConflict: the If-Match and
//     expected_revision preconditions §27.3 requires ("请求同时带 If-Match 与
//     body expected_revision 时必须一致，否则 422"), with 412 for a failed
//     If-Match and 409 for a business revision conflict.
//
// What this file deliberately does not do: it does not reference a Run service,
// a project service or any other future component. The side effect is a
// callback the caller supplies, and the tests drive it with a callback that
// inserts a minimal Task/Run through the same transaction. That is what lets
// T1.04 wire CreateRun onto this helper without this card inventing the Run
// API.

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/codeflow/backend/internal/audit"
	"github.com/codeflow/backend/internal/runstore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// HeaderIdempotencyKey is the client-owned de-duplication key (§20.4). The
// server never generates one: a key the server invented is useless to a client
// that lost the response, because the client's own key is the only identifier
// it still holds. A request without this header is refused (422
// idempotency_key_required), never silently given one.
const HeaderIdempotencyKey = "Idempotency-Key"

// HeaderIfMatch is the optimistic-concurrency precondition header. §27.3 fixes
// its failure status at 412.
const HeaderIfMatch = "If-Match"

// HeaderETag carries the revision a successful response represents, in the
// strong-entity-tag form If-Match expects back.
const HeaderETag = "ETag"

// Details reasons. They live in details.reason and are deliberately *not* part
// of the closed error-code enum (§15 T0.03 namespace ruling): they tell an
// operator which condition fired, while error.code tells a client what to do.
const (
	reasonCommandNotFound        = "command_not_found"
	reasonAmbiguousCommand       = "ambiguous_command"
	reasonRunStoreUnavailable    = "run_store_unavailable"
	reasonInvalidProjectID       = "invalid_project_id"
	reasonIdempotencyKeyRequired = "idempotency_key_required"
	reasonIdempotencyKeyInvalid  = "idempotency_key_invalid"
	reasonInvalidIfMatch         = "invalid_if_match"
	reasonInvalidExpectedRev     = "invalid_expected_revision"
	reasonIfMatchBodyMismatch    = "if_match_body_mismatch"
	reasonPreconditionFailed     = "precondition_failed"
	reasonRevisionConflict       = "revision_conflict"
)

// Command status values: the §20.4 state vocabulary the reconciliation endpoint
// answers with. It is the closed 4-value enum of the CommandStatus schema, and
// it is a *projection* of runstore's five states — in_flight and reconciling are
// both active, but only one of them means "the outcome is unknown", so they map
// to different words.
const (
	CommandStatusAccepted    = "accepted"
	CommandStatusApplied     = "applied"
	CommandStatusRejected    = "rejected"
	CommandStatusReconciling = "reconciling"
)

// ---------------------------------------------------------------------------
// Store wiring
// ---------------------------------------------------------------------------

var (
	commandStoreMu sync.RWMutex
	commandStore   *runstore.Store
)

// SetCommandStore installs the runtime store the command endpoints use. It is
// called by bootstrap once the database is open; nil means "not configured",
// which the endpoints answer as 503 backend_unavailable rather than as a 500:
// an unconfigured store is a deployment state, not a bug, and the client's
// retry after the sidecar finishes starting is meaningful.
//
// The setter is a package-level function rather than a field on a struct
// because the handlers are registered as plain functions by router.go, the same
// shape the other service-backed handlers use. It is safe to call concurrently
// with a request.
func SetCommandStore(store *runstore.Store) {
	commandStoreMu.Lock()
	defer commandStoreMu.Unlock()
	commandStore = store
}

// GetCommandStore returns the installed store, or nil when none is configured.
func GetCommandStore() *runstore.Store {
	commandStoreMu.RLock()
	defer commandStoreMu.RUnlock()
	return commandStore
}

// ---------------------------------------------------------------------------
// GET /api/v1/projects/:id/commands/:command_id
// ---------------------------------------------------------------------------

// commandStatusData is the data payload of a reconciliation response. It is the
// CommandStatus schema: command_id and status are required, everything else is
// optional and omitted when it does not apply.
type commandStatusData struct {
	CommandID string `json:"command_id"`
	Status    string `json:"status"`
	// Operation names which command this is when the client key was used more
	// than once. It is always filled in when it is known, so a client that sent
	// ?operation= can verify the server answered about the operation it meant.
	Operation string `json:"operation,omitempty"`
	// StatusCode is the HTTP status the original request was (or would have
	// been) answered with. Present for terminal commands; a tombstone keeps it
	// while its body is gone.
	StatusCode *int `json:"status_code,omitempty"`
	// Resource names what the command created, when it created something.
	Resource *commandResourceRef `json:"resource,omitempty"`
	// Expired marks a tombstone: the recorded answer outlived its retention
	// window and the body is no longer available, so the client must not expect
	// one. status_code still says what the original outcome was.
	Expired bool `json:"expired,omitempty"`
	// CreatedAt/CompletedAt are RFC3339 UTC, matching every other timestamp the
	// 3.0 contract serialises.
	CreatedAt   string `json:"created_at,omitempty"`
	CompletedAt string `json:"completed_at,omitempty"`
}

// commandResourceRef is the resource a command created (§19.1's pair).
type commandResourceRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// GetCommand handles GET /api/v1/projects/:id/commands/:command_id.
//
// The route's :id is the project and :command_id is the client's
// Idempotency-Key. Because §27.3's scope includes the operation and the client
// often does not know it, the lookup returns every record for the key and:
//
//   - zero records → 404 invalid_request + details.reason=command_not_found,
//     retryable=false. The status is 404 because the resource is absent; the
//     code is invalid_request because the closed set has no "not found" code
//     and §15 T0.03 rules the command-layer reason into details.
//   - one record → 200 with its projected status.
//   - several records → the caller must disambiguate with ?operation=. Without
//     it: 409 conflict + details.reason=ambiguous_command and the operation
//     list, retryable=false (retrying the same request cannot help; the client
//     must choose). With it: the named record, or 404 if that operation has no
//     record for the key.
func GetCommand(c *gin.Context) {
	// Both path checks answer with the 3.0 error envelope: this is a v1
	// contract endpoint, and a client that parses {error, meta} must not get the
	// pre-3.0 {success, error} shape for a malformed path.
	projectID := c.Param("id")
	if _, err := uuid.Parse(projectID); err != nil {
		respondAPIError(c, http.StatusBadRequest, CodeInvalidRequest,
			"Project ID must be a UUID", false,
			apiErrorDetails("reason", reasonInvalidProjectID, "field", "id"))
		return
	}
	// The key is taken verbatim: a write refuses a padded or blank key, so such
	// a key can never have been recorded, and trimming it here would answer
	// about a different key than the one the client asked for.
	commandID := c.Param("command_id")
	if strings.TrimSpace(commandID) == "" || strings.TrimSpace(commandID) != commandID {
		respondAPIError(c, http.StatusUnprocessableEntity, CodeInvalidRequest,
			"command_id is not a usable idempotency key", false,
			apiErrorDetails("reason", reasonIdempotencyKeyInvalid, "field", "command_id"))
		return
	}

	store := GetCommandStore()
	if store == nil {
		respondAPIError(c, http.StatusServiceUnavailable, CodeBackendUnavailable,
			"Run store is not configured", true,
			apiErrorDetails("reason", reasonRunStoreUnavailable))
		return
	}

	principalID, ok := principalFromContext(c)
	if !ok {
		respondAPIError(c, http.StatusUnauthorized, CodeUnauthorized,
			"Authentication required", false, nil)
		return
	}

	records, err := runstore.FindCommandsByClientKey(c.Request.Context(), store.DB(), principalID, projectID, commandID)
	if err != nil {
		if errors.Is(err, runstore.ErrInvalidCommand) {
			// The path segment is not a usable client key (non-ASCII, padded,
			// over-long). That is a request problem, and answering 404 would
			// hide it: the client would believe the key was simply never used.
			respondAPIError(c, http.StatusUnprocessableEntity, CodeInvalidRequest,
				"command_id is not a usable idempotency key", false,
				apiErrorDetails("reason", reasonIdempotencyKeyInvalid))
			return
		}
		// A failed read is a dependency failure, not a server bug: 503 and
		// retryable, as the OpenAPI response for this endpoint declares.
		respondAPIError(c, http.StatusServiceUnavailable, CodeBackendUnavailable,
			"Failed to read command record", true, nil)
		return
	}

	record, ok := selectCommandRecord(c, records)
	if !ok {
		return
	}
	data := commandStatusFromRecord(record)
	respondAPIData(c, http.StatusOK, data, record.Key.CommandID, 0)
}

// selectCommandRecord applies the "one row, or disambiguate" rule and writes
// the failure response itself when there is nothing to answer with. It returns
// ok=false when it has already answered.
func selectCommandRecord(c *gin.Context, records []runstore.CommandRecord) (runstore.CommandRecord, bool) {
	switch len(records) {
	case 0:
		respondAPIError(c, http.StatusNotFound, CodeInvalidRequest,
			"No command record for this key", false,
			apiErrorDetails("reason", reasonCommandNotFound))
		return runstore.CommandRecord{}, false
	case 1:
		return records[0], true
	}

	operations := make([]string, 0, len(records))
	for _, record := range records {
		operations = append(operations, record.Key.Operation)
	}
	requested := strings.TrimSpace(c.Query("operation"))
	if requested == "" {
		respondAPIError(c, http.StatusConflict, CodeConflict,
			"This command key was used for several operations; retry with ?operation=", false,
			apiErrorDetails("reason", reasonAmbiguousCommand, "operations", operations))
		return runstore.CommandRecord{}, false
	}
	for _, record := range records {
		if record.Key.Operation == requested {
			return record, true
		}
	}
	// The named operation has no record under this key. That is the same
	// condition as a key that was never used, and it is answered the same way:
	// listing the operations that do exist would turn the endpoint into an
	// oracle for other clients' operations.
	respondAPIError(c, http.StatusNotFound, CodeInvalidRequest,
		"No command record for this key and operation", false,
		apiErrorDetails("reason", reasonCommandNotFound, "operation", requested))
	return runstore.CommandRecord{}, false
}

// commandStatusFromRecord projects a stored record onto the §20.4 status
// vocabulary:
//
//	in_flight   → accepted     (the owner is still working; do not execute again)
//	reconciling → reconciling  (an external side effect has an unknown outcome)
//	succeeded   → applied
//	failed      → rejected
//	expired     → applied/rejected by the recorded status code, with expired=true
//	              and no body, because the tombstone keeps the outcome but not
//	              the response (§27.3).
func commandStatusFromRecord(record runstore.CommandRecord) commandStatusData {
	data := commandStatusData{
		CommandID: record.Key.CommandID,
		Operation: record.Key.Operation,
		CreatedAt: formatAPITime(record.CreatedAt),
	}
	if record.ResourceType != nil && record.ResourceID != nil {
		data.Resource = &commandResourceRef{Type: *record.ResourceType, ID: *record.ResourceID}
	}

	switch record.State {
	case runstore.CommandStateInFlight:
		data.Status = CommandStatusAccepted
	case runstore.CommandStateReconciling:
		data.Status = CommandStatusReconciling
	case runstore.CommandStateSucceeded:
		data.Status = CommandStatusApplied
		data.StatusCode = record.StatusCode
		data.CompletedAt = formatAPITimePtr(record.CompletedAt)
	case runstore.CommandStateFailed:
		data.Status = CommandStatusRejected
		data.StatusCode = record.StatusCode
		data.CompletedAt = formatAPITimePtr(record.CompletedAt)
	case runstore.CommandStateExpired:
		// A tombstone: the response body is gone but the outcome is not. A 2xx
		// recorded status means the command had been applied.
		data.Expired = true
		data.StatusCode = record.StatusCode
		data.CompletedAt = formatAPITimePtr(record.CompletedAt)
		data.Status = CommandStatusRejected
		if record.StatusCode != nil && *record.StatusCode >= 200 && *record.StatusCode < 300 {
			data.Status = CommandStatusApplied
		}
	default:
		// Unreachable: CommandState's set is closed and validated on write. A
		// state outside it would be a schema violation, and reporting it as
		// "reconciling" is the safe projection — it tells the client not to
		// execute anything.
		data.Status = CommandStatusReconciling
	}
	return data
}

// formatAPITime renders a stored time as RFC3339 UTC, or "" for the zero time
// (which is omitted from the payload rather than serialised as year 1).
func formatAPITime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// formatAPITimePtr is the pointer form used by optional timestamps.
func formatAPITimePtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return formatAPITime(*t)
}

// ---------------------------------------------------------------------------
// Reusable command execution
// ---------------------------------------------------------------------------

// CommandSpec is what a handler tells RunCommand about the request it is
// executing. Operation is the ledger's operation name ("runs.create"); the
// canonical path and method come from the request itself, so the hash always
// describes the request that was actually made rather than a hand-written
// string that could drift from the route.
type CommandSpec struct {
	// Operation names the command in the ledger's vocabulary. It is part of the
	// idempotency scope, so it must be stable across releases for one route.
	Operation string
	// ProjectID is the project the command acts on. It is not read from the
	// path by the helper: a handler that has already resolved and authorised
	// the project passes what it resolved.
	ProjectID string
	// Body is the de-identified request structure the hash covers. It must not
	// contain a credential: the hash is stored, and a stored hash of a secret is
	// still key material. A body naming a secret field is refused by the store.
	Body any
}

// CommandResult is what a successful command produced. StatusCode and Data are
// the response the client is given now and the response a later retry replays,
// which is why they are stored: the replay must be byte-identical to the first
// answer.
type CommandResult struct {
	// StatusCode is the HTTP status of the response (201, 202, 200, ...).
	StatusCode int
	// Data is the success payload. It is stored as the command's recorded
	// response, so it must be the whole answer — a retry gets this and nothing
	// else.
	Data any
	// ResourceType/ResourceID name what the command created, when it created
	// something. Both empty means "no resource".
	ResourceType string
	ResourceID   string
}

// CommandRejection is a business refusal: the request was understood, the
// caller was authorised, and the answer is a refusal the client should see
// replayed on a retry rather than re-executed. It is recorded as a `failed`
// command with its status and body.
//
// A *system* failure (the database refused, a dependency is down) is a plain
// error instead: its transaction rolls back, so the key is released and the
// client may retry the same key. That distinction is the whole point of having
// two types — §27.3's "事务先占 key，只有拥有者产生副作用" only holds if a
// rolled-back attempt leaves no claim behind.
type CommandRejection struct {
	// StatusCode is the HTTP status to answer with (409, 422, 403, ...).
	StatusCode int
	// Code is the closed-set error code.
	Code APICode
	// Message is the human-readable text.
	Message string
	// Details is the optional structured context.
	Details map[string]any
}

// Error implements error so a callback can return a rejection like any other
// failure.
func (r *CommandRejection) Error() string {
	if r == nil {
		return "command rejected"
	}
	return fmt.Sprintf("command rejected: %s (%s)", r.Message, r.Code)
}

// commandIdentity is the resolved (principal, key, hash) triple RunCommand
// executes under. It is built once and used by both the single-phase and the
// two-phase form, so the two cannot disagree about what the key is.
type commandIdentity struct {
	Key  runstore.CommandKey
	Hash string
}

// RunCommand executes one idempotent write command.
//
// The contract, in the order it happens:
//
//  1. The Idempotency-Key header is required and must be a usable key. Missing
//     → 422 invalid_request + details.reason=idempotency_key_required;
//     unusable → 422 + idempotency_key_invalid. The server never generates a
//     key (§20.4).
//  2. The request hash is computed from the method, the canonical path and
//     spec.Body.
//  3. One transaction claims the key. The owner — and only the owner — runs fn
//     inside that same transaction and records fn's answer in it too, so the
//     claim, the side effect and the recorded answer commit or roll back as one:
//     a crash can never leave a side effect whose answer was not recorded (a
//     key stuck in_flight forever, answered 202 to every retry). A non-owner
//     reads the existing record and answers from it: active → 202 with the
//     projected status (only a two-phase command is ever visible while active);
//     terminal → the recorded status code and body, verbatim; tombstone → the
//     recorded status code with no body.
//  4. If fn returns a *CommandRejection the refusal is recorded as `failed` and
//     answered. If it returns any other error — or its answer cannot be
//     recorded (the store refuses a response that echoes a credential) — the
//     transaction rolls back: the key is released because the side effect was
//     rolled back too, and the client may retry the same key.
//  5. A key claimed for a different request hash is 409
//     idempotency_key_reused — never the first command's answer.
//
// fn must not perform an external side effect it cannot roll back: this
// transaction can be rolled back after fn returns, and a side effect that
// already left the process cannot. A command whose outcome depends on such an
// effect uses the two-phase form (BeginCommand / FinishCommand /
// MarkCommandUnknown) instead.
func RunCommand(c *gin.Context, store *runstore.Store, spec CommandSpec, fn func(ctx context.Context, tx runstore.Tx) (CommandResult, error)) {
	if store == nil {
		respondAPIError(c, http.StatusServiceUnavailable, CodeBackendUnavailable,
			"Run store is not configured", true,
			apiErrorDetails("reason", reasonRunStoreUnavailable))
		return
	}
	identity, ok := buildCommandIdentity(c, spec)
	if !ok {
		return
	}
	principalID, ok := principalFromContext(c)
	if !ok {
		respondAPIError(c, http.StatusUnauthorized, CodeUnauthorized,
			"Authentication required", false, nil)
		return
	}
	identity.Key.PrincipalID = principalID

	var (
		record runstore.CommandRecord
		owner  bool
	)
	err := store.WithTx(c.Request.Context(), func(ctx context.Context, tx runstore.Tx) error {
		_, owns, err := runstore.ClaimCommandTx(ctx, tx, identity.Key, identity.Hash, nowUTC())
		if err != nil {
			return err
		}
		owner = owns
		if !owns {
			// Someone else's record: answer from it, do not execute.
			return nil
		}
		produced, err := fn(ctx, tx)
		if err != nil {
			var refusal *CommandRejection
			if !errors.As(err, &refusal) {
				return err
			}
			// A business refusal is part of the command's recorded answer, so it
			// is stored in the same transaction and the key stays claimed: a
			// retry replays the refusal instead of re-running.
			status, body := rejectionResponse(c, identity.Key.CommandID, refusal)
			record, err = recordCommandOutcomeTx(ctx, tx, identity, runstore.CommandStateFailed, status, body, "", "")
			return err
		}
		status, body := successResponse(c, identity.Key.CommandID, produced)
		record, err = recordCommandOutcomeTx(ctx, tx, identity, runstore.CommandStateSucceeded, status, body,
			produced.ResourceType, produced.ResourceID)
		return err
	})

	if err != nil {
		handleCommandStoreError(c, identity.Key, err)
		return
	}

	if !owner {
		replayCommand(c, store, identity)
		return
	}
	writeRecordedResponse(c, record)
}

// successResponse builds the recorded answer of a command that succeeded. An
// out-of-range status is a handler bug and is answered as 200 rather than
// recorded as something no HTTP client could read.
func successResponse(c *gin.Context, commandID string, result CommandResult) (int, APISuccessEnvelope) {
	status := result.StatusCode
	if status < 100 || status > 599 {
		status = http.StatusOK
	}
	return status, APISuccessEnvelope{
		Data: result.Data,
		Meta: APIResponseMeta(c, commandID, 0),
	}
}

// rejectionResponse builds the recorded answer of a business refusal. A code
// outside the closed set is a handler bug: it is recorded as a 500
// backend_unavailable without the handler's text, never as a code the contract
// does not have. A nil rejection is answered as a plain 409 conflict.
func rejectionResponse(c *gin.Context, commandID string, rejection *CommandRejection) (int, APIErrorEnvelope) {
	if rejection == nil {
		rejection = &CommandRejection{StatusCode: http.StatusConflict, Code: CodeConflict, Message: "command rejected"}
	}
	status := rejection.StatusCode
	if status < 100 || status > 599 {
		status = http.StatusConflict
	}
	body := APIErrorEnvelope{
		Error: APIErrorBody{
			Code:      rejection.Code,
			Message:   rejection.Message,
			Retryable: false,
			Details:   rejection.Details,
		},
		Meta: APIResponseMeta(c, commandID, 0),
	}
	if !rejection.Code.Valid() {
		status = http.StatusInternalServerError
		body.Error = APIErrorBody{Code: CodeBackendUnavailable, Message: "internal error", Retryable: false}
	}
	return status, body
}

// recordCommandOutcomeTx records the answer of a command the caller owns inside
// the caller's transaction and returns the stored record, whose Response is the
// canonical form of body: the caller answers with those bytes, so the first
// response and every later replay are the same bytes and a client cannot tell
// them apart.
func recordCommandOutcomeTx(ctx context.Context, tx runstore.Tx, identity commandIdentity, state runstore.CommandState, statusCode int, body any, resourceType, resourceID string) (runstore.CommandRecord, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return runstore.CommandRecord{}, fmt.Errorf("encode command response: %w", err)
	}
	return runstore.CompleteCommandTx(ctx, tx, identity.Key, identity.Hash, runstore.CommandOutcome{
		State:        state,
		StatusCode:   statusCode,
		Response:     json.RawMessage(encoded),
		ResourceType: resourceType,
		ResourceID:   resourceID,
	}, 0, nowUTC())
}

// completeCommand records the outcome of a two-phase command in its own
// transaction: the claim committed in BeginCommand, and the external side
// effect happened outside any transaction, so this is the first moment the
// answer is known.
//
// A failure here is reported to the caller as an error response while the
// command stays in_flight, so a retry finds it active and does not execute the
// external side effect again; the command is then reconciled rather than re-run.
func completeCommand(c *gin.Context, store *runstore.Store, identity commandIdentity, state runstore.CommandState, statusCode int, body any, resourceType, resourceID string) (runstore.CommandRecord, error) {
	var record runstore.CommandRecord
	err := store.WithTx(c.Request.Context(), func(ctx context.Context, tx runstore.Tx) error {
		var err error
		record, err = recordCommandOutcomeTx(ctx, tx, identity, state, statusCode, body, resourceType, resourceID)
		return err
	})
	if err != nil {
		return runstore.CommandRecord{}, err
	}
	return record, nil
}

// replayCommand answers a duplicate request from the record the owner left
// behind. It never executes anything.
func replayCommand(c *gin.Context, store *runstore.Store, identity commandIdentity) {
	record, err := runstore.GetCommand(c.Request.Context(), store.DB(), identity.Key)
	if err != nil {
		if errors.Is(err, runstore.ErrNotFound) {
			// The claim committed but the row is gone: impossible under the
			// schema (nothing deletes a command record), so report it as an
			// unavailable dependency rather than inventing an answer.
			respondAPIError(c, http.StatusServiceUnavailable, CodeBackendUnavailable,
				"Command record is unavailable", true, nil)
			return
		}
		handleCommandStoreError(c, identity.Key, err)
		return
	}

	switch record.State {
	case runstore.CommandStateInFlight, runstore.CommandStateReconciling:
		// The original request is still being processed (or its outcome is
		// unknown). 202 with the projected status: the client must poll the
		// reconciliation endpoint, not send the command again.
		respondAPIData(c, http.StatusAccepted, commandStatusFromRecord(record), record.Key.CommandID, 0)
	case runstore.CommandStateSucceeded, runstore.CommandStateFailed:
		// Replay the recorded answer: same status code, same bytes. The first
		// answer was written from the same stored body, so the two are identical.
		writeRecordedResponse(c, record)
	case runstore.CommandStateExpired:
		// The tombstone keeps the outcome but not the body (§27.3). Answering
		// with a body nobody is allowed to keep would be worse than answering
		// with the status and the fact that it expired.
		status := http.StatusOK
		if record.StatusCode != nil {
			status = *record.StatusCode
		}
		respondAPIData(c, status, commandStatusFromRecord(record), record.Key.CommandID, 0)
	default:
		respondAPIError(c, http.StatusServiceUnavailable, CodeBackendUnavailable,
			"Command record is in an unusable state", true, nil)
	}
}

// writeRecordedResponse writes a stored status code and body as-is. The body is
// already a complete envelope in canonical form, so it is sent without
// re-encoding: re-encoding could reorder fields, and the point of a replay is
// that the client receives the same bytes it would have received the first time.
// Both the first answer and every replay go through here, so that holds by
// construction rather than by two code paths agreeing.
func writeRecordedResponse(c *gin.Context, record runstore.CommandRecord) {
	status := http.StatusOK
	if record.StatusCode != nil {
		status = *record.StatusCode
	}
	if len(record.Response) == 0 {
		respondAPIError(c, http.StatusServiceUnavailable, CodeBackendUnavailable,
			"Command record has no response body", true, nil)
		return
	}
	c.Data(status, "application/json; charset=utf-8", record.Response)
}

// handleCommandStoreError maps a store error onto the response contract.
func handleCommandStoreError(c *gin.Context, key runstore.CommandKey, err error) {
	switch {
	case errors.Is(err, runstore.ErrCommandKeyReused):
		// §27.3: same key, different body. The stored answer belongs to another
		// request and must never be returned for this one.
		respondAPIError(c, http.StatusConflict, CodeIdempotencyKeyReused,
			"This idempotency key was already used for a different request", false,
			apiErrorDetails("reason", "idempotency_key_reused", "command_id", key.CommandID))
	case errors.Is(err, runstore.ErrCommandAlreadyCompleted):
		// The command finished while this request was racing it. The recorded
		// answer is the truth; the client should reconcile.
		respondAPIError(c, http.StatusConflict, CodeConflict,
			"Command already completed; read its recorded result", false,
			apiErrorDetails("reason", "command_already_completed", "command_id", key.CommandID))
	case errors.Is(err, runstore.ErrCommandClaimRaced):
		respondAPIError(c, http.StatusServiceUnavailable, CodeBackendUnavailable,
			"Command key was claimed concurrently; retry", true, nil)
	case errors.Is(err, runstore.ErrInvalidCommand):
		respondAPIError(c, http.StatusUnprocessableEntity, CodeInvalidRequest,
			"The command could not be recorded as given", false,
			apiErrorDetails("reason", reasonIdempotencyKeyInvalid))
	case errors.Is(err, runstore.ErrInvalidRecord), errors.Is(err, runstore.ErrSecretInSnapshot):
		respondAPIError(c, http.StatusUnprocessableEntity, CodeInvalidRequest,
			"The request could not be executed as given", false, nil)
	default:
		respondAPIError(c, http.StatusInternalServerError, CodeBackendUnavailable,
			"Command execution failed", true, nil)
	}
}

// ---------------------------------------------------------------------------
// Two-phase execution: an external side effect whose outcome can be unknown
// ---------------------------------------------------------------------------

// PendingCommand is a claimed command whose side effect has not happened yet.
type PendingCommand struct {
	identity commandIdentity
	store    *runstore.Store
}

// BeginCommand claims a command's key in its own transaction and commits the
// claim as in_flight, so the key is taken before the external side effect
// starts.
//
// This is the form §20.4 requires for an operation that can leave the process:
// a command whose effect is a git push, an API call or a process start cannot
// be rolled back by a database transaction, so its key must survive a crash
// between "started" and "finished". The caller's obligations afterwards are
// exactly:
//
//   - the side effect succeeded → FinishCommand;
//   - the side effect's outcome is unknown → MarkCommandUnknown, which moves the
//     command to reconciling and keeps the key claimed forever, so no retry can
//     start the operation a second time;
//   - the side effect definitely did not happen → the caller may let the claim
//     stand (it will be reconciled) but must NOT call FinishCommand with a
//     fabricated result. Releasing the key is not offered: a released key that
//     a client retries is a second execution, which is the failure this whole
//     mechanism exists to prevent.
//
// The returned PendingCommand is not safe for concurrent use by two goroutines
// for the same request, but two requests for the same key are safe: the loser
// of the claim never receives a PendingCommand at all.
func BeginCommand(c *gin.Context, store *runstore.Store, spec CommandSpec) (*PendingCommand, bool) {
	if store == nil {
		respondAPIError(c, http.StatusServiceUnavailable, CodeBackendUnavailable,
			"Run store is not configured", true,
			apiErrorDetails("reason", reasonRunStoreUnavailable))
		return nil, false
	}
	identity, ok := buildCommandIdentity(c, spec)
	if !ok {
		return nil, false
	}
	principalID, ok := principalFromContext(c)
	if !ok {
		respondAPIError(c, http.StatusUnauthorized, CodeUnauthorized,
			"Authentication required", false, nil)
		return nil, false
	}
	identity.Key.PrincipalID = principalID

	var owner bool
	err := store.WithTx(c.Request.Context(), func(ctx context.Context, tx runstore.Tx) error {
		_, owns, err := runstore.ClaimCommandTx(ctx, tx, identity.Key, identity.Hash, nowUTC())
		owner = owns
		return err
	})
	if err != nil {
		handleCommandStoreError(c, identity.Key, err)
		return nil, false
	}
	if !owner {
		// The key is already claimed for this same request. Answer from the
		// record — 202 while active, the recorded answer once terminal — and
		// hand the caller no pending command, so it cannot start the side
		// effect again.
		replayCommand(c, store, identity)
		return nil, false
	}
	return &PendingCommand{identity: identity, store: store}, true
}

// Key returns the scope this pending command was claimed under.
func (p *PendingCommand) Key() runstore.CommandKey {
	if p == nil {
		return runstore.CommandKey{}
	}
	return p.identity.Key
}

// CommandID returns the client's Idempotency-Key.
func (p *PendingCommand) CommandID() string {
	if p == nil {
		return ""
	}
	return p.identity.Key.CommandID
}

// FinishCommand records a successful outcome and writes the response the client
// is given. It must only be called when the side effect definitely happened and
// its result is known.
func (p *PendingCommand) FinishCommand(c *gin.Context, result CommandResult) {
	if p == nil {
		return
	}
	status, body := successResponse(c, p.identity.Key.CommandID, result)
	record, err := completeCommand(c, p.store, p.identity, runstore.CommandStateSucceeded,
		status, body, result.ResourceType, result.ResourceID)
	if err != nil {
		handleCommandStoreError(c, p.identity.Key, err)
		return
	}
	writeRecordedResponse(c, record)
}

// FinishCommandWithRejection records a business refusal produced by an external
// step (a remote service said no) and writes the refusal. It is the two-phase
// counterpart of returning a *CommandRejection from RunCommand's callback: the
// refusal is a recorded answer, so a retry replays it instead of starting the
// external operation again.
func (p *PendingCommand) FinishCommandWithRejection(c *gin.Context, rejection *CommandRejection) {
	if p == nil {
		return
	}
	status, body := rejectionResponse(c, p.identity.Key.CommandID, rejection)
	record, err := completeCommand(c, p.store, p.identity, runstore.CommandStateFailed,
		status, body, "", "")
	if err != nil {
		handleCommandStoreError(c, p.identity.Key, err)
		return
	}
	writeRecordedResponse(c, record)
}

// MarkCommandUnknown records that the external side effect's outcome is unknown
// and answers 202 reconciling. The key stays claimed: §20.4's "有外部副作用而
// 状态不明时进入 reconciling，禁止启动第二次操作".
//
// After this call the command can still be finished — a reconciler that learns
// the outcome calls FinishCommand — but no duplicate request will ever execute
// it, because every duplicate finds the active record and is answered 202.
func (p *PendingCommand) MarkCommandUnknown(c *gin.Context, reason string) {
	if p == nil {
		return
	}
	if strings.TrimSpace(reason) == "" {
		reason = "external side effect outcome is unknown"
	}
	var record runstore.CommandRecord
	err := p.store.WithTx(c.Request.Context(), func(ctx context.Context, tx runstore.Tx) error {
		var err error
		record, err = runstore.MarkCommandReconcilingTx(ctx, tx, p.identity.Key, p.identity.Hash, reason, nowUTC())
		return err
	})
	if err != nil {
		handleCommandStoreError(c, p.identity.Key, err)
		return
	}
	respondAPIData(c, http.StatusAccepted, commandStatusFromRecord(record), record.Key.CommandID, 0)
}

// ---------------------------------------------------------------------------
// Idempotency key and request hash
// ---------------------------------------------------------------------------

// buildCommandIdentity reads and validates the request's Idempotency-Key,
// resolves the principal, and computes the request hash. It answers the request
// itself and returns ok=false when anything is wrong.
func buildCommandIdentity(c *gin.Context, spec CommandSpec) (commandIdentity, bool) {
	raw := c.GetHeader(HeaderIdempotencyKey)
	commandID := strings.TrimSpace(raw)
	if commandID == "" {
		respondAPIError(c, http.StatusUnprocessableEntity, CodeInvalidRequest,
			"Idempotency-Key header is required", false,
			apiErrorDetails("reason", reasonIdempotencyKeyRequired, "header", HeaderIdempotencyKey))
		return commandIdentity{}, false
	}
	if raw != commandID {
		// Surrounding whitespace: " rq_1" and "rq_1" must not become two keys
		// for one client retry, so the padded form is refused rather than
		// trimmed into the other.
		respondAPIError(c, http.StatusUnprocessableEntity, CodeInvalidRequest,
			"Idempotency-Key must not be padded with whitespace", false,
			apiErrorDetails("reason", reasonIdempotencyKeyInvalid, "header", HeaderIdempotencyKey))
		return commandIdentity{}, false
	}
	if len(commandID) > runstore.MaxCommandIDLength {
		respondAPIError(c, http.StatusUnprocessableEntity, CodeInvalidRequest,
			"Idempotency-Key is too long", false,
			apiErrorDetails("reason", reasonIdempotencyKeyInvalid, "header", HeaderIdempotencyKey))
		return commandIdentity{}, false
	}
	for _, r := range commandID {
		if r > 127 || r < 32 {
			respondAPIError(c, http.StatusUnprocessableEntity, CodeInvalidRequest,
				"Idempotency-Key must be printable ASCII", false,
				apiErrorDetails("reason", reasonIdempotencyKeyInvalid, "header", HeaderIdempotencyKey))
			return commandIdentity{}, false
		}
	}
	if strings.TrimSpace(spec.Operation) == "" {
		respondAPIError(c, http.StatusInternalServerError, CodeBackendUnavailable,
			"Command operation is not configured", true, nil)
		return commandIdentity{}, false
	}
	if strings.TrimSpace(spec.ProjectID) == "" {
		respondAPIError(c, http.StatusUnprocessableEntity, CodeInvalidRequest,
			"Command project is required", false, nil)
		return commandIdentity{}, false
	}

	body, err := json.Marshal(spec.Body)
	if err != nil {
		respondAPIError(c, http.StatusUnprocessableEntity, CodeInvalidRequest,
			"Request body could not be encoded", false, nil)
		return commandIdentity{}, false
	}
	hash, err := runstore.CommandRequestHash(c.Request.Method, canonicalRequestPath(c), body)
	if err != nil {
		if errors.Is(err, runstore.ErrInvalidCommand) {
			respondAPIError(c, http.StatusUnprocessableEntity, CodeInvalidRequest,
				"Request body is not a usable command payload", false, nil)
			return commandIdentity{}, false
		}
		respondAPIError(c, http.StatusInternalServerError, CodeBackendUnavailable,
			"Request could not be hashed", true, nil)
		return commandIdentity{}, false
	}

	return commandIdentity{
		Key: runstore.CommandKey{
			ProjectID: spec.ProjectID,
			Operation: spec.Operation,
			CommandID: commandID,
		},
		Hash: hash,
	}, true
}

// canonicalRequestPath is the path the request hash covers (§27.3: the hash
// includes the canonical path). Gin's FullPath is the route pattern — for
// /api/v1/projects/:id/runs it is "/api/v1/projects/:id/runs" — which is the
// stable, canonical form: two requests for the same route hash the same however
// the concrete ids differ, while a request to a different route cannot collide
// with it. Falling back to the raw path keeps a route mounted outside gin's
// router (a test-only handler) working rather than hashing an empty string.
func canonicalRequestPath(c *gin.Context) string {
	if full := strings.TrimSpace(c.FullPath()); full != "" {
		return full
	}
	return c.Request.URL.Path
}

// principalFromContext derives the idempotency principal from the authenticated
// identity.
//
// The principal is "<type>:<id>" — the actor type is part of the identity
// because "agent:7" and "user:7" are different principals, and two principals
// may choose the same client command id. The actor comes from the request
// context, where the auth middleware injected it after validating the token
// (T0.10.c); it is never read from a request header, which would let a client
// choose its own principal and therefore its own idempotency scope.
//
// A request with no authenticated actor is refused: §27.3's scope begins with
// the principal, and defaulting to the system actor would make every
// unauthenticated caller share one scope.
func principalFromContext(c *gin.Context) (string, bool) {
	actor, err := audit.ResolveActor(c.Request.Context(), audit.MissingActorReject)
	if err != nil {
		return "", false
	}
	id := strings.TrimSpace(actor.ID)
	if id == "" {
		return "", false
	}
	return string(actor.Type) + ":" + id, true
}

// ---------------------------------------------------------------------------
// If-Match and expected_revision
// ---------------------------------------------------------------------------

// ExpectedRevision resolves the optimistic-concurrency precondition of a
// request that may carry it in two places (§27.3): the If-Match header and an
// expected_revision field in the body. The rules:
//
//   - If-Match must be a single strong entity tag holding a positive integer:
//     `"7"`. A weak tag (`W/"7"`), a list of tags, an unquoted number or a
//     non-numeric value is 422 invalid_request + details.reason=invalid_if_match.
//     It is refused rather than ignored because a client that sends a
//     precondition believes it is protected by it.
//   - When both the header and the body carry a revision they must agree.
//     Disagreement is 422 + details.reason=if_match_body_mismatch: the client
//     sent two different expectations and the server must not guess which one it
//     meant. This is §27.3's "必须一致，否则 422".
//   - Neither present → ok=false, and the caller proceeds without a
//     precondition.
//
// fromIfMatch reports which source supplied the revision, because a failure
// afterwards is answered differently: 412 for If-Match, 409 for a body
// expected_revision.
func ExpectedRevision(c *gin.Context, bodyExpected *int64) (rev int64, fromIfMatch bool, ok bool) {
	header := strings.TrimSpace(c.GetHeader(HeaderIfMatch))
	var headerRev int64
	headerSet := false
	if header != "" {
		parsed, err := parseIfMatch(header)
		if err != nil {
			respondAPIError(c, http.StatusUnprocessableEntity, CodeInvalidRequest,
				"If-Match must be a single strong entity tag holding a positive integer", false,
				apiErrorDetails("reason", reasonInvalidIfMatch, "if_match", header))
			return 0, false, false
		}
		headerRev = parsed
		headerSet = true
	}

	bodySet := bodyExpected != nil
	var bodyRev int64
	if bodySet {
		bodyRev = *bodyExpected
	}

	switch {
	case headerSet && bodySet:
		if headerRev != bodyRev {
			respondAPIError(c, http.StatusUnprocessableEntity, CodeInvalidRequest,
				"If-Match and expected_revision disagree", false,
				apiErrorDetails("reason", reasonIfMatchBodyMismatch,
					"if_match", headerRev, "expected_revision", bodyRev))
			return 0, false, false
		}
		return headerRev, true, true
	case headerSet:
		return headerRev, true, true
	case bodySet:
		if bodyRev < 1 {
			respondAPIError(c, http.StatusUnprocessableEntity, CodeInvalidRequest,
				"expected_revision must be a positive integer", false,
				apiErrorDetails("reason", reasonInvalidExpectedRev, "expected_revision", bodyRev))
			return 0, false, false
		}
		return bodyRev, false, true
	default:
		return 0, false, false
	}
}

// parseIfMatch accepts exactly one strong entity tag holding a positive
// integer. Anything else is an error, because a precondition the server cannot
// read is a precondition the client believes is protecting it.
func parseIfMatch(value string) (int64, error) {
	// A list ("7", "8") or a weak tag (W/"7") is not a precondition this
	// contract supports.
	if strings.Contains(value, ",") {
		return 0, errors.New("multiple entity tags are not supported")
	}
	if strings.HasPrefix(value, "W/") || strings.HasPrefix(value, "w/") {
		return 0, errors.New("weak entity tags are not supported")
	}
	if !strings.HasPrefix(value, `"`) || !strings.HasSuffix(value, `"`) || len(value) < 3 {
		return 0, errors.New("entity tag must be a quoted integer")
	}
	digits := value[1 : len(value)-1]
	if strings.Contains(digits, `"`) {
		return 0, errors.New("entity tag must be a quoted integer")
	}
	rev, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("entity tag is not an integer: %w", err)
	}
	if rev < 1 {
		return 0, errors.New("revision must be positive")
	}
	return rev, nil
}

// SetRevisionETag stamps a successful response with the revision it represents,
// so the client can send it back in If-Match on the next write. The value is a
// strong entity tag: it names a concrete stored revision, which is what makes
// "if and only if this is still revision N" a meaningful precondition.
func SetRevisionETag(c *gin.Context, revision int64) {
	if revision < 1 {
		return
	}
	c.Header(HeaderETag, `"`+strconv.FormatInt(revision, 10)+`"`)
}

// RespondRevisionConflict answers a write whose precondition failed. The status
// depends on where the expectation came from (§27.3):
//
//   - If-Match → 412 conflict + details.reason=precondition_failed;
//   - body expected_revision → 409 conflict + details.reason=revision_conflict.
//
// Both carry the expected and current revisions when they are known, so the
// client can re-read and decide. err is normally a *runstore.RevisionConflictError
// or *runstore.TaskRevisionConflictError; anything else is answered with the
// expected revision alone rather than inventing a current one.
func RespondRevisionConflict(c *gin.Context, err error, fromIfMatch bool) {
	details := map[string]any{}
	var expected, current int64
	switch typed := err.(type) {
	case *runstore.RevisionConflictError:
		expected, current = typed.Expected, typed.Current
		details["run_id"] = typed.RunID
		details["current_status"] = string(typed.CurrentStatus)
	case *runstore.TaskRevisionConflictError:
		expected, current = typed.Expected, typed.Current
		details["task_id"] = typed.TaskID
		details["current_status"] = string(typed.CurrentStatus)
	default:
		var runConflict *runstore.RevisionConflictError
		var taskConflict *runstore.TaskRevisionConflictError
		switch {
		case errors.As(err, &runConflict):
			expected, current = runConflict.Expected, runConflict.Current
			details["run_id"] = runConflict.RunID
			details["current_status"] = string(runConflict.CurrentStatus)
		case errors.As(err, &taskConflict):
			expected, current = taskConflict.Expected, taskConflict.Current
			details["task_id"] = taskConflict.TaskID
			details["current_status"] = string(taskConflict.CurrentStatus)
		}
	}
	if expected > 0 {
		details["expected_revision"] = expected
	}
	if current > 0 {
		details["current_revision"] = current
	}

	if fromIfMatch {
		details["reason"] = reasonPreconditionFailed
		respondAPIError(c, http.StatusPreconditionFailed, CodeConflict,
			"If-Match precondition failed", false, details)
		return
	}
	details["reason"] = reasonRevisionConflict
	respondAPIError(c, http.StatusConflict, CodeConflict,
		"Resource revision has changed", false, details)
}

// ---------------------------------------------------------------------------
// Time
// ---------------------------------------------------------------------------

// nowUTC is the clock the command helpers stamp records with. It is a variable
// so a test can pin time if it needs to; production never replaces it.
var nowUTC = func() time.Time { return time.Now().UTC() }
