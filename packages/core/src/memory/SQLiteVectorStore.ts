/**
 * SQLite 向量存储实现
 * 基于 better-sqlite3 的本地嵌入式向量数据库
 * AI 友好：零配置、纯本地文件、无需 Docker/远程服务
 */

import Database from 'better-sqlite3';
import * as path from 'path';
import * as fs from 'fs';
import {
  IVectorStore,
  IEmbeddingProvider,
  VectorStoreConfig,
  DocumentChunk,
  VectorSearchResult,
  VectorSearchOptions,
  CollectionInfo,
  ChunkMetadata,
  DEFAULT_VECTOR_CONFIG,
} from './types.js';
import { SimpleEmbeddingProvider } from './SimpleEmbeddingProvider.js';

/**
 * SQLite 向量存储配置
 */
export interface SQLiteVectorConfig extends VectorStoreConfig {
  dbPath: string;
  walMode?: boolean;
}

/**
 * 默认 SQLite 配置
 */
export const DEFAULT_SQLITE_CONFIG: SQLiteVectorConfig = {
  ...DEFAULT_VECTOR_CONFIG,
  dbPath: './data/vectors.db',
  walMode: true,
};

/**
 * SQLite 向量存储实现
 */
export class SQLiteVectorStore implements IVectorStore {
  private db: Database.Database | null = null;
  private config: SQLiteVectorConfig;
  private embeddingProvider: IEmbeddingProvider;
  private initialized: boolean = false;

  constructor(config?: Partial<SQLiteVectorConfig>, embeddingProvider?: IEmbeddingProvider) {
    this.config = { ...DEFAULT_SQLITE_CONFIG, ...config };
    this.embeddingProvider = embeddingProvider || new SimpleEmbeddingProvider();
  }

  /**
   * 初始化数据库
   */
  async initialize(): Promise<void> {
    if (this.initialized) return;

    // 确保目录存在
    const dbDir = path.dirname(this.config.dbPath);
    if (!fs.existsSync(dbDir)) {
      fs.mkdirSync(dbDir, { recursive: true });
    }

    // 打开数据库
    this.db = new Database(this.config.dbPath);

    // 启用 WAL 模式（更好的并发性能）
    if (this.config.walMode) {
      this.db.pragma('journal_mode = WAL');
    }

    // 创建表结构
    this.createTables();

    this.initialized = true;
  }

  /**
   * 添加文档块
   */
  async add(chunks: DocumentChunk[]): Promise<void> {
    await this.ensureInitialized();

    if (chunks.length === 0) return;

    const texts = chunks.map((c) => c.content);
    const embeddings = await this.embeddingProvider.embedBatch(texts);

    const insert = this.db!.prepare(`
      INSERT OR REPLACE INTO vectors (
        id, content, embedding, session_id, agent_role, git_commit_hash,
        message_index, chunk_index, timestamp, source, collection_name
      ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `);

    const insertMany = this.db!.transaction((items: Array<{ chunk: DocumentChunk; embedding: number[] }>) => {
      for (const { chunk, embedding } of items) {
        insert.run(
          chunk.id,
          chunk.content,
          this.serializeEmbedding(embedding),
          chunk.metadata.sessionId,
          chunk.metadata.agentRole,
          chunk.metadata.gitCommitHash || null,
          chunk.metadata.messageIndex,
          chunk.metadata.chunkIndex,
          chunk.metadata.timestamp,
          chunk.metadata.source,
          this.config.collectionName
        );
      }
    });

    insertMany(chunks.map((chunk, i) => ({ chunk, embedding: embeddings[i] })));
  }

