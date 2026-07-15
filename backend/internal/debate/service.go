// Package debate - Debate service for Generator/Critic adversarial validation
package debate

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// DebateStatus 辩论状态
type DebateStatus string

const (
	DebateStatusPending    DebateStatus = "pending"
	DebateStatusInProgress DebateStatus = "in_progress"
	DebateStatusPaused     DebateStatus = "paused"
	DebateStatusResolved   DebateStatus = "resolved"
	DebateStatusAborted    DebateStatus = "aborted"
)

// AgentRole 智能体角色
type AgentRole string

const (
	RoleGenerator AgentRole = "generator"
	RoleCritic    AgentRole = "critic"
	RoleMediator  AgentRole = "mediator"
)

// ConflictSeverity 冲突严重程度
type ConflictSeverity string

const (
	SeverityLow      ConflictSeverity = "low"
	SeverityMedium   ConflictSeverity = "medium"
	SeverityHigh     ConflictSeverity = "high"
	SeverityCritical ConflictSeverity = "critical"
)

// ConflictStatus 冲突状态
type ConflictStatus string

const (
	ConflictStatusOpen     ConflictStatus = "open"
	ConflictStatusResolved ConflictStatus = "resolved"
	ConflictStatusDeferred ConflictStatus = "deferred"
)

