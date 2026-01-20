// Package summarize - Conversation summarization types
package summarize

import "time"

// Message represents a conversation message.
type Message struct {
	Role      string                 `json:"role"`      // user, assistant, system
	Content   string                 `json:"content"`
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// ConversationSummary represents a summarized conversation.
type ConversationSummary struct {
	OriginalMessages int       `json:"original_messages"`
	SummaryText      string    `json:"summary_text"`
	KeyPoints        []string  `json:"key_points"`
	Timestamp        time.Time `json:"timestamp"`
	CompressionRatio float64   `json:"compression_ratio"` // 0-1, higher = more compressed
}

// ContextCompression represents compressed context.
type ContextCompression struct {
	OriginalTokens   int       `json:"original_tokens"`
	CompressedTokens int       `json:"compressed_tokens"`
	CompressionRatio float64   `json:"compression_ratio"` // Percentage saved
	Summary          string    `json:"summary"`           // First 80% summarized
	RecentContext    string    `json:"recent_context"`    // Last 20% original
	Timestamp        time.Time `json:"timestamp"`
}

// DecisionSkeleton represents key decisions and context.
type DecisionSkeleton struct {
	ArchitectureDecisions []string               `json:"architecture_decisions"`
	UnfixedBugs           []string               `json:"unfixed_bugs"`
	VariableDefinitions   map[string]string      `json:"variable_definitions"`
	KeyFiles              []string               `json:"key_files"`
	Timestamp             time.Time              `json:"timestamp"`
	Metadata              map[string]interface{} `json:"metadata,omitempty"`
}

// SummarizeRequest represents a summarization request.
type SummarizeRequest struct {
	Messages          []Message `json:"messages"`
	CompressionTarget float64   `json:"compression_target,omitempty"` // Target compression ratio (default: 0.6)
	PreserveRecent    int       `json:"preserve_recent,omitempty"`    // Number of recent messages to preserve (default: 20%)
}

// CompressRequest represents a context compression request.
type CompressRequest struct {
	Context           string  `json:"context"`
	TargetTokens      int     `json:"target_tokens,omitempty"`      // Target token count
	CompressionRatio  float64 `json:"compression_ratio,omitempty"`  // Target compression ratio (default: 0.6)
	PreserveRecentPct float64 `json:"preserve_recent_pct,omitempty"` // Percentage of recent context to preserve (default: 0.2)
}

// ISummarizer defines the interface for conversation summarization.
type ISummarizer interface {
	// SummarizeConversation summarizes a conversation.
	SummarizeConversation(req *SummarizeRequest) (*ConversationSummary, error)

	// CompressContext compresses context using 80/20 strategy.
	CompressContext(req *CompressRequest) (*ContextCompression, error)

	// ExtractSkeleton extracts decision skeleton from conversation.
	ExtractSkeleton(messages []Message) (*DecisionSkeleton, error)

	// CalculateTokens estimates token count for text.
	CalculateTokens(text string) int
}
