# ADR 0009: SQLite 驱动迁移至 modernc.org/sqlite（CGO=0 基线）

- 状态：Accepted（决策层面；实施在计划 T0.01.b/T0.01.c，完成前 ADR 0003 的 go-sqlite3 + CGO=1 基线继续有效）
- 日期：2026-09-10
- 关联：ADR 0003（`docs/adr/0003-sqlite-cgo.md`，§3 后置项激活）；计划 T0.01（`docs/design/codeflow-3.0/2026-09-03-codeflow-3.0-implementation-plan.md` §15 任务卡、§28 S0 派发步骤）；问题 I-29；§26.5 执行基线（T0.02.a 回执）；`backend/go.mod`（`github.com/mattn/go-sqlite3 v1.14.33`，无 modernc 依赖）

## 背景

ADR 0003（2026-07-15）拍板以 `github.com/mattn/go-sqlite3` + `CGO_ENABLED=1` 为基线，其 §3 把评估/迁移 `modernc.org/sqlite` 列为「后置，非默认」，并要求启动时「单独 PR + 新 ADR」，同时给出 5 条最低验收。本 ADR 就是 §3 要求的那份新 ADR：把该后置项正式激活为 CodeFlow 3.0 的迁移决策。

3.0 计划问题 I-29：go-sqlite3 在 `CGO_ENABLED=0` 下不可执行。当前本机环境（go1.26.6 windows/amd64、`CGO_ENABLED=0`、无 gcc）下该问题有完整实测基线（计划文档 §26.5，T0.02.a，2026-09-10）：

- `backend/` 下 GA `go test ./... -count=1`：**exit 1**；43 个包 = 25 ok + 16 FAIL + 2 无测试文件（`internal/search`、`internal/web`）。
- **121 个顶层测试 + 3 个子测试失败；skipped = 0**——CGO=0 下 SQLite 测试表现为**运行期失败，而不是 skip**；GA 日志中无任何 `--- SKIP` 行。
- CGO 错误签名 `Binary was compiled with 'CGO_ENABLED=0', go-sqlite3 requires cgo` 共出现 **106 次**；没有任何包是编译失败，全部为运行期失败（另有少数与环境无关的源码扫描契约失败和全局服务未初始化失败，见 §26.5 逐包清单）。
- CI 现状（`.github/workflows/nx-ci.yml`，只读核实）：后端 job 在 ubuntu-latest 上用 Go 1.23、apt 安装 gcc、`CGO_ENABLED=1 go test -race ./...`；`e2e-smoke.yml` 不含 Go 测试。即「SQLite 测试可运行」目前完全依赖带 C 工具链的 Linux CI，本机 Windows CGO=0 环境无法验证任何 SQLite 路径。

迁移的目标收益（**目标态，非现状**）：迁移到 modernc.org/sqlite（纯 Go 转译 SQLite，库文件格式不变）后，`CGO_ENABLED=0` 且无 gcc 的 Windows 环境应能**真实运行** SQLite 集成测试——上述 106 次 stub 错误对应的路径从「失败」变为「真实执行」。**当前（2026-09-10）这些测试是失败而非已可跑，本 ADR 不宣称迁移已完成。**

本 ADR 只做决策与连接清单；实施边界：T0.01.b 新建 `backend/internal/dbx` 连接工厂并分组切换；T0.01.c 全量切换 + GA + CI。本步（T0.01.a）未修改任何源码、`go.mod`、CI 或其他文档。

## 与 ADR 0003 的关系

