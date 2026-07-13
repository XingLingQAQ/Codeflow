# CodeFlow 文档索引（Docs IA）

> 状态：Active  
> 建立：2026-07-13（M0.6）  
> 约定：文档三分法 + 需求/参考；**以真实代码为准**，未实现能力不得写成已实现。

---

## 1. 目录约定

| 目录 | 用途 | 放什么 | 不放什么 |
|---|---|---|---|
| `docs/design/` | 设计正文（Living / 目标态） | 产品/架构/模块详细设计、对等矩阵 | 一次性执行 checklist、过期审计长文 |
| `docs/plans/` | 实施计划与里程碑 | 可执行计划、进度看板、PR 清单 | 纯产品愿景（应在 design） |
| `docs/adr/` | 架构决策记录 | 已拍板或待决的重大取舍 | 长篇设计展开（链到 design） |
| `docs/requirements/` | 需求/范围说明 | 功能范围、验收意图 | 实现细节与进度 |
| `docs/` 根文件 | 参考与索引 | 概览、目录结构、专题说明 | 新的默认「第二真相源」 |

### 命名

- 计划 / 需求：`YYYY-MM-DD-<slug>.md`（历史迁移可加 `-historical` 后缀）
- ADR：`NNNN-<slug>.md`（四位序号，从 0001 起）
- 设计：语义化短名，如 `flow-engine.md`；总路线图可保留日期前缀

### 状态标签（文首）

`Active` · `Living` · `Accepted` · `Draft` · `Superseded` · `Historical`

### 写入规则

1. **SSOT**：同一主题只维护一份 Active/Living 正文；旧文标 Superseded/Historical 并链到新文。
2. **进度同步**：每完成里程碑，至少更新：
   - `docs/plans/2026-07-11-codeflow-2.0-implementation-and-hardening-plan.md`
   - `docs/design/feature-parity-matrix.md`
   - 相关裁决 / ADR
3. **中文优先**；代码路径、命令、包名保持仓库真实写法。
4. **`backend/docs/openapi.yaml`** 仍是 OpenAPI 契约源，**不**迁入 `docs/`（契约 ≠ 设计散文）。
5. 禁止再在仓库根 `plan/`、`archive/docs/`、`backend/docs/plans|requirements` 新增散文文档。

---

## 2. Living / Active 入口（先读这些）

| 文档 | 角色 |
|---|---|
| [2.0 实施与硬化计划](plans/2026-07-11-codeflow-2.0-implementation-and-hardening-plan.md) | 主实施计划 / 进度看板 / M0 门禁 |
| [前端主线裁决](frontend-mainline-decision.md) | 唯一主前端 = `apps/workbench` |
| [功能对等矩阵](design/feature-parity-matrix.md) | 能力差距 Living 跟踪 |
| [2.0 总体路线图](design/2026-06-12-overall-roadmap.md) | 产品重定位与里程碑总表 |
| [Flow Engine 设计](design/flow-engine.md) | `internal/floweng` 目标模型 |
| [Workbench / Shell 设计](design/workbench-and-shell.md) | App Shell + 阶段画布 |
| [Agent 质量体系](design/agent-quality-system.md) | 广场 / 辩论 / 守卫 / 配置 |
| [前端体验设计](design/frontend-experience.md) | 设计系统 / 启动 / 多端 |

---

## 3. 目录地图

```text
docs/
├── README.md                          # 本文件（IA + 索引）
├── frontend-mainline-decision.md      # 前端主线裁决（Active）
├── PROJECT_OVERVIEW.md                # 项目概览（参考）
├── DIRECTORY_STRUCTURE.md             # 仓库目录说明（参考）
├── FRONTEND_FEATURES.md               # 前端功能清单（参考，可能滞后）
├── TEST_COVERAGE.md
├── REFACTORING_2026-05-27.md
├── adr/
│   ├── README.md
│   ├── 0001-docs-information-architecture.md
│   └── 0002-directory-structure-and-archive.md
├── design/
│   ├── 2026-06-12-overall-roadmap.md
│   ├── feature-parity-matrix.md
│   ├── flow-engine.md
│   ├── workbench-and-shell.md
│   ├── agent-quality-system.md
│   ├── frontend-experience.md
│   └── early/                         # 早期设计归档（Historical）
├── plans/                             # 实施计划
└── requirements/                      # 需求文档
```

相关契约（不在本树散文索引内维护实现细节）：

- OpenAPI：`backend/docs/openapi.yaml`

---

## 4. M0.6 迁移记录（2026-07-13）

| 来源 | 去向 |
|---|---|
| 5 份 untracked 2.0 设计 dump | `docs/design/*`（纳入跟踪） |
| `archive/docs/early-design/*` | `docs/design/early/*`（中文文件名改为 ASCII slug） |
| 根目录 `plan/*` | `docs/plans/*-historical.md` |
| `backend/docs/plans|requirements/*` | `docs/plans` / `docs/requirements` |
| `docs/directory-structure-and-archive-decision.md` | `docs/adr/0002-directory-structure-and-archive.md` |

**保留原位**：`backend/docs/openapi.yaml`。

---

## 5. 快速自检

- 新设计 → `docs/design/`
- 新执行计划 → `docs/plans/`
- 新重大决策 → `docs/adr/NNNN-*.md`（先 ADR 再动手）
- 更新能力状态 → `docs/design/feature-parity-matrix.md`
