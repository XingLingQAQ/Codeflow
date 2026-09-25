package backendtest

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/execbackend"
)

// 本文件是 RunContractSuite 的逐条检查实现与断言辅助。
//
// 约定：每个 check* 都是独立子测试体，自己构造后端与会话，结束时关闭会话并做
// goroutine 泄漏检查（defer closeSession）。断言失败用 t.Errorf / t.Fatalf 报出
// 可读原因——suite 的输出就是契约违例的证据。

// contractApprovalID 是 suite 投递裁决时使用的服务端审批对象 ID（不透明字符串）。
// 适配器应按 ApprovalDecision.ProviderRef 关联待审批项；ApprovalID 只是服务端标识，
// 被测后端不需要认识它的结构。
const contractApprovalID = "contract-approval-1"

// ---------------------------------------------------------------------------
// Prepare
// ---------------------------------------------------------------------------

// 契约：Prepare 校验请求与 Required 能力，无副作用；req 不合法返回 invalid_request
// （Field 指出字段）；缺能力返回 capability_unavailable（Missing 列出缺失项）。
func checkPrepareInvalidRequest(t *testing.T, h Harness) {
	be := h.New(t, ScenarioCompletes)
	t.Logf("backend under test: %s", be.Name())

	cases := []struct {
		name   string
		mutate func(*execbackend.StartRequest)
	}{
		{"missing run.run_id", func(r *execbackend.StartRequest) { r.Run.RunID = "" }},
		{"negative process.grace_period", func(r *execbackend.StartRequest) { r.Process.GracePeriod = -time.Second }},
	}
	for _, tc := range cases {
		req := h.Request(t)
		tc.mutate(&req)
		_, err := be.Prepare(context.Background(), req)
		e := requireCode(t, err, execbackend.CodeInvalidRequest, "Prepare with "+tc.name)
		if e != nil && strings.TrimSpace(e.Field) == "" {
			t.Errorf("Prepare with %s: invalid_request Field is empty; 契约要求 Field 指出不合法字段", tc.name)
		}
	}
}

// 契约：Required 里未被证据证明的能力必须使 Prepare 失败为 capability_unavailable，
// 且 Missing 列出该能力（§27.7：缺证据的能力一律为 false）。
func checkPrepareMissingCapability(t *testing.T, h Harness) {
	be := h.New(t, ScenarioCompletes)
	report := mustCapabilities(t, be, h.Timeout)

	missing := firstUnproven(report)
	if missing == "" {
		t.Logf("backend %s proves every known capability; 缺能力路径在本后端上不适用", be.Name())
		return
	}
	req := h.Request(t)
	req.Required = []execbackend.Capability{missing}
	_, err := be.Prepare(context.Background(), req)
	e := requireCode(t, err, execbackend.CodeCapabilityUnavailable,
		"Prepare requiring the unproven capability "+string(missing))
	if e != nil && !containsCapability(e.Missing, missing) {
		t.Errorf("Prepare requiring %s: error Missing = %v, want it to contain %s",
			missing, e.Missing, missing)
	}
}

// 契约：零值或未经过 Prepare 的 PreparedStart 必须被拒绝（invalid_request），
// 且失败时不得返回可用 Session（调用方不会 Close 一个失败句柄）。
func checkPrepareStartRequiresPreparedStart(t *testing.T, h Harness) {
	be := h.New(t, ScenarioCompletes)

	cases := []struct {
		name     string
		prepared execbackend.PreparedStart
	}{
		{"zero PreparedStart", execbackend.PreparedStart{}},
		{"PreparedStart without a capability report", execbackend.PreparedStart{Request: h.Request(t)}},
	}
	for _, tc := range cases {
		sess, err := be.Start(context.Background(), tc.prepared)
		if sess != nil {
			t.Errorf("Start with a %s: returned a non-nil Session (%T) together with an error", tc.name, sess)
			_ = sess.Close()
		}
		requireCode(t, err, execbackend.CodeInvalidRequest, "Start with a "+tc.name)
	}
}

