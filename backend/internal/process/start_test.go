package process

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/policy"
	"github.com/codeflow/backend/internal/policy/policytesting"
)

// 本文件的测试全部经 Supervisor 启动 helper（测试二进制自我重入），并且全部在结束
// 时确认没有残留进程：每个 Start 都登记了强制清理。

const (
	testWaitShort = 5 * time.Second
	testWaitLong  = 20 * time.Second
)

// helperEnv 构造 helper 的显式环境白名单（不继承测试进程环境）。SystemRoot 是
// Windows 上启动进程所需的最小项，其他平台不需要。
func helperEnv(mode string, extra ...string) []string {
	env := append([]string{helperModeEnv + "=" + mode}, extra...)
	if v := os.Getenv("SystemRoot"); v != "" {
		env = append(env, "SystemRoot="+v)
	}
	return env
}

// testSupervisor 返回 OwnerInstance 与 testOwnership 一致的 supervisor。
func testSupervisor() Supervisor {
	return NewSupervisor(SupervisorOptions{OwnerInstance: testOwnership().OwnerInstance})
}

// allowProcessStart 安装只放行 process_start 的策略（T0.09.c 起无策略必拒绝）。
func allowProcessStart(t *testing.T) {
	t.Helper()
	policytesting.AllowForTest(t, policy.OperationProcessStart)
}

// supervisorSpec 构造一个以 helper 为目标的合法 Spec。
func supervisorSpec(t *testing.T, mode string, extra ...string) Spec {
	t.Helper()
	return Spec{
		Path:          helperExecutable(),
		Dir:           t.TempDir(),
		Env:           helperEnv(mode, extra...),
		Owner:         testOwnership(),
		GracePeriod:   DefaultGracePeriod,
		MaxSpoolBytes: DefaultMaxSpoolBytes,
	}
}

// startHandle 启动进程并登记强制清理，返回句柄。
func startHandle(t *testing.T, spec Spec) Handle {
	t.Helper()
	allowProcessStart(t)
	h, err := testSupervisor().Start(context.Background(), spec)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { reapHandle(t, h) })
	return h
}

// reapHandle 保证测试结束时进程树一定被结束：Cancel(force) 对已经结束的进程是
// 空操作（返回 nil），对还在跑的进程是整树强杀；随后等终态。
func reapHandle(t *testing.T, h Handle) {
	t.Helper()
	if err := h.Cancel(context.Background(), CancelForce); err != nil && !errors.Is(err, ErrIdentityUnconfirmed) {
		t.Errorf("cleanup: force cancel pid %d: %v", h.Identity().PID, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), testWaitShort)
	defer cancel()
	if _, err := h.Wait(ctx); err != nil {
		t.Errorf("cleanup: pid %d did not reach a terminal state: %v", h.Identity().PID, err)
	}
}

// waitExit 在超时内等待终态，超时即失败。
func waitExit(t *testing.T, h Handle, timeout time.Duration) Exit {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	exit, err := h.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait(pid %d): %v", h.Identity().PID, err)
	}
	return exit
}

// captureAll 为报告里的每个 PID 捕获身份（要求进程存活）。
func captureAll(t *testing.T, lines []helperReportLine) []Identity {
	t.Helper()
	ids := make([]Identity, 0, len(lines))
	for _, line := range lines {
		id, err := CaptureIdentity(line.PID, testOwnership())
		if err != nil {
			t.Fatalf("CaptureIdentity(pid %d): %v", line.PID, err)
		}
		ids = append(ids, id)
	}
	return ids
}

// waitSame 轮询直到 Verify 报告 same（进程确实在运行），超时即失败。
func waitSame(t *testing.T, id Identity, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		res, err := Verify(id)
		if err != nil {
			t.Fatalf("Verify(pid %d): %v", id.PID, err)
		}
		if res == VerifySame {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("pid %d verify = %s after %s, want same (process is not running)", id.PID, res, timeout)
		}
		time.Sleep(helperReleasePoll)
	}
}

// assertNotSameAll 断言所有身份都不再是同一个进程（即全部已被终止）。
func assertNotSameAll(t *testing.T, ids []Identity, timeout time.Duration) map[int]VerifyResult {
	t.Helper()
	results := make(map[int]VerifyResult, len(ids))
	for _, id := range ids {
		res := waitNotSame(t, id, timeout)
		results[id.PID] = res
		if res == VerifySame {
			t.Errorf("pid %d is still the same process after %s (want it terminated)", id.PID, timeout)
		}
	}
	return results
}

// ---------------------------------------------------------------------------
// 闸控与前置校验
// ---------------------------------------------------------------------------

