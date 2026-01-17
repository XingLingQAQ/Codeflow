package memory

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestSimpleEmbeddingProvider(t *testing.T) {
	provider := NewSimpleEmbeddingProvider(384)

	ctx := context.Background()

	// 测试单个文本嵌入
	vec, err := provider.Embed(ctx, "hello world")
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	if len(vec) != 384 {
		t.Errorf("expected dimension 384, got %d", len(vec))
	}

	// 验证 L2 归一化
	var magnitude float64
	for _, v := range vec {
		magnitude += v * v
	}
	if magnitude < 0.99 || magnitude > 1.01 {
		t.Errorf("vector not normalized, magnitude: %f", magnitude)
	}

	// 测试批量嵌入
	vecs, err := provider.EmbedBatch(ctx, []string{"hello", "world"})
	if err != nil {
		t.Fatalf("EmbedBatch failed: %v", err)
	}
	if len(vecs) != 2 {
		t.Errorf("expected 2 vectors, got %d", len(vecs))
	}

	// 测试维度获取
	if provider.GetDimension() != 384 {
		t.Errorf("expected dimension 384, got %d", provider.GetDimension())
	}
}

func TestTextChunker(t *testing.T) {
	chunker := NewTextChunker(&ChunkerConfig{
		ChunkSize:    50,
		ChunkOverlap: 10,
		Separator:    "\n",
	})

	baseMeta := BaseMetadata{
		SessionID:    "test-session",
		AgentRole:    "assistant",
		MessageIndex: 0,
		Timestamp:    time.Now().UnixMilli(),
		Source:       SourceAssistant,
	}

	text := "Line 1 content here\nLine 2 content here\nLine 3 content here\nLine 4 content here"
	chunks := chunker.Chunk(text, baseMeta)

	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}

	// 验证块 ID 格式
	for i, chunk := range chunks {
		if chunk.Metadata.ChunkIndex != i {
			t.Errorf("chunk %d has wrong index %d", i, chunk.Metadata.ChunkIndex)
		}
		if chunk.Metadata.SessionID != "test-session" {
			t.Errorf("chunk %d has wrong session ID", i)
		}
	}
}

func TestCosineSimilarity(t *testing.T) {
	// 相同向量相似度应为 1
	a := []float64{1, 0, 0}
	b := []float64{1, 0, 0}
	sim := cosineSimilarity(a, b)
	if sim < 0.99 || sim > 1.01 {
		t.Errorf("same vectors should have similarity ~1, got %f", sim)
	}

	// 正交向量相似度应为 0
	c := []float64{1, 0, 0}
	d := []float64{0, 1, 0}
	sim = cosineSimilarity(c, d)
	if sim < -0.01 || sim > 0.01 {
		t.Errorf("orthogonal vectors should have similarity ~0, got %f", sim)
	}

	// 相反向量相似度应为 -1
	e := []float64{1, 0, 0}
	f := []float64{-1, 0, 0}
	sim = cosineSimilarity(e, f)
	if sim < -1.01 || sim > -0.99 {
		t.Errorf("opposite vectors should have similarity ~-1, got %f", sim)
	}
}

func TestSQLiteVectorStore(t *testing.T) {
	// 使用临时数据库
	tmpDir, err := os.MkdirTemp("", "memory_test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := CreateSQLiteVectorStore(&VectorStoreConfig{
		CollectionName: "test_collection",
		DBPath:         tmpDir + "/test.db",
		WALMode:        true,
	}, nil)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// 测试添加
	chunks := []DocumentChunk{
		{
			ID:      "chunk1",
			Content: "Hello world this is a test",
			Metadata: ChunkMetadata{
				SessionID:    "session1",
				AgentRole:    "assistant",
				MessageIndex: 0,
				ChunkIndex:   0,
				Timestamp:    time.Now().UnixMilli(),
				Source:       SourceAssistant,
			},
		},
		{
			ID:      "chunk2",
			Content: "Another test content here",
			Metadata: ChunkMetadata{
				SessionID:    "session1",
				AgentRole:    "user",
				MessageIndex: 1,
				ChunkIndex:   0,
				Timestamp:    time.Now().UnixMilli(),
				Source:       SourceUser,
			},
		},
	}

	if err := store.Add(ctx, chunks); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// 测试计数
	count, err := store.Count(ctx)
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}
	if count != 2 {
		t.Errorf("expected count 2, got %d", count)
	}

	// 测试搜索
	results, err := store.Search(ctx, "hello world test", &VectorSearchOptions{
		TopK:     10,
		MinScore: 0,
	})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected at least one search result")
	}

	// 测试按 session ID 获取
	sessionChunks, err := store.GetBySessionID(ctx, "session1")
	if err != nil {
		t.Fatalf("GetBySessionID failed: %v", err)
	}
	if len(sessionChunks) != 2 {
		t.Errorf("expected 2 chunks for session1, got %d", len(sessionChunks))
	}

	// 测试集合信息
	info, err := store.GetCollectionInfo(ctx)
	if err != nil {
		t.Fatalf("GetCollectionInfo failed: %v", err)
	}
	if info.Name != "test_collection" {
		t.Errorf("expected collection name 'test_collection', got %s", info.Name)
	}
	if info.Count != 2 {
		t.Errorf("expected count 2, got %d", info.Count)
	}

	// 测试删除
	if err := store.Delete(ctx, []string{"chunk1"}); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	count, _ = store.Count(ctx)
	if count != 1 {
		t.Errorf("expected count 1 after delete, got %d", count)
	}

	// 测试清空
	if err := store.Clear(ctx); err != nil {
		t.Fatalf("Clear failed: %v", err)
	}

	count, _ = store.Count(ctx)
	if count != 0 {
		t.Errorf("expected count 0 after clear, got %d", count)
	}
}
