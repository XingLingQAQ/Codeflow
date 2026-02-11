/**
 * 语义检索工具类型定义
 * search_historical_context 工具实现
 */
import { MemoryMatch } from '../hooks/types.js';
import { VectorSearchResult, ChunkMetadata } from '../memory/types.js';
/**
 * 混合搜索配置
 */
export interface HybridSearchConfig {
    vectorWeight: number;
    keywordWeight: number;
    topK: number;
    minScore: number;
    reranking: boolean;
}
/**
 * 关键词搜索结果
 */
export interface KeywordSearchResult {
    content: string;
    score: number;
    metadata: ChunkMetadata;
    highlights: string[];
}
/**
 * 混合搜索结果
 */
export interface HybridSearchResult {
    content: string;
    score: number;
    vectorScore?: number;
    keywordScore?: number;
    source: 'vector' | 'keyword' | 'hybrid';
    metadata: ChunkMetadata;
    highlights?: string[];
}
/**
 * 搜索历史上下文参数
 */
export interface SearchHistoricalContextParams {
    query: string;
    sessionId?: string;
    agentRole?: string;
    gitCommitHash?: string;
    timeRange?: {
        start: number;
        end: number;
    };
    limit?: number;
    searchType?: 'vector' | 'keyword' | 'hybrid';
}
/**
 * 搜索历史上下文结果
 */
export interface SearchHistoricalContextResult {
    matches: MemoryMatch[];
    totalCount: number;
    searchType: 'vector' | 'keyword' | 'hybrid';
    queryTime: number;
}
/**
 * 语义检索工具接口
 */
export interface ISemanticRetriever {
    searchHistoricalContext(params: SearchHistoricalContextParams): Promise<SearchHistoricalContextResult>;
    vectorSearch(query: string, options?: Partial<HybridSearchConfig>): Promise<VectorSearchResult[]>;
    keywordSearch(query: string, options?: Partial<HybridSearchConfig>): Promise<KeywordSearchResult[]>;
    hybridSearch(query: string, options?: Partial<HybridSearchConfig>): Promise<HybridSearchResult[]>;
}
/**
 * 工具定义
 */
export declare const SEARCH_HISTORICAL_CONTEXT_TOOL: {
    name: string;
    description: string;
    parameters: {
        type: "object";
        properties: {
            query: {
                type: string;
                description: string;
            };
            sessionId: {
                type: string;
                description: string;
            };
            agentRole: {
                type: string;
                description: string;
            };
            gitCommitHash: {
                type: string;
                description: string;
            };
            limit: {
                type: string;
                description: string;
            };
            searchType: {
                type: string;
                enum: string[];
                description: string;
            };
        };
        required: string[];
    };
};
/**
 * 默认混合搜索配置
 */
export declare const DEFAULT_HYBRID_CONFIG: HybridSearchConfig;
//# sourceMappingURL=types.d.ts.map