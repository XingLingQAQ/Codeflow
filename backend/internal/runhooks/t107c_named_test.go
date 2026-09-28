package runhooks

// 本文件是 T1.07.c 的三个点名测试（计划 §28 T1.07.c、§15 T1.07 验收）：
//
//	TestToolHookCalledOnce              一个事件只有一个触发记录（hook 与审计两侧）；
//	TestBeforeHookDenyPreventsProcess   before 拒绝 = 进程一次都不启动（工具与 RunStart）；
//	TestAfterHookFailureDoesNotReplayTool  after 失败只重跑 handler，绝不重跑工具。
//
// 与端口层等价物（TestToolHookCalledOncePortLevel / TestBeforeHookDenyPreventsProcessPortLevel
// / TestAfterHookFailureDoesNotReplayToolPortLevel）的区别：那些用假端口或假 execute 证明
// 端口语义，这里全部走真实组件——
//
//	execbackend.RunTool / AuthorizeRunStart
//	  → 真实 runhooks.Port（单飞记忆，生产实现）
//	  → 真实 hooks.NewHookManager()（生产 allowlist：DefaultAllowedHooks，同 main.go）
//	  → 真实审计服务（内存存储），并按 (hook_type, source_event_id) 分组核对
//	  → 假后端的 tool_requested / process_started 观察 + 真的进程（重新执行测试二进制）。
//
// event_id 由测试里的“服务端”按 (run, tool_call_id) 分配（t107cEventID）：CLI 不能伪造
// 领域事件（§27.4），所以同一工具调用的重复帧拿到同一个 event_id——这正是单飞记忆的输入。
//
// 边界（点名测试只证明它该证明的）：RunTool 不是“工具执行去重层”。同一个 event_id 的
// 重复帧会让 execute 再跑一次——执行去重属于 T1.04/T1.13 的“观察 → 事件映射”（同一
// ProviderRef 只有一个 tool 事件），本文件不对 execute 次数作任何承诺。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/audit"
	"github.com/codeflow/backend/internal/execbackend"
	backendfake "github.com/codeflow/backend/internal/execbackend/fake"
	backendhooks "github.com/codeflow/backend/internal/hooks"
)

// ---------------------------------------------------------------------------
// 测试环境：真实管理器（生产 allowlist）+ 真实审计服务
// ---------------------------------------------------------------------------

// t107cAuditEnv 安装真实的审计服务（内存存储）：hooks 管理器把每次触发写成一条审计
// 记录，读取方按 (hook_type, source_event_id) 分组核对“一个事件只有一个触发记录”。
func t107cAuditEnv(t *testing.T) *audit.MemoryStorage {
	t.Helper()
	storage := audit.NewMemoryStorage()
	audit.SetAuditService(audit.NewAuditService(storage))
	t.Cleanup(func() { audit.SetAuditService(nil) })
	return storage
}

// t107cAllowlist 按 main.go 的 configureHookRuntimeControls 装配运行时 allowlist：
// 直接用它用的 DefaultAllowedHooks()。这是生产 allowlist 真的放行这四个保留类型的证据
// ——allowlist 不含某个类型时 manager.Trigger 返回 (payload, nil)，reject 会静默变成
// 放行（§26.30），所以这里先钉住集合，再让测试跑在它下面。
func t107cAllowlist(t *testing.T, mgr *backendhooks.HookManager) {
	t.Helper()
	enabled := true
	allowed := backendhooks.DefaultAllowedHooks()
	mgr.SetControls(backendhooks.HookRuntimeControls{Enabled: &enabled, AllowedHooks: allowed})

	listed := map[backendhooks.HookType]bool{}
	for _, hook := range allowed {
		listed[hook] = true
	}
	for _, hook := range []backendhooks.HookType{
		backendhooks.HookPreToolUse,
		backendhooks.HookPostToolUse,
		backendhooks.HookRunStart,
		backendhooks.HookRunFinish,
	} {
		if !listed[hook] {
			t.Fatalf("DefaultAllowedHooks() is missing %q: the production allowlist would silently pass this hook", hook)
		}
	}
}

// t107cRegisterHook 注册一个 hook（可控制重试次数）。它与 port_test.go 的 registerHook
// 分开，因为重试是 TestAfterHookFailureDoesNotReplayTool 的被测事实。
func t107cRegisterHook(t *testing.T, mgr *backendhooks.HookManager, config backendhooks.HookConfig, handler backendhooks.HookFunc) {
	t.Helper()
	config.Enabled = true
	if config.Timeout == 0 {
		config.Timeout = 5 * time.Second
	}
	if err := mgr.Register(config, handler); err != nil {
		t.Fatalf("register %s hook %q: %v", config.Type, config.Name, err)
	}
}

// t107cWarnRecorder 记录 execbackend 的警告（"恰好一次"断言用）。
type t107cWarnRecorder struct {
	mu       sync.Mutex
	warnings []execbackend.HookWarning
}

