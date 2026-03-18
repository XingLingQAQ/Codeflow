/**
 * 原子快照类型定义
 * 三位一体快照结构：Git + Conversation + Vector/Graph
 */

import { GitSnapshot, GitCommitInfo } from './types.js';
import { Message } from '../hooks/types.js';

/**
 * 对话状态快照
 */
export interface ConversationSnapshot {
  sessionId: string;
  messages: Message[];
  messageCount: number;
  lastMessageIndex: number;
  checksum: string;
}

/**
 * 向量存储快照
 */
export interface VectorSnapshot {
  collectionName: string;
  chunkCount: number;
  lastChunkId: string;
  timestamp: number;
  checksum: string;
}

/**
 * 图谱快照
 */
export interface GraphSnapshot {
  graphId: string;
  tripleCount: number;
  entityCount: number;
  lastTripleId: string;
  timestamp: number;
  checksum: string;
}

/**
 * 三位一体原子快照
 */
export interface AtomicSnapshot {
  id: string;
  version: string;
  timestamp: number;
  description?: string;

  // Git 层
  git: {
    hash: string;
    shortHash: string;
    message: string;
    files: string[];
  };

  // 对话层
  conversation: ConversationSnapshot;

  // 记忆层（向量 + 图谱）
  memory: {
    vector?: VectorSnapshot;
    graph?: GraphSnapshot;
  };

  // 元数据
  metadata: {
    createdBy: string;
    trigger: SnapshotTrigger;
    parentSnapshotId?: string;
    tags?: string[];
    sessionId?: string;
    taskId?: string;
    agentId?: string;
    codeChangeEventIds?: string[];
  };
}

/**
 * 快照触发类型
 */
export type SnapshotTrigger =
  | 'hook_after_exec'
  | 'manual'
  | 'auto_checkpoint'
  | 'before_rollback'
  | 'session_end';

/**
 * 回滚选项
 */
export interface RollbackOptions {
  targetSnapshotId: string;
  rollbackGit: boolean;
  rollbackConversation: boolean;
  rollbackVector: boolean;
  rollbackGraph: boolean;
  createBackupSnapshot: boolean;
}

/**
 * 回滚结果
 */
export interface RollbackResult {
  success: boolean;
  backupSnapshotId?: string;
  rolledBack: {
    git: boolean;
    conversation: boolean;
    vector: boolean;
    graph: boolean;
  };
  errors: string[];
}

/**
 * 快照验证结果
 */
export interface SnapshotValidation {
  valid: boolean;
  gitValid: boolean;
  conversationValid: boolean;
  vectorValid: boolean;
  graphValid: boolean;
  errors: string[];
}

export interface CreateAtomicSnapshotOptions {
  sessionId?: string;
  taskId?: string;
  agentId?: string;
  codeChangeEventIds?: string[];
}

/**
 * 原子快照管理器接口
 */
export interface IAtomicSnapshotManager {
  // 快照创建
  createSnapshot(
    description?: string,
    trigger?: SnapshotTrigger,
    options?: CreateAtomicSnapshotOptions
  ): Promise<AtomicSnapshot>;

  // 快照查询
  getSnapshot(id: string): Promise<AtomicSnapshot | null>;
  listSnapshots(limit?: number): Promise<AtomicSnapshot[]>;
  findSnapshotByGitHash(gitHash: string): Promise<AtomicSnapshot | null>;
  findSnapshotsBySession(sessionId: string): Promise<AtomicSnapshot[]>;

  // 回滚操作
  rollback(options: RollbackOptions): Promise<RollbackResult>;
  canRollback(snapshotId: string): Promise<boolean>;

  // 验证
  validateSnapshot(id: string): Promise<SnapshotValidation>;
  validateConsistency(): Promise<SnapshotValidation>;

  // 清理
  pruneSnapshots(keepCount: number): Promise<number>;
  deleteSnapshot(id: string): Promise<boolean>;
}

/**
 * 快照存储接口
 */
export interface ISnapshotStorage {
  save(snapshot: AtomicSnapshot): Promise<void>;
  load(id: string): Promise<AtomicSnapshot | null>;
  list(limit?: number, offset?: number): Promise<AtomicSnapshot[]>;
  delete(id: string): Promise<boolean>;
  clear(): Promise<void>;
  count(): Promise<number>;
}

/**
 * 默认快照配置
 */
export const DEFAULT_SNAPSHOT_CONFIG = {
  maxSnapshots: 100,
  autoCheckpointInterval: 300000, // 5 minutes
  enableAutoCheckpoint: true,
  createBackupOnRollback: true,
};
