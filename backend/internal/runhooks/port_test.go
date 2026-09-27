package runhooks

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/execbackend"
	backendhooks "github.com/codeflow/backend/internal/hooks"
	"github.com/codeflow/backend/internal/policy"
	"github.com/codeflow/backend/internal/policy/policytesting"
	"github.com/codeflow/backend/internal/run"
)

// 本文件是 T1.07.b 第 1 组在服务端适配器侧的测试（§15 T1.07、§28 T1.07.b）：
// 用真实的 hooks.NewHookManager()（SetHookManager 注入、Cleanup 恢复）与真实注册的
// hook handler 验证：
//   - PreToolUse handler 报错 → BeforeTool 拒绝；
//   - 20 个 goroutine 并发同一 event_id 只触发 1 次、全部拿到同一结论；
//   - 不同 event_id 各触发一次；
//   - allowlist 显式不含该类型时 Trigger 静默放行（所以 main.go 必须用
//     DefaultAllowedHooks，且它必须含 4 个新类型）；
//   - payload 不合法（缺 AttemptID、RunStart 状态不是 starting）→ before 拒绝且
//     handler 0 次；
//   - PostToolUse handler 失败 → AfterTool 返回错误（由 execbackend 层转成警告）、
//     handler 只调用一次。

// hookTestEnv 安装一个真实的 hook 管理器，并允许 hook 执行所需的 policy 操作。
func hookTestEnv(t *testing.T) *backendhooks.HookManager {
	t.Helper()
	policytesting.AllowForTest(t, policy.OperationHookExecute)
	previous := backendhooks.GetHookManager()
	mgr := backendhooks.NewHookManager()
	backendhooks.SetHookManager(mgr)
	t.Cleanup(func() { backendhooks.SetHookManager(previous) })
	return mgr
}

func registerHook(t *testing.T, mgr *backendhooks.HookManager, name string, hook backendhooks.HookType, handler backendhooks.HookFunc) {
	t.Helper()
	if err := mgr.Register(backendhooks.HookConfig{
		Name:    name,
		Type:    hook,
		Enabled: true,
		Timeout: 5 * time.Second,
	}, handler); err != nil {
		t.Fatalf("register %s hook %q: %v", hook, name, err)
	}
}

func testRunRef() execbackend.RunRef {
	return execbackend.RunRef{
		ProjectID:       "project-1",
		RunID:           "run-1",
		AttemptID:       "attempt-1",
		AgentRevisionID: "revision-1",
	}
}

func testToolRequest(t *testing.T, eventID string) execbackend.ToolCallRequest {
	t.Helper()
	req, err := execbackend.NewToolCallRequest(testRunRef(), "claude_code", eventID, "toolu_1", "read_file", json.RawMessage(`{"path":"src/x.go"}`))
	if err != nil {
		t.Fatalf("NewToolCallRequest: %v", err)
	}
	return req
}

func testRunRequest(t *testing.T, eventID, status, previous string) execbackend.RunLifecycleRequest {
	t.Helper()
	req, err := execbackend.NewRunLifecycleRequest(testRunRef(), "claude_code", eventID, status, previous)
	if err != nil {
		t.Fatalf("NewRunLifecycleRequest: %v", err)
	}
	return req
}

// TestHookPointNamesMatchHooksTypes 钉住 execbackend 复刻的四个 hook 点名字与
// hooks 包的 HookType 取值一致（execbackend 不能 import hooks）。
func TestHookPointNamesMatchHooksTypes(t *testing.T) {
	pairs := []struct {
		point string
		hook  backendhooks.HookType
	}{
		{execbackend.HookPointPreToolUse, backendhooks.HookPreToolUse},
		{execbackend.HookPointPostToolUse, backendhooks.HookPostToolUse},
		{execbackend.HookPointRunStart, backendhooks.HookRunStart},
		{execbackend.HookPointRunFinish, backendhooks.HookRunFinish},
	}
	for _, pair := range pairs {
		if pair.point != string(pair.hook) {
			t.Errorf("execbackend hook point %q != hooks.HookType %q", pair.point, pair.hook)
		}
	}
	for _, status := range []struct {
		port string
		hook backendhooks.ToolHookResultStatus
	}{
		{execbackend.ToolResultStatusOK, backendhooks.ToolHookResultOK},
		{execbackend.ToolResultStatusError, backendhooks.ToolHookResultError},
		{execbackend.ToolResultStatusDenied, backendhooks.ToolHookResultDenied},
	} {
		if status.port != string(status.hook) {
			t.Errorf("execbackend result status %q != hooks status %q", status.port, status.hook)
		}
	}
}

