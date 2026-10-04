// Approval decision endpoint: POST /api/v1/projects/:id/approvals/:approval_id/decide
// (T2.02.c group 2; plan §27.3's frozen request `approved bool`, `fingerprint`,
// `expected_revision`, `reason` — and §28 T2.02.c's rule that `approved:false`
// is a decision that lands as `rejected`, never "not filled in").
//
// The endpoint is a write command in the §20.4/§27.3 sense, so it runs through
// RunCommand (T1.11.b): the Idempotency-Key is claimed in the same transaction
// that decides the approval, a business refusal (*CommandRejection) is recorded
// as `failed` and replayed verbatim on retry, and a system failure rolls back
// and releases the key. It is the first production user of that helper, which
// is why this file follows its contract to the letter:
//
//   - Everything that can be refused from the request alone (path, body,
//     precondition) is refused BEFORE RunCommand, so no idempotency key is
//     claimed and nothing is written for a malformed request (§27.3).
//   - The approval row is read inside the command's transaction and the project
//     ownership check is made there, so "approval does not exist" and "approval
//     belongs to another project" produce one byte-identical refusal
//     (approval_not_found): the endpoint is not an existence oracle.
//   - DecideTx's errors are classified into recorded refusals rather than plain
//     errors. The one case that matters most is ErrApprovalExpired: writing
//     `expired` IS the outcome (RefusalWrote), so it must be returned as a
//     *CommandRejection — RunCommand commits the transaction for a rejection,
//     which is what lets the expiry survive the 409.
//
// The decision itself never moves the Run (§27.2 item 3): a grant is applied at
// consumption time and a denial only after it is delivered to the backend, both
// of which belong to other cards. This endpoint writes exactly one event,
// approval.decided, through DecideTx.
package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/codeflow/backend/internal/approval"
	"github.com/codeflow/backend/internal/audit"
	"github.com/codeflow/backend/internal/run"
	"github.com/codeflow/backend/internal/runstore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// decideApprovalOperation is the ledger's operation name for this route. It is
// part of the idempotency scope, so it is a constant and not built from the
// path.
const decideApprovalOperation = "approvals.decide"

// maxApprovalDecisionBodyBytes bounds the request body. The contract's fields
// are all small; a body above this is refused as invalid rather than read into
// memory, which is the fail-closed direction.
const maxApprovalDecisionBodyBytes = 1 << 20

// Decision-layer reasons. Like the command reasons, they travel in
// details.reason and are deliberately not part of the closed error-code enum
// (§15 T0.03 namespace ruling).
const (
	reasonApprovalNotFound         = "approval_not_found"
	reasonApprovedRequired         = "approved_required"
	reasonFingerprintRequired      = "fingerprint_required"
	reasonFingerprintMismatch      = "fingerprint_mismatch"
	reasonExpectedRevisionRequired = "expected_revision_required"
	reasonInvalidBody              = "invalid_body"
	reasonCommentTooLong           = "comment_too_long"
	reasonApprovalAlreadyDecided   = "approval_already_decided"
	reasonApprovalExpired          = "approval_expired"
	reasonInvalidApproval          = "invalid_approval"
)

// approvalDecisionRequest is the parsed body as the handler works with it:
// ExpectedRevision is a pointer so "absent" and "0" are different inputs, which
// is the distinction the shared ExpectedRevision helper makes.
type approvalDecisionRequest struct {
	Approved         bool
	Fingerprint      string
	ExpectedRevision *int64
	Reason           string
}

// approvalDecisionCommandBody is the de-identified request structure the
// command hash covers. It holds every field that decides what the command
// does — including the comment, because two requests that differ in their
// answer or note are different requests and a shared key must be refused as
// reused rather than replayed (fail closed). No field names a credential, so
// the store's secret scan is satisfied by construction.
type approvalDecisionCommandBody struct {
	Approved         bool   `json:"approved"`
	Fingerprint      string `json:"fingerprint"`
	ExpectedRevision int64  `json:"expected_revision"`
	Reason           string `json:"reason"`
}

