import Database from 'better-sqlite3';
import { randomUUID } from 'crypto';
import { CREATE_TABLES_SQL, SCHEMA_VERSION } from './schema.js';
/**
 * SQLite 会话存储实现
 * 基于 better-sqlite3，支持外键约束和事务
 */
export class SessionStorage {
    constructor(dbPath = ':memory:') {
        this.db = new Database(dbPath);
        this.initialize();
    }
    initialize() {
        // 启用外键约束
        this.db.pragma('foreign_keys = ON');
        // 执行 Schema 创建
        this.db.exec(CREATE_TABLES_SQL);
        // 检查并更新 Schema 版本
        const versionRow = this.db.prepare('SELECT version FROM schema_version LIMIT 1').get();
        if (!versionRow) {
            this.db.prepare('INSERT INTO schema_version (version) VALUES (?)').run(SCHEMA_VERSION);
        }
    }
    // ==================== Session CRUD ====================
    createSession(input) {
        const now = Date.now();
        const session = {
            id: randomUUID(),
            title: input.title || 'New Session',
            createdAt: now,
            updatedAt: now,
            model: input.model,
            config: input.config ? JSON.stringify(input.config) : undefined,
        };
        this.db
            .prepare(`INSERT INTO sessions (id, title, created_at, updated_at, model, config)
         VALUES (?, ?, ?, ?, ?, ?)`)
            .run(session.id, session.title, session.createdAt, session.updatedAt, session.model, session.config);
        return session;
    }
    getSession(id) {
        const row = this.db.prepare('SELECT * FROM sessions WHERE id = ?').get(id);
        return row ? this.rowToSession(row) : null;
    }
    getAllSessions(options) {
        const { limit = 100, offset = 0, orderBy = 'updated_at', order = 'DESC' } = options || {};
        const rows = this.db
            .prepare(`SELECT * FROM sessions ORDER BY ${orderBy} ${order} LIMIT ? OFFSET ?`)
            .all(limit, offset);
        return rows.map((row) => this.rowToSession(row));
    }
    updateSession(id, updates) {
        const existing = this.getSession(id);
        if (!existing)
            return null;
        const updated = {
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
    deleteSession(id) {
        const result = this.db.prepare('DELETE FROM sessions WHERE id = ?').run(id);
        return result.changes > 0;
    }
    // ==================== Message CRUD ====================
    createMessage(input) {
        const message = {
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
            .prepare(`INSERT INTO messages (id, session_id, role, content, timestamp, model, token_count, parent_id)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
            .run(message.id, message.sessionId, message.role, message.content, message.timestamp, message.model, message.tokenCount, message.parentId);
        // 更新会话的 updated_at
        this.db
            .prepare('UPDATE sessions SET updated_at = ? WHERE id = ?')
            .run(Date.now(), input.sessionId);
        return message;
    }
    getMessage(id) {
        const row = this.db.prepare('SELECT * FROM messages WHERE id = ?').get(id);
        return row ? this.rowToMessage(row) : null;
    }
    getSessionMessages(sessionId, options) {
        const { limit = 1000, offset = 0, orderBy = 'timestamp', order = 'ASC' } = options || {};
        const rows = this.db
            .prepare(`SELECT * FROM messages WHERE session_id = ? ORDER BY ${orderBy} ${order} LIMIT ? OFFSET ?`)
            .all(sessionId, limit, offset);
        return rows.map((row) => this.rowToMessage(row));
    }
    deleteMessage(id) {
        const result = this.db.prepare('DELETE FROM messages WHERE id = ?').run(id);
        return result.changes > 0;
    }
    deleteSessionMessages(sessionId) {
        const result = this.db.prepare('DELETE FROM messages WHERE session_id = ?').run(sessionId);
        return result.changes;
    }
    // ==================== Checkpoint CRUD ====================
    createCheckpoint(input) {
        const checkpoint = {
            id: randomUUID(),
            sessionId: input.sessionId,
            gitHash: input.gitHash,
            dialogStateHash: input.dialogStateHash,
            vectorStateHash: input.vectorStateHash,
            createdAt: Date.now(),
            description: input.description,
        };
        this.db
            .prepare(`INSERT INTO checkpoints (id, session_id, git_hash, dialog_state_hash, vector_state_hash, created_at, description)
         VALUES (?, ?, ?, ?, ?, ?, ?)`)
            .run(checkpoint.id, checkpoint.sessionId, checkpoint.gitHash, checkpoint.dialogStateHash, checkpoint.vectorStateHash, checkpoint.createdAt, checkpoint.description);
        return checkpoint;
    }
    getCheckpoint(id) {
        const row = this.db.prepare('SELECT * FROM checkpoints WHERE id = ?').get(id);
        return row ? this.rowToCheckpoint(row) : null;
    }
    getSessionCheckpoints(sessionId, options) {
        const { limit = 100, offset = 0, orderBy = 'created_at', order = 'DESC' } = options || {};
        const rows = this.db
            .prepare(`SELECT * FROM checkpoints WHERE session_id = ? ORDER BY ${orderBy} ${order} LIMIT ? OFFSET ?`)
            .all(sessionId, limit, offset);
        return rows.map((row) => this.rowToCheckpoint(row));
    }
    deleteCheckpoint(id) {
        const result = this.db.prepare('DELETE FROM checkpoints WHERE id = ?').run(id);
        return result.changes > 0;
    }
    // ==================== Utility ====================
    getSessionWithMessages(id) {
        const session = this.getSession(id);
        if (!session)
            return null;
        const messages = this.getSessionMessages(id);
        return { ...session, messages };
    }
    close() {
        this.db.close();
    }
    // ==================== Private Helpers ====================
    rowToSession(row) {
        return {
            id: row.id,
            title: row.title,
            createdAt: row.created_at,
            updatedAt: row.updated_at,
            model: row.model || undefined,
            config: row.config || undefined,
        };
    }
    rowToMessage(row) {
        return {
            id: row.id,
            sessionId: row.session_id,
            role: row.role,
            content: row.content,
            timestamp: row.timestamp,
            model: row.model || undefined,
            tokenCount: row.token_count || undefined,
            parentId: row.parent_id || undefined,
        };
    }
    rowToCheckpoint(row) {
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
//# sourceMappingURL=SessionStorage.js.map