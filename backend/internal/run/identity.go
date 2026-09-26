package run

import (
	"errors"
	"fmt"
	"strings"
)

// This file fixes the execution-identity contract of plan §27.1 / §20.2 as Go
// types, so that the event producers (T0.03/T1.05) and the API layer (T1.11)
// cannot disagree with schemas/execution-identity.schema.json or with
// schemas/execution-event.schema.json. The JSON tags below are the wire names
// of that schema; identity_test.go reads both schema files and fails if the
// value sets or the property names drift.
//
// §27.1 rules encoded here:
//   - every event carries project_id and an actor;
//   - a Run event adds run_id;
//   - attempt_id/agent_revision_id exist only while an attempt is alive;
//   - Gate / manual-document / system-recovery events may have no Run at all.
// §28 adds: a queued-stage event may have no attempt, a tool event must have
// one.

// ExecutionEventType is a type of the closed execution-event enum in
// schemas/execution-event.schema.json. The enum is closed: producers must not
// send anything else, and a consumer that meets a type it does not know must
// keep its cursor moving instead of stopping the stream (schema description,
// §27.4).
type ExecutionEventType string

// The event types of the closed enum, in the schema's declaration order: the 15
// types T0.03 named from the plan, plus the six run.* state events contract
// amendment CA-1 (plan §26.31) added so that every §21.1 transition has an event
// type to write in its CAS transaction (§19.3 item 1), plus legacy.flow_event,
// which contract amendment CA-2 (plan §26.31) added for the T1.05.c projection
// of pre-3.0 Flow timelines.
const (
	// EventApprovalApproved: a waiting_approval Run is approved (§21.1).
	EventApprovalApproved ExecutionEventType = "approval.approved"
	// EventApprovalDecided: an approval decision was recorded (§19.3). This is
	// the human decision event of a Gate or a manual document, which may exist
	// without a Run.
	EventApprovalDecided ExecutionEventType = "approval.decided"
	// EventApprovalRequired: a running Run needs approval (§21.1).
	EventApprovalRequired ExecutionEventType = "approval.required"
	// EventBudgetSoftExceeded: the soft budget was exceeded; it never fakes a
	// pause (§21.1).
	EventBudgetSoftExceeded ExecutionEventType = "budget.soft_exceeded"
	// EventBudgetWarning: a budget warning notification (§15 T9.01).
	EventBudgetWarning ExecutionEventType = "budget.warning"
	// EventCheckpointAcknowledged: the backend acknowledged a checkpoint; the
	// Run may be paused on it (§21.1).
	EventCheckpointAcknowledged ExecutionEventType = "checkpoint.acknowledged"
	// EventLegacyFlowEvent: one timeline entry of a pre-3.0 Flow, projected from
	// the legacy flow store's local outbox after the Flow write committed
	// (T1.05.c, CA-2). The legacy name (flow.created, stage.done, gate.approved,
	// ...) travels in payload.source_type, so the enum does not grow a second
	// copy of the Flow vocabulary that T3.01 will replace. A Flow is not a Run:
	// the event is project-scoped and carries no run_id.
	EventLegacyFlowEvent ExecutionEventType = "legacy.flow_event"
	// EventMergeCompleted: a MergeOperation reached applied (§19.3).
	EventMergeCompleted ExecutionEventType = "merge.completed"
	// EventProcessExited: the backend process exited, with its exit code (§21.1).
	EventProcessExited ExecutionEventType = "process.exited"
	// EventProcessStarted: the backend process started and is identified (§21.1).
	EventProcessStarted ExecutionEventType = "process.started"
	// EventProcessTerminated: the process tree was terminated and confirmed
	// (§21.1).
	EventProcessTerminated ExecutionEventType = "process.terminated"
	// EventRunCancelRequested: a run.cancel command or the hard deadline moved
	// the Run to cancelling; the payload records the expected terminal state
	// (cancelled or expired) and the reason (CA-1).
	EventRunCancelRequested ExecutionEventType = "run.cancel_requested"
	// EventRunCancelled: a Run reached cancelled (CA-1).
	EventRunCancelled ExecutionEventType = "run.cancelled"
	// EventRunCompleted: a Run reached completed (§15 T9.01).
	EventRunCompleted ExecutionEventType = "run.completed"
	// EventRunExpired: a Run reached expired, the terminal state of a hard
	// deadline (CA-1).
	EventRunExpired ExecutionEventType = "run.expired"
	// EventRunFailed: a Run reached failed (§15 T9.01).
	EventRunFailed ExecutionEventType = "run.failed"
	// EventRunReattached: the recoverer verified that the attempt's process is
	// still its own and the Run returned to running (CA-1).
	EventRunReattached ExecutionEventType = "run.reattached"
	// EventRunRecovering: the Run moved to recovering, after a server restart
	// or a process tree that could not be confirmed gone (CA-1).
	EventRunRecovering ExecutionEventType = "run.recovering"
	// EventRunResumed: a paused Run resumed from its checkpoint (CA-1).
	EventRunResumed ExecutionEventType = "run.resumed"
	// EventSchedulerClaimed: the scheduler claimed a queued Run (§21.1).
	EventSchedulerClaimed ExecutionEventType = "scheduler.claimed"
	// EventServerRestart: the server restarted and must recover what it owned
	// (§21.1, §27.1).
	EventServerRestart ExecutionEventType = "server.restart"
	// EventToolRequested: the executing agent requested a tool (§20.2, §28).
	EventToolRequested ExecutionEventType = "tool.requested"
)

