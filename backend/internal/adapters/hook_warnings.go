package adapters

import (
	"context"
	"log"
	"sync"

	backendhooks "github.com/codeflow/backend/internal/hooks"
)

// 本文件是 T1.07.b 第 2 组在模型 provider 层的警告出口（§15 T1.07、§28 T1.07.b）。
//
// adapters 是模型 provider 层，不 import internal/execbackend（那会形成反向依赖，
// 见 execbackend/hooks.go 的包注释），所以这里按同一形状复刻 HookWarning /
// HookWarningSink / emitHookWarning：warn 类触发点（HookPostResponse、HookOnStream）
// 的失败只记录、绝不让已经完成的模型调用失败。
//
// 警告的去向：有 sink 走 sink，没有 sink 写一行固定格式日志。日志与警告都只带
// hook 类型与错误，**绝不带响应正文或分片内容**（模型输出可能含用户数据）。
// 警告的持久化（outbox / Run 时间线）需要新的事件类型，封闭的
// run.ExecutionEventTypes 里没有，归 T1.04；今天另一处记录是 hooks 管理器自身
// 的审计（失败写 OutcomeFailure）。

// HookWarning 是一次 hook 失败的报告。Hook 是失败的 hook 类型，Err 是失败原因。
// 它绝不携带响应或分片内容：警告会进日志与审计。
type HookWarning struct {
	// Hook 是失败的 hook 类型。
	Hook backendhooks.HookType
	// Err 是失败原因（hook 管理器返回的错误）。
	Err error
}

// HookWarningSink 接收 hook 警告。实现必须快速返回（它在模型响应路径上被同步
// 调用）；返回错误没有意义，因为警告不能改变调用方流程。
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
// 日志。两者都不包含响应或分片内容。
func emitHookWarning(ctx context.Context, w HookWarning) {
	if sink := currentHookWarningSink(); sink != nil {
		sink(ctx, w)
		return
	}
	log.Printf("[WARN] adapters: hook %s failed: %v", w.Hook, w.Err)
}