// Debate 辩论会话
type Debate struct {
	ID            string                 `json:"id"`
	Title         string                 `json:"title"`
	Description   string                 `json:"description,omitempty"`
	Status        DebateStatus           `json:"status"`
	CurrentRound  int                    `json:"current_round"`
	MaxRounds     int                    `json:"max_rounds"`
	GeneratorID   string                 `json:"generator_id"`
	CriticID      string                 `json:"critic_id"`
	MediatorID    string                 `json:"mediator_id,omitempty"`
	// FlowID / StageID optionally bind this debate to a floweng stage (M4 FK).
	FlowID        string                 `json:"flow_id,omitempty"`
	StageID       string                 `json:"stage_id,omitempty"`
	Rounds        []*DebateRound         `json:"rounds"`
	Conflicts     []*Conflict            `json:"conflicts"`
	Solutions     []*Solution            `json:"solutions"`
	SelectedSolution string              `json:"selected_solution,omitempty"`
	CreatedAt     int64                  `json:"created_at"`
	UpdatedAt     int64                  `json:"updated_at"`
	ResolvedAt    int64                  `json:"resolved_at,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// DebateRound 辩论轮次
type DebateRound struct {
	Number         int       `json:"number"`
	GeneratorInput string    `json:"generator_input"`
	GeneratorOutput string   `json:"generator_output,omitempty"`
	CriticFeedback string    `json:"critic_feedback,omitempty"`
	ConflictsFound []string  `json:"conflicts_found,omitempty"` // conflict IDs
	StartedAt      int64     `json:"started_at"`
	CompletedAt    int64     `json:"completed_at,omitempty"`
}

// Conflict 冲突
type Conflict struct {
	ID          string                 `json:"id"`
	DebateID    string                 `json:"debate_id"`
	RoundNumber int                    `json:"round_number"`
	Type        string                 `json:"type"` // logic, syntax, semantic, style, security
	Severity    ConflictSeverity       `json:"severity"`
	Status      ConflictStatus         `json:"status"`
	Description string                 `json:"description"`
	Location    string                 `json:"location,omitempty"` // file:line or code snippet
	GeneratorView string               `json:"generator_view,omitempty"`
	CriticView    string               `json:"critic_view,omitempty"`
	Resolution    string               `json:"resolution,omitempty"`
	ResolvedBy    string               `json:"resolved_by,omitempty"`
	CreatedAt   int64                  `json:"created_at"`
	ResolvedAt  int64                  `json:"resolved_at,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// Solution 解决方案
type Solution struct {
	ID          string                 `json:"id"`
	DebateID    string                 `json:"debate_id"`
	ProposedBy  string                 `json:"proposed_by"` // agent ID
	Role        AgentRole              `json:"role"`
	Title       string                 `json:"title"`
	Description string                 `json:"description"`
	Code        string                 `json:"code,omitempty"`
	Pros        []string               `json:"pros,omitempty"`
	Cons        []string               `json:"cons,omitempty"`
	Score       float64                `json:"score"`
	CreatedAt   int64                  `json:"created_at"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// AuditReport 审计报告
type AuditReport struct {
	DebateID       string      `json:"debate_id"`
	Title          string      `json:"title"`
	Status         DebateStatus `json:"status"`
	TotalRounds    int         `json:"total_rounds"`
	TotalConflicts int         `json:"total_conflicts"`
	ResolvedConflicts int      `json:"resolved_conflicts"`
	OpenConflicts  int         `json:"open_conflicts"`
	Solutions      []*Solution `json:"solutions"`
	SelectedSolution string    `json:"selected_solution,omitempty"`
	Timeline       []TimelineEvent `json:"timeline"`
	GeneratedAt    int64       `json:"generated_at"`
}

// TimelineEvent 时间线事件
type TimelineEvent struct {
	Timestamp   int64  `json:"timestamp"`
	Type        string `json:"type"` // round_start, round_end, conflict_found, conflict_resolved, solution_proposed
	Description string `json:"description"`
	ActorID     string `json:"actor_id,omitempty"`
	ActorRole   AgentRole `json:"actor_role,omitempty"`
}

// DebateCreateRequest 创建辩论请求
type DebateCreateRequest struct {
	Title       string                 `json:"title" binding:"required"`
	Description string                 `json:"description,omitempty"`
	GeneratorID string                 `json:"generator_id" binding:"required"`
	CriticID    string                 `json:"critic_id" binding:"required"`
	MediatorID  string                 `json:"mediator_id,omitempty"`
	MaxRounds   int                    `json:"max_rounds,omitempty"` // default 5
	InitialInput string                `json:"initial_input" binding:"required"`
	FlowID      string                 `json:"flow_id,omitempty"`
	StageID     string                 `json:"stage_id,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// NextRoundRequest 下一轮请求
type NextRoundRequest struct {
	GeneratorOutput string `json:"generator_output" binding:"required"`
	CriticFeedback  string `json:"critic_feedback" binding:"required"`
}

// ResolveConflictRequest 解决冲突请求
type ResolveConflictRequest struct {
	Resolution string `json:"resolution" binding:"required"`
	ResolvedBy string `json:"resolved_by" binding:"required"`
}

// SelectSolutionRequest 选择方案请求
type SelectSolutionRequest struct {
	SolutionID string `json:"solution_id" binding:"required"`
}

// ProposeSolutionRequest 提出方案请求
type ProposeSolutionRequest struct {
	ProposedBy  string   `json:"proposed_by" binding:"required"`
	Role        AgentRole `json:"role" binding:"required"`
	Title       string   `json:"title" binding:"required"`
	Description string   `json:"description" binding:"required"`
	Code        string   `json:"code,omitempty"`
	Pros        []string `json:"pros,omitempty"`
	Cons        []string `json:"cons,omitempty"`
}

// DebateListRequest 辩论列表请求
type DebateListRequest struct {
	Status  string `form:"status"`
	FlowID  string `form:"flow_id"`
	StageID string `form:"stage_id"`
	Limit   int    `form:"limit"`
	Offset  int    `form:"offset"`
}

// DebateListResponse 辩论列表响应
type DebateListResponse struct {
	Debates []Debate `json:"debates"`
	Total   int      `json:"total"`
	HasMore bool     `json:"has_more"`
}

// IDebateManager 辩论管理接口
type IDebateManager interface {
	// Debate CRUD
	CreateDebate(ctx context.Context, req *DebateCreateRequest) (*Debate, error)
	GetDebate(ctx context.Context, id string) (*Debate, error)
	ListDebates(ctx context.Context, req *DebateListRequest) (*DebateListResponse, error)

	// Round management
	NextRound(ctx context.Context, debateID string, req *NextRoundRequest) (*Debate, error)

	// Conflict management
	DetectConflicts(generatorOutput, criticFeedback string) []*Conflict
	ResolveConflict(ctx context.Context, debateID, conflictID string, req *ResolveConflictRequest) (*Conflict, error)

	// Solution management
	ProposeSolution(ctx context.Context, debateID string, req *ProposeSolutionRequest) (*Solution, error)
	SelectSolution(ctx context.Context, debateID string, req *SelectSolutionRequest) (*Debate, error)

	// Export
	ExportReport(ctx context.Context, debateID string) (*AuditReport, error)
}

// InMemoryDebateManager 内存实现的辩论管理器
type InMemoryDebateManager struct {
	mu      sync.RWMutex
	debates map[string]*Debate
}

// NewInMemoryDebateManager 创建内存辩论管理器
func NewInMemoryDebateManager() *InMemoryDebateManager {
	return &InMemoryDebateManager{
		debates: make(map[string]*Debate),
	}
}

// CreateDebate 创建辩论
func (m *InMemoryDebateManager) CreateDebate(ctx context.Context, req *DebateCreateRequest) (*Debate, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	maxRounds := req.MaxRounds
	if maxRounds <= 0 {
		maxRounds = 5
	}

	now := time.Now().Unix()
	debate := &Debate{
		ID:           uuid.New().String(),
		Title:        req.Title,
		Description:  req.Description,
		Status:       DebateStatusInProgress,
		CurrentRound: 1,
		MaxRounds:    maxRounds,
		GeneratorID:  req.GeneratorID,
		CriticID:     req.CriticID,
		MediatorID:   req.MediatorID,
		FlowID:       req.FlowID,
		StageID:      req.StageID,
		Rounds: []*DebateRound{{
			Number:         1,
			GeneratorInput: req.InitialInput,
			StartedAt:      now,
		}},
		Conflicts:  make([]*Conflict, 0),
		Solutions:  make([]*Solution, 0),
		CreatedAt:  now,
		UpdatedAt:  now,
		Metadata:   req.Metadata,
	}

	m.debates[debate.ID] = debate
	return debate, nil
}

// GetDebate 获取辩论
func (m *InMemoryDebateManager) GetDebate(ctx context.Context, id string) (*Debate, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	debate, ok := m.debates[id]
	if !ok {
		return nil, nil
	}
	return debate, nil
}

// ListDebates 列出辩论
func (m *InMemoryDebateManager) ListDebates(ctx context.Context, req *DebateListRequest) (*DebateListResponse, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var filtered []*Debate
	for _, d := range m.debates {
		if req.Status != "" && string(d.Status) != req.Status {
			continue
		}
			if req.FlowID != "" && d.FlowID != req.FlowID {
				continue
			}
			if req.StageID != "" && d.StageID != req.StageID {
				continue
			}
		filtered = append(filtered, d)
	}

	// 按更新时间倒序
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].UpdatedAt > filtered[j].UpdatedAt
	})

	total := len(filtered)
	limit := req.Limit
	if limit <= 0 {
		limit = 20
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

	result := make([]Debate, end-start)
	for i := start; i < end; i++ {
		result[i-start] = *filtered[i]
	}

	return &DebateListResponse{
		Debates: result,
		Total:   total,
		HasMore: end < total,
	}, nil
}

