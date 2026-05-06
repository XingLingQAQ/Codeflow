package retriever

import (
	"context"
	"testing"
	"time"
)

// mockVectorStore 模拟向量存储
type mockVectorStore struct {
	results []VectorSearchResult
}

func (m *mockVectorStore) Search(ctx context.Context, query string, topK int, minScore float64) ([]VectorSearchResult, error) {
	if len(m.results) > topK {
		return m.results[:topK], nil
	}
	return m.results, nil
}

func TestSemanticRetrieverBasic(t *testing.T) {
	retriever := NewSemanticRetriever(nil, nil)

	// 索引一些内容
	retriever.IndexContent("doc1", "This is a test document about Go programming", ChunkMetadata{
		SessionID:    "session1",
		MessageIndex: 0,
		ChunkIndex:   0,
		Timestamp:    time.Now().UnixMilli(),
	})

	retriever.IndexContent("doc2", "Another document discussing Python and machine learning", ChunkMetadata{
		SessionID:    "session1",
		MessageIndex: 1,
		ChunkIndex:   0,
		Timestamp:    time.Now().UnixMilli(),
	})

	retriever.IndexContent("doc3", "Go is great for building web services and APIs", ChunkMetadata{
		SessionID:    "session2",
		MessageIndex: 0,
		ChunkIndex:   0,
		Timestamp:    time.Now().UnixMilli(),
	})

	ctx := context.Background()

	// 关键词搜索
	results, err := retriever.KeywordSearch(ctx, "Go programming", nil)
	if err != nil {
		t.Fatalf("keyword search: %v", err)
	}

	if len(results) == 0 {
		t.Error("expected results for 'Go programming'")
	}

	// 应该找到包含Go的文档
	foundGo := false
	for _, r := range results {
		if contains(r.Content, "Go") {
			foundGo = true
			break
		}
	}
	if !foundGo {
		t.Error("expected to find documents containing 'Go'")
	}
}

func TestSemanticRetrieverKeywordSearch(t *testing.T) {
	retriever := NewSemanticRetriever(nil, nil)

	// 索引内容
	docs := []struct {
		id      string
		content string
	}{
		{"d1", "The quick brown fox jumps over the lazy dog"},
		{"d2", "A lazy cat sleeps all day long"},
		{"d3", "Quick sort is a fast sorting algorithm"},
		{"d4", "The dog runs fast in the park"},
	}

	now := time.Now().UnixMilli()
	for i, doc := range docs {
		retriever.IndexContent(doc.id, doc.content, ChunkMetadata{
			SessionID:    "test",
			MessageIndex: i,
			Timestamp:    now,
		})
	}

	ctx := context.Background()

	// 搜索 "lazy"
	results, err := retriever.KeywordSearch(ctx, "lazy", nil)
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if len(results) < 2 {
		t.Errorf("expected at least 2 results for 'lazy', got %d", len(results))
	}

	// 搜索 "quick fast"
	results, err = retriever.KeywordSearch(ctx, "quick fast", nil)
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if len(results) < 2 {
		t.Errorf("expected at least 2 results for 'quick fast', got %d", len(results))
	}
}

func TestSemanticRetrieverHybridSearch(t *testing.T) {
	// 创建mock向量存储
	mockStore := &mockVectorStore{
		results: []VectorSearchResult{
			{
				ID:      "v1",
				Content: "Vector result about programming",
				Score:   0.9,
				Metadata: ChunkMetadata{
					SessionID:    "session1",
					MessageIndex: 0,
					Timestamp:    time.Now().UnixMilli(),
				},
			},
		},
	}

	retriever := NewSemanticRetriever(mockStore, nil)

	// 索引关键词内容
	retriever.IndexContent("k1", "Keyword result about programming languages", ChunkMetadata{
		SessionID:    "session1",
		MessageIndex: 1,
		Timestamp:    time.Now().UnixMilli(),
	})

	ctx := context.Background()

	// 混合搜索
	results, err := retriever.HybridSearch(ctx, "programming", nil)
	if err != nil {
		t.Fatalf("hybrid search: %v", err)
	}

	if len(results) == 0 {
		t.Error("expected results from hybrid search")
	}

	// 应该有向量和关键词结果
	hasVector := false
	hasKeyword := false
	for _, r := range results {
		if r.VectorScore > 0 {
			hasVector = true
		}
		if r.KeywordScore > 0 {
			hasKeyword = true
		}
	}

	if !hasVector && mockStore.results != nil {
		t.Error("expected vector results in hybrid search")
	}
	if !hasKeyword {
		t.Error("expected keyword results in hybrid search")
	}
}

