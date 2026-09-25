package fake

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/codeflow/backend/internal/execbackend"
)

// Step 是脚本中的一步，可自由组合（Repeat 可嵌套）。
//
// Step 只能在 fake 包内实现；外部通过 Emit/Duplicate/Garbled/Flood/AwaitApproval/
// Sleep/Exit/ExitNoCode/ExitWith/Crash/Repeat/IfApproved/IfDenied 构造。
type Step interface {
	run(*runner) error
}

// errScriptExited 表示脚本已投递终结观察并结束（不是错误）。
var errScriptExited = errors.New("fake: script finished")

// errTerminated 表示步骤被终结请求（Cancel / HardDeadline / Close）打断。
var errTerminated = errors.New("fake: session terminated")

// scriptError 是脚本自身的内部错误（构造了非法观察、编排有误等）。
// 会话据此以 protocol_error 终结：仍投递终结观察，同时 Wait 返回该错误。
type scriptError struct{ err error }

func (e *scriptError) Error() string { return "fake: script error: " + e.err.Error() }

// Unwrap 暴露底层原因。
func (e *scriptError) Unwrap() error { return e.err }

func scriptErrf(format string, args ...any) *scriptError {
	return &scriptError{err: fmt.Errorf(format, args...)}
}

// runner 由会话的 run goroutine 独占，不需要加锁。
type runner struct {
	s *session
	// last 是最近一次投递的观察，供 Duplicate 复制（ProviderRef 与 payload 相同）。
	last *execbackend.Observation
	// emitted 已投递的普通观察条数，用于生成稳定的 provider_ref。
	emitted int
	// delivered 终结观察是否已由脚本投递（会话据此避免重复投递 exited）。
	delivered bool
	// approvalDecided/approvalApproved 记录最近一次 AwaitApproval 的结果，供
	// IfApproved / IfDenied 使用。
	approvalDecided  bool
	approvalApproved bool
}

func (r *runner) runSteps(steps []Step) error {
	for i, st := range steps {
		if st == nil {
			return scriptErrf("step %d is nil", i)
		}
		if err := st.run(r); err != nil {
			return err
		}
	}
	return nil
}

// deliver 投递一条普通观察：盖章 ObservedAt、校验、发送。
func (r *runner) deliver(o execbackend.Observation) error {
	prepared, err := r.s.prepare(o)
	if err != nil {
		return err
	}
	if err := r.s.send(prepared); err != nil {
		return err
	}
	copied := prepared
	r.last = &copied
	r.emitted++
	return nil
}

// deliverTerminal 处理脚本自己投递的终结观察（Emit(exited)/Exit/Crash）：
// 校验 → 记录终结结果 → 投递 → 结束脚本。
//
// 若会话已经因 Cancel/HardDeadline/Close 终结，则本观察被丢弃（不会产生第二条 exited）。
func (r *runner) deliverTerminal(o execbackend.Observation) error {
	prepared, err := r.s.prepare(o)
	if err != nil {
		return err
	}
	var result execbackend.ExitResult
	if err := json.Unmarshal(prepared.Payload, &result); err != nil {
		// Validate 已保证 exited 的 payload 可解码为 ExitResult，这里只是兜底。
		return scriptErrf("exited payload is not a valid ExitResult: %v", err)
	}
	if !r.s.recordTerminal(result, nil) {
		return errTerminated
	}
	r.delivered = r.s.sendTerminal(prepared)
	return errScriptExited
}

// ---------------------------------------------------------------------------
// 步骤构造器
// ---------------------------------------------------------------------------

// Emit 投递一条观察（投递前校验 Observation.Validate）。
//
// payload 可用任意可 JSON 编码值（map[string]any、结构体、json.RawMessage……），
// nil 表示不带 payload。Emit(ObservationExited, ...) 被当作终结：payload 必须是
// 合法的 ExitResult，投递后脚本结束。
func Emit(kind execbackend.ObservationKind, providerRef string, payload any) Step {
	return emitStep{kind: kind, ref: providerRef, payload: payload}
}

