package run

import (
	"context"
	"encoding/json"
	"time"
)

// This file declares the dependency interfaces of the Run command service
// (T1.04.a). The run package imports only the standard library (types.go
// package comment), so nothing the service needs may be named as a concrete
// type from another package: every dependency is an interface declared here and
// implemented elsewhere. runstore implements RunStore, CommandLedger and RunTx;
// the API layer and the background scheduler each pass their own AnswerEncoder
// (see CommandContext) so one piece of command logic serves both.
//
// The interfaces are deliberately narrow: each one names only the operations
// the command service performs, so a test can implement them without a
// database, and a later card can add an implementation without editing this
// file. An error returned by a method that only reads comes from the
// implementation; this step deliberately introduces no new sentinel error for
// "not found" — whether a missing row is (ErrNotFound, nil) or a typed zero
// value is the implementer's contract, and the command methods of T1.04.b are
// written against the implementation that exists then.

// RunStoreOwnership is a compile-time reminder, not a declaration: runstore owns
// the only production implementation of RunStore, CommandLedger and RunTx, and
// runstore.Owner... simply has no corresponding symbol here. The comment exists
// so a reader does not look for a *run.Store.

// RunStore is the storage dependency of the command service: it opens the one
// transaction a command is atomic in. runstore.Store implements it with
// Store.WithTx (store.go), adapted to this signature — its callback takes a
// runstore.Tx, which the adapter wraps as a RunTx.
//
// The implementation must make fn's effects and the ledger claim of
// CommandLedger.Execute commit or roll back together (§27.3: 事务先占 key，只有
// 拥有者产生副作用). WithinTx never partially commits: an error from fn rolls
// everything back.
type RunStore interface {
	// WithinTx runs fn inside one transaction with BEGIN IMMEDIATE semantics and
	// commits when fn returns nil, rolls back otherwise. fn receives the ctx the
	// implementation hands it (runstore passes the caller's through unchanged) and
	// must use that ctx for every call it makes inside the transaction.
	WithinTx(ctx context.Context, fn func(ctx context.Context, tx RunTx) error) error
}

// RunTx is one transaction of the runtime library: the reads and the two
// compare-and-swap writes the command service needs. runstore implements it over
// its own Tx.
//
// Every read returns the zero value of the result type when the row does not
// exist and the error the implementation defines for that case; which error
// that is, is fixed by the implementation (runstore's ErrNotFound today) and
// not by this interface.
type RunTx interface {
	// GetTask returns the task row. It is how a command checks the task status,
	// revision and input hash it CASes against.
	GetTask(ctx context.Context, taskID string) (Task, error)
	// GetRun returns the run row. It is how a command reads the revision and
	// status its transition is based on.
	GetRun(ctx context.Context, runID string) (Run, error)
	// GetAttempt returns the attempt row named by attemptID.
	GetAttempt(ctx context.Context, attemptID string) (Attempt, error)
	// ListRunsByTask returns every run of one task. The order is the
	// implementation's; a caller that needs an order sorts.
	ListRunsByTask(ctx context.Context, taskID string) ([]Run, error)
	// ListRunsByProject returns at most limit runs of one project, most recent
	// first. limit must be positive; a caller that wants no bound passes a
	// value the caller owns, because the interface defines no "unlimited".
	// No runstore implementation exists yet; T1.04.b adds it.
	ListRunsByProject(ctx context.Context, projectID string, limit int) ([]Run, error)
	// InsertInputSnapshot stores the frozen input body and returns the generated
	// snapshot id. projectID scopes the row; content is the canonical input
	// JSON; now is the capture instant (Unix milliseconds in the DB). It is a
	// separate call so the snapshot and the run it belongs to commit together.
	InsertInputSnapshot(ctx context.Context, projectID, snapshotID string, content json.RawMessage, now time.Time) (string, error)
	// InsertRun stores a new run. r must carry every immutable input column;
	// the implementation refuses an id that already exists (runs.command_id is
	// UNIQUE when set).
	InsertRun(ctx context.Context, r *Run) error
	// TransitionRun moves one run according to §21.1 with the CAS pair the
	// caller read, and writes the transition's state event in the same
	// transaction. runstore.TransitionRunTx is the operation this method wraps.
	TransitionRun(ctx context.Context, in RunTransitionInput) (RunTransitionResult, error)
	// RetryRun creates the new run of an explicit retry and CASes its task back
	// to queued, in the same transaction. runstore.RetryRunTx is the operation
	// this method wraps.
	RetryRun(ctx context.Context, in RetryRunInput) (RetryRunResult, error)
}

