package claudecode

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/codeflow/backend/internal/execbackend"
)

// 只读探测的默认参数。
const (
	// DefaultProbeTimeout 单次探测命令的超时（--version 与 --help 各自计时）。
	DefaultProbeTimeout = 15 * time.Second
	// DefaultProbeOutputLimit 单次探测命令 stdout 的上限；超出记 probe_output_too_large。
	DefaultProbeOutputLimit = 1 << 20
	// probeWaitDelay 超时后等待子进程真正退出的宽限期（exec.Cmd.WaitDelay）。
	probeWaitDelay = 2 * time.Second
	// nonEssentialTrafficEnv 关闭自动更新/遥测/错误上报等非必要流量
	// （https://code.claude.com/docs/en/env-vars.md 的 CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC）。
	nonEssentialTrafficEnv = "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC"
	// probeCommand 是定位可执行文件时查询的命令名。
	probeCommand = "claude"
)

// ErrProbeOutputTooLarge 表示探测命令的 stdout 超过上限（Runner 的约定返回值）。
var ErrProbeOutputTooLarge = errors.New("claudecode: probe output exceeds limit")

// ProbeError 是探测子进程失败的原因，只携带参数名与退出码。
//
// 默认 Runner 不回显也不保留 stderr 正文：Detail 只写退出码（§27.7 要求不回显凭据，
// stderr 里可能带环境或凭据片段）。Err 字段留给调用方注入底层原因用于日志，本包的
// Problem.Detail 永不使用 Err.Error()。
type ProbeError struct {
	// Arg 探测参数（"--version" 或 "--help"）。
	Arg string
	// ExitCode 退出码；-1 表示进程没能启动或被超时终止。
	ExitCode int
	// Err 可选的底层原因，仅供上层日志；不得进入面向用户的问题详情。
	Err error
}

// Error 实现 error；nil 接收者安全。
func (e *ProbeError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.ExitCode >= 0 {
		return fmt.Sprintf("claudecode: probe `%s %s` exited with code %d", probeCommand, e.Arg, e.ExitCode)
	}
	return fmt.Sprintf("claudecode: probe `%s %s` could not be started", probeCommand, e.Arg)
}