// 契约：合法请求可 Prepare + Start；Prepare 不修改调用方请求；PreparedStart 固化
// 已校验请求（之后改原请求不影响 Start）。
func checkPrepareValidRequestStarts(t *testing.T, h Harness) {
	be := h.New(t, ScenarioCompletes)
	guard := newLeakGuard(t, h.Timeout)

	req := h.Request(t)
	snapshot := req.Clone()

	// Start 的 ctx 只覆盖启动调用；不要因为 Prepare/Start 返回就取消它，
	// 否则会误杀把会话生命周期挂在启动 ctx 上的实现（契约并未要求解绑）。
	ctx, cancel := context.WithTimeout(context.Background(), h.Timeout)
	t.Cleanup(cancel)

	prepared, err := be.Prepare(ctx, req)
	requireNoError(t, err, "Prepare with a valid request")
	if !reflect.DeepEqual(req, snapshot) {
		t.Errorf("Prepare modified the caller's StartRequest:\n got: %+v\nwant: %+v", req, snapshot)
	}
	if prepared.Request.Run != snapshot.Run || prepared.Request.Input != snapshot.Input ||
		prepared.Request.WorkDir != snapshot.WorkDir {
		t.Errorf("PreparedStart.Request does not carry the validated request:\n got: %+v\nwant: %+v",
			prepared.Request, snapshot)
	}

	// 固化检查：Prepare 之后改原请求，Start 仍必须成功。
	req.Run.RunID = ""
	req.Input.SnapshotHash = "bogus"
	req.Process.GracePeriod = -time.Second

	sess, err := be.Start(ctx, prepared)
	requireNoError(t, err, "Start with a PreparedStart whose original request was mutated afterwards")
	if sess == nil {
		t.Fatalf("Start returned a nil Session")
	}
	defer closeSession(t, sess, guard)
}

// ---------------------------------------------------------------------------
// Completes
// ---------------------------------------------------------------------------

// 契约：会话的第一条观察是 process_started（Run 状态机据此 starting → running）。
func checkCompletesProcessStartedFirst(t *testing.T, h Harness) {
	be := h.New(t, ScenarioCompletes)
	guard := newLeakGuard(t, h.Timeout)
	sess := startSession(t, h, be)
	defer closeSession(t, sess, guard)

	first := recvObservation(t, sess, h.Timeout, "Completes")
	if first.Kind != execbackend.ObservationProcessStarted {
		t.Errorf("first observation kind = %s, want %s", first.Kind, execbackend.ObservationProcessStarted)
	}
}

// 契约：所有观察 Validate()==nil；恰好一条终结观察（exited）且是最后一条；
// 之后 Observations channel 关闭。
func checkCompletesObservations(t *testing.T, h Harness) {
	be := h.New(t, ScenarioCompletes)
	guard := newLeakGuard(t, h.Timeout)
	sess := startSession(t, h, be)
	defer closeSession(t, sess, guard)

	obs := collectUntilClosed(t, sess, h.Timeout, "Completes")
	if len(obs) == 0 {
		t.Fatalf("no observations were delivered before the channel closed")
	}
	if obs[0].Kind != execbackend.ObservationProcessStarted {
		t.Errorf("first observation kind = %s, want %s", obs[0].Kind, execbackend.ObservationProcessStarted)
	}

	terminal := -1
	for i, o := range obs {
		if !o.IsTerminal() {
			continue
		}
		if terminal >= 0 {
			t.Errorf("observation[%d] kind=%s is a second terminal observation (first at [%d])",
				i, o.Kind, terminal)
		}
		terminal = i
	}
	if terminal < 0 {
		t.Fatalf("no terminal (exited) observation among %d observations: %s",
			len(obs), describeObservations(obs))
	}
	if terminal != len(obs)-1 {
		t.Errorf("terminal observation is at index %d of %d, want the last one: %s",
			terminal, len(obs), describeObservations(obs))
	}
	result := decodeExitResult(t, obs[terminal])
	if result.Reason != execbackend.ExitReasonCompleted {
		t.Errorf("terminal reason = %s, want %s", result.Reason, execbackend.ExitReasonCompleted)
	}
}

// 契约：Wait 可重复调用（并发 4 + 顺序 3），每次返回同一最终结果，且等于终结观察的
// ExitResult（types.go：exited 的 Payload 就是 Wait 返回的最终结果）。
func checkCompletesWaitRepeatable(t *testing.T, h Harness) {
	be := h.New(t, ScenarioCompletes)
	guard := newLeakGuard(t, h.Timeout)
	sess := startSession(t, h, be)
	defer closeSession(t, sess, guard)

	obs := collectUntilClosed(t, sess, h.Timeout, "Completes")
	want := decodeExitResult(t, lastObservation(t, obs, "Completes"))

	const parallel = 4
	const sequential = 3
	results := make([]execbackend.ExitResult, parallel+sequential)
	errs := make([]error, parallel+sequential)

	ctx, cancel := context.WithTimeout(context.Background(), h.Timeout)
	defer cancel()

	var wg sync.WaitGroup
	for i := 0; i < parallel; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = sess.Wait(ctx)
		}(i)
	}
	wg.Wait()
	for i := parallel; i < parallel+sequential; i++ {
		results[i], errs[i] = sess.Wait(ctx)
	}

	for i := range results {
		if errs[i] != nil {
			t.Errorf("Wait call %d: %v", i, errs[i])
			continue
		}
		if !reflect.DeepEqual(results[i], want) {
			t.Errorf("Wait call %d result = %+v, want %+v (the exited observation payload)", i, results[i], want)
		}
	}
}

