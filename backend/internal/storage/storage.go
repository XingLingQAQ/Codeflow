package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

const createTablesSQL = `
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  model TEXT,
  config TEXT
);

CREATE TABLE IF NOT EXISTS messages (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL,
  role TEXT NOT NULL CHECK (role IN ('user', 'assistant', 'system')),
  content TEXT NOT NULL,
  timestamp INTEGER NOT NULL,
  model TEXT,
  token_count INTEGER,
  parent_id TEXT,
  FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE,
  FOREIGN KEY (parent_id) REFERENCES messages(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS checkpoints (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL,
  git_hash TEXT,
  dialog_state_hash TEXT NOT NULL,
  vector_state_hash TEXT,
  created_at INTEGER NOT NULL,
  description TEXT,
  FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS schema_version (
  version INTEGER PRIMARY KEY
);

CREATE INDEX IF NOT EXISTS idx_messages_session_id ON messages(session_id);
CREATE INDEX IF NOT EXISTS idx_messages_timestamp ON messages(timestamp);
CREATE INDEX IF NOT EXISTS idx_messages_session_timestamp ON messages(session_id, timestamp);
CREATE INDEX IF NOT EXISTS idx_checkpoints_session_id ON checkpoints(session_id);
CREATE INDEX IF NOT EXISTS idx_checkpoints_created_at ON checkpoints(created_at);
CREATE INDEX IF NOT EXISTS idx_sessions_updated_at ON sessions(updated_at);
`

// SessionStorage SQLite 会话存储实现
type SessionStorage struct {
	db *sql.DB
	mu sync.RWMutex
}

// NewSessionStorage 创建会话存储
func NewSessionStorage(dbPath string) (*SessionStorage, error) {
	connStr, err := buildSessionSQLiteConnString(dbPath)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite3", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	storage := &SessionStorage{db: db}
	if err := storage.initialize(); err != nil {
		db.Close()
		return nil, err
	}

	return storage, nil
}

func buildSessionSQLiteConnString(dbPath string) (string, error) {
	if dbPath == "" || dbPath == ":memory:" {
		return "file::memory:?cache=shared&_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000", nil
	}

	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("create session db dir: %w", err)
		}
	}

	return dbPath + "?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000", nil
}

func (s *SessionStorage) initialize() error {
	_, err := s.db.Exec(createTablesSQL)
	if err != nil {
		return fmt.Errorf("failed to create tables: %w", err)
	}

	var version int
	err = s.db.QueryRow("SELECT version FROM schema_version LIMIT 1").Scan(&version)
	if err == sql.ErrNoRows {
		_, err = s.db.Exec("INSERT INTO schema_version (version) VALUES (?)", SchemaVersion)
		if err != nil {
			return fmt.Errorf("failed to insert schema version: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("failed to check schema version: %w", err)
	}

	return nil
}

// CreateSession 创建会话
func (s *SessionStorage) CreateSession(input CreateSessionInput) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixMilli()
	session := &Session{
		ID:        uuid.New().String(),
		Title:     input.Title,
		CreatedAt: now,
		UpdatedAt: now,
		Model:     input.Model,
	}

	if session.Title == "" {
		session.Title = "New Session"
	}

	if input.Config != nil {
		configBytes, err := json.Marshal(input.Config)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal config: %w", err)
		}
		session.Config = string(configBytes)
	}

	_, err := s.db.Exec(
		"INSERT INTO sessions (id, title, created_at, updated_at, model, config) VALUES (?, ?, ?, ?, ?, ?)",
		session.ID, session.Title, session.CreatedAt, session.UpdatedAt, nullString(session.Model), nullString(session.Config),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to insert session: %w", err)
	}

	return session, nil
}

// GetSession 获取会话
func (s *SessionStorage) GetSession(id string) (*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var session Session
	var model, config sql.NullString

	err := s.db.QueryRow(
		"SELECT id, title, created_at, updated_at, model, config FROM sessions WHERE id = ?",
		id,
	).Scan(&session.ID, &session.Title, &session.CreatedAt, &session.UpdatedAt, &model, &config)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	session.Model = model.String
	session.Config = config.String

	return &session, nil
}

// GetAllSessions 获取所有会话
func (s *SessionStorage) GetAllSessions(options *QueryOptions) ([]Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	opts := normalizeQueryOptions(options, "updated_at", "DESC", 100)

	query := fmt.Sprintf(
		"SELECT id, title, created_at, updated_at, model, config FROM sessions ORDER BY %s %s LIMIT ? OFFSET ?",
		opts.OrderBy, opts.Order,
	)

	rows, err := s.db.Query(query, opts.Limit, opts.Offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query sessions: %w", err)
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var session Session
		var model, config sql.NullString
		if err := rows.Scan(&session.ID, &session.Title, &session.CreatedAt, &session.UpdatedAt, &model, &config); err != nil {
			return nil, fmt.Errorf("failed to scan session: %w", err)
		}
		session.Model = model.String
		session.Config = config.String
		sessions = append(sessions, session)
	}

	return sessions, nil
}

