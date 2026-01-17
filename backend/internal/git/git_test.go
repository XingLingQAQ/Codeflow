package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGitManagerInit(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "git_test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewGitManager(tmpDir)
	ctx := context.Background()

	// 初始化仓库
	if err := manager.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}

	// 检查是否为仓库
	isRepo, err := manager.IsRepo(ctx)
	if err != nil {
		t.Fatalf("is repo: %v", err)
	}
	if !isRepo {
		t.Error("expected to be a repo after init")
	}
}

func TestGitManagerCommit(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "git_test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewGitManager(tmpDir)
	ctx := context.Background()

	// 初始化仓库
	if err := manager.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}

	// 配置git用户（测试环境需要）
	manager.execGit(ctx, "config", "user.email", "test@test.com")
	manager.execGit(ctx, "config", "user.name", "Test User")

	// 创建测试文件
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello world"), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	// 检查状态
	status, err := manager.Status(ctx)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(status) != 1 {
		t.Errorf("expected 1 file in status, got %d", len(status))
	}
	if status[0].Status != DiffAdded {
		t.Errorf("expected status 'added', got %s", status[0].Status)
	}

	// 提交
	commit, err := manager.Commit(ctx, "Initial commit")
	if err != nil {
		t.Fatalf("commit: %v", err)
	}

	if commit.Hash == "" {
		t.Error("expected non-empty hash")
	}
	if commit.Message != "Initial commit" {
		t.Errorf("expected message 'Initial commit', got %s", commit.Message)
	}

	// 检查状态应该为空
	status, err = manager.Status(ctx)
	if err != nil {
		t.Fatalf("status after commit: %v", err)
	}
	if len(status) != 0 {
		t.Errorf("expected 0 files in status after commit, got %d", len(status))
	}
}

func TestGitManagerLog(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "git_test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewGitManager(tmpDir)
	ctx := context.Background()

	// 初始化并配置
	manager.Init(ctx)
	manager.execGit(ctx, "config", "user.email", "test@test.com")
	manager.execGit(ctx, "config", "user.name", "Test User")

	// 创建多个提交
	for i := 0; i < 3; i++ {
		testFile := filepath.Join(tmpDir, "test.txt")
		os.WriteFile(testFile, []byte(string(rune('a'+i))), 0644)
		manager.Commit(ctx, "Commit "+string(rune('A'+i)))
		time.Sleep(10 * time.Millisecond)
	}

	// 获取日志
	logs, err := manager.GetLog(ctx, 10)
	if err != nil {
		t.Fatalf("get log: %v", err)
	}

	if len(logs) != 3 {
		t.Errorf("expected 3 commits, got %d", len(logs))
	}

	// 最新的应该在前面
	if logs[0].Message != "Commit C" {
		t.Errorf("expected latest commit 'Commit C', got %s", logs[0].Message)
	}
}

func TestGitManagerSnapshot(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "git_test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewGitManager(tmpDir)
	ctx := context.Background()

	// 初始化并配置
	manager.Init(ctx)
	manager.execGit(ctx, "config", "user.email", "test@test.com")
	manager.execGit(ctx, "config", "user.name", "Test User")

	// 创建初始文件
	testFile := filepath.Join(tmpDir, "test.txt")
	os.WriteFile(testFile, []byte("initial"), 0644)

	// 创建快照
	snapshot, err := manager.CreateSnapshot(ctx, "First snapshot")
	if err != nil {
		t.Fatalf("create snapshot: %v", err)
	}

	if snapshot.ID == "" {
		t.Error("expected non-empty snapshot ID")
	}
	if snapshot.GitHash == "" {
		t.Error("expected non-empty git hash")
	}

	// 获取快照
	retrieved := manager.GetSnapshot(snapshot.ID)
	if retrieved == nil {
		t.Fatal("expected to get snapshot")
	}
	if retrieved.ID != snapshot.ID {
		t.Errorf("expected ID %s, got %s", snapshot.ID, retrieved.ID)
	}

	// 列出快照
	snapshots := manager.ListSnapshots()
	if len(snapshots) != 1 {
		t.Errorf("expected 1 snapshot, got %d", len(snapshots))
	}
}

