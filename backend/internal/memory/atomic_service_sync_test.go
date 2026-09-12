package memory

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// T13.02.b part 1：mutation 同事务接线测试
// 复用 atomic_sync_store_test.go 的 fixture：commitFaultDB（提交点故障）、
// faultVectorStore（向量 Add/Delete 故障）、fakeClock。
// ---------------------------------------------------------------------------

// setupAtomicSyncService 构造带 commit fault 与向量故障注入能力的生产形态
// AtomicMemoryService：真实文件正文库 + 真实文件向量库，故障只在注入点生效。
// worker 关闭（part 1 断言手工发布路径的 pending/attempts 确定性终态；
// 自动消费由 part 2 的 atomic_index_worker_test.go 覆盖）。
func setupAtomicSyncService(t *testing.T) (*AtomicMemoryService, *commitFaultDB, *faultVectorStore, func()) {
	t.Helper()

	db, _, cleanupDB := setupAtomicSyncTestDB(t)
	vectorStore, cleanupVector := setupSyncTestVectorStore(t)

	svc, err := NewAtomicMemoryService(context.Background(), db, vectorStore, NewSimpleEmbeddingProvider(32), WithAtomicIndexWorkerDisabled())
	if err != nil {
		cleanupVector()
		cleanupDB()
		t.Fatalf("create AtomicMemoryService failed: %v", err)
	}

	faultDB := &commitFaultDB{db: db}
	svc.beginTx = func(ctx context.Context) (atomicSyncTx, error) {
		return faultDB.beginTx(ctx)
	}
	svc.syncStore.now = newFakeClock().Now

	cleanup := func() {
		cleanupVector()
		cleanupDB()
	}
	return svc, faultDB, vectorStore, cleanup
}

func newSyncTestMemory(id, sessionID, content string) *AtomicMemory {
	return &AtomicMemory{
		ID:         id,
		Timestamp:  100,
		Content:    content,
		Tags:       []string{"sync"},
		SessionID:  sessionID,
		Source:     AtomicMemorySourceUser,
		Importance: 0.5,
	}
}

// readRevisionDeleted 直读正文行的 revision/deleted 状态（含 tombstone 行）。
func readRevisionDeleted(t *testing.T, db *sql.DB, id string) (int64, int) {
	t.Helper()
	var (
		revision int64
		deleted  int
	)
	err := db.QueryRow(`SELECT revision, deleted FROM atomic_memories WHERE id = ?`, id).Scan(&revision, &deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, -1
	}
	if err != nil {
		t.Fatalf("read revision/deleted for %s failed: %v", id, err)
	}
	return revision, deleted
}

func mustJob(t *testing.T, svc *AtomicMemoryService, id string, revision int64) *AtomicIndexJob {
	t.Helper()
	job, err := svc.syncStore.GetJob(context.Background(), id, revision)
	if err != nil || job == nil {
		t.Fatalf("GetJob(%s@%d) = %+v, %v", id, revision, job, err)
	}
	return job
}

// ---------------------------------------------------------------------------
// 生产 init 接线
// ---------------------------------------------------------------------------

