package fake

import (
	"time"

	"github.com/codeflow/backend/internal/execbackend"
)

// 本文件是计划 §21.2 的固定夹具，供 T1.06.c 的 contract suite 与 Run 层测试复用：
// 脚本内容、provider_ref 与顺序都是冻结的，改这里等于改夹具，必须同步更新引用方。

// ToolSequenceTools 是 run-tool-sequence 夹具按顺序执行的工具名（§21.2）。
func ToolSequenceTools() []string {
	return []string{"read", "search", "write", "command", "test"}
}

// RunToolSequenceProviderRef 返回 run-tool-sequence 中某个工具的调用 provider_ref
// （tool_requested / tool_result / approval_required 共用同一个 ref）。
func RunToolSequenceProviderRef(tool string) string { return "tool-" + tool + "-1" }

// RunToolSequence 返回 §21.2 的 run-tool-sequence 固定脚本：
//
//	output(开始) → 对 read/search/write/command 各一对 tool_requested/tool_result
//	→ tool_requested(test) → approval_required(test) → tool_result(test, 批准)
//	→ usage → exited{completed, exit 0}
//
// 审批被拒绝时 approval_required 之后是 tool_result(denied)（由 AwaitApproval 投递），
// IfApproved 的那条成功结果不会出现——两条路径都只留一条 test 的 tool_result。
// provider_ref 稳定：tool-<name>-1；审批 ref 与 test 工具调用相同。
func RunToolSequence() []Step {
	steps := []Step{
		Emit(execbackend.ObservationOutput, "msg-0001", map[string]any{
			"stream": "stdout",
			"text":   "fake run started",
		}),
	}
	for _, tool := range ToolSequenceTools() {
		ref := RunToolSequenceProviderRef(tool)
		steps = append(steps, Emit(execbackend.ObservationToolRequested, ref, map[string]any{
			"tool":         tool,
			"tool_call_id": ref,
			"input":        map[string]any{"fixture": tool},
		}))
		if tool == "test" {
			steps = append(steps,
				AwaitApproval(ref, map[string]any{
					"tool":         tool,
					"tool_call_id": ref,
					"input":        map[string]any{"cmd": "go test ./..."},
				}),
				IfApproved(Emit(execbackend.ObservationToolResult, ref, map[string]any{
					"tool":         tool,
					"tool_call_id": ref,
					"ok":           true,
					"output":       "ok",
				})),
			)
			continue
		}
		steps = append(steps, Emit(execbackend.ObservationToolResult, ref, map[string]any{
			"tool":         tool,
			"tool_call_id": ref,
			"ok":           true,
			"output":       tool + " fixture result",
		}))
	}
	usage := CostUsage(1200, 340, 7, "USD")
	steps = append(steps,
		Emit(execbackend.ObservationUsage, "msg-usage-1", usage),
		Exit(0, execbackend.ExitReasonCompleted, usage),
	)
	return steps
}

// RunOutputFlood 返回输出洪峰脚本：n 条 1 KiB 的 output（快速超过环形日志上限），
// 最后以 completed/exit 0 结束。n <= 0 时只有一条起头 output。
//
// 供背压、artifact 引用与"不丢帧"验证使用。
func RunOutputFlood(n int) []Step {
	if n <= 0 {
		n = 1
	}
	return []Step{
		Emit(execbackend.ObservationOutput, "msg-flood-head", map[string]any{
			"stream": "stdout",
			"text":   "flood start",
		}),
		Flood(n, 1024),
		Emit(execbackend.ObservationUsage, "msg-flood-usage", UnknownUsage()),
		Exit(0, execbackend.ExitReasonCompleted, UnknownUsage()),
	}
}

// RunSlowApproval 返回"停在审批上"的脚本：投递一条 approval_required 后一直阻塞，
// 直到被 Approve / Cancel / HardDeadline / Close 处理。用于验证审批阻塞、
// 硬截止与取消必须能打断审批等待。
func RunSlowApproval(approvalProviderRef string) []Step {
	return []Step{
		Emit(execbackend.ObservationOutput, "msg-slow-1", map[string]any{
			"stream": "stdout",
			"text":   "waiting for approval",
		}),
		AwaitApproval(approvalProviderRef, map[string]any{
			"tool":         "command",
			"tool_call_id": approvalProviderRef,
			"input":        map[string]any{"cmd": "echo dangerous"},
		}),
		IfApproved(Emit(execbackend.ObservationToolResult, approvalProviderRef, map[string]any{
			"tool_call_id": approvalProviderRef,
			"ok":           true,
			"output":       "approved",
		})),
		Emit(execbackend.ObservationUsage, "msg-slow-usage", UnknownUsage()),
		Exit(0, execbackend.ExitReasonCompleted, UnknownUsage()),
	}
}

// RunIdle 返回一个不做任何事的脚本：只等 d 之后以 completed/exit 0 结束
// （配合 ManualClock 可让会话"挂在那里"）。d <= 0 时立即结束。
func RunIdle(d time.Duration) []Step {
	if d <= 0 {
		return []Step{Exit(0, execbackend.ExitReasonCompleted, UnknownUsage())}
	}
	return []Step{
		Sleep(d),
		Exit(0, execbackend.ExitReasonCompleted, UnknownUsage()),
	}
}
