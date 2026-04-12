---
mode: plan
cwd: D:/project/CodeFlow/.claude/worktrees/adapter-unification-lane/backend
task: 收口 backend adapter/provider conversion 与请求语义到 Go
complexity: complex
planning_method: builtin
created_at: 2026-04-12T12:51:13.074604+00:00
---

# Plan: Go 收口 backend adapter/provider conversion 与请求语义

**执行目标是分波次把 backend-facing conversion 与 request semantics 收口到 Go，同时把 TS runtime/core 明确固定在 execution boundary 内。**

> 重要前提：
>
> - 本计划只针对 backend-facing 功能迁移，不触碰 TS runtime/core 的整体迁移。
> - 子代理只做调研与局部执行，不能创建第二套 requirement/plan 真相面。
> - 任何实现都必须优先复用现有接口与 helper，不能补丁叠补丁。

# Internal grade

**本任务使用 XL。**

- root-governed：冻结 requirement/plan，合并结论，定义迁移切片，控制提交边界
- child-governed：并行勘察 Go 真相源、TS 保留边界、重复转换点
- wave-sequential：先调研冻结，再按切片实现，切片内允许有限并行

# Wave structure

## Wave 0：准备与冻结

**产出单一真相面。**

1. 写 `skeleton-receipt.json`
2. 写 `intent-contract.json`
3. 写 requirement doc
4. 写 execution plan
5. team 并行完成三路边界勘察

## Wave 1：识别首批迁移切片

**把“该迁 Go 的部分”压缩成可执行切片。**

1. 收敛 Go backend adapter/provider conversion 真相源
2. 收敛 TS runtime/core 必留边界
3. 建立重复转换对照表
4. 选出第一批切片：
   - request semantics normalization
   - provider request/response conversion
   - tool-turn request shaping

### Wave 1 frozen slices

**三路勘察已收敛，首批切片冻结如下。**

1. **provider canonicalization 只走 Go**
   - Go 真相源：`internal/adapters/types.go:306`、`internal/config/manager.go:155`
   - TS 只消费 canonical provider / resolved channel，不再自己解释 provider alias
   - 受影响 TS 边界：`../packages/core/src/cowork/factory.ts:177`

2. **RequestSemantics / RequestControls 只在 Go 定义**
   - Go 真相源：`internal/adapters/types.go:63`、`internal/adapters/types.go:146`
   - Go 负责把 `system_prompt` / `answer_style` / `capabilities` / declaration controls 收敛为请求语义
   - TS adapter 只消费已解析的 `system/model/maxTokens/temperature`，不再新增并行语义 DTO
   - 受影响 TS 边界：`../packages/core/src/adapters/types.ts:10`、`../packages/core/src/hooks/types.ts:5`

3. **声明性 controls 与执行性 controls 明确分层**
   - Go 负责声明与解析：`internal/config/manager.go:155`
   - TS 负责 enforcement：`../packages/core/src/hooks/types.ts:133`、`../packages/core/src/tool-runtime/SkillDispatcher.ts:130`
   - 迁移方式是映射边界 DTO，不是把 hook/skill runtime 迁入 Go

4. **tool-turn shaping 与 provider payload conversion 收口到 Go adapter**
   - Go 真相源：`internal/adapters/claude.go:334`、`internal/adapters/claude.go:495`、`internal/adapters/claude.go:697`
   - TS 不再承担 backend-facing provider payload 拼装，只保留 runtime 执行与装配

5. **首个编码切片**
   - 先实现“声明性 controls + resolved request semantics handoff”
   - 目标是让 Go 产出稳定的 semantics/control 结果，TS 仅消费执行所需字段，不改 HookManager / SkillDispatcher / HeadlessToolRuntime / Commander / cowork runtime


## Wave 2：实现 Go 收口切片

**以最小切片推进，不引入大统一抽象。**

1. 优先修改 `internal/adapters/*` 及其直接调用链
2. 删除或下沉 backend-facing TS conversion
3. 保持 `packages/core` 的 hook/skill/tool/commander/cowork 运行时不动或仅做边界解耦
4. 每个切片都补最相关的单测或回归验证

### Wave 2 frozen slices

**第二轮已冻结并完成的最小切片如下。**

1. **config provider canonical handoff 只走 Go**
   - 保留 `internal/config/types.go:3` 的对外兼容 provider 值：`anthropic/openai/google/custom`
   - Go 内部 canonical provider 真相源统一委托到 `internal/adapters/types.go:327`
   - `internal/config/types.go:15` 新增 `ToAdapterProvider()` / `AdapterProvider()`，避免继续复制 alias 规则

