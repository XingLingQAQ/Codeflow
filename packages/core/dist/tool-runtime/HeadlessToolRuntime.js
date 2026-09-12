import { HookManager } from '../hooks/HookManager.js';
import { FileOperationService } from './FileOperationService.js';
import { MCPGateway } from './MCPGateway.js';
import { SearchGateway, SearchProviderRegistry, } from './SearchProvider.js';
import { SkillDispatcher } from './SkillDispatcher.js';
import { SkillRegistry } from './SkillRegistry.js';
import { ToolExecutor } from './ToolExecutor.js';
import { ToolRegistry } from './ToolRegistry.js';
/**
 * HeadlessToolRuntime
 * 统一收口 registry / executor / file / search / MCP，共享同一条 trace 与审计链。
 */
export class HeadlessToolRuntime {
    constructor(deps = {}) {
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
        this.hookManager = deps.hookManager ?? new HookManager(undefined, deps.hookControls);
        if (deps.hookControls) {
            this.hookManager.setControls(deps.hookControls);
        }
        this.registerBuiltinFileTools();
    }
    getToolRegistry() {
        return this.toolRegistry;
    }
    getToolExecutor() {
        return this.toolExecutor;
    }
    getFileOperationService() {
        return this.fileOperationService;
    }
    getSearchProviderRegistry() {
        return this.searchProviderRegistry;
    }
    getSearchGateway() {
        return this.searchGateway;
    }
    getMCPGateway() {
        return this.mcpGateway;
    }
    getSkillRegistry() {
        return this.skillRegistry;
    }
    getSkillDispatcher() {
        return this.skillDispatcher;
    }
    getHookManager() {
        return this.hookManager;
    }
    getToolTraceCount() {
        return this.toolExecutor.getTraces().length;
    }
    getToolTraces() {
        return this.toolExecutor.getTraces();
    }
    getSkillExecutionRecords() {
        return this.skillDispatcher.getRecords();
    }
    registerTool(tool, replace = false) {
        this.toolRegistry.register(tool, { replace });
    }
    registerSkill(skill, replace = false) {
        this.skillRegistry.register(skill, { replace });
    }
    registerSkillTool(tool, replace = false) {
        this.registerTool({
            ...tool,
            source: 'skill',
        }, replace);
        this.registerSkill({
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
                            ? result.output
                            : { value: result.output ?? null }),
                        __skillToolCallId: latestTrace?.toolCallId,
                    };
                },
            },
        }, replace);
    }
    listSkills(filter = {}) {
        return this.skillRegistry.list(filter);
    }
    execute(toolId, input, context) {
        return this.toolExecutor.execute({
            toolId,
            input,
            context,
        });
    }
    executeSkill(request) {
        return this.skillDispatcher.execute(request);
    }
    registerSearchProvider(provider, options = {}) {
        this.searchGateway.register(provider, options);
    }
    executeSearch(kind, request, context) {
        return this.searchGateway.execute(kind, request, context);
    }
    registerMCPServer(registration) {
        return this.mcpGateway.registerServer(registration);
    }
    registerBuiltinFileTools() {
        const builtinTools = [
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
                    execute: (input, context) => this.fileOperationService.preview(input, context),
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
                    execute: (input, context) => this.fileOperationService.edit(input, context),
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
                    execute: (input, context) => this.fileOperationService.editMultiple(input, context),
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
                    execute: (input, context) => this.fileOperationService.applyDiff(input, context),
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
//# sourceMappingURL=HeadlessToolRuntime.js.map