func TestSQLiteAtomicMemoryServiceInitMigratesSyncSchema(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "atomic.db")
	vectorPath := filepath.Join(tmpDir, "vectors.db")
	ctx := context.Background()

	svc, err := NewSQLiteAtomicMemoryService(ctx, dbPath, vectorPath)
	if err != nil {
		t.Fatalf("NewSQLiteAtomicMemoryService failed: %v", err)
	}

	// 生产构造路径（runtime.go 的 NewSQLiteAtomicMemoryService）打开即完成
	// sync schema 迁移：user_version 推进、revision/deleted 列与 jobs 表就位。
	if v := readUserVersion(t, svc.db); v != atomicSyncSchemaVersion {
		t.Fatalf("expected user_version=%d, got %d", atomicSyncSchemaVersion, v)
	}
	cols := tableColumns(t, svc.db, "atomic_memories")
	if !cols["revision"] || !cols["deleted"] {
		t.Fatalf("expected revision/deleted columns, columns: %v", cols)
	}
	jobCols := tableColumns(t, svc.db, "atomic_index_jobs")
	for _, want := range []string{"memory_id", "revision", "operation", "state", "attempts", "last_error", "updated_at"} {
		if !jobCols[want] {
			t.Fatalf("expected column %q on atomic_index_jobs, columns: %v", want, jobCols)
		}
	}

	// 真实写路径可用：Add 落正文 + job 并发布索引。
	mem := &AtomicMemory{
		ID:         "init-1",
		Timestamp:  100,
		Content:    "生产构造路径写入的记忆",
		SessionID:  "session-init",
		Source:     AtomicMemorySourceUser,
		Importance: 0.5,
	}
	if err := svc.Add(ctx, mem); err != nil {
		t.Fatalf("Add via production-constructed service failed: %v", err)
	}
	job := mustJob(t, svc, "init-1", 1)
	if job.Operation != AtomicIndexOpUpsert || job.State != AtomicIndexJobDone {
		t.Fatalf("expected done upsert job after publish, got %+v", job)
	}
	if err := svc.Close(); err != nil {
		t.Fatalf("close service failed: %v", err)
	}

	// 重开同一对文件：迁移幂等不报错，既有正文可读。
	svc2, err := NewSQLiteAtomicMemoryService(ctx, dbPath, vectorPath)
	if err != nil {
		t.Fatalf("reopen via NewSQLiteAtomicMemoryService failed: %v", err)
	}
	defer svc2.Close()
	got, err := svc2.GetByID(ctx, "init-1")
	if err != nil || got.Content != "生产构造路径写入的记忆" {
		t.Fatalf("GetByID after reopen = %+v, %v", got, err)
	}
	if v := readUserVersion(t, svc2.db); v != atomicSyncSchemaVersion {
		t.Fatalf("expected user_version=%d after reopen, got %d", atomicSyncSchemaVersion, v)
	}
}

// ---------------------------------------------------------------------------
// Add：提交点故障 / 向量故障 / 重复 ID
// ---------------------------------------------------------------------------

func TestAtomicAddCommitFaultAtomicity(t *testing.T) {
	svc, faultDB, vectorStore, cleanup := setupAtomicSyncService(t)
	defer cleanup()
	ctx := context.Background()

	// 提交点注入故障：正文与 job 必须同时不存在（不丢——可整体重试）。
	faultDB.failNextCommit()
	err := svc.Add(ctx, newSyncTestMemory("add-c1", "session-1", "提交点故障的正文"))
	if !errors.Is(err, errInjectedCommit) {
		t.Fatalf("expected injected commit fault, got: %v", err)
	}
	if _, deleted := readRevisionDeleted(t, svc.db, "add-c1"); deleted != -1 {
		t.Fatalf("expected no row after commit fault")
	}
	if n := countJobs(t, svc.db, "add-c1"); n != 0 {
		t.Fatalf("expected 0 jobs after commit fault, got %d", n)
	}
	if n, err := vectorStore.Count(ctx); err != nil || n != 0 {
		t.Fatalf("expected 0 vectors after commit fault, count=%d err=%v", n, err)
	}

	// 无故障重试：正文 rev1 + upsert job 同事务落库，发布成功后标 done。
	if err := svc.Add(ctx, newSyncTestMemory("add-c1", "session-1", "提交点故障的正文")); err != nil {
		t.Fatalf("retry Add failed: %v", err)
	}
	if revision, deleted := readRevisionDeleted(t, svc.db, "add-c1"); revision != 1 || deleted != 0 {
		t.Fatalf("expected revision=1 deleted=0, got revision=%d deleted=%d", revision, deleted)
	}
	job := mustJob(t, svc, "add-c1", 1)
	if job.Operation != AtomicIndexOpUpsert || job.State != AtomicIndexJobDone || job.Attempts != 0 {
		t.Fatalf("expected done upsert job rev1, got %+v", job)
	}
	if n, err := vectorStore.Count(ctx); err != nil || n != 1 {
		t.Fatalf("expected 1 vector after retry, count=%d err=%v", n, err)
	}
}

