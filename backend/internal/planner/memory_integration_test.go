package planner

import (
	"context"
	"testing"
)

func TestNewMemoryIntegration(t *testing.T) {
	mi := NewMemoryIntegration()
	if mi == nil {
		t.Fatal("NewMemoryIntegration returned nil")
	}
	if len(mi.handlers) != 4 {
		t.Fatalf("expected 4 handler slots, got %d", len(mi.handlers))
	}
}

func TestMemoryIntegration_Register(t *testing.T) {
	mi := NewMemoryIntegration()
	called := false
	mi.Register(HookBeforeTaskExecute, func(ctx context.Context, data interface{}) error {
		called = true
		return nil
	})

	err := mi.Emit(context.Background(), HookBeforeTaskExecute, nil)
	if err != nil {
		t.Fatalf("Emit returned error: %v", err)
	}
	if !called {
		t.Fatal("handler was not called")
	}
}

func TestMemoryIntegration_EmitUnknownEvent(t *testing.T) {
	mi := NewMemoryIntegration()
	err := mi.Emit(context.Background(), TaskHookEvent("unknown"), nil)
	if err != nil {
		t.Fatalf("Emit for unknown event should not error, got: %v", err)
	}
}

func TestBuildTaskMemory_Completed(t *testing.T) {
	result := &TaskExecutionResult{
		TaskID:        "task-1",
		PlanID:        "plan-1",
		Title:         "测试任务",
		Status:        "completed",
		FilesModified: []string{"main.go"},
		DurationMs:    5000,
		SessionID:     "session-1",
	}

	record := BuildTaskMemory(result)
	if record == nil {
		t.Fatal("BuildTaskMemory returned nil")
	}
	if record.Importance != 0.7 {
		t.Errorf("expected importance 0.7, got %f", record.Importance)
	}
	if record.Source != "system" {
		t.Errorf("expected source 'system', got %s", record.Source)
	}
	if record.SessionID != "session-1" {
		t.Errorf("expected sessionID 'session-1', got %s", record.SessionID)
	}

	found := false
	for _, tag := range record.Tags {
		if tag == "task_complete" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected tag 'task_complete' in tags")
	}
}

func TestBuildTaskMemory_Failed(t *testing.T) {
	result := &TaskExecutionResult{
		TaskID:    "task-2",
		PlanID:    "plan-1",
		Title:     "失败任务",
		Status:    "failed",
		Error:     "编译错误",
		SessionID: "session-1",
	}

	record := BuildTaskMemory(result)
	if record.Importance != 0.5 {
		t.Errorf("expected importance 0.5 for failed task, got %f", record.Importance)
	}

	found := false
	for _, tag := range record.Tags {
		if tag == "task_failed" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected tag 'task_failed' in tags")
	}
}

func TestBuildFailureMemory(t *testing.T) {
	failCtx := &TaskFailureContext{
		TaskID:    "task-3",
		PlanID:    "plan-1",
		Title:     "失败任务",
		Error:     "类型错误",
		Phase:     "implement",
		SessionID: "session-1",
	}

	record := BuildFailureMemory(failCtx)
	if record == nil {
		t.Fatal("BuildFailureMemory returned nil")
	}
	if record.Importance != 0.8 {
		t.Errorf("expected importance 0.8, got %f", record.Importance)
	}

	foundPhase := false
	for _, tag := range record.Tags {
		if tag == "implement" {
			foundPhase = true
			break
		}
	}
	if !foundPhase {
		t.Error("expected phase tag 'implement' in tags")
	}
}

func TestBuildCompletionMemory(t *testing.T) {
	result := &TaskExecutionResult{
		TaskID:        "task-4",
		PlanID:        "plan-1",
		Title:         "完成任务",
		Status:        "completed",
		FilesModified: []string{"a.go", "b.go"},
		Output:        "测试通过",
		SessionID:     "session-1",
	}

	record := BuildCompletionMemory(result)
	if record == nil {
		t.Fatal("BuildCompletionMemory returned nil")
	}
	if record.Importance != 0.6 {
		t.Errorf("expected importance 0.6, got %f", record.Importance)
	}

	found := false
	for _, tag := range record.Tags {
		if tag == "task_complete" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected tag 'task_complete' in tags")
	}
}

func TestBuildCompletionMemory_LongOutput(t *testing.T) {
	longOutput := ""
	for i := 0; i < 300; i++ {
		longOutput += "x"
	}

	result := &TaskExecutionResult{
		TaskID:    "task-5",
		PlanID:    "plan-1",
		Title:     "长输出任务",
		Status:    "completed",
		Output:    longOutput,
		SessionID: "session-1",
	}

	record := BuildCompletionMemory(result)
	// Output should be truncated to 200 chars
	if len(record.Content) > 300 {
		t.Error("expected content to be truncated")
	}
}
