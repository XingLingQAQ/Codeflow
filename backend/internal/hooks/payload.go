// Package hooks - Payload contracts for the tool and Run lifecycle hooks
// (T1.07.a).
//
// 本文件冻结保留 hook 类型的 payload 契约（计划 §15 T1.07、§28 T1.07.a）：
//
//   - ToolHookPayload 服务于 HookPreToolUse / HookPostToolUse；
//   - RunHookPayload 服务于 HookRunStart / HookRunFinish。
//
// 两个契约的共同规则：
//
//   - Identity 由服务端从权威资源填写（Run/Attempt/AgentRevision 的实际记录），
//     绝不取自后端自报的字段。T1.07.b 在 execbackend 端口里拿到的是后端原始
//     观察，identity 必须由服务端补。
//   - EventID 是服务端分配的事件 ID（不是后端序号，不是 ProviderRef）：它就是
//     这个触发点观察的那一条执行事件的 ID，也是去重键的一半。
//   - 错误文本只写字段名和原因，绝不回显 Arguments / Result 的内容（脱敏后的
//     内容也不行：它会进日志与审计）。
//
// 本文件不 import execbackend：execbackend 只允许 import 标准库，hooks 反向
// import 它会形成环。ToolCallID 的 256 字节上限在这里以 MaxToolCallIDBytes
// 复刻，并由 payload_test.go 钉住。
package hooks

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/codeflow/backend/internal/run"
)

// MaxToolCallIDBytes 是 ToolHookPayload.ToolCallID 的最大字节数。
//
// 它等于 execbackend.MaxProviderRefBytes（工具调用 ID 是后端的 ProviderRef，
// 供服务端去重）。本包不 import execbackend（见文件头），所以复制常量；
// payload_test.go 断言这个数字是 256，T1.07.b 接线时若上游改了上限，这里会
// 先失败。
const MaxToolCallIDBytes = 256

// payloadIDMaxBytes 是服务端分配的 ID / 指纹字段的上限，取 run 包的线上 ID
// 上限（run.MaxIdentityIDLength，§27.1 execution-identity schema）。
const payloadIDMaxBytes = run.MaxIdentityIDLength

// ToolHookResultStatus 是工具结果的封闭状态枚举。
type ToolHookResultStatus string

const (
	// ToolHookResultOK：工具成功返回。
	ToolHookResultOK ToolHookResultStatus = "ok"
	// ToolHookResultError：工具报错或非零退出。
	ToolHookResultError ToolHookResultStatus = "error"
	// ToolHookResultDenied：工具没有执行（被拒绝或审批未通过）。
	ToolHookResultDenied ToolHookResultStatus = "denied"
)

// ToolHookResultStatuses 按声明顺序列出全部结果状态。
var ToolHookResultStatuses = []ToolHookResultStatus{
	ToolHookResultOK,
	ToolHookResultError,
	ToolHookResultDenied,
}

// Valid reports whether s is one of the closed result statuses.
func (s ToolHookResultStatus) Valid() bool {
	for _, known := range ToolHookResultStatuses {
		if s == known {
			return true
		}
	}
	return false
}

// ToolHookResult 是 post 工具 hook 看到的结果摘要（HookPostToolUse）。
//
// 它只携带引用，不携带原文：OutputRef 指向已经脱敏、且存在服务端存储里的输出
// （spool 片段 / 摘要），原始 stdout/stderr 绝不进入 hook payload。ExitCode 为
// nil 表示后端没有提供退出码，而不是 0。
type ToolHookResult struct {
	// Status 是结果状态（ok/error/denied）。
	Status ToolHookResultStatus
	// ExitCode 是进程/工具退出码；nil 表示未知。
	ExitCode *int
	// OutputRef 是脱敏输出的引用（spool 键或摘要），可以为空。
	OutputRef string
	// Truncated 表示输出被截断（有界 spool）。
	Truncated bool
}

// Validate checks the result summary shape.
func (r ToolHookResult) Validate() error {
	if !r.Status.Valid() {
		return &PayloadError{Hook: HookPostToolUse, Field: "Result.Status", Reason: fmt.Sprintf("%q is not one of ok/error/denied", string(r.Status))}
	}
	if strings.TrimSpace(r.OutputRef) == "" && r.Status == ToolHookResultOK {
		// 成功但没有任何输出是合法的（工具可能没有输出），所以这里不是错误；
		// 只记录契约：OutputRef 空 = 没有可引用的输出。
		return nil
	}
	return nil
}

