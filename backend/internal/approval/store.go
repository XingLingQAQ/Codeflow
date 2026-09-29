package approval

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codeflow/backend/internal/runstore"
)

// Approval errors.
var (
	// ErrInvalidApproval means the caller handed this package a record the
	// contract rejects: a blank id, project or subject; an invalid subject
	// type, risk or status; a missing required fingerprint field; a scope that
	// is not JSON; a deadline that is not after the request. Like runstore's
	// ErrInvalidRecord these are caught before any SQL runs, so a rejected call
	// leaves the caller's transaction exactly as it was.
	ErrInvalidApproval = errors.New("approval: invalid approval")

	// ErrApprovalProjectMismatch means the approval's project and its
	// fingerprint input's project disagree, or a named run belongs to another
	// project. An approval that names two projects authorizes nothing
	// unambiguously, and the schema cannot express the check (see migration
	// 009), so it is made here.
	ErrApprovalProjectMismatch = errors.New("approval: approval belongs to another project")

	// ErrRunProjectMismatch means a named run or attempt does not exist, the
	// run belongs to another project, or the attempt belongs to another run.
	// The database cannot express it: runs has no UNIQUE (id, project_id) for a
	// composite foreign key (migration 009 records the same reasoning 008 does).
	ErrRunProjectMismatch = errors.New("approval: run or attempt does not belong to the approval")

	// ErrAgentRevisionMismatch means the fingerprint names an agent revision
	// other than the one the named run executes. The hash would describe a call
	// the run can never make, so the approval is refused rather than stored.
	ErrAgentRevisionMismatch = errors.New("approval: fingerprint names another agent revision than the run")

	// ErrApprovalNotConsumable means the approval exists but cannot authorize
	// this consumption: it is not a tool approval, its status is not approved
	// (pending, rejected, expired and invalidated are all unconsumable), the
	// tool call is not the approval's subject, the attempt is not the
	// approval's own, or the grant's deadline has passed. Nothing is written.
	ErrApprovalNotConsumable = errors.New("approval: approval cannot be consumed")

	// ErrAlreadyConsumed means this authorization has already been spent:
	// either on this tool call, or on another tool call of the same attempt.
	// The receipt rows are the mutex — the unique constraints decide — and this
	// sentinel is the Go-side name for losing that race (§27.2.5 "同一
	// tool_call_id 最多一次授权消费").
	ErrAlreadyConsumed = errors.New("approval: authorization already consumed")

	// ErrConsumptionImmutable means a statement tried to UPDATE or DELETE an
	// approval_consumptions row. A receipt is evidence: the whole point of a
	// one-shot authorization is that the record of spending it cannot be moved
	// to another call or deleted to make room for a second one. The store
	// exposes no such path; reaching this means a statement was issued by hand.
	ErrConsumptionImmutable = errors.New("approval: consumption receipt is immutable")

	// ErrApprovalNotPending means an update was attempted on a row that had
	// already been decided. Migration 009's trg_approvals_terminal_is_final
	// refuses every UPDATE whose stored status is not pending, which is what
	// makes a concurrent decide/cancel race leave the winner untouched; T2.02.b
	// reports the loss as this error and re-reads the row rather than
	// overwriting it (§27.2.5).
	ErrApprovalNotPending = errors.New("approval: approval is no longer pending")

	// ErrApprovalBindingFrozen means a statement tried to rewrite what a
	// pending approval authorizes — its fingerprint, the canonical input behind
	// it, its subject, its project or its run/attempt pins. Parameters changed
	// means a *new* approval (§27.2.3), never an edit of a decision a human
	// already saw; trg_approvals_binding_is_frozen refuses the statement.
	ErrApprovalBindingFrozen = errors.New("approval: approval binding is frozen")

	// ErrApprovalIsHistory means a statement tried to DELETE an approval. An
	// approval, pending or decided, is history: a retry keeps the old run's
	// approvals (§27.2.1) and a decision is never rewritten (§19.1), so
	// migration 009's trg_approvals_no_delete refuses the statement. Stopping
	// a pending approval is a status change (expired, invalidated), not a
	// deletion.
	ErrApprovalIsHistory = errors.New("approval: approval rows are history and cannot be deleted")
)

