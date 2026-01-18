package disclosure

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestProgressiveDisclosure_Search_Basic(t *testing.T) {
	pd := NewProgressiveDisclosure(nil)

	searcher := &MockSearcher{
		Results: []SearchResult{
			{Content: "Test content 1", Score: 0.8, Source: SourceVector},
			{Content: "Test content 2", Score: 0.6, Source: SourceGraph},
		},
	}
	pd.SetSearcher(searcher)

	response, err := pd.Search(context.Background(), "test query")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}

	if len(response.Suggestions) != 2 {
		t.Errorf("Expected 2 suggestions, got %d", len(response.Suggestions))
	}
	if response.TotalMatches != 2 {
		t.Errorf("Expected 2 total matches, got %d", response.TotalMatches)
	}
}

func TestProgressiveDisclosure_Search_MinRelevance(t *testing.T) {
	config := &DisclosureConfig{
		MaxSuggestions:    5,
		MinRelevanceScore: 0.5,
		TimeoutMs:         1000,
		EnableCache:       false,
		PreviewMaxLength:  100,
	}
	pd := NewProgressiveDisclosure(config)

	searcher := &MockSearcher{
		Results: []SearchResult{
			{Content: "High relevance", Score: 0.8, Source: SourceVector},
			{Content: "Low relevance", Score: 0.3, Source: SourceVector},
			{Content: "Medium relevance", Score: 0.6, Source: SourceVector},
		},
	}
	pd.SetSearcher(searcher)

	response, err := pd.Search(context.Background(), "test")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}

	if len(response.Suggestions) != 2 {
		t.Errorf("Expected 2 suggestions (filtered by minRelevance), got %d", len(response.Suggestions))
	}
}

func TestProgressiveDisclosure_Search_MaxSuggestions(t *testing.T) {
	config := &DisclosureConfig{
		MaxSuggestions:    2,
		MinRelevanceScore: 0.0,
		TimeoutMs:         1000,
		EnableCache:       false,
		PreviewMaxLength:  100,
	}
	pd := NewProgressiveDisclosure(config)

	searcher := &MockSearcher{
		Results: []SearchResult{
			{Content: "Content 1", Score: 0.9, Source: SourceVector},
			{Content: "Content 2", Score: 0.8, Source: SourceVector},
			{Content: "Content 3", Score: 0.7, Source: SourceVector},
			{Content: "Content 4", Score: 0.6, Source: SourceVector},
		},
	}
	pd.SetSearcher(searcher)

	response, err := pd.Search(context.Background(), "test")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}

	if len(response.Suggestions) != 2 {
		t.Errorf("Expected 2 suggestions (max limit), got %d", len(response.Suggestions))
	}
	if !response.HasMore {
		t.Error("Expected HasMore to be true")
	}
}

func TestProgressiveDisclosure_Search_Timeout(t *testing.T) {
	config := &DisclosureConfig{
		MaxSuggestions:    5,
		MinRelevanceScore: 0.0,
		TimeoutMs:         50, // 50ms timeout
		EnableCache:       false,
		PreviewMaxLength:  100,
	}
	pd := NewProgressiveDisclosure(config)

	// 创建一个慢搜索器
	slowSearcher := &SlowSearcher{Delay: 200 * time.Millisecond}
	pd.SetSearcher(slowSearcher)

	start := time.Now()
	response, err := pd.Search(context.Background(), "test")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Search error: %v", err)
	}

	// 应该在超时后返回空结果
	if elapsed > 150*time.Millisecond {
		t.Errorf("Search took too long: %v", elapsed)
	}

	if len(response.Suggestions) != 0 {
		t.Errorf("Expected 0 suggestions on timeout, got %d", len(response.Suggestions))
	}
}

type SlowSearcher struct {
	Delay time.Duration
}

func (s *SlowSearcher) HybridSearch(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	select {
	case <-time.After(s.Delay):
		return []SearchResult{{Content: "Slow result", Score: 0.9, Source: SourceVector}}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestProgressiveDisclosure_Cache(t *testing.T) {
	config := &DisclosureConfig{
		MaxSuggestions:    5,
		MinRelevanceScore: 0.0,
		TimeoutMs:         1000,
		EnableCache:       true,
		CacheMaxSize:      100,
		CacheTTLMs:        60000,
		PreviewMaxLength:  100,
	}
	pd := NewProgressiveDisclosure(config)

	callCount := 0
	searcher := &CountingSearcher{
		CallCount: &callCount,
		Results: []SearchResult{
			{Content: "Cached content", Score: 0.8, Source: SourceVector},
		},
	}
	pd.SetSearcher(searcher)

	// 第一次搜索
	_, err := pd.Search(context.Background(), "test query")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}

	// 第二次搜索（应使用缓存）
	_, err = pd.Search(context.Background(), "test query")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}

	if callCount != 1 {
		t.Errorf("Expected 1 search call (cached), got %d", callCount)
	}
}

