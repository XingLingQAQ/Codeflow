package process

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// 本文件是“测试二进制自我重入”夹具：TestMain 发现 CODEFLOW_PROCESS_HELPER 后不跑
// 测试，而是进入 helper 模式。子进程就是本测试二进制自己（os.Executable()），因此
// 进程树、退出码与阻塞行为完全由测试控制，不需要额外的辅助程序或 shell。
//
// 环境变量：
//
//	CODEFLOW_PROCESS_HELPER  helper 模式：tree|flood|ignore-term|exit|graceful-exit|spawn-and-exit|lines
//	CODEFLOW_HELPER_DEPTH    tree：本进程还要往下递归几层
//	CODEFLOW_HELPER_FANOUT   tree：每层启动几个子进程
//	CODEFLOW_HELPER_REPORT   tree|graceful-exit|spawn-and-exit：{pid,ppid,depth} JSON 行追加写入的报告文件
//	CODEFLOW_HELPER_RELEASE  tree|ignore-term|exit|lines：该文件出现后进程退出（lines：出现后才开始写行）
//	CODEFLOW_HELPER_BYTES    flood：向 stdout 写入的字节数
//	CODEFLOW_HELPER_CODE     exit：退出码
//	CODEFLOW_HELPER_TIMEOUT  spawn-and-exit：等后代写出报告行的上限（毫秒，0 表示默认）
//	CODEFLOW_HELPER_LINES    lines：向 stdout 写的行数（内容 line-000001 起）
//	CODEFLOW_HELPER_LINE_BYTES    lines：每行目标字节数（0=短行；不足补 'x'，超出截断）
//	CODEFLOW_HELPER_LINE_CRLF     lines：1 表示行尾用 \r\n
//	CODEFLOW_HELPER_LINE_NO_EOL   lines：1 表示最后一行不带换行符（不完整行）
//	CODEFLOW_HELPER_STDERR_LINES  lines：额外向 stderr 写的行数（内容 err-000001 起）
//	CODEFLOW_HELPER_HOLD          lines：写完行后等该文件出现再退出（让进程活到测试订阅之后）
//	CODEFLOW_HELPER_LINE_PREFIX   lines：每行前缀（可放 canary，供 spool 过滤测试用）
const (
	helperModeEnv       = "CODEFLOW_PROCESS_HELPER"
	helperDepthEnv      = "CODEFLOW_HELPER_DEPTH"
	helperFanoutEnv     = "CODEFLOW_HELPER_FANOUT"
	helperReportEnv     = "CODEFLOW_HELPER_REPORT"
	helperReleaseEnv    = "CODEFLOW_HELPER_RELEASE"
	helperBytesEnv      = "CODEFLOW_HELPER_BYTES"
	helperCodeEnv       = "CODEFLOW_HELPER_CODE"
	helperTimeoutEnv    = "CODEFLOW_HELPER_TIMEOUT"
	helperLinesEnv      = "CODEFLOW_HELPER_LINES"
	helperLineBytesEnv  = "CODEFLOW_HELPER_LINE_BYTES"
	helperLineCRLFEnv   = "CODEFLOW_HELPER_LINE_CRLF"
	helperLineNoEOLEnv  = "CODEFLOW_HELPER_LINE_NO_EOL"
	helperStderrEnv     = "CODEFLOW_HELPER_STDERR_LINES"
	helperHoldEnv       = "CODEFLOW_HELPER_HOLD"
	helperLinePrefixEnv = "CODEFLOW_HELPER_LINE_PREFIX"

	helperModeTree        = "tree"
	helperModeFlood       = "flood"
	helperModeIgnoreTerm  = "ignore-term"
	helperModeResistsTerm = "resists-term"
	helperModeExit        = "exit"
	helperModeGraceful    = "graceful-exit"
	helperModeSpawnAndOut = "spawn-and-exit"
	helperModeLines       = "lines"

	// helperSigbreakNumber 是 Windows 的 SIGBREAK 信号号（os/signal 在 Windows 上
	// 把它对应到 CTRL_BREAK_EVENT）。在 Unix 上 21 号信号是 SIGTTIN，忽略它对
	// 非交互测试进程没有副作用，因此共享同一份 helper 代码。
	helperSigbreakNumber = 21

	// helperReleasePoll 是等待 release 文件 / 保持阻塞的轮询间隔。
	helperReleasePoll = 25 * time.Millisecond
)

