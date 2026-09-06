# CodeFlow 3.0 新设计：AI 编码 Agent 的编排与治理层

> 状态：Draft（提议）  
> 创建：2026-09-03  
> 取代：[2026-06-12-overall-roadmap.md](../2026-06-12-overall-roadmap.md) 的产品定位部分；[flow-engine.md](../flow-engine.md) §2–§5；[agent-quality-system.md](../agent-quality-system.md) §1–§3；[workbench-and-shell.md](../workbench-and-shell.md) §2–§3（本文接受后上述文档标 Superseded 并链回此处）  
> 问题依据：同目录下的 [2026-09-03-project-issues-deep-dive.md](2026-09-03-project-issues-deep-dive.md)（引用格式 `I-xx`）  
> 实施：同目录下的 [2026-09-03-codeflow-3.0-implementation-plan.md](2026-09-03-codeflow-3.0-implementation-plan.md)
> 复审同步：2026-09-06，对齐实施计划 v3.0-dispatch-3；72 张任务卡、216 个派发步骤见计划 §28–§30。本文描述目标设计，不表示代码已实现。

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
| 密钥：隔离项目凭据、净化模型输入、按授权供可信运行器使用 | Agent CLI 不持有项目密钥 |
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

执行与合入分别保存事实：

```text
Run: queued -> starting -> running -> completed | failed
                         -> waiting_approval -> running
                         -> cancelling -> cancelled | expired
MergeOperation: prepared -> applying -> applied
                            -> rolling_back -> rolled_back | needs_recovery
Task: ready -> queued -> running -> waiting_review -> completed
```

Run.completed 只表示执行成功且输出已保存；代码 Task 在合入 applied 后才 completed，无代码任务按明确成果契约验收。合入前检测冲突返回 MergeOperation.conflict；进程状态不确定转 recovering，paused 仅在后端证明支持 checkpoint 时使用，完整状态见 §15.3。任何时刻前端断开都不影响 Run；重连后按事件序号回放。

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
| `events()` | 产出 BackendObservation；由服务端分配身份、事件 ID 和序号 |
| `approve(id, decision)` | 回应审批请求 |
| `inject(text)` | 追加用户消息 |
| `cancel()` | 取消，可中断进程 |
| `capabilities` | 是否支持钩子、审批回调、JSON 事件、预算参数、MCP |

### 4.2 各后端接法

**Codex**：受监督子进程，走已验证版本的 app-server 协议。协议支持的审批请求交 CodeFlow 判定；具体请求方法、framing、取消和恢复以能力探测及 transcript 为准。Windows 上 worktree 不代替沙箱，受保护执行需验证文件/网络隔离，否则返回 capability_unavailable。

**Claude Code**：选官方 SDK 或 CLI 非交互入口之一，按实际版本验证 JSON 输出与钩子，不能混用两套参数。PreToolUse 负责策略/守卫，PostToolUse 记录审计，启动前冻结记忆/规则。退出后由 CodeFlow 单独检查成果契约；用户取消必须能终止进程，不能用 Stop 钩子强迫无限修复。钩子配置每次生成并校验。

**Gemini CLI**：验证当前版本的非交互 JSON 与钩子能力。缺少实时审批的能力必须明确标为不支持，拒绝需要该能力的任务；diff 复核不能替代执行期文件/网络隔离。

**Custom**：受策略允许的本地执行器，采用版本化、有界 JSON Lines 协议，必须支持生命周期/能力声明和公共错误。命令配置不能自行获得主树、凭据或审计写权限。

**API 直连**：现有 Claude/Gemini/OpenAI 适配器，只用于不动手的角色。

### 4.3 上下文注入

启动前在影子副本生成一份任务文件（`AGENTS.md` / `CLAUDE.md` / `GEMINI.md` 各家读的名字不同，内容相同）：当前流程与阶段、任务描述、阶段契约、相关成果摘要、检索到的记忆、守卫规则摘要、密钥占位符说明、禁止事项。