| ADR 0003 条目 | 本决策的处理 |
|---|---|
| §1.1 默认驱动 = go-sqlite3 | **保持至 T0.01.c 完成**。0009 设定迁移目标为 modernc；在 T0.01.c 验收通过前不得宣称已迁移。 |
| §1.2 默认构建/测试/sidecar `CGO_ENABLED=1` | **保持至 T0.01.c**。迁移完成后默认 `CGO_ENABLED=0`，CI/脚本由 T0.01.c 统一修改。 |
| §1.3 禁止同二进制双 driver 并行默认 | **保持并细化**：T0.01.b/c 过渡期内 `go.mod` 可同时存在两个依赖（编译过渡），但 dbx 工厂只允许一个生效 driver，禁止按域分流。 |
| §1.4 driver name `sql.Open("sqlite3", …)` 契约 | **被取代（自 T0.01.c 生效）**：统一切换为 `"sqlite"`，由 dbx 工厂集中定义，禁止各域散落字面量。 |
| §3 modernc 迁移（后置，非默认）+ 5 条最低验收 | **激活**。0009 即 §3 要求的「新 ADR」。5 条验收映射：①② 全量替换与覆盖 → T0.01.b 分组切换 + T0.01.c 全量 GA；③ CI 矩阵（CGO=0 主路径 + CGO 对照）→ T0.01.c（Windows CGO=0 普通测试 + Linux CGO=1 race job）；④ WAL/并发/性能 smoke（记忆与 SAMG 写路径）→ T0.01.b；⑤ Tauri sidecar 与 embed 打包验证 → T0.01.c。 |
| §3「暂不迁移的原因」（触达面广、桌面场景成熟、M0 目标） | **被 3.0 现实取代**：CGO=0 无 gcc 环境下凡打开 SQLite 的测试全部运行期失败且 skipped=0（§26.5），3.0 的 T0.01 验收断言明确要求「无 gcc 的 Windows 环境仍能运行 SQLite 集成测试」。 |
| 背景表列 7 个存储域（storage/config/context/project/planner/memory/samg） | **被实测清单取代并扩大**：本次扫描新增 5 个域（agent×2、debate、guard、floweng、skill），连接面共 12 个内部包，详见下文清单。ADR 0003 的域表不再完整，以本清单为准。 |

## 现状连接清单（T0.01.a 全量扫描）

### 扫描方法与对账

全部在 `D:/Project/Codeflow/backend` 下、glob `*.go`，2026-09-10 用 Grep 工具（ripgrep）执行；已逐个打开命中文件确认连接构造代码，非仅统计 import：

| # | 搜索式 | 命中数 | 说明 |
|---|---|---|---|
| 1 | `sql\.Open\(` | **22 命中 / 22 文件** | 全部入下表（16 生产 + 6 测试） |
| 2 | `mattn/go-sqlite3` | **23 命中 / 23 文件** | 21 个与搜索式 1 的文件重合；2 个仅注册驱动（`internal/memory/agent.go`、`internal/memory/folder_memory_test.go`，DB 由同包/辅助 store 打开）；1 个 `sql.Open` 文件自身无 import（`internal/config/service_secrets_test.go`，package config 内部测试，注册来自同包 `service.go`） |
| 3 | `sql\.OpenDB` | **0** | 无 OpenDB 构造 |
| 4 | `_journal_mode\|_pragma\|_busy_timeout\|_foreign_keys` | **74 命中 / 14 文件** | 全部落在清单内 14 个生产文件的 DSN 构造行 |
| 5 | `SetMaxOpenConns\|SetMaxIdleConns\|SetConnMaxLifetime\|SetConnMaxIdleTime` | **9 命中 / 9 文件** | 全部仅 `SetMaxOpenConns` |
| 6 | `PRAGMA (journal_mode\|busy_timeout\|synchronous\|foreign_keys\|quick_check\|wal_checkpoint)` | **9 命中 / 6 文件** | 语句级 pragma：`foreign_keys=ON`×3、`journal_mode=WAL`×2、`quick_check`×4 |
| 7 | `Connector\|driver\.Conn\|sql\.Register` | **0** | 无自定义 Connector / 额外注册 |
| 8 | `sqlite3\.(Err\|Error\|SQLiteDriver\|Version)` | **0** | 无任何 go-sqlite3 包级 API 直用（无错误类型断言、无 Backup/RegisterFunc） |
| 9 | `modernc\.org` | **0** | 当前无任何 modernc 引用（与 go.mod 一致） |

**对账结论**：搜索式 1 的 22 处 `sql.Open(` 全部入表，无清单外命中；driver name 字符串 22 处全为 `"sqlite3"`；**扫描未发现 sql.Open 之外的构造方式**（无 OpenDB、无 Connector、无绕过 database/sql 的直连——证据为搜索式 3、7、8 均为 0）。

### 逐连接清单（16 处生产连接）

DSN 模式代号（实际值，逐文件核对）：

