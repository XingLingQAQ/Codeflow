package process

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/runworkspace"
)

// 本文件是 §28 T1.08.c 点名的四个测试，外加两条卡片验收（参数不经 shell、nil Env 不继承）。
// 全部使用真实进程（helper_test.go 的自我重入夹具），不 mock OS。
//
// 平台口径（用户 2026-09-29 裁定）：Windows 实跑；Linux 只交叉编译、运行 not_run，
// 由 T12.04 前补做；其他平台 Supported() 为 false，测试按 capability=false 走
// requireIdentitySupport 的能力断言分支（与 identity_test.go 同一约定）。
//
// 残留进程：每个 Start/startHelper 都登记了强制清理；第 4 条测试还额外登记
// “按事先捕获的身份补杀”的兜底，保证崩溃模拟失败时也不会留下孤儿。

const (
	// t108cGrace 是点名测试用的软→强制宽限期：够短以保证测试快，够长以观察升级。
	t108cGrace = 700 * time.Millisecond
	// t108cFloodBytes 是洪峰测试每一路要写的字节数（32 MiB，且是 64 KiB 的整数倍）。
	t108cFloodBytes = 32 << 20
	// t108cFloodSpool 是洪峰测试的每路 spool 上限（1 MiB）。
	t108cFloodSpool = 1 << 20
)

// ---------------------------------------------------------------------------
// 1. TestCancelDescendantTree
// ---------------------------------------------------------------------------

// TestCancelDescendantTree：3 层、7 个进程的树，其中根与叶层（5 个）忽略软终止；
// 软取消后必须在 GracePeriod 到期时升级为强制，整棵树逐个按事先捕获的身份
// Verify 证明已结束（没有孤儿），并记录总耗时。
//
// 为什么根必须忽略软终止：主进程一旦自行退出，run() 会立刻 release 树绑定
// （Windows 关 Job 句柄 → KILL_ON_JOB_CLOSE），升级路径根本不会被走到。让根存活，
// 才能观察到“软取消不起作用 → 到期升级 → 强杀整棵树”这条真正的升级链路。
func TestCancelDescendantTree(t *testing.T) {
	if !requireIdentitySupport(t) {
		return
	}
	report, release := helperDir(t)
	spec := supervisorSpec(t, helperModeCancelTree,
		helperDepthEnv+"=2",
		helperFanoutEnv+"=2",
		helperResistDepthsEnv+"=2,0", // 根（2）与叶（0）忽略软终止；中间层（1）响应
		helperReportEnv+"="+report,
		helperReleaseEnv+"="+release,
	)
	spec.GracePeriod = t108cGrace
	h := startHandle(t, spec)

	lines := waitForReportLines(t, report, 7, testWaitLong)
	if len(lines) != 7 {
		t.Fatalf("tree reported %d processes, want 7: %+v", len(lines), lines)
	}
	depths := map[int]int{}
	for _, line := range lines {
		depths[line.Depth]++
	}
	if depths[2] != 1 || depths[1] != 2 || depths[0] != 4 {
		t.Fatalf("depth histogram = %v, want 3 layers 1/2/4 (lines %+v)", depths, lines)
	}
	ids := captureAll(t, lines)
	// 断言中途失败也不许留下孤儿：按事先捕获的身份补杀（正常情况下是空操作）。
	t.Cleanup(func() { t108cKillIdentities(t, ids) })
	for _, id := range ids {
		if res, err := Verify(id); err != nil || res != VerifySame {
			t.Fatalf("before cancel: Verify(pid %d) = (%v, %v), want (same, nil)", id.PID, res, err)
		}
	}

	rootID := h.Identity()
	started := time.Now()
	softErr := h.Cancel(context.Background(), CancelSoft)
	exit := waitExit(t, h, testWaitShort)
	elapsed := time.Since(started)

	// 软终止要么投递成功，要么如实报告投递失败（无控制台时 Windows 的已知限制）；
	// 两者都必须进入升级计时，绝不允许变成“取消请求丢失”。
	if softErr != nil && !errors.Is(softErr, ErrSoftTerminationFailed) {
		t.Fatalf("Cancel(soft) error = %v, want nil or ErrSoftTerminationFailed", softErr)
	}
	// “按时升级”：强制终止只可能发生在 Cancel(soft) 之后满一个宽限期，不可能更早。
	if elapsed < t108cGrace {
		t.Errorf("tree ended after %s, want >= grace %s (force must wait out the grace period)", elapsed, t108cGrace)
	}
	if exit.Reason != ExitCancelled {
		t.Errorf("Exit.Reason = %q, want %q", exit.Reason, ExitCancelled)
	}
	if !exit.Forced {
		t.Errorf("Exit.Forced = false, want true (root ignored soft termination, escalation must fire)")
	}
	if exit.Code != nil {
		t.Errorf("Exit.Code = %v, want nil (forced termination has no meaningful code)", *exit.Code)
	}

	// 逐个后代按事先捕获的身份证明“已结束”：每个 PID 都必须不再是同一个进程。
	results := assertNotSameAll(t, ids, testWaitShort)
	for _, id := range ids {
		if res := results[id.PID]; res == VerifySame {
			t.Errorf("pid %d is still the same process after escalation: orphan left behind", id.PID)
		}
	}
	if res, err := Verify(rootID); err != nil || res == VerifySame {
		t.Errorf("root pid %d verify = (%v, %v), want terminated", rootID.PID, res, err)
	}
	t.Logf("cancel-tree: softErr=%v, %d processes terminated after %s (grace %s), exit=%+v, verify=%v",
		softErr, len(ids), elapsed, t108cGrace, exit, results)
}