func TestMain(m *testing.M) {
	if mode := os.Getenv(helperModeEnv); mode != "" {
		os.Exit(runHelper(mode))
	}
	os.Exit(m.Run())
}

// runHelper 是 helper 模式的入口：返回进程退出码，绝不调用 m.Run()。
func runHelper(mode string) int {
	switch mode {
	case helperModeTree:
		return helperTree()
	case helperModeFlood:
		return helperFlood()
	case helperModeIgnoreTerm:
		ignoreSoftTermination()
		blockUntilReleased()
		return 0
	case helperModeResistsTerm:
		return helperResistsTermination()
	case helperModeExit:
		// 未设置 CODEFLOW_HELPER_RELEASE 时立即退出；设置了就先等该文件出现，
		// 这样测试能在进程存活期间捕获身份，再确定性地观察退出。
		if os.Getenv(helperReleaseEnv) != "" {
			blockUntilReleased()
		}
		return helperInt(helperCodeEnv, 0)
	case helperModeGraceful:
		return helperGracefulExit()
	case helperModeSpawnAndOut:
		return helperSpawnAndExit()
	case helperModeLines:
		return helperLines()
	default:
		fmt.Fprintf(os.Stderr, "unknown helper mode %q\n", mode)
		return 2
	}
}

// helperReportLine 是 tree 模式写入报告文件的一行。
type helperReportLine struct {
	PID   int `json:"pid"`
	PPID  int `json:"ppid"`
	Depth int `json:"depth"`
}

// helperTree 先记录自己的身份，再按 fanout 递归启动子进程，然后一直阻塞。
//
// 不 Wait 子进程：它们要活到被 kill 或 release 为止；本进程退出后它们会变成孤儿
// （Windows 不会因为父进程退出而结束子进程），这正是 T1.08.b 必须用 Job Object /
// 进程组才能收拾的场景。
func helperTree() int {
	depth := helperInt(helperDepthEnv, 0)
	fanout := helperInt(helperFanoutEnv, 0)
	if err := appendHelperLine(depth); err != nil {
		fmt.Fprintf(os.Stderr, "helper report: %v\n", err)
		return 3
	}
	if depth > 0 {
		for i := 0; i < fanout; i++ {
			cmd := exec.Command(helperExecutable())
			cmd.Env = helperChildEnv(depth-1, fanout)
			if err := cmd.Start(); err != nil {
				fmt.Fprintf(os.Stderr, "helper spawn: %v\n", err)
				return 4
			}
		}
	}
	blockUntilReleased()
	return 0
}

// helperChildEnv 构造子 helper 的显式环境白名单（不继承本进程环境）。
//
// SystemRoot 是 Windows 上 CreateProcess/系统调用正常工作所需的最小环境项，
// 缺失时部分机器上会启动失败，因此按需补齐；其他平台不需要。
func helperChildEnv(depth, fanout int) []string {
	env := []string{
		helperModeEnv + "=" + helperModeTree,
		helperDepthEnv + "=" + strconv.Itoa(depth),
		helperFanoutEnv + "=" + strconv.Itoa(fanout),
		helperReportEnv + "=" + os.Getenv(helperReportEnv),
		helperReleaseEnv + "=" + os.Getenv(helperReleaseEnv),
	}
	if v := os.Getenv("SystemRoot"); v != "" {
		env = append(env, "SystemRoot="+v)
	}
	return env
}

// appendHelperLine 以单次 Write + O_APPEND 追加一行 JSON：多个 helper 进程并发写
// 同一个报告文件时，每行要么完整写入要么不写，测试端因此可以按行解析。
func appendHelperLine(depth int) error {
	path := os.Getenv(helperReportEnv)
	if path == "" {
		return errors.New("CODEFLOW_HELPER_REPORT is empty")
	}
	data, err := json.Marshal(helperReportLine{PID: os.Getpid(), PPID: os.Getppid(), Depth: depth})
	if err != nil {
		return err
	}
	data = append(data, '\n')
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck // 只写一行，关闭失败由测试端按行数发现
	_, err = f.Write(data)
	return err
}

// helperFlood 尽快向 stdout 写满 CODEFLOW_HELPER_BYTES 字节后退出 0。
// 写的是可预期内容的循环字节，便于测试端校验字节数。
func helperFlood() int {
	total := helperInt64(helperBytesEnv, 0)
	buf := make([]byte, 64<<10)
	for i := range buf {
		buf[i] = byte('a' + i%26)
	}
	var written int64
	for written < total {
		chunk := int64(len(buf))
		if remaining := total - written; remaining < chunk {
			chunk = remaining
		}
		n, err := os.Stdout.Write(buf[:chunk])
		written += int64(n)
		if err != nil {
			fmt.Fprintf(os.Stderr, "helper flood: %v\n", err)
			return 5
		}
	}
	return 0
}