// Duplicate 把上一条观察原样再投递一次（重复帧：ProviderRef 与 payload 相同）。
//
// 前面没有任何观察时是脚本错误（→ protocol_error），避免静默吞掉编排错误。
func Duplicate() Step { return duplicateStep{} }

// Garbled 投递一条 protocol_warning，payload 描述一段损坏的原始字节：
//
//	{"stage":"stream_decode","reason":"...","raw_bytes":N,"raw_encoding":"base64","raw":"..."}
//
// 观察本身合法（远小于 64 KiB 控制帧上限）；用它模拟上游输出里无法解析的帧——
// 服务端应把乱码当协议警告处理，而不是当输出正文。
func Garbled() Step { return garbledStep{} }

// Flood 连续投递 n 条 output，每条 payload 恰好 size 字节（夹到 MaxObservationBytes
// 上限）：用于制造输出洪峰、验证背压与"不丢帧"。
//
// size 小到放不下结构体时退化为 JSON 字符串（长度 max(size, 2)），仍保证 payload ≤ size。
func Flood(n, size int) Step { return floodStep{n: n, size: size} }

// AwaitApproval 投递一条 approval_required（ProviderRef=approvalProviderRef，
// payload 为 {"tool_call_id": ..., "request": toolPayload}），然后阻塞直到：
//
//   - Approve 送来 ProviderRef 匹配的决定：Approved=true 继续后续步骤；Approved=false
//     先投递一条 tool_result（同一 ProviderRef，payload 含 "denied": true 与拒绝原因）
//     再继续后续步骤；
//   - 会话被 Cancel / HardDeadline / Close 终结：本步被打断。
//
// 后端未声明 approval_hook 能力时本步是脚本错误（→ protocol_error），而不是无限等待。
// 只想观察一条 approval_required 而不阻塞，用 Emit(ObservationApprovalRequired, ...)。
func AwaitApproval(approvalProviderRef string, toolPayload any) Step {
	return awaitApprovalStep{ref: approvalProviderRef, tool: toolPayload}
}

// Sleep 在 Clock 上等待 d；被 Cancel/HardDeadline/Close 打断时立即结束本步。
func Sleep(d time.Duration) Step { return sleepStep{d: d} }

// Exit 投递终结观察 exited（payload=ExitResult{ExitCode: code, Reason: reason,
// Usage: usage, Retryable: false}）并结束脚本。
//
// Retryable 一律为 false（保守：脚本没有"可安全重试"的证据）；需要别的取值用 ExitWith
// 或 Emit(ObservationExited, ...) 直接给出 ExitResult。
func Exit(code int, reason execbackend.ExitReason, usage execbackend.Usage) Step {
	return exitStep{result: execbackend.ExitResult{
		ExitCode: &code,
		Reason:   reason,
		Usage:    usage,
	}}
}

// ExitNoCode 与 Exit 相同，但不带退出码（ExitCode=nil，例如无法确认退出）。
func ExitNoCode(reason execbackend.ExitReason, usage execbackend.Usage) Step {
	return exitStep{result: execbackend.ExitResult{Reason: reason, Usage: usage}}
}

// ExitWith 投递任意 ExitResult（合法性由 Observation.Validate 校验）。
// 它是 Exit/Crash 的通用形式：可脚本化 retryable、nil exit code 等组合。
func ExitWith(result execbackend.ExitResult) Step { return exitStep{result: result} }

// Crash 投递 exited{Reason: crashed, Retryable: false, Usage.Quality: unknown} 并结束。
func Crash() Step {
	return exitStep{result: execbackend.ExitResult{
		Reason:    execbackend.ExitReasonCrashed,
		Retryable: false,
		Usage:     UnknownUsage(),
	}}
}

// Repeat 把 steps 重复 n 次（n <= 0 时不执行）。
func Repeat(n int, steps ...Step) Step { return repeatStep{n: n, steps: steps} }