func (r *t107cWarnRecorder) sink(_ context.Context, warning execbackend.HookWarning) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.warnings = append(r.warnings, warning)
}

func (r *t107cWarnRecorder) all() []execbackend.HookWarning {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]execbackend.HookWarning(nil), r.warnings...)
}

// ---------------------------------------------------------------------------
// 测试里的“服务端”：按 (run, tool_call_id) 分配事件 ID
// ---------------------------------------------------------------------------

// t107cEventID 分配事件 ID。服务端从权威资源分配 event ID、sequence 与 identity，
// CLI 不能伪造领域事件（§27.4）；所以同一 (run, tool_call_id) 的重复帧必须映射到同一个
// event_id，单飞记忆才可能把重复帧认出来。
type t107cEventID struct {
	mu     sync.Mutex
	byTool map[string]string
	next   int
}

func (s *t107cEventID) For(toolCallID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.byTool == nil {
		s.byTool = map[string]string{}
	}
	if eventID, ok := s.byTool[toolCallID]; ok {
		return eventID
	}
	s.next++
	eventID := fmt.Sprintf("t107c-evt-%02d", s.next)
	s.byTool[toolCallID] = eventID
	return eventID
}

// t107cToolRequest 构造一次工具调用请求（§27.1 的四个身份字段由 testRunRef 给出）。
func t107cToolRequest(t *testing.T, eventID, toolCallID, tool string) execbackend.ToolCallRequest {
	t.Helper()
	request, err := execbackend.NewToolCallRequest(testRunRef(), "claude_code", eventID, toolCallID, tool,
		json.RawMessage(`{"path":"src/x.go"}`))
	if err != nil {
		t.Fatalf("NewToolCallRequest(%s/%s): %v", eventID, toolCallID, err)
	}
	return request
}

// ---------------------------------------------------------------------------
// 真的进程：重新执行测试二进制（标准 helper-process 写法）
// ---------------------------------------------------------------------------

const (
	// t107cHelperEnv 是“这次执行是 helper 模式”的开关。
	t107cHelperEnv = "CODEFLOW_T107C_HELPER_PROCESS"
	// t107cHelperMarkerEnv 是标记文件路径：helper 往里面追加一行后退出。
	t107cHelperMarkerEnv = "CODEFLOW_T107C_MARKER_FILE"
	// t107cToolOutputRef 是工具结果的输出引用（只带引用，不带原文）。
	t107cToolOutputRef = "t107c/tool-output"
)

// TestT107cHelperProcess 不是真正的测试：它只在把测试二进制重新执行为“工具”时干活
// （标准 Go 做法，不依赖 sh/cmd.exe 的可移植性）。
//
// 常规运行下环境变量为空，函数直接返回（PASS，不是 SKIP），所以常规构建里 SKIP 为 0。
func TestT107cHelperProcess(t *testing.T) {
	if os.Getenv(t107cHelperEnv) != "1" {
		return
	}
	marker := os.Getenv(t107cHelperMarkerEnv)
	if strings.TrimSpace(marker) == "" {
		t.Fatal("helper process: marker file is not configured")
	}
	file, err := os.OpenFile(marker, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("helper process: open marker: %v", err)
	}
	if _, err := file.WriteString("started\n"); err != nil {
		_ = file.Close()
		t.Fatalf("helper process: append marker: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("helper process: close marker: %v", err)
	}
	os.Exit(0)
}

// t107cToolProcess 返回一次“工具执行”：重新启动测试二进制，helper 在标记文件里追加
// 一行后退出。标记文件的行数就是工具进程真实启动的次数——不断言 sleep 顺序。
func t107cToolProcess(marker string) func(context.Context) (execbackend.ToolCallResult, error) {
	return func(ctx context.Context) (execbackend.ToolCallResult, error) {
		executable, err := filepath.Abs(os.Args[0])
		if err != nil {
			return execbackend.ToolCallResult{}, fmt.Errorf("t107c tool process: resolve the test binary: %w", err)
		}
		command := exec.CommandContext(ctx, executable, "-test.run=^TestT107cHelperProcess$")
		command.Env = append(os.Environ(), t107cHelperEnv+"=1", t107cHelperMarkerEnv+"="+marker)
		if err := command.Run(); err != nil {
			return execbackend.ToolCallResult{}, fmt.Errorf("t107c tool process: %w", err)
		}
		exitCode := 0
		return execbackend.ToolCallResult{
			Status:    execbackend.ToolResultStatusOK,
			ExitCode:  &exitCode,
			OutputRef: t107cToolOutputRef,
		}, nil
	}
}

// t107cMarkerLines 数标记文件的行数：0（含文件不存在）= 工具进程从未启动。
func t107cMarkerLines(t *testing.T, marker string) int {
	t.Helper()
	data, err := os.ReadFile(marker)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("read marker %s: %v", marker, err)
	}
	return strings.Count(string(data), "\n")
}