// NextRound 进入下一轮
func (m *InMemoryDebateManager) NextRound(ctx context.Context, debateID string, req *NextRoundRequest) (*Debate, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	debate, ok := m.debates[debateID]
	if !ok {
		return nil, errors.New("debate not found")
	}

	if debate.Status != DebateStatusInProgress {
		return nil, errors.New("debate is not in progress")
	}

	now := time.Now().Unix()

	// 完成当前轮次
	currentRound := debate.Rounds[len(debate.Rounds)-1]
	currentRound.GeneratorOutput = req.GeneratorOutput
	currentRound.CriticFeedback = req.CriticFeedback
	currentRound.CompletedAt = now

	// 检测冲突
	conflicts := m.DetectConflicts(req.GeneratorOutput, req.CriticFeedback)
	for _, c := range conflicts {
		c.ID = uuid.New().String()
		c.DebateID = debateID
		c.RoundNumber = debate.CurrentRound
		c.CreatedAt = now
		debate.Conflicts = append(debate.Conflicts, c)
		currentRound.ConflictsFound = append(currentRound.ConflictsFound, c.ID)
	}

	// 检查是否达到最大轮次
	if debate.CurrentRound >= debate.MaxRounds {
		debate.Status = DebateStatusPaused
		debate.UpdatedAt = now
		return debate, nil
	}

	// 开始新轮次
	debate.CurrentRound++
	debate.Rounds = append(debate.Rounds, &DebateRound{
		Number:         debate.CurrentRound,
		GeneratorInput: req.CriticFeedback, // Critic反馈作为下一轮输入
		StartedAt:      now,
	})
	debate.UpdatedAt = now

	return debate, nil
}