// IfApproved 仅在最近一次 AwaitApproval 被批准时执行 steps。
func IfApproved(steps ...Step) Step { return conditionalStep{want: true, steps: steps} }

// IfDenied 仅在最近一次 AwaitApproval 被拒绝时执行 steps。
func IfDenied(steps ...Step) Step { return conditionalStep{want: false, steps: steps} }

// ---------------------------------------------------------------------------
// 用量便捷构造器
// ---------------------------------------------------------------------------

// UnknownUsage 返回"未知"用量：指针全为 nil + Quality=unknown（不得当成 0）。
func UnknownUsage() execbackend.Usage {
	return execbackend.Usage{Quality: execbackend.UsageQualityUnknown}
}

// ReportedUsage 返回供应商上报的 token 用量（无费用）。
func ReportedUsage(inputTokens, outputTokens int64) execbackend.Usage {
	return execbackend.Usage{
		InputTokens:  &inputTokens,
		OutputTokens: &outputTokens,
		Quality:      execbackend.UsageQualityReported,
	}
}

// EstimatedUsage 返回估计用量。
func EstimatedUsage(inputTokens, outputTokens int64) execbackend.Usage {
	return execbackend.Usage{
		InputTokens:  &inputTokens,
		OutputTokens: &outputTokens,
		Quality:      execbackend.UsageQualityEstimated,
	}
}

// CostUsage 返回带金额的用量（最小单位整数 + 币种，§27.1）。
func CostUsage(inputTokens, outputTokens, costMinor int64, currency string) execbackend.Usage {
	usage := ReportedUsage(inputTokens, outputTokens)
	usage.CostMinor = &costMinor
	usage.Currency = currency
	return usage
}

// ---------------------------------------------------------------------------
// 具体步骤实现
// ---------------------------------------------------------------------------

type emitStep struct {
	kind    execbackend.ObservationKind
	ref     string
	payload any
}

func (s emitStep) run(r *runner) error {
	raw, err := encodePayload(s.payload)
	if err != nil {
		return scriptErrf("Emit(%s): %v", s.kind, err)
	}
	obs := execbackend.Observation{Kind: s.kind, ProviderRef: s.ref, Payload: raw}
	if obs.Kind.IsTerminal() {
		return r.deliverTerminal(obs)
	}
	return r.deliver(obs)
}

type duplicateStep struct{}

func (duplicateStep) run(r *runner) error {
	if r.last == nil {
		return scriptErrf("Duplicate: no previous observation to duplicate")
	}
	return r.deliver(*r.last)
}

// garbledRaw 是模拟的损坏帧字节：非法 UTF-8 + 截断的 JSON + 控制字符。
var garbledRaw = []byte{
	0xff, 0xfe, 0x00, 0x7b, 0x22, 0x74, 0x79, 0x70, 0x65, 0x22,
	0x3a, 0x22, 0x74, 0x6f, 0x6f, 0x6c, 0x92, 0xc3, 0x28, 0x1b,
}

type garbledStep struct{}

func (garbledStep) run(r *runner) error {
	payload := map[string]any{
		"stage":        "stream_decode",
		"reason":       "frame is not valid UTF-8 JSON",
		"raw_bytes":    len(garbledRaw),
		"raw_encoding": "base64",
		"raw":          base64.StdEncoding.EncodeToString(garbledRaw),
	}
	return r.deliver(execbackend.Observation{
		Kind:        execbackend.ObservationProtocolWarning,
		ProviderRef: "garbled-1",
		Payload:     mustJSON(payload),
	})
}

type floodStep struct {
	n    int
	size int
}

