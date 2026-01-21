// Package memory - Memory preflight tests
package memory

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestMemoryPreflightService_Preflight(t *testing.T) {
	svc := NewMemoryPreflightService()

	// Add test memories
	svc.AddMemory(&MemoryMatch{
		ID:       "mem1",
		Content:  "Use React hooks for state management",
		Source:   ".claude/rules",
		Strength: MatchStrong,
		Keywords: []string{"react", "hooks", "state"},
	})

	svc.AddMemory(&MemoryMatch{
		ID:       "mem2",
		Content:  "Previous discussion about React components",
		Source:   "conversation:123",
		Strength: MatchWeak,
		Keywords: []string{"react", "components"},
	})

	req := &PreflightRequest{
		Query:      "How to use React hooks?",
		MaxResults: 10,
		MinScore:   0.3,
	}

	resp, err := svc.Preflight(req)
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Greater(t, len(resp.Matches), 0)
	assert.Less(t, resp.Duration, 200*time.Millisecond)
}

func TestMemoryPreflightService_ExtractKeywords(t *testing.T) {
	svc := NewMemoryPreflightService()

	keywords := svc.ExtractKeywords("How to use React hooks for state management?")
	assert.Contains(t, keywords, "react")
	assert.Contains(t, keywords, "hooks")
	assert.Contains(t, keywords, "state")
	assert.Contains(t, keywords, "management")
	assert.NotContains(t, keywords, "to") // Stop word
}

func TestMemoryPreflightService_InjectMemory(t *testing.T) {
	svc := NewMemoryPreflightService()

	svc.AddMemory(&MemoryMatch{
		ID:      "mem1",
		Content: "Test memory",
		Source:  ".claude/rules",
	})

	err := svc.InjectMemory("ctx1", "mem1")
	assert.NoError(t, err)

	err = svc.InjectMemory("ctx1", "nonexistent")
	assert.Error(t, err)
}

func TestMemoryPreflightService_InjectMemories(t *testing.T) {
	svc := NewMemoryPreflightService()

	svc.AddMemory(&MemoryMatch{ID: "mem1", Content: "Memory 1", Source: ".claude/rules"})
	svc.AddMemory(&MemoryMatch{ID: "mem2", Content: "Memory 2", Source: ".claude/rules"})

	err := svc.InjectMemories("ctx1", []string{"mem1", "mem2"})
	assert.NoError(t, err)
}

func TestMemoryPreflightService_GetSuggestions(t *testing.T) {
	svc := NewMemoryPreflightService()

	svc.AddMemory(&MemoryMatch{ID: "mem1", Content: "Memory 1", Source: ".claude/rules"})
	svc.AddMemory(&MemoryMatch{ID: "mem2", Content: "Memory 2", Source: ".claude/rules"})

	suggestions, err := svc.GetSuggestions("ctx1", 10)
	assert.NoError(t, err)
	assert.Len(t, suggestions, 2)
}

func TestMemoryPreflightService_MatchStrength(t *testing.T) {
	svc := NewMemoryPreflightService()

	// Strong match should have higher score
	svc.AddMemory(&MemoryMatch{
		ID:       "strong",
		Content:  "React hooks tutorial guide",
		Source:   ".claude/rules",
		Strength: MatchStrong,
	})

	svc.AddMemory(&MemoryMatch{
		ID:       "weak",
		Content:  "React hooks tutorial guide",
		Source:   "conversation:123",
		Strength: MatchWeak,
	})

	req := &PreflightRequest{
		Query:      "React hooks tutorial",
		MaxResults: 10,
		MinScore:   0.1,
	}

	resp, err := svc.Preflight(req)
	assert.NoError(t, err)
	assert.Len(t, resp.Matches, 2)

	// Strong match should be first (higher score)
	assert.Equal(t, "strong", resp.Matches[0].ID)
	assert.GreaterOrEqual(t, resp.Matches[0].Score, resp.Matches[1].Score)
}

func TestMemoryPreflightService_SourceFilter(t *testing.T) {
	svc := NewMemoryPreflightService()

	svc.AddMemory(&MemoryMatch{
		ID:      "mem1",
		Content: "React hooks",
		Source:  ".claude/rules",
	})

	svc.AddMemory(&MemoryMatch{
		ID:      "mem2",
		Content: "React hooks",
		Source:  "conversation:123",
	})

	req := &PreflightRequest{
		Query:      "React hooks",
		MaxResults: 10,
		Sources:    []string{".claude/rules"},
	}

	resp, err := svc.Preflight(req)
	assert.NoError(t, err)
	assert.Len(t, resp.Matches, 1)
	assert.Equal(t, "mem1", resp.Matches[0].ID)
}

func TestMemoryPreflightService_Performance(t *testing.T) {
	svc := NewMemoryPreflightService()

	// Add 100 memories
	for i := 0; i < 100; i++ {
		svc.AddMemory(&MemoryMatch{
			ID:      fmt.Sprintf("mem%d", i),
			Content: fmt.Sprintf("Memory content %d with React hooks", i),
			Source:  ".claude/rules",
		})
	}

	req := &PreflightRequest{
		Query:      "React hooks",
		MaxResults: 10,
	}

	start := time.Now()
	resp, err := svc.Preflight(req)
	duration := time.Since(start)

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Less(t, duration, 200*time.Millisecond, "Preflight should complete in < 200ms")
}