func TestSemanticRetrieverSearchHistoricalContext(t *testing.T) {
	retriever := NewSemanticRetriever(nil, nil)

	now := time.Now().UnixMilli()

	// 索引不同会话的内容
	retriever.IndexContent("doc1", "Discussion about authentication in session 1", ChunkMetadata{
		SessionID:    "session1",
		AgentRole:    "main",
		MessageIndex: 0,
		Timestamp:    now - 10000,
	})

	retriever.IndexContent("doc2", "Authentication implementation details", ChunkMetadata{
		SessionID:    "session2",
		AgentRole:    "coder",
		MessageIndex: 0,
		Timestamp:    now - 5000,
	})

	ctx := context.Background()

	// 搜索所有会话
	result, err := retriever.SearchHistoricalContext(ctx, SearchHistoricalContextParams{
		Query:      "authentication",
		SearchType: SearchKeyword,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if len(result.Matches) < 2 {
		t.Errorf("expected at least 2 matches, got %d", len(result.Matches))
	}

	// 按会话过滤
	result, err = retriever.SearchHistoricalContext(ctx, SearchHistoricalContextParams{
		Query:      "authentication",
		SessionID:  "session1",
		SearchType: SearchKeyword,
	})
	if err != nil {
		t.Fatalf("search with filter: %v", err)
	}

	if len(result.Matches) != 1 {
		t.Errorf("expected 1 match for session1, got %d", len(result.Matches))
	}

	// 按角色过滤
	result, err = retriever.SearchHistoricalContext(ctx, SearchHistoricalContextParams{
		Query:      "authentication",
		AgentRole:  "coder",
		SearchType: SearchKeyword,
	})
	if err != nil {
		t.Fatalf("search with role filter: %v", err)
	}

	if len(result.Matches) != 1 {
		t.Errorf("expected 1 match for coder role, got %d", len(result.Matches))
	}
}

func TestSemanticRetrieverTimeRangeFilter(t *testing.T) {
	retriever := NewSemanticRetriever(nil, nil)

	now := time.Now().UnixMilli()
	hourAgo := now - 3600000
	dayAgo := now - 86400000

	retriever.IndexContent("doc1", "Recent discussion about testing", ChunkMetadata{
		SessionID:    "test",
		MessageIndex: 0,
		Timestamp:    now - 1000, // 1秒前
	})

	retriever.IndexContent("doc2", "Older discussion about testing", ChunkMetadata{
		SessionID:    "test",
		MessageIndex: 1,
		Timestamp:    dayAgo, // 1天前
	})

	ctx := context.Background()

	// 只搜索最近1小时
	result, err := retriever.SearchHistoricalContext(ctx, SearchHistoricalContextParams{
		Query:      "testing",
		SearchType: SearchKeyword,
		TimeRange:  &TimeRange{Start: hourAgo, End: now},
	})
	if err != nil {
		t.Fatalf("search with time range: %v", err)
	}

	if len(result.Matches) != 1 {
		t.Errorf("expected 1 match in time range, got %d", len(result.Matches))
	}
}

func TestSemanticRetrieverClearIndex(t *testing.T) {
	retriever := NewSemanticRetriever(nil, nil)

	retriever.IndexContent("doc1", "Test content", ChunkMetadata{
		SessionID:    "test",
		MessageIndex: 0,
		Timestamp:    time.Now().UnixMilli(),
	})

	ctx := context.Background()

	// 验证内容已索引
	results, _ := retriever.KeywordSearch(ctx, "test", nil)
	if len(results) == 0 {
		t.Error("expected results before clear")
	}

	// 清除索引
	retriever.ClearIndex()

	// 验证索引已清除
	results, _ = retriever.KeywordSearch(ctx, "test", nil)
	if len(results) != 0 {
		t.Errorf("expected no results after clear, got %d", len(results))
	}
}

func TestSemanticRetrieverHighlights(t *testing.T) {
	retriever := NewSemanticRetriever(nil, nil)

	retriever.IndexContent("doc1", "The authentication system uses JWT tokens for secure access", ChunkMetadata{
		SessionID:    "test",
		MessageIndex: 0,
		Timestamp:    time.Now().UnixMilli(),
	})

	ctx := context.Background()

	results, err := retriever.KeywordSearch(ctx, "authentication", nil)
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("expected results")
	}

	// 检查高亮
	if len(results[0].Highlights) == 0 {
		t.Error("expected highlights")
	} else {
		hasHighlight := false
		for _, hl := range results[0].Highlights {
			if contains(hl, "**") {
				hasHighlight = true
				break
			}
		}
		if !hasHighlight {
			t.Error("expected highlighted text with ** markers")
		}
	}
}

func TestSemanticRetrieverPartialConfigKeepsDefaultWeights(t *testing.T) {
	retriever := NewSemanticRetriever(nil, &HybridSearchConfig{
		TopK:      10,
		MinScore:  0.1,
		Reranking: true,
	})

	if retriever.config.VectorWeight != DefaultHybridConfig.VectorWeight {
		t.Fatalf("expected default vector weight, got %f", retriever.config.VectorWeight)
	}
	if retriever.config.KeywordWeight != DefaultHybridConfig.KeywordWeight {
		t.Fatalf("expected default keyword weight, got %f", retriever.config.KeywordWeight)
	}
}

func TestSemanticRetrieverReranking(t *testing.T) {
	retriever := NewSemanticRetriever(nil, &HybridSearchConfig{
		TopK:      10,
		MinScore:  0.1,
		Reranking: true,
	})

	retriever.IndexContent("doc1", "Go programming language basics", ChunkMetadata{
		SessionID:    "test",
		MessageIndex: 0,
		Timestamp:    time.Now().UnixMilli(),
	})

	retriever.IndexContent("doc2", "Advanced Go programming techniques and patterns", ChunkMetadata{
		SessionID:    "test",
		MessageIndex: 1,
		Timestamp:    time.Now().UnixMilli(),
	})

	ctx := context.Background()

	// 搜索应该重排序以优先匹配更多查询词的结果
	results, err := retriever.HybridSearch(ctx, "Go programming patterns", &HybridSearchConfig{
		Reranking: true,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if len(results) < 2 {
		t.Error("expected at least 2 results")
	}

	// 包含更多查询词的文档应该排在前面
	if len(results) >= 2 {
		if !contains(results[0].Content, "patterns") && contains(results[1].Content, "patterns") {
			t.Error("reranking should prioritize documents with more query term overlap")
		}
	}
}

func TestContextCancellation(t *testing.T) {
	retriever := NewSemanticRetriever(nil, nil)

	retriever.IndexContent("doc1", "Test content", ChunkMetadata{
		SessionID:    "test",
		MessageIndex: 0,
		Timestamp:    time.Now().UnixMilli(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := retriever.KeywordSearch(ctx, "test", nil)
	if err == nil {
		t.Error("expected error on cancelled context")
	}
}

func TestDefaultConfig(t *testing.T) {
	if DefaultHybridConfig.VectorWeight != 0.7 {
		t.Errorf("expected vector weight 0.7, got %f", DefaultHybridConfig.VectorWeight)
	}
	if DefaultHybridConfig.KeywordWeight != 0.3 {
		t.Errorf("expected keyword weight 0.3, got %f", DefaultHybridConfig.KeywordWeight)
	}
	if DefaultHybridConfig.TopK != 10 {
		t.Errorf("expected topK 10, got %d", DefaultHybridConfig.TopK)
	}
	if DefaultHybridConfig.MinScore != 0.3 {
		t.Errorf("expected min score 0.3, got %f", DefaultHybridConfig.MinScore)
	}
	if !DefaultHybridConfig.Reranking {
		t.Error("expected reranking to be true by default")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
