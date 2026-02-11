/**
 * 语义检索工具类型定义
 * search_historical_context 工具实现
 */
/**
 * 工具定义
 */
export const SEARCH_HISTORICAL_CONTEXT_TOOL = {
    name: 'search_historical_context',
    description: 'Search through historical conversation context using semantic similarity and keyword matching. Returns relevant past conversations, decisions, and code discussions.',
    parameters: {
        type: 'object',
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
export const DEFAULT_HYBRID_CONFIG = {
    vectorWeight: 0.7,
    keywordWeight: 0.3,
    topK: 10,
    minScore: 0.3,
    reranking: true,
};
//# sourceMappingURL=types.js.map