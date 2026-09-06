# CodeFlow 3.0 新设计：AI 编码 Agent 的编排与治理层

> 状态：Draft（提议）  
> 创建：2026-09-03  
> 取代：[2026-06-12-overall-roadmap.md](../2026-06-12-overall-roadmap.md) 的产品定位部分；[flow-engine.md](../flow-engine.md) §2–§5；[agent-quality-system.md](../agent-quality-system.md) §1–§3；[workbench-and-shell.md](../workbench-and-shell.md) §2–§3（本文接受后上述文档标 Superseded 并链回此处）  
> 问题依据：同目录下的 [2026-09-03-project-issues-deep-dive.md](2026-09-03-project-issues-deep-dive.md)（引用格式 `I-xx`）  
> 实施：同目录下的 [2026-09-03-codeflow-3.0-implementation-plan.md](2026-09-03-codeflow-3.0-implementation-plan.md)

---

## 1. 重新定位

**CodeFlow 是 AI 编码 Agent 的编排层和治理层。**

它不再自己实现"AI 怎么读改跑"——这件事 Claude Code、Codex、Gemini CLI 已经做得很好，且在快速迭代。CodeFlow 做它们不做、也不适合做的事：

| CodeFlow 负责 | 外部执行后端负责 |
|---|---|
| 流程：任务从哪来、到哪去、什么算完成 | 在一个工作目录里完成一个明确任务 |
| 治理：谁能写什么、写之前查什么、写之后记什么 | 调用模型、调用工具、自我纠错 |
| 记忆：项目长期知识、决策、失败教训 | 单次会话上下文 |
| 调度：哪个任务给哪个 Agent、用哪个模型、预算多少 | 执行 |
| 审计与回放：发生了什么、为什么、能不能撤销 | 报告事件 |
| 密钥：模型永远看不到真值 | 使用占位符 |
| 多 Agent：并行、辩论、批评、终裁 | 单 Agent 循环 |

一句话给用户：**你把任务交给 CodeFlow，它选合适的 AI 去做、盯着它做、把过程记下来、做坏了能撤。**

### 1.1 现阶段的用户

**个人开发者**（I-23）。团队协作是后续里程碑，现阶段用"导出项目包 / 审计包"满足汇报需求。RBAC、多用户、远程配对全部后置。

### 1.2 不做什么

- 不自研工具循环（I-01/I-02）
- 不做主力代码编辑器（I-05）；编辑器定位为审阅与小改
- 不做手机端、插件市场（I-42）
- 不做多用户（I-23）
- 不做"三位一体回滚"（I-08）

---

## 2. 核心概念（领域模型）

```
Project                       项目：一个工作目录 + 长期记忆 + 守卫规则 + 密钥库
 ├─ Flow[]                    流程：一条有阶段的工作线；一个项目可并行多条（I-09）
 │   ├─ kind: task | project  任务流（默认，短）/ 项目流（可选，长）（I-06）
 │   ├─ worktree              可选的独立 git 工作副本
 │   ├─ Stage[]               阶段（由模板决定）
 │   │   ├─ Contract          完成契约：缺什么、谁能补、怎么重试（I-07）
 │   │   ├─ Gate              关卡：auto | check | human；on_fail: block | debate
 │   │   └─ Artifact[]        成果：文档 / 任务清单 / 变更集 / 报告；有版本与 stale
 │   ├─ Task[]                任务：可派给 Agent 后台执行（I-35）
 │   └─ Bookmark[]            书签：替代快照（I-08）
 ├─ Run[]                     运行：一次 Agent 执行；完全后端持有（I-31）
 │   ├─ backend               执行后端：codex | claude-code | gemini-cli | api | custom
 │   ├─ agent@version         Agent 资产版本
 │   ├─ shadow                影子副本（worktree 或临时目录）
 │   ├─ Event[]               统一执行事件流（I-24）
 │   ├─ Change[]              文件变更（合入前逐文件过守卫）
 │   ├─ Approval[]            审批请求
 │   └─ Usage                 token / 费用 / 时长
 ├─ Agent[]                   Agent 资产：提示词 + 执行后端 + 模型策略 + 挂载（I-18）
 ├─ Memory                    记忆：情景（向量）+ 语义（图谱），带来源/置信度（I-14）
 ├─ Guard                     守卫：五层规则；豁免走审批与审计（I-10）
 ├─ Vault                     密钥库：名字 → 值；占位符替换（I-21）
 └─ Audit                     审计链：所有状态变更
```

