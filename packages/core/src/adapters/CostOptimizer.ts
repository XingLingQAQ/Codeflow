/**
 * CostOptimizer - 成本优化模块
 * 实现基于任务类型的模型路由规则，优化成本
 */

import { EventEmitter } from 'events';

/**
 * 任务类型（用于成本优化）
 */
export type CostTaskType =
  | 'decision'      // 决策类任务
  | 'coding'        // 代码生成
  | 'research'      // 研究类
  | 'review'        // 代码审查
  | 'documentation' // 文档生成
  | 'simple'        // 简单任务
  | 'complex'       // 复杂任务
  | 'unknown';

/**
 * 模型成本信息
 */
export interface ModelCostInfo {
  modelId: string;
  provider: string;
  inputCostPer1k: number;   // 每 1000 tokens 输入成本（美元）
  outputCostPer1k: number;  // 每 1000 tokens 输出成本（美元）
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
  byModel: Record<string, { cost: number; tokens: number }>;
  byTaskType: Record<CostTaskType, { cost: number; tokens: number }>;
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

const DEFAULT_CONFIG: CostOptimizerConfig = {
  budgetLimit: 100,
  budgetPeriod: 'daily',
  alertThreshold: 0.8,
  enableTracking: true,
  defaultModel: 'claude-3-haiku',
};

/**
 * 默认模型成本信息
 */
const DEFAULT_MODEL_COSTS: ModelCostInfo[] = [
  {
    modelId: 'claude-3-haiku',
    provider: 'anthropic',
    inputCostPer1k: 0.00025,
    outputCostPer1k: 0.00125,
    tier: 'low',
    capabilities: ['coding', 'simple', 'documentation'],
  },
  {
    modelId: 'claude-3-5-sonnet',
    provider: 'anthropic',
    inputCostPer1k: 0.003,
    outputCostPer1k: 0.015,
    tier: 'medium',
    capabilities: ['coding', 'research', 'review', 'complex'],
  },
  {
    modelId: 'claude-3-opus',
    provider: 'anthropic',
    inputCostPer1k: 0.015,
    outputCostPer1k: 0.075,
    tier: 'high',
    capabilities: ['decision', 'complex', 'review'],
  },
  {
    modelId: 'claude-opus-4',
    provider: 'anthropic',
    inputCostPer1k: 0.015,
    outputCostPer1k: 0.075,
    tier: 'premium',
    capabilities: ['decision', 'complex', 'review', 'research'],
  },
  {
    modelId: 'gemini-flash',
    provider: 'google',
    inputCostPer1k: 0.000075,
    outputCostPer1k: 0.0003,
    tier: 'low',
    capabilities: ['coding', 'simple', 'documentation'],
  },
  {
    modelId: 'gemini-pro',
    provider: 'google',
    inputCostPer1k: 0.00125,
    outputCostPer1k: 0.005,
    tier: 'medium',
    capabilities: ['coding', 'research', 'review'],
  },
  {
    modelId: 'gpt-4o-mini',
    provider: 'openai',
    inputCostPer1k: 0.00015,
    outputCostPer1k: 0.0006,
    tier: 'low',
    capabilities: ['coding', 'simple', 'documentation'],
  },
  {
    modelId: 'gpt-4o',
    provider: 'openai',
    inputCostPer1k: 0.005,
    outputCostPer1k: 0.015,
    tier: 'high',
    capabilities: ['decision', 'complex', 'review', 'research'],
  },
];

/**
 * 默认路由规则
 */
const DEFAULT_ROUTING_RULES: RoutingRule[] = [
  {
    id: 'rule_decision',
    taskType: 'decision',
    preferredModels: ['claude-opus-4', 'claude-3-opus', 'gpt-4o'],
    fallbackModels: ['claude-3-5-sonnet'],
    priority: 1,
    enabled: true,
  },
  {
    id: 'rule_coding',
    taskType: 'coding',
    preferredModels: ['claude-3-haiku', 'gemini-flash', 'gpt-4o-mini'],
    fallbackModels: ['claude-3-5-sonnet'],
    priority: 2,
    enabled: true,
  },
  {
    id: 'rule_research',
    taskType: 'research',
    preferredModels: ['claude-3-5-sonnet', 'gemini-pro'],
    fallbackModels: ['claude-3-opus'],
    priority: 3,
    enabled: true,
  },
  {
    id: 'rule_review',
    taskType: 'review',
    preferredModels: ['claude-3-opus', 'gpt-4o'],
    fallbackModels: ['claude-3-5-sonnet'],
    priority: 4,
    enabled: true,
  },
  {
    id: 'rule_documentation',
    taskType: 'documentation',
    preferredModels: ['claude-3-haiku', 'gemini-flash'],
    fallbackModels: ['claude-3-5-sonnet'],
    priority: 5,
    enabled: true,
  },
  {
    id: 'rule_simple',
    taskType: 'simple',
    preferredModels: ['claude-3-haiku', 'gemini-flash', 'gpt-4o-mini'],
    fallbackModels: [],
    priority: 6,
    enabled: true,
  },
  {
    id: 'rule_complex',
    taskType: 'complex',
    preferredModels: ['claude-3-opus', 'claude-opus-4', 'gpt-4o'],
    fallbackModels: ['claude-3-5-sonnet'],
    priority: 7,
    enabled: true,
  },
  {
    id: 'rule_unknown',
    taskType: 'unknown',
    preferredModels: ['claude-3-5-sonnet'],
    fallbackModels: ['claude-3-haiku'],
    priority: 100,
    enabled: true,
  },
];

/**
 * TaskTypeRouter - 任务类型路由器
 */
export class TaskTypeRouter extends EventEmitter {
  private rules: Map<CostTaskType, RoutingRule> = new Map();
  private modelCosts: Map<string, ModelCostInfo> = new Map();

