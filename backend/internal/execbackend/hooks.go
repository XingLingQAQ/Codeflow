// Package execbackend - Hook port and execution policy for the tool and Run
// lifecycle hooks (T1.07.b group 1, plan §15 T1.07 / §28 T1.07.b).
//
// 本文件定义执行后端与 hooks 子系统之间的端口。端口放在 execbackend，因为本包
// 只允许依赖标准库（见 types.go 包注释）：hooks 包 import run，execbackend 反向
// import 会形成环。服务端适配器（internal/runhooks）实现 HookPort，把这里的规范
// 请求翻译成 hooks 的 payload 契约并触发真实 hook 管理器。
//
// # 冻结语义
//
//   - PreToolUse 在工具请求进入 Guard 之前触发；hook 放行之后 Guard 仍然执行，
//     hook 的放行永远不能推翻 Guard/policy 的拒绝（Guard 由 execute 内部负责，
//     RunTool 只保证顺序：AuthorizeToolCall → execute）。
//   - before 类（PreToolUse / RunStart）被拒绝 = 操作一次都不执行；端口缺失
//     （nil）同样拒绝：接线漏了不能变成放行（失败即关闭）。
//   - after 类（PostToolUse / RunFinish）失败只产生警告，绝不改变调用方流程、
//     绝不重跑已经执行的操作：报告结果不是重放。
//   - payload 必须带执行身份与请求指纹；指纹只从 {tool, 规范化 arguments} 计算，
//     键序/空白不同的等价 JSON 得到同一指纹。
//   - 一个事件只有一个触发记录：去重键（<hook>:<event_id>）由适配器（runhooks）
//     持有的单飞记忆保证；本层保证同一请求的指纹稳定，且拒绝路径根本不会触达端口。
//
// # 警告的去向
//
// hook 警告的持久化（outbox / Run 时间线）需要新的事件类型（封闭的
// run.ExecutionEventTypes 里没有），不属于 T1.07.b 第 1 组：警告今天走
// HookWarningSink（nil sink = 一行固定格式日志）加上 hooks 管理器已有的审计记录
// （失败写 OutcomeFailure）。接线方（T1.04/T1.13.b）必须传非 nil 端口与非 nil
// sink，才能拿到可持久化的警告。
package execbackend

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"strconv"
	"strings"
	"unicode/utf8"
)

// HookPoint* 是端口服务的四个 hook 点名字。它们是 hooks.HookType 的线上取值
// （hook_pre_tool_use / hook_post_tool_use / hook_run_start / hook_run_finish），
// 在这里以字符串复刻：execbackend 不得 import hooks（见文件头），runhooks 的
// 测试断言两边相等（TestHookPointNamesMatchHooksTypes）。
const (
	// HookPointPreToolUse：工具请求进入 Guard 之前。
	HookPointPreToolUse = "hook_pre_tool_use"
	// HookPointPostToolUse：工具结果净化（脱敏）之后。
	HookPointPostToolUse = "hook_post_tool_use"
	// HookPointRunStart：Run 被认领、进程启动之前。
	HookPointRunStart = "hook_run_start"
	// HookPointRunFinish：Run 到达终态之后。
	HookPointRunFinish = "hook_run_finish"
)

// ToolCallResult.Status 的封闭取值（与 hooks.ToolHookResultStatus 同值）。
const (
	// ToolResultStatusOK：工具成功返回。
	ToolResultStatusOK = "ok"
	// ToolResultStatusError：工具报错或非零退出。
	ToolResultStatusError = "error"
	// ToolResultStatusDenied：工具没有执行（被拒绝或审批未通过）。
	ToolResultStatusDenied = "denied"
)

// MaxHookEventIDBytes 是端口接受的 event ID / Run 标识上限，复刻
// run.MaxIdentityIDLength（128）与 hooks.payloadIDMaxBytes。execbackend 只依赖
// 标准库，所以在这里写明数字；hooks 的 payload.Validate 是最终权威，runhooks
// 会再校验一次。
const MaxHookEventIDBytes = 128

