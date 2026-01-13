import Database from 'better-sqlite3';
import { randomUUID } from 'crypto';
import {
  ISessionStorage,
  Session,
  SessionMessage,
  Checkpoint,
  SessionWithMessages,
  CreateSessionInput,
  CreateMessageInput,
  CreateCheckpointInput,
  QueryOptions,
} from './types.js';
import { CREATE_TABLES_SQL, SCHEMA_VERSION } from './schema.js';

/**
 * SQLite 会话存储实现
 * 基于 better-sqlite3，支持外键约束和事务
 */
export class SessionStorage implements ISessionStorage {
  private db: Database.Database;

  constructor(dbPath: string = ':memory:') {
    this.db = new Database(dbPath);
    this.initialize();
  }

  private initialize(): void {
    // 启用外键约束
    this.db.pragma('foreign_keys = ON');

    // 执行 Schema 创建
    this.db.exec(CREATE_TABLES_SQL);

    // 检查并更新 Schema 版本
    const versionRow = this.db.prepare('SELECT version FROM schema_version LIMIT 1').get() as
      | { version: number }
      | undefined;

    if (!versionRow) {
      this.db.prepare('INSERT INTO schema_version (version) VALUES (?)').run(SCHEMA_VERSION);
    }
  }

  // ==================== Session CRUD ====================

  createSession(input: CreateSessionInput): Session {
    const now = Date.now();
    const session: Session = {
      id: randomUUID(),
      title: input.title || 'New Session',
      createdAt: now,
      updatedAt: now,
      model: input.model,
      config: input.config ? JSON.stringify(input.config) : undefined,
    };

    this.db
      .prepare(
        `INSERT INTO sessions (id, title, created_at, updated_at, model, config)
         VALUES (?, ?, ?, ?, ?, ?)`
      )
      .run(
        session.id,
        session.title,
        session.createdAt,
        session.updatedAt,
        session.model,
        session.config
      );

    return session;
  }

  getSession(id: string): Session | null {
    const row = this.db.prepare('SELECT * FROM sessions WHERE id = ?').get(id) as
      | SessionRow
      | undefined;

    return row ? this.rowToSession(row) : null;
  }

  getAllSessions(options?: QueryOptions): Session[] {
    const { limit = 100, offset = 0, orderBy = 'updated_at', order = 'DESC' } = options || {};

    const rows = this.db
      .prepare(`SELECT * FROM sessions ORDER BY ${orderBy} ${order} LIMIT ? OFFSET ?`)
      .all(limit, offset) as SessionRow[];

    return rows.map((row) => this.rowToSession(row));
  }

  updateSession(id: string, updates: Partial<Session>): Session | null {
    const existing = this.getSession(id);
    if (!existing) return null;

    const updated: Session = {
      ...existing,
      ...updates,
      id: existing.id, // 不允许修改 ID
      updatedAt: Date.now(),
    };

    this.db
      .prepare(`UPDATE sessions SET title = ?, updated_at = ?, model = ?, config = ? WHERE id = ?`)
      .run(updated.title, updated.updatedAt, updated.model, updated.config, id);

    return updated;
  }

  deleteSession(id: string): boolean {
    const result = this.db.prepare('DELETE FROM sessions WHERE id = ?').run(id);
    return result.changes > 0;
  }

  // ==================== Message CRUD ====================

