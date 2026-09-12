package memory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// T13.02.b part 3：权威过滤 + 稳定排序 + 分页（E-11 修复）测试
// scriptedVectorStore 提供完全可控的候选顺序/分数，配合真实正文库构造
// "高分不匹配、低分匹配、跨页过滤、候选耗尽、达到工作量上限"的确定性场景。
// worker 全部关闭：断言只涉及读路径与候选续取，不涉及 job 消费。
// ---------------------------------------------------------------------------

// scriptedVectorStore 确定性向量 store：Search 按脚本顺序返回候选，TopK
// 截断与 FilterSessionID 下推语义同 SQLiteVectorStore（同一查询前缀单调）。
// ignoreSessionFilter=true 时不过滤 session，用于验证正文 SQL 的 session
// 权威复核（向量下推只是收窄，不是唯一防线）。
type scriptedVectorStore struct {
	mu                  sync.Mutex
	chunks              []DocumentChunk
	scores              map[string]float64
	ignoreSessionFilter bool
	topKs               []int
	// onSearch 在第 call 次 Search 返回前回调（call 从 1 开始），供测试在
	// "某个批次已接受候选"与"返回前权威复核"之间注入并发 mutation。
	onSearch    func(call int)
	searchCalls int
}

func (s *scriptedVectorStore) Search(_ context.Context, _ string, opts *VectorSearchOptions) ([]VectorSearchResult, error) {
	s.mu.Lock()

	topK := 10
	sessionFilter := ""
	if opts != nil {
		if opts.TopK > 0 {
			topK = opts.TopK
		}
		sessionFilter = opts.FilterSessionID
	}
	s.topKs = append(s.topKs, topK)
	s.searchCalls++
	call := s.searchCalls
	hook := s.onSearch

	results := make([]VectorSearchResult, 0, topK)
	for _, chunk := range s.chunks {
		if !s.ignoreSessionFilter && sessionFilter != "" && chunk.Metadata.SessionID != sessionFilter {
			continue
		}
		results = append(results, VectorSearchResult{Chunk: chunk, Score: s.scores[chunk.ID]})
		if len(results) >= topK {
			break
		}
	}
	s.mu.Unlock()

	// 锁外回调：注入的 mutation 会经发布路径回调本 store 的 Add/Delete。
	if hook != nil {
		hook(call)
	}
	return results, nil
}

func (s *scriptedVectorStore) Add(_ context.Context, _ []DocumentChunk) error { return nil }
func (s *scriptedVectorStore) Delete(_ context.Context, _ []string) error     { return nil }
func (s *scriptedVectorStore) Clear(_ context.Context) error                  { return nil }

func (s *scriptedVectorStore) GetBySessionID(_ context.Context, sessionID string) ([]DocumentChunk, error) {
	out := make([]DocumentChunk, 0)
	for _, chunk := range s.chunks {
		if chunk.Metadata.SessionID == sessionID {
			out = append(out, chunk)
		}
	}
	return out, nil
}

func (s *scriptedVectorStore) GetByGitCommit(_ context.Context, _ string) ([]DocumentChunk, error) {
	return nil, nil
}

func (s *scriptedVectorStore) Count(_ context.Context) (int, error) { return len(s.chunks), nil }

func (s *scriptedVectorStore) GetCollectionInfo(_ context.Context) (*CollectionInfo, error) {
	return &CollectionInfo{Name: "scripted", Count: len(s.chunks)}, nil
}

func (s *scriptedVectorStore) Close() error { return nil }

// setupScriptedSearchService 真实正文库 + 脚本化向量 store，worker 关闭。
func setupScriptedSearchService(t *testing.T) (*AtomicMemoryService, *scriptedVectorStore, func()) {
	t.Helper()
	db, cleanupDB := setupAtomicServiceTestDB(t)
	store := &scriptedVectorStore{scores: map[string]float64{}}
	svc, err := NewAtomicMemoryService(context.Background(), db, store, NewSimpleEmbeddingProvider(32), WithAtomicIndexWorkerDisabled())
	if err != nil {
		cleanupDB()
		t.Fatalf("create AtomicMemoryService failed: %v", err)
	}
	return svc, store, cleanupDB
}

