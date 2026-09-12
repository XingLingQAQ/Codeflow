package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	// ErrAtomicMemoryNotFound 原子记忆不存在。
	ErrAtomicMemoryNotFound = errors.New("atomic memory not found")
)

// AtomicIndexSyncState 是 mutation 回执中的索引同步状态（T13.02.c）：
// "正文已接受"（事务已提交）与向量索引的同步进度分开表达。
type AtomicIndexSyncState string

const (
	// AtomicIndexSyncPending 正文已提交，向量索引尚未同步完成（job 待
	// worker 重放）。
	AtomicIndexSyncPending AtomicIndexSyncState = "pending"
	// AtomicIndexSyncSynced 正文与向量索引均已就位（job done）。
	AtomicIndexSyncSynced AtomicIndexSyncState = "synced"
	// AtomicIndexSyncFailed 索引同步进入确定性终态 failed（attempts 上限
	// 或 stale 门禁）；索引与正文可能分叉。
	AtomicIndexSyncFailed AtomicIndexSyncState = "failed"
)

// AtomicMutationReceipt 是 Add/Update/Delete 的回执：返回成功即表示正文
// 事实已提交；IndexSync 分开报告向量索引的同步状态，IndexError 携带
// pending/failed 时 job 记录的最近错误。只查正文的读路径不产生本回执，
// 也不向调用方报告 indexed。
type AtomicMutationReceipt struct {
	ID         string
	Revision   int64
	IndexSync  AtomicIndexSyncState
	IndexError string
}

// AtomicMemoryUpdate 原子记忆更新参数。
type AtomicMemoryUpdate struct {
	Timestamp     *int64
	Content       *string
	Tags          *[]string
	SessionID     *string
	FolderID      *string
	ClearFolderID bool
	Source        *AtomicMemorySource
	Importance    *float64
	Embedding     *[]float64
	Tier          *MemoryTier
	Heat          *float64
	Surprise      *float64
}

// atomicSyncTx 抽象 mutation 路径使用的事务：SyncExecutor 加上提交/回滚。
// *sql.Tx 天然满足；测试用 commit fault fixture 在提交点注入故障。
type atomicSyncTx interface {
	SyncExecutor
	Commit() error
	Rollback() error
}

// atomicPublishLocks 是按 memory ID 的 keyed mutex（T13.02.fix1 缺陷 2 修复）。
// 提交后发布路径（publishUpsert/publishDelete，mutation 内联发布与索引
// worker 重放共用）在写向量前按 ID 取锁：同 ID 的两次发布串行，旧 revision
// 的发布不可能在物理上覆盖新 revision 的索引写入。
//
// 粒度与生命周期：锁粒度为单个 memory ID；条目带引用计数，最后一个解锁者
// 把条目从 map 移除，不随记忆数量泄漏。持有锁期间只做门禁/状态读取、向量
// I/O 与 job 状态写（单语句，受 busy_timeout 上限约束），不获取其他 keyed
// 锁、不开启/等待数据库事务、不做无上限阻塞调用——锁的获取顺序只有
// keyed lock → 向量库内部锁一个方向，不会成环，无死锁。
type atomicPublishLocks struct {
	mu    sync.Mutex
	locks map[string]*atomicPublishLockEntry
}

type atomicPublishLockEntry struct {
	mu   sync.Mutex
	refs int
}

// lock 获取指定 ID 的锁，返回解锁函数。
func (p *atomicPublishLocks) lock(id string) func() {
	p.mu.Lock()
	if p.locks == nil {
		p.locks = make(map[string]*atomicPublishLockEntry)
	}
	entry := p.locks[id]
	if entry == nil {
		entry = &atomicPublishLockEntry{}
		p.locks[id] = entry
	}
	entry.refs++
	p.mu.Unlock()

	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		p.mu.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(p.locks, id)
		}
		p.mu.Unlock()
	}
}

// AtomicMemoryService 原子记忆服务。
type AtomicMemoryService struct {
	db                *sql.DB
	vectorStore       IVectorStore
	embeddingProvider IEmbeddingProvider
	agentRole         string
	syncStore         *AtomicSyncStore
	beginTx           func(ctx context.Context) (atomicSyncTx, error)
	indexWorker       *atomicIndexWorker
	publishLocks      atomicPublishLocks
	// searchBatchSize/searchCandidateBudget 是 Search 候选续取的批量与
	// 工作量上限；<=0 时回退默认常量。测试注入小值以确定性覆盖多批续取
	// 与上限报告（T13.02.b part 3），生产不经 option 暴露。
	searchBatchSize       int
	searchCandidateBudget int
	// searchLogMu/lastIncompleteReason 给 Search 的"不完整原因"日志做去重，
	// 见 logIncompleteSearch。
	searchLogMu          sync.Mutex
	lastIncompleteReason string
}

// NewAtomicMemoryService 创建原子记忆服务。构造即完成正文同步 schema 迁移
// （revision/deleted 列与 atomic_index_jobs 表），生产 init 路径
// runtime.go 的 NewSQLiteAtomicMemoryService 经此获得同步存储。
// 构造同时完成索引同步的关闭责任接线（T13.02.b part 2）：先同步执行启动
// 恢复扫描补做遗留 pending job，再启动运行期索引 worker；Close 先停
// worker 再关存储。构造与运行期按保留策略清理 atomic_index_jobs 的 done
// 行（T13.02.c，§26.17）。测试可用 WithAtomicIndexWorkerConfig 注入短轮询/
// 低上限，或 WithAtomicIndexWorkerDisabled 关闭以确定性断言手工发布路径。
func NewAtomicMemoryService(ctx context.Context, db *sql.DB, vectorStore IVectorStore, embeddingProvider IEmbeddingProvider, opts ...AtomicMemoryServiceOption) (*AtomicMemoryService, error) {
	if db == nil {
		return nil, errors.New("atomic memory service init failed: db is nil")
	}
	if vectorStore == nil {
		return nil, errors.New("atomic memory service init failed: vector store is nil")
	}
	if embeddingProvider == nil {
		embeddingProvider = NewSimpleEmbeddingProvider(384)
	}

	ctx = ensureContext(ctx)
	syncStore, err := NewAtomicSyncStore(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("init atomic sync store: %w", err)
	}

	svc := &AtomicMemoryService{
		db:                db,
		vectorStore:       vectorStore,
		embeddingProvider: embeddingProvider,
		agentRole:         "atomic_memory",
		syncStore:         syncStore,
	}
	svc.beginTx = func(txCtx context.Context) (atomicSyncTx, error) {
		return db.BeginTx(txCtx, nil)
	}

	options := atomicMemoryServiceOptions{workerCfg: defaultAtomicIndexWorkerConfig()}
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}
	if !options.workerDisabled {
		svc.indexWorker = newAtomicIndexWorker(svc, options.workerCfg)
		svc.indexWorker.recoverPending(ctx)
		// 构造时先按保留策略清理一次 done 行（§26.17/T13.02.c），运行期
		// 由 worker 每 PruneInterval 周期执行。
		svc.indexWorker.pruneDoneJobs(ctx)
		svc.indexWorker.start()
	}
	return svc, nil
}

