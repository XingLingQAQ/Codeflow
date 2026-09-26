package claudecode

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/execbackend"
)

// call 是一次 Runner 调用记录。
type call struct {
	exe  string
	args []string
}

// scriptedRunner 是假 Runner：按参数返回预设输出/错误，并记录每次调用。
type scriptedRunner struct {
	versionOut string
	helpOut    string
	versionErr error
	helpErr    error
	calls      []call
}

// Run 实现 Runner。
func (r *scriptedRunner) Run(ctx context.Context, exe string, args []string) ([]byte, error) {
	r.calls = append(r.calls, call{exe: exe, args: append([]string(nil), args...)})
	if len(args) == 1 && args[0] == "--version" {
		if r.versionErr != nil {
			return nil, r.versionErr
		}
		return []byte(r.versionOut), nil
	}
	if len(args) == 1 && args[0] == "--help" {
		if r.helpErr != nil {
			return nil, r.helpErr
		}
		return []byte(r.helpOut), nil
	}
	return nil, errors.New("scriptedRunner: unexpected arguments")
}

// fixtureRunner 返回用 fixture 作答的假 Runner。
func fixtureRunner(t *testing.T) *scriptedRunner {
	t.Helper()
	return &scriptedRunner{
		versionOut: fixtureVersionText(t),
		helpOut:    fixtureHelpText(t),
	}
}

// fakeLookPath 是固定的 LookPath 假实现。
func fakeLookPath(path string) func(string) (string, error) {
	return func(name string) (string, error) {
		if name != probeCommand {
			return "", errors.New("not found")
		}
		return path, nil
	}
}

// probeSpec 返回一个已定位到假可执行文件的 Prober。
func probeSpec(t *testing.T, runner Runner) Prober {
	t.Helper()
	return Prober{
		Runner:   runner,
		LookPath: fakeLookPath(filepath.Join(t.TempDir(), "claude.exe")),
		Clock:    fixedClock{at: probeTime},
	}
}

func TestProbeHappyPath(t *testing.T) {
	runner := fixtureRunner(t)
	p := probeSpec(t, runner.Run)
	result, err := p.Probe(context.Background(), "", derivableCapabilities())
	if err != nil {
		t.Fatalf("Probe returned error: %v", err)
	}
	if len(result.Problems) != 0 {
		t.Fatalf("fixture probe must have no problems: %v", result.Problems)
	}
	if !result.Version.Valid() || result.Version.String() != "2.1.283" {
		t.Fatalf("Version = %v, want 2.1.283", result.Version)
	}
	if !result.Flags.Has("--print") {
		t.Fatal("Flags must be parsed from --help output")
	}
	if result.ExecutablePath == "" || !filepath.IsAbs(result.ExecutablePath) {
		t.Fatalf("ExecutablePath = %q, want absolute", result.ExecutablePath)
	}
	if result.Report.ExecutablePath != result.ExecutablePath {
		t.Fatalf("Report.ExecutablePath = %q, want %q", result.Report.ExecutablePath, result.ExecutablePath)
	}
	if !result.Report.ProbedAt.Equal(probeTime) {
		t.Fatalf("Report.ProbedAt = %v, want injected clock time %v", result.Report.ProbedAt, probeTime)
	}
	for _, c := range derivableCapabilities() {
		if !result.Report.Has(c) {
			t.Errorf("capability %s must be proven", c)
		}
	}

	// 只允许两种只读调用，且顺序固定。
	wantCalls := []call{
		{exe: result.ExecutablePath, args: []string{"--version"}},
		{exe: result.ExecutablePath, args: []string{"--help"}},
	}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("runner calls = %#v, want %#v", runner.calls, wantCalls)
	}
}

func TestProbeDoesNotRunOtherSubcommands(t *testing.T) {
	runner := fixtureRunner(t)
	p := probeSpec(t, runner.Run)
	if _, err := p.Probe(context.Background(), "", nil); err != nil {
		t.Fatalf("Probe: %v", err)
	}
	for _, c := range runner.calls {
		if len(c.args) != 1 {
			t.Fatalf("probe must pass exactly one argument, got %#v", c.args)
		}
		if c.args[0] != "--version" && c.args[0] != "--help" {
			t.Fatalf("probe must only run --version/--help, got %q", c.args[0])
		}
	}
	if len(runner.calls) != 2 {
		t.Fatalf("probe must run exactly 2 commands, got %d", len(runner.calls))
	}
}

