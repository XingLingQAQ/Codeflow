// Run state transitions with an expected-revision and from-status CAS
// (T1.02.b).
//
// This file is the other half of internal/run/transitions.go. The table there
// decides *whether* a (from status, trigger) pair is allowed and which state
// event the change writes; this file performs that decision as one transaction
// over the run row, so that the status change and the event it produces commit
// or roll back together (§19.3 item 1 "Run 状态迁移 + 状态事件 + outbox 行",
// CA-1 §21.1 "每个迁移在同一事务里写一条状态事件").
//
// The order is fixed and matters:
//
//  1. Input validation, before any SQL runs (validateTransitionInput). A
//     rejected input leaves the caller's transaction exactly as it was — not
//     even a scope counter is touched.
//  2. Re-read the run inside the caller's transaction (GetRun). A missing row is
//     ErrNotFound.
//  3. Compare *both* the expected revision and the expected from-status with the
//     stored row. A mismatch is a *RevisionConflictError carrying the stored
//     revision and status; this function then returns without writing anything.
//  4. run.Decide(stored status, trigger): an illegal pair is returned wrapped in
//     both ErrInvalidTransition and the frozen *run.TransitionError.
//  5. UpdateRunStatusCAS with the caller's expected revision.
//  6. If the transition names a state event, AppendEventTx — the event, its
//     sequence numbers and its outbox rows — in the same transaction. A state
//     event that fails (a required identity field missing, a destination listed
//     twice, a payload over the limit) fails the whole transition: the status
//     change and the row the caller composed around it roll back.
//
// Step 3 is the reason a lost race cannot resurrect a terminal Run (§27.2.5,
// §27.2.6). The caller decided which trigger to send from the status it read
// earlier; that decision is only valid for that exact (status, revision) pair.
// Re-deciding the same trigger from the *current* status would be a second,
// different decision taken on someone else's behalf — the loser of a
// concurrent cancel/finish race would then report an outcome the winner already
// decided. The loser gets the conflict error and must re-read the row and decide
// again itself (§27.2.6 "输方读取当前状态，不反向复活终态").
//
// What this file deliberately does not decide (the caller's half of §21.1's
// fourth column): whether the actor is authorised to cancel, whether a lease is
// valid, whether an approval fingerprint was persisted, whether a checkpoint is
// supported, whether output was persisted, whether the grace/force records are
// complete, and whether the owner instance is verified. A caller whose
// precondition failed reports run.FailureCodeFor(trigger) and never reaches this
// function. It also does not look the attempt up: the attempt identity travels
// as given, exactly as AppendEventTx treats it (a legacy attempt id may live in
// the old database until T12.01).

package runstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/codeflow/backend/internal/run"
)

// Transition error sentinels. They live here rather than in errors.go because
// the transition contract is this card's (T1.02.b) and errors.go is shared with
// the other writers of this package.
var (
	// ErrInvalidTransitionInput means the caller handed TransitionRunTx or
	// RetryRunTx an input the contract rejects: a blank id, an expected revision
	// below 1, an invalid expected status, a zero instant, an invalid actor, a
	// Details map carrying a reserved payload key or holding a value JSON cannot
	// encode. Like ErrInvalidRecord these are programmer errors, and they are
	// reported before any SQL runs, so a rejected call cannot leave a partial
	// write behind.
	ErrInvalidTransitionInput = errors.New("runstore: invalid transition input")

	// ErrInvalidTransition means run.Decide refused the (from status, trigger)
	// pair for the status the run was actually in. The original
	// *run.TransitionError is wrapped (errors.As), so the caller can report the
	// offending status/trigger and the §21.1 failure code. A terminal Run stays
	// terminal: no row of the §21.1 table leaves completed, failed, cancelled or
	// expired, so this is the error every "advance a finished run" attempt gets.
	ErrInvalidTransition = errors.New("runstore: transition not allowed")

	// ErrRunNotRetryable means the run named by RetryInput is not one an explicit
	// retry may start from: it is still running (or otherwise not terminal), or
	// it completed. §27.2.7 retries a *failed* execution; a completed Run's
	// output is a result to review or merge, not a failed attempt, and the four
	// terminal states are irreversible (§21.1).
	ErrRunNotRetryable = errors.New("runstore: run is not retryable")

	// ErrTaskNotRetryable means the task of the run being retried is not failed.
	// §27.2.7 "Task.completed/cancelled 不直接重开": a retry turns `failed` back
	// into `queued` and nothing else. A task that is ready/queued/running/
	// waiting_review has an execution path of its own, and reopening a
	// completed/cancelled task would silently revive a finished unit of work.
	ErrTaskNotRetryable = errors.New("runstore: task is not retryable")

	// ErrActiveRunExists means a retry was refused because the task already has
	// a non-terminal Run. §19.1 "同一 Task 最多一个 active Run": a second
	// queued/running Run for the same task would give the task two current
	// executions, and the database's partial unique index
	// (idx_runs_one_active_per_task) refuses the row behind this error.
	ErrActiveRunExists = errors.New("runstore: task already has an active run")
)

