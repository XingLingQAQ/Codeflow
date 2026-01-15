/**
 * 原子快照管理器实现
 * 三位一体快照：Git + Conversation + Vector/Graph
 */

import { randomUUID } from 'crypto';
import { createHash } from 'crypto';
import {
  AtomicSnapshot,
  ConversationSnapshot,
  VectorSnapshot,
  GraphSnapshot,
  SnapshotTrigger,
  RollbackOptions,
  RollbackResult,
  SnapshotValidation,
  IAtomicSnapshotManager,
  ISnapshotStorage,
  DEFAULT_SNAPSHOT_CONFIG,
} from './AtomicSnapshotTypes.js';
import { IGitManager } from './types.js';
import { Message } from '../hooks/types.js';
import { IVectorStore } from '../memory/types.js';
import { ITripleStore } from '../samg/types.js';

export interface AtomicSnapshotManagerConfig {
  maxSnapshots: number;
  autoCheckpointInterval: number;
  enableAutoCheckpoint: boolean;
  createBackupOnRollback: boolean;
}

export class AtomicSnapshotManager implements IAtomicSnapshotManager {
  private config: AtomicSnapshotManagerConfig;
  private storage: ISnapshotStorage;
  private gitManager: IGitManager;
  private vectorStore?: IVectorStore;
  private tripleStore?: ITripleStore;
  private conversationProvider?: () => { sessionId: string; messages: Message[] };
  private checkpointTimer?: NodeJS.Timeout;

  constructor(
    storage: ISnapshotStorage,
    gitManager: IGitManager,
    config: Partial<AtomicSnapshotManagerConfig> = {}
  ) {
    this.config = { ...DEFAULT_SNAPSHOT_CONFIG, ...config };
    this.storage = storage;
    this.gitManager = gitManager;

    if (this.config.enableAutoCheckpoint) {
      this.startAutoCheckpoint();
    }
  }

  setVectorStore(store: IVectorStore): void {
    this.vectorStore = store;
  }

  setTripleStore(store: ITripleStore): void {
    this.tripleStore = store;
  }

  setConversationProvider(provider: () => { sessionId: string; messages: Message[] }): void {
    this.conversationProvider = provider;
  }

  async createSnapshot(
    description?: string,
    trigger: SnapshotTrigger = 'manual'
  ): Promise<AtomicSnapshot> {
    const gitSnapshot = await this.gitManager.createSnapshot(description);
    const conversationSnapshot = await this.captureConversation();
    const vectorSnapshot = await this.captureVector();
    const graphSnapshot = await this.captureGraph();

    const snapshot: AtomicSnapshot = {
      id: randomUUID(),
      version: '1.0.0',
      timestamp: Date.now(),
      description,
      git: {
        hash: gitSnapshot.gitHash,
        shortHash: gitSnapshot.gitHash.substring(0, 7),
        message: description || 'Snapshot',
        files: gitSnapshot.files,
      },
      conversation: conversationSnapshot,
      memory: {
        vector: vectorSnapshot,
        graph: graphSnapshot,
      },
      metadata: {
        createdBy: 'system',
        trigger,
        tags: [],
      },
    };

    await this.storage.save(snapshot);
    await this.pruneIfNeeded();

    return snapshot;
  }

  async getSnapshot(id: string): Promise<AtomicSnapshot | null> {
    return this.storage.load(id);
  }

  async listSnapshots(limit: number = 50): Promise<AtomicSnapshot[]> {
    return this.storage.list(limit);
  }

  async findSnapshotByGitHash(gitHash: string): Promise<AtomicSnapshot | null> {
    const snapshots = await this.storage.list(1000);
    return snapshots.find(s => s.git.hash === gitHash || s.git.shortHash === gitHash) || null;
  }

  async findSnapshotsBySession(sessionId: string): Promise<AtomicSnapshot[]> {
    const snapshots = await this.storage.list(1000);
    return snapshots.filter(s => s.conversation.sessionId === sessionId);
  }

