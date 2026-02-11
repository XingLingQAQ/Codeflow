import { EventEmitter } from 'events';
/**
 * 默认模型定义
 */
const DEFAULT_MODELS = [
    {
        id: 'claude-opus-4',
        name: 'Claude Opus 4.5',
        provider: 'anthropic',
        costPerMToken: { input: 15, output: 75, cached: 1.875 },
        capabilities: ['reasoning', 'coding', 'review', 'research', 'vision', 'long-context'],
        maxContextTokens: 200000,
        maxOutputTokens: 32000,
        supportsStreaming: true,
        supportsVision: true,
        description: 'Most capable Claude model for complex reasoning and analysis',
    },
    {
        id: 'claude-sonnet-4',
        name: 'Claude Sonnet 4',
        provider: 'anthropic',
        costPerMToken: { input: 3, output: 15, cached: 0.375 },
        capabilities: ['reasoning', 'coding', 'research', 'backend', 'long-context'],
        maxContextTokens: 200000,
        maxOutputTokens: 64000,
        supportsStreaming: true,
        supportsVision: true,
        description: 'Balanced performance and cost for most tasks',
    },
    {
        id: 'claude-haiku-4',
        name: 'Claude Haiku 4',
        provider: 'anthropic',
        costPerMToken: { input: 0.25, output: 1.25, cached: 0.03 },
        capabilities: ['coding', 'simple-tasks', 'backend'],
        maxContextTokens: 200000,
        maxOutputTokens: 8192,
        supportsStreaming: true,
        supportsVision: true,
        description: 'Fast and cost-effective for simple tasks',
    },
    {
        id: 'gemini-2.0-flash',
        name: 'Gemini 2.0 Flash',
        provider: 'google',
        costPerMToken: { input: 0.1, output: 0.4 },
        capabilities: ['coding', 'frontend', 'simple-tasks', 'vision'],
        maxContextTokens: 1000000,
        maxOutputTokens: 8192,
        supportsStreaming: true,
        supportsVision: true,
        description: 'Fast multimodal model with long context',
    },
    {
        id: 'gemini-2.5-pro',
        name: 'Gemini 2.5 Pro',
        provider: 'google',
        costPerMToken: { input: 1.25, output: 10 },
        capabilities: ['reasoning', 'coding', 'research', 'frontend', 'vision', 'long-context'],
        maxContextTokens: 1000000,
        maxOutputTokens: 65536,
        supportsStreaming: true,
        supportsVision: true,
        description: 'Advanced reasoning with very long context',
    },
    {
        id: 'gpt-4o',
        name: 'GPT-4o',
        provider: 'openai',
        costPerMToken: { input: 2.5, output: 10, cached: 1.25 },
        capabilities: ['reasoning', 'coding', 'review', 'vision'],
        maxContextTokens: 128000,
        maxOutputTokens: 16384,
        supportsStreaming: true,
        supportsVision: true,
        description: 'OpenAI flagship multimodal model',
    },
    {
        id: 'gpt-4o-mini',
        name: 'GPT-4o Mini',
        provider: 'openai',
        costPerMToken: { input: 0.15, output: 0.6, cached: 0.075 },
        capabilities: ['coding', 'simple-tasks', 'vision'],
        maxContextTokens: 128000,
        maxOutputTokens: 16384,
        supportsStreaming: true,
        supportsVision: true,
        description: 'Cost-effective OpenAI model',
    },
    {
        id: 'codex-mini',
        name: 'Codex Mini',
        provider: 'openai',
        costPerMToken: { input: 1.5, output: 6 },
        capabilities: ['coding', 'algorithm', 'backend'],
        maxContextTokens: 200000,
        maxOutputTokens: 100000,
        supportsStreaming: true,
        supportsVision: false,
        description: 'Specialized for code generation and debugging',
    },
    {
        id: 'o3-mini',
        name: 'o3-mini',
        provider: 'openai',
        costPerMToken: { input: 1.1, output: 4.4, cached: 0.55 },
        capabilities: ['reasoning', 'algorithm', 'coding'],
        maxContextTokens: 200000,
        maxOutputTokens: 100000,
        supportsStreaming: true,
        supportsVision: false,
        description: 'Advanced reasoning model for complex problems',
    },
];
/**
 * 模型注册表
 * 管理可用模型的注册、查询和筛选
 */
