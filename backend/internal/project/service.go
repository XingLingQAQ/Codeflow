// Package project - Project service for workspace management
package project

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/codeflow/backend/internal/dbx"
	"github.com/codeflow/backend/internal/planner"
)

// ProjectStatus 项目状态
type ProjectStatus string

const (
	StatusActive    ProjectStatus = "active"
	StatusPlanning  ProjectStatus = "planning"
	StatusPaused    ProjectStatus = "paused"
	StatusCompleted ProjectStatus = "completed"
	StatusArchived  ProjectStatus = "archived"
)

// BindingState describes the lifecycle of a project's authoritative bindings.
// A project is never considered workspace-runnable until it is bound.
type BindingState string

const (
	BindingStateUnbound  BindingState = "unbound"
	BindingStateBound    BindingState = "bound"
	BindingStateDeleting BindingState = "deleting"
	BindingStateArchived BindingState = "archived"
)

// Project 项目
type Project struct {
	ID               string                 `json:"id"`
	Title            string                 `json:"title"`
	Description      string                 `json:"description,omitempty"`
	Status           ProjectStatus          `json:"status"`
	Progress         int                    `json:"progress"`
	Tags             []string               `json:"tags,omitempty"`
	GitBranch        string                 `json:"git_branch,omitempty"`
	CreatedAt        int64                  `json:"created_at"`
	UpdatedAt        int64                  `json:"updated_at"`
	LastActive       int64                  `json:"last_active"`
	PlanIDs          []string               `json:"plan_ids,omitempty"`
	PlanDocument     *PlanDocument          `json:"plan_document,omitempty"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
	WorkspaceRoot    string                 `json:"workspace_root,omitempty"`
	DefaultFlowID    string                 `json:"default_flow_id,omitempty"`
	DefaultSessionID string                 `json:"default_session_id,omitempty"`
	BindingState     BindingState           `json:"binding_state"`
}

// ProjectCreateRequest 创建项目请求
type ProjectCreateRequest struct {
	// ID is assigned by project creation coordinators that need to provision
	// related resources before the project is persisted. It is never accepted
	// from the HTTP payload.
	ID               string                 `json:"-"`
	Title            string                 `json:"title" binding:"required"`
	Description      string                 `json:"description,omitempty"`
	Status           ProjectStatus          `json:"status,omitempty"`
	Tags             []string               `json:"tags,omitempty"`
	GitBranch        string                 `json:"git_branch,omitempty"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
	WorkspaceRoot    string                 `json:"workspace_root,omitempty"`
	DefaultFlowID    string                 `json:"-"`
	DefaultSessionID string                 `json:"-"`
}

var ErrInvalidProjectCreate = errors.New("invalid project create request")

const (
	MaxProjectTitleLength       = 200
	MaxProjectDescriptionLength = 10_000
	MaxProjectTags              = 20
	MaxProjectTagLength         = 64
)

