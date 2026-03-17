import type { IAuditManager } from '../audit/types.js';
import { FileOperationService } from './FileOperationService.js';
import { MCPGateway } from './MCPGateway.js';
import { SearchGateway, SearchProviderRegistry } from './SearchProvider.js';
import type { SearchRegistrationOptions } from './SearchProvider.js';
import { ToolExecutor } from './ToolExecutor.js';
import { ToolRegistry } from './ToolRegistry.js';
import type { RegisteredTool, SearchProvider, SearchProviderKind, SearchRequest, SearchResponse, ToolContext, ToolExecutionResult, ToolLikeOutput, MCPServerRegistration } from './types.js';
export interface HeadlessToolRuntimeDeps {
    auditManager?: IAuditManager;
    toolRegistry?: ToolRegistry;
    toolExecutor?: ToolExecutor;
    fileOperationService?: FileOperationService;
    searchProviderRegistry?: SearchProviderRegistry;
    searchGateway?: SearchGateway;
    mcpGateway?: MCPGateway;
}
/**
 * HeadlessToolRuntime
 * 统一收口 registry / executor / file / search / MCP，共享同一条 trace 与审计链。
 */
export declare class HeadlessToolRuntime {
    private readonly toolRegistry;
    private readonly toolExecutor;
    private readonly fileOperationService;
    private readonly searchProviderRegistry;
    private readonly searchGateway;
    private readonly mcpGateway;
    constructor(deps?: HeadlessToolRuntimeDeps);
    getToolRegistry(): ToolRegistry;
    getToolExecutor(): ToolExecutor;
    getFileOperationService(): FileOperationService;
    getSearchProviderRegistry(): SearchProviderRegistry;
    getSearchGateway(): SearchGateway;
    getMCPGateway(): MCPGateway;
    getToolTraces(): import("./types.js").ToolCallTrace[];
    registerTool(tool: RegisteredTool, replace?: boolean): void;
    registerSkillTool(tool: Omit<RegisteredTool, 'source'>, replace?: boolean): void;
    execute<TOutput = ToolLikeOutput>(toolId: string, input: unknown, context: ToolContext): Promise<ToolExecutionResult<TOutput>>;
    registerSearchProvider(provider: SearchProvider, options?: SearchRegistrationOptions): void;
    executeSearch(kind: SearchProviderKind, request: SearchRequest, context: ToolContext): Promise<ToolExecutionResult<SearchResponse>>;
    registerMCPServer(registration: MCPServerRegistration): import("./types.js").MCPServerInfo;
    private registerBuiltinFileTools;
}
//# sourceMappingURL=HeadlessToolRuntime.d.ts.map