// NewApproval is what a caller supplies to create an approval.
//
// There is deliberately no Fingerprint field: the store computes it from
// FingerprintInput and writes no other value. A caller that could hand over a
// fingerprint could hand over one for an action it is not requesting, which is
// the bypass this package exists to prevent.
type NewApproval struct {
	// ID is the row identity, owned by the caller like every other id in the
	// runtime library: a retry can reuse it and the primary key decides.
	ID string
	// ProjectID is the owning project. It must equal
	// FingerprintInput.ProjectID.
	ProjectID string
	// RunID and AttemptID are optional as a pair (see the rules on
	// CreateApprovalTx): a stage gate or a manual merge has no Run, a tool
	// approval must have both.
	RunID     string
	AttemptID string
	// SubjectType and SubjectID must equal the matching FingerprintInput
	// fields.
	SubjectType SubjectType
	SubjectID   string
	Risk        Risk
	// Scope is the requested scope as JSON (usually an object: paths,
	// operations, limits). It is normalized before storage; nil means "no
	// scope", stored as the empty object.
	Scope []byte
	// FingerprintInput is everything the fingerprint binds.
	FingerprintInput FingerprintInput
	// RequestedAt and ExpiresAt are required; the expiry must be strictly after
	// the request.
	RequestedAt time.Time
	ExpiresAt   time.Time
	// PolicyVersion must equal FingerprintInput.PolicyVersion.
	PolicyVersion string
}

