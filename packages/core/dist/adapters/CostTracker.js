/**
 * CostTracker - 实时成本追踪和显示
 * 实现实时成本追踪、告警和会话汇总
 */
import { EventEmitter } from 'events';
const DEFAULT_CONFIG = {
    sessionBudget: 10,
    periodBudget: 100,
    period: 'daily',
    warningThreshold: 0.7,
    criticalThreshold: 0.9,
    enableAlerts: true,
    enableRealTimeUpdates: true,
    updateInterval: 1000,
    currency: 'USD',
};
/**
 * 默认模型定价
 */
const DEFAULT_MODEL_PRICING = [
    { modelId: 'claude-3-haiku', provider: 'anthropic', inputPricePer1k: 0.00025, outputPricePer1k: 0.00125 },
    { modelId: 'claude-3-5-sonnet', provider: 'anthropic', inputPricePer1k: 0.003, outputPricePer1k: 0.015 },
    { modelId: 'claude-3-opus', provider: 'anthropic', inputPricePer1k: 0.015, outputPricePer1k: 0.075 },
    { modelId: 'claude-opus-4', provider: 'anthropic', inputPricePer1k: 0.015, outputPricePer1k: 0.075 },
    { modelId: 'gemini-flash', provider: 'google', inputPricePer1k: 0.000075, outputPricePer1k: 0.0003 },
    { modelId: 'gemini-pro', provider: 'google', inputPricePer1k: 0.00125, outputPricePer1k: 0.005 },
    { modelId: 'gpt-4o-mini', provider: 'openai', inputPricePer1k: 0.00015, outputPricePer1k: 0.0006 },
    { modelId: 'gpt-4o', provider: 'openai', inputPricePer1k: 0.005, outputPricePer1k: 0.015 },
];
/**
 * CostTracker - 成本追踪器
 */