// helperLines 按参数向 stdout/stderr 写确定内容的行，供实时按行订阅的测试使用。
//
// 行内容是可预期的，测试端据此逐行校验：stdout 第 i 行是 line-%06d，stderr 第 i 行是
// err-%06d。LINE_BYTES 非 0 时把每行补/裁到恰好该字节数（补 'x'）——超长行测试用它。
// RELEASE 存在时先等 release 文件出现再写：测试可以先订阅、后放行，整个过程没有竞态。
// HOLD 存在时写完行后继续等该文件出现：进程保持存活，测试可以观察“进程已经写了输出
// 但还没退出”的窗口（订阅不得丢行）。
func helperLines() int {
	if os.Getenv(helperReleaseEnv) != "" {
		blockUntilReleased()
	}
	count := helperInt(helperLinesEnv, 0)
	lineBytes := helperInt(helperLineBytesEnv, 0)
	crlf := helperInt(helperLineCRLFEnv, 0) != 0
	noEOL := helperInt(helperLineNoEOLEnv, 0) != 0
	stderrCount := helperInt(helperStderrEnv, 0)
	eol := "\n"
	if crlf {
		eol = "\r\n"
	}

	stdout := bufio.NewWriterSize(os.Stdout, 64<<10)
	stderr := bufio.NewWriterSize(os.Stderr, 16<<10)
	for i := 1; i <= count; i++ {
		if _, err := stdout.WriteString(helperLineContent(os.Getenv(helperLinePrefixEnv)+"line", i, lineBytes)); err != nil {
			fmt.Fprintf(os.Stderr, "helper lines: %v\n", err)
			return 5
		}
		if !(noEOL && i == count) {
			if _, err := stdout.WriteString(eol); err != nil {
				fmt.Fprintf(os.Stderr, "helper lines: %v\n", err)
				return 5
			}
		}
	}
	for i := 1; i <= stderrCount; i++ {
		if _, err := stderr.WriteString(helperLineContent("err", i, lineBytes)); err != nil {
			fmt.Fprintf(os.Stderr, "helper lines: %v\n", err)
			return 5
		}
		if _, err := stderr.WriteString("\n"); err != nil {
			fmt.Fprintf(os.Stderr, "helper lines: %v\n", err)
			return 5
		}
	}
	if err := stdout.Flush(); err != nil {
		fmt.Fprintf(os.Stderr, "helper lines: %v\n", err)
		return 5
	}
	if err := stderr.Flush(); err != nil {
		fmt.Fprintf(os.Stderr, "helper lines: %v\n", err)
		return 5
	}
	if os.Getenv(helperHoldEnv) != "" {
		blockUntilHold()
	}
	return 0
}

// helperLineContent 生成第 i 行的确定内容：前缀 + %06d，再按 lineBytes 补/裁。
func helperLineContent(prefix string, i, lineBytes int) string {
	base := fmt.Sprintf("%s-%06d", prefix, i)
	if lineBytes <= 0 {
		return base
	}
	if len(base) >= lineBytes {
		return base[:lineBytes]
	}
	return base + strings.Repeat("x", lineBytes-len(base))
}

// blockUntilHold 等 CODEFLOW_HELPER_HOLD 指定的文件出现；未设置时立即返回。
func blockUntilHold() {
	path := os.Getenv(helperHoldEnv)
	for {
		if path != "" {
			if _, err := os.Stat(path); err == nil {
				return
			}
		}
		time.Sleep(helperReleasePoll)
	}
}

// helperResistsTermination 是“软终止无效”的对手：注册（而不是忽略）软终止处理器，
// 收到后只记数不退出，然后继续阻塞。这正是升级路径要面对的场景——软终止确实投递到了，
// 但进程不配合。
//
// 为什么不复用 ignore-term 的 signal.Ignore：在 Windows 上，signal.Ignore(SIGBREAK)
// 会让 Go 运行时的控制台处理器返回 0（未处理），默认处理器随即结束进程——反而变成
// “软终止立即生效”，测不出升级。注册处理器能让运行时把事件接住并交给我们的 channel。
// Unix 上信号被处理器接住，同样不会终止进程。
//
// 装好处理器之后写一行报告：测试端据此确认软终止一定是在处理器就绪之后投递的。
func helperResistsTermination() int {
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.Signal(helperSigbreakNumber))
	defer signal.Stop(signals)
	soft := 0
	go func() {
		for range signals {
			soft++
		}
	}()
	if err := appendHelperLine(0); err != nil {
		fmt.Fprintf(os.Stderr, "helper report: %v\n", err)
		return 6
	}
	blockUntilReleased()
	_ = soft
	return 0
}

