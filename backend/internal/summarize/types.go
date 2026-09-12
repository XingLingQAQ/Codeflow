// Package summarize - Conversation summarization types
package summarize

import "time"

// Message represents a conversation message.
type Message struct {
	Role      string                 `json:"role"` // user, assistant, system
	Content   string                 `json:"content"`
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// 摘要输出模式与计量质量标记（§31.3 兼容新增字段，T13.04.b 接线）。
const (
	// ModeLocalExtractive 默认本地抽取式摘要：不调用模型，不宣称高置信语义总结。
	ModeLocalExtractive = "local_extractive"
	// TokenCountQualityEstimated token 计量为 TokenCounter 启发式估算（非精确 tokenizer）。
	TokenCountQualityEstimated = "estimated"
)

// ConversationSummary represents a summarized conversation.
type ConversationSummary struct {
	OriginalMessages int       `json:"original_messages"`
	SummaryText      string    `json:"summary_text"`
	KeyPoints        []string  `json:"key_points"`
	Timestamp        time.Time `json:"timestamp"`
	CompressionRatio float64   `json:"compression_ratio"` // 实测 1-compressed/original，不回填请求目标
	// PreservedMessages 按 preserve_recent 原样保留的最近消息（§31.3）。
	PreservedMessages []Message `json:"preserved_messages,omitempty"`
	// Mode 摘要模式；当前固定 local_extractive。
	Mode string `json:"mode"`
	// TokenCountQuality token 计量质量；当前固定 estimated。
	TokenCountQuality string `json:"token_count_quality"`
	// OriginalTokens 全部输入消息正文的估算 token 数。
	OriginalTokens int `json:"original_tokens"`
	// CompressedTokens 完整返回语义内容（摘要正文+标记+保留区）的估算 token 数。
	CompressedTokens int `json:"compressed_tokens"`
}

// ContextCompression represents compressed context.
type ContextCompression struct {
	OriginalTokens   int       `json:"original_tokens"`
	CompressedTokens int       `json:"compressed_tokens"`
	CompressionRatio float64   `json:"compression_ratio"` // 实测节省比例（1-compressed/original）
	Summary          string    `json:"summary"`           // 可压缩区的本地抽取摘要（含 SummaryMarker 标记后缀）
	RecentContext    string    `json:"recent_context"`    // preserve_recent_pct 对应尾部的原文保留区
	Timestamp        time.Time `json:"timestamp"`
	// Mode 压缩模式；当前固定 local_extractive。
	Mode string `json:"mode"`
	// TokenCountQuality token 计量质量；当前固定 estimated。
	TokenCountQuality string `json:"token_count_quality"`
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
//
// T13.04.b：指针字段保留"字段是否出现"（§31.3）：nil 表示省略（走默认值），
// 非 nil 的 0 值表示显式 0（合法语义，不再被回填为默认值）。
type SummarizeRequest struct {
	Messages          []Message `json:"messages"`
	CompressionTarget *float64  `json:"compression_target,omitempty"` // 目标压缩率（省略默认 0.6；显式 0 = 零压缩目标）
	PreserveRecent    *int      `json:"preserve_recent,omitempty"`    // 原样保留的最近消息条数（省略默认 floor(消息数×0.2)；显式 0 = 不保留）
}

// CompressRequest represents a context compression request.
// 指针字段的出现性语义同 SummarizeRequest。
type CompressRequest struct {
	Context           string   `json:"context"`
	TargetTokens      *int     `json:"target_tokens,omitempty"`       // 总返回预算 token（省略=由比例推导；出现必须为正整数）
	CompressionRatio  *float64 `json:"compression_ratio,omitempty"`   // 目标压缩率（省略默认 0.6；显式 0 = 零压缩目标）
	PreserveRecentPct *float64 `json:"preserve_recent_pct,omitempty"` // 尾部保留比例（省略默认 0.2；显式 0 = 零保留）
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
