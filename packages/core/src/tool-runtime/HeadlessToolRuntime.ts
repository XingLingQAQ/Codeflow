import type { IAuditManager } from '../audit/types.js';
import { FileOperationService } from './FileOperationService.js';
import { MCPGateway } from './MCPGateway.js';
import {
  SearchGateway,
  SearchProviderRegistry,
} from './SearchProvider.js';
import type { SearchRegistrationOptions } from './SearchProvider.js';
import { ToolExecutor } from './ToolExecutor.js';
import { ToolRegistry } from './ToolRegistry.js';
import type {
  FileApplyDiffInput,
  FileEditInput,
  FileEditMultipleInput,
  FilePreviewInput,
  RegisteredTool,
  SearchProvider,
  SearchProviderKind,
  SearchRequest,
  SearchResponse,
  ToolContext,
  ToolExecutionResult,
  ToolLikeOutput,
  MCPServerRegistration,
} from './types.js';

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
export class HeadlessToolRuntime {
  private readonly toolRegistry: ToolRegistry;
  private readonly toolExecutor: ToolExecutor;
  private readonly fileOperationService: FileOperationService;
  private readonly searchProviderRegistry: SearchProviderRegistry;
  private readonly searchGateway: SearchGateway;
  private readonly mcpGateway: MCPGateway;

  constructor(deps: HeadlessToolRuntimeDeps = {}) {
    this.toolRegistry = deps.toolRegistry ?? new ToolRegistry();
    this.fileOperationService = deps.fileOperationService ?? new FileOperationService();
    this.toolExecutor =
      deps.toolExecutor ??
      new ToolExecutor(this.toolRegistry, {
        auditManager: deps.auditManager,
      });
    this.searchProviderRegistry = deps.searchProviderRegistry ?? new SearchProviderRegistry();
    this.searchGateway =
      deps.searchGateway ??
      new SearchGateway(this.searchProviderRegistry, this.toolRegistry, this.toolExecutor);
    this.mcpGateway = deps.mcpGateway ?? new MCPGateway(this.toolRegistry, this.toolExecutor);

    this.registerBuiltinFileTools();
  }

  getToolRegistry(): ToolRegistry {
    return this.toolRegistry;
  }

  getToolExecutor(): ToolExecutor {
    return this.toolExecutor;
  }

  getFileOperationService(): FileOperationService {
    return this.fileOperationService;
  }

  getSearchProviderRegistry(): SearchProviderRegistry {
    return this.searchProviderRegistry;
  }

  getSearchGateway(): SearchGateway {
    return this.searchGateway;
  }

  getMCPGateway(): MCPGateway {
    return this.mcpGateway;
  }

  getToolTraces() {
    return this.toolExecutor.getTraces();
  }

  registerTool(tool: RegisteredTool, replace = false): void {
    this.toolRegistry.register(tool, { replace });
  }

  registerSkillTool(tool: Omit<RegisteredTool, 'source'>, replace = false): void {
    this.toolRegistry.register(
      {
        ...tool,
        source: 'skill',
      },
      { replace }
    );
  }

  execute<TOutput = ToolLikeOutput>(
    toolId: string,
    input: unknown,
    context: ToolContext
  ): Promise<ToolExecutionResult<TOutput>> {
    return this.toolExecutor.execute<TOutput>({
      toolId,
      input,
      context,
    });
  }

  registerSearchProvider(provider: SearchProvider, options: SearchRegistrationOptions = {}): void {
    this.searchGateway.register(provider, options);
  }

  executeSearch(
    kind: SearchProviderKind,
    request: SearchRequest,
    context: ToolContext
  ): Promise<ToolExecutionResult<SearchResponse>> {
    return this.searchGateway.execute(kind, request, context);
  }

  registerMCPServer(registration: MCPServerRegistration) {
    return this.mcpGateway.registerServer(registration);
  }

  private registerBuiltinFileTools(): void {
    const builtinTools: RegisteredTool[] = [
      {
        id: 'file.preview',
        version: '1.0.0',
        description: 'Preview file edits through the shared file operation service.',
        tags: ['file', 'preview'],
        riskLevel: 'low',
        executionModes: ['sync'],
        entryPoints: ['frontend', 'agent', 'api'],
        source: 'internal',
        inputSchema: {
          type: 'object',
          properties: {
            file: { type: 'string' },
            instruction: { type: 'string' },
          },
          required: ['file', 'instruction'],
        },
        handler: {
          execute: (input, context) =>
            this.fileOperationService.preview(input as FilePreviewInput, context),
        },
      },
      {
        id: 'file.edit',
        version: '1.0.0',
        description: 'Edit a file through the shared file operation service.',
        tags: ['file', 'edit'],
        riskLevel: 'medium',
        executionModes: ['sync'],
        entryPoints: ['frontend', 'agent', 'api'],
        source: 'internal',
        inputSchema: {
          type: 'object',
          properties: {
            file: { type: 'string' },
            instruction: { type: 'string' },
          },
          required: ['file', 'instruction'],
        },
        handler: {
          execute: (input, context) => this.fileOperationService.edit(input as FileEditInput, context),
        },
      },
      {
        id: 'file.edit_multiple',
        version: '1.0.0',
        description: 'Edit multiple files through the shared file operation service.',
        tags: ['file', 'edit', 'batch'],
        riskLevel: 'medium',
        executionModes: ['sync'],
        entryPoints: ['frontend', 'agent', 'api'],
        source: 'internal',
        inputSchema: {
          type: 'object',
          properties: {
            files: { type: 'array', items: { type: 'string' } },
            instruction: { type: 'string' },
          },
          required: ['files', 'instruction'],
        },
        handler: {
          execute: (input, context) =>
            this.fileOperationService.editMultiple(input as FileEditMultipleInput, context),
        },
      },
      {
        id: 'file.apply_diff',
        version: '1.0.0',
        description: 'Apply a diff through the shared file operation service.',
        tags: ['file', 'diff'],
        riskLevel: 'high',
        executionModes: ['sync'],
        entryPoints: ['frontend', 'agent', 'api'],
        source: 'internal',
        inputSchema: {
          type: 'object',
          properties: {
            file: { type: 'string' },
            diff: { type: 'object' },
          },
          required: ['file', 'diff'],
        },
        handler: {
          execute: (input, context) =>
            this.fileOperationService.applyDiff(input as FileApplyDiffInput, context),
        },
      },
      {
        id: 'file.undo',
        version: '1.0.0',
        description: 'Undo the latest file operation through the shared file operation service.',
        tags: ['file', 'undo'],
        riskLevel: 'high',
        executionModes: ['sync'],
        entryPoints: ['frontend', 'agent', 'api'],
        source: 'internal',
        inputSchema: {
          type: 'object',
          properties: {},
          required: [],
        },
        handler: {
          execute: (_input, context) => this.fileOperationService.undo({}, context),
        },
      },
    ];

    for (const tool of builtinTools) {
      if (!this.toolRegistry.has(tool.id)) {
        this.toolRegistry.register(tool);
      }
    }
  }
}
