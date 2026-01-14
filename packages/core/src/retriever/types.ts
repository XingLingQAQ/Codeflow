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
  searchHistoricalContext(
    params: SearchHistoricalContextParams
  ): Promise<SearchHistoricalContextResult>;

  vectorSearch(query: string, options?: Partial<HybridSearchConfig>): Promise<VectorSearchResult[]>;

  keywordSearch(
    query: string,
    options?: Partial<HybridSearchConfig>
  ): Promise<KeywordSearchResult[]>;

  hybridSearch(query: string, options?: Partial<HybridSearchConfig>): Promise<HybridSearchResult[]>;
}

/**
 * 工具定义
 */
export const SEARCH_HISTORICAL_CONTEXT_TOOL = {
  name: 'search_historical_context',
  description:
    'Search through historical conversation context using semantic similarity and keyword matching. Returns relevant past conversations, decisions, and code discussions.',
  parameters: {
    type: 'object' as const,
    properties: {
      query: {
        type: 'string',
        description: 'The search query to find relevant historical context',
      },
      sessionId: {
        type: 'string',
        description: 'Optional: Filter by specific session ID',
      },
      agentRole: {
        type: 'string',
        description: 'Optional: Filter by agent role (main, coder, sub_expert)',
      },
      gitCommitHash: {
        type: 'string',
        description: 'Optional: Filter by git commit hash',
      },
      limit: {
        type: 'number',
        description: 'Maximum number of results to return (default: 10)',
      },
      searchType: {
        type: 'string',
        enum: ['vector', 'keyword', 'hybrid'],
        description: 'Type of search to perform (default: hybrid)',
      },
    },
    required: ['query'],
  },
};

/**
 * 默认混合搜索配置
 */
export const DEFAULT_HYBRID_CONFIG: HybridSearchConfig = {
  vectorWeight: 0.7,
  keywordWeight: 0.3,
  topK: 10,
  minScore: 0.3,
  reranking: true,
};
