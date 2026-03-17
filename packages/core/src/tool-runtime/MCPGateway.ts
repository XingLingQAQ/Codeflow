import type {
  MCPServerInfo,
  MCPServerRegistration,
  RegisteredTool,
  ToolContext,
  ToolExecutionResult,
} from './types.js';
import { ToolExecutor } from './ToolExecutor.js';
import { ToolRegistry } from './ToolRegistry.js';

/**
 * MCPGateway
 * 平台统一管理外部 MCP server 与 tool 映射，不允许绕过 registry 直连
 */
export class MCPGateway {
  private readonly servers = new Map<string, MCPServerInfo>();

  constructor(
    private readonly registry: ToolRegistry,
    private readonly executor: ToolExecutor
  ) {}

  registerServer(registration: MCPServerRegistration): MCPServerInfo {
    const toolIds: string[] = [];

    for (const tool of registration.tools) {
      const toolId = `${registration.serverId}.${tool.id}`;
      const registeredTool: RegisteredTool = {
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

    const info: MCPServerInfo = {
      serverId: registration.serverId,
      description: registration.description,
      timeoutMs: registration.timeoutMs ?? 30000,
      toolIds,
    };
    this.servers.set(registration.serverId, info);
    return info;
  }

  getServer(serverId: string): MCPServerInfo | undefined {
    return this.servers.get(serverId);
  }

  listServers(): MCPServerInfo[] {
    return Array.from(this.servers.values());
  }

  async execute(
    serverId: string,
    toolId: string,
    input: unknown,
    context: ToolContext
  ): Promise<ToolExecutionResult> {
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
