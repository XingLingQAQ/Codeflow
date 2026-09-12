package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/dbx"
)

// ---------------------------------------------------------------------------
// fixtures
// ---------------------------------------------------------------------------

// fakeClock 可手动推进的时间源，用于断言 updated_at 的精确取值。
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Unix(1700000000, 0)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// errInjectedCommit 是 commit fault fixture 在提交点返回的标记错误。
var errInjectedCommit = errors.New("injected commit fault")

// commitFaultDB 包装真实 *sql.DB，仅在事务提交点按注入返回错误。所有
// 语句仍在真实文件 SQLite 上执行；注入失败时底层事务真实回滚，等价于
// 进程在 commit 点崩溃后事务未生效的场景。SQLite 本身不被 mock。
type commitFaultDB struct {
	db            *sql.DB
	mu            sync.Mutex
	pendingFaults int
}

func (f *commitFaultDB) failNextCommit() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pendingFaults++
}

func (f *commitFaultDB) consumeFault() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pendingFaults == 0 {
		return false
	}
	f.pendingFaults--
	return true
}

func (f *commitFaultDB) beginTx(ctx context.Context) (*commitFaultTx, error) {
	tx, err := f.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &commitFaultTx{tx: tx, owner: f}, nil
}

// commitFaultTx 实现 SyncExecutor + Commit/Rollback，只有 Commit 被注入。
type commitFaultTx struct {
	tx    *sql.Tx
	owner *commitFaultDB
}

func (t *commitFaultTx) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return t.tx.ExecContext(ctx, query, args...)
}

func (t *commitFaultTx) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	return t.tx.QueryContext(ctx, query, args...)
}

func (t *commitFaultTx) QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row {
	return t.tx.QueryRowContext(ctx, query, args...)
}

func (t *commitFaultTx) Commit() error {
	if t.owner.consumeFault() {
		_ = t.tx.Rollback()
		return errInjectedCommit
	}
	return t.tx.Commit()
}

func (t *commitFaultTx) Rollback() error {
	return t.tx.Rollback()
}

// errInjectedVector 是向量故障 fixture 的标记错误。
var errInjectedVector = errors.New("injected vector store fault")

// faultVectorStore 包装真实 SQLiteVectorStore（真实文件库），按注入对
// Add/Delete 返回错误，其余方法直接委托。仅测试使用。
type faultVectorStore struct {
	inner     *SQLiteVectorStore
	mu        sync.Mutex
	addErr    error
	deleteErr error
}

func (f *faultVectorStore) setAddFault(on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if on {
		f.addErr = errInjectedVector
	} else {
		f.addErr = nil
	}
}

func (f *faultVectorStore) setDeleteFault(on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if on {
		f.deleteErr = errInjectedVector
	} else {
		f.deleteErr = nil
	}
}

func (f *faultVectorStore) Add(ctx context.Context, chunks []DocumentChunk) error {
	f.mu.Lock()
	err := f.addErr
	f.mu.Unlock()
	if err != nil {
		return err
	}
	return f.inner.Add(ctx, chunks)
}

func (f *faultVectorStore) Delete(ctx context.Context, ids []string) error {
	f.mu.Lock()
	err := f.deleteErr
	f.mu.Unlock()
	if err != nil {
		return err
	}
	return f.inner.Delete(ctx, ids)
}

func (f *faultVectorStore) Search(ctx context.Context, query string, opts *VectorSearchOptions) ([]VectorSearchResult, error) {
	return f.inner.Search(ctx, query, opts)
}

func (f *faultVectorStore) Clear(ctx context.Context) error { return f.inner.Clear(ctx) }

func (f *faultVectorStore) GetBySessionID(ctx context.Context, sessionID string) ([]DocumentChunk, error) {
	return f.inner.GetBySessionID(ctx, sessionID)
}

func (f *faultVectorStore) GetByGitCommit(ctx context.Context, commitHash string) ([]DocumentChunk, error) {
	return f.inner.GetByGitCommit(ctx, commitHash)
}

func (f *faultVectorStore) Count(ctx context.Context) (int, error) { return f.inner.Count(ctx) }

func (f *faultVectorStore) GetCollectionInfo(ctx context.Context) (*CollectionInfo, error) {
	return f.inner.GetCollectionInfo(ctx)
}

