# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

（同一 workbench React 构建产物同时作为 ① backend/internal/web embed 网页版 与 ② Tauri 桌面壳内嵌分发。交付主体是网页应用，Tauri 是包装器，不改变设计语言归属。断点 ≥1280 全功能，768~1280 折叠 Flow Rail / Agent 伴侣抽屉化，<768 引导使用移动伴侣端。）

## Users

主用户 = 泛用：独立开发者 + 团队项目负责人。

- 独立开发者：以编辑代码/文档为主，希望 AI 助手帮自己走完从想法到提交的完整工程流程。
- 团队项目负责人/技术负责人：关心规范门控（Gate）、人工审批停车点、守卫豁免、快照回滚与审计流水。

两者共享同一张工作台，不拆角色视图。

## Product Purpose

CodeFlow 是流程中心（flow-centric）的 Agentic IDE。它让用户驱动一条七阶段的工作流而不是操作一堆文件；阶段画布随当前阶段整体变形，Agent 伴侣随阶段切换系统提示词。成功 = 用户能从"零想法"一路驶通到提交，且全程可查审计与守卫状态。

## Positioning

与 VS Code 的文件中心相对立：顶层导航是 Flow 七阶段，中央 Stage Canvas 随阶段变形，Agent Companion 跨阶段常驻并自动切换系统提示词，底层用原子快照把"代码-对话-向量"绑定为可回滚单元。阶段画布 + 门控（block/escalate_to_debate）+ 快照绑定 + 守卫豁免审批 + 多方辩论升级，这套编排是产品身份。

## Operating Context

- 网页版（backend embed）与 Tauri 桌面壳共一份 apps/workbench 构建。
- 后端：Go + Gin + SQLite（CGO），internal/workspace、floweng、guard、debate、snapshot、agent registry、WS 事件总线。
- 工作区写操作三态：list/read / write(stage|direct) / staged→promote/discard，全部经 guard 拦截。
- AI 对话当前为 mock-first（后端无 chat HTTP 端点），是产品事实不是缺陷。
- 手机版定位为远程控制与监督终端。

## Capabilities and Constraints

- 七阶段画布：intent/design/planning/research/coding/review/submit（research 可选）。
- 编码画布（M3 已大半交付）：文件树懒加载、多 Tab 编辑、暂存区、行级 Diff、上下文构建器、守卫面板、promote/discard。
- 流程驱动：advance/skip/loop + Gate 审批；FlowProgress 结构进度 + 活动时间线双视图。
- Agent 伴侣：按阶段推荐/记忆选中（5 内置 agent），上下文注入、流式、停止、历史持久化。
- WS 实时、状态栏真实数据（阶段/模型/Token/快照/守卫灯/连接）、审计流水。
- 约束：黑白简约 + 浅色默认 + 石墨深色；mock 层仅限开发环境。

## Brand Commitments

- 名称固定为 "CodeFlow"。
- 上升流元素：LogoMark S 曲线 + 两节点渐变描边不可弃。
- 渐变为唯一签名色，仅用于 Logo/FlowProgress/启动页。
- 紫色禁止：oklch hue ≈ 280-290 的 indigo/violet 不在产品 UI 中出现。当前 #7c5cff 残留为待纠偏项。
- AI/Chat 为顶层身份：产品观感向"以 AI 对话驱动工程"收敛，不做拟物化装饰。

## Evidence on Hand

- apps/workbench/src/*（工作台 + 7 个阶段画布 + 5 内置 agent + 文件编辑/暂存/Diff）。
- docs/design/*.md 目标态设计。
- backend/internal/* 各 registry + WS 总线。
- audit/ 截图证据。

## Product Principles

1. 流程可驶通是首要：任何交互先保证能从当前阶段一路到提交，再谈表现；假骨架判为 bug。
2. 零虚假数据：连接、守卫灯、阶段、模型、快照、审计都读真实状态；取不到就明确隐藏。
3. AI 驱动是身份：Agent 伴侣必须能正常对话；阶段自动同步系统提示词是产品含义。
4. 检查胜于描述：快照/审批/守卫豁免全部走真实 record，永不以 toast 代替状态迁移。
5. 设计为表现服务：动画目的是表达状态变换，不是颜值。

## Accessibility & Inclusion

- 屏幕阅读器兼容（Radix 基座）。
- reduced-motion 由 html.cf-reduced 与 MotionConfig 双路压制。
- 键鼠等价：面板开合、编辑器 tab、命令面板均可纯键盘。
- 中文为默认 UI 语言。