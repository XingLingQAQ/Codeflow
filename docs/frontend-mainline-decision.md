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

## 当前证据

- `codeflow_template/index.tsx:11`
- `scripts/test-all-e2e.mjs:10`
- `.github/workflows/e2e-smoke.yml:34`
- `scripts/build-all.ps1:20`
- `codeflow_template/src-tauri/tauri.conf.json:7`