// The payload keys TransitionRunTx writes itself. §21.1 freezes what a state
// event must carry — from_status, to_status and the trigger always, the failure
// code the table gives the row, and the expected terminal a cancelling Run
// recorded (CA-1 "payload 记 expected_terminal 与 reason") — and a caller's
// Details map must not be able to overwrite any of them. The keys are checked
// case-insensitively: a payload carrying both `from_status` and `FROM_STATUS`
// would be ambiguous for any lenient consumer, and no legitimate caller needs
// the second spelling.
const (
	payloadKeyFromStatus       = "from_status"
	payloadKeyToStatus         = "to_status"
	payloadKeyTrigger          = "trigger"
	payloadKeyFailureCode      = "failure_code"
	payloadKeyExpectedTerminal = "expected_terminal"
)

// ReservedStateEventPayloadKeys lists, in payload order, the keys
// TransitionRunTx writes into a state event payload. They are exported so the
// API layer (T1.04) and the docs can name the same set; a caller passing any of
// them in Details — in any letter case — gets ErrInvalidTransitionInput instead
// of a silently overwritten value.
var ReservedStateEventPayloadKeys = []string{
	payloadKeyFromStatus,
	payloadKeyToStatus,
	payloadKeyTrigger,
	payloadKeyFailureCode,
	payloadKeyExpectedTerminal,
}

// TransitionInput is one request to move a Run, with the CAS pair the caller's
// decision was based on.
type TransitionInput struct {
	// RunID is the Run to move. Required.
	RunID string
	// ExpectedRevision is the revision the caller read. Required, >= 1. A stored
	// revision that differs is a *RevisionConflictError, not a retry.
	ExpectedRevision int64
	// ExpectedStatus is the status the caller read, and the status its trigger
	// decision is valid for. Required and must be a valid RunStatus; a stored
	// status that differs is a *RevisionConflictError even when the revision
	// matches.
	ExpectedStatus run.RunStatus
	// Trigger is the §21.1 event, command or conclusion. Its Kind must be set:
	// run.Decide matches the three vocabularies strictly, so a command can never
	// be mistaken for an event.
	Trigger run.Trigger
	// At is when the transition happened. Required (non-zero); it becomes both
	// the row's updated_at and the state event's occurred_at. It is the caller's
	// fact, not the write time.
	At time.Time
	// Actor is the actor object of the state event's ExecutionIdentity. Required:
	// the actor type must be one of user/agent/system/integration and the id
	// non-blank. §27.1 fixes the intended mapping: a user cancel is `user`, the
	// recoverer is `system`, a backend process event is `agent` or `system`.
	// Whether that actor is *authorised* to cancel is the caller's policy check,
	// not this function's.
	Actor run.Actor
	// AttemptID names the live attempt for the state events whose identity
	// requirement demands one (process.started, process.exited,
	// process.terminated, checkpoint.acknowledged, run.reattached, run.resumed).
	// Leave it nil for an event that does not need one — including every cancel
	// of a queued Run, which never had an attempt (§28 "queued 可无 attempt"). A
	// nil attempt on an event that requires one is refused by AppendEventTx
	// (ErrInvalidEvent wrapping *run.IdentityError) and rolls the transition
	// back.
	AttemptID *string
	// AgentRevisionID is the immutable agent revision the executing attempt
	// belongs to; required by the same event types as AttemptID. It is passed
	// through as given (AppendEventTx does not look the attempt up), so the
	// caller names the revision it verified.
	AgentRevisionID *string
	// Details is the caller's own payload: the reason for a cancel, a pid for
	// process.started, an exit code, and so on. It is merged into the state
	// event payload as a flat object; the reserved keys above are refused rather
	// than overwritten.
	Details map[string]any
	// Destinations names the outbox deliveries to queue for the state event, in
	// the same transaction. Empty is legal; a blank or repeated name is refused
	// by AppendEventTx before any SQL runs.
	Destinations []string
}

