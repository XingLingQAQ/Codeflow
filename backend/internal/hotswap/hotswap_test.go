package hotswap

import (
	"context"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/adapters"
)

// mockAdapter 模拟适配器
type mockAdapter struct {
	history []adapters.Message
	config  adapters.AdapterConfig
}

func newMockAdapter() *mockAdapter {
	return &mockAdapter{
		history: make([]adapters.Message, 0),
		config:  adapters.DefaultAdapterConfig,
	}
}

func (a *mockAdapter) Send(ctx context.Context, prompt string, options *adapters.SendOptions) (*adapters.AIResponse, error) {
	return &adapters.AIResponse{Content: "mock response"}, nil
}

func (a *mockAdapter) Stream(ctx context.Context, prompt string, options *adapters.SendOptions) (<-chan adapters.StreamChunk, error) {
	ch := make(chan adapters.StreamChunk)
	close(ch)
	return ch, nil
}

func (a *mockAdapter) GetHistory() []adapters.Message {
	return a.history
}

func (a *mockAdapter) SetHistory(messages []adapters.Message) {
	a.history = messages
}

func (a *mockAdapter) ClearHistory() {
	a.history = make([]adapters.Message, 0)
}

func (a *mockAdapter) Rewind(steps int) error {
	if steps > len(a.history) {
		a.history = make([]adapters.Message, 0)
		return nil
	}
	a.history = a.history[:len(a.history)-steps]
	return nil
}

func (a *mockAdapter) Compact(ctx context.Context) error {
	return nil
}

func (a *mockAdapter) Configure(config *adapters.AdapterConfig) {
	if config != nil {
		a.config = *config
	}
}

func (a *mockAdapter) GetConfig() adapters.AdapterConfig {
	return a.config
}

func (a *mockAdapter) Close() error {
	return nil
}

func TestHotSwapManagerBasic(t *testing.T) {
	manager := NewHotSwapManager(nil)

	// 检查预定义模型
	models := manager.GetAvailableModels()
	if len(models) == 0 {
		t.Error("expected predefined models")
	}

	// 获取模型信息
	opus := manager.GetModelInfo("claude-3-opus")
	if opus == nil {
		t.Fatal("expected claude-3-opus model")
	}
	if opus.Provider != ProviderClaude {
		t.Errorf("expected provider claude, got %s", opus.Provider)
	}
	if opus.ContextWindow != 200000 {
		t.Errorf("expected context window 200000, got %d", opus.ContextWindow)
	}
}

func TestHotSwapManagerRegisterAdapter(t *testing.T) {
	manager := NewHotSwapManager(nil)

	// 初始没有适配器
	if manager.GetCurrentAdapter() != nil {
		t.Error("expected no current adapter initially")
	}

	// 注册适配器
	adapter := newMockAdapter()
	manager.RegisterAdapter("claude-3-opus", adapter)

	// 验证注册
	current := manager.GetCurrentAdapter()
	if current == nil {
		t.Fatal("expected current adapter after registration")
	}

	// 验证当前模型
	model := manager.GetCurrentModel()
	if model == nil {
		t.Fatal("expected current model")
	}
	if model.ID != "claude-3-opus" {
		t.Errorf("expected claude-3-opus, got %s", model.ID)
	}
}

func TestHotSwapManagerSwitchModel(t *testing.T) {
	manager := NewHotSwapManager(nil)

	// 注册两个适配器
	adapter1 := newMockAdapter()
	adapter1.SetHistory([]adapters.Message{
		{Role: adapters.RoleUser, Content: "Hello"},
		{Role: adapters.RoleAssistant, Content: "Hi there"},
	})
	manager.RegisterAdapter("claude-3-opus", adapter1)

	adapter2 := newMockAdapter()
	manager.RegisterAdapter("claude-3-sonnet", adapter2)

	// 切换模型
	result, err := manager.SwitchModel("claude-3-sonnet", &SwitchOptions{
		PreserveHistory: true,
		MigrateContext:  true,
	})
	if err != nil {
		t.Fatalf("switch: %v", err)
	}

	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
	if result.PreviousModel != "claude-3-opus" {
		t.Errorf("expected previous model claude-3-opus, got %s", result.PreviousModel)
	}
	if result.CurrentModel != "claude-3-sonnet" {
		t.Errorf("expected current model claude-3-sonnet, got %s", result.CurrentModel)
	}

	// 验证历史已迁移
	newHistory := adapter2.GetHistory()
	if len(newHistory) != 2 {
		t.Errorf("expected 2 messages in history, got %d", len(newHistory))
	}
}

func TestHotSwapManagerCanSwitch(t *testing.T) {
	manager := NewHotSwapManager(nil)

	// 没有适配器，不能切换
	if manager.CanSwitch("claude-3-opus") {
		t.Error("should not be able to switch without adapter")
	}

	// 注册适配器
	manager.RegisterAdapter("claude-3-opus", newMockAdapter())

	// 现在可以切换
	if !manager.CanSwitch("claude-3-opus") {
		t.Error("should be able to switch with adapter")
	}

	// 不存在的模型不能切换
	if manager.CanSwitch("nonexistent") {
		t.Error("should not be able to switch to nonexistent model")
	}
}