// ToolHookPayload 是 HookPreToolUse / HookPostToolUse 的 payload。
//
// pre（HookPreToolUse）：在工具请求进入 Guard 之前触发。Result 必须为 nil，
// Arguments 必须带上（脱敏后的）请求参数——它就是 hook 用来判断要不要拒绝的
// 东西。
//
// post（HookPostToolUse）：在结果净化（脱敏）之后触发。Result 必须非 nil；
// Arguments 可缺省（post 观察的是结果，请求已经由 pre 上报过）。
type ToolHookPayload struct {
	// Identity 是服务端从权威资源填写的执行身份（run.ExecutionIdentity）。
	Identity run.ExecutionIdentity
	// EventID 是服务端分配的事件 ID：pre 是 tool.requested 事件，post 是工具
	// 结果事件的 ID。
	EventID string
	// ToolCallID 是后端原生工具调用 ID（ProviderRef），用于把请求与结果对齐，
	// 不超过 MaxToolCallIDBytes 字节。
	ToolCallID string
	// RequestFingerprint 是规范化请求的 hash，与守卫这次调用的
	// policy.Request.Fingerprint 同值，用于把 hook 决策和 policy 决策对上。
	RequestFingerprint string
	// Tool 是工具名。
	Tool string
	// Arguments 是脱敏后的工具请求参数（JSON）。pre 必须给出；post 可以缺省。
	Arguments json.RawMessage
	// Result 只在 post 给出。
	Result *ToolHookResult
}

// Validate checks the payload against the hook it is about to be triggered for.
// hook 必须是 HookPreToolUse 或 HookPostToolUse。
func (p ToolHookPayload) Validate(hook HookType) error {
	switch hook {
	case HookPreToolUse, HookPostToolUse:
	default:
		return &PayloadError{
			Hook:   hook,
			Field:  "hook",
			Reason: fmt.Sprintf("ToolHookPayload serves %s and %s only", HookPreToolUse, HookPostToolUse),
		}
	}

	if err := p.Identity.ValidateFor(string(run.EventToolRequested)); err != nil {
		return identityError(hook, err)
	}
	if err := checkPayloadField(hook, "EventID", p.EventID, payloadIDMaxBytes, true); err != nil {
		return err
	}
	if err := checkPayloadField(hook, "ToolCallID", p.ToolCallID, MaxToolCallIDBytes, true); err != nil {
		return err
	}
	if err := checkPayloadField(hook, "RequestFingerprint", p.RequestFingerprint, payloadIDMaxBytes, true); err != nil {
		return err
	}
	if err := checkPayloadField(hook, "Tool", p.Tool, 0, true); err != nil {
		return err
	}

	switch hook {
	case HookPreToolUse:
		if p.Result != nil {
			return &PayloadError{Hook: hook, Field: "Result", Reason: "a pre-tool payload must not carry a result: the tool has not run yet"}
		}
		if len(p.Arguments) == 0 {
			return &PayloadError{Hook: hook, Field: "Arguments", Missing: true, Reason: "a pre-tool payload must carry the (sanitized) request arguments"}
		}
		if !json.Valid(p.Arguments) {
			// 只说"不是合法 JSON"，绝不把内容写进错误文本（脱敏后也可能含敏感值）。
			return &PayloadError{Hook: hook, Field: "Arguments", Reason: fmt.Sprintf("not valid JSON (%d bytes)", len(p.Arguments))}
		}
	case HookPostToolUse:
		if p.Result == nil {
			return &PayloadError{Hook: hook, Field: "Result", Missing: true, Reason: "a post-tool payload must carry the result summary"}
		}
		if len(p.Arguments) > 0 && !json.Valid(p.Arguments) {
			return &PayloadError{Hook: hook, Field: "Arguments", Reason: fmt.Sprintf("not valid JSON (%d bytes)", len(p.Arguments))}
		}
		if err := p.Result.Validate(); err != nil {
			var pe *PayloadError
			if errors.As(err, &pe) {
				pe.Hook = hook
			}
			return err
		}
	}
	return nil
}

// DedupeKey is the "one event, one hook, one trigger record" key (§15 T1.07).
// It is empty when EventID is empty: an empty key must never be mistaken for a
// real one, so callers must Validate first (a validated payload always has an
// EventID).
func (p ToolHookPayload) DedupeKey(hook HookType) string {
	return dedupeKey(hook, p.EventID)
}