// UpdateSession 更新会话
func (s *SessionStorage) UpdateSession(id string, updates *Session) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, err := s.getSessionUnlocked(id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, nil
	}

	updated := &Session{
		ID:        existing.ID,
		Title:     existing.Title,
		CreatedAt: existing.CreatedAt,
		UpdatedAt: time.Now().UnixMilli(),
		Model:     existing.Model,
		Config:    existing.Config,
	}

	if updates.Title != "" {
		updated.Title = updates.Title
	}
	if updates.Model != "" {
		updated.Model = updates.Model
	}
	if updates.Config != "" {
		updated.Config = updates.Config
	}

	_, err = s.db.Exec(
		"UPDATE sessions SET title = ?, updated_at = ?, model = ?, config = ? WHERE id = ?",
		updated.Title, updated.UpdatedAt, nullString(updated.Model), nullString(updated.Config), id,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update session: %w", err)
	}

	return updated, nil
}

// DeleteSession 删除会话
func (s *SessionStorage) DeleteSession(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec("DELETE FROM sessions WHERE id = ?", id)
	if err != nil {
		return false, fmt.Errorf("failed to delete session: %w", err)
	}

	affected, _ := result.RowsAffected()
	return affected > 0, nil
}

// CreateMessage 创建消息
func (s *SessionStorage) CreateMessage(input CreateMessageInput) (*SessionMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	message := &SessionMessage{
		ID:         uuid.New().String(),
		SessionID:  input.SessionID,
		Role:       input.Role,
		Content:    input.Content,
		Timestamp:  time.Now().UnixMilli(),
		Model:      input.Model,
		TokenCount: input.TokenCount,
		ParentID:   input.ParentID,
	}

	_, err := s.db.Exec(
		"INSERT INTO messages (id, session_id, role, content, timestamp, model, token_count, parent_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		message.ID, message.SessionID, string(message.Role), message.Content, message.Timestamp,
		nullString(message.Model), nullInt(message.TokenCount), nullString(message.ParentID),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to insert message: %w", err)
	}

	// Update session's updated_at
	_, _ = s.db.Exec("UPDATE sessions SET updated_at = ? WHERE id = ?", time.Now().UnixMilli(), input.SessionID)

	return message, nil
}

// GetMessage 获取消息
func (s *SessionStorage) GetMessage(id string) (*SessionMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.getMessageUnlocked(id)
}

func (s *SessionStorage) getMessageUnlocked(id string) (*SessionMessage, error) {
	var msg SessionMessage
	var model, parentID sql.NullString
	var tokenCount sql.NullInt64
	var role string

	err := s.db.QueryRow(
		"SELECT id, session_id, role, content, timestamp, model, token_count, parent_id FROM messages WHERE id = ?",
		id,
	).Scan(&msg.ID, &msg.SessionID, &role, &msg.Content, &msg.Timestamp, &model, &tokenCount, &parentID)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get message: %w", err)
	}

	msg.Role = Role(role)
	msg.Model = model.String
	msg.TokenCount = int(tokenCount.Int64)
	msg.ParentID = parentID.String

	return &msg, nil
}

// GetSessionMessages 获取会话消息
func (s *SessionStorage) GetSessionMessages(sessionID string, options *QueryOptions) ([]SessionMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	opts := normalizeQueryOptions(options, "timestamp", "ASC", 1000)

	query := fmt.Sprintf(
		"SELECT id, session_id, role, content, timestamp, model, token_count, parent_id FROM messages WHERE session_id = ? ORDER BY %s %s LIMIT ? OFFSET ?",
		opts.OrderBy, opts.Order,
	)

	rows, err := s.db.Query(query, sessionID, opts.Limit, opts.Offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query messages: %w", err)
	}
	defer rows.Close()

	var messages []SessionMessage
	for rows.Next() {
		var msg SessionMessage
		var role string
		var model, parentID sql.NullString
		var tokenCount sql.NullInt64

		if err := rows.Scan(&msg.ID, &msg.SessionID, &role, &msg.Content, &msg.Timestamp, &model, &tokenCount, &parentID); err != nil {
			return nil, fmt.Errorf("failed to scan message: %w", err)
		}
		msg.Role = Role(role)
		msg.Model = model.String
		msg.TokenCount = int(tokenCount.Int64)
		msg.ParentID = parentID.String
		messages = append(messages, msg)
	}

	return messages, nil
}

// DeleteMessage 删除消息
func (s *SessionStorage) DeleteMessage(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec("DELETE FROM messages WHERE id = ?", id)
	if err != nil {
		return false, fmt.Errorf("failed to delete message: %w", err)
	}

	affected, _ := result.RowsAffected()
	return affected > 0, nil
}

// DeleteSessionMessages 删除会话所有消息
func (s *SessionStorage) DeleteSessionMessages(sessionID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec("DELETE FROM messages WHERE session_id = ?", sessionID)
	if err != nil {
		return 0, fmt.Errorf("failed to delete session messages: %w", err)
	}

	affected, _ := result.RowsAffected()
	return int(affected), nil
}