// 契约：Cancel 幂等；会话已经终结时返回 nil（已达到调用方想要的结果）。
// mode 不合法必须在任何状态判断之前返回 invalid_request（见 RunsUntilCancelled）。
func checkCompletesCancelAfterExit(t *testing.T, h Harness) {
	be := h.New(t, ScenarioCompletes)
	guard := newLeakGuard(t, h.Timeout)
	sess := startSession(t, h, be)
	defer closeSession(t, sess, guard)

	collectUntilClosed(t, sess, h.Timeout, "Completes")

	ctx, cancel := context.WithTimeout(context.Background(), h.Timeout)
	defer cancel()

	requireNoError(t, sess.Cancel(ctx, execbackend.CancelForce),
		"Cancel(force) after the session exited")
	requireNoError(t, sess.Cancel(ctx, execbackend.CancelForce),
		"repeated Cancel(force) after the session exited")
	if h.SupportsGracefulCancel {
		requireNoError(t, sess.Cancel(ctx, execbackend.CancelGraceful),
			"Cancel(graceful) after the session exited")
	}
}

// 契约：Close 幂等（多次 Close 都返回 nil）；Close 后 Wait 立即返回同一最终结果；
// Close 后 Approve/Cancel 返回 session_closed（types.go 的 Close 语义，与 mode 无关）。
func checkCompletesCloseIdempotent(t *testing.T, h Harness) {
	be := h.New(t, ScenarioCompletes)
	guard := newLeakGuard(t, h.Timeout)
	sess := startSession(t, h, be)
	defer closeSession(t, sess, guard)

	obs := collectUntilClosed(t, sess, h.Timeout, "Completes")
	want := decodeExitResult(t, lastObservation(t, obs, "Completes"))

	for i := 1; i <= 3; i++ {
		if err := sess.Close(); err != nil {
			t.Errorf("Close #%d: %v, want nil (Close 必须幂等)", i, err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), h.Timeout)
	defer cancel()

	got, err := sess.Wait(ctx)
	requireNoError(t, err, "Wait after Close")
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Wait after Close = %+v, want %+v (Close 不改变最终结果)", got, want)
	}
	requireCode(t,
		sess.Approve(ctx, execbackend.ApprovalDecision{
			ApprovalID:  contractApprovalID,
			ProviderRef: h.ApprovalProviderRef,
			Approved:    true,
		}),
		execbackend.CodeSessionClosed, "Approve after Close")
	requireCode(t, sess.Cancel(ctx, execbackend.CancelForce),
		execbackend.CodeSessionClosed, "Cancel(force) after Close")
	requireCode(t, sess.Cancel(ctx, execbackend.CancelGraceful),
		execbackend.CodeSessionClosed, "Cancel(graceful) after Close")
}

// ---------------------------------------------------------------------------
// RunsUntilCancelled
// ---------------------------------------------------------------------------

// 契约：CancelForce 永远可用（不受能力闸控），使会话以 cancelled 终结，Wait 与终结
// 观察都反映该结果；重复 Cancel 是 no-op；mode 不合法返回 invalid_request。
func checkRunsUntilCancelledForce(t *testing.T, h Harness) {
	be := h.New(t, ScenarioRunsUntilCancelled)
	guard := newLeakGuard(t, h.Timeout)
	sess := startSession(t, h, be)
	defer closeSession(t, sess, guard)

	first := recvObservation(t, sess, h.Timeout, "RunsUntilCancelled")
	if first.Kind != execbackend.ObservationProcessStarted {
		t.Errorf("first observation kind = %s, want %s", first.Kind, execbackend.ObservationProcessStarted)
	}
	if h.Advance != nil {
		// ScenarioRunsUntilCancelled 必须一直运行：时间流逝不能让它自行终结。
		h.Advance(advanceProbe)
	}
	assertStillRunning(t, sess, "after the clock advanced by "+advanceProbe.String())

	ctx, cancel := context.WithTimeout(context.Background(), h.Timeout)
	defer cancel()

	requireCode(t, sess.Cancel(ctx, execbackend.CancelMode("sideways")),
		execbackend.CodeInvalidRequest, "Cancel with an unknown mode")
	assertStillRunning(t, sess, "after Cancel with an unknown mode")

	requireNoError(t, sess.Cancel(ctx, execbackend.CancelForce), "Cancel(force)")
	requireNoError(t, sess.Cancel(ctx, execbackend.CancelForce), "repeated Cancel(force)")

	result := waitWithin(t, sess, h.Timeout, "Wait after Cancel(force)")
	if result.Reason != execbackend.ExitReasonCancelled {
		t.Errorf("Wait reason after Cancel(force) = %s, want %s",
			result.Reason, execbackend.ExitReasonCancelled)
	}

	obs := collectUntilClosed(t, sess, h.Timeout, "RunsUntilCancelled after cancel")
	last := lastObservation(t, obs, "RunsUntilCancelled after cancel")
	if last.Kind != execbackend.ObservationExited {
		t.Fatalf("last observation after Cancel(force) = %s, want %s",
			last.Kind, execbackend.ObservationExited)
	}
	if decoded := decodeExitResult(t, last); !reflect.DeepEqual(decoded, result) {
		t.Errorf("exited observation payload = %+v, Wait result = %+v; 两者必须是同一最终结果",
			decoded, result)
	}
}

