package audit

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestFileAuditStorageAppendAndGet(t *testing.T) {
	// 使用临时目录
	tmpDir, err := os.MkdirTemp("", "audit_test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	storage, err := CreateFileAuditStorage(&FileStorageConfig{
		LogDir:          tmpDir,
		FilePrefix:      "test",
		MaxFileSize:     1024 * 1024,
		MaxFiles:        5,
		VerifyOnStartup: false,
		FlushInterval:   100,
	})
	if err != nil {
		t.Fatalf("create storage: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()

	// 创建测试条目
	entry := &AuditLogEntry{
		ID:        "entry-001",
		Timestamp: time.Now().UnixMilli(),
		EventType: EventAccess,
		Severity:  SeverityInfo,
		Actor: AuditActor{
			ID:   "user-001",
			Type: "user",
			Name: "Test User",
		},
		Resource: AuditResource{
			Type: "file",
			ID:   "file-001",
			Path: "/test/file.txt",
		},
		Action:       "read",
		Outcome:      OutcomeSuccess,
		PreviousHash: GenesisHash,
	}
	entry.Hash = CalculateEntryHash(entry)

	// 追加
	if err := storage.Append(ctx, entry); err != nil {
		t.Fatalf("append: %v", err)
	}

	// 等待刷新
	time.Sleep(200 * time.Millisecond)

	// 获取
	retrieved, err := storage.Get(ctx, "entry-001")
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if retrieved == nil {
		t.Fatal("expected entry, got nil")
	}

	if retrieved.ID != entry.ID {
		t.Errorf("expected ID %s, got %s", entry.ID, retrieved.ID)
	}
}

func TestFileAuditStorageQuery(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "audit_test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	storage, err := CreateFileAuditStorage(&FileStorageConfig{
		LogDir:          tmpDir,
		FilePrefix:      "test",
		VerifyOnStartup: false,
		FlushInterval:   100,
	})
	if err != nil {
		t.Fatalf("create storage: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()
	prevHash := GenesisHash

	// 添加多条记录
	for i := 0; i < 5; i++ {
		entry := &AuditLogEntry{
			ID:        fmt.Sprintf("entry-%03d", i),
			Timestamp: time.Now().UnixMilli() + int64(i*1000),
			EventType: AuditEventType([]AuditEventType{EventAccess, EventModify, EventCreate}[i%3]),
			Severity:  SeverityInfo,
			Actor: AuditActor{
				ID:   fmt.Sprintf("user-%d", i%2),
				Type: "user",
			},
			Resource: AuditResource{
				Type: "file",
				ID:   fmt.Sprintf("file-%d", i),
			},
			Action:       "test",
			Outcome:      OutcomeSuccess,
			PreviousHash: prevHash,
		}
		entry.Hash = CalculateEntryHash(entry)
		prevHash = entry.Hash

		if err := storage.Append(ctx, entry); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	// 等待刷新
	time.Sleep(200 * time.Millisecond)

	// 查询所有
	results, err := storage.Query(ctx, &AuditQuery{})
	if err != nil {
		t.Fatalf("query all: %v", err)
	}
	if len(results) != 5 {
		t.Errorf("expected 5 results, got %d", len(results))
	}

	// 按事件类型查询
	results, err = storage.Query(ctx, &AuditQuery{
		EventTypes: []AuditEventType{EventAccess},
	})
	if err != nil {
		t.Fatalf("query by event type: %v", err)
	}
	if len(results) != 2 { // i=0, i=3
		t.Errorf("expected 2 access events, got %d", len(results))
	}

	// 分页查询
	results, err = storage.Query(ctx, &AuditQuery{
		Limit:  2,
		Offset: 1,
	})
	if err != nil {
		t.Fatalf("query with pagination: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results with pagination, got %d", len(results))
	}
}

func TestFileAuditStorageHashChain(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "audit_test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	storage, err := CreateFileAuditStorage(&FileStorageConfig{
		LogDir:          tmpDir,
		FilePrefix:      "test",
		VerifyOnStartup: false,
		FlushInterval:   100,
	})
	if err != nil {
		t.Fatalf("create storage: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()
	prevHash := GenesisHash

	// 添加链式记录
	for i := 0; i < 3; i++ {
		entry := &AuditLogEntry{
			ID:           fmt.Sprintf("chain-%d", i),
			Timestamp:    time.Now().UnixMilli() + int64(i),
			EventType:    EventAccess,
			Severity:     SeverityInfo,
			Actor:        AuditActor{ID: "user", Type: "user"},
			Resource:     AuditResource{Type: "test", ID: fmt.Sprintf("res-%d", i)},
			Action:       "test",
			Outcome:      OutcomeSuccess,
			PreviousHash: prevHash,
		}
		entry.Hash = CalculateEntryHash(entry)
		prevHash = entry.Hash

		storage.Append(ctx, entry)
	}

	// 等待刷新
	time.Sleep(200 * time.Millisecond)

	// 验证哈希链
	result, err := storage.VerifyHashChain(ctx)
	if err != nil {
		t.Fatalf("verify hash chain: %v", err)
	}

	if !result.Valid {
		t.Errorf("expected valid hash chain, got invalid. Broken at: %s", result.BrokenChainAt)
	}
	if result.CheckedEntries != 3 {
		t.Errorf("expected 3 checked entries, got %d", result.CheckedEntries)
	}
}

func TestFileAuditStorageRotation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "audit_test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	storage, err := CreateFileAuditStorage(&FileStorageConfig{
		LogDir:          tmpDir,
		FilePrefix:      "test",
		MaxFileSize:     500, // 很小的文件大小以触发轮转
		MaxFiles:        3,
		VerifyOnStartup: false,
		FlushInterval:   50,
	})
	if err != nil {
		t.Fatalf("create storage: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()
	prevHash := GenesisHash

	// 添加足够多的记录以触发轮转
	for i := 0; i < 20; i++ {
		entry := &AuditLogEntry{
			ID:           fmt.Sprintf("rotate-entry-%03d", i),
			Timestamp:    time.Now().UnixMilli() + int64(i),
			EventType:    EventAccess,
			Severity:     SeverityInfo,
			Actor:        AuditActor{ID: "user", Type: "user", Name: "Test User With Long Name"},
			Resource:     AuditResource{Type: "file", ID: fmt.Sprintf("file-%d", i), Path: "/very/long/path/to/file"},
			Action:       "read_with_long_action_name",
			Outcome:      OutcomeSuccess,
			PreviousHash: prevHash,
		}
		entry.Hash = CalculateEntryHash(entry)
		prevHash = entry.Hash

		storage.Append(ctx, entry)
		time.Sleep(10 * time.Millisecond)
	}

	// 等待刷新
	time.Sleep(200 * time.Millisecond)

	// 检查文件数量
	stats, err := storage.GetStorageStats()
	if err != nil {
		t.Fatalf("get stats: %v", err)
	}

	if stats.TotalFiles > 3 {
		t.Errorf("expected max 3 files, got %d", stats.TotalFiles)
	}
}

func TestFileAuditStorageClear(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "audit_test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	storage, err := CreateFileAuditStorage(&FileStorageConfig{
		LogDir:          tmpDir,
		FilePrefix:      "test",
		VerifyOnStartup: false,
		FlushInterval:   100,
	})
	if err != nil {
		t.Fatalf("create storage: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()

	// 添加记录
	entry := &AuditLogEntry{
		ID:           "clear-test",
		Timestamp:    time.Now().UnixMilli(),
		EventType:    EventAccess,
		Severity:     SeverityInfo,
		Actor:        AuditActor{ID: "user", Type: "user"},
		Resource:     AuditResource{Type: "test", ID: "res"},
		Action:       "test",
		Outcome:      OutcomeSuccess,
		PreviousHash: GenesisHash,
	}
	entry.Hash = CalculateEntryHash(entry)
	storage.Append(ctx, entry)

	time.Sleep(200 * time.Millisecond)

	// 验证有记录
	count, _ := storage.Count(ctx, nil)
	if count == 0 {
		t.Fatal("expected at least one entry before clear")
	}

	// 清空
	if err := storage.Clear(ctx); err != nil {
		t.Fatalf("clear: %v", err)
	}

	// 验证已清空
	count, _ = storage.Count(ctx, nil)
	if count != 0 {
		t.Errorf("expected 0 entries after clear, got %d", count)
	}
}

func TestCalculateEntryHash(t *testing.T) {
	entry := &AuditLogEntry{
		ID:           "hash-test",
		Timestamp:    1234567890,
		EventType:    EventAccess,
		Severity:     SeverityInfo,
		Actor:        AuditActor{ID: "user", Type: "user"},
		Resource:     AuditResource{Type: "file", ID: "file"},
		Action:       "read",
		Outcome:      OutcomeSuccess,
		PreviousHash: GenesisHash,
	}

	hash1 := CalculateEntryHash(entry)
	hash2 := CalculateEntryHash(entry)

	// 相同输入应产生相同哈希
	if hash1 != hash2 {
		t.Error("same entry should produce same hash")
	}

	// 哈希应该是 64 字符的十六进制字符串
	if len(hash1) != 64 {
		t.Errorf("expected 64 char hash, got %d", len(hash1))
	}

	// 修改条目应产生不同哈希
	entry.Action = "write"
	hash3 := CalculateEntryHash(entry)
	if hash1 == hash3 {
		t.Error("different entry should produce different hash")
	}
}

func TestAuditQueryMatching(t *testing.T) {
	storage := NewFileAuditStorage(nil)

	entry := &AuditLogEntry{
		ID:        "match-test",
		Timestamp: 1000,
		EventType: EventAccess,
		Severity:  SeverityWarning,
		Actor:     AuditActor{ID: "user-1", Type: "user"},
		Resource:  AuditResource{Type: "file", ID: "file-1"},
		Action:    "read",
		Outcome:   OutcomeSuccess,
	}

	// 空查询应匹配所有
	if !storage.matchesQuery(entry, nil) {
		t.Error("nil query should match")
	}

	// 时间范围
	if storage.matchesQuery(entry, &AuditQuery{StartTime: 2000}) {
		t.Error("should not match: start time after entry")
	}
	if storage.matchesQuery(entry, &AuditQuery{EndTime: 500}) {
		t.Error("should not match: end time before entry")
	}
	if !storage.matchesQuery(entry, &AuditQuery{StartTime: 500, EndTime: 1500}) {
		t.Error("should match: entry in time range")
	}

	// 事件类型
	if !storage.matchesQuery(entry, &AuditQuery{EventTypes: []AuditEventType{EventAccess}}) {
		t.Error("should match: event type matches")
	}
	if storage.matchesQuery(entry, &AuditQuery{EventTypes: []AuditEventType{EventModify}}) {
		t.Error("should not match: event type differs")
	}

	// Actor ID
	if !storage.matchesQuery(entry, &AuditQuery{ActorID: "user-1"}) {
		t.Error("should match: actor ID matches")
	}
	if storage.matchesQuery(entry, &AuditQuery{ActorID: "user-2"}) {
		t.Error("should not match: actor ID differs")
	}

	// Outcome
	if !storage.matchesQuery(entry, &AuditQuery{Outcome: OutcomeSuccess}) {
		t.Error("should match: outcome matches")
	}
	if storage.matchesQuery(entry, &AuditQuery{Outcome: OutcomeFailure}) {
		t.Error("should not match: outcome differs")
	}
}
