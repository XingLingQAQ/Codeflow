# CodeFlow 前端功能清单

> 说明：当前默认产品前端已裁决为 `apps/workbench`（`@codeflow/workbench`；历史 `codeflow_template` 已于 PR-3 删除；G01 已 rename desktop→workbench）。本文档记录的是 `packages/gui` 历史组件资产，不再代表默认浏览器、E2E、Tauri 或交付入口。

## 概述

本文档覆盖的历史 GUI 组件资产基于 **React + TypeScript** 构建，主要沉淀在 `packages/gui`。当前默认产品前端为 `apps/workbench`，它承接浏览器与 Tauri/sidecar 双模式工作台。

---

## 前端组件清单

### 1. Chat 组件 - 对话界面

**路径**: `packages/gui/src/components/Chat/`

**子组件**:
- `ChatContainer` - 主容器
- `ChatList` - 消息列表
- `ChatBubble` - 单条消息气泡
- `ChatInput` - 输入框

**功能特性**:
- ✅ 实时对话交互
- ✅ 流式输出渲染
- ✅ 消息重试机制
- ✅ 代码高亮显示
- ✅ Markdown 渲染

---

### 2. Timeline 组件 - Git 时间轴

**路径**: `packages/gui/src/components/Timeline/`

**子组件**:
- `TimelineView` - 主视图
- `TimelineItem` - 单条记录
- `FileChangeList` - 文件变更列表

**功能特性**:
- ✅ 按时间倒序显示代码快照
- ✅ 按日期分组展示
- ✅ 一键回滚到历史版本
- ✅ 显示文件变更列表（新增/修改/删除）
- ✅ 加载更多支持（分页）
- ✅ 快照详情预览

---

### 3. Graph 组件 - 知识图谱可视化

**路径**: `packages/gui/src/components/Graph/`

**子组件**:
- `GraphView` - 图谱渲染主视图
- `GraphControls` - 控制面板
- `NodeTooltip` - 节点提示框

**功能特性**:
- ✅ Cytoscape.js 动态布局
- ✅ 大数据集分片加载（>1000 节点优化）
- ✅ 节点/边点击交互
- ✅ 缩放、平移、框选
- ✅ 自定义样式表
- ✅ 节点类型图标映射
- ✅ 关系路径高亮

---

### 4. MemoryDashboard 组件 - 记忆管理仪表盘

**路径**: `packages/gui/src/components/MemoryDashboard/`

**布局**:
- 左栏：STM（短期记忆池）
- 右栏：LTM（长期规则库）

**功能特性**:
- ✅ 拖拽归档记忆（STM → LTM）
- ✅ 惊喜度评分显示（颜色条可视化）
- ✅ 热度评分展示
- ✅ 按时间/热度/惊喜度排序
- ✅ 双击跳转到原始对话
- ✅ 记忆标签管理
- ✅ 批量操作支持

---

### 5. MemoryIndicator 组件 - 记忆指示器

**路径**: `packages/gui/src/components/MemoryIndicator/`

**功能特性**:
- ✅ Token 使用量实时显示
- ✅ 内存占用监控
- ✅ 上下文窗口使用率
- ✅ 告警阈值提示

---

### 6. ContextBuilder 组件 - 动态上下文构建器

**路径**: `packages/gui/src/components/ContextBuilder/`

**布局**:
- 左侧：文件树（支持展开/折叠）
- 右侧：AST 树（函数/类/变量级别勾选）
- 底部：Token 预算显示（饼图 + 进度条）

**功能特性**:
- ✅ 文件树浏览与选择
- ✅ AST 节点级别勾选
- ✅ 批量勾选与反选
- ✅ 上下文预设保存/加载
- ✅ 实时 Token 计数
- ✅ Token 预算可视化
- ✅ AST 节点类型图标映射（函数/类/变量/接口等）

---

### 7. SemanticSearch 组件 - 语义检索引擎

**路径**: `packages/gui/src/components/SemanticSearch/`

**搜索模式**:
- 向量搜索
- 全文搜索
- 图谱关联搜索
- 混合搜索（三位一体）

**功能特性**:
- ✅ 混合搜索（向量权重 + 全文权重 + 图谱权重）
- ✅ 搜索历史记录
- ✅ 结果排序（相关度/时间/热度）
- ✅ 搜索结果卡片展示
- ✅ 结果预览与跳转
- ✅ 高级筛选选项

---

### 8. HotSwap 组件 - 模型热切换

**路径**: `packages/gui/src/components/HotSwap/`

**子组件**:
- `HotSwapDropdown` - 下拉菜单
- `StatusIndicator` - 状态指示器
- `ModelCard` - 模型信息卡片

**模型状态**:
- 🟢 online - 在线可用
- 🟡 degraded - 降级运行
- 🔴 offline - 离线
- 🔄 switching - 切换中

**功能特性**:
- ✅ 实时模型状态显示
- ✅ 模型能力展示（上下文窗口、支持功能）
- ✅ 一键切换模型
- ✅ 重试机制
- ✅ 切换历史记录

---

### 9. NestedConversation 组件 - 嵌套子对话

**路径**: `packages/gui/src/components/NestedConversation/`

