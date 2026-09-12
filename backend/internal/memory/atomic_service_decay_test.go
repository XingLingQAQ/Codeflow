package memory

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"modernc.org/sqlite"
)

// ---------------------------------------------------------------------------
// T13.02.b part 4：衰减事务测试（E-12）
// 复用 atomic_sync_store_test.go 的 fixture：commitFaultDB（提交点故障）、
// setupAtomicSyncTestDB/setupSyncTestVectorStore（真实文件库）。
// 新增 execFaultDB（事务内第 N 行 Exec 故障）、deleteOnBeginTxDB（采集后、
// 提交前记录被并发移除）与 rows.Err 探针（驱动层注册函数在迭代中途返回
// 真实 step 错误）。SQLite 本身不被 mock。
// ---------------------------------------------------------------------------

var errInjectedExec = errors.New("injected exec fault")

// execFaultDB 包装真实 *sql.DB，事务内第 failAt 次（从 1 计）ExecContext
// 返回注入错误且语句不执行，其余调用直通真实 SQLite。
type execFaultDB struct {
	db     *sql.DB
	mu     sync.Mutex
	failAt int
	calls  int
}

func (f *execFaultDB) beginTx(ctx context.Context) (*execFaultTx, error) {
	tx, err := f.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &execFaultTx{tx: tx, owner: f}, nil
}

type execFaultTx struct {
	tx    *sql.Tx
	owner *execFaultDB
}

func (t *execFaultTx) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	t.owner.mu.Lock()
	t.owner.calls++
	call := t.owner.calls
	failAt := t.owner.failAt
	t.owner.mu.Unlock()
	if call == failAt {
		return nil, errInjectedExec
	}
	return t.tx.ExecContext(ctx, query, args...)
}

func (t *execFaultTx) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	return t.tx.QueryContext(ctx, query, args...)
}

func (t *execFaultTx) QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row {
	return t.tx.QueryRowContext(ctx, query, args...)
}

func (t *execFaultTx) Commit() error   { return t.tx.Commit() }
func (t *execFaultTx) Rollback() error { return t.tx.Rollback() }

// errInjectedRows 是 rows.Err 故障注入探针函数返回的标记错误。
var errInjectedRows = errors.New("injected rows fault")

// rowsErrProbeName 是注册到 sqlite 驱动的故障注入函数名，进程内唯一。
// 非确定性标量函数保证逐行求值、不被常量折叠。
const rowsErrProbeName = "decay_rows_err_probe_p4"

// registerRowsErrProbe 在驱动层注册探针函数：第 failOn 次求值（含
// QueryContext 内首次 step）返回注入错误，使迭代在第 failOn-1 行之后
// 真实中断，错误经 rows.Err 暴露。必须在打开测试库之前注册（函数只
// 作用于注册后新建的数据库连接）。
func registerRowsErrProbe(t *testing.T, failOn int32, calls *atomic.Int32) {
	t.Helper()
	err := sqlite.RegisterScalarFunction(rowsErrProbeName, 0,
		func(ctx *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			if calls.Add(1) == failOn {
				return nil, errInjectedRows
			}
			return int64(1), nil
		})
	if err != nil {
		t.Fatalf("register rows err probe failed: %v", err)
	}
}

// deleteOnBeginTxDB 在开衰减事务前从另一连接删除目标行，模拟"采集后、
// 提交前记录被其他操作移除"（E-12 触发场景），随后事务照常开启。
type deleteOnBeginTxDB struct {
	db *sql.DB
	id string
}

func (f *deleteOnBeginTxDB) beginTx(ctx context.Context) (atomicSyncTx, error) {
	if _, err := f.db.ExecContext(ctx, `DELETE FROM atomic_memories WHERE id = ?`, f.id); err != nil {
		return nil, err
	}
	return f.db.BeginTx(ctx, nil)
}

// setupDecayService 构造生产形态 AtomicMemoryService（真实文件正文库 +
// 真实文件向量库，索引 worker 关闭以免干扰衰减断言），返回底层 *sql.DB
// 供种子数据与回滚证据直读。
func setupDecayService(t *testing.T) (*AtomicMemoryService, *sql.DB, func()) {
	t.Helper()

	db, _, cleanupDB := setupAtomicSyncTestDB(t)
	vectorStore, cleanupVector := setupSyncTestVectorStore(t)

	svc, err := NewAtomicMemoryService(context.Background(), db, vectorStore, NewSimpleEmbeddingProvider(32), WithAtomicIndexWorkerDisabled())
	if err != nil {
		cleanupVector()
		cleanupDB()
		t.Fatalf("create AtomicMemoryService failed: %v", err)
	}

	cleanup := func() {
		cleanupVector()
		cleanupDB()
	}
	return svc, db, cleanup
}

