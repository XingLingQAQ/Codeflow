package fake

import (
	"sync"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/execbackend"
	"github.com/codeflow/backend/internal/execbackend/backendtest"
)

// 本文件是 T1.06.c（§15 T1.06）的验收入口：用同一套 backendtest contract suite
// 对 fake 后端跑两遍能力配置，证明 suite 的两个取消分支都被真正执行。
//
//	1. default-capabilities：默认能力（含 cancel_graceful）→ 软取消必须终结会话；
//	2. without-cancel-graceful：显式去掉 cancel_graceful → 软取消必须返回
//	   capability_unavailable，且随后 force 取消仍能终结会话（§27.2 第 6 条）。
//
// fake 只负责把 Scenario 映射成脚本并交出合法请求；所有生命周期断言都在
// backendtest 里（不安装真实 CLI 也能验证 Run 状态、事件、审批、取消与合入前置条件）。

// contractApprovalRef 是 RequestsApproval 场景里 approval_required 观察的 ProviderRef。
const contractApprovalRef = "appr-contract-1"

// TestBackendContractLifecycle 用 fake 后端跑 backendtest 契约套件。
func TestBackendContractLifecycle(t *testing.T) {
	t.Run("default-capabilities", func(t *testing.T) {
		backendtest.RunContractSuite(t, contractHarness(DefaultCapabilities(), true))
	})
	t.Run("without-cancel-graceful", func(t *testing.T) {
		backendtest.RunContractSuite(t, contractHarness(withoutCapability(DefaultCapabilities(), execbackend.CapabilityCancelGraceful), false))
	})
}

// contractHarness 把 fake 接到 backendtest.Harness：手动时钟 + 场景脚本 + 合法请求。
//
// Advance 委托给"最近一个被构造的 fake Backend 的时钟"：suite 每个子测试只构造一个
// 用于推进时钟的后端，因此这里总能指向当前被测会话的时钟。
func contractHarness(caps []execbackend.Capability, supportsGracefulCancel bool) backendtest.Harness {
	var mu sync.Mutex
	var clock *ManualClock

	return backendtest.Harness{
		New: func(t *testing.T, s backendtest.Scenario) execbackend.Backend {
			c := NewManualClock(testStart)
			mu.Lock()
			clock = c
			mu.Unlock()
			return New(Options{
				Name:         "fake",
				Clock:        c,
				Capabilities: caps,
				Script:       contractScenarioScript(s),
			})
		},
		Request: func(t *testing.T) execbackend.StartRequest { return validRequest(t) },
		Advance: func(d time.Duration) {
			mu.Lock()
			c := clock
			mu.Unlock()
			if c != nil {
				c.Advance(d)
			}
		},
		ApprovalProviderRef:    contractApprovalRef,
		SupportsGracefulCancel: supportsGracefulCancel,
	}
}

// contractScenarioScript 把 suite 的场景映射成 fake 脚本。
//
// 每个场景都必须满足 backendtest.Scenario 的剧本说明；未知场景直接 panic：
// 新增场景时 suite 与 fake 必须一起更新，不能静默退化成空脚本。
func contractScenarioScript(s backendtest.Scenario) []Step {
	switch s {
	case backendtest.ScenarioCompletes:
		return []Step{
			Emit(execbackend.ObservationOutput, "msg-contract-1", map[string]any{
				"stream": "stdout",
				"text":   "contract suite completes",
			}),
			Emit(execbackend.ObservationToolRequested, "tool-read-1", map[string]any{
				"tool":         "read",
				"tool_call_id": "tool-read-1",
			}),
			Emit(execbackend.ObservationToolResult, "tool-read-1", map[string]any{
				"tool":         "read",
				"tool_call_id": "tool-read-1",
				"ok":           true,
			}),
			Emit(execbackend.ObservationUsage, "msg-contract-usage", ReportedUsage(120, 30)),
			Exit(0, execbackend.ExitReasonCompleted, ReportedUsage(120, 30)),
		}
	case backendtest.ScenarioRunsUntilCancelled:
		// 一直运行：只有 Cancel/Close 能打断 Sleep（手动时钟不会自己推进）。
		return []Step{Sleep(time.Hour)}
	case backendtest.ScenarioRequestsApproval:
		return RunSlowApproval(contractApprovalRef)
	case backendtest.ScenarioCrashes:
		return []Step{
			Emit(execbackend.ObservationOutput, "msg-contract-crash", map[string]any{
				"stream": "stderr",
				"text":   "boom",
			}),
			Crash(),
		}
	default:
		panic("fake: unknown backendtest.Scenario " + string(s))
	}
}

// withoutCapability 返回去掉 cap 的能力集合（保持其余顺序）。
func withoutCapability(caps []execbackend.Capability, cap execbackend.Capability) []execbackend.Capability {
	out := make([]execbackend.Capability, 0, len(caps))
	for _, c := range caps {
		if c == cap {
			continue
		}
		out = append(out, c)
	}
	return out
}
