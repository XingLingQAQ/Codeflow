package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/dbx"
	sqlite "modernc.org/sqlite"
)

// ---------------------------------------------------------------------------
// T13.02.fix1：复审复现场景回归测试
//
// 缺陷 1：Delete 的延迟事务升级（WAL deferred tx 先读后写，另一连接在读写
// 之间提交时升级 UPDATE 立即 SQLITE_BUSY_SNAPSHOT，busy handler 不生效）。
// 缺陷 2：提交后发布无按 ID 串行（A 过门禁后被挂起、B 完成 N+1 后 A 用旧
// 内容覆盖向量库并把 N 标 done，正文/索引永久分叉）。
// ---------------------------------------------------------------------------

// setupProductionLikeTestDB 按生产 runtime.go 的 DSN 特征开正文库：文件库、
// WAL、8 连接、busy_timeout 5000ms、synchronous NORMAL、foreign_keys off。
// 与生产唯一差异是路径在临时目录。
func setupProductionLikeTestDB(t *testing.T) (*sql.DB, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "atomic_fix1_repro")
	if err != nil {
		t.Fatalf("create temp dir failed: %v", err)
	}
	dbPath := filepath.Join(tmpDir, "atomic_prod_like.db")
	db, err := dbx.Open(dbPath,
		dbx.WithSynchronous("NORMAL"),
		dbx.WithForeignKeys(false),
		dbx.WithMaxOpenConns(8),
		dbx.WithBusyTimeout(5*time.Second),
	)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("open production-like sqlite failed: %v", err)
	}
	cleanup := func() {
		_ = db.Close()
		_ = os.RemoveAll(tmpDir)
	}
	return db, cleanup
}

// isSQLiteBusyErr 识别 SQLITE_BUSY 族错误（含 517 SQLITE_BUSY_SNAPSHOT：
// 扩展码掩低字节即主码 5）。
func isSQLiteBusyErr(err error) bool {
	var serr *sqlite.Error
	if errors.As(err, &serr) {
		return serr.Code()&0xff == 5
	}
	return false
}

// readRevisionDeletedLoose 与 readRevisionDeleted 相同，但只在压力
// goroutine 内使用：读取失败静默返回不存在，不向 testing.T 报错（
// t.Fatalf 不允许跨 goroutine）。
func readRevisionDeletedLoose(db *sql.DB, id string) (int64, int) {
	var (
		revision int64
		deleted  int
	)
	err := db.QueryRow(`SELECT revision, deleted FROM atomic_memories WHERE id = ?`, id).Scan(&revision, &deleted)
	if err != nil {
		return 0, -1
	}
	return revision, deleted
}

