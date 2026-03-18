import type { IAuditManager } from '../audit/types.js';
import { FileOperationService } from './FileOperationService.js';
import { MCPGateway } from './MCPGateway.js';
import {
  SearchGateway,
  SearchProviderRegistry,
} from './SearchProvider.js';
import { SkillDispatcher } from './SkillDispatcher.js';
import { SkillRegistry } from './SkillRegistry.js';
import type { SearchRegistrationOptions } from './SearchProvider.js';
import { ToolExecutor } from './ToolExecutor.js';
import { ToolRegistry } from './ToolRegistry.js';
import type {
  FileApplyDiffInput,
  FileEditInput,
  FileEditMultipleInput,
  FilePreviewInput,
  MCPServerRegistration,
  RegisteredTool,
  SearchProvider,
  SearchProviderKind,
  SearchRequest,
  SearchResponse,
  SkillExecutionRequest,
  SkillExecutionResult,
  SkillManifest,
  SkillRegistration,
  SkillRegistryFilter,
  ToolContext,
  ToolExecutionResult,
  ToolLikeOutput,
} from './types.js';

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
  private readonly skillRegistry: SkillRegistry;
  private readonly skillDispatcher: SkillDispatcher;

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
    this.skillRegistry = deps.skillRegistry ?? new SkillRegistry();
    this.skillDispatcher =
      deps.skillDispatcher ??
      new SkillDispatcher(this.skillRegistry, this, {
        auditManager: deps.auditManager,
      });

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

  getSkillRegistry(): SkillRegistry {
    return this.skillRegistry;
  }

  getSkillDispatcher(): SkillDispatcher {
    return this.skillDispatcher;
  }

  getToolTraceCount(): number {
    return this.toolExecutor.getTraces().length;
  }

  getToolTraces() {
    return this.toolExecutor.getTraces();
  }

  getSkillExecutionRecords() {
    return this.skillDispatcher.getRecords();
  }

  registerTool(tool: RegisteredTool, replace = false): void {
    this.toolRegistry.register(tool, { replace });
  }

  registerSkill(skill: SkillRegistration, replace = false): void {
    this.skillRegistry.register(skill, { replace });
  }

  registerSkillTool(tool: Omit<RegisteredTool, 'source'>, replace = false): void {
    this.registerTool(
      {
        ...tool,
        source: 'skill',
      },
      replace
    );
    this.registerSkill(
      {
        manifest: {
          skillId: tool.id,
          version: tool.version,
          description: tool.description,
          tags: tool.tags,
          riskLevel: tool.riskLevel,
          source: 'internal',
          entryPoints: tool.entryPoints,
          inputSchema: tool.inputSchema,
          outputSchema: tool.outputSchema,
          toolIds: [tool.id],
        },
        handler: {
          execute: async (input, context) => {
            const traceCountBefore = context.runtime.getToolTraceCount();
            const result = await context.runtime.execute(tool.id, input, context);
            if (!result.ok) {
              throw new Error(result.error?.message ?? `Skill tool failed: ${tool.id}`);
            }
            const traces = context.runtime.getToolTraces();
            const latestTrace = traces.length > traceCountBefore ? traces[traces.length - 1] : undefined;
            return {
              ...(typeof result.output === 'object' && result.output !== null
                ? (result.output as Record<string, unknown>)
                : { value: result.output ?? null }),
              __skillToolCallId: latestTrace?.toolCallId,
            };
          },
        },
      },
      replace
    );
  }

  listSkills(filter: SkillRegistryFilter = {}): SkillManifest[] {
    return this.skillRegistry.list(filter);
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

  executeSkill<TOutput = ToolLikeOutput>(
    request: SkillExecutionRequest
  ): Promise<SkillExecutionResult<TOutput>> {
    return this.skillDispatcher.execute<TOutput>(request);
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