另开一条反向通道：CodeFlow 自己作为 MCP 服务器暴露 `search_memory`、`query_graph`、`get_artifact`、`get_guard_rules`、`check_write`，让执行后端主动来问（I-34）。

### 4.4 双锁

1. **钩子锁**：写文件/打补丁/执行命令前回调守卫与策略，可拒绝并返回原因。
2. **合入锁**：Run 结束后对影子副本整个 diff 逐文件过守卫、跑项目自带检查，全部通过才合入主工作副本（I-12）。

任意 shell 的全部副作用无法仅从命令文本判定；未知效果必须受策略和真实隔离约束。合入锁保护最后的候选发布，不能阻止进程提前读取宿主秘密或发出网络请求。两道检查与执行隔离分别验证。

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

每个阶段声明"完成需要什么"，推进时返回结构化结果：`advanced | blocked | waiting_gate`，附 `missing[]`、`stale[]`、`next`。代码任务验证变更、检查与守卫；文档/手动任务可验证明确的无代码成果，不能统一强求非空 diff。项目流聚合固定的必需子流程及成果版本，输入变化使相关验收 stale。

### 5.3 关卡与风险分级（I-13）

| 风险 | 例子 | 默认 |
|---|---|---|
| 低 | 允许范围内的读取、搜索 | 满足策略即可执行，保留审计 |
| 中 | 允许范围内的源码/测试/文档候选修改 | 校验具体路径和内容，合入时审阅 |
| 需评估 | 跑测试、安装依赖、构建 | 按可执行脚本和实际副作用判断；不能仅凭命令名自动放行 |
| 高 | 删除文件、改配置/CI/密钥文件、跑未知命令、网络外发 | 拦，等审批 |
| 禁止 | 越出项目根、写入禁写路径 | 拒绝 |

持续授权限制 principal/project/agent revision/操作/路径/期限/次数，执行时再次验证 fingerprint 和策略。批量审批逐项独立事务，返回部分失败；决定、消费、过期及撤销都可审计。

### 5.4 回环

任务流内审阅可回到执行并创建新 Run，旧 Approval 不继承，具体高风险操作仍需有效授权。项目流回环会使依赖旧成果的下游验收 stale；回环不删除历史，也不改写已终止 Run。

---

## 6. 任务与调度

- 规划阶段的产物是 Task 列表；Task 可以：手动做、派给 Agent 后台跑、拆成子任务。
- 任务队列按项目限并发；每个 Run 有超时与预算。
- 通知中心汇总：等审批、失败、完成、预算告警。
- 多任务并行时每个 Run 一个影子副本；合入顺序由用户或队列决定，冲突交回给下一个 Run 处理。

---

## 7. 书签与分支（替代快照，I-08）

- **版本只追加**：修订生成新版本，保留可追溯来源；日志/派生索引按 retention 清理，隐私撤回传播到派生内容，不承诺所有内容永久留存。
- **书签**：固定 binding revision、manifest、对话/事件游标、成果版本集、记忆可见性和分支血缘；Git 用 CodeFlow 自有 ref，非 Git 用 blob 集。阶段完成可自动创建，引用内容需 pin。
- **回到书签** = 从书签开一条新流程（worktree/branch），旧路保留；两条路可以并排比较成果与 diff。
- 记忆不回滚：按来源版本、分支可见集和 watermark 检索，兄弟分支默认不可见；只有时间过滤不能保证分叉隔离。
- 新书签不调用破坏性恢复；旧 snapshot 入口先保留兼容/不可用说明，完成迁移和调用扫描后再退役。

---

## 8. 守卫五层（I-10 / I-11）