func TestProbeConfiguredExecutable(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "claude.exe")
	if err := os.WriteFile(exe, []byte("stub"), 0o600); err != nil {
		t.Fatalf("write stub exe: %v", err)
	}
	runner := fixtureRunner(t)
	p := Prober{Runner: runner.Run, Clock: fixedClock{at: probeTime}}
	result, err := p.Probe(context.Background(), exe, nil)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if result.ExecutablePath != exe {
		t.Fatalf("ExecutablePath = %q, want %q", result.ExecutablePath, exe)
	}
}

func TestProbeTimeout(t *testing.T) {
	runner := &scriptedRunner{versionErr: context.DeadlineExceeded}
	p := probeSpec(t, runner.Run)
	result, err := p.Probe(context.Background(), "", derivableCapabilities())
	if err != nil {
		t.Fatalf("Probe must not return error for a probe timeout: %v", err)
	}
	if _, ok := findProblem(result.Problems, ProblemProbeTimeout, "--version"); !ok {
		t.Fatalf("want probe_timeout for --version, got %v", result.Problems)
	}
	for _, c := range execbackend.AllCapabilities() {
		if result.Report.Has(c) {
			t.Errorf("timed-out probe must prove nothing, got %s", c)
		}
	}
	if result.Report.Source != "" {
		t.Fatalf("failed probe must leave Source empty, got %q", result.Report.Source)
	}
}

func TestProbeOutputTooLarge(t *testing.T) {
	runner := &scriptedRunner{versionOut: fixtureVersionText(t), helpErr: ErrProbeOutputTooLarge}
	p := probeSpec(t, runner.Run)
	result, err := p.Probe(context.Background(), "", derivableCapabilities())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if _, ok := findProblem(result.Problems, ProblemProbeOutputTooLarge, "--help"); !ok {
		t.Fatalf("want probe_output_too_large, got %v", result.Problems)
	}
	if result.Report.Has(execbackend.CapabilityNonInteractive) {
		t.Fatal("oversized output must not prove capabilities")
	}
	// --version 成功过，版本事实仍然记录（但 Source 为空 → 能力全 false）。
	if result.Version.String() != "2.1.283" {
		t.Fatalf("Version = %q, want the parsed --version fact", result.Version.String())
	}
}

func TestProbeNonZeroExitHidesStderr(t *testing.T) {
	const secret = "stderr: token=SECRET-STDERR-TEXT"
	runner := &scriptedRunner{
		versionErr: &ProbeError{Arg: "--version", ExitCode: 2, Err: errors.New(secret)},
		helpErr:    &ProbeError{Arg: "--help", ExitCode: 2, Err: errors.New(secret)},
	}
	p := probeSpec(t, runner.Run)
	result, err := p.Probe(context.Background(), "", derivableCapabilities())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if len(result.Problems) != 2 {
		t.Fatalf("want one problem per failed command, got %v", result.Problems)
	}
	for _, problem := range result.Problems {
		if problem.Code != ProblemProbeFailed {
			t.Errorf("problem code = %s, want %s", problem.Code, ProblemProbeFailed)
		}
		if strings.Contains(problem.Detail, secret) || strings.Contains(problem.Detail, "SECRET") {
			t.Errorf("problem detail must not echo stderr: %q", problem.Detail)
		}
	}
	if _, ok := findProblem(result.Problems, ProblemProbeFailed, "code 2"); !ok {
		t.Fatalf("probe_failed detail must carry the exit code: %v", result.Problems)
	}
}

func TestProbeStartFailure(t *testing.T) {
	runner := &scriptedRunner{versionErr: &ProbeError{Arg: "--version", ExitCode: -1}}
	p := probeSpec(t, runner.Run)
	result, err := p.Probe(context.Background(), "", nil)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if _, ok := findProblem(result.Problems, ProblemProbeFailed, "could not be started"); !ok {
		t.Fatalf("want probe_failed (start), got %v", result.Problems)
	}
}

