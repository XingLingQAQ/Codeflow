export class StaticSearchProvider {
    constructor(id, kind, searchFn) {
        this.id = id;
        this.kind = kind;
        this.searchFn = searchFn;
    }
    search(request, context) {
        return this.searchFn(request, context);
    }
}
/**
 * SearchProviderRegistry
 * 统一管理 code/knowledge/memory/graph 搜索提供者
 */
export class SearchProviderRegistry {
    constructor() {
        this.providers = new Map();
    }
    register(provider) {
        const existing = this.providers.get(provider.kind) ?? [];
        existing.push(provider);
        this.providers.set(provider.kind, existing);
    }
    list(kind) {
        if (!kind) {
            return Array.from(this.providers.values()).flat();
        }
        return [...(this.providers.get(kind) ?? [])];
    }
    getDefault(kind) {
        return this.providers.get(kind)?.[0];
    }
}
export class SearchRouter {
    constructor(registry) {
        this.registry = registry;
    }
    async search(kind, request, context) {
        const provider = this.registry.getDefault(kind);
        if (!provider) {
            throw new Error(`Search provider not found for kind: ${kind}`);
        }
        return provider.search(request, context);
    }
}
/**
 * SearchGateway
 * 把搜索 provider 注册为共享 tool，确保搜索入口进入统一 registry / executor / trace 链。
 */
export class SearchGateway {
    constructor(registry, toolRegistry, toolExecutor) {
        this.registry = registry;
        this.toolRegistry = toolRegistry;
        this.toolExecutor = toolExecutor;
    }
    register(provider, options = {}) {
        this.registry.register(provider);
        const toolId = this.getToolId(provider.kind);
        if (this.toolRegistry.has(toolId) && !options.replaceDefaultTool) {
            return;
        }
        const tool = {
            id: toolId,
            version: '1.0.0',
            description: `Search ${provider.kind} sources through the shared search gateway.`,
            tags: ['search', provider.kind, provider.id],
            riskLevel: 'low',
            executionModes: ['sync'],
            entryPoints: ['frontend', 'agent', 'api'],
            source: 'internal',
            inputSchema: {
                type: 'object',
                properties: {
                    query: { type: 'string' },
                    limit: { type: 'number' },
                    filters: { type: 'object' },
                },
                required: ['query'],
            },
            outputSchema: {
                type: 'object',
                properties: {
                    provider: { type: 'string' },
                    items: { type: 'array' },
                    total: { type: 'number' },
                },
                required: ['provider', 'items', 'total'],
            },
            handler: {
                execute: (input, context) => provider.search(input, context),
            },
        };
        this.toolRegistry.register(tool, { replace: true });
    }
    async execute(kind, request, context) {
        return this.toolExecutor.execute({
            toolId: this.getToolId(kind),
            input: request,
            context,
        });
    }
    getToolId(kind) {
        return `search.${kind}`;
    }
}
//# sourceMappingURL=SearchProvider.js.map