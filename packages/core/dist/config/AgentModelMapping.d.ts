import { EventEmitter } from 'events';
import { ModelRegistry, ModelDefinition, ModelCapability } from './ModelRegistry.js';
/**
 * Agent 角色类型（扩展自 Commander）
 */
export type AgentType = 'main' | 'coder' | 'research' | 'check' | 'dispatch' | 'sub';
/**
 * 任务类型（用于 Coder Agent 细分）
 */
export type TaskType = 'default' | 'frontend' | 'backend' | 'algorithm' | 'debug' | 'refactor';
/**
 * Agent 模型配置
 */
export interface AgentModelConfig {
    agent: AgentType;
    modelId: string;
    fallbackModelId?: string;
    requiredCapabilities?: ModelCapability[];
    taskTypeOverrides?: Partial<Record<TaskType, string>>;
    description?: string;
}
/**
 * Agent 模型映射事件
 */
export interface AgentModelMappingEvents {
    'mapping:changed': (agent: AgentType, modelId: string, taskType?: TaskType) => void;
    'mapping:reset': () => void;
}
/**
 * 所有 Agent 类型列表
 */
export declare const ALL_AGENTS: AgentType[];
/**
 * 所有任务类型列表
 */
export declare const ALL_TASK_TYPES: TaskType[];
/**
 * Agent 模型映射管理器
 * 管理各 Agent 使用的模型配置
 */
export declare class AgentModelMapping extends EventEmitter {
    private mappings;
    private modelRegistry;
    constructor(modelRegistry: ModelRegistry);
    /**
     * 加载默认映射
     */
    private loadDefaultMappings;
    /**
     * 获取 Agent 的模型配置
     */
    getAgentConfig(agent: AgentType): AgentModelConfig | undefined;
    /**
     * 获取 Agent 使用的模型 ID
     */
    getModelForAgent(agent: AgentType, taskType?: TaskType): string | undefined;
    /**
     * 获取 Agent 使用的模型定义
     */
    getModelDefinitionForAgent(agent: AgentType, taskType?: TaskType): ModelDefinition | undefined;
    /**
     * 设置 Agent 的模型
     */
    setModelForAgent(agent: AgentType, modelId: string): boolean;
    /**
     * 设置 Agent 的任务类型覆盖模型
     */
    setTaskTypeOverride(agent: AgentType, taskType: TaskType, modelId: string): boolean;
    /**
     * 移除任务类型覆盖
     */
    removeTaskTypeOverride(agent: AgentType, taskType: TaskType): boolean;
    /**
     * 获取 Agent 的所有任务类型覆盖
     */
    getTaskTypeOverrides(agent: AgentType): Partial<Record<TaskType, string>> | undefined;
    /**
     * 设置 Agent 的备用模型
     */
    setFallbackModelForAgent(agent: AgentType, modelId: string): boolean;
    /**
     * 获取所有 Agent 的映射配置
     */
    getAllMappings(): AgentModelConfig[];
    /**
     * 获取所有 Agent 的模型 ID 映射
     */
    getMappingsAsRecord(): Record<AgentType, string>;
    /**
     * 批量设置映射
     */
    setMappings(mappings: Partial<Record<AgentType, string>>): void;
    /**
     * 重置为默认映射
     */
    reset(): void;
    /**
     * 验证所有映射是否有效
     */
    validateMappings(): {
        valid: boolean;
        errors: string[];
    };
    /**
     * 获取适合指定 Agent 的模型列表
     */
    getSuitableModelsForAgent(agent: AgentType): ModelDefinition[];
    /**
     * 估算所有 Agent 的总成本
     */
    estimateTotalCost(tokensPerAgent?: number): number;
    /**
     * 获取成本最优的映射建议
     */
    getCostOptimizedMappings(): Record<AgentType, string>;
    /**
     * 获取质量最优的映射建议
     */
    getQualityOptimizedMappings(): Record<AgentType, string>;
    /**
     * 导出配置
     */
    exportConfig(): AgentModelConfig[];
    /**
     * 导入配置
     */
    importConfig(configs: AgentModelConfig[]): void;
    /**
     * 获取 Coder Agent 的完整任务类型映射
     */
    getCoderTaskTypeMappings(): Record<TaskType, string>;
}
//# sourceMappingURL=AgentModelMapping.d.ts.map