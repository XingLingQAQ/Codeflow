package git

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// GitManager Git管理器实现
type GitManager struct {
	workDir   string
	snapshots map[string]*GitSnapshot
	mappings  map[string]*SnapshotMapping
	mu        sync.RWMutex
}

// NewGitManager 创建Git管理器
func NewGitManager(workDir string) *GitManager {
	return &GitManager{
		workDir:   workDir,
		snapshots: make(map[string]*GitSnapshot),
		mappings:  make(map[string]*SnapshotMapping),
	}
}

// execGit 执行git命令
func (m *GitManager) execGit(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = m.workDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("git %s: %w (output: %s)", strings.Join(args, " "), err, string(output))
	}
	return strings.TrimSpace(string(output)), nil
}

// Init 初始化Git仓库
func (m *GitManager) Init(ctx context.Context) error {
	_, err := m.execGit(ctx, "init")
	return err
}

// IsRepo 检查是否为Git仓库
func (m *GitManager) IsRepo(ctx context.Context) (bool, error) {
	_, err := m.execGit(ctx, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		return false, nil
	}
	return true, nil
}

// Status 获取仓库状态
func (m *GitManager) Status(ctx context.Context) ([]GitDiff, error) {
	output, err := m.execGit(ctx, "status", "--porcelain")
	if err != nil {
		return nil, err
	}

	if output == "" {
		return []GitDiff{}, nil
	}

	var diffs []GitDiff
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) < 3 {
			continue
		}

		status := strings.TrimSpace(line[:2])
		file := line[3:]

		var diffStatus GitDiffStatus
		switch status {
		case "A", "??":
			diffStatus = DiffAdded
		case "D":
			diffStatus = DiffDeleted
		case "R":
			diffStatus = DiffRenamed
		default:
			diffStatus = DiffModified
		}

		diffs = append(diffs, GitDiff{
			File:   file,
			Status: diffStatus,
		})
	}

	return diffs, nil
}

// Add 添加文件到暂存区
func (m *GitManager) Add(ctx context.Context, paths ...string) error {
	if len(paths) == 0 {
		_, err := m.execGit(ctx, "add", "-A")
		return err
	}
	args := append([]string{"add"}, paths...)
	_, err := m.execGit(ctx, args...)
	return err
}

// Commit 创建提交
func (m *GitManager) Commit(ctx context.Context, message string) (*GitCommitInfo, error) {
	// 先添加所有变更
	if err := m.Add(ctx); err != nil {
		return nil, fmt.Errorf("add: %w", err)
	}

	// 提交
	_, err := m.execGit(ctx, "commit", "-m", message)
	if err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	// 获取提交信息
	output, err := m.execGit(ctx, "log", "-1", "--format=%H|%h|%s|%an|%at")
	if err != nil {
		return nil, fmt.Errorf("get commit info: %w", err)
	}

	parts := strings.SplitN(output, "|", 5)
	if len(parts) < 5 {
		return nil, fmt.Errorf("invalid commit info: %s", output)
	}

	timestamp, _ := strconv.ParseInt(parts[4], 10, 64)

	return &GitCommitInfo{
		Hash:      parts[0],
		ShortHash: parts[1],
		Message:   parts[2],
		Author:    parts[3],
		Timestamp: timestamp * 1000,
	}, nil
}

// GetLog 获取提交历史
func (m *GitManager) GetLog(ctx context.Context, limit int) ([]GitCommitInfo, error) {
	if limit <= 0 {
		limit = 10
	}

	output, err := m.execGit(ctx, "log", fmt.Sprintf("-%d", limit), "--format=%H|%h|%s|%an|%at")
	if err != nil {
		return nil, err
	}

	if output == "" {
		return []GitCommitInfo{}, nil
	}

	var commits []GitCommitInfo
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "|", 5)
		if len(parts) < 5 {
			continue
		}

		timestamp, _ := strconv.ParseInt(parts[4], 10, 64)
		commits = append(commits, GitCommitInfo{
			Hash:      parts[0],
			ShortHash: parts[1],
			Message:   parts[2],
			Author:    parts[3],
			Timestamp: timestamp * 1000,
		})
	}

	return commits, nil
}

// GetCurrentHash 获取当前HEAD哈希
func (m *GitManager) GetCurrentHash(ctx context.Context) (string, error) {
	output, err := m.execGit(ctx, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return output, nil
}

// ListBranches 列出所有分支
func (m *GitManager) ListBranches(ctx context.Context) ([]BranchInfo, error) {
	output, err := m.execGit(ctx, "branch", "-a", "--format=%(refname:short)|%(objectname:short)|%(HEAD)")
	if err != nil {
		return nil, err
	}

	if output == "" {
		return []BranchInfo{}, nil
	}

	var branches []BranchInfo
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, "|")
		if len(parts) < 3 {
			continue
		}

		branches = append(branches, BranchInfo{
			Name:      parts[0],
			Hash:      parts[1],
			IsCurrent: parts[2] == "*",
			IsRemote:  strings.HasPrefix(parts[0], "remotes/"),
		})
	}

	return branches, nil
}

// CreateBranch 创建分支
func (m *GitManager) CreateBranch(ctx context.Context, name string) error {
	_, err := m.execGit(ctx, "branch", name)
	return err
}

// SwitchBranch 切换分支
func (m *GitManager) SwitchBranch(ctx context.Context, name string) error {
	_, err := m.execGit(ctx, "checkout", name)
	return err
}

// DeleteBranch 删除分支
func (m *GitManager) DeleteBranch(ctx context.Context, name string) error {
	_, err := m.execGit(ctx, "branch", "-d", name)
	return err
}

