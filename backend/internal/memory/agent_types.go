package memory

// IngestRequest MemoryAgent 统一写入请求。
type IngestRequest struct {
	Content   string                 `json:"content"`
	Type      RawEntryType           `json:"type"`
	SessionID string                 `json:"session_id"`
	Source    AtomicMemorySource     `json:"source"`
	Tags      []string               `json:"tags,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// MemorySourceKind 统一来源类型。
type MemorySourceKind string

const (
	MemorySourceKindAtomicMemory MemorySourceKind = "atomic_memory"
	MemorySourceKindSAMGPointer  MemorySourceKind = "samg_pointer"
	MemorySourceKindRawArchive   MemorySourceKind = "raw_archive"
)

// MemoryAgentResolvedPointer 表示已回填 Raw Archive 内容的 SAMG 指针。
type MemoryAgentResolvedPointer struct {
	SourceID        string  `json:"source_id"`
	SourceType      string  `json:"source_type"`
	Summary         string  `json:"summary"`
	LineRange       string  `json:"line_range,omitempty"`
	FilePath        string  `json:"file_path,omitempty"`
	Timestamp       int64   `json:"timestamp"`
	Relevance       float64 `json:"relevance"`
	ResolvedContent string  `json:"resolved_content,omitempty"`
	SessionID       string  `json:"session_id,omitempty"`
}

// MemoryAgentNode 表示统一检索中的图谱节点结果。
type MemoryAgentNode struct {
	ID          string                       `json:"id"`
	Type        []string                     `json:"@type,omitempty"`
	Label       string                       `json:"label"`
	Description string                       `json:"description,omitempty"`
	Properties  map[string]interface{}       `json:"properties,omitempty"`
	Aliases     []string                     `json:"aliases,omitempty"`
	Activation  float64                      `json:"activation"`
	Hop         int                          `json:"hop"`
	Pointers    []MemoryAgentResolvedPointer `json:"pointers,omitempty"`
}

// MemoryAgentSource 表示统一检索中的可追溯来源。
type MemoryAgentSource struct {
	Kind       MemorySourceKind `json:"kind"`
	ID         string           `json:"id"`
	Title      string           `json:"title,omitempty"`
	Summary    string           `json:"summary,omitempty"`
	Content    string           `json:"content,omitempty"`
	SessionID  string           `json:"session_id,omitempty"`
	Timestamp  int64            `json:"timestamp,omitempty"`
	NodeID     string           `json:"node_id,omitempty"`
	NodeLabel  string           `json:"node_label,omitempty"`
	SourceID   string           `json:"source_id,omitempty"`
	SourceType string           `json:"source_type,omitempty"`
	FilePath   string           `json:"file_path,omitempty"`
	LineRange  string           `json:"line_range,omitempty"`
	Relevance  float64          `json:"relevance,omitempty"`
}

// IngestResult MemoryAgent 写入结果。
type IngestResult struct {
	RawArchiveID   string `json:"raw_archive_id"`
	AtomicMemoryID string `json:"atomic_memory_id"`
	SAMGTripleCount int   `json:"samg_triples_count,omitempty"`
}

// RetrieveRequest MemoryAgent 统一检索请求。
type RetrieveRequest struct {
	Query      string  `json:"query"`
	MaxResults int     `json:"max_results,omitempty"`
	MinHeat    float64 `json:"min_heat,omitempty"`
	Tier       string  `json:"tier,omitempty"`
	SessionID  string  `json:"session_id,omitempty"`
}

// RetrieveResult MemoryAgent 检索结果。
type RetrieveResult struct {
	AtomicMemories []AtomicMemory      `json:"atomic_memories"`
	SAMGNodes      []MemoryAgentNode   `json:"samg_nodes,omitempty"`
	Sources        []MemoryAgentSource `json:"sources,omitempty"`
	TotalFound     int                 `json:"total_found"`
}

// ContextRequest MemoryAgent 上下文组装请求。
type ContextRequest struct {
	SessionID string `json:"session_id"`
	Query     string `json:"query,omitempty"`
	MaxTokens int    `json:"max_tokens,omitempty"`
}

// ContextResult MemoryAgent 上下文组装结果。
type ContextResult struct {
	ContextBlock   string              `json:"context_block"`
	SourceCount    int                 `json:"source_count"`
	AtomicMemories []AtomicMemory      `json:"atomic_memories,omitempty"`
	SAMGNodes      []MemoryAgentNode   `json:"samg_nodes,omitempty"`
	Sources        []MemoryAgentSource `json:"sources,omitempty"`
}
