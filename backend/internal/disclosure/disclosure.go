package disclosure

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"
)

// SuggestionType 建议类型
type SuggestionType string

const (
	SuggestionMemory  SuggestionType = "memory"
	SuggestionContext SuggestionType = "context"
	SuggestionRule    SuggestionType = "rule"
)

// SourceType 来源类型
type SourceType string

const (
	SourceVector SourceType = "vector"
	SourceGraph  SourceType = "graph"
)

// InjectionPosition 注入位置
type InjectionPosition string

const (
	PositionPrepend InjectionPosition = "prepend"
	PositionAppend  InjectionPosition = "append"
	PositionSystem  InjectionPosition = "system"
)

// InjectionFormat 注入格式
type InjectionFormat string

const (
	FormatRaw      InjectionFormat = "raw"
	FormatMarkdown InjectionFormat = "markdown"
	FormatXML      InjectionFormat = "xml"
)

// DisclosureSuggestion 披露建议
type DisclosureSuggestion struct {
	ID             string                 `json:"id"`
	Type           SuggestionType         `json:"type"`
	Title          string                 `json:"title"`
	Preview        string                 `json:"preview"`
	FullContent    string                 `json:"full_content"`
	RelevanceScore float64                `json:"relevance_score"`
	Source         SourceType             `json:"source"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
	Timestamp      int64                  `json:"timestamp"`
}

// DisclosureResponse 披露响应
type DisclosureResponse struct {
	Suggestions  []DisclosureSuggestion `json:"suggestions"`
	QueryTime    int64                  `json:"query_time"`
	TotalMatches int                    `json:"total_matches"`
	HasMore      bool                   `json:"has_more"`
}

// DisclosureConfig 披露配置
type DisclosureConfig struct {
	MaxSuggestions    int   `json:"max_suggestions"`
	MinRelevanceScore float64 `json:"min_relevance_score"`
	TimeoutMs         int64 `json:"timeout_ms"`
	EnableCache       bool  `json:"enable_cache"`
	CacheMaxSize      int   `json:"cache_max_size"`
	CacheTTLMs        int64 `json:"cache_ttl_ms"`
	DebounceMs        int64 `json:"debounce_ms"`
	PreviewMaxLength  int   `json:"preview_max_length"`
}

// ContextInjectionOptions 上下文注入选项
type ContextInjectionOptions struct {
	Position    InjectionPosition `json:"position"`
	Format      InjectionFormat   `json:"format"`
	MaxTokens   int               `json:"max_tokens,omitempty"`
	Deduplicate bool              `json:"deduplicate"`
}

// InjectionResult 注入结果
type InjectionResult struct {
	InjectedContent   string   `json:"injected_content"`
	TokenCount        int      `json:"token_count"`
	SourceSuggestions []string `json:"source_suggestions"`
}

// SearchResult 搜索结果（来自外部检索器）
type SearchResult struct {
	Content  string                 `json:"content"`
	Score    float64                `json:"score"`
	Source   SourceType             `json:"source"`
	Entity   *EntityInfo            `json:"entity,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// EntityInfo 实体信息
type EntityInfo struct {
	Label string `json:"label"`
}

// ISearcher 搜索器接口
type ISearcher interface {
	HybridSearch(ctx context.Context, query string, limit int) ([]SearchResult, error)
}

// IProgressiveDisclosure 渐进式披露接口
type IProgressiveDisclosure interface {
	Search(ctx context.Context, input string) (*DisclosureResponse, error)
	GetSuggestionDetails(id string) *DisclosureSuggestion
	InjectContext(suggestionIDs []string, options *ContextInjectionOptions) *InjectionResult
	ClearCache()
	Configure(config DisclosureConfig)
}

// DefaultDisclosureConfig 默认配置
var DefaultDisclosureConfig = DisclosureConfig{
	MaxSuggestions:    5,
	MinRelevanceScore: 0.3,
	TimeoutMs:         200,
	EnableCache:       true,
	CacheMaxSize:      100,
	CacheTTLMs:        60000,
	DebounceMs:        150,
	PreviewMaxLength:  100,
}

type cacheEntry struct {
	response  *DisclosureResponse
	timestamp int64
}

// ProgressiveDisclosure 渐进式披露实现
type ProgressiveDisclosure struct {
	config          DisclosureConfig
	cache           map[string]*cacheEntry
	suggestionStore map[string]*DisclosureSuggestion
	searcher        ISearcher
	mu              sync.RWMutex
	randSource      *rand.Rand
}

// NewProgressiveDisclosure 创建渐进式披露管理器
func NewProgressiveDisclosure(config *DisclosureConfig) *ProgressiveDisclosure {
	cfg := DefaultDisclosureConfig
	if config != nil {
		cfg = *config
	}
	return &ProgressiveDisclosure{
		config:          cfg,
		cache:           make(map[string]*cacheEntry),
		suggestionStore: make(map[string]*DisclosureSuggestion),
		randSource:      rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// SetSearcher 设置搜索器
func (p *ProgressiveDisclosure) SetSearcher(searcher ISearcher) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.searcher = searcher
}

// Configure 配置
func (p *ProgressiveDisclosure) Configure(config DisclosureConfig) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.config = config
}

// Search 搜索
func (p *ProgressiveDisclosure) Search(ctx context.Context, input string) (*DisclosureResponse, error) {
	startTime := time.Now()

	// 检查缓存
	if p.config.EnableCache {
		if cached := p.getFromCache(input); cached != nil {
			cached.QueryTime = time.Since(startTime).Milliseconds()
			return cached, nil
		}
	}

	// 带超时的上下文
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(p.config.TimeoutMs)*time.Millisecond)
	defer cancel()

	// 执行搜索
	resultCh := make(chan *DisclosureResponse, 1)
	go func() {
		response := p.performSearch(timeoutCtx, input, startTime)
		select {
		case resultCh <- response:
		case <-timeoutCtx.Done():
		}
	}()

	// 等待结果或超时
	select {
	case response := <-resultCh:
		if p.config.EnableCache && len(response.Suggestions) > 0 {
			p.addToCache(input, response)
		}
		return response, nil
	case <-timeoutCtx.Done():
		return &DisclosureResponse{
			Suggestions:  []DisclosureSuggestion{},
			QueryTime:    p.config.TimeoutMs,
			TotalMatches: 0,
			HasMore:      false,
		}, nil
	}
}

