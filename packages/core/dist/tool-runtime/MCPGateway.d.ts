import type { MCPServerInfo, MCPServerRegistration, ToolContext, ToolExecutionResult } from './types.js';
import { ToolExecutor } from './ToolExecutor.js';
import { ToolRegistry } from './ToolRegistry.js';
/**
 * MCPGateway
 * 平台统一管理外部 MCP server 与 tool 映射，不允许绕过 registry 直连
 */
export declare class MCPGateway {
    private readonly registry;
    private readonly executor;
    private readonly servers;
    constructor(registry: ToolRegistry, executor: ToolExecutor);
    registerServer(registration: MCPServerRegistration): MCPServerInfo;
    getServer(serverId: string): MCPServerInfo | undefined;
    listServers(): MCPServerInfo[];
    execute(serverId: string, toolId: string, input: unknown, context: ToolContext): Promise<ToolExecutionResult>;
}
//# sourceMappingURL=MCPGateway.d.ts.map