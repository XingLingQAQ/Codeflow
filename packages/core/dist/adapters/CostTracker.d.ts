/**
 * CostTracker - 实时成本追踪和显示
 * 实现实时成本追踪、告警和会话汇总
 */
import { EventEmitter } from 'events';
/**
 * Token 使用信息
 */
export interface TokenUsage {
    inputTokens: number;
    outputTokens: number;
    totalTokens: number;
}
/**
 * 成本条目
 */
export interface CostEntry {
    id: string;
    modelId: string;
    provider: string;
    inputTokens: number;
    outputTokens: number;
    inputCost: number;
    outputCost: number;
    totalCost: number;
    timestamp: number;
    requestType?: string;
    metadata?: Record<string, unknown>;
}
/**
 * 会话成本汇总
 */
export interface SessionCostSummary {
    sessionId: string;
    startTime: number;
    endTime: number;
    duration: number;
    totalCost: number;
    totalInputTokens: number;
    totalOutputTokens: number;
    requestCount: number;
    byModel: Record<string, ModelCostBreakdown>;
    byRequestType: Record<string, number>;
    averageCostPerRequest: number;
    peakCostRequest: CostEntry | null;
}
/**
 * 模型成本明细
 */
export interface ModelCostBreakdown {
    modelId: string;
    provider: string;
    cost: number;
    inputTokens: number;
    outputTokens: number;
    requestCount: number;
    percentage: number;
}
/**
 * 成本告警
 */
export interface CostAlert {
    id: string;
    type: 'warning' | 'critical' | 'exceeded';
    message: string;
    currentCost: number;
    threshold: number;
    timestamp: number;
}
/**
 * 实时成本状态
 */
export interface RealTimeCostStatus {
    currentSessionCost: number;
    currentPeriodCost: number;
    budgetRemaining: number;
    budgetPercentUsed: number;
    lastUpdateTime: number;
    isOverBudget: boolean;
    alerts: CostAlert[];
}
/**
 * 成本追踪器配置
 */
export interface CostTrackerConfig {
    sessionBudget: number;
    periodBudget: number;
    period: 'hourly' | 'daily' | 'weekly' | 'monthly';
    warningThreshold: number;
    criticalThreshold: number;
    enableAlerts: boolean;
    enableRealTimeUpdates: boolean;
    updateInterval: number;
    currency: string;
}
/**
 * 模型定价信息
 */
export interface ModelPricing {
    modelId: string;
    provider: string;
    inputPricePer1k: number;
    outputPricePer1k: number;
}
/**
 * CostTracker - 成本追踪器
 */
export declare class CostTracker extends EventEmitter {
    private config;
    private pricing;
    private currentSessionId;
    private sessionStartTime;
    private sessionEntries;
    private periodEntries;
    private alerts;
    private updateTimer?;
    constructor(config?: Partial<CostTrackerConfig>);
    /**
     * 初始化定价
     */
    private initializePricing;
    /**
     * 开始会话
     */
    startSession(sessionId: string): void;
    /**
     * 结束会话
     */
    endSession(): SessionCostSummary;
    /**
     * 记录 Token 使用
     */
    recordUsage(modelId: string, usage: TokenUsage, requestType?: string, metadata?: Record<string, unknown>): CostEntry;
    /**
     * 获取实时状态
     */
    getRealTimeStatus(): RealTimeCostStatus;
    /**
     * 获取当前会话成本
     */
    getCurrentSessionCost(): number;
    /**
     * 获取当前周期成本
     */
    getCurrentPeriodCost(): number;
    /**
     * 获取会话条目
     */
    getSessionEntries(): CostEntry[];
    /**
     * 获取按模型分类的成本
     */
    getCostByModel(): Record<string, ModelCostBreakdown>;
    /**
     * 获取按请求类型分类的成本
     */
    getCostByRequestType(): Record<string, number>;
    /**
     * 生成会话汇总
     */
    private generateSessionSummary;
    /**
     * 检查告警
     */
    private checkAlerts;
    /**
     * 检查是否已有告警
     */
    private hasAlert;
    /**
     * 添加告警
     */
    private addAlert;
    /**
     * 清理过期的周期条目
     */
    private cleanupPeriodEntries;
    /**
     * 启动实时更新
     */
    private startRealTimeUpdates;
    /**
     * 添加模型定价
     */
    addPricing(pricing: ModelPricing): void;
    /**
     * 获取模型定价
     */
    getPricing(modelId: string): ModelPricing | undefined;
    /**
     * 获取所有定价
     */
    getAllPricing(): ModelPricing[];
    /**
     * 格式化成本
     */
    formatCost(cost: number): string;
    /**
     * 获取告警
     */
    getAlerts(): CostAlert[];
    /**
     * 清除告警
     */
    clearAlerts(): void;
    /**
     * 更新配置
     */
    updateConfig(config: Partial<CostTrackerConfig>): void;
    /**
     * 获取配置
     */
    getConfig(): CostTrackerConfig;
    /**
     * 重置
     */
    reset(): void;
}
//# sourceMappingURL=CostTracker.d.ts.map