// HookPort 是执行后端把工具与 Run 生命周期事件交给服务端 hook 子系统的端口。
//
// 实现者（服务端适配器 runhooks.Port）必须：
//   - before 类返回非 nil = 拒绝（工具/进程不得执行）；
//   - after 类返回错误 = 只产生警告（调用方已经完成的操作不受影响）；
//   - 同一个事件（hook + event_id）只触发一次（由实现持有的单飞记忆保证）；
//   - 并发安全（服务端可能同时跑多个 Run）。
//
// nil 端口不是"没有 hook"：Authorize* 一律按拒绝处理（失败即关闭），
// Report* 只产生一条警告。
type HookPort interface {
	// BeforeTool 在工具请求进入 Guard 之前触发；非 nil = 拒绝这次工具调用。
	BeforeTool(ctx context.Context, req ToolCallRequest) error
	// AfterTool 在工具结果净化之后触发；只产生警告，永远不能重跑工具。
	AfterTool(ctx context.Context, req ToolCallRequest, result ToolCallResult) error
	// BeforeRunStart 在 Run 被认领（starting、attempt 已创建）之后、进程启动
	// 之前触发；非 nil = 不启动进程。
	BeforeRunStart(ctx context.Context, req RunLifecycleRequest) error
	// AfterRunFinish 在 Run 到达终态之后触发；只产生警告，Run 终态不因它改变。
	AfterRunFinish(ctx context.Context, req RunLifecycleRequest) error
}

// ToolCallRequest 是一次工具调用的规范请求（PreToolUse / PostToolUse 共用）。
//
// 字段全部来自服务端权威资源：Run 是执行身份（§27.1），EventID 是服务端分配的
// 事件 ID（pre 是 tool.requested 事件、post 是结果事件的 ID），ToolCallID 是后端
// 原生工具调用 ID（ProviderRef），Fingerprint 是规范化请求的 hash（与守卫这次
// 调用的 policy.Request.Fingerprint 同值）。Arguments 是已经脱敏的请求参数。
type ToolCallRequest struct {
	// Run 是执行身份（ProjectID/RunID/AttemptID/AgentRevisionID 都不透明字符串）。
	Run RunRef
	// Backend 是后端稳定标识（如 "claude_code"）。
	Backend string
	// EventID 是服务端分配的事件 ID（去重键的一半）。
	EventID string
	// ToolCallID 是后端原生工具调用 ID（ProviderRef），≤ MaxProviderRefBytes。
	ToolCallID string
	// Tool 是工具名。
	Tool string
	// Arguments 是脱敏后的工具请求参数（JSON），≤ MaxControlFrameBytes。
	Arguments json.RawMessage
	// Fingerprint 是 ToolRequestFingerprint(Tool, Arguments) 的结果。
	Fingerprint string
}

// ToolCallResult 是 PostToolUse 看到的结果摘要。
//
// 它只携带引用，不携带原文：OutputRef 指向已经脱敏、存在服务端存储里的输出
// （spool 片段/摘要），原始 stdout/stderr 绝不进入 hook payload。ExitCode 为 nil
// 表示后端没有提供退出码，而不是 0。
type ToolCallResult struct {
	// Status 是 ok / error / denied（封闭取值，见 ToolResultStatus*）。
	Status string
	// ExitCode 是进程/工具退出码；nil 表示未知。
	ExitCode *int
	// OutputRef 是脱敏输出的引用（spool 键或摘要），可以为空。
	OutputRef string
	// Truncated 表示输出被截断（有界 spool）。
	Truncated bool
}

// RunLifecycleRequest 是一次 Run 生命周期 hook 的规范请求。
type RunLifecycleRequest struct {
	// Run 是执行身份。project_id 与 run_id 必填；RunStart 还需要 attempt 与
	// agent_revision（认领后已存在），RunFinish 允许缺 attempt：排队阶段就被取消或
	// 到期的 Run 没有 attempt（CA-1），它的结束 hook 照样要触发。
	Run RunRef
	// Backend 是后端稳定标识。
	Backend string
	// EventID 是服务端分配的事件 ID（start：scheduler.claimed；finish：终态事件）。
	EventID string
	// Status 是迁移后的 Run 状态（start 必须是 starting；finish 必须是终态）。
	Status string
	// PreviousStatus 是迁移前的状态，可缺省（空 = 生产者没有记录）。
	PreviousStatus string
}

