/**
 * ToolRegistry
 * 统一维护 headless runtime 的 tool 元数据与执行入口
 */
export class ToolRegistry {
    constructor() {
        this.tools = new Map();
    }
    register(tool, options = {}) {
        const existing = this.tools.get(tool.id);
        if (existing && !options.replace) {
            throw new Error(`Tool already registered: ${tool.id}`);
        }
        this.tools.set(tool.id, tool);
    }
    registerMany(tools, options = {}) {
        for (const tool of tools) {
            this.register(tool, options);
        }
    }
    get(toolId) {
        return this.tools.get(toolId);
    }
    getMetadata(toolId) {
        const tool = this.tools.get(toolId);
        if (!tool) {
            return undefined;
        }
        const { handler: _handler, ...metadata } = tool;
        return metadata;
    }
    has(toolId) {
        return this.tools.has(toolId);
    }
    list(filter = {}) {
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
    remove(toolId) {
        return this.tools.delete(toolId);
    }
    clear() {
        this.tools.clear();
    }
}
//# sourceMappingURL=ToolRegistry.js.map