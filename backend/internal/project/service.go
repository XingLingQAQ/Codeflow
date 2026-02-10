// Package project - Project service for workspace management
package project

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

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

// Project 项目
type Project struct {
	ID          string                 `json:"id"`
	Title       string                 `json:"title"`
	Description string                 `json:"description,omitempty"`
	Status      ProjectStatus          `json:"status"`
	Progress    int                    `json:"progress"`
	Tags        []string               `json:"tags,omitempty"`
	GitBranch   string                 `json:"git_branch,omitempty"`
	CreatedAt   int64                  `json:"created_at"`
	UpdatedAt   int64                  `json:"updated_at"`
	LastActive  int64                  `json:"last_active"`
	PlanIDs     []string               `json:"plan_ids,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// ProjectCreateRequest 创建项目请求
type ProjectCreateRequest struct {
	Title       string                 `json:"title" binding:"required"`
	Description string                 `json:"description,omitempty"`
	Status      ProjectStatus          `json:"status,omitempty"`
	Tags        []string               `json:"tags,omitempty"`
	GitBranch   string                 `json:"git_branch,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// ProjectUpdateRequest 更新项目请求
type ProjectUpdateRequest struct {
	Title       *string                `json:"title,omitempty"`
	Description *string                `json:"description,omitempty"`
	Status      *ProjectStatus         `json:"status,omitempty"`
	Tags        []string               `json:"tags,omitempty"`
	GitBranch   *string                `json:"git_branch,omitempty"`
	Progress    *int                   `json:"progress,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
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

// IProjectService 项目管理接口
type IProjectService interface {
	CreateProject(ctx context.Context, req *ProjectCreateRequest) (*Project, error)
	GetProject(ctx context.Context, id string) (*Project, error)
	ListProjects(ctx context.Context, req *ProjectListRequest) (*ProjectListResponse, error)
	UpdateProject(ctx context.Context, id string, req *ProjectUpdateRequest) (*Project, error)
	DeleteProject(ctx context.Context, id string) error
	AddPlanToProject(ctx context.Context, projectID, planID string) error
	RemovePlanFromProject(ctx context.Context, projectID, planID string) error
	GetProjectPlans(ctx context.Context, projectID string) ([]planner.Plan, error)
	RecalculateProgress(ctx context.Context, projectID string) error
}

// InMemoryProjectService 内存实现的项目管理器
type InMemoryProjectService struct {
	mu       sync.RWMutex
	projects map[string]*Project
}

// NewInMemoryProjectService 创建内存项目管理器
func NewInMemoryProjectService() *InMemoryProjectService {
	return &InMemoryProjectService{
		projects: make(map[string]*Project),
	}
}

// validStatus 校验状态枚举
func validStatus(s ProjectStatus) bool {
	switch s {
	case StatusActive, StatusPlanning, StatusPaused, StatusCompleted, StatusArchived:
		return true
	}
	return false
}

// CreateProject 创建项目
func (s *InMemoryProjectService) CreateProject(ctx context.Context, req *ProjectCreateRequest) (*Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	status := req.Status
	if status == "" {
		status = StatusPlanning
	}
	if !validStatus(status) {
		return nil, errors.New("invalid status: " + string(status))
	}

	now := time.Now().Unix()
	p := &Project{
		ID:          uuid.New().String(),
		Title:       req.Title,
		Description: req.Description,
		Status:      status,
		Progress:    0,
		Tags:        req.Tags,
		GitBranch:   req.GitBranch,
		CreatedAt:   now,
		UpdatedAt:   now,
		LastActive:  now,
		PlanIDs:     []string{},
		Metadata:    req.Metadata,
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

	// 按最近活跃时间倒序
	sort.Slice(filtered, func(i, j int) bool {
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

	p.UpdatedAt = now
	p.LastActive = now

	return p, nil
}

// DeleteProject 删除项目（仅解除 Plan 关联，不级联删除 Plans）
func (s *InMemoryProjectService) DeleteProject(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.projects[id]; !ok {
		return errors.New("project not found")
	}

	delete(s.projects, id)
	return nil
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

// RecalculateProgress 根据关联 Plans 的完成度重算进度
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

// 全局服务实例
var defaultProjectService IProjectService
var projectOnce sync.Once

// GetProjectService 获取项目管理器实例
func GetProjectService() IProjectService {
	projectOnce.Do(func() {
		defaultProjectService = NewInMemoryProjectService()
	})
	return defaultProjectService
}

// SetProjectService 设置项目管理器实例（用于测试）
func SetProjectService(svc IProjectService) {
	defaultProjectService = svc
}