// ---------------------------------------------------------------------------
// 审计读取辅助：按 (hook_type, source_event_id) 分组
// ---------------------------------------------------------------------------

// t107cAudit 是审计记录按 (hook_type, source_event_id) 的分组视图：Counts 是条数，
// Failures 是 Outcome=failure 的条数。SourceEventID 是 T1.07.c 新增的审计键。
//
// 只收 hook 域的记录：policy.EnforceBoundary 也会写审计（每个执行边界一条 EventSecurity，
// 见 policy.go 的 EnforceBoundary），它属于另一个域，读者按 EventType 过滤——这正是
// Details 里那个 source_event_id 的用途：hook 记录能自我标识挂在哪条执行事件上。
type t107cAudit struct {
	Entries []audit.AuditLogEntry
	// NonHook 是被过滤掉的非 hook 记录条数（policy 决策审计），只作诊断。
	NonHook  int
	Counts   map[string]int
	Failures map[string]int
}

func t107cGroup(hook backendhooks.HookType, eventID string) string {
	return string(hook) + "\x00" + eventID
}

func t107cReadAudit(t *testing.T, storage *audit.MemoryStorage) t107cAudit {
	t.Helper()
	entries, err := storage.Query(context.Background(), &audit.AuditQuery{Limit: 1000})
	if err != nil {
		t.Fatalf("query audit storage: %v", err)
	}
	view := t107cAudit{
		Counts:   map[string]int{},
		Failures: map[string]int{},
	}
	for _, entry := range entries {
		if entry.EventType != audit.EventHook {
			view.NonHook++
			continue
		}
		view.Entries = append(view.Entries, entry)
		eventID, _ := entry.Details[backendhooks.SourceEventIDKey].(string)
		if eventID == "" {
			t.Errorf("hook audit entry %s (action %q) carries no %s: an event-id deduping reader cannot place it",
				entry.ID, entry.Action, backendhooks.SourceEventIDKey)
		}
		key := t107cGroup(backendhooks.HookType(entry.Action), eventID)
		view.Counts[key]++
		if entry.Outcome == audit.OutcomeFailure {
			view.Failures[key]++
		}
	}
	return view
}

// t107cWantCount 断言某个 (hook, event) 组的条数与失败数。
func t107cWantCount(t *testing.T, view t107cAudit, hook backendhooks.HookType, eventID string, want, wantFailures int) {
	t.Helper()
	key := t107cGroup(hook, eventID)
	if got := view.Counts[key]; got != want {
		t.Errorf("audit records for (%s, %s) = %d, want %d", hook, eventID, got, want)
	}
	if got := view.Failures[key]; got != wantFailures {
		t.Errorf("failed audit records for (%s, %s) = %d, want %d", hook, eventID, got, wantFailures)
	}
}

// ---------------------------------------------------------------------------
// 点名测试 1：一个事件只有一个触发记录
// ---------------------------------------------------------------------------

