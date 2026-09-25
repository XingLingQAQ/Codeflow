// Package backendtest 提供执行后端（execbackend.Backend / execbackend.Session）的
// 可复用生命周期契约测试套件（计划 §15 T1.06、§28 T1.06.c）。
//
// 为什么是独立包：suite 必须同时服务 fake（不安装真实 CLI 也能验证 Run 状态、事件、
// 审批、取消与合入前置条件）与真实 CLI 适配器（T1.13.c 的 Claude Code、T4.01–T4.03
// 的 Codex/Gemini/自定义后端）。因此本包不 import fake、不 import 任何业务包，只依赖
// 标准库与 execbackend（定位同标准库的 testing/fstest）。
//
// 被验证的契约来源是 execbackend/types.go 的接口注释（Backend/Session 生命周期与
// 关闭责任）：Start/Cancel/Wait/Close 可重复、可并发、Close 幂等、终结观察唯一且最后、
// 投递终结观察后 channel 关闭、Wait 的 ctx 取消不改变会话结果、Close 后 Approve/Cancel
// 返回 session_closed、任何路径都不泄漏 goroutine。
//
// 用法：
//
//	func TestBackendContractLifecycle(t *testing.T) {
//		backendtest.RunContractSuite(t, backendtest.Harness{
//			New:                    func(t *testing.T, s backendtest.Scenario) execbackend.Backend { ... },
//			Request:                func(t *testing.T) execbackend.StartRequest { ... },
//			Advance:                clock.Advance, // 手动时钟后端；真实时钟传 nil
//			ApprovalProviderRef:    "appr-1",
//			SupportsGracefulCancel: true,
//		})
//	}
//
// 每个检查都是独立子测试：各自构造后端与会话，结束时 Close 并断言 goroutine 回落到
// 基线 +2 以内。suite 不使用 t.Parallel（goroutine 基线需要独占）。
package backendtest

import (
	"strings"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/execbackend"
)

// DefaultTimeout 是单次等待（观察、Wait、Cancel、Close、goroutine 回落）的默认上限。
const DefaultTimeout = 5 * time.Second

// advanceProbe 是 suite 借 Harness.Advance 推进的时间量：用于确认
// ScenarioRunsUntilCancelled 不会仅因时间流逝而自行终结（该场景的脚本必须一直运行）。
const advanceProbe = time.Second

// ctxCancelProbe 是"会话仍在运行"这类负向断言的 ctx 超时。
const ctxCancelProbe = 100 * time.Millisecond

// Scenario 是 suite 要求被测后端能构造的会话剧本。四个场景都必须实现：
// suite 会为每个场景调用 Harness.New 构造一个新的 Backend。
type Scenario string

const (
	// ScenarioCompletes 会话产出若干观察后以 exited{reason: completed} 终结。
	ScenarioCompletes Scenario = "completes"
	// ScenarioRunsUntilCancelled 会话一直运行（自身不会终结），直到被 Cancel。
	ScenarioRunsUntilCancelled Scenario = "runs_until_cancelled"
	// ScenarioRequestsApproval 会话投递 approval_required（ProviderRef 必须是
	// Harness.ApprovalProviderRef）后阻塞等待裁决；批准后继续并以
	// exited{reason: completed} 终结。
	ScenarioRequestsApproval Scenario = "requests_approval"
	// ScenarioCrashes 会话以 exited{reason: crashed} 终结，Usage 合法
	// （未知用量必须标 unknown 且字段缺席，而不是 0）。
	ScenarioCrashes Scenario = "crashes"
)

// AllScenarios 返回全部场景，供 Harness.New 的实现做穷举分派（遗漏即 fail）。
func AllScenarios() []Scenario {
	return []Scenario{
		ScenarioCompletes,
		ScenarioRunsUntilCancelled,
		ScenarioRequestsApproval,
		ScenarioCrashes,
	}
}

