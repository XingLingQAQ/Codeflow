package hooks

// 本文件是 T1.07.c 的生产提取函数的单元测试（计划 §28 T1.07.c“审计按 event_id 去重”）：
// 触发记录里的 source_event_id / dedupe_key 只从两个保留 payload 契约取，其它 payload
// 与空事件 ID 一律不写键——空键绝不当成真键。

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/audit"
	"github.com/codeflow/backend/internal/policy"
	"github.com/codeflow/backend/internal/policy/policytesting"
	"github.com/codeflow/backend/internal/run"
	"github.com/stretchr/testify/assert"
)

// sourceEventTestIdentity 是一个形状合法的执行身份（工具事件要求 run+attempt+revision）。
func sourceEventTestIdentity() run.ExecutionIdentity {
	runID, attemptID, revisionID := "run-1", "attempt-1", "revision-1"
	return run.ExecutionIdentity{
		ProjectID:       "project-1",
		RunID:           &runID,
		AttemptID:       &attemptID,
		AgentRevisionID: &revisionID,
		Actor:           run.Actor{Type: run.ActorTypeAgent, ID: revisionID, Source: "claude_code"},
	}
}

func TestSourceEventReferenceExtractsTheContractEventID(t *testing.T) {
	tool := ToolHookPayload{EventID: "event-1"}
	runPayload := RunHookPayload{EventID: "claimed-1"}

	for _, tc := range []struct {
		name      string
		hook      HookType
		payload   HookPayload
		wantEvent string
		wantKey   string
	}{
		{"tool payload", HookPreToolUse, tool, "event-1", "hook_pre_tool_use:event-1"},
		{"tool payload pointer", HookPostToolUse, &tool, "event-1", "hook_post_tool_use:event-1"},
		{"run payload", HookRunStart, runPayload, "claimed-1", "hook_run_start:claimed-1"},
		{"run payload pointer", HookRunFinish, &runPayload, "claimed-1", "hook_run_finish:claimed-1"},
		{"nil tool pointer", HookPreToolUse, (*ToolHookPayload)(nil), "", ""},
		{"nil run pointer", HookRunStart, (*RunHookPayload)(nil), "", ""},
		{"empty event id", HookPreToolUse, ToolHookPayload{}, "", ""},
		{"non-payload value", HookBeforeSend, "payload", "", ""},
		{"map payload", HookAfterExec, map[string]any{"event_id": "event-9"}, "", ""},
		{"missing hook type", "", ToolHookPayload{EventID: "event-1"}, "event-1", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eventID, key := SourceEventReference(tc.hook, tc.payload)
			assert.Equal(t, tc.wantEvent, eventID)
			assert.Equal(t, tc.wantKey, key)
			if key != "" {
				// 去重键必须与适配器（runhooks 的单飞记忆）用的是同一个：<hook>:<event id>。
				assert.Equal(t, dedupeKey(tc.hook, eventID), key)
			}
		})
	}

	// payload 自己的 DedupeKey 与管理器的提取结果必须是同一个键（pre/post 是同一个
	// 工具调用的两个事件，键里带 hook 类型，所以两者不同）。
	assert.Equal(t, tool.DedupeKey(HookPreToolUse), "hook_pre_tool_use:event-1")
	_, fromPayload := SourceEventReference(HookPreToolUse, tool)
	assert.Equal(t, tool.DedupeKey(HookPreToolUse), fromPayload)
}

func TestAnnotateHookEventSkipsEmptyValues(t *testing.T) {
	metadata := HookMetadata{}
	annotateHookEvent(metadata, "", "")
	assert.NotContains(t, metadata, SourceEventIDKey)
	assert.NotContains(t, metadata, DedupeKeyKey)

	annotateHookEvent(metadata, "event-1", "hook_pre_tool_use:event-1")
	assert.Equal(t, "event-1", metadata[SourceEventIDKey])
	assert.Equal(t, "hook_pre_tool_use:event-1", metadata[DedupeKeyKey])

	// nil metadata 不炸（调用方没有建元数据）。
	annotateHookEvent(nil, "event-1", "hook_pre_tool_use:event-1")
}

