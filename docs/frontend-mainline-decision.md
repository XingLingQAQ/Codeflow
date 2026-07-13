# 前端主线裁决记录

> 状态：Active  
> 最近修订：2026-07-13  
> 关联：`docs/design/2026-06-12-overall-roadmap.md`、`docs/plans/2026-07-11-codeflow-2.0-implementation-and-hardening-plan.md`、`docs/README.md`

---

## 结论（2026-07-12 现行）

- **默认主前端**：`apps/workbench`（包名 `@codeflow/workbench`，Nx project：`workbench`）
- **默认浏览器开发入口**：`apps/workbench/index.tsx`（根脚本：`pnpm dev:workbench`；兼容别名 `pnpm dev:desktop`）
- **默认浏览器 smoke / E2E 入口**：`scripts/test-all-e2e.mjs`，`CODEFLOW_DEV_CWD=apps/workbench`
- **默认 Tauri / sidecar 打包入口**：`apps/workbench/src-tauri/tauri.conf.json`（根脚本：`pnpm tauri:dev` / `pnpm tauri:build`）
- **默认嵌入式构建入口**：`scripts/build-all.ps1` / 等价 shell，前端产物同步至 `backend/internal/web/dist`
- **嵌入实现文件**：`backend/internal/web/static.go`（`//go:embed all:dist`）——**不是**历史文档中的 `embed.go`
- **目录更名（G01）**：`apps/desktop` → `apps/workbench` / `@codeflow/desktop` → `@codeflow/workbench` **已完成（2026-07-12）**
- **废弃目录**：`codeflow_template` —— **已删除（PR-3，2026-07-12）**；仓库仅保留 `apps/workbench` 作为产品前端
- **兼容别名**：根脚本 `dev:desktop` / `build:desktop` 仍可用，均转发至 `@codeflow/workbench`（便于旧文档/习惯）
- **平台标签**：Nx tag `platform:desktop` 表示桌面平台，**不是**目录名，勿与 `apps/desktop` 混淆

### 目标态路由（产品 IA，非当前实现）

`/`、`/projects`、`/workbench/:projectId/:stage`、`/agents`、`/flows`、`/plugins`、`/config/:section`、`/settings`

当前实现仍为 `ViewMode` 控制台（HOME/PROJECTS/SESSIONS/…），由 M1 App Shell 迁移。

---

## 2026-07-12 修订说明（G01 rename）

### 为何提前更名

1. **PR-2+PR-3 已消解双真相**：工具链、Nx、CI、E2E 仅绑定单一前端树后，目录名仍为过渡名 `desktop`，与路线图目标 `workbench` 及产品 IA 不一致。
2. **G01 关闭条件**：物理双树删除后，唯一剩余项为 rename；提前完成可避免后续 M1 文档与路径再次大面积改写。
3. **风险可控**：仅目录/包名/路径绑定，不改 Tauri productName、identifier、`codeflow-server` sidecar、Cargo 包名；不做 `src/` 重组（属 M1）。

### 更名范围

| 对象 | 变更 |
| --- | --- |
| 物理目录 | `apps/desktop` → `apps/workbench` |
| package name | `@codeflow/desktop` → `@codeflow/workbench` |
| Nx project | `desktop` → `workbench`（`sourceRoot` / target `cwd` → `apps/workbench`） |
| 根脚本 | 新增 `dev:workbench` / `build:workbench`；保留 `dev:desktop` / `build:desktop` 兼容别名 |
| Makefile / build / tauri / E2E / CI / OpenAPI 输出 | 全部指向 `apps/workbench` / `@codeflow/workbench` |
| Go fixture 路径字符串 | `apps/desktop` → `apps/workbench` |

### 非目标（本 PR 不做）

- 不重命名 Tauri `productName` / identifier / sidecar `codeflow-server`
- 不重组为 `apps/workbench/src/{shell,stages,...}`（M1）
- 不启动 PR-4 Snapshot 真 restore（可并行后续）

---

## 2026-07-11 修订说明（改判 desktop 主线）

### 为何改判

