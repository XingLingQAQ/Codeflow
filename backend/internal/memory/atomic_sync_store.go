package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrAtomicIndexJobNotFound 索引同步任务不存在（或已不处于可重试状态）。
var ErrAtomicIndexJobNotFound = errors.New("atomic index job not found")

// AtomicIndexOperation 索引同步操作类型（§28 T13.02.a：upsert/delete）。
type AtomicIndexOperation string

const (
	AtomicIndexOpUpsert AtomicIndexOperation = "upsert"
	AtomicIndexOpDelete AtomicIndexOperation = "delete"
)

// AtomicIndexJobState 索引同步任务状态。
type AtomicIndexJobState string

const (
	AtomicIndexJobPending AtomicIndexJobState = "pending"
	AtomicIndexJobDone    AtomicIndexJobState = "done"
	AtomicIndexJobFailed  AtomicIndexJobState = "failed"
)

// AtomicIndexJob 是 atomic_index_jobs 表的一条记录：正文与向量索引之间
// 可重入、可查询、可重试的持久化同步意图。唯一键为 (memory_id, revision)。
type AtomicIndexJob struct {
	MemoryID  string
	Revision  int64
	Operation AtomicIndexOperation
	State     AtomicIndexJobState
	Attempts  int
	LastError string
	UpdatedAt int64
}

// AtomicMemoryTombstone 已删除记忆的修订墓碑。晚到的、revision 不超过
// 墓碑的旧任务不得复活正文（§28 T13.02.a）。
type AtomicMemoryTombstone struct {
	MemoryID string
	Revision int64
}

// SyncExecutor 抽象 *sql.DB 与 *sql.Tx 共有的执行面。本步的原子方法经它
// 执行 SQL；mutation 路径（T13.02.b）用它把正文写入/tombstone 与 job
// 组装进同一个事务，测试用它注入提交点故障。
type SyncExecutor interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

// atomicSyncSchemaVersion 是正文同步 schema 的当前版本（PRAGMA user_version）。
const atomicSyncSchemaVersion = 1

// atomicSyncMigrations v1：正文表增加 revision/deleted 状态列（既有行默认
// revision=1、deleted=0，正文内容与既有 ID 原样保留），并创建
// atomic_index_jobs 表（唯一键 memory_id+revision）。
var atomicSyncMigrations = []userVersionMigration{
	{
		version: 1,
		statements: []string{
			`ALTER TABLE atomic_memories ADD COLUMN revision INTEGER NOT NULL DEFAULT 1`,
			`ALTER TABLE atomic_memories ADD COLUMN deleted INTEGER NOT NULL DEFAULT 0`,
			`CREATE TABLE IF NOT EXISTS atomic_index_jobs (
				memory_id TEXT NOT NULL,
				revision INTEGER NOT NULL,
				operation TEXT NOT NULL CHECK (operation IN ('upsert', 'delete')),
				state TEXT NOT NULL CHECK (state IN ('pending', 'done', 'failed')),
				attempts INTEGER NOT NULL DEFAULT 0,
				last_error TEXT NOT NULL DEFAULT '',
				updated_at INTEGER NOT NULL,
				PRIMARY KEY (memory_id, revision)
			)`,
			`CREATE INDEX IF NOT EXISTS idx_atomic_index_jobs_state ON atomic_index_jobs(state, updated_at)`,
		},
	},
}

// MigrateAtomicSyncSchema 是正文库的版本化迁移入口：先保证既有原子正文
// schema 存在（空库直接建表，更旧的库由 EnsureAtomicMemorySchema 补齐
// tier/heat/surprise 列），再按 PRAGMA user_version 应用增量迁移。
// 空库、旧库与已是最新的库三种入口幂等：重入不报错、不重复加列。
func MigrateAtomicSyncSchema(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return errors.New("migrate atomic sync schema: db is nil")
	}
	ctx = ensureContext(ctx)
	if err := EnsureAtomicMemorySchema(ctx, db); err != nil {
		return err
	}
	if err := migrateUserVersion(ctx, db, atomicSyncMigrations); err != nil {
		return fmt.Errorf("migrate atomic sync schema: %w", err)
	}
	return nil
}

