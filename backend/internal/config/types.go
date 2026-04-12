package config

import "github.com/codeflow/backend/internal/adapters"

// Provider API提供商类型
type Provider string

const (
	ProviderAnthropic Provider = "anthropic"
	ProviderOpenAI    Provider = "openai"
	ProviderGoogle    Provider = "google"
	ProviderCustom    Provider = "custom"
)

// ToAdapterProvider 归一化配置 provider 到 adapters 真相源。
func (p Provider) ToAdapterProvider() (adapters.Provider, error) {
	return adapters.ProviderFromString(string(p))
}

// AdapterProvider 返回 API 通道对应的 adapters provider。
func (c APIChannel) AdapterProvider() (adapters.Provider, error) {
	return c.Provider.ToAdapterProvider()
}

// SessionMode 会话模式
type SessionMode string

const (
	ModeDevelopment SessionMode = "development"
	ModeResearch    SessionMode = "research"
	ModeCreative    SessionMode = "creative"
)

// RoleType 角色类型
type RoleType string

const (
	RoleMain  RoleType = "main"
	RoleCoder RoleType = "coder"
	RoleSub   RoleType = "sub"
)

// APIChannel API通道配置
type APIChannel struct {
	ID       string   `json:"id" yaml:"id"`
	Name     string   `json:"name" yaml:"name"`
	Provider Provider `json:"provider" yaml:"provider"`
	APIKey   string   `json:"api_key,omitempty" yaml:"api_key,omitempty"`
	BaseURL  string   `json:"base_url,omitempty" yaml:"base_url,omitempty"`
	Enabled  bool     `json:"enabled" yaml:"enabled"`
}

// GlobalConfig 全局配置
type GlobalConfig struct {
	DefaultModel     string       `json:"default_model" yaml:"default_model"`
	APIPool          []APIChannel `json:"api_pool" yaml:"api_pool"`
	PublicMCP        []string     `json:"public_mcp" yaml:"public_mcp"`
	SummaryThreshold int          `json:"summary_threshold,omitempty" yaml:"summary_threshold,omitempty"`
	MaxRetries       int          `json:"max_retries,omitempty" yaml:"max_retries,omitempty"`
	Timeout          int          `json:"timeout,omitempty" yaml:"timeout,omitempty"`
}

// SessionConfig 会话配置
type SessionConfig struct {
	SessionID     string      `json:"session_id" yaml:"session_id"`
	Mode          SessionMode `json:"mode" yaml:"mode"`
	OverrideModel string      `json:"override_model,omitempty" yaml:"override_model,omitempty"`
	Temperature   *float64    `json:"temperature,omitempty" yaml:"temperature,omitempty"`
	MaxTokens     *int        `json:"max_tokens,omitempty" yaml:"max_tokens,omitempty"`
}

// RoleConfig 角色配置
type RoleConfig struct {
	Model        string   `json:"model" yaml:"model"`
	Temperature  float64  `json:"temperature" yaml:"temperature"`
	TopP         *float64 `json:"top_p,omitempty" yaml:"top_p,omitempty"`
	APIChannel   string   `json:"api_channel" yaml:"api_channel"`
	MCPTools     []string `json:"mcp_tools" yaml:"mcp_tools"`
	SystemPrompt string   `json:"system_prompt" yaml:"system_prompt"`
}

// ConfigHierarchy 配置层级
type ConfigHierarchy struct {
	Global  GlobalConfig             `json:"global" yaml:"global"`
	Session *SessionConfig           `json:"session,omitempty" yaml:"session,omitempty"`
	Role    map[RoleType]*RoleConfig `json:"role,omitempty" yaml:"role,omitempty"`
}

// ResolvedConfig 解析后的配置
type ResolvedConfig struct {
	Model        string      `json:"model"`
	Temperature  float64     `json:"temperature"`
	TopP         *float64    `json:"top_p,omitempty"`
	MaxTokens    *int        `json:"max_tokens,omitempty"`
	APIChannel   *APIChannel `json:"api_channel,omitempty"`
	MCPTools     []string    `json:"mcp_tools"`
	SystemPrompt string      `json:"system_prompt,omitempty"`
	Timeout      int         `json:"timeout,omitempty"`
	MaxRetries   int         `json:"max_retries,omitempty"`
}

// ConfigChangeCallback 配置变更回调
type ConfigChangeCallback func(config *ConfigHierarchy)

// IConfigManager 配置管理器接口
type IConfigManager interface {
	// 配置加载
	LoadGlobalConfig() *GlobalConfig
	LoadSessionConfig(sessionID string) *SessionConfig
	LoadRoleConfig(role RoleType) *RoleConfig

	// 配置保存
	SaveGlobalConfig(config *GlobalConfig) error
	SaveSessionConfig(config *SessionConfig) error
	SaveRoleConfig(role RoleType, config *RoleConfig) error

	// 配置解析（三级继承）
	ResolveConfig(sessionID string, role RoleType) *ResolvedConfig

	// 配置监听
	OnConfigChange(callback ConfigChangeCallback) func()

	// API Channel管理
	AddAPIChannel(channel *APIChannel) error
	RemoveAPIChannel(channelID string) bool

	// 冲突检测
	DetectConflicts() []string

	// 持久化
	LoadFromFile(path string) error
	SaveToFile(path string) error
}

// DefaultGlobalConfig 默认全局配置
var DefaultGlobalConfig = GlobalConfig{
	DefaultModel:     "claude-3-5-sonnet-20241022",
	APIPool:          []APIChannel{},
	PublicMCP:        []string{},
	SummaryThreshold: 20000,
	MaxRetries:       3,
	Timeout:          60000,
}

// DefaultRoleConfigs 默认角色配置
var DefaultRoleConfigs = map[RoleType]*RoleConfig{
	RoleMain: {
		Model:        "claude-3-5-sonnet-20241022",
		Temperature:  1.0,
		APIChannel:   "default",
		MCPTools:     []string{"orchestrator"},
		SystemPrompt: "You are the main AI commander.",
	},
	RoleCoder: {
		Model:        "claude-3-5-sonnet-20241022",
		Temperature:  0.7,
		APIChannel:   "default",
		MCPTools:     []string{"filesystem", "linter"},
		SystemPrompt: "You are a code implementation expert.",
	},
	RoleSub: {
		Model:        "claude-3-5-haiku-20241022",
		Temperature:  0.8,
		APIChannel:   "default",
		MCPTools:     []string{"websearch"},
		SystemPrompt: "You are a research assistant.",
	},
}
