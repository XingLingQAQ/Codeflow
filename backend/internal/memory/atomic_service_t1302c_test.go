package memory

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// T13.02.c：envelope 回执 / Search 不完整原因 / done 行保留策略 /
// §28 点名测试 / 并发+故障恢复三一致 / Close 后 worker 全退出复验
//
// 复用 fixture：atomic_sync_store_test.go（commitFaultDB/faultVectorStore/
// fakeClock/setupAtomicSyncTestDB/setupSyncTestVectorStore）、
// atomic_service_sync_test.go（newSyncTestMemory/readRevisionDeleted/
// mustJob/countJobs/setupAtomicSyncService）、atomic_index_worker_test.go
// （setupAtomicWorkerTestDB/reopenAtomicWorkerTestDB/testAtomicWorkerConfig/
// setupAtomicIndexWorkerService/waitForJobState/waitForGoroutinesAtMost）、
// atomic_service_search_test.go（scriptedVectorStore/setupScriptedSearchService/
// searchTestMemory/addSearchCandidate/pageIDs/assertIDOrder/cloneSearchOpts）、
// atomic_service_decay_test.go（execFaultDB/seedDecayCandidate/
// readHeatUpdatedAt/decayHalfLifeSeconds）、atomic_service_fix1_test.go
// （setupProductionLikeTestDB）。
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// mutation 回执：正文已接受与索引同步状态分开表达
// ---------------------------------------------------------------------------

func TestAtomicMutationReceiptReportsIndexSyncStates(t *testing.T) {
	svc, _, vectorStore, cleanup := setupAtomicSyncService(t)
	defer cleanup()
	ctx := context.Background()

	// 正常 Add：正文提交 + 内联发布成功 → synced，revision=1，无 index error。
	receipt, err := svc.AddWithReceipt(ctx, newSyncTestMemory("rcpt-1", "session-1", "回执场景一"))
	if err != nil {
		t.Fatalf("AddWithReceipt failed: %v", err)
	}
	if receipt.ID != "rcpt-1" || receipt.Revision != 1 || receipt.IndexSync != AtomicIndexSyncSynced || receipt.IndexError != "" {
		t.Fatalf("expected synced receipt rev1, got %+v", receipt)
	}

	// 向量 Add 故障：正文已接受（不报错），回执 pending + 携带 job 错误。
	vectorStore.setAddFault(true)
	receipt, err = svc.AddWithReceipt(ctx, newSyncTestMemory("rcpt-2", "session-1", "回执场景二"))
	if err != nil {
		t.Fatalf("AddWithReceipt with vector fault must not fail committed facts, got: %v", err)
	}
	if receipt.IndexSync != AtomicIndexSyncPending || !strings.Contains(receipt.IndexError, errInjectedVector.Error()) {
		t.Fatalf("expected pending receipt with index error, got %+v", receipt)
	}

	// Update 遇向量 Delete 故障（replaceVector 第一步）：正文更新已提交，
	// 回执 pending。
	vectorStore.setAddFault(false)
	vectorStore.setDeleteFault(true)
	newContent := "回执场景一 更新"
	receipt, err = svc.UpdateWithReceipt(ctx, "rcpt-1", &AtomicMemoryUpdate{Content: &newContent})
	if err != nil {
		t.Fatalf("UpdateWithReceipt with vector fault must not fail committed facts, got: %v", err)
	}
	if receipt.Revision != 2 || receipt.IndexSync != AtomicIndexSyncPending {
		t.Fatalf("expected pending receipt rev2, got %+v", receipt)
	}
	got, err := svc.GetByID(ctx, "rcpt-1")
	if err != nil || got.Content != newContent {
		t.Fatalf("committed body must reflect update, got %+v, %v", got, err)
	}

	// Delete 遇向量故障：tombstone 已提交（正文不可见），回执 pending。
	receipt, err = svc.DeleteWithReceipt(ctx, "rcpt-2")
	if err != nil {
		t.Fatalf("DeleteWithReceipt with vector fault must not fail committed facts, got: %v", err)
	}
	if receipt.Revision != 2 || receipt.IndexSync != AtomicIndexSyncPending {
		t.Fatalf("expected pending delete receipt rev2, got %+v", receipt)
	}
	if _, err := svc.GetByID(ctx, "rcpt-2"); !errors.Is(err, ErrAtomicMemoryNotFound) {
		t.Fatalf("deleted body must be invisible, got %v", err)
	}

	// 故障清除后 worker/重放收敛，同一 job 可查 synced（回执状态的后续演进
	// 经 job 表可观测）。
	vectorStore.setDeleteFault(false)
	svc.publishUpsert(ctx, got, 2)
	if job := mustJob(t, svc, "rcpt-1", 2); job.State != AtomicIndexJobDone {
		t.Fatalf("expected done job after replay, got %+v", job)
	}
	svc.publishDelete(ctx, "rcpt-2", 2)
	if job := mustJob(t, svc, "rcpt-2", 2); job.State != AtomicIndexJobDone {
		t.Fatalf("expected done delete job after replay, got %+v", job)
	}
}

