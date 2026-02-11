/**
 * Chroma 向量数据库适配器
 * 支持真实 Chroma 服务器连接与加密向量存储
 */
import { IVectorStore, IEmbeddingProvider, VectorStoreConfig, DocumentChunk, VectorSearchResult, VectorSearchOptions, CollectionInfo } from './types.js';
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
 * Chroma 向量存储实现
 */
export declare class ChromaVectorStore implements IVectorStore {
    private config;
    private embeddingProvider;
    private baseUrl;
    private collectionId;
    private connected;
    constructor(config?: Partial<ChromaConfig>, embeddingProvider?: IEmbeddingProvider);
    /**
     * 连接到 Chroma 服务器并获取/创建集合
     */
    connect(): Promise<void>;
    /**
     * 断开连接
     */
    disconnect(): Promise<void>;
    /**
     * 添加文档块
     */
    add(chunks: DocumentChunk[]): Promise<void>;
    /**
     * 搜索相似文档
     */
    search(query: string, options?: VectorSearchOptions): Promise<VectorSearchResult[]>;
    /**
     * 删除文档
     */
    delete(ids: string[]): Promise<void>;
    /**
     * 清空集合
     */
    clear(): Promise<void>;
    /**
     * 按会话 ID 获取文档
     */
    getBySessionId(sessionId: string): Promise<DocumentChunk[]>;
    /**
     * 按 Git 提交获取文档
     */
    getByGitCommit(commitHash: string): Promise<DocumentChunk[]>;
    /**
     * 获取文档数量
     */
    count(): Promise<number>;
    /**
     * 获取集合信息
     */
    getCollectionInfo(): Promise<CollectionInfo>;
    /**
     * 检查连接状态
     */
    isConnected(): boolean;
    private getOrCreateCollection;
    private getByFilter;
    private buildWhereFilter;
    private serializeMetadata;
    private deserializeMetadata;
    private ensureConnected;
    private fetch;
}
/**
 * 创建 Chroma 向量存储实例并连接
 */
export declare function createChromaStore(config?: Partial<ChromaConfig>, embeddingProvider?: IEmbeddingProvider): Promise<ChromaVectorStore>;
//# sourceMappingURL=ChromaVectorStore.d.ts.map