// helperGracefulExit 是“协作式”对手：先装好软终止处理器并写一行报告（测试据此确认
// 处理器已经就绪，避免在处理器装好之前投递信号导致默认动作直接终止进程），然后阻塞
// 等待软终止；收到就退出 0。
//
// Windows 上软终止是 CTRL_BREAK（os/signal 的 SIGBREAK），Unix 上是 SIGTERM，
// 两边都必须被这一个 helper 认出来。
func helperGracefulExit() int {
	ready := make(chan struct{}, 1)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.Signal(helperSigbreakNumber))
	defer signal.Stop(signals)
	go func() {
		<-signals
		ready <- struct{}{}
	}()
	if err := appendHelperLine(0); err != nil {
		fmt.Fprintf(os.Stderr, "helper report: %v\n", err)
		return 6
	}
	select {
	case <-ready:
		return 0
	case <-time.After(helperDuration(helperTimeoutEnv, 10*time.Minute)):
		// 兜底：软终止始终没来就自己退出，避免测试夹具变成永久孤儿。
		return 0
	}
}

// helperSpawnAndExit 派生一个后代后主进程立即退出，用来验证“父进程自然退出后残留
// 后代仍被清理”。后代是 tree 模式的 helper（depth=0）：写一行报告后永久阻塞。
//
// 主进程先写自己的报告行，再等报告文件出现第二行才退出：必须确保测试已经知道后代的
// PID，才能在后代被杀之后断言它确实死了。
func helperSpawnAndExit() int {
	if err := appendHelperLine(1); err != nil {
		fmt.Fprintf(os.Stderr, "helper report: %v\n", err)
		return 3
	}
	cmd := exec.Command(helperExecutable())
	cmd.Env = helperChildEnv(0, 0)
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "helper spawn: %v\n", err)
		return 4
	}
	// 不 Wait：后代必须活到主进程退出之后。
	timeout := helperDuration(helperTimeoutEnv, 10*time.Second)
	// 先等后代写出报告行（测试端据此拿到后代 PID）……
	if !waitForReportDepth(os.Getenv(helperReportEnv), 0, timeout) {
		fmt.Fprintln(os.Stderr, "helper: descendant did not report in time")
		return 7
	}
	// ……再等测试端写出“已接管”哨兵（depth=99）：只有测试确认过后代身份，主进程才
	// 退出。否则主进程可能抢在测试读报告之前退出，把后代一起带走，测试就无法先证明
	// “后代活着”。
	if !waitForReportDepth(os.Getenv(helperReportEnv), helperSentinelDepth, timeout) {
		fmt.Fprintln(os.Stderr, "helper: test sentinel did not appear in time")
		return 8
	}
	return 0
}

// helperSentinelDepth 是测试端写给 helper 的“已接管”哨兵行深度。
const helperSentinelDepth = 99

// waitForReportDepth 轮询报告文件直到出现指定 depth 的行。纯文件轮询，不依赖任何
// 测试框架状态，可以在 helper 模式（非 test goroutine）里使用。
func waitForReportDepth(path string, depth int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if data, err := os.ReadFile(path); err == nil {
			for _, raw := range strings.Split(string(data), "\n") {
				if strings.TrimSpace(raw) == "" {
					continue
				}
				var line helperReportLine
				if err := json.Unmarshal([]byte(raw), &line); err == nil && line.Depth == depth {
					return true
				}
			}
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(helperReleasePoll)
	}
}

// helperDuration 读取毫秒数的时长环境变量。
func helperDuration(name string, def time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return time.Duration(n) * time.Millisecond
}

// ignoreSoftTermination 让 helper 对软终止免疫：Unix 忽略 SIGTERM，Windows 忽略
// CTRL_C（os.Interrupt）与 CTRL_BREAK（SIGBREAK）。这正是 T1.08.b 需要的对手：
// 软终止无效时必须升级为强制终止。
func ignoreSoftTermination() {
	signal.Ignore(os.Interrupt, syscall.SIGTERM, syscall.Signal(helperSigbreakNumber))
}

