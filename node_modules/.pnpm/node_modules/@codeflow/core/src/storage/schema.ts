/**
 * SQLite Schema 定义
 * 支持外键约束和索引优化
 */

export const SCHEMA_VERSION = 1;

export const CREATE_TABLES_SQL = `
-- 启用外键约束
PRAGMA foreign_keys = ON;

-- 会话表
CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  model TEXT,
  config TEXT
);

-- 消息表
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

-- 检查点表
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

-- Schema 版本表
CREATE TABLE IF NOT EXISTS schema_version (
  version INTEGER PRIMARY KEY
);

-- 索引优化
CREATE INDEX IF NOT EXISTS idx_messages_session_id ON messages(session_id);
CREATE INDEX IF NOT EXISTS idx_messages_timestamp ON messages(timestamp);
CREATE INDEX IF NOT EXISTS idx_messages_session_timestamp ON messages(session_id, timestamp);
CREATE INDEX IF NOT EXISTS idx_checkpoints_session_id ON checkpoints(session_id);
CREATE INDEX IF NOT EXISTS idx_checkpoints_created_at ON checkpoints(created_at);
CREATE INDEX IF NOT EXISTS idx_sessions_updated_at ON sessions(updated_at);
`;

export const DROP_TABLES_SQL = `
DROP TABLE IF EXISTS checkpoints;
DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS schema_version;
`;
