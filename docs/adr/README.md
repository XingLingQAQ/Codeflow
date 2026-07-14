# ADR 索引（Architecture Decision Records）

> 状态：Active  
> 建立：2026-07-13（M0.6）

## 约定

1. 文件名：`NNNN-<kebab-slug>.md`（四位序号，不复用）。
2. 每个 ADR 文首至少包含：状态、日期、决策、后果。
3. 状态机：`Proposed` → `Accepted` | `Rejected` | `Superseded`。
4. 重大变更（主线路径、SQLite/CGO、Flow 事件契约等）**先 ADR 再改代码**。
5. 详细设计展开写在 `docs/design/`，ADR 只记录取舍与边界。

## 模板

```markdown
# ADR NNNN: 标题

- 状态：Proposed | Accepted | Rejected | Superseded
- 日期：YYYY-MM-DD
- 关联：相关 design / plan / issue

## 背景

## 决策

## 备选方案

## 后果
```

## 清单

| ID | 标题 | 状态 | 日期 |
|---|---|---|---|
| [0001](0001-docs-information-architecture.md) | 文档信息架构（design/plans/adr） | Accepted | 2026-07-13 |
| [0002](0002-directory-structure-and-archive.md) | 目录结构与归档（Nx 逻辑边界） | Accepted | 2026-03（迁入 2026-07-13） |
| [0003](0003-sqlite-cgo.md) | CGO / SQLite 基线（go-sqlite3 + CGO=1） | Accepted | 2026-07-15 |
