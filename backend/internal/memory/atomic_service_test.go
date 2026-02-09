package memory

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func setupAtomicServiceTestDB(t *testing.T) (*sql.DB, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "atomic_service_test")
	if err != nil {
		t.Fatalf("create temp dir failed: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "atomic_service.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("open sqlite failed: %v", err)
	}

	cleanup := func() {
		_ = db.Close()
		_ = os.RemoveAll(tmpDir)
	}

	return db, cleanup
}

func setupAtomicService(t *testing.T) (*AtomicMemoryService, func()) {
	t.Helper()

	db, cleanupDB := setupAtomicServiceTestDB(t)

	// 为向量库使用独立文件，避免与原子表耦合锁。
	tmpVectorDir, err := os.MkdirTemp("", "atomic_vector_test")
	if err != nil {
		cleanupDB()
		t.Fatalf("create temp vector dir failed: %v", err)
	}
	vectorDBPath := filepath.Join(tmpVectorDir, "vectors.db")
	vectorStore := NewSQLiteVectorStore(&VectorStoreConfig{
		CollectionName: "atomic_test",
		DBPath:         vectorDBPath,
		WALMode:        true,
	}, nil)

	svc, err := NewAtomicMemoryService(context.Background(), db, vectorStore, NewSimpleEmbeddingProvider(32))
	if err != nil {
		cleanupDB()
		_ = os.RemoveAll(tmpVectorDir)
		t.Fatalf("create AtomicMemoryService failed: %v", err)
	}

	cleanup := func() {
		_ = vectorStore.Close()
		_ = os.RemoveAll(tmpVectorDir)
		cleanupDB()
	}

	return svc, cleanup
}

func TestAtomicMemoryServiceAddAndSearch(t *testing.T) {
	svc, cleanup := setupAtomicService(t)
	defer cleanup()

	ctx := context.Background()

	mem1 := &AtomicMemory{
		ID:         "am-1",
		Timestamp:  100,
		Content:    "用户偏好使用简体中文回答技术问题",
		Tags:       []string{"preference", "language"},
		SessionID:  "session-1",
		Source:     AtomicMemorySourceUser,
		Importance: 0.9,
	}
	if err := svc.Add(ctx, mem1); err != nil {
		t.Fatalf("Add mem1 failed: %v", err)
	}

	mem2 := &AtomicMemory{
		ID:         "am-2",
		Timestamp:  200,
		Content:    "用户希望回答保持简洁并提供步骤",
		Tags:       []string{"preference", "style"},
		SessionID:  "session-1",
		Source:     AtomicMemorySourceUser,
		Importance: 0.8,
	}
	if err := svc.Add(ctx, mem2); err != nil {
		t.Fatalf("Add mem2 failed: %v", err)
	}

	results, err := svc.Search(ctx, "简体中文", &AtomicMemorySearchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected search results not empty")
	}

	if results[0].ID != "am-1" && results[0].ID != "am-2" {
		t.Fatalf("unexpected result id: %s", results[0].ID)
	}
}

func TestAtomicMemoryServiceSearchByTimeRangeAndTags(t *testing.T) {
	svc, cleanup := setupAtomicService(t)
	defer cleanup()

	ctx := context.Background()
	input := []*AtomicMemory{
		{
			ID:         "am-t1",
			Timestamp:  100,
			Content:    "会话开始背景",
			Tags:       []string{"context"},
			SessionID:  "session-2",
			Source:     AtomicMemorySourceSystem,
			Importance: 0.3,
		},
		{
			ID:         "am-t2",
			Timestamp:  200,
			Content:    "用户偏好短答案",
			Tags:       []string{"preference", "style"},
			SessionID:  "session-2",
			Source:     AtomicMemorySourceUser,
			Importance: 0.7,
		},
		{
			ID:         "am-t3",
			Timestamp:  300,
			Content:    "系统已记录关键决策",
			Tags:       []string{"decision"},
			SessionID:  "session-2",
			Source:     AtomicMemorySourceAssistant,
			Importance: 0.6,
		},
	}

	for _, mem := range input {
		if err := svc.Add(ctx, mem); err != nil {
			t.Fatalf("Add failed for %s: %v", mem.ID, err)
		}
	}

	byTime, err := svc.SearchByTimeRange(ctx, 150, 250)
	if err != nil {
		t.Fatalf("SearchByTimeRange failed: %v", err)
	}
	if len(byTime) != 1 || byTime[0].ID != "am-t2" {
		t.Fatalf("unexpected SearchByTimeRange result: %+v", byTime)
	}

	byTags, err := svc.SearchByTags(ctx, []string{"decision", "context"})
	if err != nil {
		t.Fatalf("SearchByTags failed: %v", err)
	}
	if len(byTags) != 2 {
		t.Fatalf("expected 2 tag results, got %d", len(byTags))
	}
}