func searchTestMemory(id, sessionID string, timestamp int64, tags []string, folderID *string) *AtomicMemory {
	return &AtomicMemory{
		ID:         id,
		Timestamp:  timestamp,
		Content:    "检索测试正文 " + id,
		Tags:       tags,
		SessionID:  sessionID,
		FolderID:   folderID,
		Source:     AtomicMemorySourceUser,
		Importance: 0.5,
	}
}

// addSearchCandidate 走真实写路径落正文，并把该 ID 按期望名次追加到脚本
// 候选（调用顺序即向量 score 降序名次）。
func addSearchCandidate(t *testing.T, svc *AtomicMemoryService, store *scriptedVectorStore, mem *AtomicMemory, score float64) {
	t.Helper()
	if err := svc.Add(context.Background(), mem); err != nil {
		t.Fatalf("Add %s failed: %v", mem.ID, err)
	}
	store.chunks = append(store.chunks, DocumentChunk{
		ID:      mem.ID,
		Content: mem.Content,
		Metadata: ChunkMetadata{
			SessionID: mem.SessionID,
			Timestamp: mem.Timestamp,
		},
	})
	store.scores[mem.ID] = score
}

func pageIDs(page []AtomicMemory) []string {
	ids := make([]string, 0, len(page))
	for _, mem := range page {
		ids = append(ids, mem.ID)
	}
	return ids
}

func assertIDOrder(t *testing.T, got []AtomicMemory, want []string) {
	t.Helper()
	ids := pageIDs(got)
	if len(ids) != len(want) {
		t.Fatalf("expected ids %v, got %v", want, ids)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("expected ids %v, got %v", want, ids)
		}
	}
}

// cloneSearchOpts 复制过滤条件并替换分页位置（逐页遍历用）。
func cloneSearchOpts(opts *AtomicMemorySearchOptions, offset int) *AtomicMemorySearchOptions {
	next := *opts
	next.Offset = offset
	return &next
}

// ---------------------------------------------------------------------------
// 已知数据集逐页遍历：不漏、不重、边界页正确
// ---------------------------------------------------------------------------

func TestAtomicSearchFilteredPaginationNoMissNoDup(t *testing.T) {
	svc, store, cleanup := setupScriptedSearchService(t)
	defer cleanup()
	ctx := context.Background()

	folderA := "folder-a"
	folderB := "folder-b"
	start := int64(100)
	end := int64(200)

	// 候选按 score 降序入脚本；命中全部过滤条件的只有 m1/m3/m5/m7/m10。
	folderARef, folderBRef := &folderA, &folderB
	candidates := []struct {
		id      string
		session string
		folder  *string
		tags    []string
		ts      int64
		score   float64
	}{
		{"m1", "sf-a", folderARef, []string{"t1"}, 150, 0.95},
		{"m2", "sf-a", folderBRef, []string{"t1"}, 150, 0.90}, // folder 不符
		{"m3", "sf-a", folderARef, []string{"t1", "t2"}, 120, 0.85},
		{"m4", "sf-a", folderARef, []string{"t2"}, 150, 0.80}, // tag 不符
		{"m5", "sf-a", folderARef, []string{"t1"}, 200, 0.75},
		{"m6", "sf-b", folderARef, []string{"t1"}, 150, 0.70}, // session 不符
		{"m7", "sf-a", folderARef, []string{"t1"}, 100, 0.65},
		{"m8", "sf-a", folderARef, []string{"t1"}, 250, 0.60}, // 时间窗外
		{"m9", "sf-a", folderARef, []string{"t1"}, 180, 0.55}, // 命中但随后删除
		{"m10", "sf-a", folderARef, []string{"t1"}, 160, 0.50},
	}
	for _, c := range candidates {
		addSearchCandidate(t, svc, store, searchTestMemory(c.id, c.session, c.ts, c.tags, c.folder), c.score)
	}
	if err := svc.Delete(ctx, "m9"); err != nil {
		t.Fatalf("Delete m9 failed: %v", err)
	}
	// 索引残留：向量候选里有、正文从未存在，必须被权威过滤剔除。
	store.chunks = append(store.chunks, DocumentChunk{
		ID:       "phantom",
		Content:  "索引残留",
		Metadata: ChunkMetadata{SessionID: "sf-a"},
	})
	store.scores["phantom"] = 0.52

	baseOpts := &AtomicMemorySearchOptions{
		SessionID: "sf-a",
		FolderID:  folderA,
		Tags:      []string{"t1"},
		StartAt:   &start,
		EndAt:     &end,
		Limit:     2,
	}
	wantPages := [][]string{
		{"m1", "m3"},
		{"m5", "m7"},
		{"m10"},
		{},
	}

	seen := make(map[string]int)
	for pageIdx, want := range wantPages {
		offset := pageIdx * 2
		page, report, err := svc.SearchWithReport(ctx, "检索测试", cloneSearchOpts(baseOpts, offset))
		if err != nil {
			t.Fatalf("page %d SearchWithReport failed: %v", pageIdx, err)
		}
		assertIDOrder(t, page, want)
		if report.BudgetLimited {
			t.Fatalf("page %d must not be budget limited (default budget), report=%+v", pageIdx, report)
		}
		if !report.Exhausted {
			t.Fatalf("page %d: candidates must be exhausted in a single batch, report=%+v", pageIdx, report)
		}
		for _, id := range pageIDs(page) {
			seen[id]++
			if seen[id] > 1 {
				t.Fatalf("duplicate id %s across pages", id)
			}
		}
	}

	// 不漏：全部命中 ID 恰好覆盖一遍。
	if len(seen) != 5 {
		t.Fatalf("expected 5 unique matched ids across pages, got %v", seen)
	}
	for _, id := range []string{"m1", "m3", "m5", "m7", "m10"} {
		if seen[id] != 1 {
			t.Fatalf("expected %s exactly once, seen=%v", id, seen)
		}
	}
}