// ExecutionEventTypes lists every valid ExecutionEventType in schema order.
var ExecutionEventTypes = []ExecutionEventType{
	EventApprovalApproved, EventApprovalDecided, EventApprovalRequired,
	EventBudgetSoftExceeded, EventBudgetWarning, EventCheckpointAcknowledged,
	EventLegacyFlowEvent, EventMergeCompleted, EventProcessExited, EventProcessStarted,
	EventProcessTerminated, EventRunCancelRequested, EventRunCancelled,
	EventRunCompleted, EventRunExpired, EventRunFailed, EventRunReattached,
	EventRunRecovering, EventRunResumed, EventSchedulerClaimed,
	EventServerRestart, EventToolRequested,
}

// Valid reports whether t is one of the enum values.
func (t ExecutionEventType) Valid() bool {
	switch t {
	case EventApprovalApproved, EventApprovalDecided, EventApprovalRequired,
		EventBudgetSoftExceeded, EventBudgetWarning, EventCheckpointAcknowledged,
		EventLegacyFlowEvent, EventMergeCompleted, EventProcessExited, EventProcessStarted,
		EventProcessTerminated, EventRunCancelRequested, EventRunCancelled,
		EventRunCompleted, EventRunExpired, EventRunFailed, EventRunReattached,
		EventRunRecovering, EventRunResumed, EventSchedulerClaimed,
		EventServerRestart, EventToolRequested:
		return true
	default:
		return false
	}
}

// ActorType is the actor type enum of execution-identity.schema.json, fixed by
// §15 T0.10 / I-50: user, agent, system, integration. The run package declares
// its own copy because it may import only the standard library; the values must
// stay identical to internal/audit's ActorType constants.
type ActorType string

const (
	// ActorTypeUser is a terminal user, e.g. an approver or the cancel issuer.
	ActorTypeUser ActorType = "user"
	// ActorTypeAgent is an AI agent execution body.
	ActorTypeAgent ActorType = "agent"
	// ActorTypeSystem is the server itself: the recoverer, background jobs,
	// migrations.
	ActorTypeSystem ActorType = "system"
	// ActorTypeIntegration is a verified external integration.
	ActorTypeIntegration ActorType = "integration"
)

// ActorTypes lists every valid ActorType.
var ActorTypes = []ActorType{ActorTypeUser, ActorTypeAgent, ActorTypeSystem, ActorTypeIntegration}

// Valid reports whether t is one of the four actor types.
func (t ActorType) Valid() bool {
	switch t {
	case ActorTypeUser, ActorTypeAgent, ActorTypeSystem, ActorTypeIntegration:
		return true
	default:
		return false
	}
}