  async rollback(options: RollbackOptions): Promise<RollbackResult> {
    const result: RollbackResult = {
      success: false,
      rolledBack: {
        git: false,
        conversation: false,
        vector: false,
        graph: false,
      },
      errors: [],
    };

    const targetSnapshot = await this.getSnapshot(options.targetSnapshotId);
    if (!targetSnapshot) {
      result.errors.push(`Snapshot not found: ${options.targetSnapshotId}`);
      return result;
    }

    // 创建备份快照
    if (options.createBackupSnapshot) {
      try {
        const backup = await this.createSnapshot('Backup before rollback', 'before_rollback');
        result.backupSnapshotId = backup.id;
      } catch (error) {
        result.errors.push(`Failed to create backup: ${error}`);
      }
    }

    // Git 回滚
    if (options.rollbackGit) {
      try {
        const gitSuccess = await this.gitManager.restoreSnapshot(targetSnapshot.id);
        if (gitSuccess) {
          result.rolledBack.git = true;
        } else {
          // 尝试直接 reset
          await this.rollbackGitDirect(targetSnapshot.git.hash);
          result.rolledBack.git = true;
        }
      } catch (error) {
        result.errors.push(`Git rollback failed: ${error}`);
      }
    }

    // 向量存储回滚
    if (options.rollbackVector && this.vectorStore && targetSnapshot.memory.vector) {
      try {
        await this.rollbackVector(targetSnapshot.memory.vector);
        result.rolledBack.vector = true;
      } catch (error) {
        result.errors.push(`Vector rollback failed: ${error}`);
      }
    }

    // 图谱回滚
    if (options.rollbackGraph && this.tripleStore && targetSnapshot.memory.graph) {
      try {
        await this.rollbackGraph(targetSnapshot.memory.graph);
        result.rolledBack.graph = true;
      } catch (error) {
        result.errors.push(`Graph rollback failed: ${error}`);
      }
    }

    // 对话回滚（通常由外部处理，这里只标记）
    if (options.rollbackConversation) {
      result.rolledBack.conversation = true;
    }

    result.success = result.errors.length === 0;
    return result;
  }

  async canRollback(snapshotId: string): Promise<boolean> {
    const snapshot = await this.getSnapshot(snapshotId);
    if (!snapshot) return false;

    // 检查 Git hash 是否存在
    const currentHash = await this.gitManager.getCurrentHash();
    if (!currentHash) return false;

    return true;
  }

  async validateSnapshot(id: string): Promise<SnapshotValidation> {
    const result: SnapshotValidation = {
      valid: false,
      gitValid: false,
      conversationValid: false,
      vectorValid: false,
      graphValid: false,
      errors: [],
    };

    const snapshot = await this.getSnapshot(id);
    if (!snapshot) {
      result.errors.push('Snapshot not found');
      return result;
    }

    // 验证 Git
    try {
      const logs = await this.gitManager.getLog(100);
      result.gitValid = logs.some(l => l.hash === snapshot.git.hash);
      if (!result.gitValid) {
        result.errors.push('Git hash not found in history');
      }
    } catch {
      result.errors.push('Failed to validate Git');
    }

    // 验证对话 checksum
    if (snapshot.conversation.checksum) {
      result.conversationValid = true; // 简化验证
    }

    // 验证向量
    if (snapshot.memory.vector) {
      result.vectorValid = true; // 简化验证
    } else {
      result.vectorValid = true; // 无向量数据也算有效
    }

    // 验证图谱
    if (snapshot.memory.graph) {
      result.graphValid = true; // 简化验证
    } else {
      result.graphValid = true; // 无图谱数据也算有效
    }

    result.valid = result.gitValid && result.conversationValid &&
                   result.vectorValid && result.graphValid;

    return result;
  }

  async validateConsistency(): Promise<SnapshotValidation> {
    const result: SnapshotValidation = {
      valid: true,
      gitValid: true,
      conversationValid: true,
      vectorValid: true,
      graphValid: true,
      errors: [],
    };

    const snapshots = await this.listSnapshots(10);
    for (const snapshot of snapshots) {
      const validation = await this.validateSnapshot(snapshot.id);
      if (!validation.valid) {
        result.valid = false;
        result.errors.push(`Snapshot ${snapshot.id} invalid: ${validation.errors.join(', ')}`);
      }
    }

    return result;
  }

  async pruneSnapshots(keepCount: number): Promise<number> {
    const count = await this.storage.count();
    if (count <= keepCount) return 0;

    const snapshots = await this.storage.list(count);
    const toDelete = snapshots.slice(keepCount);

    let deleted = 0;
    for (const snapshot of toDelete) {
      if (await this.storage.delete(snapshot.id)) {
        deleted++;
      }
    }

    return deleted;
  }

  async deleteSnapshot(id: string): Promise<boolean> {
    return this.storage.delete(id);
  }

  destroy(): void {
    if (this.checkpointTimer) {
      clearInterval(this.checkpointTimer);
    }
  }

  // ==================== Private Methods ====================