// 契约（能力分支）：SupportsGracefulCancel=true 时 CancelGraceful 也必须使会话以
// cancelled 终结；=false 时必须返回 capability_unavailable、会话仍在运行，随后
// CancelForce 仍能终结会话（§27.2 第 6 条：Run 必须始终能被停下）。
func checkRunsUntilCancelledGraceful(t *testing.T, h Harness) {
	be := h.New(t, ScenarioRunsUntilCancelled)
	guard := newLeakGuard(t, h.Timeout)
	sess := startSession(t, h, be)
	defer closeSession(t, sess, guard)

	first := recvObservation(t, sess, h.Timeout, "RunsUntilCancelled")
	if first.Kind != execbackend.ObservationProcessStarted {
		t.Errorf("first observation kind = %s, want %s", first.Kind, execbackend.ObservationProcessStarted)
	}

	ctx, cancel := context.WithTimeout(context.Background(), h.Timeout)
	defer cancel()

	err := sess.Cancel(ctx, execbackend.CancelGraceful)
	if h.SupportsGracefulCancel {
		t.Logf("branch: cancel_graceful is proven; CancelGraceful must terminate the session")
		requireNoError(t, err, "Cancel(graceful) with cancel_graceful proven")
	} else {
		t.Logf("branch: cancel_graceful is NOT proven; CancelGraceful must fail and force must still work")
		requireCode(t, err, execbackend.CodeCapabilityUnavailable,
			"Cancel(graceful) without cancel_graceful")
		assertStillRunning(t, sess, "after an unsupported Cancel(graceful)")
		requireNoError(t, sess.Cancel(ctx, execbackend.CancelForce),
			"Cancel(force) after an unsupported Cancel(graceful)")
	}

	result := waitWithin(t, sess, h.Timeout, "Wait after cancelling")
	if result.Reason != execbackend.ExitReasonCancelled {
		t.Errorf("Wait reason = %s, want %s", result.Reason, execbackend.ExitReasonCancelled)
	}
	obs := collectUntilClosed(t, sess, h.Timeout, "RunsUntilCancelled after cancel")
	last := lastObservation(t, obs, "RunsUntilCancelled after cancel")
	if last.Kind != execbackend.ObservationExited {
		t.Fatalf("last observation = %s, want %s", last.Kind, execbackend.ObservationExited)
	}
	if decoded := decodeExitResult(t, last); !reflect.DeepEqual(decoded, result) {
		t.Errorf("exited observation payload = %+v, Wait result = %+v; 两者必须是同一最终结果",
			decoded, result)
	}
}

// ---------------------------------------------------------------------------
// CloseWithoutReading
// ---------------------------------------------------------------------------

// 契约：调用方完全不读 channel 时，Close 仍必须在超时内返回，并且 Close 后
// Observations 已关闭（读得到 channel 关闭），不泄漏 goroutine。
func checkCloseWithoutReading(t *testing.T, h Harness) {
	be := h.New(t, ScenarioRunsUntilCancelled)
	guard := newLeakGuard(t, h.Timeout)
	sess := startSession(t, h, be)
	defer closeSession(t, sess, guard)

	closeWithin(t, sess, h.Timeout, "Close without reading observations")

	obs := collectUntilClosed(t, sess, h.Timeout, "observations after Close")
	t.Logf("observations delivered before/while closing: %s", describeObservations(obs))
}

// 契约：Close 后 Wait 立即返回最终结果（不阻塞、不返回 ctx 错误），结果本身合法。
func checkWaitAfterClose(t *testing.T, h Harness) {
	be := h.New(t, ScenarioRunsUntilCancelled)
	guard := newLeakGuard(t, h.Timeout)
	sess := startSession(t, h, be)
	defer closeSession(t, sess, guard)

	closeWithin(t, sess, h.Timeout, "Close without reading observations")

	probe := h.Timeout
	if probe > 2*time.Second {
		probe = 2 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), probe)
	defer cancel()

	result, err := sess.Wait(ctx)
	requireNoError(t, err, "Wait after Close")
	if err := result.Validate(); err != nil {
		t.Errorf("Wait after Close returned an invalid ExitResult (%+v): %v", result, err)
	}
}