// AtomicSyncStore 是原子记忆正文库的同步状态存储：管理每条记忆的
// revision/deleted 状态与 atomic_index_jobs 持久化同步意图。
// 本类型只提供存储原语；正文写入、tombstone 与 job 的事务组装由
// mutation 调用方（T13.02.b）完成。
type AtomicSyncStore struct {
	db  *sql.DB
	now func() time.Time
}

// AtomicSyncStoreOption 配置 AtomicSyncStore。
type AtomicSyncStoreOption func(*AtomicSyncStore)

// WithAtomicSyncClock 注入时间源（测试使用 fake clock），默认 time.Now。
func WithAtomicSyncClock(now func() time.Time) AtomicSyncStoreOption {
	return func(s *AtomicSyncStore) {
		if now != nil {
			s.now = now
		}
	}
}

// NewAtomicSyncStore 打开同步存储：先运行版本化迁移（空库/旧库/最新库
// 均幂等），迁移成功才返回就绪的存储。db 的所有权归调用方。
func NewAtomicSyncStore(ctx context.Context, db *sql.DB, opts ...AtomicSyncStoreOption) (*AtomicSyncStore, error) {
	if db == nil {
		return nil, errors.New("atomic sync store init failed: db is nil")
	}
	if err := MigrateAtomicSyncSchema(ctx, db); err != nil {
		return nil, err
	}
	s := &AtomicSyncStore{db: db, now: time.Now}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	return s, nil
}

// EnqueueUpsert 记录一条 upsert 同步意图（自含单语句提交）。
func (s *AtomicSyncStore) EnqueueUpsert(ctx context.Context, memoryID string, revision int64) error {
	return s.EnqueueUpsertTx(ctx, s.db, memoryID, revision)
}

// EnqueueDelete 记录一条 delete 同步意图（自含单语句提交）。
func (s *AtomicSyncStore) EnqueueDelete(ctx context.Context, memoryID string, revision int64) error {
	return s.EnqueueDeleteTx(ctx, s.db, memoryID, revision)
}

// EnqueueUpsertTx 与 EnqueueUpsert 相同，但运行在调用方提供的事务内，
// 供 mutation 路径把正文写入与 job 组装为一次提交。
func (s *AtomicSyncStore) EnqueueUpsertTx(ctx context.Context, ex SyncExecutor, memoryID string, revision int64) error {
	return s.enqueue(ctx, ex, memoryID, revision, AtomicIndexOpUpsert)
}

// EnqueueDeleteTx 与 EnqueueDelete 相同，但运行在调用方提供的事务内。
func (s *AtomicSyncStore) EnqueueDeleteTx(ctx context.Context, ex SyncExecutor, memoryID string, revision int64) error {
	return s.enqueue(ctx, ex, memoryID, revision, AtomicIndexOpDelete)
}

// enqueue 以 (memory_id, revision) 唯一键去重：重复 enqueue 不报错、
// 不重置已有 attempts/last_error/state，第一次入队的记录获胜。
func (s *AtomicSyncStore) enqueue(ctx context.Context, ex SyncExecutor, memoryID string, revision int64, op AtomicIndexOperation) error {
	if err := validateMemoryRevision(memoryID, revision); err != nil {
		return err
	}
	if ex == nil {
		return errors.New("enqueue atomic index job: executor is nil")
	}
	_, err := ex.ExecContext(ensureContext(ctx), `
		INSERT INTO atomic_index_jobs (memory_id, revision, operation, state, attempts, last_error, updated_at)
		VALUES (?, ?, ?, ?, 0, '', ?)
		ON CONFLICT (memory_id, revision) DO NOTHING
	`, memoryID, revision, string(op), string(AtomicIndexJobPending), s.now().Unix())
	if err != nil {
		return fmt.Errorf("enqueue %s index job %s@%d: %w", op, memoryID, revision, err)
	}
	return nil
}

// MarkDeleted 把正文行标记为已删 tombstone（deleted=1）并记录其修订号，
// 自含提交。正文行不存在时报 ErrAtomicMemoryNotFound；行的物理保留保证
// 旧 revision 的晚到任务可被墓碑拦截。
func (s *AtomicSyncStore) MarkDeleted(ctx context.Context, memoryID string, revision int64) error {
	return s.MarkDeletedTx(ctx, s.db, memoryID, revision)
}