// RunHookPayload 是 HookRunStart / HookRunFinish 的 payload。
//
// start（HookRunStart）：在调度器认领之后（Run 处于 starting、attempt 已经由
// scheduler.claimed 创建）、后端进程启动之前触发。EventID 是 scheduler.claimed
// 事件 ID；身份需要 run+attempt+agent_revision（比 scheduler.claimed 事件本身的
// 最低要求更强，因为这个 hook 只在 attempt 已经存在时才可能触发）。
//
// finish（HookRunFinish）：在终态迁移之后触发。Status 必须是四个终态之一；
// EventID 是对应的终态事件 ID。身份是 Run 级的（终态事实只需要指明 Run）。
type RunHookPayload struct {
	// Identity 是服务端从权威资源填写的执行身份。
	Identity run.ExecutionIdentity
	// EventID 是服务端分配的事件 ID（start：scheduler.claimed；finish：终态事件）。
	EventID string
	// Status 是迁移后的 Run 状态。start 必须是 starting；finish 必须是终态。
	Status run.RunStatus
	// PreviousStatus 是迁移前的 Run 状态，可缺省（空 = 生产者没有记录）。给出时
	// 必须是合法状态，且不能等于 Status。
	PreviousStatus run.RunStatus
}

// Validate checks the payload against the hook it is about to be triggered for.
// hook 必须是 HookRunStart 或 HookRunFinish。
func (p RunHookPayload) Validate(hook HookType) error {
	switch hook {
	case HookRunStart, HookRunFinish:
	default:
		return &PayloadError{
			Hook:   hook,
			Field:  "hook",
			Reason: fmt.Sprintf("RunHookPayload serves %s and %s only", HookRunStart, HookRunFinish),
		}
	}

	if err := checkPayloadField(hook, "EventID", p.EventID, payloadIDMaxBytes, true); err != nil {
		return err
	}
	if !p.Status.Valid() {
		return &PayloadError{Hook: hook, Field: "Status", Reason: fmt.Sprintf("%q is not a RunStatus", string(p.Status))}
	}
	if p.PreviousStatus != "" {
		if !p.PreviousStatus.Valid() {
			return &PayloadError{Hook: hook, Field: "PreviousStatus", Reason: fmt.Sprintf("%q is not a RunStatus", string(p.PreviousStatus))}
		}
		if p.PreviousStatus == p.Status {
			return &PayloadError{Hook: hook, Field: "PreviousStatus", Reason: fmt.Sprintf("equals Status (%q): a transition must move between states", string(p.Status))}
		}
	}

	switch hook {
	case HookRunStart:
		if p.Status != run.RunStatusStarting {
			return &PayloadError{
				Hook:   hook,
				Field:  "Status",
				Reason: fmt.Sprintf("must be %q after the claim, got %q", run.RunStatusStarting, string(p.Status)),
			}
		}
		if err := p.Identity.ValidateFor(string(run.EventSchedulerClaimed)); err != nil {
			return identityError(hook, err)
		}
		// §28“tool 必须有 attempt”：RunStart 在 claim 之后触发，attempt 与
		// agent_revision 此时已经存在，所以这里比事件本身的最低要求更严。
		for _, f := range []struct {
			name  string
			value *string
		}{
			{"AttemptID", p.Identity.AttemptID},
			{"AgentRevisionID", p.Identity.AgentRevisionID},
		} {
			if f.value == nil {
				return &PayloadError{
					Hook:    hook,
					Field:   f.name,
					Missing: true,
					Reason:  "required by the run-start hook: it fires after the claim created the attempt",
				}
			}
		}
	case HookRunFinish:
		if !p.Status.IsTerminal() {
			return &PayloadError{
				Hook:   hook,
				Field:  "Status",
				Reason: fmt.Sprintf("must be a terminal Run status, got %q", string(p.Status)),
			}
		}
		// Every terminal status has its own terminal event type (CA-1 added
		// run.cancelled and run.expired), so the identity is checked against
		// the event the hook fires for.
		eventType, _ := RunTerminalEventType(p.Status)
		if err := p.Identity.ValidateFor(string(eventType)); err != nil {
			return identityError(hook, err)
		}
	}
	return nil
}

// DedupeKey is the "one event, one hook, one trigger record" key, empty when
// EventID is empty (see ToolHookPayload.DedupeKey).
func (p RunHookPayload) DedupeKey(hook HookType) string {
	return dedupeKey(hook, p.EventID)
}