| 层 | 内容 | 何时 | 成本 |
|---|---|---|---|
| 1 规则 | 现有：堆叠命名、禁写/废弃路径、大小、可执行、同名符号 | 每次写 | 零 |
| 2 结构 | 真实 AST 的局部绑定归一化指纹，保留外部调用/字面量；API 注册表与模型词典复用旧 shadow 能力，统一从版本化代码索引重建 | 写前/合入 | 实测并限额 |
| 3 项目检查 | 项目自带 lint / typecheck / 受影响测试 | 合入前批量 | 秒 |
| 4 向量 | T9.05：仅对结构候选调用已配置 embedding，固定模型/维度/来源版本；TF-IDF 只标词频相似，不能当真实语义能力 | 合入前 | 限额、实测 |
| 5 AI 复审 | 只在前面不确定或用户要求时 | 罕见 | 贵 |

结果统一为 `Decision{allowed, violations[], suggestions[], rule_version, fingerprint}`；相似度是证据，规则决定 warning 或 blocking。unsupported/解析失败返回 unknown，required 检查不能因此默许通过；违规带准确版本定位和建议。

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

Agent 资产是唯一配置单位。保留旧 Version 标签，新增不可变 `agent_revision_id`；Run 固定最终解析配置。"主脑/编码员/子任务"配置保留显式兼容，项目只可覆盖 model_policy；历史修订不随用户修改或统计累计而变化。

### 9.2 分配

三维匹配：阶段 × 任务类型 × 项目上下文（语言、框架、规模）。

调度结构：**主脑 + 专家**。主脑（便宜模型）理解任务、拆分、决定叫谁；专家按任务类型召唤；批评者永远不与生成者同模型。

推荐带依据（"这个项目是 Go，该 Agent 在本项目 bug 修复评分 4.6，最近 7 次成功 6 次"）；每次 Run 结束用户一键评分；项目级可固定"审查一律用 X"。

### 9.3 辩论（I-19）

服务端执行：每方绑定 Agent 资产版本，通过 ExecBackend（多为 API 直连）逐轮产生贡献；共识策略仲裁/投票/用户终裁；结论成为 Artifact 并沉淀记忆。关卡 `on_fail = debate` 用同一机制。

---

## 10. 密钥库（Vault，I-21 / I-22）

**目标：Agent 模型输入不包含项目密钥；只有明确授权的可信运行器可以使用对应凭据。** 必须验证模型实际输入和执行器的宿主读取边界，不能以 UI 日志遮盖作为证明。

- **登记**：手动标记；文件规则（`.env`、`secrets.*`、`*.pem`、CI token 字段）；高熵/已知格式自动发现后**由用户确认**；系统钥匙串同步。
- **出站替换**：所有进入模型上下文的内容（文件、命令输出、用户消息、工具输出流）经替换器，把已登记值换成稳定占位符 `«SECRET:NAME»`。同一会话内映射稳定。
- **引用解析**：模型生成占位符不授予解密能力；只有 CredentialBroker 按 secret version/subject/scope/expiry 授权解析，不能在任意写盘时自动还原。
- **进程注入**：Agent CLI 不注入项目 secret。授权测试/devserver 由独立可信运行器获得最小环境，脚本副作用仍受文件/网络隔离；backend 登录凭据另由受信任适配器管理。
- **日志遮盖**：所有日志/审计/崩溃转储扫描已登记值并遮盖。
- **主密钥**：桌面壳生成并受系统钥匙串保护，webview 只获取状态/短期操作句柄；setup/locked/rotation/recovery 有可恢复回执，恢复导出不包含明文主密钥。
- **个人信息**与密钥分开：PII 走脱敏（有损），密钥走占位符（可逆）。

普通 worktree 共享的 Git 管理区、用户目录及网络并未被隔离。首个受保护后端使用已验证的 OCI 隔离视图，只复制净化 manifest；平台/runtime 不满足时 capability=false，不能静默降级。未登记或任意变换的秘密不能靠字符串过滤保证识别；S1 真实 smoke 仅在合成、无项目凭据环境验证。具体探针和降级行为见实施计划 T2.04–T2.06。

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

本节与实施计划 §27 共同定义 3.0 的统一约束；具体字段、HTTP 状态和逐卡前置见计划 §19/§27/§29。修改公共语义须同步三文档及 Schema，不能由子 Agent 各自创造一套。示例 ID 为阅读别名，真实实体沿用 UUID。

