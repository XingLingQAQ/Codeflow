// Package search - Search service for vector/fulltext/graph/hybrid search
package search

import (
	"context"
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// SearchType 搜索类型
type SearchType string

const (
	SearchTypeVector   SearchType = "vector"
	SearchTypeFulltext SearchType = "fulltext"
	SearchTypeGraph    SearchType = "graph"
	SearchTypeHybrid   SearchType = "hybrid"
)

// SearchResult 通用搜索结果
type SearchResult struct {
	ID         string                 `json:"id"`
	Content    string                 `json:"content"`
	Score      float64                `json:"score"`
	Type       SearchType             `json:"type"`
	Highlights []string               `json:"highlights,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// VectorSearchRequest 向量搜索请求
type VectorSearchRequest struct {
	Query    string  `json:"query" binding:"required"`
	TopK     int     `json:"top_k"`
	MinScore float64 `json:"min_score"`
}

// VectorSearchResponse 向量搜索响应
type VectorSearchResponse struct {
	Results   []VectorResult `json:"results"`
	Total     int            `json:"total"`
	QueryTime int64          `json:"query_time_ms"`
}

// VectorResult 向量搜索结果
type VectorResult struct {
	ID         string                 `json:"id"`
	Content    string                 `json:"content"`
	Score      float64                `json:"score"`
	Similarity float64                `json:"similarity"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// FulltextSearchRequest 全文搜索请求
type FulltextSearchRequest struct {
	Query     string   `json:"query" binding:"required"`
	Fields    []string `json:"fields,omitempty"`
	Highlight bool     `json:"highlight"`
	Limit     int      `json:"limit"`
	Offset    int      `json:"offset"`
}

// FulltextSearchResponse 全文搜索响应
type FulltextSearchResponse struct {
	Results   []FulltextResult `json:"results"`
	Total     int              `json:"total"`
	HasMore   bool             `json:"has_more"`
	QueryTime int64            `json:"query_time_ms"`
}

// FulltextResult 全文搜索结果
type FulltextResult struct {
	ID         string                 `json:"id"`
	Content    string                 `json:"content"`
	Score      float64                `json:"score"`
	Highlights []string               `json:"highlights,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// GraphSearchRequest 图谱搜索请求
type GraphSearchRequest struct {
	Subject       string  `json:"subject,omitempty"`
	Predicate     string  `json:"predicate,omitempty"`
	Object        string  `json:"object,omitempty"`
	MinConfidence float64 `json:"min_confidence"`
	Depth         int     `json:"depth"`
	Limit         int     `json:"limit"`
	Offset        int     `json:"offset"`
}

// GraphSearchResponse 图谱搜索响应
type GraphSearchResponse struct {
	Triples   []TripleResult `json:"triples"`
	Total     int            `json:"total"`
	HasMore   bool           `json:"has_more"`
	QueryTime int64          `json:"query_time_ms"`
}

// TripleResult 三元组结果
type TripleResult struct {
	ID         string                 `json:"id"`
	Subject    EntityRef              `json:"subject"`
	Predicate  string                 `json:"predicate"`
	Object     EntityRef              `json:"object"`
	Confidence float64                `json:"confidence"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// EntityRef 实体引用
type EntityRef struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Type  string `json:"type,omitempty"`
}

// HybridSearchRequest 混合搜索请求
type HybridSearchRequest struct {
	Query         string  `json:"query" binding:"required"`
	VectorWeight  float64 `json:"vector_weight"`
	FulltextWeight float64 `json:"fulltext_weight"`
	GraphWeight   float64 `json:"graph_weight"`
	TopK          int     `json:"top_k"`
	MinScore      float64 `json:"min_score"`
}

// HybridSearchResponse 混合搜索响应
type HybridSearchResponse struct {
	Results   []HybridResult `json:"results"`
	Total     int            `json:"total"`
	QueryTime int64          `json:"query_time_ms"`
}

// HybridResult 混合搜索结果
type HybridResult struct {
	ID             string                 `json:"id"`
	Content        string                 `json:"content"`
	Score          float64                `json:"score"`
	VectorScore    float64                `json:"vector_score,omitempty"`
	FulltextScore  float64                `json:"fulltext_score,omitempty"`
	GraphScore     float64                `json:"graph_score,omitempty"`
	Source         SearchType             `json:"source"`
	Highlights     []string               `json:"highlights,omitempty"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
}

// ISearchService 搜索服务接口
type ISearchService interface {
	VectorSearch(ctx context.Context, req *VectorSearchRequest) (*VectorSearchResponse, error)
	FulltextSearch(ctx context.Context, req *FulltextSearchRequest) (*FulltextSearchResponse, error)
	GraphSearch(ctx context.Context, req *GraphSearchRequest) (*GraphSearchResponse, error)
	HybridSearch(ctx context.Context, req *HybridSearchRequest) (*HybridSearchResponse, error)
	IndexDocument(id string, content string, metadata map[string]interface{})
	IndexTriple(triple TripleResult)
}

// InMemorySearchService 内存实现的搜索服务
type InMemorySearchService struct {
	mu        sync.RWMutex
	documents map[string]*Document
	triples   map[string]*TripleResult
}

// Document 文档
type Document struct {
	ID       string
	Content  string
	Vector   []float64
	Metadata map[string]interface{}
}

// NewInMemorySearchService 创建内存搜索服务
func NewInMemorySearchService() *InMemorySearchService {
	return &InMemorySearchService{
		documents: make(map[string]*Document),
		triples:   make(map[string]*TripleResult),
	}
}

// VectorSearch 向量搜索
func (s *InMemorySearchService) VectorSearch(ctx context.Context, req *VectorSearchRequest) (*VectorSearchResponse, error) {
	start := time.Now()
	s.mu.RLock()
	defer s.mu.RUnlock()

	topK := req.TopK
	if topK <= 0 {
		topK = 10
	}
	minScore := req.MinScore
	if minScore <= 0 {
		minScore = 0.3
	}

	queryVec := s.textToVector(req.Query)
	var results []VectorResult

	for _, doc := range s.documents {
		similarity := s.cosineSimilarity(queryVec, doc.Vector)
		if similarity >= minScore {
			results = append(results, VectorResult{
				ID:         doc.ID,
				Content:    doc.Content,
				Score:      similarity,
				Similarity: similarity,
				Metadata:   doc.Metadata,
			})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if len(results) > topK {
		results = results[:topK]
	}

	return &VectorSearchResponse{
		Results:   results,
		Total:     len(results),
		QueryTime: time.Since(start).Milliseconds(),
	}, nil
}

// FulltextSearch 全文搜索
func (s *InMemorySearchService) FulltextSearch(ctx context.Context, req *FulltextSearchRequest) (*FulltextSearchResponse, error) {
	start := time.Now()
	s.mu.RLock()
	defer s.mu.RUnlock()

	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	offset := req.Offset
	if offset < 0 {
		offset = 0
	}

	queryLower := strings.ToLower(req.Query)
	keywords := strings.Fields(queryLower)
	var results []FulltextResult

	for _, doc := range s.documents {
		contentLower := strings.ToLower(doc.Content)
		score := s.calculateBM25Score(contentLower, keywords)
		if score > 0 {
			var highlights []string
			if req.Highlight {
				highlights = s.extractHighlights(doc.Content, keywords)
			}
			results = append(results, FulltextResult{
				ID:         doc.ID,
				Content:    doc.Content,
				Score:      score,
				Highlights: highlights,
				Metadata:   doc.Metadata,
			})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	total := len(results)
	startIdx := offset
	if startIdx > total {
		startIdx = total
	}
	endIdx := startIdx + limit
	if endIdx > total {
		endIdx = total
	}

	return &FulltextSearchResponse{
		Results:   results[startIdx:endIdx],
		Total:     total,
		HasMore:   endIdx < total,
		QueryTime: time.Since(start).Milliseconds(),
	}, nil
}

// GraphSearch 图谱搜索
func (s *InMemorySearchService) GraphSearch(ctx context.Context, req *GraphSearchRequest) (*GraphSearchResponse, error) {
	start := time.Now()
	s.mu.RLock()
	defer s.mu.RUnlock()

	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	offset := req.Offset
	if offset < 0 {
		offset = 0
	}
	minConf := req.MinConfidence
	if minConf <= 0 {
		minConf = 0.5
	}

	var results []TripleResult

	for _, triple := range s.triples {
		if triple.Confidence < minConf {
			continue
		}
		if req.Subject != "" && !strings.Contains(strings.ToLower(triple.Subject.Label), strings.ToLower(req.Subject)) {
			continue
		}
		if req.Predicate != "" && !strings.Contains(strings.ToLower(triple.Predicate), strings.ToLower(req.Predicate)) {
			continue
		}
		if req.Object != "" && !strings.Contains(strings.ToLower(triple.Object.Label), strings.ToLower(req.Object)) {
			continue
		}
		results = append(results, *triple)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Confidence > results[j].Confidence
	})

	total := len(results)
	startIdx := offset
	if startIdx > total {
		startIdx = total
	}
	endIdx := startIdx + limit
	if endIdx > total {
		endIdx = total
	}

	return &GraphSearchResponse{
		Triples:   results[startIdx:endIdx],
		Total:     total,
		HasMore:   endIdx < total,
		QueryTime: time.Since(start).Milliseconds(),
	}, nil
}

// HybridSearch 混合搜索
func (s *InMemorySearchService) HybridSearch(ctx context.Context, req *HybridSearchRequest) (*HybridSearchResponse, error) {
	start := time.Now()

	vectorWeight := req.VectorWeight
	fulltextWeight := req.FulltextWeight
	if vectorWeight <= 0 && fulltextWeight <= 0 {
		vectorWeight = 0.7
		fulltextWeight = 0.3
	}
	totalWeight := vectorWeight + fulltextWeight + req.GraphWeight
	if totalWeight > 0 {
		vectorWeight /= totalWeight
		fulltextWeight /= totalWeight
	}

	topK := req.TopK
	if topK <= 0 {
		topK = 10
	}
	minScore := req.MinScore
	if minScore <= 0 {
		minScore = 0.3
	}

	vectorResp, _ := s.VectorSearch(ctx, &VectorSearchRequest{
		Query:    req.Query,
		TopK:     topK * 2,
		MinScore: 0,
	})

	fulltextResp, _ := s.FulltextSearch(ctx, &FulltextSearchRequest{
		Query:     req.Query,
		Highlight: true,
		Limit:     topK * 2,
	})

	scoreMap := make(map[string]*HybridResult)

	for _, vr := range vectorResp.Results {
		scoreMap[vr.ID] = &HybridResult{
			ID:          vr.ID,
			Content:     vr.Content,
			VectorScore: vr.Score,
			Source:      SearchTypeVector,
			Metadata:    vr.Metadata,
		}
	}

	for _, fr := range fulltextResp.Results {
		if hr, ok := scoreMap[fr.ID]; ok {
			hr.FulltextScore = fr.Score
			hr.Highlights = fr.Highlights
			hr.Source = SearchTypeHybrid
		} else {
			scoreMap[fr.ID] = &HybridResult{
				ID:            fr.ID,
				Content:       fr.Content,
				FulltextScore: fr.Score,
				Highlights:    fr.Highlights,
				Source:        SearchTypeFulltext,
				Metadata:      fr.Metadata,
			}
		}
	}

	var results []HybridResult
	for _, hr := range scoreMap {
		hr.Score = hr.VectorScore*vectorWeight + hr.FulltextScore*fulltextWeight
		if hr.Score >= minScore {
			results = append(results, *hr)
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if len(results) > topK {
		results = results[:topK]
	}

	return &HybridSearchResponse{
		Results:   results,
		Total:     len(results),
		QueryTime: time.Since(start).Milliseconds(),
	}, nil
}

// IndexDocument 索引文档
func (s *InMemorySearchService) IndexDocument(id string, content string, metadata map[string]interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if id == "" {
		id = uuid.New().String()
	}

	s.documents[id] = &Document{
		ID:       id,
		Content:  content,
		Vector:   s.textToVector(content),
		Metadata: metadata,
	}
}

// IndexTriple 索引三元组
func (s *InMemorySearchService) IndexTriple(triple TripleResult) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if triple.ID == "" {
		triple.ID = uuid.New().String()
	}
	s.triples[triple.ID] = &triple
}

// textToVector 简化的文本向量化 (基于词频)
func (s *InMemorySearchService) textToVector(text string) []float64 {
	words := strings.Fields(strings.ToLower(text))
	wordFreq := make(map[string]int)
	for _, w := range words {
		wordFreq[w]++
	}

	vec := make([]float64, 256)
	for word, freq := range wordFreq {
		idx := s.hashWord(word) % 256
		vec[idx] += float64(freq)
	}

	norm := 0.0
	for _, v := range vec {
		norm += v * v
	}
	norm = math.Sqrt(norm)
	if norm > 0 {
		for i := range vec {
			vec[i] /= norm
		}
	}

	return vec
}

// hashWord 简单哈希
func (s *InMemorySearchService) hashWord(word string) int {
	hash := 0
	for _, c := range word {
		hash = hash*31 + int(c)
	}
	if hash < 0 {
		hash = -hash
	}
	return hash
}

// cosineSimilarity 余弦相似度
func (s *InMemorySearchService) cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// calculateBM25Score 简化的BM25评分
func (s *InMemorySearchService) calculateBM25Score(content string, keywords []string) float64 {
	k1 := 1.2
	b := 0.75
	avgLen := 100.0

	words := strings.Fields(content)
	docLen := float64(len(words))
	wordFreq := make(map[string]int)
	for _, w := range words {
		wordFreq[w]++
	}

	score := 0.0
	for _, kw := range keywords {
		tf := float64(wordFreq[kw])
		if tf > 0 {
			idf := math.Log(1 + (float64(len(s.documents))-tf+0.5)/(tf+0.5))
			tfNorm := (tf * (k1 + 1)) / (tf + k1*(1-b+b*docLen/avgLen))
			score += idf * tfNorm
		}
	}

	return score
}

// extractHighlights 提取高亮片段
func (s *InMemorySearchService) extractHighlights(content string, keywords []string) []string {
	var highlights []string
	sentences := regexp.MustCompile(`[.!?。！？]+`).Split(content, -1)

	for _, sentence := range sentences {
		sentenceLower := strings.ToLower(sentence)
		for _, kw := range keywords {
			if strings.Contains(sentenceLower, kw) {
				highlighted := regexp.MustCompile(`(?i)`+regexp.QuoteMeta(kw)).ReplaceAllString(
					strings.TrimSpace(sentence),
					"<em>$0</em>",
				)
				highlights = append(highlights, highlighted)
				break
			}
		}
		if len(highlights) >= 3 {
			break
		}
	}

	return highlights
}

// 全局服务实例
var defaultSearchService ISearchService

// GetSearchService 获取搜索服务实例
func GetSearchService() ISearchService {
	if defaultSearchService == nil {
		defaultSearchService = NewInMemorySearchService()
	}
	return defaultSearchService
}

// SetSearchService 设置搜索服务实例
func SetSearchService(svc ISearchService) {
	defaultSearchService = svc
}
