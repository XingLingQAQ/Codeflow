package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codeflow/backend/internal/policy"
	"github.com/codeflow/backend/internal/policy/policytesting"
)

// 本文件的 -z 字节 fixture 按 git 2.55.0.windows.4 实际输出逐字节核对：
//   status --porcelain=v1 -z:  ` M sub/普通文件.txt\0`、`R  new name.txt\0old name.txt\0`
//   diff --name-status -z:     `R100\0old name.txt\0new name.txt\0`、`R079\0alpha.txt\0beta.txt\0`
// 两种 rename 的路径顺序不同：status -z 为新路径在前，diff -z 为源路径在前。

func TestParseStatusPorcelainZ(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []PorcelainStatusEntry
	}{
		{
			name:  "empty output",
			input: "",
			want:  []PorcelainStatusEntry{},
		},
		{
			// E-14 回归：首行未暂存修改，-z 保留前导空格，不得丢首字符
			name:  "unstaged modified keeps full filename",
			input: " M foo.go\x00",
			want: []PorcelainStatusEntry{
				{IndexStatus: ' ', WorktreeStatus: 'M', Path: "foo.go"},
			},
		},
		{
			name:  "path with spaces",
			input: " M my file.go\x00",
			want: []PorcelainStatusEntry{
				{IndexStatus: ' ', WorktreeStatus: 'M', Path: "my file.go"},
			},
		},
		{
			name:  "unicode path",
			input: " M sub/普通文件.txt\x00",
			want: []PorcelainStatusEntry{
				{IndexStatus: ' ', WorktreeStatus: 'M', Path: "sub/普通文件.txt"},
			},
		},
		{
			name:  "untracked",
			input: "?? un tracked.md\x00",
			want: []PorcelainStatusEntry{
				{IndexStatus: '?', WorktreeStatus: '?', Path: "un tracked.md"},
			},
		},
		{
			// status -z rename：新路径在前、源路径在后（与文本 old -> new 相反）
			name:  "staged rename new path first",
			input: "R  new name.txt\x00old name.txt\x00",
			want: []PorcelainStatusEntry{
				{IndexStatus: 'R', WorktreeStatus: ' ', Path: "new name.txt", OldPath: "old name.txt"},
			},
		},
		{
			name:  "multiple records mixed",
			input: " M sub/普通文件.txt\x00?? un tracked.md\x00R  new name.txt\x00old name.txt\x00",
			want: []PorcelainStatusEntry{
				{IndexStatus: ' ', WorktreeStatus: 'M', Path: "sub/普通文件.txt"},
				{IndexStatus: '?', WorktreeStatus: '?', Path: "un tracked.md"},
				{IndexStatus: 'R', WorktreeStatus: ' ', Path: "new name.txt", OldPath: "old name.txt"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseStatusPorcelainZ([]byte(tc.input))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("expected %d entries, got %d (%+v)", len(tc.want), len(got), got)
			}
			for i, want := range tc.want {
				if got[i] != want {
					t.Errorf("entry %d: expected %+v, got %+v", i, want, got[i])
				}
			}
		})
	}
}

func TestParseStatusPorcelainZErrors(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"truncated record without NUL", " M foo.go"},
		{"empty path", " M \x00"},
		{"status header too short", "M\x00"},
		{"missing space after XY", " Mx\x00"},
		{"rename missing source path", "R  new name.txt\x00"},
		{"rename empty source path", "R  new name.txt\x00\x00"},
		{"truncated inside rename pair", " M a.txt\x00R  new.txt"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseStatusPorcelainZ([]byte(tc.input))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, ErrMalformedPorcelain) {
				t.Errorf("expected ErrMalformedPorcelain, got %v", err)
			}
		})
	}
}

