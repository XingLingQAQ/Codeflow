import { ISessionStorage, Session, SessionMessage, Checkpoint, SessionWithMessages, CreateSessionInput, CreateMessageInput, CreateCheckpointInput, QueryOptions } from './types.js';
/**
 * SQLite 会话存储实现
 * 基于 better-sqlite3，支持外键约束和事务
 */
export declare class SessionStorage implements ISessionStorage {
    private db;
    constructor(dbPath?: string);
    private initialize;
    createSession(input: CreateSessionInput): Session;
    getSession(id: string): Session | null;
    getAllSessions(options?: QueryOptions): Session[];
    updateSession(id: string, updates: Partial<Session>): Session | null;
    deleteSession(id: string): boolean;
    createMessage(input: CreateMessageInput): SessionMessage;
    getMessage(id: string): SessionMessage | null;
    getSessionMessages(sessionId: string, options?: QueryOptions): SessionMessage[];
    deleteMessage(id: string): boolean;
    deleteSessionMessages(sessionId: string): number;
    createCheckpoint(input: CreateCheckpointInput): Checkpoint;
    getCheckpoint(id: string): Checkpoint | null;
    getSessionCheckpoints(sessionId: string, options?: QueryOptions): Checkpoint[];
    deleteCheckpoint(id: string): boolean;
    getSessionWithMessages(id: string): SessionWithMessages | null;
    close(): void;
    private rowToSession;
    private rowToMessage;
    private rowToCheckpoint;
}
//# sourceMappingURL=SessionStorage.d.ts.map