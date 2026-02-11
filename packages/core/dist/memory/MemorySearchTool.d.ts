/**
 * MemorySearchTool - MS-230 主动 RAG 工具
 *
 * 将记忆搜索暴露为 MCP 兼容的工具定义，
 * 供 AI 主动调用以检索历史记忆。
 */
import { AtomicMemoryService } from './AtomicMemoryService.js';
import { AtomicMemory } from './types.js';
/**
 * 工具参数定义
 */
export interface MemorySearchParams {
    query: string;
    timeRange?: {
        start: number;
        end: number;
    };
    tags?: string[];
    limit?: number;
    sessionId?: string;
    folderId?: string;
}
/**
 * 工具执行结果
 */
export interface MemorySearchResult {
    memories: AtomicMemory[];
    total: number;
    query: string;
}
/**
 * MCP 兼容的工具定义
 */
export interface ToolDefinition {
    name: string;
    description: string;
    inputSchema: {
        type: 'object';
        properties: Record<string, unknown>;
        required: string[];
    };
}
export declare class MemorySearchTool {
    private readonly memoryService;
    constructor(memoryService: AtomicMemoryService);
    /**
     * 返回 MCP 兼容的工具定义
     */
    getToolDefinition(): ToolDefinition;
    /**
     * 执行记忆搜索
     */
    execute(params: MemorySearchParams): Promise<MemorySearchResult>;
    private validateParams;
    private applyPostFilters;
}
//# sourceMappingURL=MemorySearchTool.d.ts.map