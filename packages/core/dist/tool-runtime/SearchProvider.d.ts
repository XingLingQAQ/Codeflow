import { ToolExecutor } from './ToolExecutor.js';
import { ToolRegistry } from './ToolRegistry.js';
import type { SearchProvider, SearchProviderKind, SearchRequest, SearchResponse, ToolContext, ToolExecutionResult } from './types.js';
export declare class StaticSearchProvider implements SearchProvider {
    readonly id: string;
    readonly kind: SearchProviderKind;
    private readonly searchFn;
    constructor(id: string, kind: SearchProviderKind, searchFn: (request: SearchRequest, context: ToolContext) => Promise<SearchResponse>);
    search(request: SearchRequest, context: ToolContext): Promise<SearchResponse>;
}
/**
 * SearchProviderRegistry
 * 统一管理 code/knowledge/memory/graph 搜索提供者
 */
export declare class SearchProviderRegistry {
    private readonly providers;
    register(provider: SearchProvider): void;
    list(kind?: SearchProviderKind): SearchProvider[];
    getDefault(kind: SearchProviderKind): SearchProvider | undefined;
}
export declare class SearchRouter {
    private readonly registry;
    constructor(registry: SearchProviderRegistry);
    search(kind: SearchProviderKind, request: SearchRequest, context: ToolContext): Promise<SearchResponse>;
}
export interface SearchRegistrationOptions {
    replaceDefaultTool?: boolean;
}
/**
 * SearchGateway
 * 把搜索 provider 注册为共享 tool，确保搜索入口进入统一 registry / executor / trace 链。
 */
export declare class SearchGateway {
    private readonly registry;
    private readonly toolRegistry;
    private readonly toolExecutor;
    constructor(registry: SearchProviderRegistry, toolRegistry: ToolRegistry, toolExecutor: ToolExecutor);
    register(provider: SearchProvider, options?: SearchRegistrationOptions): void;
    execute(kind: SearchProviderKind, request: SearchRequest, context: ToolContext): Promise<ToolExecutionResult<SearchResponse>>;
    getToolId(kind: SearchProviderKind): string;
}
//# sourceMappingURL=SearchProvider.d.ts.map