// MarkDeletedTx 与 MarkDeleted 相同，但运行在调用方提供的事务内。
func (s *AtomicSyncStore) MarkDeletedTx(ctx context.Context, ex SyncExecutor, memoryID string, revision int64) error {
	if err := validateMemoryRevision(memoryID, revision); err != nil {
		return err
	}
	if ex == nil {
		return errors.New("mark atomic memory deleted: executor is nil")
	}
	res, err := ex.ExecContext(ensureContext(ctx), `
		UPDATE atomic_memories SET deleted = 1, revision = ?, updated_at = ? WHERE id = ?
	`, revision, s.now().Unix(), memoryID)
	if err != nil {
		return fmt.Errorf("mark atomic memory %s deleted@%d: %w", memoryID, revision, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("mark atomic memory %s deleted: rows affected: %w", memoryID, err)
	}
	if affected == 0 {
		return ErrAtomicMemoryNotFound
	}
	return nil
}

// MarkDeletedReturningTx 单语句原子 tombstone（T13.02.fix1 缺陷 1 修复）：
// UPDATE ... SET deleted=1, revision=revision+1 WHERE id=? RETURNING revision。
// 相对 MarkDeletedTx 配套的"调用方先 SELECT revision 再 UPDATE"两步，单语句
// 是事务内的首条且即写语句，消除 WAL deferred 事务的延迟升级窗口——另一连接
// 在读写之间提交不再触发 SQLITE_BUSY_SNAPSHOT（该错误的 busy handler 不生效）。
// 锁冲突改写语句执行前的正常等待，由 busy_timeout 覆盖。
//
// 幂等语义与 b1 的读改写完全一致：WHERE 不过滤 deleted，存活行与 tombstone
// 行都令 revision 前进一步（重复删除幂等、补 delete job）；行不存在（0 行，
// RETURNING 无结果）报 ErrAtomicMemoryNotFound。返回前进后的 revision。
func (s *AtomicSyncStore) MarkDeletedReturningTx(ctx context.Context, ex SyncExecutor, memoryID string) (int64, error) {
	if strings.TrimSpace(memoryID) == "" {
		return 0, errors.New("memory id is required")
	}
	if ex == nil {
		return 0, errors.New("mark atomic memory deleted: executor is nil")
	}
	var revision int64
	err := ex.QueryRowContext(ensureContext(ctx), `
		UPDATE atomic_memories SET deleted = 1, revision = revision + 1, updated_at = ? WHERE id = ? RETURNING revision
	`, s.now().Unix(), memoryID).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrAtomicMemoryNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("mark atomic memory %s deleted: %w", memoryID, err)
	}
	return revision, nil
}

// AtomicMemoryRevisionState 是正文行的 revision/deleted 快照；行不存在时
// Exists=false（Revision/Deleted 为零值）。
type AtomicMemoryRevisionState struct {
	Revision int64
	Deleted  bool
	Exists   bool
}

// RevisionState 读取指定记忆正文行的 revision/deleted 状态（含 tombstone
// 行）。供发布路径在 MarkDone 前复查"刚写入的 revision 仍是该 ID 最新"
// （T13.02.fix1 缺陷 2 修复）：发布期间同 ID 的更新 mutation 可能已提交，
// 已落后的任务不得标 done，留 pending 由 worker 重放最新。
func (s *AtomicSyncStore) RevisionState(ctx context.Context, memoryID string) (AtomicMemoryRevisionState, error) {
	if strings.TrimSpace(memoryID) == "" {
		return AtomicMemoryRevisionState{}, errors.New("memory id is required")
	}
	var (
		revision int64
		deleted  int
	)
	err := s.db.QueryRowContext(ensureContext(ctx), `
		SELECT revision, deleted FROM atomic_memories WHERE id = ?
	`, memoryID).Scan(&revision, &deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return AtomicMemoryRevisionState{}, nil
	}
	if err != nil {
		return AtomicMemoryRevisionState{}, fmt.Errorf("read revision state for %s: %w", memoryID, err)
	}
	return AtomicMemoryRevisionState{Revision: revision, Deleted: deleted == 1, Exists: true}, nil
}