// Actor is the actor object of execution-identity.schema.json
// (Actor{type,id,source} of §15 T0.10).
//
// Source records where the identity was established, not what the client
// claimed: the server must verify the parent/child relation against the
// authoritative resource and never trust an actor self-reported in an HTTP
// body (schema description). Source is optional on the wire.
type Actor struct {
	Type   ActorType `json:"type"`
	ID     string    `json:"id"`
	Source string    `json:"source,omitempty"`
}

// Identity length limits, taken from execution-identity.schema.json:
// project_id, actor.id, run_id, attempt_id and agent_revision_id are all
// minLength 1 / maxLength 128, actor.source is maxLength 128. (The audit layer
// caps Source at 64 for its own storage; the wire contract is 128.)
const (
	// MaxIdentityIDLength is the maxLength of every identity id on the wire.
	MaxIdentityIDLength = 128
	// MaxActorSourceLength is the maxLength of actor.source on the wire.
	MaxActorSourceLength = 128
)

// ExecutionIdentity is the identity object of
// schemas/execution-identity.schema.json: required project_id + actor, optional
// run_id/attempt_id/agent_revision_id which are null or absent when they do not
// apply. The JSON tags are the schema's property names, in the schema's order.
type ExecutionIdentity struct {
	ProjectID string `json:"project_id"`
	// RunID is required for Run events and absent for Gate/manual-document/
	// system-recovery events.
	RunID *string `json:"run_id,omitempty"`
	// AttemptID exists only while an attempt is alive (§27.1).
	AttemptID *string `json:"attempt_id,omitempty"`
	// AgentRevisionID is the immutable agent revision primary key, not the
	// user-facing AgentAsset.Version label (§27.1).
	AgentRevisionID *string `json:"agent_revision_id,omitempty"`
	Actor           Actor   `json:"actor"`
}

// Identity error sentinels, for errors.Is.
var (
	// ErrUnknownEventType is reported when an event type is outside the closed
	// enum.
	ErrUnknownEventType = errors.New("run: unknown execution event type")
	// ErrMissingIdentityField is reported when a field an event type requires is
	// absent.
	ErrMissingIdentityField = errors.New("run: missing required identity field")
	// ErrInvalidIdentityField is reported when a field is present but malformed.
	ErrInvalidIdentityField = errors.New("run: invalid identity field")
)

// IdentityError names the offending field, so a producer can fix the exact
// field instead of guessing. Field is a schema property name (project_id,
// actor.type, run_id, ...); EventType is empty when only the shape was checked.
type IdentityError struct {
	EventType string
	Field     string
	// Missing is true when the field is required by the event type and absent,
	// false when the field is present but invalid.
	Missing bool
	// Reason explains the rejection (empty, too long, not in the enum, ...).
	Reason string
}

// Error implements error.
func (e *IdentityError) Error() string {
	what := "invalid"
	if e.Missing {
		what = "required"
	}
	if e.EventType != "" {
		return fmt.Sprintf("run: identity for event %q: field %q is %s: %s",
			e.EventType, e.Field, what, e.Reason)
	}
	return fmt.Sprintf("run: identity: field %q is %s: %s", e.Field, what, e.Reason)
}

// Is lets errors.Is report the matching sentinel.
func (e *IdentityError) Is(target error) bool {
	switch target {
	case ErrUnknownEventType:
		return e.Field == "event_type"
	case ErrMissingIdentityField:
		return e.Missing
	case ErrInvalidIdentityField:
		return !e.Missing && e.Field != "event_type"
	default:
		return false
	}
}

// IdentityRequirement says which of the optional identity fields an event type
// requires. A false field is optional, not forbidden: an event may carry an
// attempt_id whenever one exists, and §27.1 only forbids inventing one for a
// Run-less event in the sense that the field must then be null.
type IdentityRequirement struct {
	// Run is true when run_id must be present.
	Run bool
	// Attempt is true when attempt_id must be present.
	Attempt bool
	// AgentRevision is true when agent_revision_id must be present.
	AgentRevision bool
}