// approvalDecisionView is the success data payload: the approval as decided.
// decided_by/decided_at are present exactly when the row is decided, run_id
// only for a tool approval — the omitempty fields are the ones the row itself
// may not carry.
type approvalDecisionView struct {
	ID          string `json:"id"`
	ProjectID   string `json:"project_id"`
	RunID       string `json:"run_id,omitempty"`
	SubjectType string `json:"subject_type"`
	SubjectID   string `json:"subject_id"`
	Risk        string `json:"risk"`
	Status      string `json:"status"`
	Revision    int64  `json:"revision"`
	DecidedBy   string `json:"decided_by,omitempty"`
	DecidedAt   string `json:"decided_at,omitempty"`
	ExpiresAt   string `json:"expires_at"`
	Fingerprint string `json:"fingerprint"`
}

func approvalDecisionViewFrom(row approval.Approval) approvalDecisionView {
	return approvalDecisionView{
		ID:          row.ID,
		ProjectID:   row.ProjectID,
		RunID:       row.RunID,
		SubjectType: string(row.SubjectType),
		SubjectID:   row.SubjectID,
		Risk:        string(row.Risk),
		Status:      string(row.Status),
		Revision:    row.Revision,
		DecidedBy:   row.DecidedBy,
		DecidedAt:   formatAPITime(row.DecidedAt),
		ExpiresAt:   formatAPITime(row.ExpiresAt),
		Fingerprint: row.Fingerprint,
	}
}

// DecideApproval handles POST /api/v1/projects/:id/approvals/:approval_id/decide.
//
// The checks before RunCommand answer with the unified error envelope and
// write nothing:
//
//   - the path project must be a UUID (400, reason invalid_project_id), the
//     same first check GetCommand makes, so a malformed path is one shape on
//     every v1 route;
//   - the body is parsed strictly: a non-object, an unknown field, a duplicate
//     key, a trailing second value, or a field of the wrong JSON type is 422
//     invalid_body. `approved` may only be a JSON boolean; its absence (and
//     explicit null) is 422 approved_required — an answer nobody gave is not a
//     denial (§28 T2.02.c). `fingerprint` must be a non-blank string
//     (fingerprint_required). `reason` is optional.
//   - an `expected_revision` (or If-Match) is required: without one the
//     decision would be a write with no stated expectation, so it is 422
//     expected_revision_required. When both are present they must agree, which
//     ExpectedRevision enforces (422 if_match_body_mismatch).
//
// Everything after that happens inside the command's transaction.
func DecideApproval(c *gin.Context) {
	projectID := c.Param("id")
	if _, err := uuid.Parse(projectID); err != nil {
		// 400 with the 3.0 envelope, exactly like GetCommand: a client that
		// parses {error, meta} must not get the legacy shape for a malformed
		// path on a v1 route.
		respondAPIError(c, http.StatusBadRequest, CodeInvalidRequest,
			"Project ID must be a UUID", false,
			apiErrorDetails("reason", reasonInvalidProjectID, "field", "id"))
		return
	}
	approvalID := strings.TrimSpace(c.Param("approval_id"))

	request, ok := parseApprovalDecisionBody(c)
	if !ok {
		return
	}
	revision, fromIfMatch, ok := resolveDecisionExpectedRevision(c, request.ExpectedRevision)
	if !ok {
		return
	}
	// The revision hashed is the resolved one: an If-Match "3" and a body
	// expected_revision 3 express the same expectation, so they are the same
	// request to the ledger.
	body := approvalDecisionCommandBody{
		Approved:         request.Approved,
		Fingerprint:      request.Fingerprint,
		ExpectedRevision: revision,
		Reason:           request.Reason,
	}

	RunCommand(c, GetCommandStore(),
		CommandSpec{Operation: decideApprovalOperation, ProjectID: projectID, Body: body},
		func(ctx context.Context, tx runstore.Tx) (CommandResult, error) {
			return decideApprovalTx(ctx, c, tx, approvalID, projectID, revision, fromIfMatch, body)
		})
}