  constructor() {
    super();
    this.initializeDefaults();
  }

  /**
   * 初始化默认配置
   */
  private initializeDefaults(): void {
    for (const rule of DEFAULT_ROUTING_RULES) {
      this.rules.set(rule.taskType, rule);
    }
    for (const cost of DEFAULT_MODEL_COSTS) {
      this.modelCosts.set(cost.modelId, cost);
    }
  }

  /**
   * 路由任务到模型
   */
  route(taskType: CostTaskType, availableModels?: string[]): RoutingResult {
    const rule = this.rules.get(taskType) || this.rules.get('unknown')!;

    // 找到可用的首选模型
    let selectedModel: string | undefined;
    let reason: string;

    for (const modelId of rule.preferredModels) {
      if (!availableModels || availableModels.includes(modelId)) {
        if (this.modelCosts.has(modelId)) {
          selectedModel = modelId;
          reason = `Preferred model for ${taskType} tasks`;
          break;
        }
      }
    }

    // 如果没有首选模型，使用回退模型
    if (!selectedModel) {
      for (const modelId of rule.fallbackModels) {
        if (!availableModels || availableModels.includes(modelId)) {
          if (this.modelCosts.has(modelId)) {
            selectedModel = modelId;
            reason = `Fallback model for ${taskType} tasks`;
            break;
          }
        }
      }
    }

    // 如果还是没有，使用第一个可用模型
    if (!selectedModel) {
      selectedModel = availableModels?.[0] || 'claude-3-haiku';
      reason = 'Default model (no preferred/fallback available)';
    }

    const costInfo = this.modelCosts.get(selectedModel);
    const estimatedCost = costInfo
      ? (costInfo.inputCostPer1k + costInfo.outputCostPer1k) * 2 // 假设 2k tokens
      : 0;

    const result: RoutingResult = {
      modelId: selectedModel,
      rule,
      estimatedCost,
      reason,
    };

    this.emit('route:complete', result);

    return result;
  }

  /**
   * 添加路由规则
   */
  addRule(rule: RoutingRule): void {
    this.rules.set(rule.taskType, rule);
    this.emit('rule:added', rule);
  }

  /**
   * 移除路由规则
   */
  removeRule(taskType: CostTaskType): boolean {
    const removed = this.rules.delete(taskType);
    if (removed) {
      this.emit('rule:removed', taskType);
    }
    return removed;
  }

  /**
   * 获取所有规则
   */
  getRules(): RoutingRule[] {
    return Array.from(this.rules.values());
  }

  /**
   * 添加模型成本信息
   */
  addModelCost(cost: ModelCostInfo): void {
    this.modelCosts.set(cost.modelId, cost);
    this.emit('model:added', cost);
  }

  /**
   * 获取模型成本信息
   */
  getModelCost(modelId: string): ModelCostInfo | undefined {
    return this.modelCosts.get(modelId);
  }

