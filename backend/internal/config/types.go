package config

import (
	"context"
	"errors"

	"github.com/codeflow/backend/internal/adapters"
)

// ErrAPIChannelNotFound is returned when a caller tries to remove a channel
// that is not present. Removal is intentionally explicit so callers can
// distinguish an idempotent secret delete from a configuration mismatch.
var ErrAPIChannelNotFound = errors.New("api channel not found")

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
	// APIKey is write-only transient input. It is never serialized or retained
	// in ConfigManager state after a successful save.
	APIKey          string       `json:"-" yaml:"-"`
	DeleteSecret    bool         `json:"-" yaml:"-"`
	SecretRef       string       `json:"secret_ref,omitempty" yaml:"secret_ref,omitempty"`
	SecretStatus    SecretStatus `json:"secret_status" yaml:"secret_status"`
	MaskedValue     string       `json:"masked_value,omitempty" yaml:"masked_value,omitempty"`
	SecretVersion   int          `json:"secret_version,omitempty" yaml:"secret_version,omitempty"`
	SecretUpdatedAt int64        `json:"secret_updated_at,omitempty" yaml:"secret_updated_at,omitempty"`
	BaseURL         string       `json:"base_url,omitempty" yaml:"base_url,omitempty"`
	Enabled         bool         `json:"enabled" yaml:"enabled"`
}

// APIChannelWrite is the credential-aware HTTP/configuration write model.
// APIKey is accepted only for a write and is never part of a read model.
type APIChannelWrite struct {
	ID           string   `json:"id" yaml:"id"`
	Name         string   `json:"name,omitempty" yaml:"name,omitempty"`
	Provider     Provider `json:"provider" yaml:"provider"`
	APIKey       *string  `json:"api_key,omitempty" yaml:"-"`
	DeleteSecret bool     `json:"delete_secret,omitempty" yaml:"-"`
	BaseURL      string   `json:"base_url,omitempty" yaml:"base_url,omitempty"`
	Enabled      bool     `json:"enabled" yaml:"enabled"`
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
	Model         string   `json:"model" yaml:"model"`
	Temperature   float64  `json:"temperature" yaml:"temperature"`
	TopP          *float64 `json:"top_p,omitempty" yaml:"top_p,omitempty"`
	APIChannel    string   `json:"api_channel" yaml:"api_channel"`
	MCPTools      []string `json:"mcp_tools" yaml:"mcp_tools"`
	SystemPrompt  string   `json:"system_prompt" yaml:"system_prompt"`
	AnswerStyle   string   `json:"answer_style,omitempty" yaml:"answer_style,omitempty"`
	Capabilities  []string `json:"capabilities,omitempty" yaml:"capabilities,omitempty"`
	AllowedSkills []string `json:"allowed_skills,omitempty" yaml:"allowed_skills,omitempty"`
	AllowedHooks  []string `json:"allowed_hooks,omitempty" yaml:"allowed_hooks,omitempty"`
}

// ConfigHierarchy 配置层级
type ConfigHierarchy struct {
	Global  GlobalConfig             `json:"global" yaml:"global"`
	Session *SessionConfig           `json:"session,omitempty" yaml:"session,omitempty"`
	Role    map[RoleType]*RoleConfig `json:"role,omitempty" yaml:"role,omitempty"`
}

// ResolvedConfig 解析后的配置
type ResolvedConfig struct {
	Model         string      `json:"model"`
	Temperature   float64     `json:"temperature"`
	TopP          *float64    `json:"top_p,omitempty"`
	MaxTokens     *int        `json:"max_tokens,omitempty"`
	APIChannel    *APIChannel `json:"api_channel,omitempty"`
	MCPTools      []string    `json:"mcp_tools"`
	SystemPrompt  string      `json:"system_prompt,omitempty"`
	AnswerStyle   string      `json:"answer_style,omitempty"`
	Capabilities  []string    `json:"capabilities,omitempty"`
	AllowedSkills []string    `json:"allowed_skills,omitempty"`
	AllowedHooks  []string    `json:"allowed_hooks,omitempty"`
	Timeout       int         `json:"timeout,omitempty"`
	MaxRetries    int         `json:"max_retries,omitempty"`
	secretStore   SecretStore
}

// PublicAPIChannel is the read model returned by config APIs. It contains only
// credential state and a masked hint, never the secret value.
type PublicAPIChannel struct {
	ID              string       `json:"id"`
	Name            string       `json:"name"`
	Provider        Provider     `json:"provider"`
	BaseURL         string       `json:"base_url,omitempty"`
	Enabled         bool         `json:"enabled"`
	SecretRef       string       `json:"secret_ref,omitempty"`
	SecretStatus    SecretStatus `json:"secret_status"`
	MaskedValue     string       `json:"masked_value,omitempty"`
	SecretVersion   int          `json:"secret_version,omitempty"`
	SecretUpdatedAt int64        `json:"secret_updated_at,omitempty"`
}