// Tombstone 返回指定记忆的删除墓碑；记忆不存在或未被删除时返回 (nil, nil)。
func (s *AtomicSyncStore) Tombstone(ctx context.Context, memoryID string) (*AtomicMemoryTombstone, error) {
	if strings.TrimSpace(memoryID) == "" {
		return nil, errors.New("memory id is required")
	}
	var (
		revision int64
		deleted  int
	)
	err := s.db.QueryRowContext(ensureContext(ctx), `
		SELECT revision, deleted FROM atomic_memories WHERE id = ?
	`, memoryID).Scan(&revision, &deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query tombstone for %s: %w", memoryID, err)
	}
	if deleted == 0 {
		return nil, nil
	}
	return &AtomicMemoryTombstone{MemoryID: memoryID, Revision: revision}, nil
}

// CanApplyUpsert 判定一条 upsert 任务是否允许写入向量索引。两种拒绝：
//  1. 存在墓碑且墓碑 revision 大于等于任务 revision——已删 ID 的 revision
//     tombstone 不能被晚到的旧任务复活；
//  2. 正文行的当前 revision 高于任务 revision——同 ID 已被更新的 mutation
//     推进（含复活后墓碑已清除的情形），旧任务不得覆盖新版本。
//
// 任务 revision 与行 revision 相同且行未删除时放行（提交后正常发布路径）。
func (s *AtomicSyncStore) CanApplyUpsert(ctx context.Context, memoryID string, revision int64) (bool, error) {
	if err := validateMemoryRevision(memoryID, revision); err != nil {
		return false, err
	}
	var (
		rowRevision int64
		deleted     int
	)
	err := s.db.QueryRowContext(ensureContext(ctx), `
		SELECT revision, deleted FROM atomic_memories WHERE id = ?
	`, memoryID).Scan(&rowRevision, &deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("can apply upsert %s@%d: %w", memoryID, revision, err)
	}
	if rowRevision > revision {
		return false, nil
	}
	if deleted == 1 && rowRevision >= revision {
		return false, nil
	}
	return true, nil
}

// NextPendingJob 返回指定记忆当前应处理的同步任务：该 memory_id 下
// revision 最大的 pending job；没有 pending 任务时返回 (nil, nil)。
// 每个 memory_id 的 worker 按此串行消费，新 revision 入队后旧 revision
// 的任务不再被优先处理。
func (s *AtomicSyncStore) NextPendingJob(ctx context.Context, memoryID string) (*AtomicIndexJob, error) {
	if strings.TrimSpace(memoryID) == "" {
		return nil, errors.New("memory id is required")
	}
	row := s.db.QueryRowContext(ensureContext(ctx), `
		SELECT memory_id, revision, operation, state, attempts, last_error, updated_at
		FROM atomic_index_jobs
		WHERE memory_id = ? AND state = ?
		ORDER BY revision DESC
		LIMIT 1
	`, memoryID, string(AtomicIndexJobPending))
	job, err := scanAtomicIndexJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("next pending index job for %s: %w", memoryID, err)
	}
	return &job, nil
}

// ListPendingJobs 按 memory_id 发现待处理任务：每个 memory_id 只返回其
// revision 最大的 pending job，按 updated_at 升序（先入队先处理），供
// 启动恢复器与 worker 扫描。
func (s *AtomicSyncStore) ListPendingJobs(ctx context.Context, limit int) ([]AtomicIndexJob, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ensureContext(ctx), `
		SELECT j.memory_id, j.revision, j.operation, j.state, j.attempts, j.last_error, j.updated_at
		FROM atomic_index_jobs j
		WHERE j.state = ?
		  AND j.revision = (
			SELECT MAX(p.revision) FROM atomic_index_jobs p
			WHERE p.memory_id = j.memory_id AND p.state = ?
		  )
		ORDER BY j.updated_at ASC, j.memory_id ASC
		LIMIT ?
	`, string(AtomicIndexJobPending), string(AtomicIndexJobPending), limit)
	if err != nil {
		return nil, fmt.Errorf("list pending index jobs: %w", err)
	}
	defer rows.Close()
	jobs := make([]AtomicIndexJob, 0)
	for rows.Next() {
		job, err := scanAtomicIndexJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list pending index jobs: %w", err)
	}
	return jobs, nil
}