- **F1**（5 处）：文件库 `<dbPath>?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000`；内存库 `file::memory:?cache=shared&_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000`
- **F2**（5 处）：文件库 `file:<path>?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000`（`filepath.ToSlash`）；内存库 `file:<name>?mode=memory&cache=shared&_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000`
- **F3**（3 处）：文件库 `<dbPath>?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=5000`（**无 `_foreign_keys`**）；内存库 `file:<name>?mode=memory&cache=shared&_foreign_keys=on&_busy_timeout=5000`
- **F4**（1 处）：文件库 `<dbPath>?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=5000`；无内存特判（路径必填非空）
- **F5**（2 处）：裸路径（无任何 DSN 参数），pragma 全靠语句级 `PRAGMA journal_mode = WAL`

| # | 文件:行 | 域 | 构造方式 | driver | DSN 模式 | 连接池 | 时间字段扫描 |
|---|---|---|---|---|---|---|---|
| 1 | `internal/config/service.go:60` | config / PAPI | `sql.Open` + `buildConfigSQLiteConnString`（service.go:104） | `sqlite3` | F1 | 默认（未设置） | 仅 Scan JSON TEXT；`updated_at DATETIME DEFAULT CURRENT_TIMESTAMP`（4 表，service.go:186-204）**只写不读** |
| 2 | `internal/context/service.go:189` | context presets | `sql.Open` + `buildContextSQLiteConnString`（service.go:207） | `sqlite3` | F1 | 默认 | INTEGER → int64（Unix 秒） |
| 3 | `internal/storage/storage.go:75` | storage / sessions | `sql.Open` + `buildSessionSQLiteConnString`（storage.go:90） | `sqlite3` | F1；另 `PRAGMA foreign_keys=ON` 在建表 SQL（storage.go:16） | `SetMaxOpenConns(8)` | INTEGER → int64（`CreatedAt/UpdatedAt/Timestamp` 均 int64，types.go:16-41） |
| 4 | `internal/planner/service.go:931` | planner | `sql.Open` + `buildPlannerSQLiteConnString`（service.go:949） | `sqlite3` | F1 | 默认 | INTEGER → int64（Unix 秒） |
| 5 | `internal/project/service.go:995` | project | `sql.Open` + `buildProjectSQLiteConnString`（service.go:1013） | `sqlite3` | F1 | 默认 | INTEGER → int64（Unix 秒） |
| 6 | `internal/debate/sqlite_store.go:23` | debate | `sql.Open` + `buildDebateSQLiteConnString`（:36） | `sqlite3` | F2 | `SetMaxOpenConns(1)` | INTEGER → int64 |
| 7 | `internal/guard/exemption_store.go:24` | guard | `sql.Open` + `buildExemptionSQLiteConnString`（:37） | `sqlite3` | F2 | `SetMaxOpenConns(1)` | INTEGER → int64（`expires_at`，与 `time.Time` 互转） |
| 8 | `internal/floweng/sqlite_store.go:26` | floweng | `sql.Open` + `buildFlowSQLiteConnString`（:39） | `sqlite3` | F2 | `SetMaxOpenConns(1)` | INTEGER → int64 |
| 9 | `internal/skill/sqlite_store.go:24` | skill | `sql.Open` + `buildSkillSQLiteConnString`（:37） | `sqlite3` | F2 | `SetMaxOpenConns(1)` | INTEGER → int64 |
| 10 | `internal/agent/sqlite_registry.go:23` | agent registry | `sql.Open` + `buildAgentSQLiteConnString`（:36） | `sqlite3` | F2 | `SetMaxOpenConns(1)` | INTEGER → int64（UnixMilli，:98） |
| 11 | `internal/agent/sqlite_service.go:69` | agent runtime | `sql.Open` + 内联 DSN（:65-68） | `sqlite3` | F3；FK 靠 init Exec `PRAGMA foreign_keys=ON`（:75） | 默认 | INTEGER → int64（UnixMilli，:138） |
| 12 | `internal/memory/sqlite_service.go:34` | memory items | `sql.Open` + 内联 DSN（:32-33） | `sqlite3` | F3；FK 靠 init Exec `PRAGMA foreign_keys=ON`（:38）；另 `PRAGMA quick_check`（:58） | `SetMaxOpenConns(1)` | INTEGER → int64 |
| 13 | `internal/memory/store.go:71` | memory vector | `sql.Open("sqlite3", s.config.DBPath)` 裸路径 | `sqlite3` | F5；`PRAGMA journal_mode=WAL` 仅 `WALMode=true` 时（:79） | 默认 | INTEGER → int64（`created_at DEFAULT (strftime('%s','now'))`） |
| 14 | `internal/memory/sqlite_archive.go:57` | memory raw archive | `sql.Open("sqlite3", a.dbPath)` 裸路径 | `sqlite3` | F5；恒 `PRAGMA journal_mode=WAL`（:62） | 默认 | INTEGER → int64（raw_archive.go:57-59） |
| 15 | `internal/memory/runtime.go:21` | memory atomic | `sql.Open` + 内联 DSN | `sqlite3` | F4（同一服务另经 `CreateSQLiteVectorStore` 打开 #13 向量库，共 2 连接） | `SetMaxOpenConns(8)` | INTEGER → int64（atomic_memory.go:67-80） |
| 16 | `internal/samg/sqlite_store.go:41` | SAMG graph | `sql.Open` + 内联 DSN（:37-40）；另 `PRAGMA quick_check`（:57） | `sqlite3` | F3（文件库无 FK 配置，内存库 DSN 带 `_foreign_keys=on`） | `SetMaxOpenConns(1)` | INTEGER → int64（types.go:80-109、decay.go:41-42） |