1. **双真相（PR-2+PR-3 已消解）**：根 monorepo 脚本、Makefile、build/tauri/E2E/CI/Nx 默认入口曾分裂；PR-2 改绑至 monorepo 前端，PR-3 删除 `codeflow_template`。
2. **同构双份**：两侧 `App.tsx` 体积一致（约 145688 bytes），长期双维护无产品收益。
3. **路线图冲突**：`docs/design/2026-06-12-overall-roadmap.md` M0 明确要求删除 `codeflow_template` 并改绑路径，最终目录为 `apps/workbench`。
4. **文档过期**：旧裁决引用 `backend/internal/web/embed.go`，仓库实际为 `static.go`。

### 改判原则

- **单一可构建、可测试、可嵌入的前端真相源**
- 新功能只进主线；禁止「只改 template 或只改某一份前端」
- 已执行顺序：重绑工具链与 CI → 删除 template → rename → workbench

---

## 裁决范围

| 对象 | 定位 | 决策 |
| --- | --- | --- |
| `apps/workbench` | 现行默认 React + Tauri 工作台 | **唯一默认主前端**；所有工具链/CI/E2E/embed 指向此处 |
| `apps/desktop` | 历史过渡目录名 | **已更名**为 `apps/workbench`（G01，2026-07-12）；物理路径不再存在 |
| `codeflow_template` | 与 desktop 近乎完全重复的历史主线 | **已删除（PR-3）**：不再存在物理目录；回滚见远程 `origin/master@44fa821` 历史 |
| `codeflow_extracted` | 历史浏览器前端 / 迁移遗留 | 不作为默认 E2E/CI 入口；按需迁移或下线 |
| `packages/gui` | 历史 GUI 组件与文档中心 | 组件/包资产；非产品入口 |
| `packages/ui-components` / `packages/shared` / `packages/core` | monorepo 共享包 | 继续被主前端消费 |

---

## 迁入、保留、下线清单

### 迁入主线（已完成 / 残留）

- [x] `backend/Makefile`：`FRONTEND_DIR=../apps/workbench`
- [x] `scripts/build-all.ps1`（及 `.sh`）：前端目录 → `apps/workbench`
- [x] `scripts/build-tauri.ps1` / `scripts/dev-tauri.ps1` → `apps/workbench`
- [x] `scripts/test-all-e2e.mjs` 默认 `CODEFLOW_DEV_CWD` → `apps/workbench`
- [x] `.github/workflows/e2e-smoke.yml`、`nx-ci.yml` → `apps/workbench` / `@codeflow/workbench`
- [x] 文档与主线裁决中 template「默认入口」描述已清零/改绑（PR-2）；**`codeflow_template` 物理目录已删除（PR-3）**
- [x] **G01 rename**：`apps/desktop` → `apps/workbench`，`@codeflow/workbench`，Nx `workbench`
- [x] **M0.3 嵌入同步 smoke**：`apps/workbench/dist` → `backend/internal/web/dist`，与 `static.go` 一致；`pnpm smoke:embed` / `scripts/smoke-embed.mjs`；`Makefile` `build-frontend` 同步 dist

### 已与 monorepo 一致（保持）

- 根 `package.json`：`dev:workbench` / `build:workbench` / `tauri:dev` / `tauri:build` → `@codeflow/workbench`
- 兼容别名：`dev:desktop` / `build:desktop` → `@codeflow/workbench`
- `scripts/generate-openapi-types.mjs` 输出 → `apps/workbench/generated/openapi-types.ts`

### 保留

- `packages/gui`、`packages/ui-components` 等作为被消费资产
- 历史裁决正文（见文末「历史结论归档」）供审计追溯
- 兼容脚本别名 `*:desktop`（转发 workbench）

### 下线 / 冻结

- 禁止将 `codeflow_template` 或 `codeflow_extracted` 作为默认浏览器 smoke、E2E、CI、embed、Tauri 入口
- 禁止再把 `packages/gui` 描述为默认产品前端
- 禁止再引入第二产品前端树
- [x] M0 验收后删除 `codeflow_template` 目录（PR-3 完成）
- [x] 目录更名 `apps/desktop` → `apps/workbench`（G01 完成）

---

## 测试 / 构建 / 发布基线

