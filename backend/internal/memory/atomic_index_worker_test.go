package memory

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/dbx"
)

// ---------------------------------------------------------------------------
// T13.02.b part 2：索引 worker 与启动恢复测试
// 复用 atomic_sync_store_test.go 的 fixture：faultVectorStore/fakeClock/
// setupAtomicSyncTestDB/setupSyncTestVectorStore；复用
// atomic_service_sync_test.go 的 mustJob/newSyncTestMemory。
// ---------------------------------------------------------------------------

// setupAtomicWorkerTestDB 与 setupAtomicSyncTestDB 相同，但 busy_timeout=5s：
// worker 与 mutation 并发写同一文件库时由 SQLite 忙等串行化；a 步 fixture 的
// busy_timeout=0 服务于 commit fault 确定性注入，不适合并发 consumer 场景。
func setupAtomicWorkerTestDB(t *testing.T) (*sql.DB, string, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "atomic_worker_test")
	if err != nil {
		t.Fatalf("create temp dir failed: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "atomic_worker.db")
	db, err := dbx.Open(dbPath, dbx.WithWAL(false), dbx.WithForeignKeys(false), dbx.WithBusyTimeout(5*time.Second))
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("open sqlite failed: %v", err)
	}

	cleanup := func() {
		_ = db.Close()
		_ = os.RemoveAll(tmpDir)
	}
	return db, dbPath, cleanup
}

func reopenAtomicWorkerTestDB(t *testing.T, dbPath string) *sql.DB {
	t.Helper()
	db, err := dbx.Open(dbPath, dbx.WithWAL(false), dbx.WithForeignKeys(false), dbx.WithBusyTimeout(5*time.Second))
	if err != nil {
		t.Fatalf("reopen sqlite failed: %v", err)
	}
	return db
}

// testAtomicWorkerConfig 测试用 worker 参数：短轮询、高上限（避免运行期
// 故障场景意外触顶），上限终态用例单独注入低 MaxAttempts。
func testAtomicWorkerConfig() AtomicIndexWorkerConfig {
	return AtomicIndexWorkerConfig{
		PollInterval: 10 * time.Millisecond,
		MaxAttempts:  100,
		Concurrency:  4,
		ScanLimit:    100,
		StopTimeout:  2 * time.Second,
	}
}

// setupAtomicIndexWorkerService 构造启用索引 worker 的服务（真实文件正文库
// + 真实文件向量库，向量故障只在注入点生效）。
func setupAtomicIndexWorkerService(t *testing.T, cfg AtomicIndexWorkerConfig) (*AtomicMemoryService, *faultVectorStore, func()) {
	t.Helper()

	db, _, cleanupDB := setupAtomicWorkerTestDB(t)
	vectorStore, cleanupVector := setupSyncTestVectorStore(t)

	svc, err := NewAtomicMemoryService(context.Background(), db, vectorStore, NewSimpleEmbeddingProvider(32), WithAtomicIndexWorkerConfig(cfg))
	if err != nil {
		cleanupVector()
		cleanupDB()
		t.Fatalf("create AtomicMemoryService with worker failed: %v", err)
	}
	cleanup := func() {
		cleanupVector()
		cleanupDB()
	}
	return svc, vectorStore, cleanup
}

// waitForJobState 轮询等待 job 到达目标状态；超时带最后观测值失败。
func waitForJobState(t *testing.T, svc *AtomicMemoryService, id string, revision int64, want AtomicIndexJobState, timeout time.Duration) *AtomicIndexJob {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		job, err := svc.syncStore.GetJob(context.Background(), id, revision)
		if err == nil && job != nil && job.State == want {
			return job
		}
		time.Sleep(5 * time.Millisecond)
	}
	job, _ := svc.syncStore.GetJob(context.Background(), id, revision)
	t.Fatalf("job %s@%d did not reach state %s within %s (last: %+v)", id, revision, want, timeout, job)
	return nil
}

