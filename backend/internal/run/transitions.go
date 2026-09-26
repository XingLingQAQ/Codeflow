package run

import "fmt"

// This file is the pure, table-driven half of the Run state machine frozen by
// plan §21.1 and §27.2.6. It decides *whether* a transition is allowed and
// *what* the next status is; it performs no I/O, holds no lock and never
// touches the database. T1.02.b is the other half: it runs the decision inside
// the CAS + event transaction, and the loser of a concurrent CAS race re-reads
// the current row instead of asking for a second transition (§27.2.6).
//
// Vocabulary note (§21.1, §27.2.6): the plan's "命令/事件" column mixes three
// kinds of input, and TriggerKind keeps them apart:
//
//   - TriggerEvent: a type from the closed execution-event enum
//     (scheduler.claimed, process.started, approval.required, approval.approved,
//     budget.soft_exceeded, checkpoint.acknowledged, process.exited,
//     process.terminated, server.restart).
//   - TriggerCommand: run.cancel, run.resume, hard_deadline, merge. These are
//     commands, not events, so they are absent from the event enum.
//   - TriggerConclusion: the recoverer's own findings — process_verified,
//     cleaned, process_kill_failed. They are conclusions, not events, and
//     process_kill_failed is also the failure code of the cancel path.
//
// Consequence events (the run.* state events — run.cancel_requested,
// run.cancelled, run.completed, run.expired, run.failed, run.reattached,
// run.recovering, run.resumed — and approval.decided, budget.warning,
// merge.completed, tool.requested) are never triggers: they are emitted
// *because* of a transition, and asking NextStatus for one is a programming
// error reported as invalid_transition. Decide names the state event each
// allowed transition writes (contract amendment CA-1, plan §26.31).

// TriggerKind classifies the source of a transition request. The zero value is
// deliberately invalid: a caller must say which of the three vocabularies it is
// speaking, so a command can never be mistaken for an event.
type TriggerKind string

const (
	// TriggerEvent is a type from the closed execution-event enum.
	TriggerEvent TriggerKind = "event"
	// TriggerCommand is one of the §21.1 commands: run.cancel, run.resume,
	// hard_deadline, merge.
	TriggerCommand TriggerKind = "command"
	// TriggerConclusion is a recoverer conclusion: process_verified, cleaned,
	// process_kill_failed.
	TriggerConclusion TriggerKind = "conclusion"
)

// TriggerKinds lists every valid TriggerKind.
var TriggerKinds = []TriggerKind{TriggerEvent, TriggerCommand, TriggerConclusion}

// Valid reports whether k is one of the three trigger vocabularies.
func (k TriggerKind) Valid() bool {
	switch k {
	case TriggerEvent, TriggerCommand, TriggerConclusion:
		return true
	default:
		return false
	}
}

// Command names from §21.1. run.cancel/run.resume/hard deadline/merge are
// commands, not event types, so they do not appear in the event enum.
const (
	// CommandRunCancel is "run.cancel": actor-initiated cancellation.
	CommandRunCancel = "run.cancel"
	// CommandRunResume is "run.resume": resume a paused Run after re-validating
	// the checkpoint, the revision and the policy.
	CommandRunResume = "run.resume"
	// CommandHardDeadline is the wall-clock deadline of the Run firing. §21.1
	// names it "hard deadline" and does not freeze a token; the token chosen
	// here is the snake_case "hard_deadline".
	CommandHardDeadline = "hard_deadline"
	// CommandMerge is the merge command of a completed Run. It creates a
	// MergeOperation and never changes the Run status.
	CommandMerge = "merge"
)

// Commands lists every §21.1 command name.
var Commands = []string{CommandRunCancel, CommandRunResume, CommandHardDeadline, CommandMerge}

// Recoverer conclusion names from §21.1. They are not events either: they are
// the outcome the recovery path reached about an existing process.
const (
	// ConclusionProcessVerified is a verified attach: the process is still the
	// attempt's own process, so the Run goes back to running.
	ConclusionProcessVerified = "process_verified"
	// ConclusionProcessKillFailed is "we tried to terminate the process tree and
	// could not confirm it is gone": the Run moves to recovering while it still
	// owns process/workspace quota.
	ConclusionProcessKillFailed = "process_kill_failed"
	// ConclusionCleaned is "the process tree/workspace is confirmed empty and the
	// bookkeeping is done", with ExpectedTerminal naming the terminal state the
	// cleanup ends in.
	ConclusionCleaned = "cleaned"
)

// Conclusions lists every recoverer conclusion name.
var Conclusions = []string{ConclusionProcessVerified, ConclusionProcessKillFailed, ConclusionCleaned}

