// Package summarize - Conversation summarization implementation
package summarize

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// SummarizerService implements ISummarizer (HTTP API surface).
// Message-level compression engine lives alongside in this package (Compressor).
type SummarizerService struct {
	mu           sync.RWMutex
	tokenCounter *TokenCounter
}

// NewSummarizerService creates a new summarizer service.
func NewSummarizerService() *SummarizerService {
	return &SummarizerService{
		tokenCounter: NewTokenCounter(nil),
	}
}

// SummarizeConversation summarizes a conversation.
func (s *SummarizerService) SummarizeConversation(req *SummarizeRequest) (*ConversationSummary, error) {
	if len(req.Messages) == 0 {
		return nil, fmt.Errorf("no messages to summarize")
	}

	// Set defaults
	compressionTarget := req.CompressionTarget
	if compressionTarget == 0 {
		compressionTarget = 0.6 // 60% compression
	}

	preserveRecent := req.PreserveRecent
	if preserveRecent == 0 {
		preserveRecent = int(float64(len(req.Messages)) * 0.2) // Last 20%
	}

	// Extract key points from messages
	keyPoints := s.extractKeyPoints(req.Messages)

	// Generate summary text (in production, use LLM)
	summaryText := s.generateSummary(req.Messages, keyPoints)

	return &ConversationSummary{
		OriginalMessages: len(req.Messages),
		SummaryText:      summaryText,
		KeyPoints:        keyPoints,
		Timestamp:        time.Now(),
		CompressionRatio: compressionTarget,
	}, nil
}

// CompressContext compresses context using 80/20 strategy.
func (s *SummarizerService) CompressContext(req *CompressRequest) (*ContextCompression, error) {
	if req.Context == "" {
		return nil, fmt.Errorf("context is empty")
	}

	// Set defaults
	compressionRatio := req.CompressionRatio
	if compressionRatio == 0 {
		compressionRatio = 0.6 // 60% compression
	}

	preserveRecentPct := req.PreserveRecentPct
	if preserveRecentPct == 0 {
		preserveRecentPct = 0.2 // Last 20%
	}

	// Calculate original tokens
	originalTokens := s.CalculateTokens(req.Context)

	// Split context into 80% and 20%
	splitPoint := int(float64(len(req.Context)) * (1.0 - preserveRecentPct))
	firstPart := req.Context[:splitPoint]
	recentPart := req.Context[splitPoint:]

	// Summarize first 80% (in production, use LLM)
	summary := s.summarizeText(firstPart, compressionRatio)

	// Calculate compressed tokens
	compressedTokens := s.CalculateTokens(summary + recentPart)

	// Calculate actual compression ratio
	actualRatio := 1.0 - (float64(compressedTokens) / float64(originalTokens))

	return &ContextCompression{
		OriginalTokens:   originalTokens,
		CompressedTokens: compressedTokens,
		CompressionRatio: actualRatio,
		Summary:          summary,
		RecentContext:    recentPart,
		Timestamp:        time.Now(),
	}, nil
}

// ExtractSkeleton extracts decision skeleton from conversation.
func (s *SummarizerService) ExtractSkeleton(messages []Message) (*DecisionSkeleton, error) {
	if len(messages) == 0 {
		return nil, fmt.Errorf("no messages to extract skeleton from")
	}

	skeleton := &DecisionSkeleton{
		ArchitectureDecisions: make([]string, 0),
		UnfixedBugs:           make([]string, 0),
		VariableDefinitions:   make(map[string]string),
		KeyFiles:              make([]string, 0),
		Timestamp:             time.Now(),
	}

	// Extract key information from messages
	for _, msg := range messages {
		content := strings.ToLower(msg.Content)

		// Architecture decisions (keywords: "decided", "chose", "architecture", "design")
		if strings.Contains(content, "decided") || strings.Contains(content, "chose") ||
			strings.Contains(content, "architecture") || strings.Contains(content, "design") {
			skeleton.ArchitectureDecisions = append(skeleton.ArchitectureDecisions, s.extractDecision(msg.Content))
		}

		// Unfixed bugs (keywords: "bug", "issue", "error", "todo", "fixme")
		if strings.Contains(content, "bug") || strings.Contains(content, "issue") ||
			strings.Contains(content, "error") || strings.Contains(content, "todo") ||
			strings.Contains(content, "fixme") {
			skeleton.UnfixedBugs = append(skeleton.UnfixedBugs, s.extractBug(msg.Content))
		}

		// Variable definitions (keywords: "const", "var", "let", "define")
		if strings.Contains(content, "const ") || strings.Contains(content, "var ") ||
			strings.Contains(content, "let ") || strings.Contains(content, "define") {
			s.extractVariables(msg.Content, skeleton.VariableDefinitions)
		}

		// Key files (file paths)
		files := s.extractFilePaths(msg.Content)
		skeleton.KeyFiles = append(skeleton.KeyFiles, files...)
	}

	// Deduplicate
	skeleton.ArchitectureDecisions = s.deduplicate(skeleton.ArchitectureDecisions)
	skeleton.UnfixedBugs = s.deduplicate(skeleton.UnfixedBugs)
	skeleton.KeyFiles = s.deduplicate(skeleton.KeyFiles)

	return skeleton, nil
}

