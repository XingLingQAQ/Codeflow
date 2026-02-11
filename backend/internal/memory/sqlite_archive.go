package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

// SQLiteRawArchive SQLite 实现的 Raw Archive。
type SQLiteRawArchive struct {
	db          *sql.DB
	initialized bool
	mu          sync.RWMutex
	dbPath      string
}

// NewSQLiteRawArchive 创建 SQLite Raw Archive 实例。
func NewSQLiteRawArchive(dbPath string) *SQLiteRawArchive {
	if dbPath == "" {
		dbPath = filepath.Join(".", "data", "raw_archive.db")
	}
	return &SQLiteRawArchive{dbPath: dbPath}
}

// Initialize 初始化数据库连接和表结构。
func (a *SQLiteRawArchive) Initialize() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.initialized {
		return nil
	}

	dbDir := filepath.Dir(a.dbPath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return fmt.Errorf("create raw archive db dir: %w", err)
	}

	db, err := sql.Open("sqlite3", a.dbPath)
	if err != nil {
		return fmt.Errorf("open raw archive db: %w", err)
	}

	if _, err := db.Exec("PRAGMA journal_mode = WAL"); err != nil {
		db.Close()
		return fmt.Errorf("enable WAL for raw archive: %w", err)
	}

	ctx := context.Background()
	if err := EnsureRawArchiveSchema(ctx, db); err != nil {
		db.Close()
		return fmt.Errorf("init raw archive schema: %w", err)
	}

	a.db = db
	a.initialized = true
	return nil
}

func (a *SQLiteRawArchive) ensureInitialized() error {
	if !a.initialized {
		return a.Initialize()
	}
	return nil
}

// Store 归档一条原始数据，返回生成的 ID。
func (a *SQLiteRawArchive) Store(ctx context.Context, entry RawEntry) (string, error) {
	if err := a.ensureInitialized(); err != nil {
		return "", err
	}

	if strings.TrimSpace(entry.Content) == "" {
		return "", errors.New("raw archive store: content is required")
	}
	if entry.Type == "" {
		entry.Type = RawEntryConversation
	}
	if entry.ID == "" {
		entry.ID = uuid.New().String()
	}
	if entry.Timestamp <= 0 {
		entry.Timestamp = time.Now().UnixMilli()
	}
	if entry.Metadata == nil {
		entry.Metadata = map[string]interface{}{}
	}

	metadataJSON, err := json.Marshal(entry.Metadata)
	if err != nil {
		return "", fmt.Errorf("marshal metadata: %w", err)
	}

	_, err = a.db.ExecContext(ctx, `
		INSERT INTO raw_archive (id, type, content, metadata_json, timestamp, session_id)
		VALUES (?, ?, ?, ?, ?, ?)
	`, entry.ID, string(entry.Type), entry.Content, string(metadataJSON), entry.Timestamp, entry.SessionID)
	if err != nil {
		return "", fmt.Errorf("insert raw archive entry: %w", err)
	}

	return entry.ID, nil
}

// Get 按 ID 获取单条归档记录。
func (a *SQLiteRawArchive) Get(ctx context.Context, id string) (*RawEntry, error) {
	if err := a.ensureInitialized(); err != nil {
		return nil, err
	}

	id = strings.TrimSpace(id)
	if id == "" {
		return nil, errors.New("raw archive get: id is required")
	}

	row := a.db.QueryRowContext(ctx, `
		SELECT id, type, content, metadata_json, timestamp, session_id
		FROM raw_archive WHERE id = ?
	`, id)

	entry, err := scanRawEntry(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get raw archive entry: %w", err)
	}
	return &entry, nil
}