// TestAtomicDeleteConcurrentWithJobPressureNoBusy 是缺陷 1 的复审复现场景
// 回归：生产同款 DSN（WAL + 8 连接 + busy_timeout 5000）下，多个 ID 并发
// Delete，同时另一个 goroutine 以 worker 节奏对 jobs 表施加 MarkRetry/
// MarkDone 写压力。修复前 Delete 的 deferred 事务（先 SELECT 后 UPDATE）在
// WAL 快照失效时立即 SQLITE_BUSY_SNAPSHOT（busy handler 不生效），表现为
// 间歇 500；修复后 Delete 首条语句即写（事务立即升级），冲突走 busy
// handler 等待，全程零 busy 错误、零非幂等失败。
func TestAtomicDeleteConcurrentWithJobPressureNoBusy(t *testing.T) {
	db, cleanupDB := setupProductionLikeTestDB(t)
	defer cleanupDB()
	vectorStore, cleanupVector := setupSyncTestVectorStore(t)
	defer cleanupVector()
	ctx := context.Background()

	// worker 关闭：Delete 路径的内联发布成功标 done；压力 goroutine 的
	// MarkRetry 把 done job 留 pending 后由压力循环自行标回 done，复现的
	// 是"并发写方在读写窗口内提交"这一锁冲突场景本身。
	svc, err := NewAtomicMemoryService(ctx, db, vectorStore, NewSimpleEmbeddingProvider(32), WithAtomicIndexWorkerDisabled())
	if err != nil {
		t.Fatalf("create service failed: %v", err)
	}

	const ids = 3
	const rounds = 120

	for i := 0; i < ids; i++ {
		id := fmt.Sprintf("busy-%d", i)
		if err := svc.Add(ctx, newSyncTestMemory(id, "session-busy", fmt.Sprintf("并发删除场景 %d", i))); err != nil {
			t.Fatalf("seed Add %s failed: %v", id, err)
		}
	}

	stopPressure := make(chan struct{})
	var pressureWG sync.WaitGroup
	var busyErrors atomic.Int64
	var otherErrors atomic.Int64

	// 写压力：worker poll 同款 MarkRetry/MarkDone 单语句写，每次操作都独立
	// 提交，制造"Delete 读写窗口内另一连接提交"的 WAL 快照失效条件。
	pressureWG.Add(1)
	go func() {
		defer pressureWG.Done()
		for {
			select {
			case <-stopPressure:
				return
			default:
			}
			for i := 0; i < ids; i++ {
				id := fmt.Sprintf("busy-%d", i)
				rev, deleted := readRevisionDeletedLoose(db, id)
				if rev <= 0 {
					continue
				}
				if deleted == 0 {
					if err := svc.syncStore.MarkRetry(ctx, id, rev, errors.New("pressure retry")); err == nil {
						_ = svc.syncStore.MarkDone(ctx, id, rev)
					}
				} else if err := svc.syncStore.MarkDone(ctx, id, rev); err != nil && !errors.Is(err, ErrAtomicIndexJobNotFound) {
					otherErrors.Add(1)
				}
			}
		}
	}()

	var deleteWG sync.WaitGroup
	for i := 0; i < ids; i++ {
		deleteWG.Add(1)
		go func(i int) {
			defer deleteWG.Done()
			id := fmt.Sprintf("busy-%d", i)
			for r := 0; r < rounds; r++ {
				if r > 0 {
					// 复活 tombstone（同 ID 再 Add）：revision 继续前进，
					// 为下一轮 Delete 提供存活行。
					if err := svc.Add(ctx, newSyncTestMemory(id, "session-busy", fmt.Sprintf("复活轮次 %d", r))); err != nil {
						if isSQLiteBusyErr(err) {
							busyErrors.Add(1)
						} else {
							otherErrors.Add(1)
						}
						t.Errorf("re-Add %s round %d failed: %v", id, r, err)
						return
					}
				}
				err := svc.Delete(ctx, id)
				if err == nil {
					continue
				}
				if isSQLiteBusyErr(err) {
					busyErrors.Add(1)
				} else {
					otherErrors.Add(1)
				}
				t.Errorf("Delete %s round %d failed: %v", id, r, err)
				return
			}
		}(i)
	}
	deleteWG.Wait()
	close(stopPressure)
	pressureWG.Wait()

	if n := busyErrors.Load(); n != 0 {
		t.Fatalf("expected zero SQLITE_BUSY-class errors under production-like DSN, got %d", n)
	}
	if n := otherErrors.Load(); n != 0 {
		t.Fatalf("expected zero unexpected errors, got %d", n)
	}

	// 终态一致：每个 ID 的最终状态是 tombstone（最后一轮 Delete 生效），
	// revision 恰好前进到 2*rounds（Add rev1 + 首轮 delete + 其余每轮
	// 复活/删除各一步）。
	for i := 0; i < ids; i++ {
		id := fmt.Sprintf("busy-%d", i)
		revision, deleted := readRevisionDeleted(t, db, id)
		wantRevision := int64(2 * rounds)
		if deleted != 1 || revision != wantRevision {
			t.Fatalf("%s: expected tombstone at revision %d, got revision=%d deleted=%d", id, wantRevision, revision, deleted)
		}
	}
}

