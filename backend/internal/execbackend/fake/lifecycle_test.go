package fake

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/execbackend"
)

// 本文件覆盖超时、崩溃、取消、Close/Wait 幂等、Prepare 校验与能力报告。

func TestHardDeadlineTimesOut(t *testing.T) {
	cases := []struct {
		name   string
		script []Step
	}{
		{
			name:   "blocked on approval",
			script: RunSlowApproval("appr-deadline-1"),
		},
		{
			name:   "blocked in sleep",
			script: RunIdle(time.Hour),
		},
		{
			name:   "blocked on backpressure",
			script: []Step{Flood(5000, 256)},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := runtime.NumGoroutine()
			clock := NewManualClock(testStart)
			req := validRequest(t)
			req.Process.HardDeadline = testStart.Add(30 * time.Minute)
			opts := Options{Clock: clock, Script: tc.script}
			if tc.name == "blocked on backpressure" {
				opts.ObservationBuffer = 4
				// 消费掉 process_started 后不再读，让脚本卡在发送上。
				be := New(opts)
				sess := startSession(t, be, req)
				defer mustClose(t, sess, base)
				if o, ok := recv(t, sess, obsTimeout); !ok || o.Kind != execbackend.ObservationProcessStarted {
					t.Fatalf("first observation = %s/%v, want process_started", o.Kind, ok)
				}
				assertTimeoutAfterAdvance(t, sess, clock)
				return
			}
			be := New(opts)
			sess := startSession(t, be, req)
			defer mustClose(t, sess, base)
			assertTimeoutAfterAdvance(t, sess, clock)
		})
	}
}

// assertTimeoutAfterAdvance 读掉终结前的观察，推进手动时钟越过硬截止，断言以 timeout 终结。
func assertTimeoutAfterAdvance(t *testing.T, sess execbackend.Session, clock *ManualClock) {
	t.Helper()
	// 未推进时钟前不会有终结观察。
	if concrete := asSession(t, sess); concrete.isDone() {
		t.Fatalf("session must not finish before the hard deadline")
	}
	clock.Advance(31 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), obsTimeout)
	defer cancel()
	result, err := sess.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait after hard deadline: %v", err)
	}
	if result.Reason != execbackend.ExitReasonTimeout {
		t.Fatalf("exit reason = %s, want timeout", result.Reason)
	}
	if result.ExitCode != nil {
		t.Fatalf("timeout exit code = %v, want nil (unknown)", *result.ExitCode)
	}
	if result.Usage.Quality != execbackend.UsageQualityUnknown {
		t.Fatalf("timeout usage quality = %s, want unknown", result.Usage.Quality)
	}

	// 读尽剩余观察：最后一条必须是 exited{timeout}（Cancel/Close 之外不丢终结帧）。
	var last execbackend.Observation
	var seen bool
	deadline := time.After(obsTimeout)
readLoop:
	for {
		select {
		case o, ok := <-sess.Observations():
			if !ok {
				break readLoop
			}
			if err := o.Validate(); err != nil {
				t.Fatalf("observation kind=%s invalid: %v", o.Kind, err)
			}
			last, seen = o, true
		case <-deadline:
			t.Fatalf("timed out draining observations after hard deadline")
		}
	}
	if !seen || last.Kind != execbackend.ObservationExited {
		t.Fatalf("last observation = %s/%v, want exited", last.Kind, seen)
	}
	if got := exitResultOf(t, last); got.Reason != execbackend.ExitReasonTimeout {
		t.Fatalf("terminal reason = %s, want timeout", got.Reason)
	}
}

