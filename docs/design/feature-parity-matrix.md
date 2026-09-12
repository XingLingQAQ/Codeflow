# CodeFlow 功能对等矩阵（Feature Parity Matrix）

> 创建：2026-07-11  
> 状态：Living document  
> 审计基线：与 `docs/plans/2026-07-11-codeflow-2.0-implementation-and-hardening-plan.md` 同步  
> 图例：✅ 可用 · ⚠️ 半实现 · ❌ 缺失 · 🔄 需重构/合并 · 🚫 非目标（本阶段）

**维护约定**：每个里程碑结束更新「状态」与「备注」；禁止把未落地模块标为 ✅。

---

## 1. 工程与主线

| ID | 能力 | 状态 | 现状摘要 | 目标 | 里程碑 | 备注 |
|---|---|---|---|---|---|---|
| G01 | 单一前端主线 | ✅ | **G01 后**：唯一主线 `apps/workbench` / `@codeflow/workbench`；物理无 `apps/desktop` / `codeflow_template` | 保持单树；禁止再引入第二前端 | M0 | **2026-07-12 G01**：rename `apps/desktop`→`apps/workbench`；兼容 `dev:desktop`/`build:desktop` 别名；Nx tag `platform:desktop` 保留（平台语义） |
| G02 | 文档 SSOT（design/plans/adr） | ✅ | **M0.6**：`docs/README.md` IA；design/plans/adr/requirements；5 份 2.0 设计纳入跟踪；early/historical 迁入；ADR 0001/0002 | 持续按 IA 写入；禁止再散落 plan/archive | M0 | **2026-07-13**：见 `docs/adr/0001-docs-information-architecture.md`；openapi 仍 `backend/docs/openapi.yaml` |
| G03 | 本矩阵持续跟踪 | ✅ | 本文档 | 每里程碑更新 | M0 | |
| G30 | CGO / SQLite 基线决策 | ✅ | **M0.9**：维持 `mattn/go-sqlite3` + `CGO_ENABLED=1`；Makefile `build-all` 对齐为 1；modernc 后置 | 迁移须独立 ADR/PR | M0 | `docs/adr/0003-sqlite-cgo.md` |
| G32 | Sidecar 信任边界 | ✅ | 默认 loopback；每次启动随机 token；HTTP Bearer；精确 CORS/WS Origin；会话/辩论/项目 WS scope | 启动器消费握手，前端仅内存持有 token；远程配对后置 | Hardening B1 | `docs/adr/0004-sidecar-trust-boundary.md`；认证、轮换、监听、Origin、WS 测试 |
| G33 | API credential secret boundary | ✅ | `SecretStore` 独立加密文件（Windows DPAPI；非 Windows AES-256-GCM + 外部 master key）；配置读取只返回 `secret_ref`、状态、掩码和版本；旧 `config_json.api_key` 启动迁移 fail-closed；adapter 运行时按引用解析 | 系统钥匙串适配器、健康检查和前端配置 UI | Hardening B2 | `docs/adr/0005-api-credential-secret-store.md`；配置/迁移/轮换/删除/运行时解析测试；CGO/SQLite 全量测试待编译器环境 |

---

## 2. 前端壳与体验