// ---------------------------------------------------------------------------
// WaitContextCancel
// ---------------------------------------------------------------------------

// 契约：Wait 的 ctx 取消只终止本次等待（返回 ctx 错误），不改变会话结果——会话继续
// 运行，随后正常 Wait 仍返回最终结果。
func checkWaitContextCancel(t *testing.T, h Harness) {
	be := h.New(t, ScenarioRunsUntilCancelled)
	guard := newLeakGuard(t, h.Timeout)
	sess := startSession(t, h, be)
	defer closeSession(t, sess, guard)

	first := recvObservation(t, sess, h.Timeout, "WaitContextCancel")
	if first.Kind != execbackend.ObservationProcessStarted {
		t.Errorf("first observation kind = %s, want %s", first.Kind, execbackend.ObservationProcessStarted)
	}

	probeCtx, probeCancel := context.WithTimeout(context.Background(), ctxCancelProbe)
	_, err := sess.Wait(probeCtx)
	probeCancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Wait with an expiring ctx returned %v, want context.DeadlineExceeded "+
			"(会话仍在运行，ctx 取消只影响本次等待)", err)
	}
	assertStillRunning(t, sess, "after Wait's ctx was cancelled")

	ctx, cancel := context.WithTimeout(context.Background(), h.Timeout)
	defer cancel()

	requireNoError(t, sess.Cancel(ctx, execbackend.CancelForce), "Cancel(force)")
	result := waitWithin(t, sess, h.Timeout, "Wait after Cancel(force)")
	if result.Reason != execbackend.ExitReasonCancelled {
		t.Errorf("Wait reason = %s, want %s; ctx 取消不得改变会话结果",
			result.Reason, execbackend.ExitReasonCancelled)
	}

	obs := collectUntilClosed(t, sess, h.Timeout, "WaitContextCancel after cancel")
	last := lastObservation(t, obs, "WaitContextCancel after cancel")
	if decoded := decodeExitResult(t, last); !reflect.DeepEqual(decoded, result) {
		t.Errorf("exited observation payload = %+v, Wait result = %+v; 两者必须是同一最终结果",
			decoded, result)
	}
}

// ---------------------------------------------------------------------------
// RequestsApproval
// ---------------------------------------------------------------------------

// 契约：赞成场景必须投递 approval_required，ProviderRef 等于 Harness.ApprovalProviderRef，
// 且它出现在会话开始之后（第一条观察是 process_started）。
func checkApprovalRequiredObserved(t *testing.T, h Harness) {
	be := h.New(t, ScenarioRequestsApproval)
	report := mustCapabilities(t, be, h.Timeout)
	if !report.Has(execbackend.CapabilityApprovalHook) {
		t.Fatalf("ScenarioRequestsApproval requires the backend to prove capability %s "+
			"(capability report: %+v)", execbackend.CapabilityApprovalHook, report)
	}
	guard := newLeakGuard(t, h.Timeout)
	sess := startSession(t, h, be)
	defer closeSession(t, sess, guard)

	obs := readUntilKind(t, sess, execbackend.ObservationApprovalRequired, h.Timeout)
	approval := obs[len(obs)-1]
	if obs[0].Kind != execbackend.ObservationProcessStarted {
		t.Errorf("first observation kind = %s, want %s", obs[0].Kind, execbackend.ObservationProcessStarted)
	}
	if approval.ProviderRef != h.ApprovalProviderRef {
		t.Errorf("approval_required ProviderRef = %q, want %q (Harness.ApprovalProviderRef)",
			approval.ProviderRef, h.ApprovalProviderRef)
	}
}

// 契约：未知 ProviderRef 的 Approve 返回 invalid_request，且不产生副作用
// （会话仍在等待裁决）。
func checkApprovalUnknownRef(t *testing.T, h Harness) {
	be := h.New(t, ScenarioRequestsApproval)
	guard := newLeakGuard(t, h.Timeout)
	sess := startSession(t, h, be)
	defer closeSession(t, sess, guard)

	readUntilKind(t, sess, execbackend.ObservationApprovalRequired, h.Timeout)

	ctx, cancel := context.WithTimeout(context.Background(), h.Timeout)
	defer cancel()

	requireCode(t, sess.Approve(ctx, execbackend.ApprovalDecision{
		ApprovalID:  contractApprovalID,
		ProviderRef: h.ApprovalProviderRef + "-unknown",
		Approved:    true,
	}), execbackend.CodeInvalidRequest, "Approve with an unknown ProviderRef")
	assertStillRunning(t, sess, "after Approve with an unknown ProviderRef")
}