// TestStartRequiresPolicy：没有策略（未 AllowForTest）时 Start 必须被拒绝，并且
// 报告文件从未出现——证明一个进程都没有被创建。
func TestStartRequiresPolicy(t *testing.T) {
	report, release := helperDir(t)
	spec := supervisorSpec(t, helperModeTree,
		helperDepthEnv+"=0",
		helperFanoutEnv+"=0",
		helperReportEnv+"="+report,
		helperReleaseEnv+"="+release,
	)
	// 不调用 allowProcessStart：T0.09.c 起无策略必拒绝。
	h, err := testSupervisor().Start(context.Background(), spec)
	if err == nil {
		if h != nil {
			reapHandle(t, h)
		}
		t.Fatalf("Start() = handle, nil error; want policy denial")
	}
	var denied *policy.DeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("Start() error = %v, want *policy.DeniedError", err)
	}
	if denied.Decision.Operation != policy.OperationProcessStart {
		t.Fatalf("denied operation = %q, want %q", denied.Decision.Operation, policy.OperationProcessStart)
	}
	// 给被拒绝的启动留出“万一真的启动了”的时间窗，再断言报告文件不存在。
	time.Sleep(300 * time.Millisecond)
	if _, statErr := os.Stat(report); !os.IsNotExist(statErr) {
		t.Fatalf("report file %s exists (stat err %v): a process was started despite policy denial", report, statErr)
	}
}

// TestStartRejectsInvalidSpec：非法 Spec 在策略判定之前被拒绝——既不能创建进程，
// 错误也不能是策略拒绝（那说明顺序反了）。
func TestStartRejectsInvalidSpec(t *testing.T) {
	report, release := helperDir(t)
	cases := []struct {
		name   string
		mutate func(*Spec)
	}{
		{"relative path", func(s *Spec) { s.Path = "helper.exe" }},
		{"uncleaned path", func(s *Spec) { s.Path = testUncleaned() }},
		{"empty dir", func(s *Spec) { s.Dir = "" }},
		{"bad env entry", func(s *Spec) { s.Env = []string{"NOT_A_PAIR"} }},
		{"negative grace", func(s *Spec) { s.GracePeriod = -time.Second }},
		{"negative spool", func(s *Spec) { s.MaxSpoolBytes = -1 }},
		{"relative marker", func(s *Spec) { s.MarkerPath = "marker.json" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := supervisorSpec(t, helperModeTree,
				helperDepthEnv+"=0", helperFanoutEnv+"=0",
				helperReportEnv+"="+report, helperReleaseEnv+"="+release)
			tc.mutate(&spec)
			h, err := testSupervisor().Start(context.Background(), spec)
			if err == nil {
				reapHandle(t, h)
				t.Fatalf("Start() = handle, nil error; want spec validation error")
			}
			var denied *policy.DeniedError
			if errors.As(err, &denied) {
				t.Fatalf("Start() error = %v; want spec error before policy evaluation", err)
			}
			if !strings.HasPrefix(err.Error(), "process:") {
				t.Fatalf("Start() error = %q; want a process-prefixed error", err)
			}
			if _, statErr := os.Stat(report); !os.IsNotExist(statErr) {
				t.Fatalf("report file %s exists (stat err %v): a process was started for an invalid spec", report, statErr)
			}
		})
	}
}

// TestStartRejectsForeignOwner：归属属于别的实例时拒绝，且在策略判定之前。
func TestStartRejectsForeignOwner(t *testing.T) {
	report, release := helperDir(t)
	spec := supervisorSpec(t, helperModeTree,
		helperDepthEnv+"=0", helperFanoutEnv+"=0",
		helperReportEnv+"="+report, helperReleaseEnv+"="+release)
	spec.Owner.OwnerInstance = "another-instance"
	h, err := testSupervisor().Start(context.Background(), spec)
	if err == nil {
		reapHandle(t, h)
		t.Fatalf("Start() = handle, nil error; want foreign owner rejection")
	}
	if !errors.Is(err, ErrForeignOwnerInstance) {
		t.Fatalf("Start() error = %v, want ErrForeignOwnerInstance", err)
	}
	var denied *policy.DeniedError
	if errors.As(err, &denied) {
		t.Fatalf("Start() error = %v; want owner rejection before policy evaluation", err)
	}
	if _, statErr := os.Stat(report); !os.IsNotExist(statErr) {
		t.Fatalf("report file %s exists (stat err %v): a process was started for a foreign owner", report, statErr)
	}

	// supervisor 自身没有实例标识时同样拒绝。
	if _, err := NewSupervisor(SupervisorOptions{}).Start(context.Background(), supervisorSpec(t, helperModeExit)); !errors.Is(err, ErrMissingOwnerInstance) {
		t.Fatalf("Start() with empty supervisor instance error = %v, want ErrMissingOwnerInstance", err)
	}
}