// HookWarning 是一次 hook 失败的报告。Hook 是 HookPoint* 之一，EventID 是触发
// 这个 hook 的事件 ID；Err 是失败原因（绝不包含 Arguments / 输出内容）。
type HookWarning struct {
	Hook    string
	EventID string
	Err     error
}

// HookWarningSink 接收 hook 警告。实现必须快速返回（它在工具/迁移的关键路径上
// 被同步调用）；返回错误没有意义，因为警告不能改变调用方流程。
type HookWarningSink func(ctx context.Context, w HookWarning)

// Hook 失败的哨兵，供 errors.Is 使用。
var (
	// ErrHookDenied 是每次 hook 拒绝（含端口缺失）都会命中的哨兵：
	// errors.Is(err, ErrHookDenied) 为真，errors.As 到 *HookDeniedError 可拿到
	// Hook 与 Unwrap 出的底层原因。
	ErrHookDenied = errors.New("execbackend: hook denied")
	// ErrHookPortMissing 表示根本没有接线 hook 端口。它是拒绝原因之一（失败即
	// 关闭）：接线漏了不能变成放行。
	ErrHookPortMissing = errors.New("execbackend: hook port is not configured")
)

// HookDeniedError 是一次 hook 拒绝：哪个 hook 点拒绝了、为什么、底层原因是什么。
//
// 与 errors.go 的 *Error 一样，错误文本只写字段名与原因，绝不回显 Arguments /
// 输出内容（它们会进日志与审计）。
type HookDeniedError struct {
	// Hook 是拒绝这次操作的 hook 点（HookPoint*）。
	Hook string
	// Reason 是拒绝的说明（不引用请求内容）。
	Reason string
	// Cause 是底层原因：hook handler/管理器的错误，或 ErrHookPortMissing。
	Cause error
}

