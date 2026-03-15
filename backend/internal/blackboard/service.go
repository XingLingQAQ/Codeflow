// Package blackboard - Blackboard service for multi-agent collaboration
package blackboard

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/codeflow/backend/internal/audit"
	"github.com/google/uuid"
)

// EntryType 条目类型
type EntryType string

const (
	EntryTypeState    EntryType = "state"    // 状态信息
	EntryTypeProposal EntryType = "proposal" // 提案
	EntryTypeDecision EntryType = "decision" // 决策
	EntryTypeArtifact EntryType = "artifact" // 产出物
)

// Entry 黑板条目
type Entry struct {
	ID          string                 `json:"id"`
	Type        EntryType              `json:"type"`
	Title       string                 `json:"title"`
	Content     string                 `json:"content"`
	Author      string                 `json:"author"`
	AgentID     string                 `json:"agent_id,omitempty"`
	Version     int                    `json:"version"`
	CreatedAt   int64                  `json:"created_at"`
	UpdatedAt   int64                  `json:"updated_at"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	Tags        []string               `json:"tags,omitempty"`
	ParentID    string                 `json:"parent_id,omitempty"`
	IsArchived  bool                   `json:"is_archived"`
}

// EntryHistory 条目历史版本
type EntryHistory struct {
	EntryID   string `json:"entry_id"`
	Version   int    `json:"version"`
	Content   string `json:"content"`
	Author    string `json:"author"`
	Timestamp int64  `json:"timestamp"`
}

// VoteStatus 投票状态
type VoteStatus string

const (
	VoteStatusPending  VoteStatus = "pending"
	VoteStatusApproved VoteStatus = "approved"
	VoteStatusRejected VoteStatus = "rejected"
	VoteStatusTimeout  VoteStatus = "timeout"
)

// Vote 投票
type Vote struct {
	ID          string                 `json:"id"`
	EntryID     string                 `json:"entry_id"`
	Title       string                 `json:"title"`
	Description string                 `json:"description,omitempty"`
	Initiator   string                 `json:"initiator"`
	Status      VoteStatus             `json:"status"`
	Threshold   float64                `json:"threshold"` // BFT阈值，默认2/3
	Votes       map[string]bool        `json:"votes"`     // agentID -> approve/reject
	CreatedAt   int64                  `json:"created_at"`
	ExpiresAt   int64                  `json:"expires_at"`
	ResolvedAt  int64                  `json:"resolved_at,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// EntryListRequest 条目列表请求
type EntryListRequest struct {
	Type      string `form:"type"`
	Author    string `form:"author"`
	AgentID   string `form:"agent_id"`
	Tag       string `form:"tag"`
	ParentID  string `form:"parent_id"`
	Archived  *bool  `form:"archived"`
	SortBy    string `form:"sort_by"`    // created_at, updated_at, version
	SortOrder string `form:"sort_order"` // asc, desc
	Limit     int    `form:"limit"`
	Offset    int    `form:"offset"`
}

// EntryListResponse 条目列表响应
type EntryListResponse struct {
	Entries []Entry `json:"entries"`
	Total   int     `json:"total"`
	HasMore bool    `json:"has_more"`
}

// EntryCreateRequest 创建条目请求
type EntryCreateRequest struct {
	Type     EntryType              `json:"type" binding:"required"`
	Title    string                 `json:"title" binding:"required"`
	Content  string                 `json:"content" binding:"required"`
	Author   string                 `json:"author" binding:"required"`
	AgentID  string                 `json:"agent_id,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
	Tags     []string               `json:"tags,omitempty"`
	ParentID string                 `json:"parent_id,omitempty"`
}

// EntryUpdateRequest 更新条目请求
type EntryUpdateRequest struct {
	Title    string                 `json:"title,omitempty"`
	Content  string                 `json:"content,omitempty"`
	Author   string                 `json:"author" binding:"required"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
	Tags     []string               `json:"tags,omitempty"`
}

// VoteCreateRequest 创建投票请求
type VoteCreateRequest struct {
	EntryID     string                 `json:"entry_id" binding:"required"`
	Title       string                 `json:"title" binding:"required"`
	Description string                 `json:"description,omitempty"`
	Initiator   string                 `json:"initiator" binding:"required"`
	Threshold   float64                `json:"threshold,omitempty"` // 默认 2/3
	TimeoutSec  int                    `json:"timeout_sec,omitempty"` // 默认 300秒
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// VoteCastRequest 投票请求
type VoteCastRequest struct {
	AgentID string `json:"agent_id" binding:"required"`
	Approve bool   `json:"approve"`
}

// VoteResponse 投票响应
type VoteResponse struct {
	Vote         *Vote   `json:"vote"`
	ApproveCount int     `json:"approve_count"`
	RejectCount  int     `json:"reject_count"`
	TotalVotes   int     `json:"total_votes"`
	ApproveRate  float64 `json:"approve_rate"`
}

// IBlackboard 黑板服务接口
type IBlackboard interface {
	// Entry CRUD
	ListEntries(ctx context.Context, req *EntryListRequest) (*EntryListResponse, error)
	GetEntry(ctx context.Context, id string) (*Entry, error)
	CreateEntry(ctx context.Context, req *EntryCreateRequest) (*Entry, error)
	UpdateEntry(ctx context.Context, id string, req *EntryUpdateRequest) (*Entry, error)
	DeleteEntry(ctx context.Context, id string) error
	ArchiveEntry(ctx context.Context, id string) error

	// Version control
	GetEntryHistory(ctx context.Context, entryID string) ([]EntryHistory, error)
	RevertEntry(ctx context.Context, entryID string, version int) (*Entry, error)

	// Vote operations
	CreateVote(ctx context.Context, req *VoteCreateRequest) (*Vote, error)
	GetVote(ctx context.Context, id string) (*VoteResponse, error)
	CastVote(ctx context.Context, voteID string, req *VoteCastRequest) (*VoteResponse, error)

	// Internal
	CheckExpiredVotes()
}

// InMemoryBlackboard 内存实现的黑板服务
type InMemoryBlackboard struct {
	mu       sync.RWMutex
	entries  map[string]*Entry
	history  map[string][]*EntryHistory // entryID -> history
	votes    map[string]*Vote
	stopChan chan struct{}
}

// NewInMemoryBlackboard 创建内存黑板服务
func NewInMemoryBlackboard() *InMemoryBlackboard {
	bb := &InMemoryBlackboard{
		entries:  make(map[string]*Entry),
		history:  make(map[string][]*EntryHistory),
		votes:    make(map[string]*Vote),
		stopChan: make(chan struct{}),
	}
	go bb.runVoteChecker()
	return bb
}

// runVoteChecker 定期检查过期投票
func (bb *InMemoryBlackboard) runVoteChecker() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			bb.CheckExpiredVotes()
		case <-bb.stopChan:
			return
		}
	}
}