// Harness 把 suite 接到一个具体后端实现上。
//
// New 与 Request 必填；ApprovalProviderRef 必填（suite 始终运行审批场景）；
// 其余字段按被测后端的实际情况填写。
type Harness struct {
	// New 构造一个被测后端。suite 对每个子测试、每个场景都调用它，实现可以放心
	// 返回带 per-test 状态的后端。t 是当前子测试（可用 t.TempDir 等）。
	New func(t *testing.T, s Scenario) execbackend.Backend
	// Request 返回该后端可接受的一次合法启动请求。每次调用都必须返回全新副本
	// （suite 会改动它），且必须满足 StartRequest.Validate（绝对且已 Clean 的
	// WorkDir、合法 snapshot_hash 等）。
	Request func(t *testing.T) execbackend.StartRequest
	// Advance 推进被测后端的时钟（手动时钟实现）；真实时钟后端传 nil。
	// suite 只在一处使用它：确认 ScenarioRunsUntilCancelled 推进 advanceProbe
	// 之后仍处于运行中。
	Advance func(d time.Duration)
	// ApprovalProviderRef 是 ScenarioRequestsApproval 里 approval_required 观察
	// 必须携带的 ProviderRef；suite 也用它构造 Approve 裁决。
	ApprovalProviderRef string
	// SupportsGracefulCancel 报告被测后端是否声明并实现了 cancel_graceful 能力。
	// true：CancelGraceful 必须使会话以 cancelled 终结；
	// false：CancelGraceful 必须返回 capability_unavailable 且会话继续运行，
	// 随后 CancelForce 仍必须能终结会话（§27.2 第 6 条：Run 必须始终能被停下）。
	SupportsGracefulCancel bool
	// Timeout 是单次等待的上限；<= 0 用 DefaultTimeout。
	Timeout time.Duration
}

// RunContractSuite 依次运行全部契约子测试；任一子测试失败即整体失败。
func RunContractSuite(t *testing.T, h Harness) {
	t.Helper()
	if h.New == nil {
		t.Fatal("backendtest: Harness.New is required")
	}
	if h.Request == nil {
		t.Fatal("backendtest: Harness.Request is required")
	}
	if strings.TrimSpace(h.ApprovalProviderRef) == "" {
		t.Fatal("backendtest: Harness.ApprovalProviderRef is required: ScenarioRequestsApproval 用它匹配 approval_required 的 ProviderRef")
	}
	if h.Timeout <= 0 {
		h.Timeout = DefaultTimeout
	}

	t.Run("Prepare", func(t *testing.T) {
		t.Run("InvalidRequest", func(t *testing.T) { checkPrepareInvalidRequest(t, h) })
		t.Run("MissingCapability", func(t *testing.T) { checkPrepareMissingCapability(t, h) })
		t.Run("StartRequiresPreparedStart", func(t *testing.T) { checkPrepareStartRequiresPreparedStart(t, h) })
		t.Run("ValidRequestStarts", func(t *testing.T) { checkPrepareValidRequestStarts(t, h) })
	})

	t.Run("Completes", func(t *testing.T) {
		t.Run("ProcessStartedFirst", func(t *testing.T) { checkCompletesProcessStartedFirst(t, h) })
		t.Run("ObservationsValidTerminalLast", func(t *testing.T) { checkCompletesObservations(t, h) })
		t.Run("WaitRepeatableAndConsistent", func(t *testing.T) { checkCompletesWaitRepeatable(t, h) })
		t.Run("CancelAfterExitIsNoop", func(t *testing.T) { checkCompletesCancelAfterExit(t, h) })
		t.Run("CloseIdempotentAndClosedRejects", func(t *testing.T) { checkCompletesCloseIdempotent(t, h) })
	})

	t.Run("RunsUntilCancelled", func(t *testing.T) {
		t.Run("ForceCancelEndsSession", func(t *testing.T) { checkRunsUntilCancelledForce(t, h) })
		t.Run("GracefulCancel", func(t *testing.T) { checkRunsUntilCancelledGraceful(t, h) })
	})

	t.Run("CloseWithoutReading", func(t *testing.T) {
		t.Run("CloseReturnsAndChannelCloses", func(t *testing.T) { checkCloseWithoutReading(t, h) })
		t.Run("WaitAfterCloseReturnsImmediately", func(t *testing.T) { checkWaitAfterClose(t, h) })
	})

	t.Run("WaitContextCancel", func(t *testing.T) {
		t.Run("ContextCancelDoesNotChangeResult", func(t *testing.T) { checkWaitContextCancel(t, h) })
	})

	t.Run("RequestsApproval", func(t *testing.T) {
		t.Run("ApprovalRequiredObserved", func(t *testing.T) { checkApprovalRequiredObserved(t, h) })
		t.Run("UnknownRefIsInvalidRequest", func(t *testing.T) { checkApprovalUnknownRef(t, h) })
		t.Run("ApproveCompletesSession", func(t *testing.T) { checkApprovalCompletes(t, h) })
	})

	t.Run("Crashes", func(t *testing.T) {
		t.Run("TerminalReasonCrashedAndUsageValid", func(t *testing.T) { checkCrashes(t, h) })
	})
}
