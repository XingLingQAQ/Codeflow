package retriever

import (
	"context"
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

// SemanticRetriever 语义检索器实现
type SemanticRetriever struct {
	vectorStore  IVectorStore
	config       HybridSearchConfig
	keywordIndex map[string]map[string]struct{} // token -> id set
	contentCache map[string]*cachedContent
	mu           sync.RWMutex
}

type cachedContent struct {
	Content  string
	Metadata ChunkMetadata
}

// NewSemanticRetriever 创建语义检索器
func NewSemanticRetriever(vectorStore IVectorStore, config *HybridSearchConfig) *SemanticRetriever {
	cfg := mergeHybridSearchConfig(DefaultHybridConfig, config)

	return &SemanticRetriever{
		vectorStore:  vectorStore,
		config:       cfg,
		keywordIndex: make(map[string]map[string]struct{}),
		contentCache: make(map[string]*cachedContent),
	}
}

func mergeHybridSearchConfig(base HybridSearchConfig, override *HybridSearchConfig) HybridSearchConfig {
	if override == nil {
		return base
	}
	cfg := base
	if override.VectorWeight > 0 {
		cfg.VectorWeight = override.VectorWeight
	}
	if override.KeywordWeight > 0 {
		cfg.KeywordWeight = override.KeywordWeight
	}
	if override.TopK > 0 {
		cfg.TopK = override.TopK
	}
	if override.MinScore > 0 {
		cfg.MinScore = override.MinScore
	}
	cfg.Reranking = override.Reranking
	return cfg
}

// SearchHistoricalContext 搜索历史上下文
func (r *SemanticRetriever) SearchHistoricalContext(
	ctx context.Context,
	params SearchHistoricalContextParams,
) (*SearchHistoricalContextResult, error) {
	startTime := time.Now()
	searchType := params.SearchType
	if searchType == "" {
		searchType = SearchHybrid
	}

	limit := params.Limit
	if limit <= 0 {
		limit = r.config.TopK
	}

	var results []HybridSearchResult
	var err error

	switch searchType {
	case SearchVector:
		vectorResults, err := r.VectorSearch(ctx, params.Query, &HybridSearchConfig{TopK: limit})
		if err != nil {
			return nil, err
		}
		for _, vr := range vectorResults {
			results = append(results, HybridSearchResult{
				Content:     vr.Content,
				Score:       vr.Score,
				VectorScore: vr.Score,
				Source:      SourceVector,
				Metadata:    vr.Metadata,
			})
		}

	case SearchKeyword:
		keywordResults, err := r.KeywordSearch(ctx, params.Query, &HybridSearchConfig{TopK: limit})
		if err != nil {
			return nil, err
		}
		for _, kr := range keywordResults {
			results = append(results, HybridSearchResult{
				Content:      kr.Content,
				Score:        kr.Score,
				KeywordScore: kr.Score,
				Source:       SourceKeyword,
				Metadata:     kr.Metadata,
				Highlights:   kr.Highlights,
			})
		}

	case SearchHybrid:
		fallthrough
	default:
		results, err = r.HybridSearch(ctx, params.Query, &HybridSearchConfig{TopK: limit})
		if err != nil {
			return nil, err
		}
	}

	// 应用过滤器
	results = r.applyFilters(results, params)

	// 转换为MemoryMatch格式
	matches := make([]MemoryMatch, 0, len(results))
	for _, res := range results {
		source := "vector"
		if res.Source == SourceKeyword {
			source = "rules"
		}
		matches = append(matches, MemoryMatch{
			Content:    res.Content,
			Similarity: res.Score,
			Source:     source,
			Metadata: map[string]interface{}{
				"session_id":      res.Metadata.SessionID,
				"message_index":   res.Metadata.MessageIndex,
				"chunk_index":     res.Metadata.ChunkIndex,
				"agent_role":      res.Metadata.AgentRole,
				"git_commit_hash": res.Metadata.GitCommitHash,
				"timestamp":       res.Metadata.Timestamp,
			},
		})
	}

	return &SearchHistoricalContextResult{
		Matches:    matches,
		TotalCount: len(matches),
		SearchType: searchType,
		QueryTime:  time.Since(startTime).Milliseconds(),
	}, nil
}

// VectorSearch 向量搜索
func (r *SemanticRetriever) VectorSearch(
	ctx context.Context,
	query string,
	options *HybridSearchConfig,
) ([]VectorSearchResult, error) {
	topK := r.config.TopK
	minScore := r.config.MinScore
	if options != nil {
		if options.TopK > 0 {
			topK = options.TopK
		}
		if options.MinScore > 0 {
			minScore = options.MinScore
		}
	}

	if r.vectorStore == nil {
		return []VectorSearchResult{}, nil
	}

	return r.vectorStore.Search(ctx, query, topK, minScore)
}

// KeywordSearch 关键词搜索（BM25风格）
func (r *SemanticRetriever) KeywordSearch(
	ctx context.Context,
	query string,
	options *HybridSearchConfig,
) ([]KeywordSearchResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	topK := r.config.TopK
	minScore := r.config.MinScore
	if options != nil {
		if options.TopK > 0 {
			topK = options.TopK
		}
		if options.MinScore > 0 {
			minScore = options.MinScore
		}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	queryTokens := r.tokenize(query)
	scores := make(map[string]*scoreInfo)

	// 计算BM25风格的分数
	totalDocs := float64(len(r.contentCache))
	for _, token := range queryTokens {
		lowerToken := strings.ToLower(token)
		matchingIDs, ok := r.keywordIndex[lowerToken]
		if !ok {
			continue
		}

		// IDF
		idf := math.Log((totalDocs+1)/(float64(len(matchingIDs))+1)) + 1

		for id := range matchingIDs {
			cached, ok := r.contentCache[id]
			if !ok {
				continue
			}

			// TF
			tf := r.calculateTF(cached.Content, token)
			score := tf * idf

			info, ok := scores[id]
			if !ok {
				info = &scoreInfo{score: 0, highlights: make([]string, 0)}
				scores[id] = info
			}
			info.score += score
			if hl := r.createHighlight(cached.Content, token); hl != "" {
				info.highlights = append(info.highlights, hl)
			}
		}
	}

	// 归一化分数
	var maxScore float64
	for _, info := range scores {
		if info.score > maxScore {
			maxScore = info.score
		}
	}
	if maxScore == 0 {
		maxScore = 1
	}

	// 构建结果
	results := make([]KeywordSearchResult, 0)
	for id, info := range scores {
		normalizedScore := info.score / maxScore
		if normalizedScore < minScore {
			continue
		}

		cached, ok := r.contentCache[id]
		if !ok {
			continue
		}

		results = append(results, KeywordSearchResult{
			Content:    cached.Content,
			Score:      normalizedScore,
			Metadata:   cached.Metadata,
			Highlights: info.highlights,
		})
	}

	// 排序
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if len(results) > topK {
		results = results[:topK]
	}

	return results, nil
}

// HybridSearch 混合搜索
func (r *SemanticRetriever) HybridSearch(
	ctx context.Context,
	query string,
	options *HybridSearchConfig,
) ([]HybridSearchResult, error) {
	vectorWeight := r.config.VectorWeight
	keywordWeight := r.config.KeywordWeight
	topK := r.config.TopK
	reranking := r.config.Reranking

	if options != nil {
		if options.VectorWeight > 0 {
			vectorWeight = options.VectorWeight
		}
		if options.KeywordWeight > 0 {
			keywordWeight = options.KeywordWeight
		}
		if options.TopK > 0 {
			topK = options.TopK
		}
		reranking = options.Reranking
	}

	// 并行执行两种搜索
	type searchResult struct {
		vector  []VectorSearchResult
		keyword []KeywordSearchResult
		err     error
	}

	vectorCh := make(chan []VectorSearchResult, 1)
	keywordCh := make(chan []KeywordSearchResult, 1)
	errCh := make(chan error, 2)

	go func() {
		vr, err := r.VectorSearch(ctx, query, &HybridSearchConfig{TopK: topK * 2})
		if err != nil {
			errCh <- err
			return
		}
		vectorCh <- vr
	}()

	go func() {
		kr, err := r.KeywordSearch(ctx, query, &HybridSearchConfig{TopK: topK * 2})
		if err != nil {
			errCh <- err
			return
		}
		keywordCh <- kr
	}()

	var vectorResults []VectorSearchResult
	var keywordResults []KeywordSearchResult

	for i := 0; i < 2; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case err := <-errCh:
			return nil, err
		case vr := <-vectorCh:
			vectorResults = vr
		case kr := <-keywordCh:
			keywordResults = kr
		}
	}

	// 合并结果
	merged := make(map[string]*HybridSearchResult)

	for _, vr := range vectorResults {
		merged[vr.ID] = &HybridSearchResult{
			Content:     vr.Content,
			Score:       vr.Score * vectorWeight,
			VectorScore: vr.Score,
			Source:      SourceVector,
			Metadata:    vr.Metadata,
		}
	}

	for _, kr := range keywordResults {
		id := generateContentID(kr.Metadata)
		if existing, ok := merged[id]; ok {
			existing.Score += kr.Score * keywordWeight
			existing.KeywordScore = kr.Score
			existing.Source = SourceHybrid
			existing.Highlights = kr.Highlights
		} else {
			merged[id] = &HybridSearchResult{
				Content:      kr.Content,
				Score:        kr.Score * keywordWeight,
				KeywordScore: kr.Score,
				Source:       SourceKeyword,
				Metadata:     kr.Metadata,
				Highlights:   kr.Highlights,
			}
		}
	}

	// 转换为slice并排序
	results := make([]HybridSearchResult, 0, len(merged))
	for _, result := range merged {
		results = append(results, *result)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if len(results) > topK {
		results = results[:topK]
	}

	// 可选重排序
	if reranking {
		results = r.rerank(results, query)
	}

	return results, nil
}

// IndexContent 索引内容
func (r *SemanticRetriever) IndexContent(id string, content string, metadata ChunkMetadata) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.contentCache[id] = &cachedContent{
		Content:  content,
		Metadata: metadata,
	}

	tokens := r.tokenize(content)
	for _, token := range tokens {
		lowerToken := strings.ToLower(token)
		if r.keywordIndex[lowerToken] == nil {
			r.keywordIndex[lowerToken] = make(map[string]struct{})
		}
		r.keywordIndex[lowerToken][id] = struct{}{}
	}
}