func TestAtomicAddVectorFaultKeepsFactsAndReplay(t *testing.T) {
	svc, _, vectorStore, cleanup := setupAtomicSyncService(t)
	defer cleanup()
	ctx := context.Background()

	// 向量 Add 故障：正文已提交是权威事实，Add 不报错；job 记 retry 留 pending。
	vectorStore.setAddFault(true)
	mem := newSyncTestMemory("add-v1", "session-1", "向量故障场景正文")
	if err := svc.Add(ctx, mem); err != nil {
		t.Fatalf("Add with vector fault must not fail the committed facts, got: %v", err)
	}
	got, err := svc.GetByID(ctx, "add-v1")
	if err != nil || got.Content != "向量故障场景正文" {
		t.Fatalf("GetByID = %+v, %v", got, err)
	}
	job := mustJob(t, svc, "add-v1", 1)
	if job.State != AtomicIndexJobPending || job.Attempts != 1 || !strings.Contains(job.LastError, "injected vector store fault") {
		t.Fatalf("expected pending retry job, got %+v", job)
	}
	if n, err := vectorStore.Count(ctx); err != nil || n != 0 {
		t.Fatalf("expected 0 vectors during fault, count=%d err=%v", n, err)
	}

	// 故障清除后重放同一 job（worker 归 part 2，此处直接走发布路径）：索引补齐。
	vectorStore.setAddFault(false)
	svc.publishUpsert(ctx, mem, 1)
	if n, err := vectorStore.Count(ctx); err != nil || n != 1 {
		t.Fatalf("expected 1 vector after replay, count=%d err=%v", n, err)
	}
	job = mustJob(t, svc, "add-v1", 1)
	if job.State != AtomicIndexJobDone {
		t.Fatalf("expected done job after replay, got %+v", job)
	}
	results, err := svc.Search(ctx, "向量故障场景", &AtomicMemorySearchOptions{Limit: 10})
	if err != nil || len(results) != 1 || results[0].ID != "add-v1" {
		t.Fatalf("Search after replay = %+v, %v", results, err)
	}
}

func TestAtomicAddDuplicateLiveIDRejected(t *testing.T) {
	svc, _, vectorStore, cleanup := setupAtomicSyncService(t)
	defer cleanup()
	ctx := context.Background()

	if err := svc.Add(ctx, newSyncTestMemory("dup-1", "session-1", "原始内容")); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	// 存活 ID 重复 Add 仍是错误（语义不变），且不产生新 job、不改写正文。
	if err := svc.Add(ctx, newSyncTestMemory("dup-1", "session-1", "覆盖内容")); err == nil {
		t.Fatalf("expected duplicate id error")
	}
	got, err := svc.GetByID(ctx, "dup-1")
	if err != nil || got.Content != "原始内容" {
		t.Fatalf("GetByID = %+v, %v", got, err)
	}
	if revision, _ := readRevisionDeleted(t, svc.db, "dup-1"); revision != 1 {
		t.Fatalf("expected revision=1 after rejected duplicate, got %d", revision)
	}
	if n := countJobs(t, svc.db, "dup-1"); n != 1 {
		t.Fatalf("expected 1 job after rejected duplicate, got %d", n)
	}
	if n, err := vectorStore.Count(ctx); err != nil || n != 1 {
		t.Fatalf("expected 1 vector, count=%d err=%v", n, err)
	}
}

// ---------------------------------------------------------------------------
// Update：提交点故障 / 向量故障
// ---------------------------------------------------------------------------

func TestAtomicUpdateCommitFaultAtomicity(t *testing.T) {
	svc, faultDB, vectorStore, cleanup := setupAtomicSyncService(t)
	defer cleanup()
	ctx := context.Background()

	if err := svc.Add(ctx, newSyncTestMemory("upd-c1", "session-1", "更新前内容")); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// 提交点注入故障：正文保持旧版本、rev2 job 不存在、索引不丢。
	newContent := "更新后内容"
	faultDB.failNextCommit()
	err := svc.Update(ctx, "upd-c1", &AtomicMemoryUpdate{Content: &newContent})
	if !errors.Is(err, errInjectedCommit) {
		t.Fatalf("expected injected commit fault, got: %v", err)
	}
	got, err := svc.GetByID(ctx, "upd-c1")
	if err != nil || got.Content != "更新前内容" {
		t.Fatalf("expected old content after commit fault, got %+v, %v", got, err)
	}
	if revision, _ := readRevisionDeleted(t, svc.db, "upd-c1"); revision != 1 {
		t.Fatalf("expected revision=1 after commit fault, got %d", revision)
	}
	if job, err := svc.syncStore.GetJob(ctx, "upd-c1", 2); err != nil || job != nil {
		t.Fatalf("expected no rev2 job after commit fault, got %+v, %v", job, err)
	}
	if n := countJobs(t, svc.db, "upd-c1"); n != 1 {
		t.Fatalf("expected only the rev1 job, got %d", n)
	}
	if n, err := vectorStore.Count(ctx); err != nil || n != 1 {
		t.Fatalf("expected old vector preserved, count=%d err=%v", n, err)
	}

	// 无故障重试：正文 rev2 + upsert job 同事务提交，索引替换成功。
	if err := svc.Update(ctx, "upd-c1", &AtomicMemoryUpdate{Content: &newContent}); err != nil {
		t.Fatalf("retry Update failed: %v", err)
	}
	got, err = svc.GetByID(ctx, "upd-c1")
	if err != nil || got.Content != "更新后内容" {
		t.Fatalf("expected updated content, got %+v, %v", got, err)
	}
	if revision, _ := readRevisionDeleted(t, svc.db, "upd-c1"); revision != 2 {
		t.Fatalf("expected revision=2 after retry, got %d", revision)
	}
	job := mustJob(t, svc, "upd-c1", 2)
	if job.Operation != AtomicIndexOpUpsert || job.State != AtomicIndexJobDone {
		t.Fatalf("expected done upsert job rev2, got %+v", job)
	}
	if n, err := vectorStore.Count(ctx); err != nil || n != 1 {
		t.Fatalf("expected vector replaced (count 1), count=%d err=%v", n, err)
	}
	results, err := svc.Search(ctx, "更新后内容", &AtomicMemorySearchOptions{Limit: 10})
	if err != nil || len(results) != 1 || results[0].ID != "upd-c1" {
		t.Fatalf("Search after retry = %+v, %v", results, err)
	}
}