// TestBeforeToolRejectsWhenHandlerFails PreToolUse handler 报错 = 拒绝。
func TestBeforeToolRejectsWhenHandlerFails(t *testing.T) {
	mgr := hookTestEnv(t)
	denied := errors.New("before-tool hook says no")
	registerHook(t, mgr, "deny-tool", backendhooks.HookPreToolUse, func(context.Context, backendhooks.HookPayload) (backendhooks.HookResult, error) {
		return nil, denied
	})

	port := New()
	err := port.BeforeTool(context.Background(), testToolRequest(t, "event-1"))
	if err == nil {
		t.Fatal("BeforeTool accepted a failing PreToolUse handler")
	}
	if !errors.Is(err, denied) {
		t.Errorf("error %v does not unwrap to the handler failure", err)
	}
}

// TestToolHookCalledOnce 是 T1.07.c 点名测试的等价物：20 个 goroutine 并发同一个
// event_id，只触发一次，且全部拿到同一结论。
func TestToolHookCalledOnce(t *testing.T) {
	mgr := hookTestEnv(t)
	var calls int32
	release := make(chan struct{})
	registerHook(t, mgr, "slow-deny", backendhooks.HookPreToolUse, func(context.Context, backendhooks.HookPayload) (backendhooks.HookResult, error) {
		atomic.AddInt32(&calls, 1)
		<-release // 让第一个触发停留，保证并发者都在飞行窗口内到达
		return nil, errors.New("denied once")
	})

	port := New()
	req := testToolRequest(t, "event-1")

	const goroutines = 20
	var wg sync.WaitGroup
	errs := make([]error, goroutines)
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(index int) {
			defer wg.Done()
			errs[index] = port.BeforeTool(context.Background(), req)
		}(i)
	}

	// 等到确实有一个触发进入 handler，再放行它，让其余 goroutine 复用结论。
	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(&calls) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("no goroutine reached the hook handler")
		}
		time.Sleep(time.Millisecond)
	}
	close(release)
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("handler ran %d times for one event_id across %d goroutines, want exactly 1", got, goroutines)
	}
	first := errs[0]
	if first == nil {
		t.Fatal("concurrent calls all succeeded, want the shared denial")
	}
	for i, err := range errs {
		if err == nil || err.Error() != first.Error() {
			t.Errorf("goroutine %d got %v, want the same conclusion as goroutine 0 (%v)", i, err, first)
		}
	}

	// 重复调用（串行）同样复用结论，不再触发。
	if err := port.BeforeTool(context.Background(), req); err == nil || err.Error() != first.Error() {
		t.Errorf("repeated call = %v, want the remembered conclusion", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("handler ran %d times after a repeated call, want 1", got)
	}
}