func TestProbeCanceledContext(t *testing.T) {
	runner := fixtureRunner(t)
	p := probeSpec(t, runner.Run)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Probe(ctx, "", nil); err == nil {
		t.Fatal("canceled ctx must return an error")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("canceled ctx must not run any command, got %#v", runner.calls)
	}
}

func TestProbeResolveFailures(t *testing.T) {
	relative := Prober{
		Runner: fixtureRunner(t).Run,
		LookPath: func(string) (string, error) {
			return "", errors.New("not found")
		},
		Clock: fixedClock{at: probeTime},
	}
	result, err := relative.Probe(context.Background(), "claude", nil)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if _, ok := findProblem(result.Problems, ProblemCLIPathNotAbsolute, ""); !ok {
		t.Fatalf("want cli_path_not_absolute, got %v", result.Problems)
	}

	missing := Prober{
		Runner:   fixtureRunner(t).Run,
		LookPath: func(string) (string, error) { return "", errors.New("not found") },
		Clock:    fixedClock{at: probeTime},
	}
	result, err = missing.Probe(context.Background(), "", nil)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if _, ok := findProblem(result.Problems, ProblemCLINotFound, "PATH"); !ok {
		t.Fatalf("want cli_not_found, got %v", result.Problems)
	}
	if result.Report.Backend != BackendName {
		t.Fatalf("Report.Backend = %q, want %q", result.Report.Backend, BackendName)
	}
	if result.Report.Has(execbackend.CapabilityNonInteractive) {
		t.Fatal("unresolved CLI must not prove capabilities")
	}
}

func TestResolveExecutableConfigured(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "claude.exe")
	if err := os.WriteFile(exe, []byte("stub"), 0o600); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	got, err := ResolveExecutable(exe, nil)
	if err != nil {
		t.Fatalf("ResolveExecutable(configured): %v", err)
	}
	if got != exe {
		t.Fatalf("ResolveExecutable = %q, want %q", got, exe)
	}

	if _, err := ResolveExecutable("claude", nil); !isProblemCode(err, ProblemCLIPathNotAbsolute) {
		t.Fatalf("relative path must be rejected with %s, got %v", ProblemCLIPathNotAbsolute, err)
	}
	if _, err := ResolveExecutable(filepath.Join(dir, "nope.exe"), nil); !isProblemCode(err, ProblemCLINotFound) {
		t.Fatalf("missing file must be rejected with %s, got %v", ProblemCLINotFound, err)
	}
	if _, err := ResolveExecutable(dir, nil); !isProblemCode(err, ProblemCLINotFound) {
		t.Fatalf("directory must be rejected with %s, got %v", ProblemCLINotFound, err)
	}
	// 空白配置值等于"没配置"，必须走 lookPath（这里注入一个必然失败的实现，避免依赖本机 PATH）。
	if _, err := ResolveExecutable("   ", func(string) (string, error) {
		return "", errors.New("not found")
	}); !isProblemCode(err, ProblemCLINotFound) {
		t.Fatalf("blank configured path must fall back to lookPath, got %v", err)
	}
}

func TestResolveExecutableLookPath(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "claude.exe")
	got, err := ResolveExecutable("", fakeLookPath(exe))
	if err != nil {
		t.Fatalf("ResolveExecutable(lookPath): %v", err)
	}
	if got != exe {
		t.Fatalf("ResolveExecutable = %q, want %q", got, exe)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("resolved path %q must be absolute", got)
	}

	// 相对路径也要被转成绝对路径。
	rel, err := ResolveExecutable("", func(string) (string, error) { return filepath.Join("bin", "claude"), nil })
	if err != nil {
		t.Fatalf("ResolveExecutable(relative lookPath): %v", err)
	}
	if !filepath.IsAbs(rel) {
		t.Fatalf("resolved path %q must be absolute", rel)
	}

	if _, err := ResolveExecutable("", func(string) (string, error) {
		return "", errors.New("not found")
	}); !isProblemCode(err, ProblemCLINotFound) {
		t.Fatalf("lookPath failure must be cli_not_found, got %v", err)
	}
	if _, err := ResolveExecutable("", func(string) (string, error) { return "  ", nil }); !isProblemCode(err, ProblemCLINotFound) {
		t.Fatalf("blank lookPath result must be cli_not_found, got %v", err)
	}
}