// TransitionResult is the outcome of an accepted transition.
type TransitionResult struct {
	// Run is the stored row after the CAS: the new status, the incremented
	// revision and the finished_at stamp when the new status is terminal.
	Run run.Run
	// Transition is the frozen decision: From, To and StateEvent.
	Transition run.Transition
	// Event is the state event written in the same transaction, or nil when the
	// transition has no state event. That is exactly one case today: the merge
	// command of a completed Run, whose MergeOperation writes merge.completed
	// itself when it reaches applied (§19.3 item 4, §21.1).
	Event *Event
}

// TransitionRunTx moves one Run according to §21.1, in the caller's
// transaction, and writes the transition's state event there too.
//
// It never opens a transaction of its own — the composition of the status
// change, the event and the outbox row into one atomic unit is the point of the
// signature (§19.3 item 1) — and it never broadcasts anything: a caller that
// wants to notify someone passes Destinations and the outbox dispatcher does the
// delivery after the commit (§19.3 last paragraph).
//
// On success the returned TransitionResult carries the stored row and the event.
// On any error nothing was written: every failure path either precedes the
// writes or returns to the caller's WithTx, which rolls back.
func TransitionRunTx(ctx context.Context, tx Tx, in TransitionInput) (TransitionResult, error) {
	if tx == nil {
		return TransitionResult{}, fmt.Errorf("%w: nil Tx", ErrInvalidTransitionInput)
	}
	if err := validateTransitionInput(in); err != nil {
		return TransitionResult{}, err
	}

	// The read happens inside the caller's transaction, after BEGIN IMMEDIATE
	// took the write lock, so it observes every transition committed before this
	// transaction started and never a stale snapshot (§26.17).
	current, err := GetRun(ctx, tx, in.RunID)
	if err != nil {
		return TransitionResult{}, fmt.Errorf("transition run %s: %w", in.RunID, err)
	}

	// The CAS pair. Both halves are compared, and the mismatch is reported as a
	// conflict rather than re-decided: see the file comment.
	if current.Revision != in.ExpectedRevision || current.Status != in.ExpectedStatus {
		return TransitionResult{}, &RevisionConflictError{
			RunID:         in.RunID,
			Expected:      in.ExpectedRevision,
			Current:       current.Revision,
			CurrentStatus: current.Status,
		}
	}

	transition, err := run.Decide(current.Status, in.Trigger)
	if err != nil {
		// Both sentinels are attached: ErrInvalidTransition for the class and the
		// frozen *run.TransitionError for the exact status, trigger and §21.1
		// failure code.
		return TransitionResult{}, fmt.Errorf("%w: %w", ErrInvalidTransition, err)
	}

	updated, err := UpdateRunStatusCAS(ctx, tx, in.RunID, in.ExpectedRevision, transition.To, in.At)
	if err != nil {
		return TransitionResult{}, fmt.Errorf("transition run %s --%s--> %s: %w",
			in.RunID, in.Trigger, transition.To, err)
	}

	result := TransitionResult{Run: updated, Transition: transition}
	if transition.StateEvent == "" {
		// completed + merge: no Run status event exists for it. The MergeOperation
		// records merge.completed when it reaches applied.
		return result, nil
	}

	payload, err := stateEventPayload(in, transition)
	if err != nil {
		return TransitionResult{}, err
	}
	identity, err := stateEventIdentity(updated, in)
	if err != nil {
		return TransitionResult{}, err
	}

	event, err := AppendEventTx(ctx, tx, EventInput{
		ProjectID:    updated.ProjectID,
		RunID:        stringPtrCopy(in.RunID),
		AttemptID:    in.AttemptID,
		Type:         string(transition.StateEvent),
		OccurredAt:   in.At,
		Identity:     identity,
		Payload:      payload,
		Destinations: in.Destinations,
	})
	if err != nil {
		// A state event the contract refuses fails the transition: the caller's
		// WithTx rolls the status change back, so "the Run moved but the fact was
		// not recorded" cannot happen (§19.3 item 1). The error keeps its own
		// sentinels (ErrInvalidEvent, *run.IdentityError) and gains this context.
		return TransitionResult{}, fmt.Errorf("transition run %s --%s--> %s: state event %s: %w",
			in.RunID, in.Trigger, transition.To, transition.StateEvent, err)
	}
	result.Event = &event
	return result, nil
}