// EventIdentityRule is one row of the identity requirement table: the
// requirement plus the reason it was chosen, quoting §27.1/§28.
type EventIdentityRule struct {
	Event       ExecutionEventType
	Requirement IdentityRequirement
	Reason      string
}

// identityRules is the per-event requirement table, in the schema's enum order.
//
// The line is drawn by §27.1's "执行中才有 attempt_id/agent_revision_id": the
// events that can only be emitted while an attempt is alive (the process
// lifecycle, the tool call, the checkpoint) require the attempt and the agent
// revision; events that describe a Run-level state change decided by the server
// or a human name the Run and treat the attempt as optional detail; the
// Run-less families of §27.1 (Gate, manual document, system recovery) require
// neither.
var identityRules = []EventIdentityRule{
	{
		Event:       EventApprovalApproved,
		Requirement: IdentityRequirement{Run: true},
		Reason: "§21.1 waiting_approval→running：审批是 Run 级决定，必须指明 Run；" +
			"attempt 可缺省（审批服务可能在 attempt 生命周期之外醒来）" +
			" (Run-level approval decision; the attempt is optional detail)",
	},
	{
		Event:       EventApprovalDecided,
		Requirement: IdentityRequirement{},
		Reason: "§19.3 人工决定记录；§27.1 Gate/人工文档允许没有 Run，故 run_id 可空" +
			" (Gate / manual-document decision: §27.1 allows no Run)",
	},
	{
		Event:       EventApprovalRequired,
		Requirement: IdentityRequirement{Run: true},
		Reason: "§21.1 running→waiting_approval：approval fingerprint/scope 挂在 Run 上，" +
			"决定必须能指回它的 Run (the pending approval belongs to a Run)",
	},
	{
		Event:       EventBudgetSoftExceeded,
		Requirement: IdentityRequirement{Run: true},
		Reason: "§21.1 running→running：预算属于 Run（Run.Budget），超限必须可归属到 Run；" +
			"attempt 可缺省（用量计数是 Run 级聚合） (Run-level budget signal)",
	},
	{
		Event:       EventBudgetWarning,
		Requirement: IdentityRequirement{Run: true},
		Reason: "§15 T9.01 预算告警：同 budget.soft_exceeded，Run 级告警" +
			" (Run-level advisory, same as soft_exceeded)",
	},
	{
		Event:       EventCheckpointAcknowledged,
		Requirement: IdentityRequirement{Run: true, Attempt: true, AgentRevision: true},
		Reason: "§21.1 running→paused：暂停/恢复必须绑定产生该 checkpoint 的 attempt（S1 一 Run 一 Attempt）" +
			" (the checkpoint belongs to a live attempt)",
	},
	{
		Event:       EventLegacyFlowEvent,
		Requirement: IdentityRequirement{},
		Reason: "§27.1/§19.3 旧 Flow 时间线条目属于项目、不属于 Run（S1 旧 Flow 仍由旧库写，" +
			"经本地 outbox 投影进运行库），故 run_id 可空 (legacy Flow timeline entry: project-scoped, no Run)",
	},
	{
		Event:       EventMergeCompleted,
		Requirement: IdentityRequirement{Run: true},
		Reason: "§21.1 completed 行：MergeOperation 属于该 Run；合入时 attempt 可能已结束，" +
			"故 attempt/agent_revision 可缺省 (the MergeOperation belongs to the Run)",
	},
	{
		Event:       EventProcessExited,
		Requirement: IdentityRequirement{Run: true, Attempt: true, AgentRevision: true},
		Reason: "§21.1 条件“输出持久化且收到可信退出”：exit code 属于某个 attempt 的进程，" +
			"必须能对应到 attempt (the exit belongs to a specific attempt process)",
	},
	{
		Event:       EventProcessStarted,
		Requirement: IdentityRequirement{Run: true, Attempt: true, AgentRevision: true},
		Reason: "§21.1 条件“pid/process_start_id 已记录”：这两个字段记在 attempt 上" +
			" (pid/process_start_id live on the attempt)",
	},
	{
		Event:       EventProcessTerminated,
		Requirement: IdentityRequirement{Run: true, Attempt: true, AgentRevision: true},
		Reason: "§21.1 cancelling→cancelled 要求 grace/force 记录齐全，记录按 attempt 归属" +
			" (grace/force records are attempt-scoped)",
	},
	{
		Event:       EventRunCancelRequested,
		Requirement: IdentityRequirement{Run: true},
		Reason: "CA-1：run.cancel/hard deadline → cancelling 的状态事件；queued 的 Run 还没有 attempt，" +
			"故 attempt 可缺省 (cancel of a queued Run happens before any attempt exists)",
	},
	{
		Event:       EventRunCancelled,
		Requirement: IdentityRequirement{Run: true},
		Reason: "CA-1 终态事件：指明 Run；可能来自 queued 取消（从无 attempt）或进程已终止，attempt 可缺省" +
			" (terminal fact names the Run; there may never have been an attempt)",
	},
	{
		Event:       EventRunCompleted,
		Requirement: IdentityRequirement{Run: true},
		Reason: "§15 T9.01 终态通知：指明 Run；进程此时已退出，attempt 可缺省" +
			" (terminal notification names the Run; the attempt may be gone)",
	},
	{
		Event:       EventRunExpired,
		Requirement: IdentityRequirement{Run: true},
		Reason: "CA-1 终态事件（hard deadline）：与 run.cancelled 相同，attempt 可缺省" +
			" (same as run.cancelled: a queued Run can expire without an attempt)",
	},
	{
		Event:       EventRunFailed,
		Requirement: IdentityRequirement{Run: true},
		Reason: "§15 T9.01 终态通知：指明 Run；进程此时已退出，attempt 可缺省" +
			" (terminal notification names the Run; the attempt may be gone)",
	},
	{
		Event:       EventRunReattached,
		Requirement: IdentityRequirement{Run: true, Attempt: true, AgentRevision: true},
		Reason: "CA-1：recovering → running 只在 verified attach 时发生，验证的对象就是该 attempt 的进程" +
			" (a verified attach is about one specific attempt's process)",
	},
	{
		Event:       EventRunRecovering,
		Requirement: IdentityRequirement{Run: true},
		Reason: "CA-1：server.restart / process_kill_failed → recovering 的状态事件；恢复器是系统，" +
			"attempt 身份此刻可能正无法确认，故可缺省 (the attempt identity may be what recovery must verify)",
	},
	{
		Event:       EventRunResumed,
		Requirement: IdentityRequirement{Run: true, Attempt: true, AgentRevision: true},
		Reason: "CA-1：paused → running 从该 attempt 的 checkpoint 恢复（S1 一 Run 一 Attempt）" +
			" (resume continues the attempt that produced the checkpoint)",
	},
	{
		Event:       EventSchedulerClaimed,
		Requirement: IdentityRequirement{Run: true},
		Reason: "§21.1 queued→starting；§28“queued 可无 attempt”——认领事件创建 attempt，" +
			"故 attempt/agent_revision 可缺省，但必须指明被认领的 Run" +
			" (the claim creates the attempt, so no attempt id is required yet)",
	},
	{
		Event:       EventServerRestart,
		Requirement: IdentityRequirement{},
		Reason: "§27.1 系统恢复允许没有 Run（如重启后清扫孤儿进程）；" +
			"有 Run 的恢复事件仍应带上 run_id，但契约不强制" +
			" (system recovery may act before/without a Run)",
	},
	{
		Event:       EventToolRequested,
		Requirement: IdentityRequirement{Run: true, Attempt: true, AgentRevision: true},
		Reason: "§28“tool 必须有 attempt”；§20.2 实例为 run 作用域的执行活动" +
			" (§28: a tool event must have an attempt)",
	},
}

