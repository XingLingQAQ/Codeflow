package process

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// requireIdentitySupport 在支持进程身份原语的平台上返回 true。在不支持的平台上
// 不断言“跳过”，而是断言能力契约本身：CaptureIdentity/Verify 必须返回
// ErrUnsupportedPlatform 且 Verify 结果必须是 unknown（capability=false，
// 不得假装成功）。
func requireIdentitySupport(t *testing.T) bool {
	t.Helper()
	if Supported() {
		return true
	}
	if _, err := CaptureIdentity(os.Getpid(), testOwnership()); !errors.Is(err, ErrUnsupportedPlatform) {
		t.Fatalf("unsupported platform: CaptureIdentity error = %v, want ErrUnsupportedPlatform", err)
	}
	id := Identity{PID: os.Getpid(), StartToken: "any", Owner: testOwnership()}
	res, err := Verify(id)
	if !errors.Is(err, ErrUnsupportedPlatform) {
		t.Fatalf("unsupported platform: Verify error = %v, want ErrUnsupportedPlatform", err)
	}
	if res != VerifyUnknown {
		t.Fatalf("unsupported platform: Verify result = %s, want unknown", res)
	}
	return false
}

// wantStartTokenPrefix 返回当前平台 StartToken 的期望前缀（记录格式约定）。
func wantStartTokenPrefix() string {
	switch runtime.GOOS {
	case "windows":
		return "windows:"
	case "linux":
		return "linux:"
	}
	return ""
}

func TestSupportedMatchesPlatform(t *testing.T) {
	want := runtime.GOOS == "windows" || runtime.GOOS == "linux"
	if got := Supported(); got != want {
		t.Fatalf("Supported() = %v on %s, want %v", got, runtime.GOOS, want)
	}
}

func TestCaptureIdentitySelf(t *testing.T) {
	if !requireIdentitySupport(t) {
		return
	}
	owner := testOwnership()

	first, err := CaptureIdentity(os.Getpid(), owner)
	if err != nil {
		t.Fatalf("capture identity of self: %v", err)
	}
	if first.PID != os.Getpid() {
		t.Fatalf("identity pid = %d, want %d", first.PID, os.Getpid())
	}
	if strings.TrimSpace(first.StartToken) == "" {
		t.Fatal("start token is empty")
	}
	if prefix := wantStartTokenPrefix(); !strings.HasPrefix(first.StartToken, prefix) {
		t.Fatalf("start token %q does not start with %q", first.StartToken, prefix)
	}
	if first.Owner != owner {
		t.Fatalf("identity owner = %+v, want %+v", first.Owner, owner)
	}
	if first.CapturedAt.IsZero() || first.CapturedAt.Location() != time.UTC {
		t.Fatalf("captured at = %v, want non-zero UTC", first.CapturedAt)
	}

	second, err := CaptureIdentity(os.Getpid(), owner)
	if err != nil {
		t.Fatalf("second capture of self: %v", err)
	}
	if first.StartToken != second.StartToken {
		t.Fatalf("start token changed between captures: %q vs %q", first.StartToken, second.StartToken)
	}

	res, err := Verify(first)
	if err != nil {
		t.Fatalf("verify self: %v", err)
	}
	if res != VerifySame {
		t.Fatalf("verify self = %s, want same", res)
	}
	t.Logf("self identity: pid=%d token=%q", first.PID, first.StartToken)
}

func TestVerifyDetectsReusedPID(t *testing.T) {
	if !requireIdentitySupport(t) {
		return
	}
	live, err := CaptureIdentity(os.Getpid(), testOwnership())
	if err != nil {
		t.Fatalf("capture identity of self: %v", err)
	}

	// 同一个 PID，但启动标记不同 = 该 PID 现在属于另一个进程（PID 复用）。
	reused := live
	reused.StartToken = live.StartToken + "-stale"
	res, err := Verify(reused)
	if err != nil {
		t.Fatalf("verify reused identity: %v", err)
	}
	if res != VerifyReused {
		t.Fatalf("verify with stale start token = %s, want reused", res)
	}
}