type PublicGlobalConfig struct {
	DefaultModel     string             `json:"default_model"`
	APIPool          []PublicAPIChannel `json:"api_pool"`
	PublicMCP        []string           `json:"public_mcp"`
	SummaryThreshold int                `json:"summary_threshold,omitempty"`
	MaxRetries       int                `json:"max_retries,omitempty"`
	Timeout          int                `json:"timeout,omitempty"`
}

type PublicResolvedConfig struct {
	Model         string            `json:"model"`
	Temperature   float64           `json:"temperature"`
	TopP          *float64          `json:"top_p,omitempty"`
	MaxTokens     *int              `json:"max_tokens,omitempty"`
	APIChannel    *PublicAPIChannel `json:"api_channel,omitempty"`
	MCPTools      []string          `json:"mcp_tools"`
	SystemPrompt  string            `json:"system_prompt,omitempty"`
	AnswerStyle   string            `json:"answer_style,omitempty"`
	Capabilities  []string          `json:"capabilities,omitempty"`
	AllowedSkills []string          `json:"allowed_skills,omitempty"`
	AllowedHooks  []string          `json:"allowed_hooks,omitempty"`
	Timeout       int               `json:"timeout,omitempty"`
	MaxRetries    int               `json:"max_retries,omitempty"`
}

func NewPublicAPIChannel(channel APIChannel) PublicAPIChannel {
	return PublicAPIChannel{
		ID:              channel.ID,
		Name:            channel.Name,
		Provider:        channel.Provider,
		BaseURL:         channel.BaseURL,
		Enabled:         channel.Enabled,
		SecretRef:       channel.SecretRef,
		SecretStatus:    channel.SecretStatus,
		MaskedValue:     channel.MaskedValue,
		SecretVersion:   channel.SecretVersion,
		SecretUpdatedAt: channel.SecretUpdatedAt,
	}
}

func NewPublicGlobalConfig(cfg *GlobalConfig) PublicGlobalConfig {
	if cfg == nil {
		return PublicGlobalConfig{}
	}
	public := PublicGlobalConfig{
		DefaultModel:     cfg.DefaultModel,
		APIPool:          make([]PublicAPIChannel, len(cfg.APIPool)),
		PublicMCP:        append([]string(nil), cfg.PublicMCP...),
		SummaryThreshold: cfg.SummaryThreshold,
		MaxRetries:       cfg.MaxRetries,
		Timeout:          cfg.Timeout,
	}
	for i, channel := range cfg.APIPool {
		public.APIPool[i] = NewPublicAPIChannel(channel)
	}
	return public
}

func NewPublicResolvedConfig(cfg *ResolvedConfig) PublicResolvedConfig {
	if cfg == nil {
		return PublicResolvedConfig{}
	}
	public := PublicResolvedConfig{
		Model:         cfg.Model,
		Temperature:   cfg.Temperature,
		MCPTools:      append([]string(nil), cfg.MCPTools...),
		SystemPrompt:  cfg.SystemPrompt,
		AnswerStyle:   cfg.AnswerStyle,
		Capabilities:  append([]string(nil), cfg.Capabilities...),
		AllowedSkills: append([]string(nil), cfg.AllowedSkills...),
		AllowedHooks:  append([]string(nil), cfg.AllowedHooks...),
		Timeout:       cfg.Timeout,
		MaxRetries:    cfg.MaxRetries,
	}
	if cfg.TopP != nil {
		value := *cfg.TopP
		public.TopP = &value
	}
	if cfg.MaxTokens != nil {
		value := *cfg.MaxTokens
		public.MaxTokens = &value
	}
	if cfg.APIChannel != nil {
		channel := NewPublicAPIChannel(*cfg.APIChannel)
		public.APIChannel = &channel
	}
	return public
}

// ResolveAPIKey is the sole runtime credential lookup used by adapter
// construction. The resolved config itself remains safe to serialize.
func (c *ResolvedConfig) ResolveAPIKey(ctx context.Context) (string, error) {
	if c == nil || c.APIChannel == nil || c.APIChannel.SecretRef == "" {
		return "", errors.New("resolved config has no configured secret")
	}
	if c.secretStore == nil {
		return "", errors.New("resolved config secret store is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return c.secretStore.Get(ctx, c.APIChannel.SecretRef)
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
	RemoveAPIChannel(channelID string) error

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