// RunTransitionInput is one request to move a run. It corresponds field for
// field to runstore.TransitionInput (transition.go) and exists so that run does
// not import runstore; a future field must be added to both.
type RunTransitionInput struct {
	// RunID is the run to move. Required.
	RunID string
	// ExpectedRevision is the revision the caller read; a stored revision that
	// differs is a conflict, not a retry.
	ExpectedRevision int64
	// ExpectedStatus is the status the caller read and the status its trigger
	// decision is valid for.
	ExpectedStatus RunStatus
	// Trigger is the §21.1 event, command or conclusion named by the caller.
	Trigger Trigger
	// At is when the transition happened, not when it was written. Required.
	At time.Time
	// Actor is the actor of the state event's identity. Required; whether it is
	// authorised is the policy gate's decision, not this input's.
	Actor Actor
	// AttemptID names the live attempt for the state events that require one;
	// nil for an event that does not — including every cancel of a queued run.
	AttemptID *string
	// AgentRevisionID is the immutable agent revision the live attempt belongs
	// to; required by the same events as AttemptID.
	AgentRevisionID *string
	// Details is the caller's own payload, merged into the state event payload
	// as a flat object. Reserved keys are refused by the implementation rather
	// than overwritten.
	Details map[string]any
	// Destinations names the outbox deliveries to queue for the state event, in
	// the same transaction. Empty is legal.
	Destinations []string
}

// RunTransitionResult is the outcome of an accepted transition, corresponding
// field for field to runstore.TransitionResult.
type RunTransitionResult struct {
	// Run is the stored row after the CAS.
	Run Run
	// Transition is the frozen decision: From, To and the state event type.
	Transition Transition
	// Event is the state event written in the same transaction, or nil when the
	// transition writes none. That is exactly the merge command of a completed
	// run today (§21.1, runstore.TransitionResult.Event).
	Event *StateEvent
}

// StateEvent is one appended execution event, flattened to what a command
// service can consume without knowing the storage schema. It corresponds to the
// scalar fields of runstore.Event; runstore keeps the identity envelope and the
// payload hash, which the command service never reads.
type StateEvent struct {
	// ID is the server-generated event id.
	ID string
	// Type is a value of the closed execution-event enum (ExecutionEventType).
	Type ExecutionEventType
	// ProjectID and ProjectSeq are the project-scoped identity and monotonic
	// sequence.
	ProjectID  string
	ProjectSeq int64
	// RunID and RunSeq are the run-scoped identity and sequence.
	RunID  string
	RunSeq int64
	// OccurredAt is when the fact happened.
	OccurredAt time.Time
	// Payload is the event's payload JSON.
	Payload json.RawMessage
}

// RetryRunInput is one explicit retry request. It corresponds field for field to
// runstore.RetryInput (retry.go).
type RetryRunInput struct {
	// OriginalRunID is the run to retry: it must exist and have ended without
	// completing.
	OriginalRunID string
	// NewRunID is the new run's id, generated by the caller.
	NewRunID string
	// ExpectedTaskRevision is the task revision the caller read; the retry CASes
	// the task, so a concurrent change is a conflict.
	ExpectedTaskRevision int64
	// At is when the retry happened. Required.
	At time.Time
	// CommandID is the idempotency command that requested the retry; nil for a
	// system-initiated retry.
	CommandID *string
}

// RetryRunResult is the outcome of an accepted retry, corresponding field for
// field to runstore.RetryResult.
type RetryRunResult struct {
	// Run is the new run as stored.
	Run Run
	// Task is the task after its CAS.
	Task Task
}

// CommandKey is the idempotency scope of one command (§27.3):
// (principal, project, operation, command id). It corresponds to
// runstore.CommandKey, which the ledger implementation receives.
type CommandKey struct {
	// PrincipalID is the authenticated principal the command is executed for.
	PrincipalID string
	// ProjectID is the project the command acts on.
	ProjectID string
	// Operation names what the command does, e.g. "runs.create". The vocabulary
	// belongs to the API layer; it is part of the key, so it must be stable for
	// one route across releases.
	Operation string
	// CommandID is the client's own Idempotency-Key, generated before the
	// request was sent. The server never generates it.
	CommandID string
}

