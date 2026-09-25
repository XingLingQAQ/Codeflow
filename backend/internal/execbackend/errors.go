package execbackend

import (
	"errors"
	"fmt"
	"strings"
)

// 执行边界失败 code（计划 §15 T1.06、§21.1、§27.7）。
//
// 命名空间说明（§15 T0.03 主 Agent 裁定）：这些 code 是执行边界/状态迁移的失败
// 原因，不是 HTTP error envelope 的 code 枚举；API 层不得把它们直接当作 envelope
// code 返回，除非该端点的契约明确要求。
const (
	// CodeCapabilityUnavailable 后端未证明支持所需能力（§27.7：UI 据此禁用相应
	// 功能，不能自己模拟已通过审批）。
	CodeCapabilityUnavailable = "capability_unavailable"
	// CodeBackendUnavailable 后端不可用：可执行文件缺失、探测失败，或调度领取时
	// 后端能力不存在（§21.1 queued → starting 的失败 code）。
	CodeBackendUnavailable = "backend_unavailable"
	// CodeProcessStartFailed 进程启动失败（§21.1 starting → running 的前置条件
	// "pid/process_start_id 已记录" 未满足）。
	CodeProcessStartFailed = "process_start_failed"
	// CodeInvalidRequest 请求字段不合法：本地校验失败，未产生任何副作用。
	CodeInvalidRequest = "invalid_request"
	// CodeSessionClosed 会话已 Close，方法不再可用。
	CodeSessionClosed = "session_closed"
)

// 哨兵错误，供 errors.Is 使用；与 Code* 常量一一对应。
var (
	// ErrCapabilityUnavailable 对应 CodeCapabilityUnavailable。
	ErrCapabilityUnavailable = errors.New("execbackend: capability unavailable")
	// ErrBackendUnavailable 对应 CodeBackendUnavailable。
	ErrBackendUnavailable = errors.New("execbackend: backend unavailable")
	// ErrProcessStartFailed 对应 CodeProcessStartFailed。
	ErrProcessStartFailed = errors.New("execbackend: process start failed")
	// ErrInvalidRequest 对应 CodeInvalidRequest。
	ErrInvalidRequest = errors.New("execbackend: invalid request")
	// ErrSessionClosed 对应 CodeSessionClosed。
	ErrSessionClosed = errors.New("execbackend: session closed")
)

// Error 是执行后端协议的领域错误。
//
// 字段语义：
//   - Code 必填，取值见 Code* 常量。
//   - Capability 仅在"单一能力缺失"时填充；多能力缺失时用 Missing 列表。
//   - Field 指出不合法的请求字段（如 "work_dir"、"required"），仅 invalid_request 使用。
//   - Err 是底层原因，通过 Unwrap 暴露。
type Error struct {
	Code       string       `json:"code"`
	Capability Capability   `json:"capability,omitempty"`
	Missing    []Capability `json:"missing,omitempty"`
	Field      string       `json:"field,omitempty"`
	Message    string       `json:"message,omitempty"`
	Err        error        `json:"-"`
}

// Error 实现 error；nil 接收者安全。
func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	msg := e.Message
	if msg == "" {
		msg = e.Code
	}
	var b strings.Builder
	b.WriteString("execbackend: ")
	b.WriteString(msg)
	b.WriteString(" [code=")
	b.WriteString(e.Code)
	if e.Field != "" {
		b.WriteString(" field=")
		b.WriteString(e.Field)
	}
	if e.Capability != "" {
		b.WriteString(" capability=")
		b.WriteString(string(e.Capability))
	}
	if len(e.Missing) > 0 {
		b.WriteString(" missing=")
		b.WriteString(joinCapabilities(e.Missing))
	}
	b.WriteString("]")
	if e.Err != nil {
		b.WriteString(": ")
		b.WriteString(e.Err.Error())
	}
	return b.String()
}

// Unwrap 暴露底层原因，使 errors.Is/errors.As 能继续向下匹配；nil 接收者返回 nil。
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Is 支持两类匹配：
//   - 哨兵：errors.Is(err, ErrCapabilityUnavailable) 等，按 Code 映射；
//   - 同 code 的 *Error：errors.Is(err, &Error{Code: ...}) 视为同类错误，
//     不比较 Field/Missing/Message 细节。
func (e *Error) Is(target error) bool {
	if e == nil || target == nil {
		return false
	}
	if t, ok := target.(*Error); ok {
		return t != nil && t.Code == e.Code
	}
	if sentinel := sentinelForCode(e.Code); sentinel != nil {
		return errors.Is(sentinel, target)
	}
	return false
}

// sentinelForCode 把 code 映射到对应哨兵；未知 code 返回 nil。
func sentinelForCode(code string) error {
	switch code {
	case CodeCapabilityUnavailable:
		return ErrCapabilityUnavailable
	case CodeBackendUnavailable:
		return ErrBackendUnavailable
	case CodeProcessStartFailed:
		return ErrProcessStartFailed
	case CodeInvalidRequest:
		return ErrInvalidRequest
	case CodeSessionClosed:
		return ErrSessionClosed
	default:
		return nil
	}
}

// joinCapabilities 渲染能力列表，仅用于错误文本。
func joinCapabilities(caps []Capability) string {
	parts := make([]string, 0, len(caps))
	for _, c := range caps {
		parts = append(parts, string(c))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// NewInvalidRequest 构造 Code=invalid_request 的错误，field 指出不合法字段。
func NewInvalidRequest(field, message string) *Error {
	return &Error{Code: CodeInvalidRequest, Field: field, Message: message}
}

// invalidRequestf 是 NewInvalidRequest 的格式化版本，供本包校验函数内部使用。
func invalidRequestf(field, format string, args ...any) *Error {
	return NewInvalidRequest(field, fmt.Sprintf(format, args...))
}

// NewCapabilityUnavailable 构造 Code=capability_unavailable 的错误。
// missing 列出缺失能力（保持顺序）；恰好一项时同时填充 Capability 字段；
// 不传 missing 也合法（例如"该后端整体未证明支持审批"这类能力缺失）。
func NewCapabilityUnavailable(missing ...Capability) *Error {
	e := &Error{Code: CodeCapabilityUnavailable, Message: "required capability is not proven available"}
	if len(missing) > 0 {
		e.Missing = append([]Capability(nil), missing...)
	}
	switch len(missing) {
	case 0:
	case 1:
		e.Capability = missing[0]
		e.Message = "capability " + string(missing[0]) + " is not proven available"
	default:
		e.Message = "required capabilities are not proven available"
	}
	return e
}

// NewBackendUnavailable 构造 Code=backend_unavailable 的错误；err 为底层原因（可空）。
func NewBackendUnavailable(backend string, err error) *Error {
	msg := "backend unavailable"
	if backend != "" {
		msg = "backend " + backend + " unavailable"
	}
	return &Error{Code: CodeBackendUnavailable, Message: msg, Err: err}
}