// Unwrap 暴露底层原因，供 errors.Is/As 使用；nil 接收者返回 nil。
func (e *ProbeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// ResolveError 是定位可执行文件失败的领域错误；Problem 给出问题码与脱敏详情。
type ResolveError struct {
	Problem Problem
}

// Error 实现 error；nil 接收者安全。
func (e *ResolveError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return e.Problem.String()
}

// ResolveExecutable 定位 claude 可执行文件，返回绝对路径。
//
// 规则：
//   - configured 非空时必须已经是绝对路径，且指向存在的常规文件；否则分别返回
//     cli_path_not_absolute / cli_not_found（不回显路径，避免把环境信息带进问题详情）。
//   - configured 为空时用 lookPath("claude")（nil 用 exec.LookPath）再转成绝对路径；
//     找不到返回 cli_not_found。
func ResolveExecutable(configured string, lookPath func(string) (string, error)) (string, error) {
	if trimSpaces(configured) != "" {
		if !filepath.IsAbs(configured) {
			return "", &ResolveError{Problem: Problem{
				Code:   ProblemCLIPathNotAbsolute,
				Detail: "configured claude executable path must be absolute",
			}}
		}
		info, err := os.Stat(configured)
		if err != nil {
			return "", &ResolveError{Problem: Problem{
				Code:   ProblemCLINotFound,
				Detail: "configured claude executable does not exist",
			}}
		}
		if info.IsDir() || !info.Mode().IsRegular() {
			return "", &ResolveError{Problem: Problem{
				Code:   ProblemCLINotFound,
				Detail: "configured claude executable is not a regular file",
			}}
		}
		return filepath.Clean(configured), nil
	}
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	found, err := lookPath(probeCommand)
	if err != nil || trimSpaces(found) == "" {
		return "", &ResolveError{Problem: Problem{
			Code:   ProblemCLINotFound,
			Detail: "`claude` was not found on PATH",
		}}
	}
	abs, err := filepath.Abs(found)
	if err != nil {
		return "", &ResolveError{Problem: Problem{
			Code:   ProblemCLINotFound,
			Detail: "resolved `claude` path could not be made absolute",
		}}
	}
	return filepath.Clean(abs), nil
}

// ---------------------------------------------------------------------------
// 子进程环境
// ---------------------------------------------------------------------------

// probeEnvAllowlist 返回各平台的变量白名单（key 已大写）。
//
// 只传定位/启动可执行文件与常规临时目录所需的变量：探测命令不需要任何凭据，凭据变量
// 一律不传（测试断言 ANTHROPIC_* / AWS_* / CLAUDE_CODE_OAUTH_TOKEN 等不在结果里）。
func probeEnvAllowlist(goos string) map[string]bool {
	var names []string
	if goos == "windows" {
		names = []string{
			"SystemRoot", "SystemDrive", "windir", "PATH", "PATHEXT",
			"TEMP", "TMP", "USERPROFILE", "APPDATA", "LOCALAPPDATA",
			"HOMEDRIVE", "HOMEPATH", "ComSpec",
		}
	} else {
		names = []string{"PATH", "HOME", "TMPDIR", "LANG", "LC_ALL"}
	}
	allow := make(map[string]bool, len(names))
	for _, name := range names {
		allow[strings.ToUpper(name)] = true
	}
	return allow
}

// credentialEnvPrefixes 是即便将来误入白名单也必须拦掉的凭据变量前缀（防御性二次拦截）。
var credentialEnvPrefixes = []string{
	"ANTHROPIC_",
	"AWS_",
	"AZURE_",
	"GOOGLE_",
	"GCP_",
	"CLOUDFLARE_",
	"CLAUDE_CODE_OAUTH",
	"CLAUDE_CODE_API",
}

// ProbeEnv 返回探测子进程的环境：白名单变量 + CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1。
//
// 输入是父环境（KEY=VALUE 列表，通常来自 os.Environ()）。规则：
//   - 名字不在白名单 → 丢弃；名字命中凭据前缀 → 丢弃（双重保险）；
//   - 没有 "=" 的条目 → 丢弃；重复名字只保留第一个；
//   - Windows 的变量名大小写不敏感，因此按不区分大小写匹配。
func ProbeEnv(parent []string) []string { return probeEnvForOS(parent, runtime.GOOS) }

// probeEnvForOS 是 ProbeEnv 的平台参数化版本，便于测试固定平台行为。
func probeEnvForOS(parent []string, goos string) []string {
	allow := probeEnvAllowlist(goos)
	caseInsensitive := goos == "windows"
	out := make([]string, 0, len(allow)+1)
	seen := make(map[string]bool, len(allow))
	for _, kv := range parent {
		name, _, ok := strings.Cut(kv, "=")
		if !ok || name == "" {
			continue
		}
		key := name
		if caseInsensitive {
			key = strings.ToUpper(name)
		}
		if !allow[key] || isCredentialEnvName(key) {
			continue
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, kv)
	}
	return append(out, nonEssentialTrafficEnv+"=1")
}

// isCredentialEnvName 报告（已大写的）变量名是否像凭据。
func isCredentialEnvName(upper string) bool {
	for _, prefix := range credentialEnvPrefixes {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// 探测
// ---------------------------------------------------------------------------

// Runner 执行一次只读探测命令并返回 stdout。
//
// 约定：成功返回完整 stdout；失败返回错误，且错误里只允许带退出码（默认 Runner 即如此）。
// 需要模拟超时用 context.DeadlineExceeded，需要模拟输出超限用 ErrProbeOutputTooLarge。
type Runner func(ctx context.Context, exe string, args []string) (stdout []byte, err error)

// Clock 抽象 ProbedAt 的时间来源，便于测试注入固定时间。
type Clock interface {
	Now() time.Time
}

// SystemClock 是 Clock 的生产实现。
type SystemClock struct{}

// Now 实现 Clock。
func (SystemClock) Now() time.Time { return time.Now() }

// Prober 是一次只读探测的执行者；零值可用（Runner/LookPath/Clock 取默认值）。
type Prober struct {
	// Runner 执行探测命令；nil 用默认 os/exec Runner。
	Runner Runner
	// LookPath 定位可执行文件；nil 用 exec.LookPath。
	LookPath func(string) (string, error)
	// Clock 提供 ProbedAt；nil 用 SystemClock。
	Clock Clock
	// Timeout 单次探测超时；<= 0 用 DefaultProbeTimeout。
	Timeout time.Duration
	// MaxOutput 单次探测 stdout 上限；<= 0 用 DefaultProbeOutputLimit。
	MaxOutput int
	// Env 父环境；nil 用 os.Environ()。传给子进程前一律经 ProbeEnv 白名单过滤。
	Env []string
}

// ProbeResult 是一次探测的完整结果。
//
// 只要 ctx 没有被取消，Probe 一定返回 ProbeResult（可能带 Problems），error 为 nil：
// 探测失败是"能力未证明"的正常结果，不是调用错误。
type ProbeResult struct {
	// ExecutablePath 探测到的可执行文件绝对路径；定位失败时为空。
	ExecutablePath string
	// Version 解析出的版本；未解析出时为零值（Valid() == false）。
	Version Version
	// Flags 解析出的 help 事实；未解析出时为零值。
	Flags HelpFlags
	// Report 能力报告；探测失败时 Source 为空，因此 Has 对所有能力返回 false。
	Report execbackend.CapabilityReport
	// Problems 问题列表，按探测阶段顺序排列。
	Problems []Problem
}

// Probe 只读探测指定 CLI（configured 为空则从 PATH 找 claude）。
//
// 只执行 `<exe> --version` 与 `<exe> --help`：不发模型请求、不登录、不安装、不改任何
// 文件。implemented 是本适配器声称已实现的能力集合（T1.13.b 传入），参与能力推导。
//
// 返回值：ctx 被取消时返回 error（调用方主动放弃，不是探测结论）；其余情况都返回
// ProbeResult，失败原因在 Problems 里。
func (p Prober) Probe(ctx context.Context, configured string, implemented []execbackend.Capability) (ProbeResult, error) {
	if err := ctx.Err(); err != nil {
		return ProbeResult{}, err
	}
	now := p.now()

	exe, err := ResolveExecutable(configured, p.lookPath())
	if err != nil {
		var re *ResolveError
		if errors.As(err, &re) {
			return ProbeResult{Problems: []Problem{re.Problem}, Report: p.emptyReport("", now)}, nil
		}
		return ProbeResult{Problems: []Problem{{
			Code:   ProblemCLINotFound,
			Detail: "`claude` executable could not be resolved",
		}}, Report: p.emptyReport("", now)}, nil
	}

	runner := p.runner()
	versionOut, versionErr := runner(ctx, exe, []string{"--version"})
	if cerr := ctx.Err(); cerr != nil {
		return ProbeResult{}, cerr
	}
	helpOut, helpErr := runner(ctx, exe, []string{"--help"})
	if cerr := ctx.Err(); cerr != nil {
		return ProbeResult{}, cerr
	}

	result := ProbeResult{ExecutablePath: exe}
	if versionErr != nil || helpErr != nil {
		// 探测命令本身失败：不推导能力（Source 留空 → 全 false），只报问题。
		version, _ := ParseVersion(string(versionOut))
		result.Version = version
		result.Report = execbackend.CapabilityReport{
			Backend:        BackendName,
			ExecutablePath: exe,
			Version:        version.String(),
			ProbedAt:       now,
		}
		if versionErr != nil {
			result.Problems = append(result.Problems, p.probeProblem("--version", versionErr))
		}
		if helpErr != nil {
			result.Problems = append(result.Problems, p.probeProblem("--help", helpErr))
		}
		return result, nil
	}

	version, versionParseErr := ParseVersion(string(versionOut))
	flags, helpParseErr := ParseHelp(string(helpOut))
	result.Version = version
	result.Flags = flags
	report, problems := DeriveCapabilities(Facts{
		ExecutablePath: exe,
		ProbedAt:       now,
		Version:        version,
		VersionErr:     versionParseErr,
		Flags:          flags,
		HelpErr:        helpParseErr,
	}, implemented)
	result.Report = report
	result.Problems = problems
	return result, nil
}

// emptyReport 构造"什么都没探测到"的报告：Source 为空，因此任何能力都不算已证明。
func (p Prober) emptyReport(exe string, now time.Time) execbackend.CapabilityReport {
	return execbackend.CapabilityReport{
		Backend:        BackendName,
		ExecutablePath: exe,
		ProbedAt:       now,
	}
}

// probeProblem 把 Runner 错误映射成问题码。
//
// Detail 只写参数名、退出码与超时值：绝不使用 err.Error()，因此 stderr 正文、路径与
// 环境信息都不会进入面向用户的问题详情。
func (p Prober) probeProblem(arg string, err error) Problem {
	switch {
	case errors.Is(err, ErrProbeOutputTooLarge):
		return Problem{
			Code: ProblemProbeOutputTooLarge,
			Detail: fmt.Sprintf("`%s %s` wrote more than %d bytes to stdout",
				probeCommand, arg, p.maxOutput()),
		}
	case errors.Is(err, context.DeadlineExceeded):
		return Problem{
			Code: ProblemProbeTimeout,
			Detail: fmt.Sprintf("`%s %s` did not finish within %s",
				probeCommand, arg, p.timeout()),
		}
	}
	var pe *ProbeError
	if errors.As(err, &pe) && pe.ExitCode >= 0 {
		return Problem{
			Code:   ProblemProbeFailed,
			Detail: fmt.Sprintf("`%s %s` exited with code %d", probeCommand, arg, pe.ExitCode),
		}
	}
	return Problem{
		Code:   ProblemProbeFailed,
		Detail: fmt.Sprintf("`%s %s` could not be started", probeCommand, arg),
	}
}

// now 返回 ProbedAt 时间。
func (p Prober) now() time.Time {
	if p.Clock != nil {
		return p.Clock.Now()
	}
	return time.Now()
}

// timeout 返回单次探测超时。
func (p Prober) timeout() time.Duration {
	if p.Timeout > 0 {
		return p.Timeout
	}
	return DefaultProbeTimeout
}

// maxOutput 返回单次探测 stdout 上限。
func (p Prober) maxOutput() int {
	if p.MaxOutput > 0 {
		return p.MaxOutput
	}
	return DefaultProbeOutputLimit
}

// lookPath 返回定位函数。
func (p Prober) lookPath() func(string) (string, error) {
	if p.LookPath != nil {
		return p.LookPath
	}
	return exec.LookPath
}

// runner 返回探测 Runner；未注入时用 os/exec 的默认实现。
func (p Prober) runner() Runner {
	if p.Runner != nil {
		return p.Runner
	}
	env := p.childEnv()
	return func(ctx context.Context, exe string, args []string) ([]byte, error) {
		return runProbeCommand(ctx, exe, args, p.timeout(), p.maxOutput(), env)
	}
}

// childEnv 返回默认 Runner 传给探测子进程的环境：父环境（Env 为 nil 时取 os.Environ()）
// 经 ProbeEnv 白名单过滤。
//
// nil 不能直接交给 ProbeEnv：那样子进程只剩 CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC，
// 没有 PATH/HOME/SystemRoot。原生 claude.exe 恰好还能跑，但 npm 安装的 `claude` 是
// `#!/usr/bin/env node` 脚本，找不到 node 就会被误报成 probe_failed。
func (p Prober) childEnv() []string {
	parent := p.Env
	if parent == nil {
		parent = os.Environ()
	}
	return ProbeEnv(parent)
}

// runProbeCommand 是默认 Runner：只跑一条只读命令，stdin 为空，stderr 丢弃。
//
// 细节：
//   - 每次调用单独计时（Timeout），超时后由 exec 的 Cancel 终止进程，再等 WaitDelay；
//   - stdout 上限 MaxOutput：超出后停止累积（继续读掉剩余输出，避免子进程被 SIGPIPE
//     打死而变成"退出码非零"），返回 ErrProbeOutputTooLarge；
//   - stderr 直接丢弃：只保留退出码，不回显正文。
func runProbeCommand(ctx context.Context, exe string, args []string, timeout time.Duration, maxOutput int, env []string) ([]byte, error) {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, exe, args...)
	cmd.Env = env
	cmd.Stdin = nil // 空 stdin：探测命令不该等待任何输入
	cmd.Stderr = nil
	cmd.WaitDelay = probeWaitDelay

	sink := &limitedSink{limit: maxOutput}
	cmd.Stdout = sink
	runErr := cmd.Run()
	if sink.overflow {
		return nil, ErrProbeOutputTooLarge
	}
	if runErr == nil {
		return sink.bytes(), nil
	}
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return nil, &ProbeError{Arg: argName(args), ExitCode: -1, Err: context.DeadlineExceeded}
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return nil, &ProbeError{Arg: argName(args), ExitCode: exitErr.ExitCode()}
	}
	return nil, &ProbeError{Arg: argName(args), ExitCode: -1}
}

// argName 返回用于错误信息与问题的参数名。
func argName(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return strings.Join(args, " ")
}

// limitedSink 是带上限的 stdout 收集器。
//
// 超过上限后不再累积内容但继续吞掉写入（返回完整长度），让子进程正常跑完：否则写入
// 失败会把进程打死，问题码就变成 probe_failed 而不是 probe_output_too_large。
type limitedSink struct {
	buf      bytes.Buffer
	limit    int
	overflow bool
}

// Write 实现 io.Writer。
func (s *limitedSink) Write(p []byte) (int, error) {
	if s.overflow {
		return len(p), nil
	}
	room := s.limit - s.buf.Len()
	if room <= 0 {
		s.overflow = true
		return len(p), nil
	}
	if len(p) > room {
		s.buf.Write(p[:room])
		s.overflow = true
		return len(p), nil
	}
	s.buf.Write(p)
	return len(p), nil
}

// bytes 返回已累积的 stdout 副本。
func (s *limitedSink) bytes() []byte { return append([]byte(nil), s.buf.Bytes()...) }
