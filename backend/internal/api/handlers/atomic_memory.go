package handlers

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/codeflow/backend/internal/memory"
)

type atomicMemoryService interface {
	AddWithReceipt(ctx context.Context, mem *memory.AtomicMemory) (memory.AtomicMutationReceipt, error)
	SearchWithReport(ctx context.Context, query string, opts *memory.AtomicMemorySearchOptions) ([]memory.AtomicMemory, memory.AtomicSearchReport, error)
	GetBySession(ctx context.Context, sessionID string, limit, offset int) ([]memory.AtomicMemory, error)
	GetByID(ctx context.Context, id string) (*memory.AtomicMemory, error)
	UpdateWithReceipt(ctx context.Context, id string, updates *memory.AtomicMemoryUpdate) (memory.AtomicMutationReceipt, error)
	DeleteWithReceipt(ctx context.Context, id string) (memory.AtomicMutationReceipt, error)
	ApplyHeatDecay(ctx context.Context) (int, error)
	RecomputeTiers(ctx context.Context) (int, error)
	BoostHeat(ctx context.Context, id string, boost float64) error
	SearchByTier(ctx context.Context, tier memory.MemoryTier, limit int) ([]memory.AtomicMemory, error)
}

var (
	defaultAtomicMemoryService atomicMemoryService
	atomicMemoryServiceMu      sync.Mutex
)

func getAtomicMemoryService() (atomicMemoryService, error) {
	atomicMemoryServiceMu.Lock()
	defer atomicMemoryServiceMu.Unlock()

	if defaultAtomicMemoryService != nil {
		return defaultAtomicMemoryService, nil
	}
	if svc := memory.GetAtomicMemoryService(); svc != nil {
		return svc, nil
	}
	return nil, errors.New("atomic memory service is not initialized")
}

func setAtomicMemoryServiceForTest(svc atomicMemoryService) {
	atomicMemoryServiceMu.Lock()
	defer atomicMemoryServiceMu.Unlock()
	defaultAtomicMemoryService = svc
}

type createAtomicMemoryRequest struct {
	Content    string    `json:"content"`
	Tags       []string  `json:"tags"`
	SessionID  string    `json:"session_id"`
	FolderID   *string   `json:"folder_id,omitempty"`
	Source     string    `json:"source"`
	Importance float64   `json:"importance"`
	Timestamp  *int64    `json:"timestamp,omitempty"`
	Embedding  []float64 `json:"embedding,omitempty"`
}

type updateAtomicMemoryRequest struct {
	Timestamp     *int64     `json:"timestamp,omitempty"`
	Content       *string    `json:"content,omitempty"`
	Tags          *[]string  `json:"tags,omitempty"`
	SessionID     *string    `json:"session_id,omitempty"`
	FolderID      *string    `json:"folder_id,omitempty"`
	ClearFolderID bool       `json:"clear_folder_id,omitempty"`
	Source        *string    `json:"source,omitempty"`
	Importance    *float64   `json:"importance,omitempty"`
	Embedding     *[]float64 `json:"embedding,omitempty"`
}

// atomicMemoryMutationData 是 mutation（创建/更新）响应的 data 形状
// （T13.02.c）：保留 AtomicMemory 的全部既有字段（向后兼容），新增
// revision 与 index_sync，分开表达"正文已接受"与"向量索引同步状态"
// （pending/failed/synced）；index_error 只在 pending/failed 时出现。
// 只查正文的读路径不使用本形状，不向调用方报告索引状态。
type atomicMemoryMutationData struct {
	memory.AtomicMemory
	Revision   int64  `json:"revision"`
	IndexSync  string `json:"index_sync"`
	IndexError string `json:"index_error,omitempty"`
}

func atomicMemoryMutationDataFrom(mem *memory.AtomicMemory, receipt memory.AtomicMutationReceipt) atomicMemoryMutationData {
	return atomicMemoryMutationData{
		AtomicMemory: *mem,
		Revision:     receipt.Revision,
		IndexSync:    string(receipt.IndexSync),
		IndexError:   receipt.IndexError,
	}
}

// atomicMemoryDeleteData 是删除响应的 data 形状（T13.02.c）：保留既有
// deleted/id 字段，新增 revision/index_sync/index_error（语义同上）。
type atomicMemoryDeleteData struct {
	Deleted    bool   `json:"deleted"`
	ID         string `json:"id"`
	Revision   int64  `json:"revision"`
	IndexSync  string `json:"index_sync"`
	IndexError string `json:"index_error,omitempty"`
}

