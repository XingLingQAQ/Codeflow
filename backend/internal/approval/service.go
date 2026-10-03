// Approval lifecycle: request, decide, expire, invalidate (T2.02.b, plan §28
// T2.02.b).
//
// store.go owns the *facts* — the approval row, its frozen binding and the
// one-shot consumption receipt. This file owns the *lifecycle* over those
// facts. Every function takes the caller's runstore.Tx and opens no transaction
// of its own, exactly like CreateApprovalTx: §19.3 requires the state change,
// the event and the outbox row to commit or roll back together, and only the
// caller's transaction can promise that. Time is a parameter (Now), never the
// system clock, so a test can drive expiry with a fake clock.
//
//	RequestToolApprovalTx        a tool call asks to run; low risk is
//	                             auto-approved, everything else is pending and
//	                             the Run waits
//	DecideTx                     a human answers, with a CAS on the revision
//	ExpireDueTx                  the deadline passed; the request stops applying
//	InvalidatePendingForRunTx    the Run ended, so its pending requests stop
//	                             applying (§27.2.5)
//	ResolveDeniedTx              the refusal reached the backend, so the Run may
//	                             continue without the tool having run (CA-3)
//
// consume.go is the sixth step, the one that actually grants execution.
//
// Migration 009 makes the CAS structural rather than conventional: a decided
// row refuses every UPDATE (trg_approvals_terminal_is_final), so a lost race
// cannot overwrite or revive a decision even if this file were wrong, and a
// pending row's binding is frozen (trg_approvals_binding_is_frozen), so a
// changed parameter set must become a new approval rather than an edited one.
package approval

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codeflow/backend/internal/run"
	"github.com/codeflow/backend/internal/runstore"
)

// Lifecycle errors. Each names one distinguishable outcome a caller branches
// on; ErrApprovalFlowRefused is the one sentinel that classifies "the approval
// flow said no" without enumerating reasons.
var (
	// ErrApprovalFlowRefused is the class of every refusal this file returns,
	// so a caller can tell a flow refusal from a storage failure without
	// matching every reason — and so a reason added later is not mistaken for
	// a crash by a caller that only knows the old set.
	ErrApprovalFlowRefused = errors.New("approval: flow refused")

	// ErrApprovalConflict means the caller's view of an approval is stale: the
	// stored revision is not the expected one, the row is no longer pending,
	// or a Run to move is no longer in the status the move needs. The current
	// row is returned alongside it so the loser of a race reads what actually
	// happened instead of guessing (§27.2.5 "输方读取当前状态，不反向复活终态").
	// Nothing was written.
	ErrApprovalConflict = errors.New("approval: approval changed under the caller")

	// ErrApprovalNotRunning means a tool approval was requested for a Run that
	// cannot ask: not running, or already waiting for a tool approval. The
	// second case is the S1 rule — at most one pending tool approval per Run —
	// and the whole request writes nothing, not even the approval row, so a
	// refused request never leaves a pending row no Run is waiting on.
	ErrApprovalNotRunning = errors.New("approval: run cannot request approval in its current state")

	// ErrApprovalIdentityMismatch means the caller's claim about the subject
	// disagrees with the authoritative rows: a project, attempt or agent
	// revision that is not the stored one. §27.4 requires the server to derive
	// the parent from the authoritative resource, so the mismatch is refused
	// rather than corrected.
	ErrApprovalIdentityMismatch = errors.New("approval: request identity does not match the stored run")

	// ErrApprovalExpired means the deadline passed before a decision. The row
	// is turned into `expired` (decided_by "system:expiry") in the same
	// transaction, so a pending request is never left pending past its
	// deadline, and the answer is no: a decision can never grant an approval
	// that has expired.
	//
	// This is the one refusal that is an outcome rather than a failure: it is
	// returned as expiringErr, which carries the expired row and reports true
	// from RefusalWrote — the caller must COMMIT the transaction, because the
	// expiry is the thing the call was for (§28 T2.02.b "过期决定保留历史").
	// A caller that rolls back because an error came back would lose the
	// expired fact and find the row still pending.
	ErrApprovalExpired = errors.New("approval: approval expired before the decision")

	// ErrApprovalAlreadyDecided means the row is no longer pending: either this
	// exact decision was already recorded (a repeated call is reported as the
	// idempotent success it is), or another decision, expiry or invalidation
	// got there first. The current row is returned so the caller can say which.
	ErrApprovalAlreadyDecided = errors.New("approval: approval is already decided")

	// ErrApprovalNoRun means the caller named a Run for a subject that has
	// none, or asked to move a Run for an approval that is not bound to one.
	ErrApprovalNoRun = errors.New("approval: approval is not bound to a run")

	// ErrApprovalPolicyDenied means ConsumeTx's policy evaluation did not allow
	// the operation, or its rule version differs from the approval's
	// policy_version. Both are one outcome to the caller — the approval no
	// longer authorizes anything (§27.2.3) — and neither writes a row.
	ErrApprovalPolicyDenied = errors.New("approval: policy no longer permits this operation")

	// ErrApprovalNoReceipt means this consumer did not win the one-shot race:
	// ConsumeTx returns Granted=false, and the wrapped cause names why (for
	// example ErrAlreadyConsumed, or any refusal above). Only the transaction
	// that wrote the receipt is granted.
	ErrApprovalNoReceipt = errors.New("approval: consumption was not granted")
)

// systemActor is the actor of every decision the service takes itself: the
// automatic low-risk approval, and every way a request stops applying. The
// type is system — the server decided, not a user — and the stored decided_by
// says which rule decided ("system:policy", "system:expiry", "system:run").
var systemActor = run.Actor{Type: run.ActorTypeSystem, ID: "approval-service", Source: "approval"}

// expiringError is the error DecideTx returns when it found the approval past
// its deadline and wrote `expired` as the outcome.
//
// It exists because that one refusal is not a failure: the write succeeded, and
// the caller must let the transaction commit for the expiry to survive. Instead
// of asking every caller to special-case a sentinel by name — which reads like
// an exception to the "an error means roll back" rule and is exactly how the
// fact gets lost — the error itself says what happened:
//
//	if err := st.WithTx(ctx, func(ctx, tx) error {
//	    _, err := approval.DecideTx(ctx, tx, in)
//	    if approval.RefusalWrote(err) {
//	        return nil // the expiry is the outcome; commit it
//	    }
//	    return err
//	}); ... // then branch on errors.Is(err, approval.ErrApprovalExpired)
//
// The alternative — returning the expired row with a nil error — was rejected
// because a caller that asked to approve must never see the call succeed: the
// answer is no, and success must be reserved for the two outcomes the caller
// asked for.
type expiringError struct {
	// Approval is the row as it now stands: expired, with its revision bumped
	// and its agency recorded as system:expiry.
	Approval Approval
}

