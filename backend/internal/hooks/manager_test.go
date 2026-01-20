// Package hooks - Hook manager tests
package hooks

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestHookManager_Register(t *testing.T) {
	mgr := NewHookManager()

	config := HookConfig{
		Name:    "test_hook",
		Type:    HookBeforeSend,
		Enabled: true,
	}

	handler := func(ctx context.Context, payload interface{}) (interface{}, error) {
		return payload, nil
	}

	err := mgr.Register(config, handler)
	assert.NoError(t, err)

	// Test duplicate registration
	err = mgr.Register(config, handler)
	assert.Error(t, err)
}

func TestHookManager_RegisterValidation(t *testing.T) {
	mgr := NewHookManager()

	// Test empty name
	err := mgr.Register(HookConfig{Type: HookBeforeSend}, func(ctx context.Context, payload interface{}) (interface{}, error) {
		return payload, nil
	})
	assert.Error(t, err)

	// Test nil handler
	err = mgr.Register(HookConfig{Name: "test", Type: HookBeforeSend}, nil)
	assert.Error(t, err)
}

func TestHookManager_Unregister(t *testing.T) {
	mgr := NewHookManager()

	config := HookConfig{
		Name:    "test_hook",
		Type:    HookBeforeSend,
		Enabled: true,
	}

	handler := func(ctx context.Context, payload interface{}) (interface{}, error) {
		return payload, nil
	}

	mgr.Register(config, handler)

	err := mgr.Unregister("test_hook")
	assert.NoError(t, err)

	// Test unregister non-existent hook
	err = mgr.Unregister("nonexistent")
	assert.Error(t, err)
}

func TestHookManager_EnableDisable(t *testing.T) {
	mgr := NewHookManager()

	config := HookConfig{
		Name:    "test_hook",
		Type:    HookBeforeSend,
		Enabled: true,
	}

	handler := func(ctx context.Context, payload interface{}) (interface{}, error) {
		return payload, nil
	}

	mgr.Register(config, handler)

	// Test disable
	err := mgr.Disable("test_hook")
	assert.NoError(t, err)

	hook, _ := mgr.GetHook("test_hook")
	assert.False(t, hook.Config.Enabled)

	// Test enable
	err = mgr.Enable("test_hook")
	assert.NoError(t, err)

	hook, _ = mgr.GetHook("test_hook")
	assert.True(t, hook.Config.Enabled)
}

func TestHookManager_Trigger(t *testing.T) {
	mgr := NewHookManager()

	callCount := 0
	handler := func(ctx context.Context, payload interface{}) (interface{}, error) {
		callCount++
		return payload, nil
	}

	config := HookConfig{
		Name:     "test_hook",
		Type:     HookBeforeSend,
		Enabled:  true,
		Priority: 1,
	}

	mgr.Register(config, handler)

	ctx := context.Background()
	result, err := mgr.Trigger(ctx, HookBeforeSend, "test_payload")

	assert.NoError(t, err)
	assert.Equal(t, "test_payload", result)
	assert.Equal(t, 1, callCount)
}

func TestHookManager_TriggerMultipleHooks(t *testing.T) {
	mgr := NewHookManager()

	var executionOrder []string

	handler1 := func(ctx context.Context, payload interface{}) (interface{}, error) {
		executionOrder = append(executionOrder, "hook1")
		return payload, nil
	}

	handler2 := func(ctx context.Context, payload interface{}) (interface{}, error) {
		executionOrder = append(executionOrder, "hook2")
		return payload, nil
	}

	handler3 := func(ctx context.Context, payload interface{}) (interface{}, error) {
		executionOrder = append(executionOrder, "hook3")
		return payload, nil
	}

	mgr.Register(HookConfig{Name: "hook1", Type: HookBeforeSend, Enabled: true, Priority: 2}, handler1)
	mgr.Register(HookConfig{Name: "hook2", Type: HookBeforeSend, Enabled: true, Priority: 1}, handler2)
	mgr.Register(HookConfig{Name: "hook3", Type: HookBeforeSend, Enabled: true, Priority: 3}, handler3)

	ctx := context.Background()
	_, err := mgr.Trigger(ctx, HookBeforeSend, "test")

	assert.NoError(t, err)
	assert.Equal(t, []string{"hook2", "hook1", "hook3"}, executionOrder)
}