// TestVerifyExitedProcess 用 exit 模式的 helper：进程存活期间捕获身份（release 文件
// 未出现前它不会退出），release 后 Wait 回收，再 Verify 必须不是 same。
func TestVerifyExitedProcess(t *testing.T) {
	if !requireIdentitySupport(t) {
		return
	}
	_, release := helperDir(t)
	cmd := startHelper(t, helperModeExit,
		helperCodeEnv+"=7",
		helperReleaseEnv+"="+release,
	)
	id, err := CaptureIdentity(cmd.Process.Pid, testOwnership())
	if err != nil {
		t.Fatalf("capture identity of live helper: %v", err)
	}
	if res, err := Verify(id); err != nil || res != VerifySame {
		t.Fatalf("verify live helper = %s (err %v), want same", res, err)
	}

	if err := os.WriteFile(release, []byte("go\n"), 0o644); err != nil {
		t.Fatalf("write release file: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("helper wait error = %v, want exit status 7", err)
		}
		if got := exitErr.ExitCode(); got != 7 {
			t.Fatalf("helper exit code = %d, want 7", got)
		}
	case <-time.After(20 * time.Second):
		t.Fatalf("helper pid %d did not exit after release", id.PID)
	}

	// Wait 已回收进程：身份必须判为 exited（万一 PID 被复用则是 reused，
	// 但绝不能是 same）。
	res, err := Verify(id)
	if err != nil {
		t.Fatalf("verify after Wait: %v", err)
	}
	if res == VerifySame {
		t.Fatalf("verify after Wait = same, want exited")
	}
	if res != VerifyExited {
		t.Logf("verify after Wait = %s (PID 已被复用)", res)
	}
}

// TestVerifyNeverExistedPIDIsExited 覆盖“PID 根本不存在”的失败路径。
func TestVerifyNeverExistedPIDIsExited(t *testing.T) {
	if !requireIdentitySupport(t) {
		return
	}
	const hugePID = 0x7FFFFFF0 // 远大于任何平台的 pid_max
	id := Identity{PID: hugePID, StartToken: "test:never-existed", Owner: testOwnership()}
	res, err := Verify(id)
	if err != nil {
		t.Fatalf("verify nonexistent pid: %v", err)
	}
	if res != VerifyExited {
		t.Fatalf("verify nonexistent pid = %s, want exited", res)
	}
}

func TestCaptureIdentityRejectsInvalidInput(t *testing.T) {
	if !Supported() {
		return
	}
	if _, err := CaptureIdentity(0, testOwnership()); err == nil {
		t.Fatal("capture identity of pid 0 succeeded, want error")
	}
	if _, err := CaptureIdentity(-1, testOwnership()); err == nil {
		t.Fatal("capture identity of pid -1 succeeded, want error")
	}
	if _, err := CaptureIdentity(os.Getpid(), Ownership{}); err == nil {
		t.Fatal("capture identity without owner succeeded, want error")
	}
}

func TestVerifyRejectsInvalidIdentity(t *testing.T) {
	cases := []struct {
		name string
		id   Identity
	}{
		{"zero pid", Identity{PID: 0, StartToken: "t", Owner: testOwnership()}},
		{"negative pid", Identity{PID: -3, StartToken: "t", Owner: testOwnership()}},
		{"empty token", Identity{PID: 1, StartToken: "  ", Owner: testOwnership()}},
		{"missing owner", Identity{PID: 1, StartToken: "t"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Verify(tc.id)
			if err == nil {
				t.Fatalf("verify %+v succeeded, want error", tc.id)
			}
			if res != VerifyUnknown {
				t.Fatalf("verify %+v = %s, want unknown", tc.id, res)
			}
		})
	}
}

func TestIdentityValidate(t *testing.T) {
	valid := Identity{PID: 42, StartToken: "windows:1", Owner: testOwnership()}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid identity rejected: %v", err)
	}
}

func TestIdentityOwnedBy(t *testing.T) {
	id := Identity{PID: 42, StartToken: "windows:1", Owner: testOwnership()}
	if !id.OwnedBy(testOwnership()) {
		t.Fatal("OwnedBy(same owner) = false, want true")
	}
	other := testOwnership()
	other.OwnerInstance = "another-instance"
	if id.OwnedBy(other) {
		t.Fatal("OwnedBy(different owner instance) = true, want false")
	}
	// owner 未确认时不得 kill：这里只证明判定本身可区分实例。
	restarted := testOwnership()
	restarted.RunID = "run-other"
	if id.OwnedBy(restarted) {
		t.Fatal("OwnedBy(different run) = true, want false")
	}
}

