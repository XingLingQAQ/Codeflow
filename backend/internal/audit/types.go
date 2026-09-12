package audit

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// AuditEventType 审计事件类型
type AuditEventType string

const (
	EventAccess           AuditEventType = "access"
	EventModify           AuditEventType = "modify"
	EventDelete           AuditEventType = "delete"
	EventCreate           AuditEventType = "create"
	EventLogin            AuditEventType = "login"
	EventLogout           AuditEventType = "logout"
	EventPermissionChange AuditEventType = "permission_change"
	EventConfigChange     AuditEventType = "config_change"
	EventError            AuditEventType = "error"
	EventSecurity         AuditEventType = "security"
	EventHook             AuditEventType = "hook"
	EventApproval         AuditEventType = "approval"
	EventPrivacy          AuditEventType = "privacy"
	EventIsolation        AuditEventType = "isolation"
)

// AuditSeverity 审计严重级别
type AuditSeverity string

const (
	SeverityInfo     AuditSeverity = "info"
	SeverityWarning  AuditSeverity = "warning"
	SeverityError    AuditSeverity = "error"
	SeverityCritical AuditSeverity = "critical"
)

// AuditOutcome 审计结果
type AuditOutcome string

const (
	OutcomeSuccess AuditOutcome = "success"
	OutcomeFailure AuditOutcome = "failure"
)

// GenesisHash 创世块哈希（链的起点）
const GenesisHash = "0000000000000000000000000000000000000000000000000000000000000000"

// ActorType 审计操作者类型，固定四枚举（I-50）。
// user=终端用户（如审批人）；agent=AI Agent 执行体；system=服务端自身
// （恢复器、后台任务、迁移）；integration=外部集成（如已验签的 GitHub webhook）。
type ActorType string

const (
	ActorTypeUser        ActorType = "user"
	ActorTypeAgent       ActorType = "agent"
	ActorTypeSystem      ActorType = "system"
	ActorTypeIntegration ActorType = "integration"
)

const (
	// MaxActorIDLength 操作者 ID 最大字节长度
	MaxActorIDLength = 128
	// MaxActorSourceLength 操作者来源标识最大字节长度
	MaxActorSourceLength = 64
)

// Actor 校验错误哨兵，调用方可 errors.Is 判定。
var (
	ErrInvalidActorType   = errors.New("audit: invalid actor type")
	ErrEmptyActorID       = errors.New("audit: actor id is empty")
	ErrActorIDTooLong     = errors.New("audit: actor id too long")
	ErrActorSourceTooLong = errors.New("audit: actor source too long")
)

// Actor 带校验的审计操作者身份（I-50 / T0.10）。
//
// Source 记录身份在何处确立，用于区分可信等级，约定值：
//   - 服务端可信来源：如 "http-session"（服务端会话认证）、"approval"（审批流）、
//     "recovery"（系统恢复）、"webhook:github"（已验签的集成回调）；
//   - 客户端声明：如 "client-header"（请求头自报），仅作如实记录，不可作为授权依据。
//
// Validate 只做形状校验（type 枚举、id 非空且不超长、source 不超长）；
// 它无法也无意证明身份可信——可信性由入口（认证中间件、恢复器、webhook 验签）
// 在注入上下文时保证。校验拒绝冒充形状（如空 id 声称 agent），但不判断来源真假。
type Actor struct {
	Type   ActorType `json:"type"`
	ID     string    `json:"id"`
	Source string    `json:"source,omitempty"`
}

// Validate 校验操作者身份形状；非法 type、空 id、id/source 超长均拒绝。
func (a Actor) Validate() error {
	switch a.Type {
	case ActorTypeUser, ActorTypeAgent, ActorTypeSystem, ActorTypeIntegration:
	default:
		return fmt.Errorf("%w: %q", ErrInvalidActorType, string(a.Type))
	}
	if strings.TrimSpace(a.ID) == "" {
		return ErrEmptyActorID
	}
	if len(a.ID) > MaxActorIDLength {
		return fmt.Errorf("%w: %d > %d", ErrActorIDTooLong, len(a.ID), MaxActorIDLength)
	}
	if len(a.Source) > MaxActorSourceLength {
		return fmt.Errorf("%w: %d > %d", ErrActorSourceTooLong, len(a.Source), MaxActorSourceLength)
	}
	return nil
}

// AuditActor 转换为存量审计条目的参与者形状（兼容挂载，Source 为 omitempty 新增）。
func (a Actor) AuditActor() AuditActor {
	return AuditActor{ID: a.ID, Type: string(a.Type), Source: a.Source}
}