// ListEntries 列出条目
func (bb *InMemoryBlackboard) ListEntries(ctx context.Context, req *EntryListRequest) (*EntryListResponse, error) {
	bb.mu.RLock()
	defer bb.mu.RUnlock()

	var filtered []*Entry
	for _, e := range bb.entries {
		// 过滤条件
		if req.Type != "" && string(e.Type) != req.Type {
			continue
		}
		if req.Author != "" && e.Author != req.Author {
			continue
		}
		if req.AgentID != "" && e.AgentID != req.AgentID {
			continue
		}
		if req.ParentID != "" && e.ParentID != req.ParentID {
			continue
		}
		if req.Archived != nil && e.IsArchived != *req.Archived {
			continue
		}
		if req.Tag != "" {
			hasTag := false
			for _, t := range e.Tags {
				if t == req.Tag {
					hasTag = true
					break
				}
			}
			if !hasTag {
				continue
			}
		}
		filtered = append(filtered, e)
	}

	// 排序
	sortBy := req.SortBy
	if sortBy == "" {
		sortBy = "updated_at"
	}
	sortDesc := req.SortOrder != "asc"

	sort.Slice(filtered, func(i, j int) bool {
		var less bool
		switch sortBy {
		case "created_at":
			less = filtered[i].CreatedAt < filtered[j].CreatedAt
		case "version":
			less = filtered[i].Version < filtered[j].Version
		default: // updated_at
			less = filtered[i].UpdatedAt < filtered[j].UpdatedAt
		}
		if sortDesc {
			return !less
		}
		return less
	})

	// 分页
	total := len(filtered)
	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := req.Offset
	if offset < 0 {
		offset = 0
	}

	start := offset
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}

	result := make([]Entry, end-start)
	for i := start; i < end; i++ {
		result[i-start] = *filtered[i]
	}

	return &EntryListResponse{
		Entries: result,
		Total:   total,
		HasMore: end < total,
	}, nil
}