type CountingSearcher struct {
	CallCount *int
	Results   []SearchResult
}

func (c *CountingSearcher) HybridSearch(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	*c.CallCount++
	return c.Results, nil
}

func TestProgressiveDisclosure_CacheTTL(t *testing.T) {
	config := &DisclosureConfig{
		MaxSuggestions:    5,
		MinRelevanceScore: 0.0,
		TimeoutMs:         1000,
		EnableCache:       true,
		CacheMaxSize:      100,
		CacheTTLMs:        50, // 50ms TTL
		PreviewMaxLength:  100,
	}
	pd := NewProgressiveDisclosure(config)

	callCount := 0
	searcher := &CountingSearcher{
		CallCount: &callCount,
		Results: []SearchResult{
			{Content: "Content", Score: 0.8, Source: SourceVector},
		},
	}
	pd.SetSearcher(searcher)

	// 第一次搜索
	pd.Search(context.Background(), "test")

	// 等待缓存过期
	time.Sleep(100 * time.Millisecond)

	// 第二次搜索（缓存已过期）
	pd.Search(context.Background(), "test")

	if callCount != 2 {
		t.Errorf("Expected 2 search calls (cache expired), got %d", callCount)
	}
}

func TestProgressiveDisclosure_GetSuggestionDetails(t *testing.T) {
	pd := NewProgressiveDisclosure(nil)

	searcher := &MockSearcher{
		Results: []SearchResult{
			{Content: "Detail content", Score: 0.9, Source: SourceVector},
		},
	}
	pd.SetSearcher(searcher)

	response, _ := pd.Search(context.Background(), "test")
	if len(response.Suggestions) == 0 {
		t.Fatal("Expected suggestions")
	}

	suggestionID := response.Suggestions[0].ID
	details := pd.GetSuggestionDetails(suggestionID)

	if details == nil {
		t.Fatal("Expected to get suggestion details")
	}
	if details.FullContent != "Detail content" {
		t.Errorf("Expected 'Detail content', got '%s'", details.FullContent)
	}
}

func TestProgressiveDisclosure_InjectContext_Markdown(t *testing.T) {
	pd := NewProgressiveDisclosure(nil)

	searcher := &MockSearcher{
		Results: []SearchResult{
			{Content: "Context 1", Score: 0.9, Source: SourceVector},
			{Content: "Context 2", Score: 0.8, Source: SourceVector},
		},
	}
	pd.SetSearcher(searcher)

	response, _ := pd.Search(context.Background(), "test")
	ids := make([]string, len(response.Suggestions))
	for i, s := range response.Suggestions {
		ids[i] = s.ID
	}

	result := pd.InjectContext(ids, &ContextInjectionOptions{
		Format:      FormatMarkdown,
		Deduplicate: true,
	})

	if !strings.Contains(result.InjectedContent, "## Relevant Context") {
		t.Error("Expected markdown header")
	}
	if !strings.Contains(result.InjectedContent, "### Context 1") {
		t.Error("Expected context 1 header")
	}
	if result.TokenCount == 0 {
		t.Error("Expected non-zero token count")
	}
}

func TestProgressiveDisclosure_InjectContext_XML(t *testing.T) {
	pd := NewProgressiveDisclosure(nil)

	searcher := &MockSearcher{
		Results: []SearchResult{
			{Content: "XML Content", Score: 0.9, Source: SourceVector},
		},
	}
	pd.SetSearcher(searcher)

	response, _ := pd.Search(context.Background(), "test")
	ids := []string{response.Suggestions[0].ID}

	result := pd.InjectContext(ids, &ContextInjectionOptions{
		Format: FormatXML,
	})

	if !strings.Contains(result.InjectedContent, "<injected_context>") {
		t.Error("Expected XML wrapper")
	}
	if !strings.Contains(result.InjectedContent, "<context_item") {
		t.Error("Expected context item tag")
	}
}

