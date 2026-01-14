/**
 * Memory 1 向量存储类型定义
 * 支持 Chroma 向量数据库集成
 */

/**
 * 向量存储配置
 */
export interface VectorStoreConfig {
  collectionName: string;
  embeddingModel?: string;
  chunkSize?: number;
  chunkOverlap?: number;
  host?: string;
  port?: number;
}

/**
 * 文档块
 */
export interface DocumentChunk {
  id: string;
  content: string;
  embedding?: number[];
  metadata: ChunkMetadata;
}

/**
 * 块元数据
 */
export interface ChunkMetadata {
  sessionId: string;
  agentRole: string;
  gitCommitHash?: string;
  messageIndex: number;
  chunkIndex: number;
  timestamp: number;
  source: 'user' | 'assistant' | 'system';
}

/**
 * 向量搜索结果
 */
export interface VectorSearchResult {
  chunk: DocumentChunk;
  score: number;
  distance: number;
}

/**
 * 向量搜索选项
 */
export interface VectorSearchOptions {
  topK?: number;
  minScore?: number;
  filter?: Partial<ChunkMetadata>;
  includeEmbeddings?: boolean;
}

/**
 * Embedding 提供者接口
 */
export interface IEmbeddingProvider {
  embed(text: string): Promise<number[]>;
  embedBatch(texts: string[]): Promise<number[][]>;
  getDimension(): number;
}

/**
 * 向量存储接口
 */
export interface IVectorStore {
  // 基础操作
  add(chunks: DocumentChunk[]): Promise<void>;
  search(query: string, options?: VectorSearchOptions): Promise<VectorSearchResult[]>;
  delete(ids: string[]): Promise<void>;
  clear(): Promise<void>;

  // 元数据操作
  getBySessionId(sessionId: string): Promise<DocumentChunk[]>;
  getByGitCommit(commitHash: string): Promise<DocumentChunk[]>;

  // 统计
  count(): Promise<number>;
  getCollectionInfo(): Promise<CollectionInfo>;
}

/**
 * 集合信息
 */
export interface CollectionInfo {
  name: string;
  count: number;
  dimension: number;
  metadata?: Record<string, unknown>;
}

/**
 * 流式写入配置
 */
export interface StreamWriteConfig {
  batchSize: number;
  flushInterval: number;
  onFlush?: (chunks: DocumentChunk[]) => void;
  onError?: (error: Error) => void;
}

/**
 * 默认配置
 */
export const DEFAULT_VECTOR_CONFIG: VectorStoreConfig = {
  collectionName: 'codeflow_memory',
  chunkSize: 500,
  chunkOverlap: 50,
  host: 'localhost',
  port: 8000,
};

export const DEFAULT_STREAM_CONFIG: StreamWriteConfig = {
  batchSize: 10,
  flushInterval: 5000,
};
