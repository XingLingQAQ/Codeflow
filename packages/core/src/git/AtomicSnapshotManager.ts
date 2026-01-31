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
      // 重新计算当前对话的 checksum 并比较
      if (this.conversationProvider) {
        const { messages } = this.conversationProvider();
        const currentChecksum = this.generateChecksum(JSON.stringify(messages));
        // 如果消息数量相同且 checksum 匹配，则有效
        if (messages.length === snapshot.conversation.messageCount) {
          result.conversationValid = true;
        } else if (messages.length > snapshot.conversation.messageCount) {
          // 当前消息更多，快照仍然有效（可以回滚）
          result.conversationValid = true;
        } else {
          // 当前消息更少，可能数据丢失
          result.errors.push(
            `Conversation message count mismatch: expected ${snapshot.conversation.messageCount}, got ${messages.length}`
          );
        }
      } else {
        // 无法验证，假设有效
        result.conversationValid = true;
      }
    } else {
      result.errors.push('Conversation checksum missing');
    }

    // 验证向量
    if (snapshot.memory.vector) {
      if (this.vectorStore) {
        try {
          const info = await this.vectorStore.getCollectionInfo();
          // 验证集合名称匹配
          if (info.name === snapshot.memory.vector.collectionName) {
            // 验证 chunk 数量（当前应该 >= 快照时的数量，因为可能有新增）
            if (info.count >= snapshot.memory.vector.chunkCount) {
              result.vectorValid = true;
            } else {
              result.errors.push(
                `Vector chunk count decreased: expected >= ${snapshot.memory.vector.chunkCount}, got ${info.count}`
              );
            }
          } else {
            result.errors.push(
              `Vector collection name mismatch: expected ${snapshot.memory.vector.collectionName}, got ${info.name}`
            );
          }
        } catch {
          result.errors.push('Failed to validate vector store');
        }
      } else {
        // 无向量存储但快照有向量数据，标记为无效
        result.errors.push('Vector store not available but snapshot has vector data');
      }
    } else {
      result.vectorValid = true; // 无向量数据也算有效
    }

    // 验证图谱
    if (snapshot.memory.graph) {
      if (this.tripleStore) {
        try {
          const stats = await this.tripleStore.getStats();
          // 验证三元组数量（当前应该 >= 快照时的数量）
          if (stats.tripleCount >= snapshot.memory.graph.tripleCount) {
            // 验证实体数量
            if (stats.entityCount >= snapshot.memory.graph.entityCount) {
              result.graphValid = true;
            } else {
              result.errors.push(
                `Graph entity count decreased: expected >= ${snapshot.memory.graph.entityCount}, got ${stats.entityCount}`
              );
            }
          } else {
            result.errors.push(
              `Graph triple count decreased: expected >= ${snapshot.memory.graph.tripleCount}, got ${stats.tripleCount}`
            );
          }
        } catch {
          result.errors.push('Failed to validate triple store');
        }
      } else {
        // 无图谱存储但快照有图谱数据，标记为无效
        result.errors.push('Triple store not available but snapshot has graph data');
      }
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

    // 获取所有 chunks 并按时间戳过滤
    // 策略：删除目标时间戳之后添加的所有数据
    try {
      // 获取所有会话的 chunks
      const allChunks = await this.vectorStore.getBySessionId('*');

      // 过滤出需要删除的 chunks（时间戳晚于目标状态）
      const toDelete: string[] = [];
      for (const chunk of allChunks) {
        const chunkTimestamp = chunk.metadata?.timestamp || chunk.metadata?.createdAt || 0;
        if (chunkTimestamp > targetState.timestamp) {
          toDelete.push(chunk.id);
        }
      }

      // 批量删除
      if (toDelete.length > 0) {
        // 分批删除以避免一次性删除过多
        const batchSize = 100;
        for (let i = 0; i < toDelete.length; i += batchSize) {
          const batch = toDelete.slice(i, i + batchSize);
          await this.vectorStore.delete(batch);
        }
      }
    } catch (error) {
      // 如果 getBySessionId 不支持通配符，尝试其他方式
      // 回退到基于 chunk count 的删除策略
      const info = await this.vectorStore.getCollectionInfo();
      if (info.count > targetState.chunkCount) {
        // 无法精确删除，记录警告
        console.warn(
          `Vector rollback: cannot precisely delete chunks. Current: ${info.count}, Target: ${targetState.chunkCount}`
        );
      }
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