// Error implements error；nil 接收者安全。
func (e *HookDeniedError) Error() string {
	if e == nil {
		return "<nil>"
	}
	msg := "execbackend: hook denied"
	if e.Hook != "" {
		msg = "execbackend: hook " + e.Hook + " denied the operation"
	}
	if e.Reason != "" {
		msg += ": " + e.Reason
	}
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap 暴露底层原因，使 errors.Is/As 能找到 ErrHookPortMissing、hook handler
// 的错误或 policy.DeniedError。
func (e *HookDeniedError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// Is 让 errors.Is(err, ErrHookDenied) 命中每一次 hook 拒绝。
func (e *HookDeniedError) Is(target error) bool { return target == ErrHookDenied }

// NewToolCallRequest 构造并校验一次工具调用的规范请求，同时计算指纹。
// 校验失败返回 Code=invalid_request 的 *Error（Field 指出字段），错误文本绝不
// 回显参数内容。
func NewToolCallRequest(ref RunRef, backend, eventID, toolCallID, tool string, arguments json.RawMessage) (ToolCallRequest, error) {
	req := ToolCallRequest{
		Run:        ref,
		Backend:    strings.TrimSpace(backend),
		EventID:    strings.TrimSpace(eventID),
		ToolCallID: strings.TrimSpace(toolCallID),
		Tool:       strings.TrimSpace(tool),
		Arguments:  arguments,
	}
	fingerprint, err := ToolRequestFingerprint(req.Tool, req.Arguments)
	if err != nil {
		return ToolCallRequest{}, err
	}
	req.Fingerprint = fingerprint
	if err := req.Validate(); err != nil {
		return ToolCallRequest{}, err
	}
	return req, nil
}

// Validate 校验请求字段与指纹（只返回第一个错误；不做任何 I/O）。
//
// 指纹必须与 {Tool, Arguments} 一致：payload 契约要求 hook 看到的 fingerprint
// 就是守卫这次调用的那个值，调用方不能自己编一个。只由 NewToolCallRequest 构造
// 的请求必然满足；手写结构体必须自己算对，否则拒绝（失败即关闭）。
func (r ToolCallRequest) Validate() error {
	if err := validateHookRunRef(r.Run, "run"); err != nil {
		return err
	}
	if err := checkHookField("backend", r.Backend, MaxProviderRefBytes); err != nil {
		return err
	}
	if err := checkHookField("event_id", r.EventID, MaxHookEventIDBytes); err != nil {
		return err
	}
	if err := checkHookField("tool_call_id", r.ToolCallID, MaxProviderRefBytes); err != nil {
		return err
	}
	if err := checkHookField("tool", r.Tool, MaxProviderRefBytes); err != nil {
		return err
	}
	if len(r.Arguments) > MaxControlFrameBytes {
		return invalidRequestf("arguments", "arguments are %d bytes, limit %d", len(r.Arguments), MaxControlFrameBytes)
	}
	if _, err := canonicalJSON(r.Arguments); err != nil {
		return err
	}
	if err := checkHookField("fingerprint", r.Fingerprint, MaxHookEventIDBytes); err != nil {
		return err
	}
	want, err := ToolRequestFingerprint(r.Tool, r.Arguments)
	if err != nil {
		return err
	}
	if r.Fingerprint != want {
		return invalidRequestf("fingerprint", "fingerprint does not match the request (want %s)", want)
	}
	return nil
}

// Validate 校验结果摘要的形状。
func (r ToolCallResult) Validate() error {
	switch r.Status {
	case ToolResultStatusOK, ToolResultStatusError, ToolResultStatusDenied:
		return nil
	default:
		return invalidRequestf("status", "%q is not one of ok/error/denied", r.Status)
	}
}

// NewRunLifecycleRequest 构造并校验一次 Run 生命周期 hook 的规范请求。
func NewRunLifecycleRequest(ref RunRef, backend, eventID, status, previousStatus string) (RunLifecycleRequest, error) {
	req := RunLifecycleRequest{
		Run:            ref,
		Backend:        strings.TrimSpace(backend),
		EventID:        strings.TrimSpace(eventID),
		Status:         strings.TrimSpace(status),
		PreviousStatus: strings.TrimSpace(previousStatus),
	}
	if err := req.Validate(); err != nil {
		return RunLifecycleRequest{}, err
	}
	return req, nil
}

// Validate 校验 Run 生命周期请求字段（只返回第一个错误）。
//
// Status 的语义（start 必须是 starting、finish 必须是终态）由 hooks 的
// RunHookPayload.Validate 强制：execbackend 不得 import run，所以本层不复制
// RunStatus 枚举，只校验形状，最终闸门在 runhooks。attempt 与 agent_revision 在
// 这里是可选的（给出时检查长度），AuthorizeRunStart 另行要求两者都在。
func (r RunLifecycleRequest) Validate() error {
	if err := validateLifecycleRunRef(r.Run, "run"); err != nil {
		return err
	}
	if err := checkHookField("backend", r.Backend, MaxProviderRefBytes); err != nil {
		return err
	}
	if err := checkHookField("event_id", r.EventID, MaxHookEventIDBytes); err != nil {
		return err
	}
	if err := checkHookField("status", r.Status, MaxHookEventIDBytes); err != nil {
		return err
	}
	if r.PreviousStatus != "" {
		if err := checkHookField("previous_status", r.PreviousStatus, MaxHookEventIDBytes); err != nil {
			return err
		}
		if r.PreviousStatus == r.Status {
			return invalidRequestf("previous_status", "previous_status equals status (%q): a transition must move between states", r.Status)
		}
	}
	return nil
}

// ToolRequestFingerprint 返回一次工具请求的规范指纹：
// "sha256:" + 64 位小写 hex，输入是 {"tool":<名字>,"arguments":<规范 JSON>}。
//
// 规范化（冻结）：arguments 必须是合法 JSON；对象键按字典序重排、去掉一切无关
// 空白、数字按十进制值精确归一（1、1.0、10e-1 相同；不经过 float64，值不同的
// 数字绝不相同）。于是键序或空白不同、但语义等价的 JSON 得到同一指纹，而含义
// 不同的两个请求绝不会——审批以后按指纹绑定请求（T2.02），碰撞就是越权。所以
// 含义取决于解析器的参数一律拒绝：同一对象里重复的键、非法 UTF-8、落单的
// UTF-16 代理项转义（encoding/json 分别取最后一个值、解成 U+FFFD，不同的请求会
// 因此撞到一起）。这些情况与非法 JSON 一样返回 invalid_request 错误
// （Field=arguments），绝不回显内容。
func ToolRequestFingerprint(tool string, arguments json.RawMessage) (string, error) {
	name := strings.TrimSpace(tool)
	if name == "" {
		return "", invalidRequestf("tool", "tool is required")
	}
	canonical, err := canonicalJSON(arguments)
	if err != nil {
		return "", err
	}
	envelope, err := json.Marshal(struct {
		Tool      string          `json:"tool"`
		Arguments json.RawMessage `json:"arguments"`
	}{Tool: name, Arguments: canonical})
	if err != nil {
		return "", invalidRequestf("arguments", "the canonical request cannot be encoded: %v", err)
	}
	sum := sha256.Sum256(envelope)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// canonicalJSON 把一段 JSON 重新编码成规范形式：对象键排序、无多余空白、数字归一。
func canonicalJSON(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, invalidRequestf("arguments", "arguments are required")
	}
	if len(raw) > MaxControlFrameBytes {
		return nil, invalidRequestf("arguments", "arguments are %d bytes, limit %d", len(raw), MaxControlFrameBytes)
	}
	if !json.Valid(raw) {
		// 只说"不是合法 JSON"和字节数，绝不把内容写进错误文本（脱敏后也可能含敏感值）。
		return nil, invalidRequestf("arguments", "arguments are not valid JSON (%d bytes)", len(raw))
	}
	if !utf8.Valid(raw) {
		return nil, invalidRequestf("arguments", "arguments are not valid UTF-8 (%d bytes)", len(raw))
	}
	if hasLoneSurrogateEscape(raw) {
		return nil, invalidRequestf("arguments", "arguments contain an unpaired UTF-16 surrogate escape")
	}
	if err := rejectDuplicateKeys(raw); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, invalidRequestf("arguments", "arguments are not valid JSON (%d bytes)", len(raw))
	}
	normalized, err := normalizeJSONNumbers(value)
	if err != nil {
		return nil, err
	}
	out, err := json.Marshal(normalized)
	if err != nil {
		return nil, invalidRequestf("arguments", "arguments cannot be canonicalized: %v", err)
	}
	return out, nil
}

// hasLoneSurrogateEscape 报告 raw（已通过 json.Valid）的字符串里有没有落单的
// \uD800-\uDFFF 转义：高代理后面必须紧跟一个低代理转义，低代理不能单独出现。
// encoding/json 把落单的代理项解成 U+FFFD，不同的转义会因此撞成同一个字符串。
func hasLoneSurrogateEscape(raw []byte) bool {
	inString := false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if !inString {
			inString = c == '"'
			continue
		}
		switch c {
		case '"':
			inString = false
		case '\\':
			if i+1 >= len(raw) || raw[i+1] != 'u' {
				i++ // \" \\ \/ \b \f \n \r \t: skip the escaped character
				continue
			}
			unit, ok := hexCodeUnit(raw, i+2)
			if !ok {
				return true // cannot happen after json.Valid; refuse rather than guess
			}
			i += 5 // the last hex digit of this escape
			switch {
			case unit >= 0xDC00 && unit <= 0xDFFF:
				return true // a low surrogate with no high one before it
			case unit >= 0xD800 && unit <= 0xDBFF:
				if i+2 < len(raw) && raw[i+1] == '\\' && raw[i+2] == 'u' {
					if low, ok := hexCodeUnit(raw, i+3); ok && low >= 0xDC00 && low <= 0xDFFF {
						i += 6 // the last hex digit of the paired low surrogate
						continue
					}
				}
				return true
			}
		}
	}
	return false
}