// Failure codes of §21.1. They are the codes the API layer reports when a
// transition cannot be performed, and (for merge) when the MergeOperation
// itself fails. CodeInvalidTransition is the fallback for a request that is not
// in the table at all.
const (
	// CodeInvalidTransition is the fallback for a (from, trigger) pair the table
	// does not contain, or for a trigger the table gives no unambiguous code.
	CodeInvalidTransition = "invalid_transition"
	// CodeBackendUnavailable: no usable backend capability for the claim.
	CodeBackendUnavailable = "backend_unavailable"
	// CodeProcessStartFailed: the process could not be started/identified.
	CodeProcessStartFailed = "process_start_failed"
	// CodeApprovalPersistFailed: the approval request/fingerprint was not stored.
	CodeApprovalPersistFailed = "approval_persist_failed"
	// CodeApprovalInvalid: fingerprint/scope/expiry of the approval did not pass.
	CodeApprovalInvalid = "approval_invalid"
	// CodeCapabilityUnavailable: the backend cannot produce a safe checkpoint.
	CodeCapabilityUnavailable = "capability_unavailable"
	// CodeConflict: the resume preconditions (checkpoint, revision, policy) failed.
	CodeConflict = "conflict"
	// CodeOutputPersistFailed: the exit was credible but its output was not stored.
	CodeOutputPersistFailed = "output_persist_failed"
	// CodeForbidden: the actor is not allowed to cancel/expire this Run.
	CodeForbidden = "forbidden"
	// CodeProcessKillFailed: grace/force termination could not be confirmed.
	CodeProcessKillFailed = "process_kill_failed"
	// CodeRecoveryRequired: the owning instance/PID/backend cannot be trusted
	// after a restart, so the Run must be recovered first.
	CodeRecoveryRequired = "recovery_required"
	// CodeBaseChanged: the merge base moved under the Run.
	CodeBaseChanged = "base_changed"
	// CodeMergeConflict: the merge produced conflicts.
	CodeMergeConflict = "merge_conflict"
	// CodeGuardBlocked: a merge guard (policy/review/scope) blocked the merge.
	CodeGuardBlocked = "guard_blocked"
)

// FailureCodes lists every failure code §21.1 names, plus the fallback.
var FailureCodes = []string{
	CodeInvalidTransition, CodeBackendUnavailable, CodeProcessStartFailed,
	CodeApprovalPersistFailed, CodeApprovalInvalid, CodeCapabilityUnavailable,
	CodeConflict, CodeOutputPersistFailed, CodeForbidden, CodeProcessKillFailed,
	CodeRecoveryRequired, CodeBaseChanged, CodeMergeConflict, CodeGuardBlocked,
}

// Trigger is one transition request.
//
// Fields not consulted by the matched rule must be left at their zero value:
// ExitCode only belongs to process.exited, ExpectedTerminal only to the
// "cleaned" conclusion and the process.terminated event. A caller that sets an
// irrelevant field gets invalid_transition rather than a silent match, so a
// caller bug cannot hide behind a lenient matcher.
type Trigger struct {
	// Kind is the vocabulary the Name comes from.
	Kind TriggerKind
	// Name is an event type, a command name or a conclusion name.
	Name string
	// ExitCode is the process exit code; set only for process.exited, and nil
	// means "unknown", which matches no rule.
	ExitCode *int
	// ExpectedTerminal is the terminal state the cancel path ends in. The
	// "cleaned" conclusion carries failed, cancelled or expired; the
	// process.terminated event carries the expected terminal the cancel or hard
	// deadline recorded (cancelled or expired, §27.2.6 — CA-1: a hard deadline
	// ends expired even when a live process had to be terminated).
	ExpectedTerminal RunStatus
}

// EventTrigger builds a trigger for an execution event type.
func EventTrigger(eventType string) Trigger {
	return Trigger{Kind: TriggerEvent, Name: eventType}
}

// ProcessExitedTrigger builds the process.exited trigger for a known exit code.
func ProcessExitedTrigger(exitCode int) Trigger {
	code := exitCode
	return Trigger{Kind: TriggerEvent, Name: string(EventProcessExited), ExitCode: &code}
}

// ProcessTerminatedTrigger builds the process.terminated trigger of the cancel
// path. expected is the terminal state recorded when the Run entered
// cancelling: cancelled for run.cancel, expired for the hard deadline.
func ProcessTerminatedTrigger(expected RunStatus) Trigger {
	return Trigger{Kind: TriggerEvent, Name: string(EventProcessTerminated), ExpectedTerminal: expected}
}

