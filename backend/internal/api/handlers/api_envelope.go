// The unified response envelope and its closed error-code set (plan §20/§27.3,
// §15 T0.03; T1.11.b).
//
// §27.3 fixes one failure shape for the new endpoints: error{code,message,
// retryable,details} plus meta{request_id}. It is a different shape from the
// legacy Response{success,data,error} in common.go, and the two deliberately
// coexist: T0.03 step 4 says old clients migrate one by one and no global
// response change may break the existing UI. A handler therefore chooses an
// envelope explicitly — respondOK for a legacy route, respondData /
// respondAPIError for a CodeFlow 3.0 route — and nothing about this file
// changes what the legacy helpers emit.
//
// The code set is closed and lives in exactly one place per side of the
// contract: this Go list, and the enum in backend/schemas/error.schema.json
// (mirrored in openapi.yaml's ErrorBody). TestErrorCodeSetMatchesSchema reads
// the schema file and compares, so adding a code here without a fixture change
// fails the build rather than shipping a code no client can branch on. §15
// T0.03's namespace ruling is what keeps command-layer reasons —
// command_not_found, ambiguous_command, precondition_failed, revision_conflict,
// idempotency_key_required, idempotency_key_invalid, invalid_if_match,
// if_match_body_mismatch, run_store_unavailable — *out* of the enum: they are
// operation-state reasons and travel in details.reason, because the closed set
// is the set of things a client program can branch on, and "which command did
// you mean" is not one of them.

package handlers