### 2.1 Run 是新的中心

以前的中心是 Flow；3.0 的中心是 **Run**。用户的每一次"让 AI 做点什么"都是一个 Run。Flow 只是给 Run 排顺序、定契约；守卫、审计、记忆、密钥、预算全部围绕 Run 工作。

一个 Run 的生命周期：

```
queued → preparing(影子副本/上下文/密钥占位) → running(事件流) 
  → [approval_required ⇄ running]* 
  → reviewing(diff 过守卫) → merged | rejected | cancelled | failed
```

任何时刻前端断开都不影响 Run；重连后按事件序号回放。

---

## 3. 系统架构

```
┌──────────────────────────────────────────────────────────────────┐
│ Workbench（React；同一构建：Tauri 桌面 / 后端 embed 网页）          │
│  首页 · 项目 · 流程 · 运行详情 · Agent · 记忆 · 守卫 · 密钥 · 设置   │
├──────────────────────────────────────────────────────────────────┤
│ 统一执行事件流（一个 WS 连接，按 project/run 主题订阅）            │
├──────────────────────────────────────────────────────────────────┤
│ Go 后端（sidecar，loopback + 进程令牌）                            │
│                                                                  │
│  编排器 Orchestrator ── 流程引擎 ── 任务队列 ── 调度器(Agent 分配)  │
│        │                                                         │
│  执行后端适配层 ExecBackend                                        │
│   ├─ Codex (app-server JSON-RPC)                                  │
│   ├─ Claude Code (SDK + hooks)                                    │
│   ├─ Gemini CLI (hooks + json 流)                                 │
│   ├─ Custom CLI (约定协议)                                         │
│   └─ API 直连（追问/文档/批评/辩论/记忆抽取）                        │
│        │                                                         │
│  治理层 Governance                                                │
│   ├─ 影子副本 Shadow (git worktree)                                │
│   ├─ 守卫 Guard（五层）                                            │
│   ├─ 密钥库 Vault（占位符替换 / 进程注入）                           │
│   ├─ 策略 Policy（风险分级 / 持续授权）                              │
│   ├─ 预算 Budget                                                  │
│   └─ 审计 Audit                                                   │
│        │                                                         │
│  知识层 Knowledge                                                 │
│   ├─ 记忆（情景 + 语义）                                            │
│   ├─ 代码索引（AST / 符号 / 结构指纹 / 向量）                        │
│   └─ MCP 服务器（把以上暴露给外部 CLI）                              │
│        │                                                         │
│  存储：单 SQLite（多表） + 向量库 + 文件（审计链 / 成果）（I-25）      │
└──────────────────────────────────────────────────────────────────┘
```

---

## 4. 执行后端（ExecBackend）

### 4.1 抽象

每个执行后端实现同一组能力：

| 能力 | 说明 |
|---|---|
| `start(run)` | 在影子副本目录启动，传入任务文件与环境变量 |
| `events()` | 产出统一执行事件 |
| `approve(id, decision)` | 回应审批请求 |
| `inject(text)` | 追加用户消息 |
| `cancel()` | 取消，可中断进程 |
| `capabilities` | 是否支持钩子、审批回调、JSON 事件、预算参数、MCP |

### 4.2 各后端接法

**Codex**：子进程，走 app-server 协议。审批请求（执行命令 / 打补丁）由协议推给 CodeFlow，这就是守卫锚点。不 fork、不链库（评审结论：三种深度选最浅）。Windows 下沙箱弱，靠影子副本兜底。

