---
mode: plan
cwd: D:/project/CodeFlow
task: Plan 视图低风险 UI 收口继续执行
complexity: medium
planning_method: builtin
created_at: 2026-03-29
run_id: 2026-03-29-ui-dedup-runtime
---

# Plan: Plan 视图低风险 UI 收口继续执行

## internal_grade
L

## wave_structure
1. 冻结 requirement 与 runtime receipt。
2. 继续挑选单一低风险重复展示块。
3. 提取/增强共享组件并替换 `App.tsx` 内联 JSX。
4. 运行 `pnpm exec vite build` 验证。
5. 写入 phase receipt 与 cleanup receipt。

## ownership_boundaries
- 只改 `codeflow_template/App.tsx`
- 只新增 `codeflow_template/components/*` 下的小型展示组件
- 不碰 backend / packages/core / node_modules 的既有改动

## verification_commands
1. `cd "D:/project/CodeFlow/codeflow_template" && pnpm exec vite build`

## delivery_acceptance_plan
- 记录本轮新增组件与替换位置
- 仅汇报已实际验证通过的收口项
- 明确说明 git 尚未提交

## completion_language_rules
- 构建通过后可说“本轮改造完成”
- 未提交 git 不可说“已闭环交付”
- 未做人工浏览器验证不可说“视觉完全验证完成”

## rollback_rules
- 若新组件导致构建失败，回滚到上一轮通过的展示实现
- 若替换范围不明确，优先缩小到单一区块

## cleanup_expectations
- 写入 `phase-plan-execute.json`
- 写入 `cleanup-receipt.json`
- 不保留临时脚本
- 不生成多余 requirement/plan 真相面

## next_execution_target
优先继续识别 `App.tsx` 中剩余可复用的纯展示块，保持单轮小步、可验证推进。
