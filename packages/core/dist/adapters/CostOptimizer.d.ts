/**
 * CostOptimizer - 成本优化模块
 * 实现基于任务类型的模型路由规则，优化成本
 */
import { EventEmitter } from 'events';
/**
 * 任务类型（用于成本优化）
 */
export type CostTaskType = 'decision' | 'coding' | 'research' | 'review' | 'documentation' | 'simple' | 'complex' | 'unknown';
/**
 * 模型成本信息
 */
export interface ModelCostInfo {
    modelId: string;
    provider: string;
    inputCostPer1k: number;
    outputCostPer1k: number;
    tier: 'low' | 'medium' | 'high' | 'premium';
    capabilities: string[];
}
/**
 * 使用记录
 */
export interface UsageRecord {
    id: string;
    modelId: string;
    taskType: CostTaskType;
    inputTokens: number;
    outputTokens: number;
    cost: number;
    timestamp: number;
    sessionId?: string;
}
/**
 * 成本统计
 */
export interface CostStatistics {
    totalCost: number;
    totalInputTokens: number;
    totalOutputTokens: number;
    byModel: Record<string, {
        cost: number;
        tokens: number;
    }>;
    byTaskType: Record<CostTaskType, {
        cost: number;
        tokens: number;
    }>;
    averageCostPerRequest: number;
    estimatedSavings: number;
}
/**
 * 路由规则
 */
export interface RoutingRule {
    id: string;
    taskType: CostTaskType;
    preferredModels: string[];
    fallbackModels: string[];
    maxCostPerRequest?: number;
    priority: number;
    enabled: boolean;
}
/**
 * 路由结果
 */
export interface RoutingResult {
    modelId: string;
    rule: RoutingRule;
    estimatedCost: number;
    reason: string;
}
/**
 * 成本优化器配置
 */
export interface CostOptimizerConfig {
    budgetLimit: number;
    budgetPeriod: 'daily' | 'weekly' | 'monthly';
    alertThreshold: number;
    enableTracking: boolean;
    defaultModel: string;
}
/**
 * TaskTypeRouter - 任务类型路由器
 */
export declare class TaskTypeRouter extends EventEmitter {
    private rules;
    private modelCosts;
    constructor();
    /**
     * 初始化默认配置
     */
    private initializeDefaults;
    /**
     * 路由任务到模型
     */
    route(taskType: CostTaskType, availableModels?: string[]): RoutingResult;
    /**
     * 添加路由规则
     */
    addRule(rule: RoutingRule): void;
    /**
     * 移除路由规则
     */
    removeRule(taskType: CostTaskType): boolean;
    /**
     * 获取所有规则
     */
    getRules(): RoutingRule[];
    /**
     * 添加模型成本信息
     */
    addModelCost(cost: ModelCostInfo): void;
    /**
     * 获取模型成本信息
     */
    getModelCost(modelId: string): ModelCostInfo | undefined;
    /**
     * 获取所有模型成本
     */
    getAllModelCosts(): ModelCostInfo[];
}
/**
 * UsageTracker - 使用量追踪器
 */
export declare class UsageTracker extends EventEmitter {
    private records;
    private modelCosts;
    constructor();
    /**
     * 记录使用
     */
    record(modelId: string, taskType: CostTaskType, inputTokens: number, outputTokens: number, sessionId?: string): UsageRecord;
    /**
     * 获取统计信息
     */
    getStatistics(startTime?: number, endTime?: number): CostStatistics;
    /**
     * 获取使用记录
     */
    getRecords(limit?: number): UsageRecord[];
    /**
     * 清除记录
     */
    clearRecords(): void;
    /**
     * 获取当前周期成本
     */
    getCurrentPeriodCost(period: 'daily' | 'weekly' | 'monthly'): number;
}
/**
 * CostOptimizer - 成本优化器
 */
export declare class CostOptimizer extends EventEmitter {
    private config;
    private router;
    private tracker;
    constructor(config?: Partial<CostOptimizerConfig>);
    /**
     * 选择最优模型
     */
    selectModel(taskType: CostTaskType, availableModels?: string[]): RoutingResult;
    /**
     * 记录使用
     */
    recordUsage(modelId: string, taskType: CostTaskType, inputTokens: number, outputTokens: number, sessionId?: string): UsageRecord;
    /**
     * 获取成本统计
     */
    getStatistics(startTime?: number, endTime?: number): CostStatistics;
    /**
     * 获取节省百分比
     */
    getSavingsPercentage(): number;
    /**
     * 获取预算状态
     */
    getBudgetStatus(): {
        currentCost: number;
        budgetLimit: number;
        period: string;
        percentUsed: number;
        remaining: number;
    };
    /**
     * 添加路由规则
     */
    addRoutingRule(rule: RoutingRule): void;
    /**
     * 获取路由规则
     */
    getRoutingRules(): RoutingRule[];
    /**
     * 添加模型成本
     */
    addModelCost(cost: ModelCostInfo): void;
    /**
     * 获取模型成本
     */
    getModelCost(modelId: string): ModelCostInfo | undefined;
    /**
     * 获取所有模型成本
     */
    getAllModelCosts(): ModelCostInfo[];
    /**
     * 获取使用记录
     */
    getUsageRecords(limit?: number): UsageRecord[];
    /**
     * 更新配置
     */
    updateConfig(config: Partial<CostOptimizerConfig>): void;
    /**
     * 获取配置
     */
    getConfig(): CostOptimizerConfig;
    /**
     * 重置
     */
    reset(): void;
}
//# sourceMappingURL=CostOptimizer.d.ts.map