// CalculateTokens estimates token count for text (EN/ZH heuristics via TokenCounter).
func (s *SummarizerService) CalculateTokens(text string) int {
	if s.tokenCounter == nil {
		s.tokenCounter = NewTokenCounter(nil)
	}
	return s.tokenCounter.Count(text)
}

// Helper functions

func (s *SummarizerService) extractKeyPoints(messages []Message) []string {
	keyPoints := make([]string, 0)
	seen := make(map[string]bool)

	for _, msg := range messages {
		// Extract sentences that look like key points
		sentences := strings.Split(msg.Content, ".")
		for _, sentence := range sentences {
			sentence = strings.TrimSpace(sentence)
			if len(sentence) > 20 && len(sentence) < 200 && !seen[sentence] {
				// Check if it contains important keywords
				lower := strings.ToLower(sentence)
				if strings.Contains(lower, "implement") || strings.Contains(lower, "create") ||
					strings.Contains(lower, "fix") || strings.Contains(lower, "update") ||
					strings.Contains(lower, "add") || strings.Contains(lower, "remove") {
					keyPoints = append(keyPoints, sentence)
					seen[sentence] = true
					if len(keyPoints) >= 10 {
						break
					}
				}
			}
		}
		if len(keyPoints) >= 10 {
			break
		}
	}

	return keyPoints
}

func (s *SummarizerService) generateSummary(messages []Message, keyPoints []string) string {
	// In production, this would call an LLM
	// For demo, create a simple summary
	summary := fmt.Sprintf("Conversation with %d messages. ", len(messages))
	if len(keyPoints) > 0 {
		summary += "Key points: " + strings.Join(keyPoints[:min(3, len(keyPoints))], "; ")
	}
	return summary
}

func (s *SummarizerService) summarizeText(text string, compressionRatio float64) string {
	// In production, this would call an LLM
	// For demo, extract first few sentences
	targetLength := int(float64(len(text)) * (1.0 - compressionRatio))
	if targetLength > len(text) {
		return text
	}

	// Find sentence boundary near target length
	summary := text[:targetLength]
	lastPeriod := strings.LastIndex(summary, ".")
	if lastPeriod > 0 {
		summary = summary[:lastPeriod+1]
	}

	return summary + " [summarized]"
}

func (s *SummarizerService) extractDecision(content string) string {
	// Extract decision-related sentences
	sentences := strings.Split(content, ".")
	for _, sentence := range sentences {
		lower := strings.ToLower(sentence)
		if strings.Contains(lower, "decided") || strings.Contains(lower, "chose") {
			return strings.TrimSpace(sentence)
		}
	}
	return strings.TrimSpace(content[:min(200, len(content))])
}

func (s *SummarizerService) extractBug(content string) string {
	// Extract bug-related sentences
	sentences := strings.Split(content, ".")
	for _, sentence := range sentences {
		lower := strings.ToLower(sentence)
		if strings.Contains(lower, "bug") || strings.Contains(lower, "issue") ||
			strings.Contains(lower, "error") {
			return strings.TrimSpace(sentence)
		}
	}
	return strings.TrimSpace(content[:min(200, len(content))])
}

func (s *SummarizerService) extractVariables(content string, vars map[string]string) {
	// Simple variable extraction (demo version)
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		if strings.Contains(line, "const ") || strings.Contains(line, "var ") ||
			strings.Contains(line, "let ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				varName := strings.TrimSuffix(parts[1], ":")
				vars[varName] = strings.TrimSpace(line)
			}
		}
	}
}

func (s *SummarizerService) extractFilePaths(content string) []string {
	files := make([]string, 0)
	words := strings.Fields(content)

	for _, word := range words {
		// Check if it looks like a file path
		if strings.Contains(word, "/") || strings.Contains(word, "\\") {
			if strings.Contains(word, ".go") || strings.Contains(word, ".ts") ||
				strings.Contains(word, ".js") || strings.Contains(word, ".py") ||
				strings.Contains(word, ".java") {
				files = append(files, word)
			}
		}
	}

	return files
}

func (s *SummarizerService) deduplicate(items []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0)

	for _, item := range items {
		if !seen[item] && item != "" {
			seen[item] = true
			result = append(result, item)
		}
	}

	return result
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Global service instance
var defaultSummarizer ISummarizer

// GetSummarizer returns the global summarizer instance.
func GetSummarizer() ISummarizer {
	if defaultSummarizer == nil {
		defaultSummarizer = NewSummarizerService()
	}
	return defaultSummarizer
}

// SetSummarizer sets the global summarizer instance (for testing).
func SetSummarizer(svc ISummarizer) {
	defaultSummarizer = svc
}