**Claude Code**：SDK 非交互模式 + `stream-json` 输出。钩子：PreToolUse（守卫/预算，可拒绝）、PostToolUse（审计）、UserPromptSubmit/SessionStart（记忆与阶段人设注入）、Stop（阶段契约检查，未达标不让停）。钩子配置由 CodeFlow 每次启动前生成并校验。

**Gemini CLI**：非交互模式 + 工具前后钩子；钩子粒度不足处靠影子副本 diff。

**Custom**：任何命令；约定环境变量传任务文件路径；至少产出结束码与 diff。

**API 直连**：现有 Claude/Gemini/OpenAI 适配器，只用于不动手的角色。

### 4.3 上下文注入

启动前在影子副本生成一份任务文件（`AGENTS.md` / `CLAUDE.md` / `GEMINI.md` 各家读的名字不同，内容相同）：当前流程与阶段、任务描述、阶段契约、相关成果摘要、检索到的记忆、守卫规则摘要、密钥占位符说明、禁止事项。

另开一条反向通道：CodeFlow 自己作为 MCP 服务器暴露 `search_memory`、`query_graph`、`get_artifact`、`get_guard_rules`、`check_write`，让执行后端主动来问（I-34）。

### 4.4 双锁

1. **钩子锁**：写文件/打补丁/执行命令前回调守卫与策略，可拒绝并返回原因。
2. **合入锁**：Run 结束后对影子副本整个 diff 逐文件过守卫、跑项目自带检查，全部通过才合入主工作副本（I-12）。

钩子挡不住 shell 写文件；合入锁挡得住。两道都要。

---

## 5. 流程模型

### 5.1 两级流程（I-06）

**任务流（默认）**：一条短流程，模板如：

| 模板 | 阶段 |
|---|---|
| 快速修改 | 执行 → 审阅 → 合入 |
| Bug 修复 | 复现 → 执行 → 验证 → 审阅 → 合入 |
| 新功能 | 拆解 → 执行 → 测试 → 审阅 → 合入 |
| 重构 | 影响分析 → 执行 → 验证 → 审阅 → 合入 |
| 文档 | 执行 → 审阅 → 合入 |

**项目流（可选）**：原七步（想法→设计→规划→调研→编码→审查→提交），其中"编码"阶段展开为多条任务流。

### 5.2 阶段契约（I-07）

每个阶段声明"完成需要什么"，推进时返回结构化结果：`advanced | blocked | waiting_gate`，附 `missing[]`、`stale[]`、`next`。任务流的契约轻（有变更、检查通过、守卫通过）；项目流的契约重（成果存在且未 stale）。

### 5.3 关卡与风险分级（I-13）

| 风险 | 例子 | 默认 |
|---|---|---|
| 低 | 读文件、搜索、改测试文件、改文档 | 放行，事后可查 |
| 中 | 改源码、跑测试、装依赖 | 放行 + 合入时审阅 |
| 高 | 删除文件、改配置/CI/密钥文件、跑未知命令、网络外发 | 拦，等审批 |
| 禁止 | 越出项目根、写入禁写路径 | 拒绝 |

用户可以对"这个 Agent 在这个项目做这类操作"给持续授权；可以批量审批；每条审批进审计。

### 5.4 回环

任务流内：审阅 → 执行（高频，不需审批）。项目流内：审查 → 编码/设计（下游成果标 stale，需确认）。回环不回滚，而是开新 Run。

---

## 6. 任务与调度

- 规划阶段的产物是 Task 列表；Task 可以：手动做、派给 Agent 后台跑、拆成子任务。
- 任务队列按项目限并发；每个 Run 有超时与预算。
- 通知中心汇总：等审批、失败、完成、预算告警。
- 多任务并行时每个 Run 一个影子副本；合入顺序由用户或队列决定，冲突交回给下一个 Run 处理。

---

## 7. 书签与分支（替代快照，I-08）

- **历史只追加**：对话、事件、记忆、成果永不删除。
- **书签**：一个书签 = (git 提交或 stash 引用, 对话位置, 成果版本集, 时间戳)。阶段完成自动打书签；用户可手动打。
- **回到书签** = 从书签开一条新流程（worktree/branch），旧路保留；两条路可以并排比较成果与 diff。
- 记忆不回滚：记忆项带时间戳与来源 Run，检索时可按书签时间过滤。
- 现有快照模块中"conversation/vector/graph 真恢复"的代码降级为"按书签过滤"，不再做破坏性恢复。