// DetectConflicts 检测冲突
func (m *InMemoryDebateManager) DetectConflicts(generatorOutput, criticFeedback string) []*Conflict {
	conflicts := make([]*Conflict, 0)

	// 简化的冲突检测算法
	// 实际应用中应使用更复杂的NLP/语义分析

	feedback := strings.ToLower(criticFeedback)

	// 检测逻辑冲突
	logicKeywords := []string{"incorrect", "wrong", "error", "bug", "flaw", "mistake", "invalid"}
	for _, kw := range logicKeywords {
		if strings.Contains(feedback, kw) {
			conflicts = append(conflicts, &Conflict{
				Type:        "logic",
				Severity:    m.determineSeverity(feedback, kw),
				Status:      ConflictStatusOpen,
				Description: "Logic issue detected: " + extractContext(criticFeedback, kw),
				CriticView:  criticFeedback,
			})
			break
		}
	}

	// 检测安全冲突
	securityKeywords := []string{"security", "vulnerability", "unsafe", "injection", "xss", "csrf"}
	for _, kw := range securityKeywords {
		if strings.Contains(feedback, kw) {
			conflicts = append(conflicts, &Conflict{
				Type:        "security",
				Severity:    SeverityCritical,
				Status:      ConflictStatusOpen,
				Description: "Security concern: " + extractContext(criticFeedback, kw),
				CriticView:  criticFeedback,
			})
			break
		}
	}

	// 检测性能冲突
	perfKeywords := []string{"slow", "performance", "inefficient", "optimize", "memory leak"}
	for _, kw := range perfKeywords {
		if strings.Contains(feedback, kw) {
			conflicts = append(conflicts, &Conflict{
				Type:        "performance",
				Severity:    SeverityMedium,
				Status:      ConflictStatusOpen,
				Description: "Performance issue: " + extractContext(criticFeedback, kw),
				CriticView:  criticFeedback,
			})
			break
		}
	}

	// 检测风格冲突
	styleKeywords := []string{"style", "naming", "convention", "format", "readability"}
	for _, kw := range styleKeywords {
		if strings.Contains(feedback, kw) {
			conflicts = append(conflicts, &Conflict{
				Type:        "style",
				Severity:    SeverityLow,
				Status:      ConflictStatusOpen,
				Description: "Style issue: " + extractContext(criticFeedback, kw),
				CriticView:  criticFeedback,
			})
			break
		}
	}

	return conflicts
}

