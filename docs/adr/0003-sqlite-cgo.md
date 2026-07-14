# ADR 0003: CGO / SQLite 基线

- 状态：Accepted
- 日期：2026-07-15
- 关联：G30；M0.9；`docs/adr/0002-directory-structure-and-archive.md`（CGO 语义差异表）；`backend/go.mod`（`github.com/mattn/go-sqlite3`）

## 背景

CodeFlow 后端持久化广泛使用 SQLite：

| 域 | 证据（driver 导入 / `sql.Open("sqlite3", …)`） |
|---|---|
| storage / sessions | `backend/internal/storage/storage.go` |
| config / PAPI | `backend/internal/config/service.go` |
| context presets | `backend/internal/context/service.go` |
| project / planner | `backend/internal/project/service.go`、`planner/service.go` |
| memory / atomic / archive | `backend/internal/memory/*` |
| SAMG graph | `backend/internal/samg/sqlite_store.go` |

依赖为 **`github.com/mattn/go-sqlite3`**（`backend/go.mod`），该驱动 **需要 CGO**。

构建脚本现状（审计时点）：

| 入口 | CGO | 说明 |
|---|---|---|
| `backend/Makefile` `build` | `CGO_ENABLED=1` | 正确，匹配 go-sqlite3 |
| `backend/Makefile` `build-all` | **`CGO_ENABLED=0`** | **与 go-sqlite3 冲突**；在无 C 工具链环境会得到 stub 驱动 |
| `scripts/build-all.ps1` / `build-all.sh` | `CGO_ENABLED=1` | 正确 |
| `scripts/build-tauri.ps1` / `dev-tauri.ps1` | `CGO_ENABLED=1` | 正确（sidecar 需可用 SQLite） |

本机默认 `go test` 在 `CGO_ENABLED=0` 时，凡打开 SQLite 的测试会失败（`Binary was compiled with 'CGO_ENABLED=0', go-sqlite3 requires cgo`）——已在 handlers/e2e 路径复现，属环境/基线问题，非业务回归。

路线图曾提议评估 `modernc.org/sqlite` 以实现 `CGO_ENABLED=0` 纯 Go 静态链接。M0.9 需要先**拍板基线**，避免并行两套 driver 语义。

## 决策

### 1. 现行基线（Accepted，立即生效）

1. **默认 SQLite 驱动**：继续使用 **`github.com/mattn/go-sqlite3`**。  
2. **默认构建 / 测试 / Tauri sidecar**：**`CGO_ENABLED=1`**。  
3. **禁止**在同一二进制中混用 `modernc.org/sqlite` 与 `go-sqlite3` 双 driver 作为并行默认。  
4. **driver name** 保持 `sql.Open("sqlite3", …)` 契约，直至有独立迁移 PR 统一替换。  
5. **文档与脚本真相**：任何「可静态纯 Go 发布」的表述必须显式标注 *未来迁移*，不得描述为当前默认。

### 2. Makefile 一致性（M0.9 随 ADR 修正）

- `make build-all` 与 `make build` 对齐为 **`CGO_ENABLED=1`**。  
- 理由：`build-all` 产出的 embed 单文件仍依赖 SQLite；`CGO_ENABLED=0` 在当前依赖下不可用。  
- PowerShell/shell 构建脚本已为 1，Makefile 不得再成为反例。

### 3. modernc 迁移（后置，非默认）

评估 / 迁移 `modernc.org/sqlite` **不在 M0 阻塞路径**。若未来启动，须单独 PR + 新 ADR（或本 ADR Superseded），最低验收：

1. 全量替换 blank import 与 `sql.Open` driver name（modernc 通常为 `"sqlite"`）。  
2. 覆盖 storage/config/context/project/planner/memory/samg 及全部相关测试。  
3. CI 矩阵：`CGO_ENABLED=0` 主路径 + 可选 CGO 对照。  
4. WAL / 并发 / 性能 smoke（记忆与 SAMG 写路径）。  
5. Tauri sidecar 与 embed 发布链路各打一包验证。

**暂不迁移的原因（当前）**：

- 触达面广（10+ 包、大量测试与 connStr 参数风格 `_journal_mode=WAL` 等）。  
- go-sqlite3 在桌面/sidecar 场景成熟；Windows 上 CGO 工具链（MSVC）已在 Tauri 构建脚本中处理。  
- M0 目标是统一主线与文档，不是换存储驱动。

## 备选方案

| 方案 | 结论 |
|---|---|
| A. 维持 go-sqlite3 + CGO=1（本决策） | **采纳**：与代码、Tauri 脚本一致；修正 Makefile 例外 |
| B. 立即迁移 modernc + CGO=0 | **拒绝（此刻）**：范围大、风险高，阻塞 M1 无收益 |
| C. 双 driver 运行时切换 | **拒绝**：配置与测试矩阵翻倍，易出「本机绿 CI 红」 |
| D. 去掉 SQLite 换其他嵌入库 | **非目标**：与现有 schema/记忆/图谱资产冲突 |

## 后果

### 正面

- G30 决策落盘；构建/测试期望明确：需要 C 工具链（gcc/MSVC）。  
- 消除「build-all 声称静态 CGO=0 却依赖 CGO 驱动」的文档/脚本谎言。  
- 后续 modernc 有明确门禁，不会半吊子引入。

### 负面 / 约束

- Windows CI 与开发机必须具备 CGO 工具链；纯 `CGO_ENABLED=0` 默认 `go test` 不能作为 SQLite 路径的唯一验证。  
- 交叉编译与极简容器镜像成本高于 pure-Go driver。  
- Makefile 历史 `build-all` CGO=0 行为变更：若有人依赖「错误的 0」，需改为 1 并安装工具链。

### 工程约定（即日起）

1. 新增 SQLite 访问代码：仅 `go-sqlite3`，`sql.Open("sqlite3", …)`。  
2. 默认 Makefile / scripts / Tauri：**CGO_ENABLED=1**。  
3. CI 后端测试：应在 CGO 可用 runner 上跑；文档不得暗示「零 CGO 即可全绿」。  
4. 迁移 modernc 前不得删除 CGO=1 基线。

## 执行清单（M0.9）

- [x] 本 ADR Accepted  
- [x] 修正 `backend/Makefile` `build-all` → `CGO_ENABLED=1`  
- [x] 更新 living docs：G30 ✅、M0.9 门禁、ADR 索引  
- [ ] （后置）modernc 迁移专项 — 见 §3，非 M0 范围