// isProblemCode 报告 err 是否是指定问题码的 *ResolveError。
func isProblemCode(err error, code string) bool {
	var re *ResolveError
	if !errors.As(err, &re) {
		return false
	}
	return re.Problem.Code == code
}

func TestProbeEnvWindows(t *testing.T) {
	parent := []string{
		"ANTHROPIC_API_KEY=x",
		"AWS_SECRET_ACCESS_KEY=y",
		"CLAUDE_CODE_OAUTH_TOKEN=z",
		"PATH=C:\\bin",
		"SystemRoot=C:\\Windows",
		"ComSpec=C:\\Windows\\system32\\cmd.exe",
		"USERPROFILE=C:\\Users\\someone",
		"TEMP=C:\\Temp",
		"HOME=/home/someone",
		"LANG=en_US.UTF-8",
		"NO_EQUALS_SIGN",
		"",
	}
	got := probeEnvForOS(parent, "windows")
	want := []string{
		"PATH=C:\\bin",
		"SystemRoot=C:\\Windows",
		"ComSpec=C:\\Windows\\system32\\cmd.exe",
		"USERPROFILE=C:\\Users\\someone",
		"TEMP=C:\\Temp",
		nonEssentialTrafficEnv + "=1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("probeEnvForOS(windows) = %#v, want %#v", got, want)
	}
	assertNoCredentialEnv(t, got)
}

func TestProbeEnvUnix(t *testing.T) {
	parent := []string{
		"ANTHROPIC_API_KEY=x",
		"AWS_SECRET_ACCESS_KEY=y",
		"CLAUDE_CODE_OAUTH_TOKEN=z",
		"PATH=/usr/bin",
		"HOME=/home/someone",
		"TMPDIR=/tmp",
		"LANG=en_US.UTF-8",
		"SystemRoot=C:\\Windows",
	}
	got := probeEnvForOS(parent, "linux")
	want := []string{
		"PATH=/usr/bin",
		"HOME=/home/someone",
		"TMPDIR=/tmp",
		"LANG=en_US.UTF-8",
		nonEssentialTrafficEnv + "=1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("probeEnvForOS(linux) = %#v, want %#v", got, want)
	}
	assertNoCredentialEnv(t, got)
}

func TestProbeEnvIsCaseInsensitiveOnWindows(t *testing.T) {
	got := probeEnvForOS([]string{"Path=C:\\bin", "path=C:\\other"}, "windows")
	if len(got) != 2 {
		t.Fatalf("probeEnvForOS = %#v, want first Path entry plus the traffic flag", got)
	}
	if got[0] != "Path=C:\\bin" {
		t.Fatalf("first entry = %q, want Path=C:\\bin", got[0])
	}
}

func TestProbeEnvBlocksCredentialPrefixesEvenIfWhitelisted(t *testing.T) {
	// 防御性二次拦截：即便某天白名单里误加了凭据变量名，也必须被前缀规则拦掉。
	for _, name := range []string{
		"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "AWS_SECRET_ACCESS_KEY",
		"AWS_PROFILE", "GOOGLE_APPLICATION_CREDENTIALS", "AZURE_CLIENT_SECRET",
		"CLAUDE_CODE_OAUTH_TOKEN",
	} {
		if !isCredentialEnvName(name) {
			t.Errorf("%s must be treated as a credential variable", name)
		}
	}
	for _, name := range []string{"PATH", "SystemRoot", "TEMP", "ComSpec"} {
		if isCredentialEnvName(name) {
			t.Errorf("%s must not be treated as a credential variable", name)
		}
	}
}