func TestHardDeadlineAlreadyPast(t *testing.T) {
	base := runtime.NumGoroutine()
	clock := NewManualClock(testStart)
	req := validRequest(t)
	req.Process.HardDeadline = testStart.Add(-time.Minute)
	be := New(Options{Clock: clock, Script: RunIdle(time.Hour)})
	sess := startSession(t, be, req)
	defer mustClose(t, sess, base)

	ctx, cancel := context.WithTimeout(context.Background(), obsTimeout)
	defer cancel()
	result, err := sess.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if result.Reason != execbackend.ExitReasonTimeout {
		t.Fatalf("exit reason = %s, want timeout", result.Reason)
	}
	last := collect(t, sess, obsTimeout)
	if len(last) == 0 || last[len(last)-1].Kind != execbackend.ObservationExited {
		t.Fatalf("observations = %s, want trailing exited", describe(last))
	}
}

func TestCrashEndsSession(t *testing.T) {
	base := runtime.NumGoroutine()
	be := New(Options{
		Clock: NewManualClock(testStart),
		Script: []Step{
			Emit(execbackend.ObservationOutput, "msg-1", map[string]any{"stream": "stdout", "text": "about to crash"}),
			Crash(),
		},
	})
	sess := startSession(t, be, validRequest(t))
	defer mustClose(t, sess, base)

	all := collect(t, sess, obsTimeout)
	assertSameSequence(t, []string{"process_started:", "output:msg-1", "exited:"}, refsOf(all))
	assertAllValid(t, all)

	result := exitResultOf(t, all[len(all)-1])
	if result.Reason != execbackend.ExitReasonCrashed {
		t.Fatalf("exit reason = %s, want crashed", result.Reason)
	}
	if result.Retryable {
		t.Fatalf("crashed result must not be retryable")
	}
	if result.ExitCode != nil {
		t.Fatalf("crashed exit code = %v, want nil", *result.ExitCode)
	}
	if result.Usage.Quality != execbackend.UsageQualityUnknown {
		t.Fatalf("crashed usage quality = %s, want unknown", result.Usage.Quality)
	}
	if result.Usage.InputTokens != nil || result.Usage.OutputTokens != nil || result.Usage.CostMinor != nil {
		t.Fatalf("crashed usage must stay nil, got %+v", result.Usage)
	}
	waitResult, err := sess.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if waitResult.Reason != execbackend.ExitReasonCrashed {
		t.Fatalf("Wait reason = %s, want crashed", waitResult.Reason)
	}
}

func TestCancelGracefulAndIdempotent(t *testing.T) {
	base := runtime.NumGoroutine()
	clock := NewManualClock(testStart)
	be := New(Options{
		Clock: clock,
		Script: []Step{
			Emit(execbackend.ObservationOutput, "msg-1", map[string]any{"stream": "stdout", "text": "first"}),
			Sleep(time.Hour),
		},
	})
	sess := startSession(t, be, validRequest(t))
	defer mustClose(t, sess, base)

	first, ok := recv(t, sess, obsTimeout)
	if !ok || first.ObservedAt != testStart {
		t.Fatalf("process_started ObservedAt = %s, want %s", first.ObservedAt, testStart)
	}
	second, _ := recv(t, sess, obsTimeout)
	if second.ObservedAt != testStart {
		t.Fatalf("output ObservedAt = %s, want %s (manual clock)", second.ObservedAt, testStart)
	}
	clock.Advance(time.Minute)

	if err := sess.Cancel(context.Background(), execbackend.CancelGraceful); err != nil {
		t.Fatalf("Cancel(graceful): %v", err)
	}
	rest := collect(t, sess, obsTimeout)
	assertSameSequence(t, []string{"exited:"}, refsOf(rest))
	if result := exitResultOf(t, rest[0]); result.Reason != execbackend.ExitReasonCancelled {
		t.Fatalf("exit reason = %s, want cancelled", result.Reason)
	}
	// 终结观察的 ObservedAt 必须取 Cancel 之后的时钟值（不是脚本起点的旧值）。
	if want := testStart.Add(time.Minute); rest[0].ObservedAt != want {
		t.Fatalf("exited ObservedAt = %s, want %s", rest[0].ObservedAt, want)
	}

	// 幂等：会话已终结时再次 Cancel 返回 nil，不产生第二条终结观察。
	if err := sess.Cancel(context.Background(), execbackend.CancelGraceful); err != nil {
		t.Fatalf("second Cancel(graceful) = %v, want nil", err)
	}
	if err := sess.Cancel(context.Background(), execbackend.CancelForce); err != nil {
		t.Fatalf("Cancel(force) after terminal = %v, want nil", err)
	}
	if concrete := asSession(t, sess); concrete.pendingFrames() != 0 {
		t.Fatalf("cancel produced extra frames: %d", concrete.pendingFrames())
	}

	result, err := sess.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if result.Reason != execbackend.ExitReasonCancelled {
		t.Fatalf("Wait reason = %s, want cancelled", result.Reason)
	}
}

