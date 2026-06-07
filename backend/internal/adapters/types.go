package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// JSONValue 表示可 JSON 编码的动态值边界。
type JSONValue any

// JSONObject 表示 JSON object payload，避免在适配器边界散落 map[string]interface{}。
type JSONObject map[string]JSONValue

// Role 消息角色
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
)

// ContentBlock 结构化消息块
type ContentBlock struct {
	Type      string     `json:"type"`
	Text      string     `json:"text,omitempty"`
	ID        string     `json:"id,omitempty"`
	Name      string     `json:"name,omitempty"`
	Input     JSONObject `json:"input,omitempty"`
	ToolUseID string     `json:"tool_use_id,omitempty"`
	Result    string     `json:"result,omitempty"`
	IsError   bool       `json:"is_error,omitempty"`
}

// Message 消息
type Message struct {
	Role      Role           `json:"role"`
	Content   string         `json:"content"`
	Blocks    []ContentBlock `json:"blocks,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
}

// Usage 使用统计
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// AIResponse AI响应
type AIResponse struct {
	Content      string         `json:"content"`
	Blocks       []ContentBlock `json:"blocks,omitempty"`
	Model        string         `json:"model"`
	Usage        Usage          `json:"usage"`
	FinishReason string         `json:"finish_reason,omitempty"`
}

// StreamChunk 流式响应块
type StreamChunk struct {
	Delta string `json:"delta"`
	Index int    `json:"index"`
	Done  bool   `json:"done"`
}

// RequestControls 统一请求控制面。
type RequestControls struct {
	AllowedTools  []string `json:"allowed_tools,omitempty"`
	AllowedSkills []string `json:"allowed_skills,omitempty"`
	AllowedHooks  []string `json:"allowed_hooks,omitempty"`
	EnableTools   *bool    `json:"enable_tools,omitempty"`
	EnableSkills  *bool    `json:"enable_skills,omitempty"`
	EnableHooks   *bool    `json:"enable_hooks,omitempty"`
}

// RequestSemantics 统一请求语义面。
type RequestSemantics struct {
	SystemPrompt string           `json:"system_prompt,omitempty"`
	AnswerStyle  string           `json:"answer_style,omitempty"`
	Capabilities []string         `json:"capabilities,omitempty"`
	Controls     *RequestControls `json:"controls,omitempty"`
}

// CloneRequestControls 深拷贝请求控制面，避免调用方共享可变切片。
func CloneRequestControls(controls *RequestControls) *RequestControls {
	if controls == nil {
		return nil
	}

	clone := *controls
	if len(controls.AllowedTools) > 0 {
		clone.AllowedTools = append([]string(nil), controls.AllowedTools...)
	}
	if len(controls.AllowedSkills) > 0 {
		clone.AllowedSkills = append([]string(nil), controls.AllowedSkills...)
	}
	if len(controls.AllowedHooks) > 0 {
		clone.AllowedHooks = append([]string(nil), controls.AllowedHooks...)
	}
	return &clone
}

// CloneRequestSemantics 深拷贝请求语义，避免调用方共享可变切片。
func CloneRequestSemantics(semantics *RequestSemantics) *RequestSemantics {
	if semantics == nil {
		return nil
	}

	clone := *semantics
	if len(semantics.Capabilities) > 0 {
		clone.Capabilities = append([]string(nil), semantics.Capabilities...)
	}
	if semantics.Controls != nil {
		clone.Controls = CloneRequestControls(semantics.Controls)
	}
	return &clone
}

// SendOptions 发送选项
type SendOptions struct {
	System      string            `json:"system,omitempty"`
	Semantics   *RequestSemantics `json:"semantics,omitempty"`
	Model       string            `json:"model,omitempty"`
	Temperature *float64          `json:"temperature,omitempty"`
	MaxTokens   int               `json:"max_tokens,omitempty"`
	Stream      bool              `json:"stream,omitempty"`
	Timeout     time.Duration     `json:"timeout,omitempty"`
	Extra       JSONObject        `json:"extra,omitempty"`
}

// ToolDefinition 工具定义
type ToolDefinition struct {
	Name        string               `json:"name"`
	Description string               `json:"description"`
	Parameters  ToolDefinitionParams `json:"parameters"`
}

// ToolDefinitionParams 工具参数 schema
type ToolDefinitionParams struct {
	Type       string                        `json:"type"`
	Properties map[string]ToolPropertySchema `json:"properties"`
	Required   []string                      `json:"required"`
}

// ToolPropertySchema 工具参数属性 schema
type ToolPropertySchema struct {
	Type        string            `json:"type"`
	Description string            `json:"description"`
	Enum        []string          `json:"enum,omitempty"`
	Items       *ToolPropertyItem `json:"items,omitempty"`
}

// ToolPropertyItem 数组项 schema
type ToolPropertyItem struct {
	Type string `json:"type"`
}

// ToolTurnRequest 原生工具回合请求
type ToolTurnRequest struct {
	Messages    []Message         `json:"messages,omitempty"`
	Tools       []ToolDefinition  `json:"tools,omitempty"`
	System      string            `json:"system,omitempty"`
	Semantics   *RequestSemantics `json:"semantics,omitempty"`
	Model       string            `json:"model,omitempty"`
	Temperature *float64          `json:"temperature,omitempty"`
	MaxTokens   int               `json:"max_tokens,omitempty"`
}

// ToolTurnResponse 原生工具回合响应
type ToolTurnResponse struct {
	Message      Message        `json:"message"`
	Content      string         `json:"content"`
	Blocks       []ContentBlock `json:"blocks,omitempty"`
	Model        string         `json:"model"`
	Usage        Usage          `json:"usage"`
	FinishReason string         `json:"finish_reason,omitempty"`
}

type AdapterConfig struct {
	APIKey      string        `json:"api_key,omitempty"`
	BaseURL     string        `json:"base_url,omitempty"`
	Model       string        `json:"model"`
	Temperature float64       `json:"temperature,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Timeout     time.Duration `json:"timeout,omitempty"`
	MaxRetries  int           `json:"max_retries,omitempty"`
	RetryDelay  time.Duration `json:"retry_delay,omitempty"`

	ForceTemperature bool `json:"-"`
	ForceMaxTokens   bool `json:"-"`
	ForceTimeout     bool `json:"-"`
	ForceMaxRetries  bool `json:"-"`
	ForceRetryDelay  bool `json:"-"`
}

