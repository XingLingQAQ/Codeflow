// Package memory - Memory preflight implementation
package memory

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// MemoryPreflightService implements IMemoryPreflight.
type MemoryPreflightService struct {
	memories map[string]*MemoryMatch // In-memory storage for demo
	mu       sync.RWMutex
}

// NewMemoryPreflightService creates a new memory preflight service.
func NewMemoryPreflightService() *MemoryPreflightService {
	return &MemoryPreflightService{
		memories: make(map[string]*MemoryMatch),
	}
}

// Preflight performs a preflight check and returns matching memories.
func (s *MemoryPreflightService) Preflight(req *PreflightRequest) (*PreflightResponse, error) {
	start := time.Now()

	// Extract keywords from query
	keywords := s.ExtractKeywords(req.Query)

	// Set defaults
	if req.MaxResults == 0 {
		req.MaxResults = 10
	}
	if req.MinScore == 0 {
		req.MinScore = 0.3
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// Simple keyword-based matching (in production, use vector search)
	matches := make([]*MemoryMatch, 0)
	for _, memory := range s.memories {
		score := s.calculateScore(keywords, memory)
		if score >= req.MinScore {
			// Filter by sources if specified
			if len(req.Sources) > 0 {
				found := false
				for _, source := range req.Sources {
					if strings.Contains(memory.Source, source) {
						found = true
						break
					}
				}
				if !found {
					continue
				}
			}

			matchCopy := *memory
			matchCopy.Score = score
			matches = append(matches, &matchCopy)
		}
	}

	// Sort by score (descending)
	for i := 0; i < len(matches)-1; i++ {
		for j := i + 1; j < len(matches); j++ {
			if matches[i].Score < matches[j].Score {
				matches[i], matches[j] = matches[j], matches[i]
			}
		}
	}

	// Limit results
	if len(matches) > req.MaxResults {
		matches = matches[:req.MaxResults]
	}

	return &PreflightResponse{
		Matches:      matches,
		TotalMatches: len(matches),
		Duration:     time.Since(start),
		Keywords:     keywords,
	}, nil
}

// calculateScore calculates relevance score based on keyword matching.
func (s *MemoryPreflightService) calculateScore(keywords []string, memory *MemoryMatch) float64 {
	if len(keywords) == 0 {
		return 0
	}

	contentLower := strings.ToLower(memory.Content)
	matchCount := 0

	for _, keyword := range keywords {
		if strings.Contains(contentLower, strings.ToLower(keyword)) {
			matchCount++
		}
	}

	score := float64(matchCount) / float64(len(keywords))

	// Boost strong matches
	if memory.Strength == MatchStrong {
		score *= 1.5
		if score > 1.0 {
			score = 1.0
		}
	}

	return score
}

// ExtractKeywords extracts keywords from text.
func (s *MemoryPreflightService) ExtractKeywords(text string) []string {
	// Simple keyword extraction (in production, use NLP)
	words := strings.Fields(strings.ToLower(text))
	keywords := make([]string, 0)

	// Filter out common stop words
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "and": true, "or": true,
		"but": true, "in": true, "on": true, "at": true, "to": true,
		"for": true, "of": true, "with": true, "by": true, "from": true,
		"is": true, "are": true, "was": true, "were": true, "be": true,
	}

	seen := make(map[string]bool)
	for _, word := range words {
		// Remove punctuation
		word = strings.Trim(word, ".,!?;:\"'")
		if len(word) > 2 && !stopWords[word] && !seen[word] {
			keywords = append(keywords, word)
			seen[word] = true
		}
	}

	return keywords
}

// InjectMemory injects a memory into the context.
func (s *MemoryPreflightService) InjectMemory(contextID string, memoryID string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, exists := s.memories[memoryID]; !exists {
		return fmt.Errorf("memory %s not found", memoryID)
	}

	// In production, this would inject into actual context storage
	return nil
}

// InjectMemories injects multiple memories into the context.
func (s *MemoryPreflightService) InjectMemories(contextID string, memoryIDs []string) error {
	for _, memoryID := range memoryIDs {
		if err := s.InjectMemory(contextID, memoryID); err != nil {
			return err
		}
	}
	return nil
}

// GetSuggestions returns memory suggestions based on context.
func (s *MemoryPreflightService) GetSuggestions(contextID string, limit int) ([]*MemoryMatch, error) {
	// In production, this would analyze context and return relevant suggestions
	s.mu.RLock()
	defer s.mu.RUnlock()

	suggestions := make([]*MemoryMatch, 0)
	count := 0
	for _, memory := range s.memories {
		if count >= limit {
			break
		}
		memoryCopy := *memory
		suggestions = append(suggestions, &memoryCopy)
		count++
	}

	return suggestions, nil
}

// AddMemory adds a memory for testing/demo purposes.
func (s *MemoryPreflightService) AddMemory(memory *MemoryMatch) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.memories[memory.ID] = memory
}

// Global service instance
var defaultPreflightService IMemoryPreflight

// GetPreflightService returns the global preflight service instance.
func GetPreflightService() IMemoryPreflight {
	if defaultPreflightService == nil {
		defaultPreflightService = NewMemoryPreflightService()
	}
	return defaultPreflightService
}

// SetPreflightService sets the global preflight service instance (for testing).
func SetPreflightService(svc IMemoryPreflight) {
	defaultPreflightService = svc
}