func TestCancelForceWhileFlooding(t *testing.T) {
	base := runtime.NumGoroutine()
	be := New(Options{
		Clock:             NewManualClock(testStart),
		Script:            []Step{Flood(2000, 128)},
		ObservationBuffer: 8,
	})
	sess := startSession(t, be, validRequest(t))
	defer mustClose(t, sess, base)

	// 边读边取消：不需要读到全部帧，但终结观察必须是 exited{cancelled}。
	deadline := time.After(2 * time.Second)
	cancelled := false
	for {
		select {
		case o, ok := <-sess.Observations():
			if !ok {
				if !cancelled {
					t.Fatalf("observations closed without an exited{cancelled} frame")
				}
				return
			}
			if o.Kind == execbackend.ObservationOutput && !cancelled {
				if err := sess.Cancel(context.Background(), execbackend.CancelForce); err != nil {
					t.Fatalf("Cancel(force): %v", err)
				}
				cancelled = true
			}
			if o.Kind == execbackend.ObservationExited {
				result := exitResultOf(t, o)
				if result.Reason != execbackend.ExitReasonCancelled {
					t.Fatalf("terminal reason = %s, want cancelled", result.Reason)
				}
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for cancel to take effect")
		}
	}
}

func TestCancelInvalidModeAndWithoutCapability(t *testing.T) {
	base := runtime.NumGoroutine()
	clock := NewManualClock(testStart)
	t.Run("invalid mode", func(t *testing.T) {
		be := New(Options{Clock: clock, Script: RunIdle(time.Hour)})
		sess := startSession(t, be, validRequest(t))
		defer mustClose(t, sess, base)
		err := sess.Cancel(context.Background(), execbackend.CancelMode("nope"))
		if !errors.Is(err, execbackend.ErrInvalidRequest) {
			t.Fatalf("Cancel(bad mode) = %v, want invalid_request", err)
		}
	})
	t.Run("graceful capability missing: graceful rejected, force still stops the run", func(t *testing.T) {
		be := New(Options{
			Clock:        clock,
			Capabilities: []execbackend.Capability{execbackend.CapabilityNonInteractive},
			Script:       RunIdle(time.Hour),
		})
		sess := startSession(t, be, validRequest(t))
		defer mustClose(t, sess, base)
		err := sess.Cancel(context.Background(), execbackend.CancelGraceful)
		if !errors.Is(err, execbackend.ErrCapabilityUnavailable) {
			t.Fatalf("Cancel(graceful) = %v, want capability_unavailable", err)
		}
		if concrete := asSession(t, sess); concrete.isDone() {
			t.Fatalf("an unsupported graceful cancel must not end the session")
		}
		// Force cancel is never capability-gated: a run must always be stoppable
		// (§27.2 item 6); the caller escalates to it when graceful is unavailable.
		if err := sess.Cancel(context.Background(), execbackend.CancelForce); err != nil {
			t.Fatalf("Cancel(force) without cancel_graceful = %v, want nil", err)
		}
		result, err := sess.Wait(context.Background())
		if err != nil {
			t.Fatalf("Wait after force cancel: %v", err)
		}
		if result.Reason != execbackend.ExitReasonCancelled {
			t.Fatalf("Wait reason = %q, want %q", result.Reason, execbackend.ExitReasonCancelled)
		}
	})
}

func TestCloseIdempotentAndWaitRepeatable(t *testing.T) {
	base := runtime.NumGoroutine()
	be := New(Options{Clock: NewManualClock(testStart), Script: RunIdle(time.Hour)})
	sess := startSession(t, be, validRequest(t))

	if _, ok := recv(t, sess, obsTimeout); !ok {
		t.Fatalf("observations closed before Close")
	}

	// ctx 取消只影响本次等待。
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := sess.Wait(cancelledCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait with cancelled ctx = %v, want context.Canceled", err)
	}

	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	checkNoLeak(t, base)

	// Close 后 Observations 已关闭（缓冲里可能留有已投递的终结观察，读尽即关闭）。
	if tail := collect(t, sess, obsTimeout); len(tail) > 0 {
		assertAllValid(t, tail)
		last := tail[len(tail)-1]
		if last.Kind != execbackend.ObservationExited {
			t.Fatalf("tail = %s, want trailing exited", describe(tail))
		}
		if got := exitResultOf(t, last); got.Reason != execbackend.ExitReasonCancelled {
			t.Fatalf("early Closed terminal reason = %s, want cancelled", got.Reason)
		}
	}
	if _, ok := <-sess.Observations(); ok {
		t.Fatalf("Observations must be closed after Close")
	}

	first, err := sess.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait after Close: %v", err)
	}
	second, err := sess.Wait(context.Background())
	if err != nil {
		t.Fatalf("second Wait after Close: %v", err)
	}
	if first.Reason != second.Reason || first.Retryable != second.Retryable ||
		first.Usage.Quality != second.Usage.Quality {
		t.Fatalf("Wait results differ: %+v vs %+v", first, second)
	}
	if first.Reason != execbackend.ExitReasonCancelled {
		t.Fatalf("early Closed session reason = %s, want cancelled", first.Reason)
	}

	if err := sess.Approve(context.Background(), execbackend.ApprovalDecision{ProviderRef: "tool-test-1", Approved: true}); !errors.Is(err, execbackend.ErrSessionClosed) {
		t.Fatalf("Approve after Close = %v, want ErrSessionClosed", err)
	}
	if err := sess.Cancel(context.Background(), execbackend.CancelForce); !errors.Is(err, execbackend.ErrSessionClosed) {
		t.Fatalf("Cancel after Close = %v, want ErrSessionClosed", err)
	}
}