  /**
   * 搜索相似文档
   */
  async search(query: string, options?: VectorSearchOptions): Promise<VectorSearchResult[]> {
    await this.ensureInitialized();

    const topK = options?.topK ?? 10;
    const minScore = options?.minScore ?? 0;
    const queryEmbedding = await this.embeddingProvider.embed(query);

    // 构建查询条件
    let whereClause = 'WHERE collection_name = ?';
    const params: unknown[] = [this.config.collectionName];

    if (options?.filter) {
      if (options.filter.sessionId) {
        whereClause += ' AND session_id = ?';
        params.push(options.filter.sessionId);
      }
      if (options.filter.agentRole) {
        whereClause += ' AND agent_role = ?';
        params.push(options.filter.agentRole);
      }
      if (options.filter.gitCommitHash) {
        whereClause += ' AND git_commit_hash = ?';
        params.push(options.filter.gitCommitHash);
      }
      if (options.filter.source) {
        whereClause += ' AND source = ?';
        params.push(options.filter.source);
      }
    }

    const rows = this.db!.prepare(`
      SELECT id, content, embedding, session_id, agent_role, git_commit_hash,
             message_index, chunk_index, timestamp, source
      FROM vectors
      ${whereClause}
    `).all(...params) as Array<{
      id: string;
      content: string;
      embedding: string;
      session_id: string;
      agent_role: string;
      git_commit_hash: string | null;
      message_index: number;
      chunk_index: number;
      timestamp: number;
      source: string;
    }>;

    // 计算相似度并排序
    const results: VectorSearchResult[] = [];

    for (const row of rows) {
      const embedding = this.deserializeEmbedding(row.embedding);
      const distance = this.cosineSimilarity(queryEmbedding, embedding);
      const score = (distance + 1) / 2; // 转换为 0-1 范围

      if (score >= minScore) {
        const chunk: DocumentChunk = {
          id: row.id,
          content: row.content,
          metadata: {
            sessionId: row.session_id,
            agentRole: row.agent_role,
            gitCommitHash: row.git_commit_hash || undefined,
            messageIndex: row.message_index,
            chunkIndex: row.chunk_index,
            timestamp: row.timestamp,
            source: row.source as 'user' | 'assistant' | 'system',
          },
          ...(options?.includeEmbeddings ? { embedding } : {}),
        };

        results.push({ chunk, score, distance });
      }
    }

    // 按分数排序并返回 topK
    return results.sort((a, b) => b.score - a.score).slice(0, topK);
  }

  /**
   * 删除文档
   */
  async delete(ids: string[]): Promise<void> {
    await this.ensureInitialized();

    if (ids.length === 0) return;

    const placeholders = ids.map(() => '?').join(',');
    this.db!.prepare(`DELETE FROM vectors WHERE id IN (${placeholders})`).run(...ids);
  }

  /**
   * 清空集合
   */
  async clear(): Promise<void> {
    await this.ensureInitialized();

    this.db!.prepare('DELETE FROM vectors WHERE collection_name = ?').run(this.config.collectionName);
  }

  /**
   * 按会话 ID 获取文档
   */
  async getBySessionId(sessionId: string): Promise<DocumentChunk[]> {
    await this.ensureInitialized();

    const rows = this.db!.prepare(`
      SELECT id, content, embedding, session_id, agent_role, git_commit_hash,
             message_index, chunk_index, timestamp, source
      FROM vectors
      WHERE collection_name = ? AND session_id = ?
      ORDER BY timestamp ASC
    `).all(this.config.collectionName, sessionId) as Array<{
      id: string;
      content: string;
      embedding: string;
      session_id: string;
      agent_role: string;
      git_commit_hash: string | null;
      message_index: number;
      chunk_index: number;
      timestamp: number;
      source: string;
    }>;

    return rows.map((row) => ({
      id: row.id,
      content: row.content,
      metadata: {
        sessionId: row.session_id,
        agentRole: row.agent_role,
        gitCommitHash: row.git_commit_hash || undefined,
        messageIndex: row.message_index,
        chunkIndex: row.chunk_index,
        timestamp: row.timestamp,
        source: row.source as 'user' | 'assistant' | 'system',
      },
    }));
  }

  /**
   * 按 Git 提交获取文档
   */
  async getByGitCommit(commitHash: string): Promise<DocumentChunk[]> {
    await this.ensureInitialized();

    const rows = this.db!.prepare(`
      SELECT id, content, embedding, session_id, agent_role, git_commit_hash,
             message_index, chunk_index, timestamp, source
      FROM vectors
      WHERE collection_name = ? AND git_commit_hash = ?
      ORDER BY timestamp ASC
    `).all(this.config.collectionName, commitHash) as Array<{
      id: string;
      content: string;
      embedding: string;
      session_id: string;
      agent_role: string;
      git_commit_hash: string | null;
      message_index: number;
      chunk_index: number;
      timestamp: number;
      source: string;
    }>;

    return rows.map((row) => ({
      id: row.id,
      content: row.content,
      metadata: {
        sessionId: row.session_id,
        agentRole: row.agent_role,
        gitCommitHash: row.git_commit_hash || undefined,
        messageIndex: row.message_index,
        chunkIndex: row.chunk_index,
        timestamp: row.timestamp,
        source: row.source as 'user' | 'assistant' | 'system',
      },
    }));
  }