- **浏览器 E2E**：`apps/workbench`；`scripts/test-all-e2e.mjs` 在 smoke 前确保 `packages/core/dist` 已构建
- **CI 依赖**：根目录 `pnpm install --frozen-lockfile`；前端以 filter `@codeflow/workbench`（或等价 working-directory）构建；显式构建 `@codeflow/core` dist 后再跑 E2E
- **嵌入式构建**：前端 dist 同步到 `backend/internal/web/dist`，由 `backend/internal/web/static.go` embed
- **CGO**：默认 Go 打包链 `CGO_ENABLED=1`（`go-sqlite3`）；是否迁移 modernc 见独立 ADR（2.0 计划 M0.9）
- **Windows Tauri**：`scripts/build-tauri.ps1` 在 `tauri:build` 前探测并导入 VS MSVC 环境，避免 `vswhom-sys` / `LNK1143`

### 目标本地命令

```text
pnpm dev:workbench
pnpm build:workbench
# 兼容别名（等同 workbench）
pnpm dev:desktop
pnpm build:desktop
pnpm smoke:embed
pnpm smoke:embed:build   # optional rebuild + sync
pnpm tauri:dev
pnpm tauri:build
# E2E 默认 cwd = apps/workbench
```

---

## 当前证据（2026-07-12）

### 已指向 workbench（G01）

- 根 `package.json`：`dev:workbench` / `build:workbench` / `tauri:*` → `@codeflow/workbench`
- 兼容别名 `dev:desktop` / `build:desktop` → `@codeflow/workbench`
- `scripts/generate-openapi-types.mjs` → `apps/workbench/generated/openapi-types.ts`
- `apps/workbench/package.json` name = `@codeflow/workbench`
- `apps/workbench/project.json` name = `workbench`，`sourceRoot` / `cwd` = `apps/workbench`
- `apps/workbench/src-tauri/tauri.conf.json` 存在
- `backend/internal/web/static.go`：`//go:embed all:dist`
- 物理路径：`apps/desktop` **不存在**；`apps/workbench` **存在**

### 已改绑（PR-2，2026-07-11，路径现为 workbench）

- `backend/Makefile`：`FRONTEND_DIR=../apps/workbench`
- `scripts/build-all.ps1` / `build-all.sh`、`build-tauri.ps1`、`dev-tauri.ps1` → `apps/workbench`
- `scripts/test-all-e2e.mjs` 默认 `CODEFLOW_DEV_CWD=apps/workbench`、`CODEFLOW_DEV_PM=pnpm`
- `.github/workflows/e2e-smoke.yml`、`nx-ci.yml` → workbench + pnpm

### 已完成（PR-3，2026-07-12）

- 物理目录 `codeflow_template` **已删除**
- Go 单测 fixture 路径字符串已中性化（后随 G01 更新为 `apps/workbench`）
- 回滚 template：远程 `origin/master` 提交 `44fa821`

### 已完成（M0.3，2026-07-12）

- `backend/Makefile` `build-frontend`：构建 `@codeflow/workbench` 后同步 `apps/workbench/dist` → `backend/internal/web/dist`，并校验 `index.html`
- `build-all` 在 Go 编译前校验 embed dist 存在
- Node smoke：`scripts/smoke-embed.mjs`（`pnpm smoke:embed` / `smoke:embed:build`）——**不依赖本机 Go**
- PowerShell smoke：`scripts/smoke-embed.ps1`（可选 `go build ./internal/web`）
- 嵌入入口仍为 `backend/internal/web/static.go`（`//go:embed all:dist`）；`backend/internal/web/dist` 为生成物（gitignore）

### 已完成（M0.5，2026-07-13）

- 强化 `.gitignore`：`node_modules/`、`apps/**/dist/`、`backend/internal/web/dist/`、`coverage/`（packages core/gui/shared/ui-components dist 仍可按需跟踪）
- 取消跟踪历史误提交的 `node_modules`（index 清零，工作区依赖保留）
- 新增 `scripts/check-repo-hygiene.mjs` + 根脚本 `pnpm check:repo-hygiene`
- CI：`.github/workflows/nx-ci.yml` checkout 后执行 `Repo Hygiene Guard`
- 卫兵规则：禁止 tracked `node_modules` / `apps/*/dist` / `backend/internal/web/dist` / `codeflow_template`