// CommandTrigger builds a trigger for a §21.1 command.
func CommandTrigger(name string) Trigger {
	return Trigger{Kind: TriggerCommand, Name: name}
}

// ConclusionTrigger builds a trigger for a recoverer conclusion. expected is
// only consulted for the "cleaned" conclusion and must be zero otherwise.
func ConclusionTrigger(conclusion string, expected RunStatus) Trigger {
	return Trigger{Kind: TriggerConclusion, Name: conclusion, ExpectedTerminal: expected}
}

// String renders the trigger for logs and test failures.
func (t Trigger) String() string {
	switch {
	case t.ExitCode != nil:
		return fmt.Sprintf("%s:%s(exit=%d)", t.Kind, t.Name, *t.ExitCode)
	case t.ExpectedTerminal != "":
		return fmt.Sprintf("%s:%s(%s)", t.Kind, t.Name, t.ExpectedTerminal)
	default:
		return fmt.Sprintf("%s:%s", t.Kind, t.Name)
	}
}

// exitMatch says which exit codes a rule accepts.
type exitMatch uint8

const (
	// exitNone means "no exit code may be set" (every non-process.exited rule).
	exitNone exitMatch = iota
	// exitZero matches process.exited with exit code 0.
	exitZero
	// exitNonZero matches process.exited with any non-zero exit code.
	exitNonZero
)

// transitionRule is one row of the §21.1 table. The table is data so that tests
// can walk it row by row and so that AllowedTriggers/Rules can be derived from
// it instead of restating it.
type transitionRule struct {
	From RunStatus
	Kind TriggerKind
	Name string
	Exit exitMatch
	// Terminal is the ExpectedTerminal a trigger must carry to match; "" means
	// the caller must not set one.
	Terminal    RunStatus
	To          RunStatus
	Condition   string
	FailureCode string
	// StateEvent is the event type written in the same transaction as the
	// status change (§19.3 item 1, CA-1). It is "" only for completed + merge:
	// the merge command changes no Run status, and the MergeOperation writes its
	// own merge.completed when it reaches applied (§19.3 item 4).
	StateEvent ExecutionEventType
}

// Condition texts shared by several rows.
const (
	cancelCondition = "actor 有权限；记录期望终态 cancelled；queued 无进程可直接结束 " +
		"(actor authorised; record the expected terminal cancelled; a queued Run has no process to kill)"
	deadlineCondition = "记录期望终态 expired；queued 无进程可直接结束 " +
		"(record the expected terminal expired; a queued Run has no process to kill)"
	restartCondition = "验证 owner instance、PID 启动时间和后端重连能力 " +
		"(verify owner instance, PID start time and backend re-attach capability)"
	terminatedCondition = "grace/force 记录齐全；按进入 cancelling 时记录的期望终态结束 " +
		"(grace/force records complete; end in the expected terminal recorded on entering cancelling)"
	cleanedCondition       = "仅 verified attach 才 running，否则明确清理/终止 (explicit cleanup/termination otherwise)"
	queuedCleanedCondition = "queued 无进程可直接结束：进程树确认为空后按期望终态结束 " +
		"(queued has no process: finish at the expected terminal once the tree is confirmed empty)"
	exitWhileApprovalCondition = "等待审批期间进程退出：pending approvals 作废（§27.2.5），工具调用未获批准就结束，不能按完成处理 " +
		"(the process exited while an approval was pending: pending approvals are invalidated and the Run cannot count as completed)"
)

