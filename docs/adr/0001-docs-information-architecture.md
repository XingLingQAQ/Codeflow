# ADR 0001: 文档信息架构（design / plans / adr）

- 状态：Accepted
- 日期：2026-07-13
- 关联：M0.6；`docs/README.md`；G02 文档 SSOT

## 背景

仓库内散文文档曾分散在：

- 根目录 `plan/`
- `archive/docs/early-design/`
- `backend/docs/plans` / `backend/docs/requirements`
- `docs/` 根散落决策与参考
- 多份 untracked 2.0 设计 dump

导致「哪里找真相」成本高，且与 2.0 计划中的 docs 三分法不一致。

## 决策

采用并强制以下 IA：

| 树 | 职责 |
|---|---|
| `docs/design/` | 设计正文与 Living 矩阵 |
| `docs/plans/` | 实施计划与进度 |
| `docs/adr/` | 架构决策记录 |
| `docs/requirements/` | 需求范围 |
| `docs/README.md` | 索引与约定 |

并约定：

1. 不再向根 `plan/`、`archive/docs/`、`backend/docs/plans|requirements` 新增散文。
2. `backend/docs/openapi.yaml` 保留为 API 契约 SSOT（非散文）。
3. early-design 迁入 `docs/design/early/`，状态 Historical，中文文件名改为 ASCII slug 以便跨平台工具链。
4. 每里程碑同步 living 计划与 feature-parity-matrix。

## 备选方案

1. **维持分散 + 仅加索引**：迁移成本低，但双写/过期风险不变。
2. **全部塞进 monorepo wiki / 外部 Notion**：离开仓库，评审与 PR 无法同 diff。
3. **本决策（仓库内三分法）**：与路线图 M0 清理清单一致，可 git 审计。

## 后果

- 正面：单一入口；新 Agent 可按 `docs/README.md` 导航；G02 可勾选为 ✅（IA 层）。
- 负面：历史路径失效，需在 DIRECTORY_STRUCTURE / 计划 changelog 中注明迁移。
- 后续：M0.9 输出 `0003-sqlite-cgo.md`；前端主线裁决暂留 `docs/` 根（高频入口），不强制迁 ADR（其内容已是裁决记录）。