func atomicMemoryDeleteDataFrom(id string, receipt memory.AtomicMutationReceipt) atomicMemoryDeleteData {
	return atomicMemoryDeleteData{
		Deleted:    true,
		ID:         id,
		Revision:   receipt.Revision,
		IndexSync:  string(receipt.IndexSync),
		IndexError: receipt.IndexError,
	}
}

// atomicSearchReportData 是 Search 响应中的候选续取/索引同步报告
// （T13.02.c）：耗尽与达到工作量上限分开，另报告"当前 revision 未同步"
// 的索引 job 数。
type atomicSearchReportData struct {
	CandidatesScanned int  `json:"candidates_scanned"`
	Batches           int  `json:"batches"`
	Exhausted         bool `json:"exhausted"`
	BudgetLimited     bool `json:"budget_limited"`
	IndexPendingJobs  int  `json:"index_pending_jobs"`
	IndexFailedJobs   int  `json:"index_failed_jobs"`
}

// atomicMemorySearchData 是 Search 响应的 data 形状（T13.02.c）：保留既有
// memories/count 字段，新增 search_report 与 incomplete_reason。向量未同步
// （pending/failed job）或达到候选工作量上限时 incomplete_reason 非空，
// 不再把未同步的检索当成完整空结果。
type atomicMemorySearchData struct {
	Memories         []memory.AtomicMemory  `json:"memories"`
	Count            int                    `json:"count"`
	Report           atomicSearchReportData `json:"search_report"`
	IncompleteReason string                 `json:"incomplete_reason,omitempty"`
}

func atomicMemorySearchDataFrom(items []memory.AtomicMemory, report memory.AtomicSearchReport) atomicMemorySearchData {
	return atomicMemorySearchData{
		Memories: items,
		Count:    len(items),
		Report: atomicSearchReportData{
			CandidatesScanned: report.CandidatesScanned,
			Batches:           report.Batches,
			Exhausted:         report.Exhausted,
			BudgetLimited:     report.BudgetLimited,
			IndexPendingJobs:  report.IndexPendingJobs,
			IndexFailedJobs:   report.IndexFailedJobs,
		},
		IncompleteReason: report.IncompleteReason(),
	}
}

// CreateAtomicMemory handles POST /api/v1/memory/atomic.
func CreateAtomicMemory(c *gin.Context) {
	svc, err := getAtomicMemoryService()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to initialize atomic memory service: "+err.Error())
		return
	}

	var req createAtomicMemoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	req.Content = strings.TrimSpace(req.Content)
	req.SessionID = strings.TrimSpace(req.SessionID)
	req.Source = strings.TrimSpace(req.Source)

	if req.Content == "" {
		respondError(c, http.StatusBadRequest, "content is required")
		return
	}
	if req.SessionID == "" {
		respondError(c, http.StatusBadRequest, "session_id is required")
		return
	}

	source := memory.AtomicMemorySource(req.Source)
	if !isValidAtomicMemorySource(source) {
		respondError(c, http.StatusBadRequest, "source must be one of: user, assistant, system")
		return
	}
	if req.Importance < 0 || req.Importance > 1 {
		respondError(c, http.StatusBadRequest, "importance must be in range [0,1]")
		return
	}

	timestamp := time.Now().Unix()
	if req.Timestamp != nil {
		if *req.Timestamp <= 0 {
			respondError(c, http.StatusBadRequest, "timestamp must be positive")
			return
		}
		timestamp = *req.Timestamp
	}

	mem := &memory.AtomicMemory{
		ID:         uuid.NewString(),
		Timestamp:  timestamp,
		Content:    req.Content,
		Tags:       req.Tags,
		SessionID:  req.SessionID,
		FolderID:   req.FolderID,
		Source:     source,
		Importance: req.Importance,
		Embedding:  req.Embedding,
	}
	if err := mem.Validate(); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid atomic memory payload: "+err.Error())
		return
	}

	receipt, err := svc.AddWithReceipt(c.Request.Context(), mem)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to create atomic memory: "+err.Error())
		return
	}

	respondCreated(c, atomicMemoryMutationDataFrom(mem, receipt))
}

// SearchAtomicMemory handles GET /api/v1/memory/atomic/search.
func SearchAtomicMemory(c *gin.Context) {
	svc, err := getAtomicMemoryService()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to initialize atomic memory service: "+err.Error())
		return
	}

	query := strings.TrimSpace(c.Query("query"))
	if query == "" {
		respondError(c, http.StatusBadRequest, "query is required")
		return
	}

	opts, err := buildAtomicSearchOptions(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	items, report, err := svc.SearchWithReport(c.Request.Context(), query, opts)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to search atomic memories: "+err.Error())
		return
	}

	respondOK(c, atomicMemorySearchDataFrom(items, report))
}