### 15.1 身份传播链

所有可执行操作都必须能沿下列链路回溯，ID 创建后不可复用：

```text
Project
  └─ WorkspaceBinding (绑定版本、canonical root、manifest；Git commit 可空)
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

`ExecutionIdentity` 是所有边界函数的必填上下文。以下为有完整执行层级时的例子，不能把全部下游字段要求套在排队、人工文档或阶段审批上：

```json
{
  "project_id": "p_123",
  "binding_id": "wb_4",
  "binding_revision": 7,
  "flow_id": "f_9",
  "stage_id": "st_3",
  "task_id": "task_21",
  "run_id": "run_88",
  "attempt_id": "att_1",
  "agent_asset_id": "agent_claude_bugfix",
  "agent_revision_id": "ar_12",
  "actor": {"type": "agent", "id": "agent_claude_bugfix"}
}
```

HTTP、WS、hook、policy 和 audit 从可信上下文派生身份，不接受请求体伪造 actor。project+actor 必填，Run 事件带 run_id，执行工具必须 attempt+agent revision；gate/人工成果/系统恢复可没有 Run。actor type 支持 user/agent/system/integration；排队和独立手动 Task 的可空层级由服务端验证。

### 15.2 规范数据模型与生命周期

| 实体 | 必填字段 | 终态 | 关键不变量 |
|---|---|---|---|
| Project | id, title, status, revision | archived/completed | archived 不再创建 Run；删除采用标记和异步清理 |
| WorkspaceBinding | id, project_id, kind, canonical_root, revision；Git 字段可空 | archived | 每项目一个 active primary，派生 flow/run binding 可多条；parent_binding 可追溯 |
| Flow | id, project_id, kind, template_version, status, revision | completed/aborted | 一个 active stage；任务流可并行，项目流最多一个 active 主流 |
| Stage | id, flow_id, type, status, order, contract_version | done/skipped/failed | 入口/出口 Gate 只改变状态，不直接写成果 |
| Task | id, project_id, status, revision, input；flow/stage 可空 | completed/cancelled；failed 可显式重试 | 一次最多一个 active Run；failed 重试 CAS 到 queued 并新建 Run；执行成功先 waiting_review |
| Run | id, task_id, identity, binding_revision, base_manifest_hash, budget | completed/failed/cancelled/expired | 终态不可回写；S1 一个 Attempt；重试创建新 Run 并引用 retry_of_run_id |
| Approval | id, project_id, subject, risk, fingerprint, decision, expires_at；run/attempt 按主体可空 | approved/rejected/expired/invalidated | 决策和消费分别幂等，执行时重验 scope/内容/版本；历史决定不改写 |
| Event | id, scope, sequence, type, occurred_at, payload | immutable | sequence 在 scope 内单调；payload 经过 Vault 净化 |
| ArtifactVersion | id, artifact_id, project_id, content_hash, creator, status；run 可空 | 审核状态与不可变内容分离 | 人工成果有来源；同 hash 可用于不同成果，不设全局 hash 唯一 |
| Bookmark | id, project_id, flow_id, manifest, event_cursor, memory visibility；git_ref 可空 | immutable | 固定分支/来源/成果版本并 pin blob/ref |
| MergeOperation | id, run_id, target_binding_revision, candidate_hash, fence, journal | applied/conflict/rolled_back；needs_recovery 待处理 | 与 Run 状态分离；DB/磁盘间通过日志恢复，未恢复不放行新发布 |

状态迁移必须由服务端命令完成，并记录 `from_status/to_status/reason/actor/expected_revision`。未知迁移、终态迁移和跨项目引用均返回结构化错误。

### 15.3 状态机、重试和取消

Run 的规范状态为：

```text
queued -> starting -> running -> (waiting_approval | paused | cancelling)
waiting_approval -> running | cancelling
paused -> running | cancelling
running -> completed | failed | cancelling
cancelling -> cancelled | expired | recovering
starting/running/waiting_approval/paused/cancelling -> recovering (restart)
recovering -> running (verified attach) | failed | cancelled | expired (verified cleanup)
```

- `failed` 表示执行失败，`retryable` 是错误/事件字段而非状态名；后续检查失败阻塞 Contract/MergeOperation，不反改 completed Run。
- `cancel_requested` 先写状态和事件，再向外部进程发送温和终止；超过 grace period 才强制回收。
- 重试必须引用 `retry_of_run_id` 和失败原因，复制任务输入快照，不复制审批决定。
- 暂停只在可验证 checkpoint 的后端启用；hard deadline 与用户取消均先 cancelling，确认进程树结束后才写 expired/cancelled。
- 重启时校验 owner instance、PID 和启动时间；只有支持且完成验证的 attach 能回 running。无法确认旧进程已停止则保留 recovering、配额和工作副本，不能直接启动第二个写入者。
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
  "type": "tool.requested",
  "schema_version": 1,
  "occurred_at": "2026-09-06T08:00:00Z",
  "identity": {"project_id": "p_123", "run_id": "run_88", "agent_revision_id": "ar_12", "attempt_id": "att_1", "actor": {"type": "agent", "id": "agent_claude_bugfix"}},
  "payload": {"tool": "read_file", "path": "src/foo.go"},
  "redaction": {"secrets_replaced": true}
}
```

