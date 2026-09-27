// Package runhooks 是 execbackend.HookPort 在服务端侧的适配器（T1.07.b 第 1 组，
// 计划 §15 T1.07、§28 T1.07.b）。
//
// 它把 execbackend 的规范请求（ToolCallRequest / RunLifecycleRequest）翻译成 hooks
// 包的 payload 契约（ToolHookPayload / RunHookPayload），再触发真实的 hook 管理器。
// 触发点表的四个保留行（HookPreToolUse / HookPostToolUse / HookRunStart /
// HookRunFinish）指向本文件的 (*Port).BeforeTool / AfterTool / BeforeRunStart /
// AfterRunFinish：hooks 的 AST 扫描器（trigger_points_test.go）只承认能证明是
// 管理器的接收者，所以这里必须写 backendhooks.GetHookManager().Trigger(...)，
// 绝不能把管理器存进结构体字段或经本包自己的方法转发。
//
// # 一个事件只有一个触发记录（§15 T1.07 验收断言）
//
// 每个 hook 点按 payload 的 DedupeKey（<hook>:<event_id>）做有界记忆（默认 4096
// 条、按完成顺序先进先出淘汰）加单飞：同一键的第一次调用真正触发并记住结论
// （nil 或错误），并发/重复调用等待并复用同一结论，绝不触发第二次。EventID 为空的
// 请求在记忆之前就被拒绝：before 类返回错误（拒绝），after 类也返回错误（由
// execbackend 层转成警告），空键绝不当成真键。
//
// # 身份构造
//
//   - 工具类（BeforeTool / AfterTool）：actor 是 agent，ID = Run.AgentRevisionID、
//     Source = Backend——工具调用是某个后端在某个 agent 修订上发起的；身份来自
//     服务端权威资源（execbackend.RunRef），不是后端自报字段。
//   - Run 生命周期类（BeforeRunStart / AfterRunFinish）：actor 是 system，ID 与
//     Source 是常量。触发这两个点的是服务端自己（调度器认领、终态迁移），不是 agent
//     的请求；用 Run.AgentRevisionID 会把服务端的决定记成 agent 的行为。
//
// # 失败策略
//
// before 类的任何错误（payload 不合法、hook 拒绝、管理器错误）都原样返回给
// execbackend 层，由它变成拒绝：工具/进程不执行。after 类的错误同样返回，但
// execbackend 只把它变成警告，不改变调用方流程、不重跑已经执行的操作。
package runhooks

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/codeflow/backend/internal/execbackend"
	backendhooks "github.com/codeflow/backend/internal/hooks"
	"github.com/codeflow/backend/internal/run"
)

const (
	// systemActorID 是 Run 生命周期 hook 的 actor.id：触发 RunStart/RunFinish 的
	// 是服务端自己，所以用固定的系统标识，而不是 Run.AgentRevisionID。
	systemActorID = "codeflow-server"
	// systemActorSource 是系统身份的建立来源：进程内的执行宿主（execbackend 端口）。
	systemActorSource = "execbackend-hook-port"
	// defaultMaxEntries 是有界记忆的默认容量（先进先出淘汰）。
	defaultMaxEntries = 4096
)

// Port 是 runhooks 的 HookPort 实现。零值可用（等价于 New()）；唯一的要求是
// 不要复制一个已经用过的 Port（它持有互斥锁与记忆表）。
type Port struct {
	// MaxEntries 是有界记忆的容量；<= 0 时用 defaultMaxEntries。淘汰按完成顺序
	// 先进先出，且绝不淘汰还在飞行中的键。
	MaxEntries int

	mu      sync.Mutex
	entries map[string]*hookInvocation
	order   []string
}

// 编译期断言：Port 必须实现 execbackend 的端口（接线漏一个方法就编译失败）。
var _ execbackend.HookPort = (*Port)(nil)

// New 返回一个可用的 Port。
func New() *Port { return &Port{} }

// hookInvocation 是一次单飞的结论：done 关闭后 err 就是那次触发的结论，等待者
// 读到的永远是同一份结论。
type hookInvocation struct {
	done chan struct{}
	err  error
}

