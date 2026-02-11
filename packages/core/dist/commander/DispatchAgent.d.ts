/**
 * DispatchAgent - 轻量路由调度 Agent
 * 实现 Trellis 风格的 Dispatch Agent，纯路由不读规范
 */
import { EventEmitter } from 'events';
/**
 * 任务类型
 */
export type DispatchTaskType = 'frontend' | 'backend' | 'fullstack' | 'api' | 'database' | 'testing' | 'documentation' | 'refactoring' | 'bugfix' | 'feature' | 'research' | 'review' | 'unknown';
/**
 * Agent 类型
 */
export type DispatchAgentType = 'main' | 'coder' | 'researcher' | 'reviewer' | 'documenter' | 'tester';
/**
 * 模型层级
 */
export type ModelTier = 'low' | 'medium' | 'high' | 'premium';
/**
 * 分类结果
 */
export interface ClassificationResult {
    taskType: DispatchTaskType;
    confidence: number;
    keywords: string[];
    complexity: 'simple' | 'moderate' | 'complex';
    estimatedTokens: number;
}
/**
 * 模型选择结果
 */
export interface ModelSelectionResult {
    modelId: string;
    tier: ModelTier;
    reason: string;
    fallback?: string;
}
/**
 * Agent 选择结果
 */
export interface AgentSelectionResult {
    agentType: DispatchAgentType;
    reason: string;
    capabilities: string[];
}
/**
 * 规范选择结果
 */
export interface SpecSelectionResult {
    specIds: string[];
    domains: string[];
    priority: 'critical' | 'high' | 'medium' | 'low';
    estimatedTokens: number;
}
/**
 * 路由决策
 */
export interface RoutingDecision {
    id: string;
    classification: ClassificationResult;
    agent: AgentSelectionResult;
    model: ModelSelectionResult;
    specs: SpecSelectionResult;
    timestamp: number;
    latency: number;
}
/**
 * Dispatch Agent 配置
 */
export interface DispatchAgentConfig {
    defaultModel: string;
    maxLatency: number;
    enableCaching: boolean;
    cacheTimeout: number;
    modelTierMapping: Record<ModelTier, string[]>;
    taskTypeAgentMapping: Record<DispatchTaskType, DispatchAgentType>;
    taskTypeSpecDomains: Record<DispatchTaskType, string[]>;
}
/**
 * TaskClassifier - 任务分类器
 */
export declare class TaskClassifier extends EventEmitter {
    /**
     * 分类任务
     */
    classify(input: string): ClassificationResult;
    /**
     * 评估复杂度
     */
    private assessComplexity;
}
/**
 * ModelSelector - 模型选择器
 */
export declare class ModelSelector extends EventEmitter {
    private config;
    constructor(config?: Partial<DispatchAgentConfig>);
    /**
     * 选择模型
     */
    select(classification: ClassificationResult): ModelSelectionResult;
    /**
     * 确定模型层级
     */
    private determineTier;
    /**
     * 生成选择原因
     */
    private generateReason;
    /**
     * 更新配置
     */
    updateConfig(config: Partial<DispatchAgentConfig>): void;
}
/**
 * AgentSelector - Agent 选择器
 */
export declare class AgentSelector extends EventEmitter {
    private config;
    constructor(config?: Partial<DispatchAgentConfig>);
    /**
     * 选择 Agent
     */
    select(classification: ClassificationResult): AgentSelectionResult;
    /**
     * 获取 Agent 能力
     */
    private getAgentCapabilities;
    /**
     * 更新配置
     */
    updateConfig(config: Partial<DispatchAgentConfig>): void;
}
/**
 * SpecSelector - 规范选择器
 */
export declare class SpecSelector extends EventEmitter {
    private config;
    constructor(config?: Partial<DispatchAgentConfig>);
    /**
     * 选择规范
     */
    select(classification: ClassificationResult): SpecSelectionResult;
    /**
     * 确定优先级
     */
    private determinePriority;
    /**
     * 更新配置
     */
    updateConfig(config: Partial<DispatchAgentConfig>): void;
}
/**
 * DispatchAgent - 调度 Agent
 */
export declare class DispatchAgent extends EventEmitter {
    private config;
    private classifier;
    private modelSelector;
    private agentSelector;
    private specSelector;
    private cache;
    private decisionHistory;
    constructor(config?: Partial<DispatchAgentConfig>);
    /**
     * 路由请求
     */
    route(input: string): Promise<RoutingDecision>;
    /**
     * 获取缓存的决策
     */
    private getCachedDecision;
    /**
     * 缓存决策
     */
    private cacheDecision;
    /**
     * 生成缓存键
     */
    private generateCacheKey;
    /**
     * 清除缓存
     */
    clearCache(): void;
    /**
     * 获取决策历史
     */
    getDecisionHistory(): RoutingDecision[];
    /**
     * 获取统计信息
     */
    getStatistics(): {
        totalDecisions: number;
        averageLatency: number;
        cacheHitRate: number;
        byTaskType: Record<DispatchTaskType, number>;
        byAgent: Record<DispatchAgentType, number>;
    };
    /**
     * 更新配置
     */
    updateConfig(config: Partial<DispatchAgentConfig>): void;
    /**
     * 获取配置
     */
    getConfig(): DispatchAgentConfig;
    /**
     * 重置
     */
    reset(): void;
}
//# sourceMappingURL=DispatchAgent.d.ts.map