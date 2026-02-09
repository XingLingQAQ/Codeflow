package memory

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func TestAtomicMemoryValidate(t *testing.T) {
	now := time.Now().Unix()
	folder := "folder-a"
	valid := &AtomicMemory{
		ID:         "mem-1",
		Timestamp:  now,
		Content:    "用户偏好中文回答，倾向简洁说明。",
		Tags:       []string{"preference", "language"},
		SessionID:  "session-1",
		FolderID:   &folder,
		Source:     AtomicMemorySourceUser,
		Importance: 0.8,
		Embedding:  []float64{0.1, 0.2, 0.3},
	}

	if err := valid.Validate(); err != nil {
		t.Fatalf("expected valid atomic memory, got error: %v", err)
	}

	invalid := *valid
	invalid.Importance = 1.2
	if err := invalid.Validate(); err == nil {
		t.Fatalf("expected error when importance out of range")
	}

	invalid2 := *valid
	invalid2.Source = AtomicMemorySource("unknown")
	if err := invalid2.Validate(); err == nil {
		t.Fatalf("expected error when source invalid")
	}
}

func TestAtomicMemoryJSONHelpers(t *testing.T) {
	mem := &AtomicMemory{
		ID:         "mem-2",
		Timestamp:  time.Now().Unix(),
		Content:    "记录关键决策。",
		Tags:       []string{"decision"},
		SessionID:  "session-2",
		Source:     AtomicMemorySourceAssistant,
		Importance: 0.7,
		Embedding:  []float64{0.01, 0.02},
	}

	tagsJSON, err := mem.TagsJSON()
	if err != nil {
		t.Fatalf("TagsJSON failed: %v", err)
	}
	if tagsJSON != "[\"decision\"]" {
		t.Fatalf("unexpected tags json: %s", tagsJSON)
	}

	embJSON, err := mem.EmbeddingJSON()
	if err != nil {
		t.Fatalf("EmbeddingJSON failed: %v", err)
	}
	if embJSON == "" {
		t.Fatalf("expected embedding json not empty")
	}

	mem.Embedding = nil
	embEmpty, err := mem.EmbeddingJSON()
	if err != nil {
		t.Fatalf("EmbeddingJSON for empty embedding failed: %v", err)
	}
	if embEmpty != "" {
		t.Fatalf("expected empty embedding json, got: %s", embEmpty)
	}
}

func TestEnsureAtomicMemorySchema(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "atomic_schema_test")
	if err != nil {
		t.Fatalf("create temp dir failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "atomic.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open db failed: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := EnsureAtomicMemorySchema(ctx, db); err != nil {
		t.Fatalf("EnsureAtomicMemorySchema failed: %v", err)
	}

	// 再执行一次验证幂等。
	if err := EnsureAtomicMemorySchema(ctx, db); err != nil {
		t.Fatalf("EnsureAtomicMemorySchema second run failed: %v", err)
	}

	var tableName string
	err = db.QueryRowContext(ctx, `
		SELECT name FROM sqlite_master WHERE type='table' AND name='atomic_memories'
	`).Scan(&tableName)
	if err != nil {
		t.Fatalf("query atomic_memories table failed: %v", err)
	}
	if tableName != "atomic_memories" {
		t.Fatalf("unexpected table name: %s", tableName)
	}
}