func TestHookManager_TriggerDisabledHook(t *testing.T) {
	mgr := NewHookManager()

	callCount := 0
	handler := func(ctx context.Context, payload interface{}) (interface{}, error) {
		callCount++
		return payload, nil
	}

	config := HookConfig{
		Name:    "test_hook",
		Type:    HookBeforeSend,
		Enabled: false, // Disabled
	}

	mgr.Register(config, handler)

	ctx := context.Background()
	_, err := mgr.Trigger(ctx, HookBeforeSend, "test")

	assert.NoError(t, err)
	assert.Equal(t, 0, callCount) // Should not be called
}

func TestHookManager_TriggerWithError(t *testing.T) {
	mgr := NewHookManager()

	handler := func(ctx context.Context, payload interface{}) (interface{}, error) {
		return nil, errors.New("hook error")
	}

	config := HookConfig{
		Name:    "test_hook",
		Type:    HookBeforeSend,
		Enabled: true,
	}

	mgr.Register(config, handler)

	ctx := context.Background()
	_, err := mgr.Trigger(ctx, HookBeforeSend, "test")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "hook error")
}

func TestHookManager_TriggerWithTimeout(t *testing.T) {
	mgr := NewHookManager()

	handler := func(ctx context.Context, payload interface{}) (interface{}, error) {
		select {
		case <-time.After(200 * time.Millisecond):
			return payload, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	config := HookConfig{
		Name:    "test_hook",
		Type:    HookBeforeSend,
		Enabled: true,
		Timeout: 50 * time.Millisecond,
	}

	mgr.Register(config, handler)

	ctx := context.Background()
	_, err := mgr.Trigger(ctx, HookBeforeSend, "test")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context deadline exceeded")
}

func TestHookManager_TriggerWithRetry(t *testing.T) {
	mgr := NewHookManager()

	attemptCount := 0
	handler := func(ctx context.Context, payload interface{}) (interface{}, error) {
		attemptCount++
		if attemptCount < 3 {
			return nil, errors.New("temporary error")
		}
		return payload, nil
	}

	config := HookConfig{
		Name:       "test_hook",
		Type:       HookBeforeSend,
		Enabled:    true,
		RetryCount: 3,
	}

	mgr.Register(config, handler)

	ctx := context.Background()
	result, err := mgr.Trigger(ctx, HookBeforeSend, "test")

	assert.NoError(t, err)
	assert.Equal(t, "test", result)
	assert.Equal(t, 3, attemptCount)
}

func TestHookManager_ListHooks(t *testing.T) {
	mgr := NewHookManager()

	handler := func(ctx context.Context, payload interface{}) (interface{}, error) {
		return payload, nil
	}

	mgr.Register(HookConfig{Name: "hook1", Type: HookBeforeSend, Enabled: true}, handler)
	mgr.Register(HookConfig{Name: "hook2", Type: HookPostResponse, Enabled: true}, handler)

	hooks := mgr.ListHooks()
	assert.Len(t, hooks, 2)
}

func TestHookManager_ListHooksByType(t *testing.T) {
	mgr := NewHookManager()

	handler := func(ctx context.Context, payload interface{}) (interface{}, error) {
		return payload, nil
	}

	mgr.Register(HookConfig{Name: "hook1", Type: HookBeforeSend, Enabled: true}, handler)
	mgr.Register(HookConfig{Name: "hook2", Type: HookBeforeSend, Enabled: true}, handler)
	mgr.Register(HookConfig{Name: "hook3", Type: HookPostResponse, Enabled: true}, handler)

	hooks := mgr.ListHooksByType(HookBeforeSend)
	assert.Len(t, hooks, 2)

	hooks = mgr.ListHooksByType(HookPostResponse)
	assert.Len(t, hooks, 1)
}

func TestHookManager_GetEvents(t *testing.T) {
	mgr := NewHookManager()

	handler := func(ctx context.Context, payload interface{}) (interface{}, error) {
		return payload, nil
	}

	config := HookConfig{
		Name:    "test_hook",
		Type:    HookBeforeSend,
		Enabled: true,
	}

	mgr.Register(config, handler)

	ctx := context.Background()
	mgr.Trigger(ctx, HookBeforeSend, "test1")
	mgr.Trigger(ctx, HookBeforeSend, "test2")

	events := mgr.GetEvents(10, 0)
	assert.Len(t, events, 2)
	assert.Equal(t, "test_hook", events[0].HookName)
}

func TestHookManager_GetEventsByHook(t *testing.T) {
	mgr := NewHookManager()

	handler := func(ctx context.Context, payload interface{}) (interface{}, error) {
		return payload, nil
	}

	mgr.Register(HookConfig{Name: "hook1", Type: HookBeforeSend, Enabled: true}, handler)
	mgr.Register(HookConfig{Name: "hook2", Type: HookBeforeSend, Enabled: true}, handler)

	ctx := context.Background()
	mgr.Trigger(ctx, HookBeforeSend, "test")

	events := mgr.GetEventsByHook("hook1", 10, 0)
	assert.Len(t, events, 1)
	assert.Equal(t, "hook1", events[0].HookName)
}

func TestHookManager_ClearEvents(t *testing.T) {
	mgr := NewHookManager()

	handler := func(ctx context.Context, payload interface{}) (interface{}, error) {
		return payload, nil
	}

	config := HookConfig{
		Name:    "test_hook",
		Type:    HookBeforeSend,
		Enabled: true,
	}

	mgr.Register(config, handler)

	ctx := context.Background()
	mgr.Trigger(ctx, HookBeforeSend, "test")

	events := mgr.GetEvents(10, 0)
	assert.Len(t, events, 1)

	err := mgr.ClearEvents()
	assert.NoError(t, err)

	events = mgr.GetEvents(10, 0)
	assert.Len(t, events, 0)
}

func TestHookManager_UpdateConfig(t *testing.T) {
	mgr := NewHookManager()

	handler := func(ctx context.Context, payload interface{}) (interface{}, error) {
		return payload, nil
	}

	config := HookConfig{
		Name:     "test_hook",
		Type:     HookBeforeSend,
		Enabled:  true,
		Priority: 1,
	}

	mgr.Register(config, handler)

	newConfig := HookConfig{
		Name:     "test_hook",
		Type:     HookBeforeSend,
		Enabled:  false,
		Priority: 5,
	}

	err := mgr.UpdateConfig("test_hook", newConfig)
	assert.NoError(t, err)

	hook, _ := mgr.GetHook("test_hook")
	assert.False(t, hook.Config.Enabled)
	assert.Equal(t, 5, hook.Config.Priority)
}

func TestHookManager_TriggerAsync(t *testing.T) {
	mgr := NewHookManager()

	callCount := 0
	handler := func(ctx context.Context, payload interface{}) (interface{}, error) {
		time.Sleep(50 * time.Millisecond)
		callCount++
		return payload, nil
	}

	config := HookConfig{
		Name:    "test_hook",
		Type:    HookBeforeSend,
		Enabled: true,
	}

	mgr.Register(config, handler)

	ctx := context.Background()
	err := mgr.TriggerAsync(ctx, HookBeforeSend, "test")

	assert.NoError(t, err)
	assert.Equal(t, 0, callCount) // Should not be called yet

	// Wait for async execution
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 1, callCount)
}

func TestHookTypes(t *testing.T) {
	// Test all hook types are defined
	types := []HookType{
		HookBeforeSend,
		HookPostResponse,
		HookOnStream,
		HookBeforeCompress,
		HookOnMessageComplete,
		HookAfterExec,
		HookRestoreState,
		HookOnUserInputSubmitted,
	}

	assert.Len(t, types, 8)
}