// Error implements error.
func (e *expiringError) Error() string {
	return fmt.Sprintf("%s: approval %s expired at %s and was marked expired; commit this transaction",
		ErrApprovalExpired, e.Approval.ID, e.Approval.ExpiresAt.Format(time.RFC3339Nano))
}

// Is makes errors.Is(err, ErrApprovalExpired) true; the wrapped row stays
// reachable through RefusalWrote.
func (e *expiringError) Is(target error) bool { return target == ErrApprovalExpired }

// RefusalWrote reports whether a refusal write took effect and the caller must
// commit the transaction it is in.
//
// It is true only for DecideTx's expiry outcome (ErrApprovalExpired), whose row
// carries the expired state that was written. Every other refusal this package
// returns writes nothing and is true only to be rolled back when the caller
// returns it.
func RefusalWrote(err error) bool {
	var expired *expiringError
	return errors.As(err, &expired)
}

// ExpiredApproval returns the expired row from an ErrApprovalExpired refusal,
// and false for any other error.
func ExpiredApproval(err error) (Approval, bool) {
	var expired *expiringError
	if errors.As(err, &expired) {
		return expired.Approval, true
	}
	return Approval{}, false
}

// ToolAction is one tool call a caller wants authorized: the input of the risk
// table (risk.go) and of nothing else. The binding that gets stored is a
// FingerprintInput, assembled from the authoritative rows by this package.
type ToolAction struct {
	// Tool is the tool's name as the backend reports it ("run_shell",
	// "write_file", "read_file"). It decides the risk of an action with no
	// command.
	Tool string
	// Command is the argv the action would run, in order, or nil for a tool
	// that runs no command. RequestToolApprovalTx requires it to be exactly
	// FingerprintInput.Command: the risk is classified from the command the
	// approval binds, never from a second description of it.
	Command []string
	// TargetPaths are the files the action would touch, as reported by the
	// caller. Classification evidence only; the fingerprint binds the
	// canonicalized set passed separately in FingerprintInput.
	TargetPaths []string
}

// ToolSubject is the authoritative identity of a tool approval's subject: what
// the run, attempt and agent revision rows say, not what the caller claimed.
type ToolSubject struct {
	ProjectID       string
	RunID           string
	AttemptID       string
	AgentRevisionID string
}

// RequestToolInput is one request to authorize a tool call.
type RequestToolInput struct {
	// ID is the approval id to create. Required, caller-owned like every id in
	// the runtime library.
	ID string
	// ProjectID, RunID, AttemptID and AgentRevisionID are what the caller
	// believes about the subject. They are not trusted: every one is compared
	// against the run and attempt rows, and a mismatch is
	// ErrApprovalIdentityMismatch. RunID and AttemptID are required — a tool
	// approval always belongs to both (migration 009's CHECK).
	ProjectID       string
	RunID           string
	AttemptID       string
	AgentRevisionID string
	// SubjectID is the tool call id this approval authorizes, and the id a
	// consumption must present. Required.
	SubjectID string
	// Action is the tool call, for the risk table.
	Action ToolAction
	// FingerprintInput is what the approval binds. ProjectID, SubjectType,
	// SubjectID and AgentRevisionID are overwritten from the authoritative
	// rows before hashing, so a caller cannot bind one identity and claim
	// another; the arguments hash, command, cwd, target paths, base manifest
	// and policy version are hashed as given.
	FingerprintInput FingerprintInput
	// Scope is the requested scope as JSON; the store normalizes it.
	Scope []byte
	// RiskConfig is the project's trusted-command configuration.
	RiskConfig RiskConfig
	// PolicyVersion is the policy version in force. Required, and it must
	// equal FingerprintInput.PolicyVersion.
	PolicyVersion string
	// RequestedAt is when the call asked. Required.
	RequestedAt time.Time
	// ExpiresAt is the deadline. Required, and strictly after RequestedAt.
	ExpiresAt time.Time
}

// RequestToolResult is the outcome of an accepted tool request.
type RequestToolResult struct {
	// Approval is the stored row: pending, or approved with DecidedBy
	// "system:policy" when the risk was low.
	Approval Approval
	// Risk and Reasons are the classification the service computed. A caller
	// that disagrees cannot override it; it can only refuse to proceed.
	Risk    Risk
	Reasons []string
	// AutoApproved is true exactly when the risk was low and the service
	// approved in the same transaction. The Run was not moved: a low-risk call
	// does not interrupt the user (§15 T2.02 "低风险不弹窗").
	AutoApproved bool
	// Event is the event written in the same transaction: approval.decided for
	// an auto-approval, approval.required for a pending request.
	Event runstore.Event
	// Transition is the Run transition for a pending request. Zero for an
	// auto-approval, which writes no transition.
	Transition run.Transition
}

