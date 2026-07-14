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
| G30 | CGO / SQLite 基线决策 | ⚠️ | go-sqlite3 + CGO=1 | ADR 决策 | M0 | 落地可后置 |

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
| G08 | Snapshot 真 restore | ⚠️ | capture=digest；restore=token 校验；git opt-in | 四态真恢复 + 受控 git | M2 前置 | `snapshot/state_provider.go` |
| G09 | Flow 执行引擎 | ❌ | 无 `internal/floweng` | Flow/Stage/Artifact/Gate | M2 | |
| G10 | 阶段自动快照与回跳 | ❌ | 无 | exit → snapshot；loop restore | M2 | 依赖 G08 |
| G11 | Artifact / Gate 一等公民 | ❌ | 无 | 模型 + API | M2 | |
| G12 | 工作流模板可视化编辑器 | ❌ | 无 | 节点画布 JSON 导入导出 | M2 | |
| G14 | workflow 观测与引擎合一 | ⚠️ | `internal/workflow` 只读拼装 | events 由 floweng 产生 | M2 | overview/timeline/replay 已有 |

---

## 4. Agent、辩论、守卫

| ID | 能力 | 状态 | 现状摘要 | 目标 | 里程碑 | 备注 |
|---|---|---|---|---|---|---|
| G19 | 多方多模型辩论 | ⚠️ | Generator/Critic(+Mediator)，内存 | 2~N + model/channel + stage FK | M4 | `internal/debate` |
| G20 | Agent 广场 / Registry | ⚠️ | agent 服务基础能力 | 版本/来源/插槽/市场 UI | M4 | |
| G16 | `internal/workspace` | ❌ | 无 | list/read/watch + dev server | M3 | |
| G17 | `internal/guard` | ❌ | 无强制写入锚点 | hook_before_write + shadow | M3 | shadow/ast 资产可复用 |
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
| G22 | Skill 资产服务 | ❌ | 无 `internal/skill` | 存储 + 注入 | M5 | |
| G23 | 插件贡献点 + 沙箱 | ⚠️ | plugin + isolation 部分 | 注册表与替换点 | M6 | |

---

## 6. 架构卫生

| ID | 能力 | 状态 | 现状摘要 | 目标 | 里程碑 | 备注 |
|---|---|---|---|---|---|---|
| G26 | DI 去除全局 Get/Set | ⚠️ | bootstrap 五服务 + 大量单例 | 按域分批删除 | 贯穿 | |
| G27 | summarize 合并 | ✅ | **M0.8**：仅 `internal/summarize`；engine（Compressor/TokenCounter）迁入；删除 `internal/summarizer` | 保持单包；API 面不变 | M0 | **2026-07-15**：handlers/OpenAPI 仍 `/api/v1/summarize`；EntitySkeleton 与 API DecisionSkeleton 分型 |
| G28 | Schema-first OpenAPI | ⚠️ | 有 TS 生成脚本 | YAML SSOT + CI | 贯穿 | 输出 `apps/workbench/generated/`；契约 `backend/docs/openapi.yaml` |
| G29 | WS 统一事件总线 | ⚠️ | hub 存在；多处独立 stream | 单连接多路 topics | M2–M3 | debate stream 待迁入 |
| G31 | 仓库生成物 hygiene | ✅ | untrack node_modules；gitignore 强化；CI Repo Hygiene Guard | 持续禁止 tracked 生成物/依赖 | M0 | M0.5：`scripts/check-repo-hygiene.mjs` + `pnpm check:repo-hygiene` |

---

## 7. 后端 API 面（摘要）

| 路由族 | 状态 | 说明 |
|---|---|---|
| `/api/v1/snapshots` | ⚠️ Experimental | capture 可用；restore 非真恢复 |
| `/api/v1/workflows/:projectId/*` | ⚠️ 观测 | overview/timeline/replay |
| `/api/v1/debates` | ⚠️ 双方 | 未绑 Flow stage |
| `/api/v1/flows` / gates | ❌ | 设计有、代码无 |
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