// CreateApprovalTx writes one new approval and returns it as stored.
//
// Rules, all enforced before any SQL runs:
//
//   - FingerprintInput.ProjectID / SubjectType / SubjectID / PolicyVersion must
//     equal the same-named NewApproval fields. They are duplicated on purpose:
//     the columns exist for the table, the input exists for the hash, and an
//     approval whose hash describes a different project or subject than its row
//     would authorize one thing while claiming another.
//   - The fingerprint is computed here from FingerprintInput (so it may be
//     refused for a missing or malformed field) and the canonical input that
//     was hashed is stored in fingerprint_input_json. The caller cannot supply
//     either.
//   - A tool approval must name a run and an attempt (the schema's CHECK says
//     the same); a gate or merge approval may name neither. When a run is
//     named, it must exist and belong to ProjectID, and a named attempt must
//     exist and belong to that run (ErrRunProjectMismatch). When a run is named
//     and FingerprintInput.AgentRevisionID is set (always, for a tool), the run
//     must execute that revision (ErrAgentRevisionMismatch).
//   - A tool approval's SubjectID is the tool call it authorizes, the id a
//     consumption presents, so it obeys the same bound as that id (non-empty,
//     valid UTF-8, at most 256 bytes).
//   - A new approval is always pending with revision 1 and no decider. There is
//     no way to create a decided approval: decisions are T2.02.b, and a status
//     parameter here would let a caller manufacture one.
//   - ExpiresAt must be strictly after RequestedAt.
//
// The returned Approval is the row as written, so a caller never has to guess
// what the store normalized.
func CreateApprovalTx(ctx context.Context, tx runstore.Tx, in NewApproval) (Approval, error) {
	if tx == nil {
		return Approval{}, fmt.Errorf("%w: nil transaction", ErrInvalidApproval)
	}

	id := trimSpace(in.ID)
	if id == "" {
		return Approval{}, fmt.Errorf("%w: ID is required", ErrInvalidApproval)
	}
	projectID := trimSpace(in.ProjectID)
	if projectID == "" {
		return Approval{}, fmt.Errorf("%w: ProjectID is required", ErrInvalidApproval)
	}
	if !in.SubjectType.Valid() {
		if trimSpace(string(in.SubjectType)) == "" {
			return Approval{}, fmt.Errorf("%w: SubjectType is required", ErrInvalidApproval)
		}
		return Approval{}, fmt.Errorf("%w: SubjectType %q is not valid", ErrInvalidApproval, in.SubjectType)
	}
	subjectID := trimSpace(in.SubjectID)
	if subjectID == "" {
		return Approval{}, fmt.Errorf("%w: SubjectID is required", ErrInvalidApproval)
	}
	if in.SubjectType == SubjectTool {
		// A tool approval's subject is the tool call id a consumption has to
		// present (RecordConsumptionTx), so it obeys the same bound; a subject
		// no receipt could name is refused now, not found unspendable later.
		if _, err := checkReference("SubjectID", subjectID); err != nil {
			return Approval{}, err
		}
	}
	if !in.Risk.Valid() {
		if trimSpace(string(in.Risk)) == "" {
			return Approval{}, fmt.Errorf("%w: Risk is required", ErrInvalidApproval)
		}
		return Approval{}, fmt.Errorf("%w: Risk %q is not valid", ErrInvalidApproval, in.Risk)
	}
	policyVersion := trimSpace(in.PolicyVersion)
	if policyVersion == "" {
		return Approval{}, fmt.Errorf("%w: PolicyVersion is required", ErrInvalidApproval)
	}
	if in.RequestedAt.IsZero() {
		return Approval{}, fmt.Errorf("%w: RequestedAt is required", ErrInvalidApproval)
	}
	if in.ExpiresAt.IsZero() {
		return Approval{}, fmt.Errorf("%w: ExpiresAt is required", ErrInvalidApproval)
	}
	requestedAt := in.RequestedAt.UTC()
	expiresAt := in.ExpiresAt.UTC()
	if !expiresAt.After(requestedAt) {
		return Approval{}, fmt.Errorf("%w: ExpiresAt (%s) must be after RequestedAt (%s)",
			ErrInvalidApproval, expiresAt.Format(time.RFC3339Nano), requestedAt.Format(time.RFC3339Nano))
	}

	// The row and its hash must describe the same approval.
	subject := in.SubjectType
	if subject != in.FingerprintInput.SubjectType {
		return Approval{}, fmt.Errorf("%w: SubjectType %q does not match FingerprintInput.SubjectType %q",
			ErrInvalidApproval, subject, in.FingerprintInput.SubjectType)
	}
	if subjectID != trimSpace(in.FingerprintInput.SubjectID) {
		return Approval{}, fmt.Errorf("%w: SubjectID %q does not match FingerprintInput.SubjectID %q",
			ErrInvalidApproval, subjectID, in.FingerprintInput.SubjectID)
	}
	if projectID != trimSpace(in.FingerprintInput.ProjectID) {
		return Approval{}, fmt.Errorf("%w: ProjectID %q does not match FingerprintInput.ProjectID %q",
			ErrInvalidApproval, projectID, in.FingerprintInput.ProjectID)
	}
	if policyVersion != trimSpace(in.FingerprintInput.PolicyVersion) {
		return Approval{}, fmt.Errorf("%w: PolicyVersion %q does not match FingerprintInput.PolicyVersion %q",
			ErrInvalidApproval, policyVersion, in.FingerprintInput.PolicyVersion)
	}

	runID := trimSpace(in.RunID)
	attemptID := trimSpace(in.AttemptID)
	if subject == SubjectTool && (runID == "" || attemptID == "") {
		return Approval{}, fmt.Errorf("%w: a tool approval needs RunID and AttemptID", ErrInvalidApproval)
	}
	if attemptID != "" && runID == "" {
		return Approval{}, fmt.Errorf("%w: AttemptID needs RunID", ErrInvalidApproval)
	}

	scopeJSON, err := CanonicalScopeJSON(in.Scope)
	if err != nil {
		return Approval{}, err
	}

	canonicalInput, fingerprint, err := canonicalFingerprint(in.FingerprintInput)
	if err != nil {
		return Approval{}, err
	}

	if runID != "" {
		// The same normalization canonicalFingerprint applied, so the revision
		// compared is the revision that was hashed.
		agentRevisionID := trimSpace(in.FingerprintInput.AgentRevisionID)
		if err := checkRunAndAttempt(ctx, tx, projectID, runID, attemptID, agentRevisionID); err != nil {
			return Approval{}, err
		}
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO approvals (
			id, project_id, run_id, attempt_id, subject_type, subject_id, risk, scope_json,
			fingerprint, fingerprint_input_json, status, policy_version, requested_at,
			expires_at, decided_by, decided_at, revision)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?, ?, NULL, NULL, 1)`,
		id, projectID, nullableString(runID), nullableString(attemptID),
		string(subject), subjectID, string(in.Risk), scopeJSON,
		fingerprint, canonicalInput, policyVersion,
		requestedAt.UnixMilli(), expiresAt.UnixMilli(),
	)
	if err != nil {
		return Approval{}, fmt.Errorf("approval: insert approval %s: %w", id, err)
	}

	return Approval{
		ID:                   id,
		ProjectID:            projectID,
		RunID:                runID,
		AttemptID:            attemptID,
		SubjectType:          subject,
		SubjectID:            subjectID,
		Risk:                 in.Risk,
		ScopeJSON:            scopeJSON,
		Fingerprint:          fingerprint,
		FingerprintInputJSON: canonicalInput,
		Status:               StatusPending,
		PolicyVersion:        policyVersion,
		RequestedAt:          requestedAt,
		ExpiresAt:            expiresAt,
		Revision:             1,
	}, nil
}

// CanonicalScopeJSON normalizes a scope for storage: the body is decoded and
// re-encoded with sorted object keys and no insignificant whitespace, so two
// spellings of one scope cannot become two rows. nil or empty means "no scope"
// and is stored as `{}`.
//
// It refuses, with ErrInvalidApproval and before any SQL runs: a body that is
// not exactly one JSON value (trailing content is an error, not a prefix to be
// silently dropped) and duplicate object keys at any depth, because a decoder
// that keeps the last one turns `{"cmd":"rm -rf /","cmd":"ls"}` into a scope
// nobody reviewed — the same defect T1.07.b's fingerprint hit and fixed.
//
// Numbers are preserved exactly as written: the value is decoded with
// UseNumber, and encoding/json marshals a json.Number as its own literal, so
// nothing goes through a float64 and no digit is rounded away (1.0 stays 1.0, a
// 30-digit integer stays 30 digits, 1e999 is stored as 1e999 rather than
// overflowing). The consequence a caller should know: two spellings of one
// numeric value (`1e29` and `100000000000000000000000000000`) stay two
// different canonical strings. That is the safe direction for a scope — the
// failure that matters is two *different* scopes collapsing into one, never two
// equal values failing to collapse.
//
// Ordering is by object key (encoding/json sorts map keys) and never by
// anything else: arrays keep their order because array order is meaningful.
func CanonicalScopeJSON(raw []byte) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return "{}", nil
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return "", fmt.Errorf("%w: scope is not valid JSON: %v", ErrInvalidApproval, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("%w: scope has trailing content after the JSON value", ErrInvalidApproval)
	}
	// Duplicates have to be found on the raw text: encoding/json has already
	// resolved them (last one wins) by the time the value above exists.
	if err := checkNoDuplicateKeys(trimmed); err != nil {
		return "", err
	}
	out, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("%w: scope cannot be encoded: %v", ErrInvalidApproval, err)
	}
	if !json.Valid(out) || !bytes.Equal(bytes.TrimSpace(out), out) {
		// Unreachable for a value built by encoding/json, but a scope that is
		// not one canonical document would be stored un-normalized.
		return "", fmt.Errorf("%w: scope is not a single JSON document", ErrInvalidApproval)
	}
	return string(out), nil
}

// checkNoDuplicateKeys walks the raw JSON text and refuses a duplicate object
// key at any depth. It works on tokens, so a repeated *value* is not confused
// with a repeated key, and each frame remembers whether the next string token
// is that object's key or one of its values.
//
// This is deliberately a separate pass over the raw text: encoding/json resolves
// duplicates silently (last one wins), and the scope that gets stored must be
// the scope that was reviewed — `{"cmd":"rm -rf /","cmd":"ls"}` must not become
// a scope that only says `ls`.
func checkNoDuplicateKeys(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()

	// One frame per open object or array. In an object the tokens alternate
	// key, value, key, value; expectKey is true while the next string token
	// would be a key rather than a value. seen is that object's keys so far.
	type frame struct {
		object    bool
		expectKey bool
		seen      map[string]struct{}
	}
	var stack []frame

	// consumeValue marks the innermost open object as being back in a key slot,
	// which is the state after any complete value.
	consumeValue := func() {
		if len(stack) > 0 && stack[len(stack)-1].object {
			stack[len(stack)-1].expectKey = true
		}
	}

	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%w: scope is not valid JSON: %v", ErrInvalidApproval, err)
		}
		if delim, ok := tok.(json.Delim); ok {
			switch delim {
			case '{', '[':
				// The container is the value of the currently open object.
				consumeValue()
				stack = append(stack, frame{object: delim == '{', expectKey: delim == '{',
					seen: map[string]struct{}{}})
			case '}', ']':
				if len(stack) == 0 {
					return fmt.Errorf("%w: scope has an unbalanced %q", ErrInvalidApproval, delim)
				}
				stack = stack[:len(stack)-1]
				// Closing the container completes the parent's value.
				consumeValue()
			}
			continue
		}
		if key, ok := tok.(string); ok && len(stack) > 0 {
			if top := &stack[len(stack)-1]; top.object && top.expectKey {
				top.expectKey = false
				if _, duplicate := top.seen[key]; duplicate {
					return fmt.Errorf("%w: scope repeats the key %q", ErrInvalidApproval, key)
				}
				top.seen[key] = struct{}{}
				continue
			}
		}
		// A scalar, or a string in a value position: the object containing it
		// is back in a key slot.
		consumeValue()
	}
}

// checkRunAndAttempt verifies that a named run belongs to projectID, that a
// named attempt belongs to that run, and — when the fingerprint names an agent
// revision — that the run executes that revision. The first two are exactly
// artifact.checkRunAndAttempt's rules for artifact versions; migration 009
// explains why the database cannot check them: runs has no UNIQUE (id,
// project_id) for a composite foreign key.
func checkRunAndAttempt(ctx context.Context, tx runstore.Tx, projectID, runID, attemptID, agentRevisionID string) error {
	var runProject, runAgentRevision string
	err := tx.QueryRowContext(ctx, `SELECT project_id, agent_revision_id FROM runs WHERE id = ?`, runID).
		Scan(&runProject, &runAgentRevision)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("%w: no run %s", ErrRunProjectMismatch, runID)
	case err != nil:
		return fmt.Errorf("approval: read run %s: %w", runID, err)
	case runProject != projectID:
		return fmt.Errorf("%w: run %s belongs to project %s, the approval to project %s",
			ErrApprovalProjectMismatch, runID, runProject, projectID)
	case agentRevisionID != "" && agentRevisionID != runAgentRevision:
		return fmt.Errorf("%w: FingerprintInput.AgentRevisionID %s, run %s executes %s",
			ErrAgentRevisionMismatch, agentRevisionID, runID, runAgentRevision)
	}
	if attemptID == "" {
		return nil
	}
	var attemptRun string
	err = tx.QueryRowContext(ctx, `SELECT run_id FROM attempts WHERE id = ?`, attemptID).Scan(&attemptRun)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("%w: no attempt %s", ErrRunProjectMismatch, attemptID)
	case err != nil:
		return fmt.Errorf("approval: read attempt %s: %w", attemptID, err)
	case attemptRun != runID:
		return fmt.Errorf("%w: attempt %s belongs to run %s", ErrRunProjectMismatch, attemptID, attemptRun)
	}
	return nil
}

// ConsumptionInput is what a caller supplies to record that an authorization
// was spent.
type ConsumptionInput struct {
	// ApprovalID is the approval being consumed; it must be an approved tool
	// approval.
	ApprovalID string
	// ToolCallID is the tool call the authorization is spent on, the backend's
	// own id for it. Together with ApprovalID it is the receipt's identity.
	ToolCallID string
	// AttemptID must be the approval's own attempt: a receipt proves *that*
	// attempt spent the authorization.
	AttemptID string
	// ConsumedAt is when the call was authorized. Required.
	ConsumedAt time.Time
}

// RecordConsumptionTx writes one consumption receipt and returns it.
//
// This is the one-shot gate of §27.2.5: the caller writes the receipt in the
// same transaction that grants the execution, and only the writer of the row
// proceeds. Two consumers racing for the same approval end with exactly one
// receipt: the second gets ErrAlreadyConsumed, raised either by the check below
// or — if the two really do overlap — by the primary key.
//
// It refuses, before any SQL runs:
//
//   - malformed input (blank ids, a zero ConsumedAt, an id longer than 256
//     bytes or not valid UTF-8 — the bound execbackend uses for a backend
//     reference);
//   - an approval that is not a tool approval, or whose status is not approved.
//     A pending approval has not been decided, a rejected one was refused, and
//     an expired or invalidated one no longer applies; none of them authorize
//     anything;
//   - a tool call that is not the approval's subject: a tool approval is
//     requested for one call and authorizes only that call;
//   - a consumption at or after the approval's deadline. An approved row never
//     becomes expired (it is terminal), so this is where "过期授权拒绝" holds;
//     ConsumedAt is the caller's clock, which is what lets T2.02.c drive the
//     rule with a fake one;
//   - an attempt that is not the approval's own attempt, or whose run is not
//     the run the approval was made for.
//
// Every refusal above is ErrApprovalNotConsumable (or ErrRunProjectMismatch for
// a rewritten attempt row). Migration 009's trg_approval_consumptions_require_grant
// refuses the same receipts in SQL, as the backstop for a statement that skipped
// these checks. The receipt is immutable afterwards: migration 009's triggers
// refuse any UPDATE or DELETE against it.
//
// Note on the (attempt_id, tool_call_id) half of the rule: it is enforced by the
// schema's UNIQUE constraint and reported here as ErrAlreadyConsumed. It becomes
// observable when one call holds two live approved approvals — a request that
// was duplicated and approved twice — and keeps the call from spending both.
func RecordConsumptionTx(ctx context.Context, tx runstore.Tx, in ConsumptionInput) (Consumption, error) {
	if tx == nil {
		return Consumption{}, fmt.Errorf("%w: nil transaction", ErrInvalidApproval)
	}

	approvalID := trimSpace(in.ApprovalID)
	if approvalID == "" {
		return Consumption{}, fmt.Errorf("%w: ApprovalID is required", ErrInvalidApproval)
	}
	toolCallID, err := checkReference("ToolCallID", in.ToolCallID)
	if err != nil {
		return Consumption{}, err
	}
	attemptID := trimSpace(in.AttemptID)
	if attemptID == "" {
		return Consumption{}, fmt.Errorf("%w: AttemptID is required", ErrInvalidApproval)
	}
	if in.ConsumedAt.IsZero() {
		return Consumption{}, fmt.Errorf("%w: ConsumedAt is required", ErrInvalidApproval)
	}
	consumedAt := in.ConsumedAt.UTC()

	stored, err := GetApproval(ctx, tx, approvalID)
	if err != nil {
		return Consumption{}, err
	}
	if stored.SubjectType != SubjectTool {
		return Consumption{}, fmt.Errorf("%w: approval %s authorizes a %s subject, not a tool call",
			ErrApprovalNotConsumable, approvalID, stored.SubjectType)
	}
	if stored.Status != StatusApproved {
		return Consumption{}, fmt.Errorf("%w: approval %s is %s, not approved",
			ErrApprovalNotConsumable, approvalID, stored.Status)
	}
	if stored.SubjectID != toolCallID {
		return Consumption{}, fmt.Errorf("%w: approval %s authorizes tool call %s, not %s",
			ErrApprovalNotConsumable, approvalID, stored.SubjectID, toolCallID)
	}
	// An approved row is terminal and never becomes expired, so the deadline is
	// checked here, when the grant is spent. The deadline itself is outside it.
	if !consumedAt.Before(stored.ExpiresAt) {
		return Consumption{}, fmt.Errorf("%w: approval %s expired at %s; consumption at %s",
			ErrApprovalNotConsumable, approvalID,
			stored.ExpiresAt.Format(time.RFC3339Nano), consumedAt.Format(time.RFC3339Nano))
	}
	if stored.AttemptID != attemptID {
		return Consumption{}, fmt.Errorf("%w: approval %s belongs to attempt %s, not %s",
			ErrApprovalNotConsumable, approvalID, stored.AttemptID, attemptID)
	}
	// The approved attempt must still belong to the approval's run. The
	// approval's own attempt is schema-pinned, so this can only differ if a
	// statement rewrote the attempt row; it is checked because a consumption is
	// the moment the authorization is spent.
	var attemptRun string
	err = tx.QueryRowContext(ctx, `SELECT run_id FROM attempts WHERE id = ?`, attemptID).Scan(&attemptRun)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Consumption{}, fmt.Errorf("%w: no attempt %s", ErrRunProjectMismatch, attemptID)
	case err != nil:
		return Consumption{}, fmt.Errorf("approval: read attempt %s: %w", attemptID, err)
	case attemptRun != stored.RunID:
		return Consumption{}, fmt.Errorf("%w: attempt %s belongs to run %s, approval %s to run %s",
			ErrRunProjectMismatch, attemptID, attemptRun, approvalID, stored.RunID)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO approval_consumptions (approval_id, tool_call_id, attempt_id, consumed_at)
		VALUES (?, ?, ?, ?)`,
		approvalID, toolCallID, attemptID, consumedAt.UnixMilli(),
	)
	if err != nil {
		if isApprovalToolCallDuplicate(err) {
			return Consumption{}, fmt.Errorf("%w: approval %s was already spent on tool call %s",
				ErrAlreadyConsumed, approvalID, toolCallID)
		}
		if isAttemptToolCallDuplicate(err) {
			return Consumption{}, fmt.Errorf("%w: attempt %s already spent an authorization on tool call %s",
				ErrAlreadyConsumed, attemptID, toolCallID)
		}
		// The checks above mirror the receipt trigger, so reaching it means the
		// row changed under this statement; MapRefusal still names the sentinel.
		return Consumption{}, fmt.Errorf("approval: record consumption of %s: %w", approvalID, MapRefusal(err))
	}

	return Consumption{
		ApprovalID: approvalID,
		ToolCallID: toolCallID,
		AttemptID:  attemptID,
		ConsumedAt: consumedAt,
	}, nil
}