func TestGitManagerReset(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "git_test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewGitManager(tmpDir)
	ctx := context.Background()

	// 初始化并配置
	manager.Init(ctx)
	manager.execGit(ctx, "config", "user.email", "test@test.com")
	manager.execGit(ctx, "config", "user.name", "Test User")

	testFile := filepath.Join(tmpDir, "test.txt")

	// 第一次提交
	os.WriteFile(testFile, []byte("first"), 0644)
	commit1, _ := manager.Commit(ctx, "First")

	// 第二次提交
	os.WriteFile(testFile, []byte("second"), 0644)
	manager.Commit(ctx, "Second")

	// 验证当前内容
	content, _ := os.ReadFile(testFile)
	if string(content) != "second" {
		t.Errorf("expected 'second', got %s", string(content))
	}

	// 重置到第一次提交
	if err := manager.Reset(ctx, commit1.Hash, true); err != nil {
		t.Fatalf("reset: %v", err)
	}

	// 验证内容已回滚
	content, _ = os.ReadFile(testFile)
	if string(content) != "first" {
		t.Errorf("expected 'first' after reset, got %s", string(content))
	}
}

func TestGitManagerMapping(t *testing.T) {
	manager := NewGitManager(".")

	mapping := &SnapshotMapping{
		SnapshotID: "snap-001",
		GitHash:    "abc123",
		SessionID:  "session-001",
		MessageID:  "msg-001",
	}

	// 添加映射
	manager.AddMapping(mapping)

	// 通过快照ID获取
	retrieved := manager.GetMapping("snap-001")
	if retrieved == nil {
		t.Fatal("expected to get mapping")
	}
	if retrieved.GitHash != "abc123" {
		t.Errorf("expected git hash 'abc123', got %s", retrieved.GitHash)
	}

	// 通过Git哈希获取
	byHash := manager.GetMappingByGitHash("abc123")
	if byHash == nil {
		t.Fatal("expected to get mapping by git hash")
	}
	if byHash.SnapshotID != "snap-001" {
		t.Errorf("expected snapshot ID 'snap-001', got %s", byHash.SnapshotID)
	}

	// 获取不存在的映射
	notFound := manager.GetMapping("nonexistent")
	if notFound != nil {
		t.Error("expected nil for nonexistent mapping")
	}
}

func TestGitManagerBranch(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "git_test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewGitManager(tmpDir)
	ctx := context.Background()

	// 初始化并配置
	manager.Init(ctx)
	manager.execGit(ctx, "config", "user.email", "test@test.com")
	manager.execGit(ctx, "config", "user.name", "Test User")

	// 创建初始提交
	testFile := filepath.Join(tmpDir, "test.txt")
	os.WriteFile(testFile, []byte("initial"), 0644)
	manager.Commit(ctx, "Initial")

	// 创建分支
	if err := manager.CreateBranch(ctx, "feature"); err != nil {
		t.Fatalf("create branch: %v", err)
	}

	// 列出分支
	branches, err := manager.ListBranches(ctx)
	if err != nil {
		t.Fatalf("list branches: %v", err)
	}

	if len(branches) < 2 {
		t.Errorf("expected at least 2 branches, got %d", len(branches))
	}

	// 切换分支
	if err := manager.SwitchBranch(ctx, "feature"); err != nil {
		t.Fatalf("switch branch: %v", err)
	}

	// 切换回主分支
	manager.SwitchBranch(ctx, "master")

	// 删除分支
	if err := manager.DeleteBranch(ctx, "feature"); err != nil {
		t.Fatalf("delete branch: %v", err)
	}
}

func TestGitManagerDiffBetween(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "git_test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewGitManager(tmpDir)
	ctx := context.Background()

	// 初始化并配置
	manager.Init(ctx)
	manager.execGit(ctx, "config", "user.email", "test@test.com")
	manager.execGit(ctx, "config", "user.name", "Test User")

	testFile := filepath.Join(tmpDir, "test.txt")

	// 第一次提交
	os.WriteFile(testFile, []byte("first"), 0644)
	commit1, _ := manager.Commit(ctx, "First")

	// 第二次提交（修改文件）
	os.WriteFile(testFile, []byte("second"), 0644)
	commit2, _ := manager.Commit(ctx, "Second")

	// 添加新文件
	newFile := filepath.Join(tmpDir, "new.txt")
	os.WriteFile(newFile, []byte("new"), 0644)
	commit3, _ := manager.Commit(ctx, "Third")

	// 比较第一次和第三次提交
	diffs, err := manager.DiffBetween(ctx, commit1.Hash, commit3.Hash)
	if err != nil {
		t.Fatalf("diff between: %v", err)
	}

	if len(diffs) < 1 {
		t.Errorf("expected at least 1 diff, got %d", len(diffs))
	}

	// 比较第二次和第三次提交
	diffs, err = manager.DiffBetween(ctx, commit2.Hash, commit3.Hash)
	if err != nil {
		t.Fatalf("diff between: %v", err)
	}

	// 应该只有新增文件
	found := false
	for _, d := range diffs {
		if d.File == "new.txt" && d.Status == DiffAdded {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find new.txt as added")
	}
}
