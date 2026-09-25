// Package execbackend 定义 CodeFlow 的执行后端适配协议（计划 §15 T1.06、§27.7）。
//
// 本包是纯类型层：只有接口、结构体、枚举、校验与错误码；不含执行实现、进程监督
// 与领域事件分配。适配器（T1.06.b 的 fake、T1.13/T4.01–T4.03 的真实 CLI）实现
// Backend 与 Session 并只产出观察记录 Observation；服务端消费观察记录并自行分配
// event ID、sequence、identity 与 occurred_at（§27.4），CLI 不能伪造领域事件。
// 因此 Observation 中不存在任何可自定义的序号或身份字段（由 types_test.go 的
// 反射用例守住）。
//
// 其他冻结边界：
//   - 工具事件不承诺每个后端都能截获所有 shell 子行为；观察记录是尽力而为的证据，
//     不是审计事实（§15 T1.06）。
//   - 只读能力探测不得发送模型请求、不得自动登录或安装、不得回显凭据（§27.7）。
//   - 缺证据的能力一律为 false；未证明支持的能力调用返回 capability_unavailable。
//   - 依赖约束（§27.1）：只允许 import 标准库，不得 import internal/run、runstore、
//     adapters、policy 或其他业务包。ProjectID/RunID/AttemptID/AgentRevisionID 等
//     跨包标识一律是不透明字符串，本包不解析其结构。
package execbackend

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"time"
)

// 协议上限（§27.4：单事件默认最大 256 KiB、控制帧 64 KiB，超长输出由服务端存
// 限额 artifact 并发截断/引用事件）。
const (
	// MaxObservationBytes 单条观察记录的默认最大字节数：256 KiB。
	// 适配器不得自行截断后把不完整内容当作完整输出投递。
	MaxObservationBytes = 256 * 1024
	// MaxControlFrameBytes 控制类观察的字节上限：64 KiB。
	// 控制类 kind 见 ObservationKind.IsControlKind。
	MaxControlFrameBytes = 64 * 1024
	// MaxProviderRefBytes ProviderRef 的最大字节数。
	MaxProviderRefBytes = 256
)

// ---------------------------------------------------------------------------
// 适配器接口
// ---------------------------------------------------------------------------

// Backend 是一个执行后端的适配入口（§15 T1.06）。
//
// 实现者（fake / 真实 CLI adapter）必须满足：
//   - 三个方法并发安全，且不得修改调用方传入的 StartRequest（含 Env map）。
//   - Capabilities 只做只读探测；不得发送模型请求、不得自动登录/安装、不得回显凭据。
//   - Prepare 无副作用：只校验与查能力，不启动进程、不写磁盘。
//   - Start 成功后返回的 Session 由调用方拥有，调用方必须 Close。
type Backend interface {
	// Name 返回后端稳定标识（不透明字符串，如 "fake"、"claude-code"）。
	Name() string
	// Capabilities 只读探测后端能力（§27.7）。
	// 缺证据的能力必须为 false（Evidence 中无对应非空条目）；
	// 探测本身失败返回 backend_unavailable。
	Capabilities(ctx context.Context) (CapabilityReport, error)
	// Prepare 校验 req 与 req.Required 能力，无副作用。
	// req 不合法返回 invalid_request（Field 指出字段）；能力缺失返回
	// capability_unavailable（Missing 列出缺失项，可直接复用 CheckRequired）。
	// 返回的 PreparedStart 应固化已校验的请求（可用 StartRequest.Clone），
	// 使调用方之后修改原请求不影响 Start。
	Prepare(ctx context.Context, req StartRequest) (PreparedStart, error)
	// Start 按已校验的 PreparedStart 启动一次执行，返回由调用方拥有的 Session。
	// 零值或未经过 Prepare 的 PreparedStart 必须被拒绝（invalid_request）。
	// 启动失败返回 process_start_failed；后端整体不可用返回 backend_unavailable。
	Start(ctx context.Context, p PreparedStart) (Session, error)
}

