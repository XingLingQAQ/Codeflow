package isolation

import (
	"context"
)

// IsolationAgentRole 智能体角色
type IsolationAgentRole string

const (
	RoleMain       IsolationAgentRole = "main"
	RoleCoder      IsolationAgentRole = "coder"
	RoleReviewer   IsolationAgentRole = "reviewer"
	RolePlanner    IsolationAgentRole = "planner"
	RoleResearcher IsolationAgentRole = "researcher"
	RoleExecutor   IsolationAgentRole = "executor"
)

// PermissionLevel 权限级别
type PermissionLevel string

const (
	PermissionNone      PermissionLevel = "none"
	PermissionRead      PermissionLevel = "read"
	PermissionWrite     PermissionLevel = "write"
	PermissionExecute   PermissionLevel = "execute"
	PermissionAdmin     PermissionLevel = "admin"
)

// ResourceType 资源类型
type ResourceType string

const (
	ResourceFile       ResourceType = "file"
	ResourceMemory     ResourceType = "memory"
	ResourceNetwork    ResourceType = "network"
	ResourceProcess    ResourceType = "process"
	ResourceConfig     ResourceType = "config"
	ResourceAudit      ResourceType = "audit"
)

// Permission 权限定义
type Permission struct {
	Resource   ResourceType    `json:"resource"`
	Level      PermissionLevel `json:"level"`
	Pattern    string          `json:"pattern,omitempty"`
	Conditions []string        `json:"conditions,omitempty"`
}

// RoleDefinition 角色定义
type RoleDefinition struct {
	Name          IsolationAgentRole `json:"name"`
	Description   string             `json:"description"`
	Permissions   []Permission       `json:"permissions"`
	Inherits      []IsolationAgentRole `json:"inherits,omitempty"`
	MaxConcurrent int                `json:"max_concurrent"`
}

