/**
 * 语义检索器实现
 * 支持向量搜索、关键词搜索和混合搜索
 */
import { IVectorStore, VectorSearchResult, ChunkMetadata } from '../memory/types.js';
import { ISemanticRetriever, HybridSearchConfig, KeywordSearchResult, HybridSearchResult, SearchHistoricalContextParams, SearchHistoricalContextResult } from './types.js';
export declare class SemanticRetriever implements ISemanticRetriever {
    private vectorStore;
    private config;
    private keywordIndex;
    private contentCache;
    constructor(vectorStore: IVectorStore, config?: Partial<HybridSearchConfig>);
    searchHistoricalContext(params: SearchHistoricalContextParams): Promise<SearchHistoricalContextResult>;
    vectorSearch(query: string, options?: Partial<HybridSearchConfig>): Promise<VectorSearchResult[]>;
    keywordSearch(query: string, options?: Partial<HybridSearchConfig>): Promise<KeywordSearchResult[]>;
    hybridSearch(query: string, options?: Partial<HybridSearchConfig>): Promise<HybridSearchResult[]>;
    indexContent(id: string, content: string, metadata: ChunkMetadata): void;
    clearIndex(): void;
    private tokenize;
    private calculateTF;
    private createHighlight;
    private applyFilters;
    private rerank;
}
//# sourceMappingURL=SemanticRetriever.d.ts.map