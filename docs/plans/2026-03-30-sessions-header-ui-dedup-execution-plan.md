---
mode: plan
cwd: D:/project/CodeFlow
task: 当前低风险 UI 收口主线继续执行（Sessions header 操作按钮）
complexity: medium
planning_method: builtin
created_at: 2026-03-30
run_id: 2026-03-30-ui-dedup-runtime
---

# Plan: 当前低风险 UI 收口主线继续执行（Sessions header 操作按钮）

## internal_grade
L

## wave_structure
1. 冻结 skeleton / intent / requirement / plan 产物。
2. 提取 `Sessions` header 操作按钮组为单一共享组件。
3. 替换 `App.tsx` 中内联 `Replay / Stop / Retry` 按钮块。
4. 清理本轮顺手发现的未直接使用 import。
5. 运行 `pnpm exec vite build` 验证。
6. 写入 `phase-plan-execute.json` 与 `cleanup-receipt.json`。

## ownership_boundaries
- 只改 `codeflow_template/App.tsx`
- 只新增 `codeflow_template/components/*` 下的小型展示组件
- 只写入本轮 `docs/requirements/*`、`docs/plans/*` 与 `outputs/runtime/vibe-sessions/2026-03-30-ui-dedup-runtime/*`
- 不碰 backend / packages/core / node_modules 的既有改动

## verification_commands
1. `cd "D:/project/CodeFlow/codeflow_template" && pnpm exec vite build`

## delivery_acceptance_plan
- 记录本轮新增组件与替换位置
- 仅汇报已实际验证通过的收口项
- 明确说明 git 尚未提交

## completion_language_rules
- 构建通过且 receipt 写入后可说“本轮改造完成”
- 不可说“已闭环交付”或“已提交 git”
- 未做浏览器人工验证不可说“视觉完全验证完成”

## rollback_rules
- 若新组件导致构建失败，回滚到当前 header 原始按钮实现
- 若替换范围不明确，缩小到按钮组容器本身

## cleanup_expectations
- 写入 `outputs/runtime/vibe-sessions/2026-03-30-ui-dedup-runtime/phase-plan-execute.json`
- 写入 `outputs/runtime/vibe-sessions/2026-03-30-ui-dedup-runtime/cleanup-receipt.json`
- 不保留临时脚本
- 不生成第二份 requirement/plan 真相面以外的冗余文档

## next_execution_target
优先完成 `Sessions` header 的 `Replay / Stop / Retry` 操作按钮组收口，保持单轮单目标、小步可验证推进。