2. **非法 provider 在 config 冲突检测提前暴露**
   - `internal/config/manager.go:285` 现在会对 `APIChannel.Provider` 做 canonical 校验
   - 非法 provider 不再等到后续 runtime/adapter 构造才失败

3. **TS 继续只保留执行边界**
   - `packages/core/src/config/types.ts:15` 与 `packages/core/src/hotswap/types.ts:14` 的 provider 字面值兼容壳暂不改
   - hook/skill/tool/cowork enforcement 继续留在 TS runtime，不把声明层校验迁进去

4. **本轮验证口径**
   - `go test ./internal/config`
   - `go test ./internal/config ./internal/adapters ./internal/commander ./internal/hotswap`

### Wave 3 frozen slices

**第三轮冻结为 TS provider 类型与 alias 收口，不进入 controls metadata。**

1. **TS provider 声明与运行时 family 改为共享类型**
   - `packages/core/src/config/types.ts:6` 新增 `CanonicalProvider` / `APIChannelProvider` / `RuntimeProviderFamily`
   - `packages/core/src/config/ModelRegistry.ts:2` 改为复用 canonical provider，而不是继续内联联合类型
   - `packages/core/src/hotswap/types.ts:5` 改为复用 runtime provider family，保留 `codex` 作为 runtime executor family

2. **provider alias 规则在 TS 侧集中表达**
   - `packages/core/src/config/types.ts:13` 新增 `toCanonicalProvider()`
   - `packages/core/src/config/types.ts:31` 新增 `toRuntimeProviderFamily()`
   - `claude→anthropic`、`gemini→google`、`codex→openai` 只在 helper 内表达，不再散落字面量

3. **运行时边界保持不变**
   - `packages/core/src/cowork/factory.ts:237` 的 `codex` executor 注册保持不变
   - hook/skill/tool runtime 与 controls metadata 不在本轮改动范围内

4. **本轮验证口径（受限验收）**
   - `tsc --noEmit --target es2022 --module nodenext --moduleResolution nodenext --skipLibCheck` 针对改动文件通过
   - `vitest` 入口缺失，`packages/core` 全量编译受外部依赖类型缺失影响，需在交接中声明限制

### Wave 4 frozen slices

**第四轮冻结为 Go declaration semantics/control handoff，不进入 TS runtime boundary。**

1. **config declaration semantics 进入 ResolvedConfig 真相面**
   - `internal/config/types.go:73` 为 `RoleConfig` 新增 `answer_style` / `capabilities` / `allowed_skills` / `allowed_hooks`
   - `internal/config/types.go:94` 为 `ResolvedConfig` 同步新增声明层语义字段，避免后续 bootstrap 再复制一套 DTO
   - `internal/config/manager.go:155` 在 `ResolveConfig()` 内完成字段聚合与去重

2. **commander 新增从 resolved config 构建 agent 的装配 helper**
   - `internal/commander/commander.go:304` 新增 `BuildAgentConfigFromResolved()`
   - `internal/commander/commander.go:338` 新增 `BuildAgentFromResolved()`，复用 `APIChannel.AdapterProvider()` 与 `adapters.NewAdapter()`
   - `internal/commander/commander.go:376` 新增 `RoleFromConfigRole()`，只映射 `main/coder/sub -> main/coder/sub_expert`

3. **声明性 controls 仅映射到 runtime semantics，不改执行边界**
   - `allowed_skills` / `allowed_hooks` 只进入 `adapters.RequestControls`
   - 不新增 pricing、hotswap capabilities、executor naming 相关逻辑
   - hook/skill/tool/cowork 真实执行仍留在 TS runtime/core

4. **本轮验证口径**
   - `go test ./internal/config ./internal/commander`
   - 重点断言 `ResolveConfig()` 返回 declaration semantics，及 `BuildAgentFromResolved()` 能构建带 adapter 的 `AgentConfig`

### Wave 5 frozen slices

**第五轮冻结为 production bootstrap wiring，不引入全局 commander runtime。**

1. **config 与 agent service 在 main 启动链显式初始化**
   - `cmd/codeflow-server/main.go:50` 新增 `initConfigService()`，不再依赖 handler 首次访问时懒初始化
   - `cmd/codeflow-server/main.go:58` 启动期显式创建并设置 `agent.NewInMemoryAgentService()`
   - 目标是让 config / agent 成为可审计的 startup dependency，而不是隐式全局回退

