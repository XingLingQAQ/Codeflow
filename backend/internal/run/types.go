// Package run defines the runtime domain's value types for the minimal
// persistent runtime store in codeflow.db: the read-only snapshots of legacy
// parents, the minimal Task with its claim fields, Run and Attempt.
//
// Conventions frozen by the plan (§19.1, §21.1, §27.1, §27.2):
//
//   - IDs are opaque UUID strings. This package never parses or normalises
//     them; callers treat them as byte strings.
//   - Every *_at and lease_until field is an instant that runstore persists as
//     Unix milliseconds (UTC) in an INTEGER column. The Go fields are
//     time.Time; the conversion (Time.UnixMilli / time.UnixMilli) happens only
//     in runstore.
//   - Money is an integer amount in minor units plus a currency code
//     (Budget.CostLimitMinor / Budget.Currency). Never a float.
//   - A nil pointer means "unknown" or "absent" and must not be read as a zero
//     value: an unknown token budget is not a zero token budget, and a nil
//     BaseCommit means "not a Git workspace", not "empty commit".
//   - The SQL CHECK constraints in runstore/migrations/001_runtime.sql must
//     accept exactly the constants declared here; the runstore schema tests
//     prove both sets are equal in both directions.
//   - Wire serialisation (JSON field names, RFC3339 UTC) belongs to the API
//     cards (T1.02/T1.11). Only Budget carries JSON tags, because the create
//     Run request body in §20.1 is frozen.
//
// Dependency direction: runstore may import run. run imports only the standard
// library and must never import runstore or any other business package.
package run

import "time"

// The exported slices below are the canonical, exhaustive value sets of each
// enum. They exist so that runstore's schema tests can compare the Go sets with
// the SQL CHECK/trigger sets in both directions instead of restating them: a
// constant added here but not to the migration (or the reverse) fails the test.
// Keep each slice in the same order as its declaration, and never use one as a
// validity check — call Valid().

// TaskKind classifies what a task produces. Mirrors the tasks.kind CHECK
// constraint.
type TaskKind string

const (
	// TaskKindCode produces a code change that must pass review and merge.
	TaskKindCode TaskKind = "code"
	// TaskKindDocument produces a document artifact with an explicit,
	// non-code acceptance rule.
	TaskKindDocument TaskKind = "document"
	// TaskKindManual is finished by an explicit human action.
	TaskKindManual TaskKind = "manual"
)

// TaskKinds lists every valid TaskKind.
var TaskKinds = []TaskKind{TaskKindCode, TaskKindDocument, TaskKindManual}

// Valid reports whether k is accepted by the tasks.kind CHECK constraint.
func (k TaskKind) Valid() bool {
	switch k {
	case TaskKindCode, TaskKindDocument, TaskKindManual:
		return true
	default:
		return false
	}
}

// TaskStatus is the lifecycle state of a Task (§27.2.2). The normal path is
// ready -> queued -> running -> waiting_review -> completed; a code task only
// reaches completed after MergeOperation.applied.
type TaskStatus string

const (
	TaskStatusReady         TaskStatus = "ready"
	TaskStatusQueued        TaskStatus = "queued"
	TaskStatusRunning       TaskStatus = "running"
	TaskStatusWaitingReview TaskStatus = "waiting_review"
	TaskStatusCompleted     TaskStatus = "completed"
	TaskStatusFailed        TaskStatus = "failed"
	TaskStatusCancelled     TaskStatus = "cancelled"
)

// TaskStatuses lists every valid TaskStatus, in lifecycle order.
var TaskStatuses = []TaskStatus{
	TaskStatusReady, TaskStatusQueued, TaskStatusRunning, TaskStatusWaitingReview,
	TaskStatusCompleted, TaskStatusFailed, TaskStatusCancelled,
}

