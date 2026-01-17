package retriever

import (
	"context"
)

// SearchType 搜索类型
type SearchType string

const (
	SearchVector  SearchType = "vector"
	SearchKeyword SearchType = "keyword"
	SearchHybrid  SearchType = "hybrid"
)

// SourceType 结果来源类型
type SourceType string

const (
	SourceVector  SourceType = "vector"
	SourceKeyword SourceType = "keyword"
	SourceHybrid  SourceType = "hybrid"
)

// HybridSearchConfig 混合搜索配置
type HybridSearchConfig struct {
	VectorWeight  float64 `json:"vector_weight"`
	KeywordWeight float64 `json:"keyword_weight"`
	TopK          int     `json:"top_k"`
	MinScore      float64 `json:"min_score"`
	Reranking     bool    `json:"reranking"`
}

// DefaultHybridConfig 默认配置
var DefaultHybridConfig = HybridSearchConfig{
	VectorWeight:  0.7,
	KeywordWeight: 0.3,
	TopK:          10,
	MinScore:      0.3,
	Reranking:     true,
}

// ChunkMetadata 分块元数据
type ChunkMetadata struct {
	SessionID     string `json:"session_id"`
	MessageIndex  int    `json:"message_index"`
	ChunkIndex    int    `json:"chunk_index"`
	AgentRole     string `json:"agent_role,omitempty"`
	GitCommitHash string `json:"git_commit_hash,omitempty"`
	Timestamp     int64  `json:"timestamp"`
}

// VectorSearchResult 向量搜索结果
type VectorSearchResult struct {
	ID       string        `json:"id"`
	Content  string        `json:"content"`
	Score    float64       `json:"score"`
	Metadata ChunkMetadata `json:"metadata"`
}

// KeywordSearchResult 关键词搜索结果
type KeywordSearchResult struct {
	Content    string        `json:"content"`
	Score      float64       `json:"score"`
	Metadata   ChunkMetadata `json:"metadata"`
	Highlights []string      `json:"highlights"`
}

// HybridSearchResult 混合搜索结果
type HybridSearchResult struct {
	Content      string        `json:"content"`
	Score        float64       `json:"score"`
	VectorScore  float64       `json:"vector_score,omitempty"`
	KeywordScore float64       `json:"keyword_score,omitempty"`
	Source       SourceType    `json:"source"`
	Metadata     ChunkMetadata `json:"metadata"`
	Highlights   []string      `json:"highlights,omitempty"`
}

// MemoryMatch 记忆匹配结果
type MemoryMatch struct {
	Content    string                 `json:"content"`
	Similarity float64                `json:"similarity"`
	Source     string                 `json:"source"`
	Metadata   map[string]interface{} `json:"metadata"`
}

// TimeRange 时间范围
type TimeRange struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
}

// SearchHistoricalContextParams 历史上下文搜索参数
type SearchHistoricalContextParams struct {
	Query         string      `json:"query"`
	SessionID     string      `json:"session_id,omitempty"`
	AgentRole     string      `json:"agent_role,omitempty"`
	GitCommitHash string      `json:"git_commit_hash,omitempty"`
	TimeRange     *TimeRange  `json:"time_range,omitempty"`
	Limit         int         `json:"limit,omitempty"`
	SearchType    SearchType  `json:"search_type,omitempty"`
}

// SearchHistoricalContextResult 历史上下文搜索结果
type SearchHistoricalContextResult struct {
	Matches    []MemoryMatch `json:"matches"`
	TotalCount int           `json:"total_count"`
	SearchType SearchType    `json:"search_type"`
	QueryTime  int64         `json:"query_time"`
}

// IVectorStore 向量存储接口（简化版，用于retriever依赖）
type IVectorStore interface {
	Search(ctx context.Context, query string, topK int, minScore float64) ([]VectorSearchResult, error)
}

// ISemanticRetriever 语义检索器接口
type ISemanticRetriever interface {
	// SearchHistoricalContext 搜索历史上下文
	SearchHistoricalContext(ctx context.Context, params SearchHistoricalContextParams) (*SearchHistoricalContextResult, error)

	// VectorSearch 向量搜索
	VectorSearch(ctx context.Context, query string, options *HybridSearchConfig) ([]VectorSearchResult, error)

	// KeywordSearch 关键词搜索
	KeywordSearch(ctx context.Context, query string, options *HybridSearchConfig) ([]KeywordSearchResult, error)

	// HybridSearch 混合搜索
	HybridSearch(ctx context.Context, query string, options *HybridSearchConfig) ([]HybridSearchResult, error)

	// IndexContent 索引内容
	IndexContent(id string, content string, metadata ChunkMetadata)

	// ClearIndex 清除索引
	ClearIndex()
}

// SearchHistoricalContextTool 工具定义
var SearchHistoricalContextTool = struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}{
	Name:        "search_historical_context",
	Description: "Search through historical conversation context using semantic similarity and keyword matching. Returns relevant past conversations, decisions, and code discussions.",
}