func TestProgressiveDisclosure_InjectContext_Deduplicate(t *testing.T) {
	pd := NewProgressiveDisclosure(nil)

	searcher := &MockSearcher{
		Results: []SearchResult{
			{Content: "Same content", Score: 0.9, Source: SourceVector},
			{Content: "Same content", Score: 0.8, Source: SourceVector},
			{Content: "Different content", Score: 0.7, Source: SourceVector},
		},
	}
	pd.SetSearcher(searcher)

	response, _ := pd.Search(context.Background(), "test")
	ids := make([]string, len(response.Suggestions))
	for i, s := range response.Suggestions {
		ids[i] = s.ID
	}

	result := pd.InjectContext(ids, &ContextInjectionOptions{
		Format:      FormatRaw,
		Deduplicate: true,
	})

	// 计算 "Same content" 出现次数
	count := strings.Count(result.InjectedContent, "Same content")
	if count != 1 {
		t.Errorf("Expected 'Same content' to appear once (deduplicated), got %d", count)
	}
}

func TestProgressiveDisclosure_InjectContext_MaxTokens(t *testing.T) {
	pd := NewProgressiveDisclosure(nil)

	longContent := strings.Repeat("a", 1000)
	searcher := &MockSearcher{
		Results: []SearchResult{
			{Content: longContent, Score: 0.9, Source: SourceVector},
		},
	}
	pd.SetSearcher(searcher)

	response, _ := pd.Search(context.Background(), "test")
	ids := []string{response.Suggestions[0].ID}

	result := pd.InjectContext(ids, &ContextInjectionOptions{
		Format:    FormatRaw,
		MaxTokens: 50, // ~200 chars
	})

	if len(result.InjectedContent) > 250 {
		t.Errorf("Expected truncated content, got length %d", len(result.InjectedContent))
	}
	if !strings.Contains(result.InjectedContent, "[truncated]") {
		t.Error("Expected [truncated] marker")
	}
}

func TestProgressiveDisclosure_ClearCache(t *testing.T) {
	pd := NewProgressiveDisclosure(nil)

	searcher := &MockSearcher{
		Results: []SearchResult{
			{Content: "Content", Score: 0.8, Source: SourceVector},
		},
	}
	pd.SetSearcher(searcher)

	// 填充缓存
	pd.Search(context.Background(), "test1")
	pd.Search(context.Background(), "test2")

	pd.ClearCache()

	// 检查缓存已清空
	pd.mu.RLock()
	cacheSize := len(pd.cache)
	pd.mu.RUnlock()

	if cacheSize != 0 {
		t.Errorf("Expected cache size 0 after clear, got %d", cacheSize)
	}
}

func TestProgressiveDisclosure_SuggestionType(t *testing.T) {
	pd := NewProgressiveDisclosure(nil)

	searcher := &MockSearcher{
		Results: []SearchResult{
			{Content: "Vector content", Score: 0.9, Source: SourceVector},
			{Content: "Graph content", Score: 0.8, Source: SourceGraph},
		},
	}
	pd.SetSearcher(searcher)

	response, _ := pd.Search(context.Background(), "test")

	vectorFound := false
	graphFound := false
	for _, s := range response.Suggestions {
		if s.Type == SuggestionMemory {
			vectorFound = true
		}
		if s.Type == SuggestionContext {
			graphFound = true
		}
	}

	if !vectorFound {
		t.Error("Expected memory type suggestion from vector source")
	}
	if !graphFound {
		t.Error("Expected context type suggestion from graph source")
	}
}

func TestProgressiveDisclosure_Preview(t *testing.T) {
	config := &DisclosureConfig{
		MaxSuggestions:    5,
		MinRelevanceScore: 0.0,
		TimeoutMs:         1000,
		EnableCache:       false,
		PreviewMaxLength:  20,
	}
	pd := NewProgressiveDisclosure(config)

	longContent := "This is a very long content that should be truncated in preview"
	searcher := &MockSearcher{
		Results: []SearchResult{
			{Content: longContent, Score: 0.9, Source: SourceVector},
		},
	}
	pd.SetSearcher(searcher)

	response, _ := pd.Search(context.Background(), "test")

	if len(response.Suggestions) == 0 {
		t.Fatal("Expected suggestions")
	}

	preview := response.Suggestions[0].Preview
	if len(preview) > 25 { // 20 + "..."
		t.Errorf("Expected truncated preview, got length %d", len(preview))
	}
	if !strings.HasSuffix(preview, "...") {
		t.Error("Expected preview to end with ...")
	}
}

