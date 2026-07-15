// Package hooks - Hook system types
package hooks

import (
	"context"
	"time"
)

// HookPayload 表示 Hook 处理的动态输入边界。
type HookPayload = any

// HookResult 表示 Hook 处理的动态输出边界。
type HookResult = any

// HookMetadata 表示 Hook 相关元数据。
type HookMetadata = map[string]any

// HookDetails 表示 Hook 审计/事件详情。
type HookDetails = map[string]any

// HookType represents the type of hook.
type HookType string

const (
	// Lifecycle hooks
	HookBeforeSend   HookType = "hook_before_send"
	HookPostResponse HookType = "hook_post_response"
	HookOnStream     HookType = "hook_on_stream"

	// Context management hooks
	HookBeforeCompress    HookType = "hook_before_compress"
	HookOnMessageComplete HookType = "hook_on_message_complete"
	// Workspace write intercept (payload: map with path/content)
	HookBeforeWrite HookType = "hook_before_write"

	// State management hooks
	HookAfterExec    HookType = "hook_after_exec"
	HookRestoreState HookType = "hook_restore_state"

	// Memory retrieval hooks
	HookOnUserInputSubmitted HookType = "hook_on_user_input_submitted"

	// Task lifecycle hooks
	HookBeforeTaskExecute HookType = "hook_before_task_execute"
	HookAfterTaskExecute  HookType = "hook_after_task_execute"
	HookOnTaskFailure     HookType = "hook_on_task_failure"
	HookOnTaskComplete    HookType = "hook_on_task_complete"
)

// ExecResult describes command or tool execution output for after-exec hooks.
type ExecResult struct {
	Command       string       `json:"command"`
	ExitCode      int          `json:"exit_code"`
	Stdout        string       `json:"stdout,omitempty"`
	Stderr        string       `json:"stderr,omitempty"`
	Timestamp     int64        `json:"timestamp"`
	SessionID     string       `json:"session_id,omitempty"`
	TaskID        string       `json:"task_id,omitempty"`
	AgentID       string       `json:"agent_id,omitempty"`
	FilesModified []string     `json:"files_modified,omitempty"`
	Metadata      HookMetadata `json:"metadata,omitempty"`
}

// MemoryMatch describes memory retrieval results from user-input hooks.
type MemoryMatch struct {
	Content    string       `json:"content"`
	Similarity float64      `json:"similarity"`
	Source     string       `json:"source"`
	Metadata   HookMetadata `json:"metadata,omitempty"`
}

// HookRuntimeControls controls global hook execution.
type HookRuntimeControls struct {
	Enabled      *bool      `json:"enabled,omitempty"`
	AllowedHooks []HookType `json:"allowed_hooks,omitempty"`
}

// HookFunc is the function signature for hook handlers.
type HookFunc func(ctx context.Context, payload HookPayload) (HookResult, error)

// HookConfig represents the configuration for a hook.
type HookConfig struct {
	Name       string        `json:"name"`
	Type       HookType      `json:"type"`
	Enabled    bool          `json:"enabled"`
	Priority   int           `json:"priority"` // Lower number = higher priority
	Timeout    time.Duration `json:"timeout"`
	RetryCount int           `json:"retry_count"`
	Metadata   HookMetadata  `json:"metadata"`
}

// HookEvent represents a hook execution event.
type HookEvent struct {
	ID         string        `json:"id"`
	HookName   string        `json:"hook_name"`
	HookType   HookType      `json:"hook_type"`
	Timestamp  time.Time     `json:"timestamp"`
	Duration   time.Duration `json:"duration"`
	Success    bool          `json:"success"`
	Error      string        `json:"error,omitempty"`
	InputSize  int           `json:"input_size"`
	OutputSize int           `json:"output_size"`
	Metadata   HookMetadata  `json:"metadata,omitempty"`
}

// Hook represents a registered hook.
type Hook struct {
	Config  HookConfig
	Handler HookFunc
}

// IHookManager defines the interface for hook management.
type IHookManager interface {
	// Register registers a new hook.
	Register(config HookConfig, handler HookFunc) error

	// Unregister removes a hook by name.
	Unregister(name string) error

	// Enable enables a hook by name.
	Enable(name string) error

	// Disable disables a hook by name.
	Disable(name string) error

	// Trigger triggers all hooks of a specific type.
	Trigger(ctx context.Context, hookType HookType, payload HookPayload) (HookResult, error)

	// TriggerHook triggers a specific hook by name.
	TriggerHook(ctx context.Context, name string, payload HookPayload) (HookResult, error)

	// TriggerAsync triggers hooks asynchronously.
	TriggerAsync(ctx context.Context, hookType HookType, payload HookPayload) error

	// GetHook returns a hook by name.
	GetHook(name string) (*Hook, error)

	// ListHooks returns all registered hooks.
	ListHooks() []*Hook

	// ListHooksByType returns hooks of a specific type.
	ListHooksByType(hookType HookType) []*Hook

	// GetEvents returns hook execution events.
	GetEvents(limit int, offset int) []*HookEvent

	// GetEventsByHook returns events for a specific hook.
	GetEventsByHook(hookName string, limit int, offset int) []*HookEvent

	// ClearEvents clears all hook events.
	ClearEvents() error

	// UpdateConfig updates hook configuration.
	UpdateConfig(name string, config HookConfig) error

	// SetControls updates global hook runtime controls.
	SetControls(controls HookRuntimeControls)

	// GetControls returns global hook runtime controls.
	GetControls() HookRuntimeControls

	// HookAfterExec triggers after-exec hooks.
	HookAfterExec(ctx context.Context, payload HookPayload) (HookResult, error)

	// HookRestoreState triggers restore-state hooks.
	HookRestoreState(ctx context.Context, payload HookPayload) (HookResult, error)

	// HookOnUserInputSubmitted triggers user-input-submitted hooks.
	HookOnUserInputSubmitted(ctx context.Context, payload HookPayload) (HookResult, error)

	// HookBeforeTaskExecute triggers before-task-execute hooks.
	HookBeforeTaskExecute(ctx context.Context, payload HookPayload) error

	// HookAfterTaskExecute triggers after-task-execute hooks.
	HookAfterTaskExecute(ctx context.Context, payload HookPayload) error

	// HookOnTaskFailure triggers task-failure hooks.
	HookOnTaskFailure(ctx context.Context, payload HookPayload) error

	// HookOnTaskComplete triggers task-complete hooks.
	HookOnTaskComplete(ctx context.Context, payload HookPayload) error
}