// ---------------------------------------------------------------------------
// E-11 负向：第一页候选全被过滤时，续取必须拿到后面的真实匹配（假空页回归）
// ---------------------------------------------------------------------------

func TestAtomicSearchResumePastFilteredCandidatesNoFakeEmptyPage(t *testing.T) {
	svc, store, cleanup := setupScriptedSearchService(t)
	defer cleanup()
	ctx := context.Background()

	// 小批量强制多批续取：TopK 序列 2 -> 4 -> 8 -> 16。
	svc.searchBatchSize = 2
	svc.searchCandidateBudget = 100

	folderA := "folder-a"
	folderB := "folder-b"
	// 前 6 名候选全部 folder-b（被过滤）；匹配项 n7..n10 在更深处。
	for i := 1; i <= 10; i++ {
		folder := &folderB
		if i >= 7 {
			folder = &folderA
		}
		id := fmt.Sprintf("n%d", i)
		addSearchCandidate(t, svc, store, searchTestMemory(id, "sf-a", int64(100+i), []string{"t1"}, folder), 1.0-float64(i)*0.01)
	}

	opts := &AtomicMemorySearchOptions{FolderID: folderA, Limit: 2}

	// 第一页：旧实现 TopK=2 只取 n1/n2，过滤后返回假空；现在必须续取到 n7/n8。
	page1, report1, err := svc.SearchWithReport(ctx, "检索测试", opts)
	if err != nil {
		t.Fatalf("page 1 failed: %v", err)
	}
	assertIDOrder(t, page1, []string{"n7", "n8"})
	if report1.Exhausted || report1.BudgetLimited {
		t.Fatalf("page 1 must be a full page without exhaustion/budget, report=%+v", report1)
	}
	if report1.Batches != 3 || report1.CandidatesScanned != 8 {
		t.Fatalf("page 1 expected 3 batches / 8 candidates scanned, report=%+v", report1)
	}

	// 跨页过滤：第二页同样是真实结果，不是假空。
	page2, report2, err := svc.SearchWithReport(ctx, "检索测试", cloneSearchOpts(opts, 2))
	if err != nil {
		t.Fatalf("page 2 failed: %v", err)
	}
	assertIDOrder(t, page2, []string{"n9", "n10"})
	if !report2.Exhausted || report2.BudgetLimited {
		t.Fatalf("page 2 expected exhaustion after full scan, report=%+v", report2)
	}

	// 第三页：匹配总共 4 条，空页是真空（候选已耗尽）。
	page3, report3, err := svc.SearchWithReport(ctx, "检索测试", cloneSearchOpts(opts, 4))
	if err != nil {
		t.Fatalf("page 3 failed: %v", err)
	}
	if len(page3) != 0 {
		t.Fatalf("page 3 must be empty (only 4 matches), got %v", pageIDs(page3))
	}
	if !report3.Exhausted || report3.BudgetLimited {
		t.Fatalf("page 3 empty page must come from exhaustion, report=%+v", report3)
	}

	// 旧签名 Search 行为一致（报告退化进日志，结果相同）。
	legacy, err := svc.Search(ctx, "检索测试", opts)
	if err != nil {
		t.Fatalf("legacy Search failed: %v", err)
	}
	assertIDOrder(t, legacy, []string{"n7", "n8"})
}