// RequestToolApprovalTx requests authorization for one tool call, inside the
// caller's transaction.
//
// Order: read the authoritative rows, classify, create the approval, then move
// the Run if the call must wait. All in one transaction, so a Run waiting for
// an approval always has the pending row its event points at, and an
// auto-approved call never leaves an approval.required behind.
//
//   - An Action.Command that is not exactly FingerprintInput.Command is
//     ErrInvalidApproval: the service classifies the risk itself only because
//     a caller must not name its own, and that holds only if the command it
//     classifies is the command the approval binds.
//   - An identity that disagrees with the stored run/attempt/revision is
//     ErrApprovalIdentityMismatch, before any row is written.
//   - A Run that is neither `running` nor `waiting_approval` is
//     ErrApprovalNotRunning. Nothing is written.
//   - A low-risk call is approved by the service itself (decided_by
//     "system:policy", reason the classification) and the Run is not moved.
//     That includes a Run already waiting for another call: a backend may issue
//     tool calls in parallel, and a read of the same turn does not need the
//     human the other call waits for.
//   - Anything else becomes pending and moves the Run to waiting_approval with
//     approval.required, in the same transaction (§21.1). Only a `running` Run
//     can do that: a Run already waiting has its one pending tool approval
//     (S1: at most one per Run; the second call is refused rather than queued
//     behind a row nobody reads), and the refusal writes nothing.
func RequestToolApprovalTx(ctx context.Context, tx runstore.Tx, in RequestToolInput) (RequestToolResult, error) {
	if tx == nil {
		return RequestToolResult{}, fmt.Errorf("%w: nil transaction", ErrInvalidApproval)
	}
	if trimSpace(in.SubjectID) == "" {
		return RequestToolResult{}, fmt.Errorf("%w: SubjectID is required", ErrInvalidApproval)
	}
	if !sameArgv(in.Action.Command, in.FingerprintInput.Command) {
		return RequestToolResult{}, fmt.Errorf(
			"%w: Action.Command %q is not the bound FingerprintInput.Command %q; the risk is classified from the command the approval binds",
			ErrInvalidApproval, in.Action.Command, in.FingerprintInput.Command)
	}

	subject, runStatus, err := resolveToolSubject(ctx, tx, in)
	if err != nil {
		return RequestToolResult{}, err
	}

	risk, reasons := ClassifyToolRisk(in.Action, in.RiskConfig)
	if risk != RiskLow && runStatus != run.RunStatusRunning {
		return RequestToolResult{}, fmt.Errorf(
			"%w: run %s is %s and already waits for a tool approval; a %s-risk call must wait its turn",
			ErrApprovalNotRunning, subject.RunID, runStatus, risk)
	}

	// The binding is assembled from the authoritative rows, not from the
	// caller's claims. Only the action's own parameters — arguments hash, argv,
	// cwd, target paths, base manifest — come from the request, and those are
	// exactly what a human is being asked to approve.
	fingerprintInput := in.FingerprintInput
	fingerprintInput.ProjectID = subject.ProjectID
	fingerprintInput.SubjectType = SubjectTool
	fingerprintInput.SubjectID = trimSpace(in.SubjectID)
	fingerprintInput.AgentRevisionID = subject.AgentRevisionID

	stored, err := CreateApprovalTx(ctx, tx, NewApproval{
		ID:               in.ID,
		ProjectID:        subject.ProjectID,
		RunID:            subject.RunID,
		AttemptID:        subject.AttemptID,
		SubjectType:      SubjectTool,
		SubjectID:        trimSpace(in.SubjectID),
		Risk:             risk,
		Scope:            in.Scope,
		FingerprintInput: fingerprintInput,
		RequestedAt:      in.RequestedAt,
		ExpiresAt:        in.ExpiresAt,
		PolicyVersion:    in.PolicyVersion,
	})
	if err != nil {
		// The store's own sentinels (ErrInvalidApproval, ErrRunProjectMismatch,
		// ErrAgentRevisionMismatch, ...) travel through unchanged: they already
		// name what is wrong.
		return RequestToolResult{}, err
	}

	if risk == RiskLow {
		// §15 T2.02: a low-risk call does not stop the Run and does not ask a
		// human. The approval is still a real row with a real decision, so a
		// later consumption goes through the same one-shot receipt as a human
		// approval.
		decided, event, err := writeDecisionTx(ctx, tx, decisionWrite{
			Approval:  stored,
			Status:    StatusApproved,
			DecidedBy: "system:policy",
			At:        in.RequestedAt.UTC(),
			Actor:     systemActor,
			Reason:    "low risk: " + strings.Join(reasons, "; "),
		})
		if err != nil {
			return RequestToolResult{}, err
		}
		return RequestToolResult{
			Approval:     decided,
			Risk:         risk,
			Reasons:      reasons,
			AutoApproved: true,
			Event:        event,
		}, nil
	}

	// S1: at most one pending tool approval per Run. A waiting Run was already
	// refused above; this catches a pending row next to a `running` Run, which
	// only an administrative write can produce. Checked after the row exists
	// but before anything else is written, because the check needs the whole
	// set of this Run's approvals; a refusal rolls the new row back with the
	// caller's transaction, so it leaves no trace.
	siblings, err := ListApprovalsByRun(ctx, tx, subject.RunID)
	if err != nil {
		return RequestToolResult{}, err
	}
	for _, a := range siblings {
		if a.SubjectType == SubjectTool && a.Status == StatusPending && a.ID != stored.ID {
			return RequestToolResult{}, fmt.Errorf(
				"%w: run %s already has a pending tool approval %s", ErrApprovalNotRunning, subject.RunID, a.ID)
		}
	}

	transition, err := moveRun(ctx, tx, moveRunInput{
		RunID:           subject.RunID,
		ExpectedStatus:  run.RunStatusRunning,
		Trigger:         run.EventTrigger(string(run.EventApprovalRequired)),
		At:              in.RequestedAt.UTC(),
		Actor:           systemActor,
		AttemptID:       ptr(subject.AttemptID),
		AgentRevisionID: ptr(subject.AgentRevisionID),
		Details: map[string]any{
			"approval_id":  stored.ID,
			"tool_call_id": stored.SubjectID,
			"risk":         string(risk),
		},
		Destinations: runDestinations(subject.ProjectID, subject.RunID),
	})
	if err != nil {
		return RequestToolResult{}, err
	}
	return RequestToolResult{
		Approval:   stored,
		Risk:       risk,
		Reasons:    reasons,
		Event:      derefEvent(transition.Event),
		Transition: transition.Transition,
	}, nil
}

// RequestApprovalInput is one request to authorize a gate or a merge.
type RequestApprovalInput struct {
	// ID is the approval id to create. Required.
	ID string
	// ProjectID is the owning project. Required.
	ProjectID string
	// RunID and AttemptID are optional: §19.1 allows a stage gate to have no
	// Run. When a Run is named it must belong to ProjectID.
	RunID     string
	AttemptID string
	// SubjectType must be SubjectGate or SubjectMerge.
	SubjectType SubjectType
	// SubjectID is the stage id or merge candidate id. Required.
	SubjectID string
	// Risk is the classification the caller (a gate/merge flow, not the tool
	// table) determined. Required and validated by the store.
	Risk Risk
	// Scope is the requested scope as JSON.
	Scope []byte
	// FingerprintInput is hashed as given, except that ProjectID, SubjectType
	// and SubjectID are taken from this input and must agree with it.
	FingerprintInput FingerprintInput
	// RequestedAt and ExpiresAt are required; the expiry is strictly after the
	// request.
	RequestedAt time.Time
	ExpiresAt   time.Time
	// PolicyVersion is the policy version in force. Required.
	PolicyVersion string
}

