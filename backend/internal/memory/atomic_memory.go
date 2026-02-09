package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// AtomicMemorySource 原子记忆来源
// 与 SourceType 保持同值域，便于后续复用已有链路。
type AtomicMemorySource string

const (
	AtomicMemorySourceUser      AtomicMemorySource = "user"
	AtomicMemorySourceAssistant AtomicMemorySource = "assistant"
	AtomicMemorySourceSystem    AtomicMemorySource = "system"
)

// AtomicMemory 原子记忆数据结构
// 设计目标：细粒度、可检索、可向量化。
type AtomicMemory struct {
	ID        string             `json:"id"`
	Timestamp int64              `json:"timestamp"`
	Content   string             `json:"content"`
	Tags      []string           `json:"tags"`
	SessionID string             `json:"session_id"`
	FolderID  *string            `json:"folder_id,omitempty"`
	Source    AtomicMemorySource `json:"source"`
	Importance float64           `json:"importance"`
	Embedding []float64          `json:"embedding,omitempty"`
}

// AtomicMemorySearchOptions 原子记忆检索选项。
type AtomicMemorySearchOptions struct {
	Limit    int
	Offset   int
	SessionID string
	FolderID string
	Tags     []string
	StartAt  *int64
	EndAt    *int64
}

// CreateAtomicMemoriesTableSQL 原子记忆 SQLite schema。
// embedding_json 使用 JSON 文本落盘，兼容无扩展 SQLite 环境。
const CreateAtomicMemoriesTableSQL = `
CREATE TABLE IF NOT EXISTS atomic_memories (
  id TEXT PRIMARY KEY,
  timestamp INTEGER NOT NULL,
  content TEXT NOT NULL,
  tags_json TEXT NOT NULL DEFAULT '[]',
  session_id TEXT NOT NULL,
  folder_id TEXT,
  source TEXT NOT NULL CHECK (source IN ('user', 'assistant', 'system')),
  importance REAL NOT NULL CHECK (importance >= 0 AND importance <= 1),
  embedding_json TEXT,
  vector_dim INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
  updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_atomic_memories_session_time
  ON atomic_memories(session_id, timestamp DESC);

CREATE INDEX IF NOT EXISTS idx_atomic_memories_source_time
  ON atomic_memories(source, timestamp DESC);

CREATE INDEX IF NOT EXISTS idx_atomic_memories_folder_time
  ON atomic_memories(folder_id, timestamp DESC);
`

// EnsureAtomicMemorySchema 初始化/迁移原子记忆表结构。
func EnsureAtomicMemorySchema(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return errors.New("atomic memory schema init failed: db is nil")
	}

	if _, err := db.ExecContext(ctx, CreateAtomicMemoriesTableSQL); err != nil {
		return fmt.Errorf("create atomic memory schema: %w", err)
	}

	return nil
}

// Validate 校验 AtomicMemory 基本字段边界。
func (m *AtomicMemory) Validate() error {
	if m == nil {
		return errors.New("atomic memory is nil")
	}
	if strings.TrimSpace(m.ID) == "" {
		return errors.New("id is required")
	}
	if m.Timestamp <= 0 {
		return errors.New("timestamp must be positive")
	}
	if strings.TrimSpace(m.Content) == "" {
		return errors.New("content is required")
	}
	if strings.TrimSpace(m.SessionID) == "" {
		return errors.New("session_id is required")
	}
	if m.Importance < 0 || m.Importance > 1 {
		return errors.New("importance must be in range [0,1]")
	}
	if !isValidAtomicSource(m.Source) {
		return fmt.Errorf("invalid source: %s", m.Source)
	}

	// 仅校验 tags/embedding 可序列化，避免后续落盘失败。
	if _, err := json.Marshal(m.Tags); err != nil {
		return fmt.Errorf("invalid tags: %w", err)
	}
	if len(m.Embedding) > 0 {
		if _, err := json.Marshal(m.Embedding); err != nil {
			return fmt.Errorf("invalid embedding: %w", err)
		}
	}

	return nil
}

func isValidAtomicSource(source AtomicMemorySource) bool {
	switch source {
	case AtomicMemorySourceUser, AtomicMemorySourceAssistant, AtomicMemorySourceSystem:
		return true
	default:
		return false
	}
}

// TagsJSON 将 tags 编码为 JSON 文本。
func (m *AtomicMemory) TagsJSON() (string, error) {
	if m == nil {
		return "", errors.New("atomic memory is nil")
	}
	b, err := json.Marshal(m.Tags)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// EmbeddingJSON 将 embedding 编码为 JSON 文本。
func (m *AtomicMemory) EmbeddingJSON() (string, error) {
	if m == nil {
		return "", errors.New("atomic memory is nil")
	}
	if len(m.Embedding) == 0 {
		return "", nil
	}
	b, err := json.Marshal(m.Embedding)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
