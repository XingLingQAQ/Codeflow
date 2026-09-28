package planner

import (
	"context"
	"log"
	"sync"

	backendhooks "github.com/codeflow/backend/internal/hooks"
)

// 本文件是 T1.07.b 第 3 组在 planner 层的警告出口（§15 T1.07、§28 T1.07.b）。
//
// 四个任务 hook（HookBeforeTaskExecute、HookAfterTaskExecute、HookOnTaskFailure、
// HookOnTaskComplete）的触发点是 memory_integration.go 的 emitGlobalTaskHook：它用
// switch 分派管理器的方法，失败时不再把错误交给调用方（那会让四个调用方各自决定
// 记不记、记什么），而是在触发点自身恰好记一条警告——planner/service.go 的
// UpdateTask 因此不依赖返回值，hook 失败绝不让状态迁移失败或回滚。
//
// retry 的语义由 hooks 管理器实现（HookConfig.RetryCount 只重跑 hook handler，
// 用尽后返回错误），触发点做的只是把最终失败当 warn 记下来，**绝不重做任务状态
// 迁移**——一次 UpdateTask 的 CompletedCount / 计划完成判定 / 解除依赖只发生一次。
//
// 形状照抄第 1、2 组（internal/execbackend/hooks.go、internal/adapters/hook_warnings.go）：
// 有 sink 走 sink，没有 sink 写一行固定格式日志。日志与警告都只带 hook 类型、
// task id 与错误，**绝不带任务标题、描述、元数据或任何 payload 内容**（任务
// metadata 可能含用户数据）。planner 不 import adapters 或 execbackend（方向相反的
// 依赖会成环），所以这里按同一形状复刻，而不是共用一个包。
//
// 警告的去向：今天另一处记录是 hooks 管理器自身的审计（失败写 OutcomeFailure）。
// 警告的持久化（outbox / Run 时间线）需要新的事件类型，封闭的
// run.ExecutionEventTypes 里没有，归 T1.04。

// HookWarning 是一次任务 hook 失败的报告。Hook 是失败的 hook 类型，TaskID 是触发
// 时任务的身份，Err 是失败原因。它绝不携带任务标题、描述或元数据：警告会进日志。
//
// TaskID 由触发点从 payload 取（*TaskExecutionContext / *TaskFailureContext 的
// TaskID）；其它类型的 payload（未知结构）取空串，记录仍然发生。
type HookWarning struct {
	// Hook 是失败的 hook 类型。
	Hook backendhooks.HookType
	// TaskID 是触发这个 hook 的任务 ID，payload 里没有时为空串。
	TaskID string
	// Err 是失败原因（hook 管理器返回的错误，retry 用尽后是最后一次的错误）。
	Err error
}

// HookWarningSink 接收 hook 警告。实现必须快速返回（它在 UpdateTask 的调用路径上
// 被同步调用，且可能持有 planner 之外的锁）；返回错误没有意义，因为警告不能改变
// 调用方流程。
type HookWarningSink func(ctx context.Context, w HookWarning)

var (
	hookWarningMu   sync.RWMutex
	hookWarningSink HookWarningSink
)

// SetHookWarningSink 安装进程级的警告 sink（供 bootstrap / 测试接线）。
// sink 为 nil 恢复默认（一行固定格式日志）。读写并发安全。
func SetHookWarningSink(sink HookWarningSink) {
	hookWarningMu.Lock()
	hookWarningSink = sink
	hookWarningMu.Unlock()
}

// currentHookWarningSink 返回当前 sink（可以为 nil）。
func currentHookWarningSink() HookWarningSink {
	hookWarningMu.RLock()
	sink := hookWarningSink
	hookWarningMu.RUnlock()
	return sink
}

// emitHookWarning 是警告的唯一出口：有 sink 走 sink，没有 sink 写一行固定格式
// 日志。两者都只带 hook 类型、task id 与错误，不含任何 payload 内容。
//
// ctx 允许为 nil：触发点本身不依赖 ctx（payload 里已经有 task id）。
func emitHookWarning(ctx context.Context, w HookWarning) {
	if sink := currentHookWarningSink(); sink != nil {
		sink(ctx, w)
		return
	}
	log.Printf("[WARN] planner: hook %s failed: task=%s err=%v", w.Hook, w.TaskID, w.Err)
}