// ICliAdapter CLI适配器接口
type ICliAdapter interface {
	// 基础通信
	Send(ctx context.Context, prompt string, options *SendOptions) (*AIResponse, error)
	Stream(ctx context.Context, prompt string, options *SendOptions) (<-chan StreamChunk, error)

	// 上下文管理
	GetHistory() []Message
	SetHistory(messages []Message)
	ClearHistory()

	// 状态控制
	Rewind(steps int) error
	Compact(ctx context.Context) error

	// 配置
	Configure(config *AdapterConfig)
	GetConfig() AdapterConfig

	// 关闭
	Close() error
}

// ToolCallableAdapter 支持原生 tools/tool_use/tool_result 的适配器接口
type ToolCallableAdapter interface {
	SendToolTurn(ctx context.Context, req *ToolTurnRequest) (*ToolTurnResponse, error)
}

// Provider LLM提供商
type Provider string

const (
	ProviderClaude Provider = "claude"
	ProviderOpenAI Provider = "openai"
	ProviderGemini Provider = "gemini"
	ProviderCodex  Provider = "codex"
	ProviderCustom Provider = "custom"
)

// APIError API错误
type APIError struct {
	Message    string `json:"message"`
	StatusCode int    `json:"status_code,omitempty"`
	Code       string `json:"code,omitempty"`
	Retryable  bool   `json:"retryable"`
}

func (e *APIError) Error() string {
	return e.Message
}