// MarkDone 把任务标记为完成；任务不存在时报 ErrAtomicIndexJobNotFound。
// 对已 done 的任务重复调用是幂等的（按主键匹配），用于崩溃点恢复。
func (s *AtomicSyncStore) MarkDone(ctx context.Context, memoryID string, revision int64) error {
	if err := validateMemoryRevision(memoryID, revision); err != nil {
		return err
	}
	res, err := s.db.ExecContext(ensureContext(ctx), `
		UPDATE atomic_index_jobs SET state = ?, updated_at = ? WHERE memory_id = ? AND revision = ?
	`, string(AtomicIndexJobDone), s.now().Unix(), memoryID, revision)
	if err != nil {
		return fmt.Errorf("mark index job done %s@%d: %w", memoryID, revision, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("mark index job done %s@%d: rows affected: %w", memoryID, revision, err)
	}
	if affected == 0 {
		return ErrAtomicIndexJobNotFound
	}
	return nil
}

// MarkRetry 记录一次失败尝试：attempts+1、写入 last_error，任务保持
// pending 以便重试；任务不存在或已非 pending 时报 ErrAtomicIndexJobNotFound。
func (s *AtomicSyncStore) MarkRetry(ctx context.Context, memoryID string, revision int64, lastErr error) error {
	if err := validateMemoryRevision(memoryID, revision); err != nil {
		return err
	}
	message := ""
	if lastErr != nil {
		message = lastErr.Error()
	}
	res, err := s.db.ExecContext(ensureContext(ctx), `
		UPDATE atomic_index_jobs
		SET attempts = attempts + 1, last_error = ?, updated_at = ?
		WHERE memory_id = ? AND revision = ? AND state = ?
	`, message, s.now().Unix(), memoryID, revision, string(AtomicIndexJobPending))
	if err != nil {
		return fmt.Errorf("mark index job retry %s@%d: %w", memoryID, revision, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("mark index job retry %s@%d: rows affected: %w", memoryID, revision, err)
	}
	if affected == 0 {
		return ErrAtomicIndexJobNotFound
	}
	return nil
}

// MarkFailed 把任务标记为确定性终态 failed（不再被 worker 重试），写入
// 失败原因；任务不存在或已非 pending 时报 ErrAtomicIndexJobNotFound。
// T13.02.b part 2 最小扩展：worker 需要终态原语（重试上限、门禁拦截的
// stale job），MarkDone/MarkRetry 均无法表达 failed。
func (s *AtomicSyncStore) MarkFailed(ctx context.Context, memoryID string, revision int64, reason string) error {
	if err := validateMemoryRevision(memoryID, revision); err != nil {
		return err
	}
	res, err := s.db.ExecContext(ensureContext(ctx), `
		UPDATE atomic_index_jobs
		SET state = ?, last_error = ?, updated_at = ?
		WHERE memory_id = ? AND revision = ? AND state = ?
	`, string(AtomicIndexJobFailed), reason, s.now().Unix(), memoryID, revision, string(AtomicIndexJobPending))
	if err != nil {
		return fmt.Errorf("mark index job failed %s@%d: %w", memoryID, revision, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("mark index job failed %s@%d: rows affected: %w", memoryID, revision, err)
	}
	if affected == 0 {
		return ErrAtomicIndexJobNotFound
	}
	return nil
}

// GetJob 读取指定 (memory_id, revision) 的任务；不存在时返回 (nil, nil)。
func (s *AtomicSyncStore) GetJob(ctx context.Context, memoryID string, revision int64) (*AtomicIndexJob, error) {
	if err := validateMemoryRevision(memoryID, revision); err != nil {
		return nil, err
	}
	row := s.db.QueryRowContext(ensureContext(ctx), `
		SELECT memory_id, revision, operation, state, attempts, last_error, updated_at
		FROM atomic_index_jobs WHERE memory_id = ? AND revision = ?
	`, memoryID, revision)
	job, err := scanAtomicIndexJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get index job %s@%d: %w", memoryID, revision, err)
	}
	return &job, nil
}

// IndexSyncBacklog 统计"当前 revision 未同步"的索引 job 数（T13.02.c）。
// 只统计 revision 等于正文行当前 revision 的 pending/failed job——即最新
// 同步意图尚未完成：pending 表示正文已提交但向量未就位；failed 表示最新
// 同步意图终态失败，索引与正文可能分叉。revision 落后于正文当前 revision
// 的 job 是被更新意图取代的历史证据（stale 终态，worker 的正常产出），
// 不影响当前检索的完整性，不计入。检索回执用本统计表达"结果可能不完整"，
// 而不是把未同步的检索当成完整空结果。
func (s *AtomicSyncStore) IndexSyncBacklog(ctx context.Context) (pending int, failed int, err error) {
	rows, err := s.db.QueryContext(ensureContext(ctx), `
		SELECT j.state, COUNT(*)
		FROM atomic_index_jobs j
		JOIN atomic_memories m ON m.id = j.memory_id AND m.revision = j.revision
		WHERE j.state IN (?, ?)
		GROUP BY j.state
	`, string(AtomicIndexJobPending), string(AtomicIndexJobFailed))
	if err != nil {
		return 0, 0, fmt.Errorf("count index sync backlog: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			state string
			count int
		)
		if err := rows.Scan(&state, &count); err != nil {
			return 0, 0, fmt.Errorf("scan index sync backlog: %w", err)
		}
		switch AtomicIndexJobState(state) {
		case AtomicIndexJobPending:
			pending = count
		case AtomicIndexJobFailed:
			failed = count
		}
	}
	if err := rows.Err(); err != nil {
		return 0, 0, fmt.Errorf("iterate index sync backlog: %w", err)
	}
	return pending, failed, nil
}

// AtomicIndexJobPrunePolicy 是 atomic_index_jobs done 行的保留策略
// （T13.02.c，§26.17 登记的最小清理）。确定规则：
//  1. state != done 的行（pending/failed）永不删除——待办意图与失败证据
//     不受保留策略影响；
//  2. 每个 memory_id 始终保留 revision 最大的 1 条 done 行：最近一次同步
//     回执（LatestJob 与 mutation 回执的可观测性）不因清理丢失；
//  3. 其余 done 行中 updated_at 早于 now-MaxAge 的删除（年龄窗口）；
//  4. 每个 memory_id 的 done 行总数仍超过 MaxPerMemory 时，按 revision 从
//     旧到新删到 MaxPerMemory 条（数量上限；MaxPerMemory>=1 保证规则 2）。
//
// 零值字段回落默认：MaxAge 7 天、MaxPerMemory 16。
type AtomicIndexJobPrunePolicy struct {
	// MaxAge done 行的保留窗口；updated_at 早于 now-MaxAge 才可清理。
	MaxAge time.Duration
	// MaxPerMemory 每个 memory_id 最多保留的 done 行数（按 revision 从新到旧计）。
	MaxPerMemory int
}

// defaultAtomicIndexJobPrunePolicy 生产默认保留策略：7 天窗口，每个 memory
// 最多 16 条 done 行。
func defaultAtomicIndexJobPrunePolicy() AtomicIndexJobPrunePolicy {
	return AtomicIndexJobPrunePolicy{MaxAge: 7 * 24 * time.Hour, MaxPerMemory: 16}
}

func (p AtomicIndexJobPrunePolicy) normalized() AtomicIndexJobPrunePolicy {
	defaults := defaultAtomicIndexJobPrunePolicy()
	if p.MaxAge <= 0 {
		p.MaxAge = defaults.MaxAge
	}
	if p.MaxPerMemory <= 0 {
		p.MaxPerMemory = defaults.MaxPerMemory
	}
	return p
}

// PruneDoneJobs 按保留策略清理 atomic_index_jobs 的 done 行，返回实际删除
// 行数（两条 DELETE 的 RowsAffected 均核验）。只删 done 行：pending/failed
// 行不受影响；每个 memory_id 最新 revision 的 done 行始终保留（见策略注释
// 规则 2）。时间源为 store 时钟（测试可注入 fake clock）。
func (s *AtomicSyncStore) PruneDoneJobs(ctx context.Context, policy AtomicIndexJobPrunePolicy) (int, error) {
	policy = policy.normalized()
	cutoff := s.now().Add(-policy.MaxAge).Unix()

	// 规则 3+2：过期 done 行中，凡存在更新 revision 的 done 同胞者删除。
	resAge, err := s.db.ExecContext(ensureContext(ctx), `
		DELETE FROM atomic_index_jobs
		WHERE state = ? AND updated_at < ?
		  AND EXISTS (
			SELECT 1 FROM atomic_index_jobs newer
			WHERE newer.memory_id = atomic_index_jobs.memory_id
			  AND newer.state = ?
			  AND newer.revision > atomic_index_jobs.revision
		  )
	`, string(AtomicIndexJobDone), cutoff, string(AtomicIndexJobDone))
	if err != nil {
		return 0, fmt.Errorf("prune done index jobs by age: %w", err)
	}
	aged, err := resAge.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("prune done index jobs by age: rows affected: %w", err)
	}

	// 规则 4：每个 memory_id 的 done 行数量上限（比它新的 done 同胞达到
	// MaxPerMemory 条的行删除，即保留 revision 最大的 N 条）。
	resCap, err := s.db.ExecContext(ensureContext(ctx), `
		DELETE FROM atomic_index_jobs
		WHERE state = ?
		  AND (
			SELECT COUNT(*) FROM atomic_index_jobs newer
			WHERE newer.memory_id = atomic_index_jobs.memory_id
			  AND newer.state = ?
			  AND newer.revision > atomic_index_jobs.revision
		  ) >= ?
	`, string(AtomicIndexJobDone), string(AtomicIndexJobDone), policy.MaxPerMemory)
	if err != nil {
		return 0, fmt.Errorf("prune done index jobs by cap: %w", err)
	}
	capped, err := resCap.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("prune done index jobs by cap: rows affected: %w", err)
	}

	return int(aged + capped), nil
}