// GetEntry 获取单个条目
func (bb *InMemoryBlackboard) GetEntry(ctx context.Context, id string) (*Entry, error) {
	bb.mu.RLock()
	defer bb.mu.RUnlock()

	entry, ok := bb.entries[id]
	if !ok {
		return nil, nil
	}
	return entry, nil
}

// CreateEntry 创建条目
func (bb *InMemoryBlackboard) CreateEntry(ctx context.Context, req *EntryCreateRequest) (*Entry, error) {
	bb.mu.Lock()
	defer bb.mu.Unlock()

	now := time.Now().Unix()
	entry := &Entry{
		ID:        uuid.New().String(),
		Type:      req.Type,
		Title:     req.Title,
		Content:   req.Content,
		Author:    req.Author,
		AgentID:   req.AgentID,
		Version:   1,
		CreatedAt: now,
		UpdatedAt: now,
		Metadata:  req.Metadata,
		Tags:      req.Tags,
		ParentID:  req.ParentID,
	}

	bb.entries[entry.ID] = entry
	bb.history[entry.ID] = []*EntryHistory{{
		EntryID:   entry.ID,
		Version:   1,
		Content:   entry.Content,
		Author:    entry.Author,
		Timestamp: now,
	}}

	return entry, nil
}

// UpdateEntry 更新条目
func (bb *InMemoryBlackboard) UpdateEntry(ctx context.Context, id string, req *EntryUpdateRequest) (*Entry, error) {
	bb.mu.Lock()
	defer bb.mu.Unlock()

	entry, ok := bb.entries[id]
	if !ok {
		return nil, errors.New("entry not found")
	}

	now := time.Now().Unix()
	entry.Version++
	entry.UpdatedAt = now

	if req.Title != "" {
		entry.Title = req.Title
	}
	if req.Content != "" {
		entry.Content = req.Content
	}
	if req.Metadata != nil {
		entry.Metadata = req.Metadata
	}
	if req.Tags != nil {
		entry.Tags = req.Tags
	}

	// 记录历史
	bb.history[id] = append(bb.history[id], &EntryHistory{
		EntryID:   id,
		Version:   entry.Version,
		Content:   entry.Content,
		Author:    req.Author,
		Timestamp: now,
	})

	return entry, nil
}

// DeleteEntry 删除条目
func (bb *InMemoryBlackboard) DeleteEntry(ctx context.Context, id string) error {
	bb.mu.Lock()
	defer bb.mu.Unlock()

	if _, ok := bb.entries[id]; !ok {
		return errors.New("entry not found")
	}

	delete(bb.entries, id)
	delete(bb.history, id)
	return nil
}

// ArchiveEntry 归档条目
func (bb *InMemoryBlackboard) ArchiveEntry(ctx context.Context, id string) error {
	bb.mu.Lock()
	defer bb.mu.Unlock()

	entry, ok := bb.entries[id]
	if !ok {
		return errors.New("entry not found")
	}

	entry.IsArchived = true
	entry.UpdatedAt = time.Now().Unix()
	return nil
}

// GetEntryHistory 获取条目历史
func (bb *InMemoryBlackboard) GetEntryHistory(ctx context.Context, entryID string) ([]EntryHistory, error) {
	bb.mu.RLock()
	defer bb.mu.RUnlock()

	hist, ok := bb.history[entryID]
	if !ok {
		return []EntryHistory{}, nil
	}

	result := make([]EntryHistory, len(hist))
	for i, h := range hist {
		result[i] = *h
	}
	return result, nil
}