export class CostTracker extends EventEmitter {
    constructor(config = {}) {
        super();
        this.pricing = new Map();
        this.currentSessionId = '';
        this.sessionStartTime = 0;
        this.sessionEntries = [];
        this.periodEntries = [];
        this.alerts = [];
        this.config = { ...DEFAULT_CONFIG, ...config };
        this.initializePricing();
    }
    /**
     * 初始化定价
     */
    initializePricing() {
        for (const pricing of DEFAULT_MODEL_PRICING) {
            this.pricing.set(pricing.modelId, pricing);
        }
    }
    /**
     * 开始会话
     */
    startSession(sessionId) {
        this.currentSessionId = sessionId;
        this.sessionStartTime = Date.now();
        this.sessionEntries = [];
        this.alerts = [];
        if (this.config.enableRealTimeUpdates) {
            this.startRealTimeUpdates();
        }
        this.emit('session:started', { sessionId, startTime: this.sessionStartTime });
    }
    /**
     * 结束会话
     */
    endSession() {
        if (this.updateTimer) {
            clearInterval(this.updateTimer);
            this.updateTimer = undefined;
        }
        const summary = this.generateSessionSummary();
        this.emit('session:ended', summary);
        return summary;
    }
    /**
     * 记录 Token 使用
     */
    recordUsage(modelId, usage, requestType, metadata) {
        const pricing = this.pricing.get(modelId);
        const provider = pricing?.provider || 'unknown';
        const inputCost = pricing
            ? (usage.inputTokens / 1000) * pricing.inputPricePer1k
            : 0;
        const outputCost = pricing
            ? (usage.outputTokens / 1000) * pricing.outputPricePer1k
            : 0;
        const entry = {
            id: `cost_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`,
            modelId,
            provider,
            inputTokens: usage.inputTokens,
            outputTokens: usage.outputTokens,
            inputCost,
            outputCost,
            totalCost: inputCost + outputCost,
            timestamp: Date.now(),
            requestType,
            metadata,
        };
        this.sessionEntries.push(entry);
        this.periodEntries.push(entry);
        // 清理过期的周期条目
        this.cleanupPeriodEntries();
        // 检查告警
        this.checkAlerts();
        this.emit('cost:recorded', entry);
        this.emit('cost:updated', this.getRealTimeStatus());
        return entry;
    }
    /**
     * 获取实时状态
     */
    getRealTimeStatus() {
        const currentSessionCost = this.getCurrentSessionCost();
        const currentPeriodCost = this.getCurrentPeriodCost();
        const budgetRemaining = Math.max(0, this.config.periodBudget - currentPeriodCost);
        const budgetPercentUsed = (currentPeriodCost / this.config.periodBudget) * 100;
        return {
            currentSessionCost,
            currentPeriodCost,
            budgetRemaining,
            budgetPercentUsed,
            lastUpdateTime: Date.now(),
            isOverBudget: currentPeriodCost > this.config.periodBudget,
            alerts: [...this.alerts],
        };
    }
    /**
     * 获取当前会话成本
     */
    getCurrentSessionCost() {
        return this.sessionEntries.reduce((sum, e) => sum + e.totalCost, 0);
    }
    /**
     * 获取当前周期成本
     */
    getCurrentPeriodCost() {
        return this.periodEntries.reduce((sum, e) => sum + e.totalCost, 0);
    }
    /**
     * 获取会话条目
     */
    getSessionEntries() {
        return [...this.sessionEntries];
    }
    /**
     * 获取按模型分类的成本
     */
    getCostByModel() {
        const totalCost = this.getCurrentSessionCost();
        const byModel = {};
        for (const entry of this.sessionEntries) {
            if (!byModel[entry.modelId]) {
                byModel[entry.modelId] = {
                    modelId: entry.modelId,
                    provider: entry.provider,
                    cost: 0,
                    inputTokens: 0,
                    outputTokens: 0,
                    requestCount: 0,
                    percentage: 0,
                };
            }
            byModel[entry.modelId].cost += entry.totalCost;
            byModel[entry.modelId].inputTokens += entry.inputTokens;
            byModel[entry.modelId].outputTokens += entry.outputTokens;
            byModel[entry.modelId].requestCount++;
        }
        // 计算百分比
        for (const model of Object.values(byModel)) {
            model.percentage = totalCost > 0 ? (model.cost / totalCost) * 100 : 0;
        }
        return byModel;
    }
    /**
     * 获取按请求类型分类的成本
     */
    getCostByRequestType() {
        const byType = {};
        for (const entry of this.sessionEntries) {
            const type = entry.requestType || 'unknown';
            byType[type] = (byType[type] || 0) + entry.totalCost;
        }
        return byType;
    }
    /**
     * 生成会话汇总
     */
    generateSessionSummary() {
        const endTime = Date.now();
        const totalCost = this.getCurrentSessionCost();
        const totalInputTokens = this.sessionEntries.reduce((sum, e) => sum + e.inputTokens, 0);
        const totalOutputTokens = this.sessionEntries.reduce((sum, e) => sum + e.outputTokens, 0);
        // 找出成本最高的请求
        let peakCostRequest = null;
        for (const entry of this.sessionEntries) {
            if (!peakCostRequest || entry.totalCost > peakCostRequest.totalCost) {
                peakCostRequest = entry;
            }
        }
        return {
            sessionId: this.currentSessionId,
            startTime: this.sessionStartTime,
            endTime,
            duration: endTime - this.sessionStartTime,
            totalCost,
            totalInputTokens,
            totalOutputTokens,
            requestCount: this.sessionEntries.length,
            byModel: this.getCostByModel(),
            byRequestType: this.getCostByRequestType(),
            averageCostPerRequest: this.sessionEntries.length > 0
                ? totalCost / this.sessionEntries.length
                : 0,
            peakCostRequest,
        };
    }
    /**
     * 检查告警
     */
    checkAlerts() {
        if (!this.config.enableAlerts)
            return;
        const sessionCost = this.getCurrentSessionCost();
        const periodCost = this.getCurrentPeriodCost();
        // 会话预算告警
        const sessionPercentUsed = sessionCost / this.config.sessionBudget;
        if (sessionPercentUsed >= 1 && !this.hasAlert('session_exceeded')) {
            this.addAlert('exceeded', 'Session budget exceeded', sessionCost, this.config.sessionBudget);
        }
        else if (sessionPercentUsed >= this.config.criticalThreshold && !this.hasAlert('session_critical')) {
            this.addAlert('critical', 'Session cost approaching budget limit', sessionCost, this.config.sessionBudget);
        }
        else if (sessionPercentUsed >= this.config.warningThreshold && !this.hasAlert('session_warning')) {
            this.addAlert('warning', 'Session cost warning', sessionCost, this.config.sessionBudget);
        }
        // 周期预算告警
        const periodPercentUsed = periodCost / this.config.periodBudget;
        if (periodPercentUsed >= 1 && !this.hasAlert('period_exceeded')) {
            this.addAlert('exceeded', `${this.config.period} budget exceeded`, periodCost, this.config.periodBudget);
        }
        else if (periodPercentUsed >= this.config.criticalThreshold && !this.hasAlert('period_critical')) {
            this.addAlert('critical', `${this.config.period} cost approaching budget limit`, periodCost, this.config.periodBudget);
        }
        else if (periodPercentUsed >= this.config.warningThreshold && !this.hasAlert('period_warning')) {
            this.addAlert('warning', `${this.config.period} cost warning`, periodCost, this.config.periodBudget);
        }
    }
    /**
     * 检查是否已有告警
     */
    hasAlert(prefix) {
        return this.alerts.some(a => a.id.startsWith(prefix));
    }
    /**
     * 添加告警
     */
    addAlert(type, message, currentCost, threshold) {
        const alert = {
            id: `${type}_${Date.now()}`,
            type,
            message,
            currentCost,
            threshold,
            timestamp: Date.now(),
        };
        this.alerts.push(alert);
        this.emit('alert', alert);
    }
    /**
     * 清理过期的周期条目
     */
    cleanupPeriodEntries() {
        const now = Date.now();
        let cutoff;
        switch (this.config.period) {
            case 'hourly':
                cutoff = now - 60 * 60 * 1000;
                break;
            case 'daily':
                cutoff = now - 24 * 60 * 60 * 1000;
                break;
            case 'weekly':
                cutoff = now - 7 * 24 * 60 * 60 * 1000;
                break;
            case 'monthly':
                cutoff = now - 30 * 24 * 60 * 60 * 1000;
                break;
        }
        this.periodEntries = this.periodEntries.filter(e => e.timestamp >= cutoff);
    }
    /**
     * 启动实时更新
     */
    startRealTimeUpdates() {
        if (this.updateTimer) {
            clearInterval(this.updateTimer);
        }
        this.updateTimer = setInterval(() => {
            this.emit('cost:tick', this.getRealTimeStatus());
        }, this.config.updateInterval);
    }
    /**
     * 添加模型定价
     */
    addPricing(pricing) {
        this.pricing.set(pricing.modelId, pricing);
        this.emit('pricing:added', pricing);
    }
    /**
     * 获取模型定价
     */
    getPricing(modelId) {
        return this.pricing.get(modelId);
    }
    /**
     * 获取所有定价
     */
    getAllPricing() {
        return Array.from(this.pricing.values());
    }
    /**
     * 格式化成本
     */
    formatCost(cost) {
        return `${this.config.currency} ${cost.toFixed(4)}`;
    }
    /**
     * 获取告警
     */
    getAlerts() {
        return [...this.alerts];
    }
    /**
     * 清除告警
     */
    clearAlerts() {
        this.alerts = [];
        this.emit('alerts:cleared');
    }
    /**
     * 更新配置
     */
    updateConfig(config) {
        this.config = { ...this.config, ...config };
        // 重启实时更新（如果配置改变）
        if (this.config.enableRealTimeUpdates && this.currentSessionId) {
            this.startRealTimeUpdates();
        }
        else if (!this.config.enableRealTimeUpdates && this.updateTimer) {
            clearInterval(this.updateTimer);
            this.updateTimer = undefined;
        }
        this.emit('config:updated', this.config);
    }
    /**
     * 获取配置
     */
    getConfig() {
        return { ...this.config };
    }
    /**
     * 重置
     */
    reset() {
        if (this.updateTimer) {
            clearInterval(this.updateTimer);
            this.updateTimer = undefined;
        }
        this.currentSessionId = '';
        this.sessionStartTime = 0;
        this.sessionEntries = [];
        this.periodEntries = [];
        this.alerts = [];
        this.emit('tracker:reset');
    }
}
//# sourceMappingURL=CostTracker.js.map