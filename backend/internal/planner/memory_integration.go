// Package planner - Memory integration for Plan mode task lifecycle
package planner

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// TaskHookEvent 任务级 Hook 事件类型
type TaskHookEvent string

const (
	HookBeforeTaskExecute TaskHookEvent = "before_task_execute"
	HookAfterTaskExecute  TaskHookEvent = "after_task_execute"
	HookOnTaskFailure     TaskHookEvent = "on_task_failure"
	HookOnTaskComplete    TaskHookEvent = "on_task_complete"
)

// TaskExecutionContext 任务执行上下文
type TaskExecutionContext struct {
	TaskID      string                 `json:"task_id"`
	PlanID      string                 `json:"plan_id"`
	Title       string                 `json:"title"`
	Description string                 `json:"description"`
	Files       []string               `json:"files,omitempty"`
	SessionID   string                 `json:"session_id"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// TaskExecutionResult 任务执行结果
type TaskExecutionResult struct {
	TaskID        string                 `json:"task_id"`
	PlanID        string                 `json:"plan_id"`
	Title         string                 `json:"title"`
	Status        string                 `json:"status"` // completed | failed
	FilesModified []string               `json:"files_modified,omitempty"`
	Output        string                 `json:"output,omitempty"`
	Error         string                 `json:"error,omitempty"`
	DurationMs    int64                  `json:"duration_ms,omitempty"`
	SessionID     string                 `json:"session_id"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// TaskFailureContext 任务失败上下文
type TaskFailureContext struct {
	TaskID        string                 `json:"task_id"`
	PlanID        string                 `json:"plan_id"`
	Title         string                 `json:"title"`
	Error         string                 `json:"error"`
	Phase         string                 `json:"phase,omitempty"`
	FilesModified []string               `json:"files_modified,omitempty"`
	SessionID     string                 `json:"session_id"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// TaskMemoryRecord 任务记忆记录（用于存储到原子记忆系统）
type TaskMemoryRecord struct {
	ID         string   `json:"id"`
	Timestamp  int64    `json:"timestamp"`
	Content    string   `json:"content"`
	Tags       []string `json:"tags"`
	SessionID  string   `json:"session_id"`
	Source     string   `json:"source"`
	Importance float64  `json:"importance"`
}

// TaskHookHandler 任务级 Hook 处理函数
type TaskHookHandler func(ctx context.Context, data interface{}) error

// MemoryIntegration Plan 模式记忆集成
type MemoryIntegration struct {
	handlers map[TaskHookEvent][]TaskHookHandler
}

// NewMemoryIntegration 创建记忆集成实例
func NewMemoryIntegration() *MemoryIntegration {
	return &MemoryIntegration{
		handlers: map[TaskHookEvent][]TaskHookHandler{
			HookBeforeTaskExecute: {},
			HookAfterTaskExecute:  {},
			HookOnTaskFailure:     {},
			HookOnTaskComplete:    {},
		},
	}
}

// Register 注册 Hook 处理器
func (mi *MemoryIntegration) Register(event TaskHookEvent, handler TaskHookHandler) {
	mi.handlers[event] = append(mi.handlers[event], handler)
}

// Emit 触发 Hook 事件
func (mi *MemoryIntegration) Emit(ctx context.Context, event TaskHookEvent, data interface{}) error {
	handlers, ok := mi.handlers[event]
	if !ok {
		return nil
	}

	for _, handler := range handlers {
		if err := handler(ctx, data); err != nil {
			// Hook 处理失败不阻塞主流程，仅记录
			continue
		}
	}

	return nil
}

// BuildTaskMemory 从任务执行结果构建记忆记录
func BuildTaskMemory(result *TaskExecutionResult) *TaskMemoryRecord {
	parts := []string{
		fmt.Sprintf("任务%s: %s", statusLabel(result.Status), result.Title),
	}

	if len(result.FilesModified) > 0 {
		parts = append(parts, fmt.Sprintf("修改文件: %s", strings.Join(result.FilesModified, ", ")))
	}

	if result.DurationMs > 0 {
		parts = append(parts, fmt.Sprintf("耗时: %ds", result.DurationMs/1000))
	}

	if result.Error != "" {
		parts = append(parts, fmt.Sprintf("错误: %s", result.Error))
	}

	importance := 0.7
	if result.Status != "completed" {
		importance = 0.5
	}

	tags := []string{"task_execution", result.PlanID}
	if result.Status == "completed" {
		tags = append(tags, "task_complete")
	} else {
		tags = append(tags, "task_failed")
	}

	return &TaskMemoryRecord{
		ID:         fmt.Sprintf("task_%s_%d", result.TaskID, time.Now().UnixMilli()),
		Timestamp:  time.Now().Unix(),
		Content:    strings.Join(parts, "; "),
		Tags:       tags,
		SessionID:  result.SessionID,
		Source:     "system",
		Importance: importance,
	}
}

// BuildFailureMemory 从任务失败上下文构建记忆记录
func BuildFailureMemory(failCtx *TaskFailureContext) *TaskMemoryRecord {
	content := fmt.Sprintf("任务失败: %s - %s", failCtx.Title, failCtx.Error)
	if failCtx.Phase != "" {
		content += fmt.Sprintf(" (阶段: %s)", failCtx.Phase)
	}

	tags := []string{"task_failure", failCtx.PlanID}
	if failCtx.Phase != "" {
		tags = append(tags, failCtx.Phase)
	}

	return &TaskMemoryRecord{
		ID:         fmt.Sprintf("failure_%s_%s", failCtx.TaskID, uuid.New().String()[:8]),
		Timestamp:  time.Now().Unix(),
		Content:    content,
		Tags:       tags,
		SessionID:  failCtx.SessionID,
		Source:     "system",
		Importance: 0.8,
	}
}

// BuildCompletionMemory 从任务完成结果构建记忆记录
func BuildCompletionMemory(result *TaskExecutionResult) *TaskMemoryRecord {
	parts := []string{
		fmt.Sprintf("任务完成: %s", result.Title),
	}

	if len(result.FilesModified) > 0 {
		parts = append(parts, fmt.Sprintf("修改文件: %s", strings.Join(result.FilesModified, ", ")))
	}

	if result.Output != "" {
		output := result.Output
		if len(output) > 200 {
			output = output[:200]
		}
		parts = append(parts, fmt.Sprintf("输出: %s", output))
	}

	tags := []string{"task_complete", result.PlanID}
	if len(result.FilesModified) > 3 {
		tags = append(tags, result.FilesModified[:3]...)
	} else {
		tags = append(tags, result.FilesModified...)
	}

	return &TaskMemoryRecord{
		ID:         fmt.Sprintf("complete_%s_%d", result.TaskID, time.Now().UnixMilli()),
		Timestamp:  time.Now().Unix(),
		Content:    strings.Join(parts, "; "),
		Tags:       tags,
		SessionID:  result.SessionID,
		Source:     "system",
		Importance: 0.6,
	}
}

func statusLabel(status string) string {
	if status == "completed" {
		return "完成"
	}
	return "执行"
}