func TestAtomicUpdateVectorFaultKeepsFactsAndReplay(t *testing.T) {
	svc, _, vectorStore, cleanup := setupAtomicSyncService(t)
	defer cleanup()
	ctx := context.Background()

	if err := svc.Add(ctx, newSyncTestMemory("upd-v1", "session-1", "更新前内容")); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// 向量 Delete 故障（replaceVector 第一步）：正文更新已提交是权威事实，
	// Update 不报错；job 记 retry 留 pending；旧索引保留到重放。
	vectorStore.setDeleteFault(true)
	newContent := "更新后内容"
	if err := svc.Update(ctx, "upd-v1", &AtomicMemoryUpdate{Content: &newContent}); err != nil {
		t.Fatalf("Update with vector fault must not fail the committed facts, got: %v", err)
	}
	got, err := svc.GetByID(ctx, "upd-v1")
	if err != nil || got.Content != "更新后内容" {
		t.Fatalf("GetByID = %+v, %v", got, err)
	}
	if revision, _ := readRevisionDeleted(t, svc.db, "upd-v1"); revision != 2 {
		t.Fatalf("expected revision=2, got %d", revision)
	}
	job := mustJob(t, svc, "upd-v1", 2)
	if job.State != AtomicIndexJobPending || job.Attempts != 1 || !strings.Contains(job.LastError, "injected vector store fault") {
		t.Fatalf("expected pending retry job, got %+v", job)
	}
	if n, err := vectorStore.Count(ctx); err != nil || n != 1 {
		t.Fatalf("expected old vector preserved during fault, count=%d err=%v", n, err)
	}

	// 故障清除后重放：索引替换完成并标 done。
	vectorStore.setDeleteFault(false)
	svc.publishUpsert(ctx, got, 2)
	job = mustJob(t, svc, "upd-v1", 2)
	if job.State != AtomicIndexJobDone {
		t.Fatalf("expected done job after replay, got %+v", job)
	}
	if n, err := vectorStore.Count(ctx); err != nil || n != 1 {
		t.Fatalf("expected vector replaced after replay, count=%d err=%v", n, err)
	}
	results, err := svc.Search(ctx, "更新后内容", &AtomicMemorySearchOptions{Limit: 10})
	if err != nil || len(results) != 1 || results[0].ID != "upd-v1" {
		t.Fatalf("Search after replay = %+v, %v", results, err)
	}
}

// ---------------------------------------------------------------------------
// Delete：提交点故障 / 向量故障 / 幂等 / tombstone 防复活
// ---------------------------------------------------------------------------