---

## 8. 守卫五层（I-10 / I-11）

| 层 | 内容 | 何时 | 成本 |
|---|---|---|---|
| 1 规则 | 现有：堆叠命名、禁写/废弃路径、大小、可执行、同名符号 | 每次写 | 零 |
| 2 结构 | 函数级 AST 归一化指纹（去标识符后哈希 + 编辑距离）找"改名的复制粘贴"；**API 注册表**（路径+方法）；**模型词典**（字段集合相似）——后两者来自影子系统 | 每次写 | 毫秒 |
| 3 项目检查 | 项目自带 lint / typecheck / 受影响测试 | 合入前批量 | 秒 |
| 4 向量 | 函数级嵌入相似度，只对 1–2 层判"可疑"的再算 | 合入前 | 便宜 |
| 5 AI 复审 | 只在前面不确定或用户要求时 | 罕见 | 贵 |

结果统一为 `Decision{allowed, violations[], suggestions[]}`；违规带定位与"改哪里"建议，回给执行后端让模型自己改。

豁免流程保留：申请 → 审批 → 审计 → 合入报告汇总。

---

## 9. Agent 资产与分配（I-17 / I-18 / I-20）

### 9.1 资产

```
Agent
 ├─ id / name / version / source
 ├─ purpose: executor | advisor | critic | researcher | conductor
 ├─ backend: codex | claude-code | gemini-cli | api | custom
 ├─ model_policy: { default, cheap_for_simple, escalate_on_failure }
 ├─ prompt (系统提示)
 ├─ mounts: { skills[], mcp[] }
 ├─ fit: { stages[], task_types[], languages[] }
 └─ stats: 按 (project, task_type) 维度的评分与用量
```

Agent 资产是唯一配置单位。"主脑/编码员/子任务"退化为内置 Agent 的分类；三级配置继承退化为"项目可覆盖 Agent 的 model_policy"。

### 9.2 分配

三维匹配：阶段 × 任务类型 × 项目上下文（语言、框架、规模）。

调度结构：**主脑 + 专家**。主脑（便宜模型）理解任务、拆分、决定叫谁；专家按任务类型召唤；批评者永远不与生成者同模型。

推荐带依据（"这个项目是 Go，该 Agent 在本项目 bug 修复评分 4.6，最近 7 次成功 6 次"）；每次 Run 结束用户一键评分；项目级可固定"审查一律用 X"。

### 9.3 辩论（I-19）

服务端执行：每方绑定 Agent 资产版本，通过 ExecBackend（多为 API 直连）逐轮产生贡献；共识策略仲裁/投票/用户终裁；结论成为 Artifact 并沉淀记忆。关卡 `on_fail = debate` 用同一机制。

---

## 10. 密钥库（Vault，I-21 / I-22）

**原则：模型永远看不到真值；程序运行时拿到真值。**

- **登记**：手动标记；文件规则（`.env`、`secrets.*`、`*.pem`、CI token 字段）；高熵/已知格式自动发现后**由用户确认**；系统钥匙串同步。
- **出站替换**：所有进入模型上下文的内容（文件、命令输出、用户消息、工具输出流）经替换器，把已登记值换成稳定占位符 `«SECRET:NAME»`。同一会话内映射稳定。
- **入站还原**：模型输出在写盘/执行那一刻还原；对话、审计、记忆只存占位符。
- **进程注入**：子进程（测试、dev server、CLI）以环境变量拿真值，Jenkins `withCredentials` 模式。
- **日志遮盖**：所有日志/审计/崩溃转储扫描已登记值并遮盖。
- **主密钥**：桌面壳生成并存入系统钥匙串；提供恢复口令与导出；首启向导接管（I-22）。
- **个人信息**与密钥分开：PII 走脱敏（有损），密钥走占位符（可逆）。