// CreateCheckpoint 创建检查点
func (s *SessionStorage) CreateCheckpoint(input CreateCheckpointInput) (*Checkpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	checkpoint := &Checkpoint{
		ID:              uuid.New().String(),
		SessionID:       input.SessionID,
		GitHash:         input.GitHash,
		DialogStateHash: input.DialogStateHash,
		VectorStateHash: input.VectorStateHash,
		CreatedAt:       time.Now().UnixMilli(),
		Description:     input.Description,
	}

	_, err := s.db.Exec(
		"INSERT INTO checkpoints (id, session_id, git_hash, dialog_state_hash, vector_state_hash, created_at, description) VALUES (?, ?, ?, ?, ?, ?, ?)",
		checkpoint.ID, checkpoint.SessionID, nullString(checkpoint.GitHash), checkpoint.DialogStateHash,
		nullString(checkpoint.VectorStateHash), checkpoint.CreatedAt, nullString(checkpoint.Description),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to insert checkpoint: %w", err)
	}

	return checkpoint, nil
}

// GetCheckpoint 获取检查点
func (s *SessionStorage) GetCheckpoint(id string) (*Checkpoint, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var cp Checkpoint
	var gitHash, vectorStateHash, description sql.NullString

	err := s.db.QueryRow(
		"SELECT id, session_id, git_hash, dialog_state_hash, vector_state_hash, created_at, description FROM checkpoints WHERE id = ?",
		id,
	).Scan(&cp.ID, &cp.SessionID, &gitHash, &cp.DialogStateHash, &vectorStateHash, &cp.CreatedAt, &description)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get checkpoint: %w", err)
	}

	cp.GitHash = gitHash.String
	cp.VectorStateHash = vectorStateHash.String
	cp.Description = description.String

	return &cp, nil
}

// GetSessionCheckpoints 获取会话检查点
func (s *SessionStorage) GetSessionCheckpoints(sessionID string, options *QueryOptions) ([]Checkpoint, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	opts := normalizeQueryOptions(options, "created_at", "DESC", 100)

	query := fmt.Sprintf(
		"SELECT id, session_id, git_hash, dialog_state_hash, vector_state_hash, created_at, description FROM checkpoints WHERE session_id = ? ORDER BY %s %s LIMIT ? OFFSET ?",
		opts.OrderBy, opts.Order,
	)

	rows, err := s.db.Query(query, sessionID, opts.Limit, opts.Offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query checkpoints: %w", err)
	}
	defer rows.Close()

	var checkpoints []Checkpoint
	for rows.Next() {
		var cp Checkpoint
		var gitHash, vectorStateHash, description sql.NullString

		if err := rows.Scan(&cp.ID, &cp.SessionID, &gitHash, &cp.DialogStateHash, &vectorStateHash, &cp.CreatedAt, &description); err != nil {
			return nil, fmt.Errorf("failed to scan checkpoint: %w", err)
		}
		cp.GitHash = gitHash.String
		cp.VectorStateHash = vectorStateHash.String
		cp.Description = description.String
		checkpoints = append(checkpoints, cp)
	}

	return checkpoints, nil
}

// DeleteCheckpoint 删除检查点
func (s *SessionStorage) DeleteCheckpoint(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec("DELETE FROM checkpoints WHERE id = ?", id)
	if err != nil {
		return false, fmt.Errorf("failed to delete checkpoint: %w", err)
	}

	affected, _ := result.RowsAffected()
	return affected > 0, nil
}

// GetSessionWithMessages 获取带消息的会话
func (s *SessionStorage) GetSessionWithMessages(id string) (*SessionWithMessages, error) {
	session, err := s.GetSession(id)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, nil
	}

	messages, err := s.GetSessionMessages(id, nil)
	if err != nil {
		return nil, err
	}

	return &SessionWithMessages{
		Session:  *session,
		Messages: messages,
	}, nil
}

// Close 关闭数据库
func (s *SessionStorage) Close() error {
	return s.db.Close()
}

func (s *SessionStorage) getSessionUnlocked(id string) (*Session, error) {
	var session Session
	var model, config sql.NullString

	err := s.db.QueryRow(
		"SELECT id, title, created_at, updated_at, model, config FROM sessions WHERE id = ?",
		id,
	).Scan(&session.ID, &session.Title, &session.CreatedAt, &session.UpdatedAt, &model, &config)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	session.Model = model.String
	session.Config = config.String

	return &session, nil
}

func normalizeQueryOptions(options *QueryOptions, defaultOrderBy, defaultOrder string, defaultLimit int) QueryOptions {
	opts := QueryOptions{
		Limit:   defaultLimit,
		Offset:  0,
		OrderBy: defaultOrderBy,
		Order:   defaultOrder,
	}

	if options != nil {
		if options.Limit > 0 {
			opts.Limit = options.Limit
		}
		if options.Offset > 0 {
			opts.Offset = options.Offset
		}
		if options.OrderBy != "" {
			opts.OrderBy = options.OrderBy
		}
		if options.Order == "ASC" || options.Order == "DESC" {
			opts.Order = options.Order
		}
	}

	return opts
}

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func nullInt(i int) sql.NullInt64 {
	if i == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(i), Valid: true}
}