// t108cFloodByte 复现 flood-both 写入流的第 i 个字节：每 helperFloodLineBytes 一行，
// 行内前 helperFloodLineBytes-1 个字节沿用 helperFloodByte 的模式，行末是 '\n'。
func t108cFloodByte(i int64) byte {
	if i%helperFloodLineBytes == helperFloodLineBytes-1 {
		return '\n'
	}
	return helperFloodByte(int(i))
}

// assertFloodTail 断言 data 是整条输出的尾部，且每个字节与可预期模式一致。
func assertFloodTail(t *testing.T, label string, data []byte, total int64) {
	t.Helper()
	offset := total - int64(len(data))
	for i := range data {
		if want := t108cFloodByte(offset + int64(i)); data[i] != want {
			t.Fatalf("%s[%d] = %q, want %q (snapshot is not the tail of the output)",
				label, i, data[i], want)
		}
	}
}

// ---------------------------------------------------------------------------
// 2. TestFloodNoDeadlock
// ---------------------------------------------------------------------------

// TestFloodNoDeadlock：stdout 与 stderr 同时各写 32 MiB、全程没有任何读者
// （测试在 Wait 返回前不碰任何 spool 快照），外加一个从不读取的慢订阅者。
// 断言：进程正常结束、Wait 在期限内返回；两路 spool 都不超上限、截断字节数精确、
// 保留的是各自输出的尾部；慢订阅者以 ErrSubscriberOverflow 结束（它绝不反压子进程）；
// 测试前后 goroutine 数不增长。
//
// goroutine 测量方式：Start 之前取 runtime.NumGoroutine() 基线；Wait 返回后再轮询
// 最多 t108cGoroutineSettle，要求回落到 <= 基线。用轮询而不是瞬时比较，是因为
// run()/drain 的退出与运行时的后台 goroutine 需要一点时间落地；泄漏的排空
// goroutine 不会随时间消失，轮询不会掩盖真泄漏。
func TestFloodNoDeadlock(t *testing.T) {
	if !requireIdentitySupport(t) {
		return
	}
	const settle = 5 * time.Second

	baseline := runtime.NumGoroutine()
	report, release := helperDir(t)
	spec := supervisorSpec(t, helperModeFloodBoth,
		helperBytesEnv+"="+strconv.Itoa(t108cFloodBytes),
		helperStderrBytesEnv+"="+strconv.Itoa(t108cFloodBytes),
		helperReleaseEnv+"="+release,
		helperReportEnv+"="+report,
	)
	spec.MaxSpoolBytes = t108cFloodSpool
	h := startHandle(t, spec)

	// 慢订阅者：缓冲 1 行，测试此后从不读取。它必须因为“投递永不阻塞排空”而被
	// 判定溢出并终止，而不是把子进程卡死在写管道上。
	sub := subscribeOrFail(t, h, StreamStdout, SubscribeOptions{BufferLines: 1})

	// release 文件出现前两路都还没写：订阅已经确定性就位，没有竞态。
	releaseHelper(t, release)

	started := time.Now()
	exit := waitExit(t, h, 20*time.Second)
	t.Logf("flood-both: process exited after %s with %+v (no reader ever touched the spools)",
		time.Since(started), exit)

	if exit.Reason != ExitExited {
		t.Errorf("Exit.Reason = %q, want %q", exit.Reason, ExitExited)
	}
	if exit.Code == nil || *exit.Code != 0 {
		t.Errorf("Exit.Code = %v, want 0", exit.Code)
	}
	wantTruncEach := int64(t108cFloodBytes - t108cFloodSpool)
	if exit.SpoolTruncatedBytes != 2*wantTruncEach {
		t.Errorf("Exit.SpoolTruncatedBytes = %d, want %d (stdout+stderr truncation)",
			exit.SpoolTruncatedBytes, 2*wantTruncEach)
	}

	for _, tc := range []struct {
		name  string
		spool Spool
	}{
		{"stdout", h.Output()},
		{"stderr", h.Stderr()},
	} {
		data, truncated := tc.spool.Snapshot()
		if len(data) > t108cFloodSpool {
			t.Errorf("%s spool holds %d bytes, limit is %d", tc.name, len(data), t108cFloodSpool)
		}
		if int64(len(data)) != int64(t108cFloodSpool) {
			t.Errorf("%s spool snapshot = %d bytes, want exactly %d", tc.name, len(data), t108cFloodSpool)
		}
		if truncated != wantTruncEach {
			t.Errorf("%s truncated = %d, want %d", tc.name, truncated, wantTruncEach)
		}
		assertFloodTail(t, tc.name+" spool", data, t108cFloodBytes)
	}

	// 慢订阅者：第 1 行进了缓冲，第 2 行无处可放 → 溢出终止，丢行序号恰为 2。
	if err := sub.Err(); !errors.Is(err, ErrSubscriberOverflow) {
		t.Errorf("slow subscriber Err() = %v, want ErrSubscriberOverflow", err)
	}
	if got := sub.DroppedSeq(); got != 2 {
		t.Errorf("slow subscriber DroppedSeq() = %d, want 2", got)
	}
	var buffered []Line
	for l := range sub.C() {
		buffered = append(buffered, l)
	}
	if len(buffered) != 1 || buffered[0].Seq != 1 {
		t.Errorf("slow subscriber received %d line(s) %+v, want exactly line 1", len(buffered), buffered)
	}

	// goroutine 数不增长：轮询到回落（或超时后报失败，并给出基线/当前值）。
	deadline := time.Now().Add(settle)
	got := runtime.NumGoroutine()
	for got > baseline && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		got = runtime.NumGoroutine()
	}
	if got > baseline {
		t.Errorf("goroutines grew from %d to %d (measurement: runtime.NumGoroutine() before Start vs after Wait +/- %s)",
			baseline, got, settle)
	}
	t.Logf("goroutines: baseline=%d after=%d (settled within %s)", baseline, got, settle)
}

