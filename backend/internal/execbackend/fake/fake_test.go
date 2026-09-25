package fake

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/execbackend"
)

// 本文件是 T1.06.b 的验收测试（§15 T1.06、§28 T1.06.b）。
// 每个测试结束前都断言无 goroutine 泄漏：Close 后回落到基线 +2 以内。

// testStart 是手动时钟的起点（固定值，保证可复现）。
var testStart = time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

const (
	obsTimeout  = 3 * time.Second
	idleTimeout = 50 * time.Millisecond
)

// validRequest 返回一个合法 StartRequest；调用方可继续改字段构造非法输入。
func validRequest(t *testing.T) execbackend.StartRequest {
	t.Helper()
	return execbackend.StartRequest{
		Run: execbackend.RunRef{
			ProjectID:       "proj-1",
			RunID:           "run-1",
			AttemptID:       "attempt-1",
			AgentRevisionID: "agent-rev-1",
		},
		Input: execbackend.FrozenInput{
			SnapshotID:   "snap-1",
			SnapshotHash: "sha256:" + strings.Repeat("a", 64),
			Prompt:       "run the tool sequence",
		},
		WorkDir: filepath.Clean(t.TempDir()),
		Process: execbackend.ProcessControl{GracePeriod: 5 * time.Second},
	}
}

// startSession 用 be.Prepare + be.Start 启动一次会话。
func startSession(t *testing.T, be *Backend, req execbackend.StartRequest) execbackend.Session {
	t.Helper()
	prepared, err := be.Prepare(context.Background(), req)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	sess, err := be.Start(context.Background(), prepared)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	return sess
}

// asSession 取出测试用的具体会话类型（同一包内可见内部断言辅助）。
func asSession(t *testing.T, sess execbackend.Session) *session {
	t.Helper()
	concrete, ok := sess.(*session)
	if !ok {
		t.Fatalf("session type = %T, want *fake.session", sess)
	}
	return concrete
}

// recv 读取一条观察；超时即失败。
func recv(t *testing.T, sess execbackend.Session, timeout time.Duration) (execbackend.Observation, bool) {
	t.Helper()
	select {
	case o, ok := <-sess.Observations():
		return o, ok
	case <-time.After(timeout):
		t.Fatalf("timed out after %s waiting for an observation", timeout)
		return execbackend.Observation{}, false
	}
}

// collect 读到 channel 关闭为止（或超时失败），返回全部观察。
func collect(t *testing.T, sess execbackend.Session, timeout time.Duration) []execbackend.Observation {
	t.Helper()
	var out []execbackend.Observation
	deadline := time.After(timeout)
	for {
		select {
		case o, ok := <-sess.Observations():
			if !ok {
				return out
			}
			out = append(out, o)
		case <-deadline:
			t.Fatalf("timed out after %s; collected %d observations, last=%s", timeout, len(out), describe(out))
			return out
		}
	}
}

// assertNoFrame 断言在 d 内没有任何观察（负向断言，必须用真实时间窗口）。
func assertNoFrame(t *testing.T, sess execbackend.Session, d time.Duration) {
	t.Helper()
	select {
	case o, ok := <-sess.Observations():
		if !ok {
			t.Fatalf("observations channel closed unexpectedly")
		}
		t.Fatalf("unexpected observation kind=%s ref=%s", o.Kind, o.ProviderRef)
	case <-time.After(d):
	}
}

// refsOf 把观察序列压成 "kind:provider_ref" 便于逐条比对。
func refsOf(obs []execbackend.Observation) []string {
	out := make([]string, 0, len(obs))
	for _, o := range obs {
		out = append(out, string(o.Kind)+":"+o.ProviderRef)
	}
	return out
}

func describe(obs []execbackend.Observation) string {
	if len(obs) == 0 {
		return "<none>"
	}
	return strings.Join(refsOf(obs), ",")
}