// decideApprovalTx is the command callback: read, bind, decide — all inside the
// transaction RunCommand claimed the key in.
func decideApprovalTx(ctx context.Context, c *gin.Context, tx runstore.Tx,
	approvalID, projectID string, revision int64, fromIfMatch bool, body approvalDecisionCommandBody) (CommandResult, error) {

	current, err := approval.GetApproval(ctx, tx, approvalID)
	if err != nil {
		if errors.Is(err, runstore.ErrNotFound) {
			return CommandResult{}, approvalNotFoundRejection()
		}
		// A storage failure is a plain error: the transaction rolls back, the
		// key is released, and the client may retry the same key.
		return CommandResult{}, err
	}
	if current.ProjectID != projectID {
		// Deliberately the same refusal as "no such approval": whether the row
		// exists in another project is not this project's business, and the two
		// answers are byte-identical.
		return CommandResult{}, approvalNotFoundRejection()
	}
	if current.Fingerprint != body.Fingerprint {
		// The human is not looking at this approval. Nothing is written; the
		// stored fingerprint is not echoed back.
		return CommandResult{}, &CommandRejection{
			StatusCode: http.StatusConflict,
			Code:       CodeConflict,
			Message:    "The request fingerprint does not match the approval",
			Details:    apiErrorDetails("reason", reasonFingerprintMismatch, "approval_id", current.ID),
		}
	}

	// The principal was already verified by RunCommand (401 otherwise), and the
	// actor is the same identity the auth middleware injected; both are resolved
	// here because DecisionInput needs them together. A resolution failure is
	// unreachable after RunCommand's check and is a plain error on purpose: it
	// must roll back rather than record anything.
	principalID, ok := principalFromContext(c)
	if !ok {
		return CommandResult{}, errors.New("handlers: approval decision without an authenticated principal")
	}
	actor, err := audit.ResolveActor(ctx, audit.MissingActorReject)
	if err != nil {
		return CommandResult{}, err
	}

	decided, err := approval.DecideTx(ctx, tx, approval.DecisionInput{
		ApprovalID:       approvalID,
		ExpectedRevision: revision,
		Approved:         body.Approved,
		DecidedBy:        principalID,
		Actor: run.Actor{
			Type:   run.ActorType(actor.Type),
			ID:     actor.ID,
			Source: actor.Source,
		},
		Now:     nowUTC(),
		Comment: body.Reason,
	})
	if err != nil {
		return CommandResult{}, decisionRefusal(current, err, revision, fromIfMatch)
	}

	// The ETag names the revision this decision produced, so the client's next
	// write can carry it back in If-Match. It is set on the owning execution
	// only: a replayed answer is written from the stored bytes by RunCommand and
	// does not re-run this callback (a client replays the same key precisely
	// because it has no revision to send yet).
	SetRevisionETag(c, decided.Revision)

	return CommandResult{
		StatusCode:   http.StatusOK,
		Data:         approvalDecisionViewFrom(decided),
		ResourceType: "approval",
		ResourceID:   decided.ID,
	}, nil
}