// Session 是一次已启动执行会话的句柄（§15 T1.06）。
//
// 生命周期与关闭责任（冻结）：
//   - 构造者：Backend.Start 是唯一构造者。调用方（Run orchestrator）取得 Session
//     后拥有它，必须 Close，且只由调用方关闭。
//   - Observations：只读 channel，仅由 Session 关闭。会话投递终结观察（Kind=exited）
//     之后关闭该 channel；调用方 Close 时也必须关闭它。实现保证 channel 只被关闭
//     一次（Close 幂等）。
//   - Wait：可重复调用，同一 Session 的每次调用返回同一最终结果。会话终结后立即
//     返回；未终结时阻塞直到终结或 ctx 取消。ctx 取消只影响本次等待，不改变会话
//     结果（不因调用方放弃等待而杀进程）。
//   - Cancel：幂等，可重复调用。CancelGraceful 先软终止、超过 ProcessControl.GracePeriod
//     再强制；CancelForce 直接强制。会话已经终结时返回 nil（已达到调用方想要的结果）。
//   - Close：幂等，释放进程/管道/临时资源。Close 后 Observations 已关闭、Wait 立即
//     返回最终结果、Approve/Cancel 返回 session_closed。Close 不等待进程退出：
//     需要确认退出请先 Wait 再 Close。
//   - 并发：所有方法必须并发安全（服务端可能在读 channel 的同时取消或审批）。
//   - 适配器只投递观察记录；event ID/sequence/identity/occurred_at 由服务端分配。
type Session interface {
	// Observations 返回只读观察 channel。语义见 Session 生命周期说明：
	// 终结观察（Kind=exited）投递后或 Close 时由 Session 关闭该 channel。
	Observations() <-chan Observation
	// Approve 投递一次审批裁决（§27.2.5：批准后执行时重验；同一 tool_call_id
	// 最多一次授权消费）。后端未证明支持 approval_hook 能力时返回
	// capability_unavailable；未知 ApprovalID 返回 invalid_request；重复投递同一
	// 裁决必须幂等（不产生第二次副作用）。会话已关闭返回 session_closed。
	Approve(ctx context.Context, d ApprovalDecision) error
	// Cancel 请求取消，幂等。mode 不合法返回 invalid_request；只有 CancelGraceful
	// 受 cancel_graceful 能力约束——未声明时返回 capability_unavailable，调用方应升级
	// 为 CancelForce。CancelForce 永远可用、不受任何能力闸控：Run 必须始终能被停下
	// （§27.2 第 6 条）。会话已关闭返回 session_closed。
	Cancel(ctx context.Context, mode CancelMode) error
	// Wait 等待会话终结并返回最终结果，可重复调用且结果一致。
	// 实现内部错误（如协议损坏）以 error 返回，同时仍应有终结观察与最终结果；
	// ctx 取消只终止本次等待。
	Wait(ctx context.Context) (ExitResult, error)
	// Close 释放资源，幂等。语义见 Session 生命周期说明。
	Close() error
}

// ---------------------------------------------------------------------------
// 输入：冻结输入、工作目录、进程控制、所需能力
// ---------------------------------------------------------------------------

// StartRequest 是一次执行启动请求：冻结输入 + 工作目录 + 进程控制 + 所需能力。
//
// WorkDir 是 Run 工作副本（由 T1.09/T1.10 保证不是用户主树）；Env 是已过滤的
// 白名单环境，不得包含凭据（§27.5.8：Agent CLI 无项目 secret，凭据由
// CredentialBroker 按引用注入）。标识字段都是不透明字符串，本包不解析其结构。
type StartRequest struct {
	Run      RunRef         `json:"run"`
	Input    FrozenInput    `json:"input"`
	WorkDir  string         `json:"work_dir"`
	Process  ProcessControl `json:"process"`
	Required []Capability   `json:"required,omitempty"`
}

// RunRef 标识这次执行归属的 Run/Attempt/Agent 修订（§27.1：执行中才有
// attempt_id/agent_revision_id，服务端从权威资源验证父子关系）。
type RunRef struct {
	ProjectID       string `json:"project_id"`
	RunID           string `json:"run_id"`
	AttemptID       string `json:"attempt_id"`
	AgentRevisionID string `json:"agent_revision_id"`
}