**功能特性**:
- ✅ 递归组件设计（支持多层嵌套）
- ✅ 子对话独立折叠
- ✅ 中断子智能体执行
- ✅ 实时流式渲染
- ✅ 执行轨迹可视化
- ✅ 子对话状态指示

---

### 10. AgentBoard 组件 - 多智能体协作看板

**路径**: `packages/gui/src/components/AgentBoard/`

**布局**:
- 顶部：活跃 Agent 卡片（Main/Coder/Sub/Critic）
- 中间：黑板区域（Blackboard）显示共享状态
- 底部：BFT 共识投票进度

**Agent 角色**:
| 角色 | 职责 |
|------|------|
| Main | 主协调者，任务分解与调度 |
| Coder | 代码生成与修改 |
| Sub | 子任务执行者 |
| Critic | 代码审查与质量把控 |

**功能特性**:
- ✅ 实时 Agent 状态监控
- ✅ 点击展开详细日志
- ✅ 投票结果可视化
- ✅ 黑板条目实时更新
- ✅ Agent 通信轨迹

---

### 11. DebateView 组件 - 辩论式校验界面

**路径**: `packages/gui/src/components/DebateView/`

**布局**:
- 左栏：Generator（生成者）
- 右栏：Critic（批评者）
- 中间：时间轴显示多轮"生成-批判-精炼"过程

**功能特性**:
- ✅ 冲突点高亮显示
- ✅ 修正建议 Tooltip
- ✅ 完整辩论过程回放
- ✅ 审计报告导出
- ✅ 多轮迭代可视化
- ✅ 最终方案选择

---

### 12. PlanBoard 组件 - 任务看板

**路径**: `packages/gui/src/components/PlanBoard/`

**功能特性**:
- ✅ 任务列表管理
- ✅ 优先级设置（P0/P1/P2）
- ✅ 任务状态流转（待办/进行中/已完成）
- ✅ 模型批量切换
- ✅ 任务拖拽排序
- ✅ 任务依赖关系

---

### 13. 其他辅助组件

**路径**: `packages/gui/src/components/`

| 组件 | 功能 |
|------|------|
| `Sidebar` | 侧边导航栏 |
| `Header` | 顶部工具栏 |
| `StatusBar` | 底部状态栏 |
| `Modal` | 通用弹窗 |
| `Toast` | 消息提示 |
| `Loading` | 加载状态 |
| `ErrorBoundary` | 错误边界 |

---

## 前端状态管理

**路径**: `packages/gui/src/stores/`

| Store | 职责 |
|-------|------|
| `chatStore` | 对话状态管理 |
| `memoryStore` | 记忆状态管理 |
| `contextStore` | 上下文状态管理 |
| `agentStore` | 智能体状态管理 |
| `configStore` | 配置状态管理 |
| `uiStore` | UI 状态管理 |

---

## 前端 Hooks

**路径**: `packages/gui/src/hooks/`

| Hook | 功能 |
|------|------|
| `useChat` | 对话交互逻辑 |
| `useMemory` | 记忆操作逻辑 |
| `useContext` | 上下文构建逻辑 |
| `useSearch` | 搜索逻辑 |
| `useWebSocket` | WebSocket 连接管理 |
| `useHotSwap` | 模型切换逻辑 |
| `useAgent` | 智能体交互逻辑 |

---

## 前端路由

**路径**: `packages/gui/src/routes/`

| 路由 | 页面 |
|------|------|
| `/` | 主对话界面 |
| `/memory` | 记忆管理仪表盘 |
| `/context` | 上下文构建器 |
| `/search` | 语义搜索 |
| `/agents` | 智能体看板 |
| `/timeline` | Git 时间轴 |
| `/graph` | 知识图谱 |
| `/settings` | 设置页面 |

---

## 技术栈详情

| 技术 | 版本 | 用途 |
|------|------|------|
| Electron | 33+ | 桌面应用框架 |
| React | 19 | UI 框架 |
| TypeScript | 5.9 | 类型安全 |
| Cytoscape.js | 3.x | 图谱可视化 |
| Monaco Editor | 0.52+ | 代码编辑器 |
| Zustand | 5.x | 状态管理 |
| React Query | 5.x | 数据请求 |
| Tailwind CSS | 4.x | 样式框架 |
| Framer Motion | 11.x | 动画效果 |

---

## 组件依赖关系

```
App
├── Header
│   ├── HotSwap
│   └── MemoryIndicator
├── Sidebar
│   └── Navigation
├── MainContent
│   ├── Chat
│   │   ├── ChatList
│   │   │   ├── ChatBubble
│   │   │   └── NestedConversation
│   │   └── ChatInput
│   ├── MemoryDashboard
│   ├── ContextBuilder
│   ├── SemanticSearch
│   ├── AgentBoard
│   ├── DebateView
│   ├── PlanBoard
│   ├── Timeline
│   └── Graph
└── StatusBar
```

---

## 功能统计

| 类别 | 数量 |
|------|------|
| 核心组件 | 13 |
| 辅助组件 | 7+ |
| 状态 Store | 6 |
| 自定义 Hooks | 7+ |
| 页面路由 | 8 |

---

## 开发指南

### 启动前端开发服务器

```bash
cd packages/gui
pnpm install
pnpm dev
```

### 构建生产版本

```bash
pnpm build
```

### 运行测试

```bash
pnpm test
```

### 代码规范检查

```bash
pnpm lint
```