// RequestApprovalTx creates a pending approval for a gate or a merge.
//
// It writes no Run transition and no approval.required: that event is a Run
// state event, legal only from `running`, and a gate decision — or a gate
// request — may exist with no Run at all (§27.1). The decision path is shared
// with the tool flow (DecideTx), and so is the consumption rule for a merge
// (consume.go).
func RequestApprovalTx(ctx context.Context, tx runstore.Tx, in RequestApprovalInput) (Approval, error) {
	if tx == nil {
		return Approval{}, fmt.Errorf("%w: nil transaction", ErrInvalidApproval)
	}
	switch in.SubjectType {
	case SubjectGate, SubjectMerge:
	case SubjectTool:
		return Approval{}, fmt.Errorf("%w: use RequestToolApprovalTx for a tool subject", ErrInvalidApproval)
	default:
		return Approval{}, fmt.Errorf("%w: SubjectType %q is not valid", ErrInvalidApproval, in.SubjectType)
	}
	if runID := trimSpace(in.RunID); runID != "" {
		runRow, err := runstore.GetRun(ctx, tx, runID)
		if err != nil {
			return Approval{}, fmt.Errorf("approval: read run %s: %w", runID, err)
		}
		if runRow.ProjectID != trimSpace(in.ProjectID) {
			return Approval{}, fmt.Errorf("%w: run %s belongs to project %s, the request to project %s",
				ErrApprovalProjectMismatch, runRow.ID, runRow.ProjectID, in.ProjectID)
		}
	}

	fingerprintInput := in.FingerprintInput
	fingerprintInput.ProjectID = trimSpace(in.ProjectID)
	fingerprintInput.SubjectType = in.SubjectType
	fingerprintInput.SubjectID = trimSpace(in.SubjectID)

	return CreateApprovalTx(ctx, tx, NewApproval{
		ID:               in.ID,
		ProjectID:        in.ProjectID,
		RunID:            in.RunID,
		AttemptID:        in.AttemptID,
		SubjectType:      in.SubjectType,
		SubjectID:        in.SubjectID,
		Risk:             in.Risk,
		Scope:            in.Scope,
		FingerprintInput: fingerprintInput,
		RequestedAt:      in.RequestedAt,
		ExpiresAt:        in.ExpiresAt,
		PolicyVersion:    in.PolicyVersion,
	})
}

// DecisionInput is one human (or policy) answer to a pending approval.
type DecisionInput struct {
	// ApprovalID is the approval being answered. Required.
	ApprovalID string
	// ExpectedRevision is the revision the caller read. Required, >= 1: the
	// decision is taken against exactly the row the human saw.
	ExpectedRevision int64
	// Approved is the explicit answer. There is no default: false is a
	// rejection, not "no answer".
	Approved bool
	// DecidedBy is the decider, free text (a user id, an integration id). An
	// empty value falls back to Actor.ID.
	DecidedBy string
	// Actor is the identity the approval.decided event is written under.
	// Required — a decision nobody is accountable for is not a decision.
	Actor run.Actor
	// Now is the decision instant, and the deadline it is compared against.
	// Required.
	Now time.Time
	// Comment is the decider's optional note (§27.3's `reason` field of
	// POST /approvals/:aid/decide). It is written into the approval.decided
	// payload as "comment"; it never changes the decision. At most
	// MaxDecisionCommentRunes runes after trimming.
	Comment string
}

// MaxDecisionCommentRunes bounds a decision comment, so a note cannot push the
// approval.decided event past the event size limit.
const MaxDecisionCommentRunes = 1000

// DecideTx answers a pending approval.
//
// The decision is a compare-and-set on the revision: a caller whose
// ExpectedRevision is not the stored revision gets ErrApprovalConflict and the
// current row, and nothing is written — the loser of a concurrent
// decide/invalidate race reads what happened instead of overwriting it
// (§27.2.5). A row that is no longer pending is refused the same way, except
// when the stored fact already is this caller's decision, which is reported as
// the idempotent success it is: re-answering "approve" after a successful
// approve must not look like a conflict to a retrying client.
//
// The deadline is checked before the answer is applied: Now at or past
// ExpiresAt turns the row into `expired` (decided_by "system:expiry") in the
// same transaction and returns ErrApprovalExpired. A decision can therefore
// never grant an approval whose deadline has passed; the row's history keeps
// the expiry, and consumption would refuse the grant anyway (store.go).
//
// ErrApprovalExpired is the one error where the caller must still COMMIT the
// transaction: the expiry was written as the outcome (§28 T2.02.b "过期决定保留
// 历史"), not as a failure, and rolling it back would leave the row pending
// past its deadline. The error itself says so — RefusalWrote(err) reports true
// and ExpiredApproval(err) returns the row — so a caller needs one branch:
//
//	if err := st.WithTx(ctx, func(ctx, tx) error {
//	    _, err := approval.DecideTx(ctx, tx, in)
//	    if approval.RefusalWrote(err) {
//	        return nil // the expiry is the outcome; commit it
//	    }
//	    return err
//	}); errors.Is(err, approval.ErrApprovalExpired) { /* answered "no" */ }
//
// Every other error means nothing was written and a rollback loses nothing.
//
// The Run is not moved here: §21.1 moves it when the call is actually granted
// (consume.go, approval.approved) or when the refusal has been delivered to the
// backend (ResolveDeniedTx, approval.denied). Moving it at decision time would
// let a Run resume on an approval that is never consumed.
func DecideTx(ctx context.Context, tx runstore.Tx, in DecisionInput) (Approval, error) {
	if tx == nil {
		return Approval{}, fmt.Errorf("%w: nil transaction", ErrInvalidApproval)
	}
	approvalID := trimSpace(in.ApprovalID)
	if approvalID == "" {
		return Approval{}, fmt.Errorf("%w: ApprovalID is required", ErrInvalidApproval)
	}
	if in.ExpectedRevision < 1 {
		return Approval{}, fmt.Errorf("%w: ExpectedRevision %d must be >= 1", ErrInvalidApproval, in.ExpectedRevision)
	}
	if in.Now.IsZero() {
		return Approval{}, fmt.Errorf("%w: Now is required", ErrInvalidApproval)
	}
	if err := validateLifecycleActor(in.Actor); err != nil {
		return Approval{}, err
	}
	comment := trimSpace(in.Comment)
	if n := utf8.RuneCountInString(comment); n > MaxDecisionCommentRunes {
		return Approval{}, fmt.Errorf("%w: Comment is %d runes, limit is %d",
			ErrInvalidApproval, n, MaxDecisionCommentRunes)
	}

	current, err := GetApproval(ctx, tx, approvalID)
	if err != nil {
		return Approval{}, err
	}

	if current.Status != StatusPending {
		if isSameDecision(current, in) {
			return current, nil
		}
		return current, fmt.Errorf("%w: approval %s is %s", ErrApprovalAlreadyDecided, current.ID, current.Status)
	}
	if current.Revision != in.ExpectedRevision {
		return current, fmt.Errorf("%w: approval %s is at revision %d, the caller expected %d",
			ErrApprovalConflict, current.ID, current.Revision, in.ExpectedRevision)
	}

	now := in.Now.UTC()
	if !now.Before(current.ExpiresAt) {
		// The deadline passed before anyone answered. The request stops
		// applying and the row keeps that fact — "过期决定保留历史"
		// (§28 T2.02.b). Not a rejection: nobody decided. Never an approval.
		//
		// The write is an outcome, not a failure: the error returned below is
		// expiringErr, which reports true from RefusalWrote, and the caller
		// must commit the transaction it is in for the expiry to survive.
		expired, _, err := writeDecisionTx(ctx, tx, decisionWrite{
			Approval:  current,
			Status:    StatusExpired,
			DecidedBy: "system:expiry",
			At:        now,
			Actor:     systemActor,
			Reason:    "deadline passed before the decision",
		})
		if err != nil {
			return Approval{}, err
		}
		return expired, &expiringError{Approval: expired}
	}

	decidedBy := trimSpace(in.DecidedBy)
	if decidedBy == "" {
		decidedBy = trimSpace(in.Actor.ID)
	}
	status, reason := StatusRejected, "rejected"
	if in.Approved {
		status, reason = StatusApproved, "approved"
	}

	decided, _, err := writeDecisionTx(ctx, tx, decisionWrite{
		Approval:  current,
		Status:    status,
		DecidedBy: decidedBy,
		At:        now,
		Actor:     in.Actor,
		Reason:    reason,
		Comment:   comment,
	})
	return decided, err
}