// gateVectorStore 在 faultVectorStore 之上为指定 chunk ID 的首次成功 Add
// 提供可阻塞闸门：hook 触发时先关闭 entered channel（通知测试 A 已在
// 发布路径内），再阻塞等待 release channel 关闭。仅测试使用。
type gateVectorStore struct {
	*faultVectorStore

	mu      sync.Mutex
	pending map[string]chan struct{} // chunk ID -> 触发信号（armed）
	armed   map[string]chan struct{} // chunk ID -> 放行闸门
}

func newGateVectorStore(fault *faultVectorStore) *gateVectorStore {
	return &gateVectorStore{
		faultVectorStore: fault,
		pending:          map[string]chan struct{}{},
		armed:            map[string]chan struct{}{},
	}
}

// armOnNextAdd 预置闸门：下一次携带 chunkID 的成功 Add 将阻塞，直到返回的
// release 被 close；entered 在 Add 真正进入（已过故障注入与门禁）时关闭。
func (g *gateVectorStore) armOnNextAdd(chunkID string) (entered <-chan struct{}, release chan<- struct{}) {
	g.mu.Lock()
	defer g.mu.Unlock()
	ent := make(chan struct{})
	rel := make(chan struct{})
	g.pending[chunkID] = ent
	g.armed[chunkID] = rel
	return ent, rel
}

func (g *gateVectorStore) Add(ctx context.Context, chunks []DocumentChunk) error {
	var (
		ent chan struct{}
		rel chan struct{}
	)
	g.mu.Lock()
	for _, c := range chunks {
		if e, ok := g.pending[c.ID]; ok {
			ent = e
			rel = g.armed[c.ID]
			delete(g.pending, c.ID)
			delete(g.armed, c.ID)
			break
		}
	}
	g.mu.Unlock()
	if ent != nil {
		close(ent)
		<-rel
	}
	return g.faultVectorStore.Add(ctx, chunks)
}