func TestVerifyResultValid(t *testing.T) {
	for _, r := range []VerifyResult{VerifySame, VerifyExited, VerifyReused, VerifyUnknown} {
		if !r.Valid() {
			t.Fatalf("VerifyResult(%q).Valid() = false, want true", r)
		}
		if r.String() != string(r) {
			t.Fatalf("VerifyResult(%q).String() = %q", r, r.String())
		}
	}
	if VerifyResult("bogus").Valid() {
		t.Fatal(`VerifyResult("bogus").Valid() = true, want false`)
	}
}

// TestHelperTreeReportsDescendants 覆盖 §28 T1.08.a 的进程树夹具：depth=2、fanout=2
// 时报告文件恰好 1+2+4=7 个进程，ppid 关系正确，每个 PID 都能捕获身份且 Verify=same；
// release 后逐个 Verify 必须不再是 same（实测值记录在日志里）。
func TestHelperTreeReportsDescendants(t *testing.T) {
	if !requireIdentitySupport(t) {
		return
	}
	report, release := helperDir(t)
	root := startHelper(t, helperModeTree,
		helperDepthEnv+"=2",
		helperFanoutEnv+"=2",
		helperReportEnv+"="+report,
		helperReleaseEnv+"="+release,
	)

	lines := waitForReportLines(t, report, 7, 30*time.Second)
	byPID := make(map[int]helperReportLine, len(lines))
	depthCount := map[int]int{}
	for _, line := range lines {
		if line.PID <= 0 {
			t.Fatalf("report line with non-positive pid: %+v", line)
		}
		if _, dup := byPID[line.PID]; dup {
			t.Fatalf("report contains duplicate pid %d", line.PID)
		}
		byPID[line.PID] = line
		depthCount[line.Depth]++
	}
	if depthCount[2] != 1 || depthCount[1] != 2 || depthCount[0] != 4 {
		t.Fatalf("depth histogram = %v, want map[2:1 1:2 0:4] (lines %+v)", depthCount, lines)
	}
	for _, line := range lines {
		t.Logf("tree report line: pid=%d ppid=%d depth=%d", line.PID, line.PPID, line.Depth)
	}

	var rootLine helperReportLine
	for _, line := range byPID {
		if line.Depth == 2 {
			rootLine = line
		}
	}
	if rootLine.PPID != os.Getpid() {
		t.Fatalf("tree root ppid = %d, want test pid %d", rootLine.PPID, os.Getpid())
	}
	if root.Process.Pid != rootLine.PID {
		t.Fatalf("tree root pid = %d, reported %d", root.Process.Pid, rootLine.PID)
	}
	for _, line := range byPID {
		if line.Depth == 2 {
			continue
		}
		parent, ok := byPID[line.PPID]
		if !ok {
			t.Fatalf("pid %d (depth %d) reports ppid %d which is not in the report: %+v",
				line.PID, line.Depth, line.PPID, lines)
		}
		if parent.Depth != line.Depth+1 {
			t.Fatalf("pid %d (depth %d) parent %d has depth %d, want %d",
				line.PID, line.Depth, parent.PID, parent.Depth, line.Depth+1)
		}
	}

	// 每个 helper 都必须能被捕获身份，并在存活期间 Verify=same。
	ids := make(map[int]Identity, len(byPID))
	for pid := range byPID {
		id, err := CaptureIdentity(pid, testOwnership())
		if err != nil {
			t.Fatalf("capture identity of helper pid %d: %v", pid, err)
		}
		if id.PID != pid {
			t.Fatalf("captured identity pid = %d, want %d", id.PID, pid)
		}
		res, err := Verify(id)
		if err != nil {
			t.Fatalf("verify helper pid %d: %v", pid, err)
		}
		if res != VerifySame {
			t.Fatalf("verify live helper pid %d = %s, want same", pid, res)
		}
		ids[pid] = id
	}

	// release：所有 helper 自行退出（不靠 kill），逐个确认不再是 same。
	if err := os.WriteFile(release, []byte("release\n"), 0o644); err != nil {
		t.Fatalf("write release file: %v", err)
	}
	reaped := make(chan error, 1)
	go func() { reaped <- root.Wait() }()
	select {
	case err := <-reaped:
		if err != nil {
			t.Logf("tree root wait: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatalf("tree root pid %d did not exit after release", rootLine.PID)
	}

	results := make(map[int]VerifyResult, len(ids))
	for pid, id := range ids {
		results[pid] = waitNotSame(t, id, 20*time.Second)
		if results[pid] == VerifySame {
			t.Fatalf("helper pid %d still reports same after release", pid)
		}
	}
	t.Logf("tree pids=%v results after release=%v", sortedPIDs(byPID), results)

	// 清理兜底：万一有 helper 没退出（或 PID 已被复用后又被别人启动），逐个结束。
	for pid, id := range ids {
		res, err := Verify(id)
		if err != nil || res != VerifySame {
			continue
		}
		if p, err := os.FindProcess(pid); err == nil {
			_ = p.Kill()
		}
		if final := waitNotSame(t, id, 10*time.Second); final == VerifySame {
			t.Errorf("helper pid %d is still alive after cleanup", pid)
		}
	}
}

// sortedPIDs 返回稳定顺序的 PID 列表，便于日志比对。
func sortedPIDs(byPID map[int]helperReportLine) []int {
	out := make([]int, 0, len(byPID))
	for pid := range byPID {
		out = append(out, pid)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// TestHelperFloodWritesExactBytes 验证 flood 夹具：测试端持续读 stdout 时恰好收到
// 4 MiB（b 步的输出背压测试依赖这个精确字节数）。
func TestHelperFloodWritesExactBytes(t *testing.T) {
	const want = 4 << 20
	cmd := newHelperCmd(t, helperModeFlood, helperBytesEnv+"="+strconv.Itoa(want))
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("helper stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start flood helper: %v", err)
	}
	t.Cleanup(func() { killAndReap(t, cmd) })

	got := drainCount(t, pipe)
	if err := cmd.Wait(); err != nil {
		t.Fatalf("flood helper wait: %v", err)
	}
	t.Logf("flood helper pid %d wrote %d bytes, want %d", cmd.Process.Pid, got, want)
	if got != want {
		t.Fatalf("flood helper wrote %d bytes, want %d", got, want)
	}
}

// TestHelperIgnoreTermStaysAlive 验证 ignore-term 夹具：软终止后仍然存活
// （Verify=same），随后强制结束并确认不再 same。
func TestHelperIgnoreTermStaysAlive(t *testing.T) {
	if !requireIdentitySupport(t) {
		return
	}
	cmd := startHelper(t, helperModeIgnoreTerm)
	id, err := CaptureIdentity(cmd.Process.Pid, testOwnership())
	if err != nil {
		t.Fatalf("capture identity of ignore-term helper: %v", err)
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		if runtime.GOOS != "windows" {
			t.Fatalf("signal helper with SIGTERM: %v", err)
		}
		// Windows 上 os.Process.Signal 不支持 SIGTERM；真正的软终止
		// （CTRL_BREAK，需要 CREATE_NEW_PROCESS_GROUP + GenerateConsoleCtrlEvent）
		// 属于 T1.08.b，本步不验证投递，只验证进程对软终止免疫后仍在运行。
		t.Logf("windows: os.Process.Signal(SIGTERM) unsupported: %v", err)
	}

	time.Sleep(time.Second)
	res, err := Verify(id)
	if err != nil {
		t.Fatalf("verify helper after soft term: %v", err)
	}
	if res != VerifySame {
		t.Fatalf("verify helper after soft term = %s, want same (must ignore soft termination)", res)
	}

	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill helper: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("helper pid %d did not exit after Kill", id.PID)
	}
	final, err := Verify(id)
	if err != nil {
		t.Fatalf("verify helper after Kill: %v", err)
	}
	if final == VerifySame {
		t.Fatalf("verify helper after Kill = same, want exited/reused")
	}
	t.Logf("ignore-term helper verify after Kill = %s", final)
}