// transitionRules is the §21.1 table, row by row, as amended by contract
// amendment CA-1 (plan §26.31): process.terminated carries the expected
// terminal of the cancel path, a process that exits while its Run waits for
// approval fails the Run, and every row names the state event it writes.
// Condition and FailureCode are the plan's own columns; FailureCode "" means
// the plan writes "—" (no failure code for that row).
var transitionRules = []transitionRule{
	// queued --scheduler.claimed--> starting
	{
		From: RunStatusQueued, Kind: TriggerEvent, Name: string(EventSchedulerClaimed),
		To:          RunStatusStarting,
		Condition:   "lease 有效、backend capability 存在 (valid lease, backend capability present)",
		FailureCode: CodeBackendUnavailable,
		StateEvent:  EventSchedulerClaimed,
	},
	// starting --process.started--> running
	{
		From: RunStatusStarting, Kind: TriggerEvent, Name: string(EventProcessStarted),
		To:          RunStatusRunning,
		Condition:   "pid/process_start_id 已记录 (pid and process_start_id recorded)",
		FailureCode: CodeProcessStartFailed,
		StateEvent:  EventProcessStarted,
	},
	// running --approval.required--> waiting_approval
	{
		From: RunStatusRunning, Kind: TriggerEvent, Name: string(EventApprovalRequired),
		To:          RunStatusWaitingApproval,
		Condition:   "approval pending 且 fingerprint 已持久化 (approval pending, fingerprint persisted)",
		FailureCode: CodeApprovalPersistFailed,
		StateEvent:  EventApprovalRequired,
	},
	// waiting_approval --approval.approved--> running
	{
		From: RunStatusWaitingApproval, Kind: TriggerEvent, Name: string(EventApprovalApproved),
		To:          RunStatusRunning,
		Condition:   "fingerprint/scope/expiry 通过 (fingerprint, scope and expiry pass)",
		FailureCode: CodeApprovalInvalid,
		StateEvent:  EventApprovalApproved,
	},
	// running --budget.soft_exceeded--> running (usage warning only; never a
	// faked pause). The warning is what gets recorded.
	{
		From: RunStatusRunning, Kind: TriggerEvent, Name: string(EventBudgetSoftExceeded),
		To:         RunStatusRunning,
		Condition:  "仅记录 usage.warning；不伪造暂停 (record usage.warning only; do not fake a pause)",
		StateEvent: EventBudgetWarning,
	},
	// running --checkpoint.acknowledged--> paused
	{
		From: RunStatusRunning, Kind: TriggerEvent, Name: string(EventCheckpointAcknowledged),
		To:          RunStatusPaused,
		Condition:   "仅后端明确支持安全检查点时可用 (only when the backend explicitly supports safe checkpoints)",
		FailureCode: CodeCapabilityUnavailable,
		StateEvent:  EventCheckpointAcknowledged,
	},
	// paused --run.resume--> running
	{
		From: RunStatusPaused, Kind: TriggerCommand, Name: CommandRunResume,
		To:          RunStatusRunning,
		Condition:   "checkpoint 和 revision 均有效，仍重验策略 (valid checkpoint and revision, policy re-checked)",
		FailureCode: CodeConflict,
		StateEvent:  EventRunResumed,
	},
	// running --process.exited(0)--> completed
	{
		From: RunStatusRunning, Kind: TriggerEvent, Name: string(EventProcessExited), Exit: exitZero,
		To: RunStatusCompleted,
		Condition: "输出持久化且收到可信退出；Task 转 waiting_review，检查/合入另处理 " +
			"(output persisted and exit credible; Task -> waiting_review, review/merge handled elsewhere)",
		FailureCode: CodeOutputPersistFailed,
		StateEvent:  EventRunCompleted,
	},
	// running --process.exited(!=0)--> failed
	{
		From: RunStatusRunning, Kind: TriggerEvent, Name: string(EventProcessExited), Exit: exitNonZero,
		To:         RunStatusFailed,
		Condition:  "记录 exit_reason/retryable (record exit_reason and retryable)",
		StateEvent: EventRunFailed,
	},
	// waiting_approval --process.exited(any)--> failed (CA-1)
	{
		From: RunStatusWaitingApproval, Kind: TriggerEvent, Name: string(EventProcessExited), Exit: exitZero,
		To:         RunStatusFailed,
		Condition:  exitWhileApprovalCondition,
		StateEvent: EventRunFailed,
	},
	{
		From: RunStatusWaitingApproval, Kind: TriggerEvent, Name: string(EventProcessExited), Exit: exitNonZero,
		To:         RunStatusFailed,
		Condition:  exitWhileApprovalCondition,
		StateEvent: EventRunFailed,
	},
	// queued/starting/running/waiting_approval/paused --run.cancel or hard
	// deadline--> cancelling. queued has no process; see the cleaned rows below
	// for how such a Run finishes.
	{From: RunStatusQueued, Kind: TriggerCommand, Name: CommandRunCancel, To: RunStatusCancelling, Condition: cancelCondition, FailureCode: CodeForbidden, StateEvent: EventRunCancelRequested},
	{From: RunStatusStarting, Kind: TriggerCommand, Name: CommandRunCancel, To: RunStatusCancelling, Condition: cancelCondition, FailureCode: CodeForbidden, StateEvent: EventRunCancelRequested},
	{From: RunStatusRunning, Kind: TriggerCommand, Name: CommandRunCancel, To: RunStatusCancelling, Condition: cancelCondition, FailureCode: CodeForbidden, StateEvent: EventRunCancelRequested},
	{From: RunStatusWaitingApproval, Kind: TriggerCommand, Name: CommandRunCancel, To: RunStatusCancelling, Condition: cancelCondition, FailureCode: CodeForbidden, StateEvent: EventRunCancelRequested},
	{From: RunStatusPaused, Kind: TriggerCommand, Name: CommandRunCancel, To: RunStatusCancelling, Condition: cancelCondition, FailureCode: CodeForbidden, StateEvent: EventRunCancelRequested},
	{From: RunStatusQueued, Kind: TriggerCommand, Name: CommandHardDeadline, To: RunStatusCancelling, Condition: deadlineCondition, FailureCode: CodeForbidden, StateEvent: EventRunCancelRequested},
	{From: RunStatusStarting, Kind: TriggerCommand, Name: CommandHardDeadline, To: RunStatusCancelling, Condition: deadlineCondition, FailureCode: CodeForbidden, StateEvent: EventRunCancelRequested},
	{From: RunStatusRunning, Kind: TriggerCommand, Name: CommandHardDeadline, To: RunStatusCancelling, Condition: deadlineCondition, FailureCode: CodeForbidden, StateEvent: EventRunCancelRequested},
	{From: RunStatusWaitingApproval, Kind: TriggerCommand, Name: CommandHardDeadline, To: RunStatusCancelling, Condition: deadlineCondition, FailureCode: CodeForbidden, StateEvent: EventRunCancelRequested},
	{From: RunStatusPaused, Kind: TriggerCommand, Name: CommandHardDeadline, To: RunStatusCancelling, Condition: deadlineCondition, FailureCode: CodeForbidden, StateEvent: EventRunCancelRequested},
	// cancelling --process.terminated(expected)--> cancelled/expired. The
	// expected terminal is the one recorded on entering cancelling (CA-1): a
	// hard deadline ends expired even when a live process had to be killed.
	{
		From: RunStatusCancelling, Kind: TriggerEvent, Name: string(EventProcessTerminated), Terminal: RunStatusCancelled,
		To: RunStatusCancelled, Condition: terminatedCondition, FailureCode: CodeProcessKillFailed, StateEvent: EventRunCancelled,
	},
	{
		From: RunStatusCancelling, Kind: TriggerEvent, Name: string(EventProcessTerminated), Terminal: RunStatusExpired,
		To: RunStatusExpired, Condition: terminatedCondition, FailureCode: CodeProcessKillFailed, StateEvent: EventRunExpired,
	},
	// cancelling --process_kill_failed--> recovering
	{
		From: RunStatusCancelling, Kind: TriggerConclusion, Name: ConclusionProcessKillFailed,
		To:          RunStatusRecovering,
		Condition:   "仍占有进程/工作区配额，禁止并发重派 (still owns process/workspace quota; no concurrent re-dispatch)",
		FailureCode: CodeProcessKillFailed,
		StateEvent:  EventRunRecovering,
	},
	// starting/running/waiting_approval/paused/cancelling --server.restart-->
	// recovering. server.restart itself may be a Run-less system event; the
	// per-Run record of the transition is run.recovering.
	{From: RunStatusStarting, Kind: TriggerEvent, Name: string(EventServerRestart), To: RunStatusRecovering, Condition: restartCondition, FailureCode: CodeRecoveryRequired, StateEvent: EventRunRecovering},
	{From: RunStatusRunning, Kind: TriggerEvent, Name: string(EventServerRestart), To: RunStatusRecovering, Condition: restartCondition, FailureCode: CodeRecoveryRequired, StateEvent: EventRunRecovering},
	{From: RunStatusWaitingApproval, Kind: TriggerEvent, Name: string(EventServerRestart), To: RunStatusRecovering, Condition: restartCondition, FailureCode: CodeRecoveryRequired, StateEvent: EventRunRecovering},
	{From: RunStatusPaused, Kind: TriggerEvent, Name: string(EventServerRestart), To: RunStatusRecovering, Condition: restartCondition, FailureCode: CodeRecoveryRequired, StateEvent: EventRunRecovering},
	{From: RunStatusCancelling, Kind: TriggerEvent, Name: string(EventServerRestart), To: RunStatusRecovering, Condition: restartCondition, FailureCode: CodeRecoveryRequired, StateEvent: EventRunRecovering},
	// recovering --process_verified--> running
	{
		From: RunStatusRecovering, Kind: TriggerConclusion, Name: ConclusionProcessVerified,
		To:         RunStatusRunning,
		Condition:  "仅 verified attach 才 running (only a verified attach returns to running)",
		StateEvent: EventRunReattached,
	},
	// recovering --cleaned--> failed/cancelled/expired
	{From: RunStatusRecovering, Kind: TriggerConclusion, Name: ConclusionCleaned, Terminal: RunStatusFailed, To: RunStatusFailed, Condition: cleanedCondition, StateEvent: EventRunFailed},
	{From: RunStatusRecovering, Kind: TriggerConclusion, Name: ConclusionCleaned, Terminal: RunStatusCancelled, To: RunStatusCancelled, Condition: cleanedCondition, StateEvent: EventRunCancelled},
	{From: RunStatusRecovering, Kind: TriggerConclusion, Name: ConclusionCleaned, Terminal: RunStatusExpired, To: RunStatusExpired, Condition: cleanedCondition, StateEvent: EventRunExpired},
	// cancelling --cleaned--> cancelled/expired. This is the queued-no-process
	// path: a Run that never started can never receive process.terminated, so
	// the canceller reports the same "cleaned" conclusion the recovery path
	// uses, with the expected terminal from the cancel/deadline record
	// (§27.2.6: cancel and hard deadline go to cancelling first, and only end
	// cancelled/expired once the process tree is confirmed empty).
	{From: RunStatusCancelling, Kind: TriggerConclusion, Name: ConclusionCleaned, Terminal: RunStatusCancelled, To: RunStatusCancelled, Condition: queuedCleanedCondition, StateEvent: EventRunCancelled},
	{From: RunStatusCancelling, Kind: TriggerConclusion, Name: ConclusionCleaned, Terminal: RunStatusExpired, To: RunStatusExpired, Condition: queuedCleanedCondition, StateEvent: EventRunExpired},
	// completed --merge--> completed: the merge only creates a MergeOperation
	// (prepared -> applying -> applied); the Run status does not change and the
	// merge's own failures are base_changed/merge_conflict/guard_blocked, which
	// are not Run transitions. No Run state event: the MergeOperation records
	// merge.completed itself when it reaches applied.
	{
		From: RunStatusCompleted, Kind: TriggerCommand, Name: CommandMerge,
		To: RunStatusCompleted,
		Condition: "只创建 MergeOperation；prepared→applying→applied，失败为 base_changed/merge_conflict/guard_blocked " +
			"(only creates a MergeOperation; failures belong to the MergeOperation, not to the Run status)",
	},
}

