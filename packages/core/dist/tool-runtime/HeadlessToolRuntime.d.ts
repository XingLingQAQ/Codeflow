import { HookManager } from '../hooks/HookManager.js';
import type { HookRuntimeControls } from '../hooks/types.js';
import type { IAuditManager } from '../audit/types.js';
import { FileOperationService } from './FileOperationService.js';
import { MCPGateway } from './MCPGateway.js';
import { SearchGateway, SearchProviderRegistry } from './SearchProvider.js';
import { SkillDispatcher } from './SkillDispatcher.js';
import { SkillRegistry } from './SkillRegistry.js';
import type { SearchRegistrationOptions } from './SearchProvider.js';
import { ToolExecutor } from './ToolExecutor.js';
import { ToolRegistry } from './ToolRegistry.js';
import type { MCPServerRegistration, RegisteredTool, SearchProvider, SearchProviderKind, SearchRequest, SearchResponse, SkillExecutionRequest, SkillExecutionResult, SkillManifest, SkillRegistration, SkillRegistryFilter, ToolContext, ToolExecutionResult, ToolLikeOutput } from './types.js';
export interface HeadlessToolRuntimeDeps {
    auditManager?: IAuditManager;
    toolRegistry?: ToolRegistry;
    toolExecutor?: ToolExecutor;
    fileOperationService?: FileOperationService;
    searchProviderRegistry?: SearchProviderRegistry;
    searchGateway?: SearchGateway;
    mcpGateway?: MCPGateway;
    skillRegistry?: SkillRegistry;
    skillDispatcher?: SkillDispatcher;
    hookManager?: HookManager;
    hookControls?: HookRuntimeControls;
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
    private readonly skillRegistry;
    private readonly skillDispatcher;
    private readonly hookManager;
    constructor(deps?: HeadlessToolRuntimeDeps);
    getToolRegistry(): ToolRegistry;
    getToolExecutor(): ToolExecutor;
    getFileOperationService(): FileOperationService;
    getSearchProviderRegistry(): SearchProviderRegistry;
    getSearchGateway(): SearchGateway;
    getMCPGateway(): MCPGateway;
    getSkillRegistry(): SkillRegistry;
    getSkillDispatcher(): SkillDispatcher;
    getHookManager(): HookManager;
    getToolTraceCount(): number;
    getToolTraces(): import("./types.js").ToolCallTrace[];
    getSkillExecutionRecords(): import("./types.js").SkillExecutionRecord[];
    registerTool(tool: RegisteredTool, replace?: boolean): void;
    registerSkill(skill: SkillRegistration, replace?: boolean): void;
    registerSkillTool(tool: Omit<RegisteredTool, 'source'>, replace?: boolean): void;
    listSkills(filter?: SkillRegistryFilter): SkillManifest[];
    execute<TOutput = ToolLikeOutput>(toolId: string, input: unknown, context: ToolContext): Promise<ToolExecutionResult<TOutput>>;
    executeSkill<TOutput = ToolLikeOutput>(request: SkillExecutionRequest): Promise<SkillExecutionResult<TOutput>>;
    registerSearchProvider(provider: SearchProvider, options?: SearchRegistrationOptions): void;
    executeSearch(kind: SearchProviderKind, request: SearchRequest, context: ToolContext): Promise<ToolExecutionResult<SearchResponse>>;
    registerMCPServer(registration: MCPServerRegistration): import("./types.js").MCPServerInfo;
    private registerBuiltinFileTools;
}
//# sourceMappingURL=HeadlessToolRuntime.d.ts.map