// ---------------------------------------------------------------------------
// "候选耗尽" 与 "达到工作量上限" 必须分开报告
// ---------------------------------------------------------------------------

func TestAtomicSearchExhaustionVsBudgetLimitReporting(t *testing.T) {
	folderA := "folder-a"
	folderB := "folder-b"

	t.Run("exhausted empty page is a true empty", func(t *testing.T) {
		svc, store, cleanup := setupScriptedSearchService(t)
		defer cleanup()

		// 3 个候选全不匹配；默认批量一批取尽。
		for i := 1; i <= 3; i++ {
			id := fmt.Sprintf("e%d", i)
			addSearchCandidate(t, svc, store, searchTestMemory(id, "sf-a", int64(100+i), []string{"t1"}, &folderB), 0.9-float64(i)*0.01)
		}
		page, report, err := svc.SearchWithReport(context.Background(), "检索测试", &AtomicMemorySearchOptions{FolderID: folderA, Limit: 5})
		if err != nil {
			t.Fatalf("SearchWithReport failed: %v", err)
		}
		if len(page) != 0 {
			t.Fatalf("expected empty page, got %v", pageIDs(page))
		}
		if !report.Exhausted || report.BudgetLimited {
			t.Fatalf("expected exhausted=true budgetLimited=false, report=%+v", report)
		}
		if report.Batches != 1 || report.CandidatesScanned != 3 {
			t.Fatalf("expected 1 batch / 3 candidates, report=%+v", report)
		}
	})

	t.Run("budget limit before any match is not reported as empty truth", func(t *testing.T) {
		svc, store, cleanup := setupScriptedSearchService(t)
		defer cleanup()

		// 匹配在 rank 9-10；budget=4、batch=2 → 扫完 4 个候选即达上限。
		svc.searchBatchSize = 2
		svc.searchCandidateBudget = 4
		for i := 1; i <= 10; i++ {
			folder := &folderB
			if i >= 9 {
				folder = &folderA
			}
			id := fmt.Sprintf("b%d", i)
			addSearchCandidate(t, svc, store, searchTestMemory(id, "sf-a", int64(100+i), []string{"t1"}, folder), 1.0-float64(i)*0.01)
		}
		page, report, err := svc.SearchWithReport(context.Background(), "检索测试", &AtomicMemorySearchOptions{FolderID: folderA, Limit: 2})
		if err != nil {
			t.Fatalf("SearchWithReport failed: %v", err)
		}
		if len(page) != 0 {
			t.Fatalf("expected empty prefix page, got %v", pageIDs(page))
		}
		if !report.BudgetLimited || report.Exhausted {
			t.Fatalf("expected budgetLimited=true exhausted=false, report=%+v", report)
		}
		if report.CandidatesScanned != 4 || report.Batches != 2 {
			t.Fatalf("expected 4 candidates / 2 batches, report=%+v", report)
		}

		// 旧签名 Search：达到上限时返回已扫描前缀（此处为空）但不报错，
		// 与假空页的区分体现在 SearchWithReport 的 BudgetLimited 标志。
		legacy, err := svc.Search(context.Background(), "检索测试", &AtomicMemorySearchOptions{FolderID: folderA, Limit: 2})
		if err != nil {
			t.Fatalf("legacy Search must not error on budget limit, got: %v", err)
		}
		if len(legacy) != 0 {
			t.Fatalf("expected empty legacy prefix, got %v", pageIDs(legacy))
		}
	})

	t.Run("budget limit returns collected filtered prefix", func(t *testing.T) {
		svc, store, cleanup := setupScriptedSearchService(t)
		defer cleanup()

		// 唯一匹配在 rank 3；budget=4 → 上限触发时已收集 1 条，必须返回。
		svc.searchBatchSize = 2
		svc.searchCandidateBudget = 4
		for i := 1; i <= 10; i++ {
			folder := &folderB
			if i == 3 {
				folder = &folderA
			}
			id := fmt.Sprintf("c%d", i)
			addSearchCandidate(t, svc, store, searchTestMemory(id, "sf-a", int64(100+i), []string{"t1"}, folder), 1.0-float64(i)*0.01)
		}
		page, report, err := svc.SearchWithReport(context.Background(), "检索测试", &AtomicMemorySearchOptions{FolderID: folderA, Limit: 2})
		if err != nil {
			t.Fatalf("SearchWithReport failed: %v", err)
		}
		assertIDOrder(t, page, []string{"c3"})
		if !report.BudgetLimited || report.Exhausted {
			t.Fatalf("expected budgetLimited=true exhausted=false, report=%+v", report)
		}
	})
}