// hexCodeUnit 读取 raw[at:at+4] 的四位十六进制 UTF-16 码元。
func hexCodeUnit(raw []byte, at int) (uint16, bool) {
	if at+4 > len(raw) {
		return 0, false
	}
	value, err := strconv.ParseUint(string(raw[at:at+4]), 16, 16)
	if err != nil {
		return 0, false
	}
	return uint16(value), true
}

// rejectDuplicateKeys 拒绝在同一个对象里出现两次的键（逐字比较、区分大小写）：
// encoding/json 静默保留最后一个值，别的解析器可能保留第一个，这样的请求没有唯一
// 含义。错误文本不回显键名。
func rejectDuplicateKeys(raw []byte) error {
	type level struct {
		keys      map[string]struct{} // nil: an array
		expectKey bool
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var stack []*level
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return invalidRequestf("arguments", "arguments are not valid JSON (%d bytes)", len(raw))
		}
		var top *level
		if n := len(stack); n > 0 {
			top = stack[n-1]
		}
		if delim, ok := token.(json.Delim); ok {
			switch delim {
			case '{', '[':
				if top != nil && top.keys != nil {
					top.expectKey = true // this value is a container; a key follows it
				}
				next := &level{}
				if delim == '{' {
					next.keys = map[string]struct{}{}
					next.expectKey = true
				}
				stack = append(stack, next)
			default: // '}' or ']'
				stack = stack[:len(stack)-1]
			}
			continue
		}
		if top == nil || top.keys == nil {
			continue // a top-level scalar or an array element
		}
		if !top.expectKey {
			top.expectKey = true // a scalar value; a key or '}' follows it
			continue
		}
		key, _ := token.(string)
		if _, seen := top.keys[key]; seen {
			return invalidRequestf("arguments", "arguments repeat a key within one object")
		}
		top.keys[key] = struct{}{}
		top.expectKey = false
	}
}