  createMessage(input: CreateMessageInput): SessionMessage {
    const message: SessionMessage = {
      id: randomUUID(),
      sessionId: input.sessionId,
      role: input.role,
      content: input.content,
      timestamp: Date.now(),
      model: input.model,
      tokenCount: input.tokenCount,
      parentId: input.parentId,
    };

    this.db
      .prepare(
        `INSERT INTO messages (id, session_id, role, content, timestamp, model, token_count, parent_id)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
      )
      .run(
        message.id,
        message.sessionId,
        message.role,
        message.content,
        message.timestamp,
        message.model,
        message.tokenCount,
        message.parentId
      );

    // 更新会话的 updated_at
    this.db
      .prepare('UPDATE sessions SET updated_at = ? WHERE id = ?')
      .run(Date.now(), input.sessionId);

    return message;
  }

  getMessage(id: string): SessionMessage | null {
    const row = this.db.prepare('SELECT * FROM messages WHERE id = ?').get(id) as
      | MessageRow
      | undefined;

    return row ? this.rowToMessage(row) : null;
  }

  getSessionMessages(sessionId: string, options?: QueryOptions): SessionMessage[] {
    const { limit = 1000, offset = 0, orderBy = 'timestamp', order = 'ASC' } = options || {};

    const rows = this.db
      .prepare(
        `SELECT * FROM messages WHERE session_id = ? ORDER BY ${orderBy} ${order} LIMIT ? OFFSET ?`
      )
      .all(sessionId, limit, offset) as MessageRow[];

    return rows.map((row) => this.rowToMessage(row));
  }

  deleteMessage(id: string): boolean {
    const result = this.db.prepare('DELETE FROM messages WHERE id = ?').run(id);
    return result.changes > 0;
  }

  deleteSessionMessages(sessionId: string): number {
    const result = this.db.prepare('DELETE FROM messages WHERE session_id = ?').run(sessionId);
    return result.changes;
  }

  // ==================== Checkpoint CRUD ====================

  createCheckpoint(input: CreateCheckpointInput): Checkpoint {
    const checkpoint: Checkpoint = {
      id: randomUUID(),
      sessionId: input.sessionId,
      gitHash: input.gitHash,
      dialogStateHash: input.dialogStateHash,
      vectorStateHash: input.vectorStateHash,
      createdAt: Date.now(),
      description: input.description,
    };

    this.db
      .prepare(
        `INSERT INTO checkpoints (id, session_id, git_hash, dialog_state_hash, vector_state_hash, created_at, description)
         VALUES (?, ?, ?, ?, ?, ?, ?)`
      )
      .run(
        checkpoint.id,
        checkpoint.sessionId,
        checkpoint.gitHash,
        checkpoint.dialogStateHash,
        checkpoint.vectorStateHash,
        checkpoint.createdAt,
        checkpoint.description
      );

    return checkpoint;
  }

  getCheckpoint(id: string): Checkpoint | null {
    const row = this.db.prepare('SELECT * FROM checkpoints WHERE id = ?').get(id) as
      | CheckpointRow
      | undefined;

    return row ? this.rowToCheckpoint(row) : null;
  }

  getSessionCheckpoints(sessionId: string, options?: QueryOptions): Checkpoint[] {
    const { limit = 100, offset = 0, orderBy = 'created_at', order = 'DESC' } = options || {};

    const rows = this.db
      .prepare(
        `SELECT * FROM checkpoints WHERE session_id = ? ORDER BY ${orderBy} ${order} LIMIT ? OFFSET ?`
      )
      .all(sessionId, limit, offset) as CheckpointRow[];

    return rows.map((row) => this.rowToCheckpoint(row));
  }

  deleteCheckpoint(id: string): boolean {
    const result = this.db.prepare('DELETE FROM checkpoints WHERE id = ?').run(id);
    return result.changes > 0;
  }

  // ==================== Utility ====================

  getSessionWithMessages(id: string): SessionWithMessages | null {
    const session = this.getSession(id);
    if (!session) return null;

    const messages = this.getSessionMessages(id);

    return { ...session, messages };
  }

  close(): void {
    this.db.close();
  }

  // ==================== Private Helpers ====================

  private rowToSession(row: SessionRow): Session {
    return {
      id: row.id,
      title: row.title,
      createdAt: row.created_at,
      updatedAt: row.updated_at,
      model: row.model || undefined,
      config: row.config || undefined,
    };
  }

  private rowToMessage(row: MessageRow): SessionMessage {
    return {
      id: row.id,
      sessionId: row.session_id,
      role: row.role as 'user' | 'assistant' | 'system',
      content: row.content,
      timestamp: row.timestamp,
      model: row.model || undefined,
      tokenCount: row.token_count || undefined,
      parentId: row.parent_id || undefined,
    };
  }

  private rowToCheckpoint(row: CheckpointRow): Checkpoint {
    return {
      id: row.id,
      sessionId: row.session_id,
      gitHash: row.git_hash || undefined,
      dialogStateHash: row.dialog_state_hash,
      vectorStateHash: row.vector_state_hash || undefined,
      createdAt: row.created_at,
      description: row.description || undefined,
    };
  }
}

// Row 类型定义（数据库返回的原始行）
interface SessionRow {
  id: string;
  title: string;
  created_at: number;
  updated_at: number;
  model: string | null;
  config: string | null;
}

interface MessageRow {
  id: string;
  session_id: string;
  role: string;
  content: string;
  timestamp: number;
  model: string | null;
  token_count: number | null;
  parent_id: string | null;
}

interface CheckpointRow {
  id: string;
  session_id: string;
  git_hash: string | null;
  dialog_state_hash: string;
  vector_state_hash: string | null;
  created_at: number;
  description: string | null;
}
