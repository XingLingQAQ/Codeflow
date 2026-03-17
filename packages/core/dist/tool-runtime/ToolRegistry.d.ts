import type { RegisterToolOptions, RegisteredTool, ToolMetadata, ToolRegistryFilter } from './types.js';
/**
 * ToolRegistry
 * 统一维护 headless runtime 的 tool 元数据与执行入口
 */
export declare class ToolRegistry {
    private readonly tools;
    register(tool: RegisteredTool, options?: RegisterToolOptions): void;
    registerMany(tools: RegisteredTool[], options?: RegisterToolOptions): void;
    get(toolId: string): RegisteredTool | undefined;
    getMetadata(toolId: string): ToolMetadata | undefined;
    has(toolId: string): boolean;
    list(filter?: ToolRegistryFilter): ToolMetadata[];
    remove(toolId: string): boolean;
    clear(): void;
}
//# sourceMappingURL=ToolRegistry.d.ts.map