// ---------------------------------------------------------------------------
// 3. TestPidReuseNotKilled
// ---------------------------------------------------------------------------

// TestPidReuseNotKilled：一个不属于本 supervisor 的“无辜”进程，其 PID 恰好等于
// supervisor 记录里那个已退出进程的 PID（PID 被系统复用）。经真实的取消/强杀路径
// 操作时，身份校验必须拒绝（Verify 判为 reused），一个信号都不能发；无辜进程与
// 被监督进程都继续存活。最后测试自己用无辜进程的真实身份 Verify 它是 alive，
// 再自行清理它。
//
// 构造方式：把真实句柄上的身份换成 {PID: 无辜进程, StartToken: 被监督进程的启动标记}。
// 语义就是“记录里的 PID 现在是一个另一个进程”——Windows 上启动标记是进程创建时间，
// PID 被复用时必然不同，这正是防误杀的唯一依据（§27.5 第 6 条）。不真的等系统复用
// PID，是为了可确定性复现，与 start_test.go 里既有做法一致。
func TestPidReuseNotKilled(t *testing.T) {
	if !requireIdentitySupport(t) {
		return
	}
	report, release := helperDir(t)
	spec := supervisorSpec(t, helperModeExit,
		helperCodeEnv+"=0",
		helperReleaseEnv+"="+release, // 永不创建：被监督进程一直活着
		helperReportEnv+"="+report,
	)
	h := startHandle(t, spec)
	supervisedID := h.Identity()
	waitSame(t, supervisedID, testWaitLong)

	// 无辜进程：不属于本 supervisor（另一个 owner），自己也不是这条执行的一部分。
	innocent := startHelper(t, helperModeIgnoreTerm)
	innocentOwner := Ownership{RunID: "run-innocent", AttemptID: "attempt-innocent", OwnerInstance: "innocent-instance"}
	innocentID, err := CaptureIdentity(innocent.Process.Pid, innocentOwner)
	if err != nil {
		t.Fatalf("CaptureIdentity(innocent pid %d): %v", innocent.Process.Pid, err)
	}
	if res, err := Verify(innocentID); err != nil || res != VerifySame {
		t.Fatalf("innocent pid %d verify = (%v, %v), want (same, nil)", innocentID.PID, res, err)
	}
	// 归属不同：恢复路径不得把它当成自己的进程。
	if innocentID.OwnedBy(supervisedID.Owner) {
		t.Fatalf("innocent owner %+v must not match supervised owner %+v", innocentID.Owner, supervisedID.Owner)
	}

	ph, ok := h.(*procHandle)
	if !ok {
		t.Fatalf("handle type = %T, want *procHandle", h)
	}
	// “原进程已退出、PID 被复用”：记录里的 PID 指向无辜进程，启动标记仍是原进程的。
	reused := Identity{
		PID:        innocentID.PID,
		StartToken: supervisedID.StartToken,
		Owner:      supervisedID.Owner,
		CapturedAt: supervisedID.CapturedAt,
	}
	if res, err := Verify(reused); err != nil || res != VerifyReused {
		t.Fatalf("Verify(reused identity pinning innocent pid %d) = (%v, %v), want (reused, nil): "+
			"test prerequisite not met (start tokens collided)", innocentID.PID, res, err)
	}
	ph.id = reused

	for _, mode := range []CancelMode{CancelSoft, CancelForce} {
		err := h.Cancel(context.Background(), mode)
		if !errors.Is(err, ErrIdentityUnconfirmed) {
			t.Fatalf("Cancel(%s) with reused identity = %v, want ErrIdentityUnconfirmed", mode, err)
		}
	}
	// 身份无法确认：一个信号都不许发出去。真实 Job 若被强杀，被监督进程必然消失；
	// 无辜进程若被误杀，它也会消失。两个都还在 = 取消路径确实什么都没做。
	if res, err := Verify(innocentID); err != nil || res != VerifySame {
		t.Errorf("innocent pid %d verify = (%v, %v), want (same, nil): it must not be signalled",
			innocentID.PID, res, err)
	}
	if res, err := Verify(supervisedID); err != nil || res != VerifySame {
		t.Errorf("supervised pid %d verify = (%v, %v), want (same, nil): no signal may be delivered",
			supervisedID.PID, res, err)
	}

	// 对照：把身份还原成真实身份后，同一个句柄的强杀路径确实能工作（失败不是“取消坏了”）。
	ph.id = supervisedID
	if err := h.Cancel(context.Background(), CancelForce); err != nil {
		t.Fatalf("Cancel(force) with the real identity = %v, want nil", err)
	}
	exit := waitExit(t, h, testWaitShort)
	// 身份丢失已经记在句柄上：终态必须如实报 lost（且不给退出码），
	// 绝不能因为之后用真实身份补杀成功就伪装成正常退出。
	if exit.Reason != ExitLost {
		t.Errorf("control force cancel: Exit.Reason = %q, want %q (lost identity may not be reported as exited)",
			exit.Reason, ExitLost)
	}
	if exit.Code != nil {
		t.Errorf("control force cancel: Exit.Code = %v, want nil (a lost process has no known code)", *exit.Code)
	}

	// 测试自己清理无辜进程：先按真实身份再确认一次，再结束它，最后证明它真的没了。
	if res, err := Verify(innocentID); err != nil || res != VerifySame {
		t.Fatalf("innocent pid %d verify before cleanup = (%v, %v), want (same, nil)", innocentID.PID, res, err)
	}
	if err := innocent.Process.Kill(); err != nil {
		t.Fatalf("kill innocent pid %d: %v", innocentID.PID, err)
	}
	done := make(chan error, 1)
	go func() { done <- innocent.Wait() }()
	select {
	case <-done:
	case <-time.After(testWaitShort):
		t.Fatalf("innocent pid %d did not exit after Kill", innocentID.PID)
	}
	if res, err := Verify(innocentID); err != nil {
		t.Fatalf("Verify(innocent) after kill: %v", err)
	} else if res == VerifySame {
		t.Errorf("innocent pid %d is still the same process after Kill", innocentID.PID)
	}
	t.Logf("pid-reuse: innocent pid %d untouched by cancel paths, cleaned up by the test", innocentID.PID)
}

