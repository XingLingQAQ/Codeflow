// Package memory - Memory preflight types
package memory

import "time"

// MatchStrength represents the strength of a memory match.
type MatchStrength string

const (
	MatchStrong MatchStrength = "strong" // .claude/rules, explicit references
	MatchWeak   MatchStrength = "weak"   // Historical conversations
)

// MemoryMatch represents a matched memory item.
type MemoryMatch struct {
	ID          string        `json:"id"`
	Content     string        `json:"content"`
	Source      string        `json:"source"`      // e.g., ".claude/rules", "conversation:123"
	Strength    MatchStrength `json:"strength"`
	Score       float64       `json:"score"`       // Relevance score (0-1)
	Keywords    []string      `json:"keywords"`
	Timestamp   time.Time     `json:"timestamp"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// PreflightRequest represents a preflight check request.
type PreflightRequest struct {
	Query       string   `json:"query"`
	Context     string   `json:"context,omitempty"`
	MaxResults  int      `json:"max_results,omitempty"`
	MinScore    float64  `json:"min_score,omitempty"`
	Sources     []string `json:"sources,omitempty"` // Filter by sources
}

// PreflightResponse represents a preflight check response.
type PreflightResponse struct {
	Matches      []*MemoryMatch `json:"matches"`
	TotalMatches int            `json:"total_matches"`
	Duration     time.Duration  `json:"duration"`
	Keywords     []string       `json:"keywords"` // Extracted keywords
}

// IMemoryPreflight defines the interface for memory preflight operations.
type IMemoryPreflight interface {
	// Preflight performs a preflight check and returns matching memories.
	Preflight(req *PreflightRequest) (*PreflightResponse, error)

	// ExtractKeywords extracts keywords from text.
	ExtractKeywords(text string) []string

	// InjectMemory injects a memory into the context.
	InjectMemory(contextID string, memoryID string) error

	// InjectMemories injects multiple memories into the context.
	InjectMemories(contextID string, memoryIDs []string) error

	// GetSuggestions returns memory suggestions based on context.
	GetSuggestions(contextID string, limit int) ([]*MemoryMatch, error)
}