// MapRefusal translates a database refusal raised by migration 009's approval
// triggers into the sentinel a caller branches on, and returns err unchanged
// when it is not one of them (including when err is nil).
//
// It exists because this package's store deliberately issues no decision
// statements — deciding, expiring and invalidating are T2.02.b — while the
// triggers that bound those statements are already here. The mapping is by
// stable trigger message and never by driver result code, exactly as runstore
// classifies its own refusals, so T2.02.b does not have to know the message
// text:
//
//	approval_terminal_is_final         -> ErrApprovalNotPending
//	approval_binding_is_frozen         -> ErrApprovalBindingFrozen
//	approval_is_history                -> ErrApprovalIsHistory
//	approval_consumption_is_immutable  -> ErrConsumptionImmutable
//	approval_consumption_without_grant -> ErrApprovalNotConsumable
//
// A caller that reaches one of these has lost a race or issued a statement the
// contract forbids; either way it should read the row again rather than retry
// blindly.
func MapRefusal(err error) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "approval_terminal_is_final"):
		return fmt.Errorf("%w: %w", ErrApprovalNotPending, err)
	case strings.Contains(message, "approval_binding_is_frozen"):
		return fmt.Errorf("%w: %w", ErrApprovalBindingFrozen, err)
	case strings.Contains(message, "approval_is_history"):
		return fmt.Errorf("%w: %w", ErrApprovalIsHistory, err)
	case strings.Contains(message, "approval_consumption_is_immutable"):
		return fmt.Errorf("%w: %w", ErrConsumptionImmutable, err)
	case strings.Contains(message, "approval_consumption_without_grant"):
		return fmt.Errorf("%w: %w", ErrApprovalNotConsumable, err)
	}
	return err
}

