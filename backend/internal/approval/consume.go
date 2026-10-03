// Consumption: the one place an approval turns into a granted execution
// (T2.02.b, plan §28 T2.02.b, §27.2.3, §15 T2.02).
//
// An approval is a decision about an action; ConsumeTx is where the decision is
// spent. It re-checks, in the caller's transaction and in this order, every
// condition the decision was made under — the policy still allows the
// operation, under the same rule version the approval was requested with; the
// action's canonical fingerprint still hashes to the stored one; the scope is
// unchanged; the Run is alive and the attempt is the current one — and only
// then writes the one-shot receipt. §27.2.3's "批准后执行时重验" is this
// function; without it the stored fingerprint would be a claim about the past,
// not a condition on the present.
//
// One approval grants at most one execution. The receipt is the mutex: two
// consumers in two transactions cannot both insert it, so exactly one is
// Granted — and only the transaction that inserted it proceeds to run the
// tool. The second gets ErrAlreadyConsumed, the class of every way it can lose.
//
// What this function deliberately does NOT do: delete or rewrite a receipt to
// retry an execution whose outcome is unknown. A receipt says "this
// authorization was spent on this call"; if the process may have run, the
// question is not "may I spend it again" (§27.2.5 forbids it, and migration 009
// refuses the DELETE) but "what did the existing operation do", which the
// existing operation reconciliation answers. A second execution requires a new
// approval, requested with the new request's own fingerprint.
package approval

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/codeflow/backend/internal/policy"
	"github.com/codeflow/backend/internal/run"
	"github.com/codeflow/backend/internal/runstore"
)

// Consumption refusals. Each is distinguishable because the caller reacts
// differently: a policy change may fix itself after a re-review, a fingerprint
// change always needs a new approval, a terminal Run needs nothing at all, and
// a lost receipt race is simply "someone else runs it". Every one of them wraps
// ErrApprovalNoReceipt, the class of "this consumer was not granted".
var (
	// ErrApprovalPolicyChanged means the evaluator's rule version differs from
	// the approval's policy_version: the rules the human decided under are no
	// longer the rules in force, so the decision is unconsumable history
	// (§27.2.3 "策略改变则原决定成为不可消费历史，新建 approval").
	ErrApprovalPolicyChanged = errors.New("approval: policy version changed since the approval")

	// ErrApprovalBindingChanged means the real request's canonical fingerprint —
	// or its canonical scope — differs from the stored one: a parameter, target
	// path, base manifest, agent revision or scope changed since the decision.
	// The old decision is history and a new approval is required (§27.2.3); it
	// never authorizes the changed action.
	ErrApprovalBindingChanged = errors.New("approval: request no longer matches the approved fingerprint")

	// ErrApprovalRunNotConsumable means the Run cannot have a tool call granted
	// right now: it is terminal (the authorization died with it), or it is in a
	// state that is not running or waiting for this approval.
	ErrApprovalRunNotConsumable = errors.New("approval: run is not in a state that can consume this approval")

	// ErrApprovalAttemptMismatch means the attempt presenting the approval is
	// not the Run's current attempt, or is already finished: an authorization
	// belongs to the process that asked for it, not to a later one.
	ErrApprovalAttemptMismatch = errors.New("approval: attempt is not the run's current live attempt")
)

// ConsumeInput is one attempt to spend an approval on a tool call.
type ConsumeInput struct {
	// ApprovalID is the approval to spend. Required.
	ApprovalID string
	// ToolCallID is the tool call being authorized: it must equal the
	// approval's subject (a tool approval authorizes exactly one call).
	ToolCallID string
	// AttemptID is the attempt that will run the call; it must be the
	// approval's own attempt and the Run's current attempt.
	AttemptID string
	// FingerprintInput is the binding of the request as it actually is now —
	// built by the executing path from the same fields the request was
	// approved with (execbackend.ToolRequestFingerprint for ArgumentsHash).
	// Its PolicyVersion is replaced by the evaluated RuleVersion before the
	// re-hash, so a caller does not have to know which version to write; every
	// other field must match the stored fingerprint exactly.
	FingerprintInput FingerprintInput
	// Scope is the request's scope as JSON, exactly as the request passes it.
	// It is canonicalized and compared with the stored scope; nil means "{}"
	// (the store's spelling of no scope), so a caller with a scope must pass
	// it.
	Scope []byte
	// Evaluator evaluates the operation at consumption time. A nil Evaluator
	// refuses every consumption (fail closed): this function cannot check
	// "policy still allows it" without a policy.
	Evaluator policy.Evaluator
	// PolicyRequest is the policy request for this execution. Its identity
	// fields (ProjectID, RunID, AttemptID, Risk, ApprovalID, Fingerprint) are
	// overwritten from the stored approval before evaluation, so a caller
	// cannot evaluate one operation and consume another; Operation, Resource,
	// ActorID and Context come from the caller.
	PolicyRequest policy.Request
	// Now is the consumption instant: the fake clock of tests, and the
	// deadline comparison the store applies to the receipt. Required.
	Now time.Time
}

