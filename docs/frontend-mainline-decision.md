# 前端主线裁决记录

## 结论

- 默认主前端：`codeflow_template`
- 默认浏览器开发入口：`codeflow_template/index.tsx`
- 默认浏览器 smoke / E2E 入口：`scripts/test-all-e2e.mjs` 启动 `codeflow_template`
- 默认 Tauri / sidecar 打包入口：`codeflow_template/src-tauri/tauri.conf.json`
- 默认嵌入式构建入口：`scripts/build-all.ps1`

## 裁决范围

| 对象 | 定位 | 决策 |
| --- | --- | --- |
| `codeflow_template` | 当前最完整的 React + Tauri 工作台 | 作为唯一默认主前端 |
| `codeflow_extracted` | 历史浏览器前端 / 迁移中遗留入口 | 停止作为默认 E2E/CI 入口，仅保留为待迁移资产 |
| `packages/gui` | 历史 GUI 组件与文档中心 | 保留为组件/包资产，不再作为平级产品入口 |

## 迁入、保留、下线清单

### 迁入主线

- 浏览器开发与 E2E 默认启动目录统一为 `codeflow_template`
- CI 前端依赖安装目录统一为 `codeflow_template`
- Tauri / sidecar 继续使用 `codeflow_template`
- Windows 嵌入式打包继续使用 `codeflow_template`

### 保留

- `codeflow_extracted` 作为遗留前端资产保留，后续按专题 issue 逐步迁移或下线
- `packages/gui` 作为组件/包保留，后续仅以被主前端消费或沉淀资产为目标

### 下线/冻结

- 不再允许把 `codeflow_extracted` 作为默认浏览器 smoke、E2E 或 CI 前端入口
- 不再把 `packages/gui` 描述为默认产品前端

## 测试 / 构建 / 发布基线补充

- 浏览器 E2E 默认使用 `codeflow_template` + `npm` 启动前端，`scripts/test-all-e2e.mjs` 会在执行 smoke 前自动确保 `packages/core/dist` 已构建，避免 CLI hook 脚本因缺少 dist 伪失败。
- CI 继续使用根目录 `pnpm install --frozen-lockfile` 管理 monorepo 依赖，同时在 `codeflow_template` 内执行 `npm ci`，并显式构建 `@codeflow/core` dist 后再跑 E2E。
- 嵌入式构建脚本会把 `codeflow_template/dist` 同步到 `backend/internal/web/dist`，与 `backend/internal/web/embed.go` 的嵌入路径保持一致。
- 默认 Go 打包链采用 `CGO_ENABLED=1`，与 `backend/README.md` 中 `go-sqlite3` 需要 CGO 的要求保持一致；Tauri/sidecar 继续沿用同一 CGO 决策。

## 当前证据

- `codeflow_template/index.tsx:11`
- `scripts/test-all-e2e.mjs:10`
- `scripts/_shared/runtime.mjs:15`
- `.github/workflows/e2e-smoke.yml:34`
- `scripts/build-all.ps1:20`
- `scripts/build-all.sh:19`
- `backend/internal/web/embed.go:6`
- `backend/README.md:30`
- `codeflow_template/src-tauri/tauri.conf.json:7`
