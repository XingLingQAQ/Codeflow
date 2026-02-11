/**
 * MemorySearchTool - MS-230 主动 RAG 工具
 *
 * 将记忆搜索暴露为 MCP 兼容的工具定义，
 * 供 AI 主动调用以检索历史记忆。
 */
export class MemorySearchTool {
    constructor(memoryService) {
        this.memoryService = memoryService;
    }
    /**
     * 返回 MCP 兼容的工具定义
     */
    getToolDefinition() {
        return {
            name: 'search_memory',
            description: '搜索历史记忆。支持语义搜索、时间范围过滤和标签过滤。用于检索之前对话中的关键信息、用户偏好、技术决策等。',
            inputSchema: {
                type: 'object',
                properties: {
                    query: {
                        type: 'string',
                        description: '搜索查询文本，支持自然语言语义搜索',
                    },
                    timeRange: {
                        type: 'object',
                        description: '时间范围过滤（Unix 时间戳，秒）',
                        properties: {
                            start: {
                                type: 'number',
                                description: '起始时间戳',
                            },
                            end: {
                                type: 'number',
                                description: '结束时间戳',
                            },
                        },
                        required: ['start', 'end'],
                    },
                    tags: {
                        type: 'array',
                        items: { type: 'string' },
                        description: '按标签过滤，如 ["preference", "decision"]',
                    },
                    limit: {
                        type: 'number',
                        description: '返回结果数量上限，默认 10',
                    },
                    sessionId: {
                        type: 'string',
                        description: '限定搜索的会话 ID',
                    },
                    folderId: {
                        type: 'string',
                        description: '限定搜索的文件夹 ID',
                    },
                },
                required: ['query'],
            },
        };
    }
    /**
     * 执行记忆搜索
     */
    async execute(params) {
        this.validateParams(params);
        const limit = params.limit && params.limit > 0 ? params.limit : 10;
        let memories;
        if (params.timeRange) {
            const timeResults = await this.memoryService.searchByTimeRange(params.timeRange.start, params.timeRange.end);
            memories = this.applyPostFilters(timeResults, params);
            if (params.query.trim()) {
                const semanticResults = await this.memoryService.search(params.query, {
                    limit: limit * 2,
                    offset: 0,
                    sessionId: params.sessionId,
                    folderId: params.folderId,
                    startAt: params.timeRange.start,
                    endAt: params.timeRange.end,
                    tags: params.tags,
                });
                const idSet = new Set(memories.map((m) => m.id));
                for (const result of semanticResults) {
                    if (!idSet.has(result.id)) {
                        memories.push(result);
                    }
                }
            }
        }
        else if (params.tags && params.tags.length > 0 && !params.query.trim()) {
            const tagResults = await this.memoryService.searchByTags(params.tags);
            memories = this.applyPostFilters(tagResults, params);
        }
        else {
            memories = await this.memoryService.search(params.query, {
                limit: limit * 2,
                offset: 0,
                sessionId: params.sessionId,
                folderId: params.folderId,
                tags: params.tags,
            });
        }
        const limited = memories.slice(0, limit);
        return {
            memories: limited,
            total: limited.length,
            query: params.query,
        };
    }
    validateParams(params) {
        if (!params) {
            throw new Error('search_memory: params is required');
        }
        if (typeof params.query !== 'string') {
            throw new Error('search_memory: query must be a string');
        }
        if (params.timeRange) {
            if (typeof params.timeRange.start !== 'number' ||
                typeof params.timeRange.end !== 'number') {
                throw new Error('search_memory: timeRange.start and timeRange.end must be numbers');
            }
            if (params.timeRange.start > params.timeRange.end) {
                throw new Error('search_memory: timeRange.start must be <= timeRange.end');
            }
        }
        if (params.tags && !Array.isArray(params.tags)) {
            throw new Error('search_memory: tags must be an array');
        }
        if (params.limit !== undefined && (typeof params.limit !== 'number' || params.limit < 0)) {
            throw new Error('search_memory: limit must be a non-negative number');
        }
    }
    applyPostFilters(memories, params) {
        let filtered = memories;
        if (params.sessionId) {
            filtered = filtered.filter((m) => m.sessionId === params.sessionId);
        }
        if (params.folderId) {
            filtered = filtered.filter((m) => m.folderId === params.folderId);
        }
        if (params.tags && params.tags.length > 0) {
            const tagSet = new Set(params.tags);
            filtered = filtered.filter((m) => m.tags.some((tag) => tagSet.has(tag)));
        }
        return filtered;
    }
}
//# sourceMappingURL=MemorySearchTool.js.map