// GetAtomicMemoriesBySession handles GET /api/v1/memory/atomic/session/:id.
func GetAtomicMemoriesBySession(c *gin.Context) {
	svc, err := getAtomicMemoryService()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to initialize atomic memory service: "+err.Error())
		return
	}

	sessionID := strings.TrimSpace(c.Param("id"))
	if sessionID == "" {
		respondError(c, http.StatusBadRequest, "session id is required")
		return
	}

	limit := 50
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		value, parseErr := strconv.Atoi(raw)
		if parseErr != nil || value <= 0 {
			respondError(c, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = value
	}

	offset := 0
	if raw := strings.TrimSpace(c.Query("offset")); raw != "" {
		value, parseErr := strconv.Atoi(raw)
		if parseErr != nil || value < 0 {
			respondError(c, http.StatusBadRequest, "offset must be a non-negative integer")
			return
		}
		offset = value
	}

	items, err := svc.GetBySession(c.Request.Context(), sessionID, limit, offset)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to get session atomic memories: "+err.Error())
		return
	}

	respondOK(c, gin.H{"memories": items, "count": len(items)})
}

// UpdateAtomicMemory handles PUT /api/v1/memory/atomic/:id.
func UpdateAtomicMemory(c *gin.Context) {
	svc, err := getAtomicMemoryService()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to initialize atomic memory service: "+err.Error())
		return
	}

	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		respondError(c, http.StatusBadRequest, "id is required")
		return
	}

	var req updateAtomicMemoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	updates, err := buildAtomicMemoryUpdate(&req)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	if !hasAtomicUpdates(updates) {
		respondError(c, http.StatusBadRequest, "no updatable fields provided")
		return
	}

	receipt, err := svc.UpdateWithReceipt(c.Request.Context(), id, updates)
	if err != nil {
		if errors.Is(err, memory.ErrAtomicMemoryNotFound) {
			respondError(c, http.StatusNotFound, "Atomic memory not found")
			return
		}
		respondError(c, http.StatusInternalServerError, "Failed to update atomic memory: "+err.Error())
		return
	}

	item, err := svc.GetByID(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, memory.ErrAtomicMemoryNotFound) {
			respondError(c, http.StatusNotFound, "Atomic memory not found")
			return
		}
		respondError(c, http.StatusInternalServerError, "Failed to load updated atomic memory: "+err.Error())
		return
	}

	respondOK(c, atomicMemoryMutationDataFrom(item, receipt))
}

// DeleteAtomicMemory handles DELETE /api/v1/memory/atomic/:id.
func DeleteAtomicMemory(c *gin.Context) {
	svc, err := getAtomicMemoryService()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to initialize atomic memory service: "+err.Error())
		return
	}

	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		respondError(c, http.StatusBadRequest, "id is required")
		return
	}

	receipt, err := svc.DeleteWithReceipt(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, memory.ErrAtomicMemoryNotFound) {
			respondError(c, http.StatusNotFound, "Atomic memory not found")
			return
		}
		respondError(c, http.StatusInternalServerError, "Failed to delete atomic memory: "+err.Error())
		return
	}

	respondOK(c, atomicMemoryDeleteDataFrom(id, receipt))
}

func buildAtomicSearchOptions(c *gin.Context) (*memory.AtomicMemorySearchOptions, error) {
	opts := &memory.AtomicMemorySearchOptions{}

	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit <= 0 {
			return nil, errors.New("limit must be a positive integer")
		}
		opts.Limit = limit
	}

	if raw := strings.TrimSpace(c.Query("offset")); raw != "" {
		offset, err := strconv.Atoi(raw)
		if err != nil || offset < 0 {
			return nil, errors.New("offset must be a non-negative integer")
		}
		opts.Offset = offset
	}

	opts.SessionID = strings.TrimSpace(c.Query("session_id"))
	opts.FolderID = strings.TrimSpace(c.Query("folder_id"))
	opts.Tags = splitAndTrim(c.Query("tags"))

	if raw := strings.TrimSpace(c.Query("start_at")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 {
			return nil, errors.New("start_at must be a positive unix timestamp")
		}
		opts.StartAt = &value
	}

	if raw := strings.TrimSpace(c.Query("end_at")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 {
			return nil, errors.New("end_at must be a positive unix timestamp")
		}
		opts.EndAt = &value
	}

	if opts.StartAt != nil && opts.EndAt != nil && *opts.StartAt > *opts.EndAt {
		return nil, errors.New("start_at cannot be greater than end_at")
	}

	return opts, nil
}