// Valid reports whether s is accepted by the tasks.status CHECK constraint.
func (s TaskStatus) Valid() bool {
	switch s {
	case TaskStatusReady, TaskStatusQueued, TaskStatusRunning, TaskStatusWaitingReview,
		TaskStatusCompleted, TaskStatusFailed, TaskStatusCancelled:
		return true
	default:
		return false
	}
}

// IsTerminal reports whether s can never change again. failed is deliberately
// not terminal: an explicit retry command may CAS it back to queued (§27.2.7),
// which is why the DB trigger only freezes completed/cancelled.
func (s TaskStatus) IsTerminal() bool {
	switch s {
	case TaskStatusCompleted, TaskStatusCancelled:
		return true
	default:
		return false
	}
}

// RunStatus is the lifecycle state of a Run (§21.1). One Run is one execution
// over one frozen input; a retry creates a new Run.
type RunStatus string

const (
	RunStatusQueued          RunStatus = "queued"
	RunStatusStarting        RunStatus = "starting"
	RunStatusRunning         RunStatus = "running"
	RunStatusWaitingApproval RunStatus = "waiting_approval"
	RunStatusPaused          RunStatus = "paused"
	RunStatusCancelling      RunStatus = "cancelling"
	RunStatusRecovering      RunStatus = "recovering"
	RunStatusCompleted       RunStatus = "completed"
	RunStatusFailed          RunStatus = "failed"
	RunStatusCancelled       RunStatus = "cancelled"
	RunStatusExpired         RunStatus = "expired"
)

// RunStatuses lists every valid RunStatus, in the order of §21.1.
var RunStatuses = []RunStatus{
	RunStatusQueued, RunStatusStarting, RunStatusRunning, RunStatusWaitingApproval,
	RunStatusPaused, RunStatusCancelling, RunStatusRecovering,
	RunStatusCompleted, RunStatusFailed, RunStatusCancelled, RunStatusExpired,
}

// Valid reports whether s is accepted by the runs.status CHECK constraint.
func (s RunStatus) Valid() bool {
	switch s {
	case RunStatusQueued, RunStatusStarting, RunStatusRunning, RunStatusWaitingApproval,
		RunStatusPaused, RunStatusCancelling, RunStatusRecovering,
		RunStatusCompleted, RunStatusFailed, RunStatusCancelled, RunStatusExpired:
		return true
	default:
		return false
	}
}

// IsTerminal reports whether s is one of the four Run terminal states. Terminal
// states are irreversible in the DB, and no Run may leave them (§21.1).
func (s RunStatus) IsTerminal() bool {
	switch s {
	case RunStatusCompleted, RunStatusFailed, RunStatusCancelled, RunStatusExpired:
		return true
	default:
		return false
	}
}

// AttemptStatus is the lifecycle state of one backend process attempt. S1 runs
// exactly one Attempt per Run (§27.2.1).
type AttemptStatus string

const (
	AttemptStatusStarting   AttemptStatus = "starting"
	AttemptStatusRunning    AttemptStatus = "running"
	AttemptStatusExited     AttemptStatus = "exited"
	AttemptStatusTerminated AttemptStatus = "terminated"
	AttemptStatusAbandoned  AttemptStatus = "abandoned"
)

// AttemptStatuses lists every valid AttemptStatus. T1.02 may append values in a
// later migration but must not rename these.
var AttemptStatuses = []AttemptStatus{
	AttemptStatusStarting, AttemptStatusRunning, AttemptStatusExited,
	AttemptStatusTerminated, AttemptStatusAbandoned,
}

// Valid reports whether s is accepted by the attempts.status CHECK constraint.
func (s AttemptStatus) Valid() bool {
	switch s {
	case AttemptStatusStarting, AttemptStatusRunning, AttemptStatusExited,
		AttemptStatusTerminated, AttemptStatusAbandoned:
		return true
	default:
		return false
	}
}

// IsTerminal reports whether s can never change again.
func (s AttemptStatus) IsTerminal() bool {
	switch s {
	case AttemptStatusExited, AttemptStatusTerminated, AttemptStatusAbandoned:
		return true
	default:
		return false
	}
}

