package shadow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/codeflow/backend/internal/memory"
)

// IndexedDocument 已索引的文档。
type IndexedDocument struct {
	ID        string    `json:"id"`
	FilePath  string    `json:"file_path"`
	Content   string    `json:"content"`
	UpdatedAt time.Time `json:"updated_at"`
}

// IndexStats 索引统计信息。
type IndexStats struct {
	DocumentCount int       `json:"document_count"`
	LastUpdated   time.Time `json:"last_updated"`
}

// SemanticIndexConfig 语义索引配置。
type SemanticIndexConfig struct {
	IndexDir string
}

// SemanticIndex .codeflow 文档的语义索引。
type SemanticIndex struct {
	mu                sync.RWMutex
	vectorStore       memory.IVectorStore
	embeddingProvider memory.IEmbeddingProvider
	config            SemanticIndexConfig
	indexedFiles      map[string]time.Time // filePath -> lastIndexed
}

// NewSemanticIndex 创建语义索引实例。
func NewSemanticIndex(
	vectorStore memory.IVectorStore,
	embeddingProvider memory.IEmbeddingProvider,
	config *SemanticIndexConfig,
) *SemanticIndex {
	cfg := SemanticIndexConfig{
		IndexDir: ".codeflow",
	}
	if config != nil {
		if config.IndexDir != "" {
			cfg.IndexDir = config.IndexDir
		}
	}
	return &SemanticIndex{
		vectorStore:       vectorStore,
		embeddingProvider: embeddingProvider,
		config:            cfg,
		indexedFiles:       make(map[string]time.Time),
	}
}

// BuildIndex 扫描 .codeflow 目录下的 .md 文件，生成 embedding 并存入向量库。
func (s *SemanticIndex) BuildIndex(ctx context.Context, codeflowDir string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	mdFiles, err := s.scanMarkdownFiles(codeflowDir)
	if err != nil {
		return fmt.Errorf("scan markdown files: %w", err)
	}

	for _, filePath := range mdFiles {
		if err := s.indexFile(ctx, filePath); err != nil {
			return fmt.Errorf("index file %s: %w", filePath, err)
		}
	}

	return nil
}

// IncrementalUpdate 仅重新索引变更的文件。
func (s *SemanticIndex) IncrementalUpdate(ctx context.Context, changedFiles []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, filePath := range changedFiles {
		info, err := os.Stat(filePath)
		if err != nil {
			if os.IsNotExist(err) {
				if err := s.deleteFileLocked(ctx, filePath); err != nil {
					return fmt.Errorf("delete index for %s: %w", filePath, err)
				}
				continue
			}
			return fmt.Errorf("stat file %s: %w", filePath, err)
		}

		lastIndexed, exists := s.indexedFiles[filePath]
		if exists && !info.ModTime().After(lastIndexed) {
			continue
		}

		if err := s.indexFile(ctx, filePath); err != nil {
			return fmt.Errorf("index file %s: %w", filePath, err)
		}
	}

	return nil
}

// Search 语义搜索。
func (s *SemanticIndex) Search(ctx context.Context, query string, topK int) ([]memory.VectorSearchResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if topK <= 0 {
		topK = 10
	}

	opts := &memory.VectorSearchOptions{
		TopK: topK,
	}

	return s.vectorStore.Search(ctx, query, opts)
}

// DeleteIndex 删除指定文件的索引。
func (s *SemanticIndex) DeleteIndex(ctx context.Context, fileID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deleteFileLocked(ctx, fileID)
}

// GetIndexStats 获取索引统计信息。
func (s *SemanticIndex) GetIndexStats(ctx context.Context) (*IndexStats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count, err := s.vectorStore.Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("get count: %w", err)
	}

	var lastUpdated time.Time
	for _, t := range s.indexedFiles {
		if t.After(lastUpdated) {
			lastUpdated = t
		}
	}

	return &IndexStats{
		DocumentCount: count,
		LastUpdated:   lastUpdated,
	}, nil
}

func (s *SemanticIndex) indexFile(ctx context.Context, filePath string) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	fileID := s.fileID(filePath)

	// 先删除旧索引
	_ = s.vectorStore.Delete(ctx, []string{fileID})

	chunk := memory.DocumentChunk{
		ID:      fileID,
		Content: string(content),
		Metadata: memory.ChunkMetadata{
			SessionID:    "semantic-index",
			AgentRole:    "indexer",
			MessageIndex: 0,
			ChunkIndex:   0,
			Timestamp:    time.Now().Unix(),
			Source:        memory.SourceSystem,
		},
	}

	if err := s.vectorStore.Add(ctx, []memory.DocumentChunk{chunk}); err != nil {
		return fmt.Errorf("add to vector store: %w", err)
	}

	s.indexedFiles[filePath] = time.Now()
	return nil
}

func (s *SemanticIndex) deleteFileLocked(ctx context.Context, filePath string) error {
	fileID := s.fileID(filePath)
	if err := s.vectorStore.Delete(ctx, []string{fileID}); err != nil {
		return err
	}
	delete(s.indexedFiles, filePath)
	return nil
}

func (s *SemanticIndex) fileID(filePath string) string {
	return "semantic:" + filepath.ToSlash(filePath)
}

func (s *SemanticIndex) scanMarkdownFiles(dir string) ([]string, error) {
	var files []string

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if strings.HasSuffix(info.Name(), ".md") {
			files = append(files, path)
		}
		return nil
	})

	if err != nil {
		return nil, err
	}
	return files, nil
}
