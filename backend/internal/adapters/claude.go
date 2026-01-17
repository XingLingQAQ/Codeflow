package adapters

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// BaseAdapter 基础适配器实现
type BaseAdapter struct {
	config     AdapterConfig
	history    []Message
	httpClient *http.Client
	mu         sync.RWMutex
}

// NewBaseAdapter 创建基础适配器
func NewBaseAdapter(config *AdapterConfig) *BaseAdapter {
	cfg := DefaultAdapterConfig
	if config != nil {
		if config.APIKey != "" {
			cfg.APIKey = config.APIKey
		}
		if config.BaseURL != "" {
			cfg.BaseURL = config.BaseURL
		}
		if config.Model != "" {
			cfg.Model = config.Model
		}
		if config.Temperature != 0 {
			cfg.Temperature = config.Temperature
		}
		if config.MaxTokens > 0 {
			cfg.MaxTokens = config.MaxTokens
		}
		if config.Timeout > 0 {
			cfg.Timeout = config.Timeout
		}
		if config.MaxRetries > 0 {
			cfg.MaxRetries = config.MaxRetries
		}
		if config.RetryDelay > 0 {
			cfg.RetryDelay = config.RetryDelay
		}
	}

	return &BaseAdapter{
		config:  cfg,
		history: make([]Message, 0),
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

// GetHistory 获取历史
func (a *BaseAdapter) GetHistory() []Message {
	a.mu.RLock()
	defer a.mu.RUnlock()

	history := make([]Message, len(a.history))
	copy(history, a.history)
	return history
}

// SetHistory 设置历史
func (a *BaseAdapter) SetHistory(messages []Message) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.history = make([]Message, len(messages))
	copy(a.history, messages)
}

// ClearHistory 清空历史
func (a *BaseAdapter) ClearHistory() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.history = make([]Message, 0)
}

// AddMessage 添加消息
func (a *BaseAdapter) AddMessage(msg Message) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.history = append(a.history, msg)
}

// Rewind 回退
func (a *BaseAdapter) Rewind(steps int) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if steps < 0 {
		return fmt.Errorf("steps must be non-negative")
	}
	if steps > len(a.history) {
		steps = len(a.history)
	}

	a.history = a.history[:len(a.history)-steps]
	return nil
}

// Configure 配置
func (a *BaseAdapter) Configure(config *AdapterConfig) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if config.APIKey != "" {
		a.config.APIKey = config.APIKey
	}
	if config.BaseURL != "" {
		a.config.BaseURL = config.BaseURL
	}
	if config.Model != "" {
		a.config.Model = config.Model
	}
	if config.Temperature != 0 {
		a.config.Temperature = config.Temperature
	}
	if config.MaxTokens > 0 {
		a.config.MaxTokens = config.MaxTokens
	}
	if config.Timeout > 0 {
		a.config.Timeout = config.Timeout
		a.httpClient.Timeout = config.Timeout
	}
	if config.MaxRetries > 0 {
		a.config.MaxRetries = config.MaxRetries
	}
	if config.RetryDelay > 0 {
		a.config.RetryDelay = config.RetryDelay
	}
}

// GetConfig 获取配置
func (a *BaseAdapter) GetConfig() AdapterConfig {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.config
}

// Close 关闭
func (a *BaseAdapter) Close() error {
	a.httpClient.CloseIdleConnections()
	return nil
}

// DoRequest 执行HTTP请求（带重试）
func (a *BaseAdapter) DoRequest(ctx context.Context, method, url string, body interface{}, headers map[string]string) (*http.Response, error) {
	var lastErr error

	for i := 0; i <= a.config.MaxRetries; i++ {
		resp, err := a.doRequestOnce(ctx, method, url, body, headers)
		if err == nil {
			return resp, nil
		}

		lastErr = err

		// 检查是否可重试
		if apiErr, ok := err.(*APIError); ok && !apiErr.Retryable {
			return nil, err
		}

		// 等待重试
		if i < a.config.MaxRetries {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(a.config.RetryDelay * time.Duration(i+1)):
			}
		}
	}

	return nil, lastErr
}

func (a *BaseAdapter) doRequestOnce(ctx context.Context, method, url string, body interface{}, headers map[string]string) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, &TimeoutError{Message: "request timeout"}
		}
		return nil, NewAPIError(err.Error(), 0, "", true)
	}

	// 检查状态码
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		retryable := resp.StatusCode >= 500 || resp.StatusCode == 429
		return nil, NewAPIError(
			fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body)),
			resp.StatusCode,
			"",
			retryable,
		)
	}

	return resp, nil
}

// ClaudeAdapter Claude适配器
type ClaudeAdapter struct {
	*BaseAdapter
}

// NewClaudeAdapter 创建Claude适配器
func NewClaudeAdapter(config *AdapterConfig) *ClaudeAdapter {
	if config != nil && config.BaseURL == "" {
		config.BaseURL = "https://api.anthropic.com"
	}
	return &ClaudeAdapter{
		BaseAdapter: NewBaseAdapter(config),
	}
}

// claudeRequest Claude API请求
type claudeRequest struct {
	Model       string                   `json:"model"`
	Messages    []map[string]interface{} `json:"messages"`
	MaxTokens   int                      `json:"max_tokens"`
	Temperature float64                  `json:"temperature,omitempty"`
	Stream      bool                     `json:"stream,omitempty"`
}