// ContextContainer 上下文容器
type ContextContainer struct {
	ID              string             `json:"id"`
	Role            IsolationAgentRole `json:"role"`
	ParentID        string             `json:"parent_id,omitempty"`
	CreatedAt       int64              `json:"created_at"`
	ExpiresAt       int64              `json:"expires_at,omitempty"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
	IsolationLevel  string             `json:"isolation_level"`
	ResourceQuota   *ResourceQuota     `json:"resource_quota,omitempty"`
}

// ResourceQuota 资源配额
type ResourceQuota struct {
	MaxMemoryMB     int   `json:"max_memory_mb"`
	MaxFileSizeMB   int   `json:"max_file_size_mb"`
	MaxFileCount    int   `json:"max_file_count"`
	MaxNetworkConns int   `json:"max_network_conns"`
	MaxCPUPercent   int   `json:"max_cpu_percent"`
}

// AccessRequest 访问请求
type AccessRequest struct {
	ContainerID  string       `json:"container_id"`
	Resource     ResourceType `json:"resource"`
	ResourcePath string       `json:"resource_path,omitempty"`
	Action       string       `json:"action"`
	Timestamp    int64        `json:"timestamp"`
	Context      map[string]interface{} `json:"context,omitempty"`
}

// AccessDecision 访问决策
type AccessDecision struct {
	Allowed     bool     `json:"allowed"`
	Reason      string   `json:"reason"`
	Constraints []string `json:"constraints,omitempty"`
	AuditID     string   `json:"audit_id,omitempty"`
}

// IOValidationResult I/O验证结果
type IOValidationResult struct {
	Valid           bool     `json:"valid"`
	SanitizedInput  string   `json:"sanitized_input,omitempty"`
	Warnings        []string `json:"warnings,omitempty"`
	BlockedPatterns []string `json:"blocked_patterns,omitempty"`
}

// IsolationLevel 隔离级别
type IsolationLevel string

const (
	IsolationNone     IsolationLevel = "none"
	IsolationProcess  IsolationLevel = "process"
	IsolationSandbox  IsolationLevel = "sandbox"
	IsolationStrict   IsolationLevel = "strict"
)

// PermissionPriority 权限优先级
var PermissionPriority = map[PermissionLevel]int{
	PermissionNone:    0,
	PermissionRead:    1,
	PermissionWrite:   2,
	PermissionExecute: 3,
	PermissionAdmin:   4,
}

// DefaultRoles 默认角色定义
var DefaultRoles = map[IsolationAgentRole]RoleDefinition{
	RoleMain: {
		Name:        RoleMain,
		Description: "Main orchestrator with full access",
		Permissions: []Permission{
			{Resource: ResourceFile, Level: PermissionAdmin},
			{Resource: ResourceMemory, Level: PermissionAdmin},
			{Resource: ResourceNetwork, Level: PermissionAdmin},
			{Resource: ResourceProcess, Level: PermissionAdmin},
			{Resource: ResourceConfig, Level: PermissionAdmin},
			{Resource: ResourceAudit, Level: PermissionRead},
		},
		MaxConcurrent: 1,
	},
	RoleCoder: {
		Name:        RoleCoder,
		Description: "Code generation and modification",
		Permissions: []Permission{
			{Resource: ResourceFile, Level: PermissionWrite, Pattern: "**/*.{ts,js,go,py,java}"},
			{Resource: ResourceMemory, Level: PermissionRead},
			{Resource: ResourceProcess, Level: PermissionExecute},
		},
		MaxConcurrent: 3,
	},
	RoleReviewer: {
		Name:        RoleReviewer,
		Description: "Code review and analysis",
		Permissions: []Permission{
			{Resource: ResourceFile, Level: PermissionRead},
			{Resource: ResourceMemory, Level: PermissionRead},
			{Resource: ResourceAudit, Level: PermissionRead},
		},
		MaxConcurrent: 5,
	},
	RolePlanner: {
		Name:        RolePlanner,
		Description: "Task planning and decomposition",
		Permissions: []Permission{
			{Resource: ResourceFile, Level: PermissionRead},
			{Resource: ResourceMemory, Level: PermissionWrite},
			{Resource: ResourceConfig, Level: PermissionRead},
		},
		MaxConcurrent: 2,
	},
	RoleResearcher: {
		Name:        RoleResearcher,
		Description: "Information gathering and research",
		Permissions: []Permission{
			{Resource: ResourceFile, Level: PermissionRead},
			{Resource: ResourceNetwork, Level: PermissionRead},
			{Resource: ResourceMemory, Level: PermissionWrite},
		},
		MaxConcurrent: 5,
	},
	RoleExecutor: {
		Name:        RoleExecutor,
		Description: "Command execution",
		Permissions: []Permission{
			{Resource: ResourceProcess, Level: PermissionExecute},
			{Resource: ResourceFile, Level: PermissionRead},
		},
		MaxConcurrent: 2,
	},
}

// IIsolationManager 隔离管理器接口
type IIsolationManager interface {
	// CreateContainer 创建隔离容器
	CreateContainer(ctx context.Context, role IsolationAgentRole, parentID string) (*ContextContainer, error)

	// GetContainer 获取容器
	GetContainer(ctx context.Context, containerID string) (*ContextContainer, error)

	// DestroyContainer 销毁容器
	DestroyContainer(ctx context.Context, containerID string) error

	// CheckAccess 检查访问权限
	CheckAccess(ctx context.Context, request AccessRequest) (*AccessDecision, error)

	// ValidateIO 验证I/O
	ValidateIO(ctx context.Context, containerID string, input string, direction string) (*IOValidationResult, error)

	// GetContainersByRole 按角色获取容器
	GetContainersByRole(ctx context.Context, role IsolationAgentRole) ([]*ContextContainer, error)

	// SetResourceQuota 设置资源配额
	SetResourceQuota(ctx context.Context, containerID string, quota ResourceQuota) error
}

// IRBACManager RBAC管理器接口
type IRBACManager interface {
	// GetRole 获取角色定义
	GetRole(role IsolationAgentRole) (*RoleDefinition, bool)

	// HasPermission 检查权限
	HasPermission(role IsolationAgentRole, resource ResourceType, level PermissionLevel) bool

	// GetEffectivePermissions 获取有效权限（含继承）
	GetEffectivePermissions(role IsolationAgentRole) []Permission

	// RegisterRole 注册自定义角色
	RegisterRole(definition RoleDefinition) error

	// CheckResourceAccess 检查资源访问
	CheckResourceAccess(role IsolationAgentRole, resource ResourceType, path string, action string) bool
}