// NewAPIError 创建API错误
func NewAPIError(message string, statusCode int, code string, retryable bool) *APIError {
	return &APIError{
		Message:    message,
		StatusCode: statusCode,
		Code:       code,
		Retryable:  retryable,
	}
}

// TimeoutError 超时错误
type TimeoutError struct {
	Message string `json:"message"`
}

func (e *TimeoutError) Error() string {
	if e.Message == "" {
		return "request timeout"
	}
	return e.Message
}

// ApproxMessageChars 估算消息字符数，优先使用结构化块。
func ApproxMessageChars(msg Message) int {
	if len(msg.Blocks) == 0 {
		return len(msg.Content)
	}

	total := 0
	for _, block := range msg.Blocks {
		total += approxContentBlockChars(block)
	}
	if total == 0 {
		return len(msg.Content)
	}
	return total
}

func approxContentBlockChars(block ContentBlock) int {
	switch block.Type {
	case "text":
		return len(block.Text)
	case "tool_use":
		total := len(block.ID) + len(block.Name)
		if len(block.Input) > 0 {
			payload, err := json.Marshal(block.Input)
			if err == nil {
				total += len(payload)
			}
		}
		return total
	case "tool_result":
		return len(block.ToolUseID) + len(block.Result)
	default:
		payload, err := json.Marshal(block)
		if err != nil {
			return len(block.Text) + len(block.Name) + len(block.Result)
		}
		return len(payload)
	}
}

// DefaultAdapterConfig 默认配置
var DefaultAdapterConfig = AdapterConfig{
	Model:       "claude-3-5-sonnet-20241022",
	Temperature: 1.0,
	MaxTokens:   4096,
	Timeout:     60 * time.Second,
	MaxRetries:  3,
	RetryDelay:  time.Second,
}

var providerAliases = map[string]Provider{
	"":          ProviderClaude,
	"claude":    ProviderClaude,
	"anthropic": ProviderClaude,
	"custom":    ProviderClaude,
	"openai":    ProviderOpenAI,
	"gemini":    ProviderGemini,
	"google":    ProviderGemini,
	"codex":     ProviderCodex,
}

// ProviderFromString 规范化外部 provider 名称到 adapters.Provider。
func ProviderFromString(value string) (Provider, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(ProviderClaude), "anthropic", "custom":
		return ProviderClaude, nil
	case string(ProviderOpenAI):
		return ProviderOpenAI, nil
	case string(ProviderGemini), "google":
		return ProviderGemini, nil
	case string(ProviderCodex):
		return ProviderCodex, nil
	default:
		return "", fmt.Errorf("unsupported adapter provider %q", value)
	}
}

// NormalizeProvider 兼容旧调用方，委托到 ProviderFromString。
func NormalizeProvider(raw string) (Provider, error) {
	return ProviderFromString(raw)
}

// NewAdapter 通过统一 provider 入口创建适配器。
func NewAdapter(provider Provider, config *AdapterConfig) (ICliAdapter, error) {
	normalized, err := ProviderFromString(string(provider))
	if err != nil {
		return nil, err
	}

	switch normalized {
	case ProviderClaude:
		return NewClaudeAdapter(config), nil
	case ProviderOpenAI:
		return NewOpenAIAdapter(config), nil
	case ProviderGemini:
		return NewGeminiAdapter(config), nil
	case ProviderCodex:
		return NewCodexAdapter(config), nil
	default:
		return nil, fmt.Errorf("unsupported adapter provider %q", normalized)
	}
}

// AsToolCallableAdapter 将通用 adapter 断言为支持原生 tools 的能力接口。
func AsToolCallableAdapter(adapter ICliAdapter) (ToolCallableAdapter, error) {
	if adapter == nil {
		return nil, fmt.Errorf("adapter is nil")
	}
	toolAdapter, ok := adapter.(ToolCallableAdapter)
	if !ok {
		return nil, fmt.Errorf("adapter %T does not support tool calls", adapter)
	}
	return toolAdapter, nil
}