// validateTransitionInput enforces the input contract before any SQL runs.
func validateTransitionInput(in TransitionInput) error {
	if strings.TrimSpace(in.RunID) == "" {
		return fmt.Errorf("%w: RunID is empty", ErrInvalidTransitionInput)
	}
	if in.ExpectedRevision < 1 {
		return fmt.Errorf("%w: ExpectedRevision %d must be >= 1", ErrInvalidTransitionInput, in.ExpectedRevision)
	}
	if !in.ExpectedStatus.Valid() {
		return fmt.Errorf("%w: ExpectedStatus %q is not a valid run status", ErrInvalidTransitionInput, in.ExpectedStatus)
	}
	if in.At.IsZero() {
		return fmt.Errorf("%w: At is zero", ErrInvalidTransitionInput)
	}
	if err := validateTransitionActor(in.Actor); err != nil {
		return err
	}
	if err := checkOptionalTransitionID("AttemptID", in.AttemptID); err != nil {
		return err
	}
	if err := checkOptionalTransitionID("AgentRevisionID", in.AgentRevisionID); err != nil {
		return err
	}
	return validateTransitionDetails(in.Details)
}

// validateTransitionActor checks the actor half of the state event identity: the
// type is one of the four fixed values, the id is present, and the source fits
// the wire limit. These are the same rules run.ExecutionIdentity.Validate
// applies; checking them here means a caller with a typo in the actor hears
// about it before a transaction is opened at all.
func validateTransitionActor(actor run.Actor) error {
	if !actor.Type.Valid() {
		return fmt.Errorf("%w: Actor.Type %q is not one of user/agent/system/integration",
			ErrInvalidTransitionInput, actor.Type)
	}
	if strings.TrimSpace(actor.ID) == "" {
		return fmt.Errorf("%w: Actor.ID is empty", ErrInvalidTransitionInput)
	}
	if len(actor.Source) > run.MaxActorSourceLength {
		return fmt.Errorf("%w: Actor.Source is %d bytes, limit is %d",
			ErrInvalidTransitionInput, len(actor.Source), run.MaxActorSourceLength)
	}
	return nil
}

// checkOptionalTransitionID refuses an optional id that is present but empty.
// nil means "this event has no attempt"; "" means the caller built the pointer
// from a field it forgot to fill, and it would name an attempt nobody can read.
func checkOptionalTransitionID(field string, value *string) error {
	if value != nil && strings.TrimSpace(*value) == "" {
		return fmt.Errorf("%w: %s is set but empty", ErrInvalidTransitionInput, field)
	}
	return nil
}

// validateTransitionDetails refuses a Details map that carries a reserved key
// or holds a value JSON cannot encode. Both are caught before any SQL so the
// caller's transaction is left untouched.
func validateTransitionDetails(details map[string]any) error {
	for key := range details {
		if IsReservedStateEventPayloadKey(key) {
			return fmt.Errorf("%w: Details carries the reserved payload key %q (the store writes it)",
				ErrInvalidTransitionInput, key)
		}
	}
	if len(details) == 0 {
		// Marshal(nil map) is `null`, not an object, which is why an empty
		// Details is not passed through the check below.
		return nil
	}
	encoded, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("%w: Details is not JSON-encodable: %v", ErrInvalidTransitionInput, err)
	}
	if !strings.HasPrefix(string(encoded), "{") {
		return fmt.Errorf("%w: Details did not encode to a JSON object", ErrInvalidTransitionInput)
	}
	return nil
}