// CreateSnapshot 创建快照
func (m *GitManager) CreateSnapshot(ctx context.Context, description string) (*GitSnapshot, error) {
	status, err := m.Status(ctx)
	if err != nil {
		return nil, err
	}

	var gitHash string
	if len(status) > 0 {
		// 有变更，创建新提交
		commitInfo, err := m.Commit(ctx, description)
		if err != nil {
			// 如果提交失败（可能没有变更），获取当前HEAD
			gitHash, _ = m.GetCurrentHash(ctx)
			if gitHash == "" {
				gitHash = "initial"
			}
		} else {
			gitHash = commitInfo.Hash
		}
	} else {
		// 无变更，使用当前HEAD
		gitHash, _ = m.GetCurrentHash(ctx)
		if gitHash == "" {
			gitHash = "initial"
		}
	}

	files := make([]string, len(status))
	for i, d := range status {
		files[i] = d.File
	}

	snapshot := &GitSnapshot{
		ID:              uuid.New().String(),
		GitHash:         gitHash,
		DialogStateHash: m.generateStateHash(),
		Timestamp:       time.Now().UnixMilli(),
		Description:     description,
		Files:           files,
	}

	m.mu.Lock()
	m.snapshots[snapshot.ID] = snapshot
	m.mu.Unlock()

	return snapshot, nil
}

// RestoreSnapshot 恢复快照
func (m *GitManager) RestoreSnapshot(ctx context.Context, snapshotID string) error {
	m.mu.RLock()
	snapshot, ok := m.snapshots[snapshotID]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("snapshot not found: %s", snapshotID)
	}

	return m.Reset(ctx, snapshot.GitHash, true)
}

// GetSnapshot 获取快照
func (m *GitManager) GetSnapshot(snapshotID string) *GitSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.snapshots[snapshotID]
}

// ListSnapshots 列出所有快照
func (m *GitManager) ListSnapshots() []GitSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	snapshots := make([]GitSnapshot, 0, len(m.snapshots))
	for _, s := range m.snapshots {
		snapshots = append(snapshots, *s)
	}
	return snapshots
}

// Reset 重置到指定提交
func (m *GitManager) Reset(ctx context.Context, hash string, hard bool) error {
	args := []string{"reset"}
	if hard {
		args = append(args, "--hard")
	}
	args = append(args, hash)
	_, err := m.execGit(ctx, args...)
	return err
}

// Revert 回滚指定提交
func (m *GitManager) Revert(ctx context.Context, hash string) (*GitCommitInfo, error) {
	_, err := m.execGit(ctx, "revert", "--no-edit", hash)
	if err != nil {
		return nil, err
	}

	// 获取新生成的提交信息
	output, err := m.execGit(ctx, "log", "-1", "--format=%H|%h|%s|%an|%at")
	if err != nil {
		return nil, err
	}

	parts := strings.SplitN(output, "|", 5)
	if len(parts) < 5 {
		return nil, fmt.Errorf("invalid commit info")
	}

	timestamp, _ := strconv.ParseInt(parts[4], 10, 64)
	return &GitCommitInfo{
		Hash:      parts[0],
		ShortHash: parts[1],
		Message:   parts[2],
		Author:    parts[3],
		Timestamp: timestamp * 1000,
	}, nil
}

// HasConflicts 检查是否有冲突
func (m *GitManager) HasConflicts(ctx context.Context) (bool, error) {
	output, err := m.execGit(ctx, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		// 没有冲突时可能返回错误
		return false, nil
	}
	return strings.TrimSpace(output) != "", nil
}

// GetConflicts 获取冲突文件详情
func (m *GitManager) GetConflicts(ctx context.Context) ([]ConflictInfo, error) {
	output, err := m.execGit(ctx, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return []ConflictInfo{}, nil
	}

	if output == "" {
		return []ConflictInfo{}, nil
	}

	var conflicts []ConflictInfo
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		file := scanner.Text()
		conflicts = append(conflicts, ConflictInfo{
			File:    file,
			Markers: []string{"<<<<<<<", "=======", ">>>>>>>"},
		})
	}

	return conflicts, nil
}

// AddMapping 添加映射
func (m *GitManager) AddMapping(mapping *SnapshotMapping) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mappings[mapping.SnapshotID] = mapping
}

// GetMapping 获取映射
func (m *GitManager) GetMapping(snapshotID string) *SnapshotMapping {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.mappings[snapshotID]
}

// GetMappingByGitHash 通过Git哈希获取映射
func (m *GitManager) GetMappingByGitHash(gitHash string) *SnapshotMapping {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, mapping := range m.mappings {
		if mapping.GitHash == gitHash {
			return mapping
		}
	}
	return nil
}

// DiffBetween 获取两个提交之间的差异
func (m *GitManager) DiffBetween(ctx context.Context, from, to string) ([]GitDiff, error) {
	output, err := m.execGit(ctx, "diff", "--name-status", from, to)
	if err != nil {
		return nil, err
	}

	if output == "" {
		return []GitDiff{}, nil
	}

	var diffs []GitDiff
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		status := parts[0]
		file := parts[1]

		var diffStatus GitDiffStatus
		switch status {
		case "A":
			diffStatus = DiffAdded
		case "D":
			diffStatus = DiffDeleted
		case "R":
			diffStatus = DiffRenamed
		default:
			diffStatus = DiffModified
		}

		diffs = append(diffs, GitDiff{
			File:   file,
			Status: diffStatus,
		})
	}

	return diffs, nil
}

// generateStateHash 生成状态哈希
func (m *GitManager) generateStateHash() string {
	data := fmt.Sprintf("%d_%d", time.Now().UnixNano(), time.Now().UnixMilli())
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])[:16]
}