func TestHotSwapManagerRelay(t *testing.T) {
	manager := NewHotSwapManager(&HotSwapConfig{
		RelayConfig: RelayConfig{
			Enabled:       true,
			FallbackChain: []string{"claude-3-opus", "claude-3-sonnet", "gemini-pro"},
		},
	})

	// 注册所有适配器
	manager.RegisterAdapter("claude-3-opus", newMockAdapter())
	manager.RegisterAdapter("claude-3-sonnet", newMockAdapter())
	manager.RegisterAdapter("gemini-pro", newMockAdapter())

	// 当前是opus，接力到sonnet
	result, err := manager.Relay(nil)
	if err != nil {
		t.Fatalf("relay: %v", err)
	}

	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
	if result.CurrentModel != "claude-3-sonnet" {
		t.Errorf("expected claude-3-sonnet, got %s", result.CurrentModel)
	}

	// 再次接力到gemini-pro
	result, err = manager.Relay(nil)
	if err != nil {
		t.Fatalf("relay: %v", err)
	}

	if result.CurrentModel != "gemini-pro" {
		t.Errorf("expected gemini-pro, got %s", result.CurrentModel)
	}
}

func TestHotSwapManagerMigrateContext(t *testing.T) {
	manager := NewHotSwapManager(nil)

	// 注册适配器并设置历史
	adapter := newMockAdapter()
	adapter.SetHistory([]adapters.Message{
		{Role: adapters.RoleUser, Content: "Message 1"},
		{Role: adapters.RoleAssistant, Content: "Response 1"},
		{Role: adapters.RoleUser, Content: "Message 2"},
	})
	manager.RegisterAdapter("claude-3-opus", adapter)

	// 迁移上下文
	result, err := manager.MigrateContext("claude-3-sonnet")
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if !result.Success {
		t.Error("expected success")
	}
	if result.OriginalTokens == 0 {
		t.Error("expected non-zero original tokens")
	}
	if len(result.Messages) == 0 {
		t.Error("expected messages")
	}
}

func TestHotSwapManagerFailureTracking(t *testing.T) {
	manager := NewHotSwapManager(nil)
	manager.RegisterAdapter("claude-3-opus", newMockAdapter())

	// 初始失败次数应该是0
	if manager.GetFailureCount("claude-3-opus") != 0 {
		t.Error("expected 0 failures initially")
	}

	// 记录失败
	manager.RecordFailure("claude-3-opus")
	manager.RecordFailure("claude-3-opus")

	if manager.GetFailureCount("claude-3-opus") != 2 {
		t.Errorf("expected 2 failures, got %d", manager.GetFailureCount("claude-3-opus"))
	}

	// 重置
	manager.ResetFailureCount("claude-3-opus")
	if manager.GetFailureCount("claude-3-opus") != 0 {
		t.Error("expected 0 failures after reset")
	}
}

func TestHotSwapManagerRegisterModel(t *testing.T) {
	manager := NewHotSwapManager(nil)

	// 注册自定义模型
	customModel := &ModelInfo{
		ID:       "custom-model",
		Name:     "Custom Model",
		Provider: ProviderCustom,
		Capabilities: ModelCapabilities{
			Streaming: true,
		},
		ContextWindow:   50000,
		MaxOutputTokens: 2000,
		Available:       true,
		Status:          StatusOnline,
	}
	manager.RegisterModel(customModel)

	// 验证注册
	model := manager.GetModelInfo("custom-model")
	if model == nil {
		t.Fatal("expected custom model")
	}
	if model.Provider != ProviderCustom {
		t.Errorf("expected custom provider, got %s", model.Provider)
	}
}

func TestHotSwapManagerConfigure(t *testing.T) {
	manager := NewHotSwapManager(nil)

	// 配置新设置
	newConfig := &HotSwapConfig{
		DefaultModel:            "gemini-pro",
		AutoRetry:               false,
		ContextMigrationEnabled: false,
		MaxContextTokens:        50000,
		RetryStrategy: RetryStrategy{
			MaxRetries: 5,
			BaseDelay:  2 * time.Second,
		},
	}
	manager.Configure(newConfig)

	// 验证通过GetCurrentModel等方式间接验证配置已应用
	// 由于config是私有的，我们通过行为验证
}

func TestHotSwapManagerSwitchToUnavailable(t *testing.T) {
	manager := NewHotSwapManager(nil)

	// 注册一个模型但标记为不可用
	manager.RegisterModel(&ModelInfo{
		ID:        "unavailable-model",
		Name:      "Unavailable",
		Provider:  ProviderCustom,
		Available: false,
		Status:    StatusOffline,
	})
	manager.RegisterAdapter("unavailable-model", newMockAdapter())

	// 尝试切换应该失败
	if manager.CanSwitch("unavailable-model") {
		t.Error("should not be able to switch to unavailable model")
	}
}

func TestHotSwapManagerRetry(t *testing.T) {
	manager := NewHotSwapManager(&HotSwapConfig{
		RetryStrategy: RetryStrategy{
			MaxRetries:        3,
			BaseDelay:         10 * time.Millisecond,
			MaxDelay:          100 * time.Millisecond,
			BackoffMultiplier: 2,
		},
	})
	manager.RegisterAdapter("claude-3-opus", newMockAdapter())

	// 重试
	result, err := manager.Retry(nil)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}

	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
}
