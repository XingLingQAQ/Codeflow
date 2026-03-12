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
	_ "github.com/mattn/go-sqlite3"

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
	metadata_json TEXT
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
`

type projectStateSnapshot struct {
	projects map[string]*Project
}

func NewSQLiteProjectService(dbPath string) (*SQLiteProjectService, error) {
	connStr, err := buildProjectSQLiteConnString(dbPath)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite3", connStr)
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

func buildProjectSQLiteConnString(dbPath string) (string, error) {
	if dbPath == "" || dbPath == ":memory:" {
		return "file::memory:?cache=shared&_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000", nil
	}

	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("create project db dir: %w", err)
		}
	}

	return dbPath + "?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000", nil
}

func (s *SQLiteProjectService) initialize() error {
	s.dbMu.Lock()
	defer s.dbMu.Unlock()

	if _, err := s.db.Exec(createProjectTablesSQL); err != nil {
		return fmt.Errorf("create project tables: %w", err)
	}

	return s.loadFromDBLocked()
}

func (s *SQLiteProjectService) loadFromDBLocked() error {
	projects := make(map[string]*Project)

	rows, err := s.db.Query(`
		SELECT id, title, description, status, progress, tags_json, git_branch, created_at, updated_at, last_active, metadata_json
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
			INSERT INTO projects (id, title, description, status, progress, tags_json, git_branch, created_at, updated_at, last_active, metadata_json)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, project.ID, project.Title, project.Description, string(project.Status), project.Progress, tagsJSON, nullProjectString(project.GitBranch), project.CreatedAt, project.UpdatedAt, project.LastActive, nullProjectString(metadataJSON)); err != nil {
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
	cloned.Metadata = cloneProjectMetadata(project.Metadata)
	return &cloned
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

// SetProjectService 设置项目管理器实例（用于测试）
func SetProjectService(svc IProjectService) {
	defaultProjectService = svc
}