func TestProgressiveDisclosure_EntityTitle(t *testing.T) {
	pd := NewProgressiveDisclosure(nil)

	searcher := &MockSearcher{
		Results: []SearchResult{
			{
				Content: "Content with entity",
				Score:   0.9,
				Source:  SourceGraph,
				Entity:  &EntityInfo{Label: "Custom Entity"},
			},
		},
	}
	pd.SetSearcher(searcher)

	response, _ := pd.Search(context.Background(), "test")

	if len(response.Suggestions) == 0 {
		t.Fatal("Expected suggestions")
	}

	if response.Suggestions[0].Title != "Custom Entity" {
		t.Errorf("Expected title 'Custom Entity', got '%s'", response.Suggestions[0].Title)
	}
}

func TestProgressiveDisclosure_SessionTitle(t *testing.T) {
	pd := NewProgressiveDisclosure(nil)

	searcher := &MockSearcher{
		Results: []SearchResult{
			{
				Content:  "Content with session",
				Score:    0.9,
				Source:   SourceVector,
				Metadata: map[string]interface{}{"sessionId": "session-123"},
			},
		},
	}
	pd.SetSearcher(searcher)

	response, _ := pd.Search(context.Background(), "test")

	if len(response.Suggestions) == 0 {
		t.Fatal("Expected suggestions")
	}

	if response.Suggestions[0].Title != "Session: session-123" {
		t.Errorf("Expected title 'Session: session-123', got '%s'", response.Suggestions[0].Title)
	}
}

func TestProgressiveDisclosure_SearchError(t *testing.T) {
	pd := NewProgressiveDisclosure(nil)

	searcher := &MockSearcher{
		Err: errors.New("search failed"),
	}
	pd.SetSearcher(searcher)

	response, err := pd.Search(context.Background(), "test")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}

	// 搜索失败应返回空结果，不是错误
	if len(response.Suggestions) != 0 {
		t.Errorf("Expected 0 suggestions on search error, got %d", len(response.Suggestions))
	}
}

func TestProgressiveDisclosure_Concurrent(t *testing.T) {
	pd := NewProgressiveDisclosure(nil)

	searcher := &MockSearcher{
		Results: []SearchResult{
			{Content: "Concurrent content", Score: 0.9, Source: SourceVector},
		},
	}
	pd.SetSearcher(searcher)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			pd.Search(context.Background(), "query")
		}(i)
	}

	wg.Wait()
}

func TestProgressiveDisclosure_CacheEviction(t *testing.T) {
	config := &DisclosureConfig{
		MaxSuggestions:    5,
		MinRelevanceScore: 0.0,
		TimeoutMs:         1000,
		EnableCache:       true,
		CacheMaxSize:      3,
		CacheTTLMs:        60000,
		PreviewMaxLength:  100,
	}
	pd := NewProgressiveDisclosure(config)

	// 直接测试 addToCache 的逐出逻辑
	for i := 0; i < 10; i++ {
		key := string(rune('a' + i))
		response := &DisclosureResponse{
			Suggestions:  []DisclosureSuggestion{{ID: key}},
			TotalMatches: 1,
		}
		pd.addToCache(key, response)
	}

	pd.mu.RLock()
	cacheSize := len(pd.cache)
	pd.mu.RUnlock()

	// 验证缓存被逐出限制
	if cacheSize > config.CacheMaxSize {
		t.Errorf("Expected cache size <= %d after eviction, got %d", config.CacheMaxSize, cacheSize)
	}
}

func TestProgressiveDisclosure_NoSearcher(t *testing.T) {
	pd := NewProgressiveDisclosure(nil)
	// 不设置 searcher

	response, err := pd.Search(context.Background(), "test")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}

	if len(response.Suggestions) != 0 {
		t.Errorf("Expected 0 suggestions without searcher, got %d", len(response.Suggestions))
	}
}

func TestProgressiveDisclosure_InjectEmptyIDs(t *testing.T) {
	pd := NewProgressiveDisclosure(nil)

	result := pd.InjectContext([]string{}, nil)

	if result.InjectedContent != "" {
		t.Error("Expected empty content for empty IDs")
	}
	if result.TokenCount != 0 {
		t.Error("Expected 0 token count for empty IDs")
	}
}

func TestProgressiveDisclosure_InjectNonexistentID(t *testing.T) {
	pd := NewProgressiveDisclosure(nil)

	result := pd.InjectContext([]string{"nonexistent-id"}, nil)

	if result.InjectedContent != "" {
		t.Error("Expected empty content for nonexistent ID")
	}
}