func TestParseNameStatusZ(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []NameStatusEntry
	}{
		{
			name:  "empty output",
			input: "",
			want:  []NameStatusEntry{},
		},
		{
			name:  "modified",
			input: "M\x00foo.go\x00",
			want: []NameStatusEntry{
				{Status: 'M', Score: -1, Path: "foo.go"},
			},
		},
		{
			name:  "added and deleted with spaces and unicode",
			input: "A\x00new file.txt\x00D\x00sub/旧 文件.txt\x00",
			want: []NameStatusEntry{
				{Status: 'A', Score: -1, Path: "new file.txt"},
				{Status: 'D', Score: -1, Path: "sub/旧 文件.txt"},
			},
		},
		{
			// diff -z rename：源路径在前、新路径在后（与 status -z 相反），R100 纯重命名
			name:  "rename with score 100 old path first",
			input: "R100\x00old name.txt\x00new name.txt\x00",
			want: []NameStatusEntry{
				{Status: 'R', Score: 100, Path: "new name.txt", OldPath: "old name.txt"},
			},
		},
		{
			name:  "rename with partial similarity score",
			input: "R079\x00alpha.txt\x00beta.txt\x00",
			want: []NameStatusEntry{
				{Status: 'R', Score: 79, Path: "beta.txt", OldPath: "alpha.txt"},
			},
		},
		{
			name:  "copy with score",
			input: "C100\x00beta.txt\x00gamma.txt\x00",
			want: []NameStatusEntry{
				{Status: 'C', Score: 100, Path: "gamma.txt", OldPath: "beta.txt"},
			},
		},
		{
			name:  "rename mixed with plain records",
			input: "M\x00main.go\x00R100\x00old name.txt\x00new name.txt\x00A\x00doc.md\x00",
			want: []NameStatusEntry{
				{Status: 'M', Score: -1, Path: "main.go"},
				{Status: 'R', Score: 100, Path: "new name.txt", OldPath: "old name.txt"},
				{Status: 'A', Score: -1, Path: "doc.md"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseNameStatusZ([]byte(tc.input))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("expected %d entries, got %d (%+v)", len(tc.want), len(got), got)
			}
			for i, want := range tc.want {
				if got[i] != want {
					t.Errorf("entry %d: expected %+v, got %+v", i, want, got[i])
				}
			}
		})
	}
}

func TestParseNameStatusZErrors(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"truncated record without NUL", "M\x00foo.go"},
		{"empty path", "M\x00\x00"},
		{"empty status token", "\x00foo.go\x00"},
		{"truncated missing path", "M\x00"},
		{"score on non-rename status", "M42\x00foo.go\x00"},
		{"rename missing score", "R\x00a.txt\x00b.txt\x00"},
		{"rename non-numeric score", "R9x\x00a.txt\x00b.txt\x00"},
		{"rename score above 100", "R101\x00a.txt\x00b.txt\x00"},
		{"rename truncated to one path", "R100\x00old name.txt\x00"},
		{"rename truncated to zero paths", "R100\x00"},
		{"rename empty target path", "R100\x00old.txt\x00\x00"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseNameStatusZ([]byte(tc.input))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, ErrMalformedPorcelain) {
				t.Errorf("expected ErrMalformedPorcelain, got %v", err)
			}
		})
	}
}

// initTestRepo 在 t.TempDir() 自建仓库（只触碰测试自己的目录），返回已配置的 manager。
func initTestRepo(t *testing.T) (*GitManager, string, context.Context) {
	t.Helper()
	dir := t.TempDir()
	manager := NewGitManager(dir)
	ctx := context.Background()
	if err := manager.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := manager.execGit(ctx, "config", "user.email", "test@test.com"); err != nil {
		t.Fatalf("config email: %v", err)
	}
	if _, err := manager.execGit(ctx, "config", "user.name", "Test User"); err != nil {
		t.Fatalf("config name: %v", err)
	}
	return manager, dir, ctx
}

