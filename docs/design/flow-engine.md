# 工作流引擎（Flow Engine）详细设计

> 状态：Active / 目标态设计（**PR-8 最小实现已落地**：内存引擎 + experimental API）  
> 模块：`backend/internal/floweng`（与现有 `internal/workflow` 观测层同域合并——观测合并仍待）  
> 里程碑：M2  
> 纳入跟踪：2026-07-13（M0.6）；实现起步 2026-07-15（PR-8）

---

## 1. 设计原则

- 阶段（Stage）是可复用积木，模板（Template）只是积木编排；新项目流与导入流共享同一套阶段实现
- 产物（Artifact）是一等公民，是阶段衔接与记忆沉淀的载体
- 每个阶段完成点自动打原子快照，回跳基于快照
- 模板、阶段类型、Gate 校验器均为插件贡献点

## 2. 数据模型

```
Flow
├── id / project_id / template_id / status / created_at
├── stages: Stage[]
│   ├── id / type / status: pending|active|waiting_gate|done|skipped
│   ├── canvas: 画布类型标识
│   ├── agent_slots: AgentBinding[]   # 单Agent/协作组/辩论组，绑定模型与渠道
│   ├── artifacts: ArtifactRef[]
│   ├── gates: Gate[]                 # enter/exit 两类
│   └── snapshot_id                   # 完成点快照
├── loops: Edge[]                     # 允许的回环（如 review→coding、review→design）
└── events → internal/workflow timeline（观测层消费）

Artifact
├── id / stage_id / type / version / created_by(agent|user)
├── content_ref（存储指针）
├── status: draft|approved|stale      # 上游回跳后下游标 stale
└── memory_links: SAMG 三元组引用

Gate
├── type: human_approval | agent_check | auto
├── config（如测试通过率阈值、审批人）
└── on_fail: block | escalate_to_debate
```

## 3. 内置模板

### 3.1 新项目流

| 阶段 | 画布 | 产物 | 后端支撑 |
|---|---|---|---|
| 想法提出 | 意图画布（自由文本 + Agent 苏格拉底追问） | idea.md（问题/目标/约束/成功标准） | floweng/intake，Main 角色 |
| 设计 | 文档 + 架构图（Mermaid）双栏，可触发辩论 | design.md + ADR | internal/debate、blackboard |
| 规划 | 任务看板 + 依赖图，Agent 分解任务 | Plan/PlanTask（复用现有模型） | internal/planner、project |
| 调研（可选） | DeepSearch 工作面（检索计划/结果流/证据篮） | research-report.md，结论沉淀 SAMG | retriever、search + 联网 Provider |
| 编码 | 编辑器网格 + 文件树 + AST 构建器 | 按任务分组的变更集 | git、ast、snapshot、guard |
| Review/Debug | Diff 审查 + Critic 评审流 + 辩论面板 | review-report.md | debate、audit |
| 提交 | 变更集 → commit 分组 → 三位一体快照绑定 | commits + 快照 + 流程归档报告 | git、snapshot |

### 3.2 导入项目流

前两阶段差异化，其余共享：

1. **导入**：目录/Git URL → 流水线：克隆 → AST 全量解析 → 向量化索引 → SAMG 初始图谱 → 技术栈探测；画布显示索引进度
2. **理解（Comprehension）**：Agent 生成项目理解报告（架构图/模块职责/关键流程/技术债/风险），用户可纠正且纠正写回 SAMG；产物 comprehension.md 为后续基底上下文
3. 规划阶段附加"影响分析"：经 SAMG 推导改动波及模块并高亮

### 3.3 其他模板（同引擎免费获得）

Bug 修复流、重构流、文档流；插件可注册自定义模板与阶段类型。

## 4. 回环与一致性

- Review → 编码：默认高频循环，不需审批
- Review → 设计：产物升版本，下游产物标 stale，需用户确认
- 回跳基于阶段快照，提供回跳前后差异对比视图
- 阶段切换持锁：Git 事务/写入未结束禁止回跳（早期设计 3.3.3 执行锁落地）

## 5. 与快照系统的硬依赖（前置任务）

M2 前必须补全 internal/snapshot：

- conversation/vector/graph 三个 StateProvider 的真实 restore（当前仅稳定摘要）
- 破坏性 git restore 从环境变量 opt-in 升级为引擎受控调用（带执行锁与语义预检）
- E2E：创建快照 → 修改 → 回滚 → 一致性校验

## 6. API 草案

```
POST   /api/v1/flows                      创建（template_id + project_id）
GET    /api/v1/flows/:id                  详情（含 stages/artifacts/gates）
POST   /api/v1/flows/:id/stages/:sid/advance   完成并推进（触发 exit gate）
POST   /api/v1/flows/:id/stages/:sid/skip      跳过可选阶段
POST   /api/v1/flows/:id/loop             回跳 {from,to,reason}
GET    /api/v1/flows/:id/artifacts        产物列表/版本
POST   /api/v1/gates/:id/approve|reject   人工审批
WS     flow.* 事件（阶段变更/产物产生/Gate 等待）→ 统一事件总线
```

## 7. 工作流模板可视化编辑器（前端）

- 节点画布：阶段节点（含 Agent 插槽、Gate 配置、画布类型选择）+ 顺序边 + 回环边
- Agent 插槽点击跳转 Agent 广场选择；支持辩论组（2~N Agent 各绑模型）
- 模板导出/导入 JSON，可经插件市场分发
- 校验：无孤立节点、回环边必须指向上游、可选阶段需显式标记