// assertSameSequence 比对观察序列，失败时给出可读差异。
func assertSameSequence(t *testing.T, want, got []string) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("observation count = %d, want %d\n got: %s\nwant: %s",
			len(got), len(want), strings.Join(got, ","), strings.Join(want, ","))
	}
	for i := range want {
		if want[i] != got[i] {
			t.Fatalf("observation[%d] = %q, want %q\n got: %s\nwant: %s",
				i, got[i], want[i], strings.Join(got, ","), strings.Join(want, ","))
		}
	}
}

// assertAllValid 断言每条观察都满足协议（含 exited 的 ExitResult）。
func assertAllValid(t *testing.T, obs []execbackend.Observation) {
	t.Helper()
	for i, o := range obs {
		if err := o.Validate(); err != nil {
			t.Fatalf("observation[%d] kind=%s invalid: %v", i, o.Kind, err)
		}
		if o.ObservedAt.IsZero() {
			t.Fatalf("observation[%d] kind=%s has zero ObservedAt", i, o.Kind)
		}
	}
}

// exitResultOf 解码一条 exited 观察的 payload。
func exitResultOf(t *testing.T, o execbackend.Observation) execbackend.ExitResult {
	t.Helper()
	if o.Kind != execbackend.ObservationExited {
		t.Fatalf("observation kind = %s, want exited", o.Kind)
	}
	var result execbackend.ExitResult
	if err := json.Unmarshal(o.Payload, &result); err != nil {
		t.Fatalf("exited payload is not an ExitResult: %v (payload=%s)", err, o.Payload)
	}
	return result
}

// mustClose 幂等关闭并检查 goroutine 回落。
func mustClose(t *testing.T, sess execbackend.Session, base int) {
	t.Helper()
	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	checkNoLeak(t, base)
}

// checkNoLeak 断言 goroutine 数在短时间内回落到 base+2 以内。
func checkNoLeak(t *testing.T, base int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		now := runtime.NumGoroutine()
		if now <= base+2 {
			return
		}
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<20)
			n := runtime.Stack(buf, true)
			t.Fatalf("goroutine leak: base=%d now=%d\n%s", base, now, buf[:n])
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// ---------------------------------------------------------------------------
// 正常路径：工具序列 + 审批通过
// ---------------------------------------------------------------------------

func TestRunToolSequenceOrder(t *testing.T) {
	base := runtime.NumGoroutine()
	clock := NewManualClock(testStart)
	be := New(Options{Clock: clock, Script: RunToolSequence()})
	sess := startSession(t, be, validRequest(t))
	concrete := asSession(t, sess)
	defer mustClose(t, sess, base)

	// 审批之前的固定序列：process_started → output → 四个工具的成对帧 → test 的请求与审批。
	want := []string{"process_started:", "output:msg-0001"}
	for _, tool := range ToolSequenceTools() {
		ref := RunToolSequenceProviderRef(tool)
		want = append(want, "tool_requested:"+ref)
		if tool == "test" {
			want = append(want, "approval_required:"+ref)
			break
		}
		want = append(want, "tool_result:"+ref)
	}

	got := make([]string, 0, len(want))
	for i := range want {
		o, ok := recv(t, sess, obsTimeout)
		if !ok {
			t.Fatalf("observations closed early at frame %d (got %s)", i, strings.Join(got, ","))
		}
		if err := o.Validate(); err != nil {
			t.Fatalf("observation[%d] invalid: %v", i, err)
		}
		got = append(got, string(o.Kind)+":"+o.ProviderRef)
	}
	assertSameSequence(t, want, got)

	// 审批前阻塞：脚本停在审批等待上，推进手动时钟也不会继续。
	if !concrete.awaitingApproval("tool-test-1") {
		t.Fatalf("session is not parked on approval for tool-test-1")
	}
	if concrete.isDone() {
		t.Fatalf("session must not be done before approval")
	}
	if pending := concrete.pendingFrames(); pending != 0 {
		t.Fatalf("pending frames before approval = %d, want 0", pending)
	}
	clock.Advance(24 * time.Hour)
	assertNoFrame(t, sess, idleTimeout)

	// 批准后继续：tool_result(test) → usage → exited(completed, exit 0)。
	if err := sess.Approve(context.Background(), execbackend.ApprovalDecision{
		ApprovalID:  "appr-1",
		ProviderRef: "tool-test-1",
		Approved:    true,
	}); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	rest := collect(t, sess, obsTimeout)
	assertSameSequence(t, []string{"tool_result:tool-test-1", "usage:msg-usage-1", "exited:"}, refsOf(rest))
	assertAllValid(t, rest)

	result := exitResultOf(t, rest[len(rest)-1])
	if result.Reason != execbackend.ExitReasonCompleted {
		t.Fatalf("exit reason = %s, want completed", result.Reason)
	}
	if result.ExitCode == nil || *result.ExitCode != 0 {
		t.Fatalf("exit code = %v, want 0", result.ExitCode)
	}
	if result.Usage.Quality != execbackend.UsageQualityReported {
		t.Fatalf("usage quality = %s, want reported", result.Usage.Quality)
	}
	if result.Usage.InputTokens == nil || *result.Usage.InputTokens != 1200 ||
		result.Usage.OutputTokens == nil || *result.Usage.OutputTokens != 340 {
		t.Fatalf("usage tokens = %+v, want 1200/340", result.Usage)
	}

	waitResult, err := sess.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if waitResult.Reason != result.Reason || waitResult.ExitCode == nil || *waitResult.ExitCode != *result.ExitCode {
		t.Fatalf("Wait result %+v != exited observation %+v", waitResult, result)
	}
	if _, ok := recv(t, sess, idleTimeout); ok {
		t.Fatalf("observations channel is not closed after exited")
	}
}