// Search 全文搜索归档内容（SQLite LIKE）。
func (a *SQLiteRawArchive) Search(ctx context.Context, query string, limit int) ([]RawEntry, error) {
	if err := a.ensureInitialized(); err != nil {
		return nil, err
	}

	query = strings.TrimSpace(query)
	if query == "" {
		return []RawEntry{}, nil
	}
	if limit <= 0 {
		limit = 20
	}

	rows, err := a.db.QueryContext(ctx, `
		SELECT id, type, content, metadata_json, timestamp, session_id
		FROM raw_archive
		WHERE content LIKE ?
		ORDER BY timestamp DESC
		LIMIT ?
	`, "%"+query+"%", limit)
	if err != nil {
		return nil, fmt.Errorf("search raw archive: %w", err)
	}
	defer rows.Close()

	return scanRawEntries(rows)
}

// List 按条件列出归档记录。
func (a *SQLiteRawArchive) List(ctx context.Context, opts *RawArchiveSearchOptions) ([]RawEntry, error) {
	if err := a.ensureInitialized(); err != nil {
		return nil, err
	}

	where := []string{"1=1"}
	params := []interface{}{}

	if opts != nil {
		if opts.Type != "" {
			where = append(where, "type = ?")
			params = append(params, string(opts.Type))
		}
		if opts.SessionID != "" {
			where = append(where, "session_id = ?")
			params = append(params, opts.SessionID)
		}
		if opts.StartAt != nil {
			where = append(where, "timestamp >= ?")
			params = append(params, *opts.StartAt)
		}
		if opts.EndAt != nil {
			where = append(where, "timestamp <= ?")
			params = append(params, *opts.EndAt)
		}
	}

	limit := 50
	offset := 0
	if opts != nil {
		if opts.Limit > 0 {
			limit = opts.Limit
		}
		if opts.Offset > 0 {
			offset = opts.Offset
		}
	}

	q := fmt.Sprintf(`
		SELECT id, type, content, metadata_json, timestamp, session_id
		FROM raw_archive
		WHERE %s
		ORDER BY timestamp DESC
		LIMIT ? OFFSET ?
	`, strings.Join(where, " AND "))
	params = append(params, limit, offset)

	rows, err := a.db.QueryContext(ctx, q, params...)
	if err != nil {
		return nil, fmt.Errorf("list raw archive: %w", err)
	}
	defer rows.Close()

	return scanRawEntries(rows)
}

// Count 返回归档总条数。
func (a *SQLiteRawArchive) Count(ctx context.Context) (int, error) {
	if err := a.ensureInitialized(); err != nil {
		return 0, err
	}

	var count int
	err := a.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM raw_archive").Scan(&count)
	return count, err
}

// Close 关闭数据库连接。
func (a *SQLiteRawArchive) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.db != nil {
		err := a.db.Close()
		a.db = nil
		a.initialized = false
		return err
	}
	return nil
}

// --- scan helpers ---

func scanRawEntry(scanner interface{ Scan(dest ...interface{}) error }) (RawEntry, error) {
	var (
		entry        RawEntry
		entryType    string
		metadataJSON string
	)
	err := scanner.Scan(&entry.ID, &entryType, &entry.Content, &metadataJSON, &entry.Timestamp, &entry.SessionID)
	if err != nil {
		return RawEntry{}, err
	}
	entry.Type = RawEntryType(entryType)
	if metadataJSON != "" {
		if err := json.Unmarshal([]byte(metadataJSON), &entry.Metadata); err != nil {
			return RawEntry{}, fmt.Errorf("unmarshal metadata: %w", err)
		}
	}
	return entry, nil
}

func scanRawEntries(rows *sql.Rows) ([]RawEntry, error) {
	entries := make([]RawEntry, 0)
	for rows.Next() {
		entry, err := scanRawEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

// --- singleton ---

var (
	defaultRawArchive IRawArchive
	rawArchiveMu      sync.Mutex
)

// GetRawArchive 获取全局 Raw Archive 实例。
func GetRawArchive() (IRawArchive, error) {
	rawArchiveMu.Lock()
	defer rawArchiveMu.Unlock()

	if defaultRawArchive != nil {
		return defaultRawArchive, nil
	}

	archive := NewSQLiteRawArchive("")
	if err := archive.Initialize(); err != nil {
		return nil, err
	}
	defaultRawArchive = archive
	return defaultRawArchive, nil
}