// assertNoCredentialEnv 断言结果里没有任何凭据变量。
func assertNoCredentialEnv(t *testing.T, env []string) {
	t.Helper()
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		upper := strings.ToUpper(name)
		if isCredentialEnvName(upper) {
			t.Fatalf("credential variable %s must not be passed to the probe subprocess", name)
		}
	}
	count := 0
	for _, kv := range env {
		if strings.HasPrefix(kv, nonEssentialTrafficEnv+"=") {
			count++
			if kv != nonEssentialTrafficEnv+"=1" {
				t.Fatalf("%s must be set to 1, got %q", nonEssentialTrafficEnv, kv)
			}
		}
	}
	if count != 1 {
		t.Fatalf("%s must appear exactly once, got %d", nonEssentialTrafficEnv, count)
	}
}

func TestProbeEnvUsesHostOSByDefault(t *testing.T) {
	got := ProbeEnv([]string{"PATH=/usr/bin", "ANTHROPIC_API_KEY=x"})
	if len(got) == 0 || got[len(got)-1] != nonEssentialTrafficEnv+"=1" {
		t.Fatalf("ProbeEnv = %#v, want the traffic flag last", got)
	}
	assertNoCredentialEnv(t, got)
}

func TestProbeTimeoutAndLimitDefaults(t *testing.T) {
	p := Prober{}
	if p.timeout() != DefaultProbeTimeout || DefaultProbeTimeout != 15*time.Second {
		t.Fatalf("default timeout = %v, want 15s", p.timeout())
	}
	if p.maxOutput() != DefaultProbeOutputLimit || DefaultProbeOutputLimit != 1<<20 {
		t.Fatalf("default output limit = %d, want 1 MiB", p.maxOutput())
	}
	if probeWaitDelay != 2*time.Second {
		t.Fatalf("probeWaitDelay = %v, want 2s", probeWaitDelay)
	}
	custom := Prober{Timeout: time.Second, MaxOutput: 16}
	if custom.timeout() != time.Second || custom.maxOutput() != 16 {
		t.Fatalf("explicit timeout/limit must win: %v/%d", custom.timeout(), custom.maxOutput())
	}
}

func TestLimitedSink(t *testing.T) {
	sink := &limitedSink{limit: 4}
	if n, err := sink.Write([]byte("abc")); n != 3 || err != nil {
		t.Fatalf("Write = %d, %v", n, err)
	}
	if sink.overflow {
		t.Fatal("must not overflow yet")
	}
	if n, err := sink.Write([]byte("def")); n != 3 || err != nil {
		t.Fatalf("Write must keep reporting full length, got %d, %v", n, err)
	}
	if !sink.overflow {
		t.Fatal("must flag overflow")
	}
	if got := string(sink.bytes()); got != "abcd" {
		t.Fatalf("sink content = %q, want abcd", got)
	}
	if n, err := sink.Write([]byte("more")); n != 4 || err != nil {
		t.Fatalf("post-overflow Write = %d, %v", n, err)
	}
}

// helperEnvName 是重新执行测试二进制时的模式开关。
const helperEnvName = "CODEFLOW_PROBE_TEST_HELPER"

// TestProbeHelperProcess 不是真正的测试：它只在本包把测试二进制当作"被探测的命令"
// 重新执行时干活（标准 Go 做法，避免依赖 sh/cmd.exe/ping 的可移植性）。
//
// 常规运行下环境变量为空，函数直接返回（PASS，不是 SKIP），因此常规构建里 SKIP 为 0。
func TestProbeHelperProcess(t *testing.T) {
	switch os.Getenv(helperEnvName) {
	case "ok":
		os.Stdout.WriteString("hello\n")
	case "fail":
		os.Stdout.WriteString("partial output\n")
		os.Stderr.WriteString("boom SECRET-STDERR-TEXT\n")
		os.Exit(3)
	case "flood":
		chunk := []byte(strings.Repeat("x", 4096))
		for i := 0; i < 64; i++ {
			os.Stdout.Write(chunk)
		}
	case "sleep":
		time.Sleep(30 * time.Second)
	}
}

// helperCommand 返回"把测试二进制当作被探测命令"所需的可执行文件与参数。
func helperCommand(t *testing.T, mode string) (string, []string, []string) {
	t.Helper()
	exe, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatalf("abs(test binary): %v", err)
	}
	env := append(ProbeEnv(nil), helperEnvName+"="+mode)
	return exe, []string{"-test.run=TestProbeHelperProcess"}, env
}

