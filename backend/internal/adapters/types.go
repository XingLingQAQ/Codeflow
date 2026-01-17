package adapters

import (
	"context"
	"time"
)

// Role 消息角色
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
)

// Message 消息
type Message struct {
	Role      Role      `json:"role"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// Usage 使用统计
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// AIResponse AI响应
type AIResponse struct {
	Content      string `json:"content"`
	Model        string `json:"model"`
	Usage        Usage  `json:"usage"`
	FinishReason string `json:"finish_reason,omitempty"`
}

// StreamChunk 流式响应块
type StreamChunk struct {
	Delta string `json:"delta"`
	Index int    `json:"index"`
	Done  bool   `json:"done"`
}

// SendOptions 发送选项
type SendOptions struct {
	Model       string         `json:"model,omitempty"`
	Temperature *float64       `json:"temperature,omitempty"`
	MaxTokens   int            `json:"max_tokens,omitempty"`
	Stream      bool           `json:"stream,omitempty"`
	Timeout     time.Duration  `json:"timeout,omitempty"`
	Extra       map[string]any `json:"extra,omitempty"`
}

// AdapterConfig 适配器配置
type AdapterConfig struct {
	APIKey      string        `json:"api_key,omitempty"`
	BaseURL     string        `json:"base_url,omitempty"`
	Model       string        `json:"model"`
	Temperature float64       `json:"temperature,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Timeout     time.Duration `json:"timeout,omitempty"`
	MaxRetries  int           `json:"max_retries,omitempty"`
	RetryDelay  time.Duration `json:"retry_delay,omitempty"`
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

// Provider LLM提供商
type Provider string

const (
	ProviderClaude Provider = "claude"
	ProviderOpenAI Provider = "openai"
	ProviderGemini Provider = "gemini"
	ProviderCodex  Provider = "codex"
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

// DefaultAdapterConfig 默认配置
var DefaultAdapterConfig = AdapterConfig{
	Model:       "claude-3-5-sonnet-20241022",
	Temperature: 1.0,
	MaxTokens:   4096,
	Timeout:     60 * time.Second,
	MaxRetries:  3,
	RetryDelay:  time.Second,
}