// FrozenInput 是这次执行的冻结输入（§27.2.1：Run 是一次冻结输入的执行）。
type FrozenInput struct {
	// SnapshotID 冻结输入快照标识（不透明字符串）。
	SnapshotID string `json:"snapshot_id"`
	// SnapshotHash 快照内容 hash，格式固定为 "sha256:" + 64 位小写 hex。
	SnapshotHash string `json:"snapshot_hash"`
	// Prompt 用户提示词。是否为空由 Run 创建侧约束，本层不强制。
	Prompt string `json:"prompt"`
}

// ProcessControl 是调用方对进程的控制参数（§27.6 默认值：grace cancel 5s、
// Run wall deadline 30min）。
type ProcessControl struct {
	// Env 已过滤的白名单环境变量；适配器不得修改该 map。
	Env map[string]string `json:"env,omitempty"`
	// GracePeriod 软终止到强制终止的宽限期；不得为负。
	GracePeriod time.Duration `json:"grace_period"`
	// HardDeadline 硬截止时间（wall deadline）。零值表示调用方未设硬截止，
	// 由服务端另行控制。
	HardDeadline time.Time `json:"hard_deadline,omitempty"`
	// MaxOutputBytes 输出字节上限；不得为负。<=0 表示调用方未设上限，
	// 由服务端 spool/磁盘上限控制（§27.6）。
	MaxOutputBytes int64 `json:"max_output_bytes"`
}

// PreparedStart 是 Prepare 的产物：已校验的请求 + 当时的 CapabilityReport。
// 只能由 Backend.Prepare 产生并交给 Backend.Start 使用；调用方不应自行拼装。
type PreparedStart struct {
	Request StartRequest     `json:"request"`
	Report  CapabilityReport `json:"report"`
}

// Validate 校验请求字段，返回 Code=invalid_request 的 *Error（Field 指出字段）。
// 只返回第一个错误；不做任何 I/O，也不启动进程。
//
// 覆盖：RunRef 四字段非空、SnapshotID 非空、SnapshotHash 格式、WorkDir 绝对且已
// Clean、GracePeriod >= 0、MaxOutputBytes >= 0、Required 均为已知枚举且无重复。
func (r StartRequest) Validate() error {
	if strings.TrimSpace(r.Run.ProjectID) == "" {
		return invalidRequestf("run.project_id", "run.project_id is required")
	}
	if strings.TrimSpace(r.Run.RunID) == "" {
		return invalidRequestf("run.run_id", "run.run_id is required")
	}
	if strings.TrimSpace(r.Run.AttemptID) == "" {
		return invalidRequestf("run.attempt_id", "run.attempt_id is required")
	}
	if strings.TrimSpace(r.Run.AgentRevisionID) == "" {
		return invalidRequestf("run.agent_revision_id", "run.agent_revision_id is required")
	}
	if strings.TrimSpace(r.Input.SnapshotID) == "" {
		return invalidRequestf("input.snapshot_id", "input.snapshot_id is required")
	}
	if !validSnapshotHash(r.Input.SnapshotHash) {
		return invalidRequestf("input.snapshot_hash", "input.snapshot_hash %q must be \"sha256:\" followed by 64 lowercase hex characters", r.Input.SnapshotHash)
	}
	if r.WorkDir == "" {
		return invalidRequestf("work_dir", "work_dir is required")
	}
	if !filepath.IsAbs(r.WorkDir) {
		return invalidRequestf("work_dir", "work_dir %q must be an absolute path", r.WorkDir)
	}
	if cleaned := filepath.Clean(r.WorkDir); cleaned != r.WorkDir {
		return invalidRequestf("work_dir", "work_dir %q must be cleaned (%q)", r.WorkDir, cleaned)
	}
	if r.Process.GracePeriod < 0 {
		return invalidRequestf("process.grace_period", "process.grace_period must not be negative, got %s", r.Process.GracePeriod)
	}
	if r.Process.MaxOutputBytes < 0 {
		return invalidRequestf("process.max_output_bytes", "process.max_output_bytes must not be negative, got %d", r.Process.MaxOutputBytes)
	}
	seen := make(map[Capability]struct{}, len(r.Required))
	for _, c := range r.Required {
		if !c.Valid() {
			return invalidRequestf("required", "unknown capability %q in required", string(c))
		}
		if _, dup := seen[c]; dup {
			return invalidRequestf("required", "duplicate capability %q in required", string(c))
		}
		seen[c] = struct{}{}
	}
	return nil
}