// determineSeverity 确定严重程度
func (m *InMemoryDebateManager) determineSeverity(text, keyword string) ConflictSeverity {
	text = strings.ToLower(text)

	// 严重程度修饰词
	criticalMods := []string{"critical", "severe", "major", "breaking", "crash"}
	highMods := []string{"significant", "important", "serious"}
	lowMods := []string{"minor", "trivial", "small", "cosmetic"}

	for _, mod := range criticalMods {
		if strings.Contains(text, mod) {
			return SeverityCritical
		}
	}
	for _, mod := range highMods {
		if strings.Contains(text, mod) {
			return SeverityHigh
		}
	}
	for _, mod := range lowMods {
		if strings.Contains(text, mod) {
			return SeverityLow
		}
	}

	return SeverityMedium
}

// extractContext 提取上下文
func extractContext(text, keyword string) string {
	lower := strings.ToLower(text)
	idx := strings.Index(lower, keyword)
	if idx == -1 {
		return text[:min(100, len(text))]
	}

	start := max(0, idx-30)
	end := min(len(text), idx+len(keyword)+50)
	return "..." + text[start:end] + "..."
}

// ResolveConflict 解决冲突
func (m *InMemoryDebateManager) ResolveConflict(ctx context.Context, debateID, conflictID string, req *ResolveConflictRequest) (*Conflict, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	debate, ok := m.debates[debateID]
	if !ok {
		return nil, errors.New("debate not found")
	}

	for _, c := range debate.Conflicts {
		if c.ID == conflictID {
			if c.Status == ConflictStatusResolved {
				return nil, errors.New("conflict already resolved")
			}
			c.Resolution = req.Resolution
			c.ResolvedBy = req.ResolvedBy
			c.Status = ConflictStatusResolved
			c.ResolvedAt = time.Now().Unix()
			debate.UpdatedAt = time.Now().Unix()
			return c, nil
		}
	}

	return nil, errors.New("conflict not found")
}

// ProposeSolution 提出方案
func (m *InMemoryDebateManager) ProposeSolution(ctx context.Context, debateID string, req *ProposeSolutionRequest) (*Solution, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	debate, ok := m.debates[debateID]
	if !ok {
		return nil, errors.New("debate not found")
	}

	solution := &Solution{
		ID:          uuid.New().String(),
		DebateID:    debateID,
		ProposedBy:  req.ProposedBy,
		Role:        req.Role,
		Title:       req.Title,
		Description: req.Description,
		Code:        req.Code,
		Pros:        req.Pros,
		Cons:        req.Cons,
		Score:       m.calculateSolutionScore(req),
		CreatedAt:   time.Now().Unix(),
	}

	debate.Solutions = append(debate.Solutions, solution)
	debate.UpdatedAt = time.Now().Unix()

	return solution, nil
}

// calculateSolutionScore 计算方案评分
func (m *InMemoryDebateManager) calculateSolutionScore(req *ProposeSolutionRequest) float64 {
	// 简化评分：pros数量 - cons数量，归一化到0-1
	prosCount := len(req.Pros)
	consCount := len(req.Cons)
	total := prosCount + consCount
	if total == 0 {
		return 0.5
	}
	return float64(prosCount) / float64(total)
}

// SelectSolution 选择方案
func (m *InMemoryDebateManager) SelectSolution(ctx context.Context, debateID string, req *SelectSolutionRequest) (*Debate, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	debate, ok := m.debates[debateID]
	if !ok {
		return nil, errors.New("debate not found")
	}

	// 验证方案存在
	found := false
	for _, s := range debate.Solutions {
		if s.ID == req.SolutionID {
			found = true
			break
		}
	}
	if !found {
		return nil, errors.New("solution not found")
	}

	now := time.Now().Unix()
	debate.SelectedSolution = req.SolutionID
	debate.Status = DebateStatusResolved
	debate.ResolvedAt = now
	debate.UpdatedAt = now

	return debate, nil
}

