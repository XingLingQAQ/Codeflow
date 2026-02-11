import { EventEmitter } from 'events';
/**
 * 默认阶段模型映射
 * 基于成本优化原则：
 * - 决策类任务使用高端模型（Opus）
 * - 代码生成使用低成本模型（Haiku）
 * - 研究类使用中端模型（Sonnet）
 */
const DEFAULT_PHASE_MAPPINGS = {
    vision: {
        phase: 'vision',
        modelId: 'claude-opus-4',
        fallbackModelId: 'claude-sonnet-4',
        requiredCapabilities: ['reasoning'],
        description: '愿景构建需要深度理解和推理能力',
    },
    constraints: {
        phase: 'constraints',
        modelId: 'claude-sonnet-4',
        fallbackModelId: 'claude-haiku-4',
        requiredCapabilities: ['reasoning'],
        description: '约束提取需要分析能力',
    },
    architecture: {
        phase: 'architecture',
        modelId: 'claude-opus-4',
        fallbackModelId: 'claude-sonnet-4',
        requiredCapabilities: ['reasoning', 'coding'],
        description: '架构设计需要最强推理能力',
    },
    research: {
        phase: 'research',
        modelId: 'claude-sonnet-4',
        fallbackModelId: 'gemini-2.5-pro',
        requiredCapabilities: ['research'],
        description: '技术研究需要广泛知识',
    },
    explore: {
        phase: 'explore',
        modelId: 'claude-haiku-4',
        fallbackModelId: 'gemini-2.0-flash',
        requiredCapabilities: ['coding'],
        description: '并行探索使用低成本模型',
    },
    review: {
        phase: 'review',
        modelId: 'claude-opus-4',
        fallbackModelId: 'claude-sonnet-4',
        requiredCapabilities: ['review'],
        description: '计划评审需要高质量审查',
    },
    implement: {
        phase: 'implement',
        modelId: 'claude-haiku-4',
        fallbackModelId: 'gemini-2.0-flash',
        requiredCapabilities: ['coding'],
        description: '代码实现使用低成本模型',
    },
    qa: {
        phase: 'qa',
        modelId: 'claude-opus-4',
        fallbackModelId: 'claude-sonnet-4',
        requiredCapabilities: ['review', 'coding'],
        description: '质量检查需要高质量审查',
    },
};
/**
 * 所有阶段列表
 */
export const ALL_PHASES = [
    'vision',
    'constraints',
    'architecture',
    'research',
    'explore',
    'review',
    'implement',
    'qa',
];
/**
 * 阶段模型映射管理器
 * 管理 Plan 模式各阶段使用的模型配置
 */