// TestAtomicPublishStaleUpdateCannotOverwriteNewer 是缺陷 2 的 A/B 交错
// 回归：同一 ID 两个并发 Update，A（rev2）在向量写入点被闸门挂起，B 完整
// 提交 rev3；再放行 A。修复后 B 的发布被按 ID 串行锁挡在 A 之后，A 恢复后
// 用旧内容写向量但在 MarkDone 前复查发现已落后（不标 done），随后 B 的发布
// 用 rev3 内容覆盖——索引最终等于 rev3 内容、rev2 job 不被标 done。
//
// 修复前（无锁无复查）：B 在 A 挂起期间完成发布（job3 done、向量为 rev3），
// A 放行后用 rev2 旧内容覆盖向量并 MarkDone(rev2)——正文 rev3 / 向量 rev2、
// 无 pending job，永久分叉（E-02 要消除的形态）。舞蹈说明：revision==3 断言
// B 已提交（修复前后都成立）；job3 的有界等待只在修复前会成功（修复后 B 的
// 发布在锁外等待 A），两种世界线下都随后放行 A，不依赖睡眠时序正确性。
func TestAtomicPublishStaleUpdateCannotOverwriteNewer(t *testing.T) {
	db, _, cleanupDB := setupAtomicWorkerTestDB(t) // busy_timeout=5s：并发 mutation 需要忙等预算
	defer cleanupDB()
	fault, cleanupVector := setupSyncTestVectorStore(t)
	defer cleanupVector()
	gated := newGateVectorStore(fault)
	ctx := context.Background()

	svc, err := NewAtomicMemoryService(ctx, db, gated, NewSimpleEmbeddingProvider(32), WithAtomicIndexWorkerDisabled())
	if err != nil {
		t.Fatalf("create service failed: %v", err)
	}

	if err := svc.Add(ctx, newSyncTestMemory("ab-1", "session-1", "版本一")); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	entered, release := gated.armOnNextAdd("ab-1")

	// A：rev2 更新（旧内容），发布路径在向量写入点被挂起。
	contentA := "版本二（A 的旧内容）"
	aDone := make(chan error, 1)
	go func() {
		aDone <- svc.Update(ctx, "ab-1", &AtomicMemoryUpdate{Content: &contentA})
	}()

	// 等 A 进入发布路径（已过 rev2 门禁，正拿着旧内容准备写向量）。
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatalf("A did not enter gated vector add")
	}

	// B：rev3 更新（新内容）。修复后 B 的发布会等 A 的按 ID 串行锁，因此只
	// 断言到"B 已提交"（revision==3），两种世界线都成立。
	contentB := "版本三（B 的新内容）"
	bDone := make(chan error, 1)
	go func() {
		bDone <- svc.Update(ctx, "ab-1", &AtomicMemoryUpdate{Content: &contentB})
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		if revision, _ := readRevisionDeleted(t, db, "ab-1"); revision == 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("B did not commit rev3 within 5s")
		}
		time.Sleep(5 * time.Millisecond)
	}
	// 修复前：B 的发布无锁，job3 很快 done；修复后：B 在锁外等 A，等待超时
	// 是直接信号。两种情形都在等待结束后放行 A。
	job3DoneDeadline := time.Now().Add(800 * time.Millisecond)
	for time.Now().Before(job3DoneDeadline) {
		if job, err := svc.syncStore.GetJob(ctx, "ab-1", 3); err == nil && job != nil && job.State == AtomicIndexJobDone {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// 放行 A；两个 Update 都应返回成功（正文事实各自提交）。
	close(release)
	if err := <-aDone; err != nil {
		t.Fatalf("A update failed: %v", err)
	}
	if err := <-bDone; err != nil {
		t.Fatalf("B update failed: %v", err)
	}

	// 正文权威事实：rev3、B 的内容。
	got, err := svc.GetByID(ctx, "ab-1")
	if err != nil || got.Content != contentB {
		t.Fatalf("GetByID = %+v, %v", got, err)
	}
	if revision, deleted := readRevisionDeleted(t, svc.db, "ab-1"); revision != 3 || deleted != 0 {
		t.Fatalf("expected revision=3 deleted=0, got revision=%d deleted=%d", revision, deleted)
	}

	// 无分叉核心断言一：索引内容与正文一致（rev3 / B 的内容）。直读向量行
	// 内容（不经相似度语义），唯一 chunk 必须是 B 的内容。
	chunks, err := gated.GetBySessionID(ctx, "session-1")
	if err != nil {
		t.Fatalf("GetBySessionID failed: %v", err)
	}
	if len(chunks) != 1 || chunks[0].ID != "ab-1" || chunks[0].Content != contentB {
		t.Fatalf("index diverged from body: chunks=%+v, want exactly one ab-1 chunk with rev3 content", chunks)
	}
	results, err := svc.Search(ctx, "版本三", &AtomicMemorySearchOptions{Limit: 10})
	if err != nil || len(results) != 1 || results[0].ID != "ab-1" || results[0].Content != contentB {
		t.Fatalf("Search = %+v, %v (index must carry rev3 content)", results, err)
	}

	// 无分叉核心断言二：rev2 job 不得被 A 标 done（stale 发布拦截后留
	// pending，终态由 worker 对账，而不是错误完成）。
	job2 := mustJob(t, svc, "ab-1", 2)
	if job2.State == AtomicIndexJobDone {
		t.Fatalf("stale rev2 job must not be marked done by A, got %+v", job2)
	}
	// rev3 job 已发布完成。
	job3 := mustJob(t, svc, "ab-1", 3)
	if job3.State != AtomicIndexJobDone || job3.Operation != AtomicIndexOpUpsert {
		t.Fatalf("expected done upsert job rev3, got %+v", job3)
	}
}