// 契约：批准待审批项返回 nil，会话随后以 exited{completed} 终结，Wait 与终结观察一致。
func checkApprovalCompletes(t *testing.T, h Harness) {
	be := h.New(t, ScenarioRequestsApproval)
	guard := newLeakGuard(t, h.Timeout)
	sess := startSession(t, h, be)
	defer closeSession(t, sess, guard)

	readUntilKind(t, sess, execbackend.ObservationApprovalRequired, h.Timeout)

	ctx, cancel := context.WithTimeout(context.Background(), h.Timeout)
	defer cancel()

	requireNoError(t, sess.Approve(ctx, execbackend.ApprovalDecision{
		ApprovalID:  contractApprovalID,
		ProviderRef: h.ApprovalProviderRef,
		Approved:    true,
	}), "Approve with the pending ProviderRef")

	result := waitWithin(t, sess, h.Timeout, "Wait after Approve")
	if result.Reason != execbackend.ExitReasonCompleted {
		t.Errorf("Wait reason after Approve = %s, want %s", result.Reason, execbackend.ExitReasonCompleted)
	}

	obs := collectUntilClosed(t, sess, h.Timeout, "RequestsApproval after Approve")
	last := lastObservation(t, obs, "RequestsApproval after Approve")
	if last.Kind != execbackend.ObservationExited {
		t.Fatalf("last observation after Approve = %s, want %s", last.Kind, execbackend.ObservationExited)
	}
	if decoded := decodeExitResult(t, last); !reflect.DeepEqual(decoded, result) {
		t.Errorf("exited observation payload = %+v, Wait result = %+v; 两者必须是同一最终结果",
			decoded, result)
	}
}

// ---------------------------------------------------------------------------
// Crashes
// ---------------------------------------------------------------------------

// 契约：崩溃场景以 exited{reason: crashed} 终结；Usage 合法——未知用量必须标 unknown
// 且数值字段缺席（nil），不得用 0 冒充未知（§27.6）。
func checkCrashes(t *testing.T, h Harness) {
	be := h.New(t, ScenarioCrashes)
	guard := newLeakGuard(t, h.Timeout)
	sess := startSession(t, h, be)
	defer closeSession(t, sess, guard)

	obs := collectUntilClosed(t, sess, h.Timeout, "Crashes")
	if len(obs) == 0 {
		t.Fatalf("no observations were delivered before the channel closed")
	}
	if obs[0].Kind != execbackend.ObservationProcessStarted {
		t.Errorf("first observation kind = %s, want %s", obs[0].Kind, execbackend.ObservationProcessStarted)
	}
	result := decodeExitResult(t, lastObservation(t, obs, "Crashes"))
	if result.Reason != execbackend.ExitReasonCrashed {
		t.Errorf("terminal reason = %s, want %s", result.Reason, execbackend.ExitReasonCrashed)
	}
	if err := result.Usage.Validate(); err != nil {
		t.Errorf("crashed ExitResult usage is invalid: %v (usage=%+v)", err, result.Usage)
	}
	if result.Usage.Quality == execbackend.UsageQualityUnknown {
		if result.Usage.InputTokens != nil || result.Usage.OutputTokens != nil || result.Usage.CostMinor != nil {
			t.Errorf("usage quality is unknown but numeric fields are present: %+v; "+
				"未知用量必须字段缺席而不是 0", result.Usage)
		}
	}

	got := waitWithin(t, sess, h.Timeout, "Wait after a crash")
	if !reflect.DeepEqual(got, result) {
		t.Errorf("Wait result = %+v, want %+v (the exited observation payload)", got, result)
	}
}

// ---------------------------------------------------------------------------
// 断言与读取辅助
// ---------------------------------------------------------------------------

// requireNoError 断言 err 为 nil。
func requireNoError(t *testing.T, err error, what string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v, want nil", what, err)
	}
}

// requireCode 断言 err 表示失败 code=code。
//
// 两种表达都被接受（errors.go 同时提供）：Code=code 的 *execbackend.Error，
// 或与 Code* 一一对应的哨兵（如直接返回 ErrSessionClosed）。
// 返回 *execbackend.Error（哨兵时为 nil），供调用方继续检查 Field/Missing。
func requireCode(t *testing.T, err error, code string, what string) *execbackend.Error {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: got nil error, want code %s", what, code)
	}
	var e *execbackend.Error
	if errors.As(err, &e) {
		if e.Code != code {
			t.Fatalf("%s: error = %v, code = %s, want %s", what, err, e.Code, code)
		}
		return e
	}
	if sentinel := sentinelForCode(code); sentinel != nil && errors.Is(err, sentinel) {
		return nil
	}
	t.Fatalf("%s: error = %v (%T), want code %s (either *execbackend.Error or the matching sentinel)",
		what, err, err, code)
	return nil
}