func (p *ProgressiveDisclosure) performSearch(ctx context.Context, input string, startTime time.Time) *DisclosureResponse {
	suggestions := make([]DisclosureSuggestion, 0)

	p.mu.RLock()
	searcher := p.searcher
	p.mu.RUnlock()

	if searcher != nil {
		results, err := searcher.HybridSearch(ctx, input, p.config.MaxSuggestions*2)
		if err == nil {
			for _, result := range results {
				if result.Score < p.config.MinRelevanceScore {
					continue
				}

				suggestion := p.createSuggestion(&result)
				suggestions = append(suggestions, *suggestion)

				p.mu.Lock()
				p.suggestionStore[suggestion.ID] = suggestion
				p.mu.Unlock()

				if len(suggestions) >= p.config.MaxSuggestions {
					break
				}
			}
		}
	}

	return &DisclosureResponse{
		Suggestions:  suggestions,
		QueryTime:    time.Since(startTime).Milliseconds(),
		TotalMatches: len(suggestions),
		HasMore:      len(suggestions) >= p.config.MaxSuggestions,
	}
}

func (p *ProgressiveDisclosure) createSuggestion(result *SearchResult) *DisclosureSuggestion {
	id := fmt.Sprintf("suggestion_%d_%s", time.Now().UnixMilli(), p.randomString(7))
	content := result.Content
	preview := content
	if len(preview) > p.config.PreviewMaxLength {
		preview = preview[:p.config.PreviewMaxLength] + "..."
	}

	title := "Memory Match"
	if result.Entity != nil && result.Entity.Label != "" {
		title = result.Entity.Label
	} else if result.Metadata != nil {
		if sessionID, ok := result.Metadata["sessionId"].(string); ok {
			title = "Session: " + sessionID
		}
	}

	suggType := SuggestionMemory
	if result.Source == SourceGraph {
		suggType = SuggestionContext
	}

	return &DisclosureSuggestion{
		ID:             id,
		Type:           suggType,
		Title:          title,
		Preview:        preview,
		FullContent:    content,
		RelevanceScore: result.Score,
		Source:         result.Source,
		Metadata:       result.Metadata,
		Timestamp:      time.Now().UnixMilli(),
	}
}

func (p *ProgressiveDisclosure) randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[p.randSource.Intn(len(letters))]
	}
	return string(b)
}

// GetSuggestionDetails 获取建议详情
func (p *ProgressiveDisclosure) GetSuggestionDetails(id string) *DisclosureSuggestion {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.suggestionStore[id]
}