// decisionWrite is one decision update plus the event that records it.
type decisionWrite struct {
	Approval Approval
	Status   Status
	// DecidedBy is stored verbatim in decided_by.
	DecidedBy string
	At        time.Time
	// Actor is the identity of the approval.decided event.
	Actor run.Actor
	// Reason is the payload's reason: the answer itself, or why the request
	// stopped applying.
	Reason string
	// Comment is the decider's note, written as the payload's "comment" when
	// present.
	Comment string
}

// writeDecisionTx is the one place a decision is written: the status change,
// the approval.decided event and its outbox rows, all in the caller's
// transaction (§19.3 item 2).
//
// It refuses a status that is not terminal (a decision is always final) and a
// missing decider before any SQL. The write is a single UPDATE constrained by
// `status = 'pending'` and the revision that was read, so a decision can never
// land on a row somebody else already answered — migration 009's trigger
// refuses it in SQL even if this file were wrong. A statement that matched no
// row is read back and reported as ErrApprovalConflict with the current facts.
func writeDecisionTx(ctx context.Context, tx runstore.Tx, w decisionWrite) (Approval, runstore.Event, error) {
	if !w.Status.IsTerminal() {
		return Approval{}, runstore.Event{}, fmt.Errorf("%w: a decision status must be terminal, got %q",
			ErrInvalidApproval, w.Status)
	}
	decidedBy := trimSpace(w.DecidedBy)
	if decidedBy == "" {
		return Approval{}, runstore.Event{}, fmt.Errorf("%w: DecidedBy is required", ErrInvalidApproval)
	}
	if w.At.IsZero() {
		return Approval{}, runstore.Event{}, fmt.Errorf("%w: decision time is required", ErrInvalidApproval)
	}
	if err := validateLifecycleActor(w.Actor); err != nil {
		return Approval{}, runstore.Event{}, err
	}
	before := w.Approval
	now := w.At.UTC()

	res, err := tx.ExecContext(ctx, `
		UPDATE approvals
		SET status = ?, decided_by = ?, decided_at = ?, revision = revision + 1
		WHERE id = ? AND status = 'pending' AND revision = ?`,
		string(w.Status), decidedBy, now.UnixMilli(), before.ID, before.Revision)
	if err != nil {
		return Approval{}, runstore.Event{}, fmt.Errorf("approval: decide %s: %w", before.ID, MapRefusal(err))
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return Approval{}, runstore.Event{}, fmt.Errorf("approval: decide %s: %w", before.ID, err)
	}
	if affected == 0 {
		current, readErr := GetApproval(ctx, tx, before.ID)
		if readErr != nil {
			return Approval{}, runstore.Event{}, readErr
		}
		return current, runstore.Event{}, fmt.Errorf(
			"%w: approval %s is %s at revision %d, the caller saw pending at revision %d",
			ErrApprovalConflict, before.ID, current.Status, current.Revision, before.Revision)
	}

	stored := before
	stored.Status = w.Status
	stored.DecidedBy = decidedBy
	stored.DecidedAt = now
	stored.Revision = before.Revision + 1

	event, err := appendDecisionEventTx(ctx, tx, stored, w.Actor, now, w.Reason, w.Comment)
	if err != nil {
		return Approval{}, runstore.Event{}, err
	}
	return stored, event, nil
}

// appendDecisionEventTx writes the approval.decided event for a decision.
//
// The event names the Run when there is one (approval.decided itself requires
// none: a Gate decision may exist with no Run, §27.1), the approval's attempt
// when there is one, and a payload with the identity of what was decided: the
// approval, its subject, the resulting status, the new revision, the
// fingerprint and the risk. An audit reader with only the event stream can tell
// what was approved without joining a table a later retention job may have
// trimmed.
func appendDecisionEventTx(ctx context.Context, tx runstore.Tx, stored Approval, actor run.Actor, at time.Time, reason, comment string) (runstore.Event, error) {
	payload := map[string]any{
		"approval_id":  stored.ID,
		"subject_type": string(stored.SubjectType),
		"subject_id":   stored.SubjectID,
		"status":       string(stored.Status),
		"revision":     stored.Revision,
		"fingerprint":  stored.Fingerprint,
		"risk":         string(stored.Risk),
		"decided_by":   stored.DecidedBy,
	}
	if r := trimSpace(reason); r != "" {
		payload["reason"] = r
	}
	if c := trimSpace(comment); c != "" {
		payload["comment"] = c
	}
	if stored.RunID != "" {
		payload["run_id"] = stored.RunID
	}
	encoded, err := marshalJSONObject(payload)
	if err != nil {
		return runstore.Event{}, err
	}

	event, err := runstore.AppendEventTx(ctx, tx, runstore.EventInput{
		ProjectID:    stored.ProjectID,
		RunID:        optionalString(stored.RunID),
		AttemptID:    optionalString(stored.AttemptID),
		Type:         string(run.EventApprovalDecided),
		OccurredAt:   at.UTC(),
		Identity:     identityJSON(stored.ProjectID, stored.RunID, stored.AttemptID, actor),
		Payload:      encoded,
		Destinations: runDestinations(stored.ProjectID, stored.RunID),
	})
	if err != nil {
		return runstore.Event{}, fmt.Errorf("approval: write %s for %s: %w",
			run.EventApprovalDecided, stored.ID, err)
	}
	return event, nil
}