// maxReferenceBytes is the bound on an opaque reference this package stores on
// a receipt. It is execbackend.MaxProviderRefBytes, restated here so this
// package does not import execbackend (the rule that keeps the dependency
// direction one-way); a caller with a longer id is out of contract either way.
const maxReferenceBytes = 256

// checkReference validates one opaque reference id: non-empty after trimming,
// valid UTF-8, within the byte bound. The value is returned trimmed so the
// caller stores exactly what was validated.
func checkReference(field, value string) (string, error) {
	trimmed := trimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("%w: %s is required", ErrInvalidApproval, field)
	}
	if !utf8.ValidString(trimmed) {
		return "", fmt.Errorf("%w: %s is not valid UTF-8", ErrInvalidApproval, field)
	}
	if len(trimmed) > maxReferenceBytes {
		return "", fmt.Errorf("%w: %s is %d bytes, limit %d",
			ErrInvalidApproval, field, len(trimmed), maxReferenceBytes)
	}
	return trimmed, nil
}

// selectApprovalColumns is the projection every approval read shares, so a
// column added by a later migration has one place to be added and cannot drift
// between readers.
const selectApprovalColumns = `
	SELECT id, project_id, run_id, attempt_id, subject_type, subject_id, risk, scope_json,
	       fingerprint, fingerprint_input_json, status, policy_version, requested_at,
	       expires_at, decided_by, decided_at, revision
	FROM approvals`