事件类型最小集合：`run.queued`、`run.started`、`run.delta`、`tool.requested`、`tool.completed`、`file.changed`、`approval.required`、`approval.decided`、`guard.blocked`、`check.completed`、`run.paused`、`run.completed`、`run.failed`、`run.cancelled`、`merge.completed`。

- `sequence` 按 project/run scope 分别投影为 project_seq/run_seq，不能混用；occurred_at 只展示。服务端分配序号，不接受外部 backend 自报序号。
- `GET /events?after=42&limit=200` 返回 events/next_after/has_more/high_watermark/retention_floor；after 小于 retention_floor-1 返回 410 event_gap，大于当前高水位返回 422 invalid_cursor。
- 客户端 subscribe 后服务端回 subscribed、回放到捕获的 H、replay_finished(H)，再发 >H 的实时事件；scope cursor 独立推进，同一 event_id 跨 scope 收到不重复展示。慢消费者关闭 1013 并从最后应用游标补读。
- outbox dispatcher 失败可重试，事件本身不因广播失败而删除；队列满时只能施加背压或断开并要求回放，不能静默丢弃。

### 15.5 HTTP API、幂等和并发

统一资源前缀为 `/api/v1/projects/:pid`；下表路径均相对该前缀，旧路由保持兼容至调用者迁完。核心接口：

| 方法 | 路径 | 语义 |
|---|---|---|
| POST | `/runs` | 创建 Run；202 返回 command_id，支持 Idempotency-Key |
| GET | `/runs/:rid` | 读取 Run、当前 revision 和 usage |
| GET | `/runs/:rid/events?after=` | 按游标回放 |
| POST | `/approvals/:aid/decide` | 决策指定主体，带 fingerprint/expected_revision/approved/reason |
| POST | `/approvals/batch` | 最多 50 项，逐项返回部分成功/失败 |
| POST | `/runs/:rid/cancel` | 请求取消；重复调用返回同一结果 |
| POST | `/runs/:rid/merge` | 以 base revision 获取合入锁并执行三方检查 |
| POST | `/runs/:rid/discard` | 丢弃影子副本并保留审计 |
| GET | `/bookmarks` | 列出不可变书签 |
| POST | `/bookmarks/:bid/fork` | 从书签创建新 Flow/Binding |
| GET | `/commands/:command_id` | 查询 accepted/applied/rejected/reconciling |

所有可变请求：