func TestApprovalRejectedEmitsDenied(t *testing.T) {
	base := runtime.NumGoroutine()
	clock := NewManualClock(testStart)
	script := []Step{
		Emit(execbackend.ObservationToolRequested, "appr-1", map[string]any{
			"tool":         "command",
			"tool_call_id": "appr-1",
		}),
		AwaitApproval("appr-1", map[string]any{"cmd": "rm -rf build"}),
		IfApproved(Emit(execbackend.ObservationToolResult, "appr-1", map[string]any{"ok": true})),
		IfDenied(Emit(execbackend.ObservationOutput, "msg-denied-note", map[string]any{
			"stream": "stderr",
			"text":   "approval denied",
		})),
		Exit(0, execbackend.ExitReasonCompleted, UnknownUsage()),
	}
	be := New(Options{Clock: clock, Script: script})
	sess := startSession(t, be, validRequest(t))
	defer mustClose(t, sess, base)

	// process_started + tool_requested + approval_required
	for i := 0; i < 3; i++ {
		if _, ok := recv(t, sess, obsTimeout); !ok {
			t.Fatalf("observations closed early at frame %d", i)
		}
	}
	if err := sess.Approve(context.Background(), execbackend.ApprovalDecision{
		ApprovalID:  "appr-1",
		ProviderRef: "appr-1",
		Approved:    false,
		Reason:      "policy denies",
	}); err != nil {
		t.Fatalf("Approve(false): %v", err)
	}

	rest := collect(t, sess, obsTimeout)
	assertSameSequence(t, []string{"tool_result:appr-1", "output:msg-denied-note", "exited:"}, refsOf(rest))
	assertAllValid(t, rest)

	var denied map[string]any
	if err := json.Unmarshal(rest[0].Payload, &denied); err != nil {
		t.Fatalf("denied tool_result payload: %v", err)
	}
	if approved, ok := denied["denied"].(bool); !ok || !approved {
		t.Fatalf("denied tool_result payload = %s, want denied=true", rest[0].Payload)
	}
	if reason, _ := denied["reason"].(string); reason != "policy denies" {
		t.Fatalf("denied reason = %q, want %q", reason, "policy denies")
	}
	if result := exitResultOf(t, rest[len(rest)-1]); result.Reason != execbackend.ExitReasonCompleted {
		t.Fatalf("exit reason = %s, want completed", result.Reason)
	}
}

