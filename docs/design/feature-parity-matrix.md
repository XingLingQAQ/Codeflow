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

---

## 2. 前端壳与体验

| ID | 能力 | 状态 | 现状摘要 | 目标 | 里程碑 | 备注 |
|---|---|---|---|---|---|---|
| G04 | 设计系统 + 基础组件 | ❌ | 无统一 `src/ui` 令牌体系 | Tailwind 令牌 + Radix 基座 | M1 | |
| G05 | App Shell 路由 IA | ⚠️ | `ViewMode` 控制台八页 | 设计路由表 | M1 | types.ts ViewMode |
| G06 | Flow Rail + Dockview 工作台 | ❌ | 无 | 阶段自适应布局 | M1 | |
| G07 | 启动 / 加载体系 | ⚠️ | 基础加载 | 启动窗 + 骨架 + 失败态 | M1 | |
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
| G08 | Snapshot 真 restore | ⚠️ | conversation/vector/graph **真 restore**（PR-4/5）；git hard reset 仍 opt-in | 产品化 API + 受控 git + 执行锁 | M2 前置 | experimental 警告已更新 |
| G09 | Flow 执行引擎 | ⚠️ | 状态机 + SQLite + abort/gate decide + timeline/overview 桥 | WS flow.* + 模板市场 | M2 | `internal/floweng` |
| G10 | 阶段自动快照与回跳 | ⚠️ | advance 可挂 snapshot hook；loop 回跳存在；未绑全量 restore UX | loop → snapshot restore 闭环 | M2 | |
| G11 | Artifact / Gate 一等公民 | ⚠️ | gate decide + **artifact attach/list API** + loop stale | 完整审批 UI + content_ref | M2 | |
| G12 | 工作流模板可视化编辑器 | ❌ | 无 | 节点画布 JSON 导入导出 | M2 | |
| G14 | workflow 观测与引擎合一 | ⚠️ | timeline **已合并** floweng 事件（lane=floweng）；overview/replay 仍 planner/audit 拼装 | overview 含 Flow 状态；replay 消费 flow events | M2 | 2026-07-15 bridge |

---

## 4. Agent、辩论、守卫

| ID | 能力 | 状态 | 现状摘要 | 目标 | 里程碑 | 备注 |
|---|---|---|---|---|---|---|
| G19 | 多方多模型辩论 | ⚠️ | Generator/Critic(+Mediator)；**flow_id/stage_id FK**；内存 | 2~N + model/channel | M4 | `internal/debate` |
| G20 | Agent 广场 / Registry | ⚠️ | agent 服务基础能力 | 版本/来源/插槽/市场 UI | M4 | |
| G16 | `internal/workspace` | ⚠️ | list/read/write + 沙箱 + **stage/promote**；API experimental；无 watch/WS | watch + project root 绑定 | M3 | 2026-07-15 |
| G17 | `internal/guard` | ⚠️ | WriteGuard + AST 重复检测 + guard.yaml + IndexTree + audit + check/index/**exempt** + stage/promote 链路 | 持久化豁免审批 + 全量 shadow | M3 | 2026-07-15 |
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
| G22 | Skill 资产服务 | ⚠️ | Registry CRUD/Match/Inject + **SQLite 持久化**（`skills.db`）；2 builtin；API experimental | frontmatter 市场 + Agent 挂载 UI | M5 | 2026-07-15 |
| G23 | 插件贡献点 + 沙箱 | ⚠️ | plugin + isolation 部分 | 注册表与替换点 | M6 | |

---

## 6. 架构卫生

| ID | 能力 | 状态 | 现状摘要 | 目标 | 里程碑 | 备注 |
|---|---|---|---|---|---|---|
| G26 | DI 去除全局 Get/Set | ⚠️ | bootstrap B0+B1 八服务 Apply；handlers 仍走 Get* | 按域继续 B2+ 删除全局 | 贯穿 | **2026-07-15 PR-6**：Snapshot/Debate/Summarize 已入 bootstrap |
| G27 | summarize 合并 | ✅ | **M0.8**：仅 `internal/summarize`；engine（Compressor/TokenCounter）迁入；删除 `internal/summarizer` | 保持单包；API 面不变 | M0 | **2026-07-15**：handlers/OpenAPI 仍 `/api/v1/summarize`；EntitySkeleton 与 API DecisionSkeleton 分型 |
| G28 | Schema-first OpenAPI | ⚠️ | 有 TS 生成脚本 | YAML SSOT + CI | 贯穿 | 输出 `apps/workbench/generated/`；契约 `backend/docs/openapi.yaml` |
| G29 | WS 统一事件总线 | ⚠️ | hub 存在；**flow_event 广播**；debate stream 仍独立 | 单连接多路 topics | M2–M3 | 2026-07-15 floweng→WS |
| G31 | 仓库生成物 hygiene | ✅ | untrack node_modules；gitignore 强化；CI Repo Hygiene Guard | 持续禁止 tracked 生成物/依赖 | M0 | M0.5：`scripts/check-repo-hygiene.mjs` + `pnpm check:repo-hygiene` |

---

## 7. 后端 API 面（摘要）

| 路由族 | 状态 | 说明 |
|---|---|---|
| `/api/v1/snapshots` | ⚠️ Experimental | 真 restore（conv/vector/graph）；git opt-in |
| `/api/v1/workflows/:projectId/*` | ⚠️ 观测 | overview/timeline/replay；timeline/summary 含 floweng |
| `/api/v1/debates` | ⚠️ 双方 | 可选 flow_id/stage_id FK |
| `/api/v1/flows` | ⚠️ Experimental | create/advance/skip/loop/abort/gate/artifacts |
| `/api/v1/workspace` | ⚠️ Experimental | list/read/write/promote |
| `/api/v1/skills` | ⚠️ Experimental | CRUD/match/inject/import |
| `/api/v1/guard` | ⚠️ Experimental | check/index |
| 静态 embed `/` | ✅ | `static.go` + dist（需构建同步） |

---

## 8. 更新日志

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
