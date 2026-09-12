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
//
// T13.04.b 接线（E-06/E-07，§31.3）：
//   - 参数经 ResolveSummarizeParams 校验/补齐：显式 0 与省略区分，非法值返回 *ValidationError。
//   - preserve_recent 对消息生效：最近 N 条原样保留进 PreservedMessages，SummaryText 只总结
//     可压缩区；compression_target 驱动本地 extractive 摘要的收录预算。
//   - 统计为实测：compressed_tokens 覆盖完整返回语义内容（摘要正文+SummaryMarker+保留区），
//     CompressionRatio = 1-compressed/original，不回填请求目标。
//   - 默认模式为本地 extractive（Mode=local_extractive），不做高置信语义总结。
func (s *SummarizerService) SummarizeConversation(req *SummarizeRequest) (*ConversationSummary, error) {
	if len(req.Messages) == 0 {
		return nil, fmt.Errorf("no messages to summarize")
	}

	resolved, err := ResolveSummarizeParams(SummarizeParams{
		MessageCount:      len(req.Messages),
		CompressionTarget: req.CompressionTarget,
		PreserveRecent:    req.PreserveRecent,
	})
	if err != nil {
		return nil, err
	}

	splitAt := len(req.Messages) - resolved.PreserveRecent
	compressible := req.Messages[:splitAt]
	preserved := append([]Message(nil), req.Messages[splitAt:]...)

	keyPoints := s.extractKeyPoints(compressible)
	summaryBody := s.generateSummary(compressible, keyPoints, resolved.CompressionTarget)
	marker := ""
	if summaryBody != "" {
		marker = SummaryMarker
	}
	summaryText := summaryBody + marker

	originalTokens := s.countMessageContents(req.Messages)
	compressedTokens := MeasureCompressedOutput(s.tokenCounter, summaryBody, marker, joinMessageContents(preserved))

	return &ConversationSummary{
		OriginalMessages:  len(req.Messages),
		SummaryText:       summaryText,
		KeyPoints:         keyPoints,
		Timestamp:         time.Now(),
		CompressionRatio:  MeasuredCompressionRatio(originalTokens, compressedTokens),
		PreservedMessages: preserved,
		Mode:              ModeLocalExtractive,
		TokenCountQuality: TokenCountQualityEstimated,
		OriginalTokens:    originalTokens,
		CompressedTokens:  compressedTokens,
	}, nil
}

