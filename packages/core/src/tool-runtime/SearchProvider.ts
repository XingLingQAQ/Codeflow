import { ToolExecutor } from './ToolExecutor.js';
import { ToolRegistry } from './ToolRegistry.js';
import type {
  RegisteredTool,
  SearchProvider,
  SearchProviderKind,
  SearchRequest,
  SearchResponse,
  ToolContext,
  ToolExecutionResult,
} from './types.js';

export class StaticSearchProvider implements SearchProvider {
  constructor(
    public readonly id: string,
    public readonly kind: SearchProviderKind,
    private readonly searchFn: (request: SearchRequest, context: ToolContext) => Promise<SearchResponse>
  ) {}

  search(request: SearchRequest, context: ToolContext): Promise<SearchResponse> {
    return this.searchFn(request, context);
  }
}

/**
 * SearchProviderRegistry
 * 统一管理 code/knowledge/memory/graph 搜索提供者
 */
export class SearchProviderRegistry {
  private readonly providers = new Map<SearchProviderKind, SearchProvider[]>();

  register(provider: SearchProvider): void {
    const existing = this.providers.get(provider.kind) ?? [];
    existing.push(provider);
    this.providers.set(provider.kind, existing);
  }

  list(kind?: SearchProviderKind): SearchProvider[] {
    if (!kind) {
      return Array.from(this.providers.values()).flat();
    }
    return [...(this.providers.get(kind) ?? [])];
  }

  getDefault(kind: SearchProviderKind): SearchProvider | undefined {
    return this.providers.get(kind)?.[0];
  }
}

export class SearchRouter {
  constructor(private readonly registry: SearchProviderRegistry) {}

  async search(
    kind: SearchProviderKind,
    request: SearchRequest,
    context: ToolContext
  ): Promise<SearchResponse> {
    const provider = this.registry.getDefault(kind);
    if (!provider) {
      throw new Error(`Search provider not found for kind: ${kind}`);
    }
    return provider.search(request, context);
  }
}

export interface SearchRegistrationOptions {
  replaceDefaultTool?: boolean;
}

/**
 * SearchGateway
 * 把搜索 provider 注册为共享 tool，确保搜索入口进入统一 registry / executor / trace 链。
 */
export class SearchGateway {
  constructor(
    private readonly registry: SearchProviderRegistry,
    private readonly toolRegistry: ToolRegistry,
    private readonly toolExecutor: ToolExecutor
  ) {}

  register(provider: SearchProvider, options: SearchRegistrationOptions = {}): void {
    this.registry.register(provider);

    const toolId = this.getToolId(provider.kind);
    if (this.toolRegistry.has(toolId) && !options.replaceDefaultTool) {
      return;
    }

    const tool: RegisteredTool<SearchRequest, SearchResponse> = {
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

  async execute(
    kind: SearchProviderKind,
    request: SearchRequest,
    context: ToolContext
  ): Promise<ToolExecutionResult<SearchResponse>> {
    return this.toolExecutor.execute<SearchResponse>({
      toolId: this.getToolId(kind),
      input: request,
      context,
    });
  }

  getToolId(kind: SearchProviderKind): string {
    return `search.${kind}`;
  }
}