2. **默认角色 resolve/build 在启动期前置校验**
   - `cmd/codeflow-server/main.go:63` 新增 `registerConfiguredAgents()`
   - 对 `main/coder/sub` 依次执行 `ResolveConfig()`、`RoleFromConfigRole()`、`BuildAgentFromResolved()`
   - 非法 provider 在 server 启动期直接暴露；缺失 API channel 维持兼容并跳过注册

3. **本轮只注册 agent metadata，不把 commander 生产 runtime 整体接入**
   - `cmd/codeflow-server/main.go:80` 仅将构建结果映射为 `agent.Agent` 并注册到 `agent service`
   - `/api/v1/agents` 与 workflow overview 可见默认角色信息，但不新增全局 commander 单例
   - hook/skill/tool/cowork execution boundary 继续留在 TS runtime/core

4. **本轮验证口径**
   - `go test ./cmd/codeflow-server ./internal/config ./internal/commander`
   - 重点断言 `registerConfiguredAgents()` 能注册 3 个默认角色、缺失 API channel 时跳过、非法 provider 时启动失败

## Wave 3：验证与清理

**验证边界真的收口，避免留下双真相源。**

1. 跑 Go 侧 adapter / handler / service 相关测试
2. 跑 TS runtime/core 相关回归，确认 hook/skill/runtime 没被误迁
3. 检查是否还有新的并行语义层或重复 helper
4. 输出 cleanup receipt 与 delivery acceptance report

# Ownership boundaries

**每层只负责自己的真相。**

| 领域 | 归属 | 边界 |
|---|---|---|
| Request semantics / controls | Go | backend-facing 唯一真相源 |
| Provider conversion | Go | backend 调用 provider 的请求/响应与 tool-turn 映射 |
| Hook execution | TS runtime/core | 真实执行入口，不迁入 Go |
| Skill authorization / execution | TS runtime/core | 真实执行入口，不迁入 Go |
| Tool runtime / commander / cowork | TS runtime/core | orchestration boundary，不整体迁移 |

# Verification commands

**验证按切片贴近执行，不做空泛全量跑。**

1. `go test ./internal/adapters/...`
2. `go test ./internal/api/handlers/...`
3. `go test ./internal/workflow/... ./internal/project/...`
4. 针对受影响的 `packages/core` 测试做最小回归，重点确认 runtime boundary 未被误改

# Delivery acceptance plan

**只有当“边界清晰 + 切片可执行 + 有验证口径”同时满足，才允许宣称准备完成。**

1. requirement/plan/receipt 已落盘
2. 三路 team 勘察结果已并入 root 结论
3. 第一批迁移切片可直接进入编码
4. 非目标没有漂移

# Completion language rules

**只对已证实内容下结论。**

- 可以说“迁移准备完成”
- 不可以说“Go 收口迁移已完成”
- 如果测试受限，必须单列受限原因与替代证据

# Rollback rules

**若后续实现发现切片跨越 runtime boundary，则立即回退到本计划定义的边界。**

1. 不把 hook/skill 执行塞进 Go adapter
2. 不在 TS 再长一套 backend-facing 兼容层
3. 出现双真相源时，以 requirement doc 约束为准，删除重复层而非继续叠桥

# Phase cleanup expectations

**每个阶段都要留下可审计产物。**

- `outputs/runtime/vibe-sessions/2026-04-12_20-48-47-go-adapter-migration-prep/skeleton-receipt.json`
- `outputs/runtime/vibe-sessions/2026-04-12_20-48-47-go-adapter-migration-prep/intent-contract.json`
- `outputs/runtime/vibe-sessions/2026-04-12_20-48-47-go-adapter-migration-prep/phase-prep.json`
- `outputs/runtime/vibe-sessions/2026-04-12_20-48-47-go-adapter-migration-prep/cleanup-receipt.json`

# Critical references

- `internal/adapters/types.go:63`
- `internal/adapters/types.go:107`
- `internal/adapters/types.go:146`
- `internal/adapters/claude.go:16`
- `internal/adapters/claude.go:177`
- `../packages/core/src/hooks/HookManager.ts:26`
- `../packages/core/src/tool-runtime/SkillDispatcher.ts:18`
- `../packages/core/src/tool-runtime/HeadlessToolRuntime.ts:54`
- `../packages/core/src/cowork/runtime.ts:495`
- `../packages/core/src/commander/Commander.ts:31`
