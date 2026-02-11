import { EventEmitter } from 'events';
/**
 * 模型能力标签
 */
export type ModelCapability = 'reasoning' | 'coding' | 'review' | 'research' | 'frontend' | 'backend' | 'algorithm' | 'simple-tasks' | 'vision' | 'long-context';
/**
 * 模型提供商
 */
export type ModelProvider = 'anthropic' | 'openai' | 'google' | 'local' | 'custom';
/**
 * 模型成本配置（每百万 Token）
 */
export interface ModelCost {
    input: number;
    output: number;
    cached?: number;
}
/**
 * 模型定义
 */
export interface ModelDefinition {
    id: string;
    name: string;
    provider: ModelProvider;
    costPerMToken: ModelCost;
    capabilities: ModelCapability[];
    maxContextTokens?: number;
    maxOutputTokens?: number;
    supportsStreaming?: boolean;
    supportsVision?: boolean;
    description?: string;
    deprecated?: boolean;
}
/**
 * 模型筛选条件
 */
export interface ModelFilter {
    provider?: ModelProvider;
    capabilities?: ModelCapability[];
    maxCostPerMToken?: number;
    minContextTokens?: number;
    supportsVision?: boolean;
    includeDeprecated?: boolean;
}
/**
 * 模型注册表事件
 */
export interface ModelRegistryEvents {
    'model:registered': (model: ModelDefinition) => void;
    'model:removed': (modelId: string) => void;
    'model:updated': (model: ModelDefinition) => void;
}
/**
 * 模型注册表
 * 管理可用模型的注册、查询和筛选
 */
export declare class ModelRegistry extends EventEmitter {
    private models;
    constructor(loadDefaults?: boolean);
    /**
     * 加载默认模型
     */
    private loadDefaultModels;
    /**
     * 注册新模型
     */
    registerModel(model: ModelDefinition): void;
    /**
     * 批量注册模型
     */
    registerModels(models: ModelDefinition[]): void;
    /**
     * 移除模型
     */
    removeModel(modelId: string): boolean;
    /**
     * 获取模型定义
     */
    getModel(modelId: string): ModelDefinition | undefined;
    /**
     * 获取所有可用模型
     */
    getAvailableModels(filter?: ModelFilter): ModelDefinition[];
    /**
     * 按能力获取模型
     */
    getModelsByCapability(capability: ModelCapability): ModelDefinition[];
    /**
     * 按提供商获取模型
     */
    getModelsByProvider(provider: ModelProvider): ModelDefinition[];
    /**
     * 获取最低成本模型（满足指定能力）
     */
    getCheapestModel(capabilities?: ModelCapability[]): ModelDefinition | undefined;
    /**
     * 获取最高能力模型（满足指定能力）
     */
    getMostCapableModel(capabilities?: ModelCapability[]): ModelDefinition | undefined;
    /**
     * 检查模型是否存在
     */
    hasModel(modelId: string): boolean;
    /**
     * 获取模型数量
     */
    getModelCount(): number;
    /**
     * 获取所有提供商
     */
    getProviders(): ModelProvider[];
    /**
     * 获取所有能力标签
     */
    getAllCapabilities(): ModelCapability[];
    /**
     * 估算成本
     */
    estimateCost(modelId: string, inputTokens: number, outputTokens: number): number | undefined;
    /**
     * 导出模型配置
     */
    exportConfig(): ModelDefinition[];
    /**
     * 导入模型配置
     */
    importConfig(models: ModelDefinition[], replace?: boolean): void;
    /**
     * 重置为默认模型
     */
    reset(): void;
}
//# sourceMappingURL=ModelRegistry.d.ts.map