// seedDecayCandidate 直写一行可被衰减扫到的正文（heat > 0.001）。
func seedDecayCandidate(t *testing.T, db *sql.DB, id string, heat float64, updatedAt int64) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO atomic_memories (
			id, timestamp, content, tags_json, session_id, source, importance,
			embedding_json, tier, heat, surprise, revision, deleted, updated_at
		) VALUES (?, 100, ?, '[]', 'decay-session', 'user', 0.5, '[]', 'hot', ?, 0.5, 1, 0, ?)
	`, id, "content-"+id, heat, updatedAt)
	if err != nil {
		t.Fatalf("seed decay candidate %s failed: %v", id, err)
	}
}

func readHeatUpdatedAt(t *testing.T, db *sql.DB, id string) (float64, int64) {
	t.Helper()
	var (
		heat      float64
		updatedAt int64
	)
	if err := db.QueryRow(`SELECT heat, updated_at FROM atomic_memories WHERE id = ?`, id).Scan(&heat, &updatedAt); err != nil {
		t.Fatalf("read heat/updated_at for %s failed: %v", id, err)
	}
	return heat, updatedAt
}

func decayHalfLifeSeconds() int64 {
	return int64(HeatHalfLifeDays) * 24 * 3600
}

// 正常路径：全部候选行真实提交，返回数量与实际提交行数一致；低于扫描
// 阈值的行不被触碰；updated_at 在未来（dt<=0）的行跳过不更新。
func TestApplyHeatDecayCommitsActualCount(t *testing.T) {
	svc, db, cleanup := setupDecayService(t)
	defer cleanup()

	old := time.Now().Unix() - decayHalfLifeSeconds()
	ids := []string{"d1", "d2", "d3"}
	for _, id := range ids {
		seedDecayCandidate(t, db, id, 0.8, old)
	}
	// heat 低于 0.001 扫描阈值：不进入更新集。
	seedDecayCandidate(t, db, "below-threshold", 0.0005, old)
	// updated_at 在未来：dt<=0，扫描到但跳过。
	future := time.Now().Unix() + 3600
	seedDecayCandidate(t, db, "future", 0.8, future)

	callStart := time.Now().Unix()
	n, err := svc.ApplyHeatDecay(context.Background())
	if err != nil {
		t.Fatalf("ApplyHeatDecay failed: %v", err)
	}
	if n != len(ids) {
		t.Fatalf("expected %d committed rows, got %d", len(ids), n)
	}

	for _, id := range ids {
		heat, updatedAt := readHeatUpdatedAt(t, db, id)
		// 恰过一个半衰期（加数秒执行误差）：heat' ∈ (0.3999, 0.4]。
		if heat > 0.4 || heat < 0.3999 {
			t.Fatalf("%s: expected heat near 0.4 after one half-life, got %f", id, heat)
		}
		if updatedAt < callStart {
			t.Fatalf("%s: updated_at %d not bumped to decay time (>= %d)", id, updatedAt, callStart)
		}
	}

	heat, updatedAt := readHeatUpdatedAt(t, db, "below-threshold")
	if heat != 0.0005 || updatedAt != old {
		t.Fatalf("below-threshold row must stay untouched, got heat=%f updated_at=%d", heat, updatedAt)
	}
	heat, updatedAt = readHeatUpdatedAt(t, db, "future")
	if heat != 0.8 || updatedAt != future {
		t.Fatalf("future row must stay untouched, got heat=%f updated_at=%d", heat, updatedAt)
	}
}

// 空候选集：返回 0 与 nil，不开事务。
func TestApplyHeatDecayNoCandidates(t *testing.T) {
	svc, _, cleanup := setupDecayService(t)
	defer cleanup()

	n, err := svc.ApplyHeatDecay(context.Background())
	if err != nil {
		t.Fatalf("ApplyHeatDecay on empty set failed: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 committed rows, got %d", n)
	}
}

// 负向核心：第 3 行 Exec 注入失败 → 返回错误与 0，且前两行已在事务内
// 执行的更新随整体回滚，全部 5 行保持原值（无半套衰减结果）。
func TestApplyHeatDecayExecFailureRollsBackAll(t *testing.T) {
	svc, db, cleanup := setupDecayService(t)
	defer cleanup()

	old := time.Now().Unix() - decayHalfLifeSeconds()
	ids := []string{"e1", "e2", "e3", "e4", "e5"}
	for _, id := range ids {
		seedDecayCandidate(t, db, id, 0.8, old)
	}

	faultDB := &execFaultDB{db: db, failAt: 3}
	svc.beginTx = func(ctx context.Context) (atomicSyncTx, error) {
		return faultDB.beginTx(ctx)
	}

	n, err := svc.ApplyHeatDecay(context.Background())
	if !errors.Is(err, errInjectedExec) {
		t.Fatalf("expected injected exec fault, got n=%d err=%v", n, err)
	}
	if n != 0 {
		t.Fatalf("failed decay must report 0 committed rows, got %d", n)
	}
	for _, id := range ids {
		heat, updatedAt := readHeatUpdatedAt(t, db, id)
		if heat != 0.8 || updatedAt != old {
			t.Fatalf("%s: rollback evidence broken, heat=%f updated_at=%d (want 0.8/%d)", id, heat, updatedAt, old)
		}
	}
}

// 提交点故障：整体回滚，返回错误与 0，全部行保持原值。
func TestApplyHeatDecayCommitFaultRollsBack(t *testing.T) {
	svc, db, cleanup := setupDecayService(t)
	defer cleanup()

	old := time.Now().Unix() - decayHalfLifeSeconds()
	ids := []string{"c1", "c2"}
	for _, id := range ids {
		seedDecayCandidate(t, db, id, 0.8, old)
	}

	faultDB := &commitFaultDB{db: db}
	faultDB.failNextCommit()
	svc.beginTx = func(ctx context.Context) (atomicSyncTx, error) {
		return faultDB.beginTx(ctx)
	}

	n, err := svc.ApplyHeatDecay(context.Background())
	if !errors.Is(err, errInjectedCommit) {
		t.Fatalf("expected injected commit fault, got n=%d err=%v", n, err)
	}
	if n != 0 {
		t.Fatalf("failed decay must report 0 committed rows, got %d", n)
	}
	for _, id := range ids {
		heat, updatedAt := readHeatUpdatedAt(t, db, id)
		if heat != 0.8 || updatedAt != old {
			t.Fatalf("%s: rollback evidence broken, heat=%f updated_at=%d", id, heat, updatedAt)
		}
	}
}

// 采集后、提交前行被并发移除：该行 UPDATE 的 RowsAffected=0，整体回滚
// 并返回错误，其余行保持原值。
func TestApplyHeatDecayRowRemovedMidwayRollsBack(t *testing.T) {
	svc, db, cleanup := setupDecayService(t)
	defer cleanup()

	old := time.Now().Unix() - decayHalfLifeSeconds()
	ids := []string{"r1", "r2", "victim", "r4", "r5"}
	for _, id := range ids {
		seedDecayCandidate(t, db, id, 0.8, old)
	}

	remover := &deleteOnBeginTxDB{db: db, id: "victim"}
	svc.beginTx = remover.beginTx

	n, err := svc.ApplyHeatDecay(context.Background())
	if err == nil || !strings.Contains(err.Error(), "expected 1 row affected") {
		t.Fatalf("expected rows-affected mismatch error, got n=%d err=%v", n, err)
	}
	if !strings.Contains(err.Error(), "victim") {
		t.Fatalf("error must identify the failed row, got %v", err)
	}
	if n != 0 {
		t.Fatalf("failed decay must report 0 committed rows, got %d", n)
	}
	for _, id := range []string{"r1", "r2", "r4", "r5"} {
		heat, updatedAt := readHeatUpdatedAt(t, db, id)
		if heat != 0.8 || updatedAt != old {
			t.Fatalf("%s: rollback evidence broken, heat=%f updated_at=%d", id, heat, updatedAt)
		}
	}
}

// rows.Err 注入：驱动层探针在第 2 次求值（第 1 行已完整迭代之后）返回
// 真实 step 错误，读路径必须经 rows.Err 真实检出（包装为 iterate decay
// rows）并返回错误，不得静默截断成部分集合（E-12 读侧核心）。
func TestCollectDecayItemsDetectsRowsErr(t *testing.T) {
	var probeCalls atomic.Int32
	registerRowsErrProbe(t, 2, &probeCalls)

	db, _, cleanup := setupAtomicSyncTestDB(t)
	defer cleanup()
	if err := MigrateAtomicSyncSchema(context.Background(), db); err != nil {
		t.Fatalf("migrate schema failed: %v", err)
	}

	old := time.Now().Unix() - decayHalfLifeSeconds()
	seedDecayCandidate(t, db, "ok1", 0.8, old)
	seedDecayCandidate(t, db, "ok2", 0.8, old)
	seedDecayCandidate(t, db, "ok3", 0.8, old)

	rows, err := db.QueryContext(context.Background(), `
		SELECT id, heat, updated_at FROM atomic_memories
		WHERE heat > 0.001 AND `+rowsErrProbeName+`() = 1
	`)
	if err != nil {
		t.Fatalf("probe query failed before iteration: %v", err)
	}

	items, err := collectDecayItems(rows, time.Now().Unix(), float64(decayHalfLifeSeconds()))
	if err == nil {
		t.Fatalf("rows.Err must be detected, got items=%v", items)
	}
	if items != nil {
		t.Fatalf("partial result must not be returned, got %d items", len(items))
	}
	if !strings.Contains(err.Error(), "iterate decay rows") {
		t.Fatalf("error must come from the rows.Err check, got %v", err)
	}
	// UDF 错误经 sqlite3_result_error 传播为新错误值，按文本断言来源。
	if !strings.Contains(err.Error(), errInjectedRows.Error()) {
		t.Fatalf("error must wrap the injected mid-iteration fault, got %v", err)
	}
	if got := probeCalls.Load(); got < 2 {
		t.Fatalf("probe fault did not fire mid-iteration, calls=%d", got)
	}
}

// 端到端读错误：取消的 context 使查询直接失败，返回错误与 0，不写任何行。
func TestApplyHeatDecayCancelledContext(t *testing.T) {
	svc, db, cleanup := setupDecayService(t)
	defer cleanup()

	old := time.Now().Unix() - decayHalfLifeSeconds()
	seedDecayCandidate(t, db, "x1", 0.8, old)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	n, err := svc.ApplyHeatDecay(ctx)
	if err == nil {
		t.Fatalf("cancelled context must fail the decay read, got n=%d", n)
	}
	if n != 0 {
		t.Fatalf("failed decay must report 0 committed rows, got %d", n)
	}
	heat, updatedAt := readHeatUpdatedAt(t, db, "x1")
	if heat != 0.8 || updatedAt != old {
		t.Fatalf("no row may be written on read failure, heat=%f updated_at=%d", heat, updatedAt)
	}
}

// tombstone 现状保持：衰减会扫到 tombstone 行并更新其 heat/updated_at，
// 但不得触碰 revision/deleted。
func TestApplyHeatDecayTombstoneScannedButRevisionUntouched(t *testing.T) {
	svc, db, cleanup := setupDecayService(t)
	defer cleanup()

	mem := newSyncTestMemory("tomb", "decay-session", "to be deleted")
	if err := svc.Add(context.Background(), mem); err != nil {
		t.Fatalf("add memory failed: %v", err)
	}
	if err := svc.Delete(context.Background(), "tomb"); err != nil {
		t.Fatalf("delete memory failed: %v", err)
	}
	revision, deleted := readRevisionDeleted(t, db, "tomb")
	if revision != 2 || deleted != 1 {
		t.Fatalf("expected tombstone revision=2 deleted=1, got revision=%d deleted=%d", revision, deleted)
	}

	old := time.Now().Unix() - decayHalfLifeSeconds()
	if _, err := db.Exec(`UPDATE atomic_memories SET heat = 0.8, updated_at = ? WHERE id = 'tomb'`, old); err != nil {
		t.Fatalf("rearm tombstone heat failed: %v", err)
	}

	n, err := svc.ApplyHeatDecay(context.Background())
	if err != nil {
		t.Fatalf("ApplyHeatDecay failed: %v", err)
	}
	if n != 1 {
		t.Fatalf("tombstone row is scanned and committed under current semantics, want 1 got %d", n)
	}
	revision, deleted = readRevisionDeleted(t, db, "tomb")
	if revision != 2 || deleted != 1 {
		t.Fatalf("decay must not touch revision/deleted, got revision=%d deleted=%d", revision, deleted)
	}
	heat, _ := readHeatUpdatedAt(t, db, "tomb")
	if heat > 0.4 || heat < 0.3999 {
		t.Fatalf("tombstone heat should decay like any scanned row, got %f", heat)
	}
}

// RecomputeTiers：单语句原子重算，返回实际命中行数；tombstone 行同样重算
// （现状保持），但不触碰 revision/deleted；二次调用无变化返回 0。
func TestRecomputeTiersReportsActualAffected(t *testing.T) {
	svc, db, cleanup := setupDecayService(t)
	defer cleanup()

	seed := func(id string, heat float64, tier string, revision int64, deleted int) {
		t.Helper()
		_, err := db.Exec(`
			INSERT INTO atomic_memories (
				id, timestamp, content, tags_json, session_id, source, importance,
				embedding_json, tier, heat, surprise, revision, deleted, updated_at
			) VALUES (?, 100, ?, '[]', 'decay-session', 'user', 0.5, '[]', ?, ?, 0.5, ?, ?, 100)
		`, id, "content-"+id, tier, heat, revision, deleted)
		if err != nil {
			t.Fatalf("seed %s failed: %v", id, err)
		}
	}
	seed("mismatch-hot", 0.9, "cold", 1, 0)
	seed("mismatch-cold", 0.05, "hot", 1, 0)
	seed("already-warm", 0.3, "warm", 1, 0)
	seed("already-hot", 0.6, "hot", 1, 0)
	seed("tomb-mismatch", 0.9, "cold", 3, 1)

	n, err := svc.RecomputeTiers(context.Background())
	if err != nil {
		t.Fatalf("RecomputeTiers failed: %v", err)
	}
	if n != 3 {
		t.Fatalf("expected 3 affected rows (2 live + 1 tombstone), got %d", n)
	}

	readTier := func(id string) string {
		t.Helper()
		var tier string
		if err := db.QueryRow(`SELECT tier FROM atomic_memories WHERE id = ?`, id).Scan(&tier); err != nil {
			t.Fatalf("read tier for %s failed: %v", id, err)
		}
		return tier
	}
	if tier := readTier("mismatch-hot"); tier != "hot" {
		t.Fatalf("mismatch-hot tier = %s", tier)
	}
	if tier := readTier("mismatch-cold"); tier != "cold" {
		t.Fatalf("mismatch-cold tier = %s", tier)
	}
	if tier := readTier("tomb-mismatch"); tier != "hot" {
		t.Fatalf("tombstone tier recomputed under current semantics, got %s", tier)
	}
	revision, deleted := readRevisionDeleted(t, db, "tomb-mismatch")
	if revision != 3 || deleted != 1 {
		t.Fatalf("recompute must not touch revision/deleted, got revision=%d deleted=%d", revision, deleted)
	}

	n, err = svc.RecomputeTiers(context.Background())
	if err != nil {
		t.Fatalf("second RecomputeTiers failed: %v", err)
	}
	if n != 0 {
		t.Fatalf("idempotent recompute must report 0, got %d", n)
	}
}

// BoostHeat：提升热度并按新热度重算层级，封顶 1.0；目标行不存在维持
// no-op 成功语义（既有 handler 可见契约）。
func TestBoostHeatUpdatesHeatAndTier(t *testing.T) {
	svc, db, cleanup := setupDecayService(t)
	defer cleanup()

	seed := func(id string, heat float64) {
		t.Helper()
		_, err := db.Exec(`
			INSERT INTO atomic_memories (
				id, timestamp, content, tags_json, session_id, source, importance,
				embedding_json, tier, heat, surprise, revision, deleted, updated_at
			) VALUES (?, 100, ?, '[]', 'decay-session', 'user', 0.5, '[]', 'cold', ?, 0.5, 1, 0, 100)
		`, id, "content-"+id, heat)
		if err != nil {
			t.Fatalf("seed %s failed: %v", id, err)
		}
	}
	seed("boost", 0.4)
	seed("cap", 0.95)

	if err := svc.BoostHeat(context.Background(), "boost", 0.3); err != nil {
		t.Fatalf("BoostHeat failed: %v", err)
	}
	var heat float64
	var tier string
	if err := db.QueryRow(`SELECT heat, tier FROM atomic_memories WHERE id = 'boost'`).Scan(&heat, &tier); err != nil {
		t.Fatalf("read boosted row failed: %v", err)
	}
	if heat < 0.7-1e-9 || heat > 0.7+1e-9 {
		t.Fatalf("expected heat 0.7, got %f", heat)
	}
	if tier != "hot" {
		t.Fatalf("expected tier hot after boost, got %s", tier)
	}

	// boost<=0 取默认 0.2，并封顶 1.0。
	if err := svc.BoostHeat(context.Background(), "cap", 0); err != nil {
		t.Fatalf("BoostHeat with default boost failed: %v", err)
	}
	if err := db.QueryRow(`SELECT heat FROM atomic_memories WHERE id = 'cap'`).Scan(&heat); err != nil {
		t.Fatalf("read capped row failed: %v", err)
	}
	if heat != 1.0 {
		t.Fatalf("expected heat capped at 1.0, got %f", heat)
	}

	// 目标不存在：no-op 成功（契约保持），不产生新行。
	if err := svc.BoostHeat(context.Background(), "missing", 0.5); err != nil {
		t.Fatalf("BoostHeat on missing id must stay a no-op success, got %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM atomic_memories WHERE id = 'missing'`).Scan(&count); err != nil {
		t.Fatalf("count missing row failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("no-op boost must not create rows, got %d", count)
	}
}