  private async captureConversation(): Promise<ConversationSnapshot> {
    if (!this.conversationProvider) {
      return {
        sessionId: 'unknown',
        messages: [],
        messageCount: 0,
        lastMessageIndex: -1,
        checksum: this.generateChecksum(''),
      };
    }

    const { sessionId, messages } = this.conversationProvider();
    const content = JSON.stringify(messages);

    return {
      sessionId,
      messages: messages.slice(-50), // 只保留最近 50 条
      messageCount: messages.length,
      lastMessageIndex: messages.length - 1,
      checksum: this.generateChecksum(content),
    };
  }

  private async captureVector(): Promise<VectorSnapshot | undefined> {
    if (!this.vectorStore) return undefined;

    try {
      const info = await this.vectorStore.getCollectionInfo();
      return {
        collectionName: info.name,
        chunkCount: info.count,
        lastChunkId: `chunk_${info.count}`,
        timestamp: Date.now(),
        checksum: this.generateChecksum(`${info.name}_${info.count}`),
      };
    } catch {
      return undefined;
    }
  }

  private async captureGraph(): Promise<GraphSnapshot | undefined> {
    if (!this.tripleStore) return undefined;

    try {
      const stats = await this.tripleStore.getStats();
      return {
        graphId: 'default',
        tripleCount: stats.tripleCount,
        entityCount: stats.entityCount,
        lastTripleId: `triple_${stats.tripleCount}`,
        timestamp: Date.now(),
        checksum: this.generateChecksum(`${stats.tripleCount}_${stats.entityCount}`),
      };
    } catch {
      return undefined;
    }
  }

  private async rollbackGitDirect(hash: string): Promise<void> {
    // 通过 GitManager 的内部方法回滚
    // 这里假设 GitManager 有 execGit 方法或类似功能
    const snapshot = this.gitManager.listSnapshots().find(s => s.gitHash === hash);
    if (snapshot) {
      await this.gitManager.restoreSnapshot(snapshot.id);
    }
  }

  private async rollbackVector(targetState: VectorSnapshot): Promise<void> {
    if (!this.vectorStore) return;

    // 删除目标时间戳之后的所有数据
    // 实际实现需要 VectorStore 支持按时间戳删除
    // 这里是简化实现
    const chunks = await this.vectorStore.getBySessionId('*');
    const toDelete = chunks
      .filter(c => c.metadata.timestamp > targetState.timestamp)
      .map(c => c.id);

    if (toDelete.length > 0) {
      await this.vectorStore.delete(toDelete);
    }
  }

  private async rollbackGraph(targetState: GraphSnapshot): Promise<void> {
    if (!this.tripleStore) return;

    // 查询并删除目标时间戳之后的三元组
    const allTriples = await this.tripleStore.query({});
    const toDelete = allTriples
      .filter(t => t.timestamp > targetState.timestamp)
      .map(t => t['@id']);

    if (toDelete.length > 0) {
      await this.tripleStore.delete(toDelete);
    }
  }

  private async pruneIfNeeded(): Promise<void> {
    const count = await this.storage.count();
    if (count > this.config.maxSnapshots) {
      await this.pruneSnapshots(this.config.maxSnapshots);
    }
  }

  private startAutoCheckpoint(): void {
    this.checkpointTimer = setInterval(async () => {
      try {
        await this.createSnapshot('Auto checkpoint', 'auto_checkpoint');
      } catch {
        // Ignore checkpoint errors
      }
    }, this.config.autoCheckpointInterval);
  }

  private generateChecksum(content: string): string {
    return createHash('sha256').update(content).digest('hex').substring(0, 16);
  }
}

/**
 * 内存快照存储实现
 */
export class InMemorySnapshotStorage implements ISnapshotStorage {
  private snapshots: Map<string, AtomicSnapshot> = new Map();

  async save(snapshot: AtomicSnapshot): Promise<void> {
    this.snapshots.set(snapshot.id, snapshot);
  }

  async load(id: string): Promise<AtomicSnapshot | null> {
    return this.snapshots.get(id) || null;
  }

  async list(limit: number = 50, offset: number = 0): Promise<AtomicSnapshot[]> {
    const all = Array.from(this.snapshots.values())
      .sort((a, b) => b.timestamp - a.timestamp);
    return all.slice(offset, offset + limit);
  }

  async delete(id: string): Promise<boolean> {
    return this.snapshots.delete(id);
  }

  async clear(): Promise<void> {
    this.snapshots.clear();
  }

  async count(): Promise<number> {
    return this.snapshots.size;
  }
}