// waitForGoroutinesAtMost 等待 goroutine 数回落到不超过 baseline（Close 后
// worker 全退出的证据）。
func waitForGoroutinesAtMost(t *testing.T, baseline int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= baseline {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("goroutines did not exit: have %d, want <= %d", runtime.NumGoroutine(), baseline)
}

// orderedVectorStore 记录成功落库的 Add/Delete 顺序（故障调用不记录），
// 用于断言同一 memory_id 的执行顺序与"旧 job 不覆盖新版本"。
type orderedVectorStore struct {
	*faultVectorStore
	mu      sync.Mutex
	adds    []string
	deletes []string
}

func (o *orderedVectorStore) Add(ctx context.Context, chunks []DocumentChunk) error {
	err := o.faultVectorStore.Add(ctx, chunks)
	if err != nil {
		return err
	}
	o.mu.Lock()
	for _, c := range chunks {
		o.adds = append(o.adds, c.ID+"|"+c.Content)
	}
	o.mu.Unlock()
	return nil
}

func (o *orderedVectorStore) Delete(ctx context.Context, ids []string) error {
	err := o.faultVectorStore.Delete(ctx, ids)
	if err != nil {
		return err
	}
	o.mu.Lock()
	o.deletes = append(o.deletes, ids...)
	o.mu.Unlock()
	return nil
}

func (o *orderedVectorStore) recordedAdds() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.adds...)
}

// ---------------------------------------------------------------------------
// 运行期故障→恢复闭环
// ---------------------------------------------------------------------------

func TestAtomicIndexWorkerRecoversAfterVectorFault(t *testing.T) {
	svc, vectorStore, cleanup := setupAtomicIndexWorkerService(t, testAtomicWorkerConfig())
	defer cleanup()
	defer svc.Close()
	ctx := context.Background()

	// 向量故障：Add 返回 nil（正文已提交是权威事实），job 留 pending。
	vectorStore.setAddFault(true)
	if err := svc.Add(ctx, newSyncTestMemory("wrk-1", "session-1", "worker 自动补齐的正文")); err != nil {
		t.Fatalf("Add with vector fault must not fail, got: %v", err)
	}
	job := mustJob(t, svc, "wrk-1", 1)
	// attempts 不钉死精确值：worker 在故障期间可能在途重试（断言点确定
	// 的是仍 pending 且索引未写），精确 attempts=1 由 worker 关闭的
	// part 1 fixture（TestAtomicAddVectorFaultKeepsFactsAndReplay）覆盖。
	if job.State != AtomicIndexJobPending || job.Attempts < 1 {
		t.Fatalf("expected pending job after fault, got %+v", job)
	}
	if n, err := vectorStore.Count(ctx); err != nil || n != 0 {
		t.Fatalf("expected 0 vectors during fault, count=%d err=%v", n, err)
	}

	// 清障后不做任何手工发布：worker 自动补齐索引并标 done（闭环证据）。
	vectorStore.setAddFault(false)
	job = waitForJobState(t, svc, "wrk-1", 1, AtomicIndexJobDone, 5*time.Second)
	if job.Operation != AtomicIndexOpUpsert {
		t.Fatalf("expected upsert job, got %+v", job)
	}
	if n, err := vectorStore.Count(ctx); err != nil || n != 1 {
		t.Fatalf("expected worker to backfill vector, count=%d err=%v", n, err)
	}
	results, err := svc.Search(ctx, "自动补齐", &AtomicMemorySearchOptions{Limit: 10})
	if err != nil || len(results) != 1 || results[0].ID != "wrk-1" {
		t.Fatalf("Search after worker recovery = %+v, %v", results, err)
	}
}

// ---------------------------------------------------------------------------
// 启动恢复 + 重开幂等
// ---------------------------------------------------------------------------