注：`internal/samg/sqlite_service.go` 无 `sql.Open`，委托 `NewSQLiteTripleStore`（即 #16），非独立连接点。

### 逐连接清单（6 处测试直连）

均为测试脚手架直接开库（裸路径、无 DSN 参数、默认连接池），配合对应服务/ store 使用：

| # | 文件:行 | 域 | 构造方式 | driver | DSN | 连接池 | 时间字段扫描 |
|---|---|---|---|---|---|---|---|
| 17 | `internal/config/service_secrets_test.go:23` | config | `sql.Open` 裸 dbPath | `sqlite3` | 无参数 | 默认 | 同 #1 |
| 18 | `internal/memory/agent_test.go:29` | memory atomic | `sql.Open` 裸路径 | `sqlite3` | 无参数 | 默认 | 同 #15 |
| 19 | `internal/memory/user_profile_test.go:97` | memory profile | `sql.Open` 裸路径 | `sqlite3` | 无参数 | 默认 | INTEGER → int64 |
| 20 | `internal/memory/atomic_memory_test.go:92` | memory atomic | `sql.Open` 裸路径 | `sqlite3` | 无参数 | 默认 | 同 #15 |
| 21 | `internal/memory/atomic_service_test.go:23` | memory atomic | `sql.Open` 裸路径 | `sqlite3` | 无参数 | 默认 | 同 #15 |
| 22 | `internal/api/handlers/memory_agent_test.go:75` | api/handlers（测试 memory agent） | `sql.Open` 裸路径 | `sqlite3` | 无参数 | 默认 | 同 #15 |

### 清单观察（T0.01.b 的设计输入，不在本步实施）

1. **pragma 应用不一致**：F3 文件库 DSN 无 `_foreign_keys`，agent runtime / memory items 靠 init 时 `PRAGMA foreign_keys=ON` 语句补齐——该语句只作用于执行它的那一条物理连接，连接池里其他连接不保证 FK 开启（#11 默认池、#12 `SetMaxOpenConns(1)` 才恰好安全）；#16 samg 文件库则完全没有 FK 配置，与内存库行为不一致。dbx 工厂应统一以 DSN 方式应用 FK。
2. **裸路径缺 busy_timeout**：#13、#14 及 6 处测试直连无 `_busy_timeout`，锁冲突立即 `SQLITE_BUSY`；#13 的 WAL 还依赖配置开关。dbx 工厂应统一补 busy_timeout=5000 与 WAL 默认。
3. **时间字段全部 INTEGER → int64**：全仓库没有任何一处依赖驱动把 TEXT 解析为 `time.Time`；唯一 `DATETIME` 声明（config 4 表 `updated_at`）只写不读。两驱动的时间解析差异对本仓库几乎无影响（唯一风险点见比较章节）。
4. 连接池语义：`SetMaxOpenConns(1)` 7 处（写序列化）、`SetMaxOpenConns(8)` 2 处、默认 7 处生产 + 6 处测试。`database/sql` 池行为与驱动无关，迁移后原样保留。

