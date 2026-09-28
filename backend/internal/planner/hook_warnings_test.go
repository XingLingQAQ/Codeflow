package planner

import (
	"bytes"
	stdcontext "context"
	"errors"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	backendhooks "github.com/codeflow/backend/internal/hooks"
	"github.com/codeflow/backend/internal/policy"
	"github.com/codeflow/backend/internal/policy/policytesting"
)

// 本文件是 T1.07.b 第 3 组的触发点测试（§15 T1.07、§28 T1.07.b）。它用真实的
// InMemoryPlanner.UpdateTask 与真实的 hooks.NewHookManager()（SetHookManager 注入、
// Cleanup 恢复）钉住四件事：
//
//   - 四个任务 hook（in_progress → BeforeTaskExecute、completed →
//     AfterTaskExecute + OnTaskComplete、cancelled → OnTaskFailure）失败时，
//     UpdateTask 成功、状态正确、CompletedCount 与计划状态与「没有失败 hook」时
//     逐项相同，每个失败 hook 恰好触发一次、恰好一条警告；
//   - retry：HookConfig.RetryCount=2 时 handler 恰好跑 3 次、警告恰好 1 条，
//     任务迁移只发生一次（状态/计数/时间戳各查一次）——retry 只重跑 handler，
//     绝不重做状态迁移；
//   - 没有 sink 时走默认日志：每次失败恰好一行，含 hook 类型与 task id，
//     绝不含任务标题/描述里的标记串；
//   - sink 读写并发安全（20 个 goroutine 同时触发失败的 hook，20 条警告一条不多
//     一条不少；本机无 cgo/gcc，-race 不可用，见回执的 not_run 登记）。

// hookTestEnv 安装一个真实的 hook 管理器（Cleanup 时恢复原管理器与 nil sink）。
func hookTestEnv(t *testing.T) *backendhooks.HookManager {
	t.Helper()
	policytesting.AllowForTest(t, policy.OperationHookExecute)
	previous := backendhooks.GetHookManager()
	mgr := backendhooks.NewHookManager()
	backendhooks.SetHookManager(mgr)
	t.Cleanup(func() {
		backendhooks.SetHookManager(previous)
		SetHookWarningSink(nil)
	})
	return mgr
}

// registerTaskHook 注册一个 hook handler，并返回它的调用次数。handler 必须非 nil：
// 断言错误身份时要用同一个错误值（管理器会用 %w 包一层，errors.Is 仍然认得）。
func registerTaskHook(t *testing.T, mgr *backendhooks.HookManager, hook backendhooks.HookType, retryCount int, handler backendhooks.HookFunc) *atomic.Int32 {
	t.Helper()
	var calls atomic.Int32
	name := "task-hook-" + string(hook)
	err := mgr.Register(backendhooks.HookConfig{
		Name:       name,
		Type:       hook,
		Enabled:    true,
		Timeout:    5 * time.Second,
		RetryCount: retryCount,
	}, func(ctx stdcontext.Context, payload backendhooks.HookPayload) (backendhooks.HookResult, error) {
		calls.Add(1)
		return handler(ctx, payload)
	})
	if err != nil {
		t.Fatalf("register %s hook: %v", hook, err)
	}
	return &calls
}

// failingHandler 是必然失败的 handler，错误身份固定为 hookErr。
func failingHandler(hookErr error) backendhooks.HookFunc {
	return func(stdcontext.Context, backendhooks.HookPayload) (backendhooks.HookResult, error) {
		return nil, hookErr
	}
}

// succeedingHandler 是成功的 handler，用于对照组。
func succeedingHandler() backendhooks.HookFunc {
	return func(_ stdcontext.Context, payload backendhooks.HookPayload) (backendhooks.HookResult, error) {
		return payload, nil
	}
}

// capturePlannerWarnings 安装一个收集警告的 sink，Cleanup 时恢复 nil（默认日志）。
func capturePlannerWarnings(t *testing.T) (*[]HookWarning, *sync.Mutex) {
	t.Helper()
	var mu sync.Mutex
	var warnings []HookWarning
	SetHookWarningSink(func(_ stdcontext.Context, w HookWarning) {
		mu.Lock()
		warnings = append(warnings, w)
		mu.Unlock()
	})
	t.Cleanup(func() { SetHookWarningSink(nil) })
	return &warnings, &mu
}