// ---------------------------------------------------------------------------
// 审批失败路径
// ---------------------------------------------------------------------------

func TestApproveWithoutCapability(t *testing.T) {
	base := runtime.NumGoroutine()
	clock := NewManualClock(testStart)
	be := New(Options{
		Clock:        clock,
		Capabilities: []execbackend.Capability{execbackend.CapabilityNonInteractive},
		Script:       RunIdle(time.Hour),
	})
	sess := startSession(t, be, validRequest(t))
	defer mustClose(t, sess, base)

	err := sess.Approve(context.Background(), execbackend.ApprovalDecision{
		ApprovalID:  "appr-1",
		ProviderRef: "tool-test-1",
		Approved:    true,
	})
	if !errors.Is(err, execbackend.ErrCapabilityUnavailable) {
		t.Fatalf("Approve error = %v, want capability_unavailable", err)
	}
	var domain *execbackend.Error
	if !errors.As(err, &domain) {
		t.Fatalf("Approve error type = %T, want *execbackend.Error", err)
	}
	if domain.Code != execbackend.CodeCapabilityUnavailable {
		t.Fatalf("error code = %s, want %s", domain.Code, execbackend.CodeCapabilityUnavailable)
	}
	if domain.Capability != execbackend.CapabilityApprovalHook {
		t.Fatalf("error capability = %s, want approval_hook", domain.Capability)
	}
	if asSession(t, sess).isDone() {
		t.Fatalf("session must keep running when approval is unsupported")
	}
}

func TestApproveUnknownRef(t *testing.T) {
	base := runtime.NumGoroutine()
	clock := NewManualClock(testStart)
	be := New(Options{Clock: clock, Script: RunIdle(time.Hour)})
	sess := startSession(t, be, validRequest(t))
	defer mustClose(t, sess, base)

	for _, tc := range []struct {
		name        string
		providerRef string
	}{
		{name: "unknown ref", providerRef: "tool-nope-1"},
		{name: "empty ref", providerRef: ""},
	} {
		err := sess.Approve(context.Background(), execbackend.ApprovalDecision{
			ApprovalID:  "appr-1",
			ProviderRef: tc.providerRef,
			Approved:    true,
		})
		if !errors.Is(err, execbackend.ErrInvalidRequest) {
			t.Fatalf("%s: Approve error = %v, want invalid_request", tc.name, err)
		}
		var domain *execbackend.Error
		if !errors.As(err, &domain) || domain.Code != execbackend.CodeInvalidRequest {
			t.Fatalf("%s: error = %v, want code invalid_request", tc.name, err)
		}
		if domain.Field != "provider_ref" {
			t.Fatalf("%s: error field = %q, want provider_ref", tc.name, domain.Field)
		}
	}
}

func TestApproveAfterExit(t *testing.T) {
	base := runtime.NumGoroutine()
	clock := NewManualClock(testStart)
	be := New(Options{Clock: clock, Script: RunToolSequence()})
	sess := startSession(t, be, validRequest(t))
	defer mustClose(t, sess, base)

	// 读到审批帧，批准后让会话跑到终结。
	for {
		o, ok := recv(t, sess, obsTimeout)
		if !ok {
			t.Fatalf("observations closed before approval")
		}
		if o.Kind == execbackend.ObservationApprovalRequired {
			break
		}
	}
	if err := sess.Approve(context.Background(), execbackend.ApprovalDecision{
		ApprovalID:  "appr-1",
		ProviderRef: "tool-test-1",
		Approved:    true,
	}); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	collect(t, sess, obsTimeout)

	if !asSession(t, sess).isDone() {
		t.Fatalf("session should be done after exited")
	}
	err := sess.Approve(context.Background(), execbackend.ApprovalDecision{
		ApprovalID:  "appr-1",
		ProviderRef: "tool-test-1",
		Approved:    true,
	})
	if !errors.Is(err, execbackend.ErrSessionClosed) {
		t.Fatalf("Approve after exit error = %v, want ErrSessionClosed", err)
	}
}
