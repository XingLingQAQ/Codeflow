---
mode: requirement
cwd: D:/project/CodeFlow
task: 当前低风险 UI 收口主线继续执行（Sessions header 操作按钮）
created_at: 2026-03-30
run_id: 2026-03-30-ui-dedup-runtime
---

# Requirement: 当前低风险 UI 收口主线继续执行（Sessions header 操作按钮）

## 目标
在 `codeflow_template/App.tsx` 与新拆分展示组件周边，继续推进低风险 UI 去重与组件化收口。本轮聚焦 `Sessions` 视图 header 的 `Replay / Stop / Retry` 操作按钮组，降低内联展示复杂度，同时保持当前交互行为与视觉语义不变。

## 交付物
- 新增一个小型共享展示组件用于承载 `Sessions` header 操作按钮组
- 将 `App.tsx` 中对应的内联按钮块替换为共享组件调用
- 运行 `pnpm exec vite build` 提供编译验证
- 写入本轮 vibe runtime 阶段产物与 cleanup receipt

## 约束
- 仅处理前端展示层与轻量交互壳
- 不改后端接口、不改业务流程、不新增架构
- 不打断用户、不反复确认
- 不声称未验证的结果
- 不做 git push / PR

## 验收标准
1. 新改动必须是可明确命名的重复展示块收口。
2. `Sessions` header 中的 `Replay / Stop / Retry` 由单一共享组件承载。
3. `cd "D:/project/CodeFlow/codeflow_template" && pnpm exec vite build` 必须通过。
4. 交付说明只覆盖已完成并验证的收口项。

## 产品验收标准
- Sessions 视图 header 的三个操作按钮颜色、文案、圆角和 hover 语义保持一致。
- selectedSessionId 的保护逻辑不回退。

## 手工抽查
- 检查 Sessions 头部 `Replay / Stop / Retry` 的样式与布局
- 检查聊天内容区、trace 列表与 composer 不受影响

## 非目标
- backend plugin 相关改动
- 大范围跨页面重构
- git 提交闭环

## 完成语言规则
- 只允许声明“本轮改造完成”或“本轮收口完成”。
- 未执行 git commit 前，不允许声明“全部完成”或“已闭环交付”。