// normalizeJSONNumbers 递归地把 json.Number 换成它的精确规范写法（见 normalizeJSONNumber）。
func normalizeJSONNumbers(value any) (any, error) {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			normalized, err := normalizeJSONNumbers(item)
			if err != nil {
				return nil, err
			}
			typed[key] = normalized
		}
		return typed, nil
	case []any:
		for i, item := range typed {
			normalized, err := normalizeJSONNumbers(item)
			if err != nil {
				return nil, err
			}
			typed[i] = normalized
		}
		return typed, nil
	case json.Number:
		return normalizeJSONNumber(typed)
	default:
		return value, nil
	}
}

// normalizeJSONNumber 把一个 JSON 数字字面量换成精确的规范写法 [-]D e E：D 是去掉
// 前导与末尾 0 的有效数字，E 是十进制指数（值 = D × 10^E，0 写作 "0"）。值相等的
// 字面量（1、1.0、10e-1、0.1e1）得到同一写法，值不同的绝不会：全程只做十进制字符
// 运算，不经过 float64，所以不存在舍入碰撞（2^53+1 与 2^53、第 18 位之后才不同的
// 小数都保持可区分）。指数超出 int32 的字面量被拒绝。
func normalizeJSONNumber(number json.Number) (any, error) {
	mantissa := number.String() // the literal exactly as written; the decoder validated its syntax
	negative := strings.HasPrefix(mantissa, "-")
	mantissa = strings.TrimPrefix(mantissa, "-")
	exponent := int64(0)
	if i := strings.IndexAny(mantissa, "eE"); i >= 0 {
		parsed, err := strconv.ParseInt(mantissa[i+1:], 10, 32)
		if err != nil {
			return nil, invalidRequestf("arguments", "a number exponent is out of range")
		}
		exponent = parsed
		mantissa = mantissa[:i]
	}
	integer, fraction := mantissa, ""
	if i := strings.IndexByte(mantissa, '.'); i >= 0 {
		integer, fraction = mantissa[:i], mantissa[i+1:]
	}
	digits := strings.TrimLeft(integer+fraction, "0")
	if digits == "" {
		return json.Number("0"), nil // every zero: 0, -0, 0.000, 0e7
	}
	significant := strings.TrimRight(digits, "0")
	exponent += int64(len(digits)-len(significant)) - int64(len(fraction))
	sign := ""
	if negative {
		sign = "-"
	}
	return json.Number(sign + significant + "e" + strconv.FormatInt(exponent, 10)), nil
}

