package memory

import "context"

// VectorStoreConfig 向量存储配置
type VectorStoreConfig struct {
	CollectionName string
	EmbeddingModel string
	ChunkSize      int
	ChunkOverlap   int
	DBPath         string
	WALMode        bool
}

// DefaultConfig 默认配置
var DefaultConfig = VectorStoreConfig{
	CollectionName: "codeflow_memory",
	ChunkSize:      500,
	ChunkOverlap:   50,
	DBPath:         "./data/vectors.db",
	WALMode:        true,
}

// SourceType 消息来源类型
type SourceType string

const (
	SourceUser      SourceType = "user"
	SourceAssistant SourceType = "assistant"
	SourceSystem    SourceType = "system"
)

// ChunkMetadata 块元数据
type ChunkMetadata struct {
	SessionID     string     `json:"session_id"`
	AgentRole     string     `json:"agent_role"`
	GitCommitHash string     `json:"git_commit_hash,omitempty"`
	MessageIndex  int        `json:"message_index"`
	ChunkIndex    int        `json:"chunk_index"`
	Timestamp     int64      `json:"timestamp"`
	Source        SourceType `json:"source"`
}

// DocumentChunk 文档块
type DocumentChunk struct {
	ID        string        `json:"id"`
	Content   string        `json:"content"`
	Embedding []float64     `json:"embedding,omitempty"`
	Metadata  ChunkMetadata `json:"metadata"`
}

// VectorSearchResult 向量搜索结果
type VectorSearchResult struct {
	Chunk    DocumentChunk `json:"chunk"`
	Score    float64       `json:"score"`
	Distance float64       `json:"distance"`
}

// VectorSearchOptions 向量搜索选项
type VectorSearchOptions struct {
	TopK              int
	MinScore          float64
	FilterSessionID   string
	FilterAgentRole   string
	FilterGitCommit   string
	FilterSource      SourceType
	IncludeEmbeddings bool
}

// CollectionInfo 集合信息
type CollectionInfo struct {
	Name      string                 `json:"name"`
	Count     int                    `json:"count"`
	Dimension int                    `json:"dimension"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// IEmbeddingProvider Embedding 提供者接口
type IEmbeddingProvider interface {
	Embed(ctx context.Context, text string) ([]float64, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float64, error)
	GetDimension() int
}

// IVectorStore 向量存储接口
type IVectorStore interface {
	// 基础操作
	Add(ctx context.Context, chunks []DocumentChunk) error
	Search(ctx context.Context, query string, opts *VectorSearchOptions) ([]VectorSearchResult, error)
	Delete(ctx context.Context, ids []string) error
	Clear(ctx context.Context) error

	// 元数据操作
	GetBySessionID(ctx context.Context, sessionID string) ([]DocumentChunk, error)
	GetByGitCommit(ctx context.Context, commitHash string) ([]DocumentChunk, error)

	// 统计
	Count(ctx context.Context) (int, error)
	GetCollectionInfo(ctx context.Context) (*CollectionInfo, error)

	// 生命周期
	Close() error
}
