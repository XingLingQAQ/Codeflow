package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// RawEntryType 原始数据类型。
type RawEntryType string

const (
	RawEntryConversation RawEntryType = "conversation"
	RawEntryCodeDiff     RawEntryType = "code_diff"
	RawEntryDocument     RawEntryType = "document"
)

// RawEntry 原始数据归档条目。
// 不可变：写入后不允许修改 Content，仅允许追加 Metadata。
type RawEntry struct {
	ID        string                 `json:"id"`
	Type      RawEntryType           `json:"type"`
	Content   string                 `json:"content"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	Timestamp int64                  `json:"timestamp"`
	SessionID string                 `json:"session_id"`
}

// RawArchiveSearchOptions 归档检索选项。
type RawArchiveSearchOptions struct {
	Type      RawEntryType `json:"type,omitempty"`
	SessionID string       `json:"session_id,omitempty"`
	StartAt   *int64       `json:"start_at,omitempty"`
	EndAt     *int64       `json:"end_at,omitempty"`
	Limit     int          `json:"limit,omitempty"`
	Offset    int          `json:"offset,omitempty"`
}

// IRawArchive 原始数据归档接口。
type IRawArchive interface {
	Store(ctx context.Context, entry RawEntry) (string, error)
	Get(ctx context.Context, id string) (*RawEntry, error)
	Search(ctx context.Context, query string, limit int) ([]RawEntry, error)
	List(ctx context.Context, opts *RawArchiveSearchOptions) ([]RawEntry, error)
	Count(ctx context.Context) (int, error)
	Close() error
}

// CreateRawArchiveTableSQL Raw Archive SQLite schema。
const CreateRawArchiveTableSQL = `
CREATE TABLE IF NOT EXISTS raw_archive (
	id TEXT PRIMARY KEY,
	type TEXT NOT NULL CHECK (type IN ('conversation', 'code_diff', 'document')),
	content TEXT NOT NULL,
	metadata_json TEXT NOT NULL DEFAULT '{}',
	timestamp INTEGER NOT NULL,
	session_id TEXT NOT NULL,
	created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_raw_archive_type
	ON raw_archive(type);

CREATE INDEX IF NOT EXISTS idx_raw_archive_session
	ON raw_archive(session_id);

CREATE INDEX IF NOT EXISTS idx_raw_archive_timestamp
	ON raw_archive(timestamp DESC);

CREATE INDEX IF NOT EXISTS idx_raw_archive_session_time
	ON raw_archive(session_id, timestamp DESC);
`

// EnsureRawArchiveSchema 初始化 Raw Archive 表结构。
func EnsureRawArchiveSchema(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return errors.New("raw archive schema init failed: db is nil")
	}
	if _, err := db.ExecContext(ctx, CreateRawArchiveTableSQL); err != nil {
		return fmt.Errorf("create raw archive schema: %w", err)
	}
	return nil
}