// IdentityRules returns a copy of the requirement table in schema order, with
// each row's reason, for UI, audit and docs.
func IdentityRules() []EventIdentityRule {
	out := make([]EventIdentityRule, len(identityRules))
	copy(out, identityRules)
	return out
}

// RequiredIdentity returns what the event type requires. An event type outside
// the closed enum is an error (wrapping ErrUnknownEventType), never a silent
// "nothing required".
func RequiredIdentity(eventType string) (IdentityRequirement, error) {
	t := ExecutionEventType(eventType)
	if !t.Valid() {
		return IdentityRequirement{}, &IdentityError{
			EventType: eventType,
			Field:     "event_type",
			Reason:    fmt.Sprintf("%q is not one of the %d execution event types", eventType, len(ExecutionEventTypes)),
		}
	}
	for _, r := range identityRules {
		if r.Event == t {
			return r.Requirement, nil
		}
	}
	// Unreachable while identityRules covers the enum; identity_test.go proves
	// the two sets are equal.
	return IdentityRequirement{}, &IdentityError{
		EventType: eventType,
		Field:     "event_type",
		Reason:    "no identity requirement is declared for this event type",
	}
}

// Validate checks the wire shape of the identity against
// execution-identity.schema.json, without applying per-event requirements:
// project_id present and within limits, actor.type in the enum, actor.id
// present and within limits, actor.source within limits, and every optional id
// either absent or a non-empty string within limits.
//
// Blank-only ids (spaces) are rejected even though the schema only checks
// length: an identity of spaces is never meaningful, and the audit layer
// (I-50/T0.10) rejects it the same way.
func (id ExecutionIdentity) Validate() error {
	if err := checkIdentityID("project_id", id.ProjectID); err != nil {
		return err
	}
	if !id.Actor.Type.Valid() {
		return &IdentityError{
			Field:  "actor.type",
			Reason: fmt.Sprintf("%q is not one of user/agent/system/integration", string(id.Actor.Type)),
		}
	}
	if err := checkIdentityID("actor.id", id.Actor.ID); err != nil {
		return err
	}
	if len(id.Actor.Source) > MaxActorSourceLength {
		return &IdentityError{
			Field:  "actor.source",
			Reason: fmt.Sprintf("length %d > %d", len(id.Actor.Source), MaxActorSourceLength),
		}
	}
	for _, f := range []struct {
		name  string
		value *string
	}{
		{"run_id", id.RunID},
		{"attempt_id", id.AttemptID},
		{"agent_revision_id", id.AgentRevisionID},
	} {
		if f.value == nil {
			continue
		}
		if err := checkIdentityID(f.name, *f.value); err != nil {
			return err
		}
	}
	return nil
}