// CanApplyDelete 判定一条 delete 任务是否允许清理向量索引（T13.02.c）。
// 拒绝情形：正文行存在且当前 revision 高于任务 revision——同 ID 已被更新
// 的 mutation 推进（复活后行已存活，或重复删除把墓碑推进到更高 revision），
// 旧删除意图不得清理新版本内容的索引/不得重复充当事实的清理动作。
// 行不存在（物理缺失）放行：删除幂等，清理残留索引正是任务目的；行已删
// 且 revision 与任务相同放行（提交后正常发布/重放路径）。
func (s *AtomicSyncStore) CanApplyDelete(ctx context.Context, memoryID string, revision int64) (bool, error) {
	if err := validateMemoryRevision(memoryID, revision); err != nil {
		return false, err
	}
	state, err := s.RevisionState(ctx, memoryID)
	if err != nil {
		return false, fmt.Errorf("can apply delete %s@%d: %w", memoryID, revision, err)
	}
	if !state.Exists {
		return true, nil
	}
	if state.Revision > revision {
		return false, nil
	}
	if !state.Deleted {
		// 行存活且 revision 未超过任务：删除意图没有对应 tombstone（不一致
		// 状态，如手工 enqueue），不得清理存活记忆的索引。
		return false, nil
	}
	return true, nil
}