func (f *faultVectorStore) Close() error { return f.inner.Close() }

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func setupAtomicSyncTestDB(t *testing.T) (*sql.DB, string, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "atomic_sync_test")
	if err != nil {
		t.Fatalf("create temp dir failed: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "atomic_sync.db")
	db, err := dbx.Open(dbPath, dbx.WithWAL(false), dbx.WithForeignKeys(false), dbx.WithBusyTimeout(0))
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

func reopenAtomicSyncTestDB(t *testing.T, dbPath string) *sql.DB {
	t.Helper()
	db, err := dbx.Open(dbPath, dbx.WithWAL(false), dbx.WithForeignKeys(false), dbx.WithBusyTimeout(0))
	if err != nil {
		t.Fatalf("reopen sqlite failed: %v", err)
	}
	return db
}

func setupSyncTestVectorStore(t *testing.T) (*faultVectorStore, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "atomic_sync_vector_test")
	if err != nil {
		t.Fatalf("create temp vector dir failed: %v", err)
	}
	inner := NewSQLiteVectorStore(&VectorStoreConfig{
		CollectionName: "atomic_sync_test",
		DBPath:         filepath.Join(tmpDir, "vectors.db"),
		WALMode:        true,
	}, NewSimpleEmbeddingProvider(32))
	fault := &faultVectorStore{inner: inner}
	cleanup := func() {
		_ = fault.Close()
		_ = os.RemoveAll(tmpDir)
	}
	return fault, cleanup
}

// legacyAtomicMemoriesSchema 手工构造的旧版正文库（迁移前的生产 schema：
// 有 tier/heat/surprise，无 revision/deleted，无 atomic_index_jobs）。
// 注：dedca13 引入正文表、4cfab56 才加 tier/heat/surprise；更老的无 tier
// schema 会先被 EnsureAtomicMemorySchema 的索引语句挡住（既有缺陷，
// 本步不改该文件），因此旧库 fixture 以现行生产 schema 为准。
const legacyAtomicMemoriesSchema = `
CREATE TABLE atomic_memories (
  id TEXT PRIMARY KEY,
  timestamp INTEGER NOT NULL,
  content TEXT NOT NULL,
  tags_json TEXT NOT NULL DEFAULT '[]',
  session_id TEXT NOT NULL,
  folder_id TEXT,
  source TEXT NOT NULL CHECK (source IN ('user', 'assistant', 'system')),
  importance REAL NOT NULL CHECK (importance >= 0 AND importance <= 1),
  embedding_json TEXT,
  vector_dim INTEGER NOT NULL DEFAULT 0,
  tier TEXT NOT NULL DEFAULT 'hot' CHECK (tier IN ('hot', 'warm', 'cold')),
  heat REAL NOT NULL DEFAULT 1.0,
  surprise REAL NOT NULL DEFAULT 0.5,
  created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
  updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);
CREATE INDEX idx_atomic_memories_session_time ON atomic_memories(session_id, timestamp DESC);
`

func insertLegacyMemory(t *testing.T, db *sql.DB, id, sessionID, content string) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO atomic_memories (id, timestamp, content, tags_json, session_id, folder_id, source, importance, embedding_json, vector_dim)
		VALUES (?, ?, ?, '["legacy"]', ?, NULL, 'user', 0.5, NULL, 0)
	`, id, 100, content, sessionID)
	if err != nil {
		t.Fatalf("insert legacy memory %s failed: %v", id, err)
	}
}

// insertSyncTestMemory 在迁移后的表上插入正文行（revision/deleted 走默认值）。
func insertSyncTestMemory(t *testing.T, db *sql.DB, id, sessionID, content string) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO atomic_memories (id, timestamp, content, tags_json, session_id, folder_id, source, importance, embedding_json, vector_dim, tier, heat, surprise)
		VALUES (?, ?, ?, '[]', ?, NULL, 'user', 0.5, NULL, 0, 'hot', 1.0, 0.5)
	`, id, 100, content, sessionID)
	if err != nil {
		t.Fatalf("insert memory %s failed: %v", id, err)
	}
}

func readUserVersion(t *testing.T, db *sql.DB) int {
	t.Helper()
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read user_version failed: %v", err)
	}
	return version
}

func tableColumns(t *testing.T, db *sql.DB, table string) map[string]bool {
	t.Helper()
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		t.Fatalf("read table_info(%s) failed: %v", table, err)
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var (
			cid     int
			name    string
			typ     string
			notnull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan table_info(%s) failed: %v", table, err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate table_info(%s) failed: %v", table, err)
	}
	return columns
}

func countJobs(t *testing.T, db *sql.DB, memoryID string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM atomic_index_jobs WHERE memory_id = ?", memoryID).Scan(&n); err != nil {
		t.Fatalf("count jobs for %s failed: %v", memoryID, err)
	}
	return n
}

