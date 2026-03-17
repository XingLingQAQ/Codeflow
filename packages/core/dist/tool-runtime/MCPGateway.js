/**
 * MCPGateway
 * 平台统一管理外部 MCP server 与 tool 映射，不允许绕过 registry 直连
 */
export class MCPGateway {
    constructor(registry, executor) {
        this.registry = registry;
        this.executor = executor;
        this.servers = new Map();
    }
    registerServer(registration) {
        const toolIds = [];
        for (const tool of registration.tools) {
            const toolId = `${registration.serverId}.${tool.id}`;
            const registeredTool = {
                id: toolId,
                version: tool.version ?? '1.0.0',
                description: tool.description,
                tags: tool.tags ?? [],
                riskLevel: tool.riskLevel ?? 'medium',
                executionModes: tool.executionModes ?? ['sync'],
                entryPoints: tool.entryPoints ?? ['agent'],
                source: 'mcp',
                inputSchema: tool.inputSchema,
                outputSchema: tool.outputSchema,
                handler: tool.handler,
            };
            this.registry.register(registeredTool, { replace: true });
            toolIds.push(toolId);
        }
        const info = {
            serverId: registration.serverId,
            description: registration.description,
            timeoutMs: registration.timeoutMs ?? 30000,
            toolIds,
        };
        this.servers.set(registration.serverId, info);
        return info;
    }
    getServer(serverId) {
        return this.servers.get(serverId);
    }
    listServers() {
        return Array.from(this.servers.values());
    }
    async execute(serverId, toolId, input, context) {
        const server = this.servers.get(serverId);
        if (!server) {
            throw new Error(`MCP server not registered: ${serverId}`);
        }
        const scopedToolId = `${serverId}.${toolId}`;
        if (!server.toolIds.includes(scopedToolId)) {
            throw new Error(`MCP tool not registered: ${scopedToolId}`);
        }
        return this.executor.execute({
            toolId: scopedToolId,
            input,
            context,
        });
    }
}
//# sourceMappingURL=MCPGateway.js.map