func TestAtomicDeleteCommitFaultAtomicity(t *testing.T) {
	svc, faultDB, vectorStore, cleanup := setupAtomicSyncService(t)
	defer cleanup()
	ctx := context.Background()

	if err := svc.Add(ctx, newSyncTestMemory("del-c1", "session-1", "待删除内容")); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// 提交点注入故障：正文可见、tombstone 未生效、delete job 未落库、索引保留。
	faultDB.failNextCommit()
	err := svc.Delete(ctx, "del-c1")
	if !errors.Is(err, errInjectedCommit) {
		t.Fatalf("expected injected commit fault, got: %v", err)
	}
	if _, err := svc.GetByID(ctx, "del-c1"); err != nil {
		t.Fatalf("expected memory still visible after commit fault, got: %v", err)
	}
	if tombstone, err := svc.syncStore.Tombstone(ctx, "del-c1"); err != nil || tombstone != nil {
		t.Fatalf("expected no tombstone after commit fault, got %+v, %v", tombstone, err)
	}
	if job, err := svc.syncStore.GetJob(ctx, "del-c1", 2); err != nil || job != nil {
		t.Fatalf("expected no delete job after commit fault, got %+v, %v", job, err)
	}
	if n := countJobs(t, svc.db, "del-c1"); n != 1 {
		t.Fatalf("expected only the rev1 job, got %d", n)
	}
	if n, err := vectorStore.Count(ctx); err != nil || n != 1 {
		t.Fatalf("expected vector preserved, count=%d err=%v", n, err)
	}

	// 无故障重试：tombstone rev2 + delete job 同事务提交，索引清理完成。
	if err := svc.Delete(ctx, "del-c1"); err != nil {
		t.Fatalf("retry Delete failed: %v", err)
	}
	if _, err := svc.GetByID(ctx, "del-c1"); !errors.Is(err, ErrAtomicMemoryNotFound) {
		t.Fatalf("expected ErrAtomicMemoryNotFound after delete, got: %v", err)
	}
	tombstone, err := svc.syncStore.Tombstone(ctx, "del-c1")
	if err != nil || tombstone == nil || tombstone.Revision != 2 {
		t.Fatalf("expected tombstone rev2, got %+v, %v", tombstone, err)
	}
	job := mustJob(t, svc, "del-c1", 2)
	if job.Operation != AtomicIndexOpDelete || job.State != AtomicIndexJobDone {
		t.Fatalf("expected done delete job rev2, got %+v", job)
	}
	if n, err := vectorStore.Count(ctx); err != nil || n != 0 {
		t.Fatalf("expected vector cleaned, count=%d err=%v", n, err)
	}
}

func TestAtomicDeleteVectorFaultKeepsFactsAndReplay(t *testing.T) {
	svc, _, vectorStore, cleanup := setupAtomicSyncService(t)
	defer cleanup()
	ctx := context.Background()

	if err := svc.Add(ctx, newSyncTestMemory("del-v1", "session-1", "待删除内容")); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// 向量 Delete 故障：tombstone 已提交是权威事实（正文不可见），Delete
	// 不报错；delete job 记 retry 留 pending；索引残留等重放清理（E-02 触发二）。
	vectorStore.setDeleteFault(true)
	if err := svc.Delete(ctx, "del-v1"); err != nil {
		t.Fatalf("Delete with vector fault must not fail the committed facts, got: %v", err)
	}
	if _, err := svc.GetByID(ctx, "del-v1"); !errors.Is(err, ErrAtomicMemoryNotFound) {
		t.Fatalf("expected ErrAtomicMemoryNotFound after delete, got: %v", err)
	}
	tombstone, err := svc.syncStore.Tombstone(ctx, "del-v1")
	if err != nil || tombstone == nil || tombstone.Revision != 2 {
		t.Fatalf("expected tombstone rev2, got %+v, %v", tombstone, err)
	}
	job := mustJob(t, svc, "del-v1", 2)
	if job.State != AtomicIndexJobPending || job.Attempts != 1 || !strings.Contains(job.LastError, "injected vector store fault") {
		t.Fatalf("expected pending retry job, got %+v", job)
	}
	if n, err := vectorStore.Count(ctx); err != nil || n != 1 {
		t.Fatalf("expected vector residue during fault, count=%d err=%v", n, err)
	}

	// 故障清除后重放：索引残留清理完成并标 done；墓碑依旧拦截复活。
	vectorStore.setDeleteFault(false)
	svc.publishDelete(ctx, "del-v1", 2)
	if n, err := vectorStore.Count(ctx); err != nil || n != 0 {
		t.Fatalf("expected vector cleaned after replay, count=%d err=%v", n, err)
	}
	job = mustJob(t, svc, "del-v1", 2)
	if job.State != AtomicIndexJobDone {
		t.Fatalf("expected done job after replay, got %+v", job)
	}
	if ok, err := svc.syncStore.CanApplyUpsert(ctx, "del-v1", 2); err != nil || ok {
		t.Fatalf("tombstone must block resurrection at rev2: ok=%v err=%v", ok, err)
	}
}