// TestToolHookCalledOnce 是 §15 T1.07 的第一条验收：一个事件只有一个触发记录。
//
// 同一个工具事件被 20 个 goroutine 同时送进 RunTool（屏障放行，随后串行重复一次，模拟
// 重复帧）——PreToolUse / PostToolUse handler 各恰好 1 次、PostToolUse handler 看到恰好
// 1 条结果、所有调用拿到同一结论；审计里该 event_id 的两个 hook 各恰好 1 条。另外两个
// 工具事件（不同 tool_call_id）各自再有 1 条。重复帧还会把“结论被记忆”钉死：把 PreToolUse
// 换成拒绝 handler 之后，同一 event_id 的重复帧仍然拿到第一次的结论（不触发第二次）。
//
// 边界（写在这里以免被读成更强的承诺）：
//   - 本测试不断言 execute 次数。重复帧如果穿过单飞窗口以外的路径（例如 port 记忆被淘汰）
//     会让工具再执行一次：RunTool 不是执行去重层，执行去重归 T1.04/T1.13 的
//     观察 → 事件映射（同一 ProviderRef 只有一个 tool 事件）。
//   - 20 个并发调用各自都真的执行了工具（每个 goroutine 的结果来自它自己那次 execute，
//     只是形状相同）。让 PostToolUse handler 只看到 1 次的是 Port.once 的单飞：第一个
//     触发持有键，其余在飞者等待并复用同一结论；串行重复帧则整条路径复用记忆。
func TestToolHookCalledOnce(t *testing.T) {
	mgr := hookTestEnv(t)
	t107cAllowlist(t, mgr)
	storage := t107cAuditEnv(t)

	var preCalls, postCalls, denyCalls int32
	t107cRegisterHook(t, mgr, backendhooks.HookConfig{Name: "t107c-once-pre", Type: backendhooks.HookPreToolUse},
		func(_ context.Context, payload backendhooks.HookPayload) (backendhooks.HookResult, error) {
			atomic.AddInt32(&preCalls, 1)
			return payload, nil
		})
	t107cRegisterHook(t, mgr, backendhooks.HookConfig{Name: "t107c-once-post", Type: backendhooks.HookPostToolUse},
		func(_ context.Context, payload backendhooks.HookPayload) (backendhooks.HookResult, error) {
			atomic.AddInt32(&postCalls, 1)
			return payload, nil
		})

	port := New()
	server := &t107cEventID{}
	marker := filepath.Join(t.TempDir(), "tool-runs.log")
	execute := t107cToolProcess(marker)
	warn := &t107cWarnRecorder{}

	// 重复帧：20 个 goroutine 先到屏障，再一起放行（真正的并发进入窗口）。
	dupEvent := server.For("toolu_dup")
	dup := t107cToolRequest(t, dupEvent, "toolu_dup", "read_file")

	const goroutines = 20
	ready := make(chan struct{}, goroutines)
	release := make(chan struct{})
	results := make([]execbackend.ToolCallResult, goroutines)
	errs := make([]error, goroutines)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(index int) {
			defer wg.Done()
			ready <- struct{}{}
			<-release
			results[index], errs[index] = execbackend.RunTool(context.Background(), port, warn.sink, dup, execute)
		}(i)
	}
	for i := 0; i < goroutines; i++ {
		<-ready
	}
	close(release)
	wg.Wait()

	if got := atomic.LoadInt32(&preCalls); got != 1 {
		t.Fatalf("PreToolUse handler ran %d times for one event_id across %d goroutines, want exactly 1", got, goroutines)
	}
	if got := atomic.LoadInt32(&postCalls); got != 1 {
		t.Fatalf("PostToolUse handler ran %d times for one event_id across %d goroutines, want exactly 1", got, goroutines)
	}
	for i := range results {
		if errs[i] != nil {
			t.Fatalf("goroutine %d got %v, want the remembered conclusion (nil)", i, errs[i])
		}
		if got := results[i]; got.Status != execbackend.ToolResultStatusOK || got.ExitCode == nil || *got.ExitCode != 0 || got.OutputRef != t107cToolOutputRef {
			t.Fatalf("goroutine %d result = %+v, want the execute result (every caller must get the same conclusion)", i, got)
		}
	}

	// 重复帧（串行）：服务端为同一 (run, tool_call_id) 分配同一个 event_id，不产生第二次触发。
	if _, err := execbackend.RunTool(context.Background(), port, warn.sink, dup, execute); err != nil {
		t.Fatalf("repeated frame: %v, want the remembered conclusion", err)
	}
	if got := atomic.LoadInt32(&preCalls); got != 1 {
		t.Fatalf("PreToolUse handler ran %d times after a repeated frame, want 1", got)
	}
	if got := atomic.LoadInt32(&postCalls); got != 1 {
		t.Fatalf("PostToolUse handler ran %d times after a repeated frame, want 1", got)
	}

	// 另一个工具事件（不同 tool_call_id）：自己的 event_id 各触发一次；其中 toolu_other
	// 也发一次重复帧，仍然是 1 条。
	otherEvent := server.For("toolu_other")
	thirdEvent := server.For("toolu_third")
	for _, frame := range []struct {
		eventID    string
		toolCallID string
		tool       string
	}{
		{otherEvent, "toolu_other", "search"},
		{otherEvent, "toolu_other", "search"}, // 重复帧
		{thirdEvent, "toolu_third", "write"},
	} {
		request := t107cToolRequest(t, frame.eventID, frame.toolCallID, frame.tool)
		if _, err := execbackend.RunTool(context.Background(), port, warn.sink, request, execute); err != nil {
			t.Fatalf("RunTool(%s): %v", frame.eventID, err)
		}
	}
	if got := atomic.LoadInt32(&preCalls); got != 3 {
		t.Fatalf("PreToolUse handler ran %d times for 3 distinct events, want 3", got)
	}
	if got := atomic.LoadInt32(&postCalls); got != 3 {
		t.Fatalf("PostToolUse handler ran %d times for 3 distinct events, want 3", got)
	}

	// 记忆的是结论：把 PreToolUse 换成拒绝 handler 之后，同一 event_id 的重复帧仍然拿到
	// 第一次的结论（重复帧根本不会再触发）。
	if err := mgr.Unregister("t107c-once-pre"); err != nil {
		t.Fatalf("unregister the allowing hook: %v", err)
	}
	t107cRegisterHook(t, mgr, backendhooks.HookConfig{Name: "t107c-once-pre-deny", Type: backendhooks.HookPreToolUse},
		func(_ context.Context, payload backendhooks.HookPayload) (backendhooks.HookResult, error) {
			atomic.AddInt32(&denyCalls, 1)
			return nil, errors.New("denied by the replacement hook")
		})
	if _, err := execbackend.RunTool(context.Background(), port, warn.sink, dup, execute); err != nil {
		t.Fatalf("repeated frame after the hook was replaced = %v, want the remembered conclusion (the first trigger's)", err)
	}
	if got := atomic.LoadInt32(&denyCalls); got != 0 {
		t.Fatalf("the replacement handler ran %d times for a repeated frame, want 0 (one event, one trigger record)", got)
	}
	if got := atomic.LoadInt32(&preCalls); got != 3 {
		t.Fatalf("PreToolUse handler ran %d times in total, want 3 (3 distinct events)", got)
	}

	// 审计：该 event_id 的 PreToolUse 恰好 1 条、PostToolUse 恰好 1 条；另一个事件各自再有 1 条。
	view := t107cReadAudit(t, storage)
	t107cWantCount(t, view, backendhooks.HookPreToolUse, dupEvent, 1, 0)
	t107cWantCount(t, view, backendhooks.HookPostToolUse, dupEvent, 1, 0)
	t107cWantCount(t, view, backendhooks.HookPreToolUse, otherEvent, 1, 0)
	t107cWantCount(t, view, backendhooks.HookPostToolUse, otherEvent, 1, 0)
	t107cWantCount(t, view, backendhooks.HookPreToolUse, thirdEvent, 1, 0)
	t107cWantCount(t, view, backendhooks.HookPostToolUse, thirdEvent, 1, 0)
	if len(view.Entries) != 6 {
		t.Errorf("audit has %d hook records, want exactly 6 (3 events x pre+post)", len(view.Entries))
	}
	if warnings := warn.all(); len(warnings) != 0 {
		t.Errorf("warnings = %+v, want none (every hook succeeded)", warnings)
	}
}