// 重复删除幂等的回执：第二次 Delete 仍成功，revision 前进，索引已清理。
func TestAtomicDeleteWithReceiptIdempotentRevisionAdvances(t *testing.T) {
	svc, _, _, cleanup := setupAtomicSyncService(t)
	defer cleanup()
	ctx := context.Background()

	if _, err := svc.AddWithReceipt(ctx, newSyncTestMemory("rcpt-d", "session-1", "幂等删除回执")); err != nil {
		t.Fatalf("AddWithReceipt failed: %v", err)
	}
	first, err := svc.DeleteWithReceipt(ctx, "rcpt-d")
	if err != nil {
		t.Fatalf("first DeleteWithReceipt failed: %v", err)
	}
	second, err := svc.DeleteWithReceipt(ctx, "rcpt-d")
	if err != nil {
		t.Fatalf("repeated DeleteWithReceipt must be idempotent, got: %v", err)
	}
	if first.Revision != 2 || second.Revision != 3 {
		t.Fatalf("expected revisions 2 then 3, got %d then %d", first.Revision, second.Revision)
	}
	if first.IndexSync != AtomicIndexSyncSynced || second.IndexSync != AtomicIndexSyncSynced {
		t.Fatalf("expected synced receipts, got %+v then %+v", first, second)
	}
	// 从未存在的 ID 仍报未找到，无回执。
	if _, err := svc.DeleteWithReceipt(ctx, "rcpt-never"); !errors.Is(err, ErrAtomicMemoryNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Search 不完整原因：向量未同步时报告原因，不返回"完整空结果"
// ---------------------------------------------------------------------------

// 负向核心：正文已提交但向量未同步（pending job）时，Search 返回空页但
// 报告必须携带不完整原因（index_sync_pending=1），而不是看似完整的空结果。
func TestAtomicSearchReportsIncompleteWhenIndexUnsynced(t *testing.T) {
	svc, _, vectorStore, cleanup := setupAtomicSyncService(t)
	defer cleanup()
	ctx := context.Background()

	vectorStore.setAddFault(true)
	if _, err := svc.AddWithReceipt(ctx, newSyncTestMemory("inc-1", "session-1", "未同步检索场景")); err != nil {
		t.Fatalf("AddWithReceipt failed: %v", err)
	}

	page, report, err := svc.SearchWithReport(ctx, "未同步检索", &AtomicMemorySearchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("SearchWithReport failed: %v", err)
	}
	if len(page) != 0 {
		t.Fatalf("vector not synced: page must be empty, got %v", pageIDs(page))
	}
	if report.IndexPendingJobs != 1 || report.IndexFailedJobs != 0 {
		t.Fatalf("expected 1 pending 0 failed, report=%+v", report)
	}
	if reason := report.IncompleteReason(); !strings.Contains(reason, "index_sync_pending=1") {
		t.Fatalf("incomplete reason must carry index_sync_pending=1, got %q", reason)
	}

	// 故障清除并重放后：结果真实出现，不完整原因消失。
	vectorStore.setAddFault(false)
	svc.publishUpsert(ctx, newSyncTestMemory("inc-1", "session-1", "未同步检索场景"), 1)
	page, report, err = svc.SearchWithReport(ctx, "未同步检索", &AtomicMemorySearchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("SearchWithReport after replay failed: %v", err)
	}
	if len(page) != 1 || page[0].ID != "inc-1" {
		t.Fatalf("expected inc-1 after replay, got %v", pageIDs(page))
	}
	if report.IncompleteReason() != "" {
		t.Fatalf("synced search must have no incomplete reason, report=%+v", report)
	}
}

// 统计口径：被更新 revision 取代的旧 job（stale pending/failed）不影响当前
// 检索完整性，不计入 backlog；当前 revision 的终态失败才计入。
func TestAtomicSearchBacklogIgnoresSupersededJobs(t *testing.T) {
	svc, _, vectorStore, cleanup := setupAtomicSyncService(t)
	defer cleanup()
	ctx := context.Background()

	// rev1 upsert 因向量故障 pending；Update 推进到 rev2 并同步完成。
	// rev1（落后于当前 revision）是被取代的历史意图，不得计入 backlog。
	vectorStore.setAddFault(true)
	if _, err := svc.AddWithReceipt(ctx, newSyncTestMemory("sup-1", "session-1", "旧版本正文")); err != nil {
		t.Fatalf("AddWithReceipt failed: %v", err)
	}
	vectorStore.setAddFault(false)
	newContent := "新版本正文"
	if _, err := svc.UpdateWithReceipt(ctx, "sup-1", &AtomicMemoryUpdate{Content: &newContent}); err != nil {
		t.Fatalf("UpdateWithReceipt failed: %v", err)
	}
	// 把 stale 的 rev1 标为 failed 终态（worker 门禁同款结局）。
	if err := svc.syncStore.MarkFailed(ctx, "sup-1", 1, "stale_superseded: test"); err != nil {
		t.Fatalf("MarkFailed failed: %v", err)
	}

	_, report, err := svc.SearchWithReport(ctx, "新版本", &AtomicMemorySearchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("SearchWithReport failed: %v", err)
	}
	if report.IndexPendingJobs != 0 || report.IndexFailedJobs != 0 {
		t.Fatalf("superseded jobs must not count as unsynced, report=%+v", report)
	}
	if reason := report.IncompleteReason(); reason != "" {
		t.Fatalf("expected empty incomplete reason, got %q", reason)
	}

	// 当前 revision 的失败才计入：rev2 已 done（MarkFailed 只作用于
	// pending），改用手工构造当前 revision 的 failed 行——enqueue rev3 并
	// 把正文推进到 rev3，再标 failed（模拟最新同步意图终态失败）。
	if err := svc.syncStore.EnqueueUpsert(ctx, "sup-1", 3); err != nil {
		t.Fatalf("EnqueueUpsert failed: %v", err)
	}
	if _, err := svc.db.Exec(`UPDATE atomic_memories SET revision = 3 WHERE id = 'sup-1'`); err != nil {
		t.Fatalf("bump body revision failed: %v", err)
	}
	if err := svc.syncStore.MarkFailed(ctx, "sup-1", 3, "attempts_exceeded: probe"); err != nil {
		t.Fatalf("MarkFailed rev3 failed: %v", err)
	}
	_, report, err = svc.SearchWithReport(ctx, "新版本", &AtomicMemorySearchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("SearchWithReport failed: %v", err)
	}
	if report.IndexFailedJobs != 1 {
		t.Fatalf("current-revision failed job must be counted, report=%+v", report)
	}
	if reason := report.IncompleteReason(); !strings.Contains(reason, "index_sync_failed=1") {
		t.Fatalf("expected index_sync_failed=1, got %q", reason)
	}
}

// 返回前的权威复核：批次过滤之后、返回之前的并发删除不得漏出；
// revision 已前进的候选以最新行刷新正文。
func TestAtomicSearchRechecksAuthoritativeStateBeforeReturn(t *testing.T) {
	svc, store, cleanup := setupScriptedSearchService(t)
	defer cleanup()
	ctx := context.Background()

	addSearchCandidate(t, svc, store, searchTestMemory("chk-1", "sf-a", 100, []string{"t1"}, nil), 0.9)
	addSearchCandidate(t, svc, store, searchTestMemory("chk-2", "sf-a", 100, []string{"t1"}, nil), 0.8)

	filtered := []atomicScoredMemory{
		{memory: AtomicMemory{ID: "chk-1", Content: "旧快照"}, score: 0.9},
		{memory: AtomicMemory{ID: "chk-2", Content: "旧快照"}, score: 0.8},
	}

	// 复核窗口内的并发事实：chk-2 被删除、chk-1 内容被更新。
	if err := svc.Delete(ctx, "chk-2"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	newContent := "检索测试正文 chk-1（已更新）"
	if err := svc.Update(ctx, "chk-1", &AtomicMemoryUpdate{Content: &newContent}); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	rechecked, err := svc.recheckFilteredAuthoritative(ctx, filtered, nil)
	if err != nil {
		t.Fatalf("recheckFilteredAuthoritative failed: %v", err)
	}
	if len(rechecked) != 1 || rechecked[0].memory.ID != "chk-1" {
		t.Fatalf("deleted candidate must be dropped at return time, got %+v", rechecked)
	}
	if rechecked[0].memory.Content != newContent {
		t.Fatalf("stale snapshot must be refreshed to latest body, got %q", rechecked[0].memory.Content)
	}

	// 空候选集直接返回，不发起多余查询。
	out, err := svc.recheckFilteredAuthoritative(ctx, nil, nil)
	if err != nil || out != nil {
		t.Fatalf("empty input must short-circuit, got %+v, %v", out, err)
	}
}

// ---------------------------------------------------------------------------
// §28 点名测试一：TestAtomicIndexRetryAfterDeleteRowMissing
// 删除重试先读 tombstone/job：正文已不可见（tombstone），重试仍继续清理
// 索引残留直至完成——重试不依赖正文读路径（GetByID 已报未找到）。
// ---------------------------------------------------------------------------

func TestAtomicIndexRetryAfterDeleteRowMissing(t *testing.T) {
	svc, vectorStore, cleanup := setupAtomicIndexWorkerService(t, testAtomicWorkerConfig())
	defer cleanup()
	defer svc.Close()
	ctx := context.Background()

	// Add 成功（向量已写）；随后向量 Delete 故障下删除：tombstone rev2 提交，
	// 正文不可见，delete job 留 pending，向量残留待重试清理（E-02 触发二）。
	if _, err := svc.AddWithReceipt(ctx, newSyncTestMemory("drm-1", "session-1", "删除重试正文")); err != nil {
		t.Fatalf("AddWithReceipt failed: %v", err)
	}
	vectorStore.setDeleteFault(true)
	receipt, err := svc.DeleteWithReceipt(ctx, "drm-1")
	if err != nil {
		t.Fatalf("DeleteWithReceipt with vector fault must not fail committed facts, got: %v", err)
	}
	if receipt.IndexSync != AtomicIndexSyncPending {
		t.Fatalf("expected pending delete receipt, got %+v", receipt)
	}
	if _, err := svc.GetByID(ctx, "drm-1"); !errors.Is(err, ErrAtomicMemoryNotFound) {
		t.Fatalf("body must be invisible (tombstone), got %v", err)
	}
	if n, err := vectorStore.Count(ctx); err != nil || n != 1 {
		t.Fatalf("expected vector residue during fault, count=%d err=%v", n, err)
	}

	// 故障清除：worker 的重试从 job 表恢复删除意图（不依赖正文可见性），
	// 清理索引残留并标 done。
	vectorStore.setDeleteFault(false)
	job := waitForJobState(t, svc, "drm-1", 2, AtomicIndexJobDone, 5*time.Second)
	if job.Operation != AtomicIndexOpDelete {
		t.Fatalf("expected delete job, got %+v", job)
	}
	if n, err := vectorStore.Count(ctx); err != nil || n != 0 {
		t.Fatalf("retried delete must clean vector residue, count=%d err=%v", n, err)
	}
	if _, err := svc.GetByID(ctx, "drm-1"); !errors.Is(err, ErrAtomicMemoryNotFound) {
		t.Fatalf("body must stay invisible after retry, got %v", err)
	}
	tombstone, err := svc.syncStore.Tombstone(ctx, "drm-1")
	if err != nil || tombstone == nil || tombstone.Revision != 2 {
		t.Fatalf("tombstone must stay at rev2, got %+v, %v", tombstone, err)
	}
}

// ---------------------------------------------------------------------------
// §28 点名测试二：TestAtomicIndexOldJobCannotResurrectDeletedMemory
// 晚到的旧 revision upsert job（worker 重放）不得复活已删记忆：门禁拦截
// 标 failed 终态，正文保持 tombstone 不可见，索引不写入。
// ---------------------------------------------------------------------------

func TestAtomicIndexOldJobCannotResurrectDeletedMemory(t *testing.T) {
	svc, vectorStore, cleanup := setupAtomicIndexWorkerService(t, testAtomicWorkerConfig())
	defer cleanup()
	defer svc.Close()
	ctx := context.Background()

	// Add 时向量故障：rev1 upsert 留 pending（旧 job），索引未写。故障保持
	// （Add 必败），worker 在 Delete 落 tombstone 前不可能成功应用 rev1——
	// 消除"worker 先应用 rev1 成功"的竞态。
	vectorStore.setAddFault(true)
	if _, err := svc.AddWithReceipt(ctx, newSyncTestMemory("res-1", "session-1", "旧版本正文")); err != nil {
		t.Fatalf("AddWithReceipt failed: %v", err)
	}

	// 删除不受 Add 故障影响（publishDelete 只删向量）：tombstone rev2 +
	// delete job 内联完成（索引本就为空，清理幂等）。
	dReceipt, err := svc.DeleteWithReceipt(ctx, "res-1")
	if err != nil {
		t.Fatalf("DeleteWithReceipt failed: %v", err)
	}
	if dReceipt.Revision != 2 || dReceipt.IndexSync != AtomicIndexSyncSynced {
		t.Fatalf("expected synced delete receipt rev2, got %+v", dReceipt)
	}
	vectorStore.setAddFault(false)

	// worker 重放晚到的 rev1 upsert：tombstone 门禁拦截 → failed 终态，
	// 正文不复活、索引不写入。
	job := waitForJobState(t, svc, "res-1", 1, AtomicIndexJobFailed, 5*time.Second)
	if !strings.Contains(job.LastError, atomicIndexFailStaleSuperseded) {
		t.Fatalf("expected stale_superseded terminal reason, got %+v", job)
	}
	if _, err := svc.GetByID(ctx, "res-1"); !errors.Is(err, ErrAtomicMemoryNotFound) {
		t.Fatalf("old job must not resurrect deleted memory, got %v", err)
	}
	if n, err := vectorStore.Count(ctx); err != nil || n != 0 {
		t.Fatalf("old job must not write vector, count=%d err=%v", n, err)
	}
	if tombstone, err := svc.syncStore.Tombstone(ctx, "res-1"); err != nil || tombstone == nil || tombstone.Revision != 2 {
		t.Fatalf("tombstone must stay at rev2, got %+v, %v", tombstone, err)
	}
	// 终态冻结：不再重试。
	frozenAttempts := job.Attempts
	time.Sleep(100 * time.Millisecond)
	job = mustJob(t, svc, "res-1", 1)
	if job.State != AtomicIndexJobFailed || job.Attempts != frozenAttempts {
		t.Fatalf("terminal failed job must stay frozen, got %+v", job)
	}
}

// ---------------------------------------------------------------------------
// §28 点名测试三：TestAtomicIndexReconcileAfterReopen
// 遗留 pending job（upsert + delete）在进程重开后由启动恢复对账：向量补齐/
// 残留清理、job 标 done；二次重开幂等（不重复处理、状态不回退）。
// ---------------------------------------------------------------------------

func TestAtomicIndexReconcileAfterReopen(t *testing.T) {
	db, dbPath, cleanupDB := setupAtomicWorkerTestDB(t)
	defer cleanupDB()
	vectorStore, cleanupVector := setupSyncTestVectorStore(t)
	defer cleanupVector()
	ctx := context.Background()

	// 第一世代：worker 关闭。mem-a 向量 Add 故障 → pending upsert；
	// mem-b 正常 Add 后向量 Delete 故障 → pending delete + 向量残留。
	svc1, err := NewAtomicMemoryService(ctx, db, vectorStore, NewSimpleEmbeddingProvider(32), WithAtomicIndexWorkerDisabled())
	if err != nil {
		t.Fatalf("create svc1 failed: %v", err)
	}
	vectorStore.setAddFault(true)
	if _, err := svc1.AddWithReceipt(ctx, newSyncTestMemory("recon-a", "session-1", "恢复补齐正文")); err != nil {
		t.Fatalf("AddWithReceipt mem-a failed: %v", err)
	}
	vectorStore.setAddFault(false)
	if _, err := svc1.AddWithReceipt(ctx, newSyncTestMemory("recon-b", "session-1", "恢复清理正文")); err != nil {
		t.Fatalf("AddWithReceipt mem-b failed: %v", err)
	}
	vectorStore.setDeleteFault(true)
	dReceipt, err := svc1.DeleteWithReceipt(ctx, "recon-b")
	if err != nil {
		t.Fatalf("DeleteWithReceipt mem-b failed: %v", err)
	}
	if dReceipt.IndexSync != AtomicIndexSyncPending {
		t.Fatalf("expected pending delete receipt, got %+v", dReceipt)
	}
	if job := mustJob(t, svc1, "recon-a", 1); job.State != AtomicIndexJobPending {
		t.Fatalf("expected pending upsert job, got %+v", job)
	}
	if n, err := vectorStore.Count(ctx); err != nil || n != 1 {
		t.Fatalf("expected 1 residual vector (mem-b), count=%d err=%v", n, err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close db failed: %v", err)
	}

	// 模拟进程重启：重开正文库重建服务（worker 启用），构造即完成恢复对账。
	vectorStore.setDeleteFault(false)
	db2 := reopenAtomicWorkerTestDB(t, dbPath)
	svc2, err := NewAtomicMemoryService(ctx, db2, vectorStore, NewSimpleEmbeddingProvider(32), WithAtomicIndexWorkerConfig(testAtomicWorkerConfig()))
	if err != nil {
		t.Fatalf("create svc2 failed: %v", err)
	}
	if job := mustJob(t, svc2, "recon-a", 1); job.State != AtomicIndexJobDone {
		t.Fatalf("expected upsert job reconciled at reopen, got %+v", job)
	}
	if job := mustJob(t, svc2, "recon-b", 2); job.State != AtomicIndexJobDone {
		t.Fatalf("expected delete job reconciled at reopen, got %+v", job)
	}
	// 三一致：mem-a 正文可见+向量就位；mem-b 正文不可见+向量已清理。
	if got, err := svc2.GetByID(ctx, "recon-a"); err != nil || got.Content != "恢复补齐正文" {
		t.Fatalf("GetByID recon-a = %+v, %v", got, err)
	}
	if _, err := svc2.GetByID(ctx, "recon-b"); !errors.Is(err, ErrAtomicMemoryNotFound) {
		t.Fatalf("recon-b must stay deleted, got %v", err)
	}
	if n, err := vectorStore.Count(ctx); err != nil || n != 1 {
		t.Fatalf("expected exactly 1 vector (recon-a) after reconcile, count=%d err=%v", n, err)
	}
	if results, err := svc2.Search(ctx, "恢复补齐", &AtomicMemorySearchOptions{Limit: 10}); err != nil || len(results) != 1 || results[0].ID != "recon-a" {
		t.Fatalf("Search after reconcile = %+v, %v", results, err)
	}
	svc2.indexWorker.stop()
	if err := db2.Close(); err != nil {
		t.Fatalf("close db2 failed: %v", err)
	}

	// 二次重开幂等：无 pending，已 done job 不重放（attempts 不回退），
	// 向量计数稳定。
	db3 := reopenAtomicWorkerTestDB(t, dbPath)
	svc3, err := NewAtomicMemoryService(ctx, db3, vectorStore, NewSimpleEmbeddingProvider(32), WithAtomicIndexWorkerConfig(testAtomicWorkerConfig()))
	if err != nil {
		t.Fatalf("create svc3 failed: %v", err)
	}
	pending, err := svc3.syncStore.ListPendingJobs(ctx, 10)
	if err != nil || len(pending) != 0 {
		t.Fatalf("expected no pending jobs after idempotent reopen, got %+v, %v", pending, err)
	}
	if n, err := vectorStore.Count(ctx); err != nil || n != 1 {
		t.Fatalf("vector count must stay stable across reopen, count=%d err=%v", n, err)
	}
	if _, report, err := svc3.SearchWithReport(ctx, "恢复补齐", &AtomicMemorySearchOptions{Limit: 10}); err != nil || report.IndexPendingJobs != 0 {
		t.Fatalf("reopened search must report no unsynced jobs, report=%+v err=%v", report, err)
	}
	svc3.indexWorker.stop()
	if err := db3.Close(); err != nil {
		t.Fatalf("close db3 failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// §28 点名测试四：TestAtomicFilteredTopKFindsSecondCandidate
// E-11 触发场景：最高分不匹配指定 tag、第二名匹配；limit=1 时必须续取到
// 第二名（旧实现 TopK=1 截断后过滤，返回假空）。
// ---------------------------------------------------------------------------

func TestAtomicFilteredTopKFindsSecondCandidate(t *testing.T) {
	svc, store, cleanup := setupScriptedSearchService(t)
	defer cleanup()
	ctx := context.Background()

	// 第 1 名（score 0.95）tag=skip 不匹配；第 2 名（score 0.90）tag=keep 匹配。
	addSearchCandidate(t, svc, store, searchTestMemory("top-1st", "sf-a", 100, []string{"skip"}, nil), 0.95)
	addSearchCandidate(t, svc, store, searchTestMemory("top-2nd", "sf-a", 100, []string{"keep"}, nil), 0.90)

	opts := &AtomicMemorySearchOptions{Tags: []string{"keep"}, Limit: 1}
	page, report, err := svc.SearchWithReport(ctx, "检索测试", opts)
	if err != nil {
		t.Fatalf("SearchWithReport failed: %v", err)
	}
	assertIDOrder(t, page, []string{"top-2nd"})
	if report.BudgetLimited {
		t.Fatalf("must not be budget limited, report=%+v", report)
	}
	if report.CandidatesScanned < 2 {
		t.Fatalf("resume must scan past the filtered first candidate, report=%+v", report)
	}

	// 旧签名 Search 行为一致：找到第二名而非假空。
	legacy, err := svc.Search(ctx, "检索测试", opts)
	if err != nil {
		t.Fatalf("legacy Search failed: %v", err)
	}
	assertIDOrder(t, legacy, []string{"top-2nd"})
}

// ---------------------------------------------------------------------------
// §28 点名测试五：TestAtomicFilteredPaginationAcrossPages
// 匹配项分散在候选深处，符合条件的第一页与第二页都可取到；逐页遍历不漏
// 不重，末尾空页来自候选耗尽（真空）。
// ---------------------------------------------------------------------------

func TestAtomicFilteredPaginationAcrossPages(t *testing.T) {
	svc, store, cleanup := setupScriptedSearchService(t)
	defer cleanup()
	ctx := context.Background()

	// 小批量强制多批续取；8 名候选中仅 rank 2/4/7（1 起）folder-a 匹配。
	svc.searchBatchSize = 2
	svc.searchCandidateBudget = 100

	folderA := "folder-a"
	folderB := "folder-b"
	matchRanks := map[int]bool{2: true, 4: true, 7: true}
	for i := 1; i <= 8; i++ {
		folder := &folderB
		if matchRanks[i] {
			folder = &folderA
		}
		id := fmt.Sprintf("pg-%d", i)
		addSearchCandidate(t, svc, store, searchTestMemory(id, "sf-a", int64(100+i), []string{"t1"}, folder), 1.0-float64(i)*0.01)
	}

	baseOpts := &AtomicMemorySearchOptions{FolderID: folderA, Limit: 2}
	wantPages := [][]string{
		{"pg-2", "pg-4"},
		{"pg-7"},
		{},
	}

	seen := make(map[string]int)
	for pageIdx, want := range wantPages {
		page, report, err := svc.SearchWithReport(ctx, "检索测试", cloneSearchOpts(baseOpts, pageIdx*2))
		if err != nil {
			t.Fatalf("page %d failed: %v", pageIdx, err)
		}
		assertIDOrder(t, page, want)
		if report.BudgetLimited {
			t.Fatalf("page %d must not be budget limited, report=%+v", pageIdx, report)
		}
		for _, id := range pageIDs(page) {
			seen[id]++
			if seen[id] > 1 {
				t.Fatalf("duplicate id %s across pages", id)
			}
		}
		if len(page) == 0 && !report.Exhausted {
			t.Fatalf("page %d empty page must come from exhaustion, report=%+v", pageIdx, report)
		}
	}
	if len(seen) != 3 {
		t.Fatalf("expected 3 unique matched ids across pages, got %v", seen)
	}
}

// ---------------------------------------------------------------------------
// §28 点名测试六：TestAtomicHeatDecayExecFailureRollsBack
// 衰减事务内第 3 行 Exec 注入失败：整体回滚、返回错误与 0（实际提交数），
// 全部行保持原值；故障清除后重试可完整提交（失败不留半套状态）。
// ---------------------------------------------------------------------------

func TestAtomicHeatDecayExecFailureRollsBack(t *testing.T) {
	svc, db, cleanup := setupDecayService(t)
	defer cleanup()

	old := time.Now().Unix() - decayHalfLifeSeconds()
	ids := []string{"he1", "he2", "he3", "he4", "he5"}
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
		t.Fatalf("failed decay must report 0 committed rows (actual, not planned), got %d", n)
	}
	for _, id := range ids {
		heat, updatedAt := readHeatUpdatedAt(t, db, id)
		if heat != 0.8 || updatedAt != old {
			t.Fatalf("%s: rollback evidence broken, heat=%f updated_at=%d (want 0.8/%d)", id, heat, updatedAt, old)
		}
	}

	// 故障清除后重试：全部 5 行真实提交，返回实际提交数。
	svc.beginTx = func(ctx context.Context) (atomicSyncTx, error) {
		return db.BeginTx(ctx, nil)
	}
	n, err = svc.ApplyHeatDecay(context.Background())
	if err != nil {
		t.Fatalf("retry ApplyHeatDecay failed: %v", err)
	}
	if n != len(ids) {
		t.Fatalf("expected %d committed rows on retry, got %d", len(ids), n)
	}
	for _, id := range ids {
		heat, _ := readHeatUpdatedAt(t, db, id)
		if heat > 0.4 || heat < 0.3999 {
			t.Fatalf("%s: expected heat near 0.4 after one half-life, got %f", id, heat)
		}
	}
}

// ---------------------------------------------------------------------------
// done 行保留策略（§26.17 登记，T13.02.c 定义）
// ---------------------------------------------------------------------------

// 规则验证：过期非最新 done 行清理；每个 memory 最新 done 行无论多旧都保留；
// 超出每 memory 数量上限的从最旧删；pending/failed 永不删除。
func TestAtomicSyncStorePruneDoneJobs(t *testing.T) {
	db, _, cleanup := setupAtomicSyncTestDB(t)
	defer cleanup()
	ctx := context.Background()
	clock := newFakeClock() // t0 = 1700000000
	store, err := NewAtomicSyncStore(ctx, db, WithAtomicSyncClock(clock.Now))
	if err != nil {
		t.Fatalf("NewAtomicSyncStore failed: %v", err)
	}

	done := func(id string, rev int64) {
		t.Helper()
		if err := store.EnqueueUpsert(ctx, id, rev); err != nil {
			t.Fatalf("enqueue %s@%d failed: %v", id, rev, err)
		}
		if err := store.MarkDone(ctx, id, rev); err != nil {
			t.Fatalf("MarkDone %s@%d failed: %v", id, rev, err)
		}
	}

	// 第 0 天（旧）：mem-a rev1、mem-b rev1、mem-c rev1/2/3 全部 done。
	done("mem-a", 1)
	done("mem-b", 1)
	done("mem-c", 1)
	done("mem-c", 2)
	done("mem-c", 3)
	// mem-d 永远 pending（待办证据）；mem-e failed 终态（失败证据）。
	if err := store.EnqueueUpsert(ctx, "mem-d", 1); err != nil {
		t.Fatalf("enqueue mem-d failed: %v", err)
	}
	if err := store.EnqueueUpsert(ctx, "mem-e", 1); err != nil {
		t.Fatalf("enqueue mem-e failed: %v", err)
	}
	if err := store.MarkFailed(ctx, "mem-e", 1, "attempts_exceeded: probe"); err != nil {
		t.Fatalf("MarkFailed mem-e failed: %v", err)
	}

	// 前进 10 天（超过 7 天窗口）：mem-a/mem-b 各补一条新的 done（rev2）；
	// mem-f 连续 20 条 done（全部新），用于数量上限规则。
	clock.Advance(10 * 24 * time.Hour)
	done("mem-a", 2)
	done("mem-b", 2)
	for rev := int64(1); rev <= 20; rev++ {
		done("mem-f", rev)
	}

	deleted, err := store.PruneDoneJobs(ctx, AtomicIndexJobPrunePolicy{MaxAge: 7 * 24 * time.Hour, MaxPerMemory: 16})
	if err != nil {
		t.Fatalf("PruneDoneJobs failed: %v", err)
	}
	// 删除：mem-a rev1、mem-b rev1、mem-c rev1/rev2（过期且有更新 done 同胞）、
	// mem-f rev1..rev4（超出每 memory 16 条上限的最旧 4 条）= 8 行。
	if deleted != 8 {
		t.Fatalf("expected 8 pruned rows, got %d", deleted)
	}

	// 每个 memory 最新 done 行保留（mem-c rev3 虽过期仍是最新回执）。
	for id, want := range map[string]int{"mem-a": 1, "mem-b": 1, "mem-c": 1, "mem-f": 16} {
		if n := countJobs(t, db, id); n != want {
			t.Fatalf("%s: expected %d jobs after prune, got %d", id, want, n)
		}
	}
	if latest, err := store.LatestJob(ctx, "mem-c"); err != nil || latest == nil || latest.Revision != 3 || latest.State != AtomicIndexJobDone {
		t.Fatalf("mem-c latest done receipt must survive pruning, got %+v, %v", latest, err)
	}
	// mem-f 保留 revision 最大的 16 条（rev5..rev20）。
	if job, err := store.GetJob(ctx, "mem-f", 4); err != nil || job != nil {
		t.Fatalf("mem-f rev4 must be pruned by cap, got %+v, %v", job, err)
	}
	if job, err := store.GetJob(ctx, "mem-f", 5); err != nil || job == nil || job.State != AtomicIndexJobDone {
		t.Fatalf("mem-f rev5 must survive cap, got %+v, %v", job, err)
	}

	// 负向：pending/failed 行不被动删。
	if job := mustJobState(t, store, "mem-d", 1); job.State != AtomicIndexJobPending {
		t.Fatalf("pending job must survive pruning, got %+v", job)
	}
	if job := mustJobState(t, store, "mem-e", 1); job.State != AtomicIndexJobFailed {
		t.Fatalf("failed job must survive pruning, got %+v", job)
	}

	// 幂等：无新增 done 行时再次清理删除 0 行。
	again, err := store.PruneDoneJobs(ctx, AtomicIndexJobPrunePolicy{MaxAge: 7 * 24 * time.Hour, MaxPerMemory: 16})
	if err != nil {
		t.Fatalf("second PruneDoneJobs failed: %v", err)
	}
	if again != 0 {
		t.Fatalf("idempotent prune must delete 0, got %d", again)
	}
}

// 保留策略零值回落默认（7 天窗口、每 memory 16 条），且默认同样不触碰
// pending/failed。
func TestAtomicSyncStorePruneDoneJobsDefaults(t *testing.T) {
	db, _, cleanup := setupAtomicSyncTestDB(t)
	defer cleanup()
	ctx := context.Background()
	clock := newFakeClock()
	store, err := NewAtomicSyncStore(ctx, db, WithAtomicSyncClock(clock.Now))
	if err != nil {
		t.Fatalf("NewAtomicSyncStore failed: %v", err)
	}

	if err := store.EnqueueUpsert(ctx, "mem-z", 1); err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}
	if err := store.MarkDone(ctx, "mem-z", 1); err != nil {
		t.Fatalf("MarkDone failed: %v", err)
	}
	clock.Advance(30 * 24 * time.Hour)
	if err := store.EnqueueUpsert(ctx, "mem-z", 2); err != nil {
		t.Fatalf("enqueue rev2 failed: %v", err)
	}
	if err := store.MarkDone(ctx, "mem-z", 2); err != nil {
		t.Fatalf("MarkDone rev2 failed: %v", err)
	}

	deleted, err := store.PruneDoneJobs(ctx, AtomicIndexJobPrunePolicy{})
	if err != nil {
		t.Fatalf("PruneDoneJobs with defaults failed: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 pruned row under defaults, got %d", deleted)
	}
	if latest, err := store.LatestJob(ctx, "mem-z"); err != nil || latest == nil || latest.Revision != 2 {
		t.Fatalf("latest done must survive default prune, got %+v, %v", latest, err)
	}
}

// worker 生产接线：构造时先按保留策略清理一次（非仅测试可调用的死代码）。
func TestAtomicIndexWorkerPrunesDoneRowsAtConstruct(t *testing.T) {
	db, dbPath, cleanupDB := setupAtomicWorkerTestDB(t)
	defer cleanupDB()
	vectorStore, cleanupVector := setupSyncTestVectorStore(t)
	defer cleanupVector()
	ctx := context.Background()

	// 第一世代：rev1/rev2 均 done；随后把 rev1 的 updated_at 老化到 10 天前。
	svc1, err := NewAtomicMemoryService(ctx, db, vectorStore, NewSimpleEmbeddingProvider(32), WithAtomicIndexWorkerDisabled())
	if err != nil {
		t.Fatalf("create svc1 failed: %v", err)
	}
	if err := svc1.Add(ctx, newSyncTestMemory("prune-1", "session-1", "清理场景 v1")); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	newContent := "清理场景 v2"
	if err := svc1.Update(ctx, "prune-1", &AtomicMemoryUpdate{Content: &newContent}); err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	aged := time.Now().Add(-10 * 24 * time.Hour).Unix()
	if _, err := db.Exec(`UPDATE atomic_index_jobs SET updated_at = ? WHERE memory_id = 'prune-1' AND revision = 1`, aged); err != nil {
		t.Fatalf("age done row failed: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close db failed: %v", err)
	}

	// 重开重建（worker 启用）：构造即清理，过期非最新 done 行 rev1 被删，
	// 最新 done 行 rev2 保留。
	db2 := reopenAtomicWorkerTestDB(t, dbPath)
	svc2, err := NewAtomicMemoryService(ctx, db2, vectorStore, NewSimpleEmbeddingProvider(32), WithAtomicIndexWorkerConfig(testAtomicWorkerConfig()))
	if err != nil {
		t.Fatalf("create svc2 failed: %v", err)
	}
	if job, err := svc2.syncStore.GetJob(ctx, "prune-1", 1); err != nil || job != nil {
		t.Fatalf("aged non-latest done row must be pruned at construct, got %+v, %v", job, err)
	}
	if latest, err := svc2.syncStore.LatestJob(ctx, "prune-1"); err != nil || latest == nil || latest.Revision != 2 || latest.State != AtomicIndexJobDone {
		t.Fatalf("latest done receipt must survive construct prune, got %+v, %v", latest, err)
	}
	svc2.indexWorker.stop()
	if err := db2.Close(); err != nil {
		t.Fatalf("close db2 failed: %v", err)
	}
}

// 运行期周期清理：PruneInterval 到期后 worker 在轮询 tick 执行清理。
func TestAtomicIndexWorkerPrunesDoneRowsPeriodically(t *testing.T) {
	cfg := testAtomicWorkerConfig()
	cfg.PruneInterval = 50 * time.Millisecond // 构造清理之后，下一个到期 tick 即触发运行期清理

	db, _, cleanupDB := setupAtomicWorkerTestDB(t)
	defer cleanupDB()
	vectorStore, cleanupVector := setupSyncTestVectorStore(t)
	defer cleanupVector()
	ctx := context.Background()

	svc, err := NewAtomicMemoryService(ctx, db, vectorStore, NewSimpleEmbeddingProvider(32), WithAtomicIndexWorkerConfig(cfg))
	if err != nil {
		t.Fatalf("create service failed: %v", err)
	}
	defer svc.Close()

	if err := svc.Add(ctx, newSyncTestMemory("prune-p", "session-1", "周期清理 v1")); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	newContent := "周期清理 v2"
	if err := svc.Update(ctx, "prune-p", &AtomicMemoryUpdate{Content: &newContent}); err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	// 老化 rev1 done 行（10 天前，超过默认 7 天窗口）：构造时的清理已过，
	// 只能由运行期周期清理删除。
	aged := time.Now().Add(-10 * 24 * time.Hour).Unix()
	if _, err := db.Exec(`UPDATE atomic_index_jobs SET updated_at = ? WHERE memory_id = 'prune-p' AND revision = 1`, aged); err != nil {
		t.Fatalf("age done row failed: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		job, err := svc.syncStore.GetJob(ctx, "prune-p", 1)
		if err != nil {
			t.Fatalf("GetJob failed: %v", err)
		}
		if job == nil {
			break // 已被运行期清理
		}
		if time.Now().After(deadline) {
			t.Fatalf("periodic prune did not remove aged done row within 5s")
		}
		time.Sleep(5 * time.Millisecond)
	}
	// 最新 done 行保留。
	if latest, err := svc.syncStore.LatestJob(ctx, "prune-p"); err != nil || latest == nil || latest.Revision != 2 {
		t.Fatalf("latest done must survive periodic prune, got %+v, %v", latest, err)
	}
}

// mustJobState 直接经 store 读 job（worker 测试的 mustJob 走 svc）。
func mustJobState(t *testing.T, store *AtomicSyncStore, id string, revision int64) *AtomicIndexJob {
	t.Helper()
	job, err := store.GetJob(context.Background(), id, revision)
	if err != nil || job == nil {
		t.Fatalf("GetJob(%s@%d) = %+v, %v", id, revision, job, err)
	}
	return job
}

// waitForWorkerIdle 等待索引 worker 真正收敛：既无 pending job 遗留，也无
// 在途 apply。"无 pending"单独不构成收敛——inline 发布与 worker 重放可对
// 同一 job 产生幂等的重复发布：worker 在 job 已被 inline 标 done 后仍可能
// 正处于 replaceVector 的 Delete/Add 之间（内容幂等、自愈的瞬时空窗），
// 此时采样向量存在性会采到假缺失。读取顺序必须是 pending 在前、inFlight
// 在后：MarkRetry 只发生在在途 apply 内（release 在 goroutine 末尾），
// inFlight==0 时不可能有 pending 正在生成。
func waitForWorkerIdle(t *testing.T, svc *AtomicMemoryService, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		pending, err := svc.syncStore.ListPendingJobs(context.Background(), 1000)
		if err != nil {
			t.Fatalf("ListPendingJobs failed: %v", err)
		}
		svc.indexWorker.mu.Lock()
		inFlight := len(svc.indexWorker.inFlight)
		svc.indexWorker.mu.Unlock()
		if len(pending) == 0 && inFlight == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("worker did not become idle within %s: pending=%d inFlight=%d", timeout, len(pending), inFlight)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// ---------------------------------------------------------------------------
// 并发 update/delete + 故障恢复后，正文、向量、回执三者一致；
// Close 后 worker 全退出（复验）。
// ---------------------------------------------------------------------------

// 生产同款 DSN（WAL+8 连接+busy_timeout 5s）下：每个 ID 一个 goroutine
// 串行执行确定性 mutation 序列（Update/Delete/复活 Add 混合），前半段叠加
// 向量 Add/Delete 故障；故障清除后由 worker 收敛。终态逐 ID 校验：
// 存活 ID 向量内容==正文内容且最新 job done；tombstone ID 无向量且最新
// delete job done；无 pending 遗留；回执 revision 逐 ID 严格递增。
func TestAtomicConcurrentUpdateDeleteRecoveryConsistent(t *testing.T) {
	baseline := runtime.NumGoroutine()
	db, cleanupDB := setupProductionLikeTestDB(t)
	defer cleanupDB()
	vectorStore, cleanupVector := setupSyncTestVectorStore(t)
	defer cleanupVector()
	ctx := context.Background()

	cfg := testAtomicWorkerConfig()
	cfg.MaxAttempts = 1000 // 故障窗口内不触顶：本测试只验收敛，不验上限终态
	svc, err := NewAtomicMemoryService(ctx, db, vectorStore, NewSimpleEmbeddingProvider(32), WithAtomicIndexWorkerConfig(cfg))
	if err != nil {
		t.Fatalf("create service failed: %v", err)
	}

	const idCount = 6
	const rounds = 24
	const faultRounds = 10 // 前 10 轮叠加向量故障，之后清除由 worker 收敛

	type mutationRecord struct {
		revision  int64
		indexSync AtomicIndexSyncState
	}
	receipts := make([][]mutationRecord, idCount)
	var receiptsMu sync.Mutex
	record := func(i int, r AtomicMutationReceipt) {
		receiptsMu.Lock()
		receipts[i] = append(receipts[i], mutationRecord{revision: r.Revision, indexSync: r.IndexSync})
		receiptsMu.Unlock()
	}

	// 种子：全部 ID 存活 rev1。
	for i := 0; i < idCount; i++ {
		id := fmt.Sprintf("cc-%d", i)
		r, err := svc.AddWithReceipt(ctx, newSyncTestMemory(id, "cc-session", fmt.Sprintf("并发一致 种子 %d", i)))
		if err != nil {
			t.Fatalf("seed Add %s failed: %v", id, err)
		}
		record(i, r)
	}
	if _, err := svc.AddWithReceipt(ctx, newSyncTestMemory("cc-gate", "cc-session", "并发一致 门禁种子")); err != nil {
		t.Fatalf("seed Add cc-gate failed: %v", err)
	}

	vectorStore.setAddFault(true)
	vectorStore.setDeleteFault(true)

	// 删除侧 stale 门禁的收敛验证：cc-gate 在故障窗口内"删 rev2（pending）
	// → 复活 rev3（pending）"，之后不再触碰。无门禁时旧 delete job 会误删
	// rev3 向量并永久 pending（下方收敛等待必超时）；有门禁时旧 delete job
	// 被拦截为 stale 终态，rev3 正文/向量/job 三者一致收敛。
	if _, err := svc.DeleteWithReceipt(ctx, "cc-gate"); err != nil {
		t.Fatalf("gate Delete failed: %v", err)
	}
	gateContent := "并发一致 门禁复活"
	gateReceipt, err := svc.AddWithReceipt(ctx, newSyncTestMemory("cc-gate", "cc-session", gateContent))
	if err != nil {
		t.Fatalf("gate resurrect Add failed: %v", err)
	}
	if gateReceipt.Revision != 3 {
		t.Fatalf("gate: expected resurrect revision 3, got %+v", gateReceipt)
	}

	var wg sync.WaitGroup
	for i := 0; i < idCount; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("cc-%d", i)
			deleted := false
			for round := 0; round < rounds; round++ {
				if round == faultRounds {
					vectorStore.setAddFault(false)
					vectorStore.setDeleteFault(false)
				}
				content := fmt.Sprintf("并发一致 id%d 轮次%d", i, round)
				switch {
				case deleted:
					// 复活：同 ID 再 Add（revision 高于墓碑）。
					r, err := svc.AddWithReceipt(ctx, newSyncTestMemory(id, "cc-session", content))
					if err != nil {
						t.Errorf("resurrect Add %s round %d failed: %v", id, round, err)
						return
					}
					record(i, r)
					deleted = false
				case i%2 == 1 && round%4 == 3:
					// 奇数 ID 每 4 轮删除一次。
					r, err := svc.DeleteWithReceipt(ctx, id)
					if err != nil {
						t.Errorf("Delete %s round %d failed: %v", id, round, err)
						return
					}
					record(i, r)
					deleted = true
				default:
					r, err := svc.UpdateWithReceipt(ctx, id, &AtomicMemoryUpdate{Content: &content})
					if err != nil {
						t.Errorf("Update %s round %d failed: %v", id, round, err)
						return
					}
					record(i, r)
				}
			}
		}(i)
	}
	wg.Wait()
	vectorStore.setAddFault(false)
	vectorStore.setDeleteFault(false)

	// 最终确定性动作（主 goroutine 串行，故障已清除）：偶数 ID 最终 Update
	// （存活），奇数 ID 最终 Delete（tombstone）。
	for i := 0; i < idCount; i++ {
		id := fmt.Sprintf("cc-%d", i)
		if i%2 == 0 {
			content := fmt.Sprintf("并发一致 终态 id%d", i)
			r, err := svc.UpdateWithReceipt(ctx, id, &AtomicMemoryUpdate{Content: &content})
			if err != nil {
				t.Fatalf("final Update %s failed: %v", id, err)
			}
			record(i, r)
		} else {
			r, err := svc.DeleteWithReceipt(ctx, id)
			if err != nil {
				t.Fatalf("final Delete %s failed: %v", id, err)
			}
			record(i, r)
		}
	}

	// 等待 worker 真正收敛（见 waitForWorkerIdle）：无 pending 遗留且无在途
	// apply。"无 pending"单独不构成收敛——对已 done job 的幂等重放可能正
	// 处于 replaceVector 的 Delete/Add 之间（瞬时空窗，内容幂等自愈）。
	waitForWorkerIdle(t, svc, 20*time.Second)

	// 回执校验：逐 ID 的 revision 严格递增（每次成功 mutation 恰好 +1），
	// index_sync 三值之一。
	for i := 0; i < idCount; i++ {
		for k, rec := range receipts[i] {
			if rec.revision != int64(k+1) {
				t.Fatalf("cc-%d: receipt revision sequence broken at #%d: got %d, want %d", i, k, rec.revision, k+1)
			}
			switch rec.indexSync {
			case AtomicIndexSyncPending, AtomicIndexSyncSynced, AtomicIndexSyncFailed:
			default:
				t.Fatalf("cc-%d: unknown index_sync %q at #%d", i, rec.indexSync, k)
			}
		}
	}

	// 三一致校验（正文 / 向量 / 回执-job）。
	chunks, err := vectorStore.GetBySessionID(ctx, "cc-session")
	if err != nil {
		t.Fatalf("GetBySessionID failed: %v", err)
	}
	chunkByID := make(map[string]string, len(chunks))
	for _, c := range chunks {
		chunkByID[c.ID] = c.Content
	}
	for i := 0; i < idCount; i++ {
		id := fmt.Sprintf("cc-%d", i)
		revision, deletedFlag := readRevisionDeleted(t, db, id)
		wantRevision := int64(len(receipts[i])) // 种子 rev1 + 每条记录一步
		if revision != wantRevision {
			t.Fatalf("%s: expected final revision %d, got %d", id, wantRevision, revision)
		}
		latest, err := svc.syncStore.LatestJob(ctx, id)
		if err != nil || latest == nil {
			t.Fatalf("%s: no latest job: %+v, %v", id, latest, err)
		}
		if latest.Revision != revision {
			t.Fatalf("%s: latest job revision %d != body revision %d", id, latest.Revision, revision)
		}
		if i%2 == 0 {
			// 存活：正文可见、内容=终态、向量内容一致、最新 upsert job done。
			if deletedFlag != 0 {
				t.Fatalf("%s: expected live row, deleted=%d", id, deletedFlag)
			}
			got, err := svc.GetByID(ctx, id)
			if err != nil {
				t.Fatalf("%s: GetByID failed: %v", id, err)
			}
			wantContent := fmt.Sprintf("并发一致 终态 id%d", i)
			if got.Content != wantContent {
				t.Fatalf("%s: body content %q != %q", id, got.Content, wantContent)
			}
			if chunkByID[id] != wantContent {
				var allJobs string
				rows, qerr := db.Query(`SELECT revision, operation, state, attempts, last_error FROM atomic_index_jobs WHERE memory_id = ? ORDER BY revision`, id)
				if qerr == nil {
					for rows.Next() {
						var rev, op, st, le string
						var att int
						_ = rows.Scan(&rev, &op, &st, &att, &le)
						allJobs += fmt.Sprintf("[%s %s %s att=%d %s] ", rev, op, st, att, le)
					}
					rows.Close()
				}
				t.Fatalf("%s: vector content %q diverged from body %q; jobs=%s qerr=%v", id, chunkByID[id], wantContent, allJobs, qerr)
			}
			if latest.Operation != AtomicIndexOpUpsert || latest.State != AtomicIndexJobDone {
				t.Fatalf("%s: expected done upsert job at latest revision, got %+v", id, latest)
			}
		} else {
			// tombstone：正文不可见、无向量、最新 delete job done。
			if deletedFlag != 1 {
				t.Fatalf("%s: expected tombstone, deleted=%d", id, deletedFlag)
			}
			if _, err := svc.GetByID(ctx, id); !errors.Is(err, ErrAtomicMemoryNotFound) {
				t.Fatalf("%s: expected not found, got %v", id, err)
			}
			if _, hasVector := chunkByID[id]; hasVector {
				t.Fatalf("%s: tombstoned memory must have no vector, got %q", id, chunkByID[id])
			}
			if latest.Operation != AtomicIndexOpDelete || latest.State != AtomicIndexJobDone {
				t.Fatalf("%s: expected done delete job at latest revision, got %+v", id, latest)
			}
		}
	}

	// cc-gate 终态：存活 rev3，向量=复活内容，rev3 upsert done，rev2 delete
	// 被删除侧门禁拦截为 stale 终态（旧删除意图未误删新版本索引）。
	if revision, deletedFlag := readRevisionDeleted(t, db, "cc-gate"); revision != 3 || deletedFlag != 0 {
		t.Fatalf("cc-gate: expected live rev3, got revision=%d deleted=%d", revision, deletedFlag)
	}
	if chunkByID["cc-gate"] != gateContent {
		t.Fatalf("cc-gate: vector must carry resurrected content, got %q", chunkByID["cc-gate"])
	}
	gateDeleteJob := mustJob(t, svc, "cc-gate", 2)
	if gateDeleteJob.State != AtomicIndexJobFailed || !strings.Contains(gateDeleteJob.LastError, atomicIndexFailStaleSuperseded) {
		t.Fatalf("cc-gate: stale delete job must be gated terminal, got %+v", gateDeleteJob)
	}
	gateUpsertJob := mustJob(t, svc, "cc-gate", 3)
	if gateUpsertJob.State != AtomicIndexJobDone || gateUpsertJob.Operation != AtomicIndexOpUpsert {
		t.Fatalf("cc-gate: resurrect upsert must be done, got %+v", gateUpsertJob)
	}

	// 收敛后检索不含未同步原因。
	if _, report, err := svc.SearchWithReport(ctx, "并发一致", &AtomicMemorySearchOptions{Limit: 10}); err != nil {
		t.Fatalf("SearchWithReport failed: %v", err)
	} else if report.IndexPendingJobs != 0 || report.IndexFailedJobs != 0 {
		t.Fatalf("converged state must have no unsynced jobs, report=%+v", report)
	}
	// 存活 ID 可检索、tombstone ID 不出现。
	results, err := svc.Search(ctx, "并发一致", &AtomicMemorySearchOptions{Limit: 20})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	found := map[string]bool{}
	for _, mem := range results {
		found[mem.ID] = true
	}
	for i := 0; i < idCount; i++ {
		id := fmt.Sprintf("cc-%d", i)
		if i%2 == 0 && !found[id] {
			t.Fatalf("%s: live memory must be searchable after recovery; results=%v chunks=%v", id, pageIDs(results), chunkByID)
		}
		if i%2 == 1 && found[id] {
			t.Fatalf("%s: tombstoned memory must not be searchable", id)
		}
	}

	// Close 后 worker 全退出（复验）。
	if err := svc.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	waitForGoroutinesAtMost(t, baseline, 5*time.Second)
}
