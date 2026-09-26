// Package custom 定义 CodeFlow 自定义执行器协议 v1（计划 §15 T4.03、§28 T4.03.a）。
//
// 协议：一个受监督的本地子进程通过 stdin/stdout 上的有界 UTF-8 JSON Lines 与服务端
// 通信，一帧一行、以 "\n" 结尾；stderr 只是诊断，永不作为帧解析。本包只做**无状态**
// 的帧编解码、校验与握手协商；有状态的 pending tool map、终止状态、迟到帧、真实子
// 进程与启动命令配置属 T4.03.b/c。
//
// 冻结边界（§27.4/§27.7 与 execbackend 包文档）：
//   - 适配器只产出观察记录；event ID/sequence/identity/occurred_at 由服务端在同一
//     数据库事务内分配。后端帧不得携带这些字段——**拒绝，而不是忽略**
//     （ReservedFieldNames + CodeForbiddenField）。
//   - 协议里不存在"写主树/写 runstore/写审计"的帧；唯一可写根是 start.work_dir。
//   - Env 永不进入协议帧：Env 由监督器在 spawn 时设置（可能含受控变量），不能回显。
//   - 未知字段一律拒绝（DisallowUnknownFields + schema additionalProperties:false）。
//   - 错误文本只含字段名、长度与协议枚举短标识符，绝不回显原始帧字节或 payload 内容。
//   - 依赖约束（§27.1）：只允许 import 标准库与 internal/execbackend。
package custom

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/codeflow/backend/internal/execbackend"
)

// 协议常量。
const (
	// ProtocolVersion 是当前协议版本：hello.protocol_version 与每帧的 schema_version
	// 都必须等于它。
	ProtocolVersion = 1
	// MaxFrameBytes 是单帧（整行，含信封）的字节上限：观察负载上限 + 16 KiB 信封余量。
	MaxFrameBytes = execbackend.MaxObservationBytes + 16*1024
	// MaxControlFrameBytes 是控制帧整行字节上限（hello/exit/start/approval_response/
	// cancel，以及 kind 为控制类的 observation 帧），与 §27.4 的 64 KiB 控制帧一致。
	MaxControlFrameBytes = execbackend.MaxControlFrameBytes
	// MaxToolNameBytes 是 hello.tools[].name 的字节上限。
	MaxToolNameBytes = 128
	// maxTokenEchoBytes 是错误文本里允许回显的单个标识符长度上限；更长的值只报长度。
	maxTokenEchoBytes = 64
)

// ---------------------------------------------------------------------------
// 帧类型
// ---------------------------------------------------------------------------

// FrameType 是帧类型（封闭枚举）。
type FrameType string

const (
	// FrameHello 后端→服务端：握手（协议版本、能力、工具效果声明）。
	FrameHello FrameType = "hello"
	// FrameObservation 后端→服务端：一条观察记录。
	FrameObservation FrameType = "observation"
	// FrameExit 后端→服务端：终结帧（ExitResult），映射为 exited 观察。
	FrameExit FrameType = "exit"
	// FrameStart 服务端→后端：启动一次执行（只读身份 + 冻结输入 + 工作目录 + 进程控制）。
	FrameStart FrameType = "start"
	// FrameApprovalResponse 服务端→后端：审批裁决。
	FrameApprovalResponse FrameType = "approval_response"
	// FrameCancel 服务端→后端：取消请求（graceful|force）。
	FrameCancel FrameType = "cancel"
)

// AllFrameTypes 返回规范顺序的全部帧类型（后端→服务端 3 种 + 服务端→后端 3 种）。
// 该顺序与 custom-backend.schema.json 的 frame_type 枚举一致（漂移测试守住）。
func AllFrameTypes() []FrameType {
	return []FrameType{
		FrameHello,
		FrameObservation,
		FrameExit,
		FrameStart,
		FrameApprovalResponse,
		FrameCancel,
	}
}

// BackendFrameTypes 返回后端→服务端的帧类型。
func BackendFrameTypes() []FrameType {
	return []FrameType{FrameHello, FrameObservation, FrameExit}
}

// HostFrameTypes 返回服务端→后端的帧类型。
func HostFrameTypes() []FrameType {
	return []FrameType{FrameStart, FrameApprovalResponse, FrameCancel}
}

// Valid 报告帧类型是否为已知枚举值。
func (t FrameType) Valid() bool {
	switch t {
	case FrameHello, FrameObservation, FrameExit, FrameStart, FrameApprovalResponse, FrameCancel:
		return true
	default:
		return false
	}
}

// IsBackendFrame 报告帧是否由后端发往服务端。
func (t FrameType) IsBackendFrame() bool {
	switch t {
	case FrameHello, FrameObservation, FrameExit:
		return true
	default:
		return false
	}
}

// IsHostFrame 报告帧是否由服务端发往后端。
func (t FrameType) IsHostFrame() bool {
	switch t {
	case FrameStart, FrameApprovalResponse, FrameCancel:
		return true
	default:
		return false
	}
}

