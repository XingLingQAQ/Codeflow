package fake

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/execbackend"
)

// TestAcceptanceEvidence 逐场景跑一遍并把关键数字打进日志（-v 时可见）：
// 终结 Reason、observation 计数、goroutine 前后值、取消耗时。
//
// 它是 T1.06.b 回执的取证入口，断言只做"必须成立"的部分；细粒度断言在
// fake_test.go / frames_test.go / lifecycle_test.go。
func TestAcceptanceEvidence(t *testing.T) {
	logScenario := func(name string, sess execbackend.Session, base, preRead int, note string) {
		t.Helper()
		obs := collect(t, sess, obsTimeout)
		result, err := sess.Wait(context.Background())
		after := runtime.NumGoroutine()
		reason := string(result.Reason)
		if err != nil && !errors.Is(err, context.Canceled) {
			reason += " (waitErr=" + err.Error() + ")"
		}
		t.Logf("scenario=%s observations=%d terminal_reason=%s goroutines_base=%d goroutines_after_close=%d %s",
			name, preRead+len(obs), reason, base, after, note)
		if err := sess.Close(); err != nil {
			t.Fatalf("%s: Close: %v", name, err)
		}
		checkNoLeak(t, base)
	}

	t.Run("tool-sequence-approved", func(t *testing.T) {
		base := runtime.NumGoroutine()
		be := New(Options{Clock: NewManualClock(testStart), Script: RunToolSequence()})
		sess := startSession(t, be, validRequest(t))
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
			ApprovalID: "appr-1", ProviderRef: "tool-test-1", Approved: true,
		}); err != nil {
			t.Fatalf("Approve: %v", err)
		}
		// 上面已经读掉 12 条，collect 只能看到剩余部分，这里单独重跑一遍完整计数。
		_ = collect(t, sess, obsTimeout)
		if err := sess.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		checkNoLeak(t, base)

		full := startSession(t, be, validRequest(t))
		for {
			o, ok := recv(t, full, obsTimeout)
			if !ok {
				t.Fatalf("observations closed before approval")
			}
			if o.Kind == execbackend.ObservationApprovalRequired {
				break
			}
		}
		if err := full.Approve(context.Background(), execbackend.ApprovalDecision{
			ApprovalID: "appr-1", ProviderRef: "tool-test-1", Approved: true,
		}); err != nil {
			t.Fatalf("Approve: %v", err)
		}
		logScenario("tool-sequence-approved", full, base, 12, "fixture=RunToolSequence tools=read,search,write,command,test")
	})

	t.Run("approval-denied", func(t *testing.T) {
		base := runtime.NumGoroutine()
		be := New(Options{Clock: NewManualClock(testStart), Script: RunSlowApproval("appr-1")})
		sess := startSession(t, be, validRequest(t))
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
			ApprovalID: "appr-1", ProviderRef: "appr-1", Approved: false, Reason: "denied by policy",
		}); err != nil {
			t.Fatalf("Approve(false): %v", err)
		}
		logScenario("approval-denied", sess, base, 3, "fixture=RunSlowApproval")
	})

	t.Run("hard-deadline", func(t *testing.T) {
		base := runtime.NumGoroutine()
		clock := NewManualClock(testStart)
		req := validRequest(t)
		req.Process.HardDeadline = testStart.Add(30 * time.Minute)
		be := New(Options{Clock: clock, Script: RunSlowApproval("appr-1")})
		sess := startSession(t, be, req)
		// 先读到审批帧（脚本确实卡在审批上），再推进时钟越过硬截止。
		preRead := 0
		for {
			o, ok := recv(t, sess, obsTimeout)
			if !ok {
				t.Fatalf("observations closed before approval")
			}
			preRead++
			if o.Kind == execbackend.ObservationApprovalRequired {
				break
			}
		}
		clock.Advance(31 * time.Minute)
		logScenario("hard-deadline-while-approval", sess, base, preRead, "fixture=RunSlowApproval deadline=+30m")
	})

	t.Run("crash", func(t *testing.T) {
		base := runtime.NumGoroutine()
		be := New(Options{Clock: NewManualClock(testStart), Script: []Step{
			Emit(execbackend.ObservationOutput, "msg-1", map[string]any{"stream": "stdout", "text": "boom"}),
			Crash(),
		}})
		sess := startSession(t, be, validRequest(t))
		logScenario("crash", sess, base, 0, "step=Crash()")
	})

	t.Run("cancel-under-backpressure", func(t *testing.T) {
		base := runtime.NumGoroutine()
		be := New(Options{
			Clock:             NewManualClock(testStart),
			Script:            []Step{Flood(5000, 256)},
			ObservationBuffer: 4,
		})
		sess := startSession(t, be, validRequest(t))
		concrete := asSession(t, sess)
		if _, ok := recv(t, sess, obsTimeout); !ok {
			t.Fatalf("observations closed immediately")
		}
		deadline := time.Now().Add(2 * time.Second)
		for concrete.pendingFrames() < cap(concrete.obs) {
			if time.Now().After(deadline) {
				t.Fatalf("script never hit backpressure")
			}
			time.Sleep(time.Millisecond)
		}
		start := time.Now()
		if err := sess.Cancel(context.Background(), execbackend.CancelForce); err != nil {
			t.Fatalf("Cancel: %v", err)
		}
		result, err := sess.Wait(context.Background())
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("Wait: %v", err)
		}
		pending := concrete.pendingFrames()
		t.Logf("scenario=cancel-under-backpressure terminal_reason=%s cancel_elapsed=%s pending_frames_at_close=%d buffer=%d goroutines_base=%d",
			result.Reason, elapsed.Round(time.Microsecond), pending, cap(concrete.obs), base)
		if elapsed > time.Second {
			t.Fatalf("cancel took %s, want < 1s", elapsed)
		}
		if err := sess.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		checkNoLeak(t, base)
	})

	t.Run("flood-no-loss", func(t *testing.T) {
		base := runtime.NumGoroutine()
		const count = 5000
		be := New(Options{Clock: NewManualClock(testStart), Script: []Step{
			Flood(count, 1024),
			Exit(0, execbackend.ExitReasonCompleted, UnknownUsage()),
		}})
		sess := startSession(t, be, validRequest(t))
		logScenario("flood", sess, base, 0, "frames=5000 payload=1024B (process_started+5000+exited=5002)")
	})
}