// AuthorizeToolCall 是 PreToolUse 的闸门：请求不合法、端口缺失或 hook 拒绝时返回
// 错误，调用方必须因此不执行工具（一次都不执行）。
//
// 返回错误的类型：
//   - 请求不合法：Code=invalid_request 的 *Error（errors.Is 命中 ErrInvalidRequest）；
//   - hook 拒绝或端口缺失：*HookDeniedError，errors.Is(err, ErrHookDenied) 为真，
//     端口缺失还能 Unwrap 到 ErrHookPortMissing，hook 失败能 Unwrap 到原因。
func AuthorizeToolCall(ctx context.Context, port HookPort, req ToolCallRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	if port == nil {
		return &HookDeniedError{Hook: HookPointPreToolUse, Reason: "no hook port is wired", Cause: ErrHookPortMissing}
	}
	if err := port.BeforeTool(ctx, req); err != nil {
		return &HookDeniedError{Hook: HookPointPreToolUse, Reason: "the before-tool hook refused the call", Cause: err}
	}
	return nil
}

// ReportToolResult 把工具结果（净化后的摘要）交给 PostToolUse。
//
// 它从不返回错误，也从不改变调用方流程：请求/结果不合法、端口缺失、hook 失败
// 一律只交给 warn。warn 为 nil 时写一行固定格式日志（不打印 Arguments / 输出）。
// 它绝不重跑工具：报告结果不是重放。
func ReportToolResult(ctx context.Context, port HookPort, warn HookWarningSink, req ToolCallRequest, result ToolCallResult) {
	if err := req.Validate(); err != nil {
		emitHookWarning(ctx, warn, HookWarning{Hook: HookPointPostToolUse, EventID: req.EventID, Err: err})
		return
	}
	if err := result.Validate(); err != nil {
		emitHookWarning(ctx, warn, HookWarning{Hook: HookPointPostToolUse, EventID: req.EventID, Err: err})
		return
	}
	if port == nil {
		emitHookWarning(ctx, warn, HookWarning{Hook: HookPointPostToolUse, EventID: req.EventID, Err: ErrHookPortMissing})
		return
	}
	if err := port.AfterTool(ctx, req, result); err != nil {
		emitHookWarning(ctx, warn, HookWarning{Hook: HookPointPostToolUse, EventID: req.EventID, Err: err})
	}
}

// RunTool 是工具调用在 hook 端口上的唯一入口：
//
//  1. AuthorizeToolCall（PreToolUse 在 Guard 之前）；被拒绝则 execute 一次都不
//     调用，返回 Status=denied 与拒绝错误；
//  2. 放行则 execute 恰好调用一次（Guard 由 execute 内部负责）；
//  3. ReportToolResult（PostToolUse 在结果净化之后）。after 失败不重跑 execute、
//     不改变返回值：只有警告，没有第二次执行。
//
// execute 报错却没有给出结果状态时补 Status=error（post 观察者永远不该看到无状态
// 结果）；这只补状态，不改返回值。
func RunTool(ctx context.Context, port HookPort, warn HookWarningSink, req ToolCallRequest, execute func(context.Context) (ToolCallResult, error)) (ToolCallResult, error) {
	if err := AuthorizeToolCall(ctx, port, req); err != nil {
		return ToolCallResult{Status: ToolResultStatusDenied}, err
	}
	if execute == nil {
		return ToolCallResult{Status: ToolResultStatusDenied}, invalidRequestf("execute", "execute is required once the hook allowed the call")
	}
	result, err := execute(ctx)
	if err != nil && strings.TrimSpace(result.Status) == "" {
		result.Status = ToolResultStatusError
	}
	ReportToolResult(ctx, port, warn, req, result)
	return result, err
}

