# ADR 0002: 目录结构与归档决策

- 状态：Accepted
- 日期：2026-03（原文）；2026-07-13 迁入 `docs/adr/`（M0.6）
- 关联：`docs/README.md`、ADR 0001、G01 workbench rename

> 原路径：`docs/directory-structure-and-archive-decision.md`。  
> 本文记录 monorepo 逻辑边界与归档策略；**文档散文 IA** 见 [ADR 0001](0001-docs-information-architecture.md)。

## 逻辑归类（Phase A/B — 当前阶段）

通过 Nx project 命名与 target 编排表达边界，不立即移动物理目录。

| Nx 项目名 | 物理路径 | 逻辑归类 | 类型 | 状态 |
|-----------|---------|---------|------|------|
| `backend` | `backend/` | `apps/backend` | application | Active |
| `workbench` | `apps/workbench/` | `apps/workbench` | application | Active |
| `core` | `packages/core/` | `libs/core` | library | Active |
| `shared` | `packages/shared/` | `libs/shared` | library | Active |
| `cli` | `packages/cli/` | `apps/cli` | application | Active |
| `gui-legacy` | `packages/gui/` | `libs/gui-legacy` | library | Frozen |
| `e2e` | `scripts/` | `tools/e2e` | tooling | Active |

## 冻结策略

### Active（继续主线维护）

- `backend` — Go 后端，含 embed 嵌入式构建
- `apps/workbench` — 唯一默认主前端（React + Tauri；G01 rename；`codeflow_template` 已于 PR-3 删除）
- `packages/core` — 核心库，被 E2E 和 workbench 依赖
- `packages/shared` — 共享类型
- `packages/cli` — CLI 工具
- `scripts` — 构建/测试/E2E 脚本

### Frozen（冻结角色，不立即移动）

- `packages/gui` — 标记为 `gui-legacy`，不再作为默认产品前端或主入口
- `backend/internal/web/dist` — embed 输入路径，禁止在前期迁移
- `apps/workbench/src-tauri/binaries` — sidecar 输出/输入路径，禁止在前期迁移

### Freeze → Archive（已完成）

- `codeflow_extracted/` → `archive/frontend/codeflow_extracted/` ✅
  - 引用审计：脚本/CI/构建中零引用 ✅
  - 物理迁移完成（2026-03-12），node_modules 已清理
  - 目录未纳入 git 跟踪，不影响版本历史

## 关键路径绑定（禁止在 Phase E 之前变更）

| 绑定点 | 文件 | 当前值 |
|--------|------|--------|
| Go embed | `backend/internal/web/static.go` | `//go:embed all:dist` |
| Tauri frontendDist | `apps/workbench/src-tauri/tauri.conf.json` | `"../dist"` |
| Tauri sidecar | `apps/workbench/src-tauri/tauri.conf.json` | `"binaries/codeflow-server"` |
| Makefile frontend dir | `backend/Makefile` | `FRONTEND_DIR=../apps/workbench` |
| Makefile web dist | `backend/Makefile:8` | `WEB_DIST_DIR=./internal/web/dist` |
| E2E dev cwd | `scripts/test-all-e2e.mjs` | 默认 `CODEFLOW_DEV_CWD=apps/workbench` |

## CGO 语义差异

| Makefile target | CGO_ENABLED | 用途 |
|----------------|-------------|------|
| `build` | 1 | 开发构建，依赖 go-sqlite3 |
| `build-all` | 0 | 嵌入式单文件发布，静态链接 |

## 迁移路线图

### Phase A ✅ 基线核验
### Phase B ✅ Nx 最小壳层
### Phase C ✅ CI 双轨验证
- 保留 `.github/workflows/e2e-smoke.yml` 原有链路
- 新增 `.github/workflows/nx-ci.yml` 并行验证 Nx 编排链路
- 连续稳定后再切主入口到 Nx

### Phase D ✅ 冻结遗留目录
- `packages/gui` 已标记为 `gui-legacy`（Nx tag: `status:legacy`）
- `codeflow_extracted` 引用审计完成（2026-03-12）：
  - 脚本/CI/构建中零引用 ✅
  - 仅在文档中作为历史记录被提及 ✅
  - 目录本身未被 git 跟踪（untracked）
  - 物理迁移完成 ✅（2026-03-12）：`archive/frontend/codeflow_extracted`
  - node_modules 已清理，目录未纳入 git 跟踪

### Phase E ✅ 物理迁移
- `codeflow_extracted` → `archive/frontend/codeflow_extracted` ✅
- `packages/gui` 保持原位（仍被 tsconfig references 引用，标记为 legacy 即可）

## 回滚策略

- 每个阶段单独提交
- 根 `package.json` 保留旧脚本名（`build`、`test:e2e:all` 等），确保随时可切回
- 目录物理迁移必须单目录单提交
- `codeflow_extracted` 真正移动前，先完成"断默认引用"