// ExpireDueTx turns every pending approval of a project whose deadline has
// passed into `expired` (decided_by "system:expiry", one approval.decided event
// each) and returns what it expired.
//
// It is the sweep a background job runs, with the caller's clock so tests drive
// it. An already approved row is untouched: a decision is final and keeps its
// history (§28 T2.02.b "过期决定保留历史，不原地改成新批准"); its deadline is
// enforced where the grant is spent — RecordConsumptionTx refuses a
// past-deadline approval (store.go), which is where "过期授权拒绝" holds for a
// grant that was never consumed.
//
// Expiring a pending *tool* approval is only half the job: the Run is still
// waiting for an answer it will never get. The caller delivers the refusal to
// the backend (execbackend Session.Approve with Approved=false) and then calls
// ResolveDeniedTx. The returned slice names which approvals those are.
func ExpireDueTx(ctx context.Context, tx runstore.Tx, projectID string, now time.Time) ([]Approval, error) {
	if tx == nil {
		return nil, fmt.Errorf("%w: nil transaction", ErrInvalidApproval)
	}
	key := trimSpace(projectID)
	if key == "" {
		return nil, fmt.Errorf("%w: project id is required", ErrInvalidApproval)
	}
	if now.IsZero() {
		return nil, fmt.Errorf("%w: Now is required", ErrInvalidApproval)
	}

	pending, err := listPendingApprovalsTx(ctx, tx, key)
	if err != nil {
		return nil, err
	}
	out := make([]Approval, 0, len(pending))
	for _, a := range pending {
		if now.UTC().Before(a.ExpiresAt) {
			continue
		}
		stored, _, err := writeDecisionTx(ctx, tx, decisionWrite{
			Approval:  a,
			Status:    StatusExpired,
			DecidedBy: "system:expiry",
			At:        now.UTC(),
			Actor:     systemActor,
			Reason:    "expired",
		})
		if err != nil {
			return nil, err
		}
		out = append(out, stored)
	}
	return out, nil
}