// ClearIndex 清除索引
func (r *SemanticRetriever) ClearIndex() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.keywordIndex = make(map[string]map[string]struct{})
	r.contentCache = make(map[string]*cachedContent)
}

// 私有方法

type scoreInfo struct {
	score      float64
	highlights []string
}

func (r *SemanticRetriever) tokenize(text string) []string {
	// 分词：按空白和标点分割，保留中文字符
	text = strings.ToLower(text)

	var tokens []string
	var current strings.Builder

	for _, char := range text {
		if unicode.IsLetter(char) || unicode.IsDigit(char) || char > 0x4E00 && char < 0x9FFF {
			current.WriteRune(char)
		} else {
			if current.Len() > 1 {
				tokens = append(tokens, current.String())
			}
			current.Reset()
		}
	}
	if current.Len() > 1 {
		tokens = append(tokens, current.String())
	}

	return tokens
}

func (r *SemanticRetriever) calculateTF(content, term string) float64 {
	lowerContent := strings.ToLower(content)
	lowerTerm := strings.ToLower(term)
	count := strings.Count(lowerContent, lowerTerm)
	words := len(strings.Fields(content))
	if words == 0 {
		return 0
	}
	return float64(count) / float64(words)
}

func (r *SemanticRetriever) createHighlight(content, term string) string {
	pattern := regexp.MustCompile(`(?i)(.{0,30})(\b` + regexp.QuoteMeta(term) + `\b)(.{0,30})`)
	match := pattern.FindStringSubmatch(content)
	if len(match) < 4 {
		return ""
	}
	return "..." + match[1] + "**" + match[2] + "**" + match[3] + "..."
}