## 比较：go-sqlite3 vs modernc.org/sqlite

modernc 侧事实来源：pkg.go.dev `modernc.org/sqlite` 官方文档（2026-09-10 抓取，页面载明 SQLite 3.53.4、支持平台含 windows/amd64/arm64/386）；本机无该模块缓存（`go doc` 不可用），凡依赖具体 pin 版本的行为均列入文末「待 T0.01.b 实测验证」。

| 维度 | go-sqlite3（现状，v1.14.33） | modernc.org/sqlite（迁移目标） |
|---|---|---|
| CGO 与 Windows 工具链 | cgo 绑定捆绑的 SQLite C 源码；编译/测试需 C 工具链（Windows: gcc/MSVC）。本机 CGO=0 + 无 gcc → stub driver，22 处连接所在路径运行期全部失败（§26.5：106 次错误签名）。CI 靠 ubuntu + apt gcc + CGO=1。 | 纯 Go（ccgo 转译），`CGO_ENABLED=0` 即可编译运行；官方支持矩阵含 windows/amd64。无 C 工具链的 Windows 本机可跑 SQLite 测试（目标态）。 |
| driver name 与注册 | blank import `_ "github.com/mattn/go-sqlite3"` 注册 `"sqlite3"`；本仓库 22 处 `sql.Open` 全用 `"sqlite3"`。 | blank import `_ "modernc.org/sqlite"` 注册 `"sqlite"`；另提供 `sqlite.NewConnector(dsn)` 供 `sql.OpenDB`（本仓库暂不需要，dbx 工厂可保留该选项）。 |
| DSN / pragma 语法 | `_journal_mode=WAL` 风格 query 参数；`mode=memory&cache=shared` 等 SQLite URI 参数透传。 | **同时支持两套**：①规范形式 `_pragma=journal_mode(WAL)`（可多次，原样执行不校验）；②与 go-sqlite3 同名的 shorthand keys——文档明示「for easier DSN compatibility when migrating from github.com/mattn/go-sqlite3」，值集与 go-sqlite3 相同（大小写不敏感），非法值报错而非静默忽略；应用顺序固定（`_busy_timeout`/`_auto_vacuum` → `_pragma` → 其余 shorthand → `_query_only`）。`mode=memory&cache=shared` 为 SQLite URI 参数，两驱动等价透传。 |
| 本仓库 pragma 映射（按清单实际值） | —（现状） | `_foreign_keys=on` → `_foreign_keys=on`（shorthand，值集 0/1/false/true/no/yes/off/on）或 `_pragma=foreign_keys(on)`；`_journal_mode=WAL` → `_journal_mode=WAL`（DELETE/TRUNCATE/PERSIST/MEMORY/WAL/OFF）或 `_pragma=journal_mode(WAL)`；`_busy_timeout=5000` → `_busy_timeout=5000`（整数）或 `_pragma=busy_timeout(5000)`；`_synchronous=NORMAL` → `_synchronous=NORMAL`（0/OFF/1/NORMAL/2/FULL/3/EXTRA）或 `_pragma=synchronous(NORMAL)`。语句级 `PRAGMA foreign_keys=ON` / `journal_mode=WAL` / `quick_check` 两驱动完全一致。**预期本仓库 DSN 文本可原样工作（待 pin 版本实测确认）。** |
| time.Time 扫描 | 声明 DATE/DATETIME/TIMESTAMP 的 TEXT 列自动解析为 `time.Time`（`_loc` 控制时区）。 | 默认 `time.Time` 以 `String()` 格式写字符串；读侧由 `_time_format`/`_time_integer_format`/`_inttotime`/`_texttotime`/`_timezone` 控制；**默认** Scan TEXT DATETIME 列得到 string，与 go-sqlite3 不同。本仓库影响：全部时间列为 INTEGER→int64（无差异）；唯一 `DATETIME` 列（config `updated_at`）只写不读，若未来被 Scan 则两驱动行为不同（列入 T0.01.b 验证）。 |
| WAL / busy timeout | DSN `_journal_mode=WAL`（F1-F4）或语句级（F5）；`_busy_timeout=5000` 覆盖全部带参 DSN。WAL 是数据库文件级持久属性。 | 等价物相同（同一 PRAGMA、同一文件格式）；shorthand 应用顺序保证 `_busy_timeout` 最先应用。对 `:memory:` 库 WAL 无意义，SQLite 忽略，两驱动一致。 |
| 事务与并发写 | `_txlock`（deferred/immediate/exclusive，本仓库未使用，默认 deferred）；WAL 单写多读 + busy_timeout 重试；`SetMaxOpenConns(1)/(8)` 模式为 database/sql 层语义。 | 同样支持 `_txlock`（文档列 deferred/immediate/exclusive）；事务与锁行为由 SQLite 本身决定，两驱动等价；池设置原样保留。并发写 smoke（memory/SAMG 写路径）为 ADR 0003 §3.4 验收项，列入 T0.01.b。 |
| 错误类型 | `sqlite3.Error`/`ErrBusy` 等（本仓库 0 处直用，grep 证据见对账 #8）。 | 自有错误类型；因本仓库无 go-sqlite3 错误类型断言，切换风险低。 |
| 版本维护与风险 | v1.14.33（go.mod）；成熟，桌面/Tauri sidecar 链路已验证（ADR 0003）；交叉编译与极简镜像成本高。 | 活跃维护，文档版 SQLite 3.53.4；**官方警告「Fragile modernc.org/libc dependency」——go.mod 必须 pin 与该驱动 go.mod 完全相同的 modernc.org/libc 版本**；转译代码 CPU 性能一般低于 C 绑定（memory 向量扫描、SAMG 写路径需 benchmark smoke）；二进制体积增大。 |
| 库文件格式 | SQLite 数据库文件 | **同一 SQLite 文件格式**：无 schema/数据迁移，两套驱动可互换打开同一文件。 |