// triggerKey identifies a trigger operation in the failure-code map.
type triggerKey struct {
	kind TriggerKind
	name string
}

// triggerFailureCodes maps a trigger operation to the single failure code
// §21.1 attaches to it, so that a caller whose *condition* failed (rather than
// the table row) still reports the plan's code. Operations the plan gives no
// code for, or several codes for (merge: base_changed/merge_conflict/
// guard_blocked), are absent and fall back to invalid_transition.
var triggerFailureCodes = map[triggerKey]string{
	{TriggerEvent, string(EventSchedulerClaimed)}:       CodeBackendUnavailable,
	{TriggerEvent, string(EventProcessStarted)}:         CodeProcessStartFailed,
	{TriggerEvent, string(EventApprovalRequired)}:       CodeApprovalPersistFailed,
	{TriggerEvent, string(EventApprovalApproved)}:       CodeApprovalInvalid,
	{TriggerEvent, string(EventCheckpointAcknowledged)}: CodeCapabilityUnavailable,
	{TriggerEvent, string(EventProcessExited)}:          CodeOutputPersistFailed,
	{TriggerEvent, string(EventProcessTerminated)}:      CodeProcessKillFailed,
	{TriggerEvent, string(EventServerRestart)}:          CodeRecoveryRequired,
	{TriggerCommand, CommandRunCancel}:                  CodeForbidden,
	{TriggerCommand, CommandHardDeadline}:               CodeForbidden,
	{TriggerCommand, CommandRunResume}:                  CodeConflict,
	{TriggerConclusion, ConclusionProcessKillFailed}:    CodeProcessKillFailed,
}