func TestAtomicMemoryServiceUpdateDelete(t *testing.T) {
	svc, cleanup := setupAtomicService(t)
	defer cleanup()

	ctx := context.Background()
	folder := "folder-a"
	mem := &AtomicMemory{
		ID:         "am-u1",
		Timestamp:  100,
		Content:    "初始内容",
		Tags:       []string{"draft"},
		SessionID:  "session-3",
		FolderID:   &folder,
		Source:     AtomicMemorySourceUser,
		Importance: 0.4,
	}
	if err := svc.Add(ctx, mem); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	newContent := "更新后的内容"
	newTags := []string{"final", "decision"}
	newImportance := 0.95
	if err := svc.Update(ctx, "am-u1", &AtomicMemoryUpdate{
		Content:    &newContent,
		Tags:       &newTags,
		Importance: &newImportance,
	}); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	updated, err := svc.GetByID(ctx, "am-u1")
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if updated.Content != newContent {
		t.Fatalf("content not updated: %s", updated.Content)
	}
	if len(updated.Tags) != 2 || updated.Tags[0] != "final" {
		t.Fatalf("tags not updated: %+v", updated.Tags)
	}
	if updated.Importance != newImportance {
		t.Fatalf("importance not updated: %f", updated.Importance)
	}

	if err := svc.Delete(ctx, "am-u1"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	if _, err := svc.GetByID(ctx, "am-u1"); !errors.Is(err, ErrAtomicMemoryNotFound) {
		t.Fatalf("expected ErrAtomicMemoryNotFound after delete, got: %v", err)
	}
}

func TestAtomicMemoryServiceGetBySessionAndSearchFilters(t *testing.T) {
	svc, cleanup := setupAtomicService(t)
	defer cleanup()

	ctx := context.Background()
	folderA := "folder-a"
	folderB := "folder-b"
	input := []*AtomicMemory{
		{
			ID:         "am-s1",
			Timestamp:  100,
			Content:    "用户喜欢分点回答",
			Tags:       []string{"style", "preference"},
			SessionID:  "session-4",
			FolderID:   &folderA,
			Source:     AtomicMemorySourceUser,
			Importance: 0.7,
		},
		{
			ID:         "am-s2",
			Timestamp:  200,
			Content:    "用户关注性能优化",
			Tags:       []string{"performance"},
			SessionID:  "session-4",
			FolderID:   &folderB,
			Source:     AtomicMemorySourceUser,
			Importance: 0.8,
		},
		{
			ID:         "am-s3",
			Timestamp:  300,
			Content:    "另一个会话内容",
			Tags:       []string{"other"},
			SessionID:  "session-5",
			FolderID:   &folderA,
			Source:     AtomicMemorySourceAssistant,
			Importance: 0.5,
		},
	}
	for _, mem := range input {
		if err := svc.Add(ctx, mem); err != nil {
			t.Fatalf("Add failed for %s: %v", mem.ID, err)
		}
	}

	sessionResults, err := svc.GetBySession(ctx, "session-4", 10, 0)
	if err != nil {
		t.Fatalf("GetBySession failed: %v", err)
	}
	if len(sessionResults) != 2 {
		t.Fatalf("expected 2 session results, got %d", len(sessionResults))
	}

	start := int64(50)
	end := int64(250)
	filtered, err := svc.Search(ctx, "用户", &AtomicMemorySearchOptions{
		SessionID: "session-4",
		FolderID:  "folder-a",
		Tags:      []string{"style"},
		StartAt:   &start,
		EndAt:     &end,
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("Search with filters failed: %v", err)
	}
	if len(filtered) != 1 || filtered[0].ID != "am-s1" {
		t.Fatalf("unexpected filtered result: %+v", filtered)
	}
}

func TestAtomicMemoryServiceErrorHandling(t *testing.T) {
	svc, cleanup := setupAtomicService(t)
	defer cleanup()

	ctx := context.Background()

	if err := svc.Add(ctx, nil); err == nil {
		t.Fatalf("expected Add nil memory error")
	}

	if _, err := svc.SearchByTimeRange(ctx, 200, 100); err == nil {
		t.Fatalf("expected invalid range error")
	}

	if err := svc.Delete(ctx, "not-exists"); !errors.Is(err, ErrAtomicMemoryNotFound) {
		t.Fatalf("expected ErrAtomicMemoryNotFound, got: %v", err)
	}
}
