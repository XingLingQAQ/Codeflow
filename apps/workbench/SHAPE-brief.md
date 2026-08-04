# Shape Brief — CodeFlow Workbench 全界面重构

> 基线：PRODUCT.md（2026-08-03 确认）。适配目标：apps/workbench 全部页面。本轮为 shape 产出，设计简报。

## 1. Job & Audience
- 谁：独立开发者 + 团队项目负责人（共享同一张工作台）。
- 情境：桌面（1280+），连续沉浸式编码会话。
- 需求：从当前 stage 到下一 stage 或 gate 审批/回退，AI 帮忙但不代替决策。
- 模式：Operate（任务完成型），嵌入一层 Experience——Flow 驱动叙事本身属于产品化体验。

## 2. Outcome & Proof
- 主任务：完成当前阶段（advance/skip/loop）、审批 Gate、AI 对话 + 文件编辑、提交到快照。
- 成功指征：一屏内看到 Flow 结构（七节点）+ 事件流 + 当前产物 + 决策点；编码画布足以支撑真实工作；所有指标对应真实状态。

## 3. Selected Direction
### 3.1 「驾驶舱 + 轨迹」双 Spine
Flow Spine（七节点结构进度，TopBar FlowProgress）+ Activity Spine（事件时间线，BottomPanel 日志页），共享同一 flow:project:{id} WS 订阅。
### 3.2 信息分层
顶部 Flow Spine / Left Flow Rail 七阶段垂直轴 / Center Stage Canvas 随阶段变形 / Right Agent Companion 常驻 / 底部 Activity Spine 事件时间线+守卫审批+终端。
### 3.3 Visual-World
既定：LogoMark S 曲线+两节点渐变是签名；黑白/油墨/实色+语义色；玻璃/发丝/微升保持；动画指纹 cubic-bezier(0.32,0.72,0,1)/150/250/400ms 三档不变。渐变色紫→cobalt 属 new-work 待决项。

## 4. Scope & Boundaries
- 范围：apps/workbench 全部页面 + 启动门 + 命令面板；可改 smoke 脚本。
- 禁：backend/docs/.claude；新运行时依赖；永久 shimmer/硬编码占位；紫色；mock 数据进生产构建。
- 执行序：P0-6（阶段推进）→ P1（时间线/字体/动画/状态栏/画布去骨架）→ P2（命令面板扩展/编辑器 QoL）。

## 5. States & Ranges
Flow active/active+waiting_gate/done；后端不可用（mock 可用；online 显示未启用）；<768 引导页；768~1280 折叠 rail；主题切换同步背景；reduced-motion 全链路；Chat 无后端时 EmptyState。

## 6. Interaction & Layout
- TopBar：项目切换 + FlowProgress + 三面板开合按钮 + 连接徽章。
- Flow Rail：LogoTile 锚点 + 七阶段垂直轴 + 底部 StageActions 卡片（advance/skip/loop；waiting_gate 整卡变审批面板）。
- Center：每阶段独立 canvas；编码画布多 Tab、Diff/暂存区、选区引用 chip。
- Right Companion：对话气泡 + 上下文 chips（阶段+文件）+ 自适应 Composer。
- Bottom Panel 五 Tab：终端/日志/审计/问题/守卫（阶段联动默认页）；守卫含豁免审批。
- StatusBar：阶段/模型/Token/快照/守卫灯/连接。

## 7. Constraints & Open Decisions
已决：平台 web、黑白+浅色默认+石墨深色、Chat mock-first、动画三档单缓动、字体四族。未决（new-work 所有）：渐变色紫→cobalt 纠偏；视觉其余由 new-work 流程定。