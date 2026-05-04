package summarizer

import (
	"context"

	"github.com/codeflow/backend/internal/adapters"
)

// TokenCount Token计数结果
type TokenCount struct {
	Total     int            `json:"total"`
	ByRole    map[string]int `json:"by_role"`
	ByMessage []int          `json:"by_message"`
}

// CompressionConfig 压缩配置
type CompressionConfig struct {
	MaxTokens               int     `json:"max_tokens"`
	TargetRatio             float64 `json:"target_ratio"`
	PreserveSystemPrompt    bool    `json:"preserve_system_prompt"`
	PreserveRecentMessages  int     `json:"preserve_recent_messages"`
	ExtractDecisionSkeleton bool    `json:"extract_decision_skeleton"`
}

// DecisionSkeleton 决策骨架
type DecisionSkeleton struct {
	Entities  []string   `json:"entities"`
	Decisions []string   `json:"decisions"`
	Relations []Relation `json:"relations"`
}

// Relation 关系
type Relation struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type"`
}

// CompressionResult 压缩结果
type CompressionResult struct {
	OriginalTokens    int                `json:"original_tokens"`
	CompressedTokens  int                `json:"compressed_tokens"`
	CompressionRatio  float64            `json:"compression_ratio"`
	PreservedMessages []adapters.Message `json:"preserved_messages"`
	Summary           string             `json:"summary,omitempty"`
	DecisionSkeleton  *DecisionSkeleton  `json:"decision_skeleton,omitempty"`
}

// SummaryAgentConfig Summary Agent配置
type SummaryAgentConfig struct {
	Model            string `json:"model,omitempty"`
	MaxSummaryTokens int    `json:"max_summary_tokens,omitempty"`
	IncludeEntities  bool   `json:"include_entities,omitempty"`
	IncludeDecisions bool   `json:"include_decisions,omitempty"`
	IncludeRelations bool   `json:"include_relations,omitempty"`
}

// Context 上下文
type Context struct {
	Messages   []adapters.Message `json:"messages"`
	TokenCount int                `json:"token_count"`
}

// SummaryHistory 总结历史记录
type SummaryHistory struct {
	ID           string            `json:"id"`
	SessionID    string            `json:"session_id"`
	Summary      string            `json:"summary"`
	Skeleton     *DecisionSkeleton `json:"skeleton,omitempty"`
	MessageRange struct {
		Start int `json:"start"`
		End   int `json:"end"`
	} `json:"message_range"`
	CreatedAt int64 `json:"created_at"`
}

// ITokenCounter Token计数器接口
type ITokenCounter interface {
	Count(text string) int
	CountMessages(messages []adapters.Message) TokenCount
	EstimateTokens(text string) int
}

// ICompressor 压缩器接口
type ICompressor interface {
	Compress(ctx Context, config *CompressionConfig) (*CompressionResult, error)
	CompressContext(parent context.Context, ctx Context, config *CompressionConfig) (*CompressionResult, error)
	ExtractSkeleton(messages []adapters.Message) (*DecisionSkeleton, error)
	GenerateSummary(messages []adapters.Message, config *SummaryAgentConfig) (string, error)
	GenerateSummaryContext(ctx context.Context, messages []adapters.Message, config *SummaryAgentConfig) (string, error)
}

// ISummaryHistory 总结历史接口
type ISummaryHistory interface {
	Save(history *SummaryHistory) error
	Load(id string) (*SummaryHistory, error)
	LoadBySession(sessionID string) ([]*SummaryHistory, error)
	Delete(id string) error
	List() ([]*SummaryHistory, error)
}

// DefaultCompressionConfig 默认压缩配置
var DefaultCompressionConfig = CompressionConfig{
	MaxTokens:               4000,
	TargetRatio:             0.2,
	PreserveSystemPrompt:    true,
	PreserveRecentMessages:  3,
	ExtractDecisionSkeleton: true,
}

// TokenEstimation Token估算常量
var TokenEstimation = struct {
	CharsPerTokenEN    float64
	CharsPerTokenZH    float64
	OverheadPerMessage int
}{
	CharsPerTokenEN:    4.0,
	CharsPerTokenZH:    1.5,
	OverheadPerMessage: 4,
}