// IsControlFrame 报告帧是否整体适用 64 KiB 控制帧上限。
// observation 不在其中：它按 kind 判定（见 frameLimitBytes）。
func (t FrameType) IsControlFrame() bool {
	switch t {
	case FrameHello, FrameExit, FrameStart, FrameApprovalResponse, FrameCancel:
		return true
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// 工具效果与 actor 类型
// ---------------------------------------------------------------------------

// ToolEffectKind 是工具效果声明（封闭枚举）。工具效果是后端对"这个工具会做什么"的
// 声明，服务端据此判断是否需要审批/沙箱；声明本身不授予任何权限。
type ToolEffectKind string

const (
	// ToolEffectReadWorkspace 读工作副本（唯一可读根是 start.work_dir）。
	ToolEffectReadWorkspace ToolEffectKind = "read_workspace"
	// ToolEffectWriteWorkspace 写工作副本（后端不得写主树，§27.5.7）。
	ToolEffectWriteWorkspace ToolEffectKind = "write_workspace"
	// ToolEffectExecuteProcess 启动子进程。
	ToolEffectExecuteProcess ToolEffectKind = "execute_process"
	// ToolEffectNetwork 访问网络。
	ToolEffectNetwork ToolEffectKind = "network"
)

// AllToolEffectKinds 返回规范顺序的全部工具效果枚举。
func AllToolEffectKinds() []ToolEffectKind {
	return []ToolEffectKind{
		ToolEffectReadWorkspace,
		ToolEffectWriteWorkspace,
		ToolEffectExecuteProcess,
		ToolEffectNetwork,
	}
}

// Valid 报告工具效果是否为已知枚举值。
func (k ToolEffectKind) Valid() bool {
	switch k {
	case ToolEffectReadWorkspace, ToolEffectWriteWorkspace, ToolEffectExecuteProcess, ToolEffectNetwork:
		return true
	default:
		return false
	}
}

// ActorType 是 start.identity.actor.type（封闭枚举），镜像
// backend/schemas/execution-identity.schema.json。
type ActorType string

const (
	// ActorUser 用户发起。
	ActorUser ActorType = "user"
	// ActorAgent 代理发起。
	ActorAgent ActorType = "agent"
	// ActorSystem 系统发起。
	ActorSystem ActorType = "system"
	// ActorIntegration 外部集成发起。
	ActorIntegration ActorType = "integration"
)

// AllActorTypes 返回规范顺序的全部 actor 类型枚举。
func AllActorTypes() []ActorType {
	return []ActorType{ActorUser, ActorAgent, ActorSystem, ActorIntegration}
}

// Valid 报告 actor 类型是否为已知枚举值。
func (a ActorType) Valid() bool {
	switch a {
	case ActorUser, ActorAgent, ActorSystem, ActorIntegration:
		return true
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// 后端→服务端帧
// ---------------------------------------------------------------------------

// BackendFrame 是后端→服务端的一帧（hello | observation | exit）。
//
// 接口含本包私有方法，因此帧类型集合是封闭的：外部包不能自定义帧类型混进协议。
type BackendFrame interface {
	// FrameType 返回帧类型。
	FrameType() FrameType
	// Validate 校验帧字段（含 execbackend 层的观察/终结结果校验）。
	Validate() error
	// normalize 返回写入正确信封（schema_version/type）的副本。
	normalize() BackendFrame
	backendFrame()
}

// BackendInfo 标识一个自定义后端。
type BackendInfo struct {
	// Name 后端稳定标识（不透明字符串，如 "my-agent"）。
	Name string `json:"name"`
	// Version 后端版本字符串；缺省表示未声明版本（不表示无限制）。
	Version string `json:"version,omitempty"`
}

// ToolEffect 是一个工具的效果声明。
type ToolEffect struct {
	// Name 工具名（后端原生名，不透明字符串）。
	Name string `json:"name"`
	// Effects 该工具的效果集合，至少一项且不得重复。
	Effects []ToolEffectKind `json:"effects"`
}

// Hello 是握手帧：声明协议版本、能力、必需能力与工具效果。
//
// 它只是**声明**，不是证据（§27.7：缺证据的能力一律 false）：Negotiated.Report 里
// 每条能力的证据字符串固定为 "declared in hello frame (not independently probed)"，
// 由服务端决定是否采信。
type Hello struct {
	SchemaVersion   int                      `json:"schema_version"`
	Type            FrameType                `json:"type"`
	ProtocolVersion int                      `json:"protocol_version"`
	Backend         BackendInfo              `json:"backend"`
	Capabilities    []execbackend.Capability `json:"capabilities,omitempty"`
	Requires        []execbackend.Capability `json:"requires,omitempty"`
	Tools           []ToolEffect             `json:"tools,omitempty"`
}

// ObservationFrame 是一条观察记录帧，一一映射到 execbackend.Observation。
//
// kind 不含 exited：终结只能走 exit 帧（唯一终结帧）。
type ObservationFrame struct {
	SchemaVersion int                         `json:"schema_version"`
	Type          FrameType                   `json:"type"`
	Kind          execbackend.ObservationKind `json:"kind"`
	ObservedAt    time.Time                   `json:"observed_at"`
	ProviderRef   string                      `json:"provider_ref,omitempty"`
	Payload       json.RawMessage             `json:"payload,omitempty"`
}

// ExitFrame 是终结帧：携带 ExitResult，映射为 exited 观察。
type ExitFrame struct {
	SchemaVersion int                    `json:"schema_version"`
	Type          FrameType              `json:"type"`
	Result        execbackend.ExitResult `json:"result"`
}

// FrameType 实现 BackendFrame。
func (f Hello) FrameType() FrameType { return FrameHello }

// FrameType 实现 BackendFrame。
func (f ObservationFrame) FrameType() FrameType { return FrameObservation }

// FrameType 实现 BackendFrame。
func (f ExitFrame) FrameType() FrameType { return FrameExit }

func (f Hello) backendFrame()            {}
func (f ObservationFrame) backendFrame() {}
func (f ExitFrame) backendFrame()        {}

// normalize 写入正确信封后的副本（调用方不必自己填 schema_version/type）。
func (f Hello) normalize() BackendFrame {
	f.SchemaVersion, f.Type = ProtocolVersion, FrameHello
	return f
}

// normalize 写入正确信封后的副本。
func (f ObservationFrame) normalize() BackendFrame {
	f.SchemaVersion, f.Type = ProtocolVersion, FrameObservation
	return f
}

// normalize 写入正确信封后的副本。
func (f ExitFrame) normalize() BackendFrame {
	f.SchemaVersion, f.Type = ProtocolVersion, FrameExit
	return f
}

// Validate 校验 hello 帧：信封、后端标识、能力列表（已知且不重复）、工具效果。
//
// 不校验 protocol_version：版本协商是 Negotiate 的职责（见 Negotiate）。
func (h Hello) Validate() error {
	if err := validateEnvelope(h.SchemaVersion, h.Type, FrameHello); err != nil {
		return err
	}
	if strings.TrimSpace(h.Backend.Name) == "" {
		return invalidFrame("backend.name", "backend.name is required")
	}
	if err := validateCapabilityList(h.Capabilities, "capabilities"); err != nil {
		return err
	}
	if err := validateCapabilityList(h.Requires, "requires"); err != nil {
		return err
	}
	seen := make(map[string]int, len(h.Tools))
	for i, tool := range h.Tools {
		path := fmt.Sprintf("tools[%d]", i)
		if strings.TrimSpace(tool.Name) == "" {
			return invalidFrame(path+".name", "tool name is required")
		}
		if len(tool.Name) > MaxToolNameBytes {
			return invalidFrame(path+".name", "tool name is %d bytes, limit %d", len(tool.Name), MaxToolNameBytes)
		}
		if first, dup := seen[tool.Name]; dup {
			return invalidFrame(path+".name", "duplicate tool name (already declared at tools[%d])", first)
		}
		seen[tool.Name] = i
		if len(tool.Effects) == 0 {
			return invalidFrame(path+".effects", "effects must not be empty")
		}
		effects := make(map[ToolEffectKind]int, len(tool.Effects))
		for j, effect := range tool.Effects {
			if !effect.Valid() {
				return invalidFrame(fmt.Sprintf("%s.effects[%d]", path, j), "unknown tool effect %s", quoteToken(string(effect)))
			}
			if first, dup := effects[effect]; dup {
				return invalidFrame(fmt.Sprintf("%s.effects[%d]", path, j), "duplicate tool effect (already declared at effects[%d])", first)
			}
			effects[effect] = j
		}
	}
	return nil
}

// Validate 校验 observation 帧并映射到 execbackend.Observation 后调用其 Validate。
func (f ObservationFrame) Validate() error {
	if err := validateEnvelope(f.SchemaVersion, f.Type, FrameObservation); err != nil {
		return err
	}
	if !f.Kind.Valid() {
		return newProtocolError(CodeUnknownObservationKind, "kind", "unknown observation kind %s", quoteToken(string(f.Kind)))
	}
	if f.Kind.IsTerminal() {
		return newProtocolError(CodeUnknownObservationKind, "kind",
			"observation frame must not carry the terminal kind exited; use the exit frame")
	}
	if f.ObservedAt.IsZero() {
		// Go 的 time.Time 零值无法与"字段缺失"区分，必须显式拒绝，
		// 否则 schema 的 required: observed_at 在 Go 侧失效。
		return invalidFrame("observed_at", "observed_at is required")
	}
	_, err := f.Observation()
	return err
}

// Validate 校验 exit 帧并调用 execbackend.ExitResult.Validate。
func (f ExitFrame) Validate() error {
	if err := validateEnvelope(f.SchemaVersion, f.Type, FrameExit); err != nil {
		return err
	}
	_, err := f.Observation()
	return err
}

// Observation 把 observation 帧一一映射为 execbackend.Observation 并校验。
// 映射是无损的：kind/observed_at/provider_ref/payload 原样传递，不解释 payload 语义。
func (f ObservationFrame) Observation() (execbackend.Observation, error) {
	obs := execbackend.Observation{
		Kind:        f.Kind,
		ObservedAt:  f.ObservedAt,
		ProviderRef: f.ProviderRef,
		Payload:     f.Payload,
	}
	if err := obs.Validate(); err != nil {
		return execbackend.Observation{}, wrapExecbackendError(err, "observation")
	}
	return obs, nil
}

// Observation 把 exit 帧映射为终结观察（Kind=exited，Payload=ExitResult）。
//
// 返回的观察不带 ObservedAt：终结观察的时间由监督器/服务端盖章（§27.4）。
func (f ExitFrame) Observation() (execbackend.Observation, error) {
	if err := f.Result.Validate(); err != nil {
		return execbackend.Observation{}, wrapExecbackendError(err, "result")
	}
	payload, err := json.Marshal(f.Result)
	if err != nil {
		return execbackend.Observation{}, invalidFrame("result", "exit result could not be encoded as JSON")
	}
	obs := execbackend.Observation{Kind: execbackend.ObservationExited, Payload: payload}
	if err := obs.Validate(); err != nil {
		return execbackend.Observation{}, wrapExecbackendError(err, "result")
	}
	return obs, nil
}

// ---------------------------------------------------------------------------
// 服务端→后端帧
// ---------------------------------------------------------------------------

// HostFrame 是服务端→后端的一帧（start | approval_response | cancel）。
// 接口含本包私有方法，帧类型集合封闭。
type HostFrame interface {
	// FrameType 返回帧类型。
	FrameType() FrameType
	// Validate 校验帧字段。
	Validate() error
	// normalize 返回写入正确信封（schema_version/type）的副本。
	normalize() HostFrame
	hostFrame()
}

// IdentityActor 是执行身份里的 actor，镜像 execution-identity.schema.json 的
// actor{type,id,source}。
type IdentityActor struct {
	// Type actor 类型（user|agent|system|integration）。
	Type ActorType `json:"type"`
	// ID actor 标识（不透明字符串）。
	ID string `json:"id"`
	// Source 可选来源说明（如 desktop/cli/组件名）。
	Source string `json:"source,omitempty"`
}

// Identity 是 start 帧里的**只读**执行上下文，镜像
// backend/schemas/execution-identity.schema.json。
//
// 后端只能读它，不能回写：协议里没有任何携带身份的后端→服务端帧，出现这些键的
// 后端帧一律以 forbidden_field 拒绝（ReservedFieldNames）。
//
// run_id/attempt_id/agent_revision_id 用 string+omitempty 表示"可缺省/可 null"；
// 本协议的 start 帧只用于 Run 执行，Validate 要求四者非空。
type Identity struct {
	ProjectID       string        `json:"project_id"`
	RunID           string        `json:"run_id,omitempty"`
	AttemptID       string        `json:"attempt_id,omitempty"`
	AgentRevisionID string        `json:"agent_revision_id,omitempty"`
	Actor           IdentityActor `json:"actor"`
}

// ProcessSpec 是 start 帧里的进程控制参数（毫秒表示，便于跨语言）。
//
// 与 execbackend.ProcessControl 的差别是**没有 Env**：环境变量由监督器在 spawn 时
// 设置，可能含受控变量，不能进协议帧。
type ProcessSpec struct {
	// GracePeriodMS 软终止到强制终止的宽限期（毫秒），不得为负。
	GracePeriodMS int64 `json:"grace_period_ms"`
	// HardDeadline 硬截止时间；缺省表示服务端另行控制。
	HardDeadline *time.Time `json:"hard_deadline,omitempty"`
	// MaxOutputBytes 输出字节上限，不得为负；0 表示未设上限。
	MaxOutputBytes int64 `json:"max_output_bytes"`
}

// StartFrame 是启动帧：只读身份 + 冻结输入 + 工作副本路径 + 进程控制 + 所需能力。
type StartFrame struct {
	SchemaVersion int                      `json:"schema_version"`
	Type          FrameType                `json:"type"`
	Identity      Identity                 `json:"identity"`
	Input         execbackend.FrozenInput  `json:"input"`
	WorkDir       string                   `json:"work_dir"`
	Process       ProcessSpec              `json:"process"`
	Required      []execbackend.Capability `json:"required,omitempty"`
}

// ApprovalResponseFrame 是审批裁决帧。
type ApprovalResponseFrame struct {
	SchemaVersion int                          `json:"schema_version"`
	Type          FrameType                    `json:"type"`
	Decision      execbackend.ApprovalDecision `json:"decision"`
}

// CancelFrame 是取消请求帧。
type CancelFrame struct {
	SchemaVersion int                    `json:"schema_version"`
	Type          FrameType              `json:"type"`
	Mode          execbackend.CancelMode `json:"mode"`
}

// FrameType 实现 HostFrame。
func (f StartFrame) FrameType() FrameType { return FrameStart }

// FrameType 实现 HostFrame。
func (f ApprovalResponseFrame) FrameType() FrameType { return FrameApprovalResponse }

// FrameType 实现 HostFrame。
func (f CancelFrame) FrameType() FrameType { return FrameCancel }

func (f StartFrame) hostFrame()            {}
func (f ApprovalResponseFrame) hostFrame() {}
func (f CancelFrame) hostFrame()           {}

// normalize 写入正确信封后的副本。
func (f StartFrame) normalize() HostFrame {
	f.SchemaVersion, f.Type = ProtocolVersion, FrameStart
	return f
}

// normalize 写入正确信封后的副本。
func (f ApprovalResponseFrame) normalize() HostFrame {
	f.SchemaVersion, f.Type = ProtocolVersion, FrameApprovalResponse
	return f
}

// normalize 写入正确信封后的副本。
func (f CancelFrame) normalize() HostFrame {
	f.SchemaVersion, f.Type = ProtocolVersion, FrameCancel
	return f
}

// Validate 校验 identity：project_id 非空、actor 类型已知且 id 非空。
func (i Identity) Validate() error {
	if strings.TrimSpace(i.ProjectID) == "" {
		return invalidFrame("identity.project_id", "identity.project_id is required")
	}
	if !i.Actor.Type.Valid() {
		return invalidFrame("identity.actor.type", "unknown actor type %s", quoteToken(string(i.Actor.Type)))
	}
	if strings.TrimSpace(i.Actor.ID) == "" {
		return invalidFrame("identity.actor.id", "identity.actor.id is required")
	}
	return nil
}

// Validate 校验 start 帧。
//
// 复用 execbackend.StartRequest.Validate 作为唯一事实来源：身份四标识非空、冻结输入
// 的 snapshot_hash 格式、work_dir 绝对且已 Clean、grace/max_output 非负、required 均为
// 已知能力且不重复，全部与后端实现看到的一致。Env 不在帧里（留空）。
func (f StartFrame) Validate() error {
	if err := validateEnvelope(f.SchemaVersion, f.Type, FrameStart); err != nil {
		return err
	}
	if err := f.Identity.Validate(); err != nil {
		return err
	}
	req := execbackend.StartRequest{
		Run: execbackend.RunRef{
			ProjectID:       f.Identity.ProjectID,
			RunID:           f.Identity.RunID,
			AttemptID:       f.Identity.AttemptID,
			AgentRevisionID: f.Identity.AgentRevisionID,
		},
		Input:   f.Input,
		WorkDir: f.WorkDir,
		Process: execbackend.ProcessControl{
			GracePeriod:    time.Duration(f.Process.GracePeriodMS) * time.Millisecond,
			MaxOutputBytes: f.Process.MaxOutputBytes,
		},
		Required: f.Required,
	}
	if f.Process.HardDeadline != nil {
		req.Process.HardDeadline = *f.Process.HardDeadline
	}
	if err := req.Validate(); err != nil {
		return wrapExecbackendError(err, "start")
	}
	return nil
}

// Validate 校验 approval_response 帧：approval_id 非空。
func (f ApprovalResponseFrame) Validate() error {
	if err := validateEnvelope(f.SchemaVersion, f.Type, FrameApprovalResponse); err != nil {
		return err
	}
	if strings.TrimSpace(f.Decision.ApprovalID) == "" {
		return invalidFrame("decision.approval_id", "decision.approval_id is required")
	}
	return nil
}

// Validate 校验 cancel 帧：mode 必须是已知枚举。
func (f CancelFrame) Validate() error {
	if err := validateEnvelope(f.SchemaVersion, f.Type, FrameCancel); err != nil {
		return err
	}
	if !f.Mode.Valid() {
		return invalidFrame("mode", "unknown cancel mode %s", quoteToken(string(f.Mode)))
	}
	return nil
}

// ---------------------------------------------------------------------------
// 错误
// ---------------------------------------------------------------------------

// 协议错误码（稳定，可 errors.As 取出 *ProtocolError 后比较）。
const (
	// CodeFrameTooLarge 帧整行超过 MaxFrameBytes，或控制帧超过 MaxControlFrameBytes。
	CodeFrameTooLarge = "frame_too_large"
	// CodeInvalidFrame JSON 非法/多值/尾随垃圾、未知字段、缺必填、类型错、枚举外值、
	// 结构校验失败（含映射到 execbackend 后的校验失败）。
	CodeInvalidFrame = "invalid_frame"
	// CodeUnknownFrameType type 存在但不在封闭枚举内。
	CodeUnknownFrameType = "unknown_frame_type"
	// CodeSchemaVersionMismatch schema_version 存在但不等于 ProtocolVersion。
	CodeSchemaVersionMismatch = "schema_version_mismatch"
	// CodeForbiddenField 后端→服务端帧出现保留字段名（payload 子树除外）。
	CodeForbiddenField = "forbidden_field"
	// CodeUnknownCapability hello.capabilities 含未知能力值。
	CodeUnknownCapability = "unknown_capability"
	// CodeUnknownRequiredCapability hello.requires 含未知能力值——拒绝启动。
	CodeUnknownRequiredCapability = "unknown_required_capability"
	// CodeDuplicateCapability capabilities/requires 出现重复值。
	CodeDuplicateCapability = "duplicate_capability"
	// CodeProtocolVersionMismatch Negotiate：protocol_version 不等于 ProtocolVersion。
	CodeProtocolVersionMismatch = "protocol_version_mismatch"
	// CodeUnknownObservationKind observation.kind 不在封闭枚举内（含只允许出现在 exit
	// 帧里的终结值 exited）。
	CodeUnknownObservationKind = "unknown_observation_kind"
)

// ProtocolError 是自定义执行器协议的领域错误。
//
// 字段语义：
//   - Code 必填，取值见 Code* 常量。
//   - Field 指出出错的字段路径（如 "kind"、"backend.name"、"tools[0].effects[1]"）。
//   - Message 只含字段名、长度与协议枚举短标识符，**不含**原始帧字节或 payload 内容。
//   - Err 是底层原因（如 execbackend 的校验错误），通过 Unwrap 暴露。
type ProtocolError struct {
	Code    string
	Field   string
	Message string
	Err     error
}

// Error 实现 error；nil 接收者安全。
func (e *ProtocolError) Error() string {
	if e == nil {
		return "<nil>"
	}
	msg := e.Message
	if msg == "" {
		msg = e.Code
	}
	var b strings.Builder
	b.WriteString("custom: ")
	b.WriteString(msg)
	b.WriteString(" [code=")
	b.WriteString(e.Code)
	if e.Field != "" {
		b.WriteString(" field=")
		b.WriteString(e.Field)
	}
	b.WriteString("]")
	if e.Err != nil {
		b.WriteString(": ")
		b.WriteString(e.Err.Error())
	}
	return b.String()
}

// Unwrap 暴露底层原因；nil 接收者返回 nil。
func (e *ProtocolError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Is 支持 errors.Is(err, &ProtocolError{Code: ...})：同 code 视为同类错误，
// 不比较 Field/Message 细节。
func (e *ProtocolError) Is(target error) bool {
	if e == nil || target == nil {
		return false
	}
	t, ok := target.(*ProtocolError)
	return ok && t != nil && t.Code == e.Code
}

// newProtocolError 构造 *ProtocolError。
func newProtocolError(code, field, format string, args ...any) *ProtocolError {
	return &ProtocolError{Code: code, Field: field, Message: fmt.Sprintf(format, args...)}
}

// invalidFrame 构造 Code=invalid_frame 的错误。
func invalidFrame(field, format string, args ...any) *ProtocolError {
	return newProtocolError(CodeInvalidFrame, field, format, args...)
}

// quoteToken 渲染错误文本里的单个标识符：超过 maxTokenEchoBytes 只报长度，
// 避免把任意帧内容写进错误。
func quoteToken(s string) string {
	if len(s) > maxTokenEchoBytes {
		return fmt.Sprintf("<%d bytes>", len(s))
	}
	return strconv.Quote(s)
}

// validateEnvelope 校验信封：schema_version == ProtocolVersion 且 type 与期望一致。
func validateEnvelope(schemaVersion int, got, want FrameType) error {
	if schemaVersion != ProtocolVersion {
		return newProtocolError(CodeSchemaVersionMismatch, "schema_version",
			"unsupported schema_version: host requires %d", ProtocolVersion)
	}
	if got != want {
		return invalidFrame("type", "frame type %s does not match the %s frame", quoteToken(string(got)), string(want))
	}
	return nil
}

// validateCapabilityList 校验能力列表：每个值必须是已知枚举、不得重复。
//
// 未知值一律拒绝（不是忽略）：协议 v1 的帧类型集合与能力集合都是封闭的，静默丢掉一个
// 服务端不认识的能力会让后端以为协商成功，而服务端从未承认它；requires 里的未知值
// 更必须拒绝启动（CodeUnknownRequiredCapability，见 §28 T4.03.a）。
func validateCapabilityList(caps []execbackend.Capability, field string) error {
	seen := make(map[execbackend.Capability]int, len(caps))
	for i, c := range caps {
		path := fmt.Sprintf("%s[%d]", field, i)
		if !c.Valid() {
			code := CodeUnknownCapability
			if field == "requires" {
				code = CodeUnknownRequiredCapability
			}
			return newProtocolError(code, path, "unknown capability %s in %s", quoteToken(string(c)), field)
		}
		if first, dup := seen[c]; dup {
			return newProtocolError(CodeDuplicateCapability, path,
				"duplicate capability (already declared at %s[%d])", field, first)
		}
		seen[c] = i
	}
	return nil
}

// wrapExecbackendError 把 execbackend 的校验错误映射为 *ProtocolError，保留底层原因
// 以便 errors.As 仍能取出 *execbackend.Error。
func wrapExecbackendError(err error, field string) error {
	var ee *execbackend.Error
	if errors.As(err, &ee) {
		f := ee.Field
		if f == "" {
			f = field
		}
		return &ProtocolError{Code: CodeInvalidFrame, Field: f,
			Message: "frame failed execbackend validation: " + ee.Message, Err: err}
	}
	return &ProtocolError{Code: CodeInvalidFrame, Field: field,
		Message: "frame failed execbackend validation", Err: err}
}

// ---------------------------------------------------------------------------
// 保留字段（后端不得自报序号/身份/审计）
// ---------------------------------------------------------------------------

// reservedFieldNames 是后端→服务端帧里禁止出现的字段名（小写比较）。
//
// 这些键一旦出现，就意味着后端可以自报序号、身份或审计事实，违反 §27.4
// 「服务端填 identity/ID/time，不能直接接受 CLI 自报 project/sequence 或审计」。
// event_type 与 execbackend/types_test.go 的 Observation 结构防线一致（领域事件类型
// 由服务端映射）。
var reservedFieldNames = map[string]struct{}{
	"project_seq":       {},
	"run_seq":           {},
	"sequence":          {},
	"seq":               {},
	"event_id":          {},
	"event_type":        {},
	"occurred_at":       {},
	"identity":          {},
	"actor":             {},
	"audit":             {},
	"project_id":        {},
	"run_id":            {},
	"attempt_id":        {},
	"agent_revision_id": {},
}

// ReservedFieldNames 返回全部保留字段名（字典序），供服务端映射与测试复用。
func ReservedFieldNames() []string {
	names := make([]string, 0, len(reservedFieldNames))
	for name := range reservedFieldNames {
		names = append(names, name)
	}
	sortStrings(names)
	return names
}

// IsReservedFieldName 报告 name 是否为保留字段名（不区分大小写）。
func IsReservedFieldName(name string) bool {
	_, banned := reservedFieldNames[strings.ToLower(strings.TrimSpace(name))]
	return banned
}

// FrameJSONFields 返回 v 的字段集合视图（json tag → 是否 omitempty），供漂移测试对照
// custom-backend.schema.json 的 properties/required。
//
// v 必须是本包定义的帧结构体（hello/observation/exit/start/approval_response/cancel）
// 或嵌套结构 Identity/IdentityActor/BackendInfo/ToolEffect/ProcessSpec，或 execbackend 的
// ExitResult/Usage/ApprovalDecision/FrozenInput；未知类型返回 nil。
func FrameJSONFields(v any) map[string]bool {
	switch v.(type) {
	case Hello:
		return structJSONFields(reflect.TypeOf(Hello{}))
	case ObservationFrame:
		return structJSONFields(reflect.TypeOf(ObservationFrame{}))
	case ExitFrame:
		return structJSONFields(reflect.TypeOf(ExitFrame{}))
	case StartFrame:
		return structJSONFields(reflect.TypeOf(StartFrame{}))
	case ApprovalResponseFrame:
		return structJSONFields(reflect.TypeOf(ApprovalResponseFrame{}))
	case CancelFrame:
		return structJSONFields(reflect.TypeOf(CancelFrame{}))
	case Identity:
		return structJSONFields(reflect.TypeOf(Identity{}))
	case IdentityActor:
		return structJSONFields(reflect.TypeOf(IdentityActor{}))
	case BackendInfo:
		return structJSONFields(reflect.TypeOf(BackendInfo{}))
	case ToolEffect:
		return structJSONFields(reflect.TypeOf(ToolEffect{}))
	case ProcessSpec:
		return structJSONFields(reflect.TypeOf(ProcessSpec{}))
	case execbackend.ExitResult:
		return structJSONFields(reflect.TypeOf(execbackend.ExitResult{}))
	case execbackend.Usage:
		return structJSONFields(reflect.TypeOf(execbackend.Usage{}))
	case execbackend.ApprovalDecision:
		return structJSONFields(reflect.TypeOf(execbackend.ApprovalDecision{}))
	case execbackend.FrozenInput:
		return structJSONFields(reflect.TypeOf(execbackend.FrozenInput{}))
	default:
		return nil
	}
}

// structJSONFields 反射出结构体的 json tag 集合（值 = 是否 omitempty）。
func structJSONFields(typ reflect.Type) map[string]bool {
	fields := make(map[string]bool, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		tag := field.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		parts := strings.Split(tag, ",")
		name := parts[0]
		if name == "" || name == "-" {
			continue
		}
		omitempty := false
		for _, opt := range parts[1:] {
			if opt == "omitempty" {
				omitempty = true
			}
		}
		fields[name] = omitempty
	}
	return fields
}

// checkForbiddenFields 扫描后端帧的保留字段名。
//
// 边界（T4.03.a 裁定）：只检查信封与协议已定义的结构；observation 帧**顶层**的
// payload 子树例外——它是后端原生 JSON，原样保留、由服务端映射（服务端不会把 payload
// 里的键当身份用）。其余任何层级（含嵌套对象与数组）出现保留名都拒绝。
func checkForbiddenFields(env map[string]any) error {
	if field, found := scanReserved(env, "", true); found {
		return newProtocolError(CodeForbiddenField, field,
			"backend frame must not carry reserved field (server assigns sequence/identity/audit)")
	}
	return nil
}

// scanReserved 递归扫描保留字段名，返回第一个命中的路径（键按字典序，保证可复现）。
func scanReserved(value any, path string, topLevel bool) (string, bool) {
	switch v := value.(type) {
	case map[string]any:
		for _, key := range sortedKeys(v) {
			lower := strings.ToLower(strings.TrimSpace(key))
			if _, banned := reservedFieldNames[lower]; banned {
				return joinPath(path, key), true
			}
			if topLevel && lower == "payload" {
				continue // 后端原生负载：原样保留，不扫描
			}
			if found, ok := scanReserved(v[key], joinPath(path, key), false); ok {
				return found, true
			}
		}
	case []any:
		for i, item := range v {
			if found, ok := scanReserved(item, fmt.Sprintf("%s[%d]", path, i), false); ok {
				return found, true
			}
		}
	}
	return "", false
}

// checkCanonicalKeys 拒绝不是全小写拼写的键（observation 帧顶层 payload 子树例外，
// 与保留字段扫描同一边界）。
//
// encoding/json 按字段名大小写不敏感匹配：同一帧里 "kind" 与 "KIND" 会解进同一个
// 字段（后出现者胜出），而信封视图按精确键读到的是 "kind"——两个视图对同一帧的理解
// 不一致（例如长度上限按信封里的 kind 选，结构体里却是另一个 kind）。v1 的字段名
// 全部是小写 snake_case，非小写拼写只可能是走私尝试或缺陷，一律拒绝。
// 在保留字段扫描之后执行：Sequence 这类保留名的大小写变体仍报 forbidden_field。
func checkCanonicalKeys(value any, path string, topLevel bool) error {
	switch v := value.(type) {
	case map[string]any:
		for _, key := range sortedKeys(v) {
			keyPath := joinPath(path, key)
			if key != strings.ToLower(key) {
				field := keyPath
				if len(field) > maxTokenEchoBytes {
					field = ""
				}
				return invalidFrame(field, "field name %s is not lower case; v1 field names are lower-case snake_case", quoteToken(key))
			}
			if topLevel && key == "payload" {
				continue // 后端原生负载：键名由后端决定，原样保留
			}
			if err := checkCanonicalKeys(v[key], keyPath, false); err != nil {
				return err
			}
		}
	case []any:
		for i, item := range v {
			if err := checkCanonicalKeys(item, fmt.Sprintf("%s[%d]", path, i), false); err != nil {
				return err
			}
		}
	}
	return nil
}

// sortedKeys 返回 map 的键按字典序排列的结果（错误定位可复现）。
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sortStrings(keys)
	return keys
}

// sortStrings 就地插入排序（少量键，避免额外依赖）。
func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

// joinPath 拼接字段路径。
func joinPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

// ---------------------------------------------------------------------------
// 解码
// ---------------------------------------------------------------------------

// DecodeBackendFrame 解码一行后端→服务端帧（无状态）。
//
// 顺序（冻结）：长度上限 → 严格 JSON（单值、无尾随垃圾）→ 保留字段扫描 → 键名小写检查 →
// schema_version → type（封闭枚举 + 方向）→ 控制帧上限 → 严格字段解码 →
// 帧校验（含映射到 execbackend.Observation/ExitResult 的校验）。
//
// 不做协商：hello 的 protocol_version 由 Negotiate 拒绝（见 Negotiate）。
// 不保留任何状态：同一行解码结果永远相同。
func DecodeBackendFrame(line []byte) (BackendFrame, error) {
	env, err := decodeEnvelope(line, frameDirectionBackend)
	if err != nil {
		return nil, err
	}
	switch FrameType(stringValue(env["type"])) {
	case FrameHello:
		var f Hello
		if err := decodeStrict(line, &f); err != nil {
			return nil, err
		}
		if err := f.Validate(); err != nil {
			return nil, err
		}
		return f, nil
	case FrameObservation:
		var f ObservationFrame
		if err := decodeStrict(line, &f); err != nil {
			return nil, err
		}
		if err := f.Validate(); err != nil {
			return nil, err
		}
		return f, nil
	case FrameExit:
		var f ExitFrame
		if err := decodeStrict(line, &f); err != nil {
			return nil, err
		}
		if err := f.Validate(); err != nil {
			return nil, err
		}
		return f, nil
	default:
		// decodeEnvelope 已保证 type 合法且方向正确。
		return nil, newProtocolError(CodeUnknownFrameType, "type", "unknown backend frame type")
	}
}

// DecodeHostFrame 解码一行服务端→后端帧（无状态）。
//
// 与 DecodeBackendFrame 的区别：不扫描保留字段——start.identity 里的 project_id/
// run_id/actor 等是服务端**发给**后端的只读执行上下文，不是后端自报。
func DecodeHostFrame(line []byte) (HostFrame, error) {
	env, err := decodeEnvelope(line, frameDirectionHost)
	if err != nil {
		return nil, err
	}
	switch FrameType(stringValue(env["type"])) {
	case FrameStart:
		var f StartFrame
		if err := decodeStrict(line, &f); err != nil {
			return nil, err
		}
		if err := f.Validate(); err != nil {
			return nil, err
		}
		return f, nil
	case FrameApprovalResponse:
		var f ApprovalResponseFrame
		if err := decodeStrict(line, &f); err != nil {
			return nil, err
		}
		if err := f.Validate(); err != nil {
			return nil, err
		}
		return f, nil
	case FrameCancel:
		var f CancelFrame
		if err := decodeStrict(line, &f); err != nil {
			return nil, err
		}
		if err := f.Validate(); err != nil {
			return nil, err
		}
		return f, nil
	default:
		return nil, newProtocolError(CodeUnknownFrameType, "type", "unknown host frame type")
	}
}

// frameDirection 是帧方向（用于区分保留字段扫描与方向校验）。
type frameDirection int

const (
	frameDirectionBackend frameDirection = iota
	frameDirectionHost
)

// decodeEnvelope 解析并校验信封，返回原始键值视图。
func decodeEnvelope(line []byte, dir frameDirection) (map[string]any, error) {
	if len(line) > MaxFrameBytes {
		return nil, newProtocolError(CodeFrameTooLarge, "",
			"frame is %d bytes, limit %d", len(line), MaxFrameBytes)
	}
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return nil, invalidFrame("", "frame line is empty")
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	var env map[string]any
	if err := dec.Decode(&env); err != nil {
		return nil, jsonErrorToProtocolError(err)
	}
	if err := requireEOF(dec); err != nil {
		return nil, err
	}
	if env == nil {
		return nil, invalidFrame("", "frame must be a JSON object")
	}

	if dir == frameDirectionBackend {
		if err := checkForbiddenFields(env); err != nil {
			return nil, err
		}
	}
	if err := checkCanonicalKeys(env, "", true); err != nil {
		return nil, err
	}

	rawVersion, ok := env["schema_version"]
	if !ok {
		return nil, invalidFrame("schema_version", "schema_version is required")
	}
	version, ok := rawVersion.(json.Number)
	if !ok || version.String() != strconv.Itoa(ProtocolVersion) {
		return nil, newProtocolError(CodeSchemaVersionMismatch, "schema_version",
			"unsupported schema_version: host requires %d", ProtocolVersion)
	}

	rawType, ok := env["type"]
	if !ok {
		return nil, invalidFrame("type", "type is required")
	}
	name, ok := rawType.(string)
	if !ok {
		return nil, invalidFrame("type", "type must be a string")
	}
	ft := FrameType(name)
	if !ft.Valid() {
		return nil, newProtocolError(CodeUnknownFrameType, "type", "unknown frame type %s", quoteToken(name))
	}
	switch dir {
	case frameDirectionBackend:
		if !ft.IsBackendFrame() {
			return nil, invalidFrame("type", "frame type %s is host→backend, not backend→host", quoteToken(name))
		}
	case frameDirectionHost:
		if !ft.IsHostFrame() {
			return nil, invalidFrame("type", "frame type %s is backend→host, not host→backend", quoteToken(name))
		}
	}

	if limit := frameLimitBytes(ft, rawObservationKind(env)); len(line) > limit {
		return nil, newProtocolError(CodeFrameTooLarge, "",
			"control frame is %d bytes, limit %d", len(line), limit)
	}
	return env, nil
}

// frameLimitBytes 返回该帧整行的字节上限：控制帧 64 KiB，其余（observation）MaxFrameBytes。
func frameLimitBytes(t FrameType, kind execbackend.ObservationKind) int {
	if t.IsControlFrame() {
		return MaxControlFrameBytes
	}
	if t == FrameObservation && kind.IsControlKind() {
		return MaxControlFrameBytes
	}
	return MaxFrameBytes
}

// rawObservationKind 从信封里读 observation 的 kind（只为挑长度上限；非法值留给
// 逐字段解码报错）。
func rawObservationKind(env map[string]any) execbackend.ObservationKind {
	kind, _ := env["kind"].(string)
	return execbackend.ObservationKind(kind)
}

// stringValue 返回信封里的字符串值；非字符串返回空串。
func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

// requireEOF 拒绝一行里的多个 JSON 值或尾随垃圾。
func requireEOF(dec *json.Decoder) error {
	var extra json.RawMessage
	if err := dec.Decode(&extra); err != io.EOF {
		return invalidFrame("", "frame line must contain exactly one JSON value")
	}
	return nil
}

// decodeStrict 严格解码：未知字段拒绝（DisallowUnknownFields）。
func decodeStrict(line []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(bytes.TrimSpace(line)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return jsonErrorToProtocolError(err)
	}
	return nil
}

// jsonErrorToProtocolError 把 encoding/json 的错误映射为 *ProtocolError。
// 只保留字段名、偏移与类型，不回显帧内容。
func jsonErrorToProtocolError(err error) error {
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return invalidFrame("", "frame is not valid JSON (at byte %d)", syntaxErr.Offset)
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return invalidFrame(typeErr.Field, "field has wrong JSON type (want %s)", typeErr.Type.String())
	}
	if field, ok := unknownFieldFromError(err); ok {
		return invalidFrame(field, "unknown field is not allowed in a v1 frame")
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return invalidFrame("", "frame is truncated JSON")
	}
	return &ProtocolError{Code: CodeInvalidFrame, Message: "frame could not be decoded", Err: err}
}

// unknownFieldFromError 从 DisallowUnknownFields 的错误文本里取出字段名。
func unknownFieldFromError(err error) (string, bool) {
	const prefix = `json: unknown field "`
	msg := err.Error()
	if !strings.HasPrefix(msg, prefix) {
		return "", false
	}
	name := strings.TrimSuffix(strings.TrimPrefix(msg, prefix), `"`)
	if name == "" || len(name) > maxTokenEchoBytes {
		return "", false
	}
	return name, true
}

// ---------------------------------------------------------------------------
// 编码
// ---------------------------------------------------------------------------

// EncodeBackendFrame 编码一帧后端→服务端帧：一行 JSON + 末尾 "\n"。
//
// 信封（schema_version/type）由本函数按帧类型写入，调用方不必自己填；帧字段先校验
// 再编码，超长返回 frame_too_large（T4.03.c 会据此终止进程）。
func EncodeBackendFrame(f BackendFrame) ([]byte, error) {
	if f == nil {
		return nil, invalidFrame("", "frame is nil")
	}
	t := f.FrameType()
	if !t.Valid() || !t.IsBackendFrame() {
		return nil, invalidFrame("type", "not a backend→host frame type")
	}
	normalized := f.normalize()
	if err := normalized.Validate(); err != nil {
		return nil, err
	}
	kind := execbackend.ObservationKind("")
	if obs, ok := normalized.(ObservationFrame); ok {
		kind = obs.Kind
	}
	return marshalFrame(normalized, t, kind)
}

// EncodeHostFrame 编码一帧服务端→后端帧：一行 JSON + 末尾 "\n"。
//
// 信封由本函数写入；start 帧按控制帧上限（64 KiB）校验。
func EncodeHostFrame(f HostFrame) ([]byte, error) {
	if f == nil {
		return nil, invalidFrame("", "frame is nil")
	}
	t := f.FrameType()
	if !t.Valid() || !t.IsHostFrame() {
		return nil, invalidFrame("type", "not a host→backend frame type")
	}
	normalized := f.normalize()
	if err := normalized.Validate(); err != nil {
		return nil, err
	}
	return marshalFrame(normalized, t, "")
}

// marshalFrame 编码并检查长度上限。
func marshalFrame(frame any, t FrameType, kind execbackend.ObservationKind) ([]byte, error) {
	raw, err := json.Marshal(frame)
	if err != nil {
		return nil, &ProtocolError{Code: CodeInvalidFrame, Field: "type",
			Message: "frame could not be encoded as JSON", Err: err}
	}
	raw = append(raw, '\n')
	if limit := frameLimitBytes(t, kind); len(raw) > limit {
		return nil, newProtocolError(CodeFrameTooLarge, "",
			"encoded frame is %d bytes, limit %d", len(raw), limit)
	}
	return raw, nil
}

// ---------------------------------------------------------------------------
// 协商
// ---------------------------------------------------------------------------

// capabilityEvidence 是 hello 声明能力的证据字符串：声明不是探测，服务端可据此拒绝采信。
const capabilityEvidence = "declared in hello frame (not independently probed)"

// capabilitySource 是 Negotiated.Report.Source 的固定取值。
const capabilitySource = "custom:hello:declared"

// Negotiated 是一次握手的产物。
//
// Report 可直接交给 execbackend.CheckRequired/Backend.Prepare 复用（与 fake、真实
// adapter 的失败 code 与 Missing 语义一致）；ProbedAt 留零值——协商是纯函数，由调用方
// 在采用该报告时盖章。
type Negotiated struct {
	// BackendName 后端标识（hello.backend.name）。
	BackendName string
	// BackendVersion 后端版本（hello.backend.version，可为空）。
	BackendVersion string
	// ProtocolVersion 协商后的协议版本（等于 ProtocolVersion）。
	ProtocolVersion int
	// Capabilities 后端声明的能力（保持声明顺序，已保证已知且不重复）。
	Capabilities []execbackend.Capability
	// Requires 后端要求服务端具备的能力（保持声明顺序）。
	Requires []execbackend.Capability
	// Tools 工具效果声明。
	Tools []ToolEffect
	// Required 本次 Run 要求的能力（保持输入顺序）。
	Required []execbackend.Capability
	// Report 由 hello 声明构造的能力报告，供 CheckRequired/Prepare 复用。
	Report execbackend.CapabilityReport
}

// Has 报告协商结果里后端是否声明了能力 c（未声明的能力一律为 false，§27.7）。
func (n Negotiated) Has(c execbackend.Capability) bool {
	return n.Report.Has(c)
}

// Negotiate 校验握手并核对本次 Run 的能力需求。
//
// 判定顺序（冻结）：
//  1. h.Validate()：信封、backend.name、capabilities/requires（已知且不重复）、tools。
//  2. protocol_version != ProtocolVersion → protocol_version_mismatch（拒绝启动）。
//  3. requires 里的未知值已在第 1 步以 unknown_required_capability 拒绝——未知的
//     "必需能力"意味着服务端无法判断后端到底要什么，必须拒绝而不是假装满足。
//  4. 本次 required 未被 hello.capabilities 声明（含枚举外值）→
//     execbackend.NewCapabilityUnavailable(missing...)，与 fake/真实 adapter 的
//     Prepare 失败形状一致（errors.Is(err, execbackend.ErrCapabilityUnavailable)）。
func Negotiate(h Hello, required []execbackend.Capability) (Negotiated, error) {
	if err := h.Validate(); err != nil {
		return Negotiated{}, err
	}
	if h.ProtocolVersion != ProtocolVersion {
		return Negotiated{}, newProtocolError(CodeProtocolVersionMismatch, "protocol_version",
			"backend speaks protocol_version %d, host supports %d", h.ProtocolVersion, ProtocolVersion)
	}
	report := execbackend.CapabilityReport{
		Backend:  h.Backend.Name,
		Version:  h.Backend.Version,
		Source:   capabilitySource,
		Evidence: make(map[execbackend.Capability]string, len(h.Capabilities)),
	}
	for _, c := range h.Capabilities {
		report.Evidence[c] = capabilityEvidence
	}
	if err := execbackend.CheckRequired(report, required); err != nil {
		return Negotiated{}, err
	}
	return Negotiated{
		BackendName:     h.Backend.Name,
		BackendVersion:  h.Backend.Version,
		ProtocolVersion: ProtocolVersion,
		Capabilities:    append([]execbackend.Capability(nil), h.Capabilities...),
		Requires:        append([]execbackend.Capability(nil), h.Requires...),
		Tools:           append([]ToolEffect(nil), h.Tools...),
		Required:        append([]execbackend.Capability(nil), required...),
		Report:          report,
	}, nil
}
