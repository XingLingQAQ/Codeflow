package memory

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func setupFolderMemoryService(t *testing.T) (*FolderMemoryService, func()) {
	t.Helper()

	atomicSvc, cleanupAtomic := setupAtomicService(t)

	folderSvc, err := NewFolderMemoryService(atomicSvc)
	if err != nil {
		cleanupAtomic()
		t.Fatalf("create FolderMemoryService failed: %v", err)
	}

	return folderSvc, cleanupAtomic
}

func setupFolderTestDB(t *testing.T) (*FolderMemoryService, *AtomicMemoryService, func()) {
	t.Helper()

	db, cleanupDB := setupAtomicServiceTestDB(t)

	tmpVectorDir, err := os.MkdirTemp("", "folder_vector_test")
	if err != nil {
		cleanupDB()
		t.Fatalf("create temp vector dir failed: %v", err)
	}
	vectorDBPath := filepath.Join(tmpVectorDir, "vectors.db")
	vectorStore := NewSQLiteVectorStore(&VectorStoreConfig{
		CollectionName: "folder_test",
		DBPath:         vectorDBPath,
		WALMode:        true,
	}, nil)

	atomicSvc, err := NewAtomicMemoryService(context.Background(), db, vectorStore, NewSimpleEmbeddingProvider(32))
	if err != nil {
		_ = os.RemoveAll(tmpVectorDir)
		cleanupDB()
		t.Fatalf("create AtomicMemoryService failed: %v", err)
	}

	folderSvc, err := NewFolderMemoryService(atomicSvc)
	if err != nil {
		_ = vectorStore.Close()
		_ = os.RemoveAll(tmpVectorDir)
		cleanupDB()
		t.Fatalf("create FolderMemoryService failed: %v", err)
	}

	cleanup := func() {
		_ = vectorStore.Close()
		_ = os.RemoveAll(tmpVectorDir)
		cleanupDB()
	}

	return folderSvc, atomicSvc, cleanup
}

func TestNewFolderMemoryServiceNilAtomic(t *testing.T) {
	_, err := NewFolderMemoryService(nil)
	if err == nil {
		t.Fatal("expected error for nil atomic service")
	}
}

func TestFolderMemoryServiceAddToFolder(t *testing.T) {
	svc, _, cleanup := setupFolderTestDB(t)
	defer cleanup()

	ctx := context.Background()

	mem := &AtomicMemory{
		ID:         "fm-1",
		Timestamp:  100,
		Content:    "文件夹A的记忆内容",
		Tags:       []string{"test"},
		SessionID:  "session-1",
		Source:     AtomicMemorySourceUser,
		Importance: 0.8,
	}

	if err := svc.AddToFolder(ctx, "folder-A", mem); err != nil {
		t.Fatalf("AddToFolder failed: %v", err)
	}

	if mem.FolderID == nil || *mem.FolderID != "folder-A" {
		t.Fatalf("expected folderId to be set to folder-A, got: %v", mem.FolderID)
	}
}

func TestFolderMemoryServiceAddToFolderEmptyID(t *testing.T) {
	svc, _, cleanup := setupFolderTestDB(t)
	defer cleanup()

	ctx := context.Background()
	mem := &AtomicMemory{
		ID:         "fm-2",
		Timestamp:  100,
		Content:    "内容",
		Tags:       []string{},
		SessionID:  "session-1",
		Source:     AtomicMemorySourceUser,
		Importance: 0.5,
	}

	err := svc.AddToFolder(ctx, "", mem)
	if err == nil {
		t.Fatal("expected error for empty folder_id")
	}
}

func TestFolderMemoryServiceAddToFolderNilMemory(t *testing.T) {
	svc, _, cleanup := setupFolderTestDB(t)
	defer cleanup()

	ctx := context.Background()
	err := svc.AddToFolder(ctx, "folder-A", nil)
	if err == nil {
		t.Fatal("expected error for nil memory")
	}
}

func TestFolderMemoryServiceSearchInFolder(t *testing.T) {
	svc, _, cleanup := setupFolderTestDB(t)
	defer cleanup()

	ctx := context.Background()

	mem1 := &AtomicMemory{
		ID:         "fm-s1",
		Timestamp:  100,
		Content:    "文件夹A的TypeScript偏好",
		Tags:       []string{"preference"},
		SessionID:  "session-1",
		Source:     AtomicMemorySourceUser,
		Importance: 0.8,
	}
	if err := svc.AddToFolder(ctx, "folder-A", mem1); err != nil {
		t.Fatalf("AddToFolder mem1 failed: %v", err)
	}

	mem2 := &AtomicMemory{
		ID:         "fm-s2",
		Timestamp:  200,
		Content:    "文件夹B的Go偏好",
		Tags:       []string{"preference"},
		SessionID:  "session-1",
		Source:     AtomicMemorySourceUser,
		Importance: 0.7,
	}
	if err := svc.AddToFolder(ctx, "folder-B", mem2); err != nil {
		t.Fatalf("AddToFolder mem2 failed: %v", err)
	}

	results, err := svc.SearchInFolder(ctx, "folder-A", "偏好", &AtomicMemorySearchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("SearchInFolder failed: %v", err)
	}

	for _, r := range results {
		if r.FolderID == nil || *r.FolderID != "folder-A" {
			t.Fatalf("expected all results in folder-A, got: %v", r.FolderID)
		}
	}
}

