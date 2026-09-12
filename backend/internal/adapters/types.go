package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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

// TerminalStatus 流终结状态（T13.04 冻结契约，§31.3）。
// 枚举值固定为 completed/error/cancelled，不得扩展其它取值。
type TerminalStatus string

const (
	// TerminalCompleted 协议级正常完成，只有它代表执行成功。
	TerminalCompleted TerminalStatus = "completed"
	// TerminalError 异常终结：provider 流内错误、扫描失败、非正常 EOF 或关键帧损坏。
	TerminalError TerminalStatus = "error"
	// TerminalCancelled 调用方取消导致的终结（由发送侧依据 ctx 判定，parser 不产生）。
	TerminalCancelled TerminalStatus = "cancelled"
)

// StreamError 终结帧上的结构化错误（§31.3：error{code,message,retryable}）。
type StreamError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

// StreamChunk 流式响应块
//
// T13.04 冻结的终结契约（§31.3）：
//   - Done=false 表示内容帧，只携带 Delta/Index，绝不携带 TerminalStatus/Error/Usage。
//   - Done=true 仅表示"流已终结"，不代表成功；终结帧只发送一次，
//     且必须携带 TerminalStatus，只有 TerminalCompleted 才是执行成功。
//   - 消费者必须同时检查 Done 与 TerminalStatus，不得凭 Done=true 判定成功。
//   - 新增字段全部可空/缺省：旧消费者只认 Delta/Index/Done 不受影响。
type StreamChunk struct {
	Delta string `json:"delta"`
	Index int    `json:"index"`
	Done  bool   `json:"done"`
	// TerminalStatus 终结状态，仅 Done=true 时出现。
	TerminalStatus TerminalStatus `json:"terminal_status,omitempty"`
	// Error 结构化终结错误，仅 TerminalStatus=TerminalError 时出现。
	Error *StreamError `json:"error,omitempty"`
	// FinishReason provider 上报的真实结束原因，按各 provider 原值映射，不统一写 "stop"。
	FinishReason string `json:"finish_reason,omitempty"`
	// Usage provider 上报的流式用量，可空（provider 未上报时为 nil）。
	Usage *Usage `json:"usage,omitempty"`
}

// streamProviderBody 驱动一条 provider 流：把响应体交给 parser，内容帧一解析
// 出来就投递，解析结束后按 §31.3 终结契约投递终结帧（T13.04.b 接线）。
//
// 必须逐帧投递，不能"先整体解析再投递"：那样消费者要等 provider 把整条回答
// 生成完才收到第一帧，Stream 就退化成了一次性返回，实时输出无从谈起。parse
// 的 sink 回调因此同时承担投递与取消检测：sink 返回 false（消费者已取消）时
// parser 立即停止扫描，也不再继续读响应体。
//
// 语义：
//   - 内容帧按 parser 顺序实时投递；每个 send 都 select ctx.Done，取消即停止。
//   - 只有 TerminalCompleted 才把 assistant 内容追加为成功历史，并触发 PostResponse
//     （FinishReason 为 provider 原值，不再伪报 "stop"）。
//   - error 终结（EOF/provider error/扫描失败/帧损坏）只发终结帧，不追加成功历史、
//     不触发 PostResponse；已投递的部分 delta 仅供错误展示，不冒充完整回答。
//   - 取消路径：终结帧只尽力投递一次（channel 有缓冲空位才投），绝不阻塞等待
//     不存在的消费者；响应体与 channel 的关闭由调用方 goroutine 的 defer 完成。
func streamProviderBody(
	ctx context.Context,
	base *BaseAdapter,
	controls *RequestControls,
	model string,
	body io.Reader,
	parse func(io.Reader, streamFrameSink) *StreamParseResult,
	ch chan StreamChunk,
) {
	sent := 0
	cancelled := ctx.Err() != nil

	var result *StreamParseResult
	if cancelled {
		// 进入时已取消：不读响应体，直接走取消终结。
		result = &StreamParseResult{}
	} else {
		result = parse(body, func(chunk StreamChunk) bool {
			if !sendStreamChunk(ctx, controls, ch, chunk) {
				return false
			}
			sent++
			return true
		})
		cancelled = result.sinkCancelled
	}

	if cancelled || ctx.Err() != nil {
		terminal := StreamChunk{Done: true, Index: sent, TerminalStatus: TerminalCancelled}
		select {
		case ch <- terminal:
		default:
		}
		return
	}

	status := result.TerminalStatus()
	terminal := StreamChunk{Done: true, Index: sent, TerminalStatus: status, FinishReason: result.FinishReason, Usage: result.Usage}
	if status != TerminalCompleted {
		terminal.Error = result.TerminalError()
		sendStreamChunk(ctx, controls, ch, terminal)
		return
	}

	content := streamResultContent(result.Chunks)
	assistantMsg := Message{Role: RoleAssistant, Content: content, Blocks: []ContentBlock{{Type: "text", Text: content}}, Timestamp: time.Now()}
	base.AddMessage(assistantMsg)
	if sendStreamChunk(ctx, controls, ch, terminal) {
		_ = notifyAdapterPostResponse(ctx, controls, &AIResponse{Content: content, Blocks: cloneBlocks(assistantMsg.Blocks), Model: model, Usage: derefUsage(result.Usage), FinishReason: result.FinishReason})
	}
}

// sendStreamChunk 透传 hook 后发送一帧；ctx 取消时返回 false，由调用方走关闭路径。
func sendStreamChunk(ctx context.Context, controls *RequestControls, ch chan<- StreamChunk, chunk StreamChunk) bool {
	notifyAdapterStreamChunk(ctx, controls, chunk)
	select {
	case ch <- chunk:
		return true
	case <-ctx.Done():
		return false
	}
}

// streamResultContent 拼接 parser 产出的全部内容帧。
func streamResultContent(chunks []StreamChunk) string {
	var b strings.Builder
	for _, chunk := range chunks {
		b.WriteString(chunk.Delta)
	}
	return b.String()
}

// derefUsage 把可空的流式 usage 投影为 AIResponse 的值字段。
func derefUsage(usage *Usage) Usage {
	if usage == nil {
		return Usage{}
	}
	return *usage
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
	APIKey      string        `json:"-"`
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
