//go:build claudecli_probe

// 真实 CLI 只读探测（T1.13.a 的本机证据）。默认构建不编译本文件，因此常规测试里没有
// 需要跳过的项。
//
// 本文件只执行 `claude --version` 与 `claude --help` 两条只读命令：不发模型请求、不登录、
// 不安装、不改任何文件。用 `-tags claudecli_probe` 显式开启：
//
//	CGO_ENABLED=0 go test -tags claudecli_probe -run TestRealCLIProbe \
//	  ./internal/execbackend/claudecode/ -count=1 -v
//
// 可执行文件来源：CODEFLOW_CLAUDE_CLI 环境变量（绝对路径），未设置时从 PATH 找 claude。
// 本文件不写入、也不打印任何机器特定路径以外的环境信息。
package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codeflow/backend/internal/execbackend"
)

// TestRealCLIProbe 对本机真实 CLI 跑一次只读探测，断言能力推导所依赖的事实齐全。
func TestRealCLIProbe(t *testing.T) {
	configured := strings.TrimSpace(os.Getenv("CODEFLOW_CLAUDE_CLI"))
	p := Prober{}
	result, err := p.Probe(context.Background(), configured, execbackend.AllCapabilities())
	if err != nil {
		t.Fatalf("Probe(real CLI) returned error: %v", err)
	}

	t.Logf("executable: %s", result.ExecutablePath)
	t.Logf("version: %q", result.Version.String())
	t.Logf("problems: %v", result.Problems)
	t.Logf("report: backend=%s version=%s source=%q",
		result.Report.Backend, result.Report.Version, result.Report.Source)
	for _, c := range execbackend.AllCapabilities() {
		t.Logf("capability %-18s proven=%v evidence=%q", c, result.Report.Has(c), result.Report.EvidenceFor(c))
	}

	if configured != "" && !filepath.IsAbs(configured) {
		t.Fatalf("CODEFLOW_CLAUDE_CLI must be an absolute path")
	}
	if result.ExecutablePath == "" {
		t.Fatalf("real CLI not found: %v", result.Problems)
	}
	if !filepath.IsAbs(result.ExecutablePath) {
		t.Fatalf("ExecutablePath %q must be absolute", result.ExecutablePath)
	}
	if !result.Version.Valid() {
		t.Fatalf("real CLI version must be parsable: %v", result.Problems)
	}
	if !result.Version.AtLeast(minSupportedVersion) {
		t.Fatalf("real CLI version %s is below MinSupportedVersion %s",
			result.Version.String(), MinSupportedVersion)
	}
	if result.Report.Backend != BackendName {
		t.Fatalf("Backend = %q, want %q", result.Report.Backend, BackendName)
	}

	// 闸门与入口所依赖的 help 事实必须齐全。
	for _, flag := range []string{
		"-p", "--print", "--output-format", "--verbose", "--restricted",
		"--permission-mode", "--permission-prompts", "--settings", "--strict-mcp-config",
		"--tools", "--session-id", "--no-session-persistence", "--include-hook-events",
		"--model", "--add-dir", "--mcp-config", "--allowedTools", "--allowed-tools",
	} {
		if !result.Flags.Has(flag) {
			t.Errorf("real CLI help is missing %s", flag)
		}
	}
	if !result.Flags.HasChoice("--permission-mode", "dontAsk") {
		t.Error("real CLI help: --permission-mode must offer dontAsk")
	}
	if !result.Flags.HasChoice("--permission-prompts", "none") {
		t.Error("real CLI help: --permission-prompts must offer none")
	}
	if !result.Flags.HasChoice("--output-format", "stream-json") {
		t.Error("real CLI help: --output-format must offer stream-json")
	}
	if len(result.Problems) != 0 {
		t.Fatalf("real CLI probe must be problem-free, got %v", result.Problems)
	}

	// 可从只读事实证明的能力必须为真；其余能力必须保持 false。
	for _, c := range derivableCapabilities() {
		if !result.Report.Has(c) {
			t.Errorf("real CLI must prove %s", c)
		}
	}
	for _, c := range alwaysFalseCapabilities() {
		if result.Report.Has(c) {
			t.Errorf("real CLI must not prove %s in T1.13.a", c)
		}
	}

	// 启动参数必须能真正构造出来，且每个 flag 都在真实 help 里。
	spec := LaunchSpec{
		Executable:   result.ExecutablePath,
		SettingsPath: filepath.Join(t.TempDir(), "settings.json"),
		Tools:        AllowedTools(),
		SessionID:    "3f2a1c4e-5b6d-4e8f-9a0b-1c2d3e4f5a6b",
	}
	if err := spec.Validate(); err != nil {
		t.Fatalf("LaunchSpec.Validate: %v", err)
	}
	args := spec.Args()
	t.Logf("argv: %v", args)
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") && !result.Flags.Has(arg) {
			t.Errorf("argv flag %s is not in the real CLI help", arg)
		}
	}
}
