/**
 * SQLite 向量存储实现
 * 基于 better-sqlite3 的本地嵌入式向量数据库
 * AI 友好：零配置、纯本地文件、无需 Docker/远程服务
 */
import { IVectorStore, IEmbeddingProvider, VectorStoreConfig, DocumentChunk, VectorSearchResult, VectorSearchOptions, CollectionInfo } from './types.js';
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
export declare const DEFAULT_SQLITE_CONFIG: SQLiteVectorConfig;
/**
 * SQLite 向量存储实现
 */
export declare class SQLiteVectorStore implements IVectorStore {
    private db;
    private config;
    private embeddingProvider;
    private initialized;
    constructor(config?: Partial<SQLiteVectorConfig>, embeddingProvider?: IEmbeddingProvider);
    /**
     * 初始化数据库
     */
    initialize(): Promise<void>;
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
     * 关闭数据库连接
     */
    close(): void;
    /**
     * 获取数据库统计信息
     */
    getStats(): Promise<{
        totalVectors: number;
        collections: string[];
        dbSizeBytes: number;
    }>;
    private ensureInitialized;
    private createTables;
    private serializeEmbedding;
    private deserializeEmbedding;
    private cosineSimilarity;
}
/**
 * 创建 SQLite 向量存储实例并初始化
 */
export declare function createSQLiteVectorStore(config?: Partial<SQLiteVectorConfig>, embeddingProvider?: IEmbeddingProvider): Promise<SQLiteVectorStore>;
//# sourceMappingURL=SQLiteVectorStore.d.ts.map