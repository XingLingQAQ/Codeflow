/**
 * SQLite 会话存储类型定义
 */

export interface Session {
  id: string;
  title: string;
  createdAt: number;
  updatedAt: number;
  model?: string;
  config?: string; // JSON 序列化的配置
}

export interface SessionMessage {
  id: string;
  sessionId: string;
  role: 'user' | 'assistant' | 'system';
  content: string;
  timestamp: number;
  model?: string;
  tokenCount?: number;
  parentId?: string; // 支持分支对话
}

export interface Checkpoint {
  id: string;
  sessionId: string;
  gitHash?: string;
  dialogStateHash: string;
  vectorStateHash?: string;
  createdAt: number;
  description?: string;
}

export interface SessionWithMessages extends Session {
  messages: SessionMessage[];
}

export interface CreateSessionInput {
  title?: string;
  model?: string;
  config?: Record<string, unknown>;
}

export interface CreateMessageInput {
  sessionId: string;
  role: 'user' | 'assistant' | 'system';
  content: string;
  model?: string;
  tokenCount?: number;
  parentId?: string;
}

export interface CreateCheckpointInput {
  sessionId: string;
  gitHash?: string;
  dialogStateHash: string;
  vectorStateHash?: string;
  description?: string;
}

export interface QueryOptions {
  limit?: number;
  offset?: number;
  orderBy?: string;
  order?: 'ASC' | 'DESC';
}

export interface ISessionStorage {
  // Session CRUD
  createSession(input: CreateSessionInput): Session;
  getSession(id: string): Session | null;
  getAllSessions(options?: QueryOptions): Session[];
  updateSession(id: string, updates: Partial<Session>): Session | null;
  deleteSession(id: string): boolean;

  // Message CRUD
  createMessage(input: CreateMessageInput): SessionMessage;
  getMessage(id: string): SessionMessage | null;
  getSessionMessages(sessionId: string, options?: QueryOptions): SessionMessage[];
  deleteMessage(id: string): boolean;
  deleteSessionMessages(sessionId: string): number;

  // Checkpoint CRUD
  createCheckpoint(input: CreateCheckpointInput): Checkpoint;
  getCheckpoint(id: string): Checkpoint | null;
  getSessionCheckpoints(sessionId: string, options?: QueryOptions): Checkpoint[];
  deleteCheckpoint(id: string): boolean;

  // Utility
  getSessionWithMessages(id: string): SessionWithMessages | null;
  close(): void;
}