// TestStartFailureAfterLaunch：启动之后的步骤失败（这里是 marker 写不进去）必须返回
// 错误并结束已经启动的进程。
//
// 观测口径说明：失败点在进程刚被恢复执行的瞬间，helper 还没来得及跑完运行时初始化，
// 因此它不会写出任何报告行——这一点本身就说明进程被立即结束了（否则它会像其他测试
// 里那样在毫秒级内写出报告行）。本测试无法据此拿到 PID，所以另外用一个“同样配置、
// 只是 marker 路径合法”的对照启动，证明这套 Spec 确实能启动进程、失败只出在 marker。
func TestStartFailureAfterLaunch(t *testing.T) {
	report, release := helperDir(t)
	spec := supervisorSpec(t, helperModeTree,
		helperDepthEnv+"=0", helperFanoutEnv+"=0",
		helperReportEnv+"="+report, helperReleaseEnv+"="+release)
	// 绝对且已清理，但目录不存在：写 marker 必然失败。
	spec.MarkerPath = filepath.Join(filepath.Dir(report), "missing-dir", "marker.json")
	allowProcessStart(t)
	h, err := testSupervisor().Start(context.Background(), spec)
	if err == nil {
		reapHandle(t, h)
		t.Fatalf("Start() = handle, nil error; want marker write failure")
	}
	if !strings.HasPrefix(err.Error(), "process:") {
		t.Fatalf("Start() error = %q, want a process-prefixed error", err)
	}
	time.Sleep(500 * time.Millisecond)
	if _, statErr := os.Stat(report); !os.IsNotExist(statErr) {
		t.Fatalf("report file %s exists (stat err %v): the process survived a failed Start", report, statErr)
	}

	// 对照：同一份 Spec 只把 marker 换成合法路径，必须正常启动并产出报告行。
	controlReport, controlRelease := helperDir(t)
	control := supervisorSpec(t, helperModeTree,
		helperDepthEnv+"=0", helperFanoutEnv+"=0",
		helperReportEnv+"="+controlReport, helperReleaseEnv+"="+controlRelease)
	control.MarkerPath = filepath.Join(filepath.Dir(controlReport), "marker.json")
	controlHandle := startHandle(t, control)
	waitForReportLines(t, controlReport, 1, testWaitLong)
	if res, verr := Verify(controlHandle.Identity()); verr != nil || res != VerifySame {
		t.Fatalf("control start: Verify = (%v, %v), want (same, nil)", res, verr)
	}
}

// ---------------------------------------------------------------------------
// 身份
// ---------------------------------------------------------------------------

// TestHandleIdentityMatchesCapture：Handle.Identity() 必须与按 PID 重新捕获的身份
// 一致，并且运行中 Verify = same。
func TestHandleIdentityMatchesCapture(t *testing.T) {
	report, release := helperDir(t)
	spec := supervisorSpec(t, helperModeTree,
		helperDepthEnv+"=0", helperFanoutEnv+"=0",
		helperReportEnv+"="+report, helperReleaseEnv+"="+release)
	h := startHandle(t, spec)
	waitForReportLines(t, report, 1, testWaitLong)

	id := h.Identity()
	if err := id.Validate(); err != nil {
		t.Fatalf("Identity().Validate() = %v", err)
	}
	if id.PID != h.Identity().PID {
		t.Fatalf("Identity() pid changed between calls: %d vs %d", id.PID, h.Identity().PID)
	}
	fresh, err := CaptureIdentity(id.PID, testOwnership())
	if err != nil {
		t.Fatalf("CaptureIdentity(pid %d): %v", id.PID, err)
	}
	if fresh.StartToken != id.StartToken {
		t.Fatalf("start token = %q, want %q (Handle.Identity must match CaptureIdentity)", fresh.StartToken, id.StartToken)
	}
	if !id.OwnedBy(testOwnership()) {
		t.Fatalf("identity owner = %+v, want %+v", id.Owner, testOwnership())
	}
	if res, err := Verify(id); err != nil || res != VerifySame {
		t.Fatalf("Verify(identity) = (%v, %v), want (same, nil)", res, err)
	}
	// 报告文件里的 PID 就是被监督的 PID。
	lines := waitForReportLines(t, report, 1, testWaitLong)
	if lines[0].PID != id.PID {
		t.Fatalf("report pid %d, identity pid %d", lines[0].PID, id.PID)
	}
}

// ---------------------------------------------------------------------------
// 取消：整树强杀 / 软终止升级 / 协作式软终止
// ---------------------------------------------------------------------------