func TestCloseIsConcurrencySafe(t *testing.T) {
	base := runtime.NumGoroutine()
	be := New(Options{Clock: NewManualClock(testStart), Script: RunIdle(time.Hour)})
	sess := startSession(t, be, validRequest(t))

	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = sess.Close()
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Close[%d] = %v, want nil", i, err)
		}
	}
	checkNoLeak(t, base)
	result, err := sess.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if result.Reason != execbackend.ExitReasonCancelled {
		t.Fatalf("reason = %s, want cancelled", result.Reason)
	}
}

func TestSessionMethodsAreConcurrencySafe(t *testing.T) {
	base := runtime.NumGoroutine()
	be := New(Options{
		Clock: NewManualClock(testStart),
		Script: []Step{
			Emit(execbackend.ObservationToolRequested, "appr-1", map[string]any{"tool": "command"}),
			AwaitApproval("appr-1", map[string]any{"cmd": "echo hi"}),
			Exit(0, execbackend.ExitReasonCompleted, UnknownUsage()),
		},
	})
	sess := startSession(t, be, validRequest(t))

	// 先读到 approval_required：此时审批已登记，并发的 Approve 才必然命中。
	for {
		o, ok := recv(t, sess, obsTimeout)
		if !ok {
			t.Fatalf("observations closed before approval")
		}
		if o.Kind == execbackend.ObservationApprovalRequired {
			break
		}
	}

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), obsTimeout)
			defer cancel()
			if _, err := sess.Wait(ctx); err != nil {
				t.Errorf("concurrent Wait: %v", err)
			}
		}()
	}
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// 首个裁决生效；并发的其余裁决要么幂等 no-op（nil），要么在脚本已经
			// 跑完/会话终结后撞上 session_closed——都不得是别的错误。
			err := sess.Approve(context.Background(), execbackend.ApprovalDecision{
				ApprovalID:  "appr-1",
				ProviderRef: "appr-1",
				Approved:    true,
			})
			if err != nil && !errors.Is(err, execbackend.ErrSessionClosed) {
				t.Errorf("concurrent Approve: %v", err)
			}
		}()
	}
	wg.Wait()

	result, err := sess.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if result.Reason != execbackend.ExitReasonCompleted {
		t.Fatalf("reason = %s, want completed", result.Reason)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	checkNoLeak(t, base)
}