func countVectorsForID(t *testing.T, store *faultVectorStore, id string) int {
	t.Helper()
	chunks, err := store.GetBySessionID(context.Background(), "session-legacy")
	if err != nil {
		t.Fatalf("get vectors by session failed: %v", err)
	}
	n := 0
	for _, c := range chunks {
		if c.ID == id {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// 迁移测试
// ---------------------------------------------------------------------------

func TestAtomicSyncStoreMigrateEmptyDB(t *testing.T) {
	db, dbPath, cleanup := setupAtomicSyncTestDB(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := NewAtomicSyncStore(ctx, nil); err == nil {
		t.Fatalf("expected error when db is nil")
	}

	store, err := NewAtomicSyncStore(ctx, db)
	if err != nil {
		t.Fatalf("NewAtomicSyncStore on empty db failed: %v", err)
	}
	_ = store

	if v := readUserVersion(t, db); v != atomicSyncSchemaVersion {
		t.Fatalf("expected user_version=%d, got %d", atomicSyncSchemaVersion, v)
	}

	cols := tableColumns(t, db, "atomic_memories")
	for _, want := range []string{"id", "timestamp", "content", "tier", "heat", "surprise", "revision", "deleted"} {
		if !cols[want] {
			t.Fatalf("expected column %q on atomic_memories, columns: %v", want, cols)
		}
	}
	jobCols := tableColumns(t, db, "atomic_index_jobs")
	for _, want := range []string{"memory_id", "revision", "operation", "state", "attempts", "last_error", "updated_at"} {
		if !jobCols[want] {
			t.Fatalf("expected column %q on atomic_index_jobs, columns: %v", want, jobCols)
		}
	}

	// 幂等：同一 handle 上重复迁移不报错、不重复加列。
	if err := MigrateAtomicSyncSchema(ctx, db); err != nil {
		t.Fatalf("second migration run failed: %v", err)
	}

	// 重开后迁移仍然幂等。
	if err := db.Close(); err != nil {
		t.Fatalf("close db failed: %v", err)
	}
	db = reopenAtomicSyncTestDB(t, dbPath)
	if err := MigrateAtomicSyncSchema(ctx, db); err != nil {
		t.Fatalf("migration after reopen failed: %v", err)
	}
	if v := readUserVersion(t, db); v != atomicSyncSchemaVersion {
		t.Fatalf("expected user_version=%d after reopen, got %d", atomicSyncSchemaVersion, v)
	}
	if cols := tableColumns(t, db, "atomic_memories"); !cols["revision"] || !cols["deleted"] {
		t.Fatalf("expected revision/deleted columns after reopen, columns: %v", cols)
	}
}

func TestAtomicSyncStoreMigrateLegacyDB(t *testing.T) {
	db, _, cleanup := setupAtomicSyncTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// 手工造旧库：迁移前 schema + 既有正文行。
	if _, err := db.Exec(legacyAtomicMemoriesSchema); err != nil {
		t.Fatalf("create legacy schema failed: %v", err)
	}
	insertLegacyMemory(t, db, "legacy-1", "session-legacy", "旧的正文内容一")
	insertLegacyMemory(t, db, "legacy-2", "session-legacy", "旧的正文内容二")

	// 既有向量（独立文件库）与正文同 ID。
	vectorStore, cleanupVector := setupSyncTestVectorStore(t)
	defer cleanupVector()
	if err := vectorStore.Add(ctx, []DocumentChunk{
		{ID: "legacy-1", Content: "旧的正文内容一", Metadata: ChunkMetadata{SessionID: "session-legacy", Timestamp: 100, Source: SourceUser}},
	}); err != nil {
		t.Fatalf("add legacy vector failed: %v", err)
	}

	if _, err := NewAtomicSyncStore(ctx, db); err != nil {
		t.Fatalf("migrate legacy db failed: %v", err)
	}
	if v := readUserVersion(t, db); v != atomicSyncSchemaVersion {
		t.Fatalf("expected user_version=%d, got %d", atomicSyncSchemaVersion, v)
	}

	// 既有正文 ID/内容保留，revision/deleted 取默认值。
	rows, err := db.Query(`SELECT id, content, revision, deleted FROM atomic_memories ORDER BY id`)
	if err != nil {
		t.Fatalf("query migrated rows failed: %v", err)
	}
	type migratedRow struct {
		id       string
		content  string
		revision int64
		deleted  int
	}
	var got []migratedRow
	for rows.Next() {
		var r migratedRow
		if err := rows.Scan(&r.id, &r.content, &r.revision, &r.deleted); err != nil {
			t.Fatalf("scan migrated row failed: %v", err)
		}
		got = append(got, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate migrated rows failed: %v", err)
	}
	rows.Close()
	if len(got) != 2 {
		t.Fatalf("expected 2 migrated rows, got %d", len(got))
	}
	if got[0].id != "legacy-1" || got[0].content != "旧的正文内容一" || got[0].revision != 1 || got[0].deleted != 0 {
		t.Fatalf("unexpected migrated row: %+v", got[0])
	}
	if got[1].id != "legacy-2" || got[1].revision != 1 || got[1].deleted != 0 {
		t.Fatalf("unexpected migrated row: %+v", got[1])
	}

	// 既有向量 ID 不受正文库迁移影响。
	if n := countVectorsForID(t, vectorStore, "legacy-1"); n != 1 {
		t.Fatalf("expected legacy vector preserved, got %d", n)
	}

	// 幂等重入。
	if err := MigrateAtomicSyncSchema(ctx, db); err != nil {
		t.Fatalf("second migration run on legacy db failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 任务存储测试
// ---------------------------------------------------------------------------

func TestAtomicSyncStoreEnqueueAndReopen(t *testing.T) {
	db, dbPath, cleanup := setupAtomicSyncTestDB(t)
	defer cleanup()

	ctx := context.Background()
	clock := newFakeClock()
	store, err := NewAtomicSyncStore(ctx, db, WithAtomicSyncClock(clock.Now))
	if err != nil {
		t.Fatalf("NewAtomicSyncStore failed: %v", err)
	}

	if err := store.EnqueueUpsert(ctx, "mem-a", 1); err != nil {
		t.Fatalf("EnqueueUpsert failed: %v", err)
	}
	clock.Advance(5 * time.Second)
	if err := store.EnqueueDelete(ctx, "mem-b", 2); err != nil {
		t.Fatalf("EnqueueDelete failed: %v", err)
	}

	job, err := store.GetJob(ctx, "mem-a", 1)
	if err != nil || job == nil {
		t.Fatalf("GetJob(mem-a@1) = %+v, %v", job, err)
	}
	if job.Operation != AtomicIndexOpUpsert || job.State != AtomicIndexJobPending || job.Attempts != 0 || job.LastError != "" || job.UpdatedAt != 1700000000 {
		t.Fatalf("unexpected job fields: %+v", job)
	}
	job, err = store.GetJob(ctx, "mem-b", 2)
	if err != nil || job == nil {
		t.Fatalf("GetJob(mem-b@2) = %+v, %v", job, err)
	}
	if job.Operation != AtomicIndexOpDelete || job.UpdatedAt != 1700000005 {
		t.Fatalf("unexpected job fields: %+v", job)
	}

	// 关闭重开后任务仍在（持久化），且迁移幂等。
	if err := db.Close(); err != nil {
		t.Fatalf("close db failed: %v", err)
	}
	db = reopenAtomicSyncTestDB(t, dbPath)
	clock = newFakeClock()
	store, err = NewAtomicSyncStore(ctx, db, WithAtomicSyncClock(clock.Now))
	if err != nil {
		t.Fatalf("NewAtomicSyncStore after reopen failed: %v", err)
	}
	job, err = store.GetJob(ctx, "mem-a", 1)
	if err != nil || job == nil {
		t.Fatalf("GetJob(mem-a@1) after reopen = %+v, %v", job, err)
	}
	if job.State != AtomicIndexJobPending || job.Operation != AtomicIndexOpUpsert || job.UpdatedAt != 1700000000 {
		t.Fatalf("job not persisted across reopen: %+v", job)
	}
	job, err = store.GetJob(ctx, "mem-b", 2)
	if err != nil || job == nil || job.Operation != AtomicIndexOpDelete {
		t.Fatalf("GetJob(mem-b@2) after reopen = %+v, %v", job, err)
	}
}

func TestAtomicSyncStoreDuplicateEnqueue(t *testing.T) {
	db, _, cleanup := setupAtomicSyncTestDB(t)
	defer cleanup()

	ctx := context.Background()
	clock := newFakeClock()
	store, err := NewAtomicSyncStore(ctx, db, WithAtomicSyncClock(clock.Now))
	if err != nil {
		t.Fatalf("NewAtomicSyncStore failed: %v", err)
	}

	for i := 0; i < 3; i++ {
		if err := store.EnqueueUpsert(ctx, "mem-a", 1); err != nil {
			t.Fatalf("EnqueueUpsert #%d failed: %v", i, err)
		}
	}
	if n := countJobs(t, db, "mem-a"); n != 1 {
		t.Fatalf("expected 1 deduplicated job, got %d", n)
	}

	// 已有失败尝试的记录不被重复 enqueue 重置。
	if err := store.MarkRetry(ctx, "mem-a", 1, errors.New("vector write boom")); err != nil {
		t.Fatalf("MarkRetry failed: %v", err)
	}
	if err := store.EnqueueUpsert(ctx, "mem-a", 1); err != nil {
		t.Fatalf("re-enqueue failed: %v", err)
	}
	job, err := store.GetJob(ctx, "mem-a", 1)
	if err != nil || job == nil {
		t.Fatalf("GetJob failed: %v", err)
	}
	if job.Attempts != 1 || job.LastError != "vector write boom" || job.State != AtomicIndexJobPending {
		t.Fatalf("duplicate enqueue reset the job: %+v", job)
	}

	// 不同 revision 是新记录。
	if err := store.EnqueueUpsert(ctx, "mem-a", 2); err != nil {
		t.Fatalf("EnqueueUpsert rev2 failed: %v", err)
	}
	if n := countJobs(t, db, "mem-a"); n != 2 {
		t.Fatalf("expected 2 jobs for distinct revisions, got %d", n)
	}
}

func TestAtomicSyncStoreNextPendingJobSerial(t *testing.T) {
	db, _, cleanup := setupAtomicSyncTestDB(t)
	defer cleanup()

	ctx := context.Background()
	clock := newFakeClock()
	store, err := NewAtomicSyncStore(ctx, db, WithAtomicSyncClock(clock.Now))
	if err != nil {
		t.Fatalf("NewAtomicSyncStore failed: %v", err)
	}

	if err := store.EnqueueUpsert(ctx, "mem-a", 1); err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}
	clock.Advance(time.Second)
	if err := store.EnqueueUpsert(ctx, "mem-a", 2); err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}
	clock.Advance(time.Second)
	if err := store.EnqueueDelete(ctx, "mem-b", 1); err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	// 同一 memory_id 只取最新 revision 的 pending job。
	job, err := store.NextPendingJob(ctx, "mem-a")
	if err != nil || job == nil {
		t.Fatalf("NextPendingJob(mem-a) = %+v, %v", job, err)
	}
	if job.Revision != 2 || job.Operation != AtomicIndexOpUpsert {
		t.Fatalf("expected latest revision pending job, got %+v", job)
	}

	// 发现语义：每个 memory_id 一条、按 updated_at 升序。
	jobs, err := store.ListPendingJobs(ctx, 10)
	if err != nil {
		t.Fatalf("ListPendingJobs failed: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("expected 2 pending jobs (one per memory), got %d: %+v", len(jobs), jobs)
	}
	if jobs[0].MemoryID != "mem-a" || jobs[0].Revision != 2 || jobs[1].MemoryID != "mem-b" || jobs[1].Revision != 1 {
		t.Fatalf("unexpected pending job list: %+v", jobs)
	}

	// 最新 done 后回落到仍 pending 的旧 revision；全部 done 后为空。
	if err := store.MarkDone(ctx, "mem-a", 2); err != nil {
		t.Fatalf("MarkDone failed: %v", err)
	}
	job, err = store.NextPendingJob(ctx, "mem-a")
	if err != nil || job == nil || job.Revision != 1 {
		t.Fatalf("NextPendingJob after done = %+v, %v", job, err)
	}
	if err := store.MarkDone(ctx, "mem-a", 1); err != nil {
		t.Fatalf("MarkDone failed: %v", err)
	}
	if job, err = store.NextPendingJob(ctx, "mem-a"); err != nil || job != nil {
		t.Fatalf("expected no pending job for mem-a, got %+v, %v", job, err)
	}
	jobs, err = store.ListPendingJobs(ctx, 10)
	if err != nil || len(jobs) != 1 || jobs[0].MemoryID != "mem-b" {
		t.Fatalf("unexpected pending job list after done: %+v, %v", jobs, err)
	}
}

func TestAtomicSyncStoreTombstoneBlocksResurrection(t *testing.T) {
	db, _, cleanup := setupAtomicSyncTestDB(t)
	defer cleanup()

	ctx := context.Background()
	clock := newFakeClock()
	store, err := NewAtomicSyncStore(ctx, db, WithAtomicSyncClock(clock.Now))
	if err != nil {
		t.Fatalf("NewAtomicSyncStore failed: %v", err)
	}

	insertSyncTestMemory(t, db, "mem-d", "session-1", "将被删除的正文")

	// 一次更新入队 rev2 upsert（尚未处理）。
	if err := store.EnqueueUpsert(ctx, "mem-d", 2); err != nil {
		t.Fatalf("EnqueueUpsert failed: %v", err)
	}

	// 删除路径：同一事务写 tombstone + delete job（b 步组装方式）。
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx failed: %v", err)
	}
	if err := store.MarkDeletedTx(ctx, tx, "mem-d", 3); err != nil {
		t.Fatalf("MarkDeletedTx failed: %v", err)
	}
	if err := store.EnqueueDeleteTx(ctx, tx, "mem-d", 3); err != nil {
		t.Fatalf("EnqueueDeleteTx failed: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit failed: %v", err)
	}

	// 墓碑持久存在，正文行保留 deleted=1。
	tombstone, err := store.Tombstone(ctx, "mem-d")
	if err != nil || tombstone == nil {
		t.Fatalf("Tombstone = %+v, %v", tombstone, err)
	}
	if tombstone.Revision != 3 || tombstone.MemoryID != "mem-d" {
		t.Fatalf("unexpected tombstone: %+v", tombstone)
	}
	var deleted int
	if err := db.QueryRow(`SELECT deleted FROM atomic_memories WHERE id = 'mem-d'`).Scan(&deleted); err != nil || deleted != 1 {
		t.Fatalf("expected deleted=1 row, deleted=%d err=%v", deleted, err)
	}

	// 晚到的旧 revision upsert 不得复活正文；不超过墓碑的新 revision 允许。
	ok, err := store.CanApplyUpsert(ctx, "mem-d", 2)
	if err != nil {
		t.Fatalf("CanApplyUpsert failed: %v", err)
	}
	if ok {
		t.Fatalf("stale upsert job rev2 must not resurrect deleted memory (tombstone rev3)")
	}
	if ok, err = store.CanApplyUpsert(ctx, "mem-d", 3); err != nil || ok {
		t.Fatalf("upsert at tombstone revision must be rejected: ok=%v err=%v", ok, err)
	}
	if ok, err = store.CanApplyUpsert(ctx, "mem-d", 4); err != nil || !ok {
		t.Fatalf("newer revision must be allowed: ok=%v err=%v", ok, err)
	}

	// 串行语义下 worker 取到的是 rev3 的 delete job，而非晚到的 rev2 upsert。
	job, err := store.NextPendingJob(ctx, "mem-d")
	if err != nil || job == nil {
		t.Fatalf("NextPendingJob = %+v, %v", job, err)
	}
	if job.Revision != 3 || job.Operation != AtomicIndexOpDelete {
		t.Fatalf("expected delete job rev3 to win serial pick, got %+v", job)
	}
}

func TestAtomicSyncStoreMarkDoneAndRetry(t *testing.T) {
	db, _, cleanup := setupAtomicSyncTestDB(t)
	defer cleanup()

	ctx := context.Background()
	clock := newFakeClock()
	store, err := NewAtomicSyncStore(ctx, db, WithAtomicSyncClock(clock.Now))
	if err != nil {
		t.Fatalf("NewAtomicSyncStore failed: %v", err)
	}

	if err := store.EnqueueUpsert(ctx, "mem-a", 1); err != nil {
		t.Fatalf("EnqueueUpsert failed: %v", err)
	}

	clock.Advance(2 * time.Second)
	if err := store.MarkRetry(ctx, "mem-a", 1, errors.New("vector add boom")); err != nil {
		t.Fatalf("MarkRetry failed: %v", err)
	}
	job, err := store.GetJob(ctx, "mem-a", 1)
	if err != nil || job == nil {
		t.Fatalf("GetJob failed: %v", err)
	}
	if job.Attempts != 1 || job.LastError != "vector add boom" || job.State != AtomicIndexJobPending || job.UpdatedAt != 1700000002 {
		t.Fatalf("unexpected job after retry: %+v", job)
	}

	clock.Advance(3 * time.Second)
	if err := store.MarkDone(ctx, "mem-a", 1); err != nil {
		t.Fatalf("MarkDone failed: %v", err)
	}
	job, err = store.GetJob(ctx, "mem-a", 1)
	if err != nil || job == nil {
		t.Fatalf("GetJob failed: %v", err)
	}
	if job.State != AtomicIndexJobDone || job.Attempts != 1 || job.UpdatedAt != 1700000005 {
		t.Fatalf("unexpected job after done: %+v", job)
	}
	if pending, err := store.NextPendingJob(ctx, "mem-a"); err != nil || pending != nil {
		t.Fatalf("expected no pending after done, got %+v, %v", pending, err)
	}

	// done 的任务不能再标记 retry；不存在的任务报哨兵错误。
	if err := store.MarkRetry(ctx, "mem-a", 1, errors.New("late")); !errors.Is(err, ErrAtomicIndexJobNotFound) {
		t.Fatalf("expected ErrAtomicIndexJobNotFound retrying done job, got: %v", err)
	}
	if err := store.MarkDone(ctx, "mem-x", 9); !errors.Is(err, ErrAtomicIndexJobNotFound) {
		t.Fatalf("expected ErrAtomicIndexJobNotFound for unknown job, got: %v", err)
	}

	// LatestJob 无视状态取最大 revision。
	if err := store.EnqueueDelete(ctx, "mem-a", 2); err != nil {
		t.Fatalf("EnqueueDelete failed: %v", err)
	}
	latest, err := store.LatestJob(ctx, "mem-a")
	if err != nil || latest == nil || latest.Revision != 2 || latest.Operation != AtomicIndexOpDelete {
		t.Fatalf("unexpected latest job: %+v, %v", latest, err)
	}
	if latest, err = store.LatestJob(ctx, "mem-none"); err != nil || latest != nil {
		t.Fatalf("expected nil latest job, got %+v, %v", latest, err)
	}
}

// ---------------------------------------------------------------------------
// 故障注入测试
// ---------------------------------------------------------------------------

func TestAtomicSyncStoreCommitFaultInjection(t *testing.T) {
	db, _, cleanup := setupAtomicSyncTestDB(t)
	defer cleanup()

	ctx := context.Background()
	clock := newFakeClock()
	store, err := NewAtomicSyncStore(ctx, db, WithAtomicSyncClock(clock.Now))
	if err != nil {
		t.Fatalf("NewAtomicSyncStore failed: %v", err)
	}
	insertSyncTestMemory(t, db, "mem-c", "session-1", "提交点故障场景")

	faultDB := &commitFaultDB{db: db}

	// 第一轮：tombstone + job 的同事务提交被注入故障。
	faultDB.failNextCommit()
	tx, err := faultDB.beginTx(ctx)
	if err != nil {
		t.Fatalf("begin tx failed: %v", err)
	}
	if err := store.MarkDeletedTx(ctx, tx, "mem-c", 2); err != nil {
		t.Fatalf("MarkDeletedTx failed: %v", err)
	}
	if err := store.EnqueueDeleteTx(ctx, tx, "mem-c", 2); err != nil {
		t.Fatalf("EnqueueDeleteTx failed: %v", err)
	}
	if err := tx.Commit(); !errors.Is(err, errInjectedCommit) {
		t.Fatalf("expected injected commit fault, got: %v", err)
	}

	// 正确残留：tombstone 未生效、job 未落库（不丢——可由重试完整重做）。
	if tombstone, err := store.Tombstone(ctx, "mem-c"); err != nil || tombstone != nil {
		t.Fatalf("expected no tombstone after commit fault, got %+v, %v", tombstone, err)
	}
	if job, err := store.GetJob(ctx, "mem-c", 2); err != nil || job != nil {
		t.Fatalf("expected no job after commit fault, got %+v, %v", job, err)
	}
	if n := countJobs(t, db, "mem-c"); n != 0 {
		t.Fatalf("expected 0 jobs after commit fault, got %d", n)
	}

	// 第二轮：无故障重试同事务组装，一次性成功。
	tx, err = faultDB.beginTx(ctx)
	if err != nil {
		t.Fatalf("begin tx failed: %v", err)
	}
	if err := store.MarkDeletedTx(ctx, tx, "mem-c", 2); err != nil {
		t.Fatalf("MarkDeletedTx failed: %v", err)
	}
	if err := store.EnqueueDeleteTx(ctx, tx, "mem-c", 2); err != nil {
		t.Fatalf("EnqueueDeleteTx failed: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit failed: %v", err)
	}
	tombstone, err := store.Tombstone(ctx, "mem-c")
	if err != nil || tombstone == nil || tombstone.Revision != 2 {
		t.Fatalf("expected tombstone rev2 after retry, got %+v, %v", tombstone, err)
	}

	// 不重复：重复 enqueue 同 revision 仍只有一条 job。
	if err := store.EnqueueDelete(ctx, "mem-c", 2); err != nil {
		t.Fatalf("duplicate enqueue failed: %v", err)
	}
	if n := countJobs(t, db, "mem-c"); n != 1 {
		t.Fatalf("expected exactly 1 job after retry + duplicate enqueue, got %d", n)
	}
}

func TestAtomicSyncStoreVectorFaultRecovery(t *testing.T) {
	db, _, cleanup := setupAtomicSyncTestDB(t)
	defer cleanup()

	ctx := context.Background()
	clock := newFakeClock()
	store, err := NewAtomicSyncStore(ctx, db, WithAtomicSyncClock(clock.Now))
	if err != nil {
		t.Fatalf("NewAtomicSyncStore failed: %v", err)
	}
	vectorStore, cleanupVector := setupSyncTestVectorStore(t)
	defer cleanupVector()

	insertSyncTestMemory(t, db, "mem-v", "session-1", "向量删除故障场景")
	if err := vectorStore.Add(ctx, []DocumentChunk{
		{ID: "mem-v", Content: "向量删除故障场景", Metadata: ChunkMetadata{SessionID: "session-1", Timestamp: 100, Source: SourceUser}},
	}); err != nil {
		t.Fatalf("seed vector failed: %v", err)
	}

	// 删除路径提交 tombstone + delete job（正文侧已成功）。
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx failed: %v", err)
	}
	if err := store.MarkDeletedTx(ctx, tx, "mem-v", 2); err != nil {
		t.Fatalf("MarkDeletedTx failed: %v", err)
	}
	if err := store.EnqueueDeleteTx(ctx, tx, "mem-v", 2); err != nil {
		t.Fatalf("EnqueueDeleteTx failed: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit failed: %v", err)
	}

	// E-02 触发二：向量 Delete 故障。正文 tombstone 与 job 不丢。
	vectorStore.setDeleteFault(true)
	job, err := store.NextPendingJob(ctx, "mem-v")
	if err != nil || job == nil || job.Operation != AtomicIndexOpDelete {
		t.Fatalf("NextPendingJob = %+v, %v", job, err)
	}
	deleteErr := vectorStore.Delete(ctx, []string{job.MemoryID})
	if !errors.Is(deleteErr, errInjectedVector) {
		t.Fatalf("expected injected vector delete fault, got: %v", deleteErr)
	}
	clock.Advance(time.Second)
	if err := store.MarkRetry(ctx, job.MemoryID, job.Revision, deleteErr); err != nil {
		t.Fatalf("MarkRetry failed: %v", err)
	}
	if n, err := vectorStore.Count(ctx); err != nil || n != 1 {
		t.Fatalf("expected vector still present after fault, count=%d err=%v", n, err)
	}
	job, err = store.GetJob(ctx, "mem-v", 2)
	if err != nil || job == nil || job.State != AtomicIndexJobPending || job.Attempts != 1 || job.LastError == "" {
		t.Fatalf("expected pending retry job preserved, got %+v, %v", job, err)
	}

	// 故障清除后重放同一 job：清理完成并标 done；墓碑依旧拦截复活。
	vectorStore.setDeleteFault(false)
	if err := vectorStore.Delete(ctx, []string{"mem-v"}); err != nil {
		t.Fatalf("vector delete retry failed: %v", err)
	}
	if err := store.MarkDone(ctx, "mem-v", 2); err != nil {
		t.Fatalf("MarkDone failed: %v", err)
	}
	if n, err := vectorStore.Count(ctx); err != nil || n != 0 {
		t.Fatalf("expected vector cleaned after retry, count=%d err=%v", n, err)
	}
	if pending, err := store.NextPendingJob(ctx, "mem-v"); err != nil || pending != nil {
		t.Fatalf("expected no pending job, got %+v, %v", pending, err)
	}
	if ok, err := store.CanApplyUpsert(ctx, "mem-v", 2); err != nil || ok {
		t.Fatalf("tombstone must still block resurrection: ok=%v err=%v", ok, err)
	}

	// 向量 Add 故障：upsert job 同样保留 pending 并可恢复。
	insertSyncTestMemory(t, db, "mem-w", "session-1", "向量写入故障场景")
	if err := store.EnqueueUpsert(ctx, "mem-w", 1); err != nil {
		t.Fatalf("EnqueueUpsert failed: %v", err)
	}
	vectorStore.setAddFault(true)
	addErr := vectorStore.Add(ctx, []DocumentChunk{
		{ID: "mem-w", Content: "向量写入故障场景", Metadata: ChunkMetadata{SessionID: "session-1", Timestamp: 100, Source: SourceUser}},
	})
	if !errors.Is(addErr, errInjectedVector) {
		t.Fatalf("expected injected vector add fault, got: %v", addErr)
	}
	if err := store.MarkRetry(ctx, "mem-w", 1, addErr); err != nil {
		t.Fatalf("MarkRetry failed: %v", err)
	}
	vectorStore.setAddFault(false)
	if err := vectorStore.Add(ctx, []DocumentChunk{
		{ID: "mem-w", Content: "向量写入故障场景", Metadata: ChunkMetadata{SessionID: "session-1", Timestamp: 100, Source: SourceUser}},
	}); err != nil {
		t.Fatalf("vector add retry failed: %v", err)
	}
	if err := store.MarkDone(ctx, "mem-w", 1); err != nil {
		t.Fatalf("MarkDone failed: %v", err)
	}
	if n, err := vectorStore.Count(ctx); err != nil || n != 1 {
		t.Fatalf("expected vector added after retry, count=%d err=%v", n, err)
	}
}

// ---------------------------------------------------------------------------
// 参数校验
// ---------------------------------------------------------------------------

func TestAtomicSyncStoreValidation(t *testing.T) {
	db, _, cleanup := setupAtomicSyncTestDB(t)
	defer cleanup()

	ctx := context.Background()
	store, err := NewAtomicSyncStore(ctx, db)
	if err != nil {
		t.Fatalf("NewAtomicSyncStore failed: %v", err)
	}

	if err := store.EnqueueUpsert(ctx, "", 1); err == nil {
		t.Fatalf("expected error for empty memory id")
	}
	if err := store.EnqueueUpsert(ctx, "mem-a", 0); err == nil {
		t.Fatalf("expected error for non-positive revision")
	}
	if err := store.EnqueueDelete(ctx, "mem-a", -1); err == nil {
		t.Fatalf("expected error for negative revision")
	}
	if err := store.MarkDeleted(ctx, "  ", 1); err == nil {
		t.Fatalf("expected error for blank memory id")
	}
	if err := store.MarkDeleted(ctx, "mem-missing", 1); !errors.Is(err, ErrAtomicMemoryNotFound) {
		t.Fatalf("expected ErrAtomicMemoryNotFound, got: %v", err)
	}
	if _, err := store.Tombstone(ctx, ""); err == nil {
		t.Fatalf("expected error for empty memory id")
	}
	if tombstone, err := store.Tombstone(ctx, "mem-missing"); err != nil || tombstone != nil {
		t.Fatalf("expected nil tombstone, got %+v, %v", tombstone, err)
	}
	if _, err := store.CanApplyUpsert(ctx, "mem-a", 0); err == nil {
		t.Fatalf("expected error for non-positive revision")
	}
	if _, err := store.NextPendingJob(ctx, ""); err == nil {
		t.Fatalf("expected error for empty memory id")
	}
	if _, err := store.GetJob(ctx, "mem-a", 0); err == nil {
		t.Fatalf("expected error for non-positive revision")
	}
	if job, err := store.GetJob(ctx, "mem-a", 1); err != nil || job != nil {
		t.Fatalf("expected nil job, got %+v, %v", job, err)
	}
	if err := store.MarkDone(ctx, "", 1); err == nil {
		t.Fatalf("expected error for empty memory id")
	}
	if err := store.MarkRetry(ctx, "mem-a", 1, nil); !errors.Is(err, ErrAtomicIndexJobNotFound) {
		t.Fatalf("expected ErrAtomicIndexJobNotFound, got: %v", err)
	}
	if err := MigrateAtomicSyncSchema(ctx, nil); err == nil {
		t.Fatalf("expected error when db is nil")
	}
}