边界要告诉用户：git 历史、二进制、第三方服务返回的未登记密钥不在保护范围。

---

## 11. 记忆（I-14 / I-15 / I-16）

- 每条记忆：内容、类型（事实/决策/教训/偏好）、来源 Run、置信度、时间、项目。
- 写入：Run 结束后由"记忆抽取"Agent（API 直连，便宜模型）抽取；低置信度进待确认区。
- 读取：Run 启动前检索注入任务文件；执行中通过 MCP 被动查询；用户提交前预检提示相关历史。
- 管理：记忆浏览器（查看/搜索/删/改/导出）；周期性一致性清理；按项目隔离。

---

## 12. 前端信息架构

```
/                    首页：今日待办（审批/失败/完成）、运行中、最近项目、用量
/projects            项目：列表 / 新建 / 导入 / 归档
/p/:id               项目总览：流程列表、运行中、记忆概况、守卫健康
/p/:id/flows/:fid    流程：阶段进度 + 当前阶段画布 + 任务列表
/p/:id/runs/:rid     运行详情：事件时间线、文件变更、工具调用、审批、费用、撤销
/p/:id/memory        记忆浏览器
/p/:id/guard         守卫：拦截记录、豁免、规则
/p/:id/vault         密钥库
/agents              Agent 资产：列表、编辑、评分
/settings            执行后端、模型渠道、外观、快捷键
```

工作台布局：左侧项目/流程导轨；中央按流程阶段变形的画布；右侧 Agent 伴侣（发起 Run、追问）；底部终端 / 运行日志 / 守卫 / 问题；顶部状态。

关键新页面：**运行详情**（I-36）与**首页待办**（I-13 的批量审批入口）。

视觉：白色为底，中性灰做层级，单一签名色只用于"运行中/流动"语义；见 [mockups/](../mockups/)。

---

## 13. 与现有模块的映射