// TestTriggerAuditCarriesTheSourceEventID 是 manager 侧的端到端单元：一次真实的 Trigger
// 写出恰好一条审计记录，Details 带 source_event_id 与 dedupe_key；没有来源事件的 payload
// 两者都不写。
func TestTriggerAuditCarriesTheSourceEventID(t *testing.T) {
	policytesting.AllowForTest(t, policy.OperationHookExecute)
	storage := audit.NewMemoryStorage()
	audit.SetAuditService(audit.NewAuditService(storage))
	t.Cleanup(func() { audit.SetAuditService(nil) })

	mgr := NewHookManager()
	if err := mgr.Register(HookConfig{
		Name:    "t107c_audit_pre",
		Type:    HookPreToolUse,
		Enabled: true,
		Timeout: time.Second,
	}, func(_ context.Context, payload HookPayload) (HookResult, error) {
		return payload, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	payload := ToolHookPayload{
		Identity:           sourceEventTestIdentity(),
		EventID:            "event-audit-1",
		ToolCallID:         "toolu_1",
		RequestFingerprint: "sha256:" + "0",
		Tool:               "read_file",
		Arguments:          []byte(`{"path":"a"}`),
	}
	if _, err := mgr.Trigger(context.Background(), HookPreToolUse, payload); err != nil {
		t.Fatalf("Trigger: %v", err)
	}

	entries, err := storage.Query(context.Background(), &audit.AuditQuery{EventTypes: []audit.AuditEventType{audit.EventHook}, Limit: 10})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("hook audit entries = %d, want 1", len(entries))
	}
	details := entries[0].Details
	assert.Equal(t, "event-audit-1", details[SourceEventIDKey])
	assert.Equal(t, "hook_pre_tool_use:event-audit-1", details[DedupeKeyKey])

	// 触发事件的元数据里也有这两个键（GetEvents 是管理器的触发记录视图）。
	events := mgr.GetEvents(10, 0)
	if len(events) != 1 {
		t.Fatalf("trigger events = %d, want 1", len(events))
	}
	assert.Equal(t, "event-audit-1", events[0].Metadata[SourceEventIDKey])
	assert.Equal(t, "hook_pre_tool_use:event-audit-1", events[0].Metadata[DedupeKeyKey])

	// 没有来源事件的 payload（历史 hook 的自定义结构）：两个键都不写，绝不出现空字符串。
	if err := mgr.Register(HookConfig{
		Name:    "t107c_audit_send",
		Type:    HookBeforeSend,
		Enabled: true,
		Timeout: time.Second,
	}, func(_ context.Context, payload HookPayload) (HookResult, error) {
		return payload, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := mgr.Trigger(context.Background(), HookBeforeSend, "plain payload"); err != nil {
		t.Fatalf("Trigger(before send): %v", err)
	}
	entries, err = storage.Query(context.Background(), &audit.AuditQuery{EventTypes: []audit.AuditEventType{audit.EventHook}, Limit: 10})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("hook audit entries = %d, want 2", len(entries))
	}
	var plain *audit.AuditLogEntry
	for i := range entries {
		if entries[i].Action == string(HookBeforeSend) {
			plain = &entries[i]
		}
	}
	if plain == nil {
		t.Fatal("the before-send audit entry is missing")
	}
	assert.NotContains(t, plain.Details, SourceEventIDKey)
	assert.NotContains(t, plain.Details, DedupeKeyKey)
}

// TestTriggerDoesNotDedupeItself 管理器不去重（去重在 runhooks.Port）：同一个 event id
// 触发两次会留下两条记录，两条都带着同一个 source_event_id —— 这正是读取方按 event_id
// 去重的依据，也是“有界记忆的边界”那条测试的 manager 侧对应事实。
func TestTriggerDoesNotDedupeItself(t *testing.T) {
	policytesting.AllowForTest(t, policy.OperationHookExecute)
	storage := audit.NewMemoryStorage()
	audit.SetAuditService(audit.NewAuditService(storage))
	t.Cleanup(func() { audit.SetAuditService(nil) })

	mgr := NewHookManager()
	calls := 0
	if err := mgr.Register(HookConfig{
		Name:    "t107c_no_dedupe",
		Type:    HookPreToolUse,
		Enabled: true,
		Timeout: time.Second,
	}, func(_ context.Context, payload HookPayload) (HookResult, error) {
		calls++
		return nil, errors.New("denied")
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	payload := ToolHookPayload{
		Identity:           sourceEventTestIdentity(),
		EventID:            "event-dup-1",
		ToolCallID:         "toolu_1",
		RequestFingerprint: "sha256:" + "0",
		Tool:               "read_file",
		Arguments:          []byte(`{"path":"a"}`),
	}
	for i := 0; i < 2; i++ {
		if _, err := mgr.Trigger(context.Background(), HookPreToolUse, payload); err == nil {
			t.Fatal("Trigger swallowed the handler failure")
		}
	}
	assert.Equal(t, 2, calls)
	entries, err := storage.Query(context.Background(), &audit.AuditQuery{EventTypes: []audit.AuditEventType{audit.EventHook}, Limit: 10})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("hook audit entries = %d, want 2 (the manager does not dedupe; Port.once does)", len(entries))
	}
	for _, entry := range entries {
		assert.Equal(t, "event-dup-1", entry.Details[SourceEventIDKey])
		assert.Equal(t, audit.OutcomeFailure, entry.Outcome)
	}
}

// TestTriggerHookByNameAuditCarriesTheSourceEventID 钉住按名字触发（TriggerHook）的
// 那条路径：它与 Trigger 各自构造触发记录，漏掉其中一处就会有一类审计记录不带来源
// 事件 ID（主 Agent 复核时的变异“TriggerHook 不写来源键”在补这条测试前无人发现）。
// 指针形式的 RunHookPayload 同样要取到 EventID。
func TestTriggerHookByNameAuditCarriesTheSourceEventID(t *testing.T) {
	policytesting.AllowForTest(t, policy.OperationHookExecute)
	storage := audit.NewMemoryStorage()
	audit.SetAuditService(audit.NewAuditService(storage))
	t.Cleanup(func() { audit.SetAuditService(nil) })

	mgr := NewHookManager()
	if err := mgr.Register(HookConfig{
		Name:    "t107c_audit_finish",
		Type:    HookRunFinish,
		Enabled: true,
		Timeout: time.Second,
	}, func(_ context.Context, payload HookPayload) (HookResult, error) {
		return payload, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	payload := &RunHookPayload{
		Identity:       sourceEventTestIdentity(),
		EventID:        "event-finish-1",
		Status:         run.RunStatusCompleted,
		PreviousStatus: run.RunStatusRunning,
	}
	if _, err := mgr.TriggerHook(context.Background(), "t107c_audit_finish", payload); err != nil {
		t.Fatalf("TriggerHook: %v", err)
	}

	entries, err := storage.Query(context.Background(), &audit.AuditQuery{EventTypes: []audit.AuditEventType{audit.EventHook}, Limit: 10})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("hook audit entries = %d, want 1", len(entries))
	}
	assert.Equal(t, "event-finish-1", entries[0].Details[SourceEventIDKey])
	assert.Equal(t, "hook_run_finish:event-finish-1", entries[0].Details[DedupeKeyKey])

	events := mgr.GetEvents(10, 0)
	if len(events) != 1 {
		t.Fatalf("trigger events = %d, want 1", len(events))
	}
	assert.Equal(t, "event-finish-1", events[0].Metadata[SourceEventIDKey])
	assert.Equal(t, "hook_run_finish:event-finish-1", events[0].Metadata[DedupeKeyKey])
}