// FailureCodeFor returns the failure code §21.1 attaches to this trigger
// operation, or CodeInvalidTransition when the table gives none (or several, as
// for merge, and for a process.exited whose exit code the request does not pin
// down). A caller whose precondition failed uses this to report the plan's code
// (for example a claim with an invalid lease reports backend_unavailable).
func FailureCodeFor(t Trigger) string {
	if !t.Kind.Valid() || t.Name == "" {
		return CodeInvalidTransition
	}
	isExited := t.Kind == TriggerEvent && t.Name == string(EventProcessExited)
	isTerminated := t.Kind == TriggerEvent && t.Name == string(EventProcessTerminated)
	isCleaned := t.Kind == TriggerConclusion && t.Name == ConclusionCleaned
	// A field that does not belong to this trigger makes the request malformed,
	// so it reports no code rather than the code of an operation it is not.
	if t.ExitCode != nil && !isExited {
		return CodeInvalidTransition
	}
	if t.ExpectedTerminal != "" && !isCleaned && !isTerminated {
		return CodeInvalidTransition
	}
	// process.terminated ends the cancel path, so it must name the terminal that
	// path recorded (CA-1); without one — or with failed, which the cancel path
	// never records — the request cannot be classified.
	if isTerminated && t.ExpectedTerminal != RunStatusCancelled && t.ExpectedTerminal != RunStatusExpired {
		return CodeInvalidTransition
	}
	if isExited {
		if t.ExitCode == nil || *t.ExitCode != 0 {
			// exit(0) maps to output_persist_failed; a non-zero exit has "—" and
			// an unknown exit code cannot be classified at all.
			return CodeInvalidTransition
		}
		return CodeOutputPersistFailed
	}
	code, ok := triggerFailureCodes[triggerKey{t.Kind, t.Name}]
	if !ok || code == "" {
		return CodeInvalidTransition
	}
	return code
}

