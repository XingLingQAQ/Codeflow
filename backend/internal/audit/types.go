package audit

import "context"

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

// AuditActor 审计参与者
type AuditActor struct {
	ID        string `json:"id"`
	Type      string `json:"type"` // user, system, agent, service
	Name      string `json:"name,omitempty"`
	IP        string `json:"ip,omitempty"`
	SessionID string `json:"session_id,omitempty"`
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