// ---------------------------------------------------------------------------
// 点名测试 2：before 拒绝 = 进程一次都不启动
// ---------------------------------------------------------------------------

// startRunGuarded 是 T1.04.c 的 worker 必须遵守的启动顺序：AuthorizeRunStart 通过之后
// 才调用后端的 Prepare/Start；拒绝时一次都不调用。
//
// 它不是生产接线（Run 启动归 T1.04.c/T1.13），只是在本测试里把契约摆出来：
// RunStart 与 PreToolUse 一样是 reject 策略，hook 拒绝 = 进程不启动（不是“先启动再杀掉”）。
func startRunGuarded(ctx context.Context, port execbackend.HookPort, backend execbackend.Backend, hookRequest execbackend.RunLifecycleRequest, start execbackend.StartRequest) (execbackend.Session, error) {
	if err := execbackend.AuthorizeRunStart(ctx, port, hookRequest); err != nil {
		return nil, err
	}
	prepared, err := backend.Prepare(ctx, start)
	if err != nil {
		return nil, err
	}
	return backend.Start(ctx, prepared)
}

// t107cStartRequest 构造一次合法的启动请求（fake 后端会校验它）。
func t107cStartRequest(t *testing.T) execbackend.StartRequest {
	t.Helper()
	return execbackend.StartRequest{
		Run: testRunRef(),
		Input: execbackend.FrozenInput{
			SnapshotID:   "snapshot-1",
			SnapshotHash: "sha256:" + strings.Repeat("a", 64),
			Prompt:       "t107c",
		},
		WorkDir: t.TempDir(),
		Process: execbackend.ProcessControl{GracePeriod: time.Second},
	}
}

// t107cObservations 读完会话的全部观察（带超时，避免实现回归时挂死测试）。
func t107cObservations(t *testing.T, session execbackend.Session) []execbackend.Observation {
	t.Helper()
	var collected []execbackend.Observation
	timeout := time.After(10 * time.Second)
	for {
		select {
		case observation, ok := <-session.Observations():
			if !ok {
				return collected
			}
			collected = append(collected, observation)
		case <-timeout:
			t.Fatal("the fake session never closed its observations")
			return collected
		}
	}
}

