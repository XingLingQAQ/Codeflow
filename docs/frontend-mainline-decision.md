# 前端主线裁决记录

> 状态：Active  
> 最近修订：2026-07-12  
> 关联：`docs/design/2026-06-12-overall-roadmap.md`、`docs/plans/2026-07-11-codeflow-2.0-implementation-and-hardening-plan.md`

---

## 结论（2026-07-11 现行）

- **默认主前端**：`apps/desktop`（包名 `@codeflow/desktop`）
- **默认浏览器开发入口**：`apps/desktop/index.tsx`（根脚本：`pnpm dev:desktop`）
- **默认浏览器 smoke / E2E 入口**：`scripts/test-all-e2e.mjs`，`CODEFLOW_DEV_CWD=apps/desktop`
- **默认 Tauri / sidecar 打包入口**：`apps/desktop/src-tauri/tauri.conf.json`（根脚本：`pnpm tauri:dev` / `pnpm tauri:build`）
- **默认嵌入式构建入口**：`scripts/build-all.ps1` / 等价 shell，前端产物同步至 `backend/internal/web/dist`
- **嵌入实现文件**：`backend/internal/web/static.go`（`//go:embed all:dist`）——**不是**历史文档中的 `embed.go`
- **过渡更名**：M1 末 / M2 初将 `apps/desktop` 重命名为 `apps/workbench`（`@codeflow/workbench`），见 2.0 实施计划 §6
- **废弃目录**：`codeflow_template` —— **已删除（PR-3，2026-07-12）**；仓库仅保留 `apps/desktop` 作为产品前端

### 目标态路由（产品 IA，非当前实现）

`/`、`/projects`、`/workbench/:projectId/:stage`、`/agents`、`/flows`、`/plugins`、`/config/:section`、`/settings`

当前实现仍为 `ViewMode` 控制台（HOME/PROJECTS/SESSIONS/…），由 M1 App Shell 迁移。

---

## 2026-07-11 修订说明

### 为何改判

1. **双真相（PR-2+PR-3 已消解）**：根 monorepo 脚本、Makefile、build/tauri/E2E/CI/Nx 默认入口均指向 `apps/desktop`；物理目录 `codeflow_template` 已删除。
2. **同构双份**：两侧 `App.tsx` 体积一致（约 145688 bytes），长期双维护无产品收益。
3. **路线图冲突**：`docs/design/2026-06-12-overall-roadmap.md` M0 明确要求删除 `codeflow_template` 并改绑路径，最终目录为 `apps/workbench`。
4. **文档过期**：旧裁决引用 `backend/internal/web/embed.go`，仓库实际为 `static.go`。

### 改判原则

- **单一可构建、可测试、可嵌入的前端真相源**
- 新功能只进主线；禁止「只改 template 或只改 desktop」
- 先重绑工具链与 CI，再删除 template，最后再 rename → workbench（降低一次性大爆炸风险）

---

## 裁决范围

| 对象 | 定位 | 决策 |
| --- | --- | --- |
| `apps/desktop` | 现行默认 React + Tauri 工作台（过渡名） | **唯一默认主前端**；M0 起所有工具链/CI/E2E/embed 指向此处 |
| `apps/workbench` | 目标目录名（原 desktop 更名） | M1 末 / M2 初更名；更名后本文件同步更新路径 |
| `codeflow_template` | 与 desktop 近乎完全重复的历史主线 | **已删除（PR-3）**：不再存在物理目录；回滚见远程 `origin/master@44fa821` 历史 |
| `codeflow_extracted` | 历史浏览器前端 / 迁移遗留 | 不作为默认 E2E/CI 入口；按需迁移或下线 |
| `packages/gui` | 历史 GUI 组件与文档中心 | 组件/包资产；非产品入口 |
| `packages/ui-components` / `packages/shared` / `packages/core` | monorepo 共享包 | 继续被主前端消费 |

---

## 迁入、保留、下线清单

### 迁入主线（M0 必须完成）

- [x] `backend/Makefile`：`FRONTEND_DIR=../apps/desktop`
- [x] `scripts/build-all.ps1`（及 `.sh` 若存在）：前端目录 → `apps/desktop`
- [x] `scripts/build-tauri.ps1` / `scripts/dev-tauri.ps1` → `apps/desktop`
- [x] `scripts/test-all-e2e.mjs` 默认 `CODEFLOW_DEV_CWD` → `apps/desktop`
- [x] `.github/workflows/e2e-smoke.yml`、`nx-ci.yml` working-directory → `apps/desktop`
- [x] 文档与主线裁决中 template「默认入口」描述已清零/改绑（PR-2）；**`codeflow_template` 物理目录已删除（PR-3）**
- [ ] 嵌入同步：`apps/desktop/dist` → `backend/internal/web/dist`，与 `static.go` 一致

### 已与 monorepo 一致（保持）

- 根 `package.json`：`dev:desktop` / `build:desktop` / `tauri:dev` / `tauri:build` → `@codeflow/desktop`
- `scripts/generate-openapi-types.mjs` 输出 → `apps/desktop/generated/openapi-types.ts`

### 保留

- `packages/gui`、`packages/ui-components` 等作为被消费资产
- 历史裁决正文（见文末「历史结论归档」）供审计追溯

### 下线 / 冻结

- 禁止将 `codeflow_template` 或 `codeflow_extracted` 作为默认浏览器 smoke、E2E、CI、embed、Tauri 入口
- 禁止再把 `packages/gui` 描述为默认产品前端
- [x] M0 验收后删除 `codeflow_template` 目录（PR-3 完成）