// ConsumeResult is the outcome of a consumption attempt.
type ConsumeResult struct {
	// Granted is true exactly for the one transaction that wrote the receipt.
	// When it is false nothing was written; Approval names the row as it
	// currently stands.
	Granted bool
	// Approval is the approval as read (and, when granted, the state it was
	// spent in).
	Approval Approval
	// Consumption is the receipt, set only when Granted.
	Consumption Consumption
	// Transition is the Run transition approval.approved wrote, when the Run
	// was waiting for this approval. Zero when the Run was running, when it was
	// waiting for another approval (a low-risk call granted in parallel), or
	// when the call was not granted.
	Transition run.Transition
	// RunMoved reports whether Transition was written.
	RunMoved bool
}

// ConsumeTx spends an approval on one tool call, inside the caller's
// transaction, and returns Granted only to the transaction that wrote the
// one-shot receipt.
//
// Order, with every refusal writing nothing:
//
//  1. Read the approval; it must be an approved tool approval (a pending,
//     rejected, expired or invalidated row authorizes nothing — the store's
//     RecordConsumptionTx says the same, and re-checks it at the receipt).
//  2. Evaluate the operation with the caller's evaluator. A nil evaluator or a
//     disallowed decision is ErrApprovalPolicyDenied. The decision's
//     RuleVersion must equal the approval's policy_version, or
//     ErrApprovalPolicyChanged: the human decided under those rules.
//  3. Re-hash the real request's FingerprintInput (PolicyVersion := the
//     evaluated RuleVersion) and compare with the stored fingerprint, and
//     canonicalize the request's scope against the stored scope_json. A
//     difference is ErrApprovalBindingChanged: parameters, targets, base,
//     agent revision or scope moved, so the old decision is history.
//  4. The Run must be alive: running or waiting_approval. A terminal Run is
//     ErrApprovalRunNotConsumable: the authorization died with it. A low-risk
//     (auto-approved) call is granted in either state without touching the Run
//     — it never made the Run wait, and a backend may run it in parallel with
//     the call that did. A medium/high approval is granted only while the Run
//     waits for exactly it (waitedToolApprovalID), with no other pending tool
//     approval beside it; anything else — including a running Run, whose wait
//     for this approval already ended in a denial (CA-4) — is
//     ErrApprovalRunNotConsumable.
//  5. The attempt must be the Run's current attempt and not finished
//     (ErrApprovalAttemptMismatch).
//  6. RecordConsumptionTx writes the receipt — the one-shot mutex.
//  7. If this approval is the one the waiting Run waits for, the same
//     transaction writes waiting_approval --approval.approved--> running
//     (§21.1). Otherwise the Run stays as it is: a running Run has nothing to
//     release, and a waiting Run still waits for its own approval.
//
// Because the receipt is written inside the caller's transaction, "granted"
// and "the Run moved" and "the receipt exists" are one commit: a crash between
// them is impossible, and a rollback returns the approval to unconsumed.
func ConsumeTx(ctx context.Context, tx runstore.Tx, in ConsumeInput) (ConsumeResult, error) {
	if tx == nil {
		return ConsumeResult{}, fmt.Errorf("%w: nil transaction", ErrInvalidApproval)
	}
	if in.Now.IsZero() {
		return ConsumeResult{}, fmt.Errorf("%w: Now is required", ErrInvalidApproval)
	}
	approvalID := trimSpace(in.ApprovalID)
	if approvalID == "" {
		return ConsumeResult{}, fmt.Errorf("%w: ApprovalID is required", ErrInvalidApproval)
	}
	toolCallID := trimSpace(in.ToolCallID)
	if toolCallID == "" {
		return ConsumeResult{}, fmt.Errorf("%w: ToolCallID is required", ErrInvalidApproval)
	}

	stored, err := GetApproval(ctx, tx, approvalID)
	if err != nil {
		return ConsumeResult{}, err
	}
	refuse := func(cause error) (ConsumeResult, error) {
		return ConsumeResult{Approval: stored}, fmt.Errorf("%w: %w", ErrApprovalNoReceipt, cause)
	}

	if stored.SubjectType != SubjectTool {
		return refuse(fmt.Errorf("%w: approval %s authorizes a %s subject; only a tool approval is consumed here",
			ErrApprovalNotConsumable, stored.ID, stored.SubjectType))
	}
	if stored.Status != StatusApproved {
		return refuse(fmt.Errorf("%w: approval %s is %s, not approved",
			ErrApprovalNotConsumable, stored.ID, stored.Status))
	}
	if stored.SubjectID != toolCallID {
		return refuse(fmt.Errorf("%w: approval %s authorizes tool call %s, not %s",
			ErrApprovalNotConsumable, stored.ID, stored.SubjectID, toolCallID))
	}

	// Step 2: policy, evaluated now. A nil evaluator cannot say yes, so it is a
	// refusal rather than a skip; evaluation errors are the evaluator's job and
	// a Decision without a rule version cannot be compared, so it is refused
	// too.
	if in.Evaluator == nil {
		return refuse(fmt.Errorf("%w: no evaluator was supplied at consumption time", ErrApprovalPolicyDenied))
	}
	decision := in.Evaluator.Evaluate(ctx, PolicyRequestFor(in.PolicyRequest, stored))
	if !decision.Allowed {
		return refuse(fmt.Errorf("%w: %s", ErrApprovalPolicyDenied, strings.TrimSpace(decision.Reason)))
	}
	if trimSpace(decision.RuleVersion) == "" {
		return refuse(fmt.Errorf("%w: the evaluator returned no rule version", ErrApprovalPolicyDenied))
	}
	if decision.RuleVersion != stored.PolicyVersion {
		return refuse(fmt.Errorf("%w: approval %s was requested under policy %s, the evaluator runs %s",
			ErrApprovalPolicyChanged, stored.ID, stored.PolicyVersion, decision.RuleVersion))
	}

	// Step 3: the binding, re-hashed. The stored fingerprint is recomputed from
	// the request as it is now, with the approved policy version; anything the
	// request got wrong (a missing field, a malformed argument hash) is refused
	// by Fingerprint itself rather than compared.
	recomputed := in.FingerprintInput
	recomputed.PolicyVersion = decision.RuleVersion
	actualFingerprint, err := Fingerprint(recomputed)
	if err != nil {
		return refuse(fmt.Errorf("%w: the real request cannot be fingerprinted: %w", ErrApprovalBindingChanged, err))
	}
	if actualFingerprint != stored.Fingerprint {
		return refuse(fmt.Errorf("%w: approval %s binds %s, the request fingerprints as %s",
			ErrApprovalBindingChanged, stored.ID, stored.Fingerprint, actualFingerprint))
	}
	actualScope, err := CanonicalScopeJSON(in.Scope)
	if err != nil {
		return refuse(fmt.Errorf("%w: the request's scope cannot be canonicalized: %w", ErrApprovalBindingChanged, err))
	}
	if actualScope != stored.ScopeJSON {
		return refuse(fmt.Errorf("%w: approval %s binds scope %s, the request carries %s",
			ErrApprovalBindingChanged, stored.ID, stored.ScopeJSON, actualScope))
	}

	// Step 4: the Run. The approval identified it at request time; read it
	// again because a cancel, a failure or an expiry may have happened since
	// (§27.2.5: a Run's exit invalidates what was pending, and what was already
	// approved simply cannot be spent any more).
	runRow, err := runstore.GetRun(ctx, tx, stored.RunID)
	if err != nil {
		return ConsumeResult{}, fmt.Errorf("approval: read run %s: %w", stored.RunID, err)
	}
	releaseRun := false
	switch runRow.Status {
	case run.RunStatusRunning:
		// Nothing waits, nothing to move — for a low-risk call. A medium/high
		// approval is spent only while its Run waits for it: once the Run is
		// running again its wait ended without this approval being consumed,
		// which happens only when the call was denied to the backend (CA-4,
		// ResolveDeniedTx "stale"). Spending it now would run a call the backend
		// was told would not run.
		if stored.Risk != RiskLow {
			return refuse(fmt.Errorf("%w: run %s is running; a %s-risk approval is consumed only while its run waits for it",
				ErrApprovalRunNotConsumable, stored.RunID, stored.Risk))
		}
	case run.RunStatusWaitingApproval:
		if stored.Risk == RiskLow {
			// An auto-approved call never made the Run wait: it is granted, and
			// the Run keeps waiting for the approval that did.
			break
		}
		// A medium/high approval must be the one the Run waits for. Releasing
		// the Run on any other would tell the backend to continue while it is
		// still blocked on a call nobody answered.
		waited, err := waitedToolApprovalID(ctx, tx, stored.RunID)
		if err != nil {
			return ConsumeResult{}, err
		}
		if waited != stored.ID {
			return refuse(fmt.Errorf("%w: run %s is waiting for approval %q, not %s",
				ErrApprovalRunNotConsumable, stored.RunID, waited, stored.ID))
		}
		// S1 allows one pending tool approval per Run and this one is decided,
		// so a pending one beside it means the state was written around the
		// request path; refuse rather than guess which call the Run waits for.
		siblings, err := ListApprovalsByRun(ctx, tx, stored.RunID)
		if err != nil {
			return ConsumeResult{}, err
		}
		for _, a := range siblings {
			if a.ID != stored.ID && a.SubjectType == SubjectTool && a.Status == StatusPending {
				return refuse(fmt.Errorf(
					"%w: run %s is waiting for approval %s, not %s",
					ErrApprovalRunNotConsumable, stored.RunID, a.ID, stored.ID))
			}
		}
		releaseRun = true
	default:
		return refuse(fmt.Errorf("%w: run %s is %s (want running or waiting_approval)",
			ErrApprovalRunNotConsumable, runRow.ID, runRow.Status))
	}

	// Step 5: the attempt. The current attempt is the one with the highest
	// attempt_no; it must not have finished. An approval is bound to the
	// process that asked, and a later attempt needs its own request.
	attempts, err := runstore.ListAttemptsByRun(ctx, tx, stored.RunID)
	if err != nil {
		return ConsumeResult{}, fmt.Errorf("approval: list attempts of run %s: %w", stored.RunID, err)
	}
	if len(attempts) == 0 {
		return refuse(fmt.Errorf("%w: run %s has no attempt", ErrApprovalAttemptMismatch, stored.RunID))
	}
	current := attempts[len(attempts)-1]
	if current.ID != trimSpace(in.AttemptID) || current.ID != stored.AttemptID {
		return refuse(fmt.Errorf("%w: attempt %s presented, run %s's current attempt is %s and approval %s belongs to %s",
			ErrApprovalAttemptMismatch, in.AttemptID, stored.RunID, current.ID, stored.ID, stored.AttemptID))
	}
	if current.Status.IsTerminal() {
		return refuse(fmt.Errorf("%w: attempt %s is %s", ErrApprovalAttemptMismatch, current.ID, current.Status))
	}

	// Step 6: the receipt. This insert is the mutex; the loser is reported as
	// ErrAlreadyConsumed by the store. Everything above may run concurrently
	// with identical results for both racers — only one of them wins here.
	consumption, err := RecordConsumptionTx(ctx, tx, ConsumptionInput{
		ApprovalID: stored.ID,
		ToolCallID: toolCallID,
		AttemptID:  stored.AttemptID,
		ConsumedAt: in.Now,
	})
	if err != nil {
		return ConsumeResult{Approval: stored}, fmt.Errorf("%w: %w", ErrApprovalNoReceipt, err)
	}

	result := ConsumeResult{
		Granted:     true,
		Approval:    stored,
		Consumption: consumption,
	}

	// Step 7: the Run, when it was waiting for this call. The transition writes
	// approval.approved and its outbox rows in this same transaction, so a
	// granted call and the Run's resumption cannot diverge; if the CAS loses (a
	// cancel got there first), the error rolls the receipt back with it and
	// nothing was granted — which is the right way round, because the Run the
	// call would run in no longer exists.
	if releaseRun {
		moved, err := moveRun(ctx, tx, moveRunInput{
			RunID:           runRow.ID,
			ExpectedStatus:  run.RunStatusWaitingApproval,
			Trigger:         run.EventTrigger(string(run.EventApprovalApproved)),
			At:              in.Now.UTC(),
			Actor:           systemActor,
			AttemptID:       optionalString(stored.AttemptID),
			AgentRevisionID: optionalString(runRow.AgentRevisionID),
			Details:         map[string]any{"approval_id": stored.ID, "tool_call_id": stored.SubjectID},
			Destinations:    runDestinations(runRow.ProjectID, runRow.ID),
		})
		if err != nil {
			return ConsumeResult{}, err
		}
		result.Transition = moved.Transition
		result.RunMoved = true
	}
	return result, nil
}

// PolicyRequestFor fills a policy request's identity fields from the stored
// approval, so the operation the evaluator judges is the one the approval
// authorizes. The rule (which fields are authoritative, which are the caller's)
// is written once here because consumption, T2.02.c's end-to-end path and
// T3.04's batch screen must all ask the same question.
//
// Operation defaults to process_start when the caller named none: a tool
// approval authorizes starting a project process, which is the operation policy
// evaluates under normalizeRequest's "process" resource.
func PolicyRequestFor(request policy.Request, stored Approval) policy.Request {
	request.ProjectID = stored.ProjectID
	request.RunID = stored.RunID
	request.AttemptID = stored.AttemptID
	request.Risk = string(stored.Risk)
	request.ApprovalID = stored.ID
	request.Fingerprint = stored.Fingerprint
	if trimSpace(request.Operation) == "" {
		request.Operation = policy.OperationProcessStart
	}
	if request.Resource == "" {
		request.Resource = stored.SubjectID
	}
	return request
}