// GetApproval reads one approval by id.
//
// It returns runstore.ErrNotFound when there is no such row, so callers branch
// on one sentinel for "absent" across the runtime library.
func GetApproval(ctx context.Context, q runstore.Querier, id string) (Approval, error) {
	key := trimSpace(id)
	if key == "" {
		return Approval{}, fmt.Errorf("%w: id is required", ErrInvalidApproval)
	}
	row := q.QueryRowContext(ctx, selectApprovalColumns+` WHERE id = ?`, key)
	a, err := scanApproval(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Approval{}, fmt.Errorf("%w: approval %s", runstore.ErrNotFound, key)
	}
	if err != nil {
		return Approval{}, fmt.Errorf("approval: get approval %s: %w", key, err)
	}
	return a, nil
}

// ListApprovalsByRun returns the approvals of one run, oldest first.
//
// The order is (requested_at, id): two approvals requested in the same
// millisecond must still come back in a stable order, and the id is the only
// tiebreak the row owns. A run with no approvals is an empty list, not an
// error.
func ListApprovalsByRun(ctx context.Context, q runstore.Querier, runID string) ([]Approval, error) {
	key := trimSpace(runID)
	if key == "" {
		return nil, fmt.Errorf("%w: run id is required", ErrInvalidApproval)
	}
	return listApprovals(ctx, q, ` WHERE run_id = ? ORDER BY requested_at ASC, id ASC`, key, "approvals of run "+key)
}