// ProjectUpdateRequest 更新项目请求
type ProjectUpdateRequest struct {
	Title         *string                `json:"title,omitempty"`
	Description   *string                `json:"description,omitempty"`
	Status        *ProjectStatus         `json:"status,omitempty"`
	Tags          []string               `json:"tags,omitempty"`
	GitBranch     *string                `json:"git_branch,omitempty"`
	Progress      *int                   `json:"progress,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
	WorkspaceRoot *string                `json:"workspace_root,omitempty"`
}

// ProjectListRequest 项目列表请求
type ProjectListRequest struct {
	Status string `form:"status"`
	Tag    string `form:"tag"`
	Search string `form:"search"`
	Limit  int    `form:"limit"`
	Offset int    `form:"offset"`
}

// ProjectListResponse 项目列表响应
type ProjectListResponse struct {
	Projects []Project `json:"projects"`
	Total    int       `json:"total"`
	HasMore  bool      `json:"has_more"`
}

// AddPlanRequest 关联 Plan 请求
type AddPlanRequest struct {
	PlanID string `json:"plan_id" binding:"required"`
}

// PlanDocumentStatus 表示项目计划文档状态。
type PlanDocumentStatus string

const (
	PlanDocumentStatusReady         PlanDocumentStatus = "ready"
	PlanDocumentStatusNeedsRevision PlanDocumentStatus = "needs_revision"
	PlanDocumentStatusApproved      PlanDocumentStatus = "approved"
)

// PlanStep 是计划文档中的高层步骤。
type PlanStep struct {
	ID          string                 `json:"id,omitempty"`
	Title       string                 `json:"title"`
	Description string                 `json:"description,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// PlanTask 是计划文档中的可执行任务。
type PlanTask struct {
	TaskID         string                 `json:"task_id"`
	Title          string                 `json:"title"`
	Description    string                 `json:"description,omitempty"`
	Parallelizable bool                   `json:"parallelizable"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
}

// PlanReview 汇总计划文档的复核关注点。
type PlanReview struct {
	ReviewFocus []string `json:"review_focus"`
}

// PlanDecisionRationale 记录计划决策依据。
type PlanDecisionRationale struct {
	Reasons []string `json:"reasons"`
}

// PlanOverview 汇总计划执行路径。
type PlanOverview struct {
	CriticalPathTaskIDs []string `json:"critical_path_task_ids"`
}

// PlanChangeRequestInput 是修订请求中的变更意见。
type PlanChangeRequestInput struct {
	Summary     string `json:"summary"`
	RequestedBy string `json:"requested_by,omitempty"`
}

// PlanChangeRequest 记录已应用的变更意见。
type PlanChangeRequest struct {
	ID                string `json:"id"`
	Summary           string `json:"summary"`
	RequestedBy       string `json:"requested_by,omitempty"`
	Status            string `json:"status"`
	AppliedInRevision int    `json:"applied_in_revision"`
}

// PlanFeedback 记录计划反馈。
type PlanFeedback struct {
	ID        string `json:"id"`
	Message   string `json:"message"`
	CreatedAt int64  `json:"created_at"`
}

// PlanFeedbackResolution 记录反馈处理结果。
type PlanFeedbackResolution struct {
	ChangeRequestID string `json:"change_request_id"`
	Decision        string `json:"decision"`
	Reason          string `json:"reason,omitempty"`
}

// PlanFeedbackLoop 汇总反馈闭环。
type PlanFeedbackLoop struct {
	FeedbackResolution []PlanFeedbackResolution `json:"feedback_resolution,omitempty"`
}

// PlanRevision 记录计划文档修订历史。
type PlanRevision struct {
	Revision  int    `json:"revision"`
	Summary   string `json:"summary,omitempty"`
	CreatedAt int64  `json:"created_at"`
}

// PlanningTrace 记录计划生成器执行信息。
type PlanningTrace struct {
	Mode       string `json:"mode"`
	StartedAt  int64  `json:"started_at"`
	FinishedAt int64  `json:"finished_at"`
}

// PlanningGenerationInput 是计划生成器输入。
type PlanningGenerationInput struct {
	Project Project             `json:"project"`
	Request PlanGenerateRequest `json:"request"`
}

// PlanningGenerator 生成计划文档草案。
type PlanningGenerator interface {
	Generate(ctx context.Context, input PlanningGenerationInput) (*PlanGenerateRequest, *PlanningTrace, error)
}

// PlanGenerateRequest 创建计划文档请求。
type PlanGenerateRequest struct {
	Title    string                 `json:"title,omitempty"`
	Summary  string                 `json:"summary,omitempty"`
	Prompt   string                 `json:"prompt,omitempty"`
	Goal     string                 `json:"goal,omitempty"`
	Steps    []PlanStep             `json:"steps,omitempty"`
	Tasks    []PlanTask             `json:"tasks,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// PlanReviseRequest 修订计划文档请求。
type PlanReviseRequest struct {
	Goal               *string                  `json:"goal,omitempty"`
	Summary            *string                  `json:"summary,omitempty"`
	ChangeRequest      *PlanChangeRequestInput  `json:"change_request,omitempty"`
	Feedback           string                   `json:"feedback,omitempty"`
	FeedbackResolution []PlanFeedbackResolution `json:"feedback_resolution,omitempty"`
}

// PlanApproveRequest 批准计划文档请求。
type PlanApproveRequest struct {
	ApprovedBy string `json:"approved_by" binding:"required"`
}

// PlanDocument 是项目范围内的计划文档。
type PlanDocument struct {
	ID                string                 `json:"id"`
	ProjectID         string                 `json:"project_id"`
	Title             string                 `json:"title"`
	Summary           string                 `json:"summary,omitempty"`
	Goal              string                 `json:"goal,omitempty"`
	Status            PlanDocumentStatus     `json:"status"`
	Revision          int                    `json:"revision"`
	BasedOnRevision   int                    `json:"based_on_revision,omitempty"`
	ApprovedBy        string                 `json:"approved_by,omitempty"`
	CreatedAt         int64                  `json:"created_at"`
	UpdatedAt         int64                  `json:"updated_at"`
	Steps             []PlanStep             `json:"steps,omitempty"`
	Tasks             []PlanTask             `json:"tasks,omitempty"`
	Review            *PlanReview            `json:"review,omitempty"`
	DecisionRationale *PlanDecisionRationale `json:"decision_rationale,omitempty"`
	PlanOverview      *PlanOverview          `json:"plan_overview,omitempty"`
	ChangeRequests    []PlanChangeRequest    `json:"change_requests,omitempty"`
	Feedback          []PlanFeedback         `json:"feedback,omitempty"`
	FeedbackLoop      PlanFeedbackLoop       `json:"feedback_loop,omitempty"`
	Revisions         []PlanRevision         `json:"revisions,omitempty"`
	Metadata          map[string]interface{} `json:"metadata,omitempty"`
}

// IProjectService 项目管理接口
type IProjectService interface {
	CreateProject(ctx context.Context, req *ProjectCreateRequest) (*Project, error)
	GetProject(ctx context.Context, id string) (*Project, error)
	ListProjects(ctx context.Context, req *ProjectListRequest) (*ProjectListResponse, error)
	UpdateProject(ctx context.Context, id string, req *ProjectUpdateRequest) (*Project, error)
	DeleteProject(ctx context.Context, id string) error
	RestoreProject(ctx context.Context, id string) (*Project, error)
	UpdateRuntimeBindings(ctx context.Context, id, flowID, sessionID string) (*Project, error)
	BindWorkspaceRoot(ctx context.Context, id, root string) (*Project, error)
	AddPlanToProject(ctx context.Context, projectID, planID string) error
	RemovePlanFromProject(ctx context.Context, projectID, planID string) error
	GetProjectPlans(ctx context.Context, projectID string) ([]planner.Plan, error)
	GeneratePlanDocument(ctx context.Context, projectID string, req *PlanGenerateRequest) (*PlanDocument, error)
	GetPlanDocument(ctx context.Context, projectID string) (*PlanDocument, error)
	RevisePlanDocument(ctx context.Context, projectID string, req *PlanReviseRequest) (*PlanDocument, error)
	ApprovePlanDocument(ctx context.Context, projectID string, req *PlanApproveRequest) (*PlanDocument, error)
	RecalculateProgress(ctx context.Context, projectID string) error
}

// projectDeletionMarker is intentionally optional so existing lightweight
// project service implementations remain source-compatible.
type projectDeletionMarker interface {
	MarkProjectDeleting(ctx context.Context, id string) (*Project, error)
}

// InMemoryProjectService 内存实现的项目管理器
type InMemoryProjectService struct {
	mu                     sync.RWMutex
	projects               map[string]*Project
	planningGenerator      PlanningGenerator
	allowedRoots           []string
	allowUnrestrictedRoots bool
}

// NewInMemoryProjectService 创建内存项目管理器
func NewInMemoryProjectService() *InMemoryProjectService {
	return &InMemoryProjectService{
		projects: make(map[string]*Project),
	}
}

// SetAllowedWorkspaceRoots configures the canonical root policy used by new
// projects and directory bindings. An empty list rejects new bindings unless
// the explicit migration compatibility switch is enabled.
func (s *InMemoryProjectService) SetAllowedWorkspaceRoots(roots []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.allowedRoots = append([]string(nil), roots...)
}

// SetAllowUnrestrictedWorkspaceRoots enables the temporary desktop migration
// mode in which any existing directory may be bound after canonicalization.
func (s *InMemoryProjectService) SetAllowUnrestrictedWorkspaceRoots(allow bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.allowUnrestrictedRoots = allow
}

// CanonicalizeWorkspaceRoot applies the service-owned root policy without
// mutating project state.
func (s *InMemoryProjectService) CanonicalizeWorkspaceRoot(root string) (string, error) {
	s.mu.RLock()
	roots := append([]string(nil), s.allowedRoots...)
	allowUnrestricted := s.allowUnrestrictedRoots
	s.mu.RUnlock()
	return canonicalizeWorkspaceRoot(root, roots, allowUnrestricted)
}

// validStatus 校验状态枚举
func validStatus(s ProjectStatus) bool {
	switch s {
	case StatusActive, StatusPlanning, StatusPaused, StatusCompleted, StatusArchived:
		return true
	}
	return false
}

func normalizeProjectCreateRequest(req *ProjectCreateRequest) (*ProjectCreateRequest, error) {
	if req == nil {
		return nil, fmt.Errorf("%w: request is required", ErrInvalidProjectCreate)
	}

	normalized := *req
	normalized.Title = strings.TrimSpace(req.Title)
	normalized.Description = strings.TrimSpace(req.Description)
	normalized.GitBranch = strings.TrimSpace(req.GitBranch)
	if normalized.Title == "" {
		return nil, fmt.Errorf("%w: title is required", ErrInvalidProjectCreate)
	}
	if len([]rune(normalized.Title)) > MaxProjectTitleLength {
		return nil, fmt.Errorf("%w: title must be at most %d characters", ErrInvalidProjectCreate, MaxProjectTitleLength)
	}
	if len([]rune(normalized.Description)) > MaxProjectDescriptionLength {
		return nil, fmt.Errorf("%w: description must be at most %d characters", ErrInvalidProjectCreate, MaxProjectDescriptionLength)
	}
	if normalized.Status == "" {
		normalized.Status = StatusPlanning
	}
	if !validStatus(normalized.Status) {
		return nil, fmt.Errorf("%w: invalid status %q", ErrInvalidProjectCreate, normalized.Status)
	}

	tags := make([]string, 0, len(req.Tags))
	seen := make(map[string]struct{}, len(req.Tags))
	for _, raw := range req.Tags {
		tag := strings.TrimSpace(raw)
		if tag == "" {
			continue
		}
		if len([]rune(tag)) > MaxProjectTagLength {
			return nil, fmt.Errorf("%w: each tag must be at most %d characters", ErrInvalidProjectCreate, MaxProjectTagLength)
		}
		if _, exists := seen[tag]; exists {
			continue
		}
		seen[tag] = struct{}{}
		tags = append(tags, tag)
	}
	if len(tags) > MaxProjectTags {
		return nil, fmt.Errorf("%w: at most %d tags are allowed", ErrInvalidProjectCreate, MaxProjectTags)
	}
	normalized.Tags = tags
	if normalized.WorkspaceRoot != "" {
		// Root policy is applied by the owning service after normalization.
		normalized.WorkspaceRoot = strings.TrimSpace(normalized.WorkspaceRoot)
	}
	return &normalized, nil
}

// CreateProject 创建项目
func (s *InMemoryProjectService) CreateProject(ctx context.Context, req *ProjectCreateRequest) (*Project, error) {
	normalized, err := normalizeProjectCreateRequest(req)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if normalized.WorkspaceRoot != "" {
		canonical, err := canonicalizeWorkspaceRoot(normalized.WorkspaceRoot, s.allowedRoots, s.allowUnrestrictedRoots)
		if err != nil {
			return nil, fmt.Errorf("%w: workspace root: %v", ErrInvalidProjectCreate, err)
		}
		normalized.WorkspaceRoot = canonical
	}

	id := normalized.ID
	if id == "" {
		id = uuid.New().String()
	}
	if _, exists := s.projects[id]; exists {
		return nil, fmt.Errorf("project already exists: %s", id)
	}

	now := time.Now().Unix()
	p := &Project{
		ID:               id,
		Title:            normalized.Title,
		Description:      normalized.Description,
		Status:           normalized.Status,
		Progress:         0,
		Tags:             normalized.Tags,
		GitBranch:        normalized.GitBranch,
		CreatedAt:        now,
		UpdatedAt:        now,
		LastActive:       now,
		PlanIDs:          []string{},
		Metadata:         normalized.Metadata,
		BindingState:     BindingStateUnbound,
		DefaultFlowID:    normalized.DefaultFlowID,
		DefaultSessionID: normalized.DefaultSessionID,
		WorkspaceRoot:    normalized.WorkspaceRoot,
	}
	if p.WorkspaceRoot != "" {
		p.BindingState = BindingStateBound
	}

	s.projects[p.ID] = p
	return p, nil
}

// GetProject 获取项目
func (s *InMemoryProjectService) GetProject(ctx context.Context, id string) (*Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	p, ok := s.projects[id]
	if !ok {
		return nil, nil
	}
	return p, nil
}

// ListProjects 列出项目
func (s *InMemoryProjectService) ListProjects(ctx context.Context, req *ProjectListRequest) (*ProjectListResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var filtered []*Project
	for _, p := range s.projects {
		if req.Status != "" && string(p.Status) != req.Status {
			continue
		}
		if req.Tag != "" {
			found := false
			for _, t := range p.Tags {
				if t == req.Tag {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		if req.Search != "" {
			q := strings.ToLower(req.Search)
			if !strings.Contains(strings.ToLower(p.Title), q) &&
				!strings.Contains(strings.ToLower(p.Description), q) {
				continue
			}
		}
		filtered = append(filtered, p)
	}

	// 按最近活跃时间倒序，时间相同按 ID 倒序，保证稳定顺序
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].LastActive == filtered[j].LastActive {
			return filtered[i].ID > filtered[j].ID
		}
		return filtered[i].LastActive > filtered[j].LastActive
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

	result := make([]Project, end-start)
	for i := start; i < end; i++ {
		result[i-start] = *filtered[i]
	}

	return &ProjectListResponse{
		Projects: result,
		Total:    total,
		HasMore:  end < total,
	}, nil
}

// UpdateProject 更新项目
func (s *InMemoryProjectService) UpdateProject(ctx context.Context, id string, req *ProjectUpdateRequest) (*Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, ok := s.projects[id]
	if !ok {
		return nil, errors.New("project not found")
	}

	now := time.Now().Unix()

	if req.Title != nil {
		p.Title = *req.Title
	}
	if req.Description != nil {
		p.Description = *req.Description
	}
	if req.Status != nil {
		if !validStatus(*req.Status) {
			return nil, errors.New("invalid status: " + string(*req.Status))
		}
		p.Status = *req.Status
	}
	if req.Tags != nil {
		p.Tags = req.Tags
	}
	if req.GitBranch != nil {
		p.GitBranch = *req.GitBranch
	}
	if req.Progress != nil {
		prog := *req.Progress
		if prog < 0 {
			prog = 0
		}
		if prog > 100 {
			prog = 100
		}
		p.Progress = prog
	}
	if req.Metadata != nil {
		p.Metadata = req.Metadata
	}
	if req.WorkspaceRoot != nil {
		canonical, err := canonicalizeWorkspaceRoot(*req.WorkspaceRoot, s.allowedRoots, s.allowUnrestrictedRoots)
		if err != nil {
			return nil, err
		}
		p.WorkspaceRoot = canonical
		p.BindingState = BindingStateBound
	}

	p.UpdatedAt = now
	p.LastActive = now

	return p, nil
}

// DeleteProject 删除项目（仅解除 Plan 关联，不级联删除 Plans）
func (s *InMemoryProjectService) DeleteProject(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, ok := s.projects[id]
	if !ok {
		return errors.New("project not found")
	}
	p.Status = StatusArchived
	p.BindingState = BindingStateArchived
	p.UpdatedAt = time.Now().Unix()
	p.LastActive = p.UpdatedAt
	return nil
}

func (s *InMemoryProjectService) MarkProjectDeleting(ctx context.Context, id string) (*Project, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.projects[id]
	if !ok {
		return nil, errors.New("project not found")
	}
	p.BindingState = BindingStateDeleting
	p.UpdatedAt = time.Now().Unix()
	p.LastActive = p.UpdatedAt
	return cloneProject(p), nil
}

// RestoreProject returns an archived project to its previous runnable state.
func (s *InMemoryProjectService) RestoreProject(ctx context.Context, id string) (*Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.projects[id]
	if !ok {
		return nil, errors.New("project not found")
	}
	if p.Status != StatusArchived {
		return cloneProject(p), nil
	}
	p.Status = StatusPlanning
	if p.WorkspaceRoot != "" {
		p.BindingState = BindingStateBound
	} else {
		p.BindingState = BindingStateUnbound
	}
	p.UpdatedAt = time.Now().Unix()
	p.LastActive = p.UpdatedAt
	return cloneProject(p), nil
}

// UpdateRuntimeBindings changes only the durable default Flow/Session IDs. It
// is used by restore recovery after a previously aborted default Flow is
// replaced with a fresh runnable Flow.
func (s *InMemoryProjectService) UpdateRuntimeBindings(ctx context.Context, id, flowID, sessionID string) (*Project, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.projects[id]
	if !ok {
		return nil, errors.New("project not found")
	}
	if strings.TrimSpace(flowID) == "" || strings.TrimSpace(sessionID) == "" {
		return nil, errors.New("flow_id and session_id are required")
	}
	p.DefaultFlowID = flowID
	p.DefaultSessionID = sessionID
	p.UpdatedAt = time.Now().Unix()
	p.LastActive = p.UpdatedAt
	return cloneProject(p), nil
}

// AddPlanToProject 关联 Plan 到项目
func (s *InMemoryProjectService) AddPlanToProject(ctx context.Context, projectID, planID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, ok := s.projects[projectID]
	if !ok {
		return errors.New("project not found")
	}

	// 检查是否已关联
	for _, id := range p.PlanIDs {
		if id == planID {
			return errors.New("plan already associated")
		}
	}

	p.PlanIDs = append(p.PlanIDs, planID)
	p.UpdatedAt = time.Now().Unix()
	p.LastActive = p.UpdatedAt

	return nil
}

// RemovePlanFromProject 移除 Plan 关联
func (s *InMemoryProjectService) RemovePlanFromProject(ctx context.Context, projectID, planID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, ok := s.projects[projectID]
	if !ok {
		return errors.New("project not found")
	}

	found := false
	newIDs := make([]string, 0, len(p.PlanIDs))
	for _, id := range p.PlanIDs {
		if id == planID {
			found = true
			continue
		}
		newIDs = append(newIDs, id)
	}

	if !found {
		return errors.New("plan not associated with project")
	}

	p.PlanIDs = newIDs
	p.UpdatedAt = time.Now().Unix()
	p.LastActive = p.UpdatedAt

	return nil
}

// GetProjectPlans 获取项目关联的所有 Plans
func (s *InMemoryProjectService) GetProjectPlans(ctx context.Context, projectID string) ([]planner.Plan, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	p, ok := s.projects[projectID]
	if !ok {
		return nil, errors.New("project not found")
	}

	svc := planner.GetPlanner()
	var plans []planner.Plan
	for _, planID := range p.PlanIDs {
		plan, err := svc.GetPlan(ctx, planID)
		if err != nil {
			continue
		}
		if plan != nil {
			plans = append(plans, *plan)
		}
	}

	return plans, nil
}

// SetPlanningGenerator 设置计划生成器。
func (s *InMemoryProjectService) SetPlanningGenerator(generator PlanningGenerator) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.planningGenerator = generator
}

// GeneratePlanDocument 生成项目计划文档。
func (s *InMemoryProjectService) GeneratePlanDocument(ctx context.Context, projectID string, req *PlanGenerateRequest) (*PlanDocument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	project, ok := s.projects[projectID]
	if !ok {
		return nil, errors.New("project not found")
	}
	if req == nil {
		req = &PlanGenerateRequest{}
	}

	baseReq := clonePlanGenerateRequest(req)
	trace := &PlanningTrace{Mode: "synthetic", StartedAt: time.Now().Unix(), FinishedAt: time.Now().Unix()}
	if s.planningGenerator != nil {
		generated, generatedTrace, err := s.planningGenerator.Generate(ctx, PlanningGenerationInput{Project: *cloneProject(project), Request: *clonePlanGenerateRequest(req)})
		if err != nil {
			return nil, err
		}
		if generated != nil {
			baseReq = mergePlanGenerateRequests(generated, req)
		}
		if generatedTrace != nil {
			trace = generatedTrace
		}
	}

	now := time.Now().Unix()
	doc := buildPlanDocument(projectID, baseReq, trace, now)
	project.PlanDocument = doc
	project.Status = StatusPlanning
	project.UpdatedAt = now
	project.LastActive = now
	return clonePlanDocument(doc), nil
}

// GetPlanDocument 获取项目计划文档。
func (s *InMemoryProjectService) GetPlanDocument(ctx context.Context, projectID string) (*PlanDocument, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	project, ok := s.projects[projectID]
	if !ok {
		return nil, errors.New("project not found")
	}
	if project.PlanDocument == nil {
		return nil, nil
	}
	return clonePlanDocument(project.PlanDocument), nil
}

// RevisePlanDocument 修订项目计划文档。
func (s *InMemoryProjectService) RevisePlanDocument(ctx context.Context, projectID string, req *PlanReviseRequest) (*PlanDocument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	project, ok := s.projects[projectID]
	if !ok {
		return nil, errors.New("project not found")
	}
	if project.PlanDocument == nil {
		return nil, errors.New("plan document not found")
	}
	if req == nil {
		req = &PlanReviseRequest{}
	}

	doc := project.PlanDocument
	previousRevision := doc.Revision
	doc.BasedOnRevision = previousRevision
	doc.Revision++
	doc.Status = PlanDocumentStatusNeedsRevision
	doc.UpdatedAt = time.Now().Unix()
	if req.Goal != nil {
		doc.Goal = strings.TrimSpace(*req.Goal)
	}
	if req.Summary != nil {
		doc.Summary = strings.TrimSpace(*req.Summary)
	}

	if req.ChangeRequest != nil {
		changeRequest := newPlanChangeRequest(req.ChangeRequest.Summary, req.ChangeRequest.RequestedBy, doc.Revision)
		doc.ChangeRequests = append(doc.ChangeRequests, changeRequest)
		doc.Feedback = append(doc.Feedback, PlanFeedback{ID: uuid.New().String(), Message: changeRequest.Summary, CreatedAt: doc.UpdatedAt})
	}
	if strings.TrimSpace(req.Feedback) != "" {
		feedback := PlanFeedback{ID: uuid.New().String(), Message: strings.TrimSpace(req.Feedback), CreatedAt: doc.UpdatedAt}
		doc.Feedback = append(doc.Feedback, feedback)
		doc.ChangeRequests = append(doc.ChangeRequests, newPlanChangeRequest(feedback.Message, "feedback", doc.Revision))
	}
	if len(req.FeedbackResolution) > 0 {
		doc.FeedbackLoop.FeedbackResolution = append(doc.FeedbackLoop.FeedbackResolution, req.FeedbackResolution...)
		for _, resolution := range req.FeedbackResolution {
			for idx := range doc.ChangeRequests {
				if doc.ChangeRequests[idx].ID == resolution.ChangeRequestID {
					doc.ChangeRequests[idx].Status = resolution.Decision
					doc.ChangeRequests[idx].AppliedInRevision = doc.Revision
				}
			}
		}
	}
	doc.Revisions = append(doc.Revisions, PlanRevision{Revision: doc.Revision, Summary: doc.Summary, CreatedAt: doc.UpdatedAt})
	project.UpdatedAt = doc.UpdatedAt
	project.LastActive = doc.UpdatedAt
	return clonePlanDocument(doc), nil
}

// ApprovePlanDocument 批准项目计划文档。
func (s *InMemoryProjectService) ApprovePlanDocument(ctx context.Context, projectID string, req *PlanApproveRequest) (*PlanDocument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	project, ok := s.projects[projectID]
	if !ok {
		return nil, errors.New("project not found")
	}
	if project.PlanDocument == nil {
		return nil, errors.New("plan document not found")
	}
	approvedBy := ""
	if req != nil {
		approvedBy = strings.TrimSpace(req.ApprovedBy)
	}
	if approvedBy == "" {
		return nil, errors.New("approved_by is required")
	}

	now := time.Now().Unix()
	project.PlanDocument.Status = PlanDocumentStatusApproved
	project.PlanDocument.ApprovedBy = approvedBy
	project.PlanDocument.UpdatedAt = now
	project.Status = StatusActive
	project.UpdatedAt = now
	project.LastActive = now
	return clonePlanDocument(project.PlanDocument), nil
}

// RecalculateProgress 根据关联 Plans 的完成度重算进度。
func (s *InMemoryProjectService) RecalculateProgress(ctx context.Context, projectID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, ok := s.projects[projectID]
	if !ok {
		return errors.New("project not found")
	}

	if len(p.PlanIDs) == 0 {
		p.Progress = 0
		return nil
	}

	svc := planner.GetPlanner()
	totalTasks := 0
	completedTasks := 0

	for _, planID := range p.PlanIDs {
		plan, err := svc.GetPlan(ctx, planID)
		if err != nil || plan == nil {
			continue
		}
		totalTasks += plan.TaskCount
		completedTasks += plan.CompletedCount
	}

	if totalTasks == 0 {
		p.Progress = 0
	} else {
		p.Progress = (completedTasks * 100) / totalTasks
	}

	p.UpdatedAt = time.Now().Unix()
	return nil
}

type SQLiteProjectService struct {
	*InMemoryProjectService
	db      *sql.DB
	dbMu    sync.RWMutex
	writeMu sync.Mutex
}

const createProjectTablesSQL = `
CREATE TABLE IF NOT EXISTS projects (
	id TEXT PRIMARY KEY,
	title TEXT NOT NULL,
	description TEXT,
	status TEXT NOT NULL,
	progress INTEGER NOT NULL,
	tags_json TEXT NOT NULL DEFAULT '[]',
	git_branch TEXT,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL,
	last_active INTEGER NOT NULL,
	metadata_json TEXT,
	workspace_root TEXT,
	default_flow_id TEXT,
	default_session_id TEXT,
	binding_state TEXT NOT NULL DEFAULT 'unbound'
);

CREATE TABLE IF NOT EXISTS project_plans (
	project_id TEXT NOT NULL,
	plan_id TEXT NOT NULL,
	position INTEGER NOT NULL,
	PRIMARY KEY (project_id, plan_id),
	FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_projects_last_active ON projects(last_active DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_project_plans_project_position ON project_plans(project_id, position ASC, plan_id ASC);

CREATE TABLE IF NOT EXISTS project_operations (
	idempotency_key TEXT PRIMARY KEY,
	project_id TEXT NOT NULL,
	flow_id TEXT,
	session_id TEXT,
	title TEXT NOT NULL DEFAULT '',
	workspace_root TEXT,
	normalized_request_hash TEXT,
	operation TEXT NOT NULL,
	state TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
`

type projectStateSnapshot struct {
	projects map[string]*Project
}

func NewSQLiteProjectService(dbPath string) (*SQLiteProjectService, error) {
	if err := prepareProjectDBDir(dbPath); err != nil {
		return nil, err
	}

	db, err := dbx.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open project database: %w", err)
	}

	svc := &SQLiteProjectService{
		InMemoryProjectService: NewInMemoryProjectService(),
		db:                     db,
	}

	if err := svc.initialize(); err != nil {
		db.Close()
		return nil, err
	}

	return svc, nil
}

func prepareProjectDBDir(dbPath string) error {
	if dbPath == "" || dbPath == ":memory:" {
		return nil
	}
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create project db dir: %w", err)
		}
	}
	return nil
}

