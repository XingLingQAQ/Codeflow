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
	mergeAdapterConfig(&cfg, config)

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

	mergeAdapterConfig(&a.config, config)
	a.httpClient.Timeout = a.config.Timeout
}

func mergeAdapterConfig(dst *AdapterConfig, src *AdapterConfig) {
	if dst == nil || src == nil {
		return
	}
	if src.APIKey != "" {
		dst.APIKey = src.APIKey
	}
	if src.BaseURL != "" {
		dst.BaseURL = src.BaseURL
	}
	if src.Model != "" {
		dst.Model = src.Model
	}
	if src.ForceTemperature || src.Temperature != 0 {
		dst.Temperature = src.Temperature
	}
	if src.ForceMaxTokens || src.MaxTokens > 0 {
		dst.MaxTokens = src.MaxTokens
	}
	if src.ForceTimeout || src.Timeout > 0 {
		dst.Timeout = src.Timeout
	}
	if src.ForceMaxRetries || src.MaxRetries > 0 {
		dst.MaxRetries = src.MaxRetries
	}
	if src.ForceRetryDelay || src.RetryDelay > 0 {
		dst.RetryDelay = src.RetryDelay
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

type requestControls struct {
	System      string
	Semantics   *RequestSemantics
	Model       string
	Temperature *float64
	MaxTokens   int
}

func resolveSendControls(config AdapterConfig, options *SendOptions) requestControls {
	return requestControls{
		System:      pickSystem(options),
		Semantics:   pickSemantics(options),
		Model:       pickModel(config.Model, options),
		Temperature: pickTemperature(config.Temperature, options),
		MaxTokens:   pickMaxTokens(config.MaxTokens, options),
	}
}

func resolveToolTurnControls(config AdapterConfig, req *ToolTurnRequest) requestControls {
	if req == nil {
		return requestControls{
			Model:       config.Model,
			Temperature: pickTemperature(config.Temperature, nil),
			MaxTokens:   config.MaxTokens,
		}
	}
	return requestControls{
		System:      pickToolTurnSystem(req),
		Semantics:   pickToolTurnSemantics(req),
		Model:       firstNonEmpty(req.Model, config.Model),
		Temperature: pickTemperatureFromRequest(req.Temperature, config.Temperature),
		MaxTokens:   positiveOrDefault(req.MaxTokens, config.MaxTokens),
	}
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

type claudeContentBlock struct {
	Type      string                 `json:"type"`
	Text      string                 `json:"text,omitempty"`
	ID        string                 `json:"id,omitempty"`
	Name      string                 `json:"name,omitempty"`
	Input     map[string]any         `json:"input,omitempty"`
	ToolUseID string                 `json:"tool_use_id,omitempty"`
	Content   any                    `json:"content,omitempty"`
	IsError   bool                   `json:"is_error,omitempty"`
}

// claudeRequest Claude API请求
type claudeRequest struct {
	Model       string                   `json:"model"`
	Messages    []map[string]interface{} `json:"messages"`
	System      string                   `json:"system,omitempty"`
	Tools       []map[string]interface{} `json:"tools,omitempty"`
	MaxTokens   int                      `json:"max_tokens"`
	Temperature float64                  `json:"temperature,omitempty"`
	Stream      bool                     `json:"stream,omitempty"`
}

// claudeResponse Claude API响应
type claudeResponse struct {
	ID         string               `json:"id"`
	Type       string               `json:"type"`
	Role       string               `json:"role"`
	Content    []claudeContentBlock `json:"content"`
	Model      string               `json:"model"`
	StopReason string               `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// Send 发送消息
func (a *ClaudeAdapter) Send(ctx context.Context, prompt string, options *SendOptions) (*AIResponse, error) {
	userMsg := Message{
		Role:      RoleUser,
		Content:   prompt,
		Blocks:    []ContentBlock{{Type: "text", Text: prompt}},
		Timestamp: time.Now(),
	}
	a.AddMessage(userMsg)

	controls := resolveSendControls(a.config, options)
	resp, err := a.SendToolTurn(ctx, &ToolTurnRequest{
		Messages:    a.GetHistory(),
		System:      controls.System,
		Semantics:   controls.Semantics,
		Model:       controls.Model,
		MaxTokens:   controls.MaxTokens,
		Temperature: controls.Temperature,
	})
	if err != nil {
		return nil, err
	}
	a.AddMessage(resp.Message)

	return &AIResponse{
		Content:      resp.Content,
		Blocks:       cloneBlocks(resp.Blocks),
		Model:        resp.Model,
		Usage:        resp.Usage,
		FinishReason: resp.FinishReason,
	}, nil
}

// SendToolTurn 发送单轮原生工具协议请求
func (a *ClaudeAdapter) SendToolTurn(ctx context.Context, req *ToolTurnRequest) (*ToolTurnResponse, error) {
	if req == nil {
		req = &ToolTurnRequest{}
	}

	messages := req.Messages
	if len(messages) == 0 {
		messages = a.GetHistory()
	}
	controls := resolveToolTurnControls(a.config, req)

	reqBody := claudeRequest{
		Model:       controls.Model,
		Messages:    buildClaudeMessages(messages),
		System:      composeSystemInstruction(controls.System, controls.Semantics),
		Tools:       buildClaudeTools(req.Tools),
		MaxTokens:   controls.MaxTokens,
		Temperature: derefOrDefault(controls.Temperature, a.config.Temperature),
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

	blocks := fromClaudeBlocks(claudeResp.Content)
	content := extractTextFromBlocks(blocks)
	assistantMsg := Message{
		Role:      RoleAssistant,
		Content:   content,
		Blocks:    cloneBlocks(blocks),
		Timestamp: time.Now(),
	}

	if len(req.Messages) == 0 {
		a.AddMessage(assistantMsg)
	}

	return &ToolTurnResponse{
		Message: assistantMsg,
		Content: content,
		Blocks:  blocks,
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
	userMsg := Message{
		Role:      RoleUser,
		Content:   prompt,
		Blocks:    []ContentBlock{{Type: "text", Text: prompt}},
		Timestamp: time.Now(),
	}
	a.AddMessage(userMsg)

	controls := resolveSendControls(a.config, options)
	reqBody := claudeRequest{
		Model:       controls.Model,
		Messages:    buildClaudeMessages(a.GetHistory()),
		System:      composeSystemInstruction(controls.System, controls.Semantics),
		MaxTokens:   controls.MaxTokens,
		Temperature: derefOrDefault(controls.Temperature, a.config.Temperature),
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
				ch <- StreamChunk{Delta: event.Delta.Text, Index: index, Done: false}
				index++
			}
		}

		assistantMsg := Message{
			Role:      RoleAssistant,
			Content:   fullContent.String(),
			Blocks:    []ContentBlock{{Type: "text", Text: fullContent.String()}},
			Timestamp: time.Now(),
		}
		a.AddMessage(assistantMsg)

		ch <- StreamChunk{Delta: "", Index: index, Done: true}
	}()

	return ch, nil
}

// Compact 压缩历史
func (a *ClaudeAdapter) Compact(ctx context.Context) error {
	history := a.GetHistory()
	if len(history) <= 4 {
		return nil
	}

	a.SetHistory(history[len(history)-4:])
	return nil
}

func buildClaudeMessages(messages []Message) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(messages))
	for _, msg := range messages {
		out = append(out, map[string]interface{}{
			"role":    string(msg.Role),
			"content": buildClaudeMessageContent(msg),
		})
	}
	return out
}

func buildClaudeMessageContent(msg Message) any {
	if len(msg.Blocks) == 0 {
		return msg.Content
	}
	blocks := make([]map[string]interface{}, 0, len(msg.Blocks))
	for _, block := range msg.Blocks {
		blocks = append(blocks, toClaudeBlock(block))
	}
	return blocks
}

func toClaudeBlock(block ContentBlock) map[string]interface{} {
	out := map[string]interface{}{"type": block.Type}
	switch block.Type {
	case "text":
		out["text"] = block.Text
	case "tool_use":
		out["id"] = block.ID
		out["name"] = block.Name
		if block.Input == nil {
			out["input"] = map[string]any{}
		} else {
			out["input"] = block.Input
		}
	case "tool_result":
		out["tool_use_id"] = block.ToolUseID
		out["is_error"] = block.IsError
		out["content"] = block.Result
	default:
		if block.Text != "" {
			out["text"] = block.Text
		}
	}
	return out
}

func buildClaudeTools(tools []ToolDefinition) []map[string]interface{} {
	if len(tools) == 0 {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(tools))
	for _, tool := range tools {
		out = append(out, map[string]interface{}{
			"name":        tool.Name,
			"description": tool.Description,
			"input_schema": map[string]interface{}{
				"type":       tool.Parameters.Type,
				"properties": tool.Parameters.Properties,
				"required":   tool.Parameters.Required,
			},
		})
	}
	return out
}

func fromClaudeBlocks(blocks []claudeContentBlock) []ContentBlock {
	if len(blocks) == 0 {
		return nil
	}
	out := make([]ContentBlock, 0, len(blocks))
	for _, block := range blocks {
		item := ContentBlock{
			Type:      block.Type,
			Text:      block.Text,
			ID:        block.ID,
			Name:      block.Name,
			Input:     cloneMap(block.Input),
			ToolUseID: block.ToolUseID,
			IsError:   block.IsError,
		}
		if block.Type == "tool_result" {
			item.Result = normalizeClaudeToolResult(block.Content)
		}
		out = append(out, item)
	}
	return out
}

func normalizeClaudeToolResult(content any) string {
	switch v := content.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		payload, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(payload)
	}
}

func extractTextFromBlocks(blocks []ContentBlock) string {
	if len(blocks) == 0 {
		return ""
	}
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func cloneBlocks(blocks []ContentBlock) []ContentBlock {
	if len(blocks) == 0 {
		return nil
	}
	out := make([]ContentBlock, 0, len(blocks))
	for _, block := range blocks {
		out = append(out, ContentBlock{
			Type:      block.Type,
			Text:      block.Text,
			ID:        block.ID,
			Name:      block.Name,
			Input:     cloneMap(block.Input),
			ToolUseID: block.ToolUseID,
			Result:    block.Result,
			IsError:   block.IsError,
		})
	}
	return out
}

func cloneMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	out := make(map[string]any, len(input))
	for k, v := range input {
		out[k] = v
	}
	return out
}

func pickModel(defaultModel string, options *SendOptions) string {
	if options != nil && options.Model != "" {
		return options.Model
	}
	return defaultModel
}

func pickSystem(options *SendOptions) string {
	if options == nil {
		return ""
	}
	if options.System != "" {
		return options.System
	}
	if options.Semantics != nil {
		return options.Semantics.SystemPrompt
	}
	return ""
}

func pickToolTurnSystem(req *ToolTurnRequest) string {
	if req == nil {
		return ""
	}
	if req.System != "" {
		return req.System
	}
	if req.Semantics != nil {
		return req.Semantics.SystemPrompt
	}
	return ""
}

func pickSemantics(options *SendOptions) *RequestSemantics {
	if options == nil {
		return nil
	}
	return CloneRequestSemantics(options.Semantics)
}

func pickToolTurnSemantics(req *ToolTurnRequest) *RequestSemantics {
	if req == nil {
		return nil
	}
	return CloneRequestSemantics(req.Semantics)
}
func pickTemperatureFromRequest(value *float64, fallback float64) *float64 {
	if value != nil {
		return value
	}
	resolved := fallback
	return &resolved
}

func composeSystemInstruction(system string, semantics *RequestSemantics) string {
	base := strings.TrimSpace(system)
	if semantics == nil {
		return base
	}

	parts := make([]string, 0, 4)
	if base != "" {
		parts = append(parts, base)
	}
	if style := strings.TrimSpace(semantics.AnswerStyle); style != "" {
		parts = append(parts, "Answer style: "+style)
	}
	if len(semantics.Capabilities) > 0 {
		parts = append(parts, "Declared capabilities: "+strings.Join(semantics.Capabilities, ", "))
	}
	if controls := formatControlsDirective(semantics.Controls); controls != "" {
		parts = append(parts, controls)
	}
	return strings.Join(parts, "\n\n")
}

func formatControlsDirective(controls *RequestControls) string {
	if controls == nil {
		return ""
	}

	parts := make([]string, 0, 6)
	if controls.EnableTools != nil {
		parts = append(parts, fmt.Sprintf("tools enabled: %t", *controls.EnableTools))
	}
	if controls.EnableSkills != nil {
		parts = append(parts, fmt.Sprintf("skills enabled: %t", *controls.EnableSkills))
	}
	if controls.EnableHooks != nil {
		parts = append(parts, fmt.Sprintf("hooks enabled: %t", *controls.EnableHooks))
	}
	if len(controls.AllowedTools) > 0 {
		parts = append(parts, "allowed tools: "+strings.Join(controls.AllowedTools, ", "))
	}
	if len(controls.AllowedSkills) > 0 {
		parts = append(parts, "allowed skills: "+strings.Join(controls.AllowedSkills, ", "))
	}
	if len(controls.AllowedHooks) > 0 {
		parts = append(parts, "allowed hooks: "+strings.Join(controls.AllowedHooks, ", "))
	}
	if len(parts) == 0 {
		return ""
	}
	return "Runtime controls:\n- " + strings.Join(parts, "\n- ")
}

func pickMaxTokens(defaultMaxTokens int, options *SendOptions) int {
	if options != nil && options.MaxTokens > 0 {
		return options.MaxTokens
	}
	return defaultMaxTokens
}

func pickTemperature(defaultTemperature float64, options *SendOptions) *float64 {
	if options != nil && options.Temperature != nil {
		return options.Temperature
	}
	value := defaultTemperature
	return &value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func positiveOrDefault(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func derefOrDefault(value *float64, fallback float64) float64 {
	if value != nil {
		return *value
	}
	return fallback
}
