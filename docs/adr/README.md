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
| [0006](0006-project-workspace-flow-session-binding.md) | Project / Workspace / Flow / Session binding and recoverable lifecycle | Accepted | 2026-08-19 |
| [0007](0007-unified-execution-policy-and-aead-migration.md) | Unified execution policy, audited denials, and AES-GCM migration | Accepted | 2026-08-19 |
| [0008](0008-durable-runtime-state-and-audit-retention.md) | Durable runtime state, explicit injection, and anchored audit retention | Accepted | 2026-08-19 |
| [0001](0001-docs-information-architecture.md) | 文档信息架构（design/plans/adr） | Accepted | 2026-07-13 |
| [0002](0002-directory-structure-and-archive.md) | 目录结构与归档（Nx 逻辑边界） | Accepted | 2026-03（迁入 2026-07-13） |
| [0003](0003-sqlite-cgo.md) | CGO / SQLite 基线（go-sqlite3 + CGO=1） | Accepted | 2026-07-15 |
| [0004](0004-sidecar-trust-boundary.md) | Sidecar 信任边界（loopback、启动令牌、Origin、WS scope） | Accepted | 2026-08-18 |
| [0005](0005-api-credential-secret-store.md) | API 凭据与配置分离存储（DPAPI / AES-GCM、迁移、公开 DTO） | Accepted | 2026-08-19 |
| [0009](0009-sqlite-driver-modernc-migration.md) | SQLite 驱动迁移至 modernc.org/sqlite（CGO=0 基线；决策已定，实施在途 T0.01.b/T0.01.c） | Accepted | 2026-09-10 |
