package hotswap

import (
	"time"

	"github.com/codeflow/backend/internal/adapters"
)

// ModelProvider 模型提供商。
type ModelProvider = adapters.Provider

const (
	ProviderClaude ModelProvider = adapters.ProviderClaude
	ProviderGemini ModelProvider = adapters.ProviderGemini
	ProviderCodex  ModelProvider = adapters.ProviderCodex
	ProviderOpenAI ModelProvider = adapters.ProviderOpenAI
	ProviderCustom ModelProvider = adapters.ProviderCustom
)

// ModelStatus 模型状态
type ModelStatus string

const (
	StatusOnline   ModelStatus = "online"
	StatusDegraded ModelStatus = "degraded"
	StatusOffline  ModelStatus = "offline"
)

// ModelCapabilities 模型能力
type ModelCapabilities struct {
	Streaming       bool `json:"streaming"`
	Vision          bool `json:"vision"`
	FunctionCalling bool `json:"function_calling"`
	CodeExecution   bool `json:"code_execution"`
	Multimodal      bool `json:"multimodal"`
}

// ModelInfo 模型信息
type ModelInfo struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Provider        ModelProvider     `json:"provider"`
	Capabilities    ModelCapabilities `json:"capabilities"`
	ContextWindow   int               `json:"context_window"`
	MaxOutputTokens int               `json:"max_output_tokens"`
	CostPer1kInput  float64           `json:"cost_per_1k_input,omitempty"`
	CostPer1kOutput float64           `json:"cost_per_1k_output,omitempty"`
	Available       bool              `json:"available"`
	Status          ModelStatus       `json:"status,omitempty"`
}

// SwitchOptions 切换选项
type SwitchOptions struct {
	PreserveHistory bool `json:"preserve_history"`
	MigrateContext  bool `json:"migrate_context"`
	FallbackOnError bool `json:"fallback_on_error"`
	RetryCount      int  `json:"retry_count,omitempty"`
}

// SwitchResult 切换结果
type SwitchResult struct {
	Success         bool   `json:"success"`
	PreviousModel   string `json:"previous_model"`
	CurrentModel    string `json:"current_model"`
	ContextMigrated bool   `json:"context_migrated"`
	TokensMigrated  int    `json:"tokens_migrated"`
	Error           string `json:"error,omitempty"`
}

// RetryStrategy 重试策略
type RetryStrategy struct {
	MaxRetries        int           `json:"max_retries"`
	BaseDelay         time.Duration `json:"base_delay"`
	MaxDelay          time.Duration `json:"max_delay"`
	BackoffMultiplier float64       `json:"backoff_multiplier"`
	RetryableErrors   []string      `json:"retryable_errors"`
}

// RelayConfig 接力配置
type RelayConfig struct {
	Enabled         bool     `json:"enabled"`
	FallbackChain   []string `json:"fallback_chain"`
	AutoSwitch      bool     `json:"auto_switch"`
	SwitchThreshold int      `json:"switch_threshold"`
}

// ContextMigrationResult 上下文迁移结果
type ContextMigrationResult struct {
	Success        bool               `json:"success"`
	OriginalTokens int                `json:"original_tokens"`
	MigratedTokens int                `json:"migrated_tokens"`
	Truncated      bool               `json:"truncated"`
	Messages       []adapters.Message `json:"messages"`
}

// HotSwapConfig 热切换配置
type HotSwapConfig struct {
	DefaultModel            string        `json:"default_model"`
	AutoRetry               bool          `json:"auto_retry"`
	RetryStrategy           RetryStrategy `json:"retry_strategy"`
	RelayConfig             RelayConfig   `json:"relay_config"`
	ContextMigrationEnabled bool          `json:"context_migration_enabled"`
	MaxContextTokens        int           `json:"max_context_tokens"`
}

// IHotSwapManager 热切换管理器接口
type IHotSwapManager interface {
	// 模型管理
	GetAvailableModels() []ModelInfo
	GetCurrentModel() *ModelInfo
	GetModelInfo(modelID string) *ModelInfo
	RegisterModel(model *ModelInfo)

	// 适配器管理
	RegisterAdapter(modelID string, adapter adapters.ICliAdapter)
	GetCurrentAdapter() adapters.ICliAdapter

	// 切换操作
	SwitchModel(modelID string, options *SwitchOptions) (*SwitchResult, error)
	CanSwitch(modelID string) bool

	// 重试/接力
	Retry(strategy *RetryStrategy) (*SwitchResult, error)
	Relay(fallbackChain []string) (*SwitchResult, error)

	// 上下文迁移
	MigrateContext(targetModel string) (*ContextMigrationResult, error)

	// 配置
	Configure(config *HotSwapConfig)
	SetRelayConfig(config *RelayConfig)
}

// DefaultHotSwapConfig 默认配置
var DefaultHotSwapConfig = HotSwapConfig{
	DefaultModel: "claude-3-opus",
	AutoRetry:    true,
	RetryStrategy: RetryStrategy{
		MaxRetries:        3,
		BaseDelay:         time.Second,
		MaxDelay:          10 * time.Second,
		BackoffMultiplier: 2,
		RetryableErrors:   []string{"rate_limit", "timeout", "server_error"},
	},
	RelayConfig: RelayConfig{
		Enabled:         true,
		FallbackChain:   []string{"claude-3-opus", "gemini-pro", "gpt-4"},
		AutoSwitch:      false,
		SwitchThreshold: 3,
	},
	ContextMigrationEnabled: true,
	MaxContextTokens:        100000,
}

// DefaultSwitchOptions 默认切换选项
var DefaultSwitchOptions = SwitchOptions{
	PreserveHistory: true,
	MigrateContext:  true,
	FallbackOnError: true,
	RetryCount:      0,
}

// PredefinedModels 预定义模型列表
var PredefinedModels = []ModelInfo{
	{
		ID:       "claude-3-opus",
		Name:     "Claude 3 Opus",
		Provider: ProviderClaude,
		Capabilities: ModelCapabilities{
			Streaming:       true,
			Vision:          true,
			FunctionCalling: true,
			CodeExecution:   false,
			Multimodal:      true,
		},
		ContextWindow:   200000,
		MaxOutputTokens: 4096,
		Available:       true,
		Status:          StatusOnline,
	},
	{
		ID:       "claude-3-sonnet",
		Name:     "Claude 3 Sonnet",
		Provider: ProviderClaude,
		Capabilities: ModelCapabilities{
			Streaming:       true,
			Vision:          true,
			FunctionCalling: true,
			CodeExecution:   false,
			Multimodal:      true,
		},
		ContextWindow:   200000,
		MaxOutputTokens: 4096,
		Available:       true,
		Status:          StatusOnline,
	},
	{
		ID:       "gemini-pro",
		Name:     "Gemini Pro",
		Provider: ProviderGemini,
		Capabilities: ModelCapabilities{
			Streaming:       true,
			Vision:          true,
			FunctionCalling: true,
			CodeExecution:   false,
			Multimodal:      true,
		},
		ContextWindow:   1000000,
		MaxOutputTokens: 8192,
		Available:       true,
		Status:          StatusOnline,
	},
	{
		ID:       "codex-cli",
		Name:     "Codex CLI",
		Provider: ProviderCodex,
		Capabilities: ModelCapabilities{
			Streaming:       true,
			Vision:          false,
			FunctionCalling: true,
			CodeExecution:   true,
			Multimodal:      false,
		},
		ContextWindow:   128000,
		MaxOutputTokens: 4096,
		Available:       true,
		Status:          StatusOnline,
	},
}