// RunTerminalEventType maps a terminal Run status to the execution event type
// that reports it (the state event run.Decide names for every transition into
// that status). ok is false for a non-terminal status.
func RunTerminalEventType(status run.RunStatus) (run.ExecutionEventType, bool) {
	switch status {
	case run.RunStatusCompleted:
		return run.EventRunCompleted, true
	case run.RunStatusFailed:
		return run.EventRunFailed, true
	case run.RunStatusCancelled:
		return run.EventRunCancelled, true
	case run.RunStatusExpired:
		return run.EventRunExpired, true
	default:
		return "", false
	}
}

// dedupeKey builds "<hook>:<event id>". The hook type is part of the key
// because pre and post observe different events of the same tool call, and two
// hooks may legitimately observe the same event (a Run's terminal event is seen
// by HookRunFinish only, but the rule must hold for any pair).
func dedupeKey(hook HookType, eventID string) string {
	if hook == "" || eventID == "" {
		return ""
	}
	return string(hook) + ":" + eventID
}

// Payload error sentinels, for errors.Is. Mirrors run's identity errors: the
// offending field is named, the content never is.
var (
	// ErrPayloadHookMismatch is reported when a payload is validated for a hook
	// type that does not take it.
	ErrPayloadHookMismatch = errors.New("hooks: hook type does not take this payload")
	// ErrPayloadFieldMissing is reported when a field the hook type requires is
	// absent.
	ErrPayloadFieldMissing = errors.New("hooks: missing required payload field")
	// ErrPayloadFieldInvalid is reported when a field is present but malformed.
	ErrPayloadFieldInvalid = errors.New("hooks: invalid payload field")
)

// PayloadError names the offending field of a tool/Run hook payload. Field is a
// contract field name ("EventID", "ToolCallID", "Result", "Identity.run_id",
// ...); Reason explains the rejection without ever quoting the content.
type PayloadError struct {
	// Hook is the hook type the payload was validated for (empty when unknown).
	Hook HookType
	// Field is the offending field.
	Field string
	// Missing is true when the field is required and absent.
	Missing bool
	// Reason explains the rejection.
	Reason string
	// Cause is the wrapped lower-level error (e.g. run.IdentityError), if any.
	Cause error
}

// Error implements error.
func (e *PayloadError) Error() string {
	what := "invalid"
	if e.Missing {
		what = "required"
	}
	if e.Hook != "" {
		return fmt.Sprintf("hooks: payload for %s: field %q is %s: %s", e.Hook, e.Field, what, e.Reason)
	}
	return fmt.Sprintf("hooks: payload: field %q is %s: %s", e.Field, what, e.Reason)
}

// Unwrap exposes the wrapped cause, so errors.Is/As still find
// run.ErrMissingIdentityField and friends.
func (e *PayloadError) Unwrap() error { return e.Cause }

// Is lets errors.Is report the matching sentinel.
func (e *PayloadError) Is(target error) bool {
	switch target {
	case ErrPayloadHookMismatch:
		return e.Field == "hook"
	case ErrPayloadFieldMissing:
		return e.Missing
	case ErrPayloadFieldInvalid:
		return !e.Missing && e.Field != "hook"
	default:
		return false
	}
}

// checkPayloadField applies the shared non-empty / max-length rule to one
// string field; max 0 means "no length limit".
func checkPayloadField(hook HookType, field, value string, max int, required bool) error {
	if strings.TrimSpace(value) == "" {
		if required {
			return &PayloadError{Hook: hook, Field: field, Missing: true, Reason: "empty"}
		}
		return nil
	}
	if max > 0 && len(value) > max {
		return &PayloadError{
			Hook:   hook,
			Field:  field,
			Reason: fmt.Sprintf("length %d > %d bytes", len(value), max),
		}
	}
	return nil
}

// identityError wraps a run.IdentityError as a payload error that names the
// identity field while keeping the run sentinels reachable.
func identityError(hook HookType, err error) error {
	field := "Identity"
	var ie *run.IdentityError
	if errors.As(err, &ie) {
		field = "Identity." + ie.Field
	}
	return &PayloadError{Hook: hook, Field: field, Missing: isMissingIdentity(err), Reason: err.Error(), Cause: err}
}

// isMissingIdentity reports whether an identity error is about a required field
// being absent.
func isMissingIdentity(err error) bool {
	return errors.Is(err, run.ErrMissingIdentityField)
}
