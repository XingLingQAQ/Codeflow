/**
 * 内存向量存储实现
 * 轻量级本地向量存储，支持 Chroma 兼容接口
 */
import { IVectorStore, IEmbeddingProvider, VectorStoreConfig, DocumentChunk, VectorSearchResult, VectorSearchOptions, CollectionInfo } from './types.js';
export declare class InMemoryVectorStore implements IVectorStore {
    private chunks;
    private embeddings;
    private embeddingProvider;
    private config;
    constructor(config?: Partial<VectorStoreConfig>, embeddingProvider?: IEmbeddingProvider);
    add(chunks: DocumentChunk[]): Promise<void>;
    search(query: string, options?: VectorSearchOptions): Promise<VectorSearchResult[]>;
    delete(ids: string[]): Promise<void>;
    clear(): Promise<void>;
    getBySessionId(sessionId: string): Promise<DocumentChunk[]>;
    getByGitCommit(commitHash: string): Promise<DocumentChunk[]>;
    count(): Promise<number>;
    getCollectionInfo(): Promise<CollectionInfo>;
    private cosineSimilarity;
    private matchesFilter;
}
//# sourceMappingURL=InMemoryVectorStore.d.ts.map