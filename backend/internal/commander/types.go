package commander

import (
	"github.com/codeflow/backend/internal/adapters"
)

// AgentRole 代理角色类型
type AgentRole string

const (
	RoleMain      AgentRole = "main"
	RoleCoder     AgentRole = "coder"
	RoleSubExpert AgentRole = "sub_expert"
)

// AgentConfig 代理配置
type AgentConfig struct {
	Role         AgentRole              `json:"role"`
	Adapter      adapters.ICliAdapter   `json:"-"`
	SystemPrompt string                 `json:"system_prompt,omitempty"`
	MaxDepth     int                    `json:"max_depth,omitempty"`
	Timeout      int                    `json:"timeout,omitempty"`
}

// CallCoderAgentParams 调用Coder代理参数
type CallCoderAgentParams struct {
	Task        string   `json:"task"`
	Context     string   `json:"context,omitempty"`
	Files       []string `json:"files,omitempty"`
	Language    string   `json:"language,omitempty"`
	Constraints []string `json:"constraints,omitempty"`
}

// ConsultSubExpertParams 咨询子专家参数
type ConsultSubExpertParams struct {
	Domain   string `json:"domain"`
	Question string `json:"question"`
	Context  string `json:"context,omitempty"`
	Depth    int    `json:"depth,omitempty"`
}

// ToolCallResult 工具调用结果
type ToolCallResult struct {
	Success    bool       `json:"success"`
	Output     string     `json:"output"`
	AgentRole  AgentRole  `json:"agent_role"`
	TokenUsage *TokenUsage `json:"token_usage,omitempty"`
	Duration   int64      `json:"duration,omitempty"`
	Error      string     `json:"error,omitempty"`
}

// TokenUsage Token使用量
type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ContextGraftConfig 上下文嫁接配置
type ContextGraftConfig struct {
	InheritMessages     bool     `json:"inherit_messages"`
	InheritSystemPrompt bool     `json:"inherit_system_prompt"`
	MaxContextTokens    int      `json:"max_context_tokens,omitempty"`
	FilterRoles         []string `json:"filter_roles,omitempty"`
}

// GraftedContext 嫁接后的上下文
type GraftedContext struct {
	Messages     []adapters.Message    `json:"messages"`
	SystemPrompt string                `json:"system_prompt,omitempty"`
	Metadata     GraftedContextMetadata `json:"metadata"`
}

// GraftedContextMetadata 嫁接上下文元数据
type GraftedContextMetadata struct {
	SourceAgent AgentRole `json:"source_agent"`
	GraftedAt   int64     `json:"grafted_at"`
	TokenCount  int       `json:"token_count"`
}

// ToolDefinition 工具定义
type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  ToolDefinitionParams   `json:"parameters"`
}

// ToolDefinitionParams 工具定义参数
type ToolDefinitionParams struct {
	Type       string                        `json:"type"`
	Properties map[string]ToolPropertySchema `json:"properties"`
	Required   []string                      `json:"required"`
}

// ToolPropertySchema 工具属性schema
type ToolPropertySchema struct {
	Type        string            `json:"type"`
	Description string            `json:"description"`
	Enum        []string          `json:"enum,omitempty"`
	Items       *ToolPropertyItem `json:"items,omitempty"`
}

// ToolPropertyItem 工具属性项
type ToolPropertyItem struct {
	Type string `json:"type"`
}

// CallTrace 调用追踪
type CallTrace struct {
	ID        string                 `json:"id"`
	ParentID  string                 `json:"parent_id,omitempty"`
	AgentRole AgentRole              `json:"agent_role"`
	ToolName  string                 `json:"tool_name"`
	Params    map[string]interface{} `json:"params"`
	StartTime int64                  `json:"start_time"`
	EndTime   int64                  `json:"end_time,omitempty"`
	Result    *ToolCallResult        `json:"result,omitempty"`
	Children  []*CallTrace           `json:"children"`
}

// CommanderEvent 事件类型
type CommanderEvent string

const (
	EventAgentRegistered CommanderEvent = "agent_registered"
	EventToolCallStart   CommanderEvent = "tool_call_start"
	EventToolCallEnd     CommanderEvent = "tool_call_end"
	EventContextGrafted  CommanderEvent = "context_grafted"
	EventNestedCallStart CommanderEvent = "nested_call_start"
	EventNestedCallEnd   CommanderEvent = "nested_call_end"
)

// EventHandler 事件处理器
type EventHandler func(data interface{})

// ICommander 指挥官接口
type ICommander interface {
	RegisterAgent(config AgentConfig)
	GetAgent(role AgentRole) *AgentConfig
	CallCoderAgent(params CallCoderAgentParams) (*ToolCallResult, error)
	ConsultSubExpert(params ConsultSubExpertParams) (*ToolCallResult, error)
	GraftContext(sourceRole, targetRole AgentRole, config *ContextGraftConfig) (*GraftedContext, error)
	GetToolDefinitions() []ToolDefinition
	GetCallTrace() []*CallTrace
	On(event CommanderEvent, handler EventHandler)
	Off(event CommanderEvent, handler EventHandler)
}