// ---------------------------------------------------------------------------
// Prepare / Start 前置条件
// ---------------------------------------------------------------------------

func TestPrepareRejectsMissingCapability(t *testing.T) {
	t.Run("missing capability", func(t *testing.T) {
		be := New(Options{Clock: NewManualClock(testStart)})
		req := validRequest(t)
		req.Required = []execbackend.Capability{execbackend.CapabilityPTY, execbackend.CapabilityNonInteractive, execbackend.CapabilityMCP}
		_, err := be.Prepare(context.Background(), req)
		if !errors.Is(err, execbackend.ErrCapabilityUnavailable) {
			t.Fatalf("Prepare = %v, want capability_unavailable", err)
		}
		var domain *execbackend.Error
		if !errors.As(err, &domain) {
			t.Fatalf("error type = %T, want *execbackend.Error", err)
		}
		wantMissing := []execbackend.Capability{execbackend.CapabilityPTY, execbackend.CapabilityMCP}
		if len(domain.Missing) != len(wantMissing) {
			t.Fatalf("missing = %v, want %v", domain.Missing, wantMissing)
		}
		for i := range wantMissing {
			if domain.Missing[i] != wantMissing[i] {
				t.Fatalf("missing = %v, want %v", domain.Missing, wantMissing)
			}
		}
	})
	t.Run("invalid request", func(t *testing.T) {
		be := New(Options{Clock: NewManualClock(testStart)})
		req := validRequest(t)
		req.WorkDir = "relative/dir"
		_, err := be.Prepare(context.Background(), req)
		if !errors.Is(err, execbackend.ErrInvalidRequest) {
			t.Fatalf("Prepare = %v, want invalid_request", err)
		}
		var domain *execbackend.Error
		if !errors.As(err, &domain) || domain.Field != "work_dir" {
			t.Fatalf("error = %v, want field=work_dir", err)
		}
	})
	t.Run("prepared request is frozen", func(t *testing.T) {
		be := New(Options{Clock: NewManualClock(testStart), Script: RunIdle(time.Hour)})
		req := validRequest(t)
		req.Process.Env = map[string]string{"KEEP": "1"}
		req.Required = []execbackend.Capability{execbackend.CapabilityNonInteractive}
		prepared, err := be.Prepare(context.Background(), req)
		if err != nil {
			t.Fatalf("Prepare: %v", err)
		}
		// 改调用方的原请求（含 map/slice）不得影响已固化的副本，也不得被 fake 改坏。
		req.Process.Env["KEEP"] = "tampered"
		req.Process.Env["EXTRA"] = "x"
		req.Required[0] = execbackend.CapabilityMCP
		req.Input.Prompt = "tampered"

		if prepared.Request.Process.Env["KEEP"] != "1" || len(prepared.Request.Process.Env) != 1 {
			t.Fatalf("prepared env = %v, want frozen {KEEP:1}", prepared.Request.Process.Env)
		}
		if len(prepared.Request.Required) != 1 || prepared.Request.Required[0] != execbackend.CapabilityNonInteractive {
			t.Fatalf("prepared required = %v, want [non_interactive]", prepared.Request.Required)
		}
		if prepared.Request.Input.Prompt != "run the tool sequence" {
			t.Fatalf("prepared prompt = %q", prepared.Request.Input.Prompt)
		}
		if req.Process.Env["KEEP"] != "tampered" {
			t.Fatalf("fake modified the caller's request")
		}
	})
}