// decisionRefusal maps a DecideTx failure onto the recorded refusal the client
// is answered with. A *CommandRejection is recorded as `failed` and committed
// by RunCommand, which is exactly what the expiry case needs: the `expired`
// write DecideTx performed must survive the 409, and a rollback would leave the
// row pending past its deadline (RefusalWrote).
func decisionRefusal(current approval.Approval, err error, expected int64, fromIfMatch bool) error {
	switch {
	case errors.Is(err, approval.ErrApprovalConflict):
		// The row moved under the caller: the CAS revision no longer matches, or
		// a concurrent decision/expiry won. Status follows the source of the
		// expectation (§27.3): 412 for If-Match, 409 for a body expected_revision.
		return approvalRevisionConflictRejection(current, expected, fromIfMatch)
	case errors.Is(err, approval.ErrApprovalExpired):
		expired, ok := approval.ExpiredApproval(err)
		if !ok {
			// Not an expiry outcome: nothing was written, so it rolls back.
			return err
		}
		return &CommandRejection{
			StatusCode: http.StatusConflict,
			Code:       CodeConflict,
			Message:    "The approval expired before the decision",
			Details: apiErrorDetails("reason", reasonApprovalExpired,
				"approval_id", expired.ID, "status", string(expired.Status), "revision", expired.Revision),
		}
	case errors.Is(err, approval.ErrApprovalAlreadyDecided):
		return &CommandRejection{
			StatusCode: http.StatusConflict,
			Code:       CodeConflict,
			Message:    "The approval is already decided",
			Details: apiErrorDetails("reason", reasonApprovalAlreadyDecided,
				"approval_id", current.ID, "status", string(current.Status), "revision", current.Revision),
		}
	case errors.Is(err, approval.ErrInvalidApproval):
		// The service's own validation refused the input. Everything that could
		// be checked beforehand was, so this is the backstop for a combination
		// this handler did not anticipate.
		return &CommandRejection{
			StatusCode: http.StatusUnprocessableEntity,
			Code:       CodeInvalidRequest,
			Message:    "The decision is not a valid instance of the contract",
			Details:    apiErrorDetails("reason", reasonInvalidApproval, "approval_id", current.ID),
		}
	default:
		return err
	}
}

// approvalNotFoundRejection is the one answer for both "no such approval" and
// "that approval belongs to another project". It is built in one place so the
// two conditions cannot drift into distinguishable bodies.
func approvalNotFoundRejection() *CommandRejection {
	return &CommandRejection{
		StatusCode: http.StatusNotFound,
		Code:       CodeInvalidRequest,
		Message:    "No approval with this id",
		Details:    apiErrorDetails("reason", reasonApprovalNotFound),
	}
}

// approvalRevisionConflictRejection records a lost revision CAS with the same
// status, reason words and details vocabulary as the run/task helper
// RespondRevisionConflict (commands.go): 412 + precondition_failed for an
// If-Match expectation, 409 + revision_conflict for a body expected_revision,
// with expected/current revisions and the current status so the client can
// re-read and decide.
func approvalRevisionConflictRejection(current approval.Approval, expected int64, fromIfMatch bool) *CommandRejection {
	status := http.StatusConflict
	reason := reasonRevisionConflict
	message := "Resource revision has changed"
	if fromIfMatch {
		status = http.StatusPreconditionFailed
		reason = reasonPreconditionFailed
		message = "If-Match precondition failed"
	}
	return &CommandRejection{
		StatusCode: status,
		Code:       CodeConflict,
		Message:    message,
		Details: apiErrorDetails("reason", reason,
			"approval_id", current.ID,
			"expected_revision", expected,
			"current_revision", current.Revision,
			"current_status", string(current.Status)),
	}
}

// resolveDecisionExpectedRevision enforces that a decision states the revision
// it was taken against, then resolves it through the shared helper.
//
// ExpectedRevision cannot express "neither source supplied" (it returns ok=false
// both for that and for the failures it answers itself), so the presence check
// is made here first: a write with no stated expectation is 422
// expected_revision_required. Everything else — If-Match syntax, body
// expected_revision >= 1, and the 422 if_match_body_mismatch when the two
// disagree — is answered by ExpectedRevision itself.
func resolveDecisionExpectedRevision(c *gin.Context, bodyExpected *int64) (int64, bool, bool) {
	if strings.TrimSpace(c.GetHeader(HeaderIfMatch)) == "" && bodyExpected == nil {
		respondAPIError(c, http.StatusUnprocessableEntity, CodeInvalidRequest,
			"An If-Match header or a body expected_revision is required", false,
			apiErrorDetails("reason", reasonExpectedRevisionRequired))
		return 0, false, false
	}
	revision, fromIfMatch, ok := ExpectedRevision(c, bodyExpected)
	if !ok {
		return 0, false, false
	}
	return revision, fromIfMatch, true
}