export class ModelRegistry extends EventEmitter {
    constructor(loadDefaults = true) {
        super();
        this.models = new Map();
        if (loadDefaults) {
            this.loadDefaultModels();
        }
    }
    /**
     * 加载默认模型
     */
    loadDefaultModels() {
        for (const model of DEFAULT_MODELS) {
            this.models.set(model.id, { ...model });
        }
    }
    /**
     * 注册新模型
     */
    registerModel(model) {
        const existing = this.models.has(model.id);
        this.models.set(model.id, { ...model });
        if (existing) {
            this.emit('model:updated', model);
        }
        else {
            this.emit('model:registered', model);
        }
    }
    /**
     * 批量注册模型
     */
    registerModels(models) {
        for (const model of models) {
            this.registerModel(model);
        }
    }
    /**
     * 移除模型
     */
    removeModel(modelId) {
        const removed = this.models.delete(modelId);
        if (removed) {
            this.emit('model:removed', modelId);
        }
        return removed;
    }
    /**
     * 获取模型定义
     */
    getModel(modelId) {
        const model = this.models.get(modelId);
        return model ? { ...model } : undefined;
    }
    /**
     * 获取所有可用模型
     */
    getAvailableModels(filter) {
        let models = Array.from(this.models.values());
        // 默认排除已弃用模型
        const includeDeprecated = filter?.includeDeprecated ?? false;
        models = models.filter(model => {
            // 排除已弃用模型（除非明确包含）
            if (model.deprecated && !includeDeprecated) {
                return false;
            }
            if (filter) {
                // 按提供商筛选
                if (filter.provider && model.provider !== filter.provider) {
                    return false;
                }
                // 按能力筛选（必须包含所有指定能力）
                if (filter.capabilities && filter.capabilities.length > 0) {
                    const hasAllCapabilities = filter.capabilities.every(cap => model.capabilities.includes(cap));
                    if (!hasAllCapabilities) {
                        return false;
                    }
                }
                // 按成本筛选（输入成本）
                if (filter.maxCostPerMToken !== undefined) {
                    if (model.costPerMToken.input > filter.maxCostPerMToken) {
                        return false;
                    }
                }
                // 按上下文长度筛选
                if (filter.minContextTokens !== undefined) {
                    if (!model.maxContextTokens || model.maxContextTokens < filter.minContextTokens) {
                        return false;
                    }
                }
                // 按视觉支持筛选
                if (filter.supportsVision !== undefined) {
                    if (model.supportsVision !== filter.supportsVision) {
                        return false;
                    }
                }
            }
            return true;
        });
        return models.map(m => ({ ...m }));
    }
    /**
     * 按能力获取模型
     */
    getModelsByCapability(capability) {
        return this.getAvailableModels({ capabilities: [capability] });
    }
    /**
     * 按提供商获取模型
     */
    getModelsByProvider(provider) {
        return this.getAvailableModels({ provider });
    }
    /**
     * 获取最低成本模型（满足指定能力）
     */
    getCheapestModel(capabilities) {
        const models = this.getAvailableModels({ capabilities });
        if (models.length === 0)
            return undefined;
        return models.reduce((cheapest, current) => {
            const cheapestCost = cheapest.costPerMToken.input + cheapest.costPerMToken.output;
            const currentCost = current.costPerMToken.input + current.costPerMToken.output;
            return currentCost < cheapestCost ? current : cheapest;
        });
    }
    /**
     * 获取最高能力模型（满足指定能力）
     */
    getMostCapableModel(capabilities) {
        const models = this.getAvailableModels({ capabilities });
        if (models.length === 0)
            return undefined;
        return models.reduce((best, current) => {
            return current.capabilities.length > best.capabilities.length ? current : best;
        });
    }
    /**
     * 检查模型是否存在
     */
    hasModel(modelId) {
        return this.models.has(modelId);
    }
    /**
     * 获取模型数量
     */
    getModelCount() {
        return this.models.size;
    }
    /**
     * 获取所有提供商
     */
    getProviders() {
        const providers = new Set();
        for (const model of this.models.values()) {
            providers.add(model.provider);
        }
        return Array.from(providers);
    }
    /**
     * 获取所有能力标签
     */
    getAllCapabilities() {
        const capabilities = new Set();
        for (const model of this.models.values()) {
            for (const cap of model.capabilities) {
                capabilities.add(cap);
            }
        }
        return Array.from(capabilities);
    }
    /**
     * 估算成本
     */
    estimateCost(modelId, inputTokens, outputTokens) {
        const model = this.models.get(modelId);
        if (!model)
            return undefined;
        const inputCost = (inputTokens / 1000000) * model.costPerMToken.input;
        const outputCost = (outputTokens / 1000000) * model.costPerMToken.output;
        return inputCost + outputCost;
    }
    /**
     * 导出模型配置
     */
    exportConfig() {
        return this.getAvailableModels({ includeDeprecated: true });
    }
    /**
     * 导入模型配置
     */
    importConfig(models, replace = false) {
        if (replace) {
            this.models.clear();
        }
        this.registerModels(models);
    }
    /**
     * 重置为默认模型
     */
    reset() {
        this.models.clear();
        this.loadDefaultModels();
    }
}
//# sourceMappingURL=ModelRegistry.js.map