// ListPendingApprovalsByProject returns the project's pending approvals, oldest
// first, ordered and tiebroken exactly like ListApprovalsByRun. It is the read
// behind the review queue (the endpoints of §27.3 are T3.04.a; this is the
// storage half).
func ListPendingApprovalsByProject(ctx context.Context, q runstore.Querier, projectID string) ([]Approval, error) {
	key := trimSpace(projectID)
	if key == "" {
		return nil, fmt.Errorf("%w: project id is required", ErrInvalidApproval)
	}
	return listApprovals(ctx, q,
		` WHERE project_id = ? AND status = 'pending' ORDER BY requested_at ASC, id ASC`,
		key, "pending approvals of project "+key)
}

// listApprovals runs one projection query and scans every row.
func listApprovals(ctx context.Context, q runstore.Querier, where string, arg any, what string) ([]Approval, error) {
	rows, err := q.QueryContext(ctx, selectApprovalColumns+where, arg)
	if err != nil {
		return nil, fmt.Errorf("approval: list %s: %w", what, err)
	}
	defer rows.Close()

	var out []Approval
	for rows.Next() {
		a, err := scanApproval(rows)
		if err != nil {
			return nil, fmt.Errorf("approval: scan %s: %w", what, err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("approval: iterate %s: %w", what, err)
	}
	return out, nil
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

// scanApproval reads one projected row. Timestamps cross this boundary exactly
// once: the columns are Unix milliseconds, the struct fields are time in UTC.
func scanApproval(s scanner) (Approval, error) {
	var (
		a                         Approval
		runID, attemptID          sql.NullString
		subjectType, risk, status string
		decidedBy                 sql.NullString
		requestedAt, expiresAt    int64
		decidedAt                 sql.NullInt64
	)
	if err := s.Scan(
		&a.ID, &a.ProjectID, &runID, &attemptID, &subjectType, &a.SubjectID, &risk, &a.ScopeJSON,
		&a.Fingerprint, &a.FingerprintInputJSON, &status, &a.PolicyVersion, &requestedAt,
		&expiresAt, &decidedBy, &decidedAt, &a.Revision,
	); err != nil {
		return Approval{}, err
	}
	a.RunID = runID.String
	a.AttemptID = attemptID.String
	a.SubjectType = SubjectType(subjectType)
	a.Risk = Risk(risk)
	a.Status = Status(status)
	a.RequestedAt = time.UnixMilli(requestedAt).UTC()
	a.ExpiresAt = time.UnixMilli(expiresAt).UTC()
	if decidedAt.Valid {
		a.DecidedAt = time.UnixMilli(decidedAt.Int64).UTC()
	}
	a.DecidedBy = decidedBy.String
	return a, nil
}

// nullableString maps "" to SQL NULL, as runstore does for its own optional
// columns.
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// isApprovalToolCallDuplicate reports whether err is the refusal of the
// (approval_id, tool_call_id) primary key. It matches the column list, not
// "UNIQUE constraint failed" in general: the same class of error is raised by
// the (attempt_id, tool_call_id) constraint, and the two mean different things
// to a caller reading the error.
func isApprovalToolCallDuplicate(err error) bool {
	return uniqueViolationOn(err,
		"approval_consumptions.approval_id", "approval_consumptions.tool_call_id")
}

// isAttemptToolCallDuplicate reports whether err is the refusal of the
// (attempt_id, tool_call_id) unique constraint.
func isAttemptToolCallDuplicate(err error) bool {
	return uniqueViolationOn(err,
		"approval_consumptions.attempt_id", "approval_consumptions.tool_call_id")
}

// uniqueViolationOn reports whether err is a duplicate-key refusal naming every
// column in columns.
//
// It matches the column list rather than "UNIQUE constraint failed" alone,
// because the driver (modernc.org/sqlite) spells the columns out as
// "UNIQUE constraint failed: approval_consumptions.approval_id,
// approval_consumptions.tool_call_id" (verified against the pinned engine; see
// runstore/schema_test.go's version assertion). Matching the text keeps this
// package free of a driver import, which is the same reason runstore classifies
// its refusals by message. The two constraints are told apart by the column list
// they name, so the match must not be loosened to a bare prefix check.
func uniqueViolationOn(err error, columns ...string) bool {
	if err == nil {
		return false
	}
	upper := strings.ToUpper(err.Error())
	if !strings.Contains(upper, "UNIQUE CONSTRAINT FAILED") {
		return false
	}
	for _, column := range columns {
		if !strings.Contains(upper, strings.ToUpper(column)) {
			return false
		}
	}
	return true
}
