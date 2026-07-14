package summarize

import (
	"context"

	"github.com/codeflow/backend/internal/adapters"
)

// TokenCount is the result of counting tokens across messages.
type TokenCount struct {
	Total     int            `json:"total"`
	ByRole    map[string]int `json:"by_role"`
	ByMessage []int          `json:"by_message"`
}

// CompressionConfig configures message-level context compression.
type CompressionConfig struct {
	MaxTokens               int     `json:"max_tokens"`
	TargetRatio             float64 `json:"target_ratio"`
	PreserveSystemPrompt    bool    `json:"preserve_system_prompt"`
	PreserveRecentMessages  int     `json:"preserve_recent_messages"`
	ExtractDecisionSkeleton bool    `json:"extract_decision_skeleton"`
}

// EntitySkeleton is an entity/decision graph extracted from messages.
// Distinct from DecisionSkeleton (API architecture-decision shape).
type EntitySkeleton struct {
	Entities  []string         `json:"entities"`
	Decisions []string         `json:"decisions"`
	Relations []EntityRelation `json:"relations"`
}

// EntityRelation is a co-occurrence edge between entities.
type EntityRelation struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type"`
}

// CompressionResult is the output of message-level compression.
type CompressionResult struct {
	OriginalTokens    int                `json:"original_tokens"`
	CompressedTokens  int                `json:"compressed_tokens"`
	CompressionRatio  float64            `json:"compression_ratio"`
	PreservedMessages []adapters.Message `json:"preserved_messages"`
	Summary           string             `json:"summary,omitempty"`
	EntitySkeleton    *EntitySkeleton    `json:"entity_skeleton,omitempty"`
}

// SummaryAgentConfig configures optional LLM-backed summary generation.
type SummaryAgentConfig struct {
	Model            string `json:"model,omitempty"`
	MaxSummaryTokens int    `json:"max_summary_tokens,omitempty"`
	IncludeEntities  bool   `json:"include_entities,omitempty"`
	IncludeDecisions bool   `json:"include_decisions,omitempty"`
	IncludeRelations bool   `json:"include_relations,omitempty"`
}

// EngineContext is a message list plus optional precomputed token count.
type EngineContext struct {
	Messages   []adapters.Message `json:"messages"`
	TokenCount int                `json:"token_count"`
}

// SummaryHistory stores a prior compression/summary for a session.
type SummaryHistory struct {
	ID           string          `json:"id"`
	SessionID    string          `json:"session_id"`
	Summary      string          `json:"summary"`
	Skeleton     *EntitySkeleton `json:"skeleton,omitempty"`
	MessageRange struct {
		Start int `json:"start"`
		End   int `json:"end"`
	} `json:"message_range"`
	CreatedAt int64 `json:"created_at"`
}

// ITokenCounter estimates tokens for text and message lists.
type ITokenCounter interface {
	Count(text string) int
	CountMessages(messages []adapters.Message) TokenCount
	EstimateTokens(text string) int
}

// ICompressor compresses message contexts and extracts entity skeletons.
type ICompressor interface {
	Compress(ctx EngineContext, config *CompressionConfig) (*CompressionResult, error)
	CompressMessages(parent context.Context, ctx EngineContext, config *CompressionConfig) (*CompressionResult, error)
	ExtractEntitySkeleton(messages []adapters.Message) (*EntitySkeleton, error)
	GenerateSummary(messages []adapters.Message, config *SummaryAgentConfig) (string, error)
	GenerateSummaryContext(ctx context.Context, messages []adapters.Message, config *SummaryAgentConfig) (string, error)
}

// ISummaryHistory persists summary history records.
type ISummaryHistory interface {
	Save(history *SummaryHistory) error
	Load(id string) (*SummaryHistory, error)
	LoadBySession(sessionID string) ([]*SummaryHistory, error)
	Delete(id string) error
	List() ([]*SummaryHistory, error)
}

// DefaultCompressionConfig is the default message-compression policy.
var DefaultCompressionConfig = CompressionConfig{
	MaxTokens:               4000,
	TargetRatio:             0.2,
	PreserveSystemPrompt:    true,
	PreserveRecentMessages:  3,
	ExtractDecisionSkeleton: true,
}

// TokenEstimation holds heuristic token-estimation constants.
var TokenEstimation = struct {
	CharsPerTokenEN    float64
	CharsPerTokenZH    float64
	OverheadPerMessage int
}{
	CharsPerTokenEN:    4.0,
	CharsPerTokenZH:    1.5,
	OverheadPerMessage: 4,
}