func TestAtomicIndexWorkerStartupRecoveryAtConstruct(t *testing.T) {
	db, dbPath, cleanupDB := setupAtomicWorkerTestDB(t)
	defer cleanupDB()
	vectorStore, cleanupVector := setupSyncTestVectorStore(t)
	defer cleanupVector()
	ctx := context.Background()

	// 第一世代：worker 关闭 + 向量故障，落 pending job；直接关正文库句柄
	// （保留向量库句柄跨"重启"复用）。
	svc1, err := NewAtomicMemoryService(ctx, db, vectorStore, NewSimpleEmbeddingProvider(32), WithAtomicIndexWorkerDisabled())
	if err != nil {
		t.Fatalf("create svc1 failed: %v", err)
	}
	vectorStore.setAddFault(true)
	if err := svc1.Add(ctx, newSyncTestMemory("rec-1", "session-1", "启动恢复正文")); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	job := mustJob(t, svc1, "rec-1", 1)
	if job.State != AtomicIndexJobPending || job.Attempts != 1 {
		t.Fatalf("expected pending job, got %+v", job)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close db failed: %v", err)
	}

	// 模拟进程重启：重开正文库重建服务。构造返回即恢复完成（构造内同步
	// 恢复扫描与运行期 worker 同一 applyJob 路径）。
	vectorStore.setAddFault(false)
	db2 := reopenAtomicWorkerTestDB(t, dbPath)
	svc2, err := NewAtomicMemoryService(ctx, db2, vectorStore, NewSimpleEmbeddingProvider(32), WithAtomicIndexWorkerConfig(testAtomicWorkerConfig()))
	if err != nil {
		t.Fatalf("create svc2 failed: %v", err)
	}
	job = mustJob(t, svc2, "rec-1", 1)
	if job.State != AtomicIndexJobDone {
		t.Fatalf("expected startup recovery to mark job done at construct, got %+v", job)
	}
	if n, err := vectorStore.Count(ctx); err != nil || n != 1 {
		t.Fatalf("expected startup recovery to backfill vector, count=%d err=%v", n, err)
	}
	got, err := svc2.GetByID(ctx, "rec-1")
	if err != nil || got.Content != "启动恢复正文" {
		t.Fatalf("GetByID after recovery = %+v, %v", got, err)
	}
	svc2.indexWorker.stop()
	if err := db2.Close(); err != nil {
		t.Fatalf("close db2 failed: %v", err)
	}

	// 重开幂等：job 已 done，恢复扫描为空操作，向量不重复写、状态不回退。
	db3 := reopenAtomicWorkerTestDB(t, dbPath)
	svc3, err := NewAtomicMemoryService(ctx, db3, vectorStore, NewSimpleEmbeddingProvider(32), WithAtomicIndexWorkerConfig(testAtomicWorkerConfig()))
	if err != nil {
		t.Fatalf("create svc3 failed: %v", err)
	}
	job = mustJob(t, svc3, "rec-1", 1)
	if job.State != AtomicIndexJobDone || job.Attempts != 1 {
		t.Fatalf("reopen must not reprocess done job, got %+v", job)
	}
	pending, err := svc3.syncStore.ListPendingJobs(ctx, 10)
	if err != nil || len(pending) != 0 {
		t.Fatalf("expected no pending jobs after idempotent reopen, got %+v, %v", pending, err)
	}
	if n, err := vectorStore.Count(ctx); err != nil || n != 1 {
		t.Fatalf("expected vector count stable after reopen, count=%d err=%v", n, err)
	}
	svc3.indexWorker.stop()
	if err := db3.Close(); err != nil {
		t.Fatalf("close db3 failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 同一 memory_id 串行消费；旧 revision job 不覆盖新版本
// ---------------------------------------------------------------------------

func TestAtomicIndexWorkerSerialPerMemoryID(t *testing.T) {
	db, _, cleanupDB := setupAtomicWorkerTestDB(t)
	fault, cleanupVector := setupSyncTestVectorStore(t)
	ordered := &orderedVectorStore{faultVectorStore: fault}
	cleanup := func() {
		cleanupVector()
		cleanupDB()
	}
	defer cleanup()

	svc, err := NewAtomicMemoryService(context.Background(), db, ordered, NewSimpleEmbeddingProvider(32), WithAtomicIndexWorkerConfig(testAtomicWorkerConfig()))
	if err != nil {
		t.Fatalf("create service failed: %v", err)
	}
	defer svc.Close()
	ctx := context.Background()

	// 向量 Add 故障下连续两个版本：rev1/rev2 upsert job 均 pending。
	ordered.setAddFault(true)
	if err := svc.Add(ctx, newSyncTestMemory("ser-1", "session-1", "版本一")); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	newContent := "版本二"
	if err := svc.Update(ctx, "ser-1", &AtomicMemoryUpdate{Content: &newContent}); err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	ordered.setAddFault(false)

	// 串行语义：rev2（最新 pending）先执行并标 done；之后 rev1 才被发现，
	// 门禁（存活行 rev2 > job rev1）拦截，标 failed 终态且不写向量。
	waitForJobState(t, svc, "ser-1", 2, AtomicIndexJobDone, 5*time.Second)
	job1 := waitForJobState(t, svc, "ser-1", 1, AtomicIndexJobFailed, 5*time.Second)
	if !strings.Contains(job1.LastError, atomicIndexFailStaleSuperseded) {
		t.Fatalf("expected stale_superseded terminal reason, got %+v", job1)
	}

	// 执行顺序证据：成功落库的 Add 只有一次且内容是最新版本——旧 rev1
	// 从未覆盖 rev2 的索引。
	adds := ordered.recordedAdds()
	if len(adds) != 1 || adds[0] != "ser-1|版本二" {
		t.Fatalf("expected exactly one vector add with latest content, got %v", adds)
	}
	if n, err := ordered.Count(ctx); err != nil || n != 1 {
		t.Fatalf("expected 1 vector, count=%d err=%v", n, err)
	}
	results, err := svc.Search(ctx, "版本二", &AtomicMemorySearchOptions{Limit: 10})
	if err != nil || len(results) != 1 || results[0].ID != "ser-1" {
		t.Fatalf("Search = %+v, %v", results, err)
	}

	// 终态冻结：继续运行若干轮，rev1 不再被重试（attempts 不变）。
	frozenAttempts := job1.Attempts
	time.Sleep(100 * time.Millisecond)
	job1 = mustJob(t, svc, "ser-1", 1)
	if job1.State != AtomicIndexJobFailed || job1.Attempts != frozenAttempts {
		t.Fatalf("terminal failed job must not be retried, got %+v", job1)
	}
}

// ---------------------------------------------------------------------------
// 重试上限终态：不无限循环
// ---------------------------------------------------------------------------

func TestAtomicIndexWorkerAttemptsCapMarksFailed(t *testing.T) {
	cfg := testAtomicWorkerConfig()
	cfg.MaxAttempts = 3
	svc, vectorStore, cleanup := setupAtomicIndexWorkerService(t, cfg)
	defer cleanup()
	defer svc.Close()
	ctx := context.Background()

	// 故障持续不清：内联发布 1 次 + worker 重试 2 次，达到上限标 failed。
	vectorStore.setAddFault(true)
	if err := svc.Add(ctx, newSyncTestMemory("cap-1", "session-1", "上限终态正文")); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	job := waitForJobState(t, svc, "cap-1", 1, AtomicIndexJobFailed, 5*time.Second)
	if job.Attempts != cfg.MaxAttempts {
		t.Fatalf("expected attempts=%d at terminal state, got %+v", cfg.MaxAttempts, job)
	}
	if !strings.Contains(job.LastError, atomicIndexFailAttemptsExceeded) || !strings.Contains(job.LastError, errInjectedVector.Error()) {
		t.Fatalf("expected attempts_exceeded reason with last cause, got %q", job.LastError)
	}
	if n, err := vectorStore.Count(ctx); err != nil || n != 0 {
		t.Fatalf("expected 0 vectors, count=%d err=%v", n, err)
	}

	// 终态冻结：继续运行约 10 个轮询周期，状态与 attempts 均不再变化
	// （不无限循环的负向证据）。
	time.Sleep(100 * time.Millisecond)
	job = mustJob(t, svc, "cap-1", 1)
	if job.State != AtomicIndexJobFailed || job.Attempts != cfg.MaxAttempts {
		t.Fatalf("terminal failed job must stay frozen, got %+v", job)
	}
}

// ---------------------------------------------------------------------------
// 门禁拦截的 stale job 确定性终态：不复活正文、不反复重试
// ---------------------------------------------------------------------------

func TestAtomicIndexWorkerStaleGateTerminalFailed(t *testing.T) {
	svc, vectorStore, cleanup := setupAtomicIndexWorkerService(t, testAtomicWorkerConfig())
	defer cleanup()
	defer svc.Close()
	ctx := context.Background()

	// Add 时向量故障：rev1 upsert 留 pending。故障保持（Add 必败），
	// worker 在 Delete 落 tombstone 前不可能成功应用 rev1。
	vectorStore.setAddFault(true)
	if err := svc.Add(ctx, newSyncTestMemory("stale-w1", "session-1", "旧版本正文")); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// Delete 不受 Add 故障影响：tombstone rev2 + delete job 内联发布完成。
	if err := svc.Delete(ctx, "stale-w1"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	deleteJob := mustJob(t, svc, "stale-w1", 2)
	if deleteJob.State != AtomicIndexJobDone || deleteJob.Operation != AtomicIndexOpDelete {
		t.Fatalf("expected done delete job rev2, got %+v", deleteJob)
	}

	// worker 发现 rev1 pending：门禁（tombstone/更新版本）拦截 → failed
	// 确定性终态，正文不复活、索引不写入。
	job := waitForJobState(t, svc, "stale-w1", 1, AtomicIndexJobFailed, 5*time.Second)
	if !strings.Contains(job.LastError, atomicIndexFailStaleSuperseded) {
		t.Fatalf("expected stale_superseded terminal reason, got %+v", job)
	}
	if _, err := svc.GetByID(ctx, "stale-w1"); err == nil {
		t.Fatalf("stale job must not resurrect memory")
	}
	if n, err := vectorStore.Count(ctx); err != nil || n != 0 {
		t.Fatalf("stale job must not write vector, count=%d err=%v", n, err)
	}

	// 终态冻结：attempts 不再增长（门禁终态不经 MarkRetry，不反复重试）。
	frozenAttempts := job.Attempts
	time.Sleep(100 * time.Millisecond)
	job = mustJob(t, svc, "stale-w1", 1)
	if job.State != AtomicIndexJobFailed || job.Attempts != frozenAttempts {
		t.Fatalf("gated terminal job must stay frozen, got %+v", job)
	}
	if ok, err := svc.syncStore.CanApplyUpsert(ctx, "stale-w1", 1); err != nil || ok {
		t.Fatalf("tombstone must keep blocking rev1: ok=%v err=%v", ok, err)
	}
}

// ---------------------------------------------------------------------------
// 生产构造链接线：构造即启动 worker 并消费队列；Close 后 goroutine 全退出
// ---------------------------------------------------------------------------

func TestAtomicIndexWorkerProductionChainStartsAndStops(t *testing.T) {
	baseline := runtime.NumGoroutine()
	tmpDir := t.TempDir()
	ctx := context.Background()

	svc, err := NewSQLiteAtomicMemoryService(ctx, tmpDir+"/atomic.db", tmpDir+"/vectors.db")
	if err != nil {
		t.Fatalf("NewSQLiteAtomicMemoryService failed: %v", err)
	}

	// 生产构造链（runtime.go）默认接线 worker：非仅测试可启动。
	if svc.indexWorker == nil {
		t.Fatalf("production-constructed service must have index worker wired")
	}
	if svc.indexWorker.cfg.PollInterval != defaultAtomicIndexWorkerConfig().PollInterval {
		t.Fatalf("expected default production poll interval, got %s", svc.indexWorker.cfg.PollInterval)
	}

	// 真实写路径 + 手工 enqueue 一条同步意图：worker 在生产链上真实消费。
	if err := svc.Add(ctx, &AtomicMemory{
		ID: "prod-1", Timestamp: 100, Content: "生产链 worker 消费证据",
		SessionID: "session-prod", Source: AtomicMemorySourceUser, Importance: 0.5,
	}); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if err := svc.syncStore.EnqueueUpsert(ctx, "prod-1", 2); err != nil {
		t.Fatalf("manual enqueue failed: %v", err)
	}
	waitForJobState(t, svc, "prod-1", 2, AtomicIndexJobDone, 8*time.Second)
	if n, err := svc.vectorStore.Count(ctx); err != nil || n != 1 {
		t.Fatalf("expected 1 vector after worker consumed job, count=%d err=%v", n, err)
	}

	// 关闭责任：Close 后 dispatcher 与 job goroutine 全退出。
	if err := svc.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	waitForGoroutinesAtMost(t, baseline, 5*time.Second)
}

// ---------------------------------------------------------------------------
// Close 串联：在途/pending job 不丢，goroutine 全退出，重复 Close 不 panic
// ---------------------------------------------------------------------------

func TestAtomicIndexWorkerCloseWithPendingJob(t *testing.T) {
	baseline := runtime.NumGoroutine()
	cfg := testAtomicWorkerConfig()
	db, dbPath, cleanupDB := setupAtomicWorkerTestDB(t)
	defer cleanupDB()
	vectorStore, cleanupVector := setupSyncTestVectorStore(t)
	defer cleanupVector()
	ctx := context.Background()

	svc, err := NewAtomicMemoryService(ctx, db, vectorStore, NewSimpleEmbeddingProvider(32), WithAtomicIndexWorkerConfig(cfg))
	if err != nil {
		t.Fatalf("create service failed: %v", err)
	}

	// 故障持续：job 保持 pending，worker 持续在途重试时触发 Close。
	vectorStore.setAddFault(true)
	if err := svc.Add(ctx, newSyncTestMemory("close-1", "session-1", "关闭时仍 pending")); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond) // 让 worker 进入在途重试

	start := time.Now()
	_ = svc.Close()
	if elapsed := time.Since(start); elapsed > cfg.StopTimeout+2*time.Second {
		t.Fatalf("Close exceeded bounded wait: %s", elapsed)
	}
	waitForGoroutinesAtMost(t, baseline, 5*time.Second)

	// Close 后重复调用不 panic（幂等关闭）。
	_ = svc.Close()

	// pending job 不丢：重开正文库可见恢复意图仍在（留给下次启动恢复）。
	db2 := reopenAtomicWorkerTestDB(t, dbPath)
	defer db2.Close()
	var state string
	if err := db2.QueryRow(`SELECT state FROM atomic_index_jobs WHERE memory_id = ? AND revision = 1`, "close-1").Scan(&state); err != nil {
		t.Fatalf("read persisted job after Close failed: %v", err)
	}
	if state != string(AtomicIndexJobPending) {
		t.Fatalf("pending job must survive Close, got state %q", state)
	}
}
