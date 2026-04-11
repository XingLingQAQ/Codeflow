package adapters

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewAdapterFactoryClaude(t *testing.T) {
	adapter, err := NewAdapter(ProviderClaude, &AdapterConfig{Model: "claude-3-5-sonnet"})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}

	claudeAdapter, ok := adapter.(*ClaudeAdapter)
	if !ok {
		t.Fatalf("expected *ClaudeAdapter, got %T", adapter)
	}

	cfg := claudeAdapter.GetConfig()
	if cfg.BaseURL != "https://api.anthropic.com" {
		t.Fatalf("expected default Claude baseURL, got %s", cfg.BaseURL)
	}
}

func TestNewAdapterFactoryDefaultsEmptyProviderToClaude(t *testing.T) {
	adapter, err := NewAdapter("", &AdapterConfig{Model: "claude-3-5-sonnet"})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	if _, ok := adapter.(*ClaudeAdapter); !ok {
		t.Fatalf("expected *ClaudeAdapter, got %T", adapter)
	}
}

func TestNewAdapterFactoryUnsupportedProvider(t *testing.T) {
	adapter, err := NewAdapter(ProviderGemini, &AdapterConfig{Model: "gemini-pro"})
	if err == nil {
		t.Fatal("expected error for unimplemented provider")
	}
	if adapter != nil {
		t.Fatalf("expected nil adapter on error, got %T", adapter)
	}
}

