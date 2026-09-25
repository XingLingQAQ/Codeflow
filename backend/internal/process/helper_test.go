package process

import (
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
//	CODEFLOW_PROCESS_HELPER  helper 模式：tree|flood|ignore-term|exit
//	CODEFLOW_HELPER_DEPTH    tree：本进程还要往下递归几层
//	CODEFLOW_HELPER_FANOUT   tree：每层启动几个子进程
//	CODEFLOW_HELPER_REPORT   tree：{pid,ppid,depth} JSON 行追加写入的报告文件
//	CODEFLOW_HELPER_RELEASE  tree|ignore-term|exit：该文件出现后进程退出
//	CODEFLOW_HELPER_BYTES    flood：向 stdout 写入的字节数
//	CODEFLOW_HELPER_CODE     exit：退出码
const (
	helperModeEnv    = "CODEFLOW_PROCESS_HELPER"
	helperDepthEnv   = "CODEFLOW_HELPER_DEPTH"
	helperFanoutEnv  = "CODEFLOW_HELPER_FANOUT"
	helperReportEnv  = "CODEFLOW_HELPER_REPORT"
	helperReleaseEnv = "CODEFLOW_HELPER_RELEASE"
	helperBytesEnv   = "CODEFLOW_HELPER_BYTES"
	helperCodeEnv    = "CODEFLOW_HELPER_CODE"

	helperModeTree       = "tree"
	helperModeFlood      = "flood"
	helperModeIgnoreTerm = "ignore-term"
	helperModeExit       = "exit"

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
	case helperModeExit:
		// 未设置 CODEFLOW_HELPER_RELEASE 时立即退出；设置了就先等该文件出现，
		// 这样测试能在进程存活期间捕获身份，再确定性地观察退出。
		if os.Getenv(helperReleaseEnv) != "" {
			blockUntilReleased()
		}
		return helperInt(helperCodeEnv, 0)
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