// IsActive reports whether s still owns the backend process. A Run may hold at
// most one active Attempt (enforced by a partial unique index).
func (s AttemptStatus) IsActive() bool {
	switch s {
	case AttemptStatusStarting, AttemptStatusRunning:
		return true
	default:
		return false
	}
}

// RefKind is the legacy resource family a read-only reference snapshot points
// at (§27.1 legacy_resource_refs). Legacy resources stay in the old database;
// this library only stores the verified snapshot, never a cross-file foreign
// key.
type RefKind string

const (
	RefKindFlow          RefKind = "flow"
	RefKindSession       RefKind = "session"
	RefKindBinding       RefKind = "binding"
	RefKindAgentRevision RefKind = "agent_revision"
)

// RefKinds lists every valid RefKind.
var RefKinds = []RefKind{RefKindFlow, RefKindSession, RefKindBinding, RefKindAgentRevision}

// Valid reports whether k is accepted by the legacy_resource_refs.kind CHECK
// constraint.
func (k RefKind) Valid() bool {
	switch k {
	case RefKindFlow, RefKindSession, RefKindBinding, RefKindAgentRevision:
		return true
	default:
		return false
	}
}

// ProjectRefState is the archived flag of the legacy project snapshot. It is
// the only mutable field of a project_refs row: §27.1 requires that a legacy
// project which was archived or rebound after capture refuses new runs.
type ProjectRefState string

const (
	ProjectRefStateActive   ProjectRefState = "active"
	ProjectRefStateArchived ProjectRefState = "archived"
)

// ProjectRefStates lists every valid ProjectRefState.
var ProjectRefStates = []ProjectRefState{ProjectRefStateActive, ProjectRefStateArchived}

// Valid reports whether s is accepted by the project_refs.state CHECK
// constraint.
func (s ProjectRefState) Valid() bool {
	switch s {
	case ProjectRefStateActive, ProjectRefStateArchived:
		return true
	default:
		return false
	}
}

// ProjectRef is the read-only snapshot of a legacy Project row, captured when
// the project first appears in the runtime library. It is the foreign-key
// anchor that makes "a wrong parent id cannot be inserted" hold inside
// codeflow.db without declaring a cross-file foreign key.
type ProjectRef struct {
	ProjectID string
	// SourceRevision is nil when the legacy projects table has no revision
	// column; it is not a substitute for the snapshot hash.
	SourceRevision *int64
	SnapshotHash   string
	State          ProjectRefState
	CapturedAt     time.Time // Unix milliseconds in the DB
	VerifiedAt     time.Time // Unix milliseconds in the DB
}

// LegacyResourceRef is the read-only snapshot of a legacy Flow, Session,
// WorkspaceBinding or AgentRevision that a runtime fact depends on. runstore
// re-verifies it at dispatch and merge time; an archived or rebound legacy
// resource makes the run refuse to start (§27.1).
type LegacyResourceRef struct {
	ProjectID string
	Kind      RefKind
	// ResourceID is the legacy resource's own id in the old database.
	ResourceID string
	// SourceRevision is nil when the legacy resource has no revision.
	SourceRevision *int64
	SnapshotHash   string
	CapturedAt     time.Time // Unix milliseconds in the DB
}

// Task is the minimal task row owned by the runtime library. flow_id and
// stage_id are nullable pointers into the legacy flow domain and are never
// validated by a cross-file foreign key.
type Task struct {
	ID        string
	ProjectID string
	// FlowID and StageID are nil when the task is not attached to a legacy
	// flow/stage.
	FlowID  *string
	StageID *string
	Title   string
	Kind    TaskKind
	Status  TaskStatus
	// Priority is a scheduling hint; 0 is the default and is not "unset".
	Priority int64
	// InputJSON is the canonical task input; InputHash is its content hash.
	// Both are immutable: changing a task's input requires a new task
	// (§27.2.7), and the DB trigger enforces it.
	InputJSON string
	InputHash string
	// LeaseOwner/LeaseUntil are the S1 single-worker claim. They are set or
	// cleared together (DB CHECK), and nil means "not claimed".
	LeaseOwner *string
	LeaseUntil *time.Time // Unix milliseconds in the DB
	// LeaseEpoch is the fencing token; it only ever grows.
	LeaseEpoch int64
	// Revision starts at 1 and is the CAS token for task updates.
	Revision  int64
	CreatedAt time.Time // Unix milliseconds in the DB
	UpdatedAt time.Time // Unix milliseconds in the DB
}