  /**
   * 获取所有模型成本
   */
  getAllModelCosts(): ModelCostInfo[] {
    return Array.from(this.modelCosts.values());
  }
}

/**
 * UsageTracker - 使用量追踪器
 */
export class UsageTracker extends EventEmitter {
  private records: UsageRecord[] = [];
  private modelCosts: Map<string, ModelCostInfo> = new Map();

  constructor() {
    super();
    for (const cost of DEFAULT_MODEL_COSTS) {
      this.modelCosts.set(cost.modelId, cost);
    }
  }

  /**
   * 记录使用
   */
  record(
    modelId: string,
    taskType: CostTaskType,
    inputTokens: number,
    outputTokens: number,
    sessionId?: string
  ): UsageRecord {
    const costInfo = this.modelCosts.get(modelId);
    const cost = costInfo
      ? (inputTokens / 1000) * costInfo.inputCostPer1k +
        (outputTokens / 1000) * costInfo.outputCostPer1k
      : 0;

    const record: UsageRecord = {
      id: `usage_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`,
      modelId,
      taskType,
      inputTokens,
      outputTokens,
      cost,
      timestamp: Date.now(),
      sessionId,
    };

    this.records.push(record);
    this.emit('usage:recorded', record);

    return record;
  }

  /**
   * 获取统计信息
   */
  getStatistics(startTime?: number, endTime?: number): CostStatistics {
    let filteredRecords = this.records;

    if (startTime) {
      filteredRecords = filteredRecords.filter(r => r.timestamp >= startTime);
    }
    if (endTime) {
      filteredRecords = filteredRecords.filter(r => r.timestamp <= endTime);
    }

    const stats: CostStatistics = {
      totalCost: 0,
      totalInputTokens: 0,
      totalOutputTokens: 0,
      byModel: {},
      byTaskType: {} as Record<CostTaskType, { cost: number; tokens: number }>,
      averageCostPerRequest: 0,
      estimatedSavings: 0,
    };

    for (const record of filteredRecords) {
      stats.totalCost += record.cost;
      stats.totalInputTokens += record.inputTokens;
      stats.totalOutputTokens += record.outputTokens;

      // 按模型统计
      if (!stats.byModel[record.modelId]) {
        stats.byModel[record.modelId] = { cost: 0, tokens: 0 };
      }
      stats.byModel[record.modelId].cost += record.cost;
      stats.byModel[record.modelId].tokens += record.inputTokens + record.outputTokens;

      // 按任务类型统计
      if (!stats.byTaskType[record.taskType]) {
        stats.byTaskType[record.taskType] = { cost: 0, tokens: 0 };
      }
      stats.byTaskType[record.taskType].cost += record.cost;
      stats.byTaskType[record.taskType].tokens += record.inputTokens + record.outputTokens;
    }

    stats.averageCostPerRequest = filteredRecords.length > 0
      ? stats.totalCost / filteredRecords.length
      : 0;

    // 估算节省（与全部使用高端模型相比）
    const premiumCost = this.modelCosts.get('claude-opus-4');
    if (premiumCost) {
      const premiumTotalCost =
        (stats.totalInputTokens / 1000) * premiumCost.inputCostPer1k +
        (stats.totalOutputTokens / 1000) * premiumCost.outputCostPer1k;
      stats.estimatedSavings = premiumTotalCost - stats.totalCost;
    }

    return stats;
  }

  /**
   * 获取使用记录
   */
  getRecords(limit?: number): UsageRecord[] {
    const records = [...this.records].reverse();
    return limit ? records.slice(0, limit) : records;
  }

  /**
   * 清除记录
   */
  clearRecords(): void {
    this.records = [];
    this.emit('records:cleared');
  }

  /**
   * 获取当前周期成本
   */
  getCurrentPeriodCost(period: 'daily' | 'weekly' | 'monthly'): number {
    const now = Date.now();
    let startTime: number;

    switch (period) {
      case 'daily':
        startTime = now - 24 * 60 * 60 * 1000;
        break;
      case 'weekly':
        startTime = now - 7 * 24 * 60 * 60 * 1000;
        break;
      case 'monthly':
        startTime = now - 30 * 24 * 60 * 60 * 1000;
        break;
    }

    return this.records
      .filter(r => r.timestamp >= startTime)
      .reduce((sum, r) => sum + r.cost, 0);
  }
}

/**
 * CostOptimizer - 成本优化器
 */
export class CostOptimizer extends EventEmitter {
  private config: CostOptimizerConfig;
  private router: TaskTypeRouter;
  private tracker: UsageTracker;