// matches reports whether rule r accepts the trigger t coming from from.
func (r transitionRule) matches(from RunStatus, t Trigger) bool {
	if r.From != from || r.Kind != t.Kind || r.Name != t.Name {
		return false
	}
	switch r.Exit {
	case exitZero:
		if t.ExitCode == nil || *t.ExitCode != 0 {
			return false
		}
	case exitNonZero:
		if t.ExitCode == nil || *t.ExitCode == 0 {
			return false
		}
	default:
		if t.ExitCode != nil {
			return false
		}
	}
	return r.Terminal == t.ExpectedTerminal
}

// representativeTrigger turns a rule into the trigger a caller would send, for
// Rules and AllowedTriggers. The exit code is materialised (0 for the exit-zero
// rule, 1 for the non-zero rule) and the cleaned and process.terminated rows
// carry their terminal, so the returned trigger is directly usable.
func (r transitionRule) representativeTrigger() Trigger {
	t := Trigger{Kind: r.Kind, Name: r.Name, ExpectedTerminal: r.Terminal}
	switch r.Exit {
	case exitZero:
		code := 0
		t.ExitCode = &code
	case exitNonZero:
		code := 1
		t.ExitCode = &code
	}
	return t
}

// TransitionRule is a read-only view of one §21.1 row, for UI, audit and docs.
type TransitionRule struct {
	From        RunStatus
	Trigger     Trigger
	To          RunStatus
	Condition   string
	FailureCode string
	// StateEvent is the event type the transition writes (see Transition).
	StateEvent ExecutionEventType
}

// Rules returns a copy of the §21.1 table in declaration order. The returned
// slice is freshly allocated, so callers cannot mutate the package's table.
func Rules() []TransitionRule {
	out := make([]TransitionRule, 0, len(transitionRules))
	for _, r := range transitionRules {
		out = append(out, TransitionRule{
			From:        r.From,
			Trigger:     r.representativeTrigger(),
			To:          r.To,
			Condition:   r.Condition,
			FailureCode: r.FailureCode,
			StateEvent:  r.StateEvent,
		})
	}
	return out
}

// AllowedTriggers returns every trigger the table accepts from from, in
// declaration order. Terminal states return nil except completed, whose merge
// command is allowed but keeps the status at completed. Each call allocates
// fresh ExitCode pointers, so callers may keep or modify the result.
func AllowedTriggers(from RunStatus) []Trigger {
	var out []Trigger
	for _, r := range transitionRules {
		if r.From == from {
			out = append(out, r.representativeTrigger())
		}
	}
	return out
}

// AllTriggers returns the canonical trigger vocabulary: one entry per distinct
// (kind, name, exit class, expected terminal) the table speaks about. Tests
// walk every state against this list to prove the cross-product is closed; the
// list is a function because Trigger carries pointers and must not be shared.
func AllTriggers() []Trigger {
	return []Trigger{
		EventTrigger(string(EventSchedulerClaimed)),
		EventTrigger(string(EventProcessStarted)),
		ProcessExitedTrigger(0),
		ProcessExitedTrigger(1),
		EventTrigger(string(EventApprovalRequired)),
		EventTrigger(string(EventApprovalApproved)),
		EventTrigger(string(EventBudgetSoftExceeded)),
		EventTrigger(string(EventCheckpointAcknowledged)),
		ProcessTerminatedTrigger(RunStatusCancelled),
		ProcessTerminatedTrigger(RunStatusExpired),
		EventTrigger(string(EventServerRestart)),
		CommandTrigger(CommandRunResume),
		CommandTrigger(CommandRunCancel),
		CommandTrigger(CommandHardDeadline),
		CommandTrigger(CommandMerge),
		ConclusionTrigger(ConclusionProcessVerified, ""),
		ConclusionTrigger(ConclusionProcessKillFailed, ""),
		ConclusionTrigger(ConclusionCleaned, RunStatusCancelled),
		ConclusionTrigger(ConclusionCleaned, RunStatusExpired),
		ConclusionTrigger(ConclusionCleaned, RunStatusFailed),
	}
}