// BeforeTool 触发 HookPreToolUse（工具请求进入 Guard 之前）。payload 不合法、
// EventID 为空或 hook 拒绝时返回非 nil，调用方必须因此不执行工具。
func (p *Port) BeforeTool(ctx context.Context, req execbackend.ToolCallRequest) error {
	payload, err := preToolPayload(req)
	if err != nil {
		return err
	}
	return p.once(ctx, payload.DedupeKey(backendhooks.HookPreToolUse), func() error {
		_, err := backendhooks.GetHookManager().Trigger(ctx, backendhooks.HookPreToolUse, payload)
		return err
	})
}

// AfterTool 触发 HookPostToolUse（工具结果净化之后）。错误只是警告：post 观察的
// 是已经发生的事，绝不重跑工具。
func (p *Port) AfterTool(ctx context.Context, req execbackend.ToolCallRequest, result execbackend.ToolCallResult) error {
	payload, err := postToolPayload(req, result)
	if err != nil {
		return err
	}
	return p.once(ctx, payload.DedupeKey(backendhooks.HookPostToolUse), func() error {
		_, err := backendhooks.GetHookManager().Trigger(ctx, backendhooks.HookPostToolUse, payload)
		return err
	})
}

// BeforeRunStart 触发 HookRunStart（Run 被认领、进程启动之前）。错误 = 不启动进程。
func (p *Port) BeforeRunStart(ctx context.Context, req execbackend.RunLifecycleRequest) error {
	payload := runHookPayload(req)
	if err := payload.Validate(backendhooks.HookRunStart); err != nil {
		return err
	}
	return p.once(ctx, payload.DedupeKey(backendhooks.HookRunStart), func() error {
		_, err := backendhooks.GetHookManager().Trigger(ctx, backendhooks.HookRunStart, payload)
		return err
	})
}

// AfterRunFinish 触发 HookRunFinish（Run 到达终态之后）。错误只是警告：Run 的
// 终态不因 hook 失败而改变。
func (p *Port) AfterRunFinish(ctx context.Context, req execbackend.RunLifecycleRequest) error {
	payload := runHookPayload(req)
	if err := payload.Validate(backendhooks.HookRunFinish); err != nil {
		return err
	}
	return p.once(ctx, payload.DedupeKey(backendhooks.HookRunFinish), func() error {
		_, err := backendhooks.GetHookManager().Trigger(ctx, backendhooks.HookRunFinish, payload)
		return err
	})
}

// once 是"一个事件只有一个触发记录"的执行体：key 的第一次调用运行 run 并记住
// 结论，其余调用（并发或之后到达）等待并复用同一结论。key 为空时不做记忆（调用
// 方已经拒绝过空 EventID，这里是防御）。
//
// 记忆是有界的（MaxEntries，先进先出淘汰）：淘汰之后同一个键可以再次触发。容量
// 远大于同时在飞的事件数，所以"一个事件只有一个触发记录"在正常负载下成立；这是
// 有界内存换取的、写在明处的取舍。
//
// handler panic 不是结论：它被记成错误（fail-closed）并重新抛出。等待者因此不会
// 永久阻塞，也不会把 panic 当成放行（nil 结论）。
func (p *Port) once(ctx context.Context, key string, trigger func() error) error {
	if key == "" {
		return trigger()
	}

	p.mu.Lock()
	if p.entries == nil {
		p.entries = map[string]*hookInvocation{}
	}
	if entry, ok := p.entries[key]; ok {
		p.mu.Unlock()
		select {
		case <-entry.done:
			return entry.err
		case <-ctx.Done():
			// 等待单飞结论时调用方 ctx 取消：只影响这次等待，不产生第二次触发。
			return ctx.Err()
		}
	}
	entry := &hookInvocation{done: make(chan struct{})}
	p.entries[key] = entry
	p.mu.Unlock()

	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				entry.err = fmt.Errorf("runhooks: hook trigger for %s panicked: %v", key, recovered)
				close(entry.done)
				p.remember(key)
				panic(recovered)
			}
			close(entry.done)
			p.remember(key)
		}()
		entry.err = trigger()
	}()
	return entry.err
}

// remember 记录一个已经完成的键并按容量先进先出淘汰。只有完成的键会进入淘汰
// 队列：飞行中的键绝不会被淘汰（否则同一个事件可能被触发第二次）。
func (p *Port) remember(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.order = append(p.order, key)
	limit := p.MaxEntries
	if limit <= 0 {
		limit = defaultMaxEntries
	}
	for len(p.order) > limit {
		oldest := p.order[0]
		p.order = p.order[1:]
		delete(p.entries, oldest)
	}
}