// AnswerKind says whether a recorded answer was executed now or replayed.
type AnswerKind string

const (
	// AnswerApplied means this call produced the answer.
	AnswerApplied AnswerKind = "applied"
	// AnswerRejected means the command was understood and refused, and the
	// refusal is recorded: a retry replays it instead of executing again.
	AnswerRejected AnswerKind = "rejected"
)

// CommandAnswer is the complete answer of one command, as bytes.
//
// Body is the exact response body the caller is given and the ledger records,
// including the request_id of this first request: a replay after a lost
// response must be byte-identical to the first answer (§27.3). Because run does
// not know HTTP, the structured result is turned into these bytes by the
// caller's AnswerEncoder (CommandContext.Encode) before the ledger sees it.
type CommandAnswer struct {
	// Kind is AnswerApplied when the callback applied the command and
	// AnswerRejected when it refused it. A rejection is recorded, not rolled
	// back, so the retry replays the same refusal.
	Kind AnswerKind
	// StatusCode is the recorded status (201, 202, 409, ...).
	StatusCode int
	// Body is the recorded response body. It must be non-empty for a terminal
	// answer.
	Body json.RawMessage
	// ResourceType and ResourceID name what the command touched, when it named
	// something. Both empty means "no resource".
	ResourceType string
	ResourceID   string
}

// CommandDisposition says what the ledger did with this call.
type CommandDisposition string

const (
	// CommandExecuted means the callback ran now and its answer was recorded.
	CommandExecuted CommandDisposition = "executed"
	// CommandReplayed means the key was already used with the same request hash
	// and the recorded answer was returned; the callback did not run.
	CommandReplayed CommandDisposition = "replayed"
)

// CommandLedger is the idempotency ledger of every write command (§27.3). It
// wraps runstore's claim/record functions (commands.go) into one operation,
// because the claim, the callback and the recording must commit or roll back
// together.
type CommandLedger interface {
	// Execute claims key for the request identified by requestHash and runs fn
	// in the same transaction as the claim.
	//
	//   - The key is claimed before fn runs, so two concurrent callers cannot
	//     both execute (§27.3).
	//   - fn's answer is recorded and returned with CommandExecuted.
	//   - The same key with the same request hash returns the recorded answer
	//     and CommandReplayed without running fn. The recorded bytes are
	//     returned unchanged: the replay is byte-identical to the first answer.
	//   - The same key with a different request hash returns
	//     ErrCommandKeyReused and runs nothing. It must never answer with the
	//     first command's result, because that would tell the client its new
	//     request had been applied.
	//   - fn returning a non-nil error rolls the transaction back and releases
	//     the key: the caller may retry the same key, and nothing was recorded.
	//     That is how a system error differs from a business refusal: a refusal
	//     is returned as a CommandAnswer, not as an error.
	Execute(ctx context.Context, key CommandKey, requestHash string, fn func(ctx context.Context, tx RunTx) (CommandAnswer, error)) (CommandAnswer, CommandDisposition, error)
}

// CommandRejection is a business refusal of a command: the request was
// understood, the caller was authorised, and the answer is a refusal the client
// should see replayed rather than re-executed. It corresponds to
// handlers.CommandRejection, minus the HTTP context.
//
// Code is a plain string rather than a shared enum on purpose: the closed set of
// error codes belongs to the transport layer, and its encoder (AnswerEncoder) is
// what validates that Code is a member — exactly as handlers.rejectionResponse
// validates it today. run must not grow a second copy of that vocabulary.
type CommandRejection struct {
	// StatusCode is the status to answer with (409, 422, 403, ...).
	StatusCode int
	// Code is one member of the transport layer's closed set of error codes.
	Code string
	// Message is the human-readable text.
	Message string
	// Retryable tells the client whether retrying the same request could
	// succeed; it is part of the recorded refusal, not a control-flow flag.
	Retryable bool
	// Details is optional structured context.
	Details map[string]any
}

