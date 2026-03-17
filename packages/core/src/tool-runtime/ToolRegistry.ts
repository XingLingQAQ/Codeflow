import type {
  RegisterToolOptions,
  RegisteredTool,
  ToolMetadata,
  ToolRegistryFilter,
} from './types.js';

/**
 * ToolRegistry
 * 统一维护 headless runtime 的 tool 元数据与执行入口
 */
export class ToolRegistry {
  private readonly tools = new Map<string, RegisteredTool>();

  register(tool: RegisteredTool, options: RegisterToolOptions = {}): void {
    const existing = this.tools.get(tool.id);
    if (existing && !options.replace) {
      throw new Error(`Tool already registered: ${tool.id}`);
    }
    this.tools.set(tool.id, tool);
  }

  registerMany(tools: RegisteredTool[], options: RegisterToolOptions = {}): void {
    for (const tool of tools) {
      this.register(tool, options);
    }
  }

  get(toolId: string): RegisteredTool | undefined {
    return this.tools.get(toolId);
  }

  getMetadata(toolId: string): ToolMetadata | undefined {
    const tool = this.tools.get(toolId);
    if (!tool) {
      return undefined;
    }
    const { handler: _handler, ...metadata } = tool;
    return metadata;
  }

  has(toolId: string): boolean {
    return this.tools.has(toolId);
  }

  list(filter: ToolRegistryFilter = {}): ToolMetadata[] {
    return Array.from(this.tools.values())
      .filter((tool) => {
        if (filter.entryPoint && !tool.entryPoints.includes(filter.entryPoint)) {
          return false;
        }
        if (filter.source && tool.source !== filter.source) {
          return false;
        }
        if (filter.tag && !tool.tags.includes(filter.tag)) {
          return false;
        }
        return true;
      })
      .map((tool) => {
        const { handler: _handler, ...metadata } = tool;
        return metadata;
      });
  }

  remove(toolId: string): boolean {
    return this.tools.delete(toolId);
  }

  clear(): void {
    this.tools.clear();
  }
}