func TestStartRejectsUnpreparedPreparedStart(t *testing.T) {
	be := New(Options{Clock: NewManualClock(testStart)})

	if _, err := be.Start(context.Background(), execbackend.PreparedStart{}); !errors.Is(err, execbackend.ErrInvalidRequest) {
		t.Fatalf("Start(zero PreparedStart) = %v, want invalid_request", err)
	}
	handBuilt := execbackend.PreparedStart{
		Request: validRequest(t),
		Report:  execbackend.CapabilityReport{Backend: "claude-code", Source: "docs"},
	}
	if _, err := be.Start(context.Background(), handBuilt); !errors.Is(err, execbackend.ErrInvalidRequest) {
		t.Fatalf("Start(foreign report) = %v, want invalid_request", err)
	}
	noEvidence := execbackend.PreparedStart{
		Request: validRequest(t),
		Report:  execbackend.CapabilityReport{Backend: be.Name(), Source: "docs"},
	}
	noEvidence.Request.Required = []execbackend.Capability{execbackend.CapabilityNonInteractive}
	if _, err := be.Start(context.Background(), noEvidence); !errors.Is(err, execbackend.ErrCapabilityUnavailable) {
		t.Fatalf("Start(report without evidence) = %v, want capability_unavailable", err)
	}
	checkNoLeak(t, runtime.NumGoroutine())
}

func TestCapabilitiesReportIsReadOnly(t *testing.T) {
	base := runtime.NumGoroutine()
	clock := NewManualClock(testStart)
	be := New(Options{Clock: clock, Name: "fake-unit", Script: RunIdle(time.Hour)})

	report, err := be.Capabilities(context.Background())
	if err != nil {
		t.Fatalf("Capabilities: %v", err)
	}
	if report.Backend != "fake-unit" || report.Source != fakeSource {
		t.Fatalf("report = %+v, want backend=fake-unit source=%s", report, fakeSource)
	}
	if report.ExecutablePath != "" {
		t.Fatalf("fake must not claim an executable path, got %q", report.ExecutablePath)
	}
	if report.ProbedAt != testStart {
		t.Fatalf("ProbedAt = %s, want %s", report.ProbedAt, testStart)
	}
	if !report.Has(execbackend.CapabilityApprovalHook) || !report.Has(execbackend.CapabilityCancelGraceful) {
		t.Fatalf("default capabilities missing approval/cancel: %v", report.Evidence)
	}
	for _, unsupported := range []execbackend.Capability{
		execbackend.CapabilitySandbox,
		execbackend.CapabilityPTY,
		execbackend.CapabilityInject,
		execbackend.CapabilityMCP,
		execbackend.CapabilityResumeCheckpoint,
	} {
		if report.Has(unsupported) {
			t.Fatalf("fake must not claim %s", unsupported)
		}
	}

	// 改返回值不得影响后端（每次都是新副本）。
	report.Evidence[execbackend.CapabilitySandbox] = "forged"
	report.Source = ""
	again, err := be.Capabilities(context.Background())
	if err != nil {
		t.Fatalf("Capabilities: %v", err)
	}
	if again.Has(execbackend.CapabilitySandbox) || again.Source != fakeSource {
		t.Fatalf("Capabilities leaked mutable state: %+v", again)
	}
	checkNoLeak(t, base)
}