// InjectContext 注入上下文
func (p *ProgressiveDisclosure) InjectContext(suggestionIDs []string, options *ContextInjectionOptions) *InjectionResult {
	opts := ContextInjectionOptions{
		Position:    PositionPrepend,
		Format:      FormatMarkdown,
		Deduplicate: true,
	}
	if options != nil {
		if options.Position != "" {
			opts.Position = options.Position
		}
		if options.Format != "" {
			opts.Format = options.Format
		}
		opts.Deduplicate = options.Deduplicate
		opts.MaxTokens = options.MaxTokens
	}

	p.mu.RLock()
	suggestions := make([]*DisclosureSuggestion, 0, len(suggestionIDs))
	for _, id := range suggestionIDs {
		if s, ok := p.suggestionStore[id]; ok {
			suggestions = append(suggestions, s)
		}
	}
	p.mu.RUnlock()

	if len(suggestions) == 0 {
		return &InjectionResult{
			InjectedContent:   "",
			TokenCount:        0,
			SourceSuggestions: []string{},
		}
	}

	// 去重
	var uniqueContents []string
	if opts.Deduplicate {
		seen := make(map[string]bool)
		for _, s := range suggestions {
			if !seen[s.FullContent] {
				seen[s.FullContent] = true
				uniqueContents = append(uniqueContents, s.FullContent)
			}
		}
	} else {
		for _, s := range suggestions {
			uniqueContents = append(uniqueContents, s.FullContent)
		}
	}

	// 格式化
	var injectedContent string
	switch opts.Format {
	case FormatXML:
		injectedContent = formatAsXML(uniqueContents)
	case FormatMarkdown:
		injectedContent = formatAsMarkdown(uniqueContents)
	default:
		injectedContent = strings.Join(uniqueContents, "\n\n")
	}

	// 截断
	if opts.MaxTokens > 0 {
		estimatedTokens := (len(injectedContent) + 3) / 4
		if estimatedTokens > opts.MaxTokens {
			maxChars := opts.MaxTokens * 4
			if maxChars < len(injectedContent) {
				injectedContent = injectedContent[:maxChars] + "\n[truncated]"
			}
		}
	}

	return &InjectionResult{
		InjectedContent:   injectedContent,
		TokenCount:        (len(injectedContent) + 3) / 4,
		SourceSuggestions: suggestionIDs,
	}
}

func formatAsXML(contents []string) string {
	var items []string
	for i, c := range contents {
		items = append(items, fmt.Sprintf("<context_item index=\"%d\">\n%s\n</context_item>", i+1, c))
	}
	return fmt.Sprintf("<injected_context>\n%s\n</injected_context>", strings.Join(items, "\n"))
}

func formatAsMarkdown(contents []string) string {
	var items []string
	for i, c := range contents {
		items = append(items, fmt.Sprintf("### Context %d\n\n%s", i+1, c))
	}
	return fmt.Sprintf("## Relevant Context\n\n%s", strings.Join(items, "\n\n---\n\n"))
}

// ClearCache 清除缓存
func (p *ProgressiveDisclosure) ClearCache() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cache = make(map[string]*cacheEntry)
}

func (p *ProgressiveDisclosure) getFromCache(input string) *DisclosureResponse {
	p.mu.RLock()
	defer p.mu.RUnlock()

	key := p.getCacheKey(input)
	entry, ok := p.cache[key]
	if !ok {
		return nil
	}

	if time.Now().UnixMilli()-entry.timestamp > p.config.CacheTTLMs {
		return nil
	}

	// 复制响应
	return &DisclosureResponse{
		Suggestions:  entry.response.Suggestions,
		QueryTime:    entry.response.QueryTime,
		TotalMatches: entry.response.TotalMatches,
		HasMore:      entry.response.HasMore,
	}
}

func (p *ProgressiveDisclosure) addToCache(input string, response *DisclosureResponse) {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := p.getCacheKey(input)

	// 清理过期缓存
	if len(p.cache) >= p.config.CacheMaxSize {
		p.evictOldestCache()
	}

	p.cache[key] = &cacheEntry{
		response:  response,
		timestamp: time.Now().UnixMilli(),
	}
}

func (p *ProgressiveDisclosure) getCacheKey(input string) string {
	return strings.ToLower(strings.TrimSpace(input))
}

func (p *ProgressiveDisclosure) evictOldestCache() {
	var oldestKey string
	var oldestTime int64 = 1<<62 - 1 // Max int64 / 2

	for key, entry := range p.cache {
		if entry.timestamp < oldestTime {
			oldestTime = entry.timestamp
			oldestKey = key
		}
	}

	if oldestKey != "" {
		delete(p.cache, oldestKey)
	}
}

// MockSearcher 测试用搜索器
type MockSearcher struct {
	Results []SearchResult
	Err     error
}

// HybridSearch 模拟搜索
func (m *MockSearcher) HybridSearch(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	results := m.Results
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}