func TestFolderMemoryServiceSearchAcrossFolders(t *testing.T) {
	svc, _, cleanup := setupFolderTestDB(t)
	defer cleanup()

	ctx := context.Background()

	mem1 := &AtomicMemory{
		ID:         "fm-c1",
		Timestamp:  100,
		Content:    "跨文件夹搜索测试A",
		Tags:       []string{"cross"},
		SessionID:  "session-1",
		Source:     AtomicMemorySourceUser,
		Importance: 0.8,
	}
	if err := svc.AddToFolder(ctx, "folder-X", mem1); err != nil {
		t.Fatalf("AddToFolder mem1 failed: %v", err)
	}

	mem2 := &AtomicMemory{
		ID:         "fm-c2",
		Timestamp:  200,
		Content:    "跨文件夹搜索测试B",
		Tags:       []string{"cross"},
		SessionID:  "session-1",
		Source:     AtomicMemorySourceUser,
		Importance: 0.7,
	}
	if err := svc.AddToFolder(ctx, "folder-Y", mem2); err != nil {
		t.Fatalf("AddToFolder mem2 failed: %v", err)
	}

	results, err := svc.SearchAcrossFolders(ctx, "跨文件夹搜索", &AtomicMemorySearchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("SearchAcrossFolders failed: %v", err)
	}

	if len(results) < 1 {
		t.Fatalf("expected at least 1 result from cross-folder search, got %d", len(results))
	}
}

func TestFolderMemoryServiceListFolders(t *testing.T) {
	svc, atomicSvc, cleanup := setupFolderTestDB(t)
	defer cleanup()

	ctx := context.Background()

	folderA := "folder-A"
	folderB := "folder-B"
	input := []*AtomicMemory{
		{
			ID:         "fm-l1",
			Timestamp:  100,
			Content:    "记忆1",
			Tags:       []string{},
			SessionID:  "session-list",
			FolderID:   &folderA,
			Source:     AtomicMemorySourceUser,
			Importance: 0.5,
		},
		{
			ID:         "fm-l2",
			Timestamp:  200,
			Content:    "记忆2",
			Tags:       []string{},
			SessionID:  "session-list",
			FolderID:   &folderA,
			Source:     AtomicMemorySourceUser,
			Importance: 0.5,
		},
		{
			ID:         "fm-l3",
			Timestamp:  300,
			Content:    "记忆3",
			Tags:       []string{},
			SessionID:  "session-list",
			FolderID:   &folderB,
			Source:     AtomicMemorySourceUser,
			Importance: 0.5,
		},
	}

	for _, mem := range input {
		if err := atomicSvc.Add(ctx, mem); err != nil {
			t.Fatalf("Add failed for %s: %v", mem.ID, err)
		}
	}

	folders, err := svc.ListFolders(ctx, "session-list", 100, 0)
	if err != nil {
		t.Fatalf("ListFolders failed: %v", err)
	}

	if len(folders) != 2 {
		t.Fatalf("expected 2 folders, got %d", len(folders))
	}

	// folder-B 应排在前面（最新时间戳更大）
	if folders[0].FolderID != "folder-B" {
		t.Fatalf("expected folder-B first (latest timestamp), got %s", folders[0].FolderID)
	}
	if folders[0].MemoryCount != 1 {
		t.Fatalf("expected folder-B count=1, got %d", folders[0].MemoryCount)
	}
	if folders[1].FolderID != "folder-A" {
		t.Fatalf("expected folder-A second, got %s", folders[1].FolderID)
	}
	if folders[1].MemoryCount != 2 {
		t.Fatalf("expected folder-A count=2, got %d", folders[1].MemoryCount)
	}
}

func TestFolderMemoryServiceListFoldersEmptySession(t *testing.T) {
	svc, _, cleanup := setupFolderTestDB(t)
	defer cleanup()

	ctx := context.Background()
	_, err := svc.ListFolders(ctx, "", 100, 0)
	if err == nil {
		t.Fatal("expected error for empty session_id")
	}
}

func TestFolderMemoryServiceDeleteFolder(t *testing.T) {
	svc, atomicSvc, cleanup := setupFolderTestDB(t)
	defer cleanup()

	ctx := context.Background()

	folderDel := "folder-del"
	input := []*AtomicMemory{
		{
			ID:         "fm-d1",
			Timestamp:  100,
			Content:    "待删除记忆1",
			Tags:       []string{},
			SessionID:  "session-del",
			FolderID:   &folderDel,
			Source:     AtomicMemorySourceUser,
			Importance: 0.5,
		},
		{
			ID:         "fm-d2",
			Timestamp:  200,
			Content:    "待删除记忆2",
			Tags:       []string{},
			SessionID:  "session-del",
			FolderID:   &folderDel,
			Source:     AtomicMemorySourceUser,
			Importance: 0.5,
		},
	}

	for _, mem := range input {
		if err := atomicSvc.Add(ctx, mem); err != nil {
			t.Fatalf("Add failed for %s: %v", mem.ID, err)
		}
	}

	deleted, err := svc.DeleteFolder(ctx, "folder-del", "session-del")
	if err != nil {
		t.Fatalf("DeleteFolder failed: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("expected 2 deleted, got %d", deleted)
	}

	// 验证已删除
	_, err = atomicSvc.GetByID(ctx, "fm-d1")
	if !errors.Is(err, ErrAtomicMemoryNotFound) {
		t.Fatalf("expected ErrAtomicMemoryNotFound after delete, got: %v", err)
	}
}