// InvalidatePendingForRunTx turns every pending approval of a Run into
// `invalidated` and returns them.
//
// This is §27.2.5's "Run 退出/取消/过期后 pending approvals 变 invalidated",
// and it belongs in the same transaction that moves the Run: the Run service
// (T1.04) calls it from inside the transition that takes the Run to cancelling,
// failed or expired. One transaction means a Run can never be terminal while a
// request against it is still pending, so nobody can decide an approval for a
// Run that was already given up on.
//
// The reason is free text ("run_cancelled", "run_failed", ...) and lands in the
// event payload. An already approved row is not touched: §27.2.5 keeps the
// history, and the inability to consume it (a terminal Run, checked by
// ConsumeTx) is what stops the execution instead.
func InvalidatePendingForRunTx(ctx context.Context, tx runstore.Tx, runID string, reason string, now time.Time) ([]Approval, error) {
	if tx == nil {
		return nil, fmt.Errorf("%w: nil transaction", ErrInvalidApproval)
	}
	key := trimSpace(runID)
	if key == "" {
		return nil, fmt.Errorf("%w: run id is required", ErrInvalidApproval)
	}
	if now.IsZero() {
		return nil, fmt.Errorf("%w: Now is required", ErrInvalidApproval)
	}
	reason = trimSpace(reason)
	if reason == "" {
		reason = "run_invalidated"
	}

	rows, err := ListApprovalsByRun(ctx, tx, key)
	if err != nil {
		return nil, err
	}
	out := make([]Approval, 0, len(rows))
	for _, a := range rows {
		if a.Status != StatusPending {
			continue
		}
		stored, _, err := writeDecisionTx(ctx, tx, decisionWrite{
			Approval:  a,
			Status:    StatusInvalidated,
			DecidedBy: "system:run",
			At:        now.UTC(),
			Actor:     systemActor,
			Reason:    reason,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, stored)
	}
	return out, nil
}

// ResolveDeniedTx returns a Run to `running` after a refusal was delivered to
// the backend.
//
// The caller's order matters and is not optional: first the refusal must reach
// the backend — execbackend's Session.Approve with Approved=false, so the
// waiting tool call is answered and will not run — and only then may the Run be
// moved. Doing it the other way round would tell the Run it may continue while
// the backend is still blocked on an answer it will never get.
//
// It accepts an approval in any decided-but-unconsumed state (rejected,
// expired, invalidated): the reason in the payload says which, and the Run's
// move is the same. It also accepts an *approved* approval that was never
// consumed (CA-4, reason "stale"): the grant could not be spent on the waiting
// call — the arguments, the target content, the policy or the deadline moved
// since the decision, so ConsumeTx refused it and §27.2.3 makes the old decision
// unconsumable history — and the caller has told the backend no. Without this
// the Run would wait forever for an approval nobody can use, and could not ask
// for a new one (a waiting Run cannot request; S1). The row stays approved;
// ConsumeTx never spends a medium/high approval its Run is not waiting for, so
// the call cannot run afterwards on the old decision. When the Run is no longer waiting_approval — a cancellation
// got there first, or this was already called — there is nothing to move and
// the call reports ErrApprovalConflict naming the current status: idempotent,
// and no state is resurrected (§27.2.5).
//
// The denied approval must be the one the Run is waiting for
// (waitedToolApprovalID): an older denial that was already resolved, or a gate
// or merge approval that never put the Run into waiting_approval, cannot take
// it out again while it waits for a newer call. That is ErrApprovalConflict
// too, and nothing is written.
func ResolveDeniedTx(ctx context.Context, tx runstore.Tx, approvalID string, actor run.Actor, now time.Time) (Approval, run.Transition, error) {
	if tx == nil {
		return Approval{}, run.Transition{}, fmt.Errorf("%w: nil transaction", ErrInvalidApproval)
	}
	if now.IsZero() {
		return Approval{}, run.Transition{}, fmt.Errorf("%w: Now is required", ErrInvalidApproval)
	}
	if err := validateLifecycleActor(actor); err != nil {
		return Approval{}, run.Transition{}, err
	}

	current, err := GetApproval(ctx, tx, approvalID)
	if err != nil {
		return Approval{}, run.Transition{}, err
	}
	reason := denialReason(current.Status)
	if reason == "" {
		return current, run.Transition{}, fmt.Errorf(
			"%w: approval %s is %s, which is not a denial of a waiting call",
			ErrApprovalAlreadyDecided, current.ID, current.Status)
	}
	if current.RunID == "" {
		return current, run.Transition{}, fmt.Errorf("%w: approval %s is not bound to a run",
			ErrApprovalNoRun, current.ID)
	}

	runRow, err := runstore.GetRun(ctx, tx, current.RunID)
	if err != nil {
		return current, run.Transition{}, fmt.Errorf("approval: read run %s: %w", current.RunID, err)
	}
	if runRow.Status != run.RunStatusWaitingApproval {
		return current, run.Transition{}, fmt.Errorf(
			"%w: run %s is %s, not waiting_approval; nothing to move",
			ErrApprovalConflict, runRow.ID, runRow.Status)
	}
	waited, err := waitedToolApprovalID(ctx, tx, runRow.ID)
	if err != nil {
		return current, run.Transition{}, err
	}
	if waited != current.ID {
		return current, run.Transition{}, fmt.Errorf(
			"%w: run %s is waiting for approval %q, not %s; nothing to move",
			ErrApprovalConflict, runRow.ID, waited, current.ID)
	}
	if current.Status == StatusApproved {
		// CA-4: a grant the waiting call cannot use. It must still be unspent —
		// a spent grant already released the Run when it was consumed.
		consumed, err := approvalConsumed(ctx, tx, current.ID)
		if err != nil {
			return current, run.Transition{}, err
		}
		if consumed {
			return current, run.Transition{}, fmt.Errorf(
				"%w: approval %s was already consumed; its call ran", ErrApprovalConflict, current.ID)
		}
	}

	moved, err := moveRun(ctx, tx, moveRunInput{
		RunID:          runRow.ID,
		ExpectedStatus: run.RunStatusWaitingApproval,
		Trigger:        run.EventTrigger(string(run.EventApprovalDenied)),
		At:             now.UTC(),
		Actor:          actor,
		AttemptID:      optionalString(current.AttemptID),
		Details:        map[string]any{"approval_id": current.ID, "reason": reason},
		Destinations:   runDestinations(runRow.ProjectID, runRow.ID),
	})
	if err != nil {
		return current, run.Transition{}, err
	}
	return current, moved.Transition, nil
}

// denialReason maps a decided status to the approval.denied payload reason, or
// "" when the status is not a denial of a waiting call. An approved row maps to
// "stale" (CA-4): the decision stands as history, but the call it was made for
// cannot use it, so for the waiting call it is a denial.
func denialReason(status Status) string {
	switch status {
	case StatusRejected:
		return "rejected"
	case StatusExpired:
		return "expired"
	case StatusInvalidated:
		return "invalidated"
	case StatusApproved:
		return "stale"
	default:
		return ""
	}
}

// approvalConsumed reports whether the approval already has its one-shot
// consumption receipt.
func approvalConsumed(ctx context.Context, tx runstore.Tx, approvalID string) (bool, error) {
	var one int
	err := tx.QueryRowContext(ctx,
		`SELECT 1 FROM approval_consumptions WHERE approval_id = ? LIMIT 1`, approvalID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("approval: read the receipt of %s: %w", approvalID, err)
	}
	return true, nil
}

// moveRunInput is one Run move this package asks for.
type moveRunInput struct {
	RunID           string
	ExpectedStatus  run.RunStatus
	Trigger         run.Trigger
	At              time.Time
	Actor           run.Actor
	AttemptID       *string
	AgentRevisionID *string
	Details         map[string]any
	Destinations    []string
}

// moveRun performs one §21.1 Run transition inside the caller's transaction.
//
// The expected revision is read here, immediately before the CAS, inside the
// write transaction the caller already holds — so it is the committed revision,
// not something the caller read earlier. TransitionRunTx then compares revision
// and from-status and refuses a mismatch with *runstore.RevisionConflictError.
func moveRun(ctx context.Context, tx runstore.Tx, in moveRunInput) (runstore.TransitionResult, error) {
	current, err := runstore.GetRun(ctx, tx, in.RunID)
	if err != nil {
		return runstore.TransitionResult{}, fmt.Errorf("approval: read run %s: %w", in.RunID, err)
	}
	result, err := runstore.TransitionRunTx(ctx, tx, runstore.TransitionInput{
		RunID:            in.RunID,
		ExpectedRevision: current.Revision,
		ExpectedStatus:   in.ExpectedStatus,
		Trigger:          in.Trigger,
		At:               in.At.UTC(),
		Actor:            in.Actor,
		AttemptID:        in.AttemptID,
		AgentRevisionID:  in.AgentRevisionID,
		Details:          in.Details,
		Destinations:     in.Destinations,
	})
	if err != nil {
		return runstore.TransitionResult{}, err
	}
	return result, nil
}

// resolveToolSubject reads the run, its current attempt and the agent revision,
// refuses a request whose claims disagree with them, and returns the Run's
// status for the caller's risk-dependent check.
//
// The attempt must be the run's *current* attempt (the highest attempt_no): a
// tool call happens in the attempt that is running now, and an approval naming
// an earlier attempt of the same run would be consumable only by a process that
// is already gone. The Run must be running or waiting_approval: those are the
// two states in which its process is alive and asking.
func resolveToolSubject(ctx context.Context, tx runstore.Tx, in RequestToolInput) (ToolSubject, run.RunStatus, error) {
	runID := trimSpace(in.RunID)
	if runID == "" {
		return ToolSubject{}, "", fmt.Errorf("%w: RunID is required for a tool approval", ErrInvalidApproval)
	}
	attemptID := trimSpace(in.AttemptID)
	if attemptID == "" {
		return ToolSubject{}, "", fmt.Errorf("%w: AttemptID is required for a tool approval", ErrInvalidApproval)
	}

	runRow, err := runstore.GetRun(ctx, tx, runID)
	if err != nil {
		return ToolSubject{}, "", fmt.Errorf("approval: read run %s: %w", runID, err)
	}
	if claimed := trimSpace(in.ProjectID); claimed != "" && claimed != runRow.ProjectID {
		return ToolSubject{}, "", fmt.Errorf("%w: run %s belongs to project %s, the request claims %s",
			ErrApprovalIdentityMismatch, runID, runRow.ProjectID, claimed)
	}

	attempts, err := runstore.ListAttemptsByRun(ctx, tx, runID)
	if err != nil {
		return ToolSubject{}, "", fmt.Errorf("approval: list attempts of run %s: %w", runID, err)
	}
	if len(attempts) == 0 {
		return ToolSubject{}, "", fmt.Errorf("%w: run %s has no attempt", ErrApprovalIdentityMismatch, runID)
	}
	currentAttempt := attempts[len(attempts)-1]
	if currentAttempt.ID != attemptID {
		return ToolSubject{}, "", fmt.Errorf("%w: attempt %s is not run %s's current attempt (%s)",
			ErrApprovalIdentityMismatch, attemptID, runID, currentAttempt.ID)
	}
	if claimed := trimSpace(in.AgentRevisionID); claimed != "" && claimed != runRow.AgentRevisionID {
		// CreateApprovalTx would refuse this too; naming it here says which
		// claim was wrong.
		return ToolSubject{}, "", fmt.Errorf("%w: the request names agent revision %s, run %s executes %s",
			ErrAgentRevisionMismatch, claimed, runID, runRow.AgentRevisionID)
	}

	if runRow.Status != run.RunStatusRunning && runRow.Status != run.RunStatusWaitingApproval {
		return ToolSubject{}, "", fmt.Errorf("%w: run %s is %s", ErrApprovalNotRunning, runRow.ID, runRow.Status)
	}
	return ToolSubject{
		ProjectID:       runRow.ProjectID,
		RunID:           runRow.ID,
		AttemptID:       currentAttempt.ID,
		AgentRevisionID: runRow.AgentRevisionID,
	}, runRow.Status, nil
}

// waitedToolApprovalID returns the id of the approval a waiting Run waits for:
// the most recently created tool approval of the Run whose risk is not low, or
// "" when there is none.
//
// Why that identifies it: only a medium/high tool request puts a Run into
// waiting_approval, and it does so in the same transaction that creates the
// approval; such a request is refused unless the Run is running, and the Run
// leaves waiting_approval only when that approval is consumed (approval.approved)
// or its denial is resolved (approval.denied), or when the Run ends. So while a
// Run waits, the newest non-low tool approval is the one it waits for, and every
// older one is already spent. A low-risk approval never made anybody wait.
//
// Creation order is SQLite's rowid: approvals cannot be deleted (migration 009),
// so the rowid only grows, and unlike requested_at it is not a caller's value.
func waitedToolApprovalID(ctx context.Context, tx runstore.Tx, runID string) (string, error) {
	var id string
	err := tx.QueryRowContext(ctx, `
		SELECT id FROM approvals
		WHERE run_id = ? AND subject_type = 'tool' AND risk <> 'low'
		ORDER BY rowid DESC LIMIT 1`, runID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("approval: read the approval run %s waits for: %w", runID, err)
	}
	return id, nil
}

// sameArgv reports whether two argv lists are the same command, element by
// element. nil and empty are the same: both mean "no command".
func sameArgv(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// listPendingApprovalsTx reads one project's pending approvals inside the
// caller's transaction, in the same (requested_at, id) order the public reader
// uses, so a sweep and a review screen enumerate the same rows the same way.
func listPendingApprovalsTx(ctx context.Context, tx runstore.Tx, projectID string) ([]Approval, error) {
	rows, err := tx.QueryContext(ctx, selectApprovalColumns+
		` WHERE project_id = ? AND status = 'pending' ORDER BY requested_at ASC, id ASC`, projectID)
	if err != nil {
		return nil, fmt.Errorf("approval: list pending approvals of project %s: %w", projectID, err)
	}
	defer rows.Close()

	var out []Approval
	for rows.Next() {
		a, err := scanApproval(rows)
		if err != nil {
			return nil, fmt.Errorf("approval: scan pending approvals of project %s: %w", projectID, err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("approval: iterate pending approvals of project %s: %w", projectID, err)
	}
	return out, nil
}

// isSameDecision reports whether a decided row already carries this caller's
// answer, which makes a repeated DecideTx idempotent rather than a conflict.
func isSameDecision(current Approval, in DecisionInput) bool {
	want := StatusRejected
	if in.Approved {
		want = StatusApproved
	}
	if current.Status != want {
		return false
	}
	decidedBy := trimSpace(in.DecidedBy)
	if decidedBy == "" {
		decidedBy = trimSpace(in.Actor.ID)
	}
	return current.DecidedBy == decidedBy
}

// RunDestinations is the destinations a Run state event is delivered to: the
// project's live stream, the run's, and the audit export. The names are the
// ones the rest of the runtime library already uses (websocket/live.go serves
// the two "ws:" scopes; runstore's dispatcher tests use "audit:export"), so
// nothing new is invented here and T1.04 registers them exactly as they are.
//
// It is exported because the Run service queues the same set for its own state
// events, and two spellings of "the run's subscribers" would deliver one fact
// twice.
func RunDestinations(projectID, runID string) []string { return runDestinations(projectID, runID) }

// runDestinations is the internal spelling, so the exported alias cannot drift:
// a Run-less approval (a Gate) has no run destination and never an empty name.
func runDestinations(projectID, runID string) []string {
	out := make([]string, 0, 3)
	if p := trimSpace(projectID); p != "" {
		out = append(out, "ws:project:"+p)
	}
	if r := trimSpace(runID); r != "" {
		out = append(out, "ws:run:"+r)
	}
	return append(out, AuditDestination)
}

// AuditDestination is the outbox destination the audit export is delivered to:
// the name §19.3 item 2 means by "audit outbox", the row the idempotent
// dispatcher exports into the audit chain.
const AuditDestination = "audit:export"

// identityJSON encodes the ExecutionIdentity envelope of an event this package
// writes. The project, run and attempt come from the stored rows (never from a
// caller's body), and the actor is the one the caller is accountable as.
func identityJSON(projectID, runID, attemptID string, actor run.Actor) []byte {
	identity := run.ExecutionIdentity{
		ProjectID: trimSpace(projectID),
		RunID:     optionalString(runID),
		AttemptID: optionalString(attemptID),
		Actor:     actor,
	}
	encoded, err := marshalJSONObject(identity)
	if err != nil {
		// Unreachable: every field is a string or a *string, and the actor was
		// validated by the caller. A hand-built object keeps AppendEventTx's
		// strict decode in charge rather than panicking here.
		return []byte(`{}`)
	}
	return encoded
}

// marshalJSONObject encodes v as one JSON object. It is the single place this
// file turns a Go value into event bytes, so "the payload is an object" is
// proved once instead of assumed at each call site.
func marshalJSONObject(v any) ([]byte, error) {
	encoded, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("%w: event body cannot be encoded: %v", ErrInvalidApproval, err)
	}
	if !strings.HasPrefix(strings.TrimSpace(string(encoded)), "{") {
		return nil, fmt.Errorf("%w: event body did not encode to a JSON object", ErrInvalidApproval)
	}
	return encoded, nil
}

// validateLifecycleActor checks the actor half of a decision before any SQL
// runs. The rules are runstore's (the same TransitionRunTx applies): one of the
// four fixed types, a present id, and a source within the wire limit.
func validateLifecycleActor(actor run.Actor) error {
	if !actor.Type.Valid() {
		return fmt.Errorf("%w: Actor.Type %q is not one of user/agent/system/integration",
			ErrInvalidApproval, actor.Type)
	}
	if strings.TrimSpace(actor.ID) == "" {
		return fmt.Errorf("%w: Actor.ID is empty", ErrInvalidApproval)
	}
	if len(actor.Source) > run.MaxActorSourceLength {
		return fmt.Errorf("%w: Actor.Source is %d bytes, limit is %d",
			ErrInvalidApproval, len(actor.Source), run.MaxActorSourceLength)
	}
	return nil
}

// optionalString maps "" to nil, the same way the store's nullableString maps
// it for SQL.
func optionalString(s string) *string {
	trimmed := trimSpace(s)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// ptr returns a pointer to s, for the optional transition ids.
func ptr(s string) *string { return &s }

// derefEvent returns the event a transition wrote, or the zero value when the
// transition writes none.
func derefEvent(event *runstore.Event) runstore.Event {
	if event == nil {
		return runstore.Event{}
	}
	return *event
}