// AuditActor 审计参与者
type AuditActor struct {
	ID        string `json:"id"`
	Type      string `json:"type"` // user, system, agent, service
	Name      string `json:"name,omitempty"`
	IP        string `json:"ip,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	// Source 身份来源（见 Actor.Source 约定）；omitempty 兼容新增，
	// 空值时序列化与哈希与旧条目完全一致。
	Source string `json:"source,omitempty"`
}

// AuditResource 审计资源
type AuditResource struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	Path string `json:"path,omitempty"`
}

// AuditTrace 审计追踪信息
type AuditTrace struct {
	RequestID  string  `json:"request_id,omitempty"`
	ProjectID  string  `json:"project_id,omitempty"`
	PlanID     string  `json:"plan_id,omitempty"`
	SessionID  string  `json:"session_id,omitempty"`
	TaskID     string  `json:"task_id,omitempty"`
	AgentID    string  `json:"agent_id,omitempty"`
	Method     string  `json:"method,omitempty"`
	Path       string  `json:"path,omitempty"`
	Route      string  `json:"route,omitempty"`
	StatusCode int     `json:"status_code,omitempty"`
	LatencyMs  float64 `json:"latency_ms,omitempty"`
}

// AuditLogEntry 审计日志条目
type AuditLogEntry struct {
	ID           string                 `json:"id"`
	Timestamp    int64                  `json:"timestamp"`
	EventType    AuditEventType         `json:"event_type"`
	Severity     AuditSeverity          `json:"severity"`
	Actor        AuditActor             `json:"actor"`
	Resource     AuditResource          `json:"resource"`
	Action       string                 `json:"action"`
	Outcome      AuditOutcome           `json:"outcome"`
	Trace        *AuditTrace            `json:"trace,omitempty"`
	Details      map[string]interface{} `json:"details,omitempty"`
	PreviousHash string                 `json:"previous_hash"`
	Hash         string                 `json:"hash"`
}

// AuditQuery 审计查询条件
type AuditQuery struct {
	StartTime    int64            `json:"start_time,omitempty"`
	EndTime      int64            `json:"end_time,omitempty"`
	EventTypes   []AuditEventType `json:"event_types,omitempty"`
	Severities   []AuditSeverity  `json:"severities,omitempty"`
	ActorID      string           `json:"actor_id,omitempty"`
	ResourceID   string           `json:"resource_id,omitempty"`
	ResourceType string           `json:"resource_type,omitempty"`
	Outcome      AuditOutcome     `json:"outcome,omitempty"`
	Limit        int              `json:"limit,omitempty"`
	Offset       int              `json:"offset,omitempty"`
}

// AuditQueryResult 审计查询结果
type AuditQueryResult struct {
	Entries []AuditLogEntry `json:"entries"`
	Total   int             `json:"total"`
	HasMore bool            `json:"has_more"`
}

// IntegrityVerificationResult 完整性验证结果
type IntegrityVerificationResult struct {
	Valid          bool     `json:"valid"`
	CheckedEntries int      `json:"checked_entries"`
	InvalidEntries []string `json:"invalid_entries"`
	BrokenChainAt  string   `json:"broken_chain_at,omitempty"`
	VerifiedAt     int64    `json:"verified_at"`
}

// AuditStatistics 审计统计
type AuditStatistics struct {
	TotalEntries      int                       `json:"total_entries"`
	EntriesByType     map[AuditEventType]int    `json:"entries_by_type"`
	EntriesBySeverity map[AuditSeverity]int     `json:"entries_by_severity"`
	SuccessCount      int                       `json:"success_count"`
	FailureCount      int                       `json:"failure_count"`
	OldestEntry       int64                     `json:"oldest_entry,omitempty"`
	NewestEntry       int64                     `json:"newest_entry,omitempty"`
	StorageBytes      int64                     `json:"storage_bytes"`
}

// FileStorageConfig 文件存储配置
type FileStorageConfig struct {
	LogDir          string `json:"log_dir"`
	FilePrefix      string `json:"file_prefix"`
	MaxFileSize     int64  `json:"max_file_size"`
	MaxFiles        int    `json:"max_files"`
	VerifyOnStartup bool   `json:"verify_on_startup"`
	FlushInterval   int    `json:"flush_interval_ms"`
}

// DefaultFileStorageConfig 默认文件存储配置
var DefaultFileStorageConfig = FileStorageConfig{
	LogDir:          "./audit-logs",
	FilePrefix:      "audit",
	MaxFileSize:     10 * 1024 * 1024, // 10MB
	MaxFiles:        10,
	VerifyOnStartup: true,
	FlushInterval:   1000,
}

// IAuditStorage 审计存储接口
type IAuditStorage interface {
	Append(ctx context.Context, entry *AuditLogEntry) error
	Get(ctx context.Context, id string) (*AuditLogEntry, error)
	Query(ctx context.Context, query *AuditQuery) ([]AuditLogEntry, error)
	Count(ctx context.Context, query *AuditQuery) (int, error)
	GetLastEntry(ctx context.Context) (*AuditLogEntry, error)
	Delete(ctx context.Context, ids []string) (int, error)
	Clear(ctx context.Context) error
	VerifyHashChain(ctx context.Context) (*IntegrityVerificationResult, error)
	Close() error
}
