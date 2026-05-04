// Package hooks - Hook manager implementation
package hooks

import (
	"context"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/codeflow/backend/internal/audit"
	"github.com/google/uuid"
)

// HookManager implements IHookManager.
type HookManager struct {
	hooks  map[string]*Hook
	events []*HookEvent
	mu     sync.RWMutex
}

// NewHookManager creates a new hook manager.
func NewHookManager() *HookManager {
	return &HookManager{
		hooks:  make(map[string]*Hook),
		events: make([]*HookEvent, 0),
	}
}

// Register registers a new hook.
func (m *HookManager) Register(config HookConfig, handler HookFunc) error {
	if config.Name == "" {
		return fmt.Errorf("hook name cannot be empty")
	}
	if handler == nil {
		return fmt.Errorf("hook handler cannot be nil")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.hooks[config.Name]; exists {
		return fmt.Errorf("hook %s already registered", config.Name)
	}

	// Set defaults
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}
	if config.Metadata == nil {
		config.Metadata = make(map[string]interface{})
	}

	m.hooks[config.Name] = &Hook{
		Config:  config,
		Handler: handler,
	}

	return nil
}

// Unregister removes a hook by name.
func (m *HookManager) Unregister(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.hooks[name]; !exists {
		return fmt.Errorf("hook %s not found", name)
	}

	delete(m.hooks, name)
	return nil
}

// Enable enables a hook by name.
func (m *HookManager) Enable(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	hook, exists := m.hooks[name]
	if !exists {
		return fmt.Errorf("hook %s not found", name)
	}

	hook.Config.Enabled = true
	return nil
}

// Disable disables a hook by name.
func (m *HookManager) Disable(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	hook, exists := m.hooks[name]
	if !exists {
		return fmt.Errorf("hook %s not found", name)
	}

	hook.Config.Enabled = false
	return nil
}

// Trigger triggers all hooks of a specific type.
func (m *HookManager) Trigger(ctx context.Context, hookType HookType, payload interface{}) (interface{}, error) {
	m.mu.RLock()
	hooks := m.getHooksByType(hookType)
	m.mu.RUnlock()

	if len(hooks) == 0 {
		return payload, nil
	}

	// Sort by priority (lower number = higher priority)
	sort.Slice(hooks, func(i, j int) bool {
		return hooks[i].Config.Priority < hooks[j].Config.Priority
	})

	result := payload
	for _, hook := range hooks {
		if !hook.Config.Enabled {
			continue
		}

		event := &HookEvent{
			ID:        uuid.New().String(),
			HookName:  hook.Config.Name,
			HookType:  hookType,
			Timestamp: time.Now(),
			Metadata: map[string]interface{}{
				"priority":    hook.Config.Priority,
				"retry_count": hook.Config.RetryCount,
			},
		}

		// Execute with timeout
		hookCtx, cancel := context.WithTimeout(ctx, hook.Config.Timeout)
		defer cancel()

		start := time.Now()
		output, err := m.executeWithRetry(hookCtx, hook, result)
		event.Duration = time.Since(start)

		if err != nil {
			event.Success = false
			event.Error = err.Error()
			m.recordAuditEvent(hookCtx, event, hook.Config, false)
			m.recordEvent(event)
			return result, fmt.Errorf("hook %s failed: %w", hook.Config.Name, err)
		}

		event.Success = true
		result = output
		m.recordAuditEvent(hookCtx, event, hook.Config, true)
		m.recordEvent(event)
	}

	return result, nil
}

// TriggerHook triggers a specific hook by name.
func (m *HookManager) TriggerHook(ctx context.Context, name string, payload interface{}) (interface{}, error) {
	m.mu.RLock()
	hook, exists := m.hooks[name]
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("hook %s not found", name)
	}
	if !hook.Config.Enabled {
		return payload, nil
	}

	event := &HookEvent{
		ID:        uuid.New().String(),
		HookName:  hook.Config.Name,
		HookType:  hook.Config.Type,
		Timestamp: time.Now(),
		Metadata: map[string]interface{}{
			"priority":    hook.Config.Priority,
			"retry_count": hook.Config.RetryCount,
		},
	}

	hookCtx, cancel := context.WithTimeout(ctx, hook.Config.Timeout)
	defer cancel()

	start := time.Now()
	output, err := m.executeWithRetry(hookCtx, hook, payload)
	event.Duration = time.Since(start)

	if err != nil {
		event.Success = false
		event.Error = err.Error()
		m.recordAuditEvent(hookCtx, event, hook.Config, false)
		m.recordEvent(event)
		return payload, fmt.Errorf("hook %s failed: %w", hook.Config.Name, err)
	}

	event.Success = true
	m.recordAuditEvent(hookCtx, event, hook.Config, true)
	m.recordEvent(event)
	return output, nil
}

func (m *HookManager) recordAuditEvent(ctx context.Context, event *HookEvent, config HookConfig, success bool) {
	if event == nil {
		return
	}

	severity := audit.SeverityInfo
	outcome := audit.OutcomeSuccess
	if !success {
		severity = audit.SeverityError
		outcome = audit.OutcomeFailure
	}

	details := map[string]interface{}{
		"hook_name":     event.HookName,
		"hook_type":     string(event.HookType),
		"duration_ms":   float64(event.Duration.Microseconds()) / 1000.0,
		"priority":      config.Priority,
		"timeout_ms":    config.Timeout.Milliseconds(),
		"retry_count":   config.RetryCount,
		"input_size":    event.InputSize,
		"output_size":   event.OutputSize,
		"metadata_keys": sortedMapKeys(config.Metadata),
	}
	if event.Error != "" {
		details["error"] = event.Error
	}

	if _, err := audit.Record(ctx, &audit.AuditLogEntry{
		EventType: audit.EventHook,
		Severity:  severity,
		Actor:     audit.AuditActor{ID: event.HookName, Type: "service", Name: event.HookName},
		Resource: audit.AuditResource{
			Type: "hook",
			ID:   event.HookName,
			Name: event.HookName,
		},
		Action:  string(event.HookType),
		Outcome: outcome,
		Details: details,
	}); err != nil {
		log.Printf("[WARN] hook audit record failed: hook=%s action=%s err=%v", event.HookName, event.HookType, err)
	}
}