// ExportReport 导出审计报告
func (m *InMemoryDebateManager) ExportReport(ctx context.Context, debateID string) (*AuditReport, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	debate, ok := m.debates[debateID]
	if !ok {
		return nil, errors.New("debate not found")
	}

	// 统计冲突
	resolvedCount := 0
	openCount := 0
	for _, c := range debate.Conflicts {
		if c.Status == ConflictStatusResolved {
			resolvedCount++
		} else if c.Status == ConflictStatusOpen {
			openCount++
		}
	}

	// 构建时间线
	timeline := make([]TimelineEvent, 0)

	// 添加创建事件
	timeline = append(timeline, TimelineEvent{
		Timestamp:   debate.CreatedAt,
		Type:        "debate_created",
		Description: "Debate created: " + debate.Title,
	})

	// 添加轮次事件
	for _, r := range debate.Rounds {
		timeline = append(timeline, TimelineEvent{
			Timestamp:   r.StartedAt,
			Type:        "round_start",
			Description: "Round " + string(rune('0'+r.Number)) + " started",
		})
		if r.CompletedAt > 0 {
			timeline = append(timeline, TimelineEvent{
				Timestamp:   r.CompletedAt,
				Type:        "round_end",
				Description: "Round " + string(rune('0'+r.Number)) + " completed",
			})
		}
	}

	// 添加冲突事件
	for _, c := range debate.Conflicts {
		timeline = append(timeline, TimelineEvent{
			Timestamp:   c.CreatedAt,
			Type:        "conflict_found",
			Description: c.Type + " conflict: " + c.Description[:min(50, len(c.Description))],
		})
		if c.ResolvedAt > 0 {
			timeline = append(timeline, TimelineEvent{
				Timestamp:   c.ResolvedAt,
				Type:        "conflict_resolved",
				Description: "Conflict resolved by " + c.ResolvedBy,
				ActorID:     c.ResolvedBy,
			})
		}
	}

	// 添加方案事件
	for _, s := range debate.Solutions {
		timeline = append(timeline, TimelineEvent{
			Timestamp:   s.CreatedAt,
			Type:        "solution_proposed",
			Description: "Solution proposed: " + s.Title,
			ActorID:     s.ProposedBy,
			ActorRole:   s.Role,
		})
	}

	// 按时间排序
	sort.Slice(timeline, func(i, j int) bool {
		return timeline[i].Timestamp < timeline[j].Timestamp
	})

	return &AuditReport{
		DebateID:          debateID,
		Title:             debate.Title,
		Status:            debate.Status,
		TotalRounds:       len(debate.Rounds),
		TotalConflicts:    len(debate.Conflicts),
		ResolvedConflicts: resolvedCount,
		OpenConflicts:     openCount,
		Solutions:         debate.Solutions,
		SelectedSolution:  debate.SelectedSolution,
		Timeline:          timeline,
		GeneratedAt:       time.Now().Unix(),
	}, nil
}

// ExportReportJSON 导出JSON格式报告
func (m *InMemoryDebateManager) ExportReportJSON(ctx context.Context, debateID string) ([]byte, error) {
	report, err := m.ExportReport(ctx, debateID)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(report, "", "  ")
}

// 辅助函数
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// 全局服务实例
var defaultDebateManager IDebateManager
var debateOnce sync.Once

// GetDebateManager 获取辩论管理器实例
func GetDebateManager() IDebateManager {
	if defaultDebateManager == nil {
		debateOnce.Do(func() {
			if defaultDebateManager == nil {
				defaultDebateManager = NewInMemoryDebateManager()
			}
		})
		// SetDebateManager may race with first Get after Reset(nil); re-check.
		if defaultDebateManager == nil {
			defaultDebateManager = NewInMemoryDebateManager()
		}
	}
	return defaultDebateManager
}

// SetDebateManager 设置辩论管理器实例（bootstrap / 测试）。
// 传入 nil 清除全局实例，使下次 Get 可重建（Reset 兼容）。
func SetDebateManager(dm IDebateManager) {
	defaultDebateManager = dm
	if dm == nil {
		// Allow lazy re-init after Reset: recreate Once.
		debateOnce = sync.Once{}
	}
}
