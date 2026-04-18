---
mode: plan
cwd: D:/project/CodeFlow/.claude/worktrees/adapter-unification-lane/backend
task: adapter 语义统一收口执行计划
complexity: complex
planning_method: builtin
created_at: 2026-04-12T03:55:00Z
---

# Plan: adapter 语义统一收口执行计划

🎯 任务概述

本计划用于一次性并行收口 adapter 相关缺口，同时严格遵守“复用现有接口、不造平行层、不做补丁摞补丁”的约束。核心策略是把 adapter 继续限定为 provider/protocol + request controls 统一层，把 hook/skill/answer style 的执行语义留在现有 runtime，但通过统一 controls/capabilities 接口完成接线。

📋 执行计划

1. **Wave 0 / 基线审查与分工冻结**
   - 盘点 adapter、config、planning、commander、hotswap 的真实接线点。
   - 明确哪些能力进入 adapter controls，哪些继续留在 runtime。

2. **Wave 1A / adapter request controls 收口**
   - 复用现有 `SendOptions` / `ToolTurnRequest` / `AdapterConfig`，设计共享 request controls 结构。
   - 打通 `Send`、`Stream`、`SendToolTurn` 的 system prompt / model / temperature / max tokens 输入面。

3. **Wave 1B / runtime hot update 语义修复**
   - 修 `BaseAdapter.Configure` 与相关 config merge 语义中的零值判断问题。
   - 确保 runtime 热更新能区分未传与显式值。

4. **Wave 1C / hook-skill-style 边界收口**
   - 不把 hook/skill execution 搬进 adapter。
   - 在现有 runtime 或 config 入口上增加统一 capability/control 接线，并补边界说明与测试。

5. **Wave 2 / 真实链路接入**
   - 优先让 planning orchestrator 与 commander 至少一处消费统一后的 adapter 语义面。
   - 检查 hotswap 是否需要最小跟进以保持配置一致性。

6. **Wave 3 / 验证与回归**
   - 跑 adapter/config/commander/project/hotswap 的定向测试。
   - 必要时补 targeted regression。

7. **Wave 4 / 闭环与清理**
   - 更新 issues CSV 状态。
   - 生成本地提交。
   - 写 phase receipts 和 cleanup receipt。
   - 关闭 team。

## Internal grade

- Grade: XL
- 理由：可以拆成 3 条独立实现波次并行推进，然后在主链做汇合验证。

## Ownership boundaries

- team-lead
  - 冻结需求、计划、边界裁决
  - 做最终汇合、CSV 更新、提交与交付
- adapter-controls
  - 负责 adapter request controls 统一与 provider 接线
- runtime-semantics
  - 负责 hook/skill/style 边界收口与 config/commander/planning 接线分析
- validator-auditor
  - 负责测试矩阵、风险审计、回归证据

## Verification commands

- `go test ./internal/adapters ./internal/commander ./internal/hotswap ./internal/project ./internal/config ./internal/api/handlers`
- `go test ./internal/project -run Planning -count=1`
- `go test ./internal/commander -count=1`
- `go test ./internal/hotswap -count=1`

## Delivery acceptance plan

- 必须看到统一 request controls 实现落地
- 必须看到至少一条 planning/commander 实链复用
- 必须看到测试证据
- 必须看到 CSV 与代码同提交

## Completion language rules

- 没有测试证据不允许说“完成”
- 只允许对已落盘的 requirement/plan/receipt 范围内内容作完成声明

## Rollback rules

- 如果统一 controls 导致 planning/commander 链回退，则优先收缩到最小共享结构，而不是加第二套兼容层
- 如果某语义不适合进入 adapter，则撤回到 runtime 接线，不强行塞入 adapter

## Phase cleanup expectations

- 产出 `phase-*.json`
- 产出 `cleanup-receipt.json`
- team 成员 shutdown