// RevertEntry 回滚条目到指定版本
func (bb *InMemoryBlackboard) RevertEntry(ctx context.Context, entryID string, version int) (*Entry, error) {
	bb.mu.Lock()
	defer bb.mu.Unlock()

	entry, ok := bb.entries[entryID]
	if !ok {
		return nil, errors.New("entry not found")
	}

	hist, ok := bb.history[entryID]
	if !ok {
		return nil, errors.New("no history found")
	}

	var targetHist *EntryHistory
	for _, h := range hist {
		if h.Version == version {
			targetHist = h
			break
		}
	}
	if targetHist == nil {
		return nil, errors.New("version not found")
	}

	now := time.Now().Unix()
	entry.Version++
	entry.Content = targetHist.Content
	entry.UpdatedAt = now

	bb.history[entryID] = append(bb.history[entryID], &EntryHistory{
		EntryID:   entryID,
		Version:   entry.Version,
		Content:   entry.Content,
		Author:    "system:revert",
		Timestamp: now,
	})

	return entry, nil
}

// CreateVote 创建投票
func (bb *InMemoryBlackboard) CreateVote(ctx context.Context, req *VoteCreateRequest) (*Vote, error) {
	bb.mu.Lock()
	defer bb.mu.Unlock()

	// 验证条目存在
	if _, ok := bb.entries[req.EntryID]; !ok {
		return nil, errors.New("entry not found")
	}

	threshold := req.Threshold
	if threshold <= 0 || threshold > 1 {
		threshold = 2.0 / 3.0 // BFT默认阈值
	}

	timeoutSec := req.TimeoutSec
	if timeoutSec <= 0 {
		timeoutSec = 300 // 默认5分钟
	}

	now := time.Now().Unix()
	vote := &Vote{
		ID:          uuid.New().String(),
		EntryID:     req.EntryID,
		Title:       req.Title,
		Description: req.Description,
		Initiator:   req.Initiator,
		Status:      VoteStatusPending,
		Threshold:   threshold,
		Votes:       make(map[string]bool),
		CreatedAt:   now,
		ExpiresAt:   now + int64(timeoutSec),
		Metadata:    req.Metadata,
	}

	bb.votes[vote.ID] = vote
	bb.recordVoteAudit(ctx, vote, "vote_created", audit.OutcomeSuccess, audit.SeverityInfo, map[string]interface{}{
		"threshold":    vote.Threshold,
		"timeout_sec":  timeoutSec,
		"metadata_keys": mapKeys(req.Metadata),
	})
	return vote, nil
}

func (bb *InMemoryBlackboard) recordVoteAudit(ctx context.Context, vote *Vote, action string, outcome audit.AuditOutcome, severity audit.AuditSeverity, details map[string]interface{}) {
	if vote == nil {
		return
	}
	payload := map[string]interface{}{
		"status":        string(vote.Status),
		"entry_id":      vote.EntryID,
		"threshold":     vote.Threshold,
		"approve_count": countApprovals(vote),
		"reject_count":  len(vote.Votes) - countApprovals(vote),
		"total_votes":   len(vote.Votes),
	}
	for key, value := range details {
		payload[key] = value
	}

	_, _ = audit.Record(ctx, &audit.AuditLogEntry{
		EventType: audit.EventApproval,
		Severity:  severity,
		Actor: audit.AuditActor{
			ID:   vote.Initiator,
			Type: "user",
			Name: vote.Initiator,
		},
		Resource: audit.AuditResource{
			Type: "vote",
			ID:   vote.ID,
			Name: vote.Title,
		},
		Action:  action,
		Outcome: outcome,
		Details: payload,
	})
}

func countApprovals(vote *Vote) int {
	if vote == nil {
		return 0
	}
	approveCount := 0
	for _, approved := range vote.Votes {
		if approved {
			approveCount++
		}
	}
	return approveCount
}

func mapKeys(values map[string]interface{}) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// GetVote 获取投票状态
func (bb *InMemoryBlackboard) GetVote(ctx context.Context, id string) (*VoteResponse, error) {
	bb.mu.RLock()
	defer bb.mu.RUnlock()

	vote, ok := bb.votes[id]
	if !ok {
		return nil, nil
	}

	return bb.buildVoteResponse(vote), nil
}