// LatestJob 读取指定记忆 revision 最大的任务（不论状态）；不存在时返回
// (nil, nil)，用于同步状态查询回执。
func (s *AtomicSyncStore) LatestJob(ctx context.Context, memoryID string) (*AtomicIndexJob, error) {
	if strings.TrimSpace(memoryID) == "" {
		return nil, errors.New("memory id is required")
	}
	row := s.db.QueryRowContext(ensureContext(ctx), `
		SELECT memory_id, revision, operation, state, attempts, last_error, updated_at
		FROM atomic_index_jobs WHERE memory_id = ?
		ORDER BY revision DESC
		LIMIT 1
	`, memoryID)
	job, err := scanAtomicIndexJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("latest index job for %s: %w", memoryID, err)
	}
	return &job, nil
}

func validateMemoryRevision(memoryID string, revision int64) error {
	if strings.TrimSpace(memoryID) == "" {
		return errors.New("memory id is required")
	}
	if revision <= 0 {
		return errors.New("revision must be positive")
	}
	return nil
}

type atomicIndexJobScanner interface {
	Scan(dest ...interface{}) error
}

func scanAtomicIndexJob(scanner atomicIndexJobScanner) (AtomicIndexJob, error) {
	var (
		job       AtomicIndexJob
		operation string
		state     string
	)
	err := scanner.Scan(&job.MemoryID, &job.Revision, &operation, &state, &job.Attempts, &job.LastError, &job.UpdatedAt)
	if err != nil {
		return AtomicIndexJob{}, err
	}
	job.Operation = AtomicIndexOperation(operation)
	job.State = AtomicIndexJobState(state)
	return job, nil
}