// sentinelForCode 把错误 code 映射到 errors.go 里的哨兵，未知 code 返回 nil。
func sentinelForCode(code string) error {
	switch code {
	case execbackend.CodeCapabilityUnavailable:
		return execbackend.ErrCapabilityUnavailable
	case execbackend.CodeBackendUnavailable:
		return execbackend.ErrBackendUnavailable
	case execbackend.CodeProcessStartFailed:
		return execbackend.ErrProcessStartFailed
	case execbackend.CodeInvalidRequest:
		return execbackend.ErrInvalidRequest
	case execbackend.CodeSessionClosed:
		return execbackend.ErrSessionClosed
	default:
		return nil
	}
}

// assertStillRunning 断言会话尚未终结：用短超时 ctx 调用 Wait 必须返回 ctx 错误。
func assertStillRunning(t *testing.T, sess execbackend.Session, what string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), ctxCancelProbe)
	defer cancel()
	result, err := sess.Wait(ctx)
	if err == nil {
		t.Fatalf("%s: the session terminated with %+v, want it to still be running", what, result)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("%s: Wait returned %v, want context.DeadlineExceeded", what, err)
	}
}

// mustCapabilities 读取能力报告，失败即致命。
func mustCapabilities(t *testing.T, be execbackend.Backend, timeout time.Duration) execbackend.CapabilityReport {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	report, err := be.Capabilities(ctx)
	requireNoError(t, err, "Capabilities")
	return report
}

// firstUnproven 返回报告里第一个未被证明的能力；全部已证明时返回空串。
func firstUnproven(report execbackend.CapabilityReport) execbackend.Capability {
	for _, c := range execbackend.AllCapabilities() {
		if !report.Has(c) {
			return c
		}
	}
	return ""
}

// containsCapability 报告 caps 是否包含 want。
func containsCapability(caps []execbackend.Capability, want execbackend.Capability) bool {
	for _, c := range caps {
		if c == want {
			return true
		}
	}
	return false
}

// startSession 用 Harness 的合法请求 Prepare + Start 一次会话。
//
// 传入 Prepare/Start 的 ctx 绑定子测试生命周期（t.Cleanup 取消），不随调用返回而取消：
// 契约只要求 ctx 覆盖启动调用，但不能假定实现已把会话与启动 ctx 解绑。
func startSession(t *testing.T, h Harness, be execbackend.Backend) execbackend.Session {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), h.Timeout)
	t.Cleanup(cancel)

	req := h.Request(t)
	prepared, err := be.Prepare(ctx, req)
	requireNoError(t, err, "Prepare")
	sess, err := be.Start(ctx, prepared)
	requireNoError(t, err, "Start")
	if sess == nil {
		t.Fatalf("Start returned a nil Session")
	}
	return sess
}

// recvObservation 读一条观察，超时即致命。
func recvObservation(t *testing.T, sess execbackend.Session, timeout time.Duration, what string) execbackend.Observation {
	t.Helper()
	select {
	case o, ok := <-sess.Observations():
		if !ok {
			t.Fatalf("%s: Observations channel closed before the expected observation", what)
		}
		assertObservationValid(t, o)
		return o
	case <-time.After(timeout):
		t.Fatalf("%s: timed out after %s waiting for an observation", what, timeout)
		return execbackend.Observation{}
	}
}

// collectUntilClosed 读取全部观察直到 channel 关闭（每条都 Validate），超时即致命。
func collectUntilClosed(t *testing.T, sess execbackend.Session, timeout time.Duration, what string) []execbackend.Observation {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var out []execbackend.Observation
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			t.Fatalf("%s: timed out after %s waiting for the Observations channel to close; "+
				"collected %d observations: %s", what, timeout, len(out), describeObservations(out))
		}
		select {
		case o, ok := <-sess.Observations():
			if !ok {
				return out
			}
			assertObservationValid(t, o)
			out = append(out, o)
		case <-time.After(remaining):
			t.Fatalf("%s: timed out after %s waiting for the Observations channel to close; "+
				"collected %d observations: %s", what, timeout, len(out), describeObservations(out))
		}
	}
}

// readUntilKind 读到指定 kind（含）为止，返回读到的观察（每条都 Validate）。
func readUntilKind(t *testing.T, sess execbackend.Session, kind execbackend.ObservationKind,
	timeout time.Duration) []execbackend.Observation {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var out []execbackend.Observation
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			t.Fatalf("timed out after %s waiting for observation kind %s; collected %d observations: %s",
				timeout, kind, len(out), describeObservations(out))
		}
		select {
		case o, ok := <-sess.Observations():
			if !ok {
				t.Fatalf("Observations channel closed before observation kind %s appeared: %s",
					kind, describeObservations(out))
			}
			assertObservationValid(t, o)
			out = append(out, o)
			if o.Kind == kind {
				return out
			}
		case <-time.After(remaining):
			t.Fatalf("timed out after %s waiting for observation kind %s; collected %d observations: %s",
				timeout, kind, len(out), describeObservations(out))
		}
	}
}