// AuthorizeRunStart 是 RunStart 的闸门：请求不合法（包括缺 attempt 或
// agent_revision——RunStart 在认领创建 attempt 之后才触发）、端口缺失或 hook 拒绝
// 时返回错误，调用方必须因此不启动进程。错误类型同 AuthorizeToolCall。
func AuthorizeRunStart(ctx context.Context, port HookPort, req RunLifecycleRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	if err := validateHookRunRef(req.Run, "run"); err != nil {
		return err
	}
	if port == nil {
		return &HookDeniedError{Hook: HookPointRunStart, Reason: "no hook port is wired", Cause: ErrHookPortMissing}
	}
	if err := port.BeforeRunStart(ctx, req); err != nil {
		return &HookDeniedError{Hook: HookPointRunStart, Reason: "the run-start hook refused to start the process", Cause: err}
	}
	return nil
}

// ReportRunFinish 把 Run 终态迁移交给 RunFinish。语义同 ReportToolResult：
// 从不返回错误、只产生警告，Run 的终态不因 hook 失败而改变。
func ReportRunFinish(ctx context.Context, port HookPort, warn HookWarningSink, req RunLifecycleRequest) {
	if err := req.Validate(); err != nil {
		emitHookWarning(ctx, warn, HookWarning{Hook: HookPointRunFinish, EventID: req.EventID, Err: err})
		return
	}
	if port == nil {
		emitHookWarning(ctx, warn, HookWarning{Hook: HookPointRunFinish, EventID: req.EventID, Err: ErrHookPortMissing})
		return
	}
	if err := port.AfterRunFinish(ctx, req); err != nil {
		emitHookWarning(ctx, warn, HookWarning{Hook: HookPointRunFinish, EventID: req.EventID, Err: err})
	}
}

// emitHookWarning 是警告的唯一出口：warn 为 nil 时写一行固定格式日志。日志与
// 警告文本都不包含 Arguments / 输出内容。
func emitHookWarning(ctx context.Context, warn HookWarningSink, w HookWarning) {
	if warn != nil {
		warn(ctx, w)
		return
	}
	log.Printf("[WARN] execbackend: hook %s failed for event %s: %v", w.Hook, w.EventID, w.Err)
}

// validateHookRunRef 校验执行身份的四个字段（与 StartRequest.Validate 同规则）。
func validateHookRunRef(ref RunRef, field string) error {
	for _, item := range []struct{ name, value string }{
		{"project_id", ref.ProjectID},
		{"run_id", ref.RunID},
		{"attempt_id", ref.AttemptID},
		{"agent_revision_id", ref.AgentRevisionID},
	} {
		if err := checkHookField(field+"."+item.name, item.value, MaxHookEventIDBytes); err != nil {
			return err
		}
	}
	return nil
}

// validateLifecycleRunRef 校验 Run 生命周期请求的身份：project_id 与 run_id 必填，
// attempt_id 与 agent_revision_id 给出时检查长度（空 = 这个 Run 还没有它）。
func validateLifecycleRunRef(ref RunRef, field string) error {
	for _, item := range []struct {
		name, value string
		required    bool
	}{
		{"project_id", ref.ProjectID, true},
		{"run_id", ref.RunID, true},
		{"attempt_id", ref.AttemptID, false},
		{"agent_revision_id", ref.AgentRevisionID, false},
	} {
		if !item.required && item.value == "" {
			continue
		}
		if err := checkHookField(field+"."+item.name, item.value, MaxHookEventIDBytes); err != nil {
			return err
		}
	}
	return nil
}

// checkHookField 应用共享的非空 / 长度上限规则；max 为 0 表示不限长。
func checkHookField(field, value string, max int) error {
	if strings.TrimSpace(value) == "" {
		return invalidRequestf(field, "%s is required", field)
	}
	if max > 0 && len(value) > max {
		return invalidRequestf(field, "%s is %d bytes, limit %d", field, len(value), max)
	}
	return nil
}