  constructor(config: Partial<CostOptimizerConfig> = {}) {
    super();
    this.config = { ...DEFAULT_CONFIG, ...config };
    this.router = new TaskTypeRouter();
    this.tracker = new UsageTracker();

    // 转发事件
    this.router.on('route:complete', (data) => this.emit('route:complete', data));
    this.tracker.on('usage:recorded', (data) => this.emit('usage:recorded', data));
  }

  /**
   * 选择最优模型
   */
  selectModel(taskType: CostTaskType, availableModels?: string[]): RoutingResult {
    // 检查预算
    const currentCost = this.tracker.getCurrentPeriodCost(this.config.budgetPeriod);
    if (currentCost >= this.config.budgetLimit * this.config.alertThreshold) {
      this.emit('budget:warning', {
        currentCost,
        budgetLimit: this.config.budgetLimit,
        period: this.config.budgetPeriod,
      });
    }

    if (currentCost >= this.config.budgetLimit) {
      this.emit('budget:exceeded', {
        currentCost,
        budgetLimit: this.config.budgetLimit,
        period: this.config.budgetPeriod,
      });

      // 强制使用最低成本模型
      return {
        modelId: this.config.defaultModel,
        rule: this.router.getRules().find(r => r.taskType === 'simple')!,
        estimatedCost: 0,
        reason: 'Budget exceeded - using lowest cost model',
      };
    }

    return this.router.route(taskType, availableModels);
  }

  /**
   * 记录使用
   */
  recordUsage(
    modelId: string,
    taskType: CostTaskType,
    inputTokens: number,
    outputTokens: number,
    sessionId?: string
  ): UsageRecord {
    if (!this.config.enableTracking) {
      return {
        id: 'tracking_disabled',
        modelId,
        taskType,
        inputTokens,
        outputTokens,
        cost: 0,
        timestamp: Date.now(),
        sessionId,
      };
    }

    return this.tracker.record(modelId, taskType, inputTokens, outputTokens, sessionId);
  }

  /**
   * 获取成本统计
   */
  getStatistics(startTime?: number, endTime?: number): CostStatistics {
    return this.tracker.getStatistics(startTime, endTime);
  }

  /**
   * 获取节省百分比
   */
  getSavingsPercentage(): number {
    const stats = this.tracker.getStatistics();
    if (stats.totalCost === 0) return 0;

    const totalWithSavings = stats.totalCost + stats.estimatedSavings;
    return totalWithSavings > 0
      ? (stats.estimatedSavings / totalWithSavings) * 100
      : 0;
  }

  /**
   * 获取预算状态
   */
  getBudgetStatus(): {
    currentCost: number;
    budgetLimit: number;
    period: string;
    percentUsed: number;
    remaining: number;
  } {
    const currentCost = this.tracker.getCurrentPeriodCost(this.config.budgetPeriod);
    return {
      currentCost,
      budgetLimit: this.config.budgetLimit,
      period: this.config.budgetPeriod,
      percentUsed: (currentCost / this.config.budgetLimit) * 100,
      remaining: Math.max(0, this.config.budgetLimit - currentCost),
    };
  }

  /**
   * 添加路由规则
   */
  addRoutingRule(rule: RoutingRule): void {
    this.router.addRule(rule);
  }

  /**
   * 获取路由规则
   */
  getRoutingRules(): RoutingRule[] {
    return this.router.getRules();
  }

  /**
   * 添加模型成本
   */
  addModelCost(cost: ModelCostInfo): void {
    this.router.addModelCost(cost);
  }

  /**
   * 获取模型成本
   */
  getModelCost(modelId: string): ModelCostInfo | undefined {
    return this.router.getModelCost(modelId);
  }

  /**
   * 获取所有模型成本
   */
  getAllModelCosts(): ModelCostInfo[] {
    return this.router.getAllModelCosts();
  }

  /**
   * 获取使用记录
   */
  getUsageRecords(limit?: number): UsageRecord[] {
    return this.tracker.getRecords(limit);
  }

  /**
   * 更新配置
   */
  updateConfig(config: Partial<CostOptimizerConfig>): void {
    this.config = { ...this.config, ...config };
    this.emit('config:updated', this.config);
  }

  /**
   * 获取配置
   */
  getConfig(): CostOptimizerConfig {
    return { ...this.config };
  }

  /**
   * 重置
   */
  reset(): void {
    this.tracker.clearRecords();
    this.emit('optimizer:reset');
  }
}