func TestAwaitApprovalWithoutCapabilityIsScriptError(t *testing.T) {
	base := runtime.NumGoroutine()
	be := New(Options{
		Clock:        NewManualClock(testStart),
		Capabilities: []execbackend.Capability{execbackend.CapabilityNonInteractive},
		Script:       RunSlowApproval("appr-1"),
	})
	sess := startSession(t, be, validRequest(t))
	defer mustClose(t, sess, base)

	all := collect(t, sess, obsTimeout)
	assertAllValid(t, all)
	if len(all) == 0 || all[0].Kind != execbackend.ObservationProcessStarted {
		t.Fatalf("observations = %s", describe(all))
	}
	last := all[len(all)-1]
	if last.Kind != execbackend.ObservationExited {
		t.Fatalf("last observation = %s, want exited", last.Kind)
	}
	if result := exitResultOf(t, last); result.Reason != execbackend.ExitReasonProtocolError {
		t.Fatalf("reason = %s, want protocol_error (script used undeclared capability)", result.Reason)
	}
}

// ---------------------------------------------------------------------------
// 夹具与真实时钟
// ---------------------------------------------------------------------------

func TestFixtures(t *testing.T) {
	t.Run("run-tool-sequence", func(t *testing.T) {
		base := runtime.NumGoroutine()
		be := New(Options{Clock: NewManualClock(testStart), Script: RunToolSequence()})
		sess := startSession(t, be, validRequest(t))
		defer mustClose(t, sess, base)

		if got := refsOf(nil); len(got) != 0 {
			t.Fatalf("unexpected %v", got)
		}
		if tools := ToolSequenceTools(); len(tools) != 5 || tools[0] != "read" || tools[4] != "test" {
			t.Fatalf("ToolSequenceTools() = %v, want [read search write command test]", tools)
		}
		if ref := RunToolSequenceProviderRef("read"); ref != "tool-read-1" {
			t.Fatalf("provider ref = %q", ref)
		}
		if len(RunIdle(0)) == 0 || len(RunOutputFlood(0)) == 0 || len(RunSlowApproval("x")) == 0 {
			t.Fatalf("fixtures must not be empty")
		}
	})
	t.Run("run-output-flood", func(t *testing.T) {
		base := runtime.NumGoroutine()
		be := New(Options{Clock: NewManualClock(testStart), Script: RunOutputFlood(16)})
		sess := startSession(t, be, validRequest(t))
		defer mustClose(t, sess, base)

		all := collect(t, sess, obsTimeout)
		assertAllValid(t, all)
		// process_started + 起头 output + 16 条洪峰 + usage + exited
		if len(all) != 20 {
			t.Fatalf("observations = %d, want 20 (%s)", len(all), describe(all))
		}
		outputs := 0
		for _, o := range all {
			if o.Kind == execbackend.ObservationOutput {
				outputs++
				if len(o.Payload) > 1024 {
					t.Fatalf("flood payload = %d bytes, want <= 1024", len(o.Payload))
				}
			}
		}
		if outputs != 17 {
			t.Fatalf("output frames = %d, want 17", outputs)
		}
	})
}

func TestObservationsUseRealClockByDefault(t *testing.T) {
	base := runtime.NumGoroutine()
	be := New(Options{
		Script: []Step{
			Emit(execbackend.ObservationOutput, "msg-1", map[string]any{"stream": "stdout", "text": "a"}),
			Sleep(5 * time.Millisecond),
			Emit(execbackend.ObservationOutput, "msg-2", map[string]any{"stream": "stdout", "text": "b"}),
			Exit(0, execbackend.ExitReasonCompleted, UnknownUsage()),
		},
	})
	before := time.Now()
	sess := startSession(t, be, validRequest(t))
	defer mustClose(t, sess, base)

	all := collect(t, sess, obsTimeout)
	assertAllValid(t, all)
	for i := 1; i < len(all); i++ {
		if all[i].ObservedAt.Before(all[i-1].ObservedAt) {
			t.Fatalf("ObservedAt went backwards at frame %d: %s < %s", i, all[i].ObservedAt, all[i-1].ObservedAt)
		}
	}
	if all[0].ObservedAt.Before(before) {
		t.Fatalf("procedure started before the test: %s < %s", all[0].ObservedAt, before)
	}
}