// ---------------------------------------------------------------------------
// Strict request decoding
// ---------------------------------------------------------------------------

// parseApprovalDecisionBody decodes the decision body with the strict walk
// T1.12.a established for the websocket frames (websocket/replay.go): the
// object is walked key by key with a json.Decoder, so
//
//   - a non-object (including an empty body) is refused;
//   - an unknown key is refused — and, unlike encoding/json's field matching,
//     a case variant like "Approved" is unknown rather than the field
//     "approved" (Go's decoder is case-insensitive, which a client relying on
//     `approved` must not be allowed to trip);
//   - a duplicate key is refused instead of letting the last one win;
//   - trailing content after the object is refused. dec.More() must not be used
//     for that test — it reports false for a stray `}` — so a further token is
//     read and io.EOF is required;
//   - every value must have the JSON type the contract gives its field.
//
// It answers the failure itself (422 invalid_request, details.reason naming the
// condition) and returns ok=false. Nothing is written and no key is claimed.
func parseApprovalDecisionBody(c *gin.Context) (approvalDecisionRequest, bool) {
	var out approvalDecisionRequest

	fail := func(reason, field, message string) (approvalDecisionRequest, bool) {
		pairs := []any{"reason", reason}
		if field != "" {
			pairs = append(pairs, "field", field)
		}
		respondAPIError(c, http.StatusUnprocessableEntity, CodeInvalidRequest, message, false, apiErrorDetails(pairs...))
		return approvalDecisionRequest{}, false
	}

	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, maxApprovalDecisionBodyBytes+1))
	if err != nil {
		return fail(reasonInvalidBody, "", "The request body could not be read")
	}
	if len(raw) > maxApprovalDecisionBodyBytes {
		return fail(reasonInvalidBody, "", "The request body is too large")
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	// UseNumber keeps an integer literal as its own digits: a float64
	// round-trip would turn a large revision into a different one before the
	// integer check ever saw it.
	dec.UseNumber()

	if tok, err := dec.Token(); err != nil || !decisionIsDelim(tok, '{') {
		return fail(reasonInvalidBody, "", "The request body must be a JSON object")
	}

	var (
		approvedSeen    bool
		fingerprintSeen bool
		revisionSeen    bool
		reasonSeen      bool
		revision        int64
	)
	for dec.More() {
		key, ok := decisionObjectKey(dec)
		if !ok {
			return fail(reasonInvalidBody, "", "The request body must be a JSON object")
		}
		switch key {
		case "approved":
			if approvedSeen {
				return fail(reasonInvalidBody, "approved", "approved is repeated")
			}
			approvedSeen = true
			var value any
			if err := dec.Decode(&value); err != nil {
				return fail(reasonInvalidBody, "approved", "approved is not a JSON boolean")
			}
			switch typed := value.(type) {
			case bool:
				out.Approved = typed
			case nil:
				// Explicit null is "no answer given", not a denial: it is
				// refused exactly like an absent field (§28 T2.02.c).
				return fail(reasonApprovedRequired, "approved", "approved is required and must be a JSON boolean")
			default:
				return fail(reasonInvalidBody, "approved", "approved must be a JSON boolean")
			}
		case "fingerprint":
			if fingerprintSeen {
				return fail(reasonInvalidBody, "fingerprint", "fingerprint is repeated")
			}
			fingerprintSeen = true
			var value any
			if err := dec.Decode(&value); err != nil {
				return fail(reasonInvalidBody, "fingerprint", "fingerprint must be a string")
			}
			text, ok := value.(string)
			if !ok {
				if value == nil {
					return fail(reasonFingerprintRequired, "fingerprint", "fingerprint is required")
				}
				return fail(reasonInvalidBody, "fingerprint", "fingerprint must be a string")
			}
			out.Fingerprint = text
		case "expected_revision":
			if revisionSeen {
				return fail(reasonInvalidBody, "expected_revision", "expected_revision is repeated")
			}
			revisionSeen = true
			var value any
			if err := dec.Decode(&value); err != nil {
				return fail(reasonInvalidExpectedRev, "expected_revision", "expected_revision must be an integer")
			}
			number, ok := value.(json.Number)
			if !ok {
				return fail(reasonInvalidExpectedRev, "expected_revision", "expected_revision must be an integer")
			}
			parsed, err := number.Int64()
			if err != nil {
				return fail(reasonInvalidExpectedRev, "expected_revision", "expected_revision must be an integer")
			}
			// A value below 1 is left for ExpectedRevision to refuse, so the
			// rule lives in exactly one place; revisionSeen is set, so the
			// helper does see the value.
			revision = parsed
		case "reason":
			if reasonSeen {
				return fail(reasonInvalidBody, "reason", "reason is repeated")
			}
			reasonSeen = true
			var value any
			if err := dec.Decode(&value); err != nil {
				return fail(reasonInvalidBody, "reason", "reason must be a string")
			}
			text, ok := value.(string)
			if !ok {
				return fail(reasonInvalidBody, "reason", "reason must be a string")
			}
			out.Reason = text
		default:
			// The key is not echoed: it is client text.
			return fail(reasonInvalidBody, "", "The request body carries an unknown field")
		}
	}
	if !consumeDecisionObjectEnd(dec) || !requireDecisionBodyEnd(dec) {
		return fail(reasonInvalidBody, "", "The request body must be one JSON object")
	}

	if !approvedSeen {
		return fail(reasonApprovedRequired, "approved", "approved is required and must be a JSON boolean")
	}
	if !fingerprintSeen || strings.TrimSpace(out.Fingerprint) == "" {
		return fail(reasonFingerprintRequired, "fingerprint", "fingerprint is required")
	}
	if revisionSeen {
		parsed := revision
		out.ExpectedRevision = &parsed
	}
	// The comment bound is checked here, before RunCommand, so an over-long note
	// claims no key and writes nothing (§27.3's "以上 422 都在 RunCommand 之前
	// 判定"). The trim and count match DecideTx's own rule, so a comment this
	// check accepts is never refused one layer deeper.
	if trimmed := strings.TrimSpace(out.Reason); utf8.RuneCountInString(trimmed) > approval.MaxDecisionCommentRunes {
		respondAPIError(c, http.StatusUnprocessableEntity, CodeInvalidRequest,
			"reason exceeds the maximum length", false,
			apiErrorDetails("reason", reasonCommentTooLong, "field", "reason", "limit", approval.MaxDecisionCommentRunes))
		return approvalDecisionRequest{}, false
	}
	return out, true
}

// decisionIsDelim reports whether tok is the expected JSON delimiter.
func decisionIsDelim(tok json.Token, want json.Delim) bool {
	delim, ok := tok.(json.Delim)
	return ok && delim == want
}

// decisionObjectKey reads the next key of an object being walked. Inside an
// object the token stream yields strings for keys, so anything else is not a
// JSON object.
func decisionObjectKey(dec *json.Decoder) (string, bool) {
	tok, err := dec.Token()
	if err != nil {
		return "", false
	}
	key, ok := tok.(string)
	return key, ok
}

// consumeDecisionObjectEnd reads the closing brace of the object just walked.
func consumeDecisionObjectEnd(dec *json.Decoder) bool {
	tok, err := dec.Token()
	return err == nil && decisionIsDelim(tok, '}')
}

// requireDecisionBodyEnd refuses a second JSON value or trailing garbage after
// the body. dec.More() must not be used: it reports false when the next byte is
// a closing delimiter, which is exactly the `{...}}` case it would miss. One
// more token is read and io.EOF is required.
func requireDecisionBodyEnd(dec *json.Decoder) bool {
	_, err := dec.Token()
	return err == io.EOF
}