// ValidateFor checks the wire shape and then the per-event requirements of
// RequiredIdentity. Every rejection names the offending field; a missing
// required field wraps ErrMissingIdentityField, a malformed field wraps
// ErrInvalidIdentityField, and an unknown event type wraps
// ErrUnknownEventType.
func (id ExecutionIdentity) ValidateFor(eventType string) error {
	req, err := RequiredIdentity(eventType)
	if err != nil {
		return err
	}
	if err := id.Validate(); err != nil {
		if ie, ok := err.(*IdentityError); ok && ie.EventType == "" {
			ie.EventType = eventType
		}
		return err
	}
	for _, f := range []struct {
		name     string
		required bool
		value    *string
	}{
		{"run_id", req.Run, id.RunID},
		{"attempt_id", req.Attempt, id.AttemptID},
		{"agent_revision_id", req.AgentRevision, id.AgentRevisionID},
	} {
		if f.required && f.value == nil {
			return &IdentityError{
				EventType: eventType,
				Field:     f.name,
				Missing:   true,
				Reason:    "required by this event type but absent",
			}
		}
	}
	return nil
}

// checkIdentityID applies the shared minLength 1 / maxLength 128 rule of the
// schema to one id field.
func checkIdentityID(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return &IdentityError{Field: field, Reason: "empty (minLength 1)"}
	}
	if len(value) > MaxIdentityIDLength {
		return &IdentityError{
			Field:  field,
			Reason: fmt.Sprintf("length %d > %d", len(value), MaxIdentityIDLength),
		}
	}
	return nil
}