// Add 添加原子记忆（SQLite + 向量库）。
func (s *AtomicMemoryService) Add(ctx context.Context, mem *AtomicMemory) error {
	_, err := s.AddWithReceipt(ctx, mem)
	return err
}

// AddWithReceipt 同 Add，并返回 mutation 回执：返回成功即正文已提交，
// 回执的 IndexSync 报告提交后内联发布落地的索引同步状态（pending/failed/
// synced），二者分开表达（T13.02.c）。
func (s *AtomicMemoryService) AddWithReceipt(ctx context.Context, mem *AtomicMemory) (AtomicMutationReceipt, error) {
	if err := s.validateDependencies(); err != nil {
		return AtomicMutationReceipt{}, err
	}
	if mem == nil {
		return AtomicMutationReceipt{}, errors.New("add atomic memory: memory is nil")
	}

	ctx = ensureContext(ctx)
	if mem.Timestamp <= 0 {
		mem.Timestamp = time.Now().Unix()
	}
	if mem.Tier == "" {
		mem.Tier = MemoryTierHot
	}
	if mem.Heat <= 0 {
		mem.Heat = 1.0
	}
	if mem.Surprise <= 0 {
		mem.Surprise = 0.5
	}
	if len(mem.Embedding) == 0 {
		embedding, err := s.embeddingProvider.Embed(ctx, mem.Content)
		if err != nil {
			return AtomicMutationReceipt{}, fmt.Errorf("generate embedding: %w", err)
		}
		mem.Embedding = embedding
	}
	if err := mem.Validate(); err != nil {
		return AtomicMutationReceipt{}, fmt.Errorf("validate atomic memory: %w", err)
	}

	tagsJSON, err := mem.TagsJSON()
	if err != nil {
		return AtomicMutationReceipt{}, fmt.Errorf("encode tags json: %w", err)
	}
	embeddingJSON, err := mem.EmbeddingJSON()
	if err != nil {
		return AtomicMutationReceipt{}, fmt.Errorf("encode embedding json: %w", err)
	}

	tx, err := s.beginTx(ctx)
	if err != nil {
		return AtomicMutationReceipt{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// 正文 upsert 与索引 job 同一事务：新行 revision=1；命中已删 ID 的
	// tombstone 行时以 revision+1 复活（复活后的 revision 高于墓碑，允许
	// 重新发布索引）；命中存活行时不做任何写入（affected=0），报重复。
	res, err := tx.ExecContext(ctx, `
		INSERT INTO atomic_memories (
			id, timestamp, content, tags_json, session_id, folder_id,
			source, importance, embedding_json, vector_dim, tier, heat, surprise, revision, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, strftime('%s', 'now'))
		ON CONFLICT (id) DO UPDATE SET
			timestamp = excluded.timestamp,
			content = excluded.content,
			tags_json = excluded.tags_json,
			session_id = excluded.session_id,
			folder_id = excluded.folder_id,
			source = excluded.source,
			importance = excluded.importance,
			embedding_json = excluded.embedding_json,
			vector_dim = excluded.vector_dim,
			tier = excluded.tier,
			heat = excluded.heat,
			surprise = excluded.surprise,
			deleted = 0,
			revision = revision + 1,
			updated_at = strftime('%s', 'now')
		WHERE deleted = 1
	`,
		mem.ID,
		mem.Timestamp,
		mem.Content,
		tagsJSON,
		mem.SessionID,
		nullableString(mem.FolderID),
		string(mem.Source),
		mem.Importance,
		embeddingJSON,
		len(mem.Embedding),
		string(mem.Tier),
		mem.Heat,
		mem.Surprise,
	)
	if err != nil {
		return AtomicMutationReceipt{}, fmt.Errorf("insert atomic memory: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return AtomicMutationReceipt{}, fmt.Errorf("get rows affected: %w", err)
	}
	if affected == 0 {
		return AtomicMutationReceipt{}, fmt.Errorf("insert atomic memory: id %s already exists", mem.ID)
	}

	var revision int64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM atomic_memories WHERE id = ?`, mem.ID).Scan(&revision); err != nil {
		return AtomicMutationReceipt{}, fmt.Errorf("read atomic memory revision: %w", err)
	}
	if err := s.syncStore.EnqueueUpsertTx(ctx, tx, mem.ID, revision); err != nil {
		return AtomicMutationReceipt{}, err
	}
	if err := tx.Commit(); err != nil {
		return AtomicMutationReceipt{}, fmt.Errorf("commit atomic memory: %w", err)
	}

	// 提交成功后发布索引：向量失败只记入 job 供重试，正文事实不回滚。
	s.publishUpsert(ctx, mem, revision)
	return s.mutationReceipt(ctx, mem.ID, revision), nil
}

// mutationReceipt 读取刚提交的 (memory_id, revision) job 状态生成回执。
// 读 job 异常或缺失不翻转 mutation 已成功的事实：保守报 pending（绝不
// 把未确认的索引状态报成 synced）。
func (s *AtomicMemoryService) mutationReceipt(ctx context.Context, memoryID string, revision int64) AtomicMutationReceipt {
	receipt := AtomicMutationReceipt{ID: memoryID, Revision: revision, IndexSync: AtomicIndexSyncPending}
	job, err := s.syncStore.GetJob(ctx, memoryID, revision)
	if err != nil || job == nil {
		return receipt
	}
	receipt.IndexError = job.LastError
	switch job.State {
	case AtomicIndexJobDone:
		receipt.IndexSync = AtomicIndexSyncSynced
		receipt.IndexError = ""
	case AtomicIndexJobFailed:
		receipt.IndexSync = AtomicIndexSyncFailed
	}
	return receipt
}

const (
	// atomicSearchDefaultLimit Search 未指定 Limit 时的默认页大小（沿用旧行为 10）。
	atomicSearchDefaultLimit = 10
	// atomicSearchDefaultBatchSize 候选续取的初始批量，之后每批 TopK 翻倍。
	atomicSearchDefaultBatchSize = 64
	// atomicSearchDefaultCandidateBudget 单次 Search 的去重候选工作量上限：
	// 防止过滤条件极窄时续取退化为全库反复扫描。
	atomicSearchDefaultCandidateBudget = 2048
)

// AtomicSearchReport 一次 Search 的候选续取报告。终止原因必须分开读：
// Exhausted=true 表示向量候选已取尽，返回的是过滤后的完整集合（空页即
// 真空）；BudgetLimited=true 表示达到工作量上限，返回的是已扫描候选的
// 过滤前缀，更深处可能仍有匹配——调用方不得把它当完整空结果。
// IndexPendingJobs/IndexFailedJobs 是检索时"当前 revision 未同步"的索引
// job 数（T13.02.c）：>0 表示有正文已提交但向量未就位（或最新同步意图
// 终态失败），检索结果可能不完整；历史 stale 终态 job 不计入（语义见
// AtomicSyncStore.IndexSyncBacklog）。
type AtomicSearchReport struct {
	CandidatesScanned int  // 去重后的向量候选扫描数
	Batches           int  // 候选续取批次数
	Exhausted         bool // 向量候选已取尽（结果完整）
	BudgetLimited     bool // 达到工作量上限（结果为已扫描前缀，可能不完整）
	IndexPendingJobs  int  // 最新同步意图待完成的 job 数（正文已提交、向量未就位）
	IndexFailedJobs   int  // 最新同步意图终态失败的 job 数（索引与正文可能分叉）
}

// IncompleteReason 返回检索结果可能不完整的原因；结果完整时返回空串。
// 形状：分号连接的 "code=count" 片段，稳定可机读：
//
//	candidate_budget_limited=N —— 候选扫描达工作量上限，返回的是已扫描前缀；
//	index_sync_pending=N       —— N 条最新索引 job 待同步，正文已接受但向量未就位；
//	index_sync_failed=N        —— N 条最新索引 job 终态失败，索引与正文可能分叉。
func (r AtomicSearchReport) IncompleteReason() string {
	parts := make([]string, 0, 3)
	if r.BudgetLimited {
		parts = append(parts, fmt.Sprintf("candidate_budget_limited=%d", r.CandidatesScanned))
	}
	if r.IndexPendingJobs > 0 {
		parts = append(parts, fmt.Sprintf("index_sync_pending=%d", r.IndexPendingJobs))
	}
	if r.IndexFailedJobs > 0 {
		parts = append(parts, fmt.Sprintf("index_sync_failed=%d", r.IndexFailedJobs))
	}
	return strings.Join(parts, ";")
}

// Search 语义检索原子记忆（T13.02.b part 3，E-11 修复）。
//
// 顺序：先对向量候选按权威正文状态过滤（deleted=0 恒真；session/folder/
// time 精确条件下推正文 SQL；tags 由 SQL LIKE 收窄后经 matchAtomicFilters
// 精确复核），再按 score DESC + id ASC 稳定排序，最后按 offset/limit 页码
// 切片。向量 store 只支持 session 下推与 TopK 截断，候选按递增 TopK 有界
// 续取：每批新增候选经权威过滤后累计，直到凑满 limit+offset、候选耗尽或
// 达到工作量上限。达到上限时返回已扫描的真实过滤前缀并打 warning 日志，
// 不返回假空页；耗尽判定见 SearchWithReport。
func (s *AtomicMemoryService) Search(ctx context.Context, query string, opts *AtomicMemorySearchOptions) ([]AtomicMemory, error) {
	results, report, err := s.SearchWithReport(ctx, query, opts)
	if err != nil {
		return nil, err
	}
	s.logIncompleteSearch(report)
	return results, nil
}

// logIncompleteSearch 报告"结果可能不完整"，但只在原因发生变化时打一行。
//
// 不完整原因里的 index_sync_pending/index_sync_failed 是**全库**口径（未进
// 索引的记忆恰恰是检索看不见的那些，按本次候选收窄就失去了意义），所以只要
// 有一条 job 卡住，之后每一次 Search 的原因串都相同。逐次打印会让一条卡住的
// job 淹没日志；按原因去重后，稳态静默、状态变化时才有一行。
func (s *AtomicMemoryService) logIncompleteSearch(report AtomicSearchReport) {
	reason := report.IncompleteReason()
	s.searchLogMu.Lock()
	changed := reason != s.lastIncompleteReason
	s.lastIncompleteReason = reason
	s.searchLogMu.Unlock()
	if reason == "" || !changed {
		return
	}
	log.Printf("atomic memory search may be incomplete: %s; returned filtered prefix", reason)
}

// SearchWithReport 同 Search，并返回候选续取报告。续取终止条件按序判定：
// 某批返回数不足请求的 TopK 或未产生任何新候选 → 候选耗尽（Exhausted）；
// 过滤后累计满 limit+offset → 本页凑齐（正常返回，Exhausted 可同时成立）；
// 去重扫描数达到上限 → BudgetLimited。候选 ID 按批去重，同一 ID 不重复
// 计入扫描量；页内排序带 id tiebreaker，同分顺序确定，逐页遍历不漏不重。
// 排序分页前还有一次返回前的权威最新状态复核（T13.02.c）：批次过滤与返回
// 之间的并发删除不漏出，revision 已前进的候选刷新为最新正文。报告的
// IndexPendingJobs/IndexFailedJobs 表达"当前 revision 未同步"的索引 job
// 数，向量未同步时调用方必须能读到不完整原因（IncompleteReason），不得把
// 未同步的检索当成完整空结果。
func (s *AtomicMemoryService) SearchWithReport(ctx context.Context, query string, opts *AtomicMemorySearchOptions) ([]AtomicMemory, AtomicSearchReport, error) {
	var report AtomicSearchReport
	if err := s.validateDependencies(); err != nil {
		return nil, report, err
	}
	ctx = ensureContext(ctx)

	query = strings.TrimSpace(query)
	if query == "" {
		return []AtomicMemory{}, report, nil
	}

	limit, offset := atomicSearchPage(opts)
	target := limit + offset

	batchSize := s.searchBatchSize
	if batchSize <= 0 {
		batchSize = atomicSearchDefaultBatchSize
	}
	budget := s.searchCandidateBudget
	if budget <= 0 {
		budget = atomicSearchDefaultCandidateBudget
	}

	topK := maxInt(batchSize, target)
	if topK > budget {
		topK = budget
	}

	sessionFilter := ""
	if opts != nil {
		sessionFilter = opts.SessionID
	}

	seen := make(map[string]struct{})
	filtered := make([]atomicScoredMemory, 0, target)

	for {
		report.Batches++
		vectorResults, err := s.vectorStore.Search(ctx, query, &VectorSearchOptions{
			TopK:            topK,
			MinScore:        0,
			FilterSessionID: sessionFilter,
		})
		if err != nil {
			return nil, report, fmt.Errorf("vector search: %w", err)
		}

		newIDs := make([]string, 0, len(vectorResults))
		scoreByID := make(map[string]float64, len(vectorResults))
		for _, result := range vectorResults {
			id := result.Chunk.ID
			if id == "" {
				continue
			}
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			newIDs = append(newIDs, id)
			scoreByID[id] = result.Score
		}
		report.CandidatesScanned += len(newIDs)

		if len(newIDs) > 0 {
			memoryByID, err := s.filterCandidatesByIDs(ctx, newIDs, opts)
			if err != nil {
				return nil, report, err
			}
			for _, id := range newIDs {
				mem, ok := memoryByID[id]
				if !ok {
					continue // 正文不存在/已删/权威条件不满足（含索引残留）。
				}
				if !matchAtomicFilters(mem, opts) {
					continue // tags 精确复核：SQL LIKE 只做宽松收窄。
				}
				filtered = append(filtered, atomicScoredMemory{memory: mem, score: scoreByID[id]})
			}
		}

		// 本批返回不足请求的 TopK，或未产生任何新候选（同一查询的重查
		// 前缀单调，无新增即无更多）：候选耗尽，过滤结果是完整集合。
		if len(vectorResults) < topK || len(newIDs) == 0 {
			report.Exhausted = true
		}
		if len(filtered) >= target || report.Exhausted {
			break
		}
		if report.CandidatesScanned >= budget {
			report.BudgetLimited = true
			break
		}
		topK *= 2
		if topK > budget {
			topK = budget
		}
	}

	// 返回前的权威最新状态复核（T13.02.c）：批次过滤与返回之间存在并发
	// 删除/更新窗口，以一次新的正文读为准——已删除/不存在的项丢弃（不得
	// 漏出 tombstone），revision 已前进的项以最新行刷新正文后再排序分页。
	filtered, err := s.recheckFilteredAuthoritative(ctx, filtered, opts)
	if err != nil {
		return nil, report, err
	}

	// 稳定排序：score DESC 主键，id ASC tiebreaker（同分顺序确定）。
	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].score == filtered[j].score {
			return filtered[i].memory.ID < filtered[j].memory.ID
		}
		return filtered[i].score > filtered[j].score
	})

	page := make([]AtomicMemory, 0, limit)
	for i := offset; i < len(filtered) && len(page) < limit; i++ {
		page = append(page, filtered[i].memory)
	}

	// 未同步统计（T13.02.c）：有正文已提交但向量未就位（或最新同步意图
	// 终态失败）时，回执必须携带不完整原因，不得把未同步的检索当成完整
	// 空结果。统计口径（仅当前 revision 的 job）见 IndexSyncBacklog。
	pending, failed, err := s.syncStore.IndexSyncBacklog(ctx)
	if err != nil {
		return nil, report, fmt.Errorf("read index sync backlog: %w", err)
	}
	report.IndexPendingJobs = pending
	report.IndexFailedJobs = failed
	return page, report, nil
}

// recheckFilteredAuthoritative 在 Search 返回前对过滤后候选做最终权威
// 复核（T13.02.c 的"返回前再过滤权威最新状态"）：以一次新的正文读为准，
// 复核时已删除/不存在的项丢弃，revision 已前进的项以最新行刷新正文。
//
// 复核必须带上本次检索的 opts 并重跑 matchAtomicFilters，与批次过滤同一口径：
// 刷新正文和不收窄不能同时成立——若只 enforce deleted=0，批次过滤与返回之间
// 的并发 Update（session_id/folder_id/tags/timestamp 都是可更新字段）会把一条
// 已经不满足条件的记忆按最新内容放行，例如按 session=S1 检索却返回被改到 S2
// 的记忆。带 opts 复核后，这类候选在复核阶段与已删除项一样被丢弃。
func (s *AtomicMemoryService) recheckFilteredAuthoritative(ctx context.Context, filtered []atomicScoredMemory, opts *AtomicMemorySearchOptions) ([]atomicScoredMemory, error) {
	if len(filtered) == 0 {
		return filtered, nil
	}
	ids := make([]string, 0, len(filtered))
	for _, f := range filtered {
		ids = append(ids, f.memory.ID)
	}
	latestByID, err := s.filterCandidatesByIDs(ctx, ids, opts)
	if err != nil {
		return nil, err
	}
	rechecked := make([]atomicScoredMemory, 0, len(filtered))
	for _, f := range filtered {
		latest, ok := latestByID[f.memory.ID]
		if !ok {
			continue // 复核时已删除/不存在/已不满足检索条件：权威不可见，不得返回。
		}
		if !matchAtomicFilters(latest, opts) {
			continue // tags 精确复核：与批次过滤同一口径，SQL LIKE 只做宽松收窄。
		}
		rechecked = append(rechecked, atomicScoredMemory{memory: latest, score: f.score})
	}
	return rechecked, nil
}

// atomicScoredMemory 过滤后的候选及其向量分数（分页前排序用）。
type atomicScoredMemory struct {
	memory AtomicMemory
	score  float64
}

func atomicSearchPage(opts *AtomicMemorySearchOptions) (limit, offset int) {
	limit = atomicSearchDefaultLimit
	offset = 0
	if opts == nil {
		return limit, offset
	}
	if opts.Limit > 0 {
		limit = opts.Limit
	}
	if opts.Offset > 0 {
		offset = opts.Offset
	}
	return limit, offset
}

// SearchByTimeRange 按时间范围检索。
func (s *AtomicMemoryService) SearchByTimeRange(ctx context.Context, start, end int64) ([]AtomicMemory, error) {
	if err := s.validateDependencies(); err != nil {
		return nil, err
	}
	if start > end {
		return nil, errors.New("invalid time range: start is greater than end")
	}

	ctx = ensureContext(ctx)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, timestamp, content, tags_json, session_id, folder_id, source, importance, embedding_json, tier, heat, surprise
		FROM atomic_memories
		WHERE timestamp >= ? AND timestamp <= ? AND deleted = 0
		ORDER BY timestamp DESC, id ASC
	`, start, end)
	if err != nil {
		return nil, fmt.Errorf("query by time range: %w", err)
	}
	defer rows.Close()

	return scanAtomicMemories(rows)
}

// SearchByTags 按标签检索。
func (s *AtomicMemoryService) SearchByTags(ctx context.Context, tags []string) ([]AtomicMemory, error) {
	if err := s.validateDependencies(); err != nil {
		return nil, err
	}
	ctx = ensureContext(ctx)

	if len(tags) == 0 {
		return []AtomicMemory{}, nil
	}

	clauses := make([]string, 0, len(tags))
	params := make([]interface{}, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		clauses = append(clauses, `tags_json LIKE ?`)
		params = append(params, `%"`+tag+`"%`)
	}
	if len(clauses) == 0 {
		return []AtomicMemory{}, nil
	}

	query := `
		SELECT id, timestamp, content, tags_json, session_id, folder_id, source, importance, embedding_json, tier, heat, surprise
		FROM atomic_memories
		WHERE deleted = 0 AND (` + strings.Join(clauses, " OR ") + `)
		ORDER BY timestamp DESC, id ASC
	`

	rows, err := s.db.QueryContext(ctx, query, params...)
	if err != nil {
		return nil, fmt.Errorf("query by tags: %w", err)
	}
	defer rows.Close()

	return scanAtomicMemories(rows)
}

// GetBySession 获取会话下的原子记忆。
func (s *AtomicMemoryService) GetBySession(ctx context.Context, sessionID string, limit, offset int) ([]AtomicMemory, error) {
	if err := s.validateDependencies(); err != nil {
		return nil, err
	}
	ctx = ensureContext(ctx)

	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, errors.New("session id is required")
	}
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, timestamp, content, tags_json, session_id, folder_id, source, importance, embedding_json, tier, heat, surprise
		FROM atomic_memories
		WHERE session_id = ? AND deleted = 0
		ORDER BY timestamp DESC, id ASC
		LIMIT ? OFFSET ?
	`, sessionID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("query by session: %w", err)
	}
	defer rows.Close()

	return scanAtomicMemories(rows)
}

// GetByID 获取单条原子记忆。
func (s *AtomicMemoryService) GetByID(ctx context.Context, id string) (*AtomicMemory, error) {
	if err := s.validateDependencies(); err != nil {
		return nil, err
	}
	ctx = ensureContext(ctx)

	id = strings.TrimSpace(id)
	if id == "" {
		return nil, errors.New("id is required")
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, timestamp, content, tags_json, session_id, folder_id, source, importance, embedding_json, tier, heat, surprise
		FROM atomic_memories
		WHERE id = ? AND deleted = 0
	`, id)

	memory, err := scanAtomicMemoryRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAtomicMemoryNotFound
		}
		return nil, err
	}
	return &memory, nil
}