func TestDefaultRunnerReadsStdout(t *testing.T) {
	exe, args, env := helperCommand(t, "ok")
	out, err := runProbeCommand(context.Background(), exe, args, 30*time.Second, 1<<20, env)
	if err != nil {
		t.Fatalf("runProbeCommand: %v", err)
	}
	if !strings.Contains(string(out), "hello") {
		t.Fatalf("stdout = %q, want it to contain hello", out)
	}
}

func TestDefaultRunnerRejectsNonZeroExit(t *testing.T) {
	exe, args, env := helperCommand(t, "fail")
	_, err := runProbeCommand(context.Background(), exe, args, 30*time.Second, 1<<20, env)
	var pe *ProbeError
	if !errors.As(err, &pe) {
		t.Fatalf("want *ProbeError, got %T: %v", err, err)
	}
	if pe.ExitCode != 3 {
		t.Fatalf("ExitCode = %d, want 3", pe.ExitCode)
	}
	if strings.Contains(pe.Error(), "SECRET-STDERR-TEXT") || strings.Contains(pe.Error(), "boom") {
		t.Fatalf("stderr must not be echoed: %q", pe.Error())
	}
	// 同一个错误经 probeProblem 后也不得泄漏 stderr。
	p := Prober{}
	problem := p.probeProblem("--version", err)
	if problem.Code != ProblemProbeFailed {
		t.Fatalf("problem code = %s, want %s", problem.Code, ProblemProbeFailed)
	}
	if !strings.Contains(problem.Detail, "3") {
		t.Fatalf("probe_failed detail must carry the exit code: %q", problem.Detail)
	}
	if strings.Contains(problem.Detail, "SECRET") || strings.Contains(problem.Detail, "boom") {
		t.Fatalf("problem detail must not echo stderr: %q", problem.Detail)
	}
}

func TestDefaultRunnerOutputLimit(t *testing.T) {
	exe, args, env := helperCommand(t, "flood")
	_, err := runProbeCommand(context.Background(), exe, args, 30*time.Second, 64, env)
	if !errors.Is(err, ErrProbeOutputTooLarge) {
		t.Fatalf("want ErrProbeOutputTooLarge, got %v", err)
	}
}

func TestDefaultRunnerTimeout(t *testing.T) {
	exe, args, env := helperCommand(t, "sleep")
	_, err := runProbeCommand(context.Background(), exe, args, 300*time.Millisecond, 1<<20, env)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want context.DeadlineExceeded, got %v", err)
	}
	var pe *ProbeError
	if !errors.As(err, &pe) || pe.ExitCode != -1 {
		t.Fatalf("timed-out probe must report ExitCode -1, got %v", err)
	}
}

// TestProberChildEnvDefaultsToProcessEnvironment 钉住 Env 为 nil 时的默认值：父环境取
// os.Environ()，再经白名单过滤。只交给 ProbeEnv(nil) 的话子进程没有 PATH，
// 依赖 PATH 找解释器的 claude 安装方式（npm 脚本）会被误报 probe_failed。
func TestProberChildEnvDefaultsToProcessEnvironment(t *testing.T) {
	t.Setenv("PATH", "codeflow-probe-path-marker")
	t.Setenv("ANTHROPIC_API_KEY", "codeflow-probe-secret-marker")

	env := Prober{}.childEnv()
	var sawPath bool
	for _, kv := range env {
		name, value, _ := strings.Cut(kv, "=")
		if strings.EqualFold(name, "PATH") && value == "codeflow-probe-path-marker" {
			sawPath = true
		}
		if strings.Contains(kv, "codeflow-probe-secret-marker") {
			t.Fatalf("credential leaked into the probe environment: %s", name)
		}
	}
	if !sawPath {
		t.Fatalf("Prober{}.childEnv() must inherit PATH from os.Environ(), got %d entries", len(env))
	}

	explicit := Prober{Env: []string{"PATH=explicit"}}.childEnv()
	if len(explicit) != 2 || explicit[0] != "PATH=explicit" {
		t.Fatalf("explicit Env must be used as the parent environment, got %v", explicit)
	}
}
