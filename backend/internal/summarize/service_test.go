// Package summarize - Summarizer tests
package summarize

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSummarizeConversation(t *testing.T) {
	svc := NewSummarizerService()

	messages := []Message{
		{Role: "user", Content: "Let's implement a new feature for user authentication.", Timestamp: time.Now()},
		{Role: "assistant", Content: "I'll create the authentication module with JWT support.", Timestamp: time.Now()},
		{Role: "user", Content: "Also add password hashing with bcrypt.", Timestamp: time.Now()},
		{Role: "assistant", Content: "Implemented authentication with JWT and bcrypt hashing.", Timestamp: time.Now()},
	}

	req := &SummarizeRequest{
		Messages:          messages,
		CompressionTarget: 0.6,
	}

	summary, err := svc.SummarizeConversation(req)
	assert.NoError(t, err)
	assert.NotNil(t, summary)
	assert.Equal(t, 4, summary.OriginalMessages)
	assert.Greater(t, len(summary.SummaryText), 0)
	assert.Greater(t, len(summary.KeyPoints), 0)
	assert.Equal(t, 0.6, summary.CompressionRatio)
}

func TestSummarizeConversation_Empty(t *testing.T) {
	svc := NewSummarizerService()

	req := &SummarizeRequest{
		Messages: []Message{},
	}

	summary, err := svc.SummarizeConversation(req)
	assert.Error(t, err)
	assert.Nil(t, summary)
}

func TestCompressContext(t *testing.T) {
	svc := NewSummarizerService()

	context := `This is a long conversation about implementing a new feature.
We discussed various approaches and decided to use microservices architecture.
The implementation will use Go for the backend and React for the frontend.
We also talked about database choices and decided on PostgreSQL.
Recent discussion: We need to add authentication and authorization.
The user wants JWT-based authentication with refresh tokens.
We should implement rate limiting to prevent abuse.`

	req := &CompressRequest{
		Context:           context,
		CompressionRatio:  0.6,
		PreserveRecentPct: 0.2,
	}

	result, err := svc.CompressContext(req)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Greater(t, result.OriginalTokens, 0)
	assert.Greater(t, result.CompressedTokens, 0)
	assert.Less(t, result.CompressedTokens, result.OriginalTokens)
	assert.Greater(t, result.CompressionRatio, 0.0)
	assert.Greater(t, len(result.Summary), 0)
	assert.Greater(t, len(result.RecentContext), 0)
}

func TestCompressContext_Empty(t *testing.T) {
	svc := NewSummarizerService()

	req := &CompressRequest{
		Context: "",
	}

	result, err := svc.CompressContext(req)
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestCompressContext_60PercentReduction(t *testing.T) {
	svc := NewSummarizerService()

	// Create a context with known token count
	context := strings.Repeat("word ", 1000) // ~1000 tokens

	req := &CompressRequest{
		Context:           context,
		CompressionRatio:  0.6,
		PreserveRecentPct: 0.2,
	}

	result, err := svc.CompressContext(req)
	assert.NoError(t, err)
	assert.NotNil(t, result)

	// Verify compression ratio is close to target (60%)
	// Allow some variance due to sentence boundaries
	assert.Greater(t, result.CompressionRatio, 0.4) // At least 40% reduction
	assert.Less(t, result.CompressionRatio, 0.8)    // At most 80% reduction
}

func TestExtractSkeleton(t *testing.T) {
	svc := NewSummarizerService()

	messages := []Message{
		{Role: "user", Content: "We decided to use microservices architecture for scalability.", Timestamp: time.Now()},
		{Role: "assistant", Content: "I'll implement the service in backend/internal/auth/service.go", Timestamp: time.Now()},
		{Role: "user", Content: "There's a bug in the login function that needs to be fixed.", Timestamp: time.Now()},
		{Role: "assistant", Content: "const API_KEY = 'secret123' should be moved to environment variables.", Timestamp: time.Now()},
	}

	skeleton, err := svc.ExtractSkeleton(messages)
	assert.NoError(t, err)
	assert.NotNil(t, skeleton)
	assert.Greater(t, len(skeleton.ArchitectureDecisions), 0)
	assert.Greater(t, len(skeleton.UnfixedBugs), 0)
	assert.Greater(t, len(skeleton.VariableDefinitions), 0)
	assert.Greater(t, len(skeleton.KeyFiles), 0)
}

func TestExtractSkeleton_Empty(t *testing.T) {
	svc := NewSummarizerService()

	skeleton, err := svc.ExtractSkeleton([]Message{})
	assert.Error(t, err)
	assert.Nil(t, skeleton)
}

func TestExtractSkeleton_ArchitectureDecisions(t *testing.T) {
	svc := NewSummarizerService()

	messages := []Message{
		{Role: "user", Content: "We decided to use Redis for caching.", Timestamp: time.Now()},
		{Role: "user", Content: "Chose PostgreSQL over MySQL for better JSON support.", Timestamp: time.Now()},
	}

	skeleton, err := svc.ExtractSkeleton(messages)
	assert.NoError(t, err)
	assert.Len(t, skeleton.ArchitectureDecisions, 2)
}

func TestExtractSkeleton_UnfixedBugs(t *testing.T) {
	svc := NewSummarizerService()

	messages := []Message{
		{Role: "user", Content: "There's a bug in the authentication flow.", Timestamp: time.Now()},
		{Role: "user", Content: "TODO: Fix the memory leak in the cache.", Timestamp: time.Now()},
		{Role: "user", Content: "Error: Connection timeout needs investigation.", Timestamp: time.Now()},
	}

	skeleton, err := svc.ExtractSkeleton(messages)
	assert.NoError(t, err)
	assert.Len(t, skeleton.UnfixedBugs, 3)
}

func TestExtractSkeleton_KeyFiles(t *testing.T) {
	svc := NewSummarizerService()

	messages := []Message{
		{Role: "assistant", Content: "Modified backend/internal/auth/service.go", Timestamp: time.Now()},
		{Role: "assistant", Content: "Created frontend/src/components/Login.tsx", Timestamp: time.Now()},
	}

	skeleton, err := svc.ExtractSkeleton(messages)
	assert.NoError(t, err)
	assert.Len(t, skeleton.KeyFiles, 2)
}

func TestCalculateTokens(t *testing.T) {
	svc := NewSummarizerService()

	// Test with known text
	text := "This is a test sentence with approximately twenty characters per word."
	tokens := svc.CalculateTokens(text)

	// Should be roughly len(text) / 4
	expectedTokens := len(text) / 4
	assert.Equal(t, expectedTokens, tokens)
}

func TestCalculateTokens_Empty(t *testing.T) {
	svc := NewSummarizerService()

	tokens := svc.CalculateTokens("")
	assert.Equal(t, 0, tokens)
}

func TestGetSummarizer(t *testing.T) {
	svc := GetSummarizer()
	assert.NotNil(t, svc)

	// Should return same instance
	svc2 := GetSummarizer()
	assert.Equal(t, svc, svc2)
}

func TestSetSummarizer(t *testing.T) {
	original := GetSummarizer()

	// Set custom instance
	custom := NewSummarizerService()
	SetSummarizer(custom)

	retrieved := GetSummarizer()
	assert.Equal(t, custom, retrieved)

	// Restore original
	SetSummarizer(original)
}