// blockUntilReleased 在 CODEFLOW_HELPER_RELEASE 指定的文件出现前一直阻塞；
// 未设置该变量时永久阻塞（只能被 kill）。用定时轮询而不是阻塞在 select{} 上，
// 是为了在任何情况下都保持一个可运行的计时器。
func blockUntilReleased() {
	path := os.Getenv(helperReleaseEnv)
	for {
		if path != "" {
			if _, err := os.Stat(path); err == nil {
				return
			}
		}
		time.Sleep(helperReleasePoll)
	}
}

// helperExecutable 返回本测试二进制的绝对路径（自我重入用）。
func helperExecutable() string {
	exe, err := os.Executable()
	if err != nil {
		return os.Args[0]
	}
	return exe
}

func helperInt(name string, def int) int {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func helperInt64(name string, def int64) int64 {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return n
}

// ---------------------------------------------------------------------------
// 测试端工具（测试夹具：直接 os/exec 启动 helper，不经 Supervisor——Start 属于
// T1.08.b）
// ---------------------------------------------------------------------------

// testOwnership 返回测试用的固定归属。
func testOwnership() Ownership {
	return Ownership{RunID: "run-t108a", AttemptID: "attempt-t108a", OwnerInstance: "test-instance"}
}

// newHelperCmd 构造 helper 命令（未启动），供需要先接管的测试（如 flood 要先取
// StdoutPipe）使用。
func newHelperCmd(t *testing.T, mode string, extra ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(helperExecutable())
	env := append([]string{helperModeEnv + "=" + mode}, extra...)
	// 显式环境白名单，不继承测试进程环境；SystemRoot 是 Windows 启动进程所需的最小项。
	if v := os.Getenv("SystemRoot"); v != "" {
		env = append(env, "SystemRoot="+v)
	}
	cmd.Env = env
	return cmd
}

// startHelper 启动一个 helper 进程并登记清理。
func startHelper(t *testing.T, mode string, extra ...string) *exec.Cmd {
	t.Helper()
	cmd := newHelperCmd(t, mode, extra...)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper %s: %v", mode, err)
	}
	t.Cleanup(func() { killAndReap(t, cmd) })
	return cmd
}

// killAndReap 强制结束 helper 并回收，确保测试不会残留进程。
func killAndReap(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Errorf("helper pid %d did not exit after Kill", cmd.Process.Pid)
	}
}

// waitForReportLines 轮询报告文件直到出现 want 行（或超时），返回解析后的行。
func waitForReportLines(t *testing.T, path string, want int, timeout time.Duration) []helperReportLine {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lines []helperReportLine
	for {
		got, err := readReportLines(path)
		if err == nil {
			lines = got
			if len(lines) >= want {
				if len(lines) > want {
					t.Fatalf("report has %d lines, want %d: %+v", len(lines), want, lines)
				}
				return lines
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("report %s has %d lines after %s, want %d (lines: %+v, last err: %v)",
				path, len(lines), timeout, want, lines, err)
		}
		time.Sleep(helperReleasePoll)
	}
}

// readReportLines 解析报告文件的每一行 JSON。
func readReportLines(path string) ([]helperReportLine, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []helperReportLine
	for _, raw := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		var line helperReportLine
		if err := json.Unmarshal([]byte(raw), &line); err != nil {
			return out, fmt.Errorf("parse report line %q: %w", raw, err)
		}
		out = append(out, line)
	}
	return out, nil
}

// waitNotSame 轮询直到 Verify 不再报告 same（进程已退出或被复用），返回实测结果；
// 超时也返回最后一次结果（调用方断言“不是 same”时会失败并打印实测值）。
func waitNotSame(t *testing.T, id Identity, timeout time.Duration) VerifyResult {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		res, err := Verify(id)
		if err != nil {
			t.Fatalf("verify pid %d: %v", id.PID, err)
		}
		if res != VerifySame {
			return res
		}
		if time.Now().After(deadline) {
			return res
		}
		time.Sleep(helperReleasePoll)
	}
}

// drainCount 读空 r 并返回读到的字节数。
func drainCount(t *testing.T, r io.Reader) int64 {
	t.Helper()
	n, err := io.Copy(io.Discard, r)
	if err != nil {
		t.Fatalf("drain helper output: %v", err)
	}
	return n
}

// helperDir 为测试准备一个临时目录，返回 (报告文件, release 文件) 路径。
func helperDir(t *testing.T) (report, release string) {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "report.jsonl"), filepath.Join(dir, "release")
}