| 现有 | 3.0 去向 |
|---|---|
| floweng | 流程引擎；加两级模板与契约 |
| workspace | 保留 list/read/write/staging；写入口改为 Run 合入 |
| guard | 五层扩展；吸收 shadow 的 API 注册表与模型词典 |
| shadow | 拆用（见上）；目录概念 → 影子副本 |
| snapshot | 降级为书签；删除破坏性恢复 |
| agent (runtime) | 重写为 Run 服务 |
| agent (registry) | 资产模型扩展 backend / model_policy / fit |
| debate | 改服务端执行 |
| memory / samg | 加来源、置信度、待确认区；MCP 暴露 |
| privacy | 拆为 Vault + PII |
| hooks | 内部事件机制；触发点接 ExecBackend 事件 |
| adapters | 只用于 API 直连角色 |
| audit / config / project / skill | 保留 |
| blackboard / votes / isolation(RBAC) / mapagent / integrations / disclosure | 冻结（见计划 §2） |
| packages/* | 遗留，不再维护 |

---

## 14. 待决 ADR

1. 执行后端首选 Codex app-server 还是 Claude Code SDK 先接（建议 Claude Code 先，钩子最全；Codex 第二）。
2. 单 SQLite 迁移策略（一次性 vs 逐域）。
3. CGO 处理：安装编译器 vs 迁 modernc（建议 modernc，纯 Go，消除环境依赖）。
4. 书签的 git 表示：commit vs stash vs ref。
5. 影子副本默认策略：worktree（需 git）vs 临时目录复制（无 git 也行）。

---

## 15. 跨模块实现契约（I-55 至 I-70）

本节是 3.0 的统一实现约束。各模块可以独立实现，但不能自行定义另一套身份、状态、事件或审批语义。字段名以 JSON/HTTP 契约为准，数据库列名可以使用 snake_case。

### 15.1 身份传播链

所有可执行操作都必须能沿下列链路回溯，ID 创建后不可复用：

```text
Project
  └─ WorkspaceBinding (绑定版本、canonical root、base commit)
      └─ Flow (project flow / task flow)
          └─ Stage
              └─ Task
                  └─ Run
                      └─ Attempt (外部后端一次进程/API尝试)
                          ├─ Event (单调 sequence)
                          ├─ Approval
                          ├─ ArtifactVersion
                          └─ AuditEntry
```

`ExecutionIdentity` 是所有边界函数的必填上下文：

```json
{
  "project_id": "p_123",
  "workspace_binding_id": "wb_4",
  "workspace_revision": 7,
  "flow_id": "f_9",
  "stage_id": "st_3",
  "task_id": "task_21",
  "run_id": "run_88",
  "attempt_id": "att_1",
  "agent_asset_id": "agent_claude_bugfix",
  "agent_asset_version": 12,
  "actor_type": "user|agent|system",
  "actor_id": "agent_claude_bugfix"
}
```

HTTP 请求、WS envelope、hook payload、policy request、子进程环境和 audit details 都从同一上下文派生。缺少 project/workspace/run 的写操作默认拒绝；只读接口可以省略下游 ID，但响应仍返回已知父级身份。

### 15.2 规范数据模型与生命周期

| 实体 | 必填字段 | 终态 | 关键不变量 |
|---|---|---|---|
| Project | id, title, status, revision | archived/completed | archived 不再创建 Run；删除采用标记和异步清理 |
| WorkspaceBinding | id, project_id, canonical_root, repo_id, base_ref, revision | archived | 同一 project 同时只有一个 active binding；路径不能跨 root |
| Flow | id, project_id, kind, template_version, status, revision | completed/aborted | 一个 active stage；任务流可并行，项目流最多一个 active 主流 |
| Stage | id, flow_id, type, status, order, contract_version | done/skipped/failed | 入口/出口 Gate 只改变状态，不直接写成果 |
| Task | id, flow_id, stage_id, status, priority, contract | completed/failed/cancelled | 每次派发产生一个或多个 Run，不复用已结束 Run |
| Run | id, task_id, identity, status, base_revision, budget | completed/failed/cancelled/expired | 终态不可回写；重试创建新 Attempt |
| Approval | id, run_id, subject, risk, fingerprint, decision, expires_at | approved/rejected/expired | 决策幂等；scope 不能超过请求声明的资源集合 |
| Event | id, scope, sequence, type, occurred_at, payload | immutable | sequence 在 scope 内单调；payload 经过 Vault 净化 |
| ArtifactVersion | id, artifact_id, run_id, content_hash, status | approved/superseded | 内容不可变；新内容只能创建新版本 |
| Bookmark | id, project_id, flow_id, git_ref, event_cursor, memory_cutoff | immutable | 只追加，不修改旧书签指向 |

状态迁移必须由服务端命令完成，并记录 `from_status/to_status/reason/actor/expected_revision`。未知迁移、终态迁移和跨项目引用均返回结构化错误。

### 15.3 状态机、重试和取消

Run 的规范状态为：

```text
queued -> starting -> running -> (waiting_approval | paused | cancelling)
waiting_approval -> running | cancelled | expired
paused -> running | cancelled
cancelling -> cancelled | failed
running -> completed | failed | cancelling | expired
```

- `failed` 表示后端或检查失败，`retryable` 是事件字段而不是状态名。
- `cancel_requested` 先写状态和事件，再向外部进程发送温和终止；超过 grace period 才强制回收。
- 重试必须引用 `retry_of_run_id` 和失败原因，复制任务输入快照，不复制审批决定。
- 后端重启时，启动恢复器扫描 `starting/running/cancelling`：有活进程则重新 attach，无进程则转为 `failed`（reason=`orphaned_after_restart`）。
- 每个 Attempt 的 stdout/stderr、退出码、signal、token 和费用单独计量；Run 汇总只读聚合值。

### 15.4 统一事件 envelope、游标和回放

所有 HTTP 长轮询、WS、外部 CLI 适配器和审计桥接都转换为同一 envelope：

```json
{
  "id": "evt_01J...",
  "scope": "run:run_88",
  "project_id": "p_123",
  "run_id": "run_88",
  "sequence": 42,
  "type": "tool_call",
  "schema_version": 1,
  "occurred_at": "2026-09-06T08:00:00Z",
  "identity": {"agent_asset_version": 12, "attempt_id": "att_1"},
  "payload": {"tool": "read_file", "path": "src/foo.go"},
  "redaction": {"secrets_replaced": true}
}
```

事件类型最小集合：`run.queued`、`run.started`、`run.delta`、`tool.requested`、`tool.completed`、`file.changed`、`approval.required`、`approval.decided`、`guard.blocked`、`check.completed`、`run.paused`、`run.completed`、`run.failed`、`run.cancelled`、`merge.completed`。

- `sequence` 仅在 scope 内有序，`occurred_at` 只用于展示；客户端不得用时间排序事实。
- `GET /events?after=42&limit=200` 返回 `events`, `next_after`, `has_more`, `retention_floor`；游标早于保留下界时返回 `event_gap`，要求完整快照重建。
- WS 首帧为 `subscribed`，随后发送 `replay_started/replay_finished`；实时消息与回放共享同一序列，客户端以 `(scope, sequence)` 去重。
- outbox dispatcher 失败可重试，事件本身不因广播失败而删除；队列满时只能施加背压或断开并要求回放，不能静默丢弃。

### 15.5 HTTP API、幂等和并发

统一资源前缀为 `/api/v1/projects/:pid/...`，旧的无项目 scope 路由只保留兼容期并标 deprecated。核心接口：

| 方法 | 路径 | 语义 |
|---|---|---|
| POST | `/projects/:pid/runs` | 创建 Run；支持 `Idempotency-Key` |
| GET | `/runs/:rid` | 读取 Run、当前 revision 和 usage |
| GET | `/runs/:rid/events?after=` | 按游标回放 |
| POST | `/runs/:rid/approve` | 决策一个 Approval；需要 fingerprint |
| POST | `/runs/:rid/cancel` | 请求取消；重复调用返回同一结果 |
| POST | `/runs/:rid/merge` | 以 base revision 获取合入锁并执行三方检查 |
| POST | `/runs/:rid/discard` | 丢弃影子副本并保留审计 |
| GET | `/projects/:pid/bookmarks` | 列出不可变书签 |
| POST | `/projects/:pid/bookmarks/:bid/fork` | 从书签创建新 Flow/Binding |

所有可变请求：

1. `Idempotency-Key` 必须与请求体 hash 绑定；同 key 不同 body 返回 `idempotency_key_reused`。
2. `If-Match` 或 `expected_revision` 不匹配返回 HTTP 409，携带 `current_revision` 和 `conflicting_fields`。
3. 成功响应统一为 `{data, meta:{request_id, revision}}`；失败为 `{error:{code,message,retryable,details}, meta:{request_id}}`。
4. 错误 code 是稳定枚举，UI 不解析自然语言；可重试性由服务端声明。

### 15.6 阶段契约、成果和审批的边界

阶段 Contract 只回答“能否进入下一阶段”，不承担文件写入。Contract 由以下可验证断言组成：

```yaml
contract_version: 1
inputs:
  - artifact_type: plan
    status: approved
checks:
  - id: changed_files_present
    evaluator: workspace.diff
  - id: affected_tests_pass
    evaluator: project.check
  - id: no_guard_errors
    evaluator: guard.report
approvals:
  - subject: merge
    risk_at_least: high
on_fail: block
```

Gate 是 Contract 的聚合视图；高风险工具操作和合入动作都生成同一种 Approval。Gate 通过不能覆盖未决的 tool/merge Approval；审批通过也不能绕过 Contract 检查。所有 Gate/Approval 决策引用 policy version、actor、reason 和 request fingerprint。

### 15.7 调度、预算和外部进程

- 队列键为 `(project_id, backend, resource_class)`；项目并发、全局并发和每 Agent 并发分别限额。
- Run 预算包括 wall time、token、费用、输出字节、磁盘增量和网络请求；超过 soft limit 产生告警，超过 hard limit 自动 pause/cancel。
- Attempt 使用独立工作目录、环境快照和 process group/job object；所有子进程继承 `CODEFLOW_RUN_ID`、`CODEFLOW_ATTEMPT_ID`，不得仅用 PID 关联。
- stdout/stderr 采用带上限的环形日志；完整输出写 artifact，事件只发送摘要和引用。消费慢时暂停读取或断开并以游标恢复。
- 退出原因必须区分 `exit_code`、`signal`、`timeout`、`policy_denied`、`orphaned`、`backend_unavailable`。

### 15.8 工作区、影子副本与合入

Run 创建冻结 `WorkspaceBinding.revision` 和 `base_commit`。所有写入只发生在 shadow worktree/临时复制目录。合入流程是：

1. 获取 project merge lock，检查 binding、当前 HEAD 和用户未提交变更。
2. 计算 base/current/result 三方 diff；检测 deleted/renamed/binary/generated 文件。
3. 在临时索引应用完整 diff，逐文件执行 Guard 和项目检查。
4. 无冲突时一次性更新目标树或创建 commit；写入 `merge.completed`。
5. 冲突时不写目标树，返回 hunk、冲突原因和可重试 action；用户解决后创建新的 MergeAttempt。

`PromoteAll` 旧接口在迁移期只能作为单文件兼容 API，不能被 Run 合入路径调用。

### 15.9 Vault、策略和审计边界

模型上下文、MCP 响应、事件 delta、工具输出和任务文件都必须经过 `VaultGateway.Outbound`；写盘、命令执行和外部 API 请求经过 `VaultGateway.Inbound`/`InjectProcessEnv`。审计只保存占位符、hash、长度和 secret name，不保存真值。

策略 `Decision` 增加 `run_id`, `attempt_id`, `risk`, `approval_id`, `fingerprint`, `scope`；任何边界调用没有完整 identity 或没有明确 policy version 都 fail closed。PII 脱敏仍是有损变换，不能替代 Vault 的可逆占位符。

### 15.10 前端缓存、断线和离线模式

- 服务端事实：Project/Flow/Run/Approval/Bookmark 查询以 revision 为准；事件时间线以 sequence 为准。
- 客户端命令状态：`draft -> submitting -> accepted -> applied | rejected | unknown`；断网或超时进入 `unknown`，恢复后以 request ID 查询，禁止盲目重发不同 key。
- WS 断线：指数退避后重新订阅，带每个 scope 的 `after` 游标；收到 `event_gap` 先拉快照再继续事件。
- 离线模式只读缓存和本地草稿；禁止在未确认后端状态时显示“已合入/已审批”。
- 查询重试按错误 code 区分，认证/策略/校验错误不重试，网络和 5xx 使用有限退避。

## 16. 统一验收矩阵

| 场景 | 必须验证的事实 | 失败判定 |
|---|---|---|
| 创建 Run | identity、base revision、幂等 key 均持久化 | 重试产生第二个 Run 或缺父级 ID |
| WS 断线 | after 游标可回放且无重复/缺口 | 时间线少事件或 sequence 倒退 |
| 外部 CLI 崩溃 | Attempt 失败原因明确，孤儿进程被回收 | Run 永久 running 或残留进程写盘 |
| 高风险工具 | Approval 与 request fingerprint 绑定 | 改参数后复用旧批准 |
| 并行 Run 合入 | merge lock 和三方 diff 阻止覆盖 | 半套文件写入或覆盖用户改动 |
| 守卫失败 | 合入前逐文件检查且不改变目标树 | PromoteAll 部分成功 |
| 密钥扫描 | 事件、日志、审计、记忆无真值 | 任一持久化输出出现 canary |
| 重启恢复 | Run、事件、审批、预算均可恢复 | 状态回到内存初始值 |
| 前端离线 | unknown 命令可查询最终结果 | UI 误报成功或重复提交 |

## 17. 设计决策落地顺序

1. 先固定实体、状态、事件和错误 schema，再写 API 和表迁移。
2. 再实现 Run/Attempt/WorkspaceBinding 的身份传播与 outbox，接入第一执行后端。
3. 然后接入双锁 Guard、统一 Approval 和 Vault gateway。
4. 最后开放多后端、书签、外部连接和深层语义守卫；每个扩展必须复用本节契约。