// claudeResponse Claude API响应
type claudeResponse struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Model      string `json:"model"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// Send 发送消息
func (a *ClaudeAdapter) Send(ctx context.Context, prompt string, options *SendOptions) (*AIResponse, error) {
	// 添加用户消息
	userMsg := Message{
		Role:      RoleUser,
		Content:   prompt,
		Timestamp: time.Now(),
	}
	a.AddMessage(userMsg)

	// 构建请求
	model := a.config.Model
	temperature := a.config.Temperature
	maxTokens := a.config.MaxTokens

	if options != nil {
		if options.Model != "" {
			model = options.Model
		}
		if options.Temperature != nil {
			temperature = *options.Temperature
		}
		if options.MaxTokens > 0 {
			maxTokens = options.MaxTokens
		}
	}

	messages := make([]map[string]interface{}, 0, len(a.history))
	for _, msg := range a.GetHistory() {
		messages = append(messages, map[string]interface{}{
			"role":    string(msg.Role),
			"content": msg.Content,
		})
	}

	reqBody := claudeRequest{
		Model:       model,
		Messages:    messages,
		MaxTokens:   maxTokens,
		Temperature: temperature,
		Stream:      false,
	}

	url := fmt.Sprintf("%s/v1/messages", a.config.BaseURL)
	headers := map[string]string{
		"x-api-key":         a.config.APIKey,
		"anthropic-version": "2023-06-01",
	}

	resp, err := a.DoRequest(ctx, "POST", url, reqBody, headers)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var claudeResp claudeResponse
	if err := json.NewDecoder(resp.Body).Decode(&claudeResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	content := ""
	if len(claudeResp.Content) > 0 && claudeResp.Content[0].Type == "text" {
		content = claudeResp.Content[0].Text
	}

	// 添加助手消息
	assistantMsg := Message{
		Role:      RoleAssistant,
		Content:   content,
		Timestamp: time.Now(),
	}
	a.AddMessage(assistantMsg)

	return &AIResponse{
		Content: content,
		Model:   claudeResp.Model,
		Usage: Usage{
			PromptTokens:     claudeResp.Usage.InputTokens,
			CompletionTokens: claudeResp.Usage.OutputTokens,
			TotalTokens:      claudeResp.Usage.InputTokens + claudeResp.Usage.OutputTokens,
		},
		FinishReason: claudeResp.StopReason,
	}, nil
}

// Stream 流式发送
func (a *ClaudeAdapter) Stream(ctx context.Context, prompt string, options *SendOptions) (<-chan StreamChunk, error) {
	// 添加用户消息
	userMsg := Message{
		Role:      RoleUser,
		Content:   prompt,
		Timestamp: time.Now(),
	}
	a.AddMessage(userMsg)

	// 构建请求
	model := a.config.Model
	temperature := a.config.Temperature
	maxTokens := a.config.MaxTokens

	if options != nil {
		if options.Model != "" {
			model = options.Model
		}
		if options.Temperature != nil {
			temperature = *options.Temperature
		}
		if options.MaxTokens > 0 {
			maxTokens = options.MaxTokens
		}
	}

	messages := make([]map[string]interface{}, 0, len(a.history))
	for _, msg := range a.GetHistory() {
		messages = append(messages, map[string]interface{}{
			"role":    string(msg.Role),
			"content": msg.Content,
		})
	}

	reqBody := claudeRequest{
		Model:       model,
		Messages:    messages,
		MaxTokens:   maxTokens,
		Temperature: temperature,
		Stream:      true,
	}

	url := fmt.Sprintf("%s/v1/messages", a.config.BaseURL)
	headers := map[string]string{
		"x-api-key":         a.config.APIKey,
		"anthropic-version": "2023-06-01",
	}

	resp, err := a.DoRequest(ctx, "POST", url, reqBody, headers)
	if err != nil {
		return nil, err
	}

	ch := make(chan StreamChunk, 100)

	go func() {
		defer close(ch)
		defer resp.Body.Close()

		var fullContent strings.Builder
		index := 0

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				break
			}

			var event struct {
				Type  string `json:"type"`
				Delta struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"delta"`
			}

			if err := json.Unmarshal([]byte(data), &event); err != nil {
				continue
			}

			if event.Type == "content_block_delta" && event.Delta.Type == "text_delta" {
				fullContent.WriteString(event.Delta.Text)
				ch <- StreamChunk{
					Delta: event.Delta.Text,
					Index: index,
					Done:  false,
				}
				index++
			}
		}

		// 添加完整响应到历史
		assistantMsg := Message{
			Role:      RoleAssistant,
			Content:   fullContent.String(),
			Timestamp: time.Now(),
		}
		a.AddMessage(assistantMsg)

		ch <- StreamChunk{
			Delta: "",
			Index: index,
			Done:  true,
		}
	}()

	return ch, nil
}

// Compact 压缩历史
func (a *ClaudeAdapter) Compact(ctx context.Context) error {
	history := a.GetHistory()
	if len(history) <= 4 {
		return nil
	}

	// 保留最近4条消息
	a.SetHistory(history[len(history)-4:])
	return nil
}