export class PhaseModelMapping extends EventEmitter {
    constructor(modelRegistry) {
        super();
        this.mappings = new Map();
        this.modelRegistry = modelRegistry;
        this.loadDefaultMappings();
    }
    /**
     * 加载默认映射
     */
    loadDefaultMappings() {
        for (const phase of ALL_PHASES) {
            this.mappings.set(phase, { ...DEFAULT_PHASE_MAPPINGS[phase] });
        }
    }
    /**
     * 获取阶段的模型配置
     */
    getPhaseConfig(phase) {
        const config = this.mappings.get(phase);
        return config ? { ...config } : undefined;
    }
    /**
     * 获取阶段使用的模型 ID
     */
    getModelForPhase(phase) {
        const config = this.mappings.get(phase);
        if (!config)
            return undefined;
        // 检查主模型是否可用
        if (this.modelRegistry.hasModel(config.modelId)) {
            return config.modelId;
        }
        // 使用备用模型
        if (config.fallbackModelId && this.modelRegistry.hasModel(config.fallbackModelId)) {
            return config.fallbackModelId;
        }
        return undefined;
    }
    /**
     * 获取阶段使用的模型定义
     */
    getModelDefinitionForPhase(phase) {
        const modelId = this.getModelForPhase(phase);
        if (!modelId)
            return undefined;
        return this.modelRegistry.getModel(modelId);
    }
    /**
     * 设置阶段的模型
     */
    setModelForPhase(phase, modelId) {
        // 验证模型是否存在
        if (!this.modelRegistry.hasModel(modelId)) {
            return false;
        }
        // 验证模型是否满足阶段要求的能力
        const config = this.mappings.get(phase);
        if (config?.requiredCapabilities) {
            const model = this.modelRegistry.getModel(modelId);
            if (model) {
                const hasRequiredCapabilities = config.requiredCapabilities.every(cap => model.capabilities.includes(cap));
                if (!hasRequiredCapabilities) {
                    return false;
                }
            }
        }
        // 更新映射
        const existingConfig = this.mappings.get(phase);
        if (existingConfig) {
            existingConfig.modelId = modelId;
            this.mappings.set(phase, existingConfig);
        }
        else {
            this.mappings.set(phase, {
                phase,
                modelId,
            });
        }
        this.emit('mapping:changed', phase, modelId);
        return true;
    }
    /**
     * 设置阶段的备用模型
     */
    setFallbackModelForPhase(phase, modelId) {
        if (!this.modelRegistry.hasModel(modelId)) {
            return false;
        }
        const config = this.mappings.get(phase);
        if (config) {
            config.fallbackModelId = modelId;
            this.mappings.set(phase, config);
            return true;
        }
        return false;
    }
    /**
     * 获取所有阶段的映射配置
     */
    getAllMappings() {
        return Array.from(this.mappings.values()).map(config => ({ ...config }));
    }
    /**
     * 获取所有阶段的模型 ID 映射
     */
    getMappingsAsRecord() {
        const result = {};
        for (const phase of ALL_PHASES) {
            const modelId = this.getModelForPhase(phase);
            if (modelId) {
                result[phase] = modelId;
            }
        }
        return result;
    }
    /**
     * 批量设置映射
     */
    setMappings(mappings) {
        for (const [phase, modelId] of Object.entries(mappings)) {
            this.setModelForPhase(phase, modelId);
        }
    }
    /**
     * 重置为默认映射
     */
    reset() {
        this.mappings.clear();
        this.loadDefaultMappings();
        this.emit('mapping:reset');
    }
    /**
     * 验证所有映射是否有效
     */
    validateMappings() {
        const errors = [];
        for (const phase of ALL_PHASES) {
            const config = this.mappings.get(phase);
            if (!config) {
                errors.push(`Missing mapping for phase: ${phase}`);
                continue;
            }
            // 检查主模型
            if (!this.modelRegistry.hasModel(config.modelId)) {
                errors.push(`Model not found for phase ${phase}: ${config.modelId}`);
            }
            // 检查备用模型
            if (config.fallbackModelId && !this.modelRegistry.hasModel(config.fallbackModelId)) {
                errors.push(`Fallback model not found for phase ${phase}: ${config.fallbackModelId}`);
            }
            // 检查能力要求
            if (config.requiredCapabilities) {
                const model = this.modelRegistry.getModel(config.modelId);
                if (model) {
                    for (const cap of config.requiredCapabilities) {
                        if (!model.capabilities.includes(cap)) {
                            errors.push(`Model ${config.modelId} missing required capability '${cap}' for phase ${phase}`);
                        }
                    }
                }
            }
        }
        return {
            valid: errors.length === 0,
            errors,
        };
    }
    /**
     * 获取适合指定阶段的模型列表
     */
    getSuitableModelsForPhase(phase) {
        const config = this.mappings.get(phase);
        if (!config?.requiredCapabilities) {
            return this.modelRegistry.getAvailableModels();
        }
        return this.modelRegistry.getAvailableModels({
            capabilities: config.requiredCapabilities,
        });
    }
    /**
     * 估算所有阶段的总成本
     */
    estimateTotalCost(tokensPerPhase = 10000) {
        let totalCost = 0;
        for (const phase of ALL_PHASES) {
            const model = this.getModelDefinitionForPhase(phase);
            if (model) {
                const cost = this.modelRegistry.estimateCost(model.id, tokensPerPhase, tokensPerPhase / 2);
                if (cost !== undefined) {
                    totalCost += cost;
                }
            }
        }
        return totalCost;
    }
    /**
     * 获取成本最优的映射建议
     */
    getCostOptimizedMappings() {
        const result = {};
        for (const phase of ALL_PHASES) {
            const config = this.mappings.get(phase);
            const suitableModels = this.getSuitableModelsForPhase(phase);
            if (suitableModels.length > 0) {
                // 选择成本最低的模型
                const cheapest = suitableModels.reduce((min, current) => {
                    const minCost = min.costPerMToken.input + min.costPerMToken.output;
                    const currentCost = current.costPerMToken.input + current.costPerMToken.output;
                    return currentCost < minCost ? current : min;
                });
                result[phase] = cheapest.id;
            }
            else if (config) {
                result[phase] = config.modelId;
            }
        }
        return result;
    }
    /**
     * 获取质量最优的映射建议
     */
    getQualityOptimizedMappings() {
        const result = {};
        for (const phase of ALL_PHASES) {
            const config = this.mappings.get(phase);
            const suitableModels = this.getSuitableModelsForPhase(phase);
            if (suitableModels.length > 0) {
                // 选择能力最多的模型（通常意味着更高质量）
                const best = suitableModels.reduce((max, current) => {
                    return current.capabilities.length > max.capabilities.length ? current : max;
                });
                result[phase] = best.id;
            }
            else if (config) {
                result[phase] = config.modelId;
            }
        }
        return result;
    }
    /**
     * 导出配置
     */
    exportConfig() {
        return this.getAllMappings();
    }
    /**
     * 导入配置
     */
    importConfig(configs) {
        for (const config of configs) {
            if (ALL_PHASES.includes(config.phase)) {
                this.mappings.set(config.phase, { ...config });
            }
        }
    }
}
//# sourceMappingURL=PhaseModelMapping.js.map