// TestBeforeHookDenyPreventsProcess 是 §15 T1.07 的第二条验收：hook 异常不会绕过策略。
// 两个子测试覆盖 before 类的两个点：(a) 工具（PreToolUse 拒绝 → 工具进程从未启动）；
// (b) Run 启动（RunStart 拒绝 → 后端 Start 0 次、没有 process_started 观察）。
func TestBeforeHookDenyPreventsProcess(t *testing.T) {
	t.Run("tool call", func(t *testing.T) {
		mgr := hookTestEnv(t)
		t107cAllowlist(t, mgr)
		storage := t107cAuditEnv(t)

		denied := errors.New("policy says no")
		var preCalls, postCalls int32
		t107cRegisterHook(t, mgr, backendhooks.HookConfig{Name: "t107c-deny-pre", Type: backendhooks.HookPreToolUse},
			func(_ context.Context, payload backendhooks.HookPayload) (backendhooks.HookResult, error) {
				atomic.AddInt32(&preCalls, 1)
				return nil, denied
			})
		t107cRegisterHook(t, mgr, backendhooks.HookConfig{Name: "t107c-deny-post", Type: backendhooks.HookPostToolUse},
			func(_ context.Context, payload backendhooks.HookPayload) (backendhooks.HookResult, error) {
				atomic.AddInt32(&postCalls, 1)
				return payload, nil
			})

		port := New()
		eventID := "t107c-evt-deny-1"
		request := t107cToolRequest(t, eventID, "toolu_deny", "run_shell")
		marker := filepath.Join(t.TempDir(), "tool-runs.log")
		warn := &t107cWarnRecorder{}

		result, err := execbackend.RunTool(context.Background(), port, warn.sink, request, t107cToolProcess(marker))

		if !errors.Is(err, execbackend.ErrHookDenied) {
			t.Fatalf("RunTool error = %v, want it to wrap ErrHookDenied", err)
		}
		if !errors.Is(err, denied) {
			t.Errorf("RunTool error %v does not unwrap to the hook failure", err)
		}
		if result.Status != execbackend.ToolResultStatusDenied {
			t.Errorf("result status = %q, want %q", result.Status, execbackend.ToolResultStatusDenied)
		}
		if lines := t107cMarkerLines(t, marker); lines != 0 {
			t.Fatalf("the tool process started %d times after a PreToolUse denial, want 0", lines)
		}
		if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
			t.Errorf("marker file exists (%v): the tool process must not have started", statErr)
		}
		if got := atomic.LoadInt32(&preCalls); got != 1 {
			t.Errorf("PreToolUse handler ran %d times, want 1", got)
		}
		if got := atomic.LoadInt32(&postCalls); got != 0 {
			t.Errorf("PostToolUse handler ran %d times after a denial, want 0 (a denied call must not report a result)", got)
		}
		if warnings := warn.all(); len(warnings) != 0 {
			t.Errorf("warnings = %+v, want none (a denial is not a warning)", warnings)
		}

		view := t107cReadAudit(t, storage)
		t107cWantCount(t, view, backendhooks.HookPreToolUse, eventID, 1, 1)
		t107cWantCount(t, view, backendhooks.HookPostToolUse, eventID, 0, 0)
		if len(view.Entries) != 1 {
			t.Errorf("audit has %d records, want exactly 1 (the single denied PreToolUse trigger)", len(view.Entries))
		}
	})

	t.Run("run start", func(t *testing.T) {
		mgr := hookTestEnv(t)
		t107cAllowlist(t, mgr)
		storage := t107cAuditEnv(t)

		denied := errors.New("no process for you")
		var startCalls int32
		t107cRegisterHook(t, mgr, backendhooks.HookConfig{Name: "t107c-deny-run-start", Type: backendhooks.HookRunStart},
			func(_ context.Context, payload backendhooks.HookPayload) (backendhooks.HookResult, error) {
				atomic.AddInt32(&startCalls, 1)
				return nil, denied
			})

		port := New()
		backend := backendfake.New(backendfake.Options{Name: "fake"})
		start := t107cStartRequest(t)

		// Run 的启动没有（也不该有）警告 sink：RunStart 拒绝时调用方根本不启动，没有
		// “已经发生的事”需要报告。所以这里只需要断言拒绝结果与审计。
		claimID := "t107c-claim-1"
		hookRequest := testRunRequest(t, claimID, "starting", "queued")
		session, err := startRunGuarded(context.Background(), port, backend, hookRequest, start)
		if session != nil {
			t.Fatalf("startRunGuarded returned a session (%T) after a RunStart denial", session)
		}
		if !errors.Is(err, execbackend.ErrHookDenied) {
			t.Fatalf("startRunGuarded error = %v, want it to wrap ErrHookDenied", err)
		}
		if !errors.Is(err, denied) {
			t.Errorf("startRunGuarded error %v does not unwrap to the hook failure", err)
		}
		if got := atomic.LoadInt32(&startCalls); got != 1 {
			t.Errorf("RunStart handler ran %d times, want 1", got)
		}

		view := t107cReadAudit(t, storage)
		t107cWantCount(t, view, backendhooks.HookRunStart, claimID, 1, 1)
		if len(view.Entries) != 1 {
			t.Errorf("audit has %d records, want exactly 1 (the single denied RunStart trigger)", len(view.Entries))
		}

		// 正向对照：同一个 helper 在 RunStart 放行时真的会启动后端（证明上面的 0 次不是
		// “helper 从来不会启动”这个空洞事实）。
		if err := mgr.Unregister("t107c-deny-run-start"); err != nil {
			t.Fatalf("unregister the denying hook: %v", err)
		}
		t107cRegisterHook(t, mgr, backendhooks.HookConfig{Name: "t107c-allow-run-start", Type: backendhooks.HookRunStart},
			func(_ context.Context, payload backendhooks.HookPayload) (backendhooks.HookResult, error) {
				atomic.AddInt32(&startCalls, 1)
				return payload, nil
			})

		allowedID := "t107c-claim-2"
		allowedRequest := testRunRequest(t, allowedID, "starting", "queued")
		session, err = startRunGuarded(context.Background(), port, backend, allowedRequest, start)
		if err != nil {
			t.Fatalf("startRunGuarded with an allowing RunStart hook: %v", err)
		}
		if session == nil {
			t.Fatal("startRunGuarded returned no session although the RunStart hook allowed the start")
		}
		defer func() {
			if closeErr := session.Close(); closeErr != nil {
				t.Errorf("close the session: %v", closeErr)
			}
		}()
		observations := t107cObservations(t, session)
		if len(observations) == 0 || observations[0].Kind != execbackend.ObservationProcessStarted {
			t.Fatalf("observations = %+v, want process_started first (the backend really starts when the hook allows it)", observations)
		}

		view = t107cReadAudit(t, storage)
		t107cWantCount(t, view, backendhooks.HookRunStart, claimID, 1, 1)
		t107cWantCount(t, view, backendhooks.HookRunStart, allowedID, 1, 0)
		if len(view.Entries) != 2 {
			t.Errorf("audit has %d records, want 2 (the denied claim and the allowed claim)", len(view.Entries))
		}
	})
}