// CastVote 投票
func (bb *InMemoryBlackboard) CastVote(ctx context.Context, voteID string, req *VoteCastRequest) (*VoteResponse, error) {
	bb.mu.Lock()
	defer bb.mu.Unlock()

	vote, ok := bb.votes[voteID]
	if !ok {
		return nil, errors.New("vote not found")
	}

	if vote.Status != VoteStatusPending {
		return nil, errors.New("vote is not pending")
	}

	if time.Now().Unix() > vote.ExpiresAt {
		vote.Status = VoteStatusTimeout
		vote.ResolvedAt = time.Now().Unix()
		bb.recordVoteAudit(ctx, vote, "vote_expired", audit.OutcomeFailure, audit.SeverityWarning, map[string]interface{}{
			"agent_id": req.AgentID,
		})
		return nil, errors.New("vote has expired")
	}

	// 记录投票
	vote.Votes[req.AgentID] = req.Approve
	bb.recordVoteAudit(ctx, vote, "vote_cast", audit.OutcomeSuccess, audit.SeverityInfo, map[string]interface{}{
		"agent_id": req.AgentID,
		"approved": req.Approve,
	})

	// 检查是否达成共识
	bb.checkConsensus(ctx, vote)

	return bb.buildVoteResponse(vote), nil
}

// checkConsensus 检查BFT共识
func (bb *InMemoryBlackboard) checkConsensus(ctx context.Context, vote *Vote) {
	if vote.Status != VoteStatusPending {
		return
	}

	total := len(vote.Votes)
	if total == 0 {
		return
	}

	approveCount := countApprovals(vote)
	approveRate := float64(approveCount) / float64(total)
	rejectRate := float64(total-approveCount) / float64(total)

	if approveRate >= vote.Threshold {
		vote.Status = VoteStatusApproved
		vote.ResolvedAt = time.Now().Unix()
		bb.recordVoteAudit(ctx, vote, "vote_approved", audit.OutcomeSuccess, audit.SeverityInfo, map[string]interface{}{
			"approve_rate": approveRate,
		})
	} else if rejectRate > (1 - vote.Threshold) {
		vote.Status = VoteStatusRejected
		vote.ResolvedAt = time.Now().Unix()
		bb.recordVoteAudit(ctx, vote, "vote_rejected", audit.OutcomeFailure, audit.SeverityWarning, map[string]interface{}{
			"reject_rate": rejectRate,
		})
	}
}

// buildVoteResponse 构建投票响应
func (bb *InMemoryBlackboard) buildVoteResponse(vote *Vote) *VoteResponse {
	approveCount := 0
	for _, approved := range vote.Votes {
		if approved {
			approveCount++
		}
	}
	total := len(vote.Votes)

	var approveRate float64
	if total > 0 {
		approveRate = float64(approveCount) / float64(total)
	}

	return &VoteResponse{
		Vote:         vote,
		ApproveCount: approveCount,
		RejectCount:  total - approveCount,
		TotalVotes:   total,
		ApproveRate:  approveRate,
	}
}

// CheckExpiredVotes 检查过期投票
func (bb *InMemoryBlackboard) CheckExpiredVotes() {
	bb.mu.Lock()
	defer bb.mu.Unlock()

	now := time.Now().Unix()
	for _, vote := range bb.votes {
		if vote.Status == VoteStatusPending && now > vote.ExpiresAt {
			vote.Status = VoteStatusTimeout
			vote.ResolvedAt = now
			bb.recordVoteAudit(context.Background(), vote, "vote_timeout", audit.OutcomeFailure, audit.SeverityWarning, nil)
		}
	}
}

// 全局服务实例
var defaultBlackboard IBlackboard
var bbOnce sync.Once

// GetBlackboard 获取黑板服务实例
func GetBlackboard() IBlackboard {
	bbOnce.Do(func() {
		defaultBlackboard = NewInMemoryBlackboard()
	})
	return defaultBlackboard
}

// SetBlackboard 设置黑板服务实例
func SetBlackboard(bb IBlackboard) {
	defaultBlackboard = bb
}