func (s floodStep) run(r *runner) error {
	for i := 0; i < s.n; i++ {
		index := r.emitted
		err := r.deliver(execbackend.Observation{
			Kind:        execbackend.ObservationOutput,
			ProviderRef: fmt.Sprintf("out-%08d", index),
			Payload:     floodPayload(index, s.size),
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// floodPayload 生成恰好 size 字节（并夹到 MaxObservationBytes）的输出载荷。
func floodPayload(index, size int) json.RawMessage {
	if size > execbackend.MaxObservationBytes {
		size = execbackend.MaxObservationBytes
	}
	head := `{"stream":"stdout","seq":` + strconv.Itoa(index) + `,"pad":"`
	tail := `"}`
	if pad := size - len(head) - len(tail); pad >= 0 {
		return json.RawMessage(head + strings.Repeat("x", pad) + tail)
	}
	// size 太小放不下结构体：退化成恰好 size 字节的 JSON 字符串。
	if size < 2 {
		size = 2
	}
	return json.RawMessage(`"` + strings.Repeat("x", size-2) + `"`)
}

type sleepStep struct{ d time.Duration }

func (s sleepStep) run(r *runner) error {
	if s.d <= 0 {
		return nil
	}
	select {
	case <-r.s.clock.After(s.d):
		return nil
	case <-r.s.stopCh:
		return errTerminated
	}
}

type awaitApprovalStep struct {
	ref  string
	tool any
}

func (s awaitApprovalStep) run(r *runner) error {
	if !r.s.report.Has(execbackend.CapabilityApprovalHook) {
		return scriptErrf("AwaitApproval(%q): backend did not prove capability %s",
			s.ref, string(execbackend.CapabilityApprovalHook))
	}
	waiter, err := r.s.registerApproval(s.ref)
	if err != nil {
		return err
	}
	payload := map[string]any{"tool_call_id": s.ref, "request": s.tool}
	err = r.deliver(execbackend.Observation{
		Kind:        execbackend.ObservationApprovalRequired,
		ProviderRef: s.ref,
		Payload:     mustJSON(payload),
	})
	if err != nil {
		return err
	}

	select {
	case decision := <-waiter.ch:
		r.approvalDecided = true
		r.approvalApproved = decision.Approved
		if decision.Approved {
			return nil
		}
		denied := map[string]any{
			"tool_call_id": s.ref,
			"ok":           false,
			"denied":       true,
			"reason":       decision.Reason,
		}
		return r.deliver(execbackend.Observation{
			Kind:        execbackend.ObservationToolResult,
			ProviderRef: s.ref,
			Payload:     mustJSON(denied),
		})
	case <-r.s.stopCh:
		return errTerminated
	}
}

type exitStep struct{ result execbackend.ExitResult }

func (s exitStep) run(r *runner) error {
	return r.deliverTerminal(execbackend.Observation{
		Kind:    execbackend.ObservationExited,
		Payload: mustJSON(s.result),
	})
}

type repeatStep struct {
	n     int
	steps []Step
}

func (s repeatStep) run(r *runner) error {
	for i := 0; i < s.n; i++ {
		if err := r.runSteps(s.steps); err != nil {
			return err
		}
	}
	return nil
}

type conditionalStep struct {
	want  bool
	steps []Step
}

func (s conditionalStep) run(r *runner) error {
	if !r.approvalDecided || r.approvalApproved != s.want {
		return nil
	}
	return r.runSteps(s.steps)
}

// ---------------------------------------------------------------------------
// 辅助
// ---------------------------------------------------------------------------

// encodePayload 把脚本给出的 payload 编码为 JSON；nil 表示不带 payload。
func encodePayload(payload any) (json.RawMessage, error) {
	switch v := payload.(type) {
	case nil:
		return nil, nil
	case json.RawMessage:
		if len(v) == 0 {
			return nil, nil
		}
		return v, nil
	case []byte:
		// 与 encoding/json 一致：[]byte 编码为 base64 字符串（要原始 JSON 用 RawMessage）。
		raw, err := json.Marshal(base64.StdEncoding.EncodeToString(v))
		if err != nil {
			return nil, err
		}
		return raw, nil
	default:
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("payload is not JSON-marshalable: %w", err)
		}
		return raw, nil
	}
}

// mustJSON 编码夹具内部确定的 payload；失败时退化为一个可诊断的对象。
func mustJSON(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{"error":"payload is not JSON-marshalable"}`)
	}
	return raw
}
