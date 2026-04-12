---
mode: plan
cwd: D:/project/CodeFlow/.claude/worktrees/adapter-unification-lane/backend
task: adapter 语义统一收口
complexity: complex
planning_method: builtin
created_at: 2026-04-12T03:50:00Z
---

# Requirement: adapter 语义统一收口

🎯 任务概述

本轮目标是在不新建平行抽象的前提下，一次性并行收口 adapter 的关键缺口。当前 `backend/internal/adapters` 已完成 Claude 优先的协议层，但 system prompt、response controls、runtime hot update、hook/skill/answer style 的边界仍分散在 config、commander、planning orchestrator、hotswap 与 packages/core 之间。

本轮必须复用现有 `ICliAdapter`、`ToolCallableAdapter`、`ConfigManager`、`ResolvedConfig`、`Commander`、`HotSwapManager` 与既有 planning 接线，不做补丁摞补丁式修改。

## 冻结需求

1. adapter 必须继续承担协议/provider 转换职责，但要补齐统一的 system prompt 与 response controls 输入面。
2. adapter runtime 热更新必须修正零值判断问题，至少让 temperature/max_tokens 等控制项具备明确“未传 vs 显式值”语义。
3. hook / skill / answer style 不应粗暴塞进 adapter 执行层；本轮需要把它们和 adapter 的边界、接线点收口到现有 runtime。
4. planning 或 commander 至少一条真实链路要改为消费统一后的 adapter 语义面，而不是继续走分裂接线。
5. 必须补充对应测试与最小验证证据。

## 验收口径

- `Send` / `Stream` / `SendToolTurn` 共享统一的 request controls 结构，至少覆盖 system prompt、model、temperature、max_tokens。
- adapter Configure 或等价热更新入口不再使用零值判断区分未传字段。
- hook / skill / answer style 的控制信息有清晰归属：该在 adapter capability/controls 层的进入该层，不该进入的明确留在 runtime，并完成实际接线。
- planning / commander / hotswap 至少一处复用新统一面并通过测试。
- 不新增平行 runtime、平行 config 模型或额外 front-end 假语义。

## 非目标

- 不在本轮实现 OpenAI/Gemini/Codex provider 全量 adapter。
- 不把 packages/core 的整套 skill runtime 迁移到 Go adapter。
- 不重做 Settings 页面。

## 关键参考

- `backend/internal/adapters/types.go`
- `backend/internal/adapters/claude.go`
- `backend/internal/config/manager.go`
- `backend/internal/commander/types.go`
- `backend/internal/commander/commander.go`
- `backend/internal/hotswap/manager.go`
- `backend/internal/adapters/adapters_test.go`
