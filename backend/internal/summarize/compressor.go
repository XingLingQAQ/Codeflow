package summarize

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/codeflow/backend/internal/adapters"
	backendhooks "github.com/codeflow/backend/internal/hooks"
)

// Compressor performs message-level context compression.
type Compressor struct {
	tokenCounter   *TokenCounter
	summaryAdapter adapters.ICliAdapter
	config         CompressionConfig
}

// NewCompressor creates a compressor. summaryAdapter may be nil (local summary only).
func NewCompressor(summaryAdapter adapters.ICliAdapter, config *CompressionConfig) *Compressor {
	c := &Compressor{
		tokenCounter:   NewTokenCounter(nil),
		summaryAdapter: summaryAdapter,
		config:         DefaultCompressionConfig,
	}
	if config != nil {
		c.config = *config
	}
	return c
}

// Compress compresses context using a background parent context.
func (c *Compressor) Compress(ctx EngineContext, config *CompressionConfig) (*CompressionResult, error) {
	return c.CompressMessages(context.Background(), ctx, config)
}

// CompressMessages compresses context using the caller context (hooks + optional LLM).
func (c *Compressor) CompressMessages(parent context.Context, ctx EngineContext, config *CompressionConfig) (*CompressionResult, error) {
	if parent == nil {
		parent = context.Background()
	}
	ctx = notifyBeforeCompressHook(parent, ctx)
	mergedConfig := c.config
	if config != nil {
		if config.MaxTokens > 0 {
			mergedConfig.MaxTokens = config.MaxTokens
		}
		if config.TargetRatio > 0 {
			mergedConfig.TargetRatio = config.TargetRatio
		}
		mergedConfig.PreserveSystemPrompt = config.PreserveSystemPrompt
		if config.PreserveRecentMessages > 0 {
			mergedConfig.PreserveRecentMessages = config.PreserveRecentMessages
		}
		mergedConfig.ExtractDecisionSkeleton = config.ExtractDecisionSkeleton
	}

	originalTokens := ctx.TokenCount
	if originalTokens == 0 {
		originalTokens = c.tokenCounter.CountMessages(ctx.Messages).Total
	}

	var skeleton *EntitySkeleton
	if mergedConfig.ExtractDecisionSkeleton {
		var err error
		skeleton, err = c.ExtractEntitySkeleton(ctx.Messages)
		if err != nil {
			skeleton = nil
		}
	}

	preservedMessages := c.applyCompressionStrategy(ctx.Messages, mergedConfig)

	var summary string
	if c.summaryAdapter != nil && len(ctx.Messages) > len(preservedMessages) {
		messagesToSummarize := ctx.Messages[:len(ctx.Messages)-mergedConfig.PreserveRecentMessages]
		if len(messagesToSummarize) > 0 {
			var err error
			summary, err = c.GenerateSummaryContext(parent, messagesToSummarize, nil)
			if err != nil {
				summary = c.generateLocalSummary(messagesToSummarize)
			}
		}
	}

	compressedTokens := c.tokenCounter.CountMessages(preservedMessages).Total
	ratio := 0.0
	if originalTokens > 0 {
		ratio = float64(compressedTokens) / float64(originalTokens)
	}

	return &CompressionResult{
		OriginalTokens:    originalTokens,
		CompressedTokens:  compressedTokens,
		CompressionRatio:  ratio,
		PreservedMessages: preservedMessages,
		Summary:           summary,
		EntitySkeleton:    skeleton,
	}, nil
}

func notifyBeforeCompressHook(ctx context.Context, payload EngineContext) EngineContext {
	if !backendhooks.HasHookManager() {
		return payload
	}
	result, err := backendhooks.GetHookManager().Trigger(ctx, backendhooks.HookBeforeCompress, payload)
	if err != nil {
		log.Printf("[WARN] summarize before-compress hook failed: err=%v", err)
		return payload
	}
	switch converted := result.(type) {
	case EngineContext:
		return converted
	case *EngineContext:
		if converted != nil {
			return *converted
		}
	}
	return payload
}