// Clone 深拷贝请求中的可变部分（Process.Env、Required），供 Prepare 固化已校验的
// 请求，使调用方在 Prepare 之后修改原请求不影响 PreparedStart。
func (r StartRequest) Clone() StartRequest {
	clone := r
	if r.Process.Env != nil {
		env := make(map[string]string, len(r.Process.Env))
		for k, v := range r.Process.Env {
			env[k] = v
		}
		clone.Process.Env = env
	}
	if r.Required != nil {
		clone.Required = append([]Capability(nil), r.Required...)
	}
	return clone
}

// validSnapshotHash 校验 "sha256:" + 64 位小写 hex。
func validSnapshotHash(value string) bool {
	const prefix = "sha256:"
	const hexLen = 64
	if !strings.HasPrefix(value, prefix) {
		return false
	}
	hexPart := value[len(prefix):]
	if len(hexPart) != hexLen {
		return false
	}
	for i := 0; i < len(hexPart); i++ {
		c := hexPart[i]
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') {
			continue
		}
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// 输出：观察记录
// ---------------------------------------------------------------------------

// Observation 是适配器投递给服务端的一条观察记录（§27.4：适配器输出
// BackendObservation，服务端填 identity/ID/time）。
//
// 冻结约束：本结构没有、也不得有任何可自定义的序号或身份字段——没有 sequence、
// event ID、project/run/attempt ID、identity、actor、occurred_at。event ID/sequence
// 由服务端在同一数据库事务内分配，CLI 不能伪造领域事件。
type Observation struct {
	// Kind 观察类型。
	Kind ObservationKind `json:"kind"`
	// ObservedAt 后端本地时钟，仅供参考；领域事件的 occurred_at 由服务端分配
	// （§27.4），本字段不得用作事件时间。
	ObservedAt time.Time `json:"observed_at"`
	// ProviderRef 后端原生消息/工具调用 ID，供服务端去重；不是序号，≤256 字节。
	// 服务端不得把它当作 event ID。
	ProviderRef string `json:"provider_ref,omitempty"`
	// Payload 原始负载（后端原生 JSON 片段）。服务端把它映射为领域事件 payload；
	// 单条 ≤ MaxObservationBytes，控制类 kind ≤ MaxControlFrameBytes。
	Payload json.RawMessage `json:"payload,omitempty"`
}

// ObservationKind 观察类型（封闭枚举）。
//
// 与领域事件类型（backend/schemas/execution-event.schema.json）不是同一套名称：
// observation → event 的映射由服务端做，本枚举不必与事件 type 同名。
type ObservationKind string

const (
	// ObservationProcessStarted 进程已启动。
	ObservationProcessStarted ObservationKind = "process_started"
	// ObservationOutput 执行输出片段（stdout/stderr/模型输出）。
	ObservationOutput ObservationKind = "output"
	// ObservationToolRequested 工具调用请求（尽力而为：不承诺截获所有 shell 子行为）。
	ObservationToolRequested ObservationKind = "tool_requested"
	// ObservationToolResult 工具调用结果（尽力而为，同上）。
	ObservationToolResult ObservationKind = "tool_result"
	// ObservationApprovalRequired 后端要求审批；服务端据此持久化 approval 并进入
	// waiting_approval（§21.1）。
	ObservationApprovalRequired ObservationKind = "approval_required"
	// ObservationUsage 用量报告（§27.6：未知标 unknown、估计值标 estimated）。
	ObservationUsage ObservationKind = "usage"
	// ObservationProtocolWarning 协议层警告（乱码帧、未知消息等）。
	ObservationProtocolWarning ObservationKind = "protocol_warning"
	// ObservationExited 会话终结观察；Payload 必须是 ExitResult。投递本观察后
	// Session 关闭 Observations channel。
	ObservationExited ObservationKind = "exited"
)

// AllObservationKinds 返回规范顺序的全部观察类型，供适配器与服务端映射复用。
func AllObservationKinds() []ObservationKind {
	return []ObservationKind{
		ObservationProcessStarted,
		ObservationOutput,
		ObservationToolRequested,
		ObservationToolResult,
		ObservationApprovalRequired,
		ObservationUsage,
		ObservationProtocolWarning,
		ObservationExited,
	}
}

// Valid 报告 kind 是否为已知枚举值。
func (k ObservationKind) Valid() bool {
	switch k {
	case ObservationProcessStarted,
		ObservationOutput,
		ObservationToolRequested,
		ObservationToolResult,
		ObservationApprovalRequired,
		ObservationUsage,
		ObservationProtocolWarning,
		ObservationExited:
		return true
	default:
		return false
	}
}

// IsControlKind 报告 kind 是否属于控制类观察，适用 64 KiB 上限（§27.4 控制帧）。
func (k ObservationKind) IsControlKind() bool {
	switch k {
	case ObservationProcessStarted, ObservationApprovalRequired, ObservationProtocolWarning, ObservationExited:
		return true
	default:
		return false
	}
}

// IsTerminal 报告 kind 是否为终结观察。终结观察只有 exited 一种：投递它之后
// 会话终结，Session 关闭 Observations channel。
func (k ObservationKind) IsTerminal() bool {
	return k == ObservationExited
}

// IsTerminal 报告该观察是否为终结观察（见 ObservationKind.IsTerminal）。
func (o Observation) IsTerminal() bool {
	return o.Kind.IsTerminal()
}

// Validate 校验观察记录是否满足协议上限与结构要求（§27.4）。
//
// 只做本地校验，不解释 Payload 的领域语义；唯一例外是 exited：它的 Payload 必须
// 能解码为 ExitResult 且 ExitResult 合法。exited 相关的错误 Field 直接指向
// ExitResult 的字段（如 "reason"、"exit_code"、"usage.quality"）。
func (o Observation) Validate() error {
	if !o.Kind.Valid() {
		return invalidRequestf("kind", "unknown observation kind %q", string(o.Kind))
	}
	if len(o.ProviderRef) > MaxProviderRefBytes {
		return invalidRequestf("provider_ref", "provider_ref is %d bytes, limit %d", len(o.ProviderRef), MaxProviderRefBytes)
	}
	if len(o.Payload) > MaxObservationBytes {
		return invalidRequestf("payload", "payload is %d bytes, limit %d", len(o.Payload), MaxObservationBytes)
	}
	if o.Kind.IsControlKind() && len(o.Payload) > MaxControlFrameBytes {
		return invalidRequestf("payload", "control observation %s payload is %d bytes, limit %d", string(o.Kind), len(o.Payload), MaxControlFrameBytes)
	}
	if o.Kind == ObservationExited {
		var result ExitResult
		if err := json.Unmarshal(o.Payload, &result); err != nil {
			return invalidRequestf("payload", "exited payload is not a valid ExitResult: %v", err)
		}
		if err := result.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// 终结结果与用量
// ---------------------------------------------------------------------------

// ExitResult 是会话的最终结果：由 Session.Wait 返回，也是 exited 终结观察的 Payload。
type ExitResult struct {
	// ExitCode 进程退出码；未知时为 nil（不得用 0 冒充未知）。
	ExitCode *int `json:"exit_code,omitempty"`
	// Reason 终结原因。
	Reason ExitReason `json:"reason"`
	// Retryable 是否可安全重试（§27.6：仅对尚未确认接收且无副作用的失败重试；
	// policy/审批拒绝/验证失败不重试）。
	Retryable bool `json:"retryable"`
	// Usage 本次执行的用量事实。
	Usage Usage `json:"usage"`
}

// ExitReason 终结原因（封闭枚举）。
type ExitReason string

const (
	// ExitReasonCompleted 执行正常完成（对应 §21.1 process.exited(0) → completed）。
	ExitReasonCompleted ExitReason = "completed"
	// ExitReasonFailed 执行失败（对应 process.exited(!=0) → failed）。
	ExitReasonFailed ExitReason = "failed"
	// ExitReasonCancelled 调用方取消后终结（grace/force 记录齐全）。
	ExitReasonCancelled ExitReason = "cancelled"
	// ExitReasonTimeout 硬截止时间到。
	ExitReasonTimeout ExitReason = "timeout"
	// ExitReasonCrashed 进程崩溃/异常退出（含无法确认退出）。
	ExitReasonCrashed ExitReason = "crashed"
	// ExitReasonProtocolError 后端协议损坏，无法继续解析。
	ExitReasonProtocolError ExitReason = "protocol_error"
)

// Valid 报告 reason 是否为已知枚举值。
func (r ExitReason) Valid() bool {
	switch r {
	case ExitReasonCompleted,
		ExitReasonFailed,
		ExitReasonCancelled,
		ExitReasonTimeout,
		ExitReasonCrashed,
		ExitReasonProtocolError:
		return true
	default:
		return false
	}
}

// Validate 校验终结结果自身的一致性。不强制 ExitCode 与 Reason 的对应关系
// （exit 0 → completed 之类的状态迁移由服务端按 §21.1 判定）。
func (r ExitResult) Validate() error {
	if !r.Reason.Valid() {
		return invalidRequestf("reason", "unknown exit reason %q", string(r.Reason))
	}
	if r.ExitCode != nil && *r.ExitCode < 0 {
		return invalidRequestf("exit_code", "exit_code must not be negative, got %d", *r.ExitCode)
	}
	return r.Usage.Validate()
}

// Usage 是一次执行的用量事实（§27.6）。
//
// 未知标 unknown、估计值标 estimated，不能把未知当 0：nil 指针 + Quality=unknown
// 才是"未知"的表示，0 只表示真实的零。
type Usage struct {
	InputTokens  *int64       `json:"input_tokens,omitempty"`
	OutputTokens *int64       `json:"output_tokens,omitempty"`
	CostMinor    *int64       `json:"cost_minor,omitempty"`
	Currency     string       `json:"currency,omitempty"`
	Quality      UsageQuality `json:"quality"`
}

// UsageQuality 用量数据质量（封闭枚举）。
type UsageQuality string

const (
	// UsageQualityReported 后端/供应商直接上报。
	UsageQualityReported UsageQuality = "reported"
	// UsageQualityEstimated 估计值。
	UsageQualityEstimated UsageQuality = "estimated"
	// UsageQualityUnknown 未知；不得当成 0。
	UsageQualityUnknown UsageQuality = "unknown"
)

// Valid 报告 quality 是否为已知枚举值。
func (q UsageQuality) Valid() bool {
	switch q {
	case UsageQualityReported, UsageQualityEstimated, UsageQualityUnknown:
		return true
	default:
		return false
	}
}

// Validate 校验用量字段：quality 必须是已知枚举；指针字段非 nil 时不得为负；
// 有金额时必须有币种（§27.1：金额存最小单位整数 + currency）。
func (u Usage) Validate() error {
	if !u.Quality.Valid() {
		return invalidRequestf("usage.quality", "unknown usage quality %q", string(u.Quality))
	}
	if u.InputTokens != nil && *u.InputTokens < 0 {
		return invalidRequestf("usage.input_tokens", "usage.input_tokens must not be negative, got %d", *u.InputTokens)
	}
	if u.OutputTokens != nil && *u.OutputTokens < 0 {
		return invalidRequestf("usage.output_tokens", "usage.output_tokens must not be negative, got %d", *u.OutputTokens)
	}
	if u.CostMinor != nil {
		if *u.CostMinor < 0 {
			return invalidRequestf("usage.cost_minor", "usage.cost_minor must not be negative, got %d", *u.CostMinor)
		}
		if strings.TrimSpace(u.Currency) == "" {
			return invalidRequestf("usage.currency", "usage.currency is required when cost_minor is present")
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// 审批与取消
// ---------------------------------------------------------------------------

// ApprovalDecision 是服务端对一次 approval_required 观察的裁决（§27.2.5：批准后
// 执行时重验；同一 tool_call_id 最多一次授权消费）。
type ApprovalDecision struct {
	// ApprovalID 服务端审批对象 ID（不透明字符串）。
	ApprovalID string `json:"approval_id"`
	// ProviderRef 对应 approval_required 观察的 ProviderRef，便于后端关联；
	// 不是 event ID。
	ProviderRef string `json:"provider_ref,omitempty"`
	// Approved 是否批准。false 是明确拒绝，不能当成"未填写"。
	Approved bool `json:"approved"`
	// Reason 裁决原因（拒绝时建议填写）。
	Reason string `json:"reason,omitempty"`
}

// CancelMode 取消方式（封闭枚举）。
type CancelMode string

const (
	// CancelGraceful 先软终止，超过 ProcessControl.GracePeriod 再强制。
	CancelGraceful CancelMode = "graceful"
	// CancelForce 直接强制终止。
	CancelForce CancelMode = "force"
)

// Valid 报告 mode 是否为已知枚举值。
func (m CancelMode) Valid() bool {
	switch m {
	case CancelGraceful, CancelForce:
		return true
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// 能力
// ---------------------------------------------------------------------------

// Capability 后端能力标识（封闭枚举）。
//
// 未证明支持的能力调用返回 capability_unavailable（§27.7）：交互式 PTY、运行中
// inject、任意后端 pause/resume 都是可选能力，不因接口声明就算交付。
type Capability string

const (
	// CapabilityNonInteractive 支持受控非交互执行（3.0 首期验收主路径）。
	CapabilityNonInteractive Capability = "non_interactive"
	// CapabilityJSONStream 支持机器可读的 JSON/JSONL 事件流。
	CapabilityJSONStream Capability = "json_stream"
	// CapabilityApprovalHook 支持审批回调（approval_required → Approve）。
	CapabilityApprovalHook Capability = "approval_hook"
	// CapabilityCancelGraceful 支持软终止（先 grace 后 force）。
	CapabilityCancelGraceful Capability = "cancel_graceful"
	// CapabilityResumeCheckpoint 支持从已验证 checkpoint 恢复（§21.1 paused → running）。
	CapabilityResumeCheckpoint Capability = "resume_checkpoint"
	// CapabilityMCP 支持 MCP 协议接入。
	CapabilityMCP Capability = "mcp"
	// CapabilitySandbox 支持受支持的平台沙箱（§27.5.7：证明不了隔离就不能算）。
	CapabilitySandbox Capability = "sandbox"
	// CapabilityPTY 支持交互式 PTY（可选能力，默认使用 pipes）。
	CapabilityPTY Capability = "pty"
	// CapabilityInject 支持运行中注入（可选能力）。
	CapabilityInject Capability = "inject"
	// CapabilityUsageTokens 上报 token 用量。
	CapabilityUsageTokens Capability = "usage_tokens"
	// CapabilityUsageCost 上报费用（最小单位整数 + currency）。
	CapabilityUsageCost Capability = "usage_cost"
)

// AllCapabilities 返回规范顺序的全部能力枚举，供适配器探测与服务端/UI 枚举复用。
func AllCapabilities() []Capability {
	return []Capability{
		CapabilityNonInteractive,
		CapabilityJSONStream,
		CapabilityApprovalHook,
		CapabilityCancelGraceful,
		CapabilityResumeCheckpoint,
		CapabilityMCP,
		CapabilitySandbox,
		CapabilityPTY,
		CapabilityInject,
		CapabilityUsageTokens,
		CapabilityUsageCost,
	}
}

// Valid 报告能力是否为已知枚举值。
func (c Capability) Valid() bool {
	switch c {
	case CapabilityNonInteractive,
		CapabilityJSONStream,
		CapabilityApprovalHook,
		CapabilityCancelGraceful,
		CapabilityResumeCheckpoint,
		CapabilityMCP,
		CapabilitySandbox,
		CapabilityPTY,
		CapabilityInject,
		CapabilityUsageTokens,
		CapabilityUsageCost:
		return true
	default:
		return false
	}
}
