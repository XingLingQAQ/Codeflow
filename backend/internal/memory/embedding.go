package memory

import (
	"context"
	"math"
	"regexp"
	"strings"
	"unicode"
)

// SimpleEmbeddingProvider 基于 TF-IDF 的本地向量化（无需外部服务）
type SimpleEmbeddingProvider struct {
	dimension     int
	vocabulary    map[string]int
	idf           map[string]float64
	documentCount int
}

// NewSimpleEmbeddingProvider 创建简单 Embedding 提供者
func NewSimpleEmbeddingProvider(dimension int) *SimpleEmbeddingProvider {
	if dimension <= 0 {
		dimension = 384
	}
	return &SimpleEmbeddingProvider{
		dimension:  dimension,
		vocabulary: make(map[string]int),
		idf:        make(map[string]float64),
	}
}

// Embed 将文本转换为向量
func (p *SimpleEmbeddingProvider) Embed(_ context.Context, text string) ([]float64, error) {
	tokens := p.tokenize(text)
	tf := p.calculateTF(tokens)
	return p.vectorize(tf), nil
}

// EmbedBatch 批量向量化
func (p *SimpleEmbeddingProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float64, error) {
	results := make([][]float64, len(texts))
	for i, text := range texts {
		vec, err := p.Embed(ctx, text)
		if err != nil {
			return nil, err
		}
		results[i] = vec
	}
	return results, nil
}

// GetDimension 获取向量维度
func (p *SimpleEmbeddingProvider) GetDimension() int {
	return p.dimension
}

// Train 训练词汇表（可选，用于提高质量）
func (p *SimpleEmbeddingProvider) Train(documents []string) {
	p.documentCount = len(documents)
	docFreq := make(map[string]int)

	for _, doc := range documents {
		tokens := p.tokenize(doc)
		seen := make(map[string]bool)
		for _, token := range tokens {
			if !seen[token] {
				seen[token] = true
				docFreq[token]++
				if _, exists := p.vocabulary[token]; !exists {
					p.vocabulary[token] = len(p.vocabulary)
				}
			}
		}
	}

	// 计算 IDF
	for token, freq := range docFreq {
		p.idf[token] = math.Log(float64(p.documentCount)/float64(freq+1)) + 1
	}
}

// tokenize 分词
func (p *SimpleEmbeddingProvider) tokenize(text string) []string {
	text = strings.ToLower(text)
	// 移除非字母数字字符（保留中文）
	re := regexp.MustCompile(`[^\w\s\p{Han}]`)
	text = re.ReplaceAllString(text, " ")

	fields := strings.Fields(text)
	tokens := make([]string, 0, len(fields))
	for _, t := range fields {
		// 过滤短词（但保留中文单字）
		if len(t) > 1 || containsChinese(t) {
			tokens = append(tokens, t)
		}
	}
	return tokens
}

// containsChinese 检查是否包含中文字符
func containsChinese(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

// calculateTF 计算词频
func (p *SimpleEmbeddingProvider) calculateTF(tokens []string) map[string]float64 {
	tf := make(map[string]float64)
	for _, token := range tokens {
		tf[token]++
	}

	// 归一化
	maxFreq := 0.0
	for _, freq := range tf {
		if freq > maxFreq {
			maxFreq = freq
		}
	}

	if maxFreq > 0 {
		for token, freq := range tf {
			tf[token] = freq / maxFreq
		}
	}
	return tf
}

// vectorize 向量化
func (p *SimpleEmbeddingProvider) vectorize(tf map[string]float64) []float64 {
	vector := make([]float64, p.dimension)

	for token, tfValue := range tf {
		idfValue := 1.0
		if v, ok := p.idf[token]; ok {
			idfValue = v
		}
		tfidf := tfValue * idfValue

		// 使用哈希将 token 映射到向量维度
		hash := p.hashToken(token)
		indices := p.getIndices(hash, 3)

		for _, idx := range indices {
			vector[idx] += tfidf / float64(len(indices))
		}
	}

	// L2 归一化
	return p.normalize(vector)
}

// hashToken token 哈希
func (p *SimpleEmbeddingProvider) hashToken(token string) uint32 {
	var hash uint32 = 0
	for _, c := range token {
		hash = (hash << 5) - hash + uint32(c)
	}
	return hash
}

// getIndices 获取多个索引
func (p *SimpleEmbeddingProvider) getIndices(hash uint32, count int) []int {
	indices := make([]int, count)
	for i := 0; i < count; i++ {
		indices[i] = int((hash + uint32(i)*7919) % uint32(p.dimension))
	}
	return indices
}

// normalize L2 归一化
func (p *SimpleEmbeddingProvider) normalize(vector []float64) []float64 {
	var magnitude float64
	for _, v := range vector {
		magnitude += v * v
	}
	magnitude = math.Sqrt(magnitude)

	if magnitude == 0 {
		return vector
	}

	result := make([]float64, len(vector))
	for i, v := range vector {
		result[i] = v / magnitude
	}
	return result
}
