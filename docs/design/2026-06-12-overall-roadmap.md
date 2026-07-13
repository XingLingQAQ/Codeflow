# CodeFlow 2.0 总体规划与路线图

> 创建：2026-06-12  
> 状态：Active（设计目标态；实现进度以 2.0 实施计划与 feature-parity-matrix 为准）  
> 纳入跟踪：2026-07-13（M0.6）  
> 关联：`flow-engine.md`、`workbench-and-shell.md`、`agent-quality-system.md`、`frontend-experience.md`  
> 实施：`docs/plans/2026-07-11-codeflow-2.0-implementation-and-hardening-plan.md`

---

## 1. 产品重定位

CodeFlow 从"会话/计划管理控制台"升级为**流程中心（Flow-Centric）智能开发工作台**：

- 不模仿 VS Code 的文件中心布局，编码只是工作流中的一个阶段
- 原生工作流引擎是第一公民：想法 → 设计 → 规划 → 调研（可选）→ 编码 ⇄ Review/Debug → 提交
- 插件系统比 VS Code 更深：核心功能本身也是贡献点，可被插件替换
- 多端形态：桌面（Tauri 主端）+ 网页版（embed 分发）+ 手机版（远程伴侣端）

## 2. 应用级信息架构（App Shell）

```
App Shell（全局导航）
├── 首页 Dashboard      跨项目总览 / 待审批 Gate / 最近会话 / 用量
├── 项目管理            列表 / 创建（新项目 Flow）/ 导入（导入 Flow）/ 归档
├── 工作台              阶段自适应工作台（项目内工作层）
├── Agent 广场          Agent 资产市场与编排
├── 工作流模板          可视化流程模板编辑器
├── 插件管理            已装插件 + 市场
├── 配置中心            模型 / 渠道 / MCP / Skill 可视化编辑
└── 设置                外观 / 快捷键 / 隐私审计 / 实验性开关
```

路由：`/`、`/projects`、`/workbench/:projectId/:stage`、`/agents`、`/flows`、`/plugins`、`/config/:section`、`/settings`。

## 3. 目录重组目标结构

```
codeflow/
├── apps/
│   ├── workbench/          # 前端（原 apps/desktop 更名）
│   │   ├── src/
│   │   │   ├── shell/      # 全局页面层
│   │   │   ├── workbench/  # 工作台布局引擎
│   │   │   ├── stages/     # 七种阶段画布
│   │   │   ├── views/      # 跨阶段视图
│   │   │   ├── editor/     # Monaco 集成
│   │   │   ├── ui/         # 设计系统
│   │   │   ├── services/   # API 客户端（保留现有资产）
│   │   │   ├── stores/     # Zustand + TanStack Query
│   │   │   └── plugin-host/
│   │   └── src-tauri/
│   ├── mobile/             # 手机远程伴侣端
│   └── cli/                # 自 packages/cli 移入
├── packages/
│   ├── core/  ├── shared/  └── plugin-sdk/
├── backend/                # Go 后端
├── docs/{adr,design,plans}/
└── tools/                  # 原 scripts/
```

### 清理清单（前置任务，按序提交，单目录单提交）

| 序 | 动作 | 说明 |
|---|---|---|
| 1 | 移除 Git 中的 node_modules（根 + packages/core） | 仓库体积治理，加 .gitignore 与 CI 卫兵 |
| 2 | 移除 packages/core/dist、backend/outputs/runtime、生成物 | 构建时生成 |
| 3 | 删除 codeflow_template（与 apps/desktop 完全重复） | 先改 6 处路径绑定：backend/Makefile、tauri.conf.json、test-all-e2e.mjs、embed.go、build 脚本、CI |
| 4 | docs 收敛：archive/docs/early-design → docs/design/early；plan/ + backend/docs → docs/plans；decision 文档 → docs/adr | 单一事实源 |
| 5 | apps/desktop → apps/workbench，源码收进 src/ | 拆除 2636 行 App.tsx 的前置 |
| 6 | issues/ CSV 迁移至 GitLab Issues | 保留 CSV 为历史归档 |

## 4. 设计 vs 实现差距审计结论（摘要）

- ✅ 已实现：配置系统、指挥官、黑板、辩论（双方）、双轨记忆、Hook、热切换、审计、隐私、披露、适配器统一
- ⚠️ 部分实现：原子快照（capture 真实化但 restore 仍 opt-in 且对话/向量/图谱不可真实恢复）；summarize/summarizer 模块重复待合并
- ❌ 缺失（多为前端）：图谱可视化、AST 上下文构建器、Git 时间轴、记忆仪表盘拖拽、Cmd+K、披露 UI、嵌套子对话、AgentBoard
- 处置：缺失项不做孤立补全，分别并入对应阶段画布随里程碑交付；建立 docs/design/feature-parity-matrix.md 持续跟踪

## 5. 里程碑总表

| 里程碑 | 范围 | 关键交付 |
|---|---|---|
| M0 | 仓库清理 | 上表 6 项清理；CI 卫兵；Nx 链路收口 |
| M1 | 设计系统 + App Shell + 工作台骨架 | ui/ 令牌与 20 基础组件、Shell 页面骨架、Flow Rail、Dockview 面板系统、启动页/加载体系 |
| M2 | 工作流引擎 | internal/floweng、规划/提交画布、工作流模板可视化编辑器、阶段自动快照（依赖快照 restore 补全） |
| M3 | 编码画布 + 守卫 | Monaco/文件树/AST 构建器、internal/guard、internal/workspace 文件服务 |
| M4 | 想法/设计/Review 画布 + 辩论 + Agent 广场 | debate 多方多模型化、Agent Registry |
| M5 | DeepSearch + 导入理解 + 配置中心 | retriever 联网 Provider、理解报告画布、模型/渠道/MCP/Skill 四子区 |
| M6 | 插件系统 + Live Preview | 贡献点注册表、沙箱运行时、检查器桥与编辑闭环 |
| M7 | 多端 | 网页版响应式收口、apps/mobile 远程伴侣端、配对与远程审批 |

## 6. 架构优化专项（贯穿执行）

1. backend internal 35+ 模块按域收敛（agent/memory/workspace/workflow/platform/security），import 边界检查固化
2. 完成 DI 迁移：删除 22 个包级全局单例的 Get/Set 兼容层（按域分批）
3. Schema-first：OpenAPI YAML 为源生成 Go stub + TS 客户端；WS 消息定义 JSON Schema
4. WebSocket 统一事件总线：单连接多路分发，作为插件事件 API 基础
5. 快照一致性校验后台任务 + 回滚中断混沌测试
6. 评估 modernc.org/sqlite 替换 go-sqlite3，统一 CGO_ENABLED=0
7. ADR 惯例：重大变更先落 docs/adr 再动手