// TestForceCancelKillsWholeTree：depth=2、fanout=2 的 7 个进程全部被终止。
func TestForceCancelKillsWholeTree(t *testing.T) {
	report, release := helperDir(t)
	spec := supervisorSpec(t, helperModeTree,
		helperDepthEnv+"=2",
		helperFanoutEnv+"=2",
		helperReportEnv+"="+report,
		helperReleaseEnv+"="+release,
	)
	h := startHandle(t, spec)
	lines := waitForReportLines(t, report, 7, testWaitLong)
	ids := captureAll(t, lines)
	for _, id := range ids {
		if res, err := Verify(id); err != nil || res != VerifySame {
			t.Fatalf("before cancel: Verify(pid %d) = (%v, %v), want (same, nil)", id.PID, res, err)
		}
	}

	started := time.Now()
	if err := h.Cancel(context.Background(), CancelForce); err != nil {
		t.Fatalf("Cancel(force): %v", err)
	}
	results := assertNotSameAll(t, ids, testWaitShort)
	t.Logf("force cancel: %d pids terminated in %s, verify results: %v", len(ids), time.Since(started), results)

	exit := waitExit(t, h, testWaitShort)
	if exit.Reason != ExitCancelled {
		t.Errorf("Exit.Reason = %q, want %q", exit.Reason, ExitCancelled)
	}
	if !exit.Forced {
		t.Errorf("Exit.Forced = false, want true (force cancel was requested)")
	}
	if exit.Code != nil {
		t.Errorf("Exit.Code = %d, want nil (forced termination has no meaningful code)", *exit.Code)
	}
}

// TestSoftCancelEscalatesToForce：ignore-term 无视软终止，超过 GracePeriod 必须升级。
func TestSoftCancelEscalatesToForce(t *testing.T) {
	const grace = 300 * time.Millisecond
	report, release := helperDir(t)
	spec := supervisorSpec(t, helperModeResistsTerm,
		helperReportEnv+"="+report,
		helperReleaseEnv+"="+release,
	)
	spec.GracePeriod = grace
	h := startHandle(t, spec)
	waitForReportLines(t, report, 1, testWaitLong)

	started := time.Now()
	softErr := h.Cancel(context.Background(), CancelSoft)
	exit := waitExit(t, h, testWaitShort)
	elapsed := time.Since(started)
	t.Logf("soft cancel on resists-term: cancel err = %v, exit = %+v, elapsed = %s", softErr, exit, elapsed)

	if elapsed < grace {
		t.Errorf("process ended after %s, want >= grace period %s (soft cancel must not be an instant kill)", elapsed, grace)
	}
	if elapsed >= testWaitShort {
		t.Errorf("process ended after %s, want < %s (escalation must fire)", elapsed, testWaitShort)
	}
	if exit.Reason != ExitCancelled {
		t.Errorf("Exit.Reason = %q, want %q", exit.Reason, ExitCancelled)
	}
	if !exit.Forced {
		t.Errorf("Exit.Forced = false, want true (soft termination was ignored, escalation must be recorded)")
	}
	if softErr != nil && !errors.Is(softErr, ErrSoftTerminationFailed) {
		t.Errorf("Cancel(soft) error = %v, want nil or ErrSoftTerminationFailed", softErr)
	}
}

// TestSoftCancelHonoredWhenCooperative：协作式进程收到软终止后自行退出，不升级。
//
// Windows 上没有控制台的调用方无法投递 CTRL_BREAK（GenerateConsoleCtrlEvent 会失败）。
// 这时测试如实记录原因，并断言“升级为 force 仍然结束了进程”——这个平台限制决定
// Windows 适配器的 cancel_graceful 能力能否声明。
func TestSoftCancelHonoredWhenCooperative(t *testing.T) {
	report, release := helperDir(t)
	spec := supervisorSpec(t, helperModeGraceful,
		helperReportEnv+"="+report,
		helperReleaseEnv+"="+release,
	)
	h := startHandle(t, spec)
	// helper 在装好软终止处理器之后才写报告行：等它出现再取消，避免抢跑。
	waitForReportLines(t, report, 1, testWaitLong)

	started := time.Now()
	softErr := h.Cancel(context.Background(), CancelSoft)
	exit := waitExit(t, h, testWaitShort)
	elapsed := time.Since(started)

	if softErr == nil {
		t.Logf("soft cancel delivered: exit = %+v after %s", exit, elapsed)
		if exit.Forced {
			t.Errorf("Exit.Forced = true, want false (cooperative process must exit within the grace period)")
		}
		if exit.Reason != ExitCancelled {
			t.Errorf("Exit.Reason = %q, want %q", exit.Reason, ExitCancelled)
		}
		if exit.Code == nil || *exit.Code != 0 {
			t.Errorf("Exit.Code = %v, want 0 (helper exits 0 on soft termination)", exit.Code)
		}
		if elapsed >= DefaultGracePeriod {
			t.Errorf("process ended after %s, want < grace period %s", elapsed, DefaultGracePeriod)
		}
		return
	}
	// 软终止没能投递：如实记录平台限制，断言升级为 force 仍能结束进程。
	t.Logf("soft termination was not deliverable in this environment (platform limitation, "+
		"see receipt): %v; elapsed %s, exit %+v", softErr, elapsed, exit)
	if !errors.Is(softErr, ErrSoftTerminationFailed) {
		t.Fatalf("Cancel(soft) error = %v, want ErrSoftTerminationFailed", softErr)
	}
	if !exit.Forced {
		t.Errorf("Exit.Forced = false, want true (escalation must have ended the process)")
	}
	if exit.Reason != ExitCancelled {
		t.Errorf("Exit.Reason = %q, want %q", exit.Reason, ExitCancelled)
	}
}