// ---------------------------------------------------------------------------
// 点名测试 3：after 失败不重放工具
// ---------------------------------------------------------------------------

// TestAfterHookFailureDoesNotReplayTool 是 §15 T1.07 的第二条验收的后半：after 类失败
// 只重跑 handler（HookConfig.RetryCount），绝不重跑已经执行的工具，也不改变调用方拿到的
// 结果；审计里该 event_id 只有一条 PostToolUse 记录（一次 Trigger 一条，不是每次重试一条）。
func TestAfterHookFailureDoesNotReplayTool(t *testing.T) {
	mgr := hookTestEnv(t)
	t107cAllowlist(t, mgr)
	storage := t107cAuditEnv(t)

	postFailure := errors.New("post hook exploded")
	var preCalls, postCalls int32
	t107cRegisterHook(t, mgr, backendhooks.HookConfig{Name: "t107c-replay-pre", Type: backendhooks.HookPreToolUse},
		func(_ context.Context, payload backendhooks.HookPayload) (backendhooks.HookResult, error) {
			atomic.AddInt32(&preCalls, 1)
			return payload, nil
		})
	t107cRegisterHook(t, mgr, backendhooks.HookConfig{
		Name:       "t107c-replay-post",
		Type:       backendhooks.HookPostToolUse,
		RetryCount: 2,
		Timeout:    10 * time.Second,
	}, func(_ context.Context, payload backendhooks.HookPayload) (backendhooks.HookResult, error) {
		atomic.AddInt32(&postCalls, 1)
		return nil, postFailure
	})

	port := New()
	eventID := "t107c-evt-post-fail-1"
	request := t107cToolRequest(t, eventID, "toolu_post_fail", "read_file")
	marker := filepath.Join(t.TempDir(), "tool-runs.log")
	warn := &t107cWarnRecorder{}

	result, err := execbackend.RunTool(context.Background(), port, warn.sink, request, t107cToolProcess(marker))

	if err != nil {
		t.Fatalf("RunTool returned %v; the tool's own error is nil and an after-hook failure must not become the caller's error", err)
	}
	if result.Status != execbackend.ToolResultStatusOK || result.ExitCode == nil || *result.ExitCode != 0 || result.OutputRef != t107cToolOutputRef {
		t.Errorf("result = %+v, want the execute result unchanged", result)
	}
	if lines := t107cMarkerLines(t, marker); lines != 1 {
		t.Fatalf("the tool process started %d times, want exactly 1 (a failing after hook must never replay the tool)", lines)
	}
	if got := atomic.LoadInt32(&preCalls); got != 1 {
		t.Errorf("PreToolUse handler ran %d times, want 1", got)
	}
	if got := atomic.LoadInt32(&postCalls); got != 3 {
		t.Errorf("PostToolUse handler ran %d times, want 3 (1 attempt + RetryCount 2: retries rerun the handler, not the tool)", got)
	}

	warnings := warn.all()
	if len(warnings) != 1 {
		t.Fatalf("warnings = %+v, want exactly 1", warnings)
	}
	if warnings[0].Hook != execbackend.HookPointPostToolUse || warnings[0].EventID != eventID || !errors.Is(warnings[0].Err, postFailure) {
		t.Errorf("warning = %+v, want post-tool/%s/underlying error", warnings[0], eventID)
	}

	view := t107cReadAudit(t, storage)
	t107cWantCount(t, view, backendhooks.HookPostToolUse, eventID, 1, 1)
	t107cWantCount(t, view, backendhooks.HookPreToolUse, eventID, 1, 0)
	if len(view.Entries) != 2 {
		t.Fatalf("audit has %d records, want exactly 2 (pre + the single post trigger)", len(view.Entries))
	}
	for _, entry := range view.Entries {
		if backendhooks.HookType(entry.Action) != backendhooks.HookPostToolUse {
			continue
		}
		if got := entry.Details["retry_count"]; got != 2 {
			t.Errorf("post record retry_count = %v, want 2 (the configuration whose retries are recorded once)", got)
		}
		if got, ok := entry.Details[backendhooks.DedupeKeyKey]; !ok || got != string(backendhooks.HookPostToolUse)+":"+eventID {
			t.Errorf("post record %s = %v, want %q (the dedupe key the single flight uses)",
				backendhooks.DedupeKeyKey, got, string(backendhooks.HookPostToolUse)+":"+eventID)
		}
	}
}

