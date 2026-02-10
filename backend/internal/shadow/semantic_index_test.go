package shadow

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/codeflow/backend/internal/memory"
)

// mockVectorStore 模拟向量存储。
type mockVectorStore struct {
	chunks map[string]memory.DocumentChunk
}

func newMockVectorStore() *mockVectorStore {
	return &mockVectorStore{
		chunks: make(map[string]memory.DocumentChunk),
	}
}

func (m *mockVectorStore) Add(_ context.Context, chunks []memory.DocumentChunk) error {
	for _, c := range chunks {
		m.chunks[c.ID] = c
	}
	return nil
}

func (m *mockVectorStore) Search(_ context.Context, query string, opts *memory.VectorSearchOptions) ([]memory.VectorSearchResult, error) {
	topK := 10
	if opts != nil && opts.TopK > 0 {
		topK = opts.TopK
	}

	var results []memory.VectorSearchResult
	for _, chunk := range m.chunks {
		if len(results) >= topK {
			break
		}
		results = append(results, memory.VectorSearchResult{
			Chunk:    chunk,
			Score:    0.8,
			Distance: 0.2,
		})
	}
	return results, nil
}

func (m *mockVectorStore) Delete(_ context.Context, ids []string) error {
	for _, id := range ids {
		delete(m.chunks, id)
	}
	return nil
}

func (m *mockVectorStore) Clear(_ context.Context) error {
	m.chunks = make(map[string]memory.DocumentChunk)
	return nil
}

func (m *mockVectorStore) GetBySessionID(_ context.Context, _ string) ([]memory.DocumentChunk, error) {
	return nil, nil
}

func (m *mockVectorStore) GetByGitCommit(_ context.Context, _ string) ([]memory.DocumentChunk, error) {
	return nil, nil
}

func (m *mockVectorStore) Count(_ context.Context) (int, error) {
	return len(m.chunks), nil
}

func (m *mockVectorStore) GetCollectionInfo(_ context.Context) (*memory.CollectionInfo, error) {
	return &memory.CollectionInfo{
		Name:  "test",
		Count: len(m.chunks),
	}, nil
}

func (m *mockVectorStore) Close() error {
	return nil
}

// mockEmbeddingProvider 模拟 Embedding 提供者。
type mockEmbeddingProvider struct{}

func (p *mockEmbeddingProvider) Embed(_ context.Context, _ string) ([]float64, error) {
	return make([]float64, 128), nil
}

func (p *mockEmbeddingProvider) EmbedBatch(_ context.Context, texts []string) ([][]float64, error) {
	results := make([][]float64, len(texts))
	for i := range texts {
		results[i] = make([]float64, 128)
	}
	return results, nil
}

func (p *mockEmbeddingProvider) GetDimension() int {
	return 128
}

func TestSemanticIndexBuildIndex(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "semantic_index_test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// 创建测试 .md 文件
	domainDir := filepath.Join(tmpDir, "domain", "auth")
	if err := os.MkdirAll(domainDir, 0o755); err != nil {
		t.Fatalf("create domain dir: %v", err)
	}

	if err := os.WriteFile(
		filepath.Join(domainDir, "login.intent.md"),
		[]byte("# Login Intent\n\nHandles user authentication."),
		0o644,
	); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := os.WriteFile(
		filepath.Join(domainDir, "register.intent.md"),
		[]byte("# Register Intent\n\nHandles user registration."),
		0o644,
	); err != nil {
		t.Fatalf("write file: %v", err)
	}

	store := newMockVectorStore()
	provider := &mockEmbeddingProvider{}
	index := NewSemanticIndex(store, provider, &SemanticIndexConfig{IndexDir: tmpDir})

	ctx := context.Background()
	if err := index.BuildIndex(ctx, tmpDir); err != nil {
		t.Fatalf("build index failed: %v", err)
	}

	count, err := store.Count(ctx)
	if err != nil {
		t.Fatalf("count failed: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 indexed documents, got %d", count)
	}
}

func TestSemanticIndexIncrementalUpdate(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "semantic_index_incr_test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	filePath := filepath.Join(tmpDir, "test.md")
	if err := os.WriteFile(filePath, []byte("# Test\n\nOriginal content."), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	store := newMockVectorStore()
	provider := &mockEmbeddingProvider{}
	index := NewSemanticIndex(store, provider, nil)

	ctx := context.Background()
	if err := index.BuildIndex(ctx, tmpDir); err != nil {
		t.Fatalf("build index failed: %v", err)
	}

	// 更新文件
	if err := os.WriteFile(filePath, []byte("# Test\n\nUpdated content."), 0o644); err != nil {
		t.Fatalf("write updated file: %v", err)
	}

	if err := index.IncrementalUpdate(ctx, []string{filePath}); err != nil {
		t.Fatalf("incremental update failed: %v", err)
	}

	count, _ := store.Count(ctx)
	if count != 1 {
		t.Fatalf("expected 1 document after update, got %d", count)
	}
}

func TestSemanticIndexSearch(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "semantic_index_search_test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := os.WriteFile(
		filepath.Join(tmpDir, "test.md"),
		[]byte("# Auth\n\nUser authentication flow."),
		0o644,
	); err != nil {
		t.Fatalf("write file: %v", err)
	}

	store := newMockVectorStore()
	provider := &mockEmbeddingProvider{}
	index := NewSemanticIndex(store, provider, nil)

	ctx := context.Background()
	if err := index.BuildIndex(ctx, tmpDir); err != nil {
		t.Fatalf("build index failed: %v", err)
	}

	results, err := index.Search(ctx, "authentication", 5)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 search result")
	}
}

func TestSemanticIndexDeleteIndex(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "semantic_index_delete_test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	filePath := filepath.Join(tmpDir, "test.md")
	if err := os.WriteFile(filePath, []byte("# Test"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	store := newMockVectorStore()
	provider := &mockEmbeddingProvider{}
	index := NewSemanticIndex(store, provider, nil)

	ctx := context.Background()
	if err := index.BuildIndex(ctx, tmpDir); err != nil {
		t.Fatalf("build index failed: %v", err)
	}

	if err := index.DeleteIndex(ctx, filePath); err != nil {
		t.Fatalf("delete index failed: %v", err)
	}

	count, _ := store.Count(ctx)
	if count != 0 {
		t.Fatalf("expected 0 documents after delete, got %d", count)
	}
}

func TestSemanticIndexGetStats(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "semantic_index_stats_test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := os.WriteFile(
		filepath.Join(tmpDir, "test.md"),
		[]byte("# Test"),
		0o644,
	); err != nil {
		t.Fatalf("write file: %v", err)
	}

	store := newMockVectorStore()
	provider := &mockEmbeddingProvider{}
	index := NewSemanticIndex(store, provider, nil)

	ctx := context.Background()
	if err := index.BuildIndex(ctx, tmpDir); err != nil {
		t.Fatalf("build index failed: %v", err)
	}

	stats, err := index.GetIndexStats(ctx)
	if err != nil {
		t.Fatalf("get stats failed: %v", err)
	}
	if stats.DocumentCount != 1 {
		t.Fatalf("expected 1 document, got %d", stats.DocumentCount)
	}
	if stats.LastUpdated.IsZero() {
		t.Fatal("expected non-zero last updated time")
	}
}
