/**
 * Chroma 向量数据库适配器
 * 支持真实 Chroma 服务器连接与加密向量存储
 */

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
 * Chroma 连接配置
 */
export interface ChromaConfig extends VectorStoreConfig {
  host: string;
  port: number;
  ssl?: boolean;
  authToken?: string;
  tenant?: string;
  database?: string;
}

/**
 * Chroma API 响应类型
 */
interface ChromaCollection {
  name: string;
  metadata?: Record<string, unknown>;
}

interface ChromaQueryResult {
  ids: string[][];
  embeddings?: number[][][];
  documents?: (string | null)[][];
  metadatas?: (Record<string, unknown> | null)[][];
  distances?: number[][];
}

interface ChromaGetResult {
  ids: string[];
  embeddings?: number[][];
  documents?: (string | null)[];
  metadatas?: (Record<string, unknown> | null)[];
}

/**
 * Chroma 向量存储实现
 */
export class ChromaVectorStore implements IVectorStore {
  private config: ChromaConfig;
  private embeddingProvider: IEmbeddingProvider;
  private baseUrl: string;
  private collectionId: string | null = null;
  private connected: boolean = false;

  constructor(config?: Partial<ChromaConfig>, embeddingProvider?: IEmbeddingProvider) {
    this.config = {
      ...DEFAULT_VECTOR_CONFIG,
      host: 'localhost',
      port: 8000,
      ssl: false,
      ...config,
    } as ChromaConfig;

    this.embeddingProvider = embeddingProvider || new SimpleEmbeddingProvider();
    const protocol = this.config.ssl ? 'https' : 'http';
    this.baseUrl = `${protocol}://${this.config.host}:${this.config.port}`;
  }

  /**
   * 连接到 Chroma 服务器并获取/创建集合
   */
  async connect(): Promise<void> {
    try {
      // 检查服务器健康状态
      const healthResponse = await this.fetch('/api/v1/heartbeat');
      if (!healthResponse.ok) {
        throw new Error(`Chroma server not healthy: ${healthResponse.status}`);
      }

      // 获取或创建集合
      await this.getOrCreateCollection();
      this.connected = true;
    } catch (error) {
      this.connected = false;
      throw new Error(`Failed to connect to Chroma: ${(error as Error).message}`);
    }
  }

  /**
   * 断开连接
   */
  async disconnect(): Promise<void> {
    this.connected = false;
    this.collectionId = null;
  }

  /**
   * 添加文档块
   */
  async add(chunks: DocumentChunk[]): Promise<void> {
    this.ensureConnected();

    if (chunks.length === 0) return;

    const ids = chunks.map((c) => c.id);
    const documents = chunks.map((c) => c.content);
    const metadatas = chunks.map((c) => this.serializeMetadata(c.metadata));

    // 生成嵌入向量
    const embeddings = await this.embeddingProvider.embedBatch(documents);

    await this.fetch(`/api/v1/collections/${this.collectionId}/add`, {
      method: 'POST',
      body: JSON.stringify({
        ids,
        embeddings,
        documents,
        metadatas,
      }),
    });
  }

  /**
   * 搜索相似文档
   */
  async search(query: string, options?: VectorSearchOptions): Promise<VectorSearchResult[]> {
    this.ensureConnected();

    const topK = options?.topK ?? 10;
    const queryEmbedding = await this.embeddingProvider.embed(query);

    const whereFilter = options?.filter ? this.buildWhereFilter(options.filter) : undefined;

    const response = await this.fetch(`/api/v1/collections/${this.collectionId}/query`, {
      method: 'POST',
      body: JSON.stringify({
        query_embeddings: [queryEmbedding],
        n_results: topK,
        include: ['documents', 'metadatas', 'distances', ...(options?.includeEmbeddings ? ['embeddings'] : [])],
        where: whereFilter,
      }),
    });

    const result: ChromaQueryResult = await response.json();

    if (!result.ids || result.ids.length === 0 || result.ids[0].length === 0) {
      return [];
    }

    const results: VectorSearchResult[] = [];
    const ids = result.ids[0];
    const documents = result.documents?.[0] || [];
    const metadatas = result.metadatas?.[0] || [];
    const distances = result.distances?.[0] || [];
    const embeddings = result.embeddings?.[0];

    for (let i = 0; i < ids.length; i++) {
      const distance = distances[i] || 0;
      const score = 1 / (1 + distance); // 转换距离为相似度分数

      if (options?.minScore && score < options.minScore) continue;

      const chunk: DocumentChunk = {
        id: ids[i],
        content: documents[i] || '',
        metadata: this.deserializeMetadata(metadatas[i]),
        ...(options?.includeEmbeddings && embeddings ? { embedding: embeddings[i] } : {}),
      };

      results.push({ chunk, score, distance });
    }

    return results;
  }

  /**
   * 删除文档
   */
  async delete(ids: string[]): Promise<void> {
    this.ensureConnected();

    if (ids.length === 0) return;

    await this.fetch(`/api/v1/collections/${this.collectionId}/delete`, {
      method: 'POST',
      body: JSON.stringify({ ids }),
    });
  }

  /**
   * 清空集合
   */
  async clear(): Promise<void> {
    this.ensureConnected();

    // Chroma 没有直接的 clear API，需要删除并重建集合
    await this.fetch(`/api/v1/collections/${this.collectionId}`, {
      method: 'DELETE',
    });

    await this.getOrCreateCollection();
  }

