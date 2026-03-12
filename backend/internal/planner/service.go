// Package planner - Planner service for task management and dependency tracking
package planner

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
)

// TaskStatus 任务状态
type TaskStatus string

const (
	TaskStatusPending    TaskStatus = "pending"
	TaskStatusInProgress TaskStatus = "in_progress"
	TaskStatusCompleted  TaskStatus = "completed"
	TaskStatusBlocked    TaskStatus = "blocked"
	TaskStatusCancelled  TaskStatus = "cancelled"
)

// TaskPriority 任务优先级
type TaskPriority string

const (
	PriorityP0 TaskPriority = "P0" // Critical
	PriorityP1 TaskPriority = "P1" // High
	PriorityP2 TaskPriority = "P2" // Medium
	PriorityP3 TaskPriority = "P3" // Low
)

// Plan 计划
type Plan struct {
	ID             string                 `json:"id"`
	Title          string                 `json:"title"`
	Description    string                 `json:"description,omitempty"`
	Status         string                 `json:"status"` // draft, active, completed, archived
	TaskCount      int                    `json:"task_count"`
	CompletedCount int                    `json:"completed_count"`
	CreatedAt      int64                  `json:"created_at"`
	UpdatedAt      int64                  `json:"updated_at"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
}

// Task 任务
type Task struct {
	ID           string                 `json:"id"`
	PlanID       string                 `json:"plan_id"`
	Title        string                 `json:"title"`
	Description  string                 `json:"description,omitempty"`
	Status       TaskStatus             `json:"status"`
	Priority     TaskPriority           `json:"priority"`
	Model        string                 `json:"model,omitempty"` // 分配的模型
	Order        int                    `json:"order"`
	Dependencies []string               `json:"dependencies,omitempty"` // 依赖的任务ID
	Assignee     string                 `json:"assignee,omitempty"`
	EstimatedMs  int64                  `json:"estimated_ms,omitempty"`
	ActualMs     int64                  `json:"actual_ms,omitempty"`
	StartedAt    int64                  `json:"started_at,omitempty"`
	CompletedAt  int64                  `json:"completed_at,omitempty"`
	CreatedAt    int64                  `json:"created_at"`
	UpdatedAt    int64                  `json:"updated_at"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// PlanCreateRequest 创建计划请求
type PlanCreateRequest struct {
	Title       string                 `json:"title" binding:"required"`
	Description string                 `json:"description,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// PlanListRequest 计划列表请求
type PlanListRequest struct {
	Status string `form:"status"`
	Limit  int    `form:"limit"`
	Offset int    `form:"offset"`
}

// PlanListResponse 计划列表响应
type PlanListResponse struct {
	Plans   []Plan `json:"plans"`
	Total   int    `json:"total"`
	HasMore bool   `json:"has_more"`
}

// TaskCreateRequest 创建任务请求
type TaskCreateRequest struct {
	Title        string                 `json:"title" binding:"required"`
	Description  string                 `json:"description,omitempty"`
	Priority     TaskPriority           `json:"priority,omitempty"`
	Model        string                 `json:"model,omitempty"`
	Dependencies []string               `json:"dependencies,omitempty"`
	Assignee     string                 `json:"assignee,omitempty"`
	EstimatedMs  int64                  `json:"estimated_ms,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// TaskUpdateRequest 更新任务请求
type TaskUpdateRequest struct {
	Title        string                 `json:"title,omitempty"`
	Description  string                 `json:"description,omitempty"`
	Status       TaskStatus             `json:"status,omitempty"`
	Priority     TaskPriority           `json:"priority,omitempty"`
	Model        string                 `json:"model,omitempty"`
	Dependencies []string               `json:"dependencies,omitempty"`
	Assignee     string                 `json:"assignee,omitempty"`
	EstimatedMs  int64                  `json:"estimated_ms,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// TaskReorderRequest 重排序请求
type TaskReorderRequest struct {
	NewOrder int `json:"new_order" binding:"required"`
}

// BatchModelRequest 批量模型切换请求
type BatchModelRequest struct {
	TaskIDs []string `json:"task_ids" binding:"required"`
	Model   string   `json:"model" binding:"required"`
}

// BatchModelResponse 批量模型切换响应
type BatchModelResponse struct {
	Updated int      `json:"updated"`
	Failed  []string `json:"failed,omitempty"`
}

// TaskListRequest 任务列表请求
type TaskListRequest struct {
	Status    string `form:"status"`
	Priority  string `form:"priority"`
	Model     string `form:"model"`
	SortBy    string `form:"sort_by"`    // order, priority, created_at
	SortOrder string `form:"sort_order"` // asc, desc
}

// TaskListResponse 任务列表响应
type TaskListResponse struct {
	Tasks []Task `json:"tasks"`
	Total int    `json:"total"`
}

// IPlanner 计划管理接口
type IPlanner interface {
	// Plan CRUD
	CreatePlan(ctx context.Context, req *PlanCreateRequest) (*Plan, error)
	GetPlan(ctx context.Context, id string) (*Plan, error)
	ListPlans(ctx context.Context, req *PlanListRequest) (*PlanListResponse, error)
	DeletePlan(ctx context.Context, id string) error

	// Task CRUD
	CreateTask(ctx context.Context, planID string, req *TaskCreateRequest) (*Task, error)
	GetTask(ctx context.Context, planID, taskID string) (*Task, error)
	ListTasks(ctx context.Context, planID string, req *TaskListRequest) (*TaskListResponse, error)
	UpdateTask(ctx context.Context, planID, taskID string, req *TaskUpdateRequest) (*Task, error)
	DeleteTask(ctx context.Context, planID, taskID string) error

	// Task operations
	ReorderTask(ctx context.Context, planID, taskID string, req *TaskReorderRequest) (*Task, error)
	BatchUpdateModel(ctx context.Context, planID string, req *BatchModelRequest) (*BatchModelResponse, error)

	// Dependency validation
	ValidateDependencies(planID string, taskID string, dependencies []string) error
	GetBlockedTasks(ctx context.Context, planID string) ([]Task, error)
}

// InMemoryPlanner 内存实现的计划管理器
type InMemoryPlanner struct {
	mu    sync.RWMutex
	plans map[string]*Plan
	tasks map[string]map[string]*Task // planID -> taskID -> task
}

// NewInMemoryPlanner 创建内存计划管理器
func NewInMemoryPlanner() *InMemoryPlanner {
	return &InMemoryPlanner{
		plans: make(map[string]*Plan),
		tasks: make(map[string]map[string]*Task),
	}
}

// CreatePlan 创建计划
func (p *InMemoryPlanner) CreatePlan(ctx context.Context, req *PlanCreateRequest) (*Plan, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now().Unix()
	plan := &Plan{
		ID:             uuid.New().String(),
		Title:          req.Title,
		Description:    req.Description,
		Status:         "draft",
		TaskCount:      0,
		CompletedCount: 0,
		CreatedAt:      now,
		UpdatedAt:      now,
		Metadata:       req.Metadata,
	}

	p.plans[plan.ID] = plan
	p.tasks[plan.ID] = make(map[string]*Task)

	return plan, nil
}

// GetPlan 获取计划
func (p *InMemoryPlanner) GetPlan(ctx context.Context, id string) (*Plan, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	plan, ok := p.plans[id]
	if !ok {
		return nil, nil
	}
	return plan, nil
}

// ListPlans 列出计划
func (p *InMemoryPlanner) ListPlans(ctx context.Context, req *PlanListRequest) (*PlanListResponse, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var filtered []*Plan
	for _, plan := range p.plans {
		if req.Status != "" && plan.Status != req.Status {
			continue
		}
		filtered = append(filtered, plan)
	}

	// 按更新时间倒序，时间相同按 ID 倒序，保证稳定顺序
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].UpdatedAt == filtered[j].UpdatedAt {
			return filtered[i].ID > filtered[j].ID
		}
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

	result := make([]Plan, end-start)
	for i := start; i < end; i++ {
		result[i-start] = *filtered[i]
	}

	return &PlanListResponse{
		Plans:   result,
		Total:   total,
		HasMore: end < total,
	}, nil
}

// DeletePlan 删除计划
func (p *InMemoryPlanner) DeletePlan(ctx context.Context, id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, ok := p.plans[id]; !ok {
		return errors.New("plan not found")
	}

	delete(p.plans, id)
	delete(p.tasks, id)
	return nil
}

// CreateTask 创建任务
func (p *InMemoryPlanner) CreateTask(ctx context.Context, planID string, req *TaskCreateRequest) (*Task, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	plan, ok := p.plans[planID]
	if !ok {
		return nil, errors.New("plan not found")
	}

	planTasks := p.tasks[planID]

	// 验证依赖
	if len(req.Dependencies) > 0 {
		for _, depID := range req.Dependencies {
			if _, ok := planTasks[depID]; !ok {
				return nil, errors.New("dependency task not found: " + depID)
			}
		}
	}

	priority := req.Priority
	if priority == "" {
		priority = PriorityP2
	}

	now := time.Now().Unix()
	task := &Task{
		ID:           uuid.New().String(),
		PlanID:       planID,
		Title:        req.Title,
		Description:  req.Description,
		Status:       TaskStatusPending,
		Priority:     priority,
		Model:        req.Model,
		Order:        len(planTasks) + 1,
		Dependencies: req.Dependencies,
		Assignee:     req.Assignee,
		EstimatedMs:  req.EstimatedMs,
		CreatedAt:    now,
		UpdatedAt:    now,
		Metadata:     req.Metadata,
	}

	planTasks[task.ID] = task
	plan.TaskCount++
	plan.UpdatedAt = now

	// 检查是否被阻塞
	if len(task.Dependencies) > 0 {
		for _, depID := range task.Dependencies {
			if dep, ok := planTasks[depID]; ok {
				if dep.Status != TaskStatusCompleted {
					task.Status = TaskStatusBlocked
					break
				}
			}
		}
	}

	return task, nil
}

// GetTask 获取任务
func (p *InMemoryPlanner) GetTask(ctx context.Context, planID, taskID string) (*Task, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	planTasks, ok := p.tasks[planID]
	if !ok {
		return nil, errors.New("plan not found")
	}

	task, ok := planTasks[taskID]
	if !ok {
		return nil, nil
	}
	return task, nil
}

// ListTasks 列出任务
func (p *InMemoryPlanner) ListTasks(ctx context.Context, planID string, req *TaskListRequest) (*TaskListResponse, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	planTasks, ok := p.tasks[planID]
	if !ok {
		return nil, errors.New("plan not found")
	}

	var filtered []*Task
	for _, task := range planTasks {
		if req.Status != "" && string(task.Status) != req.Status {
			continue
		}
		if req.Priority != "" && string(task.Priority) != req.Priority {
			continue
		}
		if req.Model != "" && task.Model != req.Model {
			continue
		}
		filtered = append(filtered, task)
	}

	// 排序
	sortBy := req.SortBy
	if sortBy == "" {
		sortBy = "order"
	}
	sortDesc := req.SortOrder == "desc"

	sort.Slice(filtered, func(i, j int) bool {
		var less bool
		switch sortBy {
		case "priority":
			less = filtered[i].Priority < filtered[j].Priority
		case "created_at":
			less = filtered[i].CreatedAt < filtered[j].CreatedAt
		default: // order
			less = filtered[i].Order < filtered[j].Order
		}
		if sortDesc {
			return !less
		}
		return less
	})

	result := make([]Task, len(filtered))
	for i, t := range filtered {
		result[i] = *t
	}

	return &TaskListResponse{
		Tasks: result,
		Total: len(result),
	}, nil
}

// UpdateTask 更新任务
func (p *InMemoryPlanner) UpdateTask(ctx context.Context, planID, taskID string, req *TaskUpdateRequest) (*Task, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	plan, ok := p.plans[planID]
	if !ok {
		return nil, errors.New("plan not found")
	}

	planTasks := p.tasks[planID]
	task, ok := planTasks[taskID]
	if !ok {
		return nil, errors.New("task not found")
	}

	now := time.Now().Unix()
	wasCompleted := task.Status == TaskStatusCompleted

	if req.Title != "" {
		task.Title = req.Title
	}
	if req.Description != "" {
		task.Description = req.Description
	}
	if req.Status != "" {
		oldStatus := task.Status
		task.Status = req.Status

		// 状态变更处理
		if req.Status == TaskStatusInProgress && oldStatus != TaskStatusInProgress {
			task.StartedAt = now
		}
		if req.Status == TaskStatusCompleted && oldStatus != TaskStatusCompleted {
			task.CompletedAt = now
			if task.StartedAt > 0 {
				task.ActualMs = (now - task.StartedAt) * 1000
			}
			plan.CompletedCount++

			// 解除依赖此任务的其他任务的阻塞
			p.unblockDependentTasks(planID, taskID)
		}
		if oldStatus == TaskStatusCompleted && req.Status != TaskStatusCompleted {
			plan.CompletedCount--
		}
	}
	if req.Priority != "" {
		task.Priority = req.Priority
	}
	if req.Model != "" {
		task.Model = req.Model
	}
	if req.Dependencies != nil {
		// 验证依赖
		for _, depID := range req.Dependencies {
			if depID == taskID {
				return nil, errors.New("task cannot depend on itself")
			}
			if _, ok := planTasks[depID]; !ok {
				return nil, errors.New("dependency task not found: " + depID)
			}
		}
		task.Dependencies = req.Dependencies
	}
	if req.Assignee != "" {
		task.Assignee = req.Assignee
	}
	if req.EstimatedMs > 0 {
		task.EstimatedMs = req.EstimatedMs
	}
	if req.Metadata != nil {
		task.Metadata = req.Metadata
	}

	task.UpdatedAt = now
	plan.UpdatedAt = now

	// 更新计划状态
	if !wasCompleted && task.Status == TaskStatusCompleted && plan.CompletedCount == plan.TaskCount {
		plan.Status = "completed"
	}

	return task, nil
}

// unblockDependentTasks 解除依赖任务的阻塞
func (p *InMemoryPlanner) unblockDependentTasks(planID, completedTaskID string) {
	planTasks := p.tasks[planID]
	for _, task := range planTasks {
		if task.Status != TaskStatusBlocked {
			continue
		}
		// 检查是否所有依赖都已完成
		allDepsCompleted := true
		for _, depID := range task.Dependencies {
			if dep, ok := planTasks[depID]; ok {
				if dep.Status != TaskStatusCompleted {
					allDepsCompleted = false
					break
				}
			}
		}
		if allDepsCompleted {
			task.Status = TaskStatusPending
			task.UpdatedAt = time.Now().Unix()
		}
	}
}

// DeleteTask 删除任务
func (p *InMemoryPlanner) DeleteTask(ctx context.Context, planID, taskID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	plan, ok := p.plans[planID]
	if !ok {
		return errors.New("plan not found")
	}

	planTasks := p.tasks[planID]
	task, ok := planTasks[taskID]
	if !ok {
		return errors.New("task not found")
	}

	// 检查是否有其他任务依赖此任务
	for _, t := range planTasks {
		for _, depID := range t.Dependencies {
			if depID == taskID {
				return errors.New("cannot delete task: other tasks depend on it")
			}
		}
	}

	if task.Status == TaskStatusCompleted {
		plan.CompletedCount--
	}
	plan.TaskCount--
	plan.UpdatedAt = time.Now().Unix()

	delete(planTasks, taskID)
	return nil
}

// ReorderTask 重排序任务
func (p *InMemoryPlanner) ReorderTask(ctx context.Context, planID, taskID string, req *TaskReorderRequest) (*Task, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	plan, ok := p.plans[planID]
	if !ok {
		return nil, errors.New("plan not found")
	}

	planTasks := p.tasks[planID]
	task, ok := planTasks[taskID]
	if !ok {
		return nil, errors.New("task not found")
	}

	oldOrder := task.Order
	newOrder := req.NewOrder

	if newOrder < 1 {
		newOrder = 1
	}
	if newOrder > len(planTasks) {
		newOrder = len(planTasks)
	}

	if oldOrder == newOrder {
		return task, nil
	}

	// 调整其他任务的顺序
	for _, t := range planTasks {
		if t.ID == taskID {
			continue
		}
		if oldOrder < newOrder {
			// 向后移动：oldOrder+1 到 newOrder 的任务向前移一位
			if t.Order > oldOrder && t.Order <= newOrder {
				t.Order--
			}
		} else {
			// 向前移动：newOrder 到 oldOrder-1 的任务向后移一位
			if t.Order >= newOrder && t.Order < oldOrder {
				t.Order++
			}
		}
	}

	task.Order = newOrder
	task.UpdatedAt = time.Now().Unix()
	plan.UpdatedAt = time.Now().Unix()

	return task, nil
}

// BatchUpdateModel 批量更新模型
func (p *InMemoryPlanner) BatchUpdateModel(ctx context.Context, planID string, req *BatchModelRequest) (*BatchModelResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	plan, ok := p.plans[planID]
	if !ok {
		return nil, errors.New("plan not found")
	}

	planTasks := p.tasks[planID]
	now := time.Now().Unix()

	updated := 0
	var failed []string

	for _, taskID := range req.TaskIDs {
		task, ok := planTasks[taskID]
		if !ok {
			failed = append(failed, taskID)
			continue
		}
		task.Model = req.Model
		task.UpdatedAt = now
		updated++
	}

	plan.UpdatedAt = now

	return &BatchModelResponse{
		Updated: updated,
		Failed:  failed,
	}, nil
}

// ValidateDependencies 验证依赖关系
func (p *InMemoryPlanner) ValidateDependencies(planID string, taskID string, dependencies []string) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	planTasks, ok := p.tasks[planID]
	if !ok {
		return errors.New("plan not found")
	}

	// 检查循环依赖
	visited := make(map[string]bool)
	var checkCycle func(id string) bool
	checkCycle = func(id string) bool {
		if visited[id] {
			return true // 发现循环
		}
		visited[id] = true

		task, ok := planTasks[id]
		if !ok {
			return false
		}

		for _, depID := range task.Dependencies {
			if checkCycle(depID) {
				return true
			}
		}

		visited[id] = false
		return false
	}

	// 临时添加新依赖进行检查
	if task, ok := planTasks[taskID]; ok {
		originalDeps := task.Dependencies
		task.Dependencies = dependencies
		hasCycle := checkCycle(taskID)
		task.Dependencies = originalDeps
		if hasCycle {
			return errors.New("circular dependency detected")
		}
	}

	return nil
}

// GetBlockedTasks 获取被阻塞的任务
func (p *InMemoryPlanner) GetBlockedTasks(ctx context.Context, planID string) ([]Task, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	planTasks, ok := p.tasks[planID]
	if !ok {
		return nil, errors.New("plan not found")
	}

	var blocked []Task
	for _, task := range planTasks {
		if task.Status == TaskStatusBlocked {
			blocked = append(blocked, *task)
		}
	}

	return blocked, nil
}

type SQLitePlanner struct {
	*InMemoryPlanner
	db      *sql.DB
	dbMu    sync.RWMutex
	writeMu sync.Mutex
}

const createPlannerTablesSQL = `
CREATE TABLE IF NOT EXISTS plans (
	id TEXT PRIMARY KEY,
	title TEXT NOT NULL,
	description TEXT,
	status TEXT NOT NULL,
	task_count INTEGER NOT NULL,
	completed_count INTEGER NOT NULL,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL,
	metadata_json TEXT
);

CREATE TABLE IF NOT EXISTS tasks (
	id TEXT PRIMARY KEY,
	plan_id TEXT NOT NULL,
	title TEXT NOT NULL,
	description TEXT,
	status TEXT NOT NULL,
	priority TEXT NOT NULL,
	model TEXT,
	task_order INTEGER NOT NULL,
	dependencies_json TEXT NOT NULL DEFAULT '[]',
	assignee TEXT,
	estimated_ms INTEGER NOT NULL DEFAULT 0,
	actual_ms INTEGER NOT NULL DEFAULT 0,
	started_at INTEGER NOT NULL DEFAULT 0,
	completed_at INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL,
	metadata_json TEXT,
	FOREIGN KEY (plan_id) REFERENCES plans(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_plans_updated_at ON plans(updated_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_tasks_plan_order ON tasks(plan_id, task_order ASC, id ASC);
CREATE INDEX IF NOT EXISTS idx_tasks_plan_status ON tasks(plan_id, status, id);
`

type plannerStateSnapshot struct {
	plans map[string]*Plan
	tasks map[string]map[string]*Task
}

func NewSQLitePlanner(dbPath string) (*SQLitePlanner, error) {
	connStr, err := buildPlannerSQLiteConnString(dbPath)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite3", connStr)
	if err != nil {
		return nil, fmt.Errorf("open planner database: %w", err)
	}

	svc := &SQLitePlanner{
		InMemoryPlanner: NewInMemoryPlanner(),
		db:              db,
	}

	if err := svc.initialize(); err != nil {
		db.Close()
		return nil, err
	}

	return svc, nil
}

func buildPlannerSQLiteConnString(dbPath string) (string, error) {
	if dbPath == "" || dbPath == ":memory:" {
		return "file::memory:?cache=shared&_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000", nil
	}

	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("create planner db dir: %w", err)
		}
	}

	return dbPath + "?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000", nil
}

func (p *SQLitePlanner) initialize() error {
	p.dbMu.Lock()
	defer p.dbMu.Unlock()

	if _, err := p.db.Exec(createPlannerTablesSQL); err != nil {
		return fmt.Errorf("create planner tables: %w", err)
	}

	return p.loadFromDBLocked()
}

func (p *SQLitePlanner) loadFromDBLocked() error {
	plans := make(map[string]*Plan)
	tasks := make(map[string]map[string]*Task)

	planRows, err := p.db.Query(`
		SELECT id, title, description, status, task_count, completed_count, created_at, updated_at, metadata_json
		FROM plans
	`)
	if err != nil {
		return fmt.Errorf("query plans: %w", err)
	}
	defer planRows.Close()

	for planRows.Next() {
		plan, err := scanPlannerPlan(planRows)
		if err != nil {
			return err
		}
		plans[plan.ID] = plan
		tasks[plan.ID] = make(map[string]*Task)
	}
	if err := planRows.Err(); err != nil {
		return fmt.Errorf("iterate plans: %w", err)
	}

	taskRows, err := p.db.Query(`
		SELECT id, plan_id, title, description, status, priority, model, task_order, dependencies_json,
		       assignee, estimated_ms, actual_ms, started_at, completed_at, created_at, updated_at, metadata_json
		FROM tasks
		ORDER BY plan_id ASC, task_order ASC, id ASC
	`)
	if err != nil {
		return fmt.Errorf("query tasks: %w", err)
	}
	defer taskRows.Close()

	for taskRows.Next() {
		task, err := scanPlannerTask(taskRows)
		if err != nil {
			return err
		}
		if _, ok := tasks[task.PlanID]; !ok {
			tasks[task.PlanID] = make(map[string]*Task)
		}
		tasks[task.PlanID][task.ID] = task
	}
	if err := taskRows.Err(); err != nil {
		return fmt.Errorf("iterate tasks: %w", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	p.plans = plans
	p.tasks = tasks
	return nil
}

func (p *SQLitePlanner) Close() error {
	if p.db != nil {
		return p.db.Close()
	}
	return nil
}

func (p *SQLitePlanner) CreatePlan(ctx context.Context, req *PlanCreateRequest) (*Plan, error) {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()

	before := p.snapshotState()
	plan, err := p.InMemoryPlanner.CreatePlan(ctx, req)
	if err != nil {
		return nil, err
	}
	if err := p.persistCurrentState(); err != nil {
		p.restoreState(before)
		return nil, err
	}
	return plan, nil
}

func (p *SQLitePlanner) DeletePlan(ctx context.Context, id string) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()

	before := p.snapshotState()
	if err := p.InMemoryPlanner.DeletePlan(ctx, id); err != nil {
		return err
	}
	if err := p.persistCurrentState(); err != nil {
		p.restoreState(before)
		return err
	}
	return nil
}

func (p *SQLitePlanner) CreateTask(ctx context.Context, planID string, req *TaskCreateRequest) (*Task, error) {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()

	before := p.snapshotState()
	task, err := p.InMemoryPlanner.CreateTask(ctx, planID, req)
	if err != nil {
		return nil, err
	}
	if err := p.persistCurrentState(); err != nil {
		p.restoreState(before)
		return nil, err
	}
	return task, nil
}

func (p *SQLitePlanner) UpdateTask(ctx context.Context, planID, taskID string, req *TaskUpdateRequest) (*Task, error) {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()

	before := p.snapshotState()
	task, err := p.InMemoryPlanner.UpdateTask(ctx, planID, taskID, req)
	if err != nil {
		return nil, err
	}
	if err := p.persistCurrentState(); err != nil {
		p.restoreState(before)
		return nil, err
	}
	return task, nil
}

func (p *SQLitePlanner) DeleteTask(ctx context.Context, planID, taskID string) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()

	before := p.snapshotState()
	if err := p.InMemoryPlanner.DeleteTask(ctx, planID, taskID); err != nil {
		return err
	}
	if err := p.persistCurrentState(); err != nil {
		p.restoreState(before)
		return err
	}
	return nil
}

func (p *SQLitePlanner) ReorderTask(ctx context.Context, planID, taskID string, req *TaskReorderRequest) (*Task, error) {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()

	before := p.snapshotState()
	task, err := p.InMemoryPlanner.ReorderTask(ctx, planID, taskID, req)
	if err != nil {
		return nil, err
	}
	if err := p.persistCurrentState(); err != nil {
		p.restoreState(before)
		return nil, err
	}
	return task, nil
}

func (p *SQLitePlanner) BatchUpdateModel(ctx context.Context, planID string, req *BatchModelRequest) (*BatchModelResponse, error) {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()

	before := p.snapshotState()
	resp, err := p.InMemoryPlanner.BatchUpdateModel(ctx, planID, req)
	if err != nil {
		return nil, err
	}
	if err := p.persistCurrentState(); err != nil {
		p.restoreState(before)
		return nil, err
	}
	return resp, nil
}

func (p *SQLitePlanner) persistCurrentState() error {
	snapshot := p.snapshotState()
	return p.saveSnapshot(snapshot)
}

func (p *SQLitePlanner) saveSnapshot(snapshot plannerStateSnapshot) error {
	p.dbMu.Lock()
	defer p.dbMu.Unlock()

	tx, err := p.db.Begin()
	if err != nil {
		return fmt.Errorf("begin planner transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM tasks`); err != nil {
		return fmt.Errorf("clear planner tasks: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM plans`); err != nil {
		return fmt.Errorf("clear planner plans: %w", err)
	}

	for _, plan := range snapshot.plans {
		metadataJSON, err := marshalPlannerMetadata(plan.Metadata)
		if err != nil {
			return fmt.Errorf("marshal plan metadata: %w", err)
		}
		if _, err := tx.Exec(`
			INSERT INTO plans (id, title, description, status, task_count, completed_count, created_at, updated_at, metadata_json)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, plan.ID, plan.Title, plan.Description, plan.Status, plan.TaskCount, plan.CompletedCount, plan.CreatedAt, plan.UpdatedAt, nullPlannerString(metadataJSON)); err != nil {
			return fmt.Errorf("insert plan %s: %w", plan.ID, err)
		}
	}

	for _, planTasks := range snapshot.tasks {
		for _, task := range planTasks {
			dependenciesJSON, err := marshalPlannerStrings(task.Dependencies)
			if err != nil {
				return fmt.Errorf("marshal task dependencies: %w", err)
			}
			metadataJSON, err := marshalPlannerMetadata(task.Metadata)
			if err != nil {
				return fmt.Errorf("marshal task metadata: %w", err)
			}
			if _, err := tx.Exec(`
				INSERT INTO tasks (
					id, plan_id, title, description, status, priority, model, task_order, dependencies_json,
					assignee, estimated_ms, actual_ms, started_at, completed_at, created_at, updated_at, metadata_json
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			`, task.ID, task.PlanID, task.Title, task.Description, string(task.Status), string(task.Priority), nullPlannerString(task.Model), task.Order, dependenciesJSON,
				nullPlannerString(task.Assignee), task.EstimatedMs, task.ActualMs, task.StartedAt, task.CompletedAt, task.CreatedAt, task.UpdatedAt, nullPlannerString(metadataJSON)); err != nil {
				return fmt.Errorf("insert task %s: %w", task.ID, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit planner transaction: %w", err)
	}
	return nil
}

func (p *SQLitePlanner) snapshotState() plannerStateSnapshot {
	p.mu.RLock()
	defer p.mu.RUnlock()

	snapshot := plannerStateSnapshot{
		plans: make(map[string]*Plan, len(p.plans)),
		tasks: make(map[string]map[string]*Task, len(p.tasks)),
	}

	for id, plan := range p.plans {
		snapshot.plans[id] = clonePlannerPlan(plan)
	}
	for planID, planTasks := range p.tasks {
		clonedTasks := make(map[string]*Task, len(planTasks))
		for taskID, task := range planTasks {
			clonedTasks[taskID] = clonePlannerTask(task)
		}
		snapshot.tasks[planID] = clonedTasks
	}

	return snapshot
}

func (p *SQLitePlanner) restoreState(snapshot plannerStateSnapshot) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.plans = snapshot.plans
	p.tasks = snapshot.tasks
}

func scanPlannerPlan(scanner interface{ Scan(dest ...any) error }) (*Plan, error) {
	var plan Plan
	var description sql.NullString
	var metadataJSON sql.NullString

	if err := scanner.Scan(
		&plan.ID,
		&plan.Title,
		&description,
		&plan.Status,
		&plan.TaskCount,
		&plan.CompletedCount,
		&plan.CreatedAt,
		&plan.UpdatedAt,
		&metadataJSON,
	); err != nil {
		return nil, fmt.Errorf("scan plan: %w", err)
	}

	plan.Description = description.String
	metadata, err := unmarshalPlannerMetadata(metadataJSON.String)
	if err != nil {
		return nil, fmt.Errorf("decode plan metadata: %w", err)
	}
	plan.Metadata = metadata
	return &plan, nil
}

func scanPlannerTask(scanner interface{ Scan(dest ...any) error }) (*Task, error) {
	var task Task
	var description sql.NullString
	var model sql.NullString
	var dependenciesJSON string
	var assignee sql.NullString
	var metadataJSON sql.NullString
	var status string
	var priority string

	if err := scanner.Scan(
		&task.ID,
		&task.PlanID,
		&task.Title,
		&description,
		&status,
		&priority,
		&model,
		&task.Order,
		&dependenciesJSON,
		&assignee,
		&task.EstimatedMs,
		&task.ActualMs,
		&task.StartedAt,
		&task.CompletedAt,
		&task.CreatedAt,
		&task.UpdatedAt,
		&metadataJSON,
	); err != nil {
		return nil, fmt.Errorf("scan task: %w", err)
	}

	task.Description = description.String
	task.Status = TaskStatus(status)
	task.Priority = TaskPriority(priority)
	task.Model = model.String
	task.Assignee = assignee.String

	dependencies, err := unmarshalPlannerStrings(dependenciesJSON)
	if err != nil {
		return nil, fmt.Errorf("decode task dependencies: %w", err)
	}
	metadata, err := unmarshalPlannerMetadata(metadataJSON.String)
	if err != nil {
		return nil, fmt.Errorf("decode task metadata: %w", err)
	}
	task.Dependencies = dependencies
	task.Metadata = metadata
	return &task, nil
}

func clonePlannerPlan(plan *Plan) *Plan {
	if plan == nil {
		return nil
	}
	cloned := *plan
	cloned.Metadata = clonePlannerMetadata(plan.Metadata)
	return &cloned
}

func clonePlannerTask(task *Task) *Task {
	if task == nil {
		return nil
	}
	cloned := *task
	cloned.Dependencies = clonePlannerStrings(task.Dependencies)
	cloned.Metadata = clonePlannerMetadata(task.Metadata)
	return &cloned
}

func clonePlannerStrings(values []string) []string {
	if values == nil {
		return nil
	}
	return append([]string(nil), values...)
}

func marshalPlannerStrings(values []string) (string, error) {
	if len(values) == 0 {
		return "[]", nil
	}
	data, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func unmarshalPlannerStrings(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, err
	}
	return values, nil
}

func clonePlannerMetadata(metadata map[string]interface{}) map[string]interface{} {
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

func marshalPlannerMetadata(metadata map[string]interface{}) (string, error) {
	if len(metadata) == 0 {
		return "", nil
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func unmarshalPlannerMetadata(raw string) (map[string]interface{}, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var metadata map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
		return nil, err
	}
	return metadata, nil
}

func nullPlannerString(value string) interface{} {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

// 全局服务实例
var defaultPlanner IPlanner

// GetPlanner 获取计划管理器实例
func GetPlanner() IPlanner {
	if defaultPlanner == nil {
		defaultPlanner = NewInMemoryPlanner()
	}
	return defaultPlanner
}

// SetPlanner 设置计划管理器实例
func SetPlanner(planner IPlanner) {
	defaultPlanner = planner
}