// IsReservedStateEventPayloadKey reports whether a caller-supplied payload key
// collides with one the store writes. Matching is case-insensitive, so
// "FROM_STATUS" is refused as well as "from_status".
func IsReservedStateEventPayloadKey(key string) bool {
	normalized := strings.ToLower(key)
	for _, reserved := range ReservedStateEventPayloadKeys {
		if normalized == reserved {
			return true
		}
	}
	return false
}

// stateEventPayload builds the state event body: the caller's Details plus the
// fields §21.1 requires the transition to record.
func stateEventPayload(in TransitionInput, transition run.Transition) (json.RawMessage, error) {
	payload := make(map[string]any, len(in.Details)+len(ReservedStateEventPayloadKeys))
	for key, value := range in.Details {
		payload[key] = value
	}
	payload[payloadKeyFromStatus] = string(transition.From)
	payload[payloadKeyToStatus] = string(transition.To)
	payload[payloadKeyTrigger] = in.Trigger.String()
	if code := stateEventFailureCode(in.Trigger); code != "" {
		payload[payloadKeyFailureCode] = code
	}
	if terminal := expectedTerminalFor(in.Trigger, transition.To); terminal != "" {
		payload[payloadKeyExpectedTerminal] = string(terminal)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		// Unreachable: validateTransitionDetails already encoded the caller's
		// half, and the fields added here are strings.
		return nil, fmt.Errorf("%w: state event payload: %v", ErrInvalidTransitionInput, err)
	}
	return encoded, nil
}

// stateEventFailureCode is the §21.1 failure code the transition's row carries,
// or "" when the table gives none. run.FailureCodeFor falls back to
// invalid_transition for an operation the table has no code for (a non-zero
// process.exited, process_verified, cleaned, budget.soft_exceeded); that
// fallback is "no code", not a code, so it is not written into the payload of a
// successful transition.
func stateEventFailureCode(trigger run.Trigger) string {
	code := run.FailureCodeFor(trigger)
	if code == "" || code == run.CodeInvalidTransition {
		return ""
	}
	return code
}

// expectedTerminalFor is the terminal state a cancelling Run will end in, as
// recorded by the command that started the cancel (CA-1: a hard deadline ends
// expired even when a live process had to be terminated). It is written only on
// the transition into cancelling, which is the moment the cancel path records
// it.
func expectedTerminalFor(trigger run.Trigger, to run.RunStatus) run.RunStatus {
	if to != run.RunStatusCancelling || trigger.Kind != run.TriggerCommand {
		return ""
	}
	switch trigger.Name {
	case run.CommandRunCancel:
		return run.RunStatusCancelled
	case run.CommandHardDeadline:
		return run.RunStatusExpired
	default:
		return ""
	}
}

// stateEventIdentity builds the ExecutionIdentity envelope of the state event
// from the stored row and the caller's ids.
//
// project_id and run_id come from the row the event is written under (§27.4: the
// server derives the parent from the authoritative resource, never from what a
// body claims); attempt_id and agent_revision_id come from the caller, because
// only the caller knows which attempt the fact belongs to — and passing none for
// an event type that requires one is what makes AppendEventTx refuse the whole
// transition.
func stateEventIdentity(r run.Run, in TransitionInput) (json.RawMessage, error) {
	runID := r.ID
	identity := run.ExecutionIdentity{
		ProjectID:       r.ProjectID,
		RunID:           &runID,
		AttemptID:       in.AttemptID,
		AgentRevisionID: in.AgentRevisionID,
		Actor:           in.Actor,
	}
	encoded, err := json.Marshal(identity)
	if err != nil {
		return nil, fmt.Errorf("%w: state event identity: %v", ErrInvalidTransitionInput, err)
	}
	return encoded, nil
}

// stringPtrCopy returns a pointer to a copy of s, for the identity and event
// fields that must not alias the caller's struct.
func stringPtrCopy(s string) *string { return &s }