// CommandResponse is the structured result of one command execution, handed to
// an AnswerEncoder to become the CommandAnswer the caller and the ledger see.
// It corresponds to handlers.CommandResult plus the refusal case.
type CommandResponse struct {
	// StatusCode is the status of the response (201, 202, 200, ...).
	StatusCode int
	// Data is the success payload. It is the whole answer: a retry replays the
	// encoded bytes of this and nothing else.
	Data any
	// Rejection is non-nil when the command was refused; Data is then ignored.
	// A non-nil Rejection produces an AnswerRejected answer, which is recorded
	// rather than rolled back.
	Rejection *CommandRejection
	// ResourceType and ResourceID name what the command created, when it created
	// something. Both empty means "no resource".
	ResourceType string
	ResourceID   string
	// Revision is the new revision of the resource the command wrote, when it
	// has one; 0 when it has none. It is not part of the response body.
	Revision int64
}

// AnswerEncoder turns a structured CommandResponse into the recorded answer.
//
// It is supplied per call, not per service (CommandContext.Encode), because the
// caller is the only one who knows the request id, the correlation header and
// the wire shape: the HTTP handler encodes its own envelope, and the background
// scheduler supplies an encoder that needs no request. Encoding happens inside
// the ledger transaction, so an encoder error aborts the command and releases
// the key.
type AnswerEncoder func(resp CommandResponse) (CommandAnswer, error)

// CommandContext is what the transport layer supplies for one command call. The
// key and the request hash come from the request; the encoder comes from the
// caller. It is not a ServiceDeps field, because it changes with every request.
type CommandContext struct {
	// Key is the idempotency scope of this command. Required: the ledger
	// refuses an incomplete key.
	Key CommandKey
	// RequestHash identifies the request body the key was claimed for. A retry
	// with the same key must present the same hash.
	RequestHash string
	// Encode turns the structured result into the recorded answer bytes.
	// Required.
	Encode AnswerEncoder
}

// ProjectSnapshot is the verified state of a project at command time. The
// project itself lives in the legacy library; this is what the command service
// reads to refuse a command against an archived or rebound project (§27.1).
type ProjectSnapshot struct {
	// ProjectID is the project's own id.
	ProjectID string
	// Status is the project's lifecycle status as the project layer spells it.
	Status string
	// Archived is true when the project is archived: an archived project
	// refuses new runs.
	Archived bool
	// Revision is the project's revision at read time, for a caller that must
	// record what it verified.
	Revision int64
}

// BindingSnapshot is the verified workspace binding a run freezes. It
// corresponds to the project layer's WorkspaceBindingSnapshot (T1.03).
type BindingSnapshot struct {
	// BindingID is the binding's id in the legacy library.
	BindingID string
	// Revision is the binding revision at capture time; the run stores it.
	Revision int64
	// Kind is the binding kind (the workspace's storage kind).
	Kind string
	// State is the binding's lifecycle state; a non-active binding refuses a
	// new run.
	State string
	// Root is the canonicalised workspace root.
	Root string
	// RootIdentity is the verified identity of the root (device/inode or the
	// platform's equivalent), used to detect a rebound root.
	RootIdentity string
}

// ProjectResolver reads the project facts a run command must verify before it
// writes anything.
type ProjectResolver interface {
	// ResolveProject returns the project's current snapshot. The error for a
	// project that does not exist is the implementation's.
	ResolveProject(ctx context.Context, projectID string) (ProjectSnapshot, error)
	// PrimaryBinding returns the project's primary workspace binding, which is
	// what a new run freezes. A project without a usable binding fails with the
	// implementation's error.
	PrimaryBinding(ctx context.Context, projectID string) (BindingSnapshot, error)
}

// FlowParent is the legacy flow/stage a task and its run belong to (§27.1).
// Both are legacy ids; the runtime library stores them as nullable references
// and never validates them with a cross-file foreign key.
type FlowParent struct {
	// FlowID is the legacy flow id.
	FlowID string
	// StageID is the legacy stage id.
	StageID string
	// FlowStatus is the flow's status as the flow layer spells it.
	FlowStatus string
	// StageStatus is the stage's status.
	StageStatus string
}

// FlowResolver reads the flow and stage a task belongs to, and their statuses:
// a run may only start for a task whose parent flow and stage still accept it.
type FlowResolver interface {
	// ResolveTaskParent returns the flow and stage the task is attached to. A
	// task attached to no legacy flow fails with the implementation's error;
	// this step fixes no sentinel for it.
	ResolveTaskParent(ctx context.Context, projectID string, task Task) (FlowParent, error)
}