---

## 测试 / 构建 / 发布基线

- **浏览器 E2E**：`apps/desktop`；`scripts/test-all-e2e.mjs` 在 smoke 前确保 `packages/core/dist` 已构建
- **CI 依赖**：根目录 `pnpm install --frozen-lockfile`；前端以 filter `@codeflow/desktop`（或等价 working-directory）构建；显式构建 `@codeflow/core` dist 后再跑 E2E
- **嵌入式构建**：前端 dist 同步到 `backend/internal/web/dist`，由 `backend/internal/web/static.go` embed
- **CGO**：默认 Go 打包链 `CGO_ENABLED=1`（`go-sqlite3`）；是否迁移 modernc 见独立 ADR（2.0 计划 M0.9）
- **Windows Tauri**：`scripts/build-tauri.ps1` 在 `tauri:build` 前探测并导入 VS MSVC 环境，避免 `vswhom-sys` / `LNK1143`

### 目标本地命令

```text
pnpm dev:desktop
pnpm build:desktop
pnpm tauri:dev
pnpm tauri:build
# E2E 默认 cwd = apps/desktop
```

---

## 当前证据（2026-07-11 审计）

### 已指向 desktop（保持）

- 根 `package.json`：`dev:desktop` / `tauri:*` → `@codeflow/desktop`
- `scripts/generate-openapi-types.mjs` → `apps/desktop/generated/openapi-types.ts`
- `apps/desktop/src-tauri/tauri.conf.json` 存在
- `backend/internal/web/static.go`：`//go:embed all:dist`

### 已改绑（PR-2，2026-07-11）

- `backend/Makefile`：`FRONTEND_DIR=../apps/desktop`（优先 monorepo `pnpm --filter @codeflow/desktop build`）
- `scripts/build-all.ps1` / `build-all.sh`、`build-tauri.ps1`、`dev-tauri.ps1` → `apps/desktop`
- `scripts/test-all-e2e.mjs` 默认 `CODEFLOW_DEV_CWD=apps/desktop`、`CODEFLOW_DEV_PM=pnpm`
- `.github/workflows/e2e-smoke.yml`、`nx-ci.yml` → desktop + pnpm
- `apps/desktop/project.json`：`sourceRoot` / target `cwd` = `apps/desktop`；命令 `pnpm`

### 已完成（PR-3，2026-07-12）

- 物理目录 `codeflow_template` **已删除**（约 99 个跟踪文件）
- Go 单测 fixture 路径字符串已中性化为 `apps/desktop`（`service_test.go` / `context_test.go`）
- 删除前门禁：两侧 `App.tsx` SHA256 一致；template 无独有业务源码（仅 `package-lock.json` 等配置差异）
- 回滚：未打本地 tag `pre-delete-codeflow_template`（当时工作区无完整 git 历史）；以远程 `origin/master` 提交 `44fa821` 恢复 template 树

### 残留（至 M0.3+）

- 本地 desktop 生产构建已通过（`pnpm --filter @codeflow/desktop build` → `apps/desktop/dist`）
- embed 同步 smoke（`apps/desktop/dist` → `backend/internal/web/dist`，`static.go`）仍待 M0.3
- 目录更名 `apps/desktop` → `apps/workbench` 仍待 M1 末 / M2 初

---

## 与 2.0 里程碑的衔接

| 里程碑 | 前端主线动作 |
| --- | --- |
| M0 | 修订本裁决；路径重绑；删除 template；建立 feature-parity-matrix |
| M1 | `apps/desktop/src/` 重组（shell/workbench/stages/ui）；App Shell + Flow Rail |
| M1 末 / M2 初 | rename → `apps/workbench`；更新本文件与所有路径 |
| M2+ | 阶段画布与 floweng 对接；禁止再引入第二产品前端树 |

详细任务与验收见：

- `docs/plans/2026-07-11-codeflow-2.0-implementation-and-hardening-plan.md`
- `docs/design/feature-parity-matrix.md`

---

## 历史结论归档（2026-07-11 之前，已作废）

以下内容仅作审计追溯，**不再作为工程默认行为**：

- 默认主前端：`codeflow_template`
- 默认浏览器 / E2E / Tauri / embed 入口均基于 `codeflow_template`
- 嵌入路径曾误写为 `backend/internal/web/embed.go`
- `codeflow_template` 曾被描述为「当前最完整的 React + Tauri 工作台 / 唯一默认主前端」

---

## 修订记录

| 日期 | 变更 |
| --- | --- |
| （历史） | 初版：裁定 `codeflow_template` 为默认主前端 |
| 2026-07-11 | 改判：`apps/desktop` 为唯一默认主前端；废弃 template；embed 更正为 `static.go`；衔接 workbench 更名与 2.0 计划 |
| 2026-07-11 | **PR-2**：Makefile/scripts/CI/Nx 默认入口改绑 `apps/desktop`；证据段更新为「已改绑」；物理 template 删除留给 PR-3；embed 清单项仍待 smoke |
| 2026-07-11 | **PR-2 收尾**：本地 `pnpm --filter @codeflow/desktop build` 通过；pnpm 11 `allowBuilds`；失效 `onlyBuiltDependencies` 移除；embed 仍待 M0.3 |
| 2026-07-12 | **PR-3**：删除 `codeflow_template` 物理双树；Go fixture 改 `apps/desktop`；残留段改为已删除；回滚依赖远程 `44fa821` 历史（无本地 pre-delete tag） |