1. `Idempotency-Key` 必须与请求体 hash 绑定；同 key 不同 body 返回 `idempotency_key_reused`。
2. HTTP If-Match 不成立返回 412；业务 revision/状态冲突返回 409，同时提交不一致的 header/body revision 返回 422。
3. 成功响应统一为 `{data, meta:{request_id, command_id, revision}}`，读请求可无 command_id；失败为 `{error:{code,message,retryable,details}, meta:{request_id}}`。command_id 由客户端预生成，request_id 是本次 HTTP trace。
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
- Run 预算包括 wall time、token、费用、输出字节、磁盘增量等；soft limit 只告警，hard wall/output/disk 超限取消。费用/token 迟报是估算控制，unknown 不记 0；不支持 checkpoint 的后端不提供暂停。
- Attempt 使用独立工作目录、环境快照和 process group/job object；所有子进程继承 `CODEFLOW_RUN_ID`、`CODEFLOW_ATTEMPT_ID`，不得仅用 PID 关联。
- stdout/stderr 持续 drain，使用有界 spool/日志 artifact，达到限额记录截断或取消；不能因审批/前端慢而停读管道。WS 慢消费者单独断开并按游标恢复。
- 退出原因必须区分 `exit_code`、`signal`、`timeout`、`policy_denied`、`orphaned`、`backend_unavailable`。

### 15.8 工作区、影子副本与合入

Run 创建冻结 binding revision、dirty/未跟踪文件的 base_manifest_hash 及可空 base_commit，保留用户 index。工作副本由 `internal/runworkspace` 管理，旧 `internal/shadow` 留给索引迁移；候选修改只能在受控目录发生。合入流程是：

1. 按真实目标 root/repository identity 取得持久 lease/fence 与 merge lock，两个项目同根也共享锁；重验 binding、HEAD 和用户未提交变更。
2. 计算 base/current/result 三方 diff；检测 deleted/renamed/binary/generated 文件。
3. 在临时索引应用完整 diff，逐文件执行 Guard 和项目检查。
4. 检查通过后先持久化每文件 old/new hash 与备份 journal，再逐文件重验并替换；全部 hash 核对成功才提交 applied + merge.completed。SQL 与磁盘不构成事务，多文件中途失败必须补偿。
5. 预检冲突不写目标；发布失败进入 rolling_back，只恢复仍匹配本操作写入 hash 的内容。遇用户新改动标 needs_recovery、保留锁；解决后创建新的 MergeOperation，不能强制覆盖主树。

`PromoteAll` 旧接口在迁移期只能作为单文件兼容 API，不能被 Run 合入路径调用。

### 15.9 Vault、策略和审计边界

模型上下文、MCP 响应、事件 delta、工具输出和任务文件经 Vault 出站净化；凭据解密只通过 CredentialBroker 的授权引用，普通写盘不自动解密。审计保存去敏元数据和来源关联，不保存真值；出站 canary 验收涵盖实际 provider 输入。

策略 `Decision` 增加 `run_id`, `attempt_id`, `risk`, `approval_id`, `fingerprint`, `scope`；任何边界调用没有完整 identity 或没有明确 policy version 都 fail closed。PII 脱敏仍是有损变换，不能替代 Vault 的可逆占位符。

### 15.10 前端缓存、断线和离线模式

- 服务端事实：Project/Flow/Run/Approval/Bookmark 查询以 revision 为准；事件时间线以 sequence 为准。
- 客户端命令状态：draft/submitting/accepted/applied/rejected/unknown；断网或超时 unknown，恢复后以发送前已知的 command_id 查询，禁止盲目重发不同 key。reconciling 表示服务端外部副作用尚在对账。
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

5. 运行事实先进入单一 `codeflow.db`，旧域以引用快照衔接；Flow 在任务流阶段逐域冻结/复制/校验/切换，S12 收剩余旧域。跨旧 SQLite 文件不承诺双写原子，文件审计链通过幂等 outbox 投递。
6. 实际执行顺序以实施计划 §29 的 216 节点依赖图为准，不能按本节的产品顺序省略前置。§30 派发提示必须包含具体文件、上游签名、实施顺序、验收命令与停止条件。