func (s *SQLiteProjectService) initialize() error {
	s.dbMu.Lock()
	defer s.dbMu.Unlock()

	if _, err := s.db.Exec(createProjectTablesSQL); err != nil {
		return fmt.Errorf("create project tables: %w", err)
	}
	// Existing databases predate the binding columns. SQLite has no IF NOT
	// EXISTS form for ALTER COLUMN, so probe each column and add it when needed.
	for _, stmt := range []string{
		`ALTER TABLE projects ADD COLUMN workspace_root TEXT`,
		`ALTER TABLE projects ADD COLUMN default_flow_id TEXT`,
		`ALTER TABLE projects ADD COLUMN default_session_id TEXT`,
		`ALTER TABLE projects ADD COLUMN binding_state TEXT NOT NULL DEFAULT 'unbound'`,
		`ALTER TABLE project_operations ADD COLUMN title TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE project_operations ADD COLUMN workspace_root TEXT`,
		`ALTER TABLE project_operations ADD COLUMN normalized_request_hash TEXT`,
	} {
		if _, err := s.db.Exec(stmt); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return fmt.Errorf("migrate project bindings: %w", err)
		}
	}

	return s.loadFromDBLocked()
}

func (s *SQLiteProjectService) loadFromDBLocked() error {
	projects := make(map[string]*Project)

	rows, err := s.db.Query(`
	SELECT id, title, description, status, progress, tags_json, git_branch, created_at, updated_at, last_active, metadata_json, workspace_root, default_flow_id, default_session_id, binding_state
		FROM projects
	`)
	if err != nil {
		return fmt.Errorf("query projects: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		project, err := scanProject(rows)
		if err != nil {
			return err
		}
		projects[project.ID] = project
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate projects: %w", err)
	}

	planRows, err := s.db.Query(`
		SELECT project_id, plan_id
		FROM project_plans
		ORDER BY project_id ASC, position ASC, plan_id ASC
	`)
	if err != nil {
		return fmt.Errorf("query project plans: %w", err)
	}
	defer planRows.Close()

	for planRows.Next() {
		var projectID string
		var planID string
		if err := planRows.Scan(&projectID, &planID); err != nil {
			return fmt.Errorf("scan project plan relation: %w", err)
		}
		if project, ok := projects[projectID]; ok {
			project.PlanIDs = append(project.PlanIDs, planID)
		}
	}
	if err := planRows.Err(); err != nil {
		return fmt.Errorf("iterate project plan relations: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.projects = projects
	return nil
}

func (s *SQLiteProjectService) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *SQLiteProjectService) GetCreateOperation(ctx context.Context, key string) (*CreateOperationRecord, error) {
	if strings.TrimSpace(key) == "" {
		return nil, nil
	}
	s.dbMu.RLock()
	defer s.dbMu.RUnlock()
	var record CreateOperationRecord
	var flowID, sessionID, workspaceRoot, requestHash sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT project_id, flow_id, session_id, state, title, workspace_root, normalized_request_hash FROM project_operations WHERE idempotency_key = ? AND operation = 'create_project'`, key).Scan(&record.ProjectID, &flowID, &sessionID, &record.State, &record.Title, &workspaceRoot, &requestHash)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get project operation: %w", err)
	}
	record.FlowID = flowID.String
	record.SessionID = sessionID.String
	record.WorkspaceRoot = workspaceRoot.String
	record.NormalizedRequestHash = requestHash.String
	record.IdempotencyKey = key
	return &record, nil
}

func (s *SQLiteProjectService) ListIncompleteCreateOperations(ctx context.Context) ([]*CreateOperationRecord, error) {
	s.dbMu.RLock()
	defer s.dbMu.RUnlock()
	// Resumable states are an explicit whitelist (see
	// isResumableCreateOperationState): terminal states — completed and the
	// reserved rolled_back — and any unknown state must never be resurrected
	// by recovery, so this is not `state <> 'completed'`.
	rows, err := s.db.QueryContext(ctx, `SELECT idempotency_key, project_id, flow_id, session_id, state, title, workspace_root, normalized_request_hash FROM project_operations WHERE operation = 'create_project' AND state IN (?, ?, ?) ORDER BY created_at, idempotency_key`, createOperationPending, createOperationSessionCreated, createOperationFlowCreated)
	if err != nil {
		return nil, fmt.Errorf("list incomplete project operations: %w", err)
	}
	defer rows.Close()
	var records []*CreateOperationRecord
	for rows.Next() {
		var record CreateOperationRecord
		var flowID, sessionID, workspaceRoot, requestHash sql.NullString
		if err := rows.Scan(&record.IdempotencyKey, &record.ProjectID, &flowID, &sessionID, &record.State, &record.Title, &workspaceRoot, &requestHash); err != nil {
			return nil, fmt.Errorf("scan incomplete project operation: %w", err)
		}
		record.FlowID = flowID.String
		record.SessionID = sessionID.String
		record.WorkspaceRoot = workspaceRoot.String
		record.NormalizedRequestHash = requestHash.String
		records = append(records, &record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate incomplete project operations: %w", err)
	}
	return records, nil
}

func (s *SQLiteProjectService) SaveCreateOperation(ctx context.Context, key string, record *CreateOperationRecord) error {
	if strings.TrimSpace(key) == "" || record == nil {
		return nil
	}
	now := time.Now().UnixMilli()
	s.dbMu.Lock()
	defer s.dbMu.Unlock()
	state := record.State
	if state == "" {
		state = createOperationCompleted
	}
	// normalized_request_hash is intentionally absent from the conflict
	// update: it binds the first payload accepted under the key and must
	// never be rewritten by a later payload. Legacy rows (NULL hash) keep
	// their empty marker instead of being backfilled from a new payload.
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO project_operations (idempotency_key, project_id, flow_id, session_id, title, workspace_root, normalized_request_hash, operation, state, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'create_project', ?, ?, ?)
		ON CONFLICT(idempotency_key) DO UPDATE SET
			project_id = excluded.project_id,
			flow_id = excluded.flow_id,
			session_id = excluded.session_id,
			title = excluded.title,
			workspace_root = excluded.workspace_root,
			state = excluded.state,
			updated_at = excluded.updated_at
	`, key, record.ProjectID, nullProjectString(record.FlowID), nullProjectString(record.SessionID), record.Title, nullProjectString(record.WorkspaceRoot), nullProjectString(record.NormalizedRequestHash), state, now, now)
	if err != nil {
		return fmt.Errorf("save project operation: %w", err)
	}
	return nil
}

func projectLifecycleOperationKey(operation, projectID string) string {
	return "project-lifecycle:" + operation + ":" + projectID
}

func (s *SQLiteProjectService) GetLifecycleOperation(ctx context.Context, operation, projectID string) (*LifecycleOperationRecord, error) {
	if strings.TrimSpace(operation) == "" || strings.TrimSpace(projectID) == "" {
		return nil, nil
	}
	s.dbMu.RLock()
	defer s.dbMu.RUnlock()
	var record LifecycleOperationRecord
	var flowID, sessionID, workspaceRoot sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT operation, project_id, flow_id, session_id, workspace_root, state
		FROM project_operations
		WHERE idempotency_key = ? AND operation = ?
	`, projectLifecycleOperationKey(operation, projectID), operation).Scan(
		&record.Operation,
		&record.ProjectID,
		&flowID,
		&sessionID,
		&workspaceRoot,
		&record.State,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get project lifecycle operation: %w", err)
	}
	record.FlowID = flowID.String
	record.SessionID = sessionID.String
	record.WorkspaceRoot = workspaceRoot.String
	return &record, nil
}

func (s *SQLiteProjectService) ListIncompleteLifecycleOperations(ctx context.Context) ([]*LifecycleOperationRecord, error) {
	s.dbMu.RLock()
	defer s.dbMu.RUnlock()
	rows, err := s.db.QueryContext(ctx, `
		SELECT operation, project_id, flow_id, session_id, workspace_root, state
		FROM project_operations
		WHERE operation IN (?, ?) AND state <> ?
		ORDER BY created_at, idempotency_key
	`, projectOperationArchive, projectOperationRestore, lifecycleOperationCompleted)
	if err != nil {
		return nil, fmt.Errorf("list incomplete project lifecycle operations: %w", err)
	}
	defer rows.Close()
	var records []*LifecycleOperationRecord
	for rows.Next() {
		var record LifecycleOperationRecord
		var flowID, sessionID, workspaceRoot sql.NullString
		if err := rows.Scan(&record.Operation, &record.ProjectID, &flowID, &sessionID, &workspaceRoot, &record.State); err != nil {
			return nil, fmt.Errorf("scan project lifecycle operation: %w", err)
		}
		record.FlowID = flowID.String
		record.SessionID = sessionID.String
		record.WorkspaceRoot = workspaceRoot.String
		records = append(records, &record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate project lifecycle operations: %w", err)
	}
	return records, nil
}

func (s *SQLiteProjectService) SaveLifecycleOperation(ctx context.Context, record *LifecycleOperationRecord) error {
	if record == nil || strings.TrimSpace(record.Operation) == "" || strings.TrimSpace(record.ProjectID) == "" {
		return fmt.Errorf("project lifecycle operation, project id, and state are required")
	}
	if record.Operation != projectOperationArchive && record.Operation != projectOperationRestore {
		return fmt.Errorf("unsupported project lifecycle operation %q", record.Operation)
	}
	if strings.TrimSpace(record.State) == "" {
		return fmt.Errorf("project lifecycle operation state is required")
	}
	now := time.Now().UnixMilli()
	s.dbMu.Lock()
	defer s.dbMu.Unlock()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO project_operations (idempotency_key, project_id, flow_id, session_id, title, workspace_root, operation, state, created_at, updated_at)
		VALUES (?, ?, ?, ?, '', ?, ?, ?, ?, ?)
		ON CONFLICT(idempotency_key) DO UPDATE SET
			project_id = excluded.project_id,
			flow_id = excluded.flow_id,
			session_id = excluded.session_id,
			workspace_root = excluded.workspace_root,
			operation = excluded.operation,
			state = excluded.state,
			updated_at = excluded.updated_at
	`, projectLifecycleOperationKey(record.Operation, record.ProjectID), record.ProjectID, nullProjectString(record.FlowID), nullProjectString(record.SessionID), nullProjectString(record.WorkspaceRoot), record.Operation, record.State, now, now)
	if err != nil {
		return fmt.Errorf("save project lifecycle operation: %w", err)
	}
	return nil
}