func TestAtomicDeleteIdempotentAndHiddenFromReads(t *testing.T) {
	svc, _, vectorStore, cleanup := setupAtomicSyncService(t)
	defer cleanup()
	ctx := context.Background()

	if err := svc.Add(ctx, newSyncTestMemory("del-i1", "session-1", "重复删除场景")); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if err := svc.Delete(ctx, "del-i1"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// 重复 Delete 幂等成功（revision 继续前进、补 delete job），不再报未找到。
	if err := svc.Delete(ctx, "del-i1"); err != nil {
		t.Fatalf("repeated Delete must be idempotent, got: %v", err)
	}
	tombstone, err := svc.syncStore.Tombstone(ctx, "del-i1")
	if err != nil || tombstone == nil || tombstone.Revision != 3 {
		t.Fatalf("expected tombstone rev3 after repeated delete, got %+v, %v", tombstone, err)
	}
	latest, err := svc.syncStore.LatestJob(ctx, "del-i1")
	if err != nil || latest == nil || latest.Revision != 3 || latest.Operation != AtomicIndexOpDelete || latest.State != AtomicIndexJobDone {
		t.Fatalf("expected done delete job rev3, got %+v, %v", latest, err)
	}
	if n, err := vectorStore.Count(ctx); err != nil || n != 0 {
		t.Fatalf("expected vector cleaned, count=%d err=%v", n, err)
	}

	// tombstone 行对所有读路径不可见。
	if _, err := svc.GetByID(ctx, "del-i1"); !errors.Is(err, ErrAtomicMemoryNotFound) {
		t.Fatalf("GetByID: expected not found, got %v", err)
	}
	if items, err := svc.GetBySession(ctx, "session-1", 10, 0); err != nil || len(items) != 0 {
		t.Fatalf("GetBySession: expected empty, got %+v, %v", items, err)
	}
	if items, err := svc.SearchByTimeRange(ctx, 0, 1000); err != nil || len(items) != 0 {
		t.Fatalf("SearchByTimeRange: expected empty, got %+v, %v", items, err)
	}
	if items, err := svc.SearchByTags(ctx, []string{"sync"}); err != nil || len(items) != 0 {
		t.Fatalf("SearchByTags: expected empty, got %+v, %v", items, err)
	}
	if items, err := svc.SearchByTier(ctx, MemoryTierHot, 10); err != nil || len(items) != 0 {
		t.Fatalf("SearchByTier: expected empty, got %+v, %v", items, err)
	}
	// 对已删 ID 的 Update 同样按未找到处理。
	content := "复活尝试"
	if err := svc.Update(ctx, "del-i1", &AtomicMemoryUpdate{Content: &content}); !errors.Is(err, ErrAtomicMemoryNotFound) {
		t.Fatalf("Update on tombstoned id: expected not found, got %v", err)
	}
	// 从未存在的 ID 仍报未找到（幂等删除不改变这一语义）。
	if err := svc.Delete(ctx, "never-existed"); !errors.Is(err, ErrAtomicMemoryNotFound) {
		t.Fatalf("Delete of never-existing id: expected not found, got %v", err)
	}
}

func TestAtomicStaleUpsertCannotResurrectDeletedMemory(t *testing.T) {
	svc, _, vectorStore, cleanup := setupAtomicSyncService(t)
	defer cleanup()
	ctx := context.Background()

	// Add 时向量故障：rev1 upsert job 留 pending，索引未写。
	vectorStore.setAddFault(true)
	mem := newSyncTestMemory("stale-1", "session-1", "旧版本正文")
	if err := svc.Add(ctx, mem); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	vectorStore.setAddFault(false)

	// 随后 Delete：tombstone rev2 生效，delete job 发布完成。
	if err := svc.Delete(ctx, "stale-1"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// 晚到的旧 rev1 upsert 经发布路径重放：tombstone 门禁拦截，不写索引、
	// 不标 done（留给 part 2 worker 对账）。
	svc.publishUpsert(ctx, mem, 1)
	if n, err := vectorStore.Count(ctx); err != nil || n != 0 {
		t.Fatalf("stale upsert must not resurrect vector, count=%d err=%v", n, err)
	}
	job := mustJob(t, svc, "stale-1", 1)
	if job.State != AtomicIndexJobPending {
		t.Fatalf("gated stale job must stay pending (not done), got %+v", job)
	}
	if ok, err := svc.syncStore.CanApplyUpsert(ctx, "stale-1", 1); err != nil || ok {
		t.Fatalf("stale rev1 must be blocked: ok=%v err=%v", ok, err)
	}
	if _, err := svc.GetByID(ctx, "stale-1"); !errors.Is(err, ErrAtomicMemoryNotFound) {
		t.Fatalf("expected memory stays deleted, got: %v", err)
	}

	// 同 ID 重新 Add 是显式复活：revision 前进到 3（高于墓碑 rev2），
	// 门禁放行，正文与索引恢复。
	revived := newSyncTestMemory("stale-1", "session-1", "复活后的新正文")
	if err := svc.Add(ctx, revived); err != nil {
		t.Fatalf("re-Add of tombstoned id failed: %v", err)
	}
	if revision, deleted := readRevisionDeleted(t, svc.db, "stale-1"); revision != 3 || deleted != 0 {
		t.Fatalf("expected revived revision=3 deleted=0, got revision=%d deleted=%d", revision, deleted)
	}
	job = mustJob(t, svc, "stale-1", 3)
	if job.Operation != AtomicIndexOpUpsert || job.State != AtomicIndexJobDone {
		t.Fatalf("expected done upsert job rev3, got %+v", job)
	}
	got, err := svc.GetByID(ctx, "stale-1")
	if err != nil || got.Content != "复活后的新正文" {
		t.Fatalf("GetByID after revival = %+v, %v", got, err)
	}
	if n, err := vectorStore.Count(ctx); err != nil || n != 1 {
		t.Fatalf("expected revived vector, count=%d err=%v", n, err)
	}
	// 旧 rev1 job 仍受墓碑/旧版本拦截，不会因复活而被错误应用。
	if ok, err := svc.syncStore.CanApplyUpsert(ctx, "stale-1", 1); err != nil || ok {
		t.Fatalf("stale rev1 must stay blocked after revival: ok=%v err=%v", ok, err)
	}
}

// ---------------------------------------------------------------------------
// 重启持久性
// ---------------------------------------------------------------------------

func TestAtomicMutationJobsSurviveReopen(t *testing.T) {
	db, dbPath, cleanupDB := setupAtomicSyncTestDB(t)
	defer cleanupDB()
	vectorStore, cleanupVector := setupSyncTestVectorStore(t)
	defer cleanupVector()
	ctx := context.Background()

	svc, err := NewAtomicMemoryService(ctx, db, vectorStore, NewSimpleEmbeddingProvider(32), WithAtomicIndexWorkerDisabled())
	if err != nil {
		t.Fatalf("create service failed: %v", err)
	}

	// 向量故障下 Add：正文提交、job pending 落库。
	vectorStore.setAddFault(true)
	mem := newSyncTestMemory("reopen-1", "session-1", "重启恢复场景")
	if err := svc.Add(ctx, mem); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close db failed: %v", err)
	}

	// 模拟进程重启：重开正文库并重建服务（构造即迁移，幂等）。
	db2 := reopenAtomicSyncTestDB(t, dbPath)
	defer db2.Close()
	vectorStore.setAddFault(false)
	svc2, err := NewAtomicMemoryService(ctx, db2, vectorStore, NewSimpleEmbeddingProvider(32), WithAtomicIndexWorkerDisabled())
	if err != nil {
		t.Fatalf("create service after reopen failed: %v", err)
	}

	// pending job 仍在，正文事实仍在，重放后索引补齐、job 标 done。
	job := mustJob(t, svc2, "reopen-1", 1)
	if job.State != AtomicIndexJobPending || job.Attempts != 1 {
		t.Fatalf("expected pending job preserved across reopen, got %+v", job)
	}
	if _, err := svc2.GetByID(ctx, "reopen-1"); err != nil {
		t.Fatalf("expected memory preserved across reopen, got: %v", err)
	}
	svc2.publishUpsert(ctx, mem, 1)
	job = mustJob(t, svc2, "reopen-1", 1)
	if job.State != AtomicIndexJobDone {
		t.Fatalf("expected done job after replay, got %+v", job)
	}
	if n, err := vectorStore.Count(ctx); err != nil || n != 1 {
		t.Fatalf("expected vector after replay, count=%d err=%v", n, err)
	}
}