// ---------------------------------------------------------------------------
// 自然退出与退出码
// ---------------------------------------------------------------------------

// TestNaturalExitCode：自然退出必须是 exited + 实际退出码，且没有强制终止。
func TestNaturalExitCode(t *testing.T) {
	t.Run("exit code 3", func(t *testing.T) {
		report, release := helperDir(t)
		spec := supervisorSpec(t, helperModeExit,
			helperCodeEnv+"=3",
			helperReportEnv+"="+report,
			helperReleaseEnv+"="+release,
		)
		h := startHandle(t, spec)
		if res, err := Verify(h.Identity()); err != nil || res != VerifySame {
			t.Fatalf("running: Verify = (%v, %v), want (same, nil)", res, err)
		}
		// 释放：helper 自行退出。
		releaseHelper(t, release)
		exit := waitExit(t, h, testWaitShort)
		if exit.Reason != ExitExited {
			t.Errorf("Exit.Reason = %q, want %q", exit.Reason, ExitExited)
		}
		if exit.Forced {
			t.Errorf("Exit.Forced = true, want false")
		}
		if exit.Code == nil || *exit.Code != 3 {
			t.Errorf("Exit.Code = %v, want 3", exit.Code)
		}
		if exit.SpoolTruncatedBytes != 0 {
			t.Errorf("Exit.SpoolTruncatedBytes = %d, want 0", exit.SpoolTruncatedBytes)
		}
	})

	t.Run("immediate exit", func(t *testing.T) {
		// 进程可能在 CaptureIdentity 之前就退出：Start 仍必须成功（身份可从进程对象
		// 读出），并给出真实终态。
		spec := supervisorSpec(t, helperModeExit, helperCodeEnv+"=0")
		h := startHandle(t, spec)
		exit := waitExit(t, h, testWaitShort)
		if exit.Reason != ExitExited || exit.Forced {
			t.Errorf("Exit = %+v, want reason=exited forced=false", exit)
		}
		if exit.Code == nil || *exit.Code != 0 {
			t.Errorf("Exit.Code = %v, want 0", exit.Code)
		}
	})
}

// TestNaturalExitReapsDescendants：主进程自然退出后，残留后代必须被清理（不留孤儿），
// 且 Wait 不会因为后代还握着输出管道而挂住。
func TestNaturalExitReapsDescendants(t *testing.T) {
	report, release := helperDir(t)
	spec := supervisorSpec(t, helperModeSpawnAndOut,
		helperReportEnv+"="+report,
		helperReleaseEnv+"="+release,
		helperTimeoutEnv+"=10000",
	)
	h := startHandle(t, spec)
	lines := waitForReportLines(t, report, 2, testWaitLong)
	rootPID := h.Identity().PID
	descendants := make([]Identity, 0, len(lines))
	for _, line := range lines {
		if line.PID == rootPID || line.Depth == helperSentinelDepth {
			continue
		}
		id, err := CaptureIdentity(line.PID, testOwnership())
		if err != nil {
			t.Fatalf("CaptureIdentity(descendant pid %d): %v", line.PID, err)
		}
		descendants = append(descendants, id)
	}
	if len(descendants) == 0 {
		t.Fatalf("report %+v has no descendant line (root pid %d)", lines, rootPID)
	}
	for _, id := range descendants {
		if res, err := Verify(id); err != nil || res != VerifySame {
			t.Fatalf("descendant pid %d should be alive before the root exits: (%v, %v)", id.PID, res, err)
		}
	}
	// 测试侧已经确认过后代活着，写哨兵放主进程退出（见 helperSpawnAndExit）。
	writeSentinel(t, report)

	started := time.Now()
	exit := waitExit(t, h, testWaitLong)
	t.Logf("root exited naturally after %s: %+v; cleaning %d descendant(s)", time.Since(started), exit, len(descendants))
	if exit.Reason != ExitExited {
		t.Errorf("Exit.Reason = %q, want %q", exit.Reason, ExitExited)
	}
	if exit.Forced {
		t.Errorf("Exit.Forced = true, want false")
	}
	results := assertNotSameAll(t, descendants, testWaitShort)
	t.Logf("descendant verify results after Wait returned: %v", results)
}