| ID | 能力 | 状态 | 现状摘要 | 目标 | 里程碑 | 备注 |
|---|---|---|---|---|---|---|
| G04 | 设计系统 + 基础组件 | ⚠️ | `src/ui` 令牌体系（theme.css 光/暗变量 + .dark scope）；16 UI 组件（Button/Dialog/Tooltip/ScrollArea 等）；Tailwind + Radix；light 默认 + ThemeMode toggle（light/dark/system） | Tailwind 令牌 + Radix 基座 | M1 | 2026-07-27：组件/令牌就位但尚无独立 storybook/文档 |
| G05 | App Shell 路由 IA | ✅ | **8 路由**（/, /projects, /workbench/:id/:stage, /flows, /agents, /plugins, /config(/:section), /settings）+ catch-all redirect；AnimatePresence 页切换；RouteProgress | 设计路由表 | M1 | 2026-07-27 落地 |
| G06 | Flow Rail + Dockview 工作台 | ⚠️ | FlowRail + FlowProgress（阶段进度条/流程导轨）；StageCanvasHost 七画布 lazy 切换（idea/design/planning/research/coding/review/submit）；无 Dockview | 阶段自适应布局 | M1 | 2026-07-27：骨架质量，Dockview 未接入 |
| G07 | 启动 / 加载体系 | ⚠️ | StartupGate 启动门（健康检查 + 错误/重试 UI）；Suspense canvas fallback | 启动窗 + 骨架 + 失败态 | M1 | 2026-07-27：基础就位，splash screen 无 |
| G13 | 规划 / 提交画布 | ⚠️ | Plan 视图 + workflow 观测模型 | 阶段画布 | M2 | adapters/workflows 可复用 |
| G15 | 编码画布 | ❌ | 无 Monaco 工作台 | stages/coding | M3 | |
| G18 | 想法 / 设计 / Review 画布 | ❌ | 无 | stages/* | M4 | |
| G21-UI | 调研 / 导入理解画布 | ❌ | 无 | DeepSearch + comprehension | M5 | |
| G22-UI | 配置中心四子区 | ⚠️ | Settings 散落 | 渠道/模型/MCP/Skill | M5 | |
| G24 | Live Preview + 检查器桥 | ❌ | 无 | Tauri WebView / iframe | M6 | |
| G25 | 网页响应式 + 手机伴侣 | ❌ | 无 mobile app | embed 响应式 + apps/mobile | M7 | |

---

## 3. 流程与快照

| ID | 能力 | 状态 | 现状摘要 | 目标 | 里程碑 | 备注 |
|---|---|---|---|---|---|---|
| G08 | Snapshot 真 restore | ⚠️ | conversation/vector/graph **真 restore**（PR-4/5）；git hard reset 仍 opt-in；**SnapshotRestorer + DefaultSnapshotRestorer** 已入 main（非破坏性，git restore env-gated）；loop restore 发射 `flow.restored`；**VerifyConsistency**（M2.A6）；token 硬化（future schema/空 digest 拒绝）；55 测试 | 产品化 API + 受控 git + 执行锁 | M2 前置 | experimental 警告已更新 |
| G09 | Flow 执行引擎 | ⚠️ | 状态机 + SQLite + abort/gate decide + timeline/overview 桥；**Gate.OnFail**（block\|escalate_to_debate）+ gate.escalate_debate 事件；enter gates 于 Create/Advance/Skip/Loop 评估（gate.waiting）；**ExecutionGuard** 阶段锁；**RegisterTemplate/UnregisterTemplate** + 验证；Gate.Config；**GateEscalationHandler** → debate 桥（main adapter: builtin-code-artisan vs builtin-red-critic）；**ExportTemplateJSON/ImportTemplateJSON** 模板 JSON 导入导出；P2-3 双重 enter-gate 修复；P2-4 Create 竞态修复；67 测试 | WS flow.* + 模板市场 | M2 | `internal/floweng` |
| G10 | 阶段自动快照与回跳 | ⚠️ | advance 可挂 snapshot hook；loop 回跳存在；**loop restore 发射 flow.restored**；未绑全量 restore UX | loop → snapshot restore 闭环 | M2 | |
| G11 | Artifact / Gate 一等公民 | ⚠️ | gate decide + **artifact attach/list API** + loop stale；**Artifact.CreatedBy**（agent\|user）+ **AttachArtifactBy**；**Gate.Config** map[string]string；Gate.OnFail escalate_to_debate + escalation→debate 桥 | 完整审批 UI + content_ref | M2 | |
| G12 | 工作流模板可视化编辑器 | ⚠️ | **ExportTemplateJSON / ImportTemplateJSON** 域层落地；API 路由 POST `/flows/templates/import` 已接；无可视化编辑器 UI | 节点画布 JSON 导入导出 | M2 | 2026-07-27：G12 域 + API 就位，前端待做 |
| G14 | workflow 观测与引擎合一 | ⚠️ | timeline **已合并** floweng 事件（lane=floweng）；**replay 合并 floweng events**（collectFlowengReplayEvents）；overview/replay 仍 planner/audit 拼装 | overview 含 Flow 状态；replay 消费 flow events | M2 | 2026-07-15 bridge |

---

## 4. Agent、辩论、守卫

| ID | 能力 | 状态 | 现状摘要 | 目标 | 里程碑 | 备注 |
|---|---|---|---|---|---|---|
| G19 | 多方多模型辩论 | ⚠️ | Generator/Critic(+Mediator)；**flow_id/stage_id FK**；**SQLite 持久化**（`data/debates.db`，write-through + rollback-on-persist-failure）；**N 方数据模型**（Parties 2..N per-party model/channel，PartyContribution mirroring）；clone fixes（SelectSolution/ResolveConflict/ProposeSolution）；26 测试 | 2~N 运行时 adapter routing per party | M4 | `internal/debate` |
| G20 | Agent 广场 / Registry | ⚠️ | **AgentRegistry** CRUD + SQLite 持久化（`data/agents.db`，main+bootstrap 接线）；5 builtin prompted agents；**SelectForStage** 推荐；**RenderInjectionForAgent** skill 挂载注入；agents/registry API（7 routes 含 usage/score）；45 测试；无 UI/市场 | 版本/来源/插槽/市场 UI | M4 | 2026-07-27 backend+API landed |
| G16 | `internal/workspace` | ⚠️ | list/read/stat/write + 沙箱 + **stage/promote/promote-all/discard/discard-all** + staged read；**polling Watcher + WSNotifier**（workspace_event topic + `workspace:root:{hash8}`）；watch/watches/delete HTTP API（experimental，capped 16，idempotent per root）；**dev-server 管理**（DetectScripts + Start/Stop/Logs/List/Shutdown，环形日志，Windows 进程树击杀）+ HTTP scripts/dev-servers；junction/symlink escape fail-closed fix；43 测试 | project root 绑定 | M3 | 2026-07-27 watch+dev-server |
| G17 | `internal/guard` | ⚠️ | WriteGuard + AST 重复检测 + guard.yaml + IndexTree + audit + check/index/**exempt** + **SQLite 豁免持久化**（`guard_exemptions.db`）+ stage/promote 链路；**deprecated_path 规则**（deprecated_path_globs，默认 warn）；**豁免审批流**（ExemptionRequest pending/approved/rejected，decide→GrantExemption，audit 双事件，HTTP exemption-requests）；49 测试 | 全量 shadow | M3 | 2026-07-27 |
| — | 双方辩论 API | ✅ | create/round/resolve/export/stream | 保留并升级 | M4 | 不回退现有 API 直至兼容层 |

---

## 5. 记忆、检索、配置、插件

| ID | 能力 | 状态 | 现状摘要 | 目标 | 里程碑 | 备注 |
|---|---|---|---|---|---|---|
| — | 双轨记忆 / SAMG 基础 | ✅ | memory + samg | 继续增强 | 贯穿 | 快照 capture 已用 |
| — | 配置系统 / 热切换 | ✅ | config + hotswap | 配置中心 UI 增强 | M5 | |
| — | Hook / 审计 / 隐私 / 披露 | ✅ | 底座存在 | 与 gate/guard 串联 | 贯穿 | |
| — | 黑板 / 指挥官 | ✅ | 存在 | 挂接阶段 | 贯穿 | |
| G21 | DeepSearch + 导入流水线 | ⚠️ | search/retriever 底座 | 联网 Provider + 导入流 | M5 | |
| G22 | Skill 资产服务 | ⚠️ | Registry CRUD/Match/Inject + **SQLite 持久化**（`skills.db`）；2 builtin；API experimental；**版本历史**（skill_versions 表 cap 20，ListVersions/RollbackVersion 入 Registry 接口）；frontmatter/match/import edge 测试；27 测试 | frontmatter 市场 + Agent 挂载 UI | M5 | 2026-07-26 |
| G23 | 插件贡献点 + 沙箱 | ⚠️ | plugin + isolation 部分 | 注册表与替换点 | M6 | |

---

## 6. 架构卫生

| ID | 能力 | 状态 | 现状摘要 | 目标 | 里程碑 | 备注 |
|---|---|---|---|---|---|---|
| G26 | DI 去除全局 Get/Set | ⚠️ | bootstrap B0+B1 八服务 Apply；handlers 仍走 Get* | 按域继续 B2+ 删除全局 | 贯穿 | **2026-07-15 PR-6**：Snapshot/Debate/Summarize 已入 bootstrap |
| G27 | summarize 合并 | ✅ | **M0.8**：仅 `internal/summarize`；engine（Compressor/TokenCounter）迁入；删除 `internal/summarizer` | 保持单包；API 面不变 | M0 | **2026-07-15**：handlers/OpenAPI 仍 `/api/v1/summarize`；EntitySkeleton 与 API DecisionSkeleton 分型 |
| G28 | Schema-first OpenAPI | ⚠️ | 有 TS 生成脚本 | YAML SSOT + CI | 贯穿 | 输出 `apps/workbench/generated/`；契约 `backend/docs/openapi.yaml` |
| G29 | WS 统一事件总线 | ⚠️ | hub 存在；后端仍可广播全局 topic；客户端升级已按会话/辩论/项目固定 scope，不能跨资源订阅；workspace 尚未绑定 Project root | 单连接多路受权 topics | M2–M3 | 2026-08-18 B1 完成身份/Origin/scope，项目根绑定待 B3 |
| G31 | 仓库生成物 hygiene | ✅ | untrack node_modules；gitignore 强化；CI Repo Hygiene Guard | 持续禁止 tracked 生成物/依赖 | M0 | M0.5：`scripts/check-repo-hygiene.mjs` + `pnpm check:repo-hygiene` |

---

## 7. 后端 API 面（摘要）

| 路由族 | 状态 | 说明 |
|---|---|---|
| `/api/v1/snapshots` | ⚠️ Experimental | 真 restore（conv/vector/graph）；git opt-in |
| `/api/v1/workflows/:projectId/*` | ⚠️ 观测 | overview/timeline/replay；timeline/summary 含 floweng |
| `/api/v1/debates` | ⚠️ 双方 | list/create + flow_id/stage_id 过滤与 FK；**solutions**（openapi 230/230 对齐） |
| `/api/v1/flows` | ⚠️ Experimental | templates(+id)/create/stages(+id)/active-stage/artifacts(+id)/gates/events/advance/skip/loop/abort |
| `/api/v1/workspace` | ⚠️ Experimental | list/read/stat/write/promote/promote-all/staged/discard/discard-all；**watch/watches/delete** |
| `/api/v1/skills` | ⚠️ Experimental | CRUD/match/inject/import/export |
| `/api/v1/guard` | ⚠️ Experimental | config/rules/check/index/exempt/exemptions |
| `/api/v1/projects/:id/stream` | ✅ 受权 WS | Bearer 或浏览器子协议认证；仅 `flow:project:{id}` |
| 静态 embed `/` | ✅ | `static.go` + dist（需构建同步） |

---

## 8. 更新日志

### 2026-08-19 B3 Project binding

Project rows now own canonical workspace roots plus default Flow/Session IDs
and binding state. Workspace APIs resolve `project_id + relative_path`; new
roots fail closed without `CODEFLOW_WORKSPACE_ROOTS` unless the explicit desktop
migration switch is enabled. Create uses a forward-only recovery journal;
archive/restore uses a lifecycle journal, aborts active Flows, stops local
watchers/dev-servers, and restores a runnable Flow without deleting retained
Session/Flow history.

### 2026-08-19 B5 durable runtime state

Production now owns durable SQLite stores for Session/message,
conversation/trace, Memory/Atomic/vector/Raw Archive, and SAMG graph/access
metadata. Legacy getters no longer manufacture empty in-memory data. Audit
startup rejects corruption, retained files verify from a persisted rotation
anchor, shutdown syncs buffered writes, and authenticated mutation successes
and failures receive baseline records. See ADR 0008; SQLite runtime execution
remains gated by the CGO-capable B11 environment.

| 日期 | 变更 |
|---|---|
| 2026-07-11 | 初版：对齐 2.0 实施计划 §3 差距矩阵 |
| 2026-07-11 | G01 备注补强：App.tsx 同 SHA + Nx project.json 错误绑定证据（与 2.0 计划第二轮复核对齐） |
| 2026-07-11 | **PR-2**：工具链改绑 `apps/desktop`；G01 备注更新（双树残留 → 仍 ⚠️，待 PR-3） |
| 2026-07-12 | **PR-3**：删除 `codeflow_template`；G01 现状改为物理双树已删，仍 ⚠️ 至 rename workbench |
| 2026-07-12 | **G01**：rename `apps/desktop`→`apps/workbench`；G01 → ✅ |
| 2026-07-13 | **M0.5**：G31 仓库生成物 hygiene ✅；untrack node_modules；CI Repo Hygiene Guard |
| 2026-07-13 | **M0.6**：G02 文档 SSOT ✅；docs IA + ADR 0001/0002；2.0 设计五件套纳入跟踪；early/plan/backend 散文收敛 |
| 2026-07-15 | **M0.8**：G27 summarize 合并 ✅；删除 `internal/summarizer`；engine 并入 `internal/summarize` |
| 2026-07-15 | **M0.9**：G30 CGO/SQLite ADR ✅；`docs/adr/0003-sqlite-cgo.md`；Makefile `build-all` CGO=1 |
| 2026-07-15 | **PR-6 收尾**：bootstrap 注入 Snapshot/Debate/Summarize；G26 备注更新 |
| 2026-07-15 | **PR-8**：`internal/floweng` 最小机 + experimental flows API；G09 → ⚠️ |
| 2026-07-15 | **M3.1**：`internal/workspace` list/read/write + WriteGuard 钩子；G16 → ⚠️ |
| 2026-07-15 | **M3.2**：`internal/guard` 引擎 + workspace 强制挂钩；G17 → ⚠️ |
| 2026-07-15 | **M5.0**：`internal/skill` registry + match/inject API；G22 → ⚠️ |
| 2026-07-15 | **G14 bridge**：workflow timeline 合并 floweng events |
| 2026-07-15 | **floweng SQLite**：FlowStore + SQLiteFlowStore；main 默认 `data/floweng.db` |
| 2026-07-15 | **guard AST**：SymbolIndex 跨文件 duplicate_symbol 规则 |
| 2026-07-15 | **skill SQLite**：`NewSQLiteRegistry`；main → `data/skills.db` |
| 2026-07-15 | **guard.yaml + IndexTree + audit bridge**；main 挂 audit；skill frontmatter；openapi flows/workspace/skills |
| 2026-07-15 | workspace staging/promote; floweng abort + SQLite；skill import dir；guard /check /index；audit files；overview flow counts |
| 2026-07-15 | **OpenAPI**：补齐 flows abort/delete/stages/artifacts、skills import、guard exempt 与契约检查 |
| 2026-07-15 | **guard exemptions SQLite**：`data/guard_exemptions.db`；过期行 load 时清理；skill `ListFiltered` 入 Registry 接口 |
| 2026-07-15 | **guard exemptions API**：GET `/exemptions` + DELETE `/exempt`；list/clear 与持久化联动 |
| 2026-07-15 | **WS topics**：Hub 主题订阅；`flow_event` / `flow:project:{id}` 按 topic 广播 |
| 2026-07-15 | **workspace staged list**：GET `/workspace/staged` 枚举 `.codeflow/staging` |
| 2026-07-15 | **workspace discard**：POST `/workspace/discard` 放弃 staging 文件 |
| 2026-07-15 | **debate list**：GET `/debates` 支持 status/flow_id/stage_id 过滤；CI CGO=1 硬门禁 |
| 2026-07-15 | **flow template by id**；workspace promote 清理 staging；guard GET `/config`；smoke 扩展 |
| 2026-07-15 | **workspace stat**；debate WS topics；promote clears staging |
| 2026-07-15 | **workspace promote-all** + staged read query；shadow pipeline 闭环 |
| 2026-07-15 | **workspace discard-all** 批量放弃 staging |
| 2026-07-15 | **flow stage by id** GET `/flows/:id/stages/:sid` |
| 2026-07-15 | **flow artifact by id** GET `/flows/:id/artifacts/:aid` |
| 2026-07-15 | **skill export** GET `/skills/export` markdown dump；events type/stage 过滤 |
| 2026-07-15 | **workflow debate_count**；guard relative exemption 匹配；smoke bulk staging |
| 2026-07-15 | **flow gates list** GET `/flows/:id/gates`；events limit 查询 |
| 2026-07-15 | **flow active-stage** GET `/flows/:id/active-stage` |
| 2026-07-15 | **guard rules list** GET `/guard/rules` |
| 2026-07-26 | **G19 debate 持久化**：SQLite `data/debates.db`（write-through + rollback-on-persist-failure）；N 方数据模型（Parties 2..N，per-party model/channel，PartyContribution mirroring）；clone fixes；26 测试 |
| 2026-07-26 | **G09/G10/G11 floweng 强化**：Gate.OnFail（block\|escalate_to_debate）+ gate.escalate_debate 事件；enter gates 于 Create/Advance/Skip/Loop 评估；ExecutionGuard 阶段锁；RegisterTemplate/UnregisterTemplate + 验证；Artifact.CreatedBy；Gate.Config；SnapshotRestorer wired（非破坏性）；loop restore 发射 flow.restored；53 测试（+32 新） |
| 2026-07-26 | **G14 replay**：collectFlowengReplayEvents 合并 floweng events 入 replay |
| 2026-07-26 | **G16 workspace watch**：polling Watcher + WSNotifier（workspace_event topic + `workspace:root:{hash8}`）；watch/watches/delete HTTP API（experimental，capped 16）；junction/symlink escape fail-closed fix（ModeIrregular rejection）；staging 强化测试；28 测试 |
| 2026-07-26 | **G17 guard deprecated_path**：deprecated_path 规则（deprecated_path_globs，默认 warn）；规则 severity override 测试；exemption lifecycle race/expiry 测试；IndexTree + CheckAndCommit 互动测试；35 测试 |
| 2026-07-26 | **G22 skill 版本历史**：skill_versions 表（cap 20）；ListVersions/RollbackVersion 入 Registry 接口；frontmatter BOM/alt-key/edge 测试；match/import edge 测试；SQLite reload 持久化测试；27 测试 |
| 2026-07-26 | **G20 agent backend**：AgentRegistry CRUD + SQLite `data/agents.db`；5 builtin prompted agents；Match/RenderInjection；23 测试 |
| 2026-07-26 | **OpenAPI 230/230 对齐**：debate solutions、workspace watch、agent routes 全量覆盖；契约检查通过 |
| 2026-07-26 | **WS topics**：新增 `workspace_event` topic（`workspace:root:{hash8}`）|
| 2026-07-27 | **G17 豁免审批流**：ExemptionRequest pending/approved/rejected + HTTP；**G16 dev-server** 进程管理 + HTTP；**G20** SelectForStage + skill mounts + registry API；**G12** template JSON import/export/delete HTTP；**插件贡献点注册表** M6.1 切片；**前端 M1** 新壳（浅色默认+深色切换、mock 层、调色板对齐、侧栏展开、黑白简约 chrome） |
| 2026-08-18 | **后端 Hardening B1**：sidecar 默认 loopback；进程令牌握手；API Bearer；精确 CORS/WS Origin；浏览器 WS 子协议；会话/辩论/项目 topic scope；ADR 0004 |
| 2026-08-19 | **后端 Hardening B2**：API credential SecretStore 独立加密存储；公开/写入配置模型分离；旧 `config_json.api_key` 迁移 fail-closed；OpenAPI `api_key` writeOnly；ADR 0005；SQLite 依赖验收因当前 Windows 无 `gcc` 留待 B11 |