// ---------------------------------------------------------------------------
// 有界记忆的边界（显式钉住，不假装没有）
// ---------------------------------------------------------------------------

// TestSourceEventIDEvictionLeavesASecondAuditRecord 钉住 Port 有界记忆的已知边界：
// MaxEntries=1 时第一个事件很快被淘汰，同一个 event_id 再触发会真的再跑一次 handler，
// 于是审计里出现第二条同 (hook, event_id) 的记录。这不是“去重失效”的意外，而是写明的
// 取舍（Port.once 的注释）：记忆有界换内存，容量远大于同时在飞的事件数。审计携带
// source_event_id 的意义就在这里——读取方能按 event_id 去重/核对，而不是数行数。
//
// 持久化的触发记录（一个事件一行、可核对）归 T1.04：它需要事件表与 outbox，不在本步。
func TestSourceEventIDEvictionLeavesASecondAuditRecord(t *testing.T) {
	mgr := hookTestEnv(t)
	t107cAllowlist(t, mgr)
	storage := t107cAuditEnv(t)

	var calls int32
	t107cRegisterHook(t, mgr, backendhooks.HookConfig{Name: "t107c-evict-pre", Type: backendhooks.HookPreToolUse},
		func(_ context.Context, payload backendhooks.HookPayload) (backendhooks.HookResult, error) {
			atomic.AddInt32(&calls, 1)
			return payload, nil
		})

	// 容量 1：任何第二个完成的键都会把第一个挤出去。
	port := &Port{MaxEntries: 1}
	evicted := t107cToolRequest(t, "t107c-evt-evict-1", "toolu_evict", "read_file")
	other := t107cToolRequest(t, "t107c-evt-evict-2", "toolu_evict_other", "search")

	if err := port.BeforeTool(context.Background(), evicted); err != nil {
		t.Fatalf("first trigger: %v", err)
	}
	if err := port.BeforeTool(context.Background(), other); err != nil {
		t.Fatalf("second trigger: %v", err)
	}
	// 同一个 event_id 的重复帧：键已被淘汰，于是再触发一次。
	if err := port.BeforeTool(context.Background(), evicted); err != nil {
		t.Fatalf("repeated frame after eviction: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("handler ran %d times, want 3 (the bounded memory forgot the first event, so the repeated frame triggers again)", got)
	}

	view := t107cReadAudit(t, storage)
	t107cWantCount(t, view, backendhooks.HookPreToolUse, "t107c-evt-evict-1", 2, 0)
	t107cWantCount(t, view, backendhooks.HookPreToolUse, "t107c-evt-evict-2", 1, 0)
	if len(view.Entries) != 3 {
		t.Fatalf("audit has %d records, want 3 (two for the evicted event, one for the other)", len(view.Entries))
	}
	// 两条记录都带着同一个来源事件 ID 与同一个去重键：按 event_id 去重的读取方能把它们
	// 归到一行，这正是这个键存在的理由。
	seen := 0
	for _, entry := range view.Entries {
		if entry.Details[backendhooks.SourceEventIDKey] != "t107c-evt-evict-1" {
			continue
		}
		seen++
		if got := entry.Details[backendhooks.DedupeKeyKey]; got != string(backendhooks.HookPreToolUse)+":t107c-evt-evict-1" {
			t.Errorf("evicted record %s = %v, want the shared dedupe key", backendhooks.DedupeKeyKey, got)
		}
	}
	if seen != 2 {
		t.Fatalf("records carrying the evicted event id = %d, want 2", seen)
	}
}

// 编译期钉住本文件用到的两个审计键（改名会在这里先炸，而不是在断言里）。
var _ = []string{backendhooks.SourceEventIDKey, backendhooks.DedupeKeyKey}