// recordingBinding 是平台绑定的替身：只记录收到过哪类终止请求。
type recordingBinding struct {
	mu    sync.Mutex
	soft  int
	force int
}

func (b *recordingBinding) softTerminate() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.soft++
	return nil
}

func (b *recordingBinding) forceTerminate() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.force++
	return nil
}

func (b *recordingBinding) release() {}

func (b *recordingBinding) counts() (int, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.soft, b.force
}

// TestCancelRefusesWhenIdentityNotConfirmed：身份无法确认（PID 还活着但启动标记不同，
// 即 PID 已被复用）时绝不发信号，状态记为 lost（§27.5 第 6 条）。
//
// 用“换掉启动标记”的方式构造 reused，而不必真的等 PID 被系统复用：Verify 的判定
// 依据正是启动标记，两者等价且可确定性复现。
func TestCancelRefusesWhenIdentityNotConfirmed(t *testing.T) {
	report, release := helperDir(t)
	spec := supervisorSpec(t, helperModeTree,
		helperDepthEnv+"=0", helperFanoutEnv+"=0",
		helperReportEnv+"="+report, helperReleaseEnv+"="+release)
	real := startHandle(t, spec)
	waitForReportLines(t, report, 1, testWaitLong)

	binding := &recordingBinding{}
	ph := &procHandle{
		id: real.Identity(),
		// 这个 PID 现在“属于别的进程”：启动标记对不上。
		binding:  binding,
		stdout:   newRingSpool(1024),
		stderr:   newRingSpool(1024),
		waitDone: make(chan struct{}),
	}
	ph.id.StartToken += "-another-process"

	for _, mode := range []CancelMode{CancelSoft, CancelForce} {
		if err := ph.Cancel(context.Background(), mode); !errors.Is(err, ErrIdentityUnconfirmed) {
			t.Fatalf("Cancel(%s) with reused identity = %v, want ErrIdentityUnconfirmed", mode, err)
		}
	}
	if soft, force := binding.counts(); soft != 0 || force != 0 {
		t.Fatalf("binding received %d soft / %d force signals, want 0/0 (identity unconfirmed)", soft, force)
	}
	if res, err := Verify(real.Identity()); err != nil || res != VerifySame {
		t.Fatalf("target pid %d verify = (%v, %v), want (same, nil): it must be untouched", real.Identity().PID, res, err)
	}

	// 身份丢失的句柄：终态必须是 lost，且不能编造退出码。
	ph.finish(nil)
	exit, err := ph.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if exit.Reason != ExitLost {
		t.Errorf("Exit.Reason = %q, want %q", exit.Reason, ExitLost)
	}
	if exit.Code != nil {
		t.Errorf("Exit.Code = %v, want nil (a lost process has no known code)", *exit.Code)
	}
	if err := ph.Cancel(context.Background(), CancelForce); err != nil {
		t.Errorf("Cancel(force) after lost = %v, want nil (idempotent)", err)
	}
}

// ---------------------------------------------------------------------------
// 输出排空与有界 spool
// ---------------------------------------------------------------------------

// TestFloodNoDeadlockWithoutReader：32 MiB 输出 + 1 MiB 上限 + 没有任何读取者，
// 进程必须正常退出（不因管道写满而死锁），截断字节精确，保留的是输出尾部。
func TestFloodNoDeadlockWithoutReader(t *testing.T) {
	const (
		total    = 32 << 20
		maxSpool = 1 << 20
	)
	spec := supervisorSpec(t, helperModeFlood, helperBytesEnv+"="+strconv.Itoa(total))
	spec.MaxSpoolBytes = maxSpool
	h := startHandle(t, spec)

	started := time.Now()
	exit := waitExit(t, h, 10*time.Second)
	t.Logf("flood: process exited after %s with %+v", time.Since(started), exit)

	if exit.Reason != ExitExited {
		t.Errorf("Exit.Reason = %q, want %q", exit.Reason, ExitExited)
	}
	if exit.Code == nil || *exit.Code != 0 {
		t.Errorf("Exit.Code = %v, want 0", exit.Code)
	}
	if want := int64(total - maxSpool); exit.SpoolTruncatedBytes != want {
		t.Errorf("Exit.SpoolTruncatedBytes = %d, want %d", exit.SpoolTruncatedBytes, want)
	}
	data, truncated := h.Output().Snapshot()
	if int64(len(data)) != maxSpool {
		t.Errorf("stdout snapshot length = %d, want %d", len(data), maxSpool)
	}
	if truncated != int64(total-maxSpool) {
		t.Errorf("stdout truncated = %d, want %d", truncated, total-maxSpool)
	}
	// 保留的必须是输出的最后一段。
	offset := total - len(data)
	for i := 0; i < len(data); i++ {
		if want := helperFloodByte(offset + i); data[i] != want {
			t.Fatalf("stdout snapshot[%d] = %q, want %q (snapshot is not the tail of the output)",
				i, data[i], want)
		}
	}
	if stderrData, stderrTrunc := h.Stderr().Snapshot(); len(stderrData) != 0 || stderrTrunc != 0 {
		t.Errorf("stderr snapshot = (%d bytes, %d truncated), want (0, 0)", len(stderrData), stderrTrunc)
	}
}

