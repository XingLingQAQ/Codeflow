import { EventEmitter } from 'events';
import { ModelRegistry, ModelDefinition, ModelCapability } from './ModelRegistry.js';
/**
 * Plan 模式阶段类型
 */
export type PlanPhase = 'vision' | 'constraints' | 'architecture' | 'research' | 'explore' | 'review' | 'implement' | 'qa';
/**
 * 阶段模型映射配置
 */
export interface PhaseModelConfig {
    phase: PlanPhase;
    modelId: string;
    fallbackModelId?: string;
    requiredCapabilities?: ModelCapability[];
    description?: string;
}
/**
 * 阶段模型映射事件
 */
export interface PhaseModelMappingEvents {
    'mapping:changed': (phase: PlanPhase, modelId: string) => void;
    'mapping:reset': () => void;
}
/**
 * 所有阶段列表
 */
export declare const ALL_PHASES: PlanPhase[];
/**
 * 阶段模型映射管理器
 * 管理 Plan 模式各阶段使用的模型配置
 */
export declare class PhaseModelMapping extends EventEmitter {
    private mappings;
    private modelRegistry;
    constructor(modelRegistry: ModelRegistry);
    /**
     * 加载默认映射
     */
    private loadDefaultMappings;
    /**
     * 获取阶段的模型配置
     */
    getPhaseConfig(phase: PlanPhase): PhaseModelConfig | undefined;
    /**
     * 获取阶段使用的模型 ID
     */
    getModelForPhase(phase: PlanPhase): string | undefined;
    /**
     * 获取阶段使用的模型定义
     */
    getModelDefinitionForPhase(phase: PlanPhase): ModelDefinition | undefined;
    /**
     * 设置阶段的模型
     */
    setModelForPhase(phase: PlanPhase, modelId: string): boolean;
    /**
     * 设置阶段的备用模型
     */
    setFallbackModelForPhase(phase: PlanPhase, modelId: string): boolean;
    /**
     * 获取所有阶段的映射配置
     */
    getAllMappings(): PhaseModelConfig[];
    /**
     * 获取所有阶段的模型 ID 映射
     */
    getMappingsAsRecord(): Record<PlanPhase, string>;
    /**
     * 批量设置映射
     */
    setMappings(mappings: Partial<Record<PlanPhase, string>>): void;
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
     * 获取适合指定阶段的模型列表
     */
    getSuitableModelsForPhase(phase: PlanPhase): ModelDefinition[];
    /**
     * 估算所有阶段的总成本
     */
    estimateTotalCost(tokensPerPhase?: number): number;
    /**
     * 获取成本最优的映射建议
     */
    getCostOptimizedMappings(): Record<PlanPhase, string>;
    /**
     * 获取质量最优的映射建议
     */
    getQualityOptimizedMappings(): Record<PlanPhase, string>;
    /**
     * 导出配置
     */
    exportConfig(): PhaseModelConfig[];
    /**
     * 导入配置
     */
    importConfig(configs: PhaseModelConfig[]): void;
}
//# sourceMappingURL=PhaseModelMapping.d.ts.map