func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// TestExecGitRawUntrimmedStdoutSeparatedStderr 验证 raw helper：
// stdout 保留原始字节（含尾部换行，不做 TrimSpace），stderr 与 stdout 分离。
func TestExecGitRawUntrimmedStdoutSeparatedStderr(t *testing.T) {
	policytesting.AllowForTest(t, policy.OperationProcessStart)
	manager, dir, ctx := initTestRepo(t)
	writeTestFile(t, dir, "test.txt", "hello")
	if _, err := manager.Commit(ctx, "init"); err != nil {
		t.Fatalf("commit: %v", err)
	}

	stdout, stderr, err := manager.execGitRaw(ctx, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("execGitRaw rev-parse: %v", err)
	}
	if len(stderr) != 0 {
		t.Errorf("expected empty stderr, got %q", stderr)
	}
	if len(stdout) == 0 || stdout[len(stdout)-1] != '\n' {
		t.Errorf("expected raw stdout to keep trailing newline, got %q", stdout)
	}
	trimmed, err := manager.execGit(ctx, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("execGit rev-parse: %v", err)
	}
	if strings.TrimSpace(string(stdout)) != trimmed {
		t.Errorf("raw stdout trimmed should equal execGit output: raw=%q trimmed=%q", stdout, trimmed)
	}

	// 失败命令：stderr 独立携带错误文本，不与 stdout 混合
	stdout, stderr, err = manager.execGitRaw(ctx, "rev-parse", "--verify", "refs/heads/nonexistent-t13")
	if err == nil {
		t.Fatal("expected error for nonexistent ref")
	}
	if len(stderr) == 0 {
		t.Error("expected non-empty stderr on failure")
	}
	if len(stdout) != 0 {
		t.Errorf("expected empty stdout on failure, got %q", stdout)
	}
}

// TestStatusPorcelainZEndToEnd 用 TempDir 真实仓库生成 status -z 输出做端到端对照：
// 首个未暂存文件名完整（E-14 首字符回归）、空格/中文路径精确、staged rename 双路径。
func TestStatusPorcelainZEndToEnd(t *testing.T) {
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

	stdout, _, err := manager.execGitRaw(ctx, "status", "--porcelain=v1", "-z")
	if err != nil {
		t.Fatalf("status -z: %v", err)
	}
	entries, err := ParseStatusPorcelainZ(stdout)
	if err != nil {
		t.Fatalf("parse status -z: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d (%+v)", len(entries), entries)
	}
	// git 输出按路径排序，普通文件.txt 的多字节首字符必须完整
	if entries[0].Path != "sub/普通文件.txt" || entries[0].IndexStatus != ' ' || entries[0].WorktreeStatus != 'M' {
		t.Errorf("unexpected first entry: %+v", entries[0])
	}
	if entries[1].Path != "un tracked.md" || entries[1].IndexStatus != '?' {
		t.Errorf("unexpected second entry: %+v", entries[1])
	}

	// staged rename：新路径在前、源路径在后
	if _, err := manager.execGit(ctx, "mv", "old name.txt", "new name.txt"); err != nil {
		t.Fatalf("git mv: %v", err)
	}
	stdout, _, err = manager.execGitRaw(ctx, "status", "--porcelain=v1", "-z")
	if err != nil {
		t.Fatalf("status -z after rename: %v", err)
	}
	entries, err = ParseStatusPorcelainZ(stdout)
	if err != nil {
		t.Fatalf("parse status -z after rename: %v", err)
	}
	var rename *PorcelainStatusEntry
	for i := range entries {
		if entries[i].IndexStatus == 'R' {
			rename = &entries[i]
		}
	}
	if rename == nil {
		t.Fatalf("expected a rename entry, got %+v", entries)
	}
	if rename.Path != "new name.txt" || rename.OldPath != "old name.txt" {
		t.Errorf("rename paths: expected new name.txt <- old name.txt, got %+v", rename)
	}
}

