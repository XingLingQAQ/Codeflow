# Requirement: Go 收口 backend adapter/provider conversion 与请求语义

**本轮只冻结迁移边界与首批切片：将 backend-facing adapter/provider conversion 与 request semantics 收口到 Go，明确 TS runtime/core 继续保留为唯一执行面。**

> 重要前提：
>
> - 不进行 TS runtime/core 整体迁移。
> - 不新建并行 runtime、并行 adapter 抽象或补丁叠补丁式兼容层。
> - 优先复用现有 Go `RequestSemantics` / `RequestControls` / `SendOptions` / `ToolTurnRequest` 与现有 TS runtime 执行入口。

# 目标

**目标是把“服务端语义真相源”和“近端 runtime 执行真相源”分开钉死。**

- Go 侧成为 backend-facing adapter/provider conversion 与请求语义的唯一真相源。
- TS `packages/core` 保留 hook、skill、tool、commander/cowork 的 runtime 执行能力。
- 后续实现阶段优先删除重复转换，而不是叠加新的桥接层。

# 当前事实锚点

**当前代码已经具备 Go 与 TS 的双侧能力，但边界仍需继续收口。**

- Go 已定义统一请求控制与语义面：`internal/adapters/types.go:63`、`internal/adapters/types.go:73`、`internal/adapters/types.go:107`
- Go 已定义原生 tool-turn 请求/响应与可 tool-call 的 adapter 接口：`internal/adapters/types.go:146`、`internal/adapters/types.go:157`、`internal/adapters/types.go:207`
- Go 已有可复用基础适配器与发送控制解析：`internal/adapters/claude.go:16`、`internal/adapters/claude.go:177`、`internal/adapters/claude.go:187`
- TS runtime 仍是 skill 执行入口：`../packages/core/src/cowork/runtime.ts:495`
- TS runtime 仍是 hook 执行入口：`../packages/core/src/hooks/HookManager.ts:26`、`../packages/core/src/hooks/HookManager.ts:75`、`../packages/core/src/hooks/HookManager.ts:146`
- TS runtime 仍是 skill 授权执行入口：`../packages/core/src/tool-runtime/SkillDispatcher.ts:18`、`../packages/core/src/tool-runtime/SkillDispatcher.ts:79`、`../packages/core/src/tool-runtime/SkillDispatcher.ts:130`
- TS headless runtime 仍是共享 registry/executor/skill/hook runtime：`../packages/core/src/tool-runtime/HeadlessToolRuntime.ts:54`
- Commander 仍通过共享 hook manager 接线到 adapter：`../packages/core/src/commander/Commander.ts:23`、`../packages/core/src/commander/Commander.ts:44`

# 迁移范围

**只迁移属于 backend-facing 的转换与语义，不迁移 execution runtime。**

## 纳入范围

1. **Go adapter/provider conversion 收口**
   - backend 调 provider 的请求体、响应体、tool-turn 结构转换
   - send/tool-turn 控制解析与默认值归一
   - backend-facing model/system/semantics/controls 透传与裁剪

2. **Go 请求语义真相源收口**
   - `RequestControls`
   - `RequestSemantics`
   - `SendOptions`
   - `ToolTurnRequest`
   - 与这些结构直接相关的 helper / merge / normalize 方法

3. **Go/TS 重复语义的去重切片**
   - 找出仅为 backend-facing provider conversion 服务、但仍滞留 TS 的字段映射和转换点
   - 将其迁为 Go 真相源或删除重复层

## 明确排除范围

1. `HookManager`
2. `SkillDispatcher`
3. `HeadlessToolRuntime`
4. `Commander`
5. `Cowork runtime`
6. 任何会形成第二条 runtime 执行链的 Go 端 hook/skill 执行实现

# 验收标准

**准备阶段完成的标准，不是代码全部迁完，而是迁移边界与第一批切片已经冻结且可执行。**

1. requirement doc 明确写出 Go 保留面、TS 保留面、非目标与禁止事项
2. execution plan 列出首批迁移 waves、验证命令、回滚规则
3. 至少给出一组“直接可做”的迁移切片，且每一片都能指向现有复用接口
4. 不允许在计划中引入新的平行 adapter/runtime 抽象

# 非目标

**本轮不是做整体语言迁移，也不是把 runtime 逻辑挪进 backend。**

- 不把 hook/skill/tool 的执行逻辑迁入 Go adapter
- 不改前端/TUI 的 runtime 行为
- 不为假设问题增加防御性层层包裹
- 不先做“大统一抽象”再去适配现有代码

# 首批迁移策略原则

**先以“删除重复 + 收口真相源”为中心，而不是“搭桥兼容一切”为中心。**

1. 优先复用 Go 现有类型与解析方法
2. 优先把 backend-facing conversion 收到 Go
3. TS 仅保留 execution runtime 必需接口
4. 如果同一语义 Go 和 TS 都在做转换，优先判断是否属于 backend-facing；若是，则迁 Go
5. 任何迁移切片都应尽量在单一责任边界内完成，避免横向大爆炸

# 手工核对口径

**后续实现前的 spot checks 只需验证边界，不需要假想结果。**

1. Go 的 `RequestSemantics` / `ToolTurnRequest` 是否已覆盖 backend 需要的 provider-facing 语义
2. TS runtime 是否仍是 hook / skill 的唯一执行入口
3. 是否仍存在 backend-facing provider conversion 滞留 TS
4. 首批切片是否可以在不引入新抽象的情况下直接落地