// AgentRevisionSnapshot is the immutable agent revision a run pins. It is what
// select the backend, so it must be enabled at command time.
type AgentRevisionSnapshot struct {
	// RevisionID is the revision's own id; it is stored on the run.
	RevisionID string
	// AgentID is the agent the revision belongs to.
	AgentID string
	// Backend is the execution backend the revision runs on, as the backend
	// catalog names it.
	Backend string
	// Enabled is false when the revision has been disabled: a disabled revision
	// refuses a new run.
	Enabled bool
}

// AgentResolver reads the agent revision named by a run request.
type AgentResolver interface {
	// ResolveAgentRevision returns the revision snapshot. The error for an
	// unknown revision is the implementation's.
	ResolveAgentRevision(ctx context.Context, projectID, agentRevisionID string) (AgentRevisionSnapshot, error)
}

// BackendCapabilitySnapshot is what one execution backend can do right now. It
// is a capability report, never a health probe of the model itself.
type BackendCapabilitySnapshot struct {
	// Backend is the backend's own name, as AgentRevisionSnapshot.Backend and
	// Attempt.Backend spell it.
	Backend string
	// Available is false when the backend cannot be used at all; Reason then
	// says why.
	Available bool
	// Capabilities is the set of capability names the backend reported, in the
	// backend's own vocabulary.
	Capabilities []string
	// Reason explains an unavailable backend, or an empty capability set, in
	// human-readable text.
	Reason string
}

// BackendCatalog reports what an execution backend supports. The execbackend
// package owns the capability vocabulary; this interface carries the names
// verbatim so run does not import it.
type BackendCatalog interface {
	// BackendCapabilities returns the current snapshot for one backend. An
	// unknown backend name fails with the implementation's error.
	BackendCapabilities(ctx context.Context, backend string) (BackendCapabilitySnapshot, error)
}

// BaselineSnapshot is the captured baseline a run freezes: the manifest hash of
// the workspace, and the commit it was taken at when the workspace is a Git
// repository.
type BaselineSnapshot struct {
	// ManifestHash is the manifest hash of the captured workspace. Required
	// even when the workspace has no Git.
	ManifestHash string
	// BaseCommit is nil for a non-Git workspace (§27.5.1); it is never an empty
	// commit.
	BaseCommit *string
}

// BaselineCapturer captures the workspace baseline for a run. The runworkspace
// package implements it.
type BaselineCapturer interface {
	// CaptureBaseline captures the baseline of the verified binding. It must be
	// called with the binding snapshot the run freezes, because the capture and
	// the run are only consistent when they name the same root revision.
	CaptureBaseline(ctx context.Context, binding BindingSnapshot) (BaselineSnapshot, error)
}

// RunPolicyRequest is what the policy gate is asked about one command. Every
// field is context the gate may rule on; the empty string means "not applicable
// to this command".
type RunPolicyRequest struct {
	// Operation names the command, matching CommandKey.Operation
	// ("runs.create", "runs.cancel", ...).
	Operation string
	// PrincipalID is the authenticated principal the command runs as.
	PrincipalID string
	// ProjectID is the project the command acts on.
	ProjectID string
	// TaskID is the task the command acts on, when it names one.
	TaskID string
	// RunID is the run the command acts on, when it names one.
	RunID string
	// AgentRevisionID is the agent revision the command would pin, when the
	// command chooses one.
	AgentRevisionID string
}

// PolicyVerdict is the policy gate's answer. RuleVersion names the rule that
// decided, so a refusal can be audited against the policy that produced it.
type PolicyVerdict struct {
	// Allowed is false when the command is refused by policy.
	Allowed bool
	// Reason explains a refusal in human-readable text.
	Reason string
	// RuleVersion identifies the policy revision that decided.
	RuleVersion string
}

// PolicyGate authorises one run command. The policy package implements it; the
// gate decides authorisation only — a denial is a business refusal
// (CommandRejection), while a gate that cannot answer at all returns an error.
type PolicyGate interface {
	// AuthorizeRunCommand returns the verdict for one command. A verdict with
	// Allowed false is not an error.
	AuthorizeRunCommand(ctx context.Context, req RunPolicyRequest) (PolicyVerdict, error)
}