// Run is one execution over one frozen input. Every input column is immutable
// after insert (DB trigger run_frozen_input_immutable); only status, revision,
// updated_at and finished_at may move. A retry creates a new Run that points
// at the old one through RetryOfRunID.
type Run struct {
	ID        string
	TaskID    string
	ProjectID string
	// CommandID is the idempotency command that created the run; nil for runs
	// created by the system. UNIQUE when set, and several NULLs are allowed.
	CommandID *string
	// BindingID/BindingRevision name the workspace binding and its revision at
	// freeze time; the binding itself stays in the legacy library.
	BindingID       string
	BindingRevision int64
	// BaseManifestHash is the captured baseline manifest hash. It is required
	// even when the workspace has no Git, which is why BaseCommit is separate
	// and nullable.
	BaseManifestHash string
	// BaseCommit is nil for a non-Git workspace (§27.5.1).
	BaseCommit      *string
	AgentRevisionID string
	// InputSnapshotID/InputSnapshotHash pin the frozen input; the snapshot body
	// lives in runstore (T1.01.b) and never contains secret values.
	InputSnapshotID   string
	InputSnapshotHash string
	// Budget is persisted as budget_json (TEXT, NOT NULL, default '{}').
	Budget   Budget
	Status   RunStatus
	Revision int64
	// RetryOfRunID points at the run this one retries; nil for a first try.
	RetryOfRunID *string
	CreatedAt    time.Time  // Unix milliseconds in the DB
	UpdatedAt    time.Time  // Unix milliseconds in the DB
	FinishedAt   *time.Time // Unix milliseconds in the DB; nil while not terminal
}

// Attempt is one backend process attempt for a Run. run_id, attempt_no and
// backend are immutable; the process identity fields are recorded so a
// restarted server can verify ownership before it kills or re-dispatches
// anything (§27.5.6).
type Attempt struct {
	ID        string
	RunID     string
	AttemptNo int64
	Backend   string
	// BackendVersion is nil when the backend did not report a version.
	BackendVersion *string
	Status         AttemptStatus
	// OwnerInstance identifies the server instance that started the process;
	// nil while unknown (e.g. an attempt adopted after a restart).
	OwnerInstance *string
	// PID and ProcessStartID are both required to prove process ownership;
	// a bare PID is not enough (§27.5.6).
	PID            *int64
	ProcessStartID *string
	StartedAt      *time.Time // Unix milliseconds in the DB
	FinishedAt     *time.Time // Unix milliseconds in the DB
	ExitCode       *int64
	ExitReason     *string
	CreatedAt      time.Time // Unix milliseconds in the DB
}

// Budget is the request budget of a Run, matching the create-Run body in
// §20.1 ({"wall_time_seconds":1800,"tokens":50000}).
//
// Every limit is optional and nil means "unknown / not budgeted" — it must not
// be read as zero, and it must not be rendered as 0 on the wire. CostLimitMinor
// is an integer amount in minor units (cents) and Currency is its ISO code, so
// money never goes through a float.
type Budget struct {
	WallTimeSeconds *int64 `json:"wall_time_seconds,omitempty"`
	Tokens          *int64 `json:"tokens,omitempty"`
	CostLimitMinor  *int64 `json:"cost_limit_minor,omitempty"`
	Currency        string `json:"currency,omitempty"`
}