// ExtractEntitySkeleton extracts entities, decisions, and relations from messages.
func (c *Compressor) ExtractEntitySkeleton(messages []adapters.Message) (*EntitySkeleton, error) {
	entities := make([]string, 0)
	decisions := make([]string, 0)
	relations := make([]EntityRelation, 0)

	entitySet := make(map[string]bool)
	decisionSet := make(map[string]bool)
	entityRegex := regexp.MustCompile(`\b[A-Z][a-zA-Z]+\b`)
	decisionKeywords := []string{
		"decide", "choose", "select", "implement", "use", "adopt",
		"决定", "选择", "采用", "实现",
	}

	for _, msg := range messages {
		content := msg.Content
		for _, entity := range entityRegex.FindAllString(content, -1) {
			if !entitySet[entity] && len(entity) > 2 {
				entitySet[entity] = true
				entities = append(entities, entity)
			}
		}
		for _, sentence := range splitSentences(content) {
			lowerSentence := strings.ToLower(sentence)
			for _, kw := range decisionKeywords {
				if strings.Contains(lowerSentence, kw) {
					trimmed := strings.TrimSpace(sentence)
					if trimmed != "" && !decisionSet[trimmed] && len(trimmed) > 10 {
						decisionSet[trimmed] = true
						decisions = append(decisions, trimmed)
					}
					break
				}
			}
		}
	}

	entityList := entities
	for i := 0; i < len(entityList)-1 && i < 20; i++ {
		for j := i + 1; j < len(entityList) && j < 20; j++ {
			e1, e2 := entityList[i], entityList[j]
			for _, msg := range messages {
				if strings.Contains(msg.Content, e1) && strings.Contains(msg.Content, e2) {
					relations = append(relations, EntityRelation{From: e1, To: e2, Type: "related_to"})
					break
				}
			}
		}
	}

	if len(entities) > 20 {
		entities = entities[:20]
	}
	if len(decisions) > 10 {
		decisions = decisions[:10]
	}
	if len(relations) > 15 {
		relations = relations[:15]
	}

	return &EntitySkeleton{
		Entities:  entities,
		Decisions: decisions,
		Relations: relations,
	}, nil
}

// GenerateSummary generates a summary with a background context.
func (c *Compressor) GenerateSummary(messages []adapters.Message, config *SummaryAgentConfig) (string, error) {
	return c.GenerateSummaryContext(context.Background(), messages, config)
}

// GenerateSummaryContext generates a summary, falling back to local summary when no adapter.
func (c *Compressor) GenerateSummaryContext(ctx context.Context, messages []adapters.Message, config *SummaryAgentConfig) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if c.summaryAdapter == nil {
		return c.generateLocalSummary(messages), nil
	}

	prompt := c.buildSummaryPrompt(messages, config)
	maxTokens := 500
	if config != nil && config.MaxSummaryTokens > 0 {
		maxTokens = config.MaxSummaryTokens
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	response, err := c.summaryAdapter.Send(ctx, prompt, &adapters.SendOptions{MaxTokens: maxTokens})
	if err != nil {
		return c.generateLocalSummary(messages), nil
	}
	return response.Content, nil
}

func (c *Compressor) applyCompressionStrategy(messages []adapters.Message, config CompressionConfig) []adapters.Message {
	preserved := make([]adapters.Message, 0)

	if config.PreserveSystemPrompt {
		for _, m := range messages {
			if m.Role == adapters.RoleSystem {
				preserved = append(preserved, m)
			}
		}
	}

	nonSystemMessages := make([]adapters.Message, 0)
	for _, m := range messages {
		if m.Role != adapters.RoleSystem {
			nonSystemMessages = append(nonSystemMessages, m)
		}
	}

	recentCount := config.PreserveRecentMessages
	if recentCount > len(nonSystemMessages) {
		recentCount = len(nonSystemMessages)
	}
	recentMessages := nonSystemMessages[len(nonSystemMessages)-recentCount:]
	historicalMessages := nonSystemMessages[:len(nonSystemMessages)-recentCount]

	if len(historicalMessages) > 0 {
		preservedTokens := c.tokenCounter.CountMessages(preserved).Total
		recentTokens := c.tokenCounter.CountMessages(recentMessages).Total
		targetHistoricalTokens := int(float64(config.MaxTokens)*config.TargetRatio) - preservedTokens - recentTokens

		if targetHistoricalTokens > 0 {
			type scoredMessage struct {
				msg   adapters.Message
				idx   int
				score float64
			}
			scored := make([]scoredMessage, 0, len(historicalMessages))
			for idx, msg := range historicalMessages {
				scored = append(scored, scoredMessage{
					msg:   msg,
					idx:   idx,
					score: c.calculateImportance(msg, idx, len(historicalMessages)),
				})
			}
			sort.Slice(scored, func(i, j int) bool {
				return scored[i].score > scored[j].score
			})

			currentTokens := 0
			selectedHistorical := make([]scoredMessage, 0)
			for _, sm := range scored {
				msgTokens := c.tokenCounter.Count(sm.msg.Content)
				if currentTokens+msgTokens <= targetHistoricalTokens {
					selectedHistorical = append(selectedHistorical, sm)
					currentTokens += msgTokens
				}
			}
			sort.Slice(selectedHistorical, func(i, j int) bool {
				return selectedHistorical[i].idx < selectedHistorical[j].idx
			})
			for _, sm := range selectedHistorical {
				preserved = append(preserved, sm.msg)
			}
		}
	}

	preserved = append(preserved, recentMessages...)
	return preserved
}