func (r *SemanticRetriever) applyFilters(
	results []HybridSearchResult,
	params SearchHistoricalContextParams,
) []HybridSearchResult {
	filtered := make([]HybridSearchResult, 0)

	for _, res := range results {
		if params.SessionID != "" && res.Metadata.SessionID != params.SessionID {
			continue
		}
		if params.AgentRole != "" && res.Metadata.AgentRole != params.AgentRole {
			continue
		}
		if params.GitCommitHash != "" && res.Metadata.GitCommitHash != params.GitCommitHash {
			continue
		}
		if params.TimeRange != nil {
			if res.Metadata.Timestamp < params.TimeRange.Start ||
				res.Metadata.Timestamp > params.TimeRange.End {
				continue
			}
		}
		filtered = append(filtered, res)
	}

	return filtered
}

func (r *SemanticRetriever) rerank(results []HybridSearchResult, query string) []HybridSearchResult {
	queryTokens := make(map[string]struct{})
	for _, token := range r.tokenize(query) {
		queryTokens[token] = struct{}{}
	}

	for i := range results {
		contentTokens := make(map[string]struct{})
		for _, token := range r.tokenize(results[i].Content) {
			contentTokens[token] = struct{}{}
		}

		overlap := 0
		for token := range queryTokens {
			if _, ok := contentTokens[token]; ok {
				overlap++
			}
		}

		coverage := float64(overlap) / float64(len(queryTokens))
		results[i].Score = results[i].Score * (1 + coverage*0.2)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results
}

func generateContentID(metadata ChunkMetadata) string {
	return metadata.SessionID + "_" + string(rune(metadata.MessageIndex)) + "_" + string(rune(metadata.ChunkIndex))
}
