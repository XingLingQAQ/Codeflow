package git

import "context"

// GitDiffStatus 文件变更状态
type GitDiffStatus string

const (
	DiffAdded    GitDiffStatus = "added"
	DiffModified GitDiffStatus = "modified"
	DiffDeleted  GitDiffStatus = "deleted"
	DiffRenamed  GitDiffStatus = "renamed"
)

// GitDiff 文件差异
type GitDiff struct {
	File      string        `json:"file"`
	Status    GitDiffStatus `json:"status"`
	Additions int           `json:"additions"`
	Deletions int           `json:"deletions"`
}

// GitCommitInfo 提交信息
type GitCommitInfo struct {
	Hash      string `json:"hash"`
	ShortHash string `json:"short_hash"`
	Message   string `json:"message"`
	Author    string `json:"author"`
	Timestamp int64  `json:"timestamp"`
}

// GitSnapshot Git快照
type GitSnapshot struct {
	ID              string   `json:"id"`
	GitHash         string   `json:"git_hash"`
	DialogStateHash string   `json:"dialog_state_hash"`
	VectorStateHash string   `json:"vector_state_hash,omitempty"`
	Timestamp       int64    `json:"timestamp"`
	Description     string   `json:"description,omitempty"`
	Files           []string `json:"files"`
}

// SnapshotMapping 快照映射
type SnapshotMapping struct {
	SnapshotID string `json:"snapshot_id"`
	GitHash    string `json:"git_hash"`
	SessionID  string `json:"session_id"`
	MessageID  string `json:"message_id,omitempty"`
}

// ConflictInfo 冲突信息
type ConflictInfo struct {
	File        string   `json:"file"`
	OurChanges  string   `json:"our_changes"`
	TheirChanges string  `json:"their_changes"`
	Markers     []string `json:"markers"`
}

// MergeResult 合并结果
type MergeResult struct {
	Success   bool           `json:"success"`
	Conflicts []ConflictInfo `json:"conflicts,omitempty"`
	Message   string         `json:"message"`
}

// BranchInfo 分支信息
type BranchInfo struct {
	Name      string `json:"name"`
	Hash      string `json:"hash"`
	IsCurrent bool   `json:"is_current"`
	IsRemote  bool   `json:"is_remote"`
}

// IGitManager Git管理器接口
type IGitManager interface {
	// 基础操作
	Init(ctx context.Context) error
	IsRepo(ctx context.Context) (bool, error)
	Status(ctx context.Context) ([]GitDiff, error)

	// 提交操作
	Add(ctx context.Context, paths ...string) error
	Commit(ctx context.Context, message string) (*GitCommitInfo, error)
	GetLog(ctx context.Context, limit int) ([]GitCommitInfo, error)
	GetCurrentHash(ctx context.Context) (string, error)

	// 分支操作
	ListBranches(ctx context.Context) ([]BranchInfo, error)
	CreateBranch(ctx context.Context, name string) error
	SwitchBranch(ctx context.Context, name string) error
	DeleteBranch(ctx context.Context, name string) error

	// 快照操作
	CreateSnapshot(ctx context.Context, description string) (*GitSnapshot, error)
	RestoreSnapshot(ctx context.Context, snapshotID string) error
	GetSnapshot(snapshotID string) *GitSnapshot
	ListSnapshots() []GitSnapshot

	// 回滚操作
	Reset(ctx context.Context, hash string, hard bool) error
	Revert(ctx context.Context, hash string) (*GitCommitInfo, error)

	// 冲突检测
	HasConflicts(ctx context.Context) (bool, error)
	GetConflicts(ctx context.Context) ([]ConflictInfo, error)

	// 映射管理
	AddMapping(mapping *SnapshotMapping)
	GetMapping(snapshotID string) *SnapshotMapping
	GetMappingByGitHash(gitHash string) *SnapshotMapping

	// 差异比较
	DiffBetween(ctx context.Context, from, to string) ([]GitDiff, error)
}

// SnapshotTrigger 快照触发类型
type SnapshotTrigger string

const (
	TriggerHookAfterExec   SnapshotTrigger = "hook_after_exec"
	TriggerManual          SnapshotTrigger = "manual"
	TriggerAutoCheckpoint  SnapshotTrigger = "auto_checkpoint"
	TriggerBeforeRollback  SnapshotTrigger = "before_rollback"
	TriggerSessionEnd      SnapshotTrigger = "session_end"
)

// RollbackOptions 回滚选项
type RollbackOptions struct {
	TargetSnapshotID     string `json:"target_snapshot_id"`
	RollbackGit          bool   `json:"rollback_git"`
	RollbackConversation bool   `json:"rollback_conversation"`
	RollbackVector       bool   `json:"rollback_vector"`
	RollbackGraph        bool   `json:"rollback_graph"`
	CreateBackup         bool   `json:"create_backup"`
}

// RollbackResult 回滚结果
type RollbackResult struct {
	Success         bool   `json:"success"`
	BackupSnapshotID string `json:"backup_snapshot_id,omitempty"`
	RolledBack      struct {
		Git          bool `json:"git"`
		Conversation bool `json:"conversation"`
		Vector       bool `json:"vector"`
		Graph        bool `json:"graph"`
	} `json:"rolled_back"`
	Errors []string `json:"errors,omitempty"`
}