// CompressContext compresses context using 80/20 strategy.
//
// T13.04.b 接线（E-06/E-07，§31.3）：
//   - 参数经 ResolveCompressParams 校验：比例越界/NaN、target_tokens 非正整数返回 *ValidationError。
//   - preserve_recent_pct 对文本生效：SplitByPreservePct 按合法 UTF-8 边界切出尾部保留区，
//     多字节字符（中文/emoji）不会被截坏。
//   - 显式 target_tokens 优先限定总返回预算：保留区+SummaryMarker（最小合法表示）超预算返回
//     *BudgetUnsatisfiableError（不截坏保留区）；摘要正文在剩余预算内按 rune 安全边界收敛。
//   - 统计为实测：compressed_tokens 覆盖 Summary（含标记）+RecentContext 全部返回内容，
//     CompressionRatio = 1-compressed/original，不回填请求比例。
func (s *SummarizerService) CompressContext(req *CompressRequest) (*ContextCompression, error) {
	if req.Context == "" {
		return nil, fmt.Errorf("context is empty")
	}

	resolved, err := ResolveCompressParams(CompressParams{
		CompressionRatio:  req.CompressionRatio,
		PreserveRecentPct: req.PreserveRecentPct,
		TargetTokens:      req.TargetTokens,
	})
	if err != nil {
		return nil, err
	}

	compressible, recent := SplitByPreservePct(req.Context, resolved.PreserveRecentPct)

	// 最小合法输出 = 保留区 + 摘要标记；其本身超预算时明确拒绝，不截坏保留区。
	if err := CheckOutputBudget(resolved.TargetTokens, resolved.HasTargetTokens,
		MeasureCompressedOutput(s.tokenCounter, "", SummaryMarker, recent)); err != nil {
		return nil, err
	}

	summaryBody := s.summarizeText(compressible, resolved.CompressionRatio)
	if resolved.HasTargetTokens {
		summaryBody = s.fitSummaryToBudget(summaryBody, recent, resolved.TargetTokens)
	}

	originalTokens := s.CalculateTokens(req.Context)
	compressedTokens := MeasureCompressedOutput(s.tokenCounter, summaryBody, SummaryMarker, recent)

	return &ContextCompression{
		OriginalTokens:    originalTokens,
		CompressedTokens:  compressedTokens,
		CompressionRatio:  MeasuredCompressionRatio(originalTokens, compressedTokens),
		Summary:           summaryBody + SummaryMarker,
		RecentContext:     recent,
		Timestamp:         time.Now(),
		Mode:              ModeLocalExtractive,
		TokenCountQuality: TokenCountQualityEstimated,
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

// generateSummary 本地 extractive 摘要（不调用模型）：头部说明 + 在 (1-target)
// 内容字节预算内尽量收录关键句。显式 target=0 表示零压缩目标，预算覆盖全部正文。
func (s *SummarizerService) generateSummary(messages []Message, keyPoints []string, compressionTarget float64) string {
	if len(messages) == 0 {
		return ""
	}

	contentBytes := 0
	for _, msg := range messages {
		contentBytes += len(msg.Content)
	}
	budget := int(float64(contentBytes) * (1.0 - compressionTarget))

	included := make([]string, 0, len(keyPoints))
	used := 0
	for _, kp := range keyPoints {
		if len(included) > 0 && used+len(kp) > budget {
			break
		}
		included = append(included, kp)
		used += len(kp)
	}

	summary := fmt.Sprintf("Conversation with %d messages", len(messages))
	if len(included) > 0 {
		summary += ". Key points: " + strings.Join(included, "; ")
	}
	return summary
}

// summarizeText 本地 extractive 文本摘要：按 (1-ratio) 字节预算截取可压缩区前缀，
// 切分点回退到合法 UTF-8 边界，并优先对齐最近的英文句点；显式 ratio=0 原样返回。
func (s *SummarizerService) summarizeText(text string, compressionRatio float64) string {
	if text == "" {
		return ""
	}
	targetLength := int(float64(len(text)) * (1.0 - compressionRatio))
	if targetLength >= len(text) {
		return text
	}

	summary := TruncateUTF8(text, targetLength)
	if lastPeriod := strings.LastIndex(summary, "."); lastPeriod > 0 {
		summary = summary[:lastPeriod+1]
	}
	return summary
}

// fitSummaryToBudget 把摘要正文收敛到显式 target_tokens 总预算内：
// 计量覆盖 摘要正文+SummaryMarker+保留区；超长时按 rune 安全前缀折半收缩并回退句点。
// 收缩到空仍超预算的情形已由 CheckOutputBudget 提前拒绝，故此循环必然终止于可行解。
func (s *SummarizerService) fitSummaryToBudget(summary, recent string, targetTokens int) string {
	for len(summary) > 0 && MeasureCompressedOutput(s.tokenCounter, summary, SummaryMarker, recent) > targetTokens {
		summary = TruncateUTF8(summary, len(summary)/2)
		if lastPeriod := strings.LastIndex(summary, "."); lastPeriod > 0 {
			summary = summary[:lastPeriod+1]
		}
	}
	return summary
}

// countMessageContents 估算消息列表正文的 token 总量（与输出计量同一 TokenCounter）。
func (s *SummarizerService) countMessageContents(messages []Message) int {
	total := 0
	for _, msg := range messages {
		total += s.CalculateTokens(msg.Content)
	}
	return total
}

// joinMessageContents 拼接保留消息正文，用于完整返回内容的整体计量。
func joinMessageContents(messages []Message) string {
	parts := make([]string, 0, len(messages))
	for _, msg := range messages {
		parts = append(parts, msg.Content)
	}
	return strings.Join(parts, "\n")
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