  /**
   * 获取文档数量
   */
  async count(): Promise<number> {
    await this.ensureInitialized();

    const result = this.db!.prepare(
      'SELECT COUNT(*) as count FROM vectors WHERE collection_name = ?'
    ).get(this.config.collectionName) as { count: number };

    return result.count;
  }

  /**
   * 获取集合信息
   */
  async getCollectionInfo(): Promise<CollectionInfo> {
    await this.ensureInitialized();

    const countValue = await this.count();

    return {
      name: this.config.collectionName,
      count: countValue,
      dimension: this.embeddingProvider.getDimension(),
      metadata: {
        dbPath: this.config.dbPath,
        walMode: this.config.walMode,
      },
    };
  }

  /**
   * 关闭数据库连接
   */
  close(): void {
    if (this.db) {
      this.db.close();
      this.db = null;
      this.initialized = false;
    }
  }

  /**
   * 获取数据库统计信息
   */
  async getStats(): Promise<{
    totalVectors: number;
    collections: string[];
    dbSizeBytes: number;
  }> {
    await this.ensureInitialized();

    const totalResult = this.db!.prepare('SELECT COUNT(*) as count FROM vectors').get() as { count: number };

    const collectionsResult = this.db!.prepare(
      'SELECT DISTINCT collection_name FROM vectors'
    ).all() as Array<{ collection_name: string }>;

    const dbSizeBytes = fs.existsSync(this.config.dbPath)
      ? fs.statSync(this.config.dbPath).size
      : 0;

    return {
      totalVectors: totalResult.count,
      collections: collectionsResult.map((r) => r.collection_name),
      dbSizeBytes,
    };
  }

  // ==================== 私有方法 ====================

  private async ensureInitialized(): Promise<void> {
    if (!this.initialized) {
      await this.initialize();
    }
  }

  private createTables(): void {
    this.db!.exec(`
      CREATE TABLE IF NOT EXISTS vectors (
        id TEXT PRIMARY KEY,
        content TEXT NOT NULL,
        embedding TEXT NOT NULL,
        session_id TEXT NOT NULL,
        agent_role TEXT NOT NULL,
        git_commit_hash TEXT,
        message_index INTEGER NOT NULL,
        chunk_index INTEGER NOT NULL,
        timestamp INTEGER NOT NULL,
        source TEXT NOT NULL,
        collection_name TEXT NOT NULL,
        created_at INTEGER DEFAULT (strftime('%s', 'now'))
      );

      CREATE INDEX IF NOT EXISTS idx_vectors_collection ON vectors(collection_name);
      CREATE INDEX IF NOT EXISTS idx_vectors_session ON vectors(session_id);
      CREATE INDEX IF NOT EXISTS idx_vectors_git_commit ON vectors(git_commit_hash);
      CREATE INDEX IF NOT EXISTS idx_vectors_timestamp ON vectors(timestamp);
    `);
  }

  private serializeEmbedding(embedding: number[]): string {
    return JSON.stringify(embedding);
  }

  private deserializeEmbedding(data: string): number[] {
    return JSON.parse(data);
  }

  private cosineSimilarity(a: number[], b: number[]): number {
    if (a.length !== b.length) return 0;

    let dotProduct = 0;
    let normA = 0;
    let normB = 0;

    for (let i = 0; i < a.length; i++) {
      dotProduct += a[i] * b[i];
      normA += a[i] * a[i];
      normB += b[i] * b[i];
    }

    const magnitude = Math.sqrt(normA) * Math.sqrt(normB);
    return magnitude === 0 ? 0 : dotProduct / magnitude;
  }
}

/**
 * 创建 SQLite 向量存储实例并初始化
 */
export async function createSQLiteVectorStore(
  config?: Partial<SQLiteVectorConfig>,
  embeddingProvider?: IEmbeddingProvider
): Promise<SQLiteVectorStore> {
  const store = new SQLiteVectorStore(config, embeddingProvider);
  await store.initialize();
  return store;
}