// helperFloodByte 复现 helperFlood 写入的第 i 个字节（与 helper 的实现保持一致：
// 每 64 KiB 一块，块内按 26 周期循环，因此整体周期是 64 KiB 与 26 的复合）。
func helperFloodByte(i int) byte { return byte('a' + (i%(64<<10))%26) }

// ---------------------------------------------------------------------------
// Marker
// ---------------------------------------------------------------------------

const markerCanaryValue = "canary-env-value-do-not-leak-3f9a"

// TestMarkerWrittenAndUpdated：marker 启动后写身份、退出后更新终态，且绝不泄漏
// 环境变量值与参数内容。
func TestMarkerWrittenAndUpdated(t *testing.T) {
	report, release := helperDir(t)
	marker := filepath.Join(filepath.Dir(report), "marker.json")
	const argCanary = "arg-canary-value-do-not-leak-77c1"
	spec := supervisorSpec(t, helperModeExit,
		helperCodeEnv+"=0",
		helperReportEnv+"="+report,
		helperReleaseEnv+"="+release,
		"CODEFLOW_TEST_CANARY="+markerCanaryValue,
	)
	spec.MarkerPath = marker
	spec.Args = []string{"--token", argCanary}
	h := startHandle(t, spec)
	// helper 用 release 文件阻塞（不写报告行），进程存活的证据是身份仍为 same。
	waitSame(t, h.Identity(), testWaitLong)

	raw := readFileEventually(t, marker, testWaitShort)
	assertNoCanary(t, "start marker", raw)
	start := decodeMarker(t, raw)
	if start.Identity.PID != h.Identity().PID || start.Identity.StartToken != h.Identity().StartToken {
		t.Errorf("marker identity = %+v, want pid %d token %q", start.Identity, h.Identity().PID, h.Identity().StartToken)
	}
	if start.Owner != testOwnership() {
		t.Errorf("marker owner = %+v, want %+v", start.Owner, testOwnership())
	}
	if start.Path != spec.Path {
		t.Errorf("marker path = %q, want %q", start.Path, spec.Path)
	}
	if start.ArgsCount != len(spec.Args) {
		t.Errorf("marker args_count = %d, want %d", start.ArgsCount, len(spec.Args))
	}
	if start.StartedAt.IsZero() {
		t.Errorf("marker started_at is zero")
	}
	if start.Reason != "" || start.FinishedAt != nil {
		t.Errorf("start marker already has terminal fields: %+v", start)
	}

	releaseHelper(t, release)
	exit := waitExit(t, h, testWaitShort)
	if exit.Reason != ExitExited || exit.Code == nil || *exit.Code != 0 {
		t.Fatalf("Exit = %+v, want exited with code 0", exit)
	}

	raw = readFileEventually(t, marker, testWaitShort)
	assertNoCanary(t, "finish marker", raw)
	finish := decodeMarker(t, raw)
	if finish.Reason != ExitExited {
		t.Errorf("marker reason = %q, want %q", finish.Reason, ExitExited)
	}
	if finish.Code == nil || *finish.Code != 0 {
		t.Errorf("marker code = %v, want 0", finish.Code)
	}
	if finish.Forced {
		t.Errorf("marker forced = true, want false")
	}
	if finish.FinishedAt == nil || finish.FinishedAt.IsZero() {
		t.Errorf("marker finished_at is missing")
	}
	// 原子更新必须保留启动时写下的身份信息。
	if finish.Identity.PID != start.Identity.PID || finish.Identity.StartToken != start.Identity.StartToken {
		t.Errorf("finish marker identity = %+v, want %+v", finish.Identity, start.Identity)
	}
	if !finish.StartedAt.Equal(start.StartedAt) {
		t.Errorf("finish marker started_at = %s, want %s", finish.StartedAt, start.StartedAt)
	}
}