import (
	"net/http"
	"strings"

	"github.com/codeflow/backend/internal/api/middleware"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// APICode is one value of the closed 13-value error-code enum (§15 T0.03).
// The type is a string so it serialises as one; the constants are the only
// values any handler may put in error.code.
type APICode string

// The closed set, verbatim from §15 T0.03 and backend/schemas/error.schema.json.
const (
	// CodeUnauthorized means the request carried no usable credential.
	CodeUnauthorized APICode = "unauthorized"
	// CodeAuthExpired means a credential was presented but is no longer valid.
	CodeAuthExpired APICode = "auth_expired"
	// CodeProtocolMismatch means the client and server disagree about the
	// protocol version of a stream or frame.
	CodeProtocolMismatch APICode = "protocol_mismatch"
	// CodeForbidden means the principal is authenticated but not allowed to do
	// this.
	CodeForbidden APICode = "forbidden"
	// CodeInvalidRequest means the request is not a valid instance of the
	// contract: a malformed field, a missing required header, an unusable path
	// parameter. It is deliberately not used for "the resource is not there".
	CodeInvalidRequest APICode = "invalid_request"
	// CodeConflict means the request contradicts the resource's current state:
	// a revision precondition that is not the If-Match failure case, an
	// ambiguous command key, a business-state refusal.
	CodeConflict APICode = "conflict"
	// CodeIdempotencyKeyReused means an Idempotency-Key already names a
	// different request (§27.3). The stored answer is never returned for it.
	CodeIdempotencyKeyReused APICode = "idempotency_key_reused"
	// CodeEventGap means a replay cursor's history is no longer retained.
	CodeEventGap APICode = "event_gap"
	// CodePolicyDenied means a policy engine refused the action.
	CodePolicyDenied APICode = "policy_denied"
	// CodeBackendUnavailable means a dependency this endpoint needs is not
	// configured or not reachable. The request may be retried.
	CodeBackendUnavailable APICode = "backend_unavailable"
	// CodeBaseChanged means the base a merge or review was prepared against has
	// moved.
	CodeBaseChanged APICode = "base_changed"
	// CodeMergeConflict means a merge could not apply cleanly.
	CodeMergeConflict APICode = "merge_conflict"
	// CodeBudgetExceeded means a run's budget refuses the action.
	CodeBudgetExceeded APICode = "budget_exceeded"
)

// apiErrorCodes is the closed set in a stable order. Tests iterate it and
// compare it with the schema; the order is the schema's, so a diff of the two
// lists reads as a diff of the contract.
var apiErrorCodes = []APICode{
	CodeUnauthorized,
	CodeAuthExpired,
	CodeProtocolMismatch,
	CodeForbidden,
	CodeInvalidRequest,
	CodeConflict,
	CodeIdempotencyKeyReused,
	CodeEventGap,
	CodePolicyDenied,
	CodeBackendUnavailable,
	CodeBaseChanged,
	CodeMergeConflict,
	CodeBudgetExceeded,
}

// Valid reports whether c is in the closed set. A response helper refuses to
// emit anything else: an unknown code would be a contract break no client could
// branch on, and the schema comparison in the tests only covers the constants,
// not values computed at run time.
func (c APICode) Valid() bool {
	for _, known := range apiErrorCodes {
		if c == known {
			return true
		}
	}
	return false
}

// APIMeta is the meta object of both envelope branches (§20.1). request_id is
// always present; command_id appears only on write-command responses, where it
// equals the client's Idempotency-Key; revision is the represented resource's
// revision.
type APIMeta struct {
	RequestID string `json:"request_id"`
	CommandID string `json:"command_id,omitempty"`
	Revision  *int64 `json:"revision,omitempty"`
}

// APIErrorBody is the failure body (§27.3). Details is omitted when the code
// needs no context, which is the schema's contract too.
type APIErrorBody struct {
	Code      APICode        `json:"code"`
	Message   string         `json:"message"`
	Retryable bool           `json:"retryable"`
	Details   map[string]any `json:"details,omitempty"`
}

// APISuccessEnvelope is the success branch: {data, meta} and nothing else, so a
// client can tell success from failure by which key is present.
type APISuccessEnvelope struct {
	Data any     `json:"data"`
	Meta APIMeta `json:"meta"`
}

// APIErrorEnvelope is the failure branch: {error, meta}.
type APIErrorEnvelope struct {
	Error APIErrorBody `json:"error"`
	Meta  APIMeta      `json:"meta"`
}

// APIResponseMeta builds the meta object of a response. request_id comes from
// the trace middleware, which the router installs globally; a handler exercised
// without it (a unit test, or a new route mounted before Trace) still gets a
// non-empty id, because meta.request_id is required by the schema and an empty
// one would make the envelope invalid rather than merely uninformative.
//
// A commandID of "" omits the field; a zero revision is omitted too (revisions
// start at 1, so 0 is "not a revision I have").
func APIResponseMeta(c *gin.Context, commandID string, revision int64) APIMeta {
	meta := APIMeta{CommandID: commandID}
	if trace := middleware.GetTrace(c); trace != nil {
		meta.RequestID = strings.TrimSpace(trace.RequestID)
	}
	if meta.RequestID == "" {
		meta.RequestID = uuid.NewString()
	}
	if revision > 0 {
		rev := revision
		meta.Revision = &rev
	}
	return meta
}

// respondAPIError writes a failure envelope. status is the HTTP status the
// contract assigns this condition (§27.3: 401/403/404/409/412/422/429/503/410);
// the code is the closed-set value the client branches on, and the two are
// independent — a 404 command_not_found carries code invalid_request because
// "no such command" is a request problem in the closed set, not a new code.
//
// A code outside the closed set is a programming error. Rather than emit an
// invalid envelope (which would make every client's parser fail), the helper
// reports it as a 500 with the closest legal code and logs nothing about the
// caller's data. Reaching it means a handler passed a hand-written string.
func respondAPIError(c *gin.Context, status int, code APICode, message string, retryable bool, details map[string]any) {
	if !code.Valid() {
		status = http.StatusInternalServerError
		code = CodeBackendUnavailable
		message = "internal error"
		retryable = true
		details = nil
	}
	if strings.TrimSpace(message) == "" {
		message = string(code)
	}
	c.JSON(status, APIErrorEnvelope{
		Error: APIErrorBody{
			Code:      code,
			Message:   message,
			Retryable: retryable,
			Details:   details,
		},
		Meta: APIResponseMeta(c, "", 0),
	})
}

// respondAPIData writes a success envelope with an explicit status. commandID
// and revision are the meta extras; pass "" and 0 when the response names no
// command and represents no revision.
func respondAPIData(c *gin.Context, status int, data any, commandID string, revision int64) {
	c.JSON(status, APISuccessEnvelope{
		Data: data,
		Meta: APIResponseMeta(c, commandID, revision),
	})
}

// apiErrorDetails builds the details object from alternating key/value pairs,
// skipping blank keys. It exists so a call site reads as "reason:
// command_not_found" rather than as a map literal, which is where a typo in the
// key would hide. A nil return means "no details", which the envelope omits.
func apiErrorDetails(pairs ...any) map[string]any {
	if len(pairs) == 0 {
		return nil
	}
	details := make(map[string]any, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		key, ok := pairs[i].(string)
		if !ok || strings.TrimSpace(key) == "" {
			continue
		}
		details[key] = pairs[i+1]
	}
	if len(details) == 0 {
		return nil
	}
	return details
}