### 已完成（M0.6，2026-07-13）

- 建立 `docs/README.md` 文档 IA（design / plans / adr / requirements）
- ADR：`docs/adr/0001-docs-information-architecture.md`、`0002-directory-structure-and-archive.md`
- 2.0 设计五件套纳入 git：overall-roadmap / flow-engine / workbench-and-shell / agent-quality-system / frontend-experience
- `archive/docs/early-design` → `docs/design/early`；根 `plan/` → `docs/plans/*-historical`；`backend/docs` 散文 → `docs/plans|requirements`（保留 `openapi.yaml`）
- G02 文档 SSOT → ✅

### 残留（M0 后续）

- M0.8：summarize 单包方案
- M0.9：CGO/SQLite ADR（`docs/adr/0003-sqlite-cgo.md`）
- M1：`src/` 目录重组（shell/workbench/stages/ui 等）

---

## 与 2.0 里程碑的衔接

| 里程碑 | 前端主线动作 |
| --- | --- |
| M0 | 修订本裁决；路径重绑；删除 template；G01 rename → workbench；M0.3 embed smoke；M0.5 hygiene；M0.6 docs IA；建立 feature-parity-matrix |
| M1 | `apps/workbench/src/` 重组（shell/workbench/stages/ui）；App Shell + Flow Rail |
| M2+ | 阶段画布与 floweng 对接；禁止再引入第二产品前端树 |

详细任务与验收见：

- `docs/plans/2026-07-11-codeflow-2.0-implementation-and-hardening-plan.md`
- `docs/design/feature-parity-matrix.md`

---

## 历史结论归档（已作废，仅审计）

以下内容**不再作为工程默认行为**：

- 默认主前端：`codeflow_template`（PR-3 前）
- 默认主前端：`apps/desktop` / `@codeflow/desktop`（G01 前过渡名）
- 默认浏览器 / E2E / Tauri / embed 入口均基于 `codeflow_template`
- 嵌入路径曾误写为 `backend/internal/web/embed.go`
- 「M1 末 / M2 初再 rename workbench」时间表（G01 已于 2026-07-12 完成更名）

---

## 修订记录

| 日期 | 变更 |
| --- | --- |
| （历史） | 初版：裁定 `codeflow_template` 为默认主前端 |
| 2026-07-11 | 改判：`apps/desktop` 为唯一默认主前端；废弃 template；embed 更正为 `static.go`；衔接 workbench 更名与 2.0 计划 |
| 2026-07-11 | **PR-2**：Makefile/scripts/CI/Nx 默认入口改绑 `apps/desktop`；证据段更新为「已改绑」；物理 template 删除留给 PR-3；embed 清单项仍待 smoke |
| 2026-07-11 | **PR-2 收尾**：本地 `pnpm --filter @codeflow/desktop build` 通过；pnpm 11 `allowBuilds`；失效 `onlyBuiltDependencies` 移除；embed 仍待 M0.3 |
| 2026-07-12 | **PR-3**：删除 `codeflow_template` 物理双树；Go fixture 改 `apps/desktop`；残留段改为已删除；回滚依赖远程 `44fa821` 历史（无本地 pre-delete tag） |
| 2026-07-12 | **G01**：`apps/desktop` → `apps/workbench`，`@codeflow/workbench`，Nx `workbench`；兼容 `dev:desktop`/`build:desktop` 别名；本文件结论与证据全面同步；残留仅 M0.3 embed |
| 2026-07-12 | **M0.3**：Makefile `build-frontend` 同步 embed dist；新增 `scripts/smoke-embed.mjs` / `.ps1` 与 `pnpm smoke:embed`；Node 路径 smoke 通过（本机无 Go 时跳过 compile smoke）；清单勾选 M0.3；残留改为 M0.8/M0.9/M1/PR-4 |
| 2026-07-13 | **M0.5**：gitignore + untrack node_modules + `check-repo-hygiene` + CI Repo Hygiene Guard；残留去掉已完成的 PR-4，保留 M0.6/M0.8/M0.9/M1 |