  /**
   * 按会话 ID 获取文档
   */
  async getBySessionId(sessionId: string): Promise<DocumentChunk[]> {
    return this.getByFilter({ sessionId });
  }

  /**
   * 按 Git 提交获取文档
   */
  async getByGitCommit(commitHash: string): Promise<DocumentChunk[]> {
    return this.getByFilter({ gitCommitHash: commitHash });
  }

  /**
   * 获取文档数量
   */
  async count(): Promise<number> {
    this.ensureConnected();

    const response = await this.fetch(`/api/v1/collections/${this.collectionId}/count`);
    return response.json();
  }

  /**
   * 获取集合信息
   */
  async getCollectionInfo(): Promise<CollectionInfo> {
    this.ensureConnected();

    const countValue = await this.count();

    return {
      name: this.config.collectionName,
      count: countValue,
      dimension: this.embeddingProvider.getDimension(),
      metadata: {
        host: this.config.host,
        port: this.config.port,
        connected: this.connected,
      },
    };
  }

  /**
   * 检查连接状态
   */
  isConnected(): boolean {
    return this.connected;
  }

  // ==================== 私有方法 ====================

  private async getOrCreateCollection(): Promise<void> {
    // 尝试获取现有集合
    const listResponse = await this.fetch('/api/v1/collections');
    const collections: ChromaCollection[] = await listResponse.json();

    const existing = collections.find((c) => c.name === this.config.collectionName);

    if (existing) {
      // 获取集合 ID
      const getResponse = await this.fetch(`/api/v1/collections/${this.config.collectionName}`);
      const collection = await getResponse.json();
      this.collectionId = collection.id;
    } else {
      // 创建新集合
      const createResponse = await this.fetch('/api/v1/collections', {
        method: 'POST',
        body: JSON.stringify({
          name: this.config.collectionName,
          metadata: { dimension: this.embeddingProvider.getDimension() },
        }),
      });
      const collection = await createResponse.json();
      this.collectionId = collection.id;
    }
  }

  private async getByFilter(filter: Partial<ChunkMetadata>): Promise<DocumentChunk[]> {
    this.ensureConnected();

    const whereFilter = this.buildWhereFilter(filter);

    const response = await this.fetch(`/api/v1/collections/${this.collectionId}/get`, {
      method: 'POST',
      body: JSON.stringify({
        where: whereFilter,
        include: ['documents', 'metadatas'],
      }),
    });

    const result: ChromaGetResult = await response.json();

    if (!result.ids || result.ids.length === 0) {
      return [];
    }

    return result.ids.map((id, i) => ({
      id,
      content: result.documents?.[i] || '',
      metadata: this.deserializeMetadata(result.metadatas?.[i]),
    }));
  }

  private buildWhereFilter(filter: Partial<ChunkMetadata>): Record<string, unknown> | undefined {
    const conditions: Record<string, unknown>[] = [];

    for (const [key, value] of Object.entries(filter)) {
      if (value !== undefined) {
        conditions.push({ [key]: { $eq: value } });
      }
    }

    if (conditions.length === 0) return undefined;
    if (conditions.length === 1) return conditions[0];
    return { $and: conditions };
  }

  private serializeMetadata(metadata: ChunkMetadata): Record<string, unknown> {
    return {
      sessionId: metadata.sessionId,
      agentRole: metadata.agentRole,
      gitCommitHash: metadata.gitCommitHash || '',
      messageIndex: metadata.messageIndex,
      chunkIndex: metadata.chunkIndex,
      timestamp: metadata.timestamp,
      source: metadata.source,
    };
  }

  private deserializeMetadata(raw: Record<string, unknown> | null | undefined): ChunkMetadata {
    if (!raw) {
      return {
        sessionId: '',
        agentRole: '',
        messageIndex: 0,
        chunkIndex: 0,
        timestamp: Date.now(),
        source: 'system',
      };
    }

    return {
      sessionId: String(raw.sessionId || ''),
      agentRole: String(raw.agentRole || ''),
      gitCommitHash: raw.gitCommitHash ? String(raw.gitCommitHash) : undefined,
      messageIndex: Number(raw.messageIndex || 0),
      chunkIndex: Number(raw.chunkIndex || 0),
      timestamp: Number(raw.timestamp || Date.now()),
      source: (raw.source as 'user' | 'assistant' | 'system') || 'system',
    };
  }

  private ensureConnected(): void {
    if (!this.connected || !this.collectionId) {
      throw new Error('Not connected to Chroma. Call connect() first.');
    }
  }

  private async fetch(path: string, options?: RequestInit): Promise<Response> {
    const url = `${this.baseUrl}${path}`;
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
    };

    if (this.config.authToken) {
      headers['Authorization'] = `Bearer ${this.config.authToken}`;
    }

    const response = await fetch(url, {
      ...options,
      headers: { ...headers, ...options?.headers },
    });

    if (!response.ok) {
      const errorText = await response.text().catch(() => 'Unknown error');
      throw new Error(`Chroma API error: ${response.status} - ${errorText}`);
    }

    return response;
  }
}

/**
 * 创建 Chroma 向量存储实例并连接
 */
export async function createChromaStore(
  config?: Partial<ChromaConfig>,
  embeddingProvider?: IEmbeddingProvider
): Promise<ChromaVectorStore> {
  const store = new ChromaVectorStore(config, embeddingProvider);
  await store.connect();
  return store;
}
