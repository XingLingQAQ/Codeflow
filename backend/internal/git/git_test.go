package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/policy"
	"github.com/codeflow/backend/internal/policy/policytesting"
)

func TestGitManagerInit(t *testing.T) {
	policytesting.AllowForTest(t, policy.OperationProcessStart)
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
	policytesting.AllowForTest(t, policy.OperationProcessStart)
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
	policytesting.AllowForTest(t, policy.OperationProcessStart)
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
	policytesting.AllowForTest(t, policy.OperationProcessStart)
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
	policytesting.AllowForTest(t, policy.OperationProcessStart)
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
	policytesting.AllowForTest(t, policy.OperationProcessStart)
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
	policytesting.AllowForTest(t, policy.OperationProcessStart)
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

// TestGitStatusFirstUnstagedFilenameIntact（§28 T13.05.c 点名；由 b 步
// TestGitManagerStatusWiredToPorcelainZ 原断言重命名合并，逐字保留）验证 Status
// 生产路径已接 -z 原始输出 + NUL 解析器（E-14）：首个未暂存文件名不丢首字符、
// 空格/中文路径完整、staged rename File 指向新路径且 OldPath 保留来源；
// 非 rename 条目不携带 old_path/score。
func TestGitStatusFirstUnstagedFilenameIntact(t *testing.T) {
	policytesting.AllowForTest(t, policy.OperationProcessStart)
	manager, dir, ctx := initTestRepo(t)
	writeTestFile(t, dir, "old name.txt", "hello\n")
	writeTestFile(t, dir, "sub/普通文件.txt", "world\n")
	if _, err := manager.Commit(ctx, "init"); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// 未暂存修改（状态输出首条记录）+ 未跟踪空格文件名
	writeTestFile(t, dir, "sub/普通文件.txt", "world\nchanged\n")
	writeTestFile(t, dir, "un tracked.md", "u\n")

	status, err := manager.Status(ctx)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(status) != 2 {
		t.Fatalf("expected 2 entries, got %d (%+v)", len(status), status)
	}
	// git 输出按路径排序：普通文件.txt 的多字节首字符必须完整（旧文本解析在此丢字符）
	if status[0].File != "sub/普通文件.txt" || status[0].Status != DiffModified {
		t.Errorf("unexpected first entry: %+v", status[0])
	}
	if status[0].OldPath != "" || status[0].Score != nil {
		t.Errorf("non-rename entry must not carry old_path/score: %+v", status[0])
	}
	if status[1].File != "un tracked.md" || status[1].Status != DiffAdded {
		t.Errorf("unexpected second entry: %+v", status[1])
	}

	// staged rename：File=新路径，OldPath=源路径
	if _, err := manager.execGit(ctx, "mv", "old name.txt", "new name.txt"); err != nil {
		t.Fatalf("git mv: %v", err)
	}
	status, err = manager.Status(ctx)
	if err != nil {
		t.Fatalf("status after rename: %v", err)
	}
	var rename *GitDiff
	for i := range status {
		if status[i].Status == DiffRenamed {
			rename = &status[i]
		}
	}
	if rename == nil {
		t.Fatalf("expected a rename entry, got %+v", status)
	}
	if rename.File != "new name.txt" || rename.OldPath != "old name.txt" {
		t.Errorf("rename: expected File=new name.txt OldPath=old name.txt, got %+v", rename)
	}
}

// TestGitDiffRenameScoreAndBothPaths（§28 T13.05.c 点名；由 b 步
// TestGitManagerDiffBetweenWiredToNameStatusZ 原断言重命名合并，逐字保留）验证
// DiffBetween 生产路径已接 `diff --name-status -z -M` + NUL 解析器（E-13/E-14）：
// 纯 rename 得 R100 双路径与分数、空格/中文路径精确；File 指向新路径，OldPath 保留来源。
func TestGitDiffRenameScoreAndBothPaths(t *testing.T) {
	policytesting.AllowForTest(t, policy.OperationProcessStart)
	manager, dir, ctx := initTestRepo(t)
	writeTestFile(t, dir, "old name.txt", "line1\nline2\nline3\n")
	if _, err := manager.Commit(ctx, "init"); err != nil {
		t.Fatalf("commit init: %v", err)
	}
	c1, err := manager.GetCurrentHash(ctx)
	if err != nil {
		t.Fatalf("hash c1: %v", err)
	}

	// 纯 rename + 新增「空格+中文」路径文件
	if _, err := manager.execGit(ctx, "mv", "old name.txt", "new name.txt"); err != nil {
		t.Fatalf("git mv: %v", err)
	}
	writeTestFile(t, dir, "sub/普通 文件.txt", "u\n")
	if _, err := manager.Commit(ctx, "rename and add"); err != nil {
		t.Fatalf("commit rename: %v", err)
	}
	c2, err := manager.GetCurrentHash(ctx)
	if err != nil {
		t.Fatalf("hash c2: %v", err)
	}

	diffs, err := manager.DiffBetween(ctx, c1, c2)
	if err != nil {
		t.Fatalf("diff between: %v", err)
	}
	if len(diffs) != 2 {
		t.Fatalf("expected 2 diffs, got %d (%+v)", len(diffs), diffs)
	}

	var rename, added *GitDiff
	for i := range diffs {
		switch diffs[i].Status {
		case DiffRenamed:
			rename = &diffs[i]
		case DiffAdded:
			added = &diffs[i]
		}
	}
	if rename == nil {
		t.Fatalf("expected a rename diff, got %+v", diffs)
	}
	if rename.File != "new name.txt" || rename.OldPath != "old name.txt" {
		t.Errorf("rename paths: expected File=new name.txt OldPath=old name.txt, got %+v", rename)
	}
	if rename.Score == nil || *rename.Score != 100 {
		t.Errorf("expected pure rename score 100, got %+v", rename.Score)
	}
	if added == nil {
		t.Fatalf("expected an added diff, got %+v", diffs)
	}
	if added.File != "sub/普通 文件.txt" {
		t.Errorf("added path with spaces and unicode must be exact, got %q", added.File)
	}
	if added.OldPath != "" || added.Score != nil {
		t.Errorf("non-rename entry must not carry old_path/score: %+v", added)
	}
}

// TestGitFilenameSpacesAndUnicode（§28 T13.05.c 点名）分两层：
//  1. Windows 实测：t.TempDir() 真实仓库（仅测试自己的目录），路径组件同时含
//     合法空格与中文（含空格目录 + 中文文件名），经生产 Status 与 DiffBetween
//     往返后必须逐字节精确。
//  2. 换行文件名：Windows（NTFS/Win32 API 及 Git Bash）无法创建含 \n 的文件名，
//     §28 要求的真实仓库换行用例仅 Linux 可实测——本机（Windows）未执行该实测；
//     此处用纯 parser 字节 fixture 证明 -z 下 \n 字节级保真（含 rename 双路径），
//     平台限制以此注释注明。
func TestGitFilenameSpacesAndUnicode(t *testing.T) {
	policytesting.AllowForTest(t, policy.OperationProcessStart)
	t.Run("real repo space and unicode paths byte exact", func(t *testing.T) {
		manager, dir, ctx := initTestRepo(t)

		// 先让空格目录成为已跟踪目录：porcelain 会把全新未跟踪目录折叠为
		// "my dir/" 单条目录条目（git 原生语义，非解析缺陷），只有目录内已有
		// 跟踪文件时未跟踪文件才按完整路径列出。
		writeTestFile(t, dir, "my dir/占位.txt", "keep\n")
		if _, err := manager.Commit(ctx, "seed tracked dir"); err != nil {
			t.Fatalf("seed commit: %v", err)
		}

		// 未跟踪：单组件同时含空格+中文、空格目录 + 中文文件名
		writeTestFile(t, dir, "空 格 文件.txt", "v1\n")
		writeTestFile(t, dir, "my dir/中文 文件.md", "m1\n")

		status, err := manager.Status(ctx)
		if err != nil {
			t.Fatalf("status untracked: %v", err)
		}
		if len(status) != 2 {
			t.Fatalf("expected 2 entries, got %d (%+v)", len(status), status)
		}
		for _, d := range status {
			if d.File != "my dir/中文 文件.md" && d.File != "空 格 文件.txt" {
				t.Errorf("untracked space/unicode path must be byte-exact, got %q", d.File)
			}
			if d.Status != DiffAdded {
				t.Errorf("expected untracked as added, got %+v", d)
			}
		}

		c1, err := manager.Commit(ctx, "add space and unicode paths")
		if err != nil {
			t.Fatalf("commit: %v", err)
		}

		// 未暂存修改：生产 Status 必须逐字节保留路径
		writeTestFile(t, dir, "空 格 文件.txt", "v1\nv2\n")
		writeTestFile(t, dir, "my dir/中文 文件.md", "m1\nm2\n")
		status, err = manager.Status(ctx)
		if err != nil {
			t.Fatalf("status modified: %v", err)
		}
		if len(status) != 2 {
			t.Fatalf("expected 2 entries, got %d (%+v)", len(status), status)
		}
		for _, d := range status {
			if d.File != "my dir/中文 文件.md" && d.File != "空 格 文件.txt" {
				t.Errorf("modified space/unicode path must be byte-exact, got %q", d.File)
			}
			if d.Status != DiffModified {
				t.Errorf("expected modified, got %+v", d)
			}
		}

		// 提交后 DiffBetween 同样逐字节精确
		c2, err := manager.Commit(ctx, "modify space and unicode paths")
		if err != nil {
			t.Fatalf("commit 2: %v", err)
		}
		diffs, err := manager.DiffBetween(ctx, c1.Hash, c2.Hash)
		if err != nil {
			t.Fatalf("diff between: %v", err)
		}
		if len(diffs) != 2 {
			t.Fatalf("expected 2 diffs, got %d (%+v)", len(diffs), diffs)
		}
		for _, d := range diffs {
			if d.File != "my dir/中文 文件.md" && d.File != "空 格 文件.txt" {
				t.Errorf("diff space/unicode path must be byte-exact, got %q", d.File)
			}
			if d.Status != DiffModified {
				t.Errorf("expected modified, got %+v", d)
			}
			if d.OldPath != "" || d.Score != nil {
				t.Errorf("non-rename entry must not carry old_path/score: %+v", d)
			}
		}
	})

	t.Run("newline filename byte fixtures (Linux-only real repo, not executed here)", func(t *testing.T) {
		// 纯 parser 字节 fixture：-z 下 NUL 是唯一分隔符，路径中的 \n 必须原样保留；
		// 换行文件名的真实仓库用例按 §28 仅在 Linux 实测，Windows 本机不执行。
		statusEntries, err := ParseStatusPorcelainZ([]byte(" M dir/file\nname.txt\x00R  new\nfile.txt\x00old\nfile.txt\x00"))
		if err != nil {
			t.Fatalf("parse status fixture: %v", err)
		}
		if len(statusEntries) != 2 {
			t.Fatalf("expected 2 entries, got %+v", statusEntries)
		}
		if statusEntries[0].Path != "dir/file\nname.txt" {
			t.Errorf("newline in status path must be preserved byte-exact, got %q", statusEntries[0].Path)
		}
		if statusEntries[1].Path != "new\nfile.txt" || statusEntries[1].OldPath != "old\nfile.txt" {
			t.Errorf("status rename newline paths must be preserved, got %+v", statusEntries[1])
		}

		diffEntries, err := ParseNameStatusZ([]byte("M\x00dir/file\nname.txt\x00R100\x00old\nfile.txt\x00new\nfile.txt\x00"))
		if err != nil {
			t.Fatalf("parse name-status fixture: %v", err)
		}
		if len(diffEntries) != 2 {
			t.Fatalf("expected 2 entries, got %+v", diffEntries)
		}
		if diffEntries[0].Path != "dir/file\nname.txt" {
			t.Errorf("newline in diff path must be preserved byte-exact, got %q", diffEntries[0].Path)
		}
		if diffEntries[1].Status != 'R' || diffEntries[1].Score != 100 ||
			diffEntries[1].OldPath != "old\nfile.txt" || diffEntries[1].Path != "new\nfile.txt" {
			t.Errorf("diff rename newline paths must be preserved, got %+v", diffEntries[1])
		}
	})
}