// TestDifferentEventIDsEachTriggerOnce 不同 event_id 各触发一次（记忆不串键）。
func TestDifferentEventIDsEachTriggerOnce(t *testing.T) {
	mgr := hookTestEnv(t)
	var calls int32
	registerHook(t, mgr, "count", backendhooks.HookPreToolUse, func(context.Context, backendhooks.HookPayload) (backendhooks.HookResult, error) {
		atomic.AddInt32(&calls, 1)
		return nil, nil
	})

	port := New()
	for _, eventID := range []string{"event-1", "event-2", "event-3", "event-2", "event-1"} {
		if err := port.BeforeTool(context.Background(), testToolRequest(t, eventID)); err != nil {
			t.Fatalf("BeforeTool(%s): %v", eventID, err)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("handler ran %d times for 3 distinct events, want 3", got)
	}

	// 淘汰：容量 2 时最旧的键被遗忘，再触发一次是允许的（记忆有界）。
	bounded := &Port{MaxEntries: 2}
	if err := bounded.BeforeTool(context.Background(), testToolRequest(t, "event-a")); err != nil {
		t.Fatalf("bounded event-a: %v", err)
	}
	if err := bounded.BeforeTool(context.Background(), testToolRequest(t, "event-b")); err != nil {
		t.Fatalf("bounded event-b: %v", err)
	}
	if err := bounded.BeforeTool(context.Background(), testToolRequest(t, "event-c")); err != nil {
		t.Fatalf("bounded event-c: %v", err)
	}
	if err := bounded.BeforeTool(context.Background(), testToolRequest(t, "event-a")); err != nil {
		t.Fatalf("bounded event-a (after eviction): %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 7 {
		t.Fatalf("handler ran %d times, want 7 (3 distinct + 3 bounded + 1 re-run after eviction)", got)
	}
}

// TestAllowlistExcludingHookSilentlyPasses 是 §26.30 的 BOOT 须知：allowlist 不含
// 该 hook 类型时 Trigger 返回 (payload, nil)——reject 会静默变成放行。所以 main.go
// 必须用 DefaultAllowedHooks()，且它必须包含四个新类型。
func TestAllowlistExcludingHookSilentlyPasses(t *testing.T) {
	mgr := hookTestEnv(t)
	var calls int32
	registerHook(t, mgr, "deny-tool", backendhooks.HookPreToolUse, func(context.Context, backendhooks.HookPayload) (backendhooks.HookResult, error) {
		atomic.AddInt32(&calls, 1)
		return nil, errors.New("denied")
	})

	enabled := true
	mgr.SetControls(backendhooks.HookRuntimeControls{
		Enabled:      &enabled,
		AllowedHooks: []backendhooks.HookType{backendhooks.HookBeforeWrite},
	})
	port := New()
	if err := port.BeforeTool(context.Background(), testToolRequest(t, "event-1")); err != nil {
		t.Fatalf("a hook outside the allowlist returned an error (%v); the contract is a silent pass, which is exactly why the allowlist must list it", err)
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("handler ran %d times outside the allowlist, want 0 (the trigger is skipped, not the decision)", got)
	}

	// DefaultAllowedHooks 是派生出来的、必须含四个新类型的集合。
	allowed := map[backendhooks.HookType]bool{}
	for _, hook := range backendhooks.DefaultAllowedHooks() {
		allowed[hook] = true
	}
	for _, hook := range []backendhooks.HookType{
		backendhooks.HookPreToolUse,
		backendhooks.HookPostToolUse,
		backendhooks.HookRunStart,
		backendhooks.HookRunFinish,
		backendhooks.HookBeforeWrite,
	} {
		if !allowed[hook] {
			t.Errorf("DefaultAllowedHooks() is missing %q", hook)
		}
	}
	if got := len(backendhooks.DefaultAllowedHooks()); got != 17 {
		t.Errorf("DefaultAllowedHooks() has %d entries, want 17 (all retained hook-typed rows)", got)
	}

	// 用 DefaultAllowedHooks 之后，同一个拒绝 hook 真的会被执行。
	mgr.SetControls(backendhooks.HookRuntimeControls{Enabled: &enabled, AllowedHooks: backendhooks.DefaultAllowedHooks()})
	if err := port.BeforeTool(context.Background(), testToolRequest(t, "event-2")); err == nil {
		t.Fatal("BeforeTool passed with the default allowlist, want the handler denial")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("handler ran %d times with the default allowlist, want 1", got)
	}
}

// TestBeforeToolRejectsInvalidPayload 身份缺 attempt_id 时 payload 不合法：拒绝，
// handler 一次都不跑。
func TestBeforeToolRejectsInvalidPayload(t *testing.T) {
	mgr := hookTestEnv(t)
	var calls int32
	registerHook(t, mgr, "count", backendhooks.HookPreToolUse, func(context.Context, backendhooks.HookPayload) (backendhooks.HookResult, error) {
		atomic.AddInt32(&calls, 1)
		return nil, nil
	})

	port := New()
	broken := testToolRequest(t, "event-1")
	broken.Run.AttemptID = ""
	err := port.BeforeTool(context.Background(), broken)
	if err == nil {
		t.Fatal("BeforeTool accepted a payload without attempt_id")
	}
	var payloadErr *backendhooks.PayloadError
	if !errors.As(err, &payloadErr) {
		t.Fatalf("error %v is not a *PayloadError", err)
	}
	if payloadErr.Field != "Identity.attempt_id" {
		t.Errorf("payload error field = %q, want Identity.attempt_id", payloadErr.Field)
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("handler ran %d times for an invalid payload, want 0", got)
	}

	// EventID 为空同样被拒绝（不做记忆，空键绝不当真键）。
	noEvent := testToolRequest(t, "event-1")
	noEvent.EventID = ""
	if err := port.BeforeTool(context.Background(), noEvent); err == nil {
		t.Fatal("BeforeTool accepted an empty event_id")
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("handler ran %d times for an empty event_id, want 0", got)
	}
}

// TestAfterToolReturnsHandlerFailure PostToolUse 失败由 AfterTool 返回错误，
// execbackend 层再把它转成警告；handler 只调用一次（不重跑工具，也不重跑观察者）。
func TestAfterToolReturnsHandlerFailure(t *testing.T) {
	mgr := hookTestEnv(t)
	var calls int32
	postErr := errors.New("post hook exploded")
	registerHook(t, mgr, "post-fail", backendhooks.HookPostToolUse, func(context.Context, backendhooks.HookPayload) (backendhooks.HookResult, error) {
		atomic.AddInt32(&calls, 1)
		return nil, postErr
	})

	port := New()
	req := testToolRequest(t, "event-1")
	exit := 2
	result := execbackend.ToolCallResult{Status: execbackend.ToolResultStatusError, ExitCode: &exit, OutputRef: "spool/9", Truncated: true}
	if err := port.AfterTool(context.Background(), req, result); !errors.Is(err, postErr) {
		t.Fatalf("AfterTool error = %v, want the handler failure", err)
	}
	// 重复调用复用同一结论，不产生第二个触发记录。
	if err := port.AfterTool(context.Background(), req, result); !errors.Is(err, postErr) {
		t.Fatalf("repeated AfterTool error = %v, want the remembered handler failure", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("handler ran %d times, want exactly 1 (one event, one trigger record)", got)
	}

	// execbackend 层看到的是警告，不是调用方错误。
	warned := 0
	execbackend.ReportToolResult(context.Background(), port, func(context.Context, execbackend.HookWarning) { warned++ }, req, result)
	if warned != 1 {
		t.Fatalf("warnings = %d, want 1", warned)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("handler ran %d times after a report, want 1", got)
	}
}

// TestAfterToolHandlerSeesSanitizedSummary post 观察者只看到净化后的摘要。
func TestAfterToolHandlerSeesSanitizedSummary(t *testing.T) {
	mgr := hookTestEnv(t)
	var seen backendhooks.ToolHookPayload
	var hasSeen bool
	registerHook(t, mgr, "capture", backendhooks.HookPostToolUse, func(_ context.Context, payload backendhooks.HookPayload) (backendhooks.HookResult, error) {
		seen, hasSeen = payload.(backendhooks.ToolHookPayload), true
		return nil, nil
	})

	port := New()
	exit := 0
	req := testToolRequest(t, "event-1")
	if err := port.AfterTool(context.Background(), req, execbackend.ToolCallResult{Status: execbackend.ToolResultStatusOK, ExitCode: &exit, OutputRef: "spool/3"}); err != nil {
		t.Fatalf("AfterTool: %v", err)
	}
	if !hasSeen {
		t.Fatal("the handler was not called")
	}
	if seen.Result == nil || seen.Result.Status != backendhooks.ToolHookResultOK || seen.Result.OutputRef != "spool/3" {
		t.Fatalf("handler saw result %+v, want the sanitized summary", seen.Result)
	}
	if len(seen.Arguments) != 0 {
		t.Errorf("post payload carried %d argument bytes, want none (post observes the result)", len(seen.Arguments))
	}
	if seen.Identity.Actor.Type != run.ActorTypeAgent || seen.Identity.Actor.ID != "revision-1" || seen.Identity.Actor.Source != "claude_code" {
		t.Errorf("tool identity actor = %+v, want agent/revision-1/claude_code", seen.Identity.Actor)
	}
	if seen.RequestFingerprint != req.Fingerprint || seen.ToolCallID != "toolu_1" || seen.EventID != "event-1" {
		t.Errorf("payload = %+v, want the request fingerprint, tool call id and event id", seen)
	}
}

// TestRunLifecycleHooks run-start/run-finish 的拒绝与警告语义、身份构造。
func TestRunLifecycleHooks(t *testing.T) {
	mgr := hookTestEnv(t)
	var startCalls, finishCalls int32
	var startPayload, finishPayload backendhooks.RunHookPayload
	registerHook(t, mgr, "run-start", backendhooks.HookRunStart, func(_ context.Context, payload backendhooks.HookPayload) (backendhooks.HookResult, error) {
		atomic.AddInt32(&startCalls, 1)
		startPayload = payload.(backendhooks.RunHookPayload)
		return nil, errors.New("no start")
	})
	registerHook(t, mgr, "run-finish", backendhooks.HookRunFinish, func(_ context.Context, payload backendhooks.HookPayload) (backendhooks.HookResult, error) {
		atomic.AddInt32(&finishCalls, 1)
		finishPayload = payload.(backendhooks.RunHookPayload)
		return nil, errors.New("finish warning")
	})

	port := New()
	startErr := port.BeforeRunStart(context.Background(), testRunRequest(t, "claimed-1", string(run.RunStatusStarting), string(run.RunStatusQueued)))
	if startErr == nil {
		t.Fatal("BeforeRunStart accepted a failing run-start hook")
	}
	if got := atomic.LoadInt32(&startCalls); got != 1 {
		t.Fatalf("run-start handler ran %d times, want 1", got)
	}
	if startPayload.Status != run.RunStatusStarting || startPayload.PreviousStatus != run.RunStatusQueued {
		t.Errorf("run-start payload = %+v, want starting/queued", startPayload)
	}
	if startPayload.Identity.Actor.Type != run.ActorTypeSystem || startPayload.Identity.Actor.ID != systemActorID {
		t.Errorf("run-start actor = %+v, want the system actor %q", startPayload.Identity.Actor, systemActorID)
	}
	if startPayload.Identity.AttemptID == nil || *startPayload.Identity.AttemptID != "attempt-1" {
		t.Errorf("run-start identity = %+v, want the attempt id (the claim created it)", startPayload.Identity)
	}

	finishErr := port.AfterRunFinish(context.Background(), testRunRequest(t, "finished-1", string(run.RunStatusCompleted), string(run.RunStatusRunning)))
	if finishErr == nil {
		t.Fatal("AfterRunFinish swallowed the handler failure")
	}
	if got := atomic.LoadInt32(&finishCalls); got != 1 {
		t.Fatalf("run-finish handler ran %d times, want 1", got)
	}
	if finishPayload.Status != run.RunStatusCompleted || finishPayload.Identity.Actor.Type != run.ActorTypeSystem {
		t.Errorf("run-finish payload = %+v, want completed + the system actor", finishPayload)
	}

	// RunStart 的状态契约由 payload.Validate 强制：不是 starting 就拒绝，且不触发。
	badStatus := testRunRequest(t, "claimed-2", string(run.RunStatusRunning), string(run.RunStatusStarting))
	if err := port.BeforeRunStart(context.Background(), badStatus); err == nil {
		t.Fatal("BeforeRunStart accepted status=running (must be starting)")
	}
	if got := atomic.LoadInt32(&startCalls); got != 1 {
		t.Fatalf("run-start handler ran %d times after an invalid payload, want 1", got)
	}
}

// TestRunFinishWithoutAttemptFires 排队阶段取消的 Run 没有 attempt：RunFinish 仍然
// 触发，身份里的 attempt 是缺省（nil），而不是空字符串——空字符串会被
// payload.Validate 拒绝，这类 Run 的结束 hook 就永远不会触发。RunStart 没有
// attempt 就拒绝，handler 一次都不运行。
func TestRunFinishWithoutAttemptFires(t *testing.T) {
	mgr := hookTestEnv(t)
	var startCalls, finishCalls int32
	var finishPayload backendhooks.RunHookPayload
	registerHook(t, mgr, "run-start", backendhooks.HookRunStart, func(_ context.Context, payload backendhooks.HookPayload) (backendhooks.HookResult, error) {
		atomic.AddInt32(&startCalls, 1)
		return nil, nil
	})
	registerHook(t, mgr, "run-finish", backendhooks.HookRunFinish, func(_ context.Context, payload backendhooks.HookPayload) (backendhooks.HookResult, error) {
		atomic.AddInt32(&finishCalls, 1)
		finishPayload = payload.(backendhooks.RunHookPayload)
		return nil, nil
	})

	ref := testRunRef()
	ref.AttemptID = ""
	finish, err := execbackend.NewRunLifecycleRequest(ref, "claude_code", "cancelled-1", string(run.RunStatusCancelled), string(run.RunStatusQueued))
	if err != nil {
		t.Fatalf("NewRunLifecycleRequest for a Run cancelled in the queue: %v", err)
	}
	port := New()
	if err := port.AfterRunFinish(context.Background(), finish); err != nil {
		t.Fatalf("AfterRunFinish for a Run cancelled in the queue: %v", err)
	}
	if got := atomic.LoadInt32(&finishCalls); got != 1 {
		t.Fatalf("run-finish handler ran %d times, want 1", got)
	}
	if finishPayload.Identity.AttemptID != nil {
		t.Errorf("run-finish identity carries attempt %q, want it absent", *finishPayload.Identity.AttemptID)
	}
	if finishPayload.Identity.RunID == nil || *finishPayload.Identity.RunID != "run-1" || finishPayload.Status != run.RunStatusCancelled {
		t.Errorf("run-finish payload = %+v, want run-1 cancelled", finishPayload)
	}

	start, err := execbackend.NewRunLifecycleRequest(ref, "claude_code", "claimed-1", string(run.RunStatusStarting), string(run.RunStatusQueued))
	if err != nil {
		t.Fatalf("NewRunLifecycleRequest: %v", err)
	}
	if err := port.BeforeRunStart(context.Background(), start); err == nil {
		t.Fatal("BeforeRunStart accepted a claim without an attempt")
	}
	if got := atomic.LoadInt32(&startCalls); got != 0 {
		t.Fatalf("run-start handler ran %d times without an attempt, want 0", got)
	}
}

// TestHandlerPanicDoesNotBlockWaiters handler panic 不是结论：单飞的等待者不能
// 永久阻塞，也不能把 panic 当成放行（nil 结论）。panic 被记成错误（fail-closed）
// 之后原样抛出，等待者拿到同一个错误。
func TestHandlerPanicDoesNotBlockWaiters(t *testing.T) {
	mgr := hookTestEnv(t)
	var calls int32
	entered := make(chan struct{})
	release := make(chan struct{})
	registerHook(t, mgr, "boom", backendhooks.HookPreToolUse, func(context.Context, backendhooks.HookPayload) (backendhooks.HookResult, error) {
		if atomic.AddInt32(&calls, 1) == 1 {
			close(entered)
			<-release
			panic("boom")
		}
		return nil, nil
	})

	port := New()
	request := testToolRequest(t, "panic-event")
	first := make(chan any, 1)
	go func() {
		defer func() { first <- recover() }()
		_ = port.BeforeTool(context.Background(), request)
	}()
	<-entered

	second := make(chan error, 1)
	go func() { second <- port.BeforeTool(context.Background(), request) }()
	close(release)

	select {
	case recovered := <-first:
		if recovered == nil {
			t.Fatal("expected the handler panic to be re-raised")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the triggering goroutine never returned")
	}

	select {
	case err := <-second:
		if err == nil {
			t.Fatal("the waiting goroutine got nil from a panicked trigger (panic must never read as a pass)")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the waiting goroutine blocked forever after the handler panicked")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("handler ran %d times, want 1", got)
	}
}