## 决策

1. **默认目标：迁移到 `modernc.org/sqlite`**（纯 Go，`CGO_ENABLED=0` 可运行 SQLite 测试）；库文件格式仍是 SQLite，无数据迁移。迁移完成后，无 gcc 的 Windows 环境（本机）与 CI 的 Windows CGO=0 job 应能真实运行 SQLite 集成测试——§26.5 基线中 16 个 FAIL 包内的 CGO 类失败（106 次 stub 错误）应转为真实执行结果。**这是目标态表述：当前（2026-09-10）CGO=0 下这些测试是失败（skipped=0，非 skip），不是「已可跑」。**
2. **本 ADR 只做决策与清单**。实施边界：T0.01.b 新增 `backend/internal/dbx` 连接工厂（Open/Close、逐连接 pragma，测试 `TestOpenForeignKeys`、`TestBusyTimeout`、`TestMemoryConnectionsIsolation`），按本清单分组切换旧 store，每组跑该包测试验证，禁止全局替换后不验证；T0.01.c 全量切换 + GA + CI（Windows CGO=0 普通测试 + Linux CGO=1 race job），检查失败包数与 **skipped 数**——迁移后持久化测试不得因 driver 不可用而 Skip（现状基线 skipped=0 是「失败」而非「跳过」，迁移后应保持 0 skip 且 CGO 类失败清零）。
3. **迁移完成前，ADR 0003 的 go-sqlite3 + CGO=1 基线继续有效**：新增 SQLite 代码仍按 ADR 0003 工程约定执行；任何「CGO=0 可跑 SQLite 测试」的表述在 T0.01.c 验收前不得写成现状。T0.01.c 完成后由主 Agent 统一更新 ADR 0003 状态与本仓库文档。
4. **driver name 契约**：由 `"sqlite3"` 切换为 `"sqlite"`，字面量集中在 dbx 工厂，各域 store 不出现驱动名字面量与 blank import。
5. **pragma 基线统一**（dbx 工厂默认）：`foreign_keys=on`、`journal_mode=WAL`（文件库）、`busy_timeout=5000`；修平清单观察 1/2 的现状差异（F3 语句级 FK、F5/测试裸路径缺 busy_timeout 与 WAL 开关不一致），修平点逐项记入 T0.01.b 回执。

## 迁移成本与回滚

