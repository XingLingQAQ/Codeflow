---
mode: requirement
cwd: D:/project/CodeFlow
task: Plan 视图低风险 UI 收口继续执行
created_at: 2026-03-29
run_id: 2026-03-29-ui-dedup-runtime
---

# Requirement: Plan 视图低风险 UI 收口继续执行

## 目标
在 `codeflow_template/App.tsx` 及其新拆分展示组件周边，继续推进 Plan 视图的低风险 UI 去重与组件化收口，降低单文件展示复杂度，同时保持当前交互行为与视觉语义基本不变。

## 交付物
- 新增或增强少量可复用展示组件
- 将 `App.tsx` 中重复的展示 JSX 替换为共享组件调用
- 运行 `pnpm exec vite build` 提供编译验证
- 写入 vibe runtime 阶段产物与 cleanup receipt

## 约束
- 仅处理前端展示层与轻量交互壳
- 不改后端接口、不改业务流程、不新增架构
- 不打断用户、不反复确认
- 不声称未验证的结果
- 不做 git push / PR

## 验收标准
1. 新改动必须是可明确命名的重复展示块收口。
2. 每一轮收口后必须通过 `cd "D:/project/CodeFlow/codeflow_template" && pnpm exec vite build`。
3. 组件提取后 `App.tsx` 对应内联块减少，且行为不变。
4. 交付说明只覆盖已完成并验证的收口项。

## 产品验收标准
- Plan 视图中的 section、subsection、badge、按钮、信息面板、文本条目与右栏展示结构继续统一。
- 视觉上不出现明显退化：按钮样式、badge 尺寸、进度展示、节点卡展示保持一致。

## 手工抽查
- Plan 页面中 `Knowledge pack` 的 Export / Import 按钮外观与禁用态
- `Workflow details` 的 Replay 按钮与文本条目
- 右栏 Progress 的 badge 与进度条摘要
- Graph jumps 的节点选择与节点信息卡

## 非目标
- backend plugin 相关改动
- 全仓库大规模组件迁移
- git 提交闭环

## 完成语言规则
- 只允许声明“本轮改造完成”或“本轮收口完成”。
- 未执行 git commit 前，不允许声明“全部完成”或“已闭环交付”。