func (s *SQLiteProjectService) CreateProject(ctx context.Context, req *ProjectCreateRequest) (*Project, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	before := s.snapshotState()
	project, err := s.InMemoryProjectService.CreateProject(ctx, req)
	if err != nil {
		return nil, err
	}
	if err := s.persistCurrentState(); err != nil {
		s.restoreState(before)
		return nil, err
	}
	return project, nil
}

func (s *SQLiteProjectService) UpdateProject(ctx context.Context, id string, req *ProjectUpdateRequest) (*Project, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	before := s.snapshotState()
	project, err := s.InMemoryProjectService.UpdateProject(ctx, id, req)
	if err != nil {
		return nil, err
	}
	if err := s.persistCurrentState(); err != nil {
		s.restoreState(before)
		return nil, err
	}
	return project, nil
}

func (s *SQLiteProjectService) DeleteProject(ctx context.Context, id string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	before := s.snapshotState()
	if err := s.InMemoryProjectService.DeleteProject(ctx, id); err != nil {
		return err
	}
	if err := s.persistCurrentState(); err != nil {
		s.restoreState(before)
		return err
	}
	return nil
}

func (s *SQLiteProjectService) MarkProjectDeleting(ctx context.Context, id string) (*Project, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	before := s.snapshotState()
	project, err := s.InMemoryProjectService.MarkProjectDeleting(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.persistCurrentState(); err != nil {
		s.restoreState(before)
		return nil, err
	}
	return project, nil
}

func (s *SQLiteProjectService) RestoreProject(ctx context.Context, id string) (*Project, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	before := s.snapshotState()
	project, err := s.InMemoryProjectService.RestoreProject(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.persistCurrentState(); err != nil {
		s.restoreState(before)
		return nil, err
	}
	return project, nil
}

func (s *SQLiteProjectService) UpdateRuntimeBindings(ctx context.Context, id, flowID, sessionID string) (*Project, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	before := s.snapshotState()
	project, err := s.InMemoryProjectService.UpdateRuntimeBindings(ctx, id, flowID, sessionID)
	if err != nil {
		return nil, err
	}
	if err := s.persistCurrentState(); err != nil {
		s.restoreState(before)
		return nil, err
	}
	return project, nil
}

func (s *SQLiteProjectService) BindWorkspaceRoot(ctx context.Context, id, root string) (*Project, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	before := s.snapshotState()
	project, err := s.InMemoryProjectService.BindWorkspaceRoot(ctx, id, root)
	if err != nil {
		return nil, err
	}
	if err := s.persistCurrentState(); err != nil {
		s.restoreState(before)
		return nil, err
	}
	return project, nil
}

func (s *SQLiteProjectService) AddPlanToProject(ctx context.Context, projectID, planID string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	before := s.snapshotState()
	if err := s.InMemoryProjectService.AddPlanToProject(ctx, projectID, planID); err != nil {
		return err
	}
	if err := s.persistCurrentState(); err != nil {
		s.restoreState(before)
		return err
	}
	return nil
}

func (s *SQLiteProjectService) RemovePlanFromProject(ctx context.Context, projectID, planID string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	before := s.snapshotState()
	if err := s.InMemoryProjectService.RemovePlanFromProject(ctx, projectID, planID); err != nil {
		return err
	}
	if err := s.persistCurrentState(); err != nil {
		s.restoreState(before)
		return err
	}
	return nil
}

func (s *SQLiteProjectService) GeneratePlanDocument(ctx context.Context, projectID string, req *PlanGenerateRequest) (*PlanDocument, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	before := s.snapshotState()
	doc, err := s.InMemoryProjectService.GeneratePlanDocument(ctx, projectID, req)
	if err != nil {
		return nil, err
	}
	if err := s.persistCurrentState(); err != nil {
		s.restoreState(before)
		return nil, err
	}
	return doc, nil
}

func (s *SQLiteProjectService) RevisePlanDocument(ctx context.Context, projectID string, req *PlanReviseRequest) (*PlanDocument, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	before := s.snapshotState()
	doc, err := s.InMemoryProjectService.RevisePlanDocument(ctx, projectID, req)
	if err != nil {
		return nil, err
	}
	if err := s.persistCurrentState(); err != nil {
		s.restoreState(before)
		return nil, err
	}
	return doc, nil
}

func (s *SQLiteProjectService) ApprovePlanDocument(ctx context.Context, projectID string, req *PlanApproveRequest) (*PlanDocument, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	before := s.snapshotState()
	doc, err := s.InMemoryProjectService.ApprovePlanDocument(ctx, projectID, req)
	if err != nil {
		return nil, err
	}
	if err := s.persistCurrentState(); err != nil {
		s.restoreState(before)
		return nil, err
	}
	return doc, nil
}

func (s *SQLiteProjectService) RecalculateProgress(ctx context.Context, projectID string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	before := s.snapshotState()
	if err := s.InMemoryProjectService.RecalculateProgress(ctx, projectID); err != nil {
		return err
	}
	if err := s.persistCurrentState(); err != nil {
		s.restoreState(before)
		return err
	}
	return nil
}

func (s *SQLiteProjectService) persistCurrentState() error {
	snapshot := s.snapshotState()
	return s.saveSnapshot(snapshot)
}

func (s *SQLiteProjectService) saveSnapshot(snapshot projectStateSnapshot) error {
	s.dbMu.Lock()
	defer s.dbMu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin project transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM project_plans`); err != nil {
		return fmt.Errorf("clear project plans: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM projects`); err != nil {
		return fmt.Errorf("clear projects: %w", err)
	}

	for _, project := range snapshot.projects {
		tagsJSON, err := marshalProjectStrings(project.Tags)
		if err != nil {
			return fmt.Errorf("marshal project tags: %w", err)
		}
		metadataJSON, err := marshalProjectMetadata(project.Metadata)
		if err != nil {
			return fmt.Errorf("marshal project metadata: %w", err)
		}

		if _, err := tx.Exec(`
			INSERT INTO projects (id, title, description, status, progress, tags_json, git_branch, created_at, updated_at, last_active, metadata_json, workspace_root, default_flow_id, default_session_id, binding_state)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, project.ID, project.Title, project.Description, string(project.Status), project.Progress, tagsJSON, nullProjectString(project.GitBranch), project.CreatedAt, project.UpdatedAt, project.LastActive, nullProjectString(metadataJSON), nullProjectString(project.WorkspaceRoot), nullProjectString(project.DefaultFlowID), nullProjectString(project.DefaultSessionID), string(project.BindingState)); err != nil {
			return fmt.Errorf("insert project %s: %w", project.ID, err)
		}

		for idx, planID := range project.PlanIDs {
			if _, err := tx.Exec(`
				INSERT INTO project_plans (project_id, plan_id, position)
				VALUES (?, ?, ?)
			`, project.ID, planID, idx+1); err != nil {
				return fmt.Errorf("insert project plan relation %s/%s: %w", project.ID, planID, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit project transaction: %w", err)
	}
	return nil
}

func (s *SQLiteProjectService) snapshotState() projectStateSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	snapshot := projectStateSnapshot{projects: make(map[string]*Project, len(s.projects))}
	for id, project := range s.projects {
		snapshot.projects[id] = cloneProject(project)
	}
	return snapshot
}

func (s *SQLiteProjectService) restoreState(snapshot projectStateSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.projects = snapshot.projects
}

func scanProject(scanner interface{ Scan(dest ...any) error }) (*Project, error) {
	var project Project
	var description sql.NullString
	var tagsJSON string
	var gitBranch sql.NullString
	var metadataJSON sql.NullString
	var workspaceRoot, defaultFlowID, defaultSessionID, bindingState sql.NullString
	var status string

	if err := scanner.Scan(
		&project.ID,
		&project.Title,
		&description,
		&status,
		&project.Progress,
		&tagsJSON,
		&gitBranch,
		&project.CreatedAt,
		&project.UpdatedAt,
		&project.LastActive,
		&metadataJSON,
		&workspaceRoot,
		&defaultFlowID,
		&defaultSessionID,
		&bindingState,
	); err != nil {
		return nil, fmt.Errorf("scan project: %w", err)
	}

	project.Description = description.String
	project.Status = ProjectStatus(status)
	project.GitBranch = gitBranch.String

	tags, err := unmarshalProjectStrings(tagsJSON)
	if err != nil {
		return nil, fmt.Errorf("decode project tags: %w", err)
	}
	metadata, err := unmarshalProjectMetadata(metadataJSON.String)
	if err != nil {
		return nil, fmt.Errorf("decode project metadata: %w", err)
	}
	project.Tags = tags
	project.Metadata = metadata
	project.WorkspaceRoot = workspaceRoot.String
	project.DefaultFlowID = defaultFlowID.String
	project.DefaultSessionID = defaultSessionID.String
	project.BindingState = BindingState(bindingState.String)
	if project.BindingState == "" {
		project.BindingState = BindingStateUnbound
	}
	project.PlanIDs = []string{}
	return &project, nil
}

func cloneProject(project *Project) *Project {
	if project == nil {
		return nil
	}
	cloned := *project
	cloned.Tags = cloneProjectStrings(project.Tags)
	cloned.PlanIDs = cloneProjectStrings(project.PlanIDs)
	cloned.PlanDocument = clonePlanDocument(project.PlanDocument)
	cloned.Metadata = cloneProjectMetadata(project.Metadata)
	return &cloned
}

func clonePlanGenerateRequest(req *PlanGenerateRequest) *PlanGenerateRequest {
	if req == nil {
		return &PlanGenerateRequest{}
	}
	cloned := *req
	cloned.Steps = clonePlanSteps(req.Steps)
	cloned.Tasks = clonePlanTasks(req.Tasks)
	cloned.Metadata = cloneProjectMetadata(req.Metadata)
	return &cloned
}

func mergePlanGenerateRequests(generated, override *PlanGenerateRequest) *PlanGenerateRequest {
	merged := clonePlanGenerateRequest(generated)
	if override == nil {
		return merged
	}
	if strings.TrimSpace(override.Title) != "" {
		merged.Title = override.Title
	}
	if strings.TrimSpace(override.Summary) != "" {
		merged.Summary = override.Summary
	}
	if strings.TrimSpace(override.Goal) != "" {
		merged.Goal = override.Goal
	}
	if len(override.Steps) > 0 {
		merged.Steps = clonePlanSteps(override.Steps)
	}
	if len(override.Tasks) > 0 {
		merged.Tasks = clonePlanTasks(override.Tasks)
	}
	if merged.Metadata == nil {
		merged.Metadata = map[string]interface{}{}
	}
	for key, value := range override.Metadata {
		merged.Metadata[key] = value
	}
	if strings.TrimSpace(override.Prompt) != "" {
		merged.Prompt = override.Prompt
	}
	return merged
}

func buildPlanDocument(projectID string, req *PlanGenerateRequest, trace *PlanningTrace, now int64) *PlanDocument {
	if req == nil {
		req = &PlanGenerateRequest{}
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "Project planning document"
	}
	summary := strings.TrimSpace(req.Summary)
	if summary == "" && strings.TrimSpace(req.Prompt) != "" {
		summary = strings.TrimSpace(req.Prompt)
	}
	goal := strings.TrimSpace(req.Goal)
	if goal == "" {
		goal = title
	}
	steps := clonePlanSteps(req.Steps)
	if len(steps) == 0 {
		steps = []PlanStep{{ID: "plan", Title: title}}
	}
	tasks := clonePlanTasks(req.Tasks)
	if len(tasks) == 0 {
		tasks = tasksFromSteps(steps)
	}
	metadata := cloneProjectMetadata(req.Metadata)
	if metadata == nil {
		metadata = map[string]interface{}{}
	}
	metadata["planning_mode"] = "tool-first"
	if strings.TrimSpace(req.Prompt) != "" {
		metadata["prompt"] = strings.TrimSpace(req.Prompt)
	}
	if trace == nil {
		trace = &PlanningTrace{Mode: "synthetic", StartedAt: now, FinishedAt: now}
	}
	metadata["planning_executor"] = trace.Mode
	metadata["planning_trace"] = trace

	criticalPath := make([]string, 0, len(tasks))
	for _, task := range tasks {
		criticalPath = append(criticalPath, task.TaskID)
	}
	return &PlanDocument{
		ID:        uuid.New().String(),
		ProjectID: projectID,
		Title:     title,
		Summary:   summary,
		Goal:      goal,
		Status:    PlanDocumentStatusReady,
		Revision:  1,
		CreatedAt: now,
		UpdatedAt: now,
		Steps:     steps,
		Tasks:     tasks,
		Review: &PlanReview{ReviewFocus: []string{
			"scope",
			"risk",
			"verification",
		}},
		DecisionRationale: &PlanDecisionRationale{Reasons: []string{"Generated from project planning input"}},
		PlanOverview:      &PlanOverview{CriticalPathTaskIDs: criticalPath},
		Revisions:         []PlanRevision{{Revision: 1, Summary: summary, CreatedAt: now}},
		Metadata:          metadata,
	}
}

func tasksFromSteps(steps []PlanStep) []PlanTask {
	tasks := make([]PlanTask, 0, len(steps))
	for idx, step := range steps {
		taskID := strings.TrimSpace(step.ID)
		if taskID == "" {
			taskID = fmt.Sprintf("task-%d", idx+1)
		}
		title := strings.TrimSpace(step.Title)
		if title == "" {
			title = taskID
		}
		tasks = append(tasks, PlanTask{TaskID: taskID, Title: title, Description: step.Description, Metadata: cloneProjectMetadata(step.Metadata)})
	}
	return tasks
}

func newPlanChangeRequest(summary, requestedBy string, revision int) PlanChangeRequest {
	return PlanChangeRequest{
		ID:                uuid.New().String(),
		Summary:           strings.TrimSpace(summary),
		RequestedBy:       strings.TrimSpace(requestedBy),
		Status:            "applied",
		AppliedInRevision: revision,
	}
}

func clonePlanDocument(doc *PlanDocument) *PlanDocument {
	if doc == nil {
		return nil
	}
	cloned := *doc
	cloned.Steps = clonePlanSteps(doc.Steps)
	cloned.Tasks = clonePlanTasks(doc.Tasks)
	if doc.Review != nil {
		review := *doc.Review
		review.ReviewFocus = cloneProjectStrings(doc.Review.ReviewFocus)
		cloned.Review = &review
	}
	if doc.DecisionRationale != nil {
		rationale := *doc.DecisionRationale
		rationale.Reasons = cloneProjectStrings(doc.DecisionRationale.Reasons)
		cloned.DecisionRationale = &rationale
	}
	if doc.PlanOverview != nil {
		overview := *doc.PlanOverview
		overview.CriticalPathTaskIDs = cloneProjectStrings(doc.PlanOverview.CriticalPathTaskIDs)
		cloned.PlanOverview = &overview
	}
	cloned.ChangeRequests = append([]PlanChangeRequest(nil), doc.ChangeRequests...)
	cloned.Feedback = append([]PlanFeedback(nil), doc.Feedback...)
	cloned.FeedbackLoop.FeedbackResolution = append([]PlanFeedbackResolution(nil), doc.FeedbackLoop.FeedbackResolution...)
	cloned.Revisions = append([]PlanRevision(nil), doc.Revisions...)
	cloned.Metadata = cloneProjectMetadata(doc.Metadata)
	return &cloned
}

func clonePlanSteps(steps []PlanStep) []PlanStep {
	if steps == nil {
		return nil
	}
	cloned := make([]PlanStep, len(steps))
	for idx, step := range steps {
		cloned[idx] = step
		cloned[idx].Metadata = cloneProjectMetadata(step.Metadata)
	}
	return cloned
}

func clonePlanTasks(tasks []PlanTask) []PlanTask {
	if tasks == nil {
		return nil
	}
	cloned := make([]PlanTask, len(tasks))
	for idx, task := range tasks {
		cloned[idx] = task
		cloned[idx].Metadata = cloneProjectMetadata(task.Metadata)
	}
	return cloned
}

func cloneProjectStrings(values []string) []string {
	if values == nil {
		return nil
	}
	return append([]string(nil), values...)
}

func marshalProjectStrings(values []string) (string, error) {
	if len(values) == 0 {
		return "[]", nil
	}
	data, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func unmarshalProjectStrings(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, err
	}
	return values, nil
}

func cloneProjectMetadata(metadata map[string]interface{}) map[string]interface{} {
	if metadata == nil {
		return nil
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		return map[string]interface{}{}
	}
	var cloned map[string]interface{}
	if err := json.Unmarshal(data, &cloned); err != nil {
		return map[string]interface{}{}
	}
	return cloned
}

func marshalProjectMetadata(metadata map[string]interface{}) (string, error) {
	if len(metadata) == 0 {
		return "", nil
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func unmarshalProjectMetadata(raw string) (map[string]interface{}, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var metadata map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
		return nil, err
	}
	return metadata, nil
}

func nullProjectString(value string) interface{} {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

// 全局服务实例
var defaultProjectService IProjectService

// GetProjectService 获取项目管理器实例
func GetProjectService() IProjectService {
	if defaultProjectService == nil {
		defaultProjectService = NewInMemoryProjectService()
	}
	return defaultProjectService
}

// HasProjectService reports whether the global project service has been configured.
func HasProjectService() bool {
	return defaultProjectService != nil
}

// SetProjectService 设置项目管理器实例（用于测试）
func SetProjectService(svc IProjectService) {
	defaultProjectService = svc
}