// TransitionError is returned for every request the table does not accept. The
// Run keeps its current status, so callers must not apply the returned status.
type TransitionError struct {
	From    RunStatus
	Trigger Trigger
	// Code is the §21.1 failure code of the operation, or
	// invalid_transition when the table does not give one.
	Code string
}

// Error implements error.
func (e *TransitionError) Error() string {
	return fmt.Sprintf("run: transition %s --%s--> not allowed: %s", e.From, e.Trigger, e.Code)
}

// Transition is the outcome of an allowed transition request.
type Transition struct {
	From RunStatus
	To   RunStatus
	// StateEvent is the event type written in the same transaction as the
	// status change (§19.3 item 1). It is "" only for completed + merge, which
	// changes no Run status (the MergeOperation writes merge.completed itself).
	// When the trigger is itself an event that records the change (claim,
	// process start, approval, checkpoint) StateEvent is that event type; a
	// terminal status always has its own terminal event type (run.completed,
	// run.failed, run.cancelled, run.expired), and the trigger event that caused
	// it (process.exited, process.terminated) is recorded separately.
	StateEvent ExecutionEventType
}

// Decide is NextStatus plus the state event of the matched row; it is what the
// CAS + event transaction of T1.02.b calls. On a *TransitionError the returned
// Transition has To == From and no state event.
func Decide(from RunStatus, t Trigger) (Transition, error) {
	if !from.Valid() {
		return Transition{From: from, To: from}, &TransitionError{From: from, Trigger: t, Code: CodeInvalidTransition}
	}
	for _, r := range transitionRules {
		if r.matches(from, t) {
			return Transition{From: from, To: r.To, StateEvent: r.StateEvent}, nil
		}
	}
	return Transition{From: from, To: from}, &TransitionError{From: from, Trigger: t, Code: FailureCodeFor(t)}
}

// NextStatus decides the status a Run moves to when trigger t arrives while it
// is in from.
//
// A nil error means the transition is allowed and the returned status is the
// target (which may equal from for the budget.soft_exceeded warning and for the
// merge command). A *TransitionError means the request is not in the table: the
// returned status is from, unchanged. Terminal states have no outgoing row
// except completed + merge, which stays completed.
//
// NextStatus checks legality only. The conditions in the table's fourth column
// (a valid lease, a persisted approval fingerprint, a supported checkpoint, an
// authorised actor, persisted output, complete grace/force records, a verified
// owner instance) are decided by the caller from the persisted state; when such
// a condition fails, the caller reports FailureCodeFor(t) — and, for a lost CAS
// race, re-reads the current status instead of transitioning again (§27.2.6).
func NextStatus(from RunStatus, t Trigger) (RunStatus, error) {
	tr, err := Decide(from, t)
	return tr.To, err
}

// StartFailure describes a failed attempt start, as far as the dispatcher can
// observe it. Both fields default to false, which is the conservative "nothing
// happened yet" state.
type StartFailure struct {
	// ProviderAccepted is true once the backend/provider acknowledged the start
	// request: the process was spawned, or the remote API returned a success.
	ProviderAccepted bool
	// SideEffectsStarted is true once the attempt did anything durable: wrote to
	// the workspace, emitted process.started/tool.requested, consumed budget or
	// created a checkpoint.
	SideEffectsStarted bool
}

// RetryableStartFailure reports whether an automatic retry may be scheduled for
// this failed start (§15, §27.2): only a start that the provider never accepted
// and that began no side effect may be retried automatically. Anything else —
// an accepted start, a Run whose process already executed, or a Run recovered
// after an interruption — is never blindly re-dispatched: a new execution
// requires the explicit user retry that creates a new Run and a new Attempt
// (§15: S1 runs one Attempt per Run; a retry is a new Run).
//
// The caller classifies the failure as a start-phase failure; this predicate
// only decides whether an automatic retry is permitted.
func RetryableStartFailure(f StartFailure) bool {
	return !f.ProviderAccepted && !f.SideEffectsStarted
}