// TestNameStatusZEndToEnd 用 TempDir 真实仓库验证 diff -z：
// 纯 rename 得 R100、rename+edit 得部分相似度分数，双路径精确。
func TestNameStatusZEndToEnd(t *testing.T) {
	policytesting.AllowForTest(t, policy.OperationProcessStart)
	manager, dir, ctx := initTestRepo(t)
	writeTestFile(t, dir, "alpha.txt", "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\n")
	if _, err := manager.Commit(ctx, "init"); err != nil {
		t.Fatalf("commit init: %v", err)
	}
	c1, err := manager.GetCurrentHash(ctx)
	if err != nil {
		t.Fatalf("hash c1: %v", err)
	}

	// 纯 rename
	if _, err := manager.execGit(ctx, "mv", "alpha.txt", "beta.txt"); err != nil {
		t.Fatalf("git mv: %v", err)
	}
	if _, err := manager.Commit(ctx, "rename"); err != nil {
		t.Fatalf("commit rename: %v", err)
	}
	c2, err := manager.GetCurrentHash(ctx)
	if err != nil {
		t.Fatalf("hash c2: %v", err)
	}

	stdout, _, err := manager.execGitRaw(ctx, "diff", "--name-status", "-z", "-M", c1, c2)
	if err != nil {
		t.Fatalf("diff -z pure rename: %v", err)
	}
	entries, err := ParseNameStatusZ(stdout)
	if err != nil {
		t.Fatalf("parse pure rename: %v", err)
	}
	if len(entries) != 1 || entries[0].Status != 'R' || entries[0].Score != 100 {
		t.Fatalf("expected single R100 entry, got %+v", entries)
	}
	if entries[0].OldPath != "alpha.txt" || entries[0].Path != "beta.txt" {
		t.Errorf("rename paths: expected alpha.txt -> beta.txt, got %+v", entries[0])
	}

	// rename + 小编辑：部分相似度分数
	if _, err := manager.execGit(ctx, "mv", "beta.txt", "gamma.txt"); err != nil {
		t.Fatalf("git mv 2: %v", err)
	}
	writeTestFile(t, dir, "gamma.txt", "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9 CHANGED\nline10\n")
	if _, err := manager.Commit(ctx, "rename edit"); err != nil {
		t.Fatalf("commit rename edit: %v", err)
	}
	c3, err := manager.GetCurrentHash(ctx)
	if err != nil {
		t.Fatalf("hash c3: %v", err)
	}

	stdout, _, err = manager.execGitRaw(ctx, "diff", "--name-status", "-z", "-M", c2, c3)
	if err != nil {
		t.Fatalf("diff -z rename edit: %v", err)
	}
	entries, err = ParseNameStatusZ(stdout)
	if err != nil {
		t.Fatalf("parse rename edit: %v", err)
	}
	if len(entries) != 1 || entries[0].Status != 'R' {
		t.Fatalf("expected single rename entry, got %+v", entries)
	}
	if entries[0].Score < 0 || entries[0].Score >= 100 {
		t.Errorf("expected partial similarity score, got %d", entries[0].Score)
	}
	if entries[0].OldPath != "beta.txt" || entries[0].Path != "gamma.txt" {
		t.Errorf("rename paths: expected beta.txt -> gamma.txt, got %+v", entries[0])
	}
}

// TestGitNulParserRejectsTruncatedRecord（§28 T13.05.c 点名）聚焦半截记录：
// 输出不以 NUL 结尾、rename/copy 对缺源/目标路径、路径字段缺失——两个 parser
// 都必须拒绝并返回包装 ErrMalformedPorcelain 的错误，且不得按部分结果放行。
// 与 TestParseStatusPorcelainZErrors/TestParseNameStatusZErrors 的完整错误表互补：
// 本用例只锁定「半截记录」这一类，既有错误表逐字保留。
func TestGitNulParserRejectsTruncatedRecord(t *testing.T) {
	statusInputs := []struct {
		name  string
		input string
	}{
		{"no trailing NUL", " M foo.go"},
		{"truncated inside rename pair", " M a.txt\x00R  new.txt"},
		{"rename missing source path", "R  new.txt\x00"},
	}
	for _, tc := range statusInputs {
		t.Run("status/"+tc.name, func(t *testing.T) {
			got, err := ParseStatusPorcelainZ([]byte(tc.input))
			if err == nil {
				t.Fatalf("expected error, got nil (entries=%+v)", got)
			}
			if !errors.Is(err, ErrMalformedPorcelain) {
				t.Errorf("expected ErrMalformedPorcelain, got %v", err)
			}
			if got != nil {
				t.Errorf("must not return partial entries, got %+v", got)
			}
		})
	}

	nameStatusInputs := []struct {
		name  string
		input string
	}{
		{"no trailing NUL", "M\x00foo.go"},
		{"truncated missing path", "M\x00"},
		{"rename truncated to one path", "R100\x00old name.txt\x00"},
		{"rename truncated to zero paths", "R100\x00"},
	}
	for _, tc := range nameStatusInputs {
		t.Run("name-status/"+tc.name, func(t *testing.T) {
			got, err := ParseNameStatusZ([]byte(tc.input))
			if err == nil {
				t.Fatalf("expected error, got nil (entries=%+v)", got)
			}
			if !errors.Is(err, ErrMalformedPorcelain) {
				t.Errorf("expected ErrMalformedPorcelain, got %v", err)
			}
			if got != nil {
				t.Errorf("must not return partial entries, got %+v", got)
			}
		})
	}
}