- **触及面**：22 个 `sql.Open` 文件（16 生产 + 6 测试）+ 2 个仅注册文件 = **24 个文件**，跨 **12 个内部包**（config、context、storage、planner、project、debate、guard、floweng、skill、agent、memory、samg）+ `api/handlers` 测试。比 ADR 0003 背景表的 7 域多出 5 域（agent、debate、guard、floweng、skill），工作量按本清单而非 ADR 0003 估算。
- **依赖变更**：`backend/go.mod/go.sum` 增加 `modernc.org/sqlite`（及其 `modernc.org/libc` 等间接依赖，须按驱动 go.mod 对齐版本）；T0.01.c 验收后再移除 go-sqlite3。
- **DSN 转换点**：10 个 `build*SQLiteConnString` 助手（config/context/storage/planner/project 5 个 F1 + debate/guard/floweng/skill/agent-registry 5 个 F2）+ 4 个内联 DSN（agent/sqlite_service、memory/sqlite_service、memory/runtime、samg/sqlite_store）+ 2 个裸路径打开（memory/store、memory/sqlite_archive）+ 6 个测试裸路径。若 T0.01.b 实测确认 pin 版本支持 shorthand keys，则 DSN 文本无需改写、仅需 driver name；否则按比较表映射为 `_pragma=` 规范形式。两种情形都经 dbx 工厂收敛。
- **风险**：①性能回归（转译实现 vs C 绑定），重点为 memory 向量扫描与 SAMG 写路径——T0.01.b benchmark smoke；②行为差异（time 默认值、错误类型、DSN 校验严格度——modernc 对 shorthand 非法值报错，反而能暴露拼写错误）；③过渡期双依赖并存（非并行默认）；④CI 矩阵扩大（T0.01.c）。
- **回滚**：驱动切换集中在 dbx 工厂（driver name + DSN 构造单点），回退 = 工厂切回 go-sqlite3 + `go.mod` 还原；无 schema/数据迁移（文件格式相同），各域 store 不感知驱动；分组切换（T0.01.b）保证每组可独立恢复。

## 后果

### 正面（目标态，T0.01.c 验收后成立）

- 无 gcc 的 Windows 本机可运行 SQLite 集成测试，测试结果在本机与 CI 之间可解释（解决 I-29）。
- CI 可增加 Windows CGO=0 主路径；race 保留在 CGO 可用的 Linux CI（计划 §21.3）。
- 纯 Go 静态链接简化交叉编译、极简镜像与 Tauri sidecar 发布链路（移除对 C 工具链的依赖）。
- dbx 工厂顺带统一 pragma 基线，消除清单观察 1/2 的现状不一致。

### 负面 / 约束

- 引入 modernc.org/libc 版本锁定脆弱性（官方明示），升级驱动需整组对齐。
- CPU 性能预期低于 C 绑定，需 smoke 把关；性能敏感路径若回归超标，由主 Agent 裁定是否局部保留 CGO 构建变体。
- 过渡期内 `go.mod` 双依赖并存，须靠 dbx 工厂纪律避免「半迁移」状态长期化。
- ADR 0003 的工程约定（仅 go-sqlite3、CGO=1）在 T0.01.c 完成前照旧执行，文档更新集中在 T0.01.c 后由主 Agent 处理，避免双写。

## 待 T0.01.b 实测验证清单

1. pin 版本的 shorthand keys（`_foreign_keys`/`_journal_mode`/`_busy_timeout`/`_synchronous`）是否全部支持、非法值是否报错；若不支持则改用 `_pragma=` 规范形式。
2. `file::memory:?cache=shared` 匿名共享内存与 `file:<name>?mode=memory&cache=shared` 命名共享内存的连接可见性，及 `SetMaxOpenConns>1` 时的隔离行为（`TestMemoryConnectionsIsolation`）。
3. `busy_timeout=5000` 在并发写下真实生效（对照 SQLITE_BUSY 立即失败）。
4. `foreign_keys=on` 经 DSN 应用后对池内每条物理连接一致生效（对照清单观察 1 的语句级 PRAGMA 风险）。
5. config 4 表 `updated_at DATETIME` 列读写往返，确认无任何代码路径依赖 go-sqlite3 的 `time.Time` 自动解析。
6. memory 向量扫描与 SAMG 写路径 benchmark smoke，对照 go-sqlite3 基线（ADR 0003 §3.4）。
7. `PRAGMA quick_check`、WAL checkpoint、事务默认 `_txlock=deferred` 行为在全量测试套件中的表现；T0.01.c 复核失败包数与 skipped=0。