// Update 更新原子记忆并同步向量索引。
func (s *AtomicMemoryService) Update(ctx context.Context, id string, updates *AtomicMemoryUpdate) error {
	_, err := s.UpdateWithReceipt(ctx, id, updates)
	return err
}

// UpdateWithReceipt 同 Update，并返回 mutation 回执（语义同
// AddWithReceipt，T13.02.c）。
func (s *AtomicMemoryService) UpdateWithReceipt(ctx context.Context, id string, updates *AtomicMemoryUpdate) (AtomicMutationReceipt, error) {
	if err := s.validateDependencies(); err != nil {
		return AtomicMutationReceipt{}, err
	}
	if updates == nil {
		return AtomicMutationReceipt{}, errors.New("update atomic memory: updates is nil")
	}

	ctx = ensureContext(ctx)
	current, err := s.GetByID(ctx, id)
	if err != nil {
		return AtomicMutationReceipt{}, err
	}

	applyAtomicUpdates(current, updates)

	if updates.Content != nil && updates.Embedding == nil {
		embedding, embErr := s.embeddingProvider.Embed(ctx, current.Content)
		if embErr != nil {
			return AtomicMutationReceipt{}, fmt.Errorf("regenerate embedding: %w", embErr)
		}
		current.Embedding = embedding
	}

	if err := current.Validate(); err != nil {
		return AtomicMutationReceipt{}, fmt.Errorf("validate updated atomic memory: %w", err)
	}

	tagsJSON, err := current.TagsJSON()
	if err != nil {
		return AtomicMutationReceipt{}, fmt.Errorf("encode tags json: %w", err)
	}
	embeddingJSON, err := current.EmbeddingJSON()
	if err != nil {
		return AtomicMutationReceipt{}, fmt.Errorf("encode embedding json: %w", err)
	}

	tx, err := s.beginTx(ctx)
	if err != nil {
		return AtomicMutationReceipt{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// 正文更新与索引 job 同一事务：revision 前进一位；已删/不存在行
	// （affected=0）按未找到处理。tombstone 行不参与更新。
	res, err := tx.ExecContext(ctx, `
		UPDATE atomic_memories
		SET timestamp = ?,
			content = ?,
			tags_json = ?,
			session_id = ?,
			folder_id = ?,
			source = ?,
			importance = ?,
			embedding_json = ?,
			vector_dim = ?,
			tier = ?,
			heat = ?,
			surprise = ?,
			revision = revision + 1,
			updated_at = strftime('%s', 'now')
		WHERE id = ? AND deleted = 0
	`,
		current.Timestamp,
		current.Content,
		tagsJSON,
		current.SessionID,
		nullableString(current.FolderID),
		string(current.Source),
		current.Importance,
		embeddingJSON,
		len(current.Embedding),
		string(current.Tier),
		current.Heat,
		current.Surprise,
		current.ID,
	)
	if err != nil {
		return AtomicMutationReceipt{}, fmt.Errorf("update atomic memory: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return AtomicMutationReceipt{}, fmt.Errorf("get rows affected: %w", err)
	}
	if affected == 0 {
		return AtomicMutationReceipt{}, ErrAtomicMemoryNotFound
	}

	var revision int64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM atomic_memories WHERE id = ?`, id).Scan(&revision); err != nil {
		return AtomicMutationReceipt{}, fmt.Errorf("read atomic memory revision: %w", err)
	}
	if err := s.syncStore.EnqueueUpsertTx(ctx, tx, id, revision); err != nil {
		return AtomicMutationReceipt{}, err
	}
	if err := tx.Commit(); err != nil {
		return AtomicMutationReceipt{}, fmt.Errorf("commit atomic memory update: %w", err)
	}

	// 提交成功后发布索引：向量失败只记入 job 供重试，正文事实不回滚。
	s.publishUpsert(ctx, current, revision)
	return s.mutationReceipt(ctx, id, revision), nil
}

// Delete 删除原子记忆并移除向量索引。正文不物理删除：同一事务写
// tombstone（deleted=1、revision 前进）与 delete job，提交后再清理向量。
// 对已 tombstone 的 ID 重复删除是幂等的（revision 继续前进、补 delete
// job，即便正文不可见也继续清理索引残留）；对从未存在的 ID 报未找到。
//
// tombstone 写入是事务内的首条且即写单语句（UPDATE ... RETURNING，
// T13.02.fix1 缺陷 1 修复）：b1 的"事务内先 SELECT revision 再 UPDATE"在
// WAL 下构成 deferred 事务延迟升级——另一连接（如索引 worker 的
// MarkRetry/MarkDone）在读写之间提交时，升级 UPDATE 立即报
// SQLITE_BUSY_SNAPSHOT 且 busy handler 不生效（间歇 500 的根因）。单语句
// 在语句执行前才竞争写锁，冲突由 busy_timeout 正常等待覆盖，不存在可被
// 失效的快照。
func (s *AtomicMemoryService) Delete(ctx context.Context, id string) error {
	_, err := s.DeleteWithReceipt(ctx, id)
	return err
}

// DeleteWithReceipt 同 Delete，并返回 mutation 回执：返回成功即 tombstone
// 已提交（正文不可见），IndexSync 报告向量清理的同步状态（T13.02.c）。
func (s *AtomicMemoryService) DeleteWithReceipt(ctx context.Context, id string) (AtomicMutationReceipt, error) {
	if err := s.validateDependencies(); err != nil {
		return AtomicMutationReceipt{}, err
	}
	ctx = ensureContext(ctx)

	id = strings.TrimSpace(id)
	if id == "" {
		return AtomicMutationReceipt{}, errors.New("id is required")
	}

	tx, err := s.beginTx(ctx)
	if err != nil {
		return AtomicMutationReceipt{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	revision, err := s.syncStore.MarkDeletedReturningTx(ctx, tx, id)
	if err != nil {
		return AtomicMutationReceipt{}, err
	}
	if err := s.syncStore.EnqueueDeleteTx(ctx, tx, id, revision); err != nil {
		return AtomicMutationReceipt{}, err
	}
	if err := tx.Commit(); err != nil {
		return AtomicMutationReceipt{}, fmt.Errorf("commit atomic memory delete: %w", err)
	}

	// 提交成功后清理索引：向量失败只记入 job 供重试，tombstone 不回滚。
	s.publishDelete(ctx, id, revision)
	return s.mutationReceipt(ctx, id, revision), nil
}

// decayItem 是一行待提交的热度衰减结果。
type decayItem struct {
	id      string
	newHeat float64
}

// collectDecayItems 逐行扫描热度候选并计算衰减后的新热度。迭代结束核验
// rows.Err 并显式关闭结果集：中途故障返回错误，不静默截断成部分集合
// （E-12 / T13.02.b part 4）。tombstone 行会被扫到并保持现状语义：只更新
// heat/updated_at，不触碰 revision/deleted。
func collectDecayItems(rows *sql.Rows, now int64, halfLifeSeconds float64) ([]decayItem, error) {
	defer rows.Close()

	var items []decayItem
	for rows.Next() {
		var id string
		var heat float64
		var updatedAt int64
		if err := rows.Scan(&id, &heat, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan decay row: %w", err)
		}
		dt := float64(now - updatedAt)
		if dt <= 0 {
			continue
		}
		newHeat := heat * math.Pow(0.5, dt/halfLifeSeconds)
		if newHeat < 0.001 {
			newHeat = 0
		}
		items = append(items, decayItem{id: id, newHeat: newHeat})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate decay rows: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close decay rows: %w", err)
	}
	return items, nil
}

// ApplyHeatDecay 对所有原子记忆执行 Heat 衰减。
// Heat(t) = Heat(t-1) × 0.5^(Δt / HalfLife)
// 读迭代完整（rows.Err 核验、结果集关闭）后才开写事务；事务内逐行核验
// Exec 错误与 RowsAffected（恰好 1 行），任一失败整体回滚并返回错误；
// 成功返回实际提交行数，不回填计划处理数（E-12 / T13.02.b part 4）。
func (s *AtomicMemoryService) ApplyHeatDecay(ctx context.Context) (int, error) {
	if err := s.validateDependencies(); err != nil {
		return 0, err
	}
	ctx = ensureContext(ctx)

	halfLifeSeconds := float64(HeatHalfLifeDays * 24 * 3600)
	now := time.Now().Unix()

	// 读取所有 heat > 0.001 的记忆
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, heat, updated_at FROM atomic_memories WHERE heat > 0.001
	`)
	if err != nil {
		return 0, fmt.Errorf("query for decay: %w", err)
	}
	items, err := collectDecayItems(rows, now, halfLifeSeconds)
	if err != nil {
		return 0, err
	}

	if len(items) == 0 {
		return 0, nil
	}

	tx, err := s.beginTx(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin decay tx: %w", err)
	}
	defer tx.Rollback()

	committed := 0
	for _, item := range items {
		res, err := tx.ExecContext(ctx, `UPDATE atomic_memories SET heat = ?, updated_at = ? WHERE id = ?`, item.newHeat, now, item.id)
		if err != nil {
			return 0, fmt.Errorf("apply decay to %q: %w", item.id, err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("apply decay to %q: rows affected: %w", item.id, err)
		}
		if affected != 1 {
			return 0, fmt.Errorf("apply decay to %q: expected 1 row affected, got %d", item.id, affected)
		}
		committed++
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit heat decay: %w", err)
	}

	return committed, nil
}

// RecomputeTiers 根据 Heat 值重新分配层级。
// hot: heat >= 0.5, warm: 0.1 <= heat < 0.5, cold: heat < 0.1
func (s *AtomicMemoryService) RecomputeTiers(ctx context.Context) (int, error) {
	if err := s.validateDependencies(); err != nil {
		return 0, err
	}
	ctx = ensureContext(ctx)

	result, err := s.db.ExecContext(ctx, `
		UPDATE atomic_memories
		SET tier = CASE
			WHEN heat >= 0.5 THEN 'hot'
			WHEN heat >= 0.1 THEN 'warm'
			ELSE 'cold'
		END
		WHERE tier != CASE
			WHEN heat >= 0.5 THEN 'hot'
			WHEN heat >= 0.1 THEN 'warm'
			ELSE 'cold'
		END
	`)
	if err != nil {
		return 0, fmt.Errorf("recompute tiers: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("recompute tiers: rows affected: %w", err)
	}
	return int(affected), nil
}

// BoostHeat 提升指定记忆的 Heat（被访问时调用）。
func (s *AtomicMemoryService) BoostHeat(ctx context.Context, id string, boost float64) error {
	if err := s.validateDependencies(); err != nil {
		return err
	}
	if boost <= 0 {
		boost = 0.2
	}
	ctx = ensureContext(ctx)

	result, err := s.db.ExecContext(ctx, `
		UPDATE atomic_memories
		SET heat = MIN(1.0, heat + ?),
			tier = CASE
				WHEN MIN(1.0, heat + ?) >= 0.5 THEN 'hot'
				WHEN MIN(1.0, heat + ?) >= 0.1 THEN 'warm'
				ELSE 'cold'
			END,
			updated_at = strftime('%s', 'now')
		WHERE id = ?
	`, boost, boost, boost, id)
	if err != nil {
		return err
	}
	// RowsAffected 的驱动错误要核验；目标行不存在（affected=0）维持 no-op
	// 成功语义——该语义已经主 Agent 裁定保持（§26.19）：boost 是"被访问时"
	// 的提示性加热，目标缺失不构成调用方错误，改报错会变 HTTP 可见契约。
	if _, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("boost heat: rows affected: %w", err)
	}
	return nil
}

// SearchByTier 按层级检索。
func (s *AtomicMemoryService) SearchByTier(ctx context.Context, tier MemoryTier, limit int) ([]AtomicMemory, error) {
	if err := s.validateDependencies(); err != nil {
		return nil, err
	}
	ctx = ensureContext(ctx)
	if limit <= 0 {
		limit = 50
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, timestamp, content, tags_json, session_id, folder_id, source, importance, embedding_json, tier, heat, surprise
		FROM atomic_memories
		WHERE tier = ? AND deleted = 0
		ORDER BY heat DESC, id ASC
		LIMIT ?
	`, string(tier), limit)
	if err != nil {
		return nil, fmt.Errorf("query by tier: %w", err)
	}
	defer rows.Close()

	return scanAtomicMemories(rows)
}

// publishUpsert 在正文事务提交成功后发布 upsert 索引：先过 tombstone 门禁
// （晚到的旧 revision 不得复活已删正文），再写入向量。向量写成功标 done；
// 失败只记 job retry（保持 pending 供 worker 重放），不回滚正文事实。
// 门禁拒绝时任务留 pending，由 part 2 的 worker 对账，不在此处标 done。
//
// 按 ID 串行与 MarkDone 前复查（T13.02.fix1 缺陷 2 修复）：mutation 内联
// 发布与 worker 重放共用本路径，全程持有该 memory ID 的 keyed 锁，同 ID 的
// 两次发布物理上串行，旧 revision 不可能交错覆盖新 revision 的索引写入。
// 锁只串行发布路径，mutation 提交不经此锁，因此向量写完后、MarkDone 前再
// 复查"刚写入的 revision 仍是该 ID 最新"：若行已删/已推进到更新 revision，
// 不标 done、留 pending，由更新 revision 的发布（内联或 worker 重放）收敛
// 索引，消除"正文 N+1 / 向量 N 且无 pending"的永久分叉。
func (s *AtomicMemoryService) publishUpsert(ctx context.Context, mem *AtomicMemory, revision int64) {
	unlock := s.publishLocks.lock(mem.ID)
	defer unlock()

	allowed, err := s.syncStore.CanApplyUpsert(ctx, mem.ID, revision)
	if err != nil {
		_ = s.syncStore.MarkRetry(ctx, mem.ID, revision, err)
		return
	}
	if !allowed {
		return
	}
	if err := s.replaceVector(ctx, mem); err != nil {
		_ = s.syncStore.MarkRetry(ctx, mem.ID, revision, err)
		return
	}
	latest, err := s.syncStore.RevisionState(ctx, mem.ID)
	if err != nil {
		_ = s.syncStore.MarkRetry(ctx, mem.ID, revision, err)
		return
	}
	// 已落后（行被更新 mutation 推进到更高 revision）或已删/消失：不标
	// done。Revision < 任务 revision 是合法的"任务先行"（如恢复扫描/手工
	// enqueue 的意向 revision 尚未落正文），写入的本就是当前最新正文内容，
	// 标 done 安全。
	if !latest.Exists || latest.Deleted || latest.Revision > revision {
		return
	}
	if err := s.syncStore.MarkDone(ctx, mem.ID, revision); err != nil {
		_ = s.syncStore.MarkRetry(ctx, mem.ID, revision, err)
	}
}

// publishDelete 在 tombstone 提交成功后清理向量索引：删除幂等，成功标
// done，失败只记 job retry（保持 pending 供 worker 重放），tombstone 不回滚。
// 与 publishUpsert 同一 keyed 锁（T13.02.fix1 缺陷 2 修复）：同 ID 的
// upsert/delete 发布物理串行；MarkDone 前复查 tombstone revision 仍是本任务
// 的 revision（重复删除/复活可能已推进），已落后则不标 done、留 pending。
//
// 删除侧 stale 门禁（T13.02.c）：先过 CanApplyDelete——正文行已被更新的
// mutation 推进（复活/重复删除）时，旧删除意图不得清理新版本内容的索引，
// 留 pending 由 worker 对账为 stale 终态。否则"删 rev2 留 pending → 同 ID
// 复活 rev3 已发布 → worker 重放 rev2 删除"会把 rev3 的向量误删成永久分叉。
func (s *AtomicMemoryService) publishDelete(ctx context.Context, memoryID string, revision int64) {
	unlock := s.publishLocks.lock(memoryID)
	defer unlock()

	allowed, err := s.syncStore.CanApplyDelete(ctx, memoryID, revision)
	if err != nil {
		_ = s.syncStore.MarkRetry(ctx, memoryID, revision, err)
		return
	}
	if !allowed {
		return
	}
	if err := s.vectorStore.Delete(ctx, []string{memoryID}); err != nil {
		_ = s.syncStore.MarkRetry(ctx, memoryID, revision, err)
		return
	}
	latest, err := s.syncStore.RevisionState(ctx, memoryID)
	if err != nil {
		_ = s.syncStore.MarkRetry(ctx, memoryID, revision, err)
		return
	}
	// 墓碑已推进到更高 revision（重复删除/复活再删）：本任务已落后，不标
	// done、留 pending 由 worker 对账。Revision < 任务 revision 同样是合法
	// 的"任务先行"，删除幂等，标 done 安全。
	if !latest.Exists || !latest.Deleted || latest.Revision > revision {
		return
	}
	if err := s.syncStore.MarkDone(ctx, memoryID, revision); err != nil {
		_ = s.syncStore.MarkRetry(ctx, memoryID, revision, err)
	}
}

func (s *AtomicMemoryService) validateDependencies() error {
	if s == nil {
		return errors.New("atomic memory service is nil")
	}
	if s.db == nil {
		return errors.New("atomic memory service db is nil")
	}
	if s.vectorStore == nil {
		return errors.New("atomic memory service vector store is nil")
	}
	if s.embeddingProvider == nil {
		return errors.New("atomic memory service embedding provider is nil")
	}
	return nil
}

func (s *AtomicMemoryService) indexVector(ctx context.Context, mem *AtomicMemory) error {
	chunk := buildAtomicMemoryChunk(mem, s.agentRole)
	return s.vectorStore.Add(ctx, []DocumentChunk{chunk})
}

func (s *AtomicMemoryService) replaceVector(ctx context.Context, mem *AtomicMemory) error {
	if err := s.vectorStore.Delete(ctx, []string{mem.ID}); err != nil {
		return err
	}
	return s.indexVector(ctx, mem)
}

// filterCandidatesByIDs 在正文库对向量候选做权威过滤：deleted=0 恒真；
// session/folder/time 精确条件下推 SQL；tags 以 LIKE 宽松收窄（SQLite LIKE
// 对 ASCII 大小写不敏感、含通配符时只会多收不漏收，精确语义由调用处的
// matchAtomicFilters 复核）。候选可能含索引残留 ID（正文不存在或已
// tombstone），由本查询自然滤除。
func (s *AtomicMemoryService) filterCandidatesByIDs(ctx context.Context, ids []string, opts *AtomicMemorySearchOptions) (map[string]AtomicMemory, error) {
	uniqueIDs := dedupeOrderedIDs(ids)
	if len(uniqueIDs) == 0 {
		return map[string]AtomicMemory{}, nil
	}

	conditions := []string{"deleted = 0"}
	params := make([]interface{}, 0, len(uniqueIDs)+6)
	if opts != nil {
		if opts.SessionID != "" {
			conditions = append(conditions, "session_id = ?")
			params = append(params, opts.SessionID)
		}
		if opts.FolderID != "" {
			conditions = append(conditions, "folder_id = ?")
			params = append(params, opts.FolderID)
		}
		if opts.StartAt != nil {
			conditions = append(conditions, "timestamp >= ?")
			params = append(params, *opts.StartAt)
		}
		if opts.EndAt != nil {
			conditions = append(conditions, "timestamp <= ?")
			params = append(params, *opts.EndAt)
		}
		tagClauses := make([]string, 0, len(opts.Tags))
		for _, tag := range opts.Tags {
			tag = strings.TrimSpace(tag)
			if tag == "" {
				continue
			}
			tagClauses = append(tagClauses, `tags_json LIKE ?`)
			params = append(params, `%"`+tag+`"%`)
		}
		if len(tagClauses) > 0 {
			conditions = append(conditions, "("+strings.Join(tagClauses, " OR ")+")")
		}
	}

	placeholders := make([]string, len(uniqueIDs))
	for i, id := range uniqueIDs {
		placeholders[i] = "?"
		params = append(params, id)
	}
	conditions = append(conditions, "id IN ("+strings.Join(placeholders, ",")+")")

	query := `
		SELECT id, timestamp, content, tags_json, session_id, folder_id, source, importance, embedding_json, tier, heat, surprise
		FROM atomic_memories
		WHERE ` + strings.Join(conditions, " AND ")
	rows, err := s.db.QueryContext(ctx, query, params...)
	if err != nil {
		return nil, fmt.Errorf("filter atomic memory candidates: %w", err)
	}
	defer rows.Close()

	items, err := scanAtomicMemories(rows)
	if err != nil {
		return nil, err
	}

	memoryByID := make(map[string]AtomicMemory, len(items))
	for _, item := range items {
		memoryByID[item.ID] = item
	}
	return memoryByID, nil
}

func applyAtomicUpdates(current *AtomicMemory, updates *AtomicMemoryUpdate) {
	if updates.Timestamp != nil {
		current.Timestamp = *updates.Timestamp
	}
	if updates.Content != nil {
		current.Content = strings.TrimSpace(*updates.Content)
	}
	if updates.Tags != nil {
		current.Tags = *updates.Tags
	}
	if updates.SessionID != nil {
		current.SessionID = strings.TrimSpace(*updates.SessionID)
	}
	if updates.ClearFolderID {
		current.FolderID = nil
	} else if updates.FolderID != nil {
		folderID := strings.TrimSpace(*updates.FolderID)
		current.FolderID = &folderID
	}
	if updates.Source != nil {
		current.Source = *updates.Source
	}
	if updates.Importance != nil {
		current.Importance = *updates.Importance
	}
	if updates.Embedding != nil {
		current.Embedding = *updates.Embedding
	}
	if updates.Tier != nil {
		current.Tier = *updates.Tier
	}
	if updates.Heat != nil {
		current.Heat = *updates.Heat
	}
	if updates.Surprise != nil {
		current.Surprise = *updates.Surprise
	}
}

func matchAtomicFilters(mem AtomicMemory, opts *AtomicMemorySearchOptions) bool {
	if opts == nil {
		return true
	}
	if opts.FolderID != "" {
		if mem.FolderID == nil || *mem.FolderID != opts.FolderID {
			return false
		}
	}
	if len(opts.Tags) > 0 && !containsAnyTag(mem.Tags, opts.Tags) {
		return false
	}
	if opts.StartAt != nil && mem.Timestamp < *opts.StartAt {
		return false
	}
	if opts.EndAt != nil && mem.Timestamp > *opts.EndAt {
		return false
	}
	return true
}

func containsAnyTag(memoryTags []string, filterTags []string) bool {
	if len(filterTags) == 0 {
		return true
	}
	tagSet := make(map[string]struct{}, len(memoryTags))
	for _, tag := range memoryTags {
		t := strings.TrimSpace(tag)
		if t == "" {
			continue
		}
		tagSet[t] = struct{}{}
	}
	for _, tag := range filterTags {
		t := strings.TrimSpace(tag)
		if t == "" {
			continue
		}
		if _, ok := tagSet[t]; ok {
			return true
		}
	}
	return false
}

func scanAtomicMemories(rows *sql.Rows) ([]AtomicMemory, error) {
	items := make([]AtomicMemory, 0)
	for rows.Next() {
		item, err := scanAtomicMemoryRow(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func scanAtomicMemoryRow(scanner interface {
	Scan(dest ...interface{}) error
}) (AtomicMemory, error) {
	var (
		item          AtomicMemory
		tagsJSON      string
		folderID      sql.NullString
		source        string
		embeddingJSON sql.NullString
		tier          string
	)

	err := scanner.Scan(
		&item.ID,
		&item.Timestamp,
		&item.Content,
		&tagsJSON,
		&item.SessionID,
		&folderID,
		&source,
		&item.Importance,
		&embeddingJSON,
		&tier,
		&item.Heat,
		&item.Surprise,
	)
	if err != nil {
		return AtomicMemory{}, err
	}

	item.Source = AtomicMemorySource(source)
	item.Tier = MemoryTier(tier)
	if item.Tier == "" {
		item.Tier = MemoryTierHot
	}
	if folderID.Valid {
		v := folderID.String
		item.FolderID = &v
	}

	if err := json.Unmarshal([]byte(tagsJSON), &item.Tags); err != nil {
		return AtomicMemory{}, fmt.Errorf("unmarshal tags_json: %w", err)
	}
	if embeddingJSON.Valid && strings.TrimSpace(embeddingJSON.String) != "" {
		if err := json.Unmarshal([]byte(embeddingJSON.String), &item.Embedding); err != nil {
			return AtomicMemory{}, fmt.Errorf("unmarshal embedding_json: %w", err)
		}
	}

	return item, nil
}

func buildAtomicMemoryChunk(mem *AtomicMemory, agentRole string) DocumentChunk {
	return DocumentChunk{
		ID:      mem.ID,
		Content: mem.Content,
		Metadata: ChunkMetadata{
			SessionID:    mem.SessionID,
			AgentRole:    agentRole,
			MessageIndex: 0,
			ChunkIndex:   0,
			Timestamp:    mem.Timestamp,
			Source:       SourceType(mem.Source),
		},
	}
}

func dedupeOrderedIDs(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	ordered := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ordered = append(ordered, id)
	}
	return ordered
}

func nullableString(v *string) sql.NullString {
	if v == nil {
		return sql.NullString{}
	}
	value := strings.TrimSpace(*v)
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}

func ensureContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