func TestNormalizeProviderAliases(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    Provider
		wantErr bool
	}{
		{name: "empty defaults to claude", raw: "", want: ProviderClaude},
		{name: "anthropic maps to claude", raw: "anthropic", want: ProviderClaude},
		{name: "custom maps to claude", raw: "custom", want: ProviderClaude},
		{name: "google maps to gemini", raw: "google", want: ProviderGemini},
		{name: "openai preserved", raw: "openai", want: ProviderOpenAI},
		{name: "unsupported rejected", raw: "unknown", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeProvider(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeProvider: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestBaseAdapterHistory(t *testing.T) {
	adapter := NewBaseAdapter(nil)

	// 初始应该为空
	history := adapter.GetHistory()
	if len(history) != 0 {
		t.Errorf("expected empty history, got %d", len(history))
	}

	// 添加消息
	msg1 := Message{Role: RoleUser, Content: "Hello", Timestamp: time.Now()}
	msg2 := Message{Role: RoleAssistant, Content: "Hi there", Timestamp: time.Now()}
	adapter.AddMessage(msg1)
	adapter.AddMessage(msg2)

	history = adapter.GetHistory()
	if len(history) != 2 {
		t.Errorf("expected 2 messages, got %d", len(history))
	}

	// 设置历史
	newHistory := []Message{
		{Role: RoleUser, Content: "New message"},
	}
	adapter.SetHistory(newHistory)

	history = adapter.GetHistory()
	if len(history) != 1 {
		t.Errorf("expected 1 message after set, got %d", len(history))
	}

	// 清空历史
	adapter.ClearHistory()
	history = adapter.GetHistory()
	if len(history) != 0 {
		t.Errorf("expected 0 messages after clear, got %d", len(history))
	}
}

func TestBaseAdapterRewind(t *testing.T) {
	adapter := NewBaseAdapter(nil)

	// 添加5条消息
	for i := 0; i < 5; i++ {
		adapter.AddMessage(Message{Role: RoleUser, Content: "msg"})
	}

	// 回退2步
	if err := adapter.Rewind(2); err != nil {
		t.Fatalf("rewind: %v", err)
	}

	history := adapter.GetHistory()
	if len(history) != 3 {
		t.Errorf("expected 3 messages after rewind 2, got %d", len(history))
	}

	// 回退超过长度
	adapter.Rewind(10)
	history = adapter.GetHistory()
	if len(history) != 0 {
		t.Errorf("expected 0 messages after rewind all, got %d", len(history))
	}

	// 负数应该报错
	if err := adapter.Rewind(-1); err == nil {
		t.Error("expected error for negative rewind")
	}
}

func TestBaseAdapterConfig(t *testing.T) {
	config := &AdapterConfig{
		APIKey:      "test-key",
		Model:       "test-model",
		Temperature: 0.5,
		MaxTokens:   2000,
		Timeout:     30 * time.Second,
	}
	adapter := NewBaseAdapter(config)

	cfg := adapter.GetConfig()
	if cfg.APIKey != "test-key" {
		t.Errorf("expected test-key, got %s", cfg.APIKey)
	}
	if cfg.Model != "test-model" {
		t.Errorf("expected test-model, got %s", cfg.Model)
	}
	if cfg.Temperature != 0.5 {
		t.Errorf("expected temperature 0.5, got %f", cfg.Temperature)
	}

	// 修改配置
	adapter.Configure(&AdapterConfig{
		Model:       "new-model",
		Temperature: 0.8,
	})

	cfg = adapter.GetConfig()
	if cfg.Model != "new-model" {
		t.Errorf("expected new-model, got %s", cfg.Model)
	}
	if cfg.Temperature != 0.8 {
		t.Errorf("expected temperature 0.8, got %f", cfg.Temperature)
	}
}

func TestClaudeAdapterSend(t *testing.T) {
	// 创建模拟服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证请求
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/messages" {
			t.Errorf("expected /v1/messages, got %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("expected test-key, got %s", r.Header.Get("x-api-key"))
		}

		// 解析请求体
		var req claudeRequest
		json.NewDecoder(r.Body).Decode(&req)

		if req.Model != "claude-3-5-sonnet" {
			t.Errorf("expected claude-3-5-sonnet, got %s", req.Model)
		}

		// 返回模拟响应
		resp := claudeResponse{
			ID:   "msg_001",
			Type: "message",
			Role: "assistant",
			Content: []claudeContentBlock{
				{Type: "text", Text: "Hello! How can I help you?"},
			},
			Model:      "claude-3-5-sonnet",
			StopReason: "end_turn",
			Usage: struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			}{
				InputTokens:  10,
				OutputTokens: 15,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	adapter := NewClaudeAdapter(&AdapterConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "claude-3-5-sonnet",
	})

	ctx := context.Background()
	response, err := adapter.Send(ctx, "Hello", nil)
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	if response.Content != "Hello! How can I help you?" {
		t.Errorf("expected response content, got %s", response.Content)
	}
	if response.Model != "claude-3-5-sonnet" {
		t.Errorf("expected model claude-3-5-sonnet, got %s", response.Model)
	}
	if response.Usage.TotalTokens != 25 {
		t.Errorf("expected 25 total tokens, got %d", response.Usage.TotalTokens)
	}

	// 验证历史
	history := adapter.GetHistory()
	if len(history) != 2 {
		t.Errorf("expected 2 messages in history, got %d", len(history))
	}
	if history[0].Role != RoleUser {
		t.Errorf("expected first message to be user")
	}
	if history[1].Role != RoleAssistant {
		t.Errorf("expected second message to be assistant")
	}
}

func TestClaudeAdapterRetry(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("Service unavailable"))
			return
		}

		resp := claudeResponse{
			ID:   "msg_001",
			Type: "message",
			Role: "assistant",
			Content: []claudeContentBlock{
				{Type: "text", Text: "Success after retry"},
			},
			Model: "claude-3-5-sonnet",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	adapter := NewClaudeAdapter(&AdapterConfig{
		APIKey:     "test-key",
		BaseURL:    server.URL,
		Model:      "claude-3-5-sonnet",
		MaxRetries: 3,
		RetryDelay: 10 * time.Millisecond,
	})

	ctx := context.Background()
	response, err := adapter.Send(ctx, "Hello", nil)
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	if response.Content != "Success after retry" {
		t.Errorf("expected success response, got %s", response.Content)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestClaudeAdapterCompact(t *testing.T) {
	adapter := NewClaudeAdapter(&AdapterConfig{
		APIKey: "test-key",
		Model:  "claude-3-5-sonnet",
	})

	// 添加10条消息
	for i := 0; i < 10; i++ {
		adapter.AddMessage(Message{Role: RoleUser, Content: "msg"})
	}

	ctx := context.Background()
	if err := adapter.Compact(ctx); err != nil {
		t.Fatalf("compact: %v", err)
	}

	history := adapter.GetHistory()
	if len(history) != 4 {
		t.Errorf("expected 4 messages after compact, got %d", len(history))
	}
}

func TestAPIError(t *testing.T) {
	err := NewAPIError("test error", 500, "internal_error", true)

	if err.Error() != "test error" {
		t.Errorf("expected 'test error', got %s", err.Error())
	}
	if err.StatusCode != 500 {
		t.Errorf("expected status 500, got %d", err.StatusCode)
	}
	if !err.Retryable {
		t.Error("expected retryable to be true")
	}
}

func TestTimeoutError(t *testing.T) {
	err := &TimeoutError{}
	if err.Error() != "request timeout" {
		t.Errorf("expected 'request timeout', got %s", err.Error())
	}

	err = &TimeoutError{Message: "custom timeout"}
	if err.Error() != "custom timeout" {
		t.Errorf("expected 'custom timeout', got %s", err.Error())
	}
}

func TestDefaultConfig(t *testing.T) {
	adapter := NewBaseAdapter(nil)
	cfg := adapter.GetConfig()

	if cfg.Model != "claude-3-5-sonnet-20241022" {
		t.Errorf("expected default model, got %s", cfg.Model)
	}
	if cfg.Temperature != 1.0 {
		t.Errorf("expected temperature 1.0, got %f", cfg.Temperature)
	}
	if cfg.MaxTokens != 4096 {
		t.Errorf("expected max tokens 4096, got %d", cfg.MaxTokens)
	}
	if cfg.MaxRetries != 3 {
		t.Errorf("expected max retries 3, got %d", cfg.MaxRetries)
	}
}
