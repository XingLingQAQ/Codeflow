package memory

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ChunkerConfig 分块器配置
type ChunkerConfig struct {
	ChunkSize    int
	ChunkOverlap int
	Separator    string
}

// DefaultChunkerConfig 默认分块器配置
var DefaultChunkerConfig = ChunkerConfig{
	ChunkSize:    500,
	ChunkOverlap: 50,
	Separator:    "\n",
}

// TextChunker 文本分块器
type TextChunker struct {
	config ChunkerConfig
}

// NewTextChunker 创建文本分块器
func NewTextChunker(config *ChunkerConfig) *TextChunker {
	cfg := DefaultChunkerConfig
	if config != nil {
		if config.ChunkSize > 0 {
			cfg.ChunkSize = config.ChunkSize
		}
		if config.ChunkOverlap >= 0 {
			cfg.ChunkOverlap = config.ChunkOverlap
		}
		if config.Separator != "" {
			cfg.Separator = config.Separator
		}
	}
	return &TextChunker{config: cfg}
}

// BaseMetadata 基础元数据（不含 chunkIndex）
type BaseMetadata struct {
	SessionID     string
	AgentRole     string
	GitCommitHash string
	MessageIndex  int
	Timestamp     int64
	Source        SourceType
}

// Chunk 将文本分割为块
func (c *TextChunker) Chunk(text string, baseMeta BaseMetadata) []DocumentChunk {
	chunks := make([]DocumentChunk, 0)
	separator := c.config.Separator
	chunkSize := c.config.ChunkSize
	chunkOverlap := c.config.ChunkOverlap

	segments := strings.Split(text, separator)
	currentChunk := ""
	chunkIndex := 0

	for _, segment := range segments {
		var potentialChunk string
		if currentChunk != "" {
			potentialChunk = currentChunk + separator + segment
		} else {
			potentialChunk = segment
		}

		if len(potentialChunk) > chunkSize && currentChunk != "" {
			// 保存当前块
			chunks = append(chunks, c.createChunk(currentChunk, chunkIndex, baseMeta))
			chunkIndex++

			// 计算重叠部分
			if chunkOverlap > 0 {
				overlapStart := len(currentChunk) - chunkOverlap
				if overlapStart < 0 {
					overlapStart = 0
				}
				currentChunk = currentChunk[overlapStart:] + separator + segment
			} else {
				currentChunk = segment
			}
		} else {
			currentChunk = potentialChunk
		}
	}

	// 保存最后一个块
	if strings.TrimSpace(currentChunk) != "" {
		chunks = append(chunks, c.createChunk(currentChunk, chunkIndex, baseMeta))
	}

	return chunks
}

// TokenEstimator Token 估算函数类型
type TokenEstimator func(text string) int

// ChunkByTokens 按 Token 数量分块
func (c *TextChunker) ChunkByTokens(text string, baseMeta BaseMetadata, estimateTokens TokenEstimator) []DocumentChunk {
	chunks := make([]DocumentChunk, 0)
	chunkSize := c.config.ChunkSize
	chunkOverlap := c.config.ChunkOverlap

	sentences := c.splitIntoSentences(text)
	currentChunk := ""
	currentTokens := 0
	chunkIndex := 0

	for _, sentence := range sentences {
		sentenceTokens := estimateTokens(sentence)

		if currentTokens+sentenceTokens > chunkSize && currentChunk != "" {
			chunks = append(chunks, c.createChunk(currentChunk, chunkIndex, baseMeta))
			chunkIndex++

			// 重叠处理
			if chunkOverlap > 0 {
				words := strings.Fields(currentChunk)
				overlapWordCount := chunkOverlap / 4
				if overlapWordCount > len(words) {
					overlapWordCount = len(words)
				}
				overlapWords := words[len(words)-overlapWordCount:]
				currentChunk = strings.Join(overlapWords, " ") + " " + sentence
				currentTokens = estimateTokens(currentChunk)
			} else {
				currentChunk = sentence
				currentTokens = sentenceTokens
			}
		} else {
			if currentChunk != "" {
				currentChunk = currentChunk + " " + sentence
			} else {
				currentChunk = sentence
			}
			currentTokens += sentenceTokens
		}
	}

	if strings.TrimSpace(currentChunk) != "" {
		chunks = append(chunks, c.createChunk(currentChunk, chunkIndex, baseMeta))
	}

	return chunks
}

// createChunk 创建文档块
func (c *TextChunker) createChunk(content string, chunkIndex int, baseMeta BaseMetadata) DocumentChunk {
	return DocumentChunk{
		ID:      fmt.Sprintf("%s_%d_%d", baseMeta.SessionID, baseMeta.MessageIndex, chunkIndex),
		Content: strings.TrimSpace(content),
		Metadata: ChunkMetadata{
			SessionID:     baseMeta.SessionID,
			AgentRole:     baseMeta.AgentRole,
			GitCommitHash: baseMeta.GitCommitHash,
			MessageIndex:  baseMeta.MessageIndex,
			ChunkIndex:    chunkIndex,
			Timestamp:     baseMeta.Timestamp,
			Source:        baseMeta.Source,
		},
	}
}

// splitIntoSentences 按句子分割
func (c *TextChunker) splitIntoSentences(text string) []string {
	// 支持中英文标点
	re := regexp.MustCompile(`(?<=[.!?。！？])\s+`)
	parts := re.Split(text, -1)

	sentences := make([]string, 0, len(parts))
	for _, s := range parts {
		if strings.TrimSpace(s) != "" {
			sentences = append(sentences, s)
		}
	}
	return sentences
}

// SimpleTokenEstimator 简单的 Token 估算器（约 4 字符一个 token）
func SimpleTokenEstimator(text string) int {
	return (len(text) + 3) / 4
}

// NewBaseMetadata 创建基础元数据
func NewBaseMetadata(sessionID, agentRole string, messageIndex int, source SourceType) BaseMetadata {
	return BaseMetadata{
		SessionID:    sessionID,
		AgentRole:    agentRole,
		MessageIndex: messageIndex,
		Timestamp:    time.Now().UnixMilli(),
		Source:       source,
	}
}