// plannerWarnings 返回已收集的警告快照。
func plannerWarnings(warnings *[]HookWarning, mu *sync.Mutex) []HookWarning {
	mu.Lock()
	defer mu.Unlock()
	return append([]HookWarning(nil), (*warnings)...)
}

// plannerWarningLog 把默认警告日志抓到内存里（缺少 sink 时用）。
func plannerWarningLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var captured bytes.Buffer
	var mu sync.Mutex
	previous := log.Writer()
	log.SetOutput(&lockedWriter{mu: &mu, w: &captured})
	t.Cleanup(func() { log.SetOutput(previous) })
	return &captured
}

// lockedWriter 让 log.SetOutput 的写入与测试读取互斥（日志可能来自 hook goroutine）。
type lockedWriter struct {
	mu *sync.Mutex
	w  *bytes.Buffer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// taskMigration 是「一次 UpdateTask 对任务与计划做了什么」的快照。收口断言要求
// 失败 hook 之下的快照与没有失败 hook 时的快照逐项相同（时间戳除外）。
type taskMigration struct {
	Status         TaskStatus
	StartedAt      int64
	CompletedAt    int64
	ActualMs       int64
	PlanStatus     string
	CompletedCount int
	TaskCount      int
	BlockedStatus  TaskStatus
}

// seedPlanner 建一个含两个任务的计划：第一个任务依赖第二个任务被完成才解除阻塞
// （blocker → dependent），这样「解除依赖」也会出现在快照里。
func seedPlanner(t *testing.T) (*InMemoryPlanner, string, *Task, *Task) {
	t.Helper()
	svc := NewInMemoryPlanner()
	plan, err := svc.CreatePlan(stdcontext.Background(), &PlanCreateRequest{Title: "hook plan"})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	blocker, err := svc.CreateTask(stdcontext.Background(), plan.ID, &TaskCreateRequest{Title: taskTitleMark + " blocker"})
	if err != nil {
		t.Fatalf("CreateTask blocker: %v", err)
	}
	dependent, err := svc.CreateTask(stdcontext.Background(), plan.ID, &TaskCreateRequest{
		Title:        taskTitleMark + " dependent",
		Dependencies: []string{blocker.ID},
	})
	if err != nil {
		t.Fatalf("CreateTask dependent: %v", err)
	}
	return svc, plan.ID, blocker, dependent
}

// planMigration 跑一遍固定的状态序列并用真实服务读回快照。
func planMigration(t *testing.T, svc *InMemoryPlanner, planID string, blocker, dependent *Task) taskMigration {
	t.Helper()
	ctx := stdcontext.Background()
	if _, err := svc.UpdateTask(ctx, planID, blocker.ID, &TaskUpdateRequest{Status: TaskStatusInProgress}); err != nil {
		t.Fatalf("UpdateTask in_progress: %v", err)
	}
	if _, err := svc.UpdateTask(ctx, planID, blocker.ID, &TaskUpdateRequest{Status: TaskStatusCompleted}); err != nil {
		t.Fatalf("UpdateTask completed: %v", err)
	}
	if _, err := svc.UpdateTask(ctx, planID, dependent.ID, &TaskUpdateRequest{Status: TaskStatusCancelled}); err != nil {
		t.Fatalf("UpdateTask cancelled: %v", err)
	}

	got, err := svc.GetTask(ctx, planID, blocker.ID)
	if err != nil {
		t.Fatalf("GetTask blocker: %v", err)
	}
	blockedAfter, err := svc.GetTask(ctx, planID, dependent.ID)
	if err != nil {
		t.Fatalf("GetTask dependent: %v", err)
	}
	plan, err := svc.GetPlan(ctx, planID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	return taskMigration{
		Status:         got.Status,
		StartedAt:      got.StartedAt,
		CompletedAt:    got.CompletedAt,
		ActualMs:       got.ActualMs,
		PlanStatus:     plan.Status,
		CompletedCount: plan.CompletedCount,
		TaskCount:      plan.TaskCount,
		BlockedStatus:  blockedAfter.Status,
	}
}

// compareMigration 比对「失败 hook」与「对照」的快照：除时间戳外必须逐项相同；
// 时间戳只断言「失败侧有的，对照侧也有」（两边各自跑，值不可能相同）。
func compareMigration(t *testing.T, withHooks, control taskMigration) {
	t.Helper()
	if withHooks.Status != control.Status {
		t.Errorf("task status = %s, want %s (a failing hook must not change it)", withHooks.Status, control.Status)
	}
	if withHooks.PlanStatus != control.PlanStatus {
		t.Errorf("plan status = %s, want %s", withHooks.PlanStatus, control.PlanStatus)
	}
	if withHooks.CompletedCount != control.CompletedCount || withHooks.TaskCount != control.TaskCount {
		t.Errorf("plan counts = %d/%d, want %d/%d", withHooks.CompletedCount, withHooks.TaskCount, control.CompletedCount, control.TaskCount)
	}
	if withHooks.BlockedStatus != control.BlockedStatus {
		t.Errorf("dependent task status = %s, want %s (unblocking must still happen once)", withHooks.BlockedStatus, control.BlockedStatus)
	}
	for name, pair := range map[string][2]int64{
		"started_at":   {withHooks.StartedAt, control.StartedAt},
		"completed_at": {withHooks.CompletedAt, control.CompletedAt},
	} {
		if (pair[0] == 0) != (pair[1] == 0) {
			t.Errorf("%s set=%v, want it to be set=%v", name, pair[0] != 0, pair[1] != 0)
		}
	}
}

// taskTitleMark 是任务标题里的标记串：警告与默认日志都绝不能出现它。
const taskTitleMark = "zz-task-title-mark-zz"

// TestTaskHookFailuresDoNotChangeTaskMigration 是收口的正面证据：四个任务 hook
// 各注册一个必然失败的 handler，跑一遍真实的状态序列（含一次「状态没变、不该再
// 触发」的重复 UpdateTask），结果必须与「注册了同样的 hook、按同样的序列调用但
// handler 成功」的对照逐项相同，而每个失败 hook 恰好触发一次、恰好一条警告。
func TestTaskHookFailuresDoNotChangeTaskMigration(t *testing.T) {
	hookErr := errors.New("task observer exploded")
	fourHooks := []backendhooks.HookType{
		backendhooks.HookBeforeTaskExecute,
		backendhooks.HookAfterTaskExecute,
		backendhooks.HookOnTaskFailure,
		backendhooks.HookOnTaskComplete,
	}

	controlSvc, controlPlan, controlBlocker, controlDependent := seedPlanner(t)
	controlMgr := hookTestEnv(t)
	for _, hook := range fourHooks {
		registerTaskHook(t, controlMgr, hook, 0, succeedingHandler())
	}
	// 对照组必须在这里就注册，否则 hookTestEnv 的 Cleanup 顺序（LIFO）会让它先装
	// 上、再被实验组的环境替换，而两组各自读自己的服务，互不影响。
	control := planMigration(t, controlSvc, controlPlan, controlBlocker, controlDependent)

	svc, planID, blocker, dependent := seedPlanner(t)
	mgr := hookTestEnv(t)
	calls := map[backendhooks.HookType]*atomic.Int32{}
	for _, hook := range fourHooks {
		calls[hook] = registerTaskHook(t, mgr, hook, 0, failingHandler(hookErr))
	}
	warnings, mu := capturePlannerWarnings(t)

	got := planMigration(t, svc, planID, blocker, dependent)
	compareMigration(t, got, control)

	// 每个失败 hook 恰好触发一次（一次状态迁移，一个触发记录）。
	for hook, count := range calls {
		if got := count.Load(); got != 1 {
			t.Errorf("hook %s ran %d times, want exactly 1 (one state change, one trigger)", hook, got)
		}
	}

	// 警告恰好一条/每个失败触发，Hook 与 TaskID 都正确。三次迁移各自只触发它自己
	// 的那些 hook：in_progress → BeforeTaskExecute、completed →
	// AfterTaskExecute + OnTaskComplete、cancelled → OnTaskFailure。
	gotWarnings := plannerWarnings(warnings, mu)
	if len(gotWarnings) != 4 {
		t.Fatalf("warnings = %d %+v, want exactly 4 (before-execute, after-execute, on-complete, on-failure)", len(gotWarnings), gotWarnings)
	}
	wantTaskOf := map[backendhooks.HookType]string{
		backendhooks.HookBeforeTaskExecute: blocker.ID,
		backendhooks.HookAfterTaskExecute:  blocker.ID,
		backendhooks.HookOnTaskComplete:    blocker.ID,
		backendhooks.HookOnTaskFailure:     dependent.ID,
	}
	seen := map[backendhooks.HookType]int{}
	for _, warning := range gotWarnings {
		seen[warning.Hook]++
		if !errors.Is(warning.Err, hookErr) {
			t.Errorf("warning %+v does not carry the handler failure", warning)
		}
		if want := wantTaskOf[warning.Hook]; warning.TaskID != want {
			t.Errorf("warning for %s has task id %q, want %q", warning.Hook, warning.TaskID, want)
		}
	}
	for _, hook := range fourHooks {
		if seen[hook] != 1 {
			t.Errorf("warnings for %s = %d, want exactly 1", hook, seen[hook])
		}
	}

	// 一个事件一个触发记录，跨任务也一样：cancelled 只触发 OnTaskFailure，不会
	// 连带 AfterTaskExecute/OnTaskComplete（上面逐 hook 的计数已经是这条断言）。
	// 再补一次「状态没变」的 UpdateTask：状态迁移不发生，就不该有第二个触发记录。
	ctx := stdcontext.Background()
	if _, err := svc.UpdateTask(ctx, planID, blocker.ID, &TaskUpdateRequest{Status: TaskStatusCompleted}); err != nil {
		t.Fatalf("second UpdateTask completed: %v", err)
	}
	for hook, count := range calls {
		if got := count.Load(); got != 1 {
			t.Errorf("hook %s ran %d times after a no-op update, want 1 (no state change, no trigger)", hook, got)
		}
	}
	if got := len(plannerWarnings(warnings, mu)); got != 4 {
		t.Errorf("warnings = %d after a no-op update, want 4", got)
	}
}

// TestTaskHookRetryOnlyRerunsTheHandler 钉住 retry 行的语义：RetryCount=2 时
// handler 恰好跑 3 次（1 + 2 次重试），用尽后恰好一条警告，而任务的迁移只发生
// 一次——状态、CompletedCount、时间戳都不重复写。
func TestTaskHookRetryOnlyRerunsTheHandler(t *testing.T) {
	hookErr := errors.New("after-execute observer exploded")
	svc, planID, blocker, dependent := seedPlanner(t)
	mgr := hookTestEnv(t)
	calls := registerTaskHook(t, mgr, backendhooks.HookAfterTaskExecute, 2, failingHandler(hookErr))
	warnings, mu := capturePlannerWarnings(t)

	ctx := stdcontext.Background()
	if _, err := svc.UpdateTask(ctx, planID, blocker.ID, &TaskUpdateRequest{Status: TaskStatusInProgress}); err != nil {
		t.Fatalf("UpdateTask in_progress: %v", err)
	}
	updated, err := svc.UpdateTask(ctx, planID, blocker.ID, &TaskUpdateRequest{Status: TaskStatusCompleted})
	if err != nil {
		t.Fatalf("UpdateTask completed: %v", err)
	}
	blockerStartedAt := updated.StartedAt
	if updated.Status != TaskStatusCompleted {
		t.Fatalf("status = %s, want completed", updated.Status)
	}
	if blockerStartedAt == 0 {
		t.Fatalf("started_at = 0, want the in_progress migration to have set it")
	}

	if got := calls.Load(); got != 3 {
		t.Errorf("the handler ran %d times, want exactly 3 (1 + RetryCount=2)", got)
	}
	gotWarnings := plannerWarnings(warnings, mu)
	if len(gotWarnings) != 1 {
		t.Fatalf("warnings = %d %+v, want exactly 1 after the retries are exhausted", len(gotWarnings), gotWarnings)
	}
	if gotWarnings[0].Hook != backendhooks.HookAfterTaskExecute || gotWarnings[0].TaskID != blocker.ID {
		t.Errorf("warning = %+v, want the AfterTaskExecute failure for %s", gotWarnings[0], blocker.ID)
	}
	if !errors.Is(gotWarnings[0].Err, hookErr) {
		t.Errorf("warning error = %v, want the handler failure", gotWarnings[0].Err)
	}

	// 迁移只发生一次：CompletedCount 恰好 +1，解除依赖只做一次。
	plan, err := svc.GetPlan(ctx, planID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if plan.CompletedCount != 1 {
		t.Errorf("plan completed count = %d, want 1: a retried hook must not replay the migration", plan.CompletedCount)
	}
	if plan.TaskCount != 2 {
		t.Errorf("plan task count = %d, want 2", plan.TaskCount)
	}
	// 两个任务里只有一个完成，计划不会因为重试而提前完成。
	if plan.Status == "completed" {
		t.Errorf("plan status = %s, want a non-completed status (one of two tasks is complete)", plan.Status)
	}
	reloaded, err := svc.GetTask(ctx, planID, blocker.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if reloaded.Status != TaskStatusCompleted || reloaded.CompletedAt == 0 {
		t.Errorf("reloaded task = %+v, want completed with a completion timestamp", reloaded)
	}
	if reloaded.StartedAt != blockerStartedAt {
		t.Errorf("started_at = %d, want %d (the retried hook must not re-stamp the migration)", reloaded.StartedAt, blockerStartedAt)
	}
	if reloaded.ActualMs < 0 {
		t.Errorf("actual_ms = %d, want a non-negative duration from the single migration", reloaded.ActualMs)
	}
	blockedAfter, err := svc.GetTask(ctx, planID, dependent.ID)
	if err != nil {
		t.Fatalf("GetTask dependent: %v", err)
	}
	if blockedAfter.Status != TaskStatusPending {
		t.Errorf("dependent task = %s, want pending (unblocked exactly once)", blockedAfter.Status)
	}

	// 再跑一次同样的迁移：状态没有变化，所以不产生新的触发（一个事件一个触发记录）。
	if _, err := svc.UpdateTask(ctx, planID, blocker.ID, &TaskUpdateRequest{Status: TaskStatusCompleted}); err != nil {
		t.Fatalf("second UpdateTask completed: %v", err)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("the handler ran %d times after a no-op update, want 3 (no state change, no trigger)", got)
	}
	if got := len(plannerWarnings(warnings, mu)); got != 1 {
		t.Errorf("warnings = %d after a no-op update, want 1", got)
	}
}

// TestTaskHookWarningDefaultLogOmitsTaskPayload 没有 sink 时警告走一行固定格式日志：
// 每次失败恰好一行，日志含 hook 类型与 task id，绝不含任务标题/描述里的标记串。
func TestTaskHookWarningDefaultLogOmitsTaskPayload(t *testing.T) {
	const descriptionMark = "zz-task-description-mark-zz"
	svc, planID, blocker, dependent := seedPlanner(t)
	mgr := hookTestEnv(t)
	registerTaskHook(t, mgr, backendhooks.HookBeforeTaskExecute, 0, failingHandler(errors.New("observer exploded")))

	// 描述也带上标记串：payload 里有它，警告日志不该有。
	ctx := stdcontext.Background()
	if _, err := svc.UpdateTask(ctx, planID, blocker.ID, &TaskUpdateRequest{Description: descriptionMark}); err != nil {
		t.Fatalf("UpdateTask description: %v", err)
	}

	SetHookWarningSink(nil)
	logged := plannerWarningLog(t)

	if _, err := svc.UpdateTask(ctx, planID, blocker.ID, &TaskUpdateRequest{Status: TaskStatusInProgress}); err != nil {
		t.Fatalf("UpdateTask in_progress: %v", err)
	}

	text := logged.String()
	lines := 0
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		if strings.Contains(line, "WARN") {
			lines++
		}
	}
	if lines != 1 {
		t.Fatalf("default log has %d warning lines, want exactly 1:\n%s", lines, text)
	}
	if !strings.Contains(text, string(backendhooks.HookBeforeTaskExecute)) {
		t.Errorf("default log %q does not name the failing hook", text)
	}
	if !strings.Contains(text, blocker.ID) {
		t.Errorf("default log %q does not name the task", text)
	}
	if strings.Contains(text, taskTitleMark) || strings.Contains(text, descriptionMark) {
		t.Errorf("default log contains task payload content: %q", text)
	}

	// 同一次运行里的另一个任务（cancelled 触发 OnTaskFailure）也不能带标题串。
	if _, err := svc.UpdateTask(ctx, planID, dependent.ID, &TaskUpdateRequest{Status: TaskStatusCancelled}); err != nil {
		t.Fatalf("UpdateTask cancelled: %v", err)
	}
	text = logged.String()
	if strings.Contains(text, taskTitleMark) {
		t.Errorf("default log contains task payload content after a second trigger: %q", text)
	}
}

// TestTaskHookWarningSinkIsConcurrencySafe 20 个 goroutine 同时触发失败的任务 hook：
// sink 恰好收到 20 条警告。生产代码的 sink 读写用 RWMutex 保护，所以这里只做计数
// 断言（本机 -race 不可用，见回执）。
func TestTaskHookWarningSinkIsConcurrencySafe(t *testing.T) {
	mgr := hookTestEnv(t)
	registerTaskHook(t, mgr, backendhooks.HookBeforeTaskExecute, 0, failingHandler(errors.New("concurrent observer exploded")))

	var mu sync.Mutex
	count := 0
	SetHookWarningSink(func(stdcontext.Context, HookWarning) {
		mu.Lock()
		count++
		mu.Unlock()
	})
	t.Cleanup(func() { SetHookWarningSink(nil) })

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			emitGlobalTaskHook(stdcontext.Background(), HookBeforeTaskExecute, &TaskExecutionContext{TaskID: "task-concurrent", PlanID: "plan-1"})
		}()
	}
	wg.Wait()

	mu.Lock()
	got := count
	mu.Unlock()
	if got != goroutines {
		t.Fatalf("sink received %d warnings across %d goroutines, want exactly %d", got, goroutines, goroutines)
	}

	// 卸载 sink 之后再触发一次：回到默认日志，不会 panic，也不会再送给旧 sink。
	SetHookWarningSink(nil)
	plannerWarningLog(t)
	emitGlobalTaskHook(stdcontext.Background(), HookBeforeTaskExecute, &TaskExecutionContext{TaskID: "task-after-uninstall"})
	mu.Lock()
	defer mu.Unlock()
	if count != goroutines {
		t.Fatalf("the uninstalled sink received %d warnings in total, want %d", count, goroutines)
	}
}

// TestEmitGlobalTaskHookWarningShape 直接钉住触发点的警告形状：payload 里的 TaskID
// 被带出来，未知 payload 类型与未知 event 的处理，以及 nil sink 时的默认日志。
func TestEmitGlobalTaskHookWarningShape(t *testing.T) {
	hookErr := errors.New("boom")
	mgr := hookTestEnv(t)
	registerTaskHook(t, mgr, backendhooks.HookOnTaskFailure, 0, func(stdcontext.Context, backendhooks.HookPayload) (backendhooks.HookResult, error) {
		return nil, hookErr
	})
	warnings, mu := capturePlannerWarnings(t)

	emitGlobalTaskHook(stdcontext.Background(), HookOnTaskFailure, &TaskFailureContext{TaskID: "task-failure-1", Title: taskTitleMark})
	got := plannerWarnings(warnings, mu)
	if len(got) != 1 || got[0].Hook != backendhooks.HookOnTaskFailure || got[0].TaskID != "task-failure-1" {
		t.Fatalf("warnings = %+v, want the OnTaskFailure warning for task-failure-1", got)
	}

	// 未知 payload 类型：仍然记一条，只是没有 task id。
	emitGlobalTaskHook(stdcontext.Background(), HookOnTaskFailure, "not-a-task-context")
	got = plannerWarnings(warnings, mu)
	if len(got) != 2 || got[1].TaskID != "" {
		t.Fatalf("warnings = %+v, want a second warning with an empty task id", got)
	}

	// nil payload 不得 panic，也不带 task id。
	emitGlobalTaskHook(stdcontext.Background(), HookOnTaskFailure, nil)
	if got := plannerWarnings(warnings, mu); len(got) != 3 {
		t.Fatalf("warnings = %d, want 3 after a nil payload", len(got))
	}

	// 未知 event：什么都不做——不触发 hook，也不记警告。
	emitGlobalTaskHook(stdcontext.Background(), TaskHookEvent("unknown-event"), &TaskExecutionContext{TaskID: "task-unknown"})
	if got := plannerWarnings(warnings, mu); len(got) != 3 {
		t.Fatalf("warnings = %d after an unknown event, want 3 (unknown events are no-ops)", len(got))
	}
}

// TestMemoryIntegrationEmitNeverReturnsHookFailure 钉住 Emit 的签名语义：全局 hook
// 失败不再让 Emit 返回错误，但失败仍恰好记一条警告（触发点记录，不是调用方）。
func TestMemoryIntegrationEmitNeverReturnsHookFailure(t *testing.T) {
	hookErr := errors.New("before-execute observer exploded")
	mgr := hookTestEnv(t)
	calls := registerTaskHook(t, mgr, backendhooks.HookBeforeTaskExecute, 0, func(stdcontext.Context, backendhooks.HookPayload) (backendhooks.HookResult, error) {
		return nil, hookErr
	})
	warnings, mu := capturePlannerWarnings(t)

	mi := NewMemoryIntegration()
	if err := mi.Emit(stdcontext.Background(), HookBeforeTaskExecute, &TaskExecutionContext{TaskID: "task-emit-1"}); err != nil {
		t.Fatalf("Emit returned %v, want nil: a hook failure must not travel back to the caller", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("the handler ran %d times, want 1", got)
	}
	got := plannerWarnings(warnings, mu)
	if len(got) != 1 || got[0].TaskID != "task-emit-1" {
		t.Fatalf("warnings = %+v, want exactly one for task-emit-1", got)
	}
}

// TestTaskHookFailureDoesNotFailUpdateTask 是「hook 失败绝不让 UpdateTask 失败」的
// 最小钉子：四个 hook 全部失败时，每一次 UpdateTask 都返回成功。
func TestTaskHookFailureDoesNotFailUpdateTask(t *testing.T) {
	svc, planID, blocker, dependent := seedPlanner(t)
	mgr := hookTestEnv(t)
	for _, hook := range []backendhooks.HookType{
		backendhooks.HookBeforeTaskExecute,
		backendhooks.HookAfterTaskExecute,
		backendhooks.HookOnTaskFailure,
		backendhooks.HookOnTaskComplete,
	} {
		registerTaskHook(t, mgr, hook, 0, failingHandler(errors.New("observer exploded")))
	}
	capturePlannerWarnings(t)

	ctx := stdcontext.Background()
	for _, tc := range []struct {
		taskID string
		status TaskStatus
	}{
		{blocker.ID, TaskStatusInProgress},
		{blocker.ID, TaskStatusCompleted},
		{dependent.ID, TaskStatusCancelled},
	} {
		updated, err := svc.UpdateTask(ctx, planID, tc.taskID, &TaskUpdateRequest{Status: tc.status})
		if err != nil {
			t.Fatalf("UpdateTask %s → %s returned %v, want nil", tc.taskID, tc.status, err)
		}
		if updated == nil || updated.Status != tc.status {
			t.Fatalf("UpdateTask %s → %s returned %+v, want the new status applied", tc.taskID, tc.status, updated)
		}
	}
}
