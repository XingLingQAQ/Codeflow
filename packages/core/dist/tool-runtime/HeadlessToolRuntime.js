import { FileOperationService } from './FileOperationService.js';
import { MCPGateway } from './MCPGateway.js';
import { SearchGateway, SearchProviderRegistry, } from './SearchProvider.js';
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
    getToolTraces() {
        return this.toolExecutor.getTraces();
    }
    registerTool(tool, replace = false) {
        this.toolRegistry.register(tool, { replace });
    }
    registerSkillTool(tool, replace = false) {
        this.toolRegistry.register({
            ...tool,
            source: 'skill',
        }, { replace });
    }
    execute(toolId, input, context) {
        return this.toolExecutor.execute({
            toolId,
            input,
            context,
        });
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