// ---------------------------------------------------------------------------
// 4. TestCrashRetainsOwnedWorkdir
// ---------------------------------------------------------------------------

// t108cSnapshotDir 递归读取目录，返回 relpath -> 内容 的映射（用于“崩溃前后逐字节一致”）。
func t108cSnapshotDir(t *testing.T, root string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = data
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}

// t108cSameTree 比较两份目录快照是否逐字节一致。
func t108cSameTree(before, after map[string][]byte) (missing, added, changed []string) {
	for rel, want := range before {
		got, ok := after[rel]
		if !ok {
			missing = append(missing, rel)
			continue
		}
		if !bytes.Equal(got, want) {
			changed = append(changed, rel)
		}
	}
	for rel := range after {
		if _, ok := before[rel]; !ok {
			added = append(added, rel)
		}
	}
	sort.Strings(missing)
	sort.Strings(added)
	sort.Strings(changed)
	return missing, added, changed
}

// TestCrashRetainsOwnedWorkdir：模拟后端进程崩溃（TestMain 的 server 角色），
// 断言崩溃后 ①工作目录与其中文件原样保留 ②marker 仍在且足以供恢复判断
// ③按 marker 的身份 Verify 为 exited（Windows 上 Job 句柄随 server 退出关闭，
// KILL_ON_JOB_CLOSE 收掉整棵树）④另一个 owner instance 的新 supervisor 按 marker
// 操作时，身份/归属不符的一律拒绝。
func TestCrashRetainsOwnedWorkdir(t *testing.T) {
	if !requireIdentitySupport(t) {
		return
	}
	serverOwner := Ownership{RunID: t108cServerRunID, AttemptID: t108cServerAttemptID, OwnerInstance: "t108c-server"}

	// 工作目录用 runworkspace.Materialize 建（带所有权标记，和生产同一条路径）。
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "seed.txt"), []byte("baseline seed\n"), 0o644); err != nil {
		t.Fatalf("write seed: %v", err)
	}
	manifest, err := runworkspace.Capture(context.Background(), nil, src, runworkspace.CaptureOptions{})
	if err != nil {
		t.Fatalf("runworkspace.Capture: %v", err)
	}
	wc, err := runworkspace.Materialize(context.Background(), manifest, t.TempDir(), runworkspace.Ownership(serverOwner))
	if err != nil {
		t.Fatalf("runworkspace.Materialize: %v", err)
	}
	workdir := wc.Path

	base := filepath.Dir(workdir)
	markerPath := filepath.Join(workdir, "process-marker.json")
	reportPath := filepath.Join(workdir, "tree-report.jsonl")
	readyPath := filepath.Join(base, "t108c-ready.json")
	neverRelease := filepath.Join(base, "never-release")

	server := newHelperCmd(t, helperModeServer,
		helperDepthEnv+"=2",
		helperFanoutEnv+"=2",
		helperReportEnv+"="+reportPath,
		helperReleaseEnv+"="+neverRelease,
		helperMarkerEnv+"="+markerPath,
		helperOwnerInstanceEnv+"="+serverOwner.OwnerInstance,
		helperReadyEnv+"="+readyPath,
	)
	// server 进程自己的 cwd 就是“本 Run 的工作目录”（它用 os.Getwd() 取)，
	// 树进程也以它为 Dir——工作目录的保留断言才有意义。
	server.Dir = workdir
	if err := server.Start(); err != nil {
		t.Fatalf("start server helper: %v", err)
	}
	t.Cleanup(func() { killAndReap(t, server) })
	// 崩溃模拟无论成败都不许留下孤儿：按事先捕获的身份在清理阶段补杀。
	var surviving []Identity
	t.Cleanup(func() { t108cKillIdentities(t, surviving) })

	ready := t108cWaitReady(t, readyPath, testWaitLong)
	if ready.ServerPID != server.Process.Pid {
		t.Fatalf("ready server_pid = %d, want %d", ready.ServerPID, server.Process.Pid)
	}
	if ready.Workdir != workdir {
		t.Fatalf("ready workdir = %q, want %q", ready.Workdir, workdir)
	}
	if ready.RootPID <= 0 {
		t.Fatalf("ready root_pid = %d, want positive", ready.RootPID)
	}

	// 树先全部起来（7 个进程都写了报告行），证明崩溃前它真的是一棵活的进程树。
	lines := waitForReportLines(t, reportPath, 7, testWaitLong)
	ids := captureAll(t, lines)
	surviving = ids
	for _, id := range ids {
		waitSame(t, id, testWaitLong)
	}

	// ②崩溃前 marker 就位：身份 + owner 足以供恢复判断（§27.5 第 6 条）。
	raw := readFileEventually(t, markerPath, testWaitShort)
	start := decodeMarker(t, raw)
	if err := start.Identity.Validate(); err != nil {
		t.Fatalf("marker identity invalid: %v (%s)", err, raw)
	}
	if start.Identity.PID != ready.RootPID {
		t.Errorf("marker identity pid = %d, want tree root pid %d", start.Identity.PID, ready.RootPID)
	}
	if start.Owner != serverOwner {
		t.Errorf("marker owner = %+v, want %+v", start.Owner, serverOwner)
	}
	if start.Path != helperExecutable() {
		t.Errorf("marker path = %q, want %q", start.Path, helperExecutable())
	}
	if start.Reason != "" || start.FinishedAt != nil {
		t.Errorf("marker already has terminal fields before the crash: %+v", start)
	}
	rootID, err := CaptureIdentity(ready.RootPID, serverOwner)
	if err != nil {
		t.Fatalf("CaptureIdentity(tree root pid %d): %v", ready.RootPID, err)
	}
	if rootID.StartToken != start.Identity.StartToken {
		t.Errorf("marker start token %q != freshly captured %q", start.Identity.StartToken, rootID.StartToken)
	}

	// 崩溃前的完整快照（工作目录内容 + 所有权标记）。
	before := t108cSnapshotDir(t, workdir)
	for rel := range t108cWorkdirFiles() {
		if _, ok := before[filepath.ToSlash(rel)]; !ok {
			t.Fatalf("workdir is missing %s before the crash: %v", rel, keysOf(before))
		}
	}

	// 直接强杀 server 进程：不经任何 supervisor、不做任何清理。后端进程一死，
	// 它持有的 Job 句柄由系统关闭 → KILL_ON_JOB_CLOSE 收掉整棵树。
	if err := server.Process.Kill(); err != nil {
		t.Fatalf("kill server pid %d: %v", server.Process.Pid, err)
	}
	done := make(chan error, 1)
	go func() { done <- server.Wait() }()
	select {
	case <-done:
	case <-time.After(testWaitShort):
		t.Fatalf("server pid %d did not exit after Kill", server.Process.Pid)
	}

	// ③整棵树必须消失：逐个按事先捕获的身份确认不再是同一个进程；
	// marker 记录的那个根必须明确是 exited。
	for _, id := range ids {
		res := waitNotSame(t, id, 15*time.Second)
		if res == VerifySame {
			t.Fatalf("tree pid %d is still alive after the backend crashed: "+
				"Job handle was not closed with the backend process (KILL_ON_JOB_CLOSE did not reap the tree)", id.PID)
		}
	}
	if res, err := Verify(rootID); err != nil {
		t.Fatalf("Verify(marker identity) after crash: %v", err)
	} else if res == VerifySame {
		t.Fatalf("marker identity pid %d is still alive after the backend crashed; "+
			"the Job handle was not closed with the backend process", rootID.PID)
	} else if res != VerifyExited {
		t.Errorf("marker identity pid %d verify = %s, want exited (alive would mean the tree leaked)", rootID.PID, res)
	}

	// ①工作目录与其中文件原样保留：supervisor 从不删除工作目录。
	after := t108cSnapshotDir(t, workdir)
	missing, added, changed := t108cSameTree(before, after)
	if len(missing) > 0 {
		t.Errorf("workdir lost %d file(s) across the backend crash: %v", len(missing), missing)
	}
	if len(changed) > 0 {
		t.Errorf("workdir file(s) changed across the backend crash: %v", changed)
	}
	if len(added) > 0 {
		t.Logf("note: %d file(s) appeared after the crash (unexpected): %v", len(added), added)
		if len(added) > 0 {
			t.Errorf("workdir gained file(s) after all writers died: %v", added)
		}
	}
	// 逐字节复核 server 端写入的内容（快照比对之外，独立按表验证一次）。
	for rel, want := range t108cWorkdirFiles() {
		got, ok := after[filepath.ToSlash(rel)]
		if !ok {
			t.Errorf("workdir file %s is gone after the crash", rel)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("workdir file %s changed after the crash: %d bytes, want %d", rel, len(got), len(want))
		}
	}
	// 所有权标记仍在，且仍把工作目录判给同一执行：T1.04 恢复器据此接管。
	ownerRaw, err := os.ReadFile(filepath.Join(workdir, runworkspace.OwnerMarkerName()))
	if err != nil {
		t.Fatalf("read runworkspace ownership marker after crash: %v", err)
	}
	var ownerMarker struct {
		RunID         string `json:"run_id"`
		AttemptID     string `json:"attempt_id"`
		OwnerInstance string `json:"owner_instance"`
	}
	if err := json.Unmarshal(ownerRaw, &ownerMarker); err != nil {
		t.Fatalf("parse runworkspace ownership marker: %v", err)
	}
	if ownerMarker.RunID != serverOwner.RunID || ownerMarker.AttemptID != serverOwner.AttemptID || ownerMarker.OwnerInstance != serverOwner.OwnerInstance {
		t.Errorf("workdir ownership marker = %+v, want %+v", ownerMarker, serverOwner)
	}
	// marker 仍在（上面读的就是它），且崩溃后没有被写成终态。
	raw = readFileEventually(t, markerPath, testWaitShort)
	crash := decodeMarker(t, raw)
	if crash.Reason != "" || crash.FinishedAt != nil {
		t.Errorf("marker was completed after the crash: %+v", crash)
	}
	if len(after) < len(before) {
		t.Errorf("workdir has %d files after the crash, had %d", len(after), len(before))
	}
	t.Logf("crash-retain: workdir %s survived with %d file(s), %d tree processes gone",
		workdir, len(after), len(ids))

	// ④另一个 owner instance 的新 supervisor：归属不符一律拒绝。
	recoverySup := NewSupervisor(SupervisorOptions{OwnerInstance: "t108c-recovery"})
	foreignSpec := supervisorSpec(t, helperModeExit,
		helperCodeEnv+"=0",
		helperReportEnv+"="+filepath.Join(base, "recovery-report.jsonl"),
		helperReleaseEnv+"="+filepath.Join(base, "recovery-release"),
	)
	foreignSpec.Owner = crash.Owner // 崩溃那次执行的归属（另一个实例）
	foreignSpec.MarkerPath = filepath.Join(base, "recovery-marker.json")
	allowProcessStart(t)
	if _, err := recoverySup.Start(context.Background(), foreignSpec); !errors.Is(err, ErrForeignOwnerInstance) {
		t.Fatalf("recovery supervisor Start with the crashed run's owner = %v, want ErrForeignOwnerInstance", err)
	}
	// 按 marker 的身份操作：身份/归属不符时不得发任何信号。
	if crash.Identity.OwnedBy(Ownership{RunID: t108cServerRunID, AttemptID: t108cServerAttemptID, OwnerInstance: "t108c-recovery"}) {
		t.Errorf("marker identity claims to be owned by the recovery instance; want owner mismatch")
	}
	binding := &recordingBinding{}
	adopted := &procHandle{
		spec:     Spec{Owner: crash.Owner},
		id:       crash.Identity,
		binding:  binding,
		stdout:   newRingSpool(1024),
		stderr:   newRingSpool(1024),
		waitDone: make(chan struct{}),
	}
	for _, mode := range []CancelMode{CancelSoft, CancelForce} {
		if err := adopted.Cancel(context.Background(), mode); err != nil {
			t.Fatalf("adopted handle Cancel(%s) on an exited tree = %v, want nil (nothing to kill)", mode, err)
		}
	}
	if soft, force := binding.counts(); soft != 0 || force != 0 {
		t.Fatalf("adopted handle delivered %d soft / %d force signal(s) to an exited tree, want 0/0", soft, force)
	}
	// PID 已被复用给现存活进程（这里用测试进程自己）时，同样的路径必须拒绝。
	reused := Identity{PID: os.Getpid(), StartToken: crash.Identity.StartToken, Owner: crash.Owner, CapturedAt: crash.Identity.CapturedAt}
	if res, err := Verify(reused); err != nil || res != VerifyReused {
		t.Fatalf("Verify(reused marker identity on pid %d) = (%v, %v), want (reused, nil)", os.Getpid(), res, err)
	}
	reusedHandle := &procHandle{
		spec:     Spec{Owner: crash.Owner},
		id:       reused,
		binding:  binding,
		stdout:   newRingSpool(1024),
		stderr:   newRingSpool(1024),
		waitDone: make(chan struct{}),
	}
	for _, mode := range []CancelMode{CancelSoft, CancelForce} {
		if err := reusedHandle.Cancel(context.Background(), mode); !errors.Is(err, ErrIdentityUnconfirmed) {
			t.Fatalf("Cancel(%s) with a reused marker identity = %v, want ErrIdentityUnconfirmed", mode, err)
		}
	}
	if soft, force := binding.counts(); soft != 0 || force != 0 {
		t.Fatalf("reused marker identity delivered %d soft / %d force signal(s), want 0/0", soft, force)
	}
	if res, err := Verify(reused); err != nil {
		t.Fatalf("Verify(reused marker identity) after cancels: %v", err)
	} else if res != VerifyReused {
		t.Fatalf("reused marker identity verify = %s after cancels, want reused (the test process must be untouched)", res)
	}

	// 对照：同样经工作目录启动、但正常走到终态的 supervisor——它的结束路径也不得
	// 删除工作目录。崩溃路径永远不会执行 finish()，这条对照补上那个缺口。
	controlWorkdir := t.TempDir()
	controlReport := filepath.Join(controlWorkdir, "control-report.jsonl")
	controlRelease := filepath.Join(controlWorkdir, "control-release")
	controlMarker := filepath.Join(controlWorkdir, "control-marker.json")
	// tree(depth=0)：写一行报告后阻塞，release 后自行退出 0——它的终态会真的走到
	// supervisor 的 finish()（崩溃路径永远不会）。
	controlSpec := supervisorSpec(t, helperModeTree, helperDepthEnv+"=0", helperFanoutEnv+"=0")
	controlSpec.Dir = controlWorkdir
	controlSpec.Env = helperEnv(helperModeTree,
		helperDepthEnv+"=0", helperFanoutEnv+"=0",
		helperReleaseEnv+"="+controlRelease,
		helperReportEnv+"="+controlReport,
	)
	controlSpec.MarkerPath = controlMarker
	ch := startHandle(t, controlSpec)
	waitSame(t, ch.Identity(), testWaitLong)
	waitForReportLines(t, controlReport, 1, testWaitShort)
	releaseHelper(t, controlRelease)
	if cexit := waitExit(t, ch, testWaitShort); cexit.Reason != ExitExited || cexit.Code == nil || *cexit.Code != 0 {
		t.Fatalf("control exit = %+v, want exited with code 0", cexit)
	}
	for _, path := range []string{controlReport, controlRelease, controlMarker} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("workdir file %s missing after a normal supervised exit: %v (supervisor must never delete the workdir)", path, err)
		}
	}
	t.Logf("control: workdir %s intact after a normal supervised exit", controlWorkdir)
}

// t108cReady 是 server 角色写出的就绪文件内容。
type t108cReady struct {
	ServerPID     int    `json:"server_pid"`
	RootPID       int    `json:"root_pid"`
	Workdir       string `json:"workdir"`
	Marker        string `json:"marker"`
	Report        string `json:"report"`
	OwnerInstance string `json:"owner_instance"`
}

// t108cWaitReady 轮询就绪文件并解析。
func t108cWaitReady(t *testing.T, path string, timeout time.Duration) t108cReady {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last error
	for {
		data, err := os.ReadFile(path)
		if err == nil {
			var ready t108cReady
			if jerr := json.Unmarshal(data, &ready); jerr != nil {
				last = jerr
			} else {
				return ready
			}
		} else {
			last = err
		}
		if time.Now().After(deadline) {
			t.Fatalf("ready file %s not usable after %s: %v", path, timeout, last)
		}
		time.Sleep(helperReleasePoll)
	}
}

// t108cKillIdentities 兜底清理：对每个仍然 Verify=same 的身份做强制结束，
// 并等待其不再相同。崩溃模拟在断言中途失败时也必须不留孤儿。
func t108cKillIdentities(t *testing.T, ids []Identity) {
	t.Helper()
	for _, id := range ids {
		res, err := Verify(id)
		if err != nil || res != VerifySame {
			continue
		}
		if p, err := os.FindProcess(id.PID); err == nil {
			_ = p.Kill()
		}
		if final := waitNotSame(t, id, testWaitShort); final == VerifySame {
			t.Errorf("cleanup: pid %d still alive after Kill", id.PID)
		}
	}
}