func buildAtomicMemoryUpdate(req *updateAtomicMemoryRequest) (*memory.AtomicMemoryUpdate, error) {
	updates := &memory.AtomicMemoryUpdate{
		Timestamp:     req.Timestamp,
		Tags:          req.Tags,
		ClearFolderID: req.ClearFolderID,
		Importance:    req.Importance,
		Embedding:     req.Embedding,
	}

	if req.Timestamp != nil && *req.Timestamp <= 0 {
		return nil, errors.New("timestamp must be positive")
	}
	if req.Importance != nil && (*req.Importance < 0 || *req.Importance > 1) {
		return nil, errors.New("importance must be in range [0,1]")
	}

	if req.Content != nil {
		content := strings.TrimSpace(*req.Content)
		if content == "" {
			return nil, errors.New("content cannot be empty")
		}
		updates.Content = &content
	}
	if req.SessionID != nil {
		sessionID := strings.TrimSpace(*req.SessionID)
		if sessionID == "" {
			return nil, errors.New("session_id cannot be empty")
		}
		updates.SessionID = &sessionID
	}
	if req.FolderID != nil {
		folderID := strings.TrimSpace(*req.FolderID)
		updates.FolderID = &folderID
	}
	if req.Source != nil {
		source := memory.AtomicMemorySource(strings.TrimSpace(*req.Source))
		if !isValidAtomicMemorySource(source) {
			return nil, errors.New("source must be one of: user, assistant, system")
		}
		updates.Source = &source
	}

	return updates, nil
}

func hasAtomicUpdates(updates *memory.AtomicMemoryUpdate) bool {
	return updates.Timestamp != nil ||
		updates.Content != nil ||
		updates.Tags != nil ||
		updates.SessionID != nil ||
		updates.FolderID != nil ||
		updates.ClearFolderID ||
		updates.Source != nil ||
		updates.Importance != nil ||
		updates.Embedding != nil
}

func isValidAtomicMemorySource(source memory.AtomicMemorySource) bool {
	switch source {
	case memory.AtomicMemorySourceUser, memory.AtomicMemorySourceAssistant, memory.AtomicMemorySourceSystem:
		return true
	default:
		return false
	}
}

func splitAndTrim(input string) []string {
	if strings.TrimSpace(input) == "" {
		return nil
	}
	parts := strings.Split(input, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		result = append(result, value)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// ApplyAtomicHeatDecay handles POST /api/v1/memory/atomic/decay
func ApplyAtomicHeatDecay(c *gin.Context) {
	svc, err := getAtomicMemoryService()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Service init failed: "+err.Error())
		return
	}

	affected, err := svc.ApplyHeatDecay(c.Request.Context())
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Apply decay failed: "+err.Error())
		return
	}

	respondOK(c, gin.H{"affected": affected})
}

// RecomputeAtomicTiers handles POST /api/v1/memory/atomic/recompute-tiers
func RecomputeAtomicTiers(c *gin.Context) {
	svc, err := getAtomicMemoryService()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Service init failed: "+err.Error())
		return
	}

	affected, err := svc.RecomputeTiers(c.Request.Context())
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Recompute tiers failed: "+err.Error())
		return
	}

	respondOK(c, gin.H{"affected": affected})
}

// BoostAtomicHeat handles POST /api/v1/memory/atomic/:id/boost
func BoostAtomicHeat(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		respondError(c, http.StatusBadRequest, "Missing memory ID")
		return
	}

	var req struct {
		Boost float64 `json:"boost"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body")
		return
	}

	svc, err := getAtomicMemoryService()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Service init failed: "+err.Error())
		return
	}

	if err := svc.BoostHeat(c.Request.Context(), id, req.Boost); err != nil {
		respondError(c, http.StatusInternalServerError, "Boost failed: "+err.Error())
		return
	}

	respondOK(c, gin.H{"message": "heat boosted"})
}

// GetAtomicMemoriesByTier handles GET /api/v1/memory/atomic/tier/:tier
func GetAtomicMemoriesByTier(c *gin.Context) {
	tier := memory.MemoryTier(c.Param("tier"))
	if tier != memory.MemoryTierHot && tier != memory.MemoryTierWarm && tier != memory.MemoryTierCold {
		respondError(c, http.StatusBadRequest, "Invalid tier: must be hot, warm, or cold")
		return
	}

	limit := 50
	if l := c.Query("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}

	svc, err := getAtomicMemoryService()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Service init failed: "+err.Error())
		return
	}

	memories, err := svc.SearchByTier(c.Request.Context(), tier, limit)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Search by tier failed: "+err.Error())
		return
	}

	respondOK(c, gin.H{"memories": memories, "count": len(memories), "tier": tier})
}