func (c *Compressor) calculateImportance(msg adapters.Message, index, totalMessages int) float64 {
	score := 0.0
	lengthScore := float64(len(msg.Content)) / 100.0
	if lengthScore > 10 {
		lengthScore = 10
	}
	score += lengthScore
	if msg.Role == adapters.RoleAssistant {
		score += 5
	}
	if totalMessages > 0 {
		score += (float64(index) / float64(totalMessages)) * 3
	}
	importantKeywords := []string{
		"important", "critical", "must", "should", "error", "bug", "fix",
		"重要", "关键", "必须", "错误", "修复",
	}
	lowerContent := strings.ToLower(msg.Content)
	for _, kw := range importantKeywords {
		if strings.Contains(lowerContent, kw) {
			score += 2
		}
	}
	return score
}

func (c *Compressor) generateLocalSummary(messages []adapters.Message) string {
	userMessages := make([]adapters.Message, 0)
	assistantMessages := make([]adapters.Message, 0)
	for _, msg := range messages {
		switch msg.Role {
		case adapters.RoleUser:
			userMessages = append(userMessages, msg)
		case adapters.RoleAssistant:
			assistantMessages = append(assistantMessages, msg)
		}
	}

	topics := make([]string, 0)
	count := 5
	if len(userMessages) < count {
		count = len(userMessages)
	}
	if count > 0 {
		for _, msg := range userMessages[len(userMessages)-count:] {
			topic := msg.Content
			if len(topic) > 50 {
				topic = topic[:50]
			}
			topic = strings.ReplaceAll(topic, "\n", " ")
			topics = append(topics, topic)
		}
	}

	return fmt.Sprintf("Previous conversation covered %d messages. User topics: %s. Assistant provided %d responses.",
		len(messages), strings.Join(topics, "; "), len(assistantMessages))
}

func (c *Compressor) buildSummaryPrompt(messages []adapters.Message, config *SummaryAgentConfig) string {
	var sb strings.Builder
	sb.WriteString("Summarize the following conversation concisely:\n\n")
	for _, m := range messages {
		content := m.Content
		if len(content) > 500 {
			content = content[:500]
		}
		sb.WriteString(fmt.Sprintf("[%s]: %s\n\n", m.Role, content))
	}
	if config != nil {
		if config.IncludeEntities {
			sb.WriteString("Include key entities mentioned.\n")
		}
		if config.IncludeDecisions {
			sb.WriteString("Include important decisions made.\n")
		}
		if config.IncludeRelations {
			sb.WriteString("Include relationships between concepts.\n")
		}
	}
	sb.WriteString("\nProvide a concise summary:")
	return sb.String()
}

func splitSentences(text string) []string {
	sentenceRegex := regexp.MustCompile(`[.。!！?？]+`)
	parts := sentenceRegex.Split(text, -1)
	sentences := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			sentences = append(sentences, trimmed)
		}
	}
	return sentences
}

// InMemorySummaryHistory is an in-memory SummaryHistory store.
type InMemorySummaryHistory struct {
	histories map[string]*SummaryHistory
}

// NewInMemorySummaryHistory creates an empty history store.
func NewInMemorySummaryHistory() *InMemorySummaryHistory {
	return &InMemorySummaryHistory{histories: make(map[string]*SummaryHistory)}
}

// Save stores a history record.
func (h *InMemorySummaryHistory) Save(history *SummaryHistory) error {
	h.histories[history.ID] = history
	return nil
}

// Load returns a history by id, or nil if missing.
func (h *InMemorySummaryHistory) Load(id string) (*SummaryHistory, error) {
	if hist, ok := h.histories[id]; ok {
		return hist, nil
	}
	return nil, nil
}

// LoadBySession returns histories for a session.
func (h *InMemorySummaryHistory) LoadBySession(sessionID string) ([]*SummaryHistory, error) {
	result := make([]*SummaryHistory, 0)
	for _, hist := range h.histories {
		if hist.SessionID == sessionID {
			result = append(result, hist)
		}
	}
	return result, nil
}

// Delete removes a history by id.
func (h *InMemorySummaryHistory) Delete(id string) error {
	delete(h.histories, id)
	return nil
}

// List returns all histories.
func (h *InMemorySummaryHistory) List() ([]*SummaryHistory, error) {
	result := make([]*SummaryHistory, 0, len(h.histories))
	for _, hist := range h.histories {
		result = append(result, hist)
	}
	return result, nil
}