// keysOf 返回快照里的相对路径列表（失败信息用）。
func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ---------------------------------------------------------------------------
// 卡片验收 1：参数原样传给子进程，不被任何 shell 解释
// ---------------------------------------------------------------------------

// t108cShellPayload 是必须逐字节到达子进程的参数集合：shell 元字符、通配符、
// 引号、重定向、Windows cmd 的转义字符、空格与空串都在里面。任何一个被 shell
// 解释（拆分、展开、去引号、重定向）都会让 echo-args 的回显与它不等。
func t108cShellPayload() []string {
	return []string{
		"a;b",
		"c&&d",
		"e|f",
		"$(rm -rf /tmp/t108c-not-a-real-path)",
		"`id`",
		"$(echo injected)",
		"he said \"hi\"",
		"'single quoted'",
		`back\slash`,
		`C:\path\`,
		"two words",
		"*",
		"?.go",
		">out.txt",
		"<in.txt",
		"2>&1",
		"%PATH%",
		"!VAR!",
		"^&^|",
		"",
		"--flag=\"x y\"",
	}
}

// TestShellMetacharactersPassedVerbatim：Spec.Args 里的 shell 元字符必须原样到达
// 子进程。helper 把自己的 argv[2:] 写成 JSON，测试与 t108cShellPayload 逐元素比较。
func TestShellMetacharactersPassedVerbatim(t *testing.T) {
	payload := t108cShellPayload()
	dir := t.TempDir()
	out := filepath.Join(dir, "argv.json")
	spec := supervisorSpec(t, helperModeEchoArgs)
	spec.Path = helperExecutable()
	spec.Dir = dir
	spec.Args = append([]string{out}, payload...)
	h := startHandle(t, spec)
	exit := waitExit(t, h, testWaitShort)
	if exit.Reason != ExitExited || exit.Code == nil || *exit.Code != 0 {
		t.Fatalf("helper exit = %+v, want exited with code 0", exit)
	}

	raw := readFileEventually(t, out, testWaitShort)
	var got []string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode argv %s: %v", raw, err)
	}
	if len(got) != len(payload) {
		t.Fatalf("child received %d args, want %d\n got: %q\nwant: %q", len(got), len(payload), got, payload)
	}
	for i := range payload {
		if got[i] != payload[i] {
			t.Errorf("arg[%d] = %q, want %q (a shell or the OS quoting layer rewrote it)", i, got[i], payload[i])
		}
	}
	if t.Failed() {
		t.Fatalf("child argv does not match the spec args byte for byte")
	}
	t.Logf("argv verbatim: %d args round-tripped, including %q and the empty string", len(got), payload[3])
}

// ---------------------------------------------------------------------------
// 卡片验收 2：Env 为 nil 表示空环境，不继承父进程
// ---------------------------------------------------------------------------

// TestNilEnvDoesNotInheritParent：Spec.Env 为 nil 时子进程必须看不到父进程（后端）
// 的任何环境变量。用 canary 变量验证，另外用一个显式白名单的对照启动证明
// “变量确实传得进去、只是 nil 不继承”，排除“helper 根本没读环境”的假阳性。
func TestNilEnvDoesNotInheritParent(t *testing.T) {
	const (
		canaryKey   = "CODEFLOW_T108C_ENV_CANARY"
		canaryValue = "t108c-canary-value-must-not-leak-9f2c"
	)
	t.Setenv(canaryKey, canaryValue)
	if os.Getenv(canaryKey) != canaryValue {
		t.Fatalf("test prerequisite: %s is not set in the test process", canaryKey)
	}

	dir := t.TempDir()
	outNil := filepath.Join(dir, "env-nil.json")
	spec := supervisorSpec(t, helperModeCanaryEnv)
	spec.Dir = dir
	// 角色与参数全部走 argv：空环境里子进程没有 CODEFLOW_PROCESS_HELPER 可读，
	// 只能从命令行知道该做什么（见 helperModeFromArgs）。
	spec.Args = []string{helperArgPrefix + helperModeCanaryEnv, outNil, canaryKey}
	spec.Env = nil // 零值语义：空环境，不继承
	h := startHandle(t, spec)
	exit := waitExit(t, h, testWaitShort)
	if exit.Reason != ExitExited || exit.Code == nil || *exit.Code != 0 {
		t.Fatalf("nil-env helper exit = %+v, want exited with code 0", exit)
	}

	raw := readFileEventually(t, outNil, testWaitShort)
	var report struct {
		PID   int      `json:"pid"`
		Var   string   `json:"var"`
		Value string   `json:"value"`
		Env   []string `json:"env"`
	}
	// The report is the child's whole environment. When this test fails it is
	// exactly the parent's environment — tokens, proxy credentials, whatever the
	// machine holds — so no message below ever prints a value, only variable
	// names (envNames). A failing CI log must not become the leak this test is
	// about.
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatalf("decode env report (%d bytes): %v", len(raw), err)
	}
	if report.Var != canaryKey {
		t.Fatalf("helper reported variable %q, want %q", report.Var, canaryKey)
	}
	if report.Value != "" {
		t.Errorf("nil Env: child sees %s set, want it absent (child must not inherit the parent environment)",
			canaryKey)
	}
	if strings.Contains(string(raw), canaryValue) {
		t.Errorf("nil Env: the canary value appears in the child's environment (variables: %q)",
			envNames(report.Env))
	}
	// 反向证据：父进程确实有该变量（同一个 key，父进程读得到）。
	if got := os.Getenv(canaryKey); got != canaryValue {
		t.Fatalf("test prerequisite: parent lost %s between checks", canaryKey)
	}
	// 唯一允许的例外：Windows 上 os/exec 会无条件补一个 SYSTEMROOT（除非显式置空，
	// 见 os/exec 的 Env 文档），这是标准库行为，不是本包在继承父环境；它只含系统
	// 目录路径。其余任何父进程变量出现都算泄漏。
	unexpected := report.Env
	if runtime.GOOS == "windows" {
		unexpected = unexpected[:0:0]
		for _, kv := range report.Env {
			if strings.HasPrefix(strings.ToUpper(kv), "SYSTEMROOT=") {
				continue
			}
			unexpected = append(unexpected, kv)
		}
	}
	if len(unexpected) != 0 {
		t.Errorf("nil Env: child has %d unexpected environment variable(s), want none: %q",
			len(unexpected), envNames(unexpected))
	}
	for _, leaked := range []string{"PATH=", canaryKey + "="} {
		for _, kv := range report.Env {
			if strings.HasPrefix(strings.ToUpper(kv), strings.ToUpper(leaked)) {
				t.Errorf("nil Env: child inherits parent variable %s", envNames([]string{kv})[0])
			}
		}
	}
	t.Logf("nil Env: child pid %d has %d environment variable(s) (%q); canary absent",
		report.PID, len(report.Env), envNames(report.Env))

	// 对照：显式白名单里给出同样的变量，子进程必须看得到。
	outSet := filepath.Join(dir, "env-set.json")
	controlSpec := supervisorSpec(t, helperModeCanaryEnv)
	controlSpec.Dir = dir
	controlSpec.Args = []string{outSet, canaryKey}
	controlSpec.Env = append(helperEnv(helperModeCanaryEnv), canaryKey+"="+canaryValue)
	ch := startHandle(t, controlSpec)
	cexit := waitExit(t, ch, testWaitShort)
	if cexit.Reason != ExitExited || cexit.Code == nil || *cexit.Code != 0 {
		t.Fatalf("control helper exit = %+v, want exited with code 0", cexit)
	}
	craw := readFileEventually(t, outSet, testWaitShort)
	var controlReport struct {
		Value string   `json:"value"`
		Env   []string `json:"env"`
	}
	if err := json.Unmarshal(craw, &controlReport); err != nil {
		t.Fatalf("decode control env report (%d bytes): %v", len(craw), err)
	}
	if controlReport.Value != canaryValue {
		t.Errorf("control: child sees %s = %q, want %q (explicit allow-list must be visible)",
			canaryKey, controlReport.Value, canaryValue)
	}
	if len(controlReport.Env) == 0 {
		t.Errorf("control: child environment is empty, want the explicit allow-list")
	}
}

// envNames returns the variable names of KEY=VALUE pairs, without the values.
// The nil-Env test reports what the child inherited by name only, because what
// it would inherit on failure is the parent's real environment.
func envNames(kvs []string) []string {
	names := make([]string, 0, len(kvs))
	for _, kv := range kvs {
		name, _, _ := strings.Cut(kv, "=")
		names = append(names, name)
	}
	return names
}