// ---------------------------------------------------------------------------
// 过滤组合矩阵：session × folder × tags × 时间窗 × 已删
// ---------------------------------------------------------------------------

func TestAtomicSearchFilterCombinationMatrix(t *testing.T) {
	svc, store, cleanup := setupScriptedSearchService(t)
	defer cleanup()
	ctx := context.Background()

	// 关闭 stub 的 session 预过滤：session 排除必须发生在正文 SQL 权威层。
	store.ignoreSessionFilter = true

	folderF := "folder-f"
	folderG := "folder-g"
	start := int64(50)
	end := int64(150)

	rows := []struct {
		id      string
		session string
		folder  *string
		tags    []string
		ts      int64
		score   float64
	}{
		{"ok-1", "s", &folderF, []string{"want"}, 100, 0.90},
		{"bad-session", "s2", &folderF, []string{"want"}, 100, 0.85},
		{"ok-2", "s", &folderF, []string{"want", "other"}, 120, 0.80},
		{"bad-folder", "s", &folderG, []string{"want"}, 100, 0.75},
		{"bad-tag", "s", &folderF, []string{"other"}, 100, 0.70},
		{"bad-time-lo", "s", &folderF, []string{"want"}, 10, 0.65},
		{"bad-time-hi", "s", &folderF, []string{"want"}, 500, 0.60},
		{"bad-deleted", "s", &folderF, []string{"want"}, 100, 0.55},
	}
	for _, r := range rows {
		addSearchCandidate(t, svc, store, searchTestMemory(r.id, r.session, r.ts, r.tags, r.folder), r.score)
	}
	if err := svc.Delete(ctx, "bad-deleted"); err != nil {
		t.Fatalf("Delete bad-deleted failed: %v", err)
	}

	page, report, err := svc.SearchWithReport(ctx, "检索测试", &AtomicMemorySearchOptions{
		SessionID: "s",
		FolderID:  folderF,
		Tags:      []string{"want"},
		StartAt:   &start,
		EndAt:     &end,
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("SearchWithReport failed: %v", err)
	}
	assertIDOrder(t, page, []string{"ok-1", "ok-2"})
	if !report.Exhausted || report.BudgetLimited {
		t.Fatalf("expected exhausted complete result, report=%+v", report)
	}

	// nil opts：deleted=0 恒真过滤仍在，其余条件不约束；按 score DESC 全量。
	all, allReport, err := svc.SearchWithReport(ctx, "检索测试", nil)
	if err != nil {
		t.Fatalf("nil-opts SearchWithReport failed: %v", err)
	}
	assertIDOrder(t, all, []string{"ok-1", "bad-session", "ok-2", "bad-folder", "bad-tag", "bad-time-lo", "bad-time-hi"})
	if !allReport.Exhausted || allReport.BudgetLimited {
		t.Fatalf("nil-opts expected exhausted complete result, report=%+v", allReport)
	}
}

// ---------------------------------------------------------------------------
// 同分候选：id tiebreaker 稳定排序，逐页遍历确定且不漏不重
// ---------------------------------------------------------------------------

func TestAtomicSearchTiedScoresDeterministicPagination(t *testing.T) {
	svc, store, cleanup := setupScriptedSearchService(t)
	defer cleanup()
	ctx := context.Background()

	folder := "folder-tie"
	// 脚本顺序故意与 id 字典序不一致，同分必须按 id ASC 稳定化。
	scriptOrder := []string{"tie-c", "tie-a", "tie-f", "tie-b", "tie-e", "tie-d"}
	for i, id := range scriptOrder {
		addSearchCandidate(t, svc, store, searchTestMemory(id, "sf-a", int64(100+i), []string{"t1"}, &folder), 0.5)
	}

	wantOrder := []string{"tie-a", "tie-b", "tie-c", "tie-d", "tie-e", "tie-f"}
	opts := &AtomicMemorySearchOptions{FolderID: folder, Limit: 2}

	for round := 0; round < 2; round++ {
		var collected []string
		for offset := 0; offset <= 6; offset += 2 {
			page, report, err := svc.SearchWithReport(ctx, "检索测试", cloneSearchOpts(opts, offset))
			if err != nil {
				t.Fatalf("round %d offset %d failed: %v", round, offset, err)
			}
			if !report.Exhausted || report.BudgetLimited {
				t.Fatalf("round %d offset %d unexpected report=%+v", round, offset, report)
			}
			collected = append(collected, pageIDs(page)...)
		}
		if len(collected) != len(wantOrder) {
			t.Fatalf("round %d expected %v, got %v", round, wantOrder, collected)
		}
		for i := range wantOrder {
			if collected[i] != wantOrder[i] {
				t.Fatalf("round %d expected %v, got %v", round, wantOrder, collected)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// SQL 读路径：timestamp/heat 排序带 id tiebreaker，GetBySession 逐页不漏不重
// ---------------------------------------------------------------------------

func TestAtomicSQLReadPathsStableOrderAndPagination(t *testing.T) {
	svc, _, cleanup := setupScriptedSearchService(t)
	defer cleanup()
	ctx := context.Background()

	// 同 session 10 条：ts=200 组与 ts=100 组各 5 条，插入顺序打乱。
	insert := []struct {
		id string
		ts int64
	}{
		{"s200-c", 200}, {"s100-b", 100}, {"s200-a", 200}, {"s100-e", 100},
		{"s200-e", 200}, {"s100-a", 100}, {"s200-b", 200}, {"s100-d", 100},
		{"s200-d", 200}, {"s100-c", 100},
	}
	for _, r := range insert {
		mem := searchTestMemory(r.id, "sf-sql", r.ts, []string{"common"}, nil)
		if err := svc.Add(ctx, mem); err != nil {
			t.Fatalf("Add %s failed: %v", r.id, err)
		}
	}

	// 期望顺序：timestamp DESC，组内 id ASC。
	wantOrder := []string{
		"s200-a", "s200-b", "s200-c", "s200-d", "s200-e",
		"s100-a", "s100-b", "s100-c", "s100-d", "s100-e",
	}

	// GetBySession 逐页遍历：4/4/2 + 空边界页，不重不漏。
	seen := make(map[string]int)
	var collected []string
	for _, offset := range []int{0, 4, 8, 12} {
		page, err := svc.GetBySession(ctx, "sf-sql", 4, offset)
		if err != nil {
			t.Fatalf("GetBySession offset %d failed: %v", offset, err)
		}
		for _, id := range pageIDs(page) {
			seen[id]++
			if seen[id] > 1 {
				t.Fatalf("duplicate id %s across pages", id)
			}
		}
		collected = append(collected, pageIDs(page)...)
	}
	if len(seen) != 10 {
		t.Fatalf("expected 10 unique ids, got %v", seen)
	}
	for i := range wantOrder {
		if collected[i] != wantOrder[i] {
			t.Fatalf("GetBySession expected order %v, got %v", wantOrder, collected)
		}
	}

	// 两轮遍历完全一致（同 timestamp 下顺序确定）。
	again, err := svc.GetBySession(ctx, "sf-sql", 10, 0)
	if err != nil {
		t.Fatalf("GetBySession repeat failed: %v", err)
	}
	for i := range wantOrder {
		if pageIDs(again)[i] != wantOrder[i] {
			t.Fatalf("GetBySession repeat expected %v, got %v", wantOrder, pageIDs(again))
		}
	}

	// SearchByTimeRange / SearchByTags 同一稳定顺序。
	byTime, err := svc.SearchByTimeRange(ctx, 0, 1000)
	if err != nil {
		t.Fatalf("SearchByTimeRange failed: %v", err)
	}
	assertIDOrder(t, byTime, wantOrder)

	byTags, err := svc.SearchByTags(ctx, []string{"common"})
	if err != nil {
		t.Fatalf("SearchByTags failed: %v", err)
	}
	assertIDOrder(t, byTags, wantOrder)

	// SearchByTier：全部 heat=1.0 同分，必须按 id ASC 稳定。
	byTier, err := svc.SearchByTier(ctx, MemoryTierHot, 10)
	if err != nil {
		t.Fatalf("SearchByTier failed: %v", err)
	}
	assertIDOrder(t, byTier, []string{
		"s100-a", "s100-b", "s100-c", "s100-d", "s100-e",
		"s200-a", "s200-b", "s200-c", "s200-d", "s200-e",
	})
}

// ---------------------------------------------------------------------------
// 真实向量库端到端：递增 TopK 续取对真实 TopK 截断语义成立，
// 逐页遍历带过滤结果不漏不重且确定。
// ---------------------------------------------------------------------------

func TestAtomicSearchRealVectorStoreResumePagination(t *testing.T) {
	db, cleanupDB := setupAtomicServiceTestDB(t)

	tmpVectorDir, err := os.MkdirTemp("", "atomic_search_vector_test")
	if err != nil {
		cleanupDB()
		t.Fatalf("create temp vector dir failed: %v", err)
	}
	provider := NewSimpleEmbeddingProvider(32)
	vectorStore := NewSQLiteVectorStore(&VectorStoreConfig{
		CollectionName: "atomic_search_test",
		DBPath:         filepath.Join(tmpVectorDir, "vectors.db"),
		WALMode:        true,
	}, provider)

	svc, err := NewAtomicMemoryService(context.Background(), db, vectorStore, provider, WithAtomicIndexWorkerDisabled())
	if err != nil {
		cleanupDB()
		_ = os.RemoveAll(tmpVectorDir)
		t.Fatalf("create AtomicMemoryService failed: %v", err)
	}
	defer func() {
		_ = vectorStore.Close()
		_ = os.RemoveAll(tmpVectorDir)
		cleanupDB()
	}()

	ctx := context.Background()
	// 小批量强制对真实 store 多批续取。
	svc.searchBatchSize = 2
	svc.searchCandidateBudget = 100

	// 9 条共享关键字的记忆：偶数位带 tag keep，奇数位带 tag skip。
	wantKeep := make(map[string]bool)
	for i := 0; i < 9; i++ {
		tag := "skip"
		if i%2 == 0 {
			tag = "keep"
			wantKeep[fmt.Sprintf("rv-%d", i)] = true
		}
		mem := &AtomicMemory{
			ID:         fmt.Sprintf("rv-%d", i),
			Timestamp:  int64(100 + i),
			Content:    fmt.Sprintf("向量续取验证 共享关键字 第%d条", i),
			Tags:       []string{tag},
			SessionID:  "sf-real",
			Source:     AtomicMemorySourceUser,
			Importance: 0.5,
		}
		if err := svc.Add(ctx, mem); err != nil {
			t.Fatalf("Add %s failed: %v", mem.ID, err)
		}
	}

	opts := &AtomicMemorySearchOptions{Tags: []string{"keep"}, Limit: 2}

	var firstRoundPages [][]string
	for round := 0; round < 2; round++ {
		seen := make(map[string]int)
		var pages [][]string
		for offset := 0; ; offset += 2 {
			page, report, err := svc.SearchWithReport(ctx, "共享关键字", cloneSearchOpts(opts, offset))
			if err != nil {
				t.Fatalf("round %d offset %d failed: %v", round, offset, err)
			}
			if report.BudgetLimited {
				t.Fatalf("round %d offset %d must not hit budget, report=%+v", round, offset, report)
			}
			if len(page) == 0 {
				if !report.Exhausted {
					t.Fatalf("round %d offset %d empty page without exhaustion, report=%+v", round, offset, report)
				}
				break
			}
			ids := pageIDs(page)
			pages = append(pages, ids)
			for _, id := range ids {
				if !wantKeep[id] {
					t.Fatalf("round %d: unexpected id %s in filtered page", round, id)
				}
				seen[id]++
				if seen[id] > 1 {
					t.Fatalf("round %d: duplicate id %s across pages", round, id)
				}
			}
		}
		if len(seen) != len(wantKeep) {
			t.Fatalf("round %d: expected %d matched ids, got %v", round, len(wantKeep), seen)
		}
		if round == 0 {
			firstRoundPages = pages
			continue
		}
		// 两轮遍历页序列完全一致（真实 store 下分页确定）。
		if len(pages) != len(firstRoundPages) {
			t.Fatalf("round 1 pages %v differ from round 0 %v", pages, firstRoundPages)
		}
		for i := range pages {
			if len(pages[i]) != len(firstRoundPages[i]) {
				t.Fatalf("round 1 pages %v differ from round 0 %v", pages, firstRoundPages)
			}
			for j := range pages[i] {
				if pages[i][j] != firstRoundPages[i][j] {
					t.Fatalf("round 1 pages %v differ from round 0 %v", pages, firstRoundPages)
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// 返回前权威复核必须带检索条件（复审 P2 修复）
// ---------------------------------------------------------------------------

// TestSearchRecheckDropsConcurrentlyUnmatchedCandidate 钉住：批次过滤接受候选
// 之后、返回之前发生的并发 Update，若把该候选改得不再满足检索条件，复核阶段
// 必须丢弃它，而不是按最新正文放行。
//
// 修复前 recheckFilteredAuthoritative 以 nil opts 复核（只 enforce deleted=0）
// 并用最新行替换候选内容，于是按 session=S1 检索会返回一条已被改到 S2 的记忆。
func TestSearchRecheckDropsConcurrentlyUnmatchedCandidate(t *testing.T) {
	svc, store, cleanup := setupScriptedSearchService(t)
	defer cleanup()
	defer svc.Close()
	ctx := context.Background()

	// 向量层不按 session 收窄：session 归属由正文 SQL 与复核把关。
	store.ignoreSessionFilter = true

	addSearchCandidate(t, svc, store, searchTestMemory("m-other", "S2", 300, nil, nil), 0.9)
	addSearchCandidate(t, svc, store, searchTestMemory("m-target", "S1", 200, nil, nil), 0.8)
	addSearchCandidate(t, svc, store, searchTestMemory("m-tail", "S1", 100, nil, nil), 0.7)

	// 第 1 批（TopK=2）返回 m-other/m-target：m-other 被 session 条件滤掉，
	// m-target 通过并进入候选集。第 2 批开始前把 m-target 改到 S2——这正是
	// "批次过滤已接受、尚未返回"的窗口。
	moved := "S2"
	var once sync.Once
	store.onSearch = func(call int) {
		if call != 2 {
			return
		}
		once.Do(func() {
			if err := svc.Update(ctx, "m-target", &AtomicMemoryUpdate{SessionID: &moved}); err != nil {
				t.Errorf("concurrent Update failed: %v", err)
			}
		})
	}

	svc.searchBatchSize = 1
	svc.searchCandidateBudget = 100

	page, report, err := svc.SearchWithReport(ctx, "检索测试正文", &AtomicMemorySearchOptions{SessionID: "S1", Limit: 2})
	if err != nil {
		t.Fatalf("SearchWithReport failed: %v", err)
	}
	if len(store.topKs) < 2 {
		t.Fatalf("expected the recheck window to be exercised by >=2 batches, got topKs=%v", store.topKs)
	}

	for _, mem := range page {
		if mem.ID == "m-target" {
			t.Fatalf("recheck returned a memory that no longer matches the session filter: id=%s session=%s", mem.ID, mem.SessionID)
		}
		if mem.SessionID != "S1" {
			t.Fatalf("page leaked a memory outside session S1: id=%s session=%s", mem.ID, mem.SessionID)
		}
	}
	if got := pageIDs(page); len(got) != 1 || got[0] != "m-tail" {
		t.Fatalf("expected only m-tail to survive the recheck, got %v (report=%+v)", got, report)
	}
}