// assertNoCanary 断言 marker 里没有凭据载体（env 值、参数内容）。
func assertNoCanary(t *testing.T, label string, raw []byte) {
	t.Helper()
	for _, secret := range []string{markerCanaryValue, "arg-canary-value-do-not-leak-77c1", "CODEFLOW_TEST_CANARY"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("%s leaks %q: %s", label, secret, raw)
		}
	}
}

func decodeMarker(t *testing.T, raw []byte) markerRecord {
	t.Helper()
	var rec markerRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatalf("decode marker %s: %v", raw, err)
	}
	return rec
}

// readFileEventually 轮询直到文件可读（marker 由 supervisor 原子写入）。
func readFileEventually(t *testing.T, path string, timeout time.Duration) []byte {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		data, err := os.ReadFile(path)
		if err == nil {
			return data
		}
		if time.Now().After(deadline) {
			t.Fatalf("read %s: %v", path, err)
		}
		time.Sleep(helperReleasePoll)
	}
}

// ---------------------------------------------------------------------------
// 幂等与重复调用
// ---------------------------------------------------------------------------

// TestWaitAndCancelIdempotent：Wait 可重复且结果一致；ctx 到期不改终态；进程结束后
// Cancel 返回 nil。
func TestWaitAndCancelIdempotent(t *testing.T) {
	report, release := helperDir(t)
	spec := supervisorSpec(t, helperModeExit,
		helperCodeEnv+"=0",
		helperReportEnv+"="+report,
		helperReleaseEnv+"="+release,
	)
	h := startHandle(t, spec)
	waitSame(t, h.Identity(), testWaitLong)

	// ctx 到期只结束本次等待：不能伪造终态，也不能把进程当成已退出。
	shortCtx, cancelShort := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancelShort()
	if exit, err := h.Wait(shortCtx); err == nil {
		t.Fatalf("Wait with expired ctx = (%+v, nil), want ctx error", exit)
	} else if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait with expired ctx error = %v, want context.DeadlineExceeded", err)
	}
	if res, err := Verify(h.Identity()); err != nil || res != VerifySame {
		t.Fatalf("after Wait timeout: Verify = (%v, %v), want (same, nil)", res, err)
	}

	releaseHelper(t, release)
	first := waitExit(t, h, testWaitShort)
	for i := 0; i < 3; i++ {
		again, err := h.Wait(context.Background())
		if err != nil {
			t.Fatalf("Wait #%d: %v", i+2, err)
		}
		if again.Reason != first.Reason || again.Forced != first.Forced ||
			again.SpoolTruncatedBytes != first.SpoolTruncatedBytes || !sameCode(again.Code, first.Code) {
			t.Fatalf("Wait #%d = %+v, want %+v", i+2, again, first)
		}
	}
	// 结束后 Cancel 幂等返回 nil，且不会把终态改坏。
	for _, mode := range []CancelMode{CancelSoft, CancelForce} {
		if err := h.Cancel(context.Background(), mode); err != nil {
			t.Errorf("Cancel(%s) after exit = %v, want nil", mode, err)
		}
	}
	after, err := h.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait after Cancel: %v", err)
	}
	if after.Reason != first.Reason || after.Forced != first.Forced {
		t.Fatalf("Wait after Cancel = %+v, want %+v", after, first)
	}
	// 非法模式必须被拒绝。
	if err := h.Cancel(context.Background(), CancelMode("kill")); !errors.Is(err, ErrInvalidCancelMode) {
		t.Errorf("Cancel(invalid) = %v, want ErrInvalidCancelMode", err)
	}
}

func sameCode(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// writeSentinel 往报告文件追加一行哨兵（depth=helperSentinelDepth），用于让
// spawn-and-exit helper 知道“测试已经确认完后代身份，可以退出了”。
func writeSentinel(t *testing.T, report string) {
	t.Helper()
	f, err := os.OpenFile(report, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open report %s for sentinel: %v", report, err)
	}
	defer f.Close() //nolint:errcheck // 只追加一行
	if _, err := f.WriteString(`{"pid":0,"ppid":0,"depth":99}` + "\n"); err != nil {
		t.Fatalf("write sentinel to %s: %v", report, err)
	}
}

// releaseHelper 创建 release 文件，让 helper 自行退出。
func releaseHelper(t *testing.T, release string) {
	t.Helper()
	if err := os.WriteFile(release, []byte("go\n"), 0o644); err != nil {
		t.Fatalf("create release file %s: %v", release, err)
	}
}