// assertObservationValid 断言单条观察满足协议（Observation.Validate）。
func assertObservationValid(t *testing.T, o execbackend.Observation) {
	t.Helper()
	if err := o.Validate(); err != nil {
		t.Errorf("observation kind=%s provider_ref=%q failed Validate: %v",
			o.Kind, o.ProviderRef, err)
	}
}

// decodeExitResult 解码一条 exited 观察的 ExitResult（必为终结观察）。
func decodeExitResult(t *testing.T, o execbackend.Observation) execbackend.ExitResult {
	t.Helper()
	if o.Kind != execbackend.ObservationExited {
		t.Fatalf("observation kind = %s, want %s", o.Kind, execbackend.ObservationExited)
	}
	var result execbackend.ExitResult
	if err := json.Unmarshal(o.Payload, &result); err != nil {
		t.Fatalf("exited payload is not a valid ExitResult: %v (payload=%s)", err, o.Payload)
	}
	t.Logf("exited observation: reason=%s exit_code=%v retryable=%v usage.quality=%s",
		result.Reason, formatExitCode(result.ExitCode), result.Retryable, result.Usage.Quality)
	return result
}

// formatExitCode 渲染可空的退出码（nil 表示未知，不得显示成 0）。
func formatExitCode(code *int) string {
	if code == nil {
		return "<unknown>"
	}
	return strconv.Itoa(*code)
}

// lastObservation 返回最后一条观察；空序列即致命。
func lastObservation(t *testing.T, obs []execbackend.Observation, what string) execbackend.Observation {
	t.Helper()
	if len(obs) == 0 {
		t.Fatalf("%s: no observations were delivered", what)
	}
	return obs[len(obs)-1]
}

// waitWithin 在超时内调用 Wait，要求返回 nil error 并给出最终结果。
func waitWithin(t *testing.T, sess execbackend.Session, timeout time.Duration, what string) execbackend.ExitResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	result, err := sess.Wait(ctx)
	requireNoError(t, err, what)
	if err := result.Validate(); err != nil {
		t.Errorf("%s: returned an invalid ExitResult (%+v): %v", what, result, err)
	}
	return result
}

// closeWithin 在超时内关闭会话；超时即致命（Close 必须释放资源，不许挂住调用方）。
func closeWithin(t *testing.T, sess execbackend.Session, timeout time.Duration, what string) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- sess.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("%s: Close: %v, want nil", what, err)
		}
	case <-time.After(timeout):
		t.Fatalf("%s: Close did not return within %s", what, timeout)
	}
}

// closeSession 幂等关闭会话并断言无 goroutine 泄漏（子测试结束时统一 defer）。
func closeSession(t *testing.T, sess execbackend.Session, guard *leakGuard) {
	t.Helper()
	if err := sess.Close(); err != nil {
		t.Errorf("Close: %v, want nil", err)
	}
	if err := sess.Close(); err != nil {
		t.Errorf("second Close: %v, want nil (Close 必须幂等)", err)
	}
	guard.check()
}

// describeObservations 把观察序列压成 "kind:provider_ref" 便于读失败信息。
func describeObservations(obs []execbackend.Observation) string {
	if len(obs) == 0 {
		return "<none>"
	}
	parts := make([]string, 0, len(obs))
	for _, o := range obs {
		parts = append(parts, string(o.Kind)+":"+o.ProviderRef)
	}
	return strings.Join(parts, ",")
}

// ---------------------------------------------------------------------------
// goroutine 泄漏检查
// ---------------------------------------------------------------------------

// leakGuard 记录基线 goroutine 数，Close 后断言回落到基线 +2 以内。
//
// 基线在构造后端之后、Start 之前记录：suite 判定的是"会话不泄漏 goroutine"，
// 不把后端自身的常驻 goroutine（若实现选择这么做）算作泄漏。
type leakGuard struct {
	t       *testing.T
	base    int
	timeout time.Duration
}

// newLeakGuard 记录当前 goroutine 数作为基线。
func newLeakGuard(t *testing.T, timeout time.Duration) *leakGuard {
	t.Helper()
	return &leakGuard{t: t, base: runtime.NumGoroutine(), timeout: timeout}
}

// check 等待 goroutine 回落到基线 +2 以内；超时则打印全部栈。
func (g *leakGuard) check() {
	g.t.Helper()
	deadline := time.Now().Add(g.timeout)
	for {
		now := runtime.NumGoroutine()
		if now <= g.base+2 {
			g.t.Logf("goroutines: baseline=%d after Close=%d", g.base, now)
			return
		}
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<20)
			n := runtime.Stack(buf, true)
			g.t.Errorf("goroutine leak after Close: baseline=%d now=%d (limit baseline+2)\n%s",
				g.base, now, buf[:n])
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}