func sortedMapKeys(values map[string]interface{}) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// TriggerAsync triggers hooks asynchronously.
func (m *HookManager) TriggerAsync(ctx context.Context, hookType HookType, payload interface{}) error {
	go func() {
		_, _ = m.Trigger(ctx, hookType, payload)
	}()
	return nil
}

// executeWithRetry executes a hook with retry logic.
func (m *HookManager) executeWithRetry(ctx context.Context, hook *Hook, payload interface{}) (interface{}, error) {
	var lastErr error
	maxRetries := hook.Config.RetryCount
	if maxRetries < 0 {
		maxRetries = 0
	}

	for attempt := 0; attempt <= maxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		result, err := hook.Handler(ctx, payload)
		if err == nil {
			return result, nil
		}

		lastErr = err
		if attempt < maxRetries {
			// Exponential backoff
			backoff := time.Duration(attempt+1) * 100 * time.Millisecond
			time.Sleep(backoff)
		}
	}

	return nil, lastErr
}

// getHooksByType returns hooks of a specific type (caller must hold lock).
func (m *HookManager) getHooksByType(hookType HookType) []*Hook {
	hooks := make([]*Hook, 0)
	for _, hook := range m.hooks {
		if hook.Config.Type == hookType {
			hooks = append(hooks, hook)
		}
	}
	return hooks
}

// GetHook returns a hook by name.
func (m *HookManager) GetHook(name string) (*Hook, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	hook, exists := m.hooks[name]
	if !exists {
		return nil, fmt.Errorf("hook %s not found", name)
	}

	// Return a copy
	hookCopy := &Hook{
		Config:  hook.Config,
		Handler: hook.Handler,
	}
	return hookCopy, nil
}

// ListHooks returns all registered hooks.
func (m *HookManager) ListHooks() []*Hook {
	m.mu.RLock()
	defer m.mu.RUnlock()

	hooks := make([]*Hook, 0, len(m.hooks))
	for _, hook := range m.hooks {
		hookCopy := &Hook{
			Config:  hook.Config,
			Handler: hook.Handler,
		}
		hooks = append(hooks, hookCopy)
	}
	return hooks
}

// ListHooksByType returns hooks of a specific type.
func (m *HookManager) ListHooksByType(hookType HookType) []*Hook {
	m.mu.RLock()
	defer m.mu.RUnlock()

	hooks := make([]*Hook, 0)
	for _, hook := range m.hooks {
		if hook.Config.Type == hookType {
			hookCopy := &Hook{
				Config:  hook.Config,
				Handler: hook.Handler,
			}
			hooks = append(hooks, hookCopy)
		}
	}
	return hooks
}

// recordEvent records a hook execution event.
func (m *HookManager) recordEvent(event *HookEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.events = append(m.events, event)

	// Keep only last 1000 events
	if len(m.events) > 1000 {
		m.events = m.events[len(m.events)-1000:]
	}
}

// GetEvents returns hook execution events.
func (m *HookManager) GetEvents(limit int, offset int) []*HookEvent {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if offset >= len(m.events) {
		return []*HookEvent{}
	}

	end := offset + limit
	if end > len(m.events) {
		end = len(m.events)
	}

	// Return events in reverse chronological order
	events := make([]*HookEvent, 0, end-offset)
	for i := len(m.events) - 1 - offset; i >= len(m.events)-end; i-- {
		if i < 0 {
			break
		}
		eventCopy := *m.events[i]
		events = append(events, &eventCopy)
	}

	return events
}

// GetEventsByHook returns events for a specific hook.
func (m *HookManager) GetEventsByHook(hookName string, limit int, offset int) []*HookEvent {
	m.mu.RLock()
	defer m.mu.RUnlock()

	filtered := make([]*HookEvent, 0)
	for i := len(m.events) - 1; i >= 0; i-- {
		if m.events[i].HookName == hookName {
			filtered = append(filtered, m.events[i])
		}
	}

	if offset >= len(filtered) {
		return []*HookEvent{}
	}

	end := offset + limit
	if end > len(filtered) {
		end = len(filtered)
	}

	events := make([]*HookEvent, end-offset)
	for i := offset; i < end; i++ {
		eventCopy := *filtered[i]
		events[i-offset] = &eventCopy
	}

	return events
}

// ClearEvents clears all hook events.
func (m *HookManager) ClearEvents() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.events = make([]*HookEvent, 0)
	return nil
}

// UpdateConfig updates hook configuration.
func (m *HookManager) UpdateConfig(name string, config HookConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	hook, exists := m.hooks[name]
	if !exists {
		return fmt.Errorf("hook %s not found", name)
	}

	// Preserve handler and name
	config.Name = name
	hook.Config = config

	return nil
}

// Global hook manager instance
var defaultHookManager IHookManager

// GetHookManager returns the global hook manager instance.
func GetHookManager() IHookManager {
	if defaultHookManager == nil {
		defaultHookManager = NewHookManager()
	}
	return defaultHookManager
}

// SetHookManager sets the global hook manager instance (for testing).
func SetHookManager(manager IHookManager) {
	defaultHookManager = manager
}

// HasHookManager reports whether the global hook manager has been configured.
func HasHookManager() bool {
	return defaultHookManager != nil
}