// preToolPayload 构造并校验 HookPreToolUse 的 payload（Result 必须为 nil）。
func preToolPayload(req execbackend.ToolCallRequest) (backendhooks.ToolHookPayload, error) {
	payload := backendhooks.ToolHookPayload{
		Identity:           toolHookIdentity(req),
		EventID:            req.EventID,
		ToolCallID:         req.ToolCallID,
		RequestFingerprint: req.Fingerprint,
		Tool:               req.Tool,
		Arguments:          append(json.RawMessage(nil), req.Arguments...),
	}
	if err := payload.Validate(backendhooks.HookPreToolUse); err != nil {
		return backendhooks.ToolHookPayload{}, err
	}
	return payload, nil
}

// postToolPayload 构造并校验 HookPostToolUse 的 payload。Arguments 留给 pre 上报
// （post 观察的是结果），Result 只带引用与状态，不带原文。
func postToolPayload(req execbackend.ToolCallRequest, result execbackend.ToolCallResult) (backendhooks.ToolHookPayload, error) {
	payload := backendhooks.ToolHookPayload{
		Identity:           toolHookIdentity(req),
		EventID:            req.EventID,
		ToolCallID:         req.ToolCallID,
		RequestFingerprint: req.Fingerprint,
		Tool:               req.Tool,
		Result: &backendhooks.ToolHookResult{
			Status:    backendhooks.ToolHookResultStatus(result.Status),
			ExitCode:  result.ExitCode,
			OutputRef: result.OutputRef,
			Truncated: result.Truncated,
		},
	}
	if err := payload.Validate(backendhooks.HookPostToolUse); err != nil {
		return backendhooks.ToolHookPayload{}, err
	}
	return payload, nil
}

// runHookPayload 构造 RunStart/RunFinish 的 payload（各自 Validate 之后再触发）。
func runHookPayload(req execbackend.RunLifecycleRequest) backendhooks.RunHookPayload {
	return backendhooks.RunHookPayload{
		Identity:       runHookIdentity(req),
		EventID:        req.EventID,
		Status:         run.RunStatus(req.Status),
		PreviousStatus: run.RunStatus(req.PreviousStatus),
	}
}

// toolHookIdentity 构造工具类 hook 的身份：actor 是 agent（ID = Run 的 agent 修订，
// Source = 后端标识）。三个可选 ID 全部给出：工具事件要求 run+attempt+agent_revision
// （§28"tool 必须有 attempt"）。
func toolHookIdentity(req execbackend.ToolCallRequest) run.ExecutionIdentity {
	runID := req.Run.RunID
	attemptID := req.Run.AttemptID
	revisionID := req.Run.AgentRevisionID
	return run.ExecutionIdentity{
		ProjectID:       req.Run.ProjectID,
		RunID:           &runID,
		AttemptID:       &attemptID,
		AgentRevisionID: &revisionID,
		Actor: run.Actor{
			Type:   run.ActorTypeAgent,
			ID:     revisionID,
			Source: req.Backend,
		},
	}
}

// runHookIdentity 构造 Run 生命周期 hook 的身份：actor 是 system（常量），因为
// 触发点是服务端自己的调度/终态迁移，不是 agent 的请求。
//
// attempt 与 agent 修订只在给出时才放进身份：排队阶段就被取消或到期的 Run 没有
// attempt，身份里必须是缺省（nil）而不是空字符串——空字符串会被 payload.Validate
// 拒绝，这类 Run 的结束 hook 就永远不会触发。RunStart 缺它们时由 payload.Validate
// 拒绝（认领之后才会触发 RunStart）。
func runHookIdentity(req execbackend.RunLifecycleRequest) run.ExecutionIdentity {
	runID := req.Run.RunID
	identity := run.ExecutionIdentity{
		ProjectID: req.Run.ProjectID,
		RunID:     &runID,
		Actor: run.Actor{
			Type:   run.ActorTypeSystem,
			ID:     systemActorID,
			Source: systemActorSource,
		},
	}
	if attemptID := req.Run.AttemptID; attemptID != "" {
		identity.AttemptID = &attemptID
	}
	if revisionID := req.Run.AgentRevisionID; revisionID != "" {
		identity.AgentRevisionID = &